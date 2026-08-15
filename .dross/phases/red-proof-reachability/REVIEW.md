# Plan Review — red-proof-reachability

Reviewed: 2026-08-14
Plan: 5 tasks across 2 waves

## BLOCKING

(none)

Coverage is complete — c-1→t-1, c-2→t-2/t-4, c-3→t-2/t-4, c-4→t-2/t-5, c-5→t-3/t-4 — no
task contradicts a locked decision, and no task prescribes a rules.toml violation.

## FLAG

- [antipattern/missing-file] t-3 moves the `base commit:` parser "out of
  hostile_config_test.go into shared code" but does not list
  `internal/cmd/hostile_config_test.go` in its `files`. `redProofSHA` is declared at
  `internal/cmd/hostile_config_test.go:414` in **package cmd**, and t-3's new
  `internal/cmd/redproof.go` is the same package — declaring it there while the test file
  still holds it is a redeclaration compile error that breaks the whole package, not just
  the one test. The file it must edit is also t-5's *only* file, so the move straddles two
  tasks in different waves.
  Suggestion: either add `internal/cmd/hostile_config_test.go` to t-3's files (accepting
  that t-5 then re-edits it) or move the parser in t-5 and have t-3 add a new parser under
  a distinct name.

- [wave-order] t-5's `depends_on = ["t-2", "t-3"]` omits t-1, but its second contract line
  requires the failure message to name "the phase's fork point" — that value only exists
  once t-1's resolve-and-cache backfill lands. As written t-5 is runnable before t-1 and
  would have nothing to name.
  Suggestion: add t-1 to t-5's `depends_on`, or drop the fork-point half of that contract
  line.

- [test-contract] t-2's contract pins the loose-object case (object present, no ref) and
  the shallow/no-origin case, but never the case c-2 is actually about after GC: a SHA
  that is **absent from the object database entirely**. `git branch -r --contains <sha>`
  exits non-zero with "malformed object name" there, and the obvious implementation folds
  a non-zero git exit into cannot-determine — which silently downgrades c-2's hard failure
  to a warning. Nothing in the plan would catch that.
  Suggestion: add a contract line asserting an absent object classifies unreachable, not
  cannot-determine.

- [granularity] t-3 is 5 files and four separable concerns: a `changes.Changes` schema
  field, a new user-facing CLI verb with validation, glob-based pin discovery, the shared
  doc parser move, plus writing a live data record. It also covers a single criterion
  (c-5) of which only the discovery half is really c-5.
  Suggestion: split into "red_proof record + `phase red-proof set` verb" and "pin
  discovery + shared doc parser"; the second is what c-5 rests on.

- [antipattern/missing-file] No task touches `README.md` or `docs/dross.1`, yet t-3 adds a
  user-facing verb (`dross phase red-proof set`) and t-4 adds a doctor check. README:189
  enumerates every `dross phase` subcommand and README:202 enumerates every doctor check,
  so both rows go stale on merge. Repo precedent is explicit: config-trust-hardening's t-6
  updated `README.md` + `docs/dross.1` in the same task that added `dross local`.
  Suggestion: add both docs to t-3/t-4's files, or add an explicit docs task.

- [rules/r-01] rules.toml r-01 is a **hard** rule: after editing Go code, `make install`
  before relying on the change. t-3's description ends with "The existing
  config-trust-hardening pin is recorded by running the verb" — a shell invocation of
  `dross` that does not exist in the installed binary at that point.
  Suggestion: state the build step in t-3 (`make install`, or `go run ./cmd/dross phase
  red-proof set …`) so the executor doesn't run a stale binary.

- [wave-order] Three wave-1 tasks share files with no `depends_on` encoding the order:
  t-1 and t-3 both edit `internal/changes/changes.go` and `internal/cmd/phase.go`; t-2 and
  t-3 both own `internal/cmd/redproof.go` and `internal/cmd/redproof_test.go` (t-2 creates
  them). Serial `dross task next` execution in id order happens to give a workable
  sequence, so this is survivable rather than broken — but the ordering is implicit, and
  `--from t-3` or any reordering breaks it.
  Suggestion: make t-3 `depends_on = ["t-2"]` (wave 2) or state in t-3's description that
  it extends files t-1/t-2 create.

## NOTE

- [locked-decision] `reachability_scope` says "contained in some remote-tracking branch
  (origin/*)". t-2's mechanism, `git branch -r --contains`, matches every remote's
  tracking refs (and `origin/HEAD`), not just `origin/*`. Harmless on this repo — one
  remote — but a contributor with a fork remote gets a wider verdict than the decision
  describes. `git for-each-ref refs/remotes/origin` is the exact scope if it matters.

- [test-contract] t-3's `TestDiscoverRedProofPinsFindsRecordedPin` and t-4's
  `TestDoctorCleanRedProofsHaveNoFindings` both assert against the live `.dross/phases`
  tree, so `go test ./...` reddens the day any recorded pin rots or its phase dir is
  pruned. That is arguably the point — and the existing `TestRedProofPinsBaseCommit`
  already works this way — but it means pin rot in an unrelated phase becomes everyone's
  red CI, and the deferred repoint verb is what would make that cheap to fix.

- [factual] Verified: the c-5 pin `a6ef7295996db1d737be2ae2183f45e376ba3c19` is currently
  reachable from `origin/main` and `origin/milestone/v1.3`. t-4's false-positive gate is
  satisfiable today; the two live-repo assertions above are not in conflict.

- [strength] Recording the fork point inside `forkPhaseBranch` is the right seam:
  `changes.SetBase` already lives there (`internal/cmd/phase.go:897`), so both call sites —
  `phase.go:164` and `phase_lifecycle.go:188` — get the field with no extra file and no
  chance of the insert/move path silently skipping it.

- [strength] The test contracts are failure-mode-shaped and grounded in real repo detail
  rather than invented: the emphasised/backticked `**base commit: \`<sha>\`**` form is what
  `fixtures/hostile-config-c5/RUN.md:99` actually carries, "rev-parse --verify succeeding
  is not reachability" is exactly the weakness in the current check at
  `hostile_config_test.go:401`, and the shallow-clone skip-with-log is preserved rather
  than quietly hardened into a failure.

- [strength] c-4 ("one implementation") is enforced structurally by
  `TestRedProofCheckHasOneCaller` instead of by convention, matching the repo's existing
  AST-gate precedent (`gitargs_audit_test.go`). t-4 also carries an explicit
  false-positive gate — the check most plans omit.

## Summary

Structurally sound and unusually well-grounded in the actual code, with nothing blocking;
fix t-3's missing `hostile_config_test.go` (a package-wide compile break), t-5's missing
t-1 dependency, and the unpinned "object absent entirely" classification before executing.
