# Planner draft — VERIFICATION LENS

Design rule applied: for each criterion, the ideal test contract was written first,
then the smallest task that makes that contract satisfiable was derived. Every task
below exists because a named test needs it to exist.

## Design the contracts forced

The one design choice the contracts drove, stated up front so the tasks read correctly:

- **`Save` stays the single entry point; it becomes surgical.** Every existing
  project.toml writer (`project set`, `--unset`, `writeVersion`, `issue enable/disable`,
  `lane add/edit/remove`, `stack apply`) already calls `(*Project).Save`
  (`internal/project/project.go:424`). The bug is *inside* Save (whole-struct
  `toml.NewEncoder`). Rewiring Save — rather than adding a second API each writer must
  remember to call — is what makes the c-4 end-to-end test pass **without touching a
  single cmd file**, and it is what makes the c-3 guard's job small: a new writer only
  reintroduces the bug by *not* calling Save, which a go/ast pin can see.
- **Save derives its ops by diffing, not by being told.** Save re-reads the file, `Load`s
  it into an `orig` Project, converts both `orig` and the mutated `p` to generic
  `map[string]any` via `toml.Marshal` → `toml.Unmarshal` (BurntSushi, already a dep —
  locked decision honoured), and diffs the two trees into leaf ops (set / delete /
  add-element / delete-element). Unmodeled keys are in neither tree, so they cannot be
  touched (c-2 by construction). Modeled keys that a writer zeroed appear as deletes,
  matching today's `omitempty` behaviour.
- **Patcher = raw-byte line scanner** (`internal/project/patch.go`): tracks
  `[table]` / `[[array-of-table]]` regions, `key = value` lines, value spans that cross
  lines (arrays, multi-line strings), and comments as opaque lines. It never re-emits a
  line it did not change.
- **File absent → whole-file encode** (init/onboard). That branch is the *only* place the
  old encoder survives, unexported.

---

