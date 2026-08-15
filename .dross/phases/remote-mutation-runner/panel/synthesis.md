# Panel synthesis — remote-mutation-runner

Judged cold: I authored none of the three drafts. Every file path, symbol and test
name below was spot-checked against the working tree at
`/Users/rivil/Development/dross`.

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| **risk** (11t/4w) | Strongest — a named failure mode per task, each criterion owned; c-7 covered only via t-6 invoked by t-7, which is real but indirect | High — seam-level assertions ("substitutes a `runnerBuildCmd` that `t.Fatal()`s if called"), but written as prose rather than named test funcs, and cites `TestSubprocessArgsAudit`, which does not exist | Over-split — t-6 (deps table) and t-3 (consent verb) are near-trivial; 11 tasks for a 5-file change area | Sound — 4 waves, honest deps; t-10 in wave 2 on only t-1 is defensible since it needs only the sentinel |
| **mvp** (5t/2w) | Weakest — all 7 claimed, but c-4 stops at "Run returns an error" and never reaches `verify.go:508`, where an adapter failure still becomes a FLAG. "Never reads as clean" is not closed | Good — named test funcs, and the exit-code split (rsync 23 vs ssh 255) is the sharpest single contract in the panel | Too coarse — t-4 alone touches 5 files and claims 4 criteria; not an atomic commit | Correct but blunt — 2 waves, no doctor/verify separation, so the whole runtime lands in one wave |
| **verification** (8t/3w) | Complete and grounded — the only draft carrying `README.md` + `docs/dross.1` into the consent task, matching the real `TestDocsCoverAllowHosts` doc gate at `internal/cmd/local_test.go` | Highest — anchors to existing tests that must keep passing (`TestStrykerRunArgsPinned`, `TestGremlinsBuildUnleashArgsDefault`, `TestGremlinsRunRePrefixesPackagePaths`, `TestReadAllowHostsRefusesTrackedLocal`), all verified present; also the only draft refusing shell metacharacters in Workdir, a real hole argfence's dash rule does not cover | Right-sized — 8 tasks, one large (t-4 launcher) with justification | Mostly sound — 3 waves; t-6 and t-8 both land in wave 3 on `internal/mutation/launcher.go`, a same-wave file collision |

**Skeleton: `verification`.** It is the only draft whose contracts are anchored to
symbols that provably exist in this repo, and the only one that treats "argv is
byte-identical when Remote is nil" as a named regression anchor rather than an
aspiration. Its granularity is also the closest to one-task-one-commit.

Two citation defects found while checking, corrected below: `TestSubprocessArgsAudit`
(risk t-1) is not a real test — the fail-closed gate is `TestUnknownBinaryFailsClosed`
plus `TestAuditKnowsEveryPolicyBinary` in `internal/cmd/subprocargs_audit_test.go`.
And `runGremlinsOverPackages` is at `internal/cmd/survivor_drain.go:134`, not 139.

## Merged plan

9 tasks across 3 waves.

