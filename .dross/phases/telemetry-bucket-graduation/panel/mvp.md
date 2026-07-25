# MVP lens — telemetry-bucket-graduation

```
Phase telemetry-bucket-graduation — 3 tasks across 2 waves

Wave 1
  t-1  Graduate five error shapes out of other
       files:    internal/telemetry/telemetry.go,
                 internal/telemetry/telemetry_test.go
       covers:   c-1, c-2, c-3, c-4, c-5
       desc:     Add `arg_count`, `unknown_flag`, `landmark_parse`, `config_io`,
                 `env_token` cases to the ClassifyError switch; extend the existing
                 `unknown_subcommand` case with "unknown command" and the existing
                 `merge_pending` case with the three refusal shapes. Add only
                 arg_count / unknown_flag / landmark_parse to `detailBuckets` and
                 update the CarriesDetail + package-doc allowlist prose.
       contract: - ClassifyError("accepts 2 arg(s), received 1") == "arg_count";
                   ClassifyError("requires at least 1 arg(s), only received 0")
                   == "arg_count" (both real corpus shapes)
                 - ClassifyError("unknown flag: --title") == "unknown_flag" and
                   ClassifyError("flag needs an argument: --pr") == "unknown_flag"
                 - ClassifyError(`unknown command "phaze" for "dross"`) ==
                   "unknown_subcommand" — the existing `unknown subcommand "add"
                   for "dross phase"` case must still return the same bucket
                 - ClassifyError(`landmark pair "feature" has no '=' (want
                   key=value)`) and ClassifyError(`unknown landmark key "loco"`)
                   both == "landmark_parse"
                 - All three merge refusals hit merge_pending: "PR #12 for x is
                   not merged upstream", "cannot confirm x has merged into
                   milestone/v0.10", "origin/main is not merged into
                   origin/milestone/v0.10 yet"
                 - ClassifyError("read ~/p/.dross/state.json: open: no such file")
                   and ClassifyError("decode ~/p/.dross/project.toml: toml: line
                   3: expected key separator") both == "config_io", while the
                   existing case `save state: write .dross/state.json: permission
                   denied` still == "state_io" (ordering regression: config_io
                   placed before state_io steals it)
                 - ClassifyError("$JIRA_API_TOKEN is not set; run `dross env set
                   JIRA_API_TOKEN` in your shell") == "env_token"
                 - CarriesDetail is true for arg_count, unknown_flag,
                   landmark_parse and false for merge_pending, config_io,
                   env_token — a token/path leaking into the log fails
                   TestCarriesDetail

  t-2  Reclassify other at read time, print tail
       files:    internal/cmd/stats.go, internal/cmd/stats_test.go
       covers:   c-6
       desc:     In renderErrorBuckets, re-run telemetry.ClassifyError over the
                 err_detail of every event stored as `other`, counting it under the
                 recovered bucket. Details that still classify as `other` are
                 grouped by detail string and printed as an indented top-5-by-count
                 block under the errors table; the block is skipped when empty.
       contract: - An event {ErrorClass:"other", ErrorDetail:"no .dross directory
                   found"} is counted under `no_root`, not `other`, in the errors
                   table — the read-time reclassify path
                 - The stored JSONL is untouched: after `stats show`, re-Loading
                   the log still returns the event with err="other"
                 - With 6 distinct residual detail shapes at counts 6..1, exactly
                   the top 5 print, in descending count order, each with its count;
                   the count-1 shape is absent
                 - When every `other` event's detail reclassifies (or no `other`
                   events exist), the tail heading does not appear anywhere in the
                   output

Wave 2 (depends t-1)
  t-3  Pin the corpus and the 15% ceiling
       files:    internal/telemetry/testdata/error_corpus.json,
                 internal/telemetry/telemetry_test.go
       covers:   c-7
       desc:     Check in a corpus of the distinct real err_detail shapes harvested
                 from ~/.claude/dross/telemetry.jsonl as
                 [{"detail":"…","want":"…"}], with home paths already collapsed to
                 `~`. Table test asserts each entry's ClassifyError result and that
                 the share of entries classifying as `other` is under 15%.
       contract: - Every corpus entry classifies to its `want` bucket; changing a
                   ClassifyError substring (e.g. dropping "arg(s)") fails the entry
                   naming that shape, with the offending detail in the message
                 - The ratio assertion fails if `other` reaches 15% of corpus
                   entries — reverting any one of t-1's five buckets pushes it over
                 - The test fails loudly (not silently passes) when the testdata
                   file is missing, empty, or unparseable
       depends:  t-1
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (cobra arg-count / flag / unknown command) | t-1 |
| c-2 (landmark parse failure) | t-1 |
| c-3 (merge refusal → merge_pending, 3 shapes) | t-1 |
| c-4 (missing/unreadable .dross config) | t-1 |
| c-5 (unset env token) | t-1 |
| c-6 (stats tail, top 5, omitted when empty) | t-2 |
| c-7 (<15% other over checked-in corpus) | t-3 |

7/7 criteria covered.

## Judgment calls

- **One task for c-1..c-5, not five.** All five are additive `case` arms in the
  single `ClassifyError` switch plus one `detailBuckets` map edit, and all five
  extend the same test table. Split into per-criterion tasks they would be
  same-wave edits to the same 30 lines — pure merge conflict for no isolation
  gain. Rejected the "one bucket per task" shape.
- **t-2 is wave 1, not wave 2.** The read-time reclassify calls the existing
  `ClassifyError`; it compiles and its tests pass against today's buckets (the
  contract uses `no_root`, already shipped). It does not need t-1's output, so
  parking it in wave 2 would only cost wall-clock. Only t-3, whose expectations
  literally name the new buckets and whose ratio assertion only clears once they
  exist, is a real dependency.
- **Tail groups on the raw err_detail string; no fingerprinting.** No criterion
  asks for normalization, and every residual shape in the real log is either
  identifier-only or path-collapsed already. Rejected a shape-normalizer
  (strip digits/quoted tokens) as speculative structure the phase can add when a
  fragmented tail actually shows up.
- **`config_io` is a new bucket placed *after* `state_io`.** Merging state.json
  reads into the existing `state_io` would leave project.toml homeless and split
  c-4 across two buckets; placing the new case before `state_io` would steal
  `save state: write .dross/state.json: permission denied` from its current
  bucket and break a shipped test. Ordering is called out in the contract, not
  left to the implementer.
- **Corpus test appended to `telemetry_test.go`, not a new `corpus_test.go`.**
  Fewest artifacts; the file already owns `TestClassifyError`, and t-1 and t-3
  are in different waves so the shared file is edited sequentially. Rejected a
  dedicated test file as structure without a criterion behind it.
- **No task for `make install` / prompt or README updates.** Rule r-01 is an
  execution-time obligation, not a deliverable; no criterion mentions user-facing
  docs. Rejected adding a docs task.
