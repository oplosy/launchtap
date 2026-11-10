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
-- release_at is the hourly_limit-th newest comment inside the window: once it leaves the window
-- fewer than hourly_limit remain, so it is the exact end of an hourly block even when the
-- window holds more rows than the limit.
SELECT max(created_at)::timestamptz AS last_created_at,
       (count(*) FILTER (WHERE created_at > now() - make_interval(secs => sqlc.arg(window_seconds)::double precision)))::BIGINT AS count_in_window,
       (SELECT r.created_at
        FROM token_comments AS r
        WHERE r.chain_id = sqlc.arg(chain_id)
          AND r.deployment_id = sqlc.arg(deployment_id)
          AND r.privy_did = sqlc.arg(privy_did)
          AND r.created_at > now() - make_interval(secs => sqlc.arg(window_seconds)::double precision)
        ORDER BY r.created_at DESC
        OFFSET sqlc.arg(hourly_limit)::integer - 1
        LIMIT 1)::timestamptz AS release_at,
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

-- No row when the comment is missing in this scope, hidden, or its author is banned. Every author
-- has a social_users row (post creates it before inserting a comment). FOR SHARE locks the comment
-- and the author row, so a report waits for an in-flight hide or ban and then re-checks the
-- committed state; the later report insert cannot leave an open report on actioned content.
-- name: GetReportableComment :one
SELECT c.id, c.chain_id, c.deployment_id, c.token_address, c.privy_did AS author_did
FROM token_comments AS c
JOIN social_users AS u
  ON u.chain_id = c.chain_id AND u.deployment_id = c.deployment_id AND u.privy_did = c.privy_did
WHERE c.id = sqlc.arg(comment_id)
  AND c.chain_id = sqlc.arg(chain_id)
  AND c.deployment_id = sqlc.arg(deployment_id)
  AND c.hidden_at IS NULL
  AND u.banned_at IS NULL
FOR SHARE OF c, u;

-- name: InsertReport :execrows
INSERT INTO comment_reports (comment_id, chain_id, deployment_id, reporter_did, reason)
VALUES (sqlc.arg(comment_id), sqlc.arg(chain_id), sqlc.arg(deployment_id), sqlc.arg(reporter_did), sqlc.arg(reason))
ON CONFLICT DO NOTHING;

-- name: ReportRateState :one
-- release_at: the hourly_limit-th newest report in the window (see CommentRateState).
SELECT count(*)::BIGINT AS count_in_window,
       (SELECT r.created_at
        FROM comment_reports AS r
        WHERE r.chain_id = sqlc.arg(chain_id)
          AND r.deployment_id = sqlc.arg(deployment_id)
          AND r.reporter_did = sqlc.arg(reporter_did)
          AND r.created_at > now() - make_interval(secs => sqlc.arg(window_seconds)::double precision)
        ORDER BY r.created_at DESC
        OFFSET sqlc.arg(hourly_limit)::integer - 1
        LIMIT 1)::timestamptz AS release_at,
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

-- name: GetProfileState :one
SELECT username, username_changed_at, banned_at, now()::timestamptz AS db_now
FROM social_users
WHERE chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id)
  AND privy_did = sqlc.arg(privy_did);

-- name: ListOwnPublicWallets :many
SELECT wallet
FROM social_public_wallets
WHERE chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id)
  AND privy_did = sqlc.arg(privy_did)
ORDER BY wallet;

-- A name is unavailable to the caller when another user holds it, or another user released it
-- within hold_seconds (database clock). The caller's own releases never block it.
-- name: UsernameUnavailable :one
SELECT EXISTS (
    SELECT 1 FROM social_users
    WHERE chain_id = sqlc.arg(chain_id)::bigint AND deployment_id = sqlc.arg(deployment_id)::text
      AND username = sqlc.arg(username)::text AND privy_did <> sqlc.arg(privy_did)::text
) OR EXISTS (
    SELECT 1 FROM username_releases
    WHERE chain_id = sqlc.arg(chain_id)::bigint AND deployment_id = sqlc.arg(deployment_id)::text
      AND username = sqlc.arg(username)::text AND privy_did <> sqlc.arg(privy_did)::text
      AND released_at > now() - make_interval(secs => sqlc.arg(hold_seconds)::double precision)
) AS unavailable;

-- name: SetUsername :exec
UPDATE social_users
SET username = sqlc.narg(username), username_changed_at = now(), updated_at = now()
WHERE chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id)
  AND privy_did = sqlc.arg(privy_did);

-- name: ReleaseUsername :exec
INSERT INTO username_releases (chain_id, deployment_id, username, privy_did)
VALUES (sqlc.arg(chain_id), sqlc.arg(deployment_id), sqlc.arg(username), sqlc.arg(privy_did))
ON CONFLICT (chain_id, deployment_id, username, privy_did) DO UPDATE SET released_at = now();

-- R10: publishing a wallet another user published moves it to the caller.
-- name: ClaimPublicWallet :exec
INSERT INTO social_public_wallets (chain_id, deployment_id, wallet, privy_did)
VALUES (sqlc.arg(chain_id), sqlc.arg(deployment_id), sqlc.arg(wallet), sqlc.arg(privy_did))
ON CONFLICT (chain_id, deployment_id, wallet)
DO UPDATE SET privy_did = EXCLUDED.privy_did, created_at = now()
WHERE social_public_wallets.privy_did <> EXCLUDED.privy_did;
