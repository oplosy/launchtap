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

### P0-A external inputs: CoinGecko key and market-link identifiers

- **Date:** 2026-11-05
- **Reason:** scope decision (needs a commercial account and verified third-party identifiers)
- **Where it stopped:** P0-A market data and discovery is implemented and verified. The USD line
  and the Dexscreener/GeckoTerminal links stay hidden until two external inputs exist; nothing
  is invented in the meantime.
- **Related files:** `backend/internal/config/config.go`, `deploy/backend.env.example`,
  `contracts/deployments/`, `backend/deployments/testdata/`, `web/src/contracts/`
- **Resume (next step):**
  1. Provision a CoinGecko commercial API key as the backend secret `COINGECKO_API_KEY` and set
     `ETH_USD_SOURCE=coingecko` in the deployment environment.
  2. Identifiers verified 2026-11-06: Dexscreener chain `robinhood` and GeckoTerminal network
     `robinhood`. Both index Robinhood Chain **mainnet** only (sample pairs/pools have code on
     `rpc.mainnet.chain.robinhood.com` and none on testnet), so they must not go into
     `robinhood-testnet-v1.json`. Add `"marketLinks": {"dexscreenerChain": "robinhood",
     "geckoterminalNetwork": "robinhood"}` to the reviewed mainnet manifest when it exists, copy
     it to the backend testdata, then run `cd web && npm run web-contracts-sync`.
  3. Once the key is live, measure CoinGecko's `last_updated_at` lag. If the USD line flickers
     against the 120 s freshness budget, make the poll jitter symmetric and allow a few seconds
     of future-skew tolerance (`backend/internal/pricefeed/feed.go` ~201 and ~258).
- **Pitfalls / notes:** Never commit the key. The manifest copies must stay byte-identical
  (`task verify` checks drift).

### Wallet SDK upgrade: Privy 3 to clear allowlisted advisories

- **Date:** 2026-11-06
- **Reason:** scope decision (major-version auth/wallet upgrade, needs its own review)
- **Where it stopped:** The runtime audit gate allowlists two high advisories that only the
  wallet SDK chain pulls in: zod 3.x (GHSA-mrhx-c7xc-m3w5) and decode-uri-component 0.2.x
  (GHSA-qr28-fpjm-wq7f), both reached through `@privy-io/react-auth` 2.17.3, WalletConnect and
  Reown. Their only fixes are zod 4 and an ESM-only decode-uri-component, which the current
  SDKs cannot take. Both entries expire on 2027-02-01, after which CI fails again.
- **Related files:** `web/package.json`, `web/package-lock.json`, `web/audit-allowlist.json`,
  `web/src/wallet/`, `web/src/providers.tsx`
- **Checked 2026-11-06:** Privy 3 does **not** clear them. `@privy-io/react-auth` 3 depends on
  `x402`, which pulls wagmi 2 → WalletConnect 2.21.1 → `decode-uri-component` 0.2.2, and zod 3
  still arrives through `x402`, Reown and `abitype`; wagmi 3 changes nothing. Details:
  `docs/audits/runtime-advisory-review-2026-11-06.md`. The upgrade was not done.
- **Resume (next step):** Before 2027-02-01, re-run the trial install from that document with
  the then-current Privy release. Upgrade only once `npm audit --omit=dev` on the trial tree no
  longer reports the two highs; otherwise renew the allowlist entries with a fresh impact review.
  Decide with the owner whether to enable Dependabot or Renovate for version-update PRs.
- **Pitfalls / notes:** This touches wallet authentication; treat it as high-risk. Do not
  extend the allowlist dates without a fresh impact review.

### Operator actions: testnet engine switch and fork RPC secret

