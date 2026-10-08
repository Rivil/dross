# context-boundaries — panel synthesis

## Scores

| Draft | Dimension | Score | Note |
|---|---|---|---|
| risk | Criteria coverage | 5/5 | Covers all 9 criteria, and the risk register gives every failure mode one owner. c-4 and c-8 are observed live, and an in-suite test guards the record |
| risk | Contract specificity | 4/5 | Has the most named-failure contracts of the three. One contract is false: the JSON defaults golden cannot stay byte-identical. The §1g needle also keeps backticks that the test helper strips |
| risk | Granularity | 4/5 | 8 tasks, cut along risk clusters. t-7 (wiring + doctor) reaches 6 files once the goldens it missed are added |
| risk | Wave correctness | 5/5 | Every edge is a real output dependency, so all 5 waves are justified |
| mvp | Criteria coverage | 3/5 | Claims 9/9, but c-4 is proven only by a needle, c-8 is a markdown table judged at verify, and subagent origin is assumed from agent_id |
| mvp | Contract specificity | 3/5 | Uses concrete values for the sum, the band and the line. The cadence store is read-modify-write with no concurrency contract, and the c-7 timing assert sits in the slow internal/cmd package |
| mvp | Granularity | 2/5 | t-1 bundles defaults, the transcript reader, cadence, the line and payload parsing across two packages |
| mvp | Wave correctness | 4/5 | The edges are correct. The playbook task (t-2) has no link to the Go marker it must match, so drift between them is unguarded |
| verification | Criteria coverage | 5/5 | Covers all 9. It is the only draft with a below-threshold control run and with docs for c-1 and c-6 |
| verification | Contract specificity | 5/5 | Every contract names its failing test. It is right about the JSON golden and the normalised §1g needle, but misses cli_tree.txt |
| verification | Granularity | 3/5 | 11 tasks. t-8 carries 12 contracts and the whole pipeline inside the slow internal/cmd package |
| verification | Wave correctness | 3/5 | Wave 1 holds 7 tasks, and the baseline sits beside code tasks with nothing ordering it before a `make install`. t-8 waits on the playbook task only for one coupling test |

**Skeleton: risk.** Its register gives every failure mode a test that goes red, its logic lives in a fast package, and every wave edge is strict. It needs five grafts: doctor_run.txt, the JSON-golden fix, a c-8 baseline taken first, a docs task, and a wiring/doctor split.

Repo checks that moved the scores:
- **risk t-1 is false: defaults_show_json.txt cannot stay byte-identical.** encoding/json never omits an empty struct. Today's golden already has `"telemetry": {}`, so a `Context ContextDefaults` field adds `"context": {}`. verification t-4 predicted this correctly. The TOML golden does stay identical. The test is `TestOutputGoldens`, not `TestOutputGolden`.
- **risk t-7 misses `internal/cmd/testdata/cli_surface/doctor_run.txt`.** Its Hooks section prints a ✓ line for each userHooks entry (lines 41–46), so TestDoctorRunGolden goes red. mvp and verification both list the file.
- **No draft re-mints `cmd/dross/testdata/cli_tree.txt` for the ensure Short change.** risk t-7 and mvp t-4 both change `hooks ensure`'s Short, which alters line 62. verification also needs the change for UserPromptSubmit. mvp's "no cli_surface golden for the hooks tree" is wrong in effect: cli_tree.txt lists the hooks tree, so `hooks nudge` turns TestCLITreeGolden red too. Only risk t-6 lists the file.
- **Only verification's §1g needle form works.** `executePromptContent` lowercases execute.md and strips backticks, `*` and `_`. So the needle must be `mid-wave, lead with continue`; risk's and mvp's needles keep the backticks and would never match.
- **mvp is right that doctor needs no code to warn.** `hooksSection` loops over userHooks and grades every non-gate command ⚠ (doctor.go:1149–1162).
- **mvp's TestFooterPromptsLoadPlaybook doesn't exist yet; it would be a new test.** `footerCoverage` does exist, and its 9 footer-bearing prompts match mvp's list exactly.
- **mvp's git-spawn objection is true but doesn't apply to the other designs.** `gatestate.load` does spawn `git ls-files` (RefuseTracked). But only the load path does that, and the O_EXCL claim designs never call load.
- **The repo supports risk's caution about agent_id.** gate.Decode refuses payloads with no tool_name (except SubagentStop), as risk and verification say. `gate.Payload`'s own comment documents agent_id as a SubagentStop field only, and the repo has no fixture showing it on a subagent's PostToolUse.
- **mvp and verification resolve the root with `cmd.FindRoot`, which writes.** It uses the process cwd and materialises state.json. `gate.LocateRoot(dir)` only stats. `TestRootHelperCallersAreAllowlisted` pins which internal/cmd files may swallow ErrNoRoot or call LocateRoot. Its identifier scan also matches `gate.LocateRoot`. Only verification lists incompleteroot_test.go.
- **verification's transcript claim holds.** Subagent turns live in `<session>/subagents/agent-*.jsonl`, all with isSidechain true, and main-file turns are all false. I checked this with count-only jq on this machine.
- **`suggestNext` runs git, so R13 and mvp's hot-path concern are real.** It reaches git through `reconcilableCount` and `phaseMergeState`.
- **Confirmed true:** `_interaction.md` is 4,482 bytes, and `forbiddenInCmd` bans encoding/json, os/exec, net/http, go/ast and BurntSushi/toml. `render.MarshalJSON` exists, and BurntSushi is at v1.6.0.

