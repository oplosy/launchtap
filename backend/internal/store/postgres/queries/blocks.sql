-- name: UpsertIndexedBlock :execrows
INSERT INTO indexed_blocks (
    chain_id,
    block_number,
    block_hash,
    parent_hash,
    block_time,
    finality_status
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (chain_id, block_number) DO UPDATE
SET finality_status = EXCLUDED.finality_status
WHERE indexed_blocks.block_hash = EXCLUDED.block_hash
  AND indexed_blocks.parent_hash = EXCLUDED.parent_hash
  AND indexed_blocks.block_time = EXCLUDED.block_time;

-- name: UpsertIndexedBlocks :execrows
-- Multi-row form of UpsertIndexedBlock with the same conflict rule: a row whose identity
-- differs from the stored one is skipped, so fewer affected rows than given means a conflict.
INSERT INTO indexed_blocks (
    chain_id,
    block_number,
    block_hash,
    parent_hash,
    block_time,
    finality_status
)
SELECT sqlc.arg(chain_id)::bigint, n.block_number, h.block_hash, p.parent_hash, t.block_time, f.finality_status
FROM unnest(sqlc.arg(block_numbers)::bigint[]) WITH ORDINALITY AS n(block_number, ord)
JOIN unnest(sqlc.arg(block_hashes)::bytea[]) WITH ORDINALITY AS h(block_hash, ord) ON h.ord = n.ord
JOIN unnest(sqlc.arg(parent_hashes)::bytea[]) WITH ORDINALITY AS p(parent_hash, ord) ON p.ord = n.ord
JOIN unnest(sqlc.arg(block_times)::timestamptz[]) WITH ORDINALITY AS t(block_time, ord) ON t.ord = n.ord
JOIN unnest(sqlc.arg(finality_statuses)::text[]) WITH ORDINALITY AS f(finality_status, ord) ON f.ord = n.ord
ON CONFLICT (chain_id, block_number) DO UPDATE
SET finality_status = EXCLUDED.finality_status
WHERE indexed_blocks.block_hash = EXCLUDED.block_hash
  AND indexed_blocks.parent_hash = EXCLUDED.parent_hash
  AND indexed_blocks.block_time = EXCLUDED.block_time;

-- name: GetIndexedBlockByNumber :one
SELECT chain_id, block_number, block_hash, parent_hash, block_time, finality_status
FROM indexed_blocks
WHERE chain_id = $1 AND block_number = $2;

-- name: GetIndexedBlockByHash :one
SELECT chain_id, block_number, block_hash, parent_hash, block_time, finality_status
FROM indexed_blocks
WHERE chain_id = $1 AND block_hash = $2;

-- name: PromoteIndexedBlocks :exec
UPDATE indexed_blocks
SET finality_status = CASE
 WHEN $3 = 'finalized' THEN 'finalized'
 WHEN finality_status = 'finalized' THEN 'finalized'
 ELSE 'safe'
END
WHERE chain_id=$1 AND block_number > $4 AND block_number <= $2;
