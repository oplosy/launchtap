# P0-C1 — Public wallet page, PnL, leaderboard and holdings notifications

Status: Claude pre-flight, 2026-11-06. Scope and design approved by the product owner in
conversation section by section; awaiting written-spec review before the implementation plan.
Sub-project C of the P0 parity package was split in two: C1 (this spec) is the public wallet
read model; C2 (per-token comments / forum) follows with its own spec. Earlier parts:
`docs/specs/2026-11-05-p0-market-data-discovery-design.md` (A) and
`docs/specs/2026-11-06-p0b-live-feed-notifications-share-design.md` (B).

## Goal

Anyone can open `/wallet/<address>` and see what that wallet holds, what it made or lost, what
it launched and how it traded, and can share that page as a card. A leaderboard ranks wallets by
realized PnL. Notifications from P0-B also fire for tokens the connected wallet holds.

Success means:

- a wallet page shows holdings with average cost, realized and unrealized PnL, totals, trades and
  created tokens, for bonding-curve and post-graduation pool trading alike;
- the numbers survive reorgs and match a recomputation from the canonical ledger;
- `/leaderboard` lists the top 100 wallets by all-time realized PnL;
- a wallet link unfurls into a card; holding a token is enough to get its notifications.

## Non-goals

- Comments, forum (C2). Usernames, ENS, verified profile ownership.
- Time-windowed leaderboards (7d/30d); they need a separate windowed projection.
- Any contract change. No new datastore or service: one new aggregation job type in the existing
  worker, and new tables in the existing PostgreSQL.

## Decisions

| Decision | Rationale | Rejected alternative |
| --- | --- | --- |
| Split C into C1 wallet page and C2 comments. | Two independent subsystems: a read model over the ledger versus the first user-content write path with moderation. | One spec for both. |
| Attribute pool swaps to the transaction sender: new `pool_swaps.tx_from`, read with batched `eth_getTransactionByHash` for blocks that contain pool swaps, written in the same database transaction as the swap row. | Pool swaps currently record no trader (router is the swap sender), so graduated-token PnL is impossible without it. Same-transaction write keeps reorg rollback free. | Curve-only PnL (visibly incomplete for graduated tokens); attributing by Swap `to` (wrong for sells, where the router receives WETH). |
| One-off resumable `cmd/backfill-swap-sender` for existing rows, rate-limited by a flag. Until a row is filled it stays `NULL` and counts as unattributed. | Existing history needs the sender; the indexer must not block on it. | Re-indexing from genesis. |
| `wallet_positions (chain_id, wallet, token)` projection recomputed from the canonical ledger by the existing aggregation worker, driven by a new `position_dirty (chain_id, wallet, token)` queue with the `aggregation_dirty` generation/claim pattern. | Recompute-from-ledger makes reorgs a re-mark, not custom rollback, like `token_stats`. A per-pair queue avoids recomputing every holder of a popular token on each trade. Needed anyway for a ranked leaderboard. | Compute on read plus a periodic leaderboard snapshot (logic in two places, stale ranking); SQL/materialized view (full scans, reorg-hostile). |
| Weighted-average cost basis. Realized PnL = sale proceeds − average cost of tokens sold. Unrealized PnL = balance × current spot − remaining cost, computed at read time from `token_reserves`, never stored. Transfers in carry zero cost and are labelled; transfers out remove cost proportionally. | Standard, explainable, and order-independent within the recomputation. Spot moves continuously, so storing unrealized PnL would be stale. | FIFO (more state, same headline numbers for most users). |
| The pure position calculation lives in a new stdlib-only `internal/position` package with table-driven vectors. | Mirrors how curve math is kept testable and dependency-free. | Logic in SQL. |
| `wallet_totals (chain_id, wallet)` maintained by the same job; the leaderboard ranks by realized PnL only and excludes protocol addresses (router, factory, curves, pairs from the manifest and `tokens`). | Realized PnL is not movable by pumping a spot price. | Ranking by total or unrealized PnL. |
| Live `trade` events and the trades read use `tx_from` as the trader of pool swaps. | Fixes P0-B's gap where own pool trades never notified, and makes wallet trade history complete. | Leaving pool trades anonymous. |

## Backend

- Migration: `pool_swaps.tx_from BYTEA NULL` (20 bytes when set); `wallet_positions`;
  `position_dirty`; `wallet_totals`; indexes `trades (chain_id, trader, block_number,
  transaction_index, log_index)`, `pool_swaps (chain_id, tx_from, …)`, `token_launches
  (chain_id, creator, …)`, `wallet_positions (chain_id, wallet)`, `wallet_totals (chain_id,
  realized_pnl DESC)`. All amounts `NUMERIC(78,0)`.
- Indexer: fetch senders for pool-swap transactions in the batch being ingested; mark
  `position_dirty` for every `(wallet, token)` touched by a curve trade, pool swap or transfer; the
  reorg path re-marks the pairs whose rows it removed.
