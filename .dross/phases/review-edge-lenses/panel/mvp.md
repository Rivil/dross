# review-edge-lenses — MVP lens draft

```
Phase review-edge-lenses — 4 tasks across 2 waves

Wave 1
  t-1  Add edge-case and silent-failure lenses
       files:    assets/prompts/review.md, assets/commands/dross-review.md,
                 internal/cmd/review_prompt_test.go (new), internal/prtriage/split_test.go
       covers:   c-1, c-2, c-3, c-5, c-6, c-4 (enabling --diff mode)
       description:
         review.md goes four → six lenses: intro list, §2 spawns six in one block with two new
         lens templates, §3 aggregate, §4 template gains "### Edge cases" + "### Silent failures"
         sections with one example finding each, summary "across 6 lenses", hard rule "all six".
         Edge-case lens: Grep/Read the tree for the full value set, name unhandled values, where
         the set is defined, and file:line; loud catch-all (errors/panics/logs) = handled, silent
         catch-all flagged naming the values that fall into it. Silent-failure lens: discarded
         error returns, empty catch/recover, quiet fallbacks; skip a discard with an adjacent
         reason comment. Both: FLAG by default, BLOCKING only on a spec-criterion path or a
         verify.toml survivor; read verify.toml when present; idioms named for more than one
         language. Code-quality lens drops "error handling" and hands swallowed errors/quiet
         fallbacks to silent-failure; §3 drops any code-quality finding sharing a file:line with a
         silent-failure finding. `--diff <patch>` reviews a local patch: no PR fetch, no post,
         comment saved only. Command description + argument-hint name six lenses and --diff.
       contract:
         - TestReviewPromptNamesSixLenses: delete any lens from the intro list, §2 templates,
           §4 template headings, the "across 6 lenses" summary or the "spawn all six" hard rule
           and it fails; a surviving "four" anywhere in review.md fails it
         - TestReviewEdgeLensRules: the edge-case template section (text between its bold label
           and the next lens label) must contain "Grep/Read", "where the set is defined",
           "unhandled values", "file:line", "errors, panics or logs counts as handling" and
           "silent catch-all … naming the values that fall into it" — drop the loud/silent
           default distinction (locked default_arm) and it fails
         - TestReviewSilentLensRules: silent-failure section must name discarded error returns,
           empty catch AND recover (language_scope: ≥2 idioms), quiet fallback, file:line and
           "adjacent comment stating the reason" skip — dropping the skip (locked
           intentional_discards) fails it
         - TestReviewLensSeverity: both new sections say FLAG by default, BLOCKING only for a
           spec-criterion path or a verify.toml survivor, and read verify.toml — removing either
           trigger from either lens fails it (locked severity)
         - TestReviewQualityLensCedesSilentFailures (c-6): the code-quality template no longer
           contains "error handling" and names the silent-failure lens as owner; §3 contains the
           same-file:line drop rule — revert either and it fails
         - TestReviewDiffMode: §1 names `--diff <path>`, §5 says a --diff run never posts, and the
           command's argument-hint carries --diff
         - TestReviewCommandDescriptionNamesSixLenses: frontmatter description lacks any of the
           six lens names → fails
         - TestSplitReviewMatchesReviewTemplate (updated, c-3): the §4 template now splits into
           exactly 4 findings; findings 3 and 4 come from the Edge cases / Silent failures
           sections with Severity FLAG and a parsed Loc — a new-section bullet in the wrong
           shape (e.g. "- FLAG:" not "- **FLAG** — ") drops the count and fails it
         - TestSplitReviewSixSections (c-3): a synthetic six-section comment splits into one
           finding per bullet, in order, across all six headings — no SplitReview code change
         - TestPromptByteBudget: review.md stays ≤ 15,000 B

Wave 2 (depends t-1)
  t-2  Sweep remaining four-lens doc surfaces
       files:    README.md, docs/dross.1, docs/subagent-offload-audit.md,
                 internal/cmd/review_surfaces_test.go (new)
       covers:   c-5
       description:
         README §commands table row (4-lens → six), README feature bullet, dross.1 /dross-review
         entry and the offload-audit "review" entry all name the six lenses.
       contract:
         - TestNoFourLensSurfaceRemains: walks the repo (skipping .git and .dross/) for
           (?i)\b(four|4)[- ]lens; any hit — README, dross.1, audit doc, review.md, the command
           file, or a surface nobody enumerated — fails naming file:line
         - TestReviewSurfacesNameSixLenses: README's /dross-review table row and feature bullet,
           the dross.1 /dross-review paragraph and the audit doc's "### review" entry each contain
           all six lens names (security, quality, tests, spec-fidelity, edge cases, silent
           failures) — drop one from any surface and it fails naming the surface

  t-3  Give the solo task reviewer both checks
       files:    assets/agents/dross-task-reviewer.md, internal/review/agentdef_test.go
       covers:   c-7
       description:
         New "## Edge cases and silent failures — always blocking" section: partial
         enum/status/flag handling (value set via Grep/Read; loud catch-all = handled, silent
         catch-all flagged) and swallowed errors/quiet fallbacks (adjacent reason comment skips)
         under the same rule sentences as the review.md lenses; every hit is a quality BLOCKING,
         so the verdict is "block".
       contract:
         - TestReviewerSharesEdgeLensRules: each canonical rule sentence (loud catch-all counts
           as handling; silent catch-all flagged naming its values; adjacent-reason discard
           skipped; Grep/Read for the value set) appears verbatim in BOTH review.md and the agent
           body — reword one side only and it fails, so the two cannot drift (c-7 "same rules")
         - TestReviewerEdgeHitsBlock: the new section says a hit is graded BLOCKING and the
           verdict is "block"; grading it FLAG (which the existing rules say never blocks) fails it
         - TestReviewerDefinitionIsolation / TestReviewerBodyMatchesParser stay green: the edit
           adds no tool and keeps one parseable dross-verdict example

  t-4  Proof-run both lenses over Go fixtures
       files:    internal/prtriage/testdata/review-edge/seeded.patch,
                 internal/prtriage/testdata/review-edge/clean.patch,
                 internal/prtriage/testdata/review-edge/seeded-comment.md,
                 internal/prtriage/testdata/review-edge/clean-comment.md,
                 internal/prtriage/review_edge_proof_test.go (new)
       covers:   c-4, c-6 (behavioural proof), c-3 (real comment splits)
       description:
         Seeded patch adds a Go file with a Status const set (Pending/Paid/Shipped/Refunded), a
         switch handling Pending/Paid with a silent `default: return ""`, and an uncommented
         `n, _ := strconv.Atoi(s)` fallback. Clean patch: exhaustive switch with a panicking
         default and a discard carrying an adjacent reason comment. After `make install` (r-01),
         run `/dross-review --diff <patch>` over each and commit the produced
         review-comment.md verbatim as the two *-comment.md files.
       contract:
         - TestReviewEdgeFixturesSeedWhatTheyClaim: the added Go file in each patch parses
           (go/parser); seeded has a switch over Status missing exactly Shipped and Refunded with
           a non-panicking, non-logging default, and a `_` discard of an error with no adjacent
           comment; clean has neither — a fixture that silently stops seeding (or stops being
           clean) fails here before the recorded comments are trusted
         - TestReviewEdgeProofSeeded: SplitReview(seeded-comment.md) returns findings; the
           "### Edge cases" section holds one whose text names Shipped and Refunded and whose Loc
           is the seeded switch's file:line; "### Silent failures" holds one at the Atoi line —
           missing either fails
         - TestReviewEdgeProofNoDoubleReport (c-6): no finding under "### Code quality" in the
           seeded comment carries the silent-failure finding's Loc
         - TestReviewEdgeProofClean: clean-comment.md has no "### Edge cases" or
           "### Silent failures" section with a finding (empty lenses are skipped per §3) — a
           false positive on the loud default or the commented discard fails it
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 edge-case lens flags partial value-set handling, names values + file:line | t-1 (prompt + rule tests), t-4 (behavioural proof) |
| c-2 silent-failure lens flags swallowed errors / quiet fallbacks with file:line | t-1, t-4 |
| c-3 own section in existing finding shape; SplitReview splits them | t-1 (template + split tests), t-4 (real comment splits) |
| c-4 proof run: seeded fixture reports both, clean reports neither | t-4 (t-1 supplies the --diff mode it runs through) |
| c-5 command description, README, summary line name six; no "four lenses" left | t-1 (review.md summary + command description), t-2 (README, dross.1, audit doc, repo-wide sweep) |
| c-6 swallowed error reported by silent-failure only, never code-quality | t-1 (lane hand-off + §3 dedupe rule), t-4 (no-double-report on real output) |
| c-7 dross-task-reviewer checks both, hit fails verdict | t-3 |

7/7 criteria covered.

## Judgment calls

- Added `--diff <patch>` to /dross-review inside t-1 rather than proving c-4 against a throwaway PR — c-4 says "/dross-review over a fixture diff"; posting fixture reviews to the public repo is worse, and hand-spawning the lenses wouldn't be /dross-review.
- No change to internal/prtriage/split.go — SplitReview is section-agnostic (it keys on bullet shape, not heading names), so c-3 is delivered by keeping the new sections in the existing shape and pinning that with tests; rejected a speculative "lens" field on Finding.
- c-6 enforced twice cheaply (code-quality template cedes the territory + §3 same-file:line drop) rather than a separate dedupe task; the proof fixture checks it on real output.
- Proof run records the produced comments as committed testdata asserted through SplitReview, rather than leaving c-4 as an unrecorded manual step at verify — gives verify a test to map c-4 to; rejected a Go harness that drives an LLM (no such infrastructure, would be speculative).
- t-3 placed in wave 2 so its test can assert the rule sentences verbatim in BOTH files ("same rules" in c-7); rejected a wave-1 duplicate needle list that could drift between two tests.
- t-2's guard is a repo-wide regex sweep, not a fixed file list — c-5 says "no surface"; an enumerated list would miss a surface nobody remembered.
- t-4 has five files (4 testdata + 1 test) — over the soft split line but one layer of pure data; splitting fixtures from the run would make one task with no test of its own.
- Left README's v1.9 roadmap checkbox (line 461) unticked — it also covers `dross milestone complete`, which is another phase's work.
- No ARCHITECTURE.md change — it has no /dross-review section to keep truthful; adding one is not traceable to a criterion.
- Version bump / make install are workflow steps (r-01 noted in t-4's description), not tasks.
