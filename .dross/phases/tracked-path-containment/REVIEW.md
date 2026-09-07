# Plan Review — tracked-path-containment

Reviewed: 2026-09-07
Plan: 10 tasks across 3 waves

## BLOCKING

- [coverage] c-6's operative clause is the item the plan deferred. c-6 reads
  "...so adding a consumer for `env.files` or a testlane path without routing it
  through the check fails the enumerating test." No task delivers that. t-9
  checks declaration↔walker symmetry in both directions — it does not notice a
  new reader. t-10's scan fires only on `.String()` inside an `os.*` argument,
  which a raw-string consumer of `env.files` never produces. The deferred entry
  "Raw-string reader scan: a brand-new consumer that loads a declared path field
  and calls os.* on the raw string, never touching the Contained type at all" IS
  this clause. c-4's residual sentence was reworded when that was split out on
  2026-09-07; c-6's identical promise was left standing.
  Suggestion: reword c-6's tail the way c-4's was — to what t-9 actually
  falsifies (an undeclared path-shaped field, or a stale declaration) — or add
  the task. As written, verify must either fail c-6 or wave it through.

- [antipattern/granularity] t-3's description is factually wrong about 6 of its
  8 call sites, and its file list is short by two packages. Only
  `writeRunReport` (security.go:229) and `writeQualityRunReport`
  (quality.go:166) call `os.WriteFile` in package cmd. The other six hand the
  path straight into another package as a `string`:
    security.go:49,156 → `security.Load(path string)`  (internal/security/findings.go:116)
    security.go:164    → `security.WriteScaffoldSpec(path string, ...)` (internal/security/scaffold.go:47)
    quality.go:43,126  → `quality.Load(path string)`   (internal/quality/findings.go:120)
    quality.go:134     → `quality.WriteScaffoldSpec(path string, ...)` (internal/quality/scaffold.go:49)
  So "Each site keeps the returned Contained and writes through
  pathfence.WriteFile rather than unwrapping it to a string" is unachievable
  without changing internal/security and internal/quality, neither of which is
  in t-3's `files`. Four of the eight sites are reads, not writes, which the
  task title also mis-states. The executor will have to improvise the resolution
  mid-task — the exact decision that belongs at plan time.
  Suggestion: state the split explicitly — two seam sites, six that unwrap with
  `.String()` at a package boundary — and say why that unwrap is legal under
  t-10's scan (internal/security and internal/quality are outside the scanned
  six, and a non-`os.*` call is not the banned shape anyway). Either that, or add
  the two packages to `files` and retype their signatures.

- [antipattern] t-8's file list omits every caller of the two symbols it
  retypes. Changing `redProofDocSHA(repoDir, doc string)` to take a Contained
  breaks six call sites; four are not in the task's `files`:
    internal/cmd/redproof_lifecycle_test.go:128
    internal/cmd/redproof_repoint_cmd_test.go:172, :270
    internal/cmd/redproof_set_test.go:175  ← declared by t-4, same wave
  Retyping `redProofPin.Doc` additionally reaches
  internal/cmd/redproof_repoint_cmd.go:46 and
  internal/cmd/redproof_lifecycle.go:39. The description's "FOUR EXISTING
  CONSUMERS ... accounted for here rather than discovered during execution"
  enumerates consumers of the Doc *value* and misses the function's *callers*
  entirely. redproof_set_test.go being t-4's declared file is a same-wave
  collision, not just an omission.
  Suggestion: add redproof_repoint_cmd.go, redproof_lifecycle.go,
  redproof_lifecycle_test.go and redproof_repoint_cmd_test.go to t-8; move
  redproof_set_test.go's ownership, or note in both tasks which one edits which
  lines.

- [test-contract] t-7's Windows drive-letter assertion cannot pass on the
  platform the suite runs on. The contract says `"C:\\x"` and `"C:/x"` "both
  land in the escaped bucket, asserted directly rather than via a runtime.GOOS
  guard". Verified on darwin: `filepath.IsAbs("C:\\x")` is **false**,
  `strings.HasPrefix("C:\\x", "/")` is false, `path.Clean` leaves it as one
  segment — so testlane classifies it **in-tree today**, and still will after the
  delegation, whether or not the `filepath.IsAbs` disjunct is kept. Keeping that
  disjunct changes nothing off Windows; the description's reasoning for keeping
  it is sound as a Windows argument but does not make the test green here.
  CI is ubuntu-latest only (.github/workflows/ci.yml:14,73,121), so there is no
  runner where this assertion holds.
  Suggestion: either drop the assertion and keep the `filepath.IsAbs` disjunct
  with a comment citing the Windows reason, or assert the delegated helper
  directly (e.g. that testlane's absolute test is still a disjunction including
  `filepath.IsAbs`) rather than asserting a bucket outcome that is
  platform-conditional. "Asserted directly rather than via a runtime.GOOS guard"
  is the part that is impossible.

