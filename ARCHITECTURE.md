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

The single feature-organized ARCHITECTURE.md — fixed entry template + greenfield skeleton seeding, with backfill, landmark-merge and refresh-merge driven by the dross prompts. Symbol links are kept honest by a codex-backed resolver: `dross doctor` flags stale links advisorily and `dross architecture check --fix` repoints moved ones in place.

- `architecture.EntryTemplate` — `internal/architecture/architecture.go:27`
- `architecture.Skeleton` — `internal/architecture/architecture.go:41`
- `architecture.ParseDoc` / `Resolve` (codex-backed link resolver) — `internal/architecture/links.go:91`
- `codex.SupportsFile` (language-dispatch gate) — `internal/codex/codex.go:106`
- `architectureLinkWarnings` (doctor advisory section) — `internal/cmd/doctor.go:255`
- `Architecture` (`dross architecture check [--fix]`) — `internal/cmd/architecture.go:16`
- `Init` (seeds skeleton) — `internal/cmd/init.go:30`

_introduced 01-architecture-comprehension-layer · extended architecture-doc-enhancements · 89813a3_

### Artefact validation

Schema-check every .dross/ TOML/JSON artefact, including that plan `covers` reference real spec criteria.

- `Validate` — `internal/cmd/validate.go:27`
- `loadIfExists` — `internal/cmd/validate.go:137`

_c8b346e_

### Change tracking & landmarks

Append-only per-task record of files touched, plus a typed `--landmark` record (feature/symbol/loc/what) parsed into a structured `Landmarks` array — replacing the old landmark-carried-in-`--notes` convention.

- `Changes.Record` — `internal/changes/changes.go:133`
- `changes.ParseLandmark` / `Landmark` — `internal/changes/changes.go:62`
- `Changes` (CLI, repeatable `--landmark`) — `internal/cmd/changes.go:15`

_introduced 1d1f85a · extended 01-architecture-comprehension-layer · extended architecture-doc-enhancements · 12513fc_

### Code insight (codex)

Polyglot symbol / cross-file reference / sibling / recent-git insight for given files, rendered for LLM context.

- `codex.Index` — `internal/codex/codex.go:30`
- `findCallers` — `internal/codex/refs.go:25`
- `Codex` (CLI) — `internal/cmd/codex.go:15`

_4b6e027_

### Code-quality audit (dross-quality)

Calibrate-only, read-only multi-pass code-quality audit: real analyzers plus an adversarial refute-panel over cold subagents, emitting a verified maintainability-risk ledger and scaffolding a remediation phase. The `dross quality` CLI is the deterministic surface (run dirs, analyzer detection, findings→spec scaffold); `quality.md` orchestrates the audit. Sibling of the security audit, diverging on the locked context model (downrank-only, never suppress) and ranking (blast-radius-weighted maintainability-risk).

- `quality.NewRun` — `internal/quality/run.go:65`
- `quality.Catalog` / `quality.Detect` — `internal/quality/catalog.go:140`
- `quality.Ledger` — `internal/quality/findings.go:69`
- `quality.BuildManifest` — `internal/quality/recon.go:47`
- `quality.ScaffoldSpec` — `internal/quality/scaffold.go:15`
- `Quality` (CLI) — `internal/cmd/quality.go:21`

The analyzer catalog now sources language-dedicated tools from the active stack profile (agnostic tools stay inline); `recon.DetectLanguages` delegates to the single `stack.DetectLanguages`. `BuildManifest` also unions any marker-file stack's analyzers (via `stack.MarkerProfiles`) additively on top of the detected languages, so a marker-only repo (e.g. a Dockerfile) still gets its analyzers (hadolint) atop the agnostic set. The IaC marker profiles add dedicated quality analyzers — `kube-linter` (kubernetes) and `cfn-lint` (cloudformation) — surfaced at the error-handling dimension on top of (never replacing) the agnostic scc/jscpd, and absent from a marker-less Go repo.

_introduced 06-dross-quality · extended 07-stack-profiles · extended 09-marker-file-detection · extended deepen-container-iac-scanning · cea9254_

### Configuration

Read/write project settings, global defaults, environment variables, and the GSD-seeded profile. Provider recognition lives here: `gitlab.com` autodetects to the `gitlab` provider (deriving `api_base = …/api/v4`), self-hosted hosts stay manual (Provider left empty to prompt), and the GitLab `remote.auth_scheme` (private-token|bearer) + `remote.project_id` override are dotted-config fields. `dross doctor` validates a configured remote — origin/url match, `auth_env` set, and a valid `auth_scheme` — rather than rejecting unknown providers.

