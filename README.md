# Dross

A leaner successor to [GSD](https://github.com/gsd-build/get-shit-done) for working with Claude Code on real projects.

> **Status:** v0.1.0 — full plan → execute → verify loop is wired (Stryker for TS/JS/Svelte mutation testing). Tree-sitter codex and Go/C# mutation adapters are still stubs. First real-project onboarding done; expect ongoing prompt fixes as more flows are exercised.

> Scope: Dross is built for my workflow. It's public because there's no reason not to be, but I'm not marketing it and I'm not trying to grow it into a general-purpose tool. The roadmap is a flat list because my todo list is — if Dross ever picks up users, I'll think about structure (semver, milestones, contribution guidelines) then.

> Contributing: I'm unlikely to accept feature PRs that don't match how I personally use this. Bug fixes and small quality-of-life improvements are welcome; new features probably aren't, unless we've talked first. If Dross is almost what you want but not quite, fork it — that's what AGPL is for, and you'll move faster owning your own copy than waiting on me.

## Why

GSD is genuinely good at imposing planning discipline, but at a cost: ~3 MB of prompt material loaded across 65 skills, 33 subagents, and 76 workflows. A single `/gsd-plan-phase` invocation reads ~3,000 lines of instructions before doing anything. Subagent spawns multiply that.

Dross is a rebuild around three pivots:

1. **Lean prompts.** Target ≤300 lines per slash command. Most state lives in machine-parseable TOML, not prose Markdown.
2. **Pair-mode execute by default.** Code is authored *with* you, not delivered *to* you. Subagent spawns kept to genuinely independent work (parallel mutation runs, multi-language audits).
3. **Test efficacy as a first-class gate.** GSD checks that tests *exist*. Dross checks that tests *catch breakage* — via mutation testing (Stryker / Gremlins), coverage delta, and an LLM judge mapping each acceptance criterion to a specific test.

## The name

Dross is the AI sidekick from Will Wight's [Cradle](https://www.willwight.com/cradle) series — a Presence that lives in the protagonist's head, compiling battle plans, predicting opponents, crafting illusions, and handling "unimportant thoughts" to free up his bandwidth. Sarcastic, dramatic, fond of his person.

## Footprint vs GSD

Measured by recursively resolving `@`-imports for each command and summing bytes. Token estimate is `bytes ÷ 4`, the standard heuristic for English+markdown — accurate to ±15% vs an exact tokenizer.

**Per-command boot** (what loads before the model writes a single response):

| Command | Bytes | Est. tokens |
|---|---:|---:|
| GSD `/gsd-execute-phase` | 185,972 | ~46,500 |
| GSD `/gsd-plan-phase` | 103,413 | ~25,900 |
| GSD `/gsd-new-project` | 69,637 | ~17,400 |
| GSD `/gsd-progress` | 37,864 | ~9,500 |
| Dross `/dross-init` | 4,784 | **~1,200** |
| Dross `/dross-onboard` | 3,383 | **~850** |
| Dross `/dross-rule` | 2,119 | **~530** |
| Dross `/dross-spec` | 4,494 | **~1,120** |
| Dross `/dross-plan` | 5,676 | **~1,420** |
| Dross `/dross-plan-review` | 5,524 | **~1,380** |
| Dross `/dross-execute` | 7,697 | **~1,920** |
| Dross `/dross-verify` | 7,540 | **~1,890** |
| Dross `/dross-status` | 1,635 | **~410** |

**Total prompt-surface** (everything that could ever load):

| | Bytes | Est. tokens |
|---|---:|---:|
| GSD (workflows + references + skills + agents) | 2,494,659 | ~624,000 |
| Dross (commands + prompts) | 42,886 | ~10,700 |
| **Ratio** | | **≈ 58×** |

**Being honest about these numbers:**

- **Dross is still incomplete.** The codex tree-sitter indexer is a stub; only the Stryker (TS/JS/Svelte) mutation adapter is wired — Go (Gremlins) and C# (Stryker.NET) are designed but not implemented. `/dross-verify` landed at ~1,890 tokens — ~24× cheaper than GSD's 46,500 — though that's slash-command boot only; the verify loop reads project test files at runtime, which adds variable cost.
- **Per-invocation isn't the runtime cost.** GSD spawns subagents (planner, plan-checker, executor, verifier). Each loads its own agent prompt + references in fresh context, multiplying the real per-flow cost by 2-3×. The 25.9k for `/gsd-plan-phase` is closer to ~60-80k of total prompt material per phase. Dross runs inline — no subagent multiplication.
- **Prompt caching mitigates this.** Anthropic's prompt cache amortises repeats, so steady-state cost is much lower than the load surface implies. Cold starts, branch switches, and subagent spawns break the cache; that's where the bill actually shows up.
- **The ratio is the worst-case load surface, not a runtime bill.** It's still directionally meaningful — fewer files, smaller files, fewer spawns add up — but don't expect the same multiplier in your monthly Anthropic invoice.

## Concept

```
intent ─► SPEC ─► PLAN ─► CODE ─► TESTS ─► EFFICACY PROOF ─► VERIFY
         (lock)  (waves)  (atomic   (per     (mutation +      (goal-
                          commit)   task)    coverage)        backward)
```

## Layout

```
cmd/dross/         Go CLI entrypoint
internal/          project, state, rules, profile, phase, milestone, changes, verify, mutation, codex
assets/commands/   Slash command markdown (installed to ~/.claude/skills/dross-<name>/SKILL.md)
assets/prompts/    Prompt instructions (installed to ~/.claude/dross/prompts/)
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
    └── NN-slug/
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
├── dross-spec/SKILL.md
├── dross-plan/SKILL.md
├── dross-plan-review/SKILL.md
├── dross-execute/SKILL.md
├── dross-verify/SKILL.md
├── dross-status/SKILL.md
└── dross-rule/SKILL.md

~/.claude/dross/
├── profile.toml                       # cross-project user profile (planned, not yet wired)
├── rules.toml                         # cross-project rules
└── prompts/                           # symlink → assets/prompts/
```

Symlinks mean edits to `assets/` in the dross repo apply immediately — no re-install on prompt tweaks.

## Build

```sh
make build       # builds ./dross for current arch
make test        # go test -count=1 ./...
make install     # builds + installs binary + symlinks all slash commands & prompts
make doctor      # verifies every /dross-* command is correctly installed
make uninstall   # removes binary, all dross-* skills, and the prompts symlink
```

After `make install`, ensure `~/.local/bin` is on your PATH:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Then in any Claude Code session, `/dross-init` (greenfield) or `/dross-onboard` (existing repo).

## Available commands

| Command | Description | Status |
|---|---|:---:|
| `dross init` | Bootstrap `.dross/` (greenfield) | ✅ |
| `dross onboard` | Adopt an existing repo (signal scan) | ✅ |
| `dross project {show,get,set}` | Read/write `project.toml` fields | ✅ |
| `dross state {show,set,touch}` | Read/write `state.json` | ✅ |
| `dross rule {add,list,remove,promote,disable,enable,show}` | Two-tier rules system | ✅ |
| `dross phase {create,list,show}` | Phase directories | ✅ |
| `dross milestone {create,list,show}` | Milestones | ✅ |
| `dross task {next,show,status}` | Inspect / update tasks within a plan | ✅ |
| `dross changes {record,show}` | Per-phase append-only log of what was touched | ✅ |
| `dross verify <phase>` | Run mutation tests + write tests.json + verify.toml skeleton | ✅ |
| `dross status` | Where am I — project, phase, last activity, suggested next step | ✅ |
| `dross profile {show,seed}` | User profile (with GSD import) | ✅ |
| `dross validate` | Schema-check every artefact | ✅ |
| `dross codex` | Polyglot code insight (tree-sitter) | 🚧 |

**Slash commands:**

| Command | Status |
|---|:---:|
| `/dross-init` | ✅ |
| `/dross-onboard` | ✅ |
| `/dross-rule` | ✅ |
| `/dross-spec` | ✅ |
| `/dross-plan` | ✅ |
| `/dross-plan-review` | ✅ |
| `/dross-execute` | ✅ |
| `/dross-verify` | ✅ |
| `/dross-status` | ✅ |

Legend: ✅ working · 🚧 stub / partial · ⏳ not started

## Roadmap

- [x] Skeleton: types, CLI, rules system, init/onboard, validate
- [x] Tests: round-trip, merge, parser, validate checks
- [x] `/dross-spec` and `/dross-plan` slash commands
- [x] `/dross-execute` (pair-mode default, `--solo` opt-in) + task/changes CLI helpers
- [x] `/dross-verify` + Stryker adapter for TS/JS/Svelte mutation testing
- [ ] Mutation adapters: Gremlins (Go), Stryker.NET (C#)
- [ ] Codex: tree-sitter indexer for TS/Svelte/Go/C#/GDScript/HTML/CSS
- [ ] GoReleaser cross-compile (darwin/arm64 primary)

## License

[AGPL-3.0](LICENSE).

## Acknowledgements

Dross is conceptually inspired by [GSD](https://github.com/gsd-build/get-shit-done) by TÂCHES (Lex Christopherson), distributed under the MIT License. No code or prompt text is copied; this is a clean Go reimplementation built around different design pivots (lean prompts, pair-mode execution, mutation testing as a first-class gate). If you want the full-featured, well-trodden tool, GSD is excellent — Dross is a fork of the *idea*, not the implementation.