## FLAG

- [granularity] t-5 touches 6 files across internal/verify and internal/cmd and
  carries three distinct jobs (the gate, the conversion, the phaseScope
  signature change + 10 call sites). The description argues gate and conversion
  must be co-designed, which is right — but the phaseScope re-wiring is
  mechanical and separable.
  Suggestion: consider splitting the phaseScope `(*Scope, error)` re-wiring out;
  if not, leave it and accept the size deliberately rather than by default.

- [granularity] t-10 has two halves that share nothing. Half one is reflection
  over function/struct values in package cmd; half two is two go/ast scans over
  six packages with five testdata fixtures. Neither needs the other. Half one
  depends only on t-2, t-5 and t-8; half two on t-3, t-4, t-7.
  Suggestion: split. The carrier assertion could then sit in wave 2 alongside
  its dependencies rather than waiting behind t-9.

- [granularity] t-1 is two deliverables in two files: the pathfence package
  (Contain / InTree / Segment / the I/O seam / Contained) and the
  `assertDoesNotCompile` harness, which is generic test infrastructure three
  tasks consume. 16 test-contract lines for one task is a signal.
  Suggestion: consider lifting the harness into its own wave-1 task; t-5 and t-8
  depend on it as much as t-1 does, and today they inherit it through a
  dependency declared for a different reason.

- [antipattern] t-2's justification for phase.Task.Files being NotConsumed is
  inaccurate under t-6. It says "a validator constructs no Contained" — but t-6
  has ValidatePlan call `pathfence.Contain` on every task file, which does
  construct one (and discards it). The *disposition* is right (nothing opens the
  field); only the reason given is wrong, and it is wrong in a way a reader
  reconciling t-2 against t-6 will trip over.
  Suggestion: reword to "constructs a Contained and discards it — no reader
  carries one", which is both true and still distinguishes it from Consumed.

- [test-contract] c-6's guarantee rests on a hand-maintained tag list. t-9's
  walker matches eleven tag names (files, doc, path, source, tests, e2e,
  migrations, schemas, i18n, public, match). A future schema field tagged
  `toml:"dir"`, `toml:"report"` or `toml:"workdir"` is path-shaped, invisible to
  the walker, and therefore silently undeclared — with nothing red. This is the
  same "hand-maintained list" shape c-4 was rewritten to escape, relocated from
  the consumer list to the tag list. The plan never acknowledges it.
  Suggestion: record the limit in the walker's doc comment the way t-1 records
  the symlink limit, so the residual risk is declared rather than implied.

- [test-contract] t-5 does not pin NormalizePath's empty-path arm. The
  delegation risk is called out carefully for testlane's empty arm (t-7) and for
  `Segment("")` (t-1), but NormalizePath's own `p == ""` → `("", false)`
  (scope.go:329-331) sits *before* the "..'' literal being replaced, and
  `pathfence.InTree("")` returns `("", true)`. The asymmetry with t-7's
  treatment of the identical hazard is what makes this worth naming.
  Suggestion: add the empty case to t-5's NormalizePath contract line alongside
  the dot case that is already there.

- [test-contract] t-5 asserts `ok=false` for NormalizePath's rejections but not
  the `""` it returns on that branch. t-7 explicitly pins the *cleaned* string
  on testlane's false branch because the bucket consumes it — NormalizePath's
  four in-package call sites (scope.go:112,133,192,206) check only `ok`, so the
  gap is smaller, but the two tasks handle the same delegation shape to
  different standards.
  Suggestion: assert the `""` return, or say in the description that only `ok`
  is load-bearing here and why.

- [antipattern] t-3 covers c-3 but asserts no escape. The description says so
  honestly ("the refusal itself belongs to t-1"), which is the right call given
  all eight sites pass string literals — but it means c-3's only falsifiable
  content lives in t-1 and t-8, and t-3's `covers = ["c-3"]` reads as more than
  it delivers.
  Suggestion: no change to the tests; consider dropping c-3 from t-3's covers so
  the coverage map is not inflated.

## NOTE

