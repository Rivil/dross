# Plan Review — dependency-update-automation

Reviewed: 2026-09-22
Plan: 3 tasks across 1 wave

## BLOCKING

(none)

Coverage is complete: c-1 and c-2 → t-1, c-3 and c-4 → t-2, c-5 → t-3. No task
description, file, or contract contradicts a locked decision — t-1 adds no
`target-branch` (target_branch), nothing enables auto-merge (auto_merge), the
npm entry is the Stryker fixture grouped with cooldown (npm_fixture_scope), and
the security_bump_release procedure checks out against the real release.yml
(`workflow_dispatch` with an explicit `version` input exists at line 16-21;
push-to-main recomputes the tag from project.toml, so a bot merge cuts nothing).
rules.toml carries one rule (r-01, `make install` staleness) and no global
`~/.claude/dross/rules.toml` exists; no task implies a violation — these are test
files, not binary behaviour relied on in-session.

## FLAG

- [antipattern] t-1's guard test is line-based over the *text* of
  dependabot.yml. It proves the file says `weekly`, says `cooldown`, says the
  group — not that GitHub parses it. A misspelled key, a wrong nesting level, or
  a `cooldown` block GitHub ignores for one ecosystem all leave the test green
  while Dependabot silently opens nothing. That is the exact vacuous-pass shape
  this repo keeps removing elsewhere (`DROSS_REQUIRE_E2E` in ci.yml). t-3 already
  establishes the pattern of a gh-api-verified non-Go check; nothing does the
  equivalent for the config itself.
  Suggestion: extend t-3's execute-time evidence (or add one to t-1) to read
  GitHub's parse result for the config — a `gh api` call against the repo's
  Dependabot config/last-checked state, recorded as evidence alongside c-5's two
  calls.

- [antipattern] Neither spec nor plan establishes that `cooldown` is honoured for
  all three ecosystems. c-2 says "every ecosystem"; if `cooldown` is not
  supported for `gomod` or `github-actions`, the guard test passes on the file
  text while the 7-day window doesn't exist for that ecosystem — the criterion is
  green and the protection is absent.
  Suggestion: confirm ecosystem support against current GitHub docs as the first
  step of t-1, before writing the config or the test around it, and record what
  was confirmed.

- [granularity] t-3 has `files = []`. Under this repo's atomic-commit-per-task
  convention it produces no commit and leaves no artifact in the tree; the only
  record that vulnerability alerts and automated security fixes are on is the
  verify run's c-5 evidence. Six months from now nothing in the repo shows the
  setting was ever enabled, and nothing detects it being turned off.
  Suggestion: name the artifact that records it — the captured `gh api` output as
  phase evidence, or a line in the dependabot.yml header noting the two repo
  settings this config depends on.

- [test contract] t-2's c-4 contract says "loses its trailing `# vX.Y.Z`
  comment" but never pins which comment shapes the sweep accepts. Strict
  three-part rejects `# v5` or `# v4.2` — and Dependabot writes whatever tag it
  resolved. Any future action that publishes only a major tag would then need the
  hand edit c-3 exists to eliminate. Latent today: all nine `uses:` in
  ci.yml/release.yml carry three-part comments.
  Suggestion: add a contract line stating the accepted shape explicitly (e.g.
  "`# v5` fails" or "`# v5` passes"), so the decision is made now rather than
  discovered by a red bot PR.

- [test contract] t-2 changes `setupGoSteps`/`stripYAMLComment` to retain the
  trailing comment, but the existing `TestSetupGoStepScanner` in
  toolchain_source_test.go asserts exact `setupGoStep` struct literals
  (`{line: 5, sha: "1111", goVersionFile: "go.mod"}`, and two more). Adding a
  version field breaks all three `want` entries. The contract doesn't mention it,
  so the refactor's blast radius is understated.
  Suggestion: add a contract line for the scanner fixture — the version comment
  is captured when present and empty when absent (the fixture's
  `actions/setup-go@3333` line has no comment and is the case that matters).

- [test contract] The locked `target_branch` decision (PRs target main, no
  override) is the one locked decision with no guard. t-1's contract covers
  ecosystems, schedule, grouping, and cooldown; a later `target-branch:
  milestone/v1.8` added by hand would silently no-op every update and pass every
  test — which is the precise failure the decision's `why` names.
  Suggestion: add a contract line to t-1 asserting no ecosystem block carries a
  `target-branch` key.

## NOTE

- [antipattern] Verified against the repo, not assumed: all nine `uses:` lines in
  .github/workflows/*.yml are already 40-hex SHA + `# vX.Y.Z`, so t-2's sweep
  goes green on the existing tree. The plan correctly lists no workflow files —
  there is no hidden "fix the workflows first" task missing from wave 1.

- [antipattern] ci.yml triggers on `pull_request` and references no `secrets.*`
  (only release.yml does). Dependabot PRs run under the restricted token with no
  secrets access, so full CI will run on bot PRs — c-3's "passes CI without a
  hand edit" has no secrets blocker.

- [locked-decisions] c-3 moves the setup-go trust anchor from the exact SHA
  constant to the `# vX.Y.Z` comment, which is author-supplied text: a SHA with a
  falsified or stale comment now passes where the constant would have caught it.
  That is coherent with locked `auto_merge` (a human reads every bot PR, so the
  comment is checked by eye), but it is a deliberate weakening and is worth
  saying so in the code comment replacing `setupGoMinSHA` — that constant's
  current doc comment explicitly promises "a workflow pinned to any other SHA
  fails the test".

- [wave-order] All three tasks are genuinely independent and correctly sit in one
  wave. No manufactured serialization, and no task waits on output it doesn't
  need.

- [granularity] Putting c-3 and c-4 in one task is the right call, not a squash:
  the sweep's line parser is the same parser `TestToolchainSingleSource` needs to
  read the version comment. Splitting them would have invented a wave-2
  dependency for no benefit.

- [test-contract] Both new tests carry an explicit "the scanner cannot go green
  by parsing nothing" inline-fixture contract line, matching the repo's existing
  anti-vacuous-pass idiom. That is the failure mode line-based YAML guards
  actually have, and it is contracted rather than assumed.

## Summary

Coverage, locked decisions, wave order, and granularity are all sound; the real
gap is that every c-1/c-2 assertion tests the *text* of dependabot.yml rather
than GitHub's acceptance of it, which the plan could close cheaply by extending
t-3's existing gh-api evidence pattern.
