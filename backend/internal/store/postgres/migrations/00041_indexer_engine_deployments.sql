-- +goose Up
-- The launch engines one indexer ingests side by side on a chain, and the factory and start
-- block each was first ingested with (docs/specs/2026-11-20-dual-engine-preflight.md, decision
-- 5). An engine listed after the cursor passed its start block would silently miss its early
-- launches, so the indexer refuses to start instead of recording it.
CREATE TABLE indexer_engine_deployments (
    chain_id BIGINT NOT NULL,
    deployment_id TEXT NOT NULL,
    engine_deployment_id TEXT NOT NULL,
    engine_version INTEGER NOT NULL,
    factory BYTEA NOT NULL,
    start_block BIGINT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT indexer_engine_deployments_pkey PRIMARY KEY (chain_id, deployment_id, engine_deployment_id),
    CONSTRAINT indexer_engine_deployments_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT indexer_engine_deployments_engine_version_known CHECK (engine_version IN (1, 2)),
    CONSTRAINT indexer_engine_deployments_factory_length CHECK (octet_length(factory) = 20),
    CONSTRAINT indexer_engine_deployments_start_block_nonnegative CHECK (start_block >= 0)
);

GRANT SELECT ON indexer_engine_deployments TO launchpad_indexer_role;
GRANT INSERT ON indexer_engine_deployments TO launchpad_indexer_role;

-- +goose Down
DROP TABLE indexer_engine_deployments;
