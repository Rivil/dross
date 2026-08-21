# Plan Review — board-mirror-reaper

Reviewed: 2026-08-21
Plan: 13 tasks across 5 waves

## BLOCKING

- [coverage] **c-1 and the plan disagree by two cards.** c-1 requires that after one
  run "the 90 stranded cards (20 task, 2 phase, 2 milestone epics, **2 quicks**, 64
  backlog) report a resolved state". t-2 decides quicks classify *unattributable* and
  are "never closed by the sweep", and t-12's contract makes the divergence explicit:
  "88 swept plus the 2 quicks the classifier reports unattributable and the operator
  closes by hand". t-2's reasoning is correct — I confirmed there is no quick
  completion record on disk (`.dross/` holds no quicks log; `state.json` carries only
  the version counter, and monotonicity proves a later bump happened, not that the
  quick finished). But the plan cannot satisfy c-1 as written, and verify reads
  criteria literally, so this lands as a partial on the phase's headline criterion.
  Suggestion: amend c-1's text before execute — either drop quicks from the 90 and
  restate as 88 swept + 2 named for manual close, or state the quick exclusion in the
  criterion itself. This is a spec edit and belongs to the author/lead, not the plan.

- [granularity] **t-13 adds a method to `forge.BoardClient` but omits every in-repo
  implementer outside `internal/forge`.** Two test fakes are assigned to
  `boardCtx.client` (`forge.BoardClient`) and implement the interface in full:
  `pullFakeClient` at `internal/cmd/issue_task_pull_test.go:362` (assigned at lines
  332, 353, 396, 420, 430) and `fakeInboundClient` at `internal/cmd/issue_test.go:1298`
  (assigned at lines 2081, 2096). Adding `SetStateRaw` to the interface makes package
  `internal/cmd` fail to compile. t-13's file list is forge-only, and t-13 does not
  depend on t-1 (which does list both files), so t-13's commit alone is red.
  Suggestion: add `internal/cmd/issue_task_pull_test.go` and
  `internal/cmd/issue_test.go` to t-13's files. Note this also collides with t-1 —
  see the wave-1 collision flag below.