- `Project` — `internal/cmd/project.go:15`
- `Defaults` — `internal/cmd/defaults.go:14`
- `Env` — `internal/cmd/env.go:24`
- `Profile` — `internal/cmd/profile.go:14`
- `project.DetectRemote` / `KnownHostProviders` (host→provider autodetect + api_base) — `internal/project/remote.go:24`
- `Doctor` (remote + auth_scheme validation) — `internal/cmd/doctor.go:25`

_c8b346e · extended gitlab-ship-provider · 0f209c9_

### Deferred-item routing

Give every deferred idea a destination instead of leaving it write-only: `/dross-spec` routes each (pull-into-phase / milestone-backlog / named-phase / someday), parked ideas re-surface as candidate criteria when their target phase is scaffolded, and someday items get triaged through `/dross-inbox`. An item lives in one of three states — someday (no target), routed (target set, cleared back to someday with `dross deferred unroute`), or dismissed (`dross deferred dismiss`, `--undo` to reverse); a board-less repo still triages its local deferred backlog because `/dross-inbox` §0 skips the board source rather than hard-stopping.

- `Deferred.Target` (schema) — `internal/phase/phase.go:196`
- `Deferred.Dismissed` (dismissed-state flag) — `internal/phase/phase.go:201`
- `Deferred` (dross deferred list/route/unroute/dismiss) — `internal/cmd/deferred.go:29`
- `collectDeferred` (scan + filter) — `internal/cmd/deferred.go:40`
- `deferredRoute` (stamp target on disk) — `internal/cmd/deferred.go:155`
- `deferredDismiss` (retire to dismissed, someday-only) — `internal/cmd/deferred.go:194`
- `deferredUnroute` (clear target → someday; idempotent, refuses dismissed) — `internal/cmd/deferred.go:248`
- `deferredList --dismissed` (hide/surface dismissed) — `internal/cmd/deferred.go:69`
- dangling-target guard in `Validate` — `internal/cmd/validate.go:117`
- `/dross-inbox` board-off fallback + dismiss funnel — `assets/prompts/inbox.md`

_introduced deferred-item-routing · 6509930 · extended deferred-triage-gaps · 539d475 · extended deferred-unroute-command · fb24bc2_

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

- `Init` — `internal/cmd/init.go:30`
- `seedRuntimeFromProfile` — `internal/cmd/init.go`
- `project.Project` — `internal/project/project.go:16`

_c8b346e · extended 07-stack-profiles · eb602f1_

### Interaction contract

The propose-and-react contract for interactive commands — a terse builtin rule in every `dross rule show`, the full `_interaction.md` playbook, and a `dross interaction show` emitter that injects the playbook verbatim into interactive prompts (the c-3 pilot disproved nested @-include, so delivery is the CLI emitter), plus a per-decision-point audit checklist. **Every** interactive command is now wired and audited: the five core-loop prompts (plan/execute/verify/ship/review), the seven setup/config prompts (init/onboard/options/rule/inbox/quick/milestone), and the five remaining audit/handoff prompts (architecture/secure/quality/pause/resume) — each restructured to one-decision-per-turn (per-field identity walks, an options section-pick gate, per-criterion milestone scoping, single-gated-turn scaffolds, summary-confirm instead of artifact paste-back), guarded by grep + per-section prompt-sentinel tests. The model is documented as a first-class loop behaviour in the README's `## Interaction` section. Coverage is now **fail-closed**: a shared classifier proves every command-backed prompt is either interactive-with-an-audit-section or enrolled in the audit doc's machine-read `## Exempt` list (status, plan-review), failing the build on any unclassified prompt, with `dross doctor` surfacing the same verdict on-demand inside the dross source tree. `/dross-spec`'s §3 takes the contract further: instead of a multiSelect "which gray areas?" pre-selection, it walks **every** area Claude is *genuinely uncertain* about, one at a time, with a user off-ramp — the discriminator is Claude's own uncertainty, not whether the user might have an opinion. Candidate surfacing now shares a **defer-or-add** framing documented once in `_interaction.md`: a borderline/optional candidate is offered as a defer-first either/or ("defer it" leads, "add to current phase" follows), applied in spec `§4a` as a two-step entry-gate-then-destination route that drops the old §4a double-offer, and in plan `§3`/`§4` for borderline task proposals and the coverage-gap check — so spec and plan inherit the convention instead of restating it.

