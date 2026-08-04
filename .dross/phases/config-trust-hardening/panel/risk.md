# Plan draft — lens: RISK

Phase config-trust-hardening — 11 tasks across 4 waves

Framing: `.dross/` is attacker-controlled input the moment a repo is cloned. Every
task below owns exactly one failure mode end to end — the value that can go
hostile, the boundary that must refuse it, and the test that proves the refusal.
Two independent barriers guard the git surface (refuse-before-exec in t-4,
argv-separation in t-8) because either one alone has a known gap: a refusal has
call sites it can miss, and `--` cannot protect a ref position.

## Wave 1

```
t-1  Add gitsafe ref/path validator
     files:    internal/gitsafe/gitsafe.go
               internal/gitsafe/gitsafe_test.go
     covers:   c-1
     desc:     New package exporting ErrUnsafeRef plus Ref(name) / Path(p) /
               RefRange(a,b). Rejects a leading `-`, an empty string, embedded
               NUL/newline, and a value git itself would reject (`git
               check-ref-format` rules applied in-process, no subprocess).
     contract: Ref("--upload-pack=touch /tmp/pwn") returns an error wrapping
               ErrUnsafeRef and the error text names the offending value;
               Ref("phase/config-trust-hardening") and Ref("origin/main")
               return nil. Path("--output=/etc/x") errors, Path(".dross/") is
               nil. Deleting the leading-dash check makes
               TestRefRejectsLeadingDash fail; deleting the empty-string check
               makes TestRefRejectsEmpty fail (an empty git_main_branch must not
               silently become a valid argv slot).
     depends:  —

t-2  Derive host allowlist from remote URL
     files:    internal/forge/hostallow.go
               internal/forge/hostallow_test.go
     covers:   c-4
     desc:     Exports DefaultSaaSHosts, DeriveAllowedHosts(remoteURL string,
               extra []string) []string and CheckHost(rawURL string, allowed
               []string) error returning ErrHostNotAllowed. Built-ins:
               api.github.com, github.com, gitlab.com, bitbucket.org,
               api.bitbucket.org, *.atlassian.net, *.youtrack.cloud. Wildcard
               match is suffix-on-label only. Non-https scheme, an unparseable
               URL, and an empty host all fail closed.
     contract: CheckHost("https://evil.example.com/api", DeriveAllowedHosts(
               "https://github.com/Rivil/dross", nil)) returns ErrHostNotAllowed
               and the message names both the rejected host and the derived set;
               the same call with "https://api.github.com" returns nil. A
               self-hosted pair ("https://git.corp.internal/o/r" remote +
               "https://git.corp.internal/api/v1" base) passes with no extra
               config. "https://evil.atlassian.net.attacker.com" is REJECTED —
               if the wildcard match degrades to strings.HasSuffix,
               TestWildcardIsLabelBounded fails. DeriveAllowedHosts("", nil)
               returns only the SaaS defaults, never everything.
     depends:  —

t-3  Pin the Stryker core version
     files:    internal/mutation/stryker.go
               internal/mutation/stryker_test.go
     covers:   c-3
     desc:     runArgs invokes `npx --yes @stryker-mutator/core@<pinned> run`
               via an exported StrykerVersion const rather than the floating
               package name. Version documented next to the const with the
               reason for the pin.
     contract: TestRunArgsPinsVersion asserts the argv element equals
               "@stryker-mutator/core@"+StrykerVersion and that no argv element
               is the bare "@stryker-mutator/core" — dropping the suffix fails
               it. TestStrykerVersionIsExact asserts StrykerVersion matches
               ^\d+\.\d+\.\d+$, so a range ("^9.0.0", "latest") fails.
     depends:  —
```

## Wave 2 (depends t-1, t-2)

