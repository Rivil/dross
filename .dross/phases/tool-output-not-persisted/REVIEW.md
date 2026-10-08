# Plan Review — tool-output-not-persisted

Reviewed: 2026-09-09 (second pass, post-rewrite)
Plan: 9 tasks across 3 waves

## BLOCKING

- [coverage] t-7's walk roots (`../verify`, `../mutation`, `../telemetry`) exclude
  `internal/cmd` — the package the walker itself lives in and the package that
  holds a large share of the declared sinks' live writers. Verified against the
  tree:
    - `internal/cmd/verify.go:646` constructs a `verify.LanguageRun` (the struct
      carrying the Recorded field `Error`) on the detached-collect path.
    - `internal/cmd/verify.go:825` assigns `verify.SkippedFile.Reason` — a
      declared field.
    - `internal/cmd/telemetry.go:61` assigns `telemetry.Event.ErrorDetail` — a
      declared field, and its ONLY writer in the tree.
    - `internal/cmd/verify.go:1514,1521` append `verify.Finding` values.
  So three of t-2's seven declared entries have writers the assignment guard
  never sees, and t-7's stale-declaration and `NotToolStream{Writers}` arms are
  asserting against a tree half of whose writers are out of frame. c-4's second
  half — "a new writer assigning raw text to a declared field … fails the
  enumerating test" — is false for the one package most likely to gain such a
  writer. This is the same shape as the prior review's BLOCKING (a live path
  outside the guard), and the fix is small: the walker runs in `internal/cmd`,
  so adding `.` to the roots costs one entry.
  Suggestion: add `internal/cmd` (i.e. `.`) to t-7's roots and to t-2's scope
  statement, or state in t-7 why a writer in `internal/cmd` is out of scope when
  `verify.go:646`, `verify.go:825` and `telemetry.go:61` are exactly that.

## FLAG

- [test-contract] t-1's reflect contract contradicts the constructor it is
  written against. The signature is `RecordToolFailure(tool string, exit int,
  c capture)` and the contract says the reflect check "fails if any parameter
  becomes string or []byte" — `tool string` is already a string parameter, so
  the test as specified fails on landing. The intent is presumably "no
  ADDITIONAL string/[]byte parameter beyond the tool name".
  Suggestion: word the contract as a pin on the exact parameter list (tool
  string, exit int, capture) rather than a blanket type ban.

- [coverage] The existing stryker tests that pin today's error prose are in no
  task's `files`, and one of them asserts the exact behaviour c-2 inverts:
    - `internal/mutation/stryker_test.go:749` (TestStrykerReportlessErrorNames-
      HeadOfOutput) asserts the tool's cause line IS inside `err.Error()`.
      After t-3 it must assert the cause is in captured stderr instead — this is
      the c-3 pin being rewritten, and rewriting it unnamed is how a pin gets
      loosened rather than moved.
    - `stryker_test.go:454,457` assert `"did not write a report"` and that the
      error names `s.reportPath()`. With the constructor's string ban (above),
      the record cannot carry the path, so the missing-report diagnostic either
      loses the path or the adapter must wrap the record — t-3 says "returns
      RecordToolFailure", which is the former, and the plan never says so.
    - `stryker_test.go:948` caps the number of newlines in the error; after t-3
      it passes vacuously.
  t-3's files are `stryker.go` plus a new `stryker_failure_test.go` only.
  Suggestion: add `internal/mutation/stryker_test.go` to t-3 and say which
  assertions move to stderr, plus whether the expected report path survives in
  the error.

- [antipattern/files] t-4's stryker.net mechanism does not work as described.
  "hoist exitErr out of its block scope so the no-report site at
  stryker_net.go:335 has an exit status to record" — line 335 is inside
  `findReport` (`stryker_net.go:302`), a package-level function with no access
  to `Run`'s `exitErr` no matter where it is declared, and one that is called
  directly by `stryker_net_test.go:93,104` and indirectly by
  `launcher_test.go:299`. The exit status can only be attached at the caller,
  `stryker_net.go:168`, which also returns findReport's other two errors (walk
  failure, dir-not-exist) and so needs discrimination. Additionally
  `stryker_net_test.go:105` pins `"no mutation-report.json"` and is not in
  t-4's files.
  Suggestion: name the call site (stryker_net.go:168) as the recording point,
  say what happens to findReport's other two error paths, and add
  `stryker_net_test.go` to the files.

