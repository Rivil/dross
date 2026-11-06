# Plan Review — scoped-commit-gate

Reviewed: 2026-11-06
Plan: 23 tasks across 7 waves

## BLOCKING
(none)

## FLAG
- [test-contract] t-9 and t-13 ignore the exec-output taint gate, and t-9's refusal wording runs straight into it. The word "taint" appears nowhere in the plan. relate is the first `dross test` stream dross has to parse rather than just show. t-13 must capture that stream, and the engine follows writers: channels_writer.go.txt covers a builder and io.MultiWriter, and its fixture marks `errors.New(b.String())` as `WANT taint`. sinks.go.txt lists an error and a file as escapes and lets only an int or a bool through. That gives TestNoSpawnOutputEscapes three likely findings:
  - t-9 refuses the output "naming the line", which puts raw stream text in an error.
  - Scoped.Covered reaches scoped.json if it is built from relate's strings.
  - The named test paths feed the batched line.

  taint_marker_test.go forbids a marker on the stream or its writer, so the only legal spot is the conversion line. A marker there would also clear an error that echoes a malformed line, which is the one value that failed the shape check. I did not run this, since the code doesn't exist yet. If t-13 reaches its gate without a design for this, it goes red in the full suite, and any change to relate.go falls outside t-13's files.
  Suggestion: settle this in t-9. The refusal names a line number, not the line's text. Covered is taken from the asked paths, not relate's output. One `//dross:taint-cleared` marker sits on the parse call in test_scoped.go, with prose naming the shape check. Add a contract line that TestNoSpawnOutputEscapes stays green with no marker on the stream.

- [test-contract] The solo fix-round and pass-stale routes keep sending a vouching repo to a full run, and c-7 names solo. `_execute.solo.md:42` says "re-run `dross test` (bare)" after a blocked review, and line 43 says the same for pass-stale. t-16's description covers "the solo partial", but no t-16 contract mentions either route, while t-17 has this exact contract for `_quick.solo.md`. Separately, t-14 fixes the wording in gate/soloreview.go, but internal/cmd/review.go:135 and :140 print the same "re-run `dross test`" notes from `dross review status`, and that file is in no task. After t-14 the refusal says `--files` while the status line, which the prompts tell the agent to re-read, still reads as the bare run. On barktime each blocked round then costs a 1130 s suite. Also note that TestExecuteReviewFailurePath (execute_prompt_test.go:439) and quick_prompt_test.go:115 pin the literal "re-run `dross test`", which a `--files` rewording breaks. Both files are already owned by t-16 and t-17.
  Suggestion: add a t-16 contract mirroring t-17's fix-round line, and add internal/cmd/review.go to t-14 with a vouching and a non-vouching case for both notes.

- [plan-risk] t-20 and t-18 say nothing about how relate gets its dependencies on helicon's host. relate runs there by `vitest`'s own selection, but `syncTreeTo` builds its exclude list from what git ignores, and barktime ignores node_modules/, .svelte-kit/, src/lib/paraglide/ and .env. None of them reach helicon, and rsync's --delete leaves excluded paths alone, so any copy already there survives stale. t-19 measured the laptop only, so nothing has exercised this site. t-23 doesn't help: it probes relate's first token, so a host with `node` and no node_modules passes the probe and relate then exits 7. t-20's cost contract counts relate and the batched run but no bootstrap, and t-18 declares no `prepare`. If the fix is a `prepare` running `npm ci`, that cost lands on every run and first shows up in t-18, the last task in the phase. I did not check helicon's workdir.
  Suggestion: add a t-20 contract that the record states how relate's dependencies reach the site and what that costs per run, counted in the two-run projection. t-18 then declares that same `prepare`, or says why it needs none.

- [test-contract] t-18 and t-20 use different units for the same 600 s budget. t-20 fails a leaf task that projects over 600 s "at two runs", because solo re-runs after the writer stops. t-18 passes when "wall_clock_s for a multi-path leaf task commit" fits 600 s, without saying how many runs that is. A single 400 s run passes t-18 while the solo flow it stands for costs 800 s, which is the model t-20 exists to catch.
  Suggestion: have t-18 record runs alongside wall_clock_s, and compare the two-run figure against the budget.