```
Wave 1

  t-1  Add remote transport package and argv policy                   [verification+risk]
       files:    internal/remote/remote.go, internal/remote/remote_test.go,
                 internal/argfence/policy.go, internal/argfence/argfence_test.go
       covers:   c-6, c-4
       depends:  —
       desc:     New internal/remote owning Target{Host,Workdir,Cores}, the pure argv
                 builders (SSHArgs, SyncArgs, FetchArgs), a Probe reading remote cores +
                 tool presence, an exec seam var, exit-code classification, and the error
                 sentinels the phase branches on (ErrTransport, ErrPartial,
                 ErrRemoteCommand). argfence gains Reject entries for ssh and rsync.
       contract: - SyncArgs(Target{Host:"helicon",Workdir:"/srv/x"}, "/local/repo") yields
                   argv[0]=="rsync" carrying --delete, --filter=':- .gitignore',
                   --exclude=.git, a trailing-slash source and dest "helicon:/srv/x";
                   dropping --delete or the .gitignore filter fails it.
                 - SSHArgs wraps the adapter argv as `ssh <host> cd <workdir> && <argv>`;
                   argv[0] is "ssh" and the adapter binary never appears at argv[0].
                 - A Workdir containing ';', '$(', '`' or a newline is refused with an
                   error naming the workdir, and argv is not built — ssh hands the string
                   to a remote shell, which the dash rule does not cover.   [verification]
                 - A Host beginning with '-' is refused; ssh and rsync carry Kind==Reject
                   in Policy(), and DELETING either entry makes the fail-closed audit
                   (TestUnknownBinaryFailsClosed / TestAuditKnowsEveryPolicyBinary,
                   internal/cmd/subprocargs_audit_test.go) go red.               [risk]
                 - classify(exit): ssh 255 -> ErrTransport; rsync 23/24 -> ErrPartial;
                   any other non-zero -> ErrRemoteCommand carrying the code. Read from the
                   exit code, never from stderr prose.                            [mvp]
                 - Probe parses nproc-style output into Cores; unparseable or zero output
                   is an ERROR, and the test asserts the probe never returns the local
                   runtime.NumCPU() — a silent fallback reinstates the exact bug the
                   locked remote_workers decision exists to fix.                  [risk]

  t-2  Add remote consent store and grant verb                        [verification+risk]
       files:    internal/cmd/local.go, internal/cmd/mutation_remote.go,
                 internal/cmd/mutation_remote_test.go, cmd/dross/main.go,
                 README.md, docs/dross.1
       covers:   c-2, c-3
       depends:  —
       desc:     localStore gains mutation_remote_host + mutation_remote_workdir
                 (deliberately ABSENT from localKeys, internal/cmd/local.go:80) and
                 mutation_workers + mutation_test_cpu (present in localKeys). New
                 `dross mutation remote grant|status|revoke`, registered in main.go,
                 sharing refuseTrackedLocal.
       contract: - `dross local set mutation_remote_host helicon` fails with "unknown local
                   key" and the printed key list does NOT contain it — the same exclusion
                   TrustedTestCommand has; adding it to localKeys fails this test.
                 - `dross local set mutation_workers 8` round-trips through
                   `dross local get mutation_workers`.
                 - `dross mutation remote grant helicon /srv/dross` prints BOTH host and
                   workdir BEFORE the write: with the write injected to fail, the banner is
                   already in captured stdout and no key is stored.
                 - With .dross/local.toml tracked by git, `status` and the grant reader both
                   return the refuseTrackedLocal error UNREAD — the test writes a valid host
                   into the tracked file and asserts it is still refused.            [risk]
                 - `revoke` clears host AND workdir together (a workdir with no host must
                   not be reachable) and leaves mutation_workers intact.
                 - `status` on an ungranted repo prints "not granted" and exits 0.
                 - README.md and docs/dross.1 both contain "mutation remote grant",
                   "mutation_remote_host" and ".dross/local.toml" — the same doc gate
                   TestDocsCoverAllowHosts applies.                        [verification]

  t-3  Refuse a remote host in tracked project.toml                   [verification+mvp]
       files:    internal/project/project.go, internal/project/project_test.go
       covers:   c-2
       depends:  —
       desc:     project.Mutation gains explicit trap fields (remote_host / remote_workdir)
                 whose only purpose is to be refused; Load errors when either is non-empty.
       contract: - Load on a project.toml with `[mutation]\n remote_host = "helicon"`
                   returns a NIL Project and an error naming both "mutation.remote_host"
                   and "helicon", pointing at `dross mutation remote grant`. Nil matters:
                   no caller can proceed on a partly-decoded config.               [mvp]
                 - The same refusal fires for remote_workdir alone — half a config is not
                   a bypass.
                 - Existing keys under [mutation] (timeout_coefficient, workers) still
                   decode and Load succeeds; the repo's own .dross/project.toml and every
                   project fixture still load clean. The refusal is keyed to the trap
                   field, not to unknown keys generally.

