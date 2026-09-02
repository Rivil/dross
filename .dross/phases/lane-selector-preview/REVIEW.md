# Plan Review — lane-selector-preview

Reviewed: 2026-09-01
Plan: 7 tasks across 3 waves

## BLOCKING

- [locked-decision-conflict] t-5's first contract line makes "the probe" one of the routes that must not survive, and installs `remoteProbeFn` as a recorder for `TestPreviewSpawnsNothing`. Locked decision `locality_probe` says preview **probes by default** — which t-6 then implements. So the invariant t-5 pins becomes false in wave 3. Worse, t-6's `files` are `internal/cmd/lane_preview.go` and `internal/cmd/lane_preview_consent_test.go`; `lane_preview_test.go`, where that test lives, is not in its set. t-6 therefore has to either edit a file outside its declared scope or leave probing off by default, which violates the lock.
  Suggestion: restate t-5's invariant as *spawn*-only — zero `spawnLocal`, zero `spawnRemote`, no rsync in the `runLog` — and drop "the probe" from both the premise and the recorder set. (`runOneLane`/`runLanePrepare`/`syncTreeTo` are the real seams; a `command -v` round-trip is not a spawn of the lane's work.) Alternatively add `internal/cmd/lane_preview_test.go` to t-6's files and say there that the test is amended.

## FLAG

- [coverage] c-5 requires `--json` to carry "consent state and locality". t-5 is the only task covering c-5, and its own description says it fills "every field except consent/locality". t-6 fills them and pins `TestJSONCarriesConsentAndLocality`, but covers only c-3 and c-6. c-5 is therefore unsatisfiable at t-5's commit and never claimed by the task that completes it.
  Suggestion: add `c-5` to t-6's `covers`.

- [criteria-fidelity] c-1's literal invocation is `dross test lane preview --files a.go b.go`. t-5 declares `cobra.NoArgs` with a repeatable `--files`. `--files` is `StringArrayVar` on the existing gate (`internal/cmd/test.go:331`), so `b.go` parses as a positional and `NoArgs` rejects the command. Locked `preview_invocation` forbids a *lane* positional only — `NoArgs` is stricter than the lock requires.
  Suggestion: either take trailing positionals as additional file paths (`cobra.ArbitraryArgs`, still no lane positional), or record explicitly that c-1's example is shorthand for repeated `--files`, so verification does not run the criterion's literal string and score it red.

- [coverage-depth] t-1 covers c-2, one of whose four named outcomes is "a path matching no lane". Its five contract lines pin `Dropped`, `ScopedToNothing`, `OutOfTree` and `FenceErr` — nothing pins that `sel.Unmatched` is carried on the plan struct. The gate prints it inline today (`internal/cmd/test.go:401`), so lifting resolution without lifting `Unmatched` is a plausible slip that leaves t-5 re-deriving it.
  Suggestion: add a contract line pinning `Unmatched` on the returned plan, or drop c-2 from t-1's `covers` and let t-5 own it alone.

- [wave-order] t-7 (wave 3) documents `--no-probe`, which t-6 (also wave 3) introduces. `depends_on = ["t-2", "t-5"]` does not include t-6, so the two run in parallel and t-7 can commit a README row describing a flag that does not exist yet — and `TestReadmeDocumentsThePreviewVerb` asserts the row names `--no-probe`, so the docs test passes while the flag is absent.
  Suggestion: add `t-6` to t-7's `depends_on`, or move the `--no-probe` sentence into t-6 and leave t-7 the rest of the row.

- [divergence-risk] t-4's `previewHost` composes `readRemoteGrants` then `preflightRemote`. The run does not: it goes through `resolveTestTarget` → `selectRemoteTarget` (`internal/cmd/remote_pool.go:22`), which walks a multi-grant pool in preference order and takes the first that answers. With two grants where the first is down, the run executes on the second while preview reports the first as `unresolved` — a wrong answer to c-6's "where would each lane run". (Composing afresh is otherwise justified: `resolveTestTarget` drops the host name on a transport fallback, `internal/cmd/test.go:897-907`, so preview genuinely cannot reuse it.)
  Suggestion: have `previewHost` walk the pool the way `selectRemoteTarget` does — or call it — and pin it with a two-grant contract line where the first host is unreachable.

