# Panel synthesis — exec-trust-followups

Judged cold: I authored none of the three drafts. Claims about the tree were
checked against the repo before scoring (gh call sites, the non-literal
`exec.Command(args[0], args[1:]...)` shape in the mutation adapters, the
ARCHITECTURE.md landmark lines, and where token-bearing error strings are
actually built).

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| **risk** (10 tasks / 3 waves) | 4/5 solid; c-3 stops at `internal/forge` + telemetry and never reaches the four ship backends that actually echo response bodies into errors | Strongest *negative* contracts of the three — nil-argv-on-rejection, a `buildCmd` seam that fails if reached, a short-token floor so a 1-char env can't blank every message | Finest split; t-4/t-5 separate the analyzer and ast-grep legs of one policy, which is one task too many, but every task owns exactly one failure mode | Correct — 3 waves, t-7 gate last, t-9 correctly trails t-8 |
| **mvp** (6 tasks / 2 waves) | 5/5 but thin: c-4 and c-5 share one task, and c-2 has no proof task beyond its own argv asserts | Sharp where it counts — element-by-index argv assertion (deleting `--` fails on index, not substring) and a per-backend redaction table where removing any one call fails that row | Weakest: t-6 carries the policy table, the gh fixes, the non-literal-binary allowlist *and* the whole gate generalisation | 2 waves is under-serialised — the audit gate lands in the same wave as work it exists to police |
| **verification** (7 tasks / 3 waves) | 5/5, cleanest matrix — c-2/c-4/c-5 explicitly routed through one shared table plus the gate that keeps it true | Highest overall: named test functions, exact golden argv, index-relationship walks, empty-token guard, base64(`user:token`) scrub, snippet floor raised to 16, `TestExecGatedSetIsExplicit`, ARCHITECTURE.md landmark check | Balanced; t-7 carries three criteria but it *is* the gate, which is the right shape | Correct and the only draft with per-task `depends:` on every wave-2/3 task |

**Skeleton: `verification`.** It has the only structure that satisfies both
locked decisions at once — `analyzer_arg_policy` demands the gate encode the
per-tool policy, and `audit_gate_breadth` demands an unanticipated binary be in
scope the day it is added. A single shared policy table read by *both* the
runtime and the gate is the only shape where those two are one fact rather than
two that drift. Its contracts are also the ones that fail for the right reason:
they assert positions and seams, not substrings. risk was a close second and
supplies most of the grafts below; mvp supplies the file set verification got
factually wrong.

## Merged plan

Phase exec-trust-followups — 10 tasks across 3 waves.

### Wave 1

