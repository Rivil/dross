# Panel synthesis — tool-output-not-persisted

Judged cold: I authored none of the three drafts. Every file path, symbol and
line reference below was checked against the tree before it was kept.

Both findings the verification planner flagged are TRUE:

- `internal/verify/verify.go:764` builds `Finding.Text` as
  `fmt.Sprintf("mutation adapter %s failed: %s", lr.Tool, lr.Error)` — `lr.Error`
  whole, with an in-code comment saying so deliberately. `internal/ship/body.go:92`
  renders `f.Text` into the PR body's `## Findings` section. So `verify.Finding.Text`
  is a THIRD persisted sink, already on the PR path c-5 covers, and it is tagged
  `toml:"text"`.
- `dross ship comment` (internal/cmd/ship.go:526-570) sets `co.Body = body` where
  `body` comes only from `--body` / `--body-file`. It is a pass-through, not a
  composer. `buildCommentOpts` (ship.go:59) is a config mapper and assigns no Body —
  mvp's "CommentOpts.Body composer at internal/cmd/ship.go:60" is not a thing.

Three further checks that moved the merge:

- `strykerHeadBytes = 64 << 10` and `headBuffer.Write` caps `buf` at `limit`, so
  `head.buf.Len()` saturates at 65536. risk's R3 is real; mvp's contract names
  `head.buf.Len()` as the byte count and would ship the lie.
- `internal/mutation/gremlins.go:216` sets `cmd.Stdout = os.Stderr` with no capture —
  gremlins has no headBuffer today.
- `mutation.StrykerNet` is a live adapter (`Name() == "stryker-net"`,
  constructed at `internal/cmd/verify.go:1136`, in `remoteAdapterOrder`), and
  `stryker_net.go:157,335` carry their own failure prose. Only risk covers it.

## Scores

Scored 1-5. One line per draft per dimension.

| Draft | Dimension | Score | Basis |
|---|---|---|---|
| risk | criteria coverage | 4 | 6/6, and the only draft covering stryker.net and `telemetry.Event.ErrorDetail`; but nothing converts `verify.go:565`'s `Error: err.Error()`, which its own t-6 walker arm would then fail on. |
| risk | test-contract specificity | 5 | Named fixtures, per-arm vacuity guards, and the only byte-count contract that catches the saturating `buf.Len()`. |
| risk | granularity | 4 | 7 single-concern tasks; t-3 fairly loaded (two failure paths + note + hint). |
| risk | wave correctness | 2 | t-6 (walker, wave 2) requires the verify-side assignment fix, which lands in t-5 (wave 3). Inverted. |
| mvp | criteria coverage | 2 | Misses stryker.net entirely; leaves `Finding.Text` undeclared AND excludes `text` from the tag vocabulary, so the confirmed third sink is invisible to both the c-4 walker and the c-5 test; cites a composer that does not exist. |
| mvp | test-contract specificity | 4 | Named test functions, real vacuity floors (<5 fields, <2 assignments), per-group composer counts — but the byte-count contract is wrong. |
| mvp | granularity | 3 | 5 tasks; t-2 is two adapters + a new gremlins tee + a source audit, t-3 is registry + walker + a verify.go change. Two oversized tasks. |
| mvp | wave correctness | 4 | Internally consistent; the verify.go conversion sits inside t-3 with the walker, so nothing is inverted. |
| verification | criteria coverage | 4 | 6/6, the only draft that finds `Finding.Text` and the only one that classifies `ship comment` correctly; misses stryker.net. |
| verification | test-contract specificity | 5 | Canary-absence matches c-2's wording verbatim; reflect pins make the constructor structurally unable to carry output; every AST scanner has a self-test with a written-down answer. |
| verification | granularity | 5 | 9 tasks, each one seam; the residual scan is separate for a stated reason (it would be red until both adapters land). |
| verification | wave correctness | 5 | t-5 (funnel) in wave 2 depending only on t-1; every wave-3 task depends on it. The one ordering all three criteria actually need. |

**Skeleton: verification.** It is the only draft whose dependency order is
sound (the verify-side funnel before the walker that polices it), its contracts
assert absence-of-canary rather than presence-of-record — which is c-2 verbatim —
and it is the only one that found the third sink. Its two gaps, stryker.net and
the saturating byte count, are exactly what risk supplies.

## Merged plan

9 tasks across 3 waves.

