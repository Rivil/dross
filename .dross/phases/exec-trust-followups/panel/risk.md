# Panel draft — risk lens

Lens: failure modes drive the graph. Every task owns exactly one way this phase
can break, and its contract names the surface that breaks.

The four failure modes this phase exists for:

1. **Trust inherited by clone** — a `.dross/` authored elsewhere gets its
   `test_command` run because consent was repo-scoped, self-authorizing, or
   never checked at the CLI boundary (c-1).
2. **A derived value read as a flag** — by an analyzer (c-2), or by `gh` (c-4),
   in the sites nobody enumerated.
3. **A secret riding an error** — the token reaching an error string, a
   telemetry `err_detail`, or a terminal, on the failure path nobody exercises
   (c-3).
4. **The gate going quietly narrow** — a git-only audit that passes vacuously
   the day someone spawns a binary it never heard of (c-5).

---

Phase exec-trust-followups — 10 tasks across 3 waves

## Wave 1

```
t-1  Add per-tool argv fence package
     files:    internal/argvfence/argvfence.go
               internal/argvfence/argvfence_test.go
     covers:   c-2
     desc:     New package encoding the locked per-tool policy: Fence(tool,
               opts, derived...) emits the tool's end-of-options separator for
               semgrep and ast-grep, and returns ErrLeadingDash for gremlins,
               stryker and stryker-net, which have none. Unknown tool errors.
     contract: Fence("gremlins", nil, "--config=/etc/x") returns ErrLeadingDash
               and a nil argv — no argv is returned alongside the error.
               Fence("ast-grep", []string{"--lang","go"}, target) puts "--"
               immediately before target and after every opt.
               Fence("trivy", nil, "x") — a tool with no policy entry — errors
               rather than returning ["x"] unfenced (fail closed).
               Policy() exposes the table so the audit gate in t-7 reads the
               same map the runtime does; a tool present in one and absent from
               the other fails TestPolicyTableIsSingleSource.

t-2  Add exec-consent store and `dross trust`
     files:    internal/cmd/trust.go
               internal/cmd/local.go
               internal/cmd/trust_test.go
               cmd/dross/main.go
               README.md
     covers:   c-1
     desc:     New closed local.toml key `exec_consent` holding the sha256 of
               the consented runtime.test_command verbatim. `dross trust`
               prints the exact command, requires confirmation (--yes for
               non-interactive), writes the hash; `dross trust --show` reports
               granted | stale | absent. Reads and writes both refuse when
               .dross/local.toml is git-tracked, via the check extracted from
               readAllowHosts.
     contract: consent granted for "go test ./..." then test_command edited to
               "go test ./... && curl evil" → consentState returns stale, not
               granted, and the stored hash is not silently rewritten.
               A local.toml that `git ls-files --error-unmatch` reports tracked
               makes both `dross trust` and the consent read fail non-zero and
               write nothing — the same refusal readAllowHosts gives.
               Whitespace is load-bearing: "go test ./..." and "go  test ./..."
               hash differently (no normalizer — a normalizer is a classifier).
               An empty runtime.test_command yields state "not-applicable", and
               setting it non-empty flips the same repo back to absent.

t-3  Redact the forge token from errors and telemetry
     files:    internal/forge/redact.go
               internal/forge/forge.go
               internal/forge/redact_test.go
               internal/telemetry/telemetry.go
     covers:   c-3
     desc:     Every error Client.do returns (transport error, non-2xx status
               line, response-body snippet, decode error) passes through a
               scrubber replacing the token value with "[redacted $ENV]".
               telemetry gains RegisterSecret/scrub applied inside Detail, so a
               class-"other" err_detail cannot carry a token the forge layer
               missed.
     contract: with the token env seeded to a canary, a stubbed 401 whose body
               echoes the token back produces an error string containing the
               canary zero times and "[redacted $GITHUB_TOKEN]" once.
               telemetry.Detail(errors.New("...<canary>...")) returns a string
               with the canary removed, after RegisterSecret has been called.
               A token shorter than the redaction floor (e.g. "x") is NOT
               scrubbed globally — asserted, so a one-character env value can't
               blank every message into unreadability.
```

## Wave 2 (depends on wave 1)