- **Date:** 2026-10-05
- **Reason:** scope decision (needs the owner's keystores and repository secrets)
- **Where it stopped:** `contracts/script/DeployCurveImplementation.s.sol` and the runbook
  section "Engine implementation upgrade" in `docs/runbooks/robinhood-testnet-deployment.md`
  are merged (PR #11); the fork dry-run passed. The testnet v1 curve implementation and every
  existing clone still accept a buy whose token recipient is the pair (permanent graduation
  freeze). Separately, the `ROBINHOOD_MAINNET_ARCHIVE_RPC_URL` repository secret fails the TLS
  handshake (`received fatal alert: InternalError`, contracts run `37326499597`, both
  attempts), so the `Release gate` and `Robinhood mainnet fork` jobs cannot pass until it is
  replaced; the Slither step of the same run passed.
- **Related files:** `contracts/script/DeployCurveImplementation.s.sol`,
  `docs/runbooks/robinhood-testnet-deployment.md`,
  `contracts/deployments/robinhood-testnet-v1.json`, `backend/deployments/testdata/`,
  `.github/workflows/contracts.yml`
- **Resume (next step):**
  1. Follow the runbook: deploy the implementation with the deployer keystore, fund the
     timelock, and call `configureEngine(1, <new impl>, true)` from the timelock keystore.
     With the transaction hashes, record `curveImplementation` and
     `bytecodeHashes.bondingCurveV1` in the manifest and its backend testdata copy, regenerate
     the web contracts, and re-run the indexer bytecode verification.
  2. Replace the archive RPC secret with a working endpoint and re-run the `contracts`
     workflow through `workflow_dispatch`.
- **Pitfalls / notes:** Existing clones cannot be fixed. Do not commit the RPC URL, keys, or a
  predicted implementation address before its deployment receipt exists.

### P0-B follow-ups: live feed minors deferred in review

- **Date:** 2026-11-06
- **Reason:** scope decision (minor findings from the P0-B task and final reviews)
- **Where it stopped:** P0-B is done (see Done). These were judged minor and deferred.
- **Related files:** `backend/internal/feed/tail.go`, `backend/cmd/api/main.go`,
  `web/src/live/`, `web/src/token/token-detail.tsx`, `web/e2e/`,
  `web/scripts/run-e2e-live-strip.mjs`, `.gitattributes`
- **Resume (next step):** pick from this list, highest first:
  1. Backend tail: rewind to the safe block when the Postgres LISTEN connection reconnects (a
     lost `reorg` hint means no rewind); rate-limit or batch `new_token`/`graduation` like
     trades (a launch-heavy block can evict subscribers with the 64-event buffer); publish
     launches/trades/graduations in global chain order; guard `Tail.Start` against a second
     call; a reorg hint before `Newest` succeeds replays history.
  2. Notifications: large-trade and own-trade notifications are lost when trades fold into
     `trade_burst`; own pool (dex) trades never notify because the trader is null.
  3. Web: emit the `reconnected` signal on the EventSource `open` event, not right after
     `connect()`, to close the last small recovery gap (`web/src/api/sse.ts` ~131); catch the
     harmless abort rejection when a reconnect reload cancels a reorg refresh on the token page;
     show the token image in the strip (spec item); live token-page rows show "—" prices
     until the next REST refresh; `generateMetadata` can block up to 3 s on a slow API; a
     generic share card served during an API outage stays cached up to 300 s.
  4. Tests/tooling: `.gitattributes` `eol=lf` for `backend/openapi/v1.json` (local CRLF makes
     `check-openapi` report stale); live e2e excludes "mobile footer" (fix: wait for the
     `/create` panel to settle) and Windows `shell: true` splits `--grep-invert`; release-hardening
     SVG clipping exemption is broader than needed (skip opacity-0 elements instead); no e2e for
     bell focus-out or the browser reorg flow.
  5. Ops: 5+ tabs from one client exceed `API_SSE_MAX_PER_CLIENT` (default 4) because the stream
     is app-wide; document share-card render cost per distinct address in the runbook.
- **Pitfalls / notes:** keep amounts as decimal strings / `*big.Int`; the web unit tests run in
  node only (no DOM), so put logic in pure `.ts` modules.

### Production release, governance, and audit inputs

- **Date:** 2026-10-04
- **Reason:** production-only external coordination
- **Where it stopped:** A Docker Compose deployment package now includes pinned backend/web,
  PostgreSQL and HTTPS proxy images, migrations before API/indexer startup, public configuration
  preflight, external secret files, backup/restore instructions, and a disposable runtime check.
  Container build/runtime, database dump/restore, web unit/browser, and configuration rejection
  checks passed locally. The runtime npm audit gate (`npm run audit:runtime`) passes with two
  reviewed high advisories allowlisted (see the wallet SDK item) and 23 low/moderate package findings
  (7 advisories) reviewed in `docs/audits/runtime-advisory-review-2026-11-06.md`: none reachable
  with impact beyond the user's own browser tab. Hosting and domain are not provisioned; no mainnet
  manifest, production Privy allowlist, signer, monitoring owner, or external audit is invented.
- **Related files:** `docs/runbooks/production-readiness.md`,
  `docs/runbooks/web-release.md`, `web/.env.example`, `backend/.env.example`,
  `scripts/verify-release.mjs`, `deploy/README.md`, `deploy/compose.yaml`, `deploy/check.ps1`,
  `.github/workflows/deployment-package.yml`
- **Resume (next step):** Product/infrastructure/security owners must fill every `<pending>`
  row in `docs/runbooks/production-readiness.md`, provision values through the approved
  secret/variable manager, and run `node scripts/verify-release.mjs --target=production`.
  Provision hosting/domain and production Privy inputs, choose object storage/CDN delivery
  for token images (today they are stored in PostgreSQL and served by the API with an ETag and
  a short public cache),
  obtain a reviewed mainnet deployment manifest, then build/promote using `deploy/README.md`.
  Production approval still requires signed governance and external-audit evidence. An independent
  infrastructure review of the deployment package is still outstanding.
- **Pitfalls / notes:** These do not authorize changing existing launch economics or adding
  a reserve rescue path. Do not commit credentials, private RPC URLs, wallet keys, or guessed
  deployment addresses. The monitoring owner and the real alert destination
  (`ALERTMANAGER_WEBHOOK_URL_FILE`, see the runbook "Monitoring" section) are also still unassigned.

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

### P0-B: live trade feed, in-app notifications and share cards

- **Date:** 2026-11-06
- **Commits:** `145bf0f..727317b` on `dev` (plan `docs/plans/2026-11-06-p0b-live-feed.md`,
  spec `docs/specs/2026-11-06-p0b-live-feed-notifications-share-design.md`), plus this backlog
  update.
- **Gate evidence:** the Anvil gate (`cd web && npm run test:anvil`, 20 passed) asserts that a
  buy shows in the Explore strip and in Recent trades without reload within 3 s (AC1).
  `task verify`, web lint/typecheck/test (267) and the token-detail and live-strip e2e suites
  passed locally. The gate has no reorg path for the feed; reorg removal is covered by
  `backend/internal/feed/tail_test.go`, `web/src/live/feed-store.test.ts` and
  `web/src/live/live-strip-model.test.ts` (token-page reload after a reorg).
- **Sub-project C scope note:** notifications matching on wallet holdings (needs a public
  wallet-holdings read), the profile/wallet share card, public wallet page and PnL move to
  sub-project C. P0-B notifications cover watched tokens and the signed-in wallet's own trades.
- **Open follow-ups:** see the Active item "P0-B follow-ups".

### Observability: metrics and alerting

- **Completed:** 2026-11-06
- **Evidence:** commits c7a377b, eabb748, 975464c, 48844a9, 5298920, 480be42, 5bb9da9. The API
  (private `API_METRICS_ADDR` listener) and the indexer (`/metrics` on its health port) export
  Prometheus metrics, including ETH/USD price feed, indexer, RPC and aggregation series. An
  opt-in Compose `monitoring` profile runs Prometheus and Alertmanager with tested alert rules
  (`deploy/monitoring/alerts.yml`, `alerts.test.yml`). The runbook "Monitoring" section documents
  enabling it, the webhook secret, access, retention and the alert table. Gate output is in
  `.superpowers/sdd/2026-11-06-observability/task-6-report.md`.
- **Boundary:** Error tracking (Sentry or similar) was deferred by the owner; it is not an
  active item. The monitoring owner and alert destination stay under "Production release,
  governance, and audit inputs". The API rate limiter stays per-process, so one API instance
  is the supported topology. `WriteRejectionSpike` and `RPCErrorRateHigh` differ from the spec
  text as described in the runbook.

### P0-A parked review minors and dependency scanning

- **Completed:** 2026-11-06
- **Evidence:** `govulncheck` v1.9.0 runs in `task verify` (`vulncheck` task). It found 15
  reachable vulnerabilities, all closed: the Go toolchain moved to 1.26.10 (`toolchain`
  directive and `backend/Dockerfile`), and klauspost/compress, x/crypto, gorilla/websocket
  and moby/go-archive were raised to fixed versions. The web workflow runs
  `npm run audit:runtime` (`web/scripts/audit-runtime.mjs`), which fails on any high or
  critical runtime advisory missing from the reviewed, expiring `web/audit-allowlist.json`.
  Next.js moved to 16.4.5 for the critical image-optimizer RCE (GHSA-2464-fhr4-w9xw), and
  `npm audit fix` cleared follow-redirects, sharp and source-map-js. Highlights polling now
  pauses while the tab is hidden and shows a staleness cue after two failed refreshes. The
  watchlist log test asserts the response's own request ID.
- **Boundary:** The local Anvil gate "flake" was not a product failure: in both runs the
  indexer logged a graceful `indexer stopped error="context canceled"`, meaning the gate's
  console was interrupted from outside (an agent tool timeout), not a crash. Run the gate in
  the background or with a long timeout. The price freshness tuning moved to the P0-A external
  inputs item because it needs the real key.

### 2026-10-05 logic review remediation

- **Completed:** 2026-10-05
- **Evidence:** PR #9 landed the security and speed fixes (graduation-DoS recipient check,
  sells open during pause, `claim*To`, cursor validity across tip advances, per-client rate
  limit and SSE cap, image decode-bomb limit, nonce CSP, slippage cap, read-path indexes,
  batched header RPC, topic-only curve discovery, SSE filtering/coalescing, indexed metric
  sorts, on-demand wallet stack). PR #11 added the engine upgrade script and runbook; PR #12
  split performance budgets by route class. Contracts run `37326499597` printed
  `Slither analysis verified.` for the re-triaged `contracts/slither.db.json`. The
  `deployment package` workflow validates the Caddyfile with `caddy validate` on every PR and
  now also resolves `deploy/compose.yaml` and rejects a missing required value. Token image
  uploads are stored without EXIF, XMP, IPTC, comments, or embedded previews
  (`backend/internal/apiserver/image_metadata.go`, lossless container rewrite, no pixel
  re-encoding).
- **Boundary:** Token and pair log discovery stays address-batched by design: ERC-20
  `Transfer` and Uniswap `Swap`/`Sync` topics are shared by every token and pair on the chain,
  so a topic-only query would scan unrelated contracts. Revisit only if measurements show
  the address batches as the bottleneck. The testnet engine switch moved to "Operator
  actions"; image object storage/CDN moved to the production item. Images uploaded before the
  metadata change keep their metadata until re-uploaded. Two `slither.db.json` entries remain
  stored as full Slither result objects (functional, verbose).

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
