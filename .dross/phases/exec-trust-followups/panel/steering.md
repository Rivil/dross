# Panel steering record — exec-trust-followups

User decisions on `synthesis.md`'s seven disagreements, taken during
`/dross-plan --panel` on 2026-08-07. Six resolved; D-7 open.

`plan.toml` has NOT been written. On resume, settle D-7, then write the plan
from `synthesis.md`'s merged plan with these six amendments applied (all six
confirmed the judge's provisional default, so the merged plan stands as written
for D-1 through D-6).

| id | decision | resolution |
|---|---|---|
| D-1 | Consent store home | **`internal/cmd`** (judge's pick). Reuses `readAllowHosts`' tracked-file refusal in place; a separate `internal/exectrust` would need an import of `internal/cmd` or a second copy of that refusal. |
| D-2 | Argv fence shape | **Shared `internal/argfence` package** (judge's pick). One table read by both the runtime and the audit gate, per the locked `analyzer_arg_policy`; two tables would drift silently. |
| D-3 | Audit-gate rename | **Rename to `subprocargs_audit_test.go`** (judge's pick). Pays the `ARCHITECTURE.md:181-182` landmark update inside t-8. |
| D-4 | Token scrub site | **Construction boundary only** (judge's pick). No `telemetry.Detail` scrubbing, no global `RegisterSecret` registry; t-9's canary test is the proof that telemetry/stdout/stderr stay clean. |
| D-5 | c-4 (gh) as its own task | **Keep t-5 separate** (judge's pick). Four gh sites, not mvp's claimed two; and the fix must land before the gate that polices it so the gate is observed red-then-green. |
| D-6 | Consent-gated command set | **Include `changes record`** (judge's pick). Gated set = {verify, task next, task status(in_progress), state bump, changes record}, pinned by `TestExecGatedSetIsExplicit`. `task status … done` stays open. |
| D-7 | t-10 doctor + prompt surfacing | **OPEN** — user stopped to clarify before answering. See below. |

## D-7 — open

Whether `t-10` (surface consent state in `dross doctor`; add the
`dross trust --check` pre-flight to `assets/prompts/{execute,quick,verify}.md`
with grep-based prompt-content tests) stays a task of its own.

- **risk:** yes, its own task — doctor + three prompt files.
- **mvp:** fold the prompt pre-flight into t-6; no doctor work.
- **verification:** no task at all; `dross trust --check` merely exists so
  prompts *can* pre-flight.
- **Judge's provisional default:** keep it as wave-3 `t-10`.
- **The tension the judge flagged:** it is the weakest task in the plan by
  construction — a grep over prompt text proves the line is present, not that
  the agent honours it. But it is also the half the locked `exec_consent_gate`
  decision openly admits the CLI cannot enforce.
- **Note:** r-01 applies to the prompt edits — not live until `make install`.

The user asked to clarify rather than answer. Re-open the question with whatever
they raise; do not assume the default carries.
