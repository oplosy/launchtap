# Incident response

What to do when something goes wrong with the contracts, the web app or the backend: when to use
the factory pause switches, who signs, how to tell users, and how to close the incident. It
covers gap G8 in `docs/audits/2026-11-18-project-gap-assessment.md`.

Related runbooks:
- service rollback, health checks and alert meanings: [production readiness](production-readiness.md);
- indexer recovery beyond the normal reorg window: [deep-reorg recovery](indexer-deep-reorg-recovery.md);
- comment hiding and bans: [production readiness → Moderating comments](production-readiness.md#moderating-comments).

## 1. What the switches can and cannot do

The contracts are non-upgradeable and no role can move user funds. The only emergency controls are
two flags on each launch factory. Only the factory's `pauseAuthority` can set them.

| Call | Blocks | Does not block |
|------|--------|----------------|
| `setLaunchesPaused(true)` | New launches on that factory. | Anything on existing launches. |
| `setTradingPaused(true)` | Curve **buys** on every un-graduated launch of that factory. That also blocks the developer buy at launch and every graduation, since only a buy can graduate a curve. | Curve **sells**. Every claim: creator and protocol fees, refunds, snipe tax, launch fees, LP-fee `collect` and `claim`. Trading in graduated pools (engine 1 Uniswap v2 pairs, engine 2 Uniswap v4 pools). Token transfers. |

Consequences to keep in mind:
- **Each factory has its own switches.** Engine 1 and engine 2 are separate factories with separate
  manifests. When the cause is shared, for example a web or signing issue, pause each affected
  factory.
- **Graduated tokens cannot be paused.** Their pools are plain Uniswap pools. The engine 2 hook only
  guards pool initialization. For a problem after graduation, communicate and, if needed, take
  the web app's trade surface down.
- **The trading flag is factory-wide.** A forward fix registered on the same engine 2 factory
  through `configureEngine` would share the flag with the vulnerable launches. If those launches
  must stay paused, the fix needs a new factory deployment, not just a new engine version.
- The timelock (≥ 48 h) changes only future-launch defaults, the future treasury and engine
  registration. It is never an emergency tool.

## 2. Severity and triggers

| Sev | Meaning | Response target |
|-----|---------|-----------------|
| SEV1 | User funds at risk now, or an exploit in progress. | Pause proposal within 15 min of declaring, executed within 60 min. Status notice within 30 min. |
| SEV2 | A correctness or availability fault that loses no funds, such as a broken trade path or a wrong displayed value. | Status notice within 2 h. Fix or rollback the same day. |
| SEV3 | Degraded, with a workaround. | Next working day. |

These targets assume the on-call owner and the signer set from the production-readiness sheet.
Confirm or adjust them there before mainnet.

| Trigger | Sev | Contract action | Other action |
|---------|-----|-----------------|--------------|
| Credible report or proof of a bug in curve buy, sell math, fees or graduation | SEV1 | `setTradingPaused(true)` and `setLaunchesPaused(true)` on the affected factory | Contact the auditor. Follow §6 before unpausing. |
| Bug confined to the launch path (factory create, token deploy, initial parameters) | SEV1 | `setLaunchesPaused(true)` | — |
| Unexplained ETH leaving curves, or `IndexerStateDrift` paging with a reserve mismatch the indexer cannot explain | SEV1 until explained | Both pauses on the affected factory | Compare `eth_getBalance` of the curve with the indexed reserve at the same block. |
| Bug in the LP-fee locker or a graduated v4 pool | SEV1 | `setTradingPaused(true)`, which stops new graduations into it | Existing positions cannot be paused. Warn users and remove the trade surface if needed. |
| Web compromise: served JS that differs from the release build, a changed CSP header, or a DNS or CDN hijack | SEV1 | Usually none. The contracts are unaffected | Take the web app offline (`production-readiness.md` rollback). Tell users not to sign anything from the site. Approvals are exact-amount, but tell users to check them anyway. |
| Leaked deployer, pause-signer or treasury key | SEV1 | Pause if the key is a pause signer and the signer set is below threshold | Rotate the Safe owner. The deployer holds no authority after setup (`noResidualDeployerAuthority`). |
| API or indexer outage, or stale data | SEV2 | None | The web shows the site-wide stale-data notice by itself (`GET /v1/status`). Follow the rollback and health checks in `production-readiness.md`. |
| Privy, RPC provider or Robinhood Chain outage | SEV2 | None | Status notice. The indexer fails over to `RPC_FALLBACK_URL` by itself; confirm it did (`RPCErrorRateHigh` clears). An indexer that logs `primary RPC failed the identity checks; staying on the fallback until restart` found the primary serving the wrong contracts: fix `RPC_URL`, then restart it. The browser RPC (`NEXT_PUBLIC_RPC_URL`) needs a web redeploy to change. |
| Scam token, abusive comments, impersonation | SEV3 | None | Moderation CLI (`cmd/moderate`). A creator dumping their own tokens is market risk, not an incident. |

Do not pause for price moves, a single token collapsing, or anything that is not a protocol or
platform fault. Each pause stops buys for every honest user on that factory.

## 3. Signing path

### Mainnet: pause multisig (Safe)

1. **Declare.** The incident lead names the severity, the affected factories and the action, and
   opens the incident log (§5).
2. **Read the current state.** Pass `--rpc-url` with the production RPC from the secret manager.
   ```powershell
   cast call <factory> "pauseAuthority()(address)" --rpc-url $env:RPC_URL
   cast call <factory> "tradingPaused()(bool)" --rpc-url $env:RPC_URL
   cast call <factory> "launchesPaused()(bool)" --rpc-url $env:RPC_URL
   ```
   `pauseAuthority` must be the Safe recorded in the production-readiness sheet. If it is not,
   stop: the manifest or the chain is not what you think.
3. **Propose.** In the Safe app, use the transaction builder with the factory address. Call
   `setTradingPaused` and/or `setLaunchesPaused` with `true`. Batch both calls, and every
   affected factory, into one Safe transaction. Before mainnet, save this batch as a prepared
   Transaction Builder JSON in the signers' shared vault, so that an incident needs no ABI work.
4. **Sign.** Signers check the target address and calldata in their own wallet against this
   runbook, not against a link sent in chat.
5. **Execute,** then verify (§4).

### Testnet: pause authority keystore

The testnet manifest's `governance.pauseAuthority` is an operator keystore. Use the same
reads, then:
```powershell
cast send <factory> "setTradingPaused(bool)" true --account <pause-keystore> --rpc-url $env:ROBINHOOD_TESTNET_RPC_URL
```

## 4. Verify the pause

- `cast call` from §3 step 2 returns `true` for the flag you set.
- The indexer recorded the event. `TradingPauseSet` lands in `trading_pause_events` and
  `LaunchPauseSet` in `launch_pause_events`:
  ```sql
  SELECT block_number, encode(tx_hash, 'hex') AS tx, paused
  FROM trading_pause_events WHERE chain_id = <chain> ORDER BY block_number DESC LIMIT 1;
  ```
- In the web app:
  - a buy fails before signing with "Buying is paused by the reviewed deployment. Selling remains
    available.";
  - the create page lists the launch pause as a blocker;
  - a sell still goes through.

## 5. Communication

Channels: the status channel and social account named in the production-readiness sheet, plus a
pinned site notice if the web app is up. Post the first notice even when the cause is unknown.

**Never** ask users to move funds, sign anything, share a seed phrase, or use a "migration" link.
State this in every message, because phishing accounts copy incident notices.

Initial notice:
> We are investigating an issue affecting <scope>. As a precaution, <buying on the bonding curve /
> new launches> is paused. Selling, fee claims and refunds still work, and graduated tokens trade
> normally. We will never ask you to send funds or sign a transaction to fix this. Next update by
> <time UTC>.

Update: what is known, what is still paused, the next update time.

Resolution:
> <Scope> is restored as of <time UTC>. Cause: <one sentence>. Funds affected: <none / amount and
> plan>. A full post-mortem follows within 5 days at <link>.

## 6. Unpause

Unpause only when all of these hold, and record each in the incident log:
1. The root cause is understood and written down.
2. The fix is live:
   - for a web or backend fix, deployed and passing the release gate;
   - for a contract fix, a new engine version or factory, deployed and verified through the
     normal deployment runbooks. Read §1 before choosing between the two.
3. For a contract cause, the external auditor reviewed the fix, and the fix was rehearsed on
   testnet.
4. The unpause goes through the same signing path (§3) with `false`, and is verified as in §4.

## 7. Post-mortem

Within 5 days, write `docs/audits/<date>-incident-<slug>.md` with:
- a UTC timeline: detection, declaration, pause, notices, fix, unpause;
- impact: affected factories and tokens, users and ETH, and how long buys were blocked;
- the root cause and why tests, audits and alerts missed it;
- action items with owners. Add each one to `backlog.md`;
- evidence: transaction hashes, alert links, gate output.

Blameless: describe decisions and systems, not people.

## 8. Rehearsal

Rehearse once on testnet before mainnet, and again whenever the signer set changes:
1. Pause trading and launches on the testnet factory with the testnet pause authority (§3).
2. Run every check in §4. Draft the initial notice in a private channel and time each step.
3. Unpause and verify again.
4. After the mainnet Safe exists, do a dry run: propose the prepared batch (§3 step 3) on
   testnet, or on the mainnet Safe without executing it, to prove the signers can reach
   threshold.

Record each rehearsal here:

| Date | Network | Factory | Pause tx | Unpause tx | Time to pause | Notes |
|------|---------|---------|----------|------------|---------------|-------|
| — | — | — | — | — | — | Not rehearsed yet. |
