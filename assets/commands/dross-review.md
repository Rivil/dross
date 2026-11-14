---
name: dross-review
description: "Spawn a six-lens subagent panel (security / quality / tests / spec-fidelity / edge-case / silent-failure) over an open PR and post the aggregated findings as a single comment"
argument-hint: "<pr-number> [--phase <id>] | --diff <patch> --out <dir> [--phase <id>]"
allowed-tools:
  - Read
  - Bash
  - Grep
  - Glob
  - Task
  - AskUserQuestion
---

@~/.claude/dross/prompts/review.md
