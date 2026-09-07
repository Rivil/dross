# MVP lens draft

Phase tracked-path-containment — 4 tasks across 3 waves

Wave 1
  t-1  Add trackedpath check and field registry
       files:    internal/trackedpath/trackedpath.go, internal/trackedpath/fields.go,
                 internal/trackedpath/trackedpath_test.go
       covers:   c-5, c-6
       description:
                 New leaf package (imports nothing from internal/, the configenum
                 pattern) holding `Resolve(root, artifact, p string) (string, error)`:
                 lexical cleaning only, absolute rejected outright, `..` escape
                 rejected, error text carries path + artifact + root. Doc comment
                 states the no-EvalSymlinks limit. fields.go declares `Fields`, the
                 registry of every path-shaped .dross schema field — struct, Go field
                 name, toml key, Consumed bool, Reason (required when not consumed),
                 and the source sites allowed to read a not-consumed field.
       contract: - Resolve(root, "changes.json", "../x") errors, and the message
                   contains "../x", "changes.json" and the root string — asserted on
                   all three substrings, not just on err != nil.
                 - Resolve(root, a, "/etc/passwd") errors instead of returning
                   root + "/etc/passwd"; the absolute-path arm has its own case, so
                   deleting it fails even though escape rejection still passes.
                 - Resolve(root, a, "sub/../report.md") returns root/report.md — a
                   `..` that stays inside is not an escape.
                 - Every Fields entry with Consumed=false has a non-empty Reason and
                   at least one allowed reader; a table entry added without a reason
                   fails TestFieldsDeclarationsComplete.

Wave 2 (depends t-1)
  t-2  Route both containment sites through trackedpath
       files:    internal/cmd/security.go, internal/cmd/redproof_set.go,
                 internal/cmd/security_test.go, internal/cmd/redproof_set_test.go
       covers:   c-1, c-3
       description:
                 containedPath (security.go:184, shared with quality.go) becomes a
                 thin delegate to trackedpath.Resolve with the run dir as artifact
                 label; its own `..`-prefix comparison is deleted. checkRedProofDoc
                 drops its IsAbs + `..` arms and calls Resolve, keeping its
                 is-a-directory and does-not-exist refusals and its ToSlash return.
       depends_on: t-1
       contract: - containedPath(runDir, "../main.go") still errors and the message
                   now names runDir, so the scan write path (writeRunReport,
                   findings.toml, spec.toml) cannot land outside the run dir.
                 - containedPath(runDir, "/etc/passwd") errors — the old behaviour
                   (filepath.Join silently re-rooting it under runDir) is asserted
                   gone by checking the error, not the returned path.
                 - checkRedProofDoc("--doc ../x") and ("--doc /abs/x") each fail with
                   the shared message; checkRedProofDoc on a directory and on a
                   missing file still fail with their own two messages, so the
                   delegation did not swallow the pin-specific refusals.

  t-3  Hard-fail escaping paths from changes.json and plan.toml
       files:    internal/cmd/verifyscope.go, internal/cmd/verify.go,
                 internal/phase/plan_edit.go, internal/cmd/verifyscope_test.go,
                 internal/phase/plan_edit_test.go
       covers:   c-2, c-5
       description:
                 New recordedPaths(repoDir, ch.Tasks) ([]string, error) in
                 verifyscope.go runs every task file through trackedpath.Resolve and
                 returns a hard error on the first escape; both verify.go call sites
                 (the attached run ~line 74 and the detached collect ~line 585)
                 replace their inline filesByTask/FilesFromChanges pair with it and
                 return the error. ValidatePlan gains the same check over each
                 task's `files`.
       depends_on: t-1
       contract: - `dross verify` over a changes.json whose task t-2 records
                   ["../x"] returns an error naming t-2, changes.json and the repo
                   root, and returns it BEFORE mutationCandidates runs — the test
                   asserts no os.Stat happened by using a repoDir where ../x exists,
                   so a stat-then-dispatch implementation would pass otherwise.
                 - The escaping path does not appear in mutationCandidates' `gone`
                   list and no tests.json/verify.toml is written — the soft-skip
                   lane is asserted unused (escape_failure_mode lock).
                 - The detached collect path fails on the same record, so fixing
                   only the attached call site leaves TestDetachedCollectRefuses
                   EscapingRecordedPath red.
                 - ValidatePlan rejects a task with files = ["../x"] and one with
                   files = ["/etc/passwd"], each naming the task id.

