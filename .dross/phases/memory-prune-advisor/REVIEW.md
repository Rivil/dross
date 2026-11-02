# Plan Review — memory-prune-advisor

Reviewed: 2026-10-30
Plan: 11 tasks across 5 waves

## BLOCKING
(none)

Coverage is complete: every criterion from c-1 to c-9 appears in at least one `covers`. No task contradicts a locked decision. The incremental append-only rescan in t-5 fits `turn_counts`. The `ceil(bytes/4)` price on the index-line delta in t-6 fits `cost_model`. The single-doc ≥50% 8-shingle rule in t-3 matches `dup_threshold`. No task conflicts with r-01: t-11 runs `make install` first, and runtime is native.

## FLAG
- [wave-order] t-5 depends on t-1, but both are in wave 1 and t-5 has no `depends_on`. t-5 reads `<slug>/*.jsonl`, which needs t-1's `Slug` and `ClaudeDir`. Its cache path `.dross/memory/turns.json` is a constant defined in t-1's `paths.go`. Its own test `TestIgnoreEntriesMatchAdvisorPaths` compares against "the memadvise path constants", which are t-1's. If wave 1 runs in parallel, or t-5 runs first, t-5 either won't compile or will redefine the constants. The drift test exists to prevent exactly that.
  Suggestion: add `depends_on = ["t-1"]` to t-5 and move it to wave 2, or move `paths.go` and `Slug`/`ClaudeDir` into a task that precedes it.

- [antipattern: missing guard] t-5 and t-8 write into `.dross/memory/`. t-8's snapshots are full copies of the memory bodies. These files are only ignored where `ensureDrossGitignore` has run, and today only `init.go:100` and `onboard.go:119` call it. Every repo onboarded before this phase will have `.dross/memory/turns.json` and `.dross/memory/snapshots/<ts>/*.md` stageable. dross auto-commits `.dross` bookkeeping ("auto-commit bookkeeping before…"), so private memory content can end up in a commit. In a public repo it can be pushed. turns.json also carries per-file keys for transcripts of every other project, which leaks those project names. The `writers.go` header says MachineLocal is valid only because of the seed. Nothing in the plan makes Gather or Apply check `git check-ignore`, or re-seed the ignore entry, before writing. No test_contract covers an existing repo whose `.gitignore` lacks the line.
  Suggestion: in t-5 and t-8, before the first write, ensure the ignore line is present (or refuse to write while `git check-ignore` says the path is stageable). Add a contract for a repo whose `.gitignore` predates the seed.

- [test-contract] t-11's `TestFlowCostCopiesNoContent` contradicts the schema it guards. It allows string values only under `binary_version, session_id, measured_at, filter, method`. But `TestFlowCostRecords` requires the string scenario keys `repo_shape`, `wave_position`, `flow_start`, `flow_end` and `jq_method`. t-11's own contract requires `[scenario] turn_cache = "cold"`. `flowLeg` carries the string arrays `turn_tools` and `dross_calls`, plus the strings `verdict`, `gate_lead` and `fixture_tree`. As written, the test either fails on every existing flow-cost.toml or gets weakened during execution to pass.
  Suggestion: state exactly which values the "no content" rule scans (for example leg-level free-text scalars only), and list the scenario keys and string-array fields it exempts.

- [test-contract] t-11's last contract says "a README grep test fails" without naming the test. The file that would hold it, `internal/cmd/readme_doc_test.go`, is not in t-11's `files`. Every other README guard there is named (`TestReadmeDocumentsDebugSessions`, `TestReadmeDocumentsContextNudge`, …).
  Suggestion: name the test (for example `TestReadmeDocumentsMemoryAdvisor`) and add `internal/cmd/readme_doc_test.go` to t-11's files.

- [antipattern: one task that should be two] t-11 combines two unrelated pieces of work. One is a live-session measurement: two `/dross-status` runs on two binaries, jq extraction, and a new `checkMemoryPruneAdvisorFlow`. The other is README rows for `dross memory advise|apply`. The README rows depend only on t-10's command surface. Holding them until wave 5 ties docs to a measurement that needs a human-driven session and can stall.
  Suggestion: move the README rows into t-10, or into a separate wave-5 task that depends only on t-10.

- [granularity] t-5 touches 8 files across three packages (`memadvise`, `cmd`, `secretscan`) plus the repo `.gitignore`. It combines a streaming transcript parser, an incremental cache with truncation and atomic writes, ignore seeding and writer registration. It has 13 contracts, the most of any task.
  Suggestion: split the turn counter (`turns.go`) from the cache, ignore seeding and writer registration (`turncache.go`, `gitignore.go`, `.gitignore`, `writers.go`).

