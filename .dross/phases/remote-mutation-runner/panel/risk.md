# Plan draft — lens: risk

Phase remote-mutation-runner — 11 tasks across 4 waves

The graph is shaped by the failure modes, not by the layers. The named risks, each
owned by exactly one task:

| # | Failure mode | Owner |
|---|---|---|
| R1 | A derived host/workdir reaches `ssh`/`rsync` argv as an option (`-oProxyCommand=…` is local code execution) | t-1 |
| R2 | A tracked, cloned config authorizes a remote — the repo grants its own consent | t-2 |
| R3 | `dross local set` grants remote consent without ever printing the host | t-3 |
| R4 | A stale remote workdir contributes source or a report to this run | t-4 |
| R5 | Readiness discovered mid-run; a failed core probe silently reads the LOCAL machine | t-5 |
| R6 | The adapter runs against a remote tree with no dependencies restored | t-6 |
| R7 | Remoting fails and the run degrades to a LOCAL execution | t-7 |
| R8 | A transport failure is mistaken for "gremlins measured nothing" and excluded, not fatal | t-8 |
| R9 | Config honoured at verify but not at the drain (or vice versa) | t-9 |
| R10 | A failed leg lands as a soft FLAG and the phase reads clean | t-10 |
| R11 | doctor reports ready on a host verify cannot actually use | t-11 |

---

## Wave 1

```
t-1  Fence and spawn ssh/rsync argv safely
     files:    internal/remote/exec.go
               internal/remote/exec_test.go
               internal/argfence/policy.go
               internal/cmd/subprocargs_audit_test.go
     covers:   c-4
     depends:  —
     desc:     New internal/remote package: Runner holding host/user/workdir, an
               ssh/rsync argv builder, and a `runnerBuildCmd` process seam (same
               shape as gremlinsBuildCmd) so every downstream test asserts argv
               without a network. Adds argfence table entries for "ssh" (Reject —
               no end-of-options token; the first non-option is the destination
               and everything after is a remote command) and "rsync" (Separator,
               "--"), plus their value-taking flags in the audit gate's table.
               Declares the error sentinels the rest of the phase branches on:
               ErrTransport, ErrUnreachable, ErrToolMissing, ErrSync.
     contract: - a host of "-oProxyCommand=touch /tmp/pwned" is refused by
                 remote.Runner before any process is built — the test substitutes
                 a runnerBuildCmd that t.Fatal()s if called, so "refused" is
                 distinguishable from "refused after spawning ssh"
               - a workdir beginning with "-" is refused with argfence.ErrLeadingDash
                 and the error names both the tool and the field
               - removing the "ssh" entry from argfence's table makes
                 TestSubprocessArgsAudit fail (fail-closed: unlisted binary = finding)
               - every error the package returns satisfies errors.Is(err, remote.ErrTransport)

t-2  Resolve remote config, refuse tracked sources
     files:    internal/remote/config.go
               internal/remote/config_test.go
               internal/project/project.go
               internal/cmd/local.go
     covers:   c-2, c-3
     depends:  —
     desc:     Adds the flat closed keys to localStore — remote_host, remote_workdir,
               mutation_workers, mutation_test_cpu — with only the two tuning keys in
               localKeys; remote_host/remote_workdir are absent from it exactly as
               trusted_test_command is. Adds a MutationRemote block to project.Project
               whose only purpose is to be REFUSED BY NAME when non-empty. remote.Resolve
               runs refuseTrackedLocal first, then the project.toml refusal, then reads
               the store, and returns a typed Config (or nil = local run).
     contract: - project.toml carrying `[mutation.remote] host = "build.local"` makes
                 remote.Resolve return an error naming the key and the file, and the
                 returned Config is nil (no partial authorization)
               - `dross local set remote_host x` exits non-zero with "unknown local key"
                 and the key list it prints does NOT contain remote_host
               - a local.toml that git reports tracked is refused UNREAD — the test
                 writes a valid remote_host into it and asserts Resolve still errors
               - mutation_workers unset yields Workers=0, so the adapter's existing
                 NumCPU/2 and DefaultTestCPU fallbacks are what run
```

## Wave 2 (depends on wave 1)

