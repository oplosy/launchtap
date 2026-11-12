# Launch Engine v2 — Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement
> this plan task-by-task (no subagent-driven development in this repository). Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Index, project and quote an engine v2 deployment. That covers:
- the anti-snipe tax;
- the Uniswap V4 pool phase read from the singleton `PoolManager`;
- the LP fee locker;
- the two-step creator fee recipient.

The work runs through the same canonical ledger, reorg handling and API as v1, and an Anvil
e2e proves the projections match on-chain views.

**Architecture:** The indexer process still serves exactly one deployment, and that
deployment selects the engine.
- **Decoder:** `chain.NewDecoder(2)` loads `abi/v2` and gains two emitter kinds, `Locker`
  and `PoolManager`.
- **Discovery:** V4 logs are fetched by `address = PoolManager` and `topic1 ∈ known
  poolIds`. A pool becomes known at `Graduated`, and the same-block refetch works as it
  does for v1 pairs.
- **Ledger:** v2-only events land in new ledger tables; v1 tables are reused where the
  event is unchanged.
- **Projections:** engine-aware. For a graduated v2 token, the price comes from the latest
  `sqrtPriceX96`, not from reserves.
- **Unchanged:** the one-deployment-per-chain rule keeps every ledger table chain-scoped, as
  it is today.

**Tech Stack:** Go 1.26.x, PostgreSQL (`NUMERIC(78,0)`), goose migrations, sqlc,
go-ethereum (existing pin). No new dependency.

**Inputs (normative):**
- `docs/specs/2026-11-11-v2-engine-design.md` §4–§8;
- `docs/audits/2026-11-11-v2-backend-preflight.md`, including its *Decisions* section
  (B1–B5);
- the frozen `contracts/abi/v2`, `contracts/fixtures/v2/event-logs-v2.json` and
  `contracts/vectors/v2/curve-v2.json`.

**Out of this plan:**
- web: the V4 swap path, Permit2 sells, the locker claims UI and the tax countdown;
- any mainnet v2 deployment record, which waits for the external audit.

## Global Constraints

- **One deployment per chain per database (B2).** Startup fails closed if `sync_state` holds
  a row for another deployment on the same chain.
- **Number types.** Every on-chain amount is `*big.Int` or `NUMERIC(78,0)`. Signed V4 deltas
  (`int128`, `int256`, `int24`) are stored as signed `NUMERIC(78,0)` or `INTEGER`; never
  a float.
- **`internal/curve` stays stdlib-only** (depguard). The tax mirror documents the Solidity
  function it mirrors (`// mirrors AntiSnipe.taxBps`).
- **v1 behaviour must not change.** Every existing test and the v1 Anvil e2e stay green.
  The v1 rows that already exist are not migrated, apart from the column-nullability change
  described in Task 5.
- **Single-sourced artefacts.** `abi/v2`, `fixtures/v2` and `vectors/v2` are copied
  byte-identical by the sync commands and checked by `task verify`. They are never
  hand-edited at the copy site.
- **Pool key constants** come from the manifest and the spec, never from a log:
  - `currency0 = 0x0` (native ETH);
  - `fee = 10000`;
  - `tickSpacing = 200`;
  - `hooks = manifest.hook`.
- **Commits:** conventional prefix, imperative mood, one task per commit, referencing
  `Plan v2-B Task N`.

## Review Focus

1. **A swap in the same block as graduation, after the `Graduated` log.**
   - Expected: it is fetched in the same step and attributed to the pool.
   - Pinned in Task 6.
2. **A swap on a pool with the same token but a foreign hook or fee.**
   - Expected: it is invisible, because its poolId is unknown.
   - Pinned in Task 6.
3. **A reorg across the graduation block.**
   - Expected: the pool registry, V4 rows and the `graduated` phase roll back, and
     re-indexing produces an identical result.
   - Pinned in Task 11.
4. **A creator fee earned before and after `FeeRecipientAccepted` in one block.**
   - Expected: each fee is credited to the recipient in force at its log index, and the
     result equals the on-chain `creatorFeesOwed` and `owed`.
   - Pinned in Task 8 and Task 11.
5. **A quote inside the tax window.**
   - Expected: the tax is computed at the latest indexed block time, matches every
     `curve-v2.json` case, and the quote returns `snipeTax`, `taxBps` and `taxEndsAt`.
   - Pinned in Task 2 and Task 9.

---