- [coverage] `[]string` persisted text fields are invisible to both the registry
  and the walker. `verify.Scope.Degraded` (`internal/verify/scope.go:53`,
  `json:"degraded"`) is persisted into tests.json, is populated at
  `internal/cmd/verifyscope.go:43,50,66,82,89` from `gitReason(err)` —
  subprocess errors — and is interpolated into `Finding.Text` at
  `internal/verify/verify.go:857-861`, which `ship.BuildPRBody` renders into the
  PR body. It carries no tool output today only because `exec.ExitError.Error()`
  omits the stderr that `gitTrim`'s `.Output()` captured
  (`internal/cmd/ship_recover.go:267`) — a property of the stdlib, not a
  declaration. t-7's rule covers "exported string fields" and an exact
  vocabulary with no `degraded` in it, so neither arm can ever see it.
  Suggestion: either declare Scope.Degraded (NotToolStream, with the
  ExitError-omits-stderr reason recorded) and say whether `[]string` fields are
  walked, or state in t-2's scope comment that slice-of-string sinks are out of
  scope and why.

- [coverage] t-9's composer enumeration misses live body sites, and its AST arm
  cannot catch them because they are not funcs:
    - `internal/cmd/milestone.go:281` — `opts.Body = fmt.Sprintf(...)`, the
      milestone integration PR body.
    - `internal/cmd/issue.go:1337` — `Body: fmt.Sprintf(...)` on a quick-task
      board issue.
    - `internal/cmd/issue.go:609` and `:785` — backlog/someday item bodies, fed
      to `CreateIssue`/`UpdateIssue` at `:928`, `:953`, `:967`.
  All are literal today, so c-5/c-6 hold in fact; the guard is what is short.
  "every func returning a string used as an issue or PR body" resolves none of
  them, and the named-four half plus a per-group count both stay green while
  four real body sites are unwatched.
  Suggestion: make the AST arm key on the sink (`forge.IssueInput.Body`,
  `forge.IssuePatch.Body`, `ship.OpenOpts.Body`, `co.Body`) rather than on
  composer function shape, or enumerate these four sites explicitly.

- [test-contract] t-7 never pins the disposition-aware arm — the one a future
  author would widen. Every contract line covers Recorded (`writer_raw` trips,
  `writer_ok` passes) or the undeclared/stale/vocabulary arms; nothing asserts
  that a raw `fmt.Sprintf` assigned to a NotToolStream field is ACCEPTED, or how
  a Renderable field's writer is treated. This is not hypothetical:
  `internal/verify/lifecycle.go:127,136,139,146,151` assign raw strings to
  `Mutant.Note`, a declared field inside a walked root, and only the exemption
  keeps the walker green there. Unpinned, the cheapest way to make a future red
  go away is to reclassify the leaking field as NotToolStream.
  Suggestion: add a contract line pinning the exemption from both sides — a raw
  literal into a NotToolStream field passes, and flipping a Recorded entry to
  NotToolStream to silence `writer_raw` fails.

- [test-contract] t-1's "printHead writes the identical 4-space-indented block
  quote() produced, header line and continuation tail included" cannot hold once
  printHead is shared. `quote()`'s header is the hardcoded `"the head of
  stryker's output, which is where the cause is:"` (`stryker.go:212`), and t-4
  routes gremlins and stryker.net through the same helper.
  Suggestion: pin the header as tool-parameterised — identical shape, tool name
  substituted — and keep the byte-for-byte identity assertion for the stryker
  case only.

## NOTE

- [test-contract] Two of t-2's declared NotToolStream entries have no Go writer
  at all: `verify.CriterionResult.Notes` and (as a persisted sink)
  `mutation.Mutant.Snippet` is written only at `internal/mutation/stryker.go:565`
  from the parsed report, while `CriterionResult.Notes` is authored by the agent
  editing verify.toml. t-2's Validate requires `NotToolStream` to name at least
  one Writer, so those entries will name something that is not code. Worth
  saying in the doc comment what "Writer" means for an agent-authored field, so
  the stale-declaration arm is not expected to resolve it.

- [coverage] Telemetry's mutation bucket keys on message substrings —
  `internal/telemetry/telemetry.go:316`: `{"stryker"}, {"gremlins"},
  {"mutation adapter"}`. The recorder naming the tool preserves it, but nothing
  in the plan pins that link, and `telemetry_test.go:218` drives the classifier
  with hand-written literals rather than a real adapter error, so a rewording
  that drops the tool name would silently push these into the `other` bucket.

- [granularity] t-5's description reads as if it introduces
  `LegSummary.Error` being populated from `LanguageRun.Error`; that already
  happens at `internal/verify/verify.go:693`. Only the `err.Error()` →
  `RecordLegError(err).String()` change at `verify.go:565` is new. t-8's
  "if LegSummary.Error stops being populated" line is still a useful regression
  pin.

