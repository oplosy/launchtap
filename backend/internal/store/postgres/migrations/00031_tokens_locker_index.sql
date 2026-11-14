-- +goose Up
-- The leaderboard excludes every infrastructure address with NOT EXISTS lookups on tokens; curve
-- and pair have indexes since 00018 and 00004, but the engine-v2 locker column (00024) had none.
-- Without it the planner hashes all of tokens and wallet_totals and sorts every ranked wallet
-- instead of walking wallet_totals_leaderboard_idx and stopping after the page. Only engine-v2
-- tokens have a locker, so the index is partial.
CREATE INDEX tokens_locker_idx ON tokens (chain_id, locker) WHERE locker IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS tokens_locker_idx;
