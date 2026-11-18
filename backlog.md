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

### Operator-only inputs (accounts, keystores, secrets)

- **Date:** 2026-11-17 (merges the CoinGecko, Telegram, testnet engine switch, production
  release, M-3 provider and funding requeue items; first opened 2026-10-04)
- **Reason:** each step needs an account, a keystore, a secret or a signature only the owner
  holds. Every product and code decision is made; nothing here needs more code.
- **Where it stopped:** code, specs and runbooks are merged. The choices below are locked; only
  their execution is left.
- **Related files:** `docs/runbooks/production-readiness.md`,
  `docs/runbooks/robinhood-testnet-deployment.md`, `deploy/README.md`,
  `deploy/backend.env.example`, `docs/release-checklist.md`, `contracts/deployments/`,
  `backend/deployments/testdata/`.
- **Resume (next step), in this order:**
  1. **Testnet engine switch** (runbook "Engine implementation upgrade"): deploy the
     implementation with the deployer keystore, fund the timelock, call
     `configureEngine(1, <new impl>, true)` from the timelock keystore, then record
     `curveImplementation` and `bytecodeHashes.bondingCurveV1` in the manifest and its backend
     testdata copy and run `cd web && npm run web-contracts-sync`. Existing clones cannot be fixed.
  1b. **Engine 2 testnet rehearsal (gap G2):** the tooling is merged
     (`docs/specs/2026-11-18-v2-testnet-path-preflight.md`). Run the runbook section "Engine 2 on
     testnet" in `docs/runbooks/robinhood-testnet-deployment.md`:
     - `bootstrap-testnet-v4.ps1` with the deployer keystore (about 0.001 testnet ETH);
     - review and commit the v4 record;
     - `deploy-v2-testnet.ps1`;
     - review and enable the manifest;
     - the full journey through graduation and LP-fee claims.
     The live dry run passed on 2026-11-18.
  1c. **Incident-response rehearsal (gap G8):** run `docs/runbooks/incident-response.md` §8 on
     the testnet factory with the testnet pause-authority keystore: pause, verify, unpause, and
     log the row. Repeat the Safe dry run once the mainnet multisig exists (step 6). Also
     confirm the §2 response targets and the status channel in the production sheet.
  2. **Archive RPC secret:** replace `ROBINHOOD_MAINNET_ARCHIVE_RPC_URL` (fails the TLS handshake
     since contracts run `37326499597`) and re-run the `contracts` workflow via
     `workflow_dispatch`; the fork gate on the release checklist needs it too.
  3. **RPC providers (M-3):** set `RPC_FALLBACK_URL` to a second provider (decision: a different
     company than `RPC_URL`), and confirm the production provider accepts 1,000-address
     `eth_getLogs` arrays and 10-block topic-only reads; if not, lower
     `INDEXER_LOG_ADDRESS_BATCH_SIZE` to its limit.
  4. **CoinGecko Demo key:** create it, set `COINGECKO_API_KEY`, `ETH_USD_SOURCE=coingecko` and
     `COINGECKO_API_PLAN=demo` in `BACKEND_ENV_FILE`. If the USD line flickers against the 120 s
     freshness budget, make the poll jitter symmetric (`backend/internal/pricefeed/feed.go`).
  5. **Telegram bot:** create it with BotFather, set `TELEGRAM_BOT_TOKEN` and
     `TELEGRAM_BOT_USERNAME`, start `docker compose --profile telegram up -d`, and send `/start`
     once through `/notifications` (only tested against a fake Bot API so far).
  6. **Production:** fill every `<pending>` row in `docs/runbooks/production-readiness.md`
     (hosting/domain, Privy app and origin, edge rate limits with a per-prefix connection cap,
     monitoring owner and `ALERTMANAGER_WEBHOOK_URL_FILE`, multisig, timelock >= 48 h, treasury,
     external audit, legal/geo), obtain the reviewed mainnet manifest with
     `"marketLinks": {"dexscreenerChain": "robinhood", "geckoterminalNetwork": "robinhood"}`
     (mainnet only, never testnet), run `node scripts/verify-release.mjs --target=production`,
     then build and promote with `deploy/README.md`. Token images stay in PostgreSQL behind the
     API's ETag cache until a CDN is chosen with hosting.
  7. **After the first deployment:** run `go run ./cmd/funding requeue-unknown` once with the
     funding service's environment, and measure the funding pass with `pg_stat_statements`.