- `Interaction` / `interactionShow` (CLI) — `internal/cmd/interaction.go:10`
- `assets.InteractionPlaybook` (re-derived from `assets.FS`) — `assets/embed.go:26`
- `dross-interaction-contract` builtin — `internal/rules/rules.go:137`
- `_interaction.md` playbook — `assets/prompts/_interaction.md`
- per-decision-point checklist + `## Exempt` list + coverage convention — `docs/interaction-audit.md`
- README first-class write-up — `README.md` `## Interaction`
- `interactionCoverage` (fail-closed classifier + Exempt parser) — `internal/cmd/interaction_coverage.go:37`
- `interactionCoverageWarnings` (dross doctor on-demand lint) — `internal/cmd/doctor.go:278`
- `TestInteractionCoverageFailClosed` (coverage gate + convention guard) — `internal/cmd/interaction_coverage_test.go:15`
- `TestSpecPromptWalksEveryGrayArea` (spec §3 walk-all gray-area guard) — `internal/cmd/spec_prompt_test.go:102`
- `TestInteractionSnippetHasDeferOrAddPattern` (defer-or-add pattern in the playbook) — `internal/cmd/interaction_snippet_test.go`
- `TestSpecPromptTwoStepRouting` (spec §4a two-step defer-first route, no double-offer) — `internal/cmd/spec_prompt_test.go`
- `TestPlanPromptBorderlineTaskDeferFirst` / `TestPlanPromptCoverageGapEitherOr` (plan defer-first + coverage-gap either/or) — `internal/cmd/plan_prompt_test.go`
- core-loop wiring + prompt-sentinel guards — `internal/cmd/interaction_coreloop_test.go`
- setup/config wiring + anchor + no-bundle guards — `internal/cmd/interaction_setupcmds_test.go`
- audit/handoff wiring + audit-conformance + README guards — `internal/cmd/interaction_othercmds_test.go`

_introduced 10-interaction-contract · extended 11-retrofit-core-loop · extended 12-retrofit-setup-commands · extended 13-audit-and-readme · extended retrofit-readmostly-commands · extended gray-area-walkthrough · extended interaction-defer-or-add-framing · d4d05f2_

### Issue board sync

Mirror milestones, phases, quick tasks, and the milestone backlog onto an issue board — driven solely by a dedicated `[board]` config block, independent of `[remote]`, so a repo ships code to one host and tracks issues on another. Backends sit behind a `BoardClient` interface that `forge.NewBoard` dispatches by provider: the provider-aware forge `*Client` (forgejo/gitea/gitlab), a sibling `YouTrackClient` (REST CRUD, bearer permanent-token, readable-id `PROJ-7` addressing, `?fields` projection), a `JiraClient` (Jira Cloud REST v3, HTTP Basic email:token, string `PROJ-123` keys, ADF bodies, transition-driven state, milestones as project versions), or a `GitHubClient` (repo issues with integer milestones — forge-shaped — plus an isolated Projects v2 `addProjectV2ItemById` add-to-board on create when a board is configured). board.json links every artefact by the tracker's readable **string** id. YouTrack adds milestone entities per `[board].milestone_mode` (version bundle / agile board / epic), lifecycle→State mapping via the default map + `[board].state_map` (unmapped warns and skips), and backlog sync of unscaffolded slugs + someday ideas attached per mode (Fix versions / Epic subtask / project-based board). `dross doctor` validates a configured `[board]`; the inbox board source is gated on `[board].enabled`.

- `forge.BoardClient` (interface) + `forge.NewBoard` (provider dispatch) — `internal/forge/forge.go:121`
- `forge.YouTrackClient` + `NewYouTrack` — `internal/forge/youtrack.go:25`
- `YouTrackClient.EnsureMilestoneEntity` / `SetState` — `internal/forge/youtrack.go:184`
- `forge.JiraClient` + `NewJira` (REST v3, versions, transitions) — `internal/forge/jira.go:25`
- `forge.GitHubClient` + `NewGitHubProjects` (repo issues + Projects v2 attach) — `internal/forge/github.go:26`
- `board.Board` (string readable-id link registry) — `internal/board/board.go:29`
- `openBoard` (resolves client solely from `[board]`) / `syncBacklog` — `internal/cmd/issue.go:70`

_a073ab7 · extended gitlab-ship-provider · 27e1a4f · extended youtrack-board-integration · 4bdea81_
_extended additional-board-backends (GitHub Projects + Jira) · 9d60ea2_

### Milestone scoping

Author and validate milestone.toml — title, success criteria, non-goals, phase order.

- `Milestone` (CLI) — `internal/cmd/milestone.go:17`
- `milestone.Milestone` — `internal/milestone/milestone.go:20`

_c8b346e_

