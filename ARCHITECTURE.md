# Architecture

This document describes what the system *does*, organized by feature — one entry
per user-facing capability, never one per phase and never one per module. Read it
top-to-bottom to learn the capabilities; follow the symbol links to find the code.

Every entry follows one fixed template:

### <Feature name — a user-facing capability, not a module or a phase>

<One line: what this capability does.>

- Symbol.Name — path/to/file.ext:line
- Another.Symbol — path/to/other.ext:line

_introduced <phase-id> · extended <phase-id> · <short-sha>_

Entries are maintained automatically: dross-ship merges each phase's landmarks
into the matching feature entry (updating in place), and /dross-architecture can
regenerate the whole document from a scan of the code and git history.

<!-- entries below, alphabetical by feature -->

### Architecture comprehension

The single feature-organized ARCHITECTURE.md — its fixed entry template and greenfield skeleton seeding; backfill and landmark-merge live in the dross prompts.

- `architecture.EntryTemplate` — `internal/architecture/architecture.go:27`
- `architecture.Skeleton` — `internal/architecture/architecture.go:41`
- `Init` (seeds skeleton) — `internal/cmd/init.go:28`

_introduced 01-architecture-comprehension-layer · 3fdba37_

### Artefact validation

Schema-check every .dross/ TOML/JSON artefact, including that plan `covers` reference real spec criteria.

- `Validate` — `internal/cmd/validate.go:26`
- `loadIfExists` — `internal/cmd/validate.go:111`

_c8b346e_

### Change tracking & landmarks

Append-only per-task record of files touched, plus a feature·symbol·what landmark carried in `--notes`.

- `Changes.Record` — `internal/changes/changes.go:78`
- `Changes` (CLI) — `internal/cmd/changes.go:15`

_introduced 1d1f85a · extended 01-architecture-comprehension-layer · 4f31f70_

### Code insight (codex)

Polyglot symbol / cross-file reference / sibling / recent-git insight for given files, rendered for LLM context.

- `codex.Index` — `internal/codex/codex.go:30`
- `findCallers` — `internal/codex/refs.go:25`
- `Codex` (CLI) — `internal/cmd/codex.go:15`

_4b6e027_

### Configuration

Read/write project settings, global defaults, environment variables, and the GSD-seeded profile.

- `Project` — `internal/cmd/project.go:15`
- `Defaults` — `internal/cmd/defaults.go:14`
- `Env` — `internal/cmd/env.go:24`
- `Profile` — `internal/cmd/profile.go:14`

_c8b346e_

### Greenfield bootstrap

Seed the .dross/ scaffold and an ARCHITECTURE.md skeleton in a new repo.

- `Init` — `internal/cmd/init.go:28`
- `project.Project` — `internal/project/project.go:16`

_c8b346e_

### Issue board sync

Mirror milestones, phases, and quick tasks onto a Forgejo/GitHub issue board (opt-in).

- `Issue` — `internal/cmd/issue.go:35`
- `board.Load` — `internal/board/board.go:53`
- `board.SetPhase` — `internal/board/board.go:109`

_a073ab7_

### Milestone scoping

Author and validate milestone.toml — title, success criteria, non-goals, phase order.

- `Milestone` (CLI) — `internal/cmd/milestone.go:17`
- `milestone.Milestone` — `internal/milestone/milestone.go:20`

_c8b346e_

### Mutation testing adapters

Language-specific mutation tools normalised to one Report (Stryker for TS/JS/Svelte, Gremlins for Go invoked per-package).

- `Adapter` — `internal/mutation/adapter.go:46`
- `Report` — `internal/mutation/adapter.go:18`
- `Gremlins.Run` — `internal/mutation/gremlins.go:57`
- `Stryker.Run` — `internal/mutation/stryker.go:40`

_introduced c8b346e · extended 01c10f0_

### Phase lifecycle

Create, list, and complete phases on dedicated phase/<id> git branches; complete deletes both the local and the remote phase branch, idempotent when the remote is already gone.

- `Phase` (CLI) — `internal/cmd/phase.go:19`
- `phaseCreate` — `internal/cmd/phase.go:60`
- `phaseComplete` — `internal/cmd/phase.go:144`

_c8b346e · extended 02-harden-ship-merge-complete-flow · 3ed94dd_

### Repo onboarding

Scan an existing repo's signal files (Dockerfile, package.json, go.mod, …) into a draft project.toml.

- `Onboard` — `internal/cmd/onboard.go:26`
- `scanRepo` — `internal/cmd/onboard.go:109`
- `toProject` — `internal/cmd/onboard.go:140`

_c8b346e_

### Rules system

Two-tier (builtin + project) MUST-FOLLOW rules, merged and rendered via `dross rule show`.

- `rules.Set` — `internal/rules/rules.go:41`
- `rules.Merge` — `internal/rules/rules.go:82`
- `Rule` (CLI) — `internal/cmd/rule.go:14`

_c8b346e_

### Ship recovery

Heal origin/main vs local main divergence after a squash-merge.

- `shipRecover` — `internal/cmd/ship_recover.go:30`

_52f6c75_

### Shipping / pull requests

Push the phase branch and open a provider-aware PR (GitHub/Forgejo) with reviewers, merging the phase's landmarks into ARCHITECTURE.md first; commits its own post-push state write so ship returns on a clean tree; squash-merge collapses per-task commits.

- `Ship` (CLI) — `internal/cmd/ship.go:22`
- `ship.OpenPR` — `internal/ship/open.go:38`
- `ship.BuildPRBody` — `internal/ship/body.go:20`

_introduced d392501 · extended 01-architecture-comprehension-layer · extended 02-harden-ship-merge-complete-flow · c3b1d50_

### State & status

Track current milestone/phase/version + activity in state.json; summarise "where am I".

- `state.State` — `internal/state/state.go:17`
- `State` (CLI) — `internal/cmd/state.go:16`
- `Status` — `internal/cmd/status.go:18`

_c8b346e_

### Telemetry & stats

Local-only event log (counts / durations / error classes, never file content), queryable via `dross stats`.

- `telemetry.Append` — `internal/telemetry/telemetry.go:82`
- `telemetry.ClassifyError` — `internal/telemetry/telemetry.go:210`
- `RecordCLIEvent` — `internal/cmd/telemetry.go:23`

_a1b9c23_

### Verification

Map acceptance criteria to tests and run mutation testing; decide pass/partial/fail.

- `Verify` (CLI) — `internal/cmd/verify.go:27`
- `verify.Run` — `internal/verify/verify.go:125`

_e31bdbd_
