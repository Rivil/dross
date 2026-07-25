# Dross

A leaner successor to [GSD](https://github.com/gsd-build/get-shit-done) for working with Claude Code on real projects.

> **Status:** v0.10.x — the full plan → execute → verify → ship loop runs on phase-branch isolation (`dross phase create` auto-checks out `phase/<id>`; `dross phase complete` finalizes post-merge), with a milestone-branch topology layered on top (phase PRs squash-merge into `milestone/<version>`; the milestone lands in `main` as a merge commit). Mutation testing covers TS/JS/Svelte (Stryker) and Go (Gremlins); C# (Stryker.NET) is fixture-tested but undogfooded. Every interactive command is a propose-and-react conversation — one decision per turn — under the `dross-interaction-contract` rule (see [Interaction](#interaction)). Since v0.3 the loop has grown considerably: slug-based phase identity with `dross phase {insert,move,rename}` lifecycle verbs (v0.4) and `dross task {add,remove,edit}` plan-editing verbs (v0.10); deferred-item routing and triage across specs (v0.5); container/IaC scanning, a GitLab ship provider, an ARCHITECTURE.md comprehension layer, and self-update via `dross update` (v0.6); non-interactive `dross ship --auto` and the milestone-branch model (v0.7); a native Claude Code statusline (v0.8); and release trust — minisign-signed releases with verify-before-swap self-update, `/dross-watch` (read-only board + phase-drift digest), and additional board backends (v0.9). Opt-in Forgejo/Gitea issue-board sync (milestones/phases/quicks → board issues, inbound triage via `/dross-inbox`) lives behind `dross issue enable`; `/dross-pause` + `/dross-resume` capture and replay a mid-phase handoff; `/dross-plan` auto-runs an independent plan review (`--no-review` to skip) and offers `--panel` — a 3-lens planner panel merged by a cold judge; and context-free `/dross-secure` / `/dross-quality` audits (real scanners/analyzers + adversarial refute-panels) scaffold a remediation phase. See the per-milestone log under [Roadmap](#roadmap) for the full arc.

> Scope: Dross is built for my workflow. It's public because there's no reason not to be, but I'm not marketing it and I'm not trying to grow it into a general-purpose tool. The roadmap is a flat list because my todo list is — if Dross ever picks up users, I'll think about structure (semver, milestones, contribution guidelines) then.

> Contributing: I'm unlikely to accept feature PRs that don't match how I personally use this. Bug fixes and small quality-of-life improvements are welcome; new features probably aren't, unless we've talked first. If Dross is almost what you want but not quite, fork it — that's what AGPL is for, and you'll move faster owning your own copy than waiting on me.

## Why

Dross is built around three design pivots:

1. **Lean prompts.** Target ≤300 lines per slash command, with most state in machine-parseable TOML rather than prose Markdown — so a command boots cheaply and subagent spawns stay bounded and explicit.
2. **Pair-mode execute by default.** Code is authored *with* you, not delivered *to* you. Subagent spawns are kept to genuinely independent work (parallel mutation runs, multi-language audits).
3. **Test efficacy as a first-class gate.** It's not enough that tests *exist* — dross checks that tests *catch breakage*, via mutation testing (Stryker / Gremlins), coverage delta, and an LLM judge mapping each acceptance criterion to a specific test.

## The name

Dross is the AI sidekick from Will Wight's [Cradle](https://www.willwight.com/cradle) series — a Presence that lives in the protagonist's head, compiling battle plans, predicting opponents, crafting illusions, and handling "unimportant thoughts" to free up his bandwidth. Sarcastic, dramatic, fond of his person.

## Concept

```
intent ─► SPEC ─► PLAN ─► CODE ─► TESTS ─► EFFICACY PROOF ─► VERIFY
         (lock)  (waves)  (atomic   (per     (mutation +      (goal-
                          commit)   task)    coverage)        backward)
```

## Interaction

Every interactive dross command runs as a **conversation, not a broadcast**. The
contract is **propose-and-react, one decision per turn**: a command surfaces a
single decision, proposes the default it would pick, and lets you accept or steer
— never a wall of batched questions, and never a composed artifact (a spec, a
plan, a config) dumped back for blanket approval. Written artifacts are confirmed
with a one-line summary, not pasted in full.

So `/dross-spec` walks acceptance criteria one at a time rather than asking for all
seven at once:

```
spec › c-3  "returns 401 when the token is missing"
        accept · reword · drop ?            ‹ you pick, then it moves to c-4
```

That's the whole loop — you stay in it, steering as you go, instead of reviewing a
finished blob at the end. The invariant is the `dross-interaction-contract` rule
(`dross rule show`); the how-to playbook lives in `assets/prompts/_interaction.md`
and is delivered to each command verbatim via `dross interaction show`.

## Layout

```
cmd/dross/         Go CLI entrypoint
internal/          project, state, rules, profile, phase, milestone, changes, verify, mutation, codex, architecture, security, quality, stack, board
assets/commands/   Slash command markdown (installed to ~/.claude/skills/dross-<name>/SKILL.md)
assets/prompts/    Prompt instructions (installed to ~/.claude/dross/prompts/)
docs/dross.1       Man page — `man ./docs/dross.1`; print via `mandoc -T pdf docs/dross.1 > dross.pdf`
```

### Per-project artefacts

```
.dross/
├── project.toml      # vision, stack, runtime, paths, env, goals
├── rules.toml        # project-scoped rules (additive to global)
├── state.json        # current position, version, last activity
├── profile.toml      # optional project-specific profile overrides
├── milestones/
└── phases/
    └── <slug>/           # bare-slug identity (v0.4); ordering lives in the milestone's phases array
        ├── spec.toml
        ├── plan.toml
        ├── changes.json   # auto, written during execute
        ├── tests.json     # auto, written during verify
        └── verify.toml    # auto, written during verify
```

### Global install layout (after `make install`)

```
~/.local/bin/dross                     # CLI binary

~/.claude/skills/                      # one skill dir per slash command
├── dross-init/SKILL.md                # symlink → assets/commands/dross-init.md
├── dross-onboard/SKILL.md
├── dross-milestone/SKILL.md
├── dross-spec/SKILL.md
├── dross-plan/SKILL.md
├── dross-plan-review/SKILL.md
├── dross-execute/SKILL.md
├── dross-verify/SKILL.md
├── dross-quick/SKILL.md
├── dross-ship/SKILL.md
├── dross-review/SKILL.md
├── dross-secure/SKILL.md
├── dross-quality/SKILL.md
├── dross-architecture/SKILL.md
├── dross-inbox/SKILL.md
├── dross-pause/SKILL.md
├── dross-resume/SKILL.md
├── dross-status/SKILL.md
├── dross-options/SKILL.md
└── dross-rule/SKILL.md

~/.claude/dross/
├── defaults.toml                      # cross-project pre-fills + telemetry toggle
├── profile.toml                       # cross-project user profile (planned, not yet wired)
├── rules.toml                         # cross-project rules
├── telemetry.jsonl                    # local-only event log (see Telemetry section)
└── prompts/                           # symlink → assets/prompts/
```

Symlinks mean edits to `assets/` in the dross repo apply immediately — no re-install on prompt tweaks.

## Install

### Quick install (recommended)

```sh
curl -fsSL https://raw.githubusercontent.com/Rivil/dross/main/install.sh | sh
```

This downloads the latest release binary for your platform (`darwin`/`linux` × `arm64`/`amd64`), verifies its SHA-256 against the release `checksums.txt`, drops `dross` on your PATH (`~/.local/bin`), and runs `dross install` to materialize the slash commands and prompts into `~/.claude`. No Go toolchain or git checkout required.

If `~/.local/bin` isn't on your PATH, add it:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Then in any Claude Code session, `/dross-init` (greenfield) or `/dross-onboard` (existing repo).

### Windows

```powershell
irm https://raw.githubusercontent.com/Rivil/dross/main/install.ps1 | iex
```

`install.ps1` is the PowerShell analog of `install.sh`: it detects your architecture (`amd64`/`arm64`), downloads the latest Windows release `.zip`, verifies its SHA-256 against `checksums.txt` **before** placing anything on PATH, extracts `dross.exe` into `%USERPROFILE%\.local\bin` (overridable via `DROSS_BIN_DIR`), adds that dir to your user PATH, and runs `dross install`. Prefer a manual download? Grab `dross_<version>_windows_<arch>.zip` from [releases](https://github.com/Rivil/dross/releases), verify it against `checksums.txt`, extract `dross.exe` onto your PATH, then run `dross install`.

Once installed, `dross update` self-updates on Windows too (it fetches the signed Windows `.zip`, verifies signature + checksum, and atomically swaps `dross.exe`).

> The self-update, archive extraction, and SHA-256 verification are unit-tested in Go, but `install.ps1` and a real end-to-end Windows binary run have not been exercised on a Windows host in this repo — treat the first Windows run as maintainer-verified, not CI-verified.

### Updating

```sh
dross update          # update to the latest release, if it is newer
dross update --check  # report the available version without applying
dross update --force  # reinstall the latest regardless of version
```

`dross update` fetches the latest GitHub release, verifies the tarball's SHA-256 against `checksums.txt` (refusing on mismatch), atomically replaces the running binary, then re-syncs the embedded slash commands + prompts.

### Manual binary download

GoReleaser publishes archives for `darwin/arm64` (primary), `darwin/amd64`, `linux/arm64`, `linux/amd64`, and `windows/arm64`+`windows/amd64` on every `v*` tag. Grab the matching archive from [releases](https://github.com/Rivil/dross/releases) — `.tar.gz` on macOS/Linux, `.zip` on Windows — extract, drop the `dross` binary (`dross.exe` on Windows) on your PATH, then run `dross install` to set up the slash commands and prompts.

### From source

```sh
make build       # builds ./dross for current arch (with commit + build date in `dross version`)
make test        # go test -count=1 ./...
make install     # builds + installs binary, then `dross install --link` (symlinks slash commands & prompts)
make doctor      # verifies install: PATH, binary freshness, symlink targets — exits non-zero on any issue
make uninstall   # removes binary, all dross-* skills, and the prompts symlink
make release-snapshot  # local goreleaser dry-run — produces dist/, never tags or pushes
```

After `make install`, ensure `~/.local/bin` is on your PATH:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

## Available commands

| Command | Description | Status |
|---|---|:---:|
| `dross init` | Bootstrap `.dross/` (greenfield) | ✅ |
| `dross onboard` | Adopt an existing repo (signal scan) | ✅ |
| `dross project {show,get,set}` | Read/write `project.toml` fields | ✅ |
| `dross state {show,set,touch,bump}` | Read/write `state.json` (`bump internal` increments the 4th version segment) | ✅ |
| `dross rule {add,list,remove,promote,disable,enable,show}` | Two-tier rules system | ✅ |
| `dross phase {create,list,show,complete,insert,move,rename,number,migrate}` | Phase directories + slug-based lifecycle. `create` auto-checks out a `phase/<id>` branch off the base so phase work never lands on main; `complete` finalizes after squash-merge (ff base, delete local `phase/<id>`); `insert`/`move`/`rename` edit a milestone's phase array and on-disk identity; `number` prints a phase's ordinal; `migrate` converts legacy NN-slug phases to bare slugs | ✅ |
| `dross milestone {create,list,show,get,set,add}` | Milestones with dotted-path edits (set scalars, add to list fields) | ✅ |
| `dross task {next,show,status,add,remove,edit}` | Inspect and edit tasks within a plan. `add` appends or inserts (`--after`/`--before`) a task with a fresh high-water id; `remove` is dependency-safe (`--force` strips dependents); `edit` is a partial field update. `plan.toml` is mutated only through these verbs, never hand-edited | ✅ |
| `dross changes {record,show}` | Per-phase append-only log of what was touched | ✅ |
| `dross verify <phase>` | Run mutation tests + write tests.json + verify.toml skeleton. `[summary].mutation_status` is `measured` / `unmeasurable` / `skipped` so `/dross-verify` can tell a real low score from a 0/0 artefact (e.g. project Stryker scope excludes every touched file) and skip the score thresholds when there's nothing to measure | ✅ |
| `dross verify finalize <phase>` | Record resolved verdict from verify.toml as a telemetry outcome event (after `/dross-verify`) | ✅ |
| `dross status` | Where am I — project, phase, last activity, suggested next step | ✅ |
| `dross profile {show,seed}` | User profile (with GSD import) | ✅ |
| `dross validate` | Schema-check every artefact | ✅ |
| `dross codex <file>` | Polyglot code insight — symbols, refs, siblings, recent activity. Go via stdlib `go/ast`; TS/TSX/Svelte/C#/GDScript via `ast-grep` shell-out (graceful no-op if ast-grep not on PATH) | ✅ |
| `dross security {detect,run,scaffold}` | Deterministic surface of the `dross-secure` audit — run dirs, scanner detection, findings→spec scaffold (audit orchestration lives in `secure.md`) | ✅ |
| `dross quality {detect,run,scaffold}` | Deterministic surface of the `dross-quality` audit — run dirs, analyzer detection, maintainability-risk scaffold (orchestration in `quality.md`) | ✅ |
| `dross stack {detect,show,list,apply,loadout}` | Stack profiles — detect the stack, show/list profiles, `apply` re-syncs `[runtime]`, `loadout` emits the agent loadout block. Embedded built-ins + `~/.claude/dross/profiles/` drop-ins (user wins). Go/TypeScript/Kotlin/Dart/Svelte/SQL profiles ship, plus a marker-file Docker profile | ✅ |
| `dross doctor` | Project-level health check: foundational files exist (`project.toml`, `rules.toml`, `state.json`), `[remote]` ↔ git origin matches, `auth_env` exported, `.gitattributes` marks `.dross/` linguist-generated, no phase commits leaked onto main | ✅ |
| `dross defaults {show,save}` | Read/write `~/.claude/dross/defaults.toml` (cross-project pre-fills) | ✅ |
| `dross env {list,set,unset}` | Manage env keys in `~/.claude/settings.json` (hidden input, never echoed) | ✅ |
| `dross ship <phase-id>` | Push `phase/<id>` to the provider, open PR, request reviewers. Provider's squash-merge collapses per-task commits | ✅ |
| `dross ship comment` | Post a markdown comment to a PR via provider (used by /dross-review) | ✅ |
| `dross ship recover` | One-shot migration tool for legacy repos with phase commits on main or `.dross/` stripped from prior PRs — fetch + reset + restore `.dross/` + commit, atomically | ✅ |
| `dross issue {enable,disable,milestone-sync,phase-sync,quick,pull,dismiss,link,list}` | Opt-in Forgejo/Gitea issue-board sync. Mirrors milestones/phases/quicks → board issues (idempotent), pulls inbound issues for triage. Off by default; `enable` needs `[remote].provider` (forgejo/gitea) + `api_base` + `auth_env` | ✅ |
| `dross stats {show,path,opt-in,opt-out}` | Aggregates over the local telemetry log; toggle the recorder | ✅ |
| `dross architecture check` | Inspect/repair `ARCHITECTURE.md` symbol links — reports drift, `--fix` re-resolves `file:line` targets | ✅ |
| `dross deferred {list,route,unroute,dismiss}` | Inspect and route deferred items across phase specs | ✅ |
| `dross techdebt` | Scan tracked files for tech-debt markers + size heuristics (`.dross/techdebt/<id>`) | ✅ |
| `dross watch` | Read-only digest of board inbound + phase drift since the last tick (backs `/dross-watch`) | ✅ |
| `dross statusline` | Render the Claude Code status line (reads status JSON on stdin) | ✅ |
| `dross update {--check,--force}` | Self-update to the latest signed GitHub release — verifies minisign signature + SHA-256 before atomically swapping the binary, then re-syncs commands/prompts | ✅ |
| `dross version` | Print version, commit, and build date | ✅ |

**Slash commands:**

| Command | Status |
|---|:---:|
| `/dross-init` | ✅ |
| `/dross-onboard` | ✅ |
| `/dross-rule` | ✅ |
| `/dross-milestone` | ✅ |
| `/dross-spec` | ✅ |
| `/dross-plan` | ✅ (`--panel` for 3-lens planner panel + cold judge; auto-runs plan review unless `--no-review`) |
| `/dross-plan-review` | ✅ (own context — cold subagent; also auto-run by `/dross-plan`) |
| `/dross-execute` | ✅ |
| `/dross-verify` | ✅ |
| `/dross-quick` | ✅ (one-shot task with atomic commit + test gate; bumps internal version) |
| `/dross-status` | ✅ |
| `/dross-pause` | ✅ (capture a handoff before stopping — thread + next action + open loops) |
| `/dross-resume` | ✅ (replay the handoff, prune what's done) |
| `/dross-inbox` | ✅ (triage inbound board issues → phase / milestone / quick / dismiss) |
| `/dross-watch` | ✅ (read-only heartbeat — board inbound + phase-drift digest, ends with one suggested next command) |
| `/dross-options` | ✅ |
| `/dross-ship` | ✅ (CI watch + merge gate + branch cleanup) |
| `/dross-review` | ✅ (4-lens subagent panel: security / quality / tests / spec-fidelity) |
| `/dross-secure` | ✅ (context-free multi-pass security audit: real scanners + adversarial refute-panel; scaffolds a remediation phase) |
| `/dross-quality` | ✅ (multi-pass code-quality audit: real analyzers + refute-panel over substantive maintainability dimensions; scaffolds a remediation phase) |
| `/dross-architecture` | ✅ (generate/refresh the feature-organized `ARCHITECTURE.md` from a scan of code + git history) |

Legend: ✅ working · 🚧 stub / partial · ⏳ not started

## Roadmap

- [x] Skeleton: types, CLI, rules system, init/onboard, validate
- [x] Tests: round-trip, merge, parser, validate checks
- [x] `/dross-spec` and `/dross-plan` slash commands
- [x] `/dross-execute` (pair-mode default, `--solo` opt-in) + task/changes CLI helpers
- [x] `/dross-verify` + Stryker adapter for TS/JS/Svelte mutation testing
- [x] Gremlins adapter for Go mutation testing
- [x] GoReleaser cross-compile (darwin/arm64 primary, +amd64, linux arm64/amd64) on `v*` tags
- [x] `[remote]` capture in init/onboard with two-tier defaults (Forgejo / GitHub / Gitea / Bitbucket)
- [x] `/dross-options` full settings editor + secret-safe `dross env` for `~/.claude/settings.json`
- [x] `/dross-ship` — squash + filter `.dross/`, provider-aware PR open (GitHub + Forgejo), human reviewer assignment
- [x] `/dross-ship` CI watch + merge gate + branch cleanup — watches provider checks, fixes failures, prompts to merge, deletes remote and local PR branches
- [x] `/dross-milestone` — slash command + `dross milestone {get,set,add}` dotted-path edits, Brief.md-aware scoping
- [x] `/dross-spec` smart no-args routing — offers to scaffold the next phase when nothing else is in flight
- [x] `dross stats` + local-only telemetry — single-developer event log to surface friction, opt-out via `dross stats opt-out` or `DROSS_NO_TELEMETRY=1`
- [x] Builtin `.dross/` commit-hygiene rule baked into every prompt's pre-flight
- [x] Ship filter rewrite — runs in an ephemeral worktree so the user's gitignored `.dross/` is never destroyed
- [x] Ship `--preserve-history` — alternative filter that keeps per-task commits, `.dross/` stripped from each
- [x] `/dross-review` four-lens subagent panel — spawns security / code-quality / test-efficacy / spec-fidelity reviewers in parallel and posts an aggregated comment to the PR
- [x] Mutation adapter: Stryker.NET (C#) — modeled from public Stryker.NET docs, JSON shape shared with Stryker.JS, fixture-tested; real-world verify pending a C# project to dogfood against
- [x] Codex polyglot indexer — Go via stdlib `go/ast`, TS/TSX/Svelte/C#/GDScript via `ast-grep` shell-out. Graceful degradation when ast-grep isn't installed (other commands keep working). HTML/CSS get sibling + git-log enrichment only (no symbols)
- [x] `/dross-quick` — one-shot task with atomic commit + `runtime.test_command` gate, pair-mode only. Bumps `state.version`'s internal counter (`dross state bump internal`). Works inside a phase (recorded as `quick-N` in `changes.json`) or standalone
- [x] Telemetry signal upgrades — finer error classifier (no_phase / no_spec / no_plan / verify_state / mutation / provider / unknown_field / cli_args / cancelled / check_issues), cmd path captured even when cobra fails to resolve, `dross status` surfaces unfinalized verify verdicts, doctor emits outcome events instead of bucketing as `err=other`
- [x] Issue-board sync (opt-in) — `dross issue {enable,milestone-sync,phase-sync,quick,pull,dismiss,link}` mirrors dross planning onto a Forgejo/Gitea board: milestone → board milestone, phase → issue (with a task checklist rendered from `plan.toml`), quick → standalone issue. Status flows via a `dross` marker + `dross/status:*` label and closes on ship. Outbound push is wired into the milestone/plan/execute/verify/ship/quick prompts as a no-op-when-disabled CLI call; inbound bugs/feature-requests are pulled by `/dross-inbox` (and surfaced as a passive count in `/dross-status`) and triaged into a phase / milestone backlog / quick / dismiss. Links live in `.dross/board.json`; reuses the `ship` provider config (`api_base`/`auth_env`). GitHub issues backend deferred (`gh issue`)
- [x] Phase-branch model — `dross phase create` auto-checks out `phase/<id>` off main; `dross phase complete` ff-merges main and deletes the local branch; `dross ship` pushes `phase/<id>` directly (no synthetic squash) and the provider's squash-merge collapses per-task commits on merge. Removes the divergence pattern that previously required manual recovery commits. `.dross/** linguist-generated=true` scaffolded into `.gitattributes` on init/onboard so review UIs collapse planning artefacts without filtering them from history. Doctor checks foundational files, the linguist attr, and phase commits leaked onto main. `dross ship recover` retained as one-shot migration for repos already in the divergent state.
- [x] Handoff pause/resume — `/dross-pause` drafts a living handoff at `.dross/handoff.md` (thread + next action + open loops, gitignored, single file), `/dross-resume` replays it and prunes done items in place, and `dross status` nudges when one is open. Closes the "stop mid-phase, next session the brain blanks out" gap that mechanical state (`current_phase`, task progress) doesn't cover
- [x] Verify verdict hardening — `[summary].mutation_status` (`measured | unmeasurable | skipped`) distinguishes a real low score from a 0/0 artefact, so a phase whose changes fall entirely outside the project's Stryker scope (or runs with `--skip-mutation`) no longer false-fails the 0.60 threshold; `/dross-verify` now bases the verdict on criterion coverage alone when nothing was measurable. Forgejo/Gitea `dross issue phase-sync` no longer spams `cannot unmarshal array into ... issueResponse` — the labels-PUT response is now correctly treated as a `LabelList` instead of an issue. New `no_milestone` error bucket peels bare `dross milestone show` failures out of the opaque `other` pile.
- [x] Plan quality loops — `/dross-plan --panel` fans out three cold lens planners (risk-first / MVP-first / verification-first) over the locked spec in parallel, a fourth cold judge merges them (winner-as-skeleton + grafts) and surfaces lens *disagreements* as the steering agenda instead of auto-resolving them; artifacts kept in `.dross/phases/<id>/panel/`. `/dross-plan` now auto-runs the independent plan review (own-context cold subagent) after `plan.toml` is locked, with one bounded fix-and-re-review cycle on blocking findings; `--no-review` opts out. Panel costs ~4-5× a single-pass plan — meant for new subsystems / non-obvious task graphs, not 2-task UI phases
- [x] Spec gray-area discussion — `/dross-spec`'s locked-decisions step is no longer a passive "any decisions?" prompt. It analyses the phase against project goals, milestone constraints, locked stack, and the acceptance criteria, then surfaces 3–4 *phase-specific* gray areas (concrete labels, never generic categories), lets the user `multiSelect` which to pin down, and deep-dives each one at a time — outcomes land as `[[decisions]]` (locked, with a real `why`) or `[[deferred]]`. Skips anything already settled by `stack.locked` or a prior phase's decision; routes scope-creep into deferred ideas. Ported from GSD's `discuss-phase` question phase, folded into the existing spec flow rather than added as a separate command/artifact

### Milestone v0.1 — comprehension, security & quality surfaces (complete)

- [x] `ARCHITECTURE.md` comprehension layer — a single feature-organized doc at repo root (one entry per capability, never per phase/module) with a fixed entry template (heading + one-line + symbol links + provenance). Seeded greenfield at `dross init`, backfilled by `/dross-architecture` from a code + git-history scan, and kept current by `/dross-ship` folding each phase's landmarks into the matching feature entry in place
- [x] `dross status` non-spine action surfaces — when the spec→ship spine has nothing runnable, status surfaces idle-gated action areas (security / quality / tech-debt) instead of dead-ending; gated so it only shows between phases, not mid-flow
- [x] `/dross-secure` + `dross security` — context-free, read-only multi-pass security audit: real scanners (govulncheck/gosec/gitleaks/semgrep/trivy/osv-scanner…) plus an adversarial refute-panel over cold subagents, emitting a verified findings ledger that scaffolds a remediation phase. Tool-grounded (no LLM-guessed findings), no `--fix`
- [x] `/dross-quality` + `dross quality` — comparable multi-pass code-quality audit: real analyzers (gocyclo/dupl/deadcode/errcheck/ineffassign + agnostic scc/jscpd) over substantive maintainability dimensions, refute-panel verified, scaffolds a remediation phase. Diverges from secure on a downrank-only (never-suppress) context model
- [x] Stack profiles + `dross stack` — declarative per-stack profiles (embedded built-ins + `~/.claude/dross/profiles/` drop-ins, user wins on id) that tune runtime commands, the security/quality tool loadout, and the agent loadout to a detected stack. Signal-scored detection → matched id or `unsupported`; `apply` re-syncs `[runtime]`; `loadout` emits a markdown block the execute prompt injects inline. Adding a stack is a single TOML drop-in. Go-first

### Milestone v0.2 — multi-language stack profiles (complete)

- [x] Embedded profiles for Kotlin / Dart / Svelte / SQL + `extLang` detection additions, each feeding its dedicated scanners/analyzers into `dross-secure` / `dross-quality`
- [x] Marker-file stack detection (`Dockerfile` / compose) so a Docker profile's tools run on repos with no source extension

### Milestone v0.3 — conversational command UX across the dross loop (complete)

- [x] Interaction contract — a single `dross-interaction-contract` rule ("propose-and-react, one decision per turn") plus a reusable prompt snippet at `assets/prompts/_interaction.md`, delivered to each command verbatim via `dross interaction show`; documented as a first-class behaviour in the README
- [x] Core-loop retrofit — `/dross-spec`, `/dross-plan`, `/dross-execute`, `/dross-verify` surface decisions one at a time with a proposed default, never batching unrelated questions or dumping a composed artefact for blanket approval
- [x] Setup-command retrofit — `/dross-init`, `/dross-onboard`, `/dross-options`, `/dross-milestone`, `/dross-quick`, `/dross-inbox`, `/dross-rule` rewritten to the same propose-and-react choreography
- [x] Audit + README — a grep-verifiable audit checklist maps every interactive command to its decision points and confirms the pattern; the interaction model is documented in the README

### Milestone v0.4 — phase identity & lifecycle (complete)

- [x] Stable slug phase ids — phase identity is now the bare slug (`phases/auth`), not an `NN-slug` ordinal. Ordering moved to the milestone's `phases` array, so reordering no longer renames directories or rewrites ids. `dross phase migrate` converts legacy `NN-slug` phases in place, with back-compat resolution for old ids
- [x] Phase-lifecycle commands — `dross phase {insert,move,rename}` (plus `number`) edit a milestone's phase array and on-disk identity as first-class operations: `insert` scaffolds a phase at a position, `move` reorders within the array, `rename` retargets the directory + spec id + milestone entry + deferred targets + branch, all guarded against shipped-phase collisions

### Milestone v0.5 — spec & backlog flow (complete)

- [x] Deferred-item routing — deferred ideas carry a `target` (a phase slug they should re-surface in) or stay "someday"; `dross deferred` lists and routes them, and `dross validate` refuses a dangling target
- [x] Deferred-triage gaps — a `dismissed` state retires an item as wontfix/done (distinct from "someday" and "routed"), `dross deferred unroute`/`dismiss` complete the lifecycle, and the spec gray-area walkthrough routes scope-creep into deferred items instead of losing it

### Milestone v0.6 — surface depth & loop hardening (active)

- [x] Multi-language analyzer catalogs + deepened container/IaC scanning — per-stack scanner/analyzer loadouts feeding `dross-secure` / `dross-quality`, with Dockerfile/compose/IaC surfaces (hadolint/trivy/checkov-class tools) wired in
- [x] Secure/quality findings lifecycle — audit findings persist with a state machine (open → fixed/accepted/false-positive) across re-runs instead of re-surfacing every pass
- [x] Ship/complete recovery hardening — `dross ship recover` + `dross phase complete --recover` heal the diverged-main and dirty-tree failure states with one command instead of manual `.dross/` surgery
- [x] ARCHITECTURE.md enhancements + comprehension layer — richer feature entries and a scan-driven backfill/refresh (`dross architecture check` catches symbol-link drift)
- [x] GitLab ship provider + YouTrack board backend — a third PR provider and a third issue-board backend alongside GitHub and Forgejo/Gitea
- [x] Self-update & distribution — `dross update` fetches, verifies, and atomically swaps the running binary, then re-syncs commands/prompts
- [x] Gray-area walkthrough — the spec locked-decisions step deep-dives phase-specific gray areas one at a time into `[[decisions]]` / `[[deferred]]`

### Milestone v0.7 — branch topology & non-interactive ship (active)

- [x] `dross ship --auto` — a non-interactive fast-path (skips body preview / reviewers / merge gate) suitable for scripts and loops; `--json` emits `{url, number, result}`
- [x] Milestone-branch model — phase PRs squash-merge into `milestone/<version>` when a milestone is active; the milestone itself lands in `main` as a merge commit (not a squash) so `main` keeps per-phase history, finalized by `dross milestone complete`
- [x] Interaction defer-or-add framing — borderline candidates are surfaced as a single either/or that leads with "defer it" and offers "add to current phase", standardized across spec/plan

### Milestone v0.8 — Claude Code surface integration (complete)

- [x] Native statusline — `dross statusline` renders the Claude Code status line from status JSON on stdin (project, milestone, phase, progress), installed via `dross install`

### Milestone v0.9 — backlog burn-down: trust, findings & integrations (active)

- [x] Release trust & distribution — minisign-signed releases with verify-before-swap in `dross update` (public key embedded; signing key + password in CI secrets); Homebrew + Windows distribution paths
- [x] Ship-time ARCHITECTURE.md autogen — `/dross-ship` self-heals an absent doc and folds each phase's landmarks into the matching feature entry in place
- [x] `/dross-watch` — a read-only heartbeat: board inbound + phase-drift digest since the last tick, ending with one suggested next command (`watch-pr-ci-status` extends it to PR/CI state)
- [x] Additional board backends + verify-merge-before-completion + PR-record-reaches-base — more issue-board providers, plus ship/verify ordering fixes so a phase can't complete before its merge lands and the PR record reaches the base branch

### Milestone v0.10 — workflow ergonomics: task lifecycle & telemetry clarity (current)

- [x] Task-lifecycle commands — `dross task {add,remove,edit}` edit `plan.toml` through guarded verbs: `add` appends or inserts (`--after`/`--before`) with a fresh per-plan high-water id (a freed id is never reissued), `remove` is dependency-safe with a `--force` strip, `edit` is a partial field update, and every mutation is gated by a pre-write integrity guard that leaves the file byte-unchanged on rejection
- [ ] Task reordering, landmark comma-fix, telemetry bucket graduation + taxonomy overhaul, ship clean-tree, verify auto-finalize — remaining v0.10 backlog

## Telemetry

Dross records local-only usage events at `~/.claude/dross/telemetry.jsonl`. The intent is single-developer self-observation — a dogfood log you can read back later to find where the tool gets in your way.

**What's recorded.** One JSONL event per `dross` invocation (command path, duration, exit code, error class) plus outcome events from `verify` (mechanical run emits `verdict=pending` plus `mutation_status` tag; `dross verify finalize <phase>` later emits the resolved `pass | partial | fail` plus mutation score), `ship` (provider, result, force-flag use), `phase create` (ordinal), and `doctor` (result = `passed` | `issues_found`, issue count). All events carry a 12-character SHA-256 hash of the absolute repo path so per-project trends are visible without exposing the path itself.

**Error buckets.** When a CLI invocation exits non-zero, the error is classified into one of: `no_root`, `no_phase`, `no_spec`, `no_plan`, `no_milestone`, `dirty_tree`, `merge_pending`, `wrong_branch`, `verify_state`, `mutation`, `provider`, `board`, `env_token`, `unknown_subcommand`, `unknown_field`, `unknown_flag`, `arg_count`, `landmark_parse`, `cli_args`, `cancelled`, `check_issues`, `state_io`, `config_io`, `already_exists`, `invalid`, `missing`, `permission`, `git`, `network`, `other`. For most classified buckets the raw message is never recorded — the bucket already describes the failure. Two kinds of bucket are exceptions and carry a redacted, length-capped copy of the message in `err_detail`: the catch-all `other`, because the unclassified tail is otherwise undiagnosable (countable but opaque), and the identifier-only buckets `unknown_subcommand`, `unknown_field`, `unknown_flag`, `arg_count` and `landmark_parse`, whose messages are bounded CLI tokens you typed — the rejected subcommand, field path, flag, arg count or landmark pair — so the bucket shows *what* was wrong, not just that something was. The home directory is collapsed to `~` so absolute paths don't leak. As patterns surface in `other`, they graduate into named buckets; a graduated bucket only keeps its detail when the message is a bounded identifier.

Classification walks an ordered rule table first-match-wins, arranged in tiers: root/scaffold state (`no_root`), then workflow-specific buckets (phase/plan/spec/verify/mutation/provider/board and friends), then the CLI surface (cobra's unknown-command/flag/arg shapes, landmark and field parses), then the generic safety-net buckets — where the more diagnostic bucket wins: `already_exists`, `permission`, `git`, `network`, then `invalid`/`missing` — with `other` as the fallback when nothing matches. The order is enforced in code: a rule that would make a later rule unreachable fails the build.

**What's NOT recorded.** Anything you typed. No criterion text, no decision text, no commit messages, no PR titles or bodies, no reviewer names, no file contents, no repo URLs. Counts and small enums only — plus the redacted `err_detail` on the allowlisted buckets above, which holds dross's own (path-redacted) error string, never your input.

**Privacy posture.** Local file. No network. No daemon. No third party. Default ON; `/dross-init` and `/dross-onboard` ask once and stamp `asked_at` so you're never re-prompted across projects.

**Toggles.**
```sh
dross stats opt-out       # disable; persisted in defaults.toml
dross stats opt-in        # re-enable; same file
DROSS_NO_TELEMETRY=1      # authoritative kill-switch (env var; overrides on-disk config)
dross stats path          # print the log file path
```

**Reading it back.**
```sh
dross stats               # default `show` — top commands, error buckets,
                          #   force-flag count, verify verdicts, ship results,
                          #   doctor runs
dross stats show --since 7d
dross stats show --since 2026-05-01
```

The schema is versioned and stable; the log is append-only and rotates at 10 MB. Safe to share in conversations or pastebins for ad-hoc analysis since it carries no project-identifying content.

## License

[AGPL-3.0](LICENSE).

## Acknowledgements

Dross is conceptually inspired by [GSD](https://github.com/gsd-build/get-shit-done) by TÂCHES (Lex Christopherson), distributed under the MIT License. No code or prompt text is copied; this is a clean Go reimplementation built around different design pivots (lean prompts, pair-mode execution, mutation testing as a first-class gate). If you want the full-featured, well-trodden tool, GSD is excellent — Dross is a fork of the *idea*, not the implementation.
