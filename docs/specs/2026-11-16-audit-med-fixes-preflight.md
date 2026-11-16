# Pre-flight: audit MED fixes (AUDIT_REPORT_TR MED-01..13)

Date: 2026-11-16. Validation results and the rejected claims are in `backlog.md`.

## Locked decisions

- **MED-02, capped curve buy.** When the contract quote uses less than the entered amount (the
  graduation cap), the buy attaches only `inputUsed` (`readCurveTradeIntent`). `minTokensOut`
  comes from the capped quote, so attaching the full amount let a front-running sell make all
  of it buy at a far worse price. A front-running buy instead makes the cap earlier and the
  contract refunds the excess, as before.
- **MED-08, 500 causes.** `internalProblem` logs the cause with the request id
  (`redact.Error`, so no URL or credential reaches the log); the client still gets the
  generic problem. The server logger travels in the request context.
- **MED-04, metadata text.** Description and social links reject control runes (CR allowed)
  and bidi overrides/isolates with 422 (`social.UnsafeTextRune`, the comment rule).
- **MED-10, inbox read cursor on reorg.** `DeleteCanonicalAbove` moves a read position above
  the common ancestor back to the end of the ancestor block, so an event re-mined at or before
  the old position shows as unread. Trade-off: events above the ancestor that the user had read
  and that survive on the new branch show as unread again; a reorg is rare and the opposite
  error hides a notification.
- **MED-03, Permit2 vs swap deadline.** A sell review caps the swap deadline at the reusable
  exact Permit2 allowance (`sellSwapDeadline`, at least 60 s left), and `planPermit2Sell`
  re-approves an allowance that expires before the swap deadline. Client-clock deadlines stay.
- **Small hardening.** SSE stream metric counted only after `Subscribe` (MED-06; the web
  client's own backoff already stops a reconnect storm), `GET /v1/profile` is `no-store` and
  uses `authenticatePrivy` (MED-05), the image PUT body is capped at the 5 MiB image limit and
  unknown critical PNG chunks are rejected (MED-01), token identities carry the graduation
  block (MED-12).

## Not changed

MED-11 (needs an owner decision on charts excluding the snipe tax) and MED-13 (narrowing
`img-src` needs a browser check of the Privy wallet modal's external logos).
