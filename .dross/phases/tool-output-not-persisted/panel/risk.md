# Panel draft — RISK LENS

Phase tool-output-not-persisted — 7 tasks across 3 waves

The failure modes this graph assigns owners to:

| # | Risk | Owner |
|---|---|---|
| R1 | stryker's missing-report path writes 40 lines of tool stream into tests.json + verify.toml | t-3, t-5 |
| R2 | `checkInstrumented` does the same thing at a second, quieter call site | t-3 |
| R3 | the byte count lies — headBuffer caps at 64 KiB, so "bytes captured" reads 65536 on every large run | t-1 |
| R4 | the live diagnostic regresses: the cause stops being readable at the failure point | t-1, t-3 |
| R5 | the other adapters keep formatting their own failure prose, so the next leak lands in gremlins | t-4 |
| R6 | transport errors get re-labelled tool failures, and `errors.Is(err, remote.ErrTransport)` stops holding — an unreachable host becomes a FLAG instead of BLOCKING | t-4 |
| R7 | a new writer assigns raw text to a declared sink; a new sink is added with no declaration; a declaration outlives its field | t-2, t-6 |
| R8 | a safe record is laundered outward — republished into a PR body or a board issue where the blast radius is the whole internet | t-7 |

---

Wave 1

  t-1  Add the shared tool-failure recorder
       files:    internal/mutation/toolfail.go, internal/mutation/toolfail_test.go,
                 internal/mutation/stryker.go
       covers:   c-1, c-3
       desc:     New toolfail.go holds `toolFailure{Tool, Stage, ExitStatus, Bytes}` and
                 `recordToolFailure`, which prints the retained head to stderr and returns a
                 fixed-prose error naming tool, exit status, bytes captured and the stderr
                 pointer. headBuffer moves here from stryker.go, gains a `seen` counter that
                 totals every byte written (not merely the retained prefix) and a
                 `printHead(w, n)` replacing `quote`.
       contract: - a headBuffer with a 64 KiB limit fed 100 KiB records Bytes = 102400; a
                   record built off buf.Len() reads 65536 and fails
                 - a canary line fed through the buffer appears nowhere in the returned
                   error's text, at any truncation length
                 - printHead writes the identical 4-space-indented block the removed `quote`
                   produced, header line and the "… (output continues above)" tail included,
                   for a >40-line stream
                 - a start failure (no *exec.ExitError, exit -1) records "did not start"
                   rather than an exit status of -1

  t-2  Declare the tool-text sink registry
       files:    internal/toolfence/fields.go, internal/toolfence/fields_test.go
       covers:   c-4
       desc:     Mirrors pathfence's registry-plus-Validate shape. Two dispositions:
                 `Recorded{Carrier}` for a field written only from the shared recorder, and
                 `NotToolStream{Why, Writers}` for a field that is persisted and text-shaped
                 but provably not captured stream (parsed report values, classified prose).
                 Entries: verify.LanguageRun.Error, verify.LegSummary.Error,
                 verify.Finding.Text, telemetry.Event.ErrorDetail, mutation.Mutant.Snippet,
                 mutation.Mutant.Note, verify.OutOfScopeMutant.Snippet,
                 verify.OutOfScopeMutant.Note.
       contract: - Validate rejects, as synthetic entries, each of: both dispositions set,
                   neither set, Recorded with a blank Carrier, NotToolStream with an empty
                   Why, NotToolStream naming no Writer, and a duplicated Name — so a Validate
                   that returned nil unconditionally cannot pass
                 - the by-name pin fails when verify.LanguageRun.Error or
                   telemetry.Event.ErrorDetail is deleted from the registry, and fails on the
                   count check when an entry is added without being pinned by name

