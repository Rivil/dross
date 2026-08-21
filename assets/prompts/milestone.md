# /dross-milestone

Drive the milestone lifecycle: scope a new one, close out a finished one, or report what a live one still has left. Which of those you do is not a flag — it is dispatched from the milestone's own state. Scoping runs once per milestone; expect ~5-15 minutes depending on whether a `Brief.md`-style source doc exists.

**Run this as a conversation, not a broadcast.** Follow the shared interaction playbook (`_interaction.md`, printed by the `dross interaction show` pre-flight step below): walk success criteria one at a time, then non-goals, then phase order — each its own segment, never one batched dump.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for every turn of this command.

2. **Dispatch.** Ask the CLI what state the milestone is in, and let the answer choose the arm:

```
dross milestone progress --json
```

It emits `{version, status, done, total, all_done, remaining, unscaffolded}`. `remaining` is the slugs still outstanding; `unscaffolded` is the subset of those with no phase directory yet. Doneness comes from each phase's own record — do NOT re-derive it from a phase's `verify.toml` verdict, or by eyeballing the phases array. A verified phase is not a delivered one. `dross phase list --milestone <version>` reads the same record through the same reader, so it agrees with this command by construction and is a fine way to see *which* phases are outstanding; it is a second view of the same answer, not a second source of it.

**Branch on `status` FIRST, then on `all_done`.** The two arms would otherwise both fire on a milestone that was just finalized — status `complete`, every phase done, `current_milestone` still pointing at it — and the run would try to complete an already-closed milestone instead of scoping the next one.

- **`status` is `complete`** → this milestone is closed. Say so in one line, then take the **scope arm** for the next version (§1 onwards). If a leftover `milestone/<version>` branch was reported at finalize time, mention `dross milestone prune` and move on.

- **the command exits non-zero with the no-`current_milestone` message** (`no version given and state has no current_milestone`) → there is no active milestone. That is an ARM, not a broken command: take the **scope arm** (§1 onwards). Same for a `$ARGUMENTS` version whose toml does not exist yet.

- **`all_done` is true** → every phase has landed. Take the **completion arm** (§8) and drive the close-out.

- **otherwise** → work is outstanding. Take the **report arm**: print the `remaining` slugs (flagging any that are also in `unscaffolded` as not started yet), point at `/dross-spec <slug>` for the next one, and **stop**. Do not scope, do not create anything, do not walk success criteria — the milestone already has them.

Sections §1–§7 below are the **scope arm** only. §8 is the completion arm.

## 1. Scope arm — resolve the version, then read context

Reached from the dispatch's scope branch only: no active milestone, or one whose `status` already reads `complete`.

1. Resolve the milestone version from `$ARGUMENTS`:
   - `<version>` (e.g. `v0.1`, `v1.0`) → use it directly.
   - empty → ask the user via `AskUserQuestion`. Default to next minor if a previous milestone exists, else `v0.1`.
2. Determine create-vs-resume:
   - If `.dross/milestones/<version>.toml` does NOT exist → run `dross milestone create <version>`. This writes the skeleton with `status="planning"` and today's date, **and cuts + pushes the `milestone/<version>` integration branch** (HEAD stays on main) — the branch topology every phase/quick under this milestone forks off. It prints `cut …`/`pushed …` lines; surface them. Idempotent, and skips silently in a non-git dir or when there's no origin.
     - **Which branch it cuts from is conditional**, not always main: the current milestone's branch (`state.current_milestone`) while that branch is still unmerged, and the main branch once it has merged, is gone, or there is no current milestone. Stacking that way keeps the new milestone's PR showing its own commits instead of re-listing its parent's. The `cut …` line names the branch actually used — read it, don't assume main.
     - The branch it cut from is recorded as `base` in `.dross/milestones/<version>.toml` and read back verbatim ever after; it is never re-derived from git topology. `dross milestone get base` reads it back. `create` is its only writer — there is no `milestone set base`.
     - `dross milestone create <version> --base <branch>` forces the cut point when the automatic answer is wrong (e.g. an abandoned milestone you do not want to inherit). The forced branch is recorded like any automatic one.
     - When origin is unreachable the answer comes from local refs and `create` says so — surface that caveat rather than treating the cut point as settled.
   - If it DOES exist → run `dross milestone show <version>`, print the current state, and ask: "Milestone already scoped. Extend (add more criteria/phases) or replace (start over)?" If replace: delete `.dross/milestones/<version>.toml` and re-run create. If extend: continue from §3 onwards, skipping any field already populated unless the user wants to revise.
   - A wrong entry in `phases`, `scope.success_criteria` or `scope.non_goals` is correctable through the CLI — `dross milestone remove <version> <field> "<exact value>"` and `dross milestone replace <version> <field> "<old>" "<new>"`. Use those rather than hand-editing the toml; `replace` keeps the entry's position, so fixing a phase name is not a reorder.

