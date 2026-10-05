-- +goose Up
-- Holder pages read the largest positive balances of one token first.
CREATE INDEX holder_balances_token_balance_idx
    ON holder_balances (chain_id, token_address, balance DESC, holder_address DESC)
    WHERE balance > 0;
-- DEX trade pages read one pair's swaps newest first through market_trades.
CREATE INDEX pool_swaps_pair_order_idx
    ON pool_swaps (chain_id, pair_address, block_number DESC, transaction_index DESC, log_index DESC);

-- +goose Down
DROP INDEX pool_swaps_pair_order_idx;
DROP INDEX holder_balances_token_balance_idx;
