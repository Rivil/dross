# risk lens — telemetry-bucket-graduation

Phase telemetry-bucket-graduation — 6 tasks across 4 waves

Lens: failure modes drive the graph. The dominant risks here are (a) a new
substring case silently stealing a shape from an existing bucket, (b) a
graduation stripping `err_detail` and losing diagnosability, or the inverse —
leaking phase ids / paths into a bucket that shouldn't carry text, (c) read-time
re-classification double-counting or classifying an empty string, and (d) the
c-6 tail collapsing (or failing to collapse) distinct shapes. Each is owned by
exactly one task.

```
Wave 1
  t-1  Fence existing bucket assignments before graduation
       files:    internal/telemetry/telemetry_test.go
       covers:   — (guard task; no criterion of its own)
       depends:  —
       desc:     Turn TestClassifyError's case table into an ordered fence over every
                 bucket ClassifyError names today, plus a documented carve-out list of
                 the shapes this phase deliberately re-buckets.
       contract: if a new switch case is inserted above an existing one and steals a
                 shape — e.g. a broad "is not set" case swallowing
                 `verify verdict is "" — fill in pass | partial | fail` — the fence
                 fails naming the stolen shape, its old bucket and its new one.
       contract: a shape that moves bucket without being listed in the
                 intentionally-rebucketed carve-out fails the fence; the only entry
                 this phase may add is
                 `invalid argument "abc" for "--pr" flag` : invalid → unknown_flag.
       contract: `something weird` still falls through to `other`; if a new case is
                 broad enough to catch an unrelated message, the default-branch
                 assertion fails.

Wave 2 (depends t-1)
  t-2  Graduate the detail-carrying CLI shapes
       files:    internal/telemetry/telemetry.go, internal/telemetry/telemetry_test.go
       covers:   c-1, c-2
       depends:  t-1
       desc:     Add `arg_count`, `unknown_flag` and `landmark_parse` cases to
                 ClassifyError, route cobra's `unknown command "x" for "dross"` into the
                 existing `unknown_subcommand`, and add the three new classes to
                 detailBuckets.
       contract: `accepts 1 arg(s), received 2`, `requires at least 1 arg(s), only
                 received 0` and `accepts between 1 and 2 arg(s), received 3` all return
                 `arg_count`; matching only the exact-arity wording leaves the at-least
                 and between variants in `other` and the table fails on both rows.
       contract: `unknown flag: --print-title`, `unknown shorthand flag: 'j' in -j`,
                 `flag needs an argument: --title` and
                 `invalid argument "abc" for "--pr" flag` all return `unknown_flag`; the
                 last row fails if the new case is placed below the generic `invalid`
                 case, because `invalid` is reached first.
       contract: `unknown command "quick" for "dross"` returns `unknown_subcommand` —
                 introducing a separate `unknown_command` bucket fails the assertion
                 (bucket_granularity is locked to reuse).
       contract: `landmark pair "what=fixed the thing" has no '=' (want key=value)` and
                 `landmark pair "" has an empty key` both return `landmark_parse`, and
                 Detail() on the first still contains the rejected pair.
       contract: CarriesDetail is pinned to EXACTLY
                 {other, unknown_subcommand, unknown_field, arg_count, unknown_flag,
                 landmark_parse} — both a missing entry (graduated shape loses its
                 detail) and an extra entry fail the set-equality assertion.

  t-3  Graduate the detail-free workflow, config and env shapes
       files:    internal/telemetry/telemetry.go, internal/telemetry/telemetry_test.go,
                 internal/cmd/telemetry_test.go
       covers:   c-3, c-4, c-5
       depends:  t-1
       desc:     Extend the existing `merge_pending` case with the three ship/complete
                 refusal shapes; add `config_io` (absent or unreadable project.toml /
                 state.json, TOML decode failure) and `env_token` ($X is not set).
                 None of these are added to detailBuckets.
       contract: all three refusal shapes return `merge_pending` —
                 `cannot confirm 03-x has merged into main — no merged-PR status was
                 available and origin/phase/03-x is not an ancestor of origin/main`,
                 `PR #55 for 03-x is not merged upstream — refusing to complete`, and
                 `origin/milestone/v0.10 is not merged into origin/main yet — has the
                 milestone PR merged?`; adding only the `cannot confirm` substring
                 fails the other two rows.
       contract: `decode ~/proj/.dross/project.toml: toml: line 4: expected '.' or '='`
                 and `read ~/proj/.dross/state.json: open …: no such file or directory`
                 return `config_io`, while `save state: …`, `unmarshal state: bad json`
                 and `marshal state: cycle` still return `state_io` — a `config_io`
                 case matching bare "state.json" steals the state_io rows and the t-1
                 fence fails.
       contract: "$JIRA_API_TOKEN is not set; run `dross env set JIRA_API_TOKEN`"
                 returns `env_token`, while `--pr is required`, `KEY must be non-empty`
                 and `comment body is empty` still return `cli_args` — the
                 `is not set` / `must be set` substrings must not collide.
       contract: RecordCLIEvent given a merge_pending, config_io or env_token error
                 appends an event whose `err_detail` is the empty string; if any of the
                 three is added to detailBuckets the recorded event carries the phase id
                 or the config path and the assertion fails.

