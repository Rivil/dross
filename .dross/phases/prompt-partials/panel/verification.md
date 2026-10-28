# prompt-partials: verification lens draft

Lens: **design backward from the test contracts.** For each criterion I wrote the test
that would prove it first, then the smallest task that makes the test satisfiable *and
green at its own commit*. That second half matters more than usual here. The commit-green
gate refuses a code commit unless a full `dross test` is green, so no contract can land
red and wait for a later task to turn it green. Every contract below is either green on
the day it lands (it constrains future edits) or lands in the same task as the content
that satisfies it.

## Contracts first (criterion → ideal test)

| crit | ideal test contract | where it lives |
|---|---|---|
| c-1 | `TestPromptByteBudget`: `len(assets.FS["prompts/execute.md"]) > 15000` fails, naming the file and the overage | `assets/budget_test.go` |
| c-2 | the same test, `verify.md` row; plus a set guard that the budgeted set is exactly {execute.md, verify.md}, so dropping a row fails | `assets/budget_test.go` |
| c-3 | `TestPartialRegistry`: (a) a fetch line naming a partial file that does not exist fails as `prompt:line`; (b) a `_<prompt>.<name>.md` that no fetch line references fails as an orphan; (c) a fetch line with no `**When** \`k=v\`` trigger fails; (d) `dross prompt partial execute solo` prints exactly the embedded bytes | `assets/partials_test.go`, `internal/cmd/prompt_test.go` |
| c-4 | `TestCommonPathFetchesNothing`: every fetch trigger is evaluated against the fixed common-path fact set, and any trigger that holds fails (e.g. a fetch gated `mode=pair` or `test_exit=0`). The positive dual is `TestCommonPathInline`: the **raw** base prompt still contains every common-path command (§1c approve label, `dross test --files`, bare `dross test`, `git add <task.files>`, `dross changes record`, `--landmark`, §1g gate, `dross verify <phase>`, criterion classification, `dross verify finalize`, the pass next-step block) | `assets/partials_test.go` |
| c-5 | (a) every existing prompt-content test reads **prompt + partials**: a meta-guard fails any `internal/**/_test.go` that builds a raw path to `prompts/execute.md` / `prompts/verify.md` outside the expanding helper. (b) A conservation manifest: every fenced command line and hard-rule lead-in of the pre-split prompts must appear in the expanded prompt, or be claimed by an `// absorbs: <command>` marker in `internal/cmd` source. Deleting any instruction without moving or absorbing it fails, naming it. (c) `TestNoTestLost` (existing) already fails on a deleted test name | `internal/cmd/prompt_expand_test.go`, `assets/conservation_test.go` |
| c-6 | measurement scripts that refuse dishonest records: no commit in the window means no execute record; a fixture tree hash that differs from the before-run means no after record; prompt bytes are stat'ed from the installed file the session actually loaded, never typed in by hand | `.dross/phases/prompt-partials/measure/` |

**Partial layout (follows from the contracts above):** partials are top-level
`assets/prompts/_<prompt>.<name>.md`. The `_` prefix is the repo's existing convention
for a non-command prompt fragment (`_interaction.md`; `commands_parity_test` and
`interaction_coverage.go` already skip `_*`). Every existing corpus walker that globs
`prompts/*.md` (`promptFiles`, `rawPrompts`, `emittedStatuses`, the execconsent and
commit-gate walks) therefore checks partials with no edit. `TestEmbedDrift` already
covers their embedding.

**Fetch-point grammar (so "names its trigger" is machine-checkable):** one line per fetch point:
`**When** \`<key>=<value>\` — <prose>: \`dross prompt partial <prompt> <name>\``.
The keys and values come from a closed vocabulary declared once in `assets/partials.go`:
`mode{pair,solo}`, `preflight{ok,recovery}`, `board_pull{none,reported}`,
`test_exit{0,nonzero}`, `leg{attached,detached}`, `scope{ranged,degraded}`,
`unclassified{zero,nonzero}`, `reviews{none,present}`, `verdict{pass,partial,fail}`.
The common path is `mode=pair, preflight=ok, board_pull=none, test_exit=0, leg=attached,
scope=ranged, unclassified=zero, reviews=none, verdict=pass`. An unknown key fails, so a
new branch forces an explicit decision about whether it sits on the common path.

**Expansion semantics:** `assets.Expand(prompt)` inlines each partial **immediately after
its fetch line**, so the order-sensitive tests still measure order where the branch lives.

