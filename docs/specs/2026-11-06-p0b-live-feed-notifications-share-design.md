# P0-B — Live trade feed, in-app notifications and share cards

Status: Claude pre-flight, 2026-11-06. Scope and design approved by the product owner in
conversation; awaiting written-spec review before Codex plans the implementation.
Sub-project B of the P0 parity package (A → B → C). P0-A is
`docs/specs/2026-11-05-p0-market-data-discovery-design.md`.

## Goal

The product must feel live and be shareable, like competing launchpads. Success means:

- Explore shows a running strip of the latest trades, launches and graduations across all tokens,
  and a token page's Recent trades list updates without a reload;
- while the site is open, a signed-in user is told about activity on tokens they watch, and
  about their own trades confirming;
- a token link pasted into X or Telegram unfurls into a card with the token's image, name,
  ticker, market cap and bonding progress (or graduated state), and the token page offers
  "Share on X" and "Copy link".

## Non-goals

- No browser push, e-mail, Telegram or other out-of-app delivery. No server-side notification
  inbox, unread counts or cross-device sync.
- No profile or wallet share card, no public wallet page, no PnL. These move to sub-project C
  with comments, as the social-identity part of the package.
- No contract change, no indexer ledger change, no new datastore.
- No separate WebSocket service; the existing `/v1/events` SSE stream carries the feed.

## Decisions

| Decision | Rationale | Rejected alternative |
| --- | --- | --- |
| Each API process runs one **trade tail**: on an `api_refresh` hint (coalesced), it reads canonical market trades and graduations after its cursor once and publishes them to the realtime hub as `trade` and `graduation` events. | Database cost is one query per hint per process, independent of the number of viewers; reuses the existing NOTIFY → hub → SSE path. | Every client refetching `/trades` on each hint (load multiplies by viewers; a global strip would query every token); a new WebSocket service (new component for no current need). |
| The feed source is the existing `market_trades` view (curve trades plus post-graduation pool swaps), ordered by `(block_number, transaction_index, log_index)`. | Same rows and ordering the token page already serves, so the live list and a reload never disagree; graduated tokens keep a live feed. | Curve trades only (graduated tokens would go silent). |
| A new `GET /v1/trades/recent` returns the latest global market trades (default 50, max 100) for the initial strip; live events append after it. | A new viewer sees a populated strip immediately; SSE stays a delta channel. | Replaying history over SSE (`Last-Event-ID` backfill), which the hub does not buffer. |
| Notifications are derived entirely in the browser from the SSE stream, filtered by the user's watchlist and the connected wallet, kept for the tab session only. Matching on wallet holdings needs a public wallet-holdings read, which moved to sub-project C with the public wallet page. | The owner chose in-app, session-only delivery; no new table, write path or personal data on the server. | Server-side inbox table with fan-out worker. |
| Share cards are rendered by the web app with Next.js `opengraph-image` / `next/og` (bundled with Next, no new dependency), from server-side reads of our own API, cached 5 minutes. | Card and page share one framework and deployment; values at most minutes old are acceptable for an unfurl. | A Go image renderer in the API (new image/font stack in the backend); static identity-only card (owner chose live market data). |

## Backend

### Trade tail (`internal/realtime` or a new `internal/feed` package; API process only)

- Cursor: `(block_number, transaction_index, log_index)` of the last published row, initialised at
  startup to the current newest row (a restarted process does not replay history).
- On each coalesced `token` or `launch` hint it runs one bounded query for rows after the cursor
  (limit 200), joins token name and symbol, and publishes them oldest first.
- Graduations and launches are published from the `graduations` and `token_launches` tables
  with the same cursor rule.
- **Reorg:** on a `reorg` hint the cursor moves back to the common ancestor, so canonical
  replacement rows are published again. The existing `reorg` event already reaches clients first;
  clients drop rows above the ancestor before the replacements arrive.
