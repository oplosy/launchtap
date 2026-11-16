# User-facing error messages — pre-flight 2026-11-16

Goal: every error a user can hit is shown as a specific, plain message that says what happened
and what to do next. No error path ends in silence, a raw provider string, or a message that is
wrong about the outcome.

## Starting point

- The API returns RFC 9457 problems. 49 codes are typed (`urn:launchpad:problem:<code>`), but
  huma's own errors (`huma.Error400BadRequest`, `huma.Error503ServiceUnavailable`, request
  validation) and the middleware's `problem()` (rate limit, CORS refusal, panic) send
  `about:blank`, so the client can only fall back to the HTTP status.
- The web app maps problems in five places with different coverage (`api/problems.ts`,
  `comments/comment-model.ts`, `notifications/telegram-model.ts`,
  `transactions-panel/launch-metadata.ts`, ad-hoc checks), and most codes fall to "Something
  went wrong".
- `decodeTransactionError` knows 19 of the 76 contract errors in the browser ABIs, no Uniswap
  revert strings, no `Panic` codes and no wallet/RPC failure kinds. Every unknown case says "The
  wallet or network rejected this transaction. No funds were moved." — false after a transaction
  was sent (a receipt timeout can hide a mined transaction).
- A failed `fetch` (offline, DNS, CORS) surfaces as a bare `TypeError`.
- The API request ID is in `X-Request-ID` but CORS does not expose it, so a user cannot quote it.

## Decisions

1. **Every API error has a stable code.** `apiserver` overrides `huma.NewError` so huma's own
   errors carry a code derived from the status (`invalid_request`, `not_found`,
   `payload_too_large`, `unsupported_media_type`, `service_unavailable`, `internal_error`, …);
   explicit `huma.Error4xx` calls become `apiProblem` with a specific code; the middleware's
   `problem()` takes a code (`rate_limited`, `origin_not_allowed`, `internal_error`,
   `not_ready`). *Rationale:* the client keys on codes, never on `detail` text. *Alternative:* map
   statuses only on the client — loses the difference between, e.g., a bad address and a bad
   amount.

2. **One catalog in the web app** (`web/src/errors/catalog.ts`): one sentence per API code,
   contract error, Uniswap revert reason, Solidity panic, wallet/RPC failure kind and network
   failure, plus per-context overrides where the same code needs other words (a `not_holder` on a
   vote vs. on a comment). Every surface resolves errors through one function,
   `describeError(cause, { context, submitted })`. *Rationale:* one place to read and review all
   user copy. *Alternative:* keep per-feature maps — the current drift.

3. **Server `detail` text is never shown.** It is developer-facing English and may change; the
   catalog owns the words. Raw wallet, RPC and provider messages stay hidden too (they can carry
   URLs, keys or payloads), as today.

4. **Unknown errors still get a useful message and a code.** An unrecognised error shows a
   generic sentence for its family (API, contract, wallet, network) and, for API errors, a
   short reference (the first 12 characters of the request ID) so support can find the request in
   the logs. The API exposes `X-Request-ID` through CORS for this.

5. **After a transaction is sent, an unknown failure is "outcome unknown".** Any failure once a
   hash exists that is not a mined revert says the transaction may still confirm and points at the
   hash, never "no funds were moved". *Rationale:* a user who believes nothing happened may send
   it again.

6. **Network failures are typed.** `ApiTransport` turns a rejected `fetch` into an `ApiProblem`
   with code `network_unreachable` (status 0); an abort is rethrown unchanged.

7. **Completeness is enforced in CI**, by a web test that fails when a code has no catalog entry:
   every problem code literal in `backend/internal/apiserver/*.go`, every error in
   `browserAbis`, and every wallet/RPC kind the resolver can return. *Rationale:* a new backend
   code or contract error cannot ship without words. *Alternative:* a generated code list — a
   second artifact to keep in sync for the same guarantee.

8. **Background failures keep the last good data** where the screen already says so (token list,
   highlights, live data); this work does not change those flows. User-started actions always end
   in a message.