```
t-4  Fence the mutation-adapter argv builders            (depends t-1)
     files:    internal/mutation/gremlins.go
               internal/mutation/stryker.go
               internal/mutation/stryker_net.go
               internal/mutation/argv_test.go
     covers:   c-2
     desc:     Route every caller-derived value through argvfence with the
               reject-leading-dash policy: gremlins' package tail and --output
               path, stryker's --mutate list and Workdir, stryker-net's
               --output dir. A rejection surfaces as an adapter error from Run,
               never a skipped file.
     contract: a changes.json path of "-oevil/x.go" makes Gremlins.Run return
               ErrLeadingDash and buildCmd is never reached — asserted by a
               buildCmd seam that fails the test if called.
               Stryker.runArgs with Workdir "--config=x" is rejected rather
               than joined into --mutate.
               A rejected value produces a non-nil error AND a nil report;
               returning an empty-but-successful report would read as "no
               surviving mutants" and pass verify.

t-5  Fence ast-grep and register the semgrep policy      (depends t-1)
     files:    internal/codex/ast_grep.go
               internal/codex/ast_grep_test.go
               internal/security/catalog.go
     covers:   c-2
     desc:     runAstGrepFn builds its argv through argvfence so the derived
               file lands after "--"; lang is checked against the known set
               rather than passed through. semgrep has no Go call site yet, so
               its catalog entry carries the policy name, making the day one is
               added a covered day rather than a new one.
     contract: the captured argv from the runAstGrepFn seam has "--"
               immediately before the file argument and after --json=compact.
               A lang of "--config=/tmp/x" is rejected before exec, with no
               process spawned.
               The semgrep catalog entry names a policy that argvfence.Policy()
               knows; a catalog tool with no policy fails
               TestEveryCatalogToolHasAnArgvPolicy.

t-6  Fence the gh invocations                            (depends t-1)
     files:    internal/ship/open.go
               internal/ship/comment.go
               internal/ship/basepr.go
               internal/ship/merged.go
               internal/ship/gharg_test.go
     covers:   c-4
     desc:     Rebuild each gh argv so every config- or state-derived value is
               either the argument of its own value-taking flag or sits after
               "--": `pr comment --body <b> -- <n>`, `pr view --json … -- <n>`,
               `pr create --title <t> --body <b> --base <base> …`,
               `pr list --base <base> --state open …`.
     contract: the argv captured from the ghCommand seam for PostComment is
               exactly [pr comment --body <body> -- <number>] — the number is
               after the separator, and --body is before it (a --body demoted
               past "--" would be posted as a positional).
               A reviewer of "--add-label" appears in the argv only as the
               element directly following its own "--reviewer", never as a bare
               element.
               A base branch of "--upload-pack=x" cannot appear as the first
               element after "pr list".

t-8  Enforce the consent gate in the loop commands       (depends t-2)
     files:    internal/cmd/verify.go
               internal/cmd/task.go
               internal/cmd/state.go
               internal/cmd/trust.go
     covers:   c-1
     desc:     requireExecConsent(root, proj) runs before any work in `dross
               verify` (which spawns mutation adapters that run tests), `dross
               task next` / `dross task status` (the execute loop's mandatory
               calls) and `dross state bump` (quick's). Refusal exits non-zero
               naming `dross trust`, and does no I/O first.
     contract: in a repo whose .dross/ has no local.toml, `dross verify <id>`
               exits non-zero, prints the `dross trust` remedy, and writes
               neither tests.json nor verify.toml — asserted on the filesystem,
               not just on the exit code.
               `dross task next` refuses under absent consent and succeeds
               after `dross trust --yes`; editing runtime.test_command
               afterwards makes it refuse again (stale).
               `dross status` and `dross doctor` still work under absent
               consent — the gate is on the commands that lead to execution,
               not on orientation.
               Empty runtime.test_command → all three commands proceed.

t-10 Prove the token reaches no emitted surface          (depends t-3)
     files:    internal/cmd/token_leak_test.go
     covers:   c-3
     desc:     End-to-end canary test: seed a recognisable token into the
               auth_env, drive the forge/ship failure paths through the cobra
               commands with a stub transport, and assert the canary appears in
               none of stdout, stderr, the returned error, or the telemetry
               event log written during the run.
     contract: after a forced 401 on `dross issue …` and on `dross ship`'s
               status lookup, grepping captured stdout+stderr, err.Error(), and
               the written telemetry JSONL for the canary yields zero hits; the
               same test fails if the redaction from t-3 is reverted (asserted
               by also checking the "[redacted $" marker is present, so a test
               that stopped exercising the path can't pass by silence).
```

## Wave 3

