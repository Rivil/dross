# Verification-lens draft — remote-mutation-runner

Phase remote-mutation-runner — 8 tasks across 3 waves

```
Wave 1
  t-1  Add remote transport package and argv policy
       files:    internal/remote/remote.go, internal/remote/remote_test.go,
                 internal/argfence/policy.go, internal/argfence/argfence_test.go
       covers:   c-6
       depends:  —
       desc:     New package owning Target{Host,Workdir,Cores}, the pure argv builders
                 (SSHArgs, SyncArgs, FetchArgs), a Probe reading remote cores + tool
                 presence, an exec seam var, and exit-code classification. argfence gains
                 Reject rules for ssh and rsync.
       contract: - SyncArgs(Target{Host:"helicon",Workdir:"/srv/x"}, "/local/repo") yields
                   argv[0]=="rsync" carrying --delete, --filter=:- .gitignore,
                   --exclude=.git, a source with a trailing slash, and dest
                   "helicon:/srv/x"; dropping --delete or the .gitignore filter fails it.
                 - SSHArgs wraps the adapter argv as `ssh <host> cd <workdir> && <argv>`;
                   the returned argv[0] is "ssh" and the adapter binary never appears at
                   argv[0].
                 - A Workdir containing ';', '$(', '`' or a newline is refused by SSHArgs
                   with an error naming the workdir — argv is not built (ssh runs the
                   string through a remote shell, which argfence's dash rule does not
                   cover).
                 - A Host beginning with '-' is refused by argfence; Policy() has "ssh"
                   and "rsync" entries with Kind==Reject, so the subprocess audit gate
                   (internal/cmd/subprocargs_audit_test.go) stays green for the new
                   binaries and a missing entry fails it.
                 - classify(exitCode): 255 -> ErrTransport; rsync 23/24 -> ErrPartial
                   (file absent / vanished); any other non-zero -> ErrRemoteCommand
                   carrying the code. Classification reads the exit code, never stderr
                   prose.
                 - Probe parses `nproc`-style output into Cores and a per-tool present/
                   absent map; unparseable output is an error, never Cores==0 silently.

  t-2  Add remote consent store and grant verb
       files:    internal/cmd/local.go, internal/cmd/mutation_remote.go,
                 internal/cmd/mutation_remote_test.go, cmd/dross/main.go,
                 README.md, docs/dross.1
       covers:   c-2, c-3
       depends:  —
       desc:     localStore gains mutation_remote_host + mutation_remote_workdir
                 (deliberately ABSENT from localKeys) and mutation_workers +
                 mutation_test_cpu (present in localKeys). New `dross mutation remote
                 grant|status|revoke`, registered in main.go, sharing refuseTrackedLocal.
       contract: - `dross local set mutation_remote_host helicon` fails with "unknown
                   local key" and the sorted key list — the same exclusion
                   trusted_test_command has (local.go:75); if the key is ever added to
                   localKeys this test fails.
                 - `dross local set mutation_workers 8` round-trips through
                   `dross local get mutation_workers`.
                 - `dross mutation remote grant helicon /srv/dross` prints BOTH the host
                   and the workdir BEFORE the file is written (assert on captured stdout
                   ordering against a write-failure injection: on a failed write the
                   banner has already printed and no key is stored).
                 - With .dross/local.toml tracked by git, `mutation remote status` and the
                   grant reader both return the refuseTrackedLocal error unread — same
                   assertion shape as TestReadAllowHostsRefusesTrackedLocal.
                 - `revoke` clears host and workdir but leaves mutation_workers intact.
                 - `status` on an ungranted repo prints "not granted" and exits 0 (callers
                   branch on it; a non-zero exit would read as a broken repo).
                 - README.md and docs/dross.1 both contain "mutation remote grant",
                   "mutation_remote_host" and ".dross/local.toml" — same doc gate as
                   TestDocsCoverAllowHosts.

  t-3  Refuse a remote host in tracked project.toml
       files:    internal/project/project.go, internal/project/project_test.go
       covers:   c-2
       depends:  —
       desc:     Mutation gains an explicit trap field (remote_host / remote_workdir) that
                 exists only to be refused; Load errors when either is non-empty.
       contract: - project.Load on a project.toml containing
                   `[mutation]\n remote_host = "helicon"` returns a nil Project and an
                   error whose text contains both "mutation.remote_host" and "helicon",
                   and points at `dross mutation remote grant`.
                 - The same refusal fires for remote_workdir alone, so half a config is
                   not a bypass.
                 - The repo's own .dross/project.toml (and every existing project fixture)
                   still loads clean — the refusal is keyed to the trap field, not to
                   unknown keys generally.