```
t-4  Refuse hostile refs before git starts
     files:    internal/cmd/switchbranch.go
               internal/cmd/phase.go
               internal/cmd/phase_checkout.go
               internal/cmd/milestone.go
               internal/cmd/ship_recover.go
     covers:   c-1, c-2
     desc:     Route every config-derived branch/ref through gitsafe.Ref before
               it reaches exec: resolveCompleteBase + phaseRefRecordedBase +
               originRecordedPR (changes.json Base is committed, so
               attacker-controlled), checkoutBranch/checkoutBranchNew/guardedFF/
               guardedResetHard, phaseCheckout + Checkout, ensureMilestoneBranch
               (baseBranch = [repo].git_main_branch), shipRecover's baseBranch.
               gitCombined/gitTrim/gitNoOut gain a `--` before positional path
               arguments where one exists.
     contract: With [repo].git_main_branch = "--upload-pack=touch /tmp/pwn",
               TestMilestoneCreateRefusesDashBase asserts the command returns an
               error wrapping gitsafe.ErrUnsafeRef and that no git process ran
               (fake git on PATH writes a sentinel file; the test asserts the
               sentinel is absent). With changes.json Base = "-x",
               TestPhaseCompleteRefusesDashBase asserts the same and that HEAD
               is still on phase/<id>. TestPhaseCheckoutRefusesDashArg covers
               `dross checkout -- -x`. TestShipRecoverRefusesDashBase covers the
               recover entry point. Removing the guard from any one of the four
               entry points fails exactly its own test — the four are separate
               test functions, not a table over one helper.
     depends:  t-1

t-5  Make local.toml an untrusted-until-untracked store
     files:    internal/cmd/gitignore.go
               internal/cmd/local.go
               internal/cmd/allowhosts.go
     covers:   c-7
     desc:     Generalise the gitignore seeder from one hardcoded path to a
               list, adding .dross/local.toml (init and onboard already call
               ensureDrossGitignore, so no change there). Add an
               `allowed_hosts` key (comma-separated) to localStore. New
               allowhosts.go: projectAllowedHosts(root, repoDir, p) runs
               `git ls-files --error-unmatch -- .dross/local.toml` first and
               returns a named refusal when the file is tracked, otherwise
               composes forge.DeriveAllowedHosts(p.Remote.URL, localExtras).
     contract: TestGitignoreSeedsLocalToml asserts a fresh .gitignore contains
               both .dross/state.json and .dross/local.toml, and that a repo
               whose .gitignore already has a broader `.dross/*` gains no
               redundant line. TestAllowHostsRefusesTrackedLocalToml: with
               local.toml committed to the index, projectAllowedHosts returns an
               error naming the file and the `git rm --cached` fix, and does NOT
               return its allowed_hosts value — asserted by putting
               "evil.example.com" in the tracked file and checking it is absent
               from every returned slice. TestAllowHostsReadsUntrackedExtras
               asserts the same value IS honoured when the file is untracked.
     depends:  t-2

t-6  Gate the four forge clients on host allowlist
     files:    internal/forge/forge.go
               internal/forge/github.go
               internal/forge/jira.go
               internal/forge/youtrack.go
     covers:   c-4
     desc:     Config gains AllowedHosts []string. New/NewGitHubProjects/NewJira
               /NewYouTrack call CheckHost(cfg.APIBase, cfg.AllowedHosts) BEFORE
               os.Getenv(cfg.AuthEnv), so a refused host never materialises the
               token in process memory. A nil AllowedHosts means the SaaS
               defaults only — a caller that forgets to populate it fails
               closed, never open.
     contract: For each of the four constructors a test sets the auth env var to
               a sentinel, passes APIBase = "https://evil.example.com" with
               AllowedHosts = ["api.github.com"], and asserts (a) the returned
               error wraps ErrHostNotAllowed, (b) the error string does not
               contain the sentinel token. TestNewChecksHostBeforeToken asserts
               the ordering directly: with the host off-allowlist AND the env
               var unset, the error is the host refusal, not "$X is not set" —
               reversing the two statements fails it. A nil-AllowedHosts client
               against api.github.com still constructs.
     depends:  t-2

t-7  Gate ship's REST backends on host allowlist
     files:    internal/ship/hostguard.go
               internal/ship/open.go
               internal/ship/forgejo.go
               internal/ship/gitlab.go
               internal/ship/bitbucket.go
               internal/ship/comment.go
     covers:   c-4
     desc:     This is where [remote].auth_env's token is actually attached
               (open.go dispatches github to `gh`, everything else to these
               files). New hostguard.go exports resolveToken(apiBase, authEnv
               string, allowed []string) (string, error) doing the CheckHost
               then the Getenv; OpenOpts gains AllowedHosts. The five existing
               `os.Getenv(opts.AuthEnv)` sites (forgejo.go:26, gitlab.go:31,
               bitbucket.go:92, comment.go:75, comment.go:102) become calls to
               it.
     contract: TestShipRefusesOffAllowlistAPIBase, table-driven over
               forgejo/gitlab/bitbucket open + comment: APIBase =
               "https://evil.example.com/api/v1" with AllowedHosts =
               ["git.corp.internal"] returns ErrHostNotAllowed and the httptest
               server registered as the fake host records zero requests.
               TestNoGetenvOutsideHostguard greps internal/ship/*.go (non-test)
               for `os.Getenv(` and fails on any hit outside hostguard.go — so a
               new backend added later cannot reintroduce an unguarded read.
     depends:  t-2
```

## Wave 3

```
t-8  Sweep remaining git argv sites for `--`
     files:    internal/cmd/milestone_stale.go
               internal/cmd/techdebt.go
               internal/cmd/cleantree.go
               internal/cmd/repair_files.go
               internal/codex/git.go
               internal/cmd/git_argv_audit_test.go
     covers:   c-2
     desc:     Add `--` before positional paths at the remaining direct
               exec.Command("git", …) and helper call sites that pass an
               interpolated value, and route the ref operands
               (milestone_stale's merge-base/rev-list branch args,
               repair_files' `checkout <ref> -- <path>`) through gitsafe.Ref.
               The audit test is the durable half: it parses every Go source
               file under internal/ for git invocations and fails when an
               interpolated (non-literal) argument sits in a positional slot
               with no preceding `--` and no gitsafe call in the same function.
     contract: TestGitArgvAudit fails when a new
               `exec.Command("git","-C",dir,"log",userVar)` is added anywhere
               under internal/ — the test is proven by a table of synthetic
               source snippets, half of which it must flag and half it must
               pass, so an audit that trivially returns "clean" fails its own
               fixture. TestRepairFilesRefusesDashRef asserts
               `checkout -x -- .dross/` is refused before exec.
     depends:  t-4

t-9  Wire derived allowlist into board and ship
     files:    internal/cmd/issue.go
               internal/cmd/ship.go
     covers:   c-4
     desc:     boardConfig gains the AllowedHosts from projectAllowedHosts (it
               currently synthesises URL = "https://board.local/<project>", so
               the derivation must come from [remote].url, not from cfg.URL).
               buildOpenOpts does the same for ship's OpenOpts. A refusal from
               projectAllowedHosts (tracked local.toml) propagates out as the
               command's error — it never degrades to an empty allowlist.
     contract: TestBoardConfigCarriesDerivedHosts asserts the AllowedHosts on
               the produced forge.Config contains the [remote].url host and NOT
               "board.local" — if the derivation is taken from cfg.URL the
               assertion on board.local fires. TestShipOptsCarriesDerivedHosts
               is its ship-side twin. TestBoardRefusesWhenLocalTomlTracked
               asserts `dross issue …` returns the tracked-file error rather
               than proceeding with SaaS defaults.
     depends:  t-5, t-6, t-7

t-10 Report hostile config as doctor findings
     files:    internal/cmd/doctor.go
               internal/cmd/doctor_test.go
     covers:   c-6, c-2
     desc:     Two new named checks in the Remote block: [repo].git_main_branch
               / [repo].branch_pattern that gitsafe.Ref rejects, and an
               [remote].api_base (and [board].base_url) host outside the derived
               allowlist. Each counts as an issue, not a warning, and the
               allowlist finding names the exact
               `dross local set allowed_hosts <host>` command. Also fixes
               doctor's own `rev-list origin/<main>..<main>` to validate
               mainBranch first.
     contract: TestDoctorFlagsUnsafeBranchName asserts a project.toml with
               git_main_branch = "-x" produces a line containing
               "git_main_branch" and increments the issue count (exit non-zero).
               TestDoctorFlagsOffAllowlistAPIBase asserts api_base =
               "https://evil.example.com" produces a finding naming both the
               host and the `dross local set allowed_hosts` remedy.
               TestDoctorAllowlistFindingIsIssueNotWarning asserts the exit code
               is non-zero — downgrading it to a warning fails.
     depends:  t-1, t-5
```

## Wave 4

```
t-11 Add hostile-config regression fixture
     files:    fixtures/hostile-config/project.toml
               fixtures/hostile-config/local.toml
               fixtures/hostile-config/RUN.md
               internal/cmd/hostile_config_test.go
               internal/forge/hostile_config_test.go
     covers:   c-5
     desc:     One committed fixture carrying git_main_branch =
               "--upload-pack=touch $TMPDIR/dross-pwned", branch_pattern with a
               leading dash, api_base = "https://evil.example.com", and a
               local.toml that grants evil.example.com (to prove the tracked
               path is refused). Go tests copy it into a t.TempDir git repo and
               drive each attack: milestone create, phase complete, phase
               checkout, ship recover, board client construction, ship PR open,
               doctor. Each asserts the specific refusal AND the absence of the
               side effect (no sentinel file, no request to the fake host, no
               token in any error string).
     contract: Every test in the file fails on pre-phase HEAD — proven by a
               documented `git stash && go test ./internal/cmd -run
               TestHostileConfig` step in RUN.md and by each assertion being on
               the refusal, never on a tolerated pass-through.
               TestHostileConfigNoPwnSentinel asserts $TMPDIR/dross-pwned does
               not exist after the whole suite, which is the one assertion that
               fails loudly if the refusals move but the exec still happens.
     depends:  t-4, t-5, t-6, t-7, t-9, t-10
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (leading-dash branch refused pre-exec, 4 commands) | t-1, t-4 |
| c-2 (`--` separator on positional path/ref) | t-4, t-8, t-10 |
| c-3 (pinned Stryker version) | t-3 |
| c-4 (token only to allowlisted hosts) | t-2, t-6, t-7, t-9 |
| c-5 (hostile-config regression fixture) | t-11 |
| c-6 (doctor reports both) | t-10 |
| c-7 (local.toml gitignored + tracked-refusal) | t-5 |

7/7 criteria covered.

## Judgment calls

- **Included internal/ship's four REST backends (t-7) alongside the four forge
  files the `client_scope` decision names.** [remote].auth_env's token is read
  only in internal/ship — forge.Config is constructed exactly once, by
  issue.go's boardConfig, from the [board] block. Guarding only the four named
  files would leave c-4's literal subject ("the secret named by
  [remote].auth_env") entirely unguarded. The decision's own rationale is about
  *including* board tokens, not excluding ship's; I read it as a floor, not a
  ceiling. If the judge disagrees, t-7 is separable and c-4 becomes board-only.
- **Two barriers on the git surface rather than one.** t-4 refuses before exec
  (satisfies c-1's "before any git process starts"), t-8 adds `--` (c-2). A
  single mechanism would be cheaper, but `--` cannot protect a ref operand
  (`git branch -D -- -x` is the only form that works, and `git merge --ff-only
  -x` has no path slot at all), while a pre-exec refusal is only as complete as
  its call-site coverage. Neither alone is a guarantee.
- **The c-2 sweep is enforced by a source-scanning audit test, not by call-site
  review.** ~30 helper call sites interpolate values today; a hand-audited sweep
  regresses on the next feature. The audit test is itself tested against
  synthetic must-flag/must-pass snippets so it cannot pass vacuously. Rejected:
  a `git()` wrapper with typed Ref/Path argument constructors — a cleaner design
  that would touch every call site in the repo and swamp the phase.
- **CheckHost runs before os.Getenv, not after (t-6, t-7).** Rejected the
  simpler "resolve token, then check host": a refused host should never put the
  secret in process memory at all, and the ordering is directly assertable
  (host-refusal error wins over "env not set"), which makes it a real test
  rather than a comment.
- **Nil AllowedHosts = SaaS defaults only, not "allow everything".** A caller
  that forgets to populate the field breaks a self-hosted forge loudly instead
  of silently disabling the guard. Rejected the ergonomic alternative (nil =
  unrestricted) precisely because forgetting is the expected failure mode.
- **Wildcard entries match on label boundaries, and there is a test for the
  strings.HasSuffix degradation.** `*.atlassian.net` matched by suffix accepts
  `evil.atlassian.net.attacker.com`. This is the single most likely
  implementation slip in t-2, so it gets a named test rather than trusting the
  first implementation.
- **t-5 makes projectAllowedHosts *refuse* on a tracked local.toml rather than
  ignoring the file's contents.** Ignoring would be safe for the token but
  silently changes which hosts work, which is the invisible-downgrade the
  `refusal_behaviour` decision forbids for the host check itself. Same principle,
  applied one level up.
- **t-8 runs in wave 3 despite having no logical dependency on t-4.** Its audit
  test scans the same files t-4 edits, so running them in parallel means the
  audit reports on a half-migrated tree. Serialising costs one wave and removes
  a guaranteed false red.
- **Fixture is Go-test-consumed, not a manual RUN.md fixture.** The three
  existing fixtures/ entries are scanner inputs driven by hand. c-5 demands
  "every test in it fails against the pre-phase code", which requires executable
  tests; RUN.md is kept only to document the pre-phase verification procedure.
- **Not attempted: the deferred inj-1 consent gate, the non-git subprocess
  sweep, and token-redaction in error output.** All three are in [[deferred]]
  with target exec-trust-followups. t-6/t-7's contracts do assert the token is
  absent from the *refusal* error, which is the narrow slice c-4 needs; the
  general redaction guarantee stays deferred.
