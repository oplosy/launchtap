# P0-C3 — Watchlist counts and 7d/30d leaderboards

Status: Claude design, 2026-11-10. Scope chosen by the product owner ("both watchlist counts and
7d/30d leaderboards"); the owner delegated the remaining design rulings while away. Earlier
parts: C1 `docs/specs/2026-11-06-p0c1-wallet-page-pnl-design.md` (all-time leaderboard, position
projection) and C2 `docs/specs/2026-11-09-p0c2-comments-usernames-design.md`.

## Goal

- A token page shows how many signed-in users watch the token.
- `/leaderboard` ranks wallets by realized PnL over the last 7 days, the last 30 days, or all
  time.

Success means the windowed numbers equal a recomputation from the canonical ledger, survive
reorgs exactly like the all-time number, and the watch count changes when a user toggles the
token.

## Non-goals

- A "most watched" sort or a watch count on list rows (one extra aggregate per row; later).
- Unrealized PnL in windows, volume or ROI leaderboards, per-token leaderboards.
- Any new service, datastore or contract change.

## Decisions

| Decision | Rationale | Rejected alternative |
| --- | --- | --- |
| New projection table `wallet_realized_legs (chain_id, wallet, token_address, block_number, transaction_index, block_time, realized_pnl)` holding one row per leg whose realized PnL change is non-zero. The position worker replaces a pair's rows in the same transaction that upserts its `wallet_positions` row. | Reuses the C1 recompute-from-ledger path, so reorgs stay a re-mark, not a rollback. The sum of a pair's rows equals its all-time realized PnL, which a test pins. | Daily buckets (calendar-day windows, not "last 7 days"); a separate windowed worker (second copy of the cost-basis logic). |
| `position.Compute` also returns the realized PnL change of each leg (`Position.LegRealized`, aligned with the input legs). | One cost-basis implementation feeds both the total and the window. | Recomputing deltas in SQL. |
| Windows are rolling: `block_time >= db_now - 7 days` (or 30). The window query sums the legs per wallet on read, ordered by the sum, limited to 100, with the same exclusions as the all-time board (zero address, curves, pairs) and positive sums only. Index `(chain_id, block_time)`. | Exact "last N days" semantics; the read scans only the window. The page is revalidated every 60 s, so read cost stays bounded. | A materialised windowed table refreshed on a timer (staleness plus another job). |
| `GET /v1/leaderboard?window=all\|7d\|30d`, default `all`; any other value is a 400 `invalid_window`. Item shape unchanged (`realized_pnl_wei` is the window's sum; `tokens_traded` is the count of distinct tokens with a realized leg in the window, all-time count for `all`). | One endpoint, one client type, backward compatible. | Three endpoints. |
| `GET /v1/tokens/{address}/watchers` returns `{ "watch_count": n }` for the configured deployment, public, `Cache-Control: public, max-age=15`. Counts rows in `watchlist_entries`; new index `(chain_id, deployment_id, token_address)`. Unknown or reorged tokens return their count (normally 0) rather than 404: the count is about the watchlist, not the token row. | Keeps the snapshot-stamped token detail read free of app-state and its caching untouched. | A field on the token detail DTO (mixes indexer snapshot with app-state, forces a token-detail cache change). |
| The web token page fetches the count after load and adjusts it locally by ±1 when the signed-in user toggles the watch star, then refetches on the next page load only. | Instant feedback without a write-path change. | Live SSE watch-count hints (no product value at this scale). |
| Upgrade: migration `00021_windowed_leaderboard` creates the table empty. Operators run the existing `backfill-swap-sender -seed-positions` (marks every pair dirty) once after deploy; until then windowed boards are incomplete. | Same operational path as C1. | Backfilling inside the migration (long lock on a big DB). |

## RISKS

- Sybil inflation of watch counts: anyone can create Privy accounts. The count is a soft signal,
  labelled "watching", and capped watchlists (existing per-user cap) limit cost. Accepted.
- Window reads scan all legs in the window. At current volumes this is cheap; if a 30-day scan
  gets slow, add a daily rollup. Recorded in the runbook as a scaling note.
- A reorg moves a leg's `block_time`; the recompute replaces rows, so the window is correct after
  the position worker catches up (seconds), like the all-time board.

## ACCEPTANCE CRITERIA

1. `position.Compute` returns `LegRealized` whose sum equals `RealizedPnL` for every vector.
2. Recomputing a pair writes exactly its non-zero legs; recomputing to no legs deletes them.
3. `window=7d` excludes a sell older than 7 days by DB clock and includes one inside; ranks,
   exclusions and the positive-only rule match the all-time board; `window=bad` → 400
   `invalid_window`.
4. Watch count reflects inserts and deletes for the token in this deployment only (another
   deployment's or chain's rows are not counted); invalid address → 422 like other token routes.
5. Web: the leaderboard has All / 7d / 30d tabs whose state lives in the URL (`?window=`); the
   token page shows "N watching" and adjusts on toggle.
6. Gates: backend unit/race, integration, sqlc/openapi/migration checks; web vitest, lint,
   typecheck, live e2e; the Anvil gates stay green.
