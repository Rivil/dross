# Panel synthesis — mutation-diff-scope

Judged cold: none of the three drafts is mine. Claims that decided the merge were
checked against the repo, not taken on the drafter's word.

## Scores

| dimension | risk.md | mvp.md | verification.md |
|---|---|---|---|
| criteria coverage | 7/7, every criterion in ≥2 tasks except c-7 (t-2, t-4); the only draft that also covers the *anti*-criterion — a scope that matches nothing (t-8) | 7/7 but thin at the edges: c-5 rests on t-4 alone, so an empty-scope regression has one failure site | 7/7 and the densest mapping (c-3 in four tasks, c-2 in three); no criterion has a single owner |
| test-contract specificity | highest actionable: every contract names the test, the fixture, and the concrete numbers (0.50 vs 0.91/10/1), and is anchored to a real checked-in report | prose contracts with real assertions but no test names; several bundle four independent guarantees into one paragraph, so a partial implementation still reads as "contract met" | highest *diagnostic*: each contract names the regression it catches, and it is the only draft with drift assertions (Σ FileStats.Killed == Report.Killed) that fail an adapter which forgets to populate the new field |
| granularity | 8 tasks; t-1 at five files is the outlier, justified as one package/one layer; t-5 fuses persistence + status + NOTE + summary into one task, which is large but single-agent | 5 tasks, too coarse. t-3 alone owns c-1/c-2/c-4/c-6/c-7, edits three files across two concerns (partition + recompute), and t-4 fuses Go constants with a prompt edit | 8 tasks, most evenly sized; splits hunk parsing (t-3) from scope building (t-2) and persistence (t-5) from arithmetic (t-6) — each task has one reason to fail |
| wave correctness | 5 waves, DAG sound; deps are minimal rather than conservative (t-4 waits on t-1+t-2 only, t-7 on t-5 only) | **defective**: wave 3 holds t-4 and t-5, and t-5 declares `depends: t-3, t-4` — a same-wave dependency. The two also both write `internal/verify/verify.go`, so parallel execution collides | 5 waves, cleanest hygiene; t-5/t-6 are split into consecutive waves *explicitly* to stop two agents editing `verify.go` at once, and t-6/t-7 parallelise only because they touch different files |

**Skeleton: risk.md.** Not because it scores highest on every row —
verification.md beats it on wave hygiene and drift assertions — but because it is
the only draft that found the defect that decides whether this phase ships a real
gate or a vacuous one, and it is correct. Verified in this repo:
`internal/mutation/gremlins.go:364` sets `File: f.Filename` verbatim, and every
checked-in report under `reports/gremlins/` carries a bare basename
(`policy.go`, `changes.go`, `main.go`) while `changes.json` and `git diff` speak
`internal/argfence/policy.go`. A filter that is otherwise perfect scopes out
100% of Go mutants and passes every phase. mvp.md's t-2 contract asserts
`ByFile["a.go"]`, which *bakes the bug in*; verification.md's t-1 catches the
analogous Stryker `Workdir` case but assumes gremlins rows already arrive
repo-relative. Risk also owns the one injection surface here — `changes.json`'s
`base` is attacker-adjacent config reaching `git` argv, and `internal/cmd`
already has the `--end-of-options` fence and its `gitargs_test.go` audit that a
derived base must pass through.

Verification.md's contracts are grafted onto that skeleton wherever they are
sharper, which is often.

## Merged plan

8 tasks across 5 waves.

