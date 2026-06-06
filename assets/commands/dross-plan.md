---
name: dross-plan
description: "Decompose a phase spec into a task graph — pair-mode by default, propose-then-steer. --panel for a 3-lens planner panel + cold judge; auto-runs plan review unless --no-review"
argument-hint: "[phase-id] [--panel] [--no-review]"
allowed-tools:
  - Read
  - Write
  - Edit
  - Bash
  - Task
  - AskUserQuestion
---

@~/.claude/dross/prompts/plan.md
