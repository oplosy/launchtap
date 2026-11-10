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
- **P0-C2 comments, usernames, moderation — complete.** Holder/creator-gated token comments with
  rate limits, reports, an operator moderation CLI, and usernames with opt-in published wallets
  ([spec](specs/2026-11-09-p0c2-comments-usernames-design.md)).
- **P0-C3 watch counts and windowed leaderboards — complete.** Public watcher counts and 7d/30d
  realized-PnL leaderboards ([spec](specs/2026-11-10-p0c3-watch-counts-windowed-leaderboards-design.md)).
- **P0-C4 notification inbox — complete.** Server-side inbox with unread counts and cross-device
  read positions ([spec](specs/2026-11-10-p0c4-notification-inbox-design.md)).
- **P0-C5 forum (Memestock) — complete.** Cross-token feed and hot/top communities over token
  comments ([spec](specs/2026-11-10-p0c5-forum-memestock-design.md)). The P0-C package merged to
  `main` in milestone PR #21.


## Next

No new feature scope is decided. Signed-in e2e coverage (a fixture-only test session) landed on
`dev` and goes to `main` with the next milestone PR. The only open feature work in
[`backlog.md`](../backlog.md) → Active is the forum ticker tape and votes, which need an owner
decision.

## Deferred operational work

Tracked in [`backlog.md`](../backlog.md): the production CoinGecko key and market-link
identifiers, the Privy 3 upgrade, the testnet engine switch and fork RPC secret, and the
production release, governance, monitoring-owner and external-audit inputs.

## Delivery rule

`dev` contains active implementation. `main` receives only a verified milestone merge. A
green unit test run is not sufficient evidence for live testnet operation or production launch.
