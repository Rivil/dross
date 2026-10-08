# Plan Review — exec-taint-enumeration

Reviewed: 2026-09-24
Plan: 22 tasks across 7 waves

## BLOCKING
(none)

## FLAG
- [test-contract / antipattern: files] t-19's verb enforcement is red on arrival and too loose to support the single gitTrim marker.
  - Red on arrival: doctor.go:950 calls `gitTrim(".", "--version")`. `--version` is not in the fixed-shape set, and no task lists doctor.go. TestGitTrimRunsRefVerbsOnly ("every live gitTrim call site's verb must be in the fixed-shape set") therefore fails on live code that no task owns.
  - Checks the verb but not its flags. Three verb-legal calls print content, and the one gitTrim marker would clear all of them:
    - `ls-remote --get-url` prints the configured remote URL, userinfo and token included.
    - `for-each-ref --format=%(contents)` prints commit and tag messages.
    - `rev-list --format=%B` prints commit messages.
  - Verb resolution is unspecified: 33 of the 51 call sites pass the verb as the first argument of gitRefArgs/gitPathArgs rather than to gitTrim.
  - No floor on sites examined (c-7). A walker that resolves nothing passes, and the marker still clears everything.

  Today's callers are clean: for-each-ref uses only `%(refname)`/`%(refname:short)`, and rev-list only `--count`. This is a regression hole, not a live leak.
  Suggestion: add doctor.go to t-19, either routed through gitRead or with `--version` named in the set. Pin the allowed options per verb, not just the verb. Add a floor (51 sites today) and a must-trip fixture that passes a content flag through gitRefArgs.

- [antipattern: files / locked-decision: clearance_model] t-19 owns every gitRead-origin finding "in any file", but one of them lives in a file it doesn't list, and the in-file fix would launder raw diff text. The flow:
  - verify.ParseHunks copies raw hunk-header lines into its degraded list (`fmt.Sprintf("unparsable hunk header in %s: %q", where, line)` at internal/verify/scope.go:273, and again at :278).
  - verifyscope.go:97 appends that list to Scope.Degraded, which is serialized to tests.json (`json:"degraded"`, scope.go:53).
  - A hunk header carries the enclosing source line as function context, so this puts a line of source code into a persisted file.

  internal/verify/scope.go is not in t-19's files. The only remedy inside t-19's files is a marker above verifyscope.go:95, and that would clear raw diff text. clearance_model reserves markers for SHA, branch or path slices.
  Suggestion: add internal/verify/scope.go and its test to t-19. Make the degraded entry fixed prose (a file and a count, not the line), or print the line to stderr.

- [test-contract: gate scope] The burn-down gates use two different scoping rules:
  - t-15, t-19 and t-22 gate by origin.
  - t-14, t-16, t-17 and t-18 gate by escape location (a package set or a file list).
  - t-9's discovery step assigns ownership by origin ("any finding whose origin no burn-down owns").

  So a finding whose origin one task owns, but which escapes in another task's files, lands on whichever gate happens to cover that file, or on none until t-21. One concrete case, following the remote hold protocol's text:
  - ssh stdout goes through remote.ParseStatus, then ParseLockStatus, then ParseHolder (internal/remote/lock.go:356).
  - The resulting Holder text passes through scheduledReason and remote.WaitLine into `fmt.Errorf` at internal/cmd/verify.go:680.
  - The origin belongs to t-17, but the escape sits in t-18's gate.
  - t-18's description covers user-command streams "with no marker" and does not anticipate this.
  - The natural clearance site is ParseHolder in lock.go. No task lists that file, and t-17's "remote's transport output … never at a marker" reads as forbidding a marker there.

  The same package holds more escapes that t-17's gate will see but its files don't list: lock.go:382/388 (`unreadable holder pid %q`, `… since %q`), hold.go:310 (BusyError carrying the parsed Holder), and remote.go:565/573.
  Suggestion: state in t-17 whether the holder and status protocol records count as "transport output" (errors carry fixed prose) or as protocol data (a marker at ParseHolder/ParseStatus), and add internal/remote/lock.go to t-17. Have t-9's discovery step also check that the gate which will see each escape belongs to the task that owns its origin.

- [test-contract] t-21's retirement pass protects only the CANARY tests: "their CANARY tests must still pass". The six scoped files also hold guards that are neither CANARY tests nor zero-findings checks:
  - t-17's marker ban in stryker.go, gremlins.go and stryker_net.go.
  - t-18's "no marker on the test.go/run.go/verify.go stream sites".
  - t-19's TestGitTrimRunsRefVerbsOnly, the only enforcement behind the gitTrim marker.
  - t-14's and t-16's marker-removal checks.

  These are new names, so TestNoTestLost does not protect them. If they are retired along with the zero-findings checks, the gitTrim marker's claim becomes unenforced. If they stay and each one re-runs the engine over the live program, they count against c-8, and t-21's contract doesn't mention them.
  Suggestion: have t-21 name every test in the six files that survives, add a contract row that fails if any named guard disappears, and state whether the marker-removal checks share one engine run.