```
t-1  Add argfence policy table and rejection                        [verification + risk]
     files:    internal/argfence/argfence.go
               internal/argfence/policy.go
               internal/argfence/argfence_test.go
     covers:   c-2, c-5
     contract: PolicyFor("ast-grep") reports Separator; PolicyFor("gremlins"),
               PolicyFor("npx"), PolicyFor("dotnet") report Reject;
               PolicyFor("cargo") — in no table entry — returns ok=false, so an
               unknown binary can never be treated as fenced.
               TestPolicyCoversEveryKnownBinary asserts the key set is exactly
               {git, gh, gremlins, npx, dotnet, ast-grep, semgrep}; deleting an
               entry fails it.
               RejectLeadingDash("gremlins","package","-rf") wraps
               ErrLeadingDash and names both tool and value;
               (...,"./internal/cmd") returns nil; (...,"") returns nil —
               empty is not a flag.                             [verification]
     graft:    Fence() returns a NIL argv alongside a rejection error — never a
               best-effort argv plus err, so a caller that drops the error is
               not handed something runnable.                          [risk]
     graft:    Fence on a tool with no policy entry errors rather than
               returning the value unfenced — fail closed at the runtime
               boundary, not only at PolicyFor.                        [risk]
     graft:    Policy() exports the table so t-8's gate reads the same map the
               runtime does; TestPolicyTableIsSingleSource fails if a tool is
               present in one and absent from the other.               [risk]

t-2  Add exec-consent store and command fingerprint      [verification + risk + mvp]
     files:    internal/cmd/trust.go
               internal/cmd/local.go
               internal/cmd/trust_test.go
     covers:   c-1
     contract: Fingerprint("go test ./...") != Fingerprint("go test ./... ")
               and is stable across calls — one byte of drift revokes consent;
               no normalizer (a normalizer is a classifier).
               Check on a tree with no .dross/local.toml returns ErrNoConsent;
               after Grant writes trusted_test_command it returns nil; Check
               with a DIFFERENT command on that same store returns
               ErrStaleConsent — a sentinel distinct from ErrNoConsent.
               With .dross/local.toml reported tracked by
               `git ls-files --error-unmatch`, Check returns an error naming
               the tracked file and does NOT return nil — same refusal shape as
               readAllowHosts (internal/cmd/local.go:97).
               `dross local set trusted_test_command <x>` is rejected by
               localKeys as an unknown key, so consent is grantable only
               through the trust command.                       [verification]
     graft:    TestConsentStates pins the four untrusted cases *separately* —
               no local.toml, local.toml without the key, key holding a
               different command's hash, tracked local.toml — so one refusal
               path cannot mask another.                                [mvp]
     graft:    The stored value equals sha256(test_command) and is NOT the
               command itself.                                         [mvp]
     graft:    An empty runtime.test_command yields state "not-applicable";
               setting it non-empty flips the same repo back to absent.[risk]

t-3  Add redact package and scrub forge client errors     [verification + mvp]
     files:    internal/redact/redact.go
               internal/redact/redact_test.go
               internal/forge/forge.go
     covers:   c-3
     contract: With a seeded canary token, Client.do against an httptest server
               returning 401 with the token echoed in its JSON body produces an
               error whose .Error() contains neither the canary nor its base64
               form; the same holds for 403, 404, 500 and a hard transport
               failure.
               redact.Scrub("Authorization: token abc","abc") ==
               "Authorization: token [redacted]".
               redact.Scrub(s,"") == s unchanged — an empty token must not turn
               every string into "[redacted]".
               Scrub also strips base64("user:token"), so the Basic-auth
               credential forge.go builds cannot survive in an echoed body.
                                                              [verification]
     graft:    The redaction marker is "[redacted $GITHUB_TOKEN]" — naming the
               env var, not a bare "[redacted]" — so a redacted message still
               tells the user which credential failed.                 [risk]
     graft:    A token shorter than a redaction floor is NOT scrubbed globally,
               asserted, so a one-character env value can't blank every
               message into unreadability.                             [risk]
     note:     verification listed internal/ship/open.go here; the token-echoing
               error sites are actually in forgejo.go / gitlab.go /
               bitbucket.go / comment.go (open.go's gh path carries no token).
               Those four move to t-7, which also keeps open.go out of two
               concurrent wave-2 tasks.
```

### Wave 2

