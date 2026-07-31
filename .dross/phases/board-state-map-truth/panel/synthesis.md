# synthesis — board-state-map-truth

Judged cold: none of the three drafts is mine. Every file path cited below was
checked against the tree before scoring.

## Scores

Scored 1–5. Ground truth used: `grep -c 'json:"'` over the six schema files
returns 0 everywhere (~189 `toml:` tags); `internal/cmd/enum_divergence_test.go`
already uses `go/parser` + `scanFor` over `internal/forge/*.go`; twelve commands
named `show` exist in `internal/cmd`, against the nine c-5 names.

| draft | dimension | score | one-line judgment |
|---|---|---|---|
| risk | criteria coverage | 4 | 7/7 owned, but README is never updated and the c-5 gate over-scopes to all twelve `show` commands, including `rule`/`interaction` which c-5 does not name. |
| risk | test-contract specificity | 5 | Sharpest failure-mode pinning in the panel: byte-offset ordering in ship.md, "project.toml is byte-identical afterwards", "the stub board server records zero requests", "nil []string emits [] not null". |
| risk | granularity | 3 | 12 tasks; splitting `--json` three ways (t-8/t-9/t-10) is a real seam only for t-10, so t-8/t-9 read as one mechanical sweep cut in half. |
| risk | wave correctness | 4 | Correctly frees `stats --json` from the marshaller dependency and correctly makes the c-2 gate wait on the ship.md edit; 3 waves is honest. |
| mvp | criteria coverage | 2 | 7/7 on paper, but c-5 is unmet: t-2 "marshals the same struct the TOML encoder receives" with zero `json:` tags in the tree emits `GitMainBranch`, so its own `.runtime.test_command` contract cannot pass. The whole key-naming problem is invisible to this draft. |
| mvp | test-contract specificity | 3 | Directionally good and occasionally the best phrasing in the panel (the incomplete-root case names `dross init` / `dross onboard`), but several contracts assert a difference rather than the value — "returns a different command-count total" is weaker than naming which event drops out. |
| mvp | granularity | 2 | 6 tasks, two of them over-merged: c-1+c-3 into t-1 (two different failure modes, one commit) and c-2+c-6 into t-5 (the guard ships in the same commit as the thing it guards). |
| mvp | wave correctness | 3 | 2 waves is defensible, but merging the c-2 gate into the c-6 edit removes the one ordering constraint that makes `dead_map_keys` observably satisfiable. |
| verification | criteria coverage | 5 | Only draft that sees the json-tag gap and verified it by grep; only draft that carries the c-5 exempt list and the README rows. |
| verification | test-contract specificity | 5 | Anchored to symbols that exist: `resolveJiraState("planned", nil)` → `("To Do", true)` matches `defaultJiraStateMap` exactly as written at internal/forge/jira.go:278. |
| verification | granularity | 4 | 11 tasks split by payload shape, not file count — the correct seam for c-5; t-6's six files is a knowing overrun and is argued, not hidden. |
| verification | wave correctness | 5 | 4 waves, and the one non-obvious ordering is right and explained: t-9 waits on t-5 because `shipped`/`complete` are not emitted statuses until ship.md names them. |

**Skeleton: `verification`.** It is the only draft that noticed the phase's
largest hidden cost — nine `--json` surfaces over structs with no `json:` tags —
and its wave graph encodes the one dependency that makes the locked
`dead_map_keys` decision testable. risk supplies better individual contracts and
a better c-2 mechanism; both are grafted below. mvp is the runner-up only for
phrasing, and its c-5 approach is not viable as written.

## Merged plan

11 tasks across 4 waves. Origin tags: `[verification]` = skeleton task kept,
`[+risk]` / `[+mvp]` = contract or file grafted in from that draft.

