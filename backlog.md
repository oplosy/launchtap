# Backlog — Unfinished Work

> Unfinished work is noted here. Reason: ran out of time, scope decision, or
> usage limit about to run out. Goal: later, resume quickly from where we
> stopped **without rebuilding context from scratch**.

## How to use

- Every unfinished piece of work becomes an item under "Active".
- The **Resume** field is written clearly enough to include file paths,
  commands, last working state, and the next concrete step.
- When work is done, delete the item or move it to "Done".

---

## Active

### 2026-10-05 logic review — remaining remediation

- **Date:** 2026-10-05
- **Reason:** scope decision (fixed the security/speed findings that were safe to land in one
  pass on branch `task/audit-remediation`; the items below need deployment access, design
  work, or CI-only tooling)
- **Where it stopped:** Landed on `task/audit-remediation`: graduation-DoS recipient check,
  sells open during trading pause, `claim*To`, cursor validity across tip advances, per-client
  rate limit + SSE cap, LIKE escaping, image dimension limit + cache header, quote without
  per-trade sums, holder/pool-swap indexes, batched header RPC + in-process RPC backoff,
  nonce-based script CSP, 50% slippage cap, Caddy compression. Speed follow-ups also landed:
  topic-only curve log discovery, per-token SSE filter + coalescing, indexable metric sorts
  (`tokens.sort_*` mirrored by triggers), and an on-demand Privy/wagmi stack (non-wallet routes
  no longer download the ~2 MB Privy chunk up front).
- **Related files:** `contracts/src/BondingCurveV1.sol`, `contracts/slither.db.json`,
  `contracts/deployments/robinhood-testnet-v1.json`, `web/performance-budgets.json`,
  `web/scripts/check-budgets.mjs`, `deploy/Caddyfile`
- **Resume (next step):**
  1. Testnet: the deployed v1 curve implementation and every existing clone still accept a
     buy whose token recipient is the pair (permanent graduation freeze). Deploy the fixed
     `BondingCurveV1`, call `configureEngine(1, <new impl>, true)` through the timelock,
     record the new runtime-code hash in the deployment manifest, and re-run the indexer
     bytecode verification. Existing clones cannot be fixed.
  2. Run the manual `contracts` workflow (`Release gate`) to confirm the two re-triaged
     Slither findings; `contracts/slither.db.json` now stores those two as full Slither result
     objects (functional, but verbose) — trim them to the file's four-field shape if wanted.
  3. Validate `deploy/Caddyfile` and `deploy/compose.yaml` through the `deployment-package`
     workflow (`caddy validate` needs Docker, unavailable locally).
  4. Images: strip metadata by re-encoding and serve from object storage/CDN (infrastructure).
  5. Token and pair log discovery still use address batches (standard ERC-20/Uniswap topics
     cannot be queried by topic alone); revisit if token count makes this the bottleneck.
- **Pitfalls / notes:** Pages are now dynamically rendered because the CSP nonce is per
  request; keep that in mind for CDN caching. On non-wallet routes a previously connected
  wallet shows as disconnected until the reader clicks Connect or opens a wallet route (the
  stack is idle-prefetched but not mounted). The external audit is still required before
  mainnet; this review does not replace it.

### Production release, governance, and audit inputs

- **Date:** 2026-10-04
- **Reason:** production-only external coordination
- **Where it stopped:** A Docker Compose deployment package now includes pinned backend/web,
  PostgreSQL and HTTPS proxy images, migrations before API/indexer startup, public configuration
  preflight, external secret files, backup/restore instructions, and a disposable runtime check.
  Container build/runtime, database dump/restore, web unit/browser, and configuration rejection
  checks passed locally. Runtime npm audit has zero critical/high advisories, with 39 low/moderate
  advisories still requiring impact review. Hosting and domain are not provisioned; no mainnet
  manifest, production Privy allowlist, signer, monitoring owner, or external audit is invented.
- **Related files:** `docs/runbooks/production-readiness.md`,
  `docs/runbooks/web-release.md`, `web/.env.example`, `backend/.env.example`,
  `scripts/verify-release.mjs`, `deploy/README.md`, `deploy/compose.yaml`, `deploy/check.ps1`,
  `.github/workflows/deployment-package.yml`
