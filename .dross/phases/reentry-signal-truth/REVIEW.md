# Plan Review — reentry-signal-truth

Reviewed: 2026-08-22 (third pass, plan rewritten)
Plan: 5 tasks across 3 waves

## BLOCKING

- [locked-decision conflict: unreachable arm] t-4 places the merged hint in the
  terminal region — "Remove the `phase looks complete` branch … Then per
  reentry_terminal_hint: `dross phase complete <id>` only on t-2's merged answer"
  — but a merged phase almost never reaches that region. `suggestNext` returns
  earlier on two arms that fire first for exactly the phases the hint is for:
  `st.CurrentPhaseStatus == "shipped"` → "merge the open PR, then
  `dross phase complete`" (internal/cmd/status.go:285-287), and verdict=pass with
  `len(ch.Tasks) > 0` → `/dross-ship — open the PR and complete the phase`
  (status.go:293-296). Every real merged phase has recorded tasks — `dross
  execute` writes them per task — so the second arm claims it, and the re-entry
  line tells the user to ship a phase whose PR already landed. That is the same
  defect the shipped-status arm's own comment (status.go:279-284) was written to
  prevent, and it leaves reentry_terminal_hint implemented in a branch the case
  it names cannot enter.
  Suggestion: consult t-2's oracle *before* the verdict switch — merged →
  `dross phase complete <id>`; open → the existing "merge the open PR, then …"
  text; no-PR/unknown → fall through to today's logic. That also fixes the
  pre-existing wart where a phase whose status field was lost is told to re-ship
  an open PR. Then re-contract it (see the first FLAG).

## FLAG

- [test contract] Nothing in the plan fails if the merged arm is never built. Walk
  t-4's five contracts against an implementation that always falls back to
  `/dross-ship`: contracts 1 and 2 assert the unfinalized-verdict arm, 3 expects
  `/dross-ship`, 4 expects `/dross-ship`, 5 is byte-equality between two surfaces
  — all five pass. The previous revision had "if the merged-PR arm is dropped, …
  is sent to /dross-ship rather than `dross phase complete <id>`"; it was replaced
  by the negative form and the positive case went with it. Half of a locked
  decision is now uncontracted.
  Suggestion: restore the positive contract — recorded PR that is an ancestor of
  `origin/<base>`, no completion status → `dross phase complete <id>` — and keep
  the negative one alongside it.

- [test contract] t-4's contracts 3 and 4 collapse onto one fixture unless the
  plan says otherwise. The new terminal region is reachable only when the
  pass-verdict arm does not fire, i.e. the changes record carries zero tasks
  (status.go:293-296). Contract 4's fixture ("no PR recorded and HEAD on main")
  must therefore *also* carry a zero-task record — with recorded tasks it returns
  `/dross-ship` at line 294 and the assertion passes whether or not the merged arm
  exists. And once both fixtures are zero-task records, contract 4's only added
  variable is "no PR", which t-2's contract 2 already pins one level down.
  Suggestion: state that the terminal contracts share one fixture family varying
  only PR/merge state, and make contract 4's variant a *recorded but unmerged* PR
  (open → `/dross-ship`) — that is the distinction the surface can actually get
  wrong.

- [test contract] t-4's contract 2 is subsumed by contract 1. Same fixture (a
  phase with an unfinalized verdict), and contract 1's assertion — that the phase
  yields `dross verify finalize <id>` — already fails if the arm names
  `/dross-verify`. It earns its place only as a cross-surface assertion: status's
  `pending:` line (status.go:109-112) and the re-entry line must name one verb in
  a single status output.
  Suggestion: say that explicitly, or drop the contract.

- [oracle design] The three-valued split has more collapse points than contract 3
  pins, and they are the likely ones. Contracted: `origin/<base>` absent →
  unknown. Not contracted, and all currently funnelled into the same `false` that
  t-2 is decomposing (status.go:578-604): (a) the local `refs/heads/phase/<slug>`
  ref is gone — `merge-base` needs it, `dross phase complete` deletes it
  (internal/cmd/phase.go:725-732), and a fresh clone never had it; (b) a stale
  `origin/<base>` — network-free means "merged since the last fetch" is
  indistinguishable from "open", so `merged` can only ever mean "merged as of the
  last fetch"; (c) `rev-list --count <base> ^<branch>` exceeding
  `staleSquashScanLimit` (status.go:503, 40 commits) and every git error path,
  which on a busy base is the common route to "not merged".
  Suggestion: name (a) and (c) as `unknown` in the description, contract the
  scan-limit one, and put the fetch-freshness caveat in the oracle's doc comment
  — an oracle whose "open" quietly includes "merged, unfetched" must say so where
  the next caller reads it.

