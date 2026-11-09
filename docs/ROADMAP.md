# Launchpad roadmap

This roadmap is a delivery view of the normative specifications and task plans. The plans
remain the acceptance authority; this file explains what is currently usable and what comes
next. Unfinished, externally gated work is tracked in [`backlog.md`](../backlog.md).

## Delivered

### Contract Foundations — complete

The Solidity system, fixed-supply launch token, integer bonding curve, graduation flow,
Uniswap v2 pair creation, governance controls, deployment artifacts, curve vectors, invariant
tests, and release/fork gates are implemented.

### Backend Foundations — complete

The Go module, validated configuration, deployment registry, PostgreSQL migrations, canonical
event ledger, projections, sqlc adapters, transaction primitive, and pure Go curve mirror are
implemented and covered by the repository gates.

### Backend Indexer — code complete; live acceptance externally gated

RPC access, ABI decoding, staged discovery, canonical event persistence, incremental
projections, advisory ownership, chunk processing, reorg recovery, aggregate recomputation,
dirty-work polling, notifications, and the health endpoint are implemented and verified. Pool
swaps record their transaction sender, and the indexer retries missed sender lookups below the
safe head on its own.

The deterministic implementation gate includes the Anvil indexer path and live-state health
surface. The read-only public-RPC probe is recorded in
[`docs/runbooks/robinhood-rpc-probe.md`](runbooks/robinhood-rpc-probe.md). Public-network
operation still requires a reviewed chain-46630 dependency and Launchpad deployment manifest;
see [`docs/runbooks/robinhood-testnet-deployment.md`](runbooks/robinhood-testnet-deployment.md).

### API and identity (Plan 3) — code complete

Huma REST endpoints, finality and `asOfBlock` response metadata, Privy access-token and
linked-wallet verification, quote DTOs, pagination, and SSE streams
([`docs/plans/2026-09-08-backend-api-identity.md`](plans/2026-09-08-backend-api-identity.md)).

### Web delivery (Plan 4) — code complete; live release externally gated

All eight Plan 4 tasks are implemented and independently reviewed. The deterministic web,
backend, contracts, drift, browser, and Anvil gates are wired into CI. The release and handoff
boundary is documented in [`docs/runbooks/production-readiness.md`](runbooks/production-readiness.md).

### Observability — complete

Prometheus metrics on internal listeners for the API and indexer, tested alert rules, and an
opt-in Compose `monitoring` profile with Prometheus and Alertmanager
([`docs/specs/2026-11-06-observability-design.md`](specs/2026-11-06-observability-design.md)).

### P0 parity package

- **P0-A market data and discovery — complete.** ETH/USD reference price (CoinGecko adapter;
  the production key is an external input), trending, king of the hill, and a server-side
  watchlist ([spec](specs/2026-11-05-p0-market-data-discovery-design.md)).
- **P0-B live feed, notifications, share cards — complete.** Live trade feed over SSE with
  burst folding and reconnect recovery, in-app notifications, and token share cards
  ([spec](specs/2026-11-06-p0b-live-feed-notifications-share-design.md)).
- **P0-C1 wallet page, PnL, leaderboard — complete.** Per-wallet positions and realized PnL
  recomputed from the ledger, public wallet pages, the leaderboard, wallet share cards, and
  holdings-based notifications ([spec](specs/2026-11-06-p0c1-wallet-page-pnl-design.md)).

## Next

Owner decisions of 2026-11-09 set the scope:

1. **P0-C2 token comments.** Holders of a token (and its creator) with a Privy-linked wallet can
   comment; per-user rate limits, user reports, and an operator endpoint to hide comments and
   ban users.
2. **Usernames.** Unique, editable usernames tied to the Privy user, shown on wallet pages,
   comments and the leaderboard. No ENS.
3. **Social data.** Public watchlist counts; 7-day and 30-day realized-PnL leaderboards.
4. **Notification inbox.** A server-side inbox with unread counts and cross-device sync. No
   browser push, Telegram, or email.
5. **Forum (Memestock).** Every launch with its community, ranked by activity, built on the
   comment system.

## Deferred operational work

Tracked in [`backlog.md`](../backlog.md): the production CoinGecko key and market-link
identifiers, the Privy 3 upgrade, the testnet engine switch and fork RPC secret, and the
production release, governance, monitoring-owner and external-audit inputs.

## Delivery rule

`dev` contains active implementation. `main` receives only a verified milestone merge. A
green unit test run is not sufficient evidence for live testnet operation or production launch.
