# review-edge-lenses — risk-first draft

Lens: every task owns the failure modes it can cause, and its test_contract is the test that catches them. `assets/prompts/review.md` is a hot file (lens templates, comment template, local-diff mode, summary line all live in it), so its edits are serialised across waves instead of run in parallel and merged.

```
Phase review-edge-lenses — 7 tasks across 4 waves

Wave 1
  t-1  Add edge-case and silent-failure lens templates
       files:    assets/prompts/review.md, assets/review_lenses_test.go
       covers:   c-1, c-2, c-6
       desc:     Add two lens subagent templates to review.md §2 (edge-case, silent-failure), carrying the locked
                 value_set_source, default_arm, intentional_discards and severity rules as one sentence each in
                 language-agnostic wording, scoped to branches/error sites the diff adds or changes. Remove
                 "inconsistent error handling" and "surprising defaults" from the code-quality template and add an
                 explicit hand-off sentence; extend the "stays in its lane" hard rule to drop any code-quality finding
                 that reports a swallowed error or quiet fallback. Spawn count becomes six in one tool block.
       contract: - if the edge-case template stops telling the lens to Grep/Read the working tree for the value set, or
                   stops requiring the finding to say where the set is defined, TestEdgeLensValueSetSource fails
                 - if a loud catch-all (errors/panics/logs) is no longer "handled", or a silent catch-all is no longer
                   flagged naming the values that fall into it, TestEdgeLensDefaultArm fails
                 - if the edge-case finding shape stops requiring the unhandled values AND file:line, TestEdgeLensFindingShape fails
                 - if the silent-failure template drops any of: discarded error returns, empty catch/recover, quiet
                   fallback with nothing logged/surfaced — TestSilentLensScope fails (one subtest per shape)
                 - if a commented discard is flagged, or an uncommented discard is skipped (intentional_discards),
                   TestSilentLensIntentionalDiscards fails on the exact rule sentence
                 - if either lens defaults to BLOCKING, or loses the "BLOCKING only on a spec-criterion path or a
                   verify.toml survivor" rule, or stops reading verify.toml, TestEdgeLensesSeverity fails for that lens
                 - if a lens template turns Go-only (no non-Go construct named, e.g. `catch`/`except`/union type),
                   TestEdgeLensesLanguageAgnostic fails
                 - c-6: if the code-quality template still says "error handling" or "surprising defaults", or the lane
                   rule loses the "drop a code-quality swallowed-error/quiet-fallback finding" sentence,
                   TestQualityLensCedesSilentFailures fails
                 - if review.md still says "four subagents"/"all four" in §2 or the hard rules, TestReviewSpawnsSix fails
                 - if the two new templates push review.md over 15,000 B, TestPromptByteBudget/review.md fails

  t-6  Seed Go fixture diffs for the proof run
       files:    internal/prtriage/testdata/review-proof/jobstate/state.go,
                 internal/prtriage/testdata/review-proof/seeded.patch,
                 internal/prtriage/testdata/review-proof/clean.patch,
                 internal/prtriage/review_proof_test.go
       covers:   c-4
       desc:     state.go declares a 4-value State const set outside any diff. seeded.patch adds a dispatch.go whose
                 switch handles 3 values with a silent `default: return ""`, plus one uncommented `_ = f.Close()`-style
                 discard of an error return. clean.patch is the negative control carrying the near-misses: a switch
                 covering every value with a panicking default, and one discard with an adjacent reason comment.
                 Integrity test reconstructs each patch's post-image and checks it with go/parser + go/ast.
       contract: - if seeded.patch stops being a partial switch (all consts covered, or default becomes loud),
                   TestReviewProofFixtureSeeded fails naming the switch
                 - if seeded's discard gains a comment or stops discarding an error-returning call,
                   TestReviewProofFixtureSeeded fails naming the discard line
                 - if clean.patch loses a near-miss (no loud default, or its discard loses the reason comment) or
                   gains an uncommented discard / an uncovered const, TestReviewProofFixtureClean fails
                 - if the patch post-images stop parsing as Go, or the State consts move out of state.go so the value
                   set is no longer outside the diff, both fixture tests fail
                 - helpers seededSwitchSpan / seededDiscardLine are exported to the proof-record test (t-7) so a
                   fixture edit that shifts lines fails the record check instead of passing on stale lines

Wave 2 (depends t-1)
  t-2  Six-section comment shape that SplitReview splits
       files:    assets/prompts/review.md, internal/prtriage/split_test.go, internal/prtriage/items_test.go
       covers:   c-3, c-5
       desc:     §3 aggregate and §4 comment template gain "### Edge cases (N findings)" and "### Silent failures
                 (N findings)" sections in the BLOCKING/FLAG/NOTE — `file:line` shape, each with one example
                 finding; summary line becomes "across 6 lenses"; §6 wrap print lists six subagents. split.go is
                 not changed — the tests prove it needs no change.
       contract: - if a new section's bullets drift from `- **SEV** — `, TestSplitReviewMatchesReviewTemplate fails:
                   the template must split into 4 findings, 3 and 4 from Edge cases / Silent failures with Loc set
                 - if an edge-case finding that cites its value-set definition (`state.go:12`) after its own location
                   gets Loc = the definition site, TestSplitReviewLocIsBranchSite fails
                 - if an unhandled-values sub-list (indented `- StatusPaused` lines) escapes its finding or starts a
                   new one, TestSplitReviewKeepsValueList fails
                 - if a six-section comment's numbering restarts or a new heading leaks into a finding,
                   TestSplitReviewSixSections fails (N continuous 1..k, no `###` in any Text)
                 - /dross-respond path: if an own six-section review does not yield c<id>#1..#k with the new-lens
                   findings at their position, TestItemsSplitIDsSixLenses fails
                 - if the §4 summary line still says "across 4 lenses", TestReviewSummaryNamesSixLenses fails

  t-5  Edge checks block the solo task reviewer
       files:    assets/agents/dross-task-reviewer.md, internal/review/agentdef_test.go
       covers:   c-7
       desc:     Add an "Edge checks — always blocking" section to the reviewer: partial enum/status/flag handling and
                 swallowed errors/quiet fallbacks in the task's diff, using t-1's rule sentences verbatim
                 (value_set_source, default_arm, intentional_discards); a hit is a quality BLOCKING. Amend the
                 "Only these block" list. No ParseVerdict change.
       contract: - if review.md's rule sentences and the reviewer's drift apart, TestReviewerEdgeRulesMatchReviewLenses
                   fails naming the sentence missing from either file
                 - if the BLOCKING list no longer includes edge-check hits (so a hit would be graded FLAG and pass),
                   TestReviewerBodyDefinesGrades fails on the amended sentence
                 - if an edge-check BLOCKING quality finding stops failing the verdict, TestParseVerdictEdgeHitBlocks
                   fails (verdict "pass" with that finding is refused; "block" parses Pass=false)
                 - if the edge section tells the reviewer to read verify.toml or anything under .dross/ (breaks the
                   read fence), TestReviewerBodyLimitsReads / a new no-.dross-path assertion fails
                 - if the tool list grows to satisfy value-set lookup, TestReviewerDefinitionIsolation fails (Grep/Read suffice)

