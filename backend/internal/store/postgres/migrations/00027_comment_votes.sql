-- +goose Up
-- Comment votes (docs/specs/2026-11-12-comment-votes-design.md): one +1/-1 vote per account per
-- comment. Scores are summed at read time over voters who are not banned.
CREATE TABLE comment_votes (
    comment_id BIGINT NOT NULL,
    chain_id BIGINT NOT NULL,
    deployment_id TEXT NOT NULL,
    voter_did TEXT NOT NULL,
    value SMALLINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT comment_votes_pkey PRIMARY KEY (comment_id, voter_did),
    CONSTRAINT comment_votes_comment_fk FOREIGN KEY (comment_id) REFERENCES token_comments (id) ON DELETE CASCADE,
    CONSTRAINT comment_votes_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT comment_votes_deployment_id_length CHECK (octet_length(deployment_id) > 0),
    CONSTRAINT comment_votes_voter_did_length CHECK (octet_length(voter_did) BETWEEN 1 AND 255),
    CONSTRAINT comment_votes_value_valid CHECK (value IN (-1, 1))
);

-- The per-account hourly limit counts the caller's recently set votes.
CREATE INDEX comment_votes_voter_updated_idx
    ON comment_votes (chain_id, deployment_id, voter_did, updated_at DESC);

-- +goose Down
-- Down deletes every vote.
DROP TABLE comment_votes;
