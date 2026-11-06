-- +goose Up
-- The live feed tails new trades across all tokens, ordered by chain position.
CREATE INDEX trades_chain_order_idx ON trades (chain_id, block_number, transaction_index, log_index);
CREATE INDEX pool_swaps_chain_order_idx ON pool_swaps (chain_id, block_number, transaction_index, log_index);

-- +goose Down
DROP INDEX IF EXISTS pool_swaps_chain_order_idx;
DROP INDEX IF EXISTS trades_chain_order_idx;
