# Risk-lens plan — multilang-stack-profiles

Lens: failure modes drive the graph. Every named risk below is owned and tested by
exactly one task. The marquee risk surfaced while reading source: the shipped
`svelte.toml` already declares `exts = [".ts", ".tsx"]` at priority 6 and
`typescript.toml` declares the same exts at priority 4 — the locked
`ext_clash_resolution` "shipped profiles kept disjoint by convention" assumption is
**already false**. A naive winner-take-all derivation makes `.ts` resolve to
`svelte` (higher priority), so `typescript` stops being detected from a `.ts` file —
a direct c-2 regression. t-3 owns this.

Risks mapped to owners:

- R1 detection regression on extLang deletion (esp. multi-ext aliases .jsx/.h/.cc/.tsx) → t-3
- R2 the svelte↔typescript .ts/.tsx clash + the ext-clash tie-break never crashing → t-3
- R3 the 7 stubs: a malformed stub kills the whole Embedded() set; a no-loadout stub must still load → t-2
- R4 DetectLanguages signature ripple into the two recon callers → t-3 (owned by *preserving* the signature + a delegation guard)
- R5 full profiles wrong id ("node" vs "javascript") regresses .js; analyzer with cosmetic dimension; javascript Detect hijacks a TS repo → t-1
- R6 a malformed/brand-new drop-in toml: must be detectable + recon-visible with zero recompile, and a garbage drop-in must not crash → t-4

```
Phase multilang-stack-profiles — 4 tasks across 3 waves

Wave 1
  t-1  Author full profiles: python, javascript, csharp
       files:    internal/stack/profiles/python.toml,
                 internal/stack/profiles/javascript.toml,
                 internal/stack/profiles/csharp.toml,
                 internal/stack/detect_test.go,
                 internal/quality/recon_test.go
       covers:   c-1, c-2
       contract: (a) If python.toml drops .py or its [[tools]] analyzer, Detect on a
                 .py-only tree no longer returns "python" AND quality.BuildManifest on
                 that tree omits the python analyzer (the c-1 "analyzer shows in a run"
                 row). (b) id MUST be "javascript" with exts .js+.jsx — if it is "node"
                 or drops .jsx, the t-3 legacy-set guard loses "javascript". (c) If
                 javascript claims package.json without a lower priority than
                 typescript, Detect on a tsconfig.json+.ts tree returns "javascript"
                 not "typescript" → the TS-hijack guard fails. (d) If any full
                 profile's analyzer carries a cosmetic dimension,
                 quality.TestCatalogExcludesCosmetic fails.

  t-2  Author 7 detection-only stub profiles
       files:    internal/stack/profiles/ruby.toml, rust.toml, java.toml, c.toml,
                 cpp.toml, php.toml, swift.toml,
                 internal/stack/profile_test.go
       covers:   c-2
       contract: (a) A stub with ONLY id+title+[signals].exts (no [runtime]/[[tools]]/
                 [loadout]) must Decode+Validate clean — a regression in Validate that
                 rejects a bare stub fails TestStubMinimalShapeLoads. (b) If any one
                 stub is malformed (missing id, or a [[tools]] entry with no bin),
                 Embedded() returns an error and the ENTIRE shipped set fails to load —
                 TestEmbeddedProfilesAllLoad catches the blast radius. (c) If c.toml
                 drops .h or cpp.toml drops .cc, the per-stub detect row loses "c"/"cpp"
                 (multi-ext aliases the old map carried).

Wave 2 (depends t-1, t-2)
  t-3  Single-source DetectLanguages from profiles; delete extLang
       files:    internal/stack/detect.go,
                 internal/stack/detect_test.go
       covers:   c-2
       contract: (a) Delete the extLang map; derive ext->lang by UNION over every
                 loaded profile's [signals].exts. A temp tree with one file per legacy
                 ext (.go .py .js .jsx .ts .tsx .rb .rs .java .kt .dart .svelte .sql
                 .c .h .cc .cpp .cs .php .swift) must yield all 16 legacy language ids —
                 TestDetectLanguagesNoRegression loses the exact language whose ext
                 derivation broke. (b) The svelte↔typescript clash: a pure-.ts repo's
                 DetectLanguages MUST still include "typescript" — a winner-take-all
                 derivation returns "svelte" (priority 6 > 4) and fails
                 TestTsNotHijackedBySvelte. (c) Signature preserved: DetectLanguages
                 stays DetectLanguages(root string) (LoadAll internally), so the
                 security/quality DetectLanguages==stack.DetectLanguages delegation
                 tests stay green with no recon-caller edit, and TestNoDuplicateExtLangMap
                 still passes (no map literal survives anywhere).

Wave 3 (depends t-3)
  t-4  Drop-in keystone: new toml detectable + recon-visible, zero recompile
       files:    internal/stack/detect_test.go,
                 internal/quality/recon_test.go
       covers:   c-3
       contract: (a) With HOME pointed at a temp dir holding a brand-new <id>.toml
                 (exts=[".zzz"], one analyzer), a .zzz tree's stack.DetectLanguages
                 includes that id AND quality.AnalyzersFor(id)/BuildManifest surfaces
                 its analyzer — proving detectable + recon-visible with no Go rebuild;
                 reverting t-3's derivation breaks the DetectLanguages half. (b) A
                 malformed toml dropped beside it must NOT crash: DetectLanguages still
                 returns the embedded language set (never-crash drop-in seam).
```

