# risk lens — board-sync-truth

Every criterion here exists because something silently lied: a filter that
returned nothing, a fetch that looked like an empty board, a close that never
closed. The decomposition follows the failure, not the feature — each task owns
exactly one way the sync can be wrong, and its contract is the test that catches
that wrongness coming back.

```
Phase board-sync-truth — 12 tasks across 3 waves

Wave 1
  t-1  OR label queries + known-label lookup
       files:    internal/forge/youtrack.go, internal/forge/jira.go,
                 internal/forge/github.go, internal/forge/youtrack_test.go,
                 internal/forge/jira_test.go, internal/forge/github_test.go
       desc:     YouTrack buildQuery emits one comma-joined `tag:` clause; Jira
                 buildJQL emits `labels in (...)`; GitHub fans out one request
                 per label and dedupes by issue number. Each backend gains
                 KnownLabels() reading its tag/label endpoint.
       covers:   c-1
       contract: - buildQuery({a,b}) produces a single `tag: a, b` clause — a
                   query containing two separate `tag:` clauses fails the test
                   (that shape is today's AND bug)
                 - buildJQL({a,b}) contains `labels in ("a","b")` and no
                   ` AND labels = ` substring
                 - GitHub ListIssues with 2 labels issues exactly 2 GETs and an
                   issue returned by both appears once in the result
                 - KnownLabels hits /api/issueTags (YT), /rest/api/3/label
                   (Jira), /repos/o/r/labels (GH) and returns the names

  t-2  Envelope `dross issue pull` failures
       files:    internal/cmd/issue.go,
                 internal/cmd/issue_pull_envelope_test.go,
                 assets/prompts/status.md, assets/prompts/inbox.md
       desc:     `--json` emits {"issues":[...],"error":null|"..."}; a client
                 construction or ListIssues failure lands in `error` at exit 0;
                 --mark does not stamp last_pull on a failed run. Both prompts
                 read the envelope and report the board unreachable.
       covers:   c-2
       contract: - a board server answering 500 makes `pull --json` exit 0 with
                   a non-null `error` and `"issues":[]` — a bare `[]` payload or
                   a non-zero exit fails
                 - `pull --mark --json` against that same 500 leaves board.json's
                   last_pull byte-identical
                 - board sync disabled emits {"issues":[],"error":null}, not the
                   failure shape
                 - human (non-json) mode on the 500 prints a line containing
                   "unreachable" to stderr and still exits 0
                 - status.md/inbox.md contain no bare `dross issue pull … --json`
                   step that treats the output as an array (prompt-text assertion
                   in the existing prompt-test style)

  t-3  Add [board.fields] config surface
       files:    internal/project/project.go, internal/cmd/project.go,
                 internal/cmd/project_test.go, README.md
       desc:     New nested `[board.fields]` table (state, type, version)
                 mirroring board.state_map, with the same one-leaf-at-a-time
                 dotted get/set/--unset handling and README row.
       covers:   c-3
       contract: - `project set board.fields.state Status` writes
                   `[board.fields]\nstate = "Status"` and `get board.fields.state`
                   reads it back
                 - `set board.fields.bogus x` is rejected naming the three valid
                   keys, and project.toml is byte-unchanged after the refusal
                 - a project.toml with no [board.fields] table reads back "" for
                   each key (no nil-map panic, matching the state_map path)
                 - `set --unset board.fields.state` clears only that key

  t-4  Make YouTrack close resolve issues
       files:    internal/forge/forge.go, internal/forge/youtrack.go,
                 internal/forge/jira.go, internal/forge/youtrack_test.go,
                 internal/forge/jira_test.go
       desc:     Add Issue.Resolved, populated from YouTrack's `resolved` field
                 (added to ytIssueFields) and Jira's statusCategory. YouTrack
                 CloseIssue stops returning nil — it errors, naming SetState, so
                 no caller can mistake a no-op for a close.
       covers:   c-5
       contract: - GetIssue on a YouTrack payload with `"resolved":1700000000000`
                   yields Resolved true; `"resolved":null` yields false
                 - YouTrackClient.CloseIssue returns a non-nil error mentioning
                   SetState (a nil return fails — that silent nil is the bug)
                 - a jiraIssue with statusCategory key "done" yields Resolved
                   true and State "closed"

  t-5  Write YouTrack tags on update
       files:    internal/forge/youtrack.go, internal/forge/youtrack_test.go
       desc:     UpdateIssue honours IssuePatch.Labels: ensure each tag entity
                 exists via /api/issueTags, then set the issue's tags to exactly
                 the patch set so removals actually remove.
       covers:   c-6
       contract: - UpdateIssue{Labels:{a,b}} on an issue tagged {a,c} sends a
                   tags array of exactly [a,b] — a payload that leaves `c`
                   attached fails
                 - a tag name missing from /api/issueTags is POSTed once before
                   the issue write; an already-existing one triggers no create
                 - UpdateIssue with Labels nil sends no tags key at all

  t-6  Add issue-link capability to Jira/YouTrack
       files:    internal/forge/forge.go, internal/forge/jira.go,
                 internal/forge/youtrack.go, internal/forge/jira_test.go,
                 internal/forge/youtrack_test.go
       desc:     IssueLinker interface (LinkIssues(from, to)) + ErrNoLinkType.
                 Jira reads /issueLinkType, skips when the pair is already in the
                 issue's issuelinks, else POSTs /issueLink. YouTrack reuses the
                 set-semantics commands API.
       covers:   c-7
       contract: - Jira LinkIssues POSTs /rest/api/3/issueLink exactly once for a
                   new pair
                 - on a re-run where GET issue?fields=issuelinks already shows
                   the pair, zero POSTs are sent (duplicate-link regression)
                 - an empty /issueLinkType list makes LinkIssues return
                   ErrNoLinkType, not a generic HTTP 400
                 - YouTrack LinkIssues posts a /api/commands body naming both
                   readable keys

Wave 2 (depends on wave 1)
  t-7  Plumb field names into YouTrack writes
       files:    internal/forge/forge.go, internal/forge/youtrack.go,
                 internal/cmd/issue.go, internal/forge/youtrack_test.go
       desc:     Config gains the three field names; boardConfig passes
                 [board.fields] through; SetState, ensureEpic and
                 CreateBacklogItem read them instead of the literals, and
                 ensureVersion prefers the named field when resolving the bundle.
       covers:   c-3
       depends:  t-3
       contract: - with fields.state="Statuts", SetState's payload customField is
                   named "Statuts"; with the table empty it is "State"
                 - with fields.type="Kind", ensureEpic's search query reads
                   "Kind: Epic" and the create payload's custom field is "Kind"
                 - with fields.version="Release", CreateBacklogItem's fixVersion
                   custom field is named "Release"
                 - with two VersionBundle-typed fields on the project,
                   ensureVersion writes to the one whose field name matches
                   fields.version rather than the first in the response

  t-8  Verify close, fail on unmapped state
       files:    internal/cmd/issue.go, internal/cmd/issue_close_truth_test.go
       desc:     syncPhase's --close path stops warn-and-continuing: an unmapped
                 status is an error, and after the state write the issue is read
                 back and the command fails unless Resolved is true. The
                 "(closed)" line prints only after that read-back.
       covers:   c-5
       depends:  t-4
       contract: - with board.state_map.complete blanked, `phase-sync x --status
                   complete --close` exits non-zero naming the unmapped status
                   (a stderr warning plus exit 0 fails)
                 - a mock whose read-back returns `"resolved":null` makes the
                   command exit non-zero and print no "(closed)" line
                 - a read-back showing resolved set prints "(closed)" and exits 0
                 - phase-sync WITHOUT --close on an unmapped status still warns
                   and exits 0 (the lenient path is preserved)

  t-9  Resolve phase issue from the tracker
       files:    internal/cmd/issue.go, internal/board/board.go,
                 internal/cmd/issue_phase_resolve_test.go
       desc:     Phase issues carry a `dross/phase:<id>` label. Resolution order:
                 board.json cache (verified live) → single-label tracker query →
                 exact-title match among marker-labelled issues → create. The
                 cache is rewritten from whatever the tracker says; board.go gains
                 DeletePhase for the stale-entry heal.
       covers:   c-4
       depends:  t-1
       contract: - empty board.json + a tracker issue labelled `dross/phase:x`
                   makes phase-sync update that key and issue zero creates (any
                   POST /issues in the mock fails the test)
                 - a board.json entry whose issue 404s is re-resolved and
                   rewritten, not surfaced as an error
                 - two issues carrying the same phase label produce one update on
                   the lower key plus a stderr duplicate warning, never two writes
                 - the resolution query carries exactly one label, so the OR
                   semantics from t-1 cannot widen it
                 - a legacy issue with the marker label and the exact
                   `<id> — <title>` summary is adopted and back-filled with the
                   phase label

  t-10 Prune unknown labels at pull
       files:    internal/forge/forge.go, internal/cmd/issue.go,
                 internal/cmd/issue_test.go
       desc:     LabelLister + PruneUnknownLabels in forge; collectInbound prunes
                 before querying and names dropped labels on stderr. A failed
                 label lookup becomes an envelope error — never an unfiltered
                 query.
       covers:   c-1, c-2
       depends:  t-1, t-2
       contract: - `pull --labels bug,nope` against a tracker knowing only "bug"
                   sends a query carrying `bug` alone and prints "nope" on stderr
                 - when every requested label is unknown the run returns zero
                   issues; a test asserting the outgoing query carried no label
                   clause (i.e. the whole board) fails
                 - a 500 from the label endpoint yields a non-null envelope error
                   and zero ListIssues calls
                 - a client with no LabelLister passes labels through unchanged

  t-11 Sync routed deferred items with target label
       files:    internal/cmd/issue.go,
                 internal/cmd/issue_routed_backlog_test.go,
                 internal/cmd/deferred_board_test.go
       desc:     syncBacklog stops skipping Target != ""; deferredBacklogItem
                 carries labels including `dross/target:<slug>`; pushBacklogItems'
                 update path patches Labels so a re-route replaces the old target
                 label. Dismissed items stay skipped.
       covers:   c-6
       depends:  t-5
       contract: - an item routed to `alpha` produces exactly one created issue
                   whose labels include `dross/target:alpha`
                 - after `deferred route --target beta`, re-sync updates the SAME
                   issue key and the patched label set contains
                   `dross/target:beta` and not `dross/target:alpha`
                 - editing the item's text updates that issue's summary with zero
                   creates (the c-6 "stays current" half)
                 - a dismissed routed item produces no issue

Wave 3 (depends on wave 2)
  t-12 Link routed items to target phase issue
       files:    internal/cmd/issue.go, internal/cmd/issue_link_target_test.go
       desc:     After pushing a routed item, resolve its target phase's issue
                 (t-9's resolver) and call LinkIssues when the client implements
                 IssueLinker. Missing target issue, missing link type or a
                 linkless provider warns on stderr and continues.
       covers:   c-7
       depends:  t-6, t-9, t-11
       contract: - a routed item whose target phase has an issue produces exactly
                   one LinkIssues call
                 - a target phase with no issue yet: zero link calls, the item
                   keeps its `dross/target:<slug>` label, one stderr warning, exit 0
                 - a github fixture (no IssueLinker) warns once and still creates
                   the backlog item, exit 0
                 - a second sync run after the target issue exists performs the
                   link (the deferred-link half of c-7)
                 - ErrNoLinkType from the linker warns and continues rather than
                   aborting the whole backlog sync
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (label OR + unknown label tolerated) | t-1, t-10 |
| c-2 (fetch failure ≠ empty board) | t-2, t-10 |
| c-3 (configurable YouTrack field names) | t-3, t-7 |
| c-4 (one issue per phase, tracker-resolved) | t-9 |
| c-5 (close actually resolves; failure reported) | t-4, t-8 |
| c-6 (routed deferred items mirrored + current) | t-5, t-11 |
| c-7 (routed item linked to target phase issue) | t-6, t-12 |

Every criterion has at least one task; no task is criterion-less.

## Judgment calls

- **Split every criterion at the forge/cmd seam.** Chose forge-side capability in
  wave 1 and cmd-side policy in wave 2 (t-3/t-7, t-4/t-8, t-5/t-11, t-6/t-12).
  Rejected one-task-per-criterion: it puts the "does the tracker accept this?"
  risk and the "does dross decide correctly?" risk in the same commit, so a
  failing test can't tell you which half is wrong.
- **Phase resolution queries ONE label, not two.** c-1 makes multi-label queries
  OR, so a `[dross, dross/phase:x]` resolution query would match every dross
  issue on the board and adopt an arbitrary one. Rejected the marker+phase pair;
  the phase label alone is already unique, and the marker is verified on the
  returned issue instead.
- **Kept an exact-title fallback in t-9.** Rejected label-only resolution: every
  issue that exists on the real board today predates the `dross/phase:` label,
  so a label-only resolver would mint exactly the orphan c-4 exists to kill,
  once, for every live phase. The fallback costs one extra request only on the
  miss path.
- **YouTrack CloseIssue errors instead of no-opping.** Rejected leaving it
  returning nil and routing around it in the cmd layer: a nil return is a lie any
  future caller inherits. c-5 is precisely about a close that reported success
  without closing.
- **Unmapped state is fatal only under `--close`.** Rejected making it fatal
  everywhere: `phase-sync --status verifying` on a repo with a partial state map
  is a routine lenient no-op, and hardening it would break the always-safe-to-run
  contract for a case where nothing was promised. With `--close` the caller
  asserted a terminal state, so silence there is the c-5 fault.
- **Label lookup failure is an envelope error, not a silent unfiltered pull.**
  Rejected degrading to "query with no labels": that turns a network blip into
  the entire board landing in triage, which is c-2's silent-lie shape wearing a
  different hat.
- **Prompt edits (status.md, inbox.md) ride with t-2.** Rejected a separate
  prompt task: the envelope changes the `--json` contract, so a wave where the
  CLI emits the envelope and the prompts still parse an array is a window with
  a broken /dross-status. They land in one commit.
- **`dross/target:<slug>` label churn owned by t-11, tag writes by t-5.**
  Rejected folding YouTrack tag-write support into t-11: the "removed label
  actually removes" risk is a wire-level YouTrack behaviour, independently
  testable, and c-7 needs it too.
- **Forge REST backends (forgejo/gitea/gitlab) left on today's label
  semantics.** c-1 names youtrack, jira and github; widening to the REST
  backends would add untestable-against-reality guesses about each server's
  labels-param semantics. Flagged as a real inconsistency, deliberately not
  fixed here.
