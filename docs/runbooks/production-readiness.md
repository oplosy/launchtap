# Production readiness and handoff

This is the operator handoff for production release. It separates repository evidence from
inputs that only the product owner, infrastructure owner, Privy organization, governance
signers, or an external auditor can provide. No placeholder value in this document is a
production approval.

## Repository evidence already available

Before requesting production approval, record the exact commit and successful run IDs for:

```powershell
node scripts/verify-release.mjs --target=production
cd contracts
pwsh ./scripts/check.ps1 release
cd ..\backend
go run github.com/go-task/task/v3/cmd/task@v3.53.1 verify
```

The release gate must run against a reviewed manifest. It must not use an Anvil manifest,
fixture credentials, a guessed origin, or a public value copied from another chain.

## Human-owned input sheet

Fill this sheet in the release ticket, not in source control. Every value must have an owner
and evidence link before the release gate is run.

| Input | Required evidence | Owner / status |
| --- | --- | --- |
| Reviewed production deployment manifest | committed manifest digest and receipt/explorer evidence | `<owner>` / `<pending>` |
| `NEXT_PUBLIC_PRIVY_APP_ID` | Privy dashboard app ID for the reviewed organization | `<owner>` / `<pending>` |
| Privy production web origin | dashboard allowlist screenshot/export and tested origin | `<owner>` / `<pending>` |
| `PRIVY_APP_ID` | backend app identifier matching the dashboard | `<owner>` / `<pending>` |
| `PRIVY_VERIFICATION_KEY` | key retrieved through the approved secret manager | `<owner>` / `<pending>` |
| `NEXT_PUBLIC_API_BASE_URL` | deployed HTTPS API origin and certificate ownership | `<owner>` / `<pending>` |
| `NEXT_PUBLIC_RPC_URL` | approved HTTPS RPC endpoint, rate and outage contact | `<owner>` / `<pending>` |
| `API_ALLOWED_ORIGINS` | exact production web origin(s), no wildcard | `<owner>` / `<pending>` |
| Database URL | secret-manager reference and backup/restore owner | `<owner>` / `<pending>` |
| Hosting/domain/CSP owner | DNS, TLS, edge rate-limit, and deployment rollback ownership | `<owner>` / `<pending>` |
| Monitoring owner | alert destination, on-call rota, and severity policy | `<owner>` / `<pending>` |
| ETH/USD adapter configuration | selected provider account, key, attribution, and freshness owner | `<owner>` / `<pending>` |
| Pause authority | multisig address, signer set, threshold, and tested pause handoff | `<owner>` / `<pending>` |
| Timelock | address, delay, proposer/executor roles, and acceptance test | `<owner>` / `<pending>` |
| Protocol treasury | address, custody owner, and transfer verification | `<owner>` / `<pending>` |
| External audit | signed report, scope, commit/artifact digest, and accepted findings | `<owner>` / `<pending>` |
| Legal/geo policy | approved jurisdiction policy and enforcement owner | `<owner>` / `<pending>` |

Never put private keys, seed phrases, bearer tokens, database passwords, credentialed RPC URLs,
or Privy verification keys in this repository or in browser-visible `NEXT_PUBLIC_*` variables.

## Privy dashboard checklist

The Privy organization owner must complete and record:

1. Create/select the production app and record its public app ID.
2. Add the exact HTTPS web origin; do not add localhost or wildcard origins.
3. Enable only the wallet login methods approved by the product/security review.
4. Verify embedded and external wallet linking against the backend's linked-wallet proof flow.
5. Store the backend verification key in the production secret manager and test key rotation.
6. Confirm logout, wrong-chain, disconnected, and unauthorized metadata actions in a live
   staging-like environment.

The browser receives only `NEXT_PUBLIC_PRIVY_APP_ID`; the verification key never reaches the
browser, logs, URL, analytics, or persistent client storage.

## Governance handoff

The signer owner must provide the final pause multisig, timelock, and treasury addresses and
the evidence that each owner has accepted the role. The deployment script validates that the
deployer does not retain pause or timelock authority, but it cannot choose or approve the
signer set. Record:

- multisig address, chain ID, threshold, signer addresses, and recovery process;
- timelock address, delay, proposer/executor roles, and emergency procedure;
- protocol treasury address and accounting owner;
- a successful read-only role check and a controlled test of pause/timelock handoff;
- the transaction hashes and explorer links for the final role configuration.

