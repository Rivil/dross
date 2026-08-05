# Plan Review — config-trust-hardening

Reviewed: 2026-08-04 (pass 2)
Plan: 15 tasks across 5 waves

## Prior blocking findings

- [coverage / c-2] PARTIALLY RESOLVED — the three named files were added (basebranch.go and
  ship.go to t-12's list, phase_lifecycle.go with its own `branch -m` contract line), and t-12
  now instructs a re-inventory. But I re-ran that inventory and it surfaces more sites, two of
  them live and unprefixed: `internal/cmd/repair_state.go:79` (`git log <mainBranch> …`, fed
  from `internal/cmd/repair.go:44` = `p.Repo.GitMainBranch`) and `internal/cmd/topology.go:80`
  (`rev-list --count <Main>..<Work>`, `Main` = `p.Repo.GitMainBranch`). Neither file appears in
  any task. See BLOCKING below.

- [locked-decision conflict / refusal_behaviour] RESOLVED — t-5 now exports `ErrRefused` with
  its own contract line (`TestCheckRefusalIsErrRefused`), and t-14 explicitly re-raises on
  `errors.Is(err, hostallow.ErrRefused)` at both sites, with contract lines that also assert a
  *non*-sentinel error still degrades. I confirmed both degrade paths exist exactly where the
  prior review put them (`internal/cmd/phase.go:755`, `internal/cmd/milestone_dependents.go:108`)
  and that both files are in t-14's list.

## BLOCKING

- [coverage / c-2] Two files pass an unprefixed `[repo].git_main_branch` straight into git argv
  as a positional, and appear in no task. c-2 says "every git invocation".
    - `internal/cmd/repair_state.go:79` — `gitTrim(repoDir, "log", mainBranch, "--reverse", …)`.
      `mainBranch` is `p.Repo.GitMainBranch` (repair.go:44 → checkStaleState → reconstructState).
      This one is exploitable, not theoretical: I confirmed on git 2.55 that
      `git log --output=<path> …` writes an attacker-chosen file. A hostile `git_main_branch` of
      `--output=/Users/<u>/.zshrc` gives `dross repair` an arbitrary-file-write. That is the same
      class as the fixture's `--upload-pack` sentinel and it survives the phase as planned.
    - `internal/cmd/topology.go:80` — `gitTrim(repoDir, "rev-list", "--count", t.Main+".."+t.Work)`,
      `t.Main` = `p.Repo.GitMainBranch` (topology.go:52). Leading dash leads the argument; reached
      by `dross status` and `dross phase complete`.
  Neither file is in t-11/t-12's list, neither gets a `validateGitRef` from t-7, and t-15's vector
  list (phase complete, phase checkout, milestone create, ship recover, board, ship, doctor) does
  not drive `dross repair`, so the regression suite would not catch it either.
  Suggestion: add `internal/cmd/repair_state.go` and `internal/cmd/topology.go` to t-12 with their
  own argv assertions, and add a `dross repair` vector to t-1's expected-refusals.txt and t-15's
  table. Also drop t-12's sentence "the file list is the current inventory" — it is not; keep only
  the instruction to re-derive it.

- [coverage / c-2, granularity / t-13] The audit gate will fail on files no task edits. t-13 is
  specified as flagging "any git call passing a non-literal positional without a preceding
  separator" over `internal/` and `cmd/`, with "no file:line exception list". Beyond the two above,
  the current inventory has non-literal positionals here, none of which is in any task's file list:
    - `internal/cmd/phase_checkout.go:42,87` — `rev-parse --verify "refs/heads/"+branch`
    - `internal/cmd/status.go:563,575` — `rev-parse --verify --quiet baseRef`,
      `rev-list --count baseRef --not cur` (`baseRef = "origin/" + sh.base`, from changes.json)
    - `internal/cmd/repair_phasedirs.go:23` — `ls-tree --name-only -d ref+":.dross/phases"`
    - `internal/cmd/milestone_merged.go:78` — `rev-parse --verify --quiet ref`
    - `internal/cmd/repair.go:104` — `commit -m msg`
    - `internal/cmd/statusline.go:275` — `exec.CommandContext(ctx, "git", full...)`, a spread slice
      the AST heuristic cannot resolve at all
  These are prefix-guarded (`refs/heads/`, `origin/`) or flag-values, so they are not injection
  vectors — but they are exactly what t-13's stated rule flags, and t-13 forbids an exception list.
  As written, wave 4 cannot go green without either editing these files or weakening the rule, and
  under r-02 the discovery cannot be deferred silently at execute time.
  Suggestion: state t-13's rule precisely enough to be implementable before wave 4 — e.g. "flags a
  non-literal positional that is not provably prefix-constant" — and name the spread-args case
  (`statusline.go`) as in-scope-but-inert with its reason recorded, per r-02, rather than
  discovering it as a false positive mid-wave.

## FLAG

- [antipattern / t-6 docs premise] t-6's description says "docs/dross.1's local-store section …
  document the key set and go stale on this edit". That is false: `docs/dross.1` has no
  local-store section, no `quick_base`, and no `.dross/local.toml` entry anywhere — its FILES
  section (line 318ff) documents `.dross/project.toml`, `state.json`, `rules.toml`, milestones and
  phase dirs only. README.md:187 *is* accurate ("First key: `quick_base`"). So the man page needs
  a new FILES entry, not an update, and t-6 has no test-contract line covering either doc.
  Suggestion: correct the description to say the man page gains a `.dross/local.toml` FILES entry,
  and add a contract line — the docs are the only part of t-6 nothing gates.

- [granularity / t-12] Eleven files across `internal/cmd` and `internal/codex`, and BLOCKING #1
  adds two more. Splitting the audit into t-13 addressed the heuristic-failure risk but not the
  sweep's breadth: this is now the largest task in the plan, with three upstream dependencies, and
  a single bad argv anywhere in it holds the whole commit.
  Suggestion: split along the natural seam — the `push`/remote-facing sites (basebranch.go,
  ship.go, milestone.go's ls-remote) versus the local-read sites (doctor, techdebt, cleantree,
  repair_*, topology, milestone_stale, codex/git.go). Both halves depend on t-3 only.

- [locked-decision / t-3 stale prose] Spec now carries `ref_separator_token` (locked) and c-2's
  text was amended to "`--` for pathspecs, `--end-of-options` for refs". t-3's description still
  argues against the old text — "the criterion says `--` literally, but …". A verifier reading
  plan and spec together finds the plan disputing a criterion that no longer says that.
  Suggestion: replace t-3's argument with a citation of the locked decision.

- [coverage / git version floor] `ref_separator_token`'s `why` states "Requires git >= 2.24", and
  nothing in the plan implements or records it: t-10 adds two doctor sections but no git-version
  check, and no task touches README/install docs for it. A locked decision that asserts a
  precondition with no enforcement is a criterion nobody can verify.
  Suggestion: add the version check to t-10's doctor work, or record it as an explicit
  `[[deferred]]` entry rather than leaving it only in the decision's rationale.

- [test contract / t-9] `TestNoGetenvOutsideHostguard` greps `internal/ship` non-test sources for
  the bare `os.Getenv(` call. `hostguard.go` — the file the test exists to protect — contains
  exactly that call, so the test fails against a correct implementation unless it exempts its own
  file. The contract does not say it does.
  Suggestion: state the exemption in the contract line, so the exemption is a decision rather than
  something the implementer discovers and papers over.

- [test contract / t-5 layering] `TestCheckRefusesOffAllowlistHost` requires `internal/hostallow`'s
  error text to contain the literal `dross local set allow_hosts` hint. That hardcodes a CLI
  command string into a standalone package that t-5 explicitly justifies as decoupled from both
  `internal/forge` and `internal/ship`.
  Suggestion: keep the hint at the CLI boundary (t-10 already asserts it in doctor) and have t-5
  assert only that the refusal names the host, so the package stays CLI-agnostic.

## NOTE

- [separator viability] I empirically verified `--end-of-options` on git 2.55 for every shape the
  plan touches: rev-parse, rev-list, log, branch (`--list` and `-m`), merge-base, cat-file, show,
  ls-files, ls-tree, diff, checkout, reset, merge, ls-remote, and push — including the two awkward
  ones, `push origin --delete --end-of-options <branch>` and
  `checkout --end-of-options <ref> -- <path>`. All accept it, and
  `rev-parse --verify --end-of-options -x` fails with "Needed a single revision" rather than
  parsing `-x` as a flag. The locked decision is technically sound.

- [locked-decision / client_scope] t-8 names four constructors but `internal/forge` has five —
  `NewBoard` (forge.go:150). It is a pure dispatcher to `NewYouTrack`/`NewJira`/
  `NewGitHubProjects`/`New`, so the four-constructor guard is sufficient and `issue.go`'s
  `forge.NewBoard(boardConfig(...))` call is covered transitively. Recording it so a later reader
  does not read the count as a gap.

- [granularity / t-14 ripple] Verified every caller of the three changing signatures is in t-14's
  file list: `buildOpenOpts` at phase.go:426, milestone_dependents.go:95, milestone.go:185,
  ship.go:312; `buildCommentOpts` at ship.go:500; `boardConfig` at issue.go:90. The prior pass's
  ripple flag is fully closed.

- [wave order] Every cross-wave edge is either a genuine output dependency (t-7→t-2 needs
  `validateGitRef`; t-8/t-9/t-10→t-5 need `Policy`/`ErrRefused`; t-13→t-11/t-12 needs the swept
  tree; t-14→t-8/t-9 need the `Hosts` fields) or a stated file-contention edge (t-11/t-12→t-7 over
  phase.go/milestone.go/ship_recover.go; t-12→t-10 over doctor.go). t-11 and t-12 share no files,
  so wave 3 parallelises cleanly; t-13 and t-14 share no files either. Do not "optimize" the
  contention edges into earlier waves.

- [rules] No forbidden action. r-01 does not bite — nothing in the plan depends on the installed
  binary; t-15's red-proof replay runs `go test` in a worktree. r-02 is written into t-13's task
  text rather than left to the executor's memory, which is the right place for it.

- [strength] Both prior blocking findings were answered with mechanism, not acknowledgement. The
  `ErrRefused` sentinel is threaded end to end — exported in t-5, asserted in t-5's own contract,
  consumed by name in t-14 — and t-14's contract line insists a *non*-sentinel error still
  degrades, so the fix cannot be implemented by simply removing the fallback. That is the harder
  half of the finding and the author caught it unprompted.

- [strength] c-5's red proof went from a prose promise to a gated artefact:
  `TestRedProofPinsBaseCommit` requires RUN.md to name a real base commit SHA and one failing test
  per vector id, so verify can re-run the replay instead of trusting the record. Paired with
  `TestEveryVectorHasASubtest` and `TestHostileConfigNoPwnSentinel`, the fixture is hard to green
  by deletion.

- [strength] The new c-7 migration answer is right: `ensureDrossGitignore` is called only from
  init.go:101 and onboard.go:119, so already-onboarded repos never re-run it — and t-10 now reports
  the missing ignore line as a doctor finding with its own test, which is the one command those
  repos do run.

## Summary
The two prior blockers are answered — refusal_behaviour fully, c-2 only for the three files named
last pass — but re-running the inventory the plan itself asks for turns up `repair_state.go`
(a live `git log --output=` arbitrary-file-write via `git_main_branch`) and `topology.go` in no
task at all, plus six prefix-guarded sites that t-13's no-exceptions audit rule will flag in
wave 4; everything else is tightening.
