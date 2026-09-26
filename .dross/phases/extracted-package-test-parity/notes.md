# extracted-package-test-parity — execution notes

## t-11 — survivor drain (c-1)

Run: `dross survivor drain --packages ./internal/boardsync,./internal/diag,./internal/secretscan --phase extracted-package-test-parity`,
detached via nohup, 2026-09-26 17:05:53Z → 17:07:09Z (76 s). Pre-dispatch estimate: 419 go/ast mutant
sites (boardsync 264, diag 59, secretscan 96). gremlins ran on helicon.

| package              | killed | lived | not covered |
|----------------------|-------:|------:|------------:|
| internal/boardsync   |    265 |     0 |           0 |
| internal/diag        |     57 |     0 |           2 |
| internal/secretscan  |     75 |     0 |          26 |

First classification: 25 survivors — 14 accepted (pre-existing secretscan entries), 0 routed,
**11 outstanding**, every one with evidence `coverage=no coverage block`:

- internal/diag/redproof.go:91 `case pin.DocErr != nil:`, :94 `case pin.DocSHA == "":`
- internal/secretscan/writers.go:88/90/92 (ArtifactNames arms), :116 `case set == 0:`,
  :118 `case set > 1:` (NEGATION + BOUNDARY), :120/129/139 (ValidateWriters disposition arms)

All eleven are the predicted ceilings, and all are `switch {}` case conditions. The own-package
profile shows each `switch {` block ending on the switch line and every arm body starting on the
next line with count 1 — the tests run every arm; the condition line itself sits in no block, so
gremlins never builds the mutant.

**Disposition — deviation from the plan's mapping.** The plan maps evidence "no coverage block" to
`const-initializer-arithmetic`; that category's reason describes package-level declaration
initializers, which would be a false record for a case condition inside a function. The store
already accepts the identical shape (`secretscan/sinks.go` `case set == 0:` / `case set > 1:`) under
`gremlins-attribution-ceiling`, whose reason names switch-case conditions and the colon-started arm
block. All 11 were accepted there. Both categories are in the contract's allowed set.

Nine of the eleven reuse identity keys retired in t-2 (the same mutants): they return as accepted
under the ceiling category, not as outstanding and not as `cross-package-only-coverage`. No
acceptance exists for writers.go 106/110/112/121/130/133/136/140/144 — t-2's tests kill those.

Re-classification without re-running gremlins (no test was added):
`dross survivor drain --report reports/gremlins/internal_{boardsync,diag,secretscan}.json` →
**25 survivors — 25 accepted, 0 routed, 0 outstanding; "0 unclassified"; exit 0.**

Store checks: 0 `cross-package-only-coverage` acceptances (category pruned); the 25 acceptances on
the three packages are 22 `gremlins-attribution-ceiling` + 3 `const-initializer-arithmetic`;
`TestRepoAcceptanceReasonsCiteRealTests` passes.

The drain also prints "(112 routed to extracted-package-test-parity — this phase's own
destination, so still outstanding)": that counts routed deferred entries whose target is this
phase. None of their mutants survive any more (boardsync: 0 survivors); they close through the
backlog lifecycle when this phase ships.

Deferred: `3d7fab45c3b0a674` (DRO-739) — `survivor.Derive` labels a switch-case condition "no
coverage block: a const initializer or declaration", pointing the operator at the wrong category.
