# Plan Review — scoped-commit-gate

Reviewed: 2026-11-06
Plan: 26 tasks across 8 waves

## BLOCKING
(none)

## FLAG
- [test-contract] A task touching package.json pays for full-size runs that can never admit it, and no task handles this. Vitest 4.1.11's `filterTestsBySource` returns every spec when a related path matches `forceRerunTriggers` (cli-api chunk, lines 11561-11563), and the default list holds `**/package.json/**`. I confirmed that glob matches an absolute package.json path with barktime's picomatch; I read the rest from source and did not run a relate. `getRelevantTestSpecifications`, the API relate is built on, goes through this filter. So relate names all of barktime's tests (749 tracked test files) and t-13 spawns them as one batched line. t-14 then refuses the commit anyway, because t-4's fixed list beats coverage. §1e's `--files` run is unconditional (execute.md:99-101), so a solo task pays a writer run, a main-loop run and a bare run, each about a full suite (1130s on t-19's figure). zero_test_scoped_run's stated reason is that "a `--files` call never turns into a surprise full-suite wait". The decision's text isn't broken, since this isn't an escalation, but its reason is. t-18's hot-file row doesn't cover it: a hot file can still be admitted, and package.json never can.
  Suggestion: add a t-13 contract that a path on t-4's fixed list never goes to relate and prints as `uncovered:`, or a t-16 and t-17 row sending a file set holding such a path straight to bare `dross test`.

- [test-contract] `uncovered:` isn't the gate's verdict, yet t-16 makes it the trigger for a bare run. t-13 prints the paths relate omitted from the asked list. t-14 judges `snap.Changed` after exempt globs. These differ in two ways. §1e passes `task.files`, "never the git diff" (execute.md:99), so a listed file the task left unchanged and with no test prints `uncovered:`, though the commit would be admitted. And an exempt path that a lane's globs match prints `uncovered:`, though t-14 admits it. t-16's exempt branch covers exit 5 only, and its first contract row requires §1f to send `uncovered:` to the bare branch. Each false `uncovered:` costs the 15-26 min run c-7 exists to remove. The opposite case is harmless: a `no lane matches:` path prints no `uncovered:`, and the commit gate refuses and names it.
  Suggestion: key §1f's and quick §4's bare branch on the refused `git commit`, which already names the paths, or have t-13 leave exempt paths out of `uncovered:` and state in the prompt that it reports the asked list, not the commit.

- [wave-order] t-13 is missing `depends_on = ["t-6"]`, which it needs for its consent guarantee. t-13 spawns relate and has the row "an untrusted or stale vouching lane exits 6". Only t-6 binds relate into LaneLine, and t-6 isn't an ancestor of t-13: t-13's deps are t-9 and t-23, which reach t-21 and t-3. NextRunnable checks only declared deps, so a failed t-6 leaves t-13 runnable, and it would commit a repo-authored spawn whose line no grant covers. Its contract still passes, because a stale `command` exits 6 just as well. t-25 depends on t-6, so nothing records, but t-13 itself isn't fenced. Document order hides this unless t-6 fails.
  Suggestion: add t-6 to t-13's depends_on. It costs no wave, since t-6 is wave 4 and t-13 is wave 5.

- [files] t-16 can't add `test: none (<why>)` without a file it doesn't list. `TestWriterReportBounded` (internal/review/writer_test.go:101) pins the literal "- `test` — exactly `green`, or `red: <first failing test>`". t-16 requires the writer to report a third form, so the honest rewrite of that line goes red in a test outside its files, and a solo writer is fenced to those files. The way to stay green is to leave "exactly `green`, or `red: …`" in place and add `none` after it, which makes the sentence false. The same file's `TestWriterNeverCommitsOrDecides` pin on "Tests only as `dross test --files <path>`" is fine, since t-16 keeps that wording.
  Suggestion: add `internal/review/writer_test.go` to t-16's files, with a row that the pinned line names all three forms.

- [files] t-7 adds a settable `runtime.test_exempt`, but options.md:54, the hand-written `runtime.*` field list behind /dross-options, isn't in its files. The skill's promise to cover every dross-managed setting stops being true, and `TestOptionsCoversEveryProjectSection` works at section level, so it stays green. options.md has room (14,494 of 15,000 bytes).
  Suggestion: add `assets/prompts/options.md` to t-7's files, with a row that line 54 names the field.