## Merged plan

Package naming follows the majority: `internal/ctxnudge` (mvp and verification; risk called it `internal/nudge`). Bands are 1-based, so 0 means below the threshold (mvp and verification; risk used `n=0` plus an `over` flag). The marker literal is verification's `context checkpoint:`. Claim code goes in a new `internal/gatestate/nudge.go` (verification) rather than an edit to store.go (risk).

```
Phase context-boundaries — 11 tasks across 5 waves

Wave 1
  t-1  Record checkpoint-flow baseline on the pre-phase binary   [verification+risk]
       files:    .dross/phases/context-boundaries/flow-cost.toml (new)
       covers:   c-8
       depends:  —
       desc:     Run FIRST in wave 1, before any `make install` in this phase. `dross interaction show` serves the
                 playbook embedded in the installed binary, so that binary is what "before" means. Record
                 `dross version`. In a scratch dross repo holding one wave of two tasks, a human runs: §1g after t-1
                 (mid-wave) → checkpoint → /clear → /dross-execute --from t-2 → the §1c approval turn. Count
                 main-agent turns (distinct message.id) and agent wall-clock (human wait excluded) with jq over the
                 transcripts. Write [scenario] (repo shape, wave position, flow endpoints, jq method) and [before]
                 (agent_turns, wall_clock_s, binary_version, session_id). Reads are structural only; no
                 transcript content is copied.
       contract: if [before].binary_version equals t-11's [after].binary_version, TestFlowCostRecords (t-11)
                   fails. A threshold=0 run on the new binary is not "today", because the hook still spawns
                 if [scenario] lacks the repo shape, wave position or flow endpoints, TestFlowCostRecords'
                   required-keys check fails, and t-11 cannot replay the flow identically

  t-2  Add [context] threshold to defaults.toml   [risk+verification]
       files:    internal/defaults/defaults.go, internal/defaults/defaults_test.go,
                 internal/cmd/testdata/cli_surface/output/defaults_show_json.txt
       covers:   c-1
       depends:  —
       desc:     Defaults gains `Context ContextDefaults` (toml + json "context,omitempty") holding exactly one
                 field, `Threshold *int64` (toml + json "threshold,omitempty"). EffectiveThreshold(): nil → 150000,
                 0 → 0 (off), <0 → error (D-9). Re-mint defaults_show_json.txt: it gains `"context": {}`, the same
                 way `"telemetry": {}` appears today. risk's README edit moves to t-5.
       contract: no [context] → 150000; `threshold = 0` → 0 with no error. A non-pointer field returns 0 in the
                   first case and fails TestContextThresholdUnsetVsZero
                 round-trip: load `[context] threshold = 0` + `[telemetry] enabled = false`, mutate Remote (what
                   `defaults save` does) and Telemetry (what `stats opt-out` does), SaveFile, reload → still 0. A
                   file with no [context] re-saves with no [context] table (TestContextThresholdSurvivesOtherWriters)
                 `threshold = -1` → EffectiveThreshold error while LoadFile still succeeds;
                   `threshold = "150k"` → LoadFile error naming the path, never a silent 150000
                 reflect: ContextDefaults has exactly one field. A configurable step fails
                   TestContextDefaultsOneKnob (nudge_cadence)
                 TestOutputGoldens: defaults_show.txt stays byte-identical, and defaults_show_json.txt differs only
                   by `"context": {}`. Mismatched toml/json tags fail TestTomlFieldsCarryMatchingJSONTags

  t-3  Read latest main-agent usage from the transcript tail   [risk+verification+mvp]
       files:    internal/ctxnudge/transcript.go (new), internal/ctxnudge/transcript_test.go (new),
                 internal/ctxnudge/testdata/main_tail.jsonl (new), internal/ctxnudge/testdata/subagent_tail.jsonl (new)
       covers:   c-2, c-5, c-7
       depends:  —
       desc:     LatestMainContext(path) (tokens int64, ok bool). Open with O_RDONLY|O_NONBLOCK, require a regular
                 file, then read backwards in 64 KiB chunks up to an 8 MiB cap. Newest first, the first entry
                 that is type "assistant", has isSidechain false, has a usage object, has a model other than
                 "<synthetic>" and has a non-zero sum wins. tokens = input + cache_read_input +
                 cache_creation_input, saturating. Unparseable lines are skipped. The small fixtures are jq
                 projections of {type,isSidechain,message:{model,usage}}, with no content fields (verification;
                 risk wanted synthetic-only). The large and edge-case inputs are generated in-test.
       contract: tail [main assistant 120,000, then sidechain assistant 190,000] → 120,000. Taking the newest
                   assistant line regardless of isSidechain returns 190,000 and fails
                 usage {input 2, cache_read 151,000, cache_creation 407, output 9,999} → 151,409. Counting
                   output_tokens or dropping a cache field fails TestUsageSumsThreeInputFields. A missing
                   cache_creation key counts as 0
                 subagent_tail.jsonl (every line isSidechain:true, the shape of today's subagents/agent-*.jsonl)
                   → ok=false
                 newest main entry is model "<synthetic>" with zero usage → the previous real entry, not 0
                 torn last line (no closing brace, no trailing newline) → skipped; previous entry returned
                 a 3 MiB single tool_result line after the latest assistant entry → still found. A bufio.Scanner
                   at its 64 KiB default fails
                 `{"type": "assistant", …}` (space after the colon) is still recognised
                 50 MB sparse transcript, latest main entry 200 KiB from EOF → a counting ReaderAt records ≤ 1 MiB
                   read. A whole-file read fails TestTailReadIsBounded
                 same file: min of 5 runs < 50 ms, else TestTailRead50MBUnder50ms fails (D-8)
                 no qualifying entry within the 8 MiB cap → ok=false, never (0, true)
                 FIFO with no writer → ok=false within a 1 s deadline; a directory, a missing file or mode 000 →
                   ok=false
                 three usage fields of 4e18 each → a positive saturated sum, not a wrapped negative

  t-4  Nudge cadence: band, once-only claim, line   [risk+verification]
       files:    internal/ctxnudge/nudge.go (new), internal/ctxnudge/nudge_test.go (new),
                 internal/gatestate/nudge.go (new), internal/gatestate/nudge_test.go (new)
       covers:   c-2, c-3
       depends:  —
       desc:     Band(tokens, threshold): 0 when below the threshold or when the threshold is 0, then 1 at the
                 threshold and +1 per unexported fixed step of 50_000. Exported Marker = "context checkpoint:".
                 Line(tokens, threshold, reentry) renders the locked one-liner.
                 gatestate.NudgeClaimed / ClaimNudge(root, sessionID, band): an O_CREATE|O_EXCL file under
                 .dross/gate/nudge/ named sha256(sessionID)[:16]-<band>, created through the existing
                 self-ignoring ensureDir. ClaimNudge returns false when that band or any higher band is already
                 claimed for the session (verification's high-water rule, D-4). A successful claim prunes claims
                 older than 7 days, inside nudge/ only (risk, D-2).
       contract: Band table: (149,999)=0; (150,000)=1 (c-2 "at or above"); (199,999)=1; (200,000)=2;
                   (260,000)=3; threshold 0 at 10,000,000 → 0
                 a 140k→260k jump claims band 3 once: one fire gives one nudge
                 ClaimNudge(s,2) after ClaimNudge(s,3) → false (TestLowerBandNeverRenudges)
                 32 goroutines calling ClaimNudge(root,"s",1) → exactly one true. A stat-then-write claim fails
                   TestClaimNudgeExactlyOnce
                 per-session: (s1,1) true; (s2,1) true; (s1,1) false; (s1,2) true
                 session "../../escape" and "a/b" → the claim lands inside .dross/gate/nudge/; empty session id
                   → error and no file
                 `git status --porcelain` is empty after a claim in a git temp repo (self-ignored)
                 an 8-day-old claim is pruned on the next claim. A 1-day-old claim survives, including another
                   session's (a concurrent session keeps its markers). An 8-day-old .dross/gate/green.json is
                   untouched
                 Line(151_409, 150_000, "/dross-execute — run the next task") starts with Marker and contains
                   "151k / 150k", "checkpoint at your next durable boundary → /clear", the reentry verbatim and
                   "mid-thought? /dross-pause first". A reentry containing "\n" still yields one line
                   (TestLineCarriesLockedParts, nudge_content)

  t-5  Document the nudge hook and threshold key   [verification+risk]
       files:    README.md, docs/dross.1, assets/prompts/options.md, internal/cmd/readme_doc_test.go
       covers:   c-1, c-6
       depends:  —
       desc:     README gains a `dross hooks {ensure,nudge}` row, saying what ensure wires and that nudge is a
                 hook-only verb. Its defaults.toml line names `[context] threshold` (default 150000, 0 = off);
                 that line is risk's, moved here from its t-1. The man page gets the same two entries.
                 options.md §8 (Global defaults) names the key (D-11).
       contract: if README loses "dross hooks {ensure,nudge}", "[context]" or "threshold", or dross.1 loses
                   `dross hooks` or the threshold key, TestReadmeDocumentsContextNudge fails
                 if options.md §8 stops naming `[context]` + `threshold`, TestOptionsNamesContextThreshold fails,
                   and the knob exists only in source

Wave 2 (t-6 depends t-4; t-7 depends t-3, t-4)
  t-6  Add the context-checkpoint rule to the interaction playbook   [risk+verification+mvp]
       files:    assets/prompts/_interaction.md, internal/cmd/interaction_snippet_test.go,
                 internal/cmd/execute_prompt_test.go
       covers:   c-4, c-8, c-9
       depends:  t-4
       desc:     One short "## Context checkpoint" section. A line starting with Marker in context means the next
                 durable boundary leads with "checkpoint + /clear". That covers execute §1g even mid-wave, where
                 the rule overrides §1g's own mid-wave `continue` lead, and every "safe to /clear" wrap-up.
                 Mid-command gates before their artifact is written are not durable boundaries and don't change.
                 Under --solo the nudge is informational: keep going. If the thread isn't on disk, run
                 /dross-pause first. Never run a dross command to check context. execute.md is not edited
                 (gate_scope).
       contract: TestInteractionSnippetHasContextNudgeRule: _interaction.md contains ctxnudge.Marker verbatim,
                   so renaming the Go marker without the prompt fails. It also needs "checkpoint + /clear", "next
                   durable boundary", "even mid-wave", "§1g", "overrides", "safe to /clear", "--solo",
                   "informational", "before its artifact is written" and "/dross-pause"
                 `dross interaction show` output (the embed) contains Marker; a stale embed fails
                 Marker appears in no other assets/prompts/*.md or assets/commands/*.md
                   (TestNudgeRuleLivesOnlyInPlaybook; gate_scope: one rule)
                 TestFooterPromptsLoadPlaybook (new): every prompt footerCoverage marks footer-bearing (today
                   debug, execute, pause, plan, quick, respond, ship, spec, verify) still runs
                   `dross interaction show`. A wrap-up the rule cannot reach fails
                 no assets/prompts/*.md or assets/commands/*.md contains "dross hooks nudge" or a `dross context`
                   call, and the new section names no `dross <verb>` (TestContextRuleAddsNoCalls; signal_path, c-8)
                 executeGateSection (lowercased, backticks stripped) still contains "mid-wave, lead with continue"
                   and "lead with checkpoint", and the checkpoint bullet names exactly `dross validate` and
                   `dross task next`. Otherwise TestExecutePromptCheckpointWaveLead (extended) fails: the
                   below-threshold order must not change (c-4) and no call may join the flow (c-8)
                 new section ≤ 700 bytes (TestContextRuleSizeBudget, D-13)

  t-7  Nudge pipeline over the hook payload   [risk+verification]
       files:    internal/ctxnudge/run.go (new), internal/ctxnudge/run_test.go (new),
                 internal/ctxnudge/testdata/posttooluse_main.json (new),
                 internal/ctxnudge/testdata/posttooluse_subagent.json (new)
       covers:   c-2, c-3, c-5, c-8
       depends:  t-3, t-4
       desc:     FIRST capture two real PostToolUse payloads in a scratch dross repo with a temporary stdin-dump
                 hook: one from a main-agent call and one from a call inside a subagent. Use harmless content and
                 check the captures structurally for secrets. The capture decides whether agent_id marks subagent
                 origin. If it doesn't, fall back to "tool_use_id is not among the latest main-agent message's
                 tool_use blocks" (D-1).
                 Run(payload, Env{Threshold, Reentry}) []byte uses a narrow decode (hook_event_name, session_id,
                 transcript_path, cwd, agent_id, tool_use_id), not gate.Decode. It stays silent unless all hold:
                 event == PostToolUse, main-agent origin, gate.LocateRoot(cwd) finds a repo (it only stats and
                 never materialises state.json), threshold > 0, the transcript is ok, and the band is > 0 and
                 unclaimed. It then composes the line via Env.Reentry BEFORE claiming (D-10), claims, and emits
                 {"systemMessage": line, "hookSpecificOutput": {"hookEventName": "PostToolUse",
                 "additionalContext": line}}.
       contract: captured subagent fixture + over-threshold transcript → nil output AND no claim file
                   (TestSubagentFireNeitherNudgesNorClaims)
                 transcript_path pointing at an all-sidechain subagents/agent-*.jsonl → nil
                 captured main fixture over threshold → exactly one JSON object: systemMessage == additionalContext
                   == the line, hookEventName == "PostToolUse", and no "decision", "continue" or
                   "permissionDecision" key (TestEnvelopeNeverBlocks; a block forces an extra turn, which c-8
                   forbids). A second Run on the same session and band → nil
                 the transcript read is payload.transcript_path, never derived from session_id; the fixture sits
                   at an arbitrary temp path (TestReadsPayloadTranscriptPath)
                 nil output for each of: non-JSON stdin; a JSON array; cwd outside a dross repo; cwd in a .dross
                   with no project.toml; empty session_id; missing transcript; an Env.Threshold error;
                   hook_event_name "UserPromptSubmit" or "SessionStart"; threshold 0 with transcript_path at a
                   FIFO (returns at once, which proves the off switch never opens the transcript)
                 the Env.Reentry call counter stays 0 below threshold and when the band is already claimed.
                   suggestNext runs git, so it stays off the hot path
                 Env.Reentry panics → no claim is written, and the next Run with a working Reentry still nudges
                   that band (TestFailedComposeKeepsBucket)

Wave 3 (depends t-2, t-7)
  t-8  Add the `dross hooks nudge` hook verb   [risk+mvp+verification]
       files:    internal/cmd/hooks_nudge.go (new), internal/cmd/hooks_nudge_test.go (new), internal/cmd/hooks.go,
                 internal/cmd/telemetry.go, cmd/dross/testdata/cli_tree.txt
       covers:   c-1, c-3, c-5, c-7
       depends:  t-2, t-7
       desc:     const NudgeHook = "dross hooks nudge". The verb reads stdin capped at maxGatePayload and calls
                 ctxnudge.Run through a swappable seam with Env{Threshold: defaults EffectiveThreshold, Reentry:
                 suggestNext over project+state, the generator reentryLine uses}. It writes Run's bytes verbatim,
                 recovers panics, always returns nil, and sets SilenceUsage/SilenceErrors. Registered under
                 Hooks(). RecordCLIEvent skips it alongside the gate verbs. The verb must not reference ErrNoRoot
                 or (gate.)LocateRoot, because TestRootHelperCallersAreAllowlisted pins both caller sets in
                 internal/cmd; root resolution stays in ctxnudge. Re-mint cli_tree.txt. The task touches 5 files,
                 but 3 are one-line edits, and splitting them out would leave sub-10-minute tasks.
       contract: exit 0 with empty stdout AND stderr for: closed/empty stdin, garbage stdin, an undecodable
                   defaults.toml, a seam that panics, and a cwd with an incomplete .dross. A returned error makes
                   main print "dross: …" and exit 1, which shows as a hook error on every tool call
                 RecordCLIEvent(`dross hooks nudge`) writes 0 telemetry events, while `dross status` still writes 1
                 fixture repo with a planned phase and a runnable task, over-threshold payload → the line's
                   re-entry segment equals the "next: …" segment `dross reentry` prints ("/dross-execute — run the
                   next task"). A hand-rolled string fails TestNudgeNamesSessionStartReentry
                 one session, end to end: 150,000 emits one envelope, 160,000 is silent, 200,000 emits again
                   (TestNudgeCadenceEndToEnd)
                 threshold = 0 at 900k → empty stdout and nothing created under .dross/gate (c-1)
                 root.Find("hooks","nudge").CommandPath() == NudgeHook
                 cli_tree.txt gains exactly the `dross hooks nudge` line; TestCLITreeGolden fails on Use/Short drift

Wave 4 (depends t-8)
  t-9  Wire the nudge into `dross hooks ensure`   [risk+mvp+verification]
       files:    internal/cmd/hooks.go, internal/cmd/hooks_test.go,
                 internal/cmd/testdata/cli_surface/doctor_run.txt, cmd/dross/testdata/cli_tree.txt
       covers:   c-6
       depends:  t-8
       desc:     userHooks gains {PostToolUse, NudgeHook}, matcher-less, after the gate record. `hooks ensure`
                 prints userHooksSummary() instead of its hand-typed five-hook format string, and its Short names
                 the nudge. Re-mint cli_tree.txt's `dross hooks ensure` line; no draft listed it for this edit.
                 Re-mint doctor_run.txt: doctor iterates over userHooks, so its Hooks section gains
                 "✓ PostToolUse → dross hooks nudge". risk missed this golden.
       contract: today's five-hook settings.json + a foreign PostToolUse group + ensure → PostToolUse is
                   [foreign, gate record, hooks nudge] in that order, and a second ensure is byte-identical.
                   TestEnsureUserHooksUpgradesTwoHookInstall's PostToolUse count moves 1 → 2 deliberately
                 TestUserHooksResolve: every userHooks command resolves, through a root holding Pause, Reentry,
                   Gate and Hooks, to a command whose CommandPath equals the wired string. A typo'd hook command
                   (which would error on every tool call) fails
                 TestHookWordingNamesAllFour (now six entries): userHooks, ensure's Short, ensure's changed-file
                   output and init/onboard's ensured-hooks line all name PostToolUse → dross hooks nudge
                 TestDoctorRunGolden fails if the Hooks section lacks "✓ PostToolUse → dross hooks nudge" after
                   init. TestCLITreeGolden fails if ensure's Short drifts from the golden

Wave 5 (t-10 depends t-2, t-9; t-11 depends t-1, t-6, t-9)
  t-10 Doctor flags a missing nudge hook and a silently-off threshold   [verification+risk]
       files:    internal/cmd/doctor.go, internal/cmd/doctor_hooks_test.go,
                 internal/cmd/testdata/cli_surface/doctor_run.txt
       covers:   c-1, c-6
       depends:  t-2, t-9
       desc:     A missing nudge stays ⚠, not ✗; hooksSection already grades non-gate hooks ⚠ (mvp's finding).
                 The warning text says context-threshold nudges are off and names `dross hooks ensure`. A new
                 context line in the Hooks section shows the effective threshold ("150k (default)", "off
                 (threshold 0)"). When defaults.toml won't decode or holds a negative value, the line is ⚠ "nudge
                 is silently off" and names defaults.toml (D-9). The test helper pairs gain nudgePostPair, and the
                 "all wired" fixtures include it. Re-mint doctor_run.txt for the context line.
       contract: settings with every hook except the nudge → doctor exits 0 and the Hooks section shows
                   "⚠ PostToolUse → `dross hooks nudge` is not wired", "nudges are off" and "dross hooks ensure".
                   A ✗ or a non-zero exit fails the new "no nudge" row in TestDoctorMissingConvenienceHooksWarnOnly
                 every hook wired → only ✓ lines (TestDoctorGateHooks, extended after-ensure list)
                 `[context] threshold = "150k"` → ⚠ naming defaults.toml and "off"; `threshold = -5` → ⚠;
                   `threshold = 0` → ✓ "off (threshold 0)"; absent → "150k (default)". TestDoctorContextThreshold
                   fails on any row
                 TestDoctorRunGolden fails if the context line drifts

  t-11 Measure the after-flow and hook latency; guard the record   [risk+verification]
       files:    .dross/phases/context-boundaries/flow-cost.toml, internal/cmd/flowcost_record_test.go (new)
       covers:   c-3, c-4, c-7, c-8
       depends:  t-1, t-6, t-9
       desc:     Run `make install` (r-01), then confirm `dross version` matches HEAD. Lower `[context] threshold`
                 so the nudge fires before a mid-wave gate, replay t-1's [scenario] exactly, then restore
                 defaults. Record [after]: agent_turns, wall_clock_s, binary_version, session_id, nudge_fired,
                 nudges_seen, mid_wave, gate_lead. Record a below-threshold [control] (threshold 0, where the
                 mid-wave lead stays continue). Record [hook_latency]: 50 fires of the installed `dross hooks nudge`
                 against a 50 MB synthetic transcript plus a 10 MiB tool_response payload, with p50/p95 per path
                 (below / claimed / emit). Record observed numbers only.
       contract: TestFlowCostRecords: every .dross/phases/*/flow-cost.toml parses and has [scenario], [before]
                   and [after]. [before] and [after] each need agent_turns > 0, wall_clock_s > 0 and a non-empty
                   binary_version and session_id. after.agent_turns ≤ before.agent_turns, so an added round-trip
                   fails. context-boundaries' record must exist (no vacuous pass)
                 after.binary_version ≠ before.binary_version. An after-leg run on a stale installed binary fails
                 after.nudge_fired, after.mid_wave, after.gate_lead == "checkpoint", after.nudges_seen == 1 and
                   control.gate_lead == "continue". An after-leg where the nudge never fired or repeated, or where
                   §1g still led with continue, fails (observed c-3/c-4)
                 [hook_latency] fires ≥ 50, transcript_mb ≥ 50 and every path's p95_ms ≤ 50. A recorded p95 over
                   budget fails
```

