# Planner draft: verification lens

Lens: each criterion's ideal test contract comes first. Each task is the smallest change that makes those contracts satisfiable.

Phase milestone-closeout-check: 6 tasks across 4 waves

Wave 1
  t-1  Persist undisposed survivors in verify.toml
       files:    internal/verify/verify.go, internal/verify/verify_test.go
       covers:   c-3
       desc:     Add `SurvivorRecord{Key, File, Line, Op}` and `Verify.Survivors` (`toml:"survivor,omitempty"`, so `[[survivor]]`).
                 Skeleton fills it from `t.OutOfScope` entries whose Lifecycle is `unclassified` AND whose Key is non-empty, and nothing else.
       contract: TestSkeletonRecordsUndisposedSurvivors feeds OutOfScope = [unclassified k1 f.go:12, accepted k2,
                 routed k3, unclassified with Key ""] plus one in-scope unclassified survivor. It fails unless
                 v.Survivors == exactly [{k1, f.go, 12, op}].
                 An accepted, routed, keyless or in-scope survivor leaking into the records breaks it.
                 Save→LoadVerify round-trips Survivors field-for-field. A verify.toml with no [[survivor]]
                 table loads Survivors == nil, so a phase verified before this ships counts as none.

  t-2  Add closeout_ack records to milestone schema
       files:    internal/milestone/milestone.go, internal/milestone/milestone_test.go
       covers:   c-5, c-9
       desc:     Add `CloseoutAck{Item, Reason, At}` and `Milestone.Closeout` (`toml:"closeout_ack,omitempty"`).
                 Add `(*Milestone).Ack(item, reason, at)`, which upserts by item and refuses a blank or whitespace-only reason.
                 Add `ParseAckItem(id)`, which accepts only `phase:<slug>`, `deferred:<source>#<int>` and `survivor:<key>`.
       contract: Ack("phase:p", "   ", t) returns an error and len(m.Closeout) stays unchanged.
                 Ack(item,"r1",t1) followed by Ack(item,"r2",t2) leaves len==1 with Reason "r2" and At t2.
                 The ParseAckItem table rejects "bogus", "phase:", "deferred:src#x", "deferred:src" and "survivor:".
                 It accepts "deferred:_project#0".
                 TestRoundTrip extended: Save→Load keeps [[closeout_ack]] item/reason/at alongside base/phases.
                 A toml without the table loads Closeout == nil.

  t-3  Close-out check in /dross-milestone completion arm
       files:    assets/prompts/milestone.md, assets/prompts/_milestone.closeout.md (new), assets/partials.go,
                 internal/cmd/milestone_dispatch_prompt_test.go
       covers:   c-7
       desc:     Step 1 of §8 runs `dross milestone complete <version> --check` before the close-out AskUserQuestion.
                 A `**When** \`closeout=unresolved\`` fetch line pulls partial `closeout`, which walks the unresolved items one per turn.
                 Each turn offers resolve-now or acknowledge-with-reason via `dross milestone ack`, then re-runs --check.
                 The PR opens only when --check exits 0.
                 Add "closeout": {"clean","unresolved"} to TriggerVocabulary.
       contract: TestMilestonePromptCompletionArmChecksFirst fails unless, in the "## 8. Completion arm" section,
                 the `--check` command comes before the "close it out" confirm and before the bare open command.
                 The arm must carry the closeout=unresolved fetch line.
                 TestMilestonePromptCloseoutPartialWalksOnePerTurn (expanded partial) requires all of these:
                 "one per turn", AskUserQuestion, a resolve option, an acknowledge option,
                 `dross milestone ack`, `--reason`, and re-running --check until it exits 0 before the PR opens.
                 TestPromptByteBudget must stay green: milestone.md is 14687 of 15000 bytes today, so the arm text moves to the partial.
                 The existing partial-lint tests must pass for the new key and file.

