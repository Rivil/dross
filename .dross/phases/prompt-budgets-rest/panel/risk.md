# prompt-budgets-rest — RISK lens draft

The graph starts from what goes wrong. Each risk below has one owning task, and that task's contract is the test that fails when the risk happens:

- **R1. The cap goes vacuous or grows an exception.** A fixed list or glob misses a prompt, or a per-prompt override returns.
- **R2. The manifest is re-recorded after text moved,** so instructions drop out of it silently. The recorder rewrites the whole file, and the three rows that `execute begin` absorbed would vanish.
- **R3. A raw reader misses moved text.** A negative assertion over a raw reader stays green after the text moves into a partial. 15 allowlisted raw readers touch the five prompts.
- **R4. The common path fetches.** A vocabulary key's first value is wrong, or a trigger holds on every run.
- **R5. Text is copied, not moved.** "Exactly 1" counts and double instructions drift.
- **R6. An inlined `## ` heading splits a section,** so section-scoped tests read a truncated section.
- **R7. `TestExpandIdentity` is anchored on quick.md.** It fails the moment `_quick.solo.md` exists.
- **R8. Quick §6 absorption bumps on abort, double-bumps, or loses the bump on a board failure.** There is also an interim double-bump if the CLI change and the prompt change land apart.
- **R9. `TestTestStepPromptsNotGrown` caps expanded quick.md at 18,102 B.** Fetch lines added before any deletion would trip it.
- **R10. The before legs measure edited prompts.** `~/.claude/dross/prompts` is symlinked to the working tree that splits are editing.
- **R11. The flow-cost record pins live sizes,** which breaks the next phase that edits ship.md. Or it gates turns, which locked `flow_set` forbids.
- **R12. The after legs run a stale binary.** `dross prompt partial` then fails on a new partial (r-01).

