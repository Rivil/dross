# cli-surface-sweep — panel synthesis

Judge: authored none of the three drafts. Scored against spec.toml (9 criteria,
5 locked decisions), rules.toml (r-01), and the actual tree.

## Scores

Scale: A (strongest) / B / C. One line per draft per dimension.

| dimension | risk | mvp | verification |
|---|---|---|---|
| criteria coverage | A | A | A |
| test-contract specificity | A | B | A |
| granularity | A− | C+ | A |
| wave correctness | A− | B+ | A− |

**criteria coverage**
- risk — 9/9, every criterion mapped to a task that can actually satisfy it; the only draft with a telemetry-taxonomy contract (a hinted flag error must stay classified `unknown_flag`, not fall into `other`), which is real coverage of an adjacent regression no criterion names but the repo cares about.
- mvp — 9/9, but c-2's four named mis-reaches are all asserted inside one task; there is no place where all four are proven true simultaneously against the assembled tree.
- verification — 9/9 and the only draft that notices dross's own prompt corpus teaches a broken invocation (`assets/prompts/secure.md:42`), i.e. c-2's cause and not just its symptom — **verified: that line exists exactly as claimed**.

**test-contract specificity**
- risk — sharpest *failure-mode* contracts in the panel: nil `state_map` on first write, unsetting the last entry leaving no empty table, "byte-unchanged on error" (no truncate-then-fail), duplicate path emitted once, key order = argument order.
- mvp — contracts are correct but several are round-trips ("set then get round-trips"), which cannot detect the stray-quote/newline regression that `multi_get_shape` locks against; it does carry the reflection-over-`Board`-tags contract, which is the single best c-6 test in the panel.
- verification — contracts written first and it shows: golden-string (not re-derived) assert for the one-path form, reflection over `project.Board` toml tags, and an explicit "`dross validate` still exits 0" pin on the locked `status_check_home` boundary. Penalty: its milestone status set is wrong against the repo (see D1).

**granularity**
- risk — 8 tasks; correctly splits the hint *engine* from the hint *wiring* (different failure modes: table rot vs. cobra never reaching RunE), but bundles c-4's milestone half with c-5, which delays a wave-1-ready enum task into wave 2 for no dependency reason.
- mvp — 7 tasks, and t-2 is overloaded: hint table + subcommand routing + FlagErrorFunc + distance fallback across 4 files in one task. A regression there has no single owner. Task numbering is also non-monotonic across waves (t-4 sits in wave 2 after t-5/t-6/t-7), which is cosmetic but reads as a late edit.
- verification — 10 tasks, finest split, and every split falls on a real seam (renderer before adoption; engine before routing; routing before the cross-tree pin). t-10 being a pure-test task is legitimate here because it is the only place c-2's acceptance sentence is executable.

**wave correctness**
- risk — no same-wave file collisions in either wave (task.go / dotted_get.go / hints.go / doctor.go / project.go in W1; project+state / milestone / guard+main in W2), and its `t-6 depends t-5` edge is substantive, not padding — `project get` must read `board.state_map.<status>` back symmetrically per the locked decision. Weak spot: c-3 (`state show --json`) is stranded in wave 2 behind unrelated project work.
- mvp — wave split is sound and its stated rule ("no two same-wave tasks share a file") is the right invariant, but it is achieved by luck of only having two waves; t-4 depends on t-3 alone while its contract exercises `board.state_map` paths that t-6 introduces — an under-declared edge.
- verification — dependencies are all justified and t-7's `depends: t-2, t-3, t-4` correctly anticipates signature churn rather than hedging file collisions. Weak spot: t-8 and t-9 both extend the hint wiring in the same wave, which only avoids collision under its own (contested) choice to wire flags from `cmd/dross/main.go`.

**Skeleton: `verification`.** It has the sharpest contracts, the cleanest seam-aligned
splits, and it is the only draft that catches the prompt-corpus cause of `security run
--new`. Risk is a close second and contributes most of the edge-case contracts below;
mvp contributes the enum set that verification gets factually wrong and the
FlagErrorFunc placement that removes verification's one wave conflict.