```
Phase project-toml-lossless-writes — 5 tasks across 3 waves

Wave 1
  t-1  Build raw-byte project.toml patcher
       files:    internal/project/patch.go
                 internal/project/patch_test.go
                 internal/project/testdata/lossless.toml
       covers:   c-1, c-2, c-6
       contract: TestPatchScalarKeepsTrailingComment — patching project.version on a
                 line that carries `# bumped by hand` rewrites only the value span; if
                 the whole line is regenerated the trailing comment is lost and the test
                 fails; TestPatchArraySpanMultiline — replacing goals.non_goals whose
                 original value spans three lines removes all three and emits one inline
                 array; if the scanner stops at the first line the orphaned `"…",` lines
                 make the result undecodable and toml.Decode in the test fails;
                 TestPatchAddKeyMatchesSiblingIndent — inserting paths.source into the
                 empty `[paths]` table yields `  source = "internal"` (2-space indent,
                 copied from the file's other tables) before `[env]`, every other line
                 byte-identical; TestPatchDeleteLeavesNeighbourComment — deleting
                 project.description removes exactly one line and the `# ` comment line
                 above it survives; TestPatchTableHeaderCreated — setting
                 board.state_map.planned on a file with no `[board.state_map]` inserts
                 `  [board.state_map]` + `    planned = "Planned"` after `[board]`'s last
                 key and before `[paths]`; TestPatchTableDeleteKeepsUnmodeledSibling —
                 deleting the last modeled key of a table that still holds a hand-added
                 key leaves the header and that key in place (c-2), whereas an otherwise
                 empty table loses its header; TestPatchArrayOfTablesAppendAndDelete —
                 appending a `[[competition]]` element puts the new block after the last
                 existing one; deleting element 0 removes exactly that block's lines and
                 the `note = "hand-added"` under element 1 survives byte-for-byte;
                 TestPatchRefusesInlineTable — an op targeting a key inside an inline
                 `{ … }` value returns a named error and the returned bytes equal the
                 input; TestPatchQuotesViaEncoder — a string containing `"` and `\`
                 written through BurntSushi's single-value marshal reads back equal via
                 toml.Decode. Every case additionally asserts `toml.Decode(after)` succeeds
                 and the decoded value at the op path equals the op value.

  t-2  Pin project.toml writers to Save via go/ast
       files:    internal/cmd/project_toml_writer_pin_test.go
       covers:   c-3
       contract: TestDetectsDirectProjectTomlWrite — a synthetic non-test snippet calling
                 `os.WriteFile(filepath.Join(root, project.File), …)` (also os.Create /
                 os.OpenFile, and the literal "project.toml") is reported by file:line;
                 TestDetectsStrayProjectEncoder — a snippet outside internal/project
                 calling `toml.NewEncoder(f).Encode(p)` with any receiver other than
                 `os.Stdout` is reported; TestPermitsSaveAndStdoutEncode — `p.Save(path)`
                 and `toml.NewEncoder(os.Stdout).Encode(p)` (projectShow) produce zero
                 findings; TestNoWriterBypassesProjectSave — walks every non-test .go under
                 internal/ except internal/project/project.go and requires zero findings,
                 so a future writer that opens project.toml itself reddens the build. The
                 FLAG/PASS snippet table is the same self-check idiom as
                 hermetic_dross_read_test.go, so the guard cannot pass vacuously.

Wave 2 (depends t-1)
  t-4  Rewire Save to diff-and-patch existing files
       files:    internal/project/project.go
                 internal/project/save_lossless_test.go
       covers:   c-1, c-2, c-3, c-6
       contract: TestSaveNoopIsByteIdentical — Load then Save with no mutation leaves the
                 fixture (comments, unmodeled keys, 2-space indent, multi-line array)
                 byte-identical; a spurious op from the diff layer (int64 vs int, nil vs
                 empty slice, bool zero) shows up here as a changed line;
                 TestSaveSingleFieldChangesOneLine — setting Project.Version changes
                 exactly one line; TestSaveOmitemptyZeroDeletesLine — Board.Enabled=false
                 removes exactly the `enabled = true` line and nothing else;
                 TestSaveArrayOfTablesAlignment — with three `[[runtime.test_lane]]`
                 blocks: append → one new block after the third; edit lane 1's Command
                 (length unchanged → positional alignment) → only that `command =` line
                 changes and a comment inside lane 1's block survives; remove lane 0 → only
                 lane 0's block is gone; TestSaveMapTablesAreLeafOps — adding
                 Runtime.Services["db"] appends a `[runtime.services.db]` subtable inside
                 the runtime region and changing Constraints["hosting"] touches one line;
                 TestSaveAbsentFileEncodesWhole — Save to a path that does not exist
                 produces a complete file (TestRoundTrip in project_test.go keeps passing
                 unchanged); TestSavePatchErrorLeavesFileUntouched — when the patcher
                 refuses (inline-table target) the on-disk bytes are unchanged, i.e. the
                 write happens only after a successful patch, never truncate-then-fail.

Wave 3 (depends t-4)
  t-5  Prove every writer path is lossless end-to-end
       files:    internal/cmd/project_toml_lossless_test.go
       covers:   c-4, c-1, c-2, c-6
       contract: One fixture: the real .dross/project.toml shape plus a leading `# ` comment,
                 a mid-table comment, a trailing same-line comment, a hand-added
                 `note = "…"` under `[[competition]]`, an unmodeled `[custom]` table, and a
                 multi-line `non_goals` array. Each subtest runs the real cobra command
                 in-process (runCmd, as the suite does) and asserts via a shared
                 onlyLinesChanged(before, after, wantKeys) helper that the set of
                 added/removed lines is exactly the targeted key(s):
                 `project set runtime.test_command` → 1 line;
                 `project set stack.languages go,toml` → 1 line (array, c-6);
                 `project set remote.reviewers a,b` (key absent) → 1 added line inside
                 [remote]; `project set board.state_map.planned X` (table absent) → header
                 + 1 line (table, c-6); `project set board.fields.state Status` → same;
                 `project set --unset project.description` → 1 removed line, its comment
                 kept; `state set version 1.7.5.1` → only project.toml's version line and
                 state.json change; `issue enable` / `issue disable` → the `enabled` line
                 only; `lane add` / `lane edit --command` / `lane remove` → exactly the
                 affected `[[runtime.test_lane]]` block (array-of-tables, c-6);
                 `stack apply` (fixture repo carries go.mod → go profile) → only the
                 [runtime] keys the profile sets plus `profile = "go"`. A shared
                 assertion in every subtest requires the `note` key, the `[custom]` table
                 and every `# ` comment line to survive (c-2) — if any writer round-trips
                 the whole struct, that assertion is the one that reddens.

  t-3  Document the fix and retire the per-repo bans
       files:    README.md
                 ARCHITECTURE.md
                 internal/cmd/lossless_docs_test.go
       covers:   c-5
       contract: TestReadmeRetiresProjectSetBans — README.md's `### Updating` section
                 contains a subsection that names both `dross project set` and
                 `dross state set version` and says a per-repo ban on them (codriver.nvim's
                 r-01 cited as the example) can be retired; deleting or renaming the
                 subsection fails the test. TestArchitectureNamesLosslessWriter —
                 ARCHITECTURE.md's project-config section names `Save` as the sole
                 project.toml writer and the raw-byte patcher, with `internal/project/
                 patch.go` file:line pointers (the same pin shape as
                 TestArchitectureNamesHermeticGuard). The PR body itself needs no task:
                 ship.BuildPRBody (internal/ship/body.go:37) renders every spec criterion,
                 so c-5's own wording lands in the PR verbatim; the README note is what
                 outlives the PR.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 (line-level byte assertions per shape), t-4 (no-op Save byte-identical), t-5 (per-command onlyLinesChanged) |
| c-2 | t-1 (TestPatchTableDeleteKeepsUnmodeledSibling, competition `note` survives), t-4 (diff over modeled trees only), t-5 (shared note/[custom]/comment assertion on every path) |
| c-3 | t-2 (go/ast pin: no writer bypasses Save), t-4 (Save is the one surgical path; whole-encode reachable only when the file is absent) |
| c-4 | t-5 |
| c-5 | t-3 |
| c-6 | t-1 (arrays, tables, array-of-tables cases), t-4 (test_lane alignment, services/constraints maps), t-5 (`stack.languages`, `board.state_map`, `board.fields`, `lane *`) |

All 6 criteria covered; every task carries at least one named test.

## Judgment calls

- **Diff-derived ops inside Save** over an explicit `SetField(path, value)` API that each writer calls: chosen because it lets t-5 pass with zero cmd-file edits and makes t-2's pin a one-rule check ("do you call Save?"); rejected the explicit API because `lane add/edit/remove` and `stack apply` mutate the struct in several places and would each need bespoke op construction — more writer code, more surfaces to pin.
- **Generic-map diff via `toml.Marshal`/`Unmarshal`** over a reflect walk of `Project`'s toml tags: chosen because it reuses the dependency already locked in (no new TOML library, no second tag parser) and gets `omitempty` semantics for free; rejected the reflect walk as a parallel schema interpreter that would drift from BurntSushi's on the next tag edit.
- **Array-of-tables alignment = positional when lengths match, set-difference by deep equality when they differ**: chosen because it is exactly the three shapes the current writers produce (edit in place, append one, remove one) and is fully pinned by TestSaveArrayOfTablesAlignment; rejected an identity-key heuristic (match by `name`/`choice`) as more code for a case no writer produces, and rejected pure equality-diff because an edit would then delete-and-reappend the block, losing its comments and position.
- **Table header removal only when the region has no remaining keys** (modeled or not): chosen so c-2 holds for a hand-added key that outlives every modeled sibling; rejected "always drop the header on last modeled delete" because it would silently orphan the unmodeled key under the next table.
- **Inline tables and keys inside them are refused, not rewritten**: BurntSushi never emits them for `Project`, so they only appear by hand-edit; refusing with a named error and leaving the file untouched is provable (TestPatchRefusesInlineTable, TestSavePatchErrorLeavesFileUntouched) where a partial rewrite is not. Scope to note for the judge: this is the one input shape the patcher declines.
- **Write-after-successful-patch** (patch in memory, then `os.WriteFile`) over the existing `os.Create`-then-encode: chosen because TestSavePatchErrorLeavesFileUntouched is only satisfiable if the file is never truncated before the new bytes exist.
- **c-5 lives in README `### Updating` + ARCHITECTURE.md, pinned by test**, not a new CHANGELOG.md: follows the locked precedent from adopter-upgrade-note ("the upgrade note goes in README.md's Updating section, not a new CHANGELOG.md"); the PR body carries the criterion text automatically via BuildPRBody. Pinning prose with a test is the repo's existing practice (lane_install_docs_test.go).
- **t-2 in wave 1, not after t-4**: the pin constrains source shape (call Save, never open the file yourself) and is true of the tree today; it is independent of whether Save is yet surgical. Putting it first also means t-4 and t-5 are executed under the guard rather than before it.
- **t-1 kept as one task despite its size**: one file, one layer, and the locked `field_type_scope` decision forbids shipping a scalar-only patcher; splitting by shape would put two tasks on the same file in consecutive waves for no test-contract gain.
