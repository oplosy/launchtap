# P0-C4 — Server-side notification inbox

Status: Claude design, 2026-11-10. The product owner chose "server inbox only" (no push, e-mail
or Telegram) and delegated the remaining rulings while away. Builds on P0-B's session
notifications (`docs/specs/2026-11-06-p0b-live-feed-notifications-share-design.md`) and the
P0-A watchlist.

## Goal

A signed-in user sees, on any device and after the tab was closed, what happened to the tokens
they watch: graduations and trades of at least 0.5 ETH, with an unread count.

Success means the inbox is the same on every device for one Privy account, a reorg removes an
event from it without operator action, and marking read moves forward only.

## Non-goals

- Push, e-mail, Telegram or any out-of-app delivery. Per-kind settings or muting.
- Own-trade and holdings notifications (they key on a wallet, not the account; the session
  notifications of P0-B keep covering them).
- Comment or reply notifications (C2 has no replies).

## Decisions

| Decision | Rationale | Rejected alternative |
| --- | --- | --- |
| The inbox is derived at read time: a view `inbox_events` (graduations ∪ market trades with gross ETH ≥ 0.5 ETH) joined with the user's `watchlist_entries`, limited to events at or after the entry's `created_at` and within the last 30 days. | No fan-out worker, cursor or per-user rows to keep consistent; reorgs are handled because the source tables are canonical; it follows watchlist changes instantly. At ≤ 200 watched tokens and the existing per-token indexes the read is bounded. | A fan-out worker writing per-user notification rows (needs a durable cursor, reorg retraction and backfill for new watchers). |
| Only one new table: `notification_reads (chain_id, deployment_id, privy_did, read_block, read_tx_index, read_log_index, updated_at)`. Unread = events after that position. Marking read only moves the position forward (`GREATEST` by row comparison). | Cross-device unread state with one row per user; monotonic so two devices cannot un-read each other. | Per-notification read flags (needs per-user rows). |
| Event id is `"<block>-<tx_index>-<log_index>"`, also the pagination cursor (newest first, strictly older after the cursor). | Stable across reloads and devices; ids of a reorged event simply stop appearing. | Opaque signed cursors (no secret worth protecting here; the id is not user data). |
| `GET /v1/me/notifications?cursor&limit` (limit 1-50, default 20) returns `items`, `next_cursor`, `unread_count` (capped at 100; the web shows "99+" above 99). `PUT /v1/me/notifications/read` with `{"through": "<id>"}` returns 204. Both require the Privy credentials the watchlist uses, `Cache-Control: private, no-store`. Invalid id → 422 `invalid_notification_id`, invalid cursor → 422 `invalid_cursor`. | Same auth, error and cache conventions as `/me/watchlist`. | |
| The 0.5 ETH threshold lives in the view and in `web/src/live/notification-rules.ts` (`LARGE_TRADE_WEI`); a test pins both to 5e17. | One server constant, mirrored for the session toasts. | |
| Web: a `/notifications` page lists the inbox with "Load more" and marks everything read through the newest item once it is shown. The bell panel gains an "Inbox" link with the server unread count, fetched when the panel opens. The bell badge stays the session count. | Keeps P0-B's live toasts untouched and avoids double-counting the same event in two badges. | Merging the inbox into the session list (two sources, one badge, dedupe by id). |

## RISKS

- Read cost grows with watched tokens × trades in 30 days. Bounded by the watchlist cap (200) and
  the per-token order indexes; the query also caps scanned rows by the page limit per source.
- `unread_count` scans at most 100 unread events.
- Events in unsafe blocks show and may disappear after a reorg; acceptable for an inbox (same as
  the trade list).

## ACCEPTANCE CRITERIA

1. Events before the watch started, older than 30 days, under 0.5 ETH, or on unwatched tokens are
   not listed; graduations and large curve and pool trades are, newest first, paginated without
   gaps or duplicates.
2. Another deployment's or chain's watchlist rows never contribute.
3. Marking read through an id sets unread to the events after it; marking an older id afterwards
   does not move it back.
4. A reorg that deletes a listed trade removes it from the next read.
5. 401 without credentials; 422 for a malformed cursor or id; responses are `private, no-store`.
6. Web: `/notifications` lists items as links to the token page, loads more, and the bell
   panel's Inbox link shows the unread count; signed-out readers see a sign-in prompt.
7. Gates: backend unit/race, integration, sqlc/openapi/migration checks; web vitest, lint,
   typecheck, live e2e.
