# Panel synthesis — milestone-lifecycle-close

Judged cold: none of the three drafts below are mine. Scores are against the spec's
7 criteria and 5 locked decisions, cross-checked against the current code
(`internal/cmd/milestone.go`, `milestone_stale.go`, `phase.go`).

## Scores

| Draft | Dimension | Score | Judgment |
|---|---|---|---|
| risk | criteria coverage | 8/10 | 7/7 with a deliberate seam task, but never contracts locked `stacked_child_status` — a child merged into its parent is untested anywhere in the draft. |
| risk | test-contract specificity | 9/10 | Best negative assertions in the panel ("does NOT contain `is not merged into`", byte-identical toml, ordering pin t-5(c)); nearly every contract names the implementation it kills. |
| risk | granularity | 7/10 | 8 tasks, mostly clean, but c-3 (t-5) is split off from c-1/c-2 (t-1) despite both editing `milestoneFinalize` and the same `milestone_finalize_test.go` — an artificial wave-2 hop. |
| risk | wave correctness | 8/10 | 3 waves, dependencies real and one honestly labelled as serialization not data-flow; t-8's dependency on t-7 is spurious (it asserts no prompt text). |
| mvp | criteria coverage | 6/10 | 7/7 on paper, but no README task at all while adding subcommands under an existing README grep guard (`TestReadmeMilestoneRowDocumentsBaseFlag`, `milestone_narration_test.go:61`), and no coverage of `stacked_child_status` or of `milestone prune` as the destructive consumer. |
| mvp | test-contract specificity | 6/10 | Contracts are outcome-shaped and thinner (3 per task); the c-5/c-6 merge leaves one task with two predicates and no fixture that isolates either. |
| mvp | granularity | 7/10 | 5 tasks; t-1 (c-1+c-2+c-3 on one function) is the panel's best commit boundary, but t-2 fusing c-5 and c-6 into one `staleMilestoneBranches` change is under-split for two independent failure modes. |
| mvp | wave correctness | 4/10 | Weakest. Wave 1 puts t-1, t-2, t-3, t-4 in parallel with **all four** editing `internal/cmd/milestone.go`; two of them register subcommands into the same `AddCommand` call (`milestone.go:25`). |
| verification | criteria coverage | 9/10 | 7/7 and the only draft that contracts locked `stacked_child_status`, the only one asserting `dross milestone prune` (not just doctor) consumes the gated detector, and the only one guarding the prompt's existing footer-audit/interaction-coverage gates. |
| verification | test-contract specificity | 9/10 | Most mechanistic: a pre-receive hook on the bare origin to force a delete failure, "the invocation appears BEFORE the `## 2. Title` heading". Loses a point for lacking risk's absent-substring assertions in a few places. |
| verification | granularity | 8/10 | 7 tasks; extracting a pure finalize-state classifier (t-1) is what makes the ordering and gone-vs-unmerged contracts testable without git ancestry, at the cost of one new file. |
| verification | wave correctness | 8/10 | 3 waves, the only draft that spotted the `Milestone()` AddCommand collision and sequenced for it; slightly over-cautious (see note below) and it does not fully apply its own rule inside wave 2. |

**Skeleton: `verification`.** It scores highest on the two dimensions that decide
whether this phase can be verified at all — it is the only draft that turns the
finalize decision into a unit-testable classifier rather than a git-fixture-only
behaviour, the only one that pins two locked decisions instead of one, and the only
one that carries the fix through to `milestone prune`, the destructive consumer that
must not drift from `doctor`. Its ordering claim for the status write is also the only
one that matches existing repo behaviour (`phase.go:560` saves the completion
breadcrumb before the branch deletes at 568/586, with a comment stating that is
deliberate).

Grafted from the runners-up: risk's t-8 seam test wholesale (verification has no
cross-task test), risk's absent-substring and leftover-branch contracts, risk's
gone-branch *refusal* over verification's proceed arm, and mvp's catch that existing
`milestone_stale_test.go` fixtures must gain milestone tomls or the new gate silently
turns them all not-stale.

**Wave note:** verification serialized t-6 behind t-3 purely to avoid a same-file
merge conflict in `Milestone()`. `dross-execute` runs tasks in order with an atomic
commit per task, so same-file tasks inside a wave are not concurrent edits. The merged
plan keeps that dependency (it is harmless and self-documenting) but does not
propagate the rule further, which is why wave 2 holds three `milestone.go` tasks.

