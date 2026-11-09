# Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** API and indexer emit the Prometheus metric set from the spec on private listeners, and an opt-in Compose `monitoring` profile scrapes them and fires CI-tested alerts.

**Architecture:** All Prometheus code lives in one new package, `internal/metrics`; the packages it observes (`apiserver`, `chain`, `pricefeed`, `indexer`, `stats`, `store/postgres`) expose small stdlib-only hook interfaces or callbacks, so the existing depguard boundaries hold. `cmd/api` and `cmd/indexer` wire hooks to collectors. Prometheus and Alertmanager run only under the `monitoring` Compose profile.

**Tech Stack:** Go 1.26, `github.com/prometheus/client_golang` v1.25.0, Prometheus v3.15.1, Alertmanager v0.34.1, Docker Compose, PowerShell 7 (`deploy/check.ps1`).

**Spec:** `docs/specs/2026-11-06-observability-design.md`

## Global Constraints

- New Go dependency: `github.com/prometheus/client_golang` v1.25.0 only (owner-approved). No OpenTelemetry, no Sentry, no new web dependency.
- Only `internal/metrics` and `cmd/*` may import `github.com/prometheus/...` (new depguard rule). `internal/pricefeed`, `internal/config`, `internal/curve`, `internal/chain` keep their existing depguard rules.
- Metric prefixes: `launchpad_api_` and `launchpad_indexer_`. Names follow Prometheus conventions (`_total`, `_seconds`).
- No label value may contain an address, transaction hash, DID, request ID, URL or free-form error text. Every label comes from a closed set defined in this plan.
- Metrics never served on the public API listener (`API_ADDR`). API metrics listener: `API_METRICS_ADDR` (empty = disabled; `:8082` in Compose). Indexer: `/metrics` on the existing `INDEXER_HEALTH_ADDR` server.
- Images pinned by digest, resolved at implementation time with `docker buildx imagetools inspect`: `prom/prometheus:v3.15.1`, `prom/alertmanager:v0.34.1`.
- Prometheus retention: `--storage.tsdb.retention.time=15d`, `--storage.tsdb.retention.size=2GB`, named volume.
- The webhook URL is a secret: `ALERTMANAGER_WEBHOOK_URL_FILE`, mounted as a Compose secret and read with `url_file`; never committed, never baked into an image.
- Backend tools run pinned via `go run`; run `gofmt`/`prettier` only on touched files. Commits: conventional prefix, no AI attribution.

## Review Focus

1. A request to a path carrying a token address (`/v1/tokens/0xabc…/trades`) or an unknown path must produce a fixed `route` label, never the raw path. → Task 2 label-safety test.
2. An RPC call that times out (context deadline or `net.Error` timeout) must count as `timeout`, not `error`, and a batch header call must count once per batch. → Task 4 classification test.
3. The indexer starting with no committed state yet must export zero-valued lag gauges without panicking, and `/healthz` and `/metrics` must report the same lag. → Task 4 consistency test.
4. `API_METRICS_ADDR` equal to `API_ADDR` must be rejected at startup (it would put metrics on the public listener). → Task 1 config test.
5. Starting the monitoring profile without a webhook URL file must still run and route alerts nowhere harmful, while a URL file inside the repository tree is rejected by `check.ps1`. → Task 5 check.

---

### Task 1: Metrics package, dependency, config and depguard

**Files:**
- Modify: `backend/go.mod`, `backend/go.sum`, `backend/.golangci.yml`
- Modify: `backend/internal/config/config.go`, `backend/internal/config/config_test.go`
- Create: `backend/internal/metrics/registry.go`, `backend/internal/metrics/labels.go`, `backend/internal/metrics/labels_test.go`
- Modify: `backend/.env.example`, `deploy/backend.env.example`

**Interfaces:**
- Produces: `config.Config.APIMetricsAddr string` (`env:"API_METRICS_ADDR"`); `metrics.NewRegistry(component string) *prometheus.Registry` (registers Go, process and `build_info` collectors, `component` is `api` or `indexer`); `metrics.Handler(g prometheus.Gatherer) http.Handler`; `metrics.UnsafeLabels(g prometheus.Gatherer) ([]string, error)` returning every `name{label="value"}` whose value matches the unsafe patterns.

