# MVP plan — context-boundaries

Phase context-boundaries — 5 tasks across 4 waves

Wave 1
  t-1  Add context threshold + transcript-tail nudge core
       files:    internal/ctxnudge/ctxnudge.go (new), internal/ctxnudge/ctxnudge_test.go (new),
                 internal/defaults/defaults.go, internal/defaults/defaults_test.go
       covers:   c-1, c-2, c-3, c-7
       desc:     defaults.toml gains `[context] threshold` (*int; nil → 150000, 0 → off) behind
                 `ContextThreshold()`. New leaf package ctxnudge: `ParseHook` (session_id,
                 transcript_path, agent_id from the hook payload), `MainContextTokens` (reads only
                 the last ≤1 MiB, newest `type:"assistant"` entry with isSidechain≠true, sums
                 input + cache_read + cache_creation), `Bucket(tokens, threshold)` (0 below; 1 at
                 threshold; +1 per fixed 50k), `Due(stateDir, session, bucket)` (one file per
                 session; nudge only when the bucket rises; rewrite only on change; a session id
                 that is not [A-Za-z0-9-] is never due), and `Line(tokens, threshold, next)`.
       contract: - defaults: no [context] → ContextThreshold()==150000; `threshold = 0` decodes
                   to 0 (off), not 150000 — if the field loses its pointer, the 0 case fails.
                 - token sum: an entry with input 10, cache_read 140000, cache_creation 5000,
                   output 9999 yields 145010 — counting output_tokens or dropping a cache field
                   fails it.
                 - subagent: a tail whose last assistant entry is isSidechain:true at 300k, after
                   a main entry at 100k, yields 100k — a sidechain filter regression fails it.
                 - tail-only (c-7 structural): over a counting io.ReaderAt on a 50 MB input,
                   bytes read ≤ 1 MiB; a whole-file scan fails it.
                 - cadence: threshold 150000 → Bucket(149999)=0, Bucket(150000)=1,
                   Bucket(199999)=1, Bucket(200000)=2, Bucket(x, 0)=0 for any x. Due: first
                   bucket-1 → true; bucket-1 again → false (no repeat per call); bucket-2 → true;
                   drop to 0 then bucket-1 again → true (post-compaction re-crossing); a different
                   session id starts fresh.
                 - Line: Line(152000, 150000, "/dross-execute — run the next task") contains
                   "152k/150k", "checkpoint at your next durable boundary → /clear", the next
                   command verbatim, and "mid-thought? /dross-pause first", on one line.

  t-2  Add the context-nudge rule to the interaction playbook
       files:    assets/prompts/_interaction.md, internal/cmd/interaction_snippet_test.go
       covers:   c-4, c-9
       desc:     One new `## Context nudge — checkpoint first` section: with a
                 `context <N>k/<T>k — checkpoint …` nudge in context, lead with checkpoint + /clear
                 at the next durable boundary — execute §1g's post-commit gate even mid-wave, and
                 every "state is on disk — safe to /clear" wrap-up; mid-command gates (spec/plan
                 before their artifact is written) are not durable boundaries and don't change; no
                 nudge → unchanged; --solo: informational, keep going; never run a command to
                 check usage. execute.md is NOT edited (gate_scope).
       contract: - TestInteractionSnippetHasContextNudgeRule fails if _interaction.md loses any of
                   "checkpoint + /clear", "durable boundary", "even mid-wave", "§1g",
                   "safe to /clear", "not durable boundaries", "--solo", "informational".
                 - the same test fails if execute.md's §1g (executeGateSection) no longer says
                   "mid-wave, lead with `continue`" — the below-threshold order must stay as today
                   (alongside the existing TestExecutePromptCheckpointWaveLead).
                 - TestFooterPromptsLoadPlaybook fails if any prompt footerCoverage marks
                   footer-bearing (debug, execute, pause, plan, quick, respond, ship, spec, verify)
                   stops running `dross interaction show` — the rule would no longer reach that
                   command's wrap-up.

