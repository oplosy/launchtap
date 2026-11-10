-- +goose Up
-- Inbox source events (spec P0-C4): graduations and market trades of at least 0.5 ETH gross
-- (web/src/live/notification-rules.ts LARGE_TRADE_WEI mirrors the threshold). Derived from the
-- canonical ledger, so a reorged event disappears from every inbox on the next read.
CREATE VIEW inbox_events AS
SELECT g.chain_id, g.token_address, 'graduation'::TEXT AS kind, g.block_number, g.transaction_index,
       g.log_index, g.block_time, g.tx_hash, g.eth_to_pool AS eth_wei,
       NULL::BOOLEAN AS side_buy, NULL::BYTEA AS trader, NULL::TEXT AS source
FROM graduations AS g
UNION ALL
SELECT t.chain_id, t.token_address, 'large_trade'::TEXT, t.block_number, t.transaction_index,
       t.log_index, t.block_time, t.tx_hash, t.gross_eth_volume, t.side_buy, t.trader, t.source
FROM market_trades AS t
WHERE t.gross_eth_volume >= 500000000000000000;

-- One forward-only read position per account (chain, deployment, Privy DID).
CREATE TABLE notification_reads (
    chain_id BIGINT NOT NULL,
    deployment_id TEXT NOT NULL,
    privy_did TEXT NOT NULL,
    read_block BIGINT NOT NULL,
    read_tx_index INTEGER NOT NULL,
    read_log_index INTEGER NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT notification_reads_pkey PRIMARY KEY (chain_id, deployment_id, privy_did),
    CONSTRAINT notification_reads_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT notification_reads_position_nonnegative CHECK (read_block >= 0 AND read_tx_index >= 0 AND read_log_index >= 0)
);

-- +goose Down
DROP TABLE IF EXISTS notification_reads;
DROP VIEW IF EXISTS inbox_events;