- **Resume (next step):** Product/infrastructure/security owners must fill every `<pending>`
  row in `docs/runbooks/production-readiness.md`, provision values through the approved
  secret/variable manager, and run `node scripts/verify-release.mjs --target=production`.
  Provision hosting/domain and production Privy inputs, complete dependency impact review,
  obtain a reviewed mainnet deployment manifest, then build/promote using `deploy/README.md`.
  Production approval still requires signed governance and external-audit evidence. An independent
  infrastructure review of the deployment package is still outstanding.
- **Pitfalls / notes:** These do not authorize changing existing launch economics or adding
  a reserve rescue path. Do not commit credentials, private RPC URLs, wallet keys, or guessed
  deployment addresses.

<!-- Template:
### <short title>
- **Date:** YYYY-MM-DD
- **Reason:** time | limit (~__%) | scope decision
- **Where it stopped:** <what was done and the exact stopping point>
- **Related files:** <paths>
- **Resume (next step):** <concrete first step + command>
- **Pitfalls / notes:** <things to watch out for>
-->

---

## Done

### Robinhood testnet deployment manifest

- **Completed:** 2026-09-13
- **Evidence:** commits `4187301` and `0f198c2` contain the independently reviewed dependency
  record and active Launchpad manifest for chain `46630`. WETH is
  `0xcc02c43352422cc5ce27d8f222302050aeecbd6f`, Uniswap V2 Factory is
  `0x76748a790c21335ac0a170504e362b8859c514f5`, BondingCurveV1 is
  `0x979b9b8172fba6daec305e388fb500b32f740497`, and LaunchFactory is
  `0xedddb61a53226ffdc6ecf7c042b803e769168d55`.
- **Verification:** All four deployment receipts succeeded; live runtime-code hashes and
  LaunchFactory configuration getters matched the reviewed evidence. Foundry passed 97/97,
  the complete backend verification gate passed locally, and web tests, lint, typecheck,
  build, and deployment drift checks passed. GitHub contracts and web workflows for
  `0f198c2` passed; its backend workflow was still running when this item was closed.
- **Boundary:** This closes dependency bootstrap and deployment-manifest activation. The
  remaining live testnet acceptance was cancelled by the product owner on 2026-10-04
  because test funding is impractical; it was not verified or passed.

### ETH/USD enrichment source selection

- **Completed:** 2026-09-11
- **Evidence:** `docs/runbooks/eth-usd-enrichment.md` records the official Robinhood and
  Chainlink review and selects CoinGecko’s commercial `/simple/price` API as the optional
  cached source. The runbook defines authentication, commercial attribution, numeric
  handling, freshness, outage, and rate-limit semantics.
- **Boundary:** This closes source selection only. The provider adapter, source/retrieval
  timestamps, attribution UI, and production API key remain a future implementation and
  operations step; ETH-native indexing, quotes, and transactions remain independent.

### Robinhood RPC finality and getLogs capacity probe

- **Completed:** 2026-09-11
- **Evidence:** `docs/runbooks/robinhood-rpc-probe.md` records read-only measurements against
  the official mainnet (`4663`) and testnet (`46630`) endpoints. Both endpoints returned
  `latest`, `safe`, and `finalized`; fixed-height block hashes were stable across repeated
  reads; bounded `eth_getLogs` range, response-size, and address-array observations are
  recorded.
- **Limitation:** the finality sample is short (two samples over about 14.5 seconds), so it
  is not a long-run provider SLA or a statistically derived alert percentile.

### Task 11 pinned Robinhood mainnet fork acceptance

- **Completed:** 2026-09-03
- **Evidence:** GitHub Actions run `33733283872` passed both the normal Foundry gate and the
  explicit QuickNode-backed fork job at pinned block `53,240,126` on commit `d09ba9a`.

### Task 12 contract release gate

- **Completed:** 2026-09-04
- **Evidence:** GitHub Actions `workflow_dispatch` run `33811377759` passed the `Foundry`,
  `Release gate`, and `Robinhood mainnet fork` jobs on commit `e663eb0`. Closes Contract
  Foundations (Tasks 1-12); external audit stays out of scope under "Production governance
  and audit inputs".
