# Panel synthesis — tracked-path-containment

Three drafts judged cold. Merged plan: 8 tasks across 3 waves, 7 disagreements.

## Scores

| Dimension | risk (6/3) | mvp (4/3) | verification (8/3) |
|---|---|---|---|
| **Criteria coverage** | 6/6 claimed and real, single-owner per criterion — but c-1 is only satisfied against a two-site reading; its own t-5 literal scan ("whitelisting only internal/pathfence") would go red on scope.go, match.go and remote.go the day it lands. | 6/6 claimed, **c-3 is short**: no task touches the red-proof repoint write, so a tracked-artifact path still reaches `os.WriteFile` outside the repo. Same self-contradiction as risk in t-4's literal scan. | 6/6 claimed and the only draft whose coverage survives contact with the source: five `..` sites all owned (t-3, t-5), both red-proof I/O paths owned (t-6), and the literal scan given a shape rule instead of an exemption list. |
| **Test-contract specificity** | Strongest single contracts in the panel. Near-miss corpus (`..foo`, `a/..b`, `a/b..`) kills the missing-separator bug; typed `ErrEscapes`/`ErrAbsolute` make the IsAbs branch independently falsifiable; mtime+existence on the outside path falsifies check-after-write with no new seam. | Adequate and honest ("asserted on all three substrings, not just on err != nil"), but thinner: no near-miss corpus, no ordering falsifier beyond "seed ../x so a stat-then-dispatch would pass". | Broadest and most lock-aware: pins the symlink limit with a test that *accepts* a symlink escape, asserts the git `Degraded` lane is unchanged so the hard rule is not over-applied, hashes the outside file, and tests the shape rule itself with must-not-trip fixtures. Weaker than risk on sentinel-vs-message. |
| **Granularity** | Well-cut. Splitting the two enumerating guards (join sites / schema fields) is right — one file owning three criteria is the shape where a partial build still looks done. t-2 correctly carries quality.go. | Under-cut. t-3 spans three packages and two artifacts; t-4 carries c-1, c-4 and c-6 in one file — the exact concentration risk warns about. Four tasks is not a smaller phase, it is the same phase with fewer commit boundaries. | Slightly over-cut but defensibly: t-5 (2 files) and t-2 (registry only) are small, and each is a separate falsifier. No task owns more than two criteria. |
| **Wave correctness** | One real error: the field registry is *created* in wave 3 (t-6), so the declaration that everything else is judged against lands last. t-5/t-6 depending on all of wave 2 is otherwise right. | Wave 2 is two tasks and wave 3 is one; the dependency edges are correct but the graph carries no parallelism it could have had. | Best. Registry declared in wave 1, field-existence guard in wave 2 (it scans struct tags no rewire touches, so holding it back serialises for nothing), only the join-site scan behind the rewires. |

**Skeleton: `verification`.** It is the only draft whose plan is consistent with the source as it actually is — five `..` sites, not two, and an unguarded red-proof doc write. The other two both specify a literal scan that their own task list would fail. Its wave split is also the only one that does not idle the registry.

## Merged plan

8 tasks across 3 waves.

### Wave 1

