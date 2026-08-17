# board-sync-truth — verification lens

Phase board-sync-truth — 12 tasks across 3 waves

Every contract below is written against this repo's actual test idiom: `httptest`
fake trackers for wire-level behaviour (`internal/forge/*_test.go`,
`newFakeBoard` in `internal/cmd/issue_backlog_id_test.go`), the
`fakeInboundClient` BoardClient stub for command-layer filtering
(`internal/cmd/issue_test.go:1201`), dotted-path round-trips for config
(`TestBoardDottedArmsRoundTrip`, `internal/cmd/project_test.go:239`), and
substring assertions over `assets/prompts/*.md` for prompt surfaces
(`internal/cmd/inbox_prompt_test.go`).

## Wave 1

```
  t-1  OR label clauses in YouTrack + Jira queries
       files:    internal/forge/youtrack.go, internal/forge/jira.go,
                 internal/forge/youtrack_test.go, internal/forge/jira_test.go
       covers:   c-1
       description:
                 buildQuery emits one alternation clause `tag: {a}, {b}` instead of
                 N space-joined `tag:` clauses; buildJQL emits `labels IN ("a","b")`
                 instead of N `AND labels = ` clauses. Brace-quote YouTrack tag
                 values so a `dross/phase:x` tag does not fragment the query.
       contract: - TestYouTrackBuildQueryORsLabels: buildQuery(IssueFilter{Labels:
                   ["bug","enhancement"]}) contains exactly one `tag:` token; a
                   regression to two `tag:` clauses (today's AND) fails it.
                 - TestYouTrackBuildQueryQuotesSpecialTags: a label `dross/phase:01-x`
                   appears brace-wrapped; an unquoted emission fails it.
                 - TestJiraBuildJQLORsLabels: JQL contains `labels IN ("bug","enhancement")`
                   and contains no `labels = ` substring; today's AND chain fails it.
                 - TestJiraBuildJQLKeepsStateAND: `statusCategory != Done` is still
                   AND-joined — proof the OR change did not loosen the state scope.
       depends:  —

  t-2  Fan out GitHub label queries and dedupe
       files:    internal/forge/github.go, internal/forge/github_test.go
       covers:   c-1
       description:
                 GitHub's REST `labels` param is AND-only, so ListIssues issues one
                 request per label and unions the results, keyed by issue number,
                 preserving first-seen order. Zero labels keeps the single request.
       contract: - TestGitHubListIssuesORsLabels: fake serves #1 for `labels=bug` and
                   #2 for `labels=enhancement`; ListIssues(Labels:["bug","enhancement"])
                   returns both. Today's single comma-joined AND request returns one
                   or zero and fails it.
                 - TestGitHubListIssuesDedupesAcrossLabels: an issue returned by both
                   per-label requests appears exactly once in the result.
                 - TestGitHubListIssuesSingleRequestWhenNoLabels: the fake counts
                   requests; an unlabelled filter must issue exactly 1.
                 - TestGitHubListIssuesStillSkipsPRs: a `pull_request`-carrying entry
                   in one label's response is excluded from the union.
       depends:  —

  t-3  Drop unknown labels and name them on stderr
       files:    internal/forge/forge.go, internal/forge/youtrack.go,
                 internal/forge/jira.go, internal/forge/github.go,
                 internal/forge/forge_test.go, internal/forge/youtrack_test.go,
                 internal/forge/jira_test.go, internal/forge/github_test.go
       description:
                 A shared `filterKnownLabels(want, known) (kept, dropped []string)`
                 helper in forge.go plus a per-client known-label read (YouTrack
                 GET /api/issueTags, Jira GET /rest/api/3/label, GitHub GET
                 /repos/{o}/{r}/labels). ListIssues queries only kept labels and
                 warns on stderr naming each dropped one. An unreadable label index
                 degrades to "query everything asked for" rather than erroring.
       covers:   c-1
       contract: - TestFilterKnownLabelsSplits: want ["bug","typo"] against known
                   ["bug"] returns kept ["bug"], dropped ["typo"]; a helper that
                   returns everything or nothing fails it.
                 - TestYouTrackListIssuesDropsUnknownTag: fake serves issueTags
                   ["bug"]; ListIssues(Labels:["bug","typo"]) issues a query
                   containing `bug` and NOT `typo`, returns the bug issue, and
                   returns a nil error. Today's behaviour (querying `typo`,
                   yielding zero results) fails the non-empty assertion.
                 - TestJiraListIssuesDropsUnknownLabel / TestGitHubListIssuesDropsUnknownLabel:
                   same shape against /rest/api/3/label and /repos/o/r/labels.
                 - TestDroppedLabelsNamedOnStderr: captured stderr contains the
                   literal dropped label name `typo`; a silent drop fails it —
                   silence here reproduces the exact zero-vs-failure confusion c-2
                   exists to kill.
                 - TestUnreadableLabelIndexDoesNotFailTheList: label endpoint 500s;
                   ListIssues still returns the issues for the requested labels
                   with a nil error.
       depends:  —

  t-4  Emit a pull envelope and update its consumers
       files:    internal/cmd/issue.go, assets/prompts/status.md,
                 assets/prompts/inbox.md, internal/cmd/pull_envelope_test.go,
                 internal/cmd/status_prompt_test.go
       covers:   c-2
       description:
                 `dross issue pull --json` marshals `{"issues":[...],"error":null|"..."}`
                 on every path (success, disabled board, board failure) and keeps
                 exit 0 when the board fails. Human mode prints an "unreachable"
                 line naming the error instead of "no new issues on the board".
                 status.md and inbox.md read `.issues` / `.error` and report the
                 board as unreachable rather than counting it as zero.
       contract: - TestPullJSONEnvelopeOnSuccess: stdout unmarshals into a struct
                   `{Issues []forge.Issue; Error *string}` with len(Issues)==1 and
                   Error nil; today's bare array fails the unmarshal.
                 - TestPullJSONEnvelopeOnBoardFailure: fake returns HTTP 500;
                   `pull --json` returns a nil error from RunE (exit 0) AND the
                   envelope's Error is non-empty and names the failure. A version
                   that returns the error (non-zero exit) fails the first half; a
                   version that swallows it into `{"issues":[]}` fails the second.
                 - TestPullJSONEnvelopeWhenBoardDisabled: board.enabled=false emits
                   `{"issues":[],"error":null}`, not `[]`.
                 - TestPullHumanModeReportsUnreachable: non-JSON pull against a
                   500ing board prints a line containing "unreachable" and does NOT
                   print "no new issues on the board".
                 - TestPullMarkSkippedOnFailure: `--mark` against a failing board
                   leaves board.json's last_pull unchanged — stamping a pull that
                   never happened is the silent-zero fault in another shape.
                 - TestStatusPromptReadsPullEnvelope (status.md): contains
                   `.issues` and `.error` and the unreachable wording; must not
                   contain the current claim that pull "emits []".
                 - TestInboxPromptReadsPullEnvelope (inbox.md): same, so /dross-inbox
                   does not read an envelope as a zero-length array.
       depends:  —

  t-5  Add the [board.fields] config surface
       files:    internal/project/project.go, internal/cmd/project.go,
                 internal/forge/forge.go, internal/cmd/issue.go,
                 internal/cmd/project_test.go
       covers:   c-3
       description:
                 project.Board gains a nested `Fields` struct (state, type,
                 fix_versions) mirroring state_map's shape; readDotted/writeDotted/
                 unsetDotted gain `board.fields.*` arms; forge.Config gains a Fields
                 member; boardConfig copies it across the cmd→forge hop.
       contract: - TestBoardFieldsDottedArmsRoundTrip: board.fields.state /
                   .type / .fix_versions survive writeDotted → Save → Load →
                   readDotted; a missing read or write arm fails on that path
                   (same harness as TestBoardDottedArmsRoundTrip).
                 - TestBoardFieldsUnsetClearsEntry: `--unset board.fields.state`
                   reads back empty and leaves .type untouched.
                 - TestBoardConfigCarriesFields: boardConfig(project.Board{Fields:
                   {State:"Статус"}}, …) returns a forge.Config whose Fields.State
                   is "Статус"; a forgotten copy in boardConfig makes every
                   downstream field test unreachable, so this one guards the hop.
       depends:  —

  t-6  Apply tags on YouTrack issue create and update
       files:    internal/forge/youtrack.go, internal/forge/youtrack_test.go
       covers:   c-4, c-6
       description:
                 YouTrackClient.CreateIssue currently discards IssueInput.Labels and
                 UpdateIssue discards IssuePatch.Labels, so no dross label ever
                 reaches a YouTrack issue. Apply them as tags (POST
                 /api/issues/{id}/tags per name, creating the tag when absent), and
                 make a non-nil patch label set a full replace.
       contract: - TestYouTrackCreateIssueAppliesTags: CreateIssue(Labels:
                   ["dross","dross/phase:01-x"]) makes the fake observe both tag
                   names; today's create (which sends no tags at all) fails it.
                 - TestYouTrackUpdateIssueReplacesTags: an issue tagged
                   ["dross","dross/target:a"] patched with ["dross","dross/target:b"]
                   ends with exactly those two — a purely additive implementation
                   leaves `dross/target:a` behind and fails.
                 - TestYouTrackTagFailureDoesNotLoseTheIssue: the tag POST 500s on
                   create; CreateIssue still returns the created issue's key with a
                   warning, so a tagging blip never orphans a real issue.
                 - TestYouTrackIssueRoundTripsTagsAsLabels: GetIssue maps `tags`
                   back into Issue.Labels (already implemented — pinned here because
                   c-4's resolver reads it).
       depends:  —

  t-7  Add an issue-link capability to YouTrack and Jira
       files:    internal/forge/forge.go, internal/forge/youtrack.go,
                 internal/forge/jira.go, internal/forge/youtrack_test.go,
                 internal/forge/jira_test.go
       covers:   c-7
       description:
                 An optional `IssueLinker interface { LinkIssues(from, to string) error }`
                 in forge.go, implemented by YouTrackClient (commands API, "relates
                 to <target>") and JiraClient (POST /rest/api/3/issueLink with a
                 Relates type). GitHubClient and the forge REST Client deliberately
                 do not implement it — that is how the "provider cannot express a
                 link" arm of c-7 is detected.
       contract: - TestYouTrackLinkIssues: LinkIssues("PROJ-9","PROJ-3") POSTs
                   /api/commands with a query naming PROJ-3 and an issues array
                   naming PROJ-9.
                 - TestJiraLinkIssues: LinkIssues posts /rest/api/3/issueLink with
                   inwardIssue/outwardIssue keys and a non-empty type name.
                 - TestJiraLinkIssuesSurfacesMissingLinkType: a 404 on the link type
                   returns a non-nil error (the caller in t-12 turns that into a
                   warn — it must be able to tell).
                 - TestOnlyLinkCapableBackendsImplementIssueLinker: compile-time
                   `var _ IssueLinker = (*YouTrackClient)(nil)` plus a runtime
                   assertion that `(*GitHubClient)(nil)` does NOT satisfy it; if
                   someone later adds a stub no-op to GitHubClient, c-7's warn-arm
                   silently disappears and this test fails.
       depends:  —
```