- Aggregation worker: claims `position_dirty`, recomputes the pair from the ledger with
  `internal/position`, upserts `wallet_positions` and the wallet's `wallet_totals`.
- Metrics: `launchpad_aggregation_position_failures_total`,
  `launchpad_aggregation_position_dirty_backlog` (gauge).

### API (public, rate-limited like other public reads)

Every response carries the usual `snapshot`/`as_of_block`/`finality`; amounts are decimal
strings; an invalid address is 422.

- `GET /v1/wallets/{address}` — totals: portfolio value (ETH), realized and unrealized PnL, held
  token count, created token count, first/last activity block, `attribution_complete`. A wallet
  with no activity returns 200 with zeros.
- `GET /v1/wallets/{address}/positions?cursor=&closed=` — per token: name, symbol, phase,
  balance, average cost, remaining cost, realized PnL, current value, unrealized PnL,
  transferred-in amount; ordered by current value; `closed=true` lists zero-balance positions
  with realized PnL.
- `GET /v1/wallets/{address}/trades?cursor=` — curve trades by `trader` and pool swaps by
  `tx_from`, newest first, `tradeDTO` shape.
- `GET /v1/wallets/{address}/created?cursor=` — launched tokens with phase, market cap, block.
- `GET /v1/leaderboard?limit=` — up to 100: address, realized PnL, tokens traded.

Existing `/profile` and P0-B endpoints are unchanged.

## Web

- `/wallet/[address]`: public, does not load the wallet stack. Header with shortened address,
  copy, totals, Share and Copy link; tabs Holdings (open, with a closed toggle), Trades, Created.
  ETH first, USD only when fresh. "Transfer" badge on transferred-in amounts; "PnL may be
  incomplete" while `attribution_complete` is false; a note that pool trades are attributed to
  the transaction sender. Refetches on a shared-stream `trade` whose trader is this wallet
  (500 ms coalescing), no extra stream.
- Addresses in the live strip, Recent trades and Holders link to the wallet page; a "My wallet"
  nav link when a wallet is connected. `/profile` stays.
- `/leaderboard`: rank, address link, realized PnL, tokens traded; 60 s cache; linked from
  Analytics and the wallet page.
- Wallet share card: `/wallet/[address]/opengraph-image` and `twitter-image`, reusing P0-B's
  card rules (ISR 300 s, font-covered text only, 3 s timeouts, generic card on failure, no
  third-party request); shows shortened address, realized PnL, portfolio value and the top three
  position symbols.
- Notifications: the watched set becomes watchlist ∪ the connected wallet's open positions,
  refreshed on connect, on `reconnected` and when an own trade becomes safe. P0-B rulings on
  wallet-stack availability and identity reset still apply.

## Error handling

- Sender fetch failure: the swap is written with `tx_from = NULL`, the indexer continues, the
  backfill retries it, and the API reports `attribution_complete: false`.
- Position recompute failure: the dirty mark stays and is retried; logged and counted.
- Web: existing error states; share card falls back to the generic card.

## RISKS

- **Attribution.** A swap routed through an aggregator or bot contract is attributed to the EOA
  that sent the transaction. Documented in the UI and docs.
- **Leaderboard gaming.** Wash trading can still realize PnL but costs fees and slippage;
  ranking ignores unrealized PnL; protocol addresses are excluded.
- **RPC load.** One extra call per distinct pool-swap transaction, batched, under the existing
  RPC retry/limit settings; the backfill has its own rate limit.
- **Reorg correctness.** Covered by the existing reorg fixtures: sender rows roll back with their
  swap, and affected pairs recompute.
- **Privacy.** Only public chain data is shown; no Privy identity, e-mail or name.
- **Precision.** All ledger math in wei with `*big.Int` / `NUMERIC`; USD only when fresh.

## BLOCKERS

None. Every input is in the repository; no external account is needed.

## ACCEPTANCE CRITERIA

1. `internal/position` vectors cover buy, partial sell, full sell, transfer in/out, zero balance
   and rounding.
2. An Anvil pool swap is stored with `tx_from`; a reorg removes it with its swap row
   (integration test).
3. After a reorg that removes a trade (existing fixtures), positions and totals equal a fresh
   recomputation without it.
4. Anvil gate: after a buy, a sell and a post-graduation pool sell, the wallet page shows the
   expected realized PnL in wei and updates without reload.
5. Wallet and leaderboard endpoints pass integration tests for limits, ordering, empty wallet,
   invalid address and snapshot; OpenAPI and the generated client have no drift.
6. The leaderboard excludes protocol addresses and ranks the top 100 by realized PnL.
7. A held token's graduation and a ≥ 0.5 ETH trade on it notify; an own pool trade notifies when
   safe (web unit tests).
8. `/wallet/<address>/opengraph-image` returns a 1200×630 PNG with no third-party request.
9. Existing gates stay green: `task verify`, web gates, the Anvil gate and `deploy/check.ps1`.
