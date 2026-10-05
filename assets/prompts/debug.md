# /dross-debug

Debug a problem systematically, across as many sessions as it takes. The investigation lives in a **session file**, `.dross/debug/<slug>.md`: what fails, the hypotheses, the probe that tests each one, and what it showed. You write the evidence down before you move on, so a `/clear`, a compaction or a new day loses nothing: the SessionStart line names the open session and the command that resumes it.

`$ARGUMENTS` is `[--solo] [<slug>] [<symptom>]`:
- `<slug>` names an existing session → resume it.
- `<slug> <symptom>` with no session under that name → start one.
- nothing → list the sessions and propose resuming the newest open one, or ask for the symptom and propose a slug.

**Pair mode is the default**: you propose each probe and the user approves it. `--solo` runs probes without asking — but the hard stops below apply in solo exactly as in pair.

**Run this as a conversation, not a broadcast.** Follow the shared interaction playbook (`_interaction.md`, printed by `dross interaction show` in the pre-flight): one decision per turn, lead with your proposal, let the user react.

## The session file

`dross debug new` scaffolds it from a fixed template; the CLI reads only its header status line, the seven fixed headings and the list items under them:

- `## Symptom` — what fails, exactly: the command, what it printed, what it should have printed.
- `## Hypotheses` — one `- ` item per candidate cause. Each probe's evidence and verdict go as an indented sub-bullet under the hypothesis it tested (sub-bullets are not counted as hypotheses).
- `## Current theory` — the one hypothesis the next probe tests, and why it leads.
- `## Next probe` — the single probe that confirms or kills the current theory, with what each outcome would mean.
- `## Fix attempts` — one item per fix tried. A fix that did not make the symptom go away is `- [failed] <what was tried>`. After re-planning, `- [replan] <the new model of the problem>`.
- `## Resolution` — the signals that the cause is fixed, one `- ` item each.
- `## Prevention` — the rule that would have caught this, as one statement.

States `dross debug list` reports: `open`, `needs-replan`, `resolved`, `abandoned`. Markers it counts: `- [failed]`, `- [replan]`. Three `- [failed]` items since the last `- [replan]` read `needs-replan`.

**Edit, never Write.** Every update to a session goes through `Edit`, a section at a time. A `Write` over an existing session is forbidden: it is how evidence gets lost, and the curated-file gate refuses one that shrinks the file below half its size. The file is created only by `dross debug new`.

**Quote output inside fenced blocks.** Pasted logs, diffs and test output go in ``` fences, so a quoted `- [failed]` or `## Resolution` line can never be counted.

**Session content stays on this machine.** Sessions are gitignored because they quote captured output. Never paste session content into a commit message, a PR body or a board issue. The one sanctioned exit is the `dross rule add` line that `dross debug close --fixed` prints from `## Prevention` — which is why Prevention must be a rule statement carrying no captured output, paths, hostnames or tokens.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for every turn.
2. Parse `--solo` from `$ARGUMENTS`.
3. Run `dross debug list`.
   - **Any session reads `needs-replan` or lists a `problem:` line** → stop at §3 (BLOCKED) before anything else for that session.
   - **Resume** (the slug names an existing `open` session): read the whole session file. The `## Current theory` and `## Next probe` sections are where you left off.
   - **Start** (no session under that slug): run `dross debug new <slug>`, then `Edit` the symptom into `## Symptom` and your first hypotheses into `## Hypotheses`. In pair mode, confirm the symptom wording with the user first.
   - A `resolved` or `abandoned` session is closed: read it for the previous root cause, but start a new session under a new slug rather than reopening it.
4. Take the **pre-session snapshot** of the work tree — both lines, kept for the whole session:
   ```
   git status --porcelain
   git diff HEAD | shasum -a 256
   ```
   The tree must match this snapshot before and after every probe. Porcelain alone is not enough: a file that was already modified stays ` M` whatever a probe leaves in it, so the content hash is what catches an unreverted probe.

## 1. Plan the probe

Pick **one hypothesis** — the one the evidence points at most — and write it to `## Current theory`. Write **one probe** to `## Next probe`: the command or experiment, and what you expect if the theory is right and if it is wrong. One hypothesis per probe: a probe that tests two things at once tells you nothing about either.

- **Pair:** propose the probe with `AskUserQuestion` — `run it` (recommended), `steer` (the user redirects the theory or the probe), `stop` (end here; the session stays open).
- **Solo:** run it.

## 2. Probe loop

For each probe:

1. **Check the tree** against the pre-session snapshot (both lines). If it differs before you start, stop: something outside this probe changed the tree, and you must not probe on top of it.
2. **Run the probe.** Run tests through `dross test` (a package selector narrows it: `dross test ./internal/x/...`), never by pasting the raw `runtime.test_command`. A probe may add temporary instrumentation — a log line, a changed constant with `Edit`, or a new focused test file — but only to learn something, never as the fix. Never touch a file that was untracked before the session: the content hash cannot see inside untracked files, so a change there would go unnoticed.
3. **Write the evidence and the verdict to the session before the next probe starts.** `Edit` an indented sub-bullet under the hypothesis in `## Hypotheses`: the probe, the evidence (output in a fence), and the verdict — confirmed, refuted or inconclusive. Update `## Current theory`. Nothing about this probe may live only in the conversation: the next probe does not begin until the session holds this one's result.
4. **Return the tree to the pre-session snapshot.** Undo every temporary edit with `Edit` and delete every temporary file the probe created, then re-run both snapshot lines and compare. If either line differs, the probe left something behind: find it and undo it before anything else. Never use `git stash`, `git checkout -- .`, `git reset --hard` or `git clean` to get there — they also destroy work that was in the tree before the session.
5. **Next:** a refuted theory → back to §1 with the next hypothesis. A confirmed root cause → §4.

