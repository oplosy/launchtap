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

### V4 swap sender backfill

- **Date:** 2026-11-11
- **Reason:** scope decision (Plan v2-B Task 7 wired V4 swaps into the live sender lookup only)
- **Where it stopped:** The engine attaches `tx_from` to every `V4Swap` through the same batched
  `SenderSource` lookup and miss counter as v1 pool swaps. A missed lookup stores the V4 swap with
  a NULL `tx_from`, and nothing retries it: `internal/swapbackfill` only scans `pool_swaps`.
- **Related files:** `backend/internal/swapbackfill/`, `backend/internal/indexer/engine.go`
  (`attachSenders`), `v4_swaps.tx_from` in migration `00024_engine_v2_ledger.sql`
- **Resume (next step):** Extend the backfill to `v4_swaps` rows with NULL `tx_from` once their
  block is safe, and add the matching position-dirty mark when Task 9 introduces V4 positions.
  Until then the "retries them once their block is safe" warning text is true for v1 swaps only.
- **Pitfalls / notes:** The V4 `Swap.sender` is the router; never use it as the trader (pre-flight R4).

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
  **Re-checked 2026-11-09:** the newest release, `@privy-io/react-auth` 3.51.3, still depends
  on `x402` ^0.7.1, which requires wagmi ^2.15.6 and zod ^3.24.2, so both highs remain.
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

### P0-C5 forum deferrals: ticker tape and votes

- **Date:** 2026-11-10
- **Reason:** scope decision (made while the owner was away; spec
  `docs/specs/2026-11-10-p0c5-forum-memestock-design.md` non-goals)
- **Where it stopped:** the forum shipped as read views over token comments. Two reference-screen
  pieces were left out:
  - The scrolling stock ticker tape. It needs a third-party equities quote feed (provider choice,
    licence for redistribution, an API secret, a new external dependency). Owner input.
  - Up/down votes and vote-ranked Hot. Needs a sybil rule (one Privy account per vote is cheap to
    farm) and moderation of vote abuse before it is worth building.
- **Related files:** `web/app/forum/page.tsx`, `web/src/forum/forum-view.tsx`,
  `backend/internal/forum/`.
- **Resume (next step):** owner picks an equities data provider (or drops the tape); for votes,
  decide whether only holders may vote (reusing the C2 holder gate) before a spec.
- **Pitfalls / notes:** the tape must stay a client-side widget with its own failure state so a
  provider outage never blocks the forum.

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

### P0-C2 signed-in e2e coverage

- **Date:** 2026-11-10
- **What closed:** a test-only session for the fixture build. A browser test sets
  `window.__e2eSession` before load; `Providers` then loads `src/e2e/session-provider.tsx`, which
  feeds `WatchlistAuthContext` (comment composer, inbox) and `E2ESessionContext` (`ProfileView`)
  with fixed tokens instead of Privy. `/profile` and `/notifications` render their views in the
  fixture build; without Privy or a test session `ProfileView` still shows "Account unavailable".
  `web/e2e/signed-in.spec.ts` (in `test:e2e:live`) drives the composer (wallet choice, client
  rule, plain-text render, 429 message keeps the draft), the holder-less note, the username form
  (client rule, 409 `username_taken`, save with a published wallet) and the inbox (plain-text
  names, New marker, mark-read `through`, load more); every mock asserts the session's
  `Authorization` and `privy-id-token` headers.
- **Never ships:** the provider is imported as `launchpad-e2e-session`, aliased in
  `web/next.config.ts` to the real module only when `NEXT_PUBLIC_E2E_FIXTURE=1` and to an inert stub
  otherwise. Checked: a build with the flag forced to `0` contains no provider code (a dead-branch
  `import()` alone still emitted the chunk under Turbopack). Note `web/.env.local` sets the
  fixture flag, so a local "production" check must override it in the process environment.
- **Verified:** web typecheck, lint, vitest 410, `test:e2e` 79 passed, `test:e2e:live` 78 passed
  (74 + 4 new).

### P0-C2 parked review minors

- **Date:** 2026-11-10
- **Commits:** `2a3d18c`, `77699b7` on `dev`, plus this backlog update.
- **What closed:**
  - Exact `Retry-After`: `CommentRateState`/`ReportRateState` return `release_at`, the cap-th
    newest row in the window (cap+1 for reports, whose count includes the new row), so a window
    holding more rows than the cap answers the true wait instead of the oldest row's.
  - `POST /tokens/{address}/comments`: the store normalizes the body after the ban check, so a
    banned caller always gets `banned`; `CommentStore.Post` now takes the raw body.
  - SSE tests: comment hints on the unfiltered stream with a stalled subscriber evicted alone;
    the operator hide notification decodes as a realtime comment hint; post/ban race in both
    orders (`TestPostAndBanSerialize`).
  - Rejected: removing comment hints from the unfiltered stream. The web opens one unfiltered
    `/v1/events` stream per tab and the token page scopes comment hints to its token client-side,
    so filtering them out would stop live comment refreshes. Reconnect replay is by design absent
    (refresh-only stream); the token page refetches comments on reconnect.
