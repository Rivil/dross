# survivor-drain — verification lens

Every task below was derived by writing the acceptance test first and then asking
what the smallest change is that makes that test runnable and green. The
ordering falls out of that: the *instruments* (the drain gate, the retirement
verb, the evidence tests, the verdict gate) land in wave 1 so that every drain
task in wave 2 has a machine-checkable pass/fail, and wave 3 closes the backlog
against a test that fails if anything is re-routed forward.

```
Phase survivor-drain — 19 tasks across 3 waves

Wave 1
  t-1  Add `dross survivor drain` unclassified gate
       files:    internal/cmd/survivor_drain.go, internal/cmd/survivor_drain_test.go,
                 internal/cmd/survivor.go, README.md
       covers:   c-1, c-2
       desc:     New `survivor drain` subcommand. Reads one or more gremlins JSON
                 reports (--report) or runs the configured Go adapter over
                 --packages, classifies every survivor through verify.Classify
                 using acceptedReasons() + routedSurvivors(), prints the ones with
                 no disposition and returns an error when any remain. A survivor
                 routed to the phase currently being drained is reported
                 outstanding, never silenced.
       contract: - a report survivor absent from survivors.toml and from every
                   [[deferred]] makes the command return an error whose text names
                   `internal/x/y.go:12 (CONDITIONALS_NEGATION)`; the same survivor
                   with an [[accepted]] entry exits 0 printing "0 unclassified"
                 - a survivor whose deferred entry targets the phase being drained
                   (state.CurrentPhase, or --phase) is counted outstanding; the same
                   entry retargeted to another phase is reported `routed` and exits 0
                   — so the drain cannot be closed by re-routing to itself
                 - a survivor whose deferred entry is dismissed counts as
                   outstanding (dismissal is triage, not a disposition)
                 - an empty report set exits 0 rather than erroring on "nothing to do"

  t-2  Add `dross survivor retire` for stale acceptances
       files:    internal/survivor/store.go, internal/survivor/store_test.go,
                 internal/cmd/survivor.go, internal/cmd/survivor_test.go, README.md
       covers:   c-7
       desc:     Store.Remove(key) plus survivor.Retire(path, key) (load → remove →
                 atomic save), and a `survivor retire <key>...` command with --stale
                 to retire everything StaleAcceptances reports as Stale.
       contract: - retiring a key not in the store returns "no acceptance with key
                   <k>" and leaves survivors.toml byte-identical (compare file bytes
                   before/after, not just entry count)
                 - retiring 1 of 3 keys leaves the other two [[accepted]] blocks and
                   every [[category]] block present, and the store still Loads
                   (validate() passes — a retire must not orphan a category
                   reference into an unresolvable reason)
                 - `--stale` retires exactly the entries in Report.Stale; an entry in
                   Report.Unverifiable (unreadable file) is still present afterwards
                   — "I could not look" must never retire a live acceptance
                 - a failed retire (bad key) in a multi-key invocation writes nothing
                   at all

  t-3  Kill the gremlins skip-message seams
       files:    internal/mutation/gremlins.go, internal/mutation/gremlins_test.go
       covers:   c-4
       desc:     Collect the per-package skip reasons Run() currently only prints to
                 stderr into a Gremlins.Unmeasured slice (still printed), and drive
                 both branches through the gremlinsBuildCmd seam.
       contract: - a package whose report file is malformed JSON produces exactly
                   `./a (unreadable report: <parse error text>)` as an element of
                   Unmeasured — an exact string compare, so mutating the
                   `pkg + " (unreadable report: " + err.Error() + ")"` concat
                   (gremlins.go:155) fails the test
                 - a report whose every mutant is NOT_COVERED produces exactly
                   `./b (zero covered mutants — coverage blind spot)`
                   (gremlins.go:166) and contributes no file rows to the merged report
                 - a package with a readable, covered report appears in neither
                   Unmeasured nor the skip output, and its rows do reach the merge

  t-4  Kill the stryker + stryker_net error seams
       files:    internal/mutation/stryker_test.go, internal/mutation/stryker_net_test.go,
                 internal/mutation/testdata/stryker-unknown-status.json
       covers:   c-4
       desc:     Tests driven through strykerBuildCmd / strykerNetBuildCmd against a
                 temp ProjectRoot: no report written, report path is a directory,
                 report is malformed JSON, report carries an unrecognised mutant
                 status, and buildCmd with and without Prefix.
       contract: - with no report on disk the error names the expected report path and
                   says "did not write a report"; with the report path replaced by a
                   directory the error begins "read stryker report:" — inverting the
                   fs.ErrNotExist check (stryker.go:73) fails one of the two
                 - a report body that is not valid JSON makes Run return the
                   ParseStrykerJSON error and a nil report, so the `if err != nil`
                   after parsing (stryker.go:80) cannot be dropped
                 - a report whose only mutant status is "Quantum" parses to
                   Killed=0, Errors=1 and a per-file stat of Errors=1; flipping the
                   default arm's `r.Errors++` to `--` (stryker.go:263) yields -1 and
                   fails the equality assert
                 - StrykerNet.buildCmd with Prefix "" yields Args identical to the
                   args passed in; with Prefix "docker run" yields
                   ["docker","run", …args] — the empty-prefix branch
                   (stryker_net.go:221) cannot be inverted without failing one

  t-5  Prove the attribution ceiling; require cited evidence
       files:    internal/mutation/ceiling_test.go,
                 internal/mutation/testdata/ceiling/ceiling.go,
                 internal/mutation/testdata/ceiling/ceiling_test.go,
                 internal/mutation/testdata/ceiling/gremlins-report.json,
                 internal/survivor/reasons_repo_test.go, .dross/survivors.toml
       covers:   c-3, c-5
       desc:     A fixture package containing a switch-case condition, a const
                 initializer and a string-concat return. The proof test runs
                 `go test -coverprofile` over the fixture and asserts the switch-case
                 line has count>=1, then reads the recorded gremlins report for the
                 same fixture and asserts that line is NOT_COVERED. A second test
                 audits .dross/survivors.toml: every own-reason and every category
                 reason must name a Go test that exists in the repo. The
                 gremlins-attribution-ceiling category's prose is rewritten to cite
                 this proof test.
       contract: - the coverage profile for testdata/ceiling reports count>=1 on the
                   switch-case line; if the fixture test stops executing that line the
                   proof test fails rather than silently proving nothing
                 - the recorded gremlins report marks that same file:line NOT_COVERED,
                   asserted by line and status, so re-recording a report where it is
                   COVERED fails
                 - the report contains an ARITHMETIC_BASE mutant on the
                   string-concat line — the evidence the bogus-arithmetic category
                   (t-9) cites
                 - the reason audit fails, naming the offending key, for a synthetic
                   store whose acceptance reason cites no TestXxx, and for one citing
                   `TestThatDoesNotExist`; run against the real
                   .dross/survivors.toml it passes (the survivor.go:242 entry already
                   cites TestLoadRejectsHandEditedReasonlessEntry)

  t-6  Gate the verdict on unclassified in-scope count
       files:    internal/verify/verify.go, internal/verify/verify_test.go,
                 assets/prompts/verify.md, internal/cmd/verify_prompt_test.go
       covers:   c-6
       desc:     Add Summary.UnclassifiedInScope — the count of in-scope survivors
                 whose lifecycle is neither accepted nor routed — emit each as
                 BLOCKING instead of FLAG, and rewrite verify.md's verdict rules so
                 the mutation fail lever is that absolute count, with the score
                 reported but no longer a threshold.
       contract: - a Tests fixture with one in-scope survivor in state `in-diff`
                   gives Summary.UnclassifiedInScope==1 and a BLOCKING finding naming
                   `file:line (op)`; the same survivor in state `accepted` gives 0 and
                   no BLOCKING
                 - an in-scope survivor in state `routed` leaves the count at 0 and
                   keeps its existing NOTE — routing still parks debt, acceptance
                   still silences it
                 - a fixture with mutation_score 0.95 and one unclassified in-scope
                   survivor still emits BLOCKING: a high ratio can no longer buy a
                   pass, which is exactly the swap c-6 asks for
                 - the prompt test asserts the measured branch's `fail` bullet names
                   `unclassified_in_scope` and that no verdict branch lists
                   "< 0.60" or "≥ 0.80" as a pass/fail condition any more

Wave 2 (depends t-1, t-5)
  t-7  Drain doctor.go and hints.go survivors
       files:    internal/cmd/doctor_test.go, internal/cmd/hints_test.go,
                 .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     17 routed survivors (doctor.go 93/95/98/164/248/251×2/358/360/362/391,
                 hints.go 86×2/88×2/133×2). Each is reached through the command's
                 RunE with a fixture repo and killed, or accepted under the proven
                 ceiling category when gremlins cannot attribute it.
       contract: - `dross survivor drain --report reports/gremlins/internal-cmd.json`
                   reports zero outstanding survivors for doctor.go and hints.go
                 - each kill is a boundary test, not a smoke test: doctor.go:251 and
                   hints.go:86/88/133 are CONDITIONALS_BOUNDARY sites, so each new
                   test asserts behaviour at n-1, n and n+1 of the compared value —
                   a `>=`→`>` mutation changes one of the three
                 - every acceptance this task writes passes the t-5 reason audit
                   (own reason cites a live test, or names the ceiling category)

  t-8  Drain footer_coverage, validate, repair, statusline
       files:    internal/cmd/footer_coverage_test.go, internal/cmd/validate_test.go,
                 internal/cmd/repair_test.go, internal/cmd/statusline_test.go,
                 .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     footer_coverage.go 52/55/58/60, validate.go 92/96, repair.go 131/133,
                 statusline.go 189 — all CONDITIONALS_NEGATION guards. Killed through
                 the public command surface; accepted only where the guard cannot be
                 reached through the CLI, with the unreachability named.
       contract: - `dross survivor drain` reports zero outstanding for those four files
                 - each of footer_coverage.go's four guards has a test asserting BOTH
                   sides of the condition (present/absent), so negating any one of
                   them flips exactly one assertion
                 - any acceptance here states which caller makes the branch
                   unreachable and cites the test that pins that caller — the audit
                   from t-5 fails otherwise

  t-9  Accept the string-concat ARITHMETIC_BASE set as one category
       files:    .dross/survivors.toml
       covers:   c-1, c-2, c-3
       depends:  t-1, t-5
       desc:     Define the bogus-arithmetic category once (reason cites t-5's proof
                 test) and accept the ~14 ARITHMETIC_BASE survivors sitting on string
                 concatenation and const initializers: gitignore.go 17/46/47/48/64/65/66,
                 trust.go 279/280/281, basebranch.go 104, statusline.go 23,
                 status.go 444, milestone_dependents.go 160. All via
                 `dross survivor accept --op ARITHMETIC_BASE --category`.
       contract: - `dross survivor list` shows every one of those keys resolving to the
                   shared category prose, and survivors.toml contains exactly one
                   [[category]] block for it — the prose is stored once, per the
                   locked acceptance_granularity model
                 - the category's reason cites the t-5 proof test by name, so the t-5
                   reason audit fails if the prose is later replaced with an assertion
                 - `dross survivor drain` reports zero outstanding ARITHMETIC_BASE
                   survivors in internal/cmd, and the adapter still emits them (this
                   phase accepts, it does not filter — the filter is
                   mutation-score-truth's contract)

  t-10 Drain status.go and ship.go survivors
       files:    internal/cmd/status_test.go, internal/cmd/ship_test.go,
                 .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     status.go 442×2/444×2/472/474, ship.go 264/550/552/554. The status.go
                 442/444 pair are BOUNDARY+NEGATION on the same lines; ship.go's are
                 negation guards in the merge-gate path.
       contract: - `dross survivor drain` reports zero outstanding for status.go and
                   ship.go
                 - status.go:442/444's boundary tests pin the exact comparison: a run
                   at the boundary value renders one string and one past it renders
                   the other, so `>` vs `>=` is observable in stdout
                 - ship.go:550/552/554 are covered by a test per branch asserting the
                   gate's decision (proceed vs refuse) and the message naming the
                   reason — negating any one changes which message is printed

  t-11 Drain phase, phase_lifecycle and watch survivors
       files:    internal/cmd/phase_test.go, internal/cmd/phase_lifecycle_test.go,
                 internal/cmd/watch_test.go, .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     phase.go 308/310/768, phase_lifecycle.go 23/25/27, watch.go 130
                 (BOUNDARY + NEGATION on the same line).
       contract: - `dross survivor drain` reports zero outstanding for those three files
                 - phase_lifecycle.go 23/25/27 gain one test per guard driving the
                   insert/move/rename argument validation to its failure message —
                   each negation makes a rejected input succeed, which the test catches
                 - watch.go:130's pair is killed by an at-boundary and a past-boundary
                   case (the same fixture at n and n+1), not by a single call

  t-12 Drain env, state, update, project, milestone_dependents
       files:    internal/cmd/env_test.go, internal/cmd/state_test.go,
                 internal/cmd/update_test.go, internal/cmd/project_test.go,
                 internal/cmd/milestone_dependents_test.go, .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     env.go 105/109/114/119, state.go 270, update.go 181/184,
                 project.go 81/83, milestone_dependents.go 158/160 (NEGATION +
                 INVERT_NEGATIVES; the ARITHMETIC_BASE on 160 goes to t-9).
       contract: - `dross survivor drain` reports zero outstanding for those five files
                 - env.go's four guards are covered by a table test whose cases are
                   set/unset/empty/invalid for one variable, asserting a distinct
                   message per case — negating any single guard collapses two cases
                   onto the same message
                 - milestone_dependents.go:160's INVERT_NEGATIVES is killed by a case
                   with a genuinely negative operand, not merely a zero one

  t-13 Drain the single-guard command files
       files:    internal/cmd/install_test.go, internal/cmd/rule_test.go,
                 internal/cmd/hooks_test.go, internal/cmd/issue_test.go,
                 internal/cmd/root_test.go, .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     One CONDITIONALS_NEGATION guard each: install.go 65, rule.go 143,
                 hooks.go 100, issue.go 171, root.go 61. Kill through each command's
                 own entry point; accept only with a named unreachable caller.
       contract: - `dross survivor drain` reports zero outstanding for those five files
                 - each guard's test asserts both outcomes of the condition through
                   the command's RunE (not by calling the helper with a hand-built
                   struct that could never occur), so an inverted guard changes the
                   command's exit path
                 - root.go:61's test drives the real cobra root command, so the guard
                   is proven reachable from `dross` itself rather than from a helper

  t-14 Drain internal/verify/scope.go and internal/rules
       files:    internal/verify/scope_test.go, internal/rules/rules_test.go,
                 .dross/survivors.toml
       covers:   c-1, c-2, c-3
       desc:     scope.go 147/151/153/155 (BOUNDARY+NEGATION pairs) and 260, plus
                 rules.go 104 (BOUNDARY+NEGATION). Scope is a pure type, so every one
                 of these is reachable from a table test and must be killed rather
                 than accepted.
       contract: - `dross survivor drain --packages ./internal/verify/... ./internal/rules/...`
                   exits 0
                 - scope.go's line-range comparisons are pinned at the range
                   endpoints: a hunk of [10,20] answers InHunk true at 10 and 20 and
                   false at 9 and 21, so both the boundary and the negation mutants
                   on 147/151/153/155 die
                 - rules.go:104 is exercised at the length boundary in both directions
                   (one under, exactly at, one over)

  t-15 Sweep the leaf config packages
       files:    internal/argfence/argfence_test.go, internal/configenum/configenum_test.go,
                 internal/defaults/defaults_test.go, internal/hostallow/hostallow_test.go,
                 internal/profile/profile_test.go, internal/redact/redact_test.go,
                 .dross/survivors.toml
       covers:   c-1, c-3
       desc:     First half of the never-mutated packages the locked drain_scope
                 decision pulls in. Run the adapter per package, then kill or accept
                 whatever it reports.
       contract: - `dross survivor drain --packages ./internal/argfence/...
                   ./internal/configenum/... ./internal/defaults/... ./internal/hostallow/...
                   ./internal/profile/... ./internal/redact/...` exits 0
                 - redact and hostallow survivors are killed, never accepted: both are
                   security-relevant matchers reachable from a pure unit test, so an
                   acceptance in either is a hole the phase would be signing off
                 - every acceptance written here passes the t-5 reason audit

  t-16 Sweep the state and domain packages
       files:    internal/board/board_test.go, internal/changes/changes_test.go,
                 internal/hooks/settings_test.go, internal/milestone/milestone_test.go,
                 internal/state/state_test.go, internal/statusline/render_test.go,
                 internal/phase/phase_test.go, .dross/survivors.toml
       covers:   c-1, c-3
       desc:     Second sweep group: the packages that hold .dross state and its
                 rendering.
       contract: - `dross survivor drain --packages ./internal/board/... ./internal/changes/...
                   ./internal/hooks/... ./internal/milestone/... ./internal/state/...
                   ./internal/statusline/... ./internal/phase/...` exits 0
                 - state.go survivors on the save path are killed by round-trip tests
                   (write → read → compare every field), so a dropped field assignment
                   is observable rather than accepted as untestable
                 - the phase.go:190 slug-charset pair already accepted under the
                   ceiling category still resolves to that category after the sweep
                   (no duplicate key, no second reason)

  t-17 Sweep the analyzer catalog packages
       files:    internal/codex/ast_grep_test.go, internal/quality/catalog_test.go,
                 internal/security/catalog_test.go, internal/techdebt/run_test.go,
                 internal/stack/detect_test.go, internal/telemetry/taxonomy_test.go,
                 internal/findings/reconcile_test.go, .dross/survivors.toml
       covers:   c-1, c-3
       desc:     Third sweep group: the catalog/analyzer packages, where most
                 survivors will be on table-driven selection logic and are killable
                 by adding a case rather than a test file.
       contract: - `dross survivor drain --packages ./internal/codex/... ./internal/quality/...
                   ./internal/security/... ./internal/techdebt/... ./internal/stack/...
                   ./internal/telemetry/... ./internal/findings/...` exits 0
                 - catalog-selection survivors are killed by a case that selects a
                   DIFFERENT analyzer, so an inverted match is observable in the
                   chosen tool name rather than only in a count
                 - findings fingerprint/reconcile survivors are killed by a
                   same-finding/different-finding pair — a negated comparison merges
                   or splits the two, which the test asserts on

  t-18 Sweep the process and forge packages
       files:    internal/forge/github_test.go, internal/ship/basepr_test.go,
                 internal/update/update_test.go, internal/watch/drift_test.go,
                 internal/architecture/architecture_test.go, .dross/survivors.toml
       covers:   c-1, c-3
       desc:     Final sweep group: the packages that talk to processes and the
                 forge. Survivors on real network/exec paths are accepted with the
                 seam named; everything reachable through an existing fake is killed.
       contract: - `dross survivor drain --packages ./internal/forge/... ./internal/ship/...
                   ./internal/update/... ./internal/watch/... ./internal/architecture/...`
                   exits 0
                 - any acceptance here names the process or HTTP seam that makes the
                   branch unreachable AND cites the existing test that proves the
                   seam is the only way in — "needs the network" alone fails the t-5
                   audit
                 - update.go signature-verification survivors are killed, not
                   accepted: the minisign verify path already has a local fixture, so
                   an acceptance there would silence the release-signing gate

Wave 3 (depends t-7 … t-18)
  t-19 Close the routed deferred backlog
       files:    .dross/phases/survivor-lifecycle/spec.toml,
                 .dross/phases/mutation-diff-scope/spec.toml,
                 .dross/phases/dross-repair/spec.toml,
                 .dross/phases/completion-state-truth/spec.toml,
                 internal/cmd/survivor_backlog_repo_test.go
       covers:   c-2
       desc:     Walk `dross deferred list --target survivor-drain --json` and close
                 each entry with `dross deferred unroute <phase> <idx>` then
                 `dross deferred dismiss <phase> <idx>` — indices are stable because
                 neither verb removes an element. Add a repo-invariant test pinning
                 the closed state.
       contract: - TestSurvivorDrainBacklogClosed fails if collectDeferred over the
                   repo yields any entry with Target=="survivor-drain" and
                   Dismissed==false, so `dross deferred list --target survivor-drain`
                   being empty is enforced by CI, not by a ship-time eyeball
                 - the same test fails if any survivor-keyed deferred entry is routed
                   to a phase that sits AFTER survivor-drain in the milestone's phases
                   array — the "none is re-routed forward" half of c-2
                 - after closure `dross survivor drain` still exits 0: dismissing a
                   routed entry drops it out of routedSurvivors(), so a survivor
                   closed by dismissal alone (never killed, never accepted) reappears
                   as outstanding and fails the run
                 - `dross deferred list --dismissed` still shows each closed item with
                   its survivor key, so the drain is auditable after the fact
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (repo-wide zero unclassified) | t-1, t-7, t-8, t-9, t-10, t-11, t-12, t-13, t-14, t-15, t-16, t-17, t-18 |
| c-2 (routed backlog resolved, none re-routed) | t-1, t-7, t-8, t-9, t-10, t-11, t-12, t-13, t-14, t-19 |
| c-3 (reasons concrete + checkable; reachable ⇒ killed) | t-5, t-7 … t-18 |
| c-4 (adapter error/process seams killed) | t-3, t-4 |
| c-5 (attribution-ceiling proof test) | t-5 |
| c-6 (absolute unclassified-in-scope gate) | t-6 |
| c-7 (CLI retirement of a stale acceptance) | t-2 |

## Judgment calls

- **Built `dross survivor drain` as wave-1 task t-1 rather than draining by hand
  against `dross verify`.** Rejected: a shell script or a Makefile loop. c-1 and
  c-2 are both *repo-state* claims, and without a command that returns non-zero
  they can only be asserted, not tested — every later drain task would have no
  contract but "I looked".
- **Defined "unclassified in-scope" as *in-scope survivor that is neither
  accepted nor routed* — i.e. `len(InDiff) + len(in-scope Unclassified)`.**
  Rejected: counting only `LifecycleUnclassified`. Classify() puts in-scope
  survivors in `in-diff`, so gating on the literal `unclassified` bucket would
  make c-6's gate near-vacuous (it would fire only on unresolvable identities).
- **c-6 removes the 0.80/0.60 cutoffs as verdict levers instead of adding the
  count alongside them.** The criterion says "instead of a ratio". Keeping both
  would leave a phase failing for a neighbour's ratio while passing with its own
  unclassified survivor — the exact inversion the drain exists to end. The score
  stays in the summary as reporting.
- **Merged the reason-audit test into t-5 (the proof test) instead of shipping it
  first.** The audit requires every category reason to cite a live test, and the
  existing gremlins-attribution-ceiling prose cites a *date*, not a test — so an
  audit landed before the proof test would fail at its own commit. One task
  produces the evidence, cites it, and turns citation into a rule.
- **Gremlins skip reasons collected into `Gremlins.Unmeasured` rather than
  captured by swapping `os.Stderr` in the test.** Rejected: a stderr-capture
  test, which is fragile under `-count=1` parallelism and asserts on a stream
  shared with the streamed subprocess output. The field also makes a skipped
  package inspectable, which is the honest fix for a message that currently
  exists only in scrollback. Rejected too: changing `mutation.Report`'s schema,
  which would ripple into tests.json and belongs to mutation-score-truth.
- **t-9 accepts the ARITHMETIC_BASE set as one category and touches only
  survivors.toml.** Rejected: filtering them at the adapter — explicitly locked
  out of this phase (`bogus_arithmetic_class`), and routed to
  mutation-score-truth by the spec's own deferred list.
- **Backlog closure uses the existing `unroute` + `dismiss` pair rather than a new
  `dross deferred resolve` verb.** The phase already ships two new CLI verbs; a
  third with overlapping semantics is scope the criteria do not ask for. The
  index-stability of both verbs makes the bulk loop safe.
- **t-19's closure is pinned by a repo-invariant test, not by a ship-time check.**
  `dross deferred list --target survivor-drain` being empty is a property of the
  tree, so it belongs in `go test` alongside the other repo-invariant tests in
  internal/cmd — otherwise the criterion decays the moment the next phase routes
  something.
- **The four sweep tasks (t-15 … t-18) exceed the 5-file guideline.** A sweep's
  unit is a *package set*, and every file in one is a test file in one layer;
  splitting to satisfy a file count would produce sub-10-minute tasks, which the
  granularity rule forbids in the other direction. They are grouped by the kind
  of seam their survivors will sit on (pure logic → catalogs → process/forge), so
  each task has one dominant kill technique.
- **Drain tasks depend on t-1 and t-5 only, not on each other.** They touch
  disjoint test files plus `.dross/survivors.toml`; the store is append-shaped
  via `dross survivor accept`, so parallel execution risks only a TOML merge, not
  a semantic conflict.
- **t-7 … t-18 are stated as kill-first.** Each contract names the boundary or
  branch pair the new test must pin, so "accepted because untestable" has to
  survive a written contract that says otherwise — which is c-3's requirement
  turned into a plan-level gate rather than a reviewer's judgement call.