Wave 2 (depends t-1, t-2)
  t-4  Build close-out enumeration collector
       files:    internal/cmd/milestone_closeout.go (new), internal/cmd/milestone_closeout_test.go (new)
       covers:   c-1, c-2, c-3, c-4, c-8, c-11
       desc:     `milestoneCloseout(root, version)` returns four kinds: unverified (reuses milestoneUnverifiedPhases),
                 not-done (phase.Done over the phases array), unabsorbed deferred items and undisposed survivors.
                 Each item carries an ack id and the command that clears it.
                 Acks are partitioned into applied and stale; Blocking() excludes acked items.
                 Render prints groups in a fixed order, sorted within each group, with stale acks under their own heading.
       contract: c-1: phases [done (changes status complete), shipped, built (dir, no status), ghost (no dir)] gives
                 NotDone == [built, ghost] in roadmap order. done and shipped must be absent.
                 c-2: item src#1 (id X, target p1 in milestone) is listed with source, index and target. Adding X to a p1
                 criterion's `deferred` removes it. These are never listed: a dismissed item, an item targeted outside the milestone,
                 an unrouted item, and a routed survivor entry. A _project store item routed to p1 is listed as deferred:_project#0.
                 c-3: p1 verify.toml [[survivor]] K f.go:12 is listed naming "f.go:12" and "p1".
                 survivors.toml accepting K removes it. A deferred entry with survivor=K and a target removes it.
                 p2 with no [[survivor]] table contributes nothing.
                 c-4: one item of each kind renders four headings in fixed order. Each line carries its clearing command:
                 /dross-verify or --force-unverified; `dross phase complete <slug>` or `dross milestone ack phase:<slug>`;
                 `dross deferred absorb <src> <idx> --phase <target> --criterion`; `dross survivor accept|route <file>:<line>`.
                 A non-empty unverified kind still lists the other three, so there is no early return.
                 c-11: an ack for an item no longer unresolved lands in Stale, not Applied, and Blocking() ignores it.
                 c-8: no milestone toml, and phases = [], both give Blocking()==0 with no error.
                 Two consecutive renders are byte-identical.

Wave 3 (depends t-2, t-4)
  t-5  Add `dross milestone ack` command
       files:    internal/cmd/milestone_ack.go (new), internal/cmd/milestone_ack_test.go (new), internal/cmd/milestone.go,
                 README.md
       covers:   c-5, c-9
       desc:     `milestone ack <item> [version] --reason "..."` validates the id with ParseAckItem.
                 It refuses an item that milestoneCloseout does not currently report as unresolved.
                 A clean ack upserts the [[closeout_ack]] record.
                 Register the command in Milestone() and add `ack` to the README milestone subcommand row.
       contract: `ack phase:built --reason "moved to v2.0"` writes exactly one [[closeout_ack]] with that item, reason and a non-zero at.
                 `--reason ""` and `--reason "  "` exit non-zero, and the milestone toml sha256 is unchanged.
                 Re-acking with a new reason leaves one `[[closeout_ack]]` occurrence carrying the latest reason.
                 These are refused, naming the item and leaving the toml unchanged: `bogus` (the error lists the three forms),
                 `phase:done` (already done) and `phase:nope` (not in the milestone).
                 `phase:<slug>` for a done phase that is only unverified is refused, pointing at /dross-verify or --force-unverified.

