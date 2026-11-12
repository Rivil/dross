# /dross-spec

Clarify what a phase delivers. Produces `.dross/phases/<id>/spec.toml`.

**Run this as a conversation, not a broadcast.** Follow the interaction playbook (`_interaction.md`, printed by `dross interaction show` below): walk §2's criteria and §3's gray-areas one at a time via `AskUserQuestion`, and confirm the written `spec.toml` with a one-line summary — never dump orientation, criteria and TOML in one block.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for every turn of this command.
2. Resolve the target phase from `$ARGUMENTS`:
   - `--new "Phase Title"` → run `dross phase create "<title>"`, capture the new id (`NN-slug`).
   - `<phase-id>` → use it directly when `.dross/phases/<id>/` exists. If it doesn't but `<id>` is an entry in the active milestone's `phases` array, it is an unscaffolded roadmap phase: run `dross phase create "<id>"` (it also checks out `phase/<id>`) and continue. Fail only if it is in neither.
   - empty → resolve it from `state.json`:

**When** `target=empty` — `$ARGUMENTS` is empty: `dross prompt partial spec no-target`

**When** `target=new` — `--new` or the no-target `new` answer: `dross prompt partial spec create`

3. **Verify current branch is `phase/<id>`** for the resolved phase (`git symbolic-ref --short HEAD`). On resume, if you're not on the phase branch: `dross phase checkout <id>` — the guarded checkout. If it refuses because the branch doesn't exist locally, stop and surface the situation to the user (phase work belongs off main).
4. Read `.dross/phases/<id>/spec.toml` if it exists. **Resume, don't overwrite.**

**When** `spec_file=present` — the phase already has a spec.toml: `dross prompt partial spec resume`

## 1. Read context

Read these and surface their relevant bits to the user before asking questions:

- `.dross/project.toml` — `goals.core_value`, `goals.non_goals`, `stack.locked`, `runtime.mode`
- `.dross/milestones/<milestone>.toml` if `state.current_milestone` is set — milestone success criteria + non-goals constrain what this phase should accept
- Any spec.toml in the phase dir already