---

```
Phase prompt-partials — 13 tasks across 7 waves

Wave 1
  t-1  Capture before-measurement on a fixed fixture
       files:    .dross/phases/prompt-partials/measure/fixture.sh,
                 .dross/phases/prompt-partials/measure/measure.sh,
                 .dross/phases/prompt-partials/measurements.toml
       covers:   c-6
       desc:     fixture.sh writes a deterministic Go fixture repo (static .dross files, no
                 `dross init`, fixed GIT_*_DATE, one milestone/phase/task/criterion) and
                 refuses without gremlins on PATH. With the CURRENTLY installed binary and
                 prompts, the user runs one pair /dross-execute task and one attached
                 /dross-verify to pass in it. measure.sh extracts the [before] block from
                 the session transcript jsonl.
       contract: `measure.sh --selftest` fails if a synthetic transcript window with no
                 `git commit` tool_result yields an execute record instead of exit≠0 "no
                 commit in window"; same for a verify window with no `dross verify
                 finalize` result
                 measure.sh records wall_clock_s AND agent_s (wall minus
                 AskUserQuestion→answer gaps); selftest fails if agent_s is not
                 wall_clock_s − the synthetic 120s human gap
                 loaded_bytes is stat'ed from the resolved ~/.claude/dross/prompts/<name>.md
                 plus the summed bytes of `dross prompt partial` tool_results in the window;
                 selftest fails if a synthetic partial fetch of 900 bytes is not added
                 running fixture.sh twice gives identical `git rev-parse HEAD^{tree}`
                 (recorded as fixture_tree), so a non-deterministic fixture is visible
                 before any after-run depends on it

Wave 2 (depends t-1)
  t-2  Add partial registry, fetch grammar, expander
       files:    assets/partials.go, assets/partials_test.go
       covers:   c-3, c-4, c-5
       desc:     Partial(prompt,name) reads prompts/_<prompt>.<name>.md from FS;
                 FetchPoints(text) parses the grammar; Expand(prompt) inlines partials after
                 their fetch line; TriggerVocabulary + CommonPath facts; registry,
                 common-path and common-path-inline tests over the real embedded FS
                 (green today: no partials yet, and the raw prompts still hold every
                 common-path command)
       contract: Partial("execute","../x") and Partial("execute","Solo") return an error
                 (segment-fenced, lowercase-only); never a read outside prompts/
                 fstest.MapFS with a fetch line for a missing partial → registry check fails
                 naming "execute.md:<line>"; a `_execute.ghost.md` no line references →
                 fails as orphan; `_interaction.md` (no `.<name>` segment) is NOT an orphan
                 a fetch line without `**When** \`k=v\``, or with key `colour=red`, fails
                 the grammar check
                 synthetic trigger `mode=pair` (or `test_exit=0`) makes
                 TestCommonPathFetchesNothing fail
                 Expand on a MapFS puts the partial body directly after its fetch line
                 (index(fetch) < index(body) < index(next base line))
                 TestCommonPathInline fails if raw execute.md loses `git add <task.files>`,
                 `dross changes record`, `dross test --files`, `approve <task-id>` or
                 `### 1g`, or raw verify.md loses `dross verify <phase>`,
                 `dross verify finalize`, `covered`/`weak`/`uncovered`, or
                 "If verdict is `pass`:"

  t-3  Absorb version, board sync, pull into begin
       files:    internal/cmd/execute.go, internal/cmd/execute_test.go,
                 internal/boardsync/board_lifecycle_divergence_test.go
       covers:   c-1, c-4
       desc:     `dross execute begin` (after recording mode) sets version to
                 <milestone major.minor>.<DisplayNumber>.0 via writeVersion when patch or
                 major.minor differ; runs SyncPhase in-progress and a report-only taskPull
                 when the board is enabled; board errors print a named line and do not fail
                 begin. `// absorbs:` markers sit at each call. emittedStatuses learns every
                 literal status passed to boardsync.SyncPhase in internal/cmd (AST scan).
       contract: milestone v1.8 phases [a,p], version 1.8.1.3 → `begin p` writes 1.8.2.0
                 to BOTH state.json and project.toml
                 version 1.8.2.1 (a quick ran mid-phase) → `begin p` again leaves 1.8.2.1;
                 a resume never zeroes internal
                 phase absent from the milestone (ordinal 0) → version byte-unchanged and
                 output says so
                 youtrack httptest board: begin sends status in-progress for p; with a card
                 moved on the board the output lists "t-2 pending → done" and plan.toml is
                 byte-unchanged (report-only)
                 board server 500 → exit 0, execute.json still records the mode, output has
                 "board sync failed"; board off → no board line at all
                 deleting the SyncPhase call from begin makes the boardsync
                 state-map-keyed-on-emitted-status test fail once execute.md stops emitting
                 in-progress

