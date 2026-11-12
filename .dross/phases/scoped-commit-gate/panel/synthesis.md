# scoped-commit-gate: panel synthesis

## Scores

Scale 1-5. Granularity counts tasks at 5+ files or across 3+ layers, as the brief defines too large.

| Dimension | Draft | Score | Why |
|---|---|---|---|
| Criteria coverage | risk | 4 | All 9 and all 4 locks. Misses verify.md, so `/dross-verify` would hit the new refusal with no bare run in its prompt. |
| Criteria coverage | mvp | 3 | All 9 on paper. Reads "stays refused" as "not exempted by default", and c-8's markup construct is one span per run of lines, not the construct overlapping the hunk. |
| Criteria coverage | verification | 4 | All 9, with verify.md. Pads c-5 onto t-6 and t-13, and a fake runner cannot prove c-5. Misses the ~32 test files that call Verify()/Ship(). |
| Test-contract specificity | risk | 5 | Every line names a surface, including partial staging, a lane edited after the run, and the ship.md/ARCHITECTURE trap. |
| Test-contract specificity | mvp | 4 | Concrete and imaginable, but thin on the gate's edge cases (no re-staging, no stale-lane case). |
| Test-contract specificity | verification | 5 | Names test functions, remote spawn order and transport failure. The strongest c-5 check (green.json tree differs from the commit's tree). |
| Granularity | risk | 3 | 8 of 17 at 5+ files (t-2, t-6, t-7, t-8, t-9, t-10, t-12, t-15). t-2, t-7 and t-8 cross 3 layers. |
| Granularity | mvp | 2 | 4 of 8 at 5+ files. t-6 is 10 files, and t-3 joins the recorder to the store. |
| Granularity | verification | 3 | 7 of 14 at 5+ files. t-2 is 7 files across 4 layers. Its verify/ship split is the best cut in any draft. |
| Wave correctness | risk | 4 | Dependencies are real. t-16 needs t-15's audit change if §4 really drops bare, and does not declare it. |
| Wave correctness | mvp | 3 | t-1 gates everything by commit-green only. t-6 lands before the gate it describes. t-5 waits on t-3 for one type. |
| Wave correctness | verification | 4 | Per-task dependencies are right, and t-10 after t-9 is the correct edge. t-2 and t-3 both edit project.go and validate.go in wave 1, which is safe only because execution is serial. |

**Skeleton: risk.** It alone has a pure classifier, per-path units as a planned value, and the ship.md trap, and its dependency edges survive checking against the code. Its one structural liability, the output-parsing path, is reversed below (Disagreement 2).

## Merged plan

```
Phase scoped-commit-gate — 18 tasks across 5 waves

Wave 1 (t-1 runs first: every commit in this repo needs a full green, and the suite is red until it lands)
  t-1  Sync README roadmap, guard ticks and Azure  [risk+mvp+verification]
       files:    README.md, internal/cmd/readme_roadmap_test.go
       covers:   c-1
       contract: TestReadmeRoadmapMatchesRecordedMilestones fails if v1.8 reads (active) or v1.9 (planning).
                 A `- [ ]` under any (complete) heading fails.
                 An active section whose box count differs from len(success_criteria) fails: v1.9 with 4 or 6 boxes against v1.9.toml's 5.
                 "Azure DevOps" anywhere in the roadmap section fails.

  t-2  Add scoped.json record  [risk+verification+mvp]
       files:    internal/gatestate/store.go, internal/gatestate/store_test.go, internal/secretscan/writers.go
       covers:   c-2, c-6
       contract: SaveScoped refuses an empty tree and an empty Covered.
                 LoadScoped returns nil with no file, and an error naming the path on undecodable JSON, never a zero value.
                 A git-tracked scoped.json is refused unread (RefuseTracked).
                 SaveScoped leaves green.json byte-identical, and LoadGreen returns nil with only scoped.json present.
                 The record carries Tree, Covered, Lanes, At and Runner.

  t-3  Add vouch and test_exempt fields with validation  [risk+mvp+verification]
       files:    internal/project/project.go, internal/cmd/validate.go, internal/cmd/validate_lane_test.go, internal/project/save_lossless_test.go
       covers:   c-2, c-3, c-4, c-9
       contract: `vouch = true` with no selector is a validate problem naming the lane.
                 `vouch = true` with an empty empty_exit is a validate problem naming the lane.
                 `vouch = true` with empty_exit listing 1 is a validate problem (a red would be read as a miss).
                 `test_exempt = ["**"]` and `["*"]` are validate problems, and `["docs/["]` is one naming the glob.
                 A project.toml with neither field saves byte-identical (feastahead must not churn).

  t-4  Add pure scoped path classifier  [risk+verification]
       files:    internal/testlane/coverage.go (new), internal/testlane/coverage_test.go (new), internal/testlane/match.go, internal/testlane/match_test.go
       covers:   c-3, c-9
       contract: package.json present in both covered and an exempt `*.json` glob still classifies as needing a full green.
                 src/vite.config.ts in covered classifies as needing a full green.
                 `./src/a.ts` in changed matches `src/a.ts` in covered.
                 docs/x.md matches exempt `docs/**`, and src/docs.md does not.
                 MatchAny(["docs/**"], "docs/a/b.md") is true and MatchAny(["*.md"], "docs/a.md") is false, pinning `*` to one segment.
                 An empty covered set returns every non-exempt path, in input order, for the refusal to name.

  t-5  Resolve .svelte constructs via svelte compiler  [risk+verification]
       files:    internal/mutation/astspan.go, internal/mutation/construct.go, internal/mutation/construct_test.go, internal/mutation/construct_e2e_test.go, internal/mutation/testdata/ts-project/package.json, internal/mutation/testdata/ts-project/package-lock.json, internal/mutation/testdata/ts-project/src/Widget.svelte (new)
       covers:   c-8
       contract: A function on line 40 of a .svelte reports Start=40, not its line within the script block.
                 `<script module>` plus an instance `<script lang="ts">` yields constructs from both, sorted and pairwise disjoint.
                 `<!-- <script>x</script> -->` above the real script produces no phantom construct.
                 `<script>let x = 1;</script><div>{x}</div>` on one line yields one merged construct, so enclosing()'s binary search cannot miss.
                 A multi-line `{#if}` block is one construct, and `<style>` yields none.
                 svelte unresolvable from the workdir and a malformed .svelte each wrap ErrASTUnavailable with a dross-authored cause and a position; TestSvelteIsUnavailableWithoutASpawn is rewritten to these cases, not deleted.

Wave 2 (t-6, t-7, t-8, t-9 depend t-3; t-10 depends t-2; t-11 depends t-5)
  t-6  Bind vouch to the lane grant  [risk+verification]
       files:    internal/consent/lane.go, internal/consent/lane_test.go
       covers:   c-2, c-4
       contract: Flipping vouch false to true on a granted lane makes LaneConsented report stale (CHANGED, not ABSENT) until `dross trust`.
                 A vouch=false lane's LaneLine is byte-identical to today's for plain, prepared and templated lanes, so no existing grant stales.

  t-7  Expose vouch in lane CLI  [risk+mvp+verification]
       files:    internal/cmd/test_lane.go, internal/cmd/test_lane_edit_test.go, internal/cmd/test_lane_list_fields_test.go, assets/prompts/_options.lanes.md
       covers:   c-2, c-5
       contract: `lane edit --vouch` on a selector-less lane is refused, and project.toml is byte-unchanged.
                 `lane add --vouch` with no --empty-exit is refused, and project.toml is byte-unchanged.
                 `lane edit x --vouch=false` removes the key and keeps the lane's document position.
                 `lane list` prints vouch only for a lane that sets it.
                 _options.lanes.md states the exit contract and that vouch and test_exempt need `dross trust`.

  t-8  Add human grant for test_exempt  [risk]
       files:    internal/consent/exempt.go (new), internal/consent/exempt_test.go (new), internal/cmd/trust.go, internal/cmd/trust_test.go, internal/diag/trust.go
       covers:   c-3
       contract: Appending a glob makes the exempt grant stale.
                 Reordering globs does not stale it.
                 The trust output prints every glob before the grant is written.
                 A repo with no test_exempt reports not-applicable, never untrusted.

  t-9  Split vouching lanes into per-path run units  [risk+mvp]
       files:    internal/cmd/lane_plan.go, internal/cmd/lane_plan_test.go, internal/cmd/lane_preview.go, internal/cmd/lane_preview_json.go, internal/cmd/lane_preview_test.go
       covers:   c-9
       contract: A vouching `path` lane over a.ts and b.svelte plans two units with different lines.
                 A vouching `go-package` lane over two files in one package plans one unit carrying both paths.
                 A non-vouching lane plans exactly today's single Line (existing lane_plan tests unchanged).
                 A deleted path appears in Dropped and in no unit, so it can never be covered.
                 Preview's lines equal the plan's unit lines, read from the same value.

  t-10 Refuse verify without a full green  [risk+mvp+verification]
       files:    internal/cmd/full_green.go (new), internal/cmd/full_green_test.go (new), internal/cmd/verify.go, internal/cmd/hermetic_env_test.go
       covers:   c-6
       contract: green.json for an older tree refuses verify before configuredAdaptersFn runs (the seam records zero calls), no tests.json is written, and the refusal names bare `dross test`.
                 A valid scoped.json on HEAD's tree with no green.json refuses.
                 `--detach` and `--skip-mutation` are refused by the same check, before the tree push.
                 Green on the dirty work tree but not HEAD refuses.
                 A clean HEAD whose green matches passes, proving treefp.Base and WorkingTree compare equal.
                 A .dross-only commit after the green still passes.
                 A repo with no test_command and no lanes is not refused.
                 A wiring test fails if the verify call site is deleted, so the TestMain default cannot hide its removal.

  t-11 Prove svelte ranging through planner and Stryker  [risk+verification]
       files:    internal/verify/range_expand_test.go, internal/verify/range_provenance_test.go, internal/mutation/construct_e2e_test.go, internal/mutation/range_test.go
       covers:   c-8
       contract: A hunk inside one script function: Stryker generates that function's mutants and none from a sibling (real tool, DROSS_REQUIRE_E2E).
                 A hunk on a template expression `{count + 1}` generates that expression's mutant, which a raw-hunk range would have dropped.
                 A hunk only inside `<style>` stays a raw hunk, yields zero mutants, and the leg neither refuses nor records ast-unavailable.
                 PlanRanges over a .svelte with a resolved index puts it in Dispatch, and WholeFile has no entry for it.

Wave 3 (t-12 depends t-10; t-13 depends t-2, t-9; t-14 depends t-2, t-3, t-4, t-6, t-8)
  t-12 Refuse ship without a full green  [verification+risk+mvp]
       files:    internal/cmd/ship.go, internal/cmd/ship_full_green_test.go (new)
       covers:   c-6
       contract: Pass verdict, scoped.json on HEAD, no green.json: ship refuses before --print-body, the repoint and the auto-commit; HEAD and the remote are unchanged, no .dross bookkeeping is written, and the refusal names bare `dross test`.
                 `--force-unverified` warns on stderr and proceeds, keeping `ship --json` stdout to one object.
                 Full green on HEAD proceeds past the check.
                 A wiring test fails if the ship call site is deleted.

  t-13 Run units and record scoped green  [risk+mvp+verification]
       files:    internal/cmd/test.go, internal/cmd/test_scoped.go (new), internal/cmd/test_scoped_test.go (new), internal/cmd/test_green_test.go
       covers:   c-2, c-9
       contract: a.ts exits 0 and b.svelte exits the declared empty_exit: run exits 0, Covered == [a.ts], and the transcript has `uncovered: b.svelte` (fails if the lane is spawned batched).
                 Every path a miss: exit 5, no scoped.json, and only the lane's own lines spawn, with no test_command spawn in the recorder (zero_test_scoped_run).
                 One red unit records nothing and removes a prior scoped.json on that tree; exit 3, 4, 6, 7 or 8 leaves it.
                 Transport failure on a unit: run exits 3, records nothing, and is never read as a miss.
                 A non-vouching lane's path is never in Covered, even when that lane is green, and its spawned line is byte-identical to today's.
                 A file written during the run records nothing and names it, reusing the green recorder's line.
                 A deleted path stays out of Covered.
                 Lane prepare spawns once for a three-unit lane (locked prepare_scope), and remote order through the spawn seams is sync, prepare, units.
                 A full red on tree T clears scoped.json on T; a scoped red leaves green.json alone; a green scoped run leaves green.json byte-unchanged (TestPartialRunsRecordNothing holds).
                 `dross test --files` in a repo with no vouching lanes writes no scoped.json.
                 A second run on the same tree replaces the record; it does not union.
                 TestScopedGreenMatchesCandidate: the recorded tree equals treefp.Candidate after `git add -A`.

  t-14 Admit scoped commits in commit-green  [risk+mvp+verification]
       files:    internal/gate/commit.go, internal/gate/commit_test.go, internal/gate/writer.go, internal/gate/writer_test.go
       covers:   c-2, c-3, c-4, c-9
       contract: With no lanes, and with lanes but none vouching, each with a matching scoped.json and a declared test_exempt: the refusal is string-equal to today's judgeGreen output, a docs-only commit is refused too, and only a full green admits (c-4).
                 Existing commit_test.go cases pass unmodified.
                 Scoped green on the exact tree covering all changed paths admits with no green.json (c-2).
                 Covered [a], changed [a, b]: refused, reason contains b and not a (c-3, c-9).
                 No scoped.json at all names every uncovered path.
                 A covered path re-edited after the run refuses, naming that path.
                 A covered path staged with unstaged dirt elsewhere takes the "stage or clean" refusal, not "re-run".
                 package.json in a commit refuses even when listed in Covered and exempt.
                 A deletion and a rename's old side are uncovered and refused.
                 A lane edited in project.toml after the run (tree unmoved) stops vouching and the commit refuses; a Covered path no vouching lane matches any more refuses.
                 docs/x.md alone under a current exempt grant admits with no record; the same commit with a stale grant refuses; docs/x.md plus covered src/a.ts admits.
                 Full green on the candidate tree still admits, whatever scoped.json says.
                 Writer SubagentStop removes scoped.json; a stop from an unbound agent id leaves it.
                 tamper-guard refuses Write and `rm` on .dross/gate/scoped.json.
                 The gate still spawns no git in a repo with no test_command and no lanes (no_test_repo).

Wave 4 (t-15 depends t-10, t-13, t-14; t-16 depends t-13, t-14; t-17 depends t-10, t-13, t-14)
  t-15 Prove run-to-commit-to-verify journey end to end  [risk+verification]
       files:    internal/cmd/scoped_commit_e2e_test.go (new)
       covers:   c-2, c-3, c-4, c-6, c-9
       contract: Real git repo, real Test(), real gate.Judge on a real `git commit` payload.
                 `--files ./src/A.svelte --files src/a.test.ts`, then a judged `git add -A && git commit`: admitted, test_command never spawned, green.json absent. The `./` spelling fails here if the recorder and the gate's Changed disagree.
                 Next, verify refuses on the committed HEAD, and a bare green admits it.
                 A two-path run where one path misses: the commit is refused, naming that path.
                 A lanes-declared, non-vouching repo: `--files` green, commit refused, bare green, commit admitted.
                 Remove the lane's vouch: same tree refused with the full-green text.

  t-16 Scope execute, solo and writer prompts  [risk+verification+mvp]
       files:    assets/prompts/execute.md, assets/prompts/_execute.solo.md, assets/agents/dross-task-writer.md, internal/cmd/execute_prompt_test.go, internal/cmd/prompt_commit_gate_test.go
       covers:   c-7
       contract: TestExecutePromptLaneCommitRunsFullSuite is rewritten to fail unless §1f names `uncovered:` and keeps bare `dross test` as that branch only; a fenced bare run as §1f's unconditional line fails.
                 Deleting both runs from §1f fails TestPromptCommitsSatisfyGreenGate, and the vacuity and "it bites" arms still fire.
                 ship.md with its bare `dross test` swapped for `--files` fails (a scoped ARCHITECTURE commit leaves HEAD with no full green, so c-6 would refuse the ship).
                 The solo prompt without a main-loop `--files` after the writer stops fails, because that stop cleared the record.
                 The writer line naming "the full suite" fails.
                 execute.md over 15000 bytes fails the budget test.

  t-17 Scope quick prompts, add verify's full run  [verification+risk+mvp]
       files:    assets/prompts/quick.md, assets/prompts/_quick.solo.md, assets/prompts/verify.md, internal/cmd/quick_prompt_test.go, internal/cmd/verify_prompt_test.go
       covers:   c-6, c-7
       contract: quick §4 with no fenced `dross test --files` fails the quick prompt test.
                 quick §4 that loses its bare `dross test` branch for lane-less repos fails (c-4 would refuse the commit).
                 _quick.solo.md demanding a bare run after a fix round fails.
                 verify.md without a fenced bare `dross test` ahead of `dross verify <phase>` fails.
                 quick.md over 15000 bytes fails the budget test (180 bytes of headroom today, so the rewrite must net-trim); a removed manifest row fails the conservation test.

Wave 5 (t-18 depends t-7, t-15, t-16, t-17)
  t-18 Prove a real barktime task commit  [risk+mvp+verification]
       files:    .dross/phases/scoped-commit-gate/barktime-proof.toml (new)
       covers:   c-5
       contract: Acceptance record, not a Go test. `make install` from this branch, then a /dross-quick in barktime lands a wrapper running `vitest related --run <path>` that exits a declared code when nothing was collected, a vouching lane, and test_exempt; the human grants them in their own terminal.
                 The record's SHA resolves with `git -C ~/Development/barktime cat-file -e`, and its run log exists.
                 The log shows `vitest related` on a .svelte path, tests collected, no `uncovered:` line, and no bare in-container suite.
                 `dross gate status` in barktime shows commit-green not lifted at that commit.
                 barktime's green.json tree differs from that commit's tree, proving no full run admitted it.
                 Same session: a .svelte with no related test gives exit 5 and no scoped.json, which fails if the wrapper lets vitest's default pass-with-no-tests through.
                 A commit touching one covered and one test-less .svelte is refused, naming the second.
```

### Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-2, t-3, t-6, t-7, t-13, t-14, t-15 |
| c-3 | t-3, t-4, t-8, t-14, t-15 |
| c-4 | t-3, t-6, t-14, t-15 |
| c-5 | t-7, t-18 |
| c-6 | t-2, t-10, t-12, t-15, t-17 |
| c-7 | t-16, t-17 |
| c-8 | t-5, t-11 |
| c-9 | t-3, t-4, t-9, t-13, t-14, t-15 |

### Repo checks behind the merge

- **README test:** observed red today with `go test -run TestReadmeRoadmapMatchesRecordedMilestones ./internal/cmd/` (v1.8 and v1.9 status). Unticked boxes exist only under v1.8 (4) and v1.9 (2), so the generic complete-section check in t-1 is safe.
- **`treefp.Base` already exists** (treefp.go:179: HEAD, `.dross/` out). Verification's new `treefp.Head` and its two files are dropped. MeasuredTree is unusable here because it also drops ARCHITECTURE.md, as mvp says.
- **tamper-guard needs no code:** `isGateRecord` matches anything under `/.dross/gate/`. Its t-14 line is a regression pin.
- **`writers.go` is needed:** line 233 lists each gate record by name, and mvp omits it. I did not confirm that a test fails without the entry.
- **`internal/gate/fixture_test.go` is dropped from t-14:** it holds captured hook payloads, not commit helpers.
- **`internal/cmd/test_output.go` is dropped from t-13:** it was only there for risk's counting segment.
- **Gate reaches grants through a new import:** `internal/gate` imports neither consent nor localstore today, and no draft lists this. `localstore.GrantStore` is the production store, and `go list -deps` shows localstore does not pull gate in, so t-14 can import it with no cycle.
- **Task `files` must stay in the repo:** `plan_edit.go:131-141` refuses escapes, so mvp's absolute barktime paths would fail plan validation. t-18 lists only the evidence file.
- **Execution is serial:** `NextRunnable` picks lowest wave, then array position. Wave-mates cannot race, and same-file edits within a wave are safe.
- **Fixture blast radius:** Verify() appears in 15 test files and Ship() in 17, which confirms risk's count. Verification's t-9/t-10 had no plan for them; t-10 takes the TestMain seam from risk and mvp.
- **Prompt headroom:** quick.md 14820 bytes, verify.md 14336, execute.md 12589, against the 15000 cap.
- **Fixture audit:** CI runs `npm audit --audit-level=high` on ts-project (ci.yml:161-163), so adding `svelte` in t-5 must clear it.

### Tasks still over the size line

t-5 (7 files, 3 of them fixture), and t-8, t-9, t-16 and t-17 (5 each) stay at or over the 5-file line. t-8 also crosses 3 layers. No draft offers a cleaner cut for any of them. The other splits came from drafts: verify/ship from verification, writer-clear into the gate task from mvp, CLI out of the consent task from verification.

### Not a divergence, but unresolved

All three drafts apply c-6 to every repo with tests declared; only verification says so. feastahead gets a new verify/ship refusal, while the v1.9 non-goal says other stacks keep their current gate behaviour. The criterion has no carve-out, so the plan follows it, but the user should confirm.

## Disagreements

Ordered by what a wrong call costs. Items 1-5 change the design; the rest change scope or shape.

**1. How c-9 attributes a path to a test.**
- risk, mvp: one spawn per path on a vouching lane. Lane exit codes then attribute directly.
- verification: one batched lane command plus a per-path `vouch_probe`, with the probe as the opt-in field.
- Default: per-path spawn with a `vouch` bool (t-9, t-13).
- Why it matters: barktime's `in-container.sh` does `compose run --build` per spawn, so N paths means N stack boots, and no draft measured one boot. A 10-file task could lose much of the time this phase exists to save. The probe avoids that but has no concrete barktime implementation in any draft: vitest's `list` has no related mode (`related: null` in its CLI config), so it needs a custom script. It also reports a proxy for "collected", not the run's own collection. Time one stack boot before locking this.

**2. What signals "no tests collected".**
- risk: parse vitest's summary count in dross (its t-5, `Segment.Ran()`), vitest only. Claims exit codes cannot meet c-9.
- mvp: exit codes only, with a barktime wrapper mapping "nothing collected" to a declared `empty_exit`.
- verification: exit codes only, from the probe.
- Default: mvp. Risk's t-5 is not in the plan; t-3 requires a non-empty empty_exit, and t-18 proves the wrapper.
- Why it matters: `empty_detection` (lane-selector-translation, locked) says "No matching against runner output", and this spec does not name it for overturning, though `scoped_gate_optin` names the locks it overturns. A judge should not lift a lock by default. Risk's core fact is true: vitest 4.1.11 sets `passWithNoTests ??= true` for `related` (cac:2298, 2308). Its conclusion only holds for an unwrapped `vitest related`. The trade is real: a wrong wrapper fails open and every path reads covered, while risk's parser fails closed. To flip: restore risk's t-5 and its zero-count contracts in t-13, and drop t-3's non-empty empty_exit rule.

**3. Whether vouch is under the lane grant.**
- risk, verification: fold it into LaneLine under a new frame, so flipping it stales the grant.
- mvp: neither field enters the consent line; `lane edit --vouch` keeps the lane GRANTED.
- Default: bound (t-6).
- Why it matters: `.dross/` commits are ungated, so without this an agent can widen its own gate in one commit, while `dross gate off` is human-only by design. Cost: the gate gains a consent read through localstore on every commit in an opted-in repo.

**4. Human grant for test_exempt.**
- risk: a separate grant over the sorted globs (its t-8), and calls it the most cuttable item.
- verification: validate only. mvp: validate for compilable globs only.
- Default: included as t-8, following the include-first rule.
- Why it matters: without it, `test_exempt = ["src/**"]` passes validate's catch-all check, and nothing runs validate at commit, so an agent can exempt the code it just wrote. Cutting t-8 removes 5 files, one wave-3 dependency and the grant cases in t-14, and leaves that hole open.

**5. What "build and dependency config stays refused" means.**
- risk: a fixed list in the classifier beats both coverage and exempt.
- verification: a validate fence on exempt globs only, and rejects a runtime denylist because it overrides a vouching lane's match.
- mvp: no list; a repo may exempt package.json itself, and flags t-4 gains a list if the lock means unexemptable.
- Default: risk (t-4). Verification's refusal of `*.json` at validate is not grafted, since runtime precedence already refuses it; t-3 keeps only catch-all and malformed-glob checks.
- Why it matters: only risk's reading stops `src/vite.config.ts` being vouched for by a `vitest related` hit, which is the false green the lock's rationale names. It does put a fixed file list in dross, and the list will need extending as stacks appear.

**6. `--force-unverified` against the c-6 ship check.**
- risk, verification: warns and proceeds, matching gateFreshness and the `stale_override` lock.
- mvp: stays refused, because the flag "is about the verdict".
- Default: warn (t-12).
- Why it matters: mvp's premise is out of date, since `gateFreshness` already takes the flag (ship.go:233). But c-6 says "refuse" flatly, so a human-visible bypass exists. green.json is machine-local, so a hard refusal costs a full run when shipping from a second machine.

**7. Coverage across runs on one tree.**
- verification: same-tree runs union Covered, so a follow-up for one missed path keeps the rest.
- risk, mvp: one record, replaced per run.
- Default: replace (t-13).
- Why it matters: c-2 says "a green run". Union invites a stale half surviving a later red. Replace means every retry re-pays every path, and under Disagreement 1's per-path spawn that cost is stack boots.

**8. How .svelte constructs are found.**
- risk, verification: svelte/compiler via astspan, per-node markup constructs, `svelte` added to the fixture.
- mvp: a pure Go splitter, astspan untouched, one `SvelteMarkup` construct per run of lines between blocks, no fixture change.
- Default: compiler (t-5).
- Why it matters: Stryker finds its ranges through svelte's own `preprocess` and `parse`, so a Go splitter can disagree on `<!-- <script> -->`, and a line error silently drops mutants. mvp's coarse markup span widens every markup hunk to the whole run, which sits badly with c-8's "only the constructs overlapping its hunks". mvp's version costs 3 files and no lockfile or `npm audit` exposure.

**9. Separate real-tool proof for svelte ranging.**
- risk: its own task (t-11) through planner and Stryker.
- verification: folded into the construct task. mvp: `requireE2E` on Constructs only, no Stryker run.
- Default: separate (t-11), which also keeps t-5 from growing.
- Why it matters: only a real Stryker run shows the range and the mutant locations agree. Folding saves a task at the price of a larger one.

**10. Where t-1 sits.**
- mvp: alone in wave 1, with every later task depending on it, because no other commit can get a full green first.
- risk: wave 1, runs first by position. verification: wave 1, no ordering stated.
- Default: risk, first in wave 1.
- Why it matters: `NextRunnable` gives the same order with no extra wave. The gap is narrow: `dross task move`, or t-1 going `failed` (the picker skips it), would let a later task start and hit a commit it cannot land. A `depends_on` on t-1 for wave-1 tasks closes that for one line each, at the cost of mvp's 5-wave shape.

**11. Journey test between recorder and gate.**
- risk, verification: a dedicated e2e task, because each side passes against a seeded record and a path-spelling mismatch would go unseen.
- mvp: none; t-3 pins `Scoped.Tree == Candidate` and leaves the rest to t-8's real run.
- Default: included (t-15).
- Why it matters: without it, the first real recorder-to-gate read happens in barktime at wave 5, the most expensive place to find a `./` bug.

**12. `dross test lane preview` for vouching lanes.**
- risk: units flow through preview (t-9). mvp, verification: left out, both flagging it as a candidate.
- Default: included, following the include-first rule.
- Why it matters: leaving it breaks preview's "the line it prints is the line that runs". It accounts for 3 of t-9's 5 files, so cutting it also brings t-9 under the size line.

**13. Prompt task shape, and whether quick §4 keeps bare.**
- mvp: one 10-file task, arguing a split leaves a red commit. Keeps the bare fence as a branch.
- risk, verification: split by prompt family. Risk replaces quick §4 in place; verification keeps the bare branch.
- Default: split, with bare kept as a branch (t-16, t-17).
- Why it matters: mvp's red-commit worry does not hold if bare stays, since the audit passes either way. Risk's in-place replace would need t-16's audit change first and is a hidden dependency. It would also leave a lane-less repo with a commit c-4 refuses, which is why verification's contract is in t-17.

**14. One task or two for c-6.**
- risk, mvp: verify and ship in one task. verification: two, ship after verify.
- Default: two (t-10, t-12).
- Why it matters: size, mostly. It adds a wave-3 edge, and the wiring tests split with it.

**15. A CLI verb for test_exempt.**
- verification: `dross project set runtime.test_exempt` (internal/cmd/project.go).
- risk, mvp: hand-edited project.toml; mvp says so outright.
- Default: left out. Adding it would push t-3 to 5 files and 2 layers, and t-8's grant is the control that matters.
- Why it matters: this is the one place the default departs from include-first, so the user may want it back. Barktime's quick would hand-edit the field.

**16. c-5 evidence artifact.**
- risk: a `.md` proof file. mvp: `c5-evidence.toml`, with barktime paths in `files`. verification: `barktime-proof.toml`, evidence file only.
- Default: verification's name and shape (t-18).
- Why it matters: mvp's absolute paths would fail plan validation, and a toml record is checkable by field. Barktime's wrapper script lands through its own /dross-quick, as `barktime_proof` requires.
