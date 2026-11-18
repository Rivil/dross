# mvp — milestone-closeout-check

Lens: smallest task set that satisfies every criterion. Every task traces to a criterion; nothing speculative.

Phase milestone-closeout-check — 4 tasks across 2 waves

Wave 1
  t-1  Persist undisposed out-of-scope survivor records
       files:    internal/verify/verify.go, internal/verify/verify_test.go
       covers:   c-3
       desc:     Add `Survivors []SurvivorRecord` (`toml:"survivor,omitempty"`: key, file, line, op) to `verify.Verify`.
                 `Skeleton` appends one record for each out-of-scope mutant whose Lifecycle is `unclassified`, in the same
                 arm that emits the FLAG today. If identity resolution failed and the key is empty, the record is still
                 written and keeps file/line/op, so the survivor cannot drop out of the gate by being unreadable.
       contract: - Skeleton over a Tests holding three out-of-scope mutants (one unclassified, one accepted, one routed)
                   writes exactly one [[survivor]] record carrying the unclassified mutant's key, file, line and op.
                   If the accepted or routed mutant also gets a record, or the unclassified one gets none, the test fails.
                 - Save then LoadVerify round-trips the record unchanged.
                 - A verify.toml with no [[survivor]] tables (the legacy shape) loads with len(Survivors)==0 and no error.

  t-2  Add closeout_ack schema and `milestone ack`
       files:    internal/milestone/milestone.go, internal/cmd/milestone_ack.go (new),
                 internal/cmd/milestone_ack_test.go (new), internal/cmd/milestone.go
       covers:   c-5, c-9
       desc:     Add `CloseoutAcks []CloseoutAck` (`toml:"closeout_ack,omitempty"`: item, reason, at) to `milestone.Milestone`.
                 New `dross milestone ack <item> --reason "..." [--milestone <v>]` (default: state.current_milestone)
                 upserts by item, so a re-ack replaces the reason and `at`. It refuses a reason that is empty or only
                 whitespace, an item without a `phase:`, `deferred:` or `survivor:` prefix, and a version with no
                 milestone record. Register the command in `Milestone()`.
       contract: - `ack phase:p-x --reason ""` and `--reason "   "` both return an error, and the milestone toml stays byte-identical.
                 - Acking `deferred:p-a#0` twice (reasons "one", then "two") leaves exactly one [[closeout_ack]] for that
                   item, and its reason is "two". If it appends instead, the count assertion fails.
                 - `ack foo:bar` is refused, naming the three valid prefixes.
                 - `dross milestone add <v> phases p-z` after an ack keeps the [[closeout_ack]] record, so the Save
                   round-trip does not drop the new field.

  t-3  Completion arm runs --check and walks items
       files:    assets/prompts/milestone.md, assets/prompts/_milestone.closeout.md (new),
                 internal/cmd/milestone_dispatch_prompt_test.go
       covers:   c-7
       desc:     In §8, step 1 runs `dross milestone complete <version> --check` before the close-out AskUserQuestion.
                 When --check is not clean, a `**When**` line fetches `dross prompt partial milestone closeout`. The partial
                 walks one unresolved item per turn (resolve it now / acknowledge with a reason via `dross milestone ack`),
                 re-runs --check after each one, and allows step 2 only once --check exits 0. The fetch line keeps
                 milestone.md under the 15000-byte cap, which today leaves about 300 bytes of headroom.
       contract: - In section(text, "## 8. Completion arm"), the index of `dross milestone complete <version> --check`
                   is lower than the index of the close-out `AskUserQuestion`. Moving the check after the confirm fails the test.
                 - The partial contains "resolve it now", "acknowledge", `dross milestone ack` and "one" per turn, and states
                   that the integration PR opens only once `--check` is clean.
                 - assets TestPromptByteBudget stays green with milestone.md at or under 15000 bytes, and the partials test
                   resolves the new `**When**` fetch line to `_milestone.closeout.md`.

