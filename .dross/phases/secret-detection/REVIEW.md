# Plan Review — secret-detection

Reviewed: 2026-09-19
Plan: 8 tasks across 4 waves

## BLOCKING
(none)

## FLAG
- [test-contract] t-8's `TestMarkerNeverAppearsInNonTestGoOutsideArgfence` ("the only non-test .go file carrying the marker is internal/argfence/policy.go") is contradicted by the plan itself: t-1 puts the literal in `internal/secretscan/secretscan.go` (`const AllowMarker`, and `Report()` names it in the remedy line) and t-5 puts it in `internal/cmd/ship.go`'s refusal message ("mark the line dross:allow-secret"). As written the test fails deterministically once t-1 lands.
  Suggestion: scope the assertion to *silencing* markers (marker stripped → line hits), the same definition `TestAllowMarkerSitesArePinned` already uses, or exempt `internal/secretscan/` and the two refusal-message sites by name.

- [antipattern] `scanDrossArtifacts` is called with different roots across tasks: t-2 defines `listDrossArtifacts(root)` / `scanDrossArtifacts(root)` and t-5 calls `scanDrossArtifacts(repoDir)` from cleantree.go and `scanDrossArtifacts(root)` from ship.go. In `internal/cmd`, `root` is the `.dross` dir and `repoDir` is its parent (ship.go:94-98). `git ls-files -- .dross` must run from repoDir and the contracted location format `.dross/phases/p/notes.md` is repo-relative, so the function wants repoDir; validate.go only has `root` and would pass `filepath.Dir(root)`. t-5's `TestShipAndValidateShareOneScanner` requires the same callee in all three files, so the signature has to be settled in t-2.
  Suggestion: pin the signature in t-2's description as `scanDrossArtifacts(repoDir string)` and state that `pathfence.Contain` is rooted at `filepath.Join(repoDir, ".dross")` with the `.dross/`-prefixed rel used only for the Hit location.

- [test-contract] The authorization-header rule's value class is undefined ("Basic|Bearer then ≥8 value chars"). The tracked tree carries `"Bearer "+c.token` in four production files (forge.go:863, github.go:386, youtrack.go:944, ship/gitlab.go:228) and `Authorization: Bearer <token-from-auth_env>` in the youtrack-board-integration plan/synthesis. None of these are in t-8's pinned marker table, so t-1 is implicitly relying on a class that excludes `"`, `<`, `+` and `$` — but the benign corpus only pins `Bearer <token>` (7 chars, which passes on length alone and proves nothing about the class).
  Suggestion: give the auth-header rule the same value class and `$`/`<>` exclusion as key-context, and add benign rows that exercise it: `"Bearer "+c.token` and `Bearer <token-from-auth_env>` (both ≥8 chars after the scheme).

- [antipattern] t-6's transport enum walks `exec.Command` sites and requires "each site's enclosing function to be a registered Transport", but the only `exec.Command` in ship/forge is inside a func literal assigned to the package var `ghCommand` (open.go:69) — no enclosing FuncDecl, and neither disposition (Screened / ReadOnly) describes "the seam whose only caller is screened". The walker will either flag it or need an unstated special case.
  Suggestion: decide in the description: either register `ship.ghCommand` under a third disposition (e.g. `Seam{ScreenedCaller: "ship.screenedGH"}`) with a Validate rule, or have the walker treat a FuncLit bound to a package-level var as the transport named by that var, and cover the choice in the leaky fixture.

- [test-contract] t-8's `TestValidateOnDrossItself` runs the live `validate` walker, whose scope (t-2) is tracked PLUS stageable-untracked `.dross/` files, while the sibling self-scan tests walk `git ls-files` (tracked only). The two scopes legitimately disagree on any developer checkout with untracked `.dross/` scratch — including this REVIEW.md and the phase's own panel/ notes before their bookkeeping commit — so the test is non-hermetic and can go red without a code change.
  Suggestion: either run the validate assertion against a temp clone of HEAD (tracked set only), or state that the test is allowed to read the working checkout and that the panel/ notes and REVIEW.md must be marker-clean before t-8 runs.

