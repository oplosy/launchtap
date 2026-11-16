-- +goose Up
-- Position queue writes exclude any token's LpFeeLocker on every transfer leg, like the curve
-- and pair lookups (tokens_pair_lookup_idx). Only engine-2 tokens have a locker.
CREATE INDEX tokens_locker_lookup_idx ON tokens (chain_id, locker) WHERE locker IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS tokens_locker_lookup_idx;
