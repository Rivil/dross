# Plan Review — cli-surface-sweep

Reviewed: 2026-07-29
Plan: 9 tasks across 3 waves

## BLOCKING

(none)

Coverage is complete — every criterion c-1..c-9 appears in at least one `covers`
(c-1 t-1; c-2 t-6/t-8/t-9; c-3 t-4; c-4 t-4/t-7; c-5 t-3; c-6 t-2; c-7 t-5; c-8 t-6/t-8;
c-9 t-2). No task contradicts a locked decision, and nothing in the plan violates r-01
(the only rule in `.dross/rules.toml`; no global `~/.claude/dross/rules.toml` exists) —
t-6 explicitly cites it for the `assets/` prompt edit.

## FLAG

- [antipattern] t-3's D1 rationale is factually wrong about the code. It says the set
  `planning | active | complete` "matches the on-disk data — 11 milestone tomls carry
  `complete`, 1 carries `active`, and `milestone complete` writes `complete`". The on-disk
  count is exactly right (verified: 11 `complete`, 1 `active`), but **`milestone complete`
  never writes a status at all** — `internal/cmd/milestone.go:44-113` only opens the
  milestone→main PR or (`--finalize`) fast-forwards main and deletes the branch. The only
  status writes in the whole tree are `milestone create` → `"planning"`
  (`internal/cmd/milestone.go:218`) and the generic setter (`:455`). Every `complete` on
  disk was written by hand or by an agent through `milestone set`. The chosen set is still
  the right one, but a test row that claims to "pin the D1 set against the repo's own
  history" is resting one third of its evidence on a code path that does not exist.
  Suggestion: correct the rationale to cite `milestone create` → `planning` plus the
  on-disk distribution, and drop the `milestone complete` clause. If the intent was that
  `milestone complete` *should* write `complete`, that is a separate behaviour change and
  is not in any criterion.

- [test contract] t-8's `dross task shwo p t-1` row does not test the new code. Cobra
  already does distance suggestion here: `EnforceSubcommandKnown`
  (`internal/cmd/subcommand_guard.go:27-46`) sets `SuggestionsMinimumDistance = 2` and
  emits `Did you mean this? show` from `cmd.SuggestionsFor(typed)` today. That row passes
  before t-6 and t-8 exist, so it pins pre-existing behaviour rather than proving the new
  fallback is reachable on the subcommand path. The genuinely new distance work is on the
  *flag* path (cobra has no flag suggestions), which the next row does cover.
  Suggestion: either assert the message came through the t-6 resolver (e.g. its distinct
  wording/format), or pick a subcommand miss cobra's distance-2 default does *not* catch,
  so the row can fail.

- [test contract] t-8's telemetry row names a constraint the task description never
  states, and the file that would have to change is not in `files`. The `unknown_flag`
  bucket is matched by substring in `internal/telemetry/telemetry.go:351`
  (`{"unknown flag"}, {"unknown shorthand flag"}, {"flag needs an argument"}`). A
  `FlagErrorFunc` that *replaces* cobra's `unknown flag: --title` with the curated working
  invocation drops the event straight into `other`. The contract row correctly asserts the
  bucket survives, but t-8's files are only `subcommand_guard.go`, `flag_hint.go` and
  their tests — if the assertion fails, the fix lands in `internal/telemetry/telemetry.go`
  (and, per `internal/telemetry/telemetry_test.go:423-432` plus README:344, possibly the
  README bucket docs), none of which are in scope.
  Suggestion: state the design constraint in t-8's description — preserve cobra's
  `unknown flag: --x` prefix and *append* the hint — so the contract is satisfied by
  construction rather than discovered as a scope overrun mid-task.

