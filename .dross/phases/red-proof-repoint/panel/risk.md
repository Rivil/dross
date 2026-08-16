# risk lens — red-proof-repoint

Failure-mode inventory this graph is shaped around (each is owned by exactly one task):

| # | failure | owner |
|---|---|---|
| F1 | doc rewrite clobbers the unrelated SHA, misses an abbreviation, or drifts a byte | t-1 |
| F2 | the replay command — repo-committed, therefore untrusted — is a code-exec vector | t-2 (fence at write), t-6 (gate at spawn) |
| F3 | an indeterminate verdict (shallow clone, no origin refs) is read as "rotted" and mass-rewrites every doc | t-3 |
| F4 | dry-run has side effects — including `phaseForkPoint`'s cache write into changes.json | t-3, t-5 |
| F5 | partial write: record repointed, doc not (or vice versa) — trades one doctor issue for another | t-4 |
| F6 | worktree leak / hung replay / replay run at the wrong commit | t-6 |
| F7 | a repoint failure bricks `dross phase complete` after its merge gate has already passed | t-7 |
| F8 | at complete time the pin is still "reachable" via the branch about to be deleted, so a c-5-correct repoint refuses to fire | t-3 (exclusion), t-7 (wiring) |

```
Phase red-proof-repoint — 7 tasks across 3 waves

Wave 1
  t-1  Rewrite a doc's pinned SHA byte-faithfully
       files:    internal/cmd/redproof_doc.go, internal/cmd/redproof_doc_test.go
       covers:   c-6, c-2
       contract: a doc carrying the pin as a full SHA, as a 7-char abbrev and inside a
                 `git worktree add --detach <sha>` line rewrites exactly those three spans and
                 nothing else — a byte-diff against the original shows no other change;
                 an unrelated 40-hex SHA in the same doc is byte-identical afterwards, and a
                 hex run sharing the pin's first 6 chars but diverging at char 7 is untouched;
                 a 7-char abbrev is replaced by a 7-char prefix of the new SHA, not by 40 chars;
                 a doc with zero occurrences of the old pin returns an error naming the doc
                 rather than reporting an unchanged success.

  t-2  Record the command that replays a proof
       files:    internal/changes/changes.go, internal/cmd/redproof_set.go,
                 internal/cmd/redproof_set_test.go
       covers:   c-8
       contract: `red-proof set --replay 'go test ...'` round-trips into red_proof.replay with
                 sha/doc untouched, and a later `set` without --replay does not silently drop
                 the recorded replay; a --replay beginning with "-" is refused through
                 argfence.RejectLeadingDash (sh honours no end-of-options token) before
                 changes.json is written, leaving the file byte-identical.

  t-3  Decide repoint verdict and resolve target
       files:    internal/cmd/redproof_target.go, internal/cmd/redproof.go,
                 internal/cmd/redproof_target_test.go
       covers:   c-3, c-5, c-4
       description: pure decision layer over a pin — nothing-to-do / repoint-to-<sha> / refuse.
                 Adds an excluded-refs parameter to the reachability classifier and a
                 non-caching fork-point read.
       contract: a pin contained by an origin ref returns nothing-to-do and NO target, so a
                 sound pin cannot be repointed at the decision layer; a shallow clone and a
                 repo with no refs/remotes/origin/* both return nothing-to-do carrying the
                 indeterminate reason, never a rotted verdict; when the resolved fork point
                 itself classifies unreachable the resolver returns a refusal naming the fork
                 point and the reachability reason instead of a target; with
                 refs/remotes/origin/phase/<id> passed as an excluded ref, a pin contained
                 ONLY by that ref classifies unreachable while the same pin without the
                 exclusion classifies reachable; resolving a target for a phase whose
                 changes.json carries no base_commit leaves that file byte-identical (the
                 fork-point cache must not fire on a read-only path).

Wave 2 (depends t-1, t-3)
  t-4  Apply a repoint atomically, with rollback
       files:    internal/cmd/redproof_repoint.go, internal/cmd/redproof_repoint_test.go
       covers:   c-1, c-2
       description: preflight both paths, rewrite the doc, then the record; a verifier seam
                 (defaulted to "unverified") gates the write. Multi-pin loop continues past a
                 refusal.
       depends:  t-1, t-3
       contract: applying to a rotted pin writes the fork point into red_proof.sha AND rewrites
                 the doc, so a following classifyReachability says reachable and
                 redProofDocSHA equals the record — both halves of the cross-check pass in one
                 run; when the changes.json write fails after the doc write (record path made
                 read-only) the doc is restored to its original bytes and the error names both
                 files, so no half-repair survives; a doc that is unreadable or that lacks the
                 old pin aborts before either file is touched; a verifier seam returning a
                 refusal writes neither file; a blanket apply over three pins whose middle one
                 refuses still repairs the other two and returns a non-zero result.

Wave 3
  t-5  Ship `phase red-proof repoint` CLI and doctor hint
       files:    internal/cmd/redproof_repoint_cmd.go, internal/cmd/redproof_set.go,
                 internal/cmd/doctor.go, internal/cmd/redproof_repoint_cmd_test.go,
                 docs/dross.1, README.md
       covers:   c-1, c-4, c-5
       depends:  t-4
       contract: bare `repoint` with no --apply prints the old pin, the proposed commit and
                 every file it would touch while leaving changes.json and the doc
                 byte-identical — including no base_commit cache write; `repoint <phase-id>`
                 over a sound pin prints nothing-to-do and exits 0, and so does a bare run
                 with no rotted pins anywhere; doctor's unreachable-pin line names
                 `dross phase red-proof repoint <phase>` and no longer emits a
                 `red-proof set --sha` line (locked repoint_surface); README.md and
                 docs/dross.1 both mention `repoint` and `Phase()`'s red-proof tree resolves
                 the leaf — the TestDocsCoverRedProofVerb pattern extended, so a shipped verb
                 nobody documented fails CI.

  t-6  Verify the repair by replaying it red
       files:    internal/cmd/redproof_replay.go, internal/cmd/redproof_replay_test.go,
                 internal/cmd/trust.go
       covers:   c-8
       depends:  t-2, t-4
       description: implements the t-4 verifier seam — worktree detached at the proposed
                 commit, consent-gated spawn, timeout, guaranteed teardown.
       contract: with a replay recorded, apply spawns it with cwd inside a worktree detached at
                 the PROPOSED commit (asserted through the spawn seam recording cwd + argv),
                 and that worktree is outside the repo working tree; a replay exiting 0 —
                 green at the proposed commit — refuses the repoint with neither file written
                 and a message saying the proof did not reproduce; the worktree is gone on
                 every path (green, red, timeout), asserted by `git worktree list` carrying no
                 entry and the temp dir being absent; a replay exceeding the timeout is killed
                 and treated as a refusal, not as red; dry-run prints the replay line and the
                 spawn seam records zero invocations; with no consent recorded the apply
                 refuses BEFORE any spawn; with no replay recorded the apply proceeds and its
                 output says the repair is unverified rather than checked; the worktree-add
                 call passes the proposed SHA behind git's end-of-options token, or
                 subprocargs_audit_test flags it.

  t-7  Repoint during phase complete, before teardown
       files:    internal/cmd/phase.go, internal/cmd/redproof_lifecycle_test.go
       covers:   c-7
       depends:  t-4
       description: after the merge gate, before the ff/branch teardown, repoint pins whose
                 only reachability is the doomed phase branch; complete commits both files.
       contract: `dross phase complete` on a phase whose pin is contained ONLY by
                 refs/remotes/origin/phase/<id> leaves that pin classifying reachable after
                 the branch is deleted, with `git status --porcelain` empty because complete
                 committed the record and the fixtures doc together — no hand-run repoint;
                 a phase whose pin is reachable independently of its own branch comes out of
                 complete with changes.json and the doc byte-identical; a repoint that refuses
                 mid-complete (unreachable fork point) prints a deferred hint naming
                 `dross phase red-proof repoint <id> --apply` and complete still finishes —
                 branch deleted, exit 0 — because the merge gate has already passed and a
                 merged phase must not be left uncompletable.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-3, t-4, t-5 |
| c-2 | t-1, t-4 |
| c-3 | t-3 |
| c-4 | t-3, t-5 |
| c-5 | t-3, t-5 |
| c-6 | t-1 |
| c-7 | t-7 |
| c-8 | t-2, t-6 |

8/8 criteria covered.

## Judgment calls

- **Split the decision layer (t-3) from the write layer (t-4)** rather than one `repoint.go`. Chose it because c-3 and c-5 are both *refusals*, and a refusal that lives inside a writer is one `if` away from being skipped; a decision core that can only return "nothing-to-do" for a sound or indeterminate pin makes the c-5 violation unrepresentable. Rejected: a single command file with guards at the top.
- **Indeterminate is nothing-to-do, not a repair.** Chose to treat `cannot-determine` exactly like `reachable`. Rejected: repointing when reachability is unknown — that turns a shallow CI clone into a machine that rewrites every replay doc in the repo, which is a worse outcome than a rotted pin nobody repaired.
- **Excluded-refs parameter on the classifier (t-3), used only by the lifecycle path (t-7).** Chose it because at complete time the pin is still reachable through `origin/phase/<id>`, so a c-5-correct repoint would refuse to fire and c-7 would be unsatisfiable. Rejected: running the lifecycle repoint *after* the teardown — that repoints a pin whose object may already be unreachable, and it puts the repair after the point of no return.
- **Non-caching fork-point read on the read-only path.** `phaseForkPoint` writes `base_commit` back into changes.json on first use, so a dry-run that called it would mutate the record it claims not to touch. Chose a read-only variant. Rejected: letting dry-run cache "harmlessly" — c-4 says dry-run writes nothing, and a diagnostic with a write is exactly the doctor-as-writer shape the locked repoint_surface decision rejects.
- **Doc first, record second, with byte rollback (t-4).** Chose the doc as the first write because it is the larger, likelier-to-fail write and its original bytes are already in memory from t-1's rewrite. Rejected: record-first (rollback would need a re-marshal of a struct that may have changed under us) and a temp-file-rename dance (crosses no filesystem boundary problem here and hides the failure from the error message).
- **Blanket apply continues past a per-pin refusal, exits non-zero.** Chose it because pins are independent and stopping at the first refusal makes the blanket form worse than a loop of single-phase runs. Rejected: abort-on-first — it leaves the user re-running the command N times.
- **Replay execution is consent-gated and never runs in dry-run.** The replay line is committed `.dross/` data, i.e. untrusted input under this repo's own threat model (fixtures/hostile-config-c5). Chose: leading-dash fence at write (t-2), `CheckConsent`-style refusal before any spawn, and no spawn at all without `--apply`. Rejected: running the replay during dry-run so the report could say "verified" — a diagnostic that executes repo-supplied commands is the vulnerability, not the feature.
- **Any non-zero exit counts as red.** Chose it because dross cannot classify a suite's failure mode; a compile-error red and a test-failure red both exit non-zero. Mitigation is reporting the exit code and output tail so the author can see which one they got. Rejected: parsing output for test-failure markers — a per-language classifier that would rot.
- **A repoint refusal warns rather than aborting `dross phase complete` (t-7).** Chose it because by that point the PR is merged and the gate has passed; refusing would leave a merged phase permanently uncompletable, converting a doc-hygiene problem into a lifecycle deadlock. Rejected: hard-failing complete on a repoint refusal.
- **Complete commits the doc as well as the record.** The doc lives under `fixtures/`, which `autoCommitDrossDirt` will not touch, so a lifecycle repoint would otherwise hand back a code-dirty tree and break complete's clean-tree contract on the next run. The locked repoint_commit decision holds — repoint still does not commit; the caller (complete) does.
- **Docs + doctor hint ride with the CLI task (t-5), not a task of their own.** Two one-line edits and a parity assertion is under the too-small bar; splitting them would produce a task whose only content is a string.
