# risk lens — survivor-drain

Phase survivor-drain — 13 tasks across 6 waves

The failure this phase must not produce is a **green drain that is a lie**: 91 routed
items cleared, `survivors.toml` sixfold larger, and the standing debt still there —
either laundered into an acceptance that a test could have killed, or hidden in a
package the run never reached. Every task below owns exactly one way that happens.

```
Wave 1
  t-1  Gate verdict on unclassified in-scope survivors
       files:    internal/verify/lifecycle.go, internal/verify/verify.go,
                 internal/verify/lifecycle_test.go, internal/verify/verify_test.go,
                 assets/prompts/verify.md
       covers:   c-6
       desc:     Add Lifecycle.UnclassifiedInScope() = len(InDiff) + in-scope entries of
                 Unclassified; surface it as VerifySummary.UnclassifiedInScope and emit one
                 BLOCKING finding per such survivor. Rewrite verify.md §3 so the fail rule
                 reads that count, not the 0.60 ratio.
       contract: if the counter is widened to include out-of-scope unclassified survivors,
                 TestUnclassifiedInScopeExcludesOutOfScope fails — a run whose only
                 unclassified survivors are out-of-scope must report 0 and emit no BLOCKING.
                 if the in-diff bucket is dropped from the count, TestInDiffSurvivorIsBlocking
                 fails — one in-diff survivor must give unclassified_in_scope=1 and exactly
                 one BLOCKING finding.
                 if an in-scope survivor whose identity failed to resolve is skipped,
                 TestUnresolvedInDiffIdentityStillBlocks fails.
                 if verify.md still names a mutation ratio as the survivor gate,
                 TestVerifyPromptGatesOnAbsoluteCount fails (verify_prompt_test.go pattern).

  t-2  Test gremlins report-fault seams
       files:    internal/mutation/gremlins_test.go,
                 internal/mutation/testdata/gremlins_unparseable.json,
                 internal/mutation/testdata/gremlins_zero_coverage.json
       covers:   c-4
       desc:     Drive Gremlins.Run through the gremlinsBuildCmd seam with fake processes that
                 write no report, an unparseable report, and an all-NOT_COVERED report.
       contract: if the unreadable-report arm at gremlins.go:155 stops naming the parse error,
                 TestUnreadableReportSkipsPackage fails — it asserts the stderr skip line
                 contains "(unreadable report: " plus the package path.
                 if hasCoverage's zero-coverage arm at gremlins.go:166 is inverted or dropped,
                 TestZeroCoveredMutantsExcludedFromScore fails — an all-NOT_COVERED package
                 must contribute nothing to merged Killed/Survived.
                 if a package with no report starts failing the run instead of skipping,
                 TestMissingReportIsSkipNotError fails.

  t-3  Test stryker and stryker_net process seams
       files:    internal/mutation/stryker_test.go, internal/mutation/stryker_net_test.go,
                 internal/mutation/testdata/stryker_future_status.json
       covers:   c-4
       desc:     Cover stryker.go:73/80 (missing-report vs other read error), the
                 ParseStrykerJSON error return, the :263 future-status default arm, and
                 stryker_net.go:221 buildCmd with and without Prefix.
       contract: if the missing-report branch collapses into the generic read error,
                 TestStrykerMissingReportNamesPath fails — the message must contain
                 "did not write a report at" and the resolved path.
                 if the other-read-error branch is dropped, TestStrykerUnreadableReportErrors
                 fails (report path created as a directory).
                 if the default arm stops counting unknown statuses as errors,
                 TestFutureStrykerStatusCountsAsError fails — a "Quarantined" status must
                 yield Errors=1 and an error row in the per-file stats.
                 if buildCmd stops splitting Prefix on whitespace,
                 TestStrykerNetPrefixBecomesArgv0 fails — Prefix "docker compose exec app"
                 must give Args[0]=="docker" with the tool argv appended after.

  t-4  Prove both shared categories' claims
       files:    internal/mutation/ceiling_proof_test.go,
                 internal/mutation/testdata/gremlins_ceiling_report.json,
                 internal/survivor/category_evidence_test.go,
                 .dross/survivors.toml
       covers:   c-5, c-3
       desc:     One test pairs a recorded coverprofile (count>=1) for a switch-case condition
                 and a const initializer with a recorded gremlins report still marking those
                 lines NOT_COVERED. A second shows an ARITHMETIC_BASE swap on a
                 string-concatenation line is a no-op. Rewrite both category reasons to name
                 the proving test.
       contract: if the ceiling stops holding — gremlins begins attributing the switch-case
                 line — TestGremlinsCeilingIsReal fails on its NOT_COVERED assertion for a
                 line the paired profile shows executed.
                 if the coverage half is dropped so only the report is asserted,
                 TestGremlinsCeilingIsReal fails its count>=1 assertion.
                 if ARITHMETIC_BASE turns out to be applicable to string concatenation,
                 TestStringConcatArithmeticMutantIsNoOp fails.
                 if either category's reason stops naming a test that exists in the tree,
                 TestCategoryReasonsCiteAProofTest fails — it reads the repo's
                 .dross/survivors.toml and greps each cited identifier.

  t-5  Add `dross survivor retire`
       files:    internal/survivor/store.go, internal/survivor/store_test.go,
                 internal/cmd/survivor.go, internal/cmd/survivor_test.go
       covers:   c-7
       desc:     Store.Retire(key) plus Retire(path, key) read-modify-write, and a
                 `survivor retire <key>` subcommand with --all-stale driven by
                 StaleAcceptances. Drops a category when its last member goes.
       contract: if Retire matches on file or key prefix instead of exact key,
                 TestRetireRemovesOnlyTheNamedKey fails — a store with two keys in one file
                 keeps the other entry unchanged.
                 if retiring an absent key silently succeeds, TestRetireUnknownKeyErrors fails
                 — the command must exit non-zero and leave the file byte-identical.
                 if an orphaned category is left behind, TestRetireDropsOrphanedCategory fails.
                 if a rejected retire still rewrites the store,
                 TestFailedRetireLeavesStoreByteIdentical fails.
                 if --all-stale retires an Unverifiable entry,
                 TestRetireAllStaleSkipsUnverifiable fails — an unreadable file must never
                 retire the acceptances it holds.

Wave 2 (depends t-1)
  t-6  Add repo-wide `dross survivor sweep`
       files:    internal/cmd/survivor_sweep.go, internal/cmd/survivor_sweep_test.go,
                 internal/cmd/survivor.go
       covers:   c-1
       desc:     Enumerate every Go package in the repo, dispatch the configured adapters over
                 all of them with no diff scoping, classify with the same accepted/routed maps
                 verify uses, print per-state counts, and exit non-zero when any survivor is
                 unclassified OR any package produced no usable report.
       contract: if a package gremlins wrote no report for is counted as clean,
                 TestSweepFailsOnUnmeasuredPackage fails — the sweep must exit non-zero and
                 name the package, not fold it into a silent skip.
                 if enumeration narrows to packages holding changed files,
                 TestSweepCoversEveryGoPackage fails — the dispatched set must equal
                 `go list ./...` for the fixture repo.
                 if the sweep reuses verify's phase scope and buckets survivors as
                 out-of-scope, TestSweepDoesNotDiffScope fails.
                 if an accepted survivor still reddens the run,
                 TestSweepExitsZeroWhenAllClassified fails.

Wave 3 (depends t-6)
  t-7  Derive kill-vs-accept evidence per survivor
       files:    internal/survivor/evidence.go, internal/survivor/evidence_test.go,
                 internal/cmd/survivor_sweep.go, internal/cmd/survivor_sweep_test.go
       covers:   c-3
       desc:     For each unclassified survivor attach two derived facts — does a go-cover
                 profile show count>=1 for its line, and can its operator alter that line's
                 source text — and print the pair, so kill-vs-accept is answered by code.
       contract: if operator applicability is asserted rather than derived from the line,
                 TestArithmeticOnStringConcatIsInapplicable fails — ARITHMETIC_BASE over a
                 line whose only + joins string operands must report inapplicable, and over an
                 integer expression must report applicable.
                 if a missing profile degrades to "not covered",
                 TestMissingProfileIsUnknownNotUncovered fails — unknown coverage must never
                 read as evidence for acceptance.
                 if a line with count 0 is marked ceiling-eligible,
                 TestCeilingEligibilityRequiresCoverage fails.

  t-12 Add `dross survivor reconcile --target`
       files:    internal/cmd/survivor_reconcile.go, internal/cmd/survivor_reconcile_test.go,
                 internal/cmd/survivor.go
       covers:   c-2
       desc:     Clear every routed [[deferred]] item aimed at a phase whose survivor key is
                 now absent from a fresh classification or accepted in the store. Refuse to
                 clear one still live and unaccepted; never rewrite Target to another slug.
       contract: if reconcile clears an item whose survivor is still live and unaccepted,
                 TestReconcileRefusesLiveSurvivor fails and the spec must be byte-identical
                 after the run.
                 if reconcile retargets instead of retiring, TestReconcileNeverReroutesForward
                 fails — no cleared item may end with a non-empty Target.
                 if it addresses items by index while mutating the array so later indices
                 shift, TestReconcileClearsAllMatchesInOneSpec fails — all N routed items in
                 one spec must clear in a single run.
                 if a routed item carrying no survivor key is cleared,
                 TestReconcileLeavesNonSurvivorItems fails — the four non-survivor items
                 routed here must survive.

Wave 4 (depends t-7)
  t-8  Kill reachable survivors in cmd output paths
       files:    internal/cmd/hints_test.go, internal/cmd/footer_coverage_test.go,
                 internal/cmd/status_test.go, internal/cmd/statusline_test.go,
                 internal/cmd/watch_test.go
       covers:   c-1, c-2
       desc:     Tests pinning the boundary/negation conditions the evidence pass marks
                 covered-and-applicable in hints.go:86/88/133, footer_coverage.go:52-60,
                 status.go:442-474, statusline.go:189 and watch.go:130.
       contract: if hints.go:86's age boundary shifts by one unit,
                 TestHintAgeBoundaryIsInclusive fails — the hint must appear exactly at the
                 boundary and not one unit below.
                 if footer_coverage.go:52-60's band conditions are negated,
                 TestFooterCoverageBandsRenderDistinctly fails — each band must produce its
                 own footer string, so no two bands can be swapped undetected.
                 if status.go:442/444's threshold comparison is loosened,
                 TestStatusDriftThresholdBoundary fails.
                 if watch.go:130's boundary is inverted,
                 TestWatchDigestIncludesIssueAtBoundary fails.

  t-9  Kill reachable survivors in cmd gating paths
       files:    internal/cmd/doctor_test.go, internal/cmd/env_test.go,
                 internal/cmd/validate_test.go, internal/cmd/install_test.go,
                 internal/cmd/update_test.go
       covers:   c-1, c-2
       desc:     Tests for the negation conditions in doctor.go (93/95/98/164/248/251/358/360/
                 362/391), env.go (105/109/114/119), validate.go (92/96), install.go:65 and
                 update.go (181/184) that the evidence pass marks covered-and-applicable.
       contract: if doctor.go:251's staleness comparison flips,
                 TestDoctorReportsStaleBinary fails — a binary older than its source must be
                 reported and an equal-mtime one must not.
                 if env.go:105-119's per-variable arms collapse into one,
                 TestEnvReportsEachMissingVarSeparately fails — four missing-var states must
                 give four distinct messages.
                 if validate.go:92/96 stops separating its two failure shapes,
                 TestValidateSeparatesMissingFromMalformed fails.
                 if update.go:181/184's version comparison is negated,
                 TestUpdateSkipsWhenAlreadyCurrent fails.

  t-10 Kill reachable survivors outside internal/cmd
       files:    internal/verify/scope_test.go, internal/rules/rules_test.go,
                 internal/cmd/ship_test.go, internal/cmd/phase_lifecycle_test.go
       covers:   c-1, c-2
       desc:     Tests for scope.go:147/151/153/155/260, rules.go:104, ship.go:264/550/552/554
                 and phase_lifecycle.go:23/25/27 that the evidence pass marks
                 covered-and-applicable.
       contract: if scope.go:147-155's extension table drops or widens one arm,
                 TestScopeExtensionTableIsExact fails — each listed extension must be in-scope
                 and an adjacent unlisted one out.
                 if rules.go:104's boundary shifts, TestRuleSeverityBoundary fails.
                 if ship.go:550-554's merge-gate conditions are negated,
                 TestShipMergeGateRejectsEachFailingState fails — three rejection states must
                 each be reported by their own name.
                 if phase_lifecycle.go:23-27's arms are swapped,
                 TestPhaseLifecycleTransitionsAreDistinct fails.

Wave 5 (depends t-4, t-8, t-9, t-10)
  t-11 Accept the residue under the evidenced categories
       files:    .dross/survivors.toml, internal/survivor/category_evidence_test.go
       covers:   c-1, c-3
       desc:     Run `dross survivor accept --category` for every survivor the evidence pass
                 marks ceiling-eligible or operator-inapplicable, under the two proven
                 categories. Bespoke prose only for a genuinely unreachable defensive branch,
                 and only naming the guard that makes it unreachable.
       contract: if an acceptance lands whose line the profile shows uncovered and whose
                 operator is applicable, TestEveryCategoryAcceptanceMatchesItsEvidence fails —
                 it re-derives the evidence for each entry naming a category.
                 if an accepted key is ambiguous in its file, TestNoAcceptedKeyIsAmbiguous
                 fails — an ambiguous acceptance never suppresses, so it leaves the sweep red
                 while the store looks drained.
                 if a free-prose reason cites no test name and no unreachability guard,
                 TestBespokeReasonsNameACheckableGuard fails.

Wave 6 (depends t-11, t-12)
  t-13 Drain the routed backlog to empty
       files:    .dross/phases/survivor-lifecycle/spec.toml,
                 .dross/phases/mutation-diff-scope/spec.toml,
                 .dross/phases/dross-repair/spec.toml,
                 .dross/phases/completion-state-truth/spec.toml,
                 internal/cmd/deferred_test.go
       covers:   c-1, c-2
       desc:     Run `dross survivor reconcile --target survivor-drain`, hand-resolve the four
                 non-survivor routed items (the two mutation-diff-scope asks are delivered by
                 t-1/t-2/t-3; the two bulk-backlog asks by t-8/t-9/t-10), and confirm a full
                 sweep exits zero.
       contract: if any [[deferred]] entry still targets survivor-drain,
                 TestNoDeferredItemTargetsSurvivorDrain fails — it walks every
                 .dross/phases/*/spec.toml and asserts the target list is empty.
                 if an item is cleared by pointing it at a later phase instead,
                 TestNoDeferredItemTargetsSurvivorDrain still passes but
                 TestReconcileNeverReroutesForward (t-12) is what forbids it — the two
                 together are why "empty" cannot be bought by re-routing.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (zero unclassified repo-wide) | t-6, t-8, t-9, t-10, t-11, t-13 |
| c-2 (routed backlog resolved, none re-routed) | t-8, t-9, t-10, t-12, t-13 |
| c-3 (reasons concrete and checkable) | t-4, t-7, t-11 |
| c-4 (adapter seams killed by real tests) | t-2, t-3 |
| c-5 (attribution ceiling demonstrated) | t-4 |
| c-6 (absolute unclassified gate) | t-1 |
| c-7 (CLI retirement of stale acceptances) | t-5 |

## Judgment calls

- **Built a repo-wide `survivor sweep` command (t-6) instead of running gremlins by hand
  per package.** Rejected: a one-off manual sweep whose result is pasted into the phase
  record. c-1 and the locked `drain_scope` decision assert a property of *every* Go
  package; an unrepeatable manual run makes that property unverifiable the day after it
  is claimed, and never-mutated packages start accumulating again immediately.
- **The sweep fails on an unmeasured package, not just on an unclassified survivor.**
  Rejected: reusing the adapter's existing "skip and note" treatment. That treatment is
  right inside a diff-scoped verify and fatal here — a package gremlins writes no report
  for looks exactly like a package with zero survivors, which is the single cheapest way
  to fake a green drain.
- **Kill-vs-accept is decided by derived evidence (t-7), not by judgement per survivor.**
  Rejected: reading each of the ~91 survivors and deciding. With ~30 near-identical
  ARITHMETIC_BASE lines and a large switch-case cluster, per-item judgement converges on
  "accept, ceiling" for everything — the exact laundering c-3 forbids. Deriving coverage
  count and operator applicability makes the ceiling claim falsifiable per entry.
- **`reconcile` refuses to clear a live unaccepted survivor (t-12) rather than clearing
  and warning.** Rejected: unroute+dismiss, the existing two-verb path. Dismiss means
  wontfix and is someday-only, so draining 91 items through it needs 182 invocations and
  records the wrong state. A refusing verb makes "resolved" a checked fact and makes
  re-routing-forward impossible by construction, which is half of c-2.
- **The gate counts InDiff *plus* in-scope Unclassified (t-1).** Rejected: counting
  `Lifecycle.Unclassified` alone, which is the name-matching reading and would miss every
  ordinary in-diff survivor; also rejected counting all Unclassified, which pulls in the
  out-of-scope backlog and would fail every phase in the repo until the drain lands.
- **Acceptance runs last (wave 5), after all kills.** Rejected: accepting and killing in
  parallel per file cluster. Anything accepted before the kill pass is an acceptance that
  a test might have killed, and nothing in the store's design makes that reversible
  without noticing.
- **Split the kills into three tasks by call-surface (output / gating / non-cmd) rather
  than by file.** These clusters span 4-5 test files each, at the granularity ceiling —
  but each owns one behavioural surface, so a regression names its own failure. Splitting
  per file would give ~23 sub-10-minute tasks; merging them gives one 18-file task.
- **t-4 asserts the category reasons against the repo's own `.dross/survivors.toml`.**
  This couples a test to project state, which is unusual here. It is the only mechanism
  that stops a category reason from drifting back to asserted-not-proven prose after the
  proof tests exist, which is precisely what c-3 and the locked `bogus_arithmetic_class`
  decision demand.
