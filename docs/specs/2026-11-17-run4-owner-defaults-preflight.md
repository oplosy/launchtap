# Security-audit run-4 open leads and M-3 — owner defaults (2026-11-17)

The 26 needs_validation leads of security-audit run-4 (`~/security-audit-skill/launchpad/run-4/NEEDS-VALIDATION.md`)
and the rest of audit pass 3 M-3 (`docs/specs/2026-11-16-rpc-scale-design.md`) were waiting on
owner facts. The owner delegated the choices: each lead below takes the defensive default that
does not depend on the missing fact, or is accepted in writing where the fact only changes the
magnitude.

## Already fixed before this change

| Lead | Where |
|---|---|
| Graduating buy reverts while PositionManager holds extra ETH; stray ETH via SWEEP | #82 |
| Unbounded launch name or symbol halts the indexer | `storableText` caps chain text at 512 bytes, NUL and invalid UTF-8 replaced |
| Single-block log capacity error halts the indexer | audit pass 3 H-1: a failed topic-only curve query falls back to address batches |
| Image upload buffers 6 MiB before authentication | `MaxBodyBytes` on the image route (MED-01) |
| Anonymous protocol revenue full-chain aggregate | daily rollup (#84, HIGH-13 a) |
| Four web leads (stale review, sell guard, launch metadata, launch profile binding) | #88 |

## Locked decisions

1. **Write limiter (full in-memory table).** A full `SubjectLimiter` no longer refuses every new
   principal. It drops expired windows, then a batch of the oldest windows, subjects under their
   limit before throttled ones. *Rationale:* refusal let any set of verified principals block
   everyone's metadata writes; eviction bounds memory the same way and costs an attacker only
   its own throttle state.
2. **CALLCODE frames.** Funding resolution ignores CALLCODE frames like DELEGATECALL and
   STATICCALL: the value never leaves the caller. *Rationale:* the source fact is settled by the
   EVM; no provider trace is needed to know the frame moves no ETH to `to`.
3. **SSE capacity across client keys.** IPv6 clients are keyed by /56 (the usual per-customer
   allocation) instead of /64, for the rate limits and the per-client stream cap. *Rationale:* one
   /56 holder could take 256 keys, enough to fill a quarter of the 1000 SSE slots; IPv4 still
   needs about 250 addresses, which is accepted (best-effort freshness only, REST unaffected).
4. **Shared database work from public reads.** The API pool sets `statement_timeout` to 10 s.
   Early-buyer clusters, forum `top`/`best` and token search keep their result semantics; a read
   past the timeout fails with a 500 instead of holding one of the pool's connections.
   *Rationale:* capping those queries' inputs would change clustering and ranking (product
   behaviour); the timeout bounds the shared cost without it. The slowest measured read
   (HIGH-13/14 validation) is far below 10 s.
5. **Reconciliation sample.** Each pass adds a rotating sample of up to 25 tokens over every
   token (random start, wrapping) to the 25 most recently traded, so a token without a
   `token_reserves` row is compared too. Spec `2026-11-14-state-reconciliation-preflight.md`
   decisions 3 and 4 are updated. *Rationale:* closes the "sum check catches a missed transfer
   anywhere" gap at most 450 calls per 10-minute pass.
6. **Caddy X-Forwarded-For.** Verified, not assumed: the pinned `caddy:2.10.2-alpine` image,
   with no `trusted_proxies`, replaces a client-sent `X-Forwarded-For` (one hop or a chain) with
   the TCP peer address. So the rightmost untrusted hop the API reads is the real client. The
   Caddyfile records this and asks for a re-check on an image change. Reproduce:
   a Caddyfile with `:8080 { reverse_proxy 127.0.0.1:9000 }` and
   `:9000 { respond "xff=[{header.X-Forwarded-For}]" }` in the pinned image, then
   `curl -H 'X-Forwarded-For: 1.2.3.4' http://127.0.0.1:8080/` answers with the peer address only.
7. **OG image routes.** A malformed address segment on the token and wallet OG routes returns
   404 instead of rendering the generic card. *Rationale:* each distinct path is its own ISR
   entry, so a render per random string was free load. Casing variants of a valid address still
   render once each (accepted: each costs the attacker a distinct valid-address request, bounded
   by the edge limit in decision 9).
8. **M-3 RPC load.** `INDEXER_LOG_ADDRESS_BATCH_SIZE` defaults to 1000 (the 2026-09-11 probe
   recorded 1,000-address arrays accepted on both official endpoints). Known-token `Transfer`
   logs use one topic-only query filtered to the known tokens when the step covers at most 10
   blocks and the tokens need more than one address batch; any failure falls back to the address
   batches and disables the fast path for 100 steps. *Rationale:* the probe measured about 94 logs
   (83 kB) per block for all events chain-wide, so a 10-block topic-only read stays small, while a
   100-block catch-up chunk (7.5 MB) keeps the batches. Engine 2 only; engine 1 is not deployed.
9. **Accepted in writing (fact changes magnitude, not the fix).**
   - *Web-originated reads share the web container's rate-limit bucket.* Forwarding the visitor
     address would force `headers()` into the ISR routes and make them dynamic, which costs more
     than it saves. Mitigated by decision 7; `deploy/README.md` already requires approved edge
     rate limits before production exposure.
   - *Funding pass re-reads curve-outflow transfers.* Worker-only (not anonymous), one query per
     30 s; re-measure with `pg_stat_statements` on the first test deployment.
   - *Identity token keeps an unlinked wallet until it expires.* Privy identity tokens are
     short-lived and refreshed by the SDK; enforcing a shorter age would need a live Privy test.
     The effect is limited to publishing or claiming a wallet the principal linked within the
     token's lifetime.
   - *ClaimPublicWallet takeover.* Intended by design (`2026-11-09-p0c2-comments-usernames-design.md`):
     a user who proves current linkage may take the publication. Privy links an address to one
     user at a time.
   - *Live PositionManager action encoding.* The fork lifecycle gate (`check-fork.ps1`) is now a
     step of `docs/release-checklist.md` on the release commit; it needs the archive RPC secret,
     so it cannot run on every push.
   - *Advisory waiver merged in the same PR.* Single-maintainer repository: no second approver
     exists. Waivers stay visible in `web/audit-allowlist.json` with a reason and a review date.
   - *Remaining M-3 facts* (provider plan limits, fallback provider choice): operator inputs at
     deployment time; the defaults above are safe without them.