Wave 3 (depends t-2)
  t-3  Add --diff local review mode
       files:    assets/prompts/review.md, assets/commands/dross-review.md, assets/review_lenses_test.go
       covers:   c-4, c-5
       desc:     `--diff <path>` reviews a local unified diff: §1 skips the PR fetch and checks the file exists and is
                 non-empty; §5 never posts (save-only); §6 commits only review-comment.md, never the fixture.
                 Command argument-hint gains `--diff <path>` and its description names all six lenses.
       contract: - if --diff mode can reach `dross ship comment`, TestReviewDiffModeNeverPosts fails (the --diff branch
                   of §5 names save-only and no post command)
                 - if --diff still requires a PR number or fetches one, TestReviewDiffModeSkipsFetch fails
                 - if --diff's wrap `git add`s the given patch path, TestReviewDiffModeLeavesFixture fails
                 - if the command description still says "four-lens" or omits edge-case/silent-failure,
                   TestReviewCommandNamesSixLenses fails
                 - if argument-hint lacks `--diff`, the same test fails

Wave 4 (depends t-1, t-2, t-3, t-5, t-6)
  t-4  Purge "four lenses" from every surface
       files:    README.md, docs/dross.1, docs/subagent-offload-audit.md, internal/cmd/readme_doc_test.go
       covers:   c-5
       desc:     README command table row (:284) and roadmap entry (:311), man page (:458) and offload audit (:125)
                 name the six lenses. Guard test scans those files plus review.md and the command file.
       contract: - if any scanned surface matches /four[- ]lens|4[- ]lens|four subagents|all four|across 4 lenses/i,
                   TestNoFourLensSurface fails naming file:line
                 - if the README /dross-review row, README roadmap entry or man page entry omits any of security,
                   quality, tests, spec-fidelity, edge-case, silent-failure, TestReviewSurfacesNameSixLenses fails
                   naming the surface and the missing lens
                 - if the guard's file list loses a surface (glob matching nothing), the test fails on visited count < 6
       depends:  t-2, t-3 (guard scans review.md and the command file, which they finish)

  t-7  Run the proof and pin the record
       files:    internal/prtriage/testdata/review-proof/seeded.comment.md,
                 internal/prtriage/testdata/review-proof/clean.comment.md,
                 internal/prtriage/review_proof_test.go
       covers:   c-4, c-6, c-3
       desc:     `make install` (r-01), then `/dross-review --diff <patch> --phase review-edge-lenses` over each
                 fixture; copy each saved review-comment.md into testdata. Record test splits them with
                 SplitReview and attributes findings to sections by the preceding `###` heading.
       contract: - if the edge-case lens missed the seeded switch, TestReviewProofSeeded fails: no Edge-cases finding
                   with Loc inside seededSwitchSpan naming every unhandled const and `state.go`
                 - if the silent-failure lens missed the discard, TestReviewProofSeeded fails: no Silent-failures
                   finding with Loc == seededDiscardLine
                 - c-6 live: if the Code-quality section has any finding at seededDiscardLine, TestReviewProofSeeded fails
                 - c-4 empty edge: if either new lens reports anything on clean.patch (loud default or commented
                   discard flagged), TestReviewProofClean fails naming the finding
                 - if a captured comment is hand-edited out of the six-section shape, SplitReview returns ok=false
                   and both record tests fail
                 - if the record goes stale (fixture edited after the run), the line checks above fail against the
                   re-derived spans
       depends:  t-1, t-2, t-3, t-6 (and t-5 so the installed agents are final)
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 edge-case lens, unhandled values + file:line | t-1 (template + rules), t-7 (live) |
| c-2 silent-failure lens, file:line | t-1 (template + rules), t-7 (live) |
| c-3 own sections, SplitReview / respond split | t-2 (template + split/items tests), t-7 (real comments split) |
| c-4 seeded + clean proof run | t-6 (fixtures + integrity), t-3 (--diff mode to run it), t-7 (run + record) |
| c-5 six lenses named, no "four lenses" | t-2 (summary line), t-3 (command description), t-4 (README, man, audit doc + guard) |
| c-6 silent failures never from code-quality | t-1 (prompt lane rule), t-7 (live check at the discard line) |
| c-7 solo reviewer blocks on edge hits | t-5 |

