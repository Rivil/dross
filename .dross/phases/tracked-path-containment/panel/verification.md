# Panel draft — verification lens

Phase tracked-path-containment — 8 tasks across 3 waves

Designed backward from the test contracts. Every criterion here has one test that
can go red; where a criterion could only be satisfied by "we looked and it's fine",
the task exists to turn it into a scan that fails.

## Wave 1

```
t-1  Add internal/pathfence shared containment check
     files:    internal/pathfence/pathfence.go
               internal/pathfence/pathfence_test.go
     covers:   c-3, c-5
     contract: Contain(root, "changes.json", "../x.go") returns an error whose
               text contains all three of "../x.go", "changes.json" and root —
               asserted as three separate strings.Contains, not one golden string.
               Contain(root, a, "/etc/passwd") errors on the ABSOLUTE rule
               (message says "must be repo-relative"), it does not silently
               re-root — the absolute_paths lock, falsifiable.
               Contain(root, a, "a/../b.md") returns filepath.Join(root,"b.md"):
               an interior ".." that lands inside is NOT an escape.
               Contain(root, a, "..") and ("../") both error.
               InTree("../x") -> ok=false; InTree("a/../b") -> ("b", true).
               Segment("run id", "a/b") and Segment("run id", "..") both error.
               A symlink under root pointing outside is ACCEPTED, with the test
               named for the symlink_resolution lock so the documented limit is
               pinned rather than assumed.

t-2  Declare the path-shaped field registry
     files:    internal/pathfence/fields.go
               internal/pathfence/fields_test.go
     covers:   c-6
     contract: Fields is non-empty and every entry has Struct, Field, Artifact
               and a Disposition of exactly Consumed or NotConsumed — a third
               spelling fails. A NotConsumed entry with an empty Why fails
               ("a dormant field must say why nothing opens it").
               Two entries with the same Struct+Field fail as a duplicate
               declaration. Fields covers at minimum changes.TaskRecord.Files,
               changes.RedProof.Doc, phase.Task.Files, project.Env.Files,
               project.Paths.{Source,Tests,E2E,Migrations,Schemas,I18n,Public}
               and project.TestLane.Match — asserted by name, so deleting a
               declaration to make a later guard pass fails here first.
```

## Wave 2 (depends t-1, t-2)

