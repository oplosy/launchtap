# Full-Stack Cybersecurity Audit (Offensive & Defensive Analysis)

Date: 2026-11-14
Scope: `contracts/` (V1 and V2), `backend/` (API server, Privy auth, PostgreSQL/sqlc store, indexer, Telegram bot/worker), `web/` (Next.js 16 frontend), `deploy/` (Caddy, Docker Compose)
Mode: Read-only audit report (no source or test files modified)

---

## 1. Executive Summary

This audit evaluated the entire `launchpad` monorepo from both an **offensive (exploit-path, threat-modeling, and boundary-bypass)** and **defensive (remediation and control-verification)** perspective.

- **Smart Contracts (`contracts/src/`):** No critical or high-severity vulnerabilities were identified across V1 (`LaunchFactory`, `BondingCurveV1`, `LaunchToken`) or V2 (`LaunchFactoryV2`, `BondingCurveV2`, `LaunchTokenV2`, `GraduationGuardHook`, `LpFeeLocker`). Reentrancy guards (`nonReentrant`), post-transition accounting invariant assertions (`_assertAccountingInvariant`), checked arithmetic (`Math.tryAdd` / `Math.tryMul`), gas-capped ETH transfers (`50_000` gas) with pull-over-push fallback (`pendingEthRefunds`), pre-graduation transfer blocks (`PreGraduationDexTransfer` for both Uniswap V2 pairs and Uniswap V4 `PoolManager`), and `GraduationGuardHook.beforeInitialize` caller authorization are all properly enforced.
- **Off-Chain Application Stack (`backend/`, `web/`, `deploy/`):** **7 findings** were identified (**2 Medium**, **2 Low–Medium**, **3 Low**). None allow unauthenticated fund theft, SQL injection, or remote code execution; they primarily involve resource-exhaustion (decode bombs, stalled SSE streams, database write churn) and phishing/spoofing surfaces in user-supplied metadata and on-chain strings.

| ID | Severity | Title | Location | Confidence |
|---|---|---|---|---|
| **F-01** | **Medium** | WebP `VP8X` Dimension Validation Bypass and Animated Image (APNG/WebP) Decode-Bomb Risk | `backend/internal/apiserver/metadata_routes.go:322-346`, `backend/internal/apiserver/image_metadata.go:32-185` | High |
| **F-02** | **Medium** | Comment Vote Rate-Limit (`120/hour`) Bypass via In-Place Upsert Toggling and Unthrottled Vote Deletion | `backend/internal/store/postgres/social_store.go:385-411`, `backend/internal/store/postgres/queries/social.sql:211-239` | High |
| **F-03** | **Low–Medium** | Unbounded Write Blocking on Zero-Window / Stalled SSE (`/v1/events`) Connections | `backend/internal/apiserver/server.go:269-278`, `backend/internal/apiserver/event_routes.go:73-192` | High |
| **F-04** | **Low–Medium** | Multi-Line Phishing Link and Unicode Bidi Spoofing in Telegram Bot Alerts via On-Chain Token `name`/`symbol` | `backend/internal/telegram/telegram.go:163-191`, `contracts/src/LaunchFactory.sol:118-120` | High |
| **F-05** | **Low** | Infinite Delivery Retry Loop in Telegram Worker on `400 Bad Request` (`chat not found` / `PEER_ID_INVALID`) | `backend/internal/telegram/worker.go:85-106` | High |
| **F-06** | **Low** | Missing `maxPrivyDIDBytes` and Empty-DID Check in `MetadataRoutes.authenticate` | `backend/internal/apiserver/metadata_routes.go:244-260` | High |
| **F-07** | **Low** | Unrestricted Phishing Hostnames in `x_url` / `telegram_url` and Client–Server URL Validation Mismatch | `backend/internal/apiserver/metadata_routes.go:275-292`, `web/src/transactions.ts:127-138`, `web/src/token/token-panels.tsx:153-154` | High |

---

## 2. Detailed Findings (Offensive Mechanics & Defensive Remediations)

### [F-01] WebP `VP8X` Dimension Validation Bypass and Animated Image (APNG/WebP) Decode-Bomb Risk

- **Severity:** Medium
- **Confidence:** High
- **Evidence:**
  - `backend/internal/apiserver/metadata_routes.go:322-346` (`webpDimensions`)
  - `backend/internal/apiserver/image_metadata.go:32-64` (`stripPNGMetadata`)
  - `backend/internal/apiserver/image_metadata.go:143-185` (`stripWebPMetadata`)
