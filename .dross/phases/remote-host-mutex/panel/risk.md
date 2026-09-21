# Risk-lens draft — remote-host-mutex

Lens: every task below owns one cluster of failure modes and is the ONLY task
whose tests pin them. The graph starts from what breaks, not from which file
changes.

Failure-mode inventory that drove the decomposition:

| # | What breaks | Owner |
|---|---|---|
| F1 | `/tmp` + `fs.protected_regular=1`: user B's `flock <path>` / `> path` fails EACCES on user A's file; a plain create is 0644 under umask | t-1 |
| F2 | lock file absent (fresh host, post-reboot tmpfs) — first waiter must create it, 0666 | t-1 |
| F3 | lock file `rm`'d or replaced while held — a new waiter locks a different inode and overlaps | t-1 |
| F4 | `flock` binary absent — script must say so, not fall through unlocked | t-1 (script), t-4 (doctor), t-5/t-7 (policy) |
| F5 | holder record read in the window before the holder wrote it, or a previous holder's record on a free lock | t-1 (empty-record tolerance), t-4 (doctor only reads after `flock -n` fails) |
| F6 | a newline in a holder field corrupts the k=v record | t-1 |
| F7 | attached leg = many short ssh sessions; lock must outlive each and die with the local process. `exec sleep` under no-pty ssh orphans; `bash -s` + `cat` releases on stdin EOF only if the local side holds a pipe open | t-2 |
| F8 | laptop vanishes without closing the socket (suspend, cable) — sshd sees no EOF for ~2h of TCP keepalive | t-2 (lease: remote `read -t`, local keepalive) |
| F9 | holder session dies mid-run (blip) while per-package ssh calls continue — measurement no longer exclusive | t-2 (detects), t-5 (refuses to record) |
| F10 | detached: lock taken BEFORE the `--at` sleep would hold the host all night | t-3 |
| F11 | detached: `running` written before the lock is held → status lies | t-3 |
| F12 | detached: a run with no `--at` that reads `scheduled` on the host is reported as `running` (exit 11) by `verify results` | t-6 |
| F13 | `dross test` hold never times out / `--wait 0` still pays an ssh round-trip / timeout is silent | t-7 |
| F14 | multi-lane run syncs to a host before holding it | t-7 |
| F15 | flag added but undocumented (doc tests) / lock file described as clearable | t-8 |

Plan follows.

```
Phase remote-host-mutex — 8 tasks across 4 waves

Wave 1
  t-1  Build lock script fragments + holder record
       files:    internal/remote/lock.go, internal/remote/lock_test.go
       covers:   c-2, c-4, c-7
       contract: F1: the acquire fragment opens the lock ONLY as `exec 9<'/tmp/dross-host.lock'`
                 and locks the fd (`flock ... 9`); the script text contains no `flock <path>` form,
                 no `>`, `>>` or `<>` redirection onto the lock path, and the record write uses
                 `dd of='/tmp/dross-host.lock' conv=nocreat status=none` — a test greps each
                 forbidden form and fails on any hit.
                 F2: the fragment creates the file only when absent and follows the create with
                 `chmod 0666`; removing either clause fails the test that pins the exact
                 `[ -e ... ] || { : > ... && chmod 0666 ... ; }` guard.
                 F3: after acquisition the fragment checks `[ '/tmp/dross-host.lock' -ef /dev/fd/9 ]`
                 and re-opens on mismatch (bounded, 3 tries) before emitting `lock=error replaced`;
                 dropping the check fails a text pin.
                 F4: the fragment starts with `command -v flock` and emits `lock=noflock` (exit 0,
                 not a shell error) when absent — pinned before any redirection appears.
                 Wait modes render distinctly: WaitForever → `flock -x -E 75 9`; NoWait →
                 `flock -n -E 75 9`; WaitUpTo(90s) → `flock -w 90 -E 75 9`; WaitUpTo(0) refused
                 by the builder (callers skip the hold, never build it). A busy outcome is
                 detected by `$? -eq 75` only, so a `flock` usage error cannot read as busy.
                 c-2: two Targets with different Workdirs and different Holder.Project produce
                 fragments whose lock path bytes are identical and absolute; the workdir string
                 never appears inside the lock path.
                 c-4: the fragment contains no unlock, `rm` or `flock -u` — release is fd
                 lifetime only; a test asserts none of those tokens appear.
                 F5/F6: `ParseHolder("")` returns a zero Holder and ok=false (not an error);
                 `LockScript` with a `\n` in Project/Phase/RunID returns ErrUnsafeTarget and no
                 text. Record lines are exactly `project=`, `phase=`, `run=`, `pid=$$`,
                 `user=$(id -un)`, `since=$(date +%s)` and `ParseHolder` round-trips them.
                 `LockStatusScript` (doctor's probe) does `flock -n -E 75 9`, prints `lock=free`
                 on success and `lock=busy` + `holder.<k>=<v>` lines only on 75, and never
                 creates the file (pin: no `chmod`, no `: >` in its text). `ParseLockStatus`
                 distinguishes free / busy-with-holder / busy-with-empty-record / noflock.
                 Linux-only (`//go:build linux`, skipped when `flock` is not on PATH): the REAL
                 fragment with HostLockPath overridden to a temp file — two `bash -s` children,
                 the second reports `lock=waiting` while the first is up, `lock=acquired` within
                 1s of the first being SIGKILLed. This test runs on the remote Fedora runner.

