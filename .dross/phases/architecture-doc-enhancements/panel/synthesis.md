# Synthesis — architecture-doc-enhancements

Cold judge over three independent drafts (risk / mvp / verification). I authored
none of them. All referenced source files were checked against the tree:
`internal/changes/changes.go` (Changes.Tasks map, TaskRecord.Notes,
Record(...notes)), `internal/cmd/changes.go` (`record` with `--notes` StringVar),
`internal/cmd/doctor.go` (`issues` counter + `finalizeDoctor(issues)` returning
an error when `issues>0`), `cmd/dross/main.go` (`root.AddCommand(...)` block, no
`architecture` command yet, `EnforceSubcommandKnown(root)`),
`internal/codex/codex.go` (`Symbol{Name,Kind,File,Line}`, `Index()`),
`assets/prompts/execute.md` (lines 189-198 route the landmark through `--notes`),
`assets/prompts/ship.md` (§3.5 reads `notes` as the landmark),
`assets/prompts/architecture.md` (§0.3 "First-creation only … stop"). The new
files each draft creates (`internal/architecture/links.go`, `merge.go`,
`fix.go`, `internal/cmd/architecture.go`, `internal/cmd/architecture_prompt_test.go`)
do not yet exist — correct. No locked decision (landmark_field_shape,
refresh_merge_strategy, link_check, legacy_notes) is contradicted by any draft.

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|-------|-------------------|---------------------------|-------------|------------------|
| risk | 4/4; c-1→t1,t5 · c-2→t6 · c-3→t2,t3 · c-4→t2,t4 — full, shared resolver double-owned | Highest: named tests + exact invariants (delimiter/`·` survival, Ambiguous as first-class, byte-identical `:line` rewrite, `issues`-counter invariance, `-h` survives EnforceSubcommandKnown) | Clean: 6 tasks, resolver isolated as one wave-1 owner; landmark model+CLI merged with stated reason | Correct: t-2 in wave 1, t-3/t-4 depend t-2 in wave 2; prompt tasks parallel (field names locked) |
| mvp | 4/4 but thin: each criterion mostly single-owner; resolver folded into the doctor task | Medium: "grep guard" for prompts; resolver contract only ok/moved/missing — no Ambiguous, weakest on false-match hazard | Leanest (5): resolver-in-doctor couples c-3's engine inside a cmd package, awkward for c-4 reuse | Correct but muddier: t-5 (c-4) depends on a *doctor* task because the engine lives there |
| verification | 4/4, richest cross-map, but over-attributes c-1 to the merge fn (t-3 covers c-1+c-2); **defect: t-1 omits `internal/cmd/changes.go`, the `--landmark` CLI file c-1 names** | Highest on Go-testability: every criterion gets a callable Go surface + named falsifiable tests | Most granular (8): merge split to its own Go task (justified by 4-clause locked decision); fix.go split from cmd (borderline) | Correct: wave-2 t-3/t-4/t-6 all gate on wave-1 t-1/t-2 |

