# /dross-spec

Clarify what a phase delivers. Produces `.dross/phases/<id>/spec.toml`.

**Run this as a conversation, not a broadcast.** Follow the shared interaction playbook (`_interaction.md`, printed by the `dross interaction show` pre-flight step below): propose one point at a time via `AskUserQuestion` and let the user react. For `/dross-spec` that means walking §2's criteria and §3's gray-areas individually, and confirming the composed `spec.toml` with a one-line summary — never dumping the orientation, criteria, and TOML into one block.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for every turn of this command.
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

**Re-surface parked ideas.** A prior phase may have *deferred* an idea and routed it to **this** phase. Pull those candidates in via the CLI (don't grep specs by hand):
```
dross deferred list --target <id> --json
```
Each returned item is a candidate acceptance criterion for §2 — surface them ("phase `<source>` parked this for you: …") and let the user accept / reword / drop, same as any other criterion. This is the loop-closer: a punted idea reaches the planner instead of dying as a bare roadmap slug.

Print a short orientation block: "Working on phase X. Project core value: Y. Milestone success criteria: Z. Locked decisions you can't relitigate: ..."

## 2. Acceptance criteria

Ask once (freeform): **"What does success look like for this phase? List 3-7 user-observable, testable outcomes."**

Then walk the answers **one at a time** — not as a wall:
- Tighten each into a one-liner and assign id `c-1`, `c-2`, …
- Confirm that one criterion before moving to the next: `AskUserQuestion` (`accept` / `reword` / `drop`) when a quick gate fits, freeform when it needs discussion. **One criterion per turn.**
- Keep each turn to the criterion in hand — don't echo the whole growing list back every time; a short "c-3 added" is enough.
- Only after the user's list is exhausted, ask once: **"anything missing?"**

**Quality bar — push back (within that criterion's turn) if it fails any of these:**
- Not testable (you can't write a test that fails when it breaks)
- Phrased in implementation terms ("uses X library") instead of behaviour ("returns 401 when token missing")
- Vague ("works well") instead of measurable ("loads in under 200ms on 4G")
- Two outcomes squashed into one (split it)

## 3. Locked decisions — gray-area walkthrough

Don't just ask "any locked decisions?" and wait for the user to free-recall them. Surface this phase's **gray areas** — the contract-shaping choices you're *genuinely unsure how to resolve* — and walk the user through **every** one, one at a time. Each resolved area becomes a locked decision.

A gray area is a contract-shaping choice you **cannot confidently resolve on your own**: it could go multiple ways, the outcome differs, and you don't have a clear best answer. The discriminator is *your* uncertainty — **not** merely that the user *might* have an opinion. If you're confident about the right call on a contract-shaping choice, make it and note it; don't ask.

### 3a. Identify gray areas

Using the context from §1 (project goals, milestone constraints, locked stack) and the acceptance criteria from §2, list the areas where you are **genuinely uncertain** how to proceed:

- Use **concrete labels tied to this phase's domain** — never generic category names like "UI" / "Behaviour" / "Architecture".
  - Phase "CLI for backups" → `Output format`, `Flag design`, `Progress reporting`, `Error recovery`
  - Phase "Meal tagging" → `Tag storage`, `Duplicate handling`, `Tag input UX`, `Max tags per item`
- **Keep it to the genuine ones — a soft ~3–4.** Don't pad to hit a number, and don't truncate a real uncertainty to stay under one. Order them **most-impactful / most-uncertain first**, so if the user later hands you the rest (§3b off-ramp), the consequential ones are already settled.
- **Skip what's already decided.** Don't re-ask anything settled by `stack.locked` in project.toml, a `[[decisions]]` carried in a prior phase's spec.toml, or a choice already implied by an acceptance criterion. If you skip an area for this reason, say so ("session handling is fixed by the locked auth library — not asking").
- **Stay inside the phase boundary.** A gray area clarifies HOW to build what's already scoped — never WHETHER to add a new capability. If a candidate is really a new capability, it's a deferred idea (§4), not a gray area.

**What is NOT a gray area — decide these yourself, don't ask:** internal architecture, code patterns, performance tuning, anything the planner or executor resolves — **even when you're unsure about them**. Uncertainty alone doesn't earn a question; the choice must *also* be contract-shaping / user-observable. Ask only about user-facing and contract-shaping choices you can't confidently settle.

If no meaningful gray areas exist (pure infra, clear-cut implementation, all already decided, or you're confident on every call), say so plainly and skip to §4. Don't manufacture choices to fill space.

### 3b. Walk every area, one at a time

There is **no selection step** — do not ask the user which areas to discuss, and do not pre-filter the list down for them. Walk **every** identified gray area yourself, **one at a time** — a single decision each turn, in the propose-and-react shape the `_interaction.md` playbook defines:

- For each area, offer concrete options via `AskUserQuestion` where a small set of choices exists; go freeform where it's open-ended. **Lead with the option you'd pick.** Keep going on an area until the decision is crisp enough to write down, then move to the next.
- **Don't batch** areas into a single turn, and don't dump the whole list — surface one area, resolve it, advance. A short "decided: tag_storage" before the next is enough.
- **User off-ramp.** If the user says "you decide the rest" / "that's enough" at any point, stop walking and settle the remaining areas yourself (recording them as decisions), per the playbook's *when-the-contract-bends* clause. **Never self-truncate without that signal** — walking all of them is the default.

While discussing:
- If the user references a doc/spec/file ("follow the schema in `X`"), read it and let it inform your follow-ups.
- If the user raises something outside the phase boundary, capture it as a deferred idea and redirect: **"`<that>` is its own capability — noting it as deferred. For now let's stay on `<phase>`."**

### 3c. Capture outcomes

Each resolved gray area becomes a locked decision:
- `key` (short identifier, e.g. `tag_storage`)
- `choice` (the decision)
- `why` (the reason — short, but real)
- `locked = true`

If the user wants to leave an area open, don't force it — skip it (decisions can be added at plan time). **Never** mark `locked = true` without a `why`. Don't invent decisions to fill space.

## 4. Deferred ideas

The §3 discussion may already have surfaced deferred ideas (scope-creep redirects, areas the user punted). Fold those in first, then ask once more to catch the rest:

**"Anything someone might assume is in scope that you're explicitly punting? Stuff to defer to a later phase?"**

For each deferred idea capture:
- `text` (the deferred thing)
- `why` (optional — usually "premature without X" or "v1.1 not v1.0")

When resuming/extending an existing spec, **skip any deferred item that already has a target** — it's already routed; don't re-offer it (idempotent, no duplicate routing).

### 4a. Route every deferred idea — don't just record it

A bare deferred item is write-only: it dies in this spec and never comes back. So give **every** new deferred idea a destination via `AskUserQuestion` (one item per turn, lead with the most likely):

- **Pull into the current phase** — it's actually in scope. Move it *out* of deferred and add it as a new `[[criteria]]` entry (back to §2). It is no longer deferred.
- **Park in the milestone backlog** — relevant, but not this phase. Coin a short slug from the idea; after the spec is written (§5) run:
  ```
  dross milestone add <version> phases "<slug>"
  dross deferred route <phase-id> <idx> --target "<slug>"
  ```
  It now re-surfaces 1:1 when `<slug>` is later scaffolded (§1's seed).
- **Attach to a named future phase** — it belongs to a phase already on the roadmap. Stamp that existing slug (after §5):
  ```
  dross deferred route <phase-id> <idx> --target "<existing-slug>"
  ```
- **Someday** — genuinely no home yet. Leave it unrouted (no `target`); it gets triaged later via `/dross-inbox`. **Leave an item unrouted only on an explicit someday pick** — never as the silent default.

`<idx>` is the item's 0-based position in this spec's `[[deferred]]` array. The `dross deferred route` / `dross milestone add` commands mutate on-disk artefacts, so **run them after §5 has written `spec.toml`** (a one-line confirm per item is enough — don't echo the array back). Re-run `dross validate` once after routing.

This routing is gold for the planner and the backlog: parked ideas reach a real phase instead of evaporating.

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

Use the `Write` tool to save to `.dross/phases/<id>/spec.toml`. **Don't paste the TOML back** — it's a build artifact, not a review medium, and dumping it is exactly the ctrl+o wall this command avoids. Every line was already agreed point by point in §2–§4. Confirm with a one-line summary instead:

**"Spec written: N criteria, M locked decisions, K deferred — lock it? (y / edit \<what>)"**

Only surface a specific field if the user asks to see or change it.

## 6. Validate + wrap

Run `dross validate`. If it errors, surface the schema problem and fix.

Update state:
```
dross state set current_phase <id>
dross state touch "spec locked: <id>"
```

End with the standard next block — the `Next:` line, plus the conditional flag hint **only when it applies**:
```
Spec locked.

Next: /dross-plan — break the locked spec into tasks.
```
When the phase is a new subsystem, has multiple plausible architectures, or looks like 4+ tasks, append the hint under the `Next:` line:
```
      ↳ --panel — independent 3-lens planning, worth the ~4–5× cost at this size.
```

## Hard rules

- **Follow the interaction playbook (`_interaction.md`); spec.toml is never a review medium.** Drive the command as short `AskUserQuestion`-gated turns — one criterion / one gray-area at a time — and confirm the composed `spec.toml` with a one-line summary (§5) rather than pasting it back. Content is agreed in prose first; the TOML is only where it lands.
- **Never** invent criteria the user didn't explicitly approve. If you propose, say so and ask confirmation before writing.
- **Gray areas (§3) must be phase-specific and inside the phase boundary.** Generic category labels ("UI", "Behaviour") and new-capability questions ("should we also add search?") are both bugs — the first is lazy, the second is scope creep. Skip areas already settled by `stack.locked` or a prior decision rather than re-asking them.
- **Never** mark a decision `locked = true` without an explicit `why`. Locked decisions become non-negotiable in the planner.
- **Always** keep prose to bullet points and short sentences. Don't write paragraphs into TOML strings.
- **If the user pushes back on the quality bar** (e.g. "I know it's vague, just write it"), comply but flag the risk once: "noted — vague criteria can pass verify trivially, may want to revisit."
