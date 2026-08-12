# /dross-verify

Decide whether a phase actually delivered what its `spec.toml` promised — not by counting tasks, by checking that **tests catch breakage** and **every acceptance criterion has a real test that would fail when it broke**.

Three checks, in order: mutation efficacy (mechanical), criterion-to-test mapping (LLM judgement), final verdict.

**Surface results as a report, not a raw artifact.** Follow the shared interaction playbook (`_interaction.md`, printed by the `dross interaction show` pre-flight step below): §4 emits the verdict plus a compact criterion→test/status mapping — the judgement the user must trust — and never pastes the raw `verify.toml` back.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for this command.
2. Resolve target phase from `$ARGUMENTS` or `state.json`'s `current_phase`. Fail if neither resolves.
3. Read `.dross/phases/<id>/spec.toml` and `plan.toml`. If either is missing, route to `/dross-spec` or `/dross-plan` first.
4. Read `.dross/phases/<id>/changes.json`. If missing or empty: `/dross-execute` hasn't touched anything for this phase yet — stop and route there.
5. Parse `--skip-mutation` flag. Default OFF (run mutation testing). Skip if user explicitly asked.
6. Check exec consent before §1 — `dross verify` shells out to mutation tools that run this repo's test suite, and it refuses without it:
   ```
   dross trust --check
   ```
   Exit 0 means trusted; continue. Non-zero means untrusted or stale — **stop and show the user the exact `runtime.test_command` line** from project.toml, then let them run `dross trust`. Never run `dross trust` on their behalf: the gate exists so a human reads the line the repo supplied.

## 1. Mechanical pass — `dross verify`

Run:
```
dross verify <phase> [--skip-mutation]
```

This shells out to mutation tools (currently Stryker for TS/JS/Svelte; other languages skip with a reason), parses the JSON reports, and writes:

- `.dross/phases/<id>/tests.json` — raw machine output, killed/survived counts per language, plus the `scope` block (what the run scoped to) and the `out_of_scope` list (survivors in files this phase never touched)
- `.dross/phases/<id>/verify.toml` — skeleton verdict with `verdict = "pending"`, per-criterion `status = "unknown"`, and `[summary].mutation_status` of `measured | unmeasurable | skipped | out-of-scope` (use this — not the raw score — to decide whether the mutation leg gates at all in §3), plus `[summary].unclassified_in_scope`, the mutation leg's fail lever

**The score covers only this phase's changed files.** Mutation tools attribute at a coarser granularity than a phase does — gremlins mutates a whole Go package — so every report is filtered against the phase's change set before it reaches these files. A survivor in an untouched sibling is real, but it is not this phase's, and it is neither scored nor flagged here. `[summary].mutants_in_scope` is the denominator that filtering left: read it next to the score, because 0.50 over 2 mutants and 0.50 over 200 are the same number and not the same evidence.

**Read both files before continuing.** They're the inputs for the LLM judgement step.

Mark the board issue as verifying (no-op unless `[remote].board_sync` is on — safe to always run):
```
dross issue phase-sync <phase> --status verifying
```

If mutation testing fails to run (e.g. Stryker not installed), surface the error to the user and ask:
- `install Stryker` — guide them through `pnpm add -D @stryker-mutator/core` (or equivalent for their package manager from `project.toml`)
- `skip mutation` — re-run with `--skip-mutation`; verdict will note that mutation efficacy is unverified
- `abort`

## 2. Criterion-to-test mapping (the LLM judgement step)

For each criterion in `spec.toml`, find the test(s) that would fail if that criterion broke. This is where you actually *do* the verify work.

**Offload the reading when the surface is large.** When this phase's surface is large — many criteria × many test files, or a long surviving-mutant list in `tests.json` — don't drag it all into the main context: fan out read-only subagents (one per criterion or per test area) that read `tests.json` and grep the test files, and return per-criterion **candidate** mappings (test name, file:line, what its assertion exercises, relevant surviving mutants). The classification judgement (§ step 4), the mutation cross-check, and the verdict stay in the main loop — per the `dross-agent-gate` rule, fan-out agents are read-only and never fill in verify.toml or decide the verdict. For a small phase, stay inline — the subagent hop isn't worth it (see docs/subagent-offload-audit.md).

For each criterion `c-N`:

1. Restate the criterion in your own words. ("c-1: user can attach up to 10 tags per meal.")
2. Identify what the breaking surface looks like. ("11th tag should be rejected; over-limit case.")
3. Search the test files (use `Grep`/`Glob` on `paths.tests` from `project.toml`, or scan `paths.source` for colocated tests) for a test whose assertion exercises that surface.
4. Classify:
   - **`covered`** — found ≥1 test that would clearly fail if the criterion broke. Record file path + test name (e.g. `src/api/tags.test.ts:test('rejects 11th tag')`).
   - **`weak`** — found a test that touches the area but its assertions are too generic (e.g. checks for 200 OK, not for the actual rejection). Record + add a FLAG finding.
   - **`uncovered`** — no test maps to this criterion. Add a BLOCKING finding.
   - **`unknown`** — couldn't reach a confident classification (e.g. test framework you don't recognise). Record honestly + flag for user review.

