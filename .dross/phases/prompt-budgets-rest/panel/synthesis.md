# prompt-budgets-rest: panel synthesis

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| risk (17 tasks / 5 waves) | All 4 covered, and it has the most complete cap-feasibility reasoning (quick 18,102 headroom, spec three-branch cut). c-1 is compromised by a wave-1 `overCapPending` exemption set, which the locked `no_exceptions` decision forbids. The vacuity floor "fewer than 25 base prompts" fails today: there are 24. | Strongest on behaviour: httptest board counters, abort/idempotency/board-500 arms for `quick end`, and section-integrity and moved-not-copied guards. One gap: t-9 misses `protected_prompts_test.go`. `TestQuickClosesBoardIssueBeforeLeavingTheQuickBranch` anchors on ``dross issue quick $NEW_VERSION "quick:``, which §6 deletion removes. | Mostly ≤5 files. t-16 is a one-file, one-map deletion that only exists because of the pending set (too small). | Sound: every prompt edit is gated on t-1, and readers migrate ahead of the splits. t-10, t-11 and t-12 all edit budget_test.go in the same wave (a conflict the draft acknowledges). |
| mvp (8 tasks / 3 waves) | All 4 nominal, but c-2 is exposed three ways: `provider=other` makes every Forgejo/GitLab ship fetch, `local_settings=picked` fetches on any §14 visit, and `target=unresolved` puts `--new` on a fetch. c-3 has no conservation manifest. | Specific where present, but t-3's own `TestTestStepPromptsNotGrown` contract can't pass as planned. Expanded quick.md is 18,098 B against an 18,102 B cap, Expand keeps each fetch line, and t-3 deletes nothing, so two new fetch lines trip it. The c-4 checker has no break-one-field test. | t-2 spans 15 files (the plan prompt's own rule splits at 5+). t-8 merges the budget test with the after-legs (two layers). | Simple and correct dependencies. Per-split vocabulary edits put partials.go in four parallel wave-2 tasks. |
| verification (16 tasks / 4 waves) | All 4 covered, and it honours `no_exceptions` (budget lands last). It alone closes the raw-read guard's real blind spots (confirmed: `docsBody`/`readDoc`/`docText` variadic helpers read options.md and ship.md raw, and `cmd/dross/main_test.go:85` reads ship.md raw). Its spec cut is likely infeasible: §4a step 2 is about 1.5 KB of the 4.1 KB needed, and the remaining 2.6 KB would have to come from compressing a 3.4 KB §0 and a 1.3 KB Hard rules. | Strongest overall: ideal contracts written first, the append-only manifest test, registry-derived `splitPrompts`, and the break-one-field c-4 checker table. Its vacuity floor "more than 30 base prompts" is wrong (24). Its quick-begin route absorption contracts are thorough. | 16 tasks, each ≤5 files, with migrations grouped by prompt. | Clean guards → migrations → splits → budget/after order. The migrations depend on t-4 only through the shared allowlist pin, so this is serialisation, not data flow. |

**Skeleton: verification.** It is the only draft that is both faithful to every locked decision (`no_exceptions`, `branch_placement`, `flow_set`) and complete on guards. Every one of its file and blind-spot claims checked out against the repo. Its two weaknesses are the spec cut boundary and the vacuity floor. Both are fixed by grafts from risk and mvp.

## Merged plan

```
Phase prompt-budgets-rest — 16 tasks across 4 waves

Wave 1
  t-1  Capture before legs on base build   [verification+risk+mvp]
       files:    .dross/phases/prompt-budgets-rest/flow-cost-before.toml
       covers:   c-4
       desc:     Build the base dross in a scratch worktree at 8eb38e9f with `go build` there. Never run `dross test` or
                 `make install` from the worktree, because rsync clobbers helicon's .git. binary_version must name
                 8eb38e9f. Rebuild the fixtures from prompt-partials' inline setup_sh and add one `phase` step per leg:
                   - plan leg: a flow-probe phase with a locked spec and no plan
                   - spec leg: a scaffolded spec-probe phase with no spec.toml (`/dross-spec <id>`, target given)
                   - quick leg: a pair quick on the flow repo with no origin (direct route)
                 The legs are human-driven:
                   - /dross-plan <id> (no --panel), up to its `state touch "plan locked` call
                   - /dross-spec <id>, up to `state touch "spec locked`
                   - pair /dross-quick, up to the `chore(dross): record quick` bookkeeping commit. flow_end is the
                     bookkeeping commit, so t-5's absorbed §6 falls inside the measured window. [risk]
                 This must run while every prompt is unedited, because ~/.claude/dross/prompts is symlinked to this
                 working tree. That is why every prompt-editing task depends on t-1.
                 Record [before] (plan), [quick_before] and [spec_before] with the prompt-partials leg fields:
                 prompt_bytes, partial_fetches, agent_turns, turn_tools, wall_clock_s, human_wait_s, dross_calls,
                 binary_version, session_id and fixture_tree.
                 Record [bytes_only] ship_before = 23006 and options_before = 28231 from
                 `git show 8eb38e9f:assets/prompts/<p>.md | wc -c`. These have no session (locked flow_set).
       contract: if a leg ran on edited prompts, t-16's TestFlowCostRecords/prompt-budgets-rest fails:
                   [before].prompt_bytes != 21154, [quick_before] != 18098, [spec_before] != 19075, or
                   [bytes_only] ship_before != 23006 / options_before != 28231
                 if a before leg ran on a phase-built binary (for example one carrying t-5's `quick end` absorption),
                   the check fails: each before binary_version must contain "commit 8eb38e9f"
                 if the fixture is non-deterministic, the check fails: each *_after.fixture_tree must equal its
                   *_before.fixture_tree (`git rev-parse HEAD^{tree}` recorded per leg)

  t-2  Extend conservation manifest to five prompts   [verification+risk]
       files:    assets/conservation_test.go, assets/testdata/prompt_instructions.txt
       covers:   c-3
       desc:     manifestPrompts gains options, ship, plan, spec and quick. For these five only, the extractor also takes
                 inline backticked spans that start with `dross `, because prose-level steps like
                 `dross protect --check <base>` and `dross local set quick_base <base>` sit outside fences.
                 Recording becomes append-only: DROSS_RECORD_PROMPT_INSTRUCTIONS writes only prompts that have no rows
                 yet, and never rewrites execute.md or verify.md rows. Today's recorder rewrites the whole file from
                 the current Expand, which would drop the three rows `execute begin` already absorbed.
                 Record from today's unedited files, before any prompt edit. Each new prompt gets a minRows floor set
                 at 80% of its recorded count. Lands green.
       contract: if a moved instruction vanishes, TestPromptInstructionsConserved fails naming the row: remove
                   `dross ship <phase-id>` from ship.md and every _ship partial in a MapFS copy of the tree, as
                   TestConservationCatchesALostRow does for verify
                 if a prose-level step is lost, the test fails: deleting the inline `dross local set quick_base <base>`
                   span from quick.md, with no absorbs marker, is reported as "instruction lost"
                 if re-recording rewrites existing rows, TestManifestRecordIsAppendOnly fails: recording with execute.md
                   rows present leaves them byte-identical, including the three `execute begin` absorbed rows
                   (`dross state set version <major>.<minor>.$PATCH.0`, `dross issue phase sync <id> --status in-progress`,
                   `dross issue task pull <phase>`)   [risk]
                 if the manifest was recorded after §6 moved, the pinned-row check fails: quick.md rows must include
                   `dross state bump internal` and `dross issue quick $NEW_VERSION "quick: <one-line summary>"`   [risk]
                 if the manifest goes vacuous, the test fails: fewer rows than minRows for any of the five

  t-3  Add branch facts; pin common paths and sections   [verification+risk]
       files:    assets/partials.go, assets/partials_test.go
       covers:   c-2, c-3
       desc:     TriggerVocabulary gains nine keys, each listing its common-path value first:
                   - panel{off,on}
                   - auto{off,on}
                   - ci{passed,failed}
                   - merge{clean,failed}
                   - target{given,empty}
                   - spec_file{absent,present}
                   - deferral{none,chosen}
                   - lanes{unchanged,editing}
                   - remote{unchanged,changing}
                 Quick reuses mode{pair,solo}.
                 TestCommonPathInline gains raw-base needles. All of them pass today (checked against 8eb38e9f):
                   - plan: `## 2G`, `## 3. Propose`, `## 5. Write plan.toml`, `### 6.1 Auto review`,
                     `dross state set current_phase_status "planned"`
                   - spec: `Step 1 — the entry gate`, `dross phase create`, `dross phase list --milestone`,
                     `Describe a new phase`, `### 3b.`, `[[criteria]]`, `dross state touch "spec locked: <id>"`,
                     `Resume, don't overwrite`, `already has a target`
                   - quick: `dross quick begin "<description>"`, `**Red, pair mode**`, `dross quick end`, `## 5. Commit`,
                     `Pair mode by default`
                   - ship: `## 1. Preview`, `## 3. Reviewers`, `## 3.5 Merge landmarks`, `dross ship <phase-id>`,
                     `gh run watch`, `never gate on \`gh pr checks --watch\``, `dross ship recover`, the §6 merge
                     AskUserQuestion
                   - options: `## Section pick`, `Keep · Change · Skip`, every `## N.` heading 1–17,
                     `dross local set <key> "<value>"`, `dross local get`, `dross mutation remote status`,
                     `dross trust --check`, `Never run it on their behalf`, `dross test lane list`
                 TestExpandIdentity is retargeted from quick (about to split) to techdebt.md, which never splits.
                 Three guards are added:   [risk]
                   - TestCommonPathVocabularyPinned
                   - TestPartialTextMovedNotCopied (passes today: 0 shared lines of 40+ chars across all 10 existing
                     partials)
                   - TestPartialSectionsStayWhole
       contract: if a new key's common value fetches, TestCommonPathFetchesNothing fails: synthetic `panel=off`,
                   `auto=off`, `ci=passed`, `merge=clean`, `target=given`, `spec_file=absent`, `deferral=none`,
                   `lanes=unchanged` and `remote=unchanged` fetch lines are each rejected as common path
                 if a key's values are reordered (for example lanes → {editing, unchanged}),
                   TestCommonPathVocabularyPinned fails: CommonPath() must equal the pinned map (mode:pair,
                   preflight:ok, …, panel:off, ci:passed, merge:clean, lanes:unchanged, …)   [risk]
                 if a flow's landmark leaves its base prompt, TestCommonPathInline (raw, via assets.FS) fails naming the
                   file and the needle
                 if the identity check targets a split prompt, TestExpandIdentity fails: it asserts
                   FetchPoints(techdebt.md) is empty before comparing bytes
                 if a partial copies text instead of moving it, TestPartialTextMovedNotCopied fails: any partial line of
                   40+ chars also present verbatim in its base prompt   [risk]
                 if an inlined partial's `## ` heading lands above remaining base text of the fetch line's section,
                   TestPartialSectionsStayWhole fails (synthetic MapFS)   [risk]

  t-4  Close raw-read guard blind spots   [verification]
       files:    internal/cmd/prompt_expand_test.go, internal/cmd/lane_install_docs_test.go,
                 internal/cmd/lane_toolchain_docs_test.go, internal/cmd/milestone_narration_test.go, cmd/dross/main_test.go
       covers:   c-3
       desc:     Three changes to the guard:
                   - splitPrompts is derived from assets.PartialNames: every prompt with at least one partial, instead of
                     the fixed {execute, verify}.
                   - Raw-read detection also matches any call whose literal args spell "assets","prompts" (docsBody,
                     readDoc, docText, variadic parts). Today isRawPromptRead only sees os.ReadFile and readRepoFile.
                   - Both walks extend from internal/ to cmd/. internal/rules is already inside internal/.
                 The readers this newly catches are migrated in the same task, so the allowlist does not grow:
                   - lane_install docsBody(options.md) → expandedPrompt
                   - lane_toolchain readDoc(options.md) → expandedPrompt
                   - milestone_narration docText(ship.md) → expandedPrompt
                   - cmd/dross main_test ship.md → assets.Expand(assets.FS, "ship")
       contract: if a variadic helper reads a prompt raw, TestRawPromptReadDetection fails: `docsBody(t, "assets",
                   "prompts", "options.md")` and `readDoc(t, "assets", "prompts", "ship.md")` must be detected
                 if splitPrompts stays a fixed list, TestSplitPromptsFollowTheRegistry fails: on a MapFS adding
                   _plan.panel.md, plan.md is a split prompt without any list being edited
                 if cmd/dross is outside the walk, the meta-guard fails: a raw os.ReadFile of
                   "../../assets/prompts/ship.md" in cmd/dross/main_test.go is reported
                 if the allowlist grows to absorb the new detections, the pin check fails: rawPromptReadAllowlistSize
                   stays at 35 or lower

Wave 2
  t-5  Absorb quick §6 bookkeeping into quick end   (depends t-1, t-2)   [risk]
       files:    internal/cmd/quick.go, internal/cmd/quick_test.go, assets/prompts/quick.md,
                 internal/cmd/protected_prompts_test.go, cmd/dross/testdata/cli_tree.txt
       covers:   c-1, c-3
       desc:     `quick end` already runs at the end of §5 (quick.md:177), so §6 is "a CLI call already running at that
                 point" (locked branch_placement). When HEAD has moved past the head recorded by `quick begin`
                 (gatestate.Quick.Head), `quick end` does all of this:
                   - bumps internal
                   - records changes quick-<n> when in-phase and the phase is not shipped. files = begin..HEAD minus
                     .dross/
                   - runs state touch and issue quick on the description's first line
                   - prints `version: <v>`
                 Delete the §6 prose in the same commit, so there is no interim double bump. quick.md keeps:
                   - the bookkeeping commit (`chore(dross): record quick <NEW_VERSION>`), with NEW_VERSION taken from
                     the `version:` line
                   - the `board: FAILED` retry `dross issue quick <version> "<summary>"`
                   - "missing `version:` = stale binary, stop, ask for make install"
                 Add one `// absorbs:` marker per moved t-2 manifest row. Retarget
                 TestQuickClosesBoardIssueBeforeLeavingTheQuickBranch: its open-needle
                 ``dross issue quick $NEW_VERSION "quick:`` is deleted with §6. The open is now the `quick end` call, and
                 the test keeps the open < close < `dross checkout <base>` ordering. Post-commit: `make install` (r-01).
       contract: if an abort bumps, TestQuickEndAbortNoBump fails: HEAD == begin head → state.version unchanged, no
                   changes.json write, zero board requests (httptest counter)
                 if a missing quick record bumps, the test fails: `quick end` with no record exits 0 and writes nothing
                 if a re-run double-bumps, TestQuickEndIdempotent fails: a second end after success leaves version +1 once
                 if the bump disagrees with the PR-route branch, the test fails: the new version must equal begin's
                   state.version with internal+1 (the `quick/<preview-version>` name)
                 if a shipped phase records changes, TestQuickEndShippedIsStandalone fails: status=shipped → bump, no
                   changes record
                 if a board 500 loses the bump, TestQuickEndBoardFailureKeepsBump fails: version and changes.json
                   persisted, `board: FAILED` plus the retry command printed, non-zero exit distinct from a usage error,
                   quick record removed
                 if .dross/ files leak into the record, the test fails: a commit touching .dross/state.json and a.go
                   records only a.go
                 if a moved row loses its claim, TestPromptInstructionsConserved fails: deleting the
                   `// absorbs: dross state bump internal` marker reports the row lost
                 if quick.md loses its board call, board_switch_prompt_test's 6-caller floor and the
                   TestBoardCallingPromptsSurfaceFailure sentence check fail
                 if the issue close drifts after the base switch, TestQuickClosesBoardIssueBeforeLeavingTheQuickBranch
                   (retargeted) fails

  t-6  Route spec prompt readers through Expand   (depends t-4)   [verification]
       files:    internal/cmd/spec_prompt_test.go, internal/cmd/interaction_pilot_test.go, internal/cmd/phase_test.go,
                 internal/rules/rules_test.go, internal/cmd/prompt_expand_test.go
       covers:   c-3
       desc:     specPromptContent, TestSpecPilotUsesEmitter, TestPhaseListPromptConsumersAreCurrent and the rules
                 package's TestSpecPromptDefersToSnippet read the expanded spec.md. The rules package uses
                 assets.Expand(assets.FS, "spec"), since the internal/cmd helpers aren't in scope there. Remove 4
                 allowlist entries and lower the pin by 4.
       contract: if a spec reader stays raw, TestNoAllowlistedReaderReadsASplitPrompt fails the moment t-11 lands
                   spec's first partial (splitPrompts is registry-derived since t-4)
                 if a loader skips partials, a TestPromptLoadersSeePartials-style check fails: a BANNED phrase only in a
                   MapFS _spec.route.md is seen by specPromptContent
                 if an entry is removed without lowering the pin (or vice versa), TestPromptReadsGoThroughExpansion fails

  t-7  Route plan and ship readers through Expand   (depends t-4)   [verification]
       files:    internal/cmd/plan_prompt_test.go, internal/cmd/plan_review_tiers_test.go,
                 internal/cmd/interaction_coreloop_test.go, internal/cmd/ship_prompt_test.go, internal/cmd/prompt_expand_test.go
       covers:   c-3
       desc:     These read Expand:
                   - planPromptContent
                   - TestPlanPromptDescribesEveryReviewTier
                   - TestPlanPromptNoArtifactDump
                   - TestShipPromptDecisionTurnsSeparate
                   - rawShipSection (it keeps "not normalised" but reads Expand(ship)), plus its raw read at :456
                 A check that genuinely needs the base only (footer, common path) switches to assets.FS. Remove the
                 matching allowlist entries and lower the pin.
       contract: if a plan or ship reader stays raw, TestNoAllowlistedReaderReadsASplitPrompt fails once t-10 or t-12
                   adds the first partial
                 if the tier test reads the base only, TestPlanPromptDescribesEveryReviewTier still finds the `--panel`
                   tier row when §2P lives in _plan.panel.md (proven against a MapFS move)
                 if rawShipSection stays raw, the Forgejo `{"Do":"squash"}` match in §6 must still pass when the seeded §6
                   text lives only in a _ship partial   [risk]
                 if ship's merge-gate decision-turn check loses the partial text, TestShipPromptDecisionTurnsSeparate
                   (expanded) fails

  t-8  Route quick prompt readers through Expand   (depends t-4)   [verification+risk]
       files:    internal/cmd/quick_prompt_test.go, internal/cmd/squash_claim_guard_test.go,
                 internal/boardsync/board_lifecycle_divergence_test.go, internal/cmd/prompt_expand_test.go
       covers:   c-3
       desc:     quickPromptContent and TestQuickPromptHandlesShippedWindow read the expanded quick.md. boardsync
                 rawPrompt (the quick/milestone/ship emission lanes) reads assets.Expand, keeping its own normalisation.
                 Remove the matching entries and lower the pin.
       contract: if a quick reader stays raw, TestNoAllowlistedReaderReadsASplitPrompt fails once t-13 adds _quick.solo.md
                 if a loader skips partials, TestPromptLoadersSeePartials (extended) fails: a phrase seeded only in a
                   MapFS _quick.solo.md must reach quickPromptContent   [risk]
                 if an emission lane's text moves into a partial unseen, TestEmittedStatusesAreTheLifecycleSet (expanded)
                   still sees every status ship.md and quick.md emit: a MapFS partial holding the only `--status shipped`
                   keeps it in the set

  t-9  Route options prompt readers through Expand   (depends t-4)   [verification+risk]
       files:    internal/cmd/options_docs_test.go, internal/cmd/readme_doc_test.go, internal/cmd/interaction_setupcmds_test.go,
                 internal/cmd/interaction_snippet_test.go, internal/cmd/prompt_expand_test.go
       covers:   c-3
       desc:     optionsPrompt, readme_doc optionsSection (:244) and setupPromptBody (options/quick) read Expand. The
                 interaction_snippet options.md literal (:191) stays a raw-by-design footer read via assets.FS. Remove the
                 matching entries and lower the pin. After t-6..t-9 the pin is 20 (35 − 15 migrated readers), or 19 if
                 TestFooterPromptsLoadPlaybook leaves the allowlist by moving to assets.FS.
       contract: if an options reader stays raw, TestNoAllowlistedReaderReadsASplitPrompt fails once t-14 adds
                   _options.lanes.md
                 if setupPromptBody stays raw, a phrase seeded only in _quick.solo.md or _options.lanes.md must reach it   [risk]
                 if lane docs leave the loader's view, options_docs lane/selector/toolchain tests (expanded) fail when
                   _options.lanes.md loses `--selector-template`, `--empty-exit` or `trusted_lane_installs`
                 if the README option rows drift, readme_doc optionsSection still matches every options.md section heading
                   (raw headings stay in base per t-3)
                 if the allowlist ends at the wrong size after t-6..t-9, TestPromptReadsGoThroughExpansion's pin check
                   fails   [risk]

Wave 3
  t-10 Move plan panel mode to a partial   (depends t-1, t-2, t-3, t-7)   [verification+risk]
       files:    assets/prompts/plan.md, assets/prompts/_plan.panel.md, internal/cmd/plan_prompt_test.go
       covers:   c-1, c-2, c-3
       desc:     §2P, 2P.1, 2P.2 and 2P.3 (5,596 B) move to _plan.panel.md. plan.md keeps a one-line stub at §2P's old
                 position, after §2 and before `## 2G`: **When** `panel=on` — --panel was passed:
                 `dross prompt partial plan panel`. §0's flag parse keeps naming --panel but names the fetch condition
                 instead of "see §2P". Compress Hard rules and §6.1 to reach ≤14,500 B raw, under the conservation
                 guard. Post-commit: `make install` (r-01). Report the measured raw bytes.
       contract: if panel text loads on the plain path, TestPlanPanelFetchPoint fails: Fetched(plan, CommonPath()) is
                   empty and Fetched(plan, {panel: on}) == [panel]
                 if the fetch line moves, the test fails: it must be the last line before `## 2G` in raw plan.md, and
                   Expand(plan) must keep heading order 2 → 2P → 2G   [risk]
                 if base plan.md still says "see §2P", the test fails (a dangling ref to a heading absent from the base)   [risk]
                 if the lens planner brief is lost, plan_prompt panel tests (expanded) fail when _plan.panel.md drops
                   `panel/synthesis.md` or one of risk/mvp/verification
                 if the non-panel decomposition leaves base, TestCommonPathInline (raw) fails on `## 3. Propose` / `## 2G`
                 if the clear-point footer leaves base plan.md, TestBoundaryPromptsCarryFooter fails (footer_coverage
                   reads base raw)   [mvp]
                 if a fenced row is dropped while compressing, TestPromptInstructionsConserved names it

  t-11 Move spec no-target, resume and route branches   (depends t-1, t-2, t-3, t-6)   [risk+verification]
       files:    assets/prompts/spec.md, assets/prompts/_spec.no-target.md, assets/prompts/_spec.resume.md,
                 assets/prompts/_spec.route.md, internal/cmd/spec_prompt_test.go
       covers:   c-1, c-2, c-3
       desc:     These branches move to partials:
                   - the empty-$ARGUMENTS resolution (done-phase / unset asks) → _spec.no-target.md behind `target=empty`
                   - extend/replace of an existing spec.toml → _spec.resume.md behind `spec_file=present`. §0 step 4 keeps
                     its lead sentence ("Read … spec.toml if it exists. **Resume, don't overwrite.**") with the fetch line
                     under it.
                   - §4a step 2 destinations (backlog / named phase / someday, the `dross milestone add` /
                     `dross deferred route` fences and the <idx> rule) → _spec.route.md behind `deferral=chosen`, meaning
                     the user picked "defer it"
                 These stay inline:
                   - the Create flow (the common `--new` path, roadmap picklist included)
                   - §3
                   - the step-1 include-first gate
                   - §4's "skip any deferred item that already has a target"
                   - the `Spec locked` footer
                 Compress about 1.3 KB under the conservation guard to ≤14,500 B raw. Post-commit: `make install`.
                 Report the measured raw bytes.
       contract: if a spec branch fetches on the common path, TestCommonPathFetchesNothing fails. Each key's
                   non-common value fetches exactly its own partial (CheckPartials orphan check, plus a Fetched table in
                   spec_prompt_test: {target: empty} == [no-target], {spec_file: present} == [resume],
                   {deferral: chosen} == [route])
                 if route verbs are lost, TestPromptInstructionsConserved fails on
                   `dross deferred route <phase-id> <idx> --target "<slug>"`
                 if routing or gray-area instructions are trimmed away, the expanded TestSpecPrompt* suite fails, e.g.
                   TestSpecPromptRoutesFourDestinations and TestSpecPromptWalksEveryGrayArea   [mvp]
                 if the resume skip rule leaves §4, a raw §4 check fails: base §4 must contain "already has a target"
                 if the include-first gate or the create flow leaves base, TestCommonPathInline (raw) fails on
                   `Step 1 — the entry gate`, `add to current phase`, `dross phase list --milestone` or
                   `Describe a new phase`
                 if the guarded checkout leaves the expansion, TestNoPromptPerformsRawBranchSwitch fails on spec   [mvp]
                 if the `Spec locked` footer moves, TestBoundaryPromptsCarryFooter fails

  t-12 Move ship auto, recovery, CI-failure branches   (depends t-1, t-2, t-3, t-7)   [verification+risk]
       files:    assets/prompts/ship.md, assets/prompts/_ship.auto.md, assets/prompts/_ship.recovery.md,
                 assets/prompts/_ship.ci-failure.md, internal/cmd/ship_prompt_test.go
       covers:   c-1, c-2, c-3
       desc:     Four changes:
                   - §0.5 --auto (1,506 B) moves to _ship.auto.md behind `auto=on`. The fetch sits right after the flag
                     parse.
                   - `## Recovery` (3,487 B) moves to _ship.recovery.md behind `merge=failed`. Its fetch line is the last
                     line of §6, so the inlined `## Recovery` heading doesn't split a section, and §6's "see Recovery"
                     reference stays local.   [risk]
                   - §5 "On failure", including `gh run view --log-failed` and the 3-iteration cap, moves to
                     _ship.ci-failure.md behind `ci=failed`.
                   - The DEFERRED subagent-panel stub (313 B) is deleted.   [risk]
                 These stay inline:
                   - the GitHub watch steps
                   - the never-gate-on-`gh pr checks --watch` rule
                   - §6's merge gate and "a non-zero exit is a board failure"
                   - §0's `dross ship recover` hint
                   - the footers
                   - Forgejo/GitLab polling, compressed along with the two-merge-levels callout
                 Target ≤14,500 B raw. Post-commit: `make install`. Report the measured raw bytes.
       contract: if an interactive ship fetches, TestShipCommonPathFetchesNothing fails: Fetched(ship, CommonPath()) is
                   empty, and each of auto=on, merge=failed and ci=failed fetches exactly its own partial
                 if recovery's heading splits §6, TestPartialSectionsStayWhole (t-3) fails: the fetch line must end §6   [risk]
                 if recovery steps are lost, TestShipPromptRecoverySection (expanded) fails on any of the six named
                   failure modes   [mvp]
                 if CI fix guidance is lost, the expanded ship CI tests and TestShipPromptReShipsAfterEveryCIFix fail when
                   _ship.ci-failure.md drops `gh run view --log-failed` or the "re-run `dross ship <phase-id>`" step
                 if --auto starts merging, TestShipPromptAutoFastPath / TestShipPromptAutoBackfill (expanded) fail when
                   _ship.auto.md loses "returns without merging"
                 if a non-GitHub provider's CI poll or squash call is lost while compressing, TestShipPromptGitLabSections
                   (expanded) fails   [mvp]
                 if `dross ship recover` or the `gh pr checks --watch` rule leaves base, TestCommonPathInline (raw) fails
                 if §6 loses "a non-zero exit is a board failure", TestBoardCallingPromptsSurfaceFailure fails   [risk]
                 if a raw branch switch sneaks in via a partial, TestNoPromptPerformsRawBranchSwitch (expanded, walks
                   partials) fails
                 if the `state is on disk` footer moves, TestBoundaryPromptsCarryFooter fails

  t-13 Move quick solo text to one partial   (depends t-1, t-2, t-3, t-5, t-8)   [risk+verification]
       files:    assets/prompts/quick.md, assets/prompts/_quick.solo.md, internal/cmd/quick_prompt_test.go,
                 internal/cmd/execute_prompt_test.go
       covers:   c-1, c-2, c-3
       desc:     Precondition: `make install` with t-5 live. These move to _quick.solo.md:
                   - the §2 solo skip
                   - the §4 solo red path and the Solo review block
                   - the no-test solo arm
                   - the §5 `solo: yes` line and the review-gated commit
                   - the solo-only hard rules
                 It is fetched once, right after the `dross quick begin --solo` fence (`mode=solo`, locked
                 branch_placement). The refused-solo-begin stop stays inline.
                 §4 keeps a `**Re-fetch** if context was compacted:` line between the green `dross test` and
                 `## 5. Commit`. "Pair mode by default" stays in the base Hard rules. t-5's §6 deletions are the headroom
                 for the fetch lines under TestTestStepPromptsNotGrown. Post-commit: `make install`. Report the measured
                 raw bytes (≤15,000).
       contract: if the solo fetch drifts from the begin (locked branch_placement), TestQuickSoloFetchPlacement fails: the
                   `quick solo` When line must be the first fetch point after the `dross quick begin --solo` fence in raw
                   quick.md
                 if solo review order breaks, the test fails: inside the partial, re-run `dross test` < `dross review
                   context` < spawn < `dross review status` < commit, and the base re-fetch line sits between green
                   `dross test` and `## 5. Commit`. TestQuickSoloReview and TestQuickSoloReviewBlocked (expanded via t-8)
                   keep passing
                 if the refusal stop moves into the partial, a raw assets.FS check (TestQuickSoloBeginNeedsReviewer, new)
                   fails
                 if fetch lines push the expansion past its base size, TestTestStepPromptsNotGrown (expanded quick ≤ 18,102)
                   fails
                 if pair runs fetch, Fetched(quick, CommonPath()) is empty
                 if the shipped-phase window is lost, TestQuickPromptHandlesShippedWindow (expanded) fails   [mvp]
                 if the PR route loses `git push -u origin quick/` or `gh pr create --base`, TestQuickPRRouteCommands
                   (expanded) fails

  t-14 Move options lane and remote lore out   (depends t-1, t-2, t-3, t-9)   [verification+risk]
       files:    assets/prompts/options.md, assets/prompts/_options.lanes.md, assets/prompts/_options.remote.md,
                 internal/cmd/options_docs_test.go
       covers:   c-1, c-2, c-3
       desc:     Two moves out of §14, which is 15,317 B:
                   - Lane declaration, prepare, install, selector, template, empty-exit, toolchain and per-lane grant text
                     (about 10.3 KB) moves to _options.lanes.md behind **When** `lanes=editing`: the user chose Change on
                     test lanes.
                   - Grant/revoke and tuning-knob text (about 2.6 KB) moves to _options.remote.md behind
                     `remote=changing`.
                 §14 keeps these inline:
                   - every local.toml key name
                   - the current-state reads: `dross local get`, `dross mutation remote status`,
                     `dross trust --check` (the consent rule) and `dross test lane list`
                 All 17 section headings stay. Compress to ≤14,500 B raw. Post-commit: `make install`. Report the
                 measured raw bytes.
       contract: if the consent rule moves out, TestCommonPathInline (raw) fails on "Never run it on their behalf"
                 if a local.toml key name leaves base §14, TestOptionsNamesEveryLocalKeyInline (new) fails on a raw §14 read:
                   quick_base, allow_hosts, mutation_workers, mutation_test_cpu, remote_scratch_base, mutation_remote_env,
                   mutation_remote_host, mutation_remote_workdir, trusted_test_command   [risk]
                 if a local key loses its docs, TestOptionsCoversEveryLocalKey (expanded) fails naming the key   [mvp]
                 if the §14 heading or numbering breaks, TestOptionsSectionNumbersAreContiguous fails on raw options.md   [mvp]
                 if lane lore is lost, lane_install_docs and lane_toolchain_docs tests (expanded since t-4) fail when
                   _options.lanes.md drops `dross test lane install <name> [--apply] [--local]` or exit `8`
                 if lane verbs are lost, TestPromptInstructionsConserved fails on
                   `dross test lane add <name> --match <glob> --command "<cmd>"`   [risk]
                 if a walk that only reads settings fetches, TestOptionsCommonPathFetchesNothing fails:
                   Fetched(options, CommonPath()) is empty, and lanes=editing and remote=changing each fetch exactly one
                 if an inert key goes undocumented, inert_config_test's prompt walk (it reads every prompts/*.md including
                   partials) still finds each key's reason

Wave 4
  t-15 Enumerate every base prompt in budget test   (depends t-10, t-11, t-12, t-13, t-14)   [verification+mvp+risk]
       files:    assets/budget_test.go
       covers:   c-1
       desc:     Replace the promptByteBudget map and the exact-set {execute, verify} guard with one
                 `const promptByteCap = 15000` and budgetProblems(fsys fs.FS). budgetProblems globs prompts/*.md, skips
                 any `_`-prefixed name, and takes no per-file argument. TestPromptByteBudget runs it over assets.FS with
                 t.Run per file. There is no exemption list at any point (locked no_exceptions).
       contract: if a prompt is over the cap, TestPromptByteBudget/<file> fails at 15,001 B naming the file and the
                   overage; exactly 15,000 B passes
                 if a new prompt slips in unbudgeted, TestPromptByteBudgetEnumerates fails: a MapFS-only
                   prompts/brand-new.md at 15,001 B is reported
                 if any override exists, the test fails: a MapFS holding options, ship, plan, spec, quick, execute and
                   verify each at 15,001 B reports all seven
                 if a non-cut prompt grows, the test fails: milestone.md (14,687 B today) at 15,001 B is reported   [risk]
                 if partials get budgeted, the test fails: a 40,000-byte _options.lanes.md and a 40,000-byte
                   _interaction.md report nothing
                 if the glob silently matches nothing, the test fails: the real-FS run must visit at least 24 base
                   prompts, execute.md among them (24 is today's count; both "≥25" and ">30" in the drafts fail
                   today)   [mvp+risk]

  t-16 Record after legs; gate flow cost   (depends t-1, t-10, t-11, t-12, t-13, t-14)   [verification+risk]
       files:    .dross/phases/prompt-budgets-rest/flow-cost.toml, .dross/phases/prompt-budgets-rest/flow-cost-before.toml,
                 internal/cmd/flowcost_record_test.go
       covers:   c-2, c-4
       desc:     Preconditions: `make install` at the phase head, then `dross prompt partial plan panel` must succeed on
                 the installed binary (r-01; a stale binary fails on a new partial).   [risk]
                 Rebuild t-1's fixtures and re-drive the three legs identically, with the same flow_end points.
                 Merge the before legs into flow-cost.toml. Add [after], [quick_after] and [spec_after], plus
                 [bytes_only] ship_after and options_after (raw base bytes + zero fetched partial bytes). Then delete
                 flow-cost-before.toml.
                 flowCost gains QuickBefore/QuickAfter/SpecBefore/SpecAfter and BytesOnly. Add
                 checkPromptBudgetsRestFlow (run through checkLegPair for each pair) with
                 promptBudgetsRestFlowProblems returning problems, and promptBudgetsRestBase = "commit 8eb38e9f". Add
                 the phase to the required-phase list in TestFlowCostRecords. Turns and wall-clock are recorded, not
                 gated (locked flow_set).
       contract: if an after prompt is over budget or not smaller, TestFlowCostRecords/prompt-budgets-rest fails: any
                   after prompt_bytes (three legs plus ship/options bytes_only) > 15000 or >= its before
                 if the common path fetched, the test fails: partial_fetches != 0 on [after], [quick_after] or
                   [spec_after] (observed, never edited)
                 if a split added a round-trip, checkLegPair fails: an after leg runs a dross call its before leg
                   didn't, such as `dross prompt partial`
                 if the before pins drift, the test fails: before bytes != 21154/18098/19075 and bytes_only before !=
                   23006/28231
                 if the legs aren't comparable, the test fails: unequal fixture_tree per pair, or the same
                   binary_version on both legs
                 if the checker goes soft, TestPromptBudgetsRestFlowProblems fails: a sound record reports nothing, and
                   each one-field break reports exactly one problem (after over cap, fetch = 1, before off base,
                   options_after missing, tree mismatch, before file lingering)
                 if turns get gated by accident, the same table test fails: after agent_turns = 99 and
                   wall_clock_s = 999 report nothing
                 if a bytes-only leg is held to turns, the same test fails: [bytes_only] ship with no agent_turns
                   reports zero problems, while a [spec_after] with agent_turns = 0 reports one   [risk]
                 if the record pins live sizes, the test fails: the check reads only recorded numbers, never assets.FS
                   sizes, so a later ship.md edit cannot break this phase's record   [risk]
```

### Coverage

| Criterion | Tasks |
|---|---|
| c-1 (every base prompt ≤15,000; enumerated; partials exempt) | t-5, t-10, t-11, t-12, t-13, t-14, t-15 |
| c-2 (zero fetches on the five common paths) | t-3, t-10, t-11, t-12, t-13, t-14, t-16 |
| c-3 (existing prompt tests run on prompt + partials) | t-2, t-3, t-4, t-5, t-6, t-7, t-8, t-9, t-10, t-11, t-12, t-13, t-14 |
| c-4 (before/after bytes, turns, wall-clock) | t-1, t-16 |

All 4 of 4 criteria are covered.

### Graft notes (fact-checks applied in the merge)

- **t-5 file list changed.** Risk's t-9 file list swaps `quick_prompt_test.go` for `protected_prompts_test.go`. Grep found no §6 assertion in quick_prompt_test.go. `TestQuickClosesBoardIssueBeforeLeavingTheQuickBranch` anchors on the §6 issue-open line and would break. Dropping quick_prompt_test.go also removes t-5's file overlap with t-8, so t-5 can run in wave 2.
- **Spec resume needle.** Verification's `Resume, don't overwrite` needle conflicts with risk's resume move. It is kept by leaving step 4's lead sentence in base with the fetch line under it.
- **Shared allowlist pin.** t-6..t-9 all lower the same pin in prompt_expand_test.go, and t-4 edits the same file. Execution serialises them, as verification accepted. All other wave-3 tasks have disjoint files, because t-3 adds the whole vocabulary up front.

## Disagreements

1. **Which quick mechanics go to Go: §6 → `quick end`, §0 routing → `quick begin`, or nothing.**
   - **risk:** absorb §6 (bump, changes record, state touch, issue quick) into the `quick end` that already runs at quick.md:177.
   - **verification:** absorb the §0 route tree (base-branch, protect --check, quick_base) into `quick begin`. It prints a `route:` line, uses httptest provider tests and adds a new quick_route.go.
   - **mvp:** absorb nothing ("moves reach the cap on estimate").
   - **Default:** risk's §6 absorption only (t-5).
   - **Why it matters:** mvp's version is not viable. Expanded quick.md is 18,098 B against `TestTestStepPromptsNotGrown`'s 18,102 B, and Expand keeps every fetch line, so a split with no deletion goes red. Between the two absorptions:
     - §6 sits exactly at a running CLI call (the locked branch_placement wording) and is pure local bookkeeping.
     - The route absorption has to move protect/provider network logic into `begin`. Routing is decided at §0 steps 2–3 and `begin` runs at step 7, so the prompt has to be reordered too.
     - Verification's route absorption is the larger Go change. Its argument is that the PR route is this repo's common standalone path, so a partial there would be wrong. That argument holds, but it is an argument against a *partial*, not against leaving the route inline.
     - Also the quick leg's flow_end changes: risk measures to the bookkeeping commit, verification to the work commit. The default follows risk.
   - **To revisit:** if quick.md lands over 15,000 after t-13, the route absorption (verification t-5) is the next lever. Taking both would add a sixth Go-heavy task, against "speed over ceremony".

2. **When the global budget test lands: wave 1 with a shrink-only exemption set, or last.**
   - **risk:** wave-1 glob test plus a shrink-only `overCapPending` {options, plan, quick, ship, spec}, deleted at the end. Each split then proves its own cap at its commit, and milestone.md (14,687 B, 313 B of headroom) is guarded from day one.
   - **verification and mvp:** land it after all five cuts. Verification calls any temporary list a per-prompt override.
   - **Default:** land last (t-15), with no list.
   - **Why it matters:** locked `no_exceptions` reads "No per-prompt overrides: one 15,000-byte number for every base prompt". A named five-prompt skip list is that override, however temporary.
   - **Cost accepted:** no split commit is test-gated on its own cap. Each split reports measured raw bytes and targets ≤14,500 B instead, and a split that misses is found at t-15, not at its commit. milestone.md is unbudgeted until t-15, which is no worse than today.

3. **Where spec's cut comes from.**
   - **risk:** move empty-target resolution (`target=empty`), extend/replace resume (`spec_file=present`) and the post-"defer it" destinations (`deferred=routed`). Create flow and step 1 stay inline.
   - **verification:** move only §4a step 2 and compress §0 + Hard rules to ≤14,500.
   - **mvp:** move the create flow plus done/unset asks (`target=unresolved`, which includes `--new`) and resume (`preflight=recovery`).
   - **Default:** risk's three-branch move (t-11), using verification's `deferral{none,chosen}` key name.
   - **Why it matters:** spec needs 4,075 B cut. Verification's move is about 1.5 KB, leaving about 2.6 KB of compression from roughly 4.7 KB of §0 + Hard rules, which likely misses the cap. mvp puts `--new` behind a fetch. Both risk and verification argue create is the common path here (milestone.md routes new phases through `/dross-spec --new`), and the fixture leg runs a scaffolded phase, so it would never observe that fetch.
   - **Still open:** risk's call depends on empty-`$ARGUMENTS` being uncommon. If the user habitually runs bare `/dross-spec` on a scaffolded current phase, `target=empty` is on their hot path.

4. **Ship provider text: compressed inline, or a `provider=other` partial.**
   - **mvp:** move Forgejo/Gitea/GitLab steps to `_ship.provider.md`, because without it ship stays about 16 KB.
   - **risk and verification:** keep them inline, compressed. Risk notes that a provider trigger makes every non-GitHub repo's common path fetch.
   - **Default:** inline and compressed.
   - **Why it matters:** c-2 says "ship of a verified phase to PR open" with no provider qualifier, so the mvp key fails c-2 on any Forgejo/GitLab project.
   - **Feasibility is the real risk:** ship needs 8,006 B cut. Auto 1,506 + Recovery 3,487 + stub 313 + On failure (about 1.5 KB) is about 6.8 KB, so roughly 1.2–1.7 KB must come from compression. If t-12 can't reach the cap, the fallbacks are mvp's provider partial or risk's second ship task (t-15: compress the two-merge-levels blockquote and provider lines). Both need a re-decision against c-2.

5. **Options §14: one "picked §14" partial, or a Keep/Change split.**
   - **mvp:** move the whole 15.3 KB §14 body behind `local_settings=picked`.
   - **risk and verification:** split on Change. Lanes go to one partial, remote/grant/tuning to another. Key names and current-state reads stay inline, so a Keep walk through §14 fetches nothing.
   - **Default:** Keep/Change (t-14).
   - **Why it matters:** c-2 says "an options walk" fetches zero. Under mvp, any walk that visits §14 fetches, even to only read values. That reading is the walk itself.
   - **Cost:** two partials and a finer boundary to keep straight, against mvp's single clean cut that alone gets options to about 13 KB.

6. **Whether to extend the conservation manifest at all.**
   - **mvp:** no. c-3 names *existing* tests, and without absorption there are no absorbs markers to anchor.
   - **risk and verification:** yes, recorded from unedited files before any edit, with an append-only recorder. Verification additionally extracts inline `dross ` spans.
   - **Default:** extend (t-2), with verification's extractor.
   - **Why it matters:** the default plan absorbs §6 (disagreement 1), so absorbs markers are needed anyway. The manifest is also the only guard that catches an instruction lost by *compression*, which four of the five splits do. One factual point: today's recorder rewrites the whole file from the current Expand, which would silently drop the three `execute begin` rows, so append-only is required whichever extractor is used.

7. **Raw-read meta-guard: trust it, list the five statically, or derive and widen it.**
   - **risk:** lower the allowlist pin per migration and nothing more.
   - **mvp:** add the five to the static `splitPrompts`.
   - **verification:** derive `splitPrompts` from `assets.PartialNames` and close three blind spots: variadic doc helpers, cmd/dross, and the walk root.
   - **Default:** verification's t-4.
   - **Why it matters:** the blind spots are real. `lane_install_docs_test` (docsBody) and `lane_toolchain_docs_test` (readDoc) read options.md raw, `milestone_narration_test` (docText) reads ship.md raw, and `cmd/dross/main_test.go:85` reads ship.md raw. All are invisible to `isRawPromptRead`, so after the moves they would pass vacuously: exactly the c-3 failure. Correction to verification: internal/rules is already under the `internal/` walk root, so only cmd/ is new.

8. **Reader-migration granularity and timing.**
   - **mvp:** one 15-file task in wave 1.
   - **risk:** four 3–5-file tasks in wave 1, grouped quick / options / plan+spec / ship+cross.
   - **verification:** four tasks in wave 2 after t-4, grouped spec / plan+ship / quick / options.
   - **Default:** verification's four tasks after t-4.
   - **Why it matters:** mvp's task breaks the 5-file rule and is one large red-or-green unit. Running the migrations after t-4 means each one migrates against the widened detector. Migrating first would let a variadic reader slip through. The cost is a serialised pin edit across t-6..t-9 and one extra wave. mvp's "speed" case for a single task is the strongest counter-argument here.

9. **Plan --panel fetch-line position.**
   - **mvp:** in §0's flag parse.
   - **risk and verification:** a one-line stub at §2P's old position, between §2 and `## 2G`.
   - **Default:** §2P position (t-10).
   - **Why it matters:** Expand inlines right after the fetch line. A §0 fetch would put §2P's headings ahead of §1/§2 in the expansion, reordering what section-scoped plan tests read, and would leave §0's "see §2P" pointing at a heading absent from the base. Both are pinned in t-10.

10. **Trigger vocabulary: all up front, or per split; and ship recovery's key.**
    - **mvp:** each split adds its own key ("a key with no fetch line is speculative").
    - **risk and verification:** add the whole vocabulary in wave 1.
    - **Ship recovery:** verification and mvp reuse `preflight=recovery`. risk adds `merge{clean,failed}`, because Recovery fires after the merge step, not at pre-flight.
    - **Default:** up front (t-3), using `merge=failed`.
    - **Why it matters:**
      - Per-split keys put partials.go in all five parallel wave-3 tasks.
      - Up front lets `TestCommonPathVocabularyPinned` and the synthetic common-value rejections land green before any move.
      - `preflight=recovery` would mislabel a post-merge branch with a pre-flight fact. That is misleading for the next reader, though either name fetches correctly.