Wave 2 (depends on wave 1)
  t-4  Wrap the three adapters in a shared remote launcher
       files:    internal/mutation/launcher.go, internal/mutation/gremlins.go,
                 internal/mutation/stryker.go, internal/mutation/stryker_net.go,
                 internal/mutation/launcher_test.go
       covers:   c-1, c-3, c-6
       depends:  t-1
       desc:     One Launcher struct (Prefix + *remote.Target) embedded by Gremlins,
                 Stryker and StrykerNet, owning buildCmd, the one-shot rsync push, the
                 per-report fetch, and the remote-derived worker default.
       contract: - With Remote set, every *exec.Cmd the three build has argv[0] in
                   {"ssh","rsync"}; the fake seam calls t.Fatal if it ever sees
                   "gremlins", "npx", "dotnet" or "go" as argv[0] — that failure IS c-1's
                   "no local compile or test process".
                 - With Remote nil, argv is byte-identical to today: the existing
                   TestStrykerRunArgsPinned, TestGremlinsBuildUnleashArgsDefault and
                   TestStrykerNetRejectsDashOutputDir keep passing unmodified.
                 - Run pushes the tree exactly once, before the first remote command: the
                   recorded call order starts with the rsync argv and a second rsync push
                   inside the per-package loop fails the test.
                 - Per package, the recorded order is [remote rm of the package's report
                   path, ssh gremlins run, fetch] — the remote `rm` missing lets a stale
                   remote report be fetched, which is exactly what c-6 forbids, and the
                   ordering assertion is what catches it.
                 - After a remote Run with the testdata/gremlins_pkg_report.json fixture
                   delivered by the fake fetch, the file exists at
                   GremlinsReportPath(ProjectRoot, pkg) and the merged Report equals the
                   local-run expectation in TestGremlinsRunRePrefixesPackagePaths — same
                   per-file keys, same score, so verify's scoping is provably unchanged.
                 - Remote{Cores:32} with Workers unset yields `--workers 16` in the
                   gremlins argv regardless of local runtime.NumCPU(); Remote nil with
                   Workers unset still yields local NumCPU/2.

  t-5  Report remote readiness in doctor
       files:    internal/cmd/doctor.go, internal/cmd/doctor_remote_test.go
       covers:   c-5
       depends:  t-1, t-2
       desc:     New "Remote mutation:" section in checkConfigTrust reading the grant and
                 calling remote.Probe through an overridable seam, checking reachability
                 and the toolchain the configured adapters need.
       contract: - No grant: prints an advisory line and doctor's issue count is unchanged
                   (a fresh clone must not fail doctor for not having a remote).
                 - Granted + probe returns ErrTransport: prints ✗ naming the host and
                   increments issues, so `dross doctor` exits non-zero.
                 - Granted + reachable but the probe reports gremlins absent: ✗ names both
                   the missing binary and the adapter that needs it; with
                   [mutation].adapters = ["stryker"] the same missing gremlins produces NO
                   finding — readiness is checked for the configured adapters only.
                 - Healthy: prints host, workdir and the probed core count, and the core
                   count in the output equals what the probe seam returned (the number
                   doctor prints is the number the worker default will use).

