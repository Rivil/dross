# board-mirror-reaper — verification lens

Phase board-mirror-reaper — 10 tasks across 5 waves

Designed backward from the test contract for each criterion. The ordering
principle: **the classifier is a pure read-only function before it is a
command**, because every criterion except c-8 is an assertion about what the
classifier decided, and a classifier reachable only through an HTTP-writing
command can only be tested through one.

```
Wave 1
  t-1  Nest dross issue verbs as subcommands
       files:    internal/cmd/issue.go, internal/cmd/issue_task.go,
                 internal/cmd/issue_task_pull.go, internal/cmd/deferred_add.go,
                 internal/forge/forge.go
       covers:   c-8
       desc:     Add `issue phase`, `issue task`, `issue milestone`, `issue backlog`
                 parent commands; re-register the five verbs as `sync`/`pull`
                 children with no hyphenated aliases. Repoint the Go-side runtime
                 hint strings (deferred_add.go:173, issue_task_pull.go:232/234)
                 and every `runCmd(t, Issue(), "<verb>", …)` call site
                 (`grep -rln '"phase-sync"\|"task-sync"\|"backlog-sync"\|"milestone-sync"\|"task-pull"' internal/cmd/*_test.go`).
       contract: - `runCmd(t, Issue(), "phase-sync", "01-auth")` returns cobra's
                   unknown-command error; `runCmd(t, Issue(), "phase", "sync", "01-auth")`
                   creates the issue — the existing issue_test.go body assertions
                   run unchanged under the new path.
                 - TestIssueTaskPullReportsPlanMoved (issue_task_pull hint text)
                   fails if the printed remedy still reads `dross issue task-sync`.
       depends:  —

  t-2  Add record-derived reap classifier
       files:    internal/cmd/issue_reap.go,
                 internal/cmd/issue_reap_classify_test.go
       covers:   c-3, c-4
       desc:     New `classifyReap(ctx, namespaces []string) ([]reapCard, error)` —
                 pure, GET-only. Walks each board.json namespace, derives a verdict
                 from the on-disk artefact (changes.json Status for Phases/Tasks,
                 milestone.toml [milestone].status for Milestones, phase.Dir stat
                 for `slug:` backlog keys, the target phase's changes.json for
                 `[routed]`, state.json version for Quicks), and skips any card the
                 tracker already reports Resolved via boardIssueIsDone.
                 reapCard carries {Key, Lane, Terminal, Why} — Why is the record
                 that justified it, for c-2's print.
       contract: - A phase whose changes.json has status="complete" yields its phase
                   card AND its task cards; flipping that record to "shipped" (or
                   removing status entirely) yields zero cards for that phase — the
                   card's own In Review / In Progress board state is identical in
                   both fixtures, so a classifier reading the card instead of the
                   record fails here.
                 - A milestone.toml with status="active" yields no epic card; the
                   same fixture with status="complete" yields it.
                 - A `slug:foo` backlog key yields a card only when
                   .dross/phases/foo/ exists; a `[routed]` deferred entry only when
                   its target phase's changes.json reads complete.
                 - A quick whose ref equals state.json's current version is not
                   classified; a quick at an older ref is.
                 - A card the fake forge reports Resolved is absent from the plan
                   even though every namespace still links it (c-4's first half).
                 - The fake forge fails the test on any non-GET request during
                   classify.
       depends:  —

Wave 2 (depends wave 1)
  t-3  Repoint prompt corpus to nested verbs
       files:    assets/prompts/execute.md, assets/prompts/milestone.md,
                 assets/prompts/plan.md, assets/prompts/ship.md,
                 assets/prompts/verify.md, internal/cmd/board_lifecycle_divergence_test.go,
                 internal/cmd/ship_prompt_test.go, internal/cmd/issue_verb_shape_test.go
       covers:   c-8
       desc:     Rewrite every `dross issue <a>-<b>` invocation in the five prompts
                 to the nested form; update taskSyncEdgeRE and the mirrorLanes
                 emission strings, and ship_prompt_test.go's three literal
                 `phase-sync …` / `task-sync …` needles, to the new spelling. New
                 issue_verb_shape_test.go is the guard.
       contract: - TestNoHyphenatedIssueVerbInThePromptCorpus: a regexp scan of
                   assets/prompts/*.md for `dross issue [a-z]+-[a-z]+` reports zero
                   hits — re-adding `task-sync` to execute.md reddens it.
                 - TestEveryPromptIssueInvocationResolves: each `dross issue …` line
                   in the corpus is walked against the real cobra tree via
                   Issue().Find(args); an invocation naming a command the tree does
                   not have fails, naming prompt and line. A prompt repointed to a
                   verb t-1 did not actually register fails here rather than on a
                   live board.
                 - TestEveryMirrorLaneHasATerminalEmission still passes: its
                   emission strings are matched verbatim against the rewritten
                   prompts, so a half-done rewrite (prompt changed, lane registry
                   not) fails.
       depends:  t-1

  t-4  Discover unlinked mirrors by marker label
       files:    internal/cmd/issue_reap.go,
                 internal/cmd/issue_reap_discover_test.go
       covers:   c-7
       desc:     Second classify source: ListIssues{State:"open", Labels:[labelMarker]},
                 minus everything board.IsLinked already covers, then recover each
                 orphan's lane from its own identity labels (dross/phase:<id>,
                 dross/task:<phase>/<task>, dross/deferred:<id>, dross/target:<slug>,
                 labelQuick) and run it through the SAME record-derived verdict.
                 An orphan with no recognisable identity label is reported as
                 unclassifiable and never closed.
       contract: - A marker-labelled open issue absent from every board.json
                   namespace but carrying dross/phase:01-auth, where 01-auth's
                   changes.json reads complete, appears in the plan with lane
                   "Phases" — the DRO-33/36/37/38/95/96 shape. Deleting the
                   dross/phase label from the same fixture moves it to the
                   unclassifiable list instead of dropping it silently.
                 - An orphan whose recovered phase is NOT complete is not in the
                   plan: discovery does not bypass the record check.
                 - An issue with no marker label (a human-filed bug) never reaches
                   the plan or the unclassifiable list.
                 - A card present BOTH in board.json and in the marker sweep appears
                   exactly once in the plan (dedupe by issue key).
       depends:  t-2

  t-5  Add dross issue reap dry-run plan
       files:    internal/cmd/issue_reap.go, internal/cmd/issue_reap_test.go
       covers:   c-1, c-2
       desc:     Register `dross issue reap` with a repeatable `--namespace` filter
                 and `--apply` (declared here, wired in t-7). No flag prints the
                 whole-board plan: one line per card — issue id, lane, and the Why
                 record — grouped by lane, with a per-lane and total count footer.
       contract: - TestReapWithoutApplyNeverWrites: the fake forge fails the test on
                   any POST/PUT/PATCH/DELETE; the run exits 0 and prints a plan.
                 - TestReapPlanNamesTheJustifyingRecord: each printed line for a
                   task card contains the issue key, the lane, and the phase id
                   whose changes.json justified it; a plan line that prints only the
                   key fails.
                 - TestReapNamespaceFilterScopesThePlan: `--namespace tasks` over a
                   fixture with stranded cards in all five lanes prints only the
                   task cards and a footer count matching them; `--namespace tasks
                   --namespace quicks` prints both; an unknown `--namespace bogus`
                   errors naming the valid set.
                 - TestReapWholeBoardCoversFiveLanes: with no flag, the plan's lane
                   set equals board.Board's map-field set (c-1's "every stranded
                   mirror class").
       depends:  t-2

  t-6  Guard every namespace has a reap path
       files:    internal/cmd/board_lifecycle_divergence_test.go
       covers:   c-6
       desc:     Extend the existing lane guard: assert the reap lane registry's key
                 set equals boardNamespaceFields(t), and that every lane's terminal
                 status is a configenum.LifecycleStatuses member with an entry in
                 BOTH defaultYouTrackStateMap and defaultJiraStateMap (read through
                 the existing stateMapPairs AST helper).
       contract: - TestEveryBoardNamespaceHasAReapPath: adding a map field to
                   board.Board with no reap lane entry fails by field name, in the
                   same run that adds it.
                 - TestEveryReapTerminalIsMapped: a lane whose terminal status is
                   absent from either provider's default state map fails naming the
                   provider and the status — a lifecycle status with no terminal
                   mapping cannot ship.
                 - The guard fails vacuous-pass: an empty lane registry or an empty
                   reflection result is a t.Fatal, matching namespace_guard_test.go.
       depends:  t-2

Wave 3 (depends wave 2)
  t-7  Apply closes cards, isolating per-card failures
       files:    internal/cmd/issue_reap.go, internal/cmd/issue_reap_apply_test.go
       covers:   c-1, c-4, c-5
       desc:     `--apply` walks the plan, writing each card's lane terminal through
                 closeBoardIssue (the existing verified read-back path), collecting
                 a per-card reapFailure{key, lane, err} instead of returning on the
                 first error — the taskCloseError shape issue_task.go already uses.
                 Drops the board.json link only for lanes whose forward path drops
                 it; ends by printing every failure by issue id and exiting non-zero
                 if any occurred.
       contract: - TestReapApplyClosesEveryLane: a fixture with one stranded card per
                   lane leaves all five Resolved on the fake forge, each written with
                   its OWN lane terminal (task cards get task-complete, phase cards
                   complete) — a single shared terminal fails the task-lane assertion.
                 - TestReapApplyContinuesPastAFailure: the fake forge 500s on the
                   second of four cards; the other three are Resolved, the run exits
                   non-zero, and stdout/stderr names the failing issue id exactly
                   once. Aborting on the first failure leaves cards 3 and 4 open and
                   fails.
                 - TestReapApplyIsIdempotent: a second `--apply` over the same fixture
                   classifies zero cards, issues zero write requests (fake forge
                   counts them) and exits 0 — c-4 end to end.
                 - TestReapNeverTouchesCorrectlyOpenCards: cards whose record is not
                   complete, and human-filed issues with no marker, are still open
                   after apply.
       depends:  t-4, t-5

  t-8  Report stranded count in doctor and watch
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go,
                 assets/prompts/watch.md
       covers:   c-2
       desc:     A `[board] stranded mirrors` doctor section calling the same
                 read-only classifier and printing the count plus the suggested
                 `dross issue reap --namespace <lane>` remedy; watch.md reads the
                 same line as a drift signal. Skipped silently when [board].enabled
                 is false.
       contract: - TestDoctorReportsStrandedMirrors: a fixture with 3 stranded cards
                   makes doctor print a warning naming 3 and the reap remedy; with 0
                   it prints the ✓ line and doctor's exit status is unchanged.
                 - TestDoctorNeverWritesToTheBoard: the fake forge fails the test on
                   any non-GET during `dross doctor` — the detector may not become a
                   silent sweep.
                 - TestDoctorSkipsStrandedCheckWhenBoardDisabled: no board section,
                   no HTTP request at all, with [board].enabled = false.
       depends:  t-5

Wave 4 (depends wave 3)
  t-9  Record applied runs and restore with --undo
       files:    internal/board/reaplog.go, internal/board/reaplog_test.go,
                 internal/cmd/issue_reap.go, internal/cmd/issue_reap_undo_test.go
       covers:   c-9
       desc:     Capture each card's pre-write state (WorkflowState/State/Resolved,
                 plus the board.json link that was dropped) before closing it, and
                 write .dross/reap-log.json — a standalone file, NOT a board.Board
                 map field, so it never registers as a mirror namespace with the t-6
                 guard. `--undo` replays the last applied run in reverse: restores
                 each card's recorded state and re-adds any dropped link.
       contract: - TestReapUndoRestoresThePriorState: apply moves 4 cards from
                   "In Review"/"In Progress" to their terminals; --undo returns each
                   card to the exact WorkflowState string it held pre-run, per card,
                   not one blanket state.
                 - TestReapLogCapturesStateBeforeTheWrite: the log entry for a card
                   records the state read BEFORE the close — a log written from the
                   post-close read-back would record the terminal state and make
                   undo a no-op; that fixture fails.
                 - TestReapUndoRestoresDroppedBoardLinks: a backlog key deleted at
                   apply is present again in board.json after --undo.
                 - TestReapUndoWithNoRecordedRun: `--undo` with no reap-log.json
                   errors naming the missing record and writes nothing; a second
                   --undo after a successful one does the same rather than
                   re-restoring.
                 - TestReapLogIsNotABoardNamespace: reflection over board.Board finds
                   no new map field, so t-6's guard does not demand a reap path for
                   the ledger.
       depends:  t-7

Wave 5 (depends wave 4)
  t-10 Document reap and the nested verb tree
       files:    README.md, ARCHITECTURE.md,
                 internal/cmd/issue_verb_shape_test.go
       covers:   c-8
       desc:     Update README's `dross issue {…}` capability row to the nested verb
                 set plus `reap`, add the reap paragraph to ARCHITECTURE's board-sync
                 section (classify-from-record, marker discovery, dry-run default,
                 per-card failure isolation, undo ledger), and extend t-3's guard to
                 scan README.md and ARCHITECTURE.md.
       contract: - TestNoHyphenatedIssueVerbInTheDocs: the t-3 scan, extended to
                   README.md and ARCHITECTURE.md, reports zero `dross issue [a-z]+-[a-z]+`
                   hits — the stale `milestone-sync,phase-sync` list at README.md:227
                   fails until rewritten.
                 - TestEveryDocumentedIssueVerbResolves: every `dross issue …`
                   invocation quoted in README/ARCHITECTURE resolves against the real
                   cobra tree, so a documented flag combination that does not exist
                   fails in CI.
       depends:  t-3, t-9
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (one sweep closes every class; correctly-open untouched) | t-5, t-7 |
| c-2 (dry-run by default, prints classified plan) | t-5, t-8 |
| c-3 (decision derived from on-disk record) | t-2, t-4 |
| c-4 (re-run is a no-op) | t-2, t-7 |
| c-5 (per-card failure recorded, sweep continues) | t-7 |
| c-6 (guard: namespace or status with no reap path) | t-6 |
| c-7 (marker-label discovery of unlinked mirrors) | t-4 |
| c-8 (no hyphenated verbs; prompts repointed; guards assert) | t-1, t-3, t-10 |
| c-9 (reversible sweep) | t-9 |

All 9 criteria covered.

## Judgment calls

- **Classifier before command (t-2 before t-5), not the reverse.** c-3 and c-4 are
  statements about a decision, not about a CLI. A classifier reachable only
  through `RunE` forces every record-derivation test through an HTTP fixture and
  a stdout regexp; a pure function lets each of the five evidence rules get its
  own two-fixture (complete / not-complete) test with the card's board state held
  identical. Rejected: build the command first and test classification through
  its printed plan.
- **Split the verb rename in two (t-1 Go, t-3 prompts) rather than one atomic
  rename.** board_lifecycle_divergence_test.go and ship_prompt_test.go assert on
  *prompt text*, while token_leak_test.go and issue_test.go assert on *cobra
  args* — putting both in one task means an 18-file commit whose failure mode is
  unreadable. The split keeps the suite green at each commit: t-1 leaves prompts
  emitting verbs that no longer exist, which no test executes, and t-3 closes
  that window with a guard that would have caught it.
- **Prompt guard walks the real cobra tree, not a hardcoded verb list.** A test
  asserting "prompts say `issue phase sync`" passes even if t-1 registered
  `issue phase-sync` under a nested group by mistake. `Issue().Find(args)` makes
  the prompt corpus and the command tree fail together. Rejected: a regexp
  allowlist of the five new spellings.
- **Quick-lane evidence is state.json's current version.** c-3 enumerates the
  record for four lanes and is silent on quicks. The quick prompt bumps the
  internal counter only *after* the work commit lands, so a quick ref that is no
  longer `state.version` provably finished; the ref that IS the current version
  may still be running and stays open. Rejected: treating any linked quick as
  reapable (closes a quick mid-run), and reading state.json's history (capped at
  50 entries — the same window bug changes.json Status exists to fix).
- **The undo ledger is a standalone `.dross/reap-log.json`, not a board.Board
  field.** A map added to Board is, by t-6's own guard, a mirror namespace
  requiring a reap path — the ledger would demand one and get a nonsense answer.
  Keeping it outside the struct means the guard stays honest, and t-9 asserts
  exactly that. Rejected: `Board.Reaped map[string]…`.
- **Pre-state capture belongs to the apply write, but is owned by t-9.** t-7
  could record the ledger itself, but then c-9's sharpest contract — the log
  records the state read *before* the close, not the post-close read-back —
  would have no owner. t-9 inserting the capture into the write loop keeps that
  assertion attached to the task that can fail it.
- **`--namespace` validated against board.Board's field set, not a literal
  list.** Same reason as t-6: the lane vocabulary has one derivation. A hand
  list would let a new namespace be unreachable by the filter while the guard
  still passed.
- **doctor's detector maps to c-2, not c-1.** It is a locked decision
  (`prompt_edge`) with no criterion of its own; the criterion it genuinely
  strengthens is c-2, because its contract asserts the classifier is write-free
  when driven from a second caller — which is what stops the detector quietly
  becoming a sweep.
- **c-1's "the 8 correctly-open cards are untouched" is tested as a negative in
  t-7, not as a live-board count.** A test asserting "90 cards closed" pins a
  fixture to one board's contents on one day. The durable form is: cards whose
  record is not complete, and cards with no marker, are still open after apply.
