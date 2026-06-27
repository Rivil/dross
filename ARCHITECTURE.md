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

The analyzer catalog now sources language-dedicated tools from the active stack profile (agnostic tools stay inline); `recon.DetectLanguages` delegates to the single `stack.DetectLanguages`. `BuildManifest` also unions any marker-file stack's analyzers (via `stack.MarkerProfiles`) additively on top of the detected languages, so a marker-only repo (e.g. a Dockerfile) still gets its analyzers (hadolint) atop the agnostic set.

_introduced 06-dross-quality · extended 07-stack-profiles · extended 09-marker-file-detection · 9b6c14d_

### Configuration

Read/write project settings, global defaults, environment variables, and the GSD-seeded profile.

- `Project` — `internal/cmd/project.go:15`
- `Defaults` — `internal/cmd/defaults.go:14`
- `Env` — `internal/cmd/env.go:24`
- `Profile` — `internal/cmd/profile.go:14`

_c8b346e_

### Deferred-item routing

Give every deferred idea a destination instead of leaving it write-only: `/dross-spec` routes each (pull-into-phase / milestone-backlog / named-phase / someday), parked ideas re-surface as candidate criteria when their target phase is scaffolded, and someday items get triaged through `/dross-inbox`. An item lives in one of three states — someday (no target), routed (target set), or dismissed (`dross deferred dismiss`, `--undo` to reverse); a board-less repo still triages its local deferred backlog because `/dross-inbox` §0 skips the board source rather than hard-stopping.

- `Deferred.Target` (schema) — `internal/phase/phase.go:196`
- `Deferred.Dismissed` (dismissed-state flag) — `internal/phase/phase.go:201`
- `Deferred` (dross deferred list/route/dismiss) — `internal/cmd/deferred.go:28`
- `collectDeferred` (scan + filter) — `internal/cmd/deferred.go:40`
- `deferredRoute` (stamp target on disk) — `internal/cmd/deferred.go:155`
- `deferredDismiss` (retire to dismissed, someday-only) — `internal/cmd/deferred.go:194`
- `deferredList --dismissed` (hide/surface dismissed) — `internal/cmd/deferred.go:69`
- dangling-target guard in `Validate` — `internal/cmd/validate.go:117`
- `/dross-inbox` board-off fallback + dismiss funnel — `assets/prompts/inbox.md`

_introduced deferred-item-routing · 6509930 · extended deferred-triage-gaps · 539d475_

### Findings lifecycle

Durable cross-run state for security & quality findings, shared by both audits through one `internal/findings` engine: a stable fingerprint (class/dimension + normalized file path + title, deliberately no line number, so identity survives line drift), a gitignored top-level fingerprint-keyed `state.toml` ledger per tool (tracked/resolved/dismissed + a regressed flag, denormalized display fields so `list` renders after run-dir pruning, atomic temp+rename save), and a strictly post-scan `Reconcile` that folds a fresh run against prior state — a fresh finding matching a dismissed/resolved prior item is folded (not relisted as new), a resolved finding that reappears stays resolved + `regressed=true`, identical fingerprints dedup to one record, and a finding whose file vanished is retained — without ever mutating the scan input, so prior state can't prejudice the runner. Surfaced via a descriptor-driven `dross <tool> findings {list, reconcile <run-dir>, <id> --state tracked|resolved|dismissed}` group wired into both `dross security` and `dross quality` through thin per-tool adapters (security keys the fingerprint on Class, quality on Dimension; each resolves a per-run finding id off its latest run dir). The `secure.md` / `quality.md` §6a step invokes `findings reconcile` after `findings.toml` is written, making cross-run reconciliation part of the audit flow rather than a manual verb.

- `findings.Fingerprint` — `internal/findings/fingerprint.go`
- `findings.Store` / `findings.Record` — `internal/findings/state.go`
- `findings.Reconcile` / `findings.Item` — `internal/findings/reconcile.go`
- `newFindingsCmd` (shared cobra group) — `internal/cmd/findings.go`
- `security.Ledger.Items` / `security.ResolveItem` — `internal/security/lifecycle.go`
- `quality.Ledger.Items` / `quality.ResolveItem` — `internal/quality/lifecycle.go`
- post-scan reconcile step — `assets/prompts/secure.md` / `assets/prompts/quality.md` §6a

_introduced secure-quality-findings-lifecycle · fa06830_

### Greenfield bootstrap

Seed the .dross/ scaffold and an ARCHITECTURE.md skeleton in a new repo, and seed `[runtime]` + `[stack].profile` from the detected stack profile (unsupported stacks are left unseeded, never fabricated).

