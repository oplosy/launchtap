# Early-buyer clusters Level 2 (funding source): pre-flight

Status: Claude pre-flight, 2026-11-12. Superseded: the owner asked for Level 2 to be built anyway;
option A is built off by default, see `docs/specs/2026-11-12-funding-source-clusters-design.md`.
Original status: not built. The owner asked for the deferred work to be
done while away; this records why Level 2 stops at a data-source decision the owner has to make.
Follows `docs/specs/2026-11-12-early-buyer-clusters-design.md` (Level 1, shipped).

## Goal

Link early wallets that were funded with ETH by the same source wallet shortly before they
bought ("who sent these wallets ETH"), with an exclusion list so exchanges and bridges never
merge unrelated users.

## What the feature needs

For each early wallet, its funding transfer(s): the sender of the native ETH that arrived before
its first buy. Native ETH transfers are not events, so the ledger (logs only) has none of them.
They come either from top-level transaction values (any block, any time before the buy) or from
internal calls (contract wallets, bridges, batch senders), which only traces show.

## Probe (2026-11-12, official mainnet RPC `https://rpc.mainnet.chain.robinhood.com`)

| Need | Result |
| --- | --- |
| Historical balance (`eth_getBalance` at an old block; lets a worker binary-search the first funding block) | Refused: "historical state … is not available" (not an archive node). |
| Internal transfers (`debug_traceTransaction` / call tracer) | Refused: method not available. |
| Explorer API (`robinhoodchain.blockscout.com/api/v2`) | Behind a browser challenge for automated clients; no documented key-based access checked. |

Scanning every block's transactions for transfers to early wallets from the RPC alone is possible
in principle but unbounded (funding can be weeks old) and still misses internal transfers.

## Decision

Level 2 is **not built** until one of these sources exists; the owner picks:

| Option | What it costs | Coverage |
| --- | --- | --- |
| **A. Archive + trace RPC** (a paid provider or our own archive node) | Provider subscription or node ops; a secret RPC URL | Complete: binary-search the first funding block, trace it for internal transfers. Recommended. |
| B. Explorer API with a key (Blockscout or another indexer) | Third-party dependency, rate limits, an API key | Good for top-level and usually internal transfers; correctness depends on the explorer. |
| C. Forward-only top-level scan in our indexer (record `tx.value` transfers to and from wallets once they become early) | No new provider; more indexer RPC load (full blocks) | Partial: only funding seen after indexing starts and only top-level transfers; misses the usual case of wallets funded before launch. Not worth the load. |

Once a source is chosen, the design follows Level 1: read-time grouping where possible, a small
`wallet_funders` cache written by a bounded worker (funding facts are immutable once safe), an
exchange/bridge exclusion list kept as reviewed configuration, the same hub cut-off, and a new
`funding_source` link kind on the existing early-buyers response.

## Risks of building it anyway

- On the current RPC every wallet would show "funding unknown"; an empty Level 2 reads as
  "no linked funding", which overstates safety. The page must never imply completeness.