- [antipattern: files / locked-decision: clearance_model] None of t-22's listed dispositions fits init's `remote get-url`. Its output (init.go:177, `strings.TrimSpace(string(out))`) is a URL that can carry userinfo (`https://user:token@host/…`): not a SHA, branch or path, and not merely compared or counted.
  - The conversion that extracts a safe value is project.DetectRemote/parseGitRemote (internal/project/remote.go:24-57). It rebuilds the URL from host and path and drops the userinfo, but it is outside t-22's files.
  - doctor.go:101 is a second consumer of gitRemoteOriginURL and prints the raw URL at :111 and :115. doctor.go is in no task.
  - A marker at init.go:177 would clear a value that can hold a credential.

  Suggestion: name the disposition in t-22. Either put the marker at the host/path extraction in internal/project/remote.go and add that file, or leave the raw URL tainted and confirm it reaches only stdout.

- [test-contract: terminal_definition] (raised in the first review; still unaddressed) The plan never says whether stdout prints are terminal. Every stdout print in internal/cmd goes through Print/Printf (root.go:144-147, which call fmt.Println/fmt.Printf), across 698 call sites. t-5's terminal set lists only writer values and says "any other external callee receiving taint is an escape". Read literally, fmt.Printf is an escape. Verdicts that depend on this:
  - verify.go:880/888 `Printf("  state    %s\n", st.State)`, where st.State is ssh stdout.
  - doctor.go:111/115, which print the origin URL.
  - doctor.go's host-lock holder lines.

  doctor.go is in no task. If prints count as escapes, these findings reach t-21's global gate with no owner.
  Suggestion: pin fmt.Print/Printf/Println as terminal (implicit os.Stdout) in t-5. Add must-not-trip rows for `fmt.Printf("%s", out)` and a `Printf` wrapper, and a must-trip row for `fmt.Fprint(&buf, out)`.