## Merged plan

9 tasks across 3 waves. Origin tags: `[risk]`, `[mvp]`, `[verification]`, or a
combination where the task exists in several drafts.

### Wave 1

```
t-1  Add `dross task list` with --json                          [risk+mvp+verification]
     files:     internal/cmd/task.go, internal/cmd/task_test.go
     covers:    c-1
     depends_on: —
     description:
                New `task list [phase-id]` under Task(). Aligned ID/WAVE/STATUS/TITLE
                table by default; --json emits the task array. Phase-id optional,
                defaulting to state.current_phase (locked task_list_output).
     contract:
       - a 3-task/2-wave plan prints exactly 3 rows, each carrying id, wave, status
         and title                                              [verification]
       - a task whose plan.toml `status` is absent renders `pending`, not a blank
         column — the same orPending rule `task show` uses      [all three]
       - `task list --json` unmarshals into a []struct with id/wave/status/title; a
         table-only regression fails the json.Unmarshal          [mvp+verification]
       - `task list --json` on a zero-task plan emits `[]` and exits 0 — never `null`,
         never the human "(no tasks)" line                       [risk]
       - phase-id omitted with state.current_phase set lists that phase; with
         current_phase empty it errors naming current_phase rather than loading
         `.dross/phases//plan.toml`                              [risk+verification]
       - an unknown phase-id errors with the missing path and prints no table header
         first                                                   [risk]
```

```
t-2  Complete + reversible project dotted paths                 [risk+mvp+verification]
     files:     internal/cmd/project.go, internal/cmd/project_test.go
     covers:    c-6, c-9
     depends_on: —
     description:
                readDotted/writeDotted gain `board.github_project` and a dynamic
                `board.state_map.<status>` arm addressing one entry at a time (locked
                state_map_write). `project set` gains `--unset <path>` clearing a
                scalar or deleting a single state_map key.
     contract:
       - a reflection test over `project.Board`'s toml tags fails naming the field if
         any Board field lacks BOTH a readDotted and a writeDotted case — the only
         shape that stays true when a 10th Board field is added
                                                                 [mvp+verification]
       - `project set board.state_map.done Closed` on a project.toml with no
         [board.state_map] table creates the map instead of panicking on a nil map
                                                                 [risk]
       - setting one state_map key leaves the other entries intact — per-key write,
         never a whole-map replace                               [all three]
       - `project set --unset board.state_map.done` removes only that key; the other
         entries and every other [board] field are unchanged     [all three]
       - unsetting the last state_map entry writes project.toml with no
         [board.state_map] table at all, not an empty one        [risk]
       - `--unset` on an unknown path errors with the same unknown-field message
         `set` uses, and leaves project.toml byte-unchanged (no truncate-then-fail)
                                                                 [risk+verification]
       - `--unset repo.squash_merge` yields false and `--unset` on a list field yields
         an absent list, not the literal string "<nil>"          [risk]
       - `project set board.github_project PVT_x` then `project get
         board.github_project` round-trips, and reloads as Board.GitHubProject, not a
         stray top-level key                                     [all three]
```

```
t-3  Resolve bare milestone field + validate status enum        [mvp+verification]
     files:     internal/configenum/configenum.go,
                internal/configenum/configenum_test.go,
                internal/cmd/milestone.go, internal/cmd/milestone_test.go
     covers:    c-5
     depends_on: —
     description:
                Add a MilestoneStatuses set to configenum. `milestone set` expands an
                unambiguous bare field name to its dotted path before
                writeMilestoneDotted and rejects an out-of-set milestone.status
                before Save. (risk merges this into its wave-2 milestone task — see D5.)
     contract:
       - `milestone set status active` writes milestone.status on the current
         milestone; without bare-name expansion it fails with `unknown or unsettable
         milestone field: status`                                [mvp+risk]
       - `milestone set status <not-in-set>` exits non-zero, the message names the
         accepted set, and the milestone toml is byte-identical afterwards —
         rejection runs before Save, not after                   [all three]
       - `milestone set milestone.status <valid>` (fully dotted) still works
         unchanged — the resolver is additive                    [verification]
       - a bare name matching more than one settable path is rejected as ambiguous,
         listing the candidates, rather than silently taking the first
                                                                 [risk]
       - `milestone set foo x` still errors — expansion resolves known bare names
         only, no nearest-guess fallthrough                      [mvp]
       - configenum.MilestoneStatuses.Has("") is false, so `milestone set status ""`
         is rejected rather than blanking the field              [mvp+verification]
       - a valid status differing only in case/whitespace is accepted, matching
         configenum.Normalize for every other enum               [risk]