Wave 3
  t-4  Absorb uat sync and verified state (depends t-3)
       files:    internal/cmd/verify.go, internal/cmd/verify_finish_test.go
       covers:   c-2, c-4
       desc:     finishVerify (shared by attached `dross verify` and `verify results`) runs
                 SyncPhase uat when the board is enabled, non-fatally, via t-3's helper;
                 `verify finalize`'s RunE (not finalizeVerify, which ship/complete auto-heal
                 call) sets current_phase_status=verified and touches
                 "verified <id>: <verdict> (<covered>/<total>, mutation <score>)". Absorb
                 markers added.
       contract: youtrack httptest board: `dross verify p --skip-mutation` sends status uat;
                 a 500 still leaves verify.toml + tests.json written, exit 0, output has
                 "board sync failed"
                 `verify results` (detached collect) sends uat too, so a detached verdict
                 is not left at in-progress
                 finalize on a pass verify.toml → state.json current_phase_status ==
                 "verified" and the last history entry starts "verified p: pass"
                 finalize refused on verdict "pending" → state.json byte-unchanged
                 a second finalize ("already recorded") appends no second history entry
                 ship's finalize auto-heal does NOT write current_phase_status (it calls
                 finalizeVerify, not the RunE)

  t-5  Add `dross prompt partial` fetch command (depends t-2)
       files:    internal/cmd/prompt.go, internal/cmd/prompt_test.go, cmd/dross/main.go,
                 README.md
       covers:   c-3
       desc:     `dross prompt partial <prompt> <name>` prints assets.Partial bytes verbatim
                 to stdout; no session dedupe; registered in root; README command-table row
       contract: stdout of `prompt partial` for a MapFS-seeded partial is byte-equal to the
                 embedded bytes (no trailing newline added or stripped)
                 unknown name exits ≠0 and the error lists the prompt's available partial
                 names, never an empty print that reads as "nothing to do"
                 `prompt partial execute ../verify` exits ≠0 (path fence)
                 main.go source lacking `cmd.Prompt()` fails TestPromptCommandRegistered
                 README lacking `dross prompt partial` fails its doc row test

  t-6  Record instruction-conservation manifest and guard (depends t-2)
       files:    assets/testdata/prompt_instructions.txt, assets/conservation_test.go
       covers:   c-5
       desc:     Extract every fenced command line and every `- **…**` hard-rule lead-in
                 from today's execute.md and verify.md into the manifest. The test requires
                 each row to appear in Expand(prompt), or a matching `// absorbs: <row>`
                 marker in internal/cmd non-test source.
       contract: removing `dross verify finalize <phase-id>` from verify.md and every
                 partial fails TestPromptInstructionsConserved, naming the row
                 a row claimed only by an `absorbs:` marker whose marker is then deleted
                 fails
                 the extractor finding <40 rows in execute.md or <25 in verify.md fails
                 (the manifest cannot silently go vacuous)
                 green at landing: every row is still in the raw prompts

  t-7  Route central prompt helpers through Expand (depends t-2)
       files:    internal/cmd/prompt_expand_test.go, internal/cmd/prompt_test_command_test.go,
                 internal/cmd/consent_surface_test.go, internal/cmd/execute_prompt_test.go,
                 internal/cmd/verify_prompt_test.go
       covers:   c-5
       desc:     New expandedPrompt(t,name) helper; promptBody, readPrompt, rawPrompt,
                 executePromptContent, verifyPromptBody (and verify_prompt_test's inline
                 read) return Expand(name). Meta-guard TestPromptReadsGoThroughExpansion
                 with a ratchet allowlist naming the stray files t-8 migrates (plus
                 install_test.go, which compares install bytes on purpose).
       contract: a new `os.ReadFile(filepath.Join(root,"assets","prompts","verify.md"))`
                 in any internal/ test file outside the allowlist fails the meta-guard,
                 naming file:line
                 with a MapFS partial holding a banned phrase, a negative assertion over
                 promptBody sees it (proves negative checks cover partials)

