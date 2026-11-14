# Quality scorecard

> **Standing goal: overall ≥ 9.0 / 10.** This file is the baseline every milestone is measured
> against. It is kept, not replaced: re-score after each milestone (or any change that moves a
> category), append a row to the history, and update the "Current" column with evidence.
> A score only goes up with evidence in the repository (a merged PR, a test, a document, an
> external report). Plans and intentions never count.

Overall is the unweighted mean of the categories, rounded to one decimal.

## Scores

| # | Category | Baseline 2026-11-13 | Current | Evidence for the current score |
|---|----------|--------------------:|--------:|--------------------------------|
| 1 | Documentation and process | 9.0 | 9.0 | 27+ pre-flight specs, runbooks, audit folder, release checklist, single-source `CLAUDE.md`. |
| 2 | Backend architecture | 8.5 | 8.5 | stdlib-only `internal/curve` / `internal/config` (depguard), `*big.Int` / `NUMERIC(78,0)`, sqlc, read snapshots; `adapter.go` split (#55). |
| 3 | Indexer and data correctness | 8.5 | 8.5 | Reorg handling, byte-identical curve vectors in CI, dirty-mark dedupe (#54), batched stats recompute (#56). |
| 4 | Tests and verification | 8.5 | 8.5 | 155 Go test files, testcontainers integration, 36/36 contract tests, 528 vitest, Anvil e2e (22 + 1). No rendered React hook tests, no load tests. |
| 5 | Smart contracts | 7.5 | 8.0 | Checked arithmetic, Slither, fork tests, internal audit docs. #68: deep property campaign with a fixed seed, 11 fuzz tests × 10,000 runs and 10 invariants × 1,000 runs × 200 calls, all pass. No external audit yet. |
| 6 | Application security | 7.5 | 8.5 | Re-read-and-compare before every signature, exact approvals, chain/account re-check, no secrets in URLs. #68: focused review of Privy verification, wallet ownership and the API boundary, no high/medium findings, one low fixed (`Vary: Origin`), three accepted in writing; rate limits tabulated with tests. Open: Privy 3 upgrade blocked upstream. |
| 7 | CI/CD and infrastructure | 7.5 | 8.5 | Pinned tools, 5 workflows, branch protection, Compose + Caddy. Postgres backup/restore exercised by `deploy/check.ps1` in CI; `promtool test rules` and `amtool check-config` in CI. #67: Caddy balances over `--scale api=N` replicas (two-replica distribution and failover tested), scale-out and CDN runbook. Open: no CDN in front yet. |
| 8 | Performance | 7.0 | 8.0 | Batched stats (#56), parallel `eth_getLogs` (#58), parallel token-page reads and hidden-tab polling (#59). #68: targets (API read p95 < 250 ms, indexer lag, LCP < 2.5 s) with a tested `APIReadLatencyHigh` alert; `cmd/loadprobe` baseline ~2,190 req/s, every route p95 < 12.5 ms on the local stack. Open: no production-volume or LCP measurement; cache deferred until measured. |
| 9 | Maintainability (spaghetti code) | 7.0 | 9.0 | One reviewed-write pipeline for the curve, V2, V4 and launch panels (#59, #60); `token-detail` 950 → 185, `wallet-view` 669 → 201 lines (#60). #69: 8 web modules split below 400 lines of code, the direct (non-reviewed) writes share one `runDirectWrite` flow, and ESLint `max-lines` (400, blank and comment lines excluded; tests and generated code exempt) fails CI on a larger file. |
| 10 | UX and interface | 7.0 | 8.5 | Review `docs/audits/2026-11-13-ux-review.md`: 13 issues fixed, then all 8 open product items closed (#62–#65). The fixes: image and description at launch, subscript-zero prices, 2 % default slippage, a trade card without inner scroll, creator-only sections, sparse chart, compact hero, no metadata 404. Lighthouse 2026-11-14 on dev: accessibility, best practices and SEO are 100 on home, token and create, desktop and mobile; no console errors. Token page cold-load CLS 0.136 → 0.0065 (the loading page reserves the viewport height). Open: no signed-in pass recorded yet. |
| 11 | Product completeness (vs pons.family) | 6.5 | 8.0 | Launch, curve, graduation, swaps, forum, analytics and docs ship. #66: ticker tape and clusters Level 2 removed by scope decision (`docs/specs/2026-11-14-product-scope-decisions.md`), the Telegram worker deployable as an opt-in Compose profile, the ETH/USD feed runs on the free CoinGecko Demo plan. Open: Telegram and USD not live yet (bot token and Demo key pending), CoinGecko market links wait on the mainnet manifest. |
|   | **Overall** | **7.7** | **8.5** | |

## Path to 9

What each category needs before it may be scored 9 or higher. Ticking an item needs the evidence
named in it.

1. **Documentation and process (9.0 → 9.5):** an architecture overview that names every component
   and data flow, kept current with the system-design record; an operator runbook rehearsed once
   end to end (log the rehearsal).
2. **Backend architecture (8.5 → 9):** documented public API contract with versioning and a
   compatibility test; no package over ~600 lines outside generated code.
3. **Indexer and data correctness (8.5 → 9):** a reorg-depth soak test (deep reorgs on Anvil,
   indexer output compared with a fresh re-index); a reconciliation job comparing indexed state with
   chain reads, alerting on drift.
4. **Tests and verification (8.5 → 9):** rendered tests for the transaction hooks
   (`useReviewedWrite`, `useReviewedTrade`) with a mocked wallet; a load test of the public read API
   with recorded results; coverage numbers published in CI.
5. **Smart contracts (8.0 → 9):** an external audit with every finding fixed or accepted in
   writing. Already met: invariant/fuzz suites for curve math and graduation with recorded run
   parameters (`docs/audits/2026-11-14-deep-property-campaign.md`, `[profile.deep]`, #68).
6. **Application security (8.5 → 9):** the Privy 3 upgrade closed (backlog item, blocked until
   upstream clears the allowlisted advisories). Already met: a focused review of auth, wallet
   verification and the API with every finding closed or accepted, and rate limits and abuse
   controls documented with tests (`docs/audits/2026-11-14-auth-api-security-review.md`, #68).
7. **CI/CD and infrastructure (8.5 → 9):** a CDN or edge cache in front of the proxy, set up per
   `docs/runbooks/scale-out-and-cdn.md` (client-IP trust in Caddy and the API) and recorded with
   evidence. Already met: tested Postgres backup/restore (`deploy/check.ps1` in CI), alert rules
   unit-tested and Alertmanager config checked in CI, a documented and tested API scale-out path (#67).
8. **Performance (8.0 → 9):** measurements that meet the stated targets at production-like
   volume (`cmd/loadprobe` against a test deployment) and a recorded mobile LCP on a production
   build; a cache for hot public reads once a measurement shows a route near the target.
   Already met: targets with a tested alert (`docs/specs/2026-11-14-performance-targets.md`, #68).
9. **Maintainability (9.0 → 9.5):** keep the 400-line limit without new exemptions. Already met:
   no web source file over 400 lines of code, no duplicated write/approval logic outside the shared
   pipelines (`useReviewedWrite`, `runDirectWrite`), and an ESLint `max-lines` rule in CI (#69).
10. **UX and interface (8.5 → 9):** a signed-in pass
    with a connected wallet (review, sign, approval, and launch with an image through real Privy on a
    test deployment), recorded like the 2026-11-13 review, with no new P1/P2 findings.
11. **Product completeness (8.0 → 9):** run the Telegram worker with a real bot and the ETH/USD
    feed with a Demo key on a test deployment, recorded with evidence, and ship the CoinGecko
    market links once the mainnet manifest exists. Ticker tape and clusters Level 2 are out of
    scope (2026-11-14 decision); operator actions are not product scope.

## History

| Date | Overall | Change | Evidence |
|------|--------:|--------|----------|
| 2026-11-13 | 7.7 | Baseline | Codebase review after the second cleanup sweep. |
| 2026-11-13 | 7.8 | Performance 7.0 → 7.5, Maintainability 7.0 → 7.5 | #58, #59 |
| 2026-11-13 | 7.9 | Maintainability 7.5 → 8.0, UX 7.0 → 8.0 | #60; UX review and fixes (`docs/audits/2026-11-13-ux-review.md`) |
| 2026-11-14 | 8.0 | UX 8.0 → 8.5 | #62–#65 closed every open review item; Lighthouse re-run on dev |
| 2026-11-14 | 8.1 | Product completeness 6.5 → 8.0 | #66: scope decisions, CoinGecko Demo plan, opt-in Telegram profile |
| 2026-11-14 | 8.2 | CI/CD 7.5 → 8.5 | #67 scale-out path; backup/restore and alerting checks already in CI were missing from the evidence |
| 2026-11-14 | 8.4 | Smart contracts 7.5 → 8.0, Application security 7.5 → 8.5, Performance 7.5 → 8.0 | #68: deep property campaign, auth/API security review, performance targets and baseline |
| 2026-11-14 | 8.5 | Maintainability 8.0 → 9.0 | #69: web modules under 400 lines, one direct-write flow, ESLint `max-lines` in CI |