- **Pitfalls / notes:** never commit keys, RPC URLs, wallet keys or a predicted address before
  its receipt exists. Manifest copies must stay byte-identical (`task verify`). Exactly one
  Telegram worker per deployment.

### Wallet SDK upgrade: Privy 3 to clear allowlisted advisories

- **Date:** 2026-11-06
- **Reason:** blocked upstream (major-version auth/wallet upgrade, high-risk)
- **Where it stopped:** the runtime audit gate allowlists zod 3.x (GHSA-mrhx-c7xc-m3w5) and
  decode-uri-component 0.2.x (GHSA-qr28-fpjm-wq7f), reached through `@privy-io/react-auth`
  2.17.3, WalletConnect and Reown; both entries expire on 2027-02-01. Privy 3 does not clear
  them: re-checked 2026-11-17, the newest `@privy-io/react-auth` 3.51.4 still depends on
  `x402` ^0.7.1, and the newest `x402` (1.2.0) still requires wagmi ^2.15.6 and zod ^3.24.2.
  Trial lockfile evidence: `docs/audits/runtime-advisory-review-2026-11-06.md`.
- **Related files:** `web/package.json`, `web/package-lock.json`, `web/audit-allowlist.json`,
  `web/src/wallet/`, `web/src/providers.tsx`
- **Resume (next step):** before 2027-02-01, run `npm view @privy-io/react-auth@latest
  dependencies` and `npm view x402 dependencies`; upgrade only when neither pulls zod 3 or
  wagmi 2, otherwise renew the allowlist entries with a fresh impact review.
- **Pitfalls / notes:** do not extend the allowlist dates without that review.

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

### Audit follow-ups: pass 3, run-4 leads, AUDIT_REPORT_TR and second report

