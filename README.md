# Dross

A leaner successor to [GSD](https://github.com/anthropics/get-shit-done) for working with Claude Code on real projects.

> **Status:** v0 skeleton — CLI builds and the rules system works end-to-end, but `execute`, `verify`, and the tree-sitter indexer are not implemented yet. Not ready for real use.

## Why

GSD is genuinely good at imposing planning discipline, but at a cost: ~3 MB of prompt material loaded across 65 skills, 33 subagents, and 76 workflows. A single `/gsd-plan-phase` invocation reads ~3,000 lines of instructions before doing anything. Subagent spawns multiply that.

Dross is a rebuild around three pivots:

1. **Lean prompts.** Target ≤300 lines per slash command. Most state lives in machine-parseable TOML, not prose Markdown.
2. **Pair-mode execute by default.** Code is authored *with* you, not delivered *to* you. Subagent spawns kept to genuinely independent work (parallel mutation runs, multi-language audits).
3. **Test efficacy as a first-class gate.** GSD checks that tests *exist*. Dross checks that tests *catch breakage* — via mutation testing (Stryker / Gremlins), coverage delta, and an LLM judge mapping each acceptance criterion to a specific test.

## The name

Dross is the AI sidekick from Will Wight's [Cradle](https://www.willwight.com/cradle) xianxia series — a Presence that lives in the protagonist's head, compiling battle plans, predicting opponents, crafting illusions, and handling "unimportant thoughts" to free up his bandwidth. Sarcastic, dramatic, fond of his person.

What makes the name fit:

- Dross began as a **discarded prototype** by the sage Northstrider (literal dross — the slag skimmed off refined metal) and was grown into something genuinely useful. Maps neatly onto "GSD trimmed down into what actually fits a workflow."
- His core job is **meta-cognition and planning** for his user. Same shape as what this tool does.

(Dross first appears in *Ghostwater*, book 5 of the series.)

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

| Command | Status |
|---|---|
| `dross init` | Bootstrap `.dross/` (greenfield) |
| `dross onboard` | Adopt an existing repo (signal scan) |
| `dross project {show,get,set}` | Read/write `project.toml` fields |
| `dross state {show,set,touch}` | Read/write `state.json` |
| `dross rule {add,list,remove,promote,disable,enable,show}` | Two-tier rules system |
| `dross phase {create,list,show}` | Phase directories |
| `dross milestone {create,list,show}` | Milestones |
| `dross profile {show,seed}` | User profile (with GSD import) |
| `dross validate` | Schema-check every artefact |
| `dross codex` | Polyglot code insight — **stub** |

Slash commands wired so far: `/dross-init`, `/dross-onboard`, `/dross-rule`.

## Roadmap

- [x] Skeleton: types, CLI, rules system, init/onboard, validate
- [ ] Tests: round-trip, merge, parser, validate checks
- [ ] `/dross-spec` and `/dross-plan` slash commands
- [ ] Codex: tree-sitter indexer for TS/Svelte/Go/C#/GDScript/HTML/CSS
- [ ] Mutation adapters: Stryker (TS), Gremlins (Go), Stryker.NET (C#)
- [ ] `dross execute` (pair-mode default, `--solo` opt-in)
- [ ] `dross verify` — mutation + coverage + criterion mapping
- [ ] GoReleaser cross-compile (darwin/arm64 primary)

## License

TBD.