### Task 1: Sync v2 ABIs, fixtures and curve vectors into the backend

**Files:**
- Modify: `backend/cmd/sync-event-abis/main.go` and `backend/cmd/sync-curve-vectors/main.go`.
- Modify: `backend/Taskfile.yml`, only if a new check name is needed.
- Create, by copy: `backend/internal/chain/abi/v2/*.json`,
  `backend/internal/chain/testdata/v2/event-logs-v2.json` and
  `backend/internal/curve/testdata/curve-v2.json`.

**Decisions:**
- Each sync command walks a list of `(source, destination)` pairs, and v2 adds its own pairs.
- `-check` fails on any byte drift or extra file, as it does for v1.
- Fixtures go to a `v2/` subdirectory, so the v1 fixture test globs stay unchanged.

**Steps:**
- [ ] **Red:** extend the existing sync tests with v2 pairs, then run
  `go test ./cmd/sync-event-abis/ ./cmd/sync-curve-vectors/`. Expected: FAIL, because the
  destinations are missing.
- [ ] **Green:**
  - Add the pairs.
  - Run `go run ./cmd/sync-event-abis` and `go run ./cmd/sync-curve-vectors`.
  - Re-run the tests. Expected: PASS.
- [ ] **Verify:** `task verify`. Expected: the sync `-check` steps are green.
- [ ] **Commit:** `chore(backend): sync v2 ABIs, event fixtures and curve vectors`.

### Task 2: Anti-snipe tax in the Go curve mirror

**Files:**
- Modify: `backend/internal/curve/math.go` and `backend/internal/curve/vectors.go`.
- Test: `backend/internal/curve/math_test.go` and
  `backend/internal/curve/vectors_mirror_test.go`.

**Interfaces:**
- `type AntiSnipe struct { StartBPS uint16; DurationSeconds uint32 }`.
- `func TaxBPS(profile AntiSnipe, launchTime, now uint64) uint16`. This mirrors
  `AntiSnipe.taxBps` exactly, including its integer rounding.
- `func BuyV2(state State, params Parameters, profile AntiSnipe, launchTime, now uint64,
  ethIn *big.Int) (BuyQuoteV2, error)`.
  - `BuyQuoteV2` embeds the v1 `BuyQuote` result and adds `SnipeTax()`, `TaxBPS()` and
    `TaxEndsAt()`.
  - The tax is taken only on the ETH that is actually consumed. A refund is never taxed.
- Sells are untaxed, so `Sell` is reused unchanged.

**Steps:**
- [ ] **Red:** write unit tests for these cases:
  - `t = launchTime` gives the full `startBps`;
  - `t = launchTime + duration` gives 0;
  - `duration = 0` gives 0;
  - a final buy inside the window gives a tax on the consumed ETH only.

  Add the `curve-v2.json` mirror test, which runs every case. Expected: FAIL, because the
  functions are undefined.
- [ ] **Green:** implement the functions, stdlib-only, with a doc comment naming the Solidity
  functions they mirror. Expected: PASS on every vector case.
- [ ] **Verify:** `task lint`. Expected: depguard stays clean.
- [ ] **Commit:** `feat(curve): mirror the v2 anti-snipe tax`.

### Task 3: Engine-2 deployment manifests and the one-deployment-per-chain guard

**Files:**
- Modify: `backend/deployments/model.go`, `registry.go` and `registry_test.go`.
- Modify: `backend/internal/indexer/engine.go`, at the startup state read.
- Modify: `backend/internal/store/postgres/queries/sync.sql`.
- Test: `backend/internal/indexer/engine_test.go` and the store integration test.

**Decisions:**
- **Schema routing.** `supportedEngines` becomes `{1, 2}`. A manifest is validated against
  `deployment-v2.schema.json` when its `engineVersion` is 2. To read the version, the
  registry peeks at that single field first, before strict decoding.
- **Model.** `Deployment` gains an engine-specific section: `V4 *V4Deployment {PoolManager,
  PositionManager, Permit2, Hook, Locker}`. The section stays nil for v1. The existing v1
  fields become meaningful only when `EngineVersion == 1`. Every v1 caller keeps compiling
  because the existing fields stay in place.
- **B2 guard.** At startup the indexer runs `SELECT deployment_id FROM sync_state WHERE
  chain_id = $1 AND deployment_id <> $2`. Any row means a fatal `ErrForeignDeployment`,
  raised before any write.

