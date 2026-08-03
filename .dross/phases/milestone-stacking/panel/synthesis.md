# Panel synthesis — milestone-stacking

Judged cold: I authored none of the three drafts. File paths and claims were
checked against the tree (`internal/milestone/milestone.go`,
`internal/cmd/milestone.go`, `internal/cmd/milestone_stale.go`,
`internal/ship/{merged,open}.go`, `internal/cmd/json_tag_parity_test.go`,
`assets/prompts/milestone.md`, `README.md`, `docs/roadmap.md`).

## Scores

Scale: 1–5.

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| **risk** (7 tasks / 3 waves) | 5 — all six, c-5 covered twice (runtime narration in t-4, docs in t-7); only draft that catches `milestoneFinalize`'s hardcoded `origin/<main>` ancestry guard, without which a child merged into its parent can never be finalized | 4 — richest mutant-framing ("collapsing 'could not ask' into 'no PRs' fails…"), but t-5 asserts "the captured gh args" when the actual `milestone complete` fixture is a **forgejo httptest** (`milestoneOpenFixture`/`msPRCapture`, `internal/cmd/milestone_test.go:470`); "TestJSONTagParity" is really `TestTomlFieldsCarryMatchingJSONTags` in `internal/cmd/json_tag_parity_test.go` (mechanism real — `milestone.Milestone` is a schema root, so a new `Base` field is auto-gated) | 4 — t-6 carries c-4 + c-6 + two command wirings in one task; everything else well-sized | 4 — three pure engines parallel in wave 1 is right; wave 2's t-4/t-5/t-6 all edit `internal/cmd/milestone.go` and serialize |
| **mvp** (5 tasks / 4 waves) | 3 — all six, but c-1+c-2+c-3 collapse into a single task; no back-compat cases | 4 — contracts are the most faithful to the repo's real machinery (forgejo `POST /pulls` body, `milestone.md:14`, README row at ~190 — all verified accurate); thinner per-branch coverage | 2 — t-2 does create + complete + `--base` + narration in one unit; one red task blocks the whole phase | 2 — 4 waves for 5 tasks. `ship.OpenPRsTargeting` is pushed to wave 4 behind the toml scan though it has no dependency on it; t-3 depends on t-2 (the *writer*) when it only needs the reader |
| **verification** (8 tasks / 3 waves) | 5 — per-criterion mapping plus explicit back-compat: pre-v1.2 fixture toml, `TestMilestoneCompleteOpensSinglePRToMain`, `TestPruneDeletesOnlyStaleBranches`, `TestMilestoneCreateNoGitSkips` must all keep passing (all four verified to exist) | 5 — anchors contracts to named existing fixtures (`milestoneFinalizeFixture(t, bool)` — exists, `internal/cmd/milestone_test.go:550`) and states the mutant each case kills; one overreach: t-3 specifies forgejo/gitea + gitlab REST arms, which `PRMerged`'s precedent (github + bitbucket only) does not support | 5 — finest split; separating the toml gate (t-6) from the provider gate (t-7) is what makes c-6's announced-skip contract testable on its own | 5 — 3 waves, three parallel engines in wave 1, two independent tasks in wave 3; same-file serialization acknowledged rather than hidden |

**Skeleton: `verification`.** It has the correct wave shape, the finest
granularity where the criteria actually split (c-4 vs c-6 are different
gates with different failure modes), and its contracts are the only ones that
name the existing tests that must survive — the cheapest available guard
against this phase regressing the milestone commands it rewires. `risk` is a
close second and supplies most of the grafts below.

## Merged plan

8 tasks across 3 waves. Origin tags mark where each task and each grafted
contract clause came from.

