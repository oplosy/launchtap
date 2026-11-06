# P0-C1 Public Wallet Page, PnL and Leaderboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Anyone can open `/wallet/<address>` to see holdings, average cost, realized and
unrealized PnL, trades and created tokens, share it as a card, and find top wallets on
`/leaderboard`. Holding a token is enough to get its notifications.

**Architecture:** The indexer records the transaction sender of every pool swap
(`pool_swaps.tx_from`) in the same database transaction as the swap. It marks a new
`position_dirty (chain_id, wallet, token)` queue for every pair touched by a trade, swap or
transfer, and re-marks pairs on reorg. The existing aggregation worker runs a second
`stats.Worker` over that queue. For each pair it loads per-transaction legs from the ledger,
computes the position with a new stdlib-only `internal/position` package and upserts
`wallet_positions` and `wallet_totals`. Unrealized PnL is computed at read time from
`token_reserves`. The public API exposes wallet and leaderboard reads, and the web adds the
wallet and leaderboard pages, a wallet share card and holdings-based notifications.

**Tech Stack:** Go 1.26 (Huma, sqlc, pgx, go-ethereum `rpc`), PostgreSQL 18, Next.js 16
(`next/og`), React 19, Vitest, Playwright, Anvil gate.

**Spec:** `docs/specs/2026-11-06-p0c1-wallet-page-pnl-design.md`

## Global Constraints

- No new dependency in `backend/go.mod` or `web/package.json`.
- No contract change. No new service or datastore.
- `internal/position` is stdlib-only. Add it to the depguard stdlib-only list in
  `backend/.golangci.yml`, next to `internal/curve`.
- Every amount is `*big.Int` / `NUMERIC(78,0)` in Go and SQL, `bigint` in TypeScript, and a
  decimal string in JSON. Never a float. PnL columns are signed; the others are `>= 0` by
  CHECK.
- Prometheus code stays in `internal/metrics`. New metric names, exactly:
  - `launchpad_aggregation_position_failures_total` (counter)
  - `launchpad_aggregation_position_dirty_backlog` (gauge)

  No address becomes a label.
- Sender-fetch failure never stops ingestion. The swap is written with `tx_from = NULL`.
- Public endpoints carry the usual `snapshot` / `as_of_block` / `finality` and are
  rate-limited like the other public reads.
  - An invalid address returns 422.
  - A wallet with no activity returns 200 with zeros.
  - Leaderboard `limit`: default 100, minimum 1, maximum 100.
- The wallet page does not load the wallet stack. It reuses the shared SSE stream (no second
  `EventSource`) and coalesces refetches over 500 ms.
- The wallet card follows the P0-B card rules:
  - 1200×630 PNG, `revalidate = 300`;
  - `cardText` only (font-covered text);
  - 3 s fetch timeouts;
  - the generic card on any failure;
  - no third-party request.
- Migrations, sqlc output, OpenAPI and the generated web client are regenerated, never
  hand-edited (`task sqlc`, `go run ./cmd/check-openapi`, `npm run web-api-sync`).
- Format only touched files. Never run `gofmt`, `forge fmt` or `prettier` unscoped.

## Rulings made while planning

These are decisions the spec leaves open. Each is binding for the tasks below.

- **R1 — Legs are per transaction.** A position is computed from one leg per transaction
  that touched `(wallet, token)`. Each leg has three values:
  - `TokenDelta`: the wallet's net token *transfers* in that tx, signed;
  - `ETHPaid`: ETH the wallet paid in trades attributed to it in that tx;
  - `ETHReceived`: ETH the wallet received in trades attributed to it in that tx.

  So the computed balance always equals `holder_balances`. A trade's token leg is never
  counted twice.
  - Curve buy: `ETHPaid = eth_gross`, which includes fees and excludes the refund.
  - Curve sell: `ETHReceived = eth_gross − protocol_fee − creator_fee`.
  - Pool swap: `ETHPaid` is the WETH-side `amount_in` and `ETHReceived` is the WETH-side
    `amount_out`.
- **R2 — `market_trades` carries the pool-swap sender.** `market_trades` sets the pool-swap
  `trader` to `swap.tx_from` (`NULL` until filled). Live `trade` events, `/v1/trades/recent`
  and token trade lists pick it up with no Go change.
- **R3 — `stats.Claim` gains `Wallet [20]byte`.** It stays zero for token claims. The position
  job is a second `stats.Worker` with a new `PositionSource`, not a new loop.
- **R4 — Excluded addresses.** These are never marked dirty and never ranked:
  - the zero address;
  - every `tokens.curve_address` and `tokens.lp_pair`;
  - the indexer's configured factory.

  The Uniswap router needs no rule: attribution is by transaction sender, and a curve or
  pair counterparty leg never realizes PnL.
- **R5 — `attribution_complete` is chain-wide.** It is `NOT EXISTS (pool_swaps WHERE chain_id
  = $1 AND tx_from IS NULL)`, backed by a partial index.
- **R6 — The created-tokens index goes on `tokens`.** It is `tokens (chain_id, creator,
  launch_block_number DESC)`, not on `token_launches`, because the created list needs phase
  and market cap from `tokens`.

## Review Focus

1. **Self-transfer, or a buy and sell in one tx.** The leg nets to zero tokens. The position
   must not change balance. Realized PnL changes by `received − paid` only. Test in Task 1.
