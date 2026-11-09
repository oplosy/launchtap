# Runtime dependency advisory review — 2026-11-06

Scope: every advisory `npm audit --omit=dev` reports for `web/` at commit 49e1d92
(9 low, 14 moderate, 23 high package findings; these collapse to 9 distinct advisories).
`npm run audit:runtime` blocks on high/critical only, so this review covers the low/moderate
ones the production-readiness item listed as "still requiring impact review", and re-checks
the two allowlisted highs.

## Reachability baseline

- Every wallet SDK (Privy, wagmi connectors, WalletConnect, MetaMask SDK, Solana web3) is
  loaded only in the browser: `web/app/providers.tsx` imports `@/wallet/wallet-stack`
  dynamically and the module is `"use client"`. None of these packages runs in the Next.js
  server, the API or the indexer.
- The app never imports `uuid`, `stream-json`, `elliptic` or any Solana package directly
  (`grep` over `web/src` and `web/app`).
- Installs use `npm ci` against the committed lockfile, so the resolved versions below are the
  ones that ship.

## Findings

| Advisory | Severity | Package (path) | Verdict |
| --- | --- | --- | --- |
| GHSA-qj3p-xc97-xw74 | moderate | `@metamask/sdk` 0.32.0 (wagmi → `@wagmi/connectors`) | **Not affected.** The advisory is exposure to the malicious `debug@4.4.2`; the lockfile resolves `debug` to 4.4.3 and 3.2.7 only. |
| GHSA-vcc3-ghjq-m6fr | moderate | `decode-uri-component` 0.2.2 (wagmi connectors → WalletConnect utils → `query-string`) | **Accepted, low impact.** Client-side only; worst case a crafted WalletConnect URI stalls the user's own tab. Same path as the allowlisted high GHSA-qr28-fpjm-wq7f. |
| GHSA-528h-pc64-c93x, GHSA-hqr4-qq8f-hg3x, GHSA-mjw6-4jj6-33hc | moderate | `stream-json` 1.9.1 (Privy → `@solana/web3.js` → `jayson`) | **Not reachable.** `jayson` calls `stream-json` only from its Node TCP/TLS transports; `@solana/web3.js`'s browser build imports `jayson/lib/client/browser`, and the app enables no Solana wallet. |
| GHSA-w5hq-g745-h8pq | moderate | `uuid` 8.3.2 / 9.0.1 (MetaMask utils, Privy, jayson) | **Not reachable.** Needs a caller passing `buf` to v3/v5/v6; the dependents generate v4 ids. |
| GHSA-848j-6mx2-7j84 | low | `elliptic` 6.6.1 (Privy js-sdk-core → `@ethersproject/signing-key`) | **Accepted, low impact.** Used for transaction serialisation helpers in the browser; user signatures are produced by the connected wallet or Privy's isolated embedded-wallet iframe, not by this copy. |
| GHSA-mrhx-c7xc-m3w5 | high (allowlisted) | `zod` 3.x | Allowlist reason still holds (schemas fed by trusted constants). |
| GHSA-qr28-fpjm-wq7f | high (allowlisted) | `decode-uri-component` 0.2.2 | Allowlist reason still holds (client-side DoS of the user's own tab). |

## Privy 3 does not clear the allowlisted highs

Trial install (scratch copy of `web/`, not committed) of the newest matched set at least two weeks
old — `@privy-io/react-auth` 3.49.0, `@privy-io/wagmi` 4.0.19, `viem` 2.57.5 — with wagmi
2.16.9, 2.19.5 and 3.7.9:

- `@privy-io/react-auth` 3 itself depends on `x402`, which depends on `wagmi` 2 →
  `@wagmi/connectors` → WalletConnect 2.21.1 → `query-string` 7 → `decode-uri-component` 0.2.2.
  Upgrading the app's own wagmi to 3.x does not remove that path.
- `zod` 3 still arrives through `x402`, `@reown/appkit-wallet`, `abitype` and wagmi connectors.
- Result: the same two highs remain (23–24 high package findings), plus a viem peer conflict
  between Privy 3.49 and wagmi 3.

The upgrade therefore buys no audit improvement while touching wallet authentication, so it is
not done. Revisit when Privy drops `x402`'s wagmi 2 dependency or moves to zod 4.

## Next review

Before the allowlist `reviewBy` date (2027-02-01), or on any new high/critical advisory.
