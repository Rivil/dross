# Plan Review — scoped-commit-gate

Reviewed: 2026-11-06
Plan: 22 tasks across 6 waves

## BLOCKING
(none)

## FLAG
- [test-contract] c-9 says a scoped green covers only paths that "reached at least one collected test", but no task enforces the word "collected" per path. t-13 sets Covered from relate's listing plus a batched exit 0, and forbids reading test output. Only an all-empty batch records nothing. vitest's `filterFiles` keeps a test if any filter matches (`filters.some`, vitest 4.1.11 in barktime's node_modules), so a filter matching nothing is dropped silently and the run exits 0 on the rest. If relate maps a to x.test.ts and b to y.test.ts, and y is not collected, Covered is [a, b] and b passes as measured. t-15's "a miss" case and t-18's checks only exercise relate omitting a path, never relate naming a test the run skips. relate also runs on helicon's host while the batched line runs in an image built through Dockerfile.test.dockerignore, so the two sites don't share a tree by construction. Risk on barktime is low because relate reads vitest's own spec list, but t-7 documents `relate` as a general contract.
  Suggestion: decide this before t-9. Either reword c-9 to what the design proves (relate named a test and the batched line passed), or extend t-9's parser contract so a named test missing from the tree refuses the whole output. Either way, t-7's docs must say relate vouches that every test it names is collectable.

- [test-contract] t-7's `--vouch=false` contract conflicts with t-21's rule. t-21 makes `relate` a problem on a lane with no vouch, and `laneRefusal` validates the whole proposed lane (internal/cmd/test_lane.go:132). So `lane edit x --vouch=false` on any real vouching lane leaves relate behind and is refused. The contract only passes on a fixture lane that could not validate in the first place. Turning vouch on has the same shape: `--vouch` alone is refused with no relate, and `--relate` alone is refused with no vouch, so the only route is both flags in one edit. No contract says that works.
  Suggestion: state what `--vouch=false` does with relate (clears it, or needs `--relate ""` in the same edit), and add contracts for one-edit opt-in and opt-out on a lane that has a relate.

- [plan-risk] t-18's lane "over in-container.sh" cannot honestly meet t-3's empty_exit rule, which t-21 keeps. A vouching lane needs a non-empty empty_exit that doesn't list 1. barktime's run-suite.sh deliberately omits `--passWithNoTests`, so vitest exits 1 on nothing collected, and in-container.sh passes that status through. t-3's own refusal says "have the lane exit a code of its own", but t-18 no longer mentions a wrapper. barktime must either declare a code nothing emits, which passes validate while contradicting what _options.lanes.md says the field means, or add a wrapper the plan doesn't name. Safety holds, because an empty batch reads as red, but t-13's "a batched exit in empty_exit" arm is then unreachable on the one real repo.
  Suggestion: say whether empty_exit still applies to a relate lane. If it does, t-18 must name what emits that code.

- [test-contract] t-16 and t-17 route `uncovered:` but not exit 5, which the relate model makes routine. t-13 returns exit 5 when relate lists nothing, so a task touching only a test-less .svelte hits it. A docs-only `--files` run hits it too, through the existing no-lane path. Three things then go wrong:
  - `_execute.test-not-run.md` says exit 5 means no lane matched, says to widen a lane or run bare, and says it is never "a reason to commit". That contradicts t-14, which admits a docs-only commit on a current exempt grant with no record. The same file calls exit 7 a failed `prepare`, which is wrong for a relate failure.
  - In solo, the writer prompt turns any unrun test into `red: not run`, and _execute.solo.md treats a red report as a failed task. A correct test-less or docs-only task is failed and stashed instead of routed onward.
  - t-17 has no `uncovered:` or exit-5 line at all, unlike t-16, and quick.md's exit-status text only names 1, 3 and 4.
  Suggestion: add `assets/prompts/_execute.test-not-run.md` to t-16's files (t-16's writer fence would block it otherwise), and add contracts for exit 5 on exempt-only and test-less paths in the execute, writer and quick routes.

- [antipattern] t-9 omits the pathfence ledger. Its "Preview shows the relate line" and the listed lane_preview_json.go point to a new field on `previewLaneReport`, and that struct's string fields are filed row by row in not_paths.txt (`cmd.previewLaneReport.Line` and others). `TestEveryPathShapedFieldIsDeclared` fails on an unfiled tagged string, and every other task adding a serialized string handles this. t-9's surface is 48,886 bytes, over the 40,000-byte writer threshold, so a solo run hands it to a writer and the fence refuses the ledger edit. Overloading the existing `line` field avoids the ledger but breaks that field's documented meaning, "what the gate would spawn".
  Suggestion: add internal/cmd/testdata/pathfence_scan/not_paths.txt to t-9's files, with a contract line naming the new field.

- [test-contract] t-18 needs what t-13 never promises. t-18's record fails unless the run log shows relate mapping a .svelte path to its tests, but capturing relate's stdout for parsing can bypass `tr.out`, which is what feeds the log. No t-13 contract requires it in the log. Nothing requires relate to spawn as aux either: test_output.go says aux output is what "no parser should be asked to claim", so a non-aux relate puts its TSV in front of the summary parsers. A t-13 that passes all 20 lines can still leave t-18 failing in wave 6, the most expensive place to find out.
  Suggestion: add a t-13 line that relate's stdout reaches the run log and that relate spawns as aux.

- [test-contract] t-6 binds relate into the grant but not into the refusal. `consent.LaneRefusal` prints only prepare and command, under a comment saying a refusal must show "EVERY line the grant covers", and warning about the case where the line that changed is not shown. After t-6, a relate edit produces a stale refusal that shows unchanged text and tells the human to re-consent. t-8 covers `dross trust --lane` output, not this message.
  Suggestion: add a lane_test line that a stale refusal on a vouching lane prints the relate line. lane.go is already in t-6's files.

