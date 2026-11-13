-- +goose Up
-- Telegram alerts (docs/specs/2026-11-12-telegram-alerts-design.md). One linked private chat per
-- account and one account per chat, per chain and deployment. delivered_* is the forward-only
-- cursor over inbox_events positions; settings mirror internal/telegram.Settings bounds.
CREATE TABLE telegram_links (
    chain_id BIGINT NOT NULL,
    deployment_id TEXT NOT NULL,
    privy_did TEXT NOT NULL,
    chat_id BIGINT NOT NULL,
    graduations BOOLEAN NOT NULL DEFAULT true,
    large_trades BOOLEAN NOT NULL DEFAULT true,
    min_trade_wei NUMERIC(78, 0) NOT NULL DEFAULT 500000000000000000,
    delivered_block BIGINT NOT NULL,
    delivered_tx_index INTEGER NOT NULL,
    delivered_log_index INTEGER NOT NULL,
    linked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT telegram_links_pkey PRIMARY KEY (chain_id, deployment_id, privy_did),
    CONSTRAINT telegram_links_chat_unique UNIQUE (chain_id, deployment_id, chat_id),
    CONSTRAINT telegram_links_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT telegram_links_chat_id_positive CHECK (chat_id > 0),
    CONSTRAINT telegram_links_min_trade_range CHECK (
        min_trade_wei >= 500000000000000000 AND min_trade_wei <= 10000000000000000000000
    )
);

-- At most one pending link code per account; only the SHA-256 of the code is stored.
CREATE TABLE telegram_link_codes (
    chain_id BIGINT NOT NULL,
    deployment_id TEXT NOT NULL,
    privy_did TEXT NOT NULL,
    code_sha256 BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT telegram_link_codes_pkey PRIMARY KEY (chain_id, deployment_id, privy_did),
    CONSTRAINT telegram_link_codes_code_unique UNIQUE (code_sha256),
    CONSTRAINT telegram_link_codes_code_length CHECK (octet_length(code_sha256) = 32)
);

-- +goose Down
DROP TABLE IF EXISTS telegram_link_codes;
DROP TABLE IF EXISTS telegram_links;
