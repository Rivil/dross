# Synthesis — milestone-closeout-check

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| risk | 11/11, layered (unit + e2e for every gating kind) | Highest. Failure-mode contracts no other draft has: ambiguous acceptance, collector-error isolation, no id-minting, malformed-store errors, `--check --finalize`. Misses the fetch-line vocabulary and PR-body capture plumbing these contracts need. | 9 tasks. Defensible, but t-6 spans 4 packages to move helpers, and that move exists only because of the new `internal/closeout` package. t-7 (render) split from t-3 is fine-grained but OK. | Sound. The 4-wave depth comes from ack-after-wire (t-9 needs t-8's composer). Prompt task correctly in wave 1. |
| mvp | 11/11, but 9 criteria rest on one task (t-4) | Specific per criterion, but all concentrated in t-4. No negative cases for deferred filtering (dismissed, someday or out-of-milestone items). | Weakest. t-4 is a mega-task (4 collectors, ack partition, render, `--check`, gate fold, PR body). It passes the file-count rule but is far over the work-size intent. t-1 is a small single-file-pair task. | Valid and shallow (2 waves). Ack lands in wave 1 only because it validates by prefix alone (see D2). Misses `assets/partials.go` and the `msPRCapture.body` field its contracts need. |
| verification | 11/11; each gating criterion pinned at collector (unit) and command (fixture) level | Strong. Named tests and the strongest c-6 no-write contract (`.git/FETCH_HEAD`, `git for-each-ref`, unpushed commit stays unpushed). One internal inconsistency: t-5 refuses items "not currently unresolved", yet its re-ack contract acks an already-acknowledged item. | Good. The collector is separate from the git/mock-forge wiring, so per-kind contracts run as fast unit tests. Every task is ≤4 units. | Sound. t-6 after t-5 is justified (shared `milestone.go`, and the c-5 contract drives the real ack CLI). Only draft that adds the `closeout` trigger key to `TriggerVocabulary`, which the fetch-line lint at `assets/partials.go:317` requires. |

**Skeleton: verification.** It covers every criterion at two levels and keeps a sane task size. It is the only draft whose prompt task and PR-body contract compile against the real lint and test fixtures (`TriggerVocabulary` key, `msPRCapture` body). Risk supplies most of the grafts, mainly failure-mode contracts. Mvp supplies two contract lines.

## Merged plan

Phase milestone-closeout-check — 6 tasks across 4 waves

Wave 1
  t-1  Persist undisposed out-of-scope survivors in verify.toml   [verification+risk]
       files:    internal/verify/verify.go, internal/verify/verify_test.go, internal/verify/lifecycle.go,
                 assets/prompts/verify.md, internal/cmd/verify_prompt_test.go
       covers:   c-3
       desc:     Add `SurvivorRecord{Key, File, Line, Op, Ambiguous}` and `Verify.Survivors`
                 (`toml:"survivor,omitempty"`). Skeleton fills it from every `t.OutOfScope` row whose
                 lifecycle is unclassified. ApplyLifecycle stamps an Ambiguous flag onto OutOfScopeMutant
                 [risk, D7]. Keyless rows are recorded, not skipped [risk+mvp, D3]. verify.md §3 gains one
                 line telling the agent to leave `[[survivor]]` records exactly as written [risk, D10].
       depends_on: —
       contract: - TestSkeletonRecordsUndisposedSurvivors (OutOfScope = unclassified k1 f.go:12, accepted k2,
                   routed k3, unclassified keyless, plus one in-scope unclassified): fails unless
                   v.Survivors holds exactly k1 and the keyless row, each with its file/line/op. A leak
                   from accepted, routed or in-scope breaks it, and so does dropping the keyless row.
                   [verification, keyless arm per risk+mvp]
                 - An accepted-but-ambiguous OOS survivor is recorded with ambiguous=true. [risk]
                 - Two OOS rows at the same file/line/op produce one record (re-run idempotency). [risk]
                 - Save→LoadVerify round-trips Survivors field-for-field. A legacy verify.toml with no
                   `[[survivor]]` loads Survivors == nil and re-saves with no `survivor` table.
                   [verification+risk]
                 - verify_prompt_test fails if §3 loses the "leave [[survivor]] records as written" line.
                   TestPromptByteBudget keeps verify.md ≤ 15000 bytes (14612 today, 388 B headroom). [risk]

  t-2  Add closeout_ack records and item-id grammar to the milestone schema   [verification+risk]
       files:    internal/milestone/milestone.go, internal/milestone/milestone_test.go
       covers:   c-5, c-9
       desc:     Add `CloseoutAck{Item, Reason, At}` and `Milestone.Closeout` (`toml:"closeout_ack,omitempty"`).
                 `(*Milestone).Ack(item, reason, at)` upserts by item and refuses a blank or whitespace-only
                 reason. `ParseAckItem(id)` accepts only `phase:<slug>`, `deferred:<source>#<int>` and
                 `survivor:<key>`, and is used by both the collector and the ack command.
       depends_on: —
       contract: - Ack("phase:p", "   ", t) errors and leaves len(m.Closeout) unchanged. [verification]
                 - Ack(item,"r1",t1) then Ack(item,"r2",t2) leaves len==1 with Reason "r2", At t2. [verification]
                 - The ParseAckItem table rejects "bogus", "phase:", "deferred:src", "deferred:src#x",
                   "deferred:x#-1", "survivor:" and an unknown prefix. It accepts "deferred:_project#0", and
                   Parse(Format(x)) round-trips. [verification+risk]
                 - TestRoundTrip: Save→Load keeps item/reason/at alongside base/phases. A toml with no acks
                   loads Closeout == nil and re-saves with no `closeout_ack` key (c-8 byte compatibility).
                   [verification+risk]

  t-3  Close-out check in the /dross-milestone completion arm   [verification]
       files:    assets/prompts/milestone.md, assets/prompts/_milestone.closeout.md (new), assets/partials.go,
                 internal/cmd/milestone_dispatch_prompt_test.go
       covers:   c-7
       desc:     Step 1 of §8 runs `dross milestone complete <version> --check` before the close-out
                 AskUserQuestion. A `**When** \`closeout=unresolved\`` fetch line pulls the `closeout` partial,
                 which walks one item per turn (resolve it now, or acknowledge with a reason via
                 `dross milestone ack`) and re-runs --check. The PR opens only once --check exits 0. Add
                 `"closeout": {"clean","unresolved"}` to TriggerVocabulary.
       depends_on: —
       contract: - TestMilestonePromptCompletionArmChecksFirst: in the "## 8. Completion arm" section, the
                   `--check` command comes before the close-out AskUserQuestion and before the bare
                   `dross milestone complete <version>` open command. It also requires the closeout=unresolved
                   fetch line. [verification+risk+mvp]
                 - TestMilestonePromptCloseoutPartialWalksOnePerTurn (expanded partial) requires all of:
                   "one per turn", AskUserQuestion, a resolve option, an acknowledge option,
                   `dross milestone ack`, `--reason`, and "re-run --check" until exit 0 before the PR opens.
                   It fails if the PR-open step is reachable without a clean --check. [verification+risk]
                 - TestPromptByteBudget keeps milestone.md ≤ 15000 bytes (14687 today). The partial-lint
                   tests pass for the new key and file, and they would fail without the TriggerVocabulary
                   entry. [verification+mvp]

Wave 2 (depends t-1, t-2)
  t-4  Build the close-out enumeration collector   [verification+risk]
       files:    internal/cmd/milestone_closeout.go (new), internal/cmd/milestone_closeout_test.go (new)
       covers:   c-1, c-2, c-3, c-4, c-8, c-11
       desc:     `milestoneCloseout(root, version)` returns four kinds:
                 1. unverified (reuses milestoneUnverifiedPhases; no ack id, D1)
                 2. not-done (phase.Done over the phases array, deduped)
                 3. unabsorbed deferred items (read-only `deferred.Collect`; routed-survivor entries excluded, D4)
                 4. undisposed survivors (verify.toml records re-classified against acceptedReasons and
                    routedSurvivors, honouring Ambiguous)
                 Each item carries its ack id and clearing command. A collector error becomes a blocking,
                 non-ackable line under its own kind instead of aborting (D8). Acks split into applied and
                 stale. Blocking() excludes acked items. Render uses a fixed kind order, sorts within each
                 kind and puts stale acks under their own heading.
       depends_on: t-1, t-2
       contract: - c-1: phases [done, shipped, built (dir, no status), ghost (no dir), built-again (listed twice)]
                   gives NotDone == [built, ghost] in roadmap order, built once. done and shipped are
                   absent. A malformed changes.json counts as not done. [verification+risk]
                 - c-2: item src#1 (id X, target p1) is listed with source, index and target. Adding X to a p1
                   criterion's `deferred` removes it. X absorbed only by a DIFFERENT phase's criterion is
                   still listed. These are never listed: dismissed, someday (no target), targeted outside the
                   milestone, routed-survivor entries. `_project` routed to p1 is listed as
                   `deferred:_project#0`. A target with no spec.toml (unscaffolded) lists its items. A
                   pre-identity item is listed and the source spec is byte-identical afterwards (no
                   EnsureIDs mint). [verification+risk]
                 - c-3: p1 `[[survivor]]` K f.go:12 is listed naming "f.go:12" and "p1". A survivors.toml
                   acceptance clears it, unless the record is ambiguous=true, which only a route clears. A
                   deferred entry with survivor=K and a target clears it; a dismissed or target-less entry
                   does not. The same K recorded by p1 and p2 is ONE `survivor:K` item naming both phases. A
                   keyless record is listed as `survivor:<file>:<line>:<op>`. A phase with no records
                   contributes nothing. [verification+risk]
                 - c-4: one item of each kind renders four headings in fixed order, each line carrying its
                   clearing command (/dross-verify or --force-unverified; `dross phase complete` or
                   `dross milestone ack phase:<slug>`; `dross deferred absorb <src> <idx> --phase <t>
                   --criterion`; `dross survivor accept|route`). A collector stub that errors, alongside one
                   that returns items, yields both, so the test fails on an early return.
                   [verification+risk]
                 - c-4: a malformed source spec.toml or survivors.toml yields a blocking error line, never
                   an empty kind. [risk]
                 - c-11: an ack for an item no longer produced lands in Stale, not Applied, and Blocking()
                   ignores it. An ack `phase:x` where x is only unverified does not clear the unverified
                   item. [verification+risk]
                 - c-8: no milestone toml, and phases = [], both give Blocking()==0 with no error. Two
                   consecutive renders of the same tree are byte-identical and contain no wall-clock
                   value. [verification+risk]

Wave 3 (depends t-2, t-4)
  t-5  Add the `dross milestone ack` command   [verification+risk+mvp]
       files:    internal/cmd/milestone_ack.go (new), internal/cmd/milestone_ack_test.go (new),
                 internal/cmd/milestone.go, README.md
       covers:   c-5, c-9
       desc:     `milestone ack <item> [version] --reason "..."` (default: state.current_milestone)
                 validates with ParseAckItem and requires the item to be currently unresolved OR already
                 acknowledged (D2; this resolves verification's internal inconsistency in risk's favour),
                 then upserts and saves. Register it in Milestone() and add `ack` to the README
                 milestone row (D11).
       depends_on: t-2, t-4
       contract: - `ack phase:built --reason "moved to v2.0"` writes exactly one `[[closeout_ack]]` with that
                   item, reason and a non-zero at. [verification]
                 - `--reason ""` and `--reason "  "` exit non-zero and the toml sha256 is unchanged.
                   [verification+risk+mvp]
                 - Re-acking an acknowledged item with a new reason leaves one record carrying the latest
                   reason. A re-ack is allowed, not "unknown item". [risk+verification]
                 - Refused with toml unchanged: `bogus` (error lists the three forms), `phase:done`,
                   `phase:nope` (not in milestone). `phase:<slug>` for a done-but-unverified phase is
                   refused, pointing at /dross-verify or --force-unverified. [verification+risk]
                 - No milestone toml → refused ("nothing to acknowledge") and no file created. [risk]
                 - `dross milestone add <v> phases p-z` after an ack keeps the `[[closeout_ack]]` record,
                   so the struct-based writers do not drop it. [mvp+risk]

Wave 4 (depends t-4, t-5)
  t-6  Wire close-out into `milestone complete` (+ --check)   [verification+risk+mvp]
       files:    internal/cmd/milestone.go, internal/cmd/milestone_verify_gate.go,
                 internal/cmd/milestone_verify_gate_test.go, internal/cmd/milestone_closeout_cmd_test.go (new),
                 internal/cmd/milestone_test.go (msPRCapture gains body)
       covers:   c-1, c-2, c-3, c-4, c-5, c-6, c-8, c-9, c-10, c-11
       desc:     Open mode refuses with the full report when Blocking() > 0. --force-unverified drops only
                 the unverified kind and records nothing. The PR body appends a "Close-out acknowledgements"
                 section (item, reason, current detail text) for applied acks only. `--check` runs right
                 after the --finalize branch, before the [remote] check, milestonePRBase (git fetch) and
                 routeMilestoneHead. It honours --force-unverified and is refused alongside --finalize (D9).
                 --finalize never calls the collector.
       depends_on: t-4, t-5
       contract: - All four kinds seeded: `complete` returns one error with all four headings, and
                   cap.created==0. [verification+risk]
                 - --force-unverified on the same seed still refuses, without the unverified heading. With
                   only unverified seeded it opens the PR and the body has no ack line. A not-done phase
                   still refuses under the flag naming `phase:<slug>`. [verification+risk+mvp]
                 - c-5: after `milestone ack phase:built --reason "moved to v2.0"`, complete opens
                   (created==1) and cap.body has both strings. [verification]
                 - c-6: --check exits non-zero and its item lines equal the refusal's. It lists the acked
                   item and reason, and cap.posts==0. These are identical before and after: a sha256 of
                   every file under .dross, `git for-each-ref`, origin's milestone ref. No .git/FETCH_HEAD
                   appears, and an unpushed local commit stays unpushed. --check also works with no [remote]
                   configured. [verification+risk]
                 - `--check --finalize` is refused as conflicting flags. [risk]
                 - c-8: no record or empty phases makes --check exit 0 printing "nothing unresolved", and
                   the PR body is byte-equal to today's. [verification+risk+mvp]
                 - c-9: --check run twice after an ack gives byte-identical stdout. [verification+mvp+risk]
                 - c-10: finalizeFixture plus a not-done phase and an unabsorbed deferred item: --finalize
                   succeeds, status is complete, and the output has no close-out heading. [verification+mvp+risk]
                 - c-11: ack phase:p, then SetStatus(p, complete). --check exits 0 with p under the stale
                   heading, and the PR body lacks "phase:p". [verification+mvp]
                 - Existing TestMilestoneCompleteOpensWhenEveryPhasePasses and ForceUnverifiedOverrides seed
                   changes.json complete. Without it they would trip the not-done kind and fail.
                   [verification+risk+mvp]

## Coverage
- c-1 → t-4, t-6
- c-2 → t-4, t-6
- c-3 → t-1, t-4, t-6
- c-4 → t-4, t-6
- c-5 → t-2, t-5, t-6
- c-6 → t-6
- c-7 → t-3
- c-8 → t-2, t-4, t-6
- c-9 → t-2, t-5, t-6
- c-10 → t-6
- c-11 → t-4, t-6

All 11 criteria covered. Locked decisions are honoured: survivor_source (t-1, t-4), ack_storage (t-2, t-5) and force_unverified (t-6).

## Disagreements

**D1 — What a `phase:<slug>` ack clears (unverified + not-done vs. not-done only).**
- risk and verification: a `phase:<slug>` ack clears only the not-done kind. Unverified carries no ack id and is cleared only by /dross-verify or `--force-unverified`.
- mvp: one `phase:<slug>` ack clears both kinds. Its reasons are that the locked id scheme has no separate unverified namespace, and c-5 says "*any* unresolved item can be acknowledged".
- **Default: not-done only** (risk+verification). The locked force_unverified decision says "every other kind needs an ack", which reads as unverified having its own clearing path.
- **Why it matters:** a literal reading of c-5 favours mvp. Under the default, the unverified kind is the one unresolved kind an ack cannot clear. Plan review should confirm that reading of c-5, because an ack that also cleared unverified would let a reason string skip verification with nothing recorded beyond the ack.

**D2 — Whether `milestone ack` refuses items that aren't currently unresolved.**
- risk: refuses unless the item is currently unresolved OR already acknowledged.
- verification: refuses unless currently unresolved, yet its own re-ack contract acks an already-acknowledged item, which is internally inconsistent.
- mvp: validates the prefix only. A typo surfaces later as a stale ack (c-11).
- **Default: risk's rule.** It is the only one that satisfies c-9's re-ack while still catching typos.
- **Why it matters:** this choice decides the graph shape. Membership validation needs the collector, which forces ack into wave 3. Under mvp's rule ack could move to wave 1 and the plan would collapse to 3 waves. With prefix-only validation, a mistyped id leaves the real item blocking and makes c-11's stale heading report typos rather than drift.

**D3 — Keyless (identity-unresolved) and unset-lifecycle survivors: record or skip.**
- risk and mvp: record keyless unclassified rows and give them the fallback id `survivor:<file>:<line>:<op>`. risk also records rows whose lifecycle was never set.
- verification: skip keyless rows, because they cannot be addressed as `survivor:<key>` and would produce items no ack can clear.
- **Default: record keyless rows** (2-1). Unset-lifecycle rows are recorded too, per risk; this is a risk-only position.
- **Why it matters:** skipping means a survivor whose identity fails silently drops out of the gate, which is the exact failure c-3 exists to catch. Recording means the fallback id stretches the locked `survivor:<key>` grammar (the "key" becomes file:line:op). Unset-lifecycle rows may conflict with survivor_source's rule that "phases verified before this ships ... count as none" if a legacy-shaped run can still produce them.

**D4 — Whether routed-survivor deferred entries count in the deferred kind (c-2).**
- verification: excludes them explicitly. c-3 already treats routing as the survivor's disposition, so also requiring an absorb record would double-count.
- risk and mvp: filter only on dismissed, target-in-milestone and not-absorbed. As written, a survivor routed to a milestone phase would also block as an unabsorbed deferred item.
- **Default: exclude** (verification). The other drafts didn't argue against it; they never considered the case.
- **Why it matters:** without the exclusion, every survivor routed into the milestone needs both a route and an absorb, or an ack, before close-out. That doubles the friction on exactly the items the survivor drain parks.

**D5 — Where the close-out logic lives and how finely it is split.**
- risk: a new `internal/closeout` package in 4 tasks (core with phase kinds, deferred collector, survivor collector, render with ack join). It moves `acceptedReasons` and `routedSurvivors` out of `internal/cmd/verify.go` into `internal/survivor` and `internal/deferred` so both callers share them.
- verification: one collector file pair in `internal/cmd`, with wiring as a separate task.
- mvp: collector, render, `--check` and wiring all in one task.
- **Default: verification's structure.** The helpers are already in package `cmd`, so no move is needed and nothing is duplicated.
- **Why it matters:** risk's split gives wave-2 parallelism (deferred and survivor collectors side by side) and a reusable package, at the cost of 3 more tasks and a cross-package refactor. The default puts six criteria' worth of unit contracts on one task (t-4), the largest in the plan. If plan review judges t-4 too big, risk's 4-way split is the graft to take.

**D6 — Ordering of the ack command vs. the complete wiring.**
- risk: wire `complete` first (t-8), then ack (t-9), because ack reuses the wiring's composer.
- verification: ack (t-5) first, then wire (t-6), because the c-5 end-to-end test drives the real ack CLI.
- mvp: ack in wave 1, before everything.
- **Default: verification's order.** In this plan the composer lives in t-4, so ack needs only t-4. The wiring then exercises the real CLI instead of a hand-written toml.
- **Why it matters:** both orders serialise on `milestone.go`. The difference is whether the c-5 PR-body test proves the actual write path or a fixture of it.

**D7 — An `ambiguous` flag on survivor records.**
- risk: persists `ambiguous` and stamps it in lifecycle.go. Close-out then refuses to let an acceptance clear an ambiguous survivor, mirroring TestAmbiguousAcceptanceDoesNotSuppress.
- mvp and verification: record only key/file/line/op, matching the locked survivor_source wording.
- **Default: adopt** (risk).
- **Why it matters:** without the flag, close-out re-classification is laxer than verify. An acceptance would clear at close-out a survivor that verify itself refused to suppress. The flag is additive to the locked record shape but not named in it, so plan review should confirm it reads as compatible.

**D8 — A failing collector: blocking line vs. abort.**
- risk: a collector error (malformed spec or survivors.toml) becomes a blocking, non-ackable line under its kind, and the other kinds still run.
- mvp and verification: silent on errors. Their "no early return" covers non-empty kinds only.
- **Default: blocking line** (risk).
- **Why it matters:** c-4 says the check "never stops at the first failing kind". An abort hides every other kind. Swallowing the error to an empty list silently passes the gate.

**D9 — Flag semantics of `--check` (risk-only positions).**
- risk: `--check` honours `--force-unverified`, and `--check --finalize` is refused as conflicting.
- mvp and verification: silent on both.
- **Default: adopt both.**
- **Why it matters:** if `--check` ignored `--force-unverified`, a user who chose to force could never reach the clean `--check` that c-7's prompt walk requires. Allowing `--check --finalize` leaves it ambiguous which mode runs, given c-10.

**D10 — Protecting `[[survivor]]` records in the verify prompt.**
- risk: adds a verify.md §3 line telling the agent to leave the records as written, pinned by verify_prompt_test, within 388 B of budget headroom.
- mvp and verification: no prompt change.
- **Default: adopt.** The agent rewrites verify.toml in §3, and a dropped table erases the gate's input with nothing to show it happened.
- **Why it matters:** this costs prompt budget on a file with 388 B headroom, and it widens t-1 into the prompt layer (2 layers, still within the rules).

**D11 — README row for `milestone ack`.**
- verification: adds `ack` to the README milestone subcommand row inside t-5.
- mvp: rejects README work as a ship-time doc concern that no criterion requires.
- **Default: include, inside t-5.** It is cheap, and the project syncs the README with each change. TestReadmeAdvertisesOnlyRealCommands checks only top-level commands, so leaving it out wouldn't fail anything.
- **Why it matters:** low stakes. Without it, a new subcommand ships undocumented on the primary reference surface.
