# Panel synthesis — project-toml-lossless-writes

Judge: cold. Authored none of the three drafts. Every file path, symbol and line
number the drafts cite was checked against source before scoring; all exist
(`(*Project).Save` internal/project/project.go:424, `writeDotted`/`unsetDotted`
internal/cmd/project.go:350/613, `writeVersion` internal/cmd/state.go:239, the
writer call sites in stack.go/test_lane.go/issue.go/init.go/onboard.go,
`hermetic_dross_read_test.go`, `ship.BuildPRBody` internal/ship/body.go:20,
README `### Updating` at line 148, ARCHITECTURE.md, the adopter-upgrade-note
locked decision "README Updating section, not a new CHANGELOG.md"). No draft
loses points for a phantom reference.

One source fact that decides a divergence below: `dross ship --body-file`
**replaces** the generated body wholesale (internal/cmd/ship.go:211-217) — it
does not append to `BuildPRBody`'s criteria table.

## Scores

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| risk (7 tasks / 4 waves) | 6/6, each criterion hit by ≥2 tasks; c-4/c-5 single-owner as they should be | Highest: every task names failing tests with the exact input that reddens them; covers file-layout hazards (CRLF, EOF newline, stale offsets, refused Load, mtime) no other lens saw | Slightly over-split: index and apply are one layer (apply re-indexes after every op) and land as separate commits with the first unobservable; otherwise every task is a real commit | Correct. t-1 ‖ t-2 → t-3 → t-4 → {t-5, t-6} → t-7. Pin after Save is justified by its clause (c) (encodeFresh is the only encoder), which is false until Save is rewired |
| mvp (2 tasks / 2 waves) | 6/6 with the best verified-facts preamble (all 11 line numbers correct) | Good: named tests per shape, DeepEqual-after-reload on every case | Too coarse: t-1 is index + differ + apply + Save rewire in one atomic commit; t-2 is three unrelated concerns (e2e proof, go/ast pin, README) in one commit — a red in any one blocks all three | Trivially correct (a chain of two). No parallelism where there is obvious parallelism (patcher ‖ differ ‖ pin) |
| verification (5 tasks / 3 waves) | 6/6; c-2 argued "by construction" (unmodeled keys are in neither generic tree) and also tested | High: contracts written first; unique catches — `TestSaveNoopIsByteIdentical` (int64/nil-slice spurious ops), `TestSaveMapTablesAreLeafOps` (services/constraints), `TestArchitectureNamesLosslessWriter`; array-of-tables alignment that keeps sibling blocks byte-identical | Right-sized: patcher, pin, Save rewire, e2e, docs. Differ folded into Save rather than its own task (loses risk's stand-alone differ contract) | Mostly correct; pin in wave 1 is defensible but its "encoder only in the fresh-file function" clause cannot be green until Save is rewired, so it would ship red or weakened. Docs placed parallel to e2e rather than after it |

**Skeleton: verification.** It has the right task boundaries (one file/layer per
task, every task exists because a named test needs it), the design choice that
best serves c-1/c-2 on array-of-tables (positional-when-equal, identity-when-
lengths-differ — the only alignment under which removing a middle `[[…]]` block
leaves its siblings and their hand-added keys byte-identical), and a differ that
inherits `omitempty` from the locked dependency instead of re-implementing tag
semantics. Risk supplies the deepest contracts and the safety nets around the
tracked file (verify-before-swap, atomic rename, refused-Load-never-overwrites),
which are grafted in wholesale. MVP supplies the verified writer inventory and
the `stack apply` / go.mod fixture detail for the e2e matrix.

## Merged plan

Architecture (agreed by all three lenses, stated once):

- `Save(path)` keeps its signature and stays the **only** project.toml writer.
  Path absent → whole-file encode via unexported `encodeFresh` (init/onboard,
  ~30 test call sites unchanged). Path present → read bytes, `Load` into `old`,
  `diff(old, p)` → ops, `apply(bytes, ops)` → patched, verify, atomic swap.
  Zero cmd-file edits; every existing writer becomes lossless by construction.
- No new TOML dependency (locked). BurntSushi is used for reads/validation, for
  rendering single values (one-key map encode, slice after `k = `), and — in
  the differ — for producing the two generic trees to compare.
- `op = {table []string, elem int /* -1 unless array-of-tables */, key string, value any /* nil = delete */}`
  plus element-level append/delete for array-of-tables.
- Shapes covered (locked field_type_scope): scalar (string/int/bool),
  `[]scalar` (`stack.languages`, `remote.reviewers`, `goals.non_goals`, lane
  `[]string`/`[]int`), table-of-scalars (`[board.state_map]`, `[constraints]`),
  struct table (`[board.fields]`, `[mutation.gremlins]`, `[mutation.stryker]`),
  table-of-tables (`[runtime.services.<name>]`), array-of-tables
  (`[[stack.locked]]`, `[[runtime.test_lane]]`, `[[competition]]`).
- Inline tables (`x = { … }`) are refused by key and line number, never
  rewritten. Dotted keys under a header (`fields.state = …` inside `[board]`)
  are matched for replace/delete; new keys always go under explicit headers.

```
Phase project-toml-lossless-writes — 6 tasks across 4 waves

Wave 1
  t-1  Build the raw-byte project.toml patcher (index + apply)
       origin:   [risk t-1 + t-3, verification t-1, mvp t-1 (patch half)]
       files:    internal/project/patch.go
                 internal/project/patch_value.go
                 internal/project/patch_test.go
                 internal/project/testdata/lossless.toml
       covers:   c-1, c-2, c-6
       description:
         Two pure functions in one layer. `indexDoc(src []byte) (*doc, error)`
         classifies every line as header (table / array-of-tables, with dotted
         path and running element index), key line (indent, full key path incl.
         quoted and dotted keys, value start offset, value END line — multi-line
         arrays and """ / ''' strings span lines), comment, or blank; records
         the file's EOL ("\n" vs "\r\n") and whether the last line is
         newline-terminated. Inline tables `{...}` are indexed as a value span
         but flagged so apply can refuse them by line number.
         `apply(src []byte, ops []op) ([]byte, error)` re-indexes after every
         op so offsets never go stale. Set on an existing key replaces ONLY the
         value span — indent, key spelling and a trailing `# comment` on that
         line survive; a multi-line value is replaced whole (all its lines
         removed, one inline value emitted). Set on a missing key inserts after
         the table's last key line (before trailing blanks/comments), copying
         the sibling indent (header indent + the file's indent unit when the
         table is empty). Missing table / array-of-tables element → new header
         block placed after the last header sharing the parent prefix, else
         EOF (a missing final newline is added first). Delete key → remove the
         line; delete array-of-tables element → remove its header-to-next-
         header span; a table header whose span is left with no keys (modeled
         or not) AND no comments is removed too — a header sheltering a
         hand-added key or a comment stays. `renderValue(v any) string`
         produces the TOML value text by encoding a one-key map through
         BurntSushi's encoder and stripping the `k = ` prefix, so escaping is
         byte-for-byte what today's files carry. An inline-table spelling at
         the target is refused with the key, the line number and the
         `[board.state_map]`-style rewrite named — never partially rewritten;
         the returned bytes equal the input.
       contract:
         - if the value scanner stops at the `#` inside `desc = "a # b"  # real`,
           TestIndexValueSpanIgnoresHashInsideStrings fails (span must end
           before `  # real`)
         - if a multi-line `non_goals = [\n "a", # c\n "b",\n]` is not indexed
           as one 4-line value span, TestIndexMultilineArraySpan fails
         - if a `"""` string containing `]` and `=` is split,
           TestIndexMultilineBasicString fails
         - if `[[stack.locked]]` elements are not numbered 0..3 in file order,
           TestIndexArrayOfTablesElementIndex fails
         - if a CRLF file is indexed with EOL "\n", TestIndexRecordsCRLF fails
         - if `"fix versions" = "x"` (quoted key) or `state_map.planned = "x"`
           (dotted key inside [board]) is not resolved to its full key path,
           TestIndexQuotedAndDottedKeys fails
         - if replacing `version = "1.7.5.0"  # bumped` loses the trailing
           comment or the 2-space indent,
           TestApplyReplaceKeepsIndentAndTrailingComment fails
         - if replacing a three-line `goals.non_goals` array leaves orphaned
           `"…",` lines, TestApplyMultilineArrayReplacedWhole fails (the
           output must toml.Decode)
         - if inserting `profile = "go"` into [stack] lands after
           `[[stack.locked]]` (outside the table's direct-key span),
           TestApplyInsertStaysInsideTableSpan fails (re-Load would attribute
           it to the wrong table)
         - if inserting `paths.source` into the empty `[paths]` of the repo-
           shaped fixture does not yield `  source = "internal"` before `[env]`
           with every other line byte-identical,
           TestApplyAddKeyMatchesSiblingIndent fails
         - if a new `[board.state_map]` on the repo's own project.toml shape is
           not emitted as `  [board.state_map]` / `    planned = …` after
           [board]'s last key and before `[paths]`,
           TestApplyNewSubtableMatchesEncoderLayout fails
         - if two set ops on the same table land on the same line (stale
           offsets), TestApplyTwoOpsSameTableBothLand fails
         - if deleting `project.description` removes more than one line or
           the `# ` comment line above it, TestApplyDeleteLeavesNeighbourComment
           fails
         - if deleting the last state_map key leaves an empty
           `[board.state_map]` header, TestApplyDeleteLastKeyRemovesEmptyHeader
           fails — and if the header held a comment and was removed anyway,
           TestApplyKeepsHeaderThatHoldsAComment fails — and if the header held
           a hand-added (unmodeled) key and was removed anyway,
           TestApplyKeepsHeaderThatHoldsUnmodeledKey fails (c-2)
         - if appending a fifth `[[stack.locked]]` block lands at EOF instead of
           after the fourth, TestApplyAppendArrayElementAfterLastSibling fails
         - if deleting `[[competition]]` element 0 removes anything but that
           block's lines, or the `note = "hand-added"` under element 1 is not
           byte-for-byte intact, TestApplyDeleteArrayElementKeepsSiblings fails
         - if `renderValue("a\"b\\c\n")` differs from what toml.NewEncoder
           writes for the same string, TestRenderValueMatchesEncoder fails
         - if a CRLF source gets an inserted line ending in bare "\n",
           TestApplyPreservesCRLF fails
         - if a file with no trailing newline gets a new table glued onto its
           last line, TestApplyEOFNewline fails
         - if an inline-table target is silently rewritten instead of refused
           with its key and line number, and the returned bytes differing from
           the input, TestApplyRefusesInlineTableByLine fails
         - every apply test additionally asserts `toml.Decode(after)` succeeds
           and the decoded value at the op path equals the op value
       depends_on: —

  t-2  Diff two Projects into shape-typed ops
       origin:   [risk t-2 (task + contracts), verification t-4 (generic-map design)]
       files:    internal/project/patch_diff.go
                 internal/project/patch_diff_test.go
       covers:   c-3, c-6
       description:
         `diff(old, new *Project) ([]op, error)`. Each side is converted to a
         generic tree (`toml.Marshal` → `toml.Unmarshal` into
         `map[string]any`) so `omitempty`, `toml:"-"` and every other tag
         semantic come from the locked encoder, not a second tag interpreter.
         The two trees are walked together and ops are emitted only for
         leaves that differ. Key present in old and absent in new (an
         omitempty leaf that became zero) → delete op; present in new and
         absent in old → set op (insert); a non-omitempty leaf that became
         zero is present in both with a different value → set-to-zero-literal
         op. Tables and maps → per-key set/delete, never a whole-table
         replace. Array-of-tables ([]map[string]any): when lengths match,
         align positionally and diff element i vs element i field-wise; when
         lengths differ, match elements by deep equality, emit delete-element
         for unmatched old elements and append-element (whole block) for
         unmatched new ones. Any node kind the walker cannot classify returns
         an error naming the path — never a silent skip. Types are consistent
         across both trees (both come from the same Marshal→Unmarshal), so
         int64-vs-int and nil-vs-empty-slice cannot produce spurious ops.
       contract:
         - if the differ emits an op for an unchanged field,
           TestDiffNoChangeIsNoOps fails (Load of the tracked
           .dross/project.toml diffed against itself → zero ops)
         - if setting Project.Version alone yields anything but one scalar op
           at table [project] key version, TestDiffScalarChangeIsOneOp fails
         - if clearing Stack.Profile (omitempty) emits a set-"" op instead of a
           delete, TestDiffOmitemptyZeroIsDelete fails
         - if clearing Repo.Layout (no omitempty) emits a delete instead of
           `layout = ""`, or Repo.SquashMerge=false emits a delete instead of
           `squash_merge = false`, TestDiffRequiredZeroIsSetLiteral fails
         - if deleting one state_map key yields a whole-map op,
           TestDiffMapDeleteIsPerKey fails; if adding Runtime.Services["db"]
           yields anything but per-key set ops under table
           [runtime.services.db], the same test fails
         - if changing lane 1's Command among three lanes yields anything but
           one set op at elem 1 key command, TestDiffArrayOfTablesEditInPlace
           fails
         - if removing the middle of three test lanes yields anything but one
           delete-element op for elem 1 (zero ops touching elems 0 and 2),
           TestDiffArrayOfTablesShrinkRemovesOnlyThatElement fails
         - if adding a fourth lane yields anything but one append-element op,
           TestDiffArrayOfTablesGrowAppends fails
         - if a walk of the generic tree of a fully-populated Project{} (every
           field set, every map/slice non-empty, every array-of-tables with two
           elements) hits a node kind the differ cannot classify,
           TestDiffCoversEveryProjectValueKind fails — so a future field of a
           new shape reddens the suite the day it lands
       depends_on: —

Wave 2 (depends t-1, t-2)
  t-3  Make Save diff-driven, verified, atomic
       origin:   [risk t-4 (verify-before-swap, atomic swap, refused-Load rule, no-op rule),
                  verification t-4 (contracts), mvp t-1 (Save half)]
       files:    internal/project/project.go
                 internal/project/project_test.go
                 internal/project/save_lossless_test.go
       covers:   c-1, c-2, c-3, c-6
       description:
         `Save`: stat path; absent → `encodeFresh` (the ONLY toml.NewEncoder
         call site left in the package, Indent "  " as today); present →
         ReadFile, Load (a decode refusal — including the remote_host trap —
         returns the error, never falls back to whole-file encode), diff,
         apply, then VERIFY: `encodeFresh(Load(patched))` must equal
         `encodeFresh(p)` (canonical re-encode, so nil-vs-empty-slice and map
         order cannot false-positive); on mismatch return an error naming the
         first differing key and leave the file untouched. Write via temp file
         in the same dir + fsync + rename, preserving the original mode. Zero
         ops → no write at all (mtime untouched). onboard's non-adopt overwrite
         of an existing project.toml becomes a patch as a consequence (same
         semantic result; comments and unknown keys now survive).
       contract:
         - if a patcher bug yields a document that decodes differently from p,
           TestSaveRefusesUnverifiedPatch (injects an op whose rendered value
           is mis-typed) fails because the original bytes were replaced
         - if Save on a file whose Load is refused (remote_host set) overwrites
           it instead of erroring, TestSaveNeverOverwritesARefusedFile fails
         - if Load then Save with no mutation changes a single byte of the
           fixture (comments, unmodeled keys, 2-space indent, multi-line
           array), TestSaveNoopIsByteIdentical fails — a spurious op from the
           diff layer (int64 vs int, nil vs empty slice, bool zero) shows up
           here as a changed line
         - if a Save with no struct change rewrites the file,
           TestSaveNoChangeLeavesMtime fails
         - if setting Project.Version changes more than exactly one line,
           TestSaveSingleFieldChangesOneLine fails
         - if Board.Enabled=false does not remove exactly the `enabled = true`
           line and nothing else, TestSaveOmitemptyZeroDeletesLine fails; if
           clearing Stack.Profile does not remove exactly the `profile =`
           line, or Repo.SquashMerge=false does not rewrite it as `false` in
           place, TestSaveUnsetDeletesLine fails
         - if Stack.Languages = ["go","sql"] changes anything but the
           `languages =` line, TestSaveListReplacesOneValue fails
         - if adding StateMap["uat"] to an existing [board.state_map] with two
           entries adds more than one line, or with no such table does not
           produce `[board.state_map]` + one line after [board]'s last key and
           before the next header, TestSaveStateMapPerKey fails
         - if adding Runtime.Services["db"] does not append a
           `[runtime.services.db]` subtable inside the runtime region, or
           changing Constraints["hosting"] touches more than one line,
           TestSaveMapTablesAreLeafOps fails
         - with three `[[runtime.test_lane]]` blocks: if append does not add
           one new block after the third; if editing lane 1's Command changes
           anything but that `command =` line or a comment / hand-added `note`
           inside lane 1's block does not survive; if removing lane 0 removes
           anything but lane 0's block — TestSaveArrayOfTablesAlignment fails
         - if an apply/verify error mid-way leaves a truncated or altered
           project.toml (today's os.Create shape), TestSaveFailureLeavesOriginalBytes
           fails (inline-table target is the injected refusal; on-disk bytes
           must equal the input)
         - if Save on a missing path stops producing today's encoder output,
           the existing TestRoundTrip, TestSaveDefaultsAreOmittedAndOptionalsRemainEmpty
           and TestNoTestLaneIsAbsentFromTheDocument fail
         - if a 0-byte existing file cannot be patched into a Load-equal
           document, TestSaveOntoEmptyFile fails
         - every case above also Loads the result and asserts canonical
           equality with the struct that was saved (semantics, not just bytes)
       depends_on: t-1, t-2

Wave 3 (depends t-3)
  t-4  Prove every writer path is lossless end to end
       origin:   [risk t-5 (fixture, changedLines helper, real-project one-line diff),
                  verification t-5 (writer matrix incl. stack apply, absent-key insert,
                  --unset keeps comment), mvp t-2a (go.mod in temp root)]
       files:    internal/cmd/project_lossless_test.go
       covers:   c-1, c-2, c-4, c-6
       description:
         One fixture project.toml seeded with: a leading `# ` header comment, a
         mid-table comment inside [runtime], an inline `# why` comment after
         `version`, `note = "hand-added"` under `[[competition]]`, a `note`
         under `[[stack.locked]]`, an unmodeled `[custom]` table, an unmodeled
         key in `[remote]`, a multi-line `non_goals = [ … ]`, three
         `[[runtime.test_lane]]` blocks (one carrying a comment), and 2-space
         indentation; a go.mod is written beside it so `stack apply` resolves
         the go profile. A shared helper `onlyLinesChanged(t, before, after,
         wantAdded, wantRemoved)` asserts the set of added/removed lines is
         exactly the targeted key(s). Each subtest runs the real cobra command
         in-process (`runCmd`, `chdir`, `mustRead` from cmd_test.go) in a
         t.TempDir root and, after every write, a shared assertion requires
         both `note` keys, the `[custom]` table, the unmodeled [remote] key and
         every `# ` comment line to survive (c-2). Writer matrix:
         `state set version 1.7.5.1` (writeVersion) → only the `version =` line
         of project.toml (state.json changes too, that is expected);
         `project set runtime.test_command …` → 1 line;
         `project set stack.languages go,toml` → 1 line (array, c-6), the
         multi-line non_goals untouched;
         `project set remote.reviewers a,b` (key absent) → 1 added line inside
         [remote];
         `project set board.state_map.planned X` (table absent) → header + 1
         line placed after [board]'s last key (table, c-6);
         `project set board.fields.state Status` → same shape;
         `project set --unset project.description` → 1 removed line, its
         neighbouring comment kept;
         `project set --unset board.state_map.<last key>` → the key line and
         the now-empty header removed, nothing else;
         `issue enable` / `issue disable` → the `enabled` line only (bool);
         `test lane add` → exactly one new `[[runtime.test_lane]]` block after
         the third; `test lane edit --command` on lane 2 → exactly that
         `command =` line, the comment in that block intact; `test lane
         remove` on the middle of three → exactly that block, lanes 1 and 3
         byte-identical (array-of-tables, c-6);
         `stack apply` → only the [runtime] keys the go profile sets plus
         `profile = "go"`.
         Plus: a copy of the repo's own tracked `.dross/project.toml`
         (hermetic-rule-safe: a tracked file named in the same expression) with
         only the version bumped through `state set version`.
       contract:
         - if `state set version` touches any line but the `version =` line of
           the fixture, TestLosslessStateSetVersion fails on the changed-lines
           set
         - if either `note`, the `[custom]` table, the unmodeled [remote] key
           or any `# ` comment line is missing after ANY write in the matrix,
           TestLosslessUnmodeledKeysSurviveEveryWriter fails (it runs the whole
           matrix and greps for each after every step) — if any writer
           round-trips the whole struct, this is the assertion that reddens
         - if `project set stack.languages go,toml` rewrites the multi-line
           `non_goals` array or anything outside its own line,
           TestLosslessArrayWriteIsOneLine fails
         - if `project set remote.reviewers a,b` on an absent key adds more
           than one line or adds it outside [remote],
           TestLosslessAbsentKeyInsertsInsideTable fails
         - if `project set board.state_map.planned X` / `board.fields.state
           Status` on an absent sub-table produces anything but header + one
           line after [board]'s last key, TestLosslessSubtableCreatedOnFirstWrite
           fails
         - if `project set --unset project.description` removes the comment
           beside it, or `--unset` of the last state_map key leaves the empty
           header, TestLosslessUnsetIsOneLine fails
         - if `issue enable` / `issue disable` change anything but the
           `enabled` line, TestLosslessIssueToggle fails
         - if `test lane remove` on the middle of three lanes touches lane 1's
           or lane 3's block, TestLosslessLaneRemoveLeavesSiblings fails; if
           `lane edit` touches more than its `command =` line or drops the
           block's comment, or `lane add` adds anything but one block after
           the last, TestLosslessLaneAddEditOneBlock fails
         - if `stack apply` touches anything outside the [runtime] keys the
           profile sets and `stack.profile`, TestLosslessStackApply fails
         - if bumping the version on the real project.toml copy yields a diff
           of more than one line (indentation of `  [[stack.locked]]` or the
           empty `[paths]`/`[env]` headers re-rendered),
           TestLosslessRealProjectTomlOneLineDiff fails
       depends_on: t-3

  t-5  Pin the single project.toml write path
       origin:   [risk t-6, verification t-2, mvp t-2b]
       files:    internal/cmd/project_writer_pin_test.go
       covers:   c-3
       description:
         go/ast scan (same technique and FLAG/PASS snippet-table self-check as
         hermetic_dross_read_test.go) of every non-test .go file under
         internal/: (a) outside internal/project, any os.WriteFile / os.Create
         / os.OpenFile / os.Rename call whose path argument mentions
         `project.File` or the literal "project.toml" — including the two-step
         `p := filepath.Join(root, project.File); os.WriteFile(p, …)` shape —
         is a violation; (b) outside internal/project, any toml.NewEncoder /
         toml.Marshal whose argument expression is not os.Stdout and whose
         enclosing file references `project.File` is a violation; (c) inside
         internal/project, exactly one toml.NewEncoder call site exists, its
         enclosing FuncDecl is `encodeFresh`, and `encodeFresh`'s only callers
         are `Save` and the verify step. Violations are computed by a pure
         function over source text so the teeth are proven on synthetic input
         and the guard cannot pass vacuously.
       contract:
         - if a new cmd file does `os.WriteFile(filepath.Join(root,
           project.File), …)`, TestProjectTomlHasOneWriter fails naming the
           file:line
         - if someone adds a second toml.NewEncoder in internal/project (a
           re-encode fallback), TestEncodeFreshIsTheOnlyEncoder fails
         - if the detector misses the `p := filepath.Join(root, project.File);
           os.WriteFile(p, …)` two-step shape or the literal "project.toml",
           TestWriterPinCatchesIndirectPath (synthetic source) fails
         - if a snippet outside internal/project calling
           `toml.NewEncoder(f).Encode(p)` with any receiver other than
           os.Stdout is not reported, TestDetectsStrayProjectEncoder fails
         - if it flags projectShow's `toml.NewEncoder(os.Stdout)` or a plain
           `p.Save(path)`, TestWriterPinAllowsStdoutEncode fails
         - if any non-test .go under internal/ (except internal/project) has a
           finding today, TestNoWriterBypassesProjectSave fails
       depends_on: t-3

Wave 4 (depends t-4)
  t-6  Document the fix and retire the per-repo bans
       origin:   [verification t-3 (README Updating + ARCHITECTURE pin),
                  risk t-7 (wording + waits for the proving tests), mvp t-2c]
       files:    README.md
                 ARCHITECTURE.md
                 internal/cmd/lossless_docs_test.go
       covers:   c-5
       description:
         README's `### Updating` section gains a short subsection stating that
         `dross project set` and `dross state set version` are now surgical —
         comments and unknown keys survive, only the targeted line changes —
         so per-repo rules banning them (e.g. codriver.nvim's r-01) are safe to
         retire. The `dross project {show,get,set}` table row is NOT also
         edited (one location, one pin, no drift). ARCHITECTURE.md's
         project-config section names `Save` as the sole project.toml writer
         and the raw-byte patcher, with `internal/project/patch.go` file:line
         pointers (same pin shape as TestArchitectureNamesHermeticGuard). No
         PR-body task: `ship.BuildPRBody` (internal/ship/body.go:20) renders
         every spec criterion verbatim, so c-5's own wording lands in the PR;
         the README note is what outlives the PR. Lands after t-4 so the
         lossless claim is written only once the tests that prove it exist.
       contract:
         - if README.md's `### Updating` section no longer contains a
           subsection naming both `dross project set` and `dross state set
           version` together with "safe to retire" and "r-01",
           TestReadmeRetiresProjectSetBans fails (docText(t, "README.md"));
           deleting or renaming the subsection fails the test
         - if ARCHITECTURE.md's project-config section stops naming `Save` as
           the sole writer and `internal/project/patch.go` with a valid
           file:line pointer, TestArchitectureNamesLosslessWriter fails
       depends_on: t-4
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 byte-identical other lines | t-1, t-3, t-4 |
| c-2 unschematized keys survive every path | t-1, t-2 (by construction: unmodeled keys are in neither generic tree), t-3, t-4 |
| c-3 one shared write path, new writers can't regress | t-2, t-3, t-5 |
| c-4 seeded comments+unmodeled key test per affected path | t-4 |
| c-5 PR/changelog notes bans retirable | t-6 |
| c-6 arrays, tables, array-of-tables covered | t-1, t-2, t-3, t-4 |

## Disagreements

1. **Differ mechanism: reflect walk over `toml` tags vs generic-tree diff via BurntSushi Marshal→Unmarshal.**
   Risk and mvp: reflect walk honouring `omitempty` by hand, with
   `TestDiffCoversEveryProjectLeafKind` reddening on a new Go kind.
   Verification: marshal both sides through the locked encoder into
   `map[string]any` and diff the trees — omitempty and every tag semantic come
   from BurntSushi; no second tag interpreter to drift.
   **Default: generic-tree (verification).** Why it matters: the reflect walk
   re-implements `omitempty`/`toml:"-"`/TextMarshaler semantics that
   `json_tag_parity_test` already exists to stop drifting; the generic tree's
   node kinds are a closed set (string/int64/float/bool/array/table/
   array-of-tables), so risk's "future shape reddens" test survives as
   `TestDiffCoversEveryProjectValueKind`. Cost: a Marshal+Unmarshal per Save,
   negligible. If the executor finds BurntSushi's Unmarshal-into-any flattens a
   distinction the patcher needs (array-of-inline-tables vs `[[…]]`), the
   reflect walk is the fallback and t-2's contracts hold unchanged.

2. **Array-of-tables alignment when lengths differ.**
   Risk: always index-wise — remove middle of three → rewrite elem 1's fields
   with elem 2's, delete elem 2's block. Verification (and, implicitly, mvp's
   "deleting lane[0] leaves the others byte-identical"): positional when
   lengths match, deep-equality matching when they differ → remove exactly the
   vanished block.
   **Default: verification's alignment.** Why it matters: under index-wise, the
   tail block — with its hand-added `note` and comments — is the one deleted,
   which is a c-2 violation on the one shape the spec calls out by name
   (`[[competition]]`). The residual gap (edit-and-remove in one Save, which no
   writer does) degrades to delete+append of the edited block; t-2's contract
   names the three shapes writers actually produce.

3. **Empty table header after the last key is deleted.**
   Risk: remove when the span holds no keys AND no comments. Verification:
   remove when no keys remain (modeled or unmodeled); comment case unstated.
   MVP: never remove — an empty header decodes harmlessly and needs no span
   logic.
   **Default: risk's rule (no keys and no comments → remove; otherwise keep).**
   Why it matters: it matches today's on-disk result for the common case
   (`--unset` of the last state_map entry currently yields no
   `[board.state_map]` header, since nil maps are omitted) while never making a
   user's comment or hand-added key collateral. MVP's "never remove" is the
   safe fallback if the span logic proves fiddly — it is strictly less
   surprising than a wrong removal and t-1's header tests would then invert.

4. **Where the pin task sits.**
   Verification: wave 1 (the rule "call Save, never open the file yourself" is
   true today; execute later tasks under the guard). Risk: after Save is
   rewired, because clause (c) — exactly one encoder, inside `encodeFresh` —
   is false until then. MVP: folded into the final wave.
   **Default: wave 3, parallel with the e2e proof (risk).** Why it matters: a
   wave-1 pin with clause (c) ships red (forbidden), and without (c) it does
   not fence the re-encode fallback that is the most likely regression. The
   cost is that t-3 executes without the guard — acceptable, since t-3 is the
   task that introduces `encodeFresh` and its own tests cover it.

5. **Where the c-5 note lives, and whether a PR body file is needed.**
   Risk: README `dross project` table row + `pr-body.md` for `ship
   --body-file` + a test that pr-body.md names real tests. MVP: README table
   row only; PR body auto-carries the criterion. Verification: README
   `### Updating` subsection + ARCHITECTURE.md pin; PR body auto-carries the
   criterion.
   **Default: verification.** Why it matters: (a) the adopter-upgrade-note
   locked decision puts adopter-facing notes in `### Updating`, not elsewhere;
   (b) `--body-file` REPLACES `BuildPRBody` (internal/cmd/ship.go:211-217), so
   risk's pr-body.md would drop the auto-rendered criteria/verify table unless
   hand-duplicated — and it is an untestable ship-time step that is easy to
   forget; (c) the criterion text itself is rendered into the PR verbatim, so
   c-5's "PR explicitly notes" is satisfied without a bespoke body. If the
   user wants the retirement statement more prominent than a table row in the
   PR, a complete pr-body.md (criteria table included) is the option — it is
   not in the default plan.

6. **Save safety nets: verify-before-swap, atomic temp+rename, refused-Load-never-overwrites, no-op-no-write.**
   Risk: all four, each with a test. Verification: write-after-successful-
   patch only (`TestSavePatchErrorLeavesFileUntouched`). MVP: unspecified.
   **Default: all four (risk).** Why it matters: the patcher is new code
   operating on a tracked file; the canonical-re-encode check is the one
   mechanism that turns any patcher bug into a refused write instead of a
   corrupted project.toml, and it costs one Load + two in-memory encodes. The
   refused-Load rule changes one observable behaviour — onboard onto an
   existing project.toml with `mutation.remote_host` set now errors instead of
   silently overwriting — which is the loss class this phase exists to end.

7. **Hazard breadth in the index: CRLF, EOF newline, BOM.**
   Risk covers all three; verification and mvp cover none (dross only ever
   writes "\n"-terminated files itself).
   **Default: CRLF and EOF-newline kept; BOM dropped.** Why it matters:
   hand-edited files (the exact case c-2 is about) do arrive without a final
   newline, and CRLF preservation is one recorded string; a BOM is not a shape
   any editor in the author's stack emits and BurntSushi already tolerates it
   on read. Adding BOM later is one line in `indexDoc` plus one test.

8. **Granularity of the patcher layer.**
   Risk: index (t-1) ‖ diff (t-2) → apply (t-3), three commits. Verification:
   one patcher task (index+apply) with the differ inside Save. MVP: everything
   in one commit.
   **Default: index+apply as one task, differ as its own task, Save rewire
   third.** Why it matters: index and apply are one layer (apply re-indexes
   after every op) and index alone is an unobservable commit; the differ has a
   clean stand-alone contract and no dependency on the patcher, so it runs in
   parallel in wave 1. Six tasks is the smallest count under which every
   commit is independently green and observable.
