# prompt-budgets-rest: verification lens

I started from the test contracts and worked back. For each criterion I first wrote down the test that would prove it. Each task is the smallest change that makes one of those tests satisfiable. All guards land green before any prompt text moves (wave 1). The guards that can only pass once every cut is done (the global budget and the after legs) land last (wave 4).

## Ideal contracts, written first

- **c-1:** A single enumerating budget test over the embedded `assets.FS`, at `prompts/*.md` minus `_*`, with one `15000` constant.
  - It fails on a 15,001-byte prompt with *any* name, including a brand-new one and including each of the five prompts this phase cuts. That proves there is no override.
  - It ignores a 40,000-byte `_x.y.md` file and a 40,000-byte `_interaction.md`.
- **c-2:** Dynamic and static checks together:
  - **Dynamic:** the plan, quick and spec after-legs record `partial_fetches = 0`, as observed.
  - **Static:** for each of the five prompts, `Fetched(FS, p, CommonPath())` is empty, and each new branch fact fetches exactly its own partial.
  - **Raw pins:** each flow's landmark text stays in the raw base prompt (`TestCommonPathInline`). Without these pins, a branch marked with a "non-common" value would hide a common-path step.
- **c-3:** Three test-level guards:
  - The conservation manifest is extended to the five prompts and recorded from the unedited files before any edit. Every row must stay in `Expand(p)` or be claimed by exactly one `// absorbs:` marker.
  - The raw-read meta-guard's set of split prompts is derived from the partial registry, so a prompt's first partial forces its raw readers onto `Expand`.
  - The meta-guard's blind spots are closed: variadic doc helpers, `cmd/dross` and `internal/rules`.
- **c-4:** A `checkPromptBudgetsRestFlow` that pins:
  - The before bytes: plan 21154, quick 18098, spec 19075, ship 23006, options 28231.
  - The base build (`8eb38e9f`).
  - After legs at or under 15,000 bytes and below their before bytes.
  - Equal fixture trees, zero fetches, and a dross_calls subset.
  - It also has a break-one-field table test, so the checker itself is proven to bite.

## The plan