```
t-3  Route the two existing checks through pathfence
     files:    internal/cmd/security.go
               internal/cmd/redproof_set.go
               internal/cmd/security_test.go
               internal/cmd/redproof_set_test.go
     covers:   c-1, c-3
     depends:  t-1
     contract: containedPath(runDir, "../main.go") still errors AND the message
               now names runDir and the run-directory artifact — the existing
               err!=nil assertions in security_test.go/quality_test.go are
               tightened to assert the three-part message, so a delegation that
               drops the context fails.
               checkRedProofDoc(repo, "/abs/x.md") errors with the absolute rule
               and checkRedProofDoc(repo, "../x.md") with the escape rule, both
               from pathfence; its own dir-refusal and not-exists refusal still
               fire (a delegation that swallows them fails those two cases).
               After this task neither file contains a ".." string comparison —
               enforced by t-8, asserted here by grep in review.

t-4  Hard-fail escaping recorded paths in the verify scope
     files:    internal/verify/scope.go
               internal/verify/scope_test.go
               internal/cmd/verifyscope.go
               internal/cmd/verify.go
     covers:   c-2, c-5
     depends:  t-1
     contract: verify.ValidateRecorded(root, []string{"../x.go"}) returns an
               error naming "../x.go", "changes.json" and root (escape_failure_mode
               lock: an error, NOT a Degraded entry — the test asserts the returned
               scope is nil and that Degraded is untouched, so a regression back to
               the soft lane is a failure, not a passing variation).
               phaseScope propagates it: `dross verify` over a changes.json whose
               task files name "../x.go" exits non-zero, writes NO tests.json and
               NO verify.toml, and the error surfaces before mutationCandidates —
               proved by seeding a REAL file at ../x.go outside the repo, so a
               run that reached os.Stat would have succeeded and dispatched it.
               The git lane is unchanged: a git-supplied out-of-repo path still
               lands in Degraded as "ignored out-of-repo path" rather than
               aborting (asserted, so the hard rule is not over-applied).
               NormalizePath's own ".." literal is replaced by pathfence.InTree
               and NormalizePath("../x")/("/other/tree")/(".") still return
               ok=false, with Contains still false for each.

t-5  Route testlane and remote traversal checks through pathfence
     files:    internal/testlane/match.go
               internal/remote/remote.go
     covers:   c-1
     depends:  t-1
     contract: testlane still classifies "../x.go" and "/abs/x.go" as escapes
               (they land in the escaped bucket, not Unmatched — the distinction
               a naive delegation loses), and "docs/../a.go" as in-tree "a.go";
               an empty path stays in-tree so it surfaces as Unmatched.
               remote.RunDir("..") and RunDir("a/b") still return errors wrapping
               ErrUnsafeTarget — asserted with errors.Is, because a delegation
               that returns a bare pathfence error breaks every caller that
               distinguishes a refusal from a transport failure.
               RunDir("run-123") still returns ".dross-runs/run-123".

t-6  Contain the red-proof doc on the READ path
     files:    internal/cmd/redproof_repoint.go
               internal/cmd/redproof.go
               internal/cmd/doctor.go
     covers:   c-2, c-5
     depends:  t-1
     contract: redproof_repoint.go:142 and redproof.go:146 both join a doc loaded
               from changes.json onto repoDir with no check today — the write-time
               check in `red-proof set` is bypassed by any hand-edited record.
               A changes.json carrying red_proof.doc = "../../victim.md" makes
               `dross phase red-proof repoint --apply` refuse with a message
               naming the doc, changes.json and the repo root, and leaves the
               file at ../../victim.md byte-identical (asserted by hash, since
               applyRedProofRepoint rewrites the doc in place — an unguarded
               build mutates a file outside the repo and this test catches it).
               redProofDocSHA on the same doc returns a containment error, not an
               os.ReadFile error — asserted on the message, so a fix that merely
               relies on the file not existing fails.
               doctor over that record reports the containment refusal as an
               issue line rather than "cannot check the pin: no such file".

t-7  Guard: every path-shaped schema field is declared
     files:    internal/cmd/pathfence_fields_test.go
     covers:   c-6
     depends:  t-2
     contract: an AST walk over internal/project, internal/phase, internal/changes
               and internal/verify collects every struct field whose toml or json
               tag is path-shaped (files, doc, path, source, tests, e2e,
               migrations, schemas, i18n, public, match) and fails naming any that
               pathfence.Fields does not declare — verified against a source
               fixture in the test itself (a struct with `toml:"doc"` is found,
               one with `toml:"replay"` is not), so a walker that silently stops
               finding fields fails on its own fixture instead of passing
               vacuously. The reverse also fails: a Fields entry naming a struct
               or field that no longer exists reports "stale declaration".
               A minimum-count assertion (>= 10 fields found) is the vacuity
               guard, mirroring TestGuardsSeeNonEmptySets.
```

## Wave 3 (depends t-3, t-4, t-5, t-6, t-7)