## NOTE
- [coverage] All 9 criteria appear in `covers`. c-5 rests on t-18 in substance, with t-19 and t-20 as cost gates.
- [locked-decisions] No locked decision is contradicted. barktime_proof says `vitest related` while the plan uses vitest's related selection through its API and then `vitest run <tests>`; the phase memory records this as the user's choice. t-21 against zero_test_scoped_run is still the closest case: a runner that exits 0 on nothing is held only by t-7's docs. barktime holds, because run-suite.sh has no `--passWithNoTests`, as long as the lane stays on in-container.sh.
- [rules] rules.toml has one rule (r-01), and t-18's `make install` honours it. No global rules file exists. runtime.mode is native, so no task breaks a runtime fence.
- [wave-order] Every depends_on points to an earlier wave and no task can drop a wave. t-21's dependency on t-20 is a deliberate budget gate. t-7 and t-23 share test_lane.go in wave 4, which is harmless because tasks run one at a time.
- [files] Every missing file is created by the task listing it, and t-25's `test_scoped.go` comes from t-13, which it depends on. t-21's file list is right: `Relate` is a not_paths row, so it needs no fields.go edit, unlike t-3's TestExempt.
- [granularity] Ten pending tasks touch 5+ files: t-8 and t-23 (11), t-9 (9), t-7 (7), t-14, t-16, t-21, t-22 and t-25 (6), and t-17 (5). None is worth splitting now. The counts come from test twins and pathfence ledgers, and t-23 is one line at each caller. t-14 dropping to 6 files after t-26 split out resolves the last review's flag. t-22 is still three small unrelated fixes, which is fine.
- [gap] No shared "this repo has a vouching lane" predicate exists, and t-10, t-14 and t-26 all need one. project.go is in none of their files, so each will write its own loop, t-10 and t-26 in the same wave. t-26's "one helper" covers the wording only. The rule is simple today, so the drift risk is low.
- [gap] c-6 says ship refuses, while t-12 keeps `--force-unverified` as a warn-and-proceed bypass an agent can type. This matches gateFreshness and is stated, so it's a choice. verify has no such bypass.
- [gap] `_options.lanes.md:10` says a prepare is "skipped entirely for a lane that will not spawn". t-13 runs prepare before relate, so a lane whose relate lists nothing pays the prepare and spawns no command. t-7 edits that file and has no row for it.
- [gap] t-17 doesn't say what quick's `--files` takes. Quick has no `task.files`, and execute's "never the git diff" doesn't transfer. A short list self-corrects at the commit refusal, at the cost of one more scoped run.
- [gap] t-11 still lists only test files. If the proof finds a planner bug, use `dross task edit` rather than a second task.
- [verified-clean] These suspected gaps checked out as fine:
  - All findings from the last review are closed as named contracts: t-17's vouching wording and mirror row, t-13's grant row, t-8's `--check`, t-24's full flag list and local.toml wording, t-20's SHA baseline, t-18's "unchanged" wording, the `lane list` check, the fenced bare line in t-17, the toolchain doc and comment, the reader-list rows, and t-26's split from t-14.
  - relate's stdout can be captured alone. `spawnLocal` and `spawnRemote` take separate writers, and `remote.Script` merges nothing, so t-13 needs no transport change.
  - `commit.go` is the only reader of green.json, so no other gate blocks a scoped commit.
  - tamper-guard's `/.dross/gate/` prefix already covers scoped.json, so t-14 needs no override.go edit.
  - `test: none (<why>)` needs no parser change. Go holds only the fence name, and the `test` field is read by prompt. Its one Go pin is the test named in the FLAG above.
  - `dross project set --unset runtime.test_exempt` needs no separate work: `unsetDotted` falls through to `writeDotted` with an empty value.
  - The live prompt budget is assets/budget_test.go, which does exist. quick.md's 180 and verify.md's 664 bytes of headroom are right; execute.md has about 2,400.
  - Conservation checks only that manifest rows survive, so new prompt lines need no prompt_instructions.txt edit.
  - No prompt tells an agent to run a granting `dross trust`, and none writes local.toml by hand, so t-24 breaks no flow.
  - `treefp.Base` and `WorkingTree` both strip `.dross/`, which t-10's "compare equal" row depends on.
  - in-container.sh passes its args to one `vitest run` across all projects, so a batch from one project can't go red on another. All 749 test paths join to 38 KB, under Linux's 128 KiB limit for a single argument.
  - Only the project.go anchors are test-pinned. Other ARCHITECTURE.md line anchors are reported by `dross doctor`, so t-21's ARCHITECTURE.md entry is enough.
- [strength] t-20 still sits where it pays most: it tests the planned model in the real stack before t-21 and t-9 build it, and a failure blocks that chain alone.
- [strength] t-14's c-4 contract is string-equal to today's judgeGreen output across no-lane and non-vouching repos. That is the right strength for an unchanged-behaviour criterion.
- [strength] t-24 scopes its claim honestly. It blocks direct routes, says it doesn't stop a deliberate forgery, and pins the over-reach side (`--check` passing) as hard as the refusal side.

## Summary
The relate design holds against the code and all prior findings are closed, so nothing blocks; the flags to settle before t-13 and t-16 are that package.json runs a full-size batch the gate must refuse, `uncovered:` overstates what the gate would refuse, t-13 needs a declared dependency on t-6, and t-16 needs writer_test.go in its files.
