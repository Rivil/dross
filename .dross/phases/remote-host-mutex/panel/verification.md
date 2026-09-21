# remote-host-mutex — verification-lens draft

Method: for every criterion the ideal test was written first (what fake, what
assertion, what breaks it), then the smallest task that makes that test
satisfiable. Nothing below exists because the design wanted it; it exists
because a listed contract needs it.

Design the contracts forced (all inside the locked decisions):

- **One text builder owns the c-7 hazard.** `remote.LockPrelude` is the ONLY
  place the lock path is opened, created, chmod'd or written. Every other
  script (`HoldScript`, `DetachScript`, `LockStatusScript`) composes it, so
  one test file pins the O_CREAT-free open, the guarded create + `chmod 666`,
  the `dd conv=nocreat` record write and the fd-9 `flock` form once.
- **A hold is a live ssh session, not a file.** An attached leg is many short
  ssh calls, so "held across every package" (locked lock_granularity) needs a
  process that outlives them and dies with the local one: `remote.Acquire`
  starts `ssh host bash -s`, keeps its stdin pipe open, and the host-side
  script ends in `cat >/dev/null`. Local death → pipe EOF → `cat` exits →
  kernel drops the flock. That is c-4 with no cleanup step, and it is the
  same session the waiter reads the holder record from (one round trip, no
  separate status call). `dross test` and the launcher both go through it.
- **The detached leg locks inside its own inner script** (after the `--at`
  sleep, before `running` is written) so a run waiting on a holder reads as
  `scheduled` for free (locked detached_waiting_state); cancel's group kill
  releases it.
- **Holder identity and wait policy ride on `remote.Target`** (`Target.Lock`),
  on the same argument `Target.Env` already makes: every remote step of a run
  needs it and threading it through three adapters is how one adapter ends up
  not locking — which no_bypass_flag forbids. The launcher REFUSES a remote
  target with no holder identity, so the invariant is a test, not a habit.