- [plan-risk] A lane's derived toolchain ignores relate. `testlane.Toolchain(command, prepare, override)` reads only those two lines, and lane_locality.go routes on the result. A relate line needing `node` is never probed, so a host without it gets chosen, relate exits 127, and the run reports exit 7. The exit-8 and fallback routes exist for this case. Neither toolchain.go nor lane_locality.go is in t-9 or t-13's files.
  Suggestion: add both files to t-13 or t-9 with a contract that relate's binary joins the probe, or have t-7's docs and t-18 require a `toolchain` override on relate lanes.

- [test-contract] relate's input shape is unspecified. t-21, t-9 and t-7 define relate's stdout but never how paths reach it. `laneRunLine` has two placements, appended or through `selector_template`, and nothing says which applies to the relate line or to the batched line, or whether a vouching lane may declare a template at all. Two reasonable implementations differ, and t-18's script must match one.
  Suggestion: state the placement for both lines in t-21 or t-9, and have t-7's docs give the argv form next to the stdout form.

- [wave-order] t-20's stop is not wired to anything. depends_on only sees a task's status, so t-20 finishing with `t9_may_start = false` still unblocks t-9, and t-9's contracts never read the record. The stop relies on a human reading it, which a solo run doesn't provide. t-20 also has no depends_on for t-19, whose counts its first contract needs. t-19's own contract still says a projection over 10 minutes means t-9 does not start, and its record still reads `t9_may_start = false`, so the plan carries a stop condition it has silently superseded. t-21 is not gated either, and t-6, t-7 and t-8 build on it.
  Suggestion: say t-20 stays pair-mode or marks itself failed on an over-budget verdict, and state that t-20's record replaces t-19's.

- [granularity] t-22 is a grab bag: an exempt-fence fix, a config-list fix and a doc correction across three packages. Its doc contract is also misplaced. It puts the ARCHITECTURE.md check in internal/mutation/construct_test.go, while the construct-range doc test lives at internal/cmd/pad_residue_test.go:163, which t-22 doesn't list. Requiring the entry to name "construct.go's current line" also turns a drift doctor already reports into a test that fails on any later edit to construct.go; lossless_docs_test.go leaves older anchors to doctor for that reason. Separately, t-8 reaches 11 files and carries two features: the exempt grant, and vouch and relate wording in `dross trust --lane`. t-13 and t-14 carry 20 and 19 contract lines, which is a lot for one pair-mode approval each.
  Suggestion: move t-22's doc check to pad_residue_test.go and assert the claim, not the line number. Leave the task bundled if one commit is cheaper. Split t-8 if the wording can land with t-6, which already binds the same fields.

## NOTE
- [coverage] All 9 criteria appear in `covers`. c-5 rests on t-18 in substance, with t-20 supplying the cost evidence. t-19's c-9 entry is stale, since a timing record proves nothing about attribution.
- [locked-decisions] No locked decision is contradicted. barktime_proof still says `vitest related` while t-18 uses vitest's related selection through a script, which keeps the substance and keeps framework code out of dross. The spec was not updated in the re-plan, so `relate`, the phase's largest design change, appears in no spec decision.
- [rules] rules.toml has one rule (r-01), and t-18's `make install` honours it. No global rules file exists.
- [wave-order] Every depends_on points to an earlier wave, and no task can drop a wave. t-21 and t-22 share validate.go, validate_lane_test.go and ARCHITECTURE.md, and t-13 and t-14 share pathfence/fields.go and fields_test.go. The execute loop takes tasks one at a time, so these don't collide.
- [cost] t-14 refuses every deletion and rename's old side, which matches c-2 and c-3, but any task that removes or moves a file on barktime pays a full run (1130 s on helicon's last measured run). t-20's projection only models leaf edits.
- [budget] verify.md is 14,336 of 15,000 bytes, leaving t-17 664 bytes for its conditional bare run. t-17 calls out quick.md's headroom but not this one.
- [verified-clean] These suspected gaps checked out as fine:
  - `dross local set` is a key allowlist, so a new grant key is not settable unless someone adds it.
  - tamperTarget doesn't handle `sed -i` today, but override.go is in t-6's files, so the contract is achievable.
  - `uncovered:` through `tr.say` reaches stdout when stdout is not a terminal.
  - No cli_surface golden pins lane flags, so t-7 needs none.
  - ARCHITECTURE line anchors are test-enforced only for internal/project, and only t-21 touches project.go.
  - Conservation rows for `dross test` survive as long as t-16 and t-17 keep the fenced bare run, which their contracts require.
  - barktime's 145 test paths with parentheses or brackets are safe: `"$@"` preserves argv through in-container.sh and run-suite.sh, and vitest filters by substring, not glob.
- [strength] The re-plan answers t-19's measurement instead of arguing with it. One relate plus one batched line replaces per-path units, and t-20 tests that model in the real stack before t-9 builds it.
- [strength] relate fails closed where it matters. A malformed line, an escaping test path or a failed relate refuses the whole output or takes a non-miss exit, and an omitted path stays `uncovered:`. This keeps a bad attribution from turning into a silent cover, apart from the collection gap flagged above.
- [strength] The previous review's findings are closed as named contracts, not just acknowledged: t-8's store.go and localstore round trip, t-6's local.toml guard and rawTokens arm, t-4's enumerated config list, the non-vouching red arm in t-13, and vouching-aware soloreview text in t-14.

## Summary
No blocking issues and the relate re-plan fits the measured costs, but settle c-9's "collected" gap, the `--vouch=false` conflict and barktime's empty_exit before t-9 and t-7 start, and route exit 5 in the prompts before t-16 and t-17, or correct tasks will fail in solo.
