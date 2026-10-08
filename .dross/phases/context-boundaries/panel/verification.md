# context-boundaries — verification-lens draft

Lens: I wrote each criterion's ideal test contract first, then derived the smallest task that makes it satisfiable. Three things the repo forces shape the result:

- **No JSON in `internal/cmd`.** `boundary_test.go` bans `encoding/json` there (`forbiddenInCmd`). So the transcript reader, payload decode and hook envelope go in a new pure package, `internal/ctxnudge`, which is fast to test. `internal/cmd` only wires them.
- **Real transcript shape.** Real transcripts mark subagent turns `isSidechain: true`. That holds both for legacy inline sidechains and for the current `<session>/subagents/agent-*.jsonl` files (checked structurally on this machine). The reader's fixtures are therefore jq projections of real lines, not invented shapes.
- **Prompt tests prove wording, not behaviour.** A prompt test shows the rule is in `_interaction.md`; it cannot show the model obeys it. So c-3 and c-4 also get one live end-to-end observation (t-11), recorded next to the c-8 timings.

Fixed names the tasks share (the coupling tests depend on them):
- hook verb `dross hooks nudge` (const `nudgeHookCommand`)
- nudge line prefix `context checkpoint:` (const `ctxnudge.Prefix`)
- fixed band step `ctxnudge.Step = 50_000`
- defaults key `[context] threshold` (tokens; unset = 150000; 0 = off)
- claim markers `.dross/gate/nudge-<session>-<band>`

