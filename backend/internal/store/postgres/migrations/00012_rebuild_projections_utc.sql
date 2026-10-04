-- +goose Up
-- rebuild_token_projections buckets candles with date_trunc on TIMESTAMPTZ, which follows
-- the session TimeZone. Pin it to UTC so reorg rebuilds produce the same buckets as the
-- UTC-explicit incremental ApplyMarketTradeCandles query on any server configuration.
ALTER FUNCTION rebuild_token_projections(BIGINT, BYTEA) SET timezone = 'UTC';

-- +goose Down
ALTER FUNCTION rebuild_token_projections(BIGINT, BYTEA) RESET timezone;
