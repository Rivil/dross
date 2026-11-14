# review-edge-lenses — panel synthesis

Checked against the repo before scoring: review.md is 8,423 B (budget 15,000 B,
`assets/budget_test.go`); `TestSplitReviewMatchesReviewTemplate` currently expects 2
findings; `internal/prtriage/items.go:66` routes /dross-respond through `SplitReview`;
`dross-task-reviewer.md` forbids reads under `.dross/` (pinned by
`TestReviewerBodyLimitsReads`), so the reviewer can never read verify.toml. The
"four lens" surfaces in the tree today are README.md:284 (`4-lens`), README.md:311,
docs/dross.1:458, docs/subagent-offload-audit.md:125, assets/commands/dross-review.md:3
and review.md lines 3/5/33/63/90/145. README.md:445 says "all four providers", which
is not about lenses. No existing `fixtures/*` directory has a go.mod.

## Scores

| Dimension | risk | mvp | verification |
|---|---|---|---|
| Criteria coverage | 7/7; every criterion has both a prompt-pin layer and a live-proof layer where it applies; c-6 is also checked live | 7/7, but the seeded fixture declares the Status consts inside the patch, so the live run never exercises the locked value_set_source (a value set outside the diff) | 7/7; the only plan whose c-5 test reads "summary line names all six" literally; c-6 is checked live |
| Test-contract specificity | Sharpest overall: per-shape subtests, Loc-is-branch-site, sub-list containment, the /dross-respond items path, and the reviewer read fence. Flaw: the guard regex includes `all four`, which fails on README.md:445 ("all four providers") | Good needle lists. Coarser elsewhere: one test pins all of review.md, and the c-6 proof only checks the same Loc | Strong: lines computed by go/parser, six-section equivalence (moving the new bullets under Security gives an identical []Finding), `> Rule:` lines as one extractable source, and a no-severity-copy guard. Weak spots: the "contains `Go`" token check is brittle, and the live FLAG assertion is flaky |
| Granularity | 7 tasks, each one concern. --diff is its own task. Slightly over-split | 4 tasks; t-1 touches 4 files across 6 criteria and holds every review.md change. Too coarse to review or bisect | 6 tasks, each one concern; two 5-file tasks, both justified as one layer |
| Wave correctness | Correct but slow: serialising review.md costs a wave (t-3 runs alone in wave 3). The wave-4 header lists t-5 and t-6 as dependencies, but t-4 needs neither | Correct; t-1 is the bottleneck of wave 1 | Correct and tightest: review.md edits are serialised t-1 → t-3 across waves, and the files within each wave are disjoint |

**Skeleton: verification.** It has the tightest correct wave graph and contracts derived backward from each criterion, and its `> Rule:` single-source design gives c-7 parity without duplicated needles. risk's sharper per-task contracts are grafted in. mvp's broader sweep is grafted in.

## Merged plan

