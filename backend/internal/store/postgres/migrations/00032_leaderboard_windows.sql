-- +goose Up
-- Precomputed rolling-window leaderboards (docs/specs/2026-11-14-windowed-leaderboard-preflight.md).
-- The indexer's leaderboard refresher rewrites each (chain, window) ranking in one transaction:
-- the top 100 wallets by realized PnL inside the window, exclusions applied, ranked 1..n. Reads
-- use it while leaderboard_window_refreshes says it is recent and fall back to the live query
-- otherwise. Derived data: rebuilt on every refresh, no FK to wallets or tokens.
CREATE TABLE leaderboard_window_ranks (
    chain_id BIGINT NOT NULL,
    window_days INTEGER NOT NULL,
    rank INTEGER NOT NULL,
    wallet BYTEA NOT NULL,
    realized_pnl NUMERIC(79,0) NOT NULL,
    tokens_traded BIGINT NOT NULL,
    CONSTRAINT leaderboard_window_ranks_pkey PRIMARY KEY (chain_id, window_days, rank),
    CONSTRAINT leaderboard_window_ranks_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT leaderboard_window_ranks_window_days_supported CHECK (window_days IN (7, 30)),
    CONSTRAINT leaderboard_window_ranks_rank_range CHECK (rank BETWEEN 1 AND 100),
    CONSTRAINT leaderboard_window_ranks_wallet_length CHECK (octet_length(wallet) = 20),
    CONSTRAINT leaderboard_window_ranks_realized_positive CHECK (realized_pnl > 0),
    CONSTRAINT leaderboard_window_ranks_tokens_traded_positive CHECK (tokens_traded > 0)
);

CREATE TABLE leaderboard_window_refreshes (
    chain_id BIGINT NOT NULL,
    window_days INTEGER NOT NULL,
    refreshed_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT leaderboard_window_refreshes_pkey PRIMARY KEY (chain_id, window_days),
    CONSTRAINT leaderboard_window_refreshes_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT leaderboard_window_refreshes_window_days_supported CHECK (window_days IN (7, 30))
);

-- +goose Down
DROP TABLE IF EXISTS leaderboard_window_refreshes;
DROP TABLE IF EXISTS leaderboard_window_ranks;