Wave 2 (depends t-1, t-2)
  t-4  Close-out check: enumerate, refuse, --check, PR acks
       files:    internal/cmd/milestone_closeout.go (new), internal/cmd/milestone_closeout_test.go (new),
                 internal/cmd/milestone.go, internal/cmd/milestone_verify_gate.go,
                 internal/cmd/milestone_verify_gate_test.go
       covers:   c-1, c-2, c-3, c-4, c-5, c-6, c-8, c-10, c-11
       desc:     `milestoneCloseout(root, version, forceUnverified)` collects four kinds:
                 1. unverified phases (existing `milestoneUnverifiedPhases`, skipped under --force-unverified with no record kept);
                 2. not-done phases (`phase.Done`);
                 3. unabsorbed deferred items (from `deferred.Collect`: not dismissed, Target in m.Phases, ID absent from
                    every criterion's `Deferred` in the target's spec);
                 4. undisposed survivors (each phase's verify.toml [[survivor]] records re-classified against
                    `acceptedReasons` and `routedSurvivors`).
                 It splits them into blocking, acknowledged and stale-acks, and renders the blocking set grouped by kind,
                 each line naming its clearing command plus `dross milestone ack <item>`. `milestone complete` gains
                 `--check`, which runs right after the --finalize branch and before any remote, push or PR step, prints the
                 groups plus acknowledged and stale acks, and returns an error iff anything blocks. The open path swaps
                 `milestoneVerifyGate` for this refusal and appends live acks (item — reason) to the PR body.
                 `milestoneVerifyGate` is folded in. Existing gate-test fixtures gain a complete changes.json, so they
                 still mean "everything resolved".
       contract: - c-1: phases [p-done, p-notdone, p-unscaffolded], all verified pass: complete errors naming p-notdone and
                   p-unscaffolded but not p-done, and cap.created==0.
                 - c-2: a deferred item in p-a's spec, routed to milestone phase p-b, whose ID is not in any p-b criterion's
                   `deferred` list: the refusal names "p-a", index 0 and "p-b". After `dross deferred absorb p-a 0 --phase
                   p-b --criterion c-1`, that line is gone.
                 - c-3: a p-b verify.toml [[survivor]] {key K, file x.go, line 12} with K in neither store: the refusal
                   names "x.go:12" and "p-b". Adding K to survivors.toml accepted, or to a deferred entry
                   (Survivor=K, Target set), clears it. A phase with no [[survivor]] tables contributes no lines.
                 - c-4: a fixture with one item of each kind yields ONE error containing all four group headings and, per
                   line, `/dross-verify`, `dross phase complete`, `dross deferred absorb` and `dross survivor accept`.
                   Bailing at the first kind loses three headings and fails the test.
                 - c-5: acking p-notdone with reason "R" and leaving nothing else unresolved: complete opens (created==1),
                   and the captured PR body contains "phase:p-notdone" and "R".
                 - c-6: --check with unresolved items returns non-zero with cap.created==0, the origin milestone ref is
                   unchanged, and a sha256 of every file under .dross/ is identical before and after. The output includes
                   an acknowledged section when an ack exists.
                 - c-8: with no milestone toml, and again with `phases = []`, --check returns nil printing "nothing
                   unresolved", and complete opens the PR with the pre-phase body (no acknowledgements section).
                 - c-9: --check run twice after an ack produces byte-identical output.
                 - c-10: finalizeFixture with a not-done phase and an unabsorbed deferred item in m.Phases: --finalize
                   succeeds and records status=complete.
                 - c-11: an ack for phase:p-x where p-x is now done: it is absent from the PR body, and --check lists it
                   under a stale-acks heading and exits 0.
                 - force_unverified: with --force-unverified, an unverified-but-done phase opens the PR and its id is not in
                   the body; a not-done phase still refuses under the flag.

## Coverage

| criterion | tasks |
|---|---|
| c-1  not-done phases refuse, named | t-4 |
| c-2  unabsorbed deferred refuse, source/idx/target named | t-4 |
| c-3  undisposed survivors refuse, file:line + phase named | t-1 (record), t-4 (re-classify + refuse) |
| c-4  one grouped refusal, every kind, clearing command per line | t-4 |
| c-5  ack with reason unblocks, appears in PR body; empty reason refused | t-2 (ack + empty refusal), t-4 (unblock + PR body) |
| c-6  --check enumerates + acks, non-zero, writes nothing | t-4 |
| c-7  /dross-milestone runs --check before confirm, walks items one per turn | t-3 |
| c-8  nothing unresolved / no record / empty phases → today's behaviour | t-4 |
| c-9  re-ack leaves one record with latest reason; --check stable | t-2 (upsert), t-4 (stable output) |
| c-10 --finalize skips the check | t-4 |
| c-11 stale acks left out of PR body, listed separately, non-blocking | t-4 |

All 11 criteria are covered. All three locked decisions are honoured: survivor_source in t-1 and t-4, ack_storage in t-2, force_unverified in t-4.

## Judgment calls

- Folded enumeration, refusal, --check, PR-body acks and the gate rewrite into one task (t-4), rejecting a separate enumerator task followed by a wiring task. Both halves live in one cmd-layer file pair, and a split adds a third wave with zero parallelism gain.
- Kept the ack writer (t-2) separate from t-4, rejecting a merge. It is an independent write surface that can run in wave 1 alongside t-1, and folding it in would push t-4 past 5 code files.
- The prompt task (t-3) is wave 1, rejecting wave 3 after the CLI lands. It depends only on the command names, which the spec locks, not on any code output.
- Made the closeout walk a new partial (`_milestone.closeout.md`), rejecting inline text. milestone.md has about 313 bytes of headroom under the 15000-byte cap, and the walk does not fit in that.
- `phase:<slug>` acks both the unverified and the not-done item for that slug, because the locked id scheme has no separate unverified id, and c-5 says any unresolved item is ackable. Rejected making unverified --force-unverified-only.
- `ack` validates only the item's prefix, not whether the item is currently unresolved, which keeps t-2 free of a t-4 dependency. A typo'd ack shows up in --check as a stale ack next to the still-blocking real item (c-11). Rejected a live-enumeration check at ack time.
- An unclassified survivor whose identity failed (empty key) is still recorded and still blocks, rejecting a skip, so the gate cannot be dodged by an unresolvable key. Its item id falls back to `survivor:<file>:<line>:<op>`.
- --check runs before the remote-config check and `milestonePRBase`, so it works offline and touches no ref (c-6). Its refusal text omits the PR target.
- No README task. No criterion requires one, and the README roadmap box flips at milestone close. A README row for `milestone ack` is a ship-time doc concern, not a criterion.
- No separate --finalize code task. Finalize already returns before the gate, so c-10 needs only a pinning test, which lives in t-4.