```
t-1  Add internal/pathfence shared containment check
     origin:   [verification skeleton + risk corpus/sentinels]
     files:    internal/pathfence/pathfence.go
               internal/pathfence/pathfence_test.go
     covers:   c-1, c-3, c-5
     depends:  —
     desc:     New leaf package (imports nothing from internal/ — the
               argfence / hostallow / configenum shape) with three predicates
               over one lexical rule, plus typed sentinels:
                 Contain(root, artifact, p) (string, error) — refuse
                 InTree(root, p) (string, bool)             — classify
                 Segment(label, id) error                   — single-segment
                 ErrEscapes, ErrAbsolute
               Lexical only: filepath.Clean, absolute rejected outright, no
               EvalSymlinks. The doc comment records the symlink limit and why
               (locked symlink_resolution).
     contract: - Contain(root,"changes.json","../x.go") errors; the message
                 contains "../x.go", "changes.json" and root asserted as three
                 separate strings.Contains, not one golden string.
               - Contain(root,a,"/etc/passwd") matches errors.Is(err,
                 ErrAbsolute) and NOT ErrEscapes — deleting the IsAbs branch
                 cannot pass by falling through to the escape branch.
                 [risk: sentinels; verification asserted message text only]
               - Contain(root,a,"a/../b.md") returns Join(root,"b.md"): an
                 interior ".." landing inside is not an escape. Contain(..,"..")
                 and ("../") both error.
               - Hostile corpus asserted case by case, including the near-miss
                 NON-escapes "..foo", "a/..b", "a/b.." which MUST be accepted —
                 a HasPrefix(rel,"..") without the separator check fails here.
                 [risk]
               - InTree("../x") -> ok=false; InTree("a/../b") -> ("b", true).
                 Segment("run id","a/b") and ("..") both error.
               - Contain never touches the filesystem: with root pointing at a
                 path that does not exist, "report.md" is still accepted. [risk]
               - A symlink under root pointing outside is ACCEPTED, in a test
                 named for the symlink_resolution lock, so the documented limit
                 is pinned rather than assumed. [verification]

t-2  Declare the path-shaped field registry
     origin:   [verification + mvp's reason/reader requirement]
     files:    internal/pathfence/fields.go
               internal/pathfence/fields_test.go
     covers:   c-6
     depends:  —
     desc:     Fields declares one entry per path-shaped .dross schema field:
               Struct, Field, toml key, Artifact, and a Disposition of exactly
               Consumed (naming the routing function) or NotConsumed (with Why
               and the reader sites allowed to touch it).
     contract: - Every entry has Struct, Field, Artifact and exactly one
                 Disposition; a third spelling fails.
               - A NotConsumed entry with an empty Why fails, and one with no
                 allowed reader fails. [mvp]
               - Duplicate Struct+Field fails as a double declaration.
               - Fields covers by name at minimum changes.TaskRecord.Files,
                 changes.RedProof.Doc, phase.Task.Files, project.Env.Files,
                 project.Paths.{Source,Tests,E2E,Migrations,Schemas,I18n,Public}
                 and project.TestLane.Match — asserted by name, so deleting a
                 declaration to make a later guard pass fails here first.
```

### Wave 2

