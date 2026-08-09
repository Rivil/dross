# Panel synthesis — completion-state-truth

Judged from three independent drafts (risk / mvp / verification). Paths in all
three were checked against the repo; discrepancies are noted inline.

## Scores

| Dimension | risk | mvp | verification |
|---|---|---|---|
| Criteria coverage | **Strong** — 6/6 with an owner per failure mode; the only draft that catches the two surfaces the others miss: ship.md §0.5's raw `git checkout` (c-1's first clause) and `internal/watch/drift.go` (c-6's "no status surface") | Thin — 6/6 nominally, but c-1 rests entirely on one prompt edit and c-5's completion-time statement is an afterthought inside a 3-criterion task | Strong — 6/6 with an explicit map, but deliberately excludes §0.5 from c-1 ("a separate phase") and misses drift.go entirely |
| Test-contract specificity | **Strong** — every contract names the mutation that re-fails it ("restoring either clear fails it", "swapping `checkoutBranch` for a raw `git checkout` fails it") | Adequate — contracts are prose bundles; t-1 packs four assertions into one sentence with no named test functions | **Strongest per contract** — names test functions throughout and the exact wrong implementation ("counting `<work>..<main>` yields 0 and fails it") |
| Granularity | Good — 8 tasks, disjoint regions inside phase.go, justified explicitly; over-split at t-1/t-2 (see D3) | Coarse — 4 tasks; t-1 is 4 files / 3 criteria and swallows the topology statement | Good — 7 tasks, well-sized; t-7 over-broad at 7 files, two of which carry no squash claim (verified below) |
| Wave correctness | **Correct** — 3 waves, every dep stated and real | Under-sequenced — 2 waves; t-3 (status) sits in wave 1 consuming the `shipped` value t-1 introduces, and duplicates topology rendering with t-1 with no shared helper | Correct — 3 waves; the standalone wave-1 topology helper is the single best structural idea in any draft |

**Skeleton: risk.** It is the only draft that assigns an owner to every failure
mode and the only one whose criteria coverage is complete in fact rather than on
paper — the §0.5 pre-flight checkout and `drift.go`'s `verified_unshipped` bucket
are both live holes the other two leave open. Its contracts are also the only set
where each one states what breaks it.

Grafted from the runners-up: verification's standalone topology helper (wave 1,
consumed by two wave-2 tasks) replaces risk's inline `topology.go` inside the
complete task; mvp+verification's merge of the ship-side and complete-side state
writes into one task replaces risk's t-1/t-2 split; verification's wider scan path
list hardens risk's truth-pass gate; verification's `AheadOfMain` direction
contract and mvp's explicit "count 3" assertion sharpen the topology contracts.

## Merged plan

**8 tasks across 3 waves.**

