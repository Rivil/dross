# /dross-plan

Decompose a phase's `spec.toml` into a task graph with waves, dependencies, and per-task test contracts. Pair-mode: **propose → steer → write**. Never write `plan.toml` until the user has accepted the decomposition.

## 0. Pre-flight

1. Run `dross rule show` and treat output as MUST-FOLLOW.
2. Parse flags from `$ARGUMENTS`: `--panel` switches decomposition to panel mode (see §2P); `--no-review` skips the automatic plan review in §6. Strip both before resolving the phase id.
3. Resolve target phase: remaining `$ARGUMENTS` if provided, else `state.json`'s `current_phase`. If unset, list phases via `dross phase list` and ask.
4. Read `.dross/phases/<id>/spec.toml`. If missing, route the user to `/dross-spec` first and stop.
5. **Verify current branch is `phase/<id>`** (`git symbolic-ref --short HEAD`). On resume, switch with `git checkout phase/<id>` if it exists locally. If the phase branch is missing, stop — phase work belongs off main and `dross phase create` would have set this up.
6. Read `.dross/phases/<id>/plan.toml` if present — **resume mode**. Surface existing tasks, ask whether to extend or rewrite.

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

## 2P. Panel mode (`--panel`)

Replaces §2 when the flag is set: do **not** author the decomposition yourself. Three planners draft independently through deliberately different lenses, a cold judge merges them, and where they *disagree* becomes the steering agenda. Independence is the whole value — planners never see each other's drafts, and the judge authored none of them.

**When it's worth it:** a new subsystem, multiple plausible architectures, an expected task graph of 4+ tasks. Skip it (and say so) for small or pattern-following phases — panel mode costs roughly 4-5× a single-pass plan. If the user passed `--panel` on a phase that looks too small, flag it once, then respect their call.

### 2P.1 Fan out — three lens planners, one tool block

Spawn three subagents via the `Task` tool **in a single tool block** (`subagent_type: "general-purpose"`). Each gets the prompt below with its lens row substituted and absolute paths filled in. Where marked, paste in the full text of §2 (field table, granularity rules, test-contract quality) and §3's display example — the planners read their instructions cold and cannot see this file.

| lens | bias to substitute |
|---|---|
| `risk` | Failure modes drive the graph. Start from what can break — edge cases, concurrency, bad input, partial failure — and shape tasks so each risk is owned and tested by exactly one task. |
| `mvp` | Smallest task set that satisfies every criterion. Resist speculative structure; merge aggressively; every task must be traceable to a criterion or it goes. |
| `verification` | Design backward from the test contracts. Write each criterion's ideal test contract first, then derive the smallest task that makes that contract satisfiable. |

```
You are drafting a task decomposition for a phase, applying ONE deliberate bias:
<lens-bias>

Two other planners are drafting the same phase through different biases. You will
never see their drafts. Commit to your lens — a hedged middle-of-the-road plan is
useless to the judge who merges the three.

Read:
  <abs-path>/spec.toml          (criteria, locked decisions — NON-NEGOTIABLE)
  <repo>/.dross/project.toml    (paths.*, runtime, stack)
  <repo>/.dross/rules.toml      (MUST-FOLLOW)
Read existing files under paths.source when uncertain about concrete paths —
plans referencing files that don't exist are rejected.

Decomposition rules:
<full text of §2: field table, granularity rules, test-contract quality>

Write your draft to <abs-path>/panel/<lens>.md:
  1. The plan in this wave/task display format:
     <paste §3's display example here>
  2. "## Coverage" — criterion id → task ids, every criterion accounted for
  3. "## Judgment calls" — decisions where you chose between real alternatives,
     one line each: what you chose, what you rejected, why

Hard rules: do not write plan.toml; do not modify any file other than your draft;
locked decisions cannot be overridden; no task without a specific test_contract.

Return one line: "<lens>: N tasks across W waves, criteria covered X/Y".
```

### 2P.2 Join — one cold judge

After all three planners return (and only then — it reads their files), spawn a fourth subagent (`subagent_type: "general-purpose"`):

