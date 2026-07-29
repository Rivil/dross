# Plan Review — root-robustness

Reviewed: 2026-07-29
Plan: 9 tasks across 3 waves

## BLOCKING
(none)

## FLAG

- [coverage] c-2 names four commands; `dross reentry` is the only one with no assertion that it
  exits 0 silently on an incomplete root. t-3 pins `status` and `state touch` (nil error + empty
  stdout), t-4 pins `pause --auto` (nil + handoff.md absent). reentry gets two rows across the
  plan and neither is the silent case: t-4 row 5 is the *loud* bad-TOML case, and t-9's no-write
  snapshot proves reentry writes nothing but never checks its exit code or stdout. reentry.go:39
  already does `errors.Is(err, ErrNoRoot) → nil`, so it works for free after t-1 — which is
  exactly why nothing would catch a regression that makes it loud.
  Suggestion: add a row to t-4 — incomplete `.dross/`, `runCmd(Reentry())` returns nil and
  captureStdout is `""`.

- [test-contract] t-1's final row asserts the fixture migration is "proven by the package
  compiling green". It is not. `chdirDross` (findings_test.go:15) and
  TestTelemetryCover_RepoHashInRepo (telemetry_test.go:108) build their `.dross/` with
  `os.MkdirAll` inside a `t.TempDir()` at runtime; adding project.toml and state.json there
  cannot change whether the package compiles. The migration is gated by those tests *passing*
  under the new FindRoot. (Carried over unchanged from the previous review.)
  Suggestion: restate as the runtime gate — the four chdirDross call sites and
  TestTelemetryCover_RepoHashInRepo pass against the new FindRoot.

