# survivor-drain — MVP lens

Phase survivor-drain — 10 tasks across 3 waves

Wave 1

  t-1  Add repo-wide `dross survivor scan`
       files:    internal/cmd/survivor.go, internal/cmd/survivor_test.go
       covers:   c-1
       description: New `survivor scan` subcommand: enumerate every .go file in the repo,
                 run configuredAdaptersFn unscoped (verify.RunScoped with a nil scope),
                 classify with verify.ApplyLifecycle against the store (acceptedReasons)
                 and routed deferred items (routedSurvivors), print killed/accepted/
                 routed/unclassified counts plus each unclassified `file:line (op)`,
                 and exit non-zero when unclassified > 0. Gated by requireExecConsent.
       contract: with configuredAdaptersFn stubbed to an adapter that reports one
                 survivor in a package no phase ever touched,
                 TestSurvivorScanCountsUntouchedPackages fails if the enumeration stops
                 walking packages outside changes.json;
                 TestSurvivorScanExitsNonZeroOnUnclassified fails if an unclassified
                 survivor stops producing a non-zero exit;
                 TestSurvivorScanSilentOnAcceptedSurvivor fails if a survivor whose key
                 is in .dross/survivors.toml is still counted as unclassified.
       depends:  —

  t-2  Gate verdict on unclassified in-diff count
       files:    internal/verify/verify.go, internal/verify/verify_test.go,
                 assets/prompts/verify.md, internal/cmd/verify_prompt_test.go
       covers:   c-6
       description: In Skeleton(), every in-scope survivor left at LifecycleInDiff (not
                 accepted, not routed) emits a BLOCKING finding instead of FLAG, and the
                 count is written to VerifySummary. In assets/prompts/verify.md, replace
                 the `mutation score < 0.60 → fail` / `0.60-0.80 → partial` rules with
                 "any unclassified in-scope survivor → fail"; run `make install` (r-01).
       contract: TestSkeletonBlocksOnUnclassifiedInDiffSurvivor fails if an in-diff
                 survivor with no acceptance and no route drops back to FLAG;
                 TestSkeletonNoBlockingWhenAllClassified fails if an accepted or routed
                 in-diff survivor starts emitting BLOCKING;
                 TestVerifyPromptHasNoScoreRatioGate fails if `< 0.60` reappears as a
                 fail rule in the embedded verify prompt.
       depends:  —

  t-3  Add `dross survivor retire` + Store.Remove
       files:    internal/survivor/store.go, internal/survivor/store_test.go,
                 internal/cmd/survivor.go, internal/cmd/survivor_test.go
       covers:   c-7
       description: Store.Remove(key) drops an acceptance (and reports whether it was
                 present); `survivor retire <file>:<line> --op` (or `--key`) resolves the
                 identity the same way accept does, removes the entry and saves atomically.
       contract: TestRemoveDropsAcceptanceByKey fails if the entry survives the save;
                 TestRetireUnknownKeyErrors fails if retiring a key not in the store
                 exits 0 instead of erroring;
                 TestRetireLeavesOtherAcceptancesIntact fails if removing one entry
                 rewrites or drops a sibling acceptance or its category.
       depends:  —

  t-4  Kill the mutation adapter error seams
       files:    internal/mutation/gremlins_test.go, internal/mutation/stryker_test.go,
                 internal/mutation/stryker_net_test.go
       covers:   c-4, c-3
       description: Real tests over the six named seams: gremlins.go:155 unreadable-report
                 and :166 zero-covered-mutants exclusion notes (via gremlinsBuildCmd stub
                 + planted report files), stryker.go report-read and ParseStrykerJSON
                 parse-error returns, stryker.go:263 future-status default arm,
                 stryker_net.go:221 buildCmd prefix seam.
       contract: TestGremlinsUnreadableReportIsUnmeasured fails if the malformed-report
                 package stops being excluded with an `(unreadable report: …)` note;
                 TestGremlinsZeroCoveredMutantsExcluded fails if a report whose mutants
                 are all NOT_COVERED starts counting into the score;
                 TestStrykerMissingReportErrorNamesPath fails if the ErrNotExist branch
                 stops naming the expected report path;
                 TestStrykerParseErrorPropagates fails if malformed report JSON returns a
                 nil error;
                 TestStrykerUnknownStatusCountsAsError fails if a status string outside
                 the known set stops incrementing r.Errors;
                 TestStrykerNetBuildCmdSplitsPrefix fails if a Prefix of
                 "docker compose exec app" stops becoming argv[0]="docker" with the
                 remaining words ahead of args.
       depends:  —

  t-5  Prove the gremlins attribution ceiling
       files:    internal/mutation/attribution_test.go,
                 internal/mutation/testdata/attribution_gremlins_report.json,
                 internal/mutation/testdata/attribution_coverage.out,
                 .dross/survivors.toml
       covers:   c-5, c-3
       description: Record a real gremlins report and the matching `go test -coverprofile`
                 profile for the same package as fixtures; the test asserts at least one
                 switch-case / const-initializer line is NOT_COVERED in the report while
                 the profile shows count >= 1 for that line. Update the
                 gremlins-attribution-ceiling category reason to cite the test by name.
       contract: TestAttributionCeilingIsReal fails if no line in the fixture pair is
                 simultaneously NOT_COVERED in the gremlins report and executed >= 1 time
                 in the coverage profile — i.e. if the shared category's premise stops
                 holding, the proof test goes red rather than the prose going stale.
       depends:  —

