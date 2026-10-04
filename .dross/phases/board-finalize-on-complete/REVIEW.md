# Plan Review — board-finalize-on-complete

Reviewed: 2026-10-04
Plan: 17 tasks across 4 waves

## BLOCKING
(none)

Coverage is complete: c-1 (t-5, t-12), c-2 (t-16), c-3 (t-1), c-4 (t-2, t-7, t-14), c-5 (t-2, t-8), c-6 (t-11, t-15), c-7 (t-3, t-9), c-8 (t-4), c-9 (t-10), c-10 (t-6), c-11 (t-13). No task contradicts a locked decision. rules.toml has one rule (r-01), no task violates it, and there is no global rules file.

## FLAG
- [test-contract] t-2's contract contradicts itself on the `planOps` seam. Bullet 1 says `save_lossless_test.go` must pass UNMODIFIED, and it names TestSaveRefusesUnverifiedPatch. That test (`:81`) and TestSaveVerifyNamesDifferingKey (`:402`) both assign `planOps = func(old, new *Project) ([]op, error)`. Bullet 3 forces a verify mismatch on the non-Project fixture "through the planOps seam". If `planOps` is retyped to take `any` so the fixture can go through it, both existing assignments stop compiling. If `planOps` stays typed on `*Project`, the fixture cannot go through it. Go also cannot hold a generic function in a variable, so there is no way to satisfy both clauses.
  Suggestion: pick one before execution. Either keep `planOps` as Save's `*Project`-typed seam and give the door its own seam (and reword bullet 3 to name it), or drop "UNMODIFIED" for those two tests.

- [wave-order] t-7 changes the door's signature ("the door takes an identity map") in the same wave that t-8 becomes a caller of it (`survivor.Save` writes through the door). Whichever of the two lands second has to edit the other's file. t-7's files also leave out three things the change affects:
  - `internal/project/project.go`, because after t-2 `project.Save` calls the door.
  - `internal/project/save_lossless_test.go:82,403`, which call `diff(old, new)` directly.
  - `internal/project/patch_diff_test.go:36`, which also calls `diff(old, new)` directly.

  If the identity map becomes a positional parameter of `diff` or the door, all three break. The save_lossless tests are also the ones t-2 pinned as unmodified.
  Suggestion: make the identity map an optional argument (variadic or an options struct, nil meaning positional), so t-2's callers and t-8 compile unchanged. Otherwise add those files to t-7 and make t-8 depend on t-7.

- [test-contract] t-7 will almost certainly break TestArchitectureNamesLosslessWriter (`internal/cmd/lossless_docs_test.go:69`). That test checks that the ARCHITECTURE.md anchors `` `apply` — internal/project/patch.go:607 `` and `` `diff` — internal/project/patch_diff.go:105 `` land on a line naming the symbol. t-7 adds op kinds and keyed matching to `patch.go` and `patch_diff.go`, above both anchors, so both lines shift. t-2 re-points the anchors in wave 1, and t-7 then moves them again in wave 2. ARCHITECTURE.md is not in t-7's files, and none of t-7's contract bullets mention the doc test.
  Suggestion: add ARCHITECTURE.md to t-7's files, plus a contract bullet saying TestArchitectureNamesLosslessWriter still resolves every anchor after the keyed-patcher edit.

- [antipattern: missing file] t-8 (and possibly t-2) will trip TestEveryDrossWriterIsDeclared (`internal/cmd/secretscan_writer_enum_test.go:181`). `internal/secretscan/writers.go:186` declares `internal/survivor/store.go` as the writer of `survivors.toml`, and the test fails with "stale declaration — the file is gone or no longer reaches a write verb" once a declared file has no `os.CreateTemp`/`WriteFile`. t-2's door already handles the fresh-encode path. That makes `survivor.saveAtomic` dead code, and deleting it leaves `store.go` with no write verb. Separately, if t-2 puts a write verb in the new `patch_save.go`, that file is undeclared. Either way the registry's `project.go` row would now be writing `plan.toml` and `survivors.toml` while still declaring only `project.toml`.
  Suggestion: add `internal/secretscan/writers.go` to t-8's files (and to t-2's if the door gets its own write verb), with a bullet that TestEveryDrossWriterIsDeclared stays green and the registry names the real writer.

