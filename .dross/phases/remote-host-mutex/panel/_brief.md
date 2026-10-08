# Planner brief — remote-host-mutex (shared by all three lens planners)

## Decomposition rules (from /dross-plan §2)

Walk through criteria one by one. For each: what's the smallest task that
delivers this criterion? Working backward from acceptance, not forward from tech.

For each task, decide:

| Field | Notes |
|---|---|
| `id` | `t-1`, `t-2`, … sequential |
| `wave` | `1` = runs first, `2` = depends on wave-1 output, etc. |
| `title` | Imperative, ≤8 words ("Add tags + meal_tags schema") |
| `files` | Concrete paths, not patterns. Read existing files first if uncertain. |
| `description` | 1-3 lines. What changes, not why. |
| `covers` | Criterion ids this task delivers (`["c-1", "c-2"]`) |
| `test_contract` | One or more **specific** statements: "if X breaks, test Y fails". Generic ("tests pass") is rejected. |
| `depends_on` | Task ids in lower waves. Empty = pure wave-1. |
| `status` | `pending` |

### Granularity rules

- Too small: if a task touches one file and is < 10 minutes of work, it probably belongs merged into another.
- Too large: if a task spans 5+ files OR more than 2 layers, split it.
- Wave correctness: task X is wave N+1 only if it strictly needs the output of a wave-N task. If it doesn't, drop it to wave N for parallelism.

### Test contract quality

A test contract is specific when:
- It names the surface that breaks ("the unique constraint", "the rate limiter", "the 401 path")
- The user can imagine the test from the description ("11th tag insert returns 400")

Reject: "tests pass", "covered by existing tests", "integration test exists".

## Display format for the plan section of your draft

```
Phase remote-host-mutex — N tasks across W waves

Wave 1
  t-1  <title>
       files:    <path>, <path>
       covers:   c-1, c-2
       contract: <specific statement>
                 <specific statement>

Wave 2 (depends t-1)
  t-2  <title>
       ...
```

## Codebase orientation (read these before drafting)

All remote execution is script-over-ssh, built by pure argv/script builders and
run through swappable seams so tests never touch a network.

- `internal/remote/remote.go` — Target, `Script`/`ScriptAll` (attached, one
  ssh call per command, last command `exec`'d), `DetachScript` (setsid nohup
  inner `bash -c`, writes state/exit/pid files under `.dross-runs/<run>`),
  `StatusScript`/`ParseStatus` (labelled `k=v` lines), `Probe` (host tool
  readiness), `shellQuote`. Tests: `remote_test.go`.
- `internal/mutation/launcher.go` — attached mutation leg: `toolCmd` builds ONE
  ssh call PER PACKAGE via `ScriptAll`; `ensurePushed`, `clearReport`,
  `fetchReport` are further ssh/rsync calls in the same run. So an attached leg
  is a SEQUENCE of short ssh sessions from one local process — a lock held
  "across every package" must outlive any single ssh call yet die with the
  local process (c-4). Tests: `launcher_test.go`, `remote_failure_test.go`.
- `internal/cmd/verify.go` — `dispatchDetached` (builds `detachSequence`, one
  inner script for the whole run, via `DetachScript`), `printDetachedStatus`,
  `collectDetachedFrom`, `cancelDetached`; detached state vocabulary is
  scheduled/running/finished (`verify_status_test.go`, `verify_detach_test.go`,
  `verify_results_test.go`). Record type `detachedRun` in `internal/cmd/local.go`.
- `internal/cmd/mutation_remote.go`, `remote_preflight.go` (`preflightRemote`
  probes tools before a run), `remote_pool.go` (host selection).
- `internal/cmd/test.go` — `dross test`: `resolveTestTarget`, `runOneLane`,
  `spawnRemote` seam (`runRemoteCommand`), `--local` flag; lanes in
  `test_lane.go`. Tests: `test_remote_test.go`, `test_test.go`.
- `internal/cmd/doctor.go` — `checkRemoteMutation` (the "Remote mutation"
  section), `remoteProbeTools`, `remoteProbeFn` seam. Tests:
  `doctor_remote_test.go`.
- Docs that describe these surfaces and are pinned by tests: `README.md`,
  `docs/` (grep for `--detach`, `verify status`, `dross test`), and
  `ARCHITECTURE.md`. Check which doc tests exist (`*_doc_test.go`,
  `readme_doc_test.go`) before adding a flag — some assert every flag is documented.

## Known host facts (measured 2026-09-21 on the real remote host, Fedora 44)

- `flock` is util-linux 2.41.5 at `/usr/bin/flock`. It is NOT guaranteed on
  every host — c-5 makes it a probed tool.
- `/tmp` is `drwxrwxrwt root:root` and `fs.protected_regular = 1`.
  CONSEQUENCE: an `open(..., O_CREAT)` on a file in /tmp that is owned by a
  DIFFERENT user fails with EACCES even though the file exists. `flock(1)`
  always opens its path argument with O_CREAT, and bash `>`/`>>`/`<>`
  redirections do too. So under c-7 (user B waits on user A's lock):
    * `flock /tmp/dross-host.lock cmd` run by user B FAILS to open when user A
      created the file. Open the file read-only WITHOUT O_CREAT and lock the
      descriptor instead (`exec 9</tmp/dross-host.lock && flock -x 9` — flock(2)
      LOCK_EX works on a read-only fd), creating the file only when absent.
    * writing the holder record with `printf ... > /tmp/dross-host.lock` FAILS
      for the non-owner. Write without O_CREAT (e.g. `dd of=... conv=nocreat
      status=none`, or `truncate -c -s0` + a no-create write).
    * creating the file must ALSO chmod it 0666 explicitly — umask makes a
      plain create 0644, which would deny every other user the write in
      holder_record.
  A plan that ignores this ships a c-7 that only works when one unix user ever
  touches the host. Own this hazard in exactly one task with a contract that
  pins the generated script text.
- `setsid`, `nohup` are already relied on by `DetachScript`.
