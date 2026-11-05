# Planner draft — verification lens (test contracts first)

Each task below was derived backward: the contract was written first, then the
smallest change that makes it satisfiable. Contracts name test functions where the
name is decided; new test files are listed under `files`.

```
Phase scoped-commit-gate — 14 tasks across 4 waves

Wave 1
  t-1  Sync README roadmap with v1.8 and v1.9
       files:    README.md, internal/cmd/readme_roadmap_test.go
       covers:   c-1
       desc:     Mark v1.8 (complete) with every box ticked, rewrite v1.9 as (active) with its
                 5 criteria, drop the Azure DevOps line. Add one test pinning the section bodies.
       contract: TestReadmeRoadmapMatchesRecordedMilestones goes from red to green.
                 New TestReadmeRoadmapSectionBodies fails if the v1.8 section holds any `- [ ]`,
                 if the v1.9 section's checkbox count differs from len(scope.success_criteria)
                 in v1.9.toml, or if "Azure DevOps" appears under the v1.9 heading. It also fails
                 on the pre-edit README, which has one unticked placeholder plus the Azure line.

  t-2  Add lane vouch_probe field and its fences
       files:    internal/project/project.go, internal/consent/lane.go, internal/consent/lane_test.go,
                 internal/testlane/toolchain.go, internal/testlane/toolchain_test.go,
                 internal/cmd/validate.go, internal/cmd/validate_lane_test.go
       covers:   c-2, c-9
       desc:     TestLane.VouchProbe (`vouch_probe`): a line run once per path, path appended
                 shell-quoted. Declaring it is the opt-in. Exit 0 = path reaches a test, an
                 empty_exit code = it reaches none. Fold it into LaneLine under a new frame and
                 into the derived toolchain.
       contract: Adding vouch_probe to a granted lane makes LaneConsented report stale.
                 A lane with no vouch_probe hashes byte-identically to today, so no grant goes stale.
                 validate refuses vouch_probe with no selector, and with no empty_exit, each with
                 its own message. A NUL in the probe makes LaneLine return "".
                 Toolchain includes the probe's first token unless Toolchain overrides it.

  t-3  Add runtime.test_exempt with a config fence
       files:    internal/project/project.go, internal/cmd/project.go, internal/testlane/match.go,
                 internal/testlane/match_test.go, internal/cmd/validate.go
       covers:   c-3
       desc:     Runtime.TestExempt globs, get/set through `dross project`. Export
                 testlane.MatchAny over the existing glob dialect. validate refuses an exempt
                 glob that matches a closed list of build and dependency files.
       contract: validate refuses `**`, `*.json` and `package.json`, naming the matched file.
                 validate accepts `docs/**` and `*.md`.
                 MatchAny(["docs/**"], "docs/a/b.md") is true and MatchAny(["*.md"], "docs/a.md")
                 is false, pinning that `*` stays within one segment.
                 `dross project set runtime.test_exempt docs/**,*.md` round-trips through get.

  t-4  Add scoped.json gate record
       files:    internal/gatestate/store.go, internal/gatestate/store_test.go,
                 internal/secretscan/writers.go, internal/gate/writer.go, internal/gate/writer_test.go
       covers:   c-2, c-6
       desc:     gatestate.Scoped{Tree, Covered, At, Runner} in its own file, with Load/Save/Clear.
                 Never a field on Green, so no reader of green.json can see it. Writer stop clears
                 it alongside green.json. Add it to the machine-local writers inventory.
       contract: SaveScoped refuses an empty tree and an empty Covered list.
                 LoadScoped returns nil with no file, an error naming the path on bad JSON, and
                 refuses a tracked record unread.
                 SaveScoped leaves LoadGreen nil.
                 TestWriterStopClearsGreen's stop also removes scoped.json; a stop from an unbound
                 agent leaves it in place. Without this a writer's own --files green would admit
                 the main loop's commit.
                 tamper-guard refuses a Write to .dross/gate/scoped.json.

  t-5  Resolve .svelte constructs for mutation ranges
       files:    internal/mutation/construct.go, internal/mutation/astspan.go,
                 internal/mutation/construct_test.go, internal/mutation/construct_e2e_test.go,
                 internal/mutation/testdata/ts-project/package.json,
                 internal/mutation/testdata/ts-project/package-lock.json,
                 internal/mutation/testdata/ts-project/src/Counter.svelte
       covers:   c-8
       desc:     Remove the .svelte refusal. astspan blanks each <script> body in place, parses
                 the rest with svelte/compiler for top-level markup spans, and babel-parses each
                 script with startLine so its top-level statements keep file line numbers.
                 Constructs stay sorted and disjoint.
       contract: TestSvelteIsUnavailableWithoutASpawn inverts: a project with no svelte/compiler
                 gives ErrASTUnavailable "cannot resolve svelte/compiler", with node spawned once.
                 Real-parser test: Counter.svelte (lang="ts", a module script, an instance script
                 with two functions, a multi-line `{#if}` block) returns each function at its true
                 file line and the `{#if}` as one construct. <style> yields no construct.
                 Real Stryker test: a hunk inside one function gives mutants in that function and
                 none in the other; a hunk inside the `{#if}` expression generates its mutant,
                 which a raw-hunk range would have dropped.
                 PlanRanges on that file records no WholeFileASTUnavailable.

Wave 2 (t-6 depends t-2; t-7 depends t-2, t-4; t-8 depends t-2, t-3, t-4; t-9 depends t-4)
  t-6  Expose vouch_probe in lane CLI
       files:    internal/cmd/test_lane.go, internal/cmd/test_lane_edit_test.go,
                 internal/cmd/test_lane_list_fields_test.go, assets/prompts/_options.lanes.md
       covers:   c-5
       desc:     `--vouch-probe` on lane add and edit ("" clears), printed by lane list, documented
                 with the exit contract. This is how barktime opts in without hand-editing.
       contract: `lane add --vouch-probe x` with no --empty-exit is refused and project.toml is
                 byte-unchanged.
                 `lane edit --vouch-probe` keeps the grant and stales it, so `dross trust --lane`
                 reports CHANGED rather than ABSENT.
                 `lane list` prints vouch_probe only for a lane that declares it.

  t-7  Record scoped greens from probed lane runs
       files:    internal/cmd/test.go, internal/cmd/test_scoped.go, internal/cmd/test_scoped_test.go,
                 internal/cmd/test_green_test.go
       covers:   c-2, c-9
       desc:     After a lane's prepare and before its command, spawn the probe once per live path
                 on the lane's target. Skip the command when no path reached a test. On an overall
                 green, save Covered = probed paths of opted-in lanes that actually measured, and
                 print `uncovered: <paths>`. Reuse greenRecorder's before/after tree check.
       contract: Paths a (probe 0) and b (probe empty_exit), lane green: scoped.json Covered is [a],
                 the transcript has `uncovered: b`, exit 0.
                 Every probe empty: the lane command is never spawned, exit 5, no scoped.json, and
                 no second full-suite spawn (zero_test_scoped_run).
                 Probes 0 but the lane returns its empty_exit: its paths are not covered and nothing
                 is recorded.
                 Probe exits something else: that path is uncovered and the line names it, so an
                 unknown status can never read as covered.
                 Remote probe with transport failure: run exits 3 and records nothing.
                 Remote ordering, via the spawn seams: sync, prepare, probes, command.
                 Red lane in the run: no record, and an existing record for this tree is cleared.
                 Non-opted lane green beside an opted one: its paths stay out of Covered.
                 Deleted path (Dropped) stays out of Covered.
                 Tree moves during the run: no record, one line naming the paths.
                 Two runs on one tree union Covered; a run on a new tree replaces it.
                 No scoped run ever writes green.json (TestPartialRunsRecordNothing still holds).
                 A lane with no probe spawns exactly today's lines.

  t-8  Teach commit-green to admit scoped greens
       files:    internal/gate/commit.go, internal/gate/commit_scoped.go,
                 internal/gate/commit_scoped_test.go
       covers:   c-2, c-3, c-4, c-9
       desc:     When any lane declares vouch_probe and the full green does not match: admit if
                 every changed non-.dross path is exempt, or the scoped record is on this exact
                 tree and every non-exempt path is in Covered and still matches an opted-in lane.
                 Otherwise refuse naming the uncovered paths. With no opted-in lane, nothing changes.
       contract: Seeded scoped.json on the candidate tree, all paths covered, no green.json: admitted.
                 Same with one extra changed path b.ts: refused, reason contains b.ts and not a.ts.
                 Covered [a] and changed [a, b] from one run (c-9): refused naming b.
                 File edited after the run, so the tree differs: refused naming that path.
                 package.json changed with no glob matching it: refused naming package.json.
                 Only docs/a.md changed, exempt, no scoped.json at all: admitted with no run.
                 docs/a.md plus covered src/a.ts: admitted. .dross/ paths never count as uncovered.
                 Full green on the candidate tree still admits, whatever scoped.json says.
                 c-4 table: no lanes, and lanes with none opted in, each with a matching scoped.json
                 and a declared test_exempt: refused, and the refusal text is string-equal to
                 today's judgeGreen output. A docs-only commit is refused there too.
                 Covered path that no opted-in lane matches any more: refused.

  t-9  Refuse verify without a full green
       files:    internal/treefp/treefp.go, internal/treefp/treefp_test.go,
                 internal/cmd/full_green.go, internal/cmd/full_green_test.go, internal/cmd/verify.go
       covers:   c-6
       desc:     treefp.Head fingerprints HEAD with .dross/ out. requireFullGreen compares it with
                 LoadGreen only, never LoadScoped. Called in `dross verify` before any leg, on the
                 attached, --detach and --skip-mutation paths. Silent where testsDeclared is false.
       contract: treefp.Head equals WorkingTree on a clean tree with .dross/ tracked, and stays put
                 when the work tree is dirtied.
                 scoped.json on the HEAD tree with no green.json: verify refuses, no tests.json is
                 written, and no leg or detached dispatch ran.
                 green.json on a dirty tree (not HEAD): refused.
                 green.json on the HEAD tree: passes.
                 A .dross-only commit after the green still passes.
                 A repo with no test_command and no lanes is not refused.

Wave 3 (t-10 depends t-9; t-11 depends t-7, t-8; t-12 depends t-7, t-8, t-9; t-13 depends t-6, t-7, t-8)
  t-10 Refuse ship without a full green
       files:    internal/cmd/ship.go, internal/cmd/ship_full_green_test.go
       covers:   c-6
       desc:     Call requireFullGreen beside gateFreshness, before --print-body, the push and any
                 provider call. --force-unverified downgrades it to a warning, as it does for
                 a stale verdict.
       contract: Pass verdict, scoped.json on HEAD, no green.json: ship refuses, nothing is pushed
                 or written, and the refusal says to run a bare `dross test`.
                 --force-unverified: ships and prints the warning.
                 Full green on HEAD: ship proceeds past the check.

  t-11 Scope execute, solo and writer prompts
       files:    assets/prompts/execute.md, assets/prompts/_execute.solo.md,
                 assets/agents/dross-task-writer.md, internal/cmd/execute_prompt_test.go,
                 internal/cmd/prompt_commit_gate_test.go
       covers:   c-7
       desc:     §1f: a lane with vouch_probe, exit 0 and no `uncovered:` line means commit on §1e's
                 run; otherwise bare `dross test`. Solo main loop re-runs `dross test --files` after
                 the writer stops, since the stop cleared the record.
       contract: TestExecutePromptLaneCommitRunsFullSuite is rewritten to fail unless §1f names
                 `uncovered:` and keeps a bare `dross test` as that branch only.
                 _execute.solo.md no longer says "bare `dross test`" for its `test: green` step.
                 codeAdds counts a fenced `dross test --files` as tested; TestPromptCommitsSatisfy
                 GreenGate still bites when ship.md's bare test is removed.
                 Writer prompt no longer says the main loop runs the full suite.
                 TestPromptByteBudget stays green at the 15 KB cap.

  t-12 Scope quick prompts, add verify's full run
       files:    assets/prompts/quick.md, assets/prompts/_quick.solo.md, assets/prompts/verify.md,
                 internal/cmd/quick_prompt_test.go, internal/cmd/prompt_test_command_test.go
       covers:   c-6, c-7
       desc:     quick §4: with a vouching lane run `dross test --files` per changed path, and bare
                 only on `uncovered:` or a refused commit. verify.md: bare `dross test` before
                 `dross verify`, since verify now refuses without it.
       contract: quick_prompt_test's §4 order check (test, Green, solo re-fetch) passes with the
                 scoped branch, and fails if §4 loses its bare `dross test` for lane-less repos.
                 _quick.solo.md's "green `dross test`" wording no longer demands a bare run.
                 New test: verify.md has a fenced bare `dross test` ahead of its §1 `dross verify`.

  t-13 Prove recorder and gate agree end to end
       files:    internal/cmd/scoped_commit_e2e_test.go
       covers:   c-2, c-3, c-4, c-5, c-9
       desc:     Real git repo, real Test() command, real gate.Judge. Fake vitest and probe scripts
                 stand in for the runner, with a .svelte path in the set. t-7 and t-8 each test
                 against a seeded record, so this is the only test where one reads what the other wrote.
       contract: `dross test --files ./src/A.svelte --files src/a.test.ts`, then a judged `git add -A
                 && git commit`: admitted, test_command never spawned, green.json absent. The `./`
                 spelling fails here if the recorder stores a different form from the gate's Changed.
                 Same flow with src/b.ts whose probe exits empty: refused naming src/b.ts.
                 Delete the lane's vouch_probe: same tree refused with the full-green text (c-4).
                 Scoped green plus requireFullGreen on the new HEAD: refused (c-6 holds after the
                 commit it admitted).

Wave 4 (t-14 depends t-5, t-6, t-10, t-11, t-12, t-13)
  t-14 Prove scoped commit on real barktime
       files:    .dross/phases/scoped-commit-gate/barktime-proof.toml
       covers:   c-5
       desc:     `make install` from this branch. Land barktime's lane, probe and `vitest related`
                 path as a /dross-quick in ~/Development/barktime, then make one real task commit
                 there touching a .svelte file. Record SHA and log path.
       contract: Acceptance record, not a Go test, because barktime is not on CI (locked barktime_proof).
                 c-5 fails unless all of these hold:
                 the record's SHA resolves with `git -C ~/Development/barktime cat-file -e`;
                 the run log's lane line is the related run and contains no bare in-container suite;
                 that log shows the .svelte path covered with no `uncovered:` line;
                 `dross gate` state in barktime shows commit-green on for that commit;
                 barktime's green.json tree differs from that commit's tree, proving no full run
                 admitted it.
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-2, t-4, t-7, t-8, t-13 |
| c-3 | t-3, t-8, t-13 |
| c-4 | t-8, t-13 |
| c-5 | t-6, t-13, t-14 |
| c-6 | t-4, t-9, t-10, t-12 |
| c-7 | t-11, t-12 |
| c-8 | t-5 |
| c-9 | t-2, t-7, t-8, t-13 |

## Judgment calls

- **Per-path attribution (c-9):** chose a per-path `vouch_probe` line, one spawn of the suite plus cheap exit-code probes. Rejected one lane spawn per path because barktime's `in-container.sh` runs `compose run --build` with its services each call, so N paths would cost N stack boots.
- **Rejected parsing runner output for c-9:** the locked empty_detection rule keeps runner output unread, and a stdout manifest makes the claim forgeable and silent when malformed. Exit codes keep the contract testable through the spawn seams.
- **Lane exit status alone cannot attribute:** `vitest related a b` passes if a has tests. On vitest 4.1.11, `--passWithNoTests` exits 0 and without it a no-test run fails like a red suite, so barktime's probe has to be its own script.
- **The opt-in field is the probe itself:** one field satisfies scoped_gate_optin and c-9, rather than a boolean plus a second field. A lane that can't say which paths reach a test has nothing to vouch with.
- **Consent covers the probe:** folded into LaneLine under a new frame. Rejected leaving it outside the grant, since it is an executed line and flipping it weakens a gate.
- **scoped.json is a separate record:** rejected a `scope` field on Green. c-6's "never satisfies" is then structural, because verify and ship only call LoadGreen.
- **Exact-tree match plus path coverage:** rejected per-path blob matching, which would admit a commit whose other files moved since the run. c-2 says "that exact tree".
- **Same-tree runs union Covered:** rejected last-run-wins, which would make a follow-up `--files` run for one missed path discard the rest.
- **Deleted paths stay uncovered:** a deletion has no file to probe, so it needs a full green. Fail-closed, and it will cost a full run on delete-heavy tasks.
- **Unknown probe status means uncovered, not a new exit code:** keeps the exit taxonomy unchanged. Transport failures still surface as exit 3.
- **Probes run after prepare, before the command:** a cold host may need prepare to run the probe, and an all-empty lane still skips its expensive command.
- **Exempt globs reuse the lane glob dialect:** `*.md` therefore matches root-level files only and nested docs need `**/*.md`. Rejected a second matcher; t-3 pins this.
- **"Build and dependency config stays refused" enforced by validate:** a closed file list is checked against exempt globs, so `**` can't exempt package.json. Rejected a runtime denylist in the gate, which would also override a vouching lane's own match.
- **Exempt is inert with no opted-in lane:** c-4 requires today's behaviour exactly, so a repo that declares test_exempt without opting in gets no docs shortcut.
- **c-6 applies to every repo with tests declared, not only opted-in ones:** the spec gives no carve-out and c-4 is scoped to commit-green. Cost is one bare `dross test` before verify where HEAD lacks a green, added to verify.md in t-12.
- **--force-unverified bypasses the ship check with a warning:** matches gateFreshness. Rejected a hard refusal, which would add a second, inconsistent meaning to that flag.
- **t-13 exists as its own task:** t-7 and t-8 each pass against a seeded record, so a path-spelling mismatch between them would leave both green and the feature broken.
- **c-8 needs a real parser test, with `svelte` added to the ts-project fixture:** the logic is JS inside a Go string, so a stubbed node proves nothing. No Vite Svelte plugin needed, since mutantsIn counts generated mutants whatever their outcome. The lockfile change has to clear CI's fixture audit.
- **Markup gets constructs too, not just script blocks:** a hunk left bare in a multi-line `{#if}` would drop mutants whose location spills past the hunk, the hole AST ranges exist to close.
- **`dross test lane preview` left unchanged:** no criterion needs it, and the scoped run's own `uncovered:` line carries the signal. Include-first says flag it: the judge may want a probe line added to preview in t-6.
- **c-5 stays an acceptance record:** barktime can't run on CI, so t-13 gives the repeatable fixture proof and t-14 the locked real-repo evidence.
