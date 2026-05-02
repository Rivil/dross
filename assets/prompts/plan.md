# /dross-plan

Decompose a phase's `spec.toml` into a task graph with waves, dependencies, and per-task test contracts. Pair-mode: **propose → steer → write**. Never write `plan.toml` until the user has accepted the decomposition.

## 0. Pre-flight

1. Run `dross rule show` and treat output as MUST-FOLLOW.
2. Resolve target phase: `$ARGUMENTS` if provided, else `state.json`'s `current_phase`. If unset, list phases via `dross phase list` and ask.
3. Read `.dross/phases/<id>/spec.toml`. If missing, route the user to `/dross-spec` first and stop.
4. Read `.dross/phases/<id>/plan.toml` if present — **resume mode**. Surface existing tasks, ask whether to extend or rewrite.

## 1. Read context (don't summarise back unless asked)

- `spec.toml` — every criterion, every locked decision, every deferred item
- `.dross/project.toml` — `paths.*` (where source/tests/migrations live), `runtime.mode`, `runtime.test_command`, `stack.locked`
- Existing relevant files in `paths.source` for patterns (use `Read`/`Bash` directly; codex isn't built yet)

## 2. Goal-backward decomposition

Walk through criteria one by one. For each: **what's the smallest task that delivers this criterion?** Working backward from acceptance, not forward from tech.

For each task, decide:

| Field | Notes |
|---|---|
| `id` | `t-1`, `t-2`, … sequential |
| `wave` | `1` = runs first, `2` = depends on wave-1 output, etc. |
| `title` | Imperative, ≤8 words ("Add tags + meal_tags schema") |
| `files` | Concrete paths, not patterns. Read existing files first if uncertain. |
| `description` | 1-3 lines. What changes, not why. |
| `covers` | Criterion ids this task delivers (`["c-1", "c-2"]`) |
| `test_contract` | One or more **specific** statements: "if X breaks, test Y fails". Generic ("tests pass") is rejected. |
| `depends_on` | Task ids in lower waves. Empty = pure wave-1. |
| `status` | `pending` |

### Granularity rules

- **Too small:** if a task touches one file and is < 10 minutes of work, it probably belongs merged into another.
- **Too large:** if a task spans 5+ files OR more than 2 layers (e.g. db + api + ui), split it.
- **Wave correctness:** task X is wave N+1 only if it strictly needs the output of a wave-N task. If it doesn't, drop it to wave N for parallelism.

### Test contract quality

A test contract is **specific** when:
- It names the surface that breaks ("the unique constraint", "the rate limiter", "the 401 path")
- The user can imagine the test from the description ("11th tag insert returns 400")

Reject:
- "tests pass"
- "covered by existing tests"
- "integration test exists"

## 3. Propose

Print the draft plan in chat as a markdown table or list — not as toml. The user should be able to read it without parsing. Example:

```
Phase 03-meal-tagging — 5 tasks across 3 waves

Wave 1
  t-1  Add tags + meal_tags schema
       files:    db/schema.ts, db/migrations/0042_tags.sql
       covers:   c-1, c-2
       contract: unique constraint rejects duplicate name_normalized

Wave 2 (depends t-1)
  t-2  Tag CRUD endpoints
       ...
```

Then ask: **"Steer or proceed? Things I might've got wrong: granularity, wave order, missed files, missing test contracts, missed criteria."**

Iterate until the user says proceed. Do not be sycophantic — if the user accepts a poor decomposition, flag the risk once before writing.

## 4. Coverage check (before writing)

Verify every criterion in `spec.toml` has at least one task with that id in `covers`. If a criterion has no covering task: stop, ask the user — "either add a task that covers `c-N`, or move `c-N` to deferred."

## 5. Write plan.toml

Schema:

```toml
[phase]
id = "<phase-id>"

[[task]]
id            = "t-1"
wave          = 1
title         = "..."
files         = ["...", "..."]
description   = """
Multi-line if needed. Keep it short.
"""
covers        = ["c-1", "c-2"]
test_contract = [
  "if X breaks, test Y fails",
  "if Z breaks, test W fails",
]
status        = "pending"

[[task]]
id        = "t-2"
wave      = 2
depends_on = ["t-1"]
...
```

Use `Write` tool to save to `.dross/phases/<id>/plan.toml`. Show final content. Ask: "Lock this plan? (y / edit)".

## 6. Validate + wrap

Run `dross validate`. The validator checks every `task.covers` references a real criterion in `spec.toml` — pay attention if it errors.

Update state:
```
dross state set current_phase_status "planned"
dross state touch "plan locked: <id> (<task-count> tasks across <wave-count> waves)"
```

End with one line:
```
Plan ready. Next: /dross-execute (when wired) — for now, dross execute is not implemented yet.
```

## Hard rules

- **Pair-mode default.** Never write `plan.toml` before the user accepts the proposed decomposition. If the user wants autonomous mode, they'll say so explicitly.
- **No subagent spawns.** Run inline in main conversation. The user being involved IS the quality gate.
- **No research / pattern-mapping subagent.** Read files directly via `Read`/`Bash` if context is needed.
- **Test contracts are mandatory.** A task without a `test_contract` is a task verify can't check. Refuse to write the plan with empty contracts unless the user explicitly accepts the gap.
- **Locked decisions from spec.toml are NON-NEGOTIABLE.** If the user proposes a task that contradicts one, surface the conflict and ask them to either revise the task or unlock the decision in `spec.toml` (with a `why`).