Wave 2 (depends on wave 1)

  t-3  Route stryker's two failure paths through the recorder
       files:    internal/mutation/stryker.go, internal/mutation/stryker_test.go
       covers:   c-1, c-2, c-3
       depends:  t-1
       desc:     The missing-report branch and checkInstrumented both stop building an error
                 string from the head. Each prints the head to stderr at the failure point and
                 returns recordToolFailure's error; the truncation note and the
                 "did not result in any files" hint stay as fixed prose on the structured
                 record.
       contract: - missing-report: err.Error() contains no line of the fed stream and does
                   contain the tool name, exit status, byte count and the stderr-pointer
                   sentence; the same canary IS present in the captured stderr
                 - the initial-test truncation note still attaches when the head contains
                   strykerInitialTestFailureText and does not attach when it doesn't
                 - checkInstrumented's error still names every dropped path and still adds the
                   "did not result in any files" hint when the head matched, while carrying no
                   quoted output

  t-4  Route gremlins and stryker.net through the recorder
       files:    internal/mutation/gremlins.go, internal/mutation/stryker_net.go,
                 internal/mutation/gremlins_test.go, internal/mutation/stryker_net_test.go
       covers:   c-1
       depends:  t-1
       desc:     Both adapters' tool-failure sites (gremlins' invocation-failed and
                 reportless-exit paths, stryker.net's invocation-failed and no-report paths)
                 return recordToolFailure instead of their own fmt.Errorf. Launcher transport
                 errors are untouched.
       contract: - gremlins' invocation-failed error and its reportless-exit error both carry
                   the recorder's stderr-pointer sentence and the recorded exit status;
                   reverting either to a bare fmt.Errorf fails
                 - the remote-ssh-could-not-start path still names the host and satisfies
                   errors.Is(err, remote.ErrTransport), so verify still grades it BLOCKING
                   rather than FLAG
                 - stryker.net's no-report error names tool and exit status through the
                   recorder and no longer says "check stryker config"

  t-6  Walk the declared sinks and their writers
       files:    internal/cmd/toolfence_enum_test.go,
                 internal/cmd/testdata/toolfence/writer_ok.go.txt,
                 internal/cmd/testdata/toolfence/writer_raw.go.txt,
                 internal/cmd/testdata/toolfence/undeclared_sink.go.txt
       covers:   c-4
       depends:  t-1, t-2
       desc:     go/ast scan over the persisted-schema packages. Three arms: an assignment to
                 a Recorded field whose RHS is not the recorder call fails; an exported,
                 tagged, text-shaped field (tag `error`/`detail`/`stderr`/`output`, or a name
                 ending Error/Output/Stderr/Snippet) on a persisted struct with no registry
                 entry fails; a registry entry naming a field reflection can no longer find
                 fails as stale.
       contract: - the writer_raw fixture, assigning a string literal to LegSummary.Error,
                   trips the walker; writer_ok, assigning recordToolFailure's return, does not
                 - the undeclared_sink fixture — a persisted struct with `json:"stderr"` and no
                   entry — trips the undeclared arm
                 - renaming verify.LanguageRun.Error without updating the registry fails the
                   stale-declaration arm
                 - the scan asserts a non-zero per-package visited count, so a walker aimed at
                   an empty package list cannot pass green

  t-7  Enumerate PR and board composers
       files:    internal/cmd/toolfence_composer_test.go,
                 internal/cmd/testdata/toolfence_composer/leaky_body.go.txt
       covers:   c-5, c-6
       depends:  t-2
       desc:     One AST scan over the four body composers — ship.BuildPRBody plus
                 cmd.renderPhaseBody, cmd.renderTaskBody, cmd.milestoneBody — asserting none
                 references a field the registry marks Recorded. Composer set is named
                 explicitly and each name is resolved in the tree, so a renamed composer fails
                 rather than silently leaves the set.
       contract: - the enumeration fails when the resolved composer set is empty, and fails
                   when a named composer no longer exists (renamed BuildPRBody must break it,
                   not shrink it)
                 - the leaky_body fixture, formatting `leg.Error` into a markdown body, trips
                   the scan; a composer rendering Finding.Text does not
                 - adding `v.Summary.Legs[i].Error` to BuildPRBody's Efficacy section fails
                   this test against the live tree

Wave 3 (depends on wave 2)

  t-5  Prove the failed leg reaches disk clean
       files:    internal/verify/toolfail_persist_test.go, internal/verify/verify.go
       covers:   c-2
       depends:  t-1, t-3
       desc:     End-to-end test: run verify with a stryker adapter pointed at a report path
                 that never appears, write both artifacts, read the bytes back off disk.
                 verify.go changes are comment-level plus pinning the adapter-failure finding
                 to the recorder line rather than free interpolation.
       contract: - the written tests.json's languages[].error and the written verify.toml's
                   leg.error each contain the tool name, exit status, byte count and the
                   stderr-pointer sentence
                 - neither file's bytes contain the canary line nor the string
                   "the head of stryker's output"
                 - verify.toml's finding.text for that leg is exactly the recorder line
                   prefixed by "mutation adapter stryker failed: ", so an interpolation of
                   anything else into the finding fails here
                 - RemoteTransport stays false for this failure, so the finding is FLAG and
                   the phase is not falsely marked BLOCKING

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-1, t-3, t-4 |
| c-2 | t-3, t-5 |
| c-3 | t-1, t-3 |
| c-4 | t-2, t-6 |
| c-5 | t-7 |
| c-6 | t-7 |

6/6 criteria covered.

## Judgment calls

- **The record is one string, not new structured columns.** Exit status, bytes and the
  pointer live inside the recorder's fixed prose written to the existing Error fields.
  Rejected: adding ExitStatus/Bytes fields to LanguageRun and LegSummary — that triples the
  declared sink count and gives the c-4 walker three fields to police per leg instead of one,
  for a value nothing reads programmatically.
- **verify.Finding.Text stays renderable in a PR body.** It is declared NotToolStream, not
  Recorded, and t-5 pins its content to the recorder line. Rejected: declaring it Recorded,
  which would make c-5 delete the Findings section from every PR body — a real diagnostic
  loss to buy a guarantee t-5's pin already gives.
- **Launcher transport errors are out of scope.** They name ssh, a host and an exit code, and
  none of them touch a captured stream. Folding them into the recorder would flatten
  remote.ErrTransport, which verify reads to grade a leg BLOCKING rather than FLAG — R6.
- **headBuffer moves into toolfail.go rather than staying in stryker.go.** The recorder needs
  the byte total and the head printer, and leaving the type in an adapter file is what made
  "quote it into the error" the obvious thing to do twice. Rejected: passing counts as loose
  ints, which lets a caller pass buf.Len() and re-create R3.
- **mutation.Mutant.Snippet is declared, not ignored.** It is tool-derived text that reaches
  tests.json, just not captured *stream*. Declaring it NotToolStream with a Why puts that
  distinction somewhere a reader can dispute; leaving it out of the registry would make the
  walker's undeclared arm carve it out silently.
- **telemetry.Event.ErrorDetail is in the registry.** It carries err.Error() to a persisted
  jsonl, so a verify failure telemetered today is a second copy of the same leak. Rejected:
  scoping the registry to tests.json and verify.toml, which is exactly the "one field over"
  gap pathfence was written to close.
- **The composer test is one task, not two.** c-5 and c-6 are the same AST scan over two name
  lists; splitting them would produce two files that must agree on the declared-field
  predicate.
