# Pre-flight: image upload and metadata at launch

Date: 2026-11-13. Source: open item 1 of `docs/audits/2026-11-13-ux-review.md`.

## Problem

The create form collects an image URL, an X link and a Telegram link, validates them, and then
drops them. The `launch` call carries only name, symbol, engine and the developer buy
(`LaunchPanelReady.reviewLaunch`), and nothing is written to the API afterwards. Every launched
token therefore gets generated artwork and no links, whatever the creator typed. There is no
description field at all.

The API already has the verified creator write path:

- `PUT /v1/tokens/{token}/metadata` and `PUT /v1/tokens/{token}/image`, both Privy-authenticated;
- creator-wallet check, `If-Match` revisions, content sniffing, a 5 MiB / 4096 px bound, and
  EXIF stripping (`apiserver.MetadataRoutes`).

`MetadataEditor` on the token page already drives it.

## Locked decisions

1. **Upload after the launch, not before.** The browser keeps the chosen file and the text fields
   until the launch receipt is in. The writes then go through the existing creator endpoints with
   revision 0.
   - *Why:* no new endpoint, storage or abuse surface (a pre-launch staging upload would need
     anonymous blobs, expiry and binding to a future token address). Authorization stays the one
     already reviewed: the server checks the signed-in Privy user owns the creator wallet.
   - *Trade-off:* if the tab closes between receipt and upload, the token keeps generated artwork.
     The creator can still set it from the token page editor, and the success state says so.
2. **The image field becomes a file picker** (PNG, JPEG, WebP; at most 5 MiB). Client checks
   reuse the editor's: declared type, size and magic-byte signature. The server stays the
   authority (dimensions, decode, EXIF). The URL field goes away: the API serves only uploaded
   bytes, so a hotlinked URL could never have been stored.
3. **A description field (at most 2,000 UTF-8 bytes)** joins the links, with the same rules as the
   editor and `validateMetadata`.
4. **Sign-in is asked only when there is something to upload.** With no image, description or
   links, launching works exactly as today. With any of them, the review step needs a session
   (`WatchlistAuthContext`, the same seam the comment composer uses, so the e2e session provider
   covers it). If the reader is signed out, the action reads "Sign in to attach details", and the
   launch itself is never blocked by a failed upload.
5. **Order after the receipt:**
   1. take the token address from the factory's `TokenLaunched` log in the receipt (log address =
      factory, `creator` = the signing account);
   2. wait until `GET /v1/tokens/{address}` returns the token (the indexer must have seen the launch
      before the creator check can pass), polling with a bound;
   3. `PUT` metadata at revision 0;
   4. `PUT` the image at revision 0.

   Each step reports its own failure. A retry keeps the file and text and resumes from the step
   that failed.
6. **`sign` returns the receipt.** The shared pipeline's `sign` now resolves to the successful
   receipt (or `undefined` when the review changed), so launch can read its logs. No other caller
   changes behaviour.

## Not in scope

- Moderation of uploaded images beyond the existing `cmd/moderate` tooling.
- Changing the on-chain launch request: metadata stays off-chain.

## Tests

- Unit: the post-launch sequence as a pure async function with a mocked client.
  - Order: wait for the index, then metadata, then the image.
  - Revision 0 on both writes, and no image write when no file is chosen.
  - Bounded wait on the index.
  - A resumable failure at each step.
  - The `TokenLaunched` log is parsed only from the factory, for the expected creator.
- Unit: form validation (description bytes, file type and size).
- e2e: the fixture session drives a launch with an image and a description, then asserts:
  - the mocked metadata and image writes carry the session headers and revision 0;
  - the token page shows the uploaded artwork.
