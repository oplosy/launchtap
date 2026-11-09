-- +goose Up
-- Social layer: usernames, published wallets, token comments, reports and moderation log.
-- Identity is (chain_id, deployment_id, privy_did). token_comments has no FK to tokens on purpose
-- (a comment may outlive its token row after a reorg). No amounts are stored.
CREATE TABLE social_users (
    chain_id BIGINT NOT NULL,
    deployment_id TEXT NOT NULL,
    privy_did TEXT NOT NULL,
    username TEXT,
    username_changed_at TIMESTAMPTZ,
    banned_at TIMESTAMPTZ,
    ban_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT social_users_pkey PRIMARY KEY (chain_id, deployment_id, privy_did),
    CONSTRAINT social_users_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT social_users_deployment_id_length CHECK (octet_length(deployment_id) > 0),
    CONSTRAINT social_users_privy_did_length CHECK (octet_length(privy_did) BETWEEN 1 AND 255),
    CONSTRAINT social_users_username_format CHECK (username ~ '^[a-z0-9_]{3,20}$'),
    CONSTRAINT social_users_username_changed_at_set CHECK (username IS NULL OR username_changed_at IS NOT NULL),
    CONSTRAINT social_users_ban_pair CHECK ((banned_at IS NULL) = (ban_reason IS NULL)),
    CONSTRAINT social_users_username_unique UNIQUE (chain_id, deployment_id, username)
);

CREATE TABLE username_releases (
    chain_id BIGINT NOT NULL,
    deployment_id TEXT NOT NULL,
    username TEXT NOT NULL,
    privy_did TEXT NOT NULL,
    released_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT username_releases_pkey PRIMARY KEY (chain_id, deployment_id, username, privy_did),
    CONSTRAINT username_releases_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT username_releases_deployment_id_length CHECK (octet_length(deployment_id) > 0),
    CONSTRAINT username_releases_privy_did_length CHECK (octet_length(privy_did) BETWEEN 1 AND 255),
    CONSTRAINT username_releases_username_format CHECK (username ~ '^[a-z0-9_]{3,20}$')
);

CREATE TABLE social_public_wallets (
    chain_id BIGINT NOT NULL,
    deployment_id TEXT NOT NULL,
    wallet BYTEA NOT NULL,
    privy_did TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT social_public_wallets_pkey PRIMARY KEY (chain_id, deployment_id, wallet),
    CONSTRAINT social_public_wallets_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT social_public_wallets_deployment_id_length CHECK (octet_length(deployment_id) > 0),
    CONSTRAINT social_public_wallets_wallet_length CHECK (octet_length(wallet) = 20),
    CONSTRAINT social_public_wallets_privy_did_length CHECK (octet_length(privy_did) BETWEEN 1 AND 255)
);

CREATE INDEX social_public_wallets_owner_idx
    ON social_public_wallets (chain_id, deployment_id, privy_did);

CREATE TABLE token_comments (
    id BIGINT GENERATED ALWAYS AS IDENTITY,
    chain_id BIGINT NOT NULL,
    deployment_id TEXT NOT NULL,
    token_address BYTEA NOT NULL,
    privy_did TEXT NOT NULL,
    wallet BYTEA NOT NULL,
    author_role TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    hidden_at TIMESTAMPTZ,
    hidden_reason TEXT,
    CONSTRAINT token_comments_pkey PRIMARY KEY (id),
    CONSTRAINT token_comments_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT token_comments_deployment_id_length CHECK (octet_length(deployment_id) > 0),
    CONSTRAINT token_comments_token_address_length CHECK (octet_length(token_address) = 20),
    CONSTRAINT token_comments_privy_did_length CHECK (octet_length(privy_did) BETWEEN 1 AND 255),
    CONSTRAINT token_comments_wallet_length CHECK (octet_length(wallet) = 20),
    CONSTRAINT token_comments_author_role_valid CHECK (author_role IN ('creator', 'holder')),
    CONSTRAINT token_comments_body_length CHECK (char_length(body) BETWEEN 1 AND 500),
    CONSTRAINT token_comments_hidden_pair CHECK ((hidden_at IS NULL) = (hidden_reason IS NULL))
);

CREATE INDEX token_comments_public_list_idx
    ON token_comments (chain_id, deployment_id, token_address, id DESC)
    WHERE hidden_at IS NULL;

CREATE INDEX token_comments_author_created_idx
    ON token_comments (chain_id, deployment_id, privy_did, created_at DESC);

CREATE TABLE comment_reports (
    comment_id BIGINT NOT NULL,
    chain_id BIGINT NOT NULL,
    deployment_id TEXT NOT NULL,
    reporter_did TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    resolution TEXT,
    CONSTRAINT comment_reports_pkey PRIMARY KEY (comment_id, reporter_did),
    CONSTRAINT comment_reports_comment_fk FOREIGN KEY (comment_id) REFERENCES token_comments (id) ON DELETE CASCADE,
    CONSTRAINT comment_reports_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT comment_reports_deployment_id_length CHECK (octet_length(deployment_id) > 0),
    CONSTRAINT comment_reports_reporter_did_length CHECK (octet_length(reporter_did) BETWEEN 1 AND 255),
    CONSTRAINT comment_reports_reason_valid CHECK (reason IN ('spam', 'scam', 'abuse', 'other')),
    CONSTRAINT comment_reports_resolution_valid CHECK (resolution IN ('dismissed', 'actioned')),
    CONSTRAINT comment_reports_resolved_pair CHECK ((resolved_at IS NULL) = (resolution IS NULL))
);

CREATE INDEX comment_reports_reporter_created_idx
    ON comment_reports (chain_id, deployment_id, reporter_did, created_at DESC);

CREATE INDEX comment_reports_open_idx
    ON comment_reports (chain_id, deployment_id, created_at)
    WHERE resolved_at IS NULL;

-- Append-only audit log of operator actions.
CREATE TABLE moderation_actions (
    id BIGINT GENERATED ALWAYS AS IDENTITY,
    chain_id BIGINT NOT NULL,
    deployment_id TEXT NOT NULL,
    action TEXT NOT NULL,
    comment_id BIGINT,
    target_did TEXT,
    operator TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT moderation_actions_pkey PRIMARY KEY (id),
    CONSTRAINT moderation_actions_chain_id_positive CHECK (chain_id > 0),
    CONSTRAINT moderation_actions_deployment_id_length CHECK (octet_length(deployment_id) > 0),
    CONSTRAINT moderation_actions_action_valid CHECK (action IN ('hide_comment', 'unhide_comment', 'ban_user', 'unban_user', 'resolve_reports')),
    CONSTRAINT moderation_actions_operator_length CHECK (octet_length(operator) BETWEEN 1 AND 64)
);

-- The single SQL source for "which username may be shown for this wallet": published wallets of
-- users that have a username and are not banned.
CREATE VIEW public_wallet_usernames AS
SELECT w.chain_id, w.deployment_id, w.wallet, u.username
FROM social_public_wallets AS w
JOIN social_users AS u
  ON u.chain_id = w.chain_id AND u.deployment_id = w.deployment_id AND u.privy_did = w.privy_did
WHERE u.username IS NOT NULL AND u.banned_at IS NULL;

-- +goose Down
-- Down deletes all social data: usernames, published wallets, comments, reports and the
-- moderation log.
DROP VIEW public_wallet_usernames;
DROP TABLE moderation_actions;
DROP TABLE comment_reports;
DROP TABLE token_comments;
DROP TABLE social_public_wallets;
DROP TABLE username_releases;
DROP TABLE social_users;
