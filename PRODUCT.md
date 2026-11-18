# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Next.js App Router with strict TypeScript and Tailwind CSS. The stack is already fixed by
Plan 4 Task 1. Visual direction is delegated for this task.

## Users

Primary users are creators launching a fixed-supply token and participants inspecting or
trading tokens on the supported EVM deployment. They need a clear, non-custodial operating
surface for discovery, launch, trading, analytics, documentation, and account state.

This audience and usage scene are inferred from the approved web-client design and Plan 4
scope, not from a completed interview.

## Product Purpose

Launchpad is a non-custodial fixed-supply token launchpad. A token moves from a bonding curve
to graduation and then to a Uniswap v2 pool with the initial LP position burned. The web client
must make that lifecycle understandable and keep contract and backend authority boundaries clear.

## Positioning

The product connects a visible launch-to-liquidity lifecycle with executable on-chain economics,
while keeping signing in the user-selected wallet and indexed history in the backend API.

## Operating Context

The browser is an Operate-mode application used on phone, tablet, small laptop, and wide desktop.
Indexed reads come from the backend API. Contract reads, simulation, wallet transactions, and
receipts use direct RPC. The supported deployment is selected from a reviewed manifest and may be
unavailable until public configuration is complete.

## Capabilities and Constraints

- Explore and Graduated discovery, token detail, candles, trades, holders, launch, curve buy and
  sell, post-graduation routing, analytics, profile, and static docs are Plan 4 scope.
- Forum/Memestock, heatmap semantics, fabricated USD enrichment, governance/admin screens, and
  server-side signing are out of scope.
- Contract economics and executable quotes are authoritative on-chain; canonical history and
  aggregates are authoritative in the backend API.
- Amounts remain base-unit decimal strings or `bigint`; JavaScript floating-point values are
  presentation-only.
- Browser routes must fail closed when deployment, chain, API, or RPC configuration is missing.
- User-controlled metadata is untrusted. No arbitrary HTML is rendered. External links are HTTPS
  only and use safe opener behavior.

## Brand Commitments

The name is Launchpad. The visual language is delegated for Plan 4 Task 2: serious, precise,
trust-first, dark/high-density crypto tooling with a distinctive launch-to-liquidity identity.
Avoid generic purple gradients, gradient text, neon halos, fake terminal UI, excessive whitespace,
and invented market values or commercial claims.

## Evidence on Hand

- `docs/specs/2026-09-09-web-client-design.md` defines route, data, wallet, finality, visual,
  and security requirements.
- Task 1 generated a fail-closed public configuration helper and a minimal browser ABI bundle.
- No verified production Privy values, testnet deployment manifest, API/RPC URLs, or ETH/USD source
  are available yet.
- `pons.family` was requested as a visual reference on 2026-09-10 but was inaccessible (HTTP 502).

## Product Principles

1. Make the launch-to-liquidity state legible before asking for action.
2. Keep custody, signing, API, RPC, receipt, indexing, and finality boundaries explicit.
3. Treat missing, stale, provisional, and unavailable data as honest product states.
4. Prefer dense, scan-friendly operating surfaces over decorative crypto tropes.
5. Never invent market values, social proof, production configuration, or return claims.

## Accessibility & Inclusion

The shell must support full keyboard navigation, visible focus, semantic landmarks, touch targets
that work at 360px, reduced motion, readable contrast, and first-class loading, empty, unavailable,
stale, failure, and disabled states. These requirements are inferred from the approved Plan 4
acceptance criteria and are binding for this task.
