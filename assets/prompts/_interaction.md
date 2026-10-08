<!--
  _interaction.md — the shared interaction playbook for dross slash commands.

  This file is the detailed counterpart to the `dross-interaction-contract`
  builtin rule (see `dross rule show`). The rule states the terse invariant; this
  snippet is the how-to. Interactive prompts pull it in via `dross interaction
  show` so the pattern lives in one place and every command behaves the same way.

  Underscore prefix keeps it sorting/reading as a partial, not a command prompt.

  Coverage convention: every command-backed prompt must be classified — interactive
  commands get a `### dross-<name>` section in docs/interaction-audit.md;
  non-interactive ones are enrolled in that doc's `## Exempt` list with a reason.
  interaction_coverage_test.go enforces this fail-closed; `dross doctor` surfaces
  it. See docs/interaction-audit.md for the full convention.
-->

# Interaction playbook

Run interactive commands as a **conversation, not a broadcast**. The user is part
of the loop — each step is a short turn they answer, not a form they fill in.

## The contract

- **One decision per turn.** Surface a single decision, get an answer, then move
  to the next. Never batch unrelated questions into one turn, and never dump the
  whole agenda (orientation + every criterion + every option) into one block the
  user has to expand to read.
- **Propose and react.** Don't free-ask into a void. Propose a sensible default —
  the one you'd pick — and let the user *react* to it (accept / steer). A concrete
  proposal the user can reject beats an open question they have to answer cold.
- **No walls of text.** Keep each turn to a few lines. If you're writing
  paragraphs, you're broadcasting. Bullet points and short sentences only.
- **Confirm artifacts; don't broadcast them.** The TOML / config / plan you compose
  is a build artifact, not a review medium — never paste the build artifact back.
  Every line was agreed in prose first; confirm the written file with a one-line
  summary, not by dumping its contents. Surface a specific field only if the user
  asks to see or change it.

## How to drive a turn

Most turns are an `AskUserQuestion` with a proposed default first and 2–4 concrete
reactions. The canonical gate for "is this item right?" is **accept / reword /
drop**:

- **accept** — take the proposal as written, move on.
- **reword** — keep the idea, adjust the wording or scope; the user says how.
- **drop** — the item doesn't belong; remove it.

When the choice is open-ended (not a small fixed set), go freeform instead of
manufacturing options — but still lead with your recommendation.

## Surfacing a candidate — include-first

When you surface a candidate that *might* belong to the current phase — a
gray-area-adjacent proposal, a parked idea, an optional sub-feature — don't ask
open-endedly and don't silently record it. Decide which of three cases it is:

- **Belongs here** — it serves the phase's goal. Include it: it joins the phase
  through the plain accept / reword / drop gate above, with accept as the lead.
  No defer question.
- **Genuinely borderline** — present a single either/or that
  **leads with "add to current phase"** and offers "defer it" (routed to a
  destination, or someday).
- **Clearly has a home elsewhere** — a named phase already on the roadmap, or a
  genuinely separate capability. Lead with "defer it", put the reason and the
  destination in the option, and still offer "add to current phase".

Include-first: deferral is not the safe default. In-scope work parked in the
backlog is scope that leaked, not scope kept tight — a phase finishes its job
rather than handing pieces of it on. Either way the candidate never evaporates:
it is included or parked, never lost.

## Context checkpoint

A line starting `context checkpoint:` in context means the session is past its
context threshold. At the next durable boundary, lead with "checkpoint + /clear":
execute §1g even mid-wave (this overrides §1g's mid-wave `continue` lead) and
every "safe to /clear" wrap-up. A gate before its artifact is written is not a
durable boundary — leave it as it is. Under `--solo` the nudge is
informational: keep going. Thread not on disk yet → `/dross-pause` first.
Never run a command to check context.

## Anti-patterns

- A single `AskUserQuestion` that bundles several unrelated decisions "to save
  round-trips". Round-trips are the point.
- Pasting the composed artifact back "so the user can confirm" — that's the
  ctrl+o wall this contract exists to kill.
- A "you decide / skip" option on a clarifying question. The user ran the command
  to decide; give them real choices.
- Echoing the entire growing list back every turn. A short "added c-3" is enough.

## When the contract bends

If the user explicitly asks for everything at once ("just give me all the
questions"), comply — but say once that batching trades away the steer-as-you-go
benefit. The default is always one decision per turn.