- [antipattern: missing file] t-5's last contract bullet says "issue_test's ctx.go-constant membership check covers StatusComplete". That check is TestLifecycleVocabularyIsTheConfigenumSet in `internal/cmd/issue_test.go:1899`, which tests a hand-written slice (`:1900`). Making it cover the new constant means editing that file, and it is not in t-5's files. There is a related gap: t-5 adds only `StatusComplete`, but FinalizePhase also emits `task-complete`. t-16's new emittedStatuses source resolves finalize.go's status arguments "through package consts", and no `task-complete` constant exists in boardsync.
  Suggestion: add `internal/cmd/issue_test.go` to t-5's files, and have t-5 add a `StatusTaskComplete` constant next to `StatusComplete` so t-16's const-resolving scanner has something to resolve.

- [test-contract] The c-7 validate rule (t-3) and c-9's in-place re-route (t-10) can combine to make a completed phase's spec fail validation permanently. t-3 refuses an absorbed id unless the item's Target is the absorbing spec's own phase. Here is the sequence: a phase P criterion absorbs a routed survivor item through t-9, P completes, and a later verify run still lists the survivor. `dross survivor route` then rewrites that same entry's Target to R (t-10's design). From then on `dross validate` fails on P's spec. P is complete and cannot be fixed without hand-editing its history. Neither t-3's nor t-10's contract covers re-routing an item that a criterion has already absorbed. Plain `deferred route` reaches the same state, because t-4 checks only whether the target is complete, not whether the source is.
  Suggestion: settle the case in t-3 or t-10 with a contract row. Either route refuses an item that some criterion absorbs, naming it, or validate accepts an absorbed id in a complete phase whose item has since moved on.

