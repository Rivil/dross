# memory-prune-advisor — MVP lens draft

Lens: the smallest task set that meets every criterion. One new package (`internal/memadvise`), one new command file, one status edit, one measurement record. No new abstractions beyond those.

```
Phase memory-prune-advisor — 5 tasks across 4 waves

Wave 1
  t-1  Build memory analyser: index, integrity, rules
       files:    internal/memadvise/advise.go (new), internal/memadvise/advise_test.go (new)
       desc:     Analyse(memDir, docs map[path]string, phases set, now) parses MEMORY.md index lines,
                 memory files, their frontmatter `name:` and [[links]]. It reports dead index links, orphan
                 files and inbound links per slug, and builds suggestions {id=file stem, file, reason,
                 action delete|reword, proposed hook, inbound}. The rules: guard phrase in the body blocks
                 delete; status words alone produce nothing; ≥50% of the body's normalised 8-word shingles
                 in one doc → delete naming that doc; an expired date or `next: <scaffolded phase>` in the
                 hook → reword with the clause stripped. A missing dir returns an empty report, not an error.
       covers:   c-4, c-5, c-7, c-8
       depends:  —
       contract: - a body with "DECOY" that also duplicates README.md gets no delete suggestion; one with
                   only "RESOLVED" in it gets no suggestion at all
                 - a MEMORY.md link to absent foo.md is reported dead; bar.md with no index line is reported
                   orphan; [[baz-slug]] resolves through baz.md's frontmatter `name: baz-slug` (not its
                   filename) and that file's suggestion carries inbound=1
                 - a body whose shingles are 49% in docs/a.md gives no suggestion; at 50% it gives delete
                   with reason naming docs/a.md. Shingles split across two docs (30%+30%) give no suggestion
                 - hook "— next: foo" with foo in the phase set → reword, never delete; with foo absent →
                   nothing; a hook "(until 2026-10-01)" with now=2026-10-30 → reword; date after now → nothing
                 - Analyse on a non-existent dir returns zero suggestions and zero integrity issues

  t-2  Count repo turns with 24h cache
       files:    internal/memadvise/turns.go (new), internal/memadvise/turns_test.go (new),
                 internal/secretscan/writers.go, internal/cmd/secretscan_writer_enum_test.go
       desc:     Slug(absRepo) (`/` and `.` → `-`) and ProjectsDir() (honours CLAUDE_CONFIG_DIR). Turns()
                 scans ~/.claude/projects/*/*.jsonl and counts main-agent assistant turns (distinct
                 message.id, not sidechain, not <synthetic>, timestamp within 30d) per project, caches them in
                 .dross/memory/turns.toml under a self-written `*` .dross/memory/.gitignore, and rescans only
                 when the cache is >24h old. MonthlyCost(bytes, turns) = ceil(bytes/4) tokens × turns ×
                 CacheReadPerMTok / 1e6. The writer is registered as MachineLocal with a MemoryIgnoreSeed.
       covers:   c-2
       depends:  —
       contract: - a fixture with 2 entries sharing one message.id, 1 sidechain, 1 <synthetic> and 1
                   31-day-old turn counts 1 turn, not 5
                 - a cache written 23h ago is returned without opening any transcript (fixture transcripts
                   made unreadable still give the cached count); at 25h the count changes to the fixture's
                 - share = repo turns / all-project turns: 30 of 120 → 0.25
                 - MonthlyCost(400 bytes, 1000 turns) equals 100 × 1000 × rate / 1e6 exactly
                 - Slug("/Users/x/Development/dross") == "-Users-x-Development-dross"
                 - `git check-ignore .dross/memory/turns.toml` succeeds after the first write
                   (writer-enum test via MemoryIgnoreSeed); removing the registry entry fails the enum test

Wave 2 (depends t-1, t-2)
  t-3  Add dross memory advise and apply
       files:    internal/cmd/memory.go (new), internal/cmd/memory_test.go (new), cmd/dross/main.go,
                 internal/secretscan/writers.go
       desc:     memoryReport(root, now) gathers the memory dir (ProjectsDir/Slug/memory), tracked docs via
                 git ls-files (CLAUDE.md, ARCHITECTURE.md, README*, docs/**/*.md), phase dir names and turns.
                 It ranks suggestions by $/month. `advise` prints every suggestion (id, file, reason, action,
                 $/month, inbound note), the integrity issues, repo share and cache age. `apply <id> --yes`
                 copies the dir to ../memory-snapshots/<UTC ts>/, refuses a guard-phrase delete, deletes the
                 file + its index line or rewrites the hook line, then re-runs integrity and prints it.
                 apply.go's writes are registered as OutsideDross.
       covers:   c-2, c-3, c-9
       depends:  t-1, t-2
       contract: - `memory advise` leaves the fixture memory dir with the same sha256 tree hash before and after
                 - `memory apply <id>` without --yes exits non-zero and the dir hash is unchanged
                 - `apply <id> --yes` on a delete removes exactly that file and its one MEMORY.md line; every
                   other file is byte-identical, and memory-snapshots/<ts>/ holds the pre-apply copy
                 - `apply` on a guard-phrase memory's id (forced through a crafted suggestion) refuses, and the
                   dir hash is unchanged
                 - after an apply that removes the only target of a [[link]], the output names that dead link
                 - advise output's first suggestion has the largest $/month, and the line prints the
                   `share: N%` figure

Wave 3 (depends t-3)
  t-4  Print memory section in dross status
       files:    internal/cmd/status.go, internal/cmd/status_memory_test.go (new)
       desc:     Status calls memoryReport and prints a `memory:` block with the top 3 suggestions/integrity
                 issues by $/month, then `+N more — dross memory advise`, then share + cache age. It prints
                 nothing when the dir is missing or the list is empty. A report error is silent, never fatal.
       covers:   c-1, c-3
       depends:  t-3
       contract: - a fixture with 5 items prints exactly 3 item lines plus "+2 more — dross memory advise"
                 - a missing memory dir and an empty-suggestion dir both produce output with no "memory:"
                   substring
                 - each printed line contains the file name, the action word (delete|reword) and a "$" figure
                 - `dross status` leaves the memory dir hash unchanged
                 - an unreadable memory dir still lets status exit 0 with its reentry footer as the last line

Wave 4 (depends t-4)
  t-5  Record /dross-status cost; document dross memory
       files:    .dross/phases/memory-prune-advisor/status-cost.toml (new),
                 internal/cmd/status_cost_record_test.go (new), README.md
       desc:     After `make install` (r-01), run one /dross-status in a fresh session on the base binary
                 (739d5ac1) and one on the phase head. Record turns, wall-clock seconds and stdout bytes,
                 counted structurally from the transcript, in [before]/[after]. Add README rows for
                 `dross memory advise|apply` and the status `memory:` line.
       covers:   c-6
       depends:  t-4
       contract: - status_cost_record_test fails if status-cost.toml lacks before/after turns, wall_clock_s
                   or bytes, or if after.turns != before.turns (the zero-round-trip claim)
                 - a README grep test fails if `dross memory apply` or `--yes` is missing from README.md
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 status shows suggestions; silent when missing/empty | t-4 (t-1 supplies the empty report for a missing dir) |
| c-2 $/month per suggestion + repo share | t-2 (count, cache, formula), t-3 (per-suggestion display, ranking), t-4 (share + cache age in status) |
| c-3 status/advise never write; only confirmed apply | t-3 (advise and unconfirmed-apply hash), t-4 (status hash) |
| c-4 guard phrases block delete; status words alone are silent | t-1 |
| c-5 dead links, orphans, inbound via frontmatter name | t-1 (detection), t-3 (apply re-check output) |
| c-6 zero added round-trips + before/after record | t-5 |
| c-7 doc-duplicate delete naming the doc | t-1 (rule), t-3 (tracked-doc gathering) |
| c-8 expired time-bound hook → reword never delete | t-1 |
| c-9 apply: one suggestion, confirm flag, snapshot, guard refusal, re-check | t-3 |

All 9 of 9 criteria are covered.

## Judgment calls

- **Analysis in one task (t-1), not split per rule.** I rejected separate tasks for the integrity, guard, dup and time-bound rules. They share one parsed model of the memory dir, and splitting them only adds handoffs.
- **The analyser is pure (t-1).** It takes the memory dir, a docs map and a phase set as arguments; it does not call git or read `.dross` itself. I rejected having it read the filesystem for docs and phases, because injected inputs make the 49%/50% and `next:` contracts plain table tests. It also lets t-1 and t-2 run in parallel.
- **Slug derivation goes in t-2, not t-1.** No Go code derives `~/.claude/projects/<slug>` today (only test fixtures name such dirs). The transcript scan needs every project dir for the share figure anyway. t-1 gets a path and needs no slug.
- **The turn cache lives in a self-ignoring `.dross/memory/` dir, not local.toml or a new ensureDrossGitignore seed.** This copies debugsession and runlog, so it needs no change to gitignore.go. localstore is a closed config key set and is the wrong home for a cache. The locked turn_counts decision requires gitignored `.dross` state, which this meets.
- **Snapshots go to `~/.claude/projects/<slug>/memory-snapshots/<ts>/`, not `.dross/`.** That keeps memory contents out of the repo tree, which `dross test` rsyncs to helicon, and out of the memory dir itself, where they would show up as orphans (c-5).
- **The turn filter is re-implemented in turns.go rather than exported from ctxnudge.** ctxnudge's decoder is tuned for a backwards tail read on a hook hot path. Exporting it to save about 10 lines would couple two hot paths.
- **Tokens are bytes/4, priced at one named cache-read constant.** I rejected per-model pricing from each turn's `model` field. It is speculative, and the locked cost_model asks for "the cache-read $/MTok rate", which is singular.
- **The suggestion id is the memory file's stem, with one suggestion per file.** Delete wins over reword unless the memory is guarded. I rejected hash ids because a user should be able to type the id. A dup-plus-guarded memory therefore gets no delete (c-4) and may still get a reword (c-8).
- **A reword carries a proposed hook with the expired clause stripped, so apply writes it with no free-text flag.** This keeps `apply <id> --yes` to one shape for both actions.
- **The shared gather (`memoryReport`) lives in cmd/memory.go, so status waits for it (t-4 in wave 3).** I rejected moving the gather into memadvise to run t-3 and t-4 in parallel. That would put git and phase-dir reads into the pure package, at the cost of one extra wave of latency.
- **assets/prompts/status.md is not changed.** It already prints `dross status` verbatim, which is how the locked status_surface decision gets c-6's zero added calls. Any edit there could only add a round-trip.
- **The README rows ride the measurement task (t-5) and get no task of their own.** No criterion asks for docs, but README sync is this repo's convention. A standalone docs task would fail the traceability bar.
- **The before and after legs are one task at the end, not a wave-1 "before" task.** The before leg runs the base-commit binary, so its timing doesn't depend on the plan. Splitting it out would add a task for no gain.
