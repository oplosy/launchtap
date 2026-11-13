-- +goose Up
-- Incremental protocol aggregates (docs/specs/2026-11-13-incremental-protocol-aggregates-preflight.md).
-- daily_through_block is the highest indexed block protocol_daily already reflects; NULL forces a
-- full rebuild. The stats worker then rebuilds only the UTC days of blocks above it.
ALTER TABLE protocol_stats ADD COLUMN daily_through_block BIGINT;
ALTER TABLE protocol_stats
    ADD CONSTRAINT protocol_stats_daily_through_block_nonnegative CHECK (daily_through_block >= 0);

-- The from-day rebuild and the 24h columns filter every market_trades branch, launches and
-- graduations by block_time; trades and pool_swaps have this index since 00015.
CREATE INDEX token_launches_chain_block_time_idx ON token_launches (chain_id, block_time);
CREATE INDEX graduations_chain_block_time_idx ON graduations (chain_id, block_time);
CREATE INDEX v4_swaps_chain_block_time_idx ON v4_swaps (chain_id, block_time);

-- +goose Down
DROP INDEX IF EXISTS v4_swaps_chain_block_time_idx;
DROP INDEX IF EXISTS graduations_chain_block_time_idx;
DROP INDEX IF EXISTS token_launches_chain_block_time_idx;
ALTER TABLE protocol_stats DROP CONSTRAINT protocol_stats_daily_through_block_nonnegative;
ALTER TABLE protocol_stats DROP COLUMN daily_through_block;
