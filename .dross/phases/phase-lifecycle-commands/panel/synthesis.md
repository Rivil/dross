# Synthesis — phase-lifecycle-commands

Judge merge of three independent drafts (risk / mvp / verification). I authored none.

## Scores

Grades: A strongest, C weakest. One line per draft per dimension.

| Dimension | risk (7t/4w) | mvp (2t/1w) | verification (5t/3w) |
| --- | --- | --- | --- |
| **Criteria coverage** | A — every criterion an owning task; c-3 split 3 ways, c-4 a dedicated guard task | B — all four covered but c-4 is only test bullets bolted to t-1; rename is one dense bundle | A — coverage table, all four mapped; c-4 spread across all mutators |
| **Test-contract specificity** | A — names the exact failing test per regressed step (TestRenameNoPartialMoveOnCollision etc.); sharpest seams | B — "if X then test Y fails" bullets, correct but coarser; no partial-move-on-collision assertion | A− — weaker per-step naming, but the only draft that proves the byte-for-byte assertion is non-vacuous (harness self-test) |
| **Granularity** | C+ — over-decomposed; t-2 guard + t-6 deferred + t-7 numbering are seams, not independent deliverables, and inflate the wave count | C — under-decomposed; t-1 fuses pure helpers + two commands + c-4, t-2 fuses rename+deferred+guard+state; coarse test contract | A — balanced; pure helpers, shared-harness, one verb per mutator task |
| **Wave correctness** | B− — dependencies honored but 4 waves is over-sequenced; t-7 isolated in wave 4 for a test-only criterion | C — single wave hides that helpers must exist before consumers; leans on "output-dep only" to parallelize same-file edits | A — clean: independent infra (helpers+harness) → move(carries shared plumbing) → insert/rename in parallel |

**Skeleton: `verification` (5 tasks / 3 waves).** It has the cleanest dependency-correct wave split and the only provably-non-vacuous byte-for-byte harness, while staying balanced on granularity. risk supplies the sharpest contracts and an extra failure-mode (partial-move-on-collision); mvp supplies the smallest viable framing and the migrate.go reuse — both graft cleanly onto verification's spine.

## Merged plan

Display format per task: `t-N  [origin]  title — covers — files — depends`, then a contract line. Origin tags name which draft(s) the task/improvement came from.

```
Phase phase-lifecycle-commands — 5 tasks across 3 waves

Wave 1
  t-1  [verification, +risk]  Pure array-order helpers
       covers:  c-1, c-2 (infra for c-3)
       files:   internal/phase/phase.go, internal/phase/phase_test.go   (location: see D5)
       contract: InsertRelative([a,b,c],"x",anchor="b",before=false)→[a,b,x,c]; before=true→
                 [a,x,b,c]; anchor absent → ErrAnchorNotFound, NOT an end-of-array append; Move
                 preserves every non-moved element's relative order; MoveRelative to a slug's own
                 position returns the slice unchanged (no-op path). [graft risk] RenameInArray
                 replaces old in place preserving its index — length+order assertion catches an
                 append. Table-tested in isolation, no git/fs fixture.

  t-2  [verification]  Byte-for-byte snapshot test harness
       covers:  c-1, c-2, c-3 (substrate)
       files:   internal/cmd/phase_lifecycle_test.go
       contract: snapshotPhases(t,root) → slug→sha256 over (relpath,bytes) of every file under
                 phases/<slug>/ plus the phase's milestone array entry and spec.phase.id;
                 assertUntouched(before,after,except...) fails on any non-excepted diff. Self-test
                 mutates one byte of a bystander spec.toml and asserts the hash flips AND
                 assertUntouched reports it — proves the guarantee is enforced, not vacuous.

Wave 2 (depends t-1, t-2)
  t-3  [verification, +risk]  `dross phase move` + shared lifecycle plumbing
       covers:  c-2, c-4
       files:   internal/cmd/phase.go, internal/cmd/phase_lifecycle.go,
                internal/cmd/phase_lifecycle_test.go
       depends: t-1, t-2
       contract: phase_lifecycle.go houses verbs + the helpers move/insert/rename share:
                 anchor-flag validation (exactly one of --after/--before — both AND neither each
                 get their own assertion), loadCurrentMilestone, refuseIfShipped (`git ls-remote
                 --heads origin phase/<slug>` non-empty ⇒ open-PR window), and the idempotent
                 no-op check ordered BEFORE target-exists. move reorders the milestone phases
                 array via t-1 and writes nothing else: after `move c --after a` the array is
                 [a,c,b] AND assertUntouched(except=nothing) passes (zero dirs touched — strongest
                 byte-for-byte form). Self-move prints "already there" and leaves the milestone
                 .toml byte-identical. Anchor-not-found surfaces. `phase number b` reflects b's new
                 slot; `dross validate` exits 0. Move on a shipped phase refused via refuseIfShipped
                 (see D2). [graft risk t-7] assert state.json + every spec.toml hold NO numeric
                 ordinal field — pins "recomputed, never stored".

Wave 3 (depends t-3)
  t-4  [verification, +risk]  `dross phase insert`
       covers:  c-1, c-4
       files:   internal/cmd/phase.go, internal/cmd/phase_lifecycle.go,
                internal/cmd/phase_lifecycle_test.go
       depends: t-1, t-2, t-3
       contract: scaffold the phase (dir + phase/<slug> branch, reusing phaseCreate's machinery)
                 then splice with t-1 InsertRelative at the anchor — `insert "P" --after a` → array
                 [a,<P>,b,c]; if placement falls through to the tail the index-of-new-slug
                 assertion fails. assertUntouched(except=<P>) proves a/b/c dirs, spec.phase.id,
                 branch refs, array entries byte-for-byte unchanged. [graft risk] a title slugging
                 to an existing phase is REFUSED (no phaseCreate auto-suffix) and leaves NO stray
                 directory — both the refusal and a stray-dir check assert. Reuses t-3 flag
                 validation (both/neither errors here too). `dross validate` exits 0; `phase
                 number b` shows b shifted +1.

  t-5  [verification, +risk, +mvp]  `dross phase rename`
       covers:  c-3, c-4
       files:   internal/cmd/phase.go, internal/cmd/phase_lifecycle.go,
                internal/cmd/deferred.go, internal/cmd/phase_lifecycle_test.go   ([+risk] deferred.go)
       depends: t-1, t-2, t-3
       contract: rename phases/old→phases/new; rewrite spec.phase.id reusing migrate.go's
                 rewritePhaseID [mvp+verification] (one id-rewrite path for validate's
                 dir==id invariant); swap the milestone array entry in place via t-1 RenameInArray;
                 re-point every OTHER spec's [[deferred]] whose Target==old → new via the existing
                 collectDeferred scan [graft risk t-6] — items targeting other slugs untouched
                 (over-broad rewrite fails TestRenameLeavesOtherDeferredTargets); `git branch -m
                 phase/old phase/new` when that local ref exists (no remote touch); update
                 state.current_phase when the renamed phase is current. refuseIfShipped (t-3)
                 blocks a phase with a live origin branch, leaving branch + phases/old on disk
                 unchanged. [graft risk] the target-exists check runs BEFORE the directory move —
                 if it runs after, phases/old has already vanished; TestRenameNoPartialMoveOn
                 Collision catches the leftover. Self-rename prints "already there" before the
                 target-exists check and writes nothing. assertUntouched(except=old/new) proves
                 siblings byte-for-byte; `dross validate` exits 0 (no dangling deferred target).
```