Wave 2 (depends t-1)
  t-2  Hold session: lock outlives ssh calls, dies with process
       files:    internal/remote/hold.go, internal/remote/hold_test.go
       covers:   c-1, c-3, c-4
       contract: F7: `Hold` spawns `ssh <host> bash -s` with stdin = LockScript text followed by a
                 pipe the Hold KEEPS OPEN; the script's last statement is a `while read -t 120
                 -r _; do :; done` keepalive loop (pinned text) — a fake `bash -c` stand-in
                 that reads stdin exits within 1s of `Release()` and NOT before; a Hold whose
                 process is killed by the test has its stand-in exit on EOF without any
                 Release call.
                 F8: while held, the local side writes one newline to stdin every 30s
                 (injectable interval); with the interval set to 10ms and the stand-in's
                 `read -t` set to 1s, stopping the writer makes the stand-in exit — proving a
                 vanished laptop frees the host within the lease.
                 F9: `Lost()` reports true once the session process exits for any reason other
                 than Release; a stand-in that exits early flips it and `Release()` then returns
                 the exit as an error naming the host.
                 Events: stdout `lock=waiting` + `holder.*` lines call `Progress` ONCE with the
                 parsed Holder (project, phase, run, pid, user, since); with heartbeat set to
                 10ms and the stand-in holding `waiting` for 100ms, at least 3 heartbeat lines
                 mention the host and elapsed time, and exactly one names the holder.
                 `lock=acquired` returns a Hold with Held=true; `lock=busy` under NoWait returns
                 `*BusyError{Host, Holder}` (errors.Is ErrHostBusy) and Held=false with no
                 process left running; `lock=busy` under WaitUpTo returns Held=false, Holder
                 filled, nil error. `lock=noflock` returns ErrLockUnavailable naming the host.
                 An empty holder record produces a Progress line saying the holder is not yet
                 recorded, never a panic or a blank name.
                 A transport failure (stand-in exits 255 before any `lock=` line) returns
                 ErrTransport, not ErrHostBusy.

  t-3  DetachScript acquires lock before running
       files:    internal/remote/remote.go, internal/remote/remote_test.go
       covers:   c-1, c-3, c-4
       contract: `DetachScript` takes a `*Holder`; with one set, the INNER script text (the
                 single-quoted `bash -c` argument) contains the t-1 fragment; with nil, the text
                 is byte-identical to today's (pinned against the existing golden).
                 F10: with a notBefore and a Holder, the `sleep` arithmetic appears BEFORE the
                 first `flock` in the inner text.
                 F11: `printf '%s' running > <state>` appears AFTER `lock=acquired`-equivalent
                 point (after the `flock` and the `-ef` check) — a test indexes both and fails
                 if running is written first.
                 Waiting side effect: the on-wait hook copies the record to `<runDir>/holder`
                 (`cp` of the lock path, no O_CREAT on the LOCK path) and writes `scheduled` to
                 state; on acquisition nothing removes the holder file (state wins).
                 c-4: the tool's argv is exec'd with fd 9 still open — the inner text has no
                 `9<&-` before the tool and no unlock after it.
                 `StatusScript` emits `holder.<k>=<v>` lines from `<runDir>/holder` via
                 `sed 's/^/holder./'` guarded by `2>/dev/null`; `ParseStatus` fills
                 `RunStatus.Holder` and `HasHolder`; output with no holder lines parses as
                 before (existing ParseStatus tests unchanged); a `holder.since=abc` line is a
                 parse error naming the key.

  t-4  Doctor probes flock and names the holder
       files:    internal/cmd/doctor.go, internal/cmd/doctor_remote_test.go,
                 internal/cmd/remote_bootstrap.go, internal/cmd/remote_bootstrap_test.go
       covers:   c-5
       contract: `remoteProbeTools` returns `flock` after the adapter tools and before lane
                 tools, attributed to neither map (needBy/laneBy) — the existing
                 no-lanes-unchanged test is updated to expect `[gremlins flock]`.
                 F4: with the probe fake reporting Missing=[flock], the Remote section prints
                 `✗ flock is not installed on <host> — the host lock needs it; mutation legs
                 refuse until it is (util-linux)` and counts ONE issue; Missing=[gremlins]
                 output is unchanged.
                 With flock present, doctor calls the new `remoteLockStatusFn` seam once; fake
                 `lock=busy` + holder lines → prints `  ℹ host busy: held by <project>/<phase>
                 run <id> (user <u>, pid <n>) since <RFC3339> (<age>)` and counts NO issue;
                 fake `lock=free` → `  ✓ host lock free`. With flock missing the seam is NOT
                 called (a counting fake asserts zero calls).
                 F5: doctor never reads the record without the `flock -n` probe: the seam is
                 fed LockStatusScript output only (pinned by asserting the fake received the
                 t-1 status script text, not a `cat`).
                 Bootstrap: `bootstrapRecipes["flock"]` is a refusal naming util-linux and the
                 host package manager; planRemoteBootstrap on Missing=[flock] yields a step
                 with that Refusal and no Argv (never `go install`).

Wave 3 (depends t-2, t-3)
  t-5  Attached verify holds the lock for the whole leg
       files:    internal/cmd/verify.go, internal/cmd/verify_lock_test.go
       covers:   c-1, c-3, c-4
       contract: New seam `holdHost = remote.Hold`. With a remote Target and a counting fake, the
                 attached path calls it exactly ONCE per `dross verify`, BEFORE the adapters run
                 (a fake adapter records the order) and with WaitForever; the Hold's Release
                 runs after finishVerify (recorded order: adapters, finish, release).
                 The Holder passed carries project name from project.toml, the phase id, a
                 run id from `newRunID`; no field is empty.
                 `--local` and a transport fallback (Target nil) never call the seam (zero
                 calls asserted on both).
                 `--no-wait`: the seam is called with NoWait; a fake returning `*BusyError`
                 makes the command exit `exitVerifyHostBusy` (15 — pinned distinct from every
                 code in test.go and the results band) and the message names holder project,
                 phase, run id and since; nothing is written to the phase dir.
                 `--no-wait --detach` is refused before any ssh with a message naming both
                 flags (mirrors the reuse-report refusal).
                 F9: a fake whose `Lost()` flips during the adapter run makes verify return an
                 error naming the host and "lock lost", and neither tests.json nor verify.toml
                 is written; the Release error (if any) is included, not swallowed.
                 F4 policy: a fake returning ErrLockUnavailable makes the attached remote leg
                 refuse with the `dross remote bootstrap` and `--local` remedies; no adapter
                 runs (count 0) — an unlockable host is never measured on.

  t-6  Detached dispatch passes lock; status/results name holder
       files:    internal/cmd/verify.go, internal/cmd/verify_status_test.go,
                 internal/cmd/verify_results_test.go, internal/cmd/verify_detach_test.go
       covers:   c-1, c-3
       contract: `dispatchDetached` builds DetachScript with a non-nil Holder whose RunID equals
                 the recorded `run_id` and whose Phase equals the record's phase — the
                 detachSpawn fake captures the script and a test asserts the t-1 fragment and
                 `run=<that id>` are in it.
                 F12: `collectDetachedFrom` on a record with NO `--at` whose host status is
                 state=scheduled + holder lines exits `exitResultsScheduled` (10, not 11) and
                 the message reads `waiting on the host lock held by <project>/<phase> run <id>
                 since <t>`; the same host status for a record WITH `--at` and no holder keeps
                 today's `scheduled for <t>` wording; state=scheduled with neither prints
                 `not started yet` and still exits 10.
                 `verify status` prints `  state    scheduled — waiting on <project>/<phase> run
                 <id> (held since <t>)` when the fake status carries a holder, and the plain
                 `scheduled`/`running` lines otherwise; a `running` status that still carries
                 holder lines prints NO holder (state wins — pins the t-3 decision not to
                 delete the holder file).
                 An empty holder record (holder lines absent but state=scheduled, record not
                 `--at`) prints `waiting on the host lock (holder not yet recorded)`.

  t-7  dross test: bounded wait, then spawn naming holder
       files:    internal/cmd/test.go, internal/cmd/test_lock_test.go,
                 internal/cmd/test_remote_test.go
       covers:   c-6
       contract: `--wait <dur>` parses a Go duration, default `10m`; `--wait abc` is refused
                 before any spawn naming the flag; a negative value is refused.
                 F13: whole-suite remote run calls `holdHost` once with WaitUpTo(10m) by default
                 and WaitUpTo(90s) under `--wait 90s`; with `--wait 0` the seam is NOT called
                 (zero calls) and the sync + run proceed at once.
                 Timeout: a fake returning Held=false with a Holder makes the run print ONE
                 line `waited <dur> for <host>: still held by <project>/<phase> run <id> since
                 <t> — spawning alongside it; this suite will share the host's cores` to
                 stderr and then spawn the suite (spawnRemote called); exit code is the
                 suite's, never a new code.
                 Held=true: Release is called after the suite's ssh returns (order recorded),
                 so a mutation leg dispatched mid-suite waits until the suite exits.
                 F14: on the lane path, for each host in `plannedHosts(verdicts)` the hold
                 precedes that host's `syncTreeTo` (order recorded per host), and every hold
                 taken is released at the end even when a lane fails (a lane returning
                 exitSuiteFailed still yields N releases for N holds).
                 F4: ErrLockUnavailable (no flock) prints one warning naming the host and
                 `dross remote bootstrap`, then runs the suite unlocked — the suite does not
                 refuse, in contrast to t-5.
                 `inFlightRunWarning` wording changes from "will compete" to "will wait up to
                 <cap> for it, then share the host's cores"; the existing test's expected string
                 is updated, and a `--wait 0` run keeps the old "will compete" wording.
                 `--local` never calls the seam.

