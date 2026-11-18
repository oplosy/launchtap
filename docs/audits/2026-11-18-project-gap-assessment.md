# Project gap assessment — 2026-11-18

A whole-project review of what stands between the repository today and a public mainnet
launch. It sits next to `docs/quality-scorecard.md` (which scores code quality) and
`backlog.md` (which tracks unfinished work). This file adds the gaps that neither of them
makes visible on its own. Assessed at `dev` commit `85322e1`.

## 1. Verdict

**The engineering is production grade. The release is not.** Code quality, tests and
internal audits are well above typical for a project at this stage (scorecard overall 8.6 / 10).
Almost nothing between today and mainnet is code. The real blockers are:

- an independent audit;
- a live rehearsal of the mainnet engine;
- governance and hosting inputs only the owner can provide;
- a legal position.

Two of the gaps below are code work that can start now: a testnet path for engine 2 (G2) and
the legal pages (G5, code part).

## 2. Where the project is strong

These areas need no work before launch:

- **Contracts:** checked arithmetic and no upgradeability. Hard caps are visible in the bytecode.
  217 Foundry tests, a fixed-seed deep fuzz and invariant campaign, Slither, and a pinned
  mainnet fork suite.
- **Backend:** reorg-safe indexer with a deep-reorg soak and on-chain reconciliation. Per-service
  least-privilege Postgres roles, a frozen `/v1` contract with a breaking-change gate, and RPC
  failover.
- **Web:** one reviewed-write pipeline for every transaction, re-quote before signing, and
  Lighthouse 100 for accessibility, best practices and SEO.
- **Process:** pre-flight specs for every high-risk change, 5 CI workflows, branch protection,
  and repeated internal audit passes with every confirmed finding fixed or accepted in writing.

## 3. Gaps

Priority: **P0** blocks accepting mainnet funds. **P1** must close before public traffic.
**P2** can follow the launch.
Owner: **Operator** means accounts, keys, money or signatures only the product owner holds.
**Code** means repository work.

### P0 — mainnet blockers

| # | Gap | Evidence | Owner | Next step |
|---|-----|----------|-------|-----------|
| G1 | **No external audit of engine 2.** | Scorecard category 5 is capped at 8.0 for this reason. `docs/runbooks/production-readiness.md` row "External audit" is `<pending>`. | Operator | Send `docs/audits/external-audit-package.md` to 2–3 firms with Uniswap v4 hook experience. Budget is roughly $15k–40k. |
| G2 | **Engine 2, the mainnet engine, has never run on a live chain.** Robinhood testnet has no Uniswap v4 deployment, and `DeployLaunchpadV2.s.sol` refuses every chain except mainnet. Engine 2 has only run on Anvil and in the mainnet fork suite. The first live launch, graduation and LP-fee collection would happen with real funds. | `contracts/script/DeployLaunchpadV2.s.sol` (`_verifiedDependencies`). The testnet manifest is engine 1 only. | Code + Operator | **Code part done** in PR #114 (`docs/specs/2026-11-18-v2-testnet-path-preflight.md`): `bootstrap-testnet-v4.ps1` + `DeployTestnetV4.s.sol`, `DeployLaunchpadV2` accepts the reviewed testnet record, and `deploy-v2-testnet.ps1`. The live testnet dry run passed. **Open (Operator):** broadcast, review the records, then run the full v2 journey (runbook "Engine 2 on testnet"). After that, a mainnet canary with launches paused. |
| G3 | **No live product acceptance run, even for engine 1.** The testnet journey with separate creator and trader wallets was never recorded. | `README.md` status section. `docs/runbooks/robinhood-testnet-user-journey.md`. | Operator | Run the journey once and record the evidence. Repeat it for engine 2 after G2. |
| G4 | **Governance not set up.** Pause multisig, timelock (≥ 48 h) and treasury addresses are not chosen, and the pause handoff was never tested. | `production-readiness.md` "Governance handoff", three rows `<pending>`. | Operator | Create the Safe (for example 2-of-3), deploy a `TimelockController` with ≥ 48 h delay, and run the read-only role check and a pause test on testnet. |
| G5 | **No legal position and no legal pages.** No jurisdiction or geo policy. The web app has no Terms of Service, Privacy Policy or risk disclosure route, and nothing restricts access by region. Token issuance and memecoin trading are regulated differently per country. | `production-readiness.md` row "Legal/geo policy" `<pending>`. `web/app/` has no terms or privacy route. | Operator, then Code | Get a lawyer's opinion on the operating entity, target markets and excluded jurisdictions. **Code part done** (`docs/specs/2026-11-18-legal-pages-preflight.md`): `/terms` and `/privacy` with draft notices, a first-visit risk notice, footer links, and a production release gate that refuses draft legal documents. **Open (Operator):** counsel text and approval, then the jurisdiction list and, if required, an edge geo-block. |
| G6 | **No production environment.** Hosting, domain, TLS, the production Privy app and origin, production RPC providers (primary and fallback), database secret and backup owner are not chosen. | 10 more `<pending>` rows in the `production-readiness.md` input sheet. Backlog "Operator-only inputs" steps 3 and 6. | Operator | Choose a host and fill the input sheet. `deploy/` (Compose + Caddy) is ready for it. |

