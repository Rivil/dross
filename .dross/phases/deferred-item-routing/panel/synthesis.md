# Cold-judge synthesis — deferred-item-routing

Three drafts judged (risk / mvp / verification). I authored none. File paths
spot-checked against source: `phase.go` Deferred struct (`Text`+`Why`, optional
fields use `omitempty`), `milestone.go` (`dross milestone add … phases <slug>`
exists, idempotent), `validate.go` (iterates phases, accumulates "problems"; no
deferred check today), `cmd_test.go` (validate-passes harness exists). New files
(`deferred.go`, `spec_prompt_test.go`, `inbox_prompt_test.go`) don't exist yet but
match the live `*_prompt_test.go` convention — no downgrade.

## Scores

| draft        | criteria coverage | test-contract specificity | granularity | wave correctness |
| ------------ | ----------------- | ------------------------- | ----------- | ---------------- |
| risk         | strong            | strong                    | over-split  | strong           |
| mvp          | adequate          | adequate                  | weak        | adequate         |
| verification | strong            | strongest                 | strong      | strong           |

Per-dimension, one line each:

- Criteria coverage — **risk**: all 6 owned, one primary failure-mode owner per
  criterion (back-compat→t1, dangling→t2, leak→t3, …); **mvp**: all 6 owned but
  c-1/c-4/c-6 lumped into a single coarse task; **verification**: all 6 owned,
  derived test-first from each criterion.
- Test-contract specificity — **risk**: names the breaking edit + the test that
  fails for each task; **mvp**: contracts present but bundled four-to-a-task,
  less isolable; **verification**: an ideal failing test per criterion with exact
  assertions (JSON length+source, reload-and-assert Target) — the sharpest.
- Granularity — **risk**: clean except spec.md is split into two serialized
  tasks (defensible but extra); **mvp**: too coarse — schema (phase.go) + CLI
  (deferred.go) + main.go in one wave-1 task crosses two layers; **verification**:
  one layer per task (schema / CLI / spec-prompt / inbox-prompt).
- Wave correctness — **risk**: correct, 4 waves, serializes the two same-file
  spec.md edits; **mvp**: correct but the 2-layer wave-1 task weakens it;
  **verification**: correct, 3 waves, prompt tasks gated behind the CLI surface.

