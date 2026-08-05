# Panel draft — LENS: mvp

Phase config-trust-hardening — 7 tasks across 3 waves

Wave 1

  t-1  Reject leading-dash refs before git runs
       files:    internal/cmd/refguard.go, internal/cmd/refguard_test.go,
                 internal/cmd/phase.go, internal/cmd/phase_checkout.go,
                 internal/cmd/milestone.go, internal/cmd/ship_recover.go
       covers:   c-1
       description:
                 New `guardRefName(kind, value string) error` returning a wrapped
                 `ErrUnsafeRefName` for a value that is empty, starts with `-`, or
                 fails git's ref-format core rules (space, `..`, control chars,
                 `.lock` suffix). Called on every config-derived branch string
                 before the first git call in `phase complete` (resolveCompleteBase /
                 completeBaseCandidates), `phase checkout` + `checkout`
                 (forkPhaseBranch, branch arg), `milestone create/open/prune`
                 (`mainBranch`, `milestone/<version>`), and `ship recover`
                 (`p.Repo.GitMainBranch` at ship_recover.go:96).
       contract: - refguard_test table: `--upload-pack=touch /tmp/pwn`, `-x`, ``,
                   `a..b`, `x.lock` each return ErrUnsafeRefName; `main`,
                   `phase/config-trust-hardening`, `milestone/v1.2` each return nil.
                   Deleting the leading-dash arm flips the first two cases.
                 - phase_test: `dross phase complete` in a repo whose project.toml
                   sets `git_main_branch = "--output=/tmp/pwn"` returns
                   ErrUnsafeRefName naming `[repo].git_main_branch`, and the test's
                   fake-git PATH shim records zero invocations — if the guard is
                   moved below the first `gitNoOut`, the shim records one and the
                   test fails.
                 - milestone_test: `dross milestone create` with a version arg of
                   `-f` errors before any ref is created (`git branch --list`
                   over the fixture repo stays empty).
                 - ship_recover_test: `--recover` against the same hostile
                   project.toml returns ErrUnsafeRefName instead of the
                   "no branch" message it returns today.
       depends_on: []
       status:   pending

  t-2  Derive host allowlist, guard forge clients
       files:    internal/project/allowhost.go, internal/project/allowhost_test.go,
                 internal/forge/forge.go, internal/forge/github.go,
                 internal/forge/jira.go, internal/forge/youtrack.go,
                 internal/cmd/issue.go
       covers:   c-4
       description:
                 `project.AllowedHosts(remoteURL string, extra []string) []string`
                 returns the host of [remote].url (via the existing parseGitRemote)
                 plus built-in SaaS defaults (github.com, api.github.com, gitlab.com,
                 codeberg.org, bitbucket.org, api.bitbucket.org, *.atlassian.net,
                 *.youtrack.cloud) plus `extra`. `forge.Config` gains `RemoteURL`
                 and `ExtraHosts`; `New`, `NewGitHubProjects`, `NewJira` and
                 `NewYouTrack` each call a shared `guardAPIHost(cfg)` as their
                 first statement, before `os.Getenv(cfg.AuthEnv)`, returning
                 `ErrHostNotAllowed` naming the api_base host and the allowed set.
                 `boardConfig` in issue.go populates RemoteURL from the project's
                 real `[remote].url` (not the synthetic board.local URL);
                 ExtraHosts stays empty until t-4.
       contract: - allowhost_test: a self-hosted pair (remote `https://git.example.com/o/r`,
                   api_base `https://git.example.com/api/v1`) is allowed with no
                   extras — dropping the remote-derived host from the set fails it;
                   `github.com` remote + `api.github.com` api_base is allowed —
                   dropping the SaaS defaults fails it; `evil.test` is not in either.
                 - allowhost_test wildcard case: `acme.atlassian.net` matches,
                   `atlassian.net.evil.test` does not — a suffix-only match
                   implementation fails the second.
                 - forge tests (forge_test, github_test, jira_test, youtrack_test),
                   one case each: constructing with api_base `https://evil.test/api`
                   and a set auth env returns ErrHostNotAllowed and a nil client.
                   Each test sets the auth env to a sentinel and asserts the
                   returned error string does not contain it, and that an
                   httptest server registered as the transport received zero
                   requests. Moving the guard after the token read makes the
                   jira case return the "$X is not set"/success path instead.
                 - issue_test: boardConfig built from a project whose [remote].url
                   is `https://git.example.com/o/r` and [board].base_url is
                   `https://git.example.com/api/v1` yields a client; changing
                   base_url alone to `https://evil.test` yields ErrHostNotAllowed
                   — this fails if RemoteURL is left unset (fail-closed) or if
                   the synthetic `https://board.local/...` URL is passed instead.
       depends_on: []
       status:   pending

  t-3  Pin stryker core to an exact version
       files:    internal/mutation/stryker.go, internal/mutation/stryker_test.go
       covers:   c-3
       description:
                 Add a `strykerVersion` const and change `runArgs` to invoke
                 `@stryker-mutator/core@<strykerVersion>`. The "is stryker
                 installed" hint in Run's error names the same pinned spec.
       contract: - stryker_test's existing invocation assertion (stryker_test.go:207)
                   is tightened to require `@stryker-mutator/core@` + the const,
                   and to reject a bare `@stryker-mutator/core run` — reverting
                   runArgs to the unpinned name fails it.
                 - a test asserts strykerVersion parses as an exact semver triple
                   (no `latest`, `^`, `~`, or range), so a later "pin" that is
                   really a range fails.
       depends_on: []
       status:   pending

