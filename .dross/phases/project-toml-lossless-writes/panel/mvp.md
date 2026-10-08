# Panel draft — MVP lens

Bias applied: smallest task set that satisfies every criterion. Nothing here is
speculative structure; every task is traceable to a criterion.

Verified facts the plan rests on (read from source this session):

- `internal/project/project.go:424` `Save` is the bug: `toml.NewEncoder(f).Encode(p)`
  over the whole struct. Comments and keys with no struct field vanish.
- Writers of an EXISTING project.toml (all call `p.Save`, all mutate the struct
  first): `internal/cmd/project.go:116` (project set / --unset via
  writeDotted/unsetDotted), `internal/cmd/state.go:249` (writeVersion, reached by
  `state set version` and `project set project.version`),
  `internal/cmd/stack.go:124` (stack apply), `internal/cmd/test_lane.go:249,537,654`
  (lane add / edit / remove — `[[runtime.test_lane]]` array-of-tables),
  `internal/cmd/issue.go:230,268` (issue enable / disable — `board.enabled`).
- Fresh-file writers: `internal/cmd/init.go:73`, `internal/cmd/onboard.go:92`
  (whole-file encode is legitimate — no file exists yet).
- No writer produces edits as data; every one mutates `*project.Project` and
  calls `Save`. ~30 `_test.go` sites also call `p.Save(path)` on fresh paths.
- Value shapes reachable through the writers today: string/bool/int scalars,
  `[]string` lists, `map[string]string` (`board.state_map`), struct sub-tables
  (`board.fields`, `mutation.gremlins`, `mutation.stryker`), `[]TestLane`
  array-of-tables (with `[]string` and `[]int` inside). Locked decision
  field_type_scope also names `[[competition]]` / `[[stack.locked]]`; a
  reflect-driven differ covers those with the same code path as test_lane.
- Test helpers that exist: `runCmd(t, Project(), "set", …)`, `chdir`, `mustRead`,
  `Init()`, `State()`, `Stack()`, `Issue()`, `Test()` (→ `lane add|edit|remove`),
  `docText(t, "README.md")`, go/ast pin technique in
  `internal/cmd/hermetic_dross_read_test.go`.
- The repo has no CHANGELOG. `dross ship` builds the PR body from spec.toml
  criteria + decisions (`internal/ship/body.go:20`); README.md is the only
  durable release-notes surface and already has doc-truth tests.

