# Auth, wallet verification and API security review — 2026-11-14

A focused, source-first review (guidance mode of the `security-audit` skill) of the backend's
authentication, wallet ownership and public API surface. Scope, method and every finding's
disposition are recorded here; the closure is in the same change as this file.

## Scope

- `backend/internal/privyauth` — Privy access/identity token verification.
- Wallet ownership: how `Principal.Wallets` reaches each write (`internal/apiserver`
  `privy_auth.go`, `comment_routes.go`, `profile_identity_routes.go`, `metadata_routes.go`,
  `watchlist_routes.go`, `inbox_routes.go`, `telegram_routes.go`) and the store checks behind them
  (`internal/store/postgres/metadata.go`, `social_store.go`, `queries/telegram.sql`).
- HTTP boundary: `internal/apiserver/server.go` middleware (rate limit, client IP, CORS, body
  limits, timeouts, request IDs) and `ratelimit.go`.

Out of scope: the web client, contracts (see `2026-11-14-deep-property-campaign.md`), deployment
secrets, and Privy's own service. No deployed endpoint was probed; evidence is source plus the
package's unit tests.

## What holds

| Control | Evidence |
|---------|----------|
| Token signature and claims | `ES256Verifier.verifyJWT`: `alg` pinned to ES256 (no `none`/HS confusion), `crit` rejected, fixed 64-byte signature, `iss == privy.io`, `aud ==` app ID, non-empty `sub`, strict `exp`, `iat`/`nbf` with 30 s skew, size caps on token, header and claims, duplicate JSON keys and unknown fields rejected. |
| One principal per request | Access and identity token subjects must match; wallets come only from the signed identity token's `linked_accounts` (EVM, linked, deduplicated). |
| Uniform auth failure | `authenticatePrivy` and `MetadataRoutes.authenticate` return the same 401 `authentication_failed` for every cause. |
| Creator-only metadata and image writes | `MetadataStore.ReplaceMetadata` / `ReplaceImage` resolve `authorizedCreator` inside the write transaction, keyed by chain and token, from the caller's verified wallets; optimistic revision via `If-Match`. |
| Image upload | Magic-byte detection of PNG/JPEG/WebP only (no SVG), declared type must equal detected type, 5 MiB and 4096×4096 caps (decode-bomb guard), EXIF stripped, served with `nosniff` and the detected type. |
| Public wallets | `social_store` rejects any published wallet not in the caller's linked wallets (`ErrWalletNotLinked`), at most 5. |
| Account-scoped data | Watchlist, inbox and Telegram settings are keyed by (chain, deployment, Privy DID) from the verified principal, never from request input. |
| Telegram link codes | `telegram.NewCode` uses `crypto/rand`; only the SHA-256 is stored; one live code per account; consumption deletes the code and checks expiry atomically (`ConsumeTelegramLinkCode`). |
| Client IP for rate limiting | `clientAddress` honors `X-Forwarded-For` only from trusted proxies and walks it right to left; tested for spoofed left entries, malformed hops, and (new) a CDN chain. |
| CORS | Exact-match allow-list, `*` never echoed, denied preflight is 403, denied reads get no CORS headers. |
| Request bounds | 6 MiB body (`MaxBytesReader`), 32 KiB headers, 5 s header / 15 s read / 15 s write timeouts, per-request context timeout except the SSE stream, request IDs validated before echo, panics recovered without leaking detail. |

## Rate limits and abuse controls

| Control | Limit | Where | Test |
|---------|-------|-------|------|
| Per-client request rate | 600/min, burst 120 (`API_RATE_LIMIT_PER_MINUTE`, `API_RATE_LIMIT_BURST`); probes exempt | `RateLimiter`, `middleware` | `TestRateLimiterRefillsAndBoundsBurst`, `TestMiddlewareRateLimitsClientsButNotProbes` |
| Concurrent SSE streams per client | 4 (`API_SSE_MAX_PER_CLIENT`) | `StreamLimiter` | `TestStreamLimiterCapsConcurrentStreamsPerClient` |
| Metadata and image writes | 30/min per Privy DID | `SubjectLimiter` in `MetadataRoutes` | `TestMetadataWritesAreRateLimitedPerPrivyDID` (new: 429 for the same DID, a separate budget per DID), `TestSubjectLimiterBoundsIdentityState` |
| Comments | 1 per 15 s, 30/h; body ≤ 500 code points, ≤ 3 consecutive newlines | `internal/social/policy.go`, enforced in the store on the database clock | `comment_routes_test.go`, social store integration tests |
| Reports / votes | 20/h / 120/h | `internal/social/policy.go` | social store integration tests |
| Username changes | once per 7 days; released names held 30 days | `internal/social/policy.go` | social store integration tests |
| Watchlist | 200 entries | `watchlist.MaxEntries` | `watchlist_routes_test.go` |

The per-client and SSE limits are per API replica (`docs/runbooks/scale-out-and-cdn.md`).

## Findings

| ID | Finding | Disposition |
|----|---------|-------------|
| F1 | `Vary: Origin` was set only for allowed origins. Behind the CDN described in the scale-out runbook, a response cached for a request without `Origin` could be served to an allowed origin without CORS headers, breaking browser reads (availability, not a confidentiality break). Severity: low. | **Fixed**: `cors` adds `Vary: Origin` to every response; `TestServerBoundaries` asserts it for no-origin and denied-origin reads. |
| A1 | Wallets come from the identity token, so a wallet unlinked in Privy keeps its rights until that token expires. | **Accepted**: bounded by Privy's identity-token lifetime; the wallet was the user's own and Privy issues a fresh token on unlink. Revisit if a revocation requirement appears. |
| A2 | The verifier does not distinguish access from identity tokens by type; two identity tokens pass as a pair. | **Accepted**: both must be valid, signed for this app and carry the same subject, so no other principal's authority is reachable. |
| A3 | ECDSA signatures are accepted in both low-s and high-s form. | **Accepted**: malleability does not matter for bearer JWTs; nothing keys on the signature bytes. |

No high or medium findings. Re-run this review when `privyauth`, the middleware, or a write
route's authorization changes, and record a new dated file.
