# Pre-flight: leaderboard ignores positions funded by transfer (AUDIT_REPORT_TR HIGH-08)

Date: 2026-11-16. Owner decision: 2026-11-16 (the recommended option below).

## Confirmed defect

`docs/specs/2026-11-06-p0c1-wallet-page-pnl-design.md` gives tokens received by transfer a zero
cost basis. A wallet that buys, transfers the tokens to a second wallet and sells them there
makes the second wallet realize the whole sale as profit, so anyone can rank on the all-time,
7d and 30d leaderboards for the cost of fees.

## Locked decisions

- **The leaderboards rank only positions that never received tokens by transfer**
  (`wallet_positions.transferred_in = 0`). The wallet page, its totals and the per-position PnL
  are unchanged: zero cost basis stays the documented rule there. Rationale: the leaderboard is
  the only surface where inflated PnL buys something (rank), and excluding the position closes the
  loop without guessing a cost basis. Alternative rejected: carrying the sender's cost basis to the
  receiver. It is more accurate but rewrites the PnL model (cross-wallet cost lookups on every
  transfer, reorg handling of the carried basis) for one ranking surface.
- **All-time board:** `wallet_totals.leaderboard_realized_pnl` (migration 00035) is maintained by
  `RecomputeWalletTotals` next to `realized_pnl`, and `wallet_totals_leaderboard_idx` moves to it so
  the board still walks an index. The migration backfills it from `wallet_positions`.
- **Windowed boards:** `ListLeaderboardWindow` and `InsertLeaderboardWindowRanks` skip realized
  legs whose position has `transferred_in > 0`.
- `tokens_traded` on the all-time board stays the wallet's full count; it is a fact about the
  wallet, not part of the ranking.

## Known limit

A position that received any transfer is left off the board entirely, even if it was also funded
by real buys. Airdrops or gifts to an active trader therefore remove that token from their rank.
This is the conservative side of the trade-off and can be revisited with a cost-basis model.

## Tests

`TestLeaderboardIgnoresPositionsFundedByTransfer` drives the real ledger and position worker
through a buy, transfer, sell loop and checks the wallet page keeps the PnL while both boards rank
only the honest trader. The migration chain tests cover 00035 up, down and up.
