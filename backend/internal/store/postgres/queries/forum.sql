-- Forum feed (spec P0-C5): visible comments across tokens that still exist, newest first, with
-- the same read-time author display and ban filter as ListTokenComments.
-- name: ListForumPosts :many
SELECT c.id, c.token_address::bytea AS token_address, l.name, l.symbol, c.body, c.created_at,
       c.author_role AS role,
       COALESCE((SELECT sum(v.value)
                 FROM comment_votes AS v
                 JOIN social_users AS vu
                   ON vu.chain_id = v.chain_id AND vu.deployment_id = v.deployment_id AND vu.privy_did = v.voter_did
                 WHERE v.comment_id = c.id AND vu.banned_at IS NULL), 0)::bigint AS score,
       u.username AS display_username,
       (CASE
            WHEN u.username IS NULL OR pw.wallet IS NOT NULL THEN c.wallet
            ELSE NULL
        END)::bytea AS display_wallet
FROM token_comments AS c
JOIN token_launches AS l ON l.chain_id = c.chain_id AND l.token_address = c.token_address
-- Same existence rule as the token comment list (TokenExists): a reorged token drops out.
JOIN tokens AS tk ON tk.chain_id = c.chain_id AND tk.token_address = c.token_address
LEFT JOIN social_users AS u
  ON u.chain_id = c.chain_id AND u.deployment_id = c.deployment_id AND u.privy_did = c.privy_did
LEFT JOIN social_public_wallets AS pw
  ON pw.chain_id = c.chain_id AND pw.deployment_id = c.deployment_id
 AND pw.wallet = c.wallet AND pw.privy_did = c.privy_did
WHERE c.chain_id = sqlc.arg(chain_id)
  AND c.deployment_id = sqlc.arg(deployment_id)
  AND c.hidden_at IS NULL
  AND u.banned_at IS NULL
  AND (sqlc.narg(before_id)::bigint IS NULL OR c.id < sqlc.narg(before_id)::bigint)
ORDER BY c.id DESC
LIMIT sqlc.arg(row_limit)::integer;

-- Communities ranked by visible comments in the last window_hours by the database clock (NULL =
-- all time), ties by newest comment.
-- name: ListForumCommunities :many
SELECT c.token_address::bytea AS token_address, l.name, l.symbol,
       count(*)::bigint AS comment_count,
       max(c.created_at)::timestamptz AS last_comment_at
FROM token_comments AS c
JOIN token_launches AS l ON l.chain_id = c.chain_id AND l.token_address = c.token_address
-- Same existence rule as the token comment list (TokenExists): a reorged token drops out.
JOIN tokens AS tk ON tk.chain_id = c.chain_id AND tk.token_address = c.token_address
LEFT JOIN social_users AS u
  ON u.chain_id = c.chain_id AND u.deployment_id = c.deployment_id AND u.privy_did = c.privy_did
WHERE c.chain_id = sqlc.arg(chain_id)
  AND c.deployment_id = sqlc.arg(deployment_id)
  AND c.hidden_at IS NULL
  AND u.banned_at IS NULL
  AND (sqlc.narg(window_hours)::integer IS NULL
       OR c.created_at >= now() - make_interval(hours => sqlc.narg(window_hours)::integer))
GROUP BY c.token_address, l.name, l.symbol
ORDER BY comment_count DESC, max(c.id) DESC
LIMIT sqlc.arg(row_limit)::integer;

-- Forum "Best" (docs/specs/2026-11-12-comment-votes-design.md): the feed's visible posts from the
-- last window_days by the database clock, highest score first, then newest. One page, no cursor.
-- name: ListForumBestPosts :many
SELECT c.id, c.token_address::bytea AS token_address, l.name, l.symbol, c.body, c.created_at,
       c.author_role AS role,
       COALESCE((SELECT sum(v.value)
                 FROM comment_votes AS v
                 JOIN social_users AS vu
                   ON vu.chain_id = v.chain_id AND vu.deployment_id = v.deployment_id AND vu.privy_did = v.voter_did
                 WHERE v.comment_id = c.id AND vu.banned_at IS NULL), 0)::bigint AS score,
       u.username AS display_username,
       (CASE
            WHEN u.username IS NULL OR pw.wallet IS NOT NULL THEN c.wallet
            ELSE NULL
        END)::bytea AS display_wallet
FROM token_comments AS c
JOIN token_launches AS l ON l.chain_id = c.chain_id AND l.token_address = c.token_address
-- Same existence rule as the token comment list (TokenExists): a reorged token drops out.
JOIN tokens AS tk ON tk.chain_id = c.chain_id AND tk.token_address = c.token_address
LEFT JOIN social_users AS u
  ON u.chain_id = c.chain_id AND u.deployment_id = c.deployment_id AND u.privy_did = c.privy_did
LEFT JOIN social_public_wallets AS pw
  ON pw.chain_id = c.chain_id AND pw.deployment_id = c.deployment_id
 AND pw.wallet = c.wallet AND pw.privy_did = c.privy_did
WHERE c.chain_id = sqlc.arg(chain_id)
  AND c.deployment_id = sqlc.arg(deployment_id)
  AND c.hidden_at IS NULL
  AND u.banned_at IS NULL
  AND c.created_at >= now() - make_interval(days => sqlc.arg(window_days)::integer)
ORDER BY score DESC, c.id DESC
LIMIT sqlc.arg(row_limit)::integer;
