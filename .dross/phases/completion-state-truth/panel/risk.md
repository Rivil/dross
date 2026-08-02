# risk lens — completion-state-truth

Phase completion-state-truth — 8 tasks across 3 waves

The failure modes this graph assigns owners to:

| # | Failure mode | Owner |
|---|---|---|
| F1 | A step of the ship flow switches branches outside `guardLiveState` and replays a tracked `state.json` over the live one (the incident) | t-3, t-4 |
| F2 | The completion transition is written by a command that cannot verify the merge, and nothing re-asserts it after | t-1, t-2 |
| F3 | Re-running `phase complete` duplicates or destroys the record; completing phase A while `current_phase` names B clears the wrong field | t-2 |
| F4 | Teardown fails *after* a confirmed merge and the completion goes unrecorded (the "relied on an earlier write" hole, re-opened) | t-2 |
| F5 | A correct mid-milestone topology is indistinguishable from a stuck one; the answer is only available at the instant it scrolls past | t-5, t-6 |
| F6 | A shipped-but-unmerged phase is reported as stale/unshipped by a surface reasoning from a now-unreachable state | t-6 |
| F7 | Prose survives the behaviour change and keeps describing the old owner, so the next incident is diagnosed from a lie | t-8 |
| F8 | Nothing reproduces the incident, so the fix is unfalsifiable | t-7 |

---

Wave 1

  t-1  Ship records shipped, keeps current_phase
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go
       covers:   c-2, c-3
       description:
         Replace ship.go step 5 (`s.CurrentPhase = ""` / `CurrentPhaseStatus = ""` /
         `Touch("completed <id>")`) with `CurrentPhaseStatus = "shipped"`, leaving
         CurrentPhase set and appending a `shipped <id>` activity instead. Drop the
         `chore(dross): ship` commit's completion framing and the post-PR narration
         "Completion record folded into … squash-merge will land it on …", replacing
         it with a line naming `dross phase complete` as the writer.
       contract:
         - TestShipLeavesPhaseCurrent: after `dross ship`, state.json still reads
           `current_phase = <id>` and `current_phase_status = "shipped"`, and history
           carries NO `completed <id>` entry. Restoring either clear fails it.
         - TestShipNarratesCompleteOwnsRecord: ship's stdout contains no
           "Completion record folded" / "squash-merge will land it" text and names
           `dross phase complete`. (ship_test.go:668 currently asserts the opposite
           string — that assertion is inverted here, not deleted.)
         - TestShipIsReShippable: a second `dross ship` on the same phase still exits
           0 and leaves current_phase_status = "shipped" with one `shipped <id>`
           entry, not two.

  t-2  Complete writes the completion transition
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-2
       description:
         In phaseComplete, after the fast-forward succeeds and BEFORE the local and
         remote branch deletions, clear `current_phase` / `current_phase_status`
         (only when current_phase equals the phase being completed) and append one
         `completed <id>` entry, then save. Idempotent: a `completed <id>` already
         present is not appended again; an already-cleared current_phase is not an
         error. No `git add` of state.json (gitignored — an explicit add hard-fails)
         and no commit.
       contract:
         - TestCompleteWritesCompletionRecord: after a confirmed-merge complete,
           state.json reads current_phase = "", current_phase_status = "" and
           exactly one `completed <id>` history entry. Reverting to "ship already
           wrote it" leaves zero entries and fails.
         - TestCompleteIsIdempotent: `dross phase complete <id>` run twice (PR-merged
           stub, phase branch already gone) exits 0 both times and the `completed
           <id>` entry count stays 1.
         - TestCompleteOtherPhaseKeepsCurrent: completing phase A while current_phase
           is B appends `completed A` but leaves current_phase = B untouched.
         - TestCompleteRecordsBeforeTeardown: with the bare origin carrying an
           `update` hook that rejects the deletion of `phase/<id>`, complete exits
           non-zero AND state.json already carries `completed <id>`. Moving the state
           write below the teardown fails this test.

  t-3  Add guarded `dross phase checkout`
       files:    internal/cmd/phase_checkout.go, internal/cmd/phase_checkout_test.go,
                 internal/cmd/phase.go
       covers:   c-1
       description:
         New subcommand `dross phase checkout <phase-id>` that resolves `phase/<id>`
         and switches to it through `checkoutBranch` (so `guardLiveState` runs),
         refusing when the local ref is absent rather than creating it. Registered in
         `Phase()`'s AddCommand list — the only line this task touches in phase.go.
         This is the guarded primitive the ship prompt's pre-flight needs, so no step
         of the flow is left doing a raw `git checkout`.
       contract:
         - TestPhaseCheckoutRefusesClobber: against a `phase/<id>` branch that tracks
           `.dross/state.json`, the command exits non-zero with guardLiveState's
           "refusing to switch" message and the live 12-entry history is intact.
           Swapping `checkoutBranch` for a raw `git checkout` fails it.
         - TestPhaseCheckoutMissingBranch: `dross phase checkout nope` exits non-zero
           naming `phase/nope`, and `git branch --list phase/nope` stays empty (it
           must never fall through to `checkout -b`).

