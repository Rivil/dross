Add a **Solo review** block to the report, after `Findings:` — what the per-task reviewer flagged in a `--solo` run, one line per finding with its resolution:

```
  Solo review:
    t-3  pass after 2 rounds
      - BLOCKING spec c-1: <finding> — fixed in the fix round
      - FLAG quality: <finding> — non-blocking, left
    t-5  exhausted after 2 rounds — UNRESOLVED
      - BLOCKING spec c-3: <finding> — unresolved — task failed
    failed: t-5 — <reason from `dross task show`>
```

Mark every unresolved finding and every failed task: a task the reviewer failed was never committed, so no criterion's coverage may lean on it.
