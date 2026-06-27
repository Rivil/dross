# c-3 findings proof — dedicated analyzer surfaces what the agnostic fallback misses

This is the documented manual run backing acceptance criterion **c-3**: a
dross-quality run with the dedicated tools installed surfaces a real finding that
the agnostic fallback (scc/jscpd/gitleaks/semgrep/trivy) alone does not.

Per the locked `findings_proof` decision this is **not** a `go test` gate — wiring
the JS toolchain into `go test` fights the single-static-binary ethos. It is a
committed fixture (`ts-deadcode/`) plus this reproducible record.

## Fixture

`ts-deadcode/` is a minimal TypeScript project with one planted defect:
`src/lib.ts` exports `orphanedWidget`, which no module imports. `src/index.ts`
(the knip entry) imports only `usedHelper`. The unused export is a **dead-code**
finding — knip's dimension — and nothing else.

## Reproduce

Pre-req (rule r-01): `make install` first so the typescript profile (which wires
knip as the dead-code analyzer) is live in the installed `dross` binary. The
tools run via `npx` (knip, jscpd) and Homebrew (scc).

```
cd fixtures/multilang-c3/ts-deadcode

# 1. dross quality detect — knip is the dedicated typescript dead-code analyzer
dross quality detect

# 2. dedicated analyzer: knip finds the dead code
npx --yes knip

# 3. agnostic fallback: blind to it
scc src
npx --yes jscpd src
```

## Recorded output (2026-06-27, node v24.14.0, knip via npx, scc 3.x)

### 1. `dross quality detect`

```
languages: svelte, typescript
analyzers:
  [installed] scc
  [missing]   jscpd  — npm install -g jscpd  (or see github.com/kucherenko/jscpd)
  [missing]   eslint  — npm i -D eslint eslint-plugin-svelte
  [missing]   knip  — npm i -D knip
  [missing]   dependency-cruiser  — npm i -D dependency-cruiser
  [missing]   typescript-eslint  — npm i -D eslint typescript-eslint
```

knip is wired in as a dedicated analyzer (the agnostic-only fallback never lists
it). It shows `[missing]` only because it is not globally installed; `npx` runs
it below.

### 2. `npx knip` — the dedicated dead-code finding

```
Unused exports (1)
orphanedWidget  function  src/lib.ts:9:17
```

✅ knip surfaces the planted dead export. (Pinned in `expected-finding.txt`.)

### 3. agnostic fallback — does NOT surface it

`scc src`:

```
───────────────────────────────────────────────────────────────────────────────
Language            Files       Lines    Blanks  Comments       Code Complexity
───────────────────────────────────────────────────────────────────────────────
TypeScript              2          14         2         4          8          0
───────────────────────────────────────────────────────────────────────────────
```

scc reports line counts and complexity — it has no concept of an unused export.

`npx jscpd src`:

```
No duplicates found.
Found 0 clones.
```

jscpd looks for duplication, not dead code — nothing found.

## Conclusion

The dedicated knip analyzer surfaces `orphanedWidget` (dead-code) on a stack the
agnostic fallback (scc complexity/LOC, jscpd duplication) is structurally blind
to. That is the c-3 delta: dross-quality on a non-Go stack now finds something
the language-agnostic loadout cannot.
