# Plan Review — lane-selector-preview

Reviewed: 2026-09-02
Plan: 8 tasks across 4 waves

## BLOCKING

- [wave-order] t-7 is `wave = 3` and carries `depends_on = ["t-2", "t-5", "t-6"]` — but t-6 is
  also `wave = 3`. `depends_on` is defined as "task ids in **lower** waves"
  (`assets/prompts/plan.md:37`), and the invariant is enforced in the tooling this repo ships:
  `internal/phase/plan_edit.go:219` ("a dependent stays in a wave strictly greater than its
  dependency's") with the refusal at `plan_edit.go:286-287`. Any `dross task move` touching t-7
  will be refused, and the wave boundary that `execute.md:314` uses to pick the checkpoint
  posture is wrong for the whole of wave 3.
  Suggestion: move t-7 to wave 4. See the next finding — it wants to be there anyway.

## FLAG

- [wave-order] t-7 (wave 3) documents `--json`, which t-8 (wave 4) implements. Its own contract
  makes this concrete: `TestReadmeDocumentsThePreviewVerb` requires the row to name `--json`, so
  the test goes green on a README sentence describing a flag that does not exist yet. For one
  task the docs are ahead of the binary — the exact drift r-01 exists to catch.
  Suggestion: t-7 to wave 4 with `depends_on = ["t-2", "t-6", "t-8"]`, or move the `--json`
  sentence (and that assertion) into t-8 and leave t-7 documenting only what t-5/t-6 shipped.

- [antipattern/files] t-8's `files` omits `internal/cmd/lane_preview.go`. "Adds `--json` to the
  preview subcommand" means registering a flag on the cobra command t-5 constructs in
  `lane_preview.go`; t-8 lists only `lane_preview_json.go` and its test. t-6 lists
  `lane_preview.go` for precisely the same reason (adding `--no-probe`), so this is an
  inconsistency, not a deliberate design.
  Suggestion: add `internal/cmd/lane_preview.go` to t-8's files.

- [granularity] t-4 bundles a refactor of the live run path with new preview-only code: it splits
  `pickRemoteTarget` out of the existing `selectRemoteTarget` (`internal/cmd/remote_pool.go:22`),
  which every remote `dross test` run goes through. Unlike t-1 — which carries an explicit
  regression clause naming six existing tests that must still pass — t-4's seven contract items
  are all about preview. Nothing pins that the run still lands on the same target through the
  rewritten announcing wrapper.
  Suggestion: add a regression clause to t-4 ("if the split changes the run's target selection,
  existing `TestX` fails", citing the `remote_pool_test.go` cases), or lift the
  `selectRemoteTarget` split into its own wave-1 task with its own contract.

- [test-contract] Two of t-4's contract items assert *rendered* output —
  `TestUnresolvedHostNeverRefusesALane` ("the web lane renders `unresolved` naming the configured
  host", "the string `refused` appears nowhere") and `TestUnprobedHostNeverClaimsLocal` ("under
  `--no-probe` a lane ... renders `unresolved`, not `local`"). At wave 1 there is no renderer and
  no `--no-probe` flag: t-5 builds the human rendering (wave 2) and t-6 adds `--no-probe`
  (wave 3). Either `previewHost` returns those strings itself, or these tests cannot be written
  where the plan puts them.
  Suggestion: say explicitly that `previewHost` returns the rendered locality string (in which
  case the contract holds as written), or move those two assertions into t-6.

- [coverage] c-1's "spawns nothing: no lane command, no prepare, no remote sync" is asserted
  once, by t-5's `TestPreviewSpawnsNothing` — written in wave 2, before any remote code path
  exists in preview. t-6 then wires `previewHost`, which opens an ssh probe by default. Nothing
  in t-6's four contract items re-asserts that probing did not also drag in `syncTreeTo` or
  `runLanePrepare`. t-4's description says "It never syncs the tree" but no test holds it.
  Suggestion: add one contract item to t-6 — a spawn/rsync recorder over a *probing* preview,
  showing the probe is the only remote traffic.

- [coverage] t-3 declares `covers = ["c-1"]`, but all five of its tests evidence the locked
  `bare_preview_default` decision (untracked expansion, deletions retained, unquoting, rename
  destination, clean tree). c-1's text is about `dross test lane preview --files a.go b.go` — it
  says nothing about the working tree. The locked decision has no criterion of its own, so at
  verify time t-3's tests will map to a criterion whose wording does not describe them.
  Suggestion: leave the plan alone but note it in verify.toml — t-3's evidence attaches to
  locked `bare_preview_default`, with `TestBarePreviewTakesTheWorkingTree` (t-5) as the c-1 link.

## NOTE

- [granularity] t-2 is small — one source file, two `Printf` calls reusing the existing
  `printLaneTemplate` (`internal/cmd/test_lane.go:689`), plus three tests. On the size heuristic
  it reads as a merge candidate, but there is no honest partner: its wave-1 neighbours are all
  preview plumbing and t-7 depends on it. Standalone is the right call here.

- [antipattern] t-5 declares the full `previewReport` struct including `consent`/`locality`
  fields it does not fill, leaving them dead until t-6. This is stated deliberately and it avoids
  a struct rewrite in wave 3 — recorded only so the intermediate commit's unused fields are not
  read later as an oversight.

- [files] t-7's description says it extends the README row at `README.md:220`, but that row's
  first cell literally reads `dross test lane {add,list,edit,remove,install}`. The description
  never says to update that cell, and `TestReadmeDocumentsThePreviewVerb` only requires `preview`
  to appear somewhere in the row — so the command-list cell can stay stale and the test still
  passes.

- [forbidden-actions] No violations. `runtime.mode = "native"`, the only project rule is r-01
  (`make install` before relying on a prompt or Go change), and no global
  `~/.claude/dross/rules.toml` exists. r-01 does apply to verification: t-7's README tests and any
  hand-check of the new verb need a rebuilt, re-installed binary.

- [strengths] Test contracts are the strongest part of this plan. Every one of the 34 items is
  written as a failure mode with a named test function and a concrete fixture — "if `-uall` is
  dropped, `TestWorkingTreeFilesExpandsUntrackedDirectories` fails: a new directory holding two
  untracked `.go` files must return both FILE paths". Not one "tests pass" or "covered by
  integration" in eight tasks.

- [strengths] t-1 turns c-1's "same code path" claim from an assertion-by-inspection into a
  mechanical check: the recorded `spawnLocal` argv from a real `dross test --files` run must be
  byte-identical to `plan.Lanes[0].Line`. That is the right shape for a criterion whose whole
  value is that preview and gate cannot drift.

- [strengths] t-4's two negative tests pin honest uncertainty rather than convenient defaults: an
  unprobed or unreachable host may never render as `local` (claiming a fallback no probe proved)
  nor as `refused` (a judgement preview is not allowed to make). That is the failure mode this
  feature would most plausibly ship with, and reusing the existing `laneLocality` with `host=""`
  instead of adding `laneSite` enum values keeps one vocabulary.

## Summary

Structurally sound and unusually well-specified, but t-7 sits in the same wave as a task it
depends on — a violation the repo's own plan tooling enforces — and it documents a flag that
lands a wave later; fix the t-7 placement and the rest are refinements.
