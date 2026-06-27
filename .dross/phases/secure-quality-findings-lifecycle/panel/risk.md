# risk lens — secure-quality-findings-lifecycle

Phase secure-quality-findings-lifecycle — 5 tasks across 3 waves

Bias: failure modes drive the graph. The dangerous parts — fingerprint
collisions / path drift, corrupt or half-written state files, a finding whose
file was deleted, a resolved item that returns, two findings that fingerprint
identically — are pulled into a single shared `internal/findings` package so each
hazard is owned and tested in exactly one place instead of being duplicated
(and able to rot independently) across the security and quality mirrors.

Wave 1
  t-1  Compute stable fingerprint and normalize paths
       files:    internal/findings/fingerprint.go, internal/findings/fingerprint_test.go
       covers:   c-1
       contract: Fingerprint(class, file, title) hashes ONLY class/dimension +
                 normalized-path + title (locked: no line number). If line ever
                 leaks into the hash, the "line drift keeps identity" test fails:
                 two inputs differing only in line must produce one fingerprint.
                 If normalization regresses, the path-variants test fails —
                 "./a/b.go", "a/b.go", "a//b.go" and a trailing-slash form must
                 collapse to one fingerprint; empty file or empty title must not
                 panic and must yield a deterministic, distinguishable key.

  t-2  Durable per-tool state store (atomic, tolerant)
       files:    internal/findings/state.go, internal/findings/state_test.go
       covers:   c-2
       contract: StateRecord{Fingerprint, State, Regressed, LastRun, Title, File}
                 keyed by fingerprint. State enum tracked|resolved|dismissed —
                 Valid() rejects "" and "bogus" (the invalid-state test). Load of
                 a missing state.toml returns an empty store, not an error (the
                 first-run test); Load of a garbled TOML returns an error and
                 never panics (the corrupt-state test). Save is atomic via
                 temp-file + rename — the interrupted-write test asserts a failed
                 encode leaves the prior state.toml intact, never truncated.

Wave 2 (depends t-1, t-2)
  t-3  Reconcile fresh findings against prior state
       files:    internal/findings/reconcile.go, internal/findings/reconcile_test.go
       covers:   c-1, c-3, c-4
       depends:  t-1, t-2
       contract: Reconcile(prior, fresh) is pure post-scan — it accepts already-
                 produced findings and never exposes prior state to its caller as
                 a scan input (the signature has no scanner hook; the
                 no-prejudice test asserts identical fresh input yields identical
                 fingerprints regardless of prior store contents). Branch-owned
                 tests that each fail in isolation:
                 - dismissed-fold: a fresh finding matching a dismissed record is
                   returned carried=dismissed, not new.
                 - resolved-fold: same for resolved.
                 - regressed: a resolved fingerprint that reappears stays
                   State=resolved AND sets Regressed=true (locked); a resolved
                   fingerprint absent this run stays resolved, Regressed=false.
                 - identical-fingerprint dedup: two fresh findings that fingerprint
                   identically reconcile to one durable record, not two.
                 - deleted-file: a prior tracked record whose file is absent from
                   this run is retained in the store (not dropped) and not marked
                   regressed.

Wave 3 (depends t-3)
  t-4  Wire security: adapter, post-scan reconcile, findings CLI
       files:    internal/security/lifecycle.go, internal/cmd/security.go,
                 internal/cmd/security_lifecycle_test.go
       covers:   c-1, c-2, c-3, c-5
       depends:  t-3
       contract: lifecycle.go adapts security.Finding (Class/Severity) to the
                 shared fingerprint inputs and resolves the state path to
                 .dross/security/state.toml (the gitignored-location test asserts
                 the path sits under the already-ignored .dross/security/). After
                 a run, reconciliation runs and persists. `dross security findings
                 <id> --state X`: unknown id errors instead of writing a record
                 (the unknown-id test); --state with an invalid value is rejected
                 by cobra/Valid() (the bad-flag test); a valid set then re-load
                 round-trips the state (the persistence test). `dross security
                 findings list` prints each tracked fingerprint with its current
                 state and a regressed marker — the list-shows-regressed test fails
                 if a Regressed record renders as plain resolved.

  t-5  Wire quality: adapter, post-scan reconcile, findings CLI
       files:    internal/quality/lifecycle.go, internal/cmd/quality.go,
                 internal/cmd/quality_lifecycle_test.go
       covers:   c-1, c-2, c-3, c-5
       depends:  t-3
       contract: Mirror of t-4 over quality.Finding (Dimension/Risk) and
                 .dross/quality/state.toml. `dross quality findings <id> --state
                 dismissed` then re-run reconciliation: the fold-survives-rerun
                 test asserts the dismissed item is folded (not relisted new) on
                 the next run. `dross quality findings list` renders state +
                 regressed; the unknown-id and invalid-state branches fail their
                 own tests exactly as in t-4. Shares t-1..t-3, so only the adapter
                 mapping and command wiring are quality-specific.

## Coverage
- c-1 (cross-run durable identity, match only at reconciliation): t-1, t-3, t-4, t-5
- c-2 (CLI set state tracked/resolved/dismissed, persisted): t-2, t-4, t-5
- c-3 (fold dismissed/resolved fresh match, scan still surfaced it): t-3, t-4, t-5
- c-4 (resolved reappears flagged regressed): t-3
- c-5 (list tracked findings with current state across runs): t-4, t-5

## Judgment calls
- Share, don't mirror: one `internal/findings` package owns fingerprint, state
  store, and reconciliation; rejected duplicating them into security/ and
  quality/ — duplication doubles the surface where a collision or corruption bug
  can hide and drift, the exact risk this lens prioritizes. The tool-specific
  Finding types stay separate; only a thin adapter is mirrored (t-4/t-5).
- Fingerprint excludes line number AND is collision-aware within a run (t-1 owns
  dedup of identical fingerprints), rather than treating same-fingerprint
  findings as distinct — chosen because the locked identity is class+file+title,
  so a genuine duplicate must collapse, not multiply.
- Atomic temp+rename save (t-2) over a plain in-place write — a crash or failed
  encode mid-save must not corrupt durable state that survives run-dir pruning;
  in-place writing trades that durability for nothing.
- Deleted-file findings are retained-tracked, not regressed and not dropped
  (t-3) — dropping would silently lose a human's tracked decision when a file is
  briefly absent; regressing would misreport. Owned by one explicit branch.
- Reconcile is a pure (prior, fresh)->result function with no scanner hook, so
  the locked "prior state never an input to the scan" constraint is structurally
  enforced and testable, not just documented.
