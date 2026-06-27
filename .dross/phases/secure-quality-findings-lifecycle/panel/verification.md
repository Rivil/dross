# Verification-lens plan — secure-quality-findings-lifecycle

Bias: each task is the smallest unit that makes one ideal test contract satisfiable.
The durable cross-run logic (fingerprint → state store → reconcile) is *pure* and
identical between the two tools, so I pull it into a single shared `internal/findings`
package and write the table-tests **once** there; the per-tool layer stays a thin,
mirror-tested adapter (Class vs Dimension is the only real difference).

```
Phase secure-quality-findings-lifecycle — 6 tasks across 4 waves

Wave 1
  t-1  Add shared fingerprint function
       files:    internal/findings/fingerprint.go, internal/findings/fingerprint_test.go
       covers:   c-1
       contract: Fingerprint(class, file, title) ignores line and normalizes the path.
                 If a line number ever enters the hash, or "./internal/x.go" and
                 "internal/x.go" produce different fingerprints, TestFingerprintStable
                 AcrossLineAndPathDrift fails; if two findings differing only in title
                 collide to one fingerprint, TestFingerprintDistinctTitles fails.

  t-2  Add fingerprint-keyed state store
       files:    internal/findings/state.go, internal/findings/state_test.go
       covers:   c-2
       contract: Store is a TOML ledger keyed by fingerprint with fields
                 {state, regressed, title, file, class, last_run}. If Save/Load drops
                 the regressed flag or the state field, TestStoreRoundTrip fails; if
                 Get(fingerprint) returns the wrong entry after reload, or an unknown
                 --state value passes Valid(), TestStoreKeyedLookupAndStateValidation fails.

Wave 2 (depends t-1, t-2)
  t-3  Implement post-scan reconcile engine
       files:    internal/findings/reconcile.go, internal/findings/reconcile_test.go
       covers:   c-1, c-3, c-4
       contract: Reconcile(store, []Item, runID) updates the store and returns a Result
                 partitioning new / folded / regressed. Specific failures:
                 - a fresh Item whose fingerprint matches a state=dismissed entry emitted
                   as "new" instead of folded to dismissed → TestReconcileFoldsDismissed fails;
                 - same for a state=resolved entry → TestReconcileFoldsResolved fails;
                 - a resolved entry that reappears not left state=resolved AND regressed=true
                   → TestReconcileResolvedReappearsStaysResolvedRegressed fails;
                 - a never-seen fingerprint not inserted as state=tracked
                   → TestReconcileNewIsTracked fails;
                 - if Reconcile mutates the input []Item slice (the scan ledger), proving
                   prior state leaked back into the scan → TestReconcileDoesNotMutateScan fails.

Wave 3 (depends t-3)
  t-4  Build shared findings cobra group
       files:    internal/cmd/findings.go, internal/cmd/findings_test.go
       covers:   c-2, c-5
       contract: newFindingsCmd(toolDescriptor) builds `findings {list, reconcile, <id>}`,
                 where the descriptor supplies state-dir path + a run-dir-ledger→[]Item
                 loader. Specific failures:
                 - `findings <id> --state bogus` accepted instead of erroring
                   → TestFindingsStateFlagRejectsUnknown fails;
                 - `findings <id> --state resolved` not persisting resolved to state.toml
                   under that id's fingerprint → TestFindingsSetStatePersistsByFingerprint fails;
                 - `findings list` output omitting a dismissed entry's state or the
                   regressed marker → TestFindingsListRendersStateAndRegressed fails;
                 - `findings reconcile <run-dir>` not folding a prior dismissed finding
                   end-to-end through the descriptor → TestFindingsReconcileSubcommand fails.

Wave 4 (depends t-4)
  t-5  Wire security findings group + adapter
       files:    internal/cmd/security.go, internal/security/findings.go,
                 internal/cmd/security_findings_test.go
       covers:   c-1, c-2, c-5
       contract: security descriptor maps the Item fingerprint source to Class (not
                 Severity) and state-dir to .dross/security/state.toml; Ledger.Items()
                 adapts Findings → []findings.Item. Specific failures:
                 - `dross security findings` not registered on the command tree
                   → TestSecurityFindingsRegistered fails;
                 - the adapter feeding Severity where Class belongs, so two findings of
                   different class but same file+title collide → TestSecurityItemUsesClass fails;
                 - `.dross/security/state.toml` not gitignored
                   → TestSecurityStateGitignored (git check-ignore) fails.

  t-6  Wire quality findings group + adapter
       files:    internal/cmd/quality.go, internal/quality/findings.go,
                 internal/cmd/quality_findings_test.go
       covers:   c-1, c-2, c-5
       contract: quality descriptor maps the Item fingerprint source to Dimension (not
                 Risk) and state-dir to .dross/quality/state.toml; Ledger.Items() adapts
                 Findings → []findings.Item. Specific failures:
                 - `dross quality findings` not registered → TestQualityFindingsRegistered fails;
                 - the adapter feeding Risk where Dimension belongs, so two findings of
                   different dimension but same file+title collide → TestQualityItemUsesDimension fails;
                 - `.dross/quality/state.toml` not gitignored → TestQualityStateGitignored fails.
```

## Coverage

| Criterion | Delivered by |
|---|---|
| c-1 (one durable identity, match only at reconciliation) | t-1, t-3, t-5, t-6 |
| c-2 (CLI set state tracked/resolved/dismissed, persisted) | t-2, t-4, t-5, t-6 |
| c-3 (fold fresh match of dismissed/resolved; scan unprejudiced) | t-3, t-4 |
| c-4 (resolved-reappears flagged regressed) | t-3, t-4 |
| c-5 (list tracked findings + state across runs) | t-4, t-5, t-6 |

All of c-1..c-5 accounted for.

## Judgment calls

- **Shared `internal/findings` core, not a third mirror.** Chose one pure package
  (fingerprint + state + reconcile) tested once; rejected copy-pasting the new logic
  into `internal/security` and `internal/quality` as the existing files do. Reason
  under this lens: reconcile has five distinct branches (new/dismissed-fold/resolved-fold/
  regressed/no-mutation) — duplicating them doubles the verification surface for zero
  behavioural difference. The Class-vs-Dimension delta is the *only* real divergence,
  and that lives in the thin per-tool `Items()` adapter (t-5/t-6), which is exactly what
  those mirror tests pin.
- **Reconciliation is a `findings reconcile <run-dir>` subcommand, not folded into `run`.**
  Chose an explicit post-scan command the prompt invokes after `findings.toml` is
  populated; rejected reconciling inside `dross <tool> run` because `run` only creates the
  run dir before the scan writes findings — reconciling there is impossible and would also
  risk feeding state pre-scan, violating the locked reconciliation_timing decision. The
  `DoesNotMutateScan` contract (t-3) is the test that guards that boundary.
- **State store carries display fields (title/file/class), not just fingerprint→state.**
  Chose to denormalize enough to render `findings list` without re-reading a run dir;
  rejected a bare `map[fingerprint]state` because c-5 must list findings "with their
  current state across runs" even when no run dir survives pruning. The round-trip test
  (t-2) is what makes that storage obligation falsifiable.
- **No gitignore task.** `.dross/security/` and `.dross/quality/` are already ignored as
  whole directories, so `state.toml` is covered; I fold a `git check-ignore` assertion into
  t-5/t-6 as a guard rather than spending a task on it.
- **fingerprint and state split into t-1/t-2 despite both being wave-1 substrate.** Each
  owns a sharp, separable test surface (hash stability vs. round-trip/lookup); merging would
  blur which failure means what. Both still parallel.