Wave 4 (depends t-5, t-6, t-7)
  t-8  Document the host lock and its flags
       files:    README.md, ARCHITECTURE.md, assets/prompts/verify.md,
                 internal/cmd/readme_doc_test.go
       covers:   c-3, c-4, c-6
       contract: F15: `TestReadmeDocumentsHostLock` fails unless README names
                 `/tmp/dross-host.lock`, the `--no-wait` flag on the `dross verify` row with
                 exit code `15`, the `--wait <dur>` flag and its `10m` default and `--wait 0`
                 on the `dross test` row, and the sentence that a dead holder releases the
                 lock with nothing to clear (the string "no stale lock" or "nothing to clear").
                 The `dross verify results` row's exit-code list still enumerates exactly
                 10–14 (a test counts the backticked codes on that row) — the waiting state
                 is documented as `10` with a holder reason, not a new code.
                 ARCHITECTURE.md's remote section gains a "Host lock" paragraph naming the
                 kernel-flock release property, the protected_regular open rule, and the lease;
                 `assets/prompts/verify.md` tells the agent that a `scheduled` status naming a
                 holder is a wait, not a stall, and to use `--no-wait` only for an unattended
                 probe — `verify_prompt_test.go`'s existing assertions still pass and a new one
                 pins the `--no-wait` mention.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (legs never overlap, attached + detached) | t-2, t-3, t-5, t-6 |
