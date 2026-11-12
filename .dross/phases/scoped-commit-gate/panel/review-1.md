# Plan Review — scoped-commit-gate

Reviewed: 2026-11-05
Plan: 18 tasks across 5 waves

## BLOCKING
- [missing-api] No task adds a way to delete scoped.json, but t-13 and t-14 both need one. t-2 specifies only SaveScoped and LoadScoped. t-13 must "remove a prior scoped.json" and t-14's "Writer stop clears scoped.json" must do the same, the way writer.go:156 and test.go:1226 call gatestate.ClearGreen. The store's `remove` is unexported, and internal/gatestate/store.go is in neither task's `files`. In a solo run the writer fence (writerfence.go) refuses an edit outside the task's files, and the main loop may not edit a writer-owned task's code. That leaves a raw os.Remove on gatestate.Path, bypassing the store. It has to be fixed before t-2 lands, because t-2 is the only task that owns the file.
  Suggestion: add ClearScoped to t-2's description, with a contract line: no record is not an error, and green.json is untouched.

## FLAG
- [threat-model] t-6 and t-8 rest on "until a human runs `dross trust`", and t-6's contract is titled "if an agent can widen its own gate". Nothing enforces the human part. gate-off-guard (selfguard.go) covers only `dross gate off/check/record`, no gate claims `dross trust`, and trustLane writes the grant with no TTY check or confirmation. An agent can run `dross test lane edit x --vouch` then `dross trust --lane x` from Bash, and likewise grant its own exempt globs. Only the prompt line "Never run `dross trust` on their behalf" stands in the way. The stale-grant contract stays green the whole time, because it tests staleness and not who clears it.
  Suggestion: either extend the guard so a tool-call `dross trust` that writes a grant is refused while `--check` passes (execute.md and quick.md call it), and put that in t-6 or t-8, or record that the prompt line is the accepted boundary and retitle the contract.

- [test-contract] c-2 says "every changed non-.dross path", but no t-4, t-14 or t-15 contract line says `.dross/` paths are ignored. treefp's Snapshot.Changed includes `.dross/`, and judgeCommit's onlyDross only short-circuits an all-.dross commit. A classifier fed snap.Changed unfiltered returns `.dross/phases/x/plan.toml` as uncovered, so a mixed commit or `git commit -a` with bookkeeping dirt is refused and told to run a full green. t-15's `git add -A` catches this only if its fixture has tracked .dross dirt.
  Suggestion: add a t-14 line: covered src/a.ts plus a staged .dross path admits, and the refusal text never names a .dross path.

- [test-contract] t-16 branches on the wrong signal for repos without a vouching lane. Its contract keeps bare `dross test` as "that branch only", the `uncovered:` branch. t-13 gives non-vouching repos no scoped record, and its contracts only produce `uncovered:` from a vouching unit's miss. A laned, non-vouching repo (feastahead) gets a green `--files` run with no `uncovered:` line, so §1f sends it to a commit that c-4 refuses. The same goes for a vouching repo's package.json, which t-14 refuses even when Covered. t-17 has this contract for lane-less quick repos, and t-16 has no counterpart.
  Suggestion: add a t-16 contract line that §1f and the solo partial keep a bare run for repos with no vouching lane, as t-17 does for quick.

- [antipattern] t-10's TestMain seam is obsolete. The panel adopted it when the check applied to every repo with tests declared (risk.md:112). The check now applies to vouching repos only, and no existing fixture has a vouching lane because the field doesn't exist yet, so the ~15 Verify() test files pass through it unchanged. The default-off seam also disables the real check for the whole test binary. That forces the "wiring test" contracts on t-10 and t-12, and t-15's verify refusal needs to re-arm the seam, which t-15 doesn't state. As written, t-15 could pass or fail on the seam rather than the code.
  Suggestion: drop the seam, hermetic_env_test.go and both wiring-test lines, and confirm with the existing verify and ship tests.

