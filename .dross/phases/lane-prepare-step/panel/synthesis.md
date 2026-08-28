# lane-prepare-step — panel synthesis

## Scores

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| **risk** (8 tasks / 4 waves) | 4/5 — 6/6 criteria owned, but the doc task covers README only; `assets/prompts/execute.md` and `assets/prompts/options.md` are never touched, and options.md has a *live* gate (`TestOptionsDocumentsTheSelectorSurfaceCorrectly`) that the superseded `lane_edit_surface` lock now bears on | 5/5 — the sharpest contracts on the panel: byte-offset transcript asserts, `worseOutcome` in BOTH argument orders, an unforgeability assert that no tagged pair can produce an untagged fingerprint, and the only "every prepare failed → 7 not 5" contract | 4/5 — clean 8-way split, but t-7 is scoped to the *remote* re-tag only, leaving the local classification implicitly inside t-6 | 4/5 — dependency order is sound and t-3/t-6 are correctly sequenced on `test.go`; the t-6/t-7 seam is the one blur |
| **mvp** (4 tasks / 3 waves) | 5/5 — the only draft that reaches all three doc surfaces (README + options.md + execute.md) and the only one that spots the `json:"prepare,omitempty"` parity gate | 4/5 — real, named existing gates (`TestTomlFieldsCarryMatchingJSONTags`, `TestExitCodesArePairwiseDistinct`, `TestOptionsDocuments…`), but the fat tasks force coarser per-assertion contracts | 2/5 — t-1 bundles schema + `add` + `list` + a whole new `edit` verb; t-3 bundles the exit constant, its rank, the fence, the run wiring, failure classification and both transports. Too much for one atomic commit | 3/5 — only 3 waves because the tasks are fat; the docs-in-wave-2 argument is genuinely good, the rest is a consequence of the bundling |
| **verification** (7 tasks / 4 waves) | 4/5 — 6/6 criteria owned; README + execute.md covered, options.md missed. One contract is unobservable: a hand-edited `prepare = ""` is indistinguishable from an absent key after `project.Load` (omitempty), so that validate assert cannot fail | 4/5 — concrete new test names, per-lane interleave ordering, and the only draft to preserve ssh-255-stays-3 *and* classify the prepare on **both** transports | 5/5 — best separation on the panel: spawn (t-4) apart from failure classification (t-5), the exit taxonomy standalone (t-2), the `edit` verb standalone (t-6) | 4/5 — sound, except t-3 and t-4 both sit in wave 2 editing `internal/cmd/test.go`. The draft flags this itself and asks the judge to sequence them |

**Skeleton: verification.** It has the granularity the other two lack — separating "a prepare spawns at all" from "a prepare that fails costs only its own lane" is the split that makes the isolation contract checkable rather than an afterthought, and it is the only draft whose classification task covers both transports. `internal/cmd/test.go:601` confirms why that matters: `runOneLane`'s **local** arm hardcodes `exitSuiteFailed` in `test lane %q failed`, so a remote-only re-tag (risk t-7) would ship a local prepare failure still reported as exit 1 — the exact collision exit 7 was locked to prevent. mvp's coverage was better and risk's contracts were sharper; both are grafted below.

## Merged plan

