# /dross-status

One-line answer to "where am I?". Uses `dross status` for the mechanical read, surfaces it with light interpretation.

## Steps

1. Run `dross rule show` and treat output as MUST-FOLLOW (the user might have rules that override the default suggestion in step 3).
2. Run `dross status` and capture stdout.
3. Print the captured output verbatim — it's already formatted. Don't paraphrase.
4. After the status block, add one short paragraph (2-3 sentences max) that:
   - Names the most likely next action and why
   - Flags anything off (e.g. uncommitted git changes, failed tasks) the user should know about

If the user asks a follow-up like "show me failed tasks" or "what's the verify verdict", route to:
- `dross task show <phase> <task-id>` for task detail
- Read `.dross/phases/<phase-id>/verify.toml` for verdict + findings
- `dross changes show <phase>` for what's been touched

## Hard rules

- **Don't editorialise.** This command is meant to be quick — don't lecture about the project, don't suggest big architectural changes, don't write code. The user wants situational awareness, not a roadmap.
- **Don't make state mutations.** Status is read-only. No `dross state set`, no commits, no file writes.
- **Trust `dross status`'s suggestion.** The CLI's `next:` field is heuristic-driven from current state. If you disagree with it, mention why in your follow-up paragraph — but don't override it silently.
