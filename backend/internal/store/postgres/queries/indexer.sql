-- name: ListTokenIdentities :many
SELECT token_address, curve_address, lp_pair, engine_version,
       launch_block_number, launch_block_hash, launch_block_time
FROM tokens WHERE chain_id=$1 ORDER BY token_address;

-- name: ReadOperationalHealth :one
SELECT
    dirty.dirty_work,
    (SELECT count(*) FROM position_dirty AS entry WHERE entry.chain_id = $1)::BIGINT AS position_dirty_work,
    COALESCE(reorg.reorg_id, 0)::BIGINT AS last_reorg_id,
    COALESCE(reorg.depth, 0)::BIGINT AS last_reorg_depth,
    reorg.detected_at AS last_reorg_at
FROM (SELECT count(*)::BIGINT AS dirty_work FROM aggregation_dirty AS entry WHERE entry.chain_id = $1) AS dirty
LEFT JOIN LATERAL (
    SELECT reorg_id, depth, detected_at
    FROM indexer_reorgs AS entry
    WHERE entry.chain_id = $1 AND entry.deployment_id = $2
    ORDER BY reorg_id DESC
    LIMIT 1
) AS reorg ON TRUE;

-- name: ListTokenPhaseCounts :many
SELECT token.phase, count(*)::BIGINT AS token_count
FROM tokens AS token
WHERE token.chain_id = $1
GROUP BY token.phase
ORDER BY token.phase;
