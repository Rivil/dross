# validator-truth — verification lens

Designed backward from the test contracts. Each criterion's ideal test was written
first; the task is the smallest change that makes that test satisfiable.

Phase validator-truth — 10 tasks across 3 waves

## Wave 1

```
  t-1  Add configenum package of normalising enum sets
       files:    internal/configenum/configenum.go
                 internal/configenum/configenum_test.go
       covers:   c-2, c-4
       depends:  —
       description:
         New zero-dependency leaf package. Exports a `Set` type
         (Values []string, Normalize/Has/List) plus the four sets:
         BoardProviders {forgejo,gitea,gitlab,youtrack,jira,github},
         MilestoneModes {version,agile,epic}, AuthSchemes
         {private-token,bearer,basic}, ShipProviders
         {github,forgejo,gitea,gitlab,bitbucket}. Adds
         MilestoneModesFor(provider) — the per-provider mode subset
         (jira→["version"], youtrack→all three, forge/github→nil meaning
         "mode is never consulted") and BoardRequiresBaseURL(provider).
       contract:
         - TestSetNormalizeTrimsAndLowercases: MilestoneModes.Has(" Version ")
           and Has("EPIC") are true; Has("versionn") and Has("ver sion") are
           false. If Normalize stops trimming or stops lowercasing, this fails.
         - TestEmptyIsAcceptedAsDefault: MilestoneModes.Has("") is true (empty
           = "version" default in code) while AuthSchemes.Has("") is true and
           ShipProviders.Has("") is false — the three empty-value policies are
           pinned separately, so collapsing them into one rule fails.
         - TestAuthSchemesIncludeBasic: AuthSchemes.Values == ["private-token",
           "bearer","basic"] in that order; dropping "basic" fails.
         - TestShipProvidersIncludeBitbucket: ShipProviders.Has("bitbucket") is
           true; a plan that quietly drops bitbucket instead of implementing it
           fails here.
         - TestMilestoneModesForProvider: MilestoneModesFor("jira") ==
           ["version"]; MilestoneModesFor("youtrack") == all three;
           MilestoneModesFor("gitlab") == nil. If jira's runtime restriction
           (jira.go:231 rejects any non-version mode) is not encoded, fails.
         - TestSetListRendersPipeJoined: BoardProviders.List() ==
           "forgejo | gitea | gitlab | youtrack | jira | github" — the exact
           string doctor/issue/forge error messages interpolate, so a hand-typed
           list drifting from the set is caught.
         - TestBoardRequiresBaseURL: false for "github", true for every other
           member of BoardProviders (locked: github_board_base_url).

  t-2  Add remote.auth_user and basic auth scheme
       files:    internal/project/project.go
                 internal/cmd/project.go
                 internal/cmd/project_test.go
       covers:   c-2
       depends:  —
       description:
         Adds `AuthUser string \`toml:"auth_user,omitempty"\`` to
         project.Remote (mirroring Board.AuthUser), plus the
         `remote.auth_user` get/set dotted arms in readDotted/writeDotted.
         Updates the Remote.Provider / Remote.AuthScheme doc comments to the
         real sets.
       contract:
         - TestRemoteAuthUserRoundTrip: `dross project set remote.auth_user
           me@example.com` then Save→Load→`project get remote.auth_user`
           returns it. A missing `auth_user` toml tag or a dropped writeDotted
           arm fails (today the key is rejected as unknown).
         - TestRemoteAuthSchemeBasicRoundTrips: `project set remote.auth_scheme
           basic` survives Save→Load and `project get` echoes "basic".
         - TestUnknownRemoteDottedKeyStillRejected: `project set
           remote.auth_users x` (typo) still errors — proves the new arm was
           added to the switch, not a catch-all fallback.

  t-3  Add Bitbucket PR-open backend over Basic auth
       files:    internal/ship/bitbucket.go
                 internal/ship/open.go
                 internal/ship/bitbucket_test.go
       covers:   c-6
       depends:  —
       description:
         New bitbucket.go holds the transport: bbBasicAuth(req, user, token)
         setting `Authorization: Basic base64(user:token)`, bbRepoRef(url)
         → workspace/slug, bbRequest(method, endpoint, ...) returning
         (body, status, err), and openBitbucketPR. open.go gains
         `AuthUser string` on OpenOpts and a `case "bitbucket"` arm in OpenPR,
         and its default-branch message lists bitbucket.
       contract:
         - TestOpenBitbucketPRHappyPath (httptest, mirroring
           TestOpenGitLabPRHappyPath): server asserts the request hits
           `/repositories/acme/proj/pullrequests`, that the JSON body is
           {"title","description","source":{"branch":{"name":"phase/x"}},
           "destination":{"branch":{"name":"main"}}}, and that the
           Authorization header decodes to "bot@acme:secret"; the 201 response
           {"id":7,"links":{"html":{"href":"https://bitbucket.org/acme/proj/
           pull-requests/7"}}} must yield OpenResult{Number:7,URL:…}. Flattening
           source/destination to GitHub-style "head"/"base", or reading
           `html_url` instead of links.html.href, fails.
         - TestOpenBitbucketPRDraftPrefix: Draft:true sends title
           "Draft: <title>" (Bitbucket Cloud has no draft flag) — a
           `"draft":true` field or an unprefixed title fails.
         - TestOpenBitbucketPRMissingAuthUser: AuthEnv set + token exported but
           AuthUser empty → error mentions `[remote].auth_user`, and no HTTP
           request is made (server records zero hits).
         - TestOpenBitbucketPRMissingToken: $AUTH_ENV unset → error names the
           env var (parity with TestOpenForgejoPRMissingToken).
         - TestOpenBitbucketPRSurfacesHTTPError: a 401 response yields an error
           carrying "401" and the body snippet, not a nil-PR success.
         - TestBBRepoRef: "https://bitbucket.org/acme/proj.git" →
           ("acme","proj"); a URL with one path segment errors.
         - TestOpenPRRejectsUnknownProvider (extend existing): provider
           "codeberg" still errors and the message now contains "bitbucket".
```

## Wave 2 (depends on wave 1)

```
  t-4  Add Bitbucket comment and merge-status backends
       files:    internal/ship/comment.go
                 internal/ship/merged.go
                 internal/ship/bitbucket_comment_test.go
       covers:   c-6
       depends:  t-3
       description:
         postBitbucketComment and bitbucketPRMerged reuse t-3's bbRequest /
         bbBasicAuth. comment.go gains `AuthUser` on CommentOpts and a
         `case "bitbucket"` arm; merged.go gains a bitbucket arm that answers
         authoritatively (it does NOT return ErrMergeStatusUnsupported).
       contract:
         - TestPostBitbucketCommentHappyPath: POST to
           `/repositories/acme/proj/pullrequests/7/comments` with body
           {"content":{"raw":"<markdown>"}}. A flat {"body":…} payload, or
           hitting an `/issues/7/comments` path (the Forgejo shape), fails.
         - TestPostBitbucketCommentMissingAuthUser: empty AuthUser → error
           names `[remote].auth_user`, zero HTTP requests.
         - TestPRMergedBitbucketTrue: GET
           `/repositories/acme/proj/pullrequests/7` returning {"state":"MERGED"}
           → (true, nil). Returning {"state":"OPEN"} → (false, nil). If the arm
           is wired to ErrMergeStatusUnsupported, both cases fail.
         - TestPRMergedBitbucketNeedsPRNumber: PRNumber 0 → error, no request.
         - TestPRMergedUnsupportedProvider (extend existing): the
           forgejo/gitea/gitlab table still returns ErrMergeStatusUnsupported
           and "bitbucket" is NOT in that table — a regression that lumps
           bitbucket back into the unsupported set fails.

  t-5  Thread auth_user through ship opt builders
       files:    internal/cmd/ship.go
                 internal/cmd/ship_test.go
       covers:   c-6
       depends:  t-2, t-3
       description:
         buildOpenOpts and buildCommentOpts copy p.Remote.AuthUser onto
         OpenOpts.AuthUser / CommentOpts.AuthUser alongside the existing
         AuthScheme/ProjectID wiring.
       contract:
         - TestBuildOpenOptsCarriesAuthUser: a Project with
           Remote{Provider:"bitbucket", AuthUser:"bot@acme",
           AuthScheme:"basic"} produces OpenOpts with both fields populated.
           Dropping AuthUser here is exactly the bug that makes every real
           bitbucket ship 401 while every ship-package test still passes — this
           is the test that catches it.
         - TestBuildCommentOptsCarriesAuthUser: same assertion for the
           /dross-review comment path.

  t-6  Route doctor enum checks through configenum
       files:    internal/cmd/doctor.go
                 internal/cmd/doctor_test.go
       covers:   c-1, c-2, c-4
       depends:  t-1
       description:
         Replaces doctor's three inline literal switches (board provider,
         milestone_mode, remote auth_scheme) with configenum.Set.Has, and its
         hand-typed "(expected a | b)" strings with Set.List(). base_url is
         only required when configenum.BoardRequiresBaseURL(provider).
       contract:
         - TestDoctorAcceptsEveryDispatchableBoardProvider: table-driven over
           configenum.BoardProviders.Values — for each provider, init a repo,
           set board.{provider,base_url,auth_env,project,enabled}, export the
           token, run doctor; every entry must exit 0 and print
           "[board] is well-formed". Today `jira` and `github` print
           "✗ … provider … is invalid" and exit non-zero, so this test fails
           before the change and is the direct proof of c-1. Adding a seventh
           board backend to the set without teaching doctor also fails here.
         - TestDoctorBoardBaseURLOptionalForGitHub: provider=github with
           board.base_url empty → exit 0 and no "base_url" ✗ line;
           provider=youtrack with base_url empty → ✗ base_url and non-zero exit.
           (locked: github_board_base_url)
         - TestDoctorNormalisesMilestoneMode: board.milestone_mode " Version ",
           "EPIC" and "Agile" (youtrack) each exit 0 — matching
           EnsureMilestoneEntity's ToLower(TrimSpace(mode)) at youtrack.go:185
           — while "versionn" still exits non-zero with a ✗ milestone_mode line.
           This is c-4's proof; it fails today because doctor.go:136 switches on
           the raw string.
         - TestDoctorNormalisesAuthScheme: remote.auth_scheme " Bearer " exits
           0; "basic" exits 0 (new third value); "token" exits non-zero and the
           ✗ line contains "private-token | bearer | basic".
         - TestDoctorEnumMessagesComeFromConfigenum: doctor's board-provider ✗
           line contains configenum.BoardProviders.List() verbatim — a hand-typed
           list that drifts from the set fails.

  t-7  Route issue enable and forge dispatch through configenum
       files:    internal/cmd/issue.go
                 internal/forge/forge.go
                 internal/cmd/issue_test.go
                 internal/forge/forge_test.go
       covers:   c-2
       depends:  t-1
       description:
         issue.go's `case "forgejo","gitea",…` recognition list and its two
         hard-coded "forgejo/gitea/gitlab/youtrack/jira/github" note strings
         become configenum.BoardProviders.Has / .List(). forge.New's
         "expected forgejo | gitea | gitlab" message is generated from the set
         restricted to the REST backends.
       contract:
         - TestIssueEnableAcceptsEveryBoardProvider: for each value in
           configenum.BoardProviders.Values, `dross issue enable` prints
           "board sync enabled" with NO "has no board backend" note. Removing a
           provider from the set while leaving issue.go's literal list intact
           fails.
         - TestIssueEnableNoteListsConfigenumSet: with provider="svn", the note
           line contains configenum.BoardProviders.List() verbatim — proving the
           string is derived, not typed.
         - TestIssueEnableNormalisesProvider: provider "YouTrack" (mixed case)
           produces no "has no board backend" note, matching
           forge.NewBoard's ToLower dispatch at forge.go:140.
         - TestNewValidation (extend existing): the "unsupported provider" case
           error message now contains configenum's REST-backend list; the
           bitbucket row still errors (bitbucket is a ship provider, never a
           board provider) — a task that mistakenly adds bitbucket to
           BoardProviders fails here.

  t-8  Correct remote provider lists in init/onboard prompts
       files:    assets/prompts/init.md
                 assets/prompts/onboard.md
                 internal/cmd/prompt_provider_list_test.go
       covers:   c-6
       depends:  t-1
       description:
         Both prompts' "**Provider** — github / forgejo / gitea / bitbucket /
         none" bullets gain gitlab (and keep bitbucket, now implemented), plus
         the gitlab.com→gitlab / api/v4 autodetect hint. New test enforces the
         list against configenum.ShipProviders. (locked: prompt_provider_lists)
       contract:
         - TestPromptProviderListsMatchShipProviders: parses the `**Provider**`
           / `**provider**` bullet in assets/prompts/init.md and
           assets/prompts/onboard.md, extracts the slash-separated tokens minus
           "none", and asserts set-equality with configenum.ShipProviders.Values.
           Fails today (gitlab missing from both). Also fails if a future phase
           implements a ship backend without updating the prompts that write
           the value — the writer/dispatcher gap c-6 names.
         - TestPromptProviderBulletFound: the parser locates exactly one
           provider bullet per prompt, so a renamed heading makes the guard fail
           loudly instead of silently passing on zero tokens.
```

## Wave 3 (depends on wave 2)

```
  t-9  Add doctor cross-field combination warnings
       files:    internal/cmd/doctor.go
                 internal/cmd/doctor_test.go
       covers:   c-5, c-6
       depends:  t-6
       description:
         Adds a `warnings` counter that is reported but never added to
         `issues`, so exit stays 0 (locked: new_check_severity). Three checks:
         (a) milestone_mode not in configenum.MilestoneModesFor(board provider)
         → ⚠; (b) provider=jira with board.auth_user empty → ⚠ (NewJira hard-
         requires it); (c) [remote].provider set but not in
         configenum.ShipProviders → ⚠ "ship cannot open a PR for this provider".
       contract:
         - TestDoctorWarnsJiraEpicCombination: board.provider=jira +
           milestone_mode=epic → output contains "⚠", "jira" and "epic", AND
           doctor returns nil (exit 0). The same config with provider=youtrack
           produces no ⚠ line. This is c-5 exactly: today the pair passes doctor
           and only blows up inside JiraClient.EnsureMilestoneEntity
           (jira.go:231).
         - TestDoctorCombinationWarningKeepsExitZero: a repo whose ONLY finding
           is a combination warning exits 0 — the regression guard for the
           locked severity decision. If the implementation does `issues++`, this
           fails.
         - TestDoctorWarnsJiraMissingAuthUser: provider=jira with
           board.auth_user unset → ⚠ naming auth_user, exit 0; setting
           auth_user removes the line.
         - TestDoctorWarnsUnshippableRemoteProvider: remote.provider="bitbucket"
           → NO warning (t-3/t-4 made it dispatchable); remote.provider=
           "sourcehut" → ⚠ containing configenum.ShipProviders.List(), exit 0.
           A version of this check that hard-fails, or that still flags
           bitbucket, fails.
         - TestDoctorHardFailureStillNonZero: an undispatchable board.provider
           ("svn") remains a ✗ with non-zero exit — proving the warning tier
           did not soften the real failure.

  t-10 Add validator/dispatch divergence guard
       files:    internal/cmd/enum_divergence.go
                 internal/cmd/enum_divergence_test.go
       covers:   c-3
       depends:  t-3, t-4, t-6, t-7
       description:
         Mirrors the interaction_coverage.go/_test.go pattern: a go/ast walker
         (providerCasesIn(file, funcName)) collects the string literals of the
         provider `switch` in a named function, and a test compares those sets
         against configenum. Scans internal/ship/{open,comment,merged}.go and
         internal/forge/forge.go, resolving the repo root via the existing
         repoRootFromTest helper.
       contract:
         - TestShipDispatchMatchesShipProviders: providerCasesIn("open.go",
           "OpenPR"), ("comment.go","PostComment") and ("merged.go","PRMerged")
           must EACH equal configenum.ShipProviders.Values as a set. Adding
           `case "codeberg":` to OpenPR without adding it to ShipProviders fails
           naming "codeberg"; implementing bitbucket in OpenPR but forgetting
           PostComment fails naming "bitbucket" and the function it is missing
           from. This is c-3's headline: validator accept-set vs consumer
           dispatch-set.
         - TestBoardDispatchMatchesBoardProviders: the union of
           providerCasesIn("forge.go","New") and ("forge.go","NewBoard") equals
           configenum.BoardProviders.Values. Deleting the `case "jira"` arm from
           NewBoard while doctor still accepts jira fails.
         - TestProviderCasesInParsesFixture: the extractor is itself tested on
           an in-test source string with two funcs and a nested non-provider
           switch — it returns only the target function's provider-switch
           literals. Without this, a broken walker silently returns an empty set
           and both guards above pass vacuously.
         - TestGuardsSeeNonEmptySets: each scanned function yields ≥3 literals,
           so a refactor to a map-driven dispatch (no `case` literals) fails the
           guard loudly instead of degrading to a no-op.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 doctor exits 0 for every dispatchable board provider | t-1, t-6 |
| c-2 single definition per config enum | t-1, t-2, t-6, t-7 |
| c-3 divergence between accept-set and dispatch-set fails a test | t-1, t-10 |
| c-4 doctor normalises as the consumer does | t-1, t-6 |
| c-5 provider/mode combination reported at doctor time | t-1, t-9 |
| c-6 ship accepts every written [remote].provider; doctor warns otherwise | t-1, t-3, t-4, t-5, t-8, t-9 |

Every criterion is covered by at least one task that fails today.

## Judgment calls

- **New leaf package `internal/configenum`, not `internal/project`.** Rejected
  hanging the sets off project.Project: internal/ship and internal/forge
  deliberately avoid importing internal/project (they duplicate splitOwnerRepo
  to keep coupling flat). A zero-dependency leaf lets doctor, issue, forge and
  ship all resolve through one definition without reversing the cmd→project
  direction.
- **Sets carry Normalize, not just Values.** Rejected exporting bare
  `[]string` slices and leaving each caller to lower/trim: that reproduces c-4's
  bug one call site at a time. Normalisation living inside `Has` is what makes
  the c-4 test a property of the shared type rather than of doctor.
- **`MilestoneModesFor(provider)` returns nil for forge/github providers.**
  Rejected returning `["version"]` for them: those backends never read
  milestone_mode at all (issue.go's default arm calls EnsureMilestone), so
  warning about "gitlab + epic" would be a false positive. nil = "not
  consulted, never warn" is the truthful encoding.
- **Divergence guard scans source with go/ast, not a runtime registry.**
  Rejected making each backend register itself into a map at init(): a registry
  makes the sets agree by construction, which sounds better but silently
  redefines c-3 into a tautology — the test could never fail. Source-scanning
  the actual `case` literals is the only version where "add a backend, forget
  the validator" genuinely breaks the build. The extractor gets its own fixture
  test so the guard cannot pass vacuously.
- **Doctor split across two tasks (t-6 enum routing, t-9 cross-field warnings)
  despite touching one file.** Rejected one big doctor task: t-9 introduces the
  warnings-don't-count-as-issues tier, whose regression test
  (TestDoctorCombinationWarningKeepsExitZero) is meaningless until t-6's checks
  exist to compare against. Sequencing them also avoids two same-wave tasks
  editing doctor.go.
- **Bitbucket split into t-3 (open) and t-4 (comment + merged) rather than one
  ship task.** One task would touch five files. The seam is the shared
  transport: t-3 lands bbRequest/bbBasicAuth with the PR-open contract, t-4
  reuses it. Rejected splitting by verb into three tasks — comment and merged
  are ~10 minutes each alone.
- **Bitbucket merge status is authoritative, not ErrMergeStatusUnsupported.**
  Rejected mirroring the forgejo/gitlab punt: c-6 says a bitbucket repo must
  "report merge status", and `GET /pullrequests/{id}` returning
  `state == "MERGED"` is a single request. The test explicitly asserts bitbucket
  is absent from the unsupported table.
- **`[remote].auth_user` schema lands in its own wave-1 task (t-2), not inside
  the bitbucket task.** It is config-layer work in different packages, and
  keeping it wave-1 lets t-3 and t-2 run in parallel; t-5 is the join point
  where the two halves must actually meet, and its contract is written to fail
  if they don't.
- **Prompt correction (t-8) ships with an enforcing test, not as a doc edit.**
  Rejected a bare markdown change: the prompts are the *writer* side of c-6, so
  a parser + set-equality assertion against ShipProviders is what stops the next
  backend from re-opening the same gap.
- **`dross validate` is untouched.** Locked decision enum_validator_home — no
  task adds an enum check there, and no test in this plan invokes Validate.