7/7 criteria covered.

## Judgment calls

- review.md edits serialised t-1 → t-2 → t-3 across waves; rejected parallel section-split edits of one file — a merge collision on the hot prompt is a likelier failure than lost wall-clock.
- Rule parity via a verbatim-sentence test (t-5); rejected a shared partial — partials are condition-triggered branches of one prompt and agents install raw, so there is no include mechanism for an agent file.
- c-7 hit graded as quality BLOCKING in the existing verdict schema; rejected a new verdict field or ParseVerdict change — the parser already fails on BLOCKING, so a prompt + test change carries no Go risk.
- c-7 has no live reviewer proof run; rejected driving dross-task-reviewer over the fixture — it only runs from a `dross review context` digest file, and a fake spawn path is the scope creep the agent forbids.
- Added `--diff <path>` save-only mode (t-3); rejected proving c-4 on a real throwaway PR — posting fixture findings to the public repo pollutes it and the post path is not under test.
- Seeded switch carries a *silent default* rather than no default; rejected the minimal no-default shape — it live-proves default_arm, the rule most likely to misfire; the silent-failure lens may also flag that default and the record test does not forbid it.
- Clean fixture carries the near-misses (loud default, commented discard) instead of being trivially clean; rejected a bland clean diff — it would pass c-4 while proving nothing about false positives.
- Fixtures live under internal/prtriage/testdata (Go ignores testdata, the lens can still Grep state.go); rejected .dross/phases/... — the record test would read phase state from a package test.
- Record test checks Loc inside the switch span, not one exact line; rejected exact-line match — the lens may cite the `switch` line or the default arm, both correct.
- split.go left unchanged, proven by tests; rejected adding a Lens field to Finding — c-3 says "exactly as it does the original four", and respond does not need lens attribution.
- docs/subagent-offload-audit.md updated rather than left as history; c-5 says "no surface", and the guard is simpler with no exemption list.
- Edge-case vs test-efficacy double reports (a verify.toml survivor on a switch arm) not deduplicated; only c-6 demands exclusivity, and the footer calls cross-lens agreement intentional.
- Budget risk owned by t-1 (largest growth); review.md is 8,423 B today, so two templates fit under the 15,000 B cap without a partial.