Do not accept mainnet funds until the external audit and governance owners have signed off.
Contract rollback is not a database rollback: the contracts are non-upgradeable. The planned
emergency action is an approved pause/governance procedure, followed by a forward contract or
application release if required.

## Release, health, rollback, and monitoring

Before promotion, the release owner records the manifest digest and runs the production gate
with the values supplied through the CI secret/variable manager. After promotion, the operator
checks:

```powershell
Invoke-WebRequest "$webOrigin" -UseBasicParsing
Invoke-WebRequest "$apiOrigin/healthz" -UseBasicParsing
Invoke-WebRequest "$indexerHealthOrigin/healthz" -UseBasicParsing
Invoke-WebRequest "$apiOrigin/v1/tokens?limit=1" -UseBasicParsing
```

The response must be successful, the chain/deployment metadata must match the manifest, and
the logs must contain no credentials, cookies, signatures, private RPC material, or raw access
tokens. Confirm indexer head/finality lag, database connectivity, RPC error rate, HTTP 5xx,
write rejection rate, and notification/reconciliation activity against the agreed alert policy.

The rollback owner first stops or pauses new transaction-capable traffic when compatibility is
uncertain, then rolls the web/API/indexer deployment back to the last passing commit. Do not
roll back a database migration independently; deploy a forward-compatible fix. Preserve the
incident timeline, manifest digest, gate output, health responses, and scrubbed request IDs.

## Monitoring

Metrics, alerting and the optional Prometheus/Alertmanager stack are opt-in. Error tracking
(Sentry or similar) was deferred by the owner and is not part of this package.

Enable the stack from `deploy/`:

```sh
docker compose --env-file .env --profile monitoring up -d
```

- **Alert destination.** Set `ALERTMANAGER_WEBHOOK_URL_FILE` in `.env` to a file outside the
  repository that holds only the receiver webhook URL. Alertmanager runs as `nobody`
  (uid 65534), so that file must be readable by that user. If the variable is unset the
  profile uses `deploy/monitoring/webhook-url.example`, a no-op receiver, and alerts go
  nowhere. Naming the monitoring owner and the real destination stays in the production
  inputs above.
- **Prometheus access.** Only `127.0.0.1:9090` is published. Reach it from the host, or from a
  workstation through an SSH tunnel such as `ssh -L 9090:127.0.0.1:9090 USER@HOST`. The
  proxy never serves it.
- **Retention.** Prometheus keeps 15 days of data, capped at 2GB, on the named volume
  `prometheus_data`. Do not remove it with `docker compose down --volumes`.
- **Metric endpoints.** API metrics are served on `API_METRICS_ADDR` (`:8082` in Compose) and
  are never routed through Caddy or published. Indexer metrics are on its health port `:8081`
  at `/metrics`.
- **Rate limiting.** The API rate limiter is per process and in memory. One API instance is
  the supported topology; running several multiplies the effective limits until shared
  limiting is added.

Alert rules (`deploy/monitoring/alerts.yml`, unit-tested in `alerts.test.yml`):

| Alert | Condition | For | Severity |
|-------|-----------|-----|----------|
| `TargetDown` | `up{job=~"api\|indexer"} == 0` | 2m | page |
| `IndexerNotReady` | indexer writer-lock ownership is 0, or chain RPC unhealthy | 5m | page |
| `IndexerObservedLagHigh` | chain head minus observed block > 300 blocks | 5m | page |
| `IndexerFinalizedLagHigh` | finalized block lag > 1800s | 10m | ticket |
| `RPCErrorRateHigh` | error or timeout share of chain RPC calls > 5% (10m) | 10m | ticket |
| `API5xxRateHigh` | 5xx share of API requests > 1% (10m), and at least 1 request/s | 10m | page |
| `DatabaseDown` | indexer DB check down, or API pool had to make acquires wait (empty acquires rising) with no idle connection | 2m | page |
| `WriteRejectionSpike` | 5m rejection rate > 10x the 1h average ending 15m ago, and > 0.05/s | 15m | ticket |
| `EthUsdStale` | last ETH/USD refresh older than 600s (absent series: price source disabled, no alert) | 10m | ticket |

- `IndexerObservedLagHigh` fires during the initial backfill from `StartBlock`; silence it
  during first sync.
