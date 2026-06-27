# status-action-surfaces-v2 — RISK lens

Failure modes drive the graph. The signal that ranks areas is a hand-editable
TOML timestamp that can be missing, zero, garbled, or in the future; the
tech-debt scanner walks arbitrary repo files (binary, empty, CRLF, huge,
outside-a-git-repo); run dirs get pruned out from under the signal. Each of
these is owned and tested by exactly one task below.

Phase status-action-surfaces-v2 — 6 tasks across 3 waves

Wave 1
  t-1  Add store-level last_run to findings.Store
       files:    internal/findings/state.go, internal/findings/state_test.go
       covers:   c-2
       contract: Loading a pre-existing state.toml that has finding records but
                 no last_run key yields a Store whose records are intact and
                 LastRun is the zero time (the never-run sentinel) — if the new
                 field drops records or fails to default, the round-trip test
                 fails. A state.toml with a garbled last_run value makes
                 LoadStore return an error (not a panic, not silent zero).
                 Save-then-load preserves a non-zero LastRun exactly.

  t-2  Build tech-debt marker + size scan engine
       files:    internal/techdebt/scan.go, internal/techdebt/scan_test.go
       covers:   c-4
       contract: Scanning a file containing `x := 1 // FIXME later` yields a
                 FIXME finding (marker regex matches mid-line, not only at line
                 start) across all of TODO/FIXME/HACK/XXX; the identifier
                 `TODOList` yields NO marker finding (word-boundary match, not
                 substring). A file over the line-count threshold yields exactly
                 one oversized-file finding; a single line over the max-length
                 threshold yields an over-long-line finding. A NUL-containing
                 (binary) file and a zero-byte file each yield zero findings and
                 do not error. A file with no trailing newline still has its last
                 line counted.

  t-3  Action-area ranking + run-signal formatting (pure)
       files:    internal/cmd/status.go, internal/cmd/status_test.go
       covers:   c-1, c-2
       contract: rankAreas over two never-run and two ran areas returns both
                 never-run areas first in stable catalog order, then the ran
                 areas oldest-last_run first (most-stale on top); two areas with
                 an identical last_run keep catalog order (stable tiebreak).
                 formatRunSignal(zeroTime) == "never run"; formatRunSignal(now-3d)
                 contains "3d ago"; a last_run in the future does not render a
                 negative or absurd age.

Wave 2 (depends t-1, t-2)
  t-4  Add `dross techdebt` command + durable run record
       files:    internal/techdebt/run.go, internal/cmd/techdebt.go,
                 internal/cmd/techdebt_test.go, cmd/dross/main.go
       covers:   c-4
       depends:  t-1, t-2
       contract: `dross techdebt` in a temp git repo creates
                 .dross/techdebt/<runid>/ containing findings.toml and report.md
                 AND writes .dross/techdebt/state.toml whose last_run equals the
                 run time (non-zero) — if the command writes the run dir but
                 skips the state stamp, the state-file assertion fails. Run
                 outside any git repo completes with a "nogit" run id and still
                 scans walked files (git ls-files failure falls back to a tree
                 walk, no hard error). A second run in the same second on the
                 same sha gets a "-2" suffixed dir rather than clobbering the
                 first.

  t-5  Stamp last_run on security & quality runs
       files:    internal/cmd/security.go, internal/cmd/quality.go,
                 internal/cmd/security_test.go, internal/cmd/quality_test.go
       covers:   c-2
       depends:  t-1
       contract: After `dross security run`, .dross/security/state.toml exists
                 with a non-zero last_run equal to the run timestamp; ditto
                 `dross quality run` → .dross/quality/state.toml. If either run
                 path forgets the stamp, its area would read "never run"
                 immediately after a run and the test fails. Stamping into a
                 state.toml that already holds finding records leaves those
                 records untouched (merge, not overwrite).

Wave 3 (depends t-1, t-3, t-4)
  t-6  Wire status actions block: read signals, rank, render, flip catalog
       files:    internal/cmd/status.go, internal/cmd/status_test.go
       covers:   c-1, c-2, c-3
       depends:  t-1, t-3, t-4
       contract: With fixtures where security ran 5d ago, quality never ran, and
                 tech-debt ran 1d ago, the spine-idle actions block lists quality
                 (never run) first, then security (last run 5d ago), then
                 tech-debt (last run 1d ago) — and each line shows its run-signal
                 text. Every available area's command resolves to a real surface:
                 /dross-secure and /dross-quality skills plus the registered
                 `dross techdebt` command — a guard test walks the catalog and
                 fails if any available area points at a command/skill the binary
                 or assets don't provide (c-3). If every .dross/techdebt run dir
                 is deleted but state.toml retains last_run, the area still
                 renders "last run …" not "never run" (prune-proof: the signal is
                 read from state.toml, never from run-dir mtimes).

## Coverage
- c-1 (ordering by run signal): t-3 (pure rank), t-6 (wired ordering in output)
- c-2 (each area shows run-signal state): t-1 (persisted signal), t-3 (format/never-run), t-5 (sec/quality stamp), t-6 (render in block)
- c-3 (security/quality runnable, no dead commands): t-6 (catalog flip + command-existence guard)
- c-4 (tech-debt real scan + durable run record): t-2 (scan engine), t-4 (`dross techdebt` + run record)

## Judgment calls
- Split the tech-debt scan engine (t-2) from its invocation (t-4): the engine is a pure function over file paths so binary/empty/CRLF/over-long edge cases are unit-tested without a repo; the git-ls-files enumeration and the not-a-git-repo fallback are the I/O risk, owned by t-4. Rejected a single techdebt task — it would have buried the content edge cases behind filesystem setup.
- t-3 (pure rank+format) is wave 1, separate from the t-6 status wiring: ordering (tie-break, never-run-first) and rendering (zero/future timestamp) are distinct failure surfaces best tested as pure functions; the wiring then only proves they're hooked up. Rejected folding rank+format into t-6, which would force every ordering edge case through full status-command output.
- last_run lives on findings.Store (t-1), not a new per-area file: reuses the existing atomic SaveStore and the prune-proof state.toml the staleness_signal decision mandates. Rejected run-dir mtimes (pruning would silently reset an area to never-run — explicitly the risk t-6's prune contract guards).
- t-5 (sec/quality stamping) is wave 2 parallel to t-6, not a dependency of it: t-6's tests use state.toml fixtures, so the wiring doesn't import the stamping code — only the live feature needs both. Keeping them parallel widens wave 2.
- Tech-debt writes a findings.toml ledger and state.toml but full findings-lifecycle reconcile wiring is left out: no criterion asks for tech-debt triage states, and the staleness signal only needs last_run. Pulling reconcile in would add a fingerprint/lifecycle surface with no covering criterion.