```
Wave 1

  t-1  Add the shared tool-failure recorder                    [verification+risk]
       files:    internal/mutation/toolfail.go,
                 internal/mutation/toolfail_test.go,
                 internal/mutation/stryker.go
       covers:   c-1, c-3
       desc:     toolfail.go holds the unexported record type and the constructor
                 RecordToolFailure(tool string, exit int, bytes int) plus the
                 carrier RecordLegError(err) used by t-5. Error() is fixed prose
                 naming the tool, its exit status, the bytes captured and the
                 stderr pointer; Unwrap() preserves the wrapped error's identity.
                 headBuffer MOVES here from stryker.go [risk], gains a `seen`
                 counter totalling every byte written (not the retained prefix)
                 and a printHead(w, n) replacing quote().
       contract: - a headBuffer with a 64 KiB limit fed 100 KiB reports 102400;
                   a record built from buf.Len() reads 65536 and fails [risk]
                 - reflect over the constructor's signature fails if any
                   parameter becomes string or []byte, and over the record type
                   if any field becomes exported — the only two routes by which
                   captured output could re-enter [verification]
                 - a canary fed through the buffer appears nowhere in the
                   returned error's text, at any truncation length [risk]
                 - Error() dropping the exit status, the byte count or the
                   stderr-pointer sentence fails [mvp]
                 - errors.Is(record wrapping sentinel, sentinel) holds, or the
                   RemoteTransport classification silently stops firing [mvp]
                 - printHead writes the identical 4-space-indented block quote()
                   produced, header line and continuation tail included [risk]
                 - a start failure (no *exec.ExitError) records "did not start",
                   not exit status -1 [risk]

  t-2  Declare the tool-text sink registry                     [all three]
       files:    internal/toolfence/fields.go,
                 internal/toolfence/fields_test.go
       covers:   c-4
       desc:     Mirrors pathfence.Fields: Field{Struct, Field, Tag, Artifact}
                 with exactly one disposition. THREE dispositions, not two
                 [verification]: Recorded{Carrier} for a field written only
                 through the carrier; NotToolStream{Why, Writers} for a
                 persisted text field provably not captured stream; and
                 Renderable{Why, SourcedFrom} for a field derived from a
                 Recorded one that a composer is allowed to print — the
                 disposition verify.Finding.Text needs. Entries: Recorded —
                 verify.LanguageRun.Error, verify.LegSummary.Error; Renderable —
                 verify.Finding.Text; NotToolStream — telemetry.Event.ErrorDetail
                 [risk], verify.SkippedFile.Reason [mvp],
                 verify.OutOfScopeMutant.Note, mutation.Mutant.Snippet,
                 mutation.Mutant.Note [risk].
       contract: - Validate, fed synthetic entries, names each of: no
                   disposition, more than one disposition, Recorded with a blank
                   Carrier, NotToolStream with an empty Why, NotToolStream naming
                   no Writer, Renderable with no SourcedFrom, and a duplicated
                   Name — so a Validate returning nil unconditionally cannot pass
                   [risk+verification]
                 - the by-name pin fails when any of LanguageRun.Error,
                   LegSummary.Error, Finding.Text or telemetry.Event.ErrorDetail
                   is deleted, and fails on a count check so a new entry cannot
                   be added unpinned [risk+verification]

Wave 2 (all depend t-1)

  t-3  Route stryker's failure paths through the recorder      [all three]
       files:    internal/mutation/stryker.go,
                 internal/mutation/stryker_failure_test.go
       depends:  t-1
       covers:   c-1, c-2, c-3
       desc:     The missing-report branch (stryker.go:142) stops building its
                 error from head.quote: it prints the head to os.Stderr at the
                 failure point via printHead and returns RecordToolFailure. The
                 initial-test truncation note stays as fixed prose on the record.
                 checkInstrumented (stryker.go:315) prints its head to stderr and
                 returns its dropped-path message WITHOUT the quote, keeping its
                 own prose [mvp] — see disagreement D5.
       contract: - with strykerBuildCmd stubbed to print CANARY-A, exit 3 and
                   write no report: the error contains "exit status 3" and NOT
                   CANARY-A, while captured stderr contains CANARY-A twice —
                   once teed live, once under the re-printed head banner
                   [verification]. Deleting the re-print fails the stderr half;
                   keeping the quote in the error fails the other.
                 - the truncation note still attaches when the head contains
                   strykerInitialTestFailureText and not when it doesn't [risk]
                 - checkInstrumented's error still names every dropped path and
                   still adds the "stryker said so itself" hint when the head
                   matched, while carrying no quoted output [risk]

  t-4  Route gremlins and stryker.net through the recorder     [risk+mvp]
       files:    internal/mutation/gremlins.go,
                 internal/mutation/stryker_net.go,
                 internal/mutation/gremlins_failure_test.go,
                 internal/mutation/stryker_net_failure_test.go
       depends:  t-1
       covers:   c-1
       desc:     gremlins' invocation-failed and reportless-exit paths and
                 stryker.net's invocation-failed and no-report paths return the
                 recorder's error instead of their own fmt.Errorf. gremlins gains
                 the headBuffer tee it does not have today [mvp] — currently
                 cmd.Stdout = os.Stderr with no capture, so its byte count would
                 otherwise be a structural 0 for the one adapter project.toml
                 actually configures. Launcher transport errors are untouched.
       contract: - errors.As(err, &record) succeeds on both gremlins fatal paths
                   and the message names the real exit status; reverting either
                   to a bare fmt.Errorf fails [risk+verification]
                 - the remote-ssh-could-not-start path still names the host and
                   satisfies errors.Is(err, remote.ErrTransport), so verify still
                   grades it BLOCKING rather than FLAG [risk+verification]
                 - if gremlins' output stops being teed its record reports 0
                   bytes and the test fails [mvp]
                 - stryker.net's no-report error names tool and exit status
                   through the recorder and no longer says "check stryker
                   config" [risk]

  t-5  Funnel leg errors through the recorder carrier          [verification+mvp]
       files:    internal/verify/verify.go,
                 internal/verify/legerror_test.go
       depends:  t-1
       covers:   c-1, c-4
       desc:     verify.go:565's `Error: err.Error()` becomes the carrier call,
                 and LegSummary.Error is populated from it. This is the task
                 risk's graph is missing: without it the c-4 walker's
                 assignment arm is red against the live tree.
       contract: - a reflect check on the carrier's Out(0) fails the moment it
                   reverts to string [verification]
                 - a leg failing with a non-tool error (an argfence refusal)
                   still records that error's text, asserted by substring, so
                   the funnel cannot swallow dross-authored diagnostics
                   [verification] — this is also what lets t-3 leave
                   checkInstrumented's own prose intact
                 - reverting the assignment to err.Error() fails the walker in
                   t-7 [mvp]

Wave 3

  t-6  Ban tool output inside error constructors               [verification]
       files:    internal/mutation/toolfence_residual_test.go
       depends:  t-3, t-4
       covers:   c-1
       desc:     AST scan over every non-test file in internal/mutation. Its own
                 task rather than folded into t-3/t-4 [verification]: folded, it
                 is red until the second adapter lands and gets loosened to pass
                 against the first.
       contract: - any argument of errors.New, fmt.Errorf or fmt.Sprintf that
                   contains a call to headBuffer.printHead or a read of head.buf
                   is reported
                 - a self-test drives the same scanner over an in-test fixture
                   where errors.New(h.printHead(...)) IS reported and
                   fmt.Fprint(os.Stderr, ...) is NOT, so a scanner that stopped
                   matching fails on a written-down answer instead of passing
                   vacuously [verification]

  t-7  Walk the declared sinks and their writers               [risk+verification]
       files:    internal/cmd/toolfence_enum_test.go,
                 internal/cmd/toolfence_carrier_test.go,
                 internal/cmd/testdata/toolfence/writer_ok.go.txt,
                 internal/cmd/testdata/toolfence/writer_raw.go.txt,
                 internal/cmd/testdata/toolfence/undeclared_sink.go.txt
       depends:  t-2, t-5
       covers:   c-4
       desc:     Placed in internal/cmd beside pathfence_enum_test.go /
                 pathfence_carrier_test.go / testdata/pathfence_scan, the shape
                 tracked-path-containment proved [risk+mvp] — see D2. Two-way
                 walk over ../verify, ../mutation, ../telemetry plus an
                 assignment guard. Tag vocabulary {error, message, note, detail,
                 output, stderr, reason, text} — `text` INCLUDED [verification],
                 since Finding.Text is tagged toml:"text"; untagged exported
                 string fields on serialized structs match by lowercased field
                 name, so mutation.Mutant (which carries no tags at all) is not
                 silently excluded [mvp].
       contract: - writer_raw, assigning a string literal to LegSummary.Error,
                   trips the guard; writer_ok, assigning the carrier's return,
                   does not [risk]
                 - undeclared_sink — a persisted struct with json:"stderr" and
                   no entry — trips the undeclared arm [risk]
                 - renaming verify.LanguageRun.Error without updating the
                   registry fails the stale-declaration arm [risk]
                 - an RHS that copies an already-declared field
                   (LegSummary.Error = lr.Error) is accepted, without a no-op
                   wrapper [mvp+verification]
                 - vacuity floor: fewer than three declared fields found, or
                   zero assignment sites seen, fails [verification+mvp]

  t-8  Prove the failed leg reaches disk clean                 [all three]
       files:    internal/verify/persist_toolfence_test.go
       depends:  t-3, t-5
       covers:   c-2
       desc:     A stryker leg stubbed to print CANARY-B and exit 1 without a
                 report, driven through RunScoped, Tests.Save, Skeleton and
                 Verify.Save into a t.TempDir(); the bytes are read back off
                 disk.
       contract: - tests.json AND verify.toml each contain "exit status 1", the
                   byte count and the stderr-pointer sentence, and neither
                   contains CANARY-B nor the string "the head of stryker's
                   output". Both asserted, so a fix applied to one writer and
                   not the other fails [verification]
                 - verify.toml's finding.text for that leg is exactly the
                   recorder line prefixed by "mutation adapter stryker failed: ",
                   so interpolating anything else into the finding fails here
                   [risk] — this is what makes Finding.Text's Renderable
                   disposition checkable rather than asserted
                 - RemoteTransport stays false, so the finding is FLAG and the
                   phase is not falsely marked BLOCKING [risk]
                 - if LegSummary.Error stops being populated from
                   LanguageRun.Error the test fails on verify.toml's empty
                   error field — a dead leg reading as a clean run [mvp]

  t-9  Enumerate PR and board body composers                   [all three]
       files:    internal/cmd/toolfence_composer_test.go,
                 internal/cmd/testdata/toolfence_composer/leaky_body.go.txt
       depends:  t-2, t-5
       covers:   c-5, c-6
       desc:     ONE file in internal/cmd, not verification's two — three of the
                 four composers (renderPhaseBody issue.go:1267, milestoneBody
                 issue.go:391, renderTaskBody issue_task.go:303) are unexported
                 in package cmd, and ship.BuildPRBody (body.go:20) is exported,
                 so a package-cmd test can call all four; a test in
                 internal/ship cannot call the other three. Both proof halves
                 run: every Recorded field is filled with its own canary on a
                 synthetic phase.Spec / verify.Verify / phase.Plan and each
                 composer is CALLED [verification], plus an AST scan listing
                 every func returning a string used as an issue/PR body and
                 failing on one absent from the table [risk+mvp].
                 `dross ship comment` is enumerated as a pass-through with a Why
                 — its body comes only from --body/--body-file [verification].
       contract: - no composer's output contains any canary; a composer
                   rendering a Renderable field (Finding.Text) is allowed and
                   does not trip it [verification+risk]
                 - the enumeration fails when the resolved composer set is
                   empty, counted PER GROUP so a vacuous board half cannot hide
                   behind a healthy PR half [mvp]
                 - a named composer that no longer resolves fails rather than
                   silently leaving the set — renaming BuildPRBody must break it
                   [risk]
                 - the leaky_body fixture, formatting leg.Error into a markdown
                   body, trips the scan [risk]
                 - the ship-comment pass-through assertion fails if that command
                   starts reading project state [verification]
```

### Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-1, t-3, t-4, t-5, t-6 |
| c-2 | t-3, t-8 |
| c-3 | t-1, t-3 |
| c-4 | t-2, t-5, t-7 |
| c-5 | t-9 |
| c-6 | t-9 |

6/6 criteria covered.

## Disagreements

**D1 — Where the recorder lives.**
risk and verification put it in `internal/mutation/toolfail.go`; mvp creates a new
leaf package `internal/toolfail` on the argument that an adapter package holding
verify-facing declarations is the shape pathfence avoided.
*Default: internal/mutation.* The adapters are the only producers, verify already
imports mutation, and mvp's cycle argument is about the REGISTRY, which D2 keeps
in a leaf anyway. Matters because it fixes the carrier name every registry entry
declares and every walker fixture asserts — changing it later rewrites t-2 and t-7.

**D2 — Where the registry and its walker live.**
risk and verification put the registry in a leaf `internal/toolfence`; mvp folds it
into `internal/toolfail` beside the recorder. Separately, verification puts the
walker/carrier tests in `internal/toolfence`, while risk and mvp put them in
`internal/cmd` beside `pathfence_enum_test.go`.
*Default: registry in internal/toolfence, walker tests in internal/cmd.* pathfence's
own header explains the split — `internal/cmd` imports the registry, so a test in
the registry package cannot import cmd back. Verification's counter (the carrier is
exported this time, so no cmd import is needed) is true today and stops being true
the first time a cmd-side composer or writer needs asserting. Matters because it is
the difference between reusing a proven fixture harness and building a second one.

