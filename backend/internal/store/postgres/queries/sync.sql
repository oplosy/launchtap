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
