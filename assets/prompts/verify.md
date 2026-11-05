# /dross-verify

Decide whether a phase delivered what its `spec.toml` promised — not by counting tasks, but by checking that **tests catch breakage** and **every acceptance criterion has a real test that would fail when it broke**. Three checks, in order: mutation efficacy (mechanical), criterion-to-test mapping (LLM judgement), final verdict.

**Surface results as a report, not a raw artifact.** Follow the interaction playbook (`_interaction.md`, printed by `dross interaction show` below): §4 emits the verdict plus a compact criterion→test/status mapping and never pastes the raw `verify.toml` back.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; the rules are MUST-FOLLOW and the playbook governs this command.
2. Resolve the phase from `$ARGUMENTS` or `state.json`'s `current_phase`; fail if neither resolves.
3. Read `.dross/phases/<id>/spec.toml` and `plan.toml`; if either is missing, route to `/dross-spec` or `/dross-plan` first.
4. Read `.dross/phases/<id>/changes.json`; missing or empty → route to `/dross-execute`. Keep its `reviews` key for §4 (a `--solo` run's per-task reviewer outcomes and findings), and note any task `plan.toml` marks `failed`, with the reason `dross task show <id> <task-id>` prints.
5. Parse `--skip-mutation` (default off; skip only if the user asked).
6. Check exec consent — `dross verify` runs this repo's test suite through mutation tools, and every dross command that spawns a process refuses without it:
   ```
   dross trust --check
   ```
   Non-zero means untrusted or stale: **stop and show the user the exact `runtime.test_command` line** from project.toml, then let them run `dross trust`. Never run `dross trust` on their behalf: the gate exists so a human reads the line.

   To run the suite directly, use `dross test`, never the raw command (`--local` forces this machine). A **3** or **4** exit means the run did not happen — not a verdict.

## 1. Mechanical pass — `dross verify`

```
dross verify <phase> [--skip-mutation]
```

It runs the mutation tools (other languages skip with a reason) and writes:

- `.dross/phases/<id>/tests.json` — raw output, killed/survived per language, the `scope` block and the `out_of_scope` list (survivors in files this phase never touched)
- `.dross/phases/<id>/verify.toml` — a skeleton with `verdict = "pending"`, per-criterion `status = "unknown"`, `[summary].mutation_status` (`measured | unmeasurable | skipped | out-of-scope` — this, not the raw score, decides whether the mutation leg gates in §3) and `[summary].unclassified_in_scope`, the mutation leg's fail lever

Don't hold the session through a long mutation leg — dispatch it detached.

**When** `leg=detached` — the mutation leg is long (estimate it from the mutant-site count, never a past duration), or a detached run is in flight to collect: `dross prompt partial verify detached`

**Read the score with its denominator, over this phase's changed files only.** `dross verify` prints `score: 0.90 over 191 in-scope mutant(s)` (`[summary].mutants_in_scope`) and a line naming how many are uncoverable by construction, with the efficacy over the rest. Carry both into the report: 0.90 over 10 and over 400 are not the same evidence, and a survivor the tooling cannot reach is not one the tests missed. Reports are filtered to the phase's change set, so a survivor in an untouched sibling is real but not this phase's.

**A run says which lines it measured.** A leg with no `ranges` is a whole-file leg and is never called ranged: gremlins always is, and a stryker leg that fell back says why. Judge the score against what was actually instrumented, not against the phase's diff.

**When** `scope=degraded` — verify printed a `scope degraded:` line: `dross prompt partial verify scope`

**Read both files before continuing.**

`dross verify` also moves the board issue to the UAT state and prints a `board:` line: `off` when `[board].enabled` is false. On `board: FAILED` — a non-zero exit is a board failure — the artefacts are still written: surface it, never read it as the off no-op, and retry:
```
dross issue phase sync <phase> --status uat
```

**When** `mutation=failed` — mutation testing failed to run (e.g. Stryker not installed): `dross prompt partial verify mutation-error`

## 2. Criterion-to-test mapping (the LLM judgement step)

**Offload the reading when the surface is large** (many criteria × many test files, or a long survivor list): fan out read-only subagents, one per criterion or test area, that read `tests.json`, grep the tests and return **candidate** mappings (test, file:line, what it asserts, relevant survivors). Classification, cross-check and verdict stay here — per `dross-agent-gate`, fan-out agents never fill in verify.toml or decide the verdict. For a small phase, stay inline (docs/subagent-offload-audit.md).

For each criterion `c-N`: restate it in your own words, name its breaking surface ("11th tag should be rejected"), search the tests (`paths.tests` from `project.toml`, or colocated tests under `paths.source`) for an assertion that exercises it, and classify:
- **`covered`** — ≥1 test would clearly fail if it broke. Record `file:test name`.
- **`weak`** — a test touches the area but asserts too generically (200 OK, not the rejection). Record + FLAG.
- **`uncovered`** — no test maps to it. BLOCKING.
- **`unknown`** — no confident call (e.g. an unfamiliar framework). Record honestly + flag for review.

Show 1-3 lines of reasoning per criterion — the user needs the audit trail.

### Cross-check with mutation results

For each in-scope survivor under `languages[].mutation.surviving` in `tests.json`: if the mutated line is in a criterion's covering test's path, the test misses that breakage — downgrade `covered` → `weak`; if not, FLAG it ("survived mutant in <file>:<line>").

**Weight each survivor by its `origin` tag.** `in-hunk` — inside a hunk this phase changed: its own logic escaping its tests, strong evidence; downgrade. `inherited` — the file was touched but not that line: still counted, a weaker signal; prefer a FLAG unless the line is part of what a criterion claims.

**Close out every survivor in this run — the backlog only ever shrinks.** Each survivor in `tests.json` carries one `Lifecycle` state; `in-diff` ones you judge as above, `routed` and `accepted` ones you leave.

**When** `unclassified=nonzero` — a survivor is `unclassified`: `dross prompt partial verify drain`

## 3. Update `verify.toml`

Fill in each `[[criterion]]` of the skeleton:
```toml
[[criterion]]
id     = "c-1"
status = "covered"        # covered | weak | uncovered | unknown
tests  = ["src/api/tags.test.ts:test('rejects 11th tag')"]
notes  = ""               # short rationale; required for weak/uncovered
```

Update `[summary]`:
```toml
[summary]
mutation_status    = <from tests.json — preserve: measured | unmeasurable | skipped | out-of-scope>
mutation_score     = <from tests.json — preserve>
mutants_killed     = <preserve>
mutants_survived   = <preserve>
mutants_in_scope   = <preserve — the denominator the score was computed over>
criteria_total     = <count of criteria>
criteria_covered   = <count where status=covered>
criteria_uncovered = <count where status=uncovered or weak>
```

Leave `[verify].measured_commit` and `[verify].measured_tree` exactly as `dross verify` wrote them — `dross ship` refuses a pass whose tree has moved since; without them that refusal degrades to a warning. Leave every `[[summary.leg]]` as written too, and **never** recompute `mutation_score` from them (averaging legs gives a small leg a large one's vote). A leg carrying an `error` measured nothing.

Compute `[verify].verdict`. **Read `mutation_status` first** — when it is not `measured`, the score is a 0/0 artifact and the mutation leg has nothing to say. **The mutation leg gates on a count, not a ratio:** `[summary].unclassified_in_scope` (survivors in this phase's diff with no disposition) must be zero. `mutation_score` is reported as evidence, never a verdict lever — a cutoff lets killed mutants bury a live one.

If `mutation_status == "measured"`:
- **`pass`** if all criteria are `covered`, `unclassified_in_scope` is 0, and there are no BLOCKING findings.
- **`partial`** if at least one criterion is `weak`, OR there are FLAG findings but no BLOCKING.
- **`fail`** if any criterion is `uncovered`, OR `unclassified_in_scope` > 0, OR any BLOCKING findings exist.

If `mutation_status` is `unmeasurable`, `skipped` or `out-of-scope`, base verdict on criterion coverage alone — nothing was measured, so the mutation leg neither passes nor fails the phase:
- **`pass`** if all criteria are `covered` and no BLOCKING findings; add a NOTE saying why mutation didn't apply (e.g. "mutation out-of-scope: every mutant landed in files this phase did not touch").
- **`partial`** if at least one criterion is `weak` OR there are FLAG findings but no BLOCKING.
- **`fail`** if any criterion is `uncovered` OR any BLOCKING findings exist.

Each unclassified in-scope survivor is seeded as its own BLOCKING finding naming `file:line (op)`: clear it by killing it with a test, `dross survivor accept --reason`, or `dross survivor route --target` — never by routing it to the phase you are verifying. Keep the seeded findings and add your own:
```toml
[[finding]]
severity = "BLOCKING"     # BLOCKING | FLAG | NOTE
text     = "criterion c-2 (case-insensitive lookup) has no covering test"
```

## 4. Surface to user

Print this report, never the raw `verify.toml` (`verify_surface`: the file is the artifact, this is the report):

```
verify <phase-id> — <verdict>

  Mutation:    score=<X.XX> over <mutants_in_scope> in-scope mutants — killed=<N> survived=<M>
               <only when non-zero: "filtered <K> out-of-scope survivor(s)">
  Survivors:   <N> in-diff, <N> routed, <N> accepted, <N> unclassified
               <only when unclassified > 0: "↳ drain with dross survivor accept|route">
  Criteria:    <covered>/<total> covered, <weak> weak, <uncovered> uncovered

  Criterion map:
    c-1  covered   <test name / surface that catches it>
    c-2  weak      <test name — what breakage it misses>
    c-3  uncovered <no test exercises this>

  Findings:
    BLOCKING (<count>):
      - <one-line per blocking>
    FLAG (<count>):
      - <one-line per flag>
    NOTE (<count> — see verify.toml)

  Verdict: <pass | partial | fail>
```
One line per criterion; for survivor detail behind a row, point at `verify.toml`.

**When** `reviews=present` — changes.json has `reviews`, or a task failed: `dross prompt partial verify solo-review`

**When** `verdict=partial` — the verdict is `partial`: `dross prompt partial verify not-pass`
**When** `verdict=fail` — the verdict is `fail`: `dross prompt partial verify not-pass`

## 5. Wrap

Record the resolved verdict:
```
dross verify finalize <phase-id>
```
It refuses while `verdict = "pending"` — finish §3 first. It is idempotent, writes `finalized = true` into `verify.toml` (commit it below), sets `current_phase_status` to verified and records the verdict in state history — don't repeat either by hand. Ship and `dross phase complete` auto-finalize as a safety net; run it here anyway so telemetry dates the decision.

Commit the verify artefacts (`repo.commit_convention`; drop `tests.json` if `--skip-mutation` left it unwritten):
```
git add .dross/phases/<phase-id>/verify.toml .dross/phases/<phase-id>/tests.json
git commit -m "chore(dross): record verify for <phase-id> (<verdict>)"
```

If verdict is `pass`:

1. Resolve `<next-id>` from the **milestone's `phases` array** — `dross milestone show` (or `.dross/milestones/<version>.toml`) — the entry right after `<id>`. That array is the ordering truth, unstarted phases included.

   **Do not use a bare `dross phase list` for this.** It lists only phases someone has scaffolded, so every unstarted successor is invisible — that is how phase 9 of 14 once announced the milestone complete. `dross phase list --milestone <version>` is a safe second reading: it walks the array, marks `(not scaffolded)` entries and ends with `N/M done`. Only with no active milestone (`state.current_milestone` unset) fall back to the bare `dross phase list`. `<id>` is last only when it is the last entry of that array.

2. If `<next-id>` exists, print:
```
Phase <id> verified: pass.

Next:
  /dross-ship              — open PR for this phase (filters .dross/, opens via provider)
      ↳ --draft            — open the PR in draft (work-in-progress, not ready for review)
      ↳ --no-push          — preview the PR body and diff without pushing
  /dross-spec <next-id>    — start the next phase
  dross phase list         — see all phases (--milestone <version> for one roadmap)

state is on disk — safe to /clear · fresh session: /dross-ship
```

3. Otherwise (last phase in the milestone), print:
```
Phase <id> verified: pass. This is the last phase in the milestone.

Next:
  /dross-ship              — open PR for this phase
      ↳ --draft            — open the PR in draft (work-in-progress)
      ↳ --no-push          — preview the PR body and diff without pushing
  dross milestone show     — review milestone status before tagging the release

state is on disk — safe to /clear · fresh session: /dross-ship
```

## Hard rules

- **Follow the interaction playbook (`_interaction.md`); verify.toml is never a review medium.** §4 surfaces the verdict and a compact criterion map; point at the file for survivor detail.
- **Don't fake coverage.** No test maps to a criterion → `uncovered`. An honest `fail` beats a false `pass`.
- **Capture a stated preference as a rule, don't apply it ad hoc.** If the user pushes back on how the verdict is decided — including asking for a score threshold — capture it as a project-scope rule via `/dross-rule` so future verifies inherit it.
- **Don't write tests yourself.** Verify is a check, not a fix: route uncovered criteria to /dross-execute or /dross-plan.
- **Don't skip the cross-check.** A survivor in covered code that doesn't downgrade `covered` → `weak` is the theatrical coverage dross exists to catch.
- **Single pass, no checker loop.** Write the verdict; the user decides what next.