```
Phase prompt-budgets-rest — 17 tasks across 5 waves

Wave 1
  t-1  Capture before legs on base build
       files:    .dross/phases/prompt-budgets-rest/flow-cost-before.toml
       covers:   c-4
       desc:     Build base dross in a scratch worktree at 8eb38e9f (go build only; never `dross test` or `make install`
                 from the worktree). Run full pair legs for spec→lock, plan (no --panel)→lock and pair quick→commit on
                 fixtures built by a copy of prompt-partials' setup_sh, extended with plan, spec and quick probes.
                 Record ship and options base loaded bytes only. Quick flow_end = the `chore(dross): record quick`
                 commit, so absorbed §6 falls inside the window.
       contract: if a before leg loaded an edited prompt, t-17's TestFlowCostRecords/prompt-budgets-rest fails:
                 before prompt_bytes must equal 21154 (plan), 18098 (quick), 19075 (spec), 23006 (ship), 28231 (options)
                 if the before binary isn't the base, t-17's test fails: binary_version's commit must have no diff
                 under assets/, internal/, cmd/ vs 8eb38e9f
                 if a leg's tree can't be rebuilt, t-17's fixture_tree equality check fails (after ≠ before tree)

  t-2  Enumerate every base prompt under one cap
       files:    assets/budget_test.go
       covers:   c-1
       desc:     Replace the {execute, verify} map with fs.Glob(FS, "prompts/*.md") minus `_*`, checked against one
                 const 15000. Add a shrink-only overCapPending set seeded with exactly {options, plan, quick, ship,
                 spec}. A pending prompt at or under the cap fails as a stale exemption. Add a vacuity floor.
       contract: if the budget goes back to a fixed list, TestPromptByteBudgetBites fails: a new prompts/zz.md
                 at 15,001 B in a MapFS is reported by name with "1 over"
                 if partials stop being exempt, the same test fails: _zz.big.md and _interaction.md at 30,000 B pass
                 if a non-pending prompt grows, TestPromptByteBudget fails: milestone.md (14,687 today) at 15,001 B fails now
                 if a split leaves its exemption behind, TestPromptByteBudget fails: a pending prompt ≤ 15,000 B is a stale exemption
                 if the pending set grows, the test fails: any name outside the seeded five
                 if the glob goes vacuous, the test fails: fewer than 25 base prompts, or execute.md not among them

  t-3  Record five prompts into conservation manifest
       files:    assets/testdata/prompt_instructions.txt, assets/conservation_test.go
       covers:   c-3
       desc:     Append the fenced `dross `/`git ` lines and `- **…**` lead-ins of unedited options, plan, quick, ship
                 and spec. Extend manifestPrompts and minRows. The record env now appends only prompts absent from the
                 manifest and never rewrites execute.md or verify.md rows.
       contract: if a re-record drops absorbed rows, TestPromptInstructionsConserved fails: with the record env set,
                 the three execute begin rows (`dross state set version …`, `dross issue phase sync <id> --status in-progress`,
                 `dross issue task pull <phase>`) survive
                 if the manifest was recorded after §6 moved, the pinned-row check fails: quick.md must hold
                 `dross state bump internal` and `dross issue quick $NEW_VERSION "quick: <one-line summary>"`
                 if an instruction is dropped in a split, the test fails naming the row: `dross ship <phase-id>` removed
                 from ship.md and every _ship partial
                 if extraction goes vacuous, minRows fails per prompt (each floor set at its recorded count minus 10%)

  t-4  Pin common-path vocabulary, inline text, guards
       files:    assets/partials.go, assets/partials_test.go
       covers:   c-2
       desc:     Add trigger keys panel{off,on}, auto{off,on}, ci{green,red}, merge{clean,failed}, target{given,empty},
                 spec_file{absent,present}, deferred{none,routed}, lanes{keep,change}, local{keep,change}; mode is reused.
                 Add guards:
                   - TestCommonPathVocabularyPinned
                   - TestCommonPathFetchesNothing (globbed over every base prompt)
                   - TestCommonPathInline needles for the five prompts, taken from today's text
                   - TestExpandIdentity re-anchored on a prompt with no partials
                   - TestPartialTextMovedNotCopied
                   - TestPartialSectionsStayWhole
       contract: if a key's values are reordered (lanes → {change, keep}), TestCommonPathVocabularyPinned fails:
                 CommonPath() must equal the pinned map {mode:pair, panel:off, ci:green, merge:clean, lanes:keep, …}
                 if any prompts/*.md fetches under CommonPath(), TestCommonPathFetchesNothing fails naming prompt and partial
                 (globbed, so a prompt added later is covered too)
                 if common-path text leaves a base prompt, TestCommonPathInline fails on a raw assets.FS read. Needles:
                   - quick: `dross quick begin "<description>"`, `dross quick end`
                   - plan: `### 6.1 Auto review`, `## 2G`
                   - spec: `Step 1 — the entry gate`, `dross phase create`
                   - ship: `dross ship <phase-id>`, `never gate on \`gh pr checks --watch\``
                   - options: `## Section pick`, `dross trust --check`, `dross test lane list`
                 if quick gains a partial, TestExpandIdentity still passes: its anchor is any base prompt whose
                 PartialNames is empty; if none exists, the test fails
                 if a partial copies text instead of moving it, TestPartialTextMovedNotCopied fails: any partial line of
                 40+ chars also present verbatim in its base prompt
                 if an inlined partial's `## ` heading lands above remaining base text of the fetch line's section,
                 TestPartialSectionsStayWhole fails (synthetic MapFS)

  t-5  Route quick-side prompt readers through Expand
       files:    internal/cmd/quick_prompt_test.go, internal/cmd/squash_claim_guard_test.go,
                 internal/cmd/interaction_setupcmds_test.go, internal/boardsync/board_lifecycle_divergence_test.go,
                 internal/cmd/prompt_expand_test.go
       covers:   c-3
       desc:     quickPromptContent, TestQuickPromptHandlesShippedWindow, setupPromptBody and boardsync rawPrompt
                 (quick/milestone/ship) read assets.Expand, keeping their own normalisation. Drop those 4 allowlist
                 entries and lower the pin by 4.
       contract: if a quick reader stays raw, TestPromptLoadersSeePartials (extended) fails: a phrase seeded only in
                 _quick.solo.md must reach quickPromptContent and setupPromptBody
                 if boardsync's floor reads bare quick.md, a seeded _quick.x.md holding the only `dross issue quick` line
                 must still be counted by its caller scan
                 if an entry is dropped without its read, TestPromptReadsGoThroughExpansion fails (pin vs entries / stale entry)

  t-6  Route options readers through Expand
       files:    internal/cmd/options_docs_test.go, internal/cmd/readme_doc_test.go, internal/cmd/prompt_expand_test.go
       covers:   c-3
       desc:     optionsPrompt and readme_doc optionsSection read the expanded options.md. Drop 2 entries, lower the pin by 2.
       contract: if optionsPrompt stays raw, a phrase seeded only in _options.lanes.md must reach it (seedPromptFS test)
                 if TestPromptReadsGoThroughExpansion sees options_docs_test#optionsPrompt read assets/prompts off disk,
                 it fails naming file:line

  t-7  Route plan and spec readers through Expand
       files:    internal/cmd/plan_prompt_test.go, internal/cmd/plan_review_tiers_test.go, internal/cmd/spec_prompt_test.go,
                 internal/rules/rules_test.go, internal/cmd/prompt_expand_test.go
       covers:   c-3
       desc:     planPromptContent, TestPlanPromptDescribesEveryReviewTier, specPromptContent and
                 TestSpecPromptDefersToSnippet (via assets.Expand(assets.FS, …)) read expanded text. Drop 4 entries,
                 lower the pin by 4.
       contract: if planPromptContent stays raw, a phrase seeded only in _plan.panel.md must reach it
                 if rules_test still reads spec.md off disk, TestPromptReadsGoThroughExpansion flags
                 internal/rules/rules_test.go#TestSpecPromptDefersToSnippet

  t-8  Route ship and cross-prompt readers through Expand
       files:    internal/cmd/ship_prompt_test.go, internal/cmd/interaction_coreloop_test.go,
                 internal/cmd/interaction_pilot_test.go, internal/cmd/phase_test.go, internal/cmd/prompt_expand_test.go
       covers:   c-3
       desc:     rawShipSection keeps "not normalised" but reads Expand(ship). TestPlanPromptNoArtifactDump,
                 TestShipPromptDecisionTurnsSeparate, TestSpecPilotUsesEmitter and
                 TestPhaseListPromptConsumersAreCurrent read expanded text. Drop 5 entries; the pin ends at 20.
       contract: if rawShipSection stays raw, the Forgejo `{"Do":"squash"}` match in §6 must still pass when the
                 seeded §6 text lives only in a _ship partial
                 if the allowlist is not exactly 20 after t-5..t-8, the pin check fails