- `Init` — `internal/cmd/init.go:28`
- `seedRuntimeFromProfile` — `internal/cmd/init.go`
- `project.Project` — `internal/project/project.go:16`

_c8b346e · extended 07-stack-profiles · eb602f1_

### Interaction contract

The propose-and-react contract for interactive commands — a terse builtin rule in every `dross rule show`, the full `_interaction.md` playbook, and a `dross interaction show` emitter that injects the playbook verbatim into interactive prompts (the c-3 pilot disproved nested @-include, so delivery is the CLI emitter), plus a per-decision-point audit checklist. **Every** interactive command is now wired and audited: the five core-loop prompts (plan/execute/verify/ship/review), the seven setup/config prompts (init/onboard/options/rule/inbox/quick/milestone), and the five remaining audit/handoff prompts (architecture/secure/quality/pause/resume) — each restructured to one-decision-per-turn (per-field identity walks, an options section-pick gate, per-criterion milestone scoping, single-gated-turn scaffolds, summary-confirm instead of artifact paste-back), guarded by grep + per-section prompt-sentinel tests. The model is documented as a first-class loop behaviour in the README's `## Interaction` section.

- `Interaction` / `interactionShow` (CLI) — `internal/cmd/interaction.go:10`
- `assets.InteractionPlaybook` (`go:embed _interaction.md`) — `assets/embed.go:17`
- `dross-interaction-contract` builtin — `internal/rules/rules.go:137`
- `_interaction.md` playbook — `assets/prompts/_interaction.md`
- per-decision-point checklist (all commands ✅) — `docs/interaction-audit.md`
- README first-class write-up — `README.md` `## Interaction`
- core-loop wiring + prompt-sentinel guards — `internal/cmd/interaction_coreloop_test.go`
- setup/config wiring + anchor + no-bundle guards — `internal/cmd/interaction_setupcmds_test.go`
- audit/handoff wiring + audit-conformance + README guards — `internal/cmd/interaction_othercmds_test.go`

_introduced 10-interaction-contract · extended 11-retrofit-core-loop · extended 12-retrofit-setup-commands · extended 13-audit-and-readme · e26131b_

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

Create, list, number, migrate, complete, and reorder/insert/rename phases on dedicated phase/<id> git branches. Phase identity is the bare slug and order lives solely in the milestone `phases` array (phase.Ordered), so create makes bare-slug dirs and appends to the array, while `phase number` / status / the version patch digit all read the 1-based array position (DisplayNumber) and `phase migrate` converts a legacy NN-slug repo idempotently — skipping the in-flight phase and disambiguating colliding slugs — with phase.Dir resolving old NN-slug ids for permanent back-compat. complete is fast-forward + branch-delete only (no commit to main), guarded by origin's `completed <id>` record so it refuses an unmerged phase and mutates nothing, then deletes both the local and the remote phase branch idempotently. The lifecycle verbs `insert` / `move` / `rename` edit a phase's array slot and identity through pure splice helpers (InsertRelative / MoveRelative / RenameInArray) and shared plumbing (exactly-one-anchor validation, no-op-before-collision, ship-guard via the origin branch); insert scaffolds with a strict slug (no auto-suffix) and rename moves dir + spec id + array entry + deferred targets + local branch atomically — all leaving every other phase byte-for-byte untouched.

- `Phase` (CLI) — `internal/cmd/phase.go:18`
- `phaseCreate` — `internal/cmd/phase.go:112`
- `phaseNumber` — `internal/cmd/phase.go:33`
- `phaseMigrate` — `internal/cmd/migrate.go:31`
- `phaseComplete` — `internal/cmd/phase.go:209`
- `phaseMove` / `phaseInsert` / `phaseRename` — `internal/cmd/phase_lifecycle.go`
- array-order splice helpers (`InsertRelative`, `MoveRelative`, `RenameInArray`) — `internal/phase/phase.go`
- slug identity helpers (`Dir`, `Ordered`, `DisplayNumber`, `UniqueSlug`) — `internal/phase/phase.go:33`

_c8b346e · extended 02-harden-ship-merge-complete-flow · extended 03-fix-completion-chore-divergence · extended 14-stable-slug-phase-ids · extended phase-lifecycle-commands · ea4db6b_

### Repo onboarding

Scan an existing repo's signal files (Dockerfile, package.json, go.mod, …) into a draft project.toml, seeding `[runtime]` + `[stack].profile` from the matched stack profile.

- `Onboard` — `internal/cmd/onboard.go:26`
- `scanRepo` — `internal/cmd/onboard.go:109`
- `toProject` — `internal/cmd/onboard.go:140`

_c8b346e · extended 07-stack-profiles · eb602f1_

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

