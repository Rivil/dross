# status-action-surfaces-v2 — panel synthesis

Judged from the three independent drafts (risk / mvp / verification). Soundness
of cited paths was checked against the live tree: `findings.Store`
(`internal/findings/state.go`), the `security`/`quality` packages' run helpers
(`StatePath`/`NewRun`/`ShortSHA`/`SecurityDir`), `status.go`'s
`actionArea`/`actionCatalog`/`renderActionAreas`, and `.gitignore` (which ignores
`.dross/security/` and `.dross/quality/` but **not** `.dross/techdebt/`).

## Scores

Scored /5 per dimension.

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| risk | 5 — all four covered; explicit prune-proof + dead-command guard | 5 — best edge set: NUL/binary, zero-byte, no-trailing-newline, word-boundary `TODOList`, mid-line marker, future/garbled timestamp | 4 — clean 6-task split, but t-3 and t-6 both edit `status.go` in different waves | 3 — `status.go` edited in wave 1 (t-3) *and* wave 3 (t-6) serializes one file across waves |
| mvp | 4 — all four covered but coverage map is over-attributed (t-1/t-3 claimed for c-1) | 3 — concrete thresholds (600-line/400-char) but thin edges; no binary/empty/word-boundary; weak c-3 (string check, not registration) | 3 — coarsest: scan+run merged (t-2); status c-3 doesn't depend on the techdebt command | 4 — cleanest 2-wave dependency, single status task, no same-file cross-wave edit |
| verification | 5 — tightest criterion→test map; c-3 double-guarded (not-planned **and** points-at-real-command) | 4 — named tests with red-light rationale; adds gitignore behavioural check + command-resolution guard; lighter on file-content edges than risk | 5 — scan (t-2) split from run/state/report (t-3); status one task with pure sub-functions unit-tested | 5 — status is a single wave-3 task correctly depending on techdebt registration (t-4); no file edited across two waves |

**Skeleton: verification.** It has the cleanest wave graph (status is one task in
the last wave, correctly gated on the techdebt command's registration, and no
file is edited across two waves), the most rigorous criterion→test mapping
(c-3 double-guarded), and it is the only draft that catches the real
`.dross/techdebt/` gitignore gap. We then graft risk's superior file-content edge
contracts and git-ls-files enumeration, and mvp's concrete thresholds /
`commands_parity_test` hook.

## Merged plan

Display format per task:
`t-N  <title>  [origin]` then `files:` / `covers:` / `depends:` / `contract:`.

Phase status-action-surfaces-v2 — 6 tasks across 3 waves.

### Wave 1 (no deps)

```
t-1  Store-level last_run + stamp helper + never-run sentinel   [mvp+verification+risk]
     files:    internal/findings/state.go, internal/findings/state_test.go
     covers:   c-2 (and c-1: supplies the ranked signal)
     contract: Save/LoadStore round-trips a NEW store-level last_run datetime
               while leaving existing finding Records intact (a pre-existing
               state.toml with records but no last_run loads with records intact
               and LastRun IsZero) [risk]; a missing state.toml loads to
               IsZero()==true = never-run [verification]; StampLastRun(path, now)
               on an empty store leaves last_run non-zero (not IsZero) after
               write; a GARBLED last_run value makes LoadStore return an error,
               not a panic and not a silent zero [risk]. Note: distinct from the
               existing per-Record `LastRun` run-id string.

t-2  Tech-debt marker + size scan engine (pure)                 [risk+mvp+verification]
     files:    internal/techdebt/scan.go, internal/techdebt/scan_test.go
     covers:   c-4
     contract: `x := 1 // FIXME later` yields a FIXME finding (marker regex
               matches mid-line) across all of TODO/FIXME/HACK/XXX; identifier
               `TODOList` yields NO marker finding (word-boundary, not substring)
               [risk]; a file over the line-count threshold (e.g. 600) yields
               exactly one oversized-file finding and a single line over the
               max-length threshold (e.g. 400) yields one over-long-line finding
               [mvp/verification]; a NUL-containing (binary) file and a zero-byte
               file each yield zero findings without error, and a file with no
               trailing newline still has its last line counted [risk]; a clean
               marker-free small tree yields zero findings (no false positives)
               [verification].

