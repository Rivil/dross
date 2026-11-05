# scoped-commit-gate: risk lens

Three findings from reading the code shaped this graph more than the criteria did:

- **`vitest related` exits 0 when it finds no tests.** vitest 4.1.11 (barktime's pin) sets `passWithNoTests ??= true` for the `related` command (`node_modules/vitest/dist/chunks/cac.uFydS1Z4.js:2298,2308`). `empty_exit`, today's only zero-test signal, can never fire on it. Forcing `--passWithNoTests=false` gives exit 1, the same code as a red suite. So c-9 and `zero_test_scoped_run` cannot be met from exit codes.
- **One run cannot attribute coverage per path.** `vitest related a.ts b.svelte` passes if `a.ts` has tests, and says nothing about `b.svelte`. c-9 needs a per-path signal, which `Selection.Matched` does not give (it only says which lane a path hit).
- **`.dross/` is outside the tree fingerprint and commits there are ungated.** Setting the new opt-in, or `exempt = ["**"]`, moves no tree and needs no green. An agent could widen its own gate in one ungated commit unless both are bound to a human grant.

```
Phase scoped-commit-gate — 17 tasks across 5 waves

Wave 1
  t-1  Fix README roadmap, guard ticks and Azure
       files:    README.md, internal/cmd/readme_roadmap_test.go
       covers:   c-1
       owns:     the repo's own suite being red; every later commit here needs a full green, so run this first
       does:     v1.8 (complete) with all criteria ticked, v1.9 (active) with its 5 criteria, Azure DevOps line gone. Extend the test: a (complete) section has no `- [ ]`, the active section's box count equals len(success_criteria).
       contract: TestReadmeRoadmapMatchesRecordedMilestones fails if v1.8 still reads (active) or v1.9 (planning)
                 a `- [ ]` left under a (complete) heading fails the new tick check (the existing test only compares status, so unticked v1.8 boxes pass it today)
                 a v1.9 section with 4 or 6 boxes fails against v1.9.toml's 5 criteria

  t-2  Add scoped green record and its lifecycle
       files:    internal/gatestate/store.go, internal/gatestate/store_test.go, internal/gate/writer.go, internal/gate/writer_test.go, internal/secretscan/writers.go
       covers:   c-2, c-6
       owns:     forged, tracked, corrupt or empty records; a scoped green leaking into green.json; the writer's own green admitting a commit
       does:     gatestate.Scoped {Tree, Covered, Lanes, At, Runner} in its own scoped.json with Load/Save/Clear. Writer SubagentStop clears it beside ClearGreen. Add it to the machine-local list in writers.go.
       contract: SaveScoped refuses an empty tree AND an empty Covered (a green that vouches for nothing cannot be recorded at the store)
                 a git-tracked scoped.json is refused unread by LoadScoped (RefuseTracked)
                 undecodable scoped.json returns an error naming the path, never a zero value
                 SaveScoped leaves green.json byte-identical, and LoadGreen returns nil with only scoped.json present
                 writer SubagentStop removes scoped.json; a stop from a different agent id leaves it
                 tamper-guard refuses Write and `rm` on .dross/gate/scoped.json

  t-3  Add vouch and exempt fields with validation
       files:    internal/project/project.go, internal/cmd/validate.go, internal/cmd/validate_lane_test.go, internal/project/save_lossless_test.go
       covers:   c-2, c-4
       owns:     a vouching lane that can never measure a path; a catch-all exempt glob; config that does not round-trip
       does:     [[runtime.test_lane]] `vouch` bool, [runtime] `test_exempt` globs. Validate refuses vouch without a selector, vouch with empty_exit listing 1, and catch-all exempt globs.
       contract: `vouch = true` with no selector is a validate problem naming the lane (a selector-less lane spawns its whole command, so no path is attributable)
                 `vouch = true` with `empty_exit = [1]` is a validate problem (a red would be read as a miss)
                 `test_exempt = ["**"]` and `["*"]` are validate problems
                 a project.toml with neither field saves byte-identical (feastahead must not churn)

  t-4  Add pure scoped path classifier
       files:    internal/testlane/coverage.go (new), internal/testlane/coverage_test.go (new)
       covers:   c-3, c-9
       owns:     build config slipping through; exempt globs over-matching; path-spelling mismatches
       does:     Classify(changed, covered, exempt) -> uncovered, build-config and exempt sets, `.dross/` dropped. A fixed build-config list (package.json, lockfiles, go.mod, go.sum, *.config.*, tsconfig*.json, Makefile, Dockerfile*, docker-compose*, .github/**) beats both coverage and exempt.
       contract: package.json present in BOTH covered and an exempt `*.json` glob still classifies as needing a full green (the lock says build config stays refused, not merely unexempted)
                 src/vite.config.ts in covered classifies as needing a full green
                 `./src/a.ts` in changed matches `src/a.ts` in covered
                 docs/x.md matches exempt `docs/**`, and src/docs.md does not
                 an empty covered set returns every non-exempt path, in input order, for the refusal to name

  t-5  Expose a proven test count from summaries
       files:    internal/runsummary/summary.go, internal/runsummary/jsrunner.go, internal/runsummary/jsrunner_test.go
       covers:   c-9
       owns:     exit-0 zero-test runs; skip-only runs; truncated output
       does:     Segment.Ran() (n, known) from the parser's own summary line, vitest only. known=false when no parser claimed the segment, or the claiming parser has no count.
       contract: "No test files found, exiting with code 0" gives known=false (the exact barktime `vitest related` miss)
                 `Tests 3 skipped (3)` gives 0: collected but executed nothing, so it vouches for nothing
                 a run killed before the `Tests` line gives known=false
                 a test's console output printing `Tests 9 passed (9)` before the real `Tests 1 passed (1)` yields 1, the last summary
                 ANSI-coloured summary lines parse the same as plain

  t-6  Resolve .svelte constructs via svelte compiler
       files:    internal/mutation/astspan.go, internal/mutation/construct.go, internal/mutation/construct_test.go, internal/mutation/construct_e2e_test.go, internal/mutation/testdata/ts-project/package.json, internal/mutation/testdata/ts-project/package-lock.json, internal/mutation/testdata/ts-project/src/Widget.svelte (new)
       covers:   c-8
       owns:     line-offset errors; two script blocks; lang variants; `<script>` inside a comment; same-line overlap; unresolvable or unparsable input
       does:     Drop the .svelte refusal. astspan resolves svelte/compiler from the project tree, as Stryker's parser does, takes block offsets from the compiler (never a regex), Babel-parses each block with startLine, and emits top-level markup nodes as constructs too. Spans are merged so the disjoint contract holds. Add pinned svelte to the fixture.
       contract: a function on line 40 of a .svelte reports Start=40, not its line within the script block (off-by-one drops mutants, because Stryker keeps only mutants wholly inside the range)
                 a file with `<script module>` and an instance `<script lang="ts">` yields constructs from both, sorted and pairwise disjoint
                 `<!-- <script>x</script> -->` above the real script produces no phantom construct
                 `<script>let x = 1;</script><div>{x}</div>` on one line yields one merged construct, so enclosing()'s binary search cannot miss
                 svelte unresolvable from the workdir and a malformed .svelte each wrap ErrASTUnavailable with a dross-authored cause; TestSvelteIsUnavailableWithoutASpawn is rewritten to these cases, not deleted

Wave 2 (depends wave 1)
  t-7  Bind vouch to the lane grant, add flags (depends t-3)
       files:    internal/consent/lane.go, internal/consent/lane_test.go, internal/cmd/test_lane.go, internal/cmd/test_lane_edit_test.go, assets/prompts/_options.lanes.md
       covers:   c-2, c-4
       owns:     an agent opting a lane in through an ungated .dross commit
       does:     LaneLine takes a new frame only when vouch is true, so non-vouching grants keep their fingerprint. `dross test lane add/edit --vouch`; _options.lanes.md explains vouch and test_exempt.
       contract: flipping vouch false to true on a granted lane makes LaneConsented report stale until `dross trust`
                 a vouch=false lane's LaneLine is byte-identical to today's, for plain, prepared and templated lanes (no existing grant stales)
                 `lane edit --vouch` on a selector-less lane is refused, and the file is unchanged

  t-8  Add human grant for test_exempt (depends t-3)
       files:    internal/consent/exempt.go (new), internal/consent/exempt_test.go (new), internal/cmd/trust.go, internal/cmd/trust_test.go, internal/diag/trust.go
       covers:   c-3
       owns:     an agent adding `src/**` to the exempt list to pass the gate
       does:     Own grant over the sorted globs, same pattern as Install's separate grant. `dross trust` prints the globs before granting. Doctor reports stale.
       contract: appending a glob makes the exempt grant stale
                 reordering globs does not stale it
                 the trust output prints every glob before the grant is written
                 a repo with no test_exempt reports not-applicable, never untrusted

  t-9  Split vouching lanes into per-path run units (depends t-3)
       files:    internal/cmd/lane_plan.go, internal/cmd/lane_plan_test.go, internal/cmd/lane_preview.go, internal/cmd/lane_preview_json.go, internal/cmd/lane_preview_test.go
       covers:   c-9
       owns:     a covered neighbour hiding an untested path; preview drifting from the gate
       does:     plannedLane gains Units {Paths, Line, Selector}, one per distinct derived selector for a vouching lane, a single unit otherwise. Non-vouching lanes keep Line. Preview prints units from the same plan.
       contract: a vouching `path` lane over a.ts and b.svelte plans two units with different lines
                 a vouching `go-package` lane over two files in one package plans one unit carrying both paths
                 a non-vouching lane plans exactly today's single Line (existing lane_plan tests unchanged)
                 a deleted path appears in Dropped and in no unit, so it can never be covered
                 preview's lines equal the plan's unit lines, read from the same value

  t-10 Refuse verify and ship without full green (depends t-2)
       files:    internal/cmd/full_green.go (new), internal/cmd/full_green_test.go (new), internal/cmd/verify.go, internal/cmd/ship.go, internal/cmd/hermetic_env_test.go
       covers:   c-6
       owns:     a multi-hour mutation run on an unproven tree; a scoped green reaching ship; a no-test repo locked out; the blast radius across 15 verify and 17 ship test files
       does:     requireFullGreen compares treefp.Base (HEAD tree) with LoadGreen only. verify calls it right after project load, before scope or any host work, on the attached and detached paths. ship calls it beside gateFreshness, before --print-body, the repoint and the auto-commit. Skipped with no tests declared or outside git. A package seam defaults off in TestMain so existing fixtures hold.
       contract: green.json for an older tree refuses verify before configuredAdaptersFn runs (the adapter seam records zero calls), and no tests.json is written
                 a valid scoped.json on HEAD's tree with no green.json refuses both commands
                 `--detach` is refused by the same check, before the tree push
                 green on the dirty work tree but not HEAD refuses
                 a clean HEAD whose green matches passes, proving Base and WorkingTree fingerprints compare equal
                 ship refused leaves HEAD and the remote unchanged; `--force-unverified` warns on stderr and proceeds, keeping `ship --json` stdout to one object
                 a repo with no test_command and no lanes is not refused
                 wiring tests fail if either call site is deleted, so the TestMain default cannot hide its removal

  t-11 Prove svelte ranging through planner and Stryker (depends t-6)
       files:    internal/verify/range_expand_test.go, internal/verify/range_provenance_test.go, internal/mutation/construct_e2e_test.go, internal/mutation/range_test.go
       covers:   c-8
       owns:     the range and Stryker's mutant locations disagreeing; a hunk in `<style>`; a narrowed file with zero mutants read as a drop
       does:     Real-tool proof over Widget.svelte. Fix expansion or checkInstrumented only if a contract fails.
       contract: a hunk inside one script function: Stryker generates that function's mutants and none from a sibling function (real tool, DROSS_REQUIRE_E2E)
                 a hunk on a template expression `{count + 1}` generates that expression's mutant, so markup constructs are not script-only
                 a hunk only inside `<style>` stays a raw hunk, yields zero mutants, and the leg neither refuses nor records ast-unavailable
                 PlanRanges over a .svelte with a resolved index puts it in Dispatch, and WholeFile has no entry for it

Wave 3 (depends wave 2)
  t-12 Run units and record scoped green (depends t-2, t-5, t-9)
       files:    internal/cmd/test.go, internal/cmd/test_output.go, internal/cmd/test_files_test.go, internal/cmd/test_green_test.go, internal/cmd/test_lane_miss_test.go
       covers:   c-2, c-9
       owns:     zero-test green; escalation; partial failure; tree moving mid-run; TTY runs having no segment; full-red and scoped-green coexisting
       does:     Vouching units spawn in turn, after one prepare and one tree sync per lane. A unit covers its paths only if exit 0 and Ran()>0. A counting segment exists on a terminal too. Proven-zero or unknown counts a miss. scopedRecorder saves on a green run with a non-empty Covered and an unchanged tree. Prints one line: covered, uncovered.
       contract: exit 0 with "No test files found" records nothing, and an all-zero run exits 5, not 0
                 the zero-test run spawns only the lane's own lines; no test_command spawn appears in the recorder (zero_test_scoped_run: never escalates)
                 a.ts with tests and b.svelte without records Covered=[a.ts] and exits 0 (c-9)
                 one red unit records nothing and removes a prior scoped.json on that tree; exit 3, 4, 6, 7 or 8 leaves it
                 a non-vouching lane's path is never in Covered, even when that lane is green
                 a file written during the run records nothing and names it, reusing the green recorder's line
                 the same run with testStdoutIsTTY true records the same Covered
                 a full red on tree T clears scoped.json on T; a scoped red leaves green.json alone
                 `dross test --files` in a repo with no vouching lanes writes no scoped.json
                 lane prepare spawns once for a three-unit lane (locked prepare_scope)
                 TestScopedGreenMatchesCandidate: the recorded tree equals treefp.Candidate after `git add -A`

  t-13 Admit scoped commits in commit-green (depends t-2, t-3, t-4, t-7, t-8)
       files:    internal/gate/commit.go, internal/gate/commit_test.go, internal/gate/fixture_test.go
       covers:   c-2, c-3, c-4
       owns:     no-lane behaviour drifting; a stale record outliving its lane; re-edit after the run; exempt-only commits
       does:     judgeGreen order: full match admits; no vouching lane takes today's path untouched; all paths exempt under a current grant admits; otherwise Classify. Uncovered paths refuse by name; a tree mismatch refuses; else admit. Each recording lane must still vouch under a current grant.
       contract: with no vouching lane and a stale scoped.json on the exact tree, the refusal string is byte-identical to today's, and it is admitted only by a full green (c-4)
                 existing commit_test.go cases pass unmodified
                 scoped green on the exact tree covering all changed paths admits with no green.json (c-2)
                 one uncovered path refuses and names it, and only it (c-3)
                 no scoped.json at all still names every uncovered path
                 a covered path re-edited after the run refuses on the tree mismatch
                 a covered path staged but with unstaged dirt elsewhere takes the "stage or clean" refusal, not "re-run"
                 package.json in a commit refuses even when listed in Covered and exempt
                 a deletion and a rename's old side are uncovered and refused
                 lane edited in project.toml after the run (tree unmoved): the record stops vouching and the commit refuses
                 a docs-only commit under a current exempt grant admits; the same commit with a stale grant refuses
                 the gate still spawns no git in a repo with no test_command and no lanes (no_test_repo)

Wave 4 (depends wave 3)
  t-14 Prove run-to-commit-to-verify journey end to end (depends t-10, t-12, t-13)
       files:    internal/cmd/scoped_commit_e2e_test.go (new)
       covers:   c-2, c-3, c-4, c-6, c-9
       owns:     recorder and gate each passing alone but disagreeing on tree or path form
       does:     Real git fixture, spawn seam emitting vitest output, real gate.Judge on a real `git commit` payload.
       contract: `--files a.ts` green, then the commit is admitted with no bare-run spawn in the recorder
                 next, verify refuses on the committed HEAD; a bare green admits verify
                 a two-path run where one path collects nothing: the commit is refused, naming that path
                 a lanes-declared, non-vouching repo: `--files` green, commit refused, bare green, commit admitted

  t-15 Let execute, solo and writer prompts use scoped green (depends t-12, t-13)
       files:    assets/prompts/execute.md, assets/prompts/_execute.solo.md, assets/agents/dross-task-writer.md, internal/cmd/prompt_commit_gate_test.go, internal/cmd/execute_prompt_test.go
       covers:   c-7
       owns:     the audit going vacuous once `--files` counts; solo losing the writer-green rule; the ARCHITECTURE commit going scoped
       does:     §1f reads §1e's scoped line: all covered means commit; otherwise bare. Solo re-runs `--files` in the main loop after the writer stops, since that stop clears the record. Audit accepts a `--files` or bare run before a code add, except ship.md, which stays bare.
       contract: deleting both runs from execute §1f fails TestPromptCommitsSatisfyGreenGate; the vacuity and "it bites" arms still fire
                 ship.md with its bare `dross test` swapped for `--files` fails (a scoped ARCHITECTURE commit leaves HEAD with no full green, so c-6 would refuse the ship)
                 a fenced bare `dross test` as the unconditional line in §1f fails the new test
                 solo prompt without a main-loop `--files` after the writer stops fails
                 assets budget test holds execute.md at or under 15000 bytes

  t-16 Let quick prompt use scoped green (depends t-12, t-13)
       files:    assets/prompts/quick.md, assets/prompts/_quick.solo.md, internal/cmd/quick_prompt_test.go
       covers:   c-7
       owns:     quick.md's 180 bytes of budget headroom
       does:     Replace §4's run in place, net-neutral in bytes; any longer guidance goes in _quick.solo.md or a new partial.
       contract: quick.md over 15000 bytes fails the assets budget test
                 quick §4 with no fenced `dross test` of either form fails TestPromptCommitsSatisfyGreenGate
                 a bare-only §4 fails the new scoped-form test

Wave 5 (depends wave 4)
  t-17 Prove a real barktime task commit (depends t-14, t-15, t-16)
       files:    .dross/phases/scoped-commit-gate/barktime-proof.md (new)
       covers:   c-5
       owns:     every assumption about vitest, Docker and .svelte that a fixture cannot exercise
       does:     Per barktime_proof: `make install` from this branch, /dross-quick in barktime landing lane, vouch and `vitest related`, human `dross trust`, then one real task commit. Record SHA and run log. The run script needs a `vitest related` path (scripts/test/in-container.sh only does `vitest run`).
       contract: the proof names a barktime commit SHA whose run log shows `vitest related` on a .svelte path, tests collected, and no full-suite spawn
                 `dross gate status` in barktime at that commit shows commit-green not lifted
                 the same session shows one refusal naming a .svelte with no related test
                 the proof file is absent or lacks a SHA: c-5 stays unmapped and verify cannot pass
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-2, t-3, t-7, t-12, t-13, t-14 |
| c-3 | t-4, t-8, t-13, t-14 |
| c-4 | t-3, t-7, t-13, t-14 |
| c-5 | t-17 |
| c-6 | t-2, t-10, t-14 |
| c-7 | t-15, t-16 |
| c-8 | t-6, t-11 |
| c-9 | t-4, t-5, t-9, t-12, t-14 |

## Judgment calls

- **Zero-test detection reads the runner's summary count.** Rejected `empty_exit` (cannot fire on `vitest related`) and a lane-declared JSON report (needs a file fetched back from remote hosts). This overturns the earlier `empty_detection` lock ("the output is never read") for vouching lanes only. Unlike what that lock forbade, the read can only withhold coverage, never turn a red into a miss. It is not in this spec's locks, so the judge should surface it to the user.
- **c-9 attribution is one spawn per path.** Rejected a single run with attribution from coverage output (needs instrumentation in every lane) and a second cheap list pass (`vitest list` has no `related` mode, and it adds a per-framework command). Cost is real: barktime's `in-container.sh` builds a compose stack per spawn, so N paths means N stacks. t-17 may need a warm stack in the barktime quick, which stays out of dross.
- **Count parsing is vitest only.** Rejected adding every parser; the v1.9 non-goal keeps other stacks as they are. A vouching lane on any other runner records nothing and says so, which fails closed.
- **Scoped green is its own file.** Rejected a `scope` field on green.json. A reader that forgot to check the field would satisfy c-6 with a scoped green; a separate file means verify and ship cannot see it.
- **No union across runs on one tree.** Rejected merging Covered from `--files a` then `--files b`, which invites a stale half surviving a later red. One run must cover the commit (c-2 says "a green run").
- **Build-config list is fixed in code and beats coverage as well as exempt.** Rejected treating the lock as "not exempt by default". A `src/vite.config.ts` matched by a lane would otherwise be vouched for by a `vitest related` hit.
- **Vouch and test_exempt are both bound to human grants.** Rejected validate-only checks, since nothing runs validate at commit and `.dross/` commits are ungated. This is the most cuttable item here: dropping t-8 leaves the exempt list agent-editable, and that is the trade.
- **Exempt globs are ignored without a vouching lane.** Rejected honouring them on their own. c-4 and `scoped_gate_optin` keep feastahead-shape repos on the full-green rule, even if they declare globs.
- **Deletions are always uncovered.** Rejected treating a deleted file as trivially safe: its importers are outside the changed set, so a scoped run proves nothing about them. A task that deletes a file pays one full run.
- **c-6 checks HEAD strictly.** Rejected exempting docs-only drift from the last full green. A final exempt commit forces a full run before verify. The criterion says HEAD tree, and the check stays simple.
- **`--force-unverified` overrides the ship check, and verify has no override.** Matches the earlier `stale_override` lock (one override). Rejected a hard refusal because green.json is machine-local, so a ship from a second machine would otherwise need a full run for a tree CI has already measured.
- **green.json stays single-slot.** Rejected a small history. A bare run on a dirty tree can overwrite HEAD's green and cost a re-run, but that fails closed and costs time, not correctness.
- **TestMain default-off seam for t-10.** Rejected seeding green.json across 15 verify and 17 ship test files. The risk that the default hides a deleted call is owned by t-10's wiring tests.
- **t-1 stays wave 1 but must execute first.** Every commit in this no-lane repo needs a full green, and the suite is red until t-1 lands. Rejected adding it to every task's depends_on, which would serialise the graph for an ordering that task ids already give.
- **Svelte offsets come from svelte/compiler, not a regex.** Rejected scanning for `<script>`: comments, strings and the `generics` attribute break it, and Stryker's own spans come from the compiler. This adds `svelte` to the ts-project fixture lockfile, which `npm audit` in CI will now read.
- **Markup nodes count as constructs.** Rejected script-only constructs. Stryker mutates template expressions, so a markup hunk with no construct would keep only the raw hunk and drop any mutant reaching past it, the same hole AST ranges exist to close.
- **The queued Stryker `--mutate` override bug stays out.** It makes verify mutate `.svelte` files barktime's config excludes, which is why c-8 matters there, but fixing it is a separate quick and not in this spec.