```
t-4  Fence analyzer argv per tool policy       (depends t-1)  [verification + mvp + risk]
     files:    internal/mutation/gremlins.go
               internal/mutation/stryker.go
               internal/mutation/stryker_net.go
               internal/codex/ast_grep.go
               internal/mutation/argv_test.go
     covers:   c-2
     contract: runAstGrepFn's argv is
               [ast-grep run --lang L --pattern P --json=compact -- FILE]: the
               capture fails if "--" is missing or sits anywhere other than
               immediately before the file operand, and a file literally named
               "-rf" still lands after it — asserted by index, so deleting the
               "--" fails on position, not on a substring search.
               Gremlins.Run where packagesFromFiles yields a package beginning
               with "-" returns an error wrapping argfence.ErrLeadingDash and
               never reaches buildCmd — asserted by a buildCmd seam that
               t.Fatal()s if called.
               buildUnleashArgs rejects a reportRel beginning with "-"
               (reachable through sanitizePkg on a hostile path).
               Stryker.runArgs rejects a --mutate entry beginning with "-"
               after the Workdir prefix is trimmed, and rejects a Workdir
               beginning with "-"; "src/a.ts" passes.
               StrykerNet.Run rejects an OutputDir beginning with "-" before
               exec.Command is built.
               Every rejection names the tool and the offending value, matched
               as a string, so the user can fix the config line.
                                                    [verification + mvp]
     graft:    A rejection yields a non-nil error AND a nil report — an
               empty-but-successful report would read as "no surviving
               mutants" and pass verify.                                [risk]
     graft:    ast-grep's lang is checked against the known set rather than
               passed through; a lang of "--config=/tmp/x" is rejected with no
               process spawned.                                        [risk]
     graft:    TestEveryCatalogToolHasAnArgvPolicy — a tool named in
               internal/security/catalog.go with no argfence policy fails.
               This is what carries c-2's semgrep leg, which has no Go call
               site today (catalog.go:55 is a LookPath entry only).     [risk]

t-5  Fence gh argv in ship                     (depends t-1)  [verification + risk]
     files:    internal/ship/open.go
               internal/ship/comment.go
               internal/ship/merged.go
               internal/ship/basepr.go
               internal/ship/gharg_test.go
     covers:   c-4
     contract: gitHubPRStatus's captured argv puts the number behind a
               separator: [pr view -- 12 --json state,mergedAt,baseRefName];
               with PRNumber = -3 the function errors before ghCommand is
               called (a double that fails the test on invocation).
               postGitHubComment fences fmt.Sprint(opts.PRNumber) the same way
               and refuses PRNumber <= 0 before exec.
               openGitHubPR: with Title="--json", Body="-x",
               BaseBranch="--repo evil/x", Reviewers=["--json"], the captured
               argv keeps each in the value slot of its own flag (index of
               value == index of its flag + 1) and adds no new leading-dash
               positional — asserted by walking the argv, not eyeballing a
               golden string.
               listBasePRs fences `base`; a base of "--state" does not become a
               second --state flag.                            [verification]
     graft:    PostComment's argv is exactly
               [pr comment --body <body> -- <number>] — --body stays BEFORE the
               separator; a --body demoted past "--" would be posted as a
               positional, which is a silent wrong-content bug, not a crash.
                                                                       [risk]

t-6  Wire `dross trust` and gate the loop commands  (depends t-2)  [verification + mvp]
     files:    internal/cmd/trust.go
               internal/cmd/verify.go
               internal/cmd/task.go
               internal/cmd/state.go
               cmd/dross/main.go
     covers:   c-1
     contract: `dross verify <phase>` with no consent exits non-zero, prints
               the `dross trust` remedy, and never calls configuredAdapters —
               asserted with a package-level adapter seam that t.Fatal()s if
               reached, so a refusal that still shells gremlins fails.
               `dross task status <p> <t> in_progress`, `dross task next` and
               `dross state bump internal` refuse identically;
               `dross task status <p> <t> done` and `dross status` do NOT — the
               gate must not brick read-only or post-hoc commands.
               TestExecGatedSetIsExplicit asserts the gated set is exactly
               {verify, task next, task status(in_progress), state bump,
               changes record} — adding a command to the tree does not silently
               join or leave the set.
               `dross trust` prints the command it is about to trust, writes
               the fingerprint, and a second `dross verify` proceeds; editing
               runtime.test_command afterwards makes verify refuse with the
               STALE message, not the never-trusted one.
               `dross trust --check` exits 0 when trusted, 1 when not, printing
               nothing on success, so prompts can pre-flight it.  [verification]
     graft:    The refusal does no I/O first — asserted on the filesystem, not
               only on the exit code: neither tests.json nor verify.toml is
               written.                                                 [risk]
     graft:    `dross doctor` still works under absent consent.          [risk]

t-7  Redact token from ship backend errors      (depends t-3)  [mvp]
     files:    internal/ship/forgejo.go
               internal/ship/gitlab.go
               internal/ship/bitbucket.go
               internal/ship/comment.go
               internal/ship/redact_test.go
     covers:   c-3
     contract: One table row per backend (forgejo, gitlab, bitbucket, the
               comment note path) against an httptest server returning 403 with
               the seeded canary in the body: OpenPR / PostComment / GetPRStatus
               return errors containing "[redacted $…]" and never the canary.
               Removing the redact call from any single helper fails that
               helper's row and no other.                               [mvp]
     graft:    ship's `HTTP %d: %s` body snippet is scrubbed, asserted by a
               server that mirrors the Authorization header into its 400 body.
                                                              [verification]
```

