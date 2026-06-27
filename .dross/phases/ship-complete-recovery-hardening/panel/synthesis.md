# Synthesis — ship-complete-recovery-hardening

Cold judge over three independent drafts (risk / mvp / verification). Adjudicated
against the cited source (`internal/cmd/{phase,ship_recover,status}.go`,
`assets/prompts/ship.md`, prompt-test conventions in `internal/cmd/`).

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| risk | All 6 covered; c-3 dual-owned (t-1 guard owner, t-3 verifies complete path) — sharpest ownership map | Strongest *regression framing*: names existing tests, asserts main SHA byte-for-byte unchanged, explains the exact failure each guard catches | 4 tasks; c-2 folded into t-1 as a named contract (defensible), resume bundled into docs t-4 with c-5 (mixes two criteria/files) | 3 waves — over-serialized: t-4 docs sequenced behind t-3 to "match shipped flag name", but the flag name is locked in spec |
| mvp | All 6, but c-1+c-2+c-3 all land in one task (t-2); c-4 resume edit has **no guard test** | Good but coarsest: c-2 partial-restore buried as sub-assertion (b) of the complete case; lighter on fixtures | 4 tasks; under-granular — t-2 carries three criteria; resume.md folded into status task t-3 with no own guard | 2 waves, correct: only `complete --recover` (t-2) depends on t-1; docs/status parallel in wave 1 |
| verification | All 6, each with its **own** independently-failing guard; c-2 and c-4-resume isolated | Strongest overall: contracts written first, named fixtures (`divergedCompleteFixture`, two-phase), `ls-tree` assertions, cites real `repoRootFromTest`/normalise pattern | 6 tasks; finest — but t-6 is a test-only task editing the file t-1 just wrote (borderline churn) | 2 waves, correct and parallel-friendly: t-5,t-6 both depend on t-1 only |

**Skeleton: verification.** It has the most precise, write-the-test-first contracts,
the correct 2-wave dependency shape, and its file list matches the repo's real
`*_prompt_test.go` convention (`ship_prompt_test.go`, `resume_prompt_test.go`) —
which risk's `prompt_docs_test.go` violates. Grafts below pull risk's byte-for-byte
non-destructive framing and mvp's `git status --porcelain`-after-add delta detail, and
fold verification's over-fine c-2 test-only task back into t-1.

## Merged plan

5 tasks across 2 waves. `--recover` flag name and the shared-routine / delta-only-commit
shape are all locked decisions in spec.toml; the plan honors them.

### Wave 1

**t-1 — Extract delta-gated shared recovery routine** `[verification + risk(c-2) + mvp(delta-detail)]`
- files: `internal/cmd/ship_recover.go`, `internal/cmd/ship_recover_test.go`
- covers: c-6, c-2
- contract:
  - Lift `shipRecover`'s RunE body (fetch → reset --hard origin/main → restore `.dross/`
    → commit, ship_recover.go:59–155) into an internal `runDrossRecovery(repoDir, root,
    p, s, phaseID, preMergeSHA)`; `ship recover` now delegates. Gate the commit on a real
    staged `.dross/` delta vs origin — check `git status --porcelain` *after* `git add`
    and *before* `state.Touch` (touch-first would always manufacture a delta and make the
    c-6 no-op unreachable [mvp/verification judgment]). Reuse the locked message
    `chore(dross): restore .dross/ after squash-merge for <id> + merge`.
  - `TestShipRecoverIdempotentNoOp` (c-6): in-sync fixture (local main == origin/main,
    `.dross/` intact) → exit 0, "nothing to restore / already in sync" message, and
    `git rev-list --count origin/main..HEAD == 0`. Drop the delta gate → phantom empty
    commit, count becomes 1 → fails.
  - `TestRecoverRestoresAllPhases` (c-2, named contract): fixture whose pre-merge HEAD holds
    `.dross/phases/01-x/spec.toml` AND `.dross/phases/02-y/spec.toml`, origin/main has
    neither; after recovery `git ls-tree -r --name-only HEAD` contains BOTH. A current-phase-only
    restore drops 02-y → fails (guards the partial-restore regression).
  - Existing `TestShipRecoverHappyPath` still commits exactly 1 on a real delta;
    `TestShipRecoverRefusesDirtyTree` / `RefusesWrongBranch` stay green through the
    extraction — proving the c-3 ship-path guards survived [risk].
