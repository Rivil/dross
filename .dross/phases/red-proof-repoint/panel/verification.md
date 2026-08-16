# Panel draft — VERIFICATION lens

Every task below was derived by writing the criterion's ideal test contract
first and then asking what the smallest change is that makes that contract
satisfiable. Where a criterion's contract could not be written honestly against
the obvious implementation site (c-7), the site moved — see Judgment calls.

```
Phase red-proof-repoint — 7 tasks across 3 waves

Wave 1
  t-1  Rewrite the pinned SHA byte-faithfully
       files:    internal/cmd/redproof_doc.go, internal/cmd/redproof_doc_test.go
       covers:   c-6, c-2 (doc half)
       desc:     redProofRewriteDoc(body, oldSHA, newSHA) -> (newBody, count).
                 Replaces every literal occurrence of oldSHA — full form or any
                 >=7-char prefix of it — with the new SHA truncated to the same
                 width. Touches nothing else. count == 0 is an error value, not
                 a silent success.
       contract: - feeding the live fixtures/hostile-config-c5/RUN.md through it
                   rewrites exactly the three pin spans (the `base commit:` line,
                   the `BASE=` shell var, the `git worktree add --detach` recipe);
                   the test asserts the output equals the input with only those
                   spans substituted, so any extra byte moved fails it
                 - the unrelated d62be4144c50fac1ba47fab681f46f57da579e6a in the
                   "Repointed by exec-trust-followups" note is byte-identical
                   afterwards; a rewrite keyed to "any 40-hex run" fails this
                 - a 7-char occurrence comes back 7 chars wide (prefix of the new
                   SHA), not expanded to 40 — table-driven over widths 7/8/12/40
                 - a doc that does not contain the old SHA anywhere returns
                   count 0 and an error naming the doc; a caller cannot report a
                   rewrite that did not happen
                 - `redProofSHA(newBody)` returns the new SHA, so the doctor
                   cross-check the rewrite exists to satisfy is asserted on the
                   rewritten bytes, not assumed

  t-2  Plan and apply a repoint (no cobra)
       files:    internal/cmd/redproof_repoint.go, internal/cmd/redproof_repoint_test.go
       covers:   c-3, c-5, c-2 (record half), c-4 (the data it reports)
       desc:     planRedProofRepoint(root, repoDir, pin) -> plan{verdict, oldSHA,
                 newSHA, files, why} over classifyReachability + phaseForkPoint;
                 applyRedProofRepoint(plan) writes the doc first, then the
                 changes.json pin. Callable from both the verb (t-5) and the
                 lifecycle hook (t-6) without going through cobra.
       contract: - a pin classifyReachability calls reachable yields
                   verdict=nothing-to-do, and a test that stats+hashes both
                   changes.json and the doc before and after a full plan+apply
                   run fails if either byte-changed (c-5)
                 - when the owning phase's fork point is itself not reachable
                   (fixture: fork point held only by a local branch, origin/main
                   elsewhere), plan returns refuse, the message carries
                   classifyReachability's own `why`, and neither file is written
                 - when phaseForkPoint errors (phase records no base and no
                   base_commit), plan refuses naming the phase — it never
                   proposes "" as the new SHA
                 - a repoint plan's files list is exactly
                   [.dross/phases/<id>/changes.json, <pin.Doc>], asserted as a
                   set, so t-5's dry-run report cannot understate what it touches
                 - with the doc made unwritable (chmod 0444), apply errors AND
                   changes.json still pins the OLD sha — a repair never
                   half-lands as record-moved/doc-stale, which is the exact
                   issue-swap c-2 forbids
                 - an indeterminate verdict (shallow clone) is nothing-to-do,
                   not a repoint: repointing on "I cannot tell" would rewrite a
                   sound pin in every shallow CI clone

  t-3  Record a replay command on the pin
       files:    internal/changes/changes.go, internal/cmd/redproof_set.go,
                 internal/cmd/redproof_set_test.go
       covers:   c-8 (recording half)
       desc:     changes.RedProof gains Replay string (omitempty);
                 `dross phase red-proof set` gains an optional --replay. Absent
                 replay stays absent — the ~1 existing pin must round-trip
                 unchanged.
       contract: - `red-proof set <id> --sha X --doc D --replay "go test ./x"`
                   round-trips Replay through changes.Load; re-running set
                   without --replay on a pin that has one does not blank it
                   silently (it either preserves or refuses, asserted either way)
                 - a pin recorded before this field (the live
                   config-trust-hardening record) loads with Replay == "" and
                   re-saves byte-identically apart from the fields set — pinned
                   by a JSON round-trip assertion, so an added required field
                   would fail
                 - an empty/whitespace --replay is refused rather than stored:
                   a blank command would read as "recorded" to t-5's
                   verified/unverified split and be spawned as nothing

  t-4  Run a recorded replay red, consent-gated
       files:    internal/cmd/redproof_replay.go, internal/cmd/trust.go,
                 internal/cmd/local.go, internal/cmd/redproof_replay_test.go
       covers:   c-8 (verification half)
       desc:     runRedProofReplay(repoDir, sha, line) adds a detached worktree
                 at sha, runs line through the existing spawnLocal/shArgv seam
                 with the worktree as cwd, removes the worktree, and returns
                 red/green. Refuses to spawn unless sha256(line) matches a
                 fingerprint in .dross/local.toml's new trusted_replay_commands
                 (gitignored, absent from localKeys, granted by `dross trust
                 --replay`), mirroring TrustedTestCommand and remote_grant.go.
       contract: - a replay line with no consent fingerprint is refused with the
                   grant command named, and the spawnLocal seam (replaced in the
                   test) records ZERO invocations — the refusal is before the
                   spawn, not after it
                 - a consented line that exits non-zero returns red=true; one
                   that exits 0 returns red=false with the captured tail — the
                   two are distinct returns, not both "no error"
                 - the command runs with cwd inside the worktree at sha, not in
                   repoDir: the fake spawnLocal asserts `git -C <dir> rev-parse
                   HEAD` == sha
                 - the worktree is removed on BOTH paths (green run and spawn
                   error): `git worktree list` after each case shows only the
                   main tree, so a refused repoint leaves no /tmp tree behind
                 - a replay line beginning with "-" is rejected by shArgv's
                   argfence before any spawn (the .dross-is-untrusted-input
                   contract the c-5 fixture's threat model states)

Wave 2 (depends t-1, t-2, t-3, t-4)
  t-5  Add `dross phase red-proof repoint`
       files:    internal/cmd/redproof_repoint_cmd.go,
                 internal/cmd/redproof_repoint_cmd_test.go
       covers:   c-1, c-2, c-4, c-8 (surface + reporting)
       desc:     Cobra sibling of `red-proof set`: bare form scans every
                 discovered pin, optional <phase-id> narrows to one; dry-run by
                 default, --apply writes. Wires t-2's plan/apply, t-1's rewrite
                 and t-4's replay gate, and prints the per-pin outcome.
       depends:  t-1, t-2, t-3, t-4
       contract: - c-4 dry run: on a rotted pin, stdout names the old SHA, the
                   proposed SHA and BOTH file paths, and afterwards changes.json
                   and the doc are byte-identical to before — asserted by hash,
                   not by absence of an error
                 - c-1 end-to-end: fixture repo where the pin's commit lives only
                   on a deleted local branch; one `repoint --apply` run, then
                   redProofChecks over the same root returns no doctorIssue line
                   for that phase and no test touched a file by hand
                 - c-2: after --apply, redProofDocSHA(doc) == the new recorded
                   SHA — the run that fixes the reachability issue is asserted
                   not to have created the prose-disagrees-with-record issue
                 - c-3 at the verb level: an unreachable proposed target exits
                   non-zero with the reason and writes neither file
                 - c-5: a repo whose only pin is sound prints nothing-to-do,
                   exits 0, and leaves both files byte-identical — including
                   under --apply
                 - blanket scan: two phases, one rotted and one sound; the bare
                   `repoint --apply` repairs only the rotted one and the sound
                   phase's changes.json is byte-identical (the locked
                   repoint_target_selection decision)
                 - c-8 recorded: a pin with a consented replay whose command
                   exits 0 at the proposed commit is REFUSED — the output says
                   the proof did not go red there and both files are unchanged
                 - c-8 unrecorded: a pin with no Replay is repointed, and stdout
                   carries the word "unverified" (no replay recorded); the test
                   asserts the run does NOT print any claim of a checked/verified
                   replay

  t-6  Auto-repoint doomed pins at ship; guard at complete
       files:    internal/cmd/ship.go, internal/cmd/phase.go,
                 internal/cmd/redproof_lifecycle.go,
                 internal/cmd/redproof_lifecycle_test.go
       covers:   c-7
       desc:     Ship, before its pre-stage clean-tree gate, repoints any pin
                 reachable ONLY from refs/remotes/origin/phase/<id> (a doomed
                 ref) via t-2's apply path plus t-4's replay gate, and commits
                 the two files as `chore(dross): repoint red proof for <id>` so
                 the rewrite rides the phase's own PR. `phase complete` gains a
                 pre-teardown check that refuses to delete the branch while a
                 discovered pin is reachable only through it.
       depends:  t-1, t-2, t-4
       contract: - c-7 end-to-end: fixture where the pin names a commit on
                   phase/<id> only; run ship (push/PR stubbed), simulate the
                   squash-merge (base fast-forwarded, phase branch deleted local
                   + origin), then classifyReachability on the recorded pin
                   returns reachable and redProofChecks reports no issue — with
                   no repoint invoked by the test
                 - the doomed-pin predicate is asserted distinct from the rotted
                   predicate: the same fixture, run through t-5's verb, reports
                   nothing-to-do (the pin is still reachable, c-5), while the
                   ship hook repoints it. Both assertions live in one test so the
                   tension is pinned, not discovered later
                 - ship does not refuse on its own rewrite: after the hook, ship's
                   autoCommitDrossDirt sees a clean tree, and the commit it made
                   touches exactly [changes.json, the doc] — asserted with
                   diff-tree --name-only on the phase branch tip
                 - the hook never writes on the base branch: a test that runs the
                   complete path over a repo whose pin is already sound asserts
                   local <base> is not ahead of origin/<base> with any non-.dross
                   commit, i.e. pushBaseIfAheadDrossOnly (basebranch.go:96) still
                   returns without refusing
                 - complete's guard: with a doomed pin still recorded, complete
                   refuses before `branch -D`, the message names the phase and
                   the repoint verb, and `git rev-parse refs/heads/phase/<id>`
                   still resolves afterwards

Wave 3 (depends t-5)
  t-7  Point doctor's hint at repoint; docs
       files:    internal/cmd/doctor.go, README.md, docs/dross.1,
                 internal/cmd/redproof_test.go
       covers:   c-1 (the diagnostic half)
       desc:     redProofRepointHint stops emitting a `red-proof set --sha …`
                 line and names `dross phase red-proof repoint <phase>` (locked
                 repoint_surface). README/man gain the verb.
       depends:  t-5
       contract: - the unreachable-pin doctor line contains
                   "red-proof repoint" and no longer contains "--sha", so the
                   hint cannot drift back to the hand-typed form
                 - the command the hint names resolves on the real CLI tree:
                   walking Phase() finds red-proof > repoint, extending the
                   existing TestDocsCoverRedProofVerb assertion — a hint
                   narrating a command that does not resolve fails here
                 - the hint still degrades gracefully when phaseForkPoint errors:
                   the fallback text names the phase and does not print a
                   copy-pasteable command with an empty SHA in it
                 - README.md and docs/dross.1 both mention `repoint`, on the same
                   docs-coverage test that already gates `red-proof`
```