```
Phase prompt-budgets-rest: 16 tasks across 4 waves

Wave 1
  t-1  Capture before legs on base build
       files:    .dross/phases/prompt-budgets-rest/flow-cost-before.toml
       covers:   c-4
       desc:     Scratch worktree at 8eb38e9f, built with `go build` there (never `dross test` from it); binary_version names 8eb38e9f.
                 Rebuild the fixtures from prompt-partials' inline setup_sh and add a `phase` step per leg:
                   - plan leg: a flow-probe phase with a locked spec and no plan
                   - spec leg: a scaffolded spec-probe phase with no spec.toml
                   - quick leg: a pair quick on the flow repo with no origin (direct route)
                 Human-driven: /dross-plan <id> (no --panel) up to the `state touch "plan locked` call; /dross-spec <id> up to
                 `state touch "spec locked`; /dross-quick (pair) up to its `git commit`. This must run while the prompts are still unedited.
                 Record [before] (plan), [quick_before] and [spec_before] with the prompt-partials leg fields.
                 Record [bytes_only] ship_before = 23006 and options_before = 28231 from
                 `git show 8eb38e9f:assets/prompts/<p>.md | wc -c`.
       contract: if a leg ran on edited prompts, t-16's TestFlowCostRecords/prompt-budgets-rest fails: [before].prompt_bytes != 21154,
                 [quick_before] != 18098 or [spec_before] != 19075
                 if the fixture is non-deterministic, the check fails: two setup runs give different `git rev-parse HEAD^{tree}`
                 (recorded as fixture_tree per leg)
                 if a before leg ran on a binary with t-5's route absorption, the check fails: each before binary_version must
                 contain "commit 8eb38e9f"

  t-2  Extend conservation manifest to five prompts
       files:    assets/conservation_test.go, assets/testdata/prompt_instructions.txt
       covers:   c-3
       desc:     manifestPrompts gains options, ship, plan, spec, quick. For the new prompts only, the extractor also takes inline
                 backticked spans that start with `dross ` (prose-level steps like `dross protect --check <base>` are outside fences).
                 Recording is append-only: DROSS_RECORD_PROMPT_INSTRUCTIONS writes only prompts that have no rows yet. Record from
                 today's unedited files. Each new prompt gets a minRows floor. Lands green.
       contract: if a moved instruction vanishes, TestPromptInstructionsConserved fails naming the row: removing
                 `dross ship <phase-id>` from ship.md and every partial (a MapFS copy of the tree, as TestConservationCatchesALostRow
                 does for verify)
                 if a prose-level step is lost, the test fails: deleting the inline `dross local set quick_base <base>` span from
                 quick.md, with no absorbs marker, is reported as "instruction lost"
                 if re-recording rewrites existing rows, TestManifestRecordIsAppendOnly fails: recording with execute.md rows
                 present leaves those lines byte-identical
                 if the manifest goes vacuous, the test fails: fewer rows than minRows for any of the five
                 (floor = 80% of the count recorded today)

  t-3  Add branch facts; pin five common paths
       files:    assets/partials.go, assets/partials_test.go
       covers:   c-2, c-3
       desc:     TriggerVocabulary gains six keys, each with the common path as its first value:
                   - panel{off,on}
                   - auto{off,on}
                   - ci{passed,failed}
                   - deferral{none,chosen}
                   - lanes{unchanged,editing}
                   - remote{unchanged,changing}
                 Quick reuses mode{pair,solo}, and ship recovery reuses preflight{ok,recovery}.
                 TestCommonPathInline gains raw-base needles for each flow; they pass today:
                   - plan: §2G walk, `## 3. Propose`, `## 5. Write plan.toml`, `dross state set current_phase_status "planned"`
                   - spec: `### 3b.`, `[[criteria]]`, `dross state touch "spec locked: <id>"`, "Resume, don't overwrite"
                   - quick: `dross quick begin "<description>"`, `**Red, pair mode**`, `dross quick end`, `## 5. Commit`
                   - ship: `## 1. Preview`, `## 3. Reviewers`, `## 3.5 Merge landmarks`, `dross ship <phase-id>`, the GitHub
                     `gh run watch` steps, the §6 merge AskUserQuestion
                   - options: `## Section pick`, "Keep · Change · Skip", every `## N.` heading 1–17,
                     `dross local set <key> "<value>"`, `dross trust --check`, `dross test lane list`
                 TestExpandIdentity is retargeted from quick (about to split) to techdebt.md, which never splits.
       contract: if a new key's common value fetches, TestCommonPathFetchesNothing fails: synthetic `panel=off`, `auto=off`,
                 `ci=passed`, `deferral=none`, `lanes=unchanged` and `remote=unchanged` fetch lines are each rejected as common path
                 if a flow's landmark leaves its base prompt, TestCommonPathInline (raw via assets.FS) fails naming the file and needle
                 if the identity check targets a split prompt, TestExpandIdentity fails: it asserts its target has no fetch points
                 (FetchPoints(techdebt.md) is empty) before comparing bytes

  t-4  Close raw-read guard blind spots
       files:    internal/cmd/prompt_expand_test.go, internal/cmd/lane_install_docs_test.go, internal/cmd/lane_toolchain_docs_test.go,
                 internal/cmd/milestone_narration_test.go, cmd/dross/main_test.go
       covers:   c-3
       desc:     Three changes to the guard, plus migrating what it newly catches:
                   - splitPrompts is derived from assets.PartialNames, so it covers every prompt with at least one partial instead of a
                     fixed {execute, verify}.
                   - Raw-read detection also matches any call whose literal args spell "assets","prompts" (docsBody, readDoc, docText,
                     variadic parts).
                   - The walk extends to cmd/ and internal/rules.
                 The newly caught readers are migrated in this same task, so the allowlist does not grow:
                   - lane_install docsBody(options.md) → expandedPrompt
                   - lane_toolchain readDoc(options.md) → expandedPrompt
                   - milestone_narration docText(ship.md) → expandedPrompt
                   - cmd/dross main_test ship.md → assets.Expand(assets.FS, "ship")
       contract: if a variadic helper reads a prompt raw, TestRawPromptReadDetection fails: `docsBody(t, "assets", "prompts",
                 "options.md")` and `readDoc(t, "assets", "prompts", "ship.md")` must be detected
                 if splitPrompts stays a fixed list, TestSplitPromptsFollowTheRegistry fails: on a MapFS adding _plan.panel.md, plan.md is
                 a split prompt without editing any list
                 if cmd/dross is outside the walk, the meta-guard fails: a raw os.ReadFile of "../../assets/prompts/ship.md" in
                 cmd/dross/main_test.go is reported
                 if the allowlist grows to absorb the new detections, the pin check fails (rawPromptReadAllowlistSize stays at 35 or lower)