Wave 3 (depends t-2, t-3)
  t-4  Re-classify stored `other` events at read time
       files:    internal/cmd/stats.go, internal/cmd/stats_test.go
       covers:   c-6
       depends:  t-2, t-3
       desc:     In renderErrorBuckets, an event with ErrorClass "other" and a non-empty
                 ErrorDetail is re-run through telemetry.ClassifyError and counted under
                 the returned class. The log file is never rewritten.
       contract: a stored `{err:"other", err_detail:"unknown flag: --json"}` event counts
                 under `unknown_flag` and NOT under `other`; skipping the reclassify
                 renders other=1 / unknown_flag=0 and the test fails on both counts.
       contract: an `other` event with an empty err_detail stays counted as `other` —
                 classifying the empty string must not invent a bucket or drop the row.
       contract: an `other` event whose detail still classifies as `other` is counted
                 exactly once; a fall-through that counts both the stored and the
                 re-derived class doubles the total and fails.
       contract: an event stored as `dirty_tree` that happens to carry a detail is never
                 re-classified — the reclassify must be gated on the stored class, not
                 on the presence of a detail.
       contract: the telemetry.jsonl bytes are identical before and after `stats show`
                 (read_time_reclassify is read-only; the log stays append-only).

  t-5  Check in the err_detail corpus and enforce the <15% ceiling
       files:    internal/telemetry/testdata/err_detail_corpus.jsonl,
                 internal/telemetry/corpus_test.go
       covers:   c-7
       depends:  t-2, t-3
       desc:     New testdata file of distinct real err_detail shapes, each with its
                 expected bucket; a table test asserts every row's classification and
                 that the share landing in `other` stays under 15%.
       contract: the ratio assertion fails at 15% — reverting the `arg_count` case pushes
                 the corpus over the ceiling and the failure message lists the shapes
                 that fell back to `other`.
       contract: each row is asserted individually, so re-bucketing `landmark pair …`
                 from landmark_parse to invalid fails as a named-shape mismatch even
                 though the `other` ratio is unchanged.
       contract: a corpus line that is malformed JSON, or duplicates an existing shape,
                 fails the test rather than being skipped — a silently shrinking
                 denominator would make the 15% gate pass by attrition.

