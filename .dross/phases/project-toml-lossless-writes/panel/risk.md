# Risk-lens draft — project-toml-lossless-writes

Bias applied: every task owns a named way the write can go wrong. The graph is
shaped so that a patcher bug can never reach the tracked file (verify-before-
swap), a writer author can never route around the patcher (diff-driven `Save`
+ AST pin), and every value shape / file-layout hazard has exactly one test
that fails when it regresses.

Architecture the tasks assume (all in `internal/project`, no new dependency):

- `Save(path)` becomes **diff-driven**: if `path` does not exist → whole-file
  encode via a single helper `encodeFresh` (init/onboard, unchanged behaviour).
  If it exists → read bytes, `Load` them into `old`, `diff(old, p)` → ops,
  `apply(bytes, ops)` → patched, **verify** `Load(patched)` canonically equals
  `p`, then temp-file + rename. Every existing writer keeps its `p.Save(path)`
  call and becomes lossless with no per-writer edits — a future writer that
  mutates the struct and calls `Save` cannot reintroduce the bug because it
  never chooses a serializer.
- `op` = `{table []string, elem int /* -1 unless array-of-tables */, key string, value any /* nil = delete */}`.
- Shapes the differ/patcher must cover (locked `field_type_scope`): scalar
  (string/int/bool), `[]scalar`, table-of-scalars (`map[string]string`:
  `[board.state_map]`, `[constraints]`), struct table (`[board.fields]`,
  `[mutation.gremlins]`), table-of-tables (`map[string]Service`:
  `[runtime.services.app]`), array-of-tables (`[]struct`: `[[stack.locked]]`,
  `[[runtime.test_lane]]`, `[[competition]]`).

```
Phase project-toml-lossless-writes — 7 tasks across 4 waves

Wave 1
  t-1  Index raw TOML bytes into a line model
       files:    internal/project/patch_index.go, internal/project/patch_index_test.go
       covers:   c-1, c-6
       description: Pure `indexDoc(src []byte) (*doc, error)`: classifies every line as
                 header (table / array-of-tables, with dotted path and running element
                 index), key line (indent, key path incl. quoted/dotted keys, value start
                 offset, value END line — multi-line arrays and """ / ''' strings span
                 lines), comment, or blank; records the file's EOL ("\n" vs "\r\n"), a
                 leading BOM, and whether the last line is newline-terminated. Inline
                 tables `{...}` are indexed as a value span but flagged so apply can
                 refuse them by line number.
       contract: if the value scanner stops at the `#` inside `desc = "a # b"  # real`,
                 TestIndexValueSpanIgnoresHashInsideStrings fails (span must end before
                 `  # real`); if a multi-line `non_goals = [\n "a", # c\n "b",\n]` is not
                 indexed as one 4-line value span, TestIndexMultilineArraySpan fails; if a
                 `"""` string containing `]` and `=` is split, TestIndexMultilineBasicString
                 fails; if `[[stack.locked]]` elements are not numbered 0..3 in order,
                 TestIndexArrayOfTablesElementIndex fails; if a CRLF file is indexed with
                 EOL "\n", TestIndexRecordsCRLF fails; if `"fix versions" = "x"` (quoted
                 key) or `state_map.planned = "x"` (dotted key inside [board]) is not
                 resolved to its full key path, TestIndexQuotedAndDottedKeys fails.
       depends_on: —

  t-2  Diff two Projects into shape-typed ops
       files:    internal/project/patch_diff.go, internal/project/patch_diff_test.go
       covers:   c-3, c-6
       description: `diff(old, new *Project) ([]op, error)` walks both structs by
                 reflection over `toml` tags and emits ops only for leaves that differ.
                 Omitempty leaf that became zero → delete op; non-omitempty leaf that
                 became zero → set-to-zero-literal op. Maps → per-key set/delete (never a
                 whole-map replace). Array-of-tables → index-wise per-field diff, delete
                 trailing elements from the end, append new elements as whole blocks. An
                 unsupported leaf kind (e.g. `[]map`, `map[string][]string`) returns an
                 error naming the field — never a silent skip.
       contract: if the differ emits an op for an unchanged field, TestDiffNoChangeIsNoOps
                 (Load(real project.toml) diffed against itself → zero ops) fails; if
                 setting `Project.Version` alone yields anything but one scalar op at
                 table [project] key version, TestDiffScalarChangeIsOneOp fails; if
                 clearing `Stack.Profile` (omitempty) emits a set-"" op instead of a
                 delete, TestDiffOmitemptyZeroIsDelete fails; if clearing `Repo.Layout`
                 (no omitempty) emits a delete instead of `layout = ""`,
                 TestDiffRequiredZeroIsSetLiteral fails; if removing the middle of three
                 test lanes does not yield (rewrite elem 1 fields, delete elem 2),
                 TestDiffArrayOfTablesShrinkIsIndexwise fails; if deleting one state_map
                 key yields a whole-map op, TestDiffMapDeleteIsPerKey fails; if a
                 reflect walk of every leaf reachable from `Project{}` hits a kind the
                 differ cannot classify, TestDiffCoversEveryProjectLeafKind fails — so a
                 future field of a new shape reddens the suite the day it lands.
       depends_on: —

