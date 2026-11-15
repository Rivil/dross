# review-edge-lenses — verification lens draft

Lens: design backward from the test contracts. Each criterion's ideal contract was
written first; tasks are the smallest change that makes that contract satisfiable.
Lens behaviour (c-1, c-2, c-6) is LLM behaviour, so it gets two layers of contract:
(a) deterministic prompt-text pins that fail when a locked rule is dropped from the
prompt, and (b) a recorded proof run whose output is parsed by `prtriage.SplitReview`
and checked against line numbers computed from the fixture with `go/parser`.
That makes the c-4 proof a committed `go test` contract, not just a note in RUN.md.

```
Phase review-edge-lenses — 6 tasks across 3 waves

Wave 1
  t-1  Add edge-case and silent-failure lenses to review
       files:    assets/prompts/review.md, assets/commands/dross-review.md,
                 assets/review_lenses_test.go
       covers:   c-1, c-2, c-6, c-5
       desc:     review.md gains lenses 5 (edge cases) and 6 (silent failures): list entries,
                 two `subagent_type: general-purpose` templates, and every "four" in the file
                 becomes "six" (intro, spawn, aggregate, hard rule, wrap print, summary line
                 "across 6 lenses"). Locked-rule sentences go on their own `> Rule:` lines
                 (value_set_source, default_arm, intentional_discards) and the severity rule
                 on a `> Severity:` line. Both read spec.toml + verify.toml. The code-quality
                 template drops "inconsistent error handling" / "surprising defaults" and hands
                 swallowed errors and quiet fallbacks to the silent-failure lens. §3 gains a
                 drop rule for code-quality findings about those. The command description names
                 all six lenses.
       contract: if the edge-case or silent-failure template is dropped, TestReviewSpawnsSixLenses
                 fails: review.md has 6 numbered lenses, 6 `subagent_type: general-purpose`
                 templates, and "all six subagents in a single tool block"
                 if the edge-case template stops telling the lens to Grep/Read the working tree
                 for the full value set and say where it is defined, or stops requiring every
                 unhandled value to be named with file:line, TestEdgeCaseLensRules fails
                 if the default_arm rule is lost (a loud catch-all that errors, panics or logs
                 is handled; a silent one is flagged, naming the values that fall into it),
                 TestEdgeCaseLensRules fails on the missing `> Rule:` line
                 if the silent-failure template stops naming discarded error returns, empty
                 catch, empty recover and a default returned on failure with nothing logged or
                 surfaced, or loses the adjacent-reason-comment exemption, TestSilentFailureLensRules
                 fails
                 if either new template loses the `> Severity:` line (FLAG by default; BLOCKING
                 only on a spec-criterion path or a verify.toml survivor) or stops pointing at
                 verify.toml, TestNewLensSeverity fails for that lens
                 if the code-quality template still names "error handling" or "surprising
                 defaults", or §3 loses the drop rule for swallowed-error findings,
                 TestCodeQualityCedesSilentFailures fails (c-6)
                 if a new template is Go-specific (contains "Go", ".go" or `err != nil`), or
                 the silent-failure template does not name both `catch` and `recover`,
                 TestNewLensesLanguageAgnostic fails (language_scope)
                 if review.md still says "four", or its summary line is not "across 6 lenses",
                 TestReviewSpawnsSixLenses fails; if review.md grows past 15,000 B,
                 TestPromptByteBudget/review.md fails
       depends:  —

  t-2  Commit the Go proof fixture and pin its shape
       files:    fixtures/review-edge-lenses-c4/go.mod,
                 fixtures/review-edge-lenses-c4/order/status.go,
                 fixtures/review-edge-lenses-c4/seeded/handler.go,
                 fixtures/review-edge-lenses-c4/clean/handler.go,
                 internal/prtriage/lensproof_test.go
       covers:   c-4
       desc:     A separate Go module (own go.mod, so `./...`, gremlins and vet never see it).
                 order/status.go declares `type Status` with Pending/Paid/Shipped/Refunded.
                 seeded/handler.go has a Status switch with no Refunded case and no default, plus
                 one uncommented `_ = <error-returning call>`. clean/handler.go has an exhaustive
                 switch with a loud default (returns an error) and one discard whose adjacent
                 comment gives its reason. Shape tests parse the fixture with go/parser; they
                 do not compile it.
       contract: if someone adds Refunded to the seeded switch, or gives it a default arm,
                 TestLensFixtureSeededShape fails: the switch's cases cover the Status consts
                 minus exactly [Refunded], with no default
                 if the seeded discard gains a comment or disappears, TestLensFixtureSeededShape
                 fails: exactly one blank assignment of a call result with no adjacent comment
                 if the clean fixture regresses into a defect (missing case, silent default,
                 uncommented discard), TestLensFixtureCleanShape fails. Without that check the
                 clean half of c-4 could pass on a fixture that has a real gap
                 if go.mod is removed, the seeded defects enter the main module's mutation and
                 lint scope and TestLensFixtureIsOwnModule fails
       depends:  —

Wave 2 (depends t-1)
  t-3  Give new lenses comment sections; add --diff mode
       files:    assets/prompts/review.md, assets/commands/dross-review.md,
                 internal/prtriage/split_test.go, assets/review_lenses_test.go
       covers:   c-3, c-4
       desc:     The §3 aggregate block and the §4 comment template gain `### Edge cases (N
                 findings)` and `### Silent failures (N findings)`, each with one example bullet
                 in the existing `- **FLAG** — \`file:line\` — …` shape. The edge example puts
                 the switch site first and the set-definition site after it. New `--diff <patch>`
                 mode (argument-hint updated): copy the patch to review-diff.patch in place of
                 §1's fetch, skip §0's doctor requirement, §5's post and §6's commit, and print
                 the comment and save it to review-comment.md. No change to split.go.
       contract: if the template's new sections are missing or malformed,
                 TestSplitReviewMatchesReviewTemplate fails: the template splits into 4 findings
                 (was 2), and findings 3 and 4 are FLAG
                 if an edge finding's first backticked loc is the set definition instead of the
                 switch site, TestSplitReviewMatchesReviewTemplate fails: finding 3's Loc is the
                 switch path:line, even though a second path:line appears later in its text
                 if SplitReview treats the new headings differently from the original four,
                 TestSplitReviewSixSections fails: a six-section comment yields 6 findings in
                 order, and moving the two new bullets under `### Security` yields an identical
                 []Finding
                 if a new-lens finding swallows the next `### Silent failures` heading or its
                 detail line leaks across, TestSplitReviewSixSections fails on Text equality
                 if --diff mode can still reach `dross ship comment` or the §6 commit, or the
                 argument-hint lacks `--diff <patch>`, TestReviewDiffModeNeverPosts fails
       depends:  t-1

  t-4  Teach dross-task-reviewer the two lens checks
       files:    assets/agents/dross-task-reviewer.md, internal/review/agentdef_test.go
       covers:   c-7
       desc:     Add a "Partial value sets and swallowed errors" section that copies review.md's
                 two lenses' `> Rule:` sentences verbatim. It may Grep the tree for the value
                 set, which it already can. The BLOCKING grade line adds partial
                 enum/status/flag handling and an uncommented swallowed error / quiet fallback,
                 so a hit sets verdict "block". The lens severity rule (FLAG by default) is not
                 copied. Run `make install` afterwards (r-01) so doctor reports the reviewer as
                 current.
       contract: if the reviewer's rules drift from the lenses', TestReviewerSharesLensRules
                 fails: every `> Rule:` sentence in review.md's edge-case and silent-failure
                 templates appears verbatim in the reviewer body. That makes review.md the
                 single source, so a rule added to a lens without the reviewer also fails
                 if a hit is graded FLAG, the verdict passes, so TestReviewerBodyDefinesGrades
                 (needle updated) fails unless the BLOCKING line names partial
                 enum/status/flag handling and swallowed errors
                 if the reviewer copies the lens's "FLAG by default" severity sentence, a hit
                 could pass, so TestReviewerEdgeHitsBlock fails when the body contains review.md's
                 `> Severity:` text
                 if the edit breaks the worked example, TestReviewerBodyMatchesParser fails
                 (still parses, still has both finding kinds)
       depends:  t-1

  t-5  Rename every lens surface to six lenses
       files:    README.md, docs/dross.1, docs/subagent-offload-audit.md,
                 internal/cmd/readme_doc_test.go
       covers:   c-5
       desc:     README table row (line 284) and roadmap bullet (line 311), the dross.1 entry
                 (line 458) and the audit doc's review entry (line 125) name the six lenses:
                 security / quality / tests / spec-fidelity / edge-case / silent-failure.
       contract: if any README.md, docs/** or assets/** .md/.1 file still matches
                 /four[- ]lens|4-lens|across 4 lenses/i, TestNoFourLensSurface fails naming
                 file:line
                 if the README `/dross-review` row, the dross.1 entry, the command's
                 `description:` or review.md's summary line drops any of the six lens tokens,
                 TestReviewSurfacesNameSixLenses fails naming the surface and the missing token
       depends:  t-1

Wave 3 (depends t-2, t-3)
  t-6  Record the live proof run against fixtures
       files:    fixtures/review-edge-lenses-c4/RUN.md,
                 fixtures/review-edge-lenses-c4/seeded-comment.md,
                 fixtures/review-edge-lenses-c4/clean-comment.md,
                 internal/prtriage/lensproof_test.go
       covers:   c-4, c-1, c-2, c-6
       desc:     Run `make install` (r-01). Build each patch with `git diff --no-index /dev/null
                 <handler.go>`, run `/dross-review --diff <patch> --phase review-edge-lenses`
                 for seeded and for clean, and copy each review-comment.md into the fixture.
                 RUN.md records the commands, date and `dross --version`, following the
                 fixtures/multilang-c3/RUN.md shape. The tests cut each recorded comment by
                 `### ` section and split it with SplitReview.
       contract: if the edge-case lens misses the seeded gap, TestLensProofSeeded fails: the
                 Edge cases section has a FLAG finding whose Loc ends
                 seeded/handler.go:<switch line from go/parser> and whose text names Refunded
                 and status.go (value_set_source)
                 if the silent-failure lens misses the swallowed error, TestLensProofSeeded
                 fails: Silent failures has a FLAG finding at the discard line
                 if the lenses cross lanes, TestLensProofSeeded fails: there is no Silent
                 failures finding at the switch line, no Edge cases finding at the discard line,
                 and no Code quality finding at the discard line (c-6 adjacency)
                 if either new lens reports on the clean fixture (whose loud default and
                 reason-commented discard test the default_arm and intentional_discards
                 exemptions), TestLensProofClean fails: zero findings under Edge cases and
                 Silent failures
                 if the fixture is edited so lines shift without a re-run, the computed lines
                 no longer match the recorded Locs and TestLensProofSeeded fails
       depends:  t-2, t-3
```

## Coverage

- c-1 → t-1 (prompt rules pinned), t-6 (live finding names Refunded + file:line + definition site)
- c-2 → t-1 (prompt rules pinned), t-6 (live finding at the discard line)
- c-3 → t-3 (template sections + SplitReview six-section equivalence)
- c-4 → t-2 (fixture shape), t-3 (`--diff` makes the run reproducible), t-6 (recorded run parsed and asserted)
- c-5 → t-1 (review.md + command description), t-5 (README, man page, audit doc, repo sweep)
- c-6 → t-1 (code-quality scope narrowed + §3 drop rule), t-6 (no code-quality finding at the discard line in the live run)
- c-7 → t-4 (verbatim shared rules + BLOCKING grade)

7/7 criteria covered.

## Judgment calls

- Chose to make the c-4 proof a `go test` over recorded comments with lines computed by go/parser, not just a RUN.md narrative like fixtures/multilang-c3. A narrative cannot fail; this test fails when the fixture drifts or a recorded finding does not match.
- Chose to add a `--diff <patch>` mode to /dross-review instead of proving against a throwaway PR. A PR-based proof can't be reproduced at verify and posts noise on a public repo. Rejected cost: about 6 lines of prompt and one argument-hint.
- Chose review.md's `> Rule:` lines as the single source the task reviewer must copy verbatim, checked by TestReviewerSharesLensRules. Rejected: duplicated needle constants in two test packages. They would let both files drift together while each test stayed green.
- Chose to put the fixture under fixtures/ with its own go.mod, following the existing fixtures/*-c3 precedent. Rejected: testdata/, which hides the fixture from humans looking for the proof. Rejected: joining the main module, which would put the seeded defects into gremlins and techdebt scope.
- Chose a seeded switch with no default arm over a silent `default: return ""`. A silent default is also arguably a quiet fallback, so both new lenses could claim it, and "each defect reported by exactly its matching lens" would become flaky. The default_arm rule is still exercised: the clean fixture's loud default must produce no finding.
- Chose to assert FLAG severity in the live proof. The fixture paths are on no spec criterion and have no verify.toml survivor, so BLOCKING there would be a real breach of the locked severity decision, not flakiness.
- Chose not to touch split.go and not to add a `Section` field to `Finding`. c-3 needs identical splitting, which TestSplitReviewSixSections proves; a field with no consumer would be dead code. Tests cut comments by heading instead.
- Chose to grade task-reviewer hits BLOCKING and to forbid copying the lens severity sentence. c-7 says "a hit fails its verdict", and the reviewer's verdict blocks only on BLOCKING or spec findings.
- Sequenced review.md edits across waves (t-1 → t-3) instead of running them in parallel. Every review.md task is in a different wave, and each wave-2 task has disjoint files (t-3's command edit only touches argument-hint, after t-1's description edit).
- Left ARCHITECTURE.md out of the plan: /dross-ship §3.5 merges phase landmarks into it, and it holds no "four lenses" text today.
- t-2 lists 5 files, which is at the split threshold. Kept it whole because four are tiny fixture files in one layer and only mean something together with the shape test.