Wave 2 (depends on wave 1)

  t-4  Wrap the three adapters in a shared remote launcher            [verification+mvp]
       files:    internal/mutation/launcher.go, internal/mutation/gremlins.go,
                 internal/mutation/stryker.go, internal/mutation/stryker_net.go,
                 internal/mutation/launcher_test.go
       covers:   c-1, c-3, c-6
       depends:  t-1
       desc:     One Launcher struct (Prefix + *remote.Target) embedded by Gremlins,
                 Stryker and StrykerNet, owning buildCmd, the one-shot rsync push, the
                 per-package fetch, and the remote-derived worker default. Prefix and
                 Target are mutually exclusive — both set is a refusal naming both, not
                 a nesting.                                                       [risk]
       contract: - With Remote set, every *exec.Cmd the three build has argv[0] in
                   {"ssh","rsync"}; the fake seam t.Fatal()s on "gremlins", "npx",
                   "dotnet" or "go" at argv[0] — that failure IS c-1's "spawns no compile
                   or test process".
                 - With Remote nil, argv is byte-identical to today: TestStrykerRunArgsPinned,
                   TestGremlinsBuildUnleashArgsDefault and TestStrykerNetRejectsDashOutputDir
                   keep passing UNMODIFIED.
                 - The tree is pushed exactly once, before the first remote command: the
                   recorded order starts with the rsync argv, and a second push inside the
                   per-package loop fails the test.
                 - Per package the recorded order is [remote rm of that package's report
                   path, ssh gremlins run, fetch] — the missing rm is how a stale remote
                   report gets fetched, and the ordering assertion is what catches it.
                 - The fetch for package N is issued before package N+1's run — an ordering
                   assertion, not a count; a batched end-of-run fetch fails it.     [mvp]
                 - After a remote Run with testdata/gremlins_pkg_report.json delivered by
                   the fake fetch, the file exists at GremlinsReportPath(root, pkg) and the
                   merged Report equals the local expectation in
                   TestGremlinsRunRePrefixesPackagePaths — same per-file keys, same score,
                   so verify's diff scoping is provably unchanged.
                 - Target{Cores:32} with Workers unset yields `--workers 16` regardless of
                   local runtime.NumCPU(); Remote nil with Workers unset still yields the
                   local defaultWorkers() (internal/mutation/gremlins.go:36).

  t-5  Report remote readiness in doctor                              [verification+risk]
       files:    internal/cmd/doctor.go, internal/cmd/doctor_remote_test.go
       covers:   c-5
       depends:  t-1, t-2
       desc:     New "Remote mutation:" section in checkConfigTrust (internal/cmd/doctor.go:837)
                 reading the grant and calling remote.Probe through an overridable seam.
       contract: - No grant: prints an advisory line and doctor's issue count is UNCHANGED —
                   a local-only clone must not start failing doctor.
                 - Granted + probe returns ErrTransport: prints ✗ naming the host and
                   increments issues, so `dross doctor` exits non-zero.
                 - Granted + reachable but gremlins absent: ✗ names both the missing binary
                   and the adapter needing it — a distinct line, not collapsed into
                   reachability. With [mutation].adapters = ["stryker"] the same missing
                   gremlins produces NO finding.
                 - Healthy: prints host, workdir and the probed core count, and the printed
                   count EQUALS what the probe seam returned — the number doctor shows is
                   the number the worker default will use.
                 - doctor calls the SAME probe seam the run uses: a substituted seam sees
                   exactly one call, so doctor cannot pass on a check verify performs
                   differently.                                                   [risk]