**Steps:**
- [ ] **Red:**
  - Add registry tests:
    - a valid engine-2 manifest loads;
    - an engine-2 manifest carrying `uniswapV2Factory` is rejected;
    - an engine-1 manifest is still validated by the v1 schema.
  - Add an engine test: a seeded `sync_state` row for another deployment on the same chain
    stops startup.
  - Expected: FAIL.
- [ ] **Green:** implement the routing, the model and the guard. Expected: PASS.
- [ ] **Verify:** `go test ./deployments/ ./internal/indexer/` and `go run
  ./cmd/check-deployments`.
- [ ] **Commit:** `feat(deployments): load engine-2 manifests, one deployment per chain`.

### Task 4: v2 event decoder

**Files:**
- Modify: `backend/internal/chain/decoder.go` and `types.go`.
- Test: `backend/internal/chain/decoder_test.go`.

**Decisions:**
- **Selecting the registry.** `NewDecoder(engineVersion)` selects the registry. Version 2
  embeds `abi/v2/*.json`. Topic ownership, as confirmed against the `emitterKind` of every
  log in `fixtures/v2`, is:
  - factory: `TokenLaunched`, `LaunchFeesClaimed`, the pause events and the config events;
  - curve: `Trade`, `Graduated`, the creator and protocol claims, the refunds,
    `SnipeTaxClaimed`, `FeeRecipientProposed` and `FeeRecipientAccepted`;
  - token: `Transfer`;
  - locker: `LpFeesCollected` and `LpFeesClaimed`;
  - poolManager: `Initialize`, `ModifyLiquidity` and `Swap`.
- **Typed values.** New types are `TokenLaunchedV2`, `TradeV2`, `GraduatedV2`,
  `SnipeTaxClaimed`, `FeeRecipientProposed`, `FeeRecipientAccepted`, `LpFeesCollected`,
  `LpFeesClaimed`, `V4Initialize`, `V4ModifyLiquidity` and `V4Swap`. The unchanged v1
  types are reused.
- **Signed values.** `V4Swap.Amount0` and `Amount1` are signed `*big.Int`. `Tick` is
  `int32`. `PoolID` is `common.Hash`. `TxFrom` is filled by the engine, as `PoolSwap.TxFrom`
  is today.
- **Emitters.** `Emitters` gains `PoolManager common.Address`, `Locker common.Address` and
  `Pools map[common.Hash]common.Address` (poolId → token). A `PoolManager` log whose `id`
  topic is not in `Pools` is a discovery bug: the decoder returns an `EmitterError`.

**Steps:**
- [ ] **Red:** add a decoder test that decodes every log in `testdata/v2/event-logs-v2.json`
  under its `emitterKind`. It asserts:
  - each log yields the expected typed value;
  - all 22 topics are covered.

  The fixture records only raw logs and no decoded values. Field-level assertions therefore
  check invariants that hold for this lifecycle, for example:
  - `Trade.ethGross` equals the sum of its parts;
  - the `Graduated` poolId equals the `Initialize` id in the same transaction.

  Exact on-chain values are checked by the Task 11 e2e. Add these negative tests:
  - a v1 `TokenLaunched` given to `NewDecoder(2)`;
  - an unknown poolId;
  - a non-padded indexed address.

  Expected: FAIL.
- [ ] **Green:** implement. Expected: PASS. The existing v1 decoder tests stay unchanged and
  green.
- [ ] **Commit:** `feat(chain): decode engine v2 and Uniswap V4 pool events`.

### Task 5: v2 ledger schema, sqlc queries and reorg coverage

**Files:**
- Create: `backend/internal/store/postgres/migrations/00024_engine_v2_ledger.sql`.
- Modify: `queries/events.sql`, `queries/reorg.sql` and `queries/indexer.sql`.
- Regenerate: `sqlc/`.
- Test: the store integration tests (migrations up/down/up are already part of
  `task verify`).

**Decisions:**
- **`token_launches` and `tokens`:**
  - `lp_pair` and `weth` become nullable, under
    `CHECK ((engine_version = 1) = (lp_pair IS NOT NULL AND weth IS NOT NULL))`;
  - `token_is_token0` becomes `GENERATED ... AS (COALESCE(token_address < weth, FALSE))`,
    because native ETH is always `currency0`;
  - new nullable v2 columns: `hook`, `locker`, `anti_snipe_start_bps`,
    `anti_snipe_duration_seconds` and `lp_creator_share_bps`, with a CHECK that they are
    set exactly when `engine_version = 2`.
