**Step 2 — the destination (only after "defer it").** Route the deferred item via `AskUserQuestion` (lead with the most likely). The entry gate already settled add-vs-defer, so this step **does not re-offer** the pull-in — only the parking destinations:
- **Park in the milestone backlog** — relevant, but not this phase. Coin a short slug from the idea; after the spec is written (§5) run:
  ```
  dross milestone add <version> phases "<slug>"
  dross deferred route <phase-id> <idx> --target "<slug>"
  ```
  It now re-surfaces 1:1 when `<slug>` is later scaffolded (§1's seed).
- **Attach to a named future phase** — it belongs to a phase already on the roadmap. Stamp that existing slug (after §5):
  ```
  dross deferred route <phase-id> <idx> --target "<existing-slug>"
  ```
- **Someday** — genuinely no home yet. Leave it unrouted (no `target`); it gets triaged later via `/dross-inbox`. **Leave an item unrouted only on an explicit someday pick** — never as the silent default.

`<idx>` is the item's 0-based position in this spec's `[[deferred]]` array. The `dross deferred route` / `dross milestone add` commands mutate on-disk artefacts, so **run them after §5 has written `spec.toml`** (a one-line confirm per item is enough — don't echo the array back). Re-run `dross validate` once after routing.
