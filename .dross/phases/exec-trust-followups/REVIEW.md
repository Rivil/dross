# Plan Review — exec-trust-followups

Reviewed: 2026-08-08
Plan: 10 tasks across 3 waves

## BLOCKING

- [coverage] c-3 says "a refused or failed forge request never puts the token value into
  the returned error" — but `internal/forge` has **four** independent token-carrying `do`
  implementations, each echoing the upstream body into its error, and t-3 touches only one:
    - `internal/forge/forge.go:676` (t-3 covers this one) — `token`/`Bearer`/`Basic base64(user:token)`
    - `internal/forge/github.go:346` — `GitHubClient.do`, `Authorization: Bearer <token>` (github.go:323)
    - `internal/forge/jira.go:534` — `JiraClient.do`, `Basic base64(email:token)` (jira.go:510)
    - `internal/forge/youtrack.go:466` — `YouTrackClient.do`, `Bearer <token>` (youtrack.go:443)
  No task's `files` list includes github.go, jira.go or youtrack.go. Worse, this can pass
  silently: t-9 seeds its canary into `remote.auth_env`, which is the ship/forge credential —
  the Jira and YouTrack clients read `board.auth_env`, so t-9 as described never drives them.
  So c-3 is either violated with a green build, or t-9 lands red in wave 3 with no task
  owning the fix.
  Suggestion: either add the three clients to t-3's files (they are the same one-line Scrub
  wrap at the same `HTTP %d%s: %s` site), or add a table row per client to t-9's contract and
  a task that owns them. Whichever way, the seeding must cover `board.auth_env` too, not just
  `remote.auth_env`.

## FLAG

- [files] t-7's contract row "if ship's `HTTP %d: %s` body snippet stops being scrubbed,
  TestHTTPSnippetRedacted fails — the server mirrors the Authorization header into its 400
  body" targets `jsonPost` at `internal/ship/open.go:144` (the only bare `HTTP %d: %s` in
  ship, and the only one that mirrors an `Authorization: token` header it set itself at
  open.go:132). `internal/ship/open.go` is not in t-7's files list. It *is* in t-5's — and
  t-5 and t-7 are both wave 2.
  Suggestion: add open.go to t-7's files and sequence t-7 after t-5, or move the jsonPost
  redaction into t-5's scope.

- [files] t-5 and t-7 both list `internal/ship/comment.go` and are both wave 2. Two parallel
  agents editing one file.
  Suggestion: make t-7 depend on t-5, or move the comment.go note-path redaction into t-7
  alone and the gh-argv work in comment.go into t-5 alone with an explicit ordering note.

