# Synthesis — remote-host-mutex (cold judge)

Judge read: spec.toml, project.toml, rules.toml, _brief.md, risk.md, mvp.md,
verification.md. Every file path below was checked under `internal/`; the three
paths marked **(new)** — `internal/remote/lock.go`, `internal/remote/hold.go`,
`internal/cmd/verify_lock_test.go`, `internal/mutation/launcher_lock_test.go`,
`internal/cmd/test_wait_test.go` — exist in no draft's codebase and are new in
every draft that names them. Codebase facts I established that the drafts did
not all account for:

- `dross survivor drain` (`internal/cmd/survivor_drain.go`) is a SECOND
  attached remote-mutation entry point: `resolveMutationTuning` →
  `mt.gremlins(...)` → `mutation.Launcher`. A hold placed in `verify.go`
  (risk t-5, mvp t-2) does not cover it; a hold in the launcher (verification
  t-5) does. Neither risk nor mvp mentions the drain.
- `remote.writePreamble` emits `cd <Workdir>`. A hold taken BEFORE the first
  rsync (every draft's ordering) runs on a host whose workdir may not exist
  yet, so the hold script must not compose the preamble. Only mvp pins this
  (`TestHoldScriptDoesNotCd`); verification's `HoldScript = preamble +
  LockPrelude` would fail with `cd: no such directory` on a fresh host.
- `dross verify` has a flag allowlist test: `TestScopingHasNoOptOut` in
  `internal/cmd/verify_scoping_test.go` (verification calls it
  `TestVerifyFlagAllowlist` — wrong name, right file, real constraint). Adding
  `--no-wait` without touching it fails the suite; risk and mvp both miss it.
  `dross test` has no such allowlist.
- `DetachScript` today writes initial state `running` for an immediate
  dispatch and `scheduled` only with `--at`; `collectDetachedFrom` maps
  `scheduled` to exit 10 only when `rec.Scheduled()` (risk's F12 is real).
- `TestAuditScansRemotePackage` covers `internal/remote/`; a new
  `exec.Command` in `hold.go` needs an accepted-with-reason audit entry
  (verification's claim holds).
- `remoteMutationTools` (doctor.go:1427) feeds `remoteProbeTools`, which feeds
  doctor, `remote bootstrap` and the pool probe. `bootstrapRecipes` fails
  closed ("no install recipe for %q") on an unknown tool, so `flock` needs a
  deliberate refusal recipe or bootstrap prints a generic one.

## Scores

Scale 1–5.

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| risk | 4 — 7/7 with a failure-mode owner per criterion; but the verify-level hold leaves `survivor drain` unlocked (c-1 gap in practice) | 5 — the sharpest text pins in the panel: `-E 75` busy detection, `-ef /dev/fd/9` inode check, lease timings, recorded call orders, per-state exit codes | 4 — t-1 is one file but ~15 contracts (large); t-4 spans 4 files/2 layers (fine) | 5 — strict wave deps; no two wave-mates edit the same file except t-5/t-6 on verify.go |
| mvp | 3 — 7/7 nominally; misses the drain, the flag allowlist, the vanished-laptop hold (c-4 attached), and locks `dross test` per lane after the sync (F14) | 4 — named tests, imaginable assertions; several rest on script text alone with no Go-side or real-flock proof | 2 — t-1 folds builders + parsers + hold session + status into one task; t-3 spans remote+cmd (2 layers, 4 files); docs folded into feature tasks | 4 — 2 waves are correct, but t-2/t-3 both edit verify.go and t-2/t-4 both edit README concurrently |
| verification | 5 — 7/7 plus a locked-decision → enforcing-task map; launcher-level hold covers every remote caller and turns no-bypass into a tested invariant | 4 — named tests with fakes and real-flock proofs, but one misnamed test, a `cd` bug in HoldScript, and no lease | 4 — 9 tasks, 2–3 files each; t-1 adds a Target field, acceptable | 4 — t-7 depends on t-4's doctor seam for a status feature (awkward coupling); t-6/t-7 both edit verify.go in wave 3 |

**Skeleton: verification.** Its structure is the only one whose attached hold
covers every remote-mutation caller (verified: the survivor drain), it puts the
holder identity on `remote.Target` so anonymous remote runs can be refused at
`newLauncher`, and it is the only draft that catches the verify flag allowlist
and the subprocess-argv audit. Its contracts are weaker than risk's in the two
places that matter most (the lock fragment and the hold session), so those two
tasks are grafted almost wholesale from risk, with mvp's no-`cd` pin.

## Merged plan

```
Phase remote-host-mutex — 9 tasks across 4 waves

Wave 1
  t-1  Lock script builders, parsers, Target.Lock            [verification+risk+mvp]
       files:    internal/remote/lock.go (new), internal/remote/lock_test.go (new),
                 internal/remote/remote.go
       covers:   c-2, c-7 (text halves of c-1, c-4)
       description:
                 `HostLockPath = "/tmp/dross-host.lock"`, `LockTool = "flock"`,
                 `Holder{Project, Phase, RunID, PID, User, Since}`, `WaitPolicy{Forever bool,
                 Max time.Duration}` (zero = no wait), `LockSpec{Holder, Wait}`, and a
                 `Lock LockSpec` field on `Target` [verification]. `LockPrelude(spec)` renders
                 the ONLY text that ever opens, creates, chmods or writes the lock path: a
                 `command -v flock` gate emitting `lock=noflock` [risk], a guarded
                 create + `chmod 0666` [all], `exec 9<PATH` read-only [all], `flock ... -E 75 9`
                 per policy [risk], an `-ef /dev/fd/9` inode check with 3 bounded re-opens
                 [risk], the k=v holder record written via `dd of=PATH conv=nocreat
                 status=none` [all], and `lock=waiting|acquired|busy` + `holder.<k>=<v>`
                 protocol lines [risk]. No preamble, no `cd` [mvp]. `LockStatusScript` +
                 `ParseLockStatus` (tool presence, held/free by `flock -n` probe, holder)
                 [risk+verification]; `ParseHolder`. Validate refuses a holder field with a
                 newline [risk+verification].
       contract: TestLockPreludeOpensWithoutCreate — the prelude's only open of the path is
                   `exec 9<'/tmp/dross-host.lock'`; a `flock <path>` form, or any `>`, `>>`,
                   `<>` redirection onto the path outside the create guard, fails (the
                   protected_regular EACCES for user B). [all three]
                 TestLockPreludeCreatesWorldWritable — the create is guarded by
                   `[ -e PATH ] ||` and followed on the same line by `chmod 0666 PATH`;
                   dropping either clause fails. [all three]
                 TestLockPreludeWritesRecordWithoutCreate — the record is piped into
                   `dd of=PATH conv=nocreat status=none`; a `printf … > PATH` form fails;
                   for a bounded policy the write sits INSIDE the acquired branch — a write
                   reachable on the expired path (which would clobber the real holder's
                   record) fails. [all three; mvp's inside-branch pin]
                 TestLockPreludeWaitForms — Forever → `flock -x -E 75 9`; {Max:10m} →
                   `flock -w 600 -E 75 9`; zero → `flock -n -E 75 9`; none passes the
                   path to flock; busy is detected by `$? -eq 75` only, so a flock usage
                   error (exit 1) never reads as busy. [risk's -E 75 over verification's forms]
                 TestLockPreludeChecksTheInode — after acquisition the text contains
                   `[ PATH -ef /dev/fd/9 ]` with a bounded (3) re-open loop that ends in
                   `lock=error replaced`; removing the check fails. [risk F3]
                 TestLockPreludeGatesOnFlock — the fragment begins with `command -v flock`
                   and emits `lock=noflock` (exit 0, not a shell error) before any
                   redirection appears. [risk F4]
                 TestLockPreludeNeverReleasesByHand — no `rm`, `unlink`, `truncate -s0`,
                   `flock -u` or `9<&-` on the lock path anywhere in the fragment — release
                   is fd lifetime only. [risk+verification, c-4 as text]
                 TestLockPreludeQuotesTheHolder — record lines are exactly `project=`,
                   `phase=`, `run=`, `pid=$$`, `user=$(id -un)`, `since=$(date +%s)` in that
                   order, every literal through shellQuote — a project named `a'b` renders
                   `'a'\''b'`. [verification order + risk's user/since fields]
                 TestHolderNewlineRefused — a Holder whose Project/Phase/RunID contains "\n"
                   is refused with ErrUnsafeTarget and no text is built. [risk+verification]
                 TestLockPathIgnoresWorkdir — Target{Workdir:/srv/a} and Target{Workdir:/srv/b}
                   with different Holder.Project emit byte-identical lock-open and
                   record-write lines; the workdir string never appears in the lock path
                   (c-2). [risk+verification]
                 TestLockPreludeHasNoCd — the fragment contains no `cd ` and no exported
                   Env; the lock is independent of a workdir that may not exist yet. [mvp]
                 TestParseHolder — `ParseHolder("")` returns a zero Holder and ok=false (not
                   an error); a record missing `run=` or with a non-integer pid is an error;
                   a full record round-trips with Since as host-epoch time.Unix. [risk+mvp]
                 TestLockStatusScriptProbesNeverReads — LockStatusScript uses `command -v
                   flock` for the tool line and `flock -n -E 75 9` for the busy probe,
                   prints `lock=free` on success and `lock=busy` + `holder.*` lines only on
                   75, and never creates the file (no `chmod`, no `: >` in its text).
                   [risk+verification F5]
                 TestParseLockStatus — `tool=no` → ToolMissing; `lock=free` + a stale record
                   → Held=false with an EMPTY Holder (a dead holder's record is never
                   reported busy — c-4); `lock=busy` + record → Held=true and Holder
                   round-trips; busy with an empty record is distinguished from busy with a
                   holder; output with no `tool=`/`lock=` line is an error, not a free
                   lock. [risk+verification]
                 TestRealFlockSerializesAndReleasesOnKill (`//go:build linux`, t.Skip when
                   exec.LookPath("flock") fails) — the REAL fragment with HostLockPath
                   overridden to a t.TempDir file, two `bash -s` children: the second
                   reports `lock=waiting` while the first is up and `lock=acquired` within
                   1s of the first being SIGKILLed. Runs on the remote Linux `dross test`
                   gate. [risk]

Wave 2 (depends t-1)
  t-2  DetachScript locks; StatusScript reports the holder      [verification+risk+mvp]
       files:    internal/remote/remote.go, internal/remote/remote_test.go
       covers:   c-1 (detached), c-3 (detached, transport half), c-4 (detached)
       description:
                 `DetachScript` composes `LockPrelude(t.Lock, Forever)` into the INNER script
                 after the --at sleep and before `printf '%s' running`; the outer initial
                 state is ALWAYS `scheduled` (a run has not started until it holds the host)
                 [verification+mvp]; refuses an empty `t.Lock.Holder.RunID` with
                 ErrUnsafeTarget [verification]. `StatusScript` appends LockStatusScript's
                 probe lines (`lock=`, `holder.<k>=`) after the `state`/`exit`/`pid` lines;
                 `RunStatus` gains `Lock LockStatus`; `ParseStatus` fills it and output with
                 no lock lines parses exactly as before [risk's labelled-line extension,
                 verification's probe-not-cat].
       contract: TestDetachScriptLocksBeforeItRuns — in the inner `bash -c` text, index of
                   `flock -x` > index of `sleep $((__t - __n))` (when scheduled), < index of
                   `printf '%s' running`, < index of the first argv word; reorder any and it
                   fails (F10/F11: a run that locks then sleeps until --at holds the host
                   idle; `running` before the flock makes status lie). [all three]
                 TestDetachLockIsHeldByTheDetachedJob — the prelude sits INSIDE the
                   single-quoted inner string (after `setsid nohup bash -c '`, before the
                   closing quote), never in the outer chain (an outer lock is released when
                   ssh returns). [verification]
                 TestDetachedRunStartsScheduled — a zero notBefore still writes
                   `printf '%s' 'scheduled'` as the outer initial state; the old `'running'`
                   assertion in TestDetachScriptEmitsNoSleepWithoutASchedule is flipped.
                   [verification+mvp]
                 TestDetachScriptKeepsFdNineOpen — the inner text has no `9<&-` before the
                   tool argv and no unlock after it; the tool runs with fd 9 inherited. [risk]
                 TestDetachScriptRefusesAnonymousHolder — empty Lock.Holder.RunID →
                   ErrUnsafeTarget (c-3 needs a name). [verification]
                 TestStatusScriptProbesTheLock — StatusScript's text contains LockStatusScript's
                   `flock -n -E 75 9` probe and no bare `cat '/tmp/dross-host.lock'`;
                   ParseStatus of `dir=yes\nstate=scheduled\nlock=busy\nholder.project=a\n
                   holder.phase=b\nholder.run=c\nholder.pid=1\nholder.since=1700000000`
                   yields Lock.Held=true and Holder{a,b,c,1,…}; `lock=free` yields
                   Held=false with an empty Holder. [risk lines + verification probe]
                 TestParseStatusWithoutLockLinesIsUnchanged — every existing ParseStatus
                   fixture parses byte-for-byte as before; `holder.since=abc` is a parse
                   error naming the key. [risk]
                 Existing quoting / exit-code / backgrounds-only-the-job tests still pass on
                   the parts they pin. [verification]

  t-3  Hold session: Acquire, Release, lease, holder events     [risk+verification+mvp]
       files:    internal/remote/hold.go (new), internal/remote/hold_test.go (new),
                 internal/cmd/subprocargs_audit_test.go
       covers:   c-1 (attached), c-3 (attached wording), c-4 (attached)
       description:
                 `HoldScript(t)` = `LockPrelude(t.Lock)` (no preamble, no `cd`) followed by a
                 keepalive loop `while read -t 120 -r _; do :; done` [risk lease]. `Acquire(t,
                 HoldEvents{Log, HeartbeatEvery})` spawns `ssh <host> bash -s` through a
                 `holdCommandFn` seam, writes the script to stdin, KEEPS stdin open, and a
                 goroutine writes one newline every `holdKeepalive` (30s, var) [risk].
                 Reads protocol lines: `lock=waiting` + `holder.*` → one `WaitLine` to Log
                 then heartbeats; `lock=acquired` → `*Hold{Outcome: Held}`; `lock=busy`
                 under zero wait → `ErrHostBusy` (with Holder) and no process left; under
                 bounded wait → `Outcome: Alongside` with Other filled, err nil;
                 `lock=noflock` → `ErrLockTool` naming flock and `dross doctor`; exit before
                 any `lock=` line → ErrTransport [risk+verification]. `Lost()` reports the
                 session exiting for any reason other than Release [risk]. `Release()` closes
                 stdin, stops the writer, then waits [all]. `WaitLine(h)` renders "waiting on
                 <project>/<phase> run <run> (pid N) since <RFC3339>" [verification].
                 Audit entry `hold.go:argv[…]` accepted with reason [verification].
       contract: TestHoldSessionDiesWithItsStdin — HoldScript contains no `setsid`, no
                   `nohup`, no `cd `, and ends in the `read -t 120` loop; a fake `bash -c`
                   stand-in that reads stdin exits within 1s of Release() and NOT before;
                   a Hold whose local process the test kills has its stand-in exit on EOF
                   with no Release call (c-4). [risk+verification; mvp's no-cd]
                 TestVanishedLaptopFreesWithinTheLease — with holdKeepalive=10ms and the
                   stand-in's `read -t` set to 1s, stopping the writer makes the stand-in
                   exit; keeping it running keeps the stand-in alive (F8: a suspended laptop
                   frees the host in ≤ lease, not the ~2h TCP keepalive). [risk]
                 TestReleaseClosesStdinThenWaits — Release closes the pipe BEFORE Wait; a
                   fake running `bash -c 'cat >/dev/null'` returns from Release within 1s
                   (a Release that Waited first would hang). [verification]
                 TestAcquirePrintsTheHolderOnce — fake prints `lock=waiting` + holder lines
                   for {dross, p, r-1, 7, 1700000000}, sleeps 200ms, prints `lock=acquired`,
                   then reads stdin: Acquire returns Held, Other.RunID=="r-1", and Log has
                   exactly one line `waiting on dross/p run r-1 (pid 7) since
                   2023-11-14T22:13:20Z`. [verification wording, risk protocol]
                 TestAcquireHeartbeatsWhileWaiting — same fake, 300ms, HeartbeatEvery=50ms:
                   ≥3 `still waiting` lines each naming the host, elapsed time and r-1, none
                   after `lock=acquired`. [risk+verification]
                 TestAcquireEmptyRecordIsNotBlank — `lock=waiting` with no holder lines
                   yields a Progress line saying the holder is not yet recorded, never a
                   panic or a blank name. [risk F5]
                 TestAcquireExpiredRunsAlongside — fake prints waiting, record, `lock=busy`:
                   under WaitPolicy{Max:1s} → Outcome Alongside, Other filled, err nil;
                   under zero policy the SAME output → ErrHostBusy whose message names
                   dross/p run r-1, and no process is left running. [risk+verification]
                 TestMissingFlockIsNamed — fake prints `lock=noflock`: ErrLockTool, message
                   contains "flock" and "dross doctor". [all three]
                 TestEmptyAnswerIsNotAnAcquisition — fake prints nothing and exits 0 →
                   error wrapping ErrRemoteCommand, never Held; fake exits 255 before any
                   `lock=` line → ErrTransport, not ErrHostBusy. [verification+risk]
                 TestLostFlipsWhenTheSessionDies — a stand-in that exits early flips
                   Lost() to true; Release() then returns the exit as an error naming the
                   host. [risk F9]
                 TestHoldGoesOverStdin — holdCommandFn receives argv exactly SSHArgs(t)
                   (`ssh <host> bash -s`); the script and holder record never appear in
                   argv (must not show in the host's `ps`). [verification]
                 TestTwoHoldsSerializeOnARealFlock (t.Skip without flock on PATH) —
                   holdCommandFn faked to local `bash -s`, HostLockPath overridden to a
                   temp file, two Acquires with Forever from two Targets with different
                   Workdirs: the second's Log gets a `waiting on` line naming the first's
                   RunID; first.Release() → second returns Held within 2s (c-1, c-2 proven).
                   [verification]
                 TestAKilledHolderReleasesWithNoCleanup — same setup, first holder's process
                   SIGKILLed: second returns Held within 2s and the lock file still holds
                   the first's record (stale content, no stale lock — c-4 proven).
                   [verification]
                 subprocargs audit: `hold.go:argv[…]` has an accepted-with-reason entry;
                   without it TestAuditScansRemotePackage's scan fails closed on the new
                   spawn. [verification]

  t-4  Doctor probes flock and names the holder                   [risk+verification+mvp]
       files:    internal/cmd/doctor.go, internal/cmd/doctor_remote_test.go,
                 internal/cmd/remote_bootstrap.go, internal/cmd/remote_bootstrap_test.go
       covers:   c-5
       description:
                 `remoteProbeTools` appends `flock` after the adapter tools and before lane
                 tools, attributed to neither needBy nor laneBy [risk+verification].
                 `checkRemoteMutation` prints a dedicated `✗ flock is not installed on <host>
                 — the host lock needs it; mutation legs refuse until it is (util-linux)`
                 and counts one issue. With flock present it runs a new `remoteLockStatusFn`
                 seam (LockStatusScript → ExecScript → ParseLockStatus) and prints `✓ host
                 lock free` or `ℹ host busy: held by <project>/<phase> run <id> (user <u>,
                 pid <n>) since <RFC3339> (<age>)` — advisory, not an issue.
                 `bootstrapRecipes["flock"]` is a refusal naming util-linux and the host
                 package manager [risk+mvp].
       contract: TestDoctorProbesForFlock — the tool list handed to remoteProbeFn contains
                   "flock" exactly once, after the adapter tools; update
                   TestRemoteProbeToolsWithNoLanesIsUnchanged's pinned list to
                   `[gremlins flock]`, needBy/laneBy unchanged. [risk+verification]
                 TestDoctorReportsMissingFlockAsHostTool — remoteProbeFn faked to
                   Missing=["flock"]: output contains "flock is not installed on helicon"
                   and "host lock", issues rises by exactly one, and the generic
                   "the  adapter needs it there" line does NOT appear; Missing=["gremlins"]
                   output is unchanged. [all three]
                 TestDoctorNamesTheCurrentHolder — healthy probe + remoteLockStatusFn
                   returning Held with Holder{dross, remote-host-mutex, r-20260921-101500,
                   4242, since}: output contains "dross/remote-host-mutex run
                   r-20260921-101500" and "pid 4242" and the RFC3339 since; issue count
                   unchanged (busy is not a fault). [all three]
                 TestDoctorFreeLockIsOneLine — fake returns free: exactly one lock line,
                   containing "host lock free". [mvp+verification]
                 TestDoctorSkipsLockStatusWithoutFlock — with flock in Missing the seam is
                   NOT called (a counting fake asserts zero calls). [risk+verification]
                 TestDoctorNeverReadsTheRecordUnprobed — the seam receives
                   LockStatusScript's text (contains `flock -n`), never a bare `cat` of
                   the lock path. [risk F5]
                 TestDoctorRemoteUngrantedIsAdvisory — unchanged: no lock line at all for
                   an ungranted repo. [verification]
                 TestBootstrapRefusesToInstallFlock — planRemoteBootstrap with flock
                   missing yields a step with Refusal containing "util-linux" and nil
                   Argv (never `go install`); with flock present the step is Present.
                   [risk+mvp]

Wave 3 (t-5, t-6, t-8 depend on t-3; t-7 depends on t-2)
  t-5  Launcher holds the host once per run                        [verification, +risk F9]
       files:    internal/mutation/launcher.go, internal/mutation/launcher_test.go,
                 internal/mutation/launcher_lock_test.go (new)
       covers:   c-1 (attached), c-3 (attached, routing), c-4 (attached)
       description:
                 New `launcherHold` seam (= remote.Acquire) and `launcherLog` (= os.Stderr).
                 `ensureHeld()` runs at the top of ensurePushed — before the df probe and
                 the rsync — and stores the *Hold; Close releases it after
                 removeRemoteScratch. newLauncher refuses a remote target whose
                 Lock.Holder.RunID is empty. Mutation legs use Forever/zero only, so
                 Alongside is unreachable here; ErrHostBusy propagates unchanged for verify
                 to map [verification]. After each package's tool call and in Close, a
                 hold whose Lost() is true makes Run return an error naming the host and
                 "lock lost" — a perturbed measurement is never recorded [risk F9].
       contract: TestRemoteRunHoldsOnceAcrossEveryPackage — with the recorder plus a fake
                   launcherHold tagging "hold"/"release" into the same recording, a
                   two-package gremlins remote run records exactly
                   [hold push rm run fetch rm run fetch release]; a second `hold` anywhere,
                   or `push` before `hold`, fails (locked lock_granularity; the push is
                   inside the hold so a sync cannot land on a tree another leg is
                   measuring). [verification]
                 TestCloseReleasesIdempotently — Close on a launcher whose hold was never
                   taken (local run) calls Release zero times; Close twice calls Release
                   once. [verification]
                 TestBusyRefusalSpawnsNothing — fake launcherHold returning ErrHostBusy:
                   nothing spawns through launcherCommand (no push, no rm) and the adapter's
                   Run error satisfies errors.Is(err, remote.ErrHostBusy). [verification]
                 TestUnlockableHostIsNeverMeasuredOn — fake returning ErrLockTool: no
                   push, no rm, no run; the error names the host and `dross doctor`.
                   [risk F4 policy at launcher level]
                 TestLauncherRefusesAnAnonymousRemoteRun — newLauncher(gremlins, "",
                   &Target{Host, Workdir, Lock: zero}, …) errors naming "holder"; the same
                   target with Lock.Holder.RunID="r-1" is accepted; the existing launcher
                   tests' target() helper gains a RunID (no-bypass across all three
                   adapters at once). [verification]
                 TestLauncherRoutesWaitLinesToItsLog — launcherHold is invoked with the SAME
                   Target the tool scripts use except Env, and with HoldEvents.Log ==
                   launcherLog; a fake Log buffer receives the Waiting line the fake writes
                   (c-3 attached reaches the terminal). [verification]
                 TestLostHoldRefusesToRecord — a fake whose Lost() flips after package 1:
                   Run returns an error naming the host and "lock lost", package 2 never
                   runs, and the Release error (if any) is included, not swallowed.
                   [risk F9]
                 Existing TestGremlinsRemoteRunOrderAndArgv and
                   TestGremlinsRemotePushesOnceAndFetchesPerPackage keep their
                   push/rm/run/fetch assertions with hold/release added at the ends.
                   [verification]

  t-6  Verify mints the holder, adds --no-wait, maps busy to 15  [verification+risk+mvp]
       files:    internal/cmd/verify.go, internal/cmd/verify_scoping_test.go,
                 internal/cmd/verify_detach_test.go, internal/cmd/verify_lock_test.go (new)
       covers:   c-1, c-3 (attached exit path), locked attached_busy_policy
       description:
                 resolveMutationTuning fills `Target.Lock.Holder` with {Project:
                 proj.Project.Name, Phase: phaseID (empty for the survivor drain), RunID:
                 newRunID(now)} and Wait Forever; `--no-wait` sets the zero policy.
                 dispatchDetached uses the same holder with the detached run id.
                 ErrHostBusy from RunScoped becomes `&ExitCodeError{Code:
                 exitVerifyHostBusy (15)}` with the holder in the message; nothing is
                 written. `--no-wait --detach` is refused before any ssh, naming both flags
                 (mirrors the reuse-report refusal) [risk]. Allowlist "no-wait" in
                 TestScopingHasNoOptOut [verification, corrected name].
       contract: TestVerifyStampsTheHolderIdentity — after resolveMutationTuning on a
                   granted repo, Target.Lock.Holder.Project == the project.toml name,
                   .Phase == the phase id, .RunID matches `^r-\d{8}-\d{6}$`, Wait.Forever
                   true; any empty field fails (a waiter would print a blank).
                   [verification+risk]
                 TestNoWaitSetsZeroWait — `--no-wait` yields the zero WaitPolicy; without
                   the flag Forever stays true. [verification]
                 TestBusyRefusalExitsFifteenAndWritesNothing — a fake adapter returning
                   fmt.Errorf("…: %w", remote.ErrHostBusy) makes Verify() exit 15 (pinned
                   distinct from every code in test.go and the 10–14 results band) and
                   print holder project, phase, run id and since; no tests.json and no
                   verify.toml are written. [all three]
                 TestNoWaitWithDetachIsRefused — `--no-wait --detach` errors before any
                   detachSpawn/detachSync call, naming both flags. [risk]
                 TestLocalVerifyNeverTouchesTheLock — `--local` and a transport fallback
                   (Target nil) leave Lock zero and never reach launcherHold (zero calls).
                   [all three]
                 TestNoWaitWithoutAHostIsInert — `--no-wait` with no granted host is a
                   local run, exit 0. [verification]
                 TestDetachedHolderIsTheRunID — the detached record's RunID equals the
                   `run=` value inside the DetachScript text detachSpawn received, and the
                   text contains `project=<name>` and `phase=<id>`; a mismatch would make
                   status unable to tell "waiting on me" from "waiting on another".
                   [all three]
                 TestScopingHasNoOptOut passes with "no-wait" added and would fail on any
                   other new flag. [verification]

  t-7  Detached status/results name the holder                     [risk+mvp+verification]
       files:    internal/cmd/verify.go, internal/cmd/verify_status_test.go,
                 internal/cmd/verify_results_test.go
       covers:   c-3 (detached), locked detached_waiting_state
       description:
                 printDetachedStatus and collectDetachedFrom read `st.Lock` (t-2's one
                 round trip — no second probe). When state is `scheduled`, the record is
                 not waiting for a future --at, and the lock is held by a RunID other than
                 this record's, the reason line is `waiting on <project>/<phase> run <run>
                 (pid N) since <t>`; results keeps exit 10 (the `rec.Scheduled() &&` guard
                 is dropped — `scheduled` is now every detached run's initial state).
       contract: TestStatusNamesWhatAScheduledRunWaitsOn — detachStatus faked to
                   {DirExists, State:"scheduled", Lock: held by {proj-b, phase-x, r-other,
                   99, since}}: `verify status` prints `state    scheduled` followed by a
                   line containing "waiting on proj-b/phase-x run r-other (pid 99) since
                   <RFC3339>". [all three]
                 TestResultsWaitingOnAHolderIsScheduledNotRunning — same fake with a record
                   that has NO --at: `verify results` exits exitResultsScheduled (10, not
                   11 "still running") and names proj-b/phase-x run r-other; nothing is
                   written. [all three — risk F12]
                 TestScheduledByOwnRunIDIsNotWaiting — state scheduled, lock held by the
                   record's OWN RunID → no "waiting on" line (racing its own state write).
                   [verification]
                 TestAtScheduledRunKeepsItsReason — a record whose ScheduledFor is in the
                   future prints the existing "scheduled for … (host clock)" line and no
                   holder line (one reason per line). [risk+verification]
                 TestRunningRunPrintsNoHolder — state `running` with lock lines present
                   prints no holder (state wins). [risk]
                 TestScheduledWithFreeLockSaysNotStarted — state scheduled, lock free, no
                   --at: prints `not started yet` and results still exits 10. [risk]
                 TestEmptyRecordIsNamedAsSuch — state scheduled, lock busy, empty holder:
                   prints `waiting on the host lock (holder not yet recorded)`. [risk]
                 TestLockReadErrorKeepsStatusUseful — lock lines absent because the probe
                   failed (ParseStatus tolerates) prints `state    scheduled` and "(could
                   not read the host lock)" rather than failing the listing.
                   [verification]
                 Existing TestResultsOnAScheduledRunSaysScheduled still exits 10. [verification]

  t-8  dross test waits up to a cap                                [verification+risk+mvp]
       files:    internal/cmd/test.go, internal/cmd/test_remote_test.go,
                 internal/cmd/test_wait_test.go (new)
       covers:   c-6, c-4 (suite half)
       description:
                 `--wait <dur>` (default 10m) on `dross test`, threaded as an explicit
                 parameter through runTest / runTestLanes / runOneLane / runLanePrepare /
                 runRemoteLine — no package-level state [mvp]. A new `testHold` seam
                 (= remote.Acquire) is called from runTestRemotely / runTestLanes BEFORE
                 syncTreeTo with Target.Lock = {Holder{Project, Phase:"", RunID:"t-<stamp>"},
                 Wait{Max: wait}} (`--wait 0` → zero policy: `flock -n`, hold if free, run
                 at once otherwise); Alongside prints the holder and continues; Held or
                 Alongside are released after the last lane [verification]. ErrLockTool
                 prints one warning naming the host and `dross remote bootstrap`, then
                 runs the suite unlocked [risk]. `--local` never calls the seam.
                 `inFlightRunWarning` wording changes from "will compete" to "will wait up
                 to <cap> for it, then share the host's cores" [risk].
       contract: TestTestWaitDefaultsToTenMinutes — the default reaches the fake testHold
                   as Lock.Wait.Max == 10m, Forever == false; `--wait 30s` reaches it as
                   30s. [all three]
                 TestWaitFlagValidation — `--wait abc` and a negative value are refused
                   before any spawn, naming the flag. [risk]
                 TestWaitZeroSpawnsAtOnce — `--wait 0` reaches the seam as the zero policy
                   and the rendered HoldScript contains `flock -n` and no `-w`; a fake
                   returning Alongside still spawns the suite immediately. [verification]
                 TestTestNeverWaitsForever — for any --wait value including a huge one the
                   WaitPolicy never has Forever set; the ErrHostBusy refusal path is
                   unreachable from `dross test` by construction. [verification]
                 TestExpiredWaitSpawnsAnywayNamingTheHolder — testHold faked to Alongside
                   with Other={dross, remote-host-mutex, r-1, 4242, since}: the suite's
                   ssh IS spawned, stderr contains ONE line with "waited <dur>", "r-1" and
                   "running alongside", and the exit code is the suite's own (0 on a green
                   fake) — never a new code. [risk+verification]
                 TestTestHoldsAcrossSyncAndSuite — recorded order across seams is
                   [hold, rsync-sync, ssh-suite, release]; on a lanes run with two matched
                   lanes: [hold, sync, ssh, ssh, release] — one hold, not one per lane,
                   and the hold precedes each host's syncTreeTo. [verification+risk F14]
                 TestSuiteFailureStillReleases — a lane returning exitSuiteFailed and a
                   failing spawnRemote both still record Release after the spawn (N
                   releases for N holds). [risk+verification]
                 TestMissingFlockWarnsAndRuns — fake testHold returning ErrLockTool:
                   stderr has one warning naming the host and `dross remote bootstrap`,
                   the suite spawns, exit code is the suite's. [risk]
                 TestInFlightWarningSaysWait — inFlightRunWarning's string contains "will
                   wait up to 10m" and no longer "will compete"; with `--wait 0` the old
                   "will compete" wording is kept. [risk]
                 TestLocalTestTakesNoLock — `dross test --local` calls testHold zero times.
                   [all three]

Wave 4 (depends t-6, t-7, t-8)
  t-9  Document the host lock                                      [verification+risk]
       files:    README.md, ARCHITECTURE.md, assets/prompts/verify.md,
                 internal/cmd/options_docs_test.go
       covers:   c-3, c-4, c-5, c-6 (documentation halves), locked no_bypass_flag
       description:
                 README rows: `dross verify` gains the wait/`--no-wait` sentence and exit 15;
                 `verify status`/`results` gain the "scheduled — waiting on <holder>"
                 reading (still exit 10); `dross test` gains `--wait <dur>` (default 10m,
                 0 = at once) and the alongside line; `dross doctor` Remote gains flock +
                 "host busy"; a sentence that a dead holder releases the lock with nothing
                 to clear. ARCHITECTURE.md: a "Host lock" subsection under the remote
                 section naming /tmp/dross-host.lock, flock on fd 9, the protected_regular
                 open rule, the `conv=nocreat` write, and the hold session's `read -t`
                 lease. verify.md prompt: a `scheduled` result naming a holder is a wait,
                 not a stall; use `--no-wait` only for an unattended probe.
       contract: TestReadmeDocumentsTheHostLock — README contains "/tmp/dross-host.lock",
                   "--no-wait" on the `dross verify` row with exit code `15`, "--wait" with
                   "10m" and "--wait 0" on the `dross test` row, "waiting on", "running
                   alongside", and one of "no stale lock" / "nothing to clear"; each
                   missing string fails by name. [risk+verification]
                 TestResultsRowStillEnumeratesTenToFourteen — the `dross verify results`
                   row's backticked exit codes are exactly 10–14 (the waiting state is
                   documented as 10 with a holder reason, not a new code). [risk]
                 TestArchitectureDocumentsTheHostLock — ARCHITECTURE.md contains
                   "/tmp/dross-host.lock", "protected_regular", "conv=nocreat" and
                   "read -t" — the facts a future editor would "simplify" away and break
                   c-7 or c-4. [verification, lease string per D2]
                 TestVerifyPromptTeachesTheDetachedPath extended — verify.md contains
                   "waiting on", "not a failure" and "--no-wait" beside the existing
                   --detach strings. [risk+verification]
                 TestReadmeDocumentsDetachedRuns still passes — no new state word is
                   introduced (locked detached_waiting_state). [verification]
```

### Coverage of the merged plan

| criterion | tasks |
|---|---|
| c-1 | t-1 (text), t-2 (detached), t-3 (session + real-flock proof), t-5 (launcher order, covers verify AND survivor drain), t-6 (holder wiring) |
| c-2 | t-1 (path independent of workdir/project), t-3 (real-flock test over two workdirs) |
| c-3 | t-3 (WaitLine once + heartbeat), t-5 (routed to terminal), t-6 (fields stamped), t-7 (status/results), t-9 |
| c-4 | t-1 (no manual release; stale record ≠ held; inode check), t-2 (lock inside the setsid job), t-3 (stdin-bound session + lease; SIGKILL proof), t-5 (lost hold never recorded), t-8 (suite hold in the same session), t-9 |
| c-5 | t-4, t-9 |
| c-6 | t-3 (Alongside outcome), t-8, t-9 |
| c-7 | t-1 (the single hazard-owning task) |

Locked decisions → enforcing task: lock_mechanism t-1; holder_record t-1/t-3 (waiter reads it from the session), t-2 (status reads it via the probe); lock_granularity t-5; attached_busy_policy t-3/t-5/t-6; suite_participation t-8; pool_busy_is_not_a_skip — no task touches remote_pool.go; detached_waiting_state t-2/t-7; no_bypass_flag t-5 (anonymous remote run refused) + t-6.

## Disagreements

**D1 — Where the attached hold lives (and per-what it is acquired).**
Risk t-5 and mvp t-2 take one `remote.Hold` in `verify.go` before `RunScoped`,
released after `finishVerify` — one acquisition per `dross verify` spanning every
adapter and the push, adapters untouched. Verification t-5 takes it in
`mutation.Launcher.ensurePushed`, one per adapter Run, with `newLauncher`
refusing a remote target that carries no holder. **Default: launcher (verification).**
Why it matters: `dross survivor drain` also runs gremlins on the granted host
through `resolveMutationTuning` → launcher; a verify-level hold leaves it
unlocked and c-1 ("two remote mutation legs … never overlap") silently fails
for the drain-vs-verify collision, which is the one a person is most likely to
cause (drain while a detached verify runs). Cost accepted: a phase with a Go leg
and a TS leg acquires twice, sequentially, and a waiter may slot between the two
legs — each leg's numbers are still measured whole. Consequence carried into
t-1/t-6: holder identity rides on `Target.Lock` (verification) rather than a
separate `*Holder` parameter (risk/mvp), because the launcher already has the
Target and nothing else.

**D2 — Hold-session liveness: EOF-only vs lease.**
Mvp (`read -r _`) and verification (`cat >/dev/null`) end the hold script in a
stdin read, so the lock dies when the pipe EOFs. Risk ends it in a `while read
-t 120` loop and has the local side write a newline every 30s, so a laptop
that vanishes without closing the socket (suspend, cable pull) frees the host
within two minutes instead of the ~2h TCP keepalive. **Default: risk's lease.**
Why it matters: c-4 says a dead holder releases "with no cleanup step"; a
suspended laptop is the commonest way a holder "dies" for this user, and
EOF-only turns that into a two-hour stale hold nobody can clear (no_bypass_flag
forbids the escape). Cost: one goroutine and one more contract; ARCHITECTURE
pins "read -t" instead of "cat >/dev/null".

**D3 — Detached initial state.**
Mvp and verification make the outer initial state `scheduled` for every
detached run and flip one existing assertion. Risk keeps today's initial
(`running` for an immediate dispatch, pinned byte-identical for a nil Holder)
and has the on-wait hook rewrite `scheduled` plus copy the record to
`<runDir>/holder`. **Default: always `scheduled`.** Why it matters: risk's
version has a window where the state file says `running` before the inner
script has even reached the flock (its own F11, moved to the outer layer), and
it needs a second file in runDir with its own lifecycle. The cost of the
default is dropping `rec.Scheduled() &&` in collect and one flipped test.

**D4 — How detached status learns the holder.**
Risk: StatusScript emits `holder.*` from a `<runDir>/holder` copy made at wait
time (one round trip; the copy may be stale). Mvp: StatusScript `cat`s the lock
file plus a `waiting` marker (one round trip; reads the record without a
`flock -n` probe, which risk's F5 and verification both reject as "stale record
≠ held"). Verification: status/results call the doctor's `lockStatusFn`
separately (probe-gated, but a second ssh per status read and a cmd-layer
dependency on the doctor task). **Default: StatusScript composes
LockStatusScript's probe lines** — one round trip (risk/mvp) with the
`flock -n` gate (risk/verification) — and t-7 compares the holder's RunID to
its own (verification's "waiting on me" guard). This is a hybrid no single
draft wrote; it removes t-7's dependency on t-4. Why it matters: a status read
that reports a dead holder's record as "waiting on X" is exactly the stale-lock
impression c-4 forbids, and a second ssh per `verify status` doubles the cost
of the command the user polls.

**D5 — How `dross test` takes the lock.**
Mvp locks INSIDE the suite's own ssh script (`ScriptAllLocked`; the exec'd
suite inherits fd 9 — zero extra sessions), which on the lanes path locks per
lane and only after the rsync. Risk and verification use the hold session
before `syncTreeTo`, one hold across all lanes, released after the last.
**Default: hold session (risk+verification).** Why it matters: per-lane locking
queues lane 2 behind a mutation leg that queued behind lane 1 — the
gate-held-for-a-leg c-6 forbids — and a sync that lands before the lock is
taken can overwrite a tree a mutation leg is measuring (risk F14). Cost: one
extra ssh session per suite run.

**D6 — Missing `flock` on the suite host.**
Risk: warn once (naming `dross remote bootstrap`) and run the suite unlocked.
Verification: fail with exitToolchainMissing (8). Mvp: unspecified (its script
would exit 127 before the exec). **Default: warn and run (risk).** Why it
matters: c-6's premise is that a suite under load is slow, not wrong, and that
a task gate is never held on the host's account; failing the execute loop's
gate on a host-tooling gap unrelated to the suite inverts that. The mutation
leg refuses in the same situation (t-5) — the asymmetry is the spec's own.

**D7 — Docs as a separate wave-4 task vs folded into feature tasks.**
Mvp folds README rows into t-2/t-4 and the ARCHITECTURE paragraph into t-2, with
a small README check in t-2. Risk and verification give docs their own task
after all feature tasks. **Default: separate task (t-9).** Why it matters: the
doc tests live in `options_docs_test.go`, which pins detached-run vocabulary
and the verify prompt together; three feature tasks editing README/ARCHITECTURE
in the same wave would conflict on the same rows, and the results-row exit-code
count (risk) is only checkable once every code is final.

**D8 — `--wait 0` semantics.**
Risk: the seam is not called at all (zero ssh; the suite never holds and is
never named by a waiting mutation leg). Verification: zero WaitPolicy →
`flock -n` — spawn at once either way, but hold the lock if it happens to be
free, so a mutation leg dispatched mid-suite waits on the suite. **Default:
verification's.** Why it matters: the spec says the suite "takes the same lock"
and c-1 is about legs never overlapping a measurement; a `--wait 0` suite that
holds nothing can start a mutation leg's measurement under load with no
warning, whereas holding-if-free costs nothing the suite was not already
paying. Risk's `--wait 0` "keeps the old will-compete wording" contract is kept
because it is about the in-flight warning, not the hold.

Grafts that are NOT disagreements (no draft rejected them; recorded for
traceability): the `-ef /dev/fd/9` inode check and `-E 75` busy detection
(risk → t-1); `Lost()` → refuse to record (risk t-5 → launcher t-5); the no-`cd`
hold script (mvp → t-1/t-3); `--no-wait --detach` refusal (risk → t-6);
`bootstrapRecipes["flock"]` refusal (risk+mvp → t-4); `--wait` threaded as a
parameter (mvp → t-8); `inFlightRunWarning` rewording (risk → t-8);
`TestScopingHasNoOptOut` allowlist update under its real name (verification →
t-6); the subprocargs audit entry (verification → t-3).