2. **A buy whose tokens go to another recipient.** The swap `to` is not `tx_from`. The sender
   gets no balance and a realized loss of `ETHPaid`. The recipient gets a zero-cost transfer-in.
   Neither position goes negative. Test in Task 1 (sender leg) and Task 5 (SQL loader).
3. **A token launched above the reorg ancestor.** The reorg deletes the token row. Its
   positions must be deleted by the recompute (empty legs), not left orphaned. Test in Task 4.
4. **Pagination and empty states.**
   - A wallet with no activity, and positions `closed=true` with no closed rows, both return
     200 and `items: []`.
   - A cursor from `closed=false` used with `closed=true` returns 422, never a wrong page.

   Test in Task 7.
5. **Unrealized PnL with reserves of zero or missing.** A token with no `token_reserves` row,
   or `token_reserve = 0`, yields `current_value = 0`, not a division error. Test in Task 7.

---

### Task 1: `internal/position` calculator

**Files:**
- Create: `backend/internal/position/position.go`, `backend/internal/position/position_test.go`
- Modify: `backend/.golangci.yml` (stdlib-only depguard list)

**Interfaces:**
- Produces:
  ```go
  package position
  type Leg struct {
      Block       int64
      TxIndex     int32
      TokenDelta  *big.Int // signed net token transfer for the wallet in this tx
      ETHPaid     *big.Int // >= 0, attributed
      ETHReceived *big.Int // >= 0, attributed
  }
  type Position struct {
      Balance, RemainingCost, RealizedPnL, TransferredIn *big.Int
      ETHPaid, ETHReceived                               *big.Int
      TradeCount                                         int64 // legs with ETHPaid>0 or ETHReceived>0
      FirstBlock, LastBlock                              int64
  }
  var ErrUnordered, ErrNegativeBalance, ErrInvalidLeg error
  func Compute(legs []Leg) (Position, error)
  ```
- Leg rules (doc comment on `Compute`). Let `attributed = ETHPaid>0 || ETHReceived>0` and
  `net = ETHPaid − ETHReceived`.
  - `TokenDelta > 0`, attributed:
    - `Balance += d`;
    - `RemainingCost += max(net, 0)`;
    - `RealizedPnL += max(−net, 0)`.
  - `TokenDelta > 0`, not attributed:
    - `Balance += d`;
    - `TransferredIn += d`.
  - `TokenDelta < 0`. Let `s = −d` and
    `removed = (s == Balance) ? RemainingCost : RemainingCost·s / Balance` (floor).
    - `Balance −= s`;
    - `RemainingCost −= removed`;
    - when attributed, `RealizedPnL += ETHReceived − ETHPaid − removed`.
  - `TokenDelta == 0`: `RealizedPnL += ETHReceived − ETHPaid`.
- Errors:
  - `ErrInvalidLeg` for a nil field or a negative ETH value;
  - `ErrUnordered` unless legs ascend strictly by `(Block, TxIndex)`;
  - `ErrNegativeBalance` when `s > Balance`.
- An empty input returns zero values with `FirstBlock = LastBlock = 0`.

- [ ] **Step 1: Write the failing table test** `TestComputeVectors`. Amounts are in wei; tokens
  are in base units. Each case asserts `Balance`, `RemainingCost`, `RealizedPnL` and
  `TransferredIn`.

  | case | legs `(delta, paid, received)` | Balance | RemainingCost | RealizedPnL | TransferredIn |
  | --- | --- | --- | --- | --- | --- |
  | buy | (1000, 1e18, 0) | 1000 | 1e18 | 0 | 0 |
  | partial sell | (1000, 1e18, 0), (−400, 0, 6e17) | 600 | 6e17 | 2e17 | 0 |
  | full sell | (1000, 1e18, 0), (−1000, 0, 5e17) | 0 | 0 | −5e17 | 0 |
  | transfer in then sell | (500, 0, 0), (−500, 0, 1e17) | 0 | 0 | 1e17 | 500 |
  | transfer out | (1000, 1e18, 0), (−250, 0, 0) | 750 | 75e16 | 0 | 0 |
  | rebuy after zero | (10, 100, 0), (−10, 0, 50), (5, 20, 0) | 5 | 20 | −50 | 0 |
  | round trip in tx | (0, 1e18, 9e17) | 0 | 0 | −1e17 | 0 |
  | bought for another | (0, 1e18, 0) | 0 | 0 | −1e18 | 0 |

  Rounding case: legs (3, 10, 0), (−1, 0, 0), (−2, 0, 0). After the second leg
  `RemainingCost = 7` (removed 3, floor). After the third, all 7 is removed and
  `RemainingCost = 0`, with no realized PnL because the legs are not attributed.

  Also write `TestComputeErrors`:
  - unordered legs → `ErrUnordered`;
  - (10, 1, 0), (−11, 0, 0) → `ErrNegativeBalance`;
  - a nil `TokenDelta` → `ErrInvalidLeg`.
- [ ] **Step 2:** Run `cd backend && go test ./internal/position/`. Expected: FAIL (package
  missing).
- [ ] **Step 3: Implement `Compute`** in `position.go` with `math/big` only, copying every
  input. Add `internal/position` to the stdlib-only depguard rule.