- [test contract] t-7's version-vs-path heuristic has an uncovered failure mode in first
  position. Today `milestone get` disambiguates by arg count (`Args: cobra.RangeArgs(1,2)`,
  `internal/cmd/milestone.go:322-330`); variadic paths destroy that, hence the "args[0] is
  a version only when it is not a known milestone field" rule — a correct call. But the
  consequence is that a *typo'd* path in first position (`milestone get milestone.titel
  milestone.status`) is not a known field, so it is taken as a version and the user gets a
  missing-milestone error instead of the unknown-path error. That directly undercuts the
  sibling row "an unknown path among several errors naming that path", which as written
  only exercises a bad path in a later position.
  Suggestion: add a row fixing the behaviour for an unknown first-position path — either
  it errors naming the path, or it errors naming the version it tried to load and says so
  explicitly. Right now the plan does not decide which.

- [antipattern] Documentation drift: no task owns `README.md` or `docs/dross.1`. The README
  command table advertises `dross state {show,set,touch,bump}` (README.md:186) and
  `dross task {next,show,status,add,remove,edit,move}` (README.md:190). This phase adds
  `task list` (t-1), `state get` (t-4), `project set --unset` (t-2) and `state show --json`
  (t-4) — all four leave those lines stale. Nothing fails:
  `TestReadmeAdvertisesOnlyRealCommands` (`cmd/dross/main_test.go:22-51`) only catches
  *over*-claiming and explicitly allows under-claiming, and `docs/dross.1:117` is already
  stale (it still lists `task {next,show,status}`, missing add/remove/edit/move). So the
  drift is silent and cumulative.
  Suggestion: add `README.md` to whichever task ships the last user-visible verb (or a
  ship-time doc step). Not blocking — no criterion mentions docs — but this is the second
  round of command additions that would land undocumented.

## NOTE

- [granularity] t-3 trips the 5-file heuristic (5 files, 3 packages: `configenum`, `cmd`,
  `milestone`), but I am not recommending a split: the enum definition, the bare-name
  resolver and the pre-Save rejection are one semantic unit that cannot be verified apart,
  and the third package is touched only for a one-line stale doc comment
  (`internal/milestone/milestone.go:29` still says `planning | active | shipped |
  archived`). Fixing it in the same edit is the right call — it is precisely the comment a
  future reader would trust over the new enum.

- [wave order] t-7's `depends_on = ["t-2", "t-3", "t-4"]` is only a genuine output
  dependency for t-4 (`renderMultiGet`) and t-2 (the `board.state_map.<k>` path its
  contract reads). The t-3 edge is file contention on `internal/cmd/milestone.go`, not
  output. No parallelism is lost — t-7 is gated on t-4 regardless — and with atomic
  per-task commits the serialization is worth keeping. Recording it so the edge is not
  later mistaken for a real data dependency.

- [granularity] t-9 is one file with two contract rows, which normally reads as a merge
  candidate into t-8. Here it is justified and the justification is written down (D2):
  `cmd/dross/main_test.go` is the only package that sees the assembled `newRoot()` tree,
  and its second row — every curated replacement invocation must resolve against that tree
  — is real anti-rot work, not a formality.

- [strength] The four mis-reaches were checked against the real tree, not assumed. Verified
  independently: `security run` has no `--new` and unconditionally creates a fresh run dir
  (`internal/cmd/security.go:86-130`), so the working invocation is bare `security run`;
  `task edit` declares `--title/--wave/--covers/--depends-on` and no `--files`;
  `phase create <title>` is positional; `task status <phase-id> <task-id> <status>` is the
  real remap t-6 asserts. And `assets/prompts/secure.md:42` is the *only* occurrence of a
  broken invocation across `assets/`, `docs/` and README — so t-6's single-prompt file list
  is exactly right, not an undercount.

- [strength] D3 holds: `cmd/dross/main.go:67` already calls `EnforceSubcommandKnown(root)`,
  and the guard recurses the whole tree (`subcommand_guard.go:21-23`), so t-8's "no
  main.go edit" is correct rather than optimistic. Likewise t-2's "when a 10th Board field
  is added" is calibrated to reality — `internal/project/project.go:114-124` has exactly 9
  fields, with `GitHubProject` and `StateMap` the two absent from both `readDotted` and
  `writeDotted` (`project.go:189-201`, `:348-360`), which is c-6 precisely; and t-5's
  issues-vs-advisory-warnings row matches doctor's actual two-class structure
  (`internal/cmd/doctor.go:39-46`).

- [strength] Contracts are falsifiable and name the pre-change failure rather than
  restating the feature — "pre-change this failed with `unknown flag: --json`", "`[]` and
  exits 0 — never `null`", "byte-unchanged (no truncate-then-fail)", "the reflection test
  is the only shape that stays true when a 10th Board field is added". Almost no row in
  this plan would pass by accident.

## Summary

No blocking findings — coverage is complete, the locked decisions are respected, and the
plan is unusually well grounded in the actual tree; the five flags are one wrong rationale
(t-3's `milestone complete` claim), two contract rows that don't yet do the work they
promise (t-8's subcommand-typo row, t-7's first-position unknown path), one unstated design
constraint with an out-of-scope fallback file (t-8 vs the telemetry taxonomy), and silent
README/man drift nobody owns.