- [test-contract] t-12's failure-path bullets check only the on-disk completion record. They do not check what the run reports.
  - finalizeBoard runs after the remote delete but before `RecordOutcomeEvent("phase_complete", …)` and the `completed <id> — …` / topology lines (`internal/cmd/phase.go:772-794`). If it returns early on a board error, a successful completion loses both its narration and its outcome event, and gets recorded as a command error instead.
  - Every board-on bullet uses an unprotected `shipFixture`. On this repo, main and milestone/* refuse direct pushes, so the board.json chore goes out through a second `routeBaseChores` → `publishChorePR` call on top of the completion-record chore PR. That path is not exercised, and "local base == origin/base" does not hold on it.

  Suggestion: add bullets saying a board failure still prints the `completed <id>` and branch lines and records `phase_complete`, and cover the protected-base chore path with a ChorePR fake (reusing the open chore PR).

- [wave-order] After c-11 the CLI help is stale in two places, and fixing either means re-minting `internal/cmd/testdata/cli_surface/issue.txt`:
  - `issue reap`'s Short is still "Close board mirrors the forward lifecycle left stranded", although reap now also creates cards (t-13).
  - `issue task sync --close`'s usage still says "use at ship finalize" (t-16).

  t-12 owns `issue.txt` in wave 2, alongside t-13. Updating reap's Short in t-13 would collide with t-12 on the golden.
  Suggestion: either leave the help strings alone deliberately and say so, or give the help-text and golden updates to one task ordered after t-12 (t-17 is already last).

- [test-contract] t-17's ARCHITECTURE sweep misses two claims in the same `:559` paragraph that this phase makes false.
  - It says backlog sync closes "a routed item whose target phase shipped". c-6/t-15 make that explicitly wrong.
  - It says "every mirror lane must carry a terminal emission at a real call site in the prompt corpus". After t-16, the Phases and Tasks terminal emissions live in finalize.go.

  t-17's contract only pins the "no board coupling" and "ship emits the closes" wording.
  Suggestion: extend t-17's description and its ARCHITECTURE bullet to cover the disposition-record close rule and where the terminal emissions now live.

- [test-contract] t-9's `deferred absorb` takes `--phase` and defaults it to the current phase. /dross-spec is told to run absorb after §5, but `dross state set current_phase <id>` only happens in §6. On the explicit `<phase-id>` entry path, current_phase can therefore be a different phase. The absorb then refuses (target mismatch), so it fails closed, but the absorption does not happen. t-9's spec_prompt_test bullet checks only that the instruction exists and where it sits relative to §5. It does not check that the call passes `--phase <phase-id>`.
  Suggestion: add to the spec_prompt_test bullet that the instruction passes `--phase <phase-id>` explicitly.

## NOTE
- [strengths] The plan is tied closely to the real code. All 13 prompt line numbers in t-6 match the files. Every existing test, fake and helper the contracts name (stateBoard/faultBoard, writingIsFatalYT, shipFixture, simulateSquashMerge, TaskCloseError, planOps, TestReroutedDeferredSwapsItsTargetLabel and others) exists where the plan says. The TestNoTestLost discipline (names kept, inverted bodies) is applied consistently in t-2, t-14 and t-16.
- [strengths] Several contracts guard against passing vacuously:
  - t-6 notices that `promptContent` strips underscores. That makes TestInboxPromptReadsBoardEnabled's `remote.board_sync` check vacuous today, so t-6 reads the prompts raw and sets a minimum count of 6 prompts.
  - t-8 adds a negative control: re-encoding the store with `toml.NewEncoder` must trip the secret scanner.
  - t-14 runs a corpus test over every real `plan.toml`.
  - t-11's signature takes no board client, so a disposition based on card state cannot compile.
- [strengths] Several choices protect existing history and the locked failure posture:
  - t-4 keeps the complete-phase refusal out of `validDeferredTarget`, with a bullet that validate stays green on a repo already holding items routed to complete phases (the feastahead case).
  - t-12's `auth_env`-unset bullet pins that `openBoard` runs after teardown, which is the locked board_failure_posture.
- [granularity] Nine tasks reach the 5-file heuristic (t-1, t-2, t-4, t-5, t-6, t-9, t-12, t-13, t-15). In each case the count comes from test files and mechanical wiring (a golden, a pin test, an anchor doc) around one behaviour. None spans three layers, and I don't recommend splitting any. t-9 (new CLI verb plus a spec.md instruction) is the only one that combines two surfaces, and its prompt half is a single sentence.
- [scale] There are 134 phases whose `changes.json` reads `status=complete`, against 58 phase links in `.dross/board.json`. The first `dross issue reap --apply` after this phase will therefore create and close up to ~76 phase cards plus all their task cards on DRO. c-11 asks for exactly this, but it is a large write to an external tracker, so run the dry run first and read it. Catch-up detection also costs one `dross/phase:<id>` lookup per complete phase (134 today) on every reap run. That is deliberate (t-13 forbids a bulk marker listing) and grows linearly.
- [criterion-reading] t-14 treats `task_seq`, reflowed `wave` lines and stripped `depends_on` entries as "changed task state". A strict reading of c-4 ("every line outside the task(s) it changed byte-identical") would not include the root `task_seq` line. The plan's reading is the only workable one for `task add`. Record it as the agreed reading at verify so the c-4 mapping isn't argued over.
- [decision-reversal] This phase reverses an earlier locked decision, `terminal_emit_sites` ("dross phase complete gains no board coupling"), which is cited at `ship_prompt_test.go:337` and ARCHITECTURE.md `:559`. The plan does handle it: t-16 swaps the sub-check and t-17 rewrites the prose. Mention the reversal in the PR body so the history is easy to follow.
- [r-01] t-6 and t-16 carry the r-01 `make install` reminder. t-9 edits `assets/prompts/spec.md` without it. t-12 changes the binary that this phase's own `/dross-ship` → `dross phase complete` will run, so `make install` before shipping matters most here.

## Summary
The plan covers every criterion, respects the locked decisions and is grounded in real code. It needs no blocking change, but resolve the t-2 `planOps` contradiction and the t-7 coupling (door signature, missing files, ARCHITECTURE anchor drift) before wave 2. Otherwise those surface as compile or doc-test failures in the middle of execution.