```
You are judging three task decompositions drafted independently through different
lenses (risk / mvp / verification). You authored none of them. Merge the best
plan out of them — and surface where they disagree instead of papering over it.

Read:
  <abs-path>/spec.toml, <repo>/.dross/project.toml, <repo>/.dross/rules.toml
  <abs-path>/panel/risk.md
  <abs-path>/panel/mvp.md
  <abs-path>/panel/verification.md

1. Score each draft — one line per draft per dimension: criteria coverage,
   test-contract specificity, granularity, wave correctness.
2. Pick the strongest as the skeleton.
3. Graft concrete improvements from the runners-up: a sharper contract, a missed
   file, a better wave split, a risk task the skeleton lacks. Never invent a task
   that appears in no draft.
4. Where the drafts genuinely diverge — different structure, different ordering,
   a task one planner needs and another rejects — do NOT silently resolve. Pick a
   provisional default for the merged plan and record the divergence.

Write <abs-path>/panel/synthesis.md:
  1. "## Scores" — the scoring table + one line naming the skeleton and why
  2. "## Merged plan" — wave/task display format, each task tagged with origin
     (e.g. "[risk]", "[mvp+verification]")
  3. "## Disagreements" — one entry per divergence: what diverged, which lens
     said what, the provisional default taken, why the choice matters

Hard rules: do not write plan.toml; do not modify the drafts or spec; no new
criteria; no tasks from outside the three drafts.

Return one line: "synthesis: N tasks across W waves, D disagreements".
```

### 2P.3 Present

Read `panel/synthesis.md`. Print the merged plan and the **full** disagreements list. The disagreements are the steering agenda — walk them with the user first (`AskUserQuestion` works well for binary divergences), then continue with §3 steering as normal. From here the flow is identical: coverage check → write → validate.

Leave `panel/` in place — it's the audit trail for why the plan looks the way it does.

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

Mirror the plan onto the issue board (no-op unless `[remote].board_sync` is on — safe to always run):
```
dross issue phase-sync <id>
```
This creates (or updates) the phase issue with the acceptance criteria and a task checklist rendered from `plan.toml`, assigned to the milestone's board entry.

### 6.1 Auto review (skip only on `--no-review`)

Unless `--no-review` was passed, run the independent plan review now — don't wait to be asked. Read `~/.claude/dross/prompts/plan-review.md` and follow it from its §1 (the phase id is already resolved). It spawns its own cold subagent to read the artifacts, so the review context stays fully isolated from this conversation; you only relay findings.

On the outcome:
- **BLOCKING findings** — don't end. Bring them back into §3 steering: propose amendments, steer, rewrite `plan.toml`, re-validate. Then re-run the reviewer **once**. If it still blocks, stop and surface — repeated author/reviewer ping-pong without the user is exactly what pair-mode forbids.
- **FLAG / NOTE only** — print the condensed list, let the user decide what's worth acting on.

If `--no-review` was passed, mention `/dross-plan-review` is available manually.

### 6.2 Close

End with one line:
```
Plan ready and reviewed (B blocking resolved, F flags in REVIEW.md). Next: /dross-execute to run the first task (pair-mode by default; pass --solo for autonomous).
```
(Adjust the parenthetical to match what actually happened — reviewed clean, flags pending, or review skipped via --no-review.)

## Hard rules

- **Pair-mode default.** Never write `plan.toml` before the user accepts the proposed decomposition. If the user wants autonomous mode, they'll say so explicitly.
- **Subagents: read-only fan-out is fine; authoring `plan.toml` is not.** Per the `dross-agent-gate` builtin, you may fan out subagents for read-only work — research, pattern-mapping, independent review — when it sharpens the plan, and should when it widens coverage or saves wall-clock. What stays gated is the decomposition itself: never let an unattended agent author or finalize `plan.toml` — it's agreed with the user (pair mode) or by you (`--solo`). The `--panel` flow (§2P) and the §6 plan-review are the worked examples: independent agents draft or critique, the user steers the result. For a small, pattern-following plan, staying inline is still the right call — fan out when it earns its keep, not by default.
- **Test contracts are mandatory.** A task without a `test_contract` is a task verify can't check. Refuse to write the plan with empty contracts unless the user explicitly accepts the gap.
- **Locked decisions from spec.toml are NON-NEGOTIABLE.** If the user proposes a task that contradicts one, surface the conflict and ask them to either revise the task or unlock the decision in `spec.toml` (with a `why`).