Wave 4
  t-8  Migrate stray prompt readers; empty the allowlist (depends t-7, t-3)
       files:    internal/cmd/lane_toolchain_docs_test.go, internal/cmd/phase_test.go,
                 internal/cmd/options_docs_test.go, internal/cmd/pad_residue_test.go,
                 internal/cmd/execconsent_docs_test.go,
                 internal/boardsync/board_lifecycle_divergence_test.go,
                 internal/cmd/prompt_expand_test.go
       covers:   c-5
       desc:     Each raw execute.md/verify.md read becomes expandedPrompt (or assets.Expand
                 in boardsync). The ratchet allowlist shrinks to install_test.go only.
       contract: the meta-guard asserts the allowlist is exactly {install_test.go};
                 re-adding a stray file to it fails
                 TestTaskEdgesPairTheBoardStatusWithThePlanStatus reads Expand("execute"),
                 so moving the §1f done/task-in-review pair into a partial keeps it paired
                 (and transposing them still fails)

  t-9  Move solo and preflight-recovery branches to partials (depends t-5, t-6, t-7)
       files:    assets/prompts/execute.md, assets/prompts/_execute.solo.md,
                 assets/prompts/_execute.recovery.md, internal/cmd/execute_prompt_test.go
       covers:   c-1, c-3, c-4, c-5
       desc:     Solo text (§1c solo, §1e solo red, §1f solo review, §1g solo) moves into
                 _execute.solo.md, fetched once right after the `begin --solo` fence with
                 `**When** \`mode=solo\``. Base keeps one-line anchors at §1e/§1f. The
                 refused-solo-begin text stays inline because it precedes the fetch.
                 Wrong-branch, dirty-tree and in_progress resume handling moves to
                 _execute.recovery.md (`preflight=recovery`). Solo tests retargeted, names
                 kept.
       contract: the fetch line for `execute solo` must be the first fetch after the
                 `dross execute begin <id> --solo` fence in §0; moving it into §1 fails
                 (locked solo_branch)
                 TestExecuteSoloReviewPlacement (retargeted) fails if base §1f's solo anchor
                 sits after `git add <task.files>` or before the bare `dross test`, and if
                 the partial's order review context < spawn < review status breaks
                 TestExecuteSoloRedStashes (retargeted to the solo partial) still fails if
                 `failed` precedes `git stash push`
                 TestExecutePromptSoloBeginNeedsReviewer still reads RAW §0 (refusal
                 precedes the fetch); moving that text into the partial fails it
                 mode=pair facts → TestCommonPathFetchesNothing fetches neither partial

Wave 5
  t-10 Move test-exit and board-moves; enforce execute budget (depends t-8, t-9, t-3)
       files:    assets/prompts/execute.md, assets/prompts/_execute.test-exit.md,
                 assets/prompts/_execute.board-moves.md, assets/budget_test.go,
                 internal/cmd/execute_prompt_test.go
       covers:   c-1, c-3, c-4, c-5
       desc:     Delete the absorbed §0 prose (version bump, phase sync, pull) and replace it
                 with how to read begin's report. Pull reactions (--apply, conflicts,
                 refused provider) go to _execute.board-moves.md (`board_pull=reported`).
                 Pair red path and exit codes 1–8 go to _execute.test-exit.md
                 (`test_exit=nonzero`). Compress landmark, svelte, offload and §1e prose to
                 ≤14,500 B (headroom). Create TestPromptByteBudget with the execute row.
                 Retarget TestExecutePromptWiresPhaseNumber (name kept) to drive
                 `execute begin` on a milestone fixture.
       contract: TestPromptByteBudget fails at 15,001 B of execute.md, naming file and
                 overage
                 lane_toolchain exit-code tests (expanded) still fail if
                 _execute.test-exit.md drops code 8 or the "2, 3, 4, 5, 6, 7 and 8 all mean
                 the run did not happen" line
                 TestExecutePromptPullsBoardTaskMoves (expanded) still fails without
                 `dross issue task pull <phase> --apply` in the board-moves partial
                 TestPromptInstructionsConserved passes only because the version/sync/pull
                 rows are claimed by t-3's absorbs: markers
                 TestEveryPromptIssueInvocationResolves walks _execute.board-moves.md (glob)
                 and fails on a mistyped verb there

  t-11 Move detached and scope branches of verify (depends t-8, t-4, t-5, t-6)
       files:    assets/prompts/verify.md, assets/prompts/_verify.detached.md,
                 assets/prompts/_verify.scope.md, internal/cmd/verify_prompt_test.go
       covers:   c-2, c-3, c-4, c-5
       desc:     Long-legs/detach/collect/exit 10–15/host-lock text goes to
                 _verify.detached.md (`leg=detached`). The range-provenance paragraph goes to
                 _verify.scope.md (`scope=degraded`: verify printed a `scope degraded:`
                 line). Delete the absorbed uat-sync prose and say how to read verify's
                 board line. Merge the two duplicate denominator paragraphs.
       contract: options_docs detach tests (expanded) still fail if _verify.detached.md
                 loses exit 10/11/12/13/14 or "A non-finished result is not a verdict"
                 TestVerifyPromptReadsRangeProvenance (expanded) still fails if
                 _verify.scope.md loses a whole_file reason
                 new TestVerifyScopeTriggerIsTheDegradedLine: the scope fetch's prose names
                 `scope degraded:`, the literal printScopeSummary prints, so a renamed
                 output line fails
                 the `dross issue phase sync <phase> --status uat` row stays conserved only
                 through t-4's absorbs: marker