Wave 3 (depends on wave 2)

  Sequencing note: t-6 and t-8 both edit internal/mutation/launcher.go. Land t-6 first;
  t-8's restore step slots ahead of the tool argv in the same call path.

  t-6  Fail loudly on a broken remote                                 [verification+mvp]
       files:    internal/mutation/launcher.go, internal/mutation/gremlins.go,
                 internal/mutation/remote_failure_test.go
       covers:   c-4
       depends:  t-4
       desc:     Map remote.ErrTransport/ErrPartial onto adapter outcomes: transport
                 failures are fatal and named; only a genuinely absent remote report stays
                 an UnmeasuredMissing skip. No local fallback path exists.
       contract: - rsync push exits 255: Run returns an error naming the host and "rsync",
                   produces NO Report, and the fake records ZERO ssh invocations.  [mvp]
                 - An ssh package run exiting 255: Run returns non-nil error and a nil
                   Report; Unmeasured is EMPTY. Today's ExitError tolerance would swallow
                   this into UnmeasuredMissing, which the drain treats as non-fatal and the
                   score excludes — the empty-report-reads-as-clean mode c-4 forbids.
                 - A remote gremlins exiting 1 (surviving mutants) is still tolerated and
                   its report parsed — the 255-vs-1 distinction is the whole test.
                 - Fetch failing rsync 23 (remote file absent) keeps UnmeasuredMissing;
                   fetch failing 255 is fatal.
                 - With Remote set and any transport error, no *exec.Cmd is built whose
                   argv[0] is "gremlins"/"npx"/"dotnet" — proof there is no local
                   degradation path.
                 - A stale LOCAL report at GremlinsReportPath from a previous run is removed
                   before the package's remote run, so a failed run cannot merge yesterday's
                   numbers.

  t-7  Wire config into both construction sites                       [verification+risk]
       files:    internal/cmd/verify.go, internal/cmd/survivor_drain.go,
                 internal/cmd/mutation_remote_wiring_test.go
       covers:   c-1, c-3, c-4
       depends:  t-2, t-4
       desc:     One shared constructor reads the grant + mutation_workers/mutation_test_cpu,
                 probes the remote once for cores, and builds the adapters in
                 configuredAdapters (internal/cmd/verify.go:237) and runGremlinsOverPackages
                 (internal/cmd/survivor_drain.go:134).
       contract: - With a grant present, configuredAdapters returns Stryker, Gremlins and
                   StrykerNet all carrying the SAME non-nil Target — an adapter left local
                   is the silent half-remote run.
                 - runGremlinsOverPackages builds a Gremlins field-by-field EQUAL to the one
                   configuredAdapters builds for the same project + store; adding a knob at
                   one site only fails here. The risk IS the divergence, so one table
                   spanning both sites is the only shape that catches it.          [risk]
                 - mutation_workers=8, mutation_test_cpu=2: both sites yield Workers==8,
                   TestCPU==2. Unset with no remote: both yield 0, so defaultWorkers() and
                   DefaultTestCPU still apply — TestGremlinsBuildUnleashArgsDefault is the
                   downstream proof.
                 - Unset workers WITH a remote: Target.Cores comes from the probe and the
                   gremlins argv carries cores/2.
                 - A probe failure, or a refusal (tracked local.toml / tracked project.toml),
                   aborts `dross verify` with an error naming the host BEFORE any adapter
                   Run — asserted through the seams so no verify.toml or tests.json is
                   written. It must never return a local-only adapter list.

  t-8  Restore remote dependencies before the adapter runs            [verification+risk]
       files:    internal/mutation/launcher.go, internal/mutation/stryker.go,
                 internal/mutation/stryker_net.go, internal/mutation/restore_test.go
       covers:   c-7, c-4
       depends:  t-4, t-6
       desc:     A CLOSED adapter→restore table: Stryker `npm ci`, StrykerNet
                 `dotnet restore`, Gremlins none — run over the ssh seam in the workdir
                 before the tool argv. An adapter name absent from the table is an error,
                 not a silent no-op.                                              [risk]
       contract: - Stryker remote Run: recorded order is [rsync, ssh npm ci, ssh npx
                   stryker]; the npx argv appearing first fails it.
                 - `npm ci`, not `npm install` — the lockfile-respecting form is the point
                   and the test pins the exact argv.                               [risk]
                 - StrykerNet remote Run issues `dotnet restore` before `dotnet stryker`.
                 - Gremlins remote Run issues NO restore command at all — the fake fails on
                   any command containing "npm", "dotnet" or "restore".
                 - `npm ci` exiting non-zero: Run returns an error naming "npm ci" and the
                   host, and the stryker argv is NEVER built — a restore failure that fell
                   through would report against a dependency-less tree.
                 - An adapter name absent from the table is an error, so a fourth adapter
                   cannot silently skip its restore.                               [risk]
                 - Remote nil: no restore command for any adapter; native runs unchanged.

  t-9  Escalate a remote-leg failure to BLOCKING in verify            [risk]
       files:    internal/verify/verify.go, internal/verify/verify_test.go
       covers:   c-4
       depends:  t-6
       desc:     Skeleton currently turns ANY adapter error into a FLAG finding
                 (internal/verify/verify.go:508). Capture whether the error wraps
                 remote.ErrTransport while the error value is still live, carry it on
                 LanguageRun (internal/verify/verify.go:74), and emit BLOCKING with the
                 host named for that class.
       contract: - An adapter error wrapping remote.ErrTransport produces a BLOCKING
                   finding and a non-zero blocking count — a phase cannot be verified past
                   a leg that never ran.
                 - A plain adapter error (stryker misconfigured, no remote involved) stays
                   a FLAG — the escalation does not swallow existing behaviour.
                 - The leg still records its Files, so the report names WHICH files went
                   unmeasured rather than showing an empty, clean-looking language run.
                 - Record-and-continue is preserved: a failing remote gremlins leg does not
                   discard a finished stryker leg in the same run.
