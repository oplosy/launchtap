-- Serializes per-subject social writes (rate limits, username changes). The key is built in SQL.
-- name: LockSocialSubject :exec
SELECT pg_advisory_xact_lock(hashtextextended(
    'social' || chr(31) || sqlc.arg(chain_id)::bigint::text || chr(31) || sqlc.arg(deployment_id)::text
    || chr(31) || sqlc.arg(subject_key)::text, 0));

-- name: EnsureSocialUser :exec
INSERT INTO social_users (chain_id, deployment_id, privy_did)
VALUES (sqlc.arg(chain_id), sqlc.arg(deployment_id), sqlc.arg(privy_did))
ON CONFLICT DO NOTHING;

-- name: GetSocialUser :one
SELECT username, username_changed_at, banned_at
FROM social_users
WHERE chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id)
  AND privy_did = sqlc.arg(privy_did);

-- Wallets that are the token creator or hold a positive balance; creator wins.
-- deployment_id is accepted for scope uniformity; tokens and balances are chain-scoped.
-- name: QualifyWallets :many
SELECT w.wallet::bytea AS wallet,
       (CASE WHEN t.creator = w.wallet THEN 'creator' ELSE 'holder' END)::text AS role
FROM unnest(sqlc.arg(wallets)::bytea[]) AS w(wallet)
LEFT JOIN tokens AS t
  ON t.chain_id = sqlc.arg(chain_id) AND t.token_address = sqlc.arg(token_address)
LEFT JOIN holder_balances AS h
  ON h.chain_id = sqlc.arg(chain_id) AND h.token_address = sqlc.arg(token_address)
 AND h.holder_address = w.wallet
WHERE sqlc.arg(deployment_id)::text <> '' AND (t.creator = w.wallet OR h.balance > 0)
ORDER BY w.wallet;

-- Windows are measured on the database clock; window_seconds is a policy constant.
-- name: CommentRateState :one
SELECT max(created_at)::timestamptz AS last_created_at,
       (count(*) FILTER (WHERE created_at > now() - make_interval(secs => sqlc.arg(window_seconds)::double precision)))::BIGINT AS count_in_window,
       (min(created_at) FILTER (WHERE created_at > now() - make_interval(secs => sqlc.arg(window_seconds)::double precision)))::timestamptz AS oldest_in_window,
       now()::timestamptz AS db_now
FROM token_comments
WHERE chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id)
  AND privy_did = sqlc.arg(privy_did);

-- name: InsertComment :one
INSERT INTO token_comments (chain_id, deployment_id, token_address, privy_did, wallet, author_role, body)
VALUES (sqlc.arg(chain_id), sqlc.arg(deployment_id), sqlc.arg(token_address), sqlc.arg(privy_did),
        sqlc.arg(wallet), sqlc.arg(author_role), sqlc.arg(body))
RETURNING id, chain_id, deployment_id, token_address, privy_did, wallet, author_role, body, created_at, hidden_at, hidden_reason;

-- Read-time author display (R2): the username (banned authors are excluded); the posting wallet
-- only when the author has no username or published that wallet.
-- name: ListTokenComments :many
SELECT c.id, c.body, c.created_at, c.author_role AS role,
       u.username AS display_username,
       (CASE
            WHEN u.username IS NULL OR pw.wallet IS NOT NULL THEN c.wallet
            ELSE NULL
        END)::bytea AS display_wallet
FROM token_comments AS c
LEFT JOIN social_users AS u
  ON u.chain_id = c.chain_id AND u.deployment_id = c.deployment_id AND u.privy_did = c.privy_did
LEFT JOIN social_public_wallets AS pw
  ON pw.chain_id = c.chain_id AND pw.deployment_id = c.deployment_id
 AND pw.wallet = c.wallet AND pw.privy_did = c.privy_did
WHERE c.chain_id = sqlc.arg(chain_id)
  AND c.deployment_id = sqlc.arg(deployment_id)
  AND c.token_address = sqlc.arg(token_address)
  AND c.hidden_at IS NULL
  AND u.banned_at IS NULL
  AND (sqlc.narg(before_id)::bigint IS NULL OR c.id < sqlc.narg(before_id)::bigint)
ORDER BY c.id DESC
LIMIT sqlc.arg(row_limit)::integer;

-- name: CountVisibleComments :one
SELECT count(*)::BIGINT
FROM token_comments AS c
LEFT JOIN social_users AS u
  ON u.chain_id = c.chain_id AND u.deployment_id = c.deployment_id AND u.privy_did = c.privy_did
WHERE c.chain_id = sqlc.arg(chain_id)
  AND c.deployment_id = sqlc.arg(deployment_id)
  AND c.token_address = sqlc.arg(token_address)
  AND c.hidden_at IS NULL
  AND u.banned_at IS NULL;

-- No row when the comment is missing in this scope, hidden, or its author is banned.
-- name: GetReportableComment :one
SELECT c.id, c.chain_id, c.deployment_id, c.token_address, c.privy_did AS author_did
FROM token_comments AS c
LEFT JOIN social_users AS u
  ON u.chain_id = c.chain_id AND u.deployment_id = c.deployment_id AND u.privy_did = c.privy_did
WHERE c.id = sqlc.arg(comment_id)
  AND c.chain_id = sqlc.arg(chain_id)
  AND c.deployment_id = sqlc.arg(deployment_id)
  AND c.hidden_at IS NULL
  AND u.banned_at IS NULL;

-- name: InsertReport :execrows
INSERT INTO comment_reports (comment_id, chain_id, deployment_id, reporter_did, reason)
VALUES (sqlc.arg(comment_id), sqlc.arg(chain_id), sqlc.arg(deployment_id), sqlc.arg(reporter_did), sqlc.arg(reason))
ON CONFLICT DO NOTHING;

-- name: ReportRateState :one
SELECT count(*)::BIGINT AS count_in_window,
       min(created_at)::timestamptz AS oldest_in_window,
       now()::timestamptz AS db_now
FROM comment_reports
WHERE chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id)
  AND reporter_did = sqlc.arg(reporter_did)
  AND created_at > now() - make_interval(secs => sqlc.arg(window_seconds)::double precision);

-- R10: drop the caller's published wallets that are no longer linked. keep_wallets is the
-- principal's current wallet list.
-- name: PrunePublicWallets :exec
DELETE FROM social_public_wallets
WHERE chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id)
  AND privy_did = sqlc.arg(privy_did)
  AND NOT (wallet = ANY(sqlc.arg(keep_wallets)::bytea[]));

-- name: IsPublicWallet :one
SELECT EXISTS (
    SELECT 1
    FROM social_public_wallets
    WHERE chain_id = sqlc.arg(chain_id)
      AND deployment_id = sqlc.arg(deployment_id)
      AND privy_did = sqlc.arg(privy_did)
      AND wallet = sqlc.arg(wallet)
);
