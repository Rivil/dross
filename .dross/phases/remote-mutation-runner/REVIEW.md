# Plan Review — remote-mutation-runner

Reviewed: 2026-08-14 (pass 2)
Plan: 10 tasks across 3 waves

## BLOCKING

- [wave order] Three tasks depend on same-wave tasks. `t-8` (wave 3) and `t-9` (wave 3)
  both declare `depends_on` on `t-6`, which is also wave 3. This violates the plan schema
  dross itself documents and enforces: `assets/prompts/plan.md:37` — "`depends_on` | Task
  ids in **lower** waves" — and `internal/phase/plan_edit.go:215-292`, whose locked
  `move_wave_semantics` refuses any move that leaves "a dependent in a wave not strictly
  greater than its dependency's". `dross task move` would reject this shape; the plan file
  contains it.

  It is not cosmetic. Wave 3 as written is the batch {t-6, t-7, t-8, t-9}, and t-6 and t-8
  both edit `internal/mutation/launcher.go`. Executed as a wave, two tasks write the same
  file while one needs the other's error mapping.

  Suggestion: move t-8 and t-9 to wave 4. (If t-8's dependency on t-6 really is only the
  restore-failure error mapping, the alternative is to drop `t-6` from its `depends_on` and
  leave it at wave 3 — but then the launcher.go contention is unmanaged, so wave 4 is the
  better call.)

- [test contract] c-4 names three failure modes — "unreachable host, **missing tool**,
  failed sync" — and the plan pins two. t-1 classifies ssh 255 → ErrTransport and rsync
  23/24 → ErrPartial; t-6 pins 255 fatal, 23 tolerated, gremlins exit 1 tolerated. Nothing
  pins the missing-tool exit codes (126/127), and on today's code path that is the exact
  regression c-4 exists to prevent.

  `internal/mutation/gremlins.go:184-201`: a failure to *start* the process (binary not
  found) is a non-`ExitError` and is fatal; anything that *ran* and exited non-zero falls
  through to the report read, and a missing report becomes an `UnmeasuredMissing` skip.
  Under remoting that inversion is silent — ssh starts fine and returns the remote
  command's 127, which is an `ExitError`, so "gremlins is not installed on helicon"
  arrives as `UnmeasuredMissing`: non-fatal for the drain, excluded from the score, and it
  reads as clean. t-1's `classify` puts 127 in `ErrRemoteCommand` but no contract line says
  which side of fatal `ErrRemoteCommand` with no report lands on, so the implementation is
  free to pick the wrong one and pass every test listed.

  t-5's doctor check is not the answer — that is c-5, it is a separate command, and a
  toolchain can go missing between a doctor run and a verify run.

  Suggestion: add a t-6 contract line pinning remote exit 127/126 with no report as fatal
  and named ("gremlins not found on <host>"), explicitly distinguished from exit 1 with a
  report (tolerated) and from a genuinely empty package (UnmeasuredMissing).

## FLAG

- [antipatterns] t-4's declared files cannot contain its own change. "One Launcher struct
  (Prefix + *remote.Target) **embedded** by Gremlins, Stryker and StrykerNet" moves `Prefix`
  down a level, and Go keyed composite literals do not promote — every
  `&Stryker{Prefix: …}` stops compiling. There are five such sites outside t-4's file list:
  `internal/cmd/verify.go:247,249,253`, `internal/cmd/survivor_drain.go:140`,
  `internal/mutation/gremlins_test.go:515,617`, `internal/mutation/stryker_test.go:399`,
  `internal/mutation/stryker_net_test.go:342`. `go test ./...` is the phase's gate, so t-4
  as scoped has no green commit available; verify.go and survivor_drain.go are t-7's files,
  a wave later.

  The contract's "keep passing unmodified" claim does survive — the three tests it names
  (`&Stryker{ProjectRoot: …}`, `&Gremlins{}`, `&StrykerNet{OutputDir: "-rf"}`) never key
  `Prefix`. It is the other five sites that break.

  Suggestion: either add the five files to t-4, or make Launcher a named field rather than
  an embedded one so the existing literals keep compiling. Decide now, not at the compile
  error.

- [test contract] Prefix-and-Target mutual exclusion is pinned only where the user will not
  meet it. t-4 asserts the refusal inside the launcher ("no *exec.Cmd is built"), but t-7 is
  the site that actually produces both: `survivor_drain.go:140` and `verify.go:247` build
  adapters with `dockerPrefix(p)` unconditionally, and t-7 adds the grant on top. t-7's
  abort contract enumerates probe failure and the two tracked-config refusals but not this
  one, so a docker-runtime repo with a remote grant fails deep inside the first adapter Run,
  mid-verify, rather than as a pre-flight refusal.
  Suggestion: add a t-7 contract line — a grant plus a non-empty docker prefix aborts
  `dross verify` before any adapter Run, naming both.