- `WriteRejectionSpike` deliberately differs from the spec text: it compares against the 1h
  average ending 15m ago (`[1h] offset 15m`) because a plain 1h average includes the surge
  itself and could never fire. It also fires only above 0.05 rejections/s (3 per minute) so a
  zero baseline does not produce noise; the owner can retune that floor.
- `RPCErrorRateHigh` counts an unsupported safe or finalized block tag as an
  `eth_getBlockByNumber` error, so a chain without those tags will raise it.

### Live feed (P0-B)

The API publishes trades, graduations and launches over the existing SSE stream
(`GET /v1/events`) and `GET /v1/trades/recent`. Four API metrics (on `API_METRICS_ADDR`)
describe it:

| Metric | Meaning |
|--------|---------|
| `launchpad_api_feed_events_published_total{type}` | feed events handed to the hub, by event type |
| `launchpad_api_feed_bursts_total` | trade bursts: more than 20 trade events/s in one API process, the overflow becomes one `trade_burst` event |
| `launchpad_api_feed_tail_lag_seconds` | seconds since the block time of the newest published trade |
| `launchpad_api_feed_read_errors_total{op}` | feed database read failures; `op` is `newest`, `trades`, `graduations`, `launches` or `watermarks` |

- Each SSE subscriber has a buffer of 64 events. A subscriber that falls behind is evicted and
  reconnects, then refills from `GET /v1/trades/recent`. Frequent client reconnects point at
  slow clients or an event storm, not at the database.
- The web app opens one shared SSE stream per browser tab on every route. Five or more tabs from
  one client address exceed `API_SSE_MAX_PER_CLIENT` (default 4); the extra tabs fall back to
  slow retries. Raise the limit only if shared NATs make this common.
- Web server-side reads (share-card metadata and images) use the private `API_INTERNAL_BASE_URL`
  (Compose: `http://api:8080`, http or https only). It is required when
  `NEXT_PUBLIC_API_BASE_URL` is a loopback address, and the container preflight rejects the
  deployment otherwise. `NEXT_PUBLIC_WEB_ORIGIN` also drives share links and canonical URLs.
- Share cards (`/token/<address>/opengraph-image` and the Twitter image) are cached in memory
  per web process for 300 s (`experimental.isrFlushToDisk=false`, because the container root
  filesystem is read-only). A generic card served during an API outage, or for a token seconds
  after launch, can stay cached up to 300 s, plus any downstream stale-while-revalidate window.
  Card text shows only characters the bundled font covers (emoji and CJK are stripped; the card
  falls back to `$SYMBOL`), so the renderer never contacts a third party.
- Notifications are in-app and session-only (watched tokens and the signed-in wallet's own
  trades). They exist only where the wallet stack is loaded (token, create and profile routes,
  then for the rest of the tab). Matching on wallet holdings is sub-project C.

### Wallet positions (P0-C1)

The indexer's second stats worker recomputes wallet positions (cost basis and realized PnL)
from the `position_dirty` queue. Three indexer metrics (on its health port `:8081`, `/metrics`)
describe it; no address is ever a label:

| Metric | Meaning |
|--------|---------|
| `launchpad_aggregation_position_failures_total` | position recomputations that failed; the pair stays dirty and is retried |
| `launchpad_aggregation_position_dirty_backlog` | pending `position_dirty` rows for the chain |
| `launchpad_indexer_sender_lookup_misses_total` | pool swap transactions stored with `tx_from = NULL` because the sender lookup failed, errored for that hash, or returned `null` |

- No alert rule ships for these. Suggested policy for the monitoring owner: warn when
  `launchpad_aggregation_position_dirty_backlog > 500` for 15 minutes, or when
  `increase(launchpad_aggregation_position_failures_total[15m]) > 0` keeps rising. Retune the
  threshold to the chain's trade rate; a large backlog right after a backfill is expected and
  drains on its own.
- A growing backlog or rising failures mean wallet PnL and the leaderboard are stale, not wrong.
  Check the indexer log for `position compute failed`.
- Sender misses are not retried by the indexer. Each one leaves a swap with `tx_from = NULL`,
  which turns `attribution_complete` false for every wallet on the chain until it is filled.
  Suggested alert: `increase(launchpad_indexer_sender_lookup_misses_total[15m]) > 0`. When it
  fires, check the indexer log for `pool swap sender lookup missed` (one warning per batch, with
  the miss count) and run `cmd/backfill-swap-sender` (see "Backfill pool swap senders (P0-C1)"
  below) once the RPC is healthy. A lagging load-balanced node returning `null` for a
  just-mined transaction is the usual cause; the backfill resolves those later.
