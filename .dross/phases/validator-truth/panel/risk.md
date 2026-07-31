# risk lens — validator-truth

Lens: every task owns exactly one failure mode. The graph is built from the ways
this surface breaks today (doctor rejecting dispatchable values, three literal
lists drifting, doctor comparing raw strings a consumer normalises, a combination
that only fails at API-call time, a provider the tooling writes but ship cannot
route) plus the failure modes the fix itself introduces (wrong Authorization
header, reviewer failure losing an opened PR, a vacuous guard test).

```
Phase validator-truth — 10 tasks across 3 waves

Wave 1
  t-1  Add enum registry with shared normalisation
       files:    internal/enums/enums.go, internal/enums/enums_test.go
       covers:   c-2, c-4
       contract: Normalize(" Version ") == "version" and Normalize("JIRA") == "jira";
                 BoardProvider.Valid("jira")/("github")/("gitea") are all true;
                 ShipProvider.Valid("bitbucket") and ("gitlab") are both true;
                 MilestoneMode.Valid("") is true (empty = version default) while
                 BoardProvider.Valid("") is false;
                 ModeSupported("jira","epic") returns a non-empty reason string and
                 ModeSupported("youtrack","epic") returns "";
                 List() renders "forgejo | gitea | gitlab | youtrack | jira | github"
                 so no call site can hand-write the set into a message.

  t-2  Add remote.auth_user config field
       files:    internal/project/project.go, internal/cmd/project.go,
                 internal/ship/open.go, internal/ship/comment.go,
                 internal/cmd/project_test.go
       covers:   c-6
       contract: `dross project set remote.auth_user me@example.com` followed by
                 `dross project get remote.auth_user` round-trips through
                 project.toml — the existing project_test.go set/get table gains
                 remote.auth_user and fails on "unknown or unsettable field" if the
                 set path is missing;
                 ship.OpenOpts and ship.CommentOpts both expose AuthUser, so a
                 backend reading opts.AuthUser compiles (removing either field
                 breaks the build in t-4's basic-auth path).

  t-3  Add bitbucket ship wire backend
       files:    internal/ship/bitbucket.go, internal/ship/bitbucket_test.go
       covers:   c-6
       contract: against an httptest server, openBitbucketPR POSTs
                 /repositories/{workspace}/{repo}/pullrequests with
                 source.branch.name = HeadBranch and destination.branch.name =
                 BaseBranch, and returns {id, links.html.href} — a swapped
                 source/destination fails the request-body assertion;
                 the Authorization header is exactly `Basic base64(auth_user:token)`
                 — a test asserts the decoded credential and that no
                 `Authorization: token ...` / `PRIVATE-TOKEN` header is sent;
                 a 401 response yields an error naming the auth_env var, not a nil
                 result;
                 reviewer assignment that 4xxs returns BOTH a non-nil *OpenResult
                 (the PR is open) and a non-nil error — a test asserts the result is
                 not dropped, mirroring the forgejo/gitlab non-fatal contract;
                 bitbucketPRMerged reports true only for state == "MERGED" and false
                 for "OPEN"/"DECLINED"/"SUPERSEDED";
                 Draft = true prefixes the title with "Draft:" (Bitbucket has no
                 draft flag) rather than sending an unknown field.

Wave 2 (depends t-1, t-2, t-3)
  t-4  Route ship dispatch through the registry
       files:    internal/ship/open.go, internal/ship/comment.go,
                 internal/ship/merged.go, internal/cmd/ship.go,
                 internal/ship/dispatch_test.go
       covers:   c-2, c-6
       depends:  t-1, t-2, t-3
       description: OpenPR/PostComment/PRMerged switch on enums.Normalize(provider),
                 route bitbucket to t-3's backend, and return a wrapped
                 ErrUnsupportedProvider (new exported sentinel) whose message is
                 rendered from enums.ShipProvider.List(). buildOpenOpts /
                 buildCommentOpts carry Remote.AuthUser through.
       contract: OpenPR with Provider "BitBucket " (mixed case, trailing space)
                 reaches the bitbucket backend — asserted by the backend's
                 "needs APIBase" error rather than the unsupported-provider sentinel;
                 errors.Is(err, ship.ErrUnsupportedProvider) is true for
                 provider "sourcehut" and false for every enums.ShipProvider value,
                 across all three of OpenPR, PostComment and PRMerged;
                 buildOpenOpts/buildCommentOpts copy remote.auth_user onto the opts —
                 internal/cmd/ship_test.go's field-passthrough assertion fails if the
                 field is dropped (this is the exact bug class it was written for).

  t-5  Teach forge basic auth and registry dispatch
       files:    internal/forge/forge.go, internal/forge/forge_test.go
       covers:   c-2, c-6
       depends:  t-1
       description: forge.New/NewBoard resolve the accepted provider set from
                 enums.BoardProvider via enums.Normalize, export
                 ErrUnsupportedProvider, and Client.do sends
                 `Authorization: Basic base64(AuthUser:token)` when
                 enums.Normalize(authScheme) == "basic".
       contract: with AuthScheme "basic" and AuthUser set, the httptest server sees
                 `Authorization: Basic <base64(user:token)>` and no PRIVATE-TOKEN
                 header — flipping the scheme back to private-token fails it;
                 AuthScheme "basic" with an empty AuthUser returns a construction
                 error naming auth_user rather than sending `Basic base64(:token)`;
                 forge.NewBoard("jira") with an empty APIBase returns the jira
                 config error and errors.Is(err, forge.ErrUnsupportedProvider) is
                 false; NewBoard("sourcehut") returns an error for which it is true.

  t-6  Rebuild doctor's enum checks on the registry
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
       covers:   c-1, c-2, c-4
       depends:  t-1
       description: Replace doctor's three inline literal switches with
                 enums.BoardProvider / MilestoneMode / AuthScheme lookups applied
                 after enums.Normalize, and make [board].base_url optional when the
                 normalised board provider is github.
       contract: a table test iterating every enums.BoardProvider value configures
                 that provider and asserts doctor exits 0 — it fails today for
                 jira, github and (via the message) any value the switch omits;
                 board.provider = "github" with board.base_url empty exits 0, while
                 board.provider = "youtrack" with base_url empty still prints
                 "✗ ... base_url" and exits non-zero (the relaxation must not leak
                 to the other providers);
                 board.milestone_mode = " Version" and "EPIC" exit 0 while "bogus"
                 still prints "✗ ... milestone_mode" and exits non-zero;
                 remote.auth_scheme = "basic" exits 0 while "token" still fails —
                 TestDoctorFlagsInvalidAuthScheme keeps passing.

  t-7  Route issue enable and mode compare through registry
       files:    internal/cmd/issue.go, internal/cmd/issue_test.go
       covers:   c-2, c-4
       depends:  t-1
       description: issueEnable's provider switch and its two note strings render
                 from enums.BoardProvider; syncBacklog's milestone_mode compare
                 uses enums.Normalize instead of bare strings.ToLower.
       contract: with board.milestone_mode = " version " (padded), backlog-sync
                 still tags items with the milestone entity id — today the untrimmed
                 compare in syncBacklog silently leaves fixVersion empty and the
                 items lose their Fix Version;
                 `dross issue enable` with board.provider = "JIRA" prints no
                 "has no board backend" note, while provider = "sourcehut" still
                 prints one listing every registry value.

  t-8  Correct provider lists in init/onboard prompts
       files:    assets/prompts/init.md, assets/prompts/onboard.md, README.md,
                 internal/cmd/prompt_provider_parity_test.go
       covers:   c-2, c-6
       depends:  t-1
       description: The **Provider** bullets in both prompts list exactly the set
                 ship dispatches (gitlab added, bitbucket retained) plus "none";
                 README's ship row notes bitbucket. A new parity test reads the
                 installed asset text and compares it against the registry.
       contract: TestPromptProviderListsMatchShipDispatch extracts the provider
                 tokens from each prompt's provider bullet and fails when the set
                 differs from enums.ShipProvider.Values plus "none" — it fails
                 before the edit (gitlab missing from both prompts) and fails again
                 if a future ship backend is added without touching the prompts.
                 Per rule r-01 the asset edit needs `make install` before any manual
                 re-run of the slash command.

Wave 3 (depends t-4, t-5, t-6, t-7)
  t-9  Add doctor cross-field combination warnings
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
       covers:   c-5, c-6
       depends:  t-4, t-6
       description: After the per-field checks pass, doctor reports (as ⚠, never
                 counted into the exit-gating issue tally) an unsupported
                 provider/milestone_mode pair via enums.ModeSupported, a
                 [remote].provider outside enums.ShipProvider, and
                 auth_scheme = basic with no auth_user.
       contract: board.provider = "jira" + milestone_mode = "epic" prints a ⚠ line
                 naming both fields AND doctor returns a nil error — the test
                 asserts exit 0, so promoting this to a hard failure breaks it;
                 the same pair on provider = "youtrack" prints no warning;
                 remote.provider = "none" and "sourcehut" each print a ⚠ naming
                 ship, still exit 0, while remote.provider = "bitbucket" prints
                 none (it dispatches after t-4);
                 remote.auth_scheme = "basic" with remote.auth_user empty prints a
                 ⚠ naming auth_user and exits 0.

  t-10 Add validator/dispatch divergence guard test
       files:    internal/cmd/enum_dispatch_parity_test.go
       covers:   c-3
       depends:  t-4, t-5, t-6, t-7
       description: One test file, patterned on commands_parity_test.go, that walks
                 each registry and exercises the real dispatch entry points with
                 deliberately incomplete config so no network is touched — the only
                 assertion is on the unsupported-provider sentinel.
       contract: for every enums.ShipProvider value, ship.OpenPR, ship.PostComment
                 and ship.PRMerged return an error that is NOT
                 ship.ErrUnsupportedProvider (each backend trips its own
                 "needs APIBase" / "needs the gh CLI" guard first) — adding a value
                 to the registry without a switch arm fails here;
                 for every enums.BoardProvider value, forge.NewBoard returns an
                 error that is NOT forge.ErrUnsupportedProvider — same guard on the
                 board side;
                 a synthetic provider "dross-test-nonexistent" DOES return the
                 sentinel from all four entry points, so the guard cannot pass
                 vacuously by a sentinel that is never returned;
                 removing a value from enums.ShipProvider while a ship switch arm
                 still handles it fails the reverse assertion (every case label
                 reachable from the registry).
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 doctor exits 0 for any dispatchable board provider | t-6 |
| c-2 single enum definition, consumers resolve through it | t-1, t-4, t-5, t-6, t-7, t-8 |
| c-3 test fails when accept-set and dispatch-set diverge | t-10 |
| c-4 doctor normalises the same way the consumer does | t-1, t-6, t-7 |
| c-5 doomed provider/mode combination reported by doctor | t-9 |
| c-6 ship accepts every written [remote].provider | t-2, t-3, t-4, t-8, t-9 |

## Judgment calls

- Registry is a new leaf package `internal/enums`, not a home in `internal/project` — forge deliberately keeps its own `Config` to avoid importing project, and a leaf enums package lets doctor, issue, forge and ship all import it without inverting the documented `cmd → project, never project → cmd` direction or creating a cycle.
- The divergence guard (t-10) exercises the real dispatch functions and asserts on an exported sentinel, rather than comparing two Go slices. A slice-vs-slice test passes while the switch is wrong; calling `OpenPR` with an empty APIBase reaches the switch and stops before any HTTP, so the guard tests the actual routing without a network.
- The guard includes a negative case (a synthetic provider that MUST return the sentinel). Rejected: guard-by-positive-assertions-only — if a refactor stops returning the sentinel entirely, an all-positive guard goes green forever.
- Bitbucket lands as two tasks (wire backend t-3, dispatch wiring t-4) rather than one. The wire risks (auth header, body shape, reviewer partial failure, merged-state string) are testable against httptest with zero dependency on the registry, so they run in wave 1 in parallel with t-1 instead of queueing behind it.
- t-2 and t-4 each list 5 files. Splitting the `internal/cmd/ship.go` opts passthrough into its own task would create a sub-10-minute task AND split one risk — "auth_user never reaches the wire" — across two owners, which is exactly what this lens forbids. Both tasks stay within the ship/config layer.
- t-6 (accept-sets) and t-9 (cross-field warnings) are separate tasks in different waves although both edit doctor.go. They are distinct failure modes with distinct exit-code contracts (hard fail vs warning-and-exit-0); merging them risks the new warnings quietly inheriting the old checks' non-zero exit, which the locked `new_check_severity` decision forbids. The wave split is honest — they conflict on one file.
- Bitbucket reviewers are passed through as account ids / UUIDs and a lookup failure is non-fatal, matching the gitlab path. Rejected: resolving usernames via `GET /users/{username}`, which Bitbucket restricts for privacy and would make reviewer config a hard ship blocker.
- README.md is included in t-8 rather than deferred. The set of ship providers is user-facing truth and the previous milestone closed on a readme-truth pass; leaving bitbucket undocumented recreates the same class of lie this phase exists to kill.
- `dross validate` is untouched everywhere, and no task adds an enum check there — per the locked `enum_validator_home` decision. The parity test that could have lived in validate lives in `go test` (t-10) instead, so it gates CI without failing existing repos at wrap time.