- **Verified:** `task verify` (unit/race, integration, openapi/sqlc/deployment/vector diffs,
  build, govulncheck) passed; the anvil indexer e2e passed with Foundry on `PATH`; lint reports
  only the known CRLF false positive in `deployments/registry_test.go`.
- **Still open:** none (signed-in e2e closed, see "P0-C2 signed-in e2e coverage").

### P0-C5: forum (Memestock)

- **Date:** 2026-11-10
- **Commits:** see `git log --grep "P0-C5"` on `dev` (spec
  `docs/specs/2026-11-10-p0c5-forum-memestock-design.md`; its acceptance criteria served as the
  plan).
- **What shipped:** migration 00023 (partial indexes on visible comments);
  `GET /v1/forum/posts` (cursor, limit 1-50, `public, max-age=5`) and
  `GET /v1/forum/communities?sort=hot|top` (`public, max-age=15`, 400 `invalid_sort`), both with
  C2's visibility, ban and author-display rules and the same token-existence rule; web `/forum`
  with New / Hot / Top tabs in the URL, "Load more", Trade/Discuss links and a biggest-launches
  sidebar; a Forum navbar link (Docs moved to the menu sheet and footer so the desktop bar fits).
- **Gate evidence:** backend unit/race (`internal/forum`, forum routes), integration
  (`TestForumFeedAndCommunities`: paging, hidden/banned/other-deployment exclusion, display rule,
  24 h window, tie-break, reorged token; migration pins at 23), lint incl. integration tag,
  openapi; web vitest 410, lint, typecheck, `test:e2e` (accessibility now covers `/forum` and
  `/notifications`), `test:e2e:live` 74 passed (incl. hostile text, load more, tabs); the page
  checked in a browser at 1280 px and 375 px (no horizontal overflow).
- **Deferred:** ticker tape and votes (Active, "P0-C5 forum deferrals").
- **Next:** the `dev` → `main` milestone PR.

### P0-C4: server-side notification inbox

- **Date:** 2026-11-10
- **Commits:** see `git log --grep "P0-C4"` on `dev` (spec
  `docs/specs/2026-11-10-p0c4-notification-inbox-design.md`; its acceptance criteria served as
  the plan).
- **What shipped:** view `inbox_events` and table `notification_reads` (migration 00022);
  `GET /v1/me/notifications` (cursor, limit 1-50, unread count capped at 100) and
  `PUT /v1/me/notifications/read` (forward-only), both Privy-authenticated and `private,
  no-store`; web `/notifications` page with "Load more", "New" markers and mark-read on view; an
  "Inbox" link with the server unread count in the bell panel.
- **Gate evidence:** backend unit/race (`internal/inbox`, inbox routes), integration
  (`TestInboxDerivedFromWatchlistAndLedger` incl. pagination, scoping, forward-only reads and a
  reorg; `TestInboxIgnoresEventsOlderThan30Days`; migration up/down pins at 22), lint incl.
  integration tag, openapi; web vitest 406 (incl. the 0.5 ETH threshold pinned against the
  migration), lint, typecheck, `test:e2e:live` 68 passed.
- **Not covered end to end:** the signed-in inbox list and the bell's Inbox link are not driven
  in a browser, because the fixture build has no real Privy session; the e2e covers the
  signed-out prompt and asserts no inbox request is sent without credentials. The list is covered
  by the API, client and model tests.
- **Next sub-project:** Forum (Memestock).

### P0-C3: watch counts and 7d/30d leaderboards

