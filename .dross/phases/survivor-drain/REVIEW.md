# Plan Review — survivor-drain

Reviewed: 2026-08-10 (second pass)
Plan: 20 tasks across 5 waves

## BLOCKING

- [antipatterns] The zero-covered package's survivors can never reach the classify path, so t-16's
  cmd/dross assertion and t-1's `TestZeroCoveredPackageIsMeasured` contradict t-3. `gremlins.go`
  `continue`s before `mergeInto` when `!hasCoverage(rep)` (gremlins.go:160-168), and t-3's contract
  freezes that behaviour: "contributes no file rows to the merged report". But t-1 requires a
  zero-covered package's survivors to "flow into the normal classify path", and t-16 requires
  `cmd/dross`'s two `main.go` guards (96, 102) to be killed — and `cmd_dross.json` is the one report
  in the repo with 0 KILLED and 2 NOT COVERED, i.e. the only package `hasCoverage` rejects. Under
  `--packages ./cmd/dross/...` the drain gets the merged report, sees zero rows for that package, and
  exits 0 with nothing outstanding. That is a vacuous green on the phase's headline criterion (c-1),
  produced by the mechanism the plan itself installs.
  Suggestion: state in t-1 which artefact the drain classifies from. If it is per-package raw reports
  rather than `Run()`'s merged output, say so and say how `--packages` obtains them; if it is the
  merged report, t-3 must expose the excluded rows (they are already parsed) instead of dropping them.

- [forbidden-actions] t-7 violates r-01 (severity `hard`) for the twelve tasks that consume it. t-7
  edits `internal/cmd/survivor_drain.go` and adds `internal/survivor/evidence.go`, and every wave-4
  task then shells out to `dross survivor drain` and asserts "matches its t-7 evidence". t-7 is the
  last binary-changing task before wave 4 and is the only one of the four Go-code tasks in the
  drain's dependency chain that does not say `make install` — t-1, t-2 and t-6 all cite r-01
  explicitly, and t-1's reason for doing so ("so wave 4 shells out to the real binary") is exactly
  the guarantee t-7 then breaks. Wave 4 would run against a binary with no evidence output, and the
  failure mode is silent: the drain still exits 0.
  Suggestion: add `Run make install (r-01)` to t-7's description, as t-1/t-2/t-6 already do.

## FLAG

- [antipatterns] Three sweep tasks name a test file that does not correspond to where their survivors
  live, and the omitted files mostly already exist:
    - t-17 lists `internal/statusline/render_test.go` only; 2 of statusline's 10 survivors are
      `settings.go` 34/36, and `internal/statusline/settings_test.go` exists.
    - t-18 lists `internal/codex/ast_grep_test.go` only; codex's 10 split ast_grep.go 4 / codex.go 5
      / refs.go 1, and `codex_test.go` exists (refs.go has no test file). It lists
      `quality/catalog_test.go` and `security/catalog_test.go`, but 7 of each package's 8 survivors
      are in `lifecycle.go`, which has no test file in either package — 14 of t-18's 29 survivors
      have no listed home.
    - t-19 lists `internal/ship/basepr_test.go`, but ship's single survivor is `comment.go:73` and
      `comment_test.go` exists; lists `internal/architecture/architecture_test.go`, but all 3
      survivors are `links.go` 121/129×2 and `links_test.go` exists; lists only
      `forge/github_test.go`, while 15 of forge's 20 survivors are `youtrack.go` (11) and `jira.go`
      (4) — `youtrack_test.go` and `jira_test.go` both exist. t-19's contract compounds this by
      naming "the existing httptest fakes in github_test.go" as the vehicle for the block it calls
      "the largest single block outside internal/cmd" — that block is youtrack.
  Suggestion: correct the `files` lists against the per-file survivor breakdown; where no test file
  exists (quality/security `lifecycle.go`, codex `refs.go`) name the file to be created.

