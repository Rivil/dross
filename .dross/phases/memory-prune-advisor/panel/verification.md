# memory-prune-advisor — verification-lens draft

Lens: every task below was derived backward from the test contract written first.
Each contract names the surface that breaks and the fixture that catches it. Pure
rule logic sits in one new package (`internal/memadvise`, NEW) so every criterion
can be pinned in table-driven unit tests against a `t.TempDir()` memory dir, with
`CLAUDE_CONFIG_DIR` pointed at a fixture and the clock injected. The cmd layer
only wires it, and its tests only pin output shape and the c-3 hash invariant.

```
Phase memory-prune-advisor — 10 tasks across 5 waves

Wave 1
  t-1  Locate memory dir; parse index, integrity
       files:    internal/memadvise/locate.go (NEW), internal/memadvise/index.go (NEW),
                 internal/memadvise/locate_test.go (NEW), internal/memadvise/index_test.go (NEW)
       covers:   c-5, c-1
       depends:  —
       desc:     ClaudeDir() honours CLAUDE_CONFIG_DIR else ~/.claude; Slug(repoDir) = symlink-resolved
                 abs path with every non-[A-Za-z0-9] byte → '-'; Load(dir) parses MEMORY.md link lines
                 ([Title](file.md) — hook), each file's frontmatter name:/description:, body, and [[links]];
                 Integrity() returns dead index links, orphan files, and inbound-link map keyed by name: slug.
       contract: Slug("/Users/a/.claude/x") != "-Users-a--claude-x" fails TestSlugReplacesEveryNonAlnum
                 (dot and slash both become '-', matching the real ~/.claude/projects dir names);
                 a symlinked repo dir resolving to /private/tmp/r must slug to "-private-tmp-r" or
                 TestSlugResolvesSymlinks fails; CLAUDE_CONFIG_DIR=/x ignored → TestClaudeDirHonoursEnv fails;
                 Load on a missing dir must return ok=false and nil error (not an error) or
                 TestLoadMissingDirIsSilent fails; an index line naming gone.md is absent from
                 Integrity().DeadLinks → TestDeadIndexLink fails; extra.md on disk but not indexed is absent
                 from Orphans, or MEMORY.md itself listed as an orphan → TestOrphans fails;
                 B's body "[[a-thing]]" must count as inbound to the file whose frontmatter is
                 `name: a-thing` even when its filename is a_thing_v2.md — resolving by filename instead
                 fails TestInboundResolvesByFrontmatterName.

  t-2  Guard phrases and doc-duplication shingles
       files:    internal/memadvise/guard.go (NEW), internal/memadvise/dup.go (NEW),
                 internal/memadvise/guard_test.go (NEW), internal/memadvise/dup_test.go (NEW)
       covers:   c-4, c-7
       depends:  —
       desc:     Guarded(body) matches NEVER, ASK, DECOY, REOPENED as case-sensitive whole words and
                 "don't re-raise" / "don't revert" case-insensitively with ' or ’; DocCandidates(tracked)
                 keeps CLAUDE.md, ARCHITECTURE.md, README*, docs/**/*.md; Duplicates(body, docs) returns the
                 single doc holding ≥50% of the body's normalised (lowercase, alnum-only) 8-word shingle set.
       contract: each of the six phrases in an otherwise-plain body must return true, else the
                 per-phrase row of TestGuardPhrases fails; "ask" in lowercase prose ("I'll ask later")
                 and "NEVERTHELESS" must return false or TestGuardWholeWordOnly fails (a substring match
                 would guard nearly every memory); "don’t revert" (curly) must guard;
                 a body whose shingles are exactly 50% in README.md is a duplicate and 49% is not —
                 moving the threshold or using > instead of ≥ fails TestDupThresholdBoundary;
                 30% in docs/a.md + 30% in docs/b.md must NOT be a duplicate (one doc, not the union)
                 or TestDupNeedsOneDoc fails; reordering the body's paragraphs keeps it a duplicate
                 (TestDupSurvivesReorder); a body under 8 words never duplicates (TestDupShortBody);
                 docs/x.txt, notes/README.md-adjacent non-matches and src/foo.md are dropped by
                 DocCandidates or TestDocCandidates fails; the returned doc path is the matching doc's
                 repo-relative path (TestDupNamesDoc).

  t-3  Expired hook clause detection and reword
       files:    internal/memadvise/hook.go (NEW), internal/memadvise/hook_test.go (NEW)
       covers:   c-8
       depends:  —
       desc:     Expired(hook, today, phaseKnown) finds time-bound clauses — a YYYY-MM-DD after
                 until/by/before/due/expires/till, or `next[:] [phase] [/dross-<cmd>] <slug>` — and returns
                 the reworded hook with only the expired clause (and its separator) removed.
       contract: "until 2026-09-01" with today 2026-10-30 returns expired and a hook without that clause,
                 "until 2026-12-01" returns not-expired → TestDatedClause fails otherwise;
                 a bare historical date ("released 2026-01-01") must NOT be expired or
                 TestHistoricalDateIsNotTimeBound fails (otherwise every dated hook gets flagged);
                 "next: foo" with phaseKnown("foo")=true is expired, =false is not
                 (TestNextPhaseClause); the real hook shape "…; next /dross-spec memory-prune-advisor"
                 is parsed (TestNextSlashCommandForm); the reworded hook differs from the original only by
                 the removed clause — any other byte change fails TestRewordRemovesOnlyClause;
                 a hook that is nothing but the clause yields a non-empty fallback, never "" (TestRewordNeverEmpty).

  t-4  Count repo turns with 24h cache
       files:    internal/memadvise/turns.go (NEW), internal/memadvise/turns_test.go (NEW),
                 internal/cmd/gitignore.go, internal/cmd/gitignore_test.go, .gitignore
       covers:   c-2, c-6
       depends:  —
       desc:     Turns(projectsDir, slug, now) counts distinct main-agent assistant message.ids with a
                 timestamp in the last 30 days across top-level *.jsonl per project dir (repo vs all);
                 cache at .dross/memory/turns.json, reused under 24h, recounted otherwise, age returned;
                 seed `.dross/memory/` in drossIgnoreEntries and the repo .gitignore.
       contract: three transcript lines sharing one message.id must count 1, or TestTurnsDedupeMessageID
                 fails; isSidechain:true, model "<synthetic>", user lines, and files under
                 <session>/subagents/ add 0 (TestTurnsMainAgentOnly); a 31-day-old timestamp adds 0
                 (TestTurnsWindow); a transcript whose mtime is >30 days old is never opened — fixture is a
                 chmod-000 file with an old mtime, so opening it surfaces as an error and fails
                 TestTurnsSkipsStaleFilesByMtime; RepoShare = repo/all across two fixture project dirs
                 (TestTurnsShareAcrossProjects); with a cache 23h old, appending turns to the transcripts
                 must NOT change the result and age must be 23h (TestCacheReusedUnder24h); at 25h the
                 appended turns appear and the cache file is rewritten (TestCacheRecountedAt24h);
                 a corrupt turns.json recounts instead of erroring (TestCorruptCacheRecounts);
                 the cache path is under <root>/.dross/memory/ and nowhere under the memory dir
                 (TestCacheNeverInMemoryDir); `.dross/memory/` missing from drossIgnoreEntries fails the
                 new TestEnsureGitignoreCoversMemoryCache (patterned on TestEnsureGitignoreCoversLocalToml),
                 and missing from the repo's own .gitignore fails it too.

Wave 2 (depends t-1, t-2, t-3, t-4)
  t-5  Build suggestions: cost, ranking, stable ids
       files:    internal/memadvise/advise.go (NEW), internal/memadvise/cost.go (NEW),
                 internal/memadvise/advise_test.go (NEW)
       covers:   c-1, c-2, c-4, c-5, c-7, c-8
       depends:  t-1, t-2, t-3, t-4
       desc:     Advise(mem, docs, phaseKnown, turns, today) → []Suggestion{ID, File, Reason, Action
                 (delete|reword-hook|fix-index), NewHook, Doc, Inbound, MonthlyUSD}; delete only from
                 duplication and never when Guarded; reword from Expired; integrity issues as fix-index
                 items; $ = ceil(bytes saved/4) × repo turns × CacheReadUSDPerMTok/1e6, sorted desc, tie by ID.
       contract: a 400-byte index line, 1000 repo turns and rate R must cost exactly 100×1000×R/1e6 —
                 adding file-recall tokens, using all-project turns, or dividing by share fails
                 TestMonthlyCostFormula; a reword that shortens a 120-byte line to 80 bytes is priced on 40
                 bytes, not 120 (TestRewordPricesOnlyDelta); a duplicate memory containing "NEVER" yields no
                 delete suggestion (TestGuardedDuplicateNotDeleted); a fixture where every memory's
                 description carries DEAD/done/RESOLVED/DECLINED/SHIPPED and nothing else yields zero
                 suggestions — any status-word heuristic fails TestStatusWordsAloneSuggestNothing;
                 an expired-hook, non-duplicate memory yields exactly one suggestion with action
                 reword-hook (TestExpiredHookNeverDeletes); a delete for a memory with two inbound
                 [[links]] has Inbound naming both linkers and Reason saying "linked from"
                 (TestSuggestionNamesInboundLinks); a dead index link and an orphan both appear as items
                 (TestIntegrityItemsCountTowardList); a delete suggestion's Doc is the duplicated doc's path
                 (TestDeleteNamesDoc); IDs are identical across two Advise runs and unchanged when an
                 unrelated memory is added (TestSuggestionIDsStable); empty memory dir → empty slice.

Wave 3 (depends t-5)
  t-6  Gather inputs and render advice lines
       files:    internal/memadvise/gather.go (NEW), internal/memadvise/render.go (NEW),
                 internal/memadvise/render_test.go (NEW)
       covers:   c-1, c-2, c-3
       depends:  t-5
       desc:     Gather(root, now) resolves the memory dir, lists tracked docs via internal/gitrun
                 `git ls-files`, builds phaseKnown from .dross/phases/<slug>/ + phase.Done, loads turns;
                 Render(report, limit) emits one line per suggestion (id, file, action, reason, $/mo) plus a
                 share line "this repo: N of M assistant turns (P%) in 30d · turns counted <age> ago" and,
                 when truncated, "+N more — dross memory advise".
       contract: every rendered suggestion line must contain its ID, file name, action word
                 (delete|reword hook) and a "$X.XX/mo" figure, else TestRenderSuggestionLine fails;
                 the share line must carry repo turns, all turns, the percent and the cache age
                 (TestRenderShareAndCacheAge); limit 3 with 5 suggestions renders exactly 3 plus
                 "+2 more — dross memory advise", and limit 3 with 3 renders no "+0 more"
                 (TestRenderTruncation); zero suggestions or a missing memory dir render zero bytes —
                 not a header, not the share line (TestRenderEmptyIsSilent); an untracked docs/x.md with
                 identical text to a memory must NOT produce a delete (Gather uses git ls-files, not a
                 directory walk) or TestGatherUsesTrackedDocsOnly fails; Gather leaves a sha256 over the
                 memory dir's (path, mode, bytes) unchanged even when the turn cache is stale and is
                 rewritten (TestGatherNeverWritesMemoryDir).

  t-7  Apply one suggestion with snapshot and recheck
       files:    internal/memadvise/apply.go (NEW), internal/memadvise/apply_test.go (NEW)
       covers:   c-9, c-3
       depends:  t-5
       desc:     Apply(memDir, snapDir, s) copies the whole memory dir to snapDir/<timestamp>/ first, then
                 either deletes the file and its index line or replaces exactly that index line's hook;
                 refuses a delete whose body is Guarded at apply time; re-runs Integrity and returns
                 dead links and orphans.
       contract: after a delete, the file and its single MEMORY.md line are gone and every other
                 MEMORY.md byte is unchanged (TestApplyDeleteRemovesFileAndLine); after a reword only that
                 line differs, and it equals NewHook (TestApplyRewordTouchesOneLine); the snapshot dir
                 hashes identical to the pre-apply memory dir — snapshot after the mutation, or a partial
                 copy, fails TestApplySnapshotsFirst; a delete for a body that gained "DECOY" after advise is
                 refused, the memory dir hash is unchanged and no file is removed
                 (TestApplyRefusesGuardedDelete); deleting a memory another memory links via [[name]]
                 returns that dangling link in the recheck, and a pre-existing orphan is reported
                 (TestApplyRechecksIntegrity); a snapshot copy failure aborts with the memory dir untouched
                 (TestApplyAbortsWhenSnapshotFails).

Wave 4 (depends t-6; t-9 also t-7)
  t-8  Add memory: section to dross status
       files:    internal/cmd/status.go, internal/cmd/status_memory_test.go (NEW),
                 internal/cmd/status_prompt_test.go
       covers:   c-1, c-2, c-3, c-6
       depends:  t-6
       desc:     After the debug: line, status prints `memory:` + Render(report, 3) indented like actions:;
                 any Gather error is silence, never a status failure. status.md is unchanged.
       contract: with CLAUDE_CONFIG_DIR pointed at a fixture holding 5 suggestions, status output carries
                 "memory:", exactly the 3 highest-$ lines in order, and "+2 more — dross memory advise"
                 (TestStatusMemoryTopThree); with no memory dir, or a dir with nothing to suggest, the output
                 contains no "memory:" substring and is byte-identical to the pre-change output
                 (TestStatusMemorySilentWhenNothing); a sha256 over the memory dir is identical before and
                 after `dross status` (TestStatusNeverWritesMemoryDir); an unreadable MEMORY.md still lets
                 status exit 0 with its footer as the last line (TestStatusMemoryErrorIsSilent); status.md
                 gaining any `dross memory` invocation fails TestStatusPromptAddsNoMemoryCall (the round-trip
                 half of c-6 that a prompt edit could break).

  t-9  Add dross memory advise/apply commands
       files:    internal/cmd/memory.go (NEW), internal/cmd/memory_test.go (NEW), cmd/dross/main.go,
                 internal/cmd/testdata/cli_surface/tests_before.txt
       covers:   c-1, c-2, c-3, c-9
       depends:  t-6, t-7
       desc:     `dross memory advise` prints Render(report, all); `dross memory apply <id> --yes` re-gathers,
                 looks the id up among current suggestions, applies via t-7 with snapshots under
                 .dross/memory/snapshots/, prints the snapshot path and the recheck; register in main.go;
                 record the new test names.
       contract: `apply <id>` without --yes exits non-zero, names --yes, and the memory dir hash is
                 unchanged (TestApplyRefusesWithoutConfirm); an id not among the current suggestions
                 (stale or typo) exits non-zero touching nothing (TestApplyUnknownID); a fix-index item id is
                 refused as not appliable (TestApplyRejectsIntegrityItem); apply --yes on a delete id
                 removes exactly that file, prints a snapshot path under .dross/memory/snapshots/ that exists,
                 and prints the recheck's orphans/dead links (TestApplyEndToEnd); `memory advise` leaves the
                 memory dir hash unchanged and lists every suggestion, not just 3
                 (TestAdviseListsAllReadOnly); missing memory dir → advise prints nothing, exit 0
                 (TestAdviseMissingDirSilent); `dross memory` absent from main.go fails
                 TestMemoryCommandRegistered.

Wave 5 (depends t-8)
  t-10 Record before/after /dross-status flow cost
       files:    .dross/phases/memory-prune-advisor/flow-cost.toml (NEW),
                 internal/cmd/flowcost_record_test.go
       covers:   c-6
       depends:  t-8
       desc:     Run `make install` (r-01), measure one /dross-status on a base-commit build ([before]) and
                 on the phase head with a cold turn cache ([after]); record agent_turns, turn_tools,
                 wall_clock_s, dross_calls and a new status_bytes field; add checkMemoryPruneAdvisorFlow
                 and the phase to the required list.
       contract: the existing checkLegPair fails if [after] runs any dross call [before] did not (zero
                 added round-trips) or both legs ran the same binary_version; the new checker fails if
                 status_bytes is ≤0 on either leg, if [after].agent_turns > [before].agent_turns, or if
                 [scenario] lacks turn_cache = "cold" (the worst case the 24h decision allows);
                 deleting flow-cost.toml fails the "memory-prune-advisor has no flow-cost.toml" check.
```

