# memory-prune-advisor — panel synthesis

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| risk (11 tasks / 4 waves) | 9/9, each with a library and a surface task. c-6 gate matches the recorded "turns ungated" rule in TestFlowCostRecords | Strongest. Every contract names its test and a concrete failure mode (FIFO, outside-dir links, 5 MB line, stale ID, concurrent index edit, symlink target) | Good, one surface per task. Renders output twice, once in each cmd surface | Mostly sound. Two problems: t-11 (make install + measure) runs in parallel with t-10 (apply), so the after-leg binary can predate the final code. And no task registers new writers in secretscan.Writers, so the writer enum test goes red |
| mvp (5 tasks / 4 waves) | 9/9 on paper, but c-4/c-5/c-7/c-8 sit in one task and c-9 is bundled with advise. Gates c-6 on `after.turns == before.turns`, which TestFlowCostRecords' doc comment rejects (rivil, 2026-10-08) | Weakest. Contracts are mostly unnamed assertions with few failure-mode probes | Too coarse. t-1 is four rules in one file, t-3 is advise + apply + gather | Sound but serial: status waits on cmd/memory.go's gather. Only draft that registers writers in internal/secretscan/writers.go and syncs the README |
| verification (10 tasks / 5 waves) | 9/9 with redundant cover. Claims c-6 on the turns task, which is a stretch | Very high. Named tests, exact boundaries (49/50%, 23h/25h, 40-byte reword delta), cold-cache scenario | Good. Shared Gather/Render in memadvise feeds both surfaces | Clean wave split. Reuses the real machinery (flow-cost.toml picked up by TestFlowCostRecords' glob, checkLegPair, status_prompt_test.go). Misses the writer registry. Edits tests_before.txt for no reason. Gates on agent_turns |

**Skeleton: verification.** It has the cleanest wave graph and the only shared Gather/Render layer, and it is the only draft that plugs c-6 into the existing `TestFlowCostRecords` / `checkLegPair` machinery instead of building a parallel `status-cost.toml` test. I grafted in risk's sharper hazard contracts, its corpus/locate split, its incremental recount and its c-6 gate semantics. From mvp I took the secretscan writer registration (without it the plan fails `secretscan_writer_enum_test.go`) and the README rows.

Repo facts checked while merging:
- `boundary_test.go` bans os/exec, net/http, go/ast, encoding/json and BurntSushi/toml in non-test internal/cmd files.
- `secretscan.Writers()` requires one entry per write-verb file with exactly one disposition, and the enum test walks all of internal/.
- `drossIgnoreEntries` and `TestEnsureGitignoreCoversLocalToml` exist.
- `phase.Done`, `phase.DirExists` and `gitrun.Raw` exist.
- `remote.SyncArgs` excludes git-ignored paths from the helicon rsync.
- `TestNoTestLost` treats new test names as free.
- No hook invokes `dross status`; hooks.go has no status call.
- Top-level transcripts modified in the last 30 days total about 1.1 GB.

## Merged plan

```
Phase memory-prune-advisor — 11 tasks across 5 waves

Wave 1
  t-1  Locate the repo's Claude memory dir                                    [risk+verification]
       files:    internal/memadvise/locate.go (new), internal/memadvise/paths.go (new),
                 internal/memadvise/locate_test.go (new)
       covers:   c-1
       depends:  —
       desc:     ClaudeDir() = $CLAUDE_CONFIG_DIR else ~/.claude. Slug(repo) = the EvalSymlinks'd repo
                 top-level path with every non-[A-Za-z0-9] byte mapped to '-'. Locate returns (dir, ok).
                 ok=false when the dir is missing, is not a dir, or memory/ is a symlink. paths.go holds
                 the .dross-relative constants for advisor state: .dross/memory/turns.json and
                 .dross/memory/snapshots/ (see Disagreement D4).
       contract: - Slug("/Users/a/.claude/x") must be "-Users-a--claude-x", and '_' and ' ' must map to '-' too;
                   otherwise TestSlugReplacesEveryNonAlnum fails
                 - a repo reached through /tmp → /private/tmp slugs to "-private-tmp-…";
                   otherwise TestSlugResolvesSymlinks fails
                 - with CLAUDE_CONFIG_DIR set, the dir is under it and ~/.claude is never stat'd;
                   otherwise TestLocateHonorsClaudeConfigDir fails
                 - a missing dir, a regular file at that path, or a symlinked memory/ returns ok=false
                   and a nil error; TestLocateMissingIsSilent fails on any error or on ok=true

  t-2  Parse memory corpus and index integrity                                [risk+verification]
       files:    internal/memadvise/index.go (new), internal/memadvise/index_test.go (new),
                 internal/memadvise/testdata/corpus/ (new)
       covers:   c-5
       depends:  —
       desc:     Load(dir) reads MEMORY.md and its direct-child *.md files: regular files only, O_NONBLOCK,
                 size-capped. It parses index lines ([Title](file.md) — hook), frontmatter name:/description:,
                 bodies, and [[slug]] links with inline code stripped. Integrity() returns DeadLinks (index
                 target missing, outside the dir, a symlink, or not .md), Orphans (unindexed *.md other than
                 MEMORY.md), Inbound[slug] (distinct linkers, self-links excluded) and ambiguous duplicate
                 name: slugs.
       contract: - an index line naming gone.md is in DeadLinks, otherwise TestDeadIndexLink fails;
                   "../../.ssh/id.md" or "/etc/x.md" is reported dead and never opened, otherwise
                   TestIndexLinkOutsideDirIsDead fails
                 - extra.md, unindexed, is an orphan and MEMORY.md never is; otherwise TestOrphans fails
                 - [[a-thing]] resolves to a_thing_v2.md through its frontmatter `name: a-thing`;
                   resolving by filename fails TestInboundResolvesByFrontmatterName
                 - a [[x]] inside backticks, or a self-link, adds no inbound;
                   otherwise TestInboundIgnoresCodeSpansAndSelf fails
                 - two files declaring one name: slug are both reported, and neither gets inbound credit;
                   otherwise TestDuplicateNameSlugReported fails
                 - a FIFO named foo.md is skipped within 1s, otherwise TestCorpusSkipsFIFO fails;
                   files with no frontmatter or CRLF frontmatter still parse, otherwise
                   TestCorpusToleratesMissingAndCRLFFrontmatter fails

  t-3  Guard phrases and doc-duplication shingles                             [verification+risk]
       files:    internal/memadvise/guard.go (new), internal/memadvise/dup.go (new),
                 internal/memadvise/guard_test.go (new), internal/memadvise/dup_test.go (new)
       covers:   c-4, c-7
       depends:  —
       desc:     Guarded(file) checks the whole file, frontmatter included (D11). It matches NEVER, ASK,
                 DECOY and REOPENED as case-sensitive whole words, and "don't re-raise" / "don't revert"
                 case-insensitively with ' or ’. DocCandidates(tracked) keeps CLAUDE.md, ARCHITECTURE.md,
                 README* and docs/**/*.md. Duplicates(body, docs) normalises text (lowercase, alnum-only,
                 collapsed whitespace) and returns the single doc that holds ≥50% of the body's 8-word
                 shingle set.
       contract: - each of the six phrases guards; otherwise its row in TestGuardPhrases fails.
                   "TASK", "asked", "I'll ask later", "NEVERTHELESS" and "Nevermind" do not guard;
                   otherwise TestGuardWholeWordOnly fails
                 - "don’t revert" (U+2019) and "Don't re-raise" both guard; otherwise TestGuardApostropheVariants fails
                 - exactly 50% of shingles in one doc is a duplicate and 49% is not;
                   otherwise TestDupThresholdBoundary fails
                 - 30% in docs/a.md + 30% in docs/b.md is not a duplicate; otherwise TestDupNeedsOneDoc fails
                 - a paragraph-reordered restatement still matches, otherwise TestDupSurvivesReorder fails;
                   the result is the doc's repo-relative path, otherwise TestDupNamesDoc fails
                 - a body under 8 words never divides by zero and never matches; otherwise TestDupShortBody fails
                 - docs/x.txt and src/foo.md are dropped; otherwise TestDocCandidates fails

  t-4  Expired hook clause detection and reword                               [verification+risk]
       files:    internal/memadvise/hook.go (new), internal/memadvise/hook_test.go (new)
       covers:   c-8
       depends:  —
       desc:     Expired(hook, today, phaseKnown) finds a time-bound clause: a YYYY-MM-DD directly after
                 until/by/before/due/expires/till/deadline, or next[:] [phase |/dross-<verb> ]<slug>.
                 The clause is expired when the date is strictly before today or phaseKnown(slug) is true.
                 It returns the hook with only that clause and its separator removed.
       contract: - "until 2026-10-29" is expired with today 2026-10-30, and "until 2026-10-30" is not;
                   otherwise TestExpiryBoundaryIsStrict fails
                 - "RESOLVED 2026-09-19 (03983fe)", "shipped 2026-10-04" and "released 2026-01-01" are not
                   time-bound; otherwise TestHistoricalDateIsNotTimeBound fails
                 - "next: foo" expires only when phaseKnown("foo") is true; otherwise TestNextPhaseClause fails.
                   "…; next /dross-spec memory-prune-advisor" parses; otherwise TestNextSlashCommandForm fails
                 - "X — done; next phase foo" rewords to "X — done", with no dangling "; " or " — " and no
                   other byte changed; otherwise TestRewordRemovesOnlyClause fails
                 - a hook that is nothing but the clause yields a non-empty fallback;
                   otherwise TestRewordNeverEmpty fails

  t-5  Count repo turns with a 24h incremental cache                          [verification+risk+mvp]
       files:    internal/memadvise/turns.go (new), internal/memadvise/turncache.go (new),
                 internal/memadvise/turns_test.go (new), internal/memadvise/turncache_test.go (new),
                 internal/cmd/gitignore.go, internal/cmd/gitignore_test.go, .gitignore,
                 internal/secretscan/writers.go
       covers:   c-2
       depends:  —
       desc:     Turns(projectsDir, slug, now) streams top-level <slug>/*.jsonl with bufio.Reader, so there is
                 no Scanner cap. subagents/ dirs are skipped, and so are files whose mtime is older than
                 30 days. A turn is a distinct message.id per file with type=assistant, isSidechain=false
                 and model≠"<synthetic>", timestamped within 30 days. Counts are per project plus the
                 all-projects total. The cache lives at .dross/memory/turns.json and keeps per-file
                 {size, mtime, offset, day buckets}. It is reused under 24h. After 24h only appended bytes
                 are rescanned, and a file truncated below its offset is recounted from 0. Writes are atomic.
                 The function returns counts and cache age. Seed `.dross/memory/` in drossIgnoreEntries and
                 the repo .gitignore. Register turncache.go as MachineLocal (Path memory/turns.json,
                 IgnoreSeed = the dross seed).
       contract: - three lines sharing one message.id count 1; otherwise TestTurnsDedupeMessageID fails
                 - isSidechain:true, "<synthetic>", user lines and subagents/ files add 0;
                   otherwise TestTurnsMainAgentOnly fails
                 - a turn dated 31 days ago adds 0; otherwise TestTurnsWindow fails
                 - a chmod-000 transcript with a 31-day-old mtime is never opened;
                   otherwise TestTurnsSkipsStaleFilesByMtime fails
                 - a 5 MB single-line tool result between two assistant turns yields 2;
                   otherwise TestTurnsSurviveMultiMegabyteLine fails
                 - a malformed line is skipped, not fatal; otherwise TestTurnsSkipMalformedLine fails
                 - RepoShare = repo/all across two fixture project dirs; otherwise TestTurnsShareAcrossProjects fails
                 - with a 23h-old cache, appended turns don't show and age is 23h, otherwise
                   TestCacheReusedUnder24h fails; at 25h they show and the cache is rewritten, otherwise
                   TestCacheRecountedAt24h fails
                 - a corrupt or future-dated cache recounts; otherwise TestCacheRecountsOnCorruptOrFuture fails
                 - after one appended turn, the recount reads only the appended bytes (old+1), and a truncated
                   file recounts from 0; otherwise TestIncrementalRecountAndTruncation fails
                 - the cache path is under <root>/.dross/memory/ and never under the memory dir;
                   otherwise TestCacheNeverInMemoryDir fails
                 - `.dross/memory/` missing from drossIgnoreEntries or from the repo .gitignore fails
                   TestEnsureGitignoreCoversMemoryState. The ignore path comes from the memadvise constants,
                   so renaming one side alone fails TestIgnoreEntriesMatchAdvisorPaths
                 - leaving turncache.go out of secretscan.Writers() fails the existing writer enum test,
                   and a bad seed fails its git check-ignore arm

Wave 2 (depends t-1..t-5)
  t-6  Build suggestions: cost, ranking, stable ids                           [verification+risk]
       files:    internal/memadvise/advise.go (new), internal/memadvise/cost.go (new),
                 internal/memadvise/advise_test.go (new), internal/memadvise/cost_test.go (new)
       covers:   c-1, c-2, c-4, c-5, c-7, c-8
       depends:  t-1, t-2, t-3, t-4, t-5
       desc:     Advise(mem, docs, phaseKnown, turns, today) returns []Suggestion{ID, File, Reason,
                 Action (delete|reword hook), NewHook, Doc, Inbound, BytesSaved, MonthlyUSD}, at most one
                 per memory. Precedence (D1): an expired clause means reword, and that beats a duplicate
                 delete. A guarded memory never gets a delete. Integrity issues are list items (D3): a dead
                 link deletes its index line; an orphan deletes its file unless guarded. ID = action + file
                 stem (D2). cost.go: tokens = ceil(bytes/4); USD = tokens × repo turns ×
                 CacheReadUSDPerMTok / 1e6, with one rate constant (D7). Share = repo/all, display only.
                 Sort by USD desc, then ID.
       contract: - 400 bytes × 1000 turns × rate R is exactly 100×1000×R/1e6, and 0 turns is $0. Adding recall
                   tokens, using all-project turns, dividing by share, or a 1e3/1e6 slip fails TestMonthlyCostFormula
                 - a reword from 120 to 80 bytes is priced on 40 bytes; otherwise TestRewordPricesOnlyDelta fails
                 - a guarded memory that duplicates README.md gets no delete, and a guarded orphan gets no delete;
                   otherwise TestGuardBlocksEveryDeleteSource fails
                 - hooks/bodies holding only DEAD/done/RESOLVED 2026-09-19/DECLINED/SHIPPED yield zero
                   suggestions; otherwise TestStatusWordsAloneSuggestNothing fails
                 - an expired-hook memory, duplicate or not, gets exactly one suggestion with action reword hook;
                   otherwise TestExpiredHookNeverDeletes fails
                 - a memory with two inbound [[links]] has Inbound naming both and a Reason saying "linked from";
                   otherwise TestSuggestionNamesInboundLinks fails
                 - a delete's Doc is the duplicated doc's path; otherwise TestDeleteNamesDoc fails
                 - a dead index link and an orphan both appear as items; otherwise TestIntegrityItemsCountTowardList fails
                 - IDs match across runs and survive an unrelated memory added to the top of MEMORY.md;
                   otherwise TestSuggestionIDsStable fails
                 - an all-projects total of 0 gives share "n/a", not NaN or a panic; otherwise TestShareZeroTotal fails
                 - a missing dir, a missing MEMORY.md or an empty dir yields an empty slice and a nil error;
                   otherwise TestAdviseMissingDirOrIndexIsEmpty fails

Wave 3 (both depend t-6)
  t-7  Gather inputs and render advice lines                                  [verification+risk]
       files:    internal/memadvise/gather.go (new), internal/memadvise/render.go (new),
                 internal/memadvise/render_test.go (new)
       covers:   c-1, c-2, c-3, c-7
       depends:  t-6
       desc:     Gather(root, now) resolves the memory dir and lists tracked docs via gitrun.Raw("ls-files", "-z", …).
                 A non-git root means no docs and no error. It builds phaseKnown from phase.DirExists ||
                 phase.Done and loads turns. Render(report, limit) prints one line per item (id, file,
                 action, reason, $X.XX/mo), then an integrity block, a share line ("this repo: N of M
                 assistant turns (P%) in 30d · turns counted <age> ago") and, when the list is truncated,
                 "+N more — dross memory advise". It never prints memory bodies.
       contract: - every item line has its ID, file, action word and "$X.XX/mo"; otherwise TestRenderSuggestionLine fails
                 - the share line has repo turns, all turns, the percent and the cache age;
                   otherwise TestRenderShareAndCacheAge fails
                 - limit 3 over 5 items renders 3 plus "+2 more — dross memory advise", and 3 over 3 renders no
                   "+0 more"; otherwise TestRenderTruncation fails
                 - zero items or a missing dir renders zero bytes; otherwise TestRenderEmptyIsSilent fails
                 - a sentinel that appears only in a memory body never shows in rendered output;
                   otherwise TestRenderNeverPrintsBodies fails
                 - an untracked docs/x.md identical to a memory produces no delete;
                   otherwise TestGatherUsesTrackedDocsOnly fails
                 - the sha256 over (path, mode, bytes) of the memory dir is unchanged even when a stale cache
                   is rewritten; otherwise TestGatherNeverWritesMemoryDir fails

  t-8  Apply one suggestion with snapshot and recheck                         [verification+risk+mvp]
       files:    internal/memadvise/apply.go (new), internal/memadvise/snapshot.go (new),
                 internal/memadvise/apply_test.go (new), internal/secretscan/writers.go
       covers:   c-9, c-3
       depends:  t-6
       desc:     Apply(memDir, snapDir, s) first copies every file in the memory dir to
                 snapDir/<UTC ts>/ (snapshot.go) and aborts if that fails. It re-checks Guarded on the live
                 file before any delete. It touches only a direct-child regular non-symlink *.md. MEMORY.md
                 is rewritten atomically (temp + rename, CRLF and trailing newline kept) and the rewrite
                 aborts if the file's hash changed since the plan. A delete then removes the file. Afterwards
                 Integrity is re-run and dead links and orphans are returned. Register snapshot.go as
                 MachineLocal (memory/snapshots/<name>) and apply.go as OutsideDross (the memory dir).
                 They are split because Writers() allows one disposition per file.
       contract: - after a delete, the file and its one MEMORY.md line are gone and every other byte is unchanged;
                   otherwise TestApplyDeleteRemovesFileAndLine fails
                 - after a reword, only that line differs and it equals NewHook; otherwise TestApplyRewordTouchesOneLine fails
                 - the snapshot hashes identical to the pre-apply dir; otherwise TestApplySnapshotsFirst fails
                 - a snapshot failure aborts with the dir untouched; otherwise TestApplyAbortsWhenSnapshotFails fails
                 - a body that gained "DECOY" after advise is refused for delete and the dir hash is unchanged;
                   otherwise TestApplyRefusesGuardedDelete fails
                 - a dead-link target that is a symlink outside the dir is never followed or deleted (only
                   the index line goes); otherwise TestApplyNeverDeletesOutsideDir fails
                 - MEMORY.md edited between plan and rename aborts with it intact;
                   otherwise TestApplyAbortsOnConcurrentIndexEdit fails
                 - deleting a memory linked via [[name]] returns that dangling link, and a pre-existing orphan
                   is reported; otherwise TestApplyRechecksIntegrity fails
                 - apply.go or snapshot.go left out of Writers(), or one file with two dispositions,
                   fails the writer enum test

Wave 4 (t-9 depends t-7; t-10 depends t-7, t-8)
  t-9  Add memory: section to dross status                                    [verification+risk+mvp]
       files:    internal/cmd/status.go, internal/cmd/status_memory_test.go (new),
                 internal/cmd/status_prompt_test.go
       covers:   c-1, c-2, c-3
       depends:  t-7
       desc:     After the debug: line and before the reentry footer, status prints `memory:` followed by
                 Render(report, 3). A Gather error, a missing dir or zero items prints nothing, and status
                 still exits 0. status.md is unchanged (D10).
       contract: - with a 5-item fixture, status shows "memory:", the 3 highest-$ lines in order, and
                   "+2 more — dross memory advise"; otherwise TestStatusMemoryTopThree fails
                 - no memory dir, or nothing to suggest, gives output byte-identical to the advisor-off output;
                   otherwise TestStatusMemorySilentWhenNothing fails
                 - the memory dir sha256 is unchanged across `dross status`; otherwise TestStatusNeverWritesMemoryDir fails
                 - an unreadable MEMORY.md or an Advise error exits 0 with no memory: line;
                   otherwise TestStatusMemoryErrorIsSilent fails
                 - the reentry line stays last with the block present; otherwise TestStatusFooterStillLast fails
                 - status.md gaining any `dross memory` invocation fails TestStatusPromptAddsNoMemoryCall

  t-10 Add dross memory advise/apply commands                                 [verification+risk]
       files:    internal/cmd/memory.go (new), internal/cmd/memory_test.go (new), cmd/dross/main.go
       covers:   c-1, c-2, c-3, c-5, c-9
       depends:  t-7, t-8
       desc:     `dross memory advise` prints Render(report, all) with the integrity block, share and cache age.
                 A missing dir is silent with exit 0; a count/IO error is loud and non-zero. `dross memory
                 apply <id> --yes` re-gathers, looks the id up among the current suggestions, applies through
                 t-8 with snapshots under .dross/memory/snapshots/, and prints the snapshot path and the recheck.
                 The group is registered in main.go.
       contract: - `apply <id>` without --yes exits non-zero, names --yes, and leaves the dir hash unchanged;
                   otherwise TestApplyRefusesWithoutConfirm fails
                 - an id not among the current suggestions (stale or a typo) exits non-zero and touches nothing;
                   otherwise TestApplyUnknownID fails
                 - with 3 suggestions, applying one leaves the other two files and every other MEMORY.md byte
                   untouched; otherwise TestApplyChangesExactlyOne fails
                 - apply --yes on a delete removes exactly that file, prints a snapshot path that exists under
                   .dross/memory/snapshots/, and prints the recheck; otherwise TestApplyEndToEnd fails
                 - advise leaves the dir hash unchanged and lists every item, not just 3;
                   otherwise TestAdviseListsAllReadOnly fails
                 - advise with no memory dir prints nothing and exits 0; otherwise TestAdviseMissingDirSilent fails
                 - advise with an unreadable transcripts dir exits non-zero naming the path;
                   otherwise TestAdviseSurfacesCountError fails
                 - `dross memory` absent from main.go fails TestMemoryCommandRegistered

Wave 5 (depends t-9, t-10)
  t-11 Record before/after /dross-status flow cost; document dross memory      [verification+risk+mvp]
       files:    .dross/phases/memory-prune-advisor/flow-cost.toml (new),
                 internal/cmd/flowcost_record_test.go, README.md
       covers:   c-6
       depends:  t-9, t-10
       desc:     After `make install` (r-01), measure one /dross-status on a base-commit build ([before]) and one
                 on the phase head with a cold turn cache ([after]). Record agent_turns, turn_tools,
                 wall_clock_s, dross_calls and a new status_bytes field, all counted structurally from the
                 transcript with the jq method recorded. Add checkMemoryPruneAdvisorFlow and add the phase to
                 the required list. Add README rows for `dross memory advise|apply` and the status memory: line.
       contract: - the existing checkLegPair fails if [after] runs a dross call [before] did not (the zero-round-trip
                   gate) or both legs share a binary_version
                 - checkMemoryPruneAdvisorFlow fails if status_bytes ≤ 0 on either leg or [scenario] lacks
                   turn_cache = "cold". agent_turns and wall_clock_s are recorded and required non-zero, but
                   they are not compared (D8)
                 - a string value outside the declared keys (binary_version, session_id, measured_at, filter, method)
                   fails, so no transcript or memory text is tracked; otherwise TestFlowCostCopiesNoContent fails
                 - deleting flow-cost.toml fails the "memory-prune-advisor has no flow-cost.toml" check
                 - README.md missing `dross memory apply` or `--yes` fails a README grep test
```

Coverage: c-1 t-1/t-6/t-7/t-9/t-10 · c-2 t-5/t-6/t-7/t-9/t-10 · c-3 t-7/t-8/t-9/t-10 · c-4 t-3/t-6 · c-5 t-2/t-6/t-10 · c-6 t-9 (prompt pin)/t-11 · c-7 t-3/t-6/t-7 · c-8 t-4/t-6 · c-9 t-8/t-10. All 9 of 9 are covered.

## Disagreements

**D1 — Expired hook on a memory that also duplicates a doc.**
- risk: reword wins and the memory gets one suggestion. It reads c-8's "never a delete" literally.
- mvp: delete wins unless guarded.
- verification: c-7's delete stands. The clause itself never causes a delete, and the redundant reword is dropped.
- Provisional: risk. c-8 says such a hook "gets a reword-hook suggestion, never a delete", and losing a little saving is the safe direction.
- Why it matters: this is 1 draft against 2. c-7 and c-8 conflict on this overlap and the spec doesn't break the tie, so a user call settles it permanently. The other two drafts would accept a delete of a memory whose hook is merely stale.

**D2 — Suggestion ID scheme.**
- risk: a content hash of kind+file+index line. Any edit makes the ID stale.
- mvp: the file stem, typable.
- verification: action + file stem, with apply re-gathering and requiring the id to still be suggested.
- Provisional: verification's action+stem. It is typable and stable across reorder. To keep it safe I added risk's safeguards: the live guard re-check, the MEMORY.md-hash abort and the unknown-id refusal.
- Why it matters: under stem IDs, if the hook was edited between advise and apply, apply runs the recomputed suggestion, not the one the user read. A hash ID refuses that case instead.

**D3 — How integrity issues enter the list.**
- risk: they are ordinary appliable suggestions. A dead link is a delete of the index line; an orphan is a delete of the file unless guarded.
- verification: they are `fix-index` items, ranked but refused by apply.
- mvp: lists "suggestions/integrity issues" without saying which action they get.
- Provisional: risk. c-1 fixes the action vocabulary to "delete or reword hook", and verification's third action word is outside it.
- Why it matters: deleting an orphan saves $0 under the cost model and destroys a file the user may just have forgotten to index. Verification's non-appliable items are the safer reading if the user ranks safety above c-1's literal wording.

**D4 — Where advisor state (turn cache, snapshots) lives.**
- risk: `.dross/memory-turns.json` plus `.dross/memory-snapshots/`, two ignore entries in drossIgnoreEntries, and its own gitignore task.
- verification: one `.dross/memory/` dir seeded in drossIgnoreEntries.
- mvp: the cache in a self-ignoring `.dross/memory/` (the debugsession/runlog pattern) as TOML, and snapshots at `~/.claude/projects/<slug>/memory-snapshots/`, outside the repo.
- Provisional: verification's single `.dross/memory/`, folded into t-5. Risk's separate gitignore task is dropped, but its constants-match contract is kept.
- Why it matters:
  - mvp's reason, keeping memory text off the helicon rsync, does not hold. `remote.SyncArgs` asks git for ignored paths and excludes them.
  - mvp's location does avoid a real constraint, though. `secretscan.Writers()` allows one disposition per file, and apply writes both a .dross snapshot (MachineLocal) and the memory dir (OutsideDross). Under the default those writes must be split into snapshot.go and apply.go (t-8). Under mvp's location one OutsideDross entry covers both.

**D5 — Transcript recount strategy.**
- mvp: a full rescan when the cache passes 24h.
- verification: the same, but skips files whose mtime is older than 30 days.
- risk: the mtime skip plus per-file {size, mtime, offset} incremental recount.
- Provisional: risk's incremental recount, grafted into t-5. Measured on this machine, top-level transcripts modified in the last 30 days total about 1.1 GB, so the mtime skip alone still re-reads about 1.1 GB inside `dross status` once a day.
- Why it matters:
  - It costs one extra file and three contracts, and it decides whether c-6's cold-cache after-leg (verification's scenario) shows a multi-second status run.
  - Risk's supporting claim that status is a hook target is false: hooks.go never invokes `dross status`. The wall-clock argument stands without it.

