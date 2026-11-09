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
| `launchpad_api_feed_bursts_total` | feed bursts: more than 20 trade, new_token and graduation events/s in one API process, the trade and new_token overflow becomes one `trade_burst` event (graduations are never folded) |
| `launchpad_api_feed_tail_lag_seconds` | seconds since the block time of the newest published trade |
| `launchpad_api_feed_read_errors_total{op}` | feed database read failures; `op` is `newest`, `trades`, `graduations`, `launches` or `watermarks`. A persistent failure on one of `trades`, `graduations` or `launches` stalls the whole live feed, because the three are published merged in chain order |

- Each SSE subscriber has a buffer of 64 events. A subscriber that falls behind is evicted and
  reconnects, then refills from `GET /v1/trades/recent`. Frequent client reconnects point at
  slow clients or an event storm, not at the database.
- The web app opens one shared SSE stream per browser tab on every route. Five or more tabs from
  one client address exceed `API_SSE_MAX_PER_CLIENT` (default 4); the extra tabs fall back to
  slow retries. To keep background tabs from holding those slots, a tab that stays hidden for
  60 s closes its stream (and does no slow retries while hidden); when it becomes visible again it
  refills from REST, reopens the stream and refetches once the new connection opens, through the
  same path as any reconnect. So the cap is only hit by more than four tabs *visible* (or hidden
  for under a minute) at once. Raise the limit only if shared NATs make this common.
- Web server-side reads (share-card metadata and images) use the private `API_INTERNAL_BASE_URL`
  (Compose: `http://api:8080`, http or https only). It is required when
  `NEXT_PUBLIC_API_BASE_URL` is a loopback address, and the container preflight rejects the
  deployment otherwise. `NEXT_PUBLIC_WEB_ORIGIN` also drives share links and canonical URLs.
- Share cards (`/token/<address>/opengraph-image` and the Twitter image) are cached in memory
  per web process for 300 s (`experimental.isrFlushToDisk=false`, because the container root
  filesystem is read-only). A generic card served during an API outage, or for a token seconds
  after launch, can stay cached up to 300 s, plus any downstream stale-while-revalidate window.
  Wallet cards (`/wallet/<address>/opengraph-image` and the Twitter image) are cached the same
  way. For both kinds, a generic card caused by an API failure (including a 404 for a token not
  indexed yet) is sent with `Cache-Control: public, s-maxage=30`, so caches in front of the web
  server drop it within 30 s (the web process's own route cache still keeps it up to 300 s; Next
  cannot shorten that per response). A failed positions read alone does not degrade the card: it renders without symbols.
  Card text shows only characters the bundled font covers (emoji and CJK are stripped; the card
  falls back to `$SYMBOL`), so the renderer never contacts a third party.
- Share-card render cost: one server-side render (a token card reads the token, its image and the
  ETH/USD reference; a wallet card reads the wallet overview and its open positions) per distinct
  token or wallet address per 300 s revalidation window, per web process; repeat unfurls inside the window are served from the
  cache. The card routes are public and use no API key, so the cost scales with the number of
  distinct addresses requested, which a crawler can drive up; rate-limit the card routes at the
  edge if that becomes visible in API load.
- Notifications are in-app and session-only (watched tokens and the signed-in wallet's own
  trades). They exist only where the wallet stack is loaded (token, create and profile routes,
  then for the rest of the tab). Matching on wallet holdings is sub-project C.

### Wallet positions (P0-C1)

The indexer's second stats worker recomputes wallet positions (cost basis and realized PnL)
from the `position_dirty` queue. These indexer metrics (on its health port `:8081`, `/metrics`)
describe it; no address is ever a label:

| Metric | Meaning |
|--------|---------|
| `launchpad_aggregation_position_failures_total` | position recomputations that failed; the pair stays dirty and is retried |
| `launchpad_aggregation_position_dirty_backlog` | pending `position_dirty` rows for the chain |
| `launchpad_indexer_sender_lookup_misses_total` | pool swap transactions stored with `tx_from = NULL` because the sender lookup failed, errored for that hash, or returned `null`; counted once per committed chunk, so a retried step never double-counts |
| `launchpad_indexer_sender_backfill_filled_total` | pool swap transactions whose missing sender the indexer's automatic backfill filled |
| `launchpad_indexer_sender_backfill_errors_total` | automatic backfill passes that failed (RPC or database error); the next pass retries |
| `launchpad_indexer_sender_backfill_pending` | pool swaps with `tx_from = NULL` at or below the indexer's safe head, counted after each automatic backfill pass (the authoritative self-healing signal) |

- No alert rule ships for these. Suggested policy for the monitoring owner: warn when
  `launchpad_aggregation_position_dirty_backlog > 500` for 15 minutes, or when
  `increase(launchpad_aggregation_position_failures_total[15m]) > 0` keeps rising. Retune the
  threshold to the chain's trade rate; a large backlog right after a backfill is expected and
  drains on its own.
- A growing backlog or rising failures mean wallet PnL and the leaderboard are stale, not wrong.
  Check the indexer log for `position compute failed`.
- `position_dirty` is a high-churn queue (every claim updates a row, every completion deletes
  one), and its claim index (`position_dirty_claim_idx`) churns with it. After a large seed or
  backlog, check autovacuum keeps up (`pg_stat_user_tables.n_dead_tup` and `last_autovacuum` for
  `position_dirty`); tighten the table's autovacuum settings if dead tuples keep growing. Always
  apply the migrations before running `-seed-positions`, so the seed and the drain use the index.
- Sender misses heal on their own. Each miss leaves a swap with `tx_from = NULL`, which turns
  `attribution_complete` false for every wallet on the chain until it is filled. The indexer
  runs the sender backfill itself every `INDEXER_SENDER_BACKFILL_INTERVAL` (default `60s`, `0`
  disables), at most `INDEXER_SENDER_BACKFILL_MAX_TXS` transactions per pass (default `500`),
  at the manual command's default rate (5 RPC calls per second, 50 hashes each). A capped pass
  remembers where it stopped and the next pass resumes there; once a pass reaches the newest
  row, the next one starts again at the oldest, so transactions that never resolve cannot hold
  newer misses back. Each pass logs one `sender backfill pass finished` line (`capped`,
  `transactions_listed`, `transactions_attributed`, `transactions_unresolved`); individual
  unresolved transactions are logged at debug level only. A pass only
  touches swaps at or below the indexer's committed safe head, so it never races ingest; a
  lagging load-balanced node returning `null` for a just-mined transaction (the usual cause) is
  resolved by the first pass after the block turns safe. Filled senders mark their
  `(sender, token)` positions dirty exactly like the manual command, and
  `launchpad_indexer_sender_backfill_filled_total` rises. A failing pass is logged as
  `sender backfill pass failed` (redacted), counted in
  `launchpad_indexer_sender_backfill_errors_total`, and retried next interval; it never stops
  ingest or fails `/healthz`.
