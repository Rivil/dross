# MVP lens — red-proof-repoint

Phase red-proof-repoint — 4 tasks across 2 waves

Wave 1
  t-1  Add red-proof repoint verb
       files:    internal/cmd/redproof_repoint.go, internal/cmd/redproof_set.go,
                 internal/cmd/redproof_repoint_test.go
       covers:   c-1, c-2, c-3, c-4, c-5, c-6
       description:
                 New `dross phase red-proof repoint [phase-id]` registered on the existing
                 phaseRedProof() tree in redproof_set.go. Scans discoverRedProofPins, skips pins
                 classifyReachability calls reachable, resolves each rotted pin's target with
                 phaseForkPoint, refuses a target that is not reachReachable, and on --apply
                 rewrites changes.json (via changes.Load/Save, RedProof.SHA only) plus every
                 literal occurrence of the OLD sha — full or abbreviated prefix — in the pin's doc.
                 Entry point is `repointPin(repoDir, root string, pin redProofPin, doomed []string,
                 apply bool)` so the c-7 caller can name a ref that is about to be deleted; the
                 CLI passes none. Dry-run prints old pin, proposed commit, and each file it would
                 touch.
       contract: - If the doc rewriter starts matching SHAs that are not the old pin,
                   TestRepointDocRewriteSparesUnrelatedSHA fails: repointing
                   fixtures/hostile-config-c5/RUN.md off a6ef7295… must change all three
                   occurrences (the `BASE=` line, the `base commit:` line, the `worktree add
                   --detach` line) and leave the d62be414… mention in the repointed-by note
                   byte-identical, with every other byte of the file equal to the original.
                 - If dry-run stops being the default, TestRepointDryRunWritesNothing fails: a bare
                   repoint over a rotted pin prints the proposed commit while changes.json still
                   holds the old SHA and the doc bytes are unchanged.
                 - If the target-side reachability check is dropped,
                   TestRepointRefusesUnreachableTarget fails: with a fork point contained in no
                   refs/remotes/origin/* ref, --apply returns an error carrying
                   classifyReachability's why-string and the record still holds the old SHA.
                 - If a sound pin stops being a no-op, TestRepointLeavesSoundPinAlone fails: a
                   reachable pin reports nothing-to-do and neither changes.json nor the doc is
                   rewritten (byte-compare both).
                 - If the record and the doc are allowed to diverge after a repair,
                   TestRepointLeavesDoctorClean fails: redProofPinLines for the repaired phase
                   returns exactly one line at doctorOK — no unreachable issue, no
                   prose-disagrees-with-record issue.
       depends_on: []
       status:   pending

Wave 2 (depends t-1)
  t-2  Point doctor hint and docs at repoint
       files:    internal/cmd/doctor.go, README.md, docs/dross.1,
                 internal/cmd/redproof_set_test.go
       description:
                 redProofRepointHint stops emitting a `dross phase red-proof set … --sha <fork>`
                 line and names `dross phase red-proof repoint <phase-id>` instead. README's phase
                 row and the dross.1 red-proof block gain the repoint verb (dry-run default,
                 --apply). TestDocsCoverRedProofVerb extended to assert the `repoint` leaf.
       covers:   c-1
       contract: - If the hint reverts to the `red-proof set` recipe,
                   TestDoctorHintNamesRepoint fails: redProofRepointHint's string for a rotted pin
                   must contain "red-proof repoint" and must not contain "red-proof set".
                 - If the leaf is unregistered or undocumented, TestDocsCoverRedProofVerb fails on
                   the missing `repoint` subcommand under Phase()→red-proof and on README.md /
                   docs/dross.1 not containing "red-proof repoint".
       depends_on: [t-1]
       status:   pending

  t-3  Gate repoint on a recorded replay
       files:    internal/changes/changes.go, internal/cmd/redproof_set.go,
                 internal/cmd/redproof_repoint.go, internal/cmd/redproof_repoint_test.go
       description:
                 changes.RedProof gains `Replay string \`json:"replay,omitempty"\``; `red-proof
                 set` gains an optional --replay-cmd that records it. repoint, on --apply with a
                 replay recorded, runs it through shArgv/spawnLocal in a `git worktree add
                 --detach` checkout of the PROPOSED commit and refuses the repoint unless the
                 command exits non-zero (red), removing the worktree either way. With no replay
                 recorded it applies and states the repair is unverified.
       covers:   c-8
       contract: - If the replay verdict stops gating, TestRepointRefusesGreenReplay fails: a pin
                   whose replay command exits 0 at the proposed commit is refused, changes.json
                   still holds the old SHA, and the doc is unchanged; a command exiting non-zero
                   applies.
                 - If the replay is run anywhere but a detached worktree at the proposed commit,
                   TestRepointReplayRunsAtProposedCommit fails: the spawnLocal seam records a dir
                   that is not the repo root and whose HEAD rev-parses to the proposed SHA, and no
                   `git worktree list` entry survives the run.
                 - If the unverified wording is dropped, TestRepointWithoutReplaySaysUnverified
                   fails: a pin with no replay recorded applies and its output contains
                   "unverified" rather than claiming the repair was checked.
       depends_on: [t-1]
       status:   pending

  t-4  Repoint during phase complete
       files:    internal/cmd/phase.go, internal/cmd/phase_redproof_complete_test.go
       description:
                 phaseComplete calls repointPin for the completing phase after the merge gate and
                 BEFORE the branch teardown, passing refs/remotes/origin/phase/<id> as the doomed
                 ref so a pin held up only by the branch about to be deleted counts as rotted.
                 Applied non-interactively; the touched paths are staged with the completion
                 record so complete still hands back a clean tree (the doc lives under fixtures/,
                 which autoCommitDrossDirt refuses on its own).
       covers:   c-7
       contract: - If the hook is removed or moved after teardown,
                   TestCompleteRepointsPinOnOwnBranch fails: a phase pinned to a commit that only
                   phase/<id> contains, squash-merged and completed, ends with changes.json
                   holding the fork point and redProofPinLines reporting doctorOK — and the
                   pre-hook build of the same fixture reports the unreachable issue.
                 - If the rewritten doc is left uncommitted, TestCompleteLeavesCleanTree fails:
                   `git status --porcelain` is empty after complete and the repointed fixture doc
                   is present in the commit complete created.
       depends_on: [t-1]
       status:   pending

## Coverage

| criterion | tasks |
| --- | --- |
| c-1 | t-1, t-2 |
| c-2 | t-1 |
| c-3 | t-1 |
| c-4 | t-1 |
| c-5 | t-1 |
| c-6 | t-1 |
| c-7 | t-4 |
| c-8 | t-3 |

8/8 criteria covered.

## Judgment calls

- Doc rewriter merged into t-1 rather than split as its own wave-1 task: it is a pure helper with
  exactly one caller, and splitting it would have added a wave without adding parallelism.
- t-1 carries six criteria because c-2/c-4/c-5/c-6 are all statements about one code path
  (plan → refuse-or-write). Splitting them into "dry-run task", "no-op task", "doc task" would
  produce three tasks that cannot be tested independently of the command that runs them.
- `repointPin` takes a doomed-refs argument from t-1 rather than t-4 adding it later: c-7 is in
  scope now, and post-squash-merge the pin is still reachable via origin/phase/<id> until complete
  deletes it, so a hook with no way to say "this ref is about to go" would repoint nothing. The
  CLI verb passes an empty slice.
- Hook placed before branch teardown, not after: repointing after the delete leaves a window where
  a failed run ends with a rotted pin, and c-7 says the repoint happens before the commit goes.
- The complete hook runs the c-8 replay gate like any other repoint, rather than skipping it for
  speed. It only fires when a repair actually happens, which in practice is the phase being
  completed and only when it pinned to its own branch — paying for the verification exactly when a
  pin is being moved is the honest trade.
- Doc rewrite committed by complete alongside the completion record, rather than by repointPin:
  the locked repoint_commit decision says the caller commits, and autoCommitDrossDirt refuses
  non-`.dross/` dirt, so complete must stage the fixture doc explicitly or it will refuse itself.
- Rejected a separate "record the replay command" task ahead of t-3: the field, the --replay-cmd
  flag and the gate are one criterion and one behaviour; three files is still inside the size
  limit.
- Rejected any `doctor --fix`: locked repoint_surface puts the writer beside `red-proof set` and
  keeps doctor diagnostic.