```
Wave 1

  t-1  Record the cut base in milestone toml                    [verification]
       files:    internal/milestone/milestone.go,
                 internal/milestone/milestone_test.go
       covers:   c-2
       desc:     Add `Base string \`toml:"base,omitempty" json:"base,omitempty"\`` to
                 milestone.Meta and `(*Milestone) BaseOr(mainBranch string) string`
                 returning mainBranch when Base is empty (absent_base_reads_main).
                 Add `LoadAll(root) (map[string]*Milestone, error)` that errors —
                 never silently short-maps — on an unreadable toml.       [+risk]
       depends:  —
       contract: - Save/Load round-trips Base="milestone/v1.1"; dropping the toml
                   tag makes the round-trip read back "".        [verification+risk]
                 - BaseOr("main") on a Meta with no base returns "main"; removing
                   the empty guard returns "" and every consumer targets nothing.
                                                                 [verification+mvp]
                 - A pre-v1.2 fixture toml (version/title/status/started/scope/
                   phases, no `base` key) decodes without error — making the field
                   required breaks every milestone shipped before v1.2.
                                                                    [verification]
                 - LoadAll over a directory holding one syntactically broken toml
                   returns an error naming that file rather than a short map; a
                   silent skip fails the fail-closed test.                  [risk]
                 - The existing schema-wide parity gate
                   (TestTomlFieldsCarryMatchingJSONTags,
                   internal/cmd/json_tag_parity_test.go — milestone.Milestone is
                   already a schemaRoot) fails if Base carries a toml tag and no
                   matching json tag. No new test needed; do not break it.
                                                          [risk, name corrected]

  t-2  Merged-status probe with offline fallback                 [verification]
       files:    internal/cmd/milestone_merged.go,
                 internal/cmd/milestone_merged_test.go
       covers:   c-1, c-3
       desc:     milestoneMergedIntoMain(repoDir, branch, mainBranch)
                 (merged, localOnly bool, err error) — best-effort `fetch origin`,
                 then `merge-base --is-ancestor origin/<branch> origin/<main>`,
                 falling back to refs/heads when the fetch or the remote refs are
                 unavailable (locked unmerged_test). Returns localOnly so callers
                 can narrate the offline caveat rather than hiding it.  [+risk]
                 Note: internal/cmd/milestone_stale.go already has
                 isAncestor(repoDir, ref, other) — reuse it rather than
                 re-shelling merge-base.                          [judge, verified]
       depends:  —
       contract: - Against milestoneFinalizeFixture(t, true) the probe returns
                   merged=true; the (t, false) shape returns false. Swapping the
                   is-ancestor argument order flips both.              [verification]
                 - With origin's URL rewritten to a nonexistent path the probe
                   still answers from refs/heads and returns localOnly=true;
                   deleting the fallback returns an error, which makes
                   create/complete/prune unusable offline.       [verification+mvp]
                 - A branch origin has never seen is answered from local refs with
                   localOnly=true, not an "unknown revision" error.   [verification]
                 - A probe keyed on milestone.status instead of ancestry passes the
                   unmerged case and fails the merged-but-still-active case.
                                                                            [risk]

  t-3  ship.OpenPRsTargeting provider query               [verification+risk+mvp]
       files:    internal/ship/basepr.go, internal/ship/basepr_test.go
       covers:   c-6
       desc:     OpenPRsTargeting(opts OpenOpts, base string) ([]BasePR, error)
                 mirroring PRMerged's shape: github via the unexported ghCommand
                 seam (`gh pr list --base <b> --state open --json
                 number,title,url,headRefName`), ErrBasePRLookupUnsupported for the
                 unwired providers, a plain error for an unknown one; plus the
                 exported OpenPRsTargetingFunc override seam package cmd needs.
       depends:  —
       contract: - github: ghCommand stubbed to emit one PR yields one BasePR with
                   that number and URL.                        [all three]
                 - A stub exiting non-zero returns an error, never an empty slice —
                   an empty slice reads as "no dependents" and authorizes a delete.
                                                               [risk+verification]
                 - Malformed JSON on stdout returns a parse error, not zero PRs.
                                                                            [risk]
                 - An unwired provider returns ErrBasePRLookupUnsupported
                   (errors.Is) without shelling out — the test fails if any exec
                   happens.                                     [risk+verification]
                 - OpenPRsTargetingFunc is a non-nil var defaulting to
                   OpenPRsTargeting.                                [verification]