**D6 — Granularity.**
- mvp: 5 tasks (four rules in one task; advise+apply+gather in one).
- risk: 11. verification: 10.
- Provisional: 11 fine-grained tasks. Risk's locate/corpus split is kept, and verification's guard+dup / hook split replaces risk's guard+hook / dup split.
- Why it matters: mvp's t-1 and t-3 each fold four or more criteria into one review unit, which hides a failing rule behind passing ones in solo review.

**D7 — Pricing.**
- risk: per-model turn buckets and rates. Unknown models are left out of $ but kept in the share.
- mvp and verification: one CacheReadUSDPerMTok constant.
- Provisional: one constant. The locked cost_model says "the cache-read $/MTok rate", singular.
- Why it matters: mixed-model months are misprice-prone under a flat rate. Per-model buckets would also change the cache schema, so this is cheaper to decide now than to retrofit.

**D8 — The c-6 gate.**
- risk: the gate is the dross_calls set only; raw turns are recorded, not gated.
- mvp: gates `after.turns == before.turns`.
- verification: calls set, plus failing if `after.agent_turns > before.agent_turns`, plus a cold cache and status_bytes > 0.
- Provisional: risk's semantics, run inside verification's machinery.
- Why it matters: the doc comment on `TestFlowCostRecords` records the decision "Turns and wall-clock stay recorded but ungated … (rivil, 2026-10-08)". Gating turns would make c-6 flake on agent batching, and the user has already rejected that.

