# Web release runbook

This runbook covers the Task 8 release gate for the public Launchpad web client. It does not
claim live Robinhood or Privy acceptance: those require the reviewed deployment, real Privy
application, funded wallet, production API/RPC, hosting, and named operators.

## Configuration

Only the following browser-visible variables are allowed. They are public configuration, not
secrets:

| Variable                    | Meaning                                                    | Required |
| --------------------------- | ---------------------------------------------------------- | -------- |
| `NEXT_PUBLIC_PRIVY_APP_ID`  | Privy application ID from the reviewed dashboard           | yes      |
| `NEXT_PUBLIC_DEPLOYMENT_ID` | exact enabled deployment manifest ID                       | yes      |
| `NEXT_PUBLIC_CHAIN_ID`      | decimal chain ID matching that manifest                    | yes      |
| `NEXT_PUBLIC_API_BASE_URL`  | HTTPS public API origin, without credentials/query strings | yes      |
| `NEXT_PUBLIC_RPC_URL`       | HTTPS public RPC origin, without credentials/query strings | yes      |

Never put an API key, private RPC URL, bearer token, wallet private key, or seed phrase in a
`NEXT_PUBLIC_*` variable. The production validation command rejects fixture/test values,
credentials, query material, HTTP endpoints, unknown deployments, and chain mismatches. A
missing configuration intentionally leaves the shell read-only and is not a release.

## CSP and caching

`web/src/security/headers.ts` is the source of truth. `connect-src` includes only the origin
parsed from `NEXT_PUBLIC_API_BASE_URL` and `NEXT_PUBLIC_RPC_URL`, plus the exact Privy and
WalletConnect origins required by the wallet stack. `img-src` also permits HTTPS because token
metadata accepts user-supplied HTTPS image URLs; the `SafeImage` primitive rejects all other
schemes and sends no referrer.

```text
https://*.privy.io  wss://*.privy.io
https://*.walletconnect.com  wss://*.walletconnect.com
```

There is no unrestricted `https:` or `wss:` connection or frame source. HTML and API responses
are `no-store`; Next's content-hashed static assets retain the framework's immutable cache
behavior. Review any new connection or frame origin in the code and this policy before shipping
it.

When `NEXT_PUBLIC_E2E_FIXTURE=1` is set for the deterministic Anvil gate only, the policy also
allows the explicit `NEXT_PUBLIC_TASK6_ANVIL_API_URL` and
`NEXT_PUBLIC_TASK6_ANVIL_RPC_URL` origins. Those local origins are rejected unless the fixture
flag is present and are never production configuration.

## Privy and deployment selection

Create or select the Privy app in the reviewed organization, configure the production web origin,
and supply only its public app ID. Verify the linked-wallet authorization behavior against the
backend identity contract. Select a deployment only when its manifest is enabled, its chain ID
matches the public value, and its factory/WETH/router addresses are the reviewed generated
values. Never copy addresses into a page or environment value outside the manifest.

## Required validation

From a clean checkout with Node 24.16.0, npm 12.0.1, Foundry 1.8.1, and Go from
`backend/go.mod`:

```text
node scripts/verify-release.mjs --target=production
```

For deterministic local evidence, select it explicitly instead:

```text
node scripts/verify-release.mjs --target=anvil
```

The root gate runs contract checks, backend `task verify`, frozen web install, API/ABI drift,
format, lint, strict typecheck, unit tests, production build, bundle secret/budget checks,
desktop/mobile/reduced-motion browser tests, and the mandatory Anvil web → API → indexer →
PostgreSQL → contract gate. Missing commands, services, configuration, or the real Anvil gate
are failures; no required stage is silently skipped.

For a production configuration check alone:

```text
cd web
npm run verify:release
```

## Evidence and release decision

Anvil evidence is deterministic local evidence. It proves the representative launch, buy, sell,
rejection, revert, wrong-chain, and reorg paths against local contracts and the local API/indexer
database. It is not evidence of Robinhood connectivity, production RPC behavior, Privy dashboard
configuration, liquidity, or returns. Record the exact commit, gate output, browser project, and
deployment manifest digest in the release ticket.

Record page LCP for the release as well: run Lighthouse in mobile mode against the production
build on the home, token and create pages, and keep each page's LCP with the release evidence. The
target is under 2.5 s (`docs/specs/2026-11-14-performance-targets.md`); a page over it blocks the
release until it is fixed or the miss is accepted in writing in the release ticket.

Live acceptance is a separate, explicitly named check using the production API/RPC and real Privy
application. Do not substitute test wallet keys or local addresses for that evidence.

## Health checks and rollback

Before promotion, check the web origin, API `/healthz`, indexer health endpoint, database
migration status, CSP/security headers, and one read-only token route. After promotion, repeat
these checks and confirm no unexpected authorization, RPC, or transaction data appears in logs.

Rollback the hosting deployment to the last passing commit, then verify the same checks and stop
new transaction-capable traffic if API/indexer compatibility is uncertain. Do not roll back a
database migration independently of the backend compatibility plan. Preserve the incident
timeline and scrubbed request identifiers; never copy tokens, signatures, cookies, or private
RPC material into the ticket.

Ownership placeholders (must be filled before production launch):

- Release owner: `<name / team>`
- Web rollback owner: `<name / team>`
- API/indexer/database owner: `<name / team>`
- Privy and wallet escalation: `<name / team>`
- Incident channel and severity policy: `<link>`
