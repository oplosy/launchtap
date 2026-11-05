# P0-A — Market data and discovery parity

Status: design approved in conversation on 2026-11-05; awaiting written-spec review.
Scope owner: product owner. Sub-project A of the P0 parity package (A → B → C):
B is live trade feed, notifications and share cards; C is per-token comments.

## Goal

A trader arriving from a competing launchpad (notably `pons.family` on the same chain) must not
find the core discovery and pricing experience missing. Success means:

- every ETH price, market cap, volume, quote and claim amount also shows a USD value when a
  fresh reference price exists, and shows no USD value (never `$0` or a guess) when it does not;
- the tokens that are moving and the token closest to graduation are one click from Explore;
- a signed-in user keeps a private watchlist that follows them across devices;
- a graduated token links to Dexscreener and GeckoTerminal when the chain is supported there;
- the existing `volume_24h` value and Volume sort decay when a token stops trading.

## Non-goals

- No contract change, no new engine version.
- No server-side USD storage or USD sort; ETH ordering is identical to USD ordering.
- No public watchlist counts or other social signals (sub-project C decides those).
- No live trade feed, notifications or share cards (sub-project B).

## Decisions

| Decision | Rationale | Rejected alternative |
| --- | --- | --- |
| The API process polls ETH/USD and serves it at `GET /v1/reference/eth-usd`; the web converts ETH amounts to USD with exact bigint/decimal math. | One reference value for every surface (lists, detail, trade panel, quotes, history, claims); no migration; no indexer coupling, as the ETH/USD runbook requires. | Writing USD into the existing nullable `token_stats` USD columns at aggregation time: the values go stale with every ETH price move and would require re-aggregating every token per price tick. Those columns stay unused and `NULL`. |
| Provider is the CoinGecko commercial (Pro) API, per `docs/runbooks/eth-usd-enrichment.md`. | Source selection is already reviewed; the product owner will buy the commercial plan. | Demo plan (not licensed for commercial use); Coinbase public ticker (terms). |
| Trending and king of the hill are computed at query time by `GET /v1/tokens/highlights`, cached 15 s per API process. | A 60-minute window must slide even when nothing trades; query-time computation is always fresh and needs only indexes. Current scale does not justify a precomputed table. | Precomputing in the aggregation worker plus a decay sweep: more moving parts for no measured need. Revisit if the highlights query exceeds its latency budget. |
| Trending score = 60-minute gross ETH volume × distinct buyers in the same window. | Rewards breadth of demand; one wallet cycling buys and sells cannot dominate as easily as with volume alone. | Volume only (wash-tradable); most recent buy (weak quality signal). |
| Watchlist is stored server-side, keyed by Privy DID, scoped to chain and deployment. | Cross-device; later notifications (sub-project B) can read it; survives wallet changes. | Browser-only storage (lost on device change, cannot drive notifications). |
| Dexscreener and GeckoTerminal chain identifiers come from optional reviewed manifest fields. | Robinhood Chain support and slugs on those sites are unverified; an absent field hides the links instead of guessing a URL. | Hard-coded slugs. |
| The 24-hour volume fix reuses the existing dirty-token recomputation. | `stats.ComputeTokenStats` already windows candles correctly; it only runs when a token is marked dirty, so a token that stops trading keeps a stale value. Marking the tokens whose trades are leaving the window is the smallest correct change. | A new decay computation path. |

## Backend

### ETH/USD reference price

- New package `internal/pricefeed`, stdlib-only (add a depguard rule). It owns the provider call,
  validation and the in-memory sample; `cmd/api` starts it only when `ETH_USD_SOURCE=coingecko`.
- Request: `GET https://pro-api.coingecko.com/api/v3/simple/price?ids=ethereum&vs_currencies=usd&include_last_updated_at=true&precision=full`
  with header `x-cg-pro-api-key`. The key comes from `COINGECKO_API_KEY` (backend secret only,
  never a `NEXT_PUBLIC_*` value, never logged).
- Policy, as in the runbook: poll at most once per 60 s and coalesce concurrent refreshes; accept a
  sample only when `last_updated_at` is present, not in the future, and at most 120 s old at read
  time; parse the price as an exact decimal (`json.Number`), positive, at most 18 fractional
  digits; on 429 back off exponentially with jitter; on 5xx, timeout or malformed data keep the
  last sample marked stale.
- Response body: `price` (decimal string or `null`), `provider_updated_at`, `fetched_at`,
  `source` (`"coingecko"`), `attribution_url`. `price` is `null` whenever the sample is missing,
  stale or the source is disabled; the endpoint itself never fails because of the provider.
- Config: `ETH_USD_SOURCE` is already read but not validated today; it becomes empty = disabled,
  `coingecko` = enabled, any other value rejected at startup. `COINGECKO_API_KEY` is required when
  the source is `coingecko` and rejected when it is not.

### Highlights

- `GET /v1/tokens/highlights` returns `trending` (at most 20 token summaries with their window
  volume and distinct-buyer count) and `king_of_the_hill` (one curve-phase token summary with the
  highest graduation progress, or `null`), plus `window_start`, `as_of` and finality metadata
  consistent with the other read endpoints.