```
t-7  Generalise the audit gate to every subprocess       (depends t-1, t-4, t-5, t-6)
     files:    internal/cmd/subprocess_audit_test.go   (renamed from gitargs_audit_test.go)
               internal/cmd/testdata/subprocess_audit/snippets.txt
     covers:   c-5
     desc:     The audit walks every exec.Command / exec.CommandContext in
               internal/ and cmd/ regardless of binary, dispatching on
               argvfence.Policy(): separator tools require the separator,
               no-separator tools require an argvfence call or a const prefix,
               git keeps its existing --end-of-options / -- rules, and an
               unlisted binary is flagged. Argv-slice shapes
               (exec.Command(args[0], args[1:]...)) resolve to their builder
               function or are named in the accepted-with-reason header.
     contract: exec.Command("semgrep", "--config", p, target) is flagged and
               the same line with "--" before target passes.
               exec.Command("gremlins", "unleash", pkg) is flagged; the same
               line with argvfence.Fence(...) is not.
               A snippet spawning an unheard-of binary
               (exec.Command("trivy", "fs", dir)) is flagged — the default is
               flag, not pass.
               The renamed file keeps the git cases: the FLAG/PASS table still
               contains the bare-var, const-prefix and const-suffix rows and
               fails if fewer than 12 snippets are exercised.
               Scanning zero files still fails rather than passing vacuously,
               and internal/mutation + internal/ship are asserted in scope the
               way internal/codex already is.

t-9  Surface consent state in doctor and prompts         (depends t-8)
     files:    internal/cmd/doctor.go
               assets/prompts/execute.md
               assets/prompts/quick.md
               assets/prompts/verify.md
     covers:   c-1
     desc:     `dross doctor` reports exec consent as granted | stale | absent
               with the remedy line; execute/quick/verify prompts read the
               state in their preamble and stop at the gate instead of
               improvising a raw `go test`.
     contract: with a stale consent, `dross doctor` output contains the word
               "stale" and the `dross trust` command line; with consent
               granted it says granted and offers no remedy.
               A prompt that still tells the agent to run <runtime.test_command>
               without a preceding consent check fails
               TestExecutePromptChecksConsent, which greps the prompt bodies —
               the same shape as the existing prompt-content tests.
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (consent gate on test_command) | t-2, t-8, t-9 |
| c-2 (analyzer positionals fenced or rejected) | t-1, t-4, t-5 |
| c-3 (token on no emitted surface) | t-3, t-10 |
| c-4 (gh argv discipline) | t-6 |
| c-5 (audit gate covers every subprocess) | t-7 |

Every criterion has both a mechanism task and a proof task except c-4, whose
proof is its own argv assertions, and c-5, which is proof by construction.

## Judgment calls

- **Consent lives in internal/cmd, not a new package.** All three enforcement
  points (verify, task, state) are already there, and local.toml's closed-key
  store is there too. A new package would have to export the store to add one
  key. Rejected: internal/exectrust.
- **argvfence IS a new package.** Its callers span internal/mutation,
  internal/codex and internal/ship — the git equivalent could live in
  internal/cmd only because git's callers do. Rejected: duplicating the helper
  per package, which is exactly how the four-way os.Getenv duplication that
  hostguard.go was written to kill got started.
- **Gate at `dross task next` / `dross state bump`, not at prompt text.** The
  locked decision says the enforcement must sit where it cannot be talked out
  of. `task next` is the one call the execute loop cannot skip and still have a
  task; `state bump` is quick's. Rejected: gating `dross phase checkout`, which
  is also used for read-only orientation.
- **No hash normalization of test_command.** Whitespace differences re-prompt.
  Rejected: trimming/collapsing, which is a classifier, and the locked
  analyzer_arg_policy reasoning names the classifier as the vulnerability.
- **Rename gitargs_audit_test.go → subprocess_audit_test.go.** The current name
  is why someone would add a semgrep site without opening it. Rejected: keeping
  the name and widening only the body.
- **t-7 is wave 3, not wave 2.** It is the gate over t-4/t-5/t-6's output; run
  earlier it is red by construction and teaches nothing. Its cost is one wave of
  serialization, paid once.
- **Rejection returns nil argv, not a best-effort argv plus error.** A caller
  that ignores the error must not be handed something runnable. Rejected: the
  (argv, err) both-populated shape Go often uses.
- **t-10 exists separately from t-3.** The redactor and the proof that no
  surface carries the value are different failures: t-3 can be correct while
  a second code path prints the token. c-3 asks for the seeded-canary proof
  explicitly, so it gets its own owner.
- **semgrep gets a policy entry with no call site.** dross does not spawn
  semgrep today (catalog.go names the binary only). c-2 names it anyway, so the
  policy is registered and the audit covers the first future site. Rejected:
  declaring c-2's semgrep half vacuously satisfied.