Wave 2 (depends t-1, t-2, t-3)

  t-4  Ship prompt performs no branch switch
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go
       covers:   c-1
       depends:  t-3
       description:
         §6.1 GitHub becomes `gh pr merge <pr-url> --squash` — `--delete-branch` is
         dropped, with a line stating dross owns both branch deletions. §6.3's
         description of `dross phase complete` is corrected (it switches to the
         *recorded base*, not main, and writes the completion record itself — no
         chore commit). §0.5 and the Recovery section use `dross phase checkout
         phase/<id>` in place of raw `git checkout`. Forgejo/GitLab merge steps are
         untouched.
       contract:
         - TestShipPromptNoUnguardedSwitch: ship.md contains no `--delete-branch`,
           no `git checkout` and no `git switch` outside fenced examples of *other*
           tools. Re-adding `--delete-branch` to the gh line fails it.
         - TestShipPromptCommandsExist: every `dross <verb> …` invocation in ship.md
           resolves against the assembled cobra tree from `newRoot`, so the prompt
           cannot advertise `dross phase checkout` before t-3 lands it.
         - TestShipPromptCompleteDescription: §6.3 does not contain "switches to
           main" or "chore commit".

  t-5  Complete states the resulting topology
       files:    internal/cmd/topology.go, internal/cmd/topology_test.go,
                 internal/cmd/phase.go
       covers:   c-5
       depends:  t-2
       description:
         New `describeTopology(repoDir, mainBranch, workBranch)` returning the
         HEAD branch, the work branch, and its commit distance from main; degrades to
         a partial answer (never an error) with no origin, no milestone branch, or a
         detached HEAD. phaseComplete's final Printf is replaced by a statement of
         where the run landed: HEAD branch, `deleted phase/<id> (local + origin)`,
         and — only when the reconcile base is not main — `<n> commit(s) on
         <base>, not yet on main`.
       contract:
         - TestCompleteStatesTopologyMilestone: on a milestone-based fixture the last
           lines name `milestone/v1.2`, both deleted branches, and "not yet on main".
         - TestCompleteStatesTopologyMain: on a no-milestone fixture the same run
           reports the work as on main and emits NO "not yet on main" clause — a
           hardcoded clause fails here.
         - TestDescribeTopologyNoOrigin: in a repo with no remote, the helper returns
           a line with the branch names and omits the distance rather than returning
           an error (status must never fail on it).

  t-6  Status reports shipped, not stale
       files:    internal/cmd/status.go, internal/cmd/status_test.go,
                 internal/watch/drift.go, internal/watch/drift_test.go
       covers:   c-5, c-6
       depends:  t-1, t-5
       description:
         Delete the `stale:` block and `stateRecordsCompleted` (its trigger —
         `completed <id>` while on phase/<id> — is unreachable once complete is the
         sole writer). `staleCompletedState` becomes `shippedUnmergedPhase`, keyed on
         `current_phase_status == "shipped"` / the recorded PR number, and renders a
         `shipped:` line naming the PR and the base it is waiting on. Add the
         standing `branch:` topology line from t-5's helper. In watch's drift, a
         phase reading shipped with a recorded PR is not `verified_unshipped`.
       contract:
         - TestStatusReportsShippedNotStale: on phase/<id> with
           current_phase_status = shipped and PR #7 recorded but unmerged on origin,
           status prints `phase: <id> (shipped)` plus a `shipped:` line naming #7,
           and prints no `stale:` line. Restoring the completed-breadcrumb warning
           fails it.
         - TestStatusTopologyLineAlways: status prints the `branch:` line in three
           fixtures — on phase/<id> mid-milestone, on milestone/<v> between phases,
           and in a repo with no origin — and exits 0 in all three.
         - TestDriftShippedPhaseNotUnshipped: a verified phase with
           current_phase_status = shipped and a recorded PR yields no
           `verified_unshipped` drift entry; the same phase without either still does.

