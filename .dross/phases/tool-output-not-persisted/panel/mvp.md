# MVP lens

Phase tool-output-not-persisted — 5 tasks across 3 waves

```
Wave 1
  t-1  Add the shared tool-failure record
       files:    internal/toolfail/toolfail.go, internal/toolfail/toolfail_test.go
       covers:   c-1
       desc:     New package. Record{Tool, Exit, Bytes, Hint, err} implements error;
                 Error() is fixed prose naming the tool, its exit status, the bytes
                 captured and the stderr pointer. New(...) constructs it, Unwrap()
                 preserves the wrapped error's identity, Text(err) is the one call a
                 persistence site makes.
       contract: - if Error() drops the exit status, the byte count or the
                   "full output went to this run's stderr" pointer,
                   TestRecordNamesToolExitBytesAndStderr fails
                 - if Error() interpolates the wrapped error's text, a Record wrapping
                   a canary-carrying error renders the canary and
                   TestRecordMessageIsFixedProse fails
                 - if Unwrap() is dropped, errors.Is(Record{err: sentinel}, sentinel)
                   goes false and TestRecordPreservesWrappedIdentity fails — the same
                   break that would silently stop verify's RemoteTransport
                   classification firing

Wave 2 (depends t-1)
  t-2  Route both adapters through the record
       files:    internal/mutation/stryker.go, internal/mutation/gremlins.go,
                 internal/mutation/toolfail_route_test.go
       covers:   c-1, c-2, c-3
       desc:     stryker's missing-report path prints head.quote(strykerHeadLines) —
                 plus the initial-test truncation note when it applies — to os.Stderr
                 and returns toolfail.New("stryker", exit, head.buf.Len(), installHint);
                 the ExitError's code is captured into a toolExit var as gremlins
                 already does. checkInstrumented prints its quote to stderr and returns
                 its dropped-file message without it. gremlins tees stdout/stderr
                 through a headBuffer and returns a Record (wrapping the existing
                 transport/reportless errors) from its three fatal paths.
       contract: - if head.quote goes back into the missing-report error,
                   TestStrykerNoReportErrorCarriesNoToolOutput fails: a fake tool emits
                   a canary banner and writes no report; the returned error must name
                   stryker, the exit status and the byte count and must not contain
                   the canary
                 - if the head stops being printed at the failure point,
                   TestStrykerNoReportPrintsHeadToStderr fails — the canary must appear
                   in captured stderr twice (once teed live, once re-quoted)
                 - if a gremlins fatal path reverts to its own fmt.Errorf prose,
                   errors.As(err, **toolfail.Record) goes false and
                   TestGremlinsFatalPathsReturnARecord fails
                 - if gremlins' output stops being teed, its Record reports 0 bytes and
                   TestGremlinsRecordsBytesCaptured fails
                 - source audit: if any error-returning statement in internal/mutation
                   references head.quote or head.buf,
                   TestNoAdapterEmbedsToolOutputInAnError fails

  t-3  Declare persisted text sinks, enumerate writers
       files:    internal/toolfail/fields.go, internal/toolfail/fields_test.go,
                 internal/cmd/toolfail_fields_test.go, internal/verify/verify.go
       covers:   c-4
       desc:     Registry mirroring pathfence.Fields: Field{Struct, Field, Tag,
                 Artifact} with exactly one of Recorded{Writer} / NotDerived{Why,
                 Writers}, plus Validate. Entries: verify.LanguageRun.Error and
                 verify.LegSummary.Error recorded; verify.SkippedFile.Reason,
                 verify.OutOfScopeMutant.Note, mutation.Mutant.Note not-derived. The
                 internal/cmd walker checks both directions over ../verify, ../mutation,
                 ../changes, ../phase, ../project and walks every assignment to a
                 declared field. verify.go:565 becomes Error: toolfail.Text(err).
       contract: - adding a string field tagged `json:"error"` or `toml:"message"` to
                   internal/verify or internal/mutation without a registry entry fails
                   TestEveryTextShapedFieldIsDeclared as an undeclared sink
                 - renaming verify.LegSummary.Error out from under its entry fails the
                   same test as a stale declaration
                 - reverting verify.go:565 to Error: err.Error() fails
                   TestDeclaredSinksAreWrittenOnlyByTheRecorder — the RHS is neither a
                   call into internal/toolfail nor a copy of another declared field
                 - if the walker stops matching, TestTextWalkerFindsANonEmptySet fails
                   (< 5 declared fields found, or < 2 assignment sites seen) rather
                   than letting both directions pass vacuously
                 - Validate rejects an entry with both dispositions, with neither, or
                   recorded-with-no-writer: TestValidateRejectsMalformedEntries

Wave 3
  t-4  Prove the artifacts carry pointer, not output   (depends t-2, t-3)
       files:    internal/verify/toolfail_persist_test.go
       covers:   c-2
       desc:     End-to-end over the verify pipeline with a stub adapter returning a
                 Record built from a canary-laden stream; asserts what lands in
                 tests.json and verify.toml.
       contract: - if tool output reaches disk, TestFailedLegPersistsPointerNotOutput
                   fails: both tests.json and verify.toml must contain the exit status,
                   the byte count and the stderr pointer, and neither may contain the
                   canary line the fake tool printed
                 - if LegSummary.Error stops being populated from LanguageRun.Error,
                   the same test fails on verify.toml's empty error field — a dead leg
                   reading as a clean single-leg run

  t-5  Enumerate PR and board body composers          (depends t-3)
       files:    internal/cmd/bodycompose_toolfail_test.go
       covers:   c-5, c-6
       desc:     One AST test, two groups. PR group: ship.BuildPRBody in ../ship/body.go
                 and the CommentOpts.Body composer at internal/cmd/ship.go:60. Board
                 group: renderTaskBody (issue_task.go), milestoneBody and
                 renderPhaseBody (issue.go). No composer references a declared field
                 today; the test pins that.
       contract: - if BuildPRBody starts rendering leg.Error,
                   TestNoComposerRendersADeclaredSink fails naming the composer and the
                   field it read
                 - if a composer is renamed or moved so the enumeration finds none,
                   TestComposerEnumerationIsNonEmpty fails per group — the PR group and
                   the board group are counted separately, so a vacuous board half
                   cannot hide behind a healthy PR half
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 shared recorder, every adapter routed | t-1, t-2 |
| c-2 no tool text in tests.json / verify.toml | t-2, t-4 |
| c-3 live tee + head re-printed at failure | t-2 |
| c-4 sink registry + writer enumeration | t-3 |
| c-5 no PR body / comment renders a declared field | t-5 |
| c-6 no board issue body renders a declared field | t-5 |

6/6 criteria covered.

## Judgment calls

- **New `internal/toolfail` package rather than putting Record in `internal/mutation`.** verify already imports mutation, so the type would resolve — but the registry declares `verify.*` fields, and the adapter package holding verify-facing declarations is the shape pathfence deliberately avoided. One extra package, no cycles, and internal/cmd's walker imports a leaf.
- **The guard allows two RHS forms — a call into `toolfail`, or a copy of another declared field — instead of a no-op `Carried()` wrapper.** `LegSummary.Error = lr.Error` is a copy between two declared sinks; wrapping it in a function that does nothing just to satisfy an AST matcher is a lie the next reader has to decode.
- **`verify.Finding.Text` is left out of the registry.** It interpolates `lr.Error`, which after the cut is fixed prose. Declaring it would make c-5 unsatisfiable without dropping the Findings section from the PR body — a real diagnostic regression, to protect a value that is safe by construction.
- **`checkInstrumented`'s dropped-file error is not routed through the recorder.** The tool succeeded there; the verdict is dross's own. Only the embedded `head.quote` moves to stderr. A Record would replace the dropped-path list, which is the entire content of that failure.
- **Tag vocabulary is {error, message, note, detail, output, stderr, reason} — `text` and `why` excluded.** Those are spec and decision prose out of phase.toml and project.toml, never subprocess-adjacent; including them adds seven not-derived declarations of pure noise. pathfence went deliberately wide because its over-reach was ambiguous; here the excluded set has provably different provenance.
- **Untagged exported string fields on serialized structs match by lowercased field name.** `mutation.Mutant` carries no tags at all, so a tag-only walk would exclude the type sitting closest to the tool output. One extra rule in the walker, one declaration.
- **gremlins gets a `headBuffer` tee it does not have today.** Without it "the byte count captured" is a constant 0 for half the adapters — a field that satisfies c-1 by never saying anything.
- **`mutation.Unmeasured.Message` is not declared.** It is printed to stderr and read by `survivor_drain`; it reaches no .dross artifact. The registry is persisted sinks only, and declaring a non-persisted field would blur what "declared" means.
