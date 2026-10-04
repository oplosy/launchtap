# Production deployment package

This package targets one Linux Docker host with two DNS names and automatic HTTPS.
It is infrastructure preparation, not production approval. It does not deploy contracts,
assign governance roles, satisfy the external audit, or replace live acceptance evidence.
Public configuration is baked into the web image: rebuild when any public value changes.
Preflight checks both the reviewed production manifest and equality with the configuration
recorded inside the image. Changing runtime environment values cannot retarget a built client.

## Required inputs

Use the reviewed commit as `RELEASE_TAG`. Copy `.env.example` to `.env` and fill it with
the real public inputs. Keep `BACKEND_ENV_FILE` and `POSTGRES_PASSWORD_FILE` outside the
checkout, mode `0600`; populate the former from `backend.env.example`. The backend
Privy app ID must match the public app ID. Never place wallet keys in these files.
The database password file contains only the password and an optional trailing newline.
The backend DSN uses that same password, percent-encoded. Local container database traffic
uses the private Compose network; PostgreSQL has no published port.

The reviewed production manifest must first be committed and generated into the backend
registry and web contracts. A dependency configuration is not a deployment manifest.
The existing production validator rejects testnet/Anvil configuration and unknown or
disabled manifests. No production manifest or domain is invented by this package.

Before public deployment, point both DNS names at the host, allow TCP 80/443, configure
the exact HTTPS web origin in Privy, complete governance/audit sign-off, and run the root
production release gate in a clean checkout with the same public configuration.
Use the input/evidence checklist in `docs/runbooks/production-readiness.md`.
For the root production release gate, export the same `NEXT_PUBLIC_*` values used for
the image, including `NEXT_PUBLIC_API_BASE_URL=https://API_DOMAIN` and
`NEXT_PUBLIC_WEB_ORIGIN=https://WEB_DOMAIN`. The Compose `.env` file is not automatically
loaded by the root Node.js gate. Never export backend secrets into the web build environment.

## Build and promote

Run from this directory; do not print `docker compose config` because it resolves secrets.

```sh
docker compose --env-file .env config --quiet
docker compose --env-file .env build --pull
docker compose --env-file .env run --rm --no-deps preflight
docker compose --env-file .env up -d
docker compose --env-file .env ps
curl --fail https://YOUR_API_DOMAIN/healthz
curl --fail https://YOUR_API_DOMAIN/readyz
curl --fail 'https://YOUR_API_DOMAIN/v1/tokens?limit=1'
curl --fail http://127.0.0.1:8081/healthz
```

Preflight completes before migrations; migration failure prevents API/indexer startup.
Only the proxy publishes public ports. The indexer health port binds to loopback.
Container liveness does not prove current chain data: inspect observed/safe/finalized
timestamps, lag, RPC health, ownership, and errors before enabling transaction traffic.
Set external uptime and lag alerts with a named recipient; Docker health checks alone
do not deliver alerts. Confirm backup restore and record the manifest/commit/image IDs.
The standard Caddy image supplies TLS and proxying, not a per-client rate-limit policy.
Configure the approved edge rate limits before exposing transaction-capable production traffic.

## Backup and recovery

Stop metadata writes and take an encrypted off-host backup according to the agreed
operator policy. Shell redirection below is for a Linux host, not Windows PowerShell.

```sh
umask 077
docker compose --env-file .env exec -T postgres pg_dump -U launchpad -d launchpad -Fc > launchpad.dump
docker compose --env-file .env exec -T postgres pg_restore --list < launchpad.dump
```

Test restoration into a separate empty database before promotion. Preserve PostgreSQL
and Caddy volumes across upgrades; never run `docker compose down --volumes` as rollback.
PostgreSQL 18 stores its persistent cluster under `/var/lib/postgresql`.

For an application rollback, preserve the database, set `RELEASE_TAG` to the previous
validated image tag, and run `docker compose up -d --no-build --no-deps api indexer web`.
Do not roll back a migration independently. Check forward compatibility before rollback;
otherwise stop transaction traffic and release a forward fix. Contracts are non-upgradeable.

## Current boundary

Run the disposable local package check with PowerShell 7 and Docker:

```sh
pwsh -NoProfile -File deploy/check.ps1 -Build
```

The `deployment package` workflow runs this same check on relevant dev pushes and milestone
PRs. It supplies no production credentials and does not publish images or promote a release.

It builds images with empty public configuration, starts a disposable PostgreSQL/API/web
stack bound only to loopback, verifies migration and static asset delivery, checks that an
unindexed database is unready and that production preflight rejects the unconfigured image,
then removes only its own containers and network. It does not prove production activation,
certificate issuance, mainnet RPC behavior, external authentication, or monitoring delivery.
The check also restores a database dump into a separate empty database and verifies its
migration version. Operators still need encrypted off-host retention and recovery ownership.

The 2026-10-04 dependency check removed the reported critical/high runtime advisories using
Next.js 16.3.8, Axios 1.20.0, and ws 8.21.3 (only ws 8.x is overridden; 7.x is preserved).
`npm audit --omit=dev` still reports 39 lower-severity advisories (9 low, 30 moderate).
These require dependency/upstream impact review before production approval; this is not an
external audit report. Advisory counts are time-specific and must be refreshed at promotion.

Without hosting/DNS, a reviewed mainnet manifest, production Privy inputs, monitoring
ownership, governance evidence, and an external audit, this package must not be promoted.
The live testnet graduation exercise is deferred by the product owner because the faucet
limit cannot fund the 4.2 ETH threshold. This is a deferral, not a passed acceptance test.
