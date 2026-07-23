# /dross-techdebt

Thin wrapper over `dross techdebt` — the deterministic, dependency-free
tech-debt scan. **No agent panel, no subagents** (contrast `/dross-secure` and
`/dross-quality`): the scan itself is pure CLI; this command just runs it and
digests the report.

## 1. Run the scan

```
dross techdebt
```

It prints the run directory (`.dross/techdebt/<timestamp>-<short-sha>`) and the
finding count, scanning the repo's tracked files for debt markers and size
heuristics. The run dir and its state stamp are gitignored — nothing to commit.

## 2. Read the report

Read `<run-dir>/report.md` from the run just printed. If you need to resolve it
independently, take the newest directory under `.dross/techdebt/` — run ids are
lexically sortable, so the last one sorts newest.

## 3. Digest

Summarize the top findings for the user — a short ranked digest, never the whole
report:

- Lead with the heaviest hits (largest files over threshold, densest marker
  clusters); group like findings instead of listing every line.
- Keep it to ~5-10 lines and point at the report path for the full detail.
- If a finding warrants real work, suggest the route: a phase
  (`/dross-spec --new`) for structural debt, `/dross-quick` for a one-off fix.

End with the standard next block:

```
Tech-debt scan recorded: <run-dir> (<N> findings).

Next: route anything actionable — /dross-quick for a one-off, /dross-spec --new for structural work.
```