**D3 — verify.Finding.Text.**
mvp leaves it out of the registry entirely ("safe by construction"); risk declares it
NotToolStream and pins its content in the persistence test; verification declares it
with an explicit renderable disposition carrying a Why.
*Default: verification's third disposition.* Verified: `verify.go:764` interpolates
`lr.Error` whole and `ship/body.go:92` prints it into the PR body, so it is a real
sink on the c-5 path. mvp's omission hides it; risk's NotToolStream label is false on
its face (it is derived from a Recorded field). Matters because getting this wrong
either deletes the Findings section from every PR body or leaves the leak's only
public exit undeclared.

**D4 — Whether `text` is in the tag vocabulary.**
mvp explicitly excludes `text` and `why` as spec/decision prose; risk's name-suffix
list omits it too; verification includes it.
*Default: include `text`.* `Finding.Text` is tagged `toml:"text"`, so excluding it
means the undeclared-sink arm cannot see D3's field. mvp's rationale is sound in the
abstract and wrong on this tree. Matters because it decides whether the c-4 guard can
detect the third sink at all.

**D5 — Does `checkInstrumented` return a record?**
risk routes it through the recorder; mvp routes only its quote to stderr and keeps the
dropped-path message, arguing the tool succeeded and the verdict is dross's own;
verification asserts the branch's stderr/error behaviour without saying which.
*Default: mvp.* A record would replace the dropped-path list, which is the entire
content of that failure, and t-5's carrier already passes non-record errors' text
through to the declared sink. Matters because risk's version silently costs the
diagnostic that failure exists to give.