Wave 6
  t-12 Move drain, solo-review, not-pass; enforce verify budget (depends t-10, t-11)
       files:    assets/prompts/verify.md, assets/prompts/_verify.drain.md,
                 assets/prompts/_verify.solo-review.md, assets/prompts/_verify.not-pass.md,
                 assets/budget_test.go
       covers:   c-2, c-3, c-4, c-5
       desc:     Survivor drain verbs/table go to _verify.drain.md (`unclassified=nonzero`);
                 the §0 reviews note and §4 Solo review block go to _verify.solo-review.md
                 (`reviews=present`); the fail/partial recommendations and next block go to
                 _verify.not-pass.md (`verdict=partial` and `verdict=fail` fetch lines).
                 Delete the absorbed state set/touch prose. Keep the next-phase block inline
                 with its needles. Add the verify.md row and the exact-set guard to
                 TestPromptByteBudget.
       contract: TestPromptByteBudget fails at 15,001 B of verify.md, and fails if the
                 budgeted set is not exactly {execute.md, verify.md}
                 TestVerifyPromptNamesTheDrainVerbs / RestatesTheDrainPolicy (expanded)
                 still fail without `dross survivor accept|route` in _verify.drain.md
                 TestVerifyShowsReviews (expanded) still fails if _verify.solo-review.md
                 drops "UNRESOLVED"
                 TestVerifyPromptReadsTheMilestoneArray / GatesTheLastPhaseClaim /
                 KeepsTheNoMilestoneFallback still read the inline pass block
                 (TestCommonPathInline pins "If verdict is `pass`:" to raw verify.md)
                 verdict=pass facts → no not-pass fetch; unclassified=zero → no drain fetch