```
t-3  Route the run-dir and red-proof-set writers through pathfence
     origin:   [verification + risk's file list]
     files:    internal/cmd/security.go
               internal/cmd/quality.go
               internal/cmd/redproof_set.go
               internal/cmd/security_test.go
               internal/cmd/quality_test.go
               internal/cmd/redproof_set_test.go
     covers:   c-1, c-3
     depends:  t-1
     desc:     Delete containedPath (security.go:188) and repoint all eight
               call sites — security.go:45,152,160,204 and quality.go:39,122,
               130,150 — at pathfence.Contain with the run dir as root and
               "run directory" as the artifact. The two duplicated test blocks
               (security_test.go:281, quality_test.go:160) collapse onto the
               shared behaviour. checkRedProofDoc drops its IsAbs + ".." arms
               and calls Contain, keeping its is-a-directory and does-not-exist
               refusals and its ToSlash return.
               [quality.go / quality_test.go are risk's catch — verification's
                t-3 named neither file yet asserted against quality_test.go]
     contract: - `grep -rn "func containedPath" internal/` finds nothing.
               - containedPath's old cases still refuse, and the message now
                 names the run dir and the artifact — the existing err!=nil
                 assertions are tightened to the three-part message, so a
                 delegation that drops the context fails.
               - A scan-run ledger name of "../main.go" refuses and leaves no
                 file at Join(runDir,"..","main.go"), asserted by stat-ing the
                 parent before and after the run. [risk]
               - Absolute now refuses where it used to silently re-root:
                 "/tmp/x/spec.toml" returns ErrAbsolute instead of writing
                 under the run dir (absolute_paths lock).
               - checkRedProofDoc("/abs/x.md") -> ErrAbsolute and ("../x.md")
                 -> ErrEscapes; its dir-refusal and not-exists refusal still
                 fire for a contained path, so the swap swallowed neither.

t-4  Hard-fail escaping recorded paths — verify and plan.toml
     origin:   [verification + risk's gone-list assertion + mvp's ValidatePlan]
     files:    internal/verify/scope.go
               internal/verify/scope_test.go
               internal/cmd/verifyscope.go
               internal/cmd/verify.go
               internal/phase/plan_edit.go
               internal/phase/plan_edit_test.go
     covers:   c-2, c-5
     depends:  t-1
     desc:     ValidateRecorded(root, paths) runs every changes.json task file
               through pathfence.Contain and returns a hard error on the first
               escape; phaseScope propagates it on BOTH verify entry paths, the
               attached one and the detached finish/collect one, so the refusal
               precedes phaseScope, mutationCandidates and DetachSteps.
               NormalizePath's own ".." literal is replaced by pathfence.InTree.
               ValidatePlan gains the same check over each task's `files`
               (artifact_scope lock names plan.toml task files as in-scope for
               the shared check, and ValidatePlan is the one place every plan
               passes through). [mvp — see disagreement 2]
     contract: - ValidateRecorded(root,["../x.go"]) errors naming "../x.go",
                 "changes.json" and root; the returned scope is nil and
                 Degraded is untouched — a regression to the soft lane is a
                 failure, not a passing variation (escape_failure_mode lock).
               - `dross verify` over such a changes.json exits non-zero, writes
                 NO tests.json and NO verify.toml, and errors BEFORE
                 mutationCandidates — proved by seeding a REAL file at ../x.go
                 outside the repo, so a run that reached os.Stat would have
                 succeeded and dispatched it.
               - The escaping path does NOT appear in the `gone` skip list: a
                 fixture with one valid file and one "../x.go" errors out
                 rather than verifying one and recording one skip. [risk]
               - The git lane is unchanged: a git-supplied out-of-repo path
                 still lands in Degraded as an ignored out-of-repo path rather
                 than aborting, so the hard rule is not over-applied.
               - The detached path refuses on the same fixture, so guarding
                 only the attached call site leaves a red test.
               - NormalizePath("../x") / ("/other/tree") / (".") still return
                 ok=false with Contains false for each.
               - ValidatePlan rejects a task with files=["../x"] and one with
                 files=["/etc/passwd"], each error naming the task id. [mvp]

t-5  Route the testlane and remote traversal checks through pathfence
     origin:   [verification]
     files:    internal/testlane/match.go
               internal/remote/remote.go
     covers:   c-1
     depends:  t-1
     desc:     testlane.normalize's ".." arm becomes pathfence.InTree;
               remote.RunDir's segment test becomes pathfence.Segment, still
               wrapped in ErrUnsafeTarget. Without this, t-8's literal scan
               needs an exemption list — the hand-maintained list c-4 exists to
               eliminate. [see disagreement 1]
     contract: - testlane still classifies "../x.go" and "/abs/x.go" as escapes
                 (escaped bucket, not Unmatched — the distinction a naive
                 delegation loses), "docs/../a.go" as in-tree "a.go", and an
                 empty path as in-tree so it surfaces as Unmatched.
               - remote.RunDir("..") and ("a/b") still return errors matching
                 errors.Is(err, ErrUnsafeTarget) — a delegation returning a
                 bare pathfence error breaks every caller that distinguishes a
                 refusal from a transport failure. RunDir("run-123") still
                 returns ".dross-runs/run-123".

t-6  Contain the red-proof doc on the read and repoint paths
     origin:   [verification + risk's dry-run assertion]
     files:    internal/cmd/redproof_repoint.go
               internal/cmd/redproof.go
               internal/cmd/doctor.go
               internal/cmd/redproof_repoint_test.go
     covers:   c-2, c-3, c-5
     depends:  t-1
     desc:     redproof_repoint.go:142 joins p.Doc — loaded from changes.json —
               onto p.repoDir and then rewrites that file; redproof.go:146
               (redProofDocSHA) reads it the same way. checkRedProofDoc guards
               only `red-proof set`, so a hand-edited red_proof.doc reaches a
               write outside the repo today. Both joins go through Contain with
               "changes.json red_proof.doc" as the artifact, refusing before
               the plan is returned.
     contract: - A changes.json with red_proof.doc = "../../victim.md" makes
                 `dross phase red-proof repoint --apply` refuse with a message
                 naming the doc, changes.json and the repo root, and leaves the
                 outside file byte-identical (asserted by hash — an unguarded
                 build rewrites it in place, and a guard placed after
                 os.WriteFile fails here).
               - The dry-run path refuses too: a plan that prints an escaping
                 doc as "would write" is a refusal the operator never gets. [risk]
               - redProofDocSHA on the same doc returns a containment error,
                 not an os.ReadFile error — asserted on the message, so a fix
                 that merely relies on the file not existing fails.
               - doctor over that record reports the containment refusal as an
                 issue line rather than "cannot check the pin: no such file".

t-7  Guard: every path-shaped schema field is declared
     origin:   [verification + risk's stale-declaration case]
     files:    internal/cmd/pathfence_fields_test.go
     covers:   c-6
     depends:  t-2
     desc:     AST walk over internal/project, internal/phase, internal/changes
               and internal/verify collecting every struct field whose toml or
               json tag is path-shaped (files, doc, path, source, tests, e2e,
               migrations, schemas, i18n, public, match), compared against
               pathfence.Fields in both directions.
     contract: - A path-shaped field with no Fields entry fails, named by
                 struct.field and toml key.
               - The reverse fails: a Fields entry naming a struct or field
                 that no longer exists reports "stale declaration", so a field
                 renamed out from under a NotConsumed entry cannot leave a
                 stale promise. [risk]
               - Walker self-test over an in-test source fixture: a struct with
                 `toml:"doc"` is found, one with `toml:"replay"` is not — a
                 walker that silently stops finding fields fails on its own
                 fixture instead of passing vacuously.
               - Vacuity guard: >= 10 fields found, mirroring
                 TestGuardsSeeNonEmptySets. [risk + verification]
```

