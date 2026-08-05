# Panel synthesis — config-trust-hardening

Judge did not author any of the three drafts. Every file path carried into the
merged plan below was checked against the working tree; findings that changed a
merge decision are noted inline.

## Scores

| dimension | risk (11t/4w) | mvp (7t/3w) | verification (14t/4w) |
|---|---|---|---|
| criteria coverage | 7/7; widest c-2 surface — only draft naming `internal/codex/git.go`, `techdebt.go`, `cleantree.go`; includes ship backends for c-4 | 7/7 on paper, but c-4 excludes `internal/ship` entirely — verified: `[remote].auth_env` is read at 5 sites there vs 4 in forge, so c-4's literal subject goes unguarded | 7/7; only draft with a dedicated policy-population task (t-13) for the derivation-source bug; c-2 edit list narrower than risk's |
| test-contract specificity | strongest single idea in the panel — audit test proven against synthetic must-flag/must-pass snippets so it cannot pass vacuously; sentinel-token-absence; wildcard label-boundary test | good, every contract carries a falsification clause, but fewer anti-gaming assertions; leans on a fake-git PATH shim with no precedent in the tree | strongest overall — adds the three assertions nobody else has: clean-config-produces-no-finding, subtest-set-equals-vector-set, allowlist-entry-staleness; its "no git ran" proof needs no new harness |
| granularity | good; t-4 spans 5 files and mixes c-1 refusal with c-2 separators | weakest — t-5 is 14 files, t-1 is 6; author flags both as deliberate, but a 14-file mechanical sweep is one commit nobody can review | best — mostly 2–5 files, one concern per task |
| wave correctness | correct, and the only draft that spots the real hazard: the c-2 sweep must not run in parallel with the c-1 wiring or the audit reads a half-migrated tree | correct and cheapest at 3 waves, but t-1 and t-5 both edit `phase.go`/`milestone.go` in adjacent waves with the sweep depending on the guard | correct dependencies, but wave 2 puts t-7 and t-8/t-9 in parallel over the same files (`phase.go`, `milestone.go`, `ship_recover.go`) — the exact conflict risk flagged |

**Skeleton: verification.** It wins granularity and contract quality outright, its
package-placement reasoning is the only one that survives checking the real import
graph, and it is the only draft that isolates the derivation-source choice (t-13)
where the silent-allow-everything bug actually lives. Its two defects — a narrow
c-2 edit list and a wave-2 file conflict — are precisely what risk gets right, so
the graft is clean.

## Merged plan

14 tasks across 4 waves.

### Wave 1

