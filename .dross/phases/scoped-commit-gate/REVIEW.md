# Plan Review — scoped-commit-gate

Reviewed: 2026-11-06
Plan: 25 tasks across 8 waves

## BLOCKING
- [locked-decisions] t-17 keeps the bare route for the wrong kind of repo, which breaks scoped_gate_optin's "feastahead stays unchanged". Its description keeps "the bare branch for lane-less repos", and its contract says c-4 "would refuse" a lane-less repo without it. That is backwards. In a lane-less repo `--files` is the whole suite and records a green (test.go: "in a lane-less repo --files changes nothing about the line"; `startGreen(repoDir, len(args) == 0)`). The repo that needs bare is one with lanes and no vouch: `runTestLanes` records nothing there, and feastahead is that repo (`web` and `phone` lanes, no vouch). Built as written, quick §4 sends feastahead to `--files`, and its commit is refused. The t-17 row "`_quick.solo.md` demanding a bare run after a fix round" fails the prompt that would fix this. The gate stays safe, so the damage is a refused commit, but quick.md runs bare for feastahead today and this changes that. t-16 already words it correctly ("repos with no vouching lane").
  Suggestion: change t-17's description and its lane-less contract to "a repo with no vouching lane". Limit the solo row to a vouching repo, and add the mirror row t-16 has: `_quick.solo.md` without a bare run for a repo with no vouching lane fails.

