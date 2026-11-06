-- +goose Up
-- The wallet position exclusion (R4) looks up curve_address on every ingested trade, swap and transfer.
CREATE INDEX tokens_curve_address_idx ON tokens (chain_id, curve_address);

-- +goose Down
DROP INDEX IF EXISTS tokens_curve_address_idx;
