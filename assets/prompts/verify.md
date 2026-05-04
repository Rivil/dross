# /dross-verify

Decide whether a phase actually delivered what its `spec.toml` promised — not by counting tasks, by checking that **tests catch breakage** and **every acceptance criterion has a real test that would fail when it broke**.

Three checks, in order: mutation efficacy (mechanical), criterion-to-test mapping (LLM judgement), final verdict.

## 0. Pre-flight

1. Run `dross rule show` and treat output as MUST-FOLLOW.
2. Resolve target phase from `$ARGUMENTS` or `state.json`'s `current_phase`. Fail if neither resolves.
3. Read `.dross/phases/<id>/spec.toml` and `plan.toml`. If either is missing, route to `/dross-spec` or `/dross-plan` first.
4. Read `.dross/phases/<id>/changes.json`. If missing or empty: `/dross-execute` hasn't touched anything for this phase yet — stop and route there.
5. Parse `--skip-mutation` flag. Default OFF (run mutation testing). Skip if user explicitly asked.

## 1. Mechanical pass — `dross verify`

Run:
```
dross verify <phase> [--skip-mutation]
```

This shells out to mutation tools (currently Stryker for TS/JS/Svelte; other languages skip with a reason), parses the JSON reports, and writes:

- `.dross/phases/<id>/tests.json` — raw machine output, killed/survived counts per language
- `.dross/phases/<id>/verify.toml` — skeleton verdict with `verdict = "pending"` and per-criterion `status = "unknown"`

**Read both files before continuing.** They're the inputs for the LLM judgement step.

If mutation testing fails to run (e.g. Stryker not installed), surface the error to the user and ask:
- `install Stryker` — guide them through `pnpm add -D @stryker-mutator/core` (or equivalent for their package manager from `project.toml`)
- `skip mutation` — re-run with `--skip-mutation`; verdict will note that mutation efficacy is unverified
- `abort`

## 2. Criterion-to-test mapping (the LLM judgement step)

For each criterion in `spec.toml`, find the test(s) that would fail if that criterion broke. This is where you actually *do* the verify work.

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

For each surviving mutant in `tests.json`:
- Does the mutated line participate in any criterion's covering test?
- If yes: the test exists but doesn't catch this kind of breakage → downgrade that criterion from `covered` to `weak`.
- If no: less concerning, but still surface as a FLAG finding ("survived mutant in <file>:<line>").

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
mutation_score     = <from tests.json — preserve>
mutants_killed     = <preserve>
mutants_survived   = <preserve>
criteria_total     = <count of criteria>
criteria_covered   = <count where status=covered>
criteria_uncovered = <count where status=uncovered or weak>
```

Compute `[verify].verdict`:
- **`pass`** if all criteria are `covered`, mutation score ≥ 0.80, no BLOCKING findings.
- **`partial`** if at least one criterion is `weak` OR mutation score is between 0.60-0.80 OR there are FLAG findings but no BLOCKING.
- **`fail`** if any criterion is `uncovered`, OR mutation score < 0.60, OR any BLOCKING findings exist.

Don't tune the thresholds without flagging it. The 0.80/0.60 mutation cutoffs are heuristics — if the user wants different values for their project, they can edit verify.toml manually after.

Add findings as needed (preserve the ones the skeleton seeded from surviving mutants):

```toml
[[finding]]
severity = "BLOCKING"     # BLOCKING | FLAG | NOTE
text     = "criterion c-2 (case-insensitive lookup) has no covering test"
```

## 4. Surface to user

Print a compact summary, not the full file:

```
verify <phase-id> — <verdict>

  Mutation:    score=<X.XX> killed=<N> survived=<M>
  Criteria:    <covered>/<total> covered, <weak> weak, <uncovered> uncovered

  Findings:
    BLOCKING (<count>):
      - <one-line per blocking>
    FLAG (<count>):
      - <one-line per flag>
    NOTE (<count> — see verify.toml)

  Verdict: <pass | partial | fail>
```

If verdict is `fail` or `partial`, recommend next steps:
- For `uncovered` criteria: "add tests that exercise <criterion> and re-run /dross-verify"
- For `weak` criteria: "tighten assertions in <test> — currently doesn't catch <surviving mutant> kind of breakage"
- For low mutation score: "look at REVIEW.md-style surviving mutants in verify.toml to see what the tests miss"

## 5. Wrap

Update state:
```
dross state set current_phase_status verified
dross state touch "verified <phase-id>: <verdict> (<criteria-covered>/<total>, mutation <score>)"
```

Commit the verify artefacts so `.dross/` doesn't sit dirty (CLI writes the files but doesn't auto-commit):
```
git add .dross/state.json .dross/phases/<phase-id>/verify.toml .dross/phases/<phase-id>/tests.json
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
  /dross-spec <next-id>    — start the next phase
  dross phase list         — see all phases
```

3. Otherwise (last phase in the milestone), print:
```
Phase <id> verified: pass. This is the last phase in the milestone.

Next:
  /dross-ship              — open PR for this phase
  dross milestone show     — review milestone status before tagging the release
```

If `partial` or `fail`:
```
Phase <id> verdict: <verdict>. Open .dross/phases/<id>/verify.toml for full detail.

Next:
  /dross-execute <id>      — amend the failing task (add tests / fix code)
  /dross-verify            — re-run after addressing blocking findings
```

## Hard rules

- **Don't fake coverage.** If you can't find a test that maps to a criterion, mark it `uncovered`. Better to have an honest `fail` verdict than a false `pass`.
- **Don't tune thresholds silently.** If the user pushes back ("0.80 is too strict for this codebase"), capture that as a project-scope rule via `/dross-rule` ("mutation score threshold is 0.70 for this project") so future verifies inherit it consistently.
- **Don't write tests yourself.** /dross-verify is a check, not a fix. If criteria are uncovered, point the user back to /dross-execute (which can amend the failing task) or /dross-plan (to add a test-writing task).
- **Don't skip the cross-check.** Surviving mutants in covered code is the whole point of mutation testing — failing to downgrade `covered` → `weak` when a mutant in the touched file survives is the exact theatrical-coverage problem dross exists to catch.
- **Single pass, no checker loop.** /dross-verify writes a verdict; the user decides what to do. Don't auto-rerun after fixes.
