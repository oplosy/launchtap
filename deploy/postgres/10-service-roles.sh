#!/bin/sh
# Creates (or updates) the login roles of the long-running backend services and makes each a
# member of its least-privilege group role (docs/specs/2026-11-16-postgres-service-roles-preflight.md).
# Postgres runs it once when the data directory is first initialized. It is idempotent: on an
# existing database run it again with
#   docker compose exec postgres sh /docker-entrypoint-initdb.d/10-service-roles.sh
# Migration 00039 grants the group roles their privileges; until it has run they have none.
set -eu

read_secret() {
	value=$(cat "/run/secrets/$1")
	if [ -z "$value" ]; then
		echo "secret $1 is empty" >&2
		exit 1
	fi
	printf '%s' "$value"
}

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
	-v api_password="$(read_secret postgres_api_password)" \
	-v indexer_password="$(read_secret postgres_indexer_password)" \
	-v telegram_password="$(read_secret postgres_telegram_password)" <<'SQL'
SELECT format('CREATE ROLE %I NOLOGIN', name)
FROM unnest(ARRAY['launchpad_api_role', 'launchpad_indexer_role', 'launchpad_telegram_role',
                  'launchpad_funding_role', 'launchpad_moderate_role']) AS name
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = name) \gexec

SELECT format('CREATE ROLE %I LOGIN', name)
FROM unnest(ARRAY['launchpad_api', 'launchpad_indexer', 'launchpad_telegram']) AS name
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = name) \gexec

ALTER ROLE launchpad_api WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD :'api_password';
ALTER ROLE launchpad_indexer WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD :'indexer_password';
ALTER ROLE launchpad_telegram WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD :'telegram_password';
GRANT launchpad_api_role TO launchpad_api;
GRANT launchpad_indexer_role TO launchpad_indexer;
GRANT launchpad_telegram_role TO launchpad_telegram;
SQL