### Mutation testing adapters

Language-specific mutation tools normalised to one Report (Stryker for TS/JS/Svelte, Gremlins for Go invoked per-package).

- `Adapter` — `internal/mutation/adapter.go:46`
- `Report` — `internal/mutation/adapter.go:18`
- `Gremlins.Run` — `internal/mutation/gremlins.go:82`
- `Stryker.Run` — `internal/mutation/stryker.go:40`

_introduced c8b346e · extended 01c10f0_

### Phase lifecycle

Create, list, number, migrate, complete, and reorder/insert/rename phases on dedicated phase/<id> git branches. Phase identity is the bare slug and order lives solely in the milestone `phases` array (phase.Ordered), so create makes bare-slug dirs and appends to the array, while `phase number` / status / the version patch digit all read the 1-based array position (DisplayNumber) and `phase migrate` converts a legacy NN-slug repo idempotently — skipping the in-flight phase and disambiguating colliding slugs — with phase.Dir resolving old NN-slug ids for permanent back-compat. complete is fast-forward + branch-delete only (no commit to main), gated by an **authoritative merge check** (`mergeGate`): it reads the phase's recorded PR number from changes.json and requires the provider (`ship.PRMergedFunc`) to report that PR merged, falling back to a `git merge-base --is-ancestor` check that **refuses-when-inconclusive** (a missing/squash-deleted ref or a non-ancestor both refuse) — replacing the old cumulative `completed <id>` breadcrumb, which a later merged phase could drag onto the base and thereby false-complete an unmerged phase; only on a confirmed merge does it delete both the local and the remote phase branch idempotently. The lifecycle verbs `insert` / `move` / `rename` edit a phase's array slot and identity through pure splice helpers (InsertRelative / MoveRelative / RenameInArray) and shared plumbing (exactly-one-anchor validation, no-op-before-collision, ship-guard via the origin branch); insert scaffolds with a strict slug (no auto-suffix) and rename moves dir + spec id + array entry + deferred targets + local branch atomically — all leaving every other phase byte-for-byte untouched.

- `Phase` (CLI) — `internal/cmd/phase.go:18`
- `phaseCreate` — `internal/cmd/phase.go:112`
- `phaseNumber` — `internal/cmd/phase.go:33`
- `phaseMigrate` — `internal/cmd/migrate.go:31`
- `phaseComplete` — `internal/cmd/phase.go:209`
- `mergeGate` (authoritative completion gate: recorded-PR merge status + ancestry refuse-when-inconclusive fallback) — `internal/cmd/phase.go:440`
- `ship.PRMergedFunc` / `ship.PRMerged` (provider PR-merged lookup, GitHub via gh, unsupported-provider sentinel, exported overridable seam) — `internal/ship/merged.go:38`
- `phaseMove` / `phaseInsert` / `phaseRename` — `internal/cmd/phase_lifecycle.go`
- array-order splice helpers (`InsertRelative`, `MoveRelative`, `RenameInArray`) — `internal/phase/phase.go`
- slug identity helpers (`Dir`, `Ordered`, `DisplayNumber`, `UniqueSlug`) — `internal/phase/phase.go:33`

_c8b346e · extended 02-harden-ship-merge-complete-flow · extended 03-fix-completion-chore-divergence · extended 14-stable-slug-phase-ids · extended phase-lifecycle-commands · extended verify-merge-before-completion · 6d99599_

### Repo onboarding

Scan an existing repo's signal files (Dockerfile, package.json, go.mod, …) into a draft project.toml, seeding `[runtime]` + `[stack].profile` from the matched stack profile.

- `Onboard` — `internal/cmd/onboard.go:26`
- `scanRepo` — `internal/cmd/onboard.go:110`
- `toProject` — `internal/cmd/onboard.go:141`

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
- `security.DecideDockle` (three-state image-scan decision: run-supplied / skip-no-image / skip-missing-bin, never builds) — `internal/security/dockle.go:43`
- `securityRun --image` / `resolveImage` (`--image` flag, `$DROSS_IMAGE` fallback) — `internal/cmd/security.go:108`
- `Security` (CLI) — `internal/cmd/security.go:27`

The scanner catalog now sources language-dedicated tools from the active stack profile (agnostic tools stay inline); `recon.DetectLanguages` delegates to the single `stack.DetectLanguages`. `BuildManifest` also unions any marker-file stack's scanners (via `stack.MarkerProfiles`) additively on top of the detected languages, so a marker-only repo (e.g. a Dockerfile with no source extension) still gets its scanners — including the deepened IaC/container loadout (`checkov` cross-family, `dockle` for docker), each surfaced installed-vs-missing. The security surface also covers **container image-layer scanning**: `DecideDockle` is a pure three-state decision that never runs `docker build`, and `dross security run --image <ref>` (or `$DROSS_IMAGE`) feeds it — with no image the run skips-with-reason rather than emitting a silent all-clear.