- **`trades`:** gains `snipe_tax NUMERIC(78,0) NOT NULL DEFAULT 0`. A v1 trade is always 0.
- **`graduations`:**
  - `lp_pair` becomes nullable;
  - new v2 columns: `pool_id`, `position_token_id`, `liquidity`, `sqrt_price_x96`,
    `eth_remainder` and `token_remainder`;
  - an engine CHECK through the token's launch row cannot be expressed as a table CHECK, so
    the ingest query enforces it.
- **New ledger tables.** Each follows the existing coordinate, FK and immutability pattern:
  - `snipe_tax_claims`;
  - `fee_recipient_proposals`;
  - `fee_recipient_acceptances`;
  - `lp_fee_collections`;
  - `lp_fee_claims`;
  - `v4_pool_initializations`;
  - `v4_liquidity_changes`;
  - `v4_swaps`, which stores signed `amount0` and `amount1`, `sqrt_price_x96`, `liquidity`,
    `tick`, `fee` and a nullable `tx_from`.
- **Pool registry `v4_pools (chain_id, pool_id) → token_address, position_token_id, hook`.**
  It is a projection, written from `graduations`. Rebuilding it on reorg uses the existing
  per-token rebuild path, so it needs no delete of its own.
- **Reorg.** `reorg.sql` gains one `DELETE … WHERE chain_id=$1 AND block_number>$2` per new
  ledger table, in FK-safe order.
- **Down migration.** It restores v1's `NOT NULL` columns only when no v2 rows exist. If v2
  rows exist, it fails loudly instead of silently dropping data.

**Steps:**
- [ ] **Red:** write integration tests:
  - inserting a v2 launch with a NULL pair succeeds;
  - inserting a v1 launch with a NULL pair is rejected by the CHECK;
  - a reorg delete removes every new table's rows above the ancestor.

  Expected: FAIL, because the tables are missing.
- [ ] **Green:** write the migration and queries, then run `task sqlc`. Expected: PASS.
- [ ] **Verify:** `task verify` (migrations up/down/up and the sqlc diff).
- [ ] **Commit:** `feat(store): add engine v2 ledger and V4 pool tables`.

### Task 6: v2 discovery and launch verification

**Files:**
- Modify: `backend/internal/chain/discovery.go` and `verify.go`.
- Test: `backend/internal/chain/discovery_test.go` and `verify_test.go`.

**Decisions:**
- **Launch verification.** For engine 2, `VerifyLaunchPair` is replaced by
  `VerifyLaunchV2`:
  - `TokenLaunchedV2.hook == manifest.hook`;
  - `TokenLaunchedV2.locker == manifest.locker`;
  - the anti-snipe and LP-split fields are within the factory caps.

  A mismatch fails closed.
- **Graduation check.** When `Graduated` is decoded, discovery recomputes
  `poolId = keccak256(abi.encode(0x0, token, 10000, 200, hook))` and requires it to equal
  the log's `poolId`. Only then is the pool added to `Emitters.Pools`.
- **Fetching.**
  - Locker logs: address-filtered on `manifest.locker`, because the locker emits for every
    v2 token.
  - `PoolManager` logs: fetched with `address = PoolManager` and `topics = [[Swap,
    ModifyLiquidity, Initialize], [poolIds…]]`. The poolIds are batched by the existing
    `addressBatchSize`.
  - Same-block registration: a pool registered at block B is refetched from B to `to`, as
    `newByBlock` does for pairs. Logs from before the `Graduated` log in block B come from
    the curve's own initialize and mint, and are kept.

**Steps:**
- [ ] **Red:** write fake-log-source tests for:
  - (a) a swap in the graduation block after `Graduated`, which must be included;
  - (b) a swap with the same token and a foreign hook, whose poolId differs and must be
    excluded;
  - (c) a `Graduated` whose `poolId` does not match the derived key, which must fail;
  - (d) a launch with a hook other than the manifest's, which must fail;
  - (e) more than one batch of poolIds.

  Expected: FAIL.
- [ ] **Green:** implement. Expected: PASS. v1 discovery tests stay unchanged.
- [ ] **Commit:** `feat(chain): discover v2 launches and V4 pool logs by poolId`.

### Task 7: Route and ingest v2 events

**Files:**
- Modify: `backend/internal/indexer/router.go`, `engine.go` and `ports.go`.
- Modify: `backend/internal/ledger/events.go` and the postgres sink.
- Test: `backend/internal/indexer/engine_test.go`.