Wave 2 (depends t-1, t-2)

  t-4  Cut from the recorded parent and record it       [verification+risk+mvp]
       files:    internal/cmd/milestone.go,
                 internal/cmd/milestone_stacking_test.go
       covers:   c-1, c-2, c-5
       desc:     milestoneCreate resolves the cut point from state.current_milestone
                 via t-2's probe (unmerged parent → milestone/<cur>, else main),
                 passes it to ensureMilestoneBranch (which currently hardcodes
                 mainBranch, internal/cmd/milestone.go:342), writes it to the new
                 milestone's `base` before the push, and prints which branch it cut
                 from plus the offline caveat when the answer came from local refs.
                 Adds `--base <branch>` (validated against local refs) to force the
                 cut point (locked base_override). current_milestone is read before
                 any state write, and is ignored when it names the version being
                 created.                                                   [+mvp]
       depends:  t-1, t-2
       contract: - current_milestone=v1.1 with milestone/v1.1 unmerged: `milestone
                   create v1.2` leaves `rev-parse milestone/v1.2` ==
                   `rev-parse milestone/v1.1` AND writes base = "milestone/v1.1"
                   into v1.2.toml — recording without cutting, or cutting without
                   recording, fails.                             [all three]
                 - After the v1.1→main merge is simulated on origin, the same
                   create cuts at main and records "main". A create keyed on
                   milestone.status passes the first case and fails this one.
                                                                    [verification]
                 - current_milestone unset while an unmerged milestone/v1.1 exists:
                   cut point and recorded base are both "main" (locked
                   stacking_parent) — a ref-scanning implementation fails here.
                                                               [risk+verification]
                 - `--base milestone/v0.9` wins over the resolver and is recorded
                   verbatim.                                       [all three]
                 - `--base nope` fails with "no such local branch" leaving neither
                   a toml nor a branch behind (atomicity).                  [risk]
                 - The printed line names the branch actually cut from; a hardcoded
                   "from main" string fails the stacked case.        [risk+mvp]
                 - Non-git dir: create still writes the toml, records nothing, and
                   exits 0 — TestMilestoneCreateNoGitSkips keeps passing.
                                                               [verification+risk]

  t-5  Target the recorded parent in the milestone PR    [verification+risk+mvp]
       files:    internal/cmd/milestone.go,
                 internal/cmd/milestone_pr_base_test.go
       covers:   c-3
       desc:     milestoneComplete's open mode reads BaseOr and sets opts.BaseBranch
                 to it (replacing `opts.BaseBranch = mainBranch`,
                 internal/cmd/milestone.go:173) while t-2's probe says that parent
                 is unmerged, falling back to main once it has merged or its origin
                 ref is gone. milestoneFinalize's ancestry guard checks the same
                 resolved target instead of hardcoded origin/<main>.        [risk]
       depends:  t-1, t-2
       contract: - v1.2.toml with base="milestone/v1.1", v1.1 unmerged: the
                   milestoneOpenFixture mock's first POST /pulls body carries
                   base="milestone/v1.1", head="milestone/v1.2" — so the diff is
                   v1.2's own commits, not v1.1's.              [verification+mvp]
                 - Same toml once origin/main contains milestone/v1.1: the POST
                   body carries base="main" — never a branch --finalize is about to
                   delete.                                      [verification+mvp]
                 - With origin/milestone/v1.1 deleted outright it targets main
                   rather than erroring on a dead ref.                      [risk]
                 - v1.2.toml with no `base` key: POST body base="main" (locked
                   absent_base_reads_main) — TestMilestoneCompleteOpensSinglePRToMain
                   still passes unchanged.                          [verification]
                 - The already-exists (409) idempotent path reports the resolved
                   base, not main.                                          [risk]
                 - A milestone recording its own branch as base is refused rather
                   than opening a self-targeted PR.                         [risk]
                 - `complete v1.2 --finalize` after v1.2 merged into
                   milestone/v1.1 but not yet main passes its ancestry guard — a
                   guard still fixed on origin/main refuses forever.        [risk]

  t-6  Refuse deletes that strand an unmerged child      [verification+risk+mvp]
       files:    internal/cmd/milestone_dependents.go, internal/cmd/milestone.go,
                 internal/cmd/milestone_dependents_test.go
       covers:   c-4
       desc:     dependentMilestones(root, repoDir, branch, mainBranch) uses
                 LoadAll to scan .dross/milestones/*.toml for milestones whose
                 BaseOr equals branch and which t-2's probe reports unmerged;
                 milestonePrune and milestoneFinalize call it before any branch
                 delete and refuse, naming the dependent version.
       depends:  t-1, t-2
       contract: - v1.3.toml records base="milestone/v1.2" and v1.3 is unmerged:
                   `milestone prune` (with milestone/v1.2 squash-merged so the
                   stale detector names it) exits non-zero with "v1.3" in the
                   message, and refs/heads/milestone/v1.2 plus origin/milestone/v1.2
                   both still exist afterwards.                    [all three]
                 - `milestone complete v1.2 --finalize` under the same shape
                   refuses naming v1.3, deletes neither ref, and leaves local main
                   un-fast-forwarded.                          [verification+mvp]
                 - Once v1.3 is merged into origin/main, prune deletes
                   milestone/v1.2 — a gate keyed on "a dependent record exists"
                   rather than "an *unmerged* dependent exists" wedges the repo
                   permanently.                                    [all three]
                 - A milestone toml with no `base` key is never a dependent of
                   anything, so TestPruneDeletesOnlyStaleBranches keeps passing.
                                                               [verification+risk]
                 - A milestone naming itself as base does not self-block.  [risk]
                 - An unreadable milestone toml makes the gate refuse (fail closed)
                   rather than delete on a short scan.                     [risk]

Wave 3

  t-7  Provider open-PR gate with announced skip                 [verification]
       files:    internal/cmd/milestone_dependents.go,
                 internal/cmd/milestone_provider_gate_test.go
       covers:   c-6
       desc:     After the toml scan, when [remote].provider/.url are configured,
                 call ship.OpenPRsTargetingFunc for the branch about to be deleted
                 and refuse naming any open PR. An unconfigured provider or a
                 lookup error prints an explicit skip line and proceeds on the toml
                 scan alone (locked dependent_detection).
       depends:  t-3, t-6
       contract: - Seam stubbed to return PR #7 based on milestone/v1.2: prune
                   refuses with "#7" in the message and deletes nothing, even
                   though the toml scan found no dependent.    [verification+mvp]
                 - Seam stubbed to return an error: the command prints a skip line
                   naming the reason, then applies the toml scan — a clean scan
                   deletes and exits 0. A silent swallow fails the output
                   assertion; a hard error fails the exit-0 assertion.
                                                           [verification+mvp+risk]
                 - No [remote].provider configured: same skip line, and the stub
                   records zero invocations.                  [verification+mvp]
                 - The recorded stub argument is the branch being deleted
                   (milestone/v1.2), not main and not the milestone version.
                                                                    [verification]

  t-8  Rewrite the branch-cut narration in prompt and docs [verification+risk+mvp]
       files:    assets/prompts/milestone.md, README.md, docs/roadmap.md,
                 internal/cmd/milestone_narration_test.go
       covers:   c-5
       desc:     Replace the unconditional claim in assets/prompts/milestone.md:14
                 ("cuts + pushes the `milestone/<version>` integration branch from
                 main") with the conditional rule plus `--base`; extend README's
                 `dross milestone {…}` row (README.md:190) with the conditional
                 cut, the recorded base and `--base`; state the rule in
                 docs/roadmap.md's `milestone-branch-model` entry (line 310). A
                 grep test pins all three. Rule r-01: `make install` after the
                 prompt edit.                                              [+mvp]
       depends:  t-4
       contract: - The test fails while assets/prompts/milestone.md still contains
                   the literal "integration branch from main".      [all three]
                 - Each of the three files must name both arms — the current
                   milestone's branch when unmerged, main otherwise — so deleting
                   the old claim without stating the new rule still fails.
                                                               [verification+mvp]
                 - The README milestone row must mention `--base`; dropping the
                   flag from the docs while it exists in the CLI fails the row
                   grep.                                             [risk+mvp]
                 - The failure message names the file that still carries the
                   unconditional claim, so a partial doc pass is not green.
                                                                    [verification]
                 - Note (verified): the literal unconditional phrase exists **only**
                   in assets/prompts/milestone.md:14. README:190 and
                   docs/roadmap.md:310 make no cut-point claim today, so their
                   edits are additive — a test asserting the phrase's
                   presence-then-absence in all three files would be vacuous there.
                                                                [judge, verified]
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 | t-2, t-4 |
| c-2 | t-1, t-4 |
| c-3 | t-2, t-5 |
| c-4 | t-6 |
| c-5 | t-4, t-8 |
| c-6 | t-3, t-7 |

6/6 covered. No task introduces work outside the six criteria.

### Serialization note

t-4, t-5 and t-6 all edit `internal/cmd/milestone.go`, so wave 2 parallelizes
by *review* but not by *edit* — verification called this out and it is the
correct read; risk's plan has the same property without naming it.

## Disagreements

**1. Granularity of the command rewire — one task or three.**
`mvp` merges `milestone create` and `milestone complete` into a single task
(t-2, covering c-1+c-2+c-3) on the grounds that both live in
`internal/cmd/milestone.go` and are the write/read halves of one fact.
`risk` and `verification` split them (create / complete / dependents).
**Default taken: split (3 tasks).** *Why it matters:* the merged task is a
single point of failure across three criteria — a red there leaves the phase
with no partial ground, and the atomic-commit rule means one commit carries the
whole branch model. The cost of the split is real (same-file serialization, see
above); if execution proves the three tasks are constantly rebasing on each
other, mvp's merge is the fallback.

**2. Where `ship.OpenPRsTargeting` sits in the wave graph.**
`risk` and `verification` put it in wave 1 as a standalone engine with no
dependency on the toml scan. `mvp` puts it last, in wave 4, behind the
dependent-scan task. **Default taken: wave 1.** *Why it matters:* it is the
only genuinely parallelizable unit of c-6, and mvp's ordering serializes the
phase into four waves for five tasks with no dependency justifying it.

**3. Provider arms for `OpenPRsTargeting`.**
`risk` and `mvp` implement github only, with an `Err…Unsupported` sentinel for
everything else, following `PRMerged`'s precedent (verified: github +
bitbucket implemented, gitlab/forgejo/gitea unsupported).
`verification` additionally specifies forgejo/gitea and gitlab REST arms.
**Default taken: github only + sentinel.** *Why it matters:* the spec's
`dependent_detection` decision makes the skip path first-class, so unwired
backends are already a supported outcome, and github is this repo's provider.
The tension worth flagging: the existing `milestone complete` cmd fixture is
**forgejo** (`milestoneOpenFixture`), so with a github-only implementation the
c-6 gate can only ever be exercised in cmd tests through the
`OpenPRsTargetingFunc` stub — which is exactly how t-7's contract is written,
so this is coherent, not broken. If a later phase wants an unstubbed cmd-level
c-6 test, verification's forgejo arm is the thing to add.

**4. Whether `milestoneFinalize`'s ancestry guard is retargeted (grafted into t-5).**
Only `risk` includes it, and flags it as reading past c-3's literal text.
`mvp` and `verification` leave the guard on `origin/<main>`.
**Default taken: include it in t-5.** *Why it matters:* without it a stacked
child that merged into its parent but not yet into main can never be finalized
— the stacking model's second half deadlocks. Against that: it is the one place
the merged plan exceeds the literal criteria text, and a reviewer could
legitimately route it to a follow-up phase instead.

**5. Fail-closed vs skip-and-continue on an unreadable milestone toml.**
`risk` requires `LoadAll` to error (never short-map) and the delete gate to
refuse on a parse error. `mvp` and `verification` specify neither behaviour.
**Default taken: fail closed (risk's contract grafted into t-1 and t-6).**
*Why it matters:* a parse error is indistinguishable from "no dependents", and
the consequence of guessing wrong is an irreversible remote branch delete under
someone's open PR. The cost is that one malformed toml anywhere in
`.dross/milestones/` blocks every prune until it is fixed.

**6. Where the base accessor lives, and API naming.**
`verification`: `(*Milestone) BaseOr(mainBranch string)` in `internal/milestone`.
`risk`: `RecordedBase()` in `internal/milestone`, hardcoding "main".
`mvp`: `milestoneRecordedBase(root, version, mainBranch)` in `internal/cmd`,
keeping `internal/milestone` pure toml I/O.
Sentinel names diverge three ways: `ErrBasePRQueryUnsupported` (risk) /
`ErrPRListUnsupported` (mvp) / `ErrBasePRLookupUnsupported` (verification).
**Defaults taken: `BaseOr(mainBranch)` on the milestone package;
`ErrBasePRLookupUnsupported`.** *Why it matters:* `BaseOr` taking the main
branch as an argument is the only variant that honours `absent_base_reads_main`
for a repo whose `git_main_branch` is `master` without the schema layer
importing project config — risk's hardcoded "main" is wrong for those repos.
The sentinel name is cosmetic; it is recorded only so the three drafts' test
contracts are not read as describing three different APIs.

**7. What the dependent scan depends on.**
`mvp` makes it depend on the create-side writer (its t-2), reasoning that
before the writer there is nothing on disk to find. `risk` and `verification`
depend only on the schema reader. **Default taken: depend on the reader
(t-1, t-2), placing the gate in wave 2.** *Why it matters:* the scan's tests
write their own fixture tomls, so the runtime writer is not a precondition;
mvp's edge pushes the gate a wave later than it needs to be.
