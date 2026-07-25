# verification lens — telemetry-bucket-graduation

Designed backward from test contracts: each criterion's assertion was written first,
then the smallest code change that makes that assertion satisfiable became the task.

```
Phase telemetry-bucket-graduation — 5 tasks across 2 waves

Wave 1
  t-1  Graduate identifier-only shapes into detail buckets
       files:    internal/telemetry/telemetry.go
                 internal/telemetry/telemetry_test.go
       covers:   c-1, c-2
       contract: TestClassifyError table gains rows that fail if any shape falls
                 back to "other" —
                   "accepts 1 arg(s), received 2"                    → arg_count
                   "requires at least 1 arg(s), only received 0"     → arg_count
                   "accepts between 1 and 2 arg(s), received 3"      → arg_count
                   "unknown flag: --json"                            → unknown_flag
                   "unknown shorthand flag: 'x' in -x"               → unknown_flag
                   "flag needs an argument: --title"                 → unknown_flag
                   "unknown command \"sepc\" for \"dross\""          → unknown_subcommand
                   "landmark pair \"what=a, b\" has no '='"          → landmark_parse
                   "landmark pair \"=v\" has an empty key"           → landmark_parse
       contract: TestCarriesDetail's carry-list gains arg_count, unknown_flag,
                 landmark_parse — dropping any one from detailBuckets fails it,
                 and the same test's must-NOT-carry list still fails if a
                 merge/config/env bucket is added to the map.
       contract: a round-trip test asserts Detail(errors.New(`unknown flag: --json`))
                 still contains "--json" and Detail on the landmark error still
                 contains the rejected pair, so graduation does not blind the shape.
       contract: pre-existing rows in TestClassifyError still pass — specifically
                 "--pr is required" stays cli_args (arg_count must not swallow it)
                 and "unknown field: nonsense" stays unknown_field.

  t-2  Add detail-free merge, config, and env buckets
       files:    internal/telemetry/telemetry.go
                 internal/telemetry/telemetry_test.go
       covers:   c-3, c-4, c-5
       contract: TestClassifyError rows for the three merge refusals all land in
                 merge_pending and fail if any regresses to "other" —
                   "PR #12 for auth-fix is not merged upstream — refusing…"
                   "cannot confirm auth-fix has merged into main — … is not an
                    ancestor of origin/main"
                   "origin/main carries no completed auth-fix"   (historical shape,
                    still present in the log; needed for read-time reclassify)
       contract: config rows land in a named bucket and fail on "other" —
                   "decode ~/proj/.dross/project.toml: open: no such file" → config_io
                   "decode .dross/project.toml: toml: line 4: expected key
                    separator"                                        → config_io
                   "read ~/proj/.dross/state.json: no such file"       → state_io
                 (state.json is routed to the existing state_io bucket).
       contract: "$JIRA_API_TOKEN is not set; run `dross env set JIRA_API_TOKEN`
                 in your shell" → env_token; the same message wrapped as
                 "board: create issue: $JIRA_API_TOKEN is not set" must still
                 classify board — this row fails if env_token is inserted above
                 the board case and steals it.
       contract: ordering-regression rows: "load milestone .dross/milestones/
                 v0.1.toml: open: no such file" stays no_milestone, "decode plan
                 plan.toml: bad toml" stays no_plan, and "verify.toml not found at
                 .dross/phases/01/verify.toml" stays verify_state — all three fail
                 if config_io's .toml matching is placed too early in the switch.
       contract: TestCarriesDetail fails if merge_pending, config_io, or env_token
                 is ever added to detailBuckets (locked detail_allowlist).

  t-3  Expose message classifier + event reclassifier
       files:    internal/telemetry/telemetry.go
                 internal/telemetry/telemetry_test.go
       covers:   c-6
       description: Extract the switch body into exported ClassifyMessage(string)
                 with ClassifyError delegating to it, and add Reclassify(Event)
                 returning the effective class. Reclassify returns the stored
                 class unless it is "other" AND ErrorDetail is non-empty, in
                 which case it returns ClassifyMessage(ErrorDetail).
       contract: for every row in the TestClassifyError table,
                 ClassifyMessage(row.msg) == ClassifyError(errors.New(row.msg)) —
                 a delegation that drops a case fails on that row.
       contract: Reclassify(Event{ErrorClass:"other", ErrorDetail:"unknown flag:
                 --json"}) == "unknown_flag"; Reclassify(Event{ErrorClass:"other"})
                 == "other" (no detail → no reclassify); Reclassify(Event{
                 ErrorClass:"dirty_tree", ErrorDetail:"unknown flag: --json"})
                 == "dirty_tree" (a non-other stored class is never overridden);
                 Reclassify(Event{}) == "".
       contract: ClassifyMessage("") == "" — the empty string must not fall
                 through to "other" and manufacture a phantom error bucket.

Wave 2 (depends t-1, t-2, t-3)
  t-4  Reclassify at read time and print other tail
       files:    internal/cmd/stats.go
                 internal/cmd/stats_test.go
       covers:   c-6
       depends:  t-1, t-2, t-3
       description: renderErrorBuckets counts telemetry.Reclassify(e) instead of
                 e.ErrorClass, then prints an indented "unclassified" block of the
                 top 5 remaining other-detail strings with counts, omitted when
                 that set is empty.
       contract: an event stored {err:"other", err_detail:"unknown flag: --json"}
                 prints under unknown_flag in the "## errors" table and the errors
                 table shows no "other" row — reverting to e.ErrorClass fails this.
       contract: with 6 distinct other-detail shapes at counts 6/5/4/3/2/1, the
                 rendered tail contains the 5 highest strings in descending order
                 and does NOT contain the count-1 string; changing the cap to 4 or
                 6 fails it.
       contract: with zero surviving other events, the output contains neither the
                 "unclassified" header nor any indented tail line — an
                 unconditional header fails this.
       contract: TestStatsCover_renderErrorBucketsOrder still passes: events
                 carrying only ErrorClass (no err_detail) are counted under their
                 stored class, so reclassification is a no-op there.
       contract: `dross stats show` over a seeded telemetry.jsonl leaves the file
                 byte-identical (compare os.ReadFile before/after) — enforces the
                 locked "the log itself is never rewritten".
       contract: a 240-rune err_detail renders on a single truncated line (<= 90
                 runes incl. the ellipsis), so one pathological shape cannot wrap
                 and destroy the block.

  t-5  Check in error corpus + coverage floor test
       files:    internal/telemetry/testdata/error_corpus.jsonl
                 internal/telemetry/corpus_test.go
       covers:   c-7
       depends:  t-1, t-2, t-3
       description: New testdata fixture: one JSON object per line
                 {"detail": "<real redacted err_detail>", "want": "<bucket>"},
                 curated from distinct shapes in ~/.claude/dross/telemetry.jsonl.
                 New table test drives ClassifyMessage over every line.
       contract: every corpus line classifies to its recorded "want"; a
                 misclassified line fails naming the line number and both buckets,
                 so a future switch reorder that steals a shape is caught here
                 rather than in production.
       contract: the share of corpus lines whose classification is "other" is
                 < 0.15, and the failure message prints the actual ratio plus the
                 offending details — adding an unbucketed shape without a
                 classifier rule pushes the ratio over and fails.
       contract: the fixture must contain >= 20 lines, each with a non-empty
                 detail and non-empty want — an accidentally emptied or truncated
                 corpus fails loudly instead of passing vacuously with 0/0.
       contract: at least one corpus line has want == "other" (the genuine tail is
                 represented), so the ratio assertion is measuring a real
                 distribution rather than a fixture tuned to zero.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (cobra arg-count / unknown-or-incomplete flag / unknown command) | t-1 |
| c-2 (landmark parse failure) | t-1 |
| c-3 (merge-confirmation refusal → merge_pending, 3 shapes) | t-2 |
| c-4 (absent/unreadable .dross config) | t-2 |
| c-5 (required-but-unset env token) | t-2 |
| c-6 (stats top-5 other tail, omitted when empty; read-time reclassify) | t-3, t-4 |
| c-7 (< 15% other over checked-in corpus) | t-5 |

7/7 criteria covered. Every task carries at least one contract that fails on a
specific named surface; no task is justified by "existing tests cover it".

## Judgment calls

- **Merged c-1 and c-2 into one task (t-1) rather than one task per criterion.**
  Chose the locked `detail_allowlist` boundary as the seam: all three new
  identifier-only buckets edit the same `detailBuckets` map, and splitting them
  would mean three tasks racing on one map literal for <10 minutes of work each.
  Rejected: per-criterion tasks.
- **Routed `state.json` read failures into the existing `state_io` bucket instead
  of a new one.** c-4 only requires "a named bucket"; `state_io` already means
  "dross state file read/write failed", and reusing it follows the same reuse
  precedent the locked `bucket_granularity` decision set for unknown-command →
  `unknown_subcommand`. Rejected: one `config_io` swallowing both, which would
  make two buckets mean the same thing.
- **Kept the historical `origin/main carries no completed <slug>` shape in the
  merge_pending matcher even though no current code path emits that string.**
  The locked `read_time_reclassify` decision means stats re-classifies historical
  `other` events, and that shape is live in the existing log. Rejected: matching
  only strings grep-able in today's source, which would leave the historical tail
  fat and c-7's corpus unable to reach the 15% floor.
- **`Reclassify(Event)` lives in the telemetry package, not in stats.go.**
  Puts the locked read-time rule under a pure unit test with no HOME/tempdir/
  stdout capture, so its edge cases (empty detail, non-other stored class) are
  asserted directly. Rejected: an unexported helper in internal/cmd, which would
  only be reachable through captured stdout.
- **Grouped the c-6 tail and the c-7 corpus by the raw redacted `err_detail`
  string — no normalization of numbers, quoted tokens, or paths.** Normalizing
  shapes is a taxonomy concern and the spec defers taxonomy work to
  telemetry-taxonomy-overhaul. Rejected: a shape-normalizer, which would need its
  own criteria and its own tests to be trustworthy.
- **Corpus fixture is JSONL, not TOML.** It mirrors the telemetry log's own
  format so entries can be pasted straight out of
  `jq -c 'select(.err=="other")' telemetry.jsonl`; the TOML stack rule governs
  `.dross` config and state schemas, not test fixtures. Rejected: corpus.toml.
- **t-3 is wave 1, not wave 2.** The ClassifyMessage/Reclassify extraction does
  not need any new bucket to exist — it only needs the switch — so holding it
  behind t-1/t-2 would serialize the phase for no reason.
- **No task wires the new buckets into `RecordCLIEvent`.** `cmd.RecordCLIEvent`
  already gates `err_detail` on `telemetry.CarriesDetail(errClass)` generically,
  so new detail buckets take effect with no call-site change; t-1's
  CarriesDetail contract is the guard that this stays true.