Then surface the inputs that should shape the milestone, before asking questions:

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

If `Brief.md` is present, extract candidates from any "Milestone done when:" / "Acceptance:" / "v0.1 complete when:" section as the proposal. Otherwise ask once (freeform): **"What has to be true for this milestone to be considered done? 2-5 outcomes that you could write a test or observation for."**

Then walk the candidates **one criterion per turn** — mirroring `/dross-spec`: tighten each into a one-liner and confirm it via `AskUserQuestion` (accept / reword / drop) before moving to the next. Don't echo the whole growing list back each turn; a short "added" is enough.

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

Mirror the milestone onto the issue board (no-op unless `[remote].board_sync` is on — safe to always run):
```
dross issue milestone sync <version>
```
Phase issues created later by `/dross-plan` attach to this milestone automatically.

## 7. Wrap

Run `dross validate`. Should be green. Then confirm with the count-line summary below — never paste `milestone.toml` back:

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

## 8. Completion arm — every phase has landed

Reached only from the dispatch's `all_done` branch. The milestone's work is delivered; this arm gets it integrated and closed.

1. Confirm before acting. One `AskUserQuestion`: "All `<total>` phases are done — close out `<version>` now?" with **close it out** as the lead and **not yet** as the alternative. This opens a PR against a shared branch; it is not a step to take on inference alone.

2. Open the integration PR:

```
dross milestone complete <version>
```

It targets the milestone's recorded base — main, or the parent it was stacked on while that parent is still unmerged. Surface the PR URL it prints.

3. **Merge it as a MERGE COMMIT, not a squash.** Say this out loud to the user, in these words, and keep saying it if they ask about the merge. `repo.squash_merge` is set for *phase* PRs; the milestone PR is the one exception. Squashing it collapses every phase into a single commit and `main` loses the per-phase history the whole branch model exists to preserve. dross does not drive the merge, so this gate is narration or it is nothing.

4. Once the PR has merged, finalize:

```
dross milestone complete <version> --finalize
```

Then resolve the milestone's board card, so the epic does not sit open forever behind a finished milestone:

```
dross issue milestone sync <version> --close
```

A no-op when board sync is off. It refuses — without writing anything — on a board whose milestone is not itself an issue (a YouTrack version bundle or agile board, a forge/GitHub milestone id): there is no card to close there, and on the forges a milestone id and an issue number are the same string, so closing blind would resolve someone else's issue.

`dross milestone complete --finalize` records `[milestone].status = complete` **before** it fast-forwards main and deletes `milestone/<version>` local + remote. Two consequences worth stating rather than discovering:

- **Re-running `--finalize` is safe.** A second run reports the milestone already finalized and exits 0 — it is not an error state, and it is the right thing to do if the first run failed partway (a protected branch, an offline origin). If it names a leftover branch, `dross milestone prune` removes it.
- **A branch that is simply gone is reported as gone**, not as unmerged. If that happens without a finalize having run, record the milestone with `dross milestone set <version> status complete`.

5. Then scope what is next, or stop. Re-running `/dross-milestone` after a successful finalize takes the `status` = `complete` arm and offers the next version.

## Hard rules

- **Don't bypass the CLI.** Always write through `dross milestone set` / `dross milestone add` so validation runs and the toml stays canonical. Never edit `.dross/milestones/<version>.toml` directly from this command.
- **Don't create phase directories here.** Phase ids in `phases = [...]` are *intent*, not artefacts. `dross phase create` (via `/dross-spec --new`) is the only command that should write phase directories — keeps a single owner for that side effect.
- **Don't restate project non-goals as milestone non-goals.** If a non-goal already lives in `project.toml`, it applies project-wide; only add milestone-scoped non-goals here.
- **Resume-safe.** Re-running `/dross-milestone v0.1` on an existing milestone must never silently destroy data. Always show current state first and let the user choose extend vs replace.