## Wave 2

```
  t-8  Read YouTrack field names from config       (depends t-5)
       files:    internal/forge/youtrack.go, internal/forge/youtrack_test.go
       covers:   c-3
       description:
                 NewYouTrack stores cfg.Fields with today's literals as defaults
                 ("State", "Type", "Fix versions"). SetState, ensureEpic (both the
                 `Type: Epic` search query and the create payload) and
                 CreateBacklogItem read the configured names instead of the literals.
       contract: - TestYouTrackSetStateUsesConfiguredStateField: a client built with
                   Fields.State="Статус" POSTs customFields[0].name == "Статус";
                   the same test with Fields unset POSTs "State". A hardcoded
                   literal fails the first arm; a missing default fails the second.
                 - TestYouTrackBacklogItemUsesConfiguredFixVersionsField:
                   Fields.FixVersions="Release" makes CreateBacklogItem send that
                   name on the Fix-versions custom field.
                 - TestYouTrackEpicUsesConfiguredTypeField: Fields.Type="Kind" makes
                   ensureEpic's search query read `Kind: Epic` and its create payload
                   carry customFields name "Kind" — the query and the payload must
                   move together or an epic is created and then never found again.
                 - TestYouTrackFieldDefaultsAreTodaysLiterals: a zero-value Config
                   yields exactly "State" / "Type" / "Fix versions", so an existing
                   project syncs unchanged.
       depends:  t-5

  t-9  Resolve a phase's issue from the tracker    (depends t-6)
       files:    internal/cmd/issue.go, internal/board/board.go,
                 internal/cmd/issue_phase_resolve_test.go
       covers:   c-4
       description:
                 Phase issues carry a `dross/phase:<slug>` label at create and
                 update. syncPhase resolves the issue by querying
                 ListIssues{State:"all", Labels:["dross/phase:<slug>"]} — board.json
                 is consulted first as a cache and re-written from the query when it
                 is missing, or when its key no longer resolves to an issue carrying
                 the phase label. board.Board gains DeletePhase for the stale case.
       contract: - TestPhaseSyncStampsPhaseLabel: the create payload's label set
                   contains both `dross` and `dross/phase:01-auth`; the update
                   patch's label set contains it too (a wholesale label replace that
                   forgets it would orphan the issue on the NEXT run, not this one).
                 - TestPhaseSyncAdoptsLiveIssueWithNoBoardJSON: the fake already
                   holds PROJ-7 labelled `dross/phase:01-auth`, board.json is empty;
                   after sync the fake recorded ZERO creates, one update to PROJ-7,
                   and board.json maps 01-auth→PROJ-7. This is the deleted-branch
                   case from c-4 and today's code fails it by minting a duplicate.
                 - TestPhaseSyncQueriesClosedIssuesToo: the resolver's filter carries
                   State "all"; a shipped (resolved) phase issue is still found. An
                   open-only query mints an orphan on every re-ship and fails here.
                 - TestPhaseSyncSelfHealsStaleCache: board.json points at PROJ-99
                   which the fake 404s while PROJ-7 carries the label; sync adopts
                   PROJ-7, records zero creates, and rewrites board.json.
                 - TestPhaseSyncCreatesWhenTrackerHasNoMatch: an empty query result
                   yields exactly one create — proof the resolver did not turn
                   "no issue yet" into "never create".
                 - TestPhaseSyncIsIdempotentAcrossTwoRuns: two consecutive syncs on a
                   fresh repo leave the fake with exactly one issue for the phase.
       depends:  t-6

  t-10 Sync routed deferred items with a target label   (depends t-6)
       files:    internal/cmd/issue.go, internal/cmd/deferred_add.go,
                 internal/cmd/deferred_routed_sync_test.go
       covers:   c-6
       description:
                 syncBacklog stops skipping `d.Target != ""`; deferredBacklogItem
                 attaches a `dross/target:<slug>` label that pushBacklogItems applies
                 on create and re-applies on update, so a re-route re-labels the same
                 issue. The stale "left to board-sync-truth" note in
                 mirrorDeferredAdd's doc comment goes with it.
       contract: - TestBacklogSyncCoversRoutedItems: a spec with one routed and one
                   someday item produces TWO creates; today's `Target != ""` skip
                   produces one and fails it.
                 - TestBacklogSyncRoutedItemCarriesTargetLabel: the routed item's
                   create carries the tag `dross/target:host`.
                 - TestBacklogSyncUpdatesRoutedItemText: edit the routed item's text,
                   re-sync — the fake records one UPDATE on the same key and zero new
                   creates. This is c-6's "stays current" half and the exact staleness
                   deferred_add.go recorded as this phase's problem.
                 - TestBacklogSyncRelabelsOnReroute: re-route to another slug and
                   re-sync — the same issue key ends carrying `dross/target:other`
                   and not `dross/target:host` (relies on t-6's replace semantics).
                 - TestBacklogSyncStillSkipsDismissed: a dismissed item produces no
                   issue — proof the skip removal was scoped to routed items only.
       depends:  t-6
```