### P1 — before public traffic

| # | Gap | Evidence | Owner | Next step |
|---|-----|----------|-------|-----------|
| G7 | **The release gate cannot pass right now.** The archive RPC secret fails its TLS handshake, so the pinned fork suite (part of `check.ps1 release`) does not run. | Backlog "Operator-only inputs" step 2. | Operator | Replace `ROBINHOOD_MAINNET_ARCHIVE_RPC_URL` and re-run the `contracts` workflow. |
| G8 | **No contract incident playbook.** No runbook says when to call `setTradingPaused` or `setLaunchesPaused`, who signs, how fast, or what to tell users. The production sheet asks for an "emergency procedure" but none is written. | No runbook under `docs/runbooks/` covers the pause functions. | Code (doc) + Operator | **Doc done:** `docs/runbooks/incident-response.md` covers triggers, the signing path, communication templates, unpause and post-mortems. **Open (Operator):** rehearse it on testnet (backlog step 1c). |
| G9 | **No edge protection in front of the API.** No CDN and no edge rate limit. The API rate limiter is per process, so with N replicas the effective limit is N times higher. Token images are served from PostgreSQL. | Scorecard category 7 "Open: no CDN". `deploy/README.md` "Configure the approved edge rate limits". | Operator | Set up the CDN and edge limits per `docs/runbooks/scale-out-and-cdn.md` when hosting is chosen (G6). |
| G10 | **Client-side errors are invisible.** The owner deferred error tracking, and the web app reports no errors or Web Vitals anywhere. A failing wallet flow on some browser would go unnoticed. | Backlog "Observability" boundary note. | Owner decision, then Code | **Done:** the owner delegated the decision; first-party error and Web Vitals metrics through `POST /v1/client-events` with `WebClientErrorsHigh` and `WebLCPSlow` alerts (`docs/specs/2026-11-18-client-telemetry-preflight.md`). |
| G11 | **No one is on call.** No monitoring owner or alert destination, and there is a single human operator. | `production-readiness.md` row "Monitoring owner". | Operator | Name an alert destination and a second person who can at least pause (G4, G8). |
| G12 | **Unmeasured at real volume.** The load probe and LCP were measured on the local stack only. No signed-in UX pass with real Privy and a real wallet was recorded. | Scorecard "Path to 9" items 8 and 10. | Operator + Code | After G6, run `cmd/loadprobe` and Lighthouse against the test deployment and do the signed-in pass. |
| G13 | **The dependency advisory waiver expires 2027-02-01.** zod 3 and decode-uri-component are allowlisted through Privy 2. | Backlog "Wallet SDK upgrade: Privy 3". | Code | Re-check upstream before the date. Upgrade or renew with a fresh impact review. |

### P2 — can follow the launch

| # | Gap | Evidence | Owner | Next step |
|---|-----|----------|-------|-----------|
| G14 | **Optional integrations not live.** No Telegram bot, no CoinGecko key (so no USD line), and no market links until a mainnet manifest exists. | Backlog steps 4–5. Scorecard category 11. | Operator | Create the bot and the key. The code is merged. |
| G15 | **README is out of date.** It says the ETH/USD adapter is not implemented and that the Forum has no route. Both have shipped (P0-A, P0-C5). | `README.md` status section. | Code | **Done:** the README covers engine 2, the current routes and API groups, and the backlog. The roadmap and ETH/USD runbook status were refreshed too. |
| G16 | **Moderation is CLI only.** Bans and hides need shell access to `cmd/moderate`. | `backend/cmd/moderate`, `production-readiness.md` "Moderating comments". | Code | Acceptable at launch. Add an admin surface if moderation volume grows. |
| G17 | **No bug bounty.** | — | Operator | Open one for the audited scope after mainnet launch. |
| G18 | **No go-to-market plan in the repository.** Positioning against competitors on the same chain, the launch plan, community channels, the brand (the UI says "Launchpad") and the budget are undecided. The competitor facts in `notes.md` are marked unverified. | `notes.md` "Roadmap candidates". | Operator | Decide the name and domain with G6. Write a one-page launch plan with a budget covering audit, hosting, RPC and gas. |

## 4. Recommended order

1. **Start now, in parallel.** Request audit quotes (G1). Get the legal opinion (G5). Build the
   engine-2 testnet path (G2, code). Write the incident runbook (G8, code).
2. **While the audit runs.** Set up governance on testnet (G4). Fix the archive RPC (G7). Run the
   testnet journeys on both engines (G3). Pick hosting and fill the production sheet (G6, G9, G11).
3. **After the audit and fix review.** Deploy the audited bytecode to mainnet. Keep launches
   paused for a team-only canary launch and graduation. Then open to the public.
4. **After launch.** Bug bounty (G17), integrations (G14), real-volume measurements (G12).

## 5. Re-assessment

Update this file's gap tables in place as items close (strike or remove rows, cite the PR or
evidence). Write a new dated assessment at the next milestone instead of rewriting this one.
