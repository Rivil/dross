# context-boundaries — risk-lens draft

Lens: failure modes drive the graph. Each risk in the register below has exactly one owning
task, and that task's test_contract is the test that goes red when the risk materialises.
Tasks follow risk clusters: config round-trip, transcript parsing, cadence and concurrency,
hook-payload semantics, process contract, wiring, prompt rule, and measurement honesty. They
are not cut along features.

The logic lives in a new `internal/nudge` package, so its tests are fast package tests. The cmd
verb stays thin, because `internal/cmd` is the slow package that hangs under memory pressure.

## Risk register (risk → owner)

| # | What breaks | Owner |
|---|---|---|
| R1 | A plain-int threshold: `defaults save` / `stats opt-out` re-save writes `threshold = 0` and silently turns the nudge off (BurntSushi v1.6 `omitempty` does not omit zero ints) | t-1 |
| R2 | Unset vs explicit 0 vs negative vs non-integer threshold confused | t-1 |
| R3 | Newest assistant line is a sidechain or `<synthetic>` zero-usage entry, so the reported usage is wrong | t-2 |
| R4 | Torn last line, a >64 KiB line (bufio.Scanner limit), or spaced JSON makes the reader miss the entry | t-2 |
| R5 | Whole-file read on a 50 MB transcript blows c-7 | t-2 |
| R6 | `transcript_path` is a FIFO, so `open` blocks every tool call until the hook times out | t-2 |
| R7 | Off-by-one at the threshold / step edges, or a multi-bucket jump nudges more than once | t-3 |
| R8 | Parallel PostToolUse fires double-nudge (stat-then-write race) | t-3 |
| R9 | session_id path traversal; empty session_id nudges on every call | t-3 |
| R10 | Claim pruning deletes real gate records (green/approval) | t-3 |
| R11 | Subagent-origin fire injects into the subagent and burns the bucket, so the main agent never sees it | t-4 |
| R12 | Envelope reaches only one audience, `hookEventName` mismatches, or a `decision` key forces an extra turn | t-4 |
| R13 | Expensive work (suggestNext → git) on the hot path | t-4 |
| R14 | A failed compose burns the bucket with nothing printed | t-4 |
| R15 | Verb returns an error, so main prints `dross: …` and exits 1, and Claude Code shows a hook error on every tool call | t-6 |
| R16 | Telemetry append on every tool call (latency, plus noise in the usage log) | t-6 |
| R17 | Nudge names a different re-entry command than SessionStart prints | t-6 |
| R18 | Wired hook command string doesn't resolve to a real verb, so every tool call errors | t-7 |
| R19 | Upgrade from today's five-hook install drops or reorders foreign/gate groups, or is not byte-stable | t-7 |
| R20 | Broken `[context]` config leaves the nudge silently off, with no surface anywhere | t-7 |
| R21 | Go marker text and the prompt rule drift apart, so the model never recognises the nudge | t-5 |
| R22 | A `safe to /clear` wrap-up lives in a prompt that never loads `_interaction.md` | t-5 |
| R23 | The solo clause or the mid-command exemption is lost; a prompt starts polling context (signal_path) | t-5 |
| R24 | The rule bloats the playbook every interactive command loads | t-5 |
| R25 | "After" leg measured on a stale binary, or on a flow where the nudge never fired; numbers unobserved | t-8 |

