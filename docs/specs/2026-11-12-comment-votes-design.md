# Comment votes and vote-ranked posts

Status: Claude design, 2026-11-12. Closes the "votes" half of the backlog item "P0-C5 forum
deferrals". The owner asked for the deferred work to be built while away; the backlog's open
question ("only holders may vote?") is answered yes. The ticker tape stays deferred (it needs a
paid, licensed equities feed).

## Goal

Readers who hold a token can up- or down-vote its comments; every comment shows its score; the
forum gets a "Best" tab of recent posts ranked by score.

## Non-goals

- Votes from wallets that do not hold the token, reputation weighting, karma, or showing who
  voted.
- Showing the caller's own past votes on a fresh page load (a private per-caller read). The page
  remembers votes cast in the session; repeating a vote is idempotent.
- Votes changing a comment's visibility. Moderation stays the C2 operator path.

## Decisions

| Decision | Rationale | Rejected alternative |
| --- | --- | --- |
| **Holder gate**: the caller may vote on a comment when one of its linked wallets is the creator or holds a positive balance of the comment's token (C2's `QualifyWallets`), checked at vote time. Banned callers get `banned`. No voting on one's own comment. | Sybil cost becomes holding the token in every voting account, the same gate posting has. | Any signed-in account (one Privy account per vote is free to farm). |
| **One vote per account per comment** (`comment_votes`, key `(comment_id, voter_did)`), value +1 or -1; `PUT /v1/comments/{id}/vote` with `{"value": 1 \| -1 \| 0}` sets or (0) clears it and returns the comment's new `score` and the caller's `vote`. Idempotent. | Per account, not per wallet: one person with five wallets is still one vote. PUT matches C2's report endpoint. | Per-wallet votes (multiplies a whale's voice). |
| **Rate limit**: at most 120 vote rows set per account per rolling hour (database clock, C2's per-DID advisory lock and release-time pattern). Clearing is never limited. | Bounds scripted vote churn with the same machinery as comments and reports. | No limit. |
| **Score** = sum of vote values from voters who are not banned, computed at read time; shown as `score` on token comments and forum posts. | A ban removes a vote ring's effect at once without rewriting rows; no counter to keep consistent. | A stored counter column (drifts on bans; needs its own repair). |
| **Forum "Best"**: `GET /v1/forum/posts?sort=best` returns visible posts from the last 7 days by score, then newest; no cursor (one page, limit 1-50). `sort=new` (default) is unchanged. Unknown sort → 400 `invalid_sort`. | Vote-ranked posts without a decay formula to tune; a week keeps old posts from squatting the top. | A Reddit-style hot decay (a knob nobody has data to tune yet). |
| **Web**: an up/down control with the score on every token-page comment; signed-in callers vote and a refusal explains itself ("Hold this token to vote on its comments."). Forum posts show the score as text; voting stays on the token page because a forum post card is one link to its token. Forum tabs: New, Best, Hot, Top. | Same surfaces the reader already uses; no second click target inside the card link. | Vote buttons on forum cards (they would sit inside the stretched card link). |

## Risks

- **Vote rings** of real holders. Mitigation: holder gate, per-account limit, operator bans
  remove their weight at once.
- **Read cost**: score is a grouped sum per listed comment, indexed by `comment_id`; pages are at
  most 50 comments.
- **Holdings change**: a vote stays after the voter sells, like a post does. Accepted.

## Acceptance

- Integration tests: holder votes and changes a vote; a non-holder, the author and a banned
  caller are refused; clearing removes the vote; a banned voter's votes stop counting; scores on
  token comments and forum posts; Best ranks by score within 7 days; the rate limit trips.
- API wire tests for the vote endpoint and problems, the `score` field, and `sort` handling;
  OpenAPI and web types regenerated; web unit tests for the vote control state; forum and token
  e2e with fixture scores.
