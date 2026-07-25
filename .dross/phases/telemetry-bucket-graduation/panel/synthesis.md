# synthesis — telemetry-bucket-graduation

Cold judge. I authored none of the three drafts. Every file path, symbol and
message shape below was checked against `internal/telemetry/telemetry.go`,
`internal/telemetry/telemetry_test.go`, `internal/cmd/stats.go`,
`internal/cmd/stats_test.go`, `internal/cmd/telemetry.go` and the live
`~/.claude/dross/telemetry.jsonl` (17,018 lines, 140 detail-carrying events,
39 distinct `err_detail` shapes). Drafts that named a shape the log or the
source does not contain are called out in **Source checks**.

## Scores

| dimension | risk (6t/4w) | mvp (3t/2w) | verification (5t/2w) |
|---|---|---|---|
| criteria coverage | 7/7; splits c-6 into reclassify-semantics + rendering, the cleanest reading of c-6. **Misses c-3's third named shape** — uses the milestone-merge refusal where c-3 says "missing-completed-record". | 7/7 on paper, but c-1..c-5 collapse into one task so per-criterion traceability is lost at the commit boundary; same c-3 third-shape miss as risk. | 7/7 with the tightest criterion→task map; **only lens that reads c-3's "missing-completed-record" correctly** (`origin/main carries no \`completed <slug>\` record`, confirmed live in the log ×2). |
| test-contract specificity | Strongest on *failure direction* — every contract states the mutation that breaks it (set-equality on `detailBuckets`, byte-identical log, deterministic tie order, "the last row fails if placed below `invalid`"). One contract rests on a shape that isn't in the log or the test table. | Adequate and corpus-grounded (real shapes, explicit `config_io`-after-`state_io` ordering), but contracts are stated as expected values, not as "reverting X fails row Y". Thinnest of the three. | Strongest on *ordering regressions* — the only draft that pins `no_milestone` / `no_plan` / `verify_state` against a `.toml`-matching `config_io` case, `--pr is required` staying `cli_args`, `ClassifyMessage("") == ""`, and anti-vacuity floors on the corpus (≥20 lines, ≥1 `want:"other"`). |
| granularity | 6 tasks; clean detail-posture seam, but t-1 is a whole task carrying no criterion and t-4/t-6 split one `stats.go` render path across two commits. Slightly over-cut. | 3 tasks; t-1 is five buckets + `detailBuckets` + package-doc prose in a single atomic commit. Under-cut — a revert of one bucket reverts all five. | 5 tasks, all criterion-aligned, split on the locked `detail_allowlist` seam; the extra `ClassifyMessage`/`Reclassify` task is the only one that makes the locked read-time rule unit-testable without stdout capture. Best fit. |
| wave correctness | 4 waves — the most serialized. The t-1 fence gates every edit and t-6 is chained behind t-4 for a shared-file reason, not a dependency reason. Weakest. | 2 waves and the sharpest single insight of the panel: the stats task needs no new bucket to compile or pass (it calls today's `ClassifyError`), so parking it in wave 2 buys nothing. | 2 waves, correctly places the extraction in wave 1, but over-declares t-4's deps as t-1+t-2+t-3 when only t-3 is real — mvp's insight applies and narrows it. |

**Skeleton: `verification`.** It has the only correct reading of c-3, the
tightest criterion→task mapping, contracts that pin the *ordering* hazards
(which is where this switch actually breaks), and a 2-wave graph. risk is a
close second and supplies most of the grafted contracts; mvp supplies the
dependency-narrowing insight and the `config_io` routing that survives the
c-4 reading.

## Merged plan

```
Phase telemetry-bucket-graduation — 5 tasks across 2 waves

Wave 1
  t-1  Graduate the identifier-only CLI shapes into detail buckets   [verification, +risk +mvp]
       files:    internal/telemetry/telemetry.go
                 internal/telemetry/telemetry_test.go
       covers:   c-1, c-2
       desc:     Add `arg_count`, `unknown_flag` and `landmark_parse` cases to the
                 CLI-surface section of the ClassifyError switch (above the generic
                 `invalid` / `missing` buckets); extend the existing
                 `unknown_subcommand` case to also match cobra's `unknown command`.
                 Add exactly the three new classes to `detailBuckets` and refresh the
                 package-doc allowlist prose.
       contract: [verification+mvp] TestClassifyError gains rows that fail if any shape
                 falls back to "other" — "accepts 3 arg(s), received 2",
                 "accepts 1 arg(s), received 0" (both live log shapes),
                 "requires at least 1 arg(s), only received 0" and
                 "accepts between 1 and 2 arg(s), received 3" → arg_count. Matching only
                 the exact-arity wording leaves the at-least and between variants in
                 "other" and fails on both rows.
       contract: [risk+verification] "unknown flag: --json", "unknown shorthand flag:
                 'x' in -x" and "flag needs an argument: --commit" → unknown_flag; and
                 `invalid argument "abc" for "--pr" flag` → unknown_flag — this last row
                 fails if the case is placed below the generic `invalid` case, because
                 `invalid` is reached first.
       contract: [risk+mvp] `unknown command "quick" for "dross"` → unknown_subcommand,
                 and the shipped row `unknown subcommand "add" for "dross phase"` still
                 → unknown_subcommand. Introducing a separate `unknown_command` bucket
                 fails the first assertion (bucket_granularity is locked to reuse).
       contract: [risk+mvp] `landmark pair "what=fixed the thing" has no '=' (want
                 key=value)`, `landmark pair "" has an empty key` and
                 `unknown landmark key "loco" (want feature/symbol/loc/what)` all →
                 landmark_parse; the third row fails if the case is placed below the
                 `unknown field` group, which would swallow it.
       contract: [risk] TestCarriesDetail is upgraded from a spot-check to set equality:
                 detailBuckets == {other, unknown_subcommand, unknown_field, arg_count,
                 unknown_flag, landmark_parse}. A missing entry (a graduated shape
                 silently loses its err_detail) and an extra entry both fail.
       contract: [verification] Detail(errors.New("unknown flag: --json")) still contains
                 "--json" and Detail on the landmark error still contains the rejected
                 pair — graduation must not blind the shape it graduated.
       contract: [verification] pre-existing rows still pass: "--pr is required" stays
                 cli_args (arg_count must not swallow it) and "unknown field: nonsense"
                 stays unknown_field.

  t-2  Add the detail-free merge, config and env buckets            [verification, +risk +mvp]
       files:    internal/telemetry/telemetry.go
                 internal/telemetry/telemetry_test.go
                 internal/cmd/telemetry_test.go
       covers:   c-3, c-4, c-5
       desc:     Extend the existing `merge_pending` case with the ship/complete refusal
                 shapes; add `config_io` (absent or unreadable project.toml / state.json,
                 TOML decode failure) placed AFTER `state_io`; add `env_token`, placed in
                 the CLI-surface section below the `board:` case. None of the three is
                 added to detailBuckets.
       contract: [risk+mvp+verification, merged] all four live refusal shapes →
                 merge_pending, and each fails independently —
                   "PR #55 for 03-x is not merged upstream — refusing to complete…"
                   "cannot confirm 03-x has merged into main — no merged-PR status was
                    available and origin/phase/03-x is not an ancestor of origin/main…"
                   "origin/main carries no `completed 54-…` record — has the PR merged
                    upstream? Refusing so the phase branch isn't lost"   (c-3's
                    missing-completed-record shape; live in the log, absent from
                    today's source — reachable only via read-time reclassify)
                   "origin/milestone/v1.15 is not merged into origin/main yet — has the
                    milestone PR merged?…"
                 Matching only the "cannot confirm" substring fails the other three rows.
       contract: [risk+mvp] `decode ~/proj/.dross/project.toml: toml: line 47 (last key…`
                 and `read ~/proj/.dross/state.json: open …: no such file or directory`
                 → config_io, while the shipped rows "save state: write .dross/state.json:
                 permission denied", "unmarshal state: bad json" and "marshal state:
                 cycle" still → state_io. A `config_io` case matching bare "state.json"
                 placed above `state_io` steals the first of those and fails it.
       contract: [verification] ordering-regression rows all still pass, and all three
                 fail if config_io's `.toml` matching is hoisted too early:
                 "load milestone .dross/milestones/v0.1.toml: open: no such file" stays
                 no_milestone, "decode plan plan.toml: bad toml" stays no_plan,
                 "verify.toml not found at .dross/phases/01/verify.toml" stays
                 verify_state, "read spec spec.toml: open: no such file" stays no_spec.
       contract: [mvp+verification] "$JIRA_API_TOKEN is not set; run `dross env set
                 JIRA_API_TOKEN` in your shell" → env_token, while "--pr is required",
                 "KEY must be non-empty" and "comment body is empty" still → cli_args,
                 and "board: create issue: $JIRA_API_TOKEN is not set" still → board —
                 that last row fails if env_token is inserted above the `board:` case.
       contract: [verification] TestCarriesDetail fails if merge_pending, config_io or
                 env_token is ever added to detailBuckets (locked detail_allowlist).
       contract: [risk] end-to-end at the call site: RecordCLIEvent given a merge_pending,
                 config_io or env_token error appends an event whose err_detail is the
                 empty string. If any of the three reaches detailBuckets, the recorded
                 event carries a phase id, a config path or a token name and this fails.

  t-3  Extract ClassifyMessage and add Reclassify                   [verification]
       files:    internal/telemetry/telemetry.go
                 internal/telemetry/telemetry_test.go
       covers:   c-6
       desc:     Extract the switch body into exported ClassifyMessage(string), with
                 ClassifyError delegating to it. Add Reclassify(Event) returning the
                 effective class: the stored class unless it is "other" AND ErrorDetail
                 is non-empty, in which case ClassifyMessage(ErrorDetail).
       contract: for every row in the TestClassifyError table,
                 ClassifyMessage(row.msg) == ClassifyError(errors.New(row.msg)) — a
                 delegation that drops a case fails on that row.
       contract: Reclassify({ErrorClass:"other", ErrorDetail:"unknown flag: --json"})
                 == "unknown_flag"; Reclassify({ErrorClass:"other"}) == "other" (no
                 detail → no reclassify); Reclassify({ErrorClass:"dirty_tree",
                 ErrorDetail:"unknown flag: --json"}) == "dirty_tree" — [risk] gating on
                 the presence of a detail instead of on the stored class fails this row;
                 Reclassify({}) == "".
       contract: ClassifyMessage("") == "" — the empty string must not fall through to
                 "other" and manufacture a phantom bucket.

Wave 2
  t-4  Reclassify `other` at read time and print the unclassified tail   [verification, +risk +mvp]
       files:    internal/cmd/stats.go
                 internal/cmd/stats_test.go
       covers:   c-6
       depends:  t-3                                    [narrowed per mvp — see D6]
       desc:     renderErrorBuckets counts telemetry.Reclassify(e) instead of
                 e.ErrorClass, then prints an indented block of the top 5 surviving
                 `other` detail strings with counts under the errors table, omitted
                 entirely when that set is empty. The log is never rewritten.
       contract: a stored {err:"other", err_detail:"unknown flag: --json"} event prints
                 under unknown_flag and the errors table shows no "other" row —
                 reverting to e.ErrorClass renders other=1 / unknown_flag=0 and fails
                 on both counts.
       contract: [risk] an `other` event whose detail still classifies as "other" is
                 counted exactly once; a fall-through that counts both the stored and
                 the re-derived class doubles the total and fails.
       contract: with 6 distinct residual shapes at counts 6/5/4/3/2/1, exactly the 5
                 highest print, in descending count order, each with its count, and the
                 count-1 string is absent — changing the cap to 4 or 6 fails it.
       contract: [risk] two residual shapes with equal counts print in a deterministic
                 (shape-string) order, so the assertion does not flake on Go map
                 iteration order.
       contract: with zero surviving `other` events the output contains neither the
                 tail heading nor any indented tail line — an unconditional header, or
                 a header with "(none)" under it, fails this.
       contract: [verification] TestStatsCover_renderErrorBucketsOrder still passes:
                 its events carry only ErrorClass and no err_detail, so reclassification
                 must be a no-op there.
       contract: [risk+verification] `dross stats show` over a seeded telemetry.jsonl
                 leaves the file byte-identical (os.ReadFile before/after) — enforces
                 the locked "the log itself is never rewritten".
       contract: [verification] a 240-rune err_detail renders on one truncated line so a
                 single pathological shape cannot wrap and destroy the block.

  t-5  Check in the err_detail corpus and enforce the <15% ceiling  [verification, +risk +mvp]
       files:    internal/telemetry/testdata/error_corpus.jsonl
                 internal/telemetry/corpus_test.go
       covers:   c-7
       depends:  t-1, t-2
       desc:     New fixture, one JSON object per line
                 {"detail":"<real redacted err_detail>","want":"<bucket>"}, curated from
                 the distinct shapes in ~/.claude/dross/telemetry.jsonl with home paths
                 already collapsed to "~". New table test drives ClassifyMessage over
                 every line.
       contract: every line classifies to its recorded "want"; a mismatch fails naming
                 the line number and both buckets — [risk] so re-bucketing
                 `landmark pair …` from landmark_parse to invalid fails as a named-shape
                 mismatch even though the `other` ratio is unchanged.
       contract: the share of lines classifying as "other" is < 0.15, and the failure
                 message prints the actual ratio plus the offending details — reverting
                 any one of t-1's or t-2's cases pushes it over the ceiling.
       contract: [verification] the fixture has >= 20 lines, each with a non-empty detail
                 and non-empty want; an emptied or truncated corpus fails loudly instead
                 of passing vacuously at 0/0.
       contract: [verification] at least one line has want == "other", so the ratio is
                 measured against a real distribution rather than a fixture tuned to
                 zero. Without this row the gate is unfalsifiable — see Source checks.
       contract: [risk] a malformed JSON line, or a line duplicating an existing shape,
                 fails the test rather than being skipped — a silently shrinking
                 denominator would let the 15% gate pass by attrition.
```

Coverage: c-1 → t-1 · c-2 → t-1 · c-3 → t-2 · c-4 → t-2 · c-5 → t-2 ·
c-6 → t-3 + t-4 · c-7 → t-5. 7/7. Every task carries at least one contract
that names the mutation which breaks it.

## Disagreements

**D1 — c-4 bucket routing: one `config_io`, or `config_io` + reuse of `state_io`?**
risk and mvp both route project.toml *and* state.json read failures into a new
`config_io`, keeping `state_io` for the save/marshal/unmarshal/load-state
shapes. verification routes state.json reads into the existing `state_io`,
arguing reuse follows the same precedent the locked `bucket_granularity`
decision set for unknown-command.
*Provisional default: one `config_io` covering both (risk+mvp).* c-4 says
"classifies into **a** named bucket" and enumerates project.toml, state.json
and TOML syntax errors as one family; splitting them puts half of one criterion
in a bucket whose own source comment defines it as "the workflow can't persist
progress" — a write-side meaning that a read failure does not have.
*Why it matters:* this decides whether c-4 can be verified by a single bucket
assertion or needs two, and it changes the switch ordering constraint —
`config_io` must sit **after** `state_io` either way, or it steals the shipped
`save state: write .dross/state.json: permission denied` row.

**D2 — a standalone regression-fence task before any classifier edit.**
risk makes t-1 a criterion-free task that turns TestClassifyError into an
ordered fence over every existing bucket assignment plus a documented
carve-out list, arguing the task that moves a shape must not own the test
meant to catch it. mvp and verification both distribute those rows into the
bucket tasks themselves.
*Provisional default: distributed (mvp+verification).* TestClassifyError is
already checked in and green with 40+ rows, so the "guard exists before the
edit" property already holds without a new task; and risk's justification
leans on an existing `invalid argument "abc" for "--pr" flag` → `invalid`
assignment that **does not exist** in the table (see Source checks), so there
is no existing assignment actually being carved out.
*Why it matters:* costs a wave of serialization and a criterion-free commit.
If the lead prefers risk's shape, the fence rows are already written above and
would move out of t-1/t-2 wholesale rather than being re-authored.

**D3 — `ClassifyMessage` / `Reclassify` as their own task.**
verification extracts the switch into an exported string classifier plus a
`Reclassify(Event)` helper in the telemetry package (t-3). risk and mvp both
do the read-time reclassify inline in `stats.go` by wrapping the detail in
`errors.New`.
*Provisional default: verification's extraction.* It puts the locked
`read_time_reclassify` rule under pure unit tests — empty detail, non-`other`
stored class, empty string — with no HOME/tempdir/stdout capture, and it is
the only place `ClassifyMessage("") == ""` can be asserted at all.
*Why it matters:* this is the difference between 5 tasks and 4, and it puts a
third wave-1 task on `internal/telemetry/telemetry.go`. Three same-wave tasks
editing one switch is exactly what mvp warned about; it is safe under dross's
sequential atomic-commit execution but would conflict under parallel agents.

**D4 — tail grouping: normalized shapes or raw `err_detail` strings?**
risk requires normalization (collapse quoted tokens, digits, `~`-paths) before
counting, arguing a raw group-by yields ~1 count per shape and the "top 5" is
noise. mvp and verification both reject it — no criterion asks for it, and
shape taxonomy is explicitly deferred to `telemetry-taxonomy-overhaul`.
*Provisional default: raw strings (mvp+verification).* Evidence favours them
harder than the vote does: after t-1 and t-2 land, **zero** of the 39 distinct
shapes in the live log remain in `other` (see Source checks). risk's own worked
example, `landmark pair "a" has no '='`, graduates to `landmark_parse` in t-1
and never reaches the tail.
*Why it matters:* normalization needs its own correctness tests (risk's
shared-prefix over-collapse contract) with no criterion behind them, and the
spec's `[[deferred]]` block names taxonomy as out of scope for this phase.

**D5 — which message is c-3's third shape?**
risk and mvp both use the milestone-merge refusal
(`origin/milestone/v1.15 is not merged into origin/main yet`) as the third
shape. verification uses `origin/main carries no completed <slug>` and argues
it must be matched even though no current code path emits it, because
read-time reclassify replays history.
*Provisional default: match all four.* c-3's "missing-completed-record"
literally names verification's shape, and the log confirms it (×2, and absent
from today's `internal/cmd/phase.go` — it is a shape an older binary emitted).
The milestone shape is also live (×1) and is a genuine merge refusal, so
dropping it would leave a real shape in `other`.
*Why it matters:* had the skeleton been risk or mvp, c-3 would have shipped
with its third named shape unmatched and the phase would have verified green
against a criterion it did not satisfy.

**D6 — where the stats work sits in the wave graph.**
risk puts it in wave 3+4 behind every bucket task. verification puts it in
wave 2 depending on t-1+t-2+t-3. mvp puts it in **wave 1**, arguing it calls
the existing `ClassifyError`, compiles today, and its contracts pass against
already-shipped buckets.
*Provisional default: wave 2, depending on t-3 only.* mvp's reasoning is
correct and has been grafted — t-4 has no dependency on the new buckets — but
under D3's extraction it does depend on `Reclassify` existing, which is a real
compile-time edge.
*Why it matters:* if D3 resolves against the extraction, t-4 becomes a wave-1
task with no dependencies at all and the phase collapses to two independent
wave-1 tracks.

**D7 — reclassify and tail-render: one task or two?**
risk splits them (t-4 reclassify, t-6 render), arguing the failure modes are
unrelated — double-counting and stored-class gating versus top-N selection —
and that combined, a rendering bug hides behind a passing bucket-count
assertion. mvp and verification keep them as one task on `stats.go`.
*Provisional default: one task (mvp+verification).* Both halves are edits to
`renderErrorBuckets` and its immediate output; risk's isolation argument is
answered by keeping the contracts separately falsifiable, which the merged t-4
does.
*Why it matters:* it is the difference between a 5-task and a 6-task plan, and
between 2 and 3 waves under risk's ordering.

**D8 — assert the privacy posture at the call site, or trust `CarriesDetail`?**
risk adds `internal/cmd/telemetry_test.go` assertions that a RecordCLIEvent
for a merge/config/env error appends an event with an empty `err_detail`.
verification explicitly rejects any call-site work, noting `RecordCLIEvent`
already gates on `telemetry.CarriesDetail` generically. mvp is silent.
*Provisional default: graft risk's assertions into t-2; no new task.* The
call-site gate is generic, so verification is right that no *code* change is
needed — but the locked `detail_allowlist` decision is a privacy commitment,
and the cheapest end-to-end proof that a phase id or token name never reaches
the log is to record one and read the line back.
*Why it matters:* the unit-level `CarriesDetail` assertion proves the map is
right; only the call-site assertion proves the map is what actually gates the
write.

## Source checks

Findings from reading the real source and the live log, which drove several
defaults above. No draft was modified.

- **`origin/main carries no \`completed <slug>\` record`** — live in the log
  (×2), **not present in `internal/cmd/phase.go`**. verification called it a
  historical shape and was right; it is reachable only through read-time
  reclassify. Drove D5.
- **`invalid argument "abc" for "--pr" flag`** — not in the log and **not a
  row in today's TestClassifyError table**. risk described it as an existing
  `invalid` assignment being deliberately carved out; there is no existing
  assignment. The contract is still worth keeping (the ordering hazard versus
  the generic `invalid` case is real) but it is a new row, not a move. Drove D2.
- **After t-1 and t-2, the live tail is empty.** All 39 distinct `err_detail`
  shapes in the 17k-line log are covered by the proposed buckets — the residual
  set is 0. Consequences: (a) c-7's <15% gate is trivially satisfiable at 0%
  from a log-harvested corpus, which is why verification's `>= 1 line with
  want:"other"` and `>= 20 lines` floors are load-bearing rather than
  decorative; (b) c-6's "top 5 of N" contract can only be exercised with
  synthetic events; (c) risk's normalization argument loses its live evidence.
  Drove D4 and t-5's anti-vacuity contracts.
- **`read …/.dross/state.json: …` does not match any existing `state_io`
  case** (which matches only `save state` / `marshal state` / `unmarshal
  state` / `load state`). verification's phrasing implies the routing is
  already in place; it is not — a new case is required either way. Drove D1.
- **`TestCarriesDetail`'s must-NOT list is a spot-check, not set equality.**
  risk's set-equality upgrade is a genuine strengthening and is grafted into
  t-1.
- **`internal/telemetry/testdata/` does not exist**; `corpus_test.go` and
  `error_corpus.jsonl` are both new files. `internal/cmd/telemetry_test.go`
  and `internal/cmd/stats_test.go` both exist, as does
  `TestStatsCover_renderErrorBucketsOrder` (its events carry `ErrorClass` only
  and no detail, so verification's no-op contract is accurate).
- **Ordering is safe by position, not by luck**: `no_spec`, `no_plan`,
  `no_milestone` and `verify_state` all sit above the CLI-surface and generic
  sections, so a `config_io` case added near `state_io` cannot steal them —
  but only if it is added there. verification's ordering-regression rows are
  the only contracts in the panel that pin this.