```
Phase context-boundaries — 8 tasks across 5 waves

Wave 1
  t-1  Add context threshold to defaults.toml
       files:    internal/defaults/defaults.go, internal/defaults/defaults_test.go, README.md
       covers:   c-1
       depends:  —
       desc:     Defaults gains `Context ContextDefaults toml:"context,omitempty"` holding exactly one
                 field, `Threshold *int64 toml:"threshold,omitempty"`. EffectiveThreshold():
                 nil → DefaultContextThreshold (150000), 0 → 0 (off), <0 → error. README's
                 defaults.toml line names the key and "0 = off".
       contract: no [context] table → 150000; `threshold = 0` → 0 with no error. A non-pointer field
                   (unset == 0) returns 0 for the first case and fails TestContextThresholdUnsetVsZero
                 round-trip: load `[context] threshold = 0` + `[telemetry] enabled = false`, mutate
                   Remote (what `defaults save` does) and Telemetry (what `stats opt-out` does),
                   SaveFile, reload → still 0. A file with no [context] re-saves with no `[context]`
                   table (TestContextThresholdSurvivesOtherWriters)
                 `threshold = -1` → EffectiveThreshold error; `threshold = "150k"` → LoadFile error naming
                   the path, never a silent 150000
                 reflect: ContextDefaults has exactly one field. A configurable step field (locked
                   nudge_cadence: 50k is fixed) fails TestContextDefaultsOneKnob
                 internal/cmd golden output/defaults_show.txt and defaults_show_json.txt stay
                   byte-identical: a field that leaks `[context]` into every `defaults show` fails
                   TestOutputGolden

  t-2  Read latest main-agent usage from transcript tail
       files:    internal/nudge/transcript.go (new), internal/nudge/transcript_test.go (new)
       covers:   c-2, c-5, c-7
       depends:  —
       desc:     LatestMainContext(path) (tokens int64, ok bool): open O_RDONLY|O_NONBLOCK, require a
                 regular file, then read backwards in 64 KiB chunks up to an 8 MiB scan cap. Newest-first,
                 the first entry that is type "assistant", has isSidechain false, a usage object, a
                 model other than "<synthetic>" and a non-zero sum wins. tokens = input +
                 cache_read_input + cache_creation_input, saturating. Unparseable lines are skipped.
                 Fixtures are synthetic only, never copied from real ~/.claude transcripts.
       contract: tail [main assistant 120,000, then sidechain assistant 190,000] → 120,000. A reader
                   that takes the newest assistant line regardless of isSidechain returns 190,000 and fails
                 usage {input 2, cache_read 151,000, cache_creation 407} → 151,409; a missing
                   cache_creation key counts as 0 and the entry is not skipped
                 newest main entry is model "<synthetic>" with all-zero usage → the previous real entry
                   is returned, not 0
                 last line torn (no closing brace, no trailing newline) → skipped, previous entry returned
                 a 3 MiB single user/tool_result line after the latest assistant entry → still found. A
                   bufio.Scanner at its default 64 KiB token limit fails this
                 `{"type": "assistant", …}` (space after the colon) is still recognised
                 50 MB synthetic transcript, latest main entry 200 KiB from EOF → a counting ReaderAt
                   records ≤ 1 MiB read. A whole-file read fails TestTailReadIsBounded
                 no qualifying entry within the 8 MiB cap → ok=false, never (0, true)
                 path is a FIFO with no writer → returns ok=false inside a 1 s test deadline (a plain
                   os.Open blocks forever); path is a directory, missing, or mode 000 → ok=false
                 three usage fields of 4e18 each → a positive, saturated sum, not a wrapped negative

  t-3  Nudge cadence: bucket, once-only claim, line
       files:    internal/nudge/nudge.go (new), internal/nudge/nudge_test.go (new),
                 internal/gatestate/store.go, internal/gatestate/store_test.go
       covers:   c-2, c-3
       depends:  —
       desc:     Bucket(tokens, threshold) (n int, over bool): over iff threshold > 0 && tokens ≥
                 threshold; n = (tokens − threshold) / 50_000 (unexported const step). Exported Marker
                 plus Line(tokens, threshold, reentry), which renders the locked one-liner.
                 gatestate.NudgeClaimed / ClaimNudge(root, sessionID, n): O_CREATE|O_EXCL file under
                 .dross/gate/nudge/ named sha256(sessionID)[:16]-n via the existing self-ignoring
                 ensureDir. Claims older than 7 days are pruned on a successful claim, inside nudge/ only.
       contract: Bucket table: 149,999 → not over; 150,000 → n=0 (c-2 "at or above"); 199,999 → 0;
                   200,000 → 1; 250,000 → 2; threshold 0 at 10,000,000 → not over
                 a 140k→260k jump yields n=2, and claiming 2 is the only nudge: buckets 0 and 1 are
                   never claimed separately, so one fire gives one nudge
                 32 goroutines calling ClaimNudge(root, "s", 0) concurrently → exactly one true. A
                   stat-then-write claim fails TestClaimNudgeExactlyOnce
                 per-session: (s1,0) true; (s2,0) true; (s1,0) false; (s1,1) true
                 session "../../escape" and "a/b" → the claim file lands inside .dross/gate/nudge/;
                   empty session id → error and no file. The pipeline must stay silent, not nudge
                   every fire
                 in a git temp repo, `git status --porcelain` is empty after a claim (gitignore covers
                   nudge/)
                 a claim with an 8-day-old mtime is pruned on the next claim, a 1-day-old one survives,
                   and an 8-day-old .dross/gate/green.json is untouched (prune scoped to nudge/)
                 Line(151_409, 150_000, "/dross-execute — run the next task") starts with Marker and
                   contains "151k / 150k", "checkpoint at your next durable boundary → /clear", the
                   reentry string verbatim, and "mid-thought? /dross-pause first". A reentry string
                   containing "\n" still yields a line with no newline (locked nudge_content:
                   one line)

Wave 2 (depends t-2, t-3)
  t-4  Nudge pipeline over the hook payload
       files:    internal/nudge/run.go (new), internal/nudge/run_test.go (new),
                 internal/nudge/testdata/posttooluse_main.json (new),
                 internal/nudge/testdata/posttooluse_subagent.json (new)
       covers:   c-2, c-3, c-5, c-8
       depends:  t-2, t-3
       desc:     FIRST capture two real PostToolUse payloads in a scratch dross repo with a temporary
                 stdin-dump hook: one from a main-agent call, one from a call made inside a subagent.
                 Use harmless content and confirm by eye that there are no secrets. The capture decides
                 whether agent_id marks subagent origin. If it doesn't, fall back to "tool_use_id not
                 among the latest main-agent message's tool_use blocks".
                 Run(payload, Env{Threshold, Reentry}) []byte: narrow decode (hook_event_name,
                 session_id, transcript_path, cwd, agent_id, tool_use_id). Silent unless event ==
                 PostToolUse, main-agent origin, gate.LocateRoot(cwd) finds a repo, threshold > 0,
                 transcript ok, over threshold and the bucket unclaimed. Then compose the line
                 (Env.Reentry) BEFORE claiming, claim, and emit {"systemMessage": line,
                 "hookSpecificOutput": {"hookEventName": "PostToolUse", "additionalContext": line}}.
       contract: captured subagent fixture + over-threshold transcript → nil output AND no claim file
                   (TestSubagentFireNeitherNudgesNorClaims). Ignoring origin injects into the subagent
                   and burns the bucket
                 captured main fixture, over threshold → exactly one JSON object; systemMessage ==
                   additionalContext == the line; hookEventName == "PostToolUse"; no "decision" key
                   (decision:block forces an extra model turn, which c-8 forbids). Second Run on the
                   same session and bucket → nil
                 nil output for each of: non-JSON stdin, a JSON array, cwd outside any dross repo,
                   empty session_id, missing transcript, Env.Threshold error, hook_event_name
                   "UserPromptSubmit" or "SessionStart" (never emit a mismatched hookEventName), and
                   threshold 0 with transcript_path pointing at a FIFO, which returns at once and proves
                   the off switch never opens the transcript
                 Env.Reentry call counter stays 0 when under threshold and when the bucket is already
                   claimed (hot path spawns no git)
                 Env.Reentry panics → no claim written; the next Run with a working Reentry still
                   nudges for that bucket. Claim-before-compose fails TestFailedComposeKeepsBucket

  t-5  Interaction rule: checkpoint + /clear past threshold
       files:    assets/prompts/_interaction.md, internal/cmd/interaction_snippet_test.go,
                 internal/cmd/execute_prompt_test.go
       covers:   c-4, c-8, c-9
       depends:  t-3
       desc:     One short section in _interaction.md: a line starting with Marker in context means the
                 next durable boundary's options lead with "checkpoint + /clear". That covers execute
                 §1g even mid-wave, outranking §1g's mid-wave `continue` lead, and every "safe to
                 /clear" wrap-up. Mid-command gates before their artifact is written don't change.
                 --solo keeps going and the nudge is informational. Never run a dross command to check
                 context. execute.md is not edited (gate_scope lock: no per-prompt edits). Existing
                 canonical playbook phrases are left untouched (rules-side drift guard).
       contract: TestInteractionSnippetHasContextNudgeRule: _interaction.md contains nudge.Marker
                   verbatim. Renaming the Go marker without the prompt fails it. The file also contains
                   "checkpoint + /clear", "next durable boundary", "mid-wave", "§1g", "--solo",
                   "informational" and the mid-command exemption ("before its artifact is written");
                   dropping the rule, its solo clause or its exemption fails
                 `dross interaction show` output (the embedded asset) contains Marker. A stale embed
                   fails
                 every assets/prompts/*.md containing "safe to /clear" also contains `dross interaction
                   show`. A wrap-up the rule cannot reach fails TestClearWrapupsLoadInteractionRule
                 no assets/prompts/*.md contains "dross hooks nudge" or a `dross context` invocation
                   (signal_path: a gate that polls context adds a round-trip)
                 execute.md §1g still contains "Mid-wave, lead with `continue`", and its checkpoint
                   bullet still names exactly `dross validate` and `dross task next`. Below-threshold
                   order unchanged (c-4) and no command added to the checkpoint path (c-8)
                 _interaction.md ≤ 5,300 bytes (4,482 today). Every interactive command loads it, so a
                   ballooned rule fails TestInteractionSnippetBudget

Wave 3 (depends t-1, t-4)
  t-6  Add `dross hooks nudge` hook verb
       files:    internal/cmd/hooks_nudge.go (new), internal/cmd/hooks_nudge_test.go (new),
                 internal/cmd/hooks.go, internal/cmd/telemetry.go, cmd/dross/testdata/cli_tree.txt
       covers:   c-3, c-5, c-7
       depends:  t-1, t-4
       desc:     const NudgeHook = "dross hooks nudge". The verb reads stdin capped at maxGatePayload
                 and calls nudge.Run through a swappable seam with Env{Threshold: defaults
                 EffectiveThreshold, Reentry: suggestNext over project+state, the same generator
                 reentryLine uses}. It writes Run's bytes verbatim, recovers panics, always returns
                 nil, and sets SilenceUsage/SilenceErrors. Registered under Hooks(). RecordCLIEvent
                 skips it alongside the gate verbs. Re-mint cli_tree.txt.
       contract: exit 0 with empty stdout AND empty stderr for: closed/empty stdin, garbage stdin, an
                   undecodable defaults.toml, and a seam that panics. A returned error makes main print
                   "dross: …" and exit 1, which Claude Code shows as a hook error on every tool call
                 RecordCLIEvent(`dross hooks nudge`) writes 0 telemetry events while `dross status` still
                   writes 1 (skip not too wide), mirroring the gate-verb test
                 fixture repo with a planned phase and a runnable task, over-threshold payload → the
                   line's re-entry segment equals the "next: …" segment `dross reentry` prints for the
                   same repo ("/dross-execute — run the next task"). A hand-rolled string fails
                 root.Find("hooks","nudge").CommandPath() == NudgeHook
                 cli_tree.txt gains exactly the `dross hooks nudge` line. TestCLITreeGolden fails if its
                   Use/Short drift

Wave 4 (depends t-1, t-6)
  t-7  Wire nudge hook; doctor flags it
       files:    internal/cmd/hooks.go, internal/cmd/hooks_test.go, internal/cmd/doctor.go,
                 internal/cmd/doctor_hooks_test.go
       covers:   c-1, c-6
       depends:  t-1, t-6
       desc:     userHooks gains {PostToolUse, NudgeHook}, matcher-less, after the gate record. `hooks
                 ensure` prints userHooksSummary() instead of its hand-typed format string, and its
                 Short names the nudge. doctor's Hooks section: a missing nudge is ⚠, not ✗. A new
                 context line shows the effective threshold ("150k (default)", "off (threshold 0)"),
                 or ⚠ "nudge is silently off" naming defaults.toml when it won't decode or is negative.
       contract: today's five-hook settings.json + foreign PostToolUse group + ensure → PostToolUse is
                   [foreign, gate record, hooks nudge] in that order; a second ensure is byte-identical.
                   TestEnsureUserHooksUpgradesTwoHookInstall's PostToolUse count moves 1 → 2
                   deliberately, not deleted
                 TestUserHooksResolve: every userHooks command resolves through a root holding Pause,
                   Reentry, Gate and Hooks to a command whose CommandPath equals the wired string. A
                   typo'd hook command (which errors on every tool call) fails
                 ensure's changed-file output and init/onboard's ensured-hooks line contain
                   "PostToolUse → dross hooks nudge" (TestHookWordingNamesAllFour extended)
                 settings with everything except the nudge → doctor exits 0 and the Hooks section has
                   "⚠ PostToolUse → `dross hooks nudge` is not wired" with the `dross hooks ensure` fix.
                   An ✗ fails doctor for a convenience hook
                 defaults.toml `[context] threshold = "150k"` → ⚠ line naming defaults.toml and "off";
                   `threshold = -5` → ⚠; `threshold = 0` → ✓ "off (threshold 0)"; absent → "150k
                   (default)"

Wave 5 (depends t-5, t-7)
  t-8  Measure flow cost and hook latency
       files:    .dross/phases/context-boundaries/flow-cost.toml (new),
                 internal/cmd/flowcost_record_test.go (new)
       covers:   c-4, c-7, c-8
       depends:  t-5, t-7
       desc:     `make install` (r-01), then confirm `dross version` matches HEAD. In a scratch dross
                 repo, run pair-mode §1g checkpoint → /clear → /dross-execute --from twice: [before]
                 on the pre-phase binary, [after] on this one, with a lowered threshold so the nudge
                 fires before a mid-wave gate. From the transcripts, count main-agent turns (distinct
                 message.id) and wall-clock (timestamp delta). Record the binary versions, session ids,
                 the jq method, the nudge firing and the option §1g led with. Time 50 fires of the
                 installed `dross hooks nudge` against a 50 MB synthetic transcript plus a 10 MiB
                 tool_response payload and record p50/p95. Only observed numbers are recorded.
       contract: TestFlowCostRecords: every .dross/phases/*/flow-cost.toml parses; [before] and [after]
                   each have agent_turns > 0, wall_clock_s > 0, and non-empty binary_version and
                   session_id; after.agent_turns ≤ before.agent_turns (an added round-trip fails);
                   context-boundaries' record exists (no vacuous pass)
                 after.binary_version ≠ before.binary_version. An "after" leg run on the stale
                   installed binary fails
                 after.nudge_fired == true, after.mid_wave == true and after.gate_lead == "checkpoint".
                   An after-leg where the nudge never fired cannot prove zero added turns, and fails;
                   so does one whose §1g still led with continue (observed c-4)
                 [hook_latency] fires ≥ 50, transcript_mb ≥ 50, p95_ms ≤ 50. A recorded p95 over budget
                   fails
```