- [ ] **Step 1: Write failing tests**
  - `TestAPIMetricsAddr` in `config_test.go`: empty → disabled; `:8082` accepted; same value as `API_ADDR` → error mentioning `API_METRICS_ADDR`; malformed (`8082`, `:0`, `:99999`) → error.
  - `TestUnsafeLabelsFlagsSensitiveValues` in `labels_test.go`: a registry with a counter labelled `route="/v1/tokens/0x` + 40 hex + `"` , one with `0x` + 64 hex, one with `did:privy:abc`, one with `https://rpc.example/key`, and one safe `route="GET /v1/tokens/{token}"`; expect exactly the four unsafe series returned.
- [ ] **Step 2: Run** `cd backend && go test ./internal/config/ ./internal/metrics/` — expect FAIL (undefined).
- [ ] **Step 3: Implement.** `go get github.com/prometheus/client_golang@v1.25.0`, `go mod tidy`. Unsafe patterns: `0x[0-9a-fA-F]{40}`, `0x[0-9a-fA-F]{64}`, `did:`, `://`. Add depguard rule `prometheus-boundary` denying `github.com/prometheus` in all files except `**/internal/metrics/**` and `**/cmd/**`. Document `API_METRICS_ADDR` in both env examples.
- [ ] **Step 4: Run** the tests above plus `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 -tags=integration ./...` and the pinned lint — expect PASS, 0 reachable vulnerabilities.
- [ ] **Step 5: Commit** `feat(backend): add metrics package and API_METRICS_ADDR config`

### Task 2: API request, rejection, SSE and pool metrics

**Files:**
- Modify: `backend/internal/apiserver/server.go`, `backend/internal/apiserver/event_routes.go`
- Create: `backend/internal/apiserver/metrics_hooks.go`, `backend/internal/apiserver/metrics_hooks_test.go`
- Create: `backend/internal/metrics/api.go`, `backend/internal/metrics/api_test.go`, `backend/internal/metrics/pool.go`
- Modify: `backend/cmd/api/main.go`

**Interfaces:**
- Consumes: Task 1 `metrics.NewRegistry`, `metrics.Handler`, `metrics.UnsafeLabels`, `config.Config.APIMetricsAddr`.
- Produces in `apiserver` (stdlib only): `type Metrics interface { ObserveRequest(route, method string, status int, elapsed time.Duration); WriteRejected(reason string); StreamOpened(); StreamClosed() }`; `Config.Metrics Metrics` (nil → no-op). Produces in `metrics`: `NewAPI(reg prometheus.Registerer) *API` implementing `apiserver.Metrics`; `RegisterPool(reg prometheus.Registerer, prefix string, stat func() *pgxpool.Stat)`.

Decisions the implementer cannot infer:
- `route` = the pattern from `mux.Handler(r)` computed in the middleware before dispatch (the middleware wraps the mux, so `r.Pattern` is not visible there). Empty pattern → `unmatched`; a request stopped by the rate limiter → `rate_limited`; a CORS preflight → `preflight`.
- `method` ∈ `GET POST PUT DELETE OPTIONS HEAD`, anything else → `other`. `code_class` ∈ `1xx…5xx`.
- `WriteRejected` fires only for `POST`/`PUT`/`DELETE` with: 429 → `rate_limited`, 401 → `unauthorized`, 403 → `forbidden`, 400/422 → `invalid`, 409/412 → `conflict`, 413 → `too_large`. Other statuses are not rejections.
- Duration histogram buckets: `prometheus.DefBuckets`. SSE: `StreamOpened`/`StreamClosed` around the stream's lifetime in `event_routes.go` → gauge `launchpad_api_sse_clients`.
- Pool collector reads `pgxpool.Stat` on scrape: `db_pool_acquired_conns`, `db_pool_idle_conns`, `db_pool_max_conns`, `db_pool_acquire_wait_seconds_total` (from `AcquireDuration`), `db_pool_empty_acquire_total`.
- `cmd/api` starts a second `http.Server` on `APIMetricsAddr` (only when non-empty) serving `metrics.Handler`, shut down with the main server.

