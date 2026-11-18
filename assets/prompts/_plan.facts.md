`dross phase facts` exited non-zero: each entry in `unresolved` is a citation the tree does not back — its `field` (where it is cited: `criteria c-2`, `decisions <key> choice`, `task t-3 description`), its `kind` (`missing` path, `moved` symbol with `new_line`, `gone` symbol, `unresolved` symbol) and the `citation` itself. Never build a task on one of them. If `degraded` is true, say once that symbol checks were skipped for the file it names.

Walk them **one citation per turn**, in the order listed. For each, one `AskUserQuestion` naming the field, kind and citation, with two outcomes:

- **fix the citation** — the text is wrong: a stale path, a renamed symbol, a `moved` line (offer `new_line`). Lead with this unless the phase plainly creates the thing. A spec-side finding (`criteria …`, `decisions …`) is fixed in `spec.toml` with `Edit`, changing the citation only — a locked decision's choice keeps its meaning. A plan-side finding (`task t-N …`) is fixed with `dross task edit <id> t-N --description "<text>"` (or `--test-contract` / `--files`); never hand-edit `plan.toml`.
- **confirm it is new** — this phase introduces it. Record it:

  ```
  dross phase mark-new <id> <citation>
  ```

  It refuses anything `dross phase facts` does not currently report, so pass the citation as listed.

After each, re-run `dross phase facts <id> --json` and keep walking until it exits 0. Before §2, that means the spec is clean before any task is built on it; at §5, the re-run must exit 0 before the lock question, so §6's plan-lock never meets an unhandled refusal.