- [files] t-4's ast-grep test rows cannot live in the one test file it lists.
  `runAstGrepFn` is unexported in package `codex` (`internal/codex/ast_grep.go:133`), so
  TestAstGrepArgvSeparator and TestAstGrepRejectsUnknownLang — which assert on captured argv
  *by index* — must sit in `internal/codex/*_test.go`, not `internal/mutation/argv_test.go`.
  Same problem for TestEveryCatalogToolHasAnArgvPolicy, which reads
  `internal/security/catalog.go` and has no listed home either.
  Suggestion: add `internal/codex/argv_test.go` and name where the catalog cross-check lives
  (argfence's own package is the natural home, since it owns the table).

- [files] t-6 pins the gated set as exactly {verify, task next, task status(in_progress),
  state bump, **changes record**}, and TestExecGatedSetIsExplicit asserts that set — but
  `changes record` lives in `internal/cmd/changes.go:28`, which is not in t-6's files list.
  The task as scoped cannot make its own pinning test pass.
  Suggestion: add `internal/cmd/changes.go`. Also worth confirming `changes record` actually
  spawns `runtime.test_command` — if it doesn't, drop it from the pinned set instead.

- [granularity] t-4 is 5 files across three packages (`internal/mutation` ×3,
  `internal/codex`, plus a catalog cross-check) implementing *two different policies* —
  Separator for ast-grep, Reject for gremlins/npx/dotnet. Its 10-row contract is doing two
  tasks' work.
  Suggestion: split into "fence the Reject-policy mutation runners" (mutation ×3) and "fence
  ast-grep behind `--` + validate lang" (codex). Both still depend only on t-1, so they stay
  in wave 2 and gain parallelism.

- [antipattern] t-8's non-literal-binary row names three sites — gremlins.go:224,
  stryker.go:144, stryker_net.go:105 — but there is a fourth in scope:
  `internal/cmd/update.go:197`, `exec.Command(newBinary, "install")`. Under t-8's own
  fail-closed rule ("the default is flag, not pass") this is a finding the moment the gate
  runs, and it is neither a resolvable builder function nor named in the accepted-with-reason
  header.
  Suggestion: name update.go:197 in the header with its reason (self-exec of the just-verified
  downloaded binary, argv all-literal) so the executor doesn't improvise the exception
  mid-task. Sweep for others: `internal/cmd/statusline.go:275` passes a `full...` slice to git
  and will need the same treatment if the generalised walk stops recognising it.

- [test contract] t-8's "snippet floor of 16" is below what the task itself produces. The
  current `testdata/gitargs_audit/snippets.txt` already holds 13 FLAG/PASS rows, and t-8's
  contract enumerates roughly eight new ones (ast-grep ×2, gremlins ×2, gh ×2, the dotnet
  option-argument carve-out ×2, unknown-binary, non-literal-binary). A floor of 16 would let
  five of the new rows be deleted without failing.
  Suggestion: set the floor to the real post-task count, the way the existing gate's
  self-check does.

- [wave order] t-5 declares `depends_on = ["t-1"]` but nothing in its six contract rows
  references argfence — no `PolicyFor`, no `ErrLeadingDash`, no `Fence`. Every row is literal
  `--` placement, flag/value index walking, or a `PRNumber <= 0` check, none of which needs
  t-1's table. As written t-5 could run in wave 1.
  Suggestion: either drop the dependency and move it to wave 1 (t-8 already depends on both,
  so the red-then-green ordering survives), or add a contract row that actually exercises
  `gh`'s Separator policy through argfence so the dependency is real and tested.

## NOTE

- [test contract] t-8 says "the five git-specific tests fail" — `gitargs_audit_test.go` has
  four top-level tests (TestNoUnseparatedGitPositional, TestAuditFlagsItsOwnSnippets,
  TestAuditFlagsBarePrefixlessVar, TestAuditScansCodexPackage). The count in the contract is
  off by one; the three it names by description all exist.

- [files] t-8 describes a rename but lists only the new paths. The old
  `internal/cmd/gitargs_audit_test.go` and `internal/cmd/testdata/gitargs_audit/snippets.txt`
  are deleted by this task and aren't recorded anywhere in the plan.

- [locked-decision] No conflicts found. Worth recording the one place the mapping is
  implicit: `exec_consent_gate` names "`dross verify` / `dross quick` / phase execute", and
  t-6 gates a set of five *CLI* commands instead — because quick and execute are prompts, not
  binaries. The decision's own reasoning already anticipates this ("the CLI cannot stop an
  agent invoking `go test` directly"), and t-10 adds the prompt pre-flight for the other half.
  Consistent, but the plan never states the mapping; a reader diffing the locked set against
  the gated set will think something was dropped.

- [spec text] c-2 reads "stryker (Go and .NET)". In the code stryker is npx/JS-TS
  (`internal/mutation/stryker.go`) and dotnet (`stryker_net.go`); gremlins is the Go one. The
  plan reads it correctly as npx + dotnet — the imprecision is in the spec, not the plan.

- [strengths] Every repo reference the plan makes is accurate. Verified: `readAllowHosts` at
  `internal/cmd/local.go:97`, semgrep as a LookPath-only catalog entry at
  `internal/security/catalog.go:55`, the ARCHITECTURE.md landmark lines at 181-182, and the
  three `exec.Command(args[0], args[1:]...)` sites at gremlins.go:224 / stryker.go:144 /
  stryker_net.go:105. Authoring from the code rather than from memory is visible throughout.

- [strengths] The test contracts are the strongest part. Almost every row names the function,
  the concrete input, and — unusually — *why the failure mode matters*: "an empty report would
  read as 'no surviving mutants' and pass verify", "a demoted --body posts as a positional, a
  silent wrong-content bug rather than a crash", "the default is flag, not pass". Not one
  vague contract in ten tasks.

- [strengths] t-9 being its own task rather than folded into t-3/t-7 is the right call, and
  the plan says why: "t-3 and t-7 can both be correct while a fourth code path prints the
  token." Same instinct behind ordering t-5 before t-8 so the gate is observed red-then-green.

- [rules] No forbidden actions. r-01 is explicitly acknowledged in t-10 (prompt edits need
  `make install`), and t-8's accepted-with-reason header is exactly r-02's shape rather than a
  rotting exception list. Runtime is native Go; no runtime-mode conflicts.

## Summary

Structurally strong plan with exceptional test contracts, but c-3 is scoped to one of four
token-carrying forge clients — fix that and the six file-list/ownership gaps before executing.
