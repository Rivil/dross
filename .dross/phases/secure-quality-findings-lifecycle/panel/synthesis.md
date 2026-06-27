# Synthesis — secure-quality-findings-lifecycle

Cold judge over three independently-drafted plans (risk / mvp / verification).
I authored none. Skeleton = strongest draft; concrete improvements grafted from
the runners-up; genuine divergences recorded, not papered over.

## Scores

Scale: weak / ok / strong / best (relative to the other two drafts on that row).

| Dimension | risk (5t/3w) | mvp (2t/2w) | verification (6t/4w) |
|---|---|---|---|
| Criteria coverage | strong — full c-1..c-5 map, c-4 single-owner (t-3) | strong — all five, but compressed into 2 tasks | strong — full map + table, c-3/c-4 double-covered (t-3,t-4) |
| Test-contract specificity | best — names hazard tests (line-drift, path-variants, corrupt-state, interrupted-write atomic, deleted-file, identical-fp dedup) | ok — sharp asserts but bundled, blurred failure ownership | best — every test NAMED; DoesNotMutateScan structurally guards locked no-prejudice rule |
| Granularity | strong — fp/state/reconcile split + 2 wiring; CLI folded into wiring | weak — under-decomposed; t-1 bundles fp+state+reconcile, t-2 bundles both tools' CLI | best — finest clean single-test-surface units; shared cobra group is its own task |
| Wave correctness | strong — 3 waves, correct deps, parallel wiring | ok — 2 waves correct but t-2 is a fat both-tools commit, no parallelism | best — 4 waves, shared-group-before-wiring is the cleanest dependency expression |

**Skeleton: verification.** It wins granularity, wave correctness, and ties for
test-contract specificity. Its split of the shared cobra group into its own task
(t-4) — which both risk and mvp fold into per-tool wiring — gives the cleanest
ownership, and its named tests (esp. `TestReconcileDoesNotMutateScan`) turn the
locked "prior state never an input to the scan" decision into a falsifiable
guard. Its one real defect (adapter filename collision) is fixed by grafting
risk's `lifecycle.go` naming; its thin hazard contracts are thickened with
risk's atomic-save / corrupt-state / deleted-file / dedup branches.

## Merged plan

