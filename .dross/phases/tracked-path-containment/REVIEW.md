# Plan Review — tracked-path-containment

Reviewed: 2026-09-07
Plan: 11 tasks across 4 waves

## BLOCKING

- [test-contract] `assertDoesNotCompile` is unreachable from four of the five tasks
  that depend on it. t-1 puts the helper in `internal/pathfence/pathfence_test.go`
  ("this task also adds the shared test helper"), but a `_test.go` identifier in
  package `pathfence` is visible only to that package's own test binary. t-3's
  contract uses it from `internal/security` / `internal/quality` tests, t-5 and
  t-11 from `internal/cmd`, t-8 from `internal/cmd`. There is no shared test-helper
  package in this repo to fall back on — I checked all 34 dirs under `internal/`;
  none is a testutil/dtest package and no non-test file exports an `Assert*` helper.
  Five test-contract lines cannot compile as written, and they are the lines that
  carry the c-4 "fails to build" guarantee at every adopted site.
  Suggestion: name the helper's home explicitly — a non-test file in a small
  exported package (`internal/buildfence`, or an exported non-test function in
  `internal/pathfence`) — and add that file to t-1's `files`. Until it has an
  importable home, four tasks' primary assertions are unsatisfiable.

- [test-contract] t-8's redProofDocSHA and doctor contract lines cannot pass once
  `redProofPin.Doc` is `pathfence.Contained`. "redProofDocSHA on the same doc
  returns a containment error, not an os.ReadFile error — asserted on the message"
  requires calling `redProofDocSHA` with an escaping doc; if it takes a `Contained`
  (as the description states) no test can construct that argument, because `Contain`
  is the only constructor and it refuses. The same applies to "doctor over that
  record reports the containment refusal as an escape rather than under the
  'cannot be read' wording": the plan says both that `discoverRedProofPins`
  constructs pins by calling `Contain` and that the refusal surfaces at
  `doctor.go:604-608`. Those are mutually exclusive — a pin that fails `Contain`
  at discovery never reaches :604. And the discovery path behaves differently than
  the plan assumes: `redProofChecks` (doctor.go:567-570) collapses any discovery
  error into a single `could not read red-proof pins: %v` line and returns, so one
  escaping doc suppresses every other pin's verdict. That regression is undeclared.
  Suggestion: decide where `Contain` runs — at discovery (and then rewrite both
  contract lines to assert the `redProofChecks` line, and state the
  one-bad-pin-hides-the-rest consequence), or per-pin inside `redProofPinLines`
  (and keep the :604 arm split, but then `discoverRedProofPins` must keep `Doc` a
  string, which contradicts the retype).

