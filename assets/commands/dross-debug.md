---
name: dross-debug
description: "Debug systematically across sessions — one hypothesis per probe, evidence written to a gitignored .dross/debug/<slug>.md before the next, a hard stop after three failed fixes, fixes routed to /dross-quick or a task. Pair-mode by default; --solo runs probes autonomously."
argument-hint: "[--solo] [<slug>] [<symptom>]"
allowed-tools:
  - Read
  - Write
  - Edit
  - Bash
  - Grep
  - Glob
  - AskUserQuestion
---

@~/.claude/dross/prompts/debug.md
