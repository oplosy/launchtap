-- +goose Up
-- The live feed reads each trade source in chain order (ListMarketTradesAfter,
-- ListRecentMarketTrades, GetNewestFeedCursors). Curve trades and V2 swaps have this index
-- since 00016; V4 swaps join those reads now and need the same key.
CREATE INDEX v4_swaps_chain_order_idx ON v4_swaps (chain_id, block_number, transaction_index, log_index);

-- +goose Down
DROP INDEX IF EXISTS v4_swaps_chain_order_idx;
