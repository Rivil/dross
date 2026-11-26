**One-way confirm turn.** Right after an area resolves to a locked `one-way` decision, give that decision its own turn before the next area: one `AskUserQuestion`, never batched with another area or decision — **"`<key>` is one-way: <choice>. Confirm it?"**, with exactly these options:
- **confirm** (lead) — the user accepts the door as decided. Note the key; nothing is written yet.
- **make reversible** — retag it `reversibility = "reversible"`; a reversible decision needs no confirm.
- **reopen** — the choice is not settled: return to §3b on this area, and ask this turn again once it resolves.

After §5 writes `spec.toml` — never before — run the verb once per yes, for each key answered **confirm** and for no other:
```
dross phase confirm <phase-id> <key>
```
It records the confirmation against the choice as written; plan-lock refuses a one-way decision without it. If §5's **edit** changes a confirmed decision's choice, ask its confirm turn again and re-run the verb on a yes.