### Coverage

| criterion | tasks |
| --- | --- |
| c-1 | t-1, t-2, t-4 |
| c-2 | t-1, t-2, t-3 |
| c-3 | t-2, t-5 |
| c-4 | t-3, t-4, t-5 |

c-4 has no production code — ordinals already derive from array position (`phase.DisplayNumber` feeds `phase number`, status, PR title, version patch). It is proved by each mutator asserting a sibling's number shifts, plus the grafted no-stored-ordinal assertion in t-3.

## Disagreements

**D1 — Overall granularity (2 vs 5 vs 7 tasks).** risk decomposes per failure-surface (7 tasks, splitting the ship-guard, deferred re-point, and numbering into standalone tasks); mvp collapses to two machinery clusters (array-family + identity-family); verification lands at five. *Provisional default: 5 (verification).* Matters because granularity sets the wave count and how much a single red test localizes a break — risk's split localizes best but pays in coordination overhead on same-file edits (all three drafts admit move/insert/rename share internal/cmd/phase.go); mvp's two fat tasks give incoherent test contracts. Five is the balance point.

**D2 — Ship-state guard on `move`.** mvp applies refuseIfShipped to rename ONLY; risk and verification apply it to move as well. *Provisional default: guard both move and rename.* The locked `inflight_guard` decision says "move/rename may operate on the current in-flight phase ... EXCEPT when shipped" — the wording is explicit about move, so honor the locked decision over mvp's narrower technical reading (that reorder-only move can't orphan a PR). Matters: skipping it on move would let move mutate a shipped phase's current-phase state mid-PR.

**D3 — Deferred re-point: own task or folded into rename?** risk isolates it as t-6 ("dangling deferred target is a distinct failure mode"); mvp and verification fold it into the single rename task. *Provisional default: folded into t-5 (mvp+verification majority).* But risk's two specific assertions are grafted in (re-point via collectDeferred + leaves-other-targets-untouched) and risk's file `internal/cmd/deferred.go` is added to t-5. Matters: folding keeps the wave count down; isolating would give the dangling-target regression its own red test. If t-5's contract proves too dense in practice, splitting per risk is the fallback.

**D4 — Numbering c-4: standalone task or test-only?** risk makes it a standalone wave-4 task (t-7) to own proving no new move/insert path persists an ordinal; mvp and verification deliver it as test bullets on the mutator tasks. *Provisional default: no standalone task (mvp+verification), but graft risk's sharpest assertion* — "state.json + every spec.toml hold no numeric ordinal field" — into t-3. Matters: the real c-4 risk is a regression where a mutator quietly stores an ordinal; the grafted assertion captures risk's intent without a dedicated wave.

**D5 — Pure-helper package: `internal/phase` vs `internal/milestone`.** risk and mvp put InsertRelative/Move in internal/phase/phase.go; verification puts them in internal/milestone/milestone.go (arguing positioning logic operates on the milestone's phases array). *Provisional default: internal/phase/phase.go (risk+mvp majority, and the ordinal source DisplayNumber already lives there).* Matters because it sets t-1's file and test path, and which package owns the phases-array type. UNVERIFIED against source — confirm where the milestone `phases []string` array type actually lives before locking; if it lives in internal/milestone, switch to verification's location.

**D6 — Byte-for-byte check: shared harness task or inlined per test?** verification makes snapshotPhases/assertUntouched a first-class wave-1 task (t-2) with a self-test; risk and mvp inline an ad-hoc "did the dir change" check inside each command's test. *Provisional default: keep the harness as a task (t-2).* Matters: inlining lets each mutator's author write a weaker, possibly vacuous assertion; one self-tested hashing harness makes the hardest contract (byte-for-byte untouched, recurring across c-1/c-2/c-3) uniform and provably enforced. Cost is one extra wave-1 task that mvp's lens would reject as structure.