- [antipattern: two readers of one question] t-2 builds a second merge
  determination in the same file where t-3 edits the first. The ancestry +
  rev-list + `resolveSquashCommit` block inside `shippedUnmergedPhase`
  (status.go:578-604) is the same computation against the same recorded base that
  the oracle needs. Shipping both leaves two answers to "has this phase's work
  landed?" — precisely the argument t-1's own description makes about
  `phaseRecordVerdict`, applied to a bigger duplicate.
  Suggestion: have `shippedUnmergedPhase` delegate its merge check to the oracle
  (t-3 then gains `depends_on = ["t-2"]`), or record in the oracle's doc why the
  two deliberately stay separate.

- [files vs description] t-1's description instructs the executor to route
  `phaseRecordVerdict` through `Complete`, which is an edit to
  internal/cmd/issue_reap.go — not in t-1's `files`. The "or record in Complete's
  doc why the reap lane keeps its own copy" escape means the task can be
  completed without touching it, so the mismatch will resolve itself by the
  executor taking the cheaper branch.
  Suggestion: either add internal/cmd/issue_reap.go to `files` and commit to the
  swap, or drop the swap and keep only the doc note.

## NOTE

- [wave order] t-2 in wave 1 is correct: it consumes nothing from t-1, and the two
  touch disjoint packages (internal/changes + internal/watch vs internal/cmd), so
  the wave is genuinely parallel. Three of the five tasks edit
  internal/cmd/status.go, but `dross execute` runs one task at a time via
  `NextRunnable` — that is serialization, not collision, and only matters if a
  wave were ever fanned out to parallel agents.

- [feasibility] Keying the oracle on the phase id rather than HEAD is a parameter
  change, not a rewrite: `isAncestor` (internal/cmd/milestone_stale.go:260) and
  `resolveSquashCommit` (internal/cmd/milestone_stale.go:177) both take a branch
  name, so `phase/<slug>` substitutes directly for `shippedUnmergedPhase`'s `cur`.

- [answer to "can merged be told from unknown at all"] Yes, for the case contract
  3 pins, and yes for a phase whose branch ref still exists and whose base ref is
  present: `merge-base --is-ancestor` plus the patch-id squash check give a
  positive "merged" that no other outcome produces. What cannot be told apart
  locally is "open" from "merged but unfetched" — see the oracle-design FLAG.

- [coverage bookkeeping] t-2 and t-4 both declare `covers = ["c-4"]`, but t-2 is a
  helper with no user-visible output and none of its contracts assert anything
  about the re-entry line. Harmless as long as t-4 stays the criterion's owner —
  worth knowing so a coverage sweep doesn't read c-4 as doubly covered.

- [dependencies] t-5 omits t-2 from `depends_on`. Correct as written: it asserts
  on the four surfaces, and the re-entry surface reaches t-2 transitively through
  t-4; wave 3 sits after wave 1 regardless.

- [fix verified] The unfinalized-verdict arm's placement outside the
  `verdict != ""` guard and its `dross verify finalize <id>` verb both match the
  code: `readVerifyVerdict` returns "" for absent-or-garbled files, the switch is
  guarded on non-empty, and `pendingVerdicts` (status.go:626-643) uses exactly the
  `v == "" || v == "pending"` predicate the task now names.

- [fix verified] t-5's reconcilable-count-of-one caveat is right: `suggestNext`
  returns the reconcile suggestion at status.go:249-251 ahead of every
  doneness-sensitive arm, so a two-branch fixture would test the wrong line.
  Dropping the non-behavioral third contract was also right.

- [r-01] Unchanged: c-4's surface is the SessionStart hook, which runs the
  *installed* binary — `make install` before any hand-check of the re-entry line.

- [strength] Promoting the oracle to its own wave-1 task with unit contracts is
  the right shape: the previous revision hid a four-way distinction inside a hint
  rewrite, where only two of its values were observable.

- [strength] t-2's third contract — "no origin ref reports unknown, never merged"
  — is the one that matters most for a network-free reader, and it is stated as a
  mutation rather than as a property.

- [strength] t-4's description now carries its reasons inline (why `finalize` and
  not `/dross-verify`, why the arm sits outside the guard), so the executor can
  see what would break if it deviates instead of following an instruction.

## Summary

One blocker: t-4 puts the `dross phase complete` hint in a terminal region that a
merged phase with recorded tasks never reaches (status.go:293-296 claims it
first), so the locked hint would ship unreachable — and no contract in the plan
fails if the merged arm is never built at all.