Wave 2 (depends t-1)

  t-6  Drain doctor/status/gitignore/hints/footer_coverage
       files:    internal/cmd/doctor_test.go, internal/cmd/status_test.go,
                 internal/cmd/gitignore_test.go, internal/cmd/hints_test.go,
                 internal/cmd/footer_coverage_test.go, .dross/survivors.toml
       covers:   c-1, c-2, c-3
       description: The 35 routed survivors in doctor.go (93/95/98/164/248/251/358/360/
                 362/391), status.go (442/444/472/474), gitignore.go (17/46/47/48/64/65/
                 66), hints.go (86/88/133), footer_coverage.go (52/55/58/60): each is
                 killed by an assertion on the exact rendered output, or accepted via
                 `dross survivor accept` with a CLI-unreachable reason or the ceiling
                 category. Clear each item's deferred entry (`deferred unroute` then
                 `dismiss`) as it is resolved.
       contract: TestEnsureGitignoreWritesExactBlock fails if any of the three appended
                 gitignore lines (46-48) changes or is dropped;
                 TestDoctorReportsStaleBinary fails if doctor's staleness comparison at
                 doctor.go:248-251 inverts;
                 TestStatusFooterCountsTasks fails if the status.go:442-444 arithmetic on
                 done/total tasks changes;
                 `dross survivor scan` reports unclassified = 0 for internal/cmd's
                 doctor/status/gitignore/hints/footer_coverage lines.
       depends:  t-1

  t-7  Drain ship/env/milestone/trust/phase/statusline
       files:    internal/cmd/ship_test.go, internal/cmd/env_test.go,
                 internal/cmd/milestone_dependents_test.go, internal/cmd/trust_test.go,
                 internal/cmd/phase_test.go, internal/cmd/phase_lifecycle_test.go,
                 internal/cmd/statusline_test.go, .dross/survivors.toml
       covers:   c-1, c-2, c-3
       description: The 23 routed survivors in ship.go (264/550/552/554), env.go (105/109/
                 114/119), milestone_dependents.go (158/160), trust.go (279/280/281),
                 phase.go (308/310/768), phase_lifecycle.go (23/25/27), statusline.go
                 (23/189) — killed by assertion or accepted with a checkable reason;
                 deferred entries cleared as each is resolved.
       contract: TestEnvMaskingRedactsValue fails if any of env.go:105-119's masking
                 conditions inverts;
                 TestTrustConsentPromptLines fails if the trust.go:279-281 consent text
                 changes;
                 TestShipPRBodySections fails if ship.go:550-554's body assembly drops or
                 reorders a section;
                 `dross survivor scan` reports unclassified = 0 for these seven files.
       depends:  t-1

  t-8  Drain the remaining internal/cmd files
       files:    internal/cmd/validate_test.go, internal/cmd/update_test.go,
                 internal/cmd/watch_test.go, internal/cmd/project_test.go,
                 internal/cmd/repair_test.go, internal/cmd/install_test.go,
                 internal/cmd/state_test.go, internal/cmd/basebranch_test.go,
                 internal/cmd/rule_test.go, internal/cmd/root_test.go,
                 internal/cmd/hooks_test.go, internal/cmd/issue_test.go,
                 .dross/survivors.toml
       covers:   c-1, c-2, c-3
       description: The remaining 17 routed cmd survivors — validate.go (92/96), update.go
                 (181/184), watch.go (130), project.go (81/83), repair.go (131/133),
                 install.go (65), state.go (270), basebranch.go (104), rule.go (143),
                 root.go (61), hooks.go (100), issue.go (171) — killed or accepted, with
                 each deferred entry cleared.
       contract: TestValidateReportsEachSchemaError fails if validate.go:92 or :96 stops
                 discriminating the two error classes;
                 TestUpdateRejectsUnsignedRelease fails if update.go:181/184's signature
                 checks invert;
                 TestBaseBranchFallsBackToMain fails if basebranch.go:104's fallback
                 string changes;
                 `dross survivor scan` reports unclassified = 0 for these twelve files.
       depends:  t-1

  t-9  Drain every non-cmd package
       files:    internal/verify/scope_test.go, internal/rules/rules_test.go,
                 .dross/survivors.toml
       covers:   c-1, c-2, c-3
       description: The known non-cmd backlog — scope.go (147/151/153/155 boundary +
                 negation pairs, 260) and rules.go:104 — plus every survivor t-1's
                 repo-wide scan reports in packages no phase has mutated yet
                 (board, forge, phase, state, telemetry, techdebt, …): killed in that
                 package's existing `*_test.go`, or accepted with a checkable reason.
       contract: TestScopeInHunkBoundaries fails if any of scope.go:147-155's range
                 comparisons shifts by one (a line exactly on a hunk edge must stay
                 in-scope);
                 TestParseHunksSkipsMalformedHeader fails if scope.go:260's guard inverts;
                 TestRulesSeveritySplit fails if rules.go:104's severity comparison
                 inverts;
                 `dross survivor scan` reports unclassified = 0 for every package outside
                 internal/cmd.
       depends:  t-1

