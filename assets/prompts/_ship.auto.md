## 0.5 Non-interactive fast-path (`--auto`)

`--auto` runs to completion non-interactively — suitable to call from a script or loop. Skip every interactive turn below:

- **§1 body-preview dump** — skipped; nothing is shown for approval.
- **§2 body override** — skipped; no AskUserQuestion, the generated body is used.
- **§3 reviewers** — skipped; no AskUserQuestion, zero reviewers are requested for this run (the configured `remote.reviewers` default is left untouched).
- **§3.5 landmark merge + ARCHITECTURE.md auto-backfill** — both skipped under `--auto` (they show a diff / drive LLM generation and ask for an OK, which a non-interactive run can't answer); fold landmarks in — and self-heal an absent doc — later via an interactive ship or `/dross-architecture`.

§0 pre-flight still runs — the verify-pass gate is **not** bypassed (add `--force-unverified` too only if you must ship unverified). Then shell out directly:

```
dross ship --auto <phase-id>
```

Add `--json` for machine-readable output to capture in the caller: `dross ship --auto --json <phase-id>` prints a single `{url, number, result}` object on stdout and suppresses the narration.

`--auto` opens the PR and **returns without merging** — do not drive the §5 CI-watch or §6 merge gate; the caller (or a later interactive `/dross-ship`) owns the merge. Report the PR URL and stop.

Everything below (§1–§7) is the interactive path, taken when `--auto` is absent.
