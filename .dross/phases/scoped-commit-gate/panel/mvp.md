# scoped-commit-gate — mvp lens

```
Phase scoped-commit-gate — 8 tasks across 5 waves

Wave 1
  t-1  Sync README roadmap to recorded milestones
       files:    README.md, internal/cmd/readme_roadmap_test.go
       covers:   c-1
       does:     v1.8 heading (complete), its 4 open boxes ticked; v1.9 heading (active) with its
                 5 success_criteria; Azure DevOps line and the "not yet written" line gone. Extend
                 the test: a complete section holds no `- [ ]`, and an active section's box count
                 equals len(success_criteria).
       contract: README saying v1.8 (active) or v1.9 (planning) fails
                 TestReadmeRoadmapMatchesRecordedMilestones.
                 An unticked box under a (complete) heading fails the same test.
                 v1.9 listing 4 boxes against 5 recorded criteria fails the same test.
                 "Azure DevOps" anywhere in the roadmap section fails the same test.
       depends:  none

Wave 2 (depends t-1: the suite is red until t-1 lands, so no later commit can pass commit-green)
  t-2  Add vouch lane field and test_exempt globs
       files:    internal/project/project.go, internal/cmd/validate.go, internal/cmd/test_lane.go,
                 internal/cmd/validate_lane_test.go, internal/cmd/test_lane_edit_test.go
       covers:   c-2, c-3, c-9
       does:     `vouch` bool on TestLane, `test_exempt` []string on Runtime. validate refuses a
                 vouch lane with no empty_exit, and a test_exempt glob that does not compile.
                 `lane add|edit --vouch`; `lane list` prints it only when set. Neither field
                 enters the consent line.
       contract: vouch = true with an empty empty_exit fails `dross validate`, naming the lane.
                 test_exempt = ["docs/[" fails `dross validate`, naming the glob.
                 `lane edit x --vouch` keeps a granted lane GRANTED, and keeps its document position.
                 `lane edit x --vouch=false` removes the key from project.toml.
       depends:  t-1

  t-7  Resolve .svelte constructs from script blocks
       files:    internal/mutation/construct.go, internal/mutation/construct_test.go,
                 internal/mutation/construct_e2e_test.go
       covers:   c-8
       does:     Drop the `no parser for .svelte` refusal. A pure Go splitter finds each top-level
                 <script> block; each goes to the existing astspan spawn as source with everything
                 else blanked, so lines stay file-true and astspan.go is untouched. Each run of
                 lines between blocks becomes one `SvelteMarkup` construct, because Stryker also
                 places mutants in template expressions (svelte-parser getTemplateScriptRanges).
                 A block Babel refuses still wraps ErrASTUnavailable.
       contract: A .svelte with <script lang="ts"> holding two functions: a hunk in the second
                 yields only that function's span, on its true file lines
                 (TestSvelteConstructsAreFileTrue).
                 A hunk in markup yields the SvelteMarkup span, never a script construct.
                 <script module> plus <script>: constructs come back sorted and pairwise disjoint.
                 Unparseable script body: errors.Is(err, ErrASTUnavailable) with a position.
                 Against the real parser (requireE2E): Constructs on a .svelte returns no error,
                 so PlanRanges records no ast-unavailable reason for it.
       depends:  t-1

Wave 3 (depends t-2)
  t-3  Record per-path scoped green from vouching lanes
       files:    internal/cmd/test.go, internal/cmd/lane_plan.go, internal/gatestate/store.go,
                 internal/gatestate/store_test.go, internal/cmd/test_scoped_green_test.go
       covers:   c-2, c-9
       does:     gatestate.Scoped{Tree, Covered, At, Runner} in scoped.json with Load/Save/Clear;
                 Save refuses an empty tree or empty Covered. A vouching lane spawns once per path
                 (one laneRunLine each, prepare once); selectorMissCode decides per path; a miss or
                 a dropped path stays out of Covered. greenRecorder fingerprints before and after
                 and saves Scoped only on exit 0 with Covered non-empty; a red on the recorded tree
                 clears it. green.json is never written by this path.
       contract: Lane vouching a.ts and b.ts, b.ts exits the declared empty_exit: run exits 0,
                 scoped.json Covered == [a.ts] (c-9 — fails if the lane is spawned batched).
                 Every path a miss: exit 5, no scoped.json, and no bare line spawned
                 (zero_test_scoped_run: no auto-escalation).
                 Non-vouching lane green on the same files: no scoped.json, spawned line
                 byte-identical to today.
                 Tree edited during the run: nothing recorded, the moved path named.
                 Path deleted on disk: absent from Covered.
                 Scoped.Tree equals treefp.Candidate after `git add -A` (the commit-side tree).
                 A green scoped run leaves an existing green.json byte-unchanged.
       depends:  t-2

  t-6  Let task prompts commit on scoped green
       files:    assets/prompts/execute.md, assets/prompts/quick.md, assets/prompts/_execute.solo.md,
                 assets/prompts/_quick.solo.md, assets/agents/dross-task-writer.md,
                 assets/prompts/verify.md, internal/cmd/execute_prompt_test.go,
                 internal/cmd/quick_prompt_test.go, internal/cmd/prompt_commit_gate_test.go,
                 internal/cmd/verify_prompt_test.go
       covers:   c-7, c-6
       does:     §1f: a scoped green from a vouching lane is enough; the bare `dross test` fence
                 stays as the branch for a repo with no vouching lane, or a refusal naming uncovered
                 paths. quick §4 runs `dross test --files <touched-files>` with the same fallback.
                 Solo: main loop re-runs §1e's test after the writer, not a bare one. Writer: drop
                 "the main loop runs the full suite". verify.md: bare `dross test` before
                 `dross verify`. The codeAdds audit accepts `dross test --files` as tested.
       contract: execute.md §1f still saying `--files` "recorded nothing" fails
                 TestExecuteCommitAcceptsScopedGreen.
                 Dropping quick §4's `dross test --files` fails the quick prompt test.
                 _execute.solo.md or _quick.solo.md requiring a bare run after a writer or fix round
                 fails the solo prompt tests; the writer line naming "the full suite" fails too.
                 A code `git add` with neither form of test ahead of it still fails
                 TestPromptCommitsSatisfyGreenGate (ship.md bite check kept).
                 verify.md without a bare `dross test` line ahead of `dross verify <phase>` fails
                 the verify prompt test.
                 quick.md past 15000 bytes fails TestPromptByteBudget (180 bytes of headroom today,
                 so the rewrite must net-trim); a removed manifest row fails the conservation test.
       depends:  t-2

Wave 4 (depends t-3)
  t-4  Admit commits on covering scoped green
       files:    internal/gate/commit.go, internal/gate/writer.go, internal/gate/commit_test.go,
                 internal/gate/writer_test.go
       covers:   c-2, c-3, c-4
       does:     judgeGreen: a full-green tree match admits as today. With no vouching lane, every
                 existing branch stays byte-identical. With one: drop test_exempt paths via
                 testlane.Select; nothing left admits. scoped.Tree == snap.Tree and the rest within
                 Covered admits; otherwise refuse naming the uncovered paths (no record: all of
                 them; different tree: treefp.Diff of the two). Writer SubagentStop clears scoped
                 beside green.
       contract: Scoped green on the candidate tree covering every changed non-.dross path, no
                 green.json: admitted.
                 Same record, one changed path outside Covered: refused, refusal text contains that
                 path and no covered path (c-3).
                 File edited after the scoped run: refused, naming that file.
                 package.json changed with test_exempt = ["docs/**"]: refused, naming package.json.
                 docs/x.md only, test_exempt = ["docs/**"], no record at all: admitted.
                 No lanes, valid scoped.json planted: the existing refusal, verbatim (c-4).
                 Lanes declared, none vouching, scoped.json planted: same refusal.
                 No lanes, test_exempt = ["docs/**"], docs-only commit: refused (exempt needs an
                 opted-in lane).
                 Writer SubagentStop removes scoped.json (a writer's green certifies nothing).
       depends:  t-3

  t-5  Refuse verify and ship without full green
       files:    internal/cmd/full_green.go, internal/cmd/full_green_test.go, internal/cmd/verify.go,
                 internal/cmd/ship.go, internal/cmd/hermetic_env_test.go
       covers:   c-6
       does:     requireFullGreen compares gatestate.LoadGreen to treefp.Base (HEAD, .dross out).
                 Called from Verify before any detached dispatch or spawn, and from Ship beside
                 gateFreshness, outside the verdict branch. Silent outside git and in a repo with
                 no test_command and no lanes. A seam var, no-oped in TestMain, keeps ~70 existing
                 Verify()/Ship() fixtures untouched; only full_green_test restores it.
       contract: No green.json: `dross verify x` refuses naming bare `dross test`, and no adapter
                 spawns.
                 scoped.json on the HEAD tree and no green.json: verify refuses, ship refuses
                 (scoped never satisfies).
                 green.json for an older tree: both refuse.
                 green.json recorded on a clean tree: both proceed (WorkingTree == Base).
                 `ship --force-unverified` with no full green: still refused.
                 Ship refusal writes and pushes nothing, not even .dross bookkeeping.
                 Repo with no test_command and no lanes: no refusal.
       depends:  t-3

Wave 5 (depends t-3, t-4, t-6)
  t-8  Prove scoped commit on barktime
       files:    /Users/rivil/Development/barktime/.dross/project.toml,
                 /Users/rivil/Development/barktime/scripts/test/related.sh,
                 .dross/phases/scoped-commit-gate/c5-evidence.toml
       covers:   c-5
       does:     `make install` from this branch, then a /dross-quick in barktime: a wrapper that
                 runs `vitest related --run <path>` and exits a declared code when nothing was
                 collected, a vouching selector=path lane matching src/**/*.{ts,svelte} with that
                 code as empty_exit, and test_exempt for docs. Land one real task commit. The user
                 grants the lane in their own terminal (`dross trust --lane`); the agent cannot.
                 Record SHA and run-log path in c5-evidence.toml.
       contract: `dross test --files` on a .svelte with a related test: log shows only related test
                 files ran, and `git commit` is admitted with no `dross gate off commit-green`.
                 Same call on a .svelte with no related test: exit 5, no scoped.json — the failure
                 if the wrapper lets `vitest related`'s default pass-with-no-tests through.
                 Commit touching one covered and one test-less .svelte: refused, naming the second.
                 c5-evidence.toml SHA resolves in barktime (`git cat-file -e`) and its log exists.
       depends:  t-3, t-4, t-6
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-2, t-3, t-4 |
| c-3 | t-2, t-4 |
| c-4 | t-4 |
| c-5 | t-8 |
| c-6 | t-5, t-6 |
| c-7 | t-6 |
| c-8 | t-7 |
| c-9 | t-2, t-3 |

Locked decisions:

| Decision | Tasks |
|---|---|
| scoped_gate_optin | t-2, t-4 |
| uncovered_paths | t-2, t-4 |
| zero_test_scoped_run | t-3 |
| barktime_proof | t-8 |

## Judgment calls

- **c-9 mechanism:** chose one spawn per path on vouching lanes, read by exit code; rejected batched runs (one `vitest related a b` cannot say which path reached a test), output parsing (`selectorMissCode`'s comment says the output is never read, per locked empty_detection) and a new relate-command field (more surface). Cost is N runner startups per lane, still far under a 15-26 min full run.
- **vouch requires empty_exit:** chose a validate refusal; rejected allowing it, because without a declared miss code a no-test path exits 0 and reads as covered, which is the c-9 failure.
- **`vitest related` needs a wrapper:** vitest 4.1.11 sets `passWithNoTests ??= true` for `related`, so no tests exits 0, and `--passWithNoTests=false` exits 1, the same as red. Chose a barktime wrapper mapping "collected nothing" to its own code; rejected framework knowledge in dross (barktime_proof keeps per-framework selectors out). This is the phase's biggest c-5 risk.
- **Scoped record:** chose a separate scoped.json; rejected a flag on green.json, where a scoped run would overwrite the full green and break c-6. tamper-guard already covers all of .dross/gate/, so it needs no change.
- **Build config:** chose "refused unless the repo exempts it"; rejected a built-in denylist of package.json, lockfiles and vite.config. It is speculative structure, and those paths reach no lane and no default exempt. A repo writing `*.json` can exempt package.json by its own hand. If the locked "stays refused" means unexemptable, t-4 gains a short list.
- **Exempt scope:** chose that test_exempt applies only with at least one vouching lane; rejected applying it everywhere, because c-4 and "feastahead stays unchanged" pin no-lane repos to today's rule.
- **Exempt-only commits:** chose to admit them with no scoped record and no tree match; rejected requiring a run, which would defeat the docs-commit purpose of uncovered_paths.
- **Deleted paths:** chose uncovered, so a deletion needs a full green; rejected treating a dropped path as covered, because it reached no test and c-9 says exactly that.
- **Tree mismatch refusal:** chose to name treefp.Diff(candidate, scoped tree); rejected a separate partial-commit branch, since one list covers both a later edit and an unstaged omission.
- **No accumulation:** chose one scoped record, replaced per run; rejected unioning across runs, because the spec says "that exact tree" and a union needs invalidation logic.
- **c-6 location:** chose a CLI check in internal/cmd using treefp.Base; rejected a new hook gate (more machinery) and treefp.MeasuredTree (it drops ARCHITECTURE.md, which green.Tree includes, so the two never compare equal).
- **--force-unverified:** chose not to lift c-6; rejected a bypass, because it is about the verdict and the criterion says refuse. `dross test` is the way out.
- **Existing fixtures:** chose a seam no-oped in TestMain; rejected editing ~70 Verify()/Ship() call sites to plant a green.
- **Prompt fallback fence:** chose to keep the fenced bare `dross test` as a conditional branch; rejected deleting it, which would drop manifest rows (execute.md, quick.md) and rewrite most §1f ordering tests for no behaviour gain.
- **t-6 size:** kept 10 files in one task despite the 5-file guideline. It is one layer and one wording change, and the tests read across the base prompts and partials, so splitting leaves a red commit between halves.
- **t-1 ordering:** chose t-1 alone in wave 1; rejected wave-1 parallelism with t-2/t-7, because the README test is red now and no other commit can get a full green until t-1 is in the tree.
- **Left out, no criterion:** `dross test lane preview` still prints the batched line for a vouching lane, which is not the per-path lines that run. Also no docs for `--vouch` in README or `_options.lanes.md`, and no CLI verb for test_exempt (hand-edited). Preview is the one a judge may want back: it breaks preview's "the line it prints is the line that would run" property, and fixing it adds lane_preview.go to t-3.
