# Early-buyer clusters (Level 1)

Status: Claude design, 2026-11-12, approved by the product owner ("Level 1" of the sniper
clustering discussion). Source: trust & risk layer of the 2026-11-11 product research report.
Builds on the launch-block holders line (PR #39).

## Goal

Show, on the token page, which of the wallets that got tokens from the curve right after launch
are linked to each other by on-chain facts we already index, and how much of the supply those
linked groups still hold.

Success means a reader can see "these 4 early wallets are linked and hold 18% of the supply",
check every link on the explorer, and never sees a wallet grouped by a guess.

## Non-goals

- Funding-source clustering ("who sent these wallets ETH", Level 2). Native ETH transfers are
  not in our ledger; it needs a trace/archive RPC or an external indexer and an exchange/bridge
  exclusion list. Deferred until real trading shows the need.
- Labels such as "sniper" or "bundler", a risk score, notifications, or an Explore filter. The
  page states facts ("linked", "same transaction", "token transfer"), not accusations.
- Any write path, projection table or migration. Everything is computed at read time.

## Decisions

| Decision | Rationale | Rejected alternative |
| --- | --- | --- |
| **Early wallet** = a recipient of a curve delivery (`Transfer` from the token's curve) in a block whose time is within 60 seconds of the launch block's time, launch block included. | Curve deliveries are exactly buy outputs (sells transfer to the curve), and the recipient is who holds the tokens. 60 s covers bots and bundles on any block cadence; the launch-block line (PR #39) already covers the narrowest case. Both engines emit the same `Trade`/`Transfer` shape. | The `Trade.trader` (the payer, which can be a shared router); engine 2's anti-snipe duration (engine-specific, engine 1 has none); a configurable window (a knob nobody tunes yet). |
| **Same-transaction link**: early deliveries to two or more distinct recipients in one transaction link those recipients. | One transaction has one signer, so the recipients were chosen by one actor. Unambiguous. | Linking by the shared payer across transactions: a public bot or router contract would merge unrelated users. |
| **Token-transfer link**: a `Transfer` of this token (any time) between two wallets links them when neither is infrastructure (zero, dead, curve, pair, locker, PoolManager per `token_holder_excluded`) and either both are early wallets, or one is early and the other is a non-early wallet that **currently holds a positive balance**. | Consolidation into one wallet is the usual follow-up of a bundle. Requiring the outside wallet to hold tokens now excludes routers and aggregators, which end every swap at zero. | Any transfer (routers and aggregators would merge sellers); early-to-early only (misses consolidation into a fresh wallet). |
| **Hub cut-off**: a non-early wallet linked to more than 25 early wallets is dropped with all its links. | A wallet receiving from that many early buyers is far more likely a contract or service than one person; dropping it can only under-group. | No cut-off (one airdrop or multisend contract could merge everyone). |
| **Clusters** = connected components over early wallets and the outside wallets the token-transfer rule admits; only components holding two or more **early** wallets are reported (one early wallet moving its tokens to another wallet is not a group). Each cluster lists its wallets with current balances, the combined balance, the link kinds present (`same_transaction`, `token_transfer`), and whether the token's creator is in it. | Connected components are deterministic and explainable: every grouping follows a chain of listed facts. The creator flag answers "is the dev linked to early buyers?". | Weighted scores or ML clustering (not explainable, not checkable on the explorer). |
| **API**: `GET /v1/tokens/{token}/early-buyers`, snapshot-consistent like the holders read. Body: `snapshot`, `window_seconds` (60), `window_open` (the 60 s have not passed at the snapshot block), `early_wallet_count`, `early_balance` (what all early wallets hold now), `cluster_count` (before the cap), `clusters` (largest combined balance first, at most 10), each with `wallet_count`, `balance`, `links`, `includes_creator` and `wallets` (largest first, at most 20, each `address` + `balance`). Unknown token → 404 like the other token reads. | One small read the token page asks for once; caps keep the body bounded on a busy token while counts stay exact. | A field on token detail (makes the hottest read heavier); pagination (the capped list is the whole story at launch scale). |
| **Computation**: three snapshot reads (early deliveries; this token's transfers touching an early wallet; current balances of the involved wallets), then union-find in a stdlib-only Go package with unit tests. | Keeps the SQL simple and indexed (`transfers_token_rebuild_idx`), and the grouping logic testable without a database. | One recursive SQL query (hard to test, hard to cap hubs). |
| **Web**: the holders tab adds "Linked early wallets" under the concentration lines: a summary sentence, then one row per cluster (wallet count, share of supply floored to 0.01%, link kinds in words, a "creator" badge) that expands to its wallet links. With no cluster: "No linked early wallets found." While `window_open`: the summary says the window is still open. | Same exact-bigint, never-overstate rules as the other holder lines; neutral wording. | A separate tab (the holders tab is where the reader is looking at concentration). |

## RISKS

- **False grouping** through an outside wallet that holds tokens and is not one person (a
  multisig, a small distributor contract under the hub cut-off). Mitigation: every link is a
  listed fact; the wording says "linked", not "same owner".
- **Missed grouping** of careful operators (separate funding, no shared transaction, no
  transfers between wallets). That is Level 2's job; the page never claims completeness.
- **Read cost** grows with the token's transfer count (the transfer read scans this token's
  transfers touching early wallets). Bounded by the early set; acceptable behind the read cache
  at launch scale. If it gets slow, add a transfer index on `(chain_id, token_address,
  from_address)` / `to_address`.
- **Reorgs**: nothing is stored, every read is snapshot-consistent, so a reorged delivery simply
  disappears on the next read.

## ACCEPTANCE

- Unit tests for the grouping: same-transaction link, early-to-early transfer, consolidation into
  a holding outside wallet, a zero-balance outside wallet (router) not linking, the hub cut-off,
  infrastructure never linking, one early wallet moving tokens not forming a group, creator flag, caps and ordering.
- Integration test against PostgreSQL with fixture deliveries and transfers inside and outside
  the window.
- API wire test; OpenAPI and web types regenerated; web unit tests for the copy and percentages;
  token-detail e2e with a fixture cluster.