Coverage: c-1 → t-2, t-5, t-8, t-10 · c-2 → t-3, t-4, t-7 · c-3 → t-4, t-7, t-8, t-11 · c-4 → t-6, t-11 · c-5 → t-3, t-7, t-8 · c-6 → t-5, t-9, t-10 · c-7 → t-3, t-8, t-11 · c-8 → t-1, t-6, t-7, t-11 · c-9 → t-6. All 9 criteria are covered. No two tasks in the same wave share a file.

## Disagreements

**D-1 — How a subagent-origin fire is recognised.**
- risk: capture real PostToolUse payloads first and let the capture decide whether agent_id marks origin, with tool_use_id membership as the fallback. mvp and verification: assume any payload carrying agent_id is a subagent fire.
- Default: risk's capture-first (t-7).
- Why it matters: the repo documents agent_id only as a SubagentStop field (`gate/payload.go`). If the assumption is wrong, the nudge lands in the subagent's context and burns the band, so the main agent never sees it (c-2, c-3).

**D-2 — Cadence state store and pruning.**
- risk: O_EXCL claim files in .dross/gate/nudge/, pruned after 7 days. verification: O_EXCL markers in .dross/gate/, where every successful claim deletes other sessions' markers. mvp: one read-modify-write file per session under ~/.claude/dross/nudge/, never pruned.
- Default: risk's store and age prune (t-4).
- Why it matters: parallel PostToolUse fires race a read-modify-write file into a double nudge. Pruning other sessions' markers makes two sessions in one repo re-nudge each other on alternate claims, which breaks c-3. mvp's git-spawn objection applies to gatestate.load, which O_EXCL claims never call.