```
t-1  Author hostile-config fixture and refusal contract        [verification]
     files:    fixtures/hostile-config-c5/project.toml
               fixtures/hostile-config-c5/local-tracked.toml
               fixtures/hostile-config-c5/RUN.md
               fixtures/hostile-config-c5/expected-refusals.txt
     covers:   c-5
     contract: inert data only, matching the RUN.md + expected-*.txt shape of
               fixtures/terraform-c3 and fixtures/iac-multi-c5 (both verified
               present). project.toml carries git_main_branch =
               "--upload-pack=touch $TMPDIR/dross-pwned" [risk payload — a
               sentinel-writing payload is what makes t-14's no-exec assertion
               possible], branch_pattern with a leading dash [mvp], and
               api_base = "https://attacker.example/api".
               expected-refusals.txt pins one line per vector: the exact error
               prefix each guard must emit, so implementers match strings they
               did not choose. No Go test lands here; t-14 is its gate.

t-2  Add ref-name guard to the guarded switch helpers          [verification+mvp]
     files:    internal/cmd/refguard.go
               internal/cmd/refguard_test.go
               internal/cmd/switchbranch.go
     covers:   c-1
     contract: validateGitRef(kind, name) rejects leading "-", empty, and names
               git check-ref-format rejects (space, "..", control chars, ".lock"
               suffix) [mvp's fuller reject table]. Called at the top of
               checkoutBranch, checkoutBranchNew (branch AND base), guardedFF,
               guardedResetHard — all four verified at switchbranch.go:36/49/66/76
               — before guardLiveState and before any exec.
               TestGuardedHelpersRefuseLeadingDash asserts the named error AND
               that no git process ran, proven by pointing the helper at a temp
               dir that is not a git repo and asserting the returned error does
               NOT contain "not a git repository". TestValidateGitRefAccepts keeps
               "main", "milestone/v1.2", "phase/config-trust-hardening",
               "release-1.0" passing, so the guard cannot be tightened into a
               false refusal.

t-3  Add git separator argument builders                        [verification]
     files:    internal/cmd/gitargs.go
               internal/cmd/gitargs_test.go
     covers:   c-2
     contract: gitRefArgs(sub, opts, refs...) emits "--end-of-options" before
               refs; gitPathArgs emits "--" before pathspecs; gitRefPathArgs
               covers the mixed shapes (checkout <sha> -- <paths>, verified at
               repair_files.go:60 and ship_recover.go:181).
               TestGitRefArgsSeparator asserts gitRefArgs("checkout", nil, "-x")
               yields exactly ["checkout","--end-of-options","-x"] and that the
               separator is emitted even when the ref looks benign — a
               conditional separator is absent exactly when it matters.
               TestGitPathArgsSeparator asserts an option passed in opts stays
               BEFORE the separator, so "--porcelain" is not demoted to a
               pathspec.

t-4  Pin stryker core to an exact version                       [all three]
     files:    internal/mutation/stryker.go
               internal/mutation/stryker_test.go
     covers:   c-3
     contract: const strykerPin replaces the bare "@stryker-mutator/core" at
               stryker.go:94 and in the install hint at stryker.go:61.
               TestStrykerRunArgsPinned asserts the argv element after "--yes"
               matches ^@stryker-mutator/core@\d+\.\d+\.\d+$ — a bare name, an
               "@latest", or a "^9.0.0" range each fail the regex.
               TestStrykerHintUsesSamePin asserts the forced-failure error text
               contains strykerPin, so the pin cannot drift between the
               invocation and the advice. Tightens the existing loose assertion
               at stryker_test.go:207 (verified: currently only
               strings.Contains "@stryker-mutator/core run").

t-5  Add host-allowlist derivation package                      [verification]
     files:    internal/hostallow/hostallow.go
               internal/hostallow/hostallow_test.go
     covers:   c-4
     contract: Policy{RemoteURL, Extra} with Derive(remoteURL, extra) and
               (Policy) Check(kind, rawURL) error. Allowed set = host of
               [remote].url + SaaS defaults (api.github.com, github.com,
               gitlab.com, bitbucket.org, api.bitbucket.org, codeberg.org,
               *.atlassian.net, *.youtrack.cloud) + Extra. No allow-all, no
               user wildcard, no fallback return path.
               TestCheckRefusesOffAllowlistHost: error text contains
               "attacker.example" and "dross local set allow_hosts".
               TestCheckAllowsRemoteHost: self-hosted pair (remote and api_base
               both git.corp.internal) passes with empty Extra, so derivation
               costs honest repos nothing.
               TestSuffixWildcardIsNotSubstring: "*.atlassian.net" allows
               acme.atlassian.net and REFUSES evil-atlassian.net and
               atlassian.net.attacker.example — degrading the match to
               strings.HasSuffix fails it [risk named this the single most
               likely implementation slip].
               TestCheckRefusesSchemeAndPort: http:// on an allowed host and
               api.github.com:8443 are both refused.
               Derive("", nil) returns the SaaS defaults only, never everything
               [risk] — a caller that forgets the field fails closed.

t-6  Gitignore local.toml and refuse a tracked one              [all three]
     files:    internal/cmd/gitignore.go
               internal/cmd/local.go
               internal/cmd/gitignore_test.go
               internal/cmd/local_test.go
     covers:   c-7
     contract: generalize drossGitignoreBlock / ignoresDrossState (verified at
               gitignore.go:29/75) over a path list so the seeded block covers
               .dross/state.json AND .dross/local.toml. init.go:101 and
               onboard.go:119 already call ensureDrossGitignore — verified, so
               neither file is edited [mvp+verification agree; risk's t-5 also
               reached this conclusion].
               Add allow_hosts to the closed localKeys set (verified at
               local.go:56, a map of string get/set accessors — so a
               comma-separated string, not []string [mvp]) plus
               readAllowHosts(root, repoDir) which runs
               `git ls-files --error-unmatch -- .dross/local.toml` (the idiom
               already used at doctor.go:332 and switchbranch.go:94) and returns
               a named error when git reports the file TRACKED, rather than
               parsing it.
               TestEnsureGitignoreCoversLocalToml: a repo with no .gitignore ends
               matching both paths; a second call leaves the file byte-identical.
               TestIgnoresLocalTomlViaBroaderPattern: an existing ".dross/*.toml"
               line suppresses the local.toml append but NOT state.json.
               TestReadAllowHostsRefusesTrackedLocal: with .dross/local.toml
               `git add`ed and containing allow_hosts = "attacker.example",
               readAllowHosts returns an error naming the file as tracked, names
               the `git rm --cached` fix [risk], and returns a nil slice — the
               committed host is never returned to any caller.
```