## Coverage

| criterion | tasks |
| --- | --- |
| c-1 | t-5 (repair in one command), t-7 (doctor hint + resolves) |
| c-2 | t-1 (doc rewrite), t-2 (write ordering), t-5 (post-apply cross-check) |
| c-3 | t-2 (refusal + no write), t-5 (verb-level exit) |
| c-4 | t-5 (dry-run report + byte-identical files), t-2 (files list it reports) |
| c-5 | t-2 (nothing-to-do verdict), t-5 (verb + blanket scan) |
| c-6 | t-1 |
| c-7 | t-6 |
| c-8 | t-3 (record), t-4 (consented worktree red run), t-5 (refuse-unless-red / unverified wording) |

8/8 criteria covered.

## Judgment calls

- **c-7's hook goes in `ship` (pre-push), not `phase complete`.** Chose: repoint
  while HEAD is still on phase/<id> so the rewrite rides the phase's own PR.
  Rejected: writing it during complete — the doc lives under `fixtures/`, so a
  complete-time rewrite lands a non-`.dross/` commit on the reconcile branch and
  `pushBaseIfAheadDrossOnly` (internal/cmd/basebranch.go:96) hard-refuses exactly
  that, breaking the command that was supposed to perform the repair. Complete
  keeps a pre-teardown *guard* (refuse to delete a branch that is the last ref
  holding a pin), which is testable without writing anything.
