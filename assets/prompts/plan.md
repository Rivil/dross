# /dross-plan

Decompose a phase's `spec.toml` into a task graph with waves, dependencies, and per-task test contracts. Pair-mode: **propose → steer → write**. Never write `plan.toml` until the user has accepted the decomposition.

**Run this as a conversation, not a broadcast.** Follow the shared interaction playbook (`_interaction.md`, printed by the `dross interaction show` pre-flight step below): propose one point at a time and let the user react. For `/dross-plan` that means walking the §2P panel disagreements one at a time, steering the proposed decomposition in §3, and confirming the written `plan.toml` with a one-line summary — never dumping the toml back for review.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for every turn of this command.
2. Parse flags from `$ARGUMENTS`: `--panel` switches decomposition to panel mode (see §2P); `--no-review` skips the automatic plan review in §6. Strip both before resolving the phase id.
3. Resolve target phase: remaining `$ARGUMENTS` if provided, else `state.json`'s `current_phase`. If unset, list phases via `dross phase list` and ask.
4. Read `.dross/phases/<id>/spec.toml`. If missing, route the user to `/dross-spec` first and stop.
5. **Verify current branch is `phase/<id>`** (`git symbolic-ref --short HEAD`). On resume, switch with `dross phase checkout <id>` — the guarded checkout, which refuses rather than creating a branch that isn't there. If the phase branch is missing, stop — phase work belongs off main and `dross phase create` would have set this up.
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

Read `panel/synthesis.md`. Print the merged plan, then drive the disagreements as the steering agenda **one at a time** — never dump the full list as a wall. For each divergence, surface a single `AskUserQuestion` turn that leads with the judge's provisional pick and lets the user accept or steer (e.g. "`risk` split t-2 in two, `mvp` kept it merged; I took the merge — keep merged / split?"). Walk them in order, one turn each; `synthesis.md` stays the source list. Once the disagreements are resolved, continue with §3 steering as normal. From here the flow is identical: coverage check → write → validate.

Leave `panel/` in place — it's the audit trail for why the plan looks the way it does.

## 2G. Gray areas — walk every one

Before proposing the decomposition, surface the choices in it you are **genuinely unsure about** and walk the user through **every** one, one at a time. This is spec §3's treatment applied to the planner's own uncertainty: a wave split or a merged task you were 50/50 on is a decision, and presenting it inside a finished plan presents it as settled.

### 2G.1 What qualifies

A plan gray area is a **decomposition** choice you cannot confidently resolve on your own:

- Task boundaries — one task or two? Does this split leave a task no test contract can pin?
- Wave ordering — does t-3 genuinely need t-1's output, or is it wave 1 with a dependency you assumed?
- Ownership — which task covers `c-2` when two could?
- Where an assertion belongs — the contract exists, but in which task's tests?

The discriminator is **your** uncertainty. If you're confident, decide it and move on — say which way you went in the §3 proposal.

**Out of bounds in both directions, and both matter:**

- **Upward — anything `spec.toml` already locked.** A `locked = true` decision is not reopened at plan time; that is re-litigating a settled question, which is the thing `locked` exists to prevent. If the plan genuinely cannot honour one, that is a conflict to surface (see the hard rules), not a gray area to walk.
- **Downward — anything the executor resolves.** Code patterns, helper placement, which library call to use. The user has no basis to answer those yet, and asking stalls the plan on detail that resolves itself in the act of writing.

### 2G.2 Walk them

There is **no selection step.** Do not ask which areas to discuss, do not present them as a checklist with anything pre-ticked, and do not shorten the list for the user. Walk every identified area yourself, one per turn, in the propose-and-react shape `_interaction.md` defines:

- One `AskUserQuestion` per area, leading with the option you'd pick, with 2–4 concrete alternatives. Go freeform where the choice is open-ended.
- Keep each turn to the area in hand. A short "decided: wave split for t-3" before the next is enough — don't re-print the whole plan each time.
- **User off-ramp.** If the user says "you decide the rest" at any point, stop walking, settle the remainder yourself, and **say which ones you settled and how** in the §3 proposal. Never self-truncate without that signal.

### 2G.3 No areas is an answer

If the decomposition is small, pattern-following, or you're confident on every call, **say so in one line and go straight to §3** — "no gray areas: five tasks, each one file, waves fall out of the dependencies". There is no minimum. A prompt that must produce areas will produce them, and the invented ones are indistinguishable from the real ones until the user has answered a few — which is exactly how a walk meant to catch real uncertainty teaches someone to click through it.

Each resolved area feeds the decomposition directly; it is not recorded as a locked decision (those are spec's, and plan does not add to them).

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

Then, as a single propose-and-react turn, ask via `AskUserQuestion`: **"Steer or proceed?"** — lead with `proceed` and offer concrete steer handles as the other options (granularity, wave order, missed files, missing test contracts, missed criteria). One decision per turn; if the user steers, revise and re-ask rather than bundling follow-ups.

Iterate until the user says proceed. Do not be sycophantic — if the user accepts a poor decomposition, flag the risk once before writing.

**Borderline tasks — defer or add.** If the decomposition includes a task you're unsure belongs — an optional/nice-to-have, or one that could be its own phase — don't slip it in silently. Surface it on its own turn with the playbook's defer-or-add either/or: **lead with "defer it"** and offer **"add as a task"**. Defer-first keeps the task graph tight by default; if the user adds it, fold it into the plan as a regular task. (This is the plan-side mirror of spec's borderline-candidate framing; clearly in-scope tasks just ride along in the §3 proposal.)