- After a backfill or an outage, the backlog should return to 0. See "Backfill pool swap senders
  (P0-C1)" below for the `cmd/backfill-swap-sender` command and `attribution_complete`.

### Backfill pool swap senders (P0-C1)

Wallet positions attribute a pool swap to the transaction sender (`pool_swaps.tx_from`). The
indexer stores `NULL` when the sender lookup fails, and swaps ingested before this feature have
`NULL` too. Fill them with the one-off command, run from `backend/` with the indexer's
environment (`DATABASE_URL`, `RPC_URL`, `CHAIN_ID`, `DEPLOYMENT_ID`):

```bash
go run ./cmd/backfill-swap-sender -dry-run   # fetch and log senders, write nothing
go run ./cmd/backfill-swap-sender            # fill and mark positions dirty
```

| Flag | Default | Meaning |
|------|---------|---------|
| `-rate` | `5` | RPC requests per second (one request per batch) |
| `-batch` | `50` | transaction hashes per RPC call, 1 to 100 |
| `-dry-run` | off | fetch and log what would change, write nothing (with `-seed-positions`: count pairs only) |
| `-seed-positions` | off | queue every existing ledger pair for position recompute instead of filling senders (see below) |

- The command is resumable: `tx_from IS NULL` is the cursor, so a rerun touches only what is
  still missing. Each batch is one database transaction that fills the senders and marks the
  affected `(sender, token)` positions dirty; the indexer's position worker then recomputes them.
- A transaction the RPC cannot resolve is logged and skipped for that run, and the command exits
  non-zero after finishing. Rerun later (or against a better RPC) to retry the skipped ones.
- Sender fetch failures during normal ingestion never stop the indexer; they only leave `NULL`.

Seeding positions from existing history (upgrade procedure). The position queue starts empty,
and ingestion marks only new events, so on a database that already holds trades, transfers or
pool swaps from before this feature, every wallet that has not traded since has no position.
When upgrading such a database:

1. Apply the migrations and start the indexer as usual.
2. Run the sender backfill above until it exits zero.
3. Seed the queue from the whole ledger, with the same environment:

   ```bash
   go run ./cmd/backfill-swap-sender -seed-positions -dry-run   # count pairs, write nothing
   go run ./cmd/backfill-swap-sender -seed-positions            # queue every pair
   ```

   It queues every distinct `(wallet, token)` from curve trades (by trader), transfers (both
   sides) and attributed pool swaps, excluding the zero address, the factory and every curve and
   pair. It is one database statement, needs no RPC, and is safe to rerun (a queued pair only
   gets a new generation). It logs `pairs` and `marked`.
4. Watch `launchpad_aggregation_position_dirty_backlog` drain to 0; wallet pages and the
   leaderboard are complete after that. A fresh database (indexed from the start with this
   feature) needs no seed.

Reading `attribution_complete`: `GET /v1/wallets/{address}` returns `attribution_complete`,
chain-wide. It is `true` only when no pool swap on the chain has `tx_from IS NULL`. While it is
`false`, DEX trades may be missing from every wallet's PnL and trade lists. Check progress with
`SELECT count(*) FROM pool_swaps WHERE chain_id = <chain> AND tx_from IS NULL;`.
`attribution_complete` depends only on that count: it is `true` as soon as the count is 0 (the
backfill exits zero), independent of the `position_dirty` queue. Wallet PnL is current only once
the position worker has also drained the queue (`launchpad_aggregation_position_dirty_backlog`
back to 0).

## Audit evidence checklist

The audit packet must identify the exact commit and contain the Solidity source/artifact
digests, Foundry/solc versions, invariant and fuzz results, pinned Robinhood fork evidence,
deployment receipt and runtime-code hashes, manifest digest, governance role evidence,
reproducible release-gate output, threat-model findings, accepted-risk register, and the
external auditor's signed report. A green CI run is not an audit approval.

## Final approval rule

Production is not approved while any row in the input sheet is `<pending>`, the testnet or
production manifest is disabled/unreviewed, the Privy origin is unverified, the rollback and
monitoring owners are unnamed, or the external audit/governance evidence is absent.