- [test-contract] t-5 row 6 ("the missing-file list doctor prints for an incomplete root equals
  the Missing slice LocateRoot returns for the same directory") is unsatisfiable under the
  obvious reading and in tension with rows 1 and 7. `checkFoundationalFiles` (doctor.go:423) is a
  trio — project.toml, rules.toml, state.json — while root completeness is a pair. On row 1's
  fixture (`.dross/` with project.toml only) doctor prints `.dross/state.json` **and**
  `.dross/rules.toml`; LocateRoot's Missing is `[".dross/state.json"]`. Equality holds only if
  doctor emits a *separate* incomplete-root block whose list is distinct from the trio block —
  which row 3's "distinct verdict" hints at but the description never states.
  Suggestion: say in t-5's description whether the incomplete-root diagnosis is its own output
  block, and scope row 6 to that block's list rather than to doctor's whole missing-file output.

- [test-contract] Two rows demand an error that names state.json on a **decode** failure, but the
  code path can't produce one and no listed file can fix it centrally. `state.Load`
  (internal/state/state.go:46) returns `fmt.Errorf("unmarshal state: %w", err)` — no path. Its
  read-failure sibling at :42 does include the path, so t-3 row 4 (`state show`, state.json
  absent) is fine, but t-3 row 5 (`status` against `{{{`) and t-4 row 2 (`pause --auto`,
  truncated state.json) both require the filename on the unmarshal path. `internal/state/state.go`
  appears in no task's `files`.
  Suggestion: either add internal/state/state.go to t-3 and change :46 to name the path, or say
  explicitly that status.go / pause.go wrap the load error with the path themselves.

- [coverage] The locked `completeness_check` decision says a corrupt file is loud "everywhere —
  including in the hook targets", and c-2 names four. Corrupt-file loudness is pinned for
  `status` (t-3 row 5), `pause --auto` (t-4 rows 2-3) and `reentry` (t-4 row 5) but still not for
  `state touch`. `stateTouch` (state.go:76) goes through the same `loadState`; an implementation
  that swallows every `loadState` error in the handler instead of only `ErrNoRoot` passes every
  row in the plan. (Carried over from the previous review — not addressed.)
  Suggestion: add a `state touch` row against a corrupt state.json to t-3.

- [antipatterns] `verify.go:311` has the identical `if root, err := FindRoot(); err == nil`
  RepoHash swallow that t-8 fixes at telemetry.go:32 and :91, but verify.go is not in t-8's
  files. After t-1, `dross verify` outcome events in an incomplete repo lose repo attribution
  while CLI events keep it. Worse, t-9's source scan pins the LocateRoot caller set as *exactly*
  `{root.go, doctor.go, ship_recover.go, telemetry.go}` — so the consistent fix is actively
  forbidden by the regression gate.
  Suggestion: either add verify.go to t-8 and to t-9's expected set, or state in t-8 why verify's
  attribution is deliberately left behind.

- [antipatterns] t-9's description says "Re-derive both allowlists from the tree as it stands
  after wave 2 — do not copy them from the panel drafts", and then the test contract hardcodes
  both sets verbatim. The executor is told to derive and simultaneously given the answer; if the
  derived set differs, it is not clear which wins.
  Suggestion: keep the hardcoded sets as the expectation (they are the point of a regression
  gate) and drop the re-derive instruction, or keep the instruction and mark the sets as
  "expected, verify before pinning".

- [test-contract] t-2's byte-offset row is close to vacuous. `assets/prompts/pause.md` already
  has a `## 0. Pre-flight` at line 15 and the handoff write at line 70 (`## 3. Write + ignore`),
  so *any* placement in §0-§2 satisfies "gate offset < write offset" — including one placed after
  §1's drafting work. The row that would bite is ordering against the first side effect, which in
  §3 is the `.gitignore` append, and against §0 step 3's `dross status` probe.
  Suggestion: pin the gate's offset below the `## 1. Draft the handoff` heading, not below the
  §3 write line.

- [granularity] t-1 touches 5 files, tripping the split threshold. Honest read: it should not be
  split. root_test.go, findings_test.go and telemetry_test.go are fixture fallout that must land
  in the same commit or the package goes red, and ship_recover.go is a one-call-site swap.
  Recorded because the threshold was crossed, not because a split is advisable.
  Suggestion: leave as-is.

## NOTE

- [wave-order] t-2's *test* contract is prompt-text-only and genuinely wave-1-safe, but its
  runtime behaviour depends on t-1. On a `.dross/` holding state.json but no project.toml, today's
  `dross state show` **succeeds** (it loads only state.json), so the probe would wave that repo
  through the gate until t-1 makes FindRoot error. Nothing in the plan catches this because no
  test executes the probe. Same-phase delivery makes it a non-issue; recording it so the gate's
  reliance on t-1 is explicit rather than accidental.

- [antipatterns] Every line reference re-checks out against the working tree: root.go:16/18,
  onboard.go:37, rule.go:224, doctor.go:31 and :55, pause.go:47, telemetry.go:32 and :91,
  findings_test.go:15, telemetry_test.go:108, status_test.go:18, resume_prompt_test.go's
  `resumePromptContent` helper. The two files that don't exist —
  `internal/cmd/pause_prompt_test.go` (t-2) and `internal/cmd/incompleteroot_test.go` (t-9) — are
  created by the tasks that list them. The plan was written against the code, not from memory.

- [locked-decisions] No conflicts. walk_stop is honoured and pinned twice (t-1 row 3's
  parent-complete/child-incomplete fixture, t-9 row 5's onboard-not-in-LocateRoot-callers row);
  completeness_check is pinned by the `{{{` row requiring FindRoot to *succeed*; pause_refusal by
  t-2's both-cases-as-separate-needles row plus "never runs onboard itself".

- [forbidden-actions] No violation. The only project rule is r-01 (`make install` after prompt/Go
  edits), which t-2 cites explicitly. `~/.claude/dross/rules.toml` does not exist. runtime.mode is
  `native` with `go test -count=1 ./...`; nothing in the plan reaches for another runner.

- [prior-review] Three of the previous review's flags are resolved: the onboard/LocateRoot
  collision (t-1 now exposes `MissingRootFiles(dir)`, t-6 forbids LocateRoot by name, t-9 row 5
  pins it), t-9's missing `depends_on` edges to t-5/t-8, and RepairHint's undefined text (t-1 now
  fixes the literal string and t-5 row 5 requires the ship-recover sentence to survive alongside
  it). Three were not addressed and are re-raised above: the "compiling green" row, t-5's
  equality row, and the missing `state touch` corrupt-file row.

- [strengths] Contracts are mutant-oriented rather than behaviour-restating — rows name the wrong
  fix they kill ("a mutant that parses to decide completeness fails here", "moving the swallow
  into loadState() makes show silent and fails here", "a partial fix that prints a header before
  bailing fails on the non-empty stdout"). That is the difference between a contract and a wish.

- [strengths] t-6 and t-7 are second-order consequences found by reading the tree, not the spec:
  onboard.go:37's blanket refusal turns the repair command every new error message advertises
  into a dead end on exactly the roots it should repair, and rule.go:224's ErrNoRoot swallow
  silently widens into a live c-3 hole the moment IncompleteRootError wraps the sentinel. Neither
  is derivable from spec.toml.

- [strengths] The silent/loud boundary is scoped defensively. t-3 puts the swallow in the two
  handlers and adds the row that fails if it migrates into `loadState()`; t-9 turns that boundary
  into a filename-level source scan so command 57 copying the pattern fails by name rather than
  by behaviour. That is the right shape for a rule that will outlive the phase.

## Summary
No blockers — the plan is grounded in the actual tree and its contracts are unusually
mutant-aware, but reentry's half of c-2 is never asserted, two rows demand a state.json filename
the decode path doesn't emit, and three flags from the prior review (the "compiling green" row,
t-5's equality row, the `state touch` corrupt case) are still open.