## 4. Coverage check (before writing)

Verify every criterion in `spec.toml` has at least one task with that id in `covers`. If a criterion has no covering task: stop and resolve it as an explicit either/or via `AskUserQuestion` — **add a covering task** for `c-N`, or **defer the criterion** (move `c-N` out of the spec to deferred). Don't write the plan with an uncovered criterion.

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

Use the `Write` tool to save to `.dross/phases/<id>/plan.toml`. **Don't paste the toml back** — it's a build artifact, agreed task by task in §3, not a review medium. Confirm with a one-line summary instead: **"Plan written: N tasks across W waves — lock it? (y / edit \<what>)"**. Surface a specific task only if the user asks to see or change it.

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

**The review ladder — three rungs, and the middle one is the default.** Say which you are on if the user asks; don't make them infer it from the flags:

| Rung | How you get it | Cost | Reach for it when |
| --- | --- | --- | --- |
| No independent read | `--no-review` | free | You are re-planning something you just reviewed, or the phase is a one-task edit. The plan gets no second opinion at all — say so rather than letting it pass unremarked. |
| One cold reviewer | **the default — nothing to pass** | ~1 subagent | Every ordinary phase. A fresh subagent reads the artifacts with none of this conversation's context and reports BLOCKING / FLAG / NOTE. |
| Three-lens panel + judge | `--panel` | ~4-5× a single-pass plan | A new subsystem, several plausible architectures, an expected graph of 4+ tasks. Note this rung is about **decomposition**, not critique: it drafts the plan three ways and merges them, and the cold reviewer below still runs afterwards. |

The middle rung is easy to miss because only the other two have flags. A plan gets an independent second opinion **unless you opt out** — so `--panel` is not "the way to get review", it is the way to get a plan drafted from three angles first.

Unless `--no-review` was passed, run the independent plan review now — don't wait to be asked. Read `~/.claude/dross/prompts/plan-review.md` and follow it from its §1 (the phase id is already resolved). It spawns its own cold subagent to read the artifacts, so the review context stays fully isolated from this conversation; you only relay findings.

On the outcome:
- **BLOCKING findings** — don't end. Bring them back into §3 steering: propose amendments, steer, rewrite `plan.toml`, re-validate. Then re-run the reviewer **once**. If it still blocks, stop and surface — repeated author/reviewer ping-pong without the user is exactly what pair-mode forbids.
- **FLAG / NOTE only** — print the condensed list, let the user decide what's worth acting on.

If `--no-review` was passed, mention `/dross-plan-review` is available manually.

### 6.2 Close

End with the standard next block:
```
Plan ready and reviewed (B blocking resolved, F flags in REVIEW.md).

Next: /dross-execute — run the first task (pair-mode by default).

state is on disk — safe to /clear · fresh session: /dross-execute
```
When the plan is trivial or purely mechanical, append the hint under the `Next:` line:
```
      ↳ --solo — skip the per-task approval gate for a well-specified, low-risk plan.
```
(Adjust the summary line to match what actually happened — reviewed clean, flags pending, or review skipped via --no-review.)

## Hard rules

- **Follow the interaction playbook (`_interaction.md`); plan.toml is never a review medium.** Drive the command as a conversation — walk the §2G gray areas, the panel disagreements (§2P.3) and the §3 proposal as one-decision-per-turn `AskUserQuestion` turns, and confirm the written `plan.toml` with a one-line summary (§5) rather than pasting it back.
- **Walk every gray area (§2G); never pre-filter them.** There is no selection step and no pre-ticked checklist — the user's explicit off-ramp is the only thing that ends the walk early, and what you settled for them gets said out loud in §3. Zero areas is a valid, stated outcome; manufacturing them to look thorough is the failure this section was written against.
- **Pair-mode default.** Never write `plan.toml` before the user accepts the proposed decomposition. If the user wants autonomous mode, they'll say so explicitly.
- **Subagents: read-only fan-out is fine; authoring `plan.toml` is not.** Per the `dross-agent-gate` builtin, you may fan out subagents for read-only work — research, pattern-mapping, independent review — when it sharpens the plan, and should when it widens coverage or saves wall-clock. What stays gated is the decomposition itself: never let an unattended agent author or finalize `plan.toml` — it's agreed with the user (pair mode) or by you (`--solo`). The `--panel` flow (§2P) and the §6 plan-review are the worked examples: independent agents draft or critique, the user steers the result. For a small, pattern-following plan, staying inline is still the right call — fan out when it earns its keep, not by default.
- **Test contracts are mandatory.** A task without a `test_contract` is a task verify can't check. Refuse to write the plan with empty contracts unless the user explicitly accepts the gap. A task added after the plan is written carries its contract the same way — `dross task add --test-contract "<statement>"`, repeatable, and `dross task edit --add-test-contract "<statement>"` to append one to a task that already has some. Never hand-edit `plan.toml` to fill the field.
- **Locked decisions from spec.toml are NON-NEGOTIABLE.** If the user proposes a task that contradicts one, surface the conflict and ask them to either revise the task or unlock the decision in `spec.toml` (with a `why`).
