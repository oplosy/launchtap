-- +goose Up
-- One row per (wallet, token) leg whose realized PnL change is non-zero, written by the position
-- worker in the same transaction as the pair's wallet_positions row and replaced on every
-- recompute, so a pair's rows always sum to its wallet_positions.realized_pnl. Rolling 7d/30d
-- leaderboards sum the rows whose block_time is inside the window. No FK to tokens, like
-- wallet_positions: a reorged pair is recomputed to no rows.
CREATE TABLE wallet_realized_legs (
    chain_id BIGINT NOT NULL,
    wallet BYTEA NOT NULL,
    token_address BYTEA NOT NULL,
    block_number BIGINT NOT NULL,
    transaction_index INTEGER NOT NULL,
    block_time TIMESTAMPTZ NOT NULL,
    realized_pnl NUMERIC(79,0) NOT NULL,
    CONSTRAINT wallet_realized_legs_pkey PRIMARY KEY (chain_id, wallet, token_address, block_number, transaction_index),
    CONSTRAINT wallet_realized_legs_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT wallet_realized_legs_wallet_length CHECK (octet_length(wallet) = 20),
    CONSTRAINT wallet_realized_legs_token_address_length CHECK (octet_length(token_address) = 20),
    CONSTRAINT wallet_realized_legs_block_nonnegative CHECK (block_number >= 0),
    CONSTRAINT wallet_realized_legs_tx_index_nonnegative CHECK (transaction_index >= 0),
    CONSTRAINT wallet_realized_legs_nonzero CHECK (realized_pnl <> 0)
);

CREATE INDEX wallet_realized_legs_window_idx ON wallet_realized_legs (chain_id, block_time);

-- Public per-token watch counts.
CREATE INDEX watchlist_entries_token_idx ON watchlist_entries (chain_id, deployment_id, token_address);

-- +goose Down
DROP INDEX IF EXISTS watchlist_entries_token_idx;
DROP TABLE IF EXISTS wallet_realized_legs;