Wave 2
  t-5  Absorb quick branch routing into quick begin   (depends t-2)
       files:    internal/cmd/quick.go, internal/cmd/quick_route.go, internal/cmd/quick_begin_test.go
       covers:   c-1, c-3
       desc:     After the mode record, `dross quick begin` resolves the §0 route and prints exactly one `route:` line:
                   - `route: in-phase phase/<id>`
                   - `route: direct <base>` (writes local.toml quick_base)
                   - `route: pr quick/<preview>`, where preview = version with internal+1 (writes no quick_base)
                   - `route: standalone (shipped window)` prefix when current_phase is shipped
                 It does the base-branch + protect --check work in-process. A refused solo begin has no side effects.
                 One `// absorbs: <row>` marker per t-2 manifest row it takes over: `dross base-branch`, `dross protect --check <base>`,
                 `dross local set quick_base <base>`.
       contract: if routing ignores the phase, TestQuickBeginRoutesInPhase fails: current_phase p, status in-progress ->
                 `route: in-phase phase/p`, local.toml byte-identical
                 if the shipped window routes in-phase, the test fails: status shipped -> a standalone route, and the line names the
                 shipped window
                 if the PR route records a base, TestQuickBeginPRRoute fails: an httptest provider reporting protected (and separately
                 unknown/500) -> `route: pr quick/1.8.6.1` for version 1.8.6.0, and local.toml has no quick_base
                 if the direct route forgets the base, the test fails: unprotected and `not checked (<provider>)` -> `route: direct
                 milestone/v1.8`, quick_base = milestone/v1.8
                 if a refused solo begin writes, TestQuickBeginRefusedSoloNoSideEffects fails: no reviewer -> local.toml, quick.json and
                 the httptest provider (0 requests) untouched
                 if the stale-binary signal disappears, the test fails: every non-refused begin prints exactly one `route:` line
                 if a marker drifts from its manifest row, TestPromptInstructionsConserved reports "matches no manifest row"

  t-6  Route spec prompt readers through Expand   (depends t-4)
       files:    internal/cmd/spec_prompt_test.go, internal/cmd/interaction_pilot_test.go, internal/cmd/phase_test.go,
                 internal/rules/rules_test.go, internal/cmd/prompt_expand_test.go
       covers:   c-3
       desc:     specPromptContent, TestSpecPilotUsesEmitter, TestPhaseListPromptConsumersAreCurrent and the rules package's
                 TestSpecPromptDefersToSnippet read the expanded spec.md. The rules package uses assets.Expand(assets.FS, "spec"), since
                 the internal/cmd helpers aren't in scope there.
                 Remove 4 allowlist entries and lower the pin by 4.
       contract: if a spec reader stays raw, TestNoAllowlistedReaderReadsASplitPrompt fails the moment t-11 lands _spec.deferral.md
                 if a loader skips partials, TestPromptLoadersSeePartials-style check fails: a BANNED phrase only in a MapFS
                 _spec.deferral.md is seen by specPromptContent
                 if an entry is removed without lowering the pin (or vice versa), TestPromptReadsGoThroughExpansion fails

  t-7  Route plan and ship readers through Expand   (depends t-4)
       files:    internal/cmd/plan_prompt_test.go, internal/cmd/plan_review_tiers_test.go, internal/cmd/interaction_coreloop_test.go,
                 internal/cmd/ship_prompt_test.go, internal/cmd/prompt_expand_test.go
       covers:   c-3
       desc:     These read Expand:
                   - planPromptContent
                   - TestPlanPromptDescribesEveryReviewTier
                   - TestPlanPromptNoArtifactDump
                   - TestShipPromptDecisionTurnsSeparate
                   - rawShipSection plus its :456 raw read
                 A check that genuinely needs the base only (footer, common path) switches to assets.FS.
                 Remove the matching allowlist entries and lower the pin.
       contract: if a plan or ship reader stays raw, TestNoAllowlistedReaderReadsASplitPrompt fails once t-10/t-12 add the first
                 partial
                 if the tier test reads the base only, TestPlanPromptDescribesEveryReviewTier still finds the `--panel` tier row when §2P
                 lives in _plan.panel.md (proven against a MapFS move)
                 if ship's merge-gate decision-turn check loses the partial text, TestShipPromptDecisionTurnsSeparate (expanded) fails

  t-8  Route quick prompt readers through Expand   (depends t-4)
       files:    internal/cmd/quick_prompt_test.go, internal/cmd/squash_claim_guard_test.go,
                 internal/boardsync/board_lifecycle_divergence_test.go, internal/cmd/prompt_expand_test.go
       covers:   c-3
       desc:     quickPromptContent and TestQuickPromptHandlesShippedWindow read the expanded quick.md. boardsync rawPrompt
                 (quick/milestone/ship emission lanes) reads assets.Expand. Remove the matching entries and lower the pin.
       contract: if a quick reader stays raw, TestNoAllowlistedReaderReadsASplitPrompt fails once t-13 adds _quick.solo.md
                 if an emission lane's text moves into a partial unseen, TestEmittedStatusesAreTheLifecycleSet (expanded) still sees
                 every status ship.md and quick.md emit; a MapFS partial holding the only `--status shipped` keeps it in the set

  t-9  Route options prompt readers through Expand   (depends t-4)
       files:    internal/cmd/options_docs_test.go, internal/cmd/readme_doc_test.go, internal/cmd/interaction_setupcmds_test.go,
                 internal/cmd/interaction_snippet_test.go, internal/cmd/prompt_expand_test.go
       covers:   c-3
       desc:     optionsPrompt, readme_doc optionsSection (:244) and setupPromptBody (options/quick) read Expand. The
                 interaction_snippet options.md literal (:191) stays a raw-by-design footer read via assets.FS. Remove the matching
                 entries and lower the pin.
       contract: if an options reader stays raw, TestNoAllowlistedReaderReadsASplitPrompt fails once t-14 adds _options.lanes.md
                 if lane docs leave the loader's view, options_docs lane/selector/toolchain tests (expanded) fail when _options.lanes.md
                 loses `--selector-template`, `--empty-exit` or `trusted_lane_installs`
                 if the README option rows drift, readme_doc optionsSection still matches every options.md section heading (raw headings
                 stay in base per t-3)

