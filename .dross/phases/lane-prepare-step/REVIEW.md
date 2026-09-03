# Plan Review — lane-prepare-step

Reviewed: 2026-08-27
Plan: 7 tasks across 5 waves

## BLOCKING
(none)

## FLAG

- [coverage-depth] c-4's run-time half has no test contract anywhere. t-3 claims
  c-4 and asserts the fingerprint, `dross trust --lane` output, and doctor rows —
  but nothing asserts the criterion's actual run-time sentence: "refuses that lane
  only, naming the line it refused, and the other lanes still run". Concretely,
  `laneConsentRefusal(name, line, state, cerr)` in `internal/cmd/trust.go:353`
  interpolates exactly one line into every arm. Once prepare is inside the
  fingerprint, a lane refused *because its prepare changed* prints only its
  command — the user reads a line that did not change and re-grants a bootstrap
  they never saw. That is the precise failure c-4 exists to prevent, and no
  contract catches it.
  Suggestion: add a contract to t-3 asserting a run against a two-lane fixture
  where lane A's prepare alone went stale: the refusal text contains A's prepare
  line, lane B's command still reaches the recorder, and the run exits 6.

- [coverage-depth] c-1's "a lane declaring none spawns exactly what it spawns
  today" is unasserted. t-1 covers c-1 entirely at the config/CLI layer (field,
  flag, list row, validate). t-3 asserts the *fingerprint* is unchanged for a
  no-prepare lane; t-2 asserts a red no-prepare lane still exits 1. Neither
  asserts the spawn set. An implementation that unconditionally spawns the
  prepare — turning an absent prepare into `sh -c ""` — passes every contract in
  the plan.
  Suggestion: add to t-4 a contract that a no-prepare lane records exactly one
  spawn (its command), asserted on the recorder count, on both transports.

- [test-contract] t-2's contract "TestExitCodesArePairwiseDistinct in
  test_files_test.go fails if exitPrepareFailed collides with an existing code"
  is false as written. That test (`internal/cmd/test_files_test.go:137`) walks a
  hand-maintained map literal, not the const block — it currently lists only
  suiteFailed, badFileSet, transport, partial, nothingMeasured. It already omits
  `exitLaneRefused = 6`, which is exactly how a hand-maintained map rots. Nothing
  fails unless `exitPrepareFailed` is added to that map by hand, and
  `test_files_test.go` is not in t-2's files list.
  Suggestion: add `internal/cmd/test_files_test.go` to t-2's files and restate
  the contract as an action ("exitPrepareFailed is added to the map"), or make
  the assert structural. Adding the missing `exitLaneRefused` entry in the same
  edit is nearly free.

- [test-contract] Several contracts name files their task does not list, so the
  work they describe has nowhere to land:
    - t-1: "asserted on the rendered bytes beside TestNoTestLaneIsAbsentFromTheDocument"
      — that test is in `internal/project/project_test.go`, absent from t-1's files.
    - t-3: two contracts assert `dross doctor` output. No doctor test file is
      listed (`doctor_test.go`, `doctor_remote_test.go` exist); doctor's lane
      consent rows have no existing test at all — grepping the stale-lane wording
      from `doctor.go:1274` returns zero hits in `*_test.go`, so this is new
      test scaffolding, not an extension.
    - t-3: the stale/refusal run path lives in
      `internal/cmd/test_lane_consent_test.go`, which is listed on t-2 (wave 1),
      not t-3.
  Suggestion: add the missing files to each task's list. No wave conflict results
  — t-2 and t-3 are in different waves.

- [granularity] t-1 touches 5 files and bundles two separable concerns: the
  schema+CLI surface (project.go, test_lane.go) and a `dross validate` rule the
  description itself flags as a deliberate addition rather than a reading of any
  locked decision (validate.go, validate_lane_test.go). The whitespace-only
  validate rule is the one piece of t-1 with an independent rationale and an
  independent failure mode.
  Suggestion: optional split — `t-1a` schema + add-flag + list row, `t-1b` the
  whitespace-only validate rule. Both stay wave 1; t-1b depends on t-1a. If kept
  as one, no correctness issue.

## NOTE

- [wave-order] Wave assignments are all load-bearing. t-3 genuinely needs t-1's
  field; t-4 and t-6 genuinely need t-3's fingerprint (t-6's "grant is kept, goes
  STALE" contract is unwritable without it; t-4 without t-3 would spawn a prepare
  no grant ever covered); t-5 needs t-2's code and t-4's spawn seam; t-7 needs
  all three surfaces to exist. Nothing could drop a wave.

- [strengths] Contracts name the failure mode, not just the expectation. "a
  batched [prep-go, prep-docs, go-cmd, docs-cmd] fails", "a `return` instead of a
  `continue` fails the goCmd assert", transcript order asserted by byte offset
  rather than presence, and absence asserts on the recorder — these are contracts
  a wrong implementation cannot satisfy by accident. This is well above the usual
  bar.

- [strengths] t-3's backward-compat assert — a no-prepare lane must fingerprint
  to `Fingerprint(lane.Command)` byte-for-byte, with framing applied
  conditionally — catches a trap that would otherwise stale every existing lane
  grant on every machine on upgrade. So does the forgery assert (framed bytes fed
  back as a bare command must MISS). Neither is obvious.

- [strengths] t-5's "a run where EVERY lane's prepare failed exits 7, NOT 5"
  catches the `misses == len(runnable)` interaction at `internal/cmd/test.go:497`
  that would otherwise demote a total prepare failure to the lowest-ranked
  outcome. Non-obvious, and reading it out of existing code is real work.

- [antipatterns] Checked and clean: no "set up X" tasks, no artificial splits, no
  invented files (`test_lane_prepare_test.go` and `test_lane_edit_test.go` are
  created by t-4 and t-6 respectively). Every existing symbol a contract names —
  `TestTomlFieldsCarryMatchingJSONTags`, `TestExitPrecedenceIsTotal`,
  `TestReadmeDocumentsTestLanes`, `TestOptionsDocumentsTheSelectorSurfaceCorrectly`,
  `TestNoTestLaneIsAbsentFromTheDocument`, `shArgvFor`, `laneLabel`, the
  `test lane %q failed` hardcode at test.go:604 — was verified present. Exit code
  7 is genuinely free (1–6 are taken).

- [locked-decisions] All six locks were cross-checked against every task
  description, files list, and contract. No contradiction. The three that are
  easiest to violate silently — prepare_scope (no dedup), prepare_selector_miss
  (skipped lane never prepares), prepare_locality (both transports) — each have a
  dedicated t-4 contract that names the lock.

- [rules] `.dross/rules.toml` r-01: t-7 edits `assets/prompts/execute.md` and
  `assets/prompts/options.md`, which are not live until `make install` re-links
  them. The gate tests read the source assets, so CI is unaffected — but any
  manual check of the installed prompt after t-7 needs `make install` first. No
  task implies a violation; runtime.mode is `native` and every command in the
  plan is a Go build/test.

## Summary
Strong plan with no blocking defects; the real gaps are two criteria (c-1, c-4)
whose run-time halves are claimed by a task but asserted by no contract, plus one
contract (t-2's pairwise-distinct claim) that describes a test which would not
actually fail.