**D9 — The measurement record's home.**
- risk and mvp: a new `status-cost.toml` with a new test file.
- verification: `flow-cost.toml`, picked up by the existing `TestFlowCostRecords` glob and `checkLegPair`, plus a per-phase checker and a required-list entry.
- Provisional: verification.
- Why it matters: a parallel file escapes the v1.8 milestone's shared c-6 record. Every other v1.8 phase (context-boundaries, test-output-summaries, prompt-partials) uses flow-cost.toml.

**D10 — status.md.**
- risk: adds a follow-up route to `dross memory advise`, never a step that runs it.
- mvp and verification: status.md stays unchanged, and verification pins that with a prompt test.
- Provisional: unchanged and pinned (TestStatusPromptAddsNoMemoryCall). The "+N more — dross memory advise" line in status output already routes the user there.
- Why it matters: any prompt edit risks a model treating the route as an instruction, which would add a round-trip and break c-6.

**D11 — Guard-phrase scope.**
- risk: the whole file, frontmatter included.
- verification and mvp: the body, as c-4 says.
- Provisional: the whole file.
- Why it matters: it only ever adds protection, but it means a guard phrase in a description: line also blocks deletion. That goes beyond c-4's literal "body".

**D12 — Items only one draft carries.** Each was checked against the repo:
- (a) mvp's secretscan writer registration is **kept** in t-5 and t-8. The enum test in `internal/cmd/secretscan_writer_enum_test.go` fails on any unregistered write-verb file under internal/, and risk and verification both omit it.
- (b) verification's `internal/cmd/testdata/cli_surface/tests_before.txt` edit is **dropped**. That file is the pre-phase inventory, and `TestNoTestLost` says "New names are free".
- (c) mvp's README rows are **kept** in t-11, following this repo's README-sync convention. No criterion asks for them.
- (d) Risk's measurement task ran in parallel with apply. It has been **moved** to a final wave behind both surfaces, so the measured binary is the finished one.