```
t-3  Add the remote-consent verb
     files:    internal/cmd/runner.go
               internal/cmd/runner_test.go
               cmd/dross/main.go
     covers:   c-2
     depends:  t-2
     desc:     `dross runner trust <user@host> --workdir <path>` — prints the host and
               the workdir it is about to authorize BEFORE writing, then writes both
               keys to local.toml. `--check` prints nothing on success. `dross runner
               show` prints the current grant; `dross runner revoke` clears both keys.
     contract: - the printed line contains both the host and the resolved workdir, and
                 the test asserts it is emitted BEFORE the store is written (a store
                 stubbed to fail still leaves the banner in the captured output)
               - trusting into a git-tracked local.toml refuses and writes nothing
               - revoke clears remote_host AND remote_workdir together — a run left
                 with a workdir and no host must not be reachable
               - `dross runner trust -oProxyCommand=x` is refused by the same
                 argfence check t-1 owns, not by a second ad-hoc one

t-4  Push the working tree with rsync --delete
     files:    internal/remote/sync.go
               internal/remote/sync_test.go
     covers:   c-6
     depends:  t-1, t-2
     desc:     Push builds the rsync argv: --delete, --exclude .git, and
               --filter=':- .gitignore' so ignored dependency dirs stay off the wire
               while uncommitted work rides. Also wipes the remote reports/ tree for
               the adapter before the run so no prior run's report can be fetched.
     contract: - the built argv contains --delete and excludes .git; a test asserting
                 the exact flag set fails if --delete is dropped (that flag IS c-6)
               - a file present in .gitignore is absent from the argv's transfer set
                 while an uncommitted, unignored file is present
               - a non-zero rsync exit produces an error wrapping remote.ErrSync whose
                 message names the host and the workdir — and Push returns it rather
                 than a nil error with a partial tree
               - the remote report-dir wipe is issued BEFORE the adapter command, proven
                 by ordering assertions on the recorded seam invocations

t-5  Probe remote readiness and core count
     files:    internal/remote/probe.go
               internal/remote/probe_test.go
     covers:   c-5, c-3
     depends:  t-1, t-2
     desc:     Probe answers: host reachable (ssh with BatchMode=yes and a connect
               timeout, never an interactive password prompt), named tool present on
               PATH for the adapter being asked about, and the remote core count for
               the worker default. Returns a Readiness struct with per-check status
               and the failing cause, never a bare bool.
     contract: - an unreachable host yields Readiness.Reachable=false with an error
                 wrapping ErrUnreachable that names the host — not a timeout deadlock
                 (BatchMode=yes is asserted present in the argv)
               - a missing `gremlins` on the remote yields ErrToolMissing naming the
                 tool and the adapter that needs it, distinct from ErrUnreachable
               - an unparseable or zero core count is an ERROR, not a fallback: the
                 test asserts the probe does not return the local runtime.NumCPU(),
                 which would silently size the run for the wrong machine
               - Workers derived from a 16-core probe result is 8 (NumCPU/2 on the
                 REMOTE), and an explicit configured value overrides it

t-6  Restore remote dependencies per adapter
     files:    internal/remote/deps.go
               internal/remote/deps_test.go
     covers:   c-7
     depends:  t-1
     desc:     Maps adapter name to the restore command run on the remote before the
               adapter: stryker → `npm ci`, stryker-net → `dotnet restore`, gremlins →
               none. The mapping is a closed table; an unknown adapter name is an error,
               not a silent no-op.
     contract: - RestoreFor("gremlins") returns no command and issues no ssh call at all
                 (asserted via the seam, not by inspecting a returned string)
               - RestoreFor("stryker") issues `npm ci`, not `npm install` — the
                 lockfile-respecting form is the point and a test pins the exact argv
               - a non-zero restore exit returns an error wrapping ErrTransport that
                 names the restore command; it does not fall through to the adapter
               - an adapter name absent from the table is an error (fail-closed), so a
                 fourth adapter cannot silently skip its restore

t-10 Escalate a remote-leg failure to BLOCKING
     files:    internal/verify/verify.go
               internal/verify/verify_test.go
     covers:   c-4
     depends:  t-1
     desc:     RunScoped's record-and-continue currently turns any adapter error into a
               FLAG finding (verify.go:506). Capture whether the error wraps
               remote.ErrTransport at the point the error value is still live, carry it
               on LanguageRun, and emit BLOCKING with the host named for that class.
     contract: - an adapter returning an error wrapping remote.ErrTransport produces a
                 finding with severity BLOCKING, and the run's blocking count is
                 non-zero — a phase cannot be verified past a leg that never ran
               - a plain adapter error (stryker misconfigured, no remote involved) stays
                 a FLAG, so the escalation does not swallow the existing behaviour
               - the leg still records its Files, so the report says WHICH files went
                 unmeasured rather than reporting an empty, clean-looking language run
               - record-and-continue is preserved: a failing remote gremlins leg does
                 not discard a finished stryker leg in the same run
```

