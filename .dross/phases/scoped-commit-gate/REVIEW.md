# Plan Review — scoped-commit-gate

Reviewed: 2026-11-05
Plan: 19 tasks across 5 waves

## BLOCKING
- [missing-file] t-8 cannot store its grant from the files it lists, and no task owns the file it needs. Grants are fields on the `consent.Grants` struct in internal/consent/store.go. The toml tags there are local.toml's keys, and localstore embeds that struct. A new exempt.go can add functions but not a field. The only way around it is a reserved key in `TrustedLaneCommands` or `TrustedRunCommands`, which mixes the exempt grant into the lane and run namespaces. t-8's surface is 46,226 bytes, over the 40,000-byte gate, so a solo run dispatches it to a writer. writerfence then refuses the edit and the task fails, which blocks t-14 and t-18. Pair mode has no fence, so there it is only an out-of-scope edit.
  Suggestion: add internal/consent/store.go to t-8's files. Add internal/localstore/store_test.go too, since store.go says a tag drift loses a grant on reload and that file pins the round trip. Also decide how the not-applicable case renders: testdata/cli_surface/doctor_run.txt has no test_exempt, so printing a row in `dross doctor` changes that golden, and t-8 doesn't list it.

## FLAG
- [plan-risk] t-19's stop condition has no number and its measurement undercounts the cost. "A number that makes a 10-file task unworkable" names no threshold, and nobody is named to read the record. t-18's "must beat the 15-26 min full run" is the only figure, and a 25-minute task commit would pass it. The timing is one raw `vitest related` with no dross involved, so it leaves out tree sync, per-unit transport and `prepare`. barktime's local.toml sets remote_host = helicon, so those costs are real, and `run_site (host or stack)` can't say laptop or helicon. In solo the cost at least doubles: t-16 requires a main-loop `--files` after the writer's own run, because writer stop clears the record.
  Suggestion: state the per-task budget that stops the phase, add the laptop-or-helicon choice to run_site, and project at least two runs per solo task with sync and per-unit overhead included.

- [test-contract] No contract covers a bare run in a lanes-only repo with a vouching lane, and t-13 could turn that into a false full green. fullLanePlan sets only `Line`, and runPlannedLanes serves both that bare run and `--files`. If t-13 loops over units and fullLanePlan leaves them empty, nothing spawns, misses stays 0, the run returns nil and a full green is recorded with nothing measured. c-6 would then pass on that record. The opposite failure is also open: a `related`-style command run with no path can exit its empty_exit, which gives exit 5, so no full green is ever possible and verify and ship refuse forever. t-15 misses both because its fixture declares test_command. barktime is safe for the same reason, but the fence allows this shape.
  Suggestion: add a t-13 line that in a lanes-only repo a bare run spawns a vouching lane's whole command once, and that a run which spawned nothing records no green. Say whether a vouching lane can serve as a repo's only full run.

- [test-contract] t-4 never names the "fixed list of build and dependency config" that the locked uncovered_paths decision depends on. Its contracts pin only package.json and vite.config. Anything left off the list can be admitted through an exempt glob, which is the false green the decision exists to stop. barktime has package-lock.json, tsconfig.json, svelte.config.js, vitest.mutation.config.ts, drizzle.config.ts, Dockerfile.test and docker-compose.test.yml.
  Suggestion: enumerate the list in t-4, and add a contract for the lockfile and svelte.config.js, including a case where an exempt `*.json` glob doesn't admit package-lock.json.

- [threat-model] t-6 closes `dross trust` but leaves the grant file open, so "human-only" still overclaims. Grants are sha256 values in .dross/local.toml. tamper-guard covers only .dross/gate/ and the override store, and no gate names local.toml, so an agent's Edit or `sed -i` can write a matching `trusted_lane_commands` hash. tamper-guard's own comment says it protects "the records the gates trust". t-14 makes local.toml one of those records for the first time.
  Suggestion: extend tamper-guard to .dross/local.toml in t-6, or record that the guard stops accidents and not a deliberate forgery, and retitle the contract to match.

- [test-contract] t-6's guard contracts miss two shapes. options.md instructs `dross trust --lane <name> --check`, but the contract only passes bare `dross trust --check`, so a guard keyed on that exact shape breaks /dross-options. guardedGateVerb also has a second arm for unparseable lines (`s.Partial`, rawTokens). Nothing requires the trust check to be added there, so a line that doesn't parse could still run a grant.
  Suggestion: add a line for `--lane x --check` passing, and one for a partial-parse `dross trust` being refused.

- [test-contract] t-13 doesn't say what a non-vouching lane's red does to the record. testlane.Select gives a path to every lane it matches, so one path can be in a vouching lane and a non-vouching lane. "One red unit records nothing" reads as a vouching unit, and the non-vouching contract covers only the green case. An implementation that records per unit would write that path to Covered from a run that exited 1, and the commit is admitted despite the red.
  Suggestion: add a line that any red lane in the run records nothing and clears the record, whichever lane it came from.