## Wave 3

```
  t-11 Resolve on close and verify the read-back   (depends t-8)
       files:    internal/forge/youtrack.go, internal/cmd/issue.go,
                 internal/cmd/issue_close_truth_test.go,
                 internal/forge/youtrack_test.go
       covers:   c-5
       description:
                 YouTrackClient gains ResolveIssue(key, status, override): it maps
                 the status to a state (erroring, not warning, when unmapped), writes
                 it through the configured state field, then GETs `?fields=idReadable,resolved`
                 and errors when `resolved` is still null. syncPhase's `--close`
                 branch dispatches to it for YouTrack instead of the current no-op
                 CloseIssue, and reports the error rather than printing the closed line.
       contract: - TestPhaseSyncCloseResolvesOnYouTrack: `phase-sync 01-x --status
                   complete --close` makes the fake observe a State write of the
                   mapped value AND a follow-up GET; the command prints the closed
                   line. Today's no-op CloseIssue records no State write and fails.
                 - TestPhaseSyncCloseFailsWhenStatusHasNoResolvedState: state_map
                   sets complete="" ; the command returns a NON-NIL error naming the
                   status, and stdout does not contain "closed". Today's
                   warn-and-continue exits 0 and fails both halves.
                 - TestPhaseSyncCloseFailsWhenReadBackIsUnresolved: the fake accepts
                   the State write but reports `"resolved":null`; the command errors.
                   Without the read-back this test cannot fail, which is the point —
                   it is what makes "leaves the issue resolved when read back"
                   observable rather than assumed.
                 - TestPhaseSyncCloseOnCreateAlsoResolves: the close-on-create edge
                   (issue minted then closed in one run) takes the same path — the
                   `doClose && !linked` branch is the one that regressed silently.
                 - TestPhaseSyncCloseSurfacesJiraNoTransition: a Jira board with no
                   done-category transition makes the command error instead of
                   printing the closed line.
       depends:  t-8

  t-12 Link routed items to their target phase issue   (depends t-7, t-9, t-10)
       files:    internal/cmd/issue.go, internal/cmd/deferred_link_test.go
       covers:   c-7
       description:
                 After pushing a routed backlog item, syncBacklog resolves the target
                 phase's issue with t-9's resolver and, when the client satisfies
                 forge.IssueLinker, links the two. No linker, or no target issue yet,
                 warns on stderr naming the target and continues — the item keeps its
                 `dross/target:` label either way, and a later sync links it once the
                 phase issue exists.
       contract: - TestRoutedItemLinksToTargetPhaseIssue: the target phase already has
                   an issue; after backlog-sync the fake recorded one link call naming
                   both keys.
                 - TestRoutedItemWarnsWhenTargetPhaseHasNoIssue: no phase issue —
                   ZERO link calls, exit 0, stderr names the target slug, and the
                   created issue still carries `dross/target:<slug>`. A version that
                   errors here fails the exit-0 assertion; one that stays silent fails
                   the stderr assertion.
                 - TestRoutedItemLinksOnLaterSyncOnceTargetExists: sync (warn, no
                   link) → create the phase issue → re-sync; the fake now records
                   exactly one link call. This is c-7's "established on a later sync"
                   clause and it cannot pass without the warn path leaving the item
                   in a re-linkable state.
                 - TestRoutedItemSkipsLinkOnLinklessProvider: a GitHub board records
                   zero link attempts, warns once, exits 0 — the capability check
                   must be an interface assertion, not a provider-string switch.
                 - TestLinkFailureDoesNotFailTheSync: the link call 500s; backlog-sync
                   still exits 0 with the issue created and labelled.
       depends:  t-7, t-9, t-10
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (label OR + unknown labels tolerated) | t-1, t-2, t-3 |
| c-2 (failure distinguishable from empty)  | t-4 |
| c-3 (configurable YouTrack field names)   | t-5, t-8 |
| c-4 (exactly one issue per phase)         | t-6, t-9 |
| c-5 (close really resolves, or reports)   | t-11 |
| c-6 (routed items mirrored + current)     | t-6, t-10 |
| c-7 (routed item linked to target issue)  | t-7, t-12 |

7/7 criteria covered.

## Judgment calls

- **Phase resolution queries one composite label, not two AND'd labels.** c-4's locked
  decision says "the dross marker label plus the phase id". Chose a single
  `dross/phase:<slug>` label carrying both; rejected a two-label AND query because
  c-1 makes multi-label filters mean OR — an AND'd resolver would silently adopt any
  dross-marked issue and merge two phases onto one issue. One label keeps the
  resolver's semantics independent of the filter change.
- **Unknown-label filtering lives in forge, not the command layer.** Rejected filtering
  in `issuePull` before `ListIssues`: `dross watch` and `dross status` reach
  `collectInbound` on the same path, and a command-layer filter would leave them
  querying unknown labels. One helper in forge.go, called by three ListIssues
  implementations, is the only placement where every caller inherits it.
- **GitHub gets request fan-out; YouTrack and Jira get native OR.** Rejected fanning
  out uniformly (three requests where one would do, on the two providers whose query
  languages express OR) and rejected GitHub's search API (a different response shape
  and a separate rate limit for one criterion's sake). Split as t-1/t-2 because the
  mechanisms differ and so do their contracts.
- **Envelope work includes both prompt consumers.** status.md is the criterion's named
  surface, but inbox.md reads the same `--json` output; shipping the shape change
  without it would leave /dross-inbox parsing an object as an array. Rejected a
  separate prompt task — the break and the fix must land in one commit.
- **`--close` errors instead of warning when no resolved state is mapped.** c-5 says
  "reports the failure instead of success", and every other close-time signal
  (board.json write, the printed closed line) would otherwise claim success. Rejected
  keeping SetState's warn-and-continue on this path: the warn is right for a status
  label mid-flight, wrong for the terminal operation the ship step trusts.
- **YouTrack tag support is its own wave-1 task.** c-4 and c-6 both need labels to
  actually reach a YouTrack issue, and today neither CreateIssue nor UpdateIssue
  sends any. Rejected duplicating it inside both consumers; made it t-6 so the two
  wave-2 tasks share one tested tag path.
- **`IssueLinker` is an optional interface, deliberately unimplemented by GitHub.**
  Rejected a `LinkIssues` method on BoardClient returning ErrNotImplemented: c-7's
  warn-arm would then be an error-string comparison instead of a type assertion, and
  a future stub would silence it undetectably. t-7's contract asserts GitHubClient
  does *not* satisfy the interface, which is what keeps the arm reachable.
- **t-9 is wave 2 behind t-6, and t-11 wave 3 behind t-8** — both are strict output
  dependencies (a phase label that never reaches the tracker cannot be queried; a
  resolve-on-close that hardcodes "State" un-does c-3 on the close path), not merely
  file overlap.