```
Phase secure-quality-findings-lifecycle — 6 tasks across 4 waves

Wave 1
  t-1  Add shared fingerprint function                         [verification]
       id:         t-1
       wave:       1
       files:      internal/findings/fingerprint.go,
                   internal/findings/fingerprint_test.go
       covers:     c-1
       contract:   Fingerprint(class, file, title) ignores line and normalizes
                   the path. If a line number enters the hash, or "./internal/x.go"
                   / "internal/x.go" / "internal//x.go" / trailing-slash forms
                   produce different fingerprints, TestFingerprintStableAcrossLine
                   AndPathDrift fails; two inputs differing only in title colliding
                   to one fingerprint fails TestFingerprintDistinctTitles. [graft
                   risk] empty file or empty title must not panic and must yield a
                   deterministic, distinguishable key.
       depends_on: (none)

  t-2  Add fingerprint-keyed state store         [verification + risk graft]
       id:         t-2
       wave:       1
       files:      internal/findings/state.go, internal/findings/state_test.go
       covers:     c-2
       contract:   TOML ledger keyed by fingerprint, fields {state, regressed,
                   title, file, class, last_run}. Save/Load dropping the regressed
                   or state field fails TestStoreRoundTrip; Get(fp) returning the
                   wrong entry after reload, or Valid() accepting "" / "bogus",
                   fails TestStoreKeyedLookupAndStateValidation. [graft risk] Load
                   of a missing state.toml returns an empty store, not an error
                   (first-run test); Load of garbled TOML returns an error and
                   never panics (corrupt-state test); Save is atomic via temp-file
                   + rename — a failed encode mid-save leaves the prior state.toml
                   intact, never truncated (interrupted-write test).
       depends_on: (none)

Wave 2 (depends t-1, t-2)
  t-3  Implement post-scan reconcile engine       [verification + risk graft]
       id:         t-3
       wave:       2
       files:      internal/findings/reconcile.go,
                   internal/findings/reconcile_test.go
       covers:     c-1, c-3, c-4
       contract:   Reconcile(store, []Item, runID) updates the store and returns a
                   Result partitioning new / folded / regressed. Failures:
                   - fresh Item matching a state=dismissed entry emitted as "new"
                     instead of folded → TestReconcileFoldsDismissed;
                   - same for state=resolved → TestReconcileFoldsResolved;
                   - a resolved entry that reappears not left state=resolved AND
                     regressed=true → TestReconcileResolvedReappearsStaysResolved
                     Regressed;
                   - a never-seen fingerprint not inserted state=tracked →
                     TestReconcileNewIsTracked;
                   - Reconcile mutating the input []Item (scan ledger), proving
                     prior state leaked back into the scan →
                     TestReconcileDoesNotMutateScan.
                   [graft risk] two fresh Items that fingerprint identically
                   reconcile to ONE durable record, not two → identical-fingerprint
                   dedup test; a prior tracked record whose file is absent this run
                   is retained (not dropped) and NOT marked regressed →
                   deleted-file-retention test.
       depends_on: t-1, t-2

Wave 3 (depends t-3)
  t-4  Build shared findings cobra group     [verification, corroborated mvp]
       id:         t-4
       wave:       3
       files:      internal/cmd/findings.go, internal/cmd/findings_test.go
       covers:     c-2, c-5
       contract:   newFindingsCmd(toolDescriptor) builds `findings {list,
                   reconcile, <id>}`; descriptor supplies state-dir path + a
                   run-dir-ledger → []Item loader. Failures:
                   - `findings <id> --state bogus` accepted not erroring →
                     TestFindingsStateFlagRejectsUnknown;
                   - `findings <id> --state resolved` not persisting under that
                     id's fingerprint → TestFindingsSetStatePersistsByFingerprint;
                   - `findings list` omitting a dismissed entry's state or the
                     regressed marker → TestFindingsListRendersStateAndRegressed;
                   - `findings reconcile <run-dir>` not folding a prior dismissed
                     finding end-to-end through the descriptor →
                     TestFindingsReconcileSubcommand.
                   (`reconcile` subcommand corroborated by mvp t-2; it exceeds the
                   locked cli_shape enum, which constrains state-setting, not the
                   reconcile trigger — see Disagreement 4.)
       depends_on: t-3

Wave 4 (depends t-4)
  t-5  Wire security findings group + adapter  [verification + risk filename graft]
       id:         t-5
       wave:       4
       files:      internal/cmd/security.go,
                   internal/security/lifecycle.go,
                   internal/cmd/security_findings_test.go
       covers:     c-1, c-2, c-5
       contract:   security descriptor maps the Item fingerprint source to Class
                   (NOT Severity — confirmed: security.Finding has Class + the
                   panel-judged Severity) and state-dir to .dross/security/
                   state.toml; Ledger.Items() adapts []Finding → []findings.Item.
                   Failures:
                   - `dross security findings` not registered on the tree →
                     TestSecurityFindingsRegistered;
                   - adapter feeding Severity where Class belongs, so two findings
                     of different class but same file+title collide →
                     TestSecurityItemUsesClass;
                   - `.dross/security/state.toml` not gitignored (git check-ignore)
                     → TestSecurityStateGitignored.
                   [filename: risk] adapter lives in internal/security/lifecycle.go,
                   NOT findings.go (that path already exists — see Disagreement 3).
       depends_on: t-4

  t-6  Wire quality findings group + adapter   [verification + risk graft]
       id:         t-6
       wave:       4
       files:      internal/cmd/quality.go,
                   internal/quality/lifecycle.go,
                   internal/cmd/quality_findings_test.go
       covers:     c-1, c-2, c-5
       contract:   quality descriptor maps the Item fingerprint source to Dimension
                   (NOT Risk — confirmed: quality.Finding has Dimension + the
                   panel-judged Risk) and state-dir to .dross/quality/state.toml;
                   Ledger.Items() adapts []Finding → []findings.Item. Failures:
                   - `dross quality findings` not registered →
                     TestQualityFindingsRegistered;
                   - adapter feeding Risk where Dimension belongs →
                     TestQualityItemUsesDimension;
                   - `.dross/quality/state.toml` not gitignored →
                     TestQualityStateGitignored.
                   [graft risk] dismiss via `findings <id> --state dismissed` then
                   re-run `findings reconcile`: the dismissed item is folded, not
                   relisted as new → fold-survives-rerun test (c-3 end-to-end).
                   [filename: risk] adapter in internal/quality/lifecycle.go.
       depends_on: t-4
```

Coverage roll-up: c-1 → t-1,t-3,t-5,t-6 · c-2 → t-2,t-4,t-5,t-6 ·
c-3 → t-3,t-4,t-6 · c-4 → t-3,t-4 · c-5 → t-4,t-5,t-6. All five accounted for.

## Disagreements

### 1. SHARE vs MIRROR (the headline question) — UNANIMOUS, not divergent
- risk: SHARE — one `internal/findings` package owns fingerprint/state/reconcile;
  mirror rejected because duplication doubles the surface where a collision or
  corruption bug can hide and drift.
- mvp: SHARE — the only per-tool delta is one field name (Class vs Dimension) +
  the state-dir path; mirroring roughly doubles the engine task for zero
  behavioural difference.
