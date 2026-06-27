# Plan — lens: VERIFICATION (designed backward from test contracts)

Each criterion's ideal contract was written first, then the smallest task that makes
it satisfiable was derived. The c-3 keystone ("a brand-new profile toml is
detectable + recon-visible with zero Go logic changes") gets its own dedicated,
end-to-end executable contract (t-6) that exercises the real user drop-in path
(HOME-overlay → LoadAll), not an in-memory stand-in.

```
Phase multilang-stack-profiles — 6 tasks across 3 waves

Wave 1
  t-1  Author full python/javascript/csharp profiles
       files:    internal/stack/profiles/python.toml,
                 internal/stack/profiles/javascript.toml,
                 internal/stack/profiles/csharp.toml
       covers:   c-1
       contract: Embedded() decodes all three as valid profiles, each carrying a
                 [[tools]] kind="analyzer" entry; a temp tree with main.py resolves
                 Detect -> "python" and python's analyzer appears in quality.Catalog().
                 If python.toml's [signals].exts omits .py, or it ships no analyzer
                 tool, the c-1 manifest row (t-5) is empty and fails. javascript.toml
                 id MUST be "javascript" (not "node") so .js still maps to "javascript"
                 — else the c-2 regression golden (t-4) .js row fails.

  t-2  Author 7 detection-only stub profiles
       files:    internal/stack/profiles/{ruby,rust,java,c,cpp,php,swift}.toml
       covers:   c-2
       contract: Each stub is id + title + [signals].exts only (no [loadout]/[[tools]])
                 and passes Decode/Validate (stub_profile_shape). extLangFor(Embedded())
                 maps .rb->ruby, .rs->rust, .java->java, .c/.h->c, .cc/.cpp->cpp,
                 .php->php, .swift->swift. Deleting ruby.toml makes the existing
                 TestQualityReconDelegatesToStack {app.py, lib.rb} fixture drop "ruby"
                 and fail.

Wave 2 (depends t-1, t-2)
  t-3  Single-source ext->lang from profiles; delete extLang map
       files:    internal/stack/detect.go
       covers:   c-2, c-3
       depends:  t-1, t-2
       contract: A new extLangFor(profiles []*Profile) map[string]string builds the
                 ext->lang map from each profile's [signals].exts, keyed by profile id,
                 applying the locked clash rule (higher Signals.Priority wins; ties
                 break lexicographically by id). DetectLanguages walks via
                 extLangFor(LoadAll()) and the package-level `extLang` literal is gone.
                 If extLangFor is reverted to a hardcoded literal, the no-map source
                 scan (t-4c) fails; if the tie-break is inverted, the clash unit (t-4b)
                 fails; if the walk stops routing through profiles, every DetectLanguages
                 row regresses.

Wave 3 (depends t-3)
  t-4  c-2 contracts: regression golden + clash tie-break + no-map scan
       files:    internal/stack/detect_test.go
       covers:   c-2
       depends:  t-3
       contract: (a) golden list of every legacy extLang extension
                 [.go .py .js .jsx .ts .tsx .rb .rs .java .kt .dart .svelte .sql .c .h
                  .cc .cpp .cs .php .swift] — each resolves to a non-empty lang through
                 extLangFor(Embedded()); drop any one profile and its row fails.
                 (b) extLangFor over two in-memory profiles sharing ext ".x" (priority
                 5 vs 10) returns the priority-10 id; with equal priority returns the
                 lexicographically-smaller id — invert the tie-break and it fails.
                 (c) detect.go source contains no hardcoded ext->lang map literal
                 (mirrors TestNoDockerHardcode's os.ReadFile idiom).
                 (d) TestDetectUnsupportedFixture updated: the new ruby stub makes the
                 old foo.rb fixture legitimately detect "ruby", so its fixture is
                 swapped to a genuinely-unmapped ext to keep the never-false-match
                 guard meaningful.

  t-5  c-1 contract: python analyzer visible in a quality run
       files:    internal/quality/recon_test.go
       covers:   c-1
       depends:  t-1, t-3
       contract: quality.BuildManifest on a temp tree containing main.py surfaces a
                 ToolStatus whose Analyzer.Languages includes "python" and which is NOT
                 in the agnostic scc/jscpd set. If python.toml ships no analyzer tool,
                 or DetectLanguages fails to route .py->python, the python row is empty
                 and the test fails.

  t-6  c-3 KEYSTONE: brand-new drop-in toml is detectable + recon-visible
       files:    internal/quality/dropin_test.go  (new)
       covers:   c-3
       depends:  t-3
       contract: With t.Setenv("HOME", tmp) and a brand-new
                 tmp/.claude/dross/profiles/cobol.toml (id+title+exts=[".cob"]+one
                 analyzer tool) — a profile shipped by NO Go code — a tree containing
                 prog.cob makes stack.DetectLanguages include "cobol" AND
                 quality.BuildManifest surface cobol's analyzer, with ZERO edits to
                 detect.go/recon.go/catalog.go. If detection or recon were still driven
                 by a hardcoded map or switch, the unknown "cobol" profile would be
                 invisible and both assertions fail. This is the executable proof of
                 the zero-Go-logic-change keystone.
```

## Coverage

| criterion | tasks |
|-----------|-------|
| c-1 | t-1 (full python/js/csharp profiles + analyzers), t-5 (analyzer-in-manifest contract) |
| c-2 | t-2 (residual stub coverage), t-3 (delete extLang, derive from profiles + clash rule), t-4 (regression golden + clash tie-break + no-map scan + unsupported-fixture fix) |
| c-3 | t-3 (single-sourcing makes it true), t-6 (dedicated end-to-end drop-in keystone proof) |

All three criteria covered.

## Judgment calls

- chose: javascript profile id = `"javascript"`; rejected: id = `"node"`; why: the deleted extLang mapped `.js`/`.jsx` -> "javascript", so id "node" would silently rename the language and trip the c-2 no-regression golden. package.json stays a Detect file-signal.
- chose: c-3 keystone test drives the real `t.Setenv("HOME", ...)` + `~/.claude/dross/profiles/` overlay through LoadAll; rejected: injecting an in-memory `*Profile` via a new exported `DetectLanguagesWith`; why: the overlay path is exactly how a user adds a drop-in, so it proves "zero Go change" faithfully and needs no new production seam.
- chose: clash tie-break unit (t-4b) calls the package-private `extLangFor` directly (test is in-package); rejected: proving the tie-break only via a HOME-overlay of two clashing tomls; why: a pure-function unit pins the locked rule (priority, then lexicographic id) at the exact surface, with no filesystem noise.
- chose: split profiles into t-1 (full, with analyzers/runtime/loadout) and t-2 (detection-only stubs); rejected: one "author all profiles" task; why: full profiles are multi-block authoring tied to c-1, stubs are id+title+exts tied to c-2 — different contracts, independent, both pure wave-1 TOML.
- chose: t-4 also repairs TestDetectUnsupportedFixture; rejected: leaving it; why: shipping a ruby stub makes its `foo.rb` fixture legitimately detect "ruby", silently breaking an existing guard — the fixture must move to a truly-unmapped ext or the guard becomes a false red.
- chose: no profiles/README.md edit task; rejected: documenting stub_profile_shape there; why: no criterion requires it and the locked shape is already enforced executably by t-2's Decode/Validate contract — doc-only work would be gold-plating.
- chose: test tasks (t-4/t-5/t-6) are wave 3, strictly after t-3; rejected: co-locating them in wave 2; why: every contract asserts against the post-deletion extLangFor surface, which only exists once t-3 lands.
```