- depends_on: —

**t-2 — Detect stale completed-on-phase-branch in `dross status`** `[risk+mvp+verification]`
- files: `internal/cmd/status.go`, `internal/cmd/status_test.go`
- covers: c-4
- contract:
  - Add `staleCompletedState(root, repoDir) → (phaseID, bool)`: true when HEAD is
    `phase/<id>` AND branch-local state records `completed <id>` AND origin/main carries
    no `completed <id>` record. Status prints a warn-only
    `stale: on phase/<id> but state reads completed — reconcile: …` line and does NOT
    render the phase as done. Read-only — no state write in the path (locked: warn-only,
    never auto-mutate).
  - `TestStatusSurfacesStaleCompletedState`: HEAD on phase/x, history has `completed x`,
    origin/main lacks it → output contains the `stale:` line + reconcile pointer and does
    not present the phase as finished.
  - `TestStatusNoStaleOnMain` (false-positive control [risk]): on main, OR origin/main
    already carries `completed x` (genuinely merged) → the warning is absent. A detector
    keying only off local state without checking origin would warn here → fails.
- depends_on: —

**t-3 — Document stale-state reconcile in `resume.md` + guard test** `[verification]`
- files: `assets/prompts/resume.md`, `internal/cmd/resume_prompt_test.go`
- covers: c-4
- contract:
  - Add a §1 drift case: "on phase/<id> but state reads completed → stale, not done" with
    its reconcile command and a never-auto-mutate caveat; `dross resume` surfaces it via
    the `dross status` call it already makes (no second Go detection path — resume is a
    prompt, not a cobra command).
  - `TestResumePromptStaleStateSection` (mirrors `secure_prompt_test.go`'s
    `repoRootFromTest` + normalise + needle pattern): asserts resume.md contains the
    stale-completed phrase, the reconcile pointer, and "never auto-mutate"; removing the
    section fails exactly those needles.
- depends_on: —

**t-4 — Recovery cookbook in `ship.md` + guard test** `[mvp+verification; file-name corrected]`
- files: `assets/prompts/ship.md`, `internal/cmd/ship_prompt_test.go`
- covers: c-5
- contract:
  - Add a `## Recovery` section covering all three mid-merge failure states (ff-abort /
    diverged main / dirty post-push tree), each naming the exact dross command
    (`dross phase complete --recover` and/or `dross ship recover`), with zero manual
    `.dross/` surgery.
  - `TestShipPromptRecoverySection`: asserts all three failure-state phrases + both commands
    are present, AND asserts the absence of manual-surgery instructions (no
    `checkout … -- .dross/`, no `git add .dross/` presented as a user step). Dropping a
    state's recipe or reintroducing manual `.dross/` surgery fails the matching assertion.
  - Test file is `ship_prompt_test.go` (matches the repo's `*_prompt_test.go` convention),
    NOT risk's `prompt_docs_test.go`.
- depends_on: —

### Wave 2

**t-5 — Wire `--recover` into `phase complete` (heal-or-pointer)** `[risk+mvp+verification]`
- files: `internal/cmd/phase.go`, `internal/cmd/phase_test.go`
- covers: c-1, c-3
- contract:
  - Add a `--recover` bool to `phaseComplete`. At the `merge --ff-only origin/<main>`
    hook (phase.go:314), the ff abort *is* the divergence signal [mvp/verification]: with
    `--recover`, delegate to t-1's `runDrossRecovery` (reset-to-origin + restore `.dross/`
    in one shot); without it, return a one-line pointer error naming `--recover` /
    `dross ship recover` and change nothing. The pre-existing clean-tree check (phase.go:278)
    stays ahead of any reset.
  - `TestPhaseCompleteDivergedNoFlagStops` (c-1): diverged-main fixture, no flag → error
    mentions `--recover`, and `git rev-parse main` is **byte-for-byte unchanged** [risk —
    a refusal that errors *after* a partial reset would pass a weaker "an error returned"
    contract while having already corrupted main].
  - `TestPhaseCompleteRecoverHeals` (c-1): same fixture, `--recover` → exit 0, full
    cumulative `.dross/` tree on HEAD, local `phase/<id>` deleted, tree clean, zero manual git.
  - `TestPhaseCompleteRecoverRefusesDirty` (c-3): diverged + dirty → aborts with the file
    named, `git rev-parse main` byte-identical; drop complete's pre-recovery dirty guard
    and `reset --hard` destroys the uncommitted file → fails. (t-1 keeps the ship-recover
    dirty/wrong-branch guards green — no duplicated guard logic [risk].)
