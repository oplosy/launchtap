-- +goose Up
-- The position worker's claim (ClaimPositionDirty) filters by chain_id and orders by
-- (generation, chain_id, wallet, token_address); with chain_id fixed this index returns rows in that
-- order, so a claim reads only the head of the queue instead of sorting all of it.
CREATE INDEX position_dirty_claim_idx ON position_dirty (chain_id, generation, wallet, token_address);

-- +goose Down
DROP INDEX IF EXISTS position_dirty_claim_idx;
