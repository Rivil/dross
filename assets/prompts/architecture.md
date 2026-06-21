# /dross-architecture

Generate `ARCHITECTURE.md` at repo root from a scan of the code and git history.

This is the **backfill engine**. `/dross-architecture` runs it on demand (e.g. an
already-onboarded repo that has no `ARCHITECTURE.md` yet), and `/dross-onboard`
reuses it during onboarding — one engine, multiple entry points (the
`backfill_trigger` decision). It is read-the-code, write-the-prose work: fan out
**read-only** subagents freely to map features; the write of `ARCHITECTURE.md`
itself is gated behind explicit approval (the `dross-agent-gate` rule).

**Run the gated approval as a conversation, not a broadcast.** Follow the shared
interaction playbook (`_interaction.md`, printed by `dross interaction show` in the
pre-flight below): when you reach the §3 propose→approve→write gate, lead with a
proposed default and let the user react — one decision, not a wall.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as
   MUST-FOLLOW and follow the printed interaction playbook for the §3 approval gate.
2. Find the repo root — the directory holding `.dross/`.
3. **First-creation only.** If `ARCHITECTURE.md` already exists at repo root,
   stop: this engine is scoped to generating the doc when it's absent. Merging
   into an existing doc without clobbering hand edits is deferred — `/dross-ship`
   already merges each phase's landmarks incrementally. Tell the user to edit by
   hand, or proceed only if they explicitly ask to overwrite from scratch.

## 1. Map features (read-only fan-out OK)

The goal is a list of **user-facing capabilities** — what the system *does* — not
a mirror of the file tree. One entry per capability; a capability may span many
files. Never one entry per phase, never one per file.

- Enumerate the source tree (honour `.dross/project.toml` `paths.*` if set).
- For the key files behind each candidate capability, run
  `dross codex <files...>` — it returns symbols, cross-file refs, sibling files,
  and recent git activity in one shot. Use it to find the symbol-level anchors.
- Derive **provenance** from git + `.dross/phases/`: `git log --follow --oneline
  -- <file>` shows the history; map the introducing/extending work to phase ids
  and pick one representative commit short-sha.
- Group findings by capability. If the tree is large, fan out one read-only
  subagent per area — each returns {capability, one-line what, symbols+files,
  provenance}; none of them writes the doc.

## 2. Draft entries — one fixed template

Every entry follows the single micro-template below. It mirrors the Go source of
truth, `internal/architecture` `EntryTemplate` — **keep the two in sync** if you
change the shape here.

```
### <Feature name — a user-facing capability, not a module or a phase>

<One line: what this capability does.>

- Symbol.Name — path/to/file.ext:line
- Another.Symbol — path/to/other.ext:line

_introduced <phase-id> · extended <phase-id> · <short-sha>_
```

Rules:
- **One-line description only** — no prose paragraphs (`entry_template` decision).
- Each symbol-link bullet carries a real `file:line` you confirmed via codex/Read.
- Provenance is one compact inline breadcrumb (`provenance_format`): the phase(s)
  plus a commit short-sha. Drop `· extended …` when the capability has only been
  introduced.
- Order entries alphabetically by feature.
- Open the file with the same header + organizing contract the seeded skeleton
  uses (organized by feature, one entry per capability, never per phase).

## 3. Propose → approve → write (gated)

Show the user the full drafted `ARCHITECTURE.md` — the feature list and every
entry — in chat. Ask: **proceed / steer**. Only on explicit approval, write it to
`ARCHITECTURE.md` at repo root.

(When this engine runs embedded in an automated/`--solo` flow, write directly and
say so in the surrounding command's output — the gate is the caller's to relax.)

## 4. Wrap

- Run `dross validate`.
- **Standalone** (`/dross-architecture`): suggest the user commit
  `ARCHITECTURE.md` (it lives at repo root, outside `.dross/`, so it rides normal
  commits and ships in PRs). End with a bottom-anchored `Next:` line:
  ```
  ARCHITECTURE.md generated. Next: commit it, then /dross-status.
  ```
- **Embedded in onboard**: return control to onboard's wrap step — don't print a
  separate completion block.