```

```
t-4  Multi-path get renderer + `state get` + `state show --json` [mvp+verification]
     files:     internal/cmd/dotget.go, internal/cmd/dotget_test.go,
                internal/cmd/state.go, internal/cmd/state_test.go
     covers:    c-3, c-4
     depends_on: —
     description:
                New renderMultiGet(paths, lookup): one path prints the bare value
                unchanged, two or more emit a single keyed JSON object (locked
                multi_get_shape). Resolves every path before emitting anything. New
                `dross state get <path>...` over state.json. `state show` accepts
                --json and prints what it already prints.
     contract:
       - one path in -> the bare value and nothing else: `state get current_phase`
         prints exactly "<phase>\n" — a golden-string literal, not a value re-derived
         from the same code, because multi_get_shape locks this byte-identical
                                                                 [verification]
       - two-plus paths -> stdout parses as one JSON object whose keys are the
         requested paths verbatim, in argument order, so a script never depends on
         map iteration                                           [risk+verification]
       - two paths where the SECOND is unknown errors naming that path and writes
         nothing to stdout — no partial object                   [all three]
       - a path whose lookup yields a list serialises as a JSON array in multi mode,
         not as a CSV or newline-joined string                   [risk]
       - the same path passed twice appears once, not as duplicate JSON keys
                                                                 [risk]
       - `dross state --help` lists `get`, and `state get version` returns the 4-part
         version from state.json                                 [verification]
       - `state show --json` exits 0 and its stdout is byte-identical to bare
         `state show`; pre-change this failed with `unknown flag: --json`
                                                                 [mvp+verification]
       - `state get` on a path that exists in project.toml but not state.json (e.g.
         project.name) errors naming the unknown field           [risk]
