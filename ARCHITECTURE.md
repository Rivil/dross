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

### Code-quality audit (dross-quality)

Calibrate-only, read-only multi-pass code-quality audit: real analyzers plus an adversarial refute-panel over cold subagents, emitting a verified maintainability-risk ledger and scaffolding a remediation phase. The `dross quality` CLI is the deterministic surface (run dirs, analyzer detection, findings→spec scaffold); `quality.md` orchestrates the audit. Sibling of the security audit, diverging on the locked context model (downrank-only, never suppress) and ranking (blast-radius-weighted maintainability-risk).

- `quality.NewRun` — `internal/quality/run.go:65`
- `quality.Catalog` / `quality.Detect` — `internal/quality/catalog.go:107`
- `quality.Ledger` — `internal/quality/findings.go:69`
- `quality.BuildManifest` — `internal/quality/recon.go:112`
- `quality.ScaffoldSpec` — `internal/quality/scaffold.go:15`
- `Quality` (CLI) — `internal/cmd/quality.go:20`

_introduced 06-dross-quality · 715b3f2_

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

Create, list, and complete phases on dedicated phase/<id> git branches; complete is fast-forward + branch-delete only (no commit to main), guarded by origin's `completed <id>` record so it refuses an unmerged phase and mutates nothing, then deletes both the local and the remote phase branch idempotently.

- `Phase` (CLI) — `internal/cmd/phase.go:19`
- `phaseCreate` — `internal/cmd/phase.go:60`
- `phaseComplete` — `internal/cmd/phase.go:144`

_c8b346e · extended 02-harden-ship-merge-complete-flow · extended 03-fix-completion-chore-divergence · 1b883bf_

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

### Security audit (dross-secure)

Context-free, read-only multi-pass security audit: real scanners plus an adversarial refute-panel over cold subagents, emitting a verified findings ledger and scaffolding a remediation phase. The `dross security` CLI is the deterministic surface (run dirs, scanner detection, findings→spec scaffold); `secure.md` orchestrates the audit.

- `security.NewRun` — `internal/security/run.go`
- `security.Catalog` / `security.Detect` — `internal/security/catalog.go`
- `security.Ledger` — `internal/security/findings.go`
- `security.BuildManifest` — `internal/security/recon.go`
- `security.ScaffoldSpec` — `internal/security/scaffold.go`
- `Security` (CLI) — `internal/cmd/security.go:18`

_introduced 05-dross-secure · 37fde7f_

### Ship recovery

Heal origin/main vs local main divergence after a squash-merge.

- `shipRecover` — `internal/cmd/ship_recover.go:30`

_52f6c75_

### Shipping / pull requests

Push the phase branch and open a provider-aware PR (GitHub/Forgejo) with reviewers, merging the phase's landmarks into ARCHITECTURE.md first; folds the completed-state transition (cleared current_phase + `completed <id>` history) into the phase branch and commits it BEFORE the push, so the squash-merge carries the completion record to main and ship returns on a clean tree; squash-merge collapses per-task commits.

- `Ship` (CLI) — `internal/cmd/ship.go:22`
- `ship.OpenPR` — `internal/ship/open.go:38`
- `ship.BuildPRBody` — `internal/ship/body.go:20`

_introduced d392501 · extended 01-architecture-comprehension-layer · extended 02-harden-ship-merge-complete-flow · extended 03-fix-completion-chore-divergence · 77220f5_

### State & status

Track current milestone/phase/version + activity in state.json; summarise "where am I" — including milestone phase-progress (N/M phases verified) and an idle-gated non-spine action surface (security/quality/tech-debt) shown only when the spec→ship spine has nothing runnable left.

- `state.State` — `internal/state/state.go:17`
- `State` (CLI) — `internal/cmd/state.go:16`
- `Status` — `internal/cmd/status.go:18`
- `renderMilestone` — `internal/cmd/status.go:108`
- `spineIdle` — `internal/cmd/status.go:222`
- `renderActionAreas` — `internal/cmd/status.go:312`

_c8b346e · extended 04-status-action-surfaces · 2ee9736_

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