### Fix attempts

When you try a fix — inside the session as an experiment, or after one landed through /dross-quick or a task — and the symptom does not go away, `Edit` a `- [failed] <what was tried>` item into `## Fix attempts`, revert the experiment as in step 4, and then **run `dross debug list` after every Fix-attempts entry**. If the session reads `needs-replan`, or `dross debug list` prints any `problem:` line for it, go to §3.

## 3. Hard stop — BLOCKED

Three failed fixes since the last re-plan means the model of the problem is wrong; another lap compounds it. A `problem:` line means the session file is malformed and its counts cannot be trusted. Either way, stop — in pair **and** in solo — and put this on the first line of your reply:

```
BLOCKED: debug session <slug> needs a re-plan — three fixes failed since the last re-plan entry
```

(or `BLOCKED: debug session <slug> is malformed — <the problem line>`). Then summarise what the failed fixes had in common and propose a new model of the problem. Do not probe or try another fix until the user has agreed a re-plan. Record the agreed model as `- [replan] <the new model>` under `## Fix attempts`; for a malformed session, fix the named problem with `Edit`. Re-run `dross debug list` and continue only once it reads `open` with no problem lines.

## 4. Fix — route it, never commit it

/dross-debug never commits. A confirmed root cause gets its fix through the paths that already carry the atomic-commit, test-gate and version guarantees:

- **Standalone** (no phase execution in progress) → `/dross-quick` with the fix and its regression test. It needs a clean tree: if the tree already held changes when the session began, the user settles them first — say so rather than routing around it.
- **Mid-phase** (a `/dross-execute` run is in progress on this phase) → add the fix as a task with `dross task add <phase-id>` (giving it a `--test-contract`), and let execute land it. The task is written to the phase's `plan.toml`, which /dross-execute commits with the phase's bookkeeping.

While a pair-mode `/dross-execute` task is in progress, the pair-approval gate refuses edits outside `.dross/` until that task is approved. A refusal is a hard stop: never route around it.

In pair mode, propose the route with `AskUserQuestion` before taking it. When the fix has landed, record its commit in the session and **retake the pre-session snapshot** (§0 step 4): HEAD has moved, so the old one no longer describes the tree. If the symptom is gone, move to §5. If it is not, record a `- [failed]` item as in §2 and go back to §1 against the new snapshot.

## 5. Close

A **fixed** close needs at least two **independent** signals under `## Resolution` — signals of different kinds, not the same check repeated:

- the original repro no longer reproduces;
- a regression test that was red before the fix and is green after it;
- the suite (`dross test`) or CI is green;
- and, always, the commit the fix landed in, cited among them.

Write `## Prevention` as one rule statement — what to always or never do — with no captured output, paths, hostnames or tokens. Then:

```
dross debug close <slug> --fixed
```

It refuses, naming each gap, until Resolution lists two signals and Prevention is non-empty. On success it prints a `dross rule add` line carrying the Prevention text. Adding a rule is the user's call:

- **Pair:** offer it with `AskUserQuestion` — `add the rule` runs exactly the printed line, `skip` leaves it.
- **Solo:** do not run it; list it in the wrap-up for the user.

A session that will not be fixed — the cause is outside this project, or the symptom is gone for reasons nobody will chase — closes as abandoned, which needs no signals:

```
dross debug close <slug> --abandoned --reason "<why, one line>"
```

Either close keeps the file on disk and drops it from the status and reentry nudges.

## 6. Wrap-up

```
Debug session <slug>: <open | needs-replan | resolved | abandoned>
  Theory:  <current theory, one line>
  Probes:  <n> this session · fixes failed since re-plan: <m>
  Fix:     <routed to /dross-quick | task t-N | landed in <sha> | none yet>
  Rule:    <the printed `dross rule add` line, solo only — not run>

state is on disk — safe to /clear · fresh session: /dross-debug <slug>
```

## Hard rules

- **One hypothesis per probe.** Never test two at once.
- **Evidence before the next probe.** Each probe's evidence and verdict are in the session before the next probe starts.
- **The tree goes back after every probe.** Compare both snapshot lines before and after; never use `git stash`, `git checkout -- .`, `git reset --hard` or `git clean`.
- **BLOCKED means stop.** `needs-replan` or any `problem:` line halts the run in pair and solo alike, with BLOCKED on the first line.
- **Never commit.** Fixes land through /dross-quick (standalone) or `dross task add` (mid-phase).
- **Edit, never Write, an existing session.**
- **Session content never leaves this machine** except as the printed rule line, and Prevention never carries captured output, paths, hostnames or tokens.
- **Tests run through `dross test`.**
