-- Inbox page (spec P0-C4): events on the owner's watched tokens, at or after the watch started and
-- within 30 days, strictly older than the cursor position, newest first. Each watched token
-- contributes at most page_size rows through its own ordered index scan.
-- name: ListInboxEvents :many
SELECT e.kind, e.token_address::bytea AS token_address, l.name, l.symbol, e.block_number::bigint AS block_number,
       e.transaction_index::integer AS transaction_index, e.log_index::integer AS log_index,
       e.block_time::timestamptz AS block_time, e.tx_hash::bytea AS tx_hash, e.eth_wei::numeric AS eth_wei,
       e.side_buy, e.trader::bytea AS trader_raw, e.source
FROM watchlist_entries AS w
JOIN LATERAL (
    SELECT ev.*
    FROM inbox_events AS ev
    WHERE ev.chain_id = w.chain_id AND ev.token_address = w.token_address
      AND ev.block_time >= GREATEST(w.created_at, now() - interval '30 days')
      AND (ev.block_number, ev.transaction_index, ev.log_index) <
          (sqlc.arg(before_block)::bigint, sqlc.arg(before_tx_index)::integer, sqlc.arg(before_log_index)::integer)
    ORDER BY ev.block_number DESC, ev.transaction_index DESC, ev.log_index DESC
    LIMIT sqlc.arg(page_size)::integer
) AS e ON true
JOIN token_launches AS l ON l.chain_id = w.chain_id AND l.token_address = e.token_address
WHERE w.chain_id = sqlc.arg(chain_id)
  AND w.deployment_id = sqlc.arg(deployment_id)
  AND w.privy_did = sqlc.arg(privy_did)
ORDER BY e.block_number DESC, e.transaction_index DESC, e.log_index DESC
LIMIT sqlc.arg(page_size)::integer;

-- Unread events after the owner's read position, counted up to 100.
-- name: CountUnreadInboxEvents :one
WITH position AS (
    SELECT COALESCE(max(r.read_block), -1)::bigint AS read_block,
           COALESCE(max(r.read_tx_index), -1)::integer AS read_tx_index,
           COALESCE(max(r.read_log_index), -1)::integer AS read_log_index
    FROM notification_reads AS r
    WHERE r.chain_id = sqlc.arg(chain_id) AND r.deployment_id = sqlc.arg(deployment_id) AND r.privy_did = sqlc.arg(privy_did)
)
SELECT count(*)::bigint FROM (
    SELECT 1
    FROM watchlist_entries AS w
    CROSS JOIN position AS p
    JOIN LATERAL (
        SELECT 1
        FROM inbox_events AS ev
        WHERE ev.chain_id = w.chain_id AND ev.token_address = w.token_address
          AND ev.block_time >= GREATEST(w.created_at, now() - interval '30 days')
          AND (ev.block_number, ev.transaction_index, ev.log_index) > (p.read_block, p.read_tx_index, p.read_log_index)
        LIMIT 100
    ) AS e ON true
    WHERE w.chain_id = sqlc.arg(chain_id)
      AND w.deployment_id = sqlc.arg(deployment_id)
      AND w.privy_did = sqlc.arg(privy_did)
    LIMIT 100
) AS unread;

-- Moves the read position forward only; an older position leaves the row unchanged.
-- name: MarkInboxRead :exec
INSERT INTO notification_reads (chain_id, deployment_id, privy_did, read_block, read_tx_index, read_log_index)
VALUES (sqlc.arg(chain_id), sqlc.arg(deployment_id), sqlc.arg(privy_did), sqlc.arg(read_block), sqlc.arg(read_tx_index), sqlc.arg(read_log_index))
ON CONFLICT (chain_id, deployment_id, privy_did) DO UPDATE SET
    read_block = EXCLUDED.read_block, read_tx_index = EXCLUDED.read_tx_index,
    read_log_index = EXCLUDED.read_log_index, updated_at = now()
WHERE (EXCLUDED.read_block, EXCLUDED.read_tx_index, EXCLUDED.read_log_index) >
      (notification_reads.read_block, notification_reads.read_tx_index, notification_reads.read_log_index);
