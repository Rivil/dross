# Plan Review — tracked-path-containment

Reviewed: 2026-09-07
Plan: 11 tasks across 4 waves

## BLOCKING

- [antipatterns] t-3 retypes `Load`, `Save` and `WriteScaffoldSpec` in both scan
  packages, but its file list misses three non-test callers, each of which
  breaks at compile:
    - `internal/security/lifecycle.go:60` — `Load(filepath.Join(runDir, "findings.toml"))`
      inside `ResolveItem`
    - `internal/quality/lifecycle.go:60` — the same call
    - `internal/cmd/quality_findings_test.go:71` — `quality.Save(filepath.Join(runDir, "findings.toml"), led)`,
      a file in neither t-3's nor t-11's list
  The caller derivation that was redone for t-3 in an earlier round stopped at
  `findings.go`/`scaffold.go` and their own tests; it did not sweep the packages'
  other files or the two `*_findings_test.go` files in `internal/cmd`.
  Suggestion: add `internal/security/lifecycle.go`, `internal/quality/lifecycle.go`
  and `internal/cmd/quality_findings_test.go` to t-3's file list. `lifecycle.go`
  builds its path from `runDir` with `filepath.Join`, so each call site becomes a
  `pathfence.Contain(runDir, "run directory", "findings.toml")` — worth a contract
  line, since `ResolveItem` is a live read path this phase would otherwise leave
  outside the type.

