# Panel synthesis — red-proof-repoint

Judged cold: I authored none of the three drafts. Every file, symbol and line
number the drafts lean on was checked against the tree before scoring.

## Scores

| dimension | risk | mvp | verification |
|---|---|---|---|
| criteria coverage | 8/8, plus a failure-mode table that assigns each failure exactly one owner — the only draft that names F3 (indeterminate read as rotted) and F4 (dry-run's fork-point cache write) as first-class risks | 8/8, but six criteria hang off one task, so "covered" and "tested independently" come apart | 8/8, derived criterion-first; c-8 is the only one split three ways (record / run / report), which matches the criterion's own three clauses |
| test-contract specificity | highest on failure paths: rollback bytes, worktree gone on green/red/timeout, blanket run continues past a refusal, argv behind git's end-of-options token | best *format* — "if X regresses, TestY fails" with real test names — but thinner on the negative paths (no rollback, no worktree-leak, no timeout assertion) | highest on values: cites the live `d62be414…` unrelated SHA, chmod 0444 for the half-write case, `redProofSHA(newBody)` re-parsed on the rewritten bytes, spawn seam recording zero invocations |
| granularity | good; the plan/apply split is the one place it over-cuts (both halves share one struct and one test file) | too coarse: t-1 is doc rewriter + verdict + resolver + writer + cobra verb in one commit, covering c-1..c-6 | right-sized; t-7 (hint + docs) is thin but its docs-coverage assertion is a real CI gate, not a string edit |
| wave correctness | 3 waves, correct: wave-1 trio is genuinely independent, wave-3 trio all fan out from t-4 | 2 waves, correct but degenerate — everything queues behind one oversized t-1, so the parallelism is an artifact of the bundling | 3 waves, correct and the tightest: four independent wave-1 tasks, and the plan/apply-outside-cobra split is what lets the lifecycle task run *beside* the verb instead of behind it |

**Skeleton: verification.** It wins on the two dimensions that decide execution
order and executability — contract specificity anchored to real values, and a
wave split that is parallel because the seams are real rather than because
work was bundled. It is also the only draft whose c-7 placement survives
contact with the tree: `pushBaseIfAheadDrossOnly` (internal/cmd/basebranch.go:62,
refusal at :96) hard-refuses a non-`.dross/` commit sitting ahead on the base,
and `dross phase complete` calls it at internal/cmd/phase.go:599 — *before*
`branch -D` at :607. A complete-time rewrite of `fixtures/…/RUN.md` therefore
breaks the very command performing the repair. risk and mvp both put the write
there.

risk is the strongest runner-up and contributes the most grafts: it is the only
draft that saw the dry-run write hazard, and the only one that treats an
indeterminate verdict as a distinct outcome from a rotted one.

## Merged plan

7 tasks across 3 waves.

```
Wave 1

  t-1  Rewrite the pinned SHA byte-faithfully                    [verification + risk + mvp]
       files:    internal/cmd/redproof_doc.go, internal/cmd/redproof_doc_test.go
       covers:   c-6, c-2 (doc half)
       desc:     redProofRewriteDoc(body, oldSHA, newSHA) -> (newBody, count, err).
                 Replaces every literal occurrence of oldSHA — full form or any
                 >=7-char prefix — with the new SHA truncated to the same width.
                 count == 0 is an error value, never a silent success.
       contract: - the live fixtures/hostile-config-c5/RUN.md rewrites exactly the
                   three a6ef7295… spans (RUN.md:92 `base commit:`, :99 `BASE=`,
                   :143 the `git worktree add --detach` recipe); the assertion is
                   output == input with only those spans substituted, so any other
                   byte moving fails it                            [verification+mvp]
                 - the unrelated d62be4144c50fac1ba47fab681f46f57da579e6a at
                   RUN.md:106 is byte-identical afterwards; a rewriter keyed to
                   "any 40-hex run" fails here                     [verification+mvp]
                 - a hex run sharing the pin's first 6 chars but diverging at char 7
                   is untouched                                    [risk]
                 - table-driven over widths 7/8/12/40: a 7-char occurrence comes
                   back 7 chars wide, not expanded to 40           [verification+risk]
                 - a doc with zero occurrences returns count 0 and an error naming
                   the doc — a caller cannot report a rewrite that did not happen
                                                                   [verification+risk]
                 - redProofSHA(newBody) returns the new SHA, so the cross-check
                   this rewrite exists to satisfy is asserted on the rewritten
                   bytes rather than assumed                       [verification]
       note:     all three live occurrences are full 40-char. The abbreviated-width
                 cases are required by the locked doc_rewrite_scope decision and by
                 sameCommitSHA's prefix comparison (doctor.go:575), but they must be
                 built as synthetic fixtures — asserting an abbrev against the live
                 RUN.md will fail.                                 [judge fact-check]

  t-2  Plan and apply a repoint (no cobra)                       [verification + risk]
       files:    internal/cmd/redproof_repoint.go, internal/cmd/redproof_repoint_test.go
       covers:   c-3, c-5, c-2 (record half), c-4 (the data it reports)
       desc:     planRedProofRepoint(root, repoDir, pin, excluded []string) ->
                 plan{verdict, oldSHA, newSHA, files, why} over classifyReachability
                 + a non-caching fork-point read; applyRedProofRepoint(plan) writes
                 the doc first, the changes.json pin second. Callable from both the
                 verb (t-5) and the lifecycle hook (t-6) without re-entering cobra.
                 The excluded-refs parameter is threaded from here so t-6 can name a
                 ref that is about to die; the verb passes none.
       contract: - a pin classifyReachability calls reachable yields nothing-to-do
                   and carries NO target — a sound pin cannot be repointed at the
                   decision layer even if a writer wanted to; a plan+apply run over
                   it leaves both changes.json and the doc byte-identical, asserted
                   by hash                                         [verification+risk]
                 - an indeterminate verdict (shallow clone, and separately a repo
                   with no refs/remotes/origin/* refs) is nothing-to-do carrying the
                   indeterminate reason, never rotted — repointing on "I cannot
                   tell" would rewrite sound pins in every shallow CI clone
                                                                   [risk+verification]
                 - when the resolved fork point itself classifies unreachable, plan
                   refuses naming the fork point and carrying classifyReachability's
                   own why-string; neither file is written          [both]
                 - when the fork point cannot be resolved at all (no base, no
                   base_commit) plan refuses naming the phase — it never proposes
                   "" as the new SHA                                [verification]
                 - planning for a phase whose changes.json carries no base_commit
                   leaves that file BYTE-IDENTICAL: phaseForkPoint
                   (internal/cmd/forkpoint.go:30) caches the resolved SHA back and
                   Save()s it, so the read-only path must use a non-caching variant
                                                                   [risk — missed by both others]
                 - the plan's files list is exactly
                   [.dross/phases/<id>/changes.json, <pin.Doc>], asserted as a set,
                   so t-5's dry-run report cannot understate what it touches
                                                                   [verification]
                 - with the doc unwritable (chmod 0444), apply errors AND
                   changes.json still pins the OLD sha; with the record path made
                   read-only after a successful doc write, the doc is restored to
                   its original bytes and the error names both files. No half-repair
                   survives in either direction — the exact issue-swap c-2 forbids
                                                                   [verification + risk rollback half]
                 - with refs/remotes/origin/phase/<id> passed as an excluded ref, a
                   pin contained ONLY by that ref classifies unreachable, while the
                   same pin with no exclusion classifies reachable  [risk]

  t-3  Record a replay command on the pin                        [risk + verification]
       files:    internal/changes/changes.go, internal/cmd/redproof_set.go,
                 internal/cmd/redproof_set_test.go
       covers:   c-8 (recording half)
       desc:     changes.RedProof (changes.go:68) gains Replay string
                 `json:"replay,omitempty"`; `dross phase red-proof set` gains an
                 optional --replay.
       contract: - `red-proof set <id> --sha X --doc D --replay "go test ./x"`
                   round-trips Replay through changes.Load; a later set without
                   --replay on a pin that has one does not silently blank it (it
                   preserves or refuses — asserted either way)      [risk+verification]
                 - the live config-trust-hardening record, written before this
                   field, loads with Replay == "" and re-saves byte-identically
                   apart from the fields set — a JSON round-trip assertion, so an
                   accidentally-required field fails here           [verification]
                 - an empty/whitespace --replay is refused rather than stored: a
                   blank command reads as "recorded" to t-5's verified/unverified
                   split and would be spawned as nothing            [verification]
                 - a --replay beginning with "-" is refused through
                   argfence.RejectLeadingDash (internal/argfence/argfence.go:36 —
                   sh honours no end-of-options token) BEFORE changes.json is
                   written, leaving the file byte-identical         [risk]

  t-4  Run a recorded replay red, consent-gated                   [verification + risk]
       files:    internal/cmd/redproof_replay.go, internal/cmd/trust.go,
                 internal/cmd/local.go, internal/cmd/redproof_replay_test.go
       covers:   c-8 (verification half)
       desc:     runRedProofReplay(repoDir, sha, line) adds a detached worktree at
                 sha, runs line through the existing shArgv/spawnLocal seam
                 (internal/cmd/test.go:78, :111) with the worktree as cwd, removes
                 the worktree, and returns red/green + captured tail. Refuses to
                 spawn unless sha256(line) matches a fingerprint in local.toml's new
                 trusted_replay_commands (gitignored, absent from localKeys, granted
                 by a verb), mirroring TrustedTestCommand (local.go:78, trust.go:127).
       contract: - a line with no consent fingerprint is refused with the grant
                   command named, and the replaced spawnLocal seam records ZERO
                   invocations — the refusal precedes the spawn      [verification+risk]
                 - a consented line exiting non-zero returns red=true; one exiting 0
                   returns red=false with the tail — two distinct returns, not both
                   "no error". Any non-zero exit counts as red (dross cannot tell a
                   compile-error red from a test-failure red); the exit code and tail
                   are reported so the author can                    [verification+risk]
                 - cwd is inside the worktree at sha, not repoDir: the fake spawn
                   seam asserts `git -C <dir> rev-parse HEAD` == sha, and the dir is
                   outside the repo working tree                     [verification+risk]
                 - the worktree is gone on EVERY path — green, red, spawn error and
                   timeout — asserted by `git worktree list` carrying no entry and
                   the temp dir being absent                         [risk broadened]
                 - a replay exceeding a timeout is killed and treated as a REFUSAL,
                   not as red                                        [risk]
                 - the `git worktree add --detach <sha>` call passes the SHA behind
                   git's end-of-options token, or subprocargs_audit_test flags it
                                                                     [risk]

Wave 2 (depends t-1, t-2, t-3, t-4)

  t-5  Add `dross phase red-proof repoint`                        [verification + mvp]
       files:    internal/cmd/redproof_repoint_cmd.go, internal/cmd/redproof_set.go,
                 internal/cmd/redproof_repoint_cmd_test.go
       covers:   c-1, c-2, c-4, c-5, c-8 (surface + reporting)
       depends:  t-1, t-2, t-3, t-4
       desc:     Cobra sibling of `red-proof set` on the existing phaseRedProof()
                 tree: bare form scans discoverRedProofPins (redproof.go:119),
                 optional <phase-id> narrows to one; dry-run by default, --apply
                 writes. Wires t-2's plan/apply, t-1's rewrite and t-4's replay gate,
                 printing the per-pin outcome. Passes no excluded refs.
       contract: - c-4: on a rotted pin, stdout names the old SHA, the proposed SHA
                   and BOTH file paths, and afterwards changes.json and the doc are
                   byte-identical — asserted by hash, not by absence of an error, and
                   including no base_commit cache write             [verification+risk]
                 - c-1 end-to-end: fixture repo where the pin's commit lives only on
                   a deleted branch; one `repoint --apply`, then redProofChecks
                   (doctor.go:500) returns no issue line for that phase, with no file
                   touched by hand                                   [verification]
                 - c-2: after --apply, redProofDocSHA(doc) == the newly recorded SHA
                   — the run that fixes the reachability issue is asserted not to have
                   created the prose-disagrees-with-record issue     [verification+mvp]
                 - c-3 at the verb level: an unreachable proposed target exits
                   non-zero with the reason and writes neither file  [verification]
                 - c-5: a repo whose only pin is sound prints nothing-to-do, exits 0,
                   and leaves both files byte-identical — including under --apply
                                                                     [verification+mvp]
                 - blanket scan, two phases (one rotted, one sound): `repoint --apply`
                   repairs only the rotted one and the sound phase's changes.json is
                   byte-identical (locked repoint_target_selection)  [verification]
                 - blanket run over three rotted pins whose middle one refuses still
                   repairs the other two and exits non-zero — stopping at the first
                   refusal makes the blanket form worse than N single runs
                                                                     [risk — missed by both others]
                 - c-8 recorded: a pin with a consented replay whose command exits 0
                   at the proposed commit is REFUSED — output says the proof did not
                   go red there and both files are unchanged         [verification+mvp]
                 - c-8 unrecorded: a pin with no Replay is repointed and stdout
                   carries "unverified"; the test asserts the run does NOT print any
                   claim of a checked replay                         [all three]
                 - dry-run prints the replay line and the spawn seam records zero
                   invocations — a diagnostic never executes repo-supplied commands
                                                                     [risk]

  t-6  Auto-repoint doomed pins at ship; warn at complete         [verification + risk graft]
       files:    internal/cmd/ship.go, internal/cmd/phase.go,
                 internal/cmd/redproof_lifecycle.go,
                 internal/cmd/redproof_lifecycle_test.go
       covers:   c-7
       depends:  t-1, t-2, t-4
       desc:     Ship, before its pre-stage clean-tree gate (autoCommitDrossDirt at
                 ship.go:228), repoints any pin reachable ONLY from
                 refs/remotes/origin/phase/<id> via t-2's apply path plus t-4's replay
                 gate, and commits the two files as
                 `chore(dross): repoint red proof for <id>` so the rewrite rides the
                 phase's own PR. `phase complete` gains a pre-teardown check that
                 WARNS — naming the phase and the repoint verb — when a discovered pin
                 is held only by the branch it is about to delete, then proceeds.
       contract: - c-7 end-to-end: fixture where the pin names a commit on phase/<id>
                   only; run ship (push/PR stubbed), simulate the squash-merge (base
                   fast-forwarded, phase branch deleted local + origin), then
                   classifyReachability on the recorded pin returns reachable and
                   redProofChecks reports no issue — with no repoint invoked by the
                   test                                              [verification]
                 - the doomed-pin predicate is asserted DISTINCT from the rotted
                   predicate: the same fixture run through t-5's verb reports
                   nothing-to-do (still reachable, c-5) while the ship hook repoints
                   it. Both assertions live in one test so the c-5/c-7 tension is
                   pinned rather than discovered later                [verification]
                 - ship does not refuse on its own rewrite: after the hook,
                   autoCommitDrossDirt sees a clean tree, and the commit it made
                   touches exactly [changes.json, the doc], asserted with
                   `diff-tree --name-only` on the phase branch tip    [verification]
                 - the hook never writes on the base branch: over a repo whose pin is
                   already sound, local <base> is not ahead of origin/<base> with any
                   non-.dross commit, i.e. pushBaseIfAheadDrossOnly
                   (basebranch.go:62, refusal at :96) still returns without refusing
                                                                      [verification]
                 - complete's warning path: with a doomed pin still recorded (ship
                   hook skipped or refused), complete prints a deferred hint naming
                   `dross phase red-proof repoint <id> --apply` and STILL FINISHES —
                   branch deleted, exit 0. The merge gate (phase.go:448) has already
                   passed by then, and a merged phase must never be left
                   uncompletable                                      [risk graft over verification's guard]

Wave 3 (depends t-5)

  t-7  Point doctor's hint at repoint; docs                       [mvp + verification]
       files:    internal/cmd/doctor.go, README.md, docs/dross.1,
                 internal/cmd/redproof_set_test.go
       covers:   c-1 (the diagnostic half)
       depends:  t-5
       desc:     redProofRepointHint (doctor.go:564) stops emitting the
                 `dross phase red-proof set <id> --sha <fork> --doc <doc>` line and
                 names `dross phase red-proof repoint <phase>` instead (locked
                 repoint_surface). README's phase row and the dross.1 red-proof block
                 gain the verb (dry-run default, --apply).
       contract: - the unreachable-pin doctor line contains "red-proof repoint" and
                   no longer contains "red-proof set" / "--sha", so the hint cannot
                   drift back to the hand-typed form                  [mvp+verification]
                 - the command the hint names RESOLVES on the real CLI tree: walking
                   Phase() finds red-proof > repoint — the existing
                   TestDocsCoverRedProofVerb (redproof_set_test.go:182) extended, so a
                   hint narrating a command that does not resolve fails here
                                                                      [all three]
                 - the hint still degrades gracefully when phaseForkPoint errors: the
                   fallback names the phase and does not print a copy-pasteable
                   command with an empty SHA in it                    [verification]
                 - README.md and docs/dross.1 both mention `repoint`, on the same
                   docs-coverage test that already gates `red-proof` [all three]
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 | t-2, t-5, t-7 |
| c-2 | t-1, t-2, t-5 |
| c-3 | t-2, t-5 |
| c-4 | t-2, t-5 |
| c-5 | t-2, t-5 |
| c-6 | t-1 |
| c-7 | t-6 |
| c-8 | t-3, t-4, t-5 |

8/8 criteria covered. No criterion is satisfied by a single task's assertion
alone except c-6, which is a pure-function property.

## Disagreements

### 1. Where the c-7 lifecycle repoint writes — ship vs complete

- **risk (t-7) and mvp (t-4):** inside `dross phase complete`, after the merge
  gate and before branch teardown; complete commits the record and the fixtures
  doc together so it hands back a clean tree.
- **verification (t-6):** inside `ship`, before the pre-stage clean-tree gate, so
  the rewrite rides the phase's own PR; complete keeps only a pre-teardown check.
- **Provisional default: ship.** Checked against the tree, and the complete-time
  write does not survive it: complete calls `pushBaseIfAheadDrossOnly` at
  phase.go:599 — *before* `branch -D` at :607 — and that function
  (basebranch.go:62) hard-refuses when local base is ahead with a commit touching
  a non-`.dross/` path (refusal at :96). The doc lives under `fixtures/`. So the
  complete-time repoint would commit the doc onto the reconcile branch and then
  break complete at its own safety-net push. Both risk and mvp reasoned about
  `autoCommitDrossDirt` refusing non-`.dross` dirt and concluded "complete must
  stage it explicitly" — correct as far as it goes, but they missed the second
  gate downstream.
- **Why it matters:** this is the only divergence that changes which command a
  user runs to get c-7's guarantee, which file the fix appears in for review, and
  whether the phase's own PR contains the doc rewrite. It also moves the c-7
  fixture from a post-merge simulation to a pre-push one. If ship is rejected in
  favour of complete, the plan must additionally solve the basebranch refusal —
  that is real extra scope, not a one-line move.

### 2. What complete does when a doomed pin is still recorded

- **verification (t-6):** complete REFUSES before `branch -D`, naming the phase
  and the repoint verb; the branch survives.
- **risk (t-7):** a repoint problem must never abort complete — by that point the
  PR is merged and the merge gate has passed, so refusing converts a doc-hygiene
  problem into a permanent lifecycle deadlock. Print a deferred hint and finish.
- **Provisional default: warn and proceed** (risk's stance applied to
  verification's site). A merged phase that cannot be completed is unrecoverable
  by the user without hand-editing state; a rotted pin is recoverable by running
  the verb t-5 ships.
- **Why it matters:** it is the difference between a safe artifact and a safe
  lifecycle, and the two lenses picked opposite sides. Choosing "refuse" also
  makes c-7 dependent on the guard rather than on the ship hook, which changes
  what t-6's test has to prove.

### 3. Consent gating the recorded replay command

- **verification (t-4):** a new `trusted_replay_commands` fingerprint list in the
  gitignored local.toml plus a grant verb, mirroring `TrustedTestCommand`; refuse
  to spawn without it.
- **risk (t-6):** a `CheckConsent`-style refusal before any spawn — same shape,
  no store design named.
- **mvp (t-3):** no consent gate at all — run the recorded line through
  shArgv/spawnLocal.
- **Provisional default: consent-gated,** with verification's store design. Two
  lenses require it, and `changes.json` is a tracked file: this repo's own
  fixture (fixtures/hostile-config-c5) exists because `.dross/` is treated as
  untrusted input, so spawning what it records is remote code execution on clone.
- **Why it matters:** this is the largest piece of scope in the plan that c-8's
  text does not itself ask for — a new local.toml key, a grant verb, and its
  tests. If it is cut, t-4 shrinks substantially and the leading-dash fence in
  t-3 becomes the only barrier between a cloned repo and a spawned command.

### 4. Granularity of the plan/apply core

- **risk:** two tasks — a pure decision layer (t-3) and a writer (t-4) — on the
  argument that a refusal living inside a writer is one `if` away from being
  skipped.
- **verification:** one non-cobra task (t-2) holding plan and apply.
- **mvp:** one task holding plan, apply, the doc rewriter and the cobra verb.
- **Provisional default: one task (verification's t-2),** carrying risk's
  strongest contract lines — nothing-to-do returns NO target, indeterminate is
  not rotted, the fork-point read does not cache. Plan and apply share one struct
  and one test file; splitting them adds a wave without adding parallelism. mvp's
  bundling is rejected outright: a single commit spanning c-1 through c-6 has no
  independently-observable failure surface.
- **Why it matters:** if the executor finds mid-task that the refusal logic wants
  to be a separately-testable unit, risk's split is the fallback and it costs one
  extra wave.

### 5. Whether the replay-recording field is its own task

- **risk (t-2) and verification (t-3):** yes — the schema field plus `--replay`
  on `red-proof set`, independent of the runner.
- **mvp (t-3):** no — field, flag and gate are one criterion and one behaviour.
- **Provisional default: separate task.** It is the only wave-1 task that touches
  `internal/changes`, and keeping it separate lets t-4 (the runner) develop
  against a schema that already exists rather than one it is inventing.
- **Why it matters:** low stakes, but it sets whether wave 1 has three or four
  parallel tasks.

### 6. Doctor hint and docs — own task or folded into the verb

- **mvp (t-2) and verification (t-7):** own task, in the wave after the verb.
- **risk:** folded into the CLI task (t-5); two one-line edits and a parity
  assertion is under the too-small bar.
- **Provisional default: own task (wave 3).** risk is right that the edits are
  small, but the docs-coverage assertion is a CI gate that fails when a verb
  ships undocumented, and giving it its own commit is what makes that gate
  legible in history. If wave 3 looks wasteful during execution, folding it into
  t-5 is a safe collapse — no other task depends on it.
- **Why it matters:** it is the difference between a 3-wave and a 2-wave tail.

### 7. The fork-point cache write on the read-only path

- **risk (F4, t-3, t-5):** `phaseForkPoint` writes `base_commit` back into
  changes.json on first use, so a dry-run that calls it mutates the record it
  claims not to touch; a non-caching variant is required.
- **mvp and verification:** silent — both call `phaseForkPoint` directly from the
  planning path.
- **Provisional default: risk is right, grafted into t-2 and t-5 as contract
  lines.** Verified: forkpoint.go:47-50 sets `c.BaseCommit` and calls `c.Save`.
- **Why it matters:** without it, c-4's "writes only when an explicit apply flag
  is passed" is false on the first dry-run against any phase whose changes.json
  has no cached `base_commit`, and the byte-identical assertions in t-5 would fail
  for a reason nobody planned for. Recorded here rather than silently merged
  because two of three lenses missed it, which makes it the likeliest thing an
  executor also misses.
