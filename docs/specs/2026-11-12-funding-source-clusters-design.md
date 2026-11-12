# Early-buyer clusters Level 2: funding source

Status: Claude design, 2026-11-12. The owner asked for Level 2 to be built after the pre-flight
(`docs/specs/2026-11-12-funding-source-clusters-preflight.md`) found no usable data source on the
public RPC. This takes the pre-flight's recommended option A (an archive + trace RPC) and ships
it off by default: nothing runs and nothing changes on the page until `FUNDING_RPC_URL` points at
such an endpoint. Extends `docs/specs/2026-11-12-early-buyer-clusters-design.md` (Level 1).

## Goal

Link early wallets that got their first ETH from the same wallet, show it as a third link kind
next to Level 1's, and say how many early wallets were checked so an empty result never reads as
"no shared funding".

## Non-goals

- Funding after the first one, multi-hop funding trees, cross-token "operator" profiles.
- A curated exchange/bridge list shipped in code. The list is operator data (a table).
- Any change to Level 1 links or the response when funding data is absent.

## Decisions

| Decision | Rationale | Rejected alternative |
| --- | --- | --- |
| **Funding source = the sender of a wallet's first ETH.** Found per wallet with an archive RPC: binary-search the first block where the nonce is above zero (nonces only grow), then the first block before it where the balance is above zero (an EOA cannot lose ETH without sending, so the balance only grows before its first transaction). In that block, a top-level transaction to the wallet with value gives the sender; otherwise the call tracer's internal calls with value to the wallet give the caller. | About 60 RPC calls per wallet, exact, independent of how long ago the funding was. The "first funding" heuristic is the standard sybil signal. | Scanning blocks forward (unbounded); explorer APIs (third-party correctness and keys). |
| **Statuses**: `funded` (funder and transaction known), `unfunded` (no ETH ever arrived by the checked block: the recipient was picked by someone else's buy), `contract` (the wallet has code; nonce rules do not apply), `unknown` (balance appeared without a value transfer the RPC shows, e.g. a sponsored transaction or a system deposit). Only `funded` rows link. | Each state is a fact the page can count; nothing is guessed. | Collapsing everything else into "unknown". |
| **One row per wallet per chain** (`wallet_funding`), resolved only for early wallets whose delivery block is finalized, searched up to the finalized head. Rows are never rewritten: finalized history is immutable, so there is no reorg handling. An `unfunded` wallet stays unfunded as of `checked_block`. | Funding is a wallet fact, shared by every token the wallet bought early. | Per-token rows (duplicate RPC work). |
| **Worker** `cmd/funding`: optional process, one per chain and deployment (advisory lock), every 30 s resolves up to 20 waiting wallets, one at a time; an RPC failure skips that wallet until the next pass. Config: `CHAIN_ID`, `DEPLOYMENT_ID`, `DATABASE_URL`, `LOG_LEVEL`, `FUNDING_RPC_URL` (secret, must answer `eth_getBalance`/`eth_getTransactionCount` at old blocks and `debug_traceBlockByNumber`). | Keeps the singleton indexer and its RPC budget untouched; off until an endpoint exists. | A goroutine inside the indexer (couples ingestion to a slow optional RPC). |
| **Link rule**: early wallets of one token whose funder is the same wallet are linked (`funding_source`); a funder that is itself an early wallet links to the wallets it funded. A funder is ignored when it is listed in `funding_excluded_funders` (operator-maintained exchanges and bridges), is any token's curve, or funded more than `HubLimit` (25) of this token's early wallets. | Same explainable connected-components model and hub cut-off as Level 1; exchanges and bridges never merge users. | Shipping a guessed exchange list. |
| **API**: the early-buyers response gains `funding_checked` (early wallets with a funding row of any status) and the `funding_source` link kind. The web shows "Funding sources checked for N of M early wallets." only when N > 0, and words the link "funded by the same wallet". | The reader always sees coverage; with the worker off the page is unchanged. | A separate endpoint. |

## Risks

- **Shared funders that are not one person** (a faucet, a payroll wallet, an unlisted exchange):
  mitigated by the hub cut-off and the exclusion table; wording stays "linked".
- **RPC cost**: about 60 calls per new early wallet; the pass cap bounds the rate.
- **L2 specifics**: deposits arrive as system transactions; their sender is the aliased L1
  address, which the exclusion table can list for the canonical bridge.

## Acceptance

- Resolver tests against Anvil: a wallet funded by a top-level transfer, one funded by a
  contract's internal call, an unfunded recipient, a contract wallet.
- Grouping unit tests: shared funder links, funder that is early, excluded and hub funders.
- Integration: the worker query picks only finalized, unresolved early wallets; the reader
  returns `funding_checked` and funding links, honouring the exclusion table and curves.
- API wire test; OpenAPI and web types regenerated; web unit test for the coverage line and the
  link wording.