- **Finality:** each event carries the row's `finality` (`observed`, `safe`, `finalized`) at
  publish time. A row does not change when it later becomes safe, so on each hint the tail also
  reads the indexer's safe and finalized watermarks (same database round trip) and publishes a
  `finality` event whenever either advances. Watermarks can advance with no new trade and no
  hint, so the tail also runs the watermark read on a 5-second ticker. Clients upgrade every item
  at or below those blocks.
- **Rate limit:** at most 20 feed events (`trade`, `new_token` and `graduation` share one budget)
  per second per process are published individually; beyond that the tail publishes one
  `trade_burst` summary event for the window (trade count per token, plus folded launch and
  graduation totals), so a bot storm or a launch-heavy block cannot flood every stream. Mirrors the
  existing per-stream hint coalescing. Within each read the tail publishes the three tables merged
  in `(block, tx_index, log_index)` order.

### Events (`/v1/events`)

New SSE event types beside `launch`, `token`, `reorg`:

- `trade`: chain, deployment, token, token name and symbol, `source` (`curve` or `pool`), side,
  gross ETH (wei string), token amount (base-unit string), trader address, tx hash, log index,
  block number, block time, finality.
- `graduation`: chain, deployment, token, name, symbol, pair address, tx hash, block number.
- `new_token`: chain, deployment, token, creator, name, symbol, tx hash, block number, read from
  `token_launches` by the same tail. (The existing `launch` event stays a refresh hint without a
  name.)
- `trade_burst`: chain, deployment, window start and end, `items: [{token, count, max_eth_wei,
  traders, traders_truncated}]` (folded trades per token: the largest gross ETH amount as a wei
  string, and the distinct non-null traders as lowercase hex in first-seen order, at most 32 with
  `traders_truncated` set beyond that, so large-trade and own-trade notifications survive the
  fold), and `launches` / `graduations`: the number of folded `new_token` and
  `graduation` events in the window, omitted when zero. Additive totals, not per token: a client
  refetches the token lists instead.
- `finality`: chain, deployment, `safe_block`, `finalized_block`. Sent to every stream, including
  `?token=` streams.

All amounts are decimal strings, never floats. The `?token=` filter forwards only that token's
`trade` and `graduation` events, plus reorgs. The per-client stream cap and the hub capacity stay
as they are.

### `GET /v1/trades/recent`

Global latest market trades with the same item shape as the `trade` event, plus `as_of_block`
metadata like the other read endpoints. Rate-limited like other public reads. OpenAPI and the
generated web client are updated.

### Metrics

Added to `internal/metrics`: `launchpad_api_feed_events_published_total{type}`,
`launchpad_api_feed_tail_lag_seconds` (now minus the newest published block time) and
`launchpad_api_feed_bursts_total`. No address or hash becomes a label.

## Web

### Live strip (Explore)

- A horizontally scrolling strip above the Explore lists: buy/sell arrow, token image and ticker,
  ETH amount (USD when a fresh reference price exists), shortened trader, relative time. Launch
  and graduation items get a distinct badge. Each item links to the token page.
- Initial items come from `/v1/trades/recent`; live items prepend; at most 50 are kept.
- `prefers-reduced-motion` shows a static, manually scrollable row. Hidden tabs keep the
  connection policy already used by the discovery page.
- Unconfirmed (`observed`) items use the existing pending style until a `finality` event covers
  their block; on `reorg` items above the common ancestor are removed.

### Token page

Recent trades prepends `trade` events from the existing `?token=` stream instead of refetching
on every hint. A reload still yields the same list, because both read `market_trades`.

### In-app notifications

- **Triggers:**
  - a watched token graduates;
  - a trade of at least 0.5 ETH gross on a watched token (constant in code);
  - the user's own trade (connected wallet is the trader) reaches `safe` finality, detected when a
    `finality` event covers the block of a `trade` event whose trader is the connected wallet.
- **Where:** a toast for each new notification, collapsed when several arrive within 5 seconds,
  and a bell menu listing this session's notifications.
- **Storage:** the list lives in memory for the tab; only "last read" is kept in
  `sessionStorage`. No server writes.