```
Phase project-toml-lossless-writes — 2 tasks across 2 waves

Wave 1
  t-1  Make project.Save a surgical byte patch
       files:    internal/project/patch.go
                 internal/project/patch_test.go
                 internal/project/project.go
       covers:   c-1, c-2, c-6
       description:
         Save(path) keeps its signature. If path does not exist it encodes the
         whole struct exactly as today (init/onboard). If it exists, Save
         Loads the on-disk struct, reflect-diffs it against p using the toml
         tags (omitempty honoured), and turns each differing leaf into a text
         op applied to the raw bytes: SetKey / DeleteKey (scalars + inline
         arrays, honouring the extent of a multi-line array or """ string and
         keeping a trailing `# comment`), SetKey/DeleteKey under a sub-table
         header that is created after the parent table's last line when
         missing (board.state_map, board.fields, runtime.services.<name>,
         constraints), AppendBlock / PatchBlock(i) / DeleteBlock(i) for
         array-of-tables ([[runtime.test_lane]], [[stack.locked]],
         [[competition]]). Inserted lines copy the indent of the nearest
         sibling key (header indent + 2 spaces in an empty table). Values are
         rendered by encoding a one-key map with the existing BurntSushi
         encoder and slicing after "key = " — no new dependency, no
         hand-rolled quoting. A dotted key (`fields.state = ...` under
         [board]) is matched for replace/delete; an inline table value
         (`fields = { ... }`) returns an error naming the key rather than a
         best-effort rewrite (dross never writes that form).
       contract:
         - if Save re-encodes the file, TestSaveScalarChangesExactlyOneLine
           fails: on a fixture mirroring .dross/project.toml (2-space keys, a
           `# comment` inside [runtime], `note = "x"` under [[competition]],
           a `[custom]` table) changing only Project.Version produces a
           line-diff of exactly one line and the comment/note/[custom] survive
         - if list values fall back to whole-file writes,
           TestSaveListReplacesOneValue fails: Stack.Languages = ["go","sql"]
           changes exactly the `languages =` line
         - if the value-extent scanner stops at end-of-line,
           TestSaveMultiLineArrayExtent fails: a fixture `non_goals = [\n
           "a",\n "b",\n]` is replaced without orphaned lines and reloads equal
         - if map tables are rewritten whole, TestSaveStateMapPerKey fails:
           adding StateMap["uat"] to an existing [board.state_map] with two
           other entries adds one line; with no such table, a `[board.state_map]`
           header + one line appear after [board]'s last key and before the
           next header
         - if unset does not delete an omitempty line, TestSaveUnsetDeletesLine
           fails: clearing Stack.Profile removes exactly the `profile =` line;
           clearing a non-omitempty bool (Repo.SquashMerge) rewrites it as
           `false` instead
         - if array-of-tables ops are wrong, one of
           TestSaveArrayTableAppend / TestSaveArrayTablePatchInPlace /
           TestSaveArrayTableDelete fails: append puts a new
           `[[runtime.test_lane]]` block after the last runtime content and
           before [repo]; editing lane[1].Command changes one line inside the
           second block and a hand-added `note` key in that block survives;
           deleting lane[0] removes exactly that block and leaves the others
           byte-identical
         - if the fresh-file branch is lost, TestSaveMissingFileEncodesWhole
           fails: Save to a non-existent path produces the same bytes as
           before this phase (golden from toml.NewEncoder with Indent "  ")
         - if quoting is wrong, TestSaveValueEscapesLikeEncoder fails: a
           description containing `"` and `\` reloads DeepEqual
         - if an inline table is silently clobbered,
           TestSaveInlineTableRefused fails: `fields = { state = "S" }` under
           [board] makes Save return an error naming board.fields and leaves
           the file byte-unchanged
         - every case above also Loads the result and asserts DeepEqual with
           the struct that was saved (semantics, not just bytes)
       depends_on: —
       status: pending

Wave 2 (depends t-1)
  t-2  Prove every writer path and pin it structurally
       files:    internal/cmd/project_lossless_test.go
                 README.md
       covers:   c-3, c-4, c-5
       description:
         One test file: (a) a seeded round-trip through every existing
         project.toml writer at the cobra layer; (b) a go/ast pin that makes
         a future hand-encoding writer red; (c) a doc-truth assertion for the
         README note. README gains one sentence in the `dross project
         {show,get,set}` row (and a `- [x]`-style bullet is NOT added — the
         v1.7 checklist is milestone-owned) stating that `dross project set`
         and `dross state set version` are now surgical/lossless and that
         per-repo bans on them (e.g. codriver.nvim's r-01) are safe to
         retire. The PR body is generated from spec.toml, so the c-5 row
         already appears in the PR verbatim; README is the durable changelog.
       contract:
         - if any writer bypasses the patcher (someone restores an encoder in
           a cmd file), the matching subtest of TestProjectTomlWritersAreLossless
           fails: the test seeds .dross/project.toml from a literal with
           comments, 2-space indent, `note = "x"` under [[competition]] and a
           `[custom]` table, plus a go.mod in the temp dir, then runs each
           of: `project set project.description`, `project set
           stack.languages a,b`, `project set board.state_map.uat X`,
           `project set board.fields.state S`, `project set --unset
           stack.profile`, `state set version 9.9.9.9`, `issue enable`,
           `issue disable`, `test lane add/edit/remove`, `stack apply` — and
           asserts the line-diff against the seed is exactly the expected
           added/removed lines, with every `#` line, the `note` key and the
           `[custom]` table still present
         - if a new writer hand-encodes project.toml,
           TestProjectTomlWritePathIsSingular (go/ast over internal/ non-test
           files) fails: no function outside internal/project/project.go may
           both reference `project.File` and call os.WriteFile / os.Create /
           os.OpenFile / toml.NewEncoder; inside internal/project,
           toml.NewEncoder may appear only in the named fresh-file encode
           function, and that function's only caller is Save
         - if the README note is removed or reworded away from the ban,
           TestReadmeNotesLosslessProjectWrites fails: docText(README.md)
           must contain "safe to retire" and "r-01" in the same sentence as
           "project set"
       depends_on: t-1
       status: pending
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 byte-identical other lines on a scalar write | t-1 (mechanism + unit), t-2 (through every cmd path) |
| c-2 unmodeled key survives every write path | t-1 (`note` under [[competition]], `[custom]` table in fixture), t-2 (same fixture through each cobra writer) |
| c-3 one shared path, new writer cannot regress | t-1 (Save is the only path; writers untouched), t-2 (go/ast pin) |
| c-4 seeded test through each affected path | t-2 |
| c-5 PR/changelog notes bans safe to retire | t-2 (README sentence + doc-truth test; PR body carries the spec row) |
| c-6 lists, tables, array-of-tables covered | t-1 (list / state_map / fields / test_lane / locked / competition ops + tests) |

