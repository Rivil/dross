# Plan Review — milestone-closeout-check

Reviewed: 2026-11-15
Plan: 7 tasks across 5 waves

## BLOCKING
(none)

## FLAG
- [wave-order] t-3 sits in wave 1, but the prompt it writes runs `dross milestone complete <version> --check` (added by t-7, wave 5) and `dross milestone ack … --reason` (added by t-5, wave 3). Two consequences. First, none of t-3's contracts checks that those invocations resolve against the real Cobra tree. The repo already has that pattern in `TestProtectedPromptCommandsResolve` (internal/cmd/protected_prompts_test.go), which resolves `dross protect` and its flags, and t-3 cannot add an equivalent check while the verbs don't exist. A typo such as `--checks` or `milestone acknowledge` would pass every t-3 contract. Second, under rule r-01 any `make install` between waves 1 and 5 makes a prompt live that calls a flag the installed binary rejects.
  Suggestion: make t-3 depend on t-5 and t-7 (wave 6, or wave 5 next to t-7 if file overlap allows), and add a contract that `complete --check` and `ack --reason` resolve through `Milestone()` with their flags looked up.

- [test-contract] t-4 doesn't say which clearing command it names for a keyless or ambiguous survivor, and the obvious one is a trap. A keyless record has no key, so close-out cannot match it against survivors.toml. Running `dross survivor accept <file>:<line> --op X` resolves a key at accept time, succeeds, and leaves the close-out item blocking. An ambiguous record (ambiguous=true) is likewise cleared only by a route, never by accept (t-4 states this for clearing logic). c-4 requires each line to name the command that clears it, but no t-4 contract pins the command for these two shapes.
  Suggestion: add a contract that the keyless line names ack (or re-verify) and the ambiguous line names `dross survivor route`. In both cases the line must not name `survivor accept`.

- [prompt-budget] assets/prompts/milestone.md is 14,687 B against the 15,000 B cap in `TestPromptByteBudget`, which leaves 313 B. t-3 adds a `--check` step to §8 step 1, a `**When** \`closeout=unresolved\`` fetch line, and the wording that the PR opens only once --check is clean. That is plausibly over 313 B. The contract only says the budget test will fail. The plan doesn't say what moves into `_milestone.closeout.md` or what gets trimmed.
  Suggestion: in t-3's description, name the existing §8 prose to move or compress (step 2's protected-branch paragraph is the largest candidate) so the writer isn't left to improvise trims under a red budget test.

- [test-contract] t-6's refusal contracts assert `cap.created==0` but not that a refused complete has no side effects. Today the verify gate runs before `routeMilestoneHead` (the step that pushes local milestone commits). If t-6 puts the combined gate after it, a refusal still pushes, and every listed test still passes.
  Suggestion: in the all-kinds refusal test, also assert that origin's `milestone/<v>` ref is unchanged and that an unpushed local commit stays unpushed (t-7 already uses this assertion for --check).

- [locked-decision] c-5 says "Any unresolved item can be acknowledged", and c-4 counts unverified as an unresolved kind. The locked `force_unverified` decision, which t-4 and t-5 follow, makes unverified items non-ackable: t-5 refuses `phase:<slug>` for a done-but-unverified phase, and t-4 rules that ack `phase:x` doesn't clear unverified. The plan follows the lock correctly. But read literally, c-5 is unmet for the unverified kind, and /dross-verify could mark it partial.
  Suggestion: state in t-5's description that "any unresolved item" in c-5 means every ackable kind, per locked force_unverified, so the criterion-to-test mapping at verify has the interpretation written down.

- [granularity] t-4 covers 6 criteria with 8 multi-clause contracts in 2 files. It builds four collectors (one includes the routed-survivor disposition via `boardsync.Disposed` with scope and freshness rules), error-isolation per kind, the applied/stale ack partition, and deterministic rendering. Measured by behaviour rather than file count, it is the largest task in the plan. A miss in the routed-survivor branch would be reviewed alongside the render code. Separately, t-1 (6 files, spanning the verify package and verify.md), t-3 (5 files) and t-6 (5 files, 8 criteria) cross the 5-file threshold. Those are mostly source+test pairs, so they matter less.
  Suggestion: consider splitting t-4 into (a) the four kind collectors with per-kind error isolation and (b) the ack join, `Blocking()` and render. t-5 needs only (a)+(b)'s membership query, so the dependency graph would be unchanged.

## NOTE
- [coverage] All 11 criteria are covered: c-1/c-2/c-4 by t-4+t-6, c-3 by t-1+t-4+t-6, c-5 by t-2+t-5+t-6, c-6 by t-7, c-7 by t-3, c-8 by t-4+t-6+t-7, c-9 by t-2+t-5+t-7, c-10 by t-6, c-11 by t-4+t-6+t-7. Every file the plan references either exists or is created by the task that names it (milestone_closeout*.go, milestone_ack*.go, milestone_check_test.go, _milestone.closeout.md).
- [locked-decision] t-4 and t-5 use `survivor:<file>:<line>:<op>` for keyless records, which extends the locked `survivor:<key>` grammar. Real keys are sha256 hex (survivor.KeyFor), so the two forms can't collide, and t-2's parser handles the inner colons. This is an extension, not a contradiction, but the ack_storage decision text doesn't mention it.
- [strength] The test contracts are unusually concrete. Each names the fixture shape and the exact expected output: NotDone == [built, ghost] in roadmap order with built once, sha256 of every .dross file plus no FETCH_HEAD for --check, and PR body byte-equal to today on the empty path. They will kill mutants rather than just execute lines.
- [strength] The plan is grounded in existing code. `milestoneUnverifiedPhases`, `boardsync.Disposed` (its finalized/newer/in-scope rule matches t-4's routed-survivor contract), `acceptedReasons`/`routedSurvivors`, `msPRCapture`, `finalizeFixture`, `TriggerVocabulary` and `TestCommonPathVocabularyPinned` all exist as described. It also anticipates the read-only trap: the collector must not `EnsureIDs`-mint into source specs.
- [strength] The regression blast radius is scoped correctly. milestone_verify_gate_test.go is the only open-mode complete test that seeds a phases array, and t-6 owns it and re-seeds it. milestone_protected_test.go's `msComplete --force-unverified` and the pr_base/push_head tests have no milestone toml, so the new not-done kind leaves them alone. The t-5 → t-6 → t-7 serialization is justified by their shared edits to internal/cmd/milestone.go and README.md.

## Summary
The plan is sound and fully covers the spec with well-grounded, specific contracts. Before execution, fix t-3's ordering against the verbs it calls, pin the clearing command for keyless and ambiguous survivors, and plan for milestone.md's 313 B of budget headroom.
