# /dross-plan

Decompose a phase's `spec.toml` into a task graph with waves, dependencies, and per-task test contracts. Pair-mode: **propose → steer → write**. Never write `plan.toml` until the user has accepted the decomposition.

**Run this as a conversation, not a broadcast.** Follow the interaction playbook (`_interaction.md`, printed by `dross interaction show` below): walk any panel disagreements one at a time, steer the §3 decomposition, and confirm the written `plan.toml` with a one-line summary asked as an `AskUserQuestion` — never dump the toml back.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for every turn of this command.
2. Parse flags from `$ARGUMENTS`: `--panel` switches decomposition to panel mode (its steps are fetched after §2 when `panel=on`); `--no-review` skips the automatic plan review in §6. Strip both before resolving the phase id.
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

**When** `panel=on` — `--panel` was passed: `dross prompt partial plan panel`

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

**Borderline tasks — include-first.** A task that serves a spec criterion just rides along in the §3 proposal — don't ask a defer question about it. If the decomposition includes a task you're genuinely unsure belongs — an optional/nice-to-have — don't slip it in silently: surface it on its own turn as an either/or that **leads with "add as a task"** and offers **"defer it"**. Only a task that is clearly its own phase leads with "defer it", with the reason in the option. Deferral is not the safe default; if the user adds it, fold it into the plan as a regular task. (This is the plan-side mirror of the playbook's include-first rule.)

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

Use the `Write` tool to save to `.dross/phases/<id>/plan.toml`. **Don't paste the toml back** — it's a build artifact, agreed task by task in §3, not a review medium. Confirm with a one-line summary instead, asked as a single `AskUserQuestion` — never as a trailing prose line the user has to type an answer to: **"Plan written: N tasks across W waves — lock it?"** — lead option **lock**, other option **edit \<what>**. Surface a specific task only if the user asks to see or change it.

## 6. Validate + wrap

Run `dross validate`. The validator checks every `task.covers` references a real criterion in `spec.toml` — pay attention if it errors.

Update state:
```
dross state set current_phase_status "planned"
dross state touch "plan locked: <id> (<task-count> tasks across <wave-count> waves)"
```

Mirror the plan onto the issue board (it exits 0 and does nothing when `[board].enabled` is false; a non-zero exit is a board failure — surface it, never read it as the disabled no-op):
```
dross issue phase sync <id>
```
This creates (or updates) the phase issue with the acceptance criteria and a task checklist rendered from `plan.toml`, assigned to the milestone's board entry.

### 6.1 Auto review (skip only on `--no-review`)

**The review ladder — three rungs, and the middle one is the default.** Say which you are on if the user asks; don't make them infer it from the flags:

| Rung | How you get it | Cost | Reach for it when |
| --- | --- | --- | --- |
| No independent read | `--no-review` | free | Re-planning something just reviewed, or a one-task edit. Say the plan got no second opinion. |
| One cold reviewer | **the default — nothing to pass** | ~1 subagent | Every ordinary phase: a fresh subagent with none of this context reports BLOCKING / FLAG / NOTE. |
| Three-lens panel + judge | `--panel` | ~4-5× a single-pass plan | A new subsystem, several plausible architectures, 4+ tasks. It is about **decomposition**, not critique: three drafts merged; the cold reviewer still runs after. |

A plan gets an independent second opinion **unless you opt out**; `--panel` is not "the way to get review".

Unless `--no-review` was passed, run the independent plan review now — don't wait to be asked. Read `~/.claude/dross/prompts/plan-review.md` and follow it from its §1 (phase id already resolved). It spawns its own cold subagent; you only relay findings.

On the outcome:
- **BLOCKING findings** — don't end. Bring them back into §3 steering: propose amendments, steer, rewrite `plan.toml`, re-validate. Then re-run the reviewer **once**. If it still blocks, stop and surface — no author/reviewer ping-pong without the user.
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

- **Follow the interaction playbook (`_interaction.md`); plan.toml is never a review medium.** The §2G gray areas, any panel disagreements and the §3 proposal are one-decision-per-turn `AskUserQuestion` turns; confirm the written `plan.toml` per §5, never by pasting it or a prose "(y / edit)" line.
- **Walk every gray area (§2G); never pre-filter them.** No selection step, no pre-ticked checklist — only the user's explicit off-ramp ends the walk early, and what you settled for them is said out loud in §3. Zero areas is a valid, stated outcome; never manufacture them.
- **Pair-mode default.** Never write `plan.toml` before the user accepts the proposed decomposition.
- **Subagents: read-only fan-out is fine; authoring `plan.toml` is not.** Per `dross-agent-gate`, fan out subagents for read-only work (research, pattern-mapping, independent review) when it widens coverage or saves wall-clock. Never let an unattended agent author or finalize `plan.toml` — it's agreed with the user (pair mode) or by you (`--solo`). The `--panel` flow and the §6 review are the worked examples. A small, pattern-following plan stays inline.
- **Test contracts are mandatory.** A task without a `test_contract` is a task verify can't check. Refuse to write the plan with empty contracts unless the user explicitly accepts the gap. A task added later carries its contract too — `dross task add --test-contract "<statement>"` (repeatable), or `dross task edit --add-test-contract "<statement>"` to append one. Never hand-edit `plan.toml` to fill the field.
- **Locked decisions from spec.toml are NON-NEGOTIABLE.** If the user proposes a task that contradicts one, surface the conflict and ask them to either revise the task or unlock the decision in `spec.toml` (with a `why`).