```
Phase review-edge-lenses — 6 tasks across 3 waves

Wave 1
  t-1  Add edge-case and silent-failure lenses to review          [verification+risk+mvp]
       files:    assets/prompts/review.md, assets/commands/dross-review.md,
                 assets/review_lenses_test.go
       covers:   c-1, c-2, c-6, c-5
       desc:     review.md gains lenses 5 (edge cases) and 6 (silent failures): list entries,
                 two `subagent_type: general-purpose` templates, and every lens-count "four"
                 becomes "six" (intro, §2 spawn, §3 aggregate, hard rule, §6 wrap print, summary
                 line). The summary line keeps "across 6 lenses" and also names the six lenses
                 (see D7). Each locked rule sentence goes on its own `> Rule:` line
                 (value_set_source, default_arm, intentional_discards); severity goes on a
                 `> Severity:` line. Both lenses are scoped to the branches and error sites the
                 diff adds or changes [risk], and both read spec.toml and verify.toml. The
                 code-quality template drops "inconsistent error handling" and "surprising
                 defaults" and hands swallowed errors and quiet fallbacks to silent-failure. §3
                 gains a rule that drops any code-quality finding reporting a swallowed error or
                 quiet fallback (see D5). The command description names all six lenses.
       contract: - Drop either template and TestReviewSpawnsSixLenses fails. It requires 6
                   numbered lenses, 6 `subagent_type: general-purpose` templates, "all six
                   subagents in a single tool block", and no lens-count "four" anywhere in
                   review.md (intro, spawn, aggregate, hard rule, wrap print).
                 - The edge-case template must tell the lens to Grep/Read the working tree for the
                   full value set, say where the set is defined, and name every unhandled value
                   with file:line. Drop any of these and TestEdgeCaseLensRules fails.
                 - Lose the default_arm `> Rule:` line (a loud catch-all that errors, panics or
                   logs counts as handling; a silent catch-all is flagged, naming the values that
                   fall into it) and TestEdgeCaseLensRules fails.
                 - The silent-failure template must name discarded error returns, empty catch,
                   empty recover, and a default returned on failure with nothing logged or
                   surfaced. Drop any one, or the adjacent-reason-comment exemption, and
                   TestSilentFailureLensRules fails, one subtest per shape [risk].
                 - Lose either template's `> Severity:` line (FLAG by default; BLOCKING only on a
                   spec-criterion path or a verify.toml survivor), or stop pointing at
                   verify.toml, and TestNewLensSeverity fails for that lens.
                 - c-6: if the code-quality template still names "error handling" or "surprising
                   defaults", or §3 loses the drop rule, TestCodeQualityCedesSilentFailures fails.
                 - language_scope: a new template must name both `catch` and `recover`
                   [mvp+verification] and must contain no `err != nil` and no `.go` example.
                   Otherwise TestNewLensesLanguageAgnostic fails. The bare "Go" token check is
                   dropped as brittle.
                 - If the command description omits any of the six lens names,
                   TestReviewCommandNamesSixLenses fails [risk+mvp].
                 - If review.md grows past 15,000 B, TestPromptByteBudget/review.md fails.
       depends:  —

  t-2  Commit the Go proof fixture and pin its shape                 [verification+risk]
       files:    fixtures/review-edge-lenses-c4/go.mod,
                 fixtures/review-edge-lenses-c4/order/status.go,
                 fixtures/review-edge-lenses-c4/seeded/handler.go,
                 fixtures/review-edge-lenses-c4/clean/handler.go,
                 internal/prtriage/lensproof_test.go
       covers:   c-4
       desc:     A separate Go module (its own go.mod, so `./...`, gremlins and vet never see
                 it). order/status.go declares `type Status` with Pending/Paid/Shipped/Refunded,
                 outside any diff, so value_set_source is exercised [risk+verification].
                 seeded/handler.go has a Status switch with no Refunded case and a silent
                 `default: return ""` (see D2), plus one uncommented `_ = <error-returning call>`.
                 clean/handler.go carries the near-misses [risk]: an exhaustive switch with a
                 loud default (returns an error or panics) and one discard with an adjacent
                 comment giving its reason. The shape tests parse the fixture with go/parser;
                 they do not compile it.
       contract: - If Refunded is added to the seeded switch, or the default becomes loud
                   (error, panic or log), TestLensFixtureSeededShape fails. The switch's cases
                   must cover the Status consts minus exactly [Refunded], and its default arm
                   must not error, panic or log.
                 - If the seeded discard gains a comment, or no longer discards an
                   error-returning call, TestLensFixtureSeededShape fails. There must be exactly
                   one uncommented blank assignment of a call result, named by its line.
                 - If the clean fixture regresses into a defect (missing case, silent default,
                   uncommented discard) or loses a near-miss (the loud default, or the discard's
                   reason comment), TestLensFixtureCleanShape fails.
                 - If the Status consts move out of order/status.go into a handler,
                   TestLensFixtureSeededShape fails, so the value set stays outside the reviewed
                   diff [risk].
                 - If go.mod is removed, TestLensFixtureIsOwnModule fails.
                 - The helpers that compute the seeded switch span and discard line are shared
                   with t-6. A fixture edit that shifts lines then fails the record check instead
                   of passing on stale lines [risk].
       depends:  —

Wave 2 (depends t-1)
  t-3  Give new lenses comment sections; add --diff mode             [verification+risk]
       files:    assets/prompts/review.md, assets/commands/dross-review.md,
                 internal/prtriage/split_test.go, internal/prtriage/items_test.go,
                 assets/review_lenses_test.go
       covers:   c-3, c-4
       desc:     The §3 aggregate block and the §4 comment template gain `### Edge cases (N
                 findings)` and `### Silent failures (N findings)`, each with one example bullet
                 in the existing `- **FLAG** — \`file:line\` — …` shape. The edge example puts
                 the switch site first and the set-definition site after it. Add a
                 `--diff <patch>` mode and update the argument-hint. In place of §1's fetch, it
                 copies the patch to review-diff.patch and checks the file exists and is
                 non-empty [risk]. It skips the §0 doctor requirement, the §5 post and the §6
                 commit, then prints the comment and saves it to review-comment.md (see D9).
                 split.go is not changed.
       contract: - If a new section is missing or malformed (for example `- FLAG:` instead of
                   `- **FLAG** — `), TestSplitReviewMatchesReviewTemplate fails. The template
                   must split into 4 findings (it was 2), and findings 3 and 4 must be FLAG with
                   Loc set.
                 - If an edge finding's Loc becomes the set-definition site instead of the switch
                   site, TestSplitReviewMatchesReviewTemplate fails. Finding 3's Loc must be the
                   switch path:line even though a second path:line appears later in its text.
                 - If SplitReview treats the new headings differently from the original four,
                   TestSplitReviewSixSections fails. A six-section comment must yield 6 findings
                   numbered 1..6 in order, with no `###` in any Text. Moving the two new bullets
                   under `### Security` must yield an identical []Finding.
                 - If an unhandled-values sub-list (indented `- Refunded` lines) escapes its
                   finding or starts a new one, TestSplitReviewKeepsValueList fails [risk].
                 - /dross-respond path: if a six-section own review does not yield c<id>#1..#k
                   with the new-lens findings at their positions, TestItemsSplitIDsSixLenses
                   fails [risk].
                 - If --diff mode can reach `dross ship comment` or the §6 commit, or the
                   argument-hint lacks `--diff <patch>`, TestReviewDiffModeNeverPosts fails.
                 - If --diff still requires a PR number or fetches one, TestReviewDiffModeSkipsFetch
                   fails [risk].
       depends:  t-1

  t-4  Teach dross-task-reviewer the two lens checks                 [verification+risk+mvp]
       files:    assets/agents/dross-task-reviewer.md, internal/review/agentdef_test.go
       covers:   c-7
       desc:     Add a "Partial value sets and swallowed errors" section that copies the
                 `> Rule:` sentences from review.md's two new lenses verbatim. The reviewer may
                 Grep/Read the tree for a value set; it already has those tools. The BLOCKING
                 grade line adds partial enum/status/flag handling and an uncommented swallowed
                 error or quiet fallback, so a hit sets the verdict to "block". The lens severity
                 rule (FLAG by default) is not copied, and neither is any instruction to read
                 verify.toml, because the reviewer's read fence forbids `.dross/`. ParseVerdict
                 is not changed.
       contract: - If the reviewer's rules drift from the lenses', TestReviewerSharesLensRules
                   fails. Every `> Rule:` sentence in review.md's edge-case and silent-failure
                   templates must appear verbatim in the reviewer body, which makes review.md the
                   single source.
                 - If a hit is graded FLAG, the verdict passes. TestReviewerBodyDefinesGrades
                   (needle updated) fails unless the BLOCKING line names partial
                   enum/status/flag handling and swallowed errors.
                 - If the reviewer body contains review.md's `> Severity:` text, a hit could
                   pass, so TestReviewerEdgeHitsBlock fails.
                 - If a "pass" verdict carries an edge-check BLOCKING quality finding,
                   ParseVerdict must refuse it. If it accepts it, TestParseVerdictEdgeHitBlocks
                   fails. A "block" verdict must parse with Pass=false [risk].
                 - If the new section tells the reviewer to read verify.toml or anything under
                   `.dross/`, a no-.dross-path assertion fails, and TestReviewerBodyLimitsReads
                   stays green [risk].
                 - If the tool list grows, TestReviewerDefinitionIsolation fails [risk+mvp]. If
                   the worked example breaks, TestReviewerBodyMatchesParser fails.
       depends:  t-1

  t-5  Rename every lens surface to six lenses                       [verification+mvp+risk]
       files:    README.md, docs/dross.1, docs/subagent-offload-audit.md,
                 internal/cmd/readme_doc_test.go
       covers:   c-5
       desc:     The README command table row (:284) and roadmap bullet (:311), the dross.1
                 /dross-review entry (:458) and the audit doc's review entry (:125) name the six
                 lenses: security / quality / tests / spec-fidelity / edge-case /
                 silent-failure. The README v1.9 roadmap checkbox stays unticked [mvp].
       contract: - If any README.md, ARCHITECTURE.md, docs/** or assets/** `.md`/`.1` file
                   matches /\b(four|4)[- ]lens|across 4 lenses/i, TestNoFourLensSurface fails,
                   naming file:line (see D6). The regex deliberately leaves out `all four`:
                   README.md:445 "all four providers" would false-positive.
                 - If the guard's globs visit fewer files than the known surfaces (README,
                   dross.1, audit doc, review.md, command file), TestNoFourLensSurface fails on
                   the visited count [risk].
                 - If any of these surfaces drops one of the six lens tokens,
                   TestReviewSurfacesNameSixLenses fails, naming the surface and the missing
                   token: the README `/dross-review` row, the README roadmap bullet [risk+mvp],
                   the dross.1 entry, the audit doc's `### review` entry [mvp], the command's
                   `description:`, or review.md's summary line.
       depends:  t-1

Wave 3 (depends t-2, t-3)
  t-6  Record the live proof run against fixtures                    [verification+risk+mvp]
       files:    fixtures/review-edge-lenses-c4/RUN.md,
                 fixtures/review-edge-lenses-c4/seeded-comment.md,
                 fixtures/review-edge-lenses-c4/clean-comment.md,
                 internal/prtriage/lensproof_test.go
       covers:   c-4, c-1, c-2, c-6, c-3
       desc:     Run `make install` (r-01). Build each patch with
                 `git diff --no-index /dev/null <handler.go>`. Run
                 `/dross-review --diff <patch> --phase review-edge-lenses` once for seeded and
                 once for clean, and copy each review-comment.md into the fixture directory.
                 RUN.md records the commands, the date and `dross --version`, following the
                 fixtures/multilang-c3/RUN.md shape. The tests cut each recorded comment by
                 `### ` section and split it with SplitReview.
       contract: - If the edge-case lens misses the seeded gap, TestLensProofSeeded fails. The
                   Edge cases section must hold a finding whose Loc falls inside the seeded switch
                   span computed by go/parser [risk] and whose text names Refunded and status.go
                   (value_set_source).
                 - If the silent-failure lens misses the swallowed error, TestLensProofSeeded
                   fails. Silent failures must hold a finding at the discard line.
                 - c-6: if the Code quality section has any finding at the discard line,
                   TestLensProofSeeded fails. It also fails on an Edge cases finding at the
                   discard line. A Silent failures finding at the silent default is NOT forbidden
                   (see D2).
                 - c-4 empty edge: if either new lens reports anything on the clean fixture
                   (whose loud default and reason-commented discard test the default_arm and
                   intentional_discards exemptions), TestLensProofClean fails, naming the
                   finding.
                 - If a recorded comment is hand-edited out of the six-section shape,
                   SplitReview returns ok=false and both record tests fail [risk].
                 - If the fixture is edited so its lines shift without a re-run, the computed
                   lines no longer match the recorded Locs and TestLensProofSeeded fails.
                 - Severity is not asserted in the live record (see D8).
       depends:  t-2, t-3
```

Coverage: c-1 → t-1, t-6 · c-2 → t-1, t-6 · c-3 → t-3, t-6 · c-4 → t-2, t-3, t-6 ·
c-5 → t-1, t-5 · c-6 → t-1, t-6 · c-7 → t-4. 7/7.

## Disagreements

**D1. How the review.md work is split, and where --diff lives.**
- mvp puts every review.md change, the command file and the split tests in one wave-1 task.
- verification uses two tasks: t-1 for the lenses, and t-3 for the sections plus --diff.
- risk uses three serial tasks across three waves (lenses → sections → --diff alone).
- *Default:* verification's two tasks.
- *Why it matters:* mvp's single task is the coarsest unit to review or bisect. risk's separate --diff task isolates the save-only safety contract but costs a whole wave, during which nothing else runs.

**D2. Shape of the seeded switch.**
- risk and mvp seed a silent `default: return ""`. This live-proves default_arm, the rule most likely to misfire.
- verification seeds no default. A silent default is arguably also a quiet fallback, so both new lenses could claim it, and verification's "no Silent failures finding at the switch line" assertion would become flaky.
- *Default:* silent default (2 of 3). The cross-lane switch-line assertion is dropped, and t-6 does not forbid silent-failure from also flagging the default.
- *Why it matters:* this is the trade between live-proving the locked default_arm rule and having a strictly one-lens-per-defect proof. c-4 requires each defect to be reported "from the matching lens", not exclusively by it. Only c-6 demands exclusivity, and only against code-quality.

**D3. Where the fixture lives.**
- risk and mvp use `internal/prtriage/testdata/...` patch files. Go ignores testdata, and the record test sits beside SplitReview.
- verification uses `fixtures/review-edge-lenses-c4/` with its own go.mod plus RUN.md. This follows the fixtures/*-c3 precedent and is visible to humans. No existing fixture has a go.mod, so the own module is a new pattern.
- *Default:* verification.
- *Why it matters:* the choice decides whether the seeded defects can leak into the main module's mutation, lint and techdebt scope (testdata and a nested module both keep them out), and whether the proof is discoverable next to the other criterion proofs.

**D4. Whether the value set sits inside the reviewed diff.**
- mvp's seeded patch declares the Status consts itself.
- risk and verification keep them in a file outside the diff.
- *Default:* outside the diff.
- *Why it matters:* the locked value_set_source decision exists because value sets usually live outside the diff. A fixture that puts them inside never proves the lens Greps for them.

**D5. How c-6 exclusivity is enforced in the prompt.**
- mvp drops a code-quality finding only when it shares a file:line with a silent-failure finding.
- risk (a lane hard rule) and verification (a §3 drop rule) drop any code-quality finding about a swallowed error or quiet fallback, whatever its location.
- *Default:* the topical drop.
- *Why it matters:* the same-location dedupe still lets code-quality report a swallowed error that silent-failure missed, which violates "never also by the code-quality lens".

**D6. Scope of the "four lens" guard.**
- risk scans a fixed file list with a visited-count floor, and its regex includes `all four`. That regex fails today on README.md:445 ("all four providers").
- verification globs README.md, docs/** and assets/**.
- mvp walks the whole repo, skipping only .git and .dross/.
- *Default:* verification's globs plus ARCHITECTURE.md (from mvp's breadth), risk's visited-count floor, and no `all four`.
- *Why it matters:* mvp's repo-wide walk catches surfaces nobody listed, but it also walks `*_test.go` files. A guard test that asserts a string like "four-lens" is absent would then trip on its own source. A fixed list can silently go stale.

**D7. What c-5's "summary line" must say.**
- risk and mvp make the summary line `... across 6 lenses` (a count only).
- verification's t-5 requires the summary line to carry all six lens tokens.
- *Default:* keep "across 6 lenses" and append the six names, which satisfies both.
- *Why it matters:* c-5 says the summary line must "name all six lenses". A count alone may fail spec-fidelity at verify. Names add bytes to every posted comment.

**D8. Asserting severity in the live proof.**
- verification asserts FLAG on the recorded findings, arguing the fixture paths are on no spec criterion.
- risk and mvp do not assert severity.
- *Default:* no severity assertion in t-6. The template tests in t-3 still pin FLAG.
- *Why it matters:* the run uses `--phase review-edge-lenses`, and that spec's c-4 is literally about this fixture. A lens could legitimately grade the gap BLOCKING under the locked severity rule, so the assertion is flaky rather than a real guard.

**D9. --diff mode's §0 and §6 behaviour.**
- risk commits only review-comment.md and never the patch.
- verification skips the §0 doctor requirement and the whole §6 commit.
- mvp says only "comment saved only".
- *Default:* verification's behaviour.
- *Why it matters:* a --diff run is a local proof tool. Committing phase artefacts for a run with no PR mislabels the record ("on PR #<n>"). Skipping the commit means the proof survives only through t-6's copies into the fixture directory.

**D10. When the surface sweep runs.**
- risk puts the sweep last (wave 4, after t-2, t-3, t-5 and t-6), so the guard sees the final review.md and command file.
- verification and mvp run it in wave 2, alongside the other review.md and command edits.
- *Default:* wave 2.
- *Why it matters:* t-3 adds no lens-count text, so the parallel run is safe today. If a later edit to t-3 reintroduces "four", the guard only catches it on the next full run, not inside t-5's own gate.