- [strengths] The rewrite absorbed the prior review rather than paraphrasing it:
  t-4 now names three gremlins sites with the detached one carrying
  `NotCaptured` instead of a structurally false `0 bytes`, t-9 dropped to wave 2
  with its real dependency, and the Renderable exception was pushed up into a
  locked spec decision (`renderable_derivation`) with t-8's exact-composition pin
  as its check. That is the right direction for an exception: checkable, not
  asserted.

- [strengths] Contracts remain unusually falsifiable — most lines name the
  mutation that must fail ("substituting Observed(0) fails", "renaming
  BuildPRBody must break it", "block-scoping exitErr again loses the exit
  status") — and three tasks carry vacuity floors. t-6's self-test over a
  written-down fixture is still the right shape for an AST scanner.

- [locked-decisions] No task contradicts a locked decision. `cut_point`,
  `stderr_pointer`, `terminal_requote` and `no_migration` are implemented
  literally, and `renderable_derivation` is honoured exactly as written: t-2
  declares `verify.Finding.Text` Renderable, t-9 allows a composer to render it,
  and t-8 pins verify.toml's `finding.text` to the recorder line under the
  `mutation adapter <tool> failed: ` prefix — which is the composition at
  `internal/verify/verify.go:764`.

- [coverage] All six criteria are covered: c-1 (t-1, t-3, t-4, t-5, t-6), c-2
  (t-3, t-8), c-3 (t-1, t-3), c-4 (t-2, t-5, t-7), c-5 and c-6 (t-9).

- [forbidden-actions] Nothing violates `.dross/rules.toml` r-01 — no `assets/`
  prompt edits, so no `make install` dependency — and every task runs under the
  native `go test` runtime. No global rules file exists at
  `~/.claude/dross/rules.toml`.

- [granularity/wave-order] No split or merge candidates: the largest task (t-4)
  is four files in one package, and t-7's five files are three fixtures plus a
  test pair. Every wave-N+1 task consumes a wave-N output — t-6 needs both
  adapters routed, t-7 needs the registry and the carrier, t-8 needs the routed
  stryker path and the carrier.

## Prior review disposition

- BLOCKING (gremlins detached-collect fatal path) — ADDRESSED. t-4 now names
  three sites including `Gremlins.Collect`'s `gremlins.go:566`, records
  `NotCaptured` there, and its contract fails if `Observed(0)` is substituted.
- FLAG (CriterionResult.Notes missing / matching rule unstated) — ADDRESSED.
  t-2 declares it NotToolStream; t-7 states EXACT tag matching with `notes` and
  `text` in the vocabulary and asserts exactness with a `footnote` counter-case.
- FLAG (carrier return type vs serialized string field) — ADDRESSED. t-5 names
  the RHS as `RecordLegError(err).String()`, keeps the field `string` on the
  pathfence precedent, and t-7's accepted arm now covers a method call or
  conversion wrapping the carrier's return.
- FLAG (walk roots narrower than c-4) — PARTIALLY ADDRESSED. The agent-authored
  ledgers (security/quality/techdebt) are now excluded by an asserted scope
  statement in t-2 and mirrored in t-7. But the declared scope is itself wrong:
  it omits `internal/cmd`, which holds three of the seven declared fields'
  writers. See BLOCKING.
- FLAG (stryker.net tee + exitErr scope) — PARTIALLY ADDRESSED. The tee is in,
  and the symmetric byte-count assertion is in; the exit-status mechanism as
  described does not compile-follow, since `stryker_net.go:335` is inside
  `findReport`. See FLAG above.
- FLAG (ErrRemoteCommand sentinel unpinned) — ADDRESSED. t-4's third contract
  line asserts `errors.Is(err, remote.ErrRemoteCommand)` on a reportless-exit
  record, and the fourth keeps `ErrTransport` grading BLOCKING.
- FLAG (t-9 in the wrong wave) — ADDRESSED. t-9 is wave 2, `depends_on = ["t-2"]`,
  with the reason written into its description.
- FLAG (byte count says "captured" but means "seen") — ADDRESSED. t-1 pins
  Error() to say the number is tool output OBSERVED, not retained, and explains
  why in the contract line itself.
- FLAG (Renderable exception unauthorized by the spec) — ADDRESSED. The spec
  gained the locked `renderable_derivation` decision, and t-8 carries the pin
  the decision says it rests on.

## Summary

The rewrite closed every prior finding it could close and the contracts remain
the strongest part of the plan, but the enforcement scope is still drawn one
package too small: t-7 never walks `internal/cmd`, where three declared sinks
are actually written, and the composer and `[]string` blind spots mean two of the
three guards prove less than their criteria claim.