The scanner catalog now sources language-dedicated tools from the active stack profile (agnostic tools stay inline); `recon.DetectLanguages` delegates to the single `stack.DetectLanguages`. `BuildManifest` also unions any marker-file stack's scanners (via `stack.MarkerProfiles`) additively on top of the detected languages, so a marker-only repo (e.g. a Dockerfile with no source extension) still gets its scanners.

_introduced 05-dross-secure · extended 07-stack-profiles · extended 09-marker-file-detection · b10f28b_

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

### Stack profiles

Declarative per-stack profiles — embedded built-ins plus `~/.claude/dross/profiles/` drop-ins (user wins on id) — that tune dross to a detected stack: runtime commands, the security/quality tool loadout, and the agent loadout. `dross stack detect/show/list/apply/loadout`; primary detection is signal-scored (exact marker files + source extensions), returning a matched profile id or an `unsupported` sentinel rather than a guess. `apply` re-syncs `[runtime]`; `loadout` emits a markdown block the execute prompt injects inline. Adding a stack is a single TOML drop-in — zero code change. Built-ins ship full profiles (dedicated quality analyzer + runtime + loadout) for Go, Kotlin, Dart, Svelte, SQL, TypeScript, Python, JavaScript, and C#, plus detection-only stubs (id + title + `[signals].exts` only) for Ruby/Rust/Java/C/C++/PHP/Swift. The Svelte, TypeScript, and Dart profiles carry a deepened loadout: dedicated **security scanners** (osv-scanner, plus eslint-plugin-security/retire.js on JS/TS) and **quality analyzers spanning ≥3 substantive dimensions** — dead-code (knip / `dcm unused-code`), coupling (dependency-cruiser), error-handling (typescript-eslint / `dart analyze`) — on top of the existing complexity analyzer, each tool distinctly named so the by-Name manifest dedup keeps every dimension. These surface findings the agnostic scc/jscpd/gitleaks/semgrep/trivy fallback misses (proven end-to-end by the committed `fixtures/multilang-c3` run: knip flags a dead export the fallback is blind to). ext→language is single-sourced from the loaded profiles: `DetectLanguages` derives it by **union** over every profile's `[signals].exts` — a shared extension (e.g. `.ts` in both svelte@6 and typescript@4) yields *both* languages, and the old hardcoded `extLang` map is deleted — so adding a profile extends language detection with no code change. The drop-in keystone is proven end-to-end: a brand-new `.zzz` profile dropped under `~/.claude/dross/profiles/` becomes both detectable and recon-visible with zero Go edit, and a malformed drop-in never crashes detection. Marker-file stacks (e.g. Docker, detected from `Dockerfile`/compose files via case-insensitive `[signals].file_patterns` globs, no source extension) are surfaced *additively* by `MarkerProfiles` on top of the source languages in the secure/quality manifests — leaving primary `Detect` winner-take-all unchanged — so their tools run on repos that have no matching source extension: the docker profile ships hadolint (scanner+analyzer) + trivy config, and the terraform profile ships trivy config (IaC-misconfiguration scanner, named distinctly from the agnostic trivy) + tflint (quality analyzer at the error-handling dimension), detected from `*.tf`/`*.tf.json`/`*.tfvars`/`*.tfvars.json`/`*.hcl` markers (`*.hcl` accepts a known false-positive risk on non-Terraform HCL). This is proven by the committed `fixtures/terraform-c3` run: `trivy config` flags an open-ingress misconfiguration (AVD-AWS-0107) the agnostic scc/jscpd/gitleaks fallback is structurally blind to.

- `stack.Profile` / `stack.Load` — `internal/stack/profile.go:26`
- `stack.Signals.MatchesFile` (case-insensitive glob matcher) — `internal/stack/profile.go:57`
- `stack.Detect` (signal-scored, winner-take-all) / `stack.DetectLanguages` → `extLangFor` + `detectLanguagesFrom` (profile-derived union, no hardcoded map) — `internal/stack/detect.go`
- `stack.MarkerProfiles` (additive pattern-based seam) — `internal/stack/detect.go:186`
- `stack.Embedded` / `stack.LoadAll` / `stack.Merge` — `internal/stack/embed.go`
- embedded profile TOMLs (`go:embed profiles/*.toml`, incl. `docker.toml`, `terraform.toml`) — `internal/stack/profiles/`
- `stack.ResolveRuntime` — `internal/stack/runtime.go`
- `stack.RenderLoadout` — `internal/stack/loadout.go`
- `Stack` (CLI) — `internal/cmd/stack.go`

_introduced 07-stack-profiles · extended 08-language-profiles · extended 09-marker-file-detection · extended multilang-stack-profiles · extended multilang-analyzer-catalogs · extended container-iac-scanning · ab85c9a_

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