- [antipattern] **t-8 wires `--apply` but does not own the test that asserts it is
  unwired.** t-6 creates `internal/cmd/issue_reap_cmd_test.go` containing
  `TestApplyIsDeclaredButNotWired` ("at t-6 `--apply` exits non-zero with a 'not
  implemented' message"). t-8's files are `issue_reap_apply.go`,
  `issue_reap_apply_test.go`, `issue_reap_cmd.go` — the t-6 test file is absent, so
  the assertion survives t-8 and fails deterministically at t-8's commit gate.
  Suggestion: add `internal/cmd/issue_reap_cmd_test.go` to t-8's files and say in the
  description that `TestApplyIsDeclaredButNotWired` is deleted there — a scaffolding
  assertion with an explicit retirement point, not a leftover.

## FLAG

- [wave-order] **t-6 is in wave 2 and depends on t-4, also in wave 2.** Every other
  dependency in the plan crosses a wave boundary correctly; this is the one
  inconsistency. Under sequential execution `depends_on` saves it, but a wave is a
  parallel batch by definition and a parallel executor could start t-6 before
  `issue_reap_discover.go` exists.
  Suggestion: either move t-6 to wave 3 (cascading t-8/t-9 to wave 4, t-10 to 5, t-11/t-12
  to 6), or have t-6 consume only t-2's classifier and let t-8 be the first task that
  unions in t-4's marker sweep. The second is cheaper and t-6's
  `TestReapWholeBoardCoversFiveLanes` does not need discovery to pass.

- [wave-order] **t-1 and t-13 are both wave 1 and both edit
  `internal/forge/forge.go` and `internal/forge/youtrack.go`.** Run in parallel they
  collide on two files. t-1's claim on those files is also thin: the only forge
  references are comments — `forge.go:522` ("The caller (issue phase-sync) discards…")
  and `youtrack.go:37,303,304` — none of which match t-1's own stated regex
  `dross issue [a-z]+-[a-z]+`, so `TestNoHyphenatedIssueVerbInGoSource` will not force
  those edits anyway.
  Suggestion: drop `internal/forge/*` from t-1 (fold the comment tidy into t-13, which
  is editing all four clients regardless), or make t-13 depend on t-1.

- [locked-decision] **The `prompt_edge` lock has no guard.** The decision reads "No
  prompt emits reap", yet t-9 rewrites `assets/prompts/watch.md` to render the
  stranded count, and neither t-5's `TestEveryPromptIssueInvocationResolves` nor
  t-11's docs extension would object to a `dross issue reap --apply` line appearing
  there — `reap` resolves against the cobra tree, so the guard passes. Every other
  locked decision in this spec has a test that reddens when it is violated; this one
  is the exception, and it is the one a future prompt edit is most likely to breach.
  Suggestion: add to t-9's contract — a scan of `assets/prompts/*.md` for
  `dross issue reap` reports zero hits (doctor's Go-side remedy string is exempt
  because it is not a prompt).

- [coverage] **c-7's concrete claim gets no artefact.** c-7 names six cards —
  DRO-33/36/37/38/95/96 — and says they "are classified, not skipped". t-4 tests the
  *shape* against synthetic fixtures ("the DRO-33/36/37/38/95/96 shape"), which is
  right for a unit test, but t-12's proof contract records per-lane counts, the 8
  correctly-open cards, and the empty re-run — it never requires those six ids appear
  in the live plan output. Nothing in the phase will demonstrate c-7's actual
  sentence.
  Suggestion: add a line to t-12's contract: proof.md names each of the six by issue
  id with the lane and justifying record the marker sweep recovered for it.

- [granularity] **t-1 is 23 files across three packages** (`internal/cmd`,
  `internal/forge`, `internal/configenum`). This is defensible — a cobra rename that
  breaks every call site cannot be split without a red intermediate commit — but it is
  well past the split threshold and the description does not say why it must be
  atomic.
  Suggestion: keep it whole; state the atomicity reason in the description so a
  reviewer does not read it as an unsplit blob. Trimming the forge files (above) takes
  it to 21.

- [granularity] **t-9 spans two independent surfaces.** `doctor.go` + `doctor_test.go`
  and `watch.go` + `watch_test.go` + `watch.md` share only the read-only classifier
  call; neither needs the other, and the four contract lines partition cleanly (three
  doctor, one watch).
  Suggestion: split into t-9a (doctor advisory) and t-9b (watch digest + watch.md),
  both wave 3, both depending on t-6. This also gives the `prompt_edge` guard above an
  obvious home.

- [antipattern] **t-13 puts `SetStateRaw` on `BoardClient`, against this repo's own
  precedent.** `internal/forge/forge.go:184-191` documents exactly the opposite
  choice for `IssueLinker`: "deliberately NOT part of BoardClient: GitHub's REST
  issues API has no generic issue-link primitive, so that backend must fail an
  interface assertion rather than satisfy the method with a no-op — a silent stub
  would make the 'no link possible here' path untestable and invisible." t-13's own
  contract concedes GitHub "has no column model" and defines `SetStateRaw` there as
  reopen/close. That is not a no-op, but it *is* lossy: a card whose journalled prior
  state was "In Review" restores to plain `open`, so c-9's "restores those cards to
  the state they held before the run" is only approximately true on GitHub — and
  nothing in the plan says so.
  Suggestion: either make it an optional `StateWriter` capability asserted at t-10's
  call site (matching `IssueLinker`, with GitHub warning by name the way the
  no-linker path already does), or keep it on `BoardClient` and add a contract line
  that GitHub's lossy restore is named in the undo report rather than reported as an
  exact restore.

- [antipattern] **`.dross/reap-log.json` git-tracking is undecided.** `.gitignore`
  ignores `handoff.md`, `local.toml`, `state.json`, `security/`, `quality/` and
  `techdebt/` under `.dross/`, each with a comment explaining why. A new
  `.dross/reap-log.json` is tracked by default, so an applied sweep dirties the tree,
  ship's clean-tree auto-commit sweeps it into the phase branch, and the squash drags
  one machine's undo ledger into every later tree — the exact failure mode the
  `state.json` ignore comment describes. t-3 says nothing either way.
  Suggestion: decide in t-3's description and add the `.gitignore` entry (with the
  reason) to t-3's file list, or state explicitly that the ledger is meant to travel
  with the branch.

