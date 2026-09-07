# Panel draft — RISK lens

Phase tracked-path-containment — 6 tasks across 3 waves

The graph is shaped by the failure modes, one owner each:

| Risk | Owner |
|---|---|
| Two containment rules that disagree (lenient absolute vs strict) | t-1, t-2 |
| A hostile path shape one site accepts and another refuses | t-1 (single corpus) |
| A tracked artifact's path reaching `os.Stat` / a mutation adapter | t-4 |
| A tracked artifact's path reaching a **write** (`os.WriteFile`) | t-3 |
| A guard that exists but runs *after* the I/O it protects | t-3, t-4 (ordering assertions) |
| A newly added join site that skips the check | t-5 |
| A newly added consumer for a dormant field (`env.files`, testlane) | t-6 |
| The enumerating guards passing vacuously | t-5, t-6 (vacuity guards) |

## Wave 1

```
  t-1  Add pathfence leaf package with hostile corpus
       files:    internal/pathfence/pathfence.go
                 internal/pathfence/pathfence_test.go
       covers:   c-1, c-5
       depends:  —
       desc:     New leaf package (imports nothing from internal/, like
                 internal/argfence and internal/configenum). Contained(root,
                 artifact, name) (string, error) does lexical containment only:
                 reject absolute outright, filepath.Clean, reject "." parent
                 escape, return the joined path. Typed ErrEscapes /
                 ErrAbsolute sentinels so callers can errors.Is. The doc
                 comment records the symlink limit (locked symlink_resolution)
                 and says why EvalSymlinks is not called.
       contract: - Contained(root,"changes.json","../main.go") returns an error
                   matching errors.Is(err, pathfence.ErrEscapes); the message
                   contains all three of "../main.go", "changes.json" and root.
                 - Contained(root,_, "/etc/passwd") returns ErrAbsolute, NOT
                   ErrEscapes — the absolute rule is its own refusal, so
                   dropping the IsAbs branch and letting Join re-root cannot
                   pass by falling through to the escape branch.
                 - The hostile corpus table (absolute, "..", "../x", "a/../../b",
                   "./../x", "..", ".", "", "x/..", trailing "/", "\\..\\x",
                   and the near-miss non-escapes "..foo", "a/..b", "a/b..")
                   is asserted refused/accepted case by case: "..foo" and
                   "a/..b" MUST be accepted, so a HasPrefix(rel,"..") written
                   without the separator check fails here.
                 - Contained never touches the filesystem: the test runs with
                   root pointing at a path that does not exist and still gets
                   an accept for "report.md".
```

## Wave 2 (all depend on t-1)

