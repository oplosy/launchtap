-- +goose Up
-- Wallet trade pages read each source by trader newest first (WalletTradePageFloor). Curve
-- trades and V2 swaps have this index since 00017; V4 swaps had only the NULL-sender backfill
-- index (00026), so a wallet page scanned every V4 swap of the chain.
CREATE INDEX v4_swaps_tx_from_order_idx
    ON v4_swaps (chain_id, tx_from, block_number DESC, transaction_index DESC, log_index DESC);

-- +goose Down
DROP INDEX IF EXISTS v4_swaps_tx_from_order_idx;