```
Wave 1

  t-1  Attribute every mutant to its file                    [risk+verification+mvp]
       files:    internal/mutation/adapter.go, internal/mutation/gremlins.go,
                 internal/mutation/stryker.go, internal/mutation/gremlins_test.go,
                 internal/mutation/stryker_test.go
       covers:   c-2, c-4
       depends:  —
       desc:     Add per-file counters to Report (killed/survived/timeout/errors/
                 not-covered), populated in ParseGremlinsJSON and ParseStrykerJSON
                 (the latter also serves stryker-net via delegation), preserved
                 through mergeInto and Stryker.rePrefixFiles. Gremlins re-prefixes
                 its bare `file_name` with the package dir it was invoked for, in
                 both Surviving[].File and the per-file rows.
       contract:
         - per-status attribution [all three]: a report with KILLED+LIVED in
           file1.go and NOT COVERED in file2.go yields {file1: killed=1,survived=1}
           and {file2: survived=1,not_covered=1}, leaving the aggregate
           Killed/Survived totals unchanged.
         - drift assertion [verification]: Σ per-file Killed == Report.Killed, and
           likewise for Survived/Timeout/NotCovered. An adapter that forgets to
           populate the new field fails HERE, not silently three layers downstream.
         - gremlins path re-prefix [risk, repo-verified]: parsing a report that
           names `changes.go` while invoked for `./internal/changes` must surface
           `internal/changes/changes.go` in both Surviving[].File and the per-file
           keys. The checked-in reports/gremlins/*.json carry the bare name — use
           one as the fixture, not a hand-written repo-relative one.
         - merge keeps packages distinct [risk]: `./a` and `./b` each reporting
           `x.go` yield two rows (a/x.go, b/x.go), not one summed entry.
         - merge sums a genuine repeat [verification]: two per-package reports that
           both carry `internal/x/a.go` sum into ONE row. (Compatible with the
           above only once paths are re-prefixed — that is the point.)
         - stryker workdir parity [risk+verification]: Workdir="web" re-prefixes the
           per-file key `src/a.ts` → `web/src/a.ts`, matching its Surviving entry.

  t-2  Scope model, path normalisation and hunk parser                     [risk]
       files:    internal/verify/scope.go, internal/verify/scope_test.go
       covers:   c-6, c-7
       depends:  —
       desc:     verify.Scope — normalised file set, per-file changed line ranges,
                 a Sources/Degraded provenance record, Contains(file) and
                 InHunk(file,line); plus a `@@` unified-diff hunk parser and the
                 in-hunk / inherited-by-proximity classifier. Pure: no git, no I/O.
       contract:
         - normalisation [risk]: `./internal/a.go`, `internal/a.go`,
           `<root>/internal/a.go` and `internal\a.go` collapse to one key and
           Contains() is true for each.
         - hunk arithmetic [risk+verification]: `@@ -1,0 +5 @@` → [5,5];
           `@@ -3,2 +7,3 @@` → [7,9]; `@@ -10,0 +11,3 @@` → [11,13] with line 13
           INSIDE (the exclusive-end off-by-one is the failure being pinned).
         - pure deletion [risk+verification]: `@@ -4,2 +6,0 @@` contributes no range;
           mutants in that file classify as inherited rather than crashing on a
           zero-length range.
         - classifier defaults to proximity [verification]: a line in a file present
           in the map but outside every range returns inherited, NOT in-hunk. A
           classifier that defaults the other way fails here.
         - malformed header degrades [risk]: a garbage `@@` line keeps the ranges
           parsed so far and adds a Degraded entry naming the file, instead of
           erroring the whole parse out.
         - union fails open [risk+verification]: git-side [a.go] ∪ changes-side
           [a.go, b.go] = [a.go, b.go], deduped and sorted; an empty git side still
           yields the changes-side files plus a Degraded entry. A build that
           intersects returns empty and fails.
         - path escape [risk]: a changes.json entry of `../../etc/passwd` or
           `/etc/passwd` is recorded as Degraded and never becomes a Contains match.

Wave 2

  t-3  Derive the phase scope from git                          [risk+verification]
       files:    internal/cmd/verifyscope.go, internal/cmd/verifyscope_test.go
       covers:   c-6
       depends:  t-2
       desc:     Build a verify.Scope for a phase: merge-base(changes.json `base`,
                 HEAD) → `git diff --name-only --no-renames` for files and
                 `git diff -U0` for hunks, unioned with changes.json's per-task
                 files. Every git failure degrades to the changes.json side with a
                 recorded reason. Git calls go through the existing fenced argv
                 builders in internal/cmd.
       contract:
         - fallback [risk+verification]: a phase whose changes.json has no `base`
           still yields the recorded task files, returns no error, and carries a
           Degraded entry naming the missing source.
         - resolved sha, not ref [verification]: Scope records the merge-base SHA,
           not the branch name from changes.json (`Base` is a branch name —
           internal/changes/changes.go:33). Asserting on the resolved sha is what
           makes a stale-base run diagnosable instead of plausible.
         - source provenance [verification]: recorded=nil + git files → source is
           git-only; git-failed + recorded files → changes-only. Proves the union
           is symmetric rather than git-gated.
         - renames [risk]: a.go→b.go renamed in the phase puts BOTH a.go and b.go
           in scope (`--no-renames` emits both sides). See Disagreement D2.
         - quoted paths [risk]: a committed file named `internal/naïve file.go`
           comes back as that path and matches a mutant reported under it.
         - argv fence [risk]: base `--output=<tmp>/pwned` leaves that file
           non-existent, and the gitargs audit fails if the diff refs precede
           `--end-of-options`.
         - hunks collected [risk]: a phase commit editing lines 10-12 of a.go
           yields Hunks["a.go"] = [{10,12}].

  t-4  Filter mutation reports to in-scope mutants               [risk+verification]
       files:    internal/verify/verify.go, internal/verify/verify_test.go
       covers:   c-1, c-2, c-4, c-7
       depends:  t-1, t-2
       desc:     Split every adapter Report against the Scope: counts and Score
                 recomputed from in-scope per-file rows only, out-of-scope survivors
                 collected aside with file/line/op intact, each kept survivor
                 stamped in-hunk or inherited.
       contract:
         - drops the untouched sibling [all three]: a survivor in touched.go and one
           in untouched.go of the same package leaves exactly one entry in Surviving
           and the other in the out-of-scope list.
         - numerator immunity [verification]: a KILLED mutant in untouched b.go
           leaves kept.Killed and kept.Score identical to the same report without it.
         - denominator immunity [verification]: a SURVIVED mutant in b.go likewise
           leaves kept.Score unchanged. Two separate assertions — both fail if the
           filter prunes only the Surviving slice and leaves the counters alone.
         - concrete arithmetic [risk]: killed=1 survived=1 in touched.go plus
           killed=9 in untouched.go gives score 0.50 / killed=1 / survived=1 —
           not 0.91 / 10 / 1.
         - adapter-agnostic table [verification]: one table test over a
           gremlins-parsed and a stryker-parsed report built from the same logical
           mutant set yields identical kept counts and identical dropped counts. A
           filter that special-cases a tool's path shape fails here. Run it inside a
           multi-leg Tests too [risk], so the file-granular leg whose report was
           already narrow is proven to take the same path.
         - every kept survivor tagged [verification]: a survivor with an empty
           origin tag fails. A survivor inside a hunk is in-hunk, one on an
           untouched line of the same file is inherited, and BOTH stay in the
           denominator and in Surviving [risk].
         - dropped list is complete [verification]: dropped survivors preserve
           file/line/op and dropped.Count == len(dropped list) — an implementation
           that discards them fails the empty-list assertion.
         - degenerate inputs [verification]: an in-scope file with zero mutants
           invents no rows; an empty scope yields zero counts, Score 0, and
           everything dropped.
         - failed leg survives filtering [risk]: a leg with Error set and no report
           still appears in Languages and does not nil-panic the filter.

Wave 3

  t-5  Persist scope, out-of-scope survivors, status and NOTE    [risk+verification]
       files:    internal/verify/verify.go, internal/verify/verify_test.go
       covers:   c-2, c-3, c-5, c-6
       depends:  t-4
       desc:     tests.json gains `out_of_scope` (file/line/op per filtered
                 survivor) and `scope` (file set, resolved base, per-source
                 provenance, degraded reasons). Skeleton computes score/status from
                 the FILTERED reports, adds MutationOutOfScope = "out-of-scope",
                 records summary.mutants_in_scope, and emits one NOTE carrying the
                 filtered count.
       contract:
         - persisted, not dropped [risk+verification]: one {file,line,op} entry per
           filtered survivor under `out_of_scope`, and none of them under
           languages[].mutation.surviving. Both halves asserted.
         - round-trip [verification]: tests.json reloads with the scope block
           intact (files, resolved base, provenance, degraded text) — a struct
           missing json tags on the new block fails the reload, not just the write.
         - degraded scope readable after the fact [risk+verification]: a
           changes-only scope serialises its source and its reason, so a stale-base
           run is diagnosable rather than looking clean.
         - filtered score [verification]: 2 in-scope killed + 5 out-of-scope killed
           reports score 1.00 over mutants_in_scope=2. A Skeleton still reading the
           package-wide report fails with 7/7 vs 2/2.
         - own status [risk+verification]: a run where every mutant landed out of
           scope sets mutation_status="out-of-scope" — NOT "measured" with 0.00,
           NOT "unmeasurable" — with mutants_in_scope=0. Assert all three so
           collapsing it into unmeasurable fails.
         - exactly one NOTE [risk+verification]: 7 filtered survivors produce
           exactly ONE NOTE carrying the count 7 and ZERO FLAGs. Assert the count of
           NOTE findings, not its presence — the failure worth pinning is the FLAG
           flood the existing per-survivor loop would produce. Zero out-of-scope
           survivors emit no such NOTE at all.
         - sample-size signal [risk]: verify.toml's [summary] carries
           mutants_in_scope=3 for a 3-mutant in-scope run — the signal the
           small_denominator_gate lock requires in place of a threshold change.
         - adapter invocation untouched [verification]: the fake adapter records its
           argument and the assertion catches a regression that narrows what gets
           MUTATED instead of what gets ATTRIBUTED. (Pairs with t-6's widening —
           see Disagreement D5.)

Wave 4

  t-6  Wire diff scoping into `dross verify`                     [risk+verification]
       files:    internal/cmd/verify.go, internal/cmd/verify_test.go
       covers:   c-1, c-3, c-5, c-6
       depends:  t-3, t-5
       desc:     `dross verify` builds the scope before running adapters, feeds the
                 union to the adapters and to verify.Run, prints in-scope / filtered
                 / degraded lines, and tags the telemetry outcome with the scope
                 status. Unconditional — no flag.
       contract:
         - union is the adapter input [risk]: a file changed in git but absent from
           changes.json is still handed to the adapter and its survivors still gate.
         - real repo [verification]: over a temp git repo with a base commit
           touching a.go and an untouched sibling b.go, output names a.go in the
           scope line and never names b.go as a survivor.
         - summary shows scoping [risk+verification]: output carries the in-scope
           mutant count next to the score (so 0.50 over 2 reads as a small sample)
           and "filtered N out-of-scope survivors", plus a degraded-source line when
           the base was unresolvable. A summary printing score alone fails.
         - out-of-scope prints its own line [verification]: the switch-on-status
           test table gains a row; a status with no case falls through silently and
           fails it.
         - no base still runs [risk+verification]: an unresolvable or absent base
           completes the run, prints the warning, does not abort.
         - deleted paths [risk]: a git-deleted path stays in the scope set for
           filtering but never reaches the adapter's file list (passing it to
           `stryker --mutate` fails the leg).
         - telemetry cannot disagree with verify.toml [verification]: the outcome
           event's mutation_score is computed from the in-scope killed/survived;
           assert the two are equal to catch duplicated-arithmetic drift. Its
           mutation_status tag reads "out-of-scope" on an all-filtered run [risk].

  t-7  Truth-pass the verify prompt and README                   [risk+verification]
       files:    assets/prompts/verify.md, README.md,
                 internal/cmd/verify_prompt_test.go
       covers:   c-3, c-5, c-7
       depends:  t-5
       desc:     Verdict rules gain the out-of-scope status (criterion coverage
                 alone, NOTE recorded), the §4 report block prints the in-scope
                 mutant count beside the score, the survivor cross-check reads
                 in-scope survivors only and weights them by origin tag, and
                 README's verify row states the scoping guarantee. Prompt edits are
                 not live until `make install` (rule r-01).
       contract:
         - all four statuses named [risk+verification]: the prompt names
           measured|unmeasurable|skipped|out-of-scope. Adding the Go constant
           without the prompt line fails. This test is the only gate — `grep`
           confirms mutation_status is absent from internal/configenum, so there is
           no enum-divergence check to catch a missed consumer.
         - non-threshold branch [risk+verification]: assert "out-of-scope" appears
           in the verdict branch that does NOT apply the 0.80/0.60 cutoffs, so
           pasting it into the measured branch fails.
         - reads the out-of-scope list [verification]: substring assertion on
           `out_of_scope`, with the instruction not to re-list those survivors as
           phase findings (r-02's routing lives with the count NOTE and the named
           successor phase, not per-survivor FLAGs).
         - origin weighting [verification]: the cross-check is told to read the
           in-hunk vs inherited tag when weighting a survivor.
         - report block agrees with the CLI [risk+verification]: the §4 template
           carries the in-scope count alongside score, matching printVerifySummary.
         - docs no longer claim package-wide attribution [risk]: README's
           `dross verify` row says the score covers only the phase's changed files.

Wave 5

  t-8  Regression: an untouched survivor never gates                       [risk]
       files:    internal/cmd/verify_scoping_test.go
       covers:   c-1, c-3, c-5
       depends:  t-6
       desc:     End-to-end through `dross verify` over a real temp git repo with a
                 stub adapter: mutants in a touched file and an untouched file of
                 the same package.
       contract:
         - attribution holds end to end: the untouched sibling's survivor appears
           only under out_of_scope in tests.json and produces no FLAG in
           verify.toml, while the touched file's survivor produces one.
         - the vacuous pass is not reachable: an adapter whose report paths match
           nothing in scope yields mutation_status="out-of-scope" and a NOTE naming
           the count — never verdict inputs indistinguishable from a clean measured
           run.
         - no opt-out: no flag on `dross verify` disables scoping (scoping_always_on
           lock).
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 (survivors only from changed files) | t-4, t-6, t-8 |
| c-2 (score from in-scope mutants only)  | t-1, t-4, t-5 |
| c-3 (out-of-scope reported, not discarded) | t-5, t-6, t-7, t-8 |
| c-4 (adapter-agnostic)                  | t-1, t-4 |
| c-5 (empty in-scope set has a status)   | t-5, t-6, t-7, t-8 |
| c-6 (scoped file set recorded)          | t-2, t-3, t-5, t-6 |
| c-7 (in-hunk vs inherited tag)          | t-2, t-4, t-7 |

7/7 covered; no criterion has a single failure site.

### What each draft contributed

- **risk** — the skeleton: the gremlins re-prefix precondition (t-1), the argv
  fence on a config-derived base (t-3), path-escape rejection (t-2), the deleted-
  path carve-out (t-6), README in the truth pass (t-7), and t-8 entire.
- **verification** — most of the sharpened contracts: drift assertions, the
  numerator/denominator immunity pair, the adapter-agnostic table over two
  parsers, resolved-sha-not-ref, the round-trip assertion, the
  telemetry/verify.toml equality check, origin weighting in the prompt.
- **mvp** — confirmation that a smaller shape is achievable (its 5-task cut
  covers 7/7), the observation that StrykerNet inherits attribution for free via
  ParseStrykerJSON, and the explicit rejection of a `--no-scope` escape hatch and
  a standalone `dross scope` subcommand. Its wave-3 self-dependency and its
  `ByFile["a.go"]` contract (which encodes the basename bug) kept it out of the
  skeleton slot.

## Disagreements

Eight. Each carries a provisional default; none is silently resolved.

**D1 — Where scope and filtering live.**
risk: pure `Scope` in `internal/verify`, git derivation in `internal/cmd`
(reuses the existing fenced argv builders and their `gitargs_test.go` audit).
mvp: same split, git inline in `internal/cmd/verify.go`.
verification: a new `internal/scope` package with a `GitFn` seam, and the filter
in `internal/mutation/filter.go` rather than `internal/verify`.
*Default taken:* risk's split — pure Scope + hunk parser in `internal/verify`,
git in `internal/cmd`, filter in `internal/verify`.
*Why it matters:* verification's argument for `internal/mutation` is the best
single argument in any draft — c-4 says the guarantee holds across adapters, and
one exported function over `*Report` is the only shape where a single table test
*proves* that rather than asserting it. It was not taken because the Scope type
lives in `internal/verify` under this default, and putting the filter in
`internal/mutation` would need either an import edge or the callback indirection
verification designed around it. The mitigation is in t-4's contract: the
adapter-agnostic table test is mandatory and must run over two independently
parsed reports, so c-4 stays a tested property wherever the function sits. If
implementation friction shows up in t-4, moving the filter to `internal/mutation`
with an origin callback is verification's design and is a clean fallback.

**D2 — Rename handling.**
risk: `git diff --no-renames`, both sides of a rename land in scope.
verification: a `rename to` header attributes hunks to the NEW path; keying by
the old path is an explicit test failure.
*Default taken:* risk — `--no-renames`, both sides in scope.
*Why it matters:* they give different answers for the same phase. Under
verification, a mutant reported under the pre-rename path is out of scope and
does not gate. Under risk, it does. The `scope_source` lock's stated rationale is
that the union "fails open — scoping wider", and the old path costs nothing since
a renamed-away file has no mutants to attribute. Verification's rule is the
tighter one and would be right if scoping were meant to be exact; the lock says
it is meant to be generous.

**D3 — Where the out-of-scope list lives in tests.json.**
risk and mvp: one top-level `out_of_scope` list on `Tests`.
verification: per-language — `languages[].out_of_scope` — with an explicit
contract that a single merged list FAILS.
*Default taken:* top-level (2-1, and the `out_of_scope_surface` lock says
"under their own key in tests.json", singular).
*Why it matters:* the survivor-lifecycle phase is the named consumer of this
list. Per-language keeps the tool/language attribution that a multi-leg run
produces; top-level loses it unless each entry carries its own language field. If
the merged plan keeps top-level, entries should carry the language — otherwise
this decision is reopened by the consumer, not by us.

**D4 — Does a degraded scope raise a FLAG?**
mvp: yes — a failing git leg "records a FLAG so the narrowed scope is visible,
never silent".
risk and verification: no — a Degraded/Warning record plus a printed line.
*Default taken:* record and print, no FLAG.
*Why it matters:* FLAGs feed the verdict. A missing `base` in changes.json — the
common degraded case, and the exact one the `scope_source` lock's fail-open
design anticipates — would then fail phases for a bookkeeping gap rather than a
test gap. mvp's concern is real (a silently narrowed scope is the second failure
mode of this phase); it is answered by c-6's persistence and the printed line
rather than by the verdict.

**D5 — Does the scope narrow what gets mutated, or only what gets attributed?**
risk t-6: the adapter's input becomes the UNION, widening it beyond changes.json
so a git-changed file is not left un-mutated and therefore un-gated.
verification t-5: an explicit contract that the adapter still receives the
unfiltered list it was dispatched for — "filtering is post-Report, not a change
to what gets mutated".
*Default taken:* both, because they act at different layers — `internal/cmd`
widens the dispatch list to the union (t-6), `verify.Run` never narrows it (t-5).
*Why it matters:* read at the same layer these contracts contradict each other,
and a plan that carried both without this note would ship two tests that cannot
both pass. The layering is what reconciles them, and it has to be stated in the
task descriptions or the second implementer will delete one of the tests.

**D6 — Per-file counts as a map or a slice.**
risk and mvp: `map[string]FileStat`. verification: `Files []FileStats{File,...}`.
*Default taken:* map (2-1, and lookup is what the filter does).
*Why it matters:* only at the serialisation boundary — these rows reach
tests.json, and a Go map marshals in sorted key order while a slice preserves
insertion order across a merge. Either is deterministic; the map is the one that
stays deterministic without the merge caring.

**D7 — Prompt edit: its own task, and does README come with it?**
risk: t-7, its own task, with README, depends on t-5.
verification: t-8, its own task, final wave, no README.
mvp: folded into the status task (t-4), arguing the constant and the prompt
branch that consumes it must ship together or the LLM applies 0.60 thresholds to
a status it does not know.
*Default taken:* risk — a separate task, in wave 4, including README.
*Why it matters:* mvp's risk is real and there is no enum gate to catch it
(`mutation_status` is absent from `internal/configenum`, verified). It is
contained by t-7's first contract — the prompt test fails if the Go constant
exists without the prompt line — and by t-7 landing one wave after t-5 rather
than at the end of the phase, which is why it is not in wave 5 with t-8. README
is included because the current verify row documents package-wide attribution and
would become false the moment t-6 lands.

**D8 — Git invocation shape.**
mvp: a single `git diff --unified=0`, parsing `+++ b/` for the file set and `@@`
for ranges — "cheaper than two invocations and cannot disagree with itself about
which files changed".
risk and verification: merge-base, then `--name-only --no-renames` for files,
then `-U0` for hunks.
*Default taken:* the multi-call form.
*Why it matters:* mvp's self-consistency argument is correct and the default
gives it up — two commands can in principle disagree about the file set. It was
not taken because `--no-renames` (D2) has no `--unified=0` equivalent that
preserves both sides, and because a single diff makes the file set depend on the
`+++` parser being correct for every header form (binary files, mode-only
changes, pure renames) where `--name-only` is unambiguous. If D2 is ever resolved
verification's way, mvp's single-call form becomes the better shape.