```
  t-2  Route run-dir writers through pathfence
       files:    internal/cmd/security.go
                 internal/cmd/quality.go
                 internal/cmd/security_test.go
                 internal/cmd/quality_test.go
       covers:   c-1, c-3
       depends:  t-1
       desc:     Delete containedPath (security.go:184) and repoint its six
                 call sites (security.go:45,152,160; quality.go:39,122,130)
                 plus writeRunReport in both files at pathfence.Contained with
                 the run dir as root and "run directory" as the artifact label.
                 The two duplicated containedPath test blocks
                 (security_test.go:281, quality_test.go:160) collapse onto the
                 shared behaviour.
       contract: - `grep -rn "func containedPath" internal/` finds nothing;
                   the security/quality scaffold sites compile only against
                   pathfence.Contained.
                 - security scaffold with a findings ledger name of
                   "../main.go" refuses and leaves no file at
                   filepath.Join(runDir,"..","main.go") — asserted by stat-ing
                   the parent dir before and after the run.
                 - Absolute now refuses where it used to silently re-root:
                   the scaffold site given "/tmp/x/spec.toml" returns
                   ErrAbsolute instead of writing under the run dir.

  t-3  Fence the red-proof doc on read and on write
       files:    internal/cmd/redproof_set.go
                 internal/cmd/redproof_repoint.go
                 internal/cmd/redproof_repoint_test.go
                 internal/cmd/redproof_set_test.go
       covers:   c-1, c-3
       depends:  t-1
       desc:     checkRedProofDoc drops its own IsAbs + ".." block and calls
                 pathfence.Contained(repoDir, "--doc", doc), keeping its
                 is-a-dir / does-not-exist checks on top. applyRedProofRepoint
                 (redproof_repoint.go:142) currently joins pin.Doc — read out
                 of changes.json — straight onto repoDir and then os.Stat /
                 os.ReadFile / os.WriteFile it; that join goes through
                 pathfence with "changes.json red_proof.doc" as the artifact,
                 and redProofRepointPlan refuses before the plan is returned.
       contract: - A changes.json whose red_proof.doc is "../../evil.md" makes
                   `dross redproof repoint --apply` refuse, and the test
                   asserts the file outside the repo dir is neither created
                   nor modified — an mtime+existence check on the outside
                   path, so a guard placed after os.WriteFile fails.
                 - The same record makes the dry-run path refuse too: a plan
                   that prints an escaping doc as "would write" is a refusal
                   the operator does not get.
                 - checkRedProofDoc("/abs/doc.md") still refuses, now via
                   errors.Is(err, pathfence.ErrAbsolute), and its "is a
                   directory" and "does not exist" refusals still fire for a
                   contained path — the extra checks were not lost in the
                   swap.

  t-4  Refuse escaping changes.json paths before verify I/O
       files:    internal/cmd/verify.go
                 internal/cmd/verifyscope.go
                 internal/cmd/verify_scoping_test.go
                 internal/cmd/verifyscope_test.go
       covers:   c-2, c-5
       depends:  t-1
       desc:     New checkRecordedPaths(repoDir, artifactPath string, filesByTask
                 map[string][]string) error in verifyscope.go, calling
                 pathfence.Contained per recorded path. Called on both verify
                 entry paths — the attached one at verify.go:76 before
                 phaseScope, and the detached finish path at verify.go:587 —
                 so the refusal precedes phaseScope, mutationCandidates
                 (verify.go:1189) and DetachSteps. Hard error per the locked
                 escape_failure_mode: it returns, it does not join `gone`.
       contract: - A changes.json recording task file "../x.go" makes
                   `dross verify` return a non-nil error naming "../x.go",
                   the changes.json path and the repo root; tests.json and
                   verify.toml are absent afterwards.
                 - The escaping path never reaches mutationCandidates: the
                   test injects a mutationCandidates-adjacent stat counter (or
                   asserts the error surfaces with the scope never built) so
                   moving the check to after phaseScope fails.
                 - An escaping path does NOT appear in the `gone` skip list of
                   a run that otherwise succeeds — a fixture with one valid
                   file and one "../x.go" errors out rather than verifying one
                   file and recording one skip.
                 - The detached `verify finish` path refuses on the same
                   fixture, so guarding only the attached call site fails.
```

## Wave 3 (depend on the wave-2 sites existing)

