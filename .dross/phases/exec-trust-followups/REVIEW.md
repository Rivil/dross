# Plan Review — exec-trust-followups

Reviewed: 2026-08-08 (second pass, post-amendment)
Plan: 11 tasks across 3 waves

## BLOCKING

- [test contract] t-5's `TestPRStatusArgv` asserts the wrong argv and would ship a broken
  command behind a green test. The row demands
  `[pr view -- 12 --json state,mergedAt,baseRefName]`. In pflag — which cobra, and therefore
  `gh`, uses — `--` is not a scoped fence around the next token: on encountering it the parser
  appends **all** remaining args to the positional list and breaks out of flag parsing
  entirely. So `--json` and `state,mergedAt,baseRefName` become the 2nd and 3rd positionals,
  and `gh pr view` (one positional max) errors out. Because `ghCommand` is a test double
  (`internal/ship/open.go:66`), the test asserts the captured slice and passes while
  `dross ship`'s merge-status path is dead at runtime — the false-green shape exactly.
  The correct shape is flags first, operand last: `[pr view --json state,mergedAt,baseRefName -- 12]`.
  t-5's own `PostComment` row gets this right (`[pr comment --body <body> -- <number>]`) and
  even explains why, so the rule is understood — it is just misapplied one row up. t-8 then
  copies the defect into the gate's PASS exemplar (`ghCommand("pr","view","--",num)`), which
  would bless the broken ordering repo-wide.
  Suggestion: fix the asserted argv in `TestPRStatusArgv` to put `--json` before the separator,
  and fix t-8's gh PASS snippet to the same shape. If the gate is to be useful here it should
  distinguish "separator present" from "separator present and no flag follows it".

## FLAG

