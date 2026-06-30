# Verification-lens plan — architecture-doc-enhancements

Designed backward from test contracts. For each criterion I wrote the ideal
test first, then carved out the smallest Go surface that makes it mechanically
satisfiable. Prompt-only behaviour (execute/ship/architecture markdown) is
pinned by prompt-grep tests; the real logic lives in Go functions the tests can
call directly (the landmark model, the link resolver, the merge function, the
`--fix` rewriter).

```
Phase architecture-doc-enhancements — 8 tasks across 2 waves

Wave 1
  t-1  Add typed Landmark model to changes
       files:    internal/changes/changes.go, internal/changes/changes_test.go
       covers:   c-1
       contract: ParseLandmark(["feature=auth","symbol=Foo","loc=a.go:10","what=does x"])
                 returns Landmark{Feature:"auth",Symbol:"Foo",Loc:"a.go:10",What:"does x"};
                 a TaskRecord.Landmarks []Landmark survives Save→Load byte-for-field;
                 a kv list omitting fe= returns an error. If the struct, the
                 key=value parse, or the JSON round-trip breaks,
                 TestParseLandmark / TestLandmarkRoundTrip fail.

  t-2  Parse ARCHITECTURE.md and resolve symbol links
       files:    internal/architecture/links.go, internal/architecture/links_test.go
       covers:   c-3, c-4
       contract: ParseDoc(md) returns one Entry per "### " heading carrying its
                 OneLine, []SymbolLink{Symbol,File,Line} bullets, and provenance;
                 ResolveLink(link, codex.Symbol set) returns Moved(realLine) when the
                 symbol exists at a different line, Missing when the symbol is gone,
                 OK when the line matches. If bullet parsing or any of the three
                 statuses is wrong, TestParseDocBullets / TestResolveMoved /
                 TestResolveMissing / TestResolveOK fail.

  t-7  Switch execute + ship prompts to typed --landmark
       files:    assets/prompts/execute.md, assets/prompts/ship.md,
                 internal/cmd/execute_prompt_test.go, internal/cmd/ship_prompt_test.go
       covers:   c-1
       contract: execute.md emits `dross changes record ... --landmark feature=…
                 --landmark symbol=… --landmark loc=… --landmark what=…` and no longer
                 routes the landmark through --notes; ship.md §3.5 reads the structured
                 landmark fields off `dross changes show`. A prompt-grep test fails if
                 execute.md still carries the landmark in --notes or omits --landmark
                 (TestExecutePromptEmitsTypedLandmark), and if ship §3.5 no longer
                 references the typed fields (TestShipPromptReadsStructuredLandmarks).

  t-8  Lift first-creation guard in architecture prompt
       files:    assets/prompts/architecture.md,
                 internal/cmd/architecture_prompt_test.go
       covers:   c-2
       contract: architecture.md §0.3 no longer tells the agent to "stop" when
                 ARCHITECTURE.md exists; it instructs a per-entry merge keyed by
                 feature heading that preserves hand edits. A new prompt-grep test
                 (TestArchitecturePromptRegeneratesOverExisting) fails if the
                 "first-creation only / stop" guard text is still present or the
                 regenerate-over-existing instruction is absent.

Wave 2 (depends t-1, t-2)
  t-3  Feature-keyed landmark merge function
       files:    internal/architecture/merge.go, internal/architecture/merge_test.go
       covers:   c-1, c-2
       depends:  t-1, t-2
       contract: MergeLandmarks(doc, []changes.Landmark) -> (string, []string warnings)
                 per refresh_merge_strategy: a landmark whose feature equals an existing
                 heading refreshes that entry's symbol bullets + provenance in place and
                 adds NO second heading; a landmark with a new feature appends one entry;
                 an empty `what` leaves the existing one-line untouched; an existing entry
                 the landmark set didn't cover is returned in warnings and NOT removed.
                 TestMergeInPlace / TestMergeNewEntry / TestMergePreservesOneLine /
                 TestMergeFlagsUnseenEntry fail on any deviation.

  t-4  Add `dross architecture check --fix` command
       files:    internal/architecture/fix.go, internal/architecture/fix_test.go,
                 internal/cmd/architecture.go, cmd/dross/main.go
       covers:   c-4
       depends:  t-2
       contract: RewriteMovedLinks(doc, resolutions) rewrites every Moved link to the
                 symbol's real line in place and leaves Missing (renamed/deleted) bullets
                 byte-identical; the cobra `architecture check` lists stale links and with
                 --fix writes the repaired doc and exits 0. TestRewriteMovedSkipsMissing
                 fails if a deleted-symbol bullet is altered or a moved line is not
                 corrected; a cmd test over a temp repo (TestArchitectureCheckFixRewrites)
                 fails if `architecture check --fix` doesn't update the stale line on disk.

  t-6  Add advisory stale-link section to doctor
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
       covers:   c-3
       depends:  t-2
       contract: doctor gains an "Architecture links:" section that runs the resolver and
                 prints ⚠ per stale/missing link, but does NOT increment `issues`
                 (link_check: never blocks). TestDoctorStaleLinkAdvisoryDoesNotBlock fails
                 if a stale link is printed yet finalizeDoctor returns a non-nil error /
                 the issue count rises above what the rest of the repo state produced.
```