### Wave 2 (depends wave 1)

```
t-7  Validate config-derived branch names at four commands      [verification+mvp]
     files:    internal/cmd/phase.go
               internal/cmd/phase_checkout.go
               internal/cmd/milestone.go
               internal/cmd/ship_recover.go
     covers:   c-1
     depends:  t-2
     contract: call validateGitRef wherever a ref is produced from config or
               argv, before the first git call — resolveCompleteBase
               (phase.go:645; baseFlag, changes.Base, and phaseRefRecordedBase
               at :672, whose value comes from committed changes.json and is
               therefore attacker-controlled [risk]), completeBaseCandidates
               (:689), phaseCheckout (:22) and Checkout (:66) on args[0],
               resolveMilestoneCutPoint (milestone.go:466) and
               ensureMilestoneBranch (:507) on mainBranch/forced/
               "milestone/"+version, and ship_recover.go:96's
               baseBranch := p.Repo.GitMainBranch (line verified).
               Four separate test functions, not one table over a helper —
               removing the guard from any single entry point must fail exactly
               its own test [risk].
               TestPhaseCompleteRefusesDashMainBranch: HEAD unchanged and no
               branch deleted, so the refusal precedes any mutation.
               TestPhaseCheckoutRefusesDashArg: never reaches the "no local
               branch" message, which would prove git already ran.
               TestMilestoneCreateRefusesDashMainBranch: `git branch --list`
               shows no new ref.
               TestShipRecoverRefusesDashMainBranch asserts `--recover` against
               the hostile project.toml returns the guard error instead of the
               "no branch" message it returns today [mvp — a behaviour-anchored
               falsification, sharper than asserting on git's own stderr].

t-10 Enforce host allowlist in the four forge clients           [all three]
     files:    internal/forge/forge.go
               internal/forge/github.go
               internal/forge/jira.go
               internal/forge/youtrack.go
     covers:   c-4
     depends:  t-5
     contract: forge.Config gains Hosts hostallow.Policy. New (forge.go:73),
               NewGitHubProjects (github.go:42), NewJira (jira.go:47) and
               NewYouTrack (youtrack.go:48) each Check the resolved APIBase
               AFTER the presence checks and BEFORE os.Getenv(cfg.AuthEnv) —
               verified at forge.go:101, github.go:53, jira.go:60,
               youtrack.go:58. github.go's "https://api.github.com" default is
               checked too. A zero Policy means SaaS defaults only, never
               unrestricted [risk].
               One test per constructor: auth env SET to a sentinel, api_base =
               https://attacker.example, asserts the hostallow error, a nil
               client, and that the error text does not contain the sentinel.
               TestRefusedHostNeverReadsToken pins the ordering directly — with
               the host off-allowlist AND the env var unset, the error is the
               host refusal, not "$X is not set"; swapping the two statements
               fails it [risk].
               TestForgeNewAllowsDerivedHost keeps github.com/api.github.com
               constructing, so the guard does not brick the default config.

t-12 Enforce host allowlist in the ship backends                [risk+verification]
     files:    internal/ship/hostguard.go
               internal/ship/open.go
               internal/ship/forgejo.go
               internal/ship/gitlab.go
               internal/ship/bitbucket.go
               internal/ship/comment.go
     covers:   c-4
     depends:  t-5
     contract: this is where [remote].auth_env's token is actually read —
               verified at forgejo.go:26, gitlab.go:31, bitbucket.go:92,
               comment.go:75 and comment.go:102, five sites, all of them
               os.Getenv(opts.AuthEnv). New hostguard.go exports
               resolveToken(apiBase, authEnv string, p hostallow.Policy)
               (string, error) doing the Check then the Getenv [risk's
               centralization — verification edits all five sites inline, which
               leaves five places for the next backend to get it wrong].
               OpenOpts and CommentOpts gain Hosts hostallow.Policy.
               TestShipRefusesOffAllowlistAPIBase, table-driven over
               forgejo/gitlab/bitbucket open + both comment paths: returns the
               hostallow error and an empty token, with the auth env set to a
               sentinel the error must not contain, and an httptest server
               registered as the fake host recording zero requests.
               TestNoGetenvOutsideHostguard greps internal/ship/*.go (non-test)
               for `os.Getenv(` and fails on any hit outside hostguard.go, so a
               backend added later cannot reintroduce an unguarded read [risk].
               TestShipAllowsDerivedSelfHostedHost keeps an honest
               forgejo/gitlab install working.

t-11 Report hostile config as named doctor findings             [all three]
     files:    internal/cmd/doctor.go
               internal/cmd/doctor_test.go
     covers:   c-6
     depends:  t-2, t-5
     contract: two new doctor sections. "Branch names:" runs validateGitRef over
               p.Repo.GitMainBranch and over branch_pattern rendered with the
               current phase id [mvp — the pattern is config-derived too and
               risk names it as well]. "API host:" derives the policy from
               [remote].url + readAllowHosts and Checks [remote].api_base and
               [board].base_url. Each failure counts as an issue, not a warning,
               and the host finding names the exact
               `dross local set allow_hosts <host>` command per the locked
               escape_hatch decision.
               TestDoctorReportsDashBranchName: output names the offending value
               and doctor's exit code is non-zero — a finding printed without
               moving the counter is a finding nobody acts on.
               TestDoctorReportsOffAllowlistAPIBase: names both the refused host
               and the literal hint text; dropping the hint fails it.
               TestDoctorCleanConfigHasNoNewFindings: the dross repo's own
               project.toml (github.com / api.github.com / "main") produces the ✓
               lines and adds zero issues — the check cannot be satisfied by
               always reporting [verification; neither other draft has this].
```

### Wave 3 (depends wave 2)

```
t-8  Route phase and switch git calls through separators        [verification]
     files:    internal/cmd/phase.go
               internal/cmd/switchbranch.go
     covers:   c-2
     depends:  t-3, t-7
     contract: rewrite the config/user-derived positionals at phase.go:674, :709
               (show <ref>), :445 (rev-parse), :557 (branch -D), :569
               (ls-remote), :574 (push origin --delete), :761 (merge-base
               --is-ancestor) and switchbranch.go's checkout / checkout -b /
               merge --ff-only / reset --hard / cat-file -e / ls-files calls to
               build argv via t-3's builders.
               TestPhaseCompleteArgvCarriesSeparator records invocations over the
               exec seam and asserts that for every recorded call containing a
               phase-derived ref, the ref's argv index exceeds the index of
               "--end-of-options" (or "--" for pathspec calls), printing the
               offending argv on failure.
               TestGuardLiveStateRefPathSeparator asserts the cat-file -e probe
               sends "<ref>:.dross/state.json" after the separator.

t-9  Route remaining git calls through separators + audit gate  [verification+risk]
     files:    internal/cmd/milestone.go
               internal/cmd/ship_recover.go
               internal/cmd/milestone_stale.go
               internal/cmd/doctor.go
               internal/cmd/techdebt.go
               internal/cmd/cleantree.go
               internal/cmd/repair_files.go
               internal/codex/git.go
               internal/cmd/gitargs_audit_test.go
     covers:   c-2
     depends:  t-3, t-7, t-11
     contract: same rewrite at milestone.go:513/517/518/525,
               ship_recover.go:164/174/181/199, milestone_stale.go:169 (diff
               <from> <to>) and doctor.go:716 [verification], PLUS techdebt.go,
               cleantree.go, repair_files.go and internal/codex/git.go [risk —
               verification's audit test claims to scan internal/ and cmd/ but
               its edit list stops at internal/cmd, which guarantees the audit
               lands red; verified: 14 direct exec.Command("git") sites and 97
               gitNoOut/gitTrim/gitCombined call sites across internal/].
               repair_files.go:60's `checkout <ref> -- <path>` also routes its
               ref through validateGitRef [risk].
               TestNoUnseparatedGitPositional is the enforcing gate: adding a new
               `gitCombined(repoDir, "branch", "-D", someVar)` anywhere fails it
               by file:line. go/ast precedent exists in the tree
               (enum_divergence_test.go, incompleteroot_test.go) — verified.
               The audit is itself proven against a table of synthetic source
               snippets, half of which it must flag and half it must pass, so an
               audit that trivially returns "clean" fails its own fixture [risk —
               the single strongest anti-vacuous-pass idea in the panel].
               TestRepairFilesRefusesDashRef asserts `checkout -x -- .dross/` is
               refused before exec [risk].

t-13 Populate the policy at both config-build sites             [verification]
     files:    internal/cmd/issue.go
               internal/cmd/ship.go
     covers:   c-4, c-7
     depends:  t-6, t-10, t-12
     contract: boardConfig (issue.go:112, verified) and buildOpenOpts /
               buildCommentOpts (ship.go:27, :42, verified) set Hosts =
               hostallow.Derive(p.Remote.URL, readAllowHosts(root, repoDir)).
               boardConfig's synthetic "https://board.local/<project>" URL is NOT
               the derivation source — the real [remote].url is, per the locked
               allowlist_source decision. A readAllowHosts error (tracked
               local.toml) propagates as the command's refusal, never as an empty
               extras list [risk+verification agree].
               TestBoardConfigDerivesFromRemoteURL asserts the policy contains
               the [remote].url host and NOT "board.local" — deriving from the
               synthetic URL would silently allow whatever it names.
               TestBuildOpenOptsCarriesPolicy asserts an off-allowlist api_base
               aborts `dross ship` before any network call (fake transport sees
               zero requests).
               TestTrackedLocalTomlBlocksBoardConfig asserts that with local.toml
               tracked and naming attacker.example, a board client for api_base
               https://attacker.example still returns the refusal — the tracked
               store cannot self-authorize [mvp's phrasing; closes the c-7 loop
               the escape_hatch decision depends on].
```

### Wave 4 (depends wave 3)

```
t-14 Land the hostile-config regression suite and red proof     [verification+risk]
     files:    internal/cmd/hostile_config_test.go
               internal/forge/hostile_config_test.go
               fixtures/hostile-config-c5/RUN.md
     covers:   c-5
     depends:  t-7, t-8, t-9, t-10, t-11, t-12, t-13
     contract: one suite copying fixtures/hostile-config-c5/project.toml into a
               temp repo's .dross/ and driving every vector: phase complete,
               phase checkout, milestone create, ship recover, board client
               construction, a ship PR open, and doctor. Each subtest asserts the
               refusal matches its line in expected-refusals.txt AND asserts the
               side effect is absent — no sentinel file, no request to the fake
               host, no token in any error string [risk].
               TestHostileConfigVectors is table-driven off expected-refusals.txt.
               TestEveryVectorHasASubtest asserts the subtest-name set equals the
               vector-id set, so deleting a test to green the suite fails the
               suite instead [verification].
               TestHostileConfigNoPwnSentinel asserts $TMPDIR/dross-pwned does not
               exist after the whole run — the one assertion that fires loudly if
               the refusals move but the exec still happens [risk].
               The red proof is an artefact, not an assertion: RUN.md records an
               observed replay of the suite against the phase's base commit in a
               git worktree, naming which subtests fail there. Written only after
               that run is seen — per rules.toml and the execution-safety rule
               that a result must be observed before it is claimed.
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 | t-2, t-7 |
| c-2 | t-3, t-8, t-9 |
| c-3 | t-4 |
| c-4 | t-5, t-10, t-12, t-13 |
| c-5 | t-1, t-14 |
| c-6 | t-11 |
| c-7 | t-6, t-13 |

7/7 covered. No task originates outside the three drafts.

## Disagreements

### D-1 — Does c-4 extend to `internal/ship`, or stop at the four forge clients?

- **risk / verification:** in. Both add a ship-backend task (risk t-7, verification
  t-12) arguing the locked `client_scope` decision is a floor forcing board
  coverage in, not a ceiling excluding ship.
- **mvp:** out. c-4 is one task guarding the four forge constructors; ship is never
  touched.
- **Provisional default: IN** (merged t-12). Checked: `os.Getenv(opts.AuthEnv)`
  appears at `internal/ship/forgejo.go:26`, `gitlab.go:31`, `bitbucket.go:92`,
  `comment.go:75` and `comment.go:102` — five sites. `forge.Config` is built from
  the `[board]` block. So `[remote].auth_env`, which is c-4's literal subject, is
  read *only* in `internal/ship`. mvp's plan would leave the criterion's own
  sentence entirely unguarded.
- **Why it matters:** this is the difference between satisfying c-4 and satisfying
  a paraphrase of it. It is also the largest single scope delta in the panel — six
  files and a task. If the lead reads `client_scope` as exhaustive, t-12 is cleanly
  separable and c-4 becomes board-only, but the phase then ships with the
  [remote].auth_env token reaching any host a committed api_base names.

### D-2 — Where does the allowlist derivation live?

- **risk:** `internal/forge/hostallow.go`. **mvp:** `internal/project/allowhost.go`
  (reuses the existing `parseGitRemote`). **verification:** a new top-level
  `internal/hostallow` package.
- **Provisional default: `internal/hostallow`** (verification). Checked:
  `internal/ship` imports neither `internal/forge` nor `internal/project` today —
  its only internal imports are `configenum`, `phase` and `verify`. Under D-1 both
  `forge` and `ship` need the derivation, so risk's placement forces
  `ship → forge`, coupling two deliberately-separate backends. mvp's placement is
  acyclic (`internal/project` imports nothing internal, verified) and would work,
  but it puts a security boundary inside the config-parsing package.
- **Why it matters:** it is load-bearing only because of D-1. If D-1 resolves to
  mvp's forge-only scope, mvp's `internal/project` placement becomes the cheaper
  choice and this divergence evaporates. Decide D-1 first.

### D-3 — `--` or `--end-of-options` for ref positions?

- **mvp / verification:** `--end-of-options` for refs, `--` for pathspecs. Both
  explicitly flag this as reading c-2 by intent rather than letter, on the grounds
  that `git checkout -- main` reinterprets the branch as a pathspec — a different
  bug, not a fix.
- **risk:** `--` only, and argues the gap is real and unfixable by a separator, so
  it deploys a *second* barrier (pre-exec refusal in t-4) to cover ref positions
  that `--` cannot protect.
- **Provisional default: `--end-of-options` for refs, `--` for pathspecs** (2–1),
  with risk's two-barrier structure retained anyway — the merged plan keeps both
  t-7 (refuse before exec) and t-8/t-9 (separators), because neither is complete
  alone: a refusal is only as good as its call-site coverage, and a separator
  cannot help a ref git never treats as a flag position.
- **Why it matters:** c-2's text says "`--` separator" literally. Two of three
  drafts propose satisfying it with a different token. Checked: `--end-of-options`
  appears nowhere in the tree today (git ≥2.24 required). This needs the lead's
  explicit sign-off — an implementer following c-2 to the letter would write a
  worse guard, and a verifier reading c-2 to the letter could fail a correct one.

### D-4 — How is "no git process started" (c-1) proven?

- **risk / mvp:** a fake `git` on PATH that writes a sentinel file / records
  invocations; the test asserts zero.
- **verification:** point the helper at a temp dir that is not a git repo and
  assert the returned error does not contain git's own "not a repository" text.
- **Provisional default: verification's trick for the unit-level tasks
  (t-2, t-7), risk's sentinel for t-14.** Checked: no fake-git or PATH-shim
  harness exists anywhere in `internal/cmd`'s tests, and there is no exec seam for
  git in `internal/cmd` (no injectable runner). verification's approach needs no
  new machinery. But t-14's fixture payload is `--upload-pack=touch
  $TMPDIR/dross-pwned`, where a sentinel file is the natural and strongest
  assertion, so the harness earns its keep exactly once.
- **Why it matters:** building a PATH shim is unbudgeted work in three tasks that
  don't otherwise need it, and a shim that silently stops being on PATH turns every
  "no git ran" assertion green forever.

### D-5 — Is c-5's fixture one task or two?

- **risk / mvp:** one task — fixture files and the driver test land together
  (risk t-11, mvp t-7).
- **verification:** split — inert fixture data plus `expected-refusals.txt` in
  wave 1, the assertion suite in wave 4.
- **Provisional default: split** (verification). Writing the expected error
  prefixes before the guards exist forces implementers to match strings they did
  not choose, which is the whole value of a contract-first fixture.
- **Why it matters:** the split's cost is a wave-1 task with nothing runnable — it
  cannot be gated by a test at commit time, which sits awkwardly against this
  repo's atomic-commit-per-task-with-a-green-gate convention. verification
  acknowledges this and argues the alternative (committing the assertion suite red
  in wave 1) is worse. It is a live tension, not a settled one.

### D-6 — Audit-test exceptions: an allowlist, or none?

- **verification (t-9):** a file:line allowlist of reviewed exceptions with a
  required reason string, plus a companion `TestAuditAllowlistEntriesStillExist`
  so entries cannot rot into a blanket exemption.
- **risk (t-8):** no allowlist at all; the audit is instead proven against
  synthetic must-flag/must-pass snippets.
- **Provisional default: no allowlist; keep risk's synthetic self-test.**
  rules.toml r-02 is explicit that the standing backlog only ever shrinks and that
  a finding is either fixed, routed, or recorded as accepted — a file:line
  exception list is a backlog by another name. If a call site genuinely cannot take
  a separator, it should be recorded as accepted with its reason, not carried in a
  test fixture.
- **Why it matters:** 97 helper call sites plus 14 direct `exec.Command("git")`
  sites were counted in `internal/`. If the sweep hits a site that cannot be
  separated, the no-allowlist default has no escape valve and t-9 stalls. Ruling
  needed on whether verification's allowlist-plus-staleness-test is an acceptable
  r-02-compliant form, or whether such sites go to `[[deferred]]` instead.

### D-7 — Granularity: 7, 11, or 14 tasks?

- **mvp:** 7 tasks, deliberately oversized — t-5 spans 14 files, t-1 spans 6, with
  the rationale that a mechanical sweep split by file group yields tasks with
  identical contracts plus merge conflicts.
- **verification:** 14 tasks, one concern each. **risk:** 11, in between.
- **Provisional default: 14** (verification's granularity), with risk's wider file
  list grafted into t-9 — so the merged t-9 is the plan's largest task at 9 files.
- **Why it matters:** mvp's own justification is sound for a purely mechanical
  edit, but the c-2 sweep as merged is not purely mechanical — it also routes refs
  through `validateGitRef` at `repair_files.go` and touches a second package
  (`internal/codex`). The lead may still prefer fewer, larger commits; the split
  here is a reviewability judgment, not a correctness one.

### D-8 (structural, resolved by graft rather than choice) — wave placement of the c-2 sweep

verification places its c-2 separator tasks (t-8, t-9) in wave 2 *alongside* t-7,
but t-7 edits `phase.go`, `milestone.go`, `ship_recover.go` and
`phase_checkout.go` — the same files t-8/t-9 rewrite. risk spotted this and
serialized its sweep behind its guard task for exactly this reason ("running them
in parallel means the audit reports on a half-migrated tree"). The merged plan
moves t-8, t-9 and t-13 into wave 3, adding one wave to verification's skeleton
and removing a guaranteed false red. Recorded here because it changes the
skeleton's shape, not because the drafts disagree on principle.
