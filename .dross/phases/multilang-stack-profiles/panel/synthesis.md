# Synthesis — multilang-stack-profiles (cold judge over risk / mvp / verification)

I authored none of the three drafts. Below: scoring, the merged plan grafted onto the
strongest skeleton, and the divergences left unresolved (escalated, not papered over).

Source-verified before judging: `svelte.toml` declares `exts = [".svelte", ".ts", ".tsx"]`
at `priority = 6`; `typescript.toml` declares `exts = [".ts", ".tsx"]` at `priority = 4`.
The locked `ext_clash_resolution` "higher priority wins" rule therefore maps `.ts -> svelte`
and drops `typescript` — a real c-2 regression. The spec/reality conflict is genuine (see
Disagreement D1). Also confirmed: `quality/catalog.go` + both `recon.go` are already
profile-driven (`LoadAll`/`ByID`), so c-1 needs no Go change; and `TestDetectUnsupportedFixture`
(`foo.rb`) breaks the moment a ruby stub ships.

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|-------|-------------------|---------------------------|-------------|------------------|
| risk (4t/3w) | 3/3 owned, one owner per risk; c-1→t-1, c-2→t-1/t-2/t-3, c-3→t-4 | Strongest: names exact tests + full 20-ext golden, and is the **only** draft that catches the svelte/.ts clash and tests against it | Balanced 4 tasks; full/stub/mechanism/keystone split; each mechanism task carries its own guarding test | Best: t-1/t-2 wave1, t-3 wave2, t-4 wave3; every code task is gated by a test in the same task |
| mvp (3t/2w) | 3/3 but thin — c-3 folded into the t-3 mechanism task, no dedicated keystone | Weakest: generic (a)-(d) breaks; **follows priority-wins literally so its own no-regression table self-contradicts** on `.ts` (typescript row would fail) | Coarsest; bundles keystone into mechanism; good MVP economy | Correct-minimal 2 waves; tests ride inside owning tasks |
| verification (6t/3w) | 3/3 with the cleanest matrix; a dedicated test task per criterion | High on executable end-to-end proofs (real HOME-overlay keystone, analyzer-in-manifest) and **uniquely flags the `foo.rb` fixture repair**; but softens the golden to "non-empty lang", dodging rather than catching the clash | Finest, over-split; mechanism t-3 carries no test in its own wave | Weakest: detect.go rewrite (t-3) lands in wave 2 with its guarding tests deferred to wave 3 — a commit gated by nothing |

