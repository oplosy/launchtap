# Audit report 2 fixes — pre-flight decisions (2026-11-16)

Scope: the findings from the second 2026-11-16 audit pass that were not already covered by the
P0/P1/MED work — M-2, M-5, L-1, L-4. L-2, L-3 and L-5 were rejected during validation.

## M-2 — the factory does not enforce a timelock delay

**Decision.** The indexer refuses to start for a `production` manifest unless the factory's
`timelock()` is a contract whose `getMinDelay()` is at least 48 hours
(`chain.VerifyGovernanceTimelock`, run right after the bytecode check in
`initializeIndexerResources`). `local` and `testnet` manifests are not checked.

**Rationale.** The factory only checks `msg.sender == timelock`, so an EOA there makes
`configureEngine`, `setFutureDefaults` and `setFutureTreasury` instant. Changing the contract would
be a redeploy; the off-chain gate that already fails closed on unreviewed bytecode is the cheapest
place to make the delay a hard precondition. The testnet timelock is an EOA on purpose
(`docs/runbooks/robinhood-testnet-deployment.md`), so the check is limited to `production`.

**Alternatives.** A manifest field naming the timelock and its delay: rejected, because the chain
is the source of truth and a manifest copy can drift. A release-script RPC check in
`web/scripts/validate-release.mjs`: rejected, because that script is offline by design.

**Trade-off.** 48 hours is the floor, not the target; the governance handoff still records the
actual delay and roles (`docs/runbooks/production-readiness.md`). A timelock contract that answers
`getMinDelay()` honestly but has an over-broad proposer set is out of scope for this check.

## M-5 — trusted proxy CIDRs covered all of RFC 1918

**Decision.** The compose default network is pinned to `10.89.53.0/24` with dynamic addresses
limited to `10.89.53.128/25`; the proxy gets the fixed address `10.89.53.10`, and
`API_TRUSTED_PROXY_CIDRS` trusts only `10.89.53.10/32`.

**Rationale.** Trusting every private range let any other container on the network, or a host on
the operator's LAN/VPC, set `X-Forwarded-For` and pick its rate-limit identity. Only the proxy
forwards client traffic to the API.

## L-1 — panic log had no value or stack

**Decision.** The recovery middleware logs the redacted panic value and `debug.Stack()` with the
request ID; the client response is unchanged.

## L-4 — local audit drafts could be committed

**Decision.** `AUDIT_REPORT*.md` is gitignored; accepted findings live in `backlog.md` and specs.