```
Wave 1

  t-1  Define lifecycle set, rename planning to planned
       origin:   [verification] +risk +mvp
       files:    internal/configenum/configenum.go, internal/cmd/issue.go,
                 internal/forge/jira.go, internal/forge/youtrack.go,
                 internal/cmd/issue_test.go, internal/forge/jira_test.go
       covers:   c-1
       desc:     Add configenum.LifecycleStatuses (planned, in-progress,
                 verifying, shipped, complete — no default, so the empty string
                 is rejected). statusPlanning's value becomes "planned";
                 issue.go's constants and both forge default maps key off the
                 Set. Add statusShipped / statusComplete consts for t-5's
                 vocabulary.
       contract: - phase-sync on a phase whose plan.toml has every task
                   status=pending records a POST to /issue/<key>/transitions on
                   the mock Jira server; if derivePhaseStatus emits anything but
                   "planned", zero transition requests are recorded and the test
                   fails. [verification]
                 - the same run's captured stderr contains no
                   "has no Jira status mapping" warning. [verification]
                 - the label PUT carries `dross/status:planned`, not
                   `dross/status:planning`. [+risk]
                 - resolveJiraState("planned", nil) returns ("To Do", true) and
                   resolveJiraState("planning", nil) returns ok=false;
                   resolveYouTrackState("planned", nil) returns ("Open", true).
                   [verification]
                 - LifecycleStatuses.Has("planning") is false and Has("") is
                   false — newSet's def argument must be empty, or every
                   downstream validator silently accepts a blank status. [+risk]
                 - every lifecycle literal declared in issue.go is a
                   configenum.LifecycleStatuses member — retyping
                   statusVerifying as "verify" fails the membership assertion.
                   [verification]

  t-2  Mirror toml tags as json tags across schemas
       origin:   [verification]
       files:    internal/project/project.go, internal/milestone/milestone.go,
                 internal/defaults/defaults.go, internal/profile/profile.go,
                 internal/stack/profile.go, internal/phase/phase.go,
                 internal/cmd/json_tag_parity_test.go
       covers:   c-5
       depends:  —
       desc:     Every struct field carrying `toml:"x"` gains `json:"x"` with the
                 same name and omitempty. Confirmed necessary: the six files
                 carry ~189 toml tags and zero json tags, so --json without this
                 emits Go field names where the document says test_contract.
       contract: - a reflection walk over project.Project, milestone.Milestone,
                   defaults.Defaults, profile.Profile, stack.Profile, phase.Spec,
                   phase.Plan and phase.Task asserts each toml-tagged field has
                   an identical json tag; adding phase.Task.Retries with only a
                   toml tag fails it, naming the struct and field. [verification]
                 - json.Marshal(phase.Task{TestContract: []string{"x"}}) contains
                   the key "test_contract" and not "TestContract". [verification]
                 - a toml:"x,omitempty" field left zero is absent from the JSON,
                   and a non-omitempty zero field is present. [+risk]
                 - a toml:"-" field never appears. [+risk]

Wave 2 (depends wave 1)

  t-3  Reject out-of-set --status on phase-sync
       origin:   [verification] +risk +mvp
       files:    internal/cmd/issue.go, internal/cmd/issue_test.go
       covers:   c-3
       depends:  t-1
       desc:     issuePhaseSync validates --status against
                 configenum.LifecycleStatuses in RunE, before openBoard, and the
                 flag help is derived from the Set.
       contract: - `dross issue phase-sync 01-auth --status planning` returns an
                   error whose message contains
                   "planned | in-progress | verifying | shipped | complete", and
                   the mock board server records zero HTTP requests.
                   [verification+risk]
                 - the same rejection fires with [board].enabled = false: the
                   check runs ahead of the enabled short-circuit, so a bad
                   --status can never exit 0 as a silent no-op. [verification]
                 - `--status " Verifying"` is accepted and syncs as `verifying` —
                   validation goes through configenum.Normalize, so it is exactly
                   as forgiving as the map lookup. [+risk]
                 - `--status shipped` and `--status complete` return nil — the
                   two statuses t-5 starts emitting must not be rejected.
                   [verification]
                 - omitting `--status` still derives from the plan and does not
                   error. [+risk]

  t-4  Gate state_map keys in project set and doctor
       origin:   [verification] +risk +mvp
       files:    internal/cmd/project.go, internal/cmd/doctor.go,
                 internal/cmd/project_test.go, internal/cmd/doctor_test.go
       covers:   c-4
       depends:  t-1
       desc:     writeDotted's state_map branch rejects a key outside
                 LifecycleStatuses (storing the normalized form when it is
                 valid); doctor's [board] block reports an on-disk bad key as an
                 issue counting toward the non-zero exit, per the locked
                 state_map_key_severity.
       contract: - `dross project set board.state_map.planning "To Do"` errors
                   naming the five valid keys, and project.toml is byte-identical
                   to its pre-call contents — reject before write.
                   [verification+risk]
                 - `dross project set board.state_map.verifying "In Review"` still
                   writes the entry. [verification]
                 - `board.state_map.Planned` stores under the key `planned`, so
                   the sync-time lookup can find it — a near-miss is fixed, not
                   failed. [+risk]
                 - `dross project set --unset board.state_map.planning` still
                   deletes an existing bad key, and `dross project get
                   board.state_map.planning` still reads it — otherwise doctor
                   reports a fault the CLI cannot repair. [verification, +risk
                   for the `get` half]
                 - doctor over a project.toml carrying
                   `[board.state_map] planning = "To Do"` prints a ✗ line naming
                   board.state_map.planning and returns a non-nil error; the
                   warning tally is unchanged; the same fixture with only valid
                   keys returns nil. [verification, +risk for the tally]

  t-5  Emit terminal board statuses from ship
       origin:   [verification] +risk +mvp
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go
       covers:   c-6
       depends:  t-1
       desc:     ship.md §6 emits `--status shipped` on merge and
                 `--status complete --close` after finalize, replacing the bare
                 `--close`. `dross phase complete` gains no board call (locked
                 terminal_emit_sites). Note for execute: rule r-01 — `make
                 install` before relying on the edited prompt.
       contract: - ship.md contains "phase-sync <phase-id> --status shipped" at
                   an index after the squash-merge bullet and before
                   "dross phase complete"; "--status complete --close" appears
                   after "dross phase complete". Swapping the two fails the index
                   comparison, not just a substring check. [verification+risk]
                 - ship.md contains no `phase-sync <phase-id> --close` occurrence
                   lacking a --status — the pattern that produced the dead
                   terminal map entries. [verification+risk]
                 - no line in ship.md pairs `dross phase complete` with a
                   `phase-sync` call. [verification]

  t-6  Add --json helper and config-document shows
       origin:   [verification] +mvp
       files:    internal/cmd/jsonout.go, internal/cmd/project.go,
                 internal/cmd/milestone.go, internal/cmd/defaults.go,
                 internal/cmd/profile.go, internal/cmd/stack.go,
                 internal/cmd/json_show_test.go
       covers:   c-5
       depends:  t-2
       desc:     New emitJSON(v any) helper marshals the bare document. project,
                 milestone, defaults, profile and stack `show` gain --json, which
                 suppresses the `# <path>` header line (locked json_shape).
       contract: - `dross project show --json` output's first non-space byte is
                   `{`, contains no line starting with `#`, and unmarshals to an
                   object whose repo.git_main_branch equals what the toml encoder
                   printed for the same fixture. [verification+risk]
                 - `.runtime.test_command` in the payload equals what
                   `dross project get runtime.test_command` prints. [+mvp]
                 - `dross milestone show v1.1 --json` and `dross defaults show
                   --json` likewise emit no `#` header line. [verification]
                 - `milestone show --json` with no argument and no
                   state.current_milestone still fails with the existing
                   "no version given …" message, not an empty JSON doc. [+risk]
                 - `dross profile show --scope global --json` emits the global
                   profile, not the merged one, and `--scope project --json`
                   differs from it — --scope and --json compose.
                   [verification+mvp]
                 - `dross stack show nope --json` still fails with
                   `stack profile "nope" not found` instead of printing "null".
                   [verification+mvp]