**Decisions:**
- **Sink.** `EventSink` gains one `Ingest…` per new ledger table. Ingests are idempotent with
  an identity check, as v1 ingests are.
- **Router checks.** The router's pre-checks extend to v2:
  - no curve `Trade` after `Graduated`;
  - no `V4Swap` for a poolId before its `Graduated` log index.
- **Sender attribution.** `V4Swap.TxFrom` uses the existing `SenderSource` path and the miss
  counter. `Swap.sender` is the router, so it is stored but never used as the trader
  (pre-flight R4).

**Steps:**
- [ ] **Red:** write engine tests that feed the decoded v2 fixture through a fake store and
  assert one ingest per log, with the right types and coordinates. Expected: FAIL.
- [ ] **Green:** implement. Expected: PASS.
- [ ] **Commit:** `feat(indexer): ingest engine v2 and V4 pool events`.

### Task 8: Engine-aware projections

**Files:**
- Create: `backend/internal/store/postgres/migrations/00025_engine_v2_projections.sql`.
- Modify: `queries/projections.sql` and `queries/read.sql`.
- Test: the store integration tests for each projection.

**Decisions:**
- **`rebuild_token_projections`.** It copies the v2 launch columns into `tokens`. For a
  graduated v2 token it also upserts `v4_pools`.
- **Price.**
  - `token_reserves` gains `reserve_source = 'v4'` and a nullable `sqrt_price_x96`.
  - For `'v4'` rows, `eth_reserve` and `token_reserve` are NULL, under a CHECK.
  - The spot price is `1e18 · 2^192 / sqrtPriceX96²` (token is `currency1`), computed in
    `NUMERIC` and taken from the latest `v4_swaps` row, or from `graduations` before the
    first swap.
  - **Liquidity** for a graduated v2 token is reported as NULL rather than full-range
    math (pre-flight R5). The API exposes the locker position separately (Task 10).
- **`market_trades`.** It gains a third `UNION ALL` arm from `v4_swaps` joined to `v4_pools`:
  - `side_buy`: `amount0 < 0`, meaning the swapper paid ETH;
  - `gross_eth_volume`: `|amount0|`, which is gross of the LP fee, like v1 `ethGross`;
  - `token_volume`: `|amount1|`;
  - `spot_price_wad`: from the swap's own `sqrt_price_x96`.

  These sign conventions are hypotheses until the fixture swaps confirm them (pre-flight
  R3). If a fixture disagrees, fix the convention to match the fixture.
- **Holder exclusions.**
  - For v2 tokens, the curve, the `PoolManager` and the locker are excluded.
  - `0x…dEaD` is excluded only for v1, because the v2 LP NFT is locked, not burned.
  - The `lp_pair` exclusions in `projections.sql` and `read.sql` become
    `engine_version`-aware.
- **Creator fee ledger.** This is a new projection, `creator_fee_balances (chain_id,
  token_address, account)`. It replays the following in `(block, tx_index, log_index)`
  order across the curve and locker emitters:
  - `Trade.creatorFee`;
  - the locker creator share `floor(amount × lpCreatorShareBps / 10000)` of each
    `LpFeesCollected` (ETH and token);
  - the claims.

  Each credit goes to the recipient in force at its log, starting from the creator and
  updated at each `FeeRecipientAccepted`.
- **Protocol revenue.** It adds `snipe_tax` and the graduation `eth_remainder`.
  `SnipeTaxClaimed` is its own claim stream.

**Steps:**
- [ ] **Red:** write integration tests seeded from the ingested v2 fixture:
  - price after the last V4 swap equals the fixture `sqrtPriceX96` price;
  - `market_trades` has curve and dex rows in order, with signed volumes positive;
  - holder top-N excludes the `PoolManager`, the locker and the curve;
  - creator fee balances equal the values computed by hand from the fixture's logs. The
    fixture has no on-chain `owed` or `creatorFeesOwed` snapshot, so equality with the
    on-chain views is asserted in the Task 11 e2e.

  Expected: FAIL.
- [ ] **Green:** implement. Expected: PASS. Every v1 projection test is unchanged and green.
- [ ] **Commit:** `feat(store): engine-aware projections for v2 tokens`.

### Task 9: Quote with the anti-snipe tax

