# Observability — metrics and alerting for API and indexer

Status: Claude pre-flight, 2026-11-06. Stack decisions made by the product owner in conversation
(Prometheus client, Compose monitoring profile, error tracking deferred). Awaiting owner review
before Codex plans the implementation.
Backlog item: "Observability: metrics, alerting, and error tracking".

## Goal

An operator learns about an outage, a stuck indexer or a failing RPC from an alert instead of from
users. Every signal listed in `docs/runbooks/production-readiness.md` ("indexer head/finality lag,
database connectivity, RPC error rate, HTTP 5xx, write rejection rate, and
notification/reconciliation activity") is emitted, scraped and has an alert rule in the
repository that CI tests.

## Non-goals

- No error-tracking service (Sentry or similar) and no browser error reporting. Backend failures
  surface through error-rate metrics plus logs carrying request IDs. Revisit when there is real
  traffic.
- No distributed tracing, no dashboards product (Grafana). Prometheus' own UI on a private port
  is enough until an owner asks for dashboards.
- No naming of the monitoring owner, on-call rota or alert destination: those stay pending
  production inputs.

## Decisions

| Decision | Rationale | Rejected alternative |
| --- | --- | --- |
| API and indexer expose Prometheus metrics with `github.com/prometheus/client_golang` (latest release at least two weeks old at implementation time). | The de facto standard; correct histograms and registries; any scraper (self-hosted or hosted) can read it. The owner approved the dependency. | Hand-rolled stdlib exporter (more code, easy to get histograms wrong); OpenTelemetry SDK (heaviest dependency tree for no current need). |
| Metrics are served only on private listeners: the API gets a second listener `API_METRICS_ADDR` (default disabled, `:8082` in Compose), never routed by Caddy; the indexer adds `/metrics` to its existing private health server. | The public API listener must never serve `/metrics`; a separate listener makes that structural instead of a path rule that a proxy change could break. The indexer health port is already private. | `/metrics` on the public API listener behind a Caddy path block. |
| An optional Compose profile `monitoring` adds Prometheus and Alertmanager, both pinned by digest, read-only, with no published ports except `127.0.0.1` for the Prometheus UI. | Works on the single-host Compose package without choosing a hosting vendor; a hosted scraper can replace it later by scraping the same endpoints. | Hosted agent only (alert rules would live outside the repository and be untested); endpoint only (no alerts, so the gap stays open). |
| Alert rules live in `deploy/monitoring/` and are tested with `promtool test rules` in the `deployment package` workflow. | Thresholds become reviewable code with tests, like the rest of the deploy package. | Rules written by hand in a UI. |
| The Alertmanager configuration is committed, but its webhook receiver reads the URL from a Compose secret file (`url_file`, supplied through `ALERTMANAGER_WEBHOOK_URL_FILE`). Without that file the profile runs with a no-op receiver. | Routing and grouping stay reviewable code; only the credential-bearing URL is external, like the other deploy secrets. The destination is still a pending production input. | A committed receiver URL; a fully external config file (routing would go unreviewed). |
| One API instance stays the supported topology, so the in-memory rate limiter stays as it is. | Compose runs a single API container; shared limiting is only needed once there are several. The runbook states the limit. | Redis-backed limiting now (new datastore for no current need). |

## Metric set

Names follow Prometheus conventions (`_total`, `_seconds`, base units). Every label value comes
from a closed set: no token, pair or wallet address, transaction hash, DID, request ID or URL ever
becomes a label.

API (`launchpad_api_` prefix):

- `http_requests_total{route, method, code_class}` and
  `http_request_duration_seconds{route, method}`, where `route` is the matched `http.ServeMux`
  pattern (`r.Pattern`, for example `GET /v1/tokens/{token}`), never the raw path. Unmatched and
  rate-limited requests share the fixed values `unmatched` and `rate_limited`.
- `write_rejections_total{reason}` for rate-limited, unauthorized, forbidden and validation
  rejections on write routes.
- `sse_clients` gauge.
- `db_pool_*` gauges from `pgxpool.Stat` (acquired, idle, max) and
  `db_pool_acquire_wait_seconds_total`.
- `eth_usd_last_success_timestamp_seconds` and `eth_usd_fetch_failures_total{kind}`, only when
  the price source is enabled.

Indexer (`launchpad_indexer_` prefix):

- `block_number{level}` and `lag_seconds{level}` for `observed`, `safe` and `finalized`, from the
  same `HealthTracker` snapshot `/healthz` serves, so the two never disagree.
- `chain_head_block` from the latest RPC head, so lag is also measurable in blocks
  (`chain_head_block - block_number{level="observed"}`); block-time lag alone stretches on an idle
  L2 where no blocks are produced.
- `rpc_requests_total{method, outcome}` with `outcome` in `ok|error|timeout`, where `method` is
  the JSON-RPC method name.
- `ownership_held` and `rpc_healthy` gauges; `reorgs_total` and `last_reorg_depth`.
- `dirty_work` gauge and `stats_recomputed_total`; `notifications_total{channel}` for the
  Postgres notifications it sends (reconciliation activity).
- `db_up` gauge, set from the indexer's own periodic database check.

Both binaries also export the client library's Go runtime, process and `build_info` collectors.

## Alert rules (initial thresholds)

The owner can change the numbers; each has a test.

| Alert | Condition | For |
| --- | --- | --- |
| `TargetDown` | `up == 0` for API or indexer | 2 m |
| `IndexerNotReady` | `ownership_held == 0` or `rpc_healthy == 0` | 5 m |
| `IndexerObservedLagHigh` | head minus observed > 300 blocks | 5 m |
| `IndexerFinalizedLagHigh` | finalized lag > 30 m | 10 m |
| `RPCErrorRateHigh` | RPC error or timeout share > 5 % over 10 m | 10 m |
| `API5xxRateHigh` | 5xx share > 1 % over 10 m, at least 1 request/s | 10 m |
| `DatabaseDown` | indexer `db_up == 0`, or API pool acquire wait rising while no connection is idle | 2 m |
| `WriteRejectionSpike` | write rejections > 10× their 1 h average | 15 m |
| `EthUsdStale` | price source enabled and no success for 10 m | 10 m |

## BLOCKERS

None. Implementation can start without the alert destination: the no-op receiver keeps the
profile runnable, and the destination is already tracked under production inputs.

## RISKS

- **Label cardinality and data leakage.** Many routes carry token, pair or wallet addresses, and
  the RPC URL can carry an API key. Raw paths or URLs used as labels would leak data and grow
  without bound. Labels must come from the closed sets above; a test must fail if any metric
  name or label value matches an address, hash or URL pattern.
- **Accidental public exposure.** A future Caddyfile edit or a port mapping could publish
  `/metrics` or Prometheus. Covered by: a separate listener, no `ports:` entry except
  `127.0.0.1`, and a `deploy/check.ps1` assertion that `https://$API_DOMAIN/metrics` is 404.
- **Two sources of truth for health.** If the lag gauges were computed separately from
  `HealthTracker`, `/healthz` and alerts could disagree during an incident. The metrics read the
  same snapshot.
- **Hot-path overhead.** Request middleware must not allocate per-request label strings beyond
  the fixed set; use the library's `WithLabelValues` on the route pattern the mux already holds.
- **Prometheus storage on the shared host.** An unbounded TSDB can fill the disk the database
  uses. Set `--storage.tsdb.retention.size` (default 2 GB) and time retention (15 d), on a named
  volume.
- **The webhook URL is a secret.** It must be mounted from outside the repository as a Compose
  secret, never baked into an image, and `check.ps1` must reject a URL file inside the repo tree.
- **Image pinning.** Prometheus and Alertmanager images must be pinned by digest like the other
  Compose images, at a release at least two weeks old.

## ACCEPTANCE CRITERIA

1. `GET http://<api>:8082/metrics` and `GET http://<indexer>:8081/metrics` return the metric set
   above; `https://$API_DOMAIN/metrics` returns 404 through Caddy (asserted in `deploy/check.ps1`).
2. A unit test scrapes each registry and fails on any label value that looks like a hex address,
   transaction hash, DID or URL.
3. The indexer lag gauges equal the `/healthz` snapshot values in a test that drives the
   `HealthTracker`.
4. `promtool check config` and `promtool test rules` pass in the `deployment package` workflow,
   with at least one firing and one non-firing case per alert.
5. `docker compose --profile monitoring config` resolves, the profile starts in the disposable
   runtime check, and Prometheus reports both targets `up`.
6. The runbook gains a "Monitoring" section: how to enable the profile, the alert table, the webhook URL secret, and the single-API-instance rate-limit note.
7. Existing gates stay green: `task verify` (including `govulncheck` after the new dependency),
   web gates and the Anvil gate.
