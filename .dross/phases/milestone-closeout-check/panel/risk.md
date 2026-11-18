# Planner: risk (failure modes drive the graph)

Phase milestone-closeout-check — 9 tasks across 4 waves

Wave 1
  t-1  Persist undisposed out-of-scope survivors in verify.toml
       files:    internal/verify/verify.go, internal/verify/lifecycle.go, internal/verify/verify_test.go,
                 assets/prompts/verify.md, internal/cmd/verify_prompt_test.go
       covers:   c-3
       desc:     Add `Survivors []SurvivorRecord` (`toml:"survivor,omitempty"`: key, file, line, op, ambiguous)
                 to verify.Verify; Skeleton fills it from every t.OutOfScope row whose lifecycle is
                 unclassified (or unset), ApplyLifecycle stamps an Ambiguous flag onto OutOfScopeMutant.
                 verify.md §3 gains one line: leave `[[survivor]]` records exactly as written.
       contract: - Skeleton over one unclassified + one accepted + one routed OOS survivor writes exactly
                   one [[survivor]] record (the unclassified one) carrying its key, file, line and op.
                 - An OOS survivor whose identity did not resolve (empty key) is still recorded with its
                   file:line:op — a test asserting len(Survivors)==1 fails if empty-key rows are dropped.
                 - An accepted-but-ambiguous survivor is recorded with ambiguous=true.
                 - Two OOS rows at the same file/line/op produce one record (re-run idempotency).
                 - LoadVerify → Save (the `verify finalize` path) round-trips the records unchanged; a
                   legacy verify.toml with no [[survivor]] loads with zero records and re-saves with no
                   `survivor` table.
                 - verify_prompt_test fails if §3 loses the "leave [[survivor]] records as written"
                   instruction; TestPromptByteBudget still holds verify.md ≤ 15000 bytes (388 B headroom
                   today).

  t-2  Add closeout_ack schema with idempotent upsert
       files:    internal/milestone/milestone.go, internal/milestone/milestone_test.go,
                 internal/cmd/milestone_test.go
       covers:   c-5, c-9
       desc:     `CloseoutAcks []CloseoutAck` (`toml:"closeout_ack,omitempty"`: item, reason, at) on
                 Milestone; `UpsertAck(item, reason, at)` replaces an existing record in place or appends,
                 refusing a blank/whitespace reason; `Acks()` accessor.
       contract: - UpsertAck("phase:x","a") then UpsertAck("phase:x","b") leaves len==1 with reason "b".
                 - UpsertAck with reason "   " returns an error and leaves CloseoutAcks unchanged.
                 - Save → Load round-trips item, reason and at; a toml with no acks re-saves without a
                   `closeout_ack` key (c-8 byte compatibility).
                 - cmd: `dross milestone add <v> phases y` on a toml carrying an ack keeps the ack — fails
                   if the struct-based writers (set/add/remove/replace) drop the new table.

  t-3  Closeout core: item model, ids, phase kinds
       files:    internal/closeout/closeout.go (new), internal/closeout/closeout_test.go (new),
                 internal/closeout/phases.go (new), internal/closeout/phases_test.go (new),
                 internal/cmd/milestone_verify_gate.go
       covers:   c-1, c-4, c-8
       desc:     Kinds (unverified, not-done, deferred, survivor), Item{Kind, ID, Detail, Clear}, item-id
                 grammar (`phase:<slug>`, `deferred:<source>#<idx>`, `survivor:<key>`) with Parse/Format,
                 Evaluate(root, version, collectors…) that runs every collector, and the two phase-kind
                 collectors. The unverified logic moves here from milestone_verify_gate.go, which keeps a
                 thin wrapper so today's gate tests still bind.
       contract: - Not-done: an unscaffolded roadmap slug, a scaffolded slug with no changes.json, and one
                   whose changes.json is malformed are all listed as `phase:<slug>`; a complete/shipped
                   one is not. A slug listed twice in phases yields one item.
                 - Evaluate with a collector stub that errors plus one that returns an item yields BOTH: the
                   error as a blocking, non-ackable line under its own kind, the item under its kind —
                   fails if Evaluate returns early on the first collector error (c-4 "never stops").
                 - No milestone toml and an empty phases array both yield zero items (c-8).
                 - Output order is deterministic: kind order fixed, items sorted by ID within a kind; two
                   Evaluate calls on the same tree return deep-equal reports.
                 - Parse rejects `phase:`, `deferred:x#`, `deferred:x#-1`, `survivor:` and an unknown
                   prefix; Parse(Format(item)) round-trips.
                 - Unverified items carry no ack id (unverified is cleared by verify or
                   --force-unverified, per locked force_unverified).

  t-4  Walk close-out items in /dross-milestone completion arm
       files:    assets/prompts/milestone.md, assets/prompts/_milestone.closeout.md (new),
                 internal/cmd/milestone_dispatch_prompt_test.go
       covers:   c-7
       desc:     §8 runs `dross milestone complete <v> --check` before the close-out AskUserQuestion; on a
                 non-zero exit it fetches `dross prompt partial milestone closeout`, which walks one item per
                 turn (resolve now via the line's command / `dross milestone ack <item> --reason`) and
                 re-runs --check; the integration PR opens only once --check exits 0.
       contract: - Prompt test: inside the "## 8. Completion arm" section, `--check` appears before the
                   close-out AskUserQuestion and before `dross milestone complete <version>` (open).
                 - Prompt test: the partial names "one item per turn", both options (resolve / acknowledge
                   with a reason) and "re-run --check"; fails if the PR-open step is reachable without a
                   clean --check.
                 - TestPromptByteBudget keeps milestone.md ≤ 15000 bytes (313 B headroom today — the walk
                   must live in the partial, not inline).

Wave 2 (depends t-3; t-6 also t-1)
  t-5  Deferred-absorption collector
       files:    internal/closeout/deferred.go (new), internal/closeout/deferred_test.go (new)
       covers:   c-2
       desc:     Lists every non-dismissed deferred item (phase specs + _project store) whose target is a
                 phase in the milestone and whose id appears in no criterion `deferred` list of that
                 target's spec, as `deferred:<source>#<idx>` naming source, index, target and the
                 `dross deferred absorb … --criterion <c-N> --phase <target>` command. Read-only: never
                 mints ids.
       contract: - An item routed to a milestone phase with no `dross deferred absorb` record is listed with
                   source, index and target.
                 - An item absorbed by a criterion of the target spec is not listed; one whose id is
                   absorbed only by a DIFFERENT phase's criterion is still listed.
                 - An item with no id yet (pre-identity) is listed, and the spec file is byte-identical
                   after the collector runs (no EnsureIDs/mint side effect).
                 - A target that is an unscaffolded roadmap slug (no spec.toml) lists its items as
                   unabsorbed rather than erroring.
                 - Dismissed items, someday items, and items routed to a phase outside the milestone are
                   never listed; a `_project` store item routed into the milestone is.
                 - A malformed source spec.toml returns an error (surfaced by Evaluate as a blocking line),
                   never an empty list.

  t-6  Survivor-disposition collector from verify records
       files:    internal/closeout/survivors.go (new), internal/closeout/survivors_test.go (new),
                 internal/deferred/deferred.go, internal/survivor/store.go, internal/cmd/verify.go
       covers:   c-3
       desc:     Move routedSurvivors → deferred.RoutedSurvivors and acceptedReasons →
                 survivor.AcceptedReasons so verify-time and close-out classification share one source.
                 The collector reads each milestone phase's verify.toml [[survivor]] records and
                 re-classifies them against the current stores (locked survivor_source).
       contract: - A record accepted (non-ambiguous) after the run is cleared; a record with ambiguous=true
                   is NOT cleared by an acceptance, only by a route — mirrors lifecycle.Classify precedence.
                 - A record routed via a live deferred entry is cleared; routed via a dismissed entry, or a
                   someday (target-less) entry, is still listed.
                 - The same key recorded by two milestone phases yields ONE `survivor:<key>` item naming
                   file:line and both phases.
                 - An empty-key record is listed (id falls back to `survivor:<file>:<line>:<op>`), never
                   dropped.
                 - A phase with no verify.toml or no records (pre-ship legacy) contributes nothing.
                 - A malformed survivors.toml returns an error, not an empty list.
                 - Existing verify lifecycle tests stay green after the helper move (same routed/accepted
                   maps feed ApplyLifecycle).

  t-7  Ack join and grouped close-out rendering
       files:    internal/closeout/render.go (new), internal/closeout/render_test.go (new)
       covers:   c-4, c-5, c-6, c-11
       desc:     ApplyAcks(report, acks) splits items into unresolved / acknowledged, and acks matching no
                 current item into stale. Renderers: Refusal (unresolved grouped by kind, each line with its
                 clearing command), Check (Refusal + acknowledged + a separate "stale acks" heading), and
                 PRBodySection (acknowledged item + reason only, current detail text included).
       contract: - Refusal over items of all four kinds prints four kind headings, every item under its own,
                   each line containing its Clear command — fails if rendering stops at the first kind.
                 - An ack whose item is no longer produced lands under "stale acks", is absent from
                   PRBodySection, and does not count toward the blocking total (c-11).
                 - An ack never matches an unverified item (phase:x acked, x unverified → still blocking).
                 - PRBodySection is empty string when there are no live acks, so the PR body is
                   byte-identical to today's.
                 - Check output contains no wall-clock value: rendering the same report twice is identical
                   (c-9 "same result").

Wave 3 (depends t-2, t-5, t-6, t-7)
  t-8  Wire close-out into milestone complete (+ --check)
       files:    internal/cmd/milestone.go, internal/cmd/milestone_verify_gate.go,
                 internal/cmd/milestone_closeout_test.go (new), internal/cmd/milestone_verify_gate_test.go
       covers:   c-1, c-2, c-3, c-4, c-5, c-6, c-8, c-10, c-11
       desc:     `milestoneCloseout(root, version, forceUnverified)` composes all collectors + the
                 milestone's acks; open mode refuses with the Refusal render; `--check` runs BEFORE the
                 [remote] check and milestonePRBase (no git/network) and returns a non-zero exit; PR body
                 appends PRBodySection. --finalize branch untouched (returns before the check).
                 --force-unverified drops only the unverified kind. Existing gate fixtures seed
                 changes.json complete where they expect the PR to open.
       contract: - Fixture with one item of every kind: `complete` returns an error naming all four kinds,
                   cap.created==0 (c-1..c-4).
                 - `--check` on that fixture: non-zero exit, cap.posts==0, origin's milestone ref
                   unchanged, and a sha256 over every file under .dross is identical before/after (c-6);
                   also succeeds with no [remote] configured.
                 - `--check --finalize` is refused as conflicting flags.
                 - `--force-unverified` with a done-but-unverified phase opens the PR; with a not-done phase
                   it still refuses naming `phase:<slug>`.
                 - After acking every ackable item: PR opens, captured body contains each item + reason; a
                   stale ack's item is absent from the body (c-5, c-11).
                 - No toml / empty phases: PR opens with the body string byte-equal to today's; --check
                   exits 0 printing "nothing unresolved" (c-8).
                 - `--finalize` on a merged milestone with a not-done phase + unabsorbed deferred item
                   finalizes (status complete) and prints no refusal (c-10).
                 - TestMilestoneCompleteOpensWhenEveryPhasePasses / ForceUnverifiedOverrides still pass
                   with done records seeded (the not-done kind must not regress the old gate).

Wave 4 (depends t-8)
  t-9  Add `dross milestone ack <item> --reason`
       files:    internal/cmd/milestone_ack.go (new), internal/cmd/milestone_ack_test.go (new),
                 internal/cmd/milestone.go
       covers:   c-5, c-9
       desc:     Resolves the version (state.current_milestone or --milestone), validates the id with
                 closeout.Parse, requires it to be a currently unresolved OR already-acknowledged ackable
                 item (via milestoneCloseout), then UpsertAck + Save. Registered in Milestone().
       contract: - `--reason ""` and `--reason "  "` exit non-zero and the milestone toml is byte-identical.
                 - A malformed id, an id matching no current item (typo), and an unverified-only phase id
                   are refused, naming the currently ackable ids / --force-unverified; toml unchanged.
                 - Acking the same item twice leaves exactly one [[closeout_ack]] carrying the second
                   reason (re-ack of an acknowledged item is allowed, not "unknown item").
                 - `complete --check` run twice after an ack prints identical output and lists the item
                   under acknowledged (c-9).
                 - No milestone toml → refused ("nothing to acknowledge"), no file created.

## Coverage
- c-1 → t-3 (not-done collector), t-8 (refusal wiring)
- c-2 → t-5 (collector), t-8 (refusal wiring)
- c-3 → t-1 (records persisted), t-6 (re-classification), t-8 (refusal wiring)
- c-4 → t-3 (Evaluate never stops on a failing collector), t-7 (grouped render with clear commands), t-8 (end-to-end all four kinds)
- c-5 → t-2 (blank-reason refusal in schema), t-7 (PR body section), t-8 (acked item stops blocking + body), t-9 (ack command)
- c-6 → t-7 (check view incl. acknowledged), t-8 (no write / no push / no PR, exit code)
- c-7 → t-4
- c-8 → t-3 (no toml / empty phases = zero items), t-7 (empty body section), t-8 (byte-identical PR body, --check exit 0)
- c-9 → t-2 (upsert), t-7 (deterministic render), t-9 (end-to-end re-ack + repeated --check)
- c-10 → t-8
- c-11 → t-7 (stale classification + body exclusion), t-8 (end-to-end)

All 11 criteria covered.

## Judgment calls
- Collector errors become a blocking line inside their kind (t-3), rejected aborting the whole check — one malformed spec must not hide every other kind or silently pass, and c-4 forbids stopping at the first failing kind.
- Ack join/rendering is its own task (t-7), rejected folding it into the core — stale-ack and "acks never clear unverified" are distinct failure modes that deserve one owner and one test file.
- `phase:<slug>` acks clear only the not-done kind, never unverified — rejected one ack clearing both kinds; locked force_unverified keeps unverified on --force-unverified, and sharing the id would let an ack silently skip verification.
- `ack` refuses ids that are not currently unresolved or already acked (t-9), rejected accepting any well-formed id — a typo'd ack would otherwise sit as a stale ack while the real item keeps blocking.
- Empty-key survivors fall back to `survivor:<file>:<line>:<op>`, rejected dropping them — an unresolvable identity is exactly the case c-3 must not lose; it stays inside the locked `survivor:<key>` shape.
- Persist an `ambiguous` flag on survivor records (t-1), rejected key-only records — without it close-out would let an acceptance clear a survivor that verify itself refused to suppress.
- Move routedSurvivors/acceptedReasons into their packages (t-6), rejected duplicating them in closeout — two copies of "what counts as routed" would drift (e.g. dismissed handling).
- `--check` runs before the [remote] check and milestonePRBase, rejected after — PRBase does git/network work and c-6 requires no writes; it also lets --check work on a repo with no remote.
- `--check` honours `--force-unverified`, rejected always reporting unverified — otherwise the prompt's "open only once --check is clean" can never be satisfied by a user who chose to force.
- Positional `deferred:<source>#<idx>` ids can re-point if a source's items are removed; accepted as residual risk under the locked id format, mitigated by PRBodySection printing the item's current text so a re-pointed ack is visible to the reviewer.
- The closeout walk goes into a new partial `_milestone.closeout.md`, rejected inlining in milestone.md — 313 bytes of budget headroom cannot hold it.
- t-9 is wave 4 (needs t-8's milestoneCloseout composition for the membership check), rejected wave 3 — it would otherwise have to compose collectors a second time; both tasks touch milestone.go only in separate hunks (RunE vs AddCommand list).
- Already-open PR ("already exists") is not updated with new acks — out of scope; spec covers opening only.
