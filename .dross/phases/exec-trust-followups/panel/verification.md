# exec-trust-followups — verification lens

Every task below was derived by writing the failing test first and then asking
what the smallest change is that makes that test satisfiable. Where a criterion
could not be pinned by a test, the task was reshaped until it could.

```
Phase exec-trust-followups — 7 tasks across 3 waves

Wave 1
  t-1  Add argfence policy table and rejection
       files:    internal/argfence/argfence.go,
                 internal/argfence/policy.go,
                 internal/argfence/argfence_test.go
       covers:   c-2, c-5
       contract: PolicyFor("ast-grep") reports Separator, PolicyFor("gremlins")
                 reports Reject; PolicyFor("dotnet") reports Reject;
                 PolicyFor("cargo") — a binary in no table entry — returns
                 ok=false, so an unknown binary can never be treated as fenced.
                 RejectLeadingDash("gremlins","package","-rf") returns an error
                 wrapping ErrLeadingDash and naming both tool and value;
                 RejectLeadingDash(...,"./internal/cmd") returns nil;
                 RejectLeadingDash(...,"") returns nil (empty is not a flag).
                 Deleting an entry from the table fails
                 TestPolicyCoversEveryKnownBinary, which asserts the table's key
                 set is exactly {git, gh, gremlins, npx, dotnet, ast-grep,
                 semgrep}.

  t-2  Add exectrust consent store and binding
       files:    internal/exectrust/exectrust.go,
                 internal/exectrust/exectrust_test.go,
                 internal/cmd/local.go
       covers:   c-1
       contract: Fingerprint("go test ./...") != Fingerprint("go test ./... ")
                 and is stable across calls — one byte of drift in
                 runtime.test_command revokes consent.
                 Check(root, repoDir, "go test ./...") on a tree with no
                 .dross/local.toml returns ErrNoConsent; after Grant writes
                 trusted_test_command, the same Check returns nil; Check with a
                 DIFFERENT command string on that same store returns
                 ErrStaleConsent, distinct from ErrNoConsent.
                 With .dross/local.toml reported tracked by
                 `git ls-files --error-unmatch`, Check returns an error naming
                 the tracked file and does NOT return nil — a committed consent
                 key cannot self-authorize (same refusal shape as
                 readAllowHosts).
                 `dross local set trusted_test_command <x>` is rejected by
                 localKeys as an unknown key, so consent can only be granted
                 through the trust command.

  t-3  Scrub token from forge and ship errors
       files:    internal/redact/redact.go,
                 internal/redact/redact_test.go,
                 internal/forge/forge.go,
                 internal/ship/open.go
       covers:   c-3
       contract: With $DROSS_CANARY set to "dross-canary-9f2a-TOKEN", a
                 Client.do against an httptest server that returns 401 with the
                 token echoed in its JSON body produces an error whose
                 .Error() contains neither the canary nor its base64 form; the
                 same holds for 403, 404, 500 and a hard transport failure.
                 redact.Scrub("Authorization: token abc", "abc") ==
                 "Authorization: token [redacted]"; redact.Scrub(s, "") == s
                 unchanged (an empty token must not turn every string into
                 "[redacted]"); redact.Scrub also strips
                 base64("user:token") so the Basic-auth credential built at
                 forge.go:643 cannot survive in an echoed body.
                 ship.jsonPost's `HTTP %d: %s` body snippet is scrubbed the
                 same way — asserted by an httptest server that mirrors the
                 Authorization header into its 400 body.

Wave 2 (depends t-1, t-2)
  t-4  Fence analyzer argv per tool policy
       files:    internal/mutation/gremlins.go,
                 internal/mutation/stryker.go,
                 internal/mutation/stryker_net.go,
                 internal/codex/ast_grep.go
       covers:   c-2
       depends:  t-1
       contract: runAstGrepFn's argv is
                 [ast-grep run --lang L --pattern P --json=compact -- FILE]:
                 a test that captures the built argv fails if "--" is missing or
                 sits anywhere other than immediately before the file operand,
                 and a file literally named "-rf" still lands after it.
                 Gremlins.Run(files) where packagesFromFiles yields a package
                 beginning with "-" returns an error wrapping
                 argfence.ErrLeadingDash and never reaches buildCmd — asserted
                 by a buildCmd seam that t.Fatal()s if called.
                 Gremlins.buildUnleashArgs rejects a reportRel beginning with
                 "-" (reachable through sanitizePkg on a hostile file path).
                 Stryker.runArgs rejects a --mutate entry beginning with "-"
                 after the Workdir prefix is trimmed, and rejects a Workdir
                 beginning with "-"; a normal "src/a.ts" passes.
                 StrykerNet.Run rejects an OutputDir beginning with "-" before
                 exec.Command is built.
                 Every rejection message names the tool and the offending
                 value, asserted by string match, so the user can fix the
                 config line rather than guess.

  t-5  Fence gh argv in ship
       files:    internal/ship/open.go,
                 internal/ship/comment.go,
                 internal/ship/merged.go,
                 internal/ship/basepr.go
       covers:   c-4
       depends:  t-1
       contract: gitHubPRStatus's captured argv is
                 [pr view -- 12 --json state,mergedAt,baseRefName] — the number
                 sits behind a separator; with PRNumber = -3 the function
                 returns an error before ghCommand is called (a test double
                 that fails the test on invocation).
                 postGitHubComment likewise fences fmt.Sprint(opts.PRNumber)
                 and refuses PRNumber <= 0 before exec.
                 openGitHubPR: with Title = "--json", Body = "-x",
                 BaseBranch = "--repo evil/x" and Reviewers = ["--json"], the
                 captured argv keeps each of them in the value slot of its own
                 flag (index of the value == index of its flag + 1) and adds no
                 new leading-dash positional; asserted by walking the argv, not
                 by eyeballing a golden string.
                 listBasePRs fences `base` the same way; a base of "--state"
                 does not become a second --state flag.

  t-6  Wire trust command and gate loop commands
       files:    internal/cmd/trust.go,
                 internal/cmd/trust_test.go,
                 internal/cmd/verify.go,
                 internal/cmd/task.go,
                 cmd/dross/main.go
       covers:   c-1
       depends:  t-2
       contract: `dross verify <phase>` on a tree with no consent exits
                 non-zero, prints the `dross trust` remedy, and never calls
                 configuredAdapters — asserted with a package-level adapter
                 seam that t.Fatal()s if reached, so a refusal that still shells
                 gremlins fails.
                 `dross task status <p> <t> in_progress` and `dross state bump
                 internal` refuse identically; `dross task status <p> <t> done`
                 and `dross status` do NOT (the gate must not brick read-only
                 and post-hoc commands).
                 TestExecGatedSetIsExplicit asserts the gated command set is
                 exactly {verify, task next, task status, state bump, changes
                 record} — adding a command to the tree does not silently join
                 or leave the set.
                 `dross trust` prints the command it is about to trust, writes
                 the fingerprint, and a second `dross verify` proceeds;
                 editing runtime.test_command afterwards makes verify refuse
                 again with the STALE message, not the never-trusted one.
                 `dross trust --check` exits 0 when trusted and 1 when not,
                 printing nothing on success, so prompts can pre-flight it.

Wave 3 (depends t-4, t-5)
  t-7  Generalise audit gate to all subprocesses
       files:    internal/cmd/subprocargs_audit_test.go,
                 internal/cmd/testdata/subprocargs_audit/snippets.txt,
                 ARCHITECTURE.md
       covers:   c-5, c-2, c-4
       depends:  t-4, t-5
       contract: TestNoUnseparatedPositional walks internal/ and cmd/, resolves
                 each exec.Command / exec.CommandContext / ghCommand /
                 gitCombined|gitNoOut|gitTrim site to a binary, looks that
                 binary up in argfence's policy table, and reports every
                 unfenced caller-derived positional by file:line.
                 An exec.Command whose literal binary is in NO table entry is
                 itself a failure ("binary %q has no argv policy"), so a new
                 subprocess is in scope the day it is added rather than the day
                 someone remembers this phase — pinned by
                 TestUnknownBinaryFailsClosed feeding a synthetic
                 `exec.Command("cargo", "test", pkg)` snippet and asserting a
                 finding.
                 snippets.txt gains matched FLAG/PASS pairs per binary:
                 FLAG `exec.Command("ast-grep","run",file)` /
                 PASS `exec.Command("ast-grep","run","--",file)`;
                 FLAG `exec.Command("gremlins","unleash",pkg)` (Reject-policy
                 tools flag ANY non-literal positional, since no separator can
                 rescue them) / PASS with a const-prefixed "./"+pkg;
                 FLAG `ghCommand("pr","view",num)` /
                 PASS `ghCommand("pr","view","--",num)`;
                 FLAG `exec.Command("dotnet","stryker","--output",dir)` is NOT
                 flagged (option argument) while
                 FLAG `exec.Command("dotnet","stryker",dir)` is.
                 The existing snippet floor rises: the table must exercise at
                 least 16 snippets or the test fails, so a checker that
                 degraded to "return no findings" cannot pass.
                 The five git-specific tests (TestAuditFlagsBarePrefixlessVar,
                 TestAuditScansCodexPackage, the value-taking-flag carve-out)
                 survive the rename unchanged and still pass — proof the
                 generalisation did not weaken the git guarantee it grew from.
                 The ARCHITECTURE.md landmark lines for `auditFile` and the
                 gate test are updated to the new path; architecture_test.go's
                 landmark check fails if they are not.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-2 (store + binding), t-6 (command + gate at verify / execute loop / quick loop) |
| c-2 | t-1 (policy table), t-4 (call sites), t-7 (the gate that keeps it true) |
| c-3 | t-3 |
| c-4 | t-1 (gh policy entry), t-5 (call sites), t-7 (gate) |
| c-5 | t-1 (shared table), t-7 |

All five criteria covered. Each is covered by at least one task whose contract
names the exact surface that breaks.

## Judgment calls

- **One shared policy table (`internal/argfence`) rather than a table private
  to the audit test.** Rejected: encoding the per-tool policy only in the gate.
  The locked `analyzer_arg_policy` decision requires the gate to know which
  policy applies per tool, and the call sites need the same knowledge to reject
  a leading dash. Two copies drift; one table means a policy change is a
  one-line diff that both the runtime and the gate observe.
- **Fail-closed on unknown binaries via the policy table, not an exception
  list.** Rejected: scanning only the binaries this phase names. The locked
  `audit_gate_breadth` decision makes a never-anticipated subprocess in scope
  the day it is added; a missing table entry failing the build is the only
  shape that achieves that without a file:line list that rots.
- **Renamed `gitargs_audit_test.go` → `subprocargs_audit_test.go` rather than
  adding a second audit file.** Rejected: leaving the git gate intact and
  bolting a parallel non-git gate beside it. Two walkers over the same tree is
  precisely the divergence the single gate exists to prevent. Cost is the
  ARCHITECTURE.md landmark update, which is in t-7's files.
- **Gate the loop commands by an explicit named set, tested as a set.**
  Rejected: a persistent pre-run hook over the whole command tree. A blanket
  hook would brick `dross status`, `doctor` and `local get` in any fresh clone,
  which makes the gate the first thing a user disables. The explicit set is
  asserted by `TestExecGatedSetIsExplicit`, so membership is checked rather
  than remembered.
- **`dross quick`'s CLI choke point is `state bump internal` + `changes
  record`, and I am naming that as weaker than execute's.** Rejected:
  pretending `dross trust --check` in the prompt is enforcement. There is no
  `dross quick` binary command — quick is a prompt. The honest position is that
  the CLI can refuse to record or bump for an untrusted repo (so an untrusted
  quick cannot complete), plus a `--check` pre-flight in `quick.md`, and the
  locked decision's own accepted limit covers the rest.
- **Token scrubbing at the error-construction boundary, not in
  `telemetry.Detail`.** Rejected: scrubbing where the event is written.
  `Detail` does not know the token value; `forge.Client` and `ship.jsonPost`
  do. Scrubbing once at construction covers the returned error, the telemetry
  event derived from it, and the `dross: <err>` line on stderr — three surfaces
  from one choke point, which is what c-3's "no emitted surface" wording asks
  for.
- **`Fingerprint` over the raw `test_command` string, not a normalised form.**
  Rejected: trimming/collapsing whitespace before hashing. The locked
  `consent_binding` decision wants a changed command to revoke consent; a
  normaliser is a classifier, and `config-trust-hardening`'s own reasoning
  names the classifier as the vulnerability. A re-prompt after a cosmetic edit
  is the cheap side of that trade.
- **`ErrNoConsent` and `ErrStaleConsent` as distinct sentinels.** Rejected: one
  generic refusal. The two messages need different remedies (grant vs re-grant
  after reviewing what changed), and the distinction is directly testable,
  which a single error would not be.
