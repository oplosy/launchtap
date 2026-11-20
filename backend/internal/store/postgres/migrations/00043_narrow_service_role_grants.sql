-- +goose Up
-- Narrow what 00039 granted (docs/specs/2026-11-20-narrow-role-grants-preflight.md).
--
-- Reads: link codes are bearer secrets and moderation state belongs to the moderator, so only
-- the roles whose queries touch these five tables can read them. Every other table stays readable
-- by every role.
REVOKE SELECT ON telegram_link_codes, telegram_links, social_users, comment_reports,
    moderation_actions FROM launchpad_indexer_role, launchpad_funding_role;
REVOKE SELECT ON social_users, comment_reports, moderation_actions FROM launchpad_telegram_role;
REVOKE SELECT ON telegram_link_codes, telegram_links FROM launchpad_moderate_role;

-- Writes: the API inserts users, comments and reports and changes a username. It never writes
-- the moderation columns (banned_at, ban_reason, hidden_at, hidden_reason, resolved_at,
-- resolution) and never deletes these rows; only the moderate role does. Table-level privileges
-- are revoked first, then exactly the columns the API's queries name are granted back.
REVOKE INSERT, UPDATE, DELETE ON social_users, token_comments, comment_reports
    FROM launchpad_api_role;
GRANT INSERT (chain_id, deployment_id, privy_did),
    UPDATE (username, username_changed_at, updated_at)
    ON social_users TO launchpad_api_role;
GRANT INSERT (chain_id, deployment_id, token_address, privy_did, wallet, author_role, body)
    ON token_comments TO launchpad_api_role;
GRANT INSERT (comment_id, chain_id, deployment_id, reporter_did, reason)
    ON comment_reports TO launchpad_api_role;

-- A report or vote waits for an in-flight hide or ban and then re-reads the committed state. The
-- row lock that does this needs UPDATE privilege, which the API no longer has on these tables, so
-- it is taken here, owned by the migration role, and the API gets nothing but EXECUTE (the
-- lock_token_launch pattern from 00039).
-- +goose StatementBegin
CREATE FUNCTION lock_reportable_comment(p_comment_id BIGINT, p_chain_id BIGINT, p_deployment_id TEXT)
    RETURNS VOID
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
    PERFORM 1
    FROM public.token_comments AS c
    JOIN public.social_users AS u
      ON u.chain_id = c.chain_id AND u.deployment_id = c.deployment_id AND u.privy_did = c.privy_did
    WHERE c.id = p_comment_id
      AND c.chain_id = p_chain_id
      AND c.deployment_id = p_deployment_id
    FOR SHARE OF c, u;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION lock_reportable_comment(BIGINT, BIGINT, TEXT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lock_reportable_comment(BIGINT, BIGINT, TEXT) TO launchpad_api_role;

-- +goose Down
DROP FUNCTION lock_reportable_comment(BIGINT, BIGINT, TEXT);
-- Revoking the table privilege also revokes the column grants above.
REVOKE INSERT, UPDATE ON social_users, token_comments, comment_reports FROM launchpad_api_role;
GRANT INSERT, UPDATE, DELETE ON social_users, token_comments, comment_reports
    TO launchpad_api_role;
GRANT SELECT ON telegram_link_codes, telegram_links TO launchpad_moderate_role;
GRANT SELECT ON social_users, comment_reports, moderation_actions TO launchpad_telegram_role;
GRANT SELECT ON telegram_link_codes, telegram_links, social_users, comment_reports,
    moderation_actions TO launchpad_indexer_role, launchpad_funding_role;