```
Phase lane-prepare-step — 7 tasks across 5 waves

Wave 1
  t-1  Add lane prepare field, add flag, list row, validate rule   [verification+risk+mvp]
       files:    internal/project/project.go, internal/cmd/test_lane.go,
                 internal/cmd/validate.go, internal/cmd/test_lane_test.go,
                 internal/cmd/validate_lane_test.go
       covers:   c-1
       depends:  —
       contract: - `Prepare string` carries BOTH `toml:"prepare,omitempty"` and
                   `json:"prepare,omitempty"`; a toml-only tag fails the existing
                   TestTomlFieldsCarryMatchingJSONTags (json_tag_parity_test.go:48)  [mvp]
                 - `dross test lane add p --match a --command c --prepare "make build"`
                   then project.Load: lane.Prepare == "make build"; a flag parsed but
                   never written into the block fails this                          [verification]
                 - a lane added WITHOUT --prepare writes no `prepare =` key at all, and a
                   project.toml written before this phase round-trips BYTE-IDENTICALLY;
                   a missing omitempty fails the byte compare, not a later run       [risk]
                 - `dross test lane list` prints "  prepare: make build" for the declaring
                   lane and NO prepare row for its neighbour                         [verification]
                 - a lane added with a prepare starts UNGRANTED: the add output names
                   `dross trust --lane p` and trusted_lane_commands has no entry for p [risk]
                 - `--prepare "   "` normalizes to absent at the CLI (no `prepare =` key
                   written), while a hand-edited `prepare = "   "` block IS reported by
                   `dross validate` naming that lane — whitespace-only is the one shape
                   that is non-empty for the fingerprint and empty for the reader, and it
                   is the only prepare shape post-load state can still distinguish   [risk]

  t-2  Rank a prepare failure above a red suite                     [verification+risk]
       files:    internal/cmd/test.go, internal/cmd/test_lane_consent_test.go
       covers:   c-3
       depends:  —
       contract: - TestExitPrecedenceIsTotal's order slice becomes {transport, partial,
                   prepare, suiteFailed, laneRefused, nothingMeasured} and the table drives
                   every co-occurring pair in BOTH argument positions; any rank swap fails
                   it in both directions                                             [verification+risk]
                 - exitRank(exitPrepareFailed) is strictly above exitRank(exitSuiteFailed)
                   and strictly below exitRank(exitPartial), asserted on the ranks — a code
                   omitted from exitRank falls to the unknown default, which is literally
                   `return 3`, exitSuiteFailed's own rank, and would tie silently  [risk]
                 - TestExitCodesArePairwiseDistinct (test_files_test.go:137) fails if
                   exitPrepareFailed collides with an existing code                  [mvp]
                 - a red lane in a repo declaring no prepare still exits 1: the new code is
                   unreachable without a prepare line                                [risk]

Wave 2 (depends t-1)
  t-3  Fingerprint prepare into the lane grant                      [verification+mvp+risk]
       files:    internal/cmd/trust.go, internal/cmd/test.go, internal/cmd/doctor.go,
                 internal/cmd/trust_lane_test.go
       covers:   c-4
       depends:  t-1
       contract: - laneConsentInput({prepare:"a", command:"bc"}) !=
                   laneConsentInput({prepare:"ab", command:"c"}) — naive concatenation
                   collides here, which is a lane re-split across its two fields keeping a
                   grant issued for neither                                          [all three]
                 - a lane declaring NO prepare fingerprints to Fingerprint(lane.Command)
                   byte-for-byte, so a grant written into local.toml before this phase still
                   reads ConsentGranted; framing applied unconditionally fails this and
                   would stale every existing lane grant on every machine            [all three]
                 - no (prepare, command) pair can forge a no-prepare lane's fingerprint:
                   assert by feeding the framed bytes back in as a bare command and
                   requiring a MISS                                                  [risk]
                 - grant a lane, then append a prepare: LaneConsented returns ConsentStale
                   (not Granted, not Absent) and `dross trust --lane go --check` exits
                   non-zero carrying the "has CHANGED since you trusted it" wording   [verification]
                 - `dross trust --lane go` prints the prepare line AND the command line,
                   with the prepare's index BEFORE the "recorded in .dross/local.toml"
                   line — a grant that wrote before printing both fails the ordering assert;
                   a refusal showing only the command lets a user re-grant a bootstrap
                   they never read                                                   [verification+risk]
                 - `dross doctor`'s row for a lane declaring a prepare prints that line
                   under the command, and a lane declaring none prints no prepare row  [verification]
                 - doctor's STALE row for a lane whose prepare changed still names
                   `dross trust --lane <name>` as the fix, so the state that refuses and
                   the state doctor reports agree                                    [risk]

Wave 3 (depends t-3)
  t-4  Run each lane's prepare before its command                   [verification+risk+mvp]
       files:    internal/cmd/test.go, internal/cmd/test_lane_prepare_test.go
       covers:   c-2, c-5
       depends:  t-1, t-3
       contract: - remote fixture, one prepared lane: recorded remote spawns are rsync, then
                   an ssh script carrying the prepare, then an ssh script carrying the
                   command. A prepare hoisted above the sync fails the index assert  [verification]
                 - two prepared lanes interleave PER LANE: [prep-go, go-cmd, prep-docs,
                   docs-cmd]; a batched [prep-go, prep-docs, go-cmd, docs-cmd] fails  [verification]
                 - two lanes declaring the IDENTICAL prepare line spawn it TWICE (locked
                   prepare_scope); a dedup cache fails on the count                  [verification+risk]
                 - transcript order by byte offset, not presence: index of the prepare line
                   < index of the `lane go: <line>` header < index of the lane's own first
                   output                                                            [risk+verification]
                 - `--local` against a granted-remote fixture records exactly
                   [prepare, command] on the local seam and NOTHING on the remote seam, and
                   the recorded prepare string equals the one carried inside the remote
                   script for the same fixture — a prepare wired only into the remote leg
                   fails the local half, a rebuilt-per-transport line fails the equality (c-5) [verification+risk]
                 - a lane whose selector filtered to nothing records NO prepare spawn, and
                   neither does a consent-refused lane — the recorder holds neither its
                   prepare nor its command (locked prepare_selector_miss)            [all three]
                 - a prepare beginning with `-` is refused by the UP-FRONT fence sweep
                   naming runtime.test_lane[go], with zero spawns of any kind; a fence
                   moved inside the loop leaves the earlier lane already run on a two-lane
                   fixture                                                           [all three]

  t-6  Add `dross test lane edit --prepare`                          [verification+risk+mvp]
       files:    internal/cmd/test_lane.go, internal/cmd/test_lane_edit_test.go
       covers:   c-6
       depends:  t-1, t-3
       contract: - after `dross test lane edit go --prepare "make build"` the reloaded
                   lane's Match slice and Command are byte-identical and only Prepare
                   differs (whole-struct compare); a remove-then-append implementation
                   reorders the block and fails the declaration-order assert against its
                   neighbour lane                                                    [verification+mvp]
                 - `--prepare ""` clears the key (no `prepare =` line survives), while
                   OMITTING --prepare entirely errors instead of rewriting the file — read
                   from cobra's Changed, so "not passed" and "passed empty" cannot collapse
                   into one clearing behaviour that silently drops an existing prepare [risk+verification]
                 - after an edit that changed the fingerprint, local.toml STILL holds a
                   trusted_lane_commands entry for that lane and `dross trust --lane go
                   --check` fails with the "has CHANGED" wording, not the "has not been
                   trusted" wording. Calling RevokeLaneConsent fails both halves      [all three]
                 - the edit prints `dross trust --lane go` when the fingerprint changed and
                   prints no trust instruction on a no-op re-set of the same value; an
                   unconditional message fails the no-op case                        [verification]
                 - `dross test lane edit nope --prepare x` errors listing the declared lane
                   names and leaves project.toml byte-identical                      [verification+risk]

Wave 4 (depends t-2, t-4)
  t-5  Fail a lane on its prepare, spare the rest                    [verification+risk]
       files:    internal/cmd/test.go, internal/cmd/test_lane_prepare_test.go
       covers:   c-3
       depends:  t-2, t-4
       contract: - the docs lane's prepare fails: docsCmd appears NOWHERE in the recorder,
                   goCmd does, and the run exits 7. A `return` instead of a `continue`
                   fails the goCmd assert; a missing skip fails the docsCmd assert    [verification+risk]
                 - the go lane's suite goes red AND the docs lane's prepare fails: exits 7,
                   not 1 — proving the result was folded through worseOutcome rather than
                   left to the last lane                                             [verification+mvp]
                 - a run where EVERY lane's prepare failed exits 7, NOT 5: prepare failures
                   must be counted apart from selector misses, or `misses ==
                   len(runnable)` returns exitNothingMeasured, which ranks LAST       [risk]
                 - the failure message names the lane and the prepare line and does not read
                   as `test lane %q failed`; a shared wrapper fails the substring assert
                   that the two kinds are distinguishable in the transcript          [verification]
                 - classified on BOTH transports: the local arm of runOneLane
                   (test.go:601) hardcodes exitSuiteFailed today, and a prepare failing over
                   ssh with a remote-command exit status exits 7, NOT the exitSuiteFailed
                   remoteFailure (test.go:793) hands back for the command leg        [verification, scope from risk]
                 - a TRANSPORT failure (ssh 255) during a prepare still exits 3 and an
                   incomplete rsync still exits 4: the re-tag sits inside the
                   remote-command arm only and must not swallow the transport family [verification+risk]
                 - a failed remote prepare leaves NO ssh invocation carrying that lane's
                   test line: the recorded scripts hold rsync and the prepare only    [risk]

Wave 5 (depends t-2, t-5, t-6)
  t-7  Document prepare, exit 7 and the narrowed lane edit           [verification+mvp+risk]
       files:    README.md, assets/prompts/execute.md, assets/prompts/options.md,
                 internal/cmd/options_docs_test.go, internal/cmd/execute_prompt_test.go
       covers:   c-1, c-3, c-6
       depends:  t-2, t-5, t-6
       contract: - TestReadmeDocumentsTestLanes (options_docs_test.go:188) gains `--prepare`
                   and `dross test lane edit`; deleting either from README's lane row fails
                   it, and the `dross test` row must name exit `7` and place it between
                   partial and red in the stated precedence order                    [mvp+risk]
                 - a new assertion in execute_prompt_test.go fails if execute.md carries no
                   **7** entry, or if line 184's "**2, 3, 4, 5 and 6 all mean the run did
                   not happen**" still omits 7 — an agent that reads 7 as a red suite goes
                   hunting a bug in code that never ran                              [mvp+verification]
                 - TestOptionsDocumentsTheSelectorSurfaceCorrectly still passes: options.md
                   KEEPS "remove-then-re-add" for match, command, selector and empty_exit
                   while naming `dross test lane edit --prepare`, and every `--flag` in its
                   `dross test lane add` examples is one testLaneAdd() registers — the gate
                   cross-checks doc flags against the live cobra flag set            [mvp]
```

