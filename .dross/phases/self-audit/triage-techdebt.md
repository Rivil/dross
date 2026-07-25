# Tech-debt triage — self-audit (run 20260725T111945-b0aea96, 100 findings)

Prior run 20260725T111724 had 605 findings; 505 were `.dross/` artefacts —
fixed (scanner now excludes `.dross/` on the ls-files path, `TestTrackedFilesExcludesDrossDir`).

Dispositions for the remaining 100:

- **FIXED — scanner .dross noise** (was 505 findings): `trackedFiles` exclusion,
  own commit + test. The one real product defect this scan surfaced.
- **ACCEPTED — markers (29, now fewer post-fix)**: every TODO/FIXME/HACK/XXX hit
  is self-referential — the scanner's own docs/tests naming its marker
  vocabulary (internal/techdebt/*), the onboard prompt's "mark a TODO" phrasing,
  and ARCHITECTURE.md describing the scanner. Zero real debt markers in the
  codebase. Not fixable without lying in the docs.
- **ACCEPTED — markdown long-lines (ARCHITECTURE.md, README.md, docs/, assets/prompts/)**:
  prose markdown keeps one paragraph per line by convention; wrapping would
  churn every doc diff. Style, not debt.
- **ACCEPTED — oversized test files (phase_test 1607, issue_test 1246, ship_test 1063, …)**:
  table-driven Go test files grow with coverage; splitting them adds navigation
  cost without reducing complexity.
- **ACCEPTED — oversized cohesive command files (issue.go 773, forge.go 664, phase.go 612)**:
  each is one cohesive subsystem (board sync client, phase lifecycle); splitting
  is churn. Revisit only if they exceed ~1000 lines or grow mixed concerns.
- **ACCEPTED — LICENSE (661 lines)**: it's the license.