- [test contract] t-8's ARCHITECTURE.md row is unfalsifiable. It claims "if ARCHITECTURE.md's
  auditFile and gate-test landmark lines are not updated … architecture_test.go's landmark
  check fails". `internal/cmd/architecture_test.go` has three tests — `TestArchitectureCheckFix`,
  `TestArchitectureCheckNoFixWritesNothing`, `TestArchitectureCheckRegistered` — and all three
  run against a synthetic ARCHITECTURE.md written into a temp dir (the helper at
  architecture_test.go:14-27), never the repo's own document. Nothing in the suite reads
  `/ARCHITECTURE.md`'s landmark bullets, and `dross architecture check` appears in neither the
  Makefile nor CI. Deleting `gitargs_audit_test.go` and leaving ARCHITECTURE.md:181-182 pointing
  at it leaves the build green.
  Suggestion: either name a gate that actually exists (a test asserting every `loc` bullet in
  the repo's ARCHITECTURE.md resolves to a real file), or drop the row and state that the doc
  update is executor discipline, not an enforced contract.

- [coverage] t-7's one-row-per-backend table under-covers c-3 by roughly a factor of three.
  `internal/ship` has ~15 error sites that interpolate an upstream body, not 5:
  gitlab.go:65, :73, :88, :114, :151; bitbucket.go:125, :191, :203, :218;
  forgejo.go:98, :136; comment.go:124, :153; open.go:144. The three backends each funnel
  through a shared request helper (bitbucket.go:75, forgejo.go:182, gitlab.go:220) that returns
  the raw body, so the Scrub must land at each `Errorf`, not in one place per file. "One table
  row per backend" therefore proves one site per file and leaves the rest — including
  `gitlab.go:73` ("response missing iid") and the two reviewer-assignment paths — able to leak
  with the table green. t-3 has precisely the guard this needs (`TestEveryForgeDoRedacts`
  enumerates the package's `do` methods so a fifth client cannot join unscrubbed); t-7 has no
  equivalent.
  Suggestion: add a t-7 row that enumerates rather than samples — every `string(respBody)` /
  `string(rb)` interpolation in `internal/ship` must pass through Scrub, failing by file:line
  otherwise. That also makes t-9's broad canary sweep a second opinion instead of the only net.

- [coverage] t-6's description misstates the mechanism, in a way that will misdirect the
  executor. It says the gate lands "at the loop commands that spawn runtime.test_command" —
  none of the five do. The CLI reads `Runtime.TestCommand` in exactly one place,
  `dockerPrefix` at `internal/cmd/verify.go:225`, and only to derive a `docker compose exec`
  prefix under `runtime.mode = "docker"`. `dross verify` spawns gremlins/npx/dotnet;
  `task next`, `task status`, `state bump` and `changes record` spawn nothing at all. The
  gate is a loop-*chokepoint* gate, not an exec-site gate — which is what the locked
  `exec_consent_gate` decision actually endorses, so there's no conflict — but an executor
  reading the description will go looking for exec sites that do not exist.
  Suggestion: restate as "the loop commands whose success implies the agent is about to run
  test_command". Separately, reconsider `changes record`: it is pure post-hoc bookkeeping
  (`internal/cmd/changes.go:24-73` loads changes.json, records, saves) and gating it sits
  awkwardly against t-6's own `TestGateDoesNotBrickReadOnly` row, which pins the gate as not
  spreading to post-hoc commands.

- [coverage] the `not-applicable` state is an unpinned hole in the gate. t-2 pins
  "an empty `runtime.test_command` yields state not-applicable", and t-6 never says what a
  gated command does in that state. Under absent test_command, `dross verify` still calls
  `configuredAdapters` (verify.go:67, :178) and spawns gremlins against the repo, which runs
  the untrusted repo's Go tests — the code execution the gate exists to prevent, reachable by
  a hostile `.dross/` that simply leaves `test_command` blank. c-1's literal wording only
  covers running test_command, so this is outside the criterion rather than a coverage
  failure — but it is a one-line bypass of the machinery this whole phase builds.
  Suggestion: add a t-6 contract row pinning the behaviour explicitly (refuse, or proceed with
  a stated reason). Do not leave it for the executor to infer.

- [coverage] c-2's semgrep leg is nominal, not real. There is no semgrep spawn anywhere in Go —
  `internal/security/catalog.go:55-56` is a `LookPath` availability entry, and the only actual
  invocations are agent-driven from `assets/prompts/secure.md:72-78`. t-1 covers the leg with a
  table entry plus `TestEveryCatalogToolHasAnArgvPolicy`, which makes the criterion vacuously
  true and future-proofs the day a call site appears — the plan says this outright in t-1's last
  row, so it is honest rather than hidden. But no task touches secure.md, so where semgrep is
  genuinely run with derived arguments today, c-2 buys nothing.
  Suggestion: either add the `--` guidance to secure.md alongside the existing `--metrics=off`
  line, or record in the spec that c-2's semgrep leg is deliberately forward-looking.

- [antipattern] t-8's non-literal-binary row names half the sites. It lists
  gremlins.go:224, stryker.go:144, stryker_net.go:105 — but every one of those files has a
  *second* non-literal spawn on the other branch of the same builder:
  `internal/mutation/gremlins.go:228`, `internal/mutation/stryker.go:148`,
  `internal/mutation/stryker_net.go:109`, all `exec.Command(full[0], full[1:]...)`. Same shape,
  same fail-closed rule, unnamed.
  Suggestion: name both branches per file, or describe the shape (`exec.Command` whose binary
  operand is an index expression) rather than enumerating line numbers.

- [test contract] t-8's snippet floor of 21 is still under the count the task produces.
  Verified: `internal/cmd/testdata/gitargs_audit/snippets.txt` holds 13 rows today (6 FLAG,
  7 PASS). t-8's own contract enumerates ten new ones — ast-grep ×2, gremlins ×2, gh ×2, the
  dotnet option-argument carve-out ×2, unknown-binary, non-literal-binary — for 23. A hardcoded
  21 re-creates the slack the first pass objected to, just two rows deep instead of five.
  Suggestion: derive the floor from the file at write time, or state it as "equal to the row
  count, updated with the table" rather than a number chosen in advance.

- [granularity] t-6 is 6 files doing two separable jobs: building the `dross trust` command
  (+ `--check`, + wiring into main.go) and threading a refusal through five pre-existing
  commands. Its 9-row contract splits cleanly along that seam. (t-3, t-5, t-7 and t-8 are also
  5-6 files each, but each is one repeated one-line edit per file with a per-file contract row —
  those read as correctly sized, not as split candidates.)
  Suggestion: consider "add `dross trust` + `--check`" and "gate the loop commands" as two
  tasks; the second depends on the first and both stay in wave 2.

## NOTE

- [coverage] `docker` is a subprocess dross spawns that t-1's key set
  {git, gh, gremlins, npx, dotnet, ast-grep, semgrep} omits. Under `runtime.mode = "docker"`,
  `dockerPrefix` (verify.go:221-248) makes the effective argv[0] `docker` at gremlins.go:224 /
  stryker.go:144 / stryker_net.go:105. The static gate sees a non-literal binary there so t-8's
  non-literal rule catches the site, and the runtime `Fence` still applies the gremlins/stryker
  policy — so nothing is unguarded. But a strict reading of c-5's "every subprocess dross
  spawns" includes docker, and the table does not name it.

- [wave order] No ordering bugs. Verified file-disjointness within every wave (w1: argfence /
  cmd·trust+local / redact+forge / ship-gh; w2: mutation / codex / cmd·gate / ship-redact;
  w3: three distinct files) and every `depends_on` edge is load-bearing — including t-7→t-5,
  which the plan states is a file-conflict edge rather than a data edge.

- [spec text] c-2 reads "stryker (Go and .NET)". In the code stryker is npx/JS-TS
  (`internal/mutation/stryker.go`) and dotnet (`stryker_net.go`); gremlins is the Go one. The
  plan reads it correctly as npx + dotnet. Carried over from the first pass — the imprecision
  is in the spec, not the plan.

- [locked-decision] No conflicts. All four locked decisions are honoured, including the one
  place the mapping is implicit: `exec_consent_gate` names "`dross verify` / `dross quick` /
  phase execute" and t-6 gates five CLI commands instead, because quick and execute are prompts.
  t-10 covers the prompt half. The decision's own reasoning anticipates the split.

- [rules] No forbidden actions. r-01 is acknowledged in t-10 (prompt edits need `make install`).
  t-8's accepted-with-reason header is r-02's shape rather than a rotting exception list, and
  naming update.go:197 in advance is exactly r-02's "route it the same run". No global
  `~/.claude/dross/rules.toml` exists.

- [strengths] Repo accuracy is again near-total. Independently re-verified: the four `do`
  implementations at forge.go:621 / github.go:305 / jira.go:493 / youtrack.go:426 with their
  distinct auth schemes and identical 500-char snippet formats; `readAllowHosts` at
  local.go:97; `runAstGrepFn` unexported at codex/ast_grep.go:133 with the file operand bare at
  :139; jsonPost's self-set `Authorization: token` at open.go:133 echoed at open.go:144;
  the 13 existing snippet rows; ARCHITECTURE.md:181-182. The only two claims that did not hold
  are the two flagged above.

- [strengths] The amendments are surgical rather than defensive. The t-4 → t-4/t-11 split is
  justified by a real language constraint (unexported `runAstGrepFn` cannot be asserted
  by-index from `internal/mutation`) and is stated in the task description; the t-7→t-5
  dependency is documented as a file-conflict edge rather than disguised as a data edge; t-5's
  argfence dependency was dropped and it moved to wave 1, and no row in it references argfence,
  so the drop is correct.

- [strengths] The contracts still say *why* a failure mode matters, which is rare and load-
  bearing here: "an empty report would read as 'no surviving mutants' and pass verify",
  "a demoted --body posts as a positional, a silent wrong-content bug rather than a crash",
  "the default is flag, not pass". Two of this review's findings (the gh argv, the ship
  under-coverage) were only findable *because* the contracts are specific enough to be wrong.

## Resolved since first pass

- c-3's four-client gap: t-3 now lists github.go, jira.go and youtrack.go alongside forge.go,
  and adds `TestEveryForgeDoRedacts` enumerating the package's `do` methods so a fifth client
  cannot join unscrubbed. Verified all four sites are the same shape.
- t-9 now seeds both `remote.auth_env` and `board.auth_env`, with the reason stated, so the
  Jira and YouTrack clients are actually driven.
- `internal/ship/open.go` added to t-7's files, and the jsonPost/open.go:144 row now has a home.
- The t-5/t-7 parallel edit of comment.go is resolved by `depends_on = ["t-3", "t-5"]`.
- t-4's ast-grep rows rehomed into t-11 with `internal/codex/argv_test.go`;
  `TestEveryCatalogToolHasAnArgvPolicy` rehomed into t-1's argfence package with a stated reason.
- `internal/cmd/changes.go` added to t-6's files (though see the FLAG on whether it belongs in
  the gated set at all).
- `internal/cmd/update.go:197` is now named in t-8's accepted-with-reason header with its
  reason, and statusline.go:275 is called out.
- t-8's git-test count corrected from five to four; verified `gitargs_audit_test.go` has exactly
  four top-level tests (:211, :251, :311, :341).
- t-8 now records that the two `gitargs_audit` paths are deleted by the rename.
- t-5 dropped its unreal t-1 dependency and moved to wave 1.

## Summary

The amendments landed cleanly and every first-pass finding is genuinely addressed, but the new
t-5 contract pins an argv that pflag will not parse the way it assumes — a `gh pr view` that
fails at runtime while its test stays green — and two coverage gates (ship's ~15 leak sites,
ARCHITECTURE.md's landmark rows) are weaker than their contracts claim.
