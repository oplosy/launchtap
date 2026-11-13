# CLAUDE.md

> **Single source of truth.** All agent instructions for this repository live in this file.
> OpenCode reads `CLAUDE.md` when no `AGENTS.md` exists, so OpenCode subagents see the same
> rules. Do not reintroduce a separate `AGENTS.md`.

## Project

- **Name:** launchpad
- **Purpose:** a pons (`pons.family`)-style fixed-supply token launchpad. Bonding
  curve → graduation → Uniswap v2 pool with the initial LP position burned.
  EVM chain (Robinhood Chain).
  Sections: Explore (Graduated + Explore lists), coin detail + trade + chart,
  Forum (Memestock), Analytics (own indexer), Docs. Non-custodial.
- **Detailed feature analysis:** `notes.md`
- **Directory:** `C:\Users\mesut\Desktop\workspace\A-projects\launchpad`

## Language

- **English everywhere:** code, comments, identifiers, docs, commit messages, PR text.

## Setup

```bash
# Contracts (Foundry 1.8.1, solc 0.8.36 pinned in foundry.toml)
cd contracts && forge install

# Backend (Go 1.26.x, tools below run pinned via `go run`, no local install needed)
cd backend && go run github.com/go-task/task/v3/cmd/task@v3.53.1 setup

# Web
cd web && npm ci
```

## Common commands

| Purpose | Command |
|---------|---------|
| Setup   | `cd contracts && forge install` · `cd backend && task setup` · `cd web && npm ci` |
| Run     | Backend binaries read config from the process environment (reference: `backend/.env.example`, not auto-loaded): `cd backend && go run ./cmd/api` · `go run ./cmd/indexer` · `task migrate -- up\|down\|status` · `go run ./cmd/moderate`. Web: `cd web && npm run dev` (port 3417). Full local stack and configuration reference: `README.md`. |
| Test    | `cd contracts && forge test` · `cd backend && task verify` (build, unit/race, integration, lint incl. `gofmt`, migrations up/down/up, sqlc diff, deployment- and curve-vector byte-identical checks — the one reproducible gate, spec §11) |
| Lint    | `cd backend && task lint` (golangci-lint v2.13.2) · `cd backend && task fmt-check` (gofmt) |
| Build   | `cd backend && task build` |

`task` above means `go run github.com/go-task/task/v3/cmd/task@v3.53.1` — every backend tool
(`task`, `golangci-lint`, `sqlc`) is pinned and run via `go run`, never installed globally, so
CI and any developer machine execute the identical pinned version.

## Code standards

- Backend: Go 1.26.x, `internal/curve` and `internal/config` are strict stdlib-only
  (depguard-enforced, see `.golangci.yml`); every on-chain amount is `*big.Int` or Postgres
  `NUMERIC(78,0)` — never a float.
