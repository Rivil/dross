# /dross-watch

A read-only heartbeat: surface what changed on the issue board and in the phase spine since the last tick, then point at the single most useful next command. This is a **broadcast, not a conversation** — it prints a digest and stops. It never triages, commits, edits the board, or transitions a phase. Run it on a `/loop` interval (e.g. `/loop 15m /dross-watch`) to stay oriented during a work session.

## 1. Pull the digest

Run the read-only digest command:

```
dross watch --json
```

It emits one JSON object: `{ new, current, drift, suggested_command, board_ok, stranded }`.

- `new` — board issues that appeared (or reopened) since the last tick.
- `current` — board issues already seen, carried over.
- `drift` — phases still in flight, each with a `kind`: `in_progress`, `complete_unverified`, or `verified_unshipped`.
- `suggested_command` — the single next command, already ranked for you (see §3).
- `board_ok` — `false` when board sync is off, misconfigured, or unreachable; the digest is then **drift-only**.
- `stranded` — how many board mirrors have an artefact that finished and a card that did not. **Absent when it is zero**, and absent when the board was not reached — a zero there would read as "clean" when the honest answer is "unknown".

`dross watch` writes nothing but `.dross/watch.state.json` (its delta baseline). It never mutates the board, git, or phase state — so it is always safe to run on a timer.

## 2. Render the digest

Print a compact block — a few lines, no wall of text:

```
watch · <N> new · <M> carried · <K> phase(s) drifting
  stranded: <N> board mirror(s)  (only when the field is present)
  new:   #<id> <title>          (one line per new issue)
  drift: <phase> — <kind>       (one line per drifting phase)
  next:  <suggested_command>
```

**Stranded mirrors are a drift signal, not a task.** When `stranded` is present, print the count as one line and say in prose that the mirror sweep closes them — do not emit a command for it. Sweeping the board is a deliberate, human-run act, so no prompt puts the verb in the model's hands; the count exists to make the debt visible, and `dross doctor` names the remedy when someone wants it.

**Board off / unreachable.** When `board_ok` is `false`, say so on one line and render the **drift-only** digest — do not error or stop. Mirror how `/dross-inbox` handles a missing board: announce that the board source is being skipped (`board sync off / unreachable — drift only`), then continue with the phase-drift half.

## 3. Suggest exactly one next command

End with **exactly one** suggested command — the digest's `suggested_command` field, printed **verbatim**. Do not recompute or override it; the ranking is decided in the command per the locked precedence:

1. **/dross-verify** — a phase is complete-but-unverified (finish verifying in-flight work first).
2. **/dross-ship** — a phase is verified-but-unshipped (ship it).
3. **/dross-inbox** — new board issues are waiting (triage new intake).
4. **/dross-status** — nothing pressing; open the full picture.

Advance in-flight phases before pulling in new intake. Print the field exactly as given — the human render and the machine `suggested_command` must agree.

## Hard rules

- **Read-only broadcast.** `dross watch` and this prompt mutate nothing but `.dross/watch.state.json`. Never triage, commit, edit the board, or transition a phase from here — those belong to `/dross-inbox`, `/dross-execute`, `/dross-ship`.
- **One suggested command, verbatim.** Print the digest's `suggested_command` exactly; never invent a second suggestion or reorder the precedence.
- **Never error out.** A board that is off or unreachable degrades to a drift-only digest, not a failure — this runs on a loop.
- **Never emit the mirror sweep.** Report the `stranded` count; never print a runnable sweep command. The sweep writes to the board, and this prompt is a read-only broadcast.