Wave 2
  t-9  Absorb quick §6 bookkeeping into quick end
       (depends t-1, t-3, t-5)
       files:    internal/cmd/quick.go, internal/cmd/quick_test.go, assets/prompts/quick.md,
                 internal/cmd/quick_prompt_test.go, cmd/dross/testdata/cli_tree.txt
       covers:   c-1, c-3
       desc:     When HEAD moved past the begin-recorded head, `quick end` does all of this:
                   - bumps internal
                   - records changes quick-<n> (files = begin..HEAD minus .dross/) when in-phase and not shipped
                   - runs state touch and issue quick on the description's first line
                   - prints `version: <v>`
                 Delete the §6 prose in the same commit (no interim double bump). Keep the bookkeeping commit, the
                 `board: FAILED` retry `dross issue quick <version> "<summary>"`, and "missing `version:` = stale
                 binary, stop, ask for make install". Add one `// absorbs:` marker per moved row. Run `make install`
                 after the commit (r-01).
       contract: if an abort bumps, TestQuickEndAbortNoBump fails: HEAD == begin head → state.version unchanged, no
                 changes.json write, zero board requests (httptest counter)
                 if a missing record bumps, `quick end` with no record exits 0 and writes nothing
                 if a re-run double-bumps, TestQuickEndIdempotent fails: a second end after success leaves version +1 once
                 if the bump disagrees with the PR-route branch, the test fails: the new version must equal begin's
                 state.version with internal+1 (the `quick/<preview-version>` name)
                 if a shipped phase records changes, TestQuickEndShippedIsStandalone fails: status=shipped → bump,
                 no changes record
                 if a board 500 loses the bump, TestQuickEndBoardFailureKeepsBump fails: version and changes.json
                 persisted, `board: FAILED` plus the retry command printed, non-zero exit distinct from a usage error,
                 quick record removed
                 if .dross/ files leak into the record, the test fails: a commit touching .dross/state.json and a.go
                 records only a.go
                 if a moved row loses its claim, TestPromptInstructionsConserved fails: deleting the
                 `// absorbs: dross state bump internal` marker
                 if quick.md loses its board call, board_switch_prompt_test's 6-caller floor and the
                 TestBoardCallingPromptsSurfaceFailure sentence check fail

  t-10 Move plan --panel to partial
       (depends t-1, t-2, t-3, t-4, t-7)
       files:    assets/prompts/plan.md, assets/prompts/_plan.panel.md, internal/cmd/plan_prompt_test.go, assets/budget_test.go
       covers:   c-1, c-2, c-3
       desc:     §2P (5.6 KB) moves to _plan.panel.md. Its `**When** \`panel=on\`` fetch line sits exactly where §2P
                 was, after §2 and before `## 2G`. The §0 flag parse names the fetch condition instead of "see §2P".
                 Compress about 0.8 KB elsewhere under the conservation guard. Drop plan from overCapPending. Run
                 `make install` after the commit.
       contract: if the panel text fetches on the common path, TestPlanPanelFetchedOnlyUnderPanel fails:
                 Fetched(CommonPath) == [] and Fetched(CommonPath ∪ panel=on) == ["panel"]
                 if the fetch line moves, the test fails: it must be the last line before `## 2G` in raw plan.md,
                 and Expand(plan) must keep heading order 2 → 2P → 2G
                 if base plan.md still says "see §2P", the test fails (dangling ref to a heading absent from the base)
                 if the three lens names or `panel/synthesis.md` are lost, the expanded panel tests in plan_prompt_test fail
                 if plan.md is over 15,000 B, TestPromptByteBudget fails once its pending entry is gone

  t-11 Move spec no-target, resume, route branches
       (depends t-1, t-2, t-3, t-4, t-7)
       files:    assets/prompts/spec.md, assets/prompts/_spec.no-target.md, assets/prompts/_spec.resume.md,
                 assets/prompts/_spec.route.md, assets/budget_test.go
       covers:   c-1, c-2, c-3
       desc:     These branches move to partials:
                   - empty-$ARGUMENTS resolution → `target=empty`
                   - extend/replace of an existing spec.toml → `spec_file=present`
                   - step-2 destinations after "defer it" → `deferred=routed`
                 The create flow (the common `--new`), §3, the step-1 entry gate and §4's "skip any deferred item that
                 already has a target" stay inline. Compress about 1.3 KB. Drop spec from overCapPending. Run
                 `make install` after the commit.
       contract: if a spec branch fetches on the common path, TestCommonPathFetchesNothing (t-4) fails; each key's
                 off value fetches exactly its own partial (CheckPartials orphan check plus a Fetched table in
                 spec_prompt_test)
                 if route verbs are lost, TestPromptInstructionsConserved fails on `dross deferred route <phase-id> <idx> --target "<slug>"`
                 if the resume skip rule leaves §4, a raw §4 check fails: base §4 must contain "already has a target"
                 if the step-1 gate moves, TestCommonPathInline (t-4) fails on `Step 1 — the entry gate`
                 if spec.md is over 15,000 B, TestPromptByteBudget fails

  t-12 Move options lane and local-change detail
       (depends t-1, t-2, t-3, t-4, t-6)
       files:    assets/prompts/options.md, assets/prompts/_options.lanes.md, assets/prompts/_options.local.md,
                 internal/cmd/options_docs_test.go, assets/budget_test.go
       covers:   c-1, c-2, c-3
       desc:     Lane add/edit/selector/template/install/consent detail (about 10.3 KB) moves to `lanes=change`.
                 Tuning-knob and grant-change detail (about 2.6 KB) moves to `local=change`. §14 keeps every key name,
                 `dross local get`, `dross mutation remote status`, `dross trust --check` and `dross test lane list`.
                 Compress about 0.7 KB. Drop options from overCapPending. Run `make install` after the commit.
       contract: if a Keep-everything walk fetches, Fetched(options, CommonPath) == [] fails
                 if a local.toml key name leaves base §14, TestOptionsNamesEveryLocalKeyInline fails on a raw §14 read:
                 quick_base, allow_hosts, mutation_workers, mutation_test_cpu, remote_scratch_base,
                 mutation_remote_env, mutation_remote_host, mutation_remote_workdir, trusted_test_command
                 if lane verbs are lost, TestPromptInstructionsConserved fails on `dross test lane add <name> --match <glob> --command "<cmd>"`
                 if the existing options_docs coverage loses its scope, it fails: every settable project key is
                 named in expanded options.md
                 if options.md is over 15,000 B, TestPromptByteBudget fails

  t-13 Move ship recovery and --auto branches
       (depends t-1, t-3, t-4, t-5, t-8)
       files:    assets/prompts/ship.md, assets/prompts/_ship.recovery.md, assets/prompts/_ship.auto.md,
                 internal/cmd/ship_prompt_test.go
       covers:   c-2, c-3
       desc:     `## Recovery` moves to `merge=failed`, with its fetch line as the last line of §6. §0.5 moves to
                 `auto=on`. Delete the DEFERRED subagent-panel stub. §0's leaked-commits `dross ship recover` hint stays
                 inline. Ship stays in overCapPending. Run `make install` after the commit.
       contract: if recovery's heading splits §6, TestPartialSectionsStayWhole (t-4) fails: the fetch line must end §6
                 if a ship branch fetches on PR-open, Fetched(ship, CommonPath ∪ {auto:off, merge:clean}) == []
                 if `dross ship recover` leaves base §0, a raw §0 check in ship_prompt_test fails
                 if recovery heal steps are lost, TestPromptInstructionsConserved fails on its fenced rows

