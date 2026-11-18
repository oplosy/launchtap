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


### Frontend rebuild — complete

The web UI is rebuilt around a centred composition, a pure-black dark theme with a light theme,
a monochrome interface with colour reserved for data, and a new information architecture
(`/explore` replaces the old discovery routes, which redirect)
([spec](specs/2026-11-10-frontend-rebuild-design.md), [plan](plans/2026-11-10-frontend-rebuild.md)).
Merged in PR #23.

### Launch engine 2 — code complete; external audit and live run externally gated

A second, separately deployed engine, the one planned for mainnet. It adds an anti-snipe tax,
graduation into a Uniswap v4 pool whose position is locked forever in an LP-fee locker that shares
fees between creator and protocol, and a two-step creator fee-recipient handover
([spec](specs/2026-11-11-v2-engine-design.md); contracts, backend and web plans dated
2026-11-11/12).
- Contracts, backend and web delivered in PRs #26–#31, then hardened through #52, #71 and #92 and
  the audit passes.
- A pinned mainnet fork suite and a deep fuzz and invariant campaign cover it.
- The external audit package is `audits/external-audit-package.md`.
- Robinhood testnet has no Uniswap v4. PR #114 adds a rehearsal path there on a self-deployed,
  pinned v4 stack ([pre-flight](specs/2026-11-18-v2-testnet-path-preflight.md)).

## Next

No new feature scope is decided. What stands between the repository and mainnet, with owners and
order, is in the [gap assessment](audits/2026-11-18-project-gap-assessment.md). Almost all of it
is owner-held:
- the external audit;
- the live testnet runs of both engines;
- governance;
- hosting;
- a legal position.

The code-side items are the legal pages, after the legal opinion, and anything the audit finds.

## Deferred operational work

Tracked in [`backlog.md`](../backlog.md) → "Operator-only inputs":
- the testnet engine switch and the engine 2 testnet rehearsal;
- the archive fork RPC secret and a second RPC provider;
- the CoinGecko and Telegram keys;
- an incident-response rehearsal;
- the production release, governance, monitoring-owner, external-audit and legal inputs.

The Privy 3 upgrade is a separate backlog item.

## Delivery rule

`dev` contains active implementation. `main` receives only a verified milestone merge. A
green unit test run is not sufficient evidence for live testnet operation or production launch.
