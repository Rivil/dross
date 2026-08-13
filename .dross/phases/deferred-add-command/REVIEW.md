# Plan Review — deferred-add-command

Reviewed: 2026-08-13
Plan: 8 tasks across 4 waves

## BLOCKING

(none)

## FLAG

- [locked-decision] `storage_home`'s choice text reads "Write into the current phase's spec.toml
  when current_phase is set; otherwise into a project-level store". t-4 does NOT do that in one
  case: when current_phase IS set but its spec.toml is missing, it writes to `_project`. The plan
  argues this from the decision's `why`, and c-2 does demand it — but the resolution of a
  locked-decision/criterion tension currently lives in a task description where nothing enforces
  it. A future reader of spec.toml alone gets the wrong rule.
  Suggestion: amend `storage_home`'s `choice` to name the unusable-home fallback explicitly (as
  `target_validation` was amended), rather than leaving it as t-4's interpretation.

- [granularity] t-7 touches 7 files and carries two unrelated deliverables: (a) filing the two
  homeless findings with the real verb against the live board — which is c-5 — and (b) doc-truth
  edits to pause.md, inbox.md and status.md, which cover no criterion at all. (b) is also the only
  work in the plan with no criterion behind it, and it is bolted onto the one task that requires a
  live board token and non-hermetic execution. A token failure in (a) blocks (b) for no reason.
  Suggestion: split the three prompt edits into their own wave-4 task with no dependency on t-6.

- [test-contract] t-7's repo-level assertion is not self-retiring, and the existing pattern it
  cites is. `survivor_backlog_repo_test.go` asserts the ABSENCE of problems, so it goes green as
  the backlog drains. t-7's contract asserts the PRESENCE of two specific entries — "fails if no
  entry carries target `mutation-score-truth`". When mutation-score-truth actually runs and
  consumes that item, the test turns red for an entirely correct action, and the fix is deleting
  the test. That is a tripwire aimed at a future phase, not at this one.
  Suggestion: scope the assertion to execution-time observation (as the contract already does for
  handoff.md), or assert the weaker durable invariant — that neither finding's text appears in
  handoff.md's untracked-parking shape — instead of pinning a target that is meant to be consumed.

- [wave-order] `internal/cmd/deferred.go` is in the `files` list of five of eight tasks, and the
  colliding pairs are in the SAME wave: t-1 + t-2 in wave 1; t-4 + t-5 + t-8 in wave 2. The
  `depends_on` graph is correct, but wave membership is what governs parallel execution, and three
  concurrent editors of one file will either serialize by luck or clobber each other. t-5 in
  particular is *only* deferred.go plus its test.
  Suggestion: either mark deferred.go work as serialized, or fold t-5 into t-4 (both are the
  `_project`-addressing surface, both depend on t-1, and t-4 already edits deferred.go).

- [coverage] c-4 says an added item is indistinguishable to `list`, `route`, `unroute` AND
  `dismiss`. t-5's contract exercises all four — but only against `_project`. t-1's contract covers
  only the `json:"-"` tag. Nothing exercises `route`/`unroute`/`dismiss` by `<phase> <idx>` on an
  item that `add` created inside a NORMAL phase, which is c-4's primary reading. The risk is low
  (same array, same index space) but the criterion's own wording is untested on its main path.
  Suggestion: add one contract line to t-4 — add into a phase, then round-trip
  `route`/`unroute`/`dismiss` on the returned index.

- [antipattern] t-3 lists `internal/board/board.go` in `files`, but the description never says what
  changes there. `SetBacklog`/`BacklogID` already exist; the legacy-key remap plausibly needs a
  delete/enumerate accessor, but that is inference, not instruction.
  Suggestion: name the accessor t-3 adds, or drop the file from the list.

## NOTE

- [coverage] t-8 carries c-2, but validate/rename plumbing does not affect whether an add lands
  somewhere addressable — c-2 is already satisfied by t-1 + t-4. The description's justification
  ("widens the store to a first-class home") is defensible, but c-2 on t-8 is closer to attribution
  than to coverage. Harmless; noting so it is not read as a third independent guarantee.

- [forbidden-actions] No rule violations. `runtime.mode = "native"`, so the `go test` /
  `make install` calls are correct, and r-01 is honoured where it bites — t-7 is the only task that
  runs the installed binary and it runs `make install` first. t-7's prompt edits land after that
  `make install`, which is fine only because nothing in the task then executes those prompts.

- [strength] The test contracts are the strongest part of this plan. Nearly every line names the
  mutation that would fail it — "dropping the `id` toml tag fails the reload-and-compare",
  "an implementation defaulting the target to anything non-empty fails here", "a `Target != \"\"`
  skip copied from syncBacklog fails this". These are falsifiable contracts, not restatements.

- [strength] t-7's refusal to write a Go test over `.dross/handoff.md` because it is gitignored
  (verified: .gitignore:12) — and the explicit reasoning that such a test "would be vacuous in CI
  and a re-commission of the exact fault being filed" — is the correct call and the correct reason.

- [strength] t-4's byte-identical error-text assertion between `add --target typo` and
  `route --target typo`, as a single comparison of both outputs, is a real coupling test. It makes
  the shared-validator decision enforceable rather than aspirational, which is what
  `target_validation` actually needs.

- [strength] The t-6 asymmetry (routed adds ARE mirrored, routed syncs are not) is knowingly
  incurred, stated in the task description, and recorded in spec.toml's `[[deferred]]` routed to
  `board-sync-truth`. Recorded scope boundaries beat silent ones.

- [verification] I checked the plan's repo claims rather than taking them: validate.go's
  validTargets is exactly "every phase dir + every slug in EVERY milestone's phases array" as t-2
  describes; `repointDeferredTarget` is at deferred.go:313; `phase.Slugify` drops underscores
  entirely, so `_project` is genuinely unreachable as a real slug; `deferredRoute` today does zero
  target validation and builds its path straight from `phase.Dir`, which is exactly what t-2 and
  t-5 target. All files referenced either exist or are created by an earlier task.

## Summary

No blockers — the plan is well-sequenced with unusually falsifiable test contracts; the real work
is tightening t-7 (split the doc edits out, make its repo-level assertion self-retiring), moving
the `storage_home` fallback from a task description into the locked decision, and resolving the
three-way deferred.go collision inside wave 2.
