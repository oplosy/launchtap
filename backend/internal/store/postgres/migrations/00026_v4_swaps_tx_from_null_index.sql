-- +goose Up
-- The sender backfill counts and walks NULL-sender swaps on every scheduler pass. The v1 side is
-- served by pool_swaps_tx_from_null_idx; this is the same partial index for V4 swaps, so the pass
-- reads only the still-unattributed rows instead of scanning v4_swaps.
CREATE INDEX v4_swaps_tx_from_null_idx ON v4_swaps (chain_id, block_number, tx_hash) WHERE tx_from IS NULL;

-- +goose Down
DROP INDEX IF EXISTS v4_swaps_tx_from_null_idx;