_introduced 05-dross-secure · extended 07-stack-profiles · extended 09-marker-file-detection · extended deepen-container-iac-scanning · f07fc15_

### Self-update & distribution

Ship dross as a single self-contained binary that carries its own assets and updates itself. The binary embeds every command skill + prompt (`assets.FS`, with the `all:` prefix so the underscore-prefixed `_interaction.md` survives), guarded against drift from the on-disk assets/ tree. `dross install` materializes them into ~/.claude — symlinking assets/ off a source checkout, writing real-file copies from the embedded FS otherwise (`--copy`/`--link` override) — cleanly syncing the dross-* namespace (prune dropped skills *and* prompts, never touch non-dross), with `make install` delegating to it via `--link`. `dross update` fetches the latest GitHub release, then applies a two-stage trust gate before touching any binary: it verifies a **minisign signature** over checksums.txt against a public key embedded in the binary (fail-closed — a missing `checksums.txt.minisig` or a wrong-key/tampered signature refuses the update), and only the signature-verified checksums.txt is then used to verify the platform archive's SHA-256 (still refusing on mismatch). The archive is a `.tar.gz` containing `dross` on darwin/linux and a `.zip` containing `dross.exe` on windows; the updater dispatches on the asset suffix (`AssetName`/`BinaryName`), so one download→verify→extract→swap path serves every platform. It atomically swaps the running binary only when the release is strictly newer (or `--force`; `--check` reports without applying), then re-syncs assets by exec'ing the freshly-swapped binary (never the old in-process engine). The release pipeline signs checksums.txt with minisign in CI (goreleaser `signs` block; the private key is materialized to `$RUNNER_TEMP` from a GitHub secret and the password is piped via stdin), publishing `checksums.txt.minisig` as a release artifact. Install channels: `install.sh` is the `curl | sh` bootstrap (uname-detect, download+verify into a temp dir, mv onto PATH only after the checksum, then `dross install`; shellcheck CI-gated); `install.ps1` is the PowerShell analog for Windows (same verify-before-place safety); and a Homebrew tap (`brew install rivil/dross/dross`) is published by goreleaser's `brews` block. goreleaser builds darwin/linux/windows on amd64+arm64. The README documents all channels + the `dross update` flow.

- `assets.FS` (`go:embed all:commands all:prompts`) — `assets/embed.go:20`
- `update.AssetName` / `update.BinaryName` (per-OS archive + binary name: .zip/dross.exe on windows) / `VerifyChecksum` / `Decide` / `AtomicReplace` — `internal/update/update.go`
- `update.VerifySignature` / `EmbeddedMinisignPublicKey` / `TrustedMinisignKey` (signature trust anchor + override seam) — `internal/update/signature.go:43`
- `update.Client` (latest release + download) — `internal/update/update.go:214`
- `Install` (symlink/copy materialize + dross-* prune) — `internal/cmd/install.go:26`
- `Update` signature gate (verify `checksums.txt.minisig` before checksum/extract/swap) — `internal/cmd/update.go:133`
- `extractBinaryZip` (windows .zip extraction; tar.gz vs zip dispatch on asset suffix) — `internal/cmd/update.go:235`
- release signing + build matrix (`signs:` minisign, windows build, `brews:` tap) — `.goreleaser.yaml` / `.github/workflows/release.yml`
- `install.sh` (curl|sh bootstrap) / `install.ps1` (Windows PowerShell bootstrap, verify-before-place) — `install.sh` / `install.ps1`
- `make install` delegation + shellcheck CI gate — `Makefile` / `.github/workflows/ci.yml`

_introduced self-update-and-distribution · 0ccce6a_
_extended release-trust-and-distribution (minisign signing + verify-before-swap) · 46c091a_
_extended homebrew-and-windows-distribution (windows zip self-update + Homebrew tap + install.ps1) · 0007570_

### Ship recovery