- Contracts: Solidity 0.8.36, checked arithmetic only (`Math.tryAdd`/`tryMul`, no `unchecked`
  outside `ceilDiv`'s already-non-negative subtraction).
- Migrations, sqlc queries/config, and curve vectors are each single-sourced (`backend/internal/store/postgres/migrations`,
  `backend/sqlc.yaml`, `contracts/vectors/v1/`) and copied byte-identical, never hand-edited at
  the copy site — CI fails on drift (`task verify`).

## Don't / watch out

- On Windows, run `git config core.longpaths true` before `git submodule update --init --recursive`:
  `contracts/lib/universal-router` nests submodules past the 260-character path limit, and a
  clone without it fails part-way and leaves every later `git status` erroring. Then run
  `git submodule foreach --recursive git config core.longpaths true` once.
- Don't add a JSON-schema engine (or any non-stdlib import) to `internal/curve` — it's
  depguard-locked to `$gostd` and mirrors on-chain math, where a third-party dependency is the
  wrong trade-off.
- Don't run `forge fmt`/`gofmt` unscoped across the whole repo — an unscoped Foundry
  formatter run once reformatted 183 files inside a vendored submodule by accident.
- Taskfile's workspace-local `GOCACHE` override only helps when the shell has no `GOCACHE` set
  (the realistic "default cache location is inaccessible" case) — it does not override an
  already-exported, broken `GOCACHE` value in the shell.

## Working practice — backlog & limit management

- Every unfinished piece of work is written to `backlog.md` → "Active" section
  (reason: time / limit / scope decision). Template is in the file.
- **When usage limits get close to ~90-92%:**
  1. Do not push a new or half-finished task forward under an insufficient limit.
  2. Write the current state clearly to `backlog.md`: files, exact stopping point,
     next concrete step.
  3. Tell the user: "Limit ~X%, moved the work to the backlog."
- **Note:** the usage-limit percentage cannot be read automatically. Triggers:
  the user warns you, or signs such as the context starting to be summarized.
  If the user states a percentage, act on it.
- **Backlog status must be surfaced, not just written.** Every task
  completion/review report states backlog status explicitly: empty, or the open item
  count plus a one-line list. A non-empty backlog is never passed over in silence — the
  report must say it out loud so it stays prioritized instead of forgotten.

## Workflow

Claude is the only builder. The human is the product owner and final technical authority.
OpenCode free models may be used as subagents for bounded work; they never own a task.

### Per-task rhythm

1. **Pre-flight** (high-risk tasks, see list below). Before writing code, check the task
   spec for missing or contradictory requirements and record locked decisions in
   `docs/specs/` (decision + rationale). Ask the human about real blockers.
2. **Plan.** Short: affected files/modules, main changes, tests needed, migration/contract
   impact.
3. **Implement.**
4. **Verify.** Run the relevant gates (`forge test`, `task verify`, web
   lint/typecheck/test), check acceptance criteria, review `git diff` for stray changes.
5. **Self-review** the diff as an independent reviewer would: incorrect behaviour,
   architecture-boundary violations, security, transaction/data-consistency,
   concurrency/race, blockchain/reorg/indexing edge cases, missing failure handling,
   insufficient tests. For high-risk tasks also run `/code-review` (and `security-audit`
   for security-sensitive surfaces) and fix or explicitly reject each finding.
6. **Commit and report:**
   ```
   Commit: <hash>
   Implemented: ...
   Verified: ...
   Known limitations: ...
   Backlog: ...
   ```

High-risk (full pre-flight + review step): smart contract changes, bonding-curve / fee
math, graduation logic, auth / wallet verification, database schema, indexer / reorg
handling, transaction lifecycle, concurrency, security-sensitive code, public API contract,
large refactors, critical infrastructure. Small, low-risk work (typo, rename, small DTO
change) skips pre-flight.

### OpenCode subagents

- Use only for bounded, well-specified work: wide code searches, test-gap listing,
  mechanical edits, first-pass hunting in an audit. Give each a precise brief with the
  files in scope and what done looks like.
- Never delegate decisions or final verification on high-risk surfaces (see list above).
- Treat a subagent's output as a draft: read its diff, run the gates yourself, and own the
  commit. Subagents do not commit or push.

### Pre-flight documentation depth

Locked pre-flight decisions go into the spec at the level of **decision + rationale**, not
a full mechanical transcription of an algorithm or control flow that already exists (or will
exist) in source — e.g., a step-by-step restatement of a Solidity function's exact
operations. The precise, mechanical detail belongs in the implementation's own doc comment,
naming the source it mirrors (for example `// mirrors CurveMath.quoteBuy`); the spec then
points at that function by name instead of duplicating its body in prose. This keeps one
executable, tested copy of the exact logic instead of two copies that can drift apart.

## Git workflow

- **One repo, one working tree.** No worktrees. Before starting a task, `git pull` and make
  sure `git status` is clean apart from known untracked files.
- **Branches:**
  - `main` — stable, verified state. Only milestone merges land here.
  - `dev` — active development. Docs and small, low-risk changes land here directly as
    linear history.
  - `task/<slug>` — for high-risk or large changes; branched from `dev`, merged back via PR
    once CI is green and the review step passed.
- **Milestones.** Do not merge `dev` → `main` per task. At a milestone: open one PR
  `dev` → `main`, let CI pass, eyeball the diff, merge, optionally `git tag vX.Y.Z`.
  Right after the merge, fast-forward `dev` to `main`'s merge commit
  (`git push origin origin/main:dev`, only when `origin/dev` is an ancestor of `origin/main`;
  never force). Otherwise `dev` never contains that merge commit and the next milestone PR is
  behind `main`, which `main`'s up-to-date rule blocks.
- **Commit messages.** Conventional prefix (`feat: fix: test: chore: docs: refactor:`),
  imperative, one logical change; reference the plan task where relevant (`Plan 1 Task 6`).

### GitHub

- Public repo — origin of record + off-machine backup + CI.
- CI (`backend`, `contracts`, `web`, `deployment-package` workflows) runs on every push to
  `dev`, on PRs into `dev` (a `task/<slug>` branch merging back), and on the milestone PR.
- `main` is branch-protected: `backend`, `web`, `deployment package`, and `Foundry` must pass
  and the branch must be up to date before merge; force pushes and deletion are blocked. No
  required human reviewer, and admins are not enforced. The `release gate` workflow runs on
  milestone PRs that touch code but is not required, because its path filter would leave a
  docs-only PR waiting forever.
- `dev` is branch-protected: `backend`, `web`, `deployment package`, and `Foundry` must pass
  before a PR merges, and repository auto-merge is enabled so a `task/<slug>` PR can merge
  itself once green. Pull-request runs have no path filter so required checks always report.
  Direct pushes to `dev` by repository admins are not blocked (admins are not enforced).
- PRs are used for the `dev` → `main` milestone merge and for `task/<slug>` branches merging
  back into `dev`.
- No GitHub issues / project board — the plan docs are the task list.