```
Wave 1

  t-1  Move the completion write to phase complete            [mvp+verification, contracts from risk]
       files:    internal/cmd/ship.go, internal/cmd/phase.go,
                 internal/cmd/ship_test.go, internal/cmd/phase_test.go
       covers:   c-2, c-3 (narration half)
       depends:  —
       description:
         ship.go step 5 (lines 225-227): replace `s.CurrentPhase = ""` /
         `s.CurrentPhaseStatus = ""` / `Touch("completed <id>")` with
         `CurrentPhaseStatus = "shipped"`, current_phase left set, and a
         `shipped <id>` touch. Drop the post-PR narration at ship.go:339
         ("Completion record folded into … squash-merge will land it on …") for a
         line naming `dross phase complete` as the writer.
         phase.go: replace the "No state write here" block at :507-511 with the
         real write — after the fast-forward succeeds and BEFORE the local and
         remote branch deletions (see D4), clear current_phase /
         current_phase_status only when current_phase equals the phase being
         completed, append one `completed <id>` guarded by a history scan, Save.
         No `git add` (state.json is gitignored — an explicit add hard-fails) and
         no commit. phase_test.go's writeCompletion/foldCompletion fixtures stop
         clearing current_phase; they modelled ship's old write.
       contract:
         - TestShipRecordsShippedNotCompleted: after `dross ship`, state.json reads
           current_phase = "<id>", current_phase_status = "shipped", and history
           carries NO `completed <id>`. Restoring either clear fails it.
         - TestShipNarratesCompleteOwnsRecord: ship's stdout contains no
           "Completion record folded" / "squash-merge will land it" and names
           `dross phase complete`. (ship_test.go:668 currently asserts the opposite
           string — that assertion is inverted here, not deleted.)
         - TestShipIsReShippable: a second `dross ship` exits 0 and leaves
           current_phase_status = "shipped" with one `shipped <id>` entry, not two.
         - TestPhaseCompleteWritesCompletionRecord: after a confirmed-merge
           complete, state.json reads current_phase = "", current_phase_status = ""
           and exactly one `completed <id>` entry. Deleting complete's write leaves
           zero and fails — today the same assertion only passes because ship wrote it.
         - TestPhaseCompleteIsIdempotent: `dross phase complete <id>` twice
           (PR-merged stub, phase branch already gone) exits 0 both times, entry
           count stays 1. An unconditional append or a second-run error fails it.
         - TestCompleteOtherPhaseKeepsCurrent: completing phase A while
           current_phase is B appends `completed A` and leaves current_phase = B.
         - TestCompleteRecordsBeforeTeardown: with the bare origin carrying an
           `update` hook rejecting deletion of `phase/<id>`, complete exits non-zero
           AND state.json already carries `completed <id>`. Moving the write below
           the teardown fails this.

  t-2  Branch-topology helper                                  [verification, degradation contract from risk]
       files:    internal/cmd/topology.go, internal/cmd/topology_test.go
       covers:   c-5
       depends:  —
       description:
         New `branchTopology(repoDir, root)` returning {Head, Work, Main,
         AheadOfMain, OnMain} — Work from the existing `resolveNewWorkBase`
         (internal/cmd/basebranch.go:159), AheadOfMain from
         `git rev-list --count <main>..<work>` — plus a one-line renderer shared by
         status and complete. Pure read, no network; degrades to a partial answer
         (Work=main, AheadOfMain=0, nil error) on any git failure, missing
         milestone branch, missing origin or detached HEAD. Status must never
         break on it.
       contract:
         - TestBranchTopologyCountsAheadOfMain: on a fixture where milestone/v1.2
           carries 3 commits main lacks, returns Work="milestone/v1.2",
           AheadOfMain=3, OnMain=false. Counting `<work>..<main>` yields 0 and fails.
         - TestBranchTopologyNoMilestoneNoRemote: repo with no milestone branch and
           no origin returns Work="main", AheadOfMain=0 and a nil error. A helper
           that errors on a missing ref fails it.
         - TestRenderTopologyLine: table over 0 / 3 commits — the renderer emits the
           branch name always and the "not yet on main" clause only when
           AheadOfMain > 0. A hardcoded clause fails the 0 case.

  t-3  Guarded `dross phase checkout`                          [risk]
       files:    internal/cmd/phase_checkout.go,
                 internal/cmd/phase_checkout_test.go, internal/cmd/phase.go
       covers:   c-1
       depends:  —
       description:
         New subcommand `dross phase checkout <phase-id>` resolving `phase/<id>` and
         switching through `checkoutBranch` (internal/cmd/switchbranch.go:36, so
         `guardLiveState` at :88 runs), refusing when the local ref is absent rather
         than creating it. Registered in the AddCommand list at phase.go:28 — the
         only line this task touches in phase.go. This is the guarded primitive
         ship.md §0.5 and the Recovery section need so no step of the flow does a
         raw `git checkout`. See D2.
       contract:
         - TestPhaseCheckoutRefusesClobber: against a `phase/<id>` that tracks
           `.dross/state.json`, exits non-zero with guardLiveState's "refusing to
           switch" and the live 12-entry history is intact. Swapping `checkoutBranch`
           for a raw `git checkout` fails it.
         - TestPhaseCheckoutMissingBranch: `dross phase checkout nope` exits non-zero
           naming `phase/nope`, and `git branch --list phase/nope` stays empty — it
           must never fall through to `checkout -b`.

Wave 2

  t-4  Ship prompt performs no branch switch                   [risk+mvp+verification]
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go
       covers:   c-1, c-3
       depends:  t-3
       description:
         §6.1 GitHub becomes `gh pr merge <pr-url> --squash` — `--delete-branch`
         dropped (ship.md:148), with a line stating dross owns both branch
         deletions. Forgejo (:149) and GitLab (:150) merge steps are left intact
         (D1). §6.3's description of `dross phase complete` is corrected: it
         switches to the *recorded base*, not main, and writes the completion
         record itself — no chore commit. §0.5 (ship.md:19) and the Recovery
         section (~:191) use `dross phase checkout <id>` in place of raw
         `git checkout`. Run `make install` after editing (rule r-01).
       contract:
         - TestShipPromptNoUnguardedSwitch: ship.md contains no `--delete-branch`,
           no `git checkout` and no `git switch` outside fenced examples of *other*
           tools — whole file, not just §4-to-EOF, since t-3 gives §0.5 a guarded
           replacement. Re-adding `--delete-branch` to the gh line fails it.
         - TestShipPromptCommandsExist: every `dross <verb> …` invocation in ship.md
           resolves against the assembled cobra tree from `newRoot`, so the prompt
           cannot advertise `dross phase checkout` before t-3 lands it.
         - TestShipPromptNamesCompleteAsTeardownOwner: §6 names `dross phase
           complete` as performing the local AND remote phase-branch deletion, and
           §6.3 contains neither "switches to main" nor "chore commit".

  t-5  Complete states the resulting topology                  [risk+verification]
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-5
       depends:  t-1, t-2
       description:
         Replace phaseComplete's final Printf (phase.go:518, `completed %s — %s is
         at origin, phase/%s deleted`) with a statement built from t-2's helper: the
         branch HEAD now sits on, `deleted phase/<id> (local + origin)` — or "already
         gone" when the remote ref was absent — and, only when the reconcile base is
         not main, `<n> commit(s) on <base>, not yet on main`.
       contract:
         - TestPhaseCompletePrintsTopologyStatement: stdout names all three — the
           branch HEAD ends on, the deleted local `phase/<id>` AND the deleted remote
           ref, and the "on <base>, not yet on main" clause. Each substring asserted
           separately, so dropping any one clause fails.
         - sub-test "remote branch already deleted": the statement says so rather
           than claiming a deletion that did not happen (reuses
           TestPhaseCompleteRemoteDeleteIdempotent's fixture shape).
         - sub-test "no milestone": names main as the landing branch and emits NO
           "not yet on main" clause. A hardcoded clause fails here.

  t-6  Status and watch report shipped, not stale              [all three; drift half from risk]
       files:    internal/cmd/status.go, internal/cmd/status_test.go,
                 internal/watch/drift.go, internal/watch/drift_test.go
       covers:   c-5, c-6
       depends:  t-1, t-2
       description:
         Delete the `stale:` block (status.go:68-82) and `stateRecordsCompleted`
         (:542) — its trigger, `completed <id>` while on phase/<id>, is unreachable
         once complete is the sole writer. `staleCompletedState` (:491) becomes
         `shippedUnmergedPhase`, keyed on `current_phase_status == "shipped"` with
         the recorded PR number as fallback, rendering a `shipped:` line naming the
         PR and the base it waits on. Add the standing `branch:` line from t-2's
         renderer, printed on every run — unconditional, not gated on an active
         phase. In `internal/watch/drift.go`, `classifyPhase` (:91) returns
         `DriftVerifiedUnshipped` for any verify-pass phase lacking a `completed`
         entry (:98) — i.e. every shipped-but-unmerged phase under the new model;
         a phase reading shipped with a recorded PR must not drift. See D8.
       contract:
         - TestStatusReportsShippedNotStale: on phase/<id> with
           current_phase_status = shipped and PR #7 recorded but unmerged on origin,
           status prints `phase: <id> (shipped)` plus a `shipped:` line naming #7,
           and no `stale:` line. Restoring the completed-breadcrumb warning fails it.
         - TestStatusSilentAfterCompletion: after a completed phase (current_phase
           empty, `completed <id>` in history) status prints neither `shipped:` nor
           `stale:`. Keeping the old history-keyed warning fires it and fails.
         - TestStatusTopologyLineAlways: status prints the `branch:` line in three
           fixtures — on phase/<id> mid-milestone (naming milestone/v1.2 and its
           commit count), on milestone/<v> between phases, and in a repo with no
           origin — exiting 0 in all three. Gating the line behind "only when a
           phase is active" fails the between-phases case.
         - TestDriftShippedPhaseNotUnshipped: a verify-pass phase with
           current_phase_status = shipped and a recorded PR yields no
           `verified_unshipped` entry; the same phase without either still does.

Wave 3

  t-7  Reproduce the incident end to end                       [all three; control from risk+mvp]
       files:    internal/cmd/completion_state_truth_test.go
       covers:   c-4
       depends:  t-1, t-4
       description:
         One regression test over the whole ship→merge→complete flow, in a new file
         reusing the existing helpers: `incidentRepo`
         (state_clobber_regression_test.go:28), `hasAction` (state_history_test.go:79)
         and `stubPRMerged` (phase_test.go:25). Fixture: a base branch whose tree
         still tracks a 2-entry `.dross/state.json`, a live untracked copy with 12
         entries at version 9.9.9.9, a phase branch with a recorded PR, a stubbed
         merged PR. Drives `dross ship`, simulates the provider squash-merge on the
         bare origin, then `dross phase complete`.
       contract:
         - TestShipToCompleteKeepsLiveState: after the flow the live copy still
           parses, holds >= 12 history entries, contains the first live action,
           reads version 9.9.9.9, and carries `completed <id>`. It fails against
           pre-phase code because complete writes no `completed <id>` (t-1). A
           non-zero exit from complete is an allowed outcome — refusing IS survival,
           per the existing switchbranch guard; a truncated 2-entry state.json is not.
         - Control sub-test "raw checkout still clobbers" is mandatory: it performs
           the unguarded `git checkout <base>` that `gh pr merge --delete-branch`
           performs and asserts the live copy IS truncated to 2 entries. Without a
           demonstrated clobber the assertions above are vacuous on a fixture that
           never had one. Mirrors the existing
           TestStaleBranchCheckoutCannotClobberLiveState idiom.

  t-8  Retire the rides-the-squash claim everywhere            [risk+verification]
       files:    internal/cmd/phase.go, internal/cmd/ship.go,
                 internal/cmd/status.go, assets/prompts/ship.md, ARCHITECTURE.md,
                 internal/cmd/completion_truth_test.go
       covers:   c-3
       depends:  t-1, t-4, t-5, t-6
       description:
         Rewrite every surface claiming the completion record rides the squash:
         phaseComplete's `Long` (phase.go:236-249), the comments at phase.go:278 and
         :507-511, ship.go's step-5 comment block (~:211-227), status.go's
         stale-guard comment (:68-73), ship.md §4 step 5, and ARCHITECTURE.md's
         phase-lifecycle + ship paragraphs (the ship section at ~:470 currently
         reads "folds the completed-state transition … into the phase branch and
         commits it BEFORE the push, so the squash-merge carries the completion
         record to main"). Each becomes: ship marks the phase shipped and leaves
         current_phase set; `dross phase complete` writes the cleared state and the
         `completed <id>` entry into the machine-local, gitignored state.json after
         the merge is confirmed. New grep-guard test locks it. Run `make install`
         after the prompt edit (rule r-01).
       contract:
         - TestNoSurfaceClaimsCompletionRidesTheSquash: scans a fixed path list —
           internal/cmd/*.go, internal/changes/changes.go,
           internal/telemetry/telemetry.go, assets/prompts/*.md, ARCHITECTURE.md —
           for the phrase family "folds the completion", "folded into the squash",
           "squash-merge will land it", "rides the squash", "carries the completion
           record to main", "records the merge in state.json with a chore commit",
           and fails on any hit. Re-adding ship.md's old §6.3 sentence fails it.
           (changes.go and telemetry.go are in the scan list as rot insurance only —
           see D7.)
         - TestCompleteHelpNamesRecordOwner: `dross phase complete --help` contains
           "writes the completed-state transition" and the word "machine-local", and
           does not contain "'dross ship' folds". Deleting that sentence fails it,
           so the positive claim cannot rot the way the old one did.
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 | t-3, t-4 (end-to-end by t-7) |
| c-2 | t-1 |
| c-3 | t-1, t-4, t-8 |
| c-4 | t-7 |
| c-5 | t-2, t-5, t-6 |
| c-6 | t-6 |

6/6 covered. No task is outside the three drafts.

## Disagreements

### D1 — Which providers lose their remote-branch deletion

- **risk:** GitHub only. The locked `teardown_owner` decision names the GitHub step
  and nothing else; Forgejo/GitLab merge over REST and never switch branches, so
  neither is the hole. Also notes `ship_prompt_test.go:145-152` pins
  `should_remove_source_branch` as a *prior phase's locked* GitLab merge payload —
  **verified: that assertion exists and would have to be deleted.**
- **mvp and verification:** all three. c-1 says every branch deletion "local and
  remote" is performed by `dross phase complete`, and complete's `ls-remote`-guarded
  delete is already idempotent, so nothing breaks.
- **Provisional default: GitHub only.** The locked decision's *why* argues that the
  other providers are safe — an argument for leaving them alone, not for stripping
  them — and widening later is a one-line prompt edit, whereas defaulting wide means
  overwriting an assertion an earlier phase deliberately locked.
- **Why it matters:** the wide reading makes c-1's "every … branch deletion" literally
  true; the narrow reading leaves Forgejo/GitLab deleting their own remote branch, so
  a strict verifier could read c-1 as unmet. This is the one divergence where the
  criterion text and the locked decision text point different ways — lead's call.

### D2 — Is ship.md §0.5's pre-flight `git checkout phase/<id>` in c-1's scope

- **risk:** yes. c-1's first clause is absolute — "no step of /dross-ship performs a
  branch switch outside dross's guarded primitives" — and the pre-flight *is* a step
  of /dross-ship (ship.md:19, **verified**). A phase branch forked off a legacy base
  carries the tracked copy, so the risk is the incident's exact mechanism. Adds a
  guarded `dross phase checkout` (t-3).
- **verification:** no, explicitly. Reads c-1 as scoped by its second clause ("HEAD
  stays on phase/<id> *through the provider merge*"); the pre-flight happens before
  the flow, there is no guarded primitive to route it through, and "inventing that
  command is a separate phase." Flagged rather than silently widened.
- **mvp:** silent — neither includes nor rejects it.
- **Provisional default: in scope; t-3 ships.** The clause before the colon is
  unconditional, the line is textually a step of ship.md, and the implementation is a
  thin wrapper over the existing `checkoutBranch`/`guardLiveState` pair.
- **Why it matters:** dropping t-3 removes a wave-1 task and lets t-4's prompt scan
  exclude §0 (verification scopes its scan "from `## 4. Ship` to EOF" for exactly this
  reason). It is the difference between closing the incident's mechanism everywhere in
  the flow and closing it only after the PR opens.