### Wave 3

```
t-8  Guard: every join site routes through pathfence
     origin:   [verification + risk's fixture self-test + mvp's consumer cross-check]
     files:    internal/cmd/pathfence_enum_test.go
     covers:   c-1, c-4
     depends:  t-3, t-4, t-5, t-6, t-7
     desc:     Two derived scans, no hand-maintained site list. Modelled on
               enum_divergence_test.go: go/parser + go/ast, no x/tools.
     contract: (a) Join-site scan: for every pathfence.Fields entry, find each
                 non-test function that READS that field's selector and also
                 calls filepath.Join / os.Stat / os.Open / os.ReadFile /
                 os.WriteFile / os.Remove, and fail naming any such function
                 whose body does not also call pathfence.*. A NotConsumed entry
                 that acquires such a consumer fails with "declared
                 not-consumed but <fn> opens it" — this is c-6's stated failure
                 mode (an env.files or testlane consumer added without routing)
                 made falsifiable.
               (b) A Consumed entry whose named routing function does not
                 appear among the guarded join sites fails, so "consumed
                 through the shared check" is checked, not asserted. [risk+mvp]
               (c) Literal scan: any ".." comparison in a PATH context
                 (adjacent to filepath.Separator, "../", path.Clean,
                 filepath.Rel or filepath.IsAbs) outside internal/pathfence
                 fails, naming file:line. The two legal non-path uses are
                 recognised BY SHAPE, not by an exemption list: git rev-list
                 ranges are string concatenation (`a+".."+b`), refguard's is
                 strings.Contains over a ref name with no path call nearby —
                 both asserted as fixtures that must NOT trip the scan, so the
                 shape rule is itself tested.
               (d) Walker self-tests: an in-test fixture with one guarded and
                 one unguarded consumer must yield exactly the unguarded one; a
                 fixture whose consumer was refactored out must report an error
                 rather than an empty set. Vacuity guard: the walk must find at
                 least N join sites and at least one ".." literal (inside
                 pathfence). [risk + verification]
```

### Coverage

| Criterion | Tasks |
|---|---|
| c-1 one shared implementation, no second `..` test | t-1, t-3, t-5, t-8 |
| c-2 tracked path refused before any file I/O | t-4, t-6 |
| c-3 write path keeps containment | t-1, t-3, t-6 |
| c-4 enumerating test over join sites | t-8 |
| c-5 refusal names path, artifact, root | t-1, t-4, t-6 |
| c-6 every path-shaped field accounted for | t-2, t-7, t-8 |