- [spec-fidelity] c-3's premise "ship's pre-flight which runs [validate]" is false today (ship.go never calls validate) and t-5 explicitly chooses not to make it true ("Ship calls the shared scanner, not the whole `dross validate`"). The observable guarantee holds, but the verifier mapping c-3 will look for ship running validate and not find it.
  Suggestion: record the plan-time decision where verify will see it (a `[[decisions]]` entry or the phase's verify notes), so c-3 is judged on "ship refuses on a hit via the same scanner" rather than on the literal premise.

- [granularity] t-4 touches 8 files and t-8 touches 9 across production Go, test Go and `.dross/` markdown. Both are cross-cutting by nature (five of t-4's files are the ghCommand→screenedGH swap that must land with its AST test; t-8 is a marker sweep) and neither should be split. Recording it so the count isn't read as an oversight.
  Suggestion: none — leave as is.

## NOTE
- [strengths] Every line anchor in the plan was checked and is accurate: the four `doRaw` lines, `jsonPost`/`bbRequest`/`gitlabReq`/`jsonGet`, all five `ghCommand(` call sites, the six marker sites, the 15 `*File` consts, the 34 writer files, and the tracked-file floors (1775 total / 966 under `.dross/`). The author read the tree rather than guessing.
- [strengths] The hit corpus is built at runtime and the residual scans (t-6, t-7) are calibrated against known-answer fixtures rather than the post-fix tree — the plan avoids the two ways a self-scanning phase usually eats itself. The echo-free property test (50 random values per rule, 6-byte window) is a real c-5 proof, and `TestKeyContextIdentityCarveOutIsKeyAware` reuses the existing key-aware `IdentityIDAllowlist` instead of re-deriving it.
- [strengths] The screen sits at the transport (doRaw / jsonPost / screenedGH), so the c-2 guarantee covers every current and future composer with four+three one-line edits, and t-6 then proves no transport is missing. That is the right layer.
- [locked-decisions] `entropy_rules` enumerates `password|token|secret|api_key|authorization`; t-1's key-context rule adds `passwd|pwd|access_key`. A superset of labelled keys is within the decision's spirit (still no bare entropy rule), not a conflict. Likewise the AWS `EXAMPLE` carve-out is a second fixed rule carve-out, consistent with `allowlist_route`.
- [self-scan] A crude simulation of the key-context rule over the tracked tree reproduces the plan's six Go marker sites exactly, plus the phase's own panel notes: `panel/mvp.md:38` (PEM header line), `panel/synthesis.md:171,222` and `panel/verification.md:58,291` (copies of the argfence `--end-of-options` row). t-8's "plus whatever this phase's own artifacts need" is these; pre-populating the pinned table saves a red-then-fix cycle. Note `synthesis.md:171` already contains the marker text inline and will count as a *silencing* marker.
- [design] `secretscan` importing `security.IdentityIDAllowlist` makes `forge` and `ship` transitively depend on `internal/security` (the gitleaks package) for one regex. No import cycle (security imports only pathfence) and no runtime gitleaks call, so `pattern_source` holds — but a leaf `identityid` package would keep the dependency graph honest.
- [duplication] t-4's `TestNoRawGhCommandCallOutsideTheSeam` and t-6's leaky-fixture arm (d) prove the same property; fine to keep both, but the t-6 one is the durable guard.
- [registry-shape] t-7's `Writer` registry is keyed by file with one disposition per entry; the description does not say whether a file may carry more than one. Today all 34 look single-class, but `internal/pathfence/pathfence.go` (the `WriteFile` primitive itself) fits none of UnderDross / MachineLocal / OutsideDross and will need an explicit answer.
- [wave-order] t-1 is the only wave-1 task and carries 11 contracts. `payload.go` (ScanPayload/ScanArgv) is the sole dependency of t-3/t-4 while t-2 needs only rules+Scan; splitting would let t-2 start earlier only if wave 1 ran two agents. Not worth it in pair mode.

## Summary
Coverage is complete, no locked decision is contradicted, and every anchor is real; the seven flags are contract-level fixes (one self-contradicting t-8 test, an unpinned auth-header value class, a root/repoDir mismatch, the ghCommand FuncLit gap, a non-hermetic live-validate test, the c-3 premise) that are cheaper to settle now than mid-task.
