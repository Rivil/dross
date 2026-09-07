# Plan Review — tracked-path-containment

Reviewed: 2026-09-07 (third review, after type-level redesign)
Plan: 10 tasks across 3 waves

## Does the redesign deliver c-4

No. c-4 is **overclaimed**, and this time the plan says so itself.

c-4 sentence 2: "A residual test covers the gap types cannot — a reader of a
declared path field that reaches os.* with a raw string fails the suite."

t-10 description, verbatim: "What this does NOT catch, stated so c-4 is not read
as wider: a brand-new consumer that loads a path field and calls os.ReadFile on
the raw string, never touching Contained at all."

Those are the same sentence with opposite verbs. The criterion asserts the
residual test covers exactly the case the task that owns the residual test
declares out of scope. Nothing else in the plan covers it: t-9 checks only that
a path-shaped field is *declared*, not who reads it; t-2's type assertion is
about the routing function's return type, not about who else touches the stored
string. So under the current plan, adding `os.ReadFile(c.RedProof.Doc)` to a new
file in internal/cmd compiles, runs, and leaves the suite green. That is
literally the bug this phase exists to close, one consumer over.

Sentence 1 ("every site that opens a tracked-artifact path accepts a value only
the shared check can construct, so a consumer that skips the check fails to
build") is delivered for the **adopted** sites and conventional everywhere else.
Where it is genuinely true: pathfence.ReadFile/WriteFile/Stat, mutationCandidates
once its parameter is []Contained, redProofDocSHA once it takes a Contained, and
redProofPin.Doc / redProofRepointPlan.Doc once they are typed — a caller holding
only a string has no value to pass and the compiler stops it. Where it is merely
conventional: the schema fields stay `string` / `[]string` by deliberate design
(t-2, because Contained cannot marshal), so `changes.RedProof.Doc`,
`changes.TaskRecord.Files`, `verify.Scope.Files` and `phase.Task.Files` are all
still plain strings at the point a new consumer would pick them up. os.ReadFile
still takes a string. Nothing fails to build.

Is the residual scan sufficient to cover that gap? No — it is not aimed at it.
Scan (a) matches `os.*(... .String() ...)`, i.e. **unwrapping an existing
Contained**. A new consumer that never touches Contained produces no `.String()`
and is invisible to it. Scan (b) is a `".."` literal scan, unrelated.

Is scan (a) buildable from go/ast alone as claimed? The *syntactic* match is —
"os.<name> call with a `.String()` selector call somewhere in an argument" needs
no type information. But the plan describes it as "whose argument is or contains
**a Contained** unwrapped by .String()", and that qualifier is precisely what
ast alone cannot decide. What gets built is a type-blind `.String()` ban, which
over-reports (see FLAG). Fail-closed, so tolerable — but it is the same
overstatement shape the last two rounds died on, just with the error sign
flipped.

Verdict: c-4 as worded is not delivered. The type half is real and worth having;
the residual half named in the criterion is not built.

## Contained design soundness

**Unexported field.** Sound in the main case, with one hole the plan half-covers.
`pathfence.Contained{p: "x"}` from another package is a compile error, correct.
But `pathfence.Contained{}` — an empty composite literal — is **legal** outside
the package; Go only forbids *setting* unexported fields. So
`mutationCandidates(dir, []pathfence.Contained{{}, {}})` compiles today and under
this plan. t-1 line 2 catches it at runtime (the seam errors on a zero
Contained), which is the right mitigation, but it means "a caller that skips the
check has no value to pass and fails to build" is false for the zero value. t-1
line 1's fixture must therefore set the field (`Contained{p: "x"}`); an empty
literal fixture will not fail to build and the test will pass vacuously.

**Marshalling / copying.** Verified clean. t-2 line 9 explicitly pins the stored
fields as string/[]string and gives the reason. `verify.Scope.Files` is
`[]string` with tag `json:"files"` at internal/verify/scope.go:33 and the plan does
**not** type it Contained — confirmed against t-2 line 9 and t-5, which keeps
`NewScope`'s signature unchanged. Contained is copyable and comparable, so no
aliasing hazard.

**redProofPin / redProofRepointPlan not serialized.** Confirmed.
internal/cmd/redproof.go:103-107 and internal/cmd/redproof_repoint.go:41-53 carry
**no** struct tags, and `grep Marshal` over internal/cmd/redproof*.go and
doctor.go returns nothing. Typing their Doc field Contained is safe from
serialization. It is *not* safe from their other consumers — see FLAG.

**internal/verify staying I/O-free.** The claim is loose but the substance holds.
internal/verify is *not* I/O-free as a package: internal/verify/verify.go imports
os and path/filepath and calls os.WriteFile (:613), os.ReadFile (:619) and
os.Create (:638). What is I/O-free is the `Scope` **type** (scope.go:23-24: "The
type is pure: no git, no filesystem, no I/O") and scope.go itself, which imports
only `path`. Putting ValidateRecorded in scope.go keeps that property — Contain
is lexical and the seam is only called from internal/cmd — so the design intent
survives; the sentence describing it does not.

## Previous flags

- [t-6 wrong test file] CLOSED. t-6 now names internal/cmd/task_test.go, which
  does exercise `dross task add` (TestTaskAddTailAppend:225, TestTaskAddAfterAnchor:255,
  TestTaskAddRejectsUnknownCriterion:280, TestTaskEditFilesGoesThroughSaveIfValid:464).
- [t-1 slash form] PARTIALLY CLOSED — description fixed, arithmetic newly broken.
  The implementation is now unconditional `strings.ReplaceAll` + `path.Clean`,
  correctly matching internal/verify/scope.go:328 with the rationale at :323-325.
  But the expected value moved from `"a/c"` to `"a"`, and `"a"` is wrong. See FLAG.
- [t-3 undrivable contract lines] CLOSED. Line 3 now states outright that the
  escaping-name cases are not asserted and says why, and corrects containedPath's
  overstated doc comment at security.go:185-186. All eight call sites verified:
  security.go:45,152,160,204 and quality.go:39,122,130,150, each passing a string
  literal.
- [t-10 receiver-type resolution unbuildable from go/ast] CLOSED BY REMOVAL. The
  receiver-type rule is gone. Its replacement is type-blind rather than
  type-aware — a different overstatement, see FLAG.
- [t-10 line 5 unsatisfiable] CLOSED BY REMOVAL. The routing-function arm left
  t-10 entirely. It reappears in t-2 line 8, where it hits a harder wall — see BLOCKING.
- [BLOCKING: changes.RedProof.Doc has no coherent disposition] CLOSED IN SHAPE,
  REOPENED IN MECHANICS. The type-level design does give the field a disposition
  (Consumed, via discoverRedProofPins constructing a Contained), which is the
  right answer. The assertion t-2 uses to make that disposition a fact rather
  than a promise is not implementable — see BLOCKING.
- [t-5 placement contradiction: phaseScope vs the two readers] CLOSED. t-10 no
  longer asserts anything about where ValidateRecorded is called, so t-5 owns
  the phaseScope signature change alone and nothing contradicts it.

## BLOCKING

- [criterion not delivered] c-4's second sentence is not built by any task, and
  t-10 says so explicitly. Detail above. Either c-4's residual clause is scoped
  down to what t-10 actually builds (unwrapping an adopted Contained), or a task
  has to build the raw-string reader scan.

- [test contract undrivable] t-2 line 8 — "Every Consumed entry's routing
  function returns pathfence.Contained or []pathfence.Contained, asserted by
  reflecting on the named function's signature" — is unimplementable for the
  third Consumed entry, and false as stated even if it were.
    - `discoverRedProofPins` (internal/cmd/redproof.go:119) is **unexported**.
      internal/pathfence/fields_test.go cannot reference it at all — not by
      reflection, not by any means. Reflection needs a function *value*; a name
      string needs go/types, which is not "reflecting".
    - Worse, it is factually wrong: after t-8 that function still returns
      `([]redProofPin, error)`, not `Contained` or `[]Contained`. t-8 types the
      *field* `redProofPin.Doc`, not the return. The assertion fails on correct
      post-t-8 code.
    - Import direction compounds it: internal/cmd imports pathfence, so a
      `package pathfence` test importing internal/cmd is an import cycle. Only
      `package pathfence_test` is legal, and it still cannot see the unexported
      function.
  This assertion is what t-10 calls "the load-bearing half of c-4". It cannot be
  written as specified.

- [wave order making a task's own contract unpassable] t-2 is wave 1 with no
  `depends_on`, but its contract asserts the existence and signature of
  `verify.ValidateRecorded` (created in t-5, wave 2 — `grep -rn ValidateRecorded
  internal/` returns nothing today) and of a Contained-producing
  `discoverRedProofPins` (t-8, wave 2). t-2 line 7 says an entry "naming a
  function that does not exist fails here" — at the end of wave 1, both named
  functions are exactly that. t-2 cannot go green in its own wave.

- [silent scope narrowing] t-5's mutationCandidates plumbing drops the
  recorded/git union. Both call sites pass `scope.Files`, not the recorded set:
  internal/cmd/verify.go:90 `mutationCandidates(filepath.Dir(root), scope.Files)`
  and :588 `mutationCandidates(repoDir, scope.Files)`. `scope.Files` is the UNION
  of changes.json paths and git-diff paths — verify.go:84-89 spells out why ("A
  file git saw change but no task recorded would otherwise never be mutated at
  all"). t-5 says mutationCandidates takes "the []pathfence.Contained that
  ValidateRecorded returns instead of a []string", and ValidateRecorded validates
  only the *recorded* paths (t-5 line 4 deliberately keeps the git lane soft). An
  executor following that literally mutates the recorded set only and silently
  drops git-only files from mutation scope — the false-green shape the
  escape_failure_mode lock exists to prevent. No contract line in t-5 asserts the
  union survives. The plan needs to say where scope.Files becomes []Contained.

## FLAG

- [test contract arithmetic] t-1 line 12 is wrong. Under the implementation t-1
  now specifies — unconditional `strings.ReplaceAll(p, "\\", "/")` then
  `path.Clean` — ``InTree(`a\b/../c`)`` computes `a\b/../c` -> `a/b/../c` ->
  Clean -> **`a/c`**, not `"a"`. The `..` pops `b` leaving `a`, and then `c` is
  still appended. The line's own gloss ("'..' pops 'b' and leaves 'a'") forgets
  the trailing segment. Its contrast case is right: a filepath.ToSlash
  implementation yields `"c"` on darwin, so `"a/c"` does discriminate the two
  implementations. Expected value must be `("a/c", true)`.

- [antipattern: undeclared collateral] t-8 types `redProofPin.Doc` and
  `redProofRepointPlan.Doc` as Contained without accounting for the four existing
  consumers that need the repo-relative *string*, and Contained holds an OS-form
  **joined absolute** path per t-1:
    - internal/cmd/hostile_config_test.go:435 `if p.Doc == want` — Contained ==
      string does not compile, and hostile_config_test.go is not in t-8's files.
    - internal/cmd/redproof_repoint.go:117 `filepath.ToSlash(pin.Doc)` populates
      `redProofRepointPlan.Files`, documented at :47-49 as repo-relative. A
      Contained there is either a compile error or an absolute path in a
      repo-relative list.
    - internal/cmd/redproof_repoint.go:151 `redProofRewriteDoc(p.Doc, ...)` takes
      `doc string` (redproof_doc.go:34).
    - The error messages at redproof_repoint.go:146/:150/:156 format `p.Doc` with
      `%s`; via String() they would start printing absolute machine paths.
  Each is fixable, but the plan asserts a field-type change and names none of them.

- [antipattern: unverifiable claim] t-10 scan (a) cannot identify "a Contained"
  from go/parser + go/ast. What is buildable is "any os.* call with a `.String()`
  in an argument", which is type-blind. Live false positive today:
  internal/cmd/mutation_remote_wiring_test.go:47
  `os.WriteFile(filepath.Join(root, LocalFile), []byte(b.String()), 0o644)` — a
  strings.Builder. The plan never says whether the scan walks `_test.go` files;
  if it does, the scan is red on arrival. (The two non-test hits, security.go:229
  and quality.go:166, are the same Builder shape and are the sites t-3 converts,
  so the post-fix non-test tree is clean — which is exactly the calibrated-on-its-
  own-output rot t-10 line 5 otherwise guards against.)

- [test contract: no stated mechanism] Three contract lines assert a fixture
  "fails to compile" / "fails to build" (t-1 line 1, t-5 line 7, t-8 line 6). Go
  has no in-language way to do that: it needs `exec.Command("go", "build", ...)`
  over a testdata fixture. No task lists a testdata file, and the repo has no
  precedent — internal/cmd/execconsent_audit_test.go only *greps for the string*
  `exec.Command("go", "build"` (:361), it does not run one. Name the mechanism or
  the assertions get quietly downgraded to "the code compiles, so it must be fine".

- [test contract] t-2 line 7 declares `verify.Scope.Files` Consumed "naming the
  constructor that produces their Contained value ... verify.ValidateRecorded".
  ValidateRecorded does not produce Scope.Files; `verify.NewScope` does, from the
  recorded+git union (verify.go:82, :587). The declaration is describing
  changes.TaskRecord.Files twice. Same root cause as the union BLOCKING above.

- [test contract] t-3 line 4 ("a run dir that does not exist still produces a
  refusal from the seam rather than a partial write") asserts pre-existing os
  behaviour: os.WriteFile into a missing directory already fails today. It drives
  nothing this phase changes. Not wrong, just not evidence.

- [antipattern] t-9's walker must strip tag options: every field it has to find
  in internal/project is tagged with `,omitempty`
  (project.go:274-280 `toml:"source,omitempty"` etc., project.go:284
  `toml:"files,omitempty"`). A naive exact-match on the tag value finds none of
  them and the vacuity guard ("at least 10 path-shaped fields") is the only thing
  that would catch it. Worth stating in the description.

- [wave order] t-10 still declares `depends_on = ["t-9"]` and consumes nothing
  t-9 produces. Since the redesign, t-10 also no longer reads `pathfence.Fields`
  at all — neither scan (a) nor scan (b) references the registry — so its
  dependency on t-2's output is gone too. Costs no parallelism (t-3..t-8 pin it
  to wave 3), but the stated dependency is now fiction.

## NOTE

- [locks] No task contradicts a locked decision. artifact_scope is served by t-2's
  registry plus t-9's walker in both directions. escape_failure_mode is pinned as
  a must-fail variation (t-5 line 3, the `gone` lane). absolute_paths is pinned at
  t-1, t-4 and t-6; testlane's absolute-is-out-of-tree bucketing (t-7) is InTree
  classification, not the shared check, so no conflict. symlink_resolution is
  pinned by an accepted-symlink test named for the lock (t-1 line 11).

- [coverage] Complete. c-1 (t-1,t-3,t-4,t-7,t-10), c-2 (t-5,t-6,t-8), c-3
  (t-1,t-3,t-4,t-8), c-4 (t-10), c-5 (t-1,t-5,t-6,t-8), c-6 (t-2,t-9). Every
  criterion is claimed by at least one task; c-4's problem is delivery, not
  coverage.

- [granularity] t-5 and t-8 are 6 files each. t-5 spans three layers
  (internal/verify, internal/cmd command wiring, internal/cmd verify core); t-8
  spans one package and three commands. Both were flagged before; recording, not
  re-litigating. t-8 will grow by at least one more file (hostile_config_test.go).

- [strength] t-10's "What this does NOT catch" paragraph is the most useful
  writing in the plan. It is how I could pin the c-4 gap in one read instead of
  three. The engineering judgement is right and the criterion text is what is
  wrong — c-4 should be reworded down to it, not the paragraph deleted to match c-4.

- [strength] t-2 line 9 — asserting the stored fields must REMAIN string/[]string
  — is the correct guard for the one thing this redesign makes tempting. It
  blocks the obvious wrong fix (type changes.RedProof.Doc as Contained, break
  changes.json round-tripping silently) at the declaration rather than at a
  user's next verify. Verified sound: the pins that DO get typed carry no tags
  and are never marshalled, while every field that stays a string is one that is.

- [strength] Moving the "it does what it says" burden from an AST scan onto the
  type checker is the right call on the merits, independent of c-4's wording. The
  red-proof doc readers were unreachable by any ast-only dataflow scan, and a
  Contained field on redProofPin reaches them without one.

## Summary
The mechanism change is the right one and it closes the previous blocking finding
at its root: the field an AST scan could not follow is now carried by a type, and
the dead receiver-type requirement is gone. Four things stop it landing. c-4's
second sentence promises exactly the scan t-10 declares out of scope, so the
criterion is overclaimed against its own plan. t-2 line 8 — the assertion the
plan calls the load-bearing half of c-4 — cannot be written: discoverRedProofPins
is unexported in a package pathfence's tests cannot import from, and after t-8 it
still returns []redProofPin, not []Contained. t-2 sits in wave 1 asserting
signatures wave 2 creates. And t-5's plumbing, read literally, feeds
mutationCandidates the recorded set instead of the recorded+git union both call
sites pass today, silently narrowing mutation scope. The slash-form flag came
back in a new form — the expected value is now `"a"` where the stated
implementation yields `"a/c"` — and t-8's field retyping does not account for the
four consumers that need the repo-relative string.