- [test-contract] t-10's refusal doesn't converge on a dirty tree. The green recorder fingerprints WorkingTree (`git add -A`), and the check compares treefp.Base. With an untracked unignored file, a bare `dross test` records a tree that never equals HEAD, and "the refusal names bare `dross test`" sends the agent round forever. judgeGreen already has a stage-or-clean branch for this case ("re-running the suite cannot converge on that"). t-10's dirty-tree line only asserts a refusal, not its wording, and t-12 inherits it.
  Suggestion: when green matches the work tree but not HEAD, the contract should require the differing paths and a commit-or-clean instruction, not a re-run.

- [test-contract] t-12's "at the top of ship" conflicts with its own contract. ship.go auto-finalizes the verdict before the pass check at line 195, which writes verify.toml. The branch check comes after that, and gateFreshness sits at line 233. Placing the new check beside gateFreshness, as risk.md proposed, violates "no .dross bookkeeping is written" on an unfinalized verdict. Placing it above finalize means it runs before the branch check, so it compares another branch's HEAD and reports a false reason to run the suite.
  Suggestion: name the position in t-12. Reading the branch earlier or adding a fixture with an unfinalized verdict would settle it.

- [plan-risk] t-18 doesn't say where the lane runs, and that decides whether c-5 is worth having. barktime's suite runs through scripts/test/in-container.sh: each run does `compose run --build` with its own stack (postgres, mailpit, clamav, SeaweedFS), then tears it down with `--rmi local`. The script's header says its args reach `vitest run <args>`, so `vitest related` needs another route. Vitest's projects include integration, so a host-native `vitest related` can pull in tests that need the stack. A wrapper through the stack pays that bring-up once per unit, and t-9 makes one unit per path. Lane `prepare` runs once but there is no teardown hook. barktime also sets remote_host = helicon. The cost model that motivated the phase (5–8h per phase) is not in any contract, and this only surfaces in wave 5, after t-9 and t-13 have fixed per-path units.
  Suggestion: state the wrapper's execution site in t-18 and add a wall-clock line for a multi-path task commit. Do the check before wave 3 if you can, since it can invalidate t-9.

- [test-contract] t-18 doesn't identify the commit it proves, and its checks have no executor. The /dross-quick that lands the wrapper can't be c-5's evidence: it touches an uncovered wrapper path and needs a full green. The plan doesn't name the separate real task commit, which must touch a .svelte. No code reads barktime-proof.toml and its schema is undefined, so "the record fails" means an agent reading a checklist. `dross gate status` reports the moment it's read, not "at that commit". The override store shows commit-green's last lift expired on 2026-10-26, so run it right after the commit.
  Suggestion: name the commit's source, define the record's fields, and say who checks them at verify (c-5 evidence read by hand is fine, if stated).

- [test-contract] c-9 fails open on `go-package` lanes, and t-9 plans them explicitly. `go test ./x/...` exits 0 on `[no test files]`, and selectorMissCode only reads non-zero exits, so the unit passes and its paths land in Covered with no test run. t-3's rule (non-empty empty_exit, not 1) passes on a code `go test` never returns. A file in a package that has tests also counts as covered, which is a looser reading of "reached at least one collected test". `dir` selectors are unspecified: per-path units would spawn identical lines for two files in one directory, and no contract covers it.
  Suggestion: state in t-7's exit contract that a raw `go test` lane cannot vouch without a wrapper, and add a t-9 line for `dir`.

- [test-contract] The vouch and exempt fences exist only in `dross validate`. Neither laneFence (run time), t-8's trust nor t-14's gate re-checks them. A hand-edited selector-less `vouch = true` lane reaches t-9, which spawns the whole command once per path and marks every path Covered. A `test_exempt = ["**"]` gets granted and honoured. t-3 also names only `**` and `*`, so `**/*`, `*/**` and `**/**` slip past as catch-alls, and t-7 has no contract refusing a catch-all through `project set`.
  Suggestion: add the vouch rule to laneFence in t-9 and refuse catch-alls at trust in t-8, or state that the human grant is the only backstop. Widen t-3's catch-all list either way.

