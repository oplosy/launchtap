# Public API contract

The launchpad API is public and read-mostly. Its contract is an OpenAPI 3.1 document generated
from the route registrations in `backend/internal/apiserver` (`GenerateOpenAPI`).

| File | What it is |
|------|------------|
| `backend/openapi/v1.json` | The current `/v1` contract. Regenerated, never hand-edited. The web client types are generated from it (`web/src/api/generated.ts`). |
| `backend/openapi/v1-baseline.json` | The oldest `/v1` contract a client may rely on. The current contract must stay compatible with it. |

Decision record: `docs/specs/2026-11-14-public-api-contract-preflight.md`.

## Versioning

- Every route is served under `/v1`. `info.version` is `1.x` for as long as `/v1` exists.
- Within `/v1` only backward-compatible changes ship.
- A breaking change gets a new path version: serve `/v2` next to `/v1`, move the web client, and
  remove `/v1` only once no supported client uses it.

## What is breaking

A response may only narrow what clients receive. A request may only widen what the server accepts.

| Where | Breaking | Allowed |
|-------|----------|---------|
| Operations | removed path or method; removed response status or content type; removed request body | new paths, methods, statuses and content types |
| Parameters | removed parameter; parameter becomes required; new required parameter; narrower type or tighter bound | new optional parameters; wider types and looser bounds |
| Response schemas | removed property; required property becomes optional; a type added (for example `null`); changed `format` | new properties (required or optional); narrower types |
| Request schemas | new required property; removed property while unknown properties are rejected; narrower type; changed or added `format`; tighter `maxLength`, `maxItems`, `maximum`, `minLength`, `minItems`, `minimum` or `pattern`; `additionalProperties` turned off | new optional properties; wider types; looser bounds |
| Enums and `oneOf` | a value or variant removed | a value or variant added |

Clients must ignore unknown properties, unknown enum values and unknown SSE event types
(tolerant reader). The web client does: SSE listeners are registered per event name.

Descriptions, examples, tags and `readOnly` flags are never breaking.

## Making a change

All commands run in `backend/`.

1. Change the route or DTO.
2. `go run ./cmd/check-openapi -write` regenerates `openapi/v1.json`. It refuses when the change is
   breaking and lists each breaking change.
3. In `web/`, `npm run web-api-sync` regenerates the client types.
4. `task verify` runs `check-openapi` (stale contract, compatibility, `1.x` version) and the
   compatibility tests in `internal/apicompat` and `internal/apiserver`.

At a milestone release, advance the baseline to the released contract:
`go run ./cmd/check-openapi -accept-baseline`. It writes only a compatible contract, so the
baseline can only grow. Never hand-edit the baseline to remove something; that is a breaking
change and belongs in `/v2`.

The compatibility rules live in `backend/internal/apicompat` (stdlib only, depguard-locked).