```

### Criteria coverage

| Criterion | Tasks |
|---|---|
| c-1 remote run, no local process, reports land locally | t-4, t-7 |
| c-2 tracked project.toml refused by name; only untracked config authorizes | t-2, t-3 |
| c-3 workers / test-cpu from config at both sites; unset = today | t-2, t-4, t-7 |
| c-4 broken remote fails loudly, never degrades or reads clean | t-1, t-6, t-7, t-8, t-9 |
| c-5 doctor reports remote readiness | t-5 |
| c-6 working tree incl. uncommitted; no stale remote report | t-1, t-4 |
| c-7 dependencies restored before the adapter runs | t-8 |

## Disagreements

**1. Should a failed remote leg block the phase, or just flag it?**
`risk` adds a task in `internal/verify/verify.go` escalating a transport-class adapter
error from FLAG to BLOCKING; `mvp` and `verification` both stop at "Run returns an
error" and never touch verify.go. Provisional default: **include the escalation
(t-9)**. It matters because c-4 says a broken remote must never read as clean, and
`verify.go:508` currently files every adapter failure as a FLAG — one FLAG among a
dozen in a partial verdict is exactly "reads as clean". Without t-9 the phase can ship
with c-4 half-met at the only layer a human actually reads.

**2. Where do worker count and test-cpu live — tracked project.toml, or machine-local
local.toml?**
`mvp` puts them in `[mutation.gremlins]` beside the existing `timeout_coefficient`,
arguing they authorize nothing; `risk` and `verification` put them in local.toml's open
keys, arguing the right worker count is a property of the machine doing the work.
Provisional default: **local.toml** (2-of-3). It matters because it decides whether a
laptop's tuning gets committed and inherited by every clone and by CI — and because
the locked `remote_workers` decision already derives the default from the remote's core
count, which is machine-local by nature.

**3. New `internal/remote` package, or a file inside `internal/mutation`?**
`risk` and `verification` create `internal/remote`; `mvp` puts the transport in
`internal/mutation/remote.go`. Provisional default: **new `internal/remote` package**.
It matters because `internal/cmd/doctor.go` and the consent verb both need the config
and the probe: with the transport inside `internal/mutation`, doctor's test has to
import an adapter package to substitute a probe seam, and the seam stops being
independently substitutable. Note `internal/project/remote.go` already exists and means
the git forge — the name collision is conceptual only, but worth a deliberate nod.

**4. Consent verb name: `dross runner trust` or `dross mutation remote grant`?**
`risk` proposes `dross runner trust|show|revoke`, explicitly rejecting reuse of `dross
trust` because exec consent binds to a command hash while remote consent binds to a
host and workdir. `mvp` and `verification` both propose `dross mutation remote
grant|status|revoke`. Provisional default: **`dross mutation remote
grant|status|revoke`** (2-of-3, and it keeps the noun next to the thing it configures).
It matters because it is the user-facing surface, it goes into README.md and
docs/dross.1 under a doc gate, and renaming it later breaks both.

**5. Restore keyed by adapter, or by language?**
`verification` makes restore per-adapter code declared on the adapter; `mvp` keys a
table by language ("node"/"dotnet"/"go"); `risk` uses a closed adapter→command table
that errors on an unknown adapter. Provisional default: **closed table keyed by
adapter, fail-closed on unknown**. It matters because a fourth adapter added later must
not silently skip its restore and produce a report against a dependency-less tree —
which is an error-shaped clean run, the c-4 failure in its most deniable form.

**6. When does the remote probe run — at adapter construction, or lazily inside Run?**
`verification` and `risk` probe once at construction time so verify aborts before it
writes anything; `mvp` folds the probe into the transport type with no stated call
timing, implying it happens when first needed. Provisional default: **probe at
construction time**. It matters because it decides whether `dross verify` against a
dead host fails clean, or fails after having already written verify.toml and tests.json
that a later reader takes as a real run.

**7. How finely should the adapter runtime be split?**
`mvp` lands push, restore, fetch, workers and all three adapters in a single task;
`verification` splits launcher / failure-handling / restore across two waves; `risk`
splits further still, into six. Provisional default: **verification's split** (t-4,
t-6, t-8). It matters for commit atomicity under the repo's test-gate rule: mvp's t-4
cannot be committed with an observed-green gate for any one criterion, and risk's
split produces tasks whose only content is a three-entry table.