Wave 2 (depends t-1, t-2)

  t-4  Gitignore local.toml; refuse a tracked store
       files:    internal/cmd/gitignore.go, internal/cmd/gitignore_test.go,
                 internal/cmd/local.go, internal/cmd/local_test.go,
                 internal/cmd/issue.go
       covers:   c-7
       description:
                 Add `.dross/local.toml` to drossGitignoreBlock alongside
                 `.dross/state.json` and extend the already-covered check so an
                 existing broader pattern still short-circuits; init.go and
                 onboard.go already call ensureDrossGitignore, so no edit there.
                 Add an `allow_hosts` key to localStore (comma-separated string,
                 matching the existing string accessor shape) and a
                 `readAllowHosts(root, repoDir)` that runs
                 `git ls-files --error-unmatch -- .dross/local.toml` and returns an
                 error naming the tracked file instead of its contents when git
                 reports it tracked. issue.go passes the result as
                 `Config.ExtraHosts`.
       contract: - gitignore_test: a repo with no .gitignore gets one covering both
                   `.dross/state.json` and `.dross/local.toml`; a .gitignore already
                   containing `.dross/*` is left byte-identical. Dropping the
                   local.toml path from the block fails the first case.
                 - local_test: with local.toml untracked and
                   `allow_hosts = "git.acme.internal"`, readAllowHosts returns that
                   host; after `git add -f .dross/local.toml`, the same call returns
                   an error naming `.dross/local.toml` and returns no hosts —
                   removing the ls-files check makes the second case return the
                   host and fail.
                 - issue_test: with a tracked local.toml granting `evil.test`, a
                   board client for api_base `https://evil.test` still returns
                   ErrHostNotAllowed (the tracked store cannot self-authorize).
       depends_on: [t-2]
       status:   pending

  t-5  Separate derived refs and paths in git args
       files:    internal/cmd/gitargs_audit_test.go, internal/cmd/phase.go,
                 internal/cmd/phase_lifecycle.go, internal/cmd/milestone.go,
                 internal/cmd/milestone_stale.go, internal/cmd/milestone_merged.go,
                 internal/cmd/ship.go, internal/cmd/ship_recover.go,
                 internal/cmd/basebranch.go, internal/cmd/switchbranch.go,
                 internal/cmd/status.go, internal/cmd/topology.go,
                 internal/cmd/repair_state.go, internal/cmd/repair_phasedirs.go
       covers:   c-2
       description:
                 New go/ast audit test walks every `exec.Command("git", ...)`,
                 `gitTrim`, `gitCombined` and `gitNoOut` call in internal/cmd and
                 fails, listing file:line, when a non-literal positional argument
                 appears with no preceding `--` (path position) or
                 `--end-of-options` (ref position) separator. Then insert the
                 separator at every flagged site: path args take `--`
                 (already the shape at switchbranch.go:94, repair_files.go:60),
                 ref args take `--end-of-options` because `--` in ref position
                 means "path" to git.
       contract: - gitargs_audit_test fails with a named file:line list if any
                   `git` call site in internal/cmd regains an unseparated variable
                   positional; reverting the insertion at switchbranch.go:40
                   (`checkout`), basebranch.go:69 (`rev-list origin/x..x`) or
                   milestone_stale.go:114 (`merge-base main branch`) each reproduce
                   a distinct failure line.
                 - switchbranch_test: `checkoutBranch(repoDir, "-b")` fails with
                   git's "unknown revision" rather than creating a branch named
                   after the next argument — verified by asserting
                   `git branch --list` is unchanged.
                 - milestone_stale_test: a milestone branch named
                   `milestone/--all` is compared, not treated as a `rev-list` flag
                   (the returned stale set is empty rather than "everything").
                 - the audit test's own self-check: a fixture snippet containing
                   `gitTrim(dir, "log", branch)` is reported, and one containing
                   `gitTrim(dir, "log", "--end-of-options", branch)` is not.
       depends_on: [t-1]
       status:   pending

