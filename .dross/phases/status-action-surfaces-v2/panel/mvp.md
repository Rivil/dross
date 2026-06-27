Phase status-action-surfaces-v2 — 5 tasks across 2 waves

Wave 1
  t-1  Add store-level last_run + helpers
       files:    internal/findings/state.go
       covers:   c-1, c-2
       contract: state_test.go: TouchLastRun(path, t) then ReadLastRun(path)
                 returns (t, true); ReadLastRun on a missing/empty state.toml
                 returns ok=false. If the new `last_run` field isn't round-tripped
                 through LoadStore/SaveStore the equality assertion fails.

  t-2  Build tech-debt scanner + run package
       files:    internal/techdebt/scan.go, internal/techdebt/run.go
       covers:   c-4
       contract: techdebt/scan_test.go: scanning a fixture with a `// TODO` line,
                 a 600-line file, and a 400-char line yields one marker finding +
                 two size findings (oversized-file, over-long-line); a clean small
                 file yields zero. If a heuristic regresses the count assertion
                 fails. run_test.go: NewRun creates a fresh dir under
                 .dross/techdebt and StatePath returns .dross/techdebt/state.toml.

Wave 2 (depends t-1, t-2)
  t-3  Persist last_run on security & quality runs
       files:    internal/cmd/security.go, internal/cmd/quality.go
       covers:   c-1, c-2
       depends:  t-1
       contract: security_test.go / quality_test.go: after `dross security run`
                 (resp. `dross quality run`) in a temp repo, findings.ReadLastRun
                 on .dross/security/state.toml (resp. .dross/quality) returns
                 ok=true with a timestamp within seconds of now. If the run handler
                 forgets the TouchLastRun call the ok=true assertion fails.

  t-4  Add `dross techdebt` command
       files:    internal/cmd/techdebt.go, cmd/dross/main.go
       covers:   c-4
       depends:  t-1, t-2
       contract: techdebt_test.go: `dross techdebt` in a temp repo containing a
                 TODO + an oversized file creates a run dir under .dross/techdebt
                 whose report lists those findings AND writes state.toml with a set
                 last_run (ReadLastRun ok=true). If the scan is skipped the report
                 assertion fails; if TouchLastRun is skipped the last_run assertion
                 fails. commands_parity_test.go sees `techdebt` registered on root.

  t-5  Rework status action surface
       files:    internal/cmd/status.go
       covers:   c-1, c-2, c-3
       depends:  t-1, t-2
       contract: status_test.go, three behaviors against a temp repo with
                 per-area state.toml fixtures:
                 (c-1 order) security last_run=10d ago, quality never-run,
                   tech-debt last_run=1d ago → actions block lists quality first,
                   then security, then tech-debt; regressing to catalog order fails.
                 (c-2 signal) a never-run area renders "never run"; a ran area
                   renders "last run 1d ago".
                 (c-3 runnable) security/quality lines show "/dross-secure" /
                   "/dross-quality" and tech-debt shows "dross techdebt", none with
                   "(planned)". If any area still prints "(planned)" the test fails.

## Coverage
- c-1 (order by run signal):   t-1, t-3, t-4, t-5
- c-2 (per-area run signal):    t-1, t-3, t-4, t-5
- c-3 (secure/quality runnable): t-5
- c-4 (tech-debt real scan):    t-2, t-4

## Judgment calls
- Tech-debt ships scan + run-record + last_run only; rejected porting the full
  findings-state/reconcile/scaffold machinery secure/quality carry — no criterion
  asks for triage state on tech-debt, c-4 only needs concrete findings vs placeholder.
- last_run is a single store-level field on the existing findings.Store (per locked
  staleness_signal), with shared TouchLastRun/ReadLastRun helpers reused by all three
  areas; rejected per-area duplicated load/touch/save and rejected run-dir mtimes
  (locked against).
- Relative-time formatting and the ranking comparator live inline in status.go;
  rejected a new package — single consumer, YAGNI.
- t-3 (run-time writes in security.go/quality.go) kept separate from t-5 (status
  render); merging would span three files across two concerns for no saving.
- tech-debt run-dir + StatePath mirror security's own helpers rather than reusing
  security.NewRun, which is coupled to SecurityDir/root.