Every criterion accounted for: 6/6.

## Judgment calls

- **Diff-driven Save (on-disk struct vs. mutated struct) instead of an explicit edit API.** Rejected `project.Patch(path, edits...)`: writeDotted's ~100-arm switch, writeVersion, stack apply, lane add/edit/remove and issue enable/disable would all need rewriting to emit edits. With the differ, zero writer call sites change and c-3 is true by construction — Save is the one path and it is surgical.
- **Save keeps its name and signature; fresh-file encode is the missing-path branch, not a separate `WriteFresh`.** Rejected a second exported function: init/onboard plus ~30 test call sites would churn for no criterion. The go/ast pin in t-2 fences the encoder to that one function instead.
- **Reflect-driven, tag-based differ rather than a hand-listed field table.** A hand list would be a second copy of the struct that drifts (the same shape json_tag_parity_test exists to prevent). Reflection covers [[competition]] and [[stack.locked]] for free, which is what the locked field_type_scope demands even though no writer touches them today.
- **Value text comes from the existing BurntSushi encoder on a one-key map.** Rejected hand-rolled TOML quoting: Go's strconv.Quote emits `\x..` escapes TOML rejects. No new dependency either way; this reuses the one already locked.
- **Inline tables error out; dotted keys are matched but new keys go under explicit headers.** Rejected best-effort rewriting of `fields = { ... }`: it cannot be surgical and dross never writes that form. An explicit refusal that names the key beats a silent clobber, which is exactly the bug being fixed.
- **An emptied table header is left in place after its last key is deleted.** Only `internal/cmd/project.go:403` checks `StateMap == nil`, and it does so to create the map before writing, so an empty-but-present table decodes harmlessly. Deleting headers would need span logic no criterion asks for.
- **onboard's non-adopt overwrite of an existing project.toml becomes a patch.** Semantic state is identical (every differing leaf is patched); the only change is that comments and unknown keys now survive. Accepted rather than special-cased.
- **c-5 lands in README.md, pinned by a doc-truth test.** The repo has no CHANGELOG and the PR body is generated from spec.toml (so the c-5 row is already in the PR). README is the only surface a test can pin. Rejected editing spec.toml to inject a decision and rejected a ship-time `--body-file` step (untestable, easy to forget).
- **Two tasks, not three.** The text patcher and the reflect differ are not separately shippable — the patcher alone changes nothing observable and the differ alone cannot write — so they are one task with its unit tests split by fixture kind. The cobra-layer round-trip (c-4) and the ast pin (c-3) are both tests over t-1's output in a different package, so they share one wave-2 task, and the one-sentence README edit rides with it rather than being a third task under ten minutes long.