- **Date:** 2026-11-10
- **Commits:** `91fd3e3..` on `dev` (spec
  `docs/specs/2026-11-10-p0c3-watch-counts-windowed-leaderboards-design.md`; small enough that
  the spec's acceptance criteria served as the plan).
- **What shipped:** the position worker writes per-leg realized PnL (`wallet_realized_legs`,
  migration 00021); `GET /v1/leaderboard?window=all|7d|30d` (400 `invalid_window`);
  `GET /v1/tokens/{address}/watchers`; web leaderboard period tabs in the URL and "N watching"
  beside the token star.
- **Gate evidence:** backend unit/race, integration (incl. `TestRealizedLegsMatchLedger`,
  `TestLeaderboardWindows`, `TestCountWatchersScopedToDeployment`, reorg equality now covering
  realized legs), `task verify`; web vitest 401, lint, typecheck, `test:e2e:live` 65 passed;
  leaderboard tabs checked in the browser against a stub API (desktop and 375 px).
- **Not covered end to end:** no Anvil-gate assertion for the windowed board (Anvil block
  timestamps of a pinned fork are not guaranteed to be within 7 days of the DB clock); the window
  math is covered by the integration tests.
- **Upgrade step:** run `backfill-swap-sender -seed-positions` once (runbook "Windowed
  leaderboards (P0-C3)").
- **Next sub-project:** C4 (server-side notification inbox), then Forum (Memestock). C4 shipped; see above.

### P0-C2: token comments, usernames and moderation

- **Date:** 2026-11-10
- **Commits:** `0b173a6..5f2d9a7` on `dev` (plan `docs/plans/2026-11-09-p0c2-comments-usernames.md`,
  spec `docs/specs/2026-11-09-p0c2-comments-usernames-design.md`), plus the Task 9 docs commit.
- **What shipped:** holder- or creator-gated token comments with per-user rate limits, reports,
  operator hide/ban (`cmd/moderate`, runbook "Moderating comments"), SSE comment hints, usernames
  with a 7-day change interval and a 30-day release hold, published wallets (at most 5) shown on
  comments, the wallet page and the leaderboard.
- **Gate evidence:** see the Task 9 commit message: backend build, unit/race, integration,
  sqlc/openapi/migration checks and the Anvil indexer E2E; web vitest, lint, typecheck,
  `test:e2e:live` and `test:anvil`. Only the known Windows CRLF false positives failed locally.
- **Upgrade note:** migration `00020_social` Down drops all comments, reports, usernames and
  published wallets.
- **Next sub-project:** P0-C3 (watchlist counts and 7d/30d leaderboards).
- **Parked minors:** closed, see Done "P0-C2 parked review minors".

### P0-B and P0-C1 follow-ups: live feed, wallet/PnL review minors

- **Date:** 2026-11-09
- **Commits:** `a8b286e` and earlier back to `51ea316` (backend: sender retry, position worker
  throughput, leaderboard filter), `42b249a..e7aaf1d` (feed tail), `d76f7b1..8c7b236` (web), on
  `dev`, plus this backlog update.
- **What closed:**
  - Indexer retries missed pool-swap senders itself (resume cursor below the safe head) with
    `launchpad_indexer_sender_backfill_{filled_total,pending}` metrics; miss counter counted after
    commit.
  - Position worker drains full batches; migration 00019 indexes the claim; `-seed-positions`
    runs per token. Leaderboard lists only wallets with positive realized PnL.
  - Feed tail: rewind to the safe block on LISTEN reconnect without re-publishing delivered rows;
    launches share the per-second budget; graduations are never folded; events publish in chain
    order; `Start` guard; pre-start reorg hints dropped; bursts carry `max_eth_wei`, `traders`.
  - Web: burst large/own-trade notifications with replay dedupe; `reconnected` on `open`; tabs
    hidden 60 s release their SSE stream; strip token images; live row prices; 1 s metadata
    bound; leaderboard error boundary with `retry`; wallet page delayed refetch, kept pages,
    load-more abort; holdings refresh on own trade seen; share cards fall back to `[]` symbols
    and cache API-failure cards 30 s downstream; e2e/tooling fixes and `.gitattributes`.
- **Gate evidence:** backend build, unit/race, integration, sqlc/openapi/migration checks; web
  vitest (353), lint, typecheck, `test:e2e:live` (63 passed, 9 skipped), `test:anvil` (22 passed).
  Only the known Windows CRLF false positives failed locally.
- **Known residuals (accepted, not tracked):** the feed drain has no per-drain upper block bound
  (documented in `drainMerged`); the replay identity key omits the block number; burst/replay
  dedupe uses window and amount heuristics, so an equal-amount new trade can lose its alert;
  Next keeps a generic share card 300 s in-process even when downstream caches use 30 s.

### P0-C1: public wallet page, PnL and leaderboard

- **Date:** 2026-11-09
- **Commits:** `927ee70..e63c0a9` on `dev` (plan `docs/plans/2026-11-06-p0c1-wallet-page-pnl.md`,
  spec `docs/specs/2026-11-06-p0c1-wallet-page-pnl-design.md`), plus this backlog update.
- **Gate evidence:** the Anvil gate (`cd web && npm run test:anvil`, 22 passed) asserts the
  wallet page's realized PnL after curve buy, curve sell and pool sell without reload, exactly
  in wei through the API and at display precision on the page, against a value derived from
  the receipts (AC4). Backend build, unit/race, integration, sqlc and openapi checks, web
  lint/typecheck/test (301) and the live e2e suites (37 passed, 3 skipped) passed locally;
  failures were only the known Windows CRLF false positives on untouched files.
- **Upgrade step:** on a database with history, run the sender backfill and then
  `backfill-swap-sender -seed-positions` (runbook "Backfill pool swap senders (P0-C1)").
- **Next sub-project:** P0-C2 (comments).
- **Follow-ups:** closed 2026-11-09 (see "P0-B and P0-C1 follow-ups").

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
- **Follow-ups:** closed 2026-11-09 (see "P0-B and P0-C1 follow-ups").

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
