-- name: UpsertSyncState :one
INSERT INTO sync_state (
    chain_id, deployment_id,
    observed_number, observed_hash, observed_at,
    safe_number, safe_hash, safe_at,
    finalized_number, finalized_hash, finalized_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (chain_id, deployment_id) DO UPDATE
SET observed_number = EXCLUDED.observed_number,
    observed_hash = EXCLUDED.observed_hash,
    observed_at = EXCLUDED.observed_at,
    safe_number = EXCLUDED.safe_number,
    safe_hash = EXCLUDED.safe_hash,
    safe_at = EXCLUDED.safe_at,
    finalized_number = EXCLUDED.finalized_number,
    finalized_hash = EXCLUDED.finalized_hash,
    finalized_at = EXCLUDED.finalized_at
RETURNING
    chain_id, deployment_id,
    observed_number, observed_hash, observed_at,
    safe_number, safe_hash, safe_at,
    finalized_number, finalized_hash, finalized_at;

-- name: GetSyncState :one
SELECT
    chain_id, deployment_id,
    observed_number, observed_hash, observed_at,
    safe_number, safe_hash, safe_at,
    finalized_number, finalized_hash, finalized_at
FROM sync_state
WHERE chain_id = $1 AND deployment_id = $2;

-- name: ListForeignSyncDeployments :many
SELECT deployment_id
FROM sync_state
WHERE chain_id = $1 AND deployment_id <> $2
ORDER BY deployment_id;

-- name: NotifyAPIRefresh :exec
SELECT pg_notify('api_refresh', sqlc.arg(payload)::text);

-- name: TouchIndexerHeartbeat :exec
INSERT INTO indexer_heartbeats (chain_id, deployment_id, beat_at)
VALUES ($1, $2, now())
ON CONFLICT (chain_id, deployment_id) DO UPDATE SET beat_at = EXCLUDED.beat_at;

-- name: GetIndexerStatus :one
SELECT
    now()::TIMESTAMPTZ AS checked_at,
    s.observed_number,
    s.safe_number,
    s.finalized_number,
    h.beat_at
FROM (SELECT 1) AS probe
LEFT JOIN sync_state AS s
    ON s.chain_id = sqlc.arg(chain_id) AND s.deployment_id = sqlc.arg(deployment_id)
LEFT JOIN indexer_heartbeats AS h
    ON h.chain_id = s.chain_id AND h.deployment_id = s.deployment_id;

-- name: ListIndexerEngineDeployments :many
SELECT engine_deployment_id, engine_version, factory, start_block
FROM indexer_engine_deployments
WHERE chain_id = sqlc.arg(chain_id) AND deployment_id = sqlc.arg(deployment_id)
ORDER BY engine_deployment_id;

-- name: InsertIndexerEngineDeployment :exec
INSERT INTO indexer_engine_deployments (chain_id, deployment_id, engine_deployment_id, engine_version, factory, start_block)
VALUES (sqlc.arg(chain_id), sqlc.arg(deployment_id), sqlc.arg(engine_deployment_id), sqlc.arg(engine_version), sqlc.arg(factory), sqlc.arg(start_block));