Wave 7
  t-13 Capture after-measurement on the same fixture (depends t-12, t-4, t-5)
       files:    .dross/phases/prompt-partials/measurements.toml
       covers:   c-6
       desc:     `make install`. Rebuild the fixture with fixture.sh and rerun one pair
                 execute task and one attached verify with the same model. Record [after]
                 next to [before], plus a [delta] block and a one-line n=1 caveat.
       contract: measure.sh refuses the after record when fixture_tree differs from
                 [before].fixture_tree
                 measure.sh refuses when the installed binary's `dross version` commit is
                 not repo HEAD (stale binary, per the make-install memory)
                 after-run partial_bytes for execute and for verify must be 0; a non-zero
                 value is recorded as-is and flagged as a c-4 regression, never edited away
                 after.loaded_bytes equals `wc -c` of the installed execute.md / verify.md
                 (stat'ed, not typed)
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (execute.md ≤15 KB, budget test) | t-10 (budget test + content), t-9 (solo/recovery bytes out), t-3 (absorb makes the cap reachable) |
| c-2 (verify.md ≤15 KB, same test) | t-12 (verify row + set guard), t-11 (detached/scope bytes out), t-4 (absorb) |
| c-3 (one fetch command, named triggers, missing/orphan test) | t-2 (registry, grammar, missing/orphan), t-5 (`dross prompt partial`), t-9, t-10, t-11, t-12 (each fetch point written to the grammar) |
| c-4 (common pair path fetches zero) | t-2 (TestCommonPathFetchesNothing + TestCommonPathInline, landed green before any split), t-3/t-4 (common-path mechanics move to the CLI, not to partials), t-9–t-12 (triggers checked), t-13 (observed partial_bytes = 0) |
| c-5 (existing tests pass on prompt + partials) | t-2 (Expand), t-6 (conservation manifest), t-7 + t-8 (all readers expanded, meta-guard), t-9–t-12 (retargets keep names; TestNoTestLost guards deletion) |
| c-6 (before/after bytes, turns, wall-clock recorded) | t-1 (before), t-13 (after) |

6/6 criteria covered.

## Judgment calls

- **Before-measurement is the only wave-1 task.** I rejected running it alongside the Go tasks. `make install` symlinks prompts from this checkout, so any prompt edit or reinstall after t-1 starts would contaminate the "before" sample.
- **Fixture writes static `.dross` files instead of calling `dross init`.** I rejected `dross init` because the before and after binaries could produce different trees, which would break the same-starting-state guarantee the after-run checks.
- **Verify sample is an attached, measured gremlins run on a tiny fixture.** I rejected `--skip-mutation` (it skips the measured verdict branch and the survivor cross-check, which is the common path for this repo) and rejected a real dross phase (a 1h40m leg would drown the prompt cost).
- **wall_clock_s and agent_s are both recorded.** Pair mode includes human think time, so wall-clock alone would credit or blame the prompt for how fast the user answered.
- **Partials live at top level as `_<prompt>.<name>.md`, not in `prompts/partials/`.** This reuses the existing `_` convention. Every `prompts/*.md` corpus walker covers partials with zero edits. A subdirectory would have needed about 9 walker rewrites and a new install rule.
- **Expansion inlines each partial at its fetch line.** I rejected appending partials at the end of the prompt, which would break every order-sensitive test (consent before run, plan/board status pairing, test before `git add`) in a way that hides real misplacement.
- **Fetch triggers use a closed `key=value` vocabulary.** I rejected free prose, because then c-4 could only be checked by reading. The closed set is what lets "the common path fetches zero" be an executable assertion.
- **Contracts land green.** The common-path and conservation tests (t-2, t-6) land before any split, because they pass on today's prompts. The budget rows land in the same task that gets each file under 15,000 B. I rejected writing the budget test first, because a red test cannot pass the commit-green gate.
- **Conservation manifest of command lines and hard-rule lead-ins, not prose.** Prose has to be compressed to hit the cap, so a prose manifest would block that. Commands and hard rules are where a silent loss does real damage.
- **Absorbed steps are claimed by `// absorbs:` markers in Go source.** I rejected editing the manifest when a step is absorbed, which would couple every split task to the testdata file. The markers keep the claim next to the code that took over the work.
- **Existing tests over absorbed or moved text are retargeted with their names kept (WiresPhaseNumber, the solo-review tests).** Deleting them is not an option: TestNoTestLost forbids it, and their contracts (patch from phase number, solo review order) still hold, only on a new surface.
- **A board failure in begin or verify prints a named line and exits 0.** I rejected failing begin, because a board outage would then block local execution. The prompt reacts to the printed line instead.
- **begin keeps internal on a resume of the same phase.** I rejected always resetting to `.0`. A re-entered phase is not a new phase start, so resetting would erase quick-task bumps.
- **The state write lives in `verify finalize`'s RunE, not in finalizeVerify.** ship and phase complete call finalizeVerify as an auto-heal, and they must not rewrite current_phase_status.
- **Stack loadout stays a prompt call (not absorbed).** The locked decision names three steps, and absorbing a fourth would have meant retargeting TestExecutePromptInvokesLoadout for about 300 B. The paragraph is compressed instead.
- **The next-phase resolution stays inline in verify.md.** It is on the pass path, and three tests guard a real 2026-08-13 bug through its wording, so I rejected absorbing it into finalize.
- **unmeasured-verdict rules stay inline.** A `--skip-mutation` run that reaches pass is a common pair path, so making it fetch a partial would break c-4 for doc-only phases.
- **t-8 touches 7 files.** They are one-line read redirects. Splitting them further would only add commits, and the ratchet allowlist in t-7 keeps both commits green.
- **Each split task targets ≤14,500 B, not 15,000.** The byte estimates (execute about 14.8 KB, verify about 14.8 KB after moves) leave no room otherwise. The test enforces 15,000 and the target gives headroom for later fixes.
