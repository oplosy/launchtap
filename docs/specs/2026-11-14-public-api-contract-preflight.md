# Public API contract and compatibility — pre-flight 2026-11-14

Closes the "Backend architecture" Path to 9 item "documented public API contract with versioning
and a compatibility test" (`docs/quality-scorecard.md`).

## Starting point

- The contract is generated from the Huma route registrations (`apiserver.GenerateOpenAPI`) and
  committed as `backend/openapi/v1.json` (OpenAPI 3.1, 39 paths, 83 schemas). `task openapi-diff`
  fails when the committed file is stale, and the web client types are generated from it
  (`npm run web-api-diff`).
- Gap: nothing stops a breaking change. Removing a response field and running
  `check-openapi --write` regenerates the file and every gate passes.

## Decisions

1. **Versioning lives in the path.** Every route is served under `/v1` (`servers: [{url: "/v1"}]`).
   Within `/v1` only backward-compatible changes ship; `info.version` stays `1.x`. A breaking change
   needs a new path version (`/v2`) served next to `/v1` until the web client has moved. The
   check fails if the generated `info.version` major is not `1`.
   *Rationale:* the web app is the only client today, but the API is public and the indexer data
   is useful to third parties; path versioning is visible in logs, caches and the proxy.
   *Alternative:* a version header or media-type versioning — invisible to caches and CDN rules.

2. **A frozen baseline, checked on every build.** `backend/openapi/v1-baseline.json` is the oldest
   `/v1` contract a client may rely on. `check-openapi` (already in `task verify`) now also fails
   when the generated contract is incompatible with the baseline, listing each breaking change.
   `check-openapi --accept-baseline` advances the baseline to the generated contract and refuses
   to when they are incompatible, so advancing can only add. Hand edits to the baseline that remove
   anything are a review failure.
   *Rationale:* a committed file needs no git history in CI (shallow checkouts) and makes the
   promised surface reviewable in a diff.
   *Alternative:* diff against the merge base in git — depends on fetch depth and branch names.

3. **Compatibility rules** (implemented in `internal/apicompat`, stdlib only, depguard-locked).
   Direction matters: a response may only narrow what clients receive, a request may only widen
   what the server accepts.
   - Breaking for any operation: a removed path or method; a removed parameter; a parameter that
     becomes required or a new required parameter; a removed response status or content type;
     a request body that becomes required or is removed.
   - Breaking in a response schema: a removed property; a required property that becomes optional;
     a type added to the allowed set (for example `null`); a changed `format`.
   - Breaking in a request schema (parameters and bodies): a new required property; a removed
     property when unknown properties are rejected (`additionalProperties: false`); a type removed
     from the allowed set; a changed `format`; a tighter `maxLength`, `maxItems`, `maximum`,
     `minimum` or `pattern`; `additionalProperties` turned off.
   - In both directions: an enum value or a `oneOf` variant removed (variants matched by `$ref`,
     `title` or type). Adding an enum value or a variant is allowed: clients must ignore unknown
     enum values and unknown event types (tolerant reader), which the web client already does for
     SSE events.
   - Descriptions, examples, tags, `readOnly` and new optional fields, parameters, paths and
     schemas are not breaking.
   *Rationale:* these are the changes that make an existing client fail to parse or get a new
   `422`. *Alternative:* the `oasdiff` tool — a large new dependency for a 39-path contract;
   the rules above cover every keyword the generated contract uses.

4. **Documentation.** `docs/api/README.md` states the contract, the version policy, the rules
   above, how to add a field and how to make a breaking change.

## Tests

- Unit tests per rule on small hand-written specs (both directions).
- The generated contract is compatible with the committed baseline (`go test` and `check-openapi`).
- Mutation tests on the real generated contract: removing a response property, adding a required
  request property and removing a path are each reported.