- verification: SHARE — reconcile has 5 distinct branches; duplicating doubles
  the verification surface for zero behaviour.
- **Provisional default: SHARE** (`internal/findings` core + thin per-tool
  adapter). Notable: the prompt flagged this as the likely fault line, but all
  three lenses converged independently. No dissent exists to preserve — recorded
  here so the consensus is visible rather than assumed.
- **Why it matters:** it is the load-bearing structural choice; had even one lens
  argued mirror, the task count and the c-3/c-4 test surface would roughly
  double. The unanimity is the strongest signal in the panel.

### 2. Granularity / task count: 2 (mvp) vs 5 (risk) vs 6 (verification)
- mvp: 2 tasks — bundle fp+state+reconcile into one, both tools' CLI into one.
- risk: 5 — fp/state/reconcile split, but the cobra group is folded into each
  tool's wiring task (t-4/t-5).
- verification: 6 — adds the shared cobra group as its OWN task (t-4) ahead of
  per-tool wiring (t-5/t-6).
- **Provisional default: verification's 6-task split.** mvp's 2-task bundling
  blurs which failure means what (its own concern: a fat t-1 owns 3 distinct test
  surfaces); risk's folded group duplicates wiring/flag logic across t-4/t-5.
- **Why it matters:** the shared group as a discrete task means the cobra wiring,
  `--state` validation, and `reconcile`/`list` rendering are tested once against a
  descriptor, and the per-tool tasks shrink to just the adapter + registration —
  smaller, parallel, single-purpose commits.

### 3. Adapter file location: `internal/*/lifecycle.go` (risk) vs
`internal/*/findings.go` (verification) vs inline-in-cmd, no internal file (mvp)
- risk: new `internal/security/lifecycle.go` / `internal/quality/lifecycle.go`.
- verification: new `internal/security/findings.go` / `internal/quality/findings.go`.
- mvp: no new internal file — cmd extracts the field and feeds primitives.
- **Provisional default: risk's `lifecycle.go`.** SANITY-CHECK CORRECTION:
  `internal/security/findings.go` and `internal/quality/findings.go` ALREADY EXIST
  (they hold the Finding/Ledger types). Verification's chosen filename collides
  with a live file; risk's `lifecycle.go` is a clean new path. mvp's inline-in-cmd
  is rejected because the Class/Dimension→Item adapter deserves its own
  unit-testable home (and cmd is already fat), and because t-5/t-6's
  `TestSecurityItemUsesClass` / `TestQualityItemUsesDimension` want a named
  `Items()` function to pin.
- **Why it matters:** picking verification's path as-written would force an
  executor to either append to an existing file (muddying the Finding-types file)
  or hit a create-collision. This is the one place the skeleton is concretely
  wrong and a runner-up is right.

### 4. Reconcile invocation: explicit `findings reconcile <run-dir>` subcommand
(mvp + verification) vs implicit "after a run, reconciliation runs and persists"
(risk)
- risk: t-4/t-5 say reconciliation "runs and persists" after a run, without
  naming the trigger surface.
- mvp + verification: an explicit `findings reconcile <run-dir>` subcommand the
  prompt invokes after findings.toml is populated.
- **Provisional default: explicit `findings reconcile <run-dir>` subcommand.**
  risk's "after a run" is untestable and ambiguous about where it hooks; `run`
  only creates the run dir BEFORE the scan writes findings, so reconciling there
  is both impossible and a risk of feeding state pre-scan (violating locked
  reconciliation_timing). An explicit post-scan command is testable end-to-end
  (t-4 `TestFindingsReconcileSubcommand`).
- **Why it matters:** it is the one CLI surface NOT in the locked cli_shape enum
  (`list` + `<id> --state`). Both mvp and verification argue the locked shape
  constrains state-SETTING, not the reconcile trigger — a defensible reading, but
  flag it for the human: if the lock is read strictly, reconcile must instead be
  auto-invoked (rejected by mvp as "untestable magic"). Provisional = explicit
  subcommand; escalate if the lock is meant to bar it.

### 5. State-store payload: denormalized display fields (risk + verification) vs
minimal (mvp leans light)
- risk + verification: store carries {title, file, class, last_run} alongside
  fingerprint→state, so `findings list` renders without re-reading a run dir.
- mvp: t-1 asserts state+regressed round-trip; does not emphasise display fields.
- **Provisional default: denormalize display fields** (verification's t-2
  contract). c-5 requires listing tracked findings "with their current state
  across runs" even after run-dir pruning, so a bare map[fingerprint]state can't
  render the list. Low-divergence — two of three agree and the third doesn't
  object.
- **Why it matters:** modest, but it makes the c-5 storage obligation falsifiable
  via t-2's round-trip test rather than discovering at list-time that the data to
  render isn't persisted.