- [locked-decision ambiguity] `status_surface` (locked) says status prints the top 3 suggestions, plus a "+N more" line, and that "integrity issues count toward the list". t-7's `Render(report, limit)` also prints a separate "integrity block", and t-9 calls `Render(report, 3)`. The plan doesn't say whether that block falls inside the 3-item cap. If it prints in addition to the top 3, the status surface exceeds the locked shape. Duplicate `name:` slugs (t-2) are an integrity issue but never become a Suggestion, so nothing counts them toward the list. No t-7 or t-9 contract pins either point.
  Suggestion: state in t-7 and t-9 that status's integrity output counts against the cap (or is just the counts), and add a contract for a fixture where the integrity issues alone exceed 3.

- [test-contract] c-1 requires each suggestion to name "the file, the reason and the action". `TestRenderSuggestionLine` (t-7) fails when ID, file, action word or `$X.XX/mo` is missing, but not when the reason is missing. A render that drops the reason passes.
  Suggestion: add the reason to `TestRenderSuggestionLine`'s list.

- [antipattern: unbounded hot path] With a cold or corrupt cache, `dross status` scans every project's transcripts from the last 30 days during Gather. That is a slice of 2.2 GB across 36 projects. It is synchronous, and the spec cites this as the reason for caching. t-11 measures one cold run, which is the right worst case. But no task bounds it: there is no time or byte budget, and status doesn't fall back to "turns not yet counted". A first `dross status` after install, or after a corrupt cache, can block for the length of a full scan.
  Suggestion: decide whether status may defer a cold count, by printing suggestions without $ or a "counting…" line, to `dross memory advise`. Either way, pin the behaviour in a t-5 or t-9 contract.

## NOTE
- [reuse] `internal/ctxnudge/transcript.go` already parses main-agent assistant lines (`isSidechain`, `model`, usage) and opens with `O_NONBLOCK` against FIFOs. `CLAUDE_CONFIG_DIR`→`~/.claude` resolution is already written 4+ times (`hooks.go:userSettingsPath`, `statusline.go`, `reviewer_agent.go`, `install.go`). t-1 and t-5 add yet another copy of each. Worth a look at execution time; not a plan defect.
- [c-6 fidelity] c-6 says "zero agent round-trips". The gate t-11 relies on (`checkLegPair`) fails only when the after leg makes a dross call the before leg did not. `agent_turns` is recorded but not compared. This follows the 2026-10-08 decision noted in `TestFlowCostRecords`, but the criterion's wording asks for more than the gate proves. Verify should say so explicitly.
- [cost model] `CacheReadUSDPerMTok` is a single hard-coded constant. The cache-read rate differs by model, and the constant will drift from published pricing. This is acceptable under `cost_model`'s "don't over-sell" framing, but the rendered figure should name the rate it assumes.
- [locate] `Slug` is built from the EvalSymlinks'd repo top level. A Claude session started from a subdirectory or a git worktree keys its memory dir on that cwd instead. In that case `Locate` returns ok=false and the advisor goes silent, by design, with no hint about why.
- [placement] t-9 says "after the debug: line and before the reentry footer", but the conditional `actions:` block sits between those two. The plan doesn't say whether `memory:` goes before or after `actions:`.
- [strength] The test contracts are unusually falsifiable. Each names the input that breaks it: the exact 50%/49% shingle boundary, `until 2026-10-29` vs `-30` as a strict boundary, a 5 MB single-line tool result, U+2019 apostrophes, `NEVERTHELESS` as a whole-word negative, and a mutation-style formula check ("1e3/1e6 slip").
- [strength] Hostile filesystem inputs are anticipated throughout. FIFOs are skipped within 1s, symlinked `memory/` and symlinked dead-link targets are refused, `../../.ssh/id.md` index targets are treated as dead, Apply re-checks guard phrases on the live file, and MEMORY.md is rewritten atomically with an abort when it changed concurrently.
- [strength] c-3 and c-6 are enforced structurally rather than argued. The memory dir's sha256 is checked across Gather, `dross status` and `dross memory advise`. `status.md` is pinned unchanged by a prompt test, and the after leg reuses the existing `checkLegPair` gate.

## Summary
The plan is sound and fully covers the spec. Fix three things before executing: the undeclared t-5→t-1 dependency, the `.dross/memory/` snapshot and turn-cache leak in repos onboarded before this phase, and t-11's self-contradicting no-content test.
