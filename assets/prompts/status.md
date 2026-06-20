# /dross-status

One-line answer to "where am I?". Uses `dross status` for the mechanical read, surfaces it with light interpretation.

## Steps

1. Run `dross rule show` and treat output as MUST-FOLLOW (the user might have rules that override the default suggestion in step 3).
2. Run `dross status` and capture stdout.
3. Print the captured output verbatim — it's already formatted. Don't paraphrase.
4. Run `dross issue pull --labels bug,enhancement --json` (read-only; emits `[]` when board sync is off or there's nothing new). If it returns a non-empty array, add a one-line passive nudge under the status block: `inbox: N new board issue(s) — /dross-inbox to triage`. Never block on this; a board/network error here is non-fatal — skip the nudge silently.
5. After the status block, add one short paragraph (2-3 sentences max) that:
   - Names the most likely next action and why
   - Flags anything off (e.g. uncommitted git changes, failed tasks) the user should know about
6. **End with a bottom-anchored `Next:` line** mirroring `dross status`'s own `next:` field, so the suggested command is the last thing on screen. When that next command has a flag worth surfacing for the current state, append a `↳ --flag — <when>` hint under it (e.g. `/dross-plan` → `↳ --panel` for a new-subsystem phase; `/dross-verify` → `↳ --skip-mutation` when nothing measurable changed; `/dross-execute` → `↳ --from <task-id>` to resume mid-phase). Only surface flags the target command actually accepts — never invent one.

If the user asks a follow-up like "show me failed tasks" or "what's the verify verdict", route to:
- `dross task show <phase> <task-id>` for task detail
- Read `.dross/phases/<phase-id>/verify.toml` for verdict + findings
- `dross changes show <phase>` for what's been touched

## Hard rules

- **Don't editorialise.** This command is meant to be quick — don't lecture about the project, don't suggest big architectural changes, don't write code. The user wants situational awareness, not a roadmap.
- **Don't make state mutations.** Status is read-only. No `dross state set`, no commits, no file writes.
- **Trust `dross status`'s suggestion.** The CLI's `next:` field is heuristic-driven from current state. If you disagree with it, mention why in your follow-up paragraph — but don't override it silently.