## Coverage

- c-1 → t-1 (Detect resolves python on a .py tree; its analyzer appears in a
  quality BuildManifest run)
- c-2 → t-1 (full-profile exts), t-2 (stub exts restoring residual coverage), t-3
  (single-source derivation, extLang deleted, no-regression + clash guards)
- c-3 → t-4 (HOME-injected brand-new toml is detectable + recon-visible with zero
  Go change; t-1/t-2 supply the drop-in data that makes the proof possible)

All of c-1, c-2, c-3 owned.

## Judgment calls

- DetectLanguages derivation: chose UNION over the locked winner-take-all clash rule
  / rejected per-ext single-winner / why: shipped svelte (.ts/.tsx, pri 6) and
  typescript (.ts/.tsx, pri 4) already clash, so winner-take-all returns "svelte" for
  .ts and drops "typescript" — a c-2 regression. Union preserves every legacy language
  (c-2's stated mandate); the priority tie-break still governs Detect (single profile),
  which is what ext_clash_resolution's "reuses Detect's tie-break / never crash" cites.
  This is a locked-decision tension I am resolving toward the higher criterion (c-2).
- DetectLanguages signature: chose keep DetectLanguages(root string), call LoadAll
  internally / rejected adding a profiles []*Profile param / why: the param would
  ripple into both security/recon.go and quality/recon.go wrappers plus ~6 call sites
  and the dozen existing DetectLanguages(dir) tests — a wide regression surface for no
  gain. Owning R4 = proving the ripple stays at zero via the delegation guard, not
  introducing it. Internal LoadAll also makes the c-3 HOME-injection proof work end to
  end.
- JS profile id: chose id="javascript" / rejected id="node" from full_profile_set's
  "javascript/node" label / why: the legacy map keyed .js/.jsx -> "javascript"; the id
  IS the language token (ext_clash_resolution), so "node" would silently regress the
  .js language. "/node" is honored via [[package_managers]] + [runtime], not the id.
- c-1 exemplar: chose python (exts-only, no marker file) / rejected csharp or
  javascript as the c-1 proof / why: python has no root-marker clash, so Detect on a
  .py tree resolves cleanly with no risk of stealing or being stolen by an existing
  profile; javascript/csharp still ship but carry hijack-guard contracts instead.
- Stubs vs full split into two wave-1 tasks / rejected one "author all 10 profiles"
  task / why: different risks (R3 malformed-load/blast-radius vs R5 detect+analyzer+
  hijack) need different test contracts; one owner each keeps the risk map crisp.
- t-3 placed in wave 2 (not wave 1) / why: the rewrite *code* is profile-independent,
  but its no-regression test strictly needs t-1+t-2's exts to exist, so the task that
  bundles code+test depends on them.
```
```
risk: 4 tasks across 3 waves, criteria covered 3/3
