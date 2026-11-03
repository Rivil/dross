## 2P. Panel mode (`--panel`)

Under `--panel`, do **not** author §2's decomposition yourself. Three planners draft independently through deliberately different lenses, a cold judge merges them, and where they *disagree* becomes the steering agenda. Independence is the whole value — planners never see each other's drafts, and the judge authored none of them.

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