- [ ] **Step 4:** Run `go test ./internal/position/` and `task lint`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: add position cost-basis calculator (Plan P0-C1 Task 1)`

### Task 2: Ledger schema and pool-swap sender storage

**Files:**
- Create: `backend/internal/store/postgres/migrations/00017_wallet_positions.sql`
- Modify:
  - `backend/internal/ledger/events.go` (`PoolSwap.TxFrom *common.Address`)
  - `backend/internal/store/postgres/queries/events.sql` (`InsertPoolSwap`, `PoolSwapMatchesEvent`)
  - the pool-swap insert in `backend/internal/store/postgres/adapter.go`
  - regenerated `sqlc/*`
- Test: `backend/internal/store/postgres/postgrestest/wallet_schema_integration_test.go`

**Interfaces:**
- Migration Up:
  - `ALTER TABLE pool_swaps ADD COLUMN tx_from BYTEA NULL`, with CHECK
    `tx_from IS NULL OR octet_length(tx_from) = 20`.
  - `CREATE SEQUENCE position_dirty_generation_seq`.
  - `position_dirty (chain_id, wallet, token_address, generation, claimed_generation,
    claimed_at, claimed_by)`: PK `(chain_id, wallet, token_address)`, the same CHECKs as
    `aggregation_dirty`, and **no** FK to `tokens` (R3 / Review Focus 3).
  - `wallet_positions (chain_id, wallet, token_address, balance, remaining_cost, realized_pnl,
    transferred_in, eth_paid, eth_received, trade_count BIGINT, first_block_number,
    last_block_number)`: PK `(chain_id, wallet, token_address)`.
  - `wallet_totals (chain_id, wallet, realized_pnl, tokens_traded BIGINT, held_count BIGINT,
    first_block_number, last_block_number)`: PK `(chain_id, wallet)`.
  - Indexes:
    - `trades (chain_id, trader, block_number DESC, transaction_index DESC, log_index DESC)`
    - `pool_swaps (chain_id, tx_from, block_number DESC, transaction_index DESC, log_index DESC)`
    - `pool_swaps (chain_id) WHERE tx_from IS NULL`
    - `transfers (chain_id, to_address, token_address)`
    - `transfers (chain_id, from_address, token_address)`
    - `tokens (chain_id, creator, launch_block_number DESC)`
    - `wallet_positions (chain_id, wallet)`
    - `wallet_totals (chain_id, realized_pnl DESC, wallet)`
  - `CREATE OR REPLACE VIEW market_trades`, identical except that `NULL::BYTEA AS trader`
    becomes `swap.tx_from AS trader`.
- Migration Down reverses every step, restoring the old view text verbatim.
- `InsertPoolSwap` writes `tx_from`. `PoolSwapMatchesEvent` compares it with
  `IS NOT DISTINCT FROM`, so a replayed swap with the same sender is idempotent. A replay
  that fills a `NULL` sender stays the backfill's job, not the indexer's.

- [ ] **Step 1: Write the failing integration tests:**
  - `TestPoolSwapStoresTxFrom`: an ingested swap with `TxFrom` set reads back from
    `market_trades` with `trader = TxFrom`;
  - `TestPoolSwapNullTxFrom`: `TxFrom` nil gives `trader IS NULL`;
  - `TestRecentTradesCarryPoolTrader`: `/v1/trades/recent` via the feed reader returns the
    sender.
- [ ] **Step 2:** Run `cd backend && go test -tags integration ./internal/store/postgres/postgrestest/ -run 'PoolSwap|RecentTradesCarry'`.
  Expected: FAIL.
- [ ] **Step 3: Implement** the migration, queries and adapter change. Run `task sqlc`.
- [ ] **Step 4:** Run `task integration` and `task verify` (which includes migrations up/down/up).
  Expected: PASS.
- [ ] **Step 5: Commit** `feat: store pool swap sender and wallet position schema (Plan P0-C1 Task 2)`

### Task 3: Indexer fetches pool-swap senders

**Files:**
- Modify:
  - `backend/internal/chain/rpc.go` (`TransactionSenders`)
  - `backend/internal/chain/types.go` (`PoolSwap.TxFrom`, not ABI-decoded)
  - `backend/internal/indexer/ports.go` (`SenderSource`)
  - `backend/internal/indexer/engine.go`
  - `backend/internal/indexer/router.go` (copy `TxFrom`)
  - `backend/internal/metrics/indexer.go` (allow `eth_getTransactionByHash`)
  - `backend/cmd/indexer/main.go`
- Test:
  - `backend/internal/chain/rpc_test.go`
  - `backend/internal/indexer/engine_test.go`
  - `backend/internal/indexer/anvil_e2e_integration_test.go`

**Interfaces:**
- Produces:
  ```go
  // chain
  func (c *Client) TransactionSenders(ctx context.Context, hashes []common.Hash) (map[common.Hash]common.Address, error)
  // indexer
  type SenderSource interface {
      TransactionSenders(context.Context, []common.Hash) (map[common.Hash]common.Address, error)
  }
  ```
- `TransactionSenders` details:
  - one `BatchCallContext` of `eth_getTransactionByHash` per 100 hashes;
  - decodes only the `from` field;
  - goes through the client's existing retry and capacity handling with method label
    `eth_getTransactionByHash`;
  - a `null` result or a per-element error leaves that hash out of the map without failing
    the call.
- Engine: after decoding a chunk and before the write transaction, it collects the distinct
  `TxHash` of `chain.PoolSwap` logs and calls `SenderSource` once.
  - On error: log at warn with the chain ID and count, increment no RPC-health failure, and
    continue with an empty map.
  - It sets `PoolSwap.TxFrom` from the map.
  - `SenderSource` is optional. When nil, every `TxFrom` stays nil.

- [ ] **Step 1: Write the failing tests:**
  - `TestTransactionSendersBatchesAndSkipsMissing`: an httptest JSON-RPC server with 150
    hashes, one `null` → 2 batches, map size 149.
  - `TestEngineAttachesPoolSwapSender`: fake source plus fake `SenderSource` → the routed
    `ledger.PoolSwap.TxFrom` equals the fake sender.
  - `TestEngineContinuesWhenSenderFetchFails`: `SenderSource` returns an error → `Step`
    succeeds and `TxFrom` is nil.
- [ ] **Step 2:** Run `go test ./internal/chain/ ./internal/indexer/`. Expected: FAIL.
- [ ] **Step 3: Implement**, and wire `chain.Client` as `SenderSource` in `cmd/indexer/main.go`.
- [ ] **Step 4: Extend the Anvil integration test** (spec AC2): `TestAnvilPoolSwapSenderAndReorg`.
  1. Graduate a token, then do a router sell from a funded key.
  2. Assert the `pool_swaps.tx_from` row equals that key's address.
  3. Revert Anvil to a snapshot before the swap and mine a replacement block, as the
     existing reorg fixtures do.
  4. Assert the swap row, and with it `tx_from`, is gone.

  Run `go test -tags integration ./internal/indexer/ -run Anvil` with Anvil on PATH.
  Expected: PASS.
- [ ] **Step 5: Commit** `feat: attribute pool swaps to the transaction sender (Plan P0-C1 Task 3)`

### Task 4: Position dirty marking and reorg re-mark

**Files:**
- Modify:
  - `backend/internal/store/postgres/queries/projections.sql`
  - `backend/internal/store/postgres/queries/reorg.sql`
  - `backend/internal/store/postgres/adapter.go` (`IngestTrade`, `IngestPoolSwap`,
    `IngestTransfer`)
  - the reorg recovery adapter methods
  - `backend/internal/indexer/reorg.go` (`RecoveryUnit`)
  - `backend/internal/indexer/engine.go` (factory to the store)
- Test: `backend/internal/store/postgres/postgrestest/position_dirty_integration_test.go`

**Interfaces:**
- SQL:
  - `MarkPositionDirty(chain_id, wallet, token)`: upsert with
    `nextval('position_dirty_generation_seq')`, skipping excluded addresses (R4). The
    factory is passed as a parameter.
  - `MarkTransferPositionsDirty(chain_id, token, from, to, factory)`: marks both sides with
    the same exclusions.
  - `MarkPoolSwapPositionDirty(chain_id, pair, tx_from, factory)`: resolves the token by
    `lp_pair`; no-op when `tx_from` is NULL.
  - `ListPositionPairsAbove(chain_id, block) → (wallet, token)[]`: distinct pairs from
    `trades.trader`, `pool_swaps.tx_from` joined to `tokens.lp_pair`, and both transfer
    sides above `block`.
- Go:
  ```go
  // indexer.RecoveryUnit gains:
  AffectedPositionPairsAbove(ctx context.Context, chainID, block int64) ([]PositionPair, error)
  MarkPositionsDirty(ctx context.Context, chainID int64, pairs []PositionPair) error
  type PositionPair struct{ Wallet, Token common.Address }
  ```
- The adapter's factory comes from a field set at construction:
  `func (a *Adapter) WithFactory(factory common.Address) *Adapter`, called in `cmd/indexer/main.go` with the configured factory.
- Reorg order: `AffectedPositionPairsAbove` before `DeleteCanonicalAbove`, then
  `MarkPositionsDirty` after it, all in the existing recovery transaction.

- [ ] **Step 1: Write the failing integration tests:**
  - `TestIngestMarksPositionPairs`: a curve trade marks `(trader, token)`; a transfer
    curve→wallet marks only the wallet; a pool swap with `TxFrom` marks `(tx_from, token)`.
  - `TestReorgRemarksPositionPairs`: after recovery deletes a trade above the ancestor, its
    pair is dirty with a higher generation.
  - `TestReorgRemarksPairsOfDeletedToken`: a token launched above the ancestor still has its
    pairs marked even though its `tokens` row is gone (Review Focus 3).
- [ ] **Step 2:** Run `go test -tags integration ./internal/store/postgres/postgrestest/ -run Position`. Expected: FAIL.
- [ ] **Step 3: Implement** the queries, adapter calls and recovery methods. Run `task sqlc`.
- [ ] **Step 4:** Run `task integration` plus `go test ./internal/indexer/`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: mark wallet positions dirty on ingest and reorg (Plan P0-C1 Task 4)`

### Task 5: Position recompute job and metrics

**Files:**
- Create: `backend/internal/store/postgres/position_source.go`
- Modify:
  - `backend/internal/stats/worker.go` (`Claim.Wallet`)
  - `backend/internal/store/postgres/queries/projections.sql`
  - `backend/internal/store/postgres/queries/indexer.sql` (`ReadOperationalHealth`
    `position_dirty_work`)
  - `backend/internal/indexer/health.go`
  - `backend/internal/metrics/indexer.go`
  - `backend/cmd/indexer/main.go`
- Test:
  - `backend/internal/store/postgres/postgrestest/position_source_integration_test.go`
  - `backend/internal/metrics/indexer_test.go`

**Interfaces:**
- Consumes: `position.Compute`, `position.Leg` (Task 1); `position_dirty` and the marks
  (Tasks 2, 4).
- Produces:
  ```go
  type PositionSource struct{ Adapter *Adapter; ChainID int64 } // implements stats.DirtySource
  func (s PositionSource) Claim(ctx context.Context, worker string, batch int32) ([]stats.Claim, error)
  func (s PositionSource) Compute(ctx context.Context, claim stats.Claim) error
  func (s PositionSource) Complete(ctx context.Context, claim stats.Claim, worker string) (bool, error)
  ```
- SQL `ListPositionLegs(chain_id, wallet, token)` returns one row per transaction, ordered
  by `(block_number, transaction_index)`, with the R1 values:
  - `token_delta` from `transfers`;
  - `eth_paid` / `eth_received` from `trades WHERE trader = wallet` and
    `pool_swaps WHERE tx_from = wallet AND pair = tokens.lp_pair`;
  - a full outer join on `tx_hash`.
- `Compute` runs in one transaction:
  - no legs → delete the `wallet_positions` row;
  - otherwise upsert it;
  - then recompute `wallet_totals` for the wallet from its `wallet_positions` aggregate:
    `tokens_traded = count(trade_count > 0)`, `held_count = count(balance > 0)`. Delete the
    totals row when the wallet has no positions.
- Claim, Complete and stale-claim recovery copy the `aggregation_dirty` queries, with
  `wallet` added to keys.
- Metrics in `internal/metrics/indexer.go`:
  - counter `launchpad_aggregation_position_failures_total`, fed by the position worker's
    `OnError`, which also logs with chain ID and error;
  - gauge `launchpad_aggregation_position_dirty_backlog`, read from
    `indexer.Health.PositionDirtyWork`.
- `cmd/indexer/main.go` starts a second `stats.Worker` with `PositionSource`, the same
  wake channel, and `BatchSize: 64`.

- [ ] **Step 1: Write the failing tests:**
  - `TestPositionRecomputeMatchesLedger`: curve buy, curve sell, then a pool swap sell
    (`tx_from` set) for one wallet. Assert `wallet_positions` equals `position.Compute` over
    the expected legs, written out in the test, and `wallet_totals.realized_pnl` equals the
    position's.
  - `TestPositionRecipientDiffersFromSender` (Review Focus 2): a swap with
    `tx_from = A` and `to = B`. Expect:
    - `A`: balance 0 and realized `−ETHPaid`;
    - `B`: `transferred_in` equal to the amount and cost 0.
  - `TestReorgRecomputeEqualsFresh` (spec AC3): build the ledger, compute, then run reorg
    recovery removing the last trade and drain the worker. Assert the positions and totals
    rows are byte-equal to a second database built without that trade and drained.
  - `TestPositionMetricsRegistered`: both metric names are present on a registry.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3: Implement**, and run `task sqlc`.
- [ ] **Step 4:** Run `task verify`. Expected: PASS, apart from known Windows CRLF false
  positives, which must be named in the report.
- [ ] **Step 5: Commit** `feat: recompute wallet positions in the aggregation worker (Plan P0-C1 Task 5)`

### Task 6: Swap-sender backfill command

**Files:**
- Create: `backend/cmd/backfill-swap-sender/main.go`, `backend/cmd/backfill-swap-sender/main_test.go`
- Modify:
  - `backend/internal/store/postgres/queries/projections.sql` (`ListUnattributedPoolSwaps`,
    `SetPoolSwapTxFrom`)
  - `backend/Taskfile.yml` (build target if commands are enumerated)
  - `docs/runbooks/production-readiness.md`

**Interfaces:**
- Consumes: `chain.Client.TransactionSenders` (Task 3); `MarkPoolSwapPositionDirty` (Task 4).
- Flags:
  - `-rate` (requests per second, default 5);
  - `-batch` (hashes per call, default 50, maximum 100);
  - `-dry-run`.

  It reads `DATABASE_URL` and the RPC URL through the same `internal/config` loader the
  indexer uses.
- Loop:
  1. Select up to `batch` distinct `tx_hash` from `pool_swaps WHERE chain_id = $1 AND
     tx_from IS NULL ORDER BY block_number, tx_hash`.
  2. Fetch senders.
  3. In one transaction per batch, run `UPDATE … SET tx_from = $sender WHERE tx_hash = $h
     AND tx_from IS NULL` and mark the position pair dirty.
  4. Sleep to respect `-rate`.

  It stops when the select is empty. It is resumable because `tx_from IS NULL` is the
  cursor.
- A hash the RPC cannot resolve is skipped for this run and logged. The run exits non-zero
  if any were skipped.

- [ ] **Step 1: Write the failing tests:**
  - `TestBackfillFillsAndMarks` (integration): two NULL swaps → both filled and both pairs
    dirty.
  - `TestBackfillResumable`: the second run updates nothing.
  - `TestBackfillSkipsUnresolved`: exit status 1, and the row stays NULL.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3: Implement.** Add a runbook section "Backfill pool swap senders" with the
  command, flags and how to read `attribution_complete`.
- [ ] **Step 4:** Run the tests and `task lint`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: add resumable pool swap sender backfill (Plan P0-C1 Task 6)`

### Task 7: Wallet and leaderboard API

**Files:**
- Create:
  - `backend/internal/apiserver/wallet_routes.go`, `backend/internal/apiserver/wallet_routes_test.go`
  - `backend/internal/store/postgres/wallet_reader.go`
  - `backend/internal/store/postgres/postgrestest/wallet_reader_integration_test.go`
- Modify:
  - `backend/internal/store/postgres/queries/read.sql`
  - `backend/cmd/api/main.go` (register)
  - the OpenAPI snapshot via `go run ./cmd/check-openapi --write`
  - `web/src/api/generated.ts` via `npm run web-api-sync`

**Interfaces:**
- Routes (Huma, tag `wallets`; the leaderboard is tag `stats`):

  | OperationID | Path | Query |
  | --- | --- | --- |
  | `getWallet` | `GET /wallets/{address}` | — |
  | `listWalletPositions` | `GET /wallets/{address}/positions` | `cursor`, `closed` (bool, default false), `limit` (default 50, max 100) |
  | `listWalletTrades` | `GET /wallets/{address}/trades` | `cursor`, `limit` (default 50, max 100); items are `tradeDTO` |
  | `listWalletCreated` | `GET /wallets/{address}/created` | `cursor`, `limit` (default 50, max 100) |
  | `getLeaderboard` | `GET /leaderboard` | `limit` (default 100, 1..100) |

- DTO fields. Amounts are decimal strings; blocks are integers.
  - Wallet: `address`, `realized_pnl_wei`, `unrealized_pnl_wei`, `portfolio_value_wei`,
    `held_count`, `created_count`, `tokens_traded`, `first_block` (nullable),
    `last_block` (nullable), `attribution_complete`.
  - Position: `token`, `name`, `symbol`, `phase`, `balance`, `average_cost_wad` (nullable
    when balance is 0; `remaining_cost·1e18 / balance`, floor), `remaining_cost_wei`,
    `realized_pnl_wei`, `current_value_wei`, `unrealized_pnl_wei`, `transferred_in`,
    `trade_count`, `last_block`.
  - Created: `token`, `name`, `symbol`, `phase`, `market_cap_wei`, `launch_block`.
  - Leaderboard item: `rank`, `address`, `realized_pnl_wei`, `tokens_traded`.
- Read-time value, in SQL with a comment naming it the single source:
  `current_value = trunc(balance · eth_reserve / NULLIF(token_reserve, 0))`, coalesced to 0.
  `unrealized = current_value − remaining_cost`.
  - Open positions: `balance > 0`, ordered by `current_value DESC, token_address`.
  - Closed positions: `balance = 0`, ordered by `last_block_number DESC, token_address`.
  - The cursor encodes the mode. A mismatched mode is 422.
- Wallet trades: `market_trades WHERE chain_id = $1 AND trader = $2`, newest first, with the
  existing trade cursor.
- Leaderboard: `wallet_totals`, excluding the zero address and every `tokens.curve_address`
  / `tokens.lp_pair` at read time (the factory is never marked dirty, so it never has totals;
  R4), `ORDER BY realized_pnl DESC, wallet`.
- Every read uses the existing snapshot reader and public rate limiter. The address is
  parsed with the same helper token routes use, giving 422 on failure.

- [ ] **Step 1: Write the failing tests** (integration, spec AC5/AC6):
  - `TestWalletEmptyReturnsZeros`: 200, zeros, `attribution_complete: true`.
  - `TestWalletInvalidAddress422`, for every wallet route.
  - `TestWalletPositionsOrderAndClosedMode`.
  - `TestWalletPositionsCursorModeMismatch422`.
  - `TestWalletPositionsMissingReserves` (Review Focus 5): `current_value_wei == "0"`.
  - `TestWalletTradesIncludePoolSwapsBySender`.
  - `TestWalletAttributionIncomplete`: one NULL `tx_from` → false.
  - `TestLeaderboardRanksAndExcludesProtocol`: a pair address with totals is absent; the
    order is by realized PnL; `limit = 101` → 422.
  - `TestWalletSnapshotFields`.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3: Implement** the routes and reader. Run `task sqlc`, `go run ./cmd/check-openapi --write`, then run
  `npm run web-api-sync` in `web/`.
- [ ] **Step 4:** Run `task verify` and `npm run web-api-diff`. Expected: PASS, apart from known
  CRLF false positives.
- [ ] **Step 5: Commit** `feat: add public wallet and leaderboard endpoints (Plan P0-C1 Task 7)`

### Task 8: Wallet page, leaderboard page and address links

**Files:**
- Create:
  - `web/app/wallet/[address]/page.tsx`
  - `web/app/leaderboard/page.tsx`
  - `web/src/portfolio/wallet-model.ts`, `web/src/portfolio/wallet-model.test.ts`
  - `web/src/portfolio/wallet-view.tsx`
  - `web/src/portfolio/leaderboard-view.tsx`
  - `web/e2e/wallet-page.spec.ts`
- Modify:
  - `web/src/live/live-strip.tsx` (trader links)
  - `web/src/token/token-detail.tsx` (Recent trades and Holders links)
  - `web/src/components/app-shell.tsx` ("My wallet" nav link)
  - `web/app/analytics/page.tsx` (Leaderboard link)
  - `web/app/styles/pages.css`
  - `web/scripts/run-e2e-live-strip.mjs` (add `wallet-page` to the spec list)
  - `web/performance-budgets.json` / `web/scripts/check-budgets.mjs` (add `/leaderboard`
    if routes are enumerated)

**Interfaces:**
- Consumes: the generated client operations from Task 7; `useLiveEvents` (P0-B);
  `shortAddress` and `parseTokenAddress` from `src/token/address.ts`.
- Produces in `wallet-model.ts`:
  ```ts
  export const WALLET_REFETCH_COALESCE_MS = 500;
  export function walletPath(address: string): string;            // "/wallet/0x…" lowercased
  export function isWalletTrade(event: LiveEvent, wallet: string): boolean; // trade event, trader equals wallet case-insensitively; null trader → false
  export function pnlTone(wei: string): "gain" | "loss" | "flat";
  export function formatSignedEth(wei: string): string;           // "+0.2 ETH" / "−0.5 ETH" / "0 ETH", bigint math only
  export function createCoalescer(ms: number, run: () => void): { trigger(): void; cancel(): void };
  ```
- Page details:
  - Header: short address, copy, totals, Share and Copy link.
  - Tabs: Holdings (with a "Show closed" toggle), Trades, Created.
  - A "Transfer" badge when `transferred_in > 0`.
  - The banner "PnL may be incomplete" when `attribution_complete` is false.
  - The note "Pool trades are attributed to the transaction sender."
  - ETH first; USD only when the existing fresh-price helper says fresh.
  - An invalid address param renders the existing not-found page.
- `/leaderboard`: a server component with `export const revalidate = 60`, showing rank,
  address link, realized PnL and tokens traded. Linked from Analytics and the wallet header.
- "My wallet" appears only when the existing connection context reports an address. It
  must not import the wallet stack into the wallet page bundle.

- [ ] **Step 1: Write the failing unit tests** in `wallet-model.test.ts`:
  - `isWalletTrade` true for a matching mixed-case trader;
  - false for a null trader, another wallet, or a graduation event;
  - `formatSignedEth("200000000000000000") === "+0.2 ETH"`;
  - `formatSignedEth("-500000000000000000") === "−0.5 ETH"`;
  - `formatSignedEth("0") === "0 ETH"`;
  - the coalescer runs once for three triggers within 500 ms (fake timers).
- [ ] **Step 2:** Run `npm test -- wallet-model`. Expected: FAIL.
- [ ] **Step 3: Implement** the model, views, pages and links.
- [ ] **Step 4: Write the e2e** `wallet-page.spec.ts`, mocked with `page.route` like `live-strip.spec.ts`:
  - totals and Holdings render;
  - the closed toggle shows a closed row;
  - the incomplete banner shows when `attribution_complete: false`;
  - a mocked SSE `trade` for this wallet triggers exactly one refetch of `/wallets/{a}`;
  - a strip trader link navigates to `/wallet/<trader>`;
  - `/leaderboard` lists ranks;
  - the wallet page makes no request to any wallet-stack chunk. Assert no
    `connection`/Privy script request, using the pattern P0-B used for the token page.
- [ ] **Step 5:** Run `npm run format:check` (touched files), `npm run lint`, `npm run typecheck`,
  `npm test` and `npm run test:e2e:live`. Expected: PASS.
- [ ] **Step 6: Commit** `feat(web): add wallet page, leaderboard and address links (Plan P0-C1 Task 8)`

### Task 9: Wallet share card

**Files:**
- Create:
  - `web/app/wallet/[address]/opengraph-image.tsx`
  - `web/app/wallet/[address]/twitter-image.tsx` (the same ISR literals as the token card,
    not a bare re-export, matching the P0-B fix)
  - `web/src/share/wallet-card-data.ts`, `web/src/share/wallet-card-data.test.ts`
- Modify:
  - `web/app/wallet/[address]/page.tsx` (`generateMetadata` with card URLs and a 3 s
    bound, like the token page)
  - `web/e2e/wallet-page.spec.ts`

**Interfaces:**
- Consumes: `cardText`, `FETCH_TIMEOUT_MS`, `CARD_REVALIDATE_SECONDS` and
  `apiInternalBaseUrl()` (P0-B); `getWallet` and `listWalletPositions` (Task 7).
- Produces:
  ```ts
  export type WalletCardData =
    | { kind: "wallet"; address: string; realizedPnlWei: string; portfolioValueWei: string; topSymbols: string[] } // ≤ 3, cardText-cleaned
    | { kind: "generic" };
  export async function loadWalletCardData(address: string, fetcher?: typeof fetch): Promise<WalletCardData>;
  ```

- [ ] **Step 1: Write the failing tests:**
  - an invalid address → generic;
  - a fetch timeout (3 s, fake timers) → generic;
  - non-200 → generic;
  - symbols with emoji/CJK are cleaned and at most 3 are kept;
  - an empty wallet → `kind: "wallet"` with `"0"` values.
- [ ] **Step 2:** Run `npm test -- wallet-card-data`. Expected: FAIL.
- [ ] **Step 3: Implement** the loader and image routes: `size` 1200×630, `revalidate = 300`,
  `generateStaticParams` returning `[]`. Text shows the short address, signed realized PnL,
  portfolio value and up to three symbols.
- [ ] **Step 4: Extend the e2e** (spec AC8): `GET /wallet/<a>/opengraph-image` returns
  `image/png` at 1200×630, and the page makes no request to a host other than the app and
  the mocked API.
- [ ] **Step 5:** Run the web gates as in Task 8. Expected: PASS.
- [ ] **Step 6: Commit** `feat(web): add wallet share card (Plan P0-C1 Task 9)`

### Task 10: Holdings-based notifications

**Files:**
- Create: `web/src/live/holdings.ts`, `web/src/live/holdings.test.ts`
- Modify:
  - `web/src/live/notification-bridge.tsx` (the watched set)
  - `web/src/live/notification-rules.ts`, only if a dex trader-null assumption must go
  - `web/e2e/notifications.spec.ts`

**Interfaces:**
- Consumes: `listWalletPositions` (Task 7, `closed=false`, `limit=100`, first page only);
  P0-B `evaluate`, `identityChanged`, `bellVisible`; the `reconnected` live event.
- Produces:
  ```ts
  export function mergeWatched(watchlist: readonly string[], holdings: readonly string[]): string[]; // lowercased, deduped, sorted
  export function holdingsRefreshReason(event: LiveEvent, wallet: string | null, rules: RuleState): "reconnected" | "own_trade_safe" | null;
  export async function loadHoldings(wallet: string, fetcher?: typeof fetch): Promise<string[]>; // [] on any failure
  ```
- Watched set: `mergeWatched(signedIn ? watchlist : [], connected ? holdings : [])`. Holdings
  need a connected address, not sign-in.
- Holdings refresh on: connect or address change, `reconnected`, and an own trade turning
  safe. An identity change clears holdings before the refetch, as P0-B's identity reset
  does.

- [ ] **Step 1: Write the failing unit tests** (spec AC7):
  - `mergeWatched` dedupes mixed case;
  - with holdings `[T]` and an empty watchlist, `evaluate` raises a notification for a
    graduation of `T` and for a 0.5 ETH trade on `T`;
  - an own **dex** trade (trader = connected wallet) raises `own_trade_safe` when its
    finality reaches safe;
  - `holdingsRefreshReason` returns `"own_trade_safe"` for that event and `"reconnected"`
    for a reconnect;
  - `loadHoldings` returns `[]` on a 500.
- [ ] **Step 2:** Run `npm test -- holdings notification-rules`. Expected: FAIL.
- [ ] **Step 3: Implement**, and remove any rule that drops dex trades for own-trade matching.
- [ ] **Step 4: Extend** `notifications.spec.ts`: a connected wallet holding a mocked token gets
  a toast for that token's graduation without a watchlist entry.
- [ ] **Step 5:** Run the web gates as in Task 8. Expected: PASS.
- [ ] **Step 6: Commit** `feat(web): notify for tokens the connected wallet holds (Plan P0-C1 Task 10)`

### Task 11: Anvil gate, docs and backlog

**Files:**
- Modify:
  - `web/e2e/task6-anvil.spec.ts` (extend "uses the reviewed graduated router…")
  - `docs/runbooks/production-readiness.md` (Monitoring: both position metrics; backfill
    reference)
  - `backlog.md`
  - `notes.md`, only if it lists Wallet/Leaderboard as missing

**Interfaces:**
- Consumes: every earlier task.

- [ ] **Step 1: Write the failing Anvil test** (spec AC4): `wallet page shows realized PnL after
  curve buy, curve sell and pool sell without reload`.
  1. Open `/wallet/<test account>` in a second page before trading.
  2. Run the existing buy, sell, graduation and router sell.
  3. Compute the expected realized PnL in the test from the receipts' `Trade` and `Swap`
     event amounts, using the R1 rules and the Task 1 formula.
  4. Assert the page's realized PnL text equals it, within 10 s and without `page.reload()`.
- [ ] **Step 2:** Run `npm run test:anvil` (PowerShell, Foundry on PATH). Expected: FAIL before,
  PASS after any wiring fix.
- [ ] **Step 3: Docs.**
  - Runbook: the two metrics, a backlog alert threshold note, and the backfill command.
  - Backlog: move "P0-C1" to Done with the commit range and gate evidence. Add any deferred
    review minors as one Active item. Keep C2 (comments) as the next sub-project.
- [ ] **Step 4: Run the full gates** (spec AC9):
  - `cd backend && task verify`;
  - web `format:check`, `lint`, `typecheck`, `test`, `test:e2e:live`, `test:anvil`;
  - `pwsh -File deploy/check.ps1 -Build` if `pwsh` exists, otherwise CI.

  Expected: PASS, apart from the known Windows CRLF false positives, each named.
- [ ] **Step 5: Commit** `test: cover wallet PnL in the Anvil gate and document P0-C1`
