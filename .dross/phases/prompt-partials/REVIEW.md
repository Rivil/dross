# Plan Review — prompt-partials

Reviewed: 2026-10-27
Plan: 16 tasks across 7 waves

## BLOCKING
- [locked-decision / c-5 coverage] t-9 will turn `TestNoPromptPerformsRawBranchSwitch` red, and no task lists that test. t-9 moves "wrong-branch ... handling" into `_execute.recovery.md`. execute.md:14 is the only place in execute.md that says `dross phase checkout <id>`. `internal/cmd/ship_prompt_test.go:235` requires execute.md to contain "dross phase checkout", and it reads the file raw through `promptContent` (ship_prompt_test.go:14 does `os.ReadFile(filepath.Join(root, "assets", "prompts", name))`). None of t-7, t-8 or t-10 migrates `promptContent`, so the read stays raw. If t-7's meta-guard matches that read shape, the same gap fails t-10 as well: the allowlist has to be "exactly {install_test.go}", but nothing removes ship_prompt_test.go from it.
  Suggestion: add ship_prompt_test.go to a migration task (t-7 or t-10), routing `promptContent` through Expand, and list the test in t-9's files and contract. The other option is to keep the `dross phase checkout` line inline in base §0.
- [c-5 / gate] t-9 cannot pass `TestTestStepPromptsNotGrown` as written. After t-7, that test checks the *expanded* execute.md against 25854 B, and the base file is 25853 B, so there is 1 byte of headroom. t-9 moves text verbatim and adds bytes on top: at least two `**When** \`k=v\` — …: \`dross prompt partial …\`` fetch lines (about 90–120 B each) plus the one-line §1f solo hook. Expand keeps the fetch line, so expanded size = moved text + fetch lines and ends up over the limit. The absorbed §0 prose that would make room (about 2 KB) is not removed until t-11, which depends on t-9.
  Suggestion: either make t-9 offset its own fetch-line overhead and say so in t-9's description and contract, or have t-11's §0 absorb land first (t-11 depends on t-9 only because both edit the same file). The third option is for t-7 to set the limit to base + an explicit allowance for fetch-line overhead.
- [c-5 / gate] t-14 will very likely turn `TestBoardCallingPromptsSurfaceFailure` (internal/cmd/board_switch_prompt_test.go:61) red. Exactly six prompts match `boardCallRe` today: execute, milestone, plan, quick, ship and verify. verify.md's only match is `dross issue phase sync <phase> --status uat` (verify.md:93). Both t-4's contract ("with `--status uat` deleted from verify.md") and t-14's contract (the row is claimed only by an absorbs marker) mean that line is deleted. That leaves five callers, and the anti-vacuity floor `len(callers) < 6` fails fatally. No `_verify.*` partial is planned to carry a `phase sync` call. (`dross issue task pull` in `_execute.board-moves.md` does not match the regex either.)
  Suggestion: add board_switch_prompt_test.go to t-14's files with an explicit decision. One option is to lower the floor to 5 with a comment that verify's sync moved into the CLI. The other is to have the `board: FAILED` reaction name a `dross issue phase sync … --status uat` retry. That retry line would then also need `[board].enabled` and "a non-zero exit is a board failure".

## FLAG
- [locked-decision solo_branch] t-9's "one-line solo hook" in §1f, which names the re-fetch command for a compacted context, conflicts with the locked "fetched once in §0" decision and with t-2's grammar.
  - If the hook is a grammar fetch line, its only possible trigger is `mode=solo`, because the closed vocabulary has no compaction key. An agent following it would then fetch once per task, which is exactly what the decision's `why` rules out. Expand would also inline `_execute.solo.md` a second time.
  - If the hook is not a fetch line, it is unclear whether t-2's grammar or "Read-instead-of-fetch" checks would reject a prose mention of `dross prompt partial execute solo`.

  Suggestion: state in t-9 whether the hook is a grammar fetch line. If it is not, add a t-2 contract that a non-fetch mention of the command is allowed (or forbidden).
- [test-contract] Expand's semantics are underspecified for three inputs the plan itself creates:
  - **Multi-referenced partials.** t-15 deliberately points two fetch lines (`verdict=partial` and `verdict=fail`) at `_verify.not-pass.md`. Does Expand inline it twice? t-2's TestExpandIdentity only pins a single fetch on a MapFS. Double inlining would inflate expanded verify.md and break count-based checks such as the "threshold exactly 1" test that t-8 relies on.
  - **Partial filenames passed in by directory walkers.** After t-7, `TestNoPromptGrantsTrustForTheUser`, `TestPromptCommitsSatisfyGreenGate` and the gate-lift scan (prompt_test_command_test.go:242) call `promptBody(t, e.Name())`, and therefore `Expand`, on every `*.md`, including `_execute.solo.md` and `_interaction.md`.
  - **Names with a `.md` suffix.** The same walkers pass names like `verify.md`.

  Suggestion: add t-2 contracts for Expand(partial-name), Expand("x.md") and a twice-referenced partial.
- [test-contract] The t-9 contract "TestExecutePromptSoloBeginNeedsReviewer (raw §0 read) fails" cannot bite after t-7. t-7 routes `rawPrompt`/`executePromptContent` through Expand, and t-10 bans raw reads in internal/ tests. Expand inlines `_execute.solo.md` right after the `begin --solo` fence, which is inside §0. So refusal text that moved into the partial would still appear in an expanded §0 read, and the test would still pass.
  Suggestion: name the sanctioned raw reader. For example, the test could read `assets.FS` directly, with the meta-guard explicitly exempting `assets.FS` reads. Otherwise, assert on the raw-vs-expanded difference.