Heal origin/main vs local main divergence after a squash-merge — a shared, delta-gated routine reused by two entry points and documented as a three-state cookbook. `dross ship recover` is the standalone legacy-repo healer; `dross phase complete --recover` heals a diverged main in-loop (the ff-only abort is the divergence signal) and refuses with a pointer when the flag is absent. The shared `runDrossRecovery` resets main to origin, restores the full cumulative `.dross/` tree (every phase's artefacts, not just the current one), and commits only on a real delta — so an in-sync repo is a clean no-op with no phantom commit. The `ship.md` `## Recovery` section maps the three mid-merge failure states (ff-abort / diverged main / dirty post-push tree) each to a one-command fix, with no manual `.dross/` surgery (guarded by a prompt-presence test).

- `runDrossRecovery` (shared delta-gated reset+restore+commit) — `internal/cmd/ship_recover.go:132`
- `shipRecover` (standalone CLI entry, delegates to the shared routine) — `internal/cmd/ship_recover.go:31`
- `phaseComplete` `--recover` (in-loop heal) — `internal/cmd/phase.go:209`

_52f6c75 · extended ship-complete-recovery-hardening · 3a1fd7d_

### Shipping / pull requests

Push the phase branch and open a provider-aware PR/MR (GitHub/Forgejo/GitLab) with reviewers, merging the phase's landmarks into ARCHITECTURE.md first — auto-backfilling the whole doc via the prompt-driven generation when it's absent, so an older repo self-heals on its next interactive ship (non-blocking; `--auto` skips it) — folds the completed-state transition (cleared current_phase + `completed <id>` history) into the phase branch and commits it BEFORE the push, so the squash-merge carries the completion record to main and ship returns on a clean tree; squash-merge collapses per-task commits. The GitLab path is raw REST (no `gh`/`glab` CLI): `openGitLabPR` opens a Merge Request (source/target branch, `Draft:` prefix, `web_url`→URL, `iid`→Number) and resolves reviewer usernames→ids non-fatally; `postGitLabComment` posts an MR note. The post-push PR/MR URL is intentionally printed, not persisted to state.json (avoids the completion-chore divergence); the PR *number*, however, is recorded per-phase in changes.json (`changes.SetPR`), then committed **and pushed** onto the phase branch — drag-proof, unlike cumulative history — so the squash-merge carries the record onto the base's changes.json where `phase complete`'s `mergeGate` reads it to authoritatively confirm the merge (the push is essential: a local-only record never reaches the PR/squash/base, which would leave `mergeGate` blind and refusing every squash-merged completion). The CI-watch + squash-merge steps are prompt-driven (ship.md §5/§6) with the locked GitLab pipeline-status mapping. A non-interactive fast-path makes ship callable from a script or loop: `dross ship --auto` requests zero reviewers for the run without mutating `remote.reviewers` (gating the narration + telemetry off `opts.Reviewers`) and keeps the generated body, while `--json` emits a single `{url, number, result}` object on stdout through a suppressible `narrate` closure — the two compose, and explicit `--body`/`--body-file`/`--draft` still win. `ship.md §0.5` skips the interactive body-preview/body-override/reviewer turns and shells to `dross ship --auto`, opening the PR and returning without driving the merge.

- `Ship` (CLI; `--auto` / `--json` non-interactive flags) — `internal/cmd/ship.go:53`
- `ship.OpenPR` (provider switch → github/forgejo/`openGitLabPR`) — `internal/ship/open.go:41`
- `ship.PostComment` / `postGitLabComment` — `internal/ship/comment.go`
- `buildOpenOpts` / `buildCommentOpts` (thread remote auth_scheme/project_id) — `internal/cmd/ship.go`
- `changes.SetPR` (records opened PR number per-phase for the completion merge-gate) — `internal/changes/changes.go:96`
- `ship.BuildPRBody` — `internal/ship/body.go:20`

_introduced d392501 · extended 01-architecture-comprehension-layer · extended 02-harden-ship-merge-complete-flow · extended 03-fix-completion-chore-divergence · extended gitlab-ship-provider · extended ship-auto-noninteractive · extended verify-merge-before-completion · extended ship-architecture-autogen · extended pr-record-reaches-base · 9e37c37_

### Stack profiles

Declarative per-stack profiles — embedded built-ins plus `~/.claude/dross/profiles/` drop-ins (user wins on id) — that tune dross to a detected stack: runtime commands, the security/quality tool loadout, and the agent loadout. `dross stack detect/show/list/apply/loadout`; primary detection is signal-scored (exact marker files + source extensions), returning a matched profile id or an `unsupported` sentinel rather than a guess. `apply` re-syncs `[runtime]`; `loadout` emits a markdown block the execute prompt injects inline. Adding a stack is a single TOML drop-in — zero code change. Built-ins ship full profiles (dedicated quality analyzer + runtime + loadout) for Go, Kotlin, Dart, Svelte, SQL, TypeScript, Python, JavaScript, and C#, plus detection-only stubs (id + title + `[signals].exts` only) for Ruby/Rust/Java/C/C++/PHP/Swift. The Svelte, TypeScript, and Dart profiles carry a deepened loadout: dedicated **security scanners** (osv-scanner, plus eslint-plugin-security/retire.js on JS/TS) and **quality analyzers spanning ≥3 substantive dimensions** — dead-code (knip / `dcm unused-code`), coupling (dependency-cruiser), error-handling (typescript-eslint / `dart analyze`) — on top of the existing complexity analyzer, each tool distinctly named so the by-Name manifest dedup keeps every dimension. These surface findings the agnostic scc/jscpd/gitleaks/semgrep/trivy fallback misses (proven end-to-end by the committed `fixtures/multilang-c3` run: knip flags a dead export the fallback is blind to). ext→language is single-sourced from the loaded profiles: `DetectLanguages` derives it by **union** over every profile's `[signals].exts` — a shared extension (e.g. `.ts` in both svelte@6 and typescript@4) yields *both* languages, and the old hardcoded `extLang` map is deleted — so adding a profile extends language detection with no code change. The drop-in keystone is proven end-to-end: a brand-new `.zzz` profile dropped under `~/.claude/dross/profiles/` becomes both detectable and recon-visible with zero Go edit, and a malformed drop-in never crashes detection. Marker-file stacks (e.g. Docker, detected from `Dockerfile`/compose files via case-insensitive `[signals].file_patterns` globs, no source extension) are surfaced *additively* by `MarkerProfiles` on top of the source languages in the secure/quality manifests — leaving primary `Detect` winner-take-all unchanged — so their tools run on repos that have no matching source extension: the docker profile ships hadolint (scanner+analyzer) + trivy config, and the terraform profile ships trivy config (IaC-misconfiguration scanner, named distinctly from the agnostic trivy) + tflint (quality analyzer at the error-handling dimension), detected from `*.tf`/`*.tf.json`/`*.tfvars`/`*.tfvars.json`/`*.hcl` markers (`*.hcl` accepts a known false-positive risk on non-Terraform HCL). This is proven by the committed `fixtures/terraform-c3` run: `trivy config` flags an open-ingress misconfiguration (AVD-AWS-0107) the agnostic scc/jscpd/gitleaks fallback is structurally blind to. The container/IaC loadout is then *deepened* with **content-sniff marker detection**: `ContentMatch` adds an optional second gate so a profile globbing the ambiguous `*.yaml`/`*.yml`/`*.json` space confirms a candidate by case-sensitive token match (`All`=AND, `Any`=OR, body read capped at 64 KiB) before surfacing — turning a would-be every-YAML-repo false positive into a near-exact match, while a profile that declares no content keeps the pure-glob fast path. This enables two new marker profiles — `kubernetes` (content `apiVersion`+`kind`) and `cloudformation` (content `AWSTemplateFormatVersion`|`Resources`) — each shipping `trivy config` + `checkov` security scanners and a dedicated quality analyzer (`kube-linter` / `cfn-lint` at the error-handling dimension); `checkov` (cross-family IaC misconfiguration) is added to terraform/k8s/cfn and `dockle` (container image-layer) to docker, each kept distinctly named beside `trivy config` by the by-Name manifest dedup. Proven end-to-end by the committed `fixtures/iac-multi-c5` multi-family run record (a k8s manifest, a CFN template, and a Dockerfile each planting a defect the agnostic fallback misses).

- `stack.Profile` / `stack.Load` — `internal/stack/profile.go:28`
- `stack.ContentMatch` (content-sniff gate, `All`=AND / `Any`=OR, case-sensitive) — `internal/stack/profile.go:57`
- `stack.Signals.MatchesFile` (case-insensitive glob matcher) — `internal/stack/profile.go:103`
- `stack.Detect` (signal-scored, winner-take-all) / `stack.DetectLanguages` → `extLangFor` + `detectLanguagesFrom` (profile-derived union, no hardcoded map) — `internal/stack/detect.go`
- `stack.MarkerProfiles` (additive pattern-based seam; content-gates candidates via `readCapped`/`contentSniffCap`) — `internal/stack/detect.go:218`
- `stack.Embedded` / `stack.LoadAll` / `stack.Merge` — `internal/stack/embed.go`
- embedded profile TOMLs (`go:embed profiles/*.toml`, incl. `docker.toml`, `terraform.toml`, `kubernetes.toml`, `cloudformation.toml`) — `internal/stack/profiles/`
- `stack.ResolveRuntime` — `internal/stack/runtime.go`
- `stack.RenderLoadout` — `internal/stack/loadout.go`
- `Stack` (CLI) — `internal/cmd/stack.go`

_introduced 07-stack-profiles · extended 08-language-profiles · extended 09-marker-file-detection · extended multilang-stack-profiles · extended multilang-analyzer-catalogs · extended container-iac-scanning · extended deepen-container-iac-scanning · 208fec7_

### State & status

Track current milestone/phase/version + activity in state.json; summarise "where am I" — including milestone phase-progress (N/M phases verified) and an idle-gated non-spine action surface (security/quality/tech-debt) that ranks areas by run signal (never-run first, then most-stale) and shows each area's last-run state, surfaced only when the spec→ship spine has nothing runnable left. Status also warns (read-only, never mutating) when sitting on a shipped-but-unmerged `phase/<id>` branch whose branch-local state reads `completed` while origin/main carries no such record — the stale-completion drift the user must reconcile rather than trust; the same drift-case is documented as a reconcile step in `resume.md`.

- `state.State` — `internal/state/state.go:17`
- `State` (CLI) — `internal/cmd/state.go:16`
- `Status` — `internal/cmd/status.go:22`
- `staleCompletedState` (shipped-but-unmerged-branch warning) — `internal/cmd/status.go:462`
- `spineIdle` — `internal/cmd/status.go:262`
- `rankAreas` — `internal/cmd/status.go:378`
- `formatRunSignal` — `internal/cmd/status.go:397`
- `renderActionAreas` — `internal/cmd/status.go:425`

_c8b346e · extended 04-status-action-surfaces · extended status-action-surfaces-v2 · extended ship-complete-recovery-hardening · 2b6d344_

### Status line (native)

Render Claude Code's status line as a native `dross statusline` Go subcommand — a byte-faithful drop-in for the former Node `statusline.js`, with no runtime dependency. A pure `internal/statusline` core renders three lines from an explicit `Inputs`: line 1 `model │ dir ⎇ branch`; line 2 the bold in-progress todo (winning over the dim `.dross` project state) followed by a 10-cell context meter normalized for the auto-compact buffer (green/yellow/orange/blinking-💀 bands); line 3 peer background jobs sorted by attention priority with nerd-font MDI icons. Fidelity is pinned by goldens minted once from the reference node script (the tests never invoke node). A gather layer resolves those inputs from stdin + ~/.claude todos/jobs + a `.dross/state.json` walk-up + git, all behind injected env/clock/git seams so render and golden tests stay hermetic; the command reads stdin bounded (never hangs the prompt) and silent-fails on any parse/FS error. Opt-in wiring (`dross install --statusline` / `dross statusline enable`) JSON-merges ~/.claude/settings.json to invoke the absolute installed-binary path — order-preserving, idempotent, refusing to clobber a foreign statusLine without consent — with a symmetric revert (`--no-statusline` / `dross statusline disable`) that removes only dross's own entry.

- `Render` (three-line pure render core) — `internal/statusline/render.go:75`
- `contextMeter` (auto-compact-normalized 10-cell meter) — `internal/statusline/render.go:194`
- `formatPeers` (priority-sorted peer jobs, MDI icons) — `internal/statusline/render.go:144`
- `Gather` (stdin + todos/state/jobs/git behind injected seams) — `internal/statusline/gather.go:36`
- `Statusline` (bounded-stdin, silent-fail command + enable/disable) — `internal/cmd/statusline.go:31`
- `MergeStatusline` / `RemoveStatusline` (order-preserving settings.json wire/unwire) — `internal/statusline/settings.go:26`
- `enableStatuslineIn` (install --statusline wiring, absolute path, consent-gated) — `internal/cmd/statusline.go:119`

_introduced native-statusline · 46e5025_

### Tech-debt scan (dross techdebt)

Dependency-free, language-agnostic tech-debt scan: TODO/FIXME/HACK/XXX markers (word-boundary) plus size heuristics (oversized files, over-long lines) over git-tracked files, written to a prune-proof run dir with a store-level `last_run` that feeds the status action surface. Distinct from the dross-quality analyzer audit — markers are self-flagged debt, not analyzer findings.

- `Scan` — `internal/techdebt/scan.go:53`
- `NewRun` — `internal/techdebt/run.go:54`
- `StatePath` — `internal/techdebt/state.go:16`
- `Techdebt` (CLI) — `internal/cmd/techdebt.go:22`
- `findings.StampLastRun` — `internal/findings/state.go:121`

_introduced status-action-surfaces-v2 · 510e772_

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