Wave 3
  t-10 Move plan panel mode to a partial   (depends t-1, t-2, t-3, t-7)
       files:    assets/prompts/plan.md, assets/prompts/_plan.panel.md, internal/cmd/plan_prompt_test.go
       covers:   c-1, c-2, c-3
       desc:     §2P, 2P.1, 2P.2 and 2P.3 move to _plan.panel.md. plan.md keeps a one-line §2P stub: **When** `panel=on` — --panel was
                 passed: `dross prompt partial plan panel`. §0's flag parse keeps naming --panel. Compress Hard rules and §6.1 to reach
                 ≤14,500 B raw, under the conservation guard. Post-commit: `make install` (r-01). Report the measured raw bytes.
       contract: if panel text loads on the plain path, TestPlanPanelFetchPoint fails: Fetched(plan, CommonPath()) is empty and
                 Fetched(plan, {panel: on}) == [panel]
                 if the lens planner brief is lost, plan_prompt panel tests (expanded) fail when _plan.panel.md drops
                 `panel/synthesis.md` or one of risk/mvp/verification
                 if the non-panel decomposition leaves base, TestCommonPathInline (raw) fails on `## 3. Propose` / §2G
                 if a fenced row is dropped while compressing, TestPromptInstructionsConserved names it

  t-11 Move spec deferral routing to a partial   (depends t-1, t-2, t-3, t-6)
       files:    assets/prompts/spec.md, assets/prompts/_spec.deferral.md, internal/cmd/spec_prompt_test.go
       covers:   c-1, c-2, c-3
       desc:     §4a step 2 (destination: backlog / named phase / someday, with the `dross milestone add` / `dross deferred route`
                 fences and the <idx> rule) moves to _spec.deferral.md behind **When** `deferral=chosen` — the user picked "defer it".
                 The step 1 include-first gate stays inline. The create flow stays inline (judgment call below). Compress §0 and Hard
                 rules to ≤14,500 B raw. Post-commit: `make install`.
       contract: if the include-first gate leaves base, TestCommonPathInline (raw) fails on the "add to current phase" lead
                 (include-first is a memory-level decision, 2026-09-29)
                 if routing verbs are lost, spec_prompt deferred-route tests (expanded) fail without `dross deferred route <phase-id>
                 <idx> --target` in the partial
                 if the destination text fires on a spec with nothing deferred, TestSpecDeferralFetchPoint fails:
                 Fetched(spec, CommonPath()) is empty and {deferral: chosen} == [deferral]
                 if the `Spec locked` footer moves, the footer coverage gate (footer_coverage.go reads spec.md raw) fails

  t-12 Move ship auto, recovery, CI-failure branches   (depends t-1, t-2, t-3, t-7)
       files:    assets/prompts/ship.md, assets/prompts/_ship.auto.md, assets/prompts/_ship.recovery.md, assets/prompts/_ship.ci-failure.md,
                 internal/cmd/ship_prompt_test.go
       covers:   c-1, c-2, c-3
       desc:     Three moves:
                   - §0.5 --auto moves to _ship.auto.md (auto=on); the fetch sits right after flag parse.
                   - Recovery moves to _ship.recovery.md (preflight=recovery).
                   - §5 "On failure" and the 3-iteration cap move to _ship.ci-failure.md (ci=failed).
                 The GitHub watch steps, §6 merge gate and footers stay inline. Compress Forgejo/GitLab polling and the two-merge-levels
                 callout to ≤14,500 B raw. Post-commit: `make install`.
       contract: if CI fix guidance is lost, ship_prompt CI tests (expanded) fail when _ship.ci-failure.md drops `gh run view
                 --log-failed` or the "re-run `dross ship <phase-id>`" step
                 if --auto starts merging, the auto test (expanded) fails when _ship.auto.md loses "returns without merging"
                 if an interactive ship fetches, TestShipCommonPathFetchesNothing fails: Fetched(ship, CommonPath()) empty; each of
                 auto=on, preflight=recovery and ci=failed fetches exactly its own partial
                 if the never-gate-on-`gh pr checks --watch` rule leaves base, TestCommonPathInline (raw) fails
                 if a raw branch switch sneaks in via a partial, TestNoPromptPerformsRawBranchSwitch (expanded, walks partials) fails
                 if the `state is on disk` footer moves, the footer coverage gate fails

  t-13 Move quick solo text; drop absorbed routing   (depends t-1, t-2, t-3, t-5, t-8)
       files:    assets/prompts/quick.md, assets/prompts/_quick.solo.md, internal/cmd/quick_prompt_test.go,
                 internal/cmd/protected_prompts_test.go
       covers:   c-1, c-2, c-3
       desc:     Precondition: `make install` with t-5 live. Changes:
                   - Solo text moves to _quick.solo.md: solo red path, no-test solo arm, the §4 Solo review block and solo-only hard
                     rules. It is fetched once, right after the `dross quick begin --solo` fence (mode=solo, locked branch_placement).
                   - A `**Re-fetch** if context was compacted:` line sits between the green `dross test` and `## 5. Commit`.
                   - The §0 route decision tree is deleted, not moved, since begin owns it. §0 keeps the per-`route:` reactions and the
                     missing-`route:`-line stale-binary stop.
                   - TestQuickPromptRoutesProtectedBase is retargeted to assert the reactions plus the absorbs marker.
                 Post-commit: `make install`.
       contract: if the solo fetch drifts from the begin (locked), TestQuickSoloFetchPlacement fails: the `quick solo` When line is the
                 first fetch point after the `dross quick begin --solo` fence
                 if solo review order breaks, the test fails: inside the partial, re-run `dross test` < `dross review context` < spawn
                 < `dross review status`, and the base re-fetch line sits between green `dross test` and `## 5. Commit`
                 if the refusal stop moves into the partial, TestQuickSoloBeginNeedsReviewer (raw via assets.FS) fails
                 if the absorbed tree is moved instead of deleted, TestTestStepPromptsNotGrown (expanded quick cap 18102) fails
                 if the PR route loses `git push -u origin quick/` or `gh pr create --base`, TestQuickPRRouteCommands (expanded) fails
                 if pair runs fetch, Fetched(quick, {mode: pair}) is empty

  t-14 Move options lane and remote lore out   (depends t-1, t-2, t-3, t-9)
       files:    assets/prompts/options.md, assets/prompts/_options.lanes.md, assets/prompts/_options.remote.md,
                 internal/cmd/options_docs_test.go
       covers:   c-1, c-2, c-3
       desc:     Two moves out of §14:
                   - Lane declaration, prepare, install, selector, template, empty-exit, toolchain and per-lane grant text moves to
                     _options.lanes.md, behind **When** `lanes=editing` (the user chose Change on test lanes).
                   - The grant/revoke/tuning-knob text moves to _options.remote.md (remote=changing).
                 §14 keeps current-state reads inline: `dross local get`, `dross mutation remote status`, `dross trust --check` (the
                 consent rule) and `dross test lane list`. All 17 section headings stay. Compress to ≤14,500 B raw.
                 Post-commit: `make install`.
       contract: if the consent rule moves out, TestCommonPathInline (raw) fails on "Never run it on their behalf" in options §14
                 if lane lore is lost, lane_install_docs and lane_toolchain_docs tests (expanded since t-4) fail when
                 _options.lanes.md drops `dross test lane install <name> [--apply] [--local]` or exit `8`
                 if a walk that only reads settings fetches, TestOptionsCommonPathFetchesNothing fails: Fetched(options, CommonPath())
                 is empty; lanes=editing and remote=changing each fetch exactly one
                 if an inert key goes undocumented, inert_config_test's prompt walk (it reads every prompts/*.md including partials)
                 still finds each key's reason