## Coverage

| Criterion | Tasks | What each owns |
|---|---|---|
| c-1 | t-1, t-7 | t-1: key, 150k default, 0 = off, survives other writers. t-7: effective value and broken config surfaced in doctor |
| c-2 | t-2, t-3, t-4 | t-2: input + cache-read + cache-creation from the tail, sidechain/synthetic skipped. t-3: "at or above" edge. t-4: subagent-origin fires don't count |
| c-3 | t-3, t-4, t-6 | t-3: once per bucket, no repeat on parallel fires, one-line content. t-4: systemMessage + additionalContext (user + model). t-6: names the same re-entry command SessionStart prints |
| c-4 | t-5, t-8 | t-5: rule outranks §1g's mid-wave lead; below-threshold text pinned unchanged. t-8: observed checkpoint lead mid-wave |
| c-5 | t-2, t-4, t-6 | t-2: missing/unreadable/FIFO transcript. t-4: non-dross cwd, bad payload, off switch. t-6: process always exits 0, stdout/stderr empty |
| c-6 | t-7 | ensure idempotent and upgrade-safe; doctor ⚠ when missing; wired command resolves |
| c-7 | t-2, t-4, t-6, t-8 | t-2: bounded tail read. t-4: no git/Reentry on hot path. t-6: no telemetry per fire. t-8: measured p95 on the installed binary |
| c-8 | t-4, t-5, t-8 | t-4: no `decision` key (no forced turn). t-5: no prompt polls context; checkpoint path unchanged. t-8: before/after turns + wall-clock recorded |
| c-9 | t-5 | rule in _interaction.md, reaches every "safe to /clear" prompt, and a test fails if it's lost |

