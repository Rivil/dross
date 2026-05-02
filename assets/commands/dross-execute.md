---
name: dross-execute
description: "Execute a phase plan in pair-mode (default) or autonomous mode (--solo). Atomic commits per task."
argument-hint: "[phase-id] [--solo] [--from <task-id>]"
allowed-tools:
  - Read
  - Write
  - Edit
  - Bash
  - AskUserQuestion
---

@~/.claude/dross/prompts/execute.md