## Wave 3 (depends on wave 2)

```
t-7  Route adapter exec through the remote seam
     files:    internal/mutation/remote.go
               internal/mutation/remote_test.go
               internal/mutation/gremlins.go
               internal/mutation/stryker.go
               internal/mutation/stryker_net.go
     covers:   c-1, c-4
     depends:  t-4, t-5, t-6
     desc:     Adds a Remote field to all three adapters and one shared
               buildRemoteCmd the three buildCmd methods delegate to when it is set:
               push (t-4), restore (t-6), then the tool command over ssh in the remote
               workdir. Prefix and Remote are mutually exclusive — both set is a
               refusal, not a nesting.
     contract: - with Remote set, the built command's argv[0] is "ssh" for each of the
                 three adapters; a table test over Gremlins/Stryker/StrykerNet fails if
                 any one of them is left on the local path
               - a Push or restore failure makes Run return the wrapped error and spawn
                 NO local process — the test substitutes a local exec seam that t.Fatal()s
                 if reached, so silent local degradation is a test failure, not a review catch
               - Remote set together with a docker Prefix is refused with an error naming
                 both, before any process is built
               - with Remote nil every adapter's argv is byte-identical to today's

t-11 Report remote readiness in doctor
     files:    internal/cmd/doctor.go
               internal/cmd/doctor_test.go
     covers:   c-5
     depends:  t-2, t-5
     desc:     A "Mutation runner:" block reading the same remote.Resolve and
               remote.Probe the run uses — reachability, the configured adapter's
               toolchain, the remote core count, and the grant's provenance. Unreachable
               or missing tool counts as an issue; no remote configured stays silent.
     contract: - with no remote configured doctor prints nothing for the block and the
                 issue count is unchanged (a local-only repo must not start failing)
               - an unreachable host adds an ISSUE (not a warning) naming the host, so
                 doctor's exit code — which gates hooks and CI — goes non-zero
               - a reachable host with no gremlins on PATH is a distinct line naming the
                 tool, proving the toolchain check is not collapsed into reachability
               - the block calls the same Probe t-5 owns: a test substituting the probe
                 seam sees exactly one call, so doctor cannot pass on a check verify
                 performs differently
```

## Wave 4 (depends on wave 3)