Coverage: c-1 → t-1, t-7 · c-2 → t-4 · c-3 → t-2, t-5, t-7 · c-4 → t-3 · c-5 → t-4 · c-6 → t-6, t-7. 6/6.

## Disagreements

**D1 — How the run wiring is cut.** mvp puts the exit constant, its rank, the fence, the spawn, the failure classification and both transports in one task (t-3). verification splits spawn (t-4) from failure classification (t-5) and lifts the taxonomy out to wave 1 (t-2). risk splits local-run (t-6) from a remote re-tag (t-7). *Default: verification's cut.* It matters because "the other lanes still run" is not assertable until a prepare spawns at all, and because a single commit carrying the constant, its only consumer and its failure path leaves the rank order proved only indirectly by an end-to-end run.

**D2 — Which transports the failure re-tag covers.** risk scopes its dedicated re-tag task to the remote leg (t-7), leaving local classification implicit inside t-6. verification classifies both in t-5. *Default: both transports, one task.* `internal/cmd/test.go:601` shows `runOneLane`'s local arm returning `&ExitCodeError{Code: exitSuiteFailed, Err: "test lane %q failed"}` — a remote-only re-tag ships a local prepare failure still reported as exit 1, which is exactly the false-green exit 7 was locked to prevent.

**D3 — The fingerprint encoding.** risk wants hash-of-hashes behind a NUL-separated domain tag (fixed-length by construction, NUL unreachable from TOML). mvp and verification want a length-prefixed / length-framed encoding. *Default: length-framed, 2-of-3.* Both defeat the re-split collision the `prepare_consent` lock names, so this is an implementation choice, not a contract one — but risk's *unforgeability* assertion (no framed pair can produce a bare-command fingerprint) is grafted in regardless, because the two-encoding scheme both defaults require is only safe if that separation is proved rather than assumed.