- **Attacker Prerequisites:** Authenticated Privy session with a linked wallet that is the creator of a launched token.
- **Offensive Exploit Path:**
  1. **WebP Inner-Chunk Dimension Bypass:** On `PUT /v1/tokens/{address}/image`, `detectImage` calls `webpDimensions(content)`, which inspects only the first chunk at `content[12:16]`. When the first chunk is `"VP8X"`:
     - `webpDimensions` does not verify that the `VP8X` chunk payload size (`content[16:20]`) equals `10` as required by RFC 9649 §2.2. A truncated `VP8X` chunk causes `content[24:30]` to read bytes from the subsequent chunk header.
     - Even when `VP8X` is 10 bytes and declares a small canvas (e.g., `100x100`), neither `webpDimensions` nor `stripWebPMetadata` inspects the dimensions encoded inside the following `VP8 `, `VP8L`, or `ANMF` bitstream chunks. A crafted WebP can declare `100x100` in `VP8X` while embedding a `16383x16383` `VP8L` bitstream, bypassing `maxImageDimension = 4096`.
     - Furthermore, `stripWebPMetadata` accepts a WebP container that contains only a `VP8X` header and zero image bitstream chunks (`VP8 `, `VP8L`, `ANMF`).
  2. **Animated Image Decode-Bomb (APNG & Animated WebP):** Both `pngRenderingChunks` (`acTL`, `fcTL`, `fdAT`) and `webpRenderingChunks` (`ANIM`, `ANMF`) preserve multi-frame animation chunks without bounding frame count (`num_frames`) or cumulative pixel budget (`width * height * frames`). A highly compressed APNG or animated WebP under `5 MiB` containing thousands of `4096x4096` solid-color frames will pass upload validation and exhaust gigabytes of decoded bitmap memory in victim browsers viewing the token page.
- **Counterevidence:**
  - Upload size is capped at `5 MiB` (`maxImageBytes`), `X-Content-Type-Options: nosniff` is set on image responses, and SVG/HTML uploads are rejected (`415 Unsupported Media Type`).
- **Defensive Remediation:**
  1. In `webpDimensions`, verify `binary.LittleEndian.Uint32(content[16:20]) == 10` for `VP8X`.
  2. In `stripWebPMetadata`, validate the dimensions of every `VP8X`, `VP8 `, `VP8L`, and `ANMF` chunk against `1..maxImageDimension` (`4096`), require at least one image bitstream chunk (`imageChunks > 0`), and cap `ANMF` frames (e.g., `maxAnimationFrames = 100`).
  3. In `stripPNGMetadata`, validate `acTL` frame count (`1..maxAnimationFrames`) and `fcTL` frame dimensions (`1..maxImageDimension`).

---

### [F-02] Comment Vote Rate-Limit (`120/hour`) Bypass via In-Place Upsert Toggling and Unthrottled Vote Deletion

- **Severity:** Medium
- **Confidence:** High
- **Evidence:**
  - `backend/internal/store/postgres/social_store.go:385-411` (`SocialStore.Vote`)
  - `backend/internal/store/postgres/queries/social.sql:211-239` (`UpsertCommentVote`, `DeleteCommentVote`, `VoteRateState`)
- **Attacker Prerequisites:** Authenticated Privy account with a linked wallet holding a positive balance of the comment's token.
- **Offensive Exploit Path:**
  - `SocialStore.Vote` enforces `social.VotesPerHour = 120` by executing `UpsertCommentVote` and then checking `VoteRateState`:
    ```sql
    SELECT count(*)::bigint AS count_in_window, ...
    FROM comment_votes
    WHERE chain_id = $1 AND deployment_id = $2 AND voter_did = $3
      AND updated_at > now() - interval '1 hour';
    ```
  - Because `comment_votes` has `PRIMARY KEY (comment_id, voter_did)`:
    1. **Infinite In-Place Toggling:** Flipping a vote between `+1` and `-1` on 1–120 comments updates `updated_at` in place (`ON CONFLICT (comment_id, voter_did) DO UPDATE`) without ever increasing `count(*)`. Thus `state.CountInWindow > social.VotesPerHour` is never triggered regardless of how many thousands of times per hour the caller toggles their votes.
    2. **Delete-and-Revote Cycling:** Sending `{"value": 0}` executes `DeleteCommentVote` (`DELETE FROM comment_votes`) with no rate-limit check and deletes the row from `comment_votes`. An attacker who has voted on 120 comments can send `value: 0` to clear rows (reducing `count(*)` below 120) and immediately cast new votes on arbitrary additional comments within the same hour, generating sustained PostgreSQL transaction and advisory-lock churn.
