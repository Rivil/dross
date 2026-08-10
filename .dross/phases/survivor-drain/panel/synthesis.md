# survivor-drain — panel synthesis

## Scores

| dimension | draft | assessment |
|---|---|---|
| criteria coverage | risk | All 7 criteria hit, but c-1's locked repo-wide `drain_scope` is only *asserted* — the never-mutated packages have no drain task; whatever the sweep finds there lands in t-11 "accept the residue", which is the laundering path the same draft argues against. |
| criteria coverage | mvp | All 7 hit; repo-wide scope handled by one catch-all clause inside t-9 ("plus every survivor the scan reports in packages no phase has mutated yet"), whose size the draft itself admits is unknown. |
| criteria coverage | verification | All 7 hit, and the only draft that plans the repo-wide scope as work: four sweep tasks over named package sets (t-15…t-18) covering ~25 never-mutated packages. c-3 additionally gets a repo-invariant reason audit. |
| test-contract specificity | risk | Best *form* — every contract is written as "if this mutation lands, TestX fails", which is the shape c-4/c-6 actually need; slightly thinner on assertion mechanics (what exact string, what exact fixture). |
| test-contract specificity | mvp | Contracts name real test identifiers and real line numbers, but several read as "fails if X changes" without naming the observable (e.g. TestStatusFooterCountsTasks "if the arithmetic changes"). |
| test-contract specificity | verification | Best *substance* — n-1/n/n+1 boundary triples, byte-identical file compares, exact skip strings, "an inverted guard changes which message is printed". Contracts are written as the acceptance test, not as a description of one. |
| granularity | risk | Cleanest sizing on the kill work (3 tasks × 4-5 test files, split by call-surface), but t-11 "accept the residue" is unbounded and t-6 hides a whole new command plus repo enumeration in one task. |
| granularity | mvp | Weakest: t-6 carries 35 survivors across 6 files, t-9 is explicitly scan-determined and open-ended. 10 tasks is under-decomposed for ~91 routed items plus a repo-wide sweep. |
| granularity | verification | Mostly right: drain tasks are 2-5 files each and clustered by seam kind. t-13 (five single-guard files) is thin and the four sweep tasks exceed the file guidance, both consciously argued. |
| wave correctness | risk | 6 waves, dependencies all real, and the one ordering insight the others miss (acceptance strictly after kills). Over-serialized: t-12 `reconcile` is made to wait on t-6 `sweep` for no dependency that exists. |
| wave correctness | mvp | 3 waves with a genuine ordering bug: t-6…t-9 accept survivors "via the ceiling category" while depending only on t-1 — the category's proof test is t-5, which they do not wait for. |
| wave correctness | verification | 3 waves, correct: instruments in wave 1, every drain task depends on both t-1 (the gate) and t-5 (the proof + audit), closure last. Wave 2 is a 12-task flat fan-out, which is honest — those tasks touch disjoint test files. |

**Skeleton: `verification.md`.** It is the only draft where the locked `drain_scope` decision
is planned rather than promised, its contracts are executable as written, and its wave
dependencies are the only ones that hold up. `risk.md` is the strongest runner-up and supplies
four grafts below; `mvp.md` contributes two.

## Merged plan

Phase survivor-drain — 20 tasks across 4 waves

