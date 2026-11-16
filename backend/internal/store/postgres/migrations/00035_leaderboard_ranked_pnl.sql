-- +goose Up
-- The leaderboard ranks realized PnL only from positions that never received tokens by transfer
-- (docs/specs/2026-11-16-leaderboard-transfer-exclusion-preflight.md). Transfers in carry zero
-- cost, so a buy, transfer, sell loop across two wallets would otherwise rank the whole sale as
-- profit. wallet_totals.realized_pnl keeps every position for the wallet page; the leaderboard
-- reads this column and its index instead.
ALTER TABLE wallet_totals ADD COLUMN leaderboard_realized_pnl NUMERIC(79,0) NOT NULL DEFAULT 0;

UPDATE wallet_totals AS totals
SET leaderboard_realized_pnl = ranked.realized_pnl
FROM (
    SELECT p.chain_id, p.wallet, sum(p.realized_pnl) AS realized_pnl
    FROM wallet_positions AS p
    WHERE p.transferred_in = 0
    GROUP BY p.chain_id, p.wallet
) AS ranked
WHERE ranked.chain_id = totals.chain_id AND ranked.wallet = totals.wallet;

DROP INDEX wallet_totals_leaderboard_idx;
CREATE INDEX wallet_totals_leaderboard_idx ON wallet_totals (chain_id, leaderboard_realized_pnl DESC, wallet);

-- +goose Down
DROP INDEX wallet_totals_leaderboard_idx;
CREATE INDEX wallet_totals_leaderboard_idx ON wallet_totals (chain_id, realized_pnl DESC, wallet);
ALTER TABLE wallet_totals DROP COLUMN leaderboard_realized_pnl;