**D-3 — Which hook events are wired.**
- risk and mvp: PostToolUse only. verification: PostToolUse plus UserPromptSubmit, which adds a new const, a second userHooks entry and an extra doctor row.
- Default: PostToolUse only.
- Why it matters: verification argues that c-5's "never blocks a prompt" is untestable without a prompt event, and that a turn with no tool call misses the crossing. risk and mvp reply that every durable boundary is reached through a tool call, and that UserPromptSubmit is the one event where a stray exit erases the user's prompt.

**D-4 — Re-nudging after a compaction.**
- mvp: store the last-seen bucket, so a compaction that drops usage below the threshold and then climbs back re-nudges at 150k. risk: never reset, but claim only the current band, so a 140k→260k jump leaves bands 1–2 open to fire later. verification: never reset, and refuse any band at or below the highest one claimed.
- Default: no reset, with verification's high-water rule (t-4).
- Why it matters: nudge_cadence says "first crossing … in the same session", and a reset needs a state read on every below-threshold fire (c-7). The cost: after a compaction, a session can sit past the threshold with no nudge until it passes its old high-water mark plus 50k.

**D-5 — Where the hook pipeline lives.**
- risk: ctxnudge.Run owns the whole sequence (decode → root → threshold → transcript → band → claim → envelope), and the cmd verb is a thin stdin/stdout shell. mvp and verification: the orchestration sits in the cmd verb's RunE, which calls cmd.FindRoot.
- Default: risk's (t-7, t-8).
- Why it matters: internal/cmd is the slow package that stalls under memory pressure, so most behavioural tests move to a fast one. FindRoot uses the process cwd and can write state.json on the hook path, whereas gate.LocateRoot(payload cwd) only stats. A cmd-side root call must also clear TestRootHelperCallersAreAllowlisted.

