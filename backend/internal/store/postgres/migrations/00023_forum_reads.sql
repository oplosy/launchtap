-- +goose Up
-- Forum reads (spec P0-C5): the cross-token feed of visible comments, newest first, and the
-- 24-hour community window. Both are partial on visible comments; bans are applied at read time.
CREATE INDEX token_comments_forum_feed_idx
    ON token_comments (chain_id, deployment_id, id DESC)
    WHERE hidden_at IS NULL;
CREATE INDEX token_comments_forum_recent_idx
    ON token_comments (chain_id, deployment_id, created_at)
    WHERE hidden_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS token_comments_forum_recent_idx;
DROP INDEX IF EXISTS token_comments_forum_feed_idx;