| c-2 (spans projects + workdirs) | t-1 |
| c-3 (waiter names the holder: terminal / status / results) | t-2, t-3, t-6, t-8 |
| c-4 (dead holder releases, no stale state) | t-1, t-2, t-3, t-5, t-8 |
| c-5 (doctor: missing flock, current holder) | t-4 |
| c-6 (`dross test` bounded wait, `--wait`, spawns anyway) | t-7, t-8 |
| c-7 (across unix users) | t-1 |

7/7 criteria covered.

## Judgment calls

- **Lock held at the verify level, not inside the Launcher.** Chose one `remote.Hold` taken in `verify.go` before `RunScoped` and released after `finishVerify`; rejected threading a Holder through `newLauncher` and the three adapters. One acquisition per verify (spans every adapter leg AND the rsync push, so the push cannot overwrite a tree another leg is measuring), four fewer files, and the adapters stay byte-identical for local runs.
- **Holder session = `ssh host bash -s` with stdin held open, ending in a `read -t 120` keepalive loop.** Rejected `exec sleep infinity` (no pty → sshd never signals it, the lock outlives the laptop) and `exec cat` alone (a suspended laptop holds the host for the ~2h TCP keepalive). The lease costs one goroutine and bounds a vanished holder at 2 minutes.
- **A lost hold mid-run refuses to record.** If the holder session exits while packages are still running, verify errors and writes nothing; rejected re-acquiring silently (another leg may already have perturbed the numbers) and warning-only (a perturbed score looks exactly like a real one).
- **Missing `flock`: mutation leg refuses, `dross test` warns and runs.** Matches the spec's own asymmetry (a suite under load is slow, not wrong; a leg unlocked is silently overlapping). `--local` is the mutation escape, as locked.
- **Detached push stays before the lock.** rsync runs from the laptop and the detached waiter lives on the host, so pushing under the lock would require a host-initiated pull. Accepted the pre-existing overlap (SyncArgs already protects the runs dir); noted rather than solved.
- **Waiting detached run writes `<runDir>/holder` and never deletes it.** State `running` overrides; deleting would add a second write to the transition and a window where status shows neither. Readers print the holder only when state is `scheduled`.
- **Busy detection by `flock -E 75` exit code, not by `$? != 0`.** A usage error or a bad fd exits 1 and would otherwise read as "host busy" and make a waiter wait forever on nothing.
- **Inode check after acquisition (`-ef /dev/fd/9`).** Rejected trusting the path: a user who `rm`s the lock file (they own it) would otherwise split the host into two lock domains with no symptom. Three bounded retries, then an explicit error.
- **`flock` joins `remoteProbeTools`, not the lane routing set.** Doctor and bootstrap see it; pool host selection is unchanged (locked pool_busy_is_not_a_skip and "transport + toolchain" — flock is neither).
- **Exit 15 for `--no-wait`** — first free code above the results band (10–14) and the test band (1–8); pinned unique by test rather than documented by convention.
- **Real Linux test in t-1, gated on `flock` presence.** Runs on the remote Fedora test runner (where the suite is gated per memory), skips on the laptop; the only test in the phase that exercises the kernel semantics c-1/c-4 rest on.