**Skeleton: `risk`.** It has the sharpest, most falsifiable contracts, correct
waves, and the best granularity middle-ground — the symbol resolver isolated as a
single shared wave-1 owner (cleaner than mvp's fold-into-doctor, leaner than
verification's 8-way split) directly serves the dominant shared hazard
(false symbol matches against a moving codebase). Grafts pull verification's
named-test rigor and ParseDoc-shared-parser framing, and mvp/risk's complete
t-1 file set onto it.

## Merged plan

Phase **architecture-doc-enhancements** — 6 tasks across 2 waves

### Wave 1

**t-1 — Typed `--landmark` capture in changes**  `[risk+mvp]`
- files: `internal/changes/changes.go`, `internal/changes/changes_test.go`, `internal/cmd/changes.go`
- covers: c-1
- contract: add `Landmarks []Landmark{Feature,Symbol,Loc,What}` to `TaskRecord`
  (`Notes` stays a valid free-form field, no longer a landmark — legacy_notes);
  `dross changes record` gains a repeatable `--landmark key=value` flag.
  `--landmark "what=a=b · c"` round-trips through changes.json with the value's
  `=` and `·` intact (SplitN on the first `=` only; array never re-flattened to a
  notes string); two `--landmark` flags yield a 2-element `Landmarks` array;
  `--landmark feature` (no `=`) returns a parse error, never an empty key.
  *(graft: verification's named `TestParseLandmark` / `TestLandmarkRoundTrip`;
  `internal/cmd/changes.go` retained from risk/mvp — verification dropped it.)*
- depends_on: —

**t-2 — Architecture link parse + symbol resolver**  `[risk+verification]`
- files: `internal/architecture/links.go`, `internal/architecture/links_test.go`
- covers: c-3, c-4
- contract: `ParseDoc` returns one Entry per `###` heading carrying its one-line,
  `[]SymbolLink{Symbol,File,Line}` bullets, and provenance (single shared parser
  consumed by t-3 and t-4 — no per-command scanners). A
  `- Pkg.Symbol — internal/x/y.go:40` bullet whose symbol now sits at line 55
  classifies `Moved{55}`; two declarations of the same name in one file classify
  `Ambiguous` (never a silent first-match); a deleted/renamed symbol is
  `Unresolved`; a bullet missing `file:line` is `Skipped` without aborting the
  rest; em-dash separator and a `:line` with trailing text both parse. Resolver
  engine reuses `internal/codex` (`Symbol{Name,Kind,File,Line}`).
  *(graft: verification's shared-ParseDoc framing onto risk's Ambiguous-as-first-class contract.)*
- depends_on: —

**t-5 — Prompts emit & read typed landmarks**  `[risk+mvp+verification]`
- files: `assets/prompts/execute.md`, `assets/prompts/ship.md`, `internal/cmd/execute_prompt_test.go`, `internal/cmd/ship_prompt_test.go`
- covers: c-1
- contract: execute.md emits `dross changes record … --landmark feature=…
  --landmark symbol=… --landmark loc=… --landmark what=…` and no longer the
  `--notes "feature: …"` landmark form; ship.md §3.5 reads the structured fields
  from `dross changes show` JSON, not by parsing the notes string. Both
  prompt-grep tests fail if any legacy notes-as-landmark instruction survives.
- depends_on: —  *(field names are pinned by landmark_field_shape, so this does
  not strictly gate on t-1's binary; drift is caught by the exact flag/key strings in the grep test.)*

**t-6 — Lift first-creation guard, safe refresh-merge**  `[risk+mvp]`
- files: `assets/prompts/architecture.md`, `internal/cmd/architecture_prompt_test.go`
- covers: c-2
- contract: §0.3 no longer instructs "First-creation only / stop" when
  ARCHITECTURE.md exists; instead instructs the heading-keyed in-place refresh
  (refresh symbol bullets + provenance, keep existing one-line unless empty, add
  new capabilities, flag — never silently drop — entries the scan didn't
  rediscover), gated by the existing propose→approve diff. Test fails if the
  stop-on-exists language remains or the feature-keyed merge step is absent.
- depends_on: —

### Wave 2

**t-3 — doctor advisory stale-link section**  `[risk+verification]`
- files: `internal/cmd/doctor.go`, `internal/cmd/doctor_test.go`
- covers: c-3
- contract: doctor gains an "Architecture links:" section that runs t-2's
  resolver and prints one `⚠` per Moved/Unresolved bullet, yet the `issues`
  counter feeding `finalizeDoctor` is identical to the same repo without
  staleness — links never increment `issues`. A repo with no ARCHITECTURE.md
  emits no link section and no error. Test asserts `finalizeDoctor`'s
  return/issue-count, not stdout text (the only falsifiable form of "never
  blocks").  *(graft: verification's finalizeDoctor-return targeting.)*
- depends_on: t-2

**t-4 — `dross architecture check [--fix]` subcommand**  `[risk+mvp+verification]`
- files: `internal/cmd/architecture.go`, `internal/cmd/architecture_test.go`, `cmd/dross/main.go`
- covers: c-4
- contract: `architecture check --fix` rewrites only the `:line` suffix of a
  Moved bullet and leaves every other byte (heading, one-line, provenance,
  healthy bullets) identical; an Ambiguous/Unresolved bullet is left verbatim
  (never repointed to a guessed line); `architecture check` without `--fix`
  writes nothing. Registered via `main.go root.AddCommand` and survives
  `EnforceSubcommandKnown` (`dross architecture check -h` resolves).
- depends_on: t-2

### Coverage
c-1 → t-1, t-5 · c-2 → t-6 · c-3 → t-2, t-3 · c-4 → t-2, t-4. (4/4)

## Disagreements

**1. Refresh-merge engine: prompt-only vs a Go `MergeLandmarks` function.**
- risk (t-6) and mvp (t-2): c-2 is a pure prompt edit — the LLM performs the
  feature-keyed merge under the existing propose→approve diff gate.
- verification (t-3): extracts `internal/architecture/merge.go`
  `MergeLandmarks(doc, []changes.Landmark) -> (string, []warnings)`, arguing the
  locked refresh_merge_strategy has four falsifiable clauses (in-place refresh,
  keep one-line, append new, flag-don't-drop) that only a function can mechanically check.
- **Provisional default: prompt-only (risk/mvp).** `dross-architecture` and
  `dross-ship` are prompt-orchestrated; the locked decision *names* the
  propose→approve diff as the safety net, and there is no existing Go merge
  engine (`internal/architecture/architecture.go` is a scan/backfill engine, not
  a doc-merger). Matching the existing orchestration keeps the surface minimal.
- **Why it matters:** if curation-clobber on regenerate proves to bite in
  practice, verification's function is the upgrade path — it is the only version
  with a unit-test that can *prove* "never silently drop an entry the scan
  missed." Picking prompt-only trades that mechanical guarantee for one fewer Go
  surface; revisit if c-2 regressions surface.

**2. Resolver implementation: codex/AST vs textual search.**
- risk (t-2) and verification (t-2): resolve symbols via `internal/codex`
  (`Symbol{Name,Kind,File,Line}`), which already carries the duplicate-name /
  Kind data needed to flag `Ambiguous`.
- mvp (t-3): a language-agnostic textual search for the symbol token, classifying
  only ok / moved / missing.
- **Provisional default: codex-based (risk/verification).** Codex gives
  `Ambiguous` as a first-class status; a confident-wrong repair (mvp's textual
  search silently first-matching a duplicate name) is worse than leaving a stale
  link, which `architecture check --fix` (t-4) would otherwise repoint wrongly.
- **Why it matters:** codex is Go-only. If ARCHITECTURE.md ever links non-Go
  files, the codex resolver classifies them `Unresolved`/`Skipped` and mvp's
  textual fallback would have handled them. Acceptable now (the repo is
  single-language Go per project.toml), but the limitation is real and should be
  documented at t-2.

**3. Resolver placement: standalone shared task vs folded into doctor.**
- risk (t-2) and verification (t-2): the parse+resolve core is its own wave-1
  task, consumed by both the advisory path (c-3) and the repair path (c-4).
- mvp (t-3): the resolver lives inside the doctor task; c-4's `--fix` reuses it.
- **Provisional default: standalone (risk/verification).** One owner, one place
  for the false-match hazard to hide, one test surface; it also makes c-4's
  dependency point at a resolver task rather than at a doctor task.
- **Why it matters:** mvp's fold makes t-5(c-4) depend on a *doctor* task and
  buries a shared `internal/architecture` engine inside `internal/cmd` — a wrong
  package for code two commands import.

**4. `--fix` rewriter: split into `fix.go` vs inlined in the command.**
- risk (t-4) and mvp (t-5): the rewrite logic lives in `internal/cmd/architecture.go`.
- verification (t-4): splits a pure `RewriteMovedLinks(doc, resolutions)` into
  `internal/architecture/fix.go` plus a thin cobra wire.
- **Provisional default: inline in the command (risk/mvp).** The command is a
  thin wire over the resolver; a separate registration/rewriter split is
  sub-10-min busywork the granularity rule says to merge, and the byte-identity
  contract is testable through the cmd.
- **Why it matters:** low stakes. If the rewriter grows (e.g. shared with the
  merge engine of disagreement #1), promote it to `internal/architecture/fix.go`
  then; not worth an extra task now.

---
synthesis: 6 tasks across 2 waves, 4 disagreements
