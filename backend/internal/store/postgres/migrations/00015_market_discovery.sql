-- +goose Up
-- Trending and dirty-window scans filter trades and DEX swaps by block_time range.
CREATE INDEX trades_chain_block_time_idx ON trades (chain_id, block_time);
CREATE INDEX pool_swaps_chain_block_time_idx ON pool_swaps (chain_id, block_time);

-- Per-user token watchlist. token_address has no FK on purpose: an entry may outlive its token
-- row (reorg) and is returned as a nullable row by the list query.
CREATE TABLE watchlist_entries (
    chain_id BIGINT NOT NULL,
    deployment_id TEXT NOT NULL,
    privy_did TEXT NOT NULL,
    token_address BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT watchlist_entries_pkey PRIMARY KEY (chain_id, deployment_id, privy_did, token_address),
    CONSTRAINT watchlist_entries_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT watchlist_entries_deployment_id_length CHECK (octet_length(deployment_id) > 0),
    CONSTRAINT watchlist_entries_privy_did_length CHECK (octet_length(privy_did) > 0),
    CONSTRAINT watchlist_entries_token_address_length CHECK (octet_length(token_address) = 20)
);

CREATE INDEX watchlist_entries_owner_created_idx
    ON watchlist_entries (chain_id, deployment_id, privy_did, created_at DESC);

-- +goose Down
-- Down deletes every user's watchlist entries.
DROP TABLE watchlist_entries;
DROP INDEX pool_swaps_chain_block_time_idx;
DROP INDEX trades_chain_block_time_idx;