### Wave 3

```
t-8  Generalise the audit gate to every subprocess                   [verification]
     (depends t-1, t-4, t-5)                        + [risk] + [mvp] grafts
     files:    internal/cmd/subprocargs_audit_test.go
                 (renamed from gitargs_audit_test.go)
               internal/cmd/testdata/subprocargs_audit/snippets.txt
               ARCHITECTURE.md
     covers:   c-5, c-2, c-4
     contract: TestNoUnseparatedPositional walks internal/ and cmd/, resolves
               each exec.Command / exec.CommandContext / ghCommand /
               gitCombined|gitNoOut|gitTrim site to a binary, looks it up in
               argfence's table, and reports every unfenced caller-derived
               positional by file:line.
               A literal binary in NO table entry is itself a failure
               ("binary %q has no argv policy") — pinned by
               TestUnknownBinaryFailsClosed feeding
               exec.Command("cargo","test",pkg) and asserting a finding. The
               default is flag, not pass.
               snippets.txt gains matched FLAG/PASS pairs per binary:
               ast-grep bare file vs "--" before it; gremlins bare pkg (Reject
               tools flag ANY non-literal positional — no separator can rescue
               them) vs a const-prefixed "./"+pkg; ghCommand("pr","view",num)
               vs ("pr","view","--",num);
               exec.Command("dotnet","stryker","--output",dir) is NOT flagged
               (option argument) while ("dotnet","stryker",dir) is.
               The snippet floor rises to 16, so a checker degraded to "return
               no findings" cannot pass; scanning zero files fails rather than
               passing vacuously.
               The five git-specific tests (TestAuditFlagsBarePrefixlessVar,
               TestAuditScansCodexPackage, the value-taking-flag carve-out)
               survive the rename unchanged and still pass — proof the
               generalisation did not weaken the guarantee it grew from.
               ARCHITECTURE.md's `auditFile` and gate-test landmark lines
               (currently ARCHITECTURE.md:181-182, pointing at
               gitargs_audit_test.go) are updated to the new path;
               architecture_test.go's landmark check fails if they are not.
                                                              [verification]
     graft:    Non-literal binaries are handled explicitly: the
               `exec.Command(args[0], args[1:]...)` shape — which is real, at
               internal/mutation/gremlins.go:224, stryker.go:144 and
               stryker_net.go:105 — resolves to its builder function or is
               named in the accepted-with-reason header; unresolved and
               un-named is a FLAG, with its own snippet row.     [risk + mvp]
     graft:    internal/mutation and internal/ship are asserted in scope the
               way internal/codex already is, so a package silently dropping
               out of the walk fails.                                   [risk]

t-9  Prove the token reaches no emitted surface  (depends t-3, t-7)  [risk]
     files:    internal/cmd/token_leak_test.go
     covers:   c-3
     contract: Seed a recognisable canary into auth_env, drive the forge and
               ship failure paths through the cobra commands with a stub
               transport, and assert the canary appears in NONE of stdout,
               stderr, the returned error, or the telemetry JSONL written
               during the run.
               The same test also asserts the "[redacted $" marker IS present,
               so a test that stopped exercising the path cannot pass by
               silence; reverting t-3's redaction fails it.              [risk]
     why kept separate: c-3's wording asks for the seeded-canary proof over
               *emitted surfaces* explicitly. t-3 and t-7 can both be correct
               while a fourth code path prints the token, so the proof gets its
               own owner. Neither mvp nor verification has this task.

t-10 Surface consent state in doctor and prompts  (depends t-6)     [risk + mvp]
     files:    internal/cmd/doctor.go
               assets/prompts/execute.md
               assets/prompts/quick.md
               assets/prompts/verify.md
     covers:   c-1
     contract: With stale consent, `dross doctor` output contains "stale" and
               the `dross trust` command line; with consent granted it says
               granted and offers no remedy.
               A prompt that still tells the agent to run
               <runtime.test_command> without a preceding consent check fails
               TestExecutePromptChecksConsent, which greps the prompt bodies —
               the same shape as the existing prompt-content tests.      [risk]
     graft:    The prompt pre-flight is the literal `dross trust --check` line
               t-6 built, and TestQuickPromptHasTrustPreflight fails if it is
               dropped from either prompt.                              [mvp]
     note:     r-01 applies — these prompt edits are not live until
               `make install` re-links assets/.
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 | t-2 (store + binding), t-6 (command + gate), t-10 (doctor + prompts) |
| c-2 | t-1 (table), t-4 (call sites), t-8 (gate) |
| c-3 | t-3 (redactor + forge), t-7 (ship backends), t-9 (emitted-surface proof) |
| c-4 | t-1 (gh policy entry), t-5 (call sites), t-8 (gate) |
| c-5 | t-1 (shared table), t-8 (gate) |

5/5. Every criterion has a mechanism task and a distinct proof task.

## Disagreements

Seven genuine divergences. Each carries a provisional default; none is settled.

### D-1 — Where consent lives: `internal/cmd` vs a new package

- **risk, mvp:** `internal/cmd/trust.go`. The local.toml store and its
  tracked-file refusal are already there (`readAllowHosts`, local.go:97); a
  separate package would have to import internal/cmd or duplicate the store.
  Both explicitly rejected `internal/exectrust`.
- **verification:** a new `internal/exectrust` package — but its own file list
  still includes `internal/cmd/local.go`, so the store straddles the boundary
  anyway.
- **Provisional default:** `internal/cmd`. Two of three, and the concrete
  objection (the store cannot move without either an import cycle or a second
  copy of the tracked-file refusal) is unanswered by the third.
- **Why it matters:** a duplicated tracked-file refusal is exactly the
  self-authorizing hole this criterion exists to close. Two copies means one can
  be weakened without the other noticing — the same failure the shared argfence
  table (D-2) is chosen to avoid.

### D-2 — Is the fence a shared package or a package-local helper?

- **risk:** `internal/argvfence`, a package, because callers span mutation,
  codex and ship.
- **verification:** `internal/argfence`, a package, for the same reason plus the
  gate reading the same table.
- **mvp:** `internal/mutation/argsafe.go` — a helper private to mutation, with
  the policy table living only in the audit test.
- **Provisional default:** a shared package, named `internal/argfence`
  (skeleton's name).
- **Why it matters:** the locked `analyzer_arg_policy` decision says the gate
  "encodes which policy applies per tool", and `audit_gate_breadth` makes an
  unlisted binary a build failure. If the runtime table and the gate table are
  two objects they drift, and the drift is silent — the gate keeps passing while
  the runtime stops fencing. mvp's shape also puts the ast-grep and gh policies
  under `internal/mutation`, which is the wrong home for both.

### D-3 — Rename `gitargs_audit_test.go`, or widen it in place?

- **risk:** rename to `subprocess_audit_test.go` — the current name is *why*
  someone would add a semgrep site without opening it.
- **verification:** rename to `subprocargs_audit_test.go`, and pays the
  ARCHITECTURE.md landmark cost inside the task.
- **mvp:** keep the name and testdata path; a rename is "a diff across two paths
  teaching nothing", the header comment carries the new scope.
- **Provisional default:** rename, to `subprocargs_audit_test.go`, with the
  ARCHITECTURE.md update in t-8's files.
- **Why it matters:** it is not free — ARCHITECTURE.md:181-182 hard-code
  `gitargs_audit_test.go` with line numbers, and `architecture_test.go`'s
  landmark check will fail until they are updated. mvp's position is the cheaper
  one and is defensible; the case against it is discoverability, which is a
  judgment about future human behaviour, not something a test can settle.

### D-4 — Where the token gets scrubbed

- **risk:** at error construction *and* inside `telemetry.Detail`, via a
  `RegisterSecret` call — a backstop for any error path the forge layer missed.
- **verification:** at error construction only, and explicitly **rejects**
  scrubbing in `telemetry.Detail` — `Detail` does not know the token value;
  `forge.Client` and `ship.jsonPost` do.
- **mvp:** silent on the mechanism; asserts `telemetry.Detail(err)` is clean as
  a *consequence* of construction-boundary scrubbing.
- **Provisional default:** verification's construction-boundary mechanism, with
  risk's telemetry-output assertion retained as proof (t-9) rather than as a
  second scrubbing site.
- **Why it matters:** this is a defense-in-depth trade, and the two lenses want
  opposite things. Construction-only is cleaner and has one choke point, but if
  any error path is missed there is no second line — and c-3 names the telemetry
  event as a surface in its own right. Taking the proof without the backstop
  means t-9 is the only thing standing between a missed path and a leaked token.
  If the judge prefers belt-and-braces, risk's `RegisterSecret` is the graft to
  add back, at the cost of a global mutable secret registry.

### D-5 — Is c-4 its own task, or folded into the audit gate?

- **risk, verification:** its own task (t-5 here).
- **mvp:** merged into the gate task — "the two real gh fixes are one-line
  changes to comment.go and merged.go… under ten minutes of work".
- **Provisional default:** its own task.
- **Why it matters:** mvp's premise is factually short. There are four gh sites,
  not two — basepr.go:61 (`pr list --base`), comment.go:58 (`pr comment` with a
  bare number), merged.go:63 (`pr view` with a bare number) and open.go:73
  (`pr create` with title/body/base/reviewers). Merging them into the gate task
  also puts the fix and its own gate in one commit, so nothing observes the
  gate failing before the fix lands.

### D-6 — Which commands the consent gate covers

- **risk:** verify, `task next`, `task status`, `state bump`. Does not
  distinguish which status values are gated.
- **mvp:** verify and `task status … in_progress` only — other statuses stay
  open "so an in-flight phase can be closed out". Rejects gating
  `changes record` as gating nothing (it runs after the tests already did).
- **verification:** an explicitly named, explicitly tested set —
  {verify, task next, task status, state bump, changes record} — pinned by
  `TestExecGatedSetIsExplicit`.
- **Provisional default:** verification's explicit tested set, with mvp's and
  verification's shared carve-out that `task status … done` is NOT gated.
- **Why it matters:** `changes record` is the live disagreement. mvp is right
  that it gates nothing preventive; verification is right that refusing to
  record means an untrusted `quick` cannot *complete*, which is the only
  leverage the CLI has over a prompt-driven command. Including it buys
  after-the-fact enforcement at the cost of a refusal that arrives too late to
  prevent the execution it names. Also note risk gates `task next` where mvp
  does not — under mvp's set, an execute loop that skips `task next` and reads
  plan.toml directly meets no gate at all.

### D-7 — Doctor + prompt surfacing as a task of its own

- **risk:** yes — t-9 there, covering `dross doctor` plus three prompt files,
  with grep-based prompt-content tests.
- **mvp:** folds the prompt pre-flight into the gate task; no doctor work.
- **verification:** no task; only `dross trust --check` exists so prompts *can*
  pre-flight.
- **Provisional default:** keep it as its own wave-3 task (t-10).
- **Why it matters:** this is the half the locked decision admits it cannot
  enforce — an agent can always run `go test` directly. The prompt-content tests
  are the only mechanism that keeps the prompts from drifting back to a raw
  `<runtime.test_command>` instruction. It is also the weakest task in the plan
  by construction: a grep over prompt text proves the line is present, not that
  the agent honours it. Anyone who thinks that proof is not worth a task should
  cut it here rather than water down t-6.