- [test contract] The two new *settable* keys have no doc gate. t-2 adds `mutation_workers`
  and `mutation_test_cpu` to `localKeys`; t-10's doc assertion names only
  `"mutation remote grant"`, `"mutation_remote_host"` and `".dross/local.toml"`.
  `TestDocsCoverAllowHosts` (`internal/cmd/local_test.go:242-243`) is a hand-listed string
  table, not derived from `localKeys`, so nothing goes red when a documented-nowhere key
  becomes settable. The keys the user is expected to *type* are the ones most needing docs.
  Suggestion: extend t-10's doc contract to include both key names.

- [test contract] c-6's "the tree that reaches the remote is the working tree under test"
  is asserted structurally but not by origin. t-1 pins "a trailing-slash source" and t-4
  pins that the push happens exactly once before the first remote command; no line asserts
  the source path is the same root the adapter was told to measure (`ProjectRoot` / repo
  root). An rsync from the wrong local directory satisfies every listed assertion.
  Suggestion: one t-4 line — the rsync source equals the adapter's ProjectRoot, asserted
  as a value, not a shape.

- [granularity] t-4 is now the plan's clearest split candidate: 5 files, 14 contract lines,
  3 criteria, and it grew rather than shrank in this amendment. Pass 1 said leave it because
  splitting would produce "a seam nobody uses"; that reasoning is weaker now that the
  per-adapter report lifecycle is a closed table with its own failure semantics. A clean cut
  exists: (a) Launcher + embedding + one-shot push + Prefix/Target exclusivity + the
  remote-derived worker default; (b) the adapter→remote-report-path table, the rm-run-fetch
  ordering, and the per-adapter local read paths.
  Suggestion: optional, but decide deliberately — with the FLAG above, t-4 is already
  carrying a compile-breaking refactor plus three adapters' report lifecycles in one commit.

## NOTE

- [coverage] All seven criteria appear in a `covers` field: c-1 (t-4, t-7), c-2 (t-2, t-3,
  t-10), c-3 (t-2, t-4, t-7), c-4 (t-1, t-6, t-7, t-8, t-9), c-5 (t-5), c-6 (t-1, t-4),
  c-7 (t-8). All `depends_on` ids exist.

- [coverage] Pass 1's BLOCKING is genuinely closed, by mechanism and not by wording. The
  rm is now a launcher property with an adapter-supplied path table (t-4), closed against a
  fourth adapter, asserted for Stryker's `reports/mutation/mutation.json` and StrykerNet's
  `StrykerOutput` tree, and the `findReport`-returns-a-prior-run failure mode has its own
  contract line. The per-adapter *local* read paths (`GremlinsReportPath`, `reportPath()`,
  `findReport(outDir)`) are pinned to resolve after the fetch, which closes the third pass-1
  FLAG at the same seam. The other three pass-1 FLAGs are closed too: t-1 line 5 now states
  the audit accommodation with the `file:binExpr` + snippets.txt row shape (matching
  `subprocargs_audit_test.go:112,154,405`), t-4/t-8 pin the joined remote workdir, and t-3
  now names keys that exist.

- [locked decisions] No task contradicts a locked decision. The `sync_mechanism` rationale
  was rewritten to say `--delete` does *not* supply c-6 and to warn against removing the rm
  on that belief — a locked decision that documents its own regression mode is rare and
  worth keeping in that form.

- [forbidden actions] Nothing violates `.dross/rules.toml`; no global rules file exists at
  `~/.claude/dross/rules.toml`. r-01 (`make install` before relying on a change) applies at
  execute time, and applies with force to t-5 and t-7 given the 2026-06-20 stale-binary
  divergence.

- [antipatterns] Task ids are out of file order (t-1, t-2, t-10, t-3, …) because t-10 took
  the high-water id when t-2 was split. Harmless — waves are declared, not positional — but
  `dross task list` output will not read top-to-bottom.

- [strengths] The failure taxonomy is read from exit codes rather than stderr prose, and the
  255-vs-1 distinction is called out as "the whole test" — getting that backwards makes
  every real gremlins run fatal or every transport failure clean.

- [strengths] The negative assertions sit at the right seam: the fake `t.Fatal()`ing on
  `"gremlins"/"npx"/"dotnet"/"go"` at argv[0] is a mechanical encoding of c-1's "spawns no
  compile or test process", and t-6 repeats it on the failure paths so "no local
  degradation" is proven rather than asserted.

- [strengths] t-7's "field-by-field equal at both sites" is the right shape for c-3 — it is
  the assertion that stops the survivor-drain construction site rotting away from verify's,
  which is how a knob ends up honoured in one place only.

## Summary
The pass-1 blocker is properly closed, but the plan ships two structural defects of its own:
its wave graph violates dross's own strictly-greater-wave invariant, and c-4's "missing
tool" case is unpinned in exactly the direction where today's ExitError tolerance turns an
uninstalled remote toolchain into a clean-looking score.
