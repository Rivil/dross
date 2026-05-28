# /dross-spec

Clarify what a phase delivers. Produces `.dross/phases/<id>/spec.toml`.

## 0. Pre-flight

1. Run `dross rule show` and treat the output as MUST-FOLLOW for this session.
2. Resolve the target phase from `$ARGUMENTS`:
   - `--new "Phase Title"` → run `dross phase create "<title>"`, capture the new id (`NN-slug`).
   - `<phase-id>` → use it directly. Fail if `.dross/phases/<id>/` doesn't exist.
   - empty → read `.dross/state.json`'s `current_phase` and decide:
     - **set and in-progress** (spec.toml exists but no plan, OR plan exists but verify isn't `pass`) → use it directly (resume mode below). No question asked.
     - **set, but the phase looks done** (i.e. `current_phase_status` is `verified` or `shipped`, OR `.dross/phases/<current>/verify.toml` exists with `verdict = "pass"`) → there's nothing to spec on `<current>`. `AskUserQuestion`: **"Phase `<current>` is `<status>` — nothing left to spec. Create a new phase?"** options: `new` / `resume <current>` / `cancel`. On `new`, jump to the create flow below. On `resume`, use `<current>` anyway (rare; user wants to amend a locked spec). On `cancel`, stop and exit cleanly.
     - **unset** → there's nothing for this command to do. `AskUserQuestion`: **"No phase in progress. Create a new phase?"** options: `new` / `cancel`. On `cancel`, stop and exit cleanly. On `new`, jump to the create flow below.

   **Create flow** (used by `--new`, the `new` answer above, or whenever scaffolding is needed):
   - If `state.current_milestone` is set, read `.dross/milestones/<milestone>.toml`'s `phases = [...]` and intersect against `dross phase list`. Any roadmap entry without a `.dross/phases/<id>/` directory is an **unscaffolded roadmap phase**.
   - If there are unscaffolded entries, present them via `AskUserQuestion`: one option per entry (label `<id>`; description = the entry's one-line summary if a `Brief.md` at repo root contains a matching `### Phase <id>` section, otherwise the bare title). Last option is **"Describe a new phase"** (freeform).
   - If the user picks a roadmap entry, the title is the entry's slug (or its Brief.md title if present) — run `dross phase create "<title>"`.
   - If the user picks **Describe a new phase**, prompt for a freeform title, then run `dross phase create "<title>"`.
   - If milestone is unset, roadmap is empty, or all roadmap entries are already scaffolded, fall back to the freeform title prompt directly.

   In every case: capture the new id (`NN-slug`) and proceed. Do NOT tell the user to run `dross phase create` manually — this command runs it. `dross phase create` also checks out the `phase/<id>` branch.
3. **Verify current branch is `phase/<id>`** for the resolved phase (`git symbolic-ref --short HEAD`). For a freshly-created phase this is already true. On resume, if you're not on the phase branch: `git checkout phase/<id>` if it exists, otherwise stop and surface the situation to the user (phase work belongs off main).
4. Read `.dross/phases/<id>/spec.toml` if it exists. **Resume, don't overwrite.** Show the existing content and ask whether to extend or replace.

## 1. Read context

Read these and surface their relevant bits to the user before asking questions:

- `.dross/project.toml` — `goals.core_value`, `goals.non_goals`, `stack.locked`, `runtime.mode`
- `.dross/milestones/<milestone>.toml` if `state.current_milestone` is set — milestone success criteria + non-goals constrain what this phase should accept
- Any spec.toml in the phase dir already

Print a short orientation block: "Working on phase X. Project core value: Y. Milestone success criteria: Z. Locked decisions you can't relitigate: ..."

## 2. Acceptance criteria

Ask the user, via `AskUserQuestion` or freeform: **"What does success look like for this phase? List 3-7 user-observable outcomes — things that would be testable."**

For each answer:
- Assign id `c-1`, `c-2`, ...
- Tighten wording into a one-liner the user confirms

**Quality bar — push back if a criterion fails any of these:**
- Not testable (you can't write a test that fails when it breaks)
- Phrased in implementation terms ("uses X library") instead of behaviour ("returns 401 when token missing")
- Vague ("works well") instead of measurable ("loads in under 200ms on 4G")
- Two outcomes squashed into one (split it)

After each criterion, show the running list. Ask "anything missing?" When the user says no, move on.

## 3. Locked decisions

Ask: **"Any design choices already locked at this point? Schema decisions, library picks, API shapes, anything that's NON-NEGOTIABLE for the planner?"**

For each:
- `key` (short identifier, e.g. `tag_storage`)
- `choice` (the decision)
- `why` (the reason — short, but real)
- `locked = true`

If the user is unsure, skip. Decisions can be added later. Don't invent decisions to fill space.

## 4. Deferred ideas

Ask: **"Anything someone might assume is in scope that you're explicitly punting? Stuff to defer to a later phase?"**

For each:
- `text` (the deferred thing)
- `why` (optional — usually "premature without X" or "v1.1 not v1.0")

This is gold for the planner: it stops them adding tasks for things you don't want yet.

## 5. Write spec.toml

Compose the file as TOML. Schema:

```toml
[phase]
id        = "<phase-id>"           # e.g. "03-meal-tagging"
title     = "<title>"
milestone = "<version>"            # optional, only if state.current_milestone is set

[[criteria]]
id   = "c-1"
text = "..."

[[criteria]]
id   = "c-2"
text = "..."

[[decisions]]
key    = "..."
choice = "..."
why    = "..."
locked = true

[[deferred]]
text = "..."
why  = "..."                       # optional
```

Use the `Write` tool to save to `.dross/phases/<id>/spec.toml`. Show the final file content to the user. Ask: "Lock this spec? (y / edit)".

## 6. Validate + wrap

Run `dross validate`. If it errors, surface the schema problem and fix.

Update state:
```
dross state set current_phase <id>
dross state touch "spec locked: <id>"
```

End with one line:
```
Spec locked. Next: /dross-plan to break it into tasks.
```

## Hard rules

- **Never** invent criteria the user didn't explicitly approve. If you propose, say so and ask confirmation before writing.
- **Never** mark a decision `locked = true` without an explicit `why`. Locked decisions become non-negotiable in the planner.
- **Always** keep prose to bullet points and short sentences. Don't write paragraphs into TOML strings.
- **If the user pushes back on the quality bar** (e.g. "I know it's vague, just write it"), comply but flag the risk once: "noted — vague criteria can pass verify trivially, may want to revisit."