```
t-8  Guard: every join site routes through pathfence
     files:    internal/cmd/pathfence_enum_test.go
     covers:   c-1, c-4
     depends:  t-3, t-4, t-5, t-6, t-7
     contract: Two derived scans, no hand-maintained site list.
               (a) Join-site scan: for every pathfence.Fields entry, the walk
               finds each non-test function that READS that field's selector and
               also contains a filepath.Join / os.Stat / os.Open / os.ReadFile /
               os.WriteFile / os.Remove call, and fails naming any such function
               whose body does not also call pathfence.*. Adding a consumer for
               env.files or a testlane path without routing it through the check
               fails here naming the function — the c-6 obligation made
               falsifiable. A NotConsumed entry that acquires such a consumer
               fails with "declared not-consumed but <fn> opens it".
               (b) Literal scan: any ".." string comparison in a PATH context
               (adjacent to filepath.Separator, "../", path.Clean, filepath.Rel
               or filepath.IsAbs) outside internal/pathfence fails, naming
               file:line. The two non-path uses stay legal and are recognised by
               shape, not by an exemption list: git rev-list ranges are string
               CONCATENATION (`a+".."+b`) and refguard's check is
               strings.Contains over a ref name with no path call nearby — both
               asserted as fixtures that must NOT trip the scan, so the shape
               rule is itself tested.
               Walker self-tests, mirroring TestProviderCasesInParsesFixture:
               an in-test source fixture containing one guarded consumer and one
               unguarded consumer must yield exactly the unguarded one; a
               fixture whose consumer was refactored out must report an error
               rather than an empty set.
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 one shared implementation, no second `..` test | t-3, t-5, t-8 |
| c-2 tracked path refused before any file I/O | t-4, t-6 |
| c-3 write path keeps containment | t-1, t-3 |
| c-4 enumerating test over join sites | t-8 |
| c-5 refusal names path, artifact, root | t-1, t-4, t-6 |
| c-6 every path-shaped field accounted for | t-2, t-7, t-8 |

6/6 covered.

## Judgment calls

- **New leaf package `internal/pathfence` rather than a helper in internal/cmd.**
  Rejected: exporting `containedPath` from internal/cmd. internal/verify,
  internal/testlane and internal/remote all need it and none may import
  internal/cmd. `argfence` and `hostallow` are the repo's established shape for a
  shared refusal, and configenum's leaf-package rule is what lets the guard test
  compare a registry against source without a cycle.

- **Four entry points, one predicate: `Contain`, `InTree`, `Segment`, plus the
  error type.** Rejected: a single `Contain` everywhere. `verify.NormalizePath`
  and `testlane.normalize` CLASSIFY (bool) rather than refuse, and
  `remote.RunDir` needs a strictly stricter single-segment rule. Forcing one
  signature would have left those three sites carrying their own `..` literals,
  which is exactly the second implementation c-1 forbids.

- **Rewire testlane/match.go and remote/remote.go too, not just the two sites
  c-1 names.** The repo has FIVE `..`-prefix path tests in non-test code
  (security.go:194, redproof_set.go:158, scope.go:349, match.go:141,
  remote.go:329), not two. Leaving three would force t-8's literal scan to carry
  an exemption list — a hand-maintained list, which is the thing c-4 exists to
  eliminate. Rejected: exempting them; the exemption is the bug.

- **The hard failure lands in `verify.ValidateRecorded` + `phaseScope`, not in
  `NewScope`'s signature.** Rejected: changing `NewScope` to return an error,
  which touches five return sites and would apply the hard rule to the GIT side
  too — where an out-of-repo path is a git quirk, not a corrupt artifact, and the
  existing Degraded lane is correct. Splitting keeps `verify.Scope` pure and
  makes the recorded-side rule unit-testable with no git.

- **Added t-6 for the red-proof doc READ path, which the spec does not name.**
  `redproof_repoint.go:142` reads AND REWRITES `filepath.Join(repoDir, pin.Doc)`
  where `pin.Doc` came out of changes.json, and `redproof.go:146` reads it — both
  unguarded. The write-time `checkRedProofDoc` guards only `red-proof set`, so a
  hand-edited record writes outside the repo today. It is the same bug c-2
  describes, one field over, and t-8's scan would fail without it.

- **t-7 (field declarations) sits in wave 2, not behind the rewires.** It scans
  schema struct tags, which no wave-2 task changes, so holding it back would
  serialise for nothing. Only t-8's join-site scan needs the rewires to have
  landed.

- **Error assertions are three `strings.Contains` calls, not one golden string.**
  c-5 is a claim about three facts being present, and a golden string turns any
  rewording into a red test, which trains people to update the golden rather than
  read it.