- **Who:** watchlist matching needs sign-in (existing Privy session); own-trade matching uses the
  connected wallet. Visitors who are neither signed in nor connected get no notifications and see
  no bell.

### Share card and buttons

- `app/token/[address]/opengraph-image.tsx` (and the Twitter variant) renders a 1200×630 PNG:
  token image, name, `$TICKER`, market cap in ETH (USD when available), bonding progress bar or a
  "Graduated" badge, site mark. `revalidate` is 300 seconds.
- `generateMetadata` on the token route sets title, description and canonical URL.
- Token page actions: "Share on X" (an `x.com/intent/post` link with the token URL and ticker) and
  "Copy link".
- Server-side reads use a new private web setting `API_INTERNAL_BASE_URL` (Compose:
  `http://api:8080`), falling back to `NEXT_PUBLIC_API_BASE_URL`. Fonts are bundled in the
  repository; the renderer makes no third-party request.
- An unknown or invalid token, or any API failure, renders a generic site card with status 200, so
  an unfurl never shows a broken image; the failure is logged.

## Error handling

- Tail query failure: log, count, keep the cursor, retry on the next hint. Clients still receive
  the plain `token` hints, so pages stay correct.
- SSE disconnect: the existing client reconnect logic applies; after a reconnect the strip
  refetches `/v1/trades/recent`, de-duplicating by `(tx_hash, log_index)`.
- Card render failure: generic card as above.

## RISKS

- **User-controlled content in cards.** Token names, symbols and images are user input. The card
  renders text only (no HTML). Images are fetched only from our own API's image route, limited to
  the formats and size the upload path already enforces. The card never fetches a URL taken from
  metadata.
- **Fan-out under load.** A trading burst multiplies events by open streams. The per-process rate
  limit with `trade_burst`, the existing hub buffer and slow-subscriber eviction bound this. The
  plan must include a test that a full subscriber is evicted rather than blocking the tail.
- **Reorg correctness.** A live item can disappear. Rewinding the tail cursor and the client-side
  removal must be tested with the existing reorg fixtures, including a replacement trade in the
  same block range.
- **Notification spam.** The thresholds are fixed constants and toasts are collapsed. No
  per-user settings in this sub-project.
- **Server-side fetch from the web container.** `API_INTERNAL_BASE_URL` must point only at our
  API. The production preflight must reject a non-HTTP(S) value or a missing one when
  `NEXT_PUBLIC_API_BASE_URL` is not reachable from the container.
- **Cache and staleness.** A card can be up to 5 minutes old (more when X caches it), which the
  owner accepted. Cards show no "live" claim.

## BLOCKERS

None. Every input is already in the repository; no external account is needed.

## ACCEPTANCE CRITERIA

1. A buy on Anvil appears in the Explore strip and in the token page's Recent trades without a
   reload, within 3 seconds of the indexer committing it, in the existing Anvil gate.
2. After a reorg that removes a trade, the strip and the token list no longer show it, and a
   replacement trade appears (Go test plus web unit test on the existing reorg fixtures).
3. Over 20 trades per second in one process produce a `trade_burst` event, and a stalled
   subscriber is evicted without delaying other streams (Go test).
4. `GET /v1/trades/recent` returns at most 100 items in canonical order and matches the
   `market_trades` rows (integration test); OpenAPI and the generated client have no drift.
5. Notifications appear for a watched token's graduation, for a trade of 0.5 ETH or more on a
   watched token, and for the user's own trade reaching `safe`, and not for unrelated tokens (web
   unit tests). Visitors neither signed in nor connected see no bell.
6. `GET /token/<address>/opengraph-image` returns a 1200×630 PNG for a known token and the
   generic card for an unknown one. The token page HTML carries `og:image`, `twitter:card` and
   title metadata (Playwright).
7. Reduced-motion and mobile profiles pass the existing browser budgets with the strip present.
8. Existing gates stay green: `task verify`, web gates, the Anvil gate and `deploy/check.ps1`.