## Coverage

| criterion | tasks |
|-----------|-------|
| c-1 (typed --landmark, execute emits, ship reads fields) | t-1, t-7, t-3 |
| c-2 (regenerate over existing, merge without clobbering) | t-8, t-3 |
| c-3 (doctor advisory stale-link report)                  | t-2, t-6 |
| c-4 (`architecture check --fix` re-resolves moved lines) | t-2, t-4 |

All of c-1..c-4 accounted for. Every criterion has at least one Go-testable
surface (t-1/t-2/t-3/t-4/t-6); the two prompt-contract tasks (t-7, t-8) are
each pinned by a prompt-grep assertion so they're not "trust me" edits.

## Judgment calls

- Extracted a real Go `MergeLandmarks` function (t-3) rather than leaving the
  refresh_merge_strategy as prose in ship.md/architecture.md. Rejected the
  prompt-only route because that locked decision has four falsifiable clauses
  (in-place refresh, keep one-line, append new, flag-don't-drop) and a function
  is the only way to make them mechanically checkable. The prompts then describe
  the same semantics; the function is the source-of-truth contract.
- Made t-2 own a single `ParseDoc` that returns entries-with-bullets, shared by
  the resolver (c-3/c-4), the merge (t-3) and the rewriter (t-4). Rejected
  giving each consumer its own ad-hoc heading/bullet scanner — duplicate parsers
  would let the doc-shape contract drift between doctor, --fix and merge.
- Put the doctor stale-link contract on the `issues` counter specifically
  (t-6), not just "prints a warning". The link_check decision says never block;
  the only way that's falsifiable is asserting the exit status / issue count is
  unaffected, so the test targets finalizeDoctor's return, not stdout text.
- Kept t-4 as one task spanning internal/architecture + internal/cmd + the
  one-line main.go registration. Rejected splitting the rewriter from the
  command: the command is a thin wire over RewriteMovedLinks, and a separate
  registration task would be sub-10-min busywork the granularity rule says to
  merge.
- Placed t-7 and t-8 (prompt edits) in wave 1, parallel to the Go work, not
  gated behind it. The flag name `--landmark` and its keys are fixed by the
  landmark_field_shape locked decision, so the prompt text references a settled
  contract and its grep-test is self-contained — no need to wait on t-1's
  binary.
- Honoured legacy_notes (clean break): t-7 stops execute from routing landmarks
  through --notes, and t-1 leaves the free-form Notes field intact but unparsed.
  Rejected adding any --notes→landmark back-compat parser, which the locked
  decision forbids.
```

verification: 8 tasks across 2 waves, criteria covered 4/4
```
