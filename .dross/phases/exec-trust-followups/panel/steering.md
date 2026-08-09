# Panel steering record — exec-trust-followups

User decisions on `synthesis.md`'s seven disagreements. D-1…D-6 were taken during
`/dross-plan --panel` on 2026-08-07; D-7 was settled on resume, 2026-08-08.

**All seven resolved, and every one confirmed the judge's provisional default** —
so `synthesis.md`'s merged plan stood as written. `plan.toml` was written from it
on 2026-08-08: 10 tasks across 3 waves, coverage 5/5.

| id | decision | resolution |
|---|---|---|
| D-1 | Consent store home | **`internal/cmd`** (judge's pick). Reuses `readAllowHosts`' tracked-file refusal in place; a separate `internal/exectrust` would need an import of `internal/cmd` or a second copy of that refusal. |
| D-2 | Argv fence shape | **Shared `internal/argfence` package** (judge's pick). One table read by both the runtime and the audit gate, per the locked `analyzer_arg_policy`; two tables would drift silently. |
| D-3 | Audit-gate rename | **Rename to `subprocargs_audit_test.go`** (judge's pick). Pays the `ARCHITECTURE.md:181-182` landmark update inside t-8. |
| D-4 | Token scrub site | **Construction boundary only** (judge's pick). No `telemetry.Detail` scrubbing, no global `RegisterSecret` registry; t-9's canary test is the proof that telemetry/stdout/stderr stay clean. |
| D-5 | c-4 (gh) as its own task | **Keep t-5 separate** (judge's pick). Four gh sites, not mvp's claimed two; and the fix must land before the gate that polices it so the gate is observed red-then-green. |
| D-6 | Consent-gated command set | **Include `changes record`** (judge's pick). Gated set = {verify, task next, task status(in_progress), state bump, changes record}, pinned by `TestExecGatedSetIsExplicit`. `task status … done` stays open. |
| D-7 | t-10 doctor + prompt surfacing | **Keep as its own wave-3 `t-10`** (judge's pick), both halves intact — consent state in `dross doctor`, plus the `dross trust --check` pre-flight in `assets/prompts/{execute,quick,verify}.md` with grep-based prompt-content tests. Resolved on resume, 2026-08-08. It is knowingly the weakest task in the plan — a grep proves the line is present, not that the agent honours it — but it is the half the locked `exec_consent_gate` decision openly admits the CLI cannot enforce, and the prompt-content tests are the only thing stopping the prompts drifting back to a raw `<runtime.test_command>` instruction. r-01 applies to the prompt edits: not live until `make install`. |
