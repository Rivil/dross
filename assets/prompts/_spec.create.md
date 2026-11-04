**Create flow** (used by `--new`, the no-target `new` answer, or whenever scaffolding is needed):
- If `state.current_milestone` is set, run `dross phase list --milestone <milestone>`: entries marked `(not scaffolded)` are the **unscaffolded roadmap phases**. Read the slug out of the line: each line has a two-character status prefix (`✓ ` when done) and the last line is an `N/M done` footer — neither is a slug.
- If there are unscaffolded entries, present them via `AskUserQuestion`: one option per entry (label `<id>`; description = the entry's one-line summary if a `Brief.md` at repo root contains a matching `### Phase <id>` section, otherwise the bare title). Last option is **"Describe a new phase"** (freeform).
- If the user picks a roadmap entry, the title is the entry's slug (or its Brief.md title if present) — run `dross phase create "<title>"`.
- If the user picks **Describe a new phase**, prompt for a freeform title, then run `dross phase create "<title>"`.
- If milestone is unset, roadmap is empty, or all roadmap entries are already scaffolded, fall back to the freeform title prompt directly.

In every case: capture the new id (`NN-slug`) and proceed. Do NOT tell the user to run `dross phase create` manually — this command runs it. `dross phase create` also checks out the `phase/<id>` branch.
