# MVP lens — validator-truth

Phase validator-truth — 4 tasks across 3 waves

All tasks `status: pending`.

Wave 1
  t-1  Add single-source config enums package
       files:    internal/project/enums.go, internal/project/enums_test.go,
                 internal/project/project.go, internal/cmd/project.go
       covers:   c-2
       depends:  —
       desc:     New internal/project/enums.go holds the one definition of each config
                 enum: BoardProviders (forgejo|gitea|gitlab|youtrack|jira|github),
                 MilestoneModes (version|agile|epic), RemoteAuthSchemes
                 (private-token|bearer|basic), ShipProviders (github|forgejo|gitea|
                 gitlab|bitbucket), plus Normalize(s) = lower+trim, a Has(set, value)
                 that normalizes first, and MilestoneModesFor(provider) returning the
                 modes a board backend can actually dispatch (jira -> version only;
                 youtrack -> version|agile|epic; forge/github -> version). Also adds
                 Remote.AuthUser to the schema and remote.auth_user to the
                 readDotted/writeDotted tables in internal/cmd/project.go.
       contract: - dropping "basic" from RemoteAuthSchemes fails
                   TestRemoteAuthSchemesIncludeBasic (internal/project/enums_test.go)
                 - Has(MilestoneModes, " Version ") returning false fails
                   TestHasNormalizesCaseAndSpace
                 - MilestoneModesFor("jira") containing "epic" fails
                   TestMilestoneModesForJiraIsVersionOnly
                 - `dross project set remote.auth_user me@x.io` not round-tripping
                   through `get` fails TestProjectSetGetRemoteAuthUser
                   (internal/cmd/project_test.go)

Wave 2 (depends t-1)
  t-2  Implement bitbucket ship backend, shared dispatch
       files:    internal/ship/open.go, internal/ship/comment.go,
                 internal/ship/merged.go, internal/forge/forge.go
       covers:   c-6, c-2
       depends:  t-1
       desc:     Adds a bitbucket case to OpenPR (POST /2.0/repositories/{ws}/{repo}/
                 pullrequests), PostComment (POST .../pullrequests/{id}/comments with
                 {"content":{"raw":body}}) and PRMerged (GET .../pullrequests/{id},
                 merged == state "MERGED"), all over one bitbucketReq helper using HTTP
                 Basic AuthUser:$AuthEnv; AuthUser is added to OpenOpts/CommentOpts and
                 populated by buildOpenOpts/buildCommentOpts. Every unsupported-provider
                 default branch in ship and in forge.New/NewBoard builds its accept-list
                 and error text from project.ShipProviders / project.BoardProviders
                 instead of an inline literal list.
       contract: - OpenPR{Provider:"bitbucket"} returning "unsupported provider" fails
                   TestOpenPRBitbucketCreatesPullRequest (internal/ship/open_test.go),
                   which asserts the request hits /pullrequests with source.branch.name
                   = HeadBranch and an Authorization: Basic header derived from
                   AuthUser + the auth_env token
                 - PostComment{Provider:"bitbucket"} posting anything other than
                   {"content":{"raw":...}} to .../pullrequests/7/comments fails
                   TestPostCommentBitbucketPostsRawContent
                 - PRMerged{Provider:"bitbucket"} returning ErrMergeStatusUnsupported,
                   or reporting true for state "DECLINED", fails
                   TestPRMergedBitbucketReadsState (internal/ship/merged_test.go)
                 - a missing [remote].auth_user with provider=bitbucket returning a
                   raw 401 instead of the "set [remote].auth_user" error fails
                   TestOpenPRBitbucketRequiresAuthUser
                 - forge.New with provider "sourcehut" whose error text no longer
                   enumerates project.BoardProviders fails
                   TestForgeUnsupportedProviderListsEnum (internal/forge/forge_test.go)

  t-3  Validate doctor and issue enable through enums
       files:    internal/cmd/doctor.go, internal/cmd/issue.go,
                 internal/cmd/doctor_test.go
       covers:   c-1, c-2, c-4, c-5, c-6
       depends:  t-1
       desc:     doctor's board-provider, milestone_mode and auth_scheme switches are
                 replaced by project.Has(...) against the shared sets (so jira, github
                 and basic pass), values are Normalize()d before matching, [board].
                 base_url is only required when the normalized provider is not github,
                 and two new warning-only checks are added: milestone_mode not in
                 MilestoneModesFor(provider), and a non-empty [remote].provider outside
                 ShipProviders (empty and "none" stay silent). Warnings print and do not
                 increment `issues`, so exit stays 0; an unknown board provider or
                 auth_scheme remains a hard failure. issue enable's provider switch reads
                 project.BoardProviders and prints the list from it.
       contract: - `dross doctor` on a project with [board].provider = "jira" (or
                   "github") exiting non-zero fails TestDoctorAcceptsJiraAndGitHubBoard
                 - [board].provider = "github" with no base_url reporting the "not a
                   valid URL" failure fails TestDoctorGitHubBoardBaseURLOptional
                 - milestone_mode = "Version" or " version" reporting invalid fails
                   TestDoctorNormalizesMilestoneModeBeforeValidating
                 - [remote].auth_scheme = "basic" reporting invalid fails
                   TestDoctorAcceptsBasicAuthScheme
                 - provider = "jira" + milestone_mode = "epic" producing no output line,
                   or producing a ✗ / non-zero exit instead of a ⚠ with exit 0, fails
                   TestDoctorWarnsJiraEpicCombination
                 - [remote].provider = "sourcehut" not warning, or [remote].provider =
                   "bitbucket" warning, fails TestDoctorWarnsUnshippableRemoteProvider
                 - `dross issue enable` calling provider "jira" backendless fails
                   TestIssueEnableAcceptsEveryBoardProvider
                   (internal/cmd/issue_test.go)