Wave 2 (depends t-1, t-2)
  t-3  Apply ops to the line model surgically
       files:    internal/project/patch_apply.go, internal/project/patch_value.go,
                 internal/project/patch_apply_test.go
       covers:   c-1, c-2, c-6
       description: `apply(src []byte, ops []op) ([]byte, error)`; re-indexes after
                 every op so offsets never go stale. Set on an existing key replaces
                 ONLY the value span — indent, key spelling, and any trailing `# comment`
                 on that line survive. Set on a missing key inserts after the table's
                 last key line (before trailing blanks/comments), copying the sibling
                 indent (or header indent + the file's indent unit when the table is
                 empty). Missing table / array-of-tables element → new header block
                 placed after the last header sharing the parent prefix, else EOF (a
                 missing final newline is added first). Delete key → remove the line;
                 delete array-of-tables element → remove its header-to-next-header span;
                 a table header whose span is left with no keys AND no comments is
                 removed too. `renderValue(v any) string` produces the TOML value text by
                 encoding a one-key map through BurntSushi's encoder and stripping the
                 `k = ` prefix, so escaping is byte-for-byte what today's files carry.
                 An inline-table spelling (`state_map = { … }`) at the target is refused
                 with the line number and the `[board.state_map]` rewrite named — never
                 partially rewritten.
       contract: if replacing `version = "1.7.5.0"  # bumped` loses the trailing comment
                 or the 2-space indent, TestApplyReplaceKeepsIndentAndTrailingComment
                 fails; if inserting `profile = "go"` into [stack] lands after
                 `[[stack.locked]]` (outside the table's direct-key span),
                 TestApplyInsertStaysInsideTableSpan fails (re-Load would attribute it to
                 the wrong table); if a new `[board.state_map]` on the repo's own
                 project.toml is not emitted as `  [board.state_map]` / `    planned = …`,
                 TestApplyNewSubtableMatchesEncoderLayout fails; if two set ops on the
                 same table land on the same line (stale offsets),
                 TestApplyTwoOpsSameTableBothLand fails; if deleting the last state_map
                 key leaves an empty `[board.state_map]` header,
                 TestApplyDeleteLastKeyRemovesEmptyHeader fails — and if the header held a
                 comment and was removed anyway, TestApplyKeepsHeaderThatHoldsAComment
                 fails; if appending a fifth `[[stack.locked]]` block lands at EOF instead
                 of after the fourth, TestApplyAppendArrayElementAfterLastSibling fails;
                 if `renderValue("a\"b\\c\n")` differs from what toml.NewEncoder writes
                 for the same string, TestRenderValueMatchesEncoder fails; if a CRLF
                 source gets an inserted line ending in bare "\n",
                 TestApplyPreservesCRLF fails; if a file with no trailing newline gets a
                 new table glued onto its last line, TestApplyEOFNewline fails; if an
                 inline-table target is silently rewritten instead of refused with its
                 line number, TestApplyRefusesInlineTableByLine fails; every apply test
                 also asserts `toml.Decode` of the output succeeds.
       depends_on: t-1, t-2

Wave 3 (depends t-3)
  t-4  Make Save diff-driven, verified, atomic
       files:    internal/project/project.go, internal/project/project_test.go,
                 internal/project/patch_save_test.go
       covers:   c-1, c-2, c-3
       description: `Save`: stat path; absent → `encodeFresh` (the ONLY toml.NewEncoder
                 call site left in the package); present → ReadFile, Load (a decode
                 refusal — including the remote_host trap — returns the error, never falls
                 back to whole-file encode), diff, apply, then VERIFY: `encodeFresh` of
                 `Load(patched)` must equal `encodeFresh` of `p` (canonical re-encode, so
                 nil-vs-empty-slice and map order cannot false-positive); on mismatch
                 return an error naming the first differing key and leave the file
                 untouched. Write via temp file in the same dir + fsync + rename,
                 preserving the original mode. Zero ops → no write at all (mtime
                 untouched).
       contract: if a patcher bug yields a document that decodes differently from `p`,
                 TestSaveRefusesUnverifiedPatch (injects an op whose rendered value is
                 mis-typed) fails because the original bytes were replaced; if Save on a
                 file whose Load is refused (remote_host set) overwrites it instead of
                 erroring, TestSaveNeverOverwritesARefusedFile fails; if a Save with no
                 struct change rewrites the file, TestSaveNoChangeLeavesMtime fails; if an
                 apply error mid-way leaves a truncated project.toml (today's os.Create
                 shape), TestSaveFailureLeavesOriginalBytes fails; if Save on a missing
                 path stops producing today's encoder output, the existing TestRoundTrip,
                 TestSaveDefaultsAreOmittedAndOptionalsRemainEmpty and
                 TestNoTestLaneIsAbsentFromTheDocument fail; if a 0-byte existing file
                 cannot be patched into a Load-equal document, TestSaveOntoEmptyFile fails.
       depends_on: t-3

Wave 4 (depends t-4)
  t-5  Prove every writer path is lossless end to end
       files:    internal/cmd/project_lossless_test.go
       covers:   c-1, c-2, c-4, c-6
       description: One fixture project.toml seeded with: a header comment, an inline
                 `# why` comment after `version`, a `note = "hand-added"` under
                 `[[competition]]`, a `note` under `[[stack.locked]]`, an unmodeled
                 `[custom]` table, an unmodeled key in `[remote]`, a multi-line
                 `non_goals = [ … ]`, and 2-space indentation. A helper `changedLines
                 (before, after) []int` asserts the set of touched line numbers equals
                 exactly the target. Exercised through the real cobra commands in a
                 t.TempDir root: `state set version` (writeVersion); `project set` on a
                 scalar, `stack.languages` and `remote.reviewers` (arrays),
                 `board.state_map.planned` and `board.fields.state` (tables, creating
                 the sub-table on first write); `project set --unset` on a scalar and on
                 the last state_map entry; `test lane add` / `edit` / `remove`
                 (array-of-tables); `issue enable` / `disable` (bool). Plus a copy of the
                 repo's own `.dross/project.toml` (tracked, hermetic-rule-safe) with only
                 the version bumped.
       contract: if `state set version` touches any line but the `version =` line of the
                 fixture, TestLosslessStateSetVersion fails on the changedLines set; if
                 the `note` under [[competition]] or [[stack.locked]] is missing after ANY
                 of the writes, TestLosslessUnmodeledKeysSurviveEveryWriter fails (it
                 runs the whole writer matrix and greps for both notes and `[custom]`
                 after each); if `project set stack.languages go,rust` rewrites the
                 multi-line `non_goals` array or anything outside its own line,
                 TestLosslessArrayWriteIsOneLine fails; if `test lane remove` on the
                 middle of three lanes touches lane 1's block,
                 TestLosslessLaneRemoveLeavesSiblings fails; if bumping the version on the
                 real project.toml copy yields a diff of more than one line (indentation
                 of `  [[stack.locked]]` or the empty `[paths]`/`[env]` headers
                 re-rendered), TestLosslessRealProjectTomlOneLineDiff fails.
       depends_on: t-4

  t-6  Pin the single project.toml write path
       files:    internal/cmd/project_writer_pin_test.go
       covers:   c-3
       description: go/ast scan (same technique as hermetic_dross_read_test.go) of every
                 non-test .go file under internal/: (a) outside internal/project, any
                 os.WriteFile / os.Create / os.OpenFile / os.Rename call whose path
                 argument mentions `project.File` is a violation; (b) outside
                 internal/project, any toml.NewEncoder / toml.Marshal whose argument
                 expression is not os.Stdout and whose enclosing file references
                 `project.File` is a violation; (c) inside internal/project, exactly one
                 toml.NewEncoder call site exists and its enclosing FuncDecl is
                 `encodeFresh`. Violations are computed by a pure function over source
                 text so the teeth are proven on synthetic input.
       contract: if a new cmd file does `os.WriteFile(filepath.Join(root, project.File),
                 …)`, TestProjectTomlHasOneWriter fails naming the file:line; if someone
                 adds a second toml.NewEncoder in internal/project (a re-encode
                 fallback), TestEncodeFreshIsTheOnlyEncoder fails; if the detector misses
                 the `p := filepath.Join(root, project.File); os.WriteFile(p, …)` two-step
                 shape, TestWriterPinCatchesIndirectPath (synthetic source) fails; if it
                 flags projectShow's `toml.NewEncoder(os.Stdout)`,
                 TestWriterPinAllowsStdoutEncode fails.
       depends_on: t-4

  t-7  Note the retirement of per-repo set bans
       files:    README.md, .dross/phases/project-toml-lossless-writes/pr-body.md,
                 internal/cmd/readme_lossless_note_test.go
       covers:   c-5
       description: README `dross project {show,get,set}` row (and the `dross state`
                 row) gains one sentence: writes are surgical — comments and unknown keys
                 survive — so per-repo rules banning `dross project set` /
                 `dross state set version` (e.g. codriver.nvim's r-01) are safe to
                 retire. `pr-body.md` carries the same statement plus the proving test
                 names from t-5, for `dross ship --body-file`. A doc-pin test reads
                 README.md (tracked) and asserts the sentence sits on the same table row
                 as `dross project`.
       contract: if the README row for `dross project` no longer contains "safe to
                 retire" together with `dross state set version`,
                 TestReadmeNotesSetBansRetirable fails; if pr-body.md names a t-5 test
                 that does not exist in internal/cmd, TestPRBodyNamesRealTests fails.
       depends_on: t-5
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 byte-identical other lines | t-1, t-3, t-4, t-5 |
| c-2 unschematized keys survive every path | t-3, t-4, t-5 |
| c-3 one shared write path, new writers can't regress | t-2, t-4, t-6 |
| c-4 seeded comments+unmodeled key test per affected path | t-5 |
| c-5 PR/changelog notes bans retirable | t-7 |
| c-6 arrays, tables, array-of-tables covered | t-1, t-2, t-3, t-5 |

## Judgment calls

- **Diff-driven `Save` over an explicit per-writer op API.** Chose: writers keep mutating the struct and calling `Save`; the shape knowledge lives once in the reflect-based differ. Rejected: each of the ~10 call sites (stack apply, three lane verbs, issue enable/disable, writeVersion, project set/unset) hand-building ops. Why: the risk "writer author forgets to emit an op for a field they changed" vanishes by construction, and c-3's "future writer can't reintroduce" holds without relying on the AST pin alone.
- **Verify-before-swap by canonical re-encode, not DeepEqual.** Chose: `encodeFresh(Load(patched)) == encodeFresh(p)`. Rejected: reflect.DeepEqual. Why: nil-vs-empty-slice (`splitCSV` returns `[]string{}`, decode yields nil) and map iteration order would false-positive DeepEqual and block legitimate writes; the encoder already canonicalizes both.
- **A refused Load never falls back to whole-file encode.** Chose: error out (affects onboard onto a corrupt existing project.toml). Rejected: overwrite as today. Why: silently replacing an unparseable tracked file is the exact loss class this phase exists to end.
- **Index-wise array-of-tables diff.** Chose: element i vs element i, delete the tail, append the surplus. Rejected: LCS/identity-keyed matching. Why: deterministic and small; the only cost is that removing a middle lane rewrites the fields of the following block in place (its unmodeled keys and comments survive; the tail block is deleted). Sibling blocks before the removed one stay byte-identical, which is what t-5 asserts.
- **Value text rendered through BurntSushi's encoder on a one-key map.** Chose: delegate escaping. Rejected: a hand-written TOML string escaper. Why: no second escaping implementation to drift, and the replaced value is byte-for-byte what the same file would have carried under the old encoder.
- **Empty header removal only when the span holds no keys and no comments.** Chose: keep a header that shelters a comment. Rejected: always delete on last-key removal (what nil-map encoding did). Why: a user's comment is never collateral; an empty table decodes to the same struct anyway.
- **Inline tables are refused by line, not rewritten and not passed through.** Chose: explicit refusal naming the `[board.state_map]` rewrite. Rejected: handling `{…}` spans and dotted-key rewrites. Why: a half-supported syntax that sometimes corrupts is worse than one that always says no; dross never writes inline tables itself, so the case is hand-authored and rare.
- **Placement of new blocks: after the last header sharing the parent prefix, else EOF.** Chose: family-adjacent. Rejected: always EOF. Why: EOF is valid TOML but makes `[board.state_map]` land pages away from `[board]`; family-adjacent keeps the document readable without a layout engine.
- **t-7 waits for t-5.** Chose: the README claim and pr-body land only after the proving tests exist and are named. Rejected: docs in wave 1. Why: a lossless-writes claim written before the test that proves it is the false-green shape the execution rules forbid.