Wave 2 (depends t-1)
  t-3  Add the `dross hooks nudge` PostToolUse verb
       files:    internal/cmd/nudge.go (new), internal/cmd/nudge_test.go (new),
                 internal/cmd/hooks.go, internal/cmd/telemetry.go
       covers:   c-1, c-2, c-3, c-5, c-7
       desc:     `NudgeHook = "dross hooks nudge"`, registered under Hooks(). RunE: recover →
                 read stdin (capped) → FindRoot (none → return) → ContextThreshold (0 → return) →
                 ParseHook (agent_id set → return) → MainContextTokens → Bucket → Due(
                 ~/.claude/dross/nudge, …) → only when due, load project+state and emit
                 ctxnudge.Line(…, suggestNext(…)) via render.MarshalJSON as `systemMessage` +
                 `hookSpecificOutput{hookEventName:"PostToolUse", additionalContext}`. Every path
                 exits 0; no stderr. RecordCLIEvent skips NudgeHook like the gate verbs.
       contract: - over-threshold first fire in a dross repo: stdout JSON has identical
                   systemMessage and additionalContext, hookEventName "PostToolUse", containing
                   the repo's suggestNext text — if either channel is dropped, c-3 fails.
                 - second fire at the same bucket emits empty stdout; a fire at +50k emits again.
                 - threshold = 0 with a 300k transcript: empty stdout and no state file created.
                 - payload with agent_id set at 300k: empty stdout and the session's state is
                   not advanced (the next main-agent fire still nudges).
                 - c-5: non-dross cwd, missing transcript_path file, and non-JSON stdin each give
                   err == nil, empty stdout AND empty stderr.
                 - c-7 wall-clock: steady-state fire (over threshold, bucket already recorded)
                   on a sparse 50 MB transcript completes RunE in < 50 ms.
                 - RecordCLIEvent for `hooks nudge` appends no telemetry event (while `status`
                   still appends one) — a missing skip fails it.

Wave 3 (depends t-3)
  t-4  Wire the nudge hook into ensure + doctor
       files:    internal/cmd/hooks.go, internal/cmd/hooks_test.go,
                 internal/cmd/doctor_hooks_test.go, internal/cmd/testdata/cli_surface/doctor_run.txt
       covers:   c-6
       desc:     Append `{EventPostToolUse, NudgeHook}` to userHooks (matcher-less, own group
                 beside `dross gate record`); `hooks ensure` Short names it and its success line
                 prints userHooksSummary() instead of the hard-coded five. Doctor needs no code:
                 its loop over userHooks warns (non-gate → ⚠) when it is missing. Re-mint the
                 doctor_run golden with DROSS_UPDATE_GOLDEN=1.
       contract: - TestHookWordingNamesAllFour (now six) fails if userHooks lacks
                   PostToolUse → dross hooks nudge, or if `hooks ensure`, init or onboard output
                   omits that pair.
                 - ensure twice on a temp CLAUDE_CONFIG_DIR: exactly one `dross hooks nudge`
                   entry under PostToolUse, still exactly one `dross gate record`, second run
                   byte-identical.
                 - TestDoctorMissingConvenienceHooksWarnOnly gains a "no nudge" row: settings with
                   the five other pairs → Hooks section shows "⚠ PostToolUse → `dross hooks nudge`"
                   and doctor exits 0 on hooks alone (warn, not issue).
                 - TestDoctorRunGolden fails if doctor's Hooks section lacks
                   "✓ PostToolUse → dross hooks nudge" after init.

