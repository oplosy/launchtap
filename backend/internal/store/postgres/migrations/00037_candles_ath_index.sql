-- +goose Up
-- RecomputeTokensStats reads a token's all-time high as the highest '1m' candle (ties: earliest
-- bucket). With only the primary key the planner fetched and sorted every '1m' candle of the
-- token on each recompute (59 ms at 50,000 candles, AUDIT_REPORT_TR HIGH-13); this index serves
-- the ORDER BY ... LIMIT 1 directly.
CREATE INDEX candles_1m_ath_idx
    ON candles (chain_id, token_address, high_price_wad DESC, bucket_start_time)
    WHERE interval = '1m';

-- +goose Down
DROP INDEX IF EXISTS candles_1m_ath_idx;