Wave 3
  t-14 Move quick solo text to one partial
       (depends t-2, t-4, t-9)
       files:    assets/prompts/quick.md, assets/prompts/_quick.solo.md, internal/cmd/quick_prompt_test.go,
                 internal/cmd/execute_prompt_test.go, assets/budget_test.go
       covers:   c-1, c-2, c-3
       desc:     These move to _quick.solo.md:
                   - §2 solo skip
                   - §4 solo red path and solo review
                   - §5 `solo: yes` line and the review-gated commit
                   - the solo no-test branch
                 It is fetched once, right after the `dross quick begin --solo` fence (`mode=solo`). §4 keeps a
                 `**Re-fetch**` line between the green `dross test` and `git add`. Drop quick from overCapPending. Run
                 `make install` after the commit.
       contract: if the solo fetch moves out of §0 (locked branch_placement), TestQuickSoloFetchPlacement fails: it
                 must be the first fetch after the `dross quick begin --solo` fence in raw quick.md
                 if solo-review ordering breaks, the test fails: the base §4 re-fetch must sit between `dross test`
                 and `git add`; inside the partial, the review precedes the commit
                 if pair mode fetches, Fetched(quick, CommonPath) == [] fails
                 if fetch lines push expanded quick.md past its base size, TestTestStepPromptsNotGrown (18,102) fails;
                 t-9's deletions are the headroom
                 if "pair mode by default" leaves the base Hard rules, a raw check fails
                 if quick.md is over 15,000 B, TestPromptByteBudget fails

  t-15 Move ship CI-failure branch; reach cap
       (depends t-2, t-13)
       files:    assets/prompts/ship.md, assets/prompts/_ship.ci-failure.md, internal/cmd/ship_prompt_test.go, assets/budget_test.go
       covers:   c-1, c-2, c-3
       desc:     §5 "On failure" (logs, fix loop, cap at 3) moves to `ci=red`. Compress the two-merge-levels
                 blockquote and the provider lines (Forgejo and GitLab stay inline). Drop ship from overCapPending.
                 Run `make install` after the commit.
       contract: if green CI fetches, Fetched(ship, CommonPath) == [] fails
                 if the `gh pr checks --watch` warning leaves base §5, TestCommonPathInline (t-4) fails
                 if §6 loses "a non-zero exit is a board failure", TestBoardCallingPromptsSurfaceFailure fails
                 if `gh run view --log-failed` or the 3-iteration cap is lost, TestPromptInstructionsConserved or the
                 ship CI test (expanded) fails
                 if ship.md is over 15,000 B, TestPromptByteBudget fails

