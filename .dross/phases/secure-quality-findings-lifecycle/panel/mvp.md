Phase secure-quality-findings-lifecycle — 2 tasks across 2 waves

Lens: MVP. One shared engine + one thin CLI-wiring task. No per-tool
duplication of the lifecycle logic; cmd extracts the single differing field
(Class vs Dimension) and feeds primitives to the shared package.

Wave 1
  t-1  Build shared findings-state engine
       files:    internal/findings/state.go, internal/findings/reconcile.go,
                 internal/findings/state_test.go
       covers:   c-1, c-3, c-4, c-5
       contract: - if Fingerprint folds in the line number, the
                   fingerprint-stability test fails (same class+file+title at
                   two different lines must hash identically) — c-2 fingerprint
                   decision.
                 - if Reconcile lists a fresh finding whose fingerprint matches
                   a dismissed prior entry as new instead of folding it to
                   state=dismissed, the dismissed-fold test fails — c-3.
                 - if a state=resolved entry whose fingerprint reappears in the
                   fresh set is not left state=resolved AND marked
                   regressed=true, the resolved-regression test fails — c-4.
                 - if a brand-new fingerprint is not added as state=tracked,
                   the new-tracked-identity test fails — c-1.
                 - if StateLedger Load/Save does not round-trip an entry's
                   state+regressed fields through TOML, the persistence test
                   fails — c-5 backing store.

Wave 2 (depends t-1)
  t-2  Wire findings CLI group for both tools
       files:    internal/cmd/security.go, internal/cmd/quality.go,
                 internal/cmd/findings_test.go
       covers:   c-1, c-2, c-3, c-4, c-5
       contract: - if `dross <tool> findings <id> --state dismissed` does not
                   resolve the per-run finding id (from the latest run's
                   findings.toml) to its fingerprint and persist
                   state=dismissed in <tool>/state.toml, the set-state cmd test
                   fails — c-2.
                 - if `--state open` (or any value outside
                   tracked/resolved/dismissed) is accepted instead of erroring,
                   the state-validation test fails — c-2.
                 - if `dross <tool> findings reconcile <run-dir>` does not read
                   that run's findings.toml, call the engine, and write the
                   updated state.toml, the reconcile-cmd test fails — c-1/c-3.
                 - if `dross <tool> findings list` output omits the current
                   state column or the regressed marker for a regressed entry,
                   the list-output test fails — c-4/c-5.
                 - if the security `findings` group reads class but quality
                   reads dimension wrong (category field swapped), the
                   per-tool-category test fails for whichever tool — c-1.

## Coverage
- c-1 (post-scan reconcile to durable identity): t-1 (new→tracked, post-scan
  Reconcile over fingerprints), t-2 (`findings reconcile <run-dir>` wiring)
- c-2 (CLI set tracked/resolved/dismissed, persisted): t-2 (`findings <id>
  --state`, validation), t-1 (StateLedger Load/Save persistence)
- c-3 (fold fresh match to dismissed/resolved carried state): t-1 (fold
  branch), t-2 (reconcile cmd surfaces folded vs new)
- c-4 (resolved-reappear → regressed): t-1 (regressed marking), t-2 (list shows
  regressed marker)
- c-5 (list tracked findings with current state across runs): t-2 (`findings
  list`), t-1 (StateLedger entries + deterministic ordering)

## Judgment calls
- Shared `internal/findings` package over mirroring the lifecycle into both
  internal/security and internal/quality. Chose shared: the only per-tool
  difference is one field name (Class vs Dimension) and the state-dir path;
  duplicating Reconcile + StateLedger into two packages would roughly double the
  engine task for zero behavioral difference. Rejected the mirror because it
  multiplies tasks without a criterion to justify it.
- Engine takes primitives `Fingerprint(category, file, title string)` rather
  than a shared Finding interface both types implement. Chose primitives: cmd
  already imports both packages and does the trivial field extraction, so no new
  interface and no import-cycle risk. Rejected an interface as speculative
  structure.
- Added `findings reconcile <run-dir>` as a third subcommand even though the
  locked cli_shape only enumerates `list` and `<id> --state`. Reconciliation
  must be invokable to satisfy c-1/c-3/c-4, and the locked shape constrains the
  state-setting surface, not reconciliation. Rejected auto-reconcile-on-list as
  untestable magic.
- `findings <id> --state` resolves the per-run id against the latest run dir
  (lexically-greatest name; run ids already sort chronologically per run.go).
  Rejected adding a `--run` flag (not in the locked shape) — latest run is the
  obvious default.
- No .gitignore task: `.dross/security/` and `.dross/quality/` are already
  fully gitignored, so top-level state.toml under them is covered. Rejected
  adding a redundant ignore task.