## FLAG
- [test-contract] t-13 has no contract that relate spawns only behind the lane grant. relate is a new repo-authored spawn, and on the lane path the per-lane grant replaces requireExecConsent (test.go's comment above `runTestLanes`), so the grant is its only gate. `runPlannedLanes` resolves consent before any spawn, so the natural insertion point is safe, but nothing pins that. TestEverySpawnSiteGatedOrExempt checks reachability from gated commands, not order, so a relate spawned ahead of the check would pass it. t-6 proves a relate edit stales the grant, not that a stale grant stops relate.
  Suggestion: add a t-13 contract that an untrusted or stale vouching lane exits 6, and the recorder shows zero relate, prepare and lane spawns.

- [test-contract] t-8 never says what `dross trust --test-exempt --check` does, and t-24 lets every `--check` through. Each existing arm in trust.go passes `check` down, and trustLaneInstall returns before any write. t-8's eight contracts don't mention `--check`. If the new arm skips it, a tool-call `dross trust --test-exempt --check` passes t-24's guard and writes the grant, which is the route t-24 exists to close. t-24's "needs no knowledge of t-8's flag" holds only if t-8 gets this right.
  Suggestion: add a t-8 contract that `--test-exempt --check` exits 0 on a current grant, non-zero on a stale one, and writes nothing to local.toml.

- [test-contract] t-24's contract names three granting forms, and the description claims every one. `--lane-install`, `--run` and `--replay` also write grants (trust.go), and no row names them, though `--lane-install` is the one whose comment calls it a machine-changing grant. `--check=false` is a bool flag with a value, so cobra parses it as a grant, while a guard written as "has a `--check` token" or a prefix match passes it. Separately, tamper-guard's reason and remedy are written for gate records ("forge a green… `dross test` records a green"), which is wrong advice for local.toml, and no row covers that.
  Suggestion: add `--lane-install x`, `--run x`, `--replay x` and `--check=false` to the refused row. Add a row that the local.toml refusal names `dross trust` and `dross local set`, not `dross test`.

- [test-contract] t-20 fails its record on exact counts that have no baseline. "relate on helicon must give t-19's counts (Shopfront 5, StatusBadge 112)", but barktime-timing.toml records no barktime SHA, and barktime has already moved since t-19 (three commits at 07:44, after the record's 07:24). Those three changed only ARCHITECTURE.md and .dross, but barktime is under active phase work and t-20 waits on a quiet host. Any added test importing StatusBadge fails the record for a reason unrelated to the attribution model.
  Suggestion: have t-20 record the barktime SHA and compare against a `vitest related` count taken on the laptop at that SHA, or state a tolerance. Treat a count-of-zero mismatch as the real failure.

- [test-contract] t-18's "no scoped.json" can fail on a correct build. Its relate-fails-open check requires exit 5, no lane spawn and no scoped.json. t-25 says a nothing-related run "records nothing" and removes a record only on a red, and no task clears the record on commit. Run after the evidence commit, the check still finds that commit's scoped.json, and tamper-guard stops anyone clearing it by hand.
  Suggestion: reword to "scoped.json is unchanged, and no record exists for this tree", or require the check to run before the first green.

- [test-contract] t-16 and t-17 branch every prompt on "a repo with a vouching lane", and nothing tells the agent which repo it is in. A green `--files` run gives no signal either way: t-13 and t-25 define `uncovered:` but no line saying a scoped green was recorded. Only `dross test lane list` shows vouch, and no contract has a prompt call it. A wrong guess in a vouching repo costs the 15-26 min bare run that c-7 exists to remove, and one in a non-vouching repo costs a refused commit.
  Suggestion: add a t-25 contract for a stderr or transcript line when a scoped green is recorded, and have the prompts branch on it. Otherwise name `dross test lane list` as the check in §1f and quick §4.

- [wave-order] t-17 can depend on an audit change that only t-16 owns. `codeAdds` counts only a fenced line exactly equal to `dross test`, and it pins quick.md's `## 5.`. t-16's contracts ("if §1f loses both runs", "a fenced bare run as §1f's unconditional line fails") require the audit to learn `--files`, and only t-16 lists prompt_commit_gate_test.go. t-17 requires a bare "branch", not a fence, and with 180 bytes of headroom that branch will want to be inline prose. Inline bare plus a fenced `--files` stays red until t-16's audit lands. Document order puts t-16 first, so this bites only if t-16 fails or moves, but the two have no dependency. The last review's clean note on this audit predates t-16's rewrite.
  Suggestion: pin t-17's bare branch as a fenced line, which costs no wave. Otherwise add `depends_on = ["t-16"]` to t-17, which pushes t-17 to wave 8 and t-18 to wave 9.

- [test-contract] t-23 leaves a documented rule false. `_options.lanes.md:16` says the toolchain "is derived from the first token of its `command` and `prepare` lines", and the comment at project.go:177 says the same. t-23 lists neither, and t-7, which does edit that doc in the same wave, has no row for it.
  Suggestion: add a t-7 options_docs_test row that line 16 names relate, and fix the project.go comment in t-23.

- [test-contract] The pathfence rows in t-14 and t-25 name no failing test: "the pathfence entry's reader list is stale". `gatestate.Scoped.Covered` is a reader-list entry, and the only reader-text assertion in fields_test.go is on a different field. A forgotten update stays green.
  Suggestion: assert the new reader in fields_test.go, or state this as a checklist item and drop the "fails" framing.

- [granularity] t-14 is the one pending task that should split: 10 files and 20 contract rows. The commit-green admit path and the writer clear are one change. The soloreview and `dross review status` wording helper is another: it needs no classifier, no grants and no t-6, and it adds the only cmd-layer files. Most other 5+ file tasks (t-8 at 11, t-23 at 10, t-9 at 9) are inflated by test twins and pathfence ledgers and read as one change each.
  Suggestion: move the wording helper, review.go and the two note contracts into their own task, which can run earlier.

## NOTE
- [coverage] All 9 criteria appear in `covers`. c-5 rests on t-18 in substance, with t-19 and t-20 as cost gates.
- [locked-decisions] Apart from the BLOCKING finding, no locked decision is contradicted. t-21 against zero_test_scoped_run is the closest: with empty_exit optional, a runner that exits 0 on nothing is held only by t-7's docs. barktime holds, because run-suite.sh deliberately omits `--passWithNoTests` and `vitest run <paths>` goes through it. Its package.json scripts do carry that flag, so the lane must stay on in-container.sh, as t-18 says.
- [rules] rules.toml has one rule (r-01), and t-18's `make install` honours it. No global rules file exists. runtime.mode is native, so no task breaks a runtime fence.
- [wave-order] Every depends_on points to an earlier wave and no task can drop a wave. t-21's dependency on t-20 is a budget gate, not a data need, and that is deliberate. NextRunnable needs every dep `done`, so a failed t-20 does block t-21 onward. t-7 and t-23 share test_lane.go in wave 4, which is harmless because tasks run one at a time.
- [files] Every missing file is created by the task listing it. t-23's list matches every non-test `testlane.Toolchain` caller. Stated headroom is right: quick.md 14,820 and verify.md 14,336 of 15,000 bytes. quick.md stays the tight one, and it has no test-not-run partial to move text into.
- [gap] writerfence.go's takeover check treats only a bare `dross test` as a gate that certifies the tree. Once t-16 makes `--files` the main loop's certifying run, a `--files` during a launched writer is admitted. t-14's clear on writer stop means this wastes a run and admits nothing, so writerfence.go needs no change, but its header comment goes stale.
- [gap] t-11 lists only test files. If the proof finds a planner or adapter bug, there is no production file for the fix to land in, and a solo writer is fenced to the task's files. Use `dross task edit` then rather than a second task.
- [granularity] t-22 is three unrelated fixes under one title. They are small and share only their origin, so it is fine as is.
- [verified-clean] These suspected gaps checked out as fine:
  - The last review's flags are closed as named contracts: taint design in t-9 and t-13, `review.go` in t-14, the solo routes in t-16, relate's dependencies in t-20, runs in t-18, ShArgvFor in t-9, and the guards split out as t-24.
  - `verify results` is no bypass of t-10: it only collects a run that `--detach` dispatched, and `--detach` is gated first.
  - `dross local set` can't write a grant: localstore.Keys leaves the trusted_* keys out, so tamper-guard on local.toml closes the file route.
  - cli_surface goldens pin only issue, trust, doctor and verify, so t-7 needs no golden and t-8's trust.txt is the right one.
  - TestExecutePromptChecksConsent matches on "```\ndross test", which a fenced `--files` still satisfies.
  - The conservation rows for `dross test` survive t-16 and t-17, as long as both keep the bare form somewhere in the prompt.
  - gate can import localstore and consent with no cycle.
- [strength] t-20 sits where it pays most: it tests the planned model in the real stack before t-21 and t-9 build it, while t-22, t-10, t-11, t-8 and t-12 stay runnable if it fails.
- [strength] t-12 gets ship's real ordering right. Auto-finalize runs before the branch check in ship.go, so reading the branch first and refusing ahead of finalize is the only order that writes nothing on a refusal.
- [strength] t-14's c-4 contract is string-equal to today's judgeGreen output, across both no-lanes and non-vouching repos. That is the right strength for an unchanged-behaviour criterion, and it is the standard t-17 should meet.

## Summary
The relate design holds against the code and most prior flags are closed, but t-17 must stop treating lane-less as the bare-run case before it reaches feastahead, and the t-8/t-24 `--check` seam needs a contract before t-24 starts.