```
  t-5  Enumerate root-join sites with an AST guard
       files:    internal/cmd/tracked_path_join_test.go
       covers:   c-1, c-4
       depends:  t-2, t-3, t-4
       desc:     Modelled on internal/cmd/enum_divergence_test.go. Walks every
                 non-test .go file under internal/ with go/parser, collects
                 each filepath.Join call whose first argument is a root-shaped
                 expression (identifier or selector matching repoDir, root,
                 repoRoot, runDir, dir, p.repoDir) and whose remaining
                 argument is NOT a string literal, and compares that set
                 against a declared table of known sites. Second scan: no
                 non-test file contains a `".."` prefix/equality test on a
                 path (the c-1 half), whitelisting only internal/pathfence.
       contract: - Adding a new `filepath.Join(repoDir, someVar)` anywhere
                   under internal/ fails the test naming the file, function
                   and expression — proven by a fixture source string that the
                   walker is run over directly, not only by the live tree.
                 - Re-adding `strings.HasPrefix(rel, ".."+string(os.PathSeparator))`
                   outside internal/pathfence fails the second scan.
                 - Vacuity guard: the walker must find at least N join sites
                   and at least one `".."` literal (inside pathfence), so a
                   parser that silently matches nothing fails loudly instead
                   of passing every assertion emptily.

  t-6  Declare and enumerate schema path fields
       files:    internal/pathfence/registry.go
                 internal/cmd/tracked_path_schema_test.go
       covers:   c-6
       depends:  t-2, t-3, t-4
       desc:     registry.go declares one entry per path-shaped field in the
                 .dross schemas — struct, field, and either Consumed (naming
                 the function that routes it through Contained) or NotConsumed
                 (with the reason nothing opens it). Entries cover
                 changes.TaskRecord.Files, changes.RedProof.Doc,
                 phase.Task.Files, project.Env.Files (NotConsumed),
                 project.Paths.{Source,Tests,E2E,Migrations,Schemas,I18n,Public}
                 (NotConsumed), project.TestLane.Match (NotConsumed) and
                 project.Mutation*.{RootRunDir,RemoteWorkdir,Workdir}. The
                 test AST-walks internal/changes/changes.go,
                 internal/phase/phase.go and internal/project/project.go and
                 compares the string / []string fields with path-shaped names
                 against the registry, both directions.
       contract: - Adding a `Doc string` or `Files []string` field to any of
                   the three schema files without a registry entry fails,
                   naming struct.field — proven against a fixture source, so
                   the assertion is not hostage to the live tree.
                 - A registry entry naming a struct or field that no longer
                   exists fails — a field renamed out from under a
                   NotConsumed declaration cannot leave a stale promise.
                 - A Consumed entry whose named function does not appear in
                   the join-site table from t-5 fails, so "consumed through
                   the shared check" is checked rather than asserted.
                 - Vacuity guard: the schema walk must yield at least the
                   known field count, so an extraction that stops seeing
                   fields fails instead of passing empty.
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 shared implementation, no second `..` test | t-1, t-2, t-3, t-5 |
| c-2 escaping changes.json path refused before I/O | t-4 |
| c-3 write path keeps containment | t-2, t-3 |
| c-4 enumerating test over join sites | t-5 |
| c-5 diagnosable refusal (path, artifact, root) | t-1, t-4 |
| c-6 every schema path field accounted for | t-6 |

Every criterion has at least one owner; c-2, c-4, c-6 have exactly one, so no
criterion is split across tasks that could each assume the other covers it.

## Judgment calls

- **New leaf package `internal/pathfence` rather than extending an existing
  one.** Rejected: keeping the helper in internal/cmd (internal/verify and any
  future internal/changes consumer could not import it without a cycle) and
  folding it into internal/argfence (that package is about argv policy; a path
  containment rule sharing its name would blur what a fence means). The name
  follows the argfence/configenum family and the doc comment carries the
  symlink limit the lock requires.
- **Argument order `Contained(root, artifact, name)`.** Rejected the
  containedPath signature `(root, name)` plus a separate error-wrapping call at
  each site: c-5 wants the artifact in the message, and an optional wrap is a
  thing five sites will spell five ways. Making it a required parameter means
  a site cannot produce an undiagnosable refusal.
- **Absolute paths get their own sentinel, not a reuse of the escape error.**
  Rejected a single ErrContainment. The locked absolute_paths decision says
  absolutes are refused for diagnosability, not security — two sentinels let
  the test prove the IsAbs branch is doing the refusing, so deleting it cannot
  pass by falling through.
- **t-3 owns the red-proof repoint site, which no criterion names explicitly.**
  applyRedProofRepoint joins `pin.Doc` — a value read out of changes.json —
  onto repoDir and then os.WriteFile's it. That is c-3's exact shape (a
  tracked-artifact path reaching a write) at a site the spec's examples do not
  list, and c-4's enumerating test would fail on it in wave 3 anyway. Fixing
  it in wave 2 rather than discovering it in wave 3 keeps the guard task from
  turning into a second implementation task.
- **The ordering risk is asserted by outside-the-root file state, not by a
  mock.** Rejected injecting a filesystem interface: the failure mode is "the
  check runs after the write", and the cheapest falsifier is that the file the
  escape names must not exist or change mtime. It needs no new seam and it
  fails for the right reason.
- **c-2 refuses at the verify entry points, not inside verify.Scope.**
  Rejected putting the check in internal/verify/scope.go: Scope is documented
  pure and NewScope returns no error, so making it fail would change a type
  three call sites depend on. Refusing in verifyscope.go keeps the pure type
  pure and puts the refusal at the artifact boundary, where the artifact path
  is in hand for c-5's message.
- **Both enumerating tests carry a fixture-driven self-test plus a vacuity
  guard.** Rejected asserting only against the live tree: enum_divergence_test
  already learned that an extraction which silently finds nothing turns every
  guard into a no-op, and a guard that cannot fail is worse than no guard
  because it reads as coverage.
- **Split the enumeration into two tasks (join sites, schema fields) rather
  than one.** They share a parser but they falsify different things and would
  otherwise be one task writing one file that owns three criteria — the shape
  where a partial implementation still looks done.
