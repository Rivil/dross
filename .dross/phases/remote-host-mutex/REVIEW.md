# Plan Review — remote-host-mutex

Reviewed: 2026-09-21 (round 2)
Plan: 9 tasks across 5 waves

## BLOCKING
(none)

The round-1 blockers hold together after amendment: the zero WaitPolicy now
means `flock -n` everywhere (t-1 renders it, t-3 returns ErrHostBusy under it,
t-8 explicitly maps ErrHostBusy onto the alongside path, t-5/t-6 use only
Forever/zero), and dispatchDetached's holder stamping now lands in t-2 beside
DetachScript's refusal, with t-6's TestDetachedHolderIsTheRunID pinning the
record/script agreement from the other side.

## FLAG
- [antipatterns/files] t-6 changes `resolveMutationTuning`'s signature but its
  `files` list omits `internal/cmd/mutation_remote_wiring_test.go`, which calls
  `resolveMutationTuning(p, root)` at four sites (lines 173, 276, 308, 338).
  In Go a non-compiling test file takes the whole `internal/cmd` package red,
  so the gate cannot pass after t-6 as listed.
  Suggestion: add the file to t-6 and note the four call sites gain
  `phaseID, remote.WaitPolicy{Forever: true}` (or whatever the test asserts).

- [test-contract] t-6's TestLocalVerifyNeverTouchesTheLock is written against
  "`--local` or a transport fallback", but `dross verify` has no `--local` flag
  (its flag set is skip-mutation / detach / at / reuse-report / base; the
  TestScopingHasNoOptOut allowlist confirms it). The spec's locked
  `no_bypass_flag` inherits the same misreading ("`--local` remains the escape
  hatch for a mutation leg" — the real escape is `dross mutation remote
  revoke`; `--local` belongs to `dross test`). The transport-fallback half of
  the contract is real and testable; the `--local` half tests a surface that
  does not exist, and an executor who adds one to satisfy it would also have to
  allowlist it in TestScopingHasNoOptOut, which t-6 does not plan for.
  Suggestion: reword the contract to "a transport fallback or an ungranted repo
  leaves Target nil" and drop the `--local` clause; do not add a verify flag.

- [test-contract] t-1 pins the record write as `dd of=PATH conv=nocreat
  status=none` but never pins where dd's INPUT comes from. `dd` with no `if=`
  reads stdin. In the hold session (t-3) stdin is the `bash -s` script pipe that
  Acquire keeps open — dd would swallow the keepalive stream and block until
  Release, so `lock=acquired` never prints and every Acquire hangs. In the
  detached job (t-2) stdin is `/dev/null`, so dd writes an EMPTY record and
  every waiter prints "holder not yet recorded". TestRealFlockSerializesAndReleasesOnKill
  would not catch the first (its stdin closes after the script) and nothing
  catches the second.
  Suggestion: pin the pipeline form in TestLockPreludeWritesRecordWithoutCreate —
  `printf … | dd of=PATH conv=nocreat status=none` — and add an assertion that
  the dd has no `if=` AND is preceded by a pipe.

- [test-contract] t-1's LockStatusScript contract covers tool-missing, free and
  busy, but not the case where `/tmp/dross-host.lock` does not exist yet — a
  host no leg has ever run on, which is every host on the day this ships. The
  script "creates nothing" (correct), so its `exec 9<PATH` fails; in a
  non-interactive bash a failed `exec` redirection exits the shell, the output
  has no `lock=` line, and ParseLockStatus returns an error. Doctor then reports
  an error on a healthy fresh host instead of `✓ host lock free`, and `verify
  status` falls into the "(could not read the host lock)" branch.
  Suggestion: add a contract to t-1 — an absent lock path prints `lock=free`
  (e.g. `[ -e PATH ] || { printf 'lock=free\n'; exit 0; }` before the open) —
  and a corresponding fake-output case in TestParseLockStatus.

- [test-contract] t-1 declares `HostLockPath = "/tmp/dross-host.lock"` next to
  `LockTool = "flock"` as if both are constants, yet three contracts
  (TestRealFlockSerializesAndReleasesOnKill, TestTwoHoldsSerializeOnARealFlock,
  TestAKilledHolderReleasesWithNoCleanup) run "against a temp HostLockPath".
  These tests run on helicon via the remote `dross test` gate — which, after
  t-8, itself holds `/tmp/dross-host.lock` for the length of the suite. If the
  path is a const, the in-suite Forever Acquire waits on the gate's own hold
  and the suite deadlocks.
  Suggestion: state in t-1 that HostLockPath is a package `var` (or that the
  builders take the path) and add one line to the real-flock contracts: "never
  the production path".

- [test-contract] t-3's protocol leaves the prelude's post-`lock=busy`
  behaviour open, and t-8 depends on it. Under a bounded wait Acquire returns
  Outcome Alongside with err nil and t-8 then calls Release "after the last
  lane". If the prelude exits the shell after printing `lock=busy` (as the zero
  policy's "no process left" implies), Lost() flips immediately and
  TestLostFlipsWhenTheSessionDies requires Release to return the exit as an
  error naming the host — so every expired-wait `dross test` ends with a
  spurious "lock lost" error from Release, or t-8 has to know to ignore it.
  Suggestion: pin in t-1 whether the prelude continues into the caller's text
  after `lock=busy` (hold: keepalive loop runs with nothing held; detached:
  unreachable under Forever) or exits 0, and in t-3 state that an Alongside
  hold's Release returns nil.

- [antipatterns] t-2's detached noflock path writes 127 + a log line naming
  flock and `dross doctor` — on the HOST. From the laptop, `verify results`
  reaches the existing "exited 127 and produced no measurable report" branch
  (verify.go:684), which never mentions flock; `verify status` prints
  `finished (exit 127)`. verify's preflight (`selectRemoteTarget(targets,
  nil)`) probes no tools, so the missing binary is not caught at dispatch
  either. The amendment is coherent but the user never learns why.
  Suggestion: either have dispatchDetached probe `flock` before the push (the
  probe seam already exists and t-4 adds the name to the list) and refuse with
  the doctor wording, or have results print the log's last line on a 127.
  Choose one and put it in t-2 or t-6.

- [granularity] t-5 touches 9 files. Seven are test files gaining a RunID on
  inline `remote.Target{}` constructions (the list matches the repo exactly —
  verified), so the spread is mechanical rather than multi-layer; still over
  the 5-file line.
  Suggestion: acceptable as one task if the executor commits the seven
  fixture edits first (they are a no-op until the refusal lands) and then the
  launcher change; splitting is optional.

- [locked-decision] `attached_busy_policy` locks "a one-line heartbeat every
  few minutes" for an attached leg. t-3 makes HeartbeatEvery a parameter and
  tests it at 50ms; t-5 wires `HoldEvents.Log = launcherLog` but never says
  what HeartbeatEvery the launcher passes. Zero (the Go default) is either "no
  heartbeats" or "every tick" depending on Acquire's handling, and neither
  contract pins it.
  Suggestion: pin the launcher's interval in t-5 (a `launcherHeartbeat` var,
  e.g. 5m) with one assertion that HoldEvents.HeartbeatEvery > 0 reaches the
  seam.

## NOTE
- [coverage] Every criterion is covered: c-1 (t-2, t-3, t-5, t-6), c-2 (t-1),
  c-3 (t-2, t-3, t-5, t-6, t-7, t-9), c-4 (t-2, t-3, t-5, t-8, t-9), c-5 (t-4,
  t-9), c-6 (t-8, t-9), c-7 (t-1). t-3's TestTwoHoldsSerializeOnARealFlock
  says "c-1, c-2 proven" in its contract but t-3's `covers` omits c-2 — add it
  for the verify mapping.

- [coverage] c-7 (cross-user exclusion) rests entirely on script-shape
  assertions in t-1 (no O_CREAT open, guarded 0666 create, `conv=nocreat`
  write). Nothing in the plan runs as two unix users, and the gate cannot.
  Worth one manual two-user check on helicon during verify rather than
  treating the text assertions as proof.

- [wave-order] Wave layout is tight: t-6 genuinely needs t-2 (stamping), t-3
  (ErrHostBusy) and t-5 (launcher refusal); t-7 needs only t-2; t-8 only t-3;
  t-4 only t-1. No two tasks in the same wave share a file. t-6 and t-7 both
  edit verify.go but sit in different waves.

- [antipatterns] t-8 threads `wait` "through runTest / runTestLanes /
  runOneLane / runLanePrepare / runRemoteLine". The hold is taken before
  syncTreeTo at the runTestRemotely / runTestLanes level (verified: syncTreeTo
  is called only from those two, per host), so runOneLane, runLanePrepare and
  runRemoteLine never consult it. Threading a parameter through three functions
  that ignore it is noise; the "no package-level state" intent is met by the
  first two alone.

- [antipatterns] After t-2 the host's initial state is always `scheduled`, but
  dispatchDetached still records `state = "running"` locally for a no-`--at`
  run. printDetachedStatus's unreachable branch prints that recorded value, and
  inFlightRunWarning says "is running on <host>" for a run that is waiting on
  the lock. Not wrong (it is labelled "recorded"), but t-7 could set the
  recorded initial state to `scheduled` for consistency at zero cost.

- [test-contract] Holder rendering with an empty Phase — the survivor drain
  (t-6 passes "") and `dross test` (t-8 passes "") — produces "dross/ run
  t-…" in WaitLine and doctor's `held by <project>/<phase>` line. Cosmetic;
  worth a placeholder ("dross/(test)") in t-3's WaitLine.

- [test-contract] t-1's Validate refuses only a newline in a holder field; the
  empty-RunID refusal lives in DetachScript (t-2) and newLauncher (t-5) but
  not in Acquire/LockPrelude. A future third caller of Acquire (there are two
  today) could hold anonymously. Consider one refusal at LockPrelude.

- [granularity] Per-adapter Run means a verify with two legs (gremlins +
  stryker) acquires twice, with a window between legs where another host's leg
  can slip in. Fine for this repo (gremlins only) and consistent with
  `lock_granularity` as worded ("per run" = per adapter run), but worth a
  sentence in ARCHITECTURE.md (t-9).

- [granularity] t-1 carries 16 contracts and builds the prelude, status
  script, both parsers, the Target field and the real-flock test in one go.
  Single package, single layer, and every contract is about one text builder,
  so it holds together; expect it to be the longest task.

- [strengths] The contracts are the best part of this plan: each names the
  test, the exact text or ordering that breaks it, and the failure it
  prevents (e.g. "busy is detected by anything other than `$? -eq 75` so a
  flock usage error never reads as busy"; "the push is inside the hold"). An
  executor cannot satisfy them with a green-but-vague test.

- [strengths] The protected_regular / O_CREAT / `conv=nocreat` reasoning is
  encoded as tests rather than prose, which is what makes c-7 survive a future
  "simplify the shell" edit; t-9's ARCHITECTURE contract then pins the same
  strings so the doc cannot drift from the tests.

- [strengths] The round-1 zero-policy fix was resolved at the right layer:
  Acquire's contract stays uniform (zero → ErrHostBusy) and the two callers
  map it according to their locked policies, rather than teaching Acquire two
  meanings of zero.

## Summary
No blockers; the round-1 amendments cohere, but three of the new FLAGs are
things an executor would only discover on the host (dd reading the wrong stdin,
the absent-lock-file status path, and a const HostLockPath deadlocking the
remote gate) and one is a compile break (t-6's unlisted test file) — fix those
four in the plan before execution and the rest can be judgment calls.
