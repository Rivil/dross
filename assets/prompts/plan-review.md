# /dross-plan-review

Run an **independent** review of a phase's `plan.toml`. The author of the plan (you, in this conversation) is too close to it for a fair audit, so this command spawns a fresh subagent that reads the artifacts cold.

Output: `.dross/phases/<id>/REVIEW.md` with findings classified BLOCKING / FLAG / NOTE.

## 0. Resolve target

Resolve phase id from `$ARGUMENTS` or `state.json`'s `current_phase`. Fail fast if neither `spec.toml` nor `plan.toml` exist for that phase — there's nothing to review.

## 1. Spawn the reviewer

Use the `Task` tool with `subagent_type: "general-purpose"`. Pass the prompt below verbatim, with `<phase-id>` and absolute paths substituted in. Do **not** truncate the prompt — the reviewer must read its own instructions cold.

```
You are reviewing a plan.toml independently. The author has just written it; your job is to find what they missed. Be honest, not sycophantic. A clean review with no findings is fine if the plan is genuinely good — don't manufacture problems.

Read these files:
  <abs-path>/spec.toml
  <abs-path>/plan.toml
  <repo>/.dross/project.toml
  <repo>/.dross/rules.toml
  <home>/.claude/dross/rules.toml  (if it exists — global rules)

Apply these checks. For each finding, classify severity:
  BLOCKING — plan should not proceed until fixed
  FLAG     — author should consider; not blocking
  NOTE     — observation worth recording; no action required

Checks:

  1. Coverage. Every criterion in spec.toml must appear in at least one task's `covers` field. Missing coverage = BLOCKING.

  2. Locked-decision conflicts. If any task description, files, or test_contract contradicts a `locked = true` decision in spec.toml, that is BLOCKING.

  3. Test contract specificity. Reject vague contracts: "tests pass", "covered by integration", "existing tests verify". A specific contract names the surface that breaks ("the unique constraint", "the 401 path", "the rate limiter"). Vague = FLAG.

  4. Granularity.
       - Tasks touching 5+ files OR spanning 3+ layers (db + api + ui) — FLAG (split candidate).
       - Tasks with one file and < 10 minutes of work — FLAG (merge candidate).

  5. Wave order. A task in wave N+1 must strictly need output from a wave-N task. If it doesn't, it could drop to wave N for parallelism — FLAG.

  6. Antipatterns common in LLM-authored plans:
       - "set up X" or "configure Y" tasks with no concrete files — FLAG.
       - Two tasks that should be one (artificial split for granularity inflation) — FLAG.
       - One task that should be two (squashed for brevity) — FLAG.
       - Files referenced that don't exist in the repo and aren't created by an earlier task in the plan — FLAG.

  7. Forbidden actions. Cross-reference rules.toml (project + global). If any task implies a violation (e.g. running pnpm directly when runtime.mode is docker), BLOCKING.

  8. Strengths. Note 1-3 things the plan got right. Useful for the author and for calibrating future plans. Tag NOTE.

Write findings to <abs-path>/REVIEW.md in this format:

  # Plan Review — <phase-id>

  Reviewed: <YYYY-MM-DD>
  Plan: <task-count> tasks across <wave-count> waves

  ## BLOCKING
  - [<check-name>] <finding>
    Suggestion: <what to do>

  ## FLAG
  - [<check-name>] <finding>
    Suggestion: <what to do>

  ## NOTE
  - [<check-name>] <observation>

  ## Summary
  <one-sentence overall verdict>

If a section has no findings, write "(none)". Do not omit empty sections — the structure aids skimming.

After writing, return a one-line summary: "<N> blocking, <M> flags, <K> notes — see REVIEW.md".

Hard rules:
  - Do not modify spec.toml, plan.toml, or any source code.
  - Do not propose new criteria, new tasks, or rewrite the plan. You're a reviewer, not an editor.
  - Do not be diplomatic to the point of vagueness. "This task could be more specific" is useless; "task t-2's contract 'tests pass' doesn't specify what behaviour breaks" is useful.
```

## 2. Surface findings

When the subagent returns, read `REVIEW.md`. Print to the user:

- The one-line summary the subagent returned
- The full BLOCKING section (if any)
- A condensed FLAG list (1 line per item, no suggestions — point them at the file for detail)

Don't dump the whole REVIEW.md inline; let the user open the file if they want detail.

## 3. Wrap

If BLOCKING findings exist, recommend re-running `/dross-plan` with the spec/findings in mind:
```
This plan has N blocking issues. Re-run /dross-plan to address them, or open REVIEW.md to read full detail.
```

If only FLAG/NOTE: recommend the user skim and decide:
```
Plan is reviewable. M flags + K notes captured in REVIEW.md. Decide what's worth acting on, then proceed to /dross-execute when ready.
```

Update state:
```
dross state touch "plan reviewed: <phase-id> (B blocking, F flags)"
```

## Hard rules for this orchestrator

- **Do not edit the subagent's review.** The whole point is independence. If you disagree, that's a conversation to have with the user, not an edit to bury.
- **Spawn exactly one subagent.** No chains, no checker-of-checker loops.
- **Don't auto-fix.** Even if the user says "fix the blocking ones," route them to `/dross-plan` — that's the right command for plan edits.