- depends_on: t-1

### Coverage
c-1 → t-5 · c-2 → t-1 · c-3 → t-5 (+ t-1 keeps ship-path guards green) · c-4 → t-2, t-3 ·
c-5 → t-4 · c-6 → t-1. All six covered; no locked decision contradicted.

## Disagreements

### D1 — Where the c-2 cumulative-restore guard lives
- **verification**: its own test-only task (t-6), `ship_recover_test.go` only, depends t-1.
- **risk**: a named regression contract folded *into* t-1 (rejects a standalone task —
  "a follow-up task editing the same function t-1 just wrote serializes churn for no
  isolation gain").
- **mvp**: a sub-assertion (case b) of the complete task t-2.
- **Provisional default**: fold into t-1 as an explicit, separately-named contract
  (`TestRecoverRestoresAllPhases`), per risk.
- **Why it matters**: c-2 is a property of the shared recovery routine that t-1 authors,
  so the partial-restore regression breaks *in t-1's code*. A test-only wave-2 task editing
  the file t-1 just wrote is churn with no isolation gain; mvp's burial inside the complete
  case makes the guard harder to see fail independently. The fold keeps it an individually
  named, independently-failing contract without a phantom task. (Skeleton was verification;
  this is the one place the merge overrides it.)

### D2 — The `resume.md` stale-state surface
- **verification**: its own task (t-3) with its own guard test (`resume_prompt_test.go`).
- **risk**: bundled into the docs task t-4, alongside `ship.md`, covering c-4 + c-5 together.
- **mvp**: a one-line edit folded into the status task (t-3) with **no guard test**.
- **Provisional default**: keep it a standalone task with its own guard test, per verification.
- **Why it matters**: the `stale_state_surface` locked decision names resume as a
  first-class stale-state surface co-equal with status. An unguarded doc edit (mvp) can
  rot silently with nothing failing; bundling with `ship.md` (risk) mixes two criteria and
  two prompt files into one commit and one test file. A dedicated guard makes the c-4 resume
  signal independently regression-protected.

### D3 — Wave count / docs sequencing
- **risk**: 3 waves — docs (t-4) sequenced *behind* the complete task so the documented
  flag name matches what actually shipped.
- **mvp / verification**: 2 waves — docs authored in parallel in wave 1.
- **Provisional default**: 2 waves, docs in wave 1, per mvp/verification.
- **Why it matters**: the `--recover` spelling is fixed by the `recover_gate` locked
  decision in spec.toml, so the cookbook cannot document a name the implementation later
  changes — risk's name-drift rationale is moot against a locked flag. The extra wave is
  needless serialization that costs parallelism for no safety.

---
*Non-divergence note (consensus, recorded for traceability):* both mvp and verification
branch complete's recovery on the `merge --ff-only` failure (phase.go:314) rather than
adding separate divergence-detection plumbing — the ff abort already is the signal. Adopted
in t-5, not treated as a disagreement.
