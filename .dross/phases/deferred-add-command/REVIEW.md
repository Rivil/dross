# Plan Review — deferred-add-command

Reviewed: 2026-08-13
Plan: 7 tasks across 4 waves

## BLOCKING

(none)

Coverage is complete: c-1 (t-4), c-2 (t-1, t-4), c-3 (t-4), c-4 (t-1, t-5), c-5 (t-7), c-6 (t-6),
c-7 (t-2), c-8 (t-3). No task contradicts a `locked = true` decision outright, and no task implies
a rules.toml violation — r-01 (`make install` before relying on a prompt/binary change) is the only
project rule and t-7 names it explicitly. There is no global `~/.claude/dross/rules.toml`.

## FLAG

- [antipattern / duplicate rule] `internal/cmd/validate.go:70-85` **already implements** the target-
  validity rule t-2 is written to create: it builds `validTargets` from every phase dir plus every
  slug in every milestone's `phases` array, and reports `deferred target %q names no phase dir or
  milestone.phases entry` at line 118. t-2 authors a second implementation in a new
  `deferred_target.go` and never mentions the existing one. Worse, the two disagree: validate accepts
  a slug from **any** milestone's array, t-2 accepts only the **current** milestone's. So a target
  that `dross validate` blesses can be un-settable through `add`/`route`, and t-1's contract ("a
  bogus target hand-written into `.dross/deferred.toml` makes `dross validate` exit non-zero") is
  testing a different predicate than t-2's.
  Suggestion: extract validate.go's `validTargets` construction into the shared helper and have both
  call sites use it, or state in t-2 why the CLI rule is deliberately narrower than the validator's
  and what a user with an other-milestone slug is meant to do.

- [locked-decision / edge interpretation] t-4's third contract — "current phase set but its
  spec.toml absent: add exits 0, the item lands in `_project` … and NO spec.toml appears under that
  phase dir" — is not what `storage_home` says. The decision's text is "Write into the current
  phase's spec.toml **when current_phase is set**; otherwise into a project-level store". The plan
  reads "otherwise" as "when there is no usable spec", which honours the decision's *why* but not
  its text. It also silently diverges from `survivor route`, which errors on a missing/unloadable
  current-phase spec rather than falling back.
  Suggestion: not blocking — the reading is defensible — but say so in the task description as an
  explicit interpretation of `storage_home`, so execution isn't the first place the choice appears.

- [antipattern / unstated seam] The plan never says how an item's `id` reaches the code that needs
  it. `deferredEntry` (deferred.go:19-30) is the only flattener and has no ID field; `collectDeferred`
  is what `syncBacklog` consumes (issue.go:355). t-3 must therefore either add `ID string json:"-"`
  to `deferredEntry` or re-load each spec to find the id — a real design fork that t-1's file list
  (which does include deferred.go) hints at but no description states. Relatedly, t-1's contract
  "`deferred list --json` has no `id` key" passes trivially at t-1 time, since nothing has added an
  id to `deferredEntry` yet.
  Suggestion: name the carrier in t-1 or t-3 ("`deferredEntry` gains `ID string json:\"-\"`"), so the
  `deferred_identity` lock is enforced at the struct tag rather than discovered during t-3.

- [granularity / split candidate] t-1 is 5 files and three separable concerns: (a) the `phase.Deferred.ID`
  schema field, (b) the `_project` store + `deferredStore` resolver + collectDeferred, (c) the
  downstream plumbing (validate's reserved-slug + store dangling-target walk, repointDeferredTarget).
  Nine test contracts on one task is the same signal.
  Suggestion: (a)+(b) as one task, (c) as a second — (c) has no dependents inside this plan, so it
  can even move to wave 2 in parallel with t-3/t-4/t-5.

- [granularity / split candidate] t-7 is 5 files spanning four different kinds of work: a live-board
  CLI invocation against the real tracker, a gitignored working-memory edit, a shipped prompt change
  (pause.md, D8), and two new Go tests including a repo-level one. The prompt edit shares nothing with
  the filing except the phase it lands in.
  Suggestion: split the pause.md teaching (D8) into its own task; it depends on t-4 only and could
  sit in wave 3.

- [wave order] t-7's `depends_on = ["t-4", "t-5", "t-6"]` — the t-5 edge is not real. Both findings
  land in the **current** phase's spec.toml (`deferred-add-command` is current, and t-7's file list
  says so), so t-7 never addresses a `_project` item through `route`/`unroute`/`dismiss`. t-7 stays
  in wave 4 on the t-6 edge regardless, so this costs no parallelism — it just makes the graph claim
  a dependency that doesn't exist.
  Suggestion: drop t-5 from t-7's depends_on.

- [internal inconsistency] t-6 (D2) pushes an added item to the board **unconditionally on --target**,
  while t-3 explicitly preserves syncBacklog's routed-item skip ("the routed filter is untouched").
  Consequence: a routed item created by `add` gets a board issue that `backlog-sync` will then never
  update (it is filtered out), while the same item created by `route` on an existing entry gets no
  issue at all. Board coverage of routed items becomes verb-dependent, and add-created routed issues
  go permanently stale on the first text edit. This phase's own `[[deferred]]` entry parks exactly
  this territory to `board-sync-truth`, so D2 half-enters a scope the spec deferred.
  Suggestion: either keep the mirror someday-only in `add` (consistent with sync, defers cleanly), or
  keep D2 and say in the task description that the stale-update asymmetry is knowingly left for
  `board-sync-truth` — the plan currently does neither.