- [coverage] t-3 moves the real `os.*` calls into `internal/security` and
  `internal/quality`, but t-10's residual scan does not cover those packages. The
  scan's declared set is internal/cmd, internal/verify, internal/phase,
  internal/changes, internal/testlane, internal/remote. t-3's contract line
  ("t-10's scan over both packages reports no direct os.* call on a contained
  path") names a scan that never visits them. After t-3, `security.Save`,
  `security.Load`, `security.WriteScaffoldSpec` and their quality twins are exactly
  the "reader that unwraps a checked value with `.String()` to reach os.*" that
  c-4's residual clause promises to catch, and nothing catches them. This is the
  write path c-3 is about, so the hole is in the criterion's own centre.
  Suggestion: add internal/security and internal/quality to t-10's scanned package
  set and to its vacuity guard ("visits at least one file in each of the six named
  packages" becomes eight), or delete the claim from t-3's contract and say plainly
  that those two packages are guarded by the type alone.

## FLAG

- [antipattern] t-8 does not say where `discoverRedProofPins` gets a containment
  root. It takes only `root` (the `.dross` dir, redproof.go:119); `Contain` needs
  the repo root. Two resolutions exist and they have different file scopes: derive
  `filepath.Dir(root)` internally (no signature change, file list stands), or add a
  `repoDir` parameter — which breaks two production callers that are NOT in t-8's
  file list, `internal/cmd/redproof_lifecycle.go:39` and
  `internal/cmd/redproof_repoint_cmd.go:46`.
  Suggestion: state the derivation in the description. If the signature changes,
  add both files.

- [test-contract] t-1's line `Contain(root, a, "a/../b.md")` returns
  `Join(root, "b.md")` contradicts the same task's `String()` rule. `abs` is
  unexported and `String()` returns `rel`, so nothing outside pathfence can observe
  `Join(root, "b.md")`; from inside the package the test can read `abs` directly,
  but then the line is asserting a field, not a return value.
  Suggestion: reword to assert the internal `abs` field, or assert
  `String() == "b.md"` and leave the join to the seam test that already chdirs away.

- [test-contract] t-7 changes testlane's normalization on darwin without asserting
  it. `testlane.normalize` uses `filepath.ToSlash` (match.go:128); pathfence's
  `InTree` folds backslashes unconditionally (t-1, deliberately). For `a\b/../c`
  testlane returns `"c"` today on darwin and `"a/c"` through `InTree`. t-7's
  contract asserts neither form. This is precisely the divergence t-1 documents as
  load-bearing, left unpinned in the one task that consumes it.
  Suggestion: add a line fixing testlane's answer for a backslash-carrying path,
  whichever way you decide it should go.

- [test-contract] t-5 does not pin `NormalizePath`'s false-branch return. It
  returns `""` today (scope.go:349-351); `InTree`'s false branch returns the
  cleaned path, and t-1 pins that deliberately. t-5's contract asserts only
  `ok=false`. `NewScope` is unaffected (it appends the raw input to `rejected`,
  scope.go:114 and :135), but `NormalizePath` is exported.
  Suggestion: one line asserting `NormalizePath("../x")` still returns `("", false)`.

- [coverage] c-4's first clause — "every site that opens a tracked-artifact path
  accepts a value only the shared check can construct" — is built by t-3, t-5, t-8
  and t-11, none of which list c-4 in `covers`. Only t-10, the guard, claims it.
  Cut or descope any of those four and c-4 still looks covered while the guarantee
  is gone.
  Suggestion: add c-4 to the `covers` of the tasks that do the retyping.

- [claim] t-1's compile-fence precedent is misquoted. "ceiling_test.go:287 declares
  `module mutantproof`" — the file is `internal/mutation/ceiling_test.go` (path
  unqualified in the plan) and the `module mutantproof` write is at :290, with the
  `package mutantproof` source at :293. The substance of the warning is right; the
  citation is not.
  Suggestion: correct to `internal/mutation/ceiling_test.go:290`.

- [claim] Three more line-number drifts, all small but all wrong: t-5 cites
  "NormalizePath (scope.go:349)" — the function is at scope.go:327, and :349 is its
  `".."` arm; t-5 cites "scope.go:167" for the `ignored out-of-repo path` append —
  it is at :169; t-6 cites "saveIfValid (task.go:438)" — `saveIfValid` is at :439
  and its `ValidatePlan` call at :440.
  Suggestion: fix them; the executor navigates by these.

- [granularity] Three split candidates. t-8 touches 10 files (six of them tests
  adapting one retyped symbol) and carries the phase's densest design decision.
  t-3 touches 8 files across two packages. t-5 touches 6 files across two packages
  and does two separable jobs (the gate and the conversion) that the description
  itself insists are separate.
  Suggestion: at minimum consider splitting t-8's `redProofDocSHA` retype + its
  five test call sites from the `applyRedProofRepoint` seam adoption.

## NOTE

- [wave-order] The wave arithmetic is correct against this repo's rule. Every
  `depends_on` lands strictly greater than its deepest dependency under
  `deriveWave` (internal/phase/plan_edit.go:54-70), and t-10's deepest dep is wave
  3 (t-8, t-11) so wave 4 is right. I checked all 11 file lists pairwise: no two
  tasks in the same wave share a file. t-2's note on why it carries no `depends_on`
  despite sharing a directory with t-1 is correct — declaring one would push it to
  wave 2 and cascade t-9 and t-10.

- [antipattern] t-2's insistence on `package pathfence_test` is right and catches a
  failure that would otherwise appear only in wave 2: `fields_test.go` imports
  internal/changes, internal/verify, internal/phase and internal/project, and t-5
  and t-6 make two of those import pathfence. An internal test file would compile
  green in wave 1 and cycle in wave 2.

- [claim] t-11's per-site call map is accurate. All eight `containedPath` sites are
  where the plan says (security.go:45,152,160,204; quality.go:39,122,130,150), the
  two `os.WriteFile` report writers are at security.go:229 and quality.go:166, and
  the doc comment making the never-true finding-derived claim is at
  security.go:184-187. The correction of the earlier "they all write" error landed.

- [claim] t-9's tag-option finding is real and load-bearing: every field it must
  reach in internal/project carries `,omitempty` (project.go:274-280, :284), so an
  exact whole-tag match finds none of them. The field enumeration is also complete
  — 16 tagged path-shaped fields exist across the four packages, 15 are
  string/[]string, and `project.Remote.Public` (bool, project.go:231) is the only
  one the type test drops, exactly as claimed. The ">= 10" vacuity guard is
  satisfiable with margin, and t-2's by-name list accounts for all 15.

- [claim] t-10's "legal non-path `..`" shapes check out: topology.go:80,
  doctor.go:980, milestone.go:1035 and basebranch.go:69 are all `a+".."+b` string
  concatenation, and refguard.go:56 is `strings.Contains` with no path call
  anywhere in the function. The marker set as specified does not reach any of them.

- [claim] The live `os.WriteFile(p, []byte(b.String()), 0o644)` instances t-10
  bans exist today at security.go:229 and quality.go:166 and are removed by t-11
  (wave 3) before t-10 (wave 4) scans — the ordering works. The test-file exclusion
  is also justified: internal/cmd/mutation_remote_wiring_test.go:47 is the shape it
  describes.

## Summary
Structurally sound and unusually well-verified against the tree, but three
blocking defects remain: the compile-fence helper has no importable home for four
of the five tasks that use it, t-8's redProofDocSHA and doctor contract lines
cannot pass once the field is retyped, and t-3 relocates the real writes into two
packages t-10's residual scan never visits.