Wave 4 (depends t-4)
  t-6  Print the unclassified `other` tail under the errors table
       files:    internal/cmd/stats.go, internal/cmd/stats_test.go
       covers:   c-6
       depends:  t-4
       desc:     After the errors table, group the still-`other` events by a normalized
                 detail shape (quoted tokens, digits and ~-paths collapsed), print the
                 top 5 by count as an indented block, and omit the section entirely when
                 the tail is empty.
       contract: with 7 distinct tail shapes recorded, exactly 5 lines print in
                 descending count order and the 6th/7th shape strings are absent from
                 the output.
       contract: `landmark pair "a" has no '='` and `landmark pair "b" has no '='`
                 collapse to one line with count 2; without normalization they render as
                 two count-1 lines and get pushed out of the top 5 by louder shapes.
       contract: two messages that merely share a prefix (`git fetch: exit 1` vs
                 `git fetch: auth failed`) stay two separate shapes — over-collapsing
                 fails this row.
       contract: when every `other` event re-classifies into a named bucket, the tail
                 heading string does not appear in the output at all — printing the
                 heading with "(none)" under it fails the omission assertion.
       contract: two shapes with equal counts print in a deterministic (shape-string)
                 order, so the assertion doesn't flake on Go map iteration order.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (cobra arg-count / unknown flag / unknown command) | t-2 |
| c-2 (landmark parse failure) | t-2 |
| c-3 (merge-confirmation refusal → merge_pending, 3 shapes) | t-3 |
| c-4 (absent/unreadable .dross config) | t-3 |
| c-5 (required-but-unset env token) | t-3 |
| c-6 (top-5 `other` tail, omitted when empty) | t-4 (unclassified semantics), t-6 (rendering) |
| c-7 (<15% `other` in the checked-in corpus) | t-5 |

7/7 criteria covered. t-1 carries no criterion — it is the regression fence the
graduation tasks land against.

## Judgment calls

- **Fence first (t-1) as its own wave.** Chose a standalone pre-existing-assignment
  fence ahead of any classifier edit; rejected folding the regression checks into
  t-2/t-3, because then the task that moves a shape also owns the test that was
  supposed to catch it. Costs one wave of serialization, buys a guard neither
  bucket task can weaken.
- **Grouped buckets by detail posture, not by criterion.** t-2 = every class that
  carries `err_detail`, t-3 = every class that must not. Rejected one task per
  criterion (5 tasks all editing the same `switch` in telemetry.go) — the shared
  failure mode here is the privacy/diagnosability split, so the split should follow
  it, and fewer concurrent edits to one switch means fewer accidental reorderings.
- **`invalid argument "x" for "--pr" flag` is a deliberate re-bucketing.** It
  classifies as `invalid` today and c-1's "incomplete flag" pulls it into
  `unknown_flag`. Chose to name it as the single carve-out entry in t-1's fence
  rather than let it move silently; rejected leaving it in `invalid` because
  "unknown or incomplete flag" is explicit in the criterion.
- **Reclassify (t-4) split from tail rendering (t-6)** despite both living in
  stats.go. The failure modes are unrelated (double-counting / empty-detail /
  stored-class gating vs shape normalization and top-N selection). Combined, a
  normalization bug hides behind a passing bucket-count assertion.
- **Tail grouping normalizes the detail before counting.** Rejected counting raw
  `err_detail` strings: every real shape embeds a phase id, a PR number or a path,
  so a raw group-by yields ~1 count per shape and the "top 5" is noise. The
  over-collapse risk is owned by the shared-prefix contract in t-6.
- **Corpus as JSONL rows with an expected bucket per row** (t-5), not a golden
  output blob. A regression should name the shape that moved, not print a diff of
  a ratio. Also keeps the fixture extensible for the next graduation, per the
  locked coverage_measurement decision.
- **Landmark detail is user-typed text.** `landmark pair "what=fixed the thing"`
  is closer to free-form than `--json` is, which brushes the privacy posture.
  The detail_allowlist decision is locked, so it stays — the risk is confined by
  pinning `detailBuckets` to an exact set (t-2) instead of an "at least contains"
  assertion, so nothing further creeps in.
- **No generic-bucket overlap audit.** `invalid`, `missing`, `git`, `network` and
  the order-dependence of the switch are deferred to telemetry-taxonomy-overhaul.
  t-1's fence only prevents *new* steals; it does not rationalize the old ones.