Wave 3 (depends wave 2)

  t-7  Reproduce the incident end to end
       files:    internal/cmd/completion_state_truth_test.go
       covers:   c-4
       depends:  t-1, t-2, t-4
       description:
         One regression test over the whole ship→merge→complete flow. Fixture: a
         `dross init` repo whose base branch still tracks a 2-entry `.dross/state.json`
         (the pre-migration shape from `incidentRepo`), a live untracked copy with 12
         entries, a phase branch with a recorded PR, and a stubbed merged PR. Drives
         `dross ship`, simulates the provider squash-merge on the bare origin, then
         `dross phase complete`. Asserts the live copy still holds all 12 entries plus
         `completed <id>`, and that version is unchanged.
       contract:
         - TestShipToCompleteKeepsLiveState fails against pre-phase code on two
           independent counts: complete writes no `completed <id>` (t-2), and the
           control sub-test — a raw `git checkout <base>` standing in for
           `gh pr merge --delete-branch` — drops the history to 2 entries, which is
           the clobber t-4 removes from the flow.
         - The control sub-test is mandatory: without a demonstrated clobber the
           assertions would pass on a fixture that never had one.

  t-8  Retire the rides-the-squash claim everywhere
       files:    internal/cmd/phase.go, assets/prompts/ship.md, ARCHITECTURE.md,
                 internal/cmd/doc_truth_test.go
       covers:   c-3
       depends:  t-1, t-2, t-4, t-5, t-6
       description:
         Rewrite every surface that says the completion record rides the squash:
         phaseComplete's `Long` (lines 236-239) and the comments at phase.go:278 and
         :507-511, ship.md §4 step 5, and the ARCHITECTURE.md phase-lifecycle and
         ship entries. Each says instead: ship marks the phase shipped and leaves
         current_phase set; `dross phase complete` writes the cleared state and the
         `completed <id>` entry to the machine-local, gitignored state.json after the
         merge is confirmed. New grep guard test locks it.
       contract:
         - TestNoSquashCompletionClaims: greps internal/cmd/*.go, assets/prompts/*.md
           and ARCHITECTURE.md for the phrase family — "folds the completion",
           "folded into the squash", "squash-merge will land it", "rides the squash",
           "records the merge in state.json with a chore commit" — and fails on any
           hit. Re-adding ship.md's old §6.3 sentence fails it.
         - TestCompleteLongDescribesOwner: `dross phase complete --help` output
           contains "writes the completion record" and does not contain "'dross ship'
           folds".

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-3, t-4 |
| c-2 | t-1, t-2 |
| c-3 | t-1, t-8 |
| c-4 | t-7 |
| c-5 | t-5, t-6 |
| c-6 | t-6 |

6/6 criteria covered.

## Judgment calls

- **Only GitHub loses its remote-branch deletion.** Rejected unifying teardown by
  also dropping Forgejo's `DELETE …/branches/…` and GitLab's
  `should_remove_source_branch` — `ship_prompt_test.go:147` pins that token as a
  prior phase's locked GitLab merge payload, and neither call switches branches, so
  neither is the hole. `phase complete`'s `ls-remote`-then-`push --delete` is
  already idempotent against a remote branch a provider removed first.
- **Added `dross phase checkout` (t-3) rather than mitigating the §0 pre-flight
  checkout with doctor.** c-1's first clause is absolute — "no step of /dross-ship
  performs a branch switch outside dross's guarded primitives" — and the pre-flight
  `git checkout phase/<id>` is a step of /dross-ship. A phase branch forked off a
  legacy base carries the tracked copy, so the risk is real, not theoretical.
- **The completion record is written after the fast-forward, before teardown
  (t-2).** Rejected writing it last (a failed remote delete would leave a confirmed
  merge unrecorded — F2 re-opened one step later) and rejected writing it before
  the fast-forward (an aborted ff would record a completion that did not happen).
- **`stateRecordsCompleted` is deleted, not kept as a fallback signal (t-6).**
  Keeping it would preserve exactly the reasoning c-6 declares unreachable, and a
  dead branch in a warning path is how the next false warning ships.
- **The regression test carries a deliberate clobber control (t-7).** Mirrors the
  existing `state_clobber_regression_test.go` pattern; without it the test would
  pass on a fixture with nothing to protect, which is the standard way an incident
  test rots into a tautology.
- **docs/roadmap.md:297 left alone (t-8).** It records what the roadmap proposed at
  the time, not a claim about current behaviour; c-3 names code comments, CLI
  narration and prompts.
- **Three tasks touch internal/cmd/phase.go (t-2, t-3, t-5).** Rejected merging
  them into one 3-concern task: they own different failure modes, and the regions
  are disjoint (the `AddCommand` line, the state write, the final Printf). t-5
  depends on t-2 so the ordering inside phaseComplete is decided once.
