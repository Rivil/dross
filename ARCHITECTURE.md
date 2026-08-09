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

> **On `dross architecture check` residuals.** The link checker resolves the
> text *before* the `—` as a bare top-level Go symbol. Some anchors deliberately
> use a **struct field** (`Deferred.Target`, `LanguageRun.Error`), a **method**
> (`watch.State.Diff`), or a **descriptive label** for a behavior that spans
> several symbols (`verify-gate auto-heal`, `ship PR-base resolution`, `Update
> signature gate`, …). These report as `unresolved`/`ambiguous` even though their
> `file:line` targets are accurate and point at the right code — the checker just
> can't match non-bare-symbol syntax. This residual class is **intentional**;
> what the check must still catch is a *new* break: an anchor whose file no longer
> exists, or a bare symbol that genuinely moved (repoint those with `--fix`).

<!-- entries below, alphabetical by feature -->

### API host allowlist

Every outbound request that would carry the secret named by `[remote].auth_env` is host-checked first, so a committed `api_base` pointing at an attacker's host gets a refusal rather than the token. The allowed set is **derived, never configured** — the host of `[remote].url` plus built-in known-SaaS defaults (`api.github.com`, `gitlab.com`, `*.atlassian.net`, `*.youtrack.cloud`, …) — because a committed key gating a config-derived value is self-authorizing: hostile `.dross/` would simply set both. The zero-value `Policy` resolves to the built-in defaults, so an unset field fails closed instead of reopening the hole. All four forge constructors and all five ship backends check *before* reading the environment, pinned by asserting the error is the refusal and not "$X is not set". A refusal **re-raises rather than degrading**: the merge gate and the milestone dependents check abort with a named error instead of quietly falling back to git ancestry, so an active attack cannot read as a transient forge failure — and the narrowness holds in the other direction too, a genuinely transient error still degrades. A host the derivation can't reach is added through the [machine-local store](#machine-local-store), never through the tracked tree.

- `hostallow.Derive` (remote-URL host + known-SaaS defaults; empty is not allow-all) — `internal/hostallow/hostallow.go:92`
- `hostallow.Policy.Check` (fail-closed check; every refusal wraps `ErrRefused`) — `internal/hostallow/hostallow.go:132`
- `withDefaultPort` (`:80`/`:443` normalised per scheme, so an http entry can't authorize its https twin) — `internal/hostallow/hostallow.go:229`
- `forge.Config.Hosts` (all four forge constructors refuse before the token is read) — `internal/forge/forge.go:76`
- `resolveToken` (single guarded token read shared by the five ship backends) — `internal/ship/hostguard.go:33`
- `TestNoGetenvOutsideHostguard` (AST gate: a new ship backend cannot skip the check) — `internal/ship/hostguard_test.go:137`
- `mergeGate` (host refusal re-raises instead of degrading to git ancestry) — `internal/cmd/phase.go:762`
- `TestDoctorReportsOffAllowlistAPIBase` (doctor names an off-allowlist host before a command refuses mid-run) — `internal/cmd/doctor_test.go:1784`

_introduced config-trust-hardening · 6fef81a_

### Architecture comprehension

The single feature-organized ARCHITECTURE.md — fixed entry template + greenfield skeleton seeding, with backfill, landmark-merge and refresh-merge driven by the dross prompts. Symbol links are kept honest by a codex-backed resolver: `dross doctor` flags stale links advisorily and `dross architecture check --fix` repoints moved ones in place.

- `architecture.EntryTemplate` — `internal/architecture/architecture.go:27`
- `architecture.Skeleton` — `internal/architecture/architecture.go:41`
- `architecture.ParseDoc` / `Resolve` (codex-backed link resolver) — `internal/architecture/links.go:91`
- `codex.SupportsFile` (language-dispatch gate) — `internal/codex/codex.go:106`
- `architectureLinkWarnings` (doctor advisory section) — `internal/cmd/doctor.go:521`
- `Architecture` (`dross architecture check [--fix]`) — `internal/cmd/architecture.go:16`
- `Init` (seeds skeleton) — `internal/cmd/init.go:30`

_introduced 01-architecture-comprehension-layer · extended architecture-doc-enhancements · 89813a3_

### Artefact validation

Schema-check every .dross/ TOML/JSON artefact, including that plan `covers` reference real spec criteria. The two validators divide the work by blast radius (locked `status_check_home`): `dross validate` stays **structural only** because it runs in every slash command's wrap step and must never newly fail an existing repo, while `dross doctor` is the sole **enum-enforcing** validator. Doctor therefore owns the plan-task status check — a task whose `status` falls outside `pending|in_progress|done|failed` is reported as an exit-code issue naming both phase and task id, closing the hole where a hand-edited plan.toml silently dropped a task out of `NextRunnable`. A phase directory with no plan.toml is skipped in silence (spec'd-but-unplanned is legal); an unparseable one is its own issue rather than a clean verdict.

- `Validate` — `internal/cmd/validate.go:27`
- `loadIfExists` — `internal/cmd/validate.go:137`
- `taskStatusIssues` (doctor's plan-task status enum check) — `internal/cmd/doctor.go:566`

_c8b346e · extended cli-surface-sweep · d105fd0_

### Branch topology reporting

Answers "where did this work land, and is that correct-by-design or stuck?" — the doubt a milestone branch creates. `branchTopology` reads HEAD, the work branch and main, plus the work branch's commit distance from main, taking an **authoritative work override** so a caller that already knows the branch (the recorded base) beats an inferred one. `renderTopologyLine` is the single renderer both consumers share. `dross phase complete` states the resulting topology as the run ends: the branch HEAD landed on, what was actually torn down per side — `describeTeardown` never claims a deletion that did not happen, so an interrupted delete reads as origin-only rather than as both — and the `<n> commits on <base>, not yet on main` clause. Because a completion-time message scrolls away, `dross status` prints the same line **unconditionally** — mid-phase, between phases, and with no origin configured — so the answer is standing rather than a moment.

- `branchTopology` (HEAD/work/main + commits-ahead-of-main, authoritative work override) — `internal/cmd/topology.go:47`
- `renderTopologyLine` (the one renderer both consumers share) — `internal/cmd/topology.go:98`
- `describeTeardown` (per-side teardown phrasing; never claims an undone deletion) — `internal/cmd/phase.go:630`
- `TestPhaseCompletePrintsTopologyStatement` (each clause of the completion statement asserted separately) — `internal/cmd/phase_test.go:1799`
- `TestStatusTopologyLineAlways` (unconditional: mid-phase, between phases, no origin) — `internal/cmd/status_test.go:1007`
- `TestBranchTopologyWorkOverrideWins` (recorded base beats an inferable milestone branch) — `internal/cmd/topology_test.go:94`

_introduced completion-state-truth · 1ecde33_

### Branch-switch safety

Every branch switch dross performs runs behind one guard: `guardLiveState` refuses the operation when the target ref *tracks* `.dross/state.json`, naming the file and the literal recovery command, before git can silently overwrite the live machine-local copy. All the primitives sit behind it — `checkoutBranch`, `checkoutBranchNew`, `guardedFF`, `guardedResetHard` — so `phase create`, `phase complete`, `complete --recover`, `ship recover` and `milestone complete --finalize` each leave the accumulated history intact instead of reverting it to whatever a stale branch happened to carry. The guard is dross-side because git does not help here: it declines to overwrite an untracked working-tree file only when that file is *not* ignored, and clobbers an ignored one silently — the exact shape that reverted a 12-entry history to 2. A bare `git checkout` of a legacy branch remains a silent clobber, which is why `dross doctor` reports branches that still track the file (see [Machine-local store](#machine-local-store)).

- `guardLiveState` (refuses a switch to any ref tracking state.json, names the fix) — `internal/cmd/switchbranch.go:109`
- `checkoutBranch` / `checkoutBranchNew` (guarded checkout primitives) — `internal/cmd/switchbranch.go:36`
- `guardedFF` / `guardedResetHard` (guarded fast-forward + hard reset) — `internal/cmd/switchbranch.go:81`
- `milestoneFinalize` (guarded switch back to main, exercised from the milestone branch itself) — `internal/cmd/milestone.go:266`
- `TestHistorySurvivesEveryBranchSwitch` (history intact across every guarded op) — `internal/cmd/state_history_test.go:91`
- `phaseCheckout` (`dross phase checkout <id>`: guarded switch; refuses a missing ref instead of creating it) — `internal/cmd/phase_checkout.go:22`
- `Checkout` (`dross checkout <branch>`: extends the guard to non-phase targets, so `milestone prune`'s refusal stops handing over raw `git checkout`) — `internal/cmd/phase_checkout.go:73`
- `TestNoPromptPerformsRawBranchSwitch` (every prompt scanned for raw phase-branch switches, not just ship.md) — `internal/cmd/ship_prompt_test.go:206`
- `TestShipWrongBranchRefusalNamesGuardedCheckout` (ship's off-branch refusal names the dross verb, covered behaviourally) — `internal/cmd/ship_test.go:238`

The guarded surface reaches the *narration* too, not just the primitives: no prompt and no CLI refusal hands the user a raw `git checkout` of a branch that tracks state.json. The one deliberate exception is `switchbranch.go`'s own escape hatch, printed only after `guardLiveState` has already refused and only alongside the copy/remove dance that makes the switch safe.

_introduced state-json-branch-safety · extended completion-state-truth · 1ecde33_

### Change tracking & landmarks

Append-only per-task record of files touched, plus a typed `--landmark` record (feature/symbol/loc/what) parsed into a structured `Landmarks` array — replacing the old landmark-carried-in-`--notes` convention. Values may contain commas: a comma opens a new pair only at a recognised `key=` boundary (duplicate keys error loudly), and the `--landmark` help text documents the rule.

- `Changes.Record` — `internal/changes/changes.go:203`
- `changes.ParseLandmark` / `Landmark` (comma-in-value join, dup-key error) — `internal/changes/changes.go:72`
- `Changes` (CLI, repeatable `--landmark`) — `internal/cmd/changes.go:15`

_introduced 1d1f85a · extended 01-architecture-comprehension-layer · extended architecture-doc-enhancements · extended landmark-comma-fix · c896695_

### Clean-tree gates

Keep `.dross/` bookkeeping from blocking or diverging the git flow. The dirty-tree gates in ship, phase complete, and phase create/start share one helper that auto-commits `.dross/`-only dirt as a single chore commit (per repo convention) instead of refusing, while a tree with any non-`.dross` dirt still refuses having staged nothing. A ship/complete pre-flight safety net then pushes a base branch that is purely ahead of origin by `.dross/`-only chores (pause snapshots, recovery restores), so after any ship/complete/recover local base == origin/base; a code-ahead base refuses and pushes nothing, a diverged base defers to recovery, and a failed push is a hard refusal (proceeding would re-seed the divergence) — local-only writers like pause never touch the network.

- `autoCommitDrossDirt` (shared gate helper: ship / phase complete / phase create) — `internal/cmd/cleantree.go:20`
- `pushBaseIfAheadDrossOnly` (safety-net push of .dross-only base chores) — `internal/cmd/basebranch.go:62`

_introduced ship-clean-tree · cfa0023_

### CLI error hints

Make a wrong invocation self-correcting: an unknown subcommand or flag fails non-zero naming the invocation that *does* work, instead of a bare "unknown command". Two mechanisms in a fixed order (locked `hint_mechanism`): a **curated table** of known mis-reaches → their working invocation is consulted first, and Levenshtein `Nearest` over the live Cobra command/flag tree is the fallback when the table has no entry. The table exists because `task done` → `dross task status <phase-id> <task-id> done` is a *semantic remap* edit distance can never find; the fallback exists because it self-corrects plain typos the table never anticipated (`stauts` → `status`, `--titel` → `--title`), including ones cobra's own `SuggestionsMinimumDistance` gives up on. Flag errors are decorated through a root `FlagErrorFunc` walked up from the failing command — flag-parse failures never reach `RunE`, so a hint hung off the command body would never fire — and the decoration *appends* to cobra's message rather than replacing it, preserving the substrings `telemetry.ClassifyError` buckets on so a hinted error still classifies as `unknown_flag` instead of silently demoting to `other`. Two guards keep the layer honest against the tree it describes: every curated key, replacement command path and named flag must resolve in the real assembled `newRoot()`, and a corpus check over `assets/prompts/*.md` fails any prompt that teaches a known-broken invocation.

- `CuratedHint` (curated mis-reach → working-invocation table) — `internal/cmd/hints.go:62`
- `Nearest` (Levenshtein fallback over the live command/flag tree) — `internal/cmd/hints.go:76`
- `EnforceSubcommandKnown` (unknown-subcommand guard, non-zero exit) — `internal/cmd/subcommand_guard.go:17`
- `decorateFlagError` / `InstallFlagHints` (root FlagErrorFunc parent-walk, telemetry-preserving) — `internal/cmd/flag_hint.go:30`
- `LineTeachesMisreach` (prompt-corpus guard against broken invocations) — `internal/cmd/hints.go:145`
- `TestMisreachesAreSelfCorrecting` (c-2 executed against the assembled tree) — `cmd/dross/main_test.go:314`

_introduced cli-surface-sweep · ad037f3_

### CLI output formats

Make every structured `show` machine-readable on the same terms: `project`, `milestone`, `phase`, `task`, `changes`, `stats`, `stack`, `defaults` and `profile` all accept `--json` and emit the same data their default rendering shows. The payload is the **bare document** with no envelope and no `# <path>` header line (locked `json_shape`) — the config-document shows encode one decoded value twice, `emitJSON(x)` under the flag and the TOML encoder otherwise, so the two renderings cannot disagree. `phase show --json` emits a `spec` and a `plan` key (null for a missing file) and an unknown id now errors instead of returning an empty document; stats aggregation is extracted so the table and the JSON read one struct; `task show --json` normalises `Status` through `orPending` so the payload matches the aligned rendering. A tree walk over the assembled `newRoot()` requires the flag on every `show` outside a two-entry exempt list, and checks it is *wired* rather than merely registered.

- `emitJSON` (bare-document emitter behind `--json`) — `internal/cmd/jsonout.go:20`
- `summarizeStats` (one aggregation struct behind both table and JSON) — `internal/cmd/stats.go:133`
- `taskShow` (`--json` status normalised through `orPending`) — `internal/cmd/task.go:130`
- `TestEveryStructuredShowAcceptsJSON` (tree walk + wired-not-just-registered check) — `cmd/dross/main_test.go:450`

_introduced board-state-map-truth · 3272339_

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

### Config-derived git argument safety

A value read out of `.dross/project.toml` is untrusted input to git, not a trusted argument. Two layers hold. Config-derived **ref names** are validated before any git process starts: a leading `-`, or anything `git check-ref-format` rejects, is refused with a named error at `phase complete`, `phase checkout`, `milestone create` and `ship recover`. And every git invocation that passes a config- or user-derived **positional** builds its argv through separator-carrying builders — `--end-of-options` for refs, `--` for pathspecs, deliberately distinct (locked `ref_separator_token`: a `--` in a ref position makes git reinterpret the branch as a pathspec, which is a different bug, not a fix). That second layer closed a real arbitrary-file-write, where `dross repair`'s `git log` took an attacker-supplied `--output=` straight out of config. Coverage is structural rather than by inspection: a repo-wide AST audit fails by `file:line` on any positional that is neither a literal nor a prefix-constant, and the audit is itself self-checked against a FLAG/PASS snippet table so it cannot pass vacuously. That audit covers **every** binary dross spawns, not only git (locked `audit_gate_breadth`): it resolves each spawn site to a binary, looks the binary up in `internal/argfence`'s policy table, and applies whichever defence that tool can actually offer — an end-of-options token for git / `gh` / ast-grep / semgrep, outright rejection of a leading-dash value for gremlins / npx / dotnet, which have none. A binary with no table entry is a finding, so a tool nobody anticipated is in scope the day it is added, and a flag demoted *past* a separator is a finding too — in cobra `--` ends flag parsing rather than fencing one token, so a demoted flag silently becomes a positional. The `git >= 2.24` floor `--end-of-options` implies is executed, not assumed.

- `validateGitRef` (pre-exec ref guard behind the four guarded switch helpers) — `internal/cmd/refguard.go:32`
- `gitRefArgs` / `gitRefPathArgs` (separator-carrying argv builders) — `internal/cmd/gitargs.go:45`
- `TestPhaseCompleteRefusesDashMainBranch` (one entrypoint test per guarded command, refusal asserted to precede the first exec) — `internal/cmd/refguard_entrypoints_test.go:79`
- `historyFromPhaseCommits` (repair's `git log` fenced — the arbitrary-file-write site) — `internal/cmd/repair_state.go:78`
- `auditFile` (AST gate flagging unseparated positionals for EVERY spawned binary by file:line, per-tool policy read from `argfence`) — `internal/cmd/subprocargs_audit_test.go:129`
- `TestNoUnseparatedPositional` (the repo-wide run of that gate, all binaries) — `internal/cmd/subprocargs_audit_test.go:346`
- `TestNoUnseparatedGitPositional` (the original git-only guarantee, kept as its own test after the generalisation) — `internal/cmd/subprocargs_audit_test.go:359`
- `TestHostileConfigVectors` (12-vector hostile-`.dross/` suite off a pinned refusal contract, with an observed red replay) — `internal/cmd/hostile_config_test.go:302`
- hostile-config fixture (pinned refusal contract + payloads) — `fixtures/hostile-config-c5/expected-refusals.txt:1`

_introduced config-trust-hardening · 370c697_

### Configuration

Read/write project settings, global defaults, environment variables, and the GSD-seeded profile. Provider recognition lives here: `gitlab.com` autodetects to the `gitlab` provider (deriving `api_base = …/api/v4`), self-hosted hosts stay manual (Provider left empty to prompt), and the GitLab `remote.auth_scheme` (private-token|bearer|basic) + `remote.project_id` override are dotted-config fields; `basic` pairs with `[remote].auth_user` for Bitbucket-style user:token credentials. Every enumerated config value — board provider, ship provider, `auth_scheme`, `milestone_mode` — has a single home in `configenum`, whose `Set` normalises (trim + lowercase) and carries a per-field empty-value policy; doctor, `dross issue enable`, the forge/ship dispatch and the milestone-mode consumers all resolve through it instead of carrying their own literal lists. `dross doctor` therefore accepts every value the CLI can actually dispatch (jira and github boards included, with `board.base_url` optional for github but still parsed when set), normalises the way the consumer does before validating, and reports runtime-fatal pairings — jira+epic, `basic` without `auth_user`, a remote provider ship cannot dispatch — as advisory warnings on their own counter that leave exit 0. `go/ast` guards fail the build when a dispatch switch, the remote-writer map or a prompt's provider bullet diverges from its `configenum` set. Reads and writes are complete and reversible across all three surfaces. **Reads:** `project get`, `milestone get` and `state get` each take one *or more* dotted paths through a shared renderer — a single path prints its bare value byte-identically to before (locked `multi_get_shape`, so no existing prompt or script migrates), two or more emit one keyed JSON object in *argument* order rather than map order, and an unknown path among several writes nothing at all rather than a partial object; `milestone get`'s optional leading version is matched by **shape**, so a typo'd first path is named as an unknown path instead of being swallowed as "no such milestone". **Writes:** every `[board]` field the code reads is settable by dotted path — including `board.github_project` and per-key `board.state_map.<status>` entries, each addressed as its own leaf so a write never clobbers unlisted entries (locked `state_map_write`) — and `project set --unset <path>` clears a scalar or a single map entry without hand-editing TOML. `milestone set` resolves an unambiguous bare field name to its dotted path (ambiguous names are rejected listing every candidate, never resolved by first match) and rejects a status outside `configenum.MilestoneStatuses` *before* Save, leaving the file byte-unchanged. A `board.state_map.<key>` write is gated the same way against `configenum.LifecycleStatuses` and normalised, so a refused key leaves project.toml byte-identical, a case near-miss still lands, and set/get/`--unset` all address the one entry — with `dross doctor` catching a bad key already on disk as an exit-code **issue**, not a warning (locked `state_map_key_severity`: a dead override key is silently-broken config, since the remap it declares never applies). Every `toml`-tagged schema field also carries an identical `json` tag, enforced by a transitive walk over the eight document roots so the `--json` surface can't diverge from the on-disk one. Config that is *hostile or simply broken* is diagnosable before a command refuses mid-run: doctor reports a branch name git would reject, an `api_base` outside the [host allowlist](#api-host-allowlist), a git-tracked `.dross/local.toml`, and a pre-2.24 git as named exit-code findings, each naming the exact fix.

- `Project` — `internal/cmd/project.go:16`
- `Defaults` — `internal/cmd/defaults.go:14`
- `Env` — `internal/cmd/env.go:24`
- `Profile` — `internal/cmd/profile.go:14`
- `project.DetectRemote` / `KnownHostProviders` (host→provider autodetect + api_base) — `internal/project/remote.go:24`
- `Doctor` (remote + auth_scheme validation) — `internal/cmd/doctor.go:32`
- `configenum.Set` (normalising enum home + per-field empty-value policy) — `internal/configenum/configenum.go:32`
- doctor enum validation via `configenum` Sets — `internal/cmd/doctor.go:117`
- `remoteCombinationWarnings` (runtime-fatal pairings as advisory warnings) — `internal/cmd/doctor.go:452`
- `Remote.AuthUser` (`[remote].auth_user` schema + dotted read/write) — `internal/project/project.go:103`
- `forge.Client.do` (basic scheme + construction-time auth_user check) — `internal/forge/forge.go:628`
- `providerSwitchIn` (go/ast validator↔dispatch divergence guard) — `internal/cmd/enum_divergence_test.go:56`
- `TestPromptProviderListsMatchShipProviders` (init/onboard provider bullets pinned to ShipProviders) — `internal/cmd/prompt_provider_list_test.go:73`
- `renderMultiGet` (shared 1+-path get renderer: bare value or keyed JSON in argument order) — `internal/cmd/dotget.go:24`
- `looksLikeMilestoneVersion` (shape-matched leading version, so a typo'd path is named) — `internal/cmd/milestone.go:635`
- `unsetDotted` (`project set --unset`: clear a scalar or one `board.state_map` entry) — `internal/cmd/project.go:489`
- `resolveBareMilestoneField` (unambiguous bare name → dotted path, ambiguity rejected) — `internal/cmd/milestone.go:784`
- `stateMapKey` (`board.state_map` keys gated + normalised on write; doctor reports one on disk as an issue) — `internal/cmd/project.go:477`
- `TestTomlFieldsCarryMatchingJSONTags` (toml↔json tag parity, transitive walk over the eight document roots) — `internal/cmd/json_tag_parity_test.go:48`
- `writeVersion` (one validated writer for both version homes, tracked copy first) — `internal/cmd/state.go:239`
- doctor version-drift check (project.toml vs state.json, skipped on a fresh clone) — `internal/cmd/doctor.go:349`
- `checkConfigTrust` (rejectable branch name, off-allowlist api_base, tracked local.toml and pre-2.24 git as exit-code-moving findings) — `internal/cmd/doctor.go:837`

The project version has two homes and one writer. The release-facing copy lives in the tracked `.dross/project.toml` `[project].version`; `state.json` carries the copy dross bumps. `writeVersion` validates once and writes both — tracked file first — so they cannot diverge silently, and `dross doctor` reports drift (or a missing `[project].version`) as an exit-code issue naming the fix. A fresh clone with no state.json is skipped rather than flagged.

_c8b346e · extended gitlab-ship-provider · 0f209c9 · extended validator-truth · 5e73e88 · extended cli-surface-sweep · 1a840f4 · extended board-state-map-truth · 3272339 · extended state-json-branch-safety · 3f12d7e · extended config-trust-hardening · 83e5dcf_

### Deferred-item routing

Give every deferred idea a destination instead of leaving it write-only: `/dross-spec` routes each (pull-into-phase / milestone-backlog / named-phase / someday), parked ideas re-surface as candidate criteria when their target phase is scaffolded, and someday items get triaged through `/dross-inbox`. An item lives in one of three states — someday (no target), routed (target set, cleared back to someday with `dross deferred unroute`), or dismissed (`dross deferred dismiss`, `--undo` to reverse); a board-less repo still triages its local deferred backlog because `/dross-inbox` §0 skips the board source rather than hard-stopping.

- `Deferred.Target` (schema) — `internal/phase/phase.go:253`
- `Deferred.Dismissed` (dismissed-state flag) — `internal/phase/phase.go:258`
- `Deferred` (dross deferred list/route/unroute/dismiss) — `internal/cmd/deferred.go:29`
- `collectDeferred` (scan + filter) — `internal/cmd/deferred.go:40`
- `deferredRoute` (stamp target on disk) — `internal/cmd/deferred.go:155`
- `deferredDismiss` (retire to dismissed, someday-only) — `internal/cmd/deferred.go:194`
- `deferredUnroute` (clear target → someday; idempotent, refuses dismissed) — `internal/cmd/deferred.go:248`
- `deferredList --dismissed` (hide/surface dismissed) — `internal/cmd/deferred.go:74`
- dangling-target guard in `Validate` — `internal/cmd/validate.go:118`
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

The propose-and-react contract for interactive commands — a terse builtin rule in every `dross rule show`, the full `_interaction.md` playbook, and a `dross interaction show` emitter that injects the playbook verbatim into interactive prompts (the c-3 pilot disproved nested @-include, so delivery is the CLI emitter), plus a per-decision-point audit checklist. **Every** interactive command is now wired and audited: the five core-loop prompts (plan/execute/verify/ship/review), the seven setup/config prompts (init/onboard/options/rule/inbox/quick/milestone), and the five remaining audit/handoff prompts (architecture/secure/quality/pause/resume) — each restructured to one-decision-per-turn (per-field identity walks, an options section-pick gate, per-criterion milestone scoping, single-gated-turn scaffolds, summary-confirm instead of artifact paste-back), guarded by grep + per-section prompt-sentinel tests. The model is documented as a first-class loop behaviour in the README's `## Interaction` section. Coverage is now **fail-closed**: a shared classifier proves every command-backed prompt is either interactive-with-an-audit-section or enrolled in the audit doc's machine-read `## Exempt` list (status, plan-review), failing the build on any unclassified prompt, with `dross doctor` surfacing the same verdict on-demand inside the dross source tree. `/dross-spec`'s §3 takes the contract further: instead of a multiSelect "which gray areas?" pre-selection, it walks **every** area Claude is *genuinely uncertain* about, one at a time, with a user off-ramp — the discriminator is Claude's own uncertainty, not whether the user might have an opinion. Candidate surfacing now shares a **defer-or-add** framing documented once in `_interaction.md`: a borderline/optional candidate is offered as a defer-first either/or ("defer it" leads, "add to current phase" follows), applied in spec `§4a` as a two-step entry-gate-then-destination route that drops the old §4a double-offer, and in plan `§3`/`§4` for borderline task proposals and the coverage-gap check — so spec and plan inherit the convention instead of restating it. `/dross-spec` §2 now opens with a *proposed candidate-criteria slate* (derived from milestone scope, gap analysis, and parked ideas) gated accept/reword/drop per item — replacing the free-recall "list 3–7 outcomes" ask.

- `Interaction` / `interactionShow` (CLI) — `internal/cmd/interaction.go:10`
- `assets.InteractionPlaybook` (re-derived from `assets.FS`) — `assets/embed.go:26`
- `dross-interaction-contract` builtin — `internal/rules/rules.go:137`
- `_interaction.md` playbook — `assets/prompts/_interaction.md`
- per-decision-point checklist + `## Exempt` list + coverage convention — `docs/interaction-audit.md`
- README first-class write-up — `README.md` `## Interaction`
- `interactionCoverage` (fail-closed classifier + Exempt parser) — `internal/cmd/interaction_coverage.go:37`
- `interactionCoverageWarnings` (dross doctor on-demand lint) — `internal/cmd/doctor.go:544`
- `TestInteractionCoverageFailClosed` (coverage gate + convention guard) — `internal/cmd/interaction_coverage_test.go:15`
- `TestSpecPromptWalksEveryGrayArea` (spec §3 walk-all gray-area guard) — `internal/cmd/spec_prompt_test.go:102`
- `TestInteractionSnippetHasDeferOrAddPattern` (defer-or-add pattern in the playbook) — `internal/cmd/interaction_snippet_test.go`
- `TestSpecPromptTwoStepRouting` (spec §4a two-step defer-first route, no double-offer) — `internal/cmd/spec_prompt_test.go`
- `TestPlanPromptBorderlineTaskDeferFirst` / `TestPlanPromptCoverageGapEitherOr` (plan defer-first + coverage-gap either/or) — `internal/cmd/plan_prompt_test.go`
- core-loop wiring + prompt-sentinel guards — `internal/cmd/interaction_coreloop_test.go`
- setup/config wiring + anchor + no-bundle guards — `internal/cmd/interaction_setupcmds_test.go`
- audit/handoff wiring + audit-conformance + README guards — `internal/cmd/interaction_othercmds_test.go`
- subagent-offload audit (per-prompt disposition of heavy inline reads — offloads-already / offload-worthy / inline-only — fail-closed like the interaction audit; verify §2 and execute §1b carry size-gated read-only offload passages whose agent-gate boundary and conditional phrasing are test-pinned) — `docs/subagent-offload-audit.md`
- `TestSubagentOffloadAuditCoversEveryPrompt` (offload-audit coverage gate) — `internal/cmd/subagent_offload_audit_test.go:41`
- `TestVerifyPromptOffloadGuidance` / `TestExecutePromptOffloadGuidance` (offload passages pinned) — `internal/cmd/verify_prompt_test.go` / `internal/cmd/execute_prompt_test.go`

_introduced 10-interaction-contract · extended 11-retrofit-core-loop · extended 12-retrofit-setup-commands · extended 13-audit-and-readme · extended retrofit-readmostly-commands · extended gray-area-walkthrough · extended interaction-defer-or-add-framing · extended task-reordering · extended subagent-offload-audit · de83813_

### Issue board sync

Mirror milestones, phases, quick tasks, and the milestone backlog onto an issue board — driven solely by a dedicated `[board]` config block, independent of `[remote]`, so a repo ships code to one host and tracks issues on another. Backends sit behind a `BoardClient` interface that `forge.NewBoard` dispatches by provider: the provider-aware forge `*Client` (forgejo/gitea/gitlab), a sibling `YouTrackClient` (REST CRUD, bearer permanent-token, readable-id `PROJ-7` addressing, `?fields` projection), a `JiraClient` (Jira Cloud REST v3, HTTP Basic email:token, string `PROJ-123` keys, ADF bodies, transition-driven state, milestones as project versions), or a `GitHubClient` (repo issues with integer milestones — forge-shaped — plus an isolated Projects v2 `addProjectV2ItemById` add-to-board on create when a board is configured). board.json links every artefact by the tracker's readable **string** id. YouTrack adds milestone entities per `[board].milestone_mode` (version bundle / agile board / epic), lifecycle→State mapping via the default map + `[board].state_map` (unmapped warns and skips), and backlog sync of unscaffolded slugs + someday ideas attached per mode (Fix versions / Epic subtask / project-based board). The lifecycle vocabulary that both sides of that mapping speak has **one** home in `configenum.LifecycleStatuses` — planned / in-progress / verifying / shipped / complete — and the producer emits `planned` (locked `lifecycle_vocabulary`, renamed from `planning`), so a locked-but-unstarted plan sets tracker state instead of hitting the unmapped-transition skip. Two guards keep emitters and maps from drifting apart again: a **bidirectional** test fails when a status dross emits at a call site has no state-map entry *or* a state map keys on a status nothing emits (each forge map checked independently, never unioned), and `--status` on `dross issue phase-sync` is validated against the set — ahead of the `[board].enabled` short-circuit, so a bad value can't exit 0 as a silent no-op — then normalised back before use. `shipped` and `complete` are emitted rather than dead keys (locked `dead_map_keys` / `terminal_emit_sites`): ship sets `--status shipped` when the PR merges and `--status complete --close` at finalize, the two board moments ship already had, leaving `dross phase complete` with no board coupling. `dross doctor` validates a configured `[board]`; the inbox board source is gated on `[board].enabled`. The Jira path is **proven end-to-end against live Jira Cloud** (2026-07-25 v1.0 self-audit): the round-trip surfaced and fixed two bugs the httptest coverage had hidden — `board.auth_user` (required by `NewJira`) wasn't wired into `dross project set`, and `ListIssues` used Jira's removed `/rest/api/3/search` endpoint (HTTP 410), now migrated to `/search/jql`.

- `forge.BoardClient` (interface) + `forge.NewBoard` (provider dispatch) — `internal/forge/forge.go:151`
- `forge.YouTrackClient` + `NewYouTrack` — `internal/forge/youtrack.go:28`
- `YouTrackClient.EnsureMilestoneEntity` / `SetState` — `internal/forge/youtrack.go:190`
- `forge.JiraClient` + `NewJira` (REST v3, versions, transitions) — `internal/forge/jira.go:28`
- `JiraClient.ListIssues` (current `/search/jql` endpoint; the legacy `/search` was removed) — `internal/forge/jira.go:184`
- `forge.GitHubClient` + `NewGitHubProjects` (repo issues + Projects v2 attach) — `internal/forge/github.go:28`
- `board.Board` (string readable-id link registry) — `internal/board/board.go:29`
- `openBoard` (resolves client solely from `[board]`) / `syncBacklog` — `internal/cmd/issue.go:79`
- `board.*` dotted config incl. `auth_user` — `internal/cmd/project.go:193`
- `configenum.LifecycleStatuses` (single lifecycle vocabulary: status label + both forge state maps) — `internal/configenum/configenum.go:118`
- `issuePhaseSync` (`--status` validated against the set before the enabled short-circuit, then normalised) — `internal/cmd/issue.go:411`
- `TestStateMapsKeyExactlyTheEmittedStatuses` (bidirectional emitted-statuses ↔ state-map-keys gate) — `internal/cmd/board_lifecycle_divergence_test.go:242`
- `TestShipPromptEmitsTerminalBoardStatuses` (ship emits `shipped` on merge, `complete` at finalize) — `internal/cmd/ship_prompt_test.go:304`

_a073ab7 · extended gitlab-ship-provider · 27e1a4f · extended youtrack-board-integration · extended prove-or-demote-board-sync · 50290f0_
_extended additional-board-backends (GitHub Projects + Jira) · 9d60ea2 · extended board-state-map-truth · 3272339_

### Machine-local store

Facts that are true of *this* clone and must never ride cumulative history live in gitignored files, not in the tracked tree. `.dross/state.json` is the second and larger tenant: it is removed from the index and gitignored (scaffolded that way by `init` and `onboard` via `ensureDrossGitignore`), so no checkout carries a copy that could replace the live one and its `history[]` is machine-local by design. Migration is untrack-going-forward only — no history rewrite — so old branches and tags keep their copies; `dross doctor` reports a still-tracked state.json with the literal fix, and [Branch-switch safety](#branch-switch-safety) covers the dross-side switches. The incident this closes (a stale long-lived branch checked out over live state, 12 history entries down to 2) is reproduced end to end by a regression test verified to fail against the reverted code, and a docs scan pins README, the man page, the prompts and `.gitignore` to describe the file as machine-local.

The first tenant is `.dross/local.toml`, read and written through `dross local get|set`. Because it is machine-authored and never cloned, it is where the [host allowlist](#api-host-allowlist)'s escape hatch lives: `allow_hosts` adds a host the derivation can't reach, and the read **refuses a `local.toml` that git reports as tracked** rather than trusting its contents — otherwise a hostile repo could ship its own authorization and the allowlist would be self-authorizing again. `init` and `onboard` seed the gitignore entry so the file starts untracked, and doctor reports a tracked one. Its own first tenant is `quick_base` — the branch a standalone quick task forked from. Ship and `phase complete` push a recorded quick base's unpushed `.dross/` chores on *that* branch rather than on an inferred one, closing the divergence where a quick committed to main mid-phase left local main unpushed and the next phase squash-merge could not fast-forward it. An unrecorded quick base, or one whose ref is gone, is left alone rather than guessed at, and a base equal to the phase's own base is a no-op.

- `Local` (`dross local get|set`, gitignored `.dross/local.toml`) — `internal/cmd/local.go:34`
- `readAllowHosts` (`allow_hosts` escape hatch; refuses a git-tracked local.toml instead of trusting it) — `internal/cmd/local.go:107`
- `TestReadAllowHostsRefusesTrackedLocal` (the self-authorizing hole, pinned shut) — `internal/cmd/local_test.go:134`
- `pushQuickBaseIfRecorded` (ship + complete push chores on the recorded quick base, never an inferred one) — `internal/cmd/basebranch.go:129`
- `TestShipReconcilesRecordedQuickBase` (pins ship's call site so a recorded quick base can't be left unpushed) — `internal/cmd/ship_test.go:1115`
- `ensureDrossGitignore` (state.json gitignored by init + onboard, out of this repo's index) — `internal/cmd/gitignore.go:79`
- `TestStaleBranchCheckoutCannotClobberLiveState` (end-to-end incident reproduction) — `internal/cmd/state_clobber_regression_test.go:77`
- `TestDocsDoNotClaimStateIsTracked` (docs/prompts/.gitignore scan) — `internal/cmd/gitignore_test.go:284`
- `TestShipToCompleteKeepsLiveState` (full ship → squash-merge → complete leaves a long live history whole) — `internal/cmd/completion_state_truth_test.go:99`
- `TestRawCheckoutStillClobbersLiveState` (the control: proves the fixture is truncatable, so the survival assertions are not vacuous) — `internal/cmd/completion_state_truth_test.go:175`

_introduced complete-base-truth · extended state-json-branch-safety · extended completion-state-truth · 1ecde33 · extended config-trust-hardening · 000286c_

### Milestone branch model

Milestone work rides a `milestone/<version>` integration branch: scoping a milestone stacks it on the current milestone's still-unmerged branch tip when one exists — else cuts from main, or from an explicit `--base` override — and records the cut point as a stored fact. New phases and quicks fork from the resolved base (falling back to main with a nudge when no milestone is active). Phase PRs target it and `phase complete` fast-forwards it. `dross milestone complete` opens the milestone's integration PR against its recorded parent while that parent is unmerged, retargeting main once the parent has merged or vanished (merge-commit; `--finalize` fast-forwards main and deletes the branch — refusing while an unmerged stacked dependent, or an open forge PR, still targets it).

- `resolveMilestoneCutPoint` (create stacks on the current milestone's unmerged branch tip; `--base` forces it; the cut point is recorded, never re-inferred) — `internal/cmd/milestone.go:470`
- `Milestone.BaseOr` (recorded cut-point branch; empty reads as main) — `internal/milestone/milestone.go:76`
- `milestoneMergedIntoMain` (git-ancestry merge probe, origin-preferred with local fallback) — `internal/cmd/milestone_merged.go:32`
- `milestonePRBase` (milestone-complete PR targets the recorded parent while unmerged and present on origin, else main) — `internal/cmd/milestone.go:232`
- `resolveNewWorkBase` (existence-aware base resolver: milestone branch when its ref exists, else main) — `internal/cmd/basebranch.go:159`
- `forkPhaseBranch` (phase create/insert fork off the resolved base) — `internal/cmd/phase.go:853`
- `BaseBranch` (`dross base-branch`: resolved base on stdout, no-milestone nudge on stderr) — `internal/cmd/basebranch.go:20`
- `ensureMilestoneBranch` (create cuts+pushes at scope time) — `internal/cmd/milestone.go:520`
- `dependentMilestones` (prune/finalize refuse to delete a branch an unmerged stacked milestone still records as its base) — `internal/cmd/milestone_dependents.go:33`
- `OpenPRsTargeting` / `guardOpenPRsTargeting` (forge open-PR check layered over the record scan; an unavailable provider announces its skip rather than passing silently) — `internal/ship/basepr.go:36`, `internal/cmd/milestone_dependents.go:85`
- `milestoneComplete` (opens the milestone's integration PR; `--finalize` ff + branch delete) — `internal/cmd/milestone.go:135`
- `staleMilestoneBranches` (ancestry, then squash-commit resolution against the candidate's own first parent) — `internal/cmd/milestone_stale.go:58`
- `milestonePrune` (`dross milestone prune`: deletes stale branches local + on origin) — `internal/cmd/milestone.go:49`
- doctor stale-milestone-branch report (read-only) — `internal/cmd/doctor.go:376`

A milestone branch whose work already landed on main is detected and removable. Ancestry catches the merge-commit case; the squash-merged case that `git branch --merged` is blind to is resolved by matching the branch's diff patch-id against main's commits, deterministically when several are ambiguous. The surfaces are split on purpose (locked `prune_surface`): `dross doctor` *reports* a stale branch read-only, and the explicit `dross milestone prune` performs the local+remote deletion, refusing on a dirty tree, when the branch is the current HEAD, or when an unmerged stacked dependent or open forge PR still targets it.

_introduced milestone-branch-model · extended state-json-branch-safety · extended milestone-stacking · cbad9c9_

### Milestone scoping

Author and validate milestone.toml — title, success criteria, non-goals, phase order.

- `Milestone` (CLI) — `internal/cmd/milestone.go:20`
- `milestone.Milestone` — `internal/milestone/milestone.go:20`

_c8b346e_

### Mutation testing adapters

Language-specific mutation tools normalised to one Report (Stryker for TS/JS/Svelte, Gremlins for Go invoked per-package). Stryker is invoked as `npx @stryker-mutator/core` (not the deprecated bare `stryker`) at an **exact pinned version** rather than whatever the registry currently serves as latest — the same pin backs the argv and the install hint, so a compromised release can't be pulled into a verify run — with a `[mutation.stryker] workdir` monorepo knob that round-trips repo-relative paths. In docker runtime mode the exec prefix is derived from `runtime.test_command` by `dockerPrefix`, whose leading binary must be **exactly** `docker` (a field check, not `HasPrefix`) so a committed `dockerevil …` can't promote an arbitrary PATH binary into the adapter's argv under clone-and-run.

- `Adapter` — `internal/mutation/adapter.go:46`
- `Report` — `internal/mutation/adapter.go:18`
- `Gremlins.Run` — `internal/mutation/gremlins.go:84`
- `Stryker.Run` — `internal/mutation/stryker.go:48`
- `Stryker.runArgs` (npx invocation + workdir knob) — `internal/mutation/stryker.go:113`
- `strykerPin` (exact `@stryker-mutator/core@9.6.1`, shared by the argv and the install hint) — `internal/mutation/stryker.go:100`
- `dockerPrefix` (exact-`docker` exec-prefix guard) — `internal/cmd/verify.go:234`

_introduced c8b346e · extended 01c10f0 · extended context-hygiene · extended self-audit · de8b076 · extended config-trust-hardening · 266a84d_

### Phase base truth

The branch a phase forked from is a recorded fact, not an inference. `forkPhaseBranch` writes the resolved base into the phase-scoped `changes.json` (`changes.SetBase`, beside the existing `pr` field) as soon as `checkout -b` succeeds, and ship overwrites it with the base the PR was actually opened against, riding the same commit and push as the PR record — so a phase that never ships still has a base, and the PR's real target wins if the two ever diverge (locked `base_write_timing`). `phase complete` reads it back — working tree, then the phase ref, then an explicit `--base` — and **refuses** when nothing is recorded, naming the phase and both candidate branches, instead of falling back to a base derived from `current_milestone`. That inference is what fast-forwarded a stale `milestone/<version>` for a phase actually forked from main; the locked `legacy_escape` keeps pre-record phases completable by having the user *type* the branch, a conscious act rather than a guess. The incident is pinned end to end by a fixture staging the exact trap — phase forked from main, stale milestone branch present locally, PR merged to main — asserting completion either lands on main or refuses, never fast-forwarding the milestone branch and never deleting the phase branch. Side effect worth knowing: recording the base at create time makes `.dross/phases/<id>/` tracked immediately, so checking out another branch now removes a fresh phase's directory.

- `changes.SetBase` (phase-scoped forked-from record, beside `pr`) — `internal/changes/changes.go:156`
- `forkPhaseBranch` (create-time write, after `checkout -b` succeeds) — `internal/cmd/phase.go:853`
- ship-time base overwrite (what the PR was actually opened against, on the PR-record commit) — `internal/cmd/ship.go:358`
- `resolveCompleteBase` (tree → phase ref → `--base`; refuses rather than inferring) — `internal/cmd/phase.go:657`
- `staleMilestoneFixture` (end-to-end incident reproduction, success and refusal arms) — `internal/cmd/phase_base_truth_test.go:31`

_introduced complete-base-truth · e1f72be_

### Phase lifecycle

Create, list, number, migrate, complete, and reorder/insert/rename phases on dedicated phase/<id> git branches. Phase identity is the bare slug and order lives solely in the milestone `phases` array (phase.Ordered), so create makes bare-slug dirs and appends to the array, while `phase number` / status / the version patch digit all read the 1-based array position (DisplayNumber) and `phase migrate` converts a legacy NN-slug repo idempotently — skipping the in-flight phase and disambiguating colliding slugs — with phase.Dir resolving old NN-slug ids for permanent back-compat. complete is fast-forward + completion-record write + branch-delete (still no commit to main — the record lands in the machine-local, gitignored `state.json`, so it rides nothing). It is the **sole writer of the completed-state transition**: the cleared `current_phase` plus one history-scan-guarded `completed <id>` entry, re-loaded from disk immediately before the write so recovery's own `merged <id>` survives, written *before* the branch teardown so a failed deletion still leaves the confirmed merge recorded, and cleared only when `current_phase` names the phase being completed. `dross ship` marks the phase `shipped` and leaves `current_phase` set; only a confirmed merge clears it. The run ends by **stating the resulting topology** — the branch HEAD landed on, what was actually deleted per side (`describeTeardown` never claims a deletion that did not happen), and the `<n> commits on <base>, not yet on main` clause from `branchTopology`/`renderTopologyLine`, passed the authoritative recorded base rather than an inferred one; `dross status` prints the same line unconditionally so the answer is standing, not a message that scrolls away. Completion is gated by an **authoritative merge check** (`mergeGate`): it reads the phase's recorded PR number from changes.json — resolving it from origin/<base>'s fetched changes.json (`originRecordedPR`) when the stale post-squash-merge working tree lacks it — and requires the provider (`ship.PRStatusFunc`, authoritative across all 5 ship providers) to report that PR merged — announcing a skip when the provider can't answer or errors — falling back to a `git merge-base --is-ancestor` check that **refuses-when-inconclusive** (a missing/squash-deleted ref or a non-ancestor both refuse) — replacing the old cumulative `completed <id>` breadcrumb, which a later merged phase could drag onto the base and thereby false-complete an unmerged phase; only on a confirmed merge does it delete both the local and the remote phase branch idempotently. `mergeGate` also refuses completion when the provider reports the PR's base was retargeted since it was recorded (`checkBaseRetarget`, comparing `PRStatus.BaseRef` against the recorded base after normalizing `refs/heads/` prefixes) — even when merged is true, since merging into an unexpected base is a fact worth refusing on, not a formality; an empty BaseRef (a provider that can't report one) announces a skipped check rather than raising a false alarm, and the refusal is proven uniform across all 5 ship providers. The lifecycle verbs `insert` / `move` / `rename` edit a phase's array slot and identity through pure splice helpers (InsertRelative / MoveRelative / RenameInArray) and shared plumbing (exactly-one-anchor validation, no-op-before-collision, ship-guard via the origin branch); insert scaffolds with a strict slug (no auto-suffix) and rename moves dir + spec id + array entry + deferred targets + local branch atomically — all leaving every other phase byte-for-byte untouched. Completion's branch switch is deferred past *every* refusal — the merge gate, the fetch, and the safety-net push all decide while HEAD is still on `phase/<id>` — so a refused complete moves no local ref and needs no compensating checkout, which would itself be a branch switch that can fail mid-refusal and leave a worse state than not trying.

- `Phase` (CLI) — `internal/cmd/phase.go:24`
- `phaseCreate` — `internal/cmd/phase.go:133`
- `phaseNumber` — `internal/cmd/phase.go:54`
- `phaseMigrate` — `internal/cmd/migrate.go:31`
- `phaseComplete` (branch switch deferred past every refusal path) — `internal/cmd/phase.go:234`
- `mergeGate` (authoritative completion gate: recorded-PR merge status + ancestry refuse-when-inconclusive fallback) — `internal/cmd/phase.go:762`
- `originRecordedPR` (post-fetch recorded-PR resolution from origin/<base>'s changes.json) — `internal/cmd/phase.go:737`
- `ship.PRStatusFunc` / `ship.GetPRStatus` (provider merged-status + base-ref lookup across all 5 ship providers, exported overridable seam; `ErrMergeStatusUnsupported` is a forward seam, unreachable via real dispatch) — `internal/ship/merged.go:53`
- `checkBaseRetarget` (mergeGate's post-merge base-retarget refusal, refs/heads/-normalized, uniform across all 5 providers) — `internal/cmd/phase.go:821`
- `phaseMove` / `phaseInsert` / `phaseRename` — `internal/cmd/phase_lifecycle.go`
- array-order splice helpers (`InsertRelative`, `MoveRelative`, `RenameInArray`) — `internal/phase/phase.go`
- slug identity helpers (`Dir`, `Ordered`, `DisplayNumber`, `UniqueSlug`) — `internal/phase/phase.go:34`
- complete-path verify heal (records a resolved-but-unfinalized verdict before the branch switch; never invents a verdict) — `internal/cmd/phase.go:296`

The completion statement it prints at the end of a run — landing branch, per-side teardown, commits-not-yet-on-main — is covered in [Branch topology reporting](#branch-topology-reporting).

_c8b346e · extended 02-harden-ship-merge-complete-flow · extended 03-fix-completion-chore-divergence · extended 14-stable-slug-phase-ids · extended phase-lifecycle-commands · extended verify-merge-before-completion · extended ship-clean-tree · extended verify-auto-finalize · extended complete-base-truth · extended completion-state-truth · extended provider-merge-parity · 4d4fb45_

### Plan persistence

Phase artefacts (plan.toml / spec.toml) are written atomically: saveTOML encodes into a temp sibling and os.Rename's it over the target only after a fully successful write, so a mid-write crash or a failed encode leaves the previous file byte-identical rather than truncated. This is the durability guarantee behind the task-lifecycle integrity checks — a rejected or interrupted mutation can never corrupt the plan.

- `saveTOML` (atomic temp-file + rename write) — `internal/phase/phase.go:398`
- `Plan.Save` / `Spec.Save` — `internal/phase/phase.go`

_introduced task-lifecycle-commands · 367c723_

### README accuracy guard

Keeps the README's command table from lying about the CLI: `newRoot` is extracted from `main` so a test can inspect the real assembled command tree, and a parity test asserts every `` `dross <cmd>` `` the README advertises is a real top-level command (the over-claim failure mode) plus that the status line isn't a stale `v0.x`. Under-claiming (an internal command the table omits) is allowed. A companion needle guard pins the surfaces a user has to *know about* to recover a phase — `dross local`, `quick_base`, and complete's `--base` / `--recover` — so shipping the behaviour without documenting it fails the suite.

- `newRoot` (testable command-tree assembly) — `cmd/dross/main.go:16`
- `TestReadmeAdvertisesOnlyRealCommands` (over-claim guard) — `cmd/dross/main_test.go:33`
- `TestReadmeStatusNotStale` (stale-version guard) — `cmd/dross/main_test.go:285`
- `TestReadmeDocumentsBaseTruthSurfaces` (needle guard: `dross local`, `quick_base`, `--base`/`--recover`) — `internal/cmd/readme_doc_test.go:43`
- `TestNarratedCommandsResolveAgainstTheTree` (fourth sibling: `dross <cmd>` narrated from a Go string literal) — `cmd/dross/main_test.go:215`
- `TestNarratedCommandsGuardCatchesBogusSubcommands` (the guard's own failure path — top-level resolution alone must not satisfy it) — `cmd/dross/main_test.go:266`

The guard is now a family of four over every surface that can name a command: the README table, `ship.md`, the curated hint table, and — added last — the `dross …` invocations the CLI *prints at the user* from Go string literals. That fourth surface is where the family's own gap was found: a command can be unregistered from `newRoot` while an error message still tells the user to run it, and the resulting "unknown command" pushes them back to the raw git incantation the guard exists to retire. Mutation testing cannot catch it either (gremlins skips `./cmd/dross` as a zero-covered-mutant blind spot), so the guard is proven by hand-mutation: deleting `cmd.Checkout()` leaves the rest of the suite green and turns this one red. It reads **string literals only** — a stale name in a comment misleads a reader, but only a narration string reaches the user.

_introduced readme-truth-pass · extended complete-base-truth · extended completion-state-truth · 1ecde33_

### Repo onboarding

Scan an existing repo's signal files (Dockerfile, package.json, go.mod, …) into a draft project.toml, seeding `[runtime]` + `[stack].profile` from the matched stack profile.

- `Onboard` — `internal/cmd/onboard.go:26`
- `scanRepo` — `internal/cmd/onboard.go:162`
- `toProject` — `internal/cmd/onboard.go:193`

_c8b346e · extended 07-stack-profiles · eb602f1_

### Repository repair

Detect and fix a `.dross/` clobbered by a bad checkout or an accidental hand-edit. `dross repair` diffs the working tree against git history: tracked files that are missing or diverged from their last committed blob are restored via a shared checkout primitive, phase directories a checkout wiped but `origin/<mainBranch>` still knows about are repopulated the same way, and a missing or clearly stale `state.json` (its `current_phase` disagreeing with the checked-out `phase/<id>` branch) is reconstructed from phase-completion commit markers plus the checked-out branch name. Dry-run by default — `--apply` writes the restores and commits them — so a lossy state reconstruction is shown before it becomes the new truth. `dross doctor` read-only-reuses the same detectors to surface a clobber and point at `dross repair` as the fix, matching doctor's existing remediation-hint pattern.

- `detectModifiedOrMissingTracked` / `restorePathFromRef` — `internal/cmd/repair_files.go:28`
- `reconstructState` — `internal/cmd/repair_state.go:42`
- `detectMissingPhaseDirs` — `internal/cmd/repair_phasedirs.go:18`
- `Repair` / `checkStaleState` — `internal/cmd/repair.go:19`
- doctor's clobber section (`detectModifiedOrMissingTracked` + `detectMissingPhaseDirs` findings) — `internal/cmd/doctor.go:341`

_introduced dross-repair · a85d29c_

### Root resolution

Decide what counts as a dross repo, and say so the same way everywhere. `state.json` is deliberately *not* in the required set — it is machine-local and gitignored, so a fresh clone legitimately has none; `ensureState` materializes it on demand from `project.toml`'s `[project].version` with empty history rather than failing the root. A `.dross/` that exists but is missing `project.toml` resolves to *not a dross repo* — indistinguishable from no `.dross/` at all to callers — so a half-built directory can never be mistaken for an initialised one. The up-walk stops at the first `.dross/` it finds, complete or not (locked `walk_stop`), so a stray directory in a nested repo can never silently bind writes to an ancestor project's real root. Completeness is an *existence* check only (locked `completeness_check`): a file that exists but fails to parse is a broken state, not an uninitialised one, and stays loud everywhere — including in the hook targets. `LocateRoot` reports the misses without erroring, which is the seam doctor and `ship recover` read; `FindRoot` mints the `IncompleteRootError` that carries them, every message naming the missing file plus the single shared `RepairHint`. Silent exit 0 is scoped to the four hook targets alone — every other command fails loudly against an incomplete root, and a `go/ast` allowlist gates which files may swallow `ErrNoRoot` or call `LocateRoot`, so a future command can't quietly join the silent set. `dross doctor` projects the same misses as a distinct not-a-dross-repo verdict (its block is pinned equal to `LocateRoot`'s slice), and `dross onboard` adopts an incomplete `.dross/` in place, preserving what's already there.

- `FindRoot` / `ErrNoRoot` / `RequiredRootFiles` / `RepairHint` — `internal/cmd/root.go:112`
- `IncompleteRootError` — `internal/cmd/root.go:36`
- `MissingRootFiles` — `internal/cmd/root.go:56`
- `LocateRoot` (misses without erroring — doctor + ship-recover seam) — `internal/cmd/root.go:76`
- `finalizeIncompleteRoot` (doctor's distinct verdict) / `incompleteRootHeading` — `internal/cmd/doctor.go:642`
- `Onboard` (adopts an incomplete root in place) — `internal/cmd/onboard.go:26`
- `TestRootHelperCallersAreAllowlisted` (AST allowlist over the swallow set) — `internal/cmd/incompleteroot_test.go:166`
- `ensureState` (materializes a missing state.json from project.toml's version) — `internal/cmd/state.go:267`

_introduced root-robustness · extended state-json-branch-safety · 93b072a_

### Rules system

Two-tier (builtin + project) MUST-FOLLOW rules, merged and rendered via `dross rule show`. Project-rule degradation is scoped to a *genuinely absent* root: `loadMerged` falls back to builtins-only when there is no `.dross/` at all, but an incomplete or corrupt root surfaces its error, so `dross rule show` never quietly drops a repo's project rules.

- `rules.Set` — `internal/rules/rules.go:41`
- `rules.Merge` — `internal/rules/rules.go:82`
- `Rule` (CLI) — `internal/cmd/rule.go:14`
- `loadMerged` (degrades only for an absent root) — `internal/cmd/rule.go:216`

_c8b346e · extended root-robustness · 6d33d3b_

### Security audit (dross-secure)

Context-free, read-only multi-pass security audit: real scanners plus an adversarial refute-panel over cold subagents, emitting a verified findings ledger and scaffolding a remediation phase. The `dross security` CLI is the deterministic surface (run dirs, scanner detection, findings→spec scaffold); `secure.md` orchestrates the audit.

- `security.NewRun` — `internal/security/run.go`
- `security.Catalog` / `security.Detect` — `internal/security/catalog.go`
- `security.Ledger` — `internal/security/findings.go`
- `security.BuildManifest` — `internal/security/recon.go`
- `security.ScaffoldSpec` — `internal/security/scaffold.go`
- `security.DecideDockle` (three-state image-scan decision: run-supplied / skip-no-image / skip-missing-bin, never builds) — `internal/security/dockle.go:43`
- `securityRun --image` / `resolveImage` (`--image` flag, `$DROSS_IMAGE` fallback) — `internal/cmd/security.go:134`
- `Security` (CLI) — `internal/cmd/security.go:27`

The scanner catalog now sources language-dedicated tools from the active stack profile (agnostic tools stay inline); `recon.DetectLanguages` delegates to the single `stack.DetectLanguages`. `BuildManifest` also unions any marker-file stack's scanners (via `stack.MarkerProfiles`) additively on top of the detected languages, so a marker-only repo (e.g. a Dockerfile with no source extension) still gets its scanners — including the deepened IaC/container loadout (`checkov` cross-family, `dockle` for docker), each surfaced installed-vs-missing. The security surface also covers **container image-layer scanning**: `DecideDockle` is a pure three-state decision that never runs `docker build`, and `dross security run --image <ref>` (or `$DROSS_IMAGE`) feeds it — with no image the run skips-with-reason rather than emitting a silent all-clear.

_introduced 05-dross-secure · extended 07-stack-profiles · extended 09-marker-file-detection · extended deepen-container-iac-scanning · f07fc15_

### Self-update & distribution

Ship dross as a single self-contained binary that carries its own assets and updates itself. The binary embeds every command skill + prompt (`assets.FS`, with the `all:` prefix so the underscore-prefixed `_interaction.md` survives), guarded against drift from the on-disk assets/ tree. `dross install` materializes them into ~/.claude — symlinking assets/ off a source checkout, writing real-file copies from the embedded FS otherwise (`--copy`/`--link` override) — cleanly syncing the dross-* namespace (prune dropped skills *and* prompts, never touch non-dross), with `make install` delegating to it via `--link`. `dross update` fetches the latest GitHub release, then applies a two-stage trust gate before touching any binary: it verifies a **minisign signature** over checksums.txt against a public key embedded in the binary (fail-closed — a missing `checksums.txt.minisig` or a wrong-key/tampered signature refuses the update), and only the signature-verified checksums.txt is then used to verify the platform archive's SHA-256 (still refusing on mismatch). The archive is a `.tar.gz` containing `dross` on darwin/linux and a `.zip` containing `dross.exe` on windows; the updater dispatches on the asset suffix (`AssetName`/`BinaryName`), so one download→verify→extract→swap path serves every platform. It atomically swaps the running binary only when the release is strictly newer (or `--force`; `--check` reports without applying), then re-syncs assets by exec'ing the freshly-swapped binary (never the old in-process engine). The release pipeline signs checksums.txt with minisign in CI (goreleaser `signs` block; the private key is materialized to `$RUNNER_TEMP` from a GitHub secret and the password is piped via stdin), publishing `checksums.txt.minisig` as a release artifact. Install channels: `install.sh` is the `curl | sh` bootstrap (uname-detect, download+verify into a temp dir, mv onto PATH only after the checksum, then `dross install`; shellcheck CI-gated); and `install.ps1` is the PowerShell analog for Windows (same verify-before-place safety). goreleaser builds darwin/linux/windows on amd64+arm64. The README documents all channels + the `dross update` flow. (A Homebrew tap via goreleaser `brews` was scoped but deferred — not published until the tap repo + token exist.)

- `assets.FS` (`go:embed all:commands all:prompts`) — `assets/embed.go:20`
- `update.AssetName` / `update.BinaryName` (per-OS archive + binary name: .zip/dross.exe on windows) / `VerifyChecksum` / `Decide` / `AtomicReplace` — `internal/update/update.go`
- `update.VerifySignature` / `EmbeddedMinisignPublicKey` / `TrustedMinisignKey` (signature trust anchor + override seam) — `internal/update/signature.go:43`
- `update.Client` (latest release + download) — `internal/update/update.go:232`
- `Install` (symlink/copy materialize + dross-* prune) — `internal/cmd/install.go:26`
- `Update` signature gate (verify `checksums.txt.minisig` before checksum/extract/swap) — `internal/cmd/update.go:145`
- `extractBinaryZip` (windows .zip extraction; tar.gz vs zip dispatch on asset suffix) — `internal/cmd/update.go:235`
- release signing + build matrix (`signs:` minisign, windows build, `brews:` tap) — `.goreleaser.yaml` / `.github/workflows/release.yml`
- `release-version.sh` (tag resolved from tracked `project.toml` `[project].version` with grep/sed — no dross binary, no jq) — `scripts/release-version.sh:16`
- `install.sh` (curl|sh bootstrap) / `install.ps1` (Windows PowerShell bootstrap, verify-before-place) — `install.sh` / `install.ps1`
- `make install` delegation + shellcheck CI gate — `Makefile` / `.github/workflows/ci.yml`

_introduced self-update-and-distribution · 0ccce6a_
_extended release-trust-and-distribution (minisign signing + verify-before-swap) · 46c091a_
_extended homebrew-and-windows-distribution (windows zip self-update + Homebrew tap + install.ps1) · 0007570_
_extended state-json-branch-safety (release tag from tracked project.toml, no state.json read in CI) · e3674ba_

### Session continuity & context hygiene

Survive `/clear` and compaction without losing the workflow thread: every durable-boundary prompt closes with a "state is on disk — safe to /clear" footer naming the exact re-entry command (enforced fail-closed by a footer-coverage gate over `docs/footer-audit.md`), `dross pause --auto` merges a mechanical snapshot (branch, dirty files, status, timestamp) into `.dross/handoff.md` without prompting, and `dross hooks ensure` (also run by init/onboard) idempotently wires user-level Claude Code hooks — PreCompact → `dross pause --auto`, SessionStart → `dross reentry` — that no-op outside dross repos and never disturb foreign settings.json entries. `/dross-execute` pair mode adds a post-commit continue/stop/checkpoint gate whose checkpoint path validates state then ends the session with the `/clear → /dross-execute --from <next-task>` re-entry.

- `hooks.MergeHook` (order-preserving idempotent settings.json merge; foreign entries survive verbatim) — `internal/hooks/settings.go:39`
- `Hooks` (`dross hooks ensure`) / `ensureUserHooks` (init/onboard wiring) — `internal/cmd/hooks.go:18`
- `Reentry` / `reentryLine` ("you are here + next", byte-equal to status's last line) — `internal/cmd/reentry.go:33`
- `Pause` (`dross pause --auto` mechanical snapshot merge) — `internal/cmd/pause.go:24`
- `pauseAuto` / `autoSnapshot` (silent on a non-root, loud on a corrupt one, degrades without git) — `internal/cmd/pause.go:45`
- `pause.md` §0 not-a-dross-repo gate — `assets/prompts/pause.md:18`
- `footerCoverage` (fail-closed clear-point footer gate) — `internal/cmd/footer_coverage.go:30`
- execute checkpoint gate (§1g continue/stop/checkpoint) — `assets/prompts/execute.md`
- clear-point footers across the durable-boundary prompts — `assets/prompts/spec.md`

Pause refuses rather than scaffolds. `/dross-pause` in a repo with no initialised root — absent *or* incomplete, one concept per the locked `pause_refusal` decision — writes nothing, creates neither `.dross/` nor `handoff.md`, prints one line naming why and pointing at the repair, and does not run the repair itself. `dross pause --auto` agrees at CLI level: silent exit 0 on a non-root, but an undecodable `project.toml`/`state.json` surfaces its error and leaves any pre-existing `handoff.md` byte-identical; a missing git only degrades the branch line.

_introduced context-hygiene · extended root-robustness · 6d33d3b_

### Ship recovery

Heal local-vs-origin base divergence after a squash-merge — a shared, delta-gated routine parameterized by the base branch (main or a `milestone/<version>` reconcile branch, no longer aborting on the latter), reused by two entry points and documented as a three-state cookbook. `dross ship recover` is the standalone legacy-repo healer; `dross phase complete --recover` performs its reset/heal *before* re-evaluating the merge gate, so the flag works in exactly the diverged/stale state its own error recommends it for — while merge verification still precedes the destructive reset (an unmerged PR refuses with the local base byte-unchanged) — and refuses with a pointer when the flag is absent. The shared `runDrossRecovery` resets the base to origin, restores the full cumulative `.dross/` tree (every phase's artefacts, not just the current one), commits only on a real delta — so an in-sync repo is a clean no-op with no phantom commit — and pushes the restore commit via the clean-tree safety-net policy so the heal doesn't itself re-seed divergence. The `ship.md` `## Recovery` section maps the three mid-merge failure states (ff-abort / diverged main / dirty post-push tree) each to a one-command fix, with no manual `.dross/` surgery (guarded by a prompt-presence test). Both entry points now aim at the phase's **recorded** base rather than an inferred one: `--recover` resets only that branch — a stale `milestone/<version>` sitting locally is left byte-unchanged — and sources the restored `.dross/` tree from the phase tip captured *before* the checkout, so the restore can never come from the branch the command just switched to; `ship recover` guards and resets the same recorded base, falling back to `git_main_branch` for the legacy phases that predate the record and did live on main.

- `runDrossRecovery` (shared delta-gated reset+restore+commit+push, base-branch parameterized) — `internal/cmd/ship_recover.go:159`
- `shipRecover` (standalone CLI entry, delegates to the shared routine) — `internal/cmd/ship_recover.go:32`
- `ship recover` recorded-base resolution (recorded base, else `git_main_branch` for legacy phases) — `internal/cmd/ship_recover.go:94`
- `phaseComplete` `--recover` (in-loop heal-before-gate; tree from the phase tip, reset scoped to the recorded base) — `internal/cmd/phase.go:218`

The restore explicitly **excludes** `.dross/state.json`: a pre-untrack commit still carries a copy, and replaying it would undo the live machine-local history the recovery is meant to preserve. The reset itself runs through the guarded primitives, so a base ref that still tracks the file refuses rather than clobbers — see [Branch-switch safety](#branch-switch-safety).

_52f6c75 · extended ship-complete-recovery-hardening · extended ship-clean-tree · extended complete-base-truth · extended state-json-branch-safety · 90dbedb_

### Shipping / pull requests

Push the phase branch and open a provider-aware PR/MR (GitHub/Forgejo/GitLab/Bitbucket) with reviewers, merging the phase's landmarks into ARCHITECTURE.md first — auto-backfilling the whole doc via the prompt-driven generation when it's absent, so an older repo self-heals on its next interactive ship (non-blocking; `--auto` skips it) — marks the phase `shipped` in the machine-local, gitignored `state.json` and **leaves `current_phase` set**, because a phase is not complete until its PR is merged and ship runs before that is known; `dross phase complete` is the sole writer of the completed-state transition (see [Phase lifecycle](#phase-lifecycle)). The `shipped <id>` breadcrumb is history-scan-guarded so a re-ship never doubles it, and ship returns on a clean tree; squash-merge collapses per-task commits. The GitLab path is raw REST (no `gh`/`glab` CLI): `openGitLabPR` opens a Merge Request (source/target branch, `Draft:` prefix, `web_url`→URL, `iid`→Number) and resolves reviewer usernames→ids non-fatally; `postGitLabComment` posts an MR note. The post-push PR/MR URL is intentionally printed, not persisted to state.json (avoids the completion-chore divergence); the PR *number*, however, is recorded per-phase in changes.json (`changes.SetPR`), then committed **and pushed** onto the phase branch — drag-proof, unlike cumulative history — so the squash-merge carries the record onto the base's changes.json where `phase complete`'s `mergeGate` reads it to authoritatively confirm the merge (the push is essential: a local-only record never reaches the PR/squash/base, which would leave `mergeGate` blind and refusing every squash-merged completion). The CI-watch + squash-merge steps are prompt-driven (ship.md §5/§6) with the locked GitLab pipeline-status mapping. A non-interactive fast-path makes ship callable from a script or loop: `dross ship --auto` requests zero reviewers for the run without mutating `remote.reviewers` (gating the narration + telemetry off `opts.Reviewers`) and keeps the generated body, while `--json` emits a single `{url, number, result}` object on stdout through a suppressible `narrate` closure — the two compose, and explicit `--body`/`--body-file`/`--draft` still win. `ship.md §0.5` skips the interactive body-preview/body-override/reviewer turns and shells to `dross ship --auto`, opening the PR and returning without driving the merge. Bitbucket Cloud is a real ship provider over HTTP Basic (`auth_scheme = basic` + `[remote].auth_user`, its app-password/API-token wire format): `openBitbucketPR` creates the PR from the nested source/destination branch payload and reads the URL back off `links.html.href`, `postBitbucketComment` posts a `content.raw` note, and `bitbucketPRStatus` reports the authoritative merged status from `state == "MERGED"` plus `destination.branch.name` as BaseRef — a status all 5 ship providers now answer authoritatively via `ship.GetPRStatus`, not just Bitbucket — every provider switch on the path normalises through `configenum`, so the set ship dispatches, the set doctor blesses and the set the init/onboard prompts tell an agent to write are one set. GitLab and Forgejo/Gitea complete the same pair of surfaces Bitbucket did: `gitlabPRStatus` / `forgejoPRStatus` complete the `GetPRStatus` dispatch (`state == merged` + `target_branch`→BaseRef for GitLab; a `merged` boolean, not `state`, plus `base.ref`→BaseRef for Forgejo/Gitea, since Gitea's `state` field reports misleadingly), and `gitlabOpenMRsTargeting` / `forgejoOpenPRsTargeting` complete `OpenPRsTargeting` (GitLab paginates via `per_page`/`page`; Forgejo/Gitea filters client-side by `base.ref` since Gitea has no `base=` query param) — so every ship provider now answers both merge-status and open-PRs-by-base authoritatively, not just GitHub/Bitbucket.

- `Ship` (CLI; `--auto` / `--json` non-interactive flags) — `internal/cmd/ship.go:76`
- `ship.OpenPR` (provider switch → github/forgejo/`openGitLabPR`/`openBitbucketPR`) — `internal/ship/open.go:49`
- `ship.PostComment` / `postGitLabComment` / `postBitbucketComment` — `internal/ship/comment.go`
- `openBitbucketPR` (Basic-auth PR creation, nested branch payload) — `internal/ship/bitbucket.go:171`
- `bitbucketPRStatus` (authoritative `state == MERGED` + `destination.branch.name` as BaseRef) — `internal/ship/bitbucket.go:112`
- `gitlabPRStatus` / `gitlabOpenMRsTargeting` (GitLab PRStatus + open-MRs-by-target parity) — `internal/ship/gitlab.go:101`, `internal/ship/gitlab.go:104`
- `forgejoPRStatus` / `forgejoOpenPRsTargeting` (Forgejo/Gitea PRStatus + open-PRs-by-base parity) — `internal/ship/forgejo.go:86`
- `buildOpenOpts` / `buildCommentOpts` (thread remote auth_scheme/project_id/auth_user) — `internal/cmd/ship.go:43`
- `changes.SetPR` (records opened PR number per-phase for the completion merge-gate) — `internal/changes/changes.go:141`
- `ship.BuildPRBody` — `internal/ship/body.go:20`
- verify-gate auto-heal (records a resolved-but-unrecorded verdict via `finalizeVerify` BEFORE the pass-only refusal — partial/fail recorded, then still refused) — `internal/cmd/ship.go:123`

Teardown ownership is explicit and single: the merge step **merges only**. No provider is asked to delete the source branch as part of it — no `--delete-branch`, no remove-source-branch field, no branch-removal call — because GitHub's delete-on-merge flag performs its own raw checkout of the base branch, a switch outside the guard, and that is what destroyed a live `state.json` on an earlier ship. `dross phase complete` performs both the local and the remote deletion on every provider, which also makes one behaviour of what was a per-provider split (Forgejo/GitLab/Bitbucket merge over REST and never switch branches). `ship.md` is pinned to that shape by a prompt guard rather than by convention.

- `TestShipPromptNoUnguardedSwitch` (ship.md merges only; no delete flag, no raw checkout) — `internal/cmd/ship_prompt_test.go:175`
- `TestShipPromptNamesCompleteAsTeardownOwner` (the prompt names complete as the teardown owner) — `internal/cmd/ship_prompt_test.go:246`

_introduced d392501 · extended 01-architecture-comprehension-layer · extended 02-harden-ship-merge-complete-flow · extended 03-fix-completion-chore-divergence · extended gitlab-ship-provider · extended ship-auto-noninteractive · extended verify-merge-before-completion · extended ship-architecture-autogen · extended pr-record-reaches-base · extended verify-auto-finalize · extended validator-truth · extended completion-state-truth · extended provider-merge-parity · 570229c_

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

Track current milestone/phase/version + activity in state.json; summarise "where am I" — including milestone phase-progress (N/M phases verified) and an idle-gated non-spine action surface (security/quality/tech-debt) that ranks areas by run signal (never-run first, then most-stale) and shows each area's last-run state, surfaced only when the spec→ship spine has nothing runnable left. A shipped-but-unmerged phase is reported as **shipped, not stale**: status names the open PR and the base it is waiting on, read from the state.json breadcrumb or the changes.json PR record alone (either signal suffices; neither present means no claim). The older stale-completion warning is gone with the state it described — once `dross ship` leaves `current_phase` set, a shipped-but-unmerged phase whose state reads `completed` is unreachable, so warning about it was warning about an impossible state. Status also prints the branch-topology line unconditionally (see [Branch topology reporting](#branch-topology-reporting)), so "is this milestone branch correct-by-design or stuck?" has a standing answer rather than one that scrolled away.

- `state.State` — `internal/state/state.go:17`
- `State` (CLI) — `internal/cmd/state.go:20`
- `Status` — `internal/cmd/status.go:24`
- `shippedUnmergedPhase` (reports a shipped phase and the base its PR waits on; replaced the stale-completion warning) — `internal/cmd/status.go:523`
- `spineIdle` — `internal/cmd/status.go:294`
- `rankAreas` — `internal/cmd/status.go:409`
- `formatRunSignal` — `internal/cmd/status.go:428`
- `renderActionAreas` — `internal/cmd/status.go:456`
- `stateTouch` (silent on a non-root, loud on a corrupt one) — `internal/cmd/state.go:123`
- `stateGet` (1+ dotted paths via the shared `renderMultiGet`) — `internal/cmd/state.go:55`
- `stateShow` (`--json` accepted; output byte-identical to bare `show`) — `internal/cmd/state.go:29`
- `TestStatusShippedFromPRRecordAlone` (the changes.json PR fallback with the state.json breadcrumb absent — the shape every machine but the shipping one has) — `internal/cmd/status_test.go:896`
- `TestStatusTopologyLineAlways` (topology line unconditional: mid-phase, between phases, no origin) — `internal/cmd/status_test.go:1007`

As hook targets, `dross status` and `dross state touch` exit 0 with no output in a repo that isn't a dross repo — see [Root resolution](#root-resolution) for what that means. The swallow is scoped to those two entry points, not pushed down into `loadState`: `dross state show` stays loud on a non-root, and a corrupt `state.json` is loud everywhere and names its path.

State is readable through the same dotted-path grammar as project and milestone config — `dross state get` takes one or more paths through the shared `renderMultiGet`, and `dross state show --json` is accepted (it already emitted JSON, so the flag is a no-op on output rather than a format conversion). See [Configuration](#configuration) for the shared read/write shape.

_c8b346e · extended 04-status-action-surfaces · extended status-action-surfaces-v2 · extended ship-complete-recovery-hardening · extended root-robustness · extended cli-surface-sweep · extended state-json-branch-safety · extended completion-state-truth · 1ecde33_

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

### Task lifecycle

List, add, remove, edit, and reposition tasks inside a phase's plan.toml through guarded CLI verbs, so the plan is readable and mutable through dross and never hand-edited. `dross task list` renders the plan as an aligned id/wave/status/title table for a human at a terminal or a `--json` array for a script (an empty plan emits `[]`, never `null`), with the phase-id argument optional and defaulting to `state.current_phase` — matching how `phase list` and `deferred list` already behave (locked `task_list_output`). New task ids come from a persisted per-plan high-water counter (Plan.TaskSeq): NextTaskID assigns high_water+1, and RemoveTask backfills the counter before deleting, so a freed id — even the highest — is never reissued; a new task's wave is the explicit --wave or one past its deepest dependency's wave (deriveWave). `add` appends at the tail by default and only positions relative to an anchor (--after/--before) when asked; `remove` is dependency-safe, refusing when another task depends on the target unless --force strips the id from every dependent; `edit` is a partial field update that changes only the flags passed and never status (dross task status stays that owner). `move` repositions a task relative to an `--before`/`--after` anchor: a move that would break dependency order is rejected with plan.toml untouched, a legal move adopts the anchor's wave and reflows transitive pending dependents (history stays frozen), ids stay stable across a move, and `task next` follows the new order because its same-wave tie-break is plan-array position rather than lexicographic id. Every mutation passes through saveIfValid → ValidatePlan (duplicate-id, unknown-depends_on, and covers→criterion parity with dross validate) and is written only if valid, leaving plan.toml byte-unchanged on rejection.

- `taskList` (`dross task list`, table or `--json`, defaults to current_phase) — `internal/cmd/task.go:39`
- `taskAdd` / `taskRemove` / `taskEdit` (CLI verbs) — `internal/cmd/task.go:225`
- `taskMove` (`dross task move --before/--after`, resolveAnchor + saveIfValid) — `internal/cmd/task.go:359`
- `saveIfValid` (validate-then-write guard) — `internal/cmd/task.go:396`
- `Plan.AddTask` / `Plan.RemoveTask` / `Plan.EditTask` (pure in-memory mutators) — `internal/phase/plan_edit.go:143`
- `Plan.MoveTask` (guarded reposition, anchor-wave adoption + dependent reflow) — `internal/phase/plan_edit.go:228`
- `Plan.NextRunnable` (same-wave tie-break by plan-array position) — `internal/phase/phase.go:303`
- `Plan.NextTaskID` / `deriveWave` (high-water id + dependency-derived wave) — `internal/phase/plan_edit.go:35`
- `ValidatePlan` (pre-write integrity guard) — `internal/phase/plan_edit.go:82`

_introduced task-lifecycle-commands · extended task-reordering · extended cli-surface-sweep · 949b375_

### Tech-debt scan (dross techdebt)

Dependency-free, language-agnostic tech-debt scan: TODO/FIXME/HACK/XXX markers (word-boundary) plus size heuristics (oversized files, over-long lines) over git-tracked files — `.dross/` bookkeeping is excluded on both the ls-files and tree-walk enumeration paths so generated planning artefacts don't drown code debt — written to a prune-proof run dir with a store-level `last_run` that feeds the status action surface. Distinct from the dross-quality analyzer audit — markers are self-flagged debt, not analyzer findings. Surfaced as the `/dross-techdebt` thin skill (shim + prompt over `dross techdebt`), so all three status actions are runnable slash commands.

- `Scan` — `internal/techdebt/scan.go:53`
- `NewRun` — `internal/techdebt/run.go:54`
- `StatePath` — `internal/techdebt/state.go:16`
- `Techdebt` (CLI) — `internal/cmd/techdebt.go:22`
- `trackedFiles` (.dross-excluded scan set, both enumeration paths) — `internal/cmd/techdebt.go:68`
- `findings.StampLastRun` — `internal/findings/state.go:121`
- `actionCatalog` (status actions all slash commands) — `internal/cmd/status.go:376`
- `/dross-techdebt` thin skill — `assets/prompts/techdebt.md`

_introduced status-action-surfaces-v2 · extended task-reordering · extended self-audit · 01e2adb_

### Telemetry & stats

Local-only event log (counts / durations / error classes, never file content), queryable via `dross stats`. Failures classify into named buckets — cobra arg-count / unknown-flag / unknown-subcommand, landmark-parse, merge-refusal, `.dross` config-load and missing-env-token — with an `err_detail` attached only for identifier-only shapes, so paths, phase ids and token names never reach the log. The taxonomy itself is an ordered first-match-wins rule table (`classRules`) arranged in tiers (root/state → workflow-specific → CLI surface → generic safety-net → other); order is enforced by a shadowing guard (an earlier matcher subsuming a later one fails the build), the generic tier prefers the more diagnostic bucket (permission > git > network > invalid > missing), and a table-driven doc test keeps the README bucket list complete. `dross stats` re-derives the class of stored `other` events at read time (the append-only log is never rewritten) and prints the top 5 surviving unclassified shapes as a graduation queue, omitted once drained. A checked-in corpus of real `err_detail` shapes pins each to its bucket and enforces an under-15% unclassified ceiling.

- `telemetry.Append` — `internal/telemetry/telemetry.go:83`
- `telemetry.ClassifyError` — `internal/telemetry/telemetry.go:217`
- `telemetry.ClassifyMessage` (walks the rule table) — `internal/telemetry/telemetry.go:229`
- `classRules` (ordered taxonomy — first-match-wins tiers as data) — `internal/telemetry/telemetry.go:269`
- `TestNoTokenShadowing` (order-dependence enforced) — `internal/telemetry/taxonomy_test.go:33`
- `TestReadmeDocumentsBucketTiers` (table-driven README completeness) — `internal/telemetry/telemetry_test.go:479`
- `telemetry.CarriesDetail` (detail allowlist) — `internal/telemetry/telemetry.go:427`
- `telemetry.Reclassify` (read-time re-derivation) — `internal/telemetry/telemetry.go:445`
- `renderErrorBuckets` — `internal/cmd/stats.go:371`
- `renderOtherTail` (graduation queue) — `internal/cmd/stats.go:390`
- `RecordCLIEvent` (repo hash via `LocateRoot`, so a half-built repo still attributes) — `internal/cmd/telemetry.go:23`
- `TestClassifyRealIncompleteRootError` (bucketing pinned to real `FindRoot` values, not a copied literal) — `internal/cmd/telemetry_test.go:283`
- `TestCorpusOtherShareUnderCeiling` (15% ceiling) — `internal/telemetry/corpus_test.go:97`
- `TestFrictionWindow_IncompleteRoot` (one regression pin per friction class in the 2026-07-15..28 window, asserting the message a user now gets) — `internal/cmd/friction_window_test.go:36`
- `renderOutcomes` (pending verify count = phases whose latest phase-stamped event is unresolved, not raw pending events; legacy phase-less events excluded) — `internal/cmd/stats.go:428`

The root tier carries its own `incomplete_root` bucket, ordered *above* `no_root`: a half-built `.dross/` is a different friction with a different fix (`dross onboard`, not `dross init`) and must not collapse into the absent-root case or the opaque `other` tail. Both recorders resolve the repo hash through `LocateRoot`, so events from an incomplete repo are still attributed rather than dropped. Because `internal/telemetry` is imported by `cmd`, it can only pin the bucket against a hand-copied message; the load-bearing test lives in `cmd` and classifies the error value `FindRoot` really returns — so rewording the message turns the test red instead of silently mis-bucketing real failures.

_a1b9c23 · extended telemetry-bucket-graduation · extended verify-auto-finalize · extended telemetry-taxonomy-overhaul · extended root-robustness · 6d33d3b · extended board-state-map-truth · 3272339_

### Test-suite hermeticity

Keep the repo's own test runs independent of the developer's machine, so a green suite means the code is green and not that the host happened to be configured favourably. The `internal/cmd` package pins `HOME` to a throwaway directory for the whole package via `TestMain`, so no test inherits `~/.claude/dross/defaults.toml` or a stray provider token — the failure mode this closes had `internal/cmd` reddening (and being skipped by the mutation run) purely from a host global default. A hostile-globals test reproduces the old failure deterministically rather than trusting the pin by inspection.

- `TestMain` (package-wide HOME pin) — `internal/cmd/hermetic_env_test.go:35`
- `TestHermeticHome_HostileGlobalDefaultsDoNotRedden` (deterministic repro of the host-leak failure) — `internal/cmd/hermetic_env_test.go:78`
- `snapshotLiveState` (force-stages state.json and restores the live copy across a fixture's branch switches, so squash-merge fixtures hold once the file is gitignored) — `internal/cmd/phase_test.go:145`

_introduced board-state-map-truth · extended state-json-branch-safety · 47f383b_

### Verification

Map acceptance criteria to tests and run mutation testing; decide pass/partial/fail. An adapter failure records `LanguageRun.Error` plus a FLAG finding and continues — other language legs' reports are never discarded — and a `[mutation] adapters` allowlist filters adapters by name, with filtered files falling to Skipped rather than silently passing. A resolved verdict is recorded to telemetry exactly once: `finalizeVerify` writes a `finalized = true` marker back into verify.toml as the idempotency guard (works under telemetry opt-out and log rotation; a re-run reports "already recorded", exit 0), stamps verify pending/outcome events with the phase id, and backs the auto-heal in the ship / phase-complete gates so a resolved-but-unrecorded verdict can't sit unfinalized.

- `Verify` (CLI) — `internal/cmd/verify.go:29`
- `verify.Run` — `internal/verify/verify.go:137`
- `LanguageRun.Error` (record-and-continue adapter failure) — `internal/verify/verify.go:54`
- `configuredAdapters` (`[mutation] adapters` allowlist) — `internal/cmd/verify.go:191`
- `finalizeVerify` (idempotent finalize core — finalized marker + phase-stamped events) — `internal/cmd/verify.go:153`
- verify.md §2 size-gated offload (large criterion-mapping reads fan to read-only subagents; judgement + verdict stay main-loop) — `internal/cmd/verify_prompt_test.go:17`

_e31bdbd · extended context-hygiene · extended verify-auto-finalize · extended subagent-offload-audit · 5c21b79_

### Watch heartbeat (dross-watch)

Read-only `/loop` heartbeat: `dross watch --json` surfaces board issues new-since-last-tick vs carried (an atomically-persisted seen-set diff keyed on id + open/closed state) plus the current milestone's drifting phases, and ends with exactly one ranked suggested command. A board that is off or unreachable degrades to a drift-only digest; the only thing a run ever writes is `.dross/watch.state.json`.

- `Watch` (`dross watch --json`) — `internal/cmd/watch.go:27`
- `suggestedCommand` (ranked verify→ship→inbox→status) — `internal/cmd/watch.go:115`
- `watch.State.Diff` (new/carried seen-set delta) — `internal/watch/watch.go:85`
- `watch.ClassifyDrift` (milestone-scoped phase drift) — `internal/watch/drift.go:42`
- `collectInbound` (mark-free inbound filter, shared with `issue pull`) — `internal/cmd/issue.go:646`
- `/dross-watch` prompt (non-interactive broadcast) — `assets/prompts/watch.md:1`

_introduced dross-watch · 5694cf5_
