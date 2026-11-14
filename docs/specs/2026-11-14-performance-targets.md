# Performance targets and baseline — 2026-11-14

## Decision

| Target | Value | Measured by | Enforced by |
|--------|-------|-------------|-------------|
| API read latency | p95 < 250 ms per replica at expected load | `launchpad_api_http_request_duration_seconds` (GET, SSE excluded) | alert `APIReadLatencyHigh` (`deploy/monitoring/alerts.yml`, tested in `alerts.test.yml`) |
| Indexer lag | observed head within 300 blocks; finalized lag < 30 min | `launchpad_indexer_chain_head_block`, `launchpad_indexer_block_number`, `launchpad_indexer_lag_seconds` | existing alerts `IndexerObservedLagHigh`, `IndexerFinalizedLagHigh` |
| Page LCP | < 2.5 s at p75 on mobile (Core Web Vitals "good") | Lighthouse / field data on a production build | Lighthouse step in "Evidence and release decision" (`docs/runbooks/web-release.md`, added with this spec) |

**Rationale.** 250 ms sits on a default histogram bucket boundary, so the alert's quantile is
exact at the threshold rather than interpolated, and it leaves room for a cold cache and network
time under the 2.5 s LCP budget. The alert needs at least 1 GET/s for 15 minutes so a handful of
slow requests on an idle deployment do not page anyone. The indexer thresholds are the existing,
tested alert values; this spec names them as targets instead of inventing new ones.

**Alternatives considered.** A p99 target (too noisy at low traffic), per-route targets (the
routes share one store and pool; one global read target is enough until a route is shown to
differ), and a load-test gate in CI (CI runners give unstable timings; the alert on real traffic
is the gate).

## Cache layer

Not added in this change. Every public read is served from a read snapshot and the busiest
aggregate, `/v1/tokens/highlights`, already sits behind a 15 s single-entry cache
(`HighlightsRoutes`). On the baseline below every route is ~20× under the target, so another cache
would add invalidation risk (live freshness relies on refresh hints) with no measured benefit. Add
one when a production-volume measurement or the `APIReadLatencyHigh` alert shows a route near the
target, keyed by route and query and invalidated by the same `api_refresh` hints the SSE hub uses.

## Baseline measurement

Tool: `go run ./cmd/loadprobe` (backend, stdlib only, read-only GETs; reports p50/p95/p99 per
path). Setup: `web/scripts/local-stack.ps1` (Anvil, Postgres 18.6 in Docker, API and indexer
built from this change, seeded launches, trades and forum posts) on one Windows 11 developer
machine; a second API process on the same database with the rate limit raised so the probe
measures the server, not the limiter. 16 workers, 30 s, 10 routes in rotation.

| Path | Requests | Errors | p50 | p95 | p99 |
|------|---------:|-------:|----:|----:|----:|
| `/tokens` | 6,567 | 0 | 7.53 ms | 9.75 ms | 12.35 ms |
| `/tokens/highlights` | 6,569 | 0 | 0.52 ms | 0.62 ms | 1.08 ms |
| `/stats/protocol` | 6,571 | 0 | 6.95 ms | 9.04 ms | 11.81 ms |
| `/trades/recent` | 6,571 | 0 | 8.02 ms | 10.41 ms | 13.32 ms |
| `/leaderboard` | 6,573 | 0 | 7.06 ms | 9.44 ms | 12.11 ms |
| `/tokens/{token}` | 6,574 | 0 | 7.39 ms | 9.46 ms | 12.19 ms |
| `/tokens/{token}/trades` | 6,573 | 0 | 8.16 ms | 10.63 ms | 13.70 ms |
| `/tokens/{token}/candles` | 6,571 | 0 | 7.90 ms | 10.03 ms | 12.58 ms |
| `/tokens/{token}/holders` | 6,569 | 0 | 7.91 ms | 9.96 ms | 12.29 ms |
| `/wallets/{address}` | 6,568 | 0 | 10.06 ms | 12.41 ms | 15.58 ms |

About 2,190 requests/s in total with every p95 under 12.5 ms. The indexer sat at the chain head
(observed block 154 of 154).

**Limits of this baseline.** The seeded database holds a few launches, so it measures per-request
overhead and concurrency, not how queries scale with production data volume; client, API and
database share one machine. LCP is not measured here: the local stack runs the Next dev server,
which is not representative. Next measurements: the same probe against a test deployment with
production-like volume, and Lighthouse on a production build per the web release runbook.