Wave 3 (depends wave 2)

  t-7  Add --json to phase and stats shows
       origin:   [verification] +risk
       files:    internal/cmd/phase.go, internal/cmd/stats.go,
                 internal/cmd/json_show_test.go
       covers:   c-5
       depends:  t-6
       desc:     phase show --json emits {"spec":…,"plan":…} via phase.LoadSpec /
                 LoadPlan, null for a missing file. stats show's aggregation moves
                 into a statsSummary struct the renderers read, so --json emits
                 the same numbers the table prints.
       contract: - `dross phase show 01-auth --json` unmarshals to an object with
                   exactly the keys "spec" and "plan"; with plan.toml deleted,
                   "plan" is JSON null — present, not omitted, not {} — and
                   "spec" is still populated. [verification+risk]
                 - `dross stats show --json` carries the same top-command counts
                   and error buckets the table renders, compared bucket by bucket
                   against the parsed table output, including the `other` bucket's
                   err_detail tail strings — dropping a bucket from the payload
                   while it still renders fails. [verification+risk]
                 - `dross stats show --since 7d --json` omits an event stamped 30
                   days ago that `--since` filters out of the table.
                   [verification]
                 - empty telemetry emits an empty-but-valid JSON document, not the
                   "(no telemetry events at …)" sentence. [+risk]

  t-8  Add --json to task and changes shows
       origin:   [verification] +mvp
       files:    internal/cmd/task.go, internal/cmd/changes.go,
                 internal/cmd/json_show_test.go
       covers:   c-5
       depends:  t-6
       desc:     task show --json marshals the phase.Task record; changes show
                 accepts --json for symmetry (it already emits JSON at
                 internal/cmd/changes.go:93), matching how state show already
                 accepts the flag.
       contract: - `dross task show 01-auth t-1 --json` unmarshals to an object
                   carrying every field the aligned rendering prints — id, title,
                   wave, status, files, covers, depends_on, test_contract,
                   description — each compared against the plan.toml task; a
                   field present in the text rendering and absent from the payload
                   fails. [verification+mvp]
                 - task show --json on a task with empty status emits "pending",
                   matching orPending in the text path rather than "".
                   [verification]
                 - `dross task show <phase> t-99 --json` still errors
                   "task not found: t-99" and prints no JSON. [+risk]
                 - `dross changes show 01-auth --json` is byte-identical to
                   `dross changes show 01-auth`. [verification+risk]
                 - `changes show --json` on a phase with no changes.json still
                   emits the empty record, not an error. [+mvp]

  t-9  Pin lifecycle/state-map bidirectional divergence
       origin:   [verification] skeleton, [risk] mechanism
       files:    internal/cmd/board_lifecycle_divergence_test.go
       covers:   c-2
       depends:  t-1, t-5
       desc:     Test-only. Parses defaultJiraStateMap and defaultYouTrackStateMap
                 out of internal/forge source with go/parser — the pattern
                 internal/cmd/enum_divergence_test.go already uses — rather than
                 exporting them, and scans assets/prompts/*.md for
                 `--status <literal>`. Gates both directions against
                 configenum.LifecycleStatuses.
       contract: - the test collects the statuses dross emits — every
                   `--status <literal>` in assets/prompts/*.md plus issue.go's
                   lifecycle constants — and asserts the set equals
                   LifecycleStatuses; adding `--status blocked` to execute.md
                   fails it, naming the prompt file and the literal.
                   [verification+risk]
                 - deleting the "shipped" key from the Jira map fails ("a status
                   dross emits has no state-map entry") and adding a "paused" key
                   fails ("a state map keys on a status nothing emits").
                   [verification]
                 - the two provider maps are checked independently, so a key
                   present in one and missing from the other is caught rather
                   than averaged away. [+risk]
                 - reverting t-5's ship.md edit fails this test, because shipped
                   and complete stop being emitted while both maps still key
                   them. [verification]

  t-10 Pin the five telemetry friction cases
       origin:   [verification] +risk +mvp
       files:    internal/cmd/friction_window_test.go
       covers:   c-7
       depends:  t-1, t-3, t-4
       desc:     Test-only. One subtest per friction case recorded
                 2026-07-15..2026-07-28, each asserting the message a user now
                 gets.
       contract: - incomplete root: with .dross/state.json removed, `dross task
                   list` fails with a message containing ".dross/state.json" and
                   cmd.RepairHint, naming `dross init` / `dross onboard` — not a
                   bare stat/parse error. [verification+risk+mvp]
                 - `dross task list <phase>` on a phase with a plan exits nil and
                   prints the task ids — it must never reach the
                   unknown-subcommand path that produced the telemetry.
                   [verification+mvp]
                 - `dross task done 01-auth t-1` errors with a message containing
                   "dross task status <phase-id> <task-id> done".
                   [verification+risk+mvp]
                 - `dross milestone set v1.1 milestone.status active` succeeds and
                   writes status = "active"; an invalid value errors naming
                   "planning | active | complete" — no unknown_field error.
                   [verification+risk+mvp]
                 - doctor accepts every member of configenum.BoardProviders as
                   [board].provider without a ✗ line for the provider field, and
                   with auth_env exported and a valid base_url prints
                   "✓ [board] is well-formed". [+risk broadened from
                   verification's single jira case]
                 - unmapped board state: phase-sync on a locked-but-unstarted plan
                   against the mock Jira server records a transition request and
                   prints no "no Jira status mapping" warning. [verification+risk]

Wave 4 (depends wave 3)

  t-11 Gate every structured show for --json
       origin:   [verification] +risk
       files:    internal/cmd/json_show_test.go, README.md
       covers:   c-5
       depends:  t-6, t-7, t-8
       desc:     Walk the built command tree for every `show` subcommand and
                 require a registered `json` bool flag, with a documented exempt
                 list. Twelve `show` commands exist against c-5's nine: state show
                 already carries the flag, rule show and interaction show are the
                 exemptions. README's command-table rows for the nine mention
                 --json.
       contract: - the tree walk fails when a `show` subcommand has no `json`
                   flag and is not in the exempt list — deleting the flag
                   registration from stack show fails it by name, and a future
                   `dross findings show` landing without --json fails it too.
                   [verification+risk]
                 - the exempt list is asserted to be exactly {rule, interaction}:
                   adding a tenth structured show to the exempt list instead of
                   giving it a flag fails. [verification, tightened]
                 - for each command the walk finds, invoking it with --json
                   against the fixture repo produces output json.Valid accepts —
                   catching a flag registered but not wired. [verification]
                 - no output from those invocations contains a line starting `# `
                   (locked json_shape: bare document, no envelope). [+risk]
```

Coverage: c-1 → t-1; c-2 → t-9 (enabled by t-1 + t-5); c-3 → t-3; c-4 → t-4;
c-5 → t-2, t-6, t-7, t-8, t-11; c-6 → t-5; c-7 → t-10. 7/7.

## Disagreements

Eight genuine divergences. Each carries a provisional default for the merged
plan above; none is silently resolved.

**D1 — How `--json` gets document-shaped keys.**
verification adds `json:"x"` tags mirroring every `toml:"x"` across six schema
files plus a reflection parity gate (t-2). risk rejects that and builds a new
leaf package `internal/tomljson` whose `Marshal` reads the existing toml tags, so
no second tag set is ever needed (t-3). mvp has no mechanism at all — its helper
"marshals the same struct the TOML encoder receives", which with the tree's
actual zero json tags would emit `GitMainBranch`, so its own
`.runtime.test_command` contract cannot pass.
*Provisional default: verification's tags + parity gate.* It uses stdlib
`encoding/json`, so nil-slice, nested-map and omitempty semantics are the ones
every Go reader already predicts; risk's own judgment note concedes a hand-rolled
reflection encoder "has its own edge cases". *Why it matters:* this is the
largest mechanical surface in the phase and a one-way door for the other four
c-5 tasks. risk's counter is real and unresolved — tags can drift and are caught
only by t-2's test, whereas `tomljson` is drift-free by construction. If the
executor prefers construction over a gate, swapping t-2 for risk's t-3 changes no
other task's shape, only t-6's import.

**D2 — Name of the Set: `LifecycleStatuses` vs `BoardStatuses`.**
risk and mvp say `configenum.LifecycleStatuses`; the skeleton (verification) says
`BoardStatuses`.
*Provisional default: `LifecycleStatuses` — the skeleton loses this one.*
Two of three drafts, and the spec's own language is "lifecycle" throughout: the
locked key is `lifecycle_vocabulary`, c-2 says "board lifecycle statuses", c-6
says "a terminal lifecycle state". *Why it matters:* the name appears in the
user-visible error message that c-3 and c-4 both assert on, and in every
downstream import — cheap now, noisy to change after wave 2.

**D3 — Is `--status` validation its own task?**
risk (t-5) and verification (t-3) give c-3 a separate task; mvp folds it into t-1
because it is "the same edit surface".
*Provisional default: separate task (t-3).* Different failure mode —
unvalidated CLI input, not vocabulary drift — and it keeps wave 1's t-1 a leaf
edit that two wave-2 tasks can depend on cleanly. *Why it matters:* mvp's
objection is not empty — separating costs a wave boundary and puts two tasks in
`internal/cmd/issue.go`. The repo executes tasks sequentially with one commit
each, so the same-file overlap is a review concern, not a conflict risk.

**D4 — How the c-2 gate reads the forge state maps.**
verification's t-9 exports `defaultJiraStateMap` / `defaultYouTrackStateMap` (or
adds a `DefaultStateMap` accessor), editing `internal/forge/jira.go` and
`youtrack.go`. risk's t-7 parses them out of source with `go/parser`, test-only.
mvp leaves the mechanism unspecified.
*Provisional default: risk's AST scrape — grafted over the skeleton.* Verified
precedent: `internal/cmd/enum_divergence_test.go` already imports `go/ast` and
`go/parser` and scans `internal/forge/forge.go`, `jira.go` and `youtrack.go` for
exactly this class of guard. *Why it matters:* exporting is permanent API surface
widened for a test's convenience, and the two maps are `internal/forge` package
detail that `resolveJiraState` is the only legitimate reader of. It also drops
two source files from t-9, making it purely additive.

**D5 — Do the c-6 edit and the c-2 gate ship together?**
mvp merges them into one task (t-5: edit ship.md *and* write the divergence
test). risk and verification keep them separate, with the gate depending on the
edit.
*Provisional default: separate (t-5 edit, t-9 gate).* *Why it matters:* merged,
the guard lands in the same commit as the thing it guards, so it can never be
observed red-then-green — and the repo's execution rules are explicit that a gate
must be read before the gated work is queued. Separate also makes t-9 a genuine
regression guard on t-5 rather than a self-attestation.

**D6 — Does `phase show <unknown-id>` start erroring?**
risk's t-10 changes it: today the command prints two "(missing)" lines and exits
0, which under `--json` becomes `{"spec":null,"plan":null}` — indistinguishable
from a real phase with no artefacts. risk flags this itself as a behaviour change
not in any criterion. verification and mvp keep the current behaviour and simply
emit the nulls.
*Provisional default: keep the current exit-0 behaviour; `--json` emits
`{"spec":null,"plan":null}`.* c-5 asks the two modes to show the same data, and
they do — the nulls are the faithful JSON of "(missing)". *Why it matters:* this
is the one place a draft proposes changing behaviour no criterion asks for, and
the hard rule here is no new criteria. risk's argument is good product judgment
and belongs in a deferred item, not in this phase's diff. Flagging for the lead:
if you want it, it is a one-line addition to t-7's contract.

**D7 — Scope of the c-5 surface gate.**
risk's t-11 walks the tree and requires `--json` on *every* command named `show`,
with no exemptions. verification's t-11 carries a documented exempt list (rule
show, interaction show).
*Provisional default: verification's exempt list.* Confirmed against the tree:
twelve `show` commands exist — the nine c-5 names, plus `state show` (already has
the flag, at internal/cmd/state.go:47), `rule show` and `interaction show`.
risk's gate as written fails on day one against `rule show`. *Why it matters:* an
exempt list is also the thing that rots, so the merged t-11 asserts the list is
exactly `{rule, interaction}` — a future structured show cannot be silenced by
appending to it.

**D8 — Where `stats show --json` sits.**
risk puts it alone in wave 1, arguing its data is computed rather than read off
disk so it never touches the marshaller and gating it later is false
serialisation. verification pairs it with `phase show` in wave 3 behind the
helper; mvp groups it with the computed shows in wave 2.
*Provisional default: verification's t-7 pairing.* risk is right that stats needs
no json tags, but it does need t-6's `emitJSON` helper, so the wave-2 dependency
is real, not invented — and stats and phase share the only non-mechanical work in
c-5 (lifting the aggregation out of the renderers, assembling a new payload).
*Why it matters:* mostly scheduling. If the executor wants wave 1 wider, moving
stats up costs only an inlined marshal call that t-6 later replaces.

---

synthesis: 11 tasks across 4 waves, 8 disagreements