**D4 — Whether `dross validate` gets a prepare rule.** risk and verification add one; mvp rejects it as untraceable to any criterion. *Default: add it, in risk's framing only.* verification's version targets a hand-edited `prepare = ""`, which `omitempty` makes indistinguishable from an absent key after `project.Load` — that assert cannot fail. risk's whitespace-only `prepare = "   "` survives the load non-empty, so it fingerprints as a prepare and reads as none: the one shape worth a rule. If the executing agent finds even that untraceable, dropping it costs no criterion.

**D5 — Where the docs task sits.** mvp puts it in wave 2, arguing exit 7 and its rank are *locked in the spec*, not discovered by the run work, so the prose needs only t-1's registered flags. risk and verification put it last. *Default: last wave (wave 5).* Tasks commit atomically here, and a wave-2 docs commit would have README and options.md advertising `dross test lane edit --prepare` before t-6 registers the verb — a doc that sends the user to a refusal, which is the failure `TestOptionsDocumentsTheSelectorSurfaceCorrectly` already exists to catch. mvp's reasoning is sound on dependencies and wrong on commit granularity; if the phase is run with waves collapsed, moving t-7 earlier is safe.

**D6 — Which doc surfaces are in scope.** risk touches README only. verification adds `assets/prompts/execute.md`. mvp adds both plus `assets/prompts/options.md`. *Default: all three (mvp).* options.md is not optional here: its live gate asserts the prompt still says "remove-then-re-add", and the `lane_edit_surface` lock supersedes that guidance *for the prepare field only* — so the prompt must now say both things, and only mvp noticed.

**D7 — Whether doctor gets its own task.** risk gives doctor + README a joint task (t-8). verification folds doctor's prepare row into the fingerprint task (t-3) and docs into t-7. *Default: doctor lives in t-3.* Doctor's row is a read of the same encoding `LaneConsented` uses; splitting it means the row is written against an encoding it does not share a commit with. risk's stale-row contract (doctor still names `dross trust --lane <name>` when only the prepare changed) is grafted into t-3.

**D8 — Wave-2 concurrency on `internal/cmd/test.go`.** verification placed t-3 and t-4 in the same wave editing different hunks of the same file, and flagged it for this judge. *Default: sequenced — t-3 alone in wave 2, t-4 in wave 3.* This is what turns verification's 4 waves into 5. It costs one wave of parallelism and removes a merge conflict on a 800-line file mid-phase; t-6 (`test_lane.go`) rides along in wave 3 to recover the parallelism.
