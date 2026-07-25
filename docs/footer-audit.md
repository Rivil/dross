# Footer audit

Tracks the **clear-point footer convention** (c-1 of the context-hygiene phase):
every dross command that ends at a durable state boundary closes its printed
wrap-up with the sentinel line

```
state is on disk — safe to /clear · fresh session: /dross-<next>
```

so the user always knows when clearing context is free — the handoff between
commands goes through the `.dross/` files, not through the chat scrollback.

**Coverage convention.** Every command-backed prompt is classified exactly one
of two ways: **footer-bearing** → its `assets/prompts/<name>.md` carries the
sentinel with the exact `/dross-…` next command on the same line; or **exempt**
→ enrolled in the [`## Exempt`](#exempt) table below with a reason. There is no
third state — an unclassified prompt fails the build. The Go gate in
`internal/cmd/footer_coverage_test.go` enforces this fail-closed: deleting a
footer, adding an unclassified prompt, dropping an Exempt row, or leaving an
Exempt reason blank all fail the test naming the prompt. This doc is the
footer-side mirror of `docs/interaction-audit.md` — same shape, separate gate,
so the two conventions stay single-purpose.

## Footer-bearing

The seven durable-boundary commands and the fresh-session command each footer
names:

| Command | Fresh-session next |
|---|---|
| spec | /dross-plan |
| plan | /dross-execute |
| execute | /dross-verify (phase completion; the §1g checkpoint gate emits /dross-execute --from) |
| verify | /dross-ship (pass) · /dross-execute (partial/fail) |
| ship | /dross-status |
| quick | /dross-status |
| pause | /dross-resume |

## Exempt

Command-backed prompts that are intentionally **footer-less** — they end at no
durable mid-work boundary the user would clear away from. Machine-read by
`footer_coverage_test.go`: removing a row (or leaving its reason blank) fails
the build unless the command gains a footer.

| Command | Reason |
|---|---|
| architecture | doc generator — ends by pointing at the written ARCHITECTURE.md, no in-flight phase state |
| inbox | triage conversation — routes board items and ends; each route is already committed |
| init | one-time scaffold — ends by routing to /dross-milestone with nothing in flight |
| milestone | roadmap scoping — short session ending in a routed /dross-spec handoff |
| onboard | one-time adoption scan — same posture as init |
| options | settings review — edits land immediately, nothing to resume |
| plan-review | subagent-only relay — no user-facing session state of its own |
| quality | audit — heavy work runs in cold subagents; report + scaffold land on disk as they complete |
| resume | session *start* command — it replays the handoff; clearing right after defeats it |
| review | PR review panel — posts findings to the PR, holds no .dross state |
| rule | quick config edit — applied on the spot |
| secure | audit — same subagent posture as quality |
| status | read-only — its own last line *is* the re-entry line (c-4) |
| techdebt | deterministic scan digest — run artefacts are gitignored, ends by routing findings; no in-flight phase state |
| watch | read-only digest — ends with one suggested command, no state written |