Wave 3 (depends t-4, t-6, t-7, t-8, t-9)

  t-10 Close the drain backlog and prove zero
       files:    internal/cmd/deferred_test.go, .dross/phases/survivor-drain/spec.toml,
                 .dross/survivors.toml
       covers:   c-1, c-2
       description: Resolve the four non-survivor items routed to survivor-drain
                 (completion-state-truth[1], dross-repair[0], mutation-diff-scope[1] and
                 [2]) now that their content is delivered, run the full `dross survivor
                 scan` and record its zero-unclassified output, and add a repo-level guard
                 test asserting no spec still routes an unresolved item at survivor-drain.
       contract: TestNoDeferredItemTargetsSurvivorDrain fails if any
                 .dross/phases/*/spec.toml still carries a non-dismissed [[deferred]] with
                 target = "survivor-drain" — so re-routing a survivor forward instead of
                 resolving it goes red rather than passing silently;
                 `dross survivor scan` exits 0 with unclassified = 0 across all 30
                 packages.
       depends:  t-4, t-6, t-7, t-8, t-9

## Coverage

| criterion | tasks |
|---|---|
| c-1 (repo-wide zero unclassified) | t-1, t-6, t-7, t-8, t-9, t-10 |
| c-2 (routed deferred all resolved, none re-routed) | t-6, t-7, t-8, t-9, t-10 |
| c-3 (reasons concrete + CLI-reachable killed) | t-4, t-5, t-6, t-7, t-8, t-9 |
| c-4 (adapter error/process seams killed) | t-4 |
| c-5 (attribution-ceiling proof test) | t-5 |
| c-6 (absolute unclassified gate) | t-2 |
| c-7 (CLI retirement of stale acceptances) | t-3 |

## Judgment calls

- **Built `dross survivor scan` rather than draining by hand.** c-1's locked scope is
  every Go package, and nothing in the CLI runs mutation outside a phase's diff
  (`dross verify` is diff-scoped by design). Rejected: hand-running gremlins per package
  and cross-referencing keys by eye — not reproducible, and the drain loop repeats the
  run dozens of times. The command is ~60 lines of glue over existing helpers
  (`configuredAdaptersFn`, `verify.RunScoped`, `ApplyLifecycle`, `acceptedReasons`,
  `routedSurvivors`), so it is the cheapest thing that makes c-1 checkable at all.
- **Put the scan under `survivor`, not a `verify --all-packages` flag.** verify writes
  the phase's tests.json/verify.toml; an --all run would either pollute those artifacts
  or need a "write nothing" special case inside the command that owns them.
- **No new `deferred resolve` verb.** c-2 only needs `deferred list --target
  survivor-drain` empty at ship, and `unroute` + `dismiss` already does that per item in
  a shell loop with stable indices. Rejected a bulk `--target` clearing verb as
  speculative structure the criteria never asked for.
- **c-6 lands as a BLOCKING finding plus a prompt-rubric edit, not a new exit code.**
  The prompt already fails a phase on any BLOCKING finding, so emitting one is the whole
  mechanism; only the ratio rules in assets/prompts/verify.md have to be deleted for
  "instead of a ratio" to be true.
- **Drain split into four tasks by package/file cluster, not one.** 91 routed survivors
  plus whatever the repo-wide scan adds is one kind of work, but a single task spanning
  ~30 files can't be committed atomically or reviewed. Split on file clusters (the only
  axis knowable before the scan runs), keeping each task inside one package layer.
  t-6 carries six files, over the five-file guidance — all six are test files in one
  package, and splitting doctor.go's eleven survivors off from status.go's seven would
  buy nothing.
- **t-9's size is scan-determined.** Its known backlog is 11 survivors; the never-mutated
  packages could add more. Kept as one task rather than inventing per-package tasks for
  counts nobody has measured — if the scan returns a flood, that is the moment to split,
  not now.
- **t-1 and t-3 both edit internal/cmd/survivor.go in wave 1.** Same wave, sequential
  execution, no merge conflict; forcing t-3 to wave 2 would serialize an independent
  criterion for nothing.
- **t-5's proof is fixture-based, not a live gremlins run.** A test that shells out to
  gremlins needs it installed and takes minutes; a recorded report/coverage-profile pair
  asserts the same contradiction hermetically and fails if the premise ever stops
  holding.