t-3  Tech-debt run dir + StatePath + report writer              [verification]
     files:    internal/techdebt/state.go, internal/techdebt/run.go,
               internal/techdebt/state_test.go
     covers:   c-4
     contract: StatePath == .dross/techdebt/state.toml (sibling of run dirs, so
               the signal is prune-proof); NewRun never clobbers — a second run
               in the same second on the same sha gets a "-N" suffix rather than
               overwriting [verification/risk]; WriteReport renders a scanned
               finding set into the run dir's report. Mirrors security's
               run.go/lifecycle.go helpers rather than reusing security.NewRun
               (which is coupled to SecurityDir) [mvp].
```

### Wave 2 (depends Wave 1)

```
t-4  `dross techdebt` command + register + gitignore            [verification + risk enumeration + mvp parity]
     files:    internal/cmd/techdebt.go, internal/cmd/techdebt_test.go,
               internal/techdebt/gitignore_test.go, .gitignore, cmd/dross/main.go
     covers:   c-4
     depends:  t-1, t-2, t-3
     contract: `dross techdebt` enumerates TRACKED files via `git ls-files`, with
               a tree-walk fallback when not in a git repo — completes with a
               "nogit" run id and still scans walked files, no hard error [risk];
               creates .dross/techdebt/<id>/ with a report AND leaves
               findings.LoadStore(techdebt.StatePath).LastRun non-zero — if the
               stamp is skipped the area reads "never run" right after a run and
               the test fails [all]; `git check-ignore` confirms
               .dross/techdebt/<id>/report is ignored — there is no
               .dross/techdebt/ pattern today, so .gitignore must gain one
               [verification]; commands_parity_test sees `techdebt` registered on
               root [mvp].

t-5  Stamp last_run on security & quality runs                  [risk+mvp+verification]
     files:    internal/cmd/security.go, internal/cmd/quality.go,
               internal/cmd/security_test.go, internal/cmd/quality_test.go
     covers:   c-2 (and c-1: makes the two areas rank as "ran")
     depends:  t-1
     contract: after `dross security run`, .dross/security/state.toml has a
               non-zero last_run ~ now; mirror for `dross quality run` →
               .dross/quality/state.toml; stamping into a state.toml that already
               holds finding records leaves those records untouched (merge, not
               overwrite) [risk]. Parallel to the status task, not a dependency of
               it — t-6's tests use state.toml fixtures, so the wiring never
               imports the stamping code [risk].
```

### Wave 3 (depends t-1, t-3, t-4)

```
t-6  Status action block: read signals, rank, render, flip catalog, guard  [verification+risk+mvp]
     files:    internal/cmd/status.go, internal/cmd/status_test.go
     covers:   c-1, c-2, c-3
     depends:  t-1, t-3, t-4
     contract: (pure formatter) formatRunSignal(zeroTime) == "never run";
               formatRunSignal(now-3d) contains "3d ago"; a future last_run does
               not render a negative/absurd age [risk]. (pure sort) rankAreas
               returns never-run areas first in stable catalog order, then ran
               areas oldest-last_run first; an identical last_run keeps catalog
               order (stable tiebreak) [risk]. (integration) with security=5d,
               quality=never, tech-debt=1d the spine-idle actions block lists
               quality, then security, then tech-debt, each line showing its
               run-signal text. (c-3) every available area's command resolves to
               a REAL surface — /dross-secure, /dross-quality, and the registered
               `dross techdebt`; a guard test fails if any available area points
               at a command/skill the binary or assets don't provide, and no line
               still shows "(planned)" [verification+risk]. (prune-proof) with
               every .dross/techdebt run dir deleted but state.toml retaining
               last_run, the area still renders "last run …", never "never run"
               [risk].
