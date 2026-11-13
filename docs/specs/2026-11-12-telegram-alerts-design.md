# Telegram alerts

Status: Claude design, 2026-11-12. Closes the notes.md "cheap v1 wins" item "Telegram alerts:
watchlist, graduation, large trades (thresholds, dedup, opt-in)". The owner asked for the deferred
work to be built while away and delegated the rulings. Builds on the P0-C4 inbox
(`docs/specs/2026-11-10-p0c4-notification-inbox-design.md`), which deliberately left out-of-app
delivery for later.

## Goal

A signed-in user can connect a Telegram chat once and then receive a Telegram message for each
inbox event on the tokens they watch: graduations and trades at or above a threshold they choose.

## Non-goals

- Public channels or group chats, per-token settings, digests, quiet hours.
- Alerts for tokens the user does not watch, own-trade or holdings alerts (the inbox has none).
- A Telegram Mini App or Telegram login. Identity stays Privy.

## Decisions

| Decision | Rationale | Rejected alternative |
| --- | --- | --- |
| **Event source = the inbox.** Telegram delivers the P0-C4 `inbox_events` on the account's watched tokens (event at or after the watch started), nothing else. | One definition of "what happened to my tokens" for the page and the bot; no second threshold table. | A separate alert rule engine. |
| **Opt-in by linking.** `POST /v1/me/telegram/link` (Privy auth) returns a deep link `https://t.me/<bot>?start=<code>` valid 15 minutes. The code is 32 random bytes (base64url, 43 chars, the start-parameter alphabet); only its SHA-256 is stored, one pending code per account (a new one replaces it), single use. The user presses Start; the bot binds that private chat to the account. | No chat id is typed by hand, nothing guessable, a leaked old link is dead after 15 minutes or after use. | Asking the user to paste a chat id (spoofable, error-prone); Telegram Login widget (a second identity system). |
| **One chat per account, one account per chat** (per chain and deployment). Re-linking the same account moves it to the new chat and keeps its settings; linking a chat already bound to another account moves the chat. Only `private` chats can link. | Simple ownership: whoever presses Start with a valid code owns that chat's alerts. | Many chats per account (more fan-out, no demand). |
| **Settings** on the link: `graduations` (default on), `large_trades` (default on), `min_trade_wei` (default and minimum 0.5 ETH, the inbox floor; maximum 10,000 ETH). `GET/PUT /v1/me/telegram`, `DELETE /v1/me/telegram` unlinks. The bot's `/stop` also unlinks. | The "thresholds" ask, without per-token rules. | |
| **Only safe blocks are sent**: an event is delivered once its block is at or below the indexed safe head (`indexed_blocks.finality_status` safe or finalized). | A Telegram message cannot be retracted after a reorg; the inbox page can drop an event, the bot cannot. Safe lag on the L2 is seconds. | Sending unsafe events and accepting reorg ghosts. |
| **Dedup = a per-link delivery cursor** `(block, tx_index, log_index)`, set to the safe head when the chat links (no backlog flood) and moved forward after each successful send, in chain order. A crash between send and cursor write can repeat one message; nothing is skipped. | Positions are unique and safe blocks do not reorg, so a forward cursor is exact; same position type as the inbox read marker. | Per-event sent rows (grows forever). |
| **Worker** `cmd/telegram`: one process per deployment, single instance enforced by a PostgreSQL session advisory lock. It long-polls `getUpdates` for `/start`, `/stop` and help, and every 5 s sends pending events, at most 10 per link per pass and about 25 messages per second overall. Telegram `403` (bot blocked) unlinks; `429` waits `retry_after`; other failures keep the cursor and retry next pass. | Long polling needs no public webhook endpoint or secret path; the lock prevents duplicate sends and `getUpdates` conflicts. | A webhook on the API (public surface, secret token, TLS on the API host). |
| **Messages are plain text** (no parse mode, link previews off): kind, symbol and name (truncated, user-controlled), ETH amount, buy/sell and source for trades, and a link to `<TELEGRAM_WEB_ORIGIN>/token/<address>`. | Token names are attacker-controlled; plain text cannot inject markup or links. | Markdown/HTML formatting. |
| **Configuration**: worker needs `TELEGRAM_BOT_TOKEN` (secret, never logged), `TELEGRAM_BOT_USERNAME`, `TELEGRAM_WEB_ORIGIN` (https, or http on localhost); the API needs only `TELEGRAM_BOT_USERNAME` and answers 503 `telegram_unavailable` without it. Chat ids are personal data: never logged. | Feature is off until the owner creates a bot with BotFather. | |
| **Web**: a "Telegram alerts" card on `/notifications`: connect (opens the deep link), status, the two toggles and threshold, disconnect. | Next to the inbox it mirrors. | A separate settings page. |

## Risks

- Bot token leak lets someone send as the bot; it cannot read other chats. Mitigation: secret
  env, redacted from errors, rotate with BotFather.
- Telegram outages delay alerts; the cursor makes delivery resume without loss.
- Read cost: per pass one query per link over watched tokens, bounded by the watchlist cap (200)
  and the 10-event page.

## Acceptance

- Integration: a code links a chat once and expires; re-link moves the chat/account; the worker
  sends only watched, safe, post-link events that pass the settings, in order, once; unlink stops
  delivery; `403` unlinks.
- Unit: code format and hashing, message text (plain, truncated), update parsing (`/start`,
  `/stop`, non-private chats ignored), retry/backoff decisions against a fake Bot API.
- API wire tests: auth, 503 without bot username, settings validation (`invalid_threshold`),
  404 `telegram_not_linked`; OpenAPI and web types regenerated; web unit tests for the settings
  model; e2e of the card's signed-out state.