**Skeleton: risk.** It is the only draft that surfaces and tests the svelte/.ts/locked-decision
conflict (the phase's central hazard), its 4-task granularity is balanced, and its waves keep
every code task gated by a test in the same task (execution-hygiene correct). Grafts: MVP's
parameterized seam, and verification's fixture-repair task + explicit zero-Go-edit keystone framing.

## Merged plan

Phase multilang-stack-profiles — 4 tasks across 3 waves

### Wave 1

- **t-1 — Author full profiles: python, javascript, csharp** `[risk + mvp + verification]`
  - files: `internal/stack/profiles/python.toml`, `internal/stack/profiles/javascript.toml`,
    `internal/stack/profiles/csharp.toml`, `internal/stack/detect_test.go`,
    `internal/quality/recon_test.go`
  - covers: c-1, c-2
  - contract: (a) If `python.toml` drops `.py` or its `[[tools]]` analyzer, `Detect` on a
    `.py`-only tree no longer returns "python" AND `quality.BuildManifest` on that tree omits
    the python analyzer (the c-1 "analyzer shows in a run" row; verification adds: python's
    analyzer must also appear in `quality.Catalog()`). (b) `javascript.toml` id MUST be
    `"javascript"` (not `"node"`) with exts `.js`+`.jsx` — id="node" silently renames the
    language and trips the t-3 no-regression golden's `.js` row. (c) If javascript claims
    `package.json` without a lower priority than typescript, `Detect` on a `tsconfig.json`+`.ts`
    tree returns "javascript" not "typescript" (TS-hijack guard). (d) If any full profile's
    analyzer carries a cosmetic dimension, `quality.TestCatalogExcludesCosmetic` fails.

- **t-2 — Author 7 detection-only stub profiles** `[risk + mvp + verification; fixture repair: verification]`
  - files: `internal/stack/profiles/ruby.toml`, `rust.toml`, `java.toml`, `c.toml`,
    `cpp.toml`, `php.toml`, `swift.toml`, `internal/stack/profile_test.go`,
    `internal/stack/detect_test.go`
  - covers: c-2
  - contract: (a) A stub with ONLY `id`+`title`+`[signals].exts` (no `[runtime]`/`[[tools]]`/
    `[loadout]`) must Decode+Validate clean — a Validate regression that rejects a bare stub
    fails `TestStubMinimalShapeLoads`. (b) If any one stub is malformed (missing id, or a
    `[[tools]]` entry with no bin), `Embedded()` returns an error and the ENTIRE shipped set
    fails to load — `TestEmbeddedProfilesAllLoad` catches the blast radius. (c) If `c.toml`
    drops `.h` or `cpp.toml` drops `.cc`, the per-stub detect row loses "c"/"cpp" (multi-ext
    aliases the old map carried). (d) GRAFT `[verification]`: repair `TestDetectUnsupportedFixture`
    — shipping `ruby.toml` makes the existing `foo.rb` fixture legitimately `Detect` "ruby",
    silently breaking the never-false-match guard; swap that fixture to a genuinely-unmapped
    ext so the guard stays meaningful. This rides in t-2 because it is t-2's ruby stub that
    breaks the test, in the same commit.

### Wave 2 (depends t-1, t-2)

- **t-3 — Single-source DetectLanguages from profiles; delete extLang** `[risk; seam graft: mvp]`
  - files: `internal/stack/detect.go`, `internal/stack/detect_test.go`
  - covers: c-2 (and enables c-3)
  - contract: (a) Delete the `extLang` map; derive ext→lang by UNION over every loaded
    profile's `[signals].exts` (see D1 for why union, not winner-take-all). A temp tree with
    one file per legacy ext (`.go .py .js .jsx .ts .tsx .rb .rs .java .kt .dart .svelte .sql
    .c .h .cc .cpp .cs .php .swift`) must yield all 16 legacy language ids —
    `TestDetectLanguagesNoRegression` loses the exact language whose derivation broke.
    (b) svelte↔typescript clash: a pure-`.ts` repo's `DetectLanguages` MUST still include
    "typescript" — a winner-take-all derivation returns only "svelte" (pri 6 > 4) and fails
    `TestTsNotHijackedBySvelte`. (c) Signature preserved: `DetectLanguages(root string)` (loads
    profiles internally), so the security/quality delegation tests stay green with no recon-caller
    edit, and `TestNoDuplicateExtLangMap` passes (no map literal survives). GRAFT `[mvp]`:
    introduce an unexported parameterized core `detectLanguagesFrom(root, profiles)` (or
    `extLangFor(profiles)`) that `DetectLanguages(root)` wraps with `LoadAll()` — gives the
    clash/tie-break unit a filesystem-free seam without widening the public surface.

### Wave 3 (depends t-3)

- **t-4 — Drop-in keystone: new toml detectable + recon-visible, zero recompile** `[risk; zero-edit assertion graft: verification]`
  - files: `internal/stack/detect_test.go`, `internal/quality/recon_test.go`
    (verification's alternative: a new `internal/quality/dropin_test.go`)
  - covers: c-3
  - contract: (a) With `t.Setenv("HOME", tmp)` and a brand-new `<id>.toml`
    (`exts=[".zzz"]`, one analyzer) under the real user-profile overlay, a `.zzz` tree's
    `stack.DetectLanguages` includes that id AND `quality.AnalyzersFor(id)`/`BuildManifest`
    surfaces its analyzer — proving detectable + recon-visible with ZERO edits to
    `detect.go`/`recon.go`/`catalog.go` (verification's explicit zero-Go-edit framing); reverting
    t-3's derivation breaks the `DetectLanguages` half. (b) A malformed toml dropped beside it
    must NOT crash: `DetectLanguages` still returns the embedded language set (never-crash
    drop-in seam). This drives the real `HOME` → `LoadAll` user path, not an in-memory stand-in.

## Disagreements

### D1 — svelte/.ts ext-clash vs the locked ext_clash_resolution (MUST resolve; spec/reality conflict)
**What diverged.** The locked decision says ext→lang derivation uses "higher `Signals.priority`
wins, ties lexicographic," with shipped profiles "kept disjoint by convention." That convention
is **already false in the repo**: `svelte.toml` (`.ts`,`.tsx` @ pri 6) and `typescript.toml`
(`.ts`,`.tsx` @ pri 4) overlap. Applying priority-wins maps `.ts -> svelte`, so a `.ts` file
stops being detected as typescript — a direct c-2 ("No language detected today stops being
detected") regression.

**Which lens said what.** *risk* caught it and chose UNION derivation (every profile declaring
an ext contributes its language), resolving toward c-2 over the locked rule. *mvp* and
*verification* both followed priority-wins literally — and neither caught the contradiction:
mvp's own no-regression table would fail its typescript row, and verification softened its
golden to "resolves to a non-empty lang" (which passes on `.ts -> svelte` and so masks the loss).

**Provisional default taken.** UNION derivation for `DetectLanguages` (D1 = risk's choice).
Rationale: `DetectLanguages` is *already* a set-union over the tree — it returns every language
present, not one winner — so union is its natural semantics and preserves every legacy language
(c-2's letter). `Detect` (single best profile) is untouched and keeps priority-wins. Cost:
union makes a pure-TypeScript repo also report "svelte" (a new, spurious detection, not a
regression of an existing one), which is imperfect.

**Why it matters / why escalate.** This contradicts a `locked = true` decision and every clean
fix collides with another lock:
  - (a) **Union for DetectLanguages, priority-wins for Detect** [provisional default] — satisfies
    c-2's letter but adds a spurious "svelte" to every TS repo.
  - (b) **Keep priority-wins everywhere** — `.ts -> svelte` drops typescript = fails c-2 as written;
    only acceptable if c-2 is amended.
  - (c) **Change profile priorities** (raise typescript ≥ svelte on `.ts`, or remove `.ts`/`.tsx`
    from `svelte.toml`) — cleanest, but violates `full_profile_set`'s lock to "leave existing
    go/typescript/svelte/… profiles untouched."
All three touch a locked decision, so this is a user call, not a planner call. The merged plan
builds against (a) and the t-3(b) `TestTsNotHijackedBySvelte` test pins it, but the decision
record (`ext_clash_resolution`) needs the user's ruling before execution.

### D2 — Derivation seam: internal LoadAll (risk) vs parameterized core (mvp/verification)
**What diverged.** risk keeps `DetectLanguages(root)` and "calls LoadAll internally" with no
named test seam. mvp wants an unexported `detectLanguagesFrom(root, profiles)`; verification
wants `extLangFor(profiles []*Profile) map[string]string`. All three agree the *public*
signature stays `DetectLanguages(root string)` — so this is a structure/testability divergence,
not a signature one.
**Provisional default.** Adopt the parameterized core (mvp/verification). It lets the clash and
tie-break be unit-tested as a pure function (no filesystem) while leaving the public API and the
two recon delegators untouched. Grafted into t-3. Matters because the locked tie-break rule is
exactly the kind of logic that should be pinned at a pure-function surface, not only through
temp-dir fixtures.

### D3 — Test-task placement / granularity: co-located (risk/mvp) vs dedicated wave-3 test tasks (verification)
**What diverged.** risk and mvp co-locate each contract inside its owning task (4 / 3 tasks).
verification splits every criterion's tests into standalone wave-3 tasks (t-4/t-5/t-6), leaving
the detect.go rewrite (t-3) in wave 2 with no test in its own task.
**Provisional default.** Co-locate (risk/mvp). Per execution-hygiene, a code change must be gated
by an observed test in the same task/commit; verification's split would land the extLang deletion
in a commit gated by nothing, with its regression+clash guards arriving a wave later. The merged
plan keeps t-3's guarding tests inside t-3. Matters because it is the difference between a
green-gated commit and a false-green one.

### D4 — c-3 proof vehicle: real HOME-overlay (risk/verification) vs in-package loadAllFrom seam (mvp)
**What diverged.** risk and verification prove c-3 through the real user path
(`t.Setenv("HOME", …)` → `LoadAll`), asserting zero edits to detect.go/recon.go/catalog.go.
mvp proves it with an in-package `loadAllFrom(tempUserDir)` "zig" profile — lighter, but it
bypasses `UserProfileDir`/`LoadAll`.
**Provisional default.** Real HOME-overlay (risk + verification's zero-edit assertion), kept in
t-4. c-3 demands proof that a *user's* drop-in is detectable and recon-visible without recompiling;
the overlay path is exactly that mechanism, so it is the faithful keystone. mvp's loadAllFrom unit
is fine as cheap defense-in-depth but is not sufficient as the sole proof.

---
synthesis: 4 tasks across 3 waves, 4 disagreements
