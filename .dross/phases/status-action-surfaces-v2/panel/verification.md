# Plan — verification lens

Built backward from each criterion's ideal test contract, then derived the
smallest task that makes that contract satisfiable. Every task names the test
whose red light proves the surface is broken.

Phase status-action-surfaces-v2 — 6 tasks across 3 waves

Wave 1
  t-1  Add store-level last_run + stamp helper
       files:    internal/findings/state.go, internal/findings/state_test.go
       covers:   c-1, c-2
       contract: TestStoreLastRunRoundTrip fails if Save/LoadStore drops the new
                 store-level `last_run` datetime; TestStampLastRunSetsTimestamp
                 fails if StampLastRun(path, now) leaves an empty store's last_run
                 IsZero() after writing; TestLoadStoreMissingHasZeroLastRun fails
                 if a missing state.toml does not yield IsZero()==true (never-run).

  t-2  Tech-debt marker + size scanner
       files:    internal/techdebt/scan.go, internal/techdebt/scan_test.go
       covers:   c-4
       contract: TestScanFindsMarkersAndSizes fails if scanning a fixture tree
                 (a file with `// TODO: x` and `# FIXME y`, a 600-line file, a
                 line >N chars) does not return a marker finding citing that
                 file+line+marker, an oversized-file finding, and an over-long-line
                 finding; TestScanCleanTreeNoFindings fails if a small marker-free
                 tree yields any findings (no false positives → concrete findings).

  t-3  Tech-debt run dir + state path + report writer
       files:    internal/techdebt/state.go, internal/techdebt/state_test.go
       covers:   c-4
       contract: TestTechdebtStatePathIsTopLevel fails if StatePath is not
                 .dross/techdebt/state.toml (sibling of run dirs, prune-proof);
                 TestNewRunNeverClobbers fails if a second NewRun in the same
                 second/sha overwrites the first instead of suffixing; TestWriteReport
                 fails if a scanned finding set is not rendered into the run dir's
                 report.

Wave 2 (depends t-1, t-2, t-3)
  t-4  Add `dross techdebt` command + gitignore
       files:    internal/cmd/techdebt.go, internal/cmd/techdebt_test.go,
                 internal/techdebt/gitignore_test.go, .gitignore, cmd/dross/main.go
       covers:   c-4
       depends:  t-1, t-2, t-3
       contract: TestTechdebtRunWritesRunDirAndStamps fails if `dross techdebt`
                 does not (a) create a .dross/techdebt/<id> run dir with a report
                 and (b) leave findings.LoadStore(techdebt.StatePath).LastRun
                 non-zero; TestTechdebtArtifactsGitignored fails (git check-ignore)
                 if .dross/techdebt/<id>/report is not ignored.

  t-6  Stamp last_run on security/quality run
       files:    internal/cmd/security.go, internal/cmd/quality.go,
                 internal/cmd/security_test.go, internal/cmd/quality_test.go
       covers:   c-1, c-2
       depends:  t-1
       contract: TestSecurityRunStampsLastRun fails if, after `dross security run`,
                 findings.LoadStore(security.StatePath).LastRun.IsZero() is true
                 (the area would be stuck "never run" forever and never rank as
                 ran); TestQualityRunStampsLastRun is the mirror for quality.

Wave 3 (depends t-1, t-3, t-4)
  t-5  Status action-block run signals, ordering, availability
       files:    internal/cmd/status.go, internal/cmd/status_test.go
       covers:   c-1, c-2, c-3, c-4
       depends:  t-1, t-3, t-4
       contract:
         - TestActionAreaRelativeSignal: a pure relative-time/never-run formatter —
           fails if a zero/absent last_run does not render "never run" and a
           last_run 3 days ago does not render "last run 3d ago".
         - TestOrderActionAreasByRunSignal: pure sort over areas+signals — fails if
           given security(never), quality(last_run 10d), tech-debt(last_run 2d) the
           order is not [security, quality, tech-debt] (never-run first in catalog
           order, then ran oldest-last-run first).
         - TestStatusActionsRenderRunSignal: integration — fails if, with two idle
           areas' state.toml on disk (one stamped 3d ago, one absent), the
           `actions:` block does not show both "never run" and "last run 3d ago".
         - TestStatusActionsAreRunnableNotPlanned: fails if the idle-status output's
           security/quality lines omit "/dross-secure"/"/dross-quality" or still
           contain "(planned)"; tech-debt line must show its `dross techdebt` hint.
         - TestSurfacedActionsPointAtRealCommands: fails if any available area's
           command does not resolve — a `dross <x>` hint with no registered
           subcommand, or a `/dross-<x>` slash command with no assets prompt file.

## Coverage
- c-1 (order by run signal, never-run first then most-stale): t-1, t-5; t-6 supplies real timestamps for security/quality so they rank as ran.
- c-2 (each area shows never-run / last run <when>): t-1, t-5; t-6 for security/quality signals.
- c-3 (security/quality runnable, no area points at a missing command): t-5 (TestStatusActionsAreRunnableNotPlanned + TestSurfacedActionsPointAtRealCommands).
- c-4 (tech-debt backed by a real scan, not a placeholder): t-2 (scan), t-3 (run record/state), t-4 (`dross techdebt` invocation), t-5 (rendered runnable, not "(planned)").

## Judgment calls
- Store-level last_run typed as time.Time, never-run = IsZero(). Rejected a string timestamp or `*time.Time`+omitempty: IsZero gives a single clean test predicate and a missing state.toml already loads to zero, so "no state OR empty timestamp = never run" is one branch. Cost: a never-stamped-but-saved store writes `0001-01-01T00:00:00Z`; harmless, IsZero round-trips it.
- Stamp last_run in the `run` subcommands (t-6), not in `findings reconcile`. Rejected stamping at reconcile: a bare `dross security run` must register the signal even before the prompt reconciles, and "written on every run" (locked staleness_signal) reads as the run, not the fold.
- t-5 is wave 3 even though it only code-imports wave-1 outputs (Store.LastRun, techdebt.StatePath). Rejected dropping it to wave 2: its c-3 contract (TestSurfacedActionsPointAtRealCommands) verifies the tech-debt hint points at a *registered* command, so it strictly needs t-4's registration to go green.
- Kept c-1 ordering and c-2/c-3 rendering in one status task (t-5) rather than splitting ordering into a fourth wave. Rejected the split: both edit status.go's action block, so separate waves would serialize edits to one file for no real dependency; isolation is preserved instead by separate pure functions (formatter, sort) each with its own unit test.
- Split the tech-debt scanner (t-2) from its persistence (t-3) though both are one new package. Rejected merging: the scan is pure and language-agnostic (the c-4 substance) while NewRun/StatePath mirror security's run-dir mechanics; separating lets the collision-suffix and prune-proof-path behaviors get their own red lights instead of hiding behind the scan test.