- **Counterevidence:**
  - `docs/specs/2026-11-12-comment-votes-design.md:27` and `comment_votes_integration_test.go:114-120` intentionally specify that repeating an existing vote and clearing a vote (`value: 0`) are not rate-limited by the row cap. Global per-IP rate limiting (`10 req/s`, burst `20`) and the token-holder gate (`QualifyWallets`) bound anonymous abuse.
- **Defensive Remediation:**
  - Add an in-memory per-DID `SubjectLimiter` (e.g., 60 requests/minute per `PrivyDID`) in `CommentRoutes.vote` ahead of `r.Store.Vote` to bound per-account database write/lock churn while preserving the store-level `120 active vote rows/hour` invariant.

---

### [F-03] Unbounded Write Blocking on Zero-Window / Stalled SSE (`/v1/events`) Connections

- **Severity:** Low–Medium
- **Confidence:** High
- **Evidence:**
  - `backend/internal/apiserver/server.go:269-278`
  - `backend/internal/apiserver/event_routes.go:73-192` (`EventRoutes.stream`)
- **Attacker Prerequisites:** Network access to `GET /v1/events`.
- **Offensive Exploit Path:**
  - To prevent `net/http`'s server-wide `WriteTimeout` from terminating healthy long-lived SSE streams, `server.go:277` clears the socket write deadline completely (`_ = http.NewResponseController(rw).SetWriteDeadline(time.Time{})`).
  - While `realtime.Hub` evicts slow subscribers when their 64-event Go channel fills, `EventRoutes.stream` writes synchronously to the HTTP response writer via `send(...)`.
  - If a client opens `/v1/events` and advertises a TCP zero window (or stops reading packets without closing the socket), once the kernel TCP send buffer fills, the next `send(...)` call blocks indefinitely inside the socket `Write` syscall. While blocked inside `send(...)`, the handler goroutine cannot reach its `select` loop to observe `<-ctx.Done()`, `<-r.shutdown`, or `subscription.C` closure, pinning the goroutine and `StreamLimiter` slot until OS TCP keepalive/retransmission timeouts expire.
- **Counterevidence:**
  - `StreamLimiter` bounds concurrent streams to `4` per client IP (`DefaultConfig().MaxStreamsPerIP`) and `256` total (`MaxStreamsTotal`), and Caddy sits in front of the API in production.
- **Defensive Remediation:**
  - In `server.go` (`statusWriter`), apply a **per-write** deadline (`SetWriteDeadline(time.Now().Add(cfg.WriteTimeout))`) on each `Write` and `Flush` call for `/v1/events` so idle streams remain open between heartbeats while stalled socket writes time out after `cfg.WriteTimeout` (`10s`).

---

### [F-04] Multi-Line Phishing Link and Unicode Bidi Spoofing in Telegram Bot Alerts via On-Chain Token `name`/`symbol`

- **Severity:** Low–Medium
- **Confidence:** High
- **Evidence:**
  - `backend/internal/telegram/telegram.go:163-191` (`Message`, `truncate`)
  - `contracts/src/LaunchFactory.sol:118-120`
- **Attacker Prerequisites:** Launching a token directly via `LaunchFactory.launch` with crafted UTF-8 characters in `name` or `symbol` and triggering a graduation or large-trade event for a watched token.
- **Offensive Exploit Path:**
  - Although the web launch form restricts `symbol` to `[A-Za-z0-9]{1,16}`, `LaunchFactory.launch` on-chain only checks `bytes(name).length == 0 || bytes(symbol).length == 0`.
  - `telegram.Message` formats plain-text messages (`parse_mode` is unset), but Telegram clients automatically linkify any `https://...` URL in plain text.
  - Unlike `web/src/share/card-data.ts:cleanText` and `backend/internal/social/comment.go:NormalizeBody`, `telegram.go:truncate` only calls `strings.TrimSpace` and truncates to 48 runes without stripping `\n`, `\r`, control characters, or Unicode bidirectional overrides (`U+202A..U+202E`, `U+2066..U+2069`).
  - An on-chain token name such as `"Moon\nClaim airdrop: https://phishing.example"` injects a spoofed line and clickable phishing link directly into official bot alerts.