- [strengths] The pathfence.Fields enumeration is complete and independently
  verified. A sweep of every non-test struct field in internal/project,
  internal/phase, internal/changes and internal/verify whose tag's first
  component is in the marker set returns exactly 18 hits: the 14 t-2 names by
  hand, the 3 declared-misleading ones (verify.Scope.Source scope.go:47,
  verify.LanguageRun.Files verify.go:196, verify.CriterionResult.Tests
  verify.go:487), and project.Remote.Public (project.go:231, `bool`) which the
  string/[]string type test drops. Nothing is missing and nothing is invented.
  The vacuity floor of 10 is comfortably under the real 17.

- [strengths] The backslash-fold contract line is a genuine mutation-killer, and
  the reasoning behind it is empirically correct. Ran it on darwin:
  `path.Clean(filepath.ToSlash("a\\b/../c"))` = `"c"`;
  `path.Clean(strings.ReplaceAll("a\\b/../c", "\\", "/"))` = `"a/c"`. The test
  `InTree("a\\b/../c") == ("a/c", true)` therefore fails a ToSlash
  implementation on the machine the suite actually runs on, which is exactly
  what a platform-gated rule usually fails to do.

- [strengths] t-5's gate/conversion split names the specific false-green it
  exists to prevent (feeding mutationCandidates ValidateRecorded's recorded set
  instead of scope.Files) and then writes the one test only that mistake fails.
  The Degraded-lane assertion — "a fix that leaves the gate out and relies on
  the existing rejection lands green here and must not" — is the sharpest line
  in the plan: it pins the *current* soft behaviour as a failure rather than
  asserting the new behaviour in isolation.

- [strengths] t-10's testdata-fixture discipline, and the sentence justifying it
  ("a scan calibrated on its own post-fix output proves only that it was written
  after the fix"), is the correct answer to the standing hazard with guard
  tests. The two must-not-trip fixtures for the legal `..` shapes are real:
  verified that the only non-test `".."` literals in the tree are the four
  rev-range concatenations (topology.go:80, basebranch.go:69, doctor.go:980,
  milestone.go:1035), refguard.go:56's `strings.Contains`, and the five path
  checks this phase removes (security.go:194, redproof_set.go:158,
  scope.go:349, match.go:141, remote.go:329). The shape rule separates them.

- [wave-order] Wave assignment is sound throughout. t-9→t-2 and t-10→{t-3..t-9}
  are strict dependencies, and t-2's note on why it carries no depends_on —
  deriveWave (plan_edit.go:54-70) would push it to wave 2 and cascade t-9 to 3
  and t-10 to 4 for a sequencing constraint /dross-execute already enforces
  serially — is correct against the code and worth keeping.

- [file-references] Every line reference in the plan checks out against the tree
  except one: `filepath.ToSlash(pin.Doc)` is at redproof_repoint.go:**121**, not
  :117. All others verified — security.go:188/194 and its eight call sites,
  quality.go's four, redproof_set.go:149, scope.go:33/167/323-325/328/349,
  verifyscope.go:24 with exactly 8 test call sites, verify.go:82/84-89/90/587/588
  and mutationCandidates at :1189, plan_edit.go:54-70/82/287, task.go:438,
  match.go:134/137/141, remote.go:329, redproof.go:103-107/146,
  redproof_repoint.go:41-53/142/143/147/151/155/164 and the %s sites at
  :146/:150/:156, doctor.go:604-608, hostile_config_test.go:435,
  mutation_remote_wiring_test.go:47, security_test.go:281, quality_test.go:160,
  enum_divergence_test.go:385, and `module mutantproof` at
  internal/mutation/ceiling_test.go:290 (cited as :287).

- [forbidden-actions] No rule violation. Project rules.toml carries one rule
  (r-01, `make install` staleness); no global ~/.claude/dross/rules.toml exists.
  runtime.mode is native with `go test -count=1 ./...`, and no task implies a
  containerised or alternate runner. t-1's `!testing.Short()` gate is consistent
  with that test_command passing no `-short`.

- [locked-decisions] No task contradicts a locked decision. artifact_scope,
  escape_failure_mode, absolute_paths and symlink_resolution are each not only
  honoured but pinned by a named test — the symlink one by a test named for the
  lock, which is the right way to keep a documented limit from decaying into an
  assumed one.

## Summary

Strong plan with unusually good adversarial instincts in its test contracts, but
it ships four defects that will surface during execution: c-6 still promises the
raw-string reader scan that was deferred to secret-detection, t-3's description
is wrong about six of its eight call sites and short two packages, t-8's file
list misses every caller of the function it retypes, and t-7 asserts a
Windows-only outcome as a platform-unconditional test on an ubuntu-only CI.