**D6 — Where the byte count comes from.**
risk adds a `seen` counter totalling every byte written; mvp's contract names
`head.buf.Len()`; verification leaves it unspecified.
*Default: risk's counter.* Verified: `strykerHeadBytes = 64 << 10` and `Write` stops
appending at the cap, so `buf.Len()` reads 65536 on every run over 64 KiB. Matters
because the number is one of the four things c-1 requires the record to carry, and
mvp's version reports a constant.

**D7 — Does gremlins get a headBuffer tee?**
mvp adds one; risk and verification route gremlins through the recorder without
capturing anything.
*Default: add it.* Verified: gremlins sets `cmd.Stdout = os.Stderr` with no capture,
so without a tee its record reports 0 bytes forever — and gremlins is the only
adapter `project.toml` configures, so c-1's byte-count clause would be vacuous
exactly where it is exercised. Counter-argument, recorded: it is the only behaviour
change in the merged plan to an adapter that is not leaking anything today.

**D8 — Is stryker.net in scope?**
Only risk includes it; mvp and verification cover stryker and gremlins only.
*Default: in scope (folded into t-4).* Verified: `mutation.StrykerNet` is a live
adapter — `Name() == "stryker-net"`, constructed at `internal/cmd/verify.go:1136`,
listed in `remoteAdapterOrder` — with its own failure prose at `stryker_net.go:157`
and `:335`. c-1 says *every* adapter that can fail with a tool error. Matters because
leaving it out makes c-1 false on a technicality that a reader of the criterion would
call a miss.

**D9 — Is the residual AST scan its own task?**
verification makes it t-6; mvp folds it into the adapter-routing task's contract as a
"source audit"; risk has no residual scan at all.
*Default: its own task.* verification's reason holds — folded, it is red until the
second adapter lands, and a test written to pass against one adapter gets loosened
rather than fixed. Matters less than the others; it is a sequencing preference, and
folding it would cost one task, not a guarantee.

**D10 — How the composer proof works.**
risk and mvp scan the composers' ASTs for references to declared fields; verification
fills every declared field with a canary and CALLS each composer, asserting absence.
*Default: both halves in one task.* The canary execution proves no leak today; the AST
scan proves the composer table has not gone stale. Either alone has a known blind spot
(an AST scan misses an indirect render; an execution harness misses a composer nobody
added to the table). Matters because c-5 and c-6 both demand the enumeration fail when
it enumerates none — only the AST half can detect that.

**D11 — Is `telemetry.Event.ErrorDetail` declared?**
risk declares it, on the grounds that it carries `err.Error()` to a persisted jsonl;
mvp and verification omit it.
*Default: declare it (NotToolStream, with the redaction/classification path as its
Why).* Verified: `telemetry.go:69` persists `err_detail` to disk. Matters because
scoping the registry to tests.json and verify.toml is the "one field over" gap
pathfence was written to close — and c-4 says *every* persisted schema field that may
carry tool-derived text.