- **Defensive Remediation:**
  - In `telegram.go:truncate`, strip Unicode bidirectional formatting runes (`U+061C`, `U+200E`, `U+200F`, `U+202A..U+202E`, `U+2066..U+2069`) and collapse control characters (`\r`, `\n`, `\t`, `0x00..0x1F`, `0x7F..0x9F`, `U+2028`, `U+2029`) into single spaces.

---

### [F-05] Infinite Delivery Retry Loop in Telegram Worker on `400 Bad Request`

- **Severity:** Low
- **Confidence:** High
- **Evidence:** `backend/internal/telegram/worker.go:85-106` (`Worker.DeliverOnce`)
- **Mechanics:**
  - `DeliverOnce` unlinks a Telegram chat (`w.Store.UnlinkChat`) only when `apiErr.Code == 403`.
  - When a linked chat is deleted, deactivated, or has an invalid peer ID, the Telegram Bot API returns `400 Bad Request` (e.g., `Bad Request: chat not found` or `PEER_ID_INVALID`). `DeliverOnce` logs a warning and breaks out of the loop without advancing the cursor or unlinking the chat, causing the worker to re-query PostgreSQL and re-send the failing message every 5 seconds indefinitely.
- **Defensive Remediation:**
  - Unlink the chat in `DeliverOnce` when `apiErr.Code == 400` as well as `403`.

---

### [F-06] Missing `maxPrivyDIDBytes` and Empty-DID Check in `MetadataRoutes.authenticate`

- **Severity:** Low
- **Confidence:** High
- **Evidence:**
  - `backend/internal/apiserver/metadata_routes.go:244-260`
  - `backend/internal/apiserver/privy_auth.go:17-23`
- **Mechanics:**
  - Every other authenticated route group uses `authenticatePrivy`, which enforces `principal.PrivyDID == "" || len(principal.PrivyDID) > maxPrivyDIDBytes` (`128` bytes).
  - `MetadataRoutes.authenticate` calls `r.Verifier.Verify` directly and passes `principal.PrivyDID` into `r.Limiter.Allow(principal.PrivyDID, time.Now())` without checking for an empty or oversized DID string.
- **Defensive Remediation:**
  - Enforce `principal.PrivyDID == "" || len(principal.PrivyDID) > maxPrivyDIDBytes` in `MetadataRoutes.authenticate` (or delegate to `authenticatePrivy`).

---

### [F-07] Unrestricted Phishing Hostnames in `x_url` / `telegram_url` and Client–Server URL Validation Mismatch

- **Severity:** Low
- **Confidence:** High
- **Evidence:**
  - `backend/internal/apiserver/metadata_routes.go:275-292` (`validateMetadata`)
  - `web/src/transactions.ts:127-138` (`validateLaunchInput`)
  - `web/src/token/token-panels.tsx:153-154`
- **Mechanics:**
  1. **Trusted-Label Phishing Surface:** `token-panels.tsx:153-154` renders `token.x_url` and `token.telegram_url` under the fixed labels `"X profile"` and `"Telegram"`, but `validateMetadata` and `safeHttps` accept *any* `https://` hostname (e.g., `https://drainer-phishing.example`). A malicious token creator can disguise an external drainer URL behind the `"X profile"` or `"Telegram"` links.
  2. **Client–Server Validation Mismatch:** `validateLaunchInput` in `web/src/transactions.ts:127-138` only checks `new URL(value).protocol !== "https:"`, whereas `validateMetadata` on the backend also rejects URLs exceeding `2048` bytes or containing `userinfo` (`https://user:pass@host`). A creator entering such a URL in the launch form will pay gas to launch the token on-chain and then fail with `422 Unprocessable Entity` when saving metadata.
- **Defensive Remediation:**
  - Restrict `x_url` hostnames to `x.com` / `twitter.com` (and `www.` subdomains) and `telegram_url` hostnames to `t.me` / `telegram.me`.
  - Align `validateLaunchInput` in `web/src/transactions.ts` with `safeHttps` (`<= 2048` UTF-8 bytes, non-empty `hostname`, no `username`/`password`).

---

## 3. Verified Defensive Controls (No Vulnerabilities Found)

- **BondingCurveV1 & BondingCurveV2 Math and Solvency:**
  - All state-changing functions (`buy`, `ExactOutBuy`, `sell`, `claimCreatorFees`, `claimProtocolFees`, `claimEthRefund`) are `nonReentrant` and invoke `_assertAccountingInvariant()` at completion.
  - Final buys that cap at `graduationEth` refund any excess ETH (`refundWei = msg.value - grossEthUsed`) via `_deliverEth`, which caps gas at `50_000` (`ETH_SEND_GAS_LIMIT`) and credits `pendingEthRefunds[recipient]` if the recipient reverts.
