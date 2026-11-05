-- name: CountWatchlist :one
SELECT count(*)::BIGINT
FROM watchlist_entries
WHERE chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id)
  AND privy_did = sqlc.arg(privy_did);

-- Serializes cap check + insert per owner. The key is hashed in SQL.
-- name: LockWatchlistOwner :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(owner_key)::text, 0));

-- name: InsertWatchlistEntry :execrows
INSERT INTO watchlist_entries (chain_id, deployment_id, privy_did, token_address)
VALUES (sqlc.arg(chain_id), sqlc.arg(deployment_id), sqlc.arg(privy_did), sqlc.arg(token_address))
ON CONFLICT DO NOTHING;

-- name: DeleteWatchlistEntry :execrows
DELETE FROM watchlist_entries
WHERE chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id)
  AND privy_did = sqlc.arg(privy_did)
  AND token_address = sqlc.arg(token_address);

-- name: ListWatchlist :many
SELECT w.token_address AS watched_token_address, w.created_at,
       t.token_address, t.name, t.symbol, t.phase,
       t.launch_block_number, t.launch_block_time, t.total_supply,
       COALESCE(s.market_cap_eth_wad, 0::numeric) AS market_cap_eth_wad,
       COALESCE(s.volume_24h_eth_wad, 0::numeric) AS volume_24h_eth_wad,
       COALESCE(s.holder_count, 0)::BIGINT AS holder_count,
       m.description, m.image_url, m.x_url, m.telegram_url,
       t.launch_block_hash
FROM watchlist_entries AS w
LEFT JOIN tokens AS t
  ON t.chain_id = w.chain_id AND t.token_address = w.token_address
LEFT JOIN token_metadata AS m
  ON m.chain_id = t.chain_id AND m.token_address = t.token_address
 AND m.launch_tx_hash = t.launch_tx_hash AND m.launch_log_index = t.launch_log_index
LEFT JOIN token_stats AS s
  ON s.chain_id = t.chain_id AND s.token_address = t.token_address
WHERE w.chain_id = sqlc.arg(chain_id)
  AND w.deployment_id = sqlc.arg(deployment_id)
  AND w.privy_did = sqlc.arg(privy_did)
ORDER BY w.created_at DESC, w.token_address
LIMIT sqlc.arg(row_limit)::integer;