**D-6 — Where the c-8 "before" leg comes from.**
- verification: a wave-1 task records the baseline on the installed pre-phase binary before anything changes. risk: run both legs at the end, with "before" on a rebuilt pre-phase binary. mvp: take "before" from an existing pre-phase checkpoint transcript.
- Default: verification's wave-1 baseline (t-1), replayed exactly by t-11.
- Why it matters: by the end of the phase, `make install` has replaced the old binary, so risk's before-leg needs a rebuild from main. mvp's historical transcript may not match the after-leg's scenario, so the turn comparison would compare different flows. Constraint: t-1 must run before any `make install` in wave 1.

**D-7 — How the c-3/c-4/c-8 evidence is checked.**
- risk: TestFlowCostRecords parses every phase's flow-cost.toml and fails on extra turns, equal binary versions, or a nudge that never fired. verification: a measurements.toml judged at verify time, plus a below-threshold control run. mvp: measurements.md with no observed §1g lead.
- Default: risk's TOML record and guard test, plus verification's [scenario], [control] and nudges_seen (t-11).
- Why it matters: a stale-binary or vacuous after-leg then fails in CI instead of depending on the verifier noticing, and the later v1.8 phases owe the same record. The cost is one more test in internal/cmd.

**D-8 — Where the c-7 wall-clock check runs.**
- mvp: assert RunE < 50 ms inside internal/cmd. verification: a min-of-5 timing in the ctxnudge package plus a recorded p95. risk: no timing in the suite, only a byte bound plus a p95 measured on the installed binary.
- Default: byte bound, verification's package-level min-of-5, and a measured p95 (t-3, t-11). No timing assert in internal/cmd.
- Why it matters: a timing assert in the stall-prone cmd package flakes. risk's objection was to that package, not to a fast one. If the min-of-5 flakes on CI, fall back to risk's form.