Wave 4
  t-15 Enumerate every base prompt in budget test   (depends t-10, t-11, t-12, t-13, t-14)
       files:    assets/budget_test.go
       covers:   c-1
       desc:     Replace the promptByteBudget map and the exact-set {execute, verify} guard with one `const promptByteCap = 15000` and
                 budgetProblems(fsys fs.FS). budgetProblems globs prompts/*.md, skips any `_`-prefixed name, and takes no per-file
                 argument. TestPromptByteBudget runs it over assets.FS with t.Run per file.
       contract: if a prompt is over the cap, TestPromptByteBudget/<file> fails at 15,001 B naming the file and the overage
                 if a new prompt slips in unbudgeted, TestPromptByteBudgetEnumerates fails: a MapFS-only prompts/brand-new.md at 15,001 B
                 is reported
                 if any override exists, the test fails: a MapFS holding options, ship, plan, spec, quick, execute and verify each at
                 15,001 B reports all seven; exactly 15,000 B reports none
                 if partials get budgeted, the test fails: a 40,000-byte _options.lanes.md and a 40,000-byte _interaction.md report
                 nothing
                 if the glob silently matches nothing, the test fails: the real-FS run must see more than 30 base prompts

  t-16 Record after legs; gate flow cost   (depends t-1, t-10, t-11, t-12, t-13, t-14)
       files:    .dross/phases/prompt-budgets-rest/flow-cost.toml, .dross/phases/prompt-budgets-rest/flow-cost-before.toml,
                 internal/cmd/flowcost_record_test.go
       covers:   c-2, c-4
       desc:     `make install` at phase head, rebuild t-1's fixtures and re-drive the three legs identically. Merge the before legs
                 into flow-cost.toml, add [after]/[quick_after]/[spec_after] and [bytes_only] ship_after/options_after (raw base bytes
                 + zero fetched partial bytes), then delete flow-cost-before.toml.
                 flowCost gains QuickBefore/QuickAfter/SpecBefore/SpecAfter and BytesOnly. Add checkPromptBudgetsRestFlow (run through
                 checkLegPair for each pair) and promptBudgetsRestBase = "commit 8eb38e9f". Add the phase to the required-phase list.
                 Turns and wall-clock are recorded, not gated (locked flow_set).
       contract: if an after prompt is over budget or not smaller, TestFlowCostRecords/prompt-budgets-rest fails: any after
                 prompt_bytes (three legs plus ship/options bytes_only) > 15000 or >= its before
                 if the common path fetched, the test fails: partial_fetches != 0 on [after], [quick_after] or [spec_after]
                 if a split added a round-trip, checkLegPair fails: an after leg runs a dross call its before leg didn't, such as
                 `dross prompt partial`
                 if the before pins drift, the test fails: before bytes != 21154/18098/19075 and bytes_only before != 23006/28231
                 if the legs aren't comparable, the test fails: unequal fixture_tree per pair, or the same binary_version on both legs
                 if the checker goes soft, TestPromptBudgetsRestFlowProblems fails: a sound record reports nothing, and each one-field
                 break reports exactly one problem: after over cap, fetch = 1, before off base, options_after missing, tree mismatch,
                 before file lingering
                 if turns get gated by accident, the same table test fails: after agent_turns = 99 and wall_clock_s = 999 report nothing
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-5, t-10, t-11, t-12, t-13, t-14, t-15 |
| c-2 | t-3, t-10, t-11, t-12, t-13, t-14, t-16 |
| c-3 | t-2, t-3, t-4, t-5, t-6, t-7, t-8, t-9, t-10, t-11, t-12, t-13, t-14 |
| c-4 | t-1, t-16 |

All 4 of 4 criteria are covered.

## Judgment calls

- **The enumerating budget test lands last (t-15), not in wave 1.**
  - **Rejected:** a wave-1 test with a shrinking "still-over" list.
  - **Why:** that list is a per-prompt override, which locked no_exceptions forbids even temporarily. A red global test would also block every commit through the commit gate.
- **Wave 1 makes every guard green before any text moves.** That covers the manifest (t-2), the raw common-path pins and vocabulary (t-3) and the meta-guard (t-4).
  - **Rejected:** adding guards alongside each split.
  - **Why:** a guard born together with the move it checks can be fitted to the move.
- **The manifest gains inline `` `dross ...` `` spans for the five new prompts only, and recording becomes append-only.**
  - **Rejected:** keeping fenced rows plus lead-ins only.
  - **Why:** quick's routing, ship's watch steps and options' grants are prose-level. The old extractor would miss exactly what t-5 absorbs. Re-recording execute/verify would also drop their absorbed rows.
- **splitPrompts is derived from the partial registry.**
  - **Rejected:** extending the fixed list to seven names.
  - **Why:** with a derived set, the meta-guard turns red at the exact commit that adds a prompt's first partial. Nobody has to remember to update a list, which is the c-3 failure mode.
- **The meta-guard's blind spots are closed in t-4:** variadic `docsBody`/`readDoc`/`docText`, `cmd/dross`, `internal/rules`.
  - **Rejected:** trusting the prompt-partials guard as-is.
  - **Why:** grep shows that lane_install/lane_toolchain read options.md, milestone_narration reads ship.md, and cmd/dross reads ship.md, all raw and all invisible to today's detector. Those tests would pass vacuously after the move.
- **Four reader-migration tasks in wave 2 share `prompt_expand_test.go`'s allowlist map and pin.**
  - **Rejected:** one 13-file migration task, or folding each migration into its split.
  - **Why:** each group is a single test layer and stays at 5 files or fewer. The overlap is a one-line const, which execution serializes. Folding migrations into the splits would push each split to 7–9 files.
- **Quick's §0 route tree is absorbed into `dross quick begin` (t-5).**
  - **Rejected:** a `route=pr` partial, or compression only.
  - **Why:**
    - begin already runs in §0, so absorbing it matches the locked branch_placement.
    - The PR route is the *common* standalone path on this repo, because main is protected, so a partial would fetch on the hot path.
    - Absorption makes the bytes testable in Go with httptest.
- **ship §5/§6 success-path text stays inline (pinned in t-3) even though c-2's window ends at PR open.**
  - **Rejected:** moving the CI watch and merge gate past the window.
  - **Why:** every interactive ship continues there on green, so with `ci=passed` as the first value, moving them would make the common path fetch.
- **Spec's create flow stays inline. Only deferral step 2 moves.**
  - **Rejected:** a `target=create` partial.
  - **Why:** in this repo's flow, roadmap phases are often specced unscaffolded (the create path). Calling that "non-common" would hide a hot-path fetch that the fixture leg, run on a scaffolded phase, would never observe.
- **Options branch facts are "user chose Change on lanes/remote", not "picked section 14".**
  - **Rejected:** a `section=local` key.
  - **Why:** reviewing a section's current values is the walk itself. Only editing reaches the lore, and both facts keep the reads (`dross trust --check`, `dross test lane list`) in the base prompt.
- **The c-4 record pins before bytes to today's sizes and caps after bytes, but never ties after bytes to the live `assets.FS`.**
  - **Rejected:** a live tie of the after bytes to the shipped files.
  - **Why:** a live tie would break this phase's record on any future prompt edit. prompt-partials chose the same.
- **The flow-cost checker gets a break-one-field table test (t-16).**
  - **Rejected:** a checker exercised only by the real record.
  - **Why:** this is the test-summary-parsers pattern. An unexercised checker is a contract nobody has watched fail.