- [antipattern] **t-5 lists `assets/prompts/pause.md`, which has nothing to rewrite.**
  It carries no `dross issue` invocation; its only `phase-sync` is line-wrapped prose
  at `pause.md:58` ("…then re-run\n      phase-sync") that t-5's own regex
  `dross issue [a-z]+-[a-z]+` cannot match.
  Suggestion: drop it, or keep it and say the edit is a prose tidy the guard will not
  force.

## NOTE

- [strengths] The test contracts are the best part of this plan and unusually good in
  general: nearly every line names the *wrong implementation* that would redden it —
  "a classifier reading iss.State or the dross/status label fails here", "a log
  written from the post-close read-back records the terminal state and makes undo a
  no-op, and that fixture fails", "hardcoding \"Open\" fails". That is a contract you
  can execute against without re-deriving intent.

- [strengths] Anti-vacuity is designed in rather than assumed:
  `TestTaskSyncEdgeRegexIsNotVacuous` (a rename silently emptying the emit-set is the
  real risk and it is named), "the guard cannot pass vacuously — an empty lane
  registry or an empty reflection result is a t.Fatal", and t-6's
  `TestApplyIsDeclaredButNotWired` refusing a flag that silently no-ops. This matches
  the existing `boardNamespaceFields` helper's own `t.Fatal` on zero fields.

- [strengths] t-12 is sequenced correctly for a live production write: `make build &&
  make install` as its literal first step (rule r-01), and a hard dependency on t-10
  so a bad batch is reversible before 90 cards on the real DRO board are touched.
  t-2's refusal to close quicks on version-monotonicity inference is the same
  instinct — it surfaces a spec conflict rather than fabricating evidence.

- [wave-order] Verified: `board.Board`'s map-typed field set is exactly five —
  `Milestones`, `Phases`, `Quicks`, `Backlog`, `Tasks` (`internal/board/board.go:31`).
  `Dismissed` is a `[]string` and `LastPull` a `time.Time`, so neither pollutes
  `boardNamespaceFields`. t-6's `TestReapWholeBoardCoversFiveLanes` and t-7's guard
  rest on a correct premise.

- [test-contract] t-3's `TestReapLogIsNotABoardNamespace` is close to vacuous. The
  journal lives in a standalone `internal/reaplog` package, so reflection over
  `board.Board` cannot find a new map field no matter what t-3 does — nothing inside
  t-3's scope could redden it. It documents the design choice rather than guarding it.
  Harmless, but it is the one contract line in the plan that cannot fail.

- [coverage] t-9 declares `covers = []` and says so plainly ("Delivers the locked
  prompt_edge decision (no criterion of its own)"). Correct and honest — recording it
  here so verify does not read the empty field as an omission.

- [forbidden-actions] No violations. `runtime.mode = "native"`, `test_command =
  "go test -count=1 ./..."`; no task implies docker, a package manager, or a non-Go
  toolchain. Rule r-01 is the only project rule and t-12 honours it explicitly. No
  global rules file exists at `~/.claude/dross/rules.toml`.

- [coverage] Criteria mapping is otherwise complete: c-1 → t-6/t-8/t-12, c-2 → t-6,
  c-3 → t-2, c-4 → t-2/t-8, c-5 → t-8, c-6 → t-7, c-7 → t-4, c-8 → t-1/t-5/t-11,
  c-9 → t-3/t-8/t-10/t-13. Every file referenced exists in the repo except
  `internal/cmd/issue_verb_shape_test.go`, which t-1 creates before t-5 and t-11
  extend it.

## Summary
Structurally strong and unusually well-instrumented plan whose three blockers are all
mechanical — a two-card conflict between c-1 and t-2's honest quick handling, and two
file lists that omit edits their own changes force — plus a design tension worth
settling: `SetStateRaw` on `BoardClient` contradicts the repo's documented reason for
keeping `IssueLinker` off it.