- [ ] **Step 1: Write failing tests**
  - `TestMiddlewareReportsRoutePatternNotPath` (`metrics_hooks_test.go`, recording fake `Metrics`): GET `/v1/tokens/0x` + 40 hex + `/trades` → route `GET /v1/tokens/{token}/trades`; GET `/nope` → `unmatched`; a request over the rate limit → `rate_limited` and `WriteRejected` not called for GET; PUT over the limit → `WriteRejected("rate_limited")`; PUT `/v1/me/watchlist/{token}` without auth → `WriteRejected("unauthorized")`.
  - `TestPublicListenerHasNoMetricsRoute`: GET `/metrics` on `Server.Handler` → 404.
  - `TestAPIMetricsLabelsAreSafe` (`metrics/api_test.go`): drive `API` with the requests above and assert `UnsafeLabels(reg)` is empty and `launchpad_api_http_requests_total{route="unmatched",method="GET",code_class="4xx"}` is 1.
- [ ] **Step 2: Run** `go test ./internal/apiserver/ ./internal/metrics/` — expect FAIL.
- [ ] **Step 3: Implement** the hook interface, middleware calls, `metrics.API`, pool collector and the `cmd/api` listener.
- [ ] **Step 4: Run** `go test ./internal/apiserver/ ./internal/metrics/ -race` and `go build ./...` — expect PASS.
- [ ] **Step 5: Commit** `feat(api): export request, rejection, stream and pool metrics`

### Task 3: ETH/USD price feed metrics

**Files:**
- Modify: `backend/internal/pricefeed/feed.go`, `backend/internal/pricefeed/feed_test.go`
- Modify: `backend/internal/metrics/api.go`, `backend/internal/metrics/api_test.go`, `backend/cmd/api/main.go`

**Interfaces:**
- Produces: `pricefeed.Options.Observe func(outcome string)` (optional, stdlib only), called exactly once per completed fetch attempt with `outcome` ∈ `ok`, `rate_limited` (429), `rejected` (401/403), `http_error` (other non-200), `timeout`, `malformed` (parse/validation/missing or future `last_updated_at`). `(*metrics.API).ObservePriceFeed(outcome string)` sets `launchpad_api_eth_usd_last_success_timestamp_seconds` to now on `ok` and increments `launchpad_api_eth_usd_fetch_failures_total{kind}` otherwise. Both series are registered only when the source is enabled.

- [ ] **Step 1: Write failing tests**
  - `TestFeedObserveOutcomes` in `feed_test.go`, using the existing test server: 200 valid → `ok`; 429 → `rate_limited`; 403 → `rejected`; 500 → `http_error`; body with 19 fractional digits → `malformed`; client timeout → `timeout`. Each attempt reports exactly one outcome.
  - `TestPriceFeedMetrics` in `metrics/api_test.go`: `ok` sets the timestamp gauge > 0; two `timeout` → `eth_usd_fetch_failures_total{kind="timeout"} == 2`.
- [ ] **Step 2: Run** `go test ./internal/pricefeed/ ./internal/metrics/` — expect FAIL.
- [ ] **Step 3: Implement**; wire `Observe: apiMetrics.ObservePriceFeed` in `cmd/api` only inside the `ETH_USD_SOURCE=coingecko` branch.
- [ ] **Step 4: Run** the same tests with `-race` plus the pinned lint (pricefeed depguard must stay green) — expect PASS.
- [ ] **Step 5: Commit** `feat(api): export ETH/USD price feed metrics`

### Task 4: Indexer metrics

**Files:**
- Modify: `backend/internal/chain/rpc.go`, `backend/internal/chain/rpc_test.go`
- Modify: `backend/internal/indexer/health.go`, `backend/internal/indexer/engine.go`, `backend/internal/indexer/health_test.go`
- Modify: `backend/internal/stats/worker.go`, `backend/internal/stats/worker_test.go`
- Modify: `backend/internal/store/postgres/indexer_store.go`
- Create: `backend/internal/metrics/indexer.go`, `backend/internal/metrics/indexer_test.go`
- Modify: `backend/cmd/indexer/main.go`

