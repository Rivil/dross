# Panel draft — VERIFICATION LENS

Method: each criterion's ideal test contract was written first; the task is the
smallest change that makes that contract satisfiable. Where a criterion's
contract needed a seam that does not exist yet, the seam is the task.

```
Phase tool-output-not-persisted — 9 tasks across 3 waves

Wave 1
  t-1  Add the shared tool-failure recorder
       files:    internal/mutation/toolfail.go, internal/mutation/toolfail_test.go
       covers:   c-1
       contract: RecordToolFailure("stryker", 3, 4096).Error() names the tool, "exit
                 status 3", "4096 bytes" and the fixed stderr-pointer sentence; a
                 reflect.TypeOf check on the constructor's signature fails if any
                 parameter becomes string/[]byte (the only way captured output could
                 re-enter the record); reflect over mutation.Recorded fails if any
                 field becomes exported, since an exported field lets a caller mint
                 a record without the constructor.

  t-2  Declare the tool-text sink registry
       files:    internal/toolfence/fields.go, internal/toolfence/fields_test.go
       covers:   c-4
       contract: Validate returns an error naming "no disposition", "both
                 dispositions", "no Carrier" and "no Why" for four synthetic bad
                 entries fed directly to it; TestRegistryCoversEveryKnownFieldByName
                 fails if verify.LanguageRun.Error, verify.LegSummary.Error or
                 verify.Finding.Text is deleted from the registry, and fails on a
                 count mismatch so a new entry cannot be added unpinned.

Wave 2 (depends t-1)
  t-3  Route stryker failure paths through the recorder
       files:    internal/mutation/stryker.go, internal/mutation/stryker_failure_test.go
       covers:   c-1, c-2, c-3
       depends:  t-1
       contract: with strykerBuildCmd stubbed to a command that prints CANARY-A and
                 exits 3 writing no report, Run's error contains "exit status 3" and
                 does NOT contain CANARY-A, while os.Stderr captured over the call
                 contains CANARY-A twice — once from the live tee, once under the
                 re-printed "the head of stryker's output" banner. A second case
                 drives checkInstrumented's dropped-path branch to the same pair of
                 assertions. Deleting the re-print fails the stderr assertion;
                 keeping the quote in the error fails the CANARY-A assertion.

  t-4  Route gremlins failure paths through the recorder
       files:    internal/mutation/gremlins.go, internal/mutation/gremlins_failure_test.go
       covers:   c-1
       depends:  t-1
       contract: with gremlinsBuildCmd stubbed to a command exiting 2 and writing no
                 report, errors.As(err, &*mutation.ToolFailure{}) succeeds and the
                 message names "exit status 2"; the ssh-could-not-start branch still
                 wraps remote.ErrTransport, asserted with errors.Is, so routing
                 through the recorder cannot silently drop the transport
                 classification verify.LanguageRun.RemoteTransport reads.

  t-5  Funnel leg errors through the recorder carrier
       files:    internal/verify/verify.go, internal/verify/legerror_test.go
       covers:   c-1, c-4
       depends:  t-1
       contract: verify.LanguageRun.Error and verify.LegSummary.Error are assigned
                 from mutation.RecordLegError(err), whose return type is
                 mutation.Recorded — a reflect check on RecordLegError's Out(0) fails
                 the moment it reverts to string. A leg failing with a non-tool error
                 (an argfence refusal) still records that error's text, asserted by
                 substring, so the funnel cannot swallow dross-authored diagnostics.

Wave 3
  t-6  Ban tool output inside error constructors
       files:    internal/mutation/toolfence_residual_test.go
       covers:   c-1
       depends:  t-3, t-4
       contract: an AST scan over every non-test file in internal/mutation fails if
                 any argument of errors.New, fmt.Errorf or fmt.Sprintf contains a
                 call to headBuffer.quote or a read of head.buf; a self-test drives
                 the same scanner over an in-test fixture where `errors.New(h.quote(40))`
                 IS reported and `fmt.Fprint(os.Stderr, h.quote(40))` is NOT, so a
                 scanner that stopped matching fails on a written-down answer rather
                 than passing vacuously.

  t-7  Enumerate sinks and guard their assignments
       files:    internal/toolfence/walk_test.go, internal/toolfence/carrier_test.go
       covers:   c-4
       depends:  t-2, t-5
       contract: a two-way walk over ../verify and ../cmd fails on a text-shaped
                 field (tag in {error, message, reason, detail, output, stderr, text})
                 of string type with no registry entry, and fails on a registry entry
                 whose struct or field no longer exists; an assignment guard over the
                 same trees fails on any composite-literal element or `x.Error =`
                 whose right-hand side is not the declared carrier call or a copy of
                 an already-declared field — proven by a walker self-test where
                 `Error: err.Error()` is rejected and `Error: mutation.RecordLegError(err).Text()`
                 is accepted; a vacuity guard fails if the walk finds fewer than
                 three fields or zero assignments.

  t-8  Prove the record persists and the quote does not
       files:    internal/verify/persist_toolfence_test.go
       covers:   c-2
       depends:  t-3, t-5
       contract: a stryker leg stubbed to print CANARY-B and exit 1 without a report
                 is run through RunScoped, Tests.Save, Skeleton and Verify.Save into
                 a t.TempDir(); the bytes of tests.json AND of verify.toml each
                 contain "exit status 1", the byte count and the stderr-pointer
                 sentence, and neither contains CANARY-B nor the string "the head of
                 stryker's output". Both files are asserted, so a fix applied to one
                 writer and not the other fails.

  t-9  Enumerate PR and board body composers
       files:    internal/ship/body_toolfence_test.go, internal/cmd/issue_body_toolfence_test.go
       covers:   c-5, c-6
       depends:  t-2, t-5
       contract: every declared registry field is filled with its own canary on a
                 synthetic phase.Spec/verify.Verify/phase.Plan, and BuildPRBody,
                 renderPhaseBody, renderTaskBody and milestoneBody are each called;
                 the test fails if any output contains any canary, and fails if the
                 composer table it iterates is empty. An AST scan of internal/ship
                 and internal/cmd lists every func returning a string that is passed
                 as an issue/PR body, and fails on one absent from the table — so a
                 new composer is a red test. `dross ship comment` is enumerated as a
                 pass-through (body comes only from --body/--body-file), declared with
                 a Why, and the test fails if that function starts reading project
                 state.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1, t-3, t-4, t-5, t-6 |
| c-2 | t-3, t-5, t-8 |
| c-3 | t-3 |
| c-4 | t-2, t-5, t-7 |
| c-5 | t-9 |
| c-6 | t-9 |

6/6 criteria covered.

## Judgment calls

- **Canaries, not string-shape assertions.** Every persistence and composer contract
  asserts the ABSENCE of a unique string the stub tool printed, rather than asserting
  the presence of the expected record. Chose it because absence-of-canary is the
  criterion verbatim ("appears in neither file"); rejected asserting only the record's
  fields, which passes just as well if the quote is appended after it.
- **The recorder's constructor takes an int byte count, never the bytes.** Chose a
  signature that structurally cannot carry output, with a reflection test pinning it;
  rejected passing the headBuffer and having the recorder trim, which leaves the raw
  string one refactor from being formatted in.
- **Both failure paths in stryker, not just the missing-report one.** The spec names
  the missing-report path (~line 142), but checkInstrumented (~line 315) embeds the
  same `head.quote(strykerHeadLines)` and reaches the same sinks. Covering one would
  leave c-2 true and c-1 false.
- **The walker and carrier tests live in internal/toolfence (external test package),
  not internal/cmd.** pathfence put them in internal/cmd because its carriers were
  unexported there; this recorder is exported from internal/mutation, so the
  assertion can sit next to the registry. Rejected mirroring pathfence's placement
  for its own sake — it would put the test two packages away from what it guards.
- **verify.Finding.Text is declared and flagged renderable.** It interpolates the leg
  error and BuildPRBody renders findings, so leaving it undeclared would hide a real
  sink; declaring it un-renderable would force stripping the tool name and exit status
  out of the FLAG finding, a diagnostic regression c-3's spirit argues against. Chose
  an explicit Renderable disposition carrying a Why, asserted to be recorder-sourced.
- **The residual AST scan is its own wave-3 task.** Folding it into t-3 or t-4 would
  make it red until the other adapter landed, so it would be written to pass against
  one adapter and then loosened. Rejected merging despite it being a single test file.
- **t-9 spans two packages in one task.** The canary harness is the same shape for PR
  and board bodies but cannot be shared across package boundaries; splitting would
  produce two sub-ten-minute tasks with identical review surface.
