# Dross

A leaner successor to [GSD](https://github.com/gsd-build/get-shit-done) for working with Claude Code on real projects.

> **Status:** v1.0 — the full plan → execute → verify → ship loop runs on phase-branch isolation (`dross phase create` auto-checks out `phase/<id>` and records the branch it forked from; `dross phase complete` finalizes post-merge against that recorded base, never an inferred one), with a milestone-branch topology layered on top (phase PRs squash-merge into `milestone/<version>`; the milestone itself lands as a merge commit — in `main`, or in the milestone branch it was stacked on while that one is still unmerged). Mutation testing covers TS/JS/Svelte (Stryker) and Go (Gremlins); C# (Stryker.NET) is fixture-tested but undogfooded. Every interactive command is a propose-and-react conversation — one decision per turn — under the `dross-interaction-contract` rule (see [Interaction](#interaction)). Since v0.3 the loop has grown considerably: slug-based phase identity with `dross phase {insert,move,rename}` lifecycle verbs (v0.4) and `dross task {add,remove,edit}` plan-editing verbs (v0.10); deferred-item routing and triage across specs (v0.5); container/IaC scanning, a GitLab ship provider, an ARCHITECTURE.md comprehension layer, and self-update via `dross update` (v0.6); non-interactive `dross ship --auto` and the milestone-branch model (v0.7); a native Claude Code statusline (v0.8); and release trust — minisign-signed releases with verify-before-swap self-update, `/dross-watch` (read-only board + phase-drift digest), and additional board backends (v0.9). Opt-in issue-board sync across six backends (Forgejo/Gitea/GitLab/YouTrack/Jira/GitHub — milestones/phases/quicks → board issues, inbound triage via `/dross-inbox`, proven end-to-end against Jira Cloud) lives behind `dross issue enable`; `/dross-pause` + `/dross-resume` capture and replay a mid-phase handoff; `/dross-plan` auto-runs an independent plan review (`--no-review` to skip) and offers `--panel` — a 3-lens planner panel merged by a cold judge; and context-free `/dross-secure` / `/dross-quality` audits (real scanners/analyzers + adversarial refute-panels) scaffold a remediation phase. See the per-milestone log under [Roadmap](#roadmap) for the full arc.

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
├── state.json        # machine-local: current position, version, activity log — gitignored, never committed
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

#### Upgrading a repo that still tracks `.dross/state.json`

`state.json` holds machine-local position — current phase, version, activity history. It used to be tracked, and is now gitignored: a tracked copy gets dragged forward into every later tree by the squash, so one machine's position silently replays onto another's.

If your repo has branches cut before that change, the first branch switch onto one of them will **refuse** rather than proceed:

```
refusing to switch to <branch>: it would overwrite your live .dross/state.json.
```

That is the guard working. git overwrites an *ignored* working-tree file without complaint, so dross stops instead of letting the switch replace your live copy. The refusal prints the remedies in cheapest-first order, and the cheapest one that applies is the one to take:

- **Fast-forward** — offered when the branch is merely behind its remote and the remote no longer carries the tracked copy. Nothing to move aside.
- **Move aside and restore** — copy the live file, switch, copy it back. Always works, changes nothing permanently.
- **Untrack it for good** — `git rm --cached .dross/state.json` on that branch, and commit. This is the one that ends the problem: once no branch carries a tracked copy, the refusal can never fire again.

`dross doctor` reports the same condition, with the same fix, before you hit it.

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
| `dross project {show,get,set}` | Read/write `project.toml` fields by dotted path. Every `[board]` field is settable — including `board.github_project`, individual `board.state_map.<status>` entries, and the `board.fields.{state,type,fix_versions}` tracker-field-name overrides, each addressed one key at a time. `set --unset <path>` clears a field written by mistake (a scalar, a single `state_map` entry, or one field override) without hand-editing TOML; `show` also accepts `--json` (bare document, no `#` header) | ✅ |
| `dross state {show,get,set,touch,bump}` | Read/write `state.json` (`bump internal` increments the 4th version segment). `get` takes one or more field names — one prints its bare value, two or more emit a single keyed JSON object; `show --json` is accepted for symmetry (state is already JSON) | ✅ |
| `dross local {get,set}` | Read/write `.dross/local.toml` — gitignored, machine-local values that must not ride cumulative history. Keys: `quick_base`, the branch a standalone `/dross-quick` committed to, which `ship` and `phase complete` reconcile alongside the phase's own base; and `allow_hosts`, comma-separated additions to the derived API host allowlist. `allow_hosts` is machine-local *because* it is the escape hatch — a committed one would let a cloned repo authorize its own API host, so dross refuses to read a `local.toml` git reports as tracked | ✅ |
| `dross remote {grant,status,revoke,bootstrap}` | Authorize (or inspect) a host that mutation runs execute on. `dross mutation remote grant <host> <workdir>` rsyncs this working tree there and runs the mutation adapters on that machine, as you — so it prints the host and workdir it is about to authorize **before** it writes them, and it is the only writer of `mutation_remote_host` / `mutation_remote_workdir`. Those two are deliberately absent from `dross local set`: a generic key-writer would let an agent grant code execution on another machine without ever showing you what for. The grant lives in the gitignored `.dross/local.toml`, so a clone carries none, a `local.toml` git reports as tracked is refused unread, and a remote host in the tracked `project.toml` is refused by name. Three companion keys **are** settable with `dross local set`: `mutation_workers` and `mutation_test_cpu` (parallelism knobs — unset keeps NumCPU/2 and 1, sized off the remote's core count when one is granted), and `mutation_remote_env`, a comma-separated allowlist of **variable NAMES** — never name=value pairs. dross reads their values from the surrounding environment at run time, pipes them to the remote over ssh stdin, and stores none of them; an allowlisted name that is unset locally is an error rather than an empty export. `dross remote bootstrap [--apply]` provisions a granted host: it probes for the tools the configured `[mutation].adapters` need and installs the adapter **packages** it can into a runtime that already exists there (gremlins via a pinned `go install`), while a missing language **runtime** (go, node, dotnet) is refused by name for its owner to install rather than installed — version policy and PATH ownership are not a mutation run's call. Dry-run by default, printing the exact command per missing tool; `--apply` installs; a provisioned host is a no-op; one tool's failure never aborts the rest and any refusal or failure exits non-zero. A run that needs the host also **preflights** it before pushing the tree, and a host it cannot reach falls back to a local run that says so — `verify.toml` records `measured_on`, so a local score and a remote one are never indistinguishable after the fact; a host that ran and failed does not fall back | ✅ |
| `dross rule {add,list,remove,promote,disable,enable,show}` | Two-tier rules system | ✅ |
| `dross phase {create,checkout,list,show,complete,insert,move,rename,number,migrate,red-proof}` | Phase directories + slug-based lifecycle. `create` auto-checks out a `phase/<id>` branch off the base so phase work never lands on main, and records that base — plus the commit it pointed at, the phase's durable fork point — in the phase's `changes.json`; `red-proof set <phase-id> --sha <sha> --doc <path> [--replay <cmd>]` pins the commit a phase's red proof was captured at alongside the doc that replays it — and optionally the command that replays it — validating all of them rather than letting a machine-checked record be hand-edited; `red-proof repoint [<phase-id>] [--apply]` repairs a pin whose commit origin can no longer reach, rewriting both the record and every occurrence in the replay doc to the owning phase's fork point, dry-run by default and leaving a sound pin untouched — where a replay is recorded and consented (`dross trust --replay <phase-id>`) it is re-run at the proposed commit and the repair is refused unless the proof still goes red, and otherwise the repair is reported unverified rather than implying it was checked. `dross ship` runs the same repair automatically for a pin its own squash-merge would orphan, so the rewrite rides that phase's PR; `checkout` switches to an existing `phase/<id>` through the guard that refuses a branch whose tracked `state.json` would overwrite the live machine-local one (it never creates the branch); `complete` finalizes after squash-merge (ff base, write the completion record, delete `phase/<id>` locally **and on origin**) against **that recorded base** rather than one inferred from the active milestone — and it is the sole writer of the completed-state transition, since `dross ship` only marks the phase `shipped` and leaves `current_phase` set until a merge is confirmed. It ends by stating the resulting branch topology, which `dross status` also carries as a standing `branch:` line — a phase with no record refuses and names `--base <branch>`, and the branch switch happens only after every check passes, so a refusal moves no local ref. `complete --recover` heals a diverged base by resetting it to origin and restoring `.dross/` from the phase branch's tip; it follows the recorded base, so it works under a milestone and never touches a branch the phase wasn't forked from; `list` prints every phase directory in milestone-array order, marking each done phase `✓ ` (two spaces when not) and ending with an `N/M done` footer — doneness read from the phase's own completion record, the same reader `dross status` and `dross milestone progress` count with, never a `verify.toml` verdict; a slug carried onto two milestones' roadmaps lists once, at its earlier position (`dross doctor` names it). `dross phase list --milestone <version>` scopes the listing to that milestone's `phases` array in roadmap order, including slugs with no phase directory (marked `(not scaffolded)` and counted in the denominator) and reporting that milestone's done/total; an unknown version is an error naming it. `insert`/`move`/`rename` edit a milestone's phase array and on-disk identity; `number` prints a phase's ordinal; `migrate` converts legacy NN-slug phases to bare slugs; `show` also accepts `--json` (bare document, no `#` header) | ✅ |
| `dross milestone {create,list,show,get,set,add,remove,replace,progress,complete,prune}` | Milestones with dotted-path edits (set scalars, add/`remove`/`replace` on list fields — `remove` and `replace` address an entry by its exact value, mirroring `add`, and `replace` keeps the entry's position so fixing a phase name is not a reorder; a value that isn't there is an error, not a silent no-op). `create` cuts `milestone/<version>` **conditionally** — from the current milestone's branch while that branch is still unmerged, from the main branch once it has merged, is gone, or there's no current milestone — and records that branch as the milestone's `base`, read back verbatim ever after rather than re-inferred (`dross milestone get base`; `create` is its sole writer, `--base <branch>` forces the cut point). `complete` opens the integration PR against **that recorded base** while it is still unmerged and against main once it has merged, so a stacked milestone's PR never shows its parent's commits as its own; `--finalize` records `[milestone].status = complete` **before** it fast-forwards main + deletes the milestone branch, accepting a merge into the recorded parent as well as into main; that marker makes the command safe to re-run — a second `--finalize` reports already-finalized and exits 0 (naming any leftover branch for `prune`) rather than asking a merge question the deleted branch can no longer answer, and a branch that is simply gone is reported as gone rather than as unmerged. Neither `--finalize` nor `prune` will delete a branch an unmerged stacked milestone still records as its base, or (when the provider is reachable) one that is an open PR's base — the check announces itself when it has to be skipped. `progress [version] [--json]` reports status, done/total, `remaining` and `unscaffolded` in one call — a phase counts done only when its own `changes.json` records it shipped or complete (a slug with no phase directory never does), so a milestone cannot close over work that was listed and never built; every arm exits 0, it is dispatch data rather than a gate. `show` also accepts `--json` (bare document, no `#` header) | ✅ |
| `dross task {next,list,show,status,add,remove,edit,move}` | Inspect and edit tasks within a plan. `list [phase-id]` prints the whole plan — id, wave, status, title — as an aligned table or `--json`, defaulting to `state.current_phase`; `add` appends or inserts (`--after`/`--before`) a task with a fresh high-water id; `remove` is dependency-safe (`--force` strips dependents); `edit` is a partial field update; `move` re-orders a task relative to another. `show` also accepts `--json`. `plan.toml` is mutated only through these verbs, never hand-edited | ✅ |
| `dross changes {record,show}` | Per-phase append-only log of what was touched; `show` also accepts `--json` (bare document, no `#` header) | ✅ |
| `dross verify <phase>` | Run mutation tests + write tests.json + verify.toml skeleton. The score covers **only the phase's own changed files** — every report is filtered against the phase's change set (git diff ∪ changes.json), so a survivor in an untouched file of the same package neither fails the phase nor inflates its score; filtered survivors are kept under `out_of_scope` in tests.json, and every survivor — in-scope or not — carries exactly one lifecycle state (`in-diff` / `routed` / `accepted` / `unclassified`), so an unclassified one is FLAGged for draining rather than re-listed inertly forever. `[summary].mutation_status` is `measured` / `unmeasurable` / `skipped` / `out-of-scope` so `/dross-verify` can tell a real low score from a 0/0 artefact and skip the score thresholds when there's nothing to measure | ✅ |
| `dross verify finalize <phase>` | Record resolved verdict from verify.toml as a telemetry outcome event (after `/dross-verify`) | ✅ |
| `dross status` | Where am I — project, phase, last activity, suggested next step | ✅ |
| `dross profile {show,seed}` | User profile (with GSD import); `show` also accepts `--json` (bare document, no `#` header) | ✅ |
| `dross validate` | Schema-check every artefact | ✅ |
| `dross codex <file>` | Polyglot code insight — symbols, refs, siblings, recent activity. Go via stdlib `go/ast`; TS/TSX/Svelte/C#/GDScript via `ast-grep` shell-out (graceful no-op if ast-grep not on PATH) | ✅ |
| `dross security {detect,run,scaffold,findings}` | Deterministic surface of the `dross-secure` audit — run dirs, scanner detection, findings→spec scaffold, cross-run `findings` ledger (list/state/reconcile); audit orchestration lives in `secure.md` | ✅ |
| `dross quality {detect,run,scaffold,findings}` | Deterministic surface of the `dross-quality` audit — run dirs, analyzer detection, maintainability-risk scaffold, cross-run `findings` ledger; orchestration in `quality.md` | ✅ |
| `dross stack {detect,show,list,apply,loadout}` | Stack profiles — detect the stack, show/list profiles, `apply` re-syncs `[runtime]`, `loadout` emits the agent loadout block. Embedded built-ins + `~/.claude/dross/profiles/` drop-ins (user wins). Go/TypeScript/Kotlin/Dart/Svelte/SQL profiles ship, plus a marker-file Docker profile; `show` also accepts `--json` (bare document, no `#` header) | ✅ |
| `dross doctor` | Project-level health check: foundational files exist (`project.toml`, `rules.toml`), `state.json` is untracked, `project.toml` and `state.json` agree on the version, no stale `milestone/*` branches, `[remote]` ↔ git origin matches, `auth_env` exported, `.gitattributes` marks `.dross/` linguist-generated, no phase commits leaked onto main, every recorded red proof still pins a commit **reachable from origin** (a pin whose commit no `origin/*` ref contains is an issue naming the doc, the SHA and the owning phase's fork point to repoint to; a shallow clone or a repo with no origin refs is a warning that leaves the exit code alone, since an absence of history is not a verdict) | ✅ |
| `dross defaults {show,save}` | Read/write `~/.claude/dross/defaults.toml` (cross-project pre-fills); `show` also accepts `--json` (bare document, no `#` header) | ✅ |
| `dross env {list,set,unset}` | Manage env keys in `~/.claude/settings.json` (hidden input, never echoed) | ✅ |
| `dross ship <phase-id>` | Push `phase/<id>` to the provider, open PR, request reviewers. Provider's squash-merge collapses per-task commits | ✅ |
| `dross ship comment` | Post a markdown comment to a PR via provider (used by /dross-review) | ✅ |
| `dross ship recover` | One-shot migration tool for legacy repos with phase commits on main or `.dross/` stripped from prior PRs — fetch + reset + restore `.dross/` + commit, atomically | ✅ |
| `dross issue {enable,disable,milestone-sync,phase-sync,quick,pull,dismiss,link,list}` | Opt-in issue-board sync (Forgejo/Gitea/GitLab/YouTrack/Jira/GitHub). Mirrors milestones/phases/quicks → board issues (idempotent), pulls inbound issues for triage. Off by default; configured under `[board]` (`provider` + `base_url` + `auth_env`, plus `auth_user` for Jira). `[board.fields]` overrides the tracker-native field names sync writes to (`state`, `type`, `fix_versions`) so a renamed field — or a non-English tracker UI — syncs without a code change; unset keys keep today's literals. **Proven end-to-end against Jira Cloud (2026-07-25)** — full milestone-sync → phase-sync → pull round-trip; other backends wired but not yet dogfooded. | ✅ |
| `dross stats {show,path,opt-in,opt-out}` | Aggregates over the local telemetry log; toggle the recorder; `show` also accepts `--json` (bare document, no `#` header) | ✅ |
| `dross architecture check` | Inspect/repair `ARCHITECTURE.md` symbol links — reports drift, `--fix` re-resolves `file:line` targets | ✅ |
| `dross deferred {list,route,unroute,dismiss,add}` | Inspect and route deferred items across phase specs; `add` files a new one from the command line — into the current phase's spec, or a project-level store when there's no usable phase home | ✅ |
| `dross survivor drain` | Answer "does this repo have standing debt?" — classify every surviving mutant across **every** Go package (no diff scoping, unlike `dross verify`) and report the ones carrying no disposition, exiting non-zero if any remain. A survivor routed to the phase being drained counts outstanding, so the backlog can't be closed by re-routing it to itself, and a package the adapter wrote no report for is a hard failure rather than a package that looks clean. `--report` classifies a recorded gremlins report; `--packages` narrows the scope | ✅ |
| `dross survivor {accept,route,list,retire}` | Drain surviving mutants: `accept` records one in the tracked repo-level `.dross/survivors.toml` with a mandatory reason (the only state that silences it), `route` parks it as a deferred item carrying its identity key plus a destination phase, `list --stale` surfaces acceptances whose subject is gone, `retire <key>...` (or `retire --stale`) removes them again so the store never needs hand-editing — `--stale` retires exactly the acceptances whose subject is gone and never one that merely couldn't be checked. Identity is file + op + a hash of the mutated line's normalized text, so it survives line drift but never a genuinely changed line | ✅ |
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

### Milestone v0.6 — surface depth & loop hardening (complete)

- [x] Multi-language analyzer catalogs + deepened container/IaC scanning — per-stack scanner/analyzer loadouts feeding `dross-secure` / `dross-quality`, with Dockerfile/compose/IaC surfaces (hadolint/trivy/checkov-class tools) wired in
- [x] Secure/quality findings lifecycle — audit findings persist with a state machine (open → fixed/accepted/false-positive) across re-runs instead of re-surfacing every pass
- [x] Ship/complete recovery hardening — `dross ship recover` + `dross phase complete --recover` heal the diverged-main and dirty-tree failure states with one command instead of manual `.dross/` surgery
- [x] ARCHITECTURE.md enhancements + comprehension layer — richer feature entries and a scan-driven backfill/refresh (`dross architecture check` catches symbol-link drift)
- [x] GitLab ship provider + YouTrack board backend — a third PR provider and a third issue-board backend alongside GitHub and Forgejo/Gitea
- [x] Self-update & distribution — `dross update` fetches, verifies, and atomically swaps the running binary, then re-syncs commands/prompts
- [x] Gray-area walkthrough — the spec locked-decisions step deep-dives phase-specific gray areas one at a time into `[[decisions]]` / `[[deferred]]`

### Milestone v0.7 — branch topology & non-interactive ship (complete)

- [x] `dross ship --auto` — a non-interactive fast-path (skips body preview / reviewers / merge gate) suitable for scripts and loops; `--json` emits `{url, number, result}`
- [x] Milestone-branch model — phase PRs squash-merge into `milestone/<version>` when a milestone is active; the milestone itself lands in `main` as a merge commit (not a squash) so `main` keeps per-phase history, finalized by `dross milestone complete`
- [x] Interaction defer-or-add framing — borderline candidates are surfaced as a single either/or that leads with "defer it" and offers "add to current phase", standardized across spec/plan

### Milestone v0.8 — Claude Code surface integration (complete)

- [x] Native statusline — `dross statusline` renders the Claude Code status line from status JSON on stdin (project, milestone, phase, progress), installed via `dross install`

### Milestone v0.9 — backlog burn-down: trust, findings & integrations (complete)

- [x] Release trust & distribution — minisign-signed releases with verify-before-swap in `dross update` (public key embedded; signing key + password in CI secrets); Homebrew + Windows distribution paths
- [x] Ship-time ARCHITECTURE.md autogen — `/dross-ship` self-heals an absent doc and folds each phase's landmarks into the matching feature entry in place
- [x] `/dross-watch` — a read-only heartbeat: board inbound + phase-drift digest since the last tick, ending with one suggested next command (`watch-pr-ci-status` extends it to PR/CI state)
- [x] Additional board backends + verify-merge-before-completion + PR-record-reaches-base — more issue-board providers, plus ship/verify ordering fixes so a phase can't complete before its merge lands and the PR record reaches the base branch

### Milestone v0.10 — workflow ergonomics: task lifecycle & telemetry clarity (complete)

- [x] Task-lifecycle commands — `dross task {add,remove,edit}` edit `plan.toml` through guarded verbs: `add` appends or inserts (`--after`/`--before`) with a fresh per-plan high-water id (a freed id is never reissued), `remove` is dependency-safe with a `--force` strip, `edit` is a partial field update, and every mutation is gated by a pre-write integrity guard that leaves the file byte-unchanged on rejection
- [x] Task reordering, landmark comma-fix, telemetry bucket graduation + taxonomy overhaul, ship clean-tree, verify auto-finalize

### Milestone v1.0 — hardening: every claim proven (complete)

- [x] Self-audit pass — each documented guarantee re-checked against the code that is supposed to enforce it, with the gaps closed rather than noted
- [x] Board sync proven against a live Jira Cloud instance rather than fixtures alone
- [x] README truth pass — claims the tool does not honour removed instead of softened

### Milestone v1.1 — friction pass: the logs stop repeating themselves (complete)

- [x] Recurring friction surfaced by the telemetry log turned into fixes, so the same error stops being re-diagnosed by hand every session

### Milestone v1.2 — branch-model truth: the base is a fact, not a guess (complete)

- [x] A phase's base branch is recorded at fork time and read back verbatim, never re-derived from git topology — the divergence that previously needed manual recovery commits cannot recur

### Milestone v1.3 — finish the tool: the standing backlog, drained (complete)

- [x] Mutation scoping to the phase's own diff, plus a survivor lifecycle where every survivor is in-diff, routed, or accepted-with-reason and an accepted one is never re-emitted
- [x] Milestone lifecycle closes itself — `dross milestone complete --finalize` owns the status flip and is idempotent
- [x] Honest board sync, honest config: a board failure surfaces instead of counting as zero, and every documented config value either changes behaviour or is rejected
- [x] Off-box mutation runs on a consented remote host, with per-run scratch build caches wiped on every exit path
- [x] Board mirroring at task granularity, driving the tracker's own workflow field

### Milestone v1.4 — the backlog burn: every parked idea, routed or killed (complete)

- [x] Test-suite hermeticity enforced, not just documented — a test reading the real repo's gitignored `.dross` fails locally instead of only in CI
- [x] `dross task add/edit --test-contract` and `dross phase create --adopt` — nothing dross supports needs hand-editing an artefact
- [x] Every `dross issue pull --json` failure reaches the envelope, setup failures included, instead of crashing a consumer
- [x] Per-leg mutation scores persisted in `verify.toml`, so a small leg scoring badly is no longer invisible inside a large leg's good pooled number
- [x] Upgrade note for the `state.json` checkout refusal, and a roadmap guard that fails when a milestone's recorded status and the README disagree
- [x] `dross run <name>` executes every configured runtime command, and plan review runs in effort tiers
- [x] The TypeScript mutation leg runs on every PR in CI, on a pinned toolchain no developer installs by hand
- [x] Board→dross inbound mirroring — dragging a task's card moves that task in `plan.toml`, and a task edited on both sides is refused with both values named
- [x] Several authorized mutation hosts with first-ready-wins selection, and a run that refuses a full scratch volume instead of quietly filling one
- [x] Five parked ideas dismissed on measurement rather than deferred again — scanner scratch caches, gremlins workdir pinning, remote runtime installation, remote-run latency, remote test-watch

### Milestone v1.5 — signal truth: one answer to "is this done", and a board that agrees with it (active)

- [ ] One doneness reader behind `dross status`, `dross milestone progress` and `dross phase list` — the completion record, never a verify verdict
- [ ] Phases finished before `changes.json` carried a status marked done through a supported verb, not a hand-authored file
- [ ] `dross issue pull` returns only issues dross did not author, and a shipped phase's task cards leave task-in-review by themselves
- [ ] `/dross-plan` walks every unresolved gray area one turn each, with no multiSelect pre-selection

## Telemetry

Dross records local-only usage events at `~/.claude/dross/telemetry.jsonl`. The intent is single-developer self-observation — a dogfood log you can read back later to find where the tool gets in your way.

**What's recorded.** One JSONL event per `dross` invocation (command path, duration, exit code, error class) plus outcome events from `verify` (mechanical run emits `verdict=pending` plus `mutation_status` tag; `dross verify finalize <phase>` later emits the resolved `pass | partial | fail` plus mutation score), `ship` (provider, result, force-flag use), `phase create` (ordinal), and `doctor` (result = `passed` | `issues_found`, issue count). All events carry a 12-character SHA-256 hash of the absolute repo path so per-project trends are visible without exposing the path itself.

**Error buckets.** When a CLI invocation exits non-zero, the error is classified into one of: `incomplete_root`, `no_root`, `no_phase`, `no_spec`, `no_plan`, `no_milestone`, `dirty_tree`, `merge_pending`, `wrong_branch`, `verify_state`, `mutation`, `provider`, `board`, `env_token`, `unknown_subcommand`, `unknown_field`, `unknown_flag`, `arg_count`, `landmark_parse`, `cli_args`, `cancelled`, `check_issues`, `state_io`, `config_io`, `already_exists`, `invalid`, `missing`, `permission`, `git`, `network`, `other`. For most classified buckets the raw message is never recorded — the bucket already describes the failure. Two kinds of bucket are exceptions and carry a redacted, length-capped copy of the message in `err_detail`: the catch-all `other`, because the unclassified tail is otherwise undiagnosable (countable but opaque), and the identifier-only buckets `unknown_subcommand`, `unknown_field`, `unknown_flag`, `arg_count` and `landmark_parse`, whose messages are bounded CLI tokens you typed — the rejected subcommand, field path, flag, arg count or landmark pair — so the bucket shows *what* was wrong, not just that something was. The home directory is collapsed to `~` so absolute paths don't leak. As patterns surface in `other`, they graduate into named buckets; a graduated bucket only keeps its detail when the message is a bounded identifier.

Classification walks an ordered rule table first-match-wins, arranged in tiers: root/scaffold state (`incomplete_root`, `no_root`), then workflow-specific buckets (phase/plan/spec/verify/mutation/provider/board and friends), then the CLI surface (cobra's unknown-command/flag/arg shapes, landmark and field parses), then the generic safety-net buckets — where the more diagnostic bucket wins: `already_exists`, `permission`, `git`, `network`, then `invalid`/`missing` — with `other` as the fallback when nothing matches. The order is enforced in code: a rule that would make a later rule unreachable fails the build.

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