- [antipatterns] Task descriptions enumerate survivor *lines* but state *mutant* counts, and the two
  disagree — every affected task is understated:
    - t-8 says "17 routed survivors"; doctor.go carries 15 (93, 95, 98 and 362 are ×2 each) and
      hints.go 8 (86 is ×4: two BOUNDARY, two NEGATION) — 23, not 17.
    - t-9: repair.go 133 is ×2 and statusline.go 189 is ×2 — 11 mutants, not the 9 the line list implies.
    - t-11: ship.go 550 and 552 are ×2 each — 6, not 4.
    - t-12: phase_lifecycle.go 23 and 25 are ×2 each — 5, not 3.
  The per-file `drain reports zero outstanding` assertions still catch the shortfall, so this is
  scoping error rather than correctness error, but it understates four tasks by ~35%.
  Suggestion: quote mutant counts, or mark the line lists as line-level and give the mutant total.

- [antipatterns] `env.go:148` (CONDITIONALS_NEGATION) is one of the 91 routed survivors and appears in
  no task. t-13 enumerates env.go 105/109/114/119 and its contract names only those four; the file's
  fifth survivor is unmentioned anywhere in the plan.
  Suggestion: add 148 to t-13's enumeration and to its test contract, or state why it is out of scope.

- [antipatterns] Two of the 91 internal/cmd survivors — `survivor.go:242` (×2 ARITHMETIC_BASE) and
  `deferred.go:143` — are owned by no wave-4 task. Both already carry acceptances in survivors.toml,
  which is presumably why, but `survivor.go:242` is the same double-mutant/single-acceptance shape
  t-17 explicitly treats as a failure for `store.go:157` ("a single acceptance covering only one of
  them fails"). If the identity key does not cover both mutants, that survivor surfaces first at
  t-20's repo-wide gate, in a task whose mandate is closing deferred entries, not writing tests.
  Suggestion: assign `survivor.go:242` to a wave-4 task (t-10 is the natural home — it is
  ARITHMETIC_BASE on string concatenation) or state in the plan that the 91 decompose as 88 to be
  drained plus 3 pre-existing acceptances, and that the 242 key covers both mutants.

- [granularity] The four sweep tasks are far larger than the kill tasks they sit beside: t-18 carries
  29 survivors across 8 source files in 4 packages, t-17 24 across 5 files in 3 packages, t-19 24
  across 6 files in 3 packages — while t-14 carries 5 and t-12 carries 10. t-18 and t-19 in
  particular each exceed the combined weight of t-12 + t-14 + t-9.
  Suggestion: split t-18 (catalog/lifecycle vs codex vs techdebt) and t-19 (forge alone is 20).
  Mitigating: quality/lifecycle.go and security/lifecycle.go survive at identical lines
  (37/42×2/46/57/61/65), so those 14 are almost certainly one fix pattern applied twice — worth
  saying so in the task, since it changes the split calculus.

- [granularity] t-5 bundles three separable deliverables across 6 files and 3 packages: the ceiling
  fixture + live-coverage proof, the string-concat no-op proof and the const-initializer evidence,
  and a repo-wide reason audit that also rewrites the live `gremlins-attribution-ceiling` reason in
  `.dross/survivors.toml`. The audit is an invariant nine downstream tasks lean on and is
  independent of the fixture work.
  Suggestion: consider splitting the reason audit (`internal/survivor/reasons_repo_test.go` + the
  store rewrite) out as its own wave-1 task.

- [test-contract] t-6's prompt assertion is not implementable as stated: "greps the WHOLE prompt — not
  just verdict branches — for \"0.80\", \"0.60\" and \"threshold\", failing on any surviving pass/fail
  use". A grep cannot distinguish a pass/fail use from prose. verify.md currently mentions thresholds
  at :32 ("mutation thresholds apply in §3"), :125-127, :129, :134 and :247 — and :247 is about
  capturing a user's threshold preference as a project rule, which is not a verdict lever.
  Suggestion: decide whether the word must be gone entirely (then say so, and t-6 must rewrite :32
  and :247 too) or name the permitted remainder explicitly.

- [test-contract] t-20's second assertion — "fails if any survivor-keyed deferred entry is routed to a
  phase that sits AFTER survivor-drain in the milestone's phases array" — leaves "survivor-keyed"
  undefined, and this phase's own spec.toml routes a deferred item forward to `mutation-score-truth`
  (index 11; survivor-drain is index 2) whose text is entirely about ARITHMETIC_BASE survivors. That
  routing is the deliberate counterpart of the locked `bogus_arithmetic_class` decision. A loose match
  makes the phase's headline test fail on its own locked decision.
  Suggestion: define survivor-keyed as "text resolves to a `file.go:line` survivor identity" and say
  in the contract that the phase's own two [[deferred]] entries are expected to pass.

- [antipatterns] t-9 and t-10 both write `internal/cmd/statusline_test.go` in wave 4 (t-9 for
  statusline.go:189×2, t-10 for statusline.go:23). The plan's header note serialises wave 4 only on
  `.dross/survivors.toml`; this collision is on a source file and is not covered.
  Suggestion: extend the note to name the t-9/t-10 pair, or move statusline.go:23 into t-9.

## NOTE

- [strengths] The inventory arithmetic is exact and checks out against the repo: 33 packages from
  `go list ./...`, 202 non-killed mutants across `reports/gremlins/*.json`, 91 in internal/cmd, 95
  deferred entries targeting survivor-drain decomposing as 91 survivor-keyed (survivor-lifecycle) + 4
  others (mutation-diff-scope[1] and [2], completion-state-truth[1], dross-repair[0]) — exactly as
  t-20 states. The four sweep tasks' package lists union with internal/cmd, internal/mutation,
  internal/verify and internal/rules to all 33, so the locked `drain_scope` decision is genuinely
  discharged rather than asserted.

- [strengths] Line-level claims were spot-checked and are correct, including the subtle ones:
  stryker.go:73 is the `if err != nil` whose `fs.ErrNotExist` discrimination is on 74, :80 is the
  post-parse check, :263 is the `r.Errors++` default arm; stryker_net.go:221 is the `Prefix == ""`
  branch; gremlins.go:155/:166 are the unreadable-report and zero-coverage concatenations;
  verify.md's threshold coaching is at 134 and 247. t-17's "store.go:157 carries two mutants, of
  which 5 of 6 survivor-package mutants are already accepted" is verified against survivors.toml.

- [strengths] The contracts are written against laundering rather than against green:
  `TestNoKillableSurvivorIsAccepted`, `TestMissingProfileIsUnknownNotUncovered`, "a dismissed deferred
  entry counts outstanding", "the drain cannot be closed by re-routing to itself", and the kill/accept
  splits that must sum to the recorded survivor count. These are the assertions that make c-3 real.

- [wave-order] The wave graph is tight — every dependency is load-bearing (t-1 needs t-3's Unmeasured
  field, t-7 edits t-1's file, wave 4 consumes t-7's evidence, t-20 gates on all of wave 4). No task
  could drop a wave for parallelism.

- [locked-decisions] No task contradicts a locked decision. t-3's expectation that covering
  gremlins.go 155/166 flips the four ARITHMETIC_BASE mutants to NOT VIABLE is an untested hypothesis
  about gremlins' compile step rather than a filtering action, and its contract already records the
  fallback into t-10's category — so `bogus_arithmetic_class` holds either way.

## Summary
Substantially stronger than a first draft — the inventory is verifiably exact and the contracts are
built to defeat laundering — but two mechanical defects would let it report green while lying: the
zero-covered-package path drops cmd/dross's survivors before classification, and t-7 skips the
`make install` that twelve downstream tasks depend on.
