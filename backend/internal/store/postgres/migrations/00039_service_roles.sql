-- +goose Up
-- Per-service least-privilege group roles (docs/specs/2026-11-16-postgres-service-roles-preflight.md).
-- Login roles and their passwords come from deploy (deploy/postgres/10-service-roles.sh), never a
-- migration. Every table added later needs its grants in its own migration; TestServiceRoleGrants
-- fails on a table the matrix does not classify.

-- +goose StatementBegin
DO $$
DECLARE
    role_name TEXT;
BEGIN
    FOREACH role_name IN ARRAY ARRAY[
        'launchpad_api_role', 'launchpad_indexer_role', 'launchpad_telegram_role',
        'launchpad_funding_role', 'launchpad_moderate_role'
    ] LOOP
        IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = role_name) THEN
            BEGIN
                EXECUTE format('CREATE ROLE %I NOLOGIN', role_name);
            EXCEPTION WHEN duplicate_object OR unique_violation THEN
                -- Roles are cluster-wide: a concurrent migration of another database created it.
                NULL;
            END;
        END IF;
    END LOOP;
END
$$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA public TO launchpad_api_role, launchpad_indexer_role, launchpad_telegram_role,
    launchpad_funding_role, launchpad_moderate_role;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO launchpad_api_role, launchpad_indexer_role,
    launchpad_telegram_role, launchpad_funding_role, launchpad_moderate_role;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO launchpad_api_role, launchpad_indexer_role,
    launchpad_telegram_role, launchpad_funding_role, launchpad_moderate_role;

GRANT INSERT, UPDATE, DELETE ON social_users, token_comments, comment_votes, comment_reports,
    social_public_wallets, username_releases, token_metadata, token_images, watchlist_entries,
    notification_reads, telegram_link_codes, telegram_links TO launchpad_api_role;
GRANT INSERT, UPDATE, DELETE ON telegram_links, telegram_link_codes TO launchpad_telegram_role;
GRANT INSERT, UPDATE, DELETE ON wallet_funding TO launchpad_funding_role;
GRANT INSERT, UPDATE, DELETE ON moderation_actions, social_users, token_comments, comment_reports
    TO launchpad_moderate_role;

-- The indexer writes every chain-derived table, and clamps notification read cursors on a reorg;
-- it never writes user content, the other workers' tables or the migration-owned funder list.
-- +goose StatementBegin
DO $$
DECLARE
    table_name TEXT;
BEGIN
    FOR table_name IN
        SELECT tablename FROM pg_tables
        WHERE schemaname = 'public'
          AND tablename NOT IN (
              'goose_db_version', 'social_users', 'token_comments', 'comment_votes',
              'comment_reports', 'social_public_wallets', 'username_releases', 'token_metadata',
              'token_images', 'watchlist_entries', 'telegram_link_codes', 'telegram_links',
              'wallet_funding', 'moderation_actions', 'funding_excluded_funders'
          )
    LOOP
        EXECUTE format('GRANT INSERT, UPDATE, DELETE ON %I TO launchpad_indexer_role', table_name);
    END LOOP;
END
$$;
-- +goose StatementEnd

-- The API checks a token's creator before a metadata write and must hold the launch row until
-- commit. A row lock needs UPDATE privilege, so it is taken here, owned by the migration role, and
-- the API gets nothing but EXECUTE.
-- +goose StatementBegin
CREATE FUNCTION lock_token_launch(p_chain_id BIGINT, p_token_address BYTEA) RETURNS VOID
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
    PERFORM 1 FROM public.token_launches
    WHERE chain_id = p_chain_id AND token_address = p_token_address
    FOR SHARE;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION lock_token_launch(BIGINT, BYTEA) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lock_token_launch(BIGINT, BYTEA) TO launchpad_api_role;

-- +goose Down
DROP FUNCTION lock_token_launch(BIGINT, BYTEA);
-- The roles stay: they are cluster-wide and other databases on the cluster may still use them.
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM launchpad_api_role, launchpad_indexer_role,
    launchpad_telegram_role, launchpad_funding_role, launchpad_moderate_role;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM launchpad_api_role, launchpad_indexer_role,
    launchpad_telegram_role, launchpad_funding_role, launchpad_moderate_role;
REVOKE USAGE ON SCHEMA public FROM launchpad_api_role, launchpad_indexer_role,
    launchpad_telegram_role, launchpad_funding_role, launchpad_moderate_role;
