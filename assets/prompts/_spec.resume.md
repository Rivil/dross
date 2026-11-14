Show a one-line summary of the existing spec (N criteria, M locked decisions, K deferred), not the TOML, and ask via `AskUserQuestion` whether to **extend** or **replace** it:
- **extend** — keep every existing entry; §2–§4 walk only new items, continuing the `c-N` numbering.
- **replace** — start the slate fresh at §2; §5 overwrites the file only after the new spec is agreed. Replacing clears `[[edges]]`: the new spec starts with none, so §2b proposes from the `--json` `classes` list, not `open` — the old spec's resolutions don't carry over.
