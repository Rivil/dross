---
name: dross-review
description: "Spawn a four-lens subagent panel (security / quality / tests / spec-fidelity) over an open PR and post the aggregated findings as a single comment"
argument-hint: "<pr-number> [--phase <id>]"
allowed-tools:
  - Read
  - Bash
  - Grep
  - Glob
  - Task
  - AskUserQuestion
---

@~/.claude/dross/prompts/review.md