**D-9 — How an invalid threshold is surfaced.**
- risk: a negative or undecodable threshold keeps the hook silent, EffectiveThreshold does the negative check, and doctor shows "silently off". verification: LoadFile rejects negative values, and `dross defaults show` is the only surface. mvp: no doctor change at all.
- Default: risk's (t-2, t-10).
- Why it matters: rejecting at LoadFile lets one bad key break every defaults reader (telemetry, init/onboard pre-fill, `stats opt-out`). With no doctor line, a typo disables the feature without any warning.

**D-10 — Compose the line before or after claiming the band.**
- risk: compose (suggestNext) before claiming, so a failed compose never burns a band. mvp and verification: claim (or mark Due) first, then compose.
- Default: risk's (t-7, TestFailedComposeKeepsBucket).
- Why it matters: with claim-first, a suggestNext panic loses that band's nudge for the whole session. With compose-first, suggestNext (which runs git) can run twice under a race, and only over the threshold.

**D-11 — Whether docs get their own task.**
- verification: a separate task for README, dross.1 and options.md §8, with doc tests. risk: one README line inside the defaults task. mvp: drops options.md and docs as not traceable to any criterion.
- Default: include verification's task (t-5), absorbing risk's README line.
- Why it matters: /dross-options claims to reach every dross-managed setting, so without §8 the knob exists only in source. Include-first is the standing preference. The cost is one wave-1 task with no code dependency.

**D-12 — Task granularity.**
- mvp: one core task (defaults, transcript, cadence, line and payload parsing), with wiring and doctor merged. risk: the core split into three tasks, with wiring and doctor merged. verification: the core split into four, with wiring and doctor separate.
- Default: the core split into t-2/t-3/t-4, and wiring separate from doctor (t-9, t-10).
- Why it matters: mvp's t-1 spans two packages and four concerns. risk's merged t-7 reaches 6 files once doctor_run.txt and cli_tree.txt are added. The split costs one more wave.

**D-13 — Playbook size budget.**
- risk: the whole _interaction.md must stay ≤ 5,300 bytes (it is 4,482 today). verification: the new section must stay ≤ 700 bytes. mvp: no budget.
- Default: verification's section cap (t-6).
- Why it matters: a whole-file cap set in this phase turns red when the playbook grows for unrelated reasons, and later v1.8 phases such as prompt-partials are likely to edit it. A section cap guards only what this phase adds.
