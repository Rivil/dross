# solo-task-subagent — verification lens

Lens: each criterion's ideal test contract came first; each task is the smallest
change that makes those contracts satisfiable. Enforcement sits in gates, which
can be tested, rather than in prompt prose, which cannot. Prompt prose only routes the agent to the
gated verbs.

Baseline probe done while planning: per-task numbers mined from 39 v1.7 baseline
solo tasks (solo-task-review, board-finalize-on-complete, verify-staleness,
debug-sessions, review-comment-ingest; main-loop entries only; task span =
`task status … in_progress` → `done|failed`). Median per-task main-loop context
growth by the existing bytes of `task.files`: <30 kB → 15k tokens, 30–50 kB → 20k,
50–70 kB → 30k, 70–100 kB → 49k, ≥100 kB → 23k. Wall-clock is flat at 9–12 min
across the buckets. The provisional size gate is **50,000 bytes of existing
task.files**: about 24 of 39 baseline tasks fall above it. t-12 re-derives this
number from the committed record and pins it. Caveat: I measured at today's HEAD.
t-12 must measure each file at the task commit's parent.

```
Phase solo-task-subagent — 15 tasks across 5 waves

Wave 1
  t-1  Capture writer-agent hook payloads
       files:    internal/gate/testdata/writer_pre_bash.json (new), internal/gate/testdata/writer_pre_edit.json (new),
                 internal/gate/testdata/writer_subagent_stop.json (new), internal/gate/testdata/main_pre_edit.json (new),
                 internal/gate/fixture_test.go
       covers:   c-1, c-5
       desc:     Go/no-go capture in a scratch repo, using the same method as solo-task-review t-1 (headless claude -p, a scratch
                 custom agent named dross-task-writer, a temporary stdin-dump hook in the scratch repo's settings.local.json, never ~/.claude).
                 Capture a PreToolUse Bash and a PreToolUse Edit made inside the writer, the writer's SubagentStop, and a main-loop
                 PreToolUse Edit. STOP before t-9, t-10 and t-11 if the PreToolUse payloads inside the agent lack agent_type.
       contract: - if writer_pre_bash.json / writer_pre_edit.json stop being real captures (session_id, transcript_path, toolu_ id) whose
                   hook_event_name is PreToolUse with agent_type "dross-task-writer" and a non-empty agent_id, TestCapturedWriterPreToolUse fails
                 - if main_pre_edit.json carries any agent_type or agent_id key, TestCapturedMainLoopHasNoAgentType fails (the
                   takeover gate's main-loop discriminator would be unsound)
                 - if writer_subagent_stop.json's last_assistant_message holds no dross-writer-report fence, TestCapturedWriterStop fails
                 - grep of every non-trivial ~/.claude/CLAUDE.md line across the four fixtures must count 0 (checked before commit)

  t-2  Parse the writer report and state
       files:    internal/writer/writer.go (new), internal/writer/report.go (new), internal/writer/report_test.go (new),
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-2, c-5
       desc:     Agent = "dross-task-writer"; Enabled (kill switch, const). ParseReport reads exactly one ```dross-writer-report JSON fence
                 {files[], test green|red, notes}. Caps: ≤50 files, notes ≤600 chars, the reply outside the fence ≤200 chars.
                 State: none|briefed|launched|green|red|unavailable, with Outcome(report|empty|parse-error).
       contract: - parse table: zero fences, two fences, malformed JSON, test "not-run" or "", 51 files, 601-char notes, notes holding a
                   line starting "diff --git", "@@ " or "+++ ", or any ``` fence inside notes → each errors naming the reason; a valid
                   report round-trips — TestParseWriterReport (c-2 "bounded … never the diff")
                 - if a reply that is valid JSON but carries >200 chars of prose outside the fence parses, TestReportReplyBounded fails
                 - Outcome table: report test=green → green; test=red → red; "" last message → unavailable "returned nothing";
                   parse error → unavailable naming it — any drift fails TestWriterOutcome (c-5)
                 - an unfiled json-tagged string field in internal/writer fails TestEveryPathShapedFieldIsDeclared

  t-3  Mine per-task cost and compare thresholds
       files:    internal/writercost/mine.go (new), internal/writercost/mine_test.go (new), internal/writercost/compare.go (new),
                 internal/writercost/compare_test.go (new), internal/writercost/testdata/solo_run.jsonl (new), internal/ctxnudge/transcript.go
       covers:   c-6
       desc:     Mine(transcript) → []TaskRow{phase, task, ctx_start, peak, growth, wall_s, turns}. A span is the main-loop Bash
                 `dross task status <p> <t> in_progress` → done|failed. The per-line token count comes from ctxnudge (mainContext exported as
                 MainContext). Compare(before, after, threshold) filters both sides to rows with surface ≥ threshold. It takes median growth
                 and median wall_s, passes iff growth drops ≥25% AND wall rises ≤25%, and reports turns without gating on them.
       contract: - fixture with two task spans, a sidechain entry of 900k tokens inside span 1, a <synthetic> entry and a span with no
                   end → exactly two rows (unended dropped) with hand-computed peak/growth/wall/turns; if the sidechain moves span 1's
                   peak, TestMineExcludesSidechain fails (the writer's own tokens must never count as main-loop context)
                 - if a task re-run in a second transcript yields two rows instead of the longest span, TestMineDedupesTask fails
                 - Compare table: drop 25.0% + rise 25.0% → pass; drop 24.9% → fail naming context; rise 25.1% → fail naming wall;
                   turns tripled with both held → pass; no eligible after rows → error, never pass — TestCompareThresholds
                 - if TestLatestMainContext* (ctxnudge) changes result after the export, the nudge regresses — existing tests guard it

  t-4  Amend the dross-agent-gate builtin
       files:    internal/rules/rules.go, internal/rules/rules_test.go, docs/subagent-offload-audit.md
       covers:   c-1
       desc:     Builtin text gains the solo-only exception: under --solo, one dross-task-writer subagent per task may edit that task's
                 files; it never commits, sets task status or finalizes. The read-only fan-out and pair clauses stay. The audit doc's
                 boundary paragraph and its execute section name the exception.
       contract: - if the rendered rule loses "writing and deciding stay gated", "only `--solo`", or gains no clause naming
                   dross-task-writer with "one" "per task" and "never commits, sets task status or finalizes", TestRenderEmitsAgentGateBuiltin fails
                 - if the writer clause stops being scoped to --solo (the text must say pair mode never spawns one), TestAgentGateWriterSoloOnly fails
                 - TestSubagentOffloadAuditCoversEveryPrompt keeps passing (the section headers are unchanged)

  t-5  Build the writer brief and size gate
       files:    internal/writer/brief.go (new), internal/writer/brief_test.go (new), internal/writer/size.go (new),
                 internal/writer/size_test.go (new)
       covers:   c-2
       desc:     Surface(files, sizeOf) sums the bytes of each existing task.files entry (a missing file counts 0). Threshold = 50000
                 (provisional; t-12 pins it). BuildBrief(review.Scope) renders the task record (no status), the covered criteria, the
                 locked decisions, the hard rules and the report contract, with a "dross-writer-brief sha256:<digest>" header line. It
                 renders no diff. PromptLine(path, digest) is the one line the writer is spawned with.
       contract: - Surface 49,999 → below, 50,000 → above, all-missing → 0 below; if the sum reads a file's content instead of its size,
                   or the order of files changes it, TestSurfaceDeterministic fails
                 - if an uncovered criterion, the task's status or an unlocked decision appears, or a locked decision or hard rule is
                   missing, TestBriefScope fails; flipping status pending→in_progress must not change the digest
                 - if two builds of the same scope differ in digest, or a one-byte change to the description leaves it equal, TestBriefDigest fails
                 - if PromptLine stops being a single line that contains the path and digest, TestPromptLineShape fails
                 - if internal/writer imports internal/gate (cycle risk), TestWriterImportDirection fails

Wave 2 (t-6, t-7 depend t-2; t-8 depends t-3, t-5; t-9 depends t-1, t-2)
  t-6  Store the writer record behind tamper-guard
       files:    internal/gatestate/store.go, internal/gatestate/store_test.go, internal/gate/override.go, internal/gate/override_test.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-1, c-5
       desc:     New .dross/gate/writer.json {phase, task, attempt, digest, state, agent_id, report, cause} and writer-brief.md use the
                 atomic temp+rename save. A missing file reads as none, a corrupt one is an error naming the path, and a git-tracked one
                 is refused unread. Tamper-guard covers both files.
       contract: - if a truncated writer.json decodes to a zero record instead of an error naming .dross/gate/writer.json, or a missing
                   one returns other than (nil, nil), TestWriterRecordDecode fails
                 - if Write/Edit of writer.json or writer-brief.md, or Bash `echo x > .dross/gate/writer.json`, `rm .dross/gate/writer.json`,
                   `mv .dross/gate/writer.json /tmp/x` gets past tamper-guard, the tamper table fails (a deleted record would
                   re-allow a second writer and lift the takeover refusal)
                 - if a force-added writer.json is read instead of refused, TestTrackedRecordRefused fails
                 - unfiled tagged fields fail TestEveryPathShapedFieldIsDeclared

  t-7  Ship the writer agent definition
       files:    assets/agents/dross-task-writer.md (new), internal/writer/agentdef_test.go (new)
       covers:   c-1, c-2
       desc:     Frontmatter: name dross-task-writer, tools exactly Read, Grep, Glob, Edit, Write, Bash, no model key (it inherits the
                 session model), no memory. Body: read the named brief and reply "stale brief" if the digest differs; edit only
                 task.files; run `dross test --files …`; never commit, stash, set status or touch .dross/; the final reply is the report
                 fence and nothing more.
       contract: - if `tools` gains Agent, Task, WebFetch, WebSearch or NotebookEdit, or is dropped (inherit-all), or a `model` key other
                   than inherit appears, TestWriterDefinitionShape fails (writer_spawn: inherits the session model)
                 - if the frontmatter name drifts from writer.Agent, TestWriterNameMatchesConst fails
                 - if the body's worked final reply stops parsing with writer.ParseReport, TestWriterBodyMatchesParser fails
                 - if the body stops forbidding commit, stash, `dross task status`, `dross changes record` and edits under .dross/,
                   TestWriterBodyForbidsGates fails
                 - TestEmbedDrift keeps passing (embedded agents set == disk)

  t-8  Add the dross writer cost verb
       files:    internal/cmd/writer.go (new), internal/cmd/writer_cost_test.go (new), cmd/dross/main.go, cmd/dross/testdata/cli_tree.txt
       covers:   c-6
       desc:     `dross writer` group; `dross writer cost --leg before|after --phase <p>… --transcript <path>…` mines rows. It sizes each
                 row's surface at the task commit's parent (from changes.json), writes or replaces only that leg in a phase-dir
                 writer-cost.toml, and prints the Compare verdict once both legs exist. It prints numbers only, never transcript text.
       contract: - with the t-3 fixture transcript and a fixture repo whose changes.json names each task's commit, the written [before]
                   rows equal the hand-computed ones and surface is the size at <commit>^, not HEAD — TestWriterCostWritesLeg
                 - if writing [after] changes a byte of an existing [before], TestWriterCostLegIsolation fails
                 - if stdout or the toml carries any transcript text (a canary string in the fixture's message content), TestWriterCostNoContent fails
                 - a missing `writer cost` row fails TestCLITreeGolden

  t-9  Confine the writer's own tool calls
       files:    internal/gate/writerconfine.go (new), internal/gate/writerconfine_test.go (new)
       covers:   c-1, c-3
       desc:     PreToolUse gate on calls whose agent_type is dross-task-writer. Bash may run only an allowlist: git status|diff|log|show
                 and dross test|validate|rule show|task show|stack loadout|trust --check, plus non-git, non-dross commands. Edit and Write
                 are refused under .dross/ and outside the armed task's task.files. Agent and Task spawns are refused.
       contract: - from t-1's writer_pre_bash.json with the command swapped: `git commit -m x`, `git stash`, `git reset --hard`, `git push`,
                   `dross task status p t-3 done`, `dross changes record …`, `dross phase complete p`, `dross ship`, `dross execute begin p`,
                   `dross review context` → each refused naming "the writer never commits, sets task status or finalizes"; `dross test
                   --files a.go`, `git diff`, `go vet ./...` → admitted — TestWriterBashAllowlist (c-1)
                 - from writer_pre_edit.json: Edit of .dross/phases/p/plan.toml → refused; Edit of a task.files path → admitted; Edit of a
                   file outside task.files → refused naming the plan deviation — TestWriterEditScope
                 - if the same commands with no agent_type (the main loop, main_pre_edit.json shape) are refused by this gate,
                   TestWriterConfineIgnoresMainLoop fails
                 - if a bare `dross test` (no --files) from the writer is admitted, TestWriterNoFullSuite fails (c-3: only the main loop's bare run certifies)

Wave 3
  t-10 Gate writer spawns and record outcomes   (depends t-1, t-2, t-5, t-6)
       files:    internal/gate/writerspawn.go (new), internal/gate/writerspawn_test.go (new), internal/gate/scope.go
       covers:   c-1, c-2, c-3, c-4, c-5
       desc:     PreToolUse on Agent/Task with subagent_type dross-task-writer. It refuses unless ArmedScope is a solo execute task,
                 writer.Enabled holds, Surface ≥ Threshold, the record is briefed for this attempt, the prompt equals PromptLine for the
                 brief it regenerates (a whitespace-trimmed match), there is no `model` input, and run_in_background is not false.
                 Recorder: the launch PostToolUse → launched{agent_id}. The matching SubagentStop → green|red|unavailable from
                 ParseReport, plus ClearGreen. scope.go gains WriterBriefFor(root, home, scope), which the verb shares.
       contract: - spawn refused with execute.json mode pair, execute.json absent, a solo quick armed (quick_solo), no task in_progress,
                   HEAD off phase/<id> — each naming why; admitted only for a briefed solo task — TestWriterSpawnSoloOnly (c-4)
                 - a second spawn after a launched/green/red record for the same attempt → refused "one writer per task" — TestOneWriterPerTask (c-1)
                 - prompt = the line plus any other text, or a digest from a brief built before a plan edit → refused;
                   `model: "haiku"` or run_in_background false → refused — TestWriterSpawnPromptExact (c-2, writer_spawn)
                 - a task below Threshold, or writer.Enabled false (seam), → refused naming "inline" — TestWriterSpawnSizeGated (dispatch, c-6 switch)
                 - from t-1's writer_subagent_stop.json: report test=green → state green with files/test/notes stored; test=red → red;
                   empty last message → unavailable "returned nothing"; unparseable → unavailable naming the parse error;
                   a stop whose agent_id has no pending launch → no write — TestWriterRecorderOutcomes (c-5)
                 - if green.json written before the writer's SubagentStop survives that stop, TestWriterStopClearsGreen fails — the
                   follow-on case: commit-green then refuses a commit until a main-loop bare `dross test` (c-3)
                 - with execute.json pair, every writer launch/stop payload records nothing — TestWriterRecorderPairSilent (c-4)

  t-11 Refuse inline takeover of a writer task   (depends t-1, t-6)
       files:    internal/gate/writertakeover.go (new), internal/gate/writertakeover_test.go (new), internal/cmd/review.go,
                 internal/cmd/review_test.go
       covers:   c-3, c-5
       desc:     While writer.json for the armed attempt is briefed, launched, red or unavailable, it refuses main-loop (no agent_type)
                 Edit, Write, MultiEdit and NotebookEdit outside .dross/, and `git commit` with code staged. The remedy names the stash
                 line and `failed --reason`. Green, or no record, stays silent so the reviewer's fix round works. `dross review
                 context` refuses while the writer is launched.
       contract: - from main_pre_edit.json with state red: Edit a.go → refused naming `git stash push -u -- . ':(exclude).dross'` and
                   `dross task status <p> <t> failed --reason`; Edit .dross/x → admitted; `git stash push -u …` and `dross task status p t
                   failed --reason x` → admitted — TestTakeoverRefusedAfterRed (c-5)
                 - state table briefed|launched|red|unavailable → refuse; green|none|another task's record → silent — TestTakeoverStates
                 - a commit with code staged under state unavailable is refused even when green.json and a review pass match the tree,
                   TestTakeoverBlocksCommit (the main loop cannot inline-fix, test, review and commit around a dead writer)
                 - pair mode or a solo quick with any writer.json → silent — TestTakeoverPairSilent (c-4)
                 - `dross review context` with state launched → error naming "wait for the writer's completion notice", writes no
                   context file — TestReviewContextWaitsForWriter (c-3)

  t-12 Record the baseline and pin the switch   (depends t-5, t-8)
       files:    .dross/phases/solo-task-subagent/writer-cost.toml (new), internal/cmd/writercost_record_test.go (new), internal/writer/size.go
       covers:   c-6, c-2
       desc:     Run `dross writer cost --leg before` over the v1.7 solo transcripts for the five baseline phases. Record [calibration]
                 {threshold, method}, set writer.Threshold to it, and leave [after] empty with [trial] status = "pending".
       contract: - if [before] lacks rows from each of the five baseline phases, or any row misses peak, growth, wall_s, turns or surface,
                   TestWriterCostRecord fails
                 - if writer.Threshold differs from [calibration].threshold, or the median growth of [before] rows at or above it is not
                   greater than that of the rows below, TestThresholdCalibrated fails (dispatch: "calibrates from baseline data")
                 - kill switch: once [after] exists, writer.Enabled must equal writercost.Compare(before, after).Pass; with no [after],
                   [trial].status must be "pending" — any mismatch fails TestWriterKillSwitch (c-6: ships only if thresholds hold, else inline)

  t-13 Require a current writer for solo runs   (depends t-7)
       files:    internal/cmd/reviewer_agent.go, internal/cmd/execute.go, internal/cmd/solo_begin_test.go, internal/cmd/doctor.go,
                 internal/cmd/doctor_reviewer_test.go
       covers:   c-2, c-4
       desc:     Generalise the reviewer readiness check to name an agent (ok, missing, stale or shadowed). `execute begin --solo` also
                 requires a current writer while writer.Enabled holds. Pair begin checks nothing new. Doctor gains a writer line.
       contract: - if `execute begin p --solo` succeeds with the writer missing, stale (one byte changed) or shadowed by a repo
                   .claude/agents copy while Enabled, TestSoloBeginNeedsWriter fails; with Enabled false (seam) it must succeed
                 - if `execute begin p` (pair) output or exit changes with the writer missing, TestPairBeginUnchanged fails (c-4)
                 - if doctor omits a writer status line, or reports ok for a stale one, TestDoctorWriterSection fails
                 - after chdir(), the writer status reads ok (TestChdirSeedsReviewer extended) — install ships every assets/agents/*.md

Wave 4 (depends t-8, t-10)
  t-14 Add writer brief and status verbs
       files:    internal/cmd/writer_brief.go (new), internal/cmd/writer_brief_test.go (new), internal/cmd/task.go, cmd/dross/testdata/cli_tree.txt
       covers:   c-2, c-4, c-5
       desc:     `dross writer brief` prints `inline: <why>` (below threshold, or switch off) and writes nothing. Otherwise it writes
                 writer-brief.md and writer.json{briefed}, and prints only the path, digest, subagent_type and prompt line. It refuses
                 pair mode, a quick, and a second brief for the attempt. `dross writer status` prints state, files, test and notes
                 (bounded). `task status … failed` clears that scope's writer.json.
       contract: - if the printed digest differs from the one the spawn gate regenerates for the same scope, TestWriterBriefDigestParity fails
                 - if stdout carries any brief body line, file content or diff line, TestWriterBriefPrintsNoBody fails (c-2)
                 - surface 49,999 → `inline: below the 50000-byte size gate` and no file written; Enabled false → `inline: writer off` —
                   TestWriterBriefSizeGate
                 - pair mode → error naming "the human is the gate" and no .dross/ byte changes; solo quick → error naming quick_solo —
                   TestWriterBriefRefuses (c-4)
                 - if `writer status` output exceeds files + 3 lines, or prints a field outside {state, files, test, notes, cause},
                   TestWriterStatusBounded fails (c-2 "only its bounded report")
                 - after `task status p t-3 failed --reason x`, writer.json for (p,t-3) is gone and a re-brief of t-3 succeeds; failing t-4
                   leaves t-3's record — TestFailedClearsWriterRecord (c-5 retry path)
                 - missing `writer brief` / `writer status` rows fail TestCLITreeGolden

Wave 5 (depends t-11, t-14)
  t-15 Route solo tasks through the writer
       files:    assets/prompts/_execute.solo.md, internal/cmd/execute_prompt_test.go, internal/cmd/quick_prompt_test.go
       covers:   c-2, c-3, c-4, c-5
       desc:     The solo partial gains a writer step after 1a. Run `dross writer brief`; on `inline:` carry on as today. Otherwise skip
                 §1b–1d reading and the §1e `git diff` show, spawn dross-task-writer in the background with the printed line verbatim,
                 wait for its completion notice, then run `dross writer status`. Green leads to a bare `dross test`, then the reviewer,
                 then the commit. Red, unavailable, a spawn error or a still-briefed record leads to stash, then `failed --reason`, never
                 an inline fix. execute.md and quick.md stay untouched.
       contract: - if `dross writer brief` does not precede the spawn, the spawn does not precede `dross writer status`, or status green is
                   not followed in order by bare `dross test` → `dross review context` → commit, TestExecuteWriterOrder fails (c-3)
                 - if the writer failure branch lacks the stash line before `failed --reason`, or any writer branch says to fix or edit inline,
                   TestExecuteWriterFailurePath fails (c-5)
                 - if the writer step stops telling the agent to skip reading task.files and showing `git diff`, TestExecuteWriterNoDiff fails (c-2)
                 - if execute.md, or any partial the pair facts fetch (assets.Fetched with mode=pair), mentions dross-task-writer or
                   `dross writer`, TestPairPromptHasNoWriter fails (c-4 byte-for-byte); quick.md and _quick*.md likewise —
                   TestQuickHasNoWriter (quick_solo)
                 - TestPromptByteBudget keeps passing with execute.md unchanged (all writer prose in the solo partial)
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 one writer per task may edit, solo only; never commits/status/finalizes | t-1, t-4, t-6, t-7, t-9, t-10 |
| c-2 size-gated writer from a dross-printed line; bounded report only | t-2, t-5, t-7, t-10, t-12, t-13, t-14, t-15 |
| c-3 bare `dross test`, reviewer, commit stay in the main loop after return | t-9, t-10, t-11, t-15 |
| c-4 spawn refused unless --solo; pair byte-for-byte | t-10, t-11, t-13, t-14, t-15 |
| c-5 error/red/nothing → stash + `failed --reason`, never inline takeover | t-1, t-2, t-6, t-10, t-11, t-14, t-15 |
| c-6 before/after recorded; thresholds gate the writer, else inline | t-3, t-8, t-12 (switch also enforced by t-10, t-14) |

All 6/6 criteria covered.

## Judgment calls

- **The writer's red/empty/error outcome fails the task through a gate (t-11 refuses main-loop code edits and commits), not prose alone.** Rejected: relying on the solo partial's wording. c-5's "never takes the task over inline" is untestable without a refusal.
- **`dross writer brief` records `briefed` before the spawn.** Rejected: recording only at PostToolUse launch. An errored spawn never produces a PostToolUse, so without a pre-spawn record the takeover gate would see "no writer" and let the main loop fix inline.
- **The reviewer's one fix round stays inline in the main loop once the writer returned green.** Rejected: a second writer for the fix round, which breaks "one writer subagent per task", and failing the task on any block, which makes the outcome stricter than today's solo review.
- **The writer's green is discarded by ClearGreen at its SubagentStop.** Rejected: refusing every `dross test` from the writer. The writer needs `--files` runs for its report, and with no lanes (this repo) a `--files` run records a full green. Clearing at stop makes "the writer's green certifies nothing" true by construction.
- **The writer's Bash runs on an allowlist (git read verbs plus a handful of dross verbs).** Rejected: a denylist of commit/status/finalize verbs. A denylist misses the next finalizing verb dross adds.
- **The writer's Edit/Write is limited to `task.files` and refused under `.dross/`.** Rejected: allowing any path. Editing plan.toml directly would set task status around the CLI, and solo has no human to approve a plan deviation.
- **The spawn gate is PreToolUse, refusing a widened prompt before it runs.** Rejected: the reviewer's post-hoc "unavailable" pattern. The writer's edits are the damage, so the refusal has to come before it runs.
- **The size metric is the existing bytes of `task.files` at task start, gated at 50,000 bytes (t-12 pins it from the record).** Rejected: file count, because baseline growth is not monotonic in it (2-file tasks had the highest median), and also rejected counting planned-new files, which cannot be measured before writing.
- **The kill switch is a compile-time `writer.Enabled` that a record test forces to match `Compare(before, after)`.** Rejected: a project.toml or defaults.toml flag. The record lives only in the dross repo's phase dir, while the switch has to hold for every repo the binary runs in. The test makes CI fail if an [after] that misses the thresholds is committed while the writer is still on.
- **"Main-loop peak context" is compared as the median per-task peak growth over writer-eligible tasks.** Rejected: the absolute session peak. Baseline sessions started tasks at anywhere from 70k to 830k context, so the absolute peak measures session history rather than the writer. The locked baseline decision says "compared per task".
- **A writer report's `test` is green|red only; anything else is unavailable.** Rejected: a `not-run` value. An unverified writer return is exactly the case c-5 sends down the red path.
- **The [after] leg is not produced in this phase.** The baseline decision locks it to the next real solo phase, so t-12 ships the tooling, [before] and `[trial] status = "pending"`. TestWriterKillSwitch enforces the outcome once [after] lands. The verifier should expect c-6's "after" half to be satisfiable but not yet satisfied.
- **execute.md is left byte-identical, and all writer prose goes in `_execute.solo.md`.** Rejected: amending §1b's "only the main loop writes code" line. Editing it would change pair-mode prompt bytes (c-4) and spend the 15,000-byte budget. That line is still true for the read-only fan-out it describes.
- **A capture task (t-1) gates the three gate tasks.** Rejected: trusting the existing PostToolUse subagent fixture. Every refusal keys on PreToolUse `agent_type` being present inside the writer and absent in the main loop, and the dross_child_session_env memory rules out every other discriminator.
