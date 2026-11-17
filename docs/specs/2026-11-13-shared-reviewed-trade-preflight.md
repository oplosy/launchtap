# Shared reviewed-trade pipeline — pre-flight

Date: 2026-11-13. Scope: `web/src/transactions-panel/` curve, V2 router and V4 router trade panels.

## Problem

`trading-panel.tsx`, `graduated-swap-panel.tsx` and `v4-swap-panel.tsx` each carried their own
copy of the reviewed-write pipeline (lock, wallet re-checks, simulation, re-quote, sign, receipt,
failure phase). The copies had drifted: different wallet re-check points, an approval left in
`simulating` after a wallet switch (V2 router), approval failures classified with a coarse phase
(V4), and review notices that two panels never rendered outside the confirmation.

## Decisions

1. **One pipeline, `useReviewedTrade` (`reviewed-trade.tsx`).** Panels keep what differs: the quote
   source, the intent they build, balance/allowance preflight, and confirmation details. The hook
   owns state, the execution lock and the signing steps.
2. **Signing always targets `reviewed.target` / `reviewed.functionName`.** The write is built from
   the reviewed intent only, never from panel constants.
3. **Wallet re-check immediately before every signature** (each approval and the trade), using the
   strictest form the panels had: chain id, wallet's first account, the app's selected account and
   `selectedAccountVerified`. The redundant intermediate checks some panels had between two
   awaits-free steps are dropped; none sits closer to the signature than the kept one.
4. **A wallet or network switch is a review notice, never a transaction failure.** Any state the
   pipeline set before it (trade `simulating`, approval `simulating`) is restored.
5. **Failure phase is precise for approvals** (simulation → write → receipt) in all panels.
6. **After allowances mine, every panel shows a notice and asks for a fresh review**, as V4 did.
7. The curve panel's transient `mined` state before `indexing` is dropped; it was overwritten in
   the same tick.

## Verification

Anvil v1 (22) and v2 (1) Playwright gates drive the real controls through buy, approval, resumed
sell, wrong-chain and rejection paths on all three panels.
