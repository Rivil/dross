# Plan Review — telemetry-bucket-graduation

Reviewed: 2026-07-25
Plan: 5 tasks across 2 waves

## BLOCKING

(none)

Coverage is complete (c-1,c-2→t-1; c-3,c-4,c-5→t-2; c-6→t-3,t-4; c-7→t-5), all five
locked decisions are honored to the letter — including the negative ones (t-2 explicitly
asserts merge_pending/config_io/env_token must *not* enter detailBuckets, t-4 asserts the
log is never rewritten) — and nothing in the plan implies a forbidden action under
`runtime.mode = "native"` or r-01.

## FLAG

- [wave-order] t-5's declared `depends_on = ["t-1", "t-2"]` omits t-3, but its test drives
  `ClassifyMessage`, which does not exist until t-3 extracts it. The wave boundary happens
  to save this (t-3 is wave 1, t-5 is wave 2), so it will not break a straight-through run —
  but the graph is wrong, and `--from t-5` on a resume would start against a plan that
  claims its prerequisites are t-1/t-2 only.
  Suggestion: add t-3 to t-5's `depends_on`.

- [antipattern] t-1's contract row `invalid argument "abc" for "--pr" flag` -> `unknown_flag`
  is scope creep that costs more than it buys. It is neither an unknown nor an incomplete
  flag (c-1's wording), it appears zero times in the live log (58 distinct err_detail shapes
  sampled), and it is the *only* reason `unknown_flag` must be ordered above the generic
  `invalid` case — the two shapes that do occur (`unknown flag: --version`, `flag needs an
  argument: --commit`) contain neither "invalid" nor "missing" and need no ordering guard at
  all. Worse, `"abc"` there is arbitrary user-supplied flag *value*, not a bounded CLI token,
  which is the stated basis for admitting `unknown_flag` to `detailBuckets`.
  Suggestion: drop that row and the ordering constraint it forces, or state why a flag-value
  parse error belongs in a bucket named `unknown_flag`.

- [coverage] No task touches README.md, which at line 344 enumerates the complete bucket
  list ("...`state_io`, `already_exists`, `invalid`, ... `other`") and at 344/346 states the
  privacy posture as "The one exception is the catch-all `other`: it carries a redacted...
  copy of the message". This phase adds five buckets and three detail-carrying classes; both
  statements become factually wrong, and the second is a user-facing privacy claim, not
  incidental prose. (It is already mildly stale — `unknown_subcommand`/`unknown_field` carry
  detail today.)
  Suggestion: fold the README bucket list + detail-allowlist sentence into t-1 or t-2's files.

- [test-contract] t-5's guard "at least one line has `want == \"other\"`" cannot be satisfied
  from the source the description names. I classified all 58 distinct err_detail shapes in
  `~/.claude/dross/telemetry.jsonl` (117 detail-carrying events) against the post-t-1/t-2
  switch: every one lands in a named bucket. The residual is ~0%, so the `other` sentinel row
  must be synthetic, contradicting "curated from the distinct shapes in the live telemetry
  log". Corollary: c-7's 15% ceiling passes with ~15 points of slack and is not the real gate —
  the per-line `want` assertions are.
  Suggestion: say in t-5's description that the `other` row is a deliberate synthetic sentinel,
  so a future reader doesn't delete it as a curation error.

- [test-contract] t-4's contract asserts "a 240-rune err_detail renders on one truncated line",
  but neither t-4's description nor c-6 specifies a display width — the executor has to invent
  one, and the test will be written to whatever it invents. Related: 14 live events carry
  multi-line details (`unknown subcommand "get" for "dross state"\n\nDid you mean this?\n\tset`),
  and `telemetry.Detail` preserves newlines, so a surviving `other` with an embedded newline
  will break the indented block. Neither the description nor the contract addresses that.
  Suggestion: name the display cap in t-4's description and add a newline-collapse row to the
  contract, or drop the truncation row.

- [locked-decision] The `detail_allowlist` rationale calls landmark parse "identifier-only...
  bounded CLI tokens". The live log says otherwise: `landmark pair "applied in t-10) lifetimes
  made env-configurable via pure resolve*Ttl helpers" has no '=' (want key=value)` — free-form
  authored prose. t-1's contract actively requires retaining it ("Detail on the landmark error
  still contains the rejected pair"). This is net-neutral against today's behavior (the shape
  already stores detail under `other`), and the decision is locked, so the plan is compliant —
  but by the spec's own standard ("merge/config/env messages embed phase ids and paths, which
  the privacy posture keeps out of the log") landmark prose is a larger exposure than a phase id.
  Suggestion: no plan change needed; re-read the decision before executing, since the plan will
  bake this in behind a passing test.

## NOTE

- [coverage] t-3 declares `covers = ["c-6"]` but its contract asserts nothing about `dross stats`
  output — c-6 is entirely a rendering criterion, delivered by t-4. t-3 implements the
  `read_time_reclassify` *decision*, not the criterion. Dual coverage is legal, but a
  criterion→test map at verify time will point c-6 at `ClassifyMessage`/`Reclassify` unit tests
  that never render anything.

- [wave-order] t-1, t-2 and t-3 all sit in wave 1 and all edit `internal/telemetry/telemetry.go`
  — specifically the same `ClassifyError` switch, which t-3 relocates wholesale into
  `ClassifyMessage`. `/dross-execute` runs tasks one at a time via `dross task next`, so this is
  not a conflict hazard, and id order (t-1→t-2→t-3) works fine. Only worth knowing if the tasks
  are ever run out of order: t-3 first would move the edit sites t-1/t-2's descriptions point at.

- [antipattern] `ARCHITECTURE.md:417` anchors `telemetry.ClassifyError` at
  `internal/telemetry/telemetry.go:210`; t-3's extraction moves the switch body out from under
  that line number. Low stakes — the architecture refresh regenerates it — but the phase will
  ship with the anchor stale.

- [antipattern] `internal/telemetry/telemetry.go:68` documents `ErrorDetail` as "ONLY when
  ErrorClass == 'other'", which is already false today. t-1 says "refresh the package-doc
  allowlist prose"; it is ambiguous whether that includes this struct field comment.

- [strength] The test contracts are the best part of this plan. They are mutation-shaped rather
  than descriptive: each names the exact ordering hazard and the exact row that fails
  (`if config_io is placed above state_io... the shipped row "save state: write .dross/state.json:
  permission denied" -> state_io is stolen`). Nothing here would pass check 3.

- [strength] The ordering-regression rows show the author actually read the shipped switch and
  its precedence, not just the spec: `board` vs `env_token`, `unknown_field` vs `landmark_parse`,
  and the four-row guard that `config_io`'s `.toml` matching not be hoisted above
  no_milestone/no_plan/verify_state/no_spec. Those are the exact four cases that would have been
  silently stolen.

- [strength] t-2's claim that the `origin/main carries no \`completed <slug>\` record` shape has
  no live emitter and is reachable only through read-time reclassify checks out — that string
  appears nowhere under `internal/`, only in the historical log. That is a verified, non-obvious
  observation, and it is what makes the `read_time_reclassify` decision load-bearing rather than
  decorative. t-5's anti-vacuity guards (>= 20 lines, fail-on-malformed rather than skip) show
  the same instinct.

## Summary

Structurally sound and unusually well-tested for a plan of this size — no blocking issues; the
flags are a missing `depends_on`, an untouched README that carries a now-false privacy claim,
one invented test row that drags an unneeded ordering constraint with it, and a corpus sentinel
that the live data cannot actually supply.