Show your reasoning per criterion in 1-3 lines. Don't be silent — the user needs to see the audit trail.

### Cross-check with mutation results

For each surviving mutant under `languages[].mutation.surviving` in `tests.json` — the **in-scope** survivors, the ones in files this phase touched:
- Does the mutated line participate in any criterion's covering test?
- If yes: the test exists but doesn't catch this kind of breakage → downgrade that criterion from `covered` to `weak`.
- If no: less concerning, but still surface as a FLAG finding ("survived mutant in <file>:<line>").

**Weight each survivor by its `origin` tag.** Every in-scope survivor carries one:
- `in-hunk` — the mutated line is inside a hunk this phase changed. This is the phase's own new or edited logic escaping its tests; treat it as strong evidence and downgrade the criterion.
- `inherited` — the phase touched the file but not that line. Still in scope and still counted, but the weaker signal: it is pre-existing code the phase inherited responsibility for by editing around it. Prefer a FLAG over a `covered` → `weak` downgrade unless the line is genuinely part of what a criterion claims.

**Close out every survivor in this run — the backlog only ever shrinks.** Each survivor in `tests.json` carries exactly one `Lifecycle` state, and each state has one job:

| state | what it means | what you do |
| --- | --- | --- |
| `in-diff` | in a file this phase touched | judge it as above — downgrade the criterion, or FLAG it |
| `routed` | a deferred item carries its key plus a destination | leave it; the skeleton NOTEs it with its target |
| `accepted` | recorded in `.dross/survivors.toml` with a reason | leave it; it is already silent, by decision |
| `unclassified` | no state applies — out of the diff, unrouted, unaccepted, or its identity would not resolve | **drain it this run** |

Drain an `unclassified` survivor with one of exactly two verbs, then re-run `dross verify`:
```
dross survivor accept <file>:<line> --op <OP> --reason "<why it is permanently acceptable>"
dross survivor route  <file>:<line> --op <OP> --target <phase-slug>
```
`accept` is for a survivor that is genuinely unkillable (e.g. gremlins' switch-case / const-initializer attribution ceiling) — it is the only state that earns silence, and the reason is mandatory. `route` is for real debt with a home: it stays visible, labelled with where it went.

Leaving `unclassified` rows to be re-listed by the next run is the standing-backlog failure the `dross-survivor-drain` builtin rule forbids. Do not re-state them as extra findings either — the skeleton already FLAGs each one; your job is to clear them, not to copy them.

## 3. Update `verify.toml`

Edit the skeleton dross wrote in step 1. Specifically:

For each `[[criterion]]` block, fill in:
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

Compute `[verify].verdict`. **Read `mutation_status` first** — when status is not `measured`, the score is a 0/0 artifact and the mutation leg has nothing to say. This is the dogfood-surfaced bug from FeastAhead phase 04/05: Stryker scoped to `src/lib/utils` only, phase touched server/Svelte files, mutation_score landed at 0.0, verdict heuristic falsely flagged `fail` despite 5/5 criteria covered.

**The mutation leg gates on a count, not a ratio.** `[summary].unclassified_in_scope` is the number of survivors inside this phase's own diff carrying no disposition — neither accepted with a reason nor routed to a destination. The bar is zero, with no tolerance band. `mutation_score` is still reported and still worth reading as evidence of how thorough the suite is, but it is **not** a verdict lever: a phase that adds a pile of killed mutants can bury a live one and still clear any cutoff, and a cutoff re-opens the arbitrary-number argument every phase.

If `mutation_status == "measured"`:
- **`pass`** if all criteria are `covered`, `unclassified_in_scope` is 0, and there are no BLOCKING findings.
- **`partial`** if at least one criterion is `weak`, OR there are FLAG findings but no BLOCKING.
- **`fail`** if any criterion is `uncovered`, OR `unclassified_in_scope` > 0, OR any BLOCKING findings exist.

If `mutation_status` is `unmeasurable`, `skipped` or `out-of-scope`, base verdict on criterion coverage alone — nothing was measured, so the mutation leg neither passes nor fails the phase:
- **`pass`** if all criteria are `covered` and no BLOCKING findings. Add a NOTE finding recording why mutation didn't apply (e.g. "mutation unmeasurable: project scope excludes all touched files", "mutation skipped: --skip-mutation passed", or "mutation out-of-scope: every mutant landed in files this phase did not touch").
- **`partial`** if at least one criterion is `weak` OR there are FLAG findings but no BLOCKING.
- **`fail`** if any criterion is `uncovered` OR any BLOCKING findings exist.

Each unclassified in-scope survivor is seeded as its own BLOCKING finding naming `file:line (op)`, so clearing the leg means clearing them individually — kill it with a test, accept it with `dross survivor accept --reason`, or route it with `dross survivor route --target`. Do not clear one by re-routing it to the phase you are verifying.

Add findings as needed (preserve the ones the skeleton seeded from surviving mutants):

```toml
[[finding]]
severity = "BLOCKING"     # BLOCKING | FLAG | NOTE
text     = "criterion c-2 (case-insensitive lookup) has no covering test"
```

## 4. Surface to user

Surface the verdict plus a **compact criterion→test/status mapping** — the judgement the user is being asked to trust. Print this report, never the raw `verify.toml` (per the `verify_surface` decision; the file is the artifact, this is the report):

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

Keep the map to one line per criterion; if the user wants the surviving-mutant detail behind a `weak`/`uncovered` row, point them at `verify.toml` rather than dumping it.

If verdict is `fail` or `partial`, recommend next steps:
- For `uncovered` criteria: "add tests that exercise <criterion> and re-run /dross-verify"
- For `weak` criteria: "tighten assertions in <test> — currently doesn't catch <surviving mutant> kind of breakage"
- For low mutation score: "look at REVIEW.md-style surviving mutants in verify.toml to see what the tests miss"

## 5. Wrap

Record the resolved verdict in telemetry so `dross stats` and downstream gates can see the outcome:
```
dross verify finalize <phase-id>
```
This is only valid after the verdict in `verify.toml` is one of `pass | partial | fail`. If you skipped step 3 or left `verdict = "pending"`, this command will refuse — go back and finalize the file first. Finalize is idempotent (a re-run reports "already recorded"), and it writes a `finalized = true` marker into `verify.toml` — include that change in the verify-artefacts commit below. Safety net: if this step is skipped, `dross ship` and `dross phase complete` auto-finalize a resolved verdict themselves — but run it here anyway so telemetry reflects when the verdict was actually decided.

Update state:
```
dross state set current_phase_status verified
dross state touch "verified <phase-id>: <verdict> (<criteria-covered>/<total>, mutation <score>)"
```

Commit the verify artefacts so `.dross/` doesn't sit dirty (CLI writes the files but doesn't auto-commit):
```
git add .dross/phases/<phase-id>/verify.toml .dross/phases/<phase-id>/tests.json
git commit -m "chore(dross): record verify for <phase-id> (<verdict>)"
```
Use `repo.commit_convention` from project.toml. Skip `tests.json` from the `add` if mutation was skipped (`--skip-mutation`) and the file wasn't written.