Wave 3 (depends t-1, t-2, t-3)
  t-4  Add enum/dispatch divergence guard test
       files:    internal/cmd/enum_divergence_test.go, assets/prompts/init.md,
                 assets/prompts/onboard.md
       covers:   c-3, c-6, c-2
       depends:  t-1, t-2, t-3
       desc:     New source-scanning test (same repoRootFromTest + file-read pattern as
                 commands_parity_test.go / interaction_coverage_test.go) extracts the
                 `case "..."` provider literals from OpenPR/PostComment/PRMerged in
                 internal/ship, from New/NewBoard in internal/forge, and the enum
                 references in internal/cmd/doctor.go, then asserts each dispatch set
                 equals its project enum set exactly and that doctor carries no residual
                 provider/mode/scheme string literals. The same test asserts the
                 [remote].provider lists in assets/prompts/init.md and onboard.md name
                 exactly project.ShipProviders; those two prompt lines are corrected in
                 this task (gitlab added, bitbucket kept). Run `make install` after the
                 prompt edits (rule r-01).
       contract: - adding `case "sourcehut":` to OpenPR without adding it to
                   project.ShipProviders fails TestShipDispatchMatchesShipProviders
                 - deleting `case "jira":` from forge.NewBoard while jira stays in
                   project.BoardProviders fails TestBoardDispatchMatchesBoardProviders
                 - reintroducing a literal `"youtrack"` accept-list in doctor.go fails
                   TestDoctorCarriesNoProviderLiterals
                 - removing "gitlab" or "bitbucket" from the provider line in
                   assets/prompts/init.md or onboard.md fails
                   TestPromptProviderListsMatchShipProviders

## Coverage

| criterion | tasks |
|---|---|
| c-1 doctor exits 0 for dispatchable board providers (jira, github) | t-3 |
| c-2 one definition per enum, consumers resolve through it | t-1, t-2, t-3 |
| c-3 divergence between accept-set and dispatch-set fails a test | t-4 |
| c-4 doctor normalises before validating | t-1 (Normalize), t-3 (applied) |
| c-5 provider/mode combination warned at doctor time | t-1 (MilestoneModesFor), t-3 (warning) |
| c-6 ship accepts every written [remote].provider; doctor warns otherwise | t-2 (bitbucket backend), t-3 (doctor warn), t-4 (prompt writer side) |

## Judgment calls

- Enums live in `internal/project` (schema owner), not a new `internal/config` package — project imports nothing internal, so forge/ship/cmd can all depend on it without a cycle; a new package would be structure with no criterion behind it.
- Merged the forge/ship dispatch de-literalisation into the bitbucket task (t-2) rather than giving c-2 its own refactor task — both touch the same switch statements in the same layer; splitting would mean two commits editing the same lines.
- Merged `issue enable` into the doctor task (t-3) instead of a standalone task: it is a single switch plus one print, well under the ten-minute floor.
- Merged the init/onboard prompt corrections into the guard-test task (t-4) rather than a docs task, because the test that pins those lists is the thing that makes the correction stick; a docs-only task would have no enforceable contract.
- `[remote].auth_user` schema field + `dross project set` accessor kept in t-1 even though CLI-writable config is a deferred milestone theme — bitbucket Basic auth is unusable without a way to write the user half, so it is traceable to c-6.
- Chose warning-only for the c-5 combination check and the c-6 remote-provider check with `issues` left untouched (per new_check_severity), and made empty / `none` remote providers silent rather than warned — a repo with no remote is not misconfigured.
- Bitbucket merge status implemented as a real `GET /pullrequests/{id}` read rather than reusing ErrMergeStatusUnsupported: c-6 names "report merge status" explicitly, so the unsupported path would not satisfy it.
- No new `dross validate` work at all, and no enum checks there — the locked `enum_validator_home` decision makes that a non-task; the guard test lives in `internal/cmd` as a Go test instead.