```
Phase remote-host-mutex — 9 tasks across 4 waves

Wave 1
  t-1  Lock script builders, parsers, Target.Lock
       files:    internal/remote/lock.go (new), internal/remote/lock_test.go (new),
                 internal/remote/remote.go
       covers:   c-2, c-7 (text half of c-1, c-4)
       description:
                 Add `HostLockPath = "/tmp/dross-host.lock"`, `Holder{Project,
                 Phase, RunID, PID, Since}`, `WaitPolicy{Forever, Max}` (zero =
                 no wait), `LockSpec{Holder, Wait}` and a `Lock LockSpec` field
                 on Target. `LockPrelude(path, spec)` renders the shell fragment
                 that creates-if-absent, opens fd 9 read-only, flocks per policy
                 and writes the k=v holder record; `LockStatusScript(t, path)`
                 + `ParseLockStatus` report tool presence, held/free (by
                 `flock -n` probe, never by file content) and the holder;
                 `ParseHolder` reads the record lines. Validate refuses a
                 holder field carrying a newline.
       contract: LockPrelude output contains `exec 9<'/tmp/dross-host.lock'` and
                 contains NO `>`, `>>` or `<>` redirection aimed at the lock path
                 outside the `[ -e '/tmp/dross-host.lock' ] ||` create guard — if
                 the open ever becomes `flock '/tmp/dross-host.lock' …` or
                 `exec 9<>…`, TestLockPreludeOpensWithoutCreate fails (the
                 protected_regular EACCES for user B).
                 The create guard is followed by `chmod 666 '/tmp/dross-host.lock'`
                 on the same line — dropping the chmod fails
                 TestLockPreludeCreatesWorldWritable.
                 The holder record is piped into `dd of='/tmp/dross-host.lock'
                 conv=nocreat status=none`; a `printf … > lock` form fails
                 TestLockPreludeWritesRecordWithoutCreate.
                 WaitPolicy{Forever} renders `flock -x 9`; {Max: 10m} renders
                 `flock -w 600 9`; zero renders `flock -n 9`; none of the three
                 ever passes the path to flock (TestLockPreludeWaitForms).
                 Record lines are exactly `project=`, `phase=`, `run=`, `pid=$$`,
                 `since=$(date +%s)` in that order, every literal value passed
                 through shellQuote — a project named `a'b` renders `'a'\''b'`
                 (TestLockPreludeQuotesTheHolder).
                 LockPrelude for Target{Workdir:/srv/a} and Target{Workdir:/srv/b}
                 emit byte-identical lock-open and record-write lines, and the
                 lock path never contains the workdir (TestLockPathIgnoresWorkdir
                 — c-2).
                 LockPrelude contains no `rm`, `unlink` or `truncate -s0` of the
                 lock path (TestLockPreludeNeverRemovesTheLock — c-4's "no
                 cleanup" as text).
                 A Holder whose Project/Phase/RunID contains "\n" is refused
                 with ErrUnsafeTarget before any text is built.
                 LockStatusScript output uses `command -v flock` for the tool
                 line and `flock -n 9` for the busy probe; ParseLockStatus of
                 `tool=no` → ToolMissing=true; of `tool=yes\nheld=no\nproject=x…`
                 → Held=false with an EMPTY Holder (a stale record after a dead
                 holder is never reported as busy — c-4); of `held=yes` + record
                 → Held=true and Holder round-trips through ParseHolder with
                 Since as host-epoch time.Unix.
                 ParseLockStatus of output with no `tool=` line returns an error,
                 not a free lock (a banner-only answer is not "free").

Wave 2 (depends t-1)
  t-2  DetachScript takes the host lock
       files:    internal/remote/remote.go, internal/remote/remote_test.go
       covers:   c-1 (detached half), c-4 (detached half)
       description:
                 DetachScript(t, runDir, argv, notBefore) composes LockPrelude
                 (t.Lock, Forever) into the inner script AFTER the --at sleep and
                 BEFORE `printf '%s' running > state`. The initial state file is
                 always `scheduled` — a run is `running` only once it holds the
                 host. Update TestDetachScriptEmitsNoSleepWithoutASchedule for
                 the new initial state.
       contract: In DetachScript's inner `bash -c` text the index of `flock -x 9`
                 is greater than the index of `sleep $((__t - __n))` (when
                 scheduled) and less than the index of `printf '%s' running`, and
                 less than the index of the first argv word — reorder any of the
                 three and TestDetachScriptLocksBeforeItRuns fails (c-1: the
                 tool cannot start before the flock returns).
                 The outer script writes `printf '%s' 'scheduled'` as the initial
                 state even with a zero notBefore (TestDetachedRunStartsScheduled);
                 the old assertion for `'running'` initial state is deleted.
                 The lock prelude sits INSIDE the single-quoted inner string,
                 not in the outer chain — a prelude in the outer script would be
                 released when ssh returns; TestDetachLockIsHeldByTheDetachedJob
                 asserts the `flock` text appears after `setsid nohup bash -c '`
                 and before the closing quote, and never before it.
                 DetachScript refuses a target with an empty Lock.Holder.RunID
                 with ErrUnsafeTarget (no anonymous holders — c-3 needs a name).
                 Existing quoting/exit-code/backgrounds-only-the-job tests still
                 pass byte-for-byte on the parts they pin.

  t-3  Hold session: Acquire, Release, holder events
       files:    internal/remote/hold.go (new), internal/remote/hold_test.go (new),
                 internal/cmd/subprocargs_audit_test.go
       covers:   c-1 (attached half), c-3 (attached wording), c-4 (attached half)
       description:
                 `HoldScript(t)` = preamble + LockPrelude(t.Lock) wrapped in a
                 protocol: `command -v flock || { printf 'noflock\n'; exit; }`,
                 then `flock -n 9 && printf 'held\n' || { printf 'waiting\n';
                 cat LOCK; <wait-cmd> && printf 'held\n' || printf 'expired\n'; }`,
                 record write, then `cat >/dev/null`. `Acquire(t, HoldEvents{Log,
                 HeartbeatEvery})` spawns `ssh host bash -s` through a new
                 `holdCommandFn` seam (StdinPipe kept open, StdoutPipe read),
                 writes the script, reads protocol lines, prints the waiting line
                 and heartbeats to Log, returns `*Hold{Outcome: Held|Alongside,
                 Other: Holder}` or `ErrHostBusy` (zero wait, lock held) /
                 `ErrLockTool` (flock absent). `Release()` closes stdin and
                 waits. `WaitLine(h)` renders "waiting on <project>/<phase> run
                 <run> (pid N) since <RFC3339>". Audit entry for hold.go's
                 Command(argv[0]) form.
       contract: HoldScript contains no `setsid` and no `nohup`, and its last
                 command is `cat >/dev/null` — remove either property and
                 TestHoldSessionDiesWithItsStdin fails (c-4: the lock's lifetime
                 is the ssh session's, which is the local process's).
                 Release closes the stdin pipe BEFORE calling Wait; a fake
                 holdCommandFn running `bash -c 'cat >/dev/null'` returns from
                 Release within 1s (TestReleaseClosesStdinThenWaits) — a Release
                 that Waited first would hang forever.
                 With holdCommandFn faked to `bash -c` printing
                 `waiting\nproject=dross\nphase=p\nrun=r-1\npid=7\nsince=1700000000\n`,
                 sleeping 200ms, printing `held\n` then `cat >/dev/null`:
                 Acquire returns Outcome Held, Other.RunID == "r-1", and Log
                 contains exactly one line matching
                 `waiting on dross/p run r-1 (pid 7) since 2023-11-14T22:13:20Z`
                 (TestAcquirePrintsTheHolderOnce — c-3 attached).
                 Same fake with a 300ms sleep and HeartbeatEvery=50ms: Log
                 contains ≥3 lines beginning `still waiting` each naming r-1, and
                 none after `held` arrives (TestAcquireHeartbeatsWhileWaiting).
                 Fake printing `waiting`, record, `expired`: Acquire under
                 WaitPolicy{Max: 1s} returns Outcome Alongside with Other filled,
                 err nil (TestAcquireExpiredRunsAlongside — the c-6 outcome);
                 under zero WaitPolicy the SAME output returns ErrHostBusy whose
                 message names dross/p run r-1 (TestNoWaitRefusesByName).
                 Fake printing `noflock`: Acquire returns ErrLockTool, message
                 contains "flock" and "dross doctor" (TestMissingFlockIsNamed).
                 Fake printing nothing and exiting 0: Acquire returns an error
                 wrapping ErrRemoteCommand, never a Held hold (an empty answer is
                 not an acquisition).
                 holdCommandFn is invoked with argv exactly SSHArgs(t) —
                 `ssh <host> bash -s` — and the script is written to stdin, never
                 to argv (TestHoldGoesOverStdin; the holder record must not show
                 in the host's `ps`).
                 REAL flock, skipped with t.Skip when exec.LookPath("flock") fails
                 (runs on the Linux `dross test` gate, skips on macOS):
                 holdCommandFn faked to local `bash -s`, LockPath overridden to a
                 t.TempDir file, two Acquires with Forever: the second's Log gets
                 a `waiting on` line naming the first's RunID while the first is
                 held; first.Release() → second returns Held within 2s
                 (TestTwoHoldsSerializeOnARealFlock — c-1 proven, not pinned).
                 Same setup, but the first holder's process is killed with
                 SIGKILL instead of Released: the second returns Held within 2s
                 and the lock file still exists with the first's record in it
                 (TestAKilledHolderReleasesWithNoCleanup — c-4 proven: stale
                 content, no stale lock).
                 subprocargs audit: `hold.go:argv[…]` has an accepted-with-reason
                 entry and TestAuditScansRemotePackage covers hold.go — without
                 it the audit fails closed on the new spawn.

  t-4  Doctor probes flock and names the holder
       files:    internal/cmd/doctor.go, internal/cmd/doctor_remote_test.go
       covers:   c-5
       description:
                 `remoteProbeTools` appends `flock` as a host tool (new
                 `remoteHostTools` list, attributed to neither adapter nor lane).
                 checkRemoteMutation prints a dedicated `✗ flock is not installed
                 on <host> — the host lock needs it; mutation legs and \`dross
                 test\` cannot serialize without it` line and counts an issue.
                 When flock is present it runs a new `lockStatusFn` seam
                 (LockStatusScript → ExecScript → ParseLockStatus) and prints
                 `✓ host lock free` or `⚠ host busy — held by <project>/<phase>
                 run <run> (pid N) since <t>`; busy is advisory, not an issue.
       contract: The tool list handed to remoteProbeFn contains "flock" after the
                 adapter tools (TestDoctorProbesForFlock; update
                 TestRemoteProbeToolsWithNoLanesIsUnchanged's pinned list).
                 remoteProbeFn faked to Missing=["flock"]: output contains
                 "flock is not installed on helicon" and "host lock", issue count
                 rises by one, and the generic "the  adapter needs it" line is
                 NOT printed for flock (TestDoctorReportsMissingFlockAsHostTool).
                 remoteProbeFn healthy + lockStatusFn returning Held with
                 Holder{dross, remote-host-mutex, r-20260921-101500, 4242,
                 since}: output contains "dross/remote-host-mutex run
                 r-20260921-101500 (pid 4242)" and the RFC3339 since; issue
                 count unchanged (TestDoctorNamesTheCurrentHolder).
                 lockStatusFn returning free: output contains "host lock free".
                 lockStatusFn is NOT called when flock is in Missing (a status
                 probe on a host without flock would print a second error for
                 one cause) — TestDoctorSkipsLockStatusWithoutFlock.
                 An ungranted repo prints the existing advisory and no lock line
                 at all (existing TestDoctorRemoteUngrantedIsAdvisory unchanged).

Wave 3 (t-5, t-6, t-8 depend on t-3; t-7 depends on t-2 and t-4)
  t-5  Launcher holds the host once per run
       files:    internal/mutation/launcher.go, internal/mutation/launcher_test.go,
                 internal/mutation/launcher_lock_test.go (new)
       covers:   c-1 (attached), c-3 (attached, wiring), c-4 (attached)
       description:
                 New `launcherHold` seam (= remote.Acquire) and `launcherLog`
                 (= os.Stderr). `ensureHeld()` runs at the top of ensurePushed —
                 before the df probe and the rsync — and stores the *Hold;
                 Close releases it after removeRemoteScratch. newLauncher refuses
                 a remote target whose Lock.Holder.RunID is empty. An Alongside
                 outcome is impossible for a mutation leg (Forever/zero only);
                 ErrHostBusy propagates unchanged so verify can map it.
       contract: With the recorder plus a fake launcherHold that appends a
                 "hold"/"release" tag to the same recording, a two-package
                 gremlins remote run records exactly
                 [hold push rm run fetch rm run fetch release] — a second `hold`
                 anywhere, or `push` before `hold`, fails
                 TestRemoteRunHoldsOnceAcrossEveryPackage (locked
                 lock_granularity; the push is inside the hold so a sync cannot
                 land on a tree another leg is measuring).
                 Close on a launcher whose hold was never taken (local run)
                 calls Release zero times; Close called twice calls Release once
                 (TestCloseReleasesIdempotently).
                 A remote run whose fake launcherHold returns ErrHostBusy spawns
                 NOTHING through launcherCommand (no push, no rm) and the error
                 returned from the adapter's Run satisfies
                 errors.Is(err, remote.ErrHostBusy)
                 (TestBusyRefusalSpawnsNothing).
                 newLauncher(gremlins, "", &Target{Host, Workdir, Lock: zero},
                 …) returns an error naming "holder"; the same target with
                 Lock.Holder.RunID="r-1" is accepted
                 (TestLauncherRefusesAnAnonymousRemoteRun — the no-bypass
                 invariant across all three adapters; existing launcher tests'
                 target() helper gains a RunID).
                 launcherHold is invoked with the SAME Target the tool scripts
                 use except for Env (the lock must not depend on the scratch
                 exports) and with HoldEvents.Log == launcherLog — a fake Log
                 buffer receives the Waiting line the fake hold writes
                 (TestLauncherRoutesWaitLinesToItsLog — c-3 attached reaches the
                 terminal).
                 Existing TestGremlinsRemoteRunOrderAndArgv and
                 TestGremlinsRemotePushesOnceAndFetchesPerPackage keep their
                 push/rm/run/fetch order assertions with hold/release added at
                 the ends.

  t-6  Verify mints the holder, adds --no-wait
       files:    internal/cmd/verify.go, internal/cmd/verify_scoping_test.go,
                 internal/cmd/verify_lock_test.go (new)
       covers:   c-1, c-3 (attached exit path), locked attached_busy_policy
       description:
                 resolveMutationTuning fills `Target.Lock.Holder` with
                 {Project: proj.Project.Name, Phase: phaseID, RunID:
                 newRunID(now)} and Wait Forever; `--no-wait` sets zero wait.
                 dispatchDetached uses the same holder (its RunID is the
                 detached run id). ErrHostBusy from RunScoped becomes
                 `&ExitCodeError{Code: exitVerifyHostBusy (15)}` with the holder
                 in the message. Allowlist "no-wait" in the flag test.
       contract: After resolveMutationTuning on a granted repo, mt.Target.Lock.
                 Holder.Project == the project.toml name, .Phase == the phase id
                 and .RunID matches `^r-\d{8}-\d{6}$`; Wait.Forever is true
                 (TestVerifyStampsTheHolderIdentity). Missing any field and a
                 waiter would print a blank.
                 `dross verify <p> --no-wait` yields Wait == zero WaitPolicy
                 (TestNoWaitSetsZeroWait); without the flag Forever stays true.
                 A fake adapter returning fmt.Errorf("…: %w", remote.ErrHostBusy)
                 makes `Verify()` exit 15 and print the holder text from the
                 error; no tests.json and no verify.toml are written
                 (TestBusyRefusalExitsFifteenAndWritesNothing — a refused leg is
                 not a verdict).
                 The detached record's RunID equals the RunID inside the
                 DetachScript text that detachSpawn received (grep `run='r-…'`)
                 — TestDetachedHolderIsTheRunID; a mismatch would make `verify
                 status` unable to tell "waiting on me" from "waiting on another".
                 TestVerifyFlagAllowlist passes with "no-wait" added and would
                 fail on any other new flag.
                 `--no-wait` with no granted host is a no-op (local run, exit 0),
                 pinned by TestNoWaitWithoutAHostIsInert.

  t-7  Detached status/results name the holder
       files:    internal/cmd/verify.go, internal/cmd/verify_status_test.go,
                 internal/cmd/verify_results_test.go
       covers:   c-3 (detached), locked detached_waiting_state
       description:
                 printDetachedStatus and collectDetachedFrom: when the host
                 reports state `scheduled` and the record is not (or no longer)
                 waiting for --at, consult `lockStatusFn` (the seam t-4 added);
                 if held by a run id other than this record's, the reason line is
                 `waiting on <project>/<phase> run <run> (pid N) since <t>`.
                 results keeps exit 10 for that case. The `rec.Scheduled() &&`
                 guard is dropped: `scheduled` is now the initial state of every
                 detached run.
       contract: detachStatus faked to {DirExists, State:"scheduled"} and
                 lockStatusFn faked to Held by Holder{proj-b, phase-x, r-other,
                 99, since}: `verify status` prints `state    scheduled` followed
                 by a line containing "waiting on proj-b/phase-x run r-other
                 (pid 99) since <RFC3339>" (TestStatusNamesWhatAScheduledRunWaitsOn).
                 Same fakes: `verify results` exits exitResultsScheduled (10) and
                 the error text names proj-b/phase-x run r-other; nothing is
                 written (TestResultsWaitingOnAHolderIsScheduledNotRunning).
                 State "scheduled" with lockStatusFn returning Held by the
                 record's OWN RunID prints no "waiting on" line (a run that
                 holds the host and has not yet flipped to running is racing
                 its own state write, not waiting).
                 State "scheduled" with a record whose ScheduledFor is in the
                 future prints the existing "scheduled for … (host clock)" line
                 and does NOT call lockStatusFn (TestAtScheduledRunSkipsLockProbe
                 — --at explains the wait, one reason per line).
                 State "running" never calls lockStatusFn
                 (TestRunningRunNeverProbesTheLock).
                 An unscheduled record whose host reports `scheduled` and whose
                 lockStatusFn errors (transport) prints `state    scheduled` and
                 "(could not read the host lock)" rather than failing the status
                 listing (status stays useful offline, as it is today).
                 Existing TestResultsOnAScheduledRunSaysScheduled still exits 10.

  t-8  dross test waits up to a cap
       files:    internal/cmd/test.go, internal/cmd/test_remote_test.go,
                 internal/cmd/test_wait_test.go (new)
       covers:   c-6, c-4 (suite half)
       description:
                 `--wait <dur>` (default 10m) on `dross test`. A new
                 `testHold` seam (= remote.Acquire) is called from
                 runTestRemotely/runTestLanes BEFORE syncTreeTo with
                 Target.Lock = {Holder{Project, Phase:"", RunID:"t-<stamp>"},
                 Wait{Max: wait}} (`--wait 0` → zero policy), Log os.Stderr; an
                 Alongside outcome prints "host <h> busy — held by … since …;
                 --wait <d> reached, running alongside" and continues; Held or
                 Alongside are both released after the last lane. `--local`
                 never calls the seam. No local.toml key.
       contract: Default flag value parses to 10*time.Minute and reaches the
                 fake testHold as Lock.Wait.Max == 10m, Forever == false
                 (TestTestWaitDefaultsToTenMinutes); `--wait 30s` reaches it as
                 30s; `--wait 0` reaches it as the zero WaitPolicy and the
                 rendered HoldScript for that spec contains `flock -n 9` and no
                 `-w` (TestWaitZeroSpawnsAtOnce).
                 With spawnRemote recording and testHold faked to return
                 Outcome Alongside with Other={dross, remote-host-mutex, r-1,
                 4242, since}: the suite's ssh IS spawned, stderr contains
                 "r-1" and "running alongside", exit code is the suite's own
                 (0 on a green fake) — TestExpiredWaitSpawnsAnywayNamingTheHolder
                 (c-6: the task gate is never held for a leg).
                 The WaitPolicy handed to testHold never has Forever set, for
                 any --wait value including a huge one (TestTestNeverWaitsForever)
                 — the ErrHostBusy refusal path is unreachable from `dross test`
                 by construction, and c-6's "never held for the length of a leg"
                 rests on that.
                 Recording order across seams: [hold, rsync-sync, ssh-suite,
                 release] — hold before sync, release after the suite
                 (TestTestHoldsAcrossSyncAndSuite); on a lanes run with two
                 matched lanes: [hold, sync, ssh, ssh, release] — one hold, not
                 one per lane (TestLanesShareOneHold).
                 `dross test --local` calls testHold zero times
                 (TestLocalTestTakesNoLock).
                 A fake testHold returning ErrLockTool fails the run with exit
                 exitTransport-distinct code exitToolchainMissing (8) and names
                 flock and the host (TestMissingFlockOnTheSuiteHostIsAToolchainGap).
                 Release is called even when the suite spawn returns an error
                 (deferred), pinned by a fake that records Release after a
                 failing spawnRemote (TestSuiteFailureStillReleases).

Wave 4 (depends t-6, t-7, t-8)
  t-9  Document the host lock
       files:    README.md, ARCHITECTURE.md, assets/prompts/verify.md,
                 internal/cmd/options_docs_test.go
       covers:   c-3, c-5, c-6 (documentation halves), locked no_bypass_flag
       description:
                 README rows: `dross verify` gains the wait/`--no-wait`
                 sentence and exit 15; `dross verify status`/`results` gain the
                 "scheduled — waiting on <holder>" reading; `dross test` gains
                 `--wait <dur>` (default 10m, 0 = at once) and the alongside
                 line; `dross doctor` Remote gains flock + "host busy".
                 ARCHITECTURE.md: a "Host lock" subsection under the remote
                 section naming /tmp/dross-host.lock, flock on fd 9, the
                 protected_regular hazard and the hold-session lifetime.
                 verify.md prompt: a `scheduled` result that names a holder is
                 a wait, not a failure — do not cancel it.
       contract: TestReadmeDocumentsTheHostLock asserts README contains
                 "--no-wait", "--wait", "/tmp/dross-host.lock", "flock",
                 "waiting on" and "running alongside"; each string missing fails
                 by name.
                 TestArchitectureDocumentsTheHostLock asserts ARCHITECTURE.md
                 contains "/tmp/dross-host.lock", "protected_regular",
                 "conv=nocreat" and "cat >/dev/null" — the four facts a future
                 editor would "simplify" away and break c-7 or c-4.
                 TestVerifyPromptTeachesTheDetachedPath extended: verify.md
                 contains "waiting on" and "not a failure" beside the existing
                 --detach strings.
                 TestReadmeDocumentsDetachedRuns still passes (the state
                 vocabulary is unchanged: no new state word is introduced —
                 locked detached_waiting_state).
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 (text), t-2 (detached), t-3 (attached session + real-flock proof), t-5 (launcher order), t-6 (holder wiring) |
| c-2 | t-1 (lock path independent of workdir/project), t-3 (real-flock test uses two distinct Targets/workdirs against one lock file) |
| c-3 | t-3 (WaitLine + once/heartbeat), t-5 (routed to terminal), t-6 (holder fields stamped), t-7 (status/results), t-9 (docs) |
| c-4 | t-1 (no rm text; stale record ≠ held), t-2 (lock inside the setsid job; group kill releases), t-3 (stdin-bound session; SIGKILL test), t-5 (release in Close, none needed on death), t-8 (suite hold in same session) |
| c-5 | t-4, t-9 |
| c-6 | t-3 (Alongside outcome), t-8, t-9 |
| c-7 | t-1 (the single hazard-owning task: O_CREAT-free open, guarded create + chmod 666, nocreat record write) |

Locked decisions → where enforced: lock_mechanism t-1; holder_record t-1/t-3 (same session reads it); lock_granularity t-5; attached_busy_policy t-3/t-5/t-6; suite_participation t-8; pool_busy_is_not_a_skip — no task touches remote_pool.go (host selection unchanged; the lock is taken on the chosen host by t-5/t-8); detached_waiting_state t-2/t-7; no_bypass_flag t-5 (anonymous remote run refused) + t-6 (no flag added but --no-wait).

## Judgment calls

- **Live ssh hold session over inline-per-ssh-call locking for the attached leg.** Rejected: taking the flock inside each per-package `ScriptAll` (released between packages — violates lock_granularity) and a pidfile-style "holder token" (re-derives liveness — violates lock_mechanism's why). The session dies with the local process, which is the only way c-4 and "held across every package" coexist.
- **Holder record read from the hold session's own stdout, not a second status ssh.** One round trip, and the waiter names whoever actually blocked it rather than whoever held the file a second later. LockStatusScript still exists for doctor and for detached status, which have no session.
- **Holder identity + wait policy on `remote.Target` (`Target.Lock`)** rather than new fields on three adapters + newLauncher. Same reasoning Target.Env documents; and it lets newLauncher REFUSE an anonymous remote target, turning no_bypass into a tested invariant across gremlins/stryker/stryker-net at once. Cost: ~9 test target() helpers gain a RunID.
- **The launcher acquires BEFORE the rsync push, not just before the tool.** c-1 only demands the tool wait, but a push onto a workdir another leg from the same repo is measuring would perturb it; putting the push inside the hold costs nothing. The detached path cannot do the same (its push is local-time, its lock host-time) — that pre-existing hazard is noted, not fixed here.
- **Initial detached state is always `scheduled`** (was `running` for an immediate dispatch). Required by detached_waiting_state with no new state word; the cost is one existing test assertion flipped (t-2) and dropping collect's `rec.Scheduled() &&` guard (t-7).
- **Wait wording lives in `internal/remote` (`WaitLine`)** so the terminal (launcher), `dross test` and `verify status` print the same holder sentence; rejected three copies in cmd/mutation.
- **`dross test` holds through one session across all lanes** rather than locking inside each lane's ssh script. Per-lane locking would queue lane 2 behind a mutation leg that queued behind lane 1 — exactly the gate-held-for-a-leg c-6 forbids; the cap would rescue it but only after 10m.
- **Missing `flock` at run time is surfaced by the hold (ErrLockTool), not by adding flock to verify's preflight.** verify's preflight passes nil tools today and a run-time refusal names `dross doctor`; doctor (t-4) is where the prerequisite is probed, as c-5 says. `dross test` maps it to exitToolchainMissing (8), an existing code with the right meaning.
- **Real-flock tests skip on hosts without `flock(1)` (macOS).** They run on the Linux remote `dross test` gate, which is the gate this repo actually uses. Without them c-1/c-4 would be text pins only; with them the exclusion and the death-release are observed.
- **Exit 15 for `--no-wait` refusal** continues verify's 10–14 results band rather than reusing `dross test`'s 1–8 taxonomy; attached_busy_policy asks for "its own exit code".
- **Per-adapter-Run acquisition.** A phase with a Go leg and a TS leg acquires twice, sequentially; a waiter may interleave between legs. Each leg's numbers are still measured whole, which is what lock_granularity protects; a phase-wide hold would need the lock above the adapter loop where no seam exists today.