### D3 — One state-write task or two

- **risk:** two (ship-side t-1, complete-side t-2), both in wave 1 — different failure
  modes (F2 vs F3/F4), disjoint code regions.
- **mvp and verification:** one, and both give the same reason: split, the intermediate
  commit has *nobody* writing the completion record, which is strictly worse than
  today. verification adds that existing phase_test fixtures modelled ship's old write,
  so the ship-first commit lands red.
- **Provisional default: one task (merged into t-1).** Two lenses converge with a
  stated green-commit argument; risk offers no counter to it.
- **Why it matters:** dross-execute commits per task. A split produces a committed
  state where the record is written by nobody — the exact regression this phase exists
  to close — and likely a red commit, which the execution gate forbids.

### D4 — Where in phaseComplete the state write lands

- **risk:** after the fast-forward, **before** the local and remote deletions. Rejects
  writing it last (a failed remote delete leaves a confirmed merge unrecorded — F2
  reopened one step later) and before the ff (an aborted ff records a completion that
  did not happen). Backs it with TestCompleteRecordsBeforeTeardown.
- **verification:** "writes the transition **after** the branch teardown." No rationale
  given.
- **mvp:** unspecified beyond "reload state after the checkout/ff".
- **Provisional default: before teardown (risk).** It is the only position with an
  argument and a test attached.