```

```
t-5  Doctor flags unrecognised task statuses                    [risk+mvp+verification]
     files:     internal/cmd/doctor.go, internal/cmd/doctor_test.go
     covers:    c-7
     depends_on: —
     description:
                Doctor walks .dross/phases/*/plan.toml and reports every task whose
                status is outside pending|in_progress|done|failed, naming phase id and
                task id, counting each as an issue (non-zero exit). Lives in doctor,
                not validate (locked status_check_home).
     contract:
       - a plan.toml carrying a near-miss status ("Done" capitalised, "in-progress"
         hyphenated) prints one line containing BOTH the phase id and the task id and
         doctor exits non-zero — the exact value NextRunnable skips silently today
                                                                 [all three]
       - each of pending / in_progress / done / failed, plus an omitted status field,
         produces no line and leaves doctor's exit code unchanged — empty means
         pending everywhere else in the code                     [all three]
       - a phase directory with no plan.toml is skipped silently and doctor still
         reaches its final verdict rather than erroring out of the section
                                                                 [risk+verification]
       - an unparseable plan.toml is reported as its own issue naming the phase, not
         swallowed into a clean "✓" line                         [risk]
       - one bad status lands in doctor's exit-code issue count, not the advisory
         warnings block                                          [risk]
       - `dross validate` on that same repo still exits 0 — pins status_check_home
         rather than merely respecting it                        [mvp+verification]
```

```
t-6  Curated mis-reach table + nearest-match fallback           [risk+mvp+verification]
     files:     internal/cmd/hints.go, internal/cmd/hints_test.go,
                assets/prompts/secure.md
     covers:    c-2, c-8
     depends_on: —
     description:
                Curated map from known mis-reaches to their working invocation,
                consulted first; string distance over command and flag names as
                fallback (locked hint_mechanism). Also fixes the prompt that teaches
                one of the four mis-reaches (see D4).
     contract:
       - CuratedHint("dross task", "done") returns a string containing `dross task
         status <phase-id> <task-id> done` — the semantic remap edit distance can
         never produce                                           [all three]
       - CuratedHint returns the documented working invocation for each of ("dross
         phase create","--title"), ("dross task edit","--files") and ("dross security
         run","--new"); a lookup with no entry returns found=false so the fallback is
         reachable                                               [verification]
       - Nearest("stauts", ["status","show","set"]) == ["status"]; Nearest("zzzzzz",
         same) is empty — a far-off typo gets no fabricated suggestion
                                                                 [risk+verification]
       - lookup is normalised, so `dross Task Done` hits the same entry
                                                                 [risk]
       - no assets/prompts/*.md contains an invocation the table classifies as broken
         — a grep test over the prompt corpus, which fails today on
         assets/prompts/secure.md:42 (`dross security run --new`)  [verification]
     note:      r-01 — editing assets/prompts/secure.md is not live until
                `make install` re-links it.
```

### Wave 2

```
t-7  Adopt multi-path get in project + milestone                [mvp+verification]
     files:     internal/cmd/project.go, internal/cmd/project_test.go,
                internal/cmd/milestone.go, internal/cmd/milestone_test.go
     covers:    c-4
     depends_on: t-2, t-3, t-4
     description:
                `project get` and `milestone get` take 1+ dotted paths and render
                through renderMultiGet. `milestone get` keeps its optional leading
                version by treating args[0] as a version only when it is not itself a
                known milestone field.
     contract:
       - `project get project.name` prints the bare name byte-identically to today,
         asserted against a literal — the guard that keeps every existing prompt
         working                                                 [all three]
       - `project get project.name runtime.mode board.state_map.<k>` emits one JSON
         object with those three keys, including the state_map path t-2 added
                                                                 [verification]
       - `milestone get v1.1 milestone.title milestone.status` resolves v1.1 as the
         version while `milestone get milestone.title milestone.status` falls back to
         state.current_milestone — treating args[0] as a version unconditionally
         fails the second case                                   [all three]
       - `milestone get scope.success_criteria` alone still prints one entry per line
         exactly as today, while in multi-path form it serialises as a JSON array
                                                                 [risk+verification]
       - an unknown path among several errors naming that path rather than emitting
         an object with only the good keys                       [all three]
     depends rationale: t-2 changes readDotted's path set and t-3 changes how a
                milestone path resolves; wrapping either before its signature settles
                means rework.                                    [verification]
```

```
t-8  Wire hints into subcommand and flag errors                 [risk] (+mvp placement)
     files:     internal/cmd/subcommand_guard.go,
                internal/cmd/subcommand_guard_test.go,
                internal/cmd/flag_hint.go, internal/cmd/flag_hint_test.go
     covers:    c-2, c-8
     depends_on: t-6
     description:
                EnforceSubcommandKnown consults the t-6 resolver in its
                unknown-subcommand path, and installs a root FlagErrorFunc (cobra
                inherits it down the tree) so unknown-flag errors — which never reach
                RunE — get the same curated-then-distance treatment. No cmd/dross/
                main.go edit: main.go already calls EnforceSubcommandKnown on root
                (verified, main.go:67). See D3.
     contract:
       - `dross task done t-1` errors with a message containing `dross task status`,
         and the curated entry wins over cobra's own suggestion list when both would
         fire                                                    [verification]
       - `dross task shwo p t-1` (no curated entry) errors naming `show` via the
         distance fallback                                       [verification]
       - `dross task wibble` (no entry, no near match) still prints the available-
         subcommands list and exits non-zero — the hint path adds to the existing
         fallback, it does not replace it                        [risk+verification]
       - an unknown flag with a curated entry produces that working invocation
         instead of cobra's bare `unknown flag: --title`         [verification]
       - an unknown flag with NO curated entry names the nearest flag actually
         declared on that command (`--titel` -> `--title`), and names none when
         nothing is within distance                              [verification]
       - the hook reaches a depth-3 command (`dross task edit --files a.go`), proving
         cobra's FlagErrorFunc parent-walk rather than assuming it
                                                                 [risk+verification]
       - a KNOWN flag still parses and a valid subcommand still runs its RunE and
         returns nil — installing the hooks changes nothing else  [verification]
       - `dross task done` still exits non-zero, so the pre-existing exit-0-on-help
         regression does not return                              [risk]
       - a hinted flag error is still classified `unknown_flag` by the telemetry
         taxonomy — the reworded message must not knock the event into `other`
                                                                 [risk]
```

### Wave 3

```
t-9  Pin the four mis-reaches against the real tree             [verification]
     files:     cmd/dross/main_test.go
     covers:    c-2
     depends_on: t-8
     description:
                Table test over the assembled newRoot() tree. This is the only place
                c-2's acceptance sentence is executable and the only package that can
                see the real tree rather than a hand-built one. (risk folds this into
                its wiring task; see D2.)
     contract:
       - all four of `task done`, `phase create --title`, `task edit --files` and
         `security run --new` exit non-zero AND print their working invocation —
         c-2's sentence, executed                                [verification]
       - every replacement invocation in the curated table resolves against
         newRoot(): a hint naming a command path or flag that does not exist fails
         here, so the table cannot rot into pointing at nothing   [risk+verification]
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 `task list` | t-1 |
| c-2 mis-reach names working invocation | t-6, t-8, t-9 |
| c-3 `state show --json` | t-4 |
| c-4 multi-path get | t-4, t-7 |
| c-5 bare field + milestone status enum | t-3 |
| c-6 every `[board]` field settable | t-2 |
| c-7 doctor unrecognised task status | t-5 |
| c-8 distance fallback | t-6, t-8 |
| c-9 `project set --unset` | t-2 |

9/9. No same-wave file collisions: W1 touches task.go / project.go /
milestone.go+configenum / dotget.go+state.go / doctor.go / hints.go+secure.md;
W2 touches project.go+milestone.go and subcommand_guard.go+flag_hint.go; W3
touches main_test.go only.

## Disagreements

Six genuine divergences. Each carries a provisional default — none is silently
resolved.

### D1 — the milestone status value set

- **mvp**: `planning | active | complete`, with `Has("")` false.
- **verification**: `planning | active | shipped | archived`, asserted in a contract
  (`milestone set status shipped` is the *success* case).
- **risk**: names "a new configenum set" without committing to values.

The two named sets are incompatible on both `complete` and `shipped`. Checked
against the tree: `internal/milestone/milestone.go:29` comments
`planning | active | shipped | archived` (verification's source), but all 11
`.dross/milestones/*.toml` on disk carry `status = "complete"` or `"active"` —
`shipped` and `archived` appear nowhere in real data, and `complete` is what
`milestone complete` writes.

**Provisional default: mvp's set — `planning | active | complete`**, and the task
must reconcile the stale doc comment at milestone.go:29 in the same edit.

**Why it matters:** shipping verification's set makes `milestone set status
complete` a hard error, breaking the milestone-completion path this phase was
partly motivated by (the recurring "milestone.status-set CLI gap"). The reverse
error is milder — an unused `shipped` simply never validates. Adopting either
set without reading the on-disk values would have shipped a validator that
rejects the repo's own history. **Confirm the intended set before executing t-3.**

### D2 — how many tasks the hint layer takes, and whether wave 3 exists

- **mvp**: one task. "Splitting them ships a half-wired error path where the
  fallback exists but nothing routes to it."
- **risk**: two — engine (table + drift guard) and wiring — because table rot and
  cobra's flag-error path are structurally different failure modes.
- **verification**: four — table, subcommand routing, flag routing, and a wave-3
  cross-tree pin — because c-2's four invocations only become simultaneously true
  after both routing tasks land.

**Provisional default: three (t-6 engine, t-8 both wirings, t-9 wave-3 pin).**
Takes risk's engine/wiring seam, collapses verification's t-8/t-9 into one wiring
task (which D3's placement forces anyway), and keeps verification's separate pin.

**Why it matters:** this decides whether the phase is 2 or 3 waves. The pin is
~30 lines of table test and could ride inside the wiring task, saving a wave; kept
separate it stays honest about *when* c-2 is actually satisfied and can fail
without reopening the wiring commit. If wave count matters more than attribution,
fold t-9 into t-8 and the plan is 8 tasks across 2 waves.

### D3 — where the FlagErrorFunc is installed, and whether main.go is touched

- **mvp**: inside `EnforceSubcommandKnown` — main.go already calls it on root,
  cobra inherits FlagErrorFunc down the tree, the whole hint layer stays testable
  in internal/cmd with no new wiring seam.
- **risk** and **verification**: edit `cmd/dross/main.go` to install it.

Checked: `cmd/dross/main.go:67` does call `cmd.EnforceSubcommandKnown(root)`, and
main.go declares no FlagErrorFunc today — so mvp's claim holds.

**Provisional default: mvp's placement.** It removes a wave-2 file collision that
verification's split would otherwise create, and keeps every hint test in a
package that can build a tree fixture.

**Why it matters:** the drift guard still needs `cmd/dross/main_test.go` (only
package main sees the real `newRoot()` tree — risk is right about that, and t-9
preserves it). So this narrows to *production* code: internal/cmd only, main.go
untouched. If the installation is later wanted at the root for explicitness, it
is a one-line move that t-9's contract already covers.

### D4 — is `assets/prompts/secure.md` in scope?

- **verification**: yes, in t-6, with "no prompt teaches a broken invocation" as a
  grep-over-corpus contract. `security run --new` is a curated mis-reach *because*
  dross's own prompt documents a flag `securityRun` never declares.
- **mvp**: explicitly no — "no criterion mentions docs".
- **risk**: silent; treats it purely as a hint-table entry.

Checked: `assets/prompts/secure.md:42` contains `dross security run --new`
verbatim. Verification's factual claim is correct.

**Provisional default: include it** (kept in t-6, with the r-01 `make install`
note attached).

**Why it matters:** c-2 is satisfied either way — the error message names the
working invocation regardless. But leaving the prompt in place means dross keeps
*generating* the mis-reach it now apologises for, so the hint fires forever
instead of never. The counter-argument is scope discipline: it is a doc edit no
criterion requests, and r-01 means it is not even live until `make install`.
Cheap to drop if the phase is running tight.

### D5 — does c-5 travel with c-4's milestone half?

- **risk**: yes — one wave-2 task owns milestone.go, because "which argument is
  this?" (version-vs-path *and* bare-name-vs-dotted-path) is one ambiguity problem
  and one resolver should own it.
- **mvp** and **verification**: no — the enum/bare-name work is a standalone wave-1
  task; only the multi-get adoption waits for the renderer.

**Provisional default: split (mvp + verification), 2-of-3.**

**Why it matters:** c-5 has no dependency on the renderer, so grouping it with
c-4 idles it behind wave 1 for nothing and puts two unrelated regressions
(status-enum rejection, version-argument misparse) under one commit. Risk's
counterpoint is real though: both changes edit the same argument-parsing code in
milestone.go across two waves, so t-7 must not re-litigate t-3's bare-name
resolver — t-7's contract deliberately exercises only the *version* ambiguity.

### D6 — which wave `state show --json` (c-3) lands in

- **mvp** and **verification**: wave 1, bundled with the renderer + `state get`,
  since all three touch state.go.
- **risk**: wave 2, bundled with `project get` adoption.

**Provisional default: wave 1 (mvp + verification).**

**Why it matters:** c-3 is the smallest criterion in the phase (accept a flag that
changes nothing) and has no dependency on the renderer at all. Putting it in wave
2 behind project work delays the phase's one trivially-shippable fix. The only
argument for risk's placement is keeping state.go under a single owner across the
phase, which the wave-1 grouping already achieves.
