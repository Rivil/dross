# Plan Review — lane-selector-custom

Reviewed: 2026-08-30 (second pass)
Plan: 7 tasks across 4 waves

## BLOCKING
(none)

## FLAG

- [antipatterns/files] t-2 moves `shellQuoteArg` out of `internal/cmd`, but
  `internal/cmd/run.go:200` is its second caller and is in no task's files.
  t-2 says the function "MOVES from internal/cmd/test.go into internal/testlane
  and test.go calls it there" — `run.go` (`line += " " + shellQuoteArg(a)`)
  then references a symbol that no longer exists in package cmd. The break is
  compiler-caught, so the risk is not that it ships; the risk is the cheapest
  fix under compile pressure — leaving a second copy in `cmd` — which defeats
  the stated point of the move ("one quoting implementation serves both the
  plain append path and the template path").
  Suggestion: list `internal/cmd/run.go` in t-2's files, and say whether
  `dross run`'s selector quoting also routes through the moved function or
  keeps a cmd-local wrapper.

- [test-contract] t-3 introduces a THIRD consent-line form and pins no
  disjointness between it and the existing one. `laneConsentLine` already has
  two namespaces — a bare command, and `laneFrame + len(prepare) NUL prepare
  NUL len(command) NUL command` — documented as "disjoint by construction, not
  by being hard to hit" (`trust.go:290-299`). t-3 adds a template-only frame
  and its three contract items cover: template added (stale), join changed
  (stale), no-template lane unchanged. Nothing pins that a prepare-lane and a
  template-lane cannot produce the same framed string, and nothing extends the
  NUL refusal (`trust.go:319`, currently `lane.Command` and `lane.Prepare`
  only) to the two new fields — a template carrying a NUL is exactly the input
  that makes a frame forgeable. A collision here is c-2's failure mode
  literally: an untrusted line running under a grant issued for a different
  one. The lane+prepare+template case is also unpinned.
  Suggestion: add a contract item asserting the template form cannot collide
  with the prepare form (and that a NUL in selector_template or selector_join
  yields the empty line, as Command and Prepare do).

- [test-contract] t-5's "whole-lane helper reading laneProblems" does not say
  what it is fed, and the two readings behave differently.
  `laneProblems(p *project.Project) []string` (`validate.go:202`) walks EVERY
  lane and includes the cross-lane duplicate-name check. Fed the whole modified
  project, `lane edit` refuses because some OTHER hand-edited lane is broken —
  and `lane edit` is now the tool c-3 exists to provide for fixing exactly that
  lane. Fed a synthetic one-lane project, the duplicate-name check is lost and
  the label is a fabricated ordinal (the shape `laneSelectorRefusal` already
  uses: `laneLabel(0, name)`, `test_lane.go:68`). No contract item distinguishes
  the two, so either can ship as "the widened gate".
  Suggestion: state which project the helper validates, and pin the unrelated-
  lane case one way or the other in t-5's contract.

- [antipatterns/files] t-6's declaration-time warning has no test file to live
  in. Contract item 3 is "if `lane add` / `lane edit` stop printing the warning
  ... the declaration-time test fails", but t-6's only test file is
  `internal/cmd/validate_lane_test.go`; every existing lane-add assertion lives
  in `test_lane_test.go` and every lane-edit one in `test_lane_edit_test.go`
  (t-5's file). Same shape as the first pass's blocking finding, one task over.
  Suggestion: name the file the declaration-time assertions go in.

- [granularity] t-5 is two tasks wearing one id. It carries both criteria it
  covers as separable bodies of work: (a) the two NEW flags on add and edit
  plus their echo — c-1; (b) widening `lane edit` to match/command/selector/
  empty_exit, re-registering --empty-exit as a string slice, replacing the
  refusal gate, and rewriting Short/Long/the no-op error/the doc comment — c-3.
  Nine contract items, the largest in the plan, and the c-3 half is the one
  that can write a lane validate rejects. The split buys no parallelism (both
  halves sit in `test_lane.go` and would serialize), so this is about atomic
  commits and reviewable blast radius, not wave time.
  Suggestion: split on the criterion boundary, or accept the size deliberately
  and say so.

## NOTE

- [antipatterns] t-2 does not say whether `Expand` receives raw matched paths
  or already-derived args. `optionSafe` runs INSIDE `Derive` (`selector.go:97`),
  so under the template_schema lock (selector shapes, template places) the
  pipeline is Derive → Expand and the `-x.go` → `./-x.go` contract item is a
  second, idempotent application. It is still a meaningful unit assertion on
  `Expand`; the parameter name `paths` just reads as the pre-Derive set.

- [antipatterns] The moved quoter has to be exported to be called from
  `internal/cmd`; t-2 spells it `shellQuoteArg` throughout. Naming, not design.

- [antipatterns] After t-5, `--empty-exit` is an `IntSliceVar` on `lane add`
  (`test_lane.go:236`) and a string slice on `lane edit` — so `--empty-exit ""`
  clears on edit and is a parse error on add. Defensible (there is nothing to
  clear at add time), but the two verbs' flag types now differ silently.

- [locked-decisions] All five locks hold. t-1's refusal to reject unknown
  `{...}` tokens (`a{2,3}` is legitimate template text) reads `template_fence`
  correctly; t-2's verbatim-template item is its guard; t-6 keeps the warning
  out of the run-time header per `warning_surface`; t-1's "template with no
  selector is refused" matches `template_schema`. t-6's contract naming only
  the selector case is complete rather than partial — a template cannot exist
  without a selector, so "a lane that also scopes" has one spelling.

- [coverage] All four criteria covered: c-1 by t-1/t-2/t-4/t-5/t-7, c-2 by
  t-2/t-3/t-4, c-3 by t-5/t-7, c-4 by t-6/t-7.

- [wave-order] Re-checked against the amended file sets: still dependency-
  driven. t-3/t-4/t-5 all need t-1's struct fields; t-6 needs t-5's edit
  surface before a declaration-time warning on `lane edit` exists to print; t-7
  documents flags t-5 and t-6 register. Parallel tasks within a wave hold
  disjoint files (t-1 validate.go / t-2 test.go; t-3 trust.go / t-4 test.go /
  t-5 test_lane.go). Nothing in wave N+1 could drop to wave N.

- [forbidden-actions] No violations. `runtime.mode = "native"`; r-01's
  `make install` obligation is stated in t-7 where `assets/prompts/options.md`
  is edited.

- [strengths] Three things this plan does that plans usually do not:
  t-2 states WHERE quoting has to happen and why ("nothing downstream can tell
  template text from a substituted path"), which is the actual reason the move
  is required rather than tidy; t-5's widened-gate item quotes the invariant it
  protects in falsifiable form ("the CLI would have written a lane
  `dross validate` rejects"); and t-7's third item guards against its own guard
  being deleted rather than re-pointed — "a doc telling the reader to hand-edit
  a [[runtime.test_lane]] block passing clean" is the failure mode a re-point
  invites, and almost no plan writes it down.

## Previous pass

- BLOCKING, t-7 missing `options_docs_test.go`: CLOSED. The file is listed, the
  re-point (not deletion) of `TestOptionsDocumentsTheSelectorSurfaceCorrectly`
  is described, the anti-deletion item is added, and the `lane add`-only flag
  regexp (`options_docs_test.go:296`) is explicitly widened to `lane edit`.
- FLAG, t-5's refusal gate too narrow: CLOSED in substance — the gate now reads
  `laneProblems`, which is where the empty match list, blank command and
  non-compiling glob are reported. Its scope is newly ambiguous (see FLAG 3).
- FLAG, t-5 covers two of c-3's four fields / clear gestures undecided: CLOSED.
  `--empty-exit` is re-registered as a string slice on the `--toolchain`
  precedent, and one item pins both `--match ""` and `--empty-exit ""` plus the
  non-numeric refusal.
- FLAG, `Expand` has no error contract and quoting ownership unstated: CLOSED.
  The signature is given with its error return, the refusal item is added and
  tied to t-4's fence, and the quoter is stated to move into `internal/testlane`
  — which surfaces the run.go call site (see FLAG 1).
- FLAG, stale remove-then-re-add claim lives in the binary: CLOSED. t-5 names
  Short, Long, the "nothing to change" error and the doc comment, with a
  `lane edit --help` contract item.
- FLAG, t-1 pins only refusals: CLOSED. Item 6 is the accept-side case and even
  names the predicate-too-broad failure it exists to catch.
- FLAG, `lane list` deferral rests on an unpinned echo: CLOSED. t-5 echoes the
  template and join in both verbs' summary output, with an item that names the
  deferral resting on it.

## Summary
Every first-pass finding is genuinely closed; what remains is one mechanical
omission the move to `internal/testlane` created (run.go), one unpinned
consent-frame disjointness property that is c-2's own failure mode, and two
scope questions (t-5's gate input, t-5's size) worth settling before execution.
