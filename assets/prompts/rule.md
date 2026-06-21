# /dross-rule

Manage the two-tier rules system conversationally. The CLI does the heavy lifting; this prompt only handles intent → flags + confirmation.

**Run this as a conversation, not a broadcast.** Follow the shared interaction playbook (`_interaction.md`, printed by the `dross interaction show` pre-flight step below): take scope, severity, and wording as separate proposal turns, confirming the composed rule with a one-line summary.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for every turn of this command.

## Parse intent

Read the user's freeform input. Recognise these intents:

| Phrase pattern | Action |
|---|---|
| "add a rule that says X" / "remember not to Y" | `dross rule add` |
| "list rules" / "show rules" | `dross rule list` |
| "remove rule X" / "delete X" | `dross rule remove <id>` |
| "promote X" / "make X global" | `dross rule promote <id>` |
| "disable X" / "turn off X" | `dross rule disable <id>` |
| "show what claude sees" | `dross rule show` |

## Add — one decision per turn

When adding, walk these as **separate proposal turns** — never bundle scope, severity, and wording into one question:

1. **Scope.** Propose `project` (the default); promote to `global` only if the rule clearly applies across all repos (e.g. "never amend pushed commits", "always prefer docker over direct package managers"). Confirm via `AskUserQuestion` (project / global) before moving on.
2. **Severity.** Propose `hard` (the default); use `soft` if the user said "warn me" / "flag" / "remind me" rather than "never". Its own `AskUserQuestion` turn (hard / soft).
3. **Wording.** Tighten the user's phrasing into an unambiguous rule. Propose the text and confirm via `AskUserQuestion` (accept / reword) before writing.

Each line below was its own turn:
```
  Scope:    project
  Severity: hard
  Text:     "always run drizzle migrations via `docker compose exec app pnpm db:migrate`; never invoke drizzle-kit directly"
```

After the three turns:
```
dross rule add --scope <scope> --severity <hard|soft> "<text>"
```

## List

Default `--scope project`. If the user says "all" or "merged", use `dross rule list --merged` so they see exactly what gets injected into prompts.

## Remove / promote / toggle

Always ask the user to confirm the id before calling the destructive command.

## After any change

Confirm with a **one-line summary** — e.g. `Rule added: [project/hard] "<short text>"` — never paste the full merged rules block back; point the user at `dross rule show` if they want to see it. End with a bottom-anchored `Next:` line:
```
Rule <added | removed | promoted | toggled>. Next: /dross-status — back to where you were.
```