---

## Merged plan

```
Phase milestone-lifecycle-close — 8 tasks across 3 waves

Wave 1
  t-1  Classify finalize state before any teardown            [verification]
       files:    internal/cmd/milestone_finalize_state.go,
                 internal/cmd/milestone_finalize_state_test.go
       covers:   c-2, c-3
       depends:  —
       desc:     A pure classifier over (milestone toml status, origin/milestone/<v>
                 presence, ancestry vs origin/<main> and vs the recorded base) returning
                 alreadyFinalized / branchGone / merged(target) / unmerged, plus its
                 rendered message. milestoneFinalize consumes it in t-4; nothing in
                 milestone.go changes yet.
       contract: (a) status="complete" + origin/milestone/<v> deleted → alreadyFinalized,
                 reached without any ancestry query — the fixture with no branch anywhere
                 still classifies finalized (locked already_finalized_evidence);
                 (b) origin/milestone/<v> absent + status="active" → branchGone, and the
                 rendered message contains "gone" and does NOT contain
                 "is not merged into" [graft: risk t-5(a), mvp t-1];
                 (c) origin/milestone/<v> present and genuinely unmerged → unmerged, and
                 the message still contains "is not merged into origin/" — the existing
                 refusal survives for the case it was written for [graft: risk t-5(b)];
                 (d) a child merged into its recorded base but not into origin/main →
                 merged with target = the base branch; the existing stacking arm
                 (milestone.go:279-298) is pinned, not rewritten (locked
                 stacked_child_status);
                 (e) branchGone's message names `dross milestone set <v> status complete`
                 as the remedy — verified to exist (milestoneSettablePaths,
                 milestone.go:771) [graft: risk, mvp].

  t-2  Measure stale branches against origin main             [risk+mvp+verification]
       files:    internal/cmd/milestone_stale.go,
                 internal/cmd/milestone_stale_test.go,
                 internal/cmd/milestone_prune_test.go
       covers:   c-6
       depends:  —
       desc:     staleMilestoneBranches resolves its comparison ref to
                 refs/remotes/origin/<main> when it exists, falling back to
                 refs/heads/<main> only when origin carries no such ref. BOTH the
                 ancestry arm (isAncestor) and the squash-content walk
                 (resolveSquashCommit) use the resolved ref.
       contract: (a) milestone/v1.0 squash-merged onto LOCAL main only is absent from
                 staleMilestoneBranches; after `git push origin main` on the identical
                 fixture it is returned with Reason == "squash-merged" — the two runs
                 differ only in origin/main's position. A detector still reading local
                 main passes the second half and fails the first [risk t-2(a)+(b),
                 verification t-2];
                 (b) the ancestry arm follows the same ref: a branch fast-forwarded into
                 local main with origin/main behind is not "merged";
                 (c) a repo with NO origin/<main> ref at all still reports a
                 locally-merged milestone branch, non-empty and no error — the fallback
                 is exercised, not just declared [risk t-2(c)].

  t-3  Add milestone remove and replace verbs                 [risk+verification]
       files:    internal/cmd/milestone_listedit.go,
                 internal/cmd/milestone_listedit_test.go,
                 internal/cmd/milestone.go, README.md
       covers:   c-7
       depends:  —
       desc:     `dross milestone remove [version] <list.path> "<exact value>"` and
                 `dross milestone replace [version] <list.path> "<old>" "<new>"` over
                 phases / scope.success_criteria / scope.non_goals, sharing
                 normalizeListField (milestone.go:847) and add's version defaulting.
                 Non-matching value is an error. Registered in Milestone().
       contract: (a) `remove v1.3 phases b` on [a,b,c] leaves exactly [a,c], order
                 preserved; a swap-with-last implementation fails;
                 (b) the identical second remove exits non-zero quoting the missing
                 value AND the toml bytes read back byte-identical — a silent no-op
                 fails both halves (locked remove_addressing) [graft: risk t-3(a)];
                 (c) `replace v1.3 phases b x` on [a,b,c] yields [a,x,c] — the entry
                 keeps index 1, so a phase-order fix is not a reorder;
                 (d) a scalar path (milestone.title) errors naming the three valid list
                 fields and leaves the toml byte-identical;
                 (e) bare list names resolve like add does — `remove v1.3
                 success_criteria "<t>"` and `remove v1.3 scope.success_criteria "<t>"`
                 hit the same field (normalizeListField already accepts both);
                 (f) README's `dross milestone {…}` row names `remove` and `replace`,
                 same grep-guard shape as TestReadmeMilestoneRowDocumentsBaseFlag
                 (milestone_narration_test.go:61).

Wave 2 (depends on wave 1)
  t-4  Write milestone status complete on finalize            [risk+mvp+verification]
       files:    internal/cmd/milestone.go,
                 internal/cmd/milestone_finalize_test.go, README.md
       covers:   c-1, c-2
       depends:  t-1
       desc:     milestoneFinalize routes through t-1's classifier: alreadyFinalized
                 short-circuits exit 0 before the fetch/merge guard; branchGone and
                 unmerged return their distinct errors; merged proceeds. On the merged
                 path status is set to "complete" and the toml saved BEFORE the local +
                 remote branch deletes — matching `dross phase complete`, which saves its
                 breadcrumb at phase.go:560 ahead of the deletes at 568/586. README
                 milestone row updated.
       contract: (a) after `dross milestone complete <v> --finalize` on a merged
                 milestone, .dross/milestones/<v>.toml reads [milestone].status =
                 "complete" — dropping the Save fails the field assertion;
                 (b) the second --finalize exits 0, prints "already finalized", and its
                 combined output contains NEITHER "is not merged into" NOR any git delete
                 attempt [graft: risk t-1(c)];
                 (c) a child merged only into its recorded parent base finalizes to
                 status="complete" while local main's SHA is unchanged (locked
                 stacked_child_status);
                 (d) ordering — with a pre-receive hook on the bare origin rejecting
                 branch deletes, finalize returns an error naming the failed remote
                 delete, YET <v>.toml already reads status="complete" and the re-run
                 exits 0 already-finalized;
                 (e) leftover report — status="complete" with refs/heads/milestone/<v>
                 still present prints the branch name and points at `dross milestone
                 prune`, and deletes no ref (re-check rev-parse --verify after the run)
                 [graft: risk t-1(d); this is the counterweight that keeps (d)'s
                 write-first ordering from hiding an unswept branch].

  t-5  Gate stale detector on milestone status                [risk+verification]
       files:    internal/cmd/milestone_stale.go, internal/cmd/doctor.go,
                 internal/cmd/milestone.go, internal/cmd/milestone_stale_test.go,
                 internal/cmd/doctor_test.go
       covers:   c-5
       depends:  t-2
       desc:     staleMilestoneBranches takes the .dross root as well as repoDir, loads
                 milestones/<version>.toml per milestone/<version> branch, and skips the
                 branch when status is "active" — and when no toml exists (locked
                 toml_less_branch_not_stale). doctor.go and milestonePrune pass root.
                 Existing milestone_stale_test.go fixtures gain milestone tomls, or the
                 new gate silently turns every one of them not-stale — part of this task,
                 not a follow-up [graft: mvp judgment call].
       contract: (a) .dross/milestones/v1.0.toml at status="active" with milestone/v1.0
                 squash-merged → absent from staleMilestoneBranches AND `dross doctor`
                 prints no "Stale milestone branches:" section; flipping that ONE field
                 to "complete" makes both report it, nothing else in the fixture moves;
                 (b) milestone/v9.9 with no toml is never reported, even when
                 squash-merged (locked toml_less_branch_not_stale) — a distinct fixture
                 from (a) because no status was read;
                 (c) status="planning" (branch cut, work not started) is likewise absent
                 [graft: risk t-6(c)];
                 (d) `dross milestone prune` leaves an active milestone's squash-merged
                 branch in place — prune consumes the same gated detector, so the doctor
                 fix cannot drift from the destructive consumer;
                 (e) doctor's issue count is unchanged by the gate when the milestone is
                 complete (the section still counts toward a non-zero exit).

  t-6  Add milestone progress --json                          [risk+mvp+verification]
       files:    internal/cmd/milestone_progress.go,
                 internal/cmd/milestone_progress_test.go,
                 internal/cmd/milestone.go, README.md
       covers:   c-4
       depends:  t-3
       desc:     `dross milestone progress [version] [--json]` answers the dispatch
                 question in one deterministic call: milestone status verbatim,
                 done/total, remaining slugs, unscaffolded slugs. Per locked
                 phases_done_test a slug is done only when .dross/phases/<slug> exists
                 AND state history carries "completed <slug>" or "shipped <slug>"
                 (historyHasAction, phase.go:39). Doc-comment the known limit: history
                 is capped at 50 entries.
       contract: (a) phases=[a,b], both scaffolded, history carrying "completed a" only →
                 --json reports done=1, remaining=["b"], all_done=false;
                 (b) a slug in `phases` with NO directory under .dross/phases/ counts
                 not-done and is listed under `unscaffolded`, even when history carries
                 "completed <slug>" (locked phases_done_test) — an implementation that
                 counts array entries, or one that skips missing dirs, fails here;
                 (c) a phase whose only breadcrumb is "shipped <slug>" counts as done;
                 (d) emitted `status` is [milestone].status verbatim, so the prompt
                 dispatches on one field rather than re-deriving it — and exit code is 0
                 in every arm including status="planning" (dispatch data, not a gate);
                 (e) no version arg and no state.current_milestone → non-zero with the
                 no-current-milestone message, not a nil-deref [graft: risk t-4(d)].

Wave 3 (depends on wave 2)
  t-7  Dispatch /dross-milestone on milestone progress        [risk+mvp+verification]
       files:    assets/prompts/milestone.md,
                 internal/cmd/milestone_dispatch_prompt_test.go
       covers:   c-4
       depends:  t-4, t-6
       desc:     milestone.md opens with a dispatch step reading `dross milestone
                 progress --json` and branches three ways: no active milestone → the
                 existing scope flow; all_done → drive completion (`dross milestone
                 complete <v>`, merge as a MERGE COMMIT not a squash, then --finalize);
                 otherwise → report the remaining slugs and stop. The scoping body moves
                 under the first arm.
       contract: (a) the `dross milestone progress --json` invocation appears BEFORE the
                 "## 2. Title" heading — scoping is an arm of the dispatch, not the
                 default path;
                 (b) the all-done arm names `dross milestone complete` and `--finalize`
                 and states "merge commit" + "not squash" INSIDE that same arm — dropping
                 the squash gate fails the assertion pinning c-4's
                 "gating merge-commit-not-squash";
                 (c) the outstanding arm names the `remaining` field and routes to
                 /dross-spec, never reaching `dross milestone create` or the
                 success-criteria walk; it forbids `phase list` / verify.toml as the
                 source of doneness [graft: risk t-7(c)];
                 (d) the prompt names the already-finalized outcome (re-running
                 --finalize is safe) so dispatch never treats it as an error state
                 [graft: risk t-7(d)];
                 (e) the pre-flight still runs `dross rule show` + `dross interaction
                 show` and milestone.md still carries NO clear-point sentinel, so the
                 interaction-coverage and footer-audit Exempt gates stay green.

  t-8  Pin the close-path seam end to end                     [risk]
       files:    internal/cmd/milestone_lifecycle_close_test.go
       covers:   c-1, c-2, c-3, c-5
       depends:  t-4, t-5
       desc:     One scripted lifecycle over a real temp repo + bare origin asserting the
                 handoff between the status flip (t-4) and the status gate (t-5) — the
                 seam neither task can test alone: t-4's tests never load the detector,
                 t-5's never run finalize. No production code.
       contract: single fixture, asserted in order: (1) active milestone whose branch is
                 merged to origin/main → staleMilestoneBranches returns nothing for it;
                 (2) --finalize → toml status "complete" AND refs/heads/milestone/v1.0
                 and refs/remotes/origin/milestone/v1.0 both absent;
                 (3) re-run --finalize → exit 0, "already finalized", no
                 "is not merged into";
                 (4) recreate milestone/v1.0 at the merged commit → the same detector NOW
                 returns it with Reason "merged", so a finalized milestone's leftover
                 branch IS prunable. Flipping either the flip or the gate breaks a
                 numbered step.
```

### Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-4 (primary), t-8 |
| c-2 | t-1 (classifier), t-4 (primary), t-8 |
| c-3 | t-1 (primary), t-8 |
| c-4 | t-6 (readout), t-7 (prompt dispatch) |
| c-5 | t-5 (primary), t-8 |
| c-6 | t-2 |
| c-7 | t-3 |

7/7 criteria covered. All 5 locked decisions carry a named contract:
already_finalized_evidence → t-1(a); stacked_child_status → t-1(d), t-4(c);
phases_done_test → t-6(b); remove_addressing → t-3(b); toml_less_branch_not_stale
→ t-5(b).

---

## Disagreements

### D1 — When the status write happens relative to branch teardown
- **risk (t-1)**, **mvp (t-1)**: write status="complete" **after** both deletes succeed.
  Rationale: a delete failure then leaves status="active" so a re-run retries the real
  teardown; writing first turns any failed delete into a permanent already-finalized
  short-circuit over a cleanup that never ran (risk's F3).
- **verification (t-4)**: write **before** teardown. Rationale: c-2's marker must survive
  a failed remote delete; write-last leaves a window where origin's ref is already gone
  AND status is still "active" — precisely the c-3 wedge, requiring a manual
  `milestone set`.
- **Provisional default: write BEFORE teardown (verification), with risk's leftover-branch
  report grafted in as t-4(e).** This overrides a 2-1 majority, on two pieces of evidence
  the majority did not cite: (i) `dross phase complete` already does it this way —
  `cs.Save` at `internal/cmd/phase.go:560` precedes `git branch -D` (568) and
  `push --delete` (586), with an in-code comment that a failed delete "must still leave
  the merge recorded"; (ii) locked `already_finalized_evidence` says the branch is gone
  after a successful finalize so git ancestry cannot answer afterwards — a marker written
  after the deletes has a window in which neither the toml nor git can answer. Risk's F3
  objection is real and is answered, not dismissed: t-4(e) makes the already-finalized
  short-circuit *name* the leftover branch and route to `dross milestone prune`, and
  t-5 makes prune able to see it (status is no longer "active"). t-8 step (4) asserts
  exactly that recovery path.
- **Why it matters:** this is the phase's one irreversible-adjacent choice. Get it wrong
  in the write-last direction and a partially-failed finalize produces a milestone that
  can never be closed without hand-editing toml — the bug this phase exists to kill. Get
  it wrong in the write-first direction *without* t-4(e) and a branch is silently
  orphaned. The default takes the recoverable failure over the unrecoverable one and
  pays for it with an explicit report.

### D2 — What a gone origin branch does
- **risk (t-5)**, **mvp (t-1)**: always an error, with a distinct "gone from origin"
  message naming `dross milestone set <v> status complete` as the remedy.
- **verification (t-1)**: two-way classification — origin ref absent AND local
  absent-or-contained → finalize **proceeds** (ff, delete local, flip status); origin ref
  absent AND local carrying commits off origin/main → refuse. Rationale: forges that
  auto-delete merged branches would otherwise wedge every finalize.
- **Provisional default: always refuse with the distinct message (risk + mvp).** Overrules
  the skeleton on its own task. c-3's text is message-level — "the message says the branch
  is gone rather than claiming it has not merged" — it does not ask finalize to succeed.
  House precedent is explicit: `mergeGate` (`internal/cmd/phase.go:797-805`) maps a
  missing `origin/phase/<id>` ref (squash-deleted) and a false ancestry result to the
  *same* guided refusal, on the stated grounds that it "never false-completes".
  Verification's wedge concern is largely dissolved by D1: with status written before
  teardown, any successful finalize short-circuits at alreadyFinalized before the gone
  check is ever reached, so branchGone only fires when finalize never succeeded — a
  genuine anomaly. t-1(b) and risk's ordering pin (carried as t-1(a)'s "reached without
  any ancestry query") assert that composition.
- **Why it matters:** the proceed-arm invents a merge nobody observed. If the local branch
  is contained in origin/main only because a stale fetch says so, it flips a milestone
  to complete on unverified evidence. The refusal costs one documented command.

### D3 — c-5 and c-6: one task or two
- **mvp**: one task (t-2). Both are single-predicate changes to the same function, and
  c-5 forces the `root` parameter c-6's fixtures need — two tasks means two conflicting
  signature changes to the same call sites.
- **risk (t-2→t-6)**, **verification (t-2→t-5)**: two sequential tasks, split by
  contract — which ref merged-ness is measured against, vs which branches are eligible
  at all.
- **Provisional default: two tasks (t-2 → t-5).** mvp's signature concern is real but
  cheap: t-2 leaves the signature alone and t-5 adds `root`, so there is exactly one
  signature change, in one task. The split buys fixture isolation — a merged task lets a
  passing status-gate test mask a still-local-ref comparison, since a gated-out branch
  and a wrongly-compared branch both produce "absent from the result".
- **Why it matters:** both consumers of this function are destructive (`milestone prune`)
  or advisory-toward-destructive (`doctor`). A false negative here is invisible; a false
  positive deletes live work.

### D4 — Whether `replace` ships as its own verb
- **mvp**: no. c-7's "replaced" is satisfied by remove + the existing `add`; a third verb
  is structure no criterion asks for.
- **risk**, **verification**: yes. `phases` is an ordered array and `appendUnique`
  (`milestone.go:858`) appends, so remove+add moves the entry to the end — a phase-order
  fix that silently reorders delivery (risk's F7).
- **Provisional default: ship `replace` (t-3).** c-7 names replacement explicitly, and
  the code confirms the append behaviour. Locked `remove_addressing` covers remove's
  signature; replace mirrors it (exact old value, error on no match).
- **Why it matters:** without it, the documented way to fix a wrong `phases` entry
  corrupts phase order — turning a one-field correction into a silent roadmap change.

### D5 — What counts as a phase being "done"
- **risk (t-4)**: `completed <slug>` history entry OR changes.json recording `pr > 0`.
- **mvp (t-4)**: history entry OR shipped OR `verify.toml` verdict == "pass".
- **verification (t-6)**: `completed <slug>` OR `shipped <slug>` state-history breadcrumb,
  AND the phase directory exists.
- **Provisional default: verification's (breadcrumb `completed`|`shipped` + directory
  exists).** mvp's verify-verdict arm contradicts locked `phases_done_test`, which says
  complete-or-shipped — verdict "pass" means *verified*, a strictly earlier state, and
  would close a milestone over unmerged phases (risk rejects it for the same reason).
  Risk's `changes.json pr > 0` arm is looser than it looks: the PR number is recorded at
  ship time, before merge, so an open PR would read done.
- **Why it matters:** this predicate is what lets `/dross-milestone` start driving
  completion. Too loose and the prompt opens an integration PR over unfinished phases.
  Verification's own doc-comment caveat is carried into t-6: state history is capped at
  50 entries, so a very long milestone can under-report — under-reporting is the safe
  direction.

### D6 — Whether a cross-task seam test exists
- **risk (t-8)**: yes, a scripted lifecycle over a real temp repo + bare origin, no
  production code.
- **mvp**, **verification**: no such task.
- **Provisional default: include it (t-8).** The flip (t-4) and the gate (t-5) are
  individually correct and can still disagree at the seam — t-4's tests never load the
  detector, t-5's never run finalize, so a finalized milestone staying unprunable forever
  is invisible to both. Its dependency on the prompt task is dropped: t-8 asserts only
  Go behaviour, so it needs t-4 and t-5, not t-7.
- **Why it matters:** the seam is the actual user-visible outcome — "finalize, then the
  leftover branch becomes prunable". Cheap to write, and it is the only test that fails
  if D1's ordering is later reverted.

### D7 — Extracting a finalize-state classifier
- **verification (t-1)**: yes — a new `milestone_finalize_state.go` returning a state
  enum + message, so alreadyFinalized / branchGone / merged / unmerged are assertable
  without building git ancestry fixtures for each.
- **risk (t-1, t-5)**, **mvp (t-1)**: no — edit `milestoneFinalize` inline, test through
  git fixtures.
- **Provisional default: extract the classifier.** It is what makes t-1(a)'s "never
  reaches an ancestry query" and t-1(d)'s stacked-child arm testable as facts rather
  than as observed side effects, and it lets D1's and D2's orderings be pinned directly.
  Cost is one new file and one new abstraction in a 400-line function's neighbourhood.
- **Why it matters:** `milestoneFinalize` currently interleaves guard, ancestry, checkout,
  ff and two deletes (`milestone.go:266-345`). Adding three more decision arms inline is
  where the next lifecycle bug hides.

### D8 — What the prompt reads to dispatch
- **mvp (t-5)**: two calls — `dross milestone show --json` for status plus
  `dross milestone progress --json` for phase counts.
- **risk (t-7)**, **verification (t-7)**: one call — `progress --json` emits
  `[milestone].status` verbatim alongside the counts.
- **Provisional default: one call (`progress --json`).** t-6(d) makes `status` verbatim
  part of the payload contract, so the second call adds a round trip and a second place
  the prompt can derive state from. mvp's form also assumes `milestone show` accepts
  `--json`, which is unverified.
- **Why it matters:** c-4 is about dispatching on milestone status. Two sources means two
  ways to disagree, and prompt-level reconciliation is exactly the guessing c-4 removes.