- **Why it matters:** teardown touches the network (`push --delete`). Writing after it
  means a transient remote failure loses a completion that genuinely happened —
  reintroducing the "relied on an earlier write surviving" hole c-2 exists to close.

### D5 — How the c-4 regression drives the merge step

- **verification:** parse the §6 GitHub merge command out of ship.md at test time and
  perform the raw `git checkout <base>` iff `--delete-branch` is present, so
  reintroducing the flag makes the incident test itself clobber and fail.
- **risk and mvp:** hardcode a control sub-test that performs the unguarded checkout
  and asserts it clobbers.
- **Provisional default: hardcoded control (risk/mvp).** t-4's
  TestShipPromptNoUnguardedSwitch already fails on reintroduction, so the
  prompt-parsing harness buys a second guard at the cost of a test whose behaviour
  depends on parsing markdown.
- **Why it matters:** the prompt-derived harness is the stronger coupling — it ties the
  regression to the instruction the agent actually follows rather than to a snapshot of
  it — but it is also the more brittle construct, and it is the only place any draft
  proposes parsing a prompt inside a test.

### D6 — New test file or append to the existing regression file

- **risk:** new `internal/cmd/completion_state_truth_test.go`.
  **verification:** new `internal/cmd/completion_state_incident_test.go`.
  **mvp:** append to the existing `internal/cmd/state_clobber_regression_test.go`
  (**verified to exist**, with the `incidentRepo` helper at :28).
