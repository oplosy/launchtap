# AGENTS.md

> **Single source of truth.** All agent instructions live in this file.
> `CLAUDE.md` imports this file via `@AGENTS.md`, so the two are always in sync.
> Make changes **only in this file**.

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
```

## Common commands

| Purpose | Command |
|---------|---------|
| Setup   | `cd contracts && forge install` · `cd backend && task setup` |
| Run     | No API/indexer runtime exists yet (that's Plan 2/3). Today's only executable entrypoint is `cd backend && task migrate -- up\|down\|status`. |
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

- Don't add a JSON-schema engine (or any non-stdlib import) to `internal/curve` — it's
  depguard-locked to `$gostd` and mirrors on-chain math, where a third-party dependency is the
  wrong trade-off.
- Don't run `forge fmt`/`gofmt` unscoped across the whole repo — an unscoped Foundry
  formatter run once reformatted 183 files inside a vendored submodule by accident.
- Don't edit `AGENTS.md` from Codex's side of the workflow; it's Claude's commit surface (see
  "Who commits what" below).
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
  completion/review report (either agent) states backlog status explicitly:
  empty, or the open item count plus a one-line list. A non-empty backlog is
  never passed over in silence — the report must say it out loud so it stays
  prioritized instead of forgotten.

## Multi-agent workflow

Two AI agents may work this repo, in different roles, not simultaneously. This is a
default rhythm, not a rigid gate — scale it to the task.

- **Codex — builder.** Owns the whole implementation lifecycle for a task: the
  implementation plan, the code, migrations/tests/config, running test/lint/build, and
  the commits. Codex owns the repository history for implementation.
- **Claude — independent reviewer / architect / auditor.** Does not take over
  implementation and does not touch implementation history. Value is being a second,
  independent model that catches different bug classes — architecture, security, data
  consistency, concurrency, contract economics, indexer/reorg behaviour, transaction
  lifecycle, test gaps. Claude commits only its own artefacts: `docs/specs/`,
  `docs/plans/`, `notes.md`, `AGENTS.md`, `backlog.md`.
- **Human — product owner and final technical authority.**

Claude is not a manager; Codex is not a reviewer. Codex produces the solution; Claude
independently questions it.

### Per-task rhythm

1. **Claude — pre-flight review** (high-risk tasks). Critiques the task spec; does not
   write code or an implementation plan. Output:
   ```
   BLOCKERS                    - missing/contradictory requirements, decisions needed first
   RISKS                       - security, data-consistency, architecture-boundary concerns
   ACCEPTANCE CRITERIA CHANGES
   ```
   No blockers → Codex starts.
2. **Codex — implementation plan.** Short: affected files/modules, main changes, tests
   needed, migration/contract impact.
3. **Codex — implement.**
4. **Codex — verify.** Run unit/integration tests, lint/typecheck/build; check acceptance
   criteria; review `git diff` for stray changes.
5. **Codex — commit.** Output:
   ```
   Commit: <hash>
   Implemented: ...
   Verified: ...
   Known limitations: ...
   ```
6. **Claude — independent commit review.** Real engineering problems only, not style:
   incorrect behaviour, violated acceptance criteria, architecture-boundary violations,
   security, transaction/data-consistency, concurrency/race, blockchain/reorg/indexing
   edge cases, missing failure handling, insufficient tests. Severity `BLOCKER` /
   `IMPORTANT` / `MINOR`, with exact file/line where possible.
7. **Codex — triage findings.** Each: valid → fix; invalid → reject with rationale. Not
   blind obedience.
8. **Codex — re-verify + follow-up commit.**

### Pre-flight documentation depth

Locked pre-flight decisions go into the spec at the level of **decision + rationale**, not
a full mechanical transcription of an algorithm or control flow that already exists (or will
exist) in source — e.g., a step-by-step restatement of a Solidity function's exact
operations. The precise, mechanical detail belongs in the implementation's own doc comment,
naming the source it mirrors (for example `// mirrors CurveMath.quoteBuy`); the spec then
points at that function by name instead of duplicating its body in prose. This keeps one
executable, tested copy of the exact logic instead of two copies that can drift apart.

### When Claude is in the loop

Optional for small, low-risk work (typo, rename, small DTO change) — Codex proceeds solo.

Near-mandatory on both ends for: smart contract changes, bonding-curve / fee math,
graduation logic, auth / wallet verification, database schema, indexer / reorg handling,
transaction lifecycle, concurrency, security-sensitive code, public API contract, large
refactors, critical infrastructure.

### Claude outside the task lifecycle

Milestone architecture review, threat modeling, contract/economic review, test-gap
analysis, dependency/API research, spec-consistency review, milestone acceptance review,
pre-large-refactor design review, documentation/spec update review.

## Git workflow

- **One repo, one working tree, sequential.** The two agents never run at the same time —
  no per-agent branches, no worktrees.
- **Branches:**
  - `main` — stable, verified state. Only milestone merges land here.
  - `dev` — active development. All task work (Claude docs + Codex implementation +
    review fixes) happens here as linear history.
  - `task/<slug>` — short-lived, **only** for a `high`-risk task whose change is large or
    uncertain; branched from `dev`, merged back to `dev` once its review passes. Not the
    normal path.
- **Handoff rule (the important one).** Every agent handoff happens at a commit boundary:
  1. finish work → commit → `git push`
  2. `git status` must read `nothing to commit, working tree clean`
  3. the next agent starts its turn with `git pull`

  Never hand off with uncommitted changes.
- **Review by hash.** Claude reviews a named commit or range — `review commit d4e5f6` or
  `review commits a1b2c3..e1f2g3` — and states which hash/range it reviewed.
- **Who commits what.** Codex commits implementation (code, tests, migrations, config) on
  `dev`. Claude commits `docs/`, `notes.md`, `AGENTS.md`, `backlog.md` on `dev`. Whoever
  commits, pushes.
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
- PRs are used for the `dev` → `main` milestone merge and, optionally, for a `task/<slug>`
  branch merging back into `dev` so its CI runs before the merge. The routine Claude↔Codex
  loop still happens in `dev` via hash review, not PRs.
- No GitHub issues / project board — the plan docs are the task list.

## Sync rule

- `AGENTS.md` = the only file that holds content.
- `CLAUDE.md` contains only the `@AGENTS.md` line; it holds no content of its own.
- To add/change an instruction: edit **only `AGENTS.md`**. Do not touch `CLAUDE.md`.