- [locality-rendering] t-4 reuses `laneLocality` with `host=""` for the unprobed/unresolved case. With `host=""` and an empty missing set, `oneLaneLocality` (`internal/cmd/lane_locality.go:120-138`) returns `siteRefused` for any lane whose tool is absent from *this* machine, with the host unnamed. Rendered under `State=="unresolved"`, that shows a lane as refused on the strength of a probe that never ran — the same over-claim c-6 forbids for `local`. t-4's contracts pin the unresolved *host* state and the both-machines-missing case, but not the per-lane rendering when the host was never reached.
  Suggestion: add a contract line for a lane whose tool is missing locally while the host is unresolved, fixing what preview prints for it.

- [granularity] t-5 is the heaviest task by some margin: new subcommand, three flags, the bare working-tree default wiring, the whole findings rendering, and the complete `previewReport` JSON payload — nine contract lines across three concerns. It does not trip the 5-file/3-layer rule, so this is judgment rather than a rule hit, but a split at human-rendering vs `--json` would also give t-6 a declared place to land its JSON field additions (see the BLOCKING finding's file-scope problem).
  Suggestion: consider splitting `--json` into its own wave-2 task depending on the rendering task; if not, at least widen t-6's `files`.

## NOTE

- [antipattern] Every symbol the plan names checks out where it claims: `printLaneTemplate` (`internal/cmd/test_lane.go:683`, reached only from `lane add`/`lane edit` today, exactly as t-2 says), `porcelainPaths`/`unquotePath` (`internal/cmd/cleantree.go:64`/`:77` — and `porcelainPaths` does return both sides of a rename, so t-3's `TestRenameContributesOnlyTheDestination` premise is real), `laneLocality` (`internal/cmd/lane_locality.go:99`), `readRemoteGrants` (`internal/cmd/local.go:549`), `remoteProbeFn`, `preflightRemote`, `exitBadFileSet = 2`, `exitToolchainMissing = 8`, and `jsonFlagUsage`'s exact "instead of toml" wording (`internal/cmd/jsonout.go:29`). One name is off: t-7's contract says "located per-row via readmeRow" — the helper is `readmeRowContaining` (`internal/cmd/lane_install_docs_test.go:117`).

- [granularity] t-2 trips the merge-candidate heuristic — one source file, two `Printf` calls into an already-shared renderer. I would not merge it: it owns c-4 outright, it is the only wave-1 task touching `test_lane.go`, and folding it into t-7 would mix code with documentation.

- [antipattern] No file collides inside a wave. t-2 and t-5 both edit `internal/cmd/test_lane.go` but sit in waves 1 and 2; t-6 and t-7 are wave-3 parallel over disjoint files. t-7's description says outright that one task owns the README row so two parallel edits cannot collide on it — the right instinct, and the reason the collision does not exist.

- [strengths] t-1's extraction-first shape is the correct answer to c-1. One derivation site is what makes "preview and gate cannot diverge" a structural property rather than a promise, and contract line 6 re-pins six named existing gate tests plus exit 2 and exit 5, so the refactor cannot quietly move gate behaviour under cover of "no behaviour change".

- [strengths] The test contracts are written as falsification premises — "if X changes, test Y fails: <specific assertion with concrete values>" — throughout. None of them is "tests pass" or "covered by integration". `TestPreviewNamesEveryNonRunningOutcome` insisting on ONE invocation carrying all four outcomes, and `TestJSONCarriesConsentAndLocality` rejecting a payload that emits `"consent": "ok"`, are both the kind of contract that actually constrains an implementation.

- [strengths] The wave graph is honest rather than padded. t-4 genuinely needs nothing from t-1 — `laneLocality` already takes `[]matchedLane`, and `matchedLane` stays in package `cmd` whichever file t-1 leaves it in — so its wave-1 placement is real parallelism. t-5's `depends_on = ["t-1", "t-3"]` and t-6's `["t-4", "t-5"]` are both strict output dependencies.

- [forbidden-actions] No violation. `.dross/rules.toml` carries one rule (r-01: `make install` before relying on a prompt or Go change), which constrains execution hygiene rather than the plan's shape; no global `~/.claude/dross/rules.toml` exists. `runtime.mode = "native"`, so nothing here implies a wrong-runtime invocation.

## Summary
Structurally strong plan with genuinely falsifiable contracts and an honest wave graph; one blocking file-scope/lock conflict around the probe assertion, plus a cluster of coverage and host-resolution gaps that will each produce a wrong or unverifiable result if left as written.