- **Provisional default: new file**, reusing the existing file's helpers.
- **Why it matters:** only file placement and name — but two drafts chose new files with
  different names, so the plan must pick one or the task description is ambiguous.

### D7 — How wide the c-3 truth pass reaches

- **risk:** phase.go, ship.md, ARCHITECTURE.md + a grep guard. Explicitly leaves
  docs/roadmap.md alone (it records a past proposal, not current behaviour).
- **verification:** seven files, adding ship.go, status.go, internal/changes/changes.go,
  internal/telemetry/telemetry.go.
- **mvp:** no dedicated task at all — folds the prose edits into t-1/t-2 and leaves
  ARCHITECTURE.md and status.go's comment unedited.
- **Provisional default: a dedicated task over five edit surfaces** (phase.go, ship.go,
  status.go, ship.md, ARCHITECTURE.md) with verification's wider *scan* list. I read
  the two extra files: **changes.go:138-139 describes the `completed <id>` breadcrumb's
  behaviour in state history and makes no squash claim, and telemetry.go:298 is a bare
  `merge_pending` bucket name** — they belong in the scan path list as rot insurance,
  not on the guaranteed-edit list.
- **Why it matters:** mvp's version leaves ARCHITECTURE.md:~470 asserting "the
  squash-merge carries the completion record to main" after the behaviour changed —
  a direct c-3 failure. verification's version risks a task that edits files with
  nothing to fix.

### D8 — Is `internal/watch` a status surface under c-6

- **risk:** yes — t-6 also changes `internal/watch/drift.go` so a shipped phase with a
  recorded PR is not `verified_unshipped`.
- **mvp and verification:** neither mentions watch; both scope c-6 to
  `internal/cmd/status.go`.
- **Provisional default: in scope (risk).** **Verified:** `classifyPhase`
  (drift.go:91) returns `DriftVerifiedUnshipped` (:98) for *any* verify-pass phase
  lacking a `completed` history entry — which, under `record_owner`, is now every
  shipped-but-unmerged phase. Left alone, `dross watch` reports a correct
  shipped state as drift.
- **Why it matters:** c-6 says "no status surface warns about a shipped-but-unmerged
  phase". Dropping this leaves the phase shipping a fix to one surface while a second
  surface keeps emitting the warning the criterion forbids.