- [test-contract: c-8] (raised in the first review; still unaddressed) t-21's c-8 evidence can't be read as written:
  - "main's latest run" is b3df5da (2026-09-04). cmd/testsummary exists only on milestone/v1.7 (#132), so that run has no timing table.
  - CI runs on `pull_request` and on pushes to main, and the phase PR is opened only after verify. At verify time there is no "phase PR's CI timing table".
  - "The local -race elapsed time" is a laptop measurement, which this project treats as unreliable (t-1 already uses `dross test`).
  - t-1's 10s gate is measured on the warm remote runner, but CI may compile dependency export data cold for `go list -export`. The gate doesn't bound the CI number.

  Suggestion: use the latest milestone/v1.7 PR run that has the timing table as the baseline. Measure before and after with `go test -race -json ./internal/cmd | go run ./cmd/testsummary` on the remote runner, and write down which number verify reads.

- [test-contract: c-4/c-9] (raised in the first review; still unaddressed) Nothing checks a not_paths.txt row. t-12's sources are pathfence.Fields() entries only, so a path field filed as "not a path" is never traced. Most of the ~450 walked fields will go into that ledger, and t-8's failure message offers a row to paste, so the easiest way to silence a new path field is a row nothing verifies. survivor.Acceptance.File could have been silenced exactly this way.
  Suggestion: t-12 treats not_paths.txt rows as sources that never clear. A row that reaches any os.* function is a finding.

- [test-contract: c-9] (partly raised in the first review) t-8 enumerates fields whose "underlying type is string or []string". That misses in-scope path-shaped fields:
  - `[]RelPath` for `type RelPath string`: its underlying type is `[]RelPath`, not `[]string`. The contract tests only the scalar `type RelPath string`.
  - `*string`.

  The plan also doesn't say whether these are in or out of scope:
  - Untagged exported fields, which BurntSushi/toml and encoding/json serialize under the Go name.
  - Path-keyed maps: verify's `WholeFile map[string]string` (`whole_file`) and `Ranges map[string][]EffectiveRange` (range_provenance.go:216-217, verify.go:232).

  Suggestion: add fixture rows for `[]RelPath` and `*string`. Write the untagged and map boundary into the path-containment entry that t-21 rewrites.

- [locked-decision: clearance_model / marker_grammar] (raised in the first review; still unaddressed) Two ways to clear taint are left unpinned:
  - t-10 clears "the tainted values defined on that line" but does not require that line to be a conversion. A marker above `out, err := ghCommand(args...).Output()` or `c.Stdout = &buf` clears the whole stream. Both locks put the marker at the conversion. The t-17 and t-18 bans cover only six files.
  - t-5 clears any numeric or bool result from an external call. byte and rune are numeric, so a `ReadRune`/`DecodeRune` loop feeding `WriteRune` rebuilds the full output with no marker.

  Suggestion: add a t-10 must-trip row where a marker binds a source-defining line (an Output/CombinedOutput result, a Stdout/Stderr referent, or a pipe). Add t-5 must-trip rows for rune and byte loops, or clear only int and bool.

- [antipattern: files / granularity] (raised in the first review; still unaddressed) t-15 leaves out files its own change breaks:
  - guardedFF and guardedResetHard return gitCombined's `(string, error)` (switchbranch.go:88, :101). Two-value calls to them in gitseparator_test.go:188/193 and refguard_test.go:140/144 stop compiling once gitRun returns only `error`.
  - testdata/subprocargs_audit/snippets.txt has 6 gitCombined rows. Two of them are FLAG rows (`bare-var-positional`, `path-without-separator`) that stop flagging once gitCombined leaves gitCallFuncs.

  That makes 21 files. "Compile-atomic" applies only to deleting gitCombined; gitRun can land beside it.
  Suggestion: add the three files. Split into "add gitRun + migrate callers" and "delete gitCombined".

- [granularity] t-8 spans three layers:
  - Rewriting the test enumeration (pathfence_fields_test.go, plus a ~450-row not_paths.txt).
  - Production registry data (internal/pathfence/fields.go).
  - A production fix in another package (internal/survivor/stale.go), with its carrier binding in pathfence_carrier_test.go.

  It also takes on an open-ended "every newly declared field opened raw is fixed here". internal/survivor/stale_test.go, where the `File = "../escape"` contract row would live, is not listed.
  Suggestion: split candidate. Separate the enumeration rewrite from the survivor remediation, and add stale_test.go.

## NOTE
- [amendment check] The first review's three blockers are resolved:
  - t-15, t-19 and t-22 now gate by origin.
  - Every content-verb gitTrim caller is in t-19's files: status ×3, log ×2, ls-tree, ls-files ×2, diff-tree, diff ×2. The one exception is doctor.go:950 (see the first FLAG).
  - t-8 owns stale.go:101 and has a discovery step.
- [coverage] All ten criteria are covered. t-20 reads c-6's "each scan" as the two tracing scans, exec taint and path fields. The other new findings — t-8's undeclared field, t-4's outside-walk file, t-6's unresolved dispatch and t-10's orphan marker — have message contracts of their own, but they aren't in c-6's fixture. That reading is defensible.
- [wave-order] Every dependency is real, and no task can move to an earlier wave. Tasks that share files are serialized:
  - execconsent_audit_test.go: t-2 → t-6 → t-11.
  - cleantree.go and phase.go: t-15 → t-22.
  - Five shared files: t-15 → t-19.

  t-19 and t-22 share no files and can run in parallel.
- [forbidden-actions] There is no ~/.claude/dross/rules.toml, and no task edits a CI workflow. Project rule r-01 does apply once: t-17 edits internal/remote (the ssh/rsync transport) and t-18 edits internal/cmd/test.go, and together those are the `dross test` path the plan gates on. After they land the installed binary is stale relative to source. Run `make install` before later waves rely on `dross test`, or gate on the pre-phase binary knowingly.
- [TestNoTestLost] These names in tests_before.txt lose their premise during this phase:
  - TestDroppingAScanRootFailsTheFloor and TestExecConsentScansTheSpawningPackages (execConsentScanRoots goes away in t-11).
  - TestEveryPathShapedFieldIsDeclared and TestWalkerFindsANonEmptySet (t-8 deletes schemaDirs and pathShapedTags).
  - TestUnwrapScanSkipsTestFiles (trivially true under Tests:false).

  Only t-6 and t-8 mention the constraint; t-7 and t-11 don't. The suite enforces it regardless.
- [antipattern: description] t-18's survivor_drain wording is now correct (`go list`). update.go is still listed without a stated change. update.go:199-200 already sends the install stream to `o.out`, so it may need nothing; saying so would let the scoped gate's zero-findings row carry that claim.
- [strength] The plan's factual claims match the tree:
  - 39 gitCombined call sites in 14 files, 10 of them in milestone.go.
  - Five gh `%w\n%s` sites: basepr.go:70, comment.go:89, headpr.go:59, merged.go:78 and open.go:114.
  - 28 spawn files.
  - The assertion at comment_test.go:230.
  - The 6f27eaa^ stryker.go lines t-13 pins (115-118, 142, 150 and 315).
  - The "8 of 36 / 11-tag / 4 schema dirs" text that t-21 removes from ARCHITECTURE.md.
  - Every existing test and symbol the contracts name.
- [strength] The engine fails closed. Unknown external calls count as escapes, a tainted return is an escape until t-9 lands, and every transform-set package must be exercised by a corpus line. Fixtures build in their own ssa.Program, so they can't add edges to the live call graph.
- [strength] The CANARY stub tests check that tool output lands on stderr and stays out of returned errors, not just scanner verdicts. The global gate lands after every burn-down, and the t-8 and t-9 discovery steps route live hits to an owner before the gates that would see them.

## Summary
Nothing blocks execution: the amendment fixed the first review's three blockers. The remaining risks:
- The new gitTrim marker is under-enforced: doctor.go's `--version` call breaks the verb check, and content flags on allowed verbs pass it.
- Taint escapes where the files the owning task lists can't fix it: the remote holder text in verify.go and lock.go, and hunk headers written into tests.json.
- Six flags from the first review are still open.