- **Uniswap V2 & V4 Graduation Integrity:**
  - `LaunchToken` and `LaunchTokenV2` block pre-graduation transfers (`PreGraduationDexTransfer`) to the Uniswap V2 pair and Uniswap V4 `PoolManager`, preventing third parties from seeding liquidity or skewing pool price before graduation.
  - `GraduationGuardHook` restricts `beforeInitialize` to the canonical `BondingCurveV2` address for that token.
- **Authentication, Moderation, and SQL Layer:**
  - `privyauth.Verifier` restricts JWT signing algorithms to `ES256` and `EdDSA`, enforces `iss`, `aud`, `exp`, and `nbf`, and redacts tokens/DIDs from all error logs.
  - Moderation routes validate bearer tokens using `crypto/subtle.ConstantTimeCompare`.
  - All PostgreSQL queries are parameterized `sqlc` queries; no string-concatenated SQL exists.
- **Web Security Headers, CSP, and SSRF:**
  - `web/proxy.ts` issues a per-request cryptographic CSP nonce (`script-src 'self' 'nonce-...' 'strict-dynamic'`), `frame-ancestors 'none'`, `X-Frame-Options: DENY`, and `X-Content-Type-Options: nosniff`.
  - `web/app/e2e/rpc/route.ts` fails closed (`404`) unless `LAUNCHPAD_E2E_SESSION === "1"` and `NODE_ENV !== "production"`, and restricts upstream targets to loopback (`127.0.0.1` / `localhost`).

---

## 4. Quality Scorecard & Backlog Status

- **Quality Scorecard (`docs/quality-scorecard.md`):** Overall weighted score is **8.6 / 10** (`Security`: **9 / 10**).
- **Active `backlog.md` Status (`backlog.md`):** **5 active items**:
  1. `P1`: Wallet funding provenance and early-buyer cluster signal.
  2. `P1`: Live Robinhood Chain Testnet deployment and verification.
  3. `P1`: Realtime notification and watchlist scale limits (`LISTEN/NOTIFY` fan-out when running multiple API replicas).
  4. `P0-C5 forum deferrals`: Equities ticker tape.
  5. `Engine-2 deferrals`: Multi-hop routing and time-locked `LpFeeLocker` recovery.

---

## 5. Owner Verification (2026-11-16)

Each finding was checked against the source on `dev` (bd2953a) and resolved on `task/audit-2026-11-14-fixes`.

| ID | Verdict | Resolution |
|---|---|---|
| F-01 | Partly valid, Low. Browsers reject a frame larger than the declared canvas (libwebp, APNG), so the 16383x16383 bypass and the "gigabytes per frame" bomb do not hold; the missing VP8X size check, header-only WebP and unbounded frame count are real. | `webpDimensions` requires a 10-byte VP8X; `stripWebPMetadata` requires a picture chunk; APNG `acTL` and `fcTL` and WebP `ANMF` are capped at `maxAnimationFrames = 300`. |
| F-02 | Valid, Low (no score manipulation; write churn only). Delete-and-revote was already accepted by design (run-4 rejected `social-votes:hourly-cap-refunded-by-clear`). | Per-DID cap of 60 vote requests per minute ahead of the store (`CommentRoutes.VoteLimiter`), spec updated. |
| F-03 | Not reproducible. huma's `sse.Sender` sets a per-message write deadline (`sse.WriteTimeout`, 5 s) through `statusWriter.Unwrap`, so a stalled reader is cut off. | Regression test pins the Unwrap chain; the misleading comment in `server.go` is corrected. |
| F-04 | Valid; already fixed in 3655b5e (run-4, `telegram.singleLine`). | None. |
| F-05 | Valid, Low; the proposed fix (unlink on every 400) would unlink live chats on a message-level refusal. | A 400 naming a missing chat unlinks; any other 400 skips that message instead of retrying it forever. |
| F-06 | Consistency only: the verifier already rejects an empty `sub`, and the DID comes from a Privy-signed token. | `MetadataRoutes.authenticate` delegates to `authenticatePrivy`. |
| F-07 | Valid, Low. | `x_url` limited to x.com / twitter.com and `telegram_url` to t.me / telegram.me (no port) in the API, the launch form and the metadata editor; the token page shows a stored link to any other host as unavailable. |

Also inaccurate in the report: the Privy verifier accepts ES256 only (no EdDSA), and the backlog list in section 4 does not match `backlog.md`.