## Coverage

- c-1 → t-1 (missing dir silent), t-5 (file/reason/action per suggestion), t-6 (render, silence), t-8 (status surface), t-9 (advise)
- c-2 → t-4 (turn counts, share), t-5 (formula), t-6 (share line + cache age), t-8 (printed in status), t-9
- c-3 → t-6 (Gather hash), t-7 (refused apply leaves hash), t-8 (status hash), t-9 (advise hash, apply without --yes)
- c-4 → t-2 (phrase matcher), t-5 (guarded never deleted; status words produce nothing)
- c-5 → t-1 (dead links, orphans, inbound by name:), t-5 (inbound named on suggestion; integrity items in list)
- c-6 → t-4 (mtime skip + 24h cache keep status fast), t-8 (status.md adds no call), t-10 (recorded before/after)
- c-7 → t-2 (≥50% one-doc shingles, doc named), t-5 (delete names doc), t-6 (tracked docs only)
- c-8 → t-3 (clause detection + reword), t-5 (expired → reword only, never delete)
- c-9 → t-7 (snapshot first, guard refusal, recheck), t-9 (--yes gate, one id, unknown id)

## Judgment calls

- Pure `internal/memadvise` package for every rule, cmd only wires; rejected logic in internal/cmd — rules need table tests with injected clock/config dir, and cmd tests run under a pinned empty HOME.
- Slug = every non-alphanumeric byte → '-' on the symlink-resolved path; rejected slash-only replacement — ~/.claude/projects shows `/.claude` → `--claude` and `/tmp` as `-private-tmp`.
- Turns = distinct main-agent message.id in top-level *.jsonl; rejected counting lines or including subagents/ — one assistant turn spans several lines, and the cost model is the main session's MEMORY.md load (ctxnudge's main-agent definition reused).
- Skip transcripts by mtime > 30 days before opening; rejected scanning all 2.2 GB on each recount — it is the only way a cold-cache status stays inside c-6, and it is testable with a chmod-000 fixture.
- One cache-read rate constant (CacheReadUSDPerMTok); rejected a per-model rate table — the locked formula names one rate, and per-model weighting would need per-model turn splits for a display figure.
- Tokens = ceil(bytes/4); rejected a real tokenizer — no dependency, and the figure is explicitly a defensible estimate.
- "Time-bound clause" = a date after until/by/before/due/expires/till, or a next-phase clause; rejected "any date in the hook" — nearly every hook in the real index carries a historical date and would all be flagged.
- Expired clause → reword only; a duplicate memory with an expired hook still gets the c-7 delete (the clause never causes a delete), and the redundant reword is dropped.
- Integrity issues are list items with action fix-index, ranked with the rest, but `apply` refuses them; rejected auto-fixing them — c-9 limits apply to delete or reword hook.
- Unresolved [[links]] are not reported; rejected adding them to integrity — c-5 names only dead MEMORY.md links and orphans.
- Suggestion IDs derived from action + file stem (stable across runs); rejected positional ids — a positional id shifts when ranking changes between advise and apply, so apply could hit the wrong memory.
- apply re-gathers and requires the id to still be suggested, and Apply re-checks the guard; rejected trusting a cached advise result — the body can change between advise and apply.
- Turn cache and snapshots share one gitignored `.dross/memory/` dir with one ignore entry; rejected local.toml (typed config with a closed key set, not a cache) and snapshots inside ~/.claude (would put writes next to the dir c-3 hashes).
- c-6 after-leg measured on a cold cache; rejected warm-only — a warm leg hides the recount cost the 24h decision allows on one run a day.
- status.md left unchanged and pinned by a prompt test; rejected adding a `dross memory advise` step — the status_surface decision routes everything through the verbatim `dross status` output.