```
t-8  Fetch each package report; never call a transport failure "unmeasured"
     files:    internal/mutation/gremlins.go
               internal/mutation/gremlins_test.go
     covers:   c-1, c-6, c-4
     depends:  t-7
     desc:     Inside the existing per-package loop: remove the local stale report,
               run the package on the remote, then fetch THAT package's report before
               the next iteration (locked report_fetch). A fetch/transport failure
               aborts the Run with a wrapped ErrTransport; only a genuinely absent
               remote report stays UnmeasuredMissing.
     contract: - the fetch for package N is issued before package N+1's run — an
                 ordering assertion over recorded seam calls, not a count
               - a fetch that fails makes Run return an error wrapping remote.ErrTransport;
                 the test asserts it does NOT appear in g.Unmeasured, because
                 UnmeasuredMissing means "gremlins learned nothing" and is non-fatal to
                 the drain — routing a broken pipe there is exactly the empty-report-reads-
                 as-clean mode c-4 forbids
               - a local report file left by a previous run, with no remote report fetched
                 this run, yields UnmeasuredMissing rather than being parsed and merged
               - a remote run's merged Report has the same repo-relative paths in
                 Surviving and Files as a local run over the same fixture, so diff
                 scoping and the survivor keys are unchanged

t-9  Honour remote and worker config at both construction sites
     files:    internal/cmd/verify.go
               internal/cmd/survivor_drain.go
               internal/cmd/verify_test.go
               internal/cmd/survivor_drain_test.go
     covers:   c-3, c-1
     depends:  t-7
     desc:     configuredAdapters (verify.go:237) and runGremlinsOverPackages
               (survivor_drain.go:139) both resolve the remote config and set Remote,
               Workers and TestCPU. Precedence: explicit config > remote probe NumCPU/2
               > today's local defaults.
     contract: - a table test over BOTH construction sites asserts Workers and TestCPU
                 from local.toml land on the built Gremlins — adding the field at one
                 site only fails the other half of the table
               - with remote_host set, both sites produce adapters with Remote non-nil;
                 the drain in particular cannot quietly stay local while verify goes remote
               - with nothing configured, both sites produce the adapters they build
                 today: Workers=0, TestCPU=0, Remote=nil
               - a resolve error (tracked project.toml, tracked local.toml) fails the
                 command rather than returning a local-only adapter list
```

---

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 remote run, no local process, reports land locally | t-7, t-8, t-9 |
| c-2 tracked project.toml refused by name; only untracked config authorizes | t-2, t-3 |
| c-3 workers / test-cpu from config at both sites; unset = today | t-2, t-5, t-9 |
| c-4 broken remote fails loudly, never degrades or reads clean | t-1, t-7, t-8, t-10 |
| c-5 doctor reports remote readiness | t-5, t-11 |
| c-6 working tree incl. uncommitted; no stale remote workdir | t-4, t-8 |
| c-7 dependencies restored before the adapter runs | t-6 (invoked by t-7) |

Every criterion is claimed; no task is without a criterion.

## Judgment calls

- **A new `internal/remote` package rather than growing `internal/mutation`.** Rejected
  putting transport in mutation: the doctor check (t-11) and the consent verb (t-3) both
  need the config and the probe, and importing an adapter package from `internal/cmd`
  just to reach a probe would make the seam un-substitutable in doctor's tests.
- **Fencing owned by the transport task, not a separate one.** Rejected a standalone
  argfence-table task: a policy entry with no call site is untested, and the audit gate
  is fail-closed anyway, so the entry and the spawn must land together or CI is red
  between them.
- **`ssh` classified Reject, not Separator.** Rejected `--`: ssh takes the destination
  as the first non-option and treats everything after it as the remote command, so a
  separator buys nothing — a dash destination has to be refused outright.
- **A failed core probe is fatal, not a fallback to local NumCPU/2.** Rejected the
  soft fallback: the locked remote_workers decision exists because the halving rule was
  reading the wrong machine, and a silent fallback reinstates exactly that bug on the
  path where it is hardest to notice.
- **A transport failure aborts Run instead of joining `Unmeasured`.** Rejected a fourth
  UnmeasuredKind: the drain treats Missing as non-fatal-but-unknown and the score
  excludes it, so a broken pipe filed there produces a run that looks measured and clean.
  The two callers want opposite handling, so it cannot be one channel.
- **Escalating a remote leg to BLOCKING (t-10) rather than leaving it a FLAG.** Rejected
  leaving verify's existing severity: c-4 says a broken remote must never read as clean,
  and a FLAG among a dozen findings in a partial verdict is precisely reading as clean.
- **`dross runner trust`, not an extension of `dross trust`.** Rejected overloading the
  existing verb: exec consent is bound to a command hash and revoked by an edit to it;
  remote consent is bound to a host and a workdir. One verb with two bindings would make
  the revocation rule ambiguous.
- **Stryker/Stryker.NET remoting ships in the same task as Gremlins (t-7).** Rejected a
  gremlins-first task with the other two deferred — the locked adapter_scope decision
  forbids it, and the shared buildCmd shape makes the two extra call sites three lines each.
- **Both construction sites in one task (t-9).** Rejected splitting verify from the drain:
  the risk IS the divergence between them, and a single table test spanning both is the
  only shape that fails when one is updated alone.