**Interfaces:**
- Produces in `chain`: `type RPCObserver interface { ObserveRPC(method, outcome string) }`; `RPCConfig.Observer RPCObserver` (nil → none). Every `Client` method reports once per call (a batch counts once) with the JSON-RPC method name: `eth_getBlockByNumber`, `eth_getBlockByHash`, `eth_getCode`, `eth_chainId`, `eth_getLogs`, `eth_call`, `eth_blockNumber` as applicable; `Heads` reports each underlying call. `outcome` ∈ `ok`, `timeout` (`context.DeadlineExceeded` or `net.Error.Timeout()`), `error`.
- Produces in `indexer`: `Health.ChainHead int64` and `Health.DatabaseUp bool`; `(*HealthTracker).ChainHead(number int64)`; `(*HealthTracker).Database(up bool)`; `Settings.OnHeads func(latest int64)` (the engine's existing `Settings`, beside `OnCommitted`) called after a successful `Heads` read.
- Produces in `stats`: `Worker.OnComputed func(count int)` called with the number of claims completed per drain.
- Produces in `store/postgres`: `IndexerStore.OnNotify func(channel string, ok bool)` called for `market_dirty` and `api_refresh`.
- Produces in `metrics`: `NewIndexer(reg prometheus.Registerer, snapshot func() indexer.Health) *Indexer` with `ObserveRPC`, `Recomputed(count int)`, `Notified(channel string, ok bool)`. Gauges read the snapshot at scrape time: `block_number{level}`, `lag_seconds{level}` for `observed|safe|finalized`, `chain_head_block`, `ownership_held`, `rpc_healthy`, `db_up`, `dirty_work`, `last_reorg_depth`; `reorgs_total` is a counter derived from `LastReorgID` (monotonic). Counters: `rpc_requests_total{method,outcome}`, `stats_recomputed_total`, `notifications_total{channel,outcome}` with `outcome` ∈ `ok|error`.
- `cmd/indexer`: the health server's handler becomes a mux with `/healthz` (existing handler) and `/metrics`; `refreshOperationalHealth` calls `health.Database(err == nil)`.

- [ ] **Step 1: Write failing tests**
  - `TestRPCObserverClassifiesOutcomes` (`rpc_test.go`, `httptest` JSON-RPC server): success → `ok`; server sleeping past `RPCConfig.Timeout` → `timeout`; JSON-RPC error response → `error`; `HeadersByNumbers` for 3 blocks → one observation.
  - `TestHealthChainHeadAndDatabase` (`health_test.go`): set via the new methods; `Snapshot()` returns them; a fresh tracker returns zero lag without panic.
  - `TestIndexerMetricsMatchHealthSnapshot` (`metrics/indexer_test.go`): drive a `HealthTracker` with `Committed` for observed 100 / safe 90 / finalized 80 and `ChainHead(105)`; scrape; `block_number{level="observed"} == 100`, `chain_head_block == 105`, and each `lag_seconds` equals the snapshot's lag within 1 s; `UnsafeLabels` empty.
  - `TestWorkerReportsComputedCount` (`worker_test.go`): 3 claims, 1 compute error → `OnComputed(2)`.
- [ ] **Step 2: Run** `go test ./internal/chain/ ./internal/indexer/ ./internal/stats/ ./internal/metrics/` — expect FAIL.
- [ ] **Step 3: Implement** the hooks and the collector; wire them in `cmd/indexer`.
- [ ] **Step 4: Run** `go test ./... -race`, `go test -tags=integration ./internal/indexer/... ./internal/store/postgres/... -race`, and the pinned lint — expect PASS.
- [ ] **Step 5: Commit** `feat(indexer): export indexer, RPC and aggregation metrics`

### Task 5: Monitoring profile, alert rules and deploy checks

**Files:**
- Create: `deploy/monitoring/prometheus.yml`, `deploy/monitoring/alerts.yml`, `deploy/monitoring/alerts.test.yml`, `deploy/monitoring/alertmanager.yml`, `deploy/monitoring/webhook-url.example`
- Modify: `deploy/compose.yaml`, `deploy/check.ps1`, `.github/workflows/deployment-package.yml`

**Interfaces:**
- Consumes: metric names from Tasks 2–4; API metrics on `api:8082`, indexer on `indexer:8081`.
- Produces: Compose services `prometheus` and `alertmanager` with `profiles: [monitoring]`; `api` gets `API_METRICS_ADDR: :8082` with no `ports:` entry.

Decisions the implementer cannot infer:
- Both services: `read_only: true`, `cap_drop: [ALL]`, `no-new-privileges`, the shared `logging` anchor. Prometheus publishes only `127.0.0.1:9090:9090`; Alertmanager publishes nothing. Prometheus data on volume `prometheus_data`; Alertmanager on `alertmanager_data`.
- Secret `alertmanager_webhook_url` from `${ALERTMANAGER_WEBHOOK_URL_FILE:-./monitoring/webhook-url.example}`; the example file contains `http://127.0.0.1:9/disabled` so the profile runs with a receiver that cannot reach anything. `alertmanager.yml` routes everything to one `webhook_configs` receiver with `url_file: /run/secrets/alertmanager_webhook_url`, `group_by: [alertname]`, `repeat_interval: 4h`.
- `alerts.yml` implements the spec's nine alerts with the spec's thresholds; `IndexerObservedLagHigh` uses `launchpad_indexer_chain_head_block - on() launchpad_indexer_block_number{level="observed"} > 300`; `EthUsdStale` uses `time() - launchpad_api_eth_usd_last_success_timestamp_seconds > 600` (absent series = source disabled = no alert).
- CI (`deployment-package.yml`): run the pinned Prometheus image with `--entrypoint promtool` for `check config deploy/monitoring/prometheus.yml`, `check rules deploy/monitoring/alerts.yml`, `test rules deploy/monitoring/alerts.test.yml`; run the pinned Alertmanager image's `amtool check-config`.
- `check.ps1`: `docker compose --profile monitoring config` must resolve; the disposable runtime check asserts `GET http://$apiBinding/metrics` is 404, starts the profile and waits until Prometheus `/api/v1/targets` reports both `api` and `indexer` with `health == "up"`; rejects an `ALERTMANAGER_WEBHOOK_URL_FILE` that resolves inside the repository unless it is the example file. (The disposable check runs without Caddy, so the public-listener 404 is the proof that Caddy, which only forwards to `api:8080`, cannot reach metrics.)

- [ ] **Step 1: Write `alerts.test.yml`** with one firing and one non-firing case per alert (18 cases minimum).
- [ ] **Step 2: Run** `docker run --rm -v "${PWD}/deploy/monitoring:/m" --entrypoint promtool prom/prometheus@<digest> test rules /m/alerts.test.yml` — expect FAIL (no rules yet).
- [ ] **Step 3: Implement** the configs, Compose services, CI steps and `check.ps1` assertions.
- [ ] **Step 4: Run** the promtool/amtool commands and `pwsh -File deploy/check.ps1 -Build` — expect PASS and both targets `up`.
- [ ] **Step 5: Commit** `feat(deploy): add opt-in monitoring profile with tested alert rules`

### Task 6: Runbook and backlog

**Files:**
- Modify: `docs/runbooks/production-readiness.md`, `deploy/README.md`, `backlog.md`

- [ ] **Step 1: Add a "Monitoring" section** to the runbook: enabling `--profile monitoring`, supplying `ALERTMANAGER_WEBHOOK_URL_FILE` from outside the repository, reaching Prometheus through `127.0.0.1:9090` or an SSH tunnel, the alert table (copied from `alerts.yml` names and thresholds), and the single-API-instance rate-limit note. Link it from `deploy/README.md`.
- [ ] **Step 2: Move** the backlog "Observability" item to Done with evidence (commits and gate output); keep the monitoring owner and alert destination under the production-inputs item.
- [ ] **Step 3: Run** the full gates: `cd backend && task verify` (PowerShell, Foundry on PATH), `cd web && npm run format:check && npm run lint && npm run typecheck && npm test`, `pwsh -File deploy/check.ps1 -Build` — expect PASS apart from the known Windows CRLF false positives.
- [ ] **Step 4: Commit** `docs: add monitoring runbook and close observability backlog item`
