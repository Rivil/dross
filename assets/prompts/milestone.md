# /dross-milestone

Scope a milestone — populate `.dross/milestones/<version>.toml` with title, success criteria, non-goals, and the ordered phase list. Run once per milestone; expect ~5-15 minutes depending on whether a `Brief.md`-style source doc exists.

## 0. Pre-flight

1. Run `dross rule show` and treat the output as MUST-FOLLOW for this session.
2. Resolve the milestone version from `$ARGUMENTS`:
   - `<version>` (e.g. `v0.1`, `v1.0`) → use it directly.
   - empty → ask the user via `AskUserQuestion`. Default to next minor if a previous milestone exists, else `v0.1`.
3. Determine create-vs-resume:
   - If `.dross/milestones/<version>.toml` does NOT exist → run `dross milestone create <version>`. This writes the skeleton with `status="planning"` and today's date.
   - If it DOES exist → run `dross milestone show <version>`, print the current state, and ask: "Milestone already scoped. Extend (add more criteria/phases) or replace (start over)?" If replace: delete `.dross/milestones/<version>.toml` and re-run create. If extend: continue from §3 onwards, skipping any field already populated unless the user wants to revise.

## 1. Read context

Surface the inputs that should shape the milestone, before asking questions:

- `.dross/project.toml` — `goals.core_value`, `goals.non_goals`, `goals.differentiators`, `stack.locked`. The milestone scope must fit inside the project's stated non-goals.
- `Brief.md` at the repo root, if present — for projects bootstrapped from a written brief, this is the single highest-signal input. Read it in full.
- `.dross/milestones/` listing — note prior milestones for naming consistency.

Print a short orientation block: "Scoping milestone `<version>`. Project core value: ... . Project non-goals (carry through to milestone): ... . Brief.md says the milestone should deliver: <one-line summary>."

## 2. Title

Ask via `AskUserQuestion`: **"One-line milestone title?"** (e.g. "Passing perft on six canonical positions", "First public auth release").

If `Brief.md` exists and contains an obvious milestone heading (e.g. `## Milestone — v0.1: <title>`), propose that as the default. The user accepts or overrides.

Save: `dross milestone set <version> milestone.title "<title>"`

## 3. Success criteria

The acceptance bar for "this milestone is done." Aim for 2-5 criteria — sharp, testable, observable from outside the system.

If `Brief.md` is present, extract candidates from any "Milestone done when:" / "Acceptance:" / "v0.1 complete when:" section. Propose them; user accepts/edits/adds.

Otherwise ask: **"What has to be true for this milestone to be considered done? 2-5 outcomes that you could write a test or observation for."**

**Quality bar — push back if a criterion fails any of these:**
- Not externally observable (e.g. "code is clean" — not testable)
- Phrased in implementation terms ("uses X library") instead of outcome ("returns correct count for canonical perft suite")
- Vague ("works well") instead of measurable ("perft suite passes at depth 5 in under 30s")

For each accepted criterion: `dross milestone add <version> scope.success_criteria "<criterion>"`

## 4. Non-goals

What this milestone explicitly will NOT do. Even one or two helps anchor scope.

If `project.toml` already has `goals.non_goals`, carry those forward by default. If `Brief.md` has a "Non-goals" section, extract from there too.

Ask: **"Anything that's intentionally out of scope for this milestone? (Things you might be tempted to build but shouldn't, until v.next.)"**

For each: `dross milestone add <version> scope.non_goals "<non-goal>"`

## 5. Phase breakdown

The ordered list of phases that, together, deliver the milestone. Each phase id is `NN-slug` (e.g. `01-board-fen`, `02-pseudolegal-moves`).

If `Brief.md` proposes phases (a "## Suggested phase breakdown" or "Phases:" section), surface them as the proposal. Otherwise propose 2-5 phases derived from the success criteria.

Show the proposed list. Ask: **"Confirm this phase order, or revise (add / remove / re-order / rename)?"**

When the user is happy, for each phase id in delivery order:
`dross milestone add <version> phases "<phase-id>"`

Note: this only registers the *names and order*. Phase directories themselves get created by `/dross-spec --new "<title>"` (which runs `dross phase create`) — don't create them here.

## 6. Activate

Promote `status` from `planning` → `active` and record the milestone as the current one in state.

```
dross milestone set <version> milestone.status active
dross state set current_milestone <version>
dross state touch "scoped milestone <version>: <N> criteria, <M> phases"
```

## 7. Wrap

Run `dross validate`. Should be green. Then print:

```
Milestone <version> scoped: <title>
  Success criteria: <N>
  Non-goals: <M>
  Phases: <first-id> → ... → <last-id>

Next:
  /dross-spec --new "<first-phase-title>"   — clarify the first phase
  dross milestone show <version>             — review what was just written
```

If the user supplied phase ids that already exist (rare, but possible when extending), point them at `/dross-spec <existing-id>` to resume those instead.

## Hard rules

- **Don't bypass the CLI.** Always write through `dross milestone set` / `dross milestone add` so validation runs and the toml stays canonical. Never edit `.dross/milestones/<version>.toml` directly from this command.
- **Don't create phase directories here.** Phase ids in `phases = [...]` are *intent*, not artefacts. `dross phase create` (via `/dross-spec --new`) is the only command that should write phase directories — keeps a single owner for that side effect.
- **Don't restate project non-goals as milestone non-goals.** If a non-goal already lives in `project.toml`, it applies project-wide; only add milestone-scoped non-goals here.
- **Resume-safe.** Re-running `/dross-milestone v0.1` on an existing milestone must never silently destroy data. Always show current state first and let the user choose extend vs replace.