All 9 criteria are covered. All 5 locked decisions are honoured: nudge_cadence is t-1 + t-3, gate_scope is
t-5 with no execute.md edit, solo_behaviour is t-5's solo clause, signal_path is t-4 + t-5's
no-poll guard, and nudge_content is t-3's Line parts.

## Judgment calls

- **PostToolUse only, not also UserPromptSubmit.** Every dross durable boundary is reached through a tool call (commit, `dross validate`, AskUserQuestion), so PostToolUse alone reaches it. A second event doubles the payload shapes to capture, and it adds the one event where a stray exit 2 erases the user's prompt.
- **A separate verb, not folded into `dross gate record`.** gate record's contract is stderr-only and never stdout. Folding the nudge in would couple a nudge bug to the gate recorder, and c-6 wants a distinct hook doctor can flag.
- **Named `dross hooks nudge`, not `dross context …`.** A `dross context` verb invites the gate-time call that signal_path forbids. t-5 also guards that no prompt names the verb.
- **A narrow decoder in internal/nudge, not gate.Decode.** gate.Decode refuses payloads without tool_name and is the gates' contract. Widening it risks every gate for a nudge.
- **O_EXCL claim files in .dross/gate/nudge/, not a JSON record or a user-level dir.** A record is read-modify-write and races under parallel PostToolUse. A user-level dir would act outside dross repos and lose the self-ignoring .gitignore.
- **Claims never reset within a session.** After compaction, re-crossing 150k waits for 200k. The locked cadence says "first crossing … in the same session", and a reset would add a per-fire readdir. /clear gets a fresh session id anyway.
- **Threshold as `*int64`.** BurntSushi v1.6 `omitempty` does not omit zero ints, so a plain int would write `threshold = 0` on every `defaults save` and `stats opt-out`.
- **An invalid threshold keeps the hook silent and doctor warns.** Falling back to 150k was rejected because it ignores what the user wrote. Erroring in the hook was rejected because c-5 forbids it.
- **No execute.md edit; the override clause lives in the _interaction.md rule.** The gate_scope lock says no per-prompt edits. The contradiction risk with §1g's mid-wave `continue` is handled by the rule naming §1g explicitly and by t-8 observing the lead. A one-clause §1g pointer was rejected despite being the tighter fix, because the lock forbids it.
- **Compose the line before claiming.** A failed compose never burns a bucket. The cost is that Reentry can run twice under a race; only the claim winner prints.
- **Payload fixtures captured live; transcript fixtures synthetic.** No existing fixture shows a subagent-origin PostToolUse, so its shape comes from capture, not memory. Real transcripts can hold secrets, so none are copied.
- **c-7 is split.** The deterministic bytes-read bound lives in unit tests. Wall-clock is measured on the installed binary in t-8, not as an in-suite timing assert, because internal/cmd tests are unreliable on the laptop. The startup floor measured today is 5.6 ms p50 / 7.1 ms p95, so the budget is reachable without a separate binary and no spike task is needed.
- **The flow-cost record is TOML with a generic guard test over all phases.** The four later v1.8 phases owe the same record (milestone c-6). Requiring `after.binary_version ≠ before` closes the stale-binary trap.
- **_interaction.md capped at 5,300 bytes.** Every interactive command loads it, and v1.8 is a context-economy milestone.
- **Doctor: a missing nudge is ⚠, not ✗.** dross works without it, the same as PreCompact and SessionStart.
- **Logic in internal/nudge, with a thin cmd verb.** Most contracts then run in a fast package instead of the slow internal/cmd package.
- **5 waves, not 3.** The wiring (t-7) waits for the verb (t-6), so a hook can never be wired to a command that doesn't exist yet. Collapsing it would put 8+ files in one task.
