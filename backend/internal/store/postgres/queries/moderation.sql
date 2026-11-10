-- Comments with open reports, newest open report first. Policy: none; limit is the caller's.
-- name: ListOpenReportedComments :many
SELECT c.id, c.token_address, c.created_at, c.body, c.privy_did AS author_did, c.wallet,
       count(*)::BIGINT AS reports,
       array_agg(DISTINCT r.reason ORDER BY r.reason)::text[] AS reasons,
       max(r.created_at)::timestamptz AS newest_report_at
FROM comment_reports AS r
JOIN token_comments AS c ON c.id = r.comment_id
WHERE r.chain_id = sqlc.arg(chain_id)
  AND r.deployment_id = sqlc.arg(deployment_id)
  AND c.chain_id = r.chain_id
  AND c.deployment_id = r.deployment_id
  AND r.resolved_at IS NULL
GROUP BY c.id
ORDER BY newest_report_at DESC, c.id DESC
LIMIT sqlc.arg(row_limit)::integer;

-- Locks the comment row so concurrent moderation of one comment serializes.
-- name: GetCommentForModeration :one
SELECT id, token_address, privy_did AS author_did
FROM token_comments
WHERE id = sqlc.arg(comment_id)
  AND chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id)
FOR UPDATE;

-- The first hide wins: re-hiding keeps the original time and reason.
-- name: HideComment :exec
UPDATE token_comments
SET hidden_at = COALESCE(hidden_at, now()),
    hidden_reason = COALESCE(hidden_reason, sqlc.arg(reason)::text)
WHERE id = sqlc.arg(comment_id)
  AND chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id);

-- name: UnhideComment :exec
UPDATE token_comments
SET hidden_at = NULL, hidden_reason = NULL
WHERE id = sqlc.arg(comment_id)
  AND chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id);

-- name: ResolveCommentReports :execrows
UPDATE comment_reports
SET resolved_at = now(), resolution = sqlc.arg(resolution)::text
WHERE comment_id = sqlc.arg(comment_id)
  AND chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id)
  AND resolved_at IS NULL;

-- name: ResolveAuthorReports :execrows
UPDATE comment_reports AS r
SET resolved_at = now(), resolution = sqlc.arg(resolution)::text
WHERE r.chain_id = sqlc.arg(chain_id)
  AND r.deployment_id = sqlc.arg(deployment_id)
  AND r.resolved_at IS NULL
  AND r.comment_id IN (
      SELECT c.id
      FROM token_comments AS c
      WHERE c.chain_id = sqlc.arg(chain_id)
        AND c.deployment_id = sqlc.arg(deployment_id)
        AND c.privy_did = sqlc.arg(privy_did)
  );

-- True when the DID has a user row or at least one comment in scope.
-- name: SocialDIDKnown :one
SELECT (EXISTS (
            SELECT 1 FROM social_users AS u
            WHERE u.chain_id = sqlc.arg(chain_id) AND u.deployment_id = sqlc.arg(deployment_id)
              AND u.privy_did = sqlc.arg(privy_did))
        OR EXISTS (
            SELECT 1 FROM token_comments AS c
            WHERE c.chain_id = sqlc.arg(chain_id) AND c.deployment_id = sqlc.arg(deployment_id)
              AND c.privy_did = sqlc.arg(privy_did)))::boolean AS known;

-- Creates the user row if needed. An existing ban keeps its original time and reason.
-- name: BanSocialUser :exec
INSERT INTO social_users (chain_id, deployment_id, privy_did, banned_at, ban_reason)
VALUES (sqlc.arg(chain_id), sqlc.arg(deployment_id), sqlc.arg(privy_did), now(), sqlc.arg(reason)::text)
ON CONFLICT (chain_id, deployment_id, privy_did) DO UPDATE
SET banned_at = COALESCE(social_users.banned_at, EXCLUDED.banned_at),
    ban_reason = COALESCE(social_users.ban_reason, EXCLUDED.ban_reason),
    updated_at = now();

-- name: UnbanSocialUser :execrows
UPDATE social_users
SET banned_at = NULL, ban_reason = NULL, updated_at = now()
WHERE chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id)
  AND privy_did = sqlc.arg(privy_did);

-- name: ListAuthorCommentTokens :many
SELECT DISTINCT token_address
FROM token_comments
WHERE chain_id = sqlc.arg(chain_id)
  AND deployment_id = sqlc.arg(deployment_id)
  AND privy_did = sqlc.arg(privy_did)
ORDER BY token_address;

-- name: InsertModerationAction :exec
INSERT INTO moderation_actions (chain_id, deployment_id, action, comment_id, target_did, operator, reason)
VALUES (sqlc.arg(chain_id), sqlc.arg(deployment_id), sqlc.arg(action), sqlc.narg(comment_id),
        sqlc.narg(target_did), sqlc.arg(operator), sqlc.arg(reason));