Wave 4 (depends t-2, t-4)
  t-5  Record checkpoint→/clear→resume before/after cost
       files:    internal/cmd/execute_prompt_test.go,
                 .dross/phases/context-boundaries/measurements.md (new)
       covers:   c-8
       desc:     Add the zero-round-trip guard test. Then (after `make install`, r-01) run the
                 flow once with `[context] threshold` set low so the nudge fires: nudge →
                 §1g leads with checkpoint → validate + task next → re-entry line → /clear →
                 SessionStart line → /dross-execute --from <next>. Record agent turns and
                 wall-clock for after, and for before from a pre-phase checkpoint session
                 transcript, in measurements.md.
       contract: - TestCheckpointFlowAddsNoRoundTrips fails if §1g's checkpoint bullet names any
                   dross call other than `dross validate` and `dross task next`, or if any
                   assets/prompts/*.md contains "dross hooks nudge" (the agent must never call the
                   signal itself — signal_path).
                 - measurements.md carries a before row and an after row, each with turn count
                   and wall-clock; after-turns > before-turns is a c-8 failure at verify.

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 threshold setting, default 150k, 0 = off | t-1 (field + accessor), t-3 (0 short-circuits the hook) |
| c-2 main-agent context from transcript, subagents excluded | t-1 (token sum, isSidechain filter), t-3 (agent_id payloads skipped) |
| c-3 one line, both channels, no per-call repeat | t-1 (Bucket/Due cadence, Line content), t-3 (systemMessage + additionalContext) |
| c-4 §1g leads with checkpoint past threshold; order unchanged below | t-2 |
| c-5 silent exit 0 outside repo / bad transcript | t-3 |
| c-6 ensure wires idempotently; doctor flags missing | t-4 |
| c-7 ≤50 ms on 50 MB transcript, tail read | t-1 (byte bound), t-3 (wall-clock) |
| c-8 zero added round-trips; before/after recorded | t-5 |
| c-9 every wrap-up leads with checkpoint + /clear; test guards the rule | t-2 |

9/9 criteria covered.

## Judgment calls

- Hook event: PostToolUse (matcher-less), not UserPromptSubmit — pair gates answer through AskUserQuestion tool results, so UserPromptSubmit rarely fires inside /dross-execute and the nudge would miss §1g (c-4).
- Verb: `dross hooks nudge` under the existing `hooks` tree, not a top-level `dross nudge`/`dross context` — no new top-level surface, no cli_surface golden for the hooks tree, and no name that reads like the per-gate call signal_path forbids.
- New leaf package internal/ctxnudge rather than logic in internal/cmd — forced, not stylistic: boundary_test.go bans encoding/json in non-test internal/cmd files, and it keeps the c-7 byte-bound test out of the slow cmd package.
- Cadence state: one tiny file per session under ~/.claude/dross/nudge/, rejected a gatestate record in .dross/gate/ — gatestate.load spawns `git ls-files` (RefuseTracked) on every read, which would land on every past-threshold tool call against the 50 ms budget, and it would add a gatestate edit.
- Rejected deriving cadence from earlier nudges in the transcript — they fall outside the 1 MiB tail window.
- State records the last-seen bucket (written only on change), not a high-water mark, so a compaction that drops context below threshold and climbs back nudges again; a high-water mark would stay silent.
- No execute.md edit: gate_scope locks the rule into _interaction.md. The rule names §1g and "even mid-wave" itself, and the test pins §1g's below-threshold text as unchanged.
- Folded the "every wrap-up gets the rule" guarantee into a reach test (every footer-bearing prompt runs `dross interaction show`), not per-prompt needles — follows from gate_scope's one-shared-rule choice.
- Merged the defaults field into t-1 rather than a standalone task — on its own it is a one-file, under-10-minute change.
- Split the verb (t-3) from its wiring (t-4) despite the mvp lens — merged, they span 7 files; the split also stops a hook from being wired to a subcommand that doesn't exist yet.
- Doctor: no doctor.go change — hooksSection already iterates userHooks and grades non-gate hooks as warnings. "Flags it" = ⚠, not ✗, because the nudge is advisory.
- c-7 wall-clock covers the steady-state fire (every past-threshold tool call). The due path (project/state load + suggestNext, which can run git) fires once per 50k and is not timed.
- c-8 "before" comes from an existing pre-phase checkpoint transcript, not a rebuild of the base commit with its prompts reinstalled — the phase adds no calls to the flow, so counting the same flow from history is enough.
- Dropped as untraceable to any criterion: an options.md/`dross-options` entry for the new key, a `dross defaults set` verb (defaults.toml is hand-edited; `defaults show` already renders new fields), and a configurable 50k step (locked fixed).
