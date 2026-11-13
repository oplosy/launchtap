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
| 5 | Smart contracts | 7.5 | 7.5 | Checked arithmetic, Slither, fork tests, internal audit docs. No external audit yet. |
| 6 | Application security | 7.5 | 7.5 | Re-read-and-compare before every signature, exact approvals, chain/account re-check, no secrets in URLs. Privy auth not finished. |
| 7 | CI/CD and infrastructure | 7.5 | 7.5 | Pinned tools, 5 workflows, branch protection, Compose + Caddy. Single host, no CDN, no scaling plan. |
| 8 | Performance | 7.0 | 7.5 | Batched stats (#56), parallel `eth_getLogs` (#58), parallel token-page reads and hidden-tab polling (#59). No measured targets, no cache layer. |
| 9 | Maintainability (spaghetti code) | 7.0 | 8.0 | One reviewed-write pipeline for the curve, V2, V4 and launch panels (#59, #60); `token-detail` 950 → 185, `wallet-view` 669 → 201 lines (#60). |
| 10 | UX and interface | 7.0 | 8.0 | Recorded review (`docs/audits/2026-11-13-ux-review.md`): 13 issues fixed. Lighthouse accessibility is 100 on home, token, create, wallet and forum (from 90–100). Image upload at launch closed. Open items: tiny-price notation, default slippage, creator-only sections. |
| 11 | Product completeness (vs pons.family) | 6.5 | 6.5 | Launch, curve, graduation, swaps, forum, analytics and docs ship. Ticker tape, Telegram bot, CoinGecko links, clusters L2 and operator actions open. |
|   | **Overall** | **7.7** | **7.9** | |

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
5. **Smart contracts (7.5 → 9):** an external audit with every finding fixed or accepted in
   writing; invariant/fuzz suites for curve math and graduation with the run parameters recorded.
6. **Application security (7.5 → 9):** Privy auth finished (backlog items closed); a security
   review (`security-audit`) of auth, wallet verification and the API with findings closed;
   rate limits and abuse controls documented and tested.
7. **CI/CD and infrastructure (7.5 → 9):** a CDN or edge cache in front of static assets; a tested
   backup/restore of Postgres; alerting wired to the monitoring stack; a documented scale-out path
   for API and indexer.
8. **Performance (7.5 → 9):** stated targets (p95 API latency, indexer lag, page LCP) with
   measurements that meet them; a cache layer for hot public reads that respects live freshness.
9. **Maintainability (8.0 → 9):** no web source file over ~400 lines (current largest:
   `token-sections.tsx`, `launch-panel.tsx`, `trading-panel.tsx`); no duplicated write/approval
   logic outside the shared pipeline; lint rule or CI check for file size.
10. **UX and interface (8.0 → 9):** close the open items of the 2026-11-13 review
    (`docs/audits/2026-11-13-ux-review.md`). That means image upload and a description at launch,
    subscript-zero price display, a slippage default decision, creator-only sections hidden from
    visitors, and the trade panel fitting without inner scroll. Then add a signed-in pass (connected
    wallet: review, sign, approval) recorded the same way.
11. **Product completeness (6.5 → 9):** close the open product backlog items (ticker tape,
    Telegram bot, CoinGecko market links, clusters Level 2, operator actions) or record a scope
    decision that removes them.

## History

| Date | Overall | Change | Evidence |
|------|--------:|--------|----------|
| 2026-11-13 | 7.7 | Baseline | Codebase review after the second cleanup sweep. |
| 2026-11-13 | 7.8 | Performance 7.0 → 7.5, Maintainability 7.0 → 7.5 | #58, #59 |
| 2026-11-13 | 7.9 | Maintainability 7.5 → 8.0, UX 7.0 → 8.0 | #60; UX review and fixes (`docs/audits/2026-11-13-ux-review.md`) |