- [antipattern / missing files] t-8's reader inventory is incomplete. It lists verify_prompt_test.go :21 and :47, but `TestVerifyPromptReadsRangeProvenance` does its own raw `os.ReadFile` of verify.md at verify_prompt_test.go:383, and t-12's contract assumes that read is "(expanded)". pad_residue_test.go also scans `padResidueDocs` (line 35, used at :109) raw for retired pad-heuristic needles. That negative scan never sees `_verify.scope.md`, which is exactly where t-12 moves the range-provenance text, so pad residue could come back in the partial undetected.
  Suggestion: add both reads to t-8's description, and add a contract that a pad needle placed in `_verify.scope.md` fails the scan.
- [antipattern] The conservation-marker coupling between t-3, t-6 and t-11 is fragile:
  - **t-3 writes the markers before the rows exist.** t-3 writes `// absorbs: <command>` markers in wave 1. t-6 extracts the manifest rows in wave 2, and a row passes only with a *matching* marker.
  - **One call can produce several rows.** §0's version fence alone produces three candidate rows: `PATCH=$(dross phase number <id>)`, a `# major.minor …` comment line, and `dross state set version …`. t-3 promises one marker "at each call".
  - **t-11 cannot repair a mismatch.** t-11 relies on those markers ("rows pass only via t-3's absorbs markers") but does not list internal/cmd/execute.go in its files.
  - **The extractor rule is ambiguous.** t-6's "every fenced command line" does not say whether fence comment lines or the non-command orientation block (execute.md:37–42) count.

  Suggestion: define the extractor rule precisely in t-6 (whether comments count, whether non-`dross`/`git` fences count), and either add internal/cmd/execute.go to t-11's files or require t-6 to reconcile t-3's markers against the manifest.
- [forbidden-actions r-01] The `make install` preconditions are placed before t-9, t-11, t-12 and t-14, but the binary they install cannot contain those tasks' partials. `dross prompt partial` prints `assets.Partial` from the embedded FS, while the base prompts are symlinked live from assets/prompts. After t-9 commits, the live execute.md therefore points at `_execute.solo.md` and `_execute.recovery.md`, which the installed binary does not embed, until the next install. That matters because this phase is run *by* /dross-execute: a resume or a compaction-triggered re-read mid-phase hits a missing partial.
  Suggestion: move the install step to *after* each partial-creating task (t-9, t-11, t-12, t-13, t-14, t-15) as a post-commit step, not a precondition.
- [test-contract] Nothing checks that t-1's before-legs ran on the base binary; t-16 only checks that `binary_version` differs between legs. t-1 runs in wave 1 alongside t-3. If t-3 lands and `make install` runs before t-1's legs, `execute begin` already prints the absorbed `version:`/`board:`/`pull:` lines and does the bump, sync and pull itself. That would skew `dross_calls` and turns. The `prompt_bytes` contract would not catch it, because the prompts would still be unedited.
  Suggestion: add a t-16 contract pinning `[before].binary_version` to the base build (3ee0fe71), or order t-1 before t-3 explicitly.
- [granularity / feasibility] No task records a byte target, so c-1's reachability is only discovered at t-13 (wave 5). A rough measure of execute.md:
  - solo text: about 3.0 KB
  - recovery: about 1.2 KB
  - absorbed §0 mechanics: about 2–2.5 KB, part of which the board-moves partial keeps
  - exit codes 2–8: about 1.4 KB

  That leaves roughly 17.5 KB plus fetch-line overhead. t-13's compression of the landmark, svelte, offload and §1e prose would then have to find about 3–4 KB on its own to reach 14,500 B, under the conservation guard. verify.md has no estimate at all.
  Suggestion: add an expected after-bytes figure to each move task's description (t-9, t-11, t-12, t-13, t-14, t-15), so a shortfall shows up at the task that causes it.

## NOTE
- [coverage] All six criteria are covered: c-1 (t-3, t-9, t-11, t-13), c-2 (t-4, t-12, t-14, t-15), c-3 (t-2, t-5, t-9, t-11–t-15), c-4 (t-2, t-3, t-4, t-9, t-11–t-16), c-5 (t-2, t-6–t-15) and c-6 (t-1, t-16). The plan honours the locked fetch_command, budget_cap (embedded asset, 15,000 B) and cli_absorb decisions.
- [wave-order] Every dependency is real and the wave assignments are tight. Reader migrations land before the text moves they protect (t-8 before t-12, t-10 before t-13), and no task could drop a wave.
- [strength] The `_<prompt>.<name>.md` naming is a good choice. The existing name-set helpers already skip `_`-prefixed files (`mdNamesIn` commands_parity_test.go:88, `mdNameSet` interaction_coverage.go:114, `offloadAuditGaps`). `install` symlinks or copies the whole prompts dir, and `//go:embed all:prompts` already embeds `_` files. So command-parity, interaction-coverage and install need no changes.
- [strength] The test contracts are unusually concrete: named tests, exact byte counts (25853 / 25864 / 15,001), httptest request counts and MapFS fixtures. t-4 avoids a trap by putting the `verified` state write in `verify finalize`'s RunE rather than `finalizeVerify`, which ship.go and phase.go auto-heal through, and it tests that the shipped status survives. t-2 guards against a scope trigger firing on gremlins' always-whole-file path.
- [test-contract] t-1's contracts are only enforced by t-16's test, six waves later, because flow-cost-before.toml is deliberately ungated. This is intentional, but bad before-data (for example a non-deterministic fixture) surfaces only at the end of the phase.

## Summary
The structure, coverage and contracts are strong. The plan should not proceed until three tasks are fixed, each of which turns an existing gate red on a test it doesn't list: t-9 fails both `TestNoPromptPerformsRawBranchSwitch` and `TestTestStepPromptsNotGrown`, and t-14 fails the board-caller floor.