Wave 3 (depends t-1, t-2, t-4, t-5)

  t-6  Report hostile branch and api_base in doctor
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
       covers:   c-6
       description:
                 In doctor's Remote block, run t-1's guardRefName over
                 `[repo].git_main_branch` and over the branch_pattern rendered with
                 the current phase id, and check `[remote].api_base` (plus
                 `[board].base_url`) against t-2's allowlist. Each failure prints a
                 `✗` line naming the offending value and increments `issues`; the
                 api_base line names the exact escape-hatch command
                 `dross local set allow_hosts <host>`.
       contract: - doctor_test: against a project.toml with
                   `git_main_branch = "-x"`, doctor's output contains a `✗` line
                   naming `[repo].git_main_branch` and `-x`, and the issue count is
                   one higher than the same run with `main`.
                 - doctor_test: with `api_base = "https://evil.test"` and remote
                   `https://github.com/o/r`, output contains a `✗` line naming
                   `evil.test` and the literal string
                   `dross local set allow_hosts` — dropping the hint text fails it.
                 - doctor_test: the honest self-hosted pair (remote and api_base on
                   the same host) produces neither finding, so the check cannot be
                   satisfied by always reporting.
       depends_on: [t-1, t-2, t-4]
       status:   pending

  t-7  Add hostile-config regression fixture
       files:    fixtures/hostile-config/project.toml, fixtures/hostile-config/RUN.md,
                 internal/cmd/hostile_config_test.go
       covers:   c-5
       description:
                 Fixture `.dross/project.toml` carrying
                 `git_main_branch = "--output=/tmp/dross-pwn"`,
                 `branch_pattern = "-x/<id>"` and
                 `api_base = "https://evil.test"`, with RUN.md stating the four
                 attacks and the expected refusal per command (mirroring the
                 existing fixtures/*/RUN.md + expected-finding.txt shape).
                 hostile_config_test copies it into a temp repo and drives
                 `phase complete`, `phase checkout`, `milestone create`,
                 `ship recover`, the board client constructor, and `doctor`.
       contract: - each subtest asserts a specific named error — ErrUnsafeRefName
                   for the four commands, ErrHostNotAllowed for the client — and
                   asserts side-effect absence: no file at /tmp/dross-pwn (well,
                   under t.TempDir), `git branch --list` unchanged, and an
                   httptest transport that saw zero requests.
                 - the doctor subtest asserts both t-6 finding lines are present in
                   one run.
                 - every subtest is written to fail on pre-phase code: reverting
                   t-1 flips the four command subtests from a named error to a git
                   invocation, reverting t-2 flips the client subtest from
                   ErrHostNotAllowed to a constructed client with a token.
       depends_on: [t-1, t-2, t-5]
       status:   pending

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-5 |
| c-3 | t-3 |
| c-4 | t-2 |
| c-5 | t-7 |
| c-6 | t-6 |
| c-7 | t-4 |

7/7 criteria covered.

## Judgment calls

- **Kept the validator and its four call sites in one task (t-1, 6 files)** rather than
  splitting validator-then-wiring. Rejected: a wave-1 task delivering an uncalled
  function, whose only honest contract is a unit test of dead code. The wiring is
  one line per site in one layer.
- **Kept the whole c-2 sweep in one task (t-5, 14 files)**, deliberately breaking the
  5-file split guideline. Rejected splitting by file group: it is a single mechanical
  edit enforced by a single audit test, so every split produces tasks with the same
  contract plus merge conflicts in phase.go/milestone.go.
- **Used `--end-of-options` for ref positions, `--` for path positions.** c-2 says
  `--`, but in ref position `--` means "everything after this is a path" — `git
  checkout -- main` checks out a file. `--end-of-options` (git ≥2.24) is the
  ref-position form of the same separator and is what the criterion's intent requires.
  Flagging this as the one place my draft reads the criterion by intent, not letter.
- **Guard placed in the four forge constructors, not in each `do()`.** api_base is
  fixed at construction and every endpoint is built from it, so one check per client
  covers every request; the contract pins it to run *before* `os.Getenv(cfg.AuthEnv)`
  so the token is never even resolved. Rejected per-request checking as four times the
  code for the same guarantee.
- **`allow_hosts` stored as a comma-separated string** in localStore. Rejected a
  `[]string` field: localKeys' get/set accessors are `func(*localStore) string`, so a
  list type forces a schema change to the store's whole key mechanism for one key.
- **Allowlist derivation lives in internal/project, the guard in internal/forge.**
  project already owns parseGitRemote and KnownHostProviders and imports nothing from
  internal/; forge importing project introduces no cycle. Rejected computing the host
  set in internal/cmd and passing it down: a caller that forgets the field silently
  disables the guard.
- **boardConfig gets the real `[remote].url`, not the synthetic `board.local` one.**
  Without this the board clients (the most valuable tokens, per client_scope) would
  derive an allowlist from a fake host and fall back to SaaS defaults only.
- **Doctor (t-6) and the fixture (t-7) kept separate** despite both landing in wave 3.
  Merging saves one task but produces a single task spanning doctor.go, three fixture
  files and a driver test for two unrelated criteria — the one merge in this draft
  that costs more review than it saves.
- **No new task for init/onboard** (c-7): both already call `ensureDrossGitignore`
  (init.go:101, onboard.go:119), so seeding the entry is one edit to the shared block.
