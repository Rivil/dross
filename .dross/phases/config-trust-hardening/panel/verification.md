# Panel draft — lens: verification

Phase config-trust-hardening — 14 tasks across 4 waves

Design order: every criterion's ideal test was written first, then the smallest
task that makes that test satisfiable. Where a criterion's test needs a pure,
table-drivable decision function, that function is its own wave-1 task and the
call-site wiring is wave 2 — so the hard logic is proven in isolation before it
is threaded through five packages.

---

## Wave 1

```
  t-1  Author hostile-config fixture and refusal contract
       files:    fixtures/hostile-config-c5/project.toml
                 fixtures/hostile-config-c5/local-tracked.toml
                 fixtures/hostile-config-c5/RUN.md
                 fixtures/hostile-config-c5/expected-refusals.txt
       covers:   c-5
       desc:     Inert data only, matching the fixtures/ convention already used by
                 terraform-c3 and iac-multi-c5 (RUN.md + expected-*.txt). project.toml
                 carries git_main_branch = "--upload-pack=touch /tmp/pwned" and
                 [remote].api_base = "https://attacker.example/api". expected-refusals.txt
                 pins one line per attack vector: the exact error prefix each guard must
                 emit. RUN.md names the four commands (phase complete, phase checkout,
                 milestone create, ship recover), the board/ship request paths, and how to
                 replay against the pre-phase commit.
       contract: every implementation task's error string is grepped against
                 expected-refusals.txt by t-14; if an implementer invents a different
                 wording, t-14's per-vector assertion fails naming the missing prefix.
                 No Go test lands here — a fixture file has nothing to run yet, and this
                 wave must not commit red.

  t-2  Add ref-name guard to guarded switch helpers
       files:    internal/cmd/refguard.go
                 internal/cmd/refguard_test.go
                 internal/cmd/switchbranch.go
       covers:   c-1
       desc:     New validateGitRef(kind, name) error rejecting a leading "-", an empty
                 name, and names git check-ref-format would reject, with a named error
                 ("refusing to run git with a config-derived <kind> %q: a name beginning
                 with - is parsed as a flag"). Called at the top of checkoutBranch,
                 checkoutBranchNew (both branch and base), guardedFF and guardedResetHard,
                 before guardLiveState and before any exec.
       contract: TestGuardedHelpersRefuseLeadingDash asserts checkoutBranch(dir,
                 "--upload-pack=x") returns the named error AND that no git process ran —
                 proven by pointing the helper at a temp dir that is not a git repo, so a
                 real invocation would produce git's own "not a repository" text instead;
                 the test asserts the returned error does NOT contain "not a git
                 repository". Same table row for guardedFF and guardedResetHard.
                 TestValidateGitRefAccepts keeps "main", "milestone/v1.2",
                 "phase/config-trust-hardening", "release-1.0" passing so the guard cannot
                 be tightened into a false refusal.

  t-3  Add git separator argument builders
       files:    internal/cmd/gitargs.go
                 internal/cmd/gitargs_test.go
       covers:   c-2
       desc:     gitRefArgs(sub string, opts []string, refs ...string) and
                 gitPathArgs(sub string, opts []string, paths ...string) assemble argv with
                 "--end-of-options" before refs and "--" before pathspecs, plus
                 gitRefPathArgs for the mixed shapes (checkout <sha> -- <paths>,
                 ls-files --error-unmatch -- <path>).
       contract: TestGitRefArgsSeparator asserts gitRefArgs("checkout", nil, "-x") yields
                 exactly ["checkout","--end-of-options","-x"] — the ref lands after the
                 separator, not before it, and the separator is emitted even when the ref
                 looks benign (a conditional separator is a separator that is absent
                 exactly when it matters). TestGitPathArgsSeparator asserts pathspecs land
                 after "--" and that an option passed in opts stays BEFORE the separator,
                 so "--porcelain" is not demoted to a pathspec.

  t-4  Pin stryker core to an exact version
       files:    internal/mutation/stryker.go
                 internal/mutation/stryker_test.go
       covers:   c-3
       desc:     Introduce const strykerPin = "@stryker-mutator/core@<exact.x.y>" and use it
                 in runArgs in place of the unpinned "@stryker-mutator/core", and in the
                 invocation-failed hint so the install advice names the same version.
       contract: TestStrykerRunArgsPinned asserts the argv element after "--yes" matches
                 `^@stryker-mutator/core@\d+\.\d+\.\d+$` — a bare unpinned package name
                 fails the regex, and so does a floating "@latest" or "^9.0.0" range.
                 TestStrykerHintUsesSamePin asserts the error text from a forced
                 non-ExitError failure contains strykerPin, so the pin cannot drift between
                 the invocation and the advice.

  t-5  Add host-allowlist derivation package
       files:    internal/hostallow/hostallow.go
                 internal/hostallow/hostallow_test.go
       covers:   c-4
       desc:     New package. Policy{RemoteURL string; Extra []string} with
                 Derive(remoteURL string, extra []string) Policy and
                 (Policy) Check(kind, rawURL string) error. Allowed set = host of
                 [remote].url + built-in SaaS defaults (api.github.com, github.com,
                 gitlab.com, *.atlassian.net, *.youtrack.cloud, bitbucket.org,
                 api.bitbucket.org, codeberg.org) + Extra. Refusal is a named error naming
                 the offending host, the derived set, and the exact
                 `dross local set allow_hosts <host>` command. No allow-all mode, no
                 wildcard user entry, no fallback return path.
       covers the locked allowlist_source and refusal_behaviour decisions verbatim:
                 Check returns an error, never a bool the caller can shrug off.
       contract: TestCheckRefusesOffAllowlistHost asserts Check("api_base",
                 "https://attacker.example/api") returns a non-nil error whose text
                 contains "attacker.example" and "dross local set allow_hosts".
                 TestCheckAllowsRemoteHost asserts a self-hosted case — remote
                 https://git.corp.internal/o/r, api_base https://git.corp.internal/api/v1 —
                 passes with no Extra, so derivation costs honest repos nothing.
                 TestSuffixWildcardIsNotSubstring asserts "*.atlassian.net" allows
                 acme.atlassian.net and REFUSES evil-atlassian.net and
                 atlassian.net.attacker.example — the classic suffix-match bypass.
                 TestCheckRefusesSchemeAndPort asserts http:// on an allowed host and
                 api.github.com:8443 are both refused, so the host check cannot be walked
                 around sideways.

  t-6  Gitignore local.toml and refuse a tracked one
       files:    internal/cmd/gitignore.go
                 internal/cmd/init.go
                 internal/cmd/onboard.go
                 internal/cmd/local.go
                 internal/cmd/gitignore_test.go
       covers:   c-7
       desc:     Generalize ensureDrossGitignore / ignoresDrossState over a path list so the
                 seeded block covers .dross/state.json AND .dross/local.toml; init.go:101
                 and onboard.go:119 keep their single call. In local.go, add the
                 allow_hosts key to localStore/localKeys and a new
                 readAllowHosts(root, repoDir) ([]string, error) that runs
                 `git ls-files --error-unmatch -- .dross/local.toml` first and returns a
                 named error when the file is TRACKED, rather than parsing it.
       contract: TestEnsureGitignoreCoversLocalToml asserts a repo with no .gitignore ends
                 with a file matching BOTH .dross/state.json and .dross/local.toml, and
                 that a second call appends nothing (byte-identical file).
                 TestIgnoresLocalTomlViaBroaderPattern asserts an existing `.dross/*.toml`
                 line suppresses the append for local.toml but NOT for state.json.
                 TestReadAllowHostsRefusesTrackedLocal builds a temp repo, `git add`s
                 .dross/local.toml containing allow_hosts = ["attacker.example"], and
                 asserts readAllowHosts returns an error naming the file as tracked and
                 returns a nil slice — the committed hosts are never returned to a caller.
```

## Wave 2 (depends on wave 1)

```
  t-7  Validate config-derived branch names at four commands
       files:    internal/cmd/phase.go
                 internal/cmd/phase_checkout.go
                 internal/cmd/milestone.go
                 internal/cmd/ship_recover.go
       covers:   c-1
       depends:  t-2
       desc:     Call validateGitRef at every point a ref is produced from config or argv,
                 before the first git call: resolveCompleteBase (baseFlag, changes.Base and
                 phaseRefRecordedBase results), phaseCheckout/Checkout on args[0],
                 resolveMilestoneCutPoint + ensureMilestoneBranch on mainBranch/forced/
                 "milestone/"+version, and ship_recover.go:96's
                 baseBranch := p.Repo.GitMainBranch.
       contract: TestPhaseCompleteRefusesDashMainBranch writes project.toml with
                 git_main_branch = "--upload-pack=x", runs phase complete against a temp
                 repo, and asserts the error is the validateGitRef text AND that the repo's
                 HEAD is unchanged and no branch was deleted — so the refusal happens before
                 any mutation, not after a partial run.
                 TestPhaseCheckoutRefusesDashArg asserts `phase checkout -x` never reaches
                 the "no local branch" message (which would prove git already ran).
                 TestMilestoneCreateRefusesDashMainBranch asserts ensureMilestoneBranch
                 refuses and that `git branch --list` in the temp repo shows no new ref.
                 TestShipRecoverRefusesDashMainBranch asserts the refusal fires before the
                 `git fetch origin`, verified by running with no origin configured: git's
                 own "'origin' does not appear to be a git repository" must NOT appear.

  t-8  Route phase and switch git calls through separators
       files:    internal/cmd/phase.go
                 internal/cmd/switchbranch.go
       covers:   c-2
       depends:  t-3
       desc:     Rewrite the config/user-derived positionals at phase.go:674, :709 (show
                 <ref>), :445 (rev-parse), :557 (branch -D), :569 (ls-remote), :574 (push
                 origin --delete), :761 (merge-base --is-ancestor) and switchbranch.go's
                 checkout / checkout -b / merge --ff-only / reset --hard / cat-file -e /
                 ls-files calls to build argv via gitRefArgs / gitPathArgs / gitRefPathArgs.
       contract: TestPhaseCompleteArgvCarriesSeparator installs a fake git recorder over
                 the exec seam and asserts that for every recorded invocation containing a
                 phase-derived ref, the ref's index in argv is greater than the index of
                 "--end-of-options" (or "--" for the pathspec calls). A call site that keeps
                 the old raw form fails with the offending argv printed.
                 TestGuardLiveStateRefPathSeparator asserts the cat-file -e probe sends
                 "<ref>:.dross/state.json" after the separator, so a ref-with-leading-dash
                 cannot turn the probe into an option.

  t-9  Route milestone, recover and doctor git calls through separators
       files:    internal/cmd/milestone.go
                 internal/cmd/ship_recover.go
                 internal/cmd/milestone_stale.go
                 internal/cmd/doctor.go
                 internal/cmd/gitargs_audit_test.go
       covers:   c-2
       depends:  t-3
       desc:     Same rewrite for milestone.go:513/517/518/525 (rev-parse, branch, push
                 origin <branch>), ship_recover.go:164/174/181/199, milestone_stale.go:169
                 (diff <from> <to>) and doctor.go:716. Plus the audit test: it parses every
                 .go file under internal/ and cmd/ for exec.Command("git", ...) and for
                 gitNoOut/gitTrim/gitCombined calls, and fails on any call whose argv has a
                 non-literal (identifier or expression) positional not preceded by a
                 separator literal, with an allowlist of reviewed exceptions keyed by
                 file:line and a required reason string.
       contract: TestNoUnseparatedGitPositional is the enforcing gate: adding a new
                 `gitCombined(repoDir, "branch", "-D", someVar)` anywhere in the tree fails
                 it by file:line. TestAuditAllowlistEntriesStillExist fails when an
                 allowlist entry points at a file:line that no longer contains a git call,
                 so the exception list cannot rot into a blanket exemption (rules.toml r-02:
                 the backlog only shrinks).

  t-10 Enforce host allowlist in the four forge clients
       files:    internal/forge/forge.go
                 internal/forge/github.go
                 internal/forge/jira.go
                 internal/forge/youtrack.go
       covers:   c-4
       depends:  t-5
       desc:     Add Hosts hostallow.Policy to forge.Config; each of New,
                 NewGitHubProjects, NewJira and NewYouTrack calls
                 cfg.Hosts.Check("api_base", <its resolved APIBase>) AFTER the APIBase/
                 AuthEnv presence checks and BEFORE os.Getenv(cfg.AuthEnv) — so the token is
                 never even read for a refused host. github.go's
                 `apiBase = "https://api.github.com"` default is checked too.
       covers the locked client_scope decision (all four clients) and refusal_behaviour
                 (abort, never degrade).
       contract: TestForgeNewRefusesOffAllowlistAPIBase, and the same for
                 NewGitHubProjects/NewJira/NewYouTrack: with the auth env var SET to a
                 sentinel and api_base = https://attacker.example, each constructor returns
                 the hostallow error and the returned client is nil. The sentinel matters —
                 TestRefusedHostNeverReadsToken sets the env var and asserts the returned
                 error text does not contain the sentinel value, proving c-4's "gets a
                 refusal, not the token".
                 TestForgeNewAllowsDerivedHost keeps the ordinary
                 remote=github.com/api_base=api.github.com path constructing successfully,
                 so the guard does not brick the default configuration.

  t-11 Report hostile config as named doctor findings
       files:    internal/cmd/doctor.go
                 internal/cmd/doctor_test.go
       covers:   c-6
       depends:  t-2, t-5
       desc:     Two new doctor sections. "Branch names:" runs validateGitRef over
                 p.Repo.GitMainBranch and over "milestone/"+state.CurrentMilestone,
                 incrementing issues with the fix command. "API host:" derives the policy
                 from [remote].url + readAllowHosts and Checks [remote].api_base and
                 [board].base_url, incrementing issues and naming
                 `dross local set allow_hosts <host>` per the locked escape_hatch decision.
       contract: TestDoctorReportsDashBranchName asserts a repo with
                 git_main_branch = "-x" produces output containing "Branch names:" and the
                 offending value, and that doctor's exit code is non-zero — a finding
                 printed without moving the counter is a finding nobody acts on.
                 TestDoctorReportsOffAllowlistAPIBase asserts the api_base finding names
                 both the refused host and the exact `dross local set allow_hosts` command.
                 TestDoctorCleanConfigHasNoNewFindings asserts the dross repo's own
                 project.toml (remote github.com, api_base api.github.com, main branch
                 "main") produces the ✓ lines for both sections and adds zero issues.

  t-12 Enforce host allowlist in the ship backends
       files:    internal/ship/open.go
                 internal/ship/forgejo.go
                 internal/ship/gitlab.go
                 internal/ship/bitbucket.go
                 internal/ship/comment.go
       covers:   c-4
       depends:  t-5
       desc:     Add Hosts hostallow.Policy to ship.OpenOpts and ship.CommentOpts; call
                 Check("api_base", opts.APIBase) inside forgejo.go:20's credential
                 resolver, gitlab.go:25's, bbCredentials (bitbucket.go:82) and comment.go's
                 two inline APIBase+Getenv reads — in each case before the os.Getenv line.
       contract: TestShipForgejoRefusesOffAllowlistAPIBase / gitlab / bitbucket / the two
                 comment paths: each asserts the credential resolver returns the hostallow
                 error and an empty token string, with the auth env var set to a sentinel
                 the error text must not contain. This is the criterion's literal sentence —
                 "the secret named by [remote].auth_env" is the one ship reads.
                 TestShipAllowsDerivedSelfHostedHost keeps remote and api_base sharing a
                 self-hosted host working, so no honest forgejo/gitlab install regresses.
```

## Wave 3 (depends on wave 2)

```
  t-13 Populate the policy at both config-build sites
       files:    internal/cmd/issue.go
                 internal/cmd/ship.go
       covers:   c-4, c-7
       depends:  t-6, t-10, t-12
       desc:     boardConfig (issue.go:112) and buildOpenOpts/buildCommentOpts (ship.go:27,
                 :42) take the project + repoDir and set Hosts =
                 hostallow.Derive(p.Remote.URL, readAllowHosts(root, repoDir)). boardConfig's
                 synthetic URL ("https://board.local/"+project) is NOT the derivation source —
                 the real [remote].url is, per the locked allowlist_source decision. A
                 readAllowHosts error (tracked local.toml) propagates as a refusal, not an
                 empty extras list.
       contract: TestBoardConfigDerivesFromRemoteURL asserts the resulting policy's allowed
                 set contains the host of [remote].url and NOT "board.local" — a regression
                 here would silently allow any host the synthetic URL happened to name.
                 TestBuildOpenOptsCarriesPolicy asserts an api_base on an unapproved host
                 makes `dross ship` abort with the hostallow error before any network call
                 (fake transport asserts zero requests observed).
                 TestTrackedLocalTomlBlocksBoardConfig asserts that with local.toml tracked
                 and naming attacker.example, boardConfig returns the tracked-file error and
                 attacker.example is absent from the policy — closing the c-7 loop that the
                 escape_hatch decision depends on.
```

## Wave 4 (depends on wave 3)

```
  t-14 Land the hostile-config regression suite and red proof
       files:    internal/cmd/hostile_config_test.go
                 fixtures/hostile-config-c5/RUN.md
       covers:   c-5
       depends:  t-7, t-8, t-9, t-10, t-11, t-12, t-13
       desc:     One Go test file that copies fixtures/hostile-config-c5/project.toml into
                 a temp repo's .dross/ and drives every vector end to end: phase complete,
                 phase checkout, milestone create, ship recover, a board request and a ship
                 request. Each subtest asserts the refusal matches its line in
                 expected-refusals.txt. RUN.md is then updated with the observed output of
                 replaying the same suite against the phase's base commit in a git worktree,
                 recording which subtests fail there.
       contract: TestHostileConfigVectors is table-driven off expected-refusals.txt;
                 TestEveryVectorHasASubtest asserts the set of subtest names equals the set
                 of vector ids in expected-refusals.txt, so deleting a vector's test to make
                 the suite green fails the suite instead.
                 TestNoAttackerHostReachedAsserts the fake HTTP transport recorded zero
                 requests to attacker.example across the whole run — a refusal that still
                 made the request is not a refusal.
                 The red proof is an artefact, not an assertion: RUN.md must record a
                 pre-phase run where each vector's subtest fails, per rules.toml — the
                 recorded output is only written after that run is observed.
```

---

## Coverage

| criterion | tasks |
|---|---|
| c-1 (leading-dash branch refused before git, in four commands) | t-2, t-7 |
| c-2 (`--` / end-of-options separator on every derived positional) | t-3, t-8, t-9 |
| c-3 (pinned `@stryker-mutator/core` version) | t-4 |
| c-4 (token only to allowlisted hosts) | t-5, t-10, t-12, t-13 |
| c-5 (hostile-config regression fixture, red pre-phase) | t-1, t-14 |
| c-6 (doctor reports bad branch name + off-allowlist api_base) | t-11 |
| c-7 (local.toml gitignored + tracked-local refused) | t-6, t-13 |

7/7 criteria covered.

## Judgment calls

- **Fixture as inert data in wave 1, assertions in wave 4** — chose splitting c-5
  across two tasks over a single wave-4 task. Rejected committing the assertion suite
  red in wave 1 (the repo has no build-tag idiom to hide it behind, and rules.toml
  forbids committing behind an unobserved gate). Writing `expected-refusals.txt` first
  still gets the contract-first benefit: implementers match strings they did not choose.
- **`--end-of-options` for refs, `--` for pathspecs** — c-2 says "after a `--`
  separator". Taken as intent, not literal argv: `git checkout -- <branch>` reinterprets
  the branch as a pathspec, which is a different bug, not a fix. `--end-of-options` is
  git's actual answer for "this positional is not a flag" and satisfies c-2's stated
  guarantee. Rejected: wrapping every ref in `refs/heads/` (doesn't help `push origin
  --delete`, `ls-remote`, or `diff`).
- **Guard in ship backends too (t-12), not only the four forge clients** — the locked
  client_scope decision names the four forge clients to force board coverage in; it does
  not exclude ship. c-4's literal sentence names `[remote].auth_env` and `api_base`,
  which flow into `ship.OpenOpts`, not `forge.Config`. A plan honoring only the forge
  half would fail a test written from c-4 verbatim. Rejected: reading client_scope as
  exhaustive.
- **`hostallow` as a new package, not a helper in `internal/forge`** — both
  `internal/forge` and `internal/ship` need it, and `internal/ship` importing
  `internal/forge` for a string check would couple two backends that are deliberately
  separate today. Rejected: duplicating the derivation in both.
- **Check before `os.Getenv`, not before the request** — placing the guard at the
  constructor/credential-resolver instead of inside `do()` means the token is never read
  for a refused host, which makes "gets a refusal, not the token" testable by sentinel
  rather than by inspecting headers. Rejected: guarding in `do()` (four more call sites,
  weaker assertion).
- **An audit test with a file:line allowlist (t-9), not a one-off sweep** — c-2 is a
  property of the whole tree, and a sweep is true only on the day it lands. The allowlist
  has a companion test that fails when an entry goes stale, so it cannot become a
  blanket exemption. Rejected: a plain sweep with no enforcing gate.
- **t-13 split out of t-10/t-12** — populating `Hosts` at the two config-build sites is
  where the derivation source is chosen, and getting it wrong (deriving from boardConfig's
  synthetic `board.local` URL) silently allows everything. It deserves its own test and
  its own task rather than riding along in an enforcement task.
- **`allow_hosts` added to the existing closed `localKeys` set** — rather than a new
  file. The store already refuses unknown keys, which is exactly the property the
  escape_hatch decision needs. Rejected: a separate `.dross/allowed-hosts` file.
