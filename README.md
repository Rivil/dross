# Dross

A leaner successor to [GSD](https://github.com/gsd-build/get-shit-done) for working with Claude Code on real projects.

> **Status:** v0 skeleton — CLI builds and the rules system works end-to-end, but `execute`, `verify`, and the tree-sitter indexer are not implemented yet. Not ready for real use.

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

**Total prompt-surface** (everything that could ever load):

| | Bytes | Est. tokens |
|---|---:|---:|
| GSD (workflows + references + skills + agents) | 2,494,659 | ~624,000 |
| Dross (commands + prompts) | 25,980 | ~6,500 |
| **Ratio** | | **≈ 96×** |

**Being honest about these numbers:**

- **Dross is incomplete.** `/dross-execute` and `/dross-verify` are still pending. The hard work — and the bulk of the remaining prompt — is still ahead. A finished `/dross-execute` will likely land at ~2,000-3,000 tokens (still ~15-20× cheaper than GSD's 46k, but the ratio narrows as more commands ship).
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
internal/          project, state, rules, profile, phase, milestone, codex, mutation
assets/commands/   Slash command markdown (installed to ~/.claude/dross/commands/)
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

### Global artefacts

```
~/.claude/dross/
├── profile.toml      # cross-project user profile
├── rules.toml        # cross-project rules
├── commands/
└── prompts/
```

## Build

```sh
make build       # builds ./dross for current arch
make install     # builds + installs to ~/.local/bin and links assets to ~/.claude/dross
make test        # go test ./...
```

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
| `dross profile {show,seed}` | User profile (with GSD import) | ✅ |
| `dross validate` | Schema-check every artefact | ✅ |
| `dross codex` | Polyglot code insight (tree-sitter) | 🚧 |
| `dross execute` | Pair-mode phase execution | ⏳ not started |
| `dross verify` | Mutation + coverage + criterion mapping | ⏳ not started |

**Slash commands:**

| Command | Status |
|---|:---:|
| `/dross-init` | ✅ |
| `/dross-onboard` | ✅ |
| `/dross-rule` | ✅ |
| `/dross-spec` | ✅ |
| `/dross-plan` | ✅ |
| `/dross-plan-review` | ✅ |
| `/dross-execute` | ⏳ not started |
| `/dross-verify` | ⏳ not started |

Legend: ✅ working · 🚧 stub / partial · ⏳ not started

## Roadmap

- [x] Skeleton: types, CLI, rules system, init/onboard, validate
- [x] Tests: round-trip, merge, parser, validate checks
- [x] `/dross-spec` and `/dross-plan` slash commands
- [ ] Codex: tree-sitter indexer for TS/Svelte/Go/C#/GDScript/HTML/CSS
- [ ] Mutation adapters: Stryker (TS), Gremlins (Go), Stryker.NET (C#)
- [ ] `dross execute` (pair-mode default, `--solo` opt-in)
- [ ] `dross verify` — mutation + coverage + criterion mapping
- [ ] GoReleaser cross-compile (darwin/arm64 primary)

## License

[AGPL-3.0](LICENSE).

## Acknowledgements

Dross is conceptually inspired by [GSD](https://github.com/gsd-build/get-shit-done) by TÂCHES (Lex Christopherson), distributed under the MIT License. No code or prompt text is copied; this is a clean Go reimplementation built around different design pivots (lean prompts, pair-mode execution, mutation testing as a first-class gate). If you want the full-featured, well-trodden tool, GSD is excellent — Dross is a fork of the *idea*, not the implementation.
