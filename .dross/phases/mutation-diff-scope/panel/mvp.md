# Panel draft — MVP lens

Phase mutation-diff-scope — 5 tasks across 3 waves

```
Wave 1
  t-1  Add Scope type and unified-diff range parsing
       files:    internal/verify/scope.go, internal/verify/scope_test.go
       covers:   c-6, c-7
       contract: ParseUnifiedDiff on a patch containing "+++ b/internal/a.go" and
                 "@@ -10,0 +11,3 @@" yields Scope.InHunk("internal/a.go", 11) ==
                 true and InHunk(..., 14) == false; a file added via
                 Scope.AddFile (the changes.json leg, no ranges) has Has()==true
                 and InHunk()==false for every line; Scope.FileList() returns the
                 union sorted+deduped when both legs name the same file.

  t-2  Add per-file counts and scope tag to Report
       files:    internal/mutation/adapter.go, internal/mutation/gremlins.go,
                 internal/mutation/stryker.go, internal/mutation/gremlins_test.go,
                 internal/mutation/stryker_test.go
       covers:   c-2, c-4
       contract: ParseGremlinsJSON on a report with one KILLED mutation in a.go
                 and one LIVED in b.go yields ByFile["a.go"].Killed==1,
                 ByFile["b.go"].Survived==1 and leaves Killed/Survived totals
                 unchanged; ParseStrykerJSON on a two-file Stryker report yields
                 the same ByFile shape (proving StrykerNet, which delegates to it,
                 inherits attribution); mergeInto of two per-package gremlins
                 reports sums the ByFile entry for a file present in both instead
                 of dropping one.

Wave 2 (depends t-1, t-2)
  t-3  Filter mutation reports to in-scope mutants
       files:    internal/verify/scope.go, internal/verify/verify.go,
                 internal/verify/verify_test.go
       covers:   c-1, c-2, c-4, c-6, c-7
       description: Add Tests.Scope (the scoped file list), Tests.OutOfScope
                 ([]mutation.Mutant), and (*Tests).ApplyScope(Scope). ApplyScope
                 walks every LanguageRun's *mutation.Report: partitions Surviving
                 into in-scope (tagged "in-hunk" | "inherited" on Mutant.Tag) and
                 out-of-scope, and recomputes Killed / Survived / Timeout /
                 Errors / NotCovered / Score from ByFile restricted to the scope.
       contract: given a report with a survivor in a scoped file and a survivor in
                 an untouched file of the same package, ApplyScope leaves exactly
                 one entry in Report.Surviving and moves the other to
                 Tests.OutOfScope with its file/line/op intact; a ByFile entry of
                 {Killed:5} for an untouched file leaves Score unchanged (neither
                 numerator nor denominator moves), while removing the scoped
                 file's own Killed entry does change it; a survivor on a line
                 inside a recorded hunk gets Tag=="in-hunk" and one on an
                 untouched line of the same file gets Tag=="inherited"; a Tests
                 whose Languages carry two different Tool values is filtered by
                 the same code path (both reports' survivors are partitioned).

Wave 3 (depends t-3)
  t-4  Report empty-scope status and out-of-scope count
       files:    internal/verify/verify.go, internal/verify/verify_test.go,
                 assets/prompts/verify.md, internal/cmd/verify_prompt_test.go
       covers:   c-3, c-5
       description: Add MutationOutOfScope = "out-of-scope" status; Skeleton sets
                 it when adapters instrumented mutants but zero landed in scope.
                 Add summary.mutants_in_scope, and one NOTE finding carrying
                 len(Tests.OutOfScope). Survivor FLAG text carries the Mutant.Tag.
                 verify.md gains the new status in its threshold branch and points
                 at tests.json `out_of_scope` as a distinct list.
       contract: Skeleton on a Tests whose reports total 40 mutants with 0 in
                 scope returns MutationStatus=="out-of-scope" (not "measured" with
                 score 0.00, and not "unmeasurable"); Skeleton with 3 out-of-scope
                 survivors emits exactly one NOTE finding whose text contains "3"
                 and no FLAG per out-of-scope survivor; the prompt test fails if
                 assets/prompts/verify.md's mutation_status branch list omits
                 "out-of-scope" or if it still tells the LLM to apply the
                 0.80/0.60 cutoffs to that status.

  t-5  Wire diff scoping into `dross verify`
       files:    internal/cmd/verify.go, internal/cmd/verify_scope_test.go
       covers:   c-1, c-3, c-6
       description: Derive the scope before verify.Run: merge-base(ch.Base or
                 repo.git_main_branch, HEAD) then `git diff --unified=0` parsed by
                 verify.ParseUnifiedDiff, unioned with the changes.json file list.
                 Call ApplyScope before t.Save. Unconditional — no flag. A failing
                 git leg degrades to the changes.json leg and records a FLAG so the
                 narrowed scope is visible, never silent.
       depends:  t-3, t-4 (t-1 via t-3)
       contract: in a temp repo where the phase branch commit touched a.go and
                 changes.json additionally records b.go, tests.json `scope` lists
                 exactly ["a.go","b.go"]; with a stubbed adapter returning a
                 survivor in c.go (same directory, untouched), verify.toml contains
                 no FLAG naming c.go and tests.json `out_of_scope` contains exactly
                 one entry for it; with an unresolvable base ref the run still
                 completes, scope equals the changes.json list alone, and
                 verify.toml carries a FLAG naming the git failure.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (survivors in-scope only) | t-3, t-5 |
| c-2 (score from in-scope mutants only) | t-2, t-3 |
| c-3 (out-of-scope reported distinctly) | t-4, t-5 |
| c-4 (adapter-agnostic) | t-2, t-3 |
| c-5 (empty in-scope set → explicit status) | t-4 |
| c-6 (records the scoped file set) | t-1, t-3, t-5 |
| c-7 (in-hunk vs inherited tag) | t-1, t-3 |

7/7 criteria covered.

## Judgment calls

- **Per-file counters (`Report.ByFile`) over a full `Mutants []Mutant` list.** c-2 needs killed/timeout attribution, which today's `Report` only has for survivors. A complete mutant list would also work but bloats tests.json by thousands of entries per run; one counter row per file is the smallest thing that lets ApplyScope recompute the denominator.
- **Scoping lives in `internal/verify`, git shell-out stays in `internal/cmd`.** The fenced git argv builders (`gitRefArgs`, `gitTrim`) and their audit test are package-`cmd` only; moving them or duplicating an unfenced exec in `verify` would either widen the change or bypass the fence. So `verify` gets pure parsing (unit-testable, no repo fixture) and `cmd` runs the two git commands.
- **`git diff --unified=0` once, not `merge-base` + `--name-only` + a second hunk pass.** One command yields both the file set (c-6) and the line ranges (c-7); parsing `+++ b/` plus `@@` headers is cheaper than two invocations and cannot disagree with itself about which files changed.
- **No new adapter interface method.** c-4 is satisfied by filtering the normalised `Report` after the fact rather than teaching each adapter to scope itself — one filter, three adapters, and StrykerNet inherits attribution for free because it delegates to `ParseStrykerJSON`.
- **Prompt edit merged into t-4 rather than a sixth task.** The new status constant and the prompt branch that consumes it must ship together; a separate task would land a status the LLM applies 0.60 thresholds to.
- **Rejected: a `--no-scope` escape hatch and a scope-only `dross scope` subcommand.** `scoping_always_on` is locked, and a standalone command satisfies no criterion.
- **Rejected: splitting "record the scope" (c-6) into its own task.** The field is two lines on `Tests` written by the same function that computes the filter; a separate task would be under ten minutes and would leave t-3 writing a struct it can't populate.