```
Phase context-boundaries — 11 tasks across 4 waves

Wave 1
  t-1  Record the checkpoint-flow baseline before changes
       files:    .dross/phases/context-boundaries/measurements.toml (new)
       desc:     Uses the dross installed BEFORE this phase (record `dross --version`). In a scratch dross repo with one
                 wave of two tasks, a human runs: §1g after t-1 (mid-wave) → checkpoint → /clear → /dross-execute --from
                 t-2 → the §1c approval turn. The agent then counts assistant API turns, and agent wall-clock with human
                 wait excluded, using jq over the two session transcripts. Writes [scenario] + [before].
       covers:   c-8
       contract: - if [before].binary equals the post-phase version, the baseline is contaminated and verify rejects
                   it as c-8 evidence. A threshold=0 run on the new binary is not "today": the hook still spawns
                 - if [scenario] is missing the repo shape, the wave position or the flow endpoints, t-11 cannot replay
                   it identically and the c-8 comparison at verify is invalid

  t-2  Read latest main-turn usage from transcript tail
       files:    internal/ctxnudge/transcript.go (new), internal/ctxnudge/transcript_test.go (new),
                 internal/ctxnudge/testdata/main_tail.jsonl (new), internal/ctxnudge/testdata/subagent_tail.jsonl (new)
       desc:     LatestMainUsage(io.ReaderAt, size) reads backward from EOF in 64 KiB chunks, capped at 4 MiB. It returns
                 input + cache_read_input_tokens + cache_creation_input_tokens of the newest type=assistant line with
                 isSidechain false and non-zero usage. Errors are ErrNoMainTurn / a read error, never a panic. Fixtures
                 are a jq projection of real lines ({type,isSidechain,message:{model,usage}}). No content fields, so no
                 secret can ride in.
       covers:   c-2, c-5, c-7
       contract: - if output_tokens is added to the sum, or any of the three input fields is dropped,
                   TestUsageSumsThreeInputFields fails against main_tail.jsonl's known total
                 - if a newer isSidechain:true assistant line (larger usage) is taken over the older main line,
                   TestSidechainTurnsIgnored fails. subagent_tail.jsonl (all sidechain) must return ErrNoMainTurn
                 - if a trailing "<synthetic>" zero-usage entry masks the prior real turn, TestSyntheticZeroSkipped fails
                 - if the reader decodes from the head, or holds more than the window it needs, TestTailOnlyByteBudget
                   fails. The file is a 50 MB sparse transcript with the last assistant line in its final 64 KiB, and a
                   counting ReaderAt sees > 128 KiB read
                 - if the reader gives up (or uses a 64 KiB bufio.Scanner token limit) on a 3 MB trailing tool_result
                   line, TestLongTrailingLine fails. It must still find the assistant line before it
                 - if no main turn lies within the 4 MiB cap and the reader reads past the cap (or returns ok),
                   TestCapStopsRead fails
                 - if a file truncated mid-line, an empty file or a garbage line errors fatally instead of being skipped
                   or returning ErrNoMainTurn, TestTornAndGarbageTails fails
                 - if LatestMainUsage on the 50 MB sparse file takes ≥ 50 ms (min of 5 runs), TestTailRead50MBUnder50ms
                   fails

  t-3  Nudge payload, cadence, line and envelope
       files:    internal/ctxnudge/nudge.go (new), internal/ctxnudge/nudge_test.go (new)
       desc:     Decode(payload) reads hook_event_name, session_id, transcript_path and agent_id. It accepts only
                 PostToolUse/UserPromptSubmit and needs no tool_name. Band(usage, threshold) is 0 below the threshold,
                 then 1 + (usage−threshold)/Step. Line(usage, threshold, reentry) is one line. Envelope(event, line)
                 renders {systemMessage, hookSpecificOutput{hookEventName, additionalContext}}.
       covers:   c-2, c-3, c-5
       contract: - if Band's boundaries drift, the band table fails: (149_999,150k)=0, (150_000)=1, (199_999)=1,
                   (200_000)=2, (260_000)=3, and (any, threshold 0)=0. Step must equal 50_000 (nudge_cadence: fixed step)
                 - if Line loses any of the four locked parts (nudge_content), TestLineCarriesLockedParts fails. The
                   parts are: "152k/150k tokens", "checkpoint at your next durable boundary → /clear", the passed
                   re-entry text verbatim, and "mid-thought? /dross-pause first". The test also fails if the line does
                   not start with Prefix or contains "\n"
                 - if the envelope's systemMessage and additionalContext differ, hookEventName stops echoing the
                   payload's event, or any "decision"/"continue"/"permissionDecision" key appears, TestEnvelopeNeverBlocks
                   fails (c-3 both audiences; c-5 never blocks)
                 - if a UserPromptSubmit payload (no tool_name) is refused, or a PreToolUse/SubagentStop payload is
                   accepted, TestDecodeEventAllowlist fails
                 - if a payload carrying agent_id does not report Subagent()==true, TestSubagentPayloadFlagged fails
                   (c-2: a subagent's hook fire never nudges)
                 - if a session_id outside [A-Za-z0-9_-]{1,128} (e.g. "../../x") decodes as usable, TestSessionIDShape
                   fails

  t-4  Add [context] threshold to defaults.toml
       files:    internal/defaults/defaults.go, internal/defaults/defaults_test.go,
                 internal/cmd/testdata/cli_surface/output/defaults_show_json.txt
       desc:     ContextDefaults{Threshold *int `toml/json:"threshold,omitempty"`} under `context`, with
                 EffectiveThreshold(): nil → 150000, 0 → off. LoadFile rejects a negative value, naming
                 context.threshold. Re-mint the JSON golden ("context": {}).
       covers:   c-1
       contract: - if a missing defaults.toml or an absent [context] table does not yield 150000,
                   TestThresholdDefault150k fails
                 - if `threshold = 0` is read as "unset → 150000" (pointer lost), TestThresholdZeroIsOff fails
                 - if `threshold = -1` loads without an error naming context.threshold, TestNegativeThresholdRejected
                   fails
                 - if ContextDefaults gains a second field (a configurable step would be one), TestContextHasOneKnob
                   fails (reflect NumField == 1; nudge_cadence lock: one knob)
                 - if the tags diverge, TestTomlFieldsCarryMatchingJSONTags fails. If `defaults show --json` changes
                   shape unannounced, the output golden fails

  t-5  Claim one nudge per session band
       files:    internal/gatestate/nudge.go (new), internal/gatestate/nudge_test.go (new)
       desc:     ClaimNudge(repoDir, session, band) makes the marker .dross/gate/nudge-<session>-<band> with
                 O_CREATE|O_EXCL (through ensureDir's self-ignoring .gitignore). It returns false when that band or a
                 higher one is already claimed for the session. A successful claim prunes other sessions' markers.
       covers:   c-3, c-5
       contract: - if a second ClaimNudge(s,1) returns true, TestClaimOncePerBand fails ("does not repeat on every tool
                   call")
                 - if 8 goroutines claiming the same (session, band) see more than one true, TestConcurrentClaimSingleWinner
                   fails. Parallel tool calls fire concurrent PostToolUse hooks, and a read-modify-write record would
                   double-nudge
                 - if ClaimNudge(s,2) after ClaimNudge(s,3) returns true, TestLowerBandNeverRenudges fails
                 - if session B's claim does not reset the cadence (B's band 1 → true), or leaves A's markers on disk,
                   TestNewSessionFreshCadence fails
                 - if a session id like "../../x" creates any file, inside or outside .dross/gate, TestClaimRejectsPathSession
                   fails
                 - if a marker shows in `git status --porcelain` of a git-inited fixture, TestMarkersSelfIgnored fails

  t-6  Add context-checkpoint rule to playbook
       files:    assets/prompts/_interaction.md, internal/cmd/interaction_snippet_test.go,
                 internal/cmd/execute_prompt_test.go
       desc:     One short "## Context checkpoint" section. With a `context checkpoint:` line in context, the next
                 durable boundary leads with checkpoint + /clear. That covers execute §1g (even mid-wave; this overrides
                 the prompt's own option order) and every "safe to /clear" wrap-up. Spec/plan gates before their artifact
                 is on disk are not durable boundaries and don't change. Under --solo the nudge is informational: keep
                 going. If the thread isn't on disk, /dross-pause first. No new calls. execute.md is not edited
                 (gate_scope).
       covers:   c-4, c-9, c-8
       contract: - if _interaction.md loses any needle, TestInteractionSnippetHasContextCheckpointRule fails. The
                   needles are: "context checkpoint:", "durable boundary", "§1g", "even mid-wave", "safe to /clear",
                   "lead with checkpoint", "overrides", "not durable boundaries", "--solo", "informational",
                   "/dross-pause" (c-9)
                 - if the section names any `dross <verb>` CLI call (a new round-trip), TestContextRuleAddsNoCalls fails.
                   Slash commands are allowed (c-8, signal_path)
                 - if the section grows past 700 bytes, TestContextRuleSizeBudget fails. Every interactive command
                   re-reads it, and this is a context-economy milestone
                 - if §1g loses "mid-wave, lead with continue" or the wave-boundary checkpoint lead,
                   TestExecutePromptCheckpointWaveLead (extended) fails (c-4: below threshold, the order is unchanged)
                 - if any assets/prompts/*.md with a "safe to /clear" wrap-up stops running `dross interaction show`,
                   TestClearWrapUpsLoadPlaybook fails. The shared rule would then not reach that command (c-9: every
                   command's wrap-up)

  t-7  Document the nudge hook and threshold key
       files:    README.md, docs/dross.1, assets/prompts/options.md, internal/cmd/readme_doc_test.go
       desc:     README: a `dross hooks {ensure,nudge}` row (what ensure wires, that nudge is a hook-only verb), plus the
                 defaults row naming `[context] threshold` (default 150000, 0 = off). Man page: the same two entries.
                 options.md §8 names the key, because /dross-options claims to reach every dross-managed setting.
       covers:   c-1, c-6
       contract: - if README loses "dross hooks {ensure,nudge}", "[context]" or "threshold", or dross.1 loses
                   `dross hooks` or the threshold key, TestReadmeDocumentsContextNudge fails
                 - if options.md §8 stops naming `[context]` + `threshold`, TestOptionsNamesContextThreshold fails. The
                   knob would then exist only in source

Wave 2 (depends t-2, t-3, t-4, t-5, t-6)
  t-8  Add the `dross hooks nudge` hook verb
       files:    internal/cmd/hooks_nudge.go (new), internal/cmd/hooks_nudge_test.go (new), internal/cmd/hooks.go,
                 internal/cmd/incompleteroot_test.go
       desc:     The order is: stdin (LimitReader) → ctxnudge.Decode → FindRoot → EffectiveThreshold → open
                 transcript_path → LatestMainUsage → Band → gatestate.ClaimNudge → Line(…, suggestNext(root, proj, st)) →
                 Envelope to stdout. Any failure is recovered and exits 0 with empty stdout and stderr. Below the
                 threshold it does no state I/O. It registers under Hooks() with const nudgeHookCommand.
       covers:   c-1, c-2, c-3, c-5, c-7, c-8
       contract: - if a fire at 150_000 does not emit exactly one envelope, a re-fire at 160k in the same session does
                   not stay silent, or 200k does not emit again, TestNudgeCadenceEndToEnd fails (c-3, nudge_cadence)
                 - if a fire at 149_999 emits, or creates anything under .dross/gate, TestBelowThresholdNoStateIO fails
                 - if the re-entry text in the line differs from suggestNext() for the same fixture (what SessionStart's
                   `dross reentry` prints), TestNudgeNamesSessionStartReentry fails (nudge_content)
                 - if threshold = 0 emits at 900k, TestThresholdZeroSilent fails (c-1)
                 - if a payload with agent_id emits at 900k, TestSubagentFireSilent fails. The same holds when
                   transcript_path points at a subagents/agent-*.jsonl file (c-2)
                 - if the transcript read is not payload.transcript_path (e.g. derived from session_id),
                   TestReadsPayloadTranscriptPath fails. The fixture uses an arbitrary temp path
                 - if any of these exits non-zero or writes to stdout/stderr, the c-5 table fails: transcript missing,
                   a directory, mode 000, empty or garbage; stdin garbage; a malformed defaults.toml; a panic injected
                   via the nudgeEngine seam. Each row runs for PostToolUse and for UserPromptSubmit ("never blocks a
                   tool call or prompt")
                 - if `hooks nudge` writes to the tree or prints in a non-dross dir or an incomplete .dross,
                   TestHookTargetsWriteNothing (extended with the nudge verb + payload) fails (c-5)
                 - if any assets/prompts/*.md or assets/commands/*.md contains nudgeHookCommand,
                   TestPromptsNeverRunNudgeVerb fails (signal_path; c-8 zero round-trips)
                 - if ctxnudge.Prefix is not verbatim in the embedded assets.InteractionPlaybook,
                   TestPlaybookRecognisesNudgePrefix fails. If it appears in any other prompt,
                   TestNudgeRuleLivesOnlyInPlaybook fails (gate_scope: one rule, no per-prompt edits)
                 - if a fire on the 50 MB sparse transcript fails to emit, TestNudgeOn50MBTranscript fails. This catches
                   a whole-file read or scanner-limit regression at verb level

Wave 3 (depends t-8)
  t-9  Wire the nudge into `dross hooks ensure`
       files:    internal/hooks/settings.go, internal/cmd/hooks.go, internal/cmd/hooks_test.go,
                 internal/cmd/testdata/cli_surface/doctor_run.txt
       desc:     Add the EventUserPromptSubmit const. userHooks gains {PostToolUse, nudge} and {UserPromptSubmit,
                 nudge}. ensure's success line prints userHooksSummary() instead of the hard-coded five. Re-mint the
                 doctor golden: doctor iterates userHooks, so the ✓ lines change with the wiring.
       covers:   c-6
       contract: - if `hooks ensure` on an empty config does not leave exactly one nudge entry under PostToolUse and one
                   under UserPromptSubmit, with `dross gate record` still exactly once, TestHooksEnsureCommand (extended)
                   fails. It also fails if a second run is not byte-identical
                 - if ensuring over a settings.json already wired with the five pre-phase hooks plus a foreign
                   PostToolUse hook rewrites any existing group, or a second ensure writes, TestEnsureUpgradeAddsOnlyNudge
                   fails (the upgrade path every existing install takes)
                 - if ensure's stdout stops naming "UserPromptSubmit → dross hooks nudge", TestHooksEnsureNamesNudge fails
                 - if the wiring and doctor's ✓ lines drift apart, TestDoctorRunGolden fails

Wave 4 (t-10 depends t-9; t-11 depends t-1, t-9)
  t-10 Doctor flags a missing nudge hook
       files:    internal/cmd/doctor.go, internal/cmd/doctor_hooks_test.go
       desc:     hooksSection: a missing nudge is a warning (not an issue) whose text says context-threshold nudges are
                 off and names `dross hooks ensure`. Update the doc comment. The test helper pairs gain nudgePostPair /
                 nudgePromptPair, and the "all wired" fixtures include them.
       covers:   c-6
       contract: - if settings with every hook but the nudge do not show "⚠ PostToolUse → `dross hooks nudge` is not
                   wired" + "nudges are off" + "dross hooks ensure", TestDoctorMissingNudgeHookWarns fails. It also
                   fails if doctor exits non-zero for it (convenience hook, not a gate)
                 - if only the UserPromptSubmit nudge is missing and doctor stays silent about it, the same test's
                   second row fails
                 - if all hooks wired (incl. both nudges) produce anything but ✓ lines, TestDoctorGateHooks (extended
                   after-ensure list) fails

  t-11 Measure the after-flow and hook timing live
       files:    .dross/phases/context-boundaries/measurements.toml
       desc:     Run `make install` first (r-01). Set `[context] threshold` low in defaults.toml, replay t-1's
                 [scenario] exactly, then restore defaults. Record into [after]: turns, agent wall-clock, nudges seen,
                 and the §1g lead at the mid-wave gate. Record a below-threshold control (threshold 0 → mid-wave lead
                 stays continue). Record [hook_timing]: 50 fires of the installed binary on a 50 MB transcript, p50/p95
                 per path (below / claimed / emit).
       covers:   c-3, c-4, c-7, c-8
       contract: - if [after].turns > [before].turns, c-8 fails at verify ("adds zero agent round-trips")
                 - if [after].nudges_seen ≠ 1 for a single band crossing, or the systemMessage was not seen by the
                   human, c-3 fails at verify
                 - if [after].gate_lead ≠ "checkpoint" at the mid-wave §1g, or the control's lead ≠ "continue", c-4
                   fails at verify. This is the only proof the model obeys the playbook rule, not just that the rule
                   exists
                 - if any [hook_timing] p95 > 50 ms, c-7 fails at verify
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 threshold in defaults.toml, default 150k, 0 = off | t-4, t-7, t-8 |
| c-2 latest main-agent turn, three input fields, subagents excluded | t-2, t-3, t-8 |
| c-3 one line on crossing, model + user, no per-call repeat | t-3, t-5, t-8, t-11 |
| c-4 §1g leads with checkpoint past threshold even mid-wave, unchanged below | t-6, t-11 |
| c-5 non-dross / bad transcript → exit 0 silent, never blocks | t-2, t-3, t-5, t-8 |
| c-6 ensure wires idempotently, doctor flags missing | t-7, t-9, t-10 |
| c-7 ≤ 50 ms per fire on 50 MB, tail-only | t-2, t-8, t-11 |
| c-8 zero added round-trips, before/after recorded in phase dir | t-1, t-6, t-8, t-11 |
| c-9 every durable wrap-up leads with checkpoint + /clear; test guards _interaction.md | t-6 |

Locked decisions → enforcing contract: nudge_cadence → t-3 band table + Step const, t-4 one-knob reflect test, t-5 claim tests. gate_scope → t-6 needles + t-8 TestNudgeRuleLivesOnlyInPlaybook, with execute.md left unedited. solo_behaviour → t-6 "--solo"/"informational" needles. signal_path → t-6 no-calls test + t-8 TestPromptsNeverRunNudgeVerb. nudge_content → t-3 TestLineCarriesLockedParts + t-8 TestNudgeNamesSessionStartReentry.

## Judgment calls

- **Hook events.** Chose: wire the nudge on both PostToolUse and UserPromptSubmit. Rejected: PostToolUse only. Why: c-5's "never blocks a … prompt" is only testable if a prompt event is wired, and UserPromptSubmit catches a crossing in a turn with no tool call. The claim markers keep the two events from double-nudging.
- **Cadence state.** Chose: O_EXCL marker per (session, band). Rejected: one read-modify-write `nudge.json`. Why: parallel tool calls fire concurrent PostToolUse hooks, so a RMW record double-nudges. Exclusive create makes "exactly one winner" a deterministic test.
- **No band reset when usage drops after a compaction.** Chose: no reset. Rejected: reset-on-drop. Why: a reset would need a state read on every below-threshold fire. Keeping that path free of state I/O protects c-7, and the lock already says "same session".
- **Verb name.** Chose: `dross hooks nudge`. Rejected: `dross context …` (the signal_path lock names that shape as forbidden at gates) and folding into `dross gate record` (c-6 needs a separately wired, doctor-checkable hook).
- **Payload decode.** Chose: ctxnudge's own five-field decode. Rejected: reusing gate.Decode. Why: it refuses payloads with no tool_name, which every UserPromptSubmit is, and relaxing it would weaken a gate invariant.
- **Logic location.** Chose: a new internal/ctxnudge package. Rejected: logic in internal/cmd. Why: forced by the cmd `encoding/json` ban, and it also moves the heavy tests out of the slow cmd package.
- **Subagent exclusion.** Chose: two guards, skipping isSidechain lines and staying silent when the payload has agent_id. Rejected: transcript-only. Why: a subagent's own hook fire would put the nudge into the subagent's context, not the main agent's.
- **c-4 delivery.** Chose: only _interaction.md, with execute.md untouched. Its §1g "mid-wave, lead with continue" text is pinned, which proves "below, unchanged". Rejected: a one-line pointer in §1g. Why: gate_scope's "no per-prompt edits" is locked. The rule says outright that it overrides the prompt's order.
- **c-7 gate.** Chose: a deterministic byte-budget test plus a package-level min-of-5 timing in ctxnudge, with process-level p95 recorded in t-11. Rejected: a wall-clock assertion in internal/cmd. Why: that package is known to stall under memory pressure, so a timing assertion there flakes.
- **c-8 baseline.** Chose: measure on the pre-phase binary in wave 1. Rejected: threshold=0 on the new binary as "before". Why: at 0 the hook still spawns on every tool call, so that is not today's flow.
- **Live e2e evidence (t-11).** Chose: observe one live run for c-3 and c-4 alongside the c-8 timings. Rejected: relying on prompt tests alone. Why: needle tests prove the rule's text exists, not that the model leads with checkpoint mid-wave.
- **Doctor severity.** Chose: a missing nudge is a warning. Rejected: an issue. Why: dross works without it. Only the tool-call gate pair is an issue today, and this matches PreCompact/SessionStart.
- **Agent hand-running the verb.** Chose: a prompt-side test (no prompt names the verb). Rejected: extending gate-off-guard to refuse `dross hooks nudge` from Bash. Why: a forged fire can at worst emit or swallow one informational line, and nothing approvable is forgeable.
- **Malformed defaults.toml.** Chose: the hook is silent. Rejected: a warning on stderr. Why: it would print on every tool call. `dross defaults show` surfaces the error, and a negative threshold is rejected at load so it never reads as "off".
- **Playbook size budget.** Chose: a 700-byte cap on the new section. Rejected: an uncapped section. Why: every interactive command re-reads _interaction.md, and this milestone is about cutting re-reads.
- **Docs split out (t-7, wave 1).** Chose: a separate docs task. Rejected: folding docs into the wiring/doctor tasks. Why: those would cross the 5-file line, and docs need no code output, so they parallelise.