```
Wave 1 — instruments (no deps)

  m-1  Add `dross survivor drain` unclassified gate           [verification+risk+mvp]
       files:    internal/cmd/survivor_drain.go, internal/cmd/survivor_drain_test.go,
                 internal/cmd/survivor.go, README.md
       covers:   c-1, c-2
       desc:     New `survivor drain` subcommand. Reads one or more gremlins JSON reports
                 (--report) or runs the configured adapter over --packages, classifies every
                 survivor through verify.Classify using acceptedReasons() + routedSurvivors(),
                 prints the ones with no disposition, returns an error when any remain. No diff
                 scoping. A survivor routed to the phase being drained is reported outstanding,
                 never silenced.
       contract: - a report survivor absent from survivors.toml and from every [[deferred]]
                   makes the command error, naming `internal/x/y.go:12 (CONDITIONALS_NEGATION)`;
                   the same survivor with an [[accepted]] entry exits 0 printing "0 unclassified"
                 - a survivor whose deferred entry targets the phase being drained
                   (state.CurrentPhase, or --phase) counts outstanding; retargeted to another
                   phase it reports `routed` and exits 0 — the drain cannot be closed by
                   re-routing to itself
                 - a dismissed deferred entry counts outstanding (dismissal is triage, not a
                   disposition)
                 - [graft risk t-6] a package the adapter wrote no usable report for fails the
                   run and is named: TestDrainFailsOnUnmeasuredPackage — an unmeasured package
                   must never look like a package with zero survivors
                 - [graft risk t-6] with --packages omitted the dispatched set equals
                   `go list ./...` for the fixture repo: TestDrainCoversEveryGoPackage
                 - an empty report set exits 0 rather than erroring on "nothing to do"

  m-2  Add `dross survivor retire` for stale acceptances      [verification+risk+mvp]
       files:    internal/survivor/store.go, internal/survivor/store_test.go,
                 internal/cmd/survivor.go, internal/cmd/survivor_test.go, README.md
       covers:   c-7
       desc:     Store.Remove(key) plus survivor.Retire(path, key) (load → remove → atomic
                 save), and `survivor retire <key>...` with --stale to retire everything
                 StaleAcceptances reports as Stale.
       contract: - retiring an absent key returns "no acceptance with key <k>" and leaves
                   survivors.toml byte-identical (compare file bytes, not entry count)
                 - retiring 1 of 3 keys leaves the other two [[accepted]] blocks and every
                   [[category]] block present, and the store still Loads (validate() passes)
                 - [graft risk t-5] retiring the last member of a category drops the orphaned
                   [[category]] block: TestRetireDropsOrphanedCategory
                 - `--stale` retires exactly Report.Stale; an entry in Report.Unverifiable
                   (unreadable file) is still present afterwards — "I could not look" must never
                   retire a live acceptance
                 - a failed retire inside a multi-key invocation writes nothing at all

  m-3  Kill the gremlins skip-message seams                   [verification] (+risk, mvp)
       files:    internal/mutation/gremlins.go, internal/mutation/gremlins_test.go
       covers:   c-4
       desc:     Collect the per-package skip reasons Run() currently only prints to stderr into
                 a Gremlins.Unmeasured slice (still printed), and drive both branches through
                 the gremlinsBuildCmd seam.
       contract: - a malformed-JSON report produces exactly
                   `./a (unreadable report: <parse error text>)` as an Unmeasured element —
                   exact string compare, so mutating the concat at gremlins.go:155 fails
                 - an all-NOT_COVERED report produces exactly
                   `./b (zero covered mutants — coverage blind spot)` (gremlins.go:166) and
                   contributes no file rows to the merged report
                 - a readable, covered report appears in neither Unmeasured nor the skip output,
                   and its rows do reach the merge
                 - [graft risk t-2] a package with no report at all is a skip, not a run
                   failure: TestMissingReportIsSkipNotError (m-1 is where absence turns fatal)

  m-4  Kill the stryker + stryker_net error seams             [verification] (+risk, mvp)
       files:    internal/mutation/stryker_test.go, internal/mutation/stryker_net_test.go,
                 internal/mutation/testdata/stryker-unknown-status.json
       covers:   c-4
       desc:     Tests driven through strykerBuildCmd / strykerNetBuildCmd against a temp
                 ProjectRoot: no report written, report path is a directory, malformed JSON,
                 unrecognised mutant status, and buildCmd with and without Prefix.
       contract: - no report on disk → error names the expected path and says "did not write a
                   report"; report path replaced by a directory → error begins
                   "read stryker report:" — inverting the fs.ErrNotExist check (stryker.go:73)
                   fails one of the two
                 - a non-JSON body makes Run return the ParseStrykerJSON error and a nil report,
                   so the `if err != nil` after parsing (stryker.go:80) cannot be dropped
                 - a report whose only mutant status is "Quantum" parses to Killed=0, Errors=1
                   and a per-file stat of Errors=1; flipping the default arm's `r.Errors++`
                   (stryker.go:263) to `--` yields -1 and fails the equality assert
                 - buildCmd with Prefix "" yields Args identical to the args passed in; with
                   Prefix "docker run" yields ["docker","run", …args] — stryker_net.go:221
                   cannot be inverted without failing one

  m-5  Prove the attribution ceiling; require cited evidence  [verification+risk+mvp]
       files:    internal/mutation/ceiling_test.go,
                 internal/mutation/testdata/ceiling/ceiling.go,
                 internal/mutation/testdata/ceiling/ceiling_test.go,
                 internal/mutation/testdata/ceiling/gremlins-report.json,
                 internal/survivor/reasons_repo_test.go, .dross/survivors.toml
       covers:   c-3, c-5
       desc:     A fixture package holding a switch-case condition, a const initializer and a
                 string-concat return. The proof test runs `go test -coverprofile` over the
                 fixture, asserts the switch-case line has count>=1, then reads the recorded
                 gremlins report for the same fixture and asserts that line is NOT_COVERED. A
                 second test audits .dross/survivors.toml: every own-reason and every category
                 reason must name a Go test that exists in the repo. The
                 gremlins-attribution-ceiling category prose is rewritten to cite this test.
       contract: - the coverage profile reports count>=1 on the switch-case line; if the fixture
                   test stops executing it, the proof fails rather than silently proving nothing
                 - the recorded report marks that same file:line NOT_COVERED, asserted by line
                   and status, so re-recording it as COVERED fails
                 - the report contains an ARITHMETIC_BASE mutant on the string-concat line —
                   the evidence m-10 cites
                 - [graft risk t-4] that mutant is additionally shown to be a no-op:
                   TestStringConcatArithmeticMutantIsNoOp — the swap produces identical output,
                   so the bogus-arithmetic category rests on a demonstrated inapplicability, not
                   on the report's silence
                 - the reason audit fails, naming the offending key, for a synthetic store whose
                   reason cites no TestXxx and for one citing `TestThatDoesNotExist`; against
                   the real .dross/survivors.toml it passes

  m-6  Gate the verdict on unclassified in-scope count        [verification+risk+mvp]
       files:    internal/verify/verify.go, internal/verify/verify_test.go,
                 assets/prompts/verify.md, internal/cmd/verify_prompt_test.go
       covers:   c-6
       desc:     Add Summary.UnclassifiedInScope — in-scope survivors that are neither accepted
                 nor routed, i.e. len(InDiff) + in-scope Unclassified — emit each as BLOCKING
                 instead of FLAG, and rewrite verify.md's verdict rules so the mutation fail
                 lever is that absolute count, the score reported but no longer a threshold.
                 Run `make install` (r-01).
       contract: - one in-scope survivor in state `in-diff` gives UnclassifiedInScope==1 and a
                   BLOCKING finding naming `file:line (op)`; the same survivor `accepted` gives
                   0 and no BLOCKING
                 - an in-scope `routed` survivor leaves the count at 0 and keeps its NOTE
                 - a fixture with mutation_score 0.95 and one unclassified in-scope survivor
                   still emits BLOCKING — a high ratio can no longer buy a pass
                 - [graft risk t-1] an in-scope survivor whose identity failed to resolve still
                   blocks: TestUnresolvedInDiffIdentityStillBlocks — an unparseable key must not
                   fall out of the count
                 - [graft risk t-1] the count excludes out-of-scope unclassified survivors:
                   TestUnclassifiedInScopeExcludesOutOfScope — otherwise the standing backlog
                   fails every phase in the repo
                 - the prompt test asserts the measured branch's `fail` bullet names
                   `unclassified_in_scope` and that no verdict branch still lists "< 0.60" or
                   "≥ 0.80" as a pass/fail condition

Wave 2 (depends m-1, m-5)

  m-7  Derive kill-vs-accept evidence per survivor            [risk]
       files:    internal/survivor/evidence.go, internal/survivor/evidence_test.go,
                 internal/cmd/survivor_drain.go, internal/cmd/survivor_drain_test.go
       covers:   c-3
       desc:     For each unclassified survivor m-1 reports, attach two derived facts — does a
                 go-cover profile show count>=1 for its line, and can its operator alter that
                 line's source text — and print the pair, so kill-vs-accept is answered by code
                 rather than by per-item judgement across ~91 near-identical entries.
       contract: - operator applicability is derived from the line, not asserted:
                   TestArithmeticOnStringConcatIsInapplicable — ARITHMETIC_BASE over a line
                   whose only `+` joins string operands reports inapplicable, over an integer
                   expression reports applicable
                 - a missing profile is `unknown`, never `not covered`:
                   TestMissingProfileIsUnknownNotUncovered — unknown coverage must never read as
                   evidence for acceptance
                 - a line with count 0 is not ceiling-eligible:
                   TestCeilingEligibilityRequiresCoverage

Wave 3 — drain (each depends m-7)

  m-8  Drain doctor.go and hints.go survivors                 [verification] (+risk t-8/t-9, mvp t-6)
       files:    internal/cmd/doctor_test.go, internal/cmd/hints_test.go, .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     17 routed survivors (doctor.go 93/95/98/164/248/251×2/358/360/362/391,
                 hints.go 86×2/88×2/133×2), reached through each command's RunE with a fixture
                 repo and killed; accepted only where m-7 marks the entry ceiling-eligible or
                 operator-inapplicable.
       contract: - `dross survivor drain --report reports/gremlins/internal-cmd.json` reports
                   zero outstanding for doctor.go and hints.go
                 - each kill is a boundary test, not a smoke test: doctor.go:251 and
                   hints.go:86/88/133 are CONDITIONALS_BOUNDARY sites, so each test asserts at
                   n-1, n and n+1 — a `>=`→`>` mutation changes one of the three
                 - [graft risk t-9] TestDoctorReportsStaleBinary: a binary older than its source
                   is reported, an equal-mtime one is not
                 - every acceptance passes the m-5 reason audit and matches its m-7 evidence

  m-9  Drain footer_coverage, validate, repair, statusline    [verification]
       files:    internal/cmd/footer_coverage_test.go, internal/cmd/validate_test.go,
                 internal/cmd/repair_test.go, internal/cmd/statusline_test.go,
                 .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     footer_coverage.go 52/55/58/60, validate.go 92/96, repair.go 131/133,
                 statusline.go 189 — all CONDITIONALS_NEGATION guards, killed through the public
                 command surface; accepted only where the guard is unreachable through the CLI,
                 with the unreachability named.
       contract: - `dross survivor drain` reports zero outstanding for those four files
                 - each of footer_coverage.go's four guards has a test asserting BOTH sides of
                   the condition, so negating any one flips exactly one assertion; [graft risk
                   t-8] each band renders its own distinct footer string, so no two bands can be
                   swapped undetected
                 - [graft risk t-9] TestValidateSeparatesMissingFromMalformed — validate.go
                   92/96 must keep discriminating its two failure shapes
                 - any acceptance states which caller makes the branch unreachable and cites the
                   test pinning that caller — the m-5 audit fails otherwise

  m-10 Accept the string-concat ARITHMETIC_BASE set as one category   [verification+risk+mvp]
       files:    .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     Define the bogus-arithmetic category once (reason cites m-5's proof test) and
                 accept the ~14 ARITHMETIC_BASE survivors on string concatenation and const
                 initializers — gitignore.go 17/46/47/48/64/65/66, trust.go 279/280/281,
                 basebranch.go 104, statusline.go 23, status.go 444,
                 milestone_dependents.go 160 — via `dross survivor accept --op ARITHMETIC_BASE
                 --category`. Entry set is confirmed against m-7's applicability output, not
                 taken on the line list alone.
       contract: - `dross survivor list` shows every key resolving to the shared category prose,
                   and survivors.toml holds exactly one [[category]] block for it
                 - the category reason cites m-5's proof test by name, so the audit fails if the
                   prose is later replaced with an assertion
                 - [graft risk t-11] TestEveryCategoryAcceptanceMatchesItsEvidence re-derives
                   m-7's evidence per entry: an acceptance whose line is uncovered and whose
                   operator is applicable fails
                 - [graft risk t-11] TestNoAcceptedKeyIsAmbiguous — an ambiguous key never
                   suppresses, so it would leave the drain red while the store looks drained
                 - `dross survivor drain` reports zero outstanding ARITHMETIC_BASE survivors in
                   internal/cmd, and the adapter still emits them (this phase accepts, it does
                   not filter — the filter is mutation-score-truth's contract)

  m-11 Drain status.go and ship.go survivors                  [verification] (+risk t-8/t-10)
       files:    internal/cmd/status_test.go, internal/cmd/ship_test.go, .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     status.go 442×2/444×2/472/474 (BOUNDARY+NEGATION on the same lines), ship.go
                 264/550/552/554 (negation guards in the merge-gate path).
       contract: - `dross survivor drain` reports zero outstanding for status.go and ship.go
                 - status.go:442/444's boundary tests pin the exact comparison: at the boundary
                   value one string renders, one past it the other, so `>` vs `>=` is observable
                   in stdout
                 - [graft risk t-10] TestShipMergeGateRejectsEachFailingState — ship.go
                   550/552/554's three rejection states are each reported by their own name, so
                   negating any one changes which message is printed

  m-12 Drain phase, phase_lifecycle and watch survivors       [verification] (+risk t-8/t-10)
       files:    internal/cmd/phase_test.go, internal/cmd/phase_lifecycle_test.go,
                 internal/cmd/watch_test.go, .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     phase.go 308/310/768, phase_lifecycle.go 23/25/27, watch.go 130
                 (BOUNDARY + NEGATION on the same line).
       contract: - `dross survivor drain` reports zero outstanding for those three files
                 - phase_lifecycle.go 23/25/27 gain one test per guard driving insert/move/rename
                   argument validation to its failure message — each negation makes a rejected
                   input succeed; [graft risk t-10] the three arms must produce distinct
                   messages, so swapping two is caught
                 - watch.go:130's pair is killed at boundary and past boundary (same fixture at
                   n and n+1), not by a single call

  m-13 Drain env, state, update, project, milestone_dependents  [verification] (+risk t-9, mvp t-8)
       files:    internal/cmd/env_test.go, internal/cmd/state_test.go,
                 internal/cmd/update_test.go, internal/cmd/project_test.go,
                 internal/cmd/milestone_dependents_test.go, .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     env.go 105/109/114/119, state.go 270, update.go 181/184, project.go 81/83,
                 milestone_dependents.go 158/160 (the ARITHMETIC_BASE on 160 goes to m-10).
       contract: - `dross survivor drain` reports zero outstanding for those five files
                 - env.go's four guards are covered by a table test of set/unset/empty/invalid
                   for one variable asserting a distinct message per case — negating any single
                   guard collapses two cases onto the same message
                 - [graft mvp t-8 / risk t-9] update.go 181/184's signature-verification
                   comparison is pinned in both directions: an already-current version skips and
                   an unsigned release is rejected
                 - milestone_dependents.go:160's INVERT_NEGATIVES is killed by a genuinely
                   negative operand, not merely a zero one

  m-14 Drain the single-guard command files                   [verification]
       files:    internal/cmd/install_test.go, internal/cmd/rule_test.go,
                 internal/cmd/hooks_test.go, internal/cmd/issue_test.go,
                 internal/cmd/root_test.go, .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     One CONDITIONALS_NEGATION guard each: install.go 65, rule.go 143, hooks.go 100,
                 issue.go 171, root.go 61. Killed through each command's own entry point;
                 accepted only with a named unreachable caller.
       contract: - `dross survivor drain` reports zero outstanding for those five files
                 - each guard's test asserts both outcomes through the command's RunE, not by
                   calling the helper with a hand-built struct that could never occur
                 - root.go:61's test drives the real cobra root command, so the guard is proven
                   reachable from `dross` itself

  m-15 Drain internal/verify/scope.go and internal/rules      [verification] (+risk t-10, mvp t-9)
       files:    internal/verify/scope_test.go, internal/rules/rules_test.go,
                 .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     scope.go 147/151/153/155 (BOUNDARY+NEGATION pairs) and 260, plus rules.go 104.
                 Scope is a pure type, so every one is reachable from a table test and must be
                 killed, never accepted.
       contract: - `dross survivor drain --packages ./internal/verify/... ./internal/rules/...`
                   exits 0
                 - scope.go's comparisons are pinned at their endpoints in both directions, so
                   both the boundary and the negation mutants on 147/151/153/155 die (see
                   Disagreement D9 on what those four lines are)
                 - [graft mvp t-9] TestParseHunksSkipsMalformedHeader — scope.go:260's guard
                   inverted must change which headers are skipped
                 - rules.go:104 is exercised one under, exactly at, and one over the boundary

  m-16 Sweep the leaf config packages                         [verification]
       files:    internal/argfence/argfence_test.go, internal/configenum/configenum_test.go,
                 internal/defaults/defaults_test.go, internal/hostallow/hostallow_test.go,
                 internal/profile/profile_test.go, internal/redact/redact_test.go,
                 .dross/survivors.toml
       covers:   c-1, c-3
       desc:     First half of the never-mutated packages the locked drain_scope decision pulls
                 in. Run the adapter per package, then kill or accept whatever it reports.
       contract: - `dross survivor drain --packages ./internal/argfence/... ./internal/configenum/...
                   ./internal/defaults/... ./internal/hostallow/... ./internal/profile/...
                   ./internal/redact/...` exits 0
                 - redact and hostallow survivors are killed, never accepted: both are
                   security-relevant matchers reachable from a pure unit test, so an acceptance
                   in either is a hole the phase would be signing off
                 - every acceptance passes the m-5 audit and matches its m-7 evidence

  m-17 Sweep the state and domain packages                    [verification]
       files:    internal/board/board_test.go, internal/changes/changes_test.go,
                 internal/hooks/settings_test.go, internal/milestone/milestone_test.go,
                 internal/state/state_test.go, internal/statusline/render_test.go,
                 internal/phase/phase_test.go, .dross/survivors.toml
       covers:   c-1, c-3
       desc:     Second sweep group: the packages holding .dross state and its rendering.
       contract: - `dross survivor drain --packages ./internal/board/... ./internal/changes/...
                   ./internal/hooks/... ./internal/milestone/... ./internal/state/...
                   ./internal/statusline/... ./internal/phase/...` exits 0
                 - state.go survivors on the save path are killed by round-trip tests
                   (write → read → compare every field), so a dropped field assignment is
                   observable rather than accepted as untestable
                 - the phase.go:190 slug-charset pair already accepted under the ceiling
                   category still resolves to that category after the sweep (no duplicate key,
                   no second reason)

  m-18 Sweep the analyzer catalog packages                    [verification]
       files:    internal/codex/ast_grep_test.go, internal/quality/catalog_test.go,
                 internal/security/catalog_test.go, internal/techdebt/run_test.go,
                 internal/stack/detect_test.go, internal/telemetry/taxonomy_test.go,
                 internal/findings/reconcile_test.go, .dross/survivors.toml
       covers:   c-1, c-3
       desc:     Third sweep group: catalog/analyzer packages, where most survivors sit on
                 table-driven selection logic and are killable by adding a case.
       contract: - `dross survivor drain --packages ./internal/codex/... ./internal/quality/...
                   ./internal/security/... ./internal/techdebt/... ./internal/stack/...
                   ./internal/telemetry/... ./internal/findings/...` exits 0
                 - catalog-selection survivors are killed by a case that selects a DIFFERENT
                   analyzer, so an inverted match is observable in the chosen tool name rather
                   than only in a count
                 - findings fingerprint/reconcile survivors are killed by a
                   same-finding/different-finding pair — a negated comparison merges or splits
                   the two, which the test asserts on

  m-19 Sweep the process and forge packages                   [verification]
       files:    internal/forge/github_test.go, internal/ship/basepr_test.go,
                 internal/update/update_test.go, internal/watch/drift_test.go,
                 internal/architecture/architecture_test.go, .dross/survivors.toml
       covers:   c-1, c-3
       desc:     Final sweep group: packages that talk to processes and the forge. Survivors on
                 real network/exec paths are accepted with the seam named; everything reachable
                 through an existing fake is killed.
       contract: - `dross survivor drain --packages ./internal/forge/... ./internal/ship/...
                   ./internal/update/... ./internal/watch/... ./internal/architecture/...`
                   exits 0
                 - any acceptance names the process or HTTP seam that makes the branch
                   unreachable AND cites the existing test proving that seam is the only way in
                   — "needs the network" alone fails the m-5 audit
                 - update.go signature-verification survivors are killed, not accepted: the
                   minisign verify path already has a local fixture, so an acceptance there
                   would silence the release-signing gate

Wave 4 (depends m-8 … m-19)

  m-20 Close the routed deferred backlog                      [verification+mvp+risk]
       files:    .dross/phases/survivor-lifecycle/spec.toml,
                 .dross/phases/mutation-diff-scope/spec.toml,
                 .dross/phases/dross-repair/spec.toml,
                 .dross/phases/completion-state-truth/spec.toml,
                 internal/cmd/survivor_backlog_repo_test.go
       covers:   c-1, c-2
       desc:     Walk `dross deferred list --target survivor-drain --json` and close each entry
                 with `dross deferred unroute <phase> <idx>` then `dross deferred dismiss
                 <phase> <idx>` — indices are stable because neither verb removes an element.
                 [mvp t-10] The four non-survivor items (completion-state-truth[1],
                 dross-repair[0], mutation-diff-scope[1] and [2]) are resolved by hand: the two
                 mutation-diff-scope asks are delivered by m-3/m-4/m-6, the bulk-backlog asks by
                 m-8…m-15. Then a full repo-wide `dross survivor drain` must exit 0.
       contract: - TestSurvivorDrainBacklogClosed fails if collectDeferred over the repo yields
                   any entry with Target=="survivor-drain" and Dismissed==false — enforced by
                   CI, not by a ship-time eyeball
                 - the same test fails if any survivor-keyed deferred entry is routed to a phase
                   that sits AFTER survivor-drain in the milestone's phases array — the
                   "none is re-routed forward" half of c-2
                 - after closure `dross survivor drain` still exits 0: dismissing a routed entry
                   drops it out of routedSurvivors(), so a survivor closed by dismissal alone
                   (never killed, never accepted) reappears as outstanding and fails the run
                 - `dross deferred list --dismissed` still shows each closed item with its
                   survivor key, so the drain is auditable after the fact
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 (repo-wide zero unclassified) | m-1, m-8 … m-19, m-20 |
| c-2 (routed backlog resolved, none re-routed) | m-1, m-8 … m-15, m-20 |
| c-3 (reasons concrete + checkable; reachable ⇒ killed) | m-5, m-7, m-8 … m-19 |
| c-4 (adapter error/process seams killed) | m-3, m-4 |
| c-5 (attribution-ceiling proof test) | m-5 |
| c-6 (absolute unclassified-in-scope gate) | m-6 |
| c-7 (CLI retirement of stale acceptances) | m-2 |

## Disagreements

**D1 — How the routed backlog is closed.**
risk builds a new `dross survivor reconcile --target` verb that clears a routed item only when
its survivor is provably gone or accepted, and refuses otherwise; mvp and verification both
explicitly reject a new verb and use the existing `deferred unroute` + `dismiss` pair.
*Provisional default:* unroute + dismiss (skeleton). Two lenses independently rejected the new
verb as scope the criteria do not ask for, and verification's own contract supplies the safety
net risk wanted — dismissal drops the entry out of routedSurvivors(), so an item closed without
a kill or an acceptance reappears as outstanding in m-1 and reddens m-20.
*Why it matters:* if that reappearance property does not actually hold in the classifier, the
default silently becomes "91 items dismissed as wontfix", which is exactly the false-green c-2
exists to prevent — and then risk's refusing verb is the right build.

**D2 — When acceptance may happen relative to killing.**
risk puts all acceptance in a single late wave (its wave 5), after every kill task, arguing
anything accepted earlier is an acceptance a test might have killed and nothing makes that
reversible. mvp and verification interleave: each drain task kills or accepts its own cluster.
*Provisional default:* interleaved (skeleton), constrained by m-7's derived evidence and m-5's
reason audit.
*Why it matters:* the interleaved shape is only safe because acceptance now requires machine
evidence. Drop m-7 (see D3) and risk's ordering becomes the only remaining defence against
"accept, ceiling" being applied to a killable survivor.

**D3 — Is kill-vs-accept decided by code or by contract?**
risk derives it per survivor (coverage count + operator applicability, its t-7) on the argument
that per-item judgement over ~30 near-identical ARITHMETIC_BASE lines converges on "accept".
mvp and verification decide it per task through written kill-first contracts, plus (in
verification) a repo audit that every reason cites a live test.
*Provisional default:* keep both — m-7 is grafted in as the only new task the skeleton lacks.
*Why it matters:* it is the single largest scope addition in this merge (a new package plus
drain wiring) and it inserts a serialization point: all twelve drain tasks now wait on it. If
the phase needs to shrink, m-7 is the first candidate to cut — but cutting it means adopting D2
risk-style ordering in exchange.

**D4 — Does this phase change gremlins.go?**
verification collects the per-package skip reasons into a `Gremlins.Unmeasured` field (a
production change) so they can be asserted exactly; risk and mvp keep the test at the stderr
skip line with no production edit.
*Provisional default:* verification's Unmeasured field.
*Why it matters:* a stderr-capture assertion shares a stream with streamed subprocess output and
is fragile under `-count=1`; but the field is adapter surface, and the spec's deferred list
sends adapter behaviour changes to mutation-score-truth. The line taken here is that collecting
a message is not filtering a mutant — if the executor disagrees, the fallback is the risk/mvp
stderr test and c-4 is still met.

**D5 — Shape of the repo-wide sweep.**
verification plans four sweep tasks over named package sets (~25 packages); mvp folds all
never-mutated packages into one open-ended clause inside a single task; risk plans no sweep
tasks at all — its repo-wide claim rests on the sweep command plus "accept the residue".
*Provisional default:* verification's four grouped tasks.
*Why it matters:* this is the largest unknown in the phase. Nobody has measured how many
survivors those packages hold, so all three sizings are guesses; the four-task split is the only
one that can absorb a flood by splitting further without re-planning the whole wave.

**D6 — Is an unmeasured package a failure or a skip?**
risk makes the sweep exit non-zero when any package produced no usable report, calling it the
cheapest way to fake a green drain. verification's drain contract exits 0 on an empty report set
and treats missing reports as the adapter's existing skip; mvp is silent.
*Provisional default:* risk's — grafted into m-1 as TestDrainFailsOnUnmeasuredPackage, while
m-3 keeps skip-not-error inside the adapter itself.
*Why it matters:* the two are in direct tension by design (adapter skips, drain fails), and if
gremlins turns out to skip a package routinely for benign reasons, m-1 goes permanently red and
the gate gets weakened under pressure — at which point c-1's repo-wide claim is unenforced.

**D7 — gitignore.go 46-48: killed or accepted?**
mvp kills them with TestEnsureGitignoreWritesExactBlock (an exact-block assertion on the three
appended lines); verification accepts all seven gitignore.go survivors into the bogus-arithmetic
category; risk does not name the file.
*Provisional default:* m-7's applicability output decides per line, and m-10's entry set is
confirmed against it rather than taken from the line list.
*Why it matters:* if any of those lines is a real string operation rather than a concat the
operator cannot alter, accepting it is precisely the laundering c-3 forbids — and mvp's
exact-block test is cheap enough that it should land regardless of which way the evidence falls.

**D8 — Ceiling proof fixture: live or recorded?**
verification builds an in-repo fixture package and runs `go test -coverprofile` over it live,
pairing that against a recorded gremlins report; mvp and risk record both halves as fixtures on
the argument that shelling out to gremlins needs it installed and takes minutes.
*Provisional default:* verification's — the coverage half live, the gremlins half recorded.
*Why it matters:* a fully recorded pair can go stale silently (it proves a contradiction that
held once); a fully live pair needs gremlins in CI. The split keeps the falsifiable half honest
at no install cost, and if the fixture test stops executing the switch-case line the proof fails
rather than proving nothing.

**D9 — What scope.go:147-155 actually is.**
risk describes those four lines as an extension table (its contract asserts each listed
extension is in-scope and an adjacent unlisted one is not); mvp and verification describe them
as hunk line-range comparisons (a hunk of [10,20] answering true at 10 and 20, false at 9 and
21).
*Provisional default:* the line-range reading (two lenses agree), with m-15's contract to be
confirmed against the source before the test is written.
*Why it matters:* it is the only place the three drafts contradict each other about what the
code under test does — one of the two contracts cannot be written as stated, and picking wrong
costs a rewrite of the task's whole test.

**D10 — What the new command is called.**
verification `dross survivor drain`, mvp `dross survivor scan`, risk `dross survivor sweep`.
All three agree on the placement (under `survivor`, not a `verify --all-packages` flag) and on
the behaviour.
*Provisional default:* `dross survivor drain`, matching the phase slug.
*Why it matters:* only for README, prompt text and the contracts above, all of which name the
verb literally — but every drain-task contract quotes the command, so the name has to be fixed
before wave 3 rather than after.