Wave 4
  t-16 Delete the budget exemption mechanism
       (depends t-10, t-11, t-12, t-14, t-15)
       files:    assets/budget_test.go
       covers:   c-1
       desc:     overCapPending is empty after the splits. Remove it and its stale check, so the test is one glob and
                 one 15,000 constant.
       contract: if any exemption survives (locked no_exceptions), TestPromptByteBudgetBites fails: a MapFS holding
                 each of options, plan, quick, ship and spec at 15,001 B reports all five
                 if the embedded set regresses, TestPromptByteBudget fails on the real FS (every base prompt ≤ 15,000)

Wave 5
  t-17 Record after legs; gate flow cost
       (depends t-1, t-16)
       files:    .dross/phases/prompt-budgets-rest/flow-cost.toml, .dross/phases/prompt-budgets-rest/flow-cost-before.toml,
                 internal/cmd/flowcost_record_test.go
       covers:   c-4, c-2
       desc:     Preconditions: `make install` at the phase head, then `dross prompt partial plan panel` must succeed
                 on the installed binary (r-01). Re-run plan, quick and spec on the same fixture trees. Move t-1's legs
                 in as [before]/[after] (plan), [quick_*] and [spec_*], plus bytes-only [ship_bytes] and
                 [options_bytes], then delete flow-cost-before.toml. Add promptBudgetsRestFlowProblems (returns
                 problems) and register the phase in TestFlowCostRecords.
       contract: if a full after leg fetched, the test fails: partial_fetches != 0 on [after], [quick_after] or
                 [spec_after] (observed, never edited)
                 if an after prompt is over the cap or not smaller, the test fails: after prompt_bytes > 15000, or
                 ≥ before, on all five
                 if the after leg added a round-trip, checkLegPair fails: any after dross_calls entry absent from its
                 before (e.g. `dross prompt partial`)
                 if turns get gated (locked flow_set), TestPromptBudgetsRestFlowProblems fails: a synthetic record with
                 after agent_turns at 2× before reports zero problems
                 if a bytes-only leg is held to turns, the same test fails: [ship_bytes] with no agent_turns reports
                 zero problems, while a [spec_after] with agent_turns = 0 reports one
                 if the record pins live sizes, the test fails: the check never reads assets.FS sizes, only recorded
                 numbers, so a later ship.md edit cannot break it
                 if flow-cost-before.toml lingers, the stat check fails
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (every base prompt ≤ 15,000; enumerated; partials exempt) | t-2, t-9, t-10, t-11, t-12, t-14, t-15, t-16 |
| c-2 (zero fetches on the five common paths) | t-4, t-10, t-11, t-12, t-13, t-14, t-15, t-17 |
| c-3 (existing prompt tests run on prompt + partials) | t-3, t-5, t-6, t-7, t-8, t-9, t-10, t-11, t-12, t-13, t-14, t-15 |
| c-4 (before/after bytes, turns, wall-clock) | t-1, t-17 |