**Re-surface parked ideas.** A prior phase may have *deferred* an idea and routed it to **this** phase. Pull those candidates in via the CLI (don't grep specs by hand):
```
dross deferred list --target <id> --json
```
Each returned item joins the §2 candidate slate ("phase `<source>` parked this for you: …") under the same accept / reword / drop gate.

Print a short orientation block: "Working on phase X. Project core value: Y. Milestone success criteria: Z. Locked decisions you can't relitigate: ..."

## 2. Acceptance criteria

**Open with a proposed candidate slate — never a free-recall ask.** Compose the candidate criteria yourself from the §1 context before asking the user anything, derived from:

- **Milestone scope** — the milestone's success criteria plus this phase's position in its `phases` array: which slice of the milestone does this phase own?
- **CLI/context gap analysis** — the delta between what exists today (code, commands, §1 context) and what the phase title promises.
- **Parked ideas** — the §1 `dross deferred list` candidates routed to this phase join the slate directly.

Tighten each candidate into a one-liner and assign id `c-1`, `c-2`, … — a soft 3-7 in total; don't pad, don't truncate a real outcome. Present the slate once as short one-liners, then gate **each item, one per turn** — the propose-and-react shape from the playbook:

- `AskUserQuestion` per criterion: `accept` / `reword` / `drop`. Freeform only where an item needs real discussion.
- `accept` — the criterion stands as written. `reword` — keep the idea, user adjusts wording or scope. `drop` — not this phase; if it's really a separate capability, capture it as a deferred idea (§4).
- Keep each turn to the criterion in hand — don't echo the whole growing list back every time; a short "c-3 added" is enough.
- Only after the slate is walked, ask once: **"anything missing?"** — a user addition gets the same per-item gate and quality bar.

**Quality bar — push back (within that criterion's turn) if it fails any of these:**
- Not testable (you can't write a test that fails when it breaks)
- Phrased in implementation terms ("uses X library") instead of behaviour ("returns 401 when token missing")
- Vague ("works well") instead of measurable ("loads in under 200ms on 4G")
- Two outcomes squashed into one (split it)

## 2b. Edge probe

Run `dross phase edges <id> --json`. From its `open` list of classes — boundary, empty, encoding, ordering, precision, idempotency, concurrency, adjacency, each with its hint — propose the **2–3** likeliest to bite this phase, **one per turn** via `AskUserQuestion` — never walk all eight. Resolved classes are never in `open`, so a resumed spec re-proposes none; repair each `unresolved` class's record first, or plan-lock refuses it. There is no minimum: a proposal that doesn't apply is recorded dismissed with a reason.

Each answer is one `[[edges]]` record (§5):
- **covered** — `criteria` names the criterion ids that test it.
- **dismissed** — `reason` says why it can't bite.
- **deferred** — mint an id with `dross deferred new-id` and put the same id on the new `[[deferred]]` entry (§4) and in the edge's `deferred`; or name an already-parked item's id.

A class may surface a must-NOT ("never X when Y"): it joins `[[criteria]]` through §2's accept / reword / drop gate. Accepted → the edge is covered, naming it; dropped → record the edge dismissed or deferred, never covered.

## 3. Locked decisions — gray-area walkthrough

Don't ask "any locked decisions?" and wait for free recall. Surface this phase's **gray areas** and walk **every** one, one at a time; each resolved area becomes a locked decision.

A gray area is a contract-shaping choice you **cannot confidently resolve on your own**: it could go several ways and you have no clear best answer. The discriminator is *your* uncertainty, **not** that the user *might* have an opinion. Confident on a contract-shaping call? Make it, note it, don't ask.

### 3a. Identify gray areas

From §1's context and §2's criteria, list the areas where you are **genuinely uncertain** how to proceed:

- Use **concrete labels tied to this phase's domain** (phase "Meal tagging" → `Tag storage`, `Duplicate handling`), never generic ones like "UI" / "Behaviour".
- **Keep it to the genuine ones — a soft ~3–4.** Don't pad to hit a number, and don't truncate a real uncertainty to stay under one. Order them **most-impactful / most-uncertain first**, so if the user later hands you the rest (§3b off-ramp), the consequential ones are already settled.
- **Skip what's already decided.** Don't re-ask anything settled by `stack.locked` in project.toml, a `[[decisions]]` carried in a prior phase's spec.toml, or a choice already implied by an acceptance criterion. If you skip an area for this reason, say so ("session handling is fixed by the locked auth library — not asking").
- **Stay inside the phase boundary.** A gray area clarifies HOW to build what's already scoped — never WHETHER to add a new capability. If a candidate is really a new capability, it's a §4a candidate (sorted include-first), not a gray area.

**What is NOT a gray area — decide these yourself, don't ask:** internal architecture, code patterns, performance tuning, anything the planner or executor resolves — **even when you're unsure about them**. The choice must *also* be contract-shaping / user-observable.

No meaningful gray areas? Say so plainly and skip to §4. Don't manufacture choices.

### 3b. Walk every area, one at a time

There is **no selection step** — don't ask which areas to discuss or pre-filter the list. Walk **every** gray area yourself, **one at a time**, propose-and-react:

- Offer concrete options via `AskUserQuestion` where a small set exists, freeform otherwise. **Lead with the option you'd pick.** Stay on an area until the decision is crisp, then advance.
- **Don't batch** areas or dump the list. A short "decided: tag_storage" before the next is enough.
- **User off-ramp.** On "you decide the rest" / "that's enough", settle the remaining areas yourself as decisions. **Never self-truncate without that signal.**

While discussing:
- If the user references a doc/file, read it and let it inform your follow-ups.
- Something that isn't a gray area isn't deferred by reflex: note it as a §4a candidate (include-first) and redirect only the discussion — **"noting `<that>` as a candidate — I'll sort it in §4. For now let's stay on `<area>`."**

### 3c. Capture outcomes

Each resolved gray area becomes a locked decision:
- `key` (short identifier, e.g. `tag_storage`)
- `choice` (the decision)
- `why` (the reason — short, but real)
- `locked = true`

An area the user leaves open is skipped (plan time can add it). **Never** mark `locked = true` without a `why`.

## 4. Deferred ideas

The §3 discussion may already have surfaced deferred ideas (scope-creep redirects, areas the user punted). Fold those in first, then ask once more to catch the rest:

**"Anything someone might assume is in scope that you're explicitly punting? Stuff to defer to a later phase?"**

For each deferred idea capture:
- `text` (the deferred thing)
- `why` (optional — usually "premature without X" or "v1.1 not v1.0")

When resuming/extending an existing spec, **skip any deferred item that already has a target** — it's already routed; don't re-offer it (idempotent, no duplicate routing).

### 4a. Route every surfaced candidate — include-first, then destination

A bare deferred item dies in this spec. So every surfaced candidate — a gray-area-adjacent proposal, an optional sub-feature, an idea raised along the way — is sorted **include-first** (the playbook's rule). An idea the user *explicitly* punted is already a defer: skip to step 2.

Sort each candidate first. **Belongs in this phase** → don't ask a defer question: **Pull into the current phase** — add it as a new `[[criteria]]` entry through §2's accept / reword / drop gate, with accept as the lead. Anything else is driven as a **two-step** `AskUserQuestion`, one item per turn:

**Step 1 — the entry gate (include-first).** An either/or of **add to current phase** and **defer it**; which one leads depends on the case:
- **Genuinely borderline** → lead with **add to current phase**.
- **Clearly has a home elsewhere** (a named roadmap phase, or a separate capability) → lead with **defer it**, and put the reason and the likely destination in the option's description.
- **add to current phase** — it's in scope after all. This is the **Pull into the current phase** branch: add it as a new `[[criteria]]` entry (back to §2). It is not deferred. This is the *only* place the pull-in is offered.
- **defer it** — it's not this phase. Keep it deferred and go to step 2 for a destination. Deferral is not the safe default: in-scope work parked in the backlog is scope that leaked.

**When** `deferral=chosen` — an item is deferred (step 1's "defer it", or the user punted it): `dross prompt partial spec route`

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
id   = "..."                       # dross deferred new-id, when an edge links it
text = "..."
why  = "..."                       # optional

[[edges]]
class    = "boundary"
outcome  = "covered"               # covered | dismissed | deferred
criteria = ["c-1"]                 # or reason = "..." / deferred = "<id>"
```

`Write` it to `.dross/phases/<id>/spec.toml`. **Don't paste the TOML back** — every line was agreed in §2–§4. Confirm with a one-line summary asked as one `AskUserQuestion`, never a trailing prose line:

**"Spec written: N criteria, M locked decisions, K deferred — lock it?"** — lead option **lock**, other option **edit \<what>**.

Only surface a specific field if the user asks to see or change it.

**Record what the spec absorbed.** Once `spec.toml` is written, run this for every criterion seeded from a §1 parked item — accepted as written or reworded, both count — where `<source> <idx>` is that item's handle from §1's `dross deferred list --target <id> --json`:
```
dross deferred absorb <source> <idx> --criterion <c-N> --phase <phase-id>
```
Always pass `--phase <phase-id>`: `current_phase` is only set in §6. The record closes the item's board card when this phase completes; an unabsorbed item stays open. One call per parked item.

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

state is on disk — safe to /clear · fresh session: /dross-plan
```
When the phase is a new subsystem, has multiple plausible architectures, or looks like 4+ tasks, append the hint under the `Next:` line:
```
      ↳ --panel — independent 3-lens planning, worth the ~4–5× cost at this size.
```

## Hard rules

- **Follow the interaction playbook (`_interaction.md`); spec.toml is never a review medium.** One criterion / gray-area per `AskUserQuestion` turn; confirm the written file per §5, never by pasting it or a prose "(y / edit)" line.
- **Never** invent criteria the user didn't explicitly approve. If you propose, say so and ask confirmation before writing.
- **Gray areas (§3) must be phase-specific and inside the phase boundary.** Generic category labels ("UI", "Behaviour") and new-capability questions ("should we also add search?") are both bugs — the first is lazy, the second is scope creep. Skip areas already settled by `stack.locked` or a prior decision rather than re-asking them.
- **Never** mark a decision `locked = true` without an explicit `why`. Locked decisions become non-negotiable in the planner.
- **Always** keep prose to bullet points and short sentences. Don't write paragraphs into TOML strings.
- **If the user pushes back on the quality bar** (e.g. "I know it's vague, just write it"), comply but flag the risk once: "noted — vague criteria can pass verify trivially, may want to revisit."
