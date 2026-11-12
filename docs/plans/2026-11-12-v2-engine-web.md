# Launch Engine v2 — Web Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement
> this plan task-by-task (no subagent-driven development in this repository). Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user launch, trade and manage an engine v2 token in the web app. That covers:
- the anti-snipe tax on curve buys, shown as its own cost line with a countdown;
- trading a graduated v2 token in its Uniswap V4 pool through the Universal Router, with
  Permit2 for sells;
- the creator surface: curve creator fees, LP fees from the locker, and the two-step fee
  recipient change;
- launch facts, token facts and docs that describe the v2 rules honestly.

**Architecture:** The engine comes from the token, the addresses from the deployment.
- **Per-token branching.** Every token screen branches on `token.engine_version` from the
  API. v1 code paths stay as they are.
- **Reviewed addresses only.** Every v2 and V4 address the browser signs against comes from
  the generated `reviewedDeployments`: the factory, locker and hook from the reviewed v2
  manifest; `PoolManager`, `Permit2`, `UniversalRouter` and `V4Quoter` from the reviewed V4
  dependency record. Nothing comes from the API or a URL, and a missing address fails
  closed, as `reviewedRouterAddress` does for v1.
- **Authoritative amounts come from the chain.** Curve quotes come from
  `BondingCurveV2.quoteBuy`/`quoteSell` and pool quotes from `V4Quoter`, both read
  immediately before simulation. The backend quote (`snipe_tax`, `tax_bps`, `tax_ends_at`)
  stays informational, as in v1.
- **Same transaction machine.** Every v2 write reuses the existing review → simulate → sign →
  canonical-observation state machine in `src/transactions.ts` and its exact-write intents.

**Tech Stack:** Next.js 16, React 19, wagmi 2.16.9, viem 2.31.0, Privy 2.17.3 (existing pins),
Vitest, Playwright. **No new web dependency:** Universal Router inputs are encoded with viem's
`encodeAbiParameters`. No Uniswap SDK is added.

**Inputs (normative):**
- `docs/specs/2026-11-11-v2-engine-design.md` §4, §5, §8 and §11;
- the frozen `contracts/abi/v2`;
- `contracts/deployments/config/robinhood-mainnet-v4.json`, the reviewed V4 dependency record;
- the v2 API shape from Plan v2-B Task 9 (quote) and Task 10 (token detail `v2`).

**Out of this plan:**
- any v2 deployment record on a live chain. Mainnet waits for the external audit (spec §9),
  and testnet has no V4 (spec §11.1). Until then, the v2 UI is reachable only through the
  local Anvil fixture;
- adding liquidity to the pool, and any aggregator listing work;
- the Privy 3 upgrade (backlog).

## Decisions (locked here; the product owner can override any of them)

- **D1 — Local Universal Router (BLOCKER for Task 9, and for the encoding check in Task 4).**
  - The local V4 stack (`DeployLocalV2`) deploys `PoolManager`, `PositionManager` and
    `PoolSwapTest`, but no `UniversalRouter`. The browser must sign through the real router,
    so the Anvil web gate needs one.
  - **Recommended:** add `Uniswap/universal-router`, pinned to the source revision of the
    mainnet v2.1.2 deployment, as a contracts submodule. Build it in its own test-only
    Foundry profile, like `out-v4`, and deploy it with `V4Quoter` from `DeployLocalV2`.
  - **Alternative:** run the web gate on a Robinhood mainnet fork against the real router.
    This needs the fork RPC secret, which is a backlog operator item, so it cannot run in CI
    today.
  - **Approval:** adding a dependency needs the product owner's approval
    (`CLAUDE.md`). Tasks 1–8 do not depend on D1 and can start before it is decided.
