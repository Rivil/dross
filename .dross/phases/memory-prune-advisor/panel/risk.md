# memory-prune-advisor — risk lens draft

Lens: every task owns the failure modes of exactly one surface, and each test
contract names the way that surface breaks. All parsing, JSON and hashing lives in
a new library package `internal/memadvise/`. boundary_test.go bans
encoding/json, BurntSushi/toml, os/exec and cobra-outside-cmd in non-test
internal/cmd files, so internal/cmd only formats and wires.

```
Phase memory-prune-advisor — 11 tasks across 4 waves

Wave 1
  t-1  Locate the repo's Claude memory dir
       files:    internal/memadvise/locate.go (new), internal/memadvise/paths.go (new),
                 internal/memadvise/locate_test.go (new)
       covers:   c-1
       depends:  —
       desc:     Slug = EvalSymlinks(repo top-level, i.e. Dir of the .dross root), every
                 non-[A-Za-z0-9] byte → '-' (matches observed "-Users-rivil--claude-jobs-…"
                 and "-private-tmp-…"). Base = $CLAUDE_CONFIG_DIR else ~/.claude.
                 Returns (dir, ok); ok=false when missing, not a dir, or a symlink.
                 paths.go holds the constants for the turn-cache file
                 (.dross/memory-turns.json) and the snapshot dir (.dross/memory-snapshots/).
       contract: - a repo at a path with '.', '_' or ' ' (e.g. /x/.claude/a_b c) slugs to
                   "-x--claude-a-b-c"; if any of those stay literal, TestSlugReplacesNonAlnum fails
                 - a repo reached through a symlinked parent (macOS /tmp → /private/tmp) resolves
                   to the "-private-tmp-…" slug; TestSlugResolvesSymlinks fails otherwise
                 - with CLAUDE_CONFIG_DIR set, the dir is under it and ~/.claude is never stat'd;
                   TestLocateHonorsClaudeConfigDir fails otherwise
                 - a missing dir, a regular file at that path, or a memory/ that is a symlink
                   returns ok=false with nil error; TestLocateMissingIsSilent fails on any error or ok=true

  t-2  Parse memory corpus and index integrity
       files:    internal/memadvise/corpus.go (new), internal/memadvise/corpus_test.go (new),
                 internal/memadvise/testdata/corpus/ (new fixture dirs)
       covers:   c-5
       depends:  —
       desc:     Read MEMORY.md and the *.md direct children (O_NONBLOCK, regular files only,
                 size-capped). Parse index lines ([title](file.md) — hook text), each
                 file's frontmatter name: plus body, and [[slug]] wiki links in bodies with
                 inline code stripped. Produces DeadLinks (index target missing, outside
                 the dir, a symlink, or not .md), Orphans (*.md that is not MEMORY.md and
                 not indexed) and Inbound[slug] (distinct linking files, self-links excluded).
       contract: - an index link "../../.ssh/id.md" or "/etc/x.md" is reported dead and never
                   opened; TestIndexLinkOutsideDirIsDead fails if it is read or counted live
                 - a FIFO named foo.md in the dir doesn't hang the parse (it returns within 1s
                   and skips the FIFO); TestCorpusSkipsFIFO fails otherwise
                 - [[dross-v1-4-scoped]] resolves to the file whose frontmatter is
                   name: dross-v1-4-scoped even though the file is dross_v1_4_scoped.md;
                   TestInboundResolvesByFrontmatterName fails if it matches by filename
                 - a [[x]] inside `backticks`, or a file linking itself, adds no inbound;
                   TestInboundIgnoresCodeSpansAndSelf fails otherwise
                 - two files declaring the same name: slug are both reported (ambiguous), and
                   neither gets inbound credit; TestDuplicateNameSlugReported fails otherwise
                 - a file with no frontmatter, or CRLF frontmatter, still parses its body and is
                   counted (orphan or indexed); TestCorpusToleratesMissingAndCRLFFrontmatter fails otherwise

  t-3  Classify guard phrases and expired hooks
       files:    internal/memadvise/rules.go (new), internal/memadvise/rules_test.go (new)
       covers:   c-4, c-8
       depends:  —
       desc:     Guarded(file) matches NEVER, ASK, DECOY, REOPENED as case-sensitive whole words,
                 and don't re-raise / don't revert case-insensitively with straight or curly
                 apostrophes, over the whole file (frontmatter + body). ExpiredClause(hook, today,
                 isKnownPhase) finds a time-bound clause, meaning a YYYY-MM-DD that directly
                 follows a forward marker (until/by/due/before/till/expires/deadline), or
                 next[:]? [phase |/dross-<verb> ]<slug>, where the date is before today or the
                 slug is a known phase. It returns the clause span and a reworded hook with
                 the span and its separator removed.
       contract: - "TASK", "asked", "Nevermind" do NOT guard while "NEVER", "ASK first" do;
                   TestGuardWholeWordCaseSensitive fails on either direction
                 - "don’t re-raise" (U+2019) and "Don't revert" both guard;
                   TestGuardApostropheVariants fails otherwise
                 - "RESOLVED 2026-09-19 (03983fe)" and "shipped 2026-10-04" (a status word before
                   a past date, no forward marker) are NOT time-bound clauses;
                   TestPastDateAfterStatusWordIsNotExpiry fails if either matches
                 - "until 2026-10-29" with today 2026-10-30 is expired, "until 2026-10-30" is not
                   (strictly before); TestExpiryBoundaryIsStrict fails otherwise
                 - "next /dross-spec memory-prune-advisor" and "next: foo" expire only when
                   isKnownPhase(slug) is true; TestNextClauseNeedsKnownPhase fails if an unknown
                   slug expires or a known one doesn't
                 - the reworded hook for "X — done; next phase foo" is "X — done" (no dangling
                   "; " or " — "); TestRewordStripsClauseAndSeparator fails otherwise

  t-4  Detect memory bodies duplicating tracked docs
       files:    internal/memadvise/dup.go (new), internal/memadvise/dup_test.go (new)
       covers:   c-7
       depends:  —
       desc:     Tracked docs are listed via gitrun.Raw(repo, "ls-files", "-z", "--", CLAUDE.md,
                 ARCHITECTURE.md, README*, docs/**/*.md). Normalisation lowercases, strips
                 markdown punctuation and collapses whitespace. Uses 8-word shingle sets.
                 A memory duplicates the single doc with the highest overlap when
                 |body∩doc| / |body| ≥ 0.5. Not a git repo means no duplicates and no error.
       contract: - a body with exactly 50% of its shingles in one doc is a duplicate and 49% is not;
                   TestDupThresholdInclusiveAtHalf fails on either side
                 - 30% overlap with doc A plus 30% with doc B is NOT a duplicate (one doc, not the
                   union); TestDupIsPerDocNotUnion fails otherwise
                 - a body under 8 words never divides by zero and is never a duplicate;
                   TestDupShortBodyNeverMatches fails otherwise
                 - an identical copy in an untracked docs/x.md, or in a tracked docs/x.txt, does not
                   match; TestDupOnlyTrackedMarkdown fails otherwise
                 - a restatement with sentences reordered but each sentence ≥8 words still matches,
                   and the result names the doc path; TestDupCatchesReorderedRestatement fails otherwise

  t-5  Count repo turns with a 24h cache
       files:    internal/memadvise/turns.go (new), internal/memadvise/turncache.go (new),
                 internal/memadvise/turns_test.go (new), internal/memadvise/turncache_test.go (new)
       covers:   c-2
       depends:  —
       desc:     Stream every top-level <slug>/*.jsonl (subagents/ subdirs excluded) with
                 bufio.Reader, so there is no Scanner token cap. A turn is a distinct message.id per
                 file among type=assistant, isSidechain=false, model≠"<synthetic>". Bucket by UTC
                 day and model, per project slug plus all-projects total, last 30 days only. The
                 cache (path passed in) keeps per-file {size, mtime, offset, day buckets}, so a
                 recount after 24h rescans only appended bytes and skips files whose mtime is
                 older than the window. Writes are atomic (temp + rename). Returns
                 counts and cache age.
       contract: - a 5 MB single-line tool result between two assistant turns yields 2 turns, not an
                   error or a truncated count; TestTurnsSurviveMultiMegabyteLine fails otherwise
                 - three entries sharing one message.id count once, and a sidechain entry and a
                   "<synthetic>" entry count zero; TestTurnDedupeAndMainOnly fails otherwise
                 - a malformed JSON line is skipped and the rest still counts;
                   TestTurnsSkipMalformedLine fails if counting aborts
                 - a cache under 24h old opens zero transcript files (asserted by making the
                   transcripts dir unreadable); TestFreshCacheTouchesNoTranscripts fails otherwise
                 - a cache that's corrupt, has a future timestamp, or is over 24h old triggers a
                   recount; TestCacheRecountsOnCorruptFutureOrStale fails if a stale count is returned
                 - after appending one turn to a cached file, the recount reads only the appended bytes
                   and totals old+1; a file truncated below its offset is recounted from 0;
                   TestIncrementalRecountAndTruncation fails otherwise
                 - a transcript whose mtime is 31 days old is never opened;
                   TestOldTranscriptsNotOpened fails otherwise
                 - a turn dated 31 days ago is excluded from the 30-day count;
                   TestThirtyDayWindowEdge fails otherwise

Wave 2 (t-6 depends t-1..t-5; t-7 depends t-1)
  t-6  Build ranked suggestions with $/month estimates
       files:    internal/memadvise/advise.go (new), internal/memadvise/cost.go (new),
                 internal/memadvise/advise_test.go (new), internal/memadvise/cost_test.go (new)
       covers:   c-1, c-2, c-4, c-5, c-7, c-8
       depends:  t-1, t-2, t-3, t-4, t-5
       desc:     Advise(repoRoot, now) combines the corpus, classifiers, dup detector and turn
                 counts into []Suggestion{ID, File, Kind (delete|reword), Reason, Doc, Inbound,
                 BytesSaved, NewHook, USDMonth}, with at most one suggestion per memory.
                 Precedence: expired clause → reword (beats dup delete). Guarded → never delete.
                 Dead link → delete index line. Orphan → delete file unless guarded.
                 isKnownPhase = phase.DirExists || phase.Done. The ID is a hash of
                 kind+file+index line. cost.go: tokens = ceil(bytes/4) and
                 USD = tokens × turns_m × cacheReadRate_m / 1e6, summed over models; unknown
                 models are left out of $ but kept in the share. Share = repo/all, display only.
                 Sorted by USD desc, then ID.
       contract: - a guarded memory that also duplicates README.md, and a guarded orphan, get no delete
                   suggestion; TestGuardBlocksEveryDeleteSource fails otherwise
                 - a memory whose body and hook contain only DEAD / done / RESOLVED 2026-09-19 / DECLINED
                   yields zero suggestions end-to-end; TestStatusWordsAloneSuggestNothing fails otherwise
                 - a memory with an expired "next: <scaffolded phase>" hook that also duplicates a doc gets
                   exactly one suggestion, kind reword; TestExpiredHookNeverDeleted fails if it's delete or two
                 - a suggestion for a memory with 2 inbound [[links]] carries Inbound=2 and its Reason
                   names them; TestSuggestionReportsInboundLinks fails otherwise
                 - 400 bytes saved × 1000 turns × $0.50/MTok = $0.05/month exactly; a 1000-byte line at
                   0 turns is $0; TestCostFormulaUnits fails on a 1e3/1e6 slip or a non-zero $0 case
                 - all-projects total 0 yields share "n/a", not NaN or a panic; TestShareZeroTotal fails otherwise
                 - inserting a new memory at the top of MEMORY.md leaves the other suggestions' IDs unchanged,
                   while editing a suggestion's index line changes its ID; TestSuggestionIDStableAcrossReorder fails otherwise
                 - a missing memory dir or a missing MEMORY.md returns zero suggestions and nil error;
                   TestAdviseMissingDirOrIndexIsEmpty fails otherwise

  t-7  Gitignore turn cache and memory snapshots
       files:    internal/cmd/gitignore.go, internal/cmd/gitignore_test.go, .gitignore
       covers:   c-3, c-9
       depends:  t-1
       desc:     Add the cache file and snapshot dir from memadvise/paths.go to
                 drossIgnoreEntries (seeded into every dross repo) and to this repo's .gitignore,
                 each with its why-comment. Snapshots hold private memory text, and this repo is public.
       contract: - in a freshly init'd repo, `git check-ignore` matches both .dross/memory-turns.json and
                   .dross/memory-snapshots/<ts>/MEMORY.md; TestAdvisorArtifactsIgnoredOnInit fails otherwise
                 - this repo's own .gitignore covers both paths; TestRepoIgnoresAdvisorArtifacts fails if either
                   would be tracked by `git add -A`
                 - the ignore paths are read from the memadvise constants, so renaming one without the other
                   fails TestIgnoreEntriesMatchAdvisorPaths

Wave 3 (both depend t-6)
  t-8  Add `dross memory advise` command
       files:    internal/cmd/memory.go (new), internal/cmd/memory_test.go (new), cmd/dross/main.go
       covers:   c-1, c-2, c-3, c-5
       depends:  t-6
       desc:     Add a `memory` group with an `advise` subcommand that prints every suggestion
                 (ID, file, action, reason, doc, inbound, $/month), then an integrity block
                 (dead links, orphans, duplicate slugs), the repo's share of turns and the cache
                 age. Errors are loud (non-zero exit). The group is registered in main.go.
       contract: - running advise over a fixture memory dir leaves the sha256 of every file plus the dir
                   listing identical, and creates no file in it; TestAdviseNeverWritesMemoryDir fails otherwise
                 - a sentinel string that appears only in a memory body (not its index line) never appears in
                   advise output; TestAdviseNeverPrintsBodies fails otherwise
                 - every printed suggestion line has an ID, a file, delete|reword and a $/month figure, and
                   the share and cache-age lines are present; TestAdviseLineShape fails otherwise
                 - an unreadable transcripts dir makes advise exit non-zero naming the path;
                   TestAdviseSurfacesCountError fails if it exits 0

  t-9  Render memory section in dross status
       files:    internal/cmd/status.go, internal/cmd/status_memory_test.go (new)
       covers:   c-1, c-2, c-3
       depends:  t-6
       desc:     Add a memory: block with the top 3 by $/month, then a "+N more — dross memory advise"
                 line (only when N>0) and the share + cache age. It is placed before the reentry
                 footer. Any advisor error, a missing dir, or zero suggestions prints nothing,
                 and status still exits 0.
       contract: - 5 suggestions print exactly 3 in USD order plus "+2 more — dross memory advise"; 3 print no
                   "+N more" line; TestStatusMemoryTopThree fails otherwise
                 - with no memory dir, and separately with zero suggestions, status output is byte-identical
                   to the output with the advisor disabled; TestStatusMemorySilentWhenEmpty fails otherwise
                 - a corrupt memory fixture that makes Advise error still exits 0 and prints no memory: line;
                   TestStatusSurvivesAdvisorError fails otherwise
                 - the reentry line is still the last line of status output with the memory block present;
                   TestStatusFooterStillLast fails otherwise
                 - the memory dir hash is unchanged across a status run; TestStatusNeverWritesMemoryDir fails otherwise

Wave 4
  t-10 Add confirmed `dross memory apply <id>`
       files:    internal/memadvise/apply.go (new), internal/memadvise/apply_test.go (new),
                 internal/cmd/memory_apply.go (new), internal/cmd/memory.go
       covers:   c-3, c-9
       depends:  t-6, t-8
       desc:     Without --yes, refuse and write nothing. Otherwise recompute suggestions and refuse an
                 ID that's no longer present. Re-check the guard on the live file before any delete.
                 Only touch a direct-child regular non-symlink *.md. Snapshot every file in the
                 memory dir to .dross/memory-snapshots/<UTC ts>/ and abort if that fails.
                 Rewrite MEMORY.md atomically (temp + rename, CRLF and trailing newline kept,
                 aborting if its hash changed since the plan), then remove the file for a
                 memory delete. Afterwards re-check orphans and dead links and print them.
       contract: - apply without --yes exits non-zero and the memory dir hash is unchanged;
                   TestApplyRefusesWithoutConfirm fails otherwise
                 - with 3 suggestions, applying one leaves the other two files and every other MEMORY.md
                   byte untouched; TestApplyChangesExactlyOne fails otherwise
                 - an ID from before the index line was edited is refused as stale with no write;
                   TestApplyRefusesStaleID fails otherwise
                 - a memory that gained "NEVER" after advise is refused for delete at apply time;
                   TestApplyRechecksGuardLive fails otherwise
                 - a snapshot dir that can't be created aborts before MEMORY.md or any file changes;
                   TestApplyAbortsWhenSnapshotFails fails otherwise
                 - a dead-link index target that is a symlink to a file outside the dir is never followed or
                   deleted (only the index line goes); TestApplyNeverDeletesOutsideDir fails otherwise
                 - MEMORY.md changed between plan and rename aborts with it intact;
                   TestApplyAbortsOnConcurrentIndexEdit fails otherwise
                 - after deleting a memory that had inbound links, the report lists the resulting orphans/dead
                   links (none when clean); TestApplyReportsPostCheck fails otherwise

  t-11 Record /dross-status before/after cost
       files:    .dross/phases/memory-prune-advisor/status-cost.toml (new),
                 internal/cmd/statuscost_record_test.go (new), assets/prompts/status.md
       covers:   c-6
       depends:  t-9
       desc:     Following the context-boundaries flow-cost.toml pattern (a jq filter recorded in the
                 file, counts only), measure one /dross-status run on the pre-phase binary and one on
                 the new binary after `make install` (r-01). Each leg records agent_turns, wall_clock_s,
                 bytes_printed (dross status stdout) and the dross_calls set. status.md gains only a
                 follow-up route to `dross memory advise`, never a step that runs it.
       contract: - the after leg's dross_calls set contains a dross call the before leg lacks →
                   TestStatusCostNoAddedCalls fails (the zero-round-trip gate)
                 - either leg missing agent_turns, wall_clock_s or bytes_printed, or any of them is zero →
                   TestStatusCostRecorded fails
                 - status.md's numbered steps invoke `dross memory` (advise or apply) →
                   TestStatusPromptRunsNoMemoryCall fails
                 - a string value outside the declared keys (binary_version, session_id, measured_at, filter,
                   method) → TestStatusCostCopiesNoContent fails, so no transcript or memory text is tracked
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-1 (locate/silent-missing), t-6 (empty result), t-8 (advise listing), t-9 (status surface + silence) |
| c-2 | t-5 (turn counts + cache age), t-6 (cost formula + share), t-8 (advise shows $/share), t-9 (status shows $/share/age) |
| c-3 | t-8 (advise read-only), t-9 (status read-only), t-10 (only confirmed apply writes), t-7 (cache/snapshots live outside the memory dir and stay untracked) |
| c-4 | t-3 (guard + status-word date trap), t-6 (guard blocks every delete source; status words alone → nothing) |
| c-5 | t-2 (dead links, orphans, inbound by name: slug), t-6 (suggestion names inbound links), t-8 (integrity block printed) |
| c-6 | t-11 |
| c-7 | t-4 (shingle detector), t-6 (delete suggestion naming the doc) |
| c-8 | t-3 (clause grammar + expiry), t-6 (reword wins, never delete) |
| c-9 | t-10 (apply), t-7 (snapshot dir ignored) |

All 9 of 9 criteria are covered.

## Judgment calls

- **What counts as a time-bound clause.** A date counts only after a forward marker (until/by/due/…) or in a `next <slug>` clause. I rejected "any date before today" because nearly every real index hook carries a past status date (e.g. "RESOLVED 2026-09-19"), which would flood c-8 and break the second half of c-4.
- **Expired hook that also duplicates a doc.** Reword wins and the memory gets one suggestion. I rejected emitting a delete as well because c-8 says "never a delete" literally, and the safe direction is losing a little saving rather than losing a memory.
- **Guard matching.** The uppercase guards are case-sensitive whole words over the whole file, frontmatter included, and the phrase guards are case-insensitive with both apostrophe forms. I rejected case-insensitive "ask" because it hits "task"/"asked". Matching frontmatter too only adds protection.
- **Suggestion IDs.** Each ID is a content hash of kind+file+index line. I rejected list-position IDs: they shift after any edit, so `apply <id>` could carry out the wrong suggestion. A changed line now reads as stale.
- **Turn definition.** A turn is a distinct message.id per file, main agent only (isSidechain false, model not `<synthetic>`, subagents/ excluded), matching the flow-cost.toml precedent. I rejected counting subagent turns because it over-sells cost: MEMORY.md is a main-session load. I also rejected deduping across files, which would need a large id set in the cache. The cost is possible overcount from forked sessions.
- **Recount cost.** The recount is incremental, using per-file offsets plus skipping files whose mtime is older than 30 days. I rejected a full 2.2 GB rescan every 24h inside `dross status`. Status is a hook target, so one slow day breaks c-6 and session start. This stays inside the locked turn_counts decision: the cache still expires at 24h.
- **Cache location.** The cache is a separate gitignored `.dross/memory-turns.json` owned by memadvise. I rejected storing it in localstore/local.toml, which is a closed-key typed config store whose keys are trust-bearing. Bucketed counts aren't config.
- **Snapshot location.** Snapshots go to gitignored `.dross/memory-snapshots/`, with their own ignore task. I rejected snapshotting inside the memory dir, which would create orphans and break c-3's hash. A missed ignore entry would also push private memory text to a public repo.
- **Per-model pricing.** Turns are bucketed per model, and unknown models are left out of the $ figure but kept in the share. I rejected a single flat rate, which misprices mixed-model months. Pricing unknown models high would over-sell.
- **Token estimate.** tokens = ceil(bytes/4). I rejected calling a tokenizer or the API, which would be a network round-trip on a status hot path.
- **Errors split by surface.** The status memory block is silent on any advisor error, while `dross memory advise` fails loudly. I rejected surfacing errors in status: it's a hook target, and an error there costs the re-entry footer.
- **Missing MEMORY.md with files present.** This yields nothing, because no index loads, so there's nothing to save. I rejected reporting every file as an orphan here as noise.
- **c-6 gate.** The gate is the dross_calls set: the after leg adds no dross invocation, with raw turns recorded alongside. I rejected gating on raw turn equality, because turns vary with how the agent batches the same steps (the context-boundaries c-8 precedent).
- **Layering.** All JSON, hashing and transcript I/O stays in internal/memadvise. I rejected putting it in internal/cmd, where boundary_test.go bans encoding/json and toml imports.