- Alert only on misses that persist: a miss followed by a matching fill (one safe-head lag plus
  one interval later) is normal. The authoritative signal is the number of NULL-sender swaps at
  or below the safe head, exported after every pass as
  `launchpad_indexer_sender_backfill_pending`. Suggested alerts:
  `min_over_time(launchpad_indexer_sender_backfill_pending[30m]) > 0` (misses that survived
  several passes), and `increase(launchpad_indexer_sender_backfill_errors_total[30m]) > 0`. With
  the scheduler disabled, or to check by hand, run the same count in SQL:
  `SELECT count(*) FROM pool_swaps WHERE chain_id = <chain> AND tx_from IS NULL AND block_number <= (SELECT safe_number FROM sync_state WHERE chain_id = <chain> AND deployment_id = '<deployment>');`.
  When an alert fires, check the indexer log for `pool swap sender lookup missed` (one warning
  per committed chunk, with the miss count), `sender backfill pass finished` (its
  `transactions_unresolved`) and `sender backfill pass failed`. If
  the RPC keeps returning `null` for a safe transaction, run `cmd/backfill-swap-sender` (see
  "Backfill pool swap senders (P0-C1)" below) against a different RPC. A transaction that never
  resolves is retried each time the walk wraps around to it.
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
- Sender fetch failures during normal ingestion never stop the indexer; they only leave `NULL`,
  and the indexer's automatic backfill (see "Wallet positions (P0-C1)" above) fills them once
  their block is safe. The manual command is for history that predates sender attribution, an
  indexer running with `INDEXER_SENDER_BACKFILL_INTERVAL=0`, or a different RPC. It is safe to
  run alongside the indexer: both only update rows that are still `NULL`.

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
   pair. It walks the chain's tokens and runs one bounded database statement per token, each
   committed on its own, so a large ledger never becomes one long transaction; it needs no RPC
   and is safe to rerun or to resume after an interruption (a queued pair only gets a new
   generation). It logs `tokens`, `pairs` and `marked`.
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

## Moderating comments

Comment moderation is an operator CLI, `backend/cmd/moderate`. It writes straight to PostgreSQL;
there is no HTTP admin endpoint. **The `DATABASE_URL` credential is the access control**: whoever
holds it can moderate, so run the CLI only from a host or tunnel that already has database
access and never put the URL in a ticket, chat or shell history (export it from the secret
store for the session). Reports are only filed by authenticated users and trigger nothing
automatically.

Every command takes the deployment scope and the operator name that is written to the audit log:

```sh
cd backend
export DATABASE_URL=...   # from the secret store, not typed inline
go run ./cmd/moderate <command> -chain-id N -deployment-id ID -operator NAME [flags]
```

| Command | Flags | Effect |
| --- | --- | --- |
| `reports` | `-limit` (default 50) | Open reports grouped by comment, newest report first: comment id, token, report count, reasons, author DID, posting wallet, body (control characters escaped). |
| `hide` | `-comment ID -reason TEXT` | Hides the comment and resolves its open reports as `actioned`. |
| `unhide` | `-comment ID -reason TEXT` | Shows the comment again. |
| `ban` | `-did DID` or `-comment ID`, `-reason TEXT` | Bans the author: all their comments and their username disappear from public reads; their open reports resolve as `actioned`. `-comment` bans the comment's author. |
| `unban` | `-did DID -reason TEXT` | Lifts the ban; comments and username return. |
| `resolve` | `-comment ID -resolution dismissed\|actioned -reason TEXT` | Closes the comment's open reports without hiding it. |

`-operator` is 1-64 bytes and `-reason` is 1-500 bytes. Every mutation runs in one transaction and
appends a `moderation_actions` row. `hide`, `unhide`, `ban` and `unban` also send one `comment`
refresh hint per affected token so open token pages update; `resolve` changes nothing public and
sends no hint. An unknown comment or DID exits non-zero and writes nothing. Reasons
and the audit log are never public.

Example session:

```sh
go run ./cmd/moderate reports -chain-id 46630 -deployment-id prod-1 -operator alice
# comment=412  token=0x...  reports=3  reasons=scam,spam  author_did="did:privy:..."  body="buy now ..."
go run ./cmd/moderate hide -chain-id 46630 -deployment-id prod-1 -operator alice -comment 412 -reason "scam link"
# hid comment 412
go run ./cmd/moderate ban -chain-id 46630 -deployment-id prod-1 -operator alice -comment 412 -reason "repeat scam links"
# banned author; 2 token(s) affected: 0x...,0x...
```

Review history with `SELECT created_at, action, operator, comment_id, reason FROM
moderation_actions ORDER BY id DESC;`.

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
