# MVP-lens draft — remote-host-mutex

Bias applied: smallest task set that satisfies c-1..c-7 under the eight locked
decisions. No task exists that is not traceable to a criterion. The launcher,
the adapters and `remote_pool.go` are untouched: the attached leg's lock is a
sibling ssh session held by `cmd`, not a lock threaded through every ssh call.

```
Phase remote-host-mutex — 5 tasks across 2 waves

Wave 1
  t-1  Add host-lock script builders + hold session
       files:    internal/remote/lock.go, internal/remote/lock_test.go
       covers:   c-1, c-2, c-4, c-7
       description:
         New file owning EVERYTHING that touches /tmp/dross-host.lock, so the
         fs.protected_regular hazard has exactly one home:
           - `HostLockPath = "/tmp/dross-host.lock"`, `LockTool = "flock"`.
           - `Holder{Project, Phase, RunID string}` + `FormatHolder`/`ParseHolder`
             over ONE line of space-separated k=v (`project= phase= run= pid= since=`);
             pid is the host shell's `$$`, since is host `date -u` RFC3339, both
             filled in by the script, never by the local clock.
           - `LockPrologue(h Holder, w LockWait, waitingFile string) string` — a
             bash fragment: create-if-absent under `umask 000` so the file lands
             0666; `exec 9<PATH` (read-only, no O_CREAT); `flock` on fd 9 per
             mode: WaitForever (`flock -n 9 || { print holder; [touch waitingFile];
             flock 9; [rm waitingFile]; }`), NoWait (`flock -n 9 || { print
             holder; exit 75; }`), Bounded(d) (`flock -w <secs> 9`, fallthrough
             prints "host held by <holder> — spawning anyway" to stderr and
             continues). Record is written ONLY inside the acquired branch, via
             `printf ... | dd of=PATH conv=nocreat status=none`. Emits
             `dross-lock: waiting on <record>` / `dross-lock: held` on stdout.
           - `ScriptAllLocked(t, prologue, cmds)` — ScriptAll with the prologue
             after the preamble, before the first `&&` command, so the final
             `exec` inherits fd 9 and the tool holds the lock until it exits.
           - `LockStatusScript(t)` — for doctor: absent file → `lock=free`;
             else open 9< and `flock -n 9` → `lock=free`, or `lock=held` +
             `holder=<record line>`. `ParseLockStatus(out)`.
           - `HoldLock(t, h Holder, w LockWait, notices io.Writer) (*Hold, error)`
             — the attached leg's session: ONE ssh with a live stdin pipe
             running `HoldScript(h, w)` = prologue + `read -r _` (blocks on
             stdin; EOF when the local process dies → shell exits → kernel
             releases). Reads stdout: prints the waiting line once, a heartbeat
             line every `holdHeartbeat` (var, 3m) while unheld, returns after
             `held`. Exit 75 → `ErrHostBusy` carrying the parsed Holder; exit
             127 → error naming flock as missing and pointing at `dross doctor`.
             `Hold.Release()` closes stdin and waits. Spawns through a
             `holdSpawn` seam. The hold script contains NO `cd` — the lock is
             independent of the workdir, which may not exist before first push.
       contract: TestLockPrologueOpensReadOnlyWithoutCreate — the prologue's
                   only open of the path is `exec 9<`; a `>`, `>>` or `<>` on
                   the path, or `flock` invoked with the path (not `9`) as its
                   argument, fails the test.
                 TestLockPrologueCreatesAbsentFileWorldWritable — the create
                   branch is guarded by `[ -e PATH ] ||` and runs under
                   `umask 000` (or explicit `chmod 666`); a create that runs
                   unconditionally or leaves umask alone fails.
                 TestLockRecordWrittenWithoutCreateAndOnlyWhenHeld — the record
                   write uses `dd … conv=nocreat`; for Bounded, the write sits
                   inside the `if flock -w` success branch — a write reachable
                   on the fallthrough path (which would clobber the real
                   holder's record) fails the test.
                 TestLockPrologueModes — WaitForever contains `flock 9` after a
                   failed `flock -n 9`; NoWait contains `exit 75` and no
                   blocking `flock 9`; Bounded(10m) contains `flock -w 600 9`;
                   Bounded(0) contains `flock -w 0 9`; only WaitForever with a
                   waitingFile touches/removes that file around the block.
                 TestHolderRecordRoundTrips — ParseHolder(FormatHolder(h))==h
                   for project/phase/run; a record missing `run=` or with a
                   non-integer pid is an error, not a zero Holder.
                 TestScriptAllLockedKeepsExecLast — the prologue precedes the
                   first command and the last command is still `exec`'d; the
                   prologue is never inside the `&&` chain.
                 TestHoldLockPrintsHolderOnceThenHeartbeats — fake holdSpawn
                   emits `dross-lock: waiting on <rec>` then, after two
                   heartbeat ticks (holdHeartbeat=5ms), `dross-lock: held`;
                   notices contain the holder's project/phase/run exactly once
                   and ≥2 heartbeat lines; HoldLock returns only after `held`.
                 TestHoldLockBusyIsANamedError — fake exits 75 after the waiting
                   line: errors.Is(err, ErrHostBusy) and the error names the
                   holder's phase; fake exits 127: error text contains "flock"
                   and "dross doctor".
                 TestHoldReleaseClosesStdin — fake records EOF on its stdin;
                   Release() produces it and returns; a Release that leaves the
                   pipe open (fake never sees EOF) fails.
                 TestHoldScriptDoesNotCd — HoldScript contains no `cd `.
                 TestLockStatusScriptParses — `lock=held\nholder=project=a …`
                   → Held=true + Holder parsed; `lock=free` → Held=false;
                   output with neither line is an error.

Wave 2 (depends t-1)
  t-2  Wire attached verify to hold the lock
       files:    internal/cmd/verify.go, internal/cmd/verify_lock_test.go, README.md, ARCHITECTURE.md
       covers:   c-1, c-3
       description:
         In the attached path, when `tuning.Target != nil` (not --local, not a
         fallback): `hold, err := remote.HoldLock(*tuning.Target, Holder{proj.Name,
         phaseID, newRunID(now)}, mode, os.Stderr)` BEFORE `verify.RunScoped`,
         `defer hold.Release()` — one acquisition spanning every adapter and
         package (locked lock_granularity). New `--no-wait` flag → NoWait;
         `ErrHostBusy` → `&ExitCodeError{Code: exitVerifyHostBusy(15)}` naming
         the holder. Attached wait prints holder once + heartbeat (t-1 does the
         printing; this task only wires the writer). README: `dross verify` row
         gains the lock sentence + `--no-wait`; `verify status` row gains the
         "scheduled — waiting on <holder>" reading; ARCHITECTURE.md gains one
         "Remote host lock" paragraph (flock on fd, /tmp path, holder record,
         no bypass) with pointers to `internal/remote/lock.go`.
       contract: TestAttachedVerifyHoldsBeforeFirstSpawnAndReleasesAfter — with
                   a granted host, a recording holdSpawn and a recording
                   launcher seam, the hold spawn is the first remote event, the
                   Release EOF is after the last adapter ssh; a hold that starts
                   after the rsync push or releases between two packages fails.
                 TestNoWaitOnABusyHostExits15NamingTheHolder — holdSpawn fake
                   exits 75 with `dross-lock: waiting on project=chess phase=p3
                   run=r1 …`; `dross verify <phase> --no-wait` returns
                   ExitCode 15, the message contains `chess`, `p3`, `r1`, and
                   the launcher seam recorded zero spawns.
                 TestLocalVerifyNeverTouchesTheLock — `--local` (and a
                   transport fallback) never calls holdSpawn; a lock spawn on
                   either path fails.
                 TestReadmeDocumentsTheHostLock (in verify_lock_test.go) — the
                   `dross verify <phase> --detach` row or the verify row
                   contains `--no-wait` and `dross-host.lock`; the `dross verify
                   status` row contains "waiting on".

  t-3  Lock the detached run and surface its wait
       files:    internal/remote/remote.go, internal/remote/remote_test.go, internal/cmd/verify.go, internal/cmd/verify_lock_status_test.go
       covers:   c-1, c-3, c-4
       description:
         `DetachScript` gains a `Holder` param: inner script = LockPrologue(h,
         WaitForever, runDir/waiting) THEN `printf running > state` THEN argv —
         the inner bash holds fd 9 for the whole run, so a killed run releases
         with no cleanup (c-4). Outer initial state is `scheduled` ALWAYS (a run
         has not started until the lock is held). `StatusScript` emits two more
         labelled lines: `waiting=yes|no` (runDir/waiting exists) and
         `holder=<lock file line>`; `RunStatus` gains `Waiting bool, Holder
         string`. `dispatchDetached` passes Holder{proj.Name, phaseID, runID}.
         `printDetachedStatus`: state `scheduled` + Waiting → `scheduled —
         waiting on <project> <phase> <run> (held since <since>)`.
         `collectDetachedFrom`: `st.State == "scheduled"` → exitResultsScheduled
         (10) with the holder as the reason when Waiting, the --at time when
         `rec.Scheduled()`, else "starting" — no new state, no new exit code
         (locked detached_waiting_state).
       contract: TestDetachScriptLocksBeforeRunningAndAfterScheduling — in the
                   inner script the `flock` text precedes `printf '%s' running`,
                   which precedes argv[0]; with a notBefore the sleep precedes
                   the flock (a run that takes the lock and THEN sleeps until
                   --at holds the host idle — that ordering fails the test).
                 TestDetachScriptInitialStateIsScheduledWithoutAt — zero
                   notBefore still writes `scheduled` as the outer initial
                   state; `running` appears only inside the inner script.
                 TestStatusScriptReportsWaitingAndHolder — the script cats
                   `/tmp/dross-host.lock` into a `holder=` line and tests
                   `<runDir>/waiting` into `waiting=`; ParseStatus of
                   `dir=yes\nstate=scheduled\nwaiting=yes\nholder=project=a phase=b run=c pid=1 since=T`
                   yields Waiting=true, Holder=that line; `waiting=no` → false.
                 TestStatusNamesTheHolderAWaitingRunWaitsOn — detachStatus fake
                   returns scheduled+Waiting+holder; `dross verify status`
                   output contains the holder's project, phase, run id and
                   since; a fake with Waiting=false prints plain `scheduled`.
                 TestResultsOnAWaitingRunExits10NamingTheHolder — same fake with
                   a record that has NO --at: `verify results` exits
                   exitResultsScheduled (10) and names the holder; asserting the
                   old `rec.Scheduled()`-only branch (which would exit 11
                   "still running") fails.
                 TestDispatchPassesTheProjectAndPhaseAsHolder — detachSpawn's
                   recorded script contains `project=<name> phase=<id>` and the
                   run id from the record.

  t-4  Bound the suite's wait on the lock
       files:    internal/cmd/test.go, internal/cmd/test_lock_test.go, README.md
       covers:   c-6
       description:
         `dross test` gains `--wait <dur>` (default 10m). `runRemoteLine` builds
         the remote script with `remote.ScriptAllLocked(t, LockPrologue(Holder{
         proj.Name, "", "test"}, Bounded(wait), ""), [[sh -c line]])` — the
         `exec`'d suite inherits fd 9, so a mutation leg waits on a suite that
         did acquire. On cap expiry the script prints the holder to stderr and
         spawns anyway. `--wait` threads from the flag to runRemoteLine (through
         runTest / runTestLanes / runOneLane / runLanePrepare — one param, no
         package-level state). `--local` path is unchanged. README `dross test`
         row documents `--wait`, the 10m default and `--wait 0`.
       contract: TestSuiteScriptWaitsTenMinutesByDefault — the script handed to
                   spawnRemote for a granted host contains `flock -w 600 9` and
                   the consented line after it; a script with a blocking
                   `flock 9` (unbounded) fails.
                 TestWaitFlagSetsTheCap — `--wait 30s` → `flock -w 30 9`;
                   `--wait 0` → `flock -w 0 9`; `--wait nonsense` is a flag
                   parse error before any spawn.
                 TestCapExpiryNamesTheHolderAndStillSpawns — the fallthrough
                   branch of the script prints "spawning anyway" with the lock
                   file's content and is followed by the `exec sh -c <line>`;
                   a script where the fallthrough `exit`s fails.
                 TestSuiteRecordsItselfAsHolderOnlyWhenAcquired — the record
                   write (`conv=nocreat`) is inside the `if flock -w` branch and
                   names `run=test`.
                 TestLanesShareTheSuiteLockPath — a `--files` run on a granted
                   host produces per-lane scripts each containing `flock -w`;
                   the sync (rsync) argv contains no lock text.
                 TestLocalTestHasNoLock — `--local` spawns nothing through
                   spawnRemote (existing seam), so no lock script exists.

  t-5  Report flock and the holder in doctor
       files:    internal/cmd/doctor.go, internal/cmd/remote_bootstrap.go, internal/cmd/doctor_remote_test.go
       covers:   c-5
       description:
         `remoteMutationTools` appends `remote.LockTool` ("flock") with
         needBy["flock"]="host lock" so doctor, preflight and bootstrap all ask
         one probe. `bootstrapRecipes["flock"]` = refusal ("flock ships in
         util-linux — install it with the host's package manager"). In
         `checkRemoteMutation`'s healthy branch: a missing `flock` prints
         `✗ flock is not installed on <host> — the host lock (mutation runs and
         \`dross test\`) needs it` and counts an issue (own wording, not the
         adapter line); when present, run `remote.LockStatusScript` through a
         new `remoteLockStatusFn` seam and print `⚠ <host> is busy — held by
         <project> <phase> <run> since <since>` or `✓ host lock free`.
       contract: TestDoctorRemoteMissingFlockIsAnIssue — probe fake reports
                   Missing=[flock]: the Remote section contains "flock is not
                   installed on helicon" and "host lock", and issues == 1; the
                   adapter-attributed wording ("the host lock adapter needs it")
                   must NOT appear.
                 TestDoctorRemoteNamesTheHolderWhenBusy — remoteLockStatusFn
                   fake returns held + `project=chess phase=p3 run=r1 pid=4 since=T`:
                   the section contains `chess`, `p3`, `r1` and `T`, and issues
                   == 0 (busy is not a fault).
                 TestDoctorRemoteFreeLockIsOneLine — fake returns free: exactly
                   one lock line, containing "free".
                 TestFlockRidesTheSameProbe — remoteProbeTools(p) contains
                   "flock" exactly once, after the adapter tools; the existing
                   TestRemoteProbeToolsWithNoLanesIsUnchanged still holds
                   because remoteMutationTools is what appends it.
                 TestBootstrapRefusesToInstallFlock — planRemoteBootstrap with
                   flock missing yields a step with Refusal containing
                   "util-linux" and nil Argv; with flock present the step is
                   Present.
```

## Coverage

| criterion | tasks | how |
|---|---|---|
| c-1 | t-1, t-2, t-3 | kernel flock on one fd (t-1); attached leg holds one session across all packages (t-2); detached inner shell holds fd 9 for the whole run (t-3) |
| c-2 | t-1 | absolute `/tmp/dross-host.lock`, hold script has no `cd`; nothing under the workdir is part of the exclusion |
| c-3 | t-2, t-3 | attached: holder printed once + heartbeat on stderr (t-2 wires the writer, t-1 prints); detached: `verify status` / `results` name the holder from `waiting=`/`holder=` (t-3) |
| c-4 | t-1, t-3 | lock lives on an fd of the holding shell (attached: ssh session whose stdin EOFs when dross dies; detached: the inner bash) — no pidfile, no liveness check, nothing to clear |
| c-5 | t-5 | flock in the one probe list; busy holder named via LockStatusScript |
| c-6 | t-4 | `flock -w 600` default, `--wait`, `--wait 0`, fallthrough spawns naming the holder |
| c-7 | t-1 | read-only open without O_CREAT, umask-000 create, `dd conv=nocreat` record write — pinned on the generated script text |

All 7 criteria covered; every task maps to ≥1 criterion.

## Judgment calls

- **Attached lock = one sibling ssh session held by `cmd` (t-2), not a lock threaded through `Launcher`/adapters.** Rejected: putting the hold on `Launcher` (needs a Holder field on Gremlins, Stryker, StrykerNet + launcher + their tests, 5+ files) and per-package `flock` inside `ScriptAll` (violates locked lock_granularity). The session's stdin pipe is the liveness link: local death → EOF → shell exit → kernel release, which is c-4 with zero protocol.
- **One prologue builder with three modes + optional waiting-marker, not one script per surface.** The protected_regular hazard then has one owner and one set of pinning tests; verify, detach and test all consume the same text.
- **Holder record is a single k=v line.** Fits the existing labelled-line `k=v` status protocol (`holder=<line>`) and `cat` into one printf; a multi-line record would need escaping in StatusScript.
- **Detached "waiting" = existing `scheduled` state + a `waiting` marker file in runDir + `holder=` line, no new state value/exit code** (locked). Consequence accepted: the outer initial state becomes `scheduled` for every detached run, and `results` branches on host state rather than `rec.Scheduled()` alone.
- **flock absent at run time is not a pre-run refusal.** c-5 asks doctor to report it; the run-time failure is mapped in HoldLock (exit 127 → "flock missing, run dross doctor"). Rejected: changing `resolveMutationTuning`'s `selectRemoteTarget(targets, nil)` to probe tools — that is remote-preflight scope, not this phase's.
- **`dross test` writes its own holder record (`run=test`) when it acquires.** Spec says the suite "takes the same lock"; a mutation leg waiting on a suite should be able to name it. Rejected: an anonymous suite hold.
- **No separate docs task.** README rows land in the task that adds each flag (t-2, t-4); the ARCHITECTURE paragraph goes with the central wiring (t-2). No test asserts every flag is documented, so docs are pinned by a small new README check in t-2 rather than a task of their own.
- **`remote_pool.go` untouched** (locked pool_busy_is_not_a_skip): host selection stays transport+toolchain; nothing in this plan reads the lock during selection.
- **Exit codes:** remote script exits 75 (EX_TEMPFAIL) on NoWait; `dross verify --no-wait` maps it to 15, next free in verify's family. `dross test` gains no exit code — the cap never fails the suite (locked suite_participation).
- **Heartbeat is local-side (Go ticker), not script-side.** Keeps the script deterministic and pinnable; interval is a package var for tests.
- **`--wait` is threaded as an explicit parameter through the five `test.go` call sites, not a package-level var**, at the cost of signature churn inside one file — cheaper than a hidden global the test double would have to reset.
