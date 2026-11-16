# Per-service PostgreSQL roles — pre-flight (2026-11-16)

Production-readiness audit pass 3, M-1: every backend process connects as the `POSTGRES_USER`
superuser, so a compromised API can rewrite the ledger, drop tables or read any row through any
query it can reach.

## Decisions

**1. Group roles and grants live in a migration.** Migration 00039 creates five `NOLOGIN` group
roles if they do not exist and grants them:

| Group role | Reads | Writes (INSERT, UPDATE, DELETE) |
|---|---|---|
| `launchpad_api_role` | every table and view | social and user content: `social_users`, `token_comments`, `comment_votes`, `comment_reports`, `social_public_wallets`, `username_releases`, `token_metadata`, `token_images`, `watchlist_entries`, `notification_reads`, `telegram_link_codes`, `telegram_links` |
| `launchpad_indexer_role` | every table and view | every chain-derived table: blocks, sync state, ledger events, projections, aggregates, reorg bookkeeping, and the `notification_reads` reorg clamp; never user content |
| `launchpad_telegram_role` | every table and view | `telegram_links`, `telegram_link_codes` |
| `launchpad_funding_role` | every table and view | `wallet_funding` |
| `launchpad_moderate_role` | every table and view | `moderation_actions`, `social_users`, `token_comments`, `comment_reports` |

Every writer role also gets `USAGE, SELECT` on the schema's sequences (identity columns).
`funding_excluded_funders` has no runtime writer: migrations own it.

**Rationale.** The grants follow the tables each binary's store types write
(`cmd/<binary>/main.go` → `storepostgres.*Store` → `queries/*.sql`). Reads stay broad: the API
serves almost every table, and the point of M-1 is that a compromised reader cannot write the
ledger or drop schema, not read isolation (every row is public chain data or the user's own).
Grants in a migration keep schema and privileges in one reviewed, versioned place, and every test
database gets them.

**2. Every table must be classified.** `TestServiceRoleGrants` compares the live grants of each
group role with a Go matrix and fails when a table exists that the matrix does not name, so a new
table without a grant decision fails CI instead of production.

**3. Store tests run as their role.** `postgrestest.Database.URLAs(role)` creates a throwaway
`LOGIN` member of a group role. The indexer pipeline, API social/watchlist/metadata/inbox,
telegram and funding integration tests open their pools with it, so a missing grant fails the test
that needs it. Fixtures still use the superuser connection.

**4. Login roles and passwords come from deploy, not the migration.** Migrations never hold
passwords. `deploy/postgres/10-service-roles.sh` (mounted into the Postgres init directory, and
idempotent so operators can run it once on an existing database) creates `launchpad_api`,
`launchpad_indexer` and `launchpad_telegram` with passwords from Docker secrets and makes each a
member of its group role. Funding and moderation logins are created by the operator from the
runbook, because neither runs in the Compose project.

**5. Services get a password-less URL plus a password file.** `DATABASE_PASSWORD_FILE`, when set,
is read at startup and inserted into `DATABASE_URL` (which must then carry no password). Each
Compose service sets its own `DATABASE_URL` and mounts only its own secret; the shared backend env
file no longer carries a database URL, so the API container never sees another role's credential.

**6. Migrations keep the owner.** `migrate` still connects as `POSTGRES_USER`, which owns the
schema and creates the group roles. It is a one-shot job that is not network-exposed; a separate
non-superuser owner is a later hardening step, not part of M-1.

## Alternatives rejected

- `ALTER DEFAULT PRIVILEGES`: grants future tables silently, which defeats decision 2.
- Per-service env files: more files to keep in sync than one secret per service.
- Granting the indexer `ALL` on every table: it would also write user content.
- Dropping the group roles in the down migration: roles are cluster-wide and other databases on the
  same cluster (parallel test databases, a staging database) may still use them.

## Out of scope

Read restrictions per role, row-level security, a non-superuser migration owner, and
connection-level limits per role.