```

### Coverage roll-up
- c-1 (order by run signal): t-6 (rank + wired order); t-1 (persisted signal); t-5 (sec/quality rank as ran).
- c-2 (each area shows run-signal state): t-1 (persisted), t-6 (format + render), t-5 (sec/quality stamp).
- c-3 (security/quality runnable, no dead commands): t-6 (catalog flip + not-planned + command-resolution guard).
- c-4 (tech-debt real scan + durable run record): t-2 (scan engine), t-3 (run/state/report), t-4 (`dross techdebt` + stamp).

## Disagreements

1. **Status: split across waves vs single task.**
   risk splits the pure rank+format into wave-1 t-3 and the wiring into wave-3
   t-6 — both editing `internal/cmd/status.go`. mvp and verification keep status
   as ONE task in the last wave, isolating ordering and rendering as pure
   sub-functions each with its own unit test.
   *Default:* single status task (t-6), following mvp+verification.
   *Why it matters:* editing one file in two separate waves serializes those
   waves and invites merge churn; verification shows the same edge-isolation is
   achievable with pure functions inside one task, so the split buys nothing.

2. **Tech-debt run-record packaging granularity.**
   verification splits the pure scan (t-2) from the run-dir/state/report
   mechanics (t-3); mvp merges scan+run into one task; risk folds the run record
   into the command task (t-4).
   *Default:* verification's scan/state split (t-2 + t-3).
   *Why it matters:* the collision-suffix and prune-proof StatePath behaviors get
   their own red lights instead of hiding behind the scan test (mvp) or behind
   command I/O setup (risk).

3. **Gitignore for tech-debt artifacts.**
   Only verification adds a `.gitignore` entry plus a behavioural `git
   check-ignore` test; risk and mvp omit it. Confirmed real gap — `.gitignore`
   ignores `.dross/security/` and `.dross/quality/` but has no `.dross/techdebt/`
   pattern.
   *Default:* include the gitignore work in t-4, following verification.
   *Why it matters:* without it, every `dross techdebt` run commits artifacts and
   dirties the repo, breaking parity with secure/quality. A counter-view exists
   (tech-debt output is not pre-disclosure-sensitive like security, so its ledger
   *could* be tracked) — but the locked staleness model makes state.toml the
   durable signal and run dirs prunable, which favors ignoring them.

4. **File enumeration: `git ls-files` vs tree walk.**
   risk's command contract requires `git ls-files` enumeration with a tree-walk
   fallback outside a git repo; mvp and verification scan a fixture tree directly
   (filesystem walk), never mentioning git enumeration.
   *Default:* `git ls-files` with tree-walk fallback (risk), in t-4.
   *Why it matters:* the locked `techdebt_scan` decision says "across tracked
   files." A plain tree walk would scan untracked/ignored/vendored files,
   contradicting the decision and producing noise findings. (`ShortSHA` already
   returns "nogit" on git failure, so the not-a-repo path is partly precedented.)

5. **`last_run` API shape / never-run sentinel.**
   verification and risk use `time.Time` with never-run = `IsZero()`; mvp uses a
   helper pair `TouchLastRun`/`ReadLastRun` returning `(t, ok bool)`.
   *Default:* `time.Time` + `IsZero()` predicate (verification+risk), with risk's
   garbled→error contract grafted onto LoadStore.
   *Why it matters:* a single `IsZero` predicate collapses "no state OR empty
   timestamp = never run" into one branch, and the existing `LoadStore` already
   returns an empty store (zero value) on a missing file — so IsZero is
   consistent with current code without a second boolean to thread through.

6. **Does the status task's c-3 guard require the techdebt command to be
   registered first?**
   risk (t-6) and verification (t-5) make status depend on the techdebt command
   task (t-4) and assert every available area resolves to a *registered* command
   / an assets prompt file. mvp's status (t-5) depends only on t-1/t-2 and checks
   for the `dross techdebt` *string*, not registration.
   *Default:* status depends on t-4 and uses the registration-resolution guard
   (risk+verification).
   *Why it matters:* a string check passes even if `techdebt` was never wired
   onto root — exactly the "area points at a command that does not exist" failure
   c-3 forbids. Gating on t-4 is what makes the guard meaningful, at the cost of
   keeping status in wave 3.