- [test-contract] t-9 doesn't send relate through the `ShArgvFor` shell-line fence. laneFence runs command and prepare through it, with a comment saying a line reaching a shell gets the same fence. t-9's contracts cover selector, dir, go-package, relate-less and no-test_command lanes, and never relate's own line. A leading-dash relate then skips the up-front refusal, the one that fires before any lane spawns. t-6 handles NUL at LaneLine, which is a different check.
  Suggestion: add a laneFence contract that a leading-dash relate is refused naming the lane's field, and spawns nothing.

- [granularity] t-6 and t-13 are the two tasks that should be looked at for splitting. Most other 5+ file tasks (t-8 at 11, t-23 at 10, t-9 at 9) are inflated by test twins and pathfence ledgers, and read as one change each. t-6 is not: it carries three mechanisms across consent, gate and cmd, namely the LaneLine frame, the tool-call `dross trust` refusal and the local.toml tamper guard. The last two apply to every repo and have nothing to do with relate, so they could land without waiting on t-21. t-13 has grown from 20 contract lines to 23, the most in the plan, for one pair-mode approval. t-14 has 19.
  Suggestion: move t-6's two guards into their own task with no t-21 dependency. Consider splitting t-13 at the seam between running relate and the batch, and recording the result.

## NOTE
- [coverage] All 9 criteria appear in `covers`. c-5 rests on t-18 in substance.
- [locked-decisions] No locked decision is contradicted. Closest is t-21 against zero_test_scoped_run: making empty_exit optional removes the only declared signal for an empty run, and t-7's docs replace it with a promise that the runner won't exit 0 on nothing. That loses little, because `selectorMissCode` only ever saw non-zero exits, so an exit-0 runner was never detectable. barktime holds by construction: run-suite.sh omits `--passWithNoTests`. The lock is enforced by the human granting the lane, not by dross.
- [locked-decisions] t-6's guards reach every repo, while scoped_gate_optin says feastahead stays unchanged. This looks harmless: no prompt tells an agent to run a granting `dross trust` or to edit local.toml, and a test already forbids the bare instruction.
- [rules] rules.toml has one rule (r-01), and t-18's `make install` honours it. No global rules file exists.
- [wave-order] Every depends_on points to an earlier wave and no task can drop a wave. Shared files within a wave (test_lane.go, pathfence/fields.go) don't collide because tasks run one at a time. t-13 uses LaneLine but doesn't depend on t-6, so it only gets the new frame through wave order.
- [files] Every missing file is created by the task that lists it. The t-23 file list matches every non-test `testlane.Toolchain` caller.
- [budget] Stated headroom is right: quick.md is 14,820 and verify.md 14,336 of 15,000 bytes. quick.md is the tight one, since t-17 adds a `--files` fence and two routes and must still net-trim.
- [verified-clean] These suspected gaps checked out as fine:
  - t-20's claim holds: NextRunnable needs every dep `done`, so a failed t-20 blocks t-21 and everything on it in pair and solo.
  - `dross test <tests>` is accepted, appended by testCommandLine, and records no green, so t-20's leg can't pollute barktime's gate state.
  - commit-green is the only gating reader of LoadGreen, so a scoped commit meets no second full-green check.
  - gate can import consent, localstore and testlane with no cycle.
  - tamper-guard already covers `.dross/gate/`, so t-14's scoped.json contract needs no override.go change.
  - No code parses the writer report, so t-16's `test: none` is a prompt-only change.
  - TestPromptCommitsSatisfyGreenGate only needs a fenced bare `dross test`, which t-16 and t-17 keep.
- [strength] The last review's flags are closed as named contracts, and they hold up against the code: c-9 now says what the design proves, `--vouch=false` clears relate, t-21 settles empty_exit, exit 5 is routed in both prompt tasks, and t-23 picks up the toolchain gap.
- [strength] t-12 gets ship's real ordering right. Auto-finalize runs before the branch check in ship.go, so reading the branch first and refusing ahead of finalize is the only order that writes nothing on a refusal.
- [strength] t-20 sits where it does the most good: it tests the planned model in the real stack before t-21 and t-9 build it, while t-22, t-10, t-11, t-8 and t-12 stay runnable if it fails.

## Summary
No blocking issues and the relate design holds against the code, but settle the taint design in t-9 before t-13 and record how relate's dependencies reach helicon in t-20, since those two would otherwise surface latest and cost the most.
