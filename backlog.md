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

## Deferred

### Robinhood testnet live acceptance

- **Date:** 2026-10-04
- **Reason:** product-owner scope decision; the faucet limit is 0.05 test ETH/day and cannot
  practically fund the 4.2 ETH graduation threshold. The owner requested production preparation
  instead. This is not a passed live acceptance gate or permission to bypass production approval.
- **Verified progress:** Creator login and token launch succeeded on chain 46630. Transaction
  `0x8df54e34cc9e137113b03cd9ed46bb2e202e678762ac2d57c0d658ecdaee9114` has receipt status
  `0x1` at block `128661022`. Token: `0xf51c4cde6f9e168ee67ebea0ece130025bdca8ed`;
  curve: `0x84ed988b14736bbd477f9f8d74d8c6ef1fd6ed31`.
- **Remaining:** separate-trader buy/sell, graduation and burned LP, API/indexer visibility,
  finality/reorg acceptance. The local indexer was more than 9.7 million blocks behind the
  launch block; its catch-up must be addressed before relying on live API data.
- **Related files:** `docs/runbooks/robinhood-testnet-deployment.md`,
  `contracts/deployments/robinhood-testnet-v1.json`. Local receipt/evidence files are preserved
  in `backend/.cache/live-launch-*-20261004.json`; temporary acceptance services are stopped.
- **Resume:** Only when the owner resumes this scope and sufficient test funding exists, use
  creator `0x08B42F27E4CF57a8f46c0f7d2eE452BA43bdCAee` and distinct trader
  `0xf4cc408c6003ACD688b18DfDB00B0BCaaA02aC5A`; resolve indexer catch-up and finish the runbook.
  Do not change launch economics or invent a later indexing watermark to pass acceptance.

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
  first live end-to-end product acceptance remains separately tracked above.

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