## Disagreements

### 1. How many `..` sites c-1 covers — five, or the three that join onto a root

**What diverged.** c-1 says "no second `..`-prefix test remains in non-test code". There are five in the tree, and they are not the same shape: security.go:194 and redproof_set.go:158 join onto a root; scope.go:349 and match.go:141 are pure `(path, ok)` normalisers in packages documented as doing no I/O; remote.go:329 rejects a run id that is not a single path segment.

**Who said what.** *verification* rewires all five (t-5), arguing that leaving three forces the literal scan to carry an exemption list — a hand-maintained list, which is precisely what c-4 forbids. *risk* and *mvp* both rewire only the join-onto-a-root sites, and both then specify a literal scan that whitelists only the new package — which would go red on the three they left, so neither draft is internally consistent here.

**Provisional default: rewire all five (t-5 stays in the plan).** It is the only reading under which the enumerating test needs no exemption list, and it is the only one whose scan and task list agree.

**Why it matters.** This decides whether t-5 exists at all, whether pathfence needs three predicates or one (disagreement 5), and whether t-8's literal scan is a clean rule or a rule plus a maintained carve-out. Getting it wrong in the lenient direction leaves c-4's guard resting on the same hand list it was written to abolish.

**Choose one:**
- **(A)** Rewire all five — testlane and remote route through `pathfence.InTree` / `pathfence.Segment`, the literal scan carries no exemptions. *(default)*
- **(B)** Scope c-1 to the three join-onto-a-root sites; drop t-5; the literal scan gets a shape rule that recognises pure normalisers and the run-id segment check as legal, and that rule is fixture-tested.

### 2. plan.toml task `files` — validated, or declared dormant

**What diverged.** The artifact_scope lock names "changes.json and plan.toml task `files`" as the fields that go through the shared check. But nothing opens plan.toml's `files` today.

**Who said what.** *mvp* adds the check to `ValidatePlan` (internal/phase/plan_edit.go:82), reasoning it is the one place every plan passes through and it is a write-side gate. *risk* and *verification* both leave plan.toml alone and only declare `phase.Task.Files` in the registry — which, since nothing consumes it, would have to be a NotConsumed declaration, contradicting the lock's own wording.

**Provisional default: graft mvp's ValidatePlan check into t-4.** The lock puts plan.toml in the consumed group, and only mvp honours that.

**Why it matters.** Without it, `phase.Task.Files` is declared dormant while the lock says it is checked, and the registry — the artifact t-7 and t-8 both judge against — starts out disagreeing with the spec. The cost is that t-4 grows to six files across three packages.

**Choose one:**
- **(A)** Check plan.toml task files in `ValidatePlan`, inside t-4. *(default)*
- **(B)** Declare `phase.Task.Files` NotConsumed with "no consumer opens it" and let t-8 fail the day one appears; keep t-4 to verify only.

### 3. Where the hard failure lives — internal/verify, or internal/cmd

**What diverged.** The changes.json refusal has to sit above `mutationCandidates` and below the artifact load. Both spots work.

**Who said what.** *verification* puts `ValidateRecorded` in internal/verify/scope.go and propagates through `phaseScope`, arguing it is unit-testable with no git and keeps `NewScope`'s signature intact. *risk* and *mvp* both explicitly reject touching internal/verify — risk because "Scope is documented pure and NewScope returns no error", mvp because "NewScope's rejection lane is deliberately soft" — and put it in internal/cmd/verifyscope.go at the artifact boundary, where the artifact path is in hand for c-5's message.

**Provisional default: verification's placement.** `ValidateRecorded` is a new function, not a change to `NewScope`; it does no I/O, so the package's purity doc still holds, and both drafts' actual objection (don't make NewScope fail) is respected either way.

**Why it matters.** Two lenses out of three voted the other way, and the package doc comment is on their side rhetorically even if not substantively. It also changes which test file the c-2 falsifier lives in.