**Files:**
- Modify: `backend/internal/quote/service.go`, `backend/internal/token` (quote state reader)
  and the API handler.
- Modify: `backend/openapi/*.yaml`.
- Test: `backend/internal/quote/service_test.go` and the OpenAPI check.

**Decisions:**
- **Clock (B5).** The clock is the latest indexed block's `block_time`. It is already read
  for `AsOfBlock`, so no extra query is needed.
- **Response fields.** `Result` gains `SnipeTax`, `TaxBPS` and `TaxEndsAt`. All three are
  zero or empty for v1 and after the window closes.
- **Graduated v2 tokens.** The quote returns the existing "graduated" error. Quoting a V4
  pool is the web's concern, through the Uniswap quoter, so it stays out of the backend.

**Steps:**
- [ ] **Red:** write service tests:
  - for each `curve-v2.json` buy case, the quote equals the vector;
  - a v1 token returns zero tax.

  Add an OpenAPI drift test. Expected: FAIL.
- [ ] **Green:** implement and update the OpenAPI document. Expected: PASS, and
  `cmd/check-openapi` is green.
- [ ] **Commit:** `feat(quote): include the anti-snipe tax at the indexed head time`.

### Task 10: API surface for v2 token facts

**Files:**
- Modify: the token detail and creator read paths in `internal/token`, `internal/apiserver`
  and `openapi/`.
- Test: the API handler tests and the OpenAPI check.

**Decisions:**
- **Engine field.** Token detail gains `engineVersion`, which is always present.
- **v2 object.** Token detail also gains a `v2` object that is null for v1. It contains:
  - `antiSnipe {startBps, durationSeconds, endsAt}`;
  - `lpCreatorShareBps`;
  - `pool {poolId, positionTokenId, hook}`, null before graduation;
  - `feeRecipient` and `pendingFeeRecipient`;
  - `lockerOwed {eth, token}` per account, for the creator surface.
- **Unchanged v1 shape.** v1 responses are byte-identical except for the added fields; this
  is checked by the existing golden responses.

**Steps:**
- [ ] **Red:** write handler tests for a v1 token and for v2 tokens before and after
  graduation. Expected: FAIL.
- [ ] **Green:** implement. Expected: PASS.
- [ ] **Commit:** `feat(api): expose v2 launch, pool and fee recipient facts`.

### Task 11: Anvil v2 e2e and reorg acceptance

**Files:**
- Create: `backend/scripts/run-anvil-v2-indexer-e2e.ps1`. It wraps
  `contracts/scripts/deploy-v2-local.ps1` and drives the lifecycle with `cast`.
- Create: `backend/internal/indexer/anvil_v2_e2e_integration_test.go`.
- Modify: `backend/Taskfile.yml`, to add `anvil-v2-indexer-e2e` to `verify`.

**Lifecycle**, asserted through the API (the pre-flight acceptance list):
1. launch with the dev buy;
2. a taxed buy, then an untaxed buy after the window;
3. graduation;
4. V4 swaps both ways through the local `PoolSwapTest`;
5. `collect`, then `claim`;
6. a recipient propose and accept;
7. claims after the change.

**Assertions:** every projection equals the corresponding on-chain view: `owed`,
`creatorFeesOwed`, the pool `slot0` price and the holder balances.

**Reorg test:**
- Snapshot Anvil before graduation, graduate, index, revert to the snapshot, and mine a
  different block.
- Assert the indexer records the reorg and rolls back the pool registry, the V4 rows and the
  phase.
- Assert that re-indexing the replayed lifecycle yields identical projections.

**Steps:**
- [ ] **Red:** write the e2e test. Expected: FAIL until Tasks 1–10 are in place, then run it.
- [ ] **Green:** fix whatever the e2e exposes, inside the task that owns it, as a follow-up
  commit naming that task.
- [ ] **Verify:** run the full `task verify`, including both Anvil e2e runs.
- [ ] **Commit:** `test(indexer): Anvil v2 lifecycle and reorg acceptance`.

## Acceptance (from the pre-flight)

- `task verify` is green and includes the v2 sync checks, the Go tax mirror on every v2
  vector, and the v2 decoder on every fixture.
- Startup fails closed on a second deployment for the same chain.
- The Anvil v2 e2e and the graduation-crossing reorg test pass.
- The quote returns `snipeTax`, `taxBps` and `taxEndsAt` at the latest indexed block time.
- Holder views exclude the `PoolManager`, the locker and the curve for v2 tokens.
