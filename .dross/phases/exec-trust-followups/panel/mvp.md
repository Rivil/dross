# Panel draft — MVP lens

Phase exec-trust-followups — 6 tasks across 2 waves

Wave 1

  t-1  Add trust store and `dross trust` command
       files:    internal/cmd/trust.go, internal/cmd/local.go, cmd/dross/main.go
       covers:   c-1
       desc:     New closed local.toml key `trusted_test_command` holding a sha256 of the
                 consented `runtime.test_command`. `dross trust` grants it after printing the
                 command; `dross trust check` exits non-zero when not granted. Consent state is
                 computed by one exported-to-package helper the enforcement sites call.
       contract: TestConsentStates pins four untrusted cases separately — no local.toml at all,
                 local.toml present but key absent, key present holding the hash of a DIFFERENT
                 test_command (the rewritten-command revocation), and a local.toml git reports
                 tracked (reusing readAllowHosts' ls-files refusal). Granting then editing
                 project.toml's test_command by one character flips the state back to untrusted.
                 TestTrustGrantWritesHash asserts the stored value equals sha256(test_command)
                 and is not the command itself.

  t-2  Fence analyzer argv per locked per-tool policy
       files:    internal/mutation/argsafe.go, internal/mutation/gremlins.go,
                 internal/mutation/stryker.go, internal/mutation/stryker_net.go,
                 internal/codex/ast_grep.go
       covers:   c-2
       desc:     `noDash(tool, value)` rejects a leading-dash derived value for the
                 no-end-of-options tools; buildUnleashArgs / runArgs / StrykerNet.Run route every
                 derived value (package paths, --mutate entries, output dir) through it and
                 return an error instead of an argv. ast-grep gets a literal `--` immediately
                 before its file positional.
       contract: Gremlins.Run over a file whose package resolves to "-badpkg" returns an error
                 naming the value and spawns nothing (the exec is never reached — assert on the
                 error, and that no report dir was written). Stryker.runArgs with a touched file
                 "-rf" errors rather than emitting `--mutate -rf`. StrykerNet with OutputDir
                 "-o" errors. The ast-grep argv is asserted element-by-element as
                 [ast-grep run --lang L --pattern P --json=compact -- FILE]: deleting the "--"
                 fails on index, not on a substring search.

  t-3  Redact token from forge client errors
       files:    internal/secret/redact.go, internal/forge/forge.go, internal/forge/github.go
       covers:   c-3
       desc:     `secret.Redact(text, tokens...)` replaces each non-empty secret with
                 "[redacted]"; Client.do (both clients) runs the status/body/hint error text
                 through it before returning, so a server that echoes the Authorization header
                 cannot round-trip the token into an error.
       contract: TestTokenNeverLeaks drives Client.do against an httptest server that returns
                 401 with the seeded token "dross-canary-TOKEN" in the response body and in a
                 header: the returned error's string contains "[redacted]" and does NOT contain
                 the canary; telemetry.Detail(err) does not contain it either; and the same
                 assertion runs over the 403/404/500 branches and the decode-failure branch, so
                 a new error path added without redaction fails one of them.

Wave 2 (depends on wave 1)

  t-4  Gate verify and execute on recorded consent
       files:    internal/cmd/verify.go, internal/cmd/task.go,
                 assets/prompts/execute.md, assets/prompts/quick.md
       covers:   c-1
       depends:  t-1
       desc:     `dross verify` refuses before building adapters; `dross task status <p> <t>
                 in_progress` refuses (other statuses stay open so an in-flight phase can be
                 closed out). Both refusals name `dross trust`. The two prompts gain a
                 `dross trust check` pre-flight step that stops the command on non-zero.
       contract: TestVerifyRefusesWithoutConsent: with no .dross/local.toml, `dross verify p`
                 exits non-zero, the mutation-adapter spy records zero Run calls, and no
                 tests.json is written; after `dross trust` the same invocation proceeds.
                 TestTaskStatusGate: `task status p t-1 in_progress` refuses while
                 `task status p t-1 done` still succeeds. TestExecutePromptHasTrustPreflight /
                 TestQuickPromptHasTrustPreflight fail if the `dross trust check` line is
                 dropped from either prompt.

  t-5  Redact token from ship API errors
       files:    internal/ship/open.go, internal/ship/forgejo.go, internal/ship/gitlab.go,
                 internal/ship/bitbucket.go
       covers:   c-3
       depends:  t-3
       desc:     jsonPost, jsonGet and the gitlab/bitbucket request helpers build their non-2xx
                 errors through secret.Redact with the token they were handed.
       contract: One table test per backend (forgejo, gitlab, bitbucket) posts to an httptest
                 server returning 403 with the seeded token in the body: OpenPR and PostComment
                 return errors containing "[redacted]" and never the canary. Removing the redact
                 call from any single helper fails that helper's row.

  t-6  Generalize the exec-arg audit gate to every binary
       files:    internal/cmd/gitargs_audit_test.go,
                 internal/cmd/testdata/gitargs_audit/snippets.txt,
                 internal/ship/comment.go, internal/ship/merged.go
       covers:   c-4, c-5
       depends:  t-2
       desc:     The audit stops matching only the literal "git" binary: it resolves any literal
                 binary and applies a policy table — separator tools (git, semgrep, ast-grep)
                 keep the fence rule with per-tool value-taking flag sets (gh's --title/--body/
                 --head/--base/--reviewer included), reject tools (gremlins, npx/stryker, dotnet)
                 require the positional be wrapped in noDash(...). A non-literal binary
                 (`exec.Command(args[0], args[1:]...)`) is flagged unless its enclosing function
                 is in the accepted-with-reason allowlist in the file header. gh's two positional
                 sites are fixed in the same task: `pr comment` / `pr view` fence the number with
                 "--" and reject a non-positive PRNumber.
       contract: TestNoUnseparatedExecPositional flags `exec.Command("gh","pr","comment",n)` with
                 file and line, and passes for `"--", n`. Snippet table gains one FLAG and one
                 PASS per policy: gremlins bare pkg vs noDash(pkg); semgrep path without vs with
                 "--"; `exec.Command(bin, rest...)` in an unnamed function is FLAG. The existing
                 "checked >= 6" self-check is raised to cover the new rows, so an audit that
                 degrades to zero findings still fails. GetPRStatus with PRNumber = -5 returns an
                 error instead of passing "-5" to gh.

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1, t-4 |
| c-2 | t-2 |
| c-3 | t-3, t-5 |
| c-4 | t-6 |
| c-5 | t-6 |

5/5 criteria covered.

## Judgment calls

- Consent lives in `internal/cmd` (trust.go) rather than a new `internal/trust` package: the
  local.toml store and its tracked-file refusal are already there, and a separate package would
  either import internal/cmd or duplicate the store. Rejected the extra package as structure with
  no criterion behind it.
- Enforcement points chosen: `dross verify` (it actually spawns the test command via the mutation
  adapters) and `dross task status ... in_progress` (execute's mandatory per-task CLI call).
  Rejected gating `dross changes record` — it runs after the tests already did, so it gates
  nothing. `dross quick` has no pre-test CLI choke point, so it gets the prompt pre-flight plus
  the verify gate; that gap is the same one the locked decision already accepts.
- c-2's semgrep leg is spec-only in the gate: semgrep is never invoked from Go today (only
  `exec.LookPath` in internal/security/catalog.go). Rejected inventing a call site to fence;
  the policy table carries semgrep so the day one is written it is checked.
- c-4 merged into the audit-gate task rather than standing alone: the two real gh fixes are
  one-line changes to comment.go and merged.go, and the gate is what makes them stay fixed. A
  separate task would have been under ten minutes of work.
- c-3 split across two tasks purely on the 5-file limit — the mechanism is one helper applied
  identically in six one-line sites; internal/forge goes first because t-5 imports the package
  it creates. If the judge prefers one task, merging t-3 and t-5 changes nothing but file count.
- The audit file keeps its `gitargs_audit` name and testdata path. Renaming to `execargs_audit`
  would be a diff across two paths teaching nothing; the header comment carries the new scope.
- Rejected an `--` fence on `gh pr create`: it takes no positionals, so every derived value there
  is already an option-argument gh reads literally. The gate's per-tool value-taking flag table
  encodes that instead of a no-op change to the argv.