- [test-contract] t-6 lets the human grant vouch blind. trustLane prints only prepare and command, so re-granting after a vouch flip shows the same text as any other grant, with nothing saying the lane may now admit commits. trust.go is in t-8's files but not t-6's, and t-8's "if the human grants blind" covers exempt globs only.
  Suggestion: add a line to t-6 or t-8: `dross trust --lane` states that the lane vouches for commits.

- [granularity] t-8 never names the CLI shape, and its `files` may be incomplete. Existing grants each have a flag (--lane, --run, --replay, --lane-install), pinned by testdata/cli_surface/trust.txt. A new flag fails that golden, and the golden isn't in t-8's files. t-18 needs the exact command for the human to run.
  Suggestion: name the verb in t-8, and list trust.txt if it is a flag.

- [test-contract] t-2 leaves Scoped.Lanes' type undefined, and t-14 depends on it. If it holds names, then run, lane edit, re-trust, commit admits on a green from a command that no longer exists, because the grant is current. t-14's lane-edit line covers only the stale-grant half.
  Suggestion: decide names or LaneLine fingerprints in t-2, and add the re-trust case to t-14.

- [test-contract] t-7's "an options docs check fails" names no test and no file in t-7's `files`. The only test referencing _options.lanes.md is prompt_expand_test.go, which isn't listed.
  Suggestion: name the test, and add it to t-7's files.

- [granularity] Six tasks reach the 5-file threshold: t-5 (7), t-7 (6), t-8 (5), t-9 (5), t-16 (5), t-17 (5). None spans 3+ layers. Only t-7 is a real split candidate, since the lane flags and `project set runtime.test_exempt` share nothing but the docs. The others are one surface plus tests or fixtures.
  Suggestion: split t-7, or leave it if one commit is cheaper. Leave the rest.

## NOTE
- [coverage] All 9 criteria appear in `covers`. c-5 rests on t-18 alone in substance, since t-7 only supplies the CLI. Wave order is clean: every depends_on points to an earlier wave and no task could drop a wave. No locked decision is contradicted. rules.toml has one rule (r-01), and t-18's `make install` honours it. There is no global rules file.
- [cost] t-14 refuses every deletion and rename old side, so any file move on barktime costs a full run. That is the safe reading of c-2, but expect it in the first real phase.
- [fixture] stryker.conf.json's `mutate` lists only `src/**/*.ts`, so Widget.svelte reaches Stryker only through the `--mutate` override. The queued Stryker fix to honour config exclusions would break t-11 unless the conf gains .svelte.
- [c-6] `--force-unverified` in t-12 is a bypass of c-6's "refuse", consistent with the existing stale_override lock. It is fine as a human override, but it's not stated in the spec.
- [ordering] t-1 must land first because the suite is red until it does, but only position enforces that: t-2 to t-5 have no depends_on on it.
- [strength] Keeping t-14 independent of t-13, then making t-15 prove recorder and gate agree with no seeded records, is the right split for a gate this easy to test against itself.
- [strength] c-4 is pinned as string-equal to today's judgeGreen output, including a docs-only refusal with test_exempt declared. Byte-identity contracts run through t-3, t-6, t-9 and t-13, which is how the feastahead non-goal gets enforced.
- [strength] The plan's repo facts check out: quick.md's 180 bytes of headroom (14820 of 15000), the empty_exit rule beside the existing 0 and 255 refusals, `svelte` missing from the ts-project fixture, and the rewrite (not deletion) of TestSvelteIsUnavailableWithoutASpawn under TestNoTestLost.

## Summary
The plan is well-grounded and fully covers the spec, but it needs ClearScoped added to t-2 before it starts, and the unenforced human boundary on `dross trust`, the `uncovered:`-only prompt branch and t-18's unstated run site are the flags most likely to undermine what the phase claims.