- **Two predicates, not one.** Chose: the verb repairs `unreachable` pins; the
  ship hook repairs `reachable only from the doomed phase ref`. Rejected: one
  shared predicate — c-5 requires a currently-reachable pin to be a no-op for the
  verb, and a pin on a live phase branch is currently reachable. Merging them
  would make c-5 and c-7 contradict; the tension is pinned in one test in t-6.
- **Replay commands are consent-bound, reusing the trust.go store.** Chose: a new
  `trusted_replay_commands` fingerprint list in the gitignored local.toml,
  granted by a verb. Rejected: spawning whatever `changes.json` records — that
  file is tracked, and the c-5 fixture's own threat model states `.dross/` is
  untrusted input; repoint would otherwise be remote code execution on clone.
  Rejected also: overloading `TrustedTestCommand`, which is one fingerprint bound
  to one command.
- **Apply writes the doc first, the record second.** Chose that order so a failed
  doc write leaves the record pinning the old (rotted) SHA — still a doctor
  issue, but the *same* issue. Rejected record-first, which converts an
  unreachable-pin finding into a doc-disagrees finding on failure: precisely the
  trade c-2 forbids.
- **Abbreviated occurrences are replaced at their own width.** Chose:
  same-length prefix of the new SHA. Rejected: expanding every hit to 40 chars —
  it changes bytes the criterion says to preserve (line shape, table alignment)
  for no gain, since `sameCommitSHA` already compares by prefix.
- **Zero occurrences in the doc is an error.** Chose: refuse the repoint and name
  the doc. Rejected: writing the record anyway — that produces a green record
  next to prose still naming a dead commit, which is the c-2 failure with extra
  steps.
- **Plan/apply core lives outside cobra (t-2), the verb is a thin shell (t-5).**
  Chose this split so the ship hook (t-6) calls a function rather than
  re-entering the command tree, which lets t-6 run in wave 2 beside t-5 instead
  of queueing behind it.
