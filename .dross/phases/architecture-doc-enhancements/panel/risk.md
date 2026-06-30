# Risk-lens decomposition — architecture-doc-enhancements

Bias: failure modes drive the graph. The dominant shared hazard is **symbol
resolution against a moving codebase** (false matches, duplicate names,
renamed/deleted symbols, ambiguous bullets). It is isolated into one wave-1 task
(t-2) so the false-match risk is owned and tested in exactly one place, then
consumed by both the advisory path (c-3) and the repair path (c-4). The other
hazards — a delimiter char surviving the structured landmark array, clobbering
hand edits on regenerate, a stale link accidentally blocking the loop, and an
in-place rewrite corrupting untouched prose — each get a single owner.

```
Phase architecture-doc-enhancements — 6 tasks across 2 waves

Wave 1
  t-1  Typed --landmark capture in changes
       files:    internal/changes/changes.go, internal/changes/changes_test.go,
                 internal/cmd/changes.go
       covers:   c-1
       contract: A `--landmark "what=a=b · c"` round-trips through changes.json
                 with the value's `=` and `·` intact (SplitN on first `=` only,
                 array never re-flattened to a notes string) — test fails if the
                 value is re-split; two `--landmark` flags yield a 2-element
                 Landmarks array; `--landmark feature` with no `=` returns a
                 parse error instead of recording an empty key.

  t-2  Architecture link parse + symbol resolver
       files:    internal/architecture/links.go,
                 internal/architecture/links_test.go
       covers:   c-3, c-4
       contract: A `- Pkg.Symbol — internal/x/y.go:40` bullet whose symbol now
                 sits at line 55 classifies Moved{55}; a symbol with two
                 declarations in the same file classifies Ambiguous (never a
                 silent first-match pick); a deleted/renamed symbol classifies
                 Unresolved; a bullet missing `file:line` is Skipped without
                 aborting the remaining bullets; the em-dash separator and a
                 `:line` with trailing text both parse.

  t-5  Prompts emit & read typed landmarks
       files:    assets/prompts/execute.md, assets/prompts/ship.md,
                 internal/cmd/execute_prompt_test.go,
                 internal/cmd/ship_prompt_test.go
       covers:   c-1
       contract: execute_prompt_test asserts execute.md emits
                 `dross changes record … --landmark feature=… --landmark loc=…
                 --landmark symbol=… --landmark what=…` and no longer the
                 `--notes "feature: …"` landmark form; ship_prompt_test asserts
                 ship.md §3.5 reads structured fields from `dross changes show`
                 JSON, not by parsing the notes string. Either test fails if the
                 legacy notes-as-landmark instruction survives.

  t-6  Lift first-creation guard, safe refresh-merge
       files:    assets/prompts/architecture.md,
                 internal/cmd/architecture_prompt_test.go
       covers:   c-2
       contract: architecture_prompt_test asserts §0.3 no longer instructs
                 "stop / First-creation only" when ARCHITECTURE.md exists and
                 instead instructs the heading-keyed in-place refresh (refresh
                 symbol bullets + provenance, keep existing one-line unless
                 empty, flag entries the scan didn't rediscover, never silently
                 drop). Test fails if the stop-on-exists language remains.

Wave 2 (depends t-2)
  t-3  doctor advisory stale-link section
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
       covers:   c-3
       depends:  t-2
       contract: TestDoctorLinksAdvisory — an ARCHITECTURE.md with one Moved and
                 one Unresolved bullet prints two `⚠` link lines, yet the
                 `issues` counter feeding finalizeDoctor (and thus the exit code)
                 is identical to the same repo without staleness; links never
                 increment `issues`. A repo with no ARCHITECTURE.md emits no link
                 section and no error.

  t-4  dross architecture check [--fix] subcommand
       files:    internal/cmd/architecture.go,
                 internal/cmd/architecture_test.go, cmd/dross/main.go
       covers:   c-4
       depends:  t-2
       contract: TestArchitectureCheckFix — `architecture check --fix` rewrites
                 only the `:line` suffix of a Moved bullet and leaves every other
                 byte (heading, one-line, provenance, healthy bullets) identical;
                 an Ambiguous/Unresolved bullet is left verbatim (never repointed
                 to a guessed line); `architecture check` without `--fix` writes
                 nothing (file bytes unchanged). `dross architecture check -h`
                 resolves (registered via main.go AddCommand + survives
                 EnforceSubcommandKnown).
```

## Coverage

- c-1 (typed repeatable --landmark; execute emits; ship reads) → t-1, t-5
- c-2 (regenerate over existing doc without clobbering) → t-6
- c-3 (doctor advisory stale-link report) → t-2, t-3
- c-4 (architecture check --fix re-resolves moved symbols) → t-2, t-4

## Judgment calls

- Extracted the parse+resolve core (t-2) into its own wave-1 task rather than
  duplicating it inside doctor (c-3) and check --fix (c-4); rejected per-command
  resolvers because two copies = two places for a false-match bug to hide and
  double the test surface for the same hazard.
- Reused internal/codex (Index → Symbols{Name,Kind,File,Line}) as the resolver
  engine; rejected hand-rolling go/ast in this package — codex already resolves
  symbols-to-lines and the duplicate-name/Kind data needed to flag Ambiguous is
  already there.
- Made Ambiguous a first-class classification (not "pick the first match");
  rejected silent best-effort because c-4 would then repoint a bullet to the
  wrong line — a confident-wrong repair is worse than leaving the stale link.
- Put t-5/t-6 (prompt edits) in wave 1, not behind t-1; the landmark field names
  (feature/symbol/loc/what) are pinned by the landmark_field_shape locked
  decision, so the prompt contract doesn't strictly need t-1's code output —
  drift is caught by pinning the exact flag/key strings in the prompt tests.
- Kept t-5 (c-1 prompts) and t-6 (c-2 prompt) separate despite both being
  prompt-only; rejected merging because they own different failure modes
  (landmark emit/read drift vs. regenerate-clobber) and the test contracts bite
  on different criteria.
- Merged the landmark data model (internal/changes) and the --landmark flag
  parsing (internal/cmd) into one task (t-1); rejected splitting because the CLI
  layer strictly needs the model and both serve a single hazard (delimiter
  survival in the structured array), so a wave boundary would only cost
  parallelism.
- Left validate.go untouched: the link_check locked decision forbids hard-failing
  links there, so the stale-link surface lives only in doctor (advisory) and
  architecture check (opt-in fix).
```
risk: 6 tasks across 2 waves, criteria covered 4/4
```