All 4 criteria are covered.

## Judgment calls

- **Temporary pending set.** Chose a shrink-only overCapPending set in wave 1, deleted at t-16. Rejected landing the glob test only at the end: until then, every other prompt, including milestone.md 313 B under the cap, would stay unguarded, and no split would prove its own cap at its commit. The set is temporary, and t-16's five-name 15,001 B contract proves no exemption survives (`no_exceptions`).
- **Shared budget_test.go edits.** t-10, t-11 and t-12 each delete one line of budget_test.go in the same wave. Accepted: each deletes only its own entry. The rejected alternative was serialising the three splits for a one-line conflict.
- **Manifest recorder.** Chose append-only recording of the five prompts in wave 1, before any prompt edit. Rejected a full re-record, which would drop the execute rows `execute begin` already absorbed.
- **Reader migration ahead of the split.** Readers move to Expand in four wave-1 tasks that precede their prompt's split. Rejected migrating inside each split task: a negative assertion would read the bare prompt while the text moves, so the split commit would be the one where it silently passes. Rejected one 15-file migration task as too large. The cost is four tasks sharing the allowlist pin, each lowering it by its own count.
- **Atomic quick absorb.** Chose absorbing quick §6 into the already-running `quick end`, together with deleting the §6 prose in the same task (t-9). Rejected the predecessor's split of the Go change from the prompt change: a /dross-quick run on this branch between the two commits would bump twice. Chose a HEAD-moved check, using the head `quick begin` already records, over a `--committed` flag: a flag depends on the prompt passing it, and abort paths run `quick end` unconditionally.
- **Absorb before split.** t-9 precedes t-14 so the absorbed deletions give the fetch lines headroom under TestTestStepPromptsNotGrown's 18,102 B (the predecessor's B2 lesson).
- **Ship provider commands stay inline, compressed.** Rejected a `provider` trigger, because it would make every Forgejo or GitLab repo's common path fetch. Rejected absorbing provider commands into `dross ship` output: it adds an auth-header and token-adjacent print surface to ship.go to save about 1 KB.
- **Ship recovery trigger.** Recovery is keyed on a new `merge=failed` rather than reusing `preflight=recovery`. It fires after the merge, not at pre-flight, and its fetch line ends §6 so the inlined `## Recovery` doesn't split a section.
- **Plan panel fetch-line position.** It sits at §2P's old position, between §2 and §2G, not in §0's flag parse. Expand then reproduces the original heading order, so section-scoped plan tests keep their meaning. It still fetches only under --panel.
- **Spec create flow stays inline.** milestone.md routes every new phase through `/dross-spec --new`, so create is the common path. Only empty-target resolution, resume and post-"defer it" destinations move. The step-1 include-first gate stays inline, because a defer pick is the less common outcome by design.
- **Options split boundary.** The split is on Keep vs Change inside §14, not by section. "An options walk" must fetch zero even when the user picks §14. Every key name stays inline, so a Keep walk can still show current values.
- **Bytes-only ship and options legs.** Ship and options get bytes-only record tables, and the flow-cost check reads only recorded numbers. Rejected comparing after-bytes against live assets.FS sizes: the next phase that edits ship.md would break this phase's record.
- **Quick flow window.** Quick flow_end is the bookkeeping commit, not the work commit. Otherwise the absorbed §6 falls outside the measured window, and c-4's before/after can't show it.
- **Before-leg isolation.** Every prompt-editing task depends on t-1. Splits don't consume t-1's output, but running them concurrently would change what the before legs load through the `~/.claude` symlink. That is R10, and t-1's exact prompt_bytes pins catch it.
