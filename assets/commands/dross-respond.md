---
name: dross-respond
description: "Triage the review comments on a phase's PR — each claim checked against the code, then accepted as a task, rejected with a reason or routed as deferred — and send the rejections back in one human-approved reply. Pair-only."
argument-hint: "<pr>"
allowed-tools:
  - Read
  - Bash
  - Grep
  - Glob
  - AskUserQuestion
---

@~/.claude/dross/prompts/respond.md