- **Date:** 2026-11-17
- **What closed:** every finding of production-readiness audit pass 3
  (`docs/audits/2026-11-16-production-readiness-audit-3.md`; M-1 #89, M-2/L-2/L-3/L-4 #91, L-1
  #92, M-3 code #93 and #94), the 26 security-audit run-4 leads
  (`docs/specs/2026-11-17-run4-owner-defaults-preflight.md`), the `AUDIT_REPORT_TR.md`
  CRIT/HIGH/MED items (P0, P1 and MED passes, HIGH-13/14 a-e) and the second report's M-2, M-5,
  L-1 and L-4. Rejected with reasons in those specs: MED-07, MED-10 "dead code", MED-05 part 1,
  MED-01 WebP, the inbox claim, `ListTokenTransfersTouching`, second-report L-2 (static Privy
  verification key: rotation is an operator restart), L-3 (server-side launch simulation: the
  contracts enforce the rules) and L-5 (`prices.Run`/`tail.Start` return only when their context
  ends). Left to the operator: "Operator-only inputs" (Active).

### Product scope decisions: ticker tape and clusters Level 2

- **Date:** 2026-11-14
- **What closed:** the owner dropped the forum ticker tape and early-buyer clusters Level 2 from
  scope (`docs/specs/2026-11-14-product-scope-decisions.md`). Level 1 clusters stay live; the
  Level 2 code stays built and switched off.

### Telegram alerts

- **Date:** 2026-11-12
- **What closed:** the notes.md "cheap v1 win" Telegram alerts, per
  `docs/specs/2026-11-12-telegram-alerts-design.md`: opt-in through a one-time deep link
  (SHA-256-stored, 15 minutes, single use), one private chat per account, settings for
  graduations, large trades and a 0.5-10,000 ETH threshold, delivery of the account's inbox
  events from safe blocks only behind a forward-only cursor (migration 00028), plain-text
  messages, `/stop` and 403 unlink. The bot setup itself stays an active item.

### Engine v2 protocol revenue projection

- **Date:** 2026-11-12
- **What closed:** built as a read-at-query-time aggregate with its first consumer, per
  `docs/specs/2026-11-12-protocol-revenue-design.md`: `GET /v1/stats/protocol/revenue` sums the
  ETH credited to the protocol (launch fees, trade-fee protocol share, snipe tax, graduation ETH
  remainder, LP-fee protocol share mirroring `LpFeeLocker._credit`) per engine, and Analytics
  shows a "Protocol revenue" card. No projection table; move it into `protocol_stats` if the
  sums get slow.

### Engine v2 backend limitations: V4 positions, valuation, leaderboard

- **Date:** 2026-11-12
- **What closed:**
  - V4 swaps are priced position legs of their `tx_from` (`ListPositionLegs`), marked dirty on
    ingest, in the sender backfill, in the position seed and on reorg, with the zero address,
    factory, curve, locker and PoolManager excluded (`token_holder_excluded`).
  - Graduated engine-2 holdings are valued at the V4 spot price (`GetWalletValuation`,
    `ListWalletOpenPositions`) instead of 0.
  - Both leaderboards exclude every engine-2 locker and the chain's PoolManager.
  - Migration 00026 adds `v4_swaps_tx_from_null_idx` for the backfill's per-pass count.
  - V4 pool liquidity stays NULL by design (Plan v2-B pre-flight R5: no full-range math; the
    locker position is shown separately). Won't fix; the token page keeps its "not indexed yet"
    liquidity cell.
- **Left open:** the protocol revenue projection (Active).

### Plan v2-C Task 9 follow-up: pool id link

- **Date:** 2026-11-12
- **What closed:** won't fix. A V4 pool id is a `bytes32` key inside the PoolManager, not an
  address, and the chain explorer has no V4 pool page, so a link would point nowhere. The token
  page keeps showing the pool id as copyable text.

### V4 swap sender backfill

- **Date:** 2026-11-12
- **What closed:** the sender backfill (indexer scheduler and `cmd/backfill-swap-sender`) lists,
  counts and fills NULL-sender `v4_swaps` with the same keyset walk as v1 pool swaps; each
  filled V4 swap re-marks its graduated token's aggregate, as live ingest does. The indexer's
  "retries them once their block is safe" warning is now true for V4 swaps too.

### Plan v2-C Task 9 follow-ups: persisted creator panel state

- **Date:** 2026-11-12
- **What closed:** `V2CreatorPanel` writes (propose, cancel, accept, LP fee claim) persist under
  the `v2-creator` transaction action. A reload restores the hash and status, a restored
  `submitted`/`rpc-failure` hash settles on its receipt, and the lock holds until it does. The
  Anvil v2 gate reloads after the replace and checks the restored hash.

### Plan v2-C Task 9 follow-ups: notice, cancel/replace, creator panel lock

- **Date:** 2026-11-12
- **What closed:** the gate now drives proposal cancel and replace. `V4SwapPanel` shows its
  review notice outside the confirmation (the Permit2 "Allowances are set" message and failed
  review errors were set while the confirmation closed, so they were never visible).
  `V2CreatorPanel` treated its own `mined` state as unresolved, so after one successful write
  every creator action stayed disabled until a reload; a mined write is now final there.

### Plan v2-C D1 and Task 9: Anvil v2 web gate

- **Date:** 2026-11-12
- **What closed:** D1 approved by the owner. Universal Router 2.1.2 is pinned and built in the
  test-only `router` profile (runtime equal to mainnet), deployed with `V4Quoter` by
  `deploy-v2-local.ps1 -WithRouter`. `npm run test:anvil-v2` runs the full Task 9 lifecycle in
  the browser and in CI next to the Task 6 gate. It found four defects, each fixed in its owning
  task: the missing `minHopPriceX36` (Task 4), the v2 fixture wallet stack (Task 2), V4 swaps
  absent from `/v1/transactions` (Plan v2-B), and curve claims hidden after graduation (Task 6).
- **Windows note:** the router's nested submodules exceed MAX_PATH; set `core.longpaths` (see
  `CLAUDE.md` "Don't / watch out").

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