- [check 3 / migration risk] t-3's legacy-key remap inherits the very ambiguity it exists to remove.
  `someday:<phase>#0` identifies a position, so if an item was deleted between the last sync and the
  upgrade run, the migration maps the *survivor* onto the *deleted item's* board issue — c-8's fault,
  committed one last time, permanently, by the fix. The contracts cover the clean case ("board.json
  holding only the legacy `someday:<phase>#0` key produces zero CreateIssue calls") but never the
  shifted case.
  Suggestion: add a contract for a legacy board.json whose indices no longer match the spec (e.g.
  title-compare before adopting a legacy key, or drop the legacy entry and re-create), and decide
  whether a mismatched legacy key should be adopted at all.

- [coverage detail] c-3 has two halves — "`--target <slug>` files and routes in one command" and
  "omitting the flag leaves it unrouted as someday". t-4's contracts assert the first
  (`--target beta` → `list --routed` includes / `--someday` excludes) but never the second: no
  contract runs a bare `add` and asserts it appears in `list --someday`. t-7's live check does it
  against the real repo, which is not a hermetic regression guard.
  Suggestion: add the mirrored assertion to t-4 — bare `add` → `list --someday` includes,
  `list --routed` excludes.

- [check 3 / vacuous assertion] t-2's contract "a rejected route leaves the source spec.toml
  byte-identical AND makes no board HTTP call" — `deferred route` has no board path today and gains
  none in this plan (only `add` mirrors, in t-6). The board half of that assertion cannot fail.
  Suggestion: keep the byte-identical half, drop or relocate the board half to t-4/t-6 where a
  rejected `add` genuinely must not have called the board.

- [execution hazard] t-7 files two findings "against the live board" while `board.enabled = true`,
  `project = "DRO"`, `milestone_mode = "epic"` and v1.3 is current — with t-6's unconditional mirror
  that creates **two real YouTrack issues**, which is not reversible by re-running the task. It also
  needs `YOUTRACK_TOKEN` in the environment; no task mentions sourcing it, and a missing token turns
  the c-6 mirror into the warn-and-continue path, so t-7 would "pass" having silently filed nothing
  on the board.
  Suggestion: state the env prerequisite in t-7 and add an observed-output check that the two board
  issues were actually created (not warned past) — otherwise the board half of c-6 is proven only by
  hermetic fakes.

- [antipattern / doc surface] Three shipped prompts narrate the deferred verb set and none is in the
  plan except pause.md: `assets/prompts/inbox.md:28` tells the reader each entry "carries its
  originating **source phase**" (now sometimes `_project`, which is not a phase) and lines 49-52
  enumerate route/dismiss as the triage moves; `assets/prompts/status.md:12` and
  `assets/prompts/spec.md:140` describe the someday backlog as "ideas punted in /dross-spec", which
  `add` makes untrue.
  Suggestion: decide explicitly whether inbox.md/status.md are in scope. If not, say so — a reader of
  this plan will assume README + pause.md was an oversight rather than a call.

## NOTE

- [reference] Task descriptions cite D1-D8 (t-1 "D4", t-3 "D3"/"D1", t-6 "D2", t-7 "D5"/"D8"), but
  those identifiers exist only in `.dross/phases/deferred-add-command/panel/synthesis.md` — not in
  spec.toml or plan.toml. An executing agent reading plan.toml alone cannot resolve them.

- [verified] The plan's load-bearing repo claims all check out: `.gitignore:12` is indeed
  `.dross/handoff.md` (t-7's D5 reasoning is sound); `mutation-score-truth` is in v1.3's `phases`
  array with no phase dir, so it really is the only end-to-end exercise of t-2's milestone-array
  branch; `phase.Slugify` (phase.go:184-201) emits only `[a-z0-9]` plus interior dashes and trims
  trailing ones, so it cannot produce `_project` today — t-1's guard test is a real regression guard,
  not a tautology.

- [scope] `.dross/handoff.md` has **four** open loops, not two. t-7 correctly files the two the spec
  names; the other two (`dross phase reconcile` home undecided, survivors.toml 107 keys vs 106
  classified) stay behind. That matches c-5 as written — noting only so the half-emptied Open loops
  section isn't read as an incomplete t-7.

- [strength] The test contracts are unusually falsifiable. Nearly every one names the code change
  that breaks it ("dropping the `id` toml tag fails the reload-and-compare", "Index-keyed code fails
  this; it is c-8", "a `Target != \"\"` skip fails this", "deleting the milestone-array branch fails
  this test"). This is the difference between a contract and a restatement, and this plan is on the
  right side of it throughout.

- [strength] t-4's "error text BYTE-IDENTICAL to the same rejection from `route` (one assertion
  comparing both outputs, so the two paths cannot drift)" is a genuine anti-drift mechanism rather
  than a coverage claim — it is what makes c-7's "on the same rule as `add`" enforceable instead of
  aspirational.

- [strength] Validation-before-write ordering is asserted, not assumed, on both mutating paths (t-2's
  "a rejected route leaves the milestone toml byte-identical", t-4's "the spec/store file is
  byte-identical afterwards — no orphan item with a dead target"), and t-6 asserts the local-write-
  before-push ordering that the `board_push` lock depends on. Half-written state is designed out.

## Summary

No blockers — coverage is complete and the contracts are strong — but t-2 re-implements a
target-validity rule that already exists in validate.go with different semantics, the `id` carrier
between t-1 and t-3 is never named, and t-6's unconditional mirror of routed items quietly
contradicts t-3's preserved routed-skip.
