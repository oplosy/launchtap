# Production-readiness audit, pass 3 (2026-11-16)

Read-only review of contracts, backend API and auth, store, indexer, web, deployment and CI at
`e84f515` (`task/audit-report-2-fixes`). Earlier findings (`AUDIT_REPORT_2026-11-16.md`, run-4)
are not repeated. Gates run during the audit: `forge test` 213/213 passed; `go test` for
`apiserver`, `privyauth`, `chain`, `indexer`, `curve` passed.

## Findings

| ID | Severity | Finding | Status |
|----|----------|---------|--------|
| H-1 | High | Curve logs were fetched by topic only. Any contract can emit the curve signatures, so a block flooded with impostor logs exceeds the provider's `eth_getLogs` limit even for one block; the range split ends in `ErrRPCCapacity` and the indexer can never pass that block (`backend/internal/chain/discovery.go`, `fetchByTopics`). | Fixed: a failed topic-only attempt falls back to the address-batched query of known curves, which impostors cannot inflate. Regression tests fail without the fix. |
| M-1 | Medium | Every service connects as the Postgres superuser `POSTGRES_USER`; the API can write ledger tables and use superuser-only features. | Open (backlog: pre-flight) |
| M-2 | Medium | Comment and vote eligibility is `balance > 0`; dust spread over many Privy accounts manipulates scores and forum ranking. | Open (product decision) |
| M-3 | Medium | Token `Transfer` discovery is O(tokens / 500) `eth_getLogs` calls per poll. | Open (design) |
| L-1 | Low | `tradeFeeBps` up to 99.99 %, unbounded `launchFee`, curve-end vs pool-open price continuity not enforced on chain (equal under today's parameters). | Open (contract pre-flight) |
| L-2 | Low | Before graduation, holders can open a third-party AMM pair; `LaunchTokenV2._update` only guards the curve and the v4 PoolManager. | Open (product decision) |
| L-3 | Low | Metadata authority stays with the original creator and metadata is mutable after graduation without a public history. | Open |
| L-4 | Low | A launch's protocol treasury is fixed; rotating the treasury does not move fees of existing launches. | Open (design note) |
| L-5 | Low | API readiness ignores indexer freshness; the web never marks data stale. | Open (needs heartbeat) |
| L-6 | Low | Slither ran only in the manual release gate, last run 2026-10-05, before engine 2 existed; four triage entries had drifted with #82, so the gate failed. | Fixed: `Slither` job on every push/PR; the four entries refreshed (same findings, new line spans), verified with Slither 0.11.6 on Linux. |
| L-7 | Low | No HSTS header on either site. | Fixed in `deploy/Caddyfile` (one year, no `includeSubDomains`). |
| L-8 | Low | Linked wallets from the Privy identity token stay trusted until it expires, also after an unlink. | Accepted: inherent to stateless token verification. |

Dependency updates had no automation; `.github/dependabot.yml` now opens weekly PRs into `dev`
for Go, npm, Actions and the Docker base images.

## Verified as sound

Graduation guard hook (only the token's curve initializes the pool), the pre-graduation
PoolManager transfer guard, the accounting invariant after every state change, capped push with
pull fallback, the #82 SWEEP fix, strict ES256 JWT parsing, image type and decode-bomb checks with
lossless metadata stripping, nonce CSP, the pinned trusted proxy, hardened containers and
SHA-pinned actions.
