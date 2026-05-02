# /dross-rule

Manage the two-tier rules system conversationally. The CLI does the heavy lifting; this prompt only handles intent → flags + confirmation.

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

## Add — ask the right questions

When adding:

1. **Scope.** Default to project. Promote to global only if the rule clearly applies across all repos (e.g. "never amend pushed commits", "always prefer docker over direct package managers"). Confirm scope with the user before writing.
2. **Severity.** Default to `hard`. Use `soft` if the user says "warn me" / "flag" / "remind me" rather than "never".
3. **Wording.** Tighten the user's phrasing into an unambiguous rule. Show the proposed text and ask for confirmation before calling `dross rule add`.

Example:
```
User: "add a rule that says always run migrations through docker not directly"
Assistant proposes:
  Scope:    project
  Severity: hard
  Text:     "always run drizzle migrations via `docker compose exec app pnpm db:migrate`; never invoke drizzle-kit directly"
Confirm? (y / edit)
```

After confirmation:
```
dross rule add --scope <scope> --severity <hard|soft> "<text>"
```

## List

Default `--scope project`. If the user says "all" or "merged", use `dross rule list --merged` so they see exactly what gets injected into prompts.

## Remove / promote / toggle

Always ask the user to confirm the id before calling the destructive command.

## After any change

Run `dross rule show` and print the result so the user sees the new merged set.