**Choose one:**
- **(A)** `verify.ValidateRecorded` in internal/verify, propagated by `phaseScope`. *(default)*
- **(B)** `recordedPaths` / `checkRecordedPaths` in internal/cmd/verifyscope.go, internal/verify untouched except NormalizePath's literal.

### 4. The red-proof doc read path — its own task, folded, or absent

**What diverged.** `redproof_repoint.go:142` rewrites `Join(repoDir, p.Doc)` and `redproof.go:146` reads it; `p.Doc` comes from changes.json and only `red-proof set` is guarded.

**Who said what.** *verification* gives it its own task (t-6) covering repoint, redProofDocSHA and doctor. *risk* folds the repoint half into its redproof task (t-3) and does not touch redProofDocSHA or doctor. *mvp* has no task for it at all — the c-3 gap in its coverage.

**Provisional default: keep it as its own task (t-6), with risk's dry-run assertion grafted on.** It is a different artifact field from `--doc`, it is the only place a tracked path reaches a write outside the repo today, and folding it into t-3 makes one task own two artifacts and six files.

**Why it matters.** This is the live bug in the phase. Under mvp's plan it ships unfixed; under risk's plan the read side and doctor's misleading "no such file" message stay unfixed.

**Choose one:**
- **(A)** Separate t-6 covering repoint + redProofDocSHA + doctor. *(default)*
- **(B)** Fold the repoint write into t-3 alongside `checkRedProofDoc` and leave redProofDocSHA / doctor to t-8's scan to catch.

### 5. pathfence's API surface — three predicates or one

**What diverged.** *verification* exports `Contain` (refuse), `InTree` (classify), `Segment` (single-segment), on one shared lexical rule. *risk* exports one `Contained(root, artifact, name)`; *mvp* one `Resolve(root, artifact, p)`.

**Provisional default: three predicates.** This is downstream of disagreement 1: `scope.NormalizePath` and `testlane.normalize` classify and return a bool, `remote.RunDir` needs a strictly stricter rule, and forcing one refusal signature on them leaves their `..` literals in place.

**Why it matters.** Picking (B) in disagreement 1 collapses this to one predicate automatically. Picking (A) there and one predicate here is incoherent.

**Choose one:**
- **(A)** Three predicates + typed sentinels over one lexical rule. *(default)*
- **(B)** One `Contain(root, artifact, path) (string, error)` — only viable with disagreement 1 answered (B).

### 6. When the field registry is declared

**What diverged.** *verification* declares it in wave 1 as its own task (t-2) and guards it in wave 2 (t-7). *mvp* folds the table into the wave-1 package task. *risk* creates registry and guard together in wave 3 (t-6).

**Provisional default: verification's split.** The registry is the artifact both later guards judge against; creating it last means nothing checks it until the end, and it scans struct tags that no wave-2 rewire touches, so holding the guard back serialises for nothing.

**Why it matters.** Mostly schedule and reviewability — but a registry created in the same task as the test that reads it is a table an implementer can trim to make its own test pass.

**Choose one:**
- **(A)** Registry as wave-1 t-2, declaration guard as wave-2 t-7. *(default)*
- **(B)** Fold the registry into t-1 (mvp) and keep the guard in wave 2 — 7 tasks.

### 7. How the absolute-path rule is proven

**What diverged.** *risk* uses typed sentinels (`errors.Is(err, ErrAbsolute)` vs `ErrEscapes`) so deleting the IsAbs branch cannot pass by falling through to the escape branch. *verification* asserts the message says "must be repo-relative". *mvp* just gives absolute its own test case.

**Provisional default: sentinels, with the three-substring message assertion kept on top.** t-5 needs `errors.Is` wrapping for `ErrUnsafeTarget` anyway, so the error type is not extra machinery.

**Why it matters.** Lowest-consequence entry, but the absolute rule is a locked decision whose whole justification is diagnosability — a test that can pass while the branch is gone is not pinning it.

**Choose one:**
- **(A)** `ErrEscapes` / `ErrAbsolute` sentinels + three-substring message assertions. *(default)*
- **(B)** Message-text assertions only, one error type.