If verdict is `pass`:

1. Run `dross phase list` and find the phase immediately after `<id>` in the printed order. Call it `<next-id>`. If `<id>` is the last entry, there is no next phase — the milestone is feature-complete.

2. If `<next-id>` exists, print:
```
Phase <id> verified: pass.

Next:
  /dross-ship              — open PR for this phase (filters .dross/, opens via provider)
      ↳ --draft            — open the PR in draft (work-in-progress, not ready for review)
      ↳ --no-push          — preview the PR body and diff without pushing
  /dross-spec <next-id>    — start the next phase
  dross phase list         — see all phases

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

If `partial` or `fail`:
```
Phase <id> verdict: <verdict>. Open .dross/phases/<id>/verify.toml for full detail.

Next:
  /dross-execute <id>      — amend the failing task (add tests / fix code)
      ↳ --from <task-id>   — resume at the failing task, skipping earlier done tasks
  /dross-verify            — re-run after addressing blocking findings

state is on disk — safe to /clear · fresh session: /dross-execute <id>
```

## Hard rules

- **Follow the interaction playbook (`_interaction.md`); verify.toml is never a review medium.** §4 surfaces the verdict plus a compact criterion→test/status mapping — the report the user must trust — and never pastes the raw `verify.toml` back. Point the user at the file for surviving-mutant detail rather than dumping it.
- **Don't fake coverage.** If you can't find a test that maps to a criterion, mark it `uncovered`. Better to have an honest `fail` verdict than a false `pass`.
- **Capture a stated preference as a rule, don't apply it ad hoc.** If the user pushes back on how the verdict is decided — including asking for a score threshold this project should hold itself to — capture that as a project-scope rule via `/dross-rule` so future verifies inherit it consistently rather than it being re-litigated per run. A rule the user wrote is not a verdict lever this prompt invented.
- **Don't write tests yourself.** /dross-verify is a check, not a fix. If criteria are uncovered, point the user back to /dross-execute (which can amend the failing task) or /dross-plan (to add a test-writing task).
- **Don't skip the cross-check.** Surviving mutants in covered code is the whole point of mutation testing — failing to downgrade `covered` → `weak` when a mutant in the touched file survives is the exact theatrical-coverage problem dross exists to catch.
- **Single pass, no checker loop.** /dross-verify writes a verdict; the user decides what to do. Don't auto-rerun after fixes.
