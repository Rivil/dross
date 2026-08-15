# Plan draft — lens: mvp

Phase remote-mutation-runner — 5 tasks across 2 waves

```
Wave 1
  t-1  Add rsync/ssh remote transport seam
       files:    internal/mutation/remote.go, internal/mutation/remote_test.go
       covers:   c-4, c-6, c-7
       depends:  —
       what:     New Remote{Host, Workdir} value with a package-level `remoteRun`
                 exec seam. Methods: Probe() (cores int, err error) — ssh nproc +
                 `command -v` for the named tool; Push(localRoot) — rsync -a
                 --delete --filter=':- .gitignore' --exclude=.git; Restore(lang)
                 — npm ci / dotnet restore / no-op for go; Command(argv) *exec.Cmd
                 — ssh host "cd <workdir> && <argv>"; Fetch(rel) (found bool, err
                 error) — rsync one report path back under the local root.
                 Every method wraps failures as "remote <host>: <step>: <cause>".
       contract: - TestRemotePushArgs: the rsync argv contains --delete, the
                   ':- .gitignore' filter and --exclude=.git; dropping any one
                   fails it.
                 - TestRemoteFetchMissingVsBroken: a fake runner exiting 23
                   (source vanished) yields found=false,nil; exit 255 (ssh
                   connection refused) yields an error whose text names the host
                   and the word "fetch" — the two must not collapse.
                 - TestRemoteProbeUnreachable: runner error ⇒ cores==0 AND a
                   non-nil error naming the host; a probe that returned 0,nil
                   would let c-4's silent-degrade back in.
                 - TestRemoteRestorePerLanguage: lang "node" ⇒ `npm ci`, "dotnet"
                   ⇒ `dotnet restore`, "go" ⇒ zero spawns.

  t-2  Add mutation worker keys, refuse remote in project.toml
       files:    internal/project/project.go, internal/project/project_test.go
       covers:   c-2, c-3
       depends:  —
       what:     MutationGremlins gains Workers and TestCPU (toml `workers`,
                 `test_cpu`). Load() inspects the toml MetaData and returns an
                 error when project.toml carries any key under [mutation*] whose
                 leaf is remote_host or remote_workdir, naming the key and the
                 file and pointing at `dross mutation remote`.
       contract: - TestLoadRefusesRemoteHostKey: a project.toml with
                   `[mutation] remote_host = "helicon"` makes Load return an
                   error whose text contains "mutation.remote_host" and
                   "project.toml"; the *Project is nil, so no caller can proceed
                   on a partly-decoded config.
                 - TestLoadKeepsOtherMutationKeys: timeout_coefficient/workers/
                   test_cpu still decode and Load succeeds — the refusal must be
                   leaf-scoped, not "any unknown [mutation] key".

  t-3  Add mutation-remote consent verb + local keys
       files:    internal/cmd/local.go, internal/cmd/mutation_remote.go,
                 internal/cmd/mutation_remote_test.go, cmd/dross/main.go
       covers:   c-2
       depends:  —
       what:     localStore gains flat MutationRemoteHost / MutationRemoteWorkdir
                 (`mutation_remote_host`, `mutation_remote_workdir`), deliberately
                 absent from localKeys. New `dross mutation remote <user@host>
                 <workdir>` prints the host and workdir it is granting BEFORE
                 writing, plus `--clear` and a no-arg show; it goes through
                 refuseTrackedLocal first. readMutationRemote(root, repoDir)
                 (Remote, error) is the single reader.
       contract: - TestMutationRemoteKeysNotInLocalSet: `dross local set
                   mutation_remote_host h` exits non-zero with "unknown local key"
                   — the generic key-writer must not be able to grant the remote.
                 - TestMutationRemoteGrantPrintsBeforeWrite: stdout contains both
                   the host and the workdir, and captured output ordering puts
                   them before the local.toml write (write happens after the
                   print in the same RunE; assert the file is absent when the
                   printer is made to fail).
                 - TestReadMutationRemoteRefusesTrackedLocal: with local.toml
                   tracked by git, readMutationRemote returns the refuseTrackedLocal
                   error and an empty Remote — never a host.

Wave 2 (depends t-1, t-2, t-3)
  t-4  Route the three adapters through the remote
       files:    internal/mutation/gremlins.go, internal/mutation/stryker.go,
                 internal/mutation/stryker_net.go, internal/mutation/gremlins_test.go,
                 internal/mutation/stryker_test.go
       covers:   c-1, c-4, c-6, c-7
       depends:  t-1
       what:     Each adapter gains a `Remote *Remote` field. When set: Run
                 calls Push + Restore once up front (fatal on failure), buildCmd
                 wraps argv via Remote.Command instead of exec.Command, and each
                 report is fetched back before it is read — per package inside
                 the gremlins loop, once after the run for the two Strykers.
                 Gremlins' worker default reads Remote's probed core count
                 (cores/2, min 1) rather than runtime.NumCPU. Push failure,
                 restore failure and transport-level fetch failure return an
                 error from Run; only "remote wrote no report" stays
                 UnmeasuredMissing.
       contract: - TestGremlinsRemoteSpawnsOnlySSH: with Remote set, every argv
                   the fake builder sees starts with ssh or rsync — no `gremlins`
                   or `go` process is constructed locally (c-1's "spawns no
                   compile or test process").
                 - TestGremlinsRemoteFetchPerPackage: for two packages the fake
                   runner records run(pkgA), fetch(pkgA), run(pkgB), fetch(pkgB)
                   in that order; a batched end-of-run fetch fails it.
                 - TestGremlinsRemotePushFailureIsFatal: rsync push exiting
                   non-zero makes Run return an error and produce NO Report — it
                   must not fall through to a local run or an empty merged report.
                 - TestGremlinsRemoteWorkersFromProbe: probe reporting 16 cores
                   with Workers unset puts `--workers 8` in the argv regardless of
                   the local NumCPU.
                 - TestStrykerRemoteRestoreBeforeRun: for the Stryker adapter the
                   fake runner sees `npm ci` before the first stryker argv; for
                   Gremlins it sees no restore command at all.

  t-5  Wire remote into verify, drain and doctor
       files:    internal/cmd/verify.go, internal/cmd/survivor_drain.go,
                 internal/cmd/doctor.go, internal/cmd/verify_test.go,
                 internal/cmd/doctor_test.go
       covers:   c-1, c-3, c-5
       depends:  t-1, t-2, t-3
       what:     configuredAdapters and runGremlinsOverPackages both read
                 readMutationRemote + p.Mutation.Gremlins.Workers/TestCPU and set
                 them on the constructed adapters; a read error aborts rather than
                 building a local adapter. doctor grows a "Mutation remote:" block
                 that prints the configured host/workdir, runs Remote.Probe, and
                 reports reachability and the configured adapter's tool presence
                 as an issue when either fails.
       contract: - TestConfiguredAdaptersCarryWorkers: workers=6/test_cpu=2 in
                   project.toml ⇒ the constructed Gremlins has Workers==6 and
                   TestCPU==2; unset ⇒ both zero so the adapter's NumCPU/2 and 1
                   defaults still apply.
                 - TestDrainAdapterCarriesRemoteAndWorkers: the same two values
                   plus the Remote appear on the Gremlins built in
                   runGremlinsOverPackages — the drain site drifting from verify
                   is exactly what c-3 names.
                 - TestConfiguredAdaptersAbortOnTrackedLocal: with a tracked
                   local.toml, adapter construction returns the refusal error and
                   no adapter list — it must not silently build local adapters.
                 - TestDoctorRemoteUnreachable: with a host configured and the
                   probe seam failing, doctor's output contains the host and the
                   word "unreachable" and the exit is non-zero; with no host
                   configured the block prints nothing.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (remote run, local reports, verify unchanged) | t-4, t-5 |
| c-2 (tracked project.toml refused; local-only authorization) | t-2, t-3 |
| c-3 (workers/test-cpu at both construction sites) | t-2, t-5 |
| c-4 (loud named failure, never degrade) | t-1, t-4 |
| c-5 (doctor reports remote readiness) | t-5 |
| c-6 (working tree incl. uncommitted; no stale workdir) | t-1, t-4 |
| c-7 (dependency restore before the adapter) | t-1, t-4 |

7/7 criteria covered.

## Judgment calls

- **Workers/test-cpu live in project.toml `[mutation.gremlins]`, not local.toml.** Rejected adding two more flat local keys: the locked flat-key shape covers the *consent-bearing* remote, and workers/test_cpu authorize nothing — timeout_coefficient already sits in that table, so this is a zero-new-machinery extension rather than a second config surface.
- **One transport type shared by three adapters, not an Adapter-level decorator.** Rejected wrapping each Adapter in a RemoteAdapter: the per-package fetch has to interleave with the gremlins loop, which a wrapper outside Run cannot see, and the locked report_fetch decision requires that interleaving.
- **Fetch distinguishes exit 23 from every other failure** rather than treating any rsync failure as "no report". Rejected an extra `ssh test -f` round trip per package: it doubles the per-package latency to answer a question the exit code already answers, and c-4 only requires that a *broken* fetch be loud.
- **c-2's refusal lives in project.Load, not in a doctor check.** Rejected a warn-only doctor line: a refusal that lets the run proceed is not a refusal, and Load is the one chokepoint every command already goes through.
- **`dross mutation remote` as a parent+sub with three modes (grant / --clear / show)**, rejected three sibling verbs — one command is one thing to document and one place the host+workdir print lives.
- **No `dross local get` support for the remote keys either.** Rejected exposing them read-only: readMutationRemote is the single reader, and a second read path is a second thing to keep going through refuseTrackedLocal.
- **Merged the schema field and the c-2 refusal into t-2** (both in project.go) so no two wave-1 tasks touch the same file — the alternative was a 10-minute task whose only content was two struct fields.
