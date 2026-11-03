# prompt-budgets-rest — MVP draft

Phase prompt-budgets-rest — 8 tasks across 3 waves

Wave 1
  t-1  Capture before-legs on unedited prompts
       files:    .dross/phases/prompt-budgets-rest/flow-cost-before.toml
       desc:     Build the base binary in a scratch worktree at 8eb38e9f (go build there; never `dross test` from it). Extend prompt-partials'
                 inline setup_sh with three fixture states (phase scaffolded with no spec; phase with locked spec, no plan; a clean repo for a
                 pair quick) and drive /dross-spec to lock, /dross-plan (no --panel) to lock, pair /dross-quick to commit, all before any prompt
                 edit (prompts are symlinked). Record [before] (plan), [quick_before], [spec_before] with prompt_bytes, partial_fetches,
                 agent_turns, turn_tools, wall_clock_s, human_wait_s, dross_calls, binary_version, session_id, fixture_tree; plus
                 [loaded_bytes] ship_before = 23006, options_before = 28231 read from the base commit (no session, per flow_set).
       covers:   c-4
       contract: if a before-leg loaded an already-edited prompt, t-8's TestFlowCostRecords/prompt-budgets-rest fails:
                   [before].prompt_bytes != 21154, [quick_before] != 18098, [spec_before] != 19075
                 if a before-leg ran a phase-built binary, the same check fails: every *_before.binary_version must name 8eb38e9f
                 if the fixture build is not reproducible, the check fails: each *_after.fixture_tree must equal its *_before.fixture_tree

  t-2  Route five prompts' content tests through Expand
       files:    internal/cmd/prompt_expand_test.go, internal/cmd/plan_prompt_test.go, internal/cmd/plan_review_tiers_test.go,
                 internal/cmd/spec_prompt_test.go, internal/cmd/interaction_pilot_test.go, internal/cmd/phase_test.go,
                 internal/cmd/quick_prompt_test.go, internal/cmd/squash_claim_guard_test.go, internal/cmd/ship_prompt_test.go,
                 internal/cmd/interaction_coreloop_test.go, internal/cmd/options_docs_test.go, internal/cmd/readme_doc_test.go,
                 internal/cmd/interaction_setupcmds_test.go, internal/boardsync/board_lifecycle_divergence_test.go, internal/rules/rules_test.go
       desc:     Add options.md, ship.md, plan.md, spec.md, quick.md to splitPrompts. Swap every allowlisted raw reader the guard then flags to
                 expandedPrompt (assets.Expand in boardsync/rules; assets.FS only where a test truly needs the bare base), drop those allowlist
                 entries and lower rawPromptReadAllowlistSize. No prompt is edited, so Expand is identity and every test stays green.
       covers:   c-3
       contract: if a reader of a newly split prompt stays raw, TestNoAllowlistedReaderReadsASplitPrompt fails naming file:line
                   (e.g. quick_prompt_test.go#quickPromptContent with "quick.md")
                 if a migrated entry lingers in the allowlist, TestPromptReadsGoThroughExpansion fails: "matches no raw read — remove it and lower the pin"
                 if a migrated loader reads the base instead of the expansion, TestPromptLoadersSeePartials fails: a banned phrase planted only in a
                   MapFS partial is invisible to it

Wave 2 (each depends t-1, t-2)
  t-3  Move quick solo and recovery text to partials
       files:    assets/prompts/quick.md, assets/prompts/_quick.solo.md, assets/prompts/_quick.recovery.md, assets/partials_test.go,
                 internal/cmd/quick_prompt_test.go
       desc:     Solo-only text (§2 solo gate, §4 red-solo and solo review, §5 solo commit body/gate) → _quick.solo.md, fetched once
                 (`mode=solo`) right after the `dross quick begin --solo` fence; the refused-solo-begin line stays inline. Dirty-tree and
                 shipped-phase-window handling → _quick.recovery.md (`preflight=recovery`). Retarget TestExpandIdentity to a fetch-free prompt
                 (status). Report measured quick.md bytes (target ≤15,000).
       covers:   c-1, c-2, c-3
       contract: if the solo fetch is not the first fetch after the `dross quick begin --solo` fence (locked branch_placement), new
                   TestQuickSoloFetchPlacement fails
                 if solo review ordering breaks in the move, TestQuickSoloReview / TestQuickSoloReviewBlocked (expanded via t-2) fail:
                   green `dross test` < reviewer spawn < verdict < commit
                 if pair facts fetch, TestCommonPathFetchesNothing fails: Fetched(quick, CommonPath()) must be empty
                 if fetch lines push the expansion past its base, TestTestStepPromptsNotGrown fails at expanded quick.md > 18102
                 if the shipped-phase window is lost, TestQuickPromptHandlesShippedWindow (expanded) fails
                 if Expand alters a fetch-free prompt, TestExpandIdentity (now on status) fails: Expand(status) != status.md

  t-4  Move plan --panel and resume text to partials
       files:    assets/prompts/plan.md, assets/prompts/_plan.panel.md, assets/prompts/_plan.recovery.md, assets/partials.go,
                 internal/cmd/plan_prompt_test.go
       desc:     Add trigger key `panel` = {off, on}. §2P (2P.1–2P.3, ~5.6 KB) → _plan.panel.md fetched only under `panel=on` at §0's flag
                 parse; plan.toml-present resume handling → _plan.recovery.md (`preflight=recovery`). Close the last ~600 B by cutting Hard-rules
                 lines that restate body text verbatim. Report measured plan.md bytes (target ≤15,000).
       covers:   c-1, c-2, c-3
       contract: if panel text stays on the common path, new TestPlanPanelOnlyInPartial fails: raw assets.FS plan.md contains `### 2P.1`, or
                   Expand(plan) lacks it after the `panel=on` fetch line
                 if `panel=off` is used as a trigger, scanFetchLines rejects it as the common path (TestCommonPathFetchesNothing synthetic list
                   gains panel=off)
                 if a review tier description is lost, TestPlanPromptDescribesEveryReviewTier (expanded) fails
                 if the panel lens paths are lost, TestPlanPromptNoArtifactDump / interaction_coreloop tests over the expansion fail
                 if the clear-point footer leaves base plan.md, TestBoundaryPromptsCarryFooter (footer_coverage.go reads base raw) fails

  t-5  Move spec phase-create and resume text out
       files:    assets/prompts/spec.md, assets/prompts/_spec.create.md, assets/prompts/_spec.recovery.md, assets/partials.go,
                 internal/cmd/spec_prompt_test.go
       desc:     Add trigger key `target` = {resolved, unresolved}. §0's done-phase / unset-phase asks and the roadmap-aware Create flow →
                 _spec.create.md (`target=unresolved`: --new, or current_phase unset/done); spec.toml-present resume → _spec.recovery.md
                 (`preflight=recovery`). Close the remainder by cutting Hard-rules lines that restate body text or the printed interaction
                 playbook. Report measured spec.md bytes (target ≤15,000).
       covers:   c-1, c-2, c-3
       contract: if the roadmap picklist is lost, a new TestSpecCreateFlowRoadmapAware fails: Expand(spec) after the `target=unresolved`
                   fetch must keep `dross phase list --milestone` and "Describe a new phase"
                 if a resolved phase fetches, TestCommonPathFetchesNothing fails: Fetched(spec, CommonPath()) empty
                 if routing/gray-area instructions are trimmed away, the TestSpecPrompt* suite (expanded via t-2) fails, e.g.
                   TestSpecPromptRoutesFourDestinations, TestSpecPromptWalksEveryGrayArea
                 if the guarded checkout leaves the expansion, TestNoPromptPerformsRawBranchSwitch fails on spec
                 if the footer leaves base spec.md, TestBoundaryPromptsCarryFooter fails

  t-6  Move ship recovery, auto, CI-fail, provider text
       files:    assets/prompts/ship.md, assets/prompts/_ship.recovery.md, assets/prompts/_ship.auto.md, assets/prompts/_ship.ci-fail.md,
                 assets/prompts/_ship.provider.md, assets/partials.go
       desc:     Add trigger keys `ci` = {pass, fail} and `provider` = {github, other} (in assets/partials.go). ## Recovery →
                 _ship.recovery.md (`preflight=recovery`); §0.5 --auto → _ship.auto.md (`mode=solo`); §5 On failure → _ship.ci-fail.md
                 (`ci=fail`); Forgejo/Gitea/GitLab steps from §5/§6 → _ship.provider.md fetched once in §0 (`provider=other`). Its content tests already read
                 the expansion (t-2). Report measured ship.md bytes (target ≤15,000).
       covers:   c-1, c-2, c-3
       contract: if recovery steps are lost, TestShipPromptRecoverySection (expanded) fails on any of the six named failure modes
                 if the --auto skip list is lost, TestShipPromptAutoFastPath and TestShipPromptAutoBackfill (expanded) fail
                 if a non-GitHub provider's CI poll or squash call is lost, TestShipPromptGitLabSections (expanded) fails
                 if the CI fix loop loses its re-ship, TestShipPromptReShipsAfterEveryCIFix fails
                 if a verified GitHub ship fetches before PR open, TestCommonPathFetchesNothing fails: Fetched(ship, CommonPath()) empty

  t-7  Move options machine-local section to partial
       files:    assets/prompts/options.md, assets/prompts/_options.machine-local.md, assets/partials.go, internal/cmd/options_docs_test.go
       desc:     Add trigger key `local_settings` = {unpicked, picked}. §14 body (15.3 KB) → _options.machine-local.md; base keeps the
                 `## 14.` heading with a one-line fetch (`local_settings=picked`) so the section-pick list and numbering stay intact.
                 Report measured options.md bytes (target ≤15,000).
       covers:   c-1, c-2, c-3
       contract: if a local.toml key loses its docs, TestOptionsCoversEveryLocalKey (expanded) fails naming the key
                 if the §14 heading leaves the base, TestOptionsSectionNumbersAreContiguous fails (gap at 14) on raw assets.FS options.md
                 if a walk that skips §14 fetches, TestCommonPathFetchesNothing fails: Fetched(options, CommonPath()) empty

Wave 3 (depends t-1, t-3, t-4, t-5, t-6, t-7)
  t-8  Enumerate global budget; record after-legs
       files:    assets/budget_test.go, internal/cmd/flowcost_record_test.go,
                 .dross/phases/prompt-budgets-rest/flow-cost.toml, .dross/phases/prompt-budgets-rest/flow-cost-before.toml
       desc:     TestPromptByteBudget globs prompts/*.md, skips `_*` files, caps each at 15,000 (fixed list + its exact-set assertion deleted).
                 `make install`, rebuild t-1's fixtures, re-drive the three flows; merge before + after into flow-cost.toml (loaded_bytes =
                 prompt + fetched partials), delete flow-cost-before.toml. flowCost gains quick_/spec_ legs and [loaded_bytes];
                 checkPromptBudgetsRestFlow; prompt-budgets-rest joins the required-phase list. Turns/wall-clock recorded, not gated.
       covers:   c-1, c-2, c-4
       contract: if any base prompt regrows, TestPromptByteBudget fails naming it: a MapFS prompts/new.md at 15,001 B fails, at 15,000 passes
                 if partials are capped, the same test fails: a MapFS _quick.solo.md and _interaction.md at 20,000 B must pass
                 if enumeration goes vacuous, the test fails: it must visit at least the 24 embedded base prompts, including the five cut here
                 if a common path fetched, TestFlowCostRecords/prompt-budgets-rest fails: partial_fetches != 0 on [after]/[quick_after]/[spec_after]
                 if an after prompt is over cap or not smaller, the check fails: any *_after.prompt_bytes or loaded_bytes ship/options_after
                   > 15000 or ≥ its before
                 if an after-leg added a round-trip, checkLegPair fails: a dross call absent from the matching before-leg (applied to all three pairs)
                 if the before file lingers, the check fails: flow-cost-before.toml still present

## Coverage

- c-1 → t-3, t-4, t-5, t-6, t-7 (the cuts), t-8 (the enumerating budget test)
- c-2 → t-3, t-4, t-5, t-6, t-7 (common-path triggers, TestCommonPathFetchesNothing), t-8 (observed partial_fetches = 0 on plan/quick/spec)
- c-3 → t-2 (readers of all five go through Expand), t-3, t-4, t-5, t-6, t-7 (each keeps its existing content tests green over the expansion)
- c-4 → t-1 (before legs + ship/options before bytes), t-8 (after legs, merged record, gate)

4/4 criteria covered.

## Judgment calls

- Prompt moves only, no Go absorption: chose partials + verbatim-duplicate trimming; rejected absorbing quick §6 / spec §6 bookkeeping into `dross quick end` or a new call — the moves reach the cap on estimate, and absorption is Go work that branch_placement permits but does not require.
- No conservation-manifest extension: c-3 names *existing* prompt-content tests; t-2 puts all of them on the expansion. Rejected adding the five to assets/testdata/prompt_instructions.txt — it is a new guard, and absorption (what absorbs-markers exist for) is not planned.
- One reader-migration task over 15 files: kept as one because every edit is the same loader swap in one layer, driven by one guard (TestNoAllowlistedReaderReadsASplitPrompt), and all of them edit prompt_expand_test.go's pinned allowlist; splitting it puts that pin in two tasks' files.
- Reader migration up front (wave 1) instead of per split task: Expand is identity until a prompt fetches, so it lands green before any move and keeps those test files out of five wave-2 file lists.
- Budget test lands last, merged into the after-leg task: a global enumerating cap is red until all five cuts land, and no_exceptions bars a temporary skip list; it is one file of work, too small to stand alone.
- Each split task adds its own trigger key to partials.go: rejected a wave-1 vocabulary task, since a key with no fetch line is speculative. Cost: t-4..t-7 all list partials.go, edits are separate map entries.
- ship's common path = GitHub (`provider=other` fetches Forgejo/GitLab text): without it ship stays ~16 KB after the recovery, --auto and CI-fail moves; this repo's remote is GitHub and c-2 measures that path. The other option was moving §3.5's backfill, which saves only ~0.7 KB.
- `--auto` reuses `mode=solo` instead of a new key: it is the non-interactive run, which is what solo already means in the vocabulary.
- options: one partial for all of §14 rather than per-subsection: it alone takes options from 28.2 KB to ~13 KB.
- ship/options get no sessions (flow_set locked); their before bytes are read from the base commit and their after bytes from the embedded file. No fixture work for them.