Wave 3 (depends t-2, t-3)
  t-4  Add enumerating tracked-path containment guard
       files:    internal/cmd/trackedpath_containment_test.go
       covers:   c-1, c-4, c-6
       description:
                 AST guard modelled on enum_divergence_test.go: parses non-test Go
                 source with go/parser and go/ast (no x/tools — not a dependency)
                 and derives its site set from the source plus trackedpath.Fields,
                 not from a hand-written site list.
       depends_on: t-2, t-3
       contract: - Re-hand-rolling containment fails: any non-test file outside
                   internal/trackedpath containing a `".."` comparison
                   (`== ".."` or `strings.HasPrefix(x, ".."+...)`) is reported by
                   name — reverting t-2's delegation in security.go turns this red.
                 - A Fields entry with Consumed=true whose read site sits in a
                   function whose body never mentions `trackedpath.` is reported,
                   naming the function: a new consumer of changes.json task files
                   that joins them onto a root itself fails the suite.
                 - A Fields entry with Consumed=false (project Env.Files, the
                   testlane path fields) read anywhere outside its declared reader
                   sites is reported — adding an env.files loader to internal/cmd
                   fails here, which is c-6's stated failure mode.
                 - A path-shaped field added to project.Project, phase.Task or
                   changes.TaskRecord with no Fields entry is reported by toml key,
                   so the registry cannot silently fall behind the schema.
                 - Walker self-test over in-test fixture source (the
                   enum_divergence precedent): the extractors must find the seeded
                   escape comparison and the seeded field read, so an extraction
                   that silently finds nothing fails loudly instead of turning every
                   guard above into a vacuous pass.

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-2 (both sites delegate), t-4 (no second `..` test survives) |
| c-2 | t-3 |
| c-3 | t-2 |
| c-4 | t-4 |
| c-5 | t-1 (message shape), t-3 (message content at the changes.json site) |
| c-6 | t-1 (the declarations), t-4 (the test that reads them) |

6/6 criteria covered.

## Judgment calls

- One new leaf package (internal/trackedpath) rather than exporting containedPath
  from internal/cmd: internal/phase must call it for plan.toml validation, and
  internal/phase importing internal/cmd is a cycle. Rejected the alternative of
  putting the check in internal/verify — verify already imports mutation, so it is
  not a leaf and the configenum precedent for a dependency-free home applies.
- Registry lives in the same package as the check (fields.go), not a separate
  package: it is ~30 lines of table and a second package buys nothing but an
  import. Rejected splitting it into its own task for the same reason — it would be
  a one-file, sub-10-minute task.
- The changes.json guard sits in cmd (verifyscope.go), not inside
  verify.NewScope: NewScope's rejection lane is deliberately soft and degraded-
  reporting, and rewiring it to a hard error would change how out-of-repo git paths
  behave too. The lock asks for a hard abort on the tracked artifact specifically,
  so the guard goes at the load site, above the pure type.
- plan.toml task `files` is checked in ValidatePlan (a write-side gate) rather than
  at a read site, because nothing currently opens it — the artifact_scope lock
  still names it as in-scope for the shared check, and ValidatePlan is the one
  place every plan passes through.
- Kept t-2 and t-3 separate despite both being "route a call site": they share no
  file, both depend only on t-1, and merging them makes a 9-file task that spans
  three packages.
- t-4 is one task, not one per criterion: it is a single test file with one set of
  AST extractors, and splitting it would mean two files re-implementing the same
  walker.
- No task for internal/verify/scope.go: nothing in the criteria requires changing
  NormalizePath, and widening it to a hard failure would break the git-path
  degradation path that c-2 does not touch.
