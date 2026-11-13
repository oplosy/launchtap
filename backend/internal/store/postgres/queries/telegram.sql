-- Telegram alerts (docs/specs/2026-11-12-telegram-alerts-design.md).

-- Replaces the account's pending link code; expiry runs on the database clock.
-- name: UpsertTelegramLinkCode :one
INSERT INTO telegram_link_codes (chain_id, deployment_id, privy_did, code_sha256, expires_at)
VALUES (sqlc.arg(chain_id), sqlc.arg(deployment_id), sqlc.arg(privy_did), sqlc.arg(code_sha256),
        now() + make_interval(secs => sqlc.arg(ttl_seconds)::integer))
ON CONFLICT (chain_id, deployment_id, privy_did) DO UPDATE SET
    code_sha256 = EXCLUDED.code_sha256, expires_at = EXCLUDED.expires_at
RETURNING expires_at;

-- Consumes a code (used or expired codes are deleted either way); live tells whether it was valid.
-- name: ConsumeTelegramLinkCode :one
DELETE FROM telegram_link_codes
WHERE chain_id = sqlc.arg(chain_id) AND deployment_id = sqlc.arg(deployment_id) AND code_sha256 = sqlc.arg(code_sha256)
RETURNING privy_did, (expires_at > now())::boolean AS live;

-- Frees a chat bound to another account before it moves to a new one.
-- name: DeleteTelegramChatOfOtherAccount :exec
DELETE FROM telegram_links
WHERE chain_id = sqlc.arg(chain_id) AND deployment_id = sqlc.arg(deployment_id)
  AND chat_id = sqlc.arg(chat_id) AND privy_did <> sqlc.arg(privy_did);

-- Binds the chat to the account. A new or moved link starts its cursor at the safe head, so no
-- backlog is sent; settings survive a re-link.
-- name: UpsertTelegramLink :exec
INSERT INTO telegram_links (chain_id, deployment_id, privy_did, chat_id, delivered_block, delivered_tx_index, delivered_log_index)
SELECT sqlc.arg(chain_id)::bigint, sqlc.arg(deployment_id)::text, sqlc.arg(privy_did)::text, sqlc.arg(chat_id)::bigint,
       COALESCE((SELECT s.safe_number FROM sync_state AS s
                 WHERE s.chain_id = sqlc.arg(chain_id)::bigint AND s.deployment_id = sqlc.arg(deployment_id)::text), 0)::bigint,
       2147483647, 2147483647
ON CONFLICT (chain_id, deployment_id, privy_did) DO UPDATE SET
    chat_id = EXCLUDED.chat_id, delivered_block = EXCLUDED.delivered_block,
    delivered_tx_index = EXCLUDED.delivered_tx_index, delivered_log_index = EXCLUDED.delivered_log_index,
    linked_at = now();

-- name: GetTelegramLink :one
SELECT graduations, large_trades, min_trade_wei::text AS min_trade_wei_text, linked_at
FROM telegram_links
WHERE chain_id = sqlc.arg(chain_id) AND deployment_id = sqlc.arg(deployment_id) AND privy_did = sqlc.arg(privy_did);

-- name: UpdateTelegramSettings :execrows
UPDATE telegram_links
SET graduations = sqlc.arg(graduations), large_trades = sqlc.arg(large_trades),
    min_trade_wei = sqlc.arg(min_trade_wei_text)::text::numeric
WHERE chain_id = sqlc.arg(chain_id) AND deployment_id = sqlc.arg(deployment_id) AND privy_did = sqlc.arg(privy_did);

-- name: DeleteTelegramLink :exec
DELETE FROM telegram_links
WHERE chain_id = sqlc.arg(chain_id) AND deployment_id = sqlc.arg(deployment_id) AND privy_did = sqlc.arg(privy_did);

