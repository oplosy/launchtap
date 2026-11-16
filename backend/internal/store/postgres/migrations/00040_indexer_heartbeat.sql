-- +goose Up
-- Wall-clock time of the indexer's last committed step (audit pass 3, L-5). The API reports its
-- age so the web can warn that data is stale. Block time cannot: a quiet chain and a stalled
-- indexer show the same old block time, and the chain's clock is not ours.
CREATE TABLE indexer_heartbeats (
    chain_id BIGINT NOT NULL,
    deployment_id TEXT NOT NULL,
    beat_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT indexer_heartbeats_pkey PRIMARY KEY (chain_id, deployment_id),
    CONSTRAINT indexer_heartbeats_sync_state_fkey FOREIGN KEY (chain_id, deployment_id)
        REFERENCES sync_state (chain_id, deployment_id) ON DELETE CASCADE
);

GRANT SELECT ON indexer_heartbeats TO launchpad_api_role, launchpad_indexer_role,
    launchpad_telegram_role, launchpad_funding_role, launchpad_moderate_role;
GRANT INSERT, UPDATE, DELETE ON indexer_heartbeats TO launchpad_indexer_role;

-- +goose Down
DROP TABLE indexer_heartbeats;