Wave 3 (depends on wave 2)
  t-6  Fail loudly on a broken remote
       files:    internal/mutation/launcher.go, internal/mutation/gremlins.go,
                 internal/mutation/remote_failure_test.go
       covers:   c-4
       depends:  t-4
       desc:     Map remote.ErrTransport/ErrPartial onto adapter outcomes: transport
                 failures are fatal and named; only a genuinely absent remote report stays
                 an UnmeasuredMissing skip. No local fallback path exists.
       contract: - rsync push exits 255: Run returns an error naming the host and "rsync",
                   and the fake records ZERO ssh invocations — the failure happens before
                   any package runs.
                 - An ssh package run exiting 255: Run returns a non-nil error and a nil
                   Report; Unmeasured is empty. Today's ExitError tolerance would swallow
                   this into UnmeasuredMissing ("no covered mutants"), which reads as a
                   clean package — the test asserts it does not.
                 - A remote gremlins exiting 1 (surviving mutants) is still tolerated and
                   its report parsed: the 255-vs-1 distinction is the whole test, and
                   collapsing them either breaks normal runs or hides dead hosts.
                 - Fetch failing with rsync 23 (remote file absent) keeps
                   UnmeasuredMissing; fetch failing with 255 is fatal.
                 - With Remote set and any transport error, no *exec.Cmd is built whose
                   argv[0] is "gremlins"/"npx"/"dotnet" — proof there is no local
                   degradation path.
                 - A stale LOCAL report at GremlinsReportPath from a previous run is
                   removed before the package's remote run, so a failed run cannot merge
                   yesterday's numbers.

  t-7  Wire config into both construction sites
       files:    internal/cmd/verify.go, internal/cmd/survivor_drain.go,
                 internal/cmd/mutation_remote_wiring_test.go
       covers:   c-1, c-3, c-4
       depends:  t-2, t-4
       desc:     One shared constructor reads the grant + mutation_workers/mutation_test_cpu
                 from local.toml, probes the remote once for cores, and builds the adapters
                 in configuredAdapters and runGremlinsOverPackages from it.
       contract: - With a grant present, configuredAdapters returns Stryker, Gremlins and
                   StrykerNet all carrying the SAME non-nil Target (assert pointer/value
                   equality across the three — an adapter left local is the silent
                   half-remote run).
                 - runGremlinsOverPackages builds a Gremlins equal to the one
                   configuredAdapters builds for the same project + store (field-by-field
                   compare); adding a knob to one site and not the other fails here.
                 - mutation_workers=8, mutation_test_cpu=2 in local.toml: both sites yield
                   Workers==8, TestCPU==2. Unset with no remote: both yield 0, so the
                   adapter defaults (NumCPU/2, 1) still apply — the existing
                   TestGremlinsBuildUnleashArgsDefault is the downstream proof.
                 - Unset workers WITH a remote: Target.Cores comes from the probe, and the
                   gremlins argv carries cores/2.
                 - A probe failure at construction aborts `dross verify` with an error
                   naming the host, before any adapter Run — asserted through the
                   configuredAdaptersFn/probe seams so no verify.toml or tests.json is
                   written (same ordering guarantee requireExecConsent has).

  t-8  Restore remote dependencies before the adapter runs
       files:    internal/mutation/launcher.go, internal/mutation/stryker.go,
                 internal/mutation/stryker_net.go, internal/mutation/restore_test.go
       covers:   c-7, c-4
       depends:  t-4
       desc:     Each adapter declares its remote restore step — Stryker `npm ci`,
                 StrykerNet `dotnet restore`, Gremlins none — run over the ssh seam in the
                 workdir before the tool argv.
       contract: - Stryker remote Run: recorded order is [rsync, ssh npm ci, ssh npx
                   stryker]; the npx argv appearing first fails it.
                 - StrykerNet remote Run issues `dotnet restore` before `dotnet stryker`.
                 - Gremlins remote Run issues NO restore command — the fake fails on any
                   command containing "npm", "dotnet" or "restore".
                 - `npm ci` exiting non-zero: Run returns an error naming "npm ci" and the
                   host, and the stryker argv is never built (a restore failure that fell
                   through would produce a report against a dependency-less tree — an
                   error-shaped clean run).
                 - Remote nil: no restore command is issued for any adapter, so native
                   runs are unchanged.
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (runs on remote, nothing spawned locally, reports land locally) | t-4, t-7 |
| c-2 (tracked project.toml host refused by name) | t-3, t-2 |
| c-3 (workers/test-cpu from config at both sites; unset = NumCPU/2 and 1) | t-2, t-4, t-7 |
| c-4 (broken remote fails loudly, never degrades or reads clean) | t-6, t-7, t-8 |
| c-5 (doctor reports remote readiness) | t-5 |
| c-6 (working tree incl. uncommitted; no stale remote report) | t-1, t-4 |
| c-7 (dependency restore before the adapter) | t-8 |

## Judgment calls

- **Workers/test-cpu live in local.toml's open keys, host/workdir in the consent-only keys.** Rejected putting workers in `[mutation.gremlins]` next to timeout_coefficient: the right worker count is a property of the machine doing the work, and remote_workers derives it from the remote anyway. The split also keeps `dross local set` useful for tuning while leaving consent to the verb.
- **c-2's refusal is an explicit trap field on `project.Mutation`, not a generic unknown-key rejection.** A `MetaData.Undecoded()` sweep would turn every stray project.toml key in every existing repo into a hard error — a much larger change wearing this criterion's clothes. The trap field refuses exactly the named key and is trivially testable.
- **The probe runs once at adapter-construction time, not lazily inside Run.** It buys c-4's early named reachability failure and c-3's remote-derived core count in a single round trip, and it puts the failure before verify writes anything.
- **Transport failures are classified by exit code (ssh 255, rsync 23/24/30), never by matching stderr prose.** Same reasoning the UnmeasuredKind comment already records at gremlins.go:78 — a caller that greps prose breaks the moment the prose is reworded, and here the two branches (fatal vs "package had no report") are opposites.
- **One shared `Launcher` struct embedded in all three adapters, rather than an interface each implements.** The three `buildCmd` bodies are already identical; a struct makes "the seam is shared" true by construction instead of by convention. Cost: t-4 touches four adapter files. Rejected splitting it per adapter — that would triplicate one contract and let two adapters ship remote while the third silently ran local, which is the failure adapter_scope was locked to prevent.
- **Restore is per-adapter code (npm ci / dotnet restore / none), not a configurable command string.** A config-supplied restore command would be a second arbitrary-exec surface with no consent gate behind it — the same hole `trusted_test_command` exists to close.
- **`internal/hostallow` is not reused.** It gates URL hosts for API calls, derived from `[remote].url`; an ssh target is authorized by the consent verb instead. Folding the two would mean granting a mutation host also made it a legal API host.
- **Shell-metacharacter refusal on Workdir is separate from argfence.** argfence's Reject rule covers a leading dash; ssh additionally hands the whole string to a remote shell, so the workdir gets its own refusal in t-1 rather than being assumed safe because it passed the dash check.
