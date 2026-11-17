# Open product decisions — pre-flight (2026-11-16)

The owner delegated the open decisions of the production-readiness audit (pass 3) and the
`AUDIT_REPORT_TR` follow-ups: "do the most sensible thing". Each decision below is locked with
its rationale. Contract work (L-1) is high-risk and lands in its own PR.

## Decisions

**M-2 — comment and vote eligibility: hold at least 0.01 % of supply.** A wallet qualifies as a
holder when its indexed balance is at least `total_supply / 10,000` (100,000 tokens of a
1-billion supply); the creator always qualifies. One threshold for comments, votes and reports
(`QualifyWallets`).
*Rationale.* `balance > 0` lets one buy be split as dust over many Privy accounts. A share of
supply scales with every token, needs no price feed, and makes each extra account cost a real
position. A holding-age rule was rejected: it needs per-wallet acquisition history in the hot
path, and the hourly comment limit already slows a fresh account.

**HIGH-12 — V4 reserves in the token detail: document, no shape change.** For
`reserve_source = "v4"` the API returns the launch's initial virtual reserves in `eth_reserve` and
`token_reserve`, because a v4 pool has no reserves to report. The OpenAPI field docs now say these
fields are meaningful only for the `curve` and `pair` sources, and clients read v4 liquidity from
`v2`.
*Rationale.* Making the fields nullable is a breaking `/v1` change for a value the web already
hides, and `reserve_source` already tells a client which case applies.

**SSE graduation `pair` of engine-2 tokens: document.** An engine-2 token graduates into a v4
pool, which has no pair contract, so `pair` is the zero address. The field doc says so.
*Rationale.* Same as HIGH-12: an additive explanation instead of a breaking type change.

**`wallet_funding` rows written `unknown` by the old same-block rule: requeue them.** A one-shot
operator command, `go run ./cmd/funding requeue-unknown`, deletes `unknown` rows in one
statement; the running worker then resolves those wallets again with the current rule.
*Rationale.* `unknown` means "not resolved", not a fact, so dropping it loses nothing a retry
cannot restore. A command (not a migration) runs only where the funding worker runs and leaves
the operator in control of when the RPC load happens.

**L-1 — fee caps and price continuity in engine 2 (contract, separate PR).** `LaunchFactoryV2`
and `BondingCurveV2` reject a trade fee above 5 % (500 bps), a launch fee above 0.05 ETH, and
parameters whose curve-end price differs from the pool-open price by more than 1 bp.
*Rationale.* The timelock already delays every defaults change by 48 hours, but caps make the
worst case visible in the bytecode an auditor reviews. Engine 1 is deployed on testnet and is
not the mainnet engine: it is left unchanged so its reviewed bytecode stays valid. Today's
parameters (1 %, 0.0005 ETH, equal prices) pass.

**L-2 — third-party AMM pairs before graduation: accepted.** A token cannot tell a pair from a
wallet or a multisig, so blocking them would need an allowlist of every recipient. An outside
pair trades a holder's own tokens at their own price; it cannot move the curve or the
graduation. The docs page says a token trades on the curve only until graduation and that other
venues are not ours.

**L-3 — metadata authority and history: creator keeps authority; edits become visible.** The
metadata read returns `updated_at` next to `revision`, and the token page shows "Details edited"
with the date once a token's details changed after launch.
*Rationale.* The fee recipient can be a contract that cannot sign a metadata request, so moving
authority to it would lock some tokens' metadata. Freezing after graduation would stop honest
updates (new socials). Showing that and when details changed gives readers the signal a full
history would, without a new table.

**L-4 — treasury rotation does not move existing launches' fees: accepted as designed.** Each
launch pins its treasury, so a rotation cannot redirect fees users already agreed to. The
runbook says the treasury should be a multisig whose signers rotate, not an address that
changes.

## Out of scope

A nullable `/v1` reserve field and a `pool_id` in the SSE graduation event (a `/v1` baseline
bump), a full metadata revision history, and any change to engine 1.