-- name: DeleteTelegramLinkCode :exec
DELETE FROM telegram_link_codes
WHERE chain_id = sqlc.arg(chain_id) AND deployment_id = sqlc.arg(deployment_id) AND privy_did = sqlc.arg(privy_did);

-- name: DeleteTelegramLinkByChat :execrows
DELETE FROM telegram_links
WHERE chain_id = sqlc.arg(chain_id) AND deployment_id = sqlc.arg(deployment_id) AND chat_id = sqlc.arg(chat_id);

-- name: ListTelegramLinks :many
SELECT privy_did, chat_id, graduations, large_trades, min_trade_wei::text AS min_trade_wei_text,
       delivered_block, delivered_tx_index, delivered_log_index
FROM telegram_links
WHERE chain_id = sqlc.arg(chain_id) AND deployment_id = sqlc.arg(deployment_id)
ORDER BY privy_did;

-- Inbox events on the account's watched tokens after the cursor, in blocks at or below the safe
-- head, passing the link's settings, oldest first. Each watched token contributes at most
-- page_size rows through its own ordered index scan.
-- name: ListTelegramPendingEvents :many
SELECT e.kind, e.token_address::bytea AS token_address, l.name, l.symbol, e.block_number::bigint AS block_number,
       e.transaction_index::integer AS transaction_index, e.log_index::integer AS log_index,
       e.block_time::timestamptz AS block_time, e.tx_hash::bytea AS tx_hash, e.eth_wei::numeric AS eth_wei,
       e.side_buy, e.trader::bytea AS trader_raw, e.source
FROM watchlist_entries AS w
CROSS JOIN (
    SELECT COALESCE((SELECT s.safe_number FROM sync_state AS s
                     WHERE s.chain_id = sqlc.arg(chain_id) AND s.deployment_id = sqlc.arg(deployment_id)), -1)::bigint AS head
) AS safe
JOIN LATERAL (
    SELECT ev.*
    FROM inbox_events AS ev
    WHERE ev.chain_id = w.chain_id AND ev.token_address = w.token_address
      AND ev.block_time >= w.created_at
      AND ev.block_number <= safe.head
      AND (ev.block_number, ev.transaction_index, ev.log_index) >
          (sqlc.arg(after_block)::bigint, sqlc.arg(after_tx_index)::integer, sqlc.arg(after_log_index)::integer)
      AND ((ev.kind = 'graduation' AND sqlc.arg(graduations)::boolean)
           OR (ev.kind = 'large_trade' AND sqlc.arg(large_trades)::boolean
               AND ev.eth_wei >= sqlc.arg(min_trade_wei_text)::text::numeric))
    ORDER BY ev.block_number, ev.transaction_index, ev.log_index
    LIMIT sqlc.arg(page_size)::integer
) AS e ON true
JOIN token_launches AS l ON l.chain_id = w.chain_id AND l.token_address = e.token_address
WHERE w.chain_id = sqlc.arg(chain_id)
  AND w.deployment_id = sqlc.arg(deployment_id)
  AND w.privy_did = sqlc.arg(privy_did)
ORDER BY e.block_number, e.transaction_index, e.log_index
LIMIT sqlc.arg(page_size)::integer;

-- Moves the cursor forward only, and only while the account is still bound to the same chat.
-- name: AdvanceTelegramCursor :exec
UPDATE telegram_links
SET delivered_block = sqlc.arg(block)::bigint, delivered_tx_index = sqlc.arg(tx_index)::integer,
    delivered_log_index = sqlc.arg(log_index)::integer
WHERE chain_id = sqlc.arg(chain_id) AND deployment_id = sqlc.arg(deployment_id) AND privy_did = sqlc.arg(privy_did)
  AND chat_id = sqlc.arg(chat_id)
  AND (sqlc.arg(block)::bigint, sqlc.arg(tx_index)::integer, sqlc.arg(log_index)::integer) >
      (delivered_block, delivered_tx_index, delivered_log_index);