- [test-contract] t-3's `WriteScaffoldSpec` cannot do its I/O through the
  pathfence seam, so the contract line "WriteScaffoldSpec still writes a spec a
  subsequent phase scaffold can read in both packages" cannot pass as designed.
  Both bodies end in `return spec.Save(path)` (`internal/security/scaffold.go:52`,
  `internal/quality/scaffold.go:54`), where `spec` is a `*phase.Spec` and
  `phase.Spec.Save` (`internal/phase/phase.go:459` → `saveTOML:476`, which does
  `os.MkdirAll` + `os.Create(path+".tmp")`) takes a plain string. t-1 makes
  `Contained.String()` the ROOT-relative form and puts the joined absolute path
  out of reach on purpose, so `spec.Save(c.String())` writes `./spec.toml`
  relative to the process working directory — the wrong file, silently. The three
  ways out all cost a decision no task owns: retype or split `phase.Spec.Save`
  (nothing in this plan touches `internal/phase/phase.go`), add an absolute
  accessor (exactly what t-10's scan exists to ban), or duplicate `saveTOML`'s
  atomic-tmp-write in both scan packages and drop the "matches the repo's
  spec.toml formatting" guarantee the doc comment at `scaffold.go:44-46` claims.
  Suggestion: decide this at plan time, not mid-task. The cheapest shape that
  keeps every guarantee is a marshal/writer seam in `internal/phase` — e.g.
  `func (s *Spec) Encode() ([]byte, error)` — with `WriteScaffoldSpec` doing
  `pathfence.WriteFile(c, b, 0o644)`; that needs `internal/phase/phase.go` and its
  test added to t-3's file list, and t-3's `depends_on` is unaffected.

## FLAG

- [antipatterns] t-8's claim that "FOUR EXISTING CONSUMERS ... are accounted for
  here rather than discovered during execution" is not exhaustive. Two more:
    - `internal/cmd/redproof_set_test.go:173` — `if found.Doc != "fixtures/hostile-config-c5/RUN.md"`,
      the identical string-comparison shape as the enumerated
      `hostile_config_test.go:435`. The file is in the list, so it compiles-breaks
      visibly, but the enumeration that justified the list missed it.
    - `internal/cmd/redproof_repoint_cmd.go:99` — `Printf("...", pin.Phase, plan.Doc, ...)`.
      Benign (`%s` on a `fmt.Stringer`), but the file is in no task's list at all
      even though it holds a live reference to a retyped field.
  Suggestion: add `redproof_repoint_cmd.go` to t-8's file list and extend the
  enumeration to six.

- [test-contract] t-8 breaks `TestDiscoverRedProofPinsFindsRecordedPin`
  (`internal/cmd/redproof_set_test.go:149`) in a way the plan does not anticipate,
  and the repair silently weakens the test. It discovers pins from a THROWAWAY
  root (`liveRecordRoot(t, repo, "config-trust-hardening")`) and then reads the doc
  against the REAL repo (`redProofDocSHA(repoRootForDocs(t), found.Doc)`), because
  `fixtures/hostile-config-c5/RUN.md` exists only under the real tree. Once
  `discoverRedProofPins` Contains against `filepath.Dir(root)` and
  `redProofDocSHA` takes only a `Contained`, the caller can no longer supply a
  different root: the seam reads at the throwaway path and the test fails. It is
  repairable inside an owned file (rebuild the `Contained` with
  `pathfence.Contain(repoRootForDocs(t), ...)`), but that stops exercising a
  discovery-produced value, which is the whole point of a test whose own comment
  says it "reads THIS repo, not a fixture".
  Suggestion: name this in t-8's description with the intended repair, and add a
  contract line pinning that the test still asserts on the pin discovery
  produced.

- [test-contract] t-10's literal scan states two different exemption rationales
  and leaves the adjacency window undefined. The rule is written as "a `..`
  COMPARISON in a path context (adjacent to filepath.Separator, os.PathSeparator,
  "../", path.Clean, path.Base, filepath.Rel or filepath.IsAbs)", but the
  rev-list sites are then exempted BY SHAPE (concatenation, not comparison) while
  refguard's is exempted by "no path call nearby". Only one live site has a path
  marker in the same function — `doctor.go:980`'s `"origin/"+mainBranch+".."+mainBranch`
  sits 18 lines after `filepath.Join` at `doctor.go:962`, both inside
  `leakedPhaseCommits`. If the executor implements adjacency as the primary rule
  rather than shape, t-10 lands red against the live tree on arrival.
  Suggestion: state the rule as shape-first (concatenation operands are never in
  the comparison class), and pin the window explicitly — same statement, or N
  lines — so the fixture set and the live tree agree.

- [wave-order] Two contract lines assert against work owned by a LATER wave, so
  neither can be verified at its own task gate: t-3 line 2 ("t-10's scan over both
  packages reports no direct os.* call ... asserted by a must-trip testdata
  fixture") is wave 2 against wave 4, and t-8 line 9 ("asserted by t-10's scan
  over the post-fix file plus a must-trip testdata fixture") is wave 3 against
  wave 4. t-11 line 4 has the same forward reference but says "asserted there",
  which reads correctly as delegation.
  Suggestion: reword t-3 line 2 and t-8 line 9 to the t-11 form, so the executor
  does not write a second local scan to satisfy them.

- [granularity] Four tasks are at or over the 5-file mark: t-8 (10 files, 4 of
  them non-test source across `redproof*`/`doctor`), t-3 (8 files, two packages),
  t-10 (7 files, though 5 are testdata), t-5 (6 files spanning `internal/verify`
  and `internal/cmd`). t-8 is the one that reads genuinely oversized — it retypes
  two struct fields, moves five os calls to the seam, changes a doctor arm, and
  carries an accepted behavioural regression.
  Suggestion: no action required for t-3/t-5/t-10. Consider splitting t-8 into the
  retype-plus-discovery half and the `applyRedProofRepoint` seam half; they are
  separable and the second is the task's stated centre of gravity.

- [granularity] t-1 builds two unrelated packages. `internal/compilefence` has no
  dependency on `internal/pathfence` and is consumed by five contract lines across
  t-3, t-5, t-8, t-10 and t-11. Bundling them means the compile fence cannot land
  or be reviewed on its own, and t-1 already carries 17 contract lines.
  Suggestion: optional — a wave-1 `internal/compilefence` task with t-1 depending
  on nothing changes no wave numbers (both stay wave 1) and makes the two
  self-tests of `AssertDoesNotCompile` a gate of their own.

- [antipatterns] t-1's I/O seam is `ReadFile`/`WriteFile`/`Stat` only, with no
  `Create`/`OpenFile`, and t-3 needs one. `Save` currently streams:
  `f, err := os.Create(path)` then `toml.NewEncoder(f).Encode(l)`
  (`internal/security/findings.go:104-110`, `internal/quality/findings.go:108-114`),
  and `Load` uses `toml.DecodeFile(path, &l)` — a third-party function that also
  takes a string. Both must be rewritten to buffer (encode to a `bytes.Buffer`
  then `pathfence.WriteFile`; `pathfence.ReadFile` then `toml.Decode`), which
  changes the written file's permissions from `os.Create`'s `0666 &^ umask` to
  whatever `fs.FileMode` the call passes. None of this is stated.
  Suggestion: say in t-3 that the encode buffers, and name the mode explicitly
  (`0o644` matches the repo's other writers) so it is a decision rather than a
  side effect.

- [test-contract] t-4's four contract lines call `checkRedProofDoc("/abs/x.md")`,
  `checkRedProofDoc("../x.md")` and so on with one argument; the function is
  `checkRedProofDoc(repoDir, doc string)` (`internal/cmd/redproof_set.go:149`) and
  the task does not change its arity.
  Suggestion: shorthand is fine in prose, but the root matters here — the
  refusals must fire against a real `repoDir` for the is-a-directory and
  does-not-exist arms to be distinguishable. Restate with both arguments.

## NOTE

- [strengths] The path-shaped field enumeration is exactly right. An independent
  `go/ast` walk over `internal/project`, `internal/phase`, `internal/changes` and
  `internal/verify`, using the plan's own tag set and its string/[]string type
  test, returns exactly the 16 fields t-2 names — no more, no fewer — and
  `project.Remote.Public` (a bool at `project.go:231`) is correctly dropped by the
  type test without a carve-out. t-9's "at least 10" vacuity guard holds with
  margin, and the three declared false positives (`verify.Scope.Source`,
  `verify.LanguageRun.Files`, `verify.CriterionResult.Tests`) are precisely the
  three the walker reaches.

- [strengths] The compile-fence mechanics reproduce exactly as documented. A temp
  module `github.com/Rivil/dross/tmpfence` with `require`+`replace` and `-mod=mod`
  builds clean against `internal/argfence` (exit 0); the identical tree with
  `module mutantproof` fails with `use of internal package
  github.com/Rivil/dross/internal/argfence not allowed`. And
  `pf.Contained{rel: "x"}` from another package fails with `cannot refer to
  unexported field rel in struct literal of type pf.Contained` — the exact
  substring t-1's contract pins — while `pf.Contained{}` compiles, which is why
  the seam's zero-value refusal has to be its own contract line. Both are stated
  correctly and both matter.

- [strengths] t-5's split of the gate from the conversion is the right shape, and
  the "THE UNION SURVIVES" contract line is a real falsifier: a `containScope`
  fed `ValidateRecorded`'s recorded set passes every other line in the task and
  fails only that one. The rationale is grounded in the live comment at
  `verify.go:84-89`.

- [coverage] Every criterion appears in at least one task's `covers`, and no
  criterion promises something no task builds. c-4's residual clause and c-6's
  declaration clause both now map onto concrete work (t-10 halves one and two,
  t-2 plus t-9).

- [wave-order] Wave order is legal throughout. Every dependent's wave is strictly
  greater than its deepest dependency's, satisfying `deriveWave`
  (`internal/phase/plan_edit.go:54-70`) and the move guard (`:287`). No two
  same-wave tasks share a file; t-1 and t-2 share the `internal/pathfence`
  directory but not a file, and the plan already documents why forcing a
  `depends_on` there would cost two wave levels for nothing.

- [antipatterns] The literal-scan exemption set is complete. A repo-wide grep for
  `".."` in non-test Go under `internal/` returns exactly ten sites: the five this
  phase removes (`redproof_set.go:158`, `security.go:194`, `scope.go:349`,
  `match.go:141`, `remote.go:329`) and the five t-10 exempts
  (`topology.go:80`, `basebranch.go:69`, `doctor.go:980`, `milestone.go:1035`,
  `refguard.go:56`). Nothing else would trip the scan.

- [test-contract] `path.Clean("")` returns `"."`, so t-1's `InTree("") == ("", true)`
  needs an explicit empty check before cleaning. Satisfiable, not free.

- [test-contract] t-9's line "the option-carrying form that EVERY real
  internal/project field uses" overstates: `project.TestLane.Match`
  (`project.go:101`) is tagged `toml:"match"` with no option. The contract line is
  still worth keeping — every `Paths` field and `Env.Files` do carry `,omitempty`
  — only the "EVERY" is wrong.

- [test-contract] The slash-form contract line discriminates exactly as claimed:
  `path.Clean(strings.ReplaceAll(`a\b/../c`, `\`, "/"))` is `"a/c"` while
  `path.Clean(filepath.ToSlash(`a\b/../c`))` is `"c"` on darwin. Confirmed by
  running it.

- [antipatterns] Line-number drift, cosmetic only: `filepath.ToSlash(pin.Doc)` is
  at `redproof_repoint.go:121`, not `:117`; the two read-error messages are at
  `:145`/`:149`, not `:146`/`:150`; the discovery-error message arm is at
  `doctor.go:569`, not `:568`; and `module mutantproof` is at
  `internal/mutation/ceiling_test.go:290`, not `ceiling_test.go:287` — the plan
  never names that file's package. Every other cited line landed exactly:
  `security.go:188`/`:194`, `redproof_set.go:149`, `scope.go:328`/`:349`,
  `verifyscope.go:24` with all 8 test call sites, `verify.go:82`/`:90`/`:587`/
  `:588`/`:1189`, `plan_edit.go:82`, `task.go:439`, `match.go:134`/`:137`/`:141`,
  `remote.go:329`, `redproof.go:103-107`/`:134`, `doctor.go:48`/`:566-570`/`:587`/
  `:604`/`:608`, `redproof_repoint.go:41-53`/`:47-49`/`:142`/`:143`/`:147`/`:155`/
  `:156`/`:164`, `project.go:274-280`/`:284`, and
  `mutation_remote_wiring_test.go:47`.

- [forbidden-actions] No violations. `.dross/rules.toml` carries only r-01
  (run `make install` after editing prompts or Go code, which applies at execute
  time), and there is no `~/.claude/dross/rules.toml`. Separately: `verify.go`'s
  comment at `:1183` cites "rule r-02", which `rules.toml` does not define — a
  pre-existing repo inconsistency, not this plan's.

## Summary

The plan is well-grounded — its field enumeration is exactly right and its
compile-fence mechanics reproduce verbatim — but t-3 is not executable as
written: its caller set misses three non-test call sites, and `WriteScaffoldSpec`
cannot reach the seam because it delegates to `phase.Spec.Save`, which takes a
string and which no task owns.