- **D2 — Encode router inputs against the deployed router's revision.**
  - The browser must encode `SWAP_EXACT_IN_SINGLE` for the revision of the reviewed router,
    and the encoding module names that revision in its doc comment.
  - **Resolved 2026-11-12 (Task 9):** Universal Router 2.1.2 is `Uniswap/universal-router`
    `release/2.1.x`, built against v4-periphery `545a5d2a` (#564). That revision **has**
    `ExactInputSingleParams.minHopPriceX36`, and its calldata decoder needs nine static words
    before `hookData`. The first Task 4 encoding omitted the field and could not have
    executed. The fix encodes `minHopPriceX36 = 0` (off), because `amountOutMinimum` already
    bounds a single hop. The Task 9 gate signs this through a router built from that source.
- **D3 — Sells use an on-chain Permit2 allowance, not a signature.**
  - Flow:
    1. An exact ERC-20 `approve(token → Permit2)`, reset to zero first when non-zero, as the
       v1 router path does.
    2. `Permit2.approve(token, UniversalRouter, amount, expiration)` with the exact amount
       and a short expiration (the transaction TTL).
    3. `UniversalRouter.execute`.
  - Rationale: every step is an exact, reviewable on-chain write that fits the existing
    exact-write intent model. A `PERMIT2_PERMIT` signature saves one transaction but adds an
    EIP-712 signing path and replay surface to an unaudited flow. It can come later.
- **D4 — Router commands.** A buy is one `V4_SWAP` command with the actions
  `SWAP_EXACT_IN_SINGLE`, `SETTLE_ALL` and `TAKE_ALL`. It sends exactly `amountIn` as
  `msg.value`, so no native-ETH sweep is needed. A sell uses the same actions with
  `zeroForOne = false`. `amountOutMinimum` comes from the `V4Quoter` output with the user's
  slippage. `hookData` is empty, and the pool key is built from the D5 constants.
- **D5 — The pool key is never read from the API.**
  - Values: `currency0 = 0x0`, `currency1 = token`, `fee = 10000`, `tickSpacing = 200`,
    `hooks` = the reviewed manifest's hook.
  - Before the first pool write, the browser computes the `poolId` and requires it to equal
    the API's `v2.pool.pool_id`. A mismatch blocks trading with an explanatory error.
- **D6 — The anti-snipe countdown uses chain time.**
  - The remaining time is `v2.anti_snipe.ends_at` minus the latest block timestamp from the
    reviewed RPC. Between block reads it ticks down locally.
  - The tax amount always comes from `quoteBuy` at review time. Because the tax only
    decreases, a transaction mined later never receives less than the quoted
    `minTokensOut`. The copy says so instead of warning about tax slippage.
- **D7 — Engine choice on the create page.** One deployment serves one engine (Plan v2-B B2).
  The visual-only `EngineToggle` becomes a read-only engine badge for the configured
  deployment. A fake choice would mislead.
- **D8 — Launch facts before launch.** The create page reads
  `LaunchFactoryV2.futureDefaults()` through the reviewed RPC. It shows the exact anti-snipe
  profile, the LP creator share and the launch fee that the next launch will snapshot. If
  that read fails, the launch is blocked, as it is today when `launchFee` cannot be read.
  After launch, the token page shows the snapshotted profile from `v2.anti_snipe`.

## Global Constraints

- **v1 must not change.** Every existing unit, Playwright and Anvil Task 6 gate test stays
  green. v1 tokens keep the Uniswap V2 router path.
- **Generated artefacts stay single-sourced.** `src/contracts/generated.ts` and
  `src/api/generated.ts` come only from their generators. `web-contracts-diff` and
  `web-api-diff` fail on drift.
- **Fail closed.** If a reviewed address is absent, malformed or on the wrong chain, the
  affected action is disabled with a reason. No fallback to a default or a guessed address.
- **Numbers.** Every amount is a `bigint` parsed with the existing canonical parsers. No
  float ever enters a write.
- **Budgets.** `verify:bundle` budgets hold. The V4 encoder ships only on the token route.
- **Commits:** conventional prefix, imperative mood, one task per commit, referencing
  `Plan v2-C Task N`.

## Review Focus

1. **Wrong router struct revision (D2).**
   - Expected: the Anvil gate signs a real V4 buy and sell through the reviewed router
     revision. Encoding unit tests alone are not accepted as proof.
   - Pinned in Task 4 and Task 9.
2. **Pool key spoofing (D5).**
   - Expected: an API `pool_id` that differs from the locally computed one blocks trading.
   - Pinned in Task 4.
3. **Permit2 over-approval (D3).**
   - Expected: the ERC-20 approval to Permit2 and the Permit2 allowance to the router are
     both exactly the sell amount; the Permit2 expiration is at most the transaction TTL.
   - Pinned in Task 5.
4. **Fee recipient flow by the wrong account.**
   - Expected: propose and cancel are offered only to the current `feeRecipient`, and accept
     only to `pendingFeeRecipient`, as read on-chain. The creator, as identity, is never
     treated as the payout account.
   - Pinned in Task 6.
5. **Tax countdown drift (D6).**
   - Expected: with the client clock skewed by ±5 minutes, the countdown and the tax line
     still match chain time.
   - Pinned in Task 3.

---

### Task 1: v2 contract surface and reviewed deployments in the browser bundle

**Files:**
- Modify: `web/scripts/generate-contracts.mjs`.
- Regenerate: `web/src/contracts/generated.ts`.
- Create, single-sourced in contracts (outside `contracts/abi/v2`, which the backend mirrors byte for byte): `contracts/abi/external-v4/UniversalRouter.json`
  (`execute` only), `V4Quoter.json` (`quoteExactInputSingle` only) and `Permit2.json`
  (`approve` and `allowance` only).
  - Each fragment is extracted from the reviewed revision named in D1/D2. Its source
    revision is recorded in `contracts/abi/external-v4/README.md`.
- Test: `web/scripts/generate-contracts.test.mjs`, or the existing generator check.

**Decisions:**
- `browserAbis` gains `factoryV2`, `curveV2`, `tokenV2`, `locker`, `universalRouter`,
  `v4Quoter` and `permit2`. Each is filtered by an allowlist, like the v1 entries; errors are
  included so `decodeTransactionError` can name them.
- `ReviewedDeployment` gains:
  - `engineVersion: 1 | 2`;
  - `v4: { poolManager, permit2, universalRouter, v4Quoter, hook, locker } | null`.

  For engine 2, the generator joins the v2 manifest (`locker`, `hook`) with the V4
  dependency record for the same chain (`poolManager`, `permit2`, `universalRouter`,
  `v4Quoter`). It fails if they disagree on `poolManager` or `permit2`.
- No v2 manifest is listed as enabled, because none exists on a live chain yet.

**Steps:**
- [x] **Red:** add generator tests:
  - a synthetic engine-2 manifest plus V4 record produces the `v4` section;
  - a mismatched `poolManager` fails generation;
  - the existing v1 output is byte-identical.

  Expected: FAIL.
- [x] **Green:** implement, then run `npm run web-contracts-sync`. Expected: PASS, with
  `web-contracts-diff` clean.
- [x] **Verify:** `npm run typecheck`, `npm test` and `npm run verify:bundle`.
- [x] **Commit:** `feat(web): add the v2 and V4 contract surface to reviewed deployments`.

### Task 2: Engine selection, reviewed V4 addresses and the v2 Anvil fixture config

**Files:**
- Create: `web/src/engine.ts` and `web/src/engine.test.ts`.
- Modify: `web/src/config/public.ts` and `public.test.ts`. This adds `NEXT_PUBLIC_V2_ANVIL_*`
  fixture variables next to the Task 6 ones, building an engine-2 fixture deployment.
- Modify: `web/src/transactions.ts`, adding `reviewedV4Addresses` next to
  `reviewedRouterAddress`.

**Interfaces:**
- `engineOf(token): 1 | 2`. Any other `engine_version` throws, and the caller renders the
  token as read-only.
- `reviewedV4Addresses(deployment, expectedChainId)` returns the six addresses, or `null` if
  any is missing, malformed, or on the wrong chain.

**Steps:**
- [x] **Red:** unit tests for:
  - each fail-closed case;
  - a v1 fixture still resolving the V2 router;
  - the v2 fixture config requiring every V4 address.

  Expected: FAIL.
- [x] **Green:** implement. Expected: PASS.
- [x] **Commit:** `feat(web): select the engine per token and resolve reviewed V4 addresses`.

### Task 3: Curve buy with the anti-snipe line and countdown

**Files:**
- Modify: `web/src/transactions-panel.tsx`, the `TradingPanelReady` curve path.
- Create: `web/src/anti-snipe.ts` and `web/src/anti-snipe.test.ts`.

**Decisions:**
- For engine 2, the contract quote reads `BondingCurveV2.quoteBuy`, which returns
  `(ethGrossUsed, tokensOut, protocolFee, creatorFee, snipeTax, refund, graduates)`. The
  write calls the v2 `buy`.
- The quote summary shows separate lines for the curve fee, the anti-snipe tax with its bps
  and time left, and the refund (spec §5.2). The informational backend quote shows the same
  lines from `snipe_tax`, `tax_bps` and `tax_ends_at`.
- `anti-snipe.ts` is display-only and has no tax arithmetic. It formats the countdown from
  `ends_at` and the chain head time (D6), and hides the line once the window is over.

**Steps:**
- [x] **Red:** unit tests for:
  - the countdown at the window start, the middle, the end and after it;
  - client clock skew of ±5 minutes not changing the result (Review Focus 5);
  - the summary rendering the tax line only while `snipeTax > 0`.

  Expected: FAIL.
- [x] **Green:** implement. Expected: PASS. v1 curve tests are unchanged.
- [x] **Commit:** `feat(web): show the anti-snipe tax and countdown on v2 curve buys`.

### Task 4: Graduated v2 pool quote and buy through the Universal Router

**Files:**
- Create: `web/src/v4-swap.ts` and `web/src/v4-swap.test.ts`.
- Modify: `web/src/transactions-panel.tsx`. A `V4SwapPanel` sits next to
  `GraduatedSwapPanel`, chosen by `engineOf(token)`.

**Interfaces:**
- `poolKey(token, hook)` and `poolId(key)` follow D5. `poolId` is the keccak256 of the
  ABI-encoded key.
- `encodeV4ExactInSingle({ key, zeroForOne, amountIn, amountOutMinimum })` returns
  `{ commands, inputs }` for `UniversalRouter.execute` (D4). Its doc comment names the router
  revision it mirrors (D2).
- The quote uses `V4Quoter.quoteExactInputSingle` through `simulateContract`, because the
  quoter is non-view.

**Decisions:**
- The pool cost line shows the 1% LP fee. The panel also states that third-party liquidity
  dilutes the locked position's fee share (spec §5.2). It does not claim that every
  aggregator routes the pool (spec §11.2).
- If the computed `poolId` differs from `v2.pool.pool_id`, or the token has no `v2.pool`,
  the panel is disabled with a reason (Review Focus 2).

**Steps:**
- [x] **Red:**
  - `poolId` matches the `PoolManager` `Initialize` log's `topic1` in
    `contracts/fixtures/v2/event-logs-v2.json`, for that fixture's token and hook.
  - Encoder round-trip tests decode `inputs` with the reviewed ABI.
  - A mismatched API `pool_id` disables the panel.

  Expected: FAIL.
- [x] **Green:** implement the buy path: quote, review, simulate, then `execute` with
  `value = amountIn` and the deadline from `transactionDeadline`. Expected: PASS.
- [x] **Commit:** `feat(web): buy graduated v2 tokens through the V4 Universal Router`.

### Task 5: V4 sells with an exact Permit2 allowance

**Files:**
- Modify: `web/src/v4-swap.ts` and `web/src/transactions-panel.tsx`.
- Modify: `web/src/wallet/reads.ts`, adding a read of the Permit2 allowance.

**Decisions:**
- Follow D3. Each step is its own reviewed write with its own canonical observation:
  1. reset the ERC-20 allowance to zero when it is non-zero;
  2. ERC-20 `approve(Permit2, amount)`;
  3. `Permit2.approve(token, router, amount, now + TTL)`;
  4. `execute`.

  Steps that are already satisfied, read fresh, are skipped.
- An existing Permit2 allowance that is larger than the amount or longer-lived is not
  trusted: the flow re-approves exactly.

**Steps:**
- [x] **Red:** unit tests for:
  - the step planner on every allowance state;
  - the exact amounts and an expiration of at most the TTL (Review Focus 3).

  Expected: FAIL.
- [x] **Green:** implement. Expected: PASS.
- [x] **Commit:** `feat(web): sell graduated v2 tokens with an exact Permit2 allowance`.

### Task 6: Creator surface: curve fees, locker fees and the fee recipient change

**Files:**
- Modify: `web/src/transactions-panel.tsx`, the claims section.
- Create: `web/src/fee-recipient.ts` and `web/src/fee-recipient.test.ts`.

**Decisions:**
- **Balances are read on-chain for the connected account.** The values are
  `creatorFeesOwed(account)` and `pendingRefund(account)` on the curve, and
  `owed(token, account)` on the locker. `v2.locker_owed` from the API is shown as indexed
  context only.
- **Claims:**
  - `claimCreatorFees` on the curve;
  - `claim(token)` on the locker. It collects first, so no separate collect button is
    offered;
  - `claimRefund`.
- **Recipient flow:** propose, replace and cancel are offered to the on-chain `feeRecipient`;
  accept is offered to `pendingFeeRecipient`. The proposed address is validated as a
  checksummed non-zero address that differs from the current recipient. The UI shows the
  creator (identity) and the recipient (payout) as separate facts (spec §5.3).
- Balances already credited stay with their account, and the copy says so.

**Steps:**
- [x] **Red:** unit tests for the action matrix: for each role (creator, current recipient,
  pending recipient, stranger) and each state (no proposal, pending proposal), the
  offered actions. This pins Review Focus 4. Expected: FAIL.
- [x] **Green:** implement. Expected: PASS.
- [x] **Commit:** `feat(web): add v2 locker claims and the fee recipient change flow`.

### Task 7: v2 launch flow, create page and docs

**Files:**
- Modify: `web/src/transactions-panel.tsx` (`LaunchPanel`) and `web/app/create/page.tsx`.
- Modify: `web/src/components/engine-toggle.tsx`, which becomes a read-only engine badge
  (D7).
- Modify: `web/app/docs/page.tsx`, adding a v2 rules section.

**Decisions:**
- For an engine-2 deployment, launch calls `LaunchFactoryV2.launch` with
  `engineVersion = 2`. The value is the launch fee plus the developer buy, as in v1. The
  developer buy is labelled tax-exempt.
- The launch facts and docs describe the rules:
  - the decaying buy tax (D8);
  - the developer-buy exemption;
  - the 1% pool fee and its 50/50 creator/protocol split;
  - the permanently locked position;
  - dilution by third-party liquidity;
  - permissionless fee collection;
  - the two-step recipient change.

**Steps:**
- [x] **Red:** extend the create-layout Playwright spec with these checks:
  - the engine badge shows the configured engine;
  - the v2 rules copy is visible;
  - there is no engine choice control.

  Expected: FAIL.
- [x] **Green:** implement. Expected: PASS, and the accessibility spec stays green.
- [x] **Commit:** `feat(web): launch through engine v2 and describe its rules`.

### Task 8: v2 token facts and honest pool figures

**Files:**
- Modify: `web/src/token/token-detail.tsx`.

**Decisions:**
- A v2 facts block shows:
  - the anti-snipe profile and its end time;
  - the LP creator share;
  - the pool id and hook, each with an explorer link;
  - the fee recipient and any pending recipient;
  - the indexed locker balances.
- For a graduated v2 token, `liquidity_eth`, `eth_reserve` and `token_reserve` are placeholder
  values until the backend projects V4 liquidity (Plan v2-B known limitation). The page
  shows "Pool liquidity: not indexed yet" instead of a misleading 0, and price comes from
  the indexed spot price.

**Steps:**
- [x] **Red:** component tests on the API fixtures from Plan v2-B Task 10:
  - v1 has no v2 block;
  - v2 before graduation shows no pool row;
  - v2 after graduation shows the pool facts and the liquidity placeholder.

  Expected: FAIL.
- [x] **Green:** implement. Expected: PASS.
- [x] **Commit:** `feat(web): show v2 launch, pool and fee recipient facts`.

### Task 9: Anvil v2 web gate (needs D1)

**Files:**
- Modify: `contracts/script/v2/DeployLocalV2.s.sol` and `contracts/scripts/deploy-v2-local.ps1`
  to deploy the reviewed `UniversalRouter` and `V4Quoter` and record them in the local
  manifest. This is a contracts change, so it gets its own pre-flight review.
- Create: `web/scripts/anvil-v2-gate.ps1`, modelled on `anvil-task6-gate.ps1`, and
  `web/e2e/v2-lifecycle.spec.ts`.
- Modify: `web/package.json` (`test:anvil-v2`) and `.github/workflows/web.yml`, where it
  runs next to the Task 6 gate.

**Lifecycle**, driven through the browser against the local backend indexer and API:
1. Launch v2 with a developer buy.
2. A taxed buy, where the tax line and countdown are visible.
3. An untaxed buy after the window. Anvil time is advanced with `evm_increaseTime`.
4. The final buy graduates the token.
5. A V4 buy, then a V4 sell through the Permit2 steps.
6. A locker claim and a curve creator fee claim.
7. Propose a recipient, accept it from the second account, then a claim by the new
   recipient.

**Assertions:**
- Every displayed balance and every claimed amount equals the on-chain view at that block.
- The `poolId` check passes.
- No transaction reverts.

**Steps:**
- [ ] **Red:** write the spec. Expected: FAIL until Tasks 1–8 and D1 are in place.
- [ ] **Green:** fix whatever the gate exposes, inside the task that owns it, as a follow-up
  commit naming that task.
- [ ] **Verify:** `npm run lint`, `npm run typecheck`, `npm test`, `npm run test:e2e`,
  `npm run test:anvil`, `npm run test:anvil-v2` and `npm run verify:bundle`.
- [ ] **Commit:** `test(web): Anvil v2 lifecycle gate`.

## Acceptance

- A user can complete the whole Task 9 lifecycle in the browser on the local v2 stack, and
  every amount shown before signing matches what the chain executes, within the stated
  slippage.
- v1 behaviour, its tests and its gates are unchanged.
- No address the browser signs against comes from anywhere other than the generated reviewed
  deployments.
- Enabling v2 on a live chain needs only a reviewed v2 manifest and V4 record: no web code
  change.