Wave 4 (depends t-4, t-5)
  t-6  Wire close-out into milestone complete
       files:    internal/cmd/milestone.go, internal/cmd/milestone_verify_gate.go, internal/cmd/milestone_verify_gate_test.go,
                 internal/cmd/milestone_closeout_cmd_test.go (new), internal/cmd/milestone_test.go (msPRCapture gains body)
       covers:   c-1, c-2, c-3, c-4, c-5, c-6, c-8, c-9, c-10, c-11
       desc:     Open mode refuses with the full rendered report when Blocking() > 0.
                 --force-unverified drops only the unverified kind and records nothing.
                 The PR body appends "Close-out acknowledgements" (item + reason) for applied acks only.
                 New `--check` flag: it runs right after the --finalize branch, before the remote check, milestonePRBase (git fetch) and routeMilestoneHead (push).
                 It prints the report plus applied and stale acks and returns an error iff Blocking() > 0.
                 --finalize never calls the collector.
       contract: Seed all four kinds. `complete v0.9` returns one error with all four headings and cap.created==0.
                 --force-unverified on the same seed still refuses, and the unverified heading is gone.
                 With only unverified seeded, --force-unverified opens the PR and the body has no ack line.
                 c-5: after `milestone ack phase:built --reason "moved to v2.0"`, complete opens (created==1) and cap.body has both strings.
                 c-6: --check against the same seed exits non-zero. Its item lines equal the refusal's item lines, and it lists the acked item and reason.
                 cap.posts==0. These are identical before and after: a sha256 of every file under .dross, `git for-each-ref`, and the origin/milestone ref.
                 No .git/FETCH_HEAD appears, and an unpushed local commit stays unpushed.
                 c-8: no record or empty phases makes --check exit 0 printing "nothing unresolved", and the PR body equals today's text.
                 c-9: --check run twice after an ack gives byte-identical stdout.
                 c-10: finalizeFixture plus a not-done phase and an unabsorbed deferred item: --finalize succeeds, status
                 is complete, and the output has no close-out heading.
                 c-11: ack phase:p, then changes.SetStatus(p, complete). --check exits 0 with p under the stale heading,
                 and the PR body lacks "phase:p".
                 Existing pass/force tests seed changes.json complete; without it they would wrongly trip the not-done kind.

## Coverage

- c-1 → t-4 (collector), t-6 (refusal in complete)
- c-2 → t-4, t-6
- c-3 → t-1 (verify.toml records), t-4 (re-classification), t-6
- c-4 → t-4 (grouped render, no early return), t-6 (single refusal end to end)
- c-5 → t-2 (blank-reason refusal in schema), t-5 (ack CLI), t-6 (ack unblocks, PR body)
- c-6 → t-6
- c-7 → t-3
- c-8 → t-4, t-6
- c-9 → t-2 (upsert), t-5 (CLI re-ack), t-6 (--check stable after ack)
- c-10 → t-6
- c-11 → t-4 (stale partition), t-6 (body exclusion, stale heading in --check)

All 11 criteria covered.

## Judgment calls

- **Unverified kind: not ackable.** It is cleared by verify or by --force-unverified (locked force_unverified, and the locked id namespaces have no unverified form). `phase:<slug>` acks only the not-done kind. I rejected letting a phase ack also clear unverified, because that would make one ack id span two kinds and weaken the verify gate.
- **Ack scope: only currently unresolved items.** Ack refuses an item that is not currently unresolved, which catches typos and stale ids at write time. I rejected accept-anything plus stale-later, because c-11's stale heading should report drift, not typos.
- **Routed survivors are excluded from the deferred kind (c-2).** c-3 defines routing as a survivor's disposition, so requiring an absorb record for them would double-count. I rejected sharing boardsync.Disposed, which additionally requires the target to be complete.
- **The deferred absorb check lives in the collector.** It is about 10 lines duplicated from boardsync.absorbedDisposed, minus the completion requirement. I rejected exporting a shared helper from internal/deferred, because the semantics differ: c-2 needs only the record.
- **Survivor records skip keyless unclassified survivors.** They cannot be addressed as `survivor:<key>` and still FLAG in verify. Recording them would create items no ack can clear.
- **One `survivor:<key>` item per key, naming every milestone phase that recorded it.** I rejected one item per (phase, key), because the locked id format has no phase component.
- **--check runs before milestonePRBase and before the remote/provider check.** milestonePRBase does `git fetch` (it writes .git), and the no-write guarantee would be false otherwise. The no-write contract is measured over .dross and the git refs.
- **--check and the refusal share one task (t-6).** That is the only place the "same enumeration" equality contract can be written. The collector (t-4) is separate so its per-kind contracts run as fast unit tests without the git and mock-forge fixture.
- **t-5 and t-6 run sequentially.** Both touch milestone.go, and t-6's ack-in-body contract drives the real `milestone ack` CLI rather than hand-writing toml.
- **The prompt task is in wave 1.** Its contracts are prompt-text only and the CLI surface is fixed by the locked decisions. The arm detail goes into a new partial because milestone.md has 313 bytes of budget headroom.