**Skeleton = verification.** It pairs the sharpest test contracts with the
cleanest one-layer-per-task split and correct 3-wave ordering. Grafts applied:
risk's dangling-target validate guard (a task verification/mvp lack), risk's
`omitempty` back-compat contract (fixes a real bug in verification's t-1 tag), and
mvp+risk's merge/serialization rationale informing the spec.md default.

## Merged plan

Phase **deferred-item-routing** — 5 tasks across 3 waves.

### Wave 1

**t-1 — Add optional `target` to the Deferred schema** [verification+mvp+risk]
- files: `internal/phase/phase.go`, `internal/phase/phase_test.go`, `internal/cmd/cmd_test.go`
- covers: c-1
- contract: add `Target string \`toml:"target,omitempty"\`` to the `Deferred`
  struct. A target-less entry loaded then `Save`d emits **no** `target =` key
  (omitempty back-compat — a spurious `target = ""` would rewrite every legacy
  spec); an entry stamped `target="foo-slug"` reads the slug back via
  `LoadSpec`. Drop the toml tag or omitempty and the phase round-trip test fails.
  Separately, a `cmd_test.go` validate case feeds one spec with `[[deferred]]`
  `target=<slug>` and a sibling that omits it; `dross validate` exits 0 for both.
- depends_on: —
- note: `omitempty` is risk's contract grafted over verification's bare
  `toml:"target"`, which would have broken back-compat.

### Wave 2

**t-2 — Validate guards against a dangling target** [risk]
- files: `internal/cmd/validate.go`, `internal/cmd/cmd_test.go`
- covers: c-1 (guard extension)
- contract: `dross validate` reports a problem when a `[[deferred]]` `target`
  names a slug that is neither an existing `phases/<slug>` dir nor any
  `milestone.phases` entry; it still exits 0 for a target-less entry and for a
  target naming a real slug. Fits validate.go's existing `problems` accumulation.
  Regress either the dangling check or the target-optional acceptance and the
  validate-deferred case in `cmd_test.go` fails.
- depends_on: t-1
- note: minority task — see Disagreement D1.

**t-3 — `dross deferred` list (+ route) command** [verification; list-half all three]
- files: `internal/cmd/deferred.go`, `internal/cmd/deferred_test.go`, `cmd/dross/main.go`
- covers: c-3 (stamp), c-4, c-5, c-6
- contract: over a fixture of `phases/*/spec.toml`, `deferred list --someday`
  prints only target-less rows; `--target <slug> --json` returns exactly the
  matching entries, each carrying a `source` field naming its originating phase;
  `--routed` is the exact complement of `--someday`; `--milestone <v>` scopes to
  that milestone's `phases` array. `deferred route <phase> <index> --target <slug>`
  stamps `target` on that phase's Nth `[[deferred]]` on disk — reload and assert
  `Target==<slug>` (round-trips via t-1's field). Register in `cmd/dross/main.go`.
  Drop any filter, the `source` field, or the route persistence and the matching
  `deferred_test.go` assertion fails.
- depends_on: t-1
- note: the `route` sub-command is verification's call — see Disagreement D2.

### Wave 3

**t-4 — spec.md §4 routing + create-flow re-surface seed** [verification+mvp; risk splits]
- files: `assets/prompts/spec.md`, `internal/cmd/spec_prompt_test.go`
- covers: c-2, c-3, c-4, c-5
- contract: §4 names all four destinations — pull-into-current-phase (move out of
  `[[deferred]]` into a new `[[criteria]]`), park-in-milestone-backlog,
  attach-to-named-future-phase, and someday/unrouted; the park branch chains
  `dross milestone add <v> phases <slug>` then `dross deferred route … --target
  <slug>`; "leave unrouted **only** on an explicit someday pick" is present. The
  create-flow (§0/§1) seeds candidate criteria via `dross deferred list --target
  <new-slug> --json` (CLI, not a prompt grep) and instructs skipping any deferred
  item that already carries a `target` (no duplicate routing / no duplicate
  candidates). Remove any destination phrase, the append+stamp sequence, the
  list-backed seed, or the skip-already-routed instruction and its sub-assertion
  in `spec_prompt_test.go` fails. (r-01: live only after `make install`; the test
  reads `assets/` source, so it passes pre-install.)
- depends_on: t-3
- note: kept as one task by default — see Disagreement D3.

**t-5 — inbox.md adds deferred someday items as a 2nd triage source** [verification+mvp+risk]
- files: `assets/prompts/inbox.md`, `internal/cmd/inbox_prompt_test.go`
- covers: c-6
- contract: inbox.md reads someday/unrouted items via `dross deferred list
  --someday --json` as a second triage source alongside `dross issue pull`
  (§1/§2), and routes each through the same new-phase / milestone-backlog /
  quick-task / dismiss funnel. Remove the deferred source line or any funnel
  destination and the matching substring assertion in `inbox_prompt_test.go`
  fails.
- depends_on: t-3

### Coverage check

| criterion | tasks                          |
| --------- | ------------------------------ |
| c-1       | t-1 (+ t-2 guard)              |
| c-2       | t-4                            |
| c-3       | t-3 (route stamp), t-4 (prompt)|
| c-4       | t-3 (list), t-4 (re-surface)   |
| c-5       | t-3 (filters), t-4 (dedup)     |
| c-6       | t-3 (--someday), t-5 (inbox)   |

All six owned. Waves: w1={t-1}; w2={t-2, t-3} (both depend only on t-1, mutually
independent); w3={t-4, t-5} (both depend on t-3, different files — no same-file race).

## Disagreements

**D1 — Should validate guard against a dangling `target`?**
- risk: YES, its own task (t-2). A target naming a non-existent slug is a silent
  re-surface failure — the parked item never comes back — which the risk lens
  must own; fits validate.go's existing artifact checks.
- mvp: NO. Explicitly rejects any validate.go change — the BurntSushi decoder
  accepts the new key once the field exists, so c-1 ("passes with/without target")
  is met by the field add plus a passes-test alone.
- verification: NO new logic. Adds only a validate-*passes* case to cmd_test.go;
  no dangling check (c-1 doesn't test for it).
- **Provisional default: INCLUDE as t-2 (risk).** The guard protects the locked
  `resurface_model` decision — a typo'd slug silently breaks the 1:1 re-surface,
  the exact partial-failure this phase exists to prevent — and it's cheap inside
  the existing `problems` loop.
- **Why it matters / how to undo:** it's the one place 2-of-3 lenses said "no" and
  it edges past c-1's literal text ("passes whether present or absent"). A
  reviewer who reads it as scope creep can drop t-2 wholesale; the validate-passes
  assertion still lives in t-1, so c-1 stays covered either way.

**D2 — Stamp `target` via a CLI primitive (`dross deferred route`) or prompt-side TOML edits?**
- verification: CLI. A Go mutation primitive is unit-testable (route → reload →
  assert `Target`), whereas a prompt hand-editing spec.toml is only assertable as
  prompt text; c-3 demands the on-disk entry actually reflect the destination.
- risk & mvp: prompt-orchestrated. The park/attach branches in spec.md write the
  `target` key directly; no `route` command. (mvp adds no route; risk's t-4 says
  the prompt "stamps target=<slug>".)
- **Provisional default: INCLUDE `route` in t-3 (verification).** c-3 asserts the
  `[[deferred]]` entry *reflects* the destination on disk — only a CLI primitive
  makes that Go-testable, and prompt-written TOML is the more error-prone path.
- **Why it matters / how to undo:** it's a 1-of-3 minority and adds a command not
  named in any locked decision (`deferred_list_contract` locks only `list`). If
  cut, t-4's park branch must instead instruct a literal TOML edit and c-3's
  on-disk half drops to a prompt-text-only assertion — weaker, but in-scope.

**D3 — One spec.md task or two (routing vs re-surface/dedup)?**
- risk: TWO, serialized (t-4 routing completeness c-2/c-3; t-5 dedup c-4/c-5,
  `depends_on` t-4) so two edits to the same file never race in parallel
  execution, and so "left unrouted by accident" vs "re-surfaced twice" are
  independently testable failure modes.
- mvp & verification: ONE. Both regions edit a single prompt file plus one
  prompt-test, well under the split threshold; splitting forces same-file
  serialization for little gain.
- **Provisional default: ONE task, t-4 (verification+mvp, 2-of-3).** The merged
  task already keeps the two concerns as separate sub-assertions in
  `spec_prompt_test.go`, preserving independent testability without paying for an
  extra serialized wave.
- **Why it matters / how to undo:** if executed in parallel mode and a planner
  wants zero same-file contention, re-split t-4 into t-4a (c-2/c-3) and t-4b
  (c-4/c-5, depends t-4a) — risk's exact shape — yielding 6 tasks / 4 waves.