- [test-contract] Two refusal remedies send an agent to the wrong fix on barktime, and no task owns either. soloreview.go lines 77 and 81 say "re-run `dross test`", and t-16 and t-17 rewrite the matching prompt lines but not the gate text. A solo `pass-stale` or blocked refusal therefore suggests a bare 15-26 minute run, which is what c-5 removes. No test pins those strings. Separately, t-14's stale-exempt refusal has no contract on its remedy. The fix is a human running `dross trust --test-exempt`, and a generic "run bare `dross test`" hides that behind a full run.
  Suggestion: add soloreview.go to t-14's files with vouching-aware text, and require the stale-grant refusal to name the human grant.

- [antipattern] t-7's obvious docs edit breaks the conservation test, and the file that would repair it isn't listed. `_options.lanes.md` line 4 holds the `dross test lane edit <name> --match ...` span as one row in assets/testdata/prompt_instructions.txt, and rows match whole spans. Adding `| --vouch` to that span drops the row. The record mode never rewrites existing rows and t-7 doesn't list the manifest. The surface is 157,931 bytes, so a solo run hands this to a writer, and the fence blocks the manifest edit too.
  Suggestion: tell t-7 to document `--vouch` in its own span and leave the existing one byte-identical, or list the manifest.

- [granularity] Six tasks reach the 5-file threshold: t-5 (7), t-7 (7), t-8 (6, 8 with the store fix), t-9 (5), t-16 (5), t-17 (5). None spans 3+ layers. t-7 is the only real split candidate, since the lane flags and `project set runtime.test_exempt` share only the docs. The rest are one surface plus its tests or fixtures.
  Suggestion: split t-7, or leave it if one commit is cheaper. Leave the others.

## NOTE
- [coverage] All 9 criteria appear in `covers`. c-5 rests on t-18 in substance; t-7 and t-19 only support it. t-19's c-9 entry is a stretch, since a timing record proves nothing about attribution.
- [wave-order] Every depends_on points to an earlier wave, and no task could drop a wave. t-16 and t-17 look like they could, but they need t-13's `uncovered:` output and the refusal text to describe them accurately. t-5 and t-11 share construct_e2e_test.go in different waves, so there's no collision.
- [locked-decisions] No locked decision is contradicted. t-8's human grant on test_exempt is stricter than uncovered_paths requires, which is compatible.
- [rules] rules.toml has one rule (r-01), and t-18's `make install` honours it. No global rules file exists. barktime's rules don't forbid t-19's raw vitest run.
- [exempt-globs] t-4's `*` doesn't cross segments, so the spec's `*.md` example exempts root markdown only. A nested README needs `**/*.md`, which t-3's fence must keep allowing.
- [size] t-14 carries 17 contract lines and t-13 carries 12. They are coherent, but each is a large unit for one pair-mode approval.
- [c-6] `--force-unverified` in t-12 bypasses c-6's "refuse". It matches gateFreshness and is stated in the plan, but the spec doesn't mention it.
- [ordering] t-1 goes first by position only: nothing in wave 1 depends on it.
- [verified-clean] These suspected gaps checked out as fine. gate importing localstore and consent makes no cycle, and boundary_test doesn't forbid it. LoadGreen has no reader beyond the commit gate and the recorder. tamper-guard already covers scoped.json through its .dross/gate/ prefix. treefp.Base and WorkingTree both drop .dross/. t-11's checks hold against the real Stryker svelte-parser: it collects `{count + 1}`, estree-walker is already hoisted in the fixture, and checkInstrumented tolerates a zero-mutant file. verify.md's §1 fence sits ahead of the detached partial, so a bare run placed there also covers `--detach`. t-18 and t-19 stay inline under solo dispatch (surface 0), so the writer fence doesn't reach them.
- [strength] The previous review's findings are closed in the plan, and not just acknowledged: ClearScoped, the trust guard, the `.dross/` contract, the `uncovered:`-only branch, the seam removal, t-12's position, `go-package` and `dir` fencing, and LaneLine fingerprints all appear as named contracts.
- [strength] Moving the barktime timing into wave 1 as t-19, with t-9 depending on it, tests the riskiest assumption before per-path units are built instead of finding out in wave 5.
- [strength] t-15 proves recorder and gate agree with no seeded records, and c-4 is pinned string-equal to today's judgeGreen output. That is the right defence for a gate this easy to test against itself.

## Summary
The plan is well-grounded and covers the spec, but t-8 needs internal/consent/store.go before it can run in solo, and the lanes-only false green, the unnamed config list and t-19's missing stop threshold are the gaps most likely to undermine what the phase claims.