- Window: the last 60 minutes of canonical trades. The query reads `trades` and `pool_swaps`
  directly (not the `market_trades` view) so it can use time indexes. Buyers are
  `trades.trader` where `is_buy`, and `pool_swaps.to_address` for DEX buys (the view exposes no DEX
  trader). A swap routed through an aggregator counts that contract as one buyer; this is an
  accepted limitation.
- Ties break by most recent trade, then token address. Reorged rows are excluded by the same
  canonical-block join the read paths already use.
- Migration adds `trades (chain_id, block_time)` and `pool_swaps (chain_id, block_time)` indexes.
- Cached 15 s per API process; the cache key is chain and deployment.

### Age filter

- `GET /v1/tokens` accepts `age=all|24h|7d` (default `all`); other values return 400
  `invalid_age`.
- The cutoff timestamp is fixed when the first page is requested and carried in the cursor, so
  pages do not shift while a user paginates.

### 24-hour volume decay

- The indexer's aggregation worker runs a sweep once per minute that marks dirty every token with
  a canonical trade in `[now − 24 h − 2 min, now − 24 h)`. The two-minute overlap absorbs sweep
  jitter; the existing recomputation then drops that volume from `volume_24h` and the Volume sort.
- No new aggregation logic; the existing dirty lease and batching apply.

### Watchlist

- Migration creates `watchlist_entries (chain_id, deployment_id, privy_did, token_address,
  created_at)`, primary key on the first four columns. No foreign key to `tokens`: a token removed
  by a reorg stays listed and is reported as unavailable rather than deleting the user's entry.
  The down migration drops the table, which deletes user data; that is stated in the migration.
- Endpoints, all requiring the existing Privy access-token plus identity-token verification:
  - `GET /v1/me/watchlist` — newest first, at most 200 entries, each with the token summary or an
    unavailable marker;
  - `PUT /v1/me/watchlist/{token}` — idempotent add; 404 when the token is not in this deployment;
    409 `watchlist_full` above 200 entries;
  - `DELETE /v1/me/watchlist/{token}` — idempotent remove.
- Responses carry `Cache-Control: private, no-store`. Logs never contain the DID. Existing CORS and
  per-client rate limits apply.

### Contract and drift surfaces

OpenAPI (`backend/openapi/v1.json`), the generated web client, the deployment manifest schema
(optional `marketLinks.dexscreenerChain` and `marketLinks.geckoterminalNetwork`), and the generated
web contracts change together and stay under the existing drift checks.

## Web

- `useEthUsd()` fetches the reference price once per minute and pauses while the tab is hidden.
  `formatUsd` multiplies the base-unit bigint by the decimal price exactly and rounds only for
  display.
- USD appears as a secondary line beside ETH on list cards, token detail metrics, the trade panel
  inputs and quote, trade history and profile claims. When `price` is `null` the USD line is
  absent. The footer and every panel that shows USD carry “Data provided by CoinGecko” linking to
  the attribution URL.
- Explore tabs: Trending (default), New, Market cap, Volume, Watchlist. A King of the hill card
  sits above the list with its graduation progress and remaining ETH. The age filter
  (All / 24h / 7d) and the active tab live in the URL.
- A star control on cards and on token detail adds or removes a watchlist entry optimistically,
  rolls back with a toast on failure, opens Privy sign-in when signed out, and shows
  “Watchlist full (200)” on 409.
- Token detail for graduated tokens adds Dexscreener and GeckoTerminal links built from the pair
  address and the manifest identifiers; links are hidden when the identifiers are absent.

## Failure behaviour

Each new source fails independently and never blocks the rest of a page: a missing price hides
USD; a failed highlights request shows the existing error state on Trending with a switch to New;
a 401 on a watchlist call starts sign-in; indexing, quotes and transactions never depend on any of
these features.

## Testing and acceptance

- Go unit tests: price feed (fresh, stale, future timestamp, missing field, malformed and
  over-precision decimal, 429 backoff, 5xx), config validation for the new variables, highlights
  scoring, ties and cache, age-filter cursor stability, watchlist authorization, limit and
  idempotency.
- PostgreSQL integration tests: highlights query against canonical and reorged rows, the 24-hour
  sweep lowering `volume_24h` without new trades, watchlist migration up/down/up.
- Web unit tests: exact `formatUsd`, USD hidden on `null`, optimistic star rollback, tab and age
  URL state.
- Anvil browser gate: add and remove a watchlist entry, see a traded token in Trending.
- All existing gates stay green: `task verify` (including OpenAPI and sqlc drift), web typecheck,
  lint, unit tests and build, codegen drift, `forge test`.

## External inputs

- CoinGecko commercial API key (product owner is purchasing it). Until it exists, the source stays
  disabled and USD is absent everywhere.
- Verified Dexscreener and GeckoTerminal identifiers for Robinhood Chain; until then the links
  stay hidden.
