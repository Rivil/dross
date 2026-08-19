# Plan Review — completion-record-truth

Reviewed: 2026-08-19
Plan: 5 tasks across 3 waves

## BLOCKING
(none)

## FLAG

- [coverage] c-4 is worded at the `dross phase list` command surface ("appears exactly once in `dross phase list`"), but all four of t-3's contract arms sit one level below it: two test `phase.Ordered` directly and two test `dross doctor`. Nothing asserts the command output. t-2 rewrites the very rendering path that reaches `Ordered` (internal/cmd/phase.go:97 today) and does not declare `depends_on = ["t-3"]`, so if t-2's `--milestone` work grows its own ordering path and the bare listing follows it, every t-3 arm still passes green while c-4 regresses in the only place the criterion names.
  Suggestion: add one command-level arm to t-3 or t-2 — a two-milestone fixture sharing a slug where `dross phase list` prints that slug once — so the criterion is pinned at its own surface.

- [antipattern] t-2 breaks an existing byte-exact guard it never mentions. `TestPhaseListOrdersByMilestoneArray` (internal/cmd/phase_test.go:407) asserts the whole listing as `"gamma\nalpha\n"`; the `✓ `/two-space prefix and the `N/M done` footer make that assertion fail by construction. t-2's files include phase_test.go so it can be edited, but neither the description nor any contract arm says the array-ordering guarantee must survive the rewrite — the cheapest way to make the suite green is to relax that test into a substring check, silently dropping the "array order beats directory-name sort" proof.
  Suggestion: add a t-2 contract arm pinning ordering under the new rendering (reordering the milestone array flips the two `✓ `-prefixed lines), so the rewrite has to carry the old guarantee forward rather than dilute it.

- [test contract] t-5's first arm — "if README.md or docs/dross.1 loses the `phase list` entry, the doc grep test fails naming the file" — gates nothing as written. Both files already contain that string today: README.md:208 (`dross phase {create,checkout,list,show,complete,…}`) and docs/dross.1:115 (`dross phase {list|show|create|complete|red-proof}` in the synopsis). A grep for "phase list" passes before t-5 writes a word. The task description's premise ("`dross phase list` is documented in neither README.md nor docs/dross.1") is likewise inaccurate — the verb is listed in both; what is missing is any description of the done marker, the footer and the flag.
  Suggestion: repoint that arm at the material the task actually adds — the `✓` marker, the `N/M done` footer, `--milestone` — in both files, and drop the false "documented in neither" premise from the description.

- [test contract] t-1's second arm reads "if the extracted reader drops the changes.json `shipped` arm, the existing TestProgressShippedCountsDoneAndVerifiedDoesNot … fails once it is repointed at the shared reader". That test is black-box over `dross milestone progress --json` and already routes through `phaseIsDone`; it needs no repointing and fails today if the shipped arm goes. The conditional clause describes work that does not exist, and it hides a small real gap: after the extraction, the shipped arm is pinned only through the progress command — no arm asserts a `status = "shipped"` phase counts done in the status bar.
  Suggestion: drop the "once it is repointed" clause, and either add a status-surface shipped arm or say explicitly that status inherits it by construction from the single reader.

## NOTE

- [coverage] The t-1 scope call is correct and I verified it: status.go has four `readVerifyVerdict` call sites (158, 283, 334, 632) and only 158 — `renderMilestone` — is a done count. 283 refines the current-phase next-step hint, 334 is `spineIdle`, 632 is `pendingVerdicts`. Leaving those three to the deferred `reentry-signal-truth` phase does not leave a second done-count derivation inside `dross status`.

- [coverage] c-1 names milestone v1.4 literally, but t-4 verifies it via a "v1.4-shaped fixture". That is forced, not sloppy: hermetic_dross_read_test.go bans a test that hands the real `.dross` tree to a root walker, and its own docstring cites `TestProgressAgainstThisRepo` — this exact shape — as the failure it was written for. The repo data does back the premise (all 11 v1.4 phases carry `changes.json` `status = "complete"` and no verify.toml), so the real-repo confirmation is a manual run at verify time, not a test.

- [antipattern] assets/prompts/plan.md:11 also invokes `dross phase list` ("If unset, list phases via `dross phase list` and ask") and is not in t-5's consumer list. It does not go stale — the output is shown to a human to pick from, and the marker/footer only help — so no action is needed, but the omission should be a decision rather than a miss.

- [granularity] t-1 (5 files) and t-5 (6 files) trip the 5+ file threshold; neither is a split candidate. t-1's five are one package and one indivisible rewire; t-5's six are the prior review's deliberate consolidation and are all documentation/prompt text plus its own test file.

- [strengths] Every locked decision is honoured verbatim, including the negative ones: `list_scope` (bare command keeps global listing, and t-2 has an arm that fails if it stops), `done_marker` (the `✓ `/two-space prefix, the `N/M done` footer, and no `--json`), `history_fallback` (carried across with its own falsification arm in t-1), and `duplicate_slug` (silent first-occurrence-wins plus a doctor check that is a warning, matching doctor.go's warnings-do-not-gate-exit model).

- [strengths] Contracts are written as falsification arms naming a concrete surface — "a fixture phase with verify.toml verdict=\"pass\" and changes.json carrying no completion status makes the status bar read 1/1 instead of 0/1" — not as "tests pass". t-4 in particular states the disagreement it detects and which command reports what.

- [strengths] The three prior amendments landed correctly and introduced nothing new: t-1 now lists milestone_progress_test.go and points at a real test (declaration at line 97; the cited 93 is its doc-comment start); t-2's dedup-independence sentence is accurate — `phase.List` returns directory names, unique by construction, so the global footer genuinely cannot double-count regardless of t-3; and t-5's three prompt claims each check out against the files (spec.md:19 intersects raw list output, verify.md:212 asserts scaffolded-only and forbids it for ordering, milestone.md:17 forbids deriving doneness from it).

## Summary
No blockers — the decomposition, wave order and locked-decision fidelity are sound; the four flags are a criterion (c-4) verified one layer below the surface it names, an existing byte-exact ordering guard t-2 will break without saying so, and two contract arms whose wording gates less than intended.
