# board-sync-truth — panel synthesis

## Scores

| dimension | risk | mvp | verification |
|---|---|---|---|
| criteria coverage | 7/7, but c-4/c-6 rest on labels reaching YouTrack and no task writes tags on **create** — t-5 patches `UpdateIssue` only, so a freshly minted phase issue carries no `dross/phase:` label to resolve by next run. | 7/7, thinnest depth: no `--mark`-on-failure arm, no closed-issue (`State:"all"`) arm, no dismissed-item guard, and it explicitly drops production read-back for c-5's "when read back". | 7/7 with the most arms per criterion — the only draft covering create-path tags, `State:"all"` resolution, self-healing stale cache, and the "link established on a later sync" clause of c-7 as its own contract. |
| test-contract specificity | Strong. Best use of *negative* assertions ("a nil return fails — that silent nil is the bug", "a bare `[]` payload fails"), which pin the current defect rather than the desired shape. No test names. | Adequate. Exact wire strings (full JQL, full YouTrack query) are the sharpest thing here, but several contracts assert behaviour without naming the failing-today shape. | Strongest. Named test functions, cited against this repo's actual harnesses (`newFakeBoard`, `fakeInboundClient` at `internal/cmd/issue_test.go:1201`, `TestBoardDottedArmsRoundTrip` at `internal/cmd/project_test.go:239`), plus compile-time interface assertions. Every contract states what fails today. |
| granularity | Best balance. One task per failure mode, split at the forge/cmd seam so a red test says *which* half is wrong. Tasks are evenly sized. | Too coarse. t-1 fuses three concepts across 7 files (OR queries, label listing, tag writes); t-5 fuses c-6 and c-7. A failure inside t-1 doesn't localise. | Good, mildly over-split: t-1/t-2 (YouTrack+Jira OR vs GitHub fan-out) are two small tasks for one criterion, though the mechanisms genuinely differ. |
| wave correctness | Waves are declared but under-constrained: t-9 depends on t-1 only, missing the tag-write dependency — with create-path tags absent entirely, t-9's contract is unbuildable as written. | Correct but trivially so — 5 tasks in 3 waves means the wave graph carries almost no information. t-3→t-1 and t-5→t-4 are real. | Best. Dependencies are argued as *output* dependencies, not file overlap ("a phase label that never reaches the tracker cannot be queried"; "a resolve-on-close that hardcodes State un-does c-3"). Wave 1's seven tasks are genuinely independent. |

**Skeleton: `verification`.** It is the only draft whose task set is closed under its own contracts — every behaviour a later task asserts is produced by an earlier one (create-path tags before label resolution, `[board.fields]` before the close path reads the state field). Its contracts are written against harnesses that exist in this repo, so they can be executed rather than re-derived. Grafted from `risk`: the forge/cmd seam reasoning, the negative-assertion style, the legacy-title fallback, the lenient-path-preserved arm on `--close`, the multi-bundle `ensureVersion` contract, and the duplicate-label warning. Grafted from `mvp`: the exact wire strings, the `project set` unknown-key rejection, and the board-disabled envelope arm.

## Merged plan

```
Phase board-sync-truth — 12 tasks across 3 waves

Wave 1
  t-1  OR label clauses in YouTrack + Jira queries
       files:    internal/forge/youtrack.go, internal/forge/jira.go,
                 internal/forge/youtrack_test.go, internal/forge/jira_test.go
       covers:   c-1
       origin:   [verification] skeleton; wire strings [mvp]; negative arm [risk]
       contract: - buildQuery({bug,enhancement}) emits exactly ONE `tag:` token
                   (`tag: bug, enhancement`); today's two space-joined `tag:`
                   clauses (the AND bug) fail it
                 - a label `dross/phase:01-x` is brace-wrapped in the query; an
                   unquoted emission fails — the phase resolver in t-9 depends on it
                 - buildJQL({bug,enhancement}) contains `labels IN ("bug","enhancement")`
                   and contains no ` AND labels = ` substring
                 - `statusCategory != Done` is still AND-joined — proof the OR
                   change did not widen the state scope

  t-2  Fan out GitHub label queries and dedupe
       files:    internal/forge/github.go, internal/forge/github_test.go
       covers:   c-1
       origin:   [verification+mvp+risk] (all three agree on fan-out over search API)
       contract: - fake serves #1 for `labels=bug`, #2 for `labels=enhancement`;
                   ListIssues with both returns both, via exactly 2 GETs
                 - an issue returned by both requests appears exactly once
                 - an unlabelled filter issues exactly 1 request
                 - a `pull_request`-carrying entry in one label's response is
                   excluded from the union

  t-3  Known-label lookup: drop unknown labels, name them on stderr
       files:    internal/forge/forge.go, internal/forge/youtrack.go,
                 internal/forge/jira.go, internal/forge/github.go,
                 internal/forge/forge_test.go, internal/forge/youtrack_test.go,
                 internal/forge/jira_test.go, internal/forge/github_test.go
       covers:   c-1, c-2
       origin:   [verification] placement + helper; failure semantics [risk];
                 LabelLister interface name [mvp]
       contract: - filterKnownLabels(["bug","typo"], ["bug"]) → kept ["bug"],
                   dropped ["typo"]; a helper returning all or nothing fails
                 - YouTrack/Jira/GitHub each read their label index
                   (/api/issueTags, /rest/api/3/label, /repos/o/r/labels) and
                   query only kept labels; the dropped name never reaches the wire
                 - captured stderr contains the literal dropped name `typo`; a
                   silent drop fails — silence here is the same zero-vs-failure
                   confusion c-2 exists to kill
                 - when EVERY requested label is unknown the call returns zero
                   issues; a query carrying no label clause (i.e. the whole board)
                   fails the test  [see D3]
                 - a 500 from the label endpoint surfaces as an error and zero
                   ListIssues calls — never an unfiltered query  [see D2]

  t-4  Emit a pull envelope and update both prompt consumers
       files:    internal/cmd/issue.go, assets/prompts/status.md,
                 assets/prompts/inbox.md, internal/cmd/pull_envelope_test.go,
                 internal/cmd/status_prompt_test.go
       covers:   c-2
       origin:   [verification] skeleton; --mark arm [risk+verification];
                 disabled-board arm [mvp+verification]
       contract: - success: stdout unmarshals into `{Issues []forge.Issue; Error *string}`
                   with Error nil; today's bare array fails the unmarshal
                 - board 500: RunE returns nil (exit 0) AND Error is non-empty and
                   names the failure. Returning the error fails the first half;
                   `{"issues":[]}` fails the second
                 - board.enabled=false emits `{"issues":[],"error":null}`, not `[]`
                 - human (non-JSON) mode on the 500 prints a line containing
                   "unreachable" and does NOT print "no new issues on the board"
                 - `--mark` against a failing board leaves board.json's last_pull
                   byte-identical — stamping a pull that never happened is the
                   silent-zero fault wearing a different hat
                 - status.md and inbox.md both reference `.issues` / `.error` and
                   the unreachable wording; no bare `--json`-as-array step survives

  t-5  Add the [board.fields] config surface
       files:    internal/project/project.go, internal/cmd/project.go,
                 internal/forge/forge.go, internal/cmd/issue.go,
                 internal/cmd/project_test.go, README.md
       covers:   c-3
       origin:   [verification] skeleton + boardConfig hop guard; key rejection
                 [mvp]; byte-unchanged-on-refusal + README row [risk]
       contract: - board.fields.state / .type / .fix_versions survive
                   writeDotted → Save → Load → readDotted (same harness as
                   TestBoardDottedArmsRoundTrip)
                 - `--unset board.fields.state` reads back empty and leaves
                   .type untouched
                 - `project set board.fields.bogus x` is rejected naming the three
                   accepted keys, and project.toml is byte-unchanged after refusal
                 - a project.toml with no [board.fields] table reads back "" per
                   key — no nil-map panic, matching the state_map path
                 - boardConfig(project.Board{Fields:{State:"Статус"}}, …) returns a
                   forge.Config whose Fields.State is "Статус"; a forgotten copy
                   here makes every downstream field test unreachable

  t-6  Apply tags on YouTrack issue create AND update
       files:    internal/forge/youtrack.go, internal/forge/youtrack_test.go
       covers:   c-4, c-6
       origin:   [mvp+verification] (risk covers the update half only)
       note:     verified in source — CreateIssue sends no tags at all today, so no
                 dross marker has ever reached a YouTrack issue
       contract: - CreateIssue(Labels:["dross","dross/phase:01-x"]) makes the fake
                   observe both tag names; today's create fails it
                 - an issue tagged ["dross","dross/target:a"] patched with
                   ["dross","dross/target:b"] ends with exactly those two — a
                   purely additive implementation leaves `dross/target:a` and fails
                 - a tag name absent from the tag index is created once before the
                   issue write; an existing one triggers no create
                 - UpdateIssue with Labels nil sends no tag write at all
                 - a 500 on the tag write during create still returns the created
                   issue's key with a warning — a tagging blip must not orphan a
                   real issue
                 - GetIssue maps `tags` back into Issue.Labels (pinned; t-9 reads it)

  t-7  Add an issue-link capability to YouTrack and Jira
       files:    internal/forge/forge.go, internal/forge/youtrack.go,
                 internal/forge/jira.go, internal/forge/youtrack_test.go,
                 internal/forge/jira_test.go
       covers:   c-7
       origin:   [risk+verification]; ErrNoLinkType [risk]; non-implementation
                 assertion [verification]
       note:     YouTrack's existing LinkSubtask(parent, child) is subtask-shaped,
                 not relates-to — LinkIssues is a new method, not a rename
       contract: - YouTrack LinkIssues("PROJ-9","PROJ-3") POSTs /api/commands with
                   a query naming PROJ-3 and an issues array naming PROJ-9
                 - Jira LinkIssues POSTs /rest/api/3/issueLink exactly once for a
                   new pair, with inwardIssue/outwardIssue and a non-empty type
                 - on a re-run where the issue's issuelinks already show the pair,
                   zero POSTs are sent (duplicate-link regression)
                 - an empty/404 /issueLinkType yields ErrNoLinkType, not a generic
                   HTTP error — t-12 must be able to tell the difference
                 - compile-time `var _ IssueLinker = (*YouTrackClient)(nil)` plus a
                   runtime assertion that (*GitHubClient)(nil) does NOT satisfy it;
                   a future no-op stub would silence c-7's warn arm undetectably

Wave 2 (depends on wave 1)
  t-8  Read YouTrack field names from config
       files:    internal/forge/youtrack.go, internal/forge/youtrack_test.go
       covers:   c-3
       depends:  t-5
       origin:   [verification] skeleton; multi-bundle arm [risk]
       contract: - Fields.State="Статус" makes SetState POST customFields[0].name
                   == "Статус"; with Fields unset it POSTs "State"
                 - Fields.FixVersions="Release" makes CreateBacklogItem send that
                   name on the fix-versions custom field
                 - Fields.Type="Kind" makes ensureEpic's search query read
                   `Kind: Epic` AND its create payload carry "Kind" — query and
                   payload must move together or an epic is created then never found
                 - with two VersionBundle-typed fields on the project, ensureVersion
                   writes to the one whose name matches Fields.FixVersions, not the
                   first in the response
                 - a zero-value Config yields exactly "State" / "Type" /
                   "Fix versions", so an existing project syncs unchanged

  t-9  Resolve a phase's issue from the tracker
       files:    internal/cmd/issue.go, internal/board/board.go,
                 internal/cmd/issue_phase_resolve_test.go
       covers:   c-4
       depends:  t-1, t-6
       origin:   [verification] skeleton; single-label rationale [all three];
                 legacy title fallback + duplicate warning [risk]
       contract: - create payload AND update patch both carry `dross` and
                   `dross/phase:01-auth` (a wholesale label replace that forgets it
                   orphans the issue on the NEXT run, not this one)
                 - fake already holds PROJ-7 labelled `dross/phase:01-auth`,
                   board.json empty: after sync ZERO creates, one update to PROJ-7,
                   board.json maps 01-auth→PROJ-7 (the deleted-branch case in c-4)
                 - the resolver's filter carries exactly ONE label and State "all";
                   a two-label filter fails (t-1 makes it OR and it would adopt an
                   arbitrary dross issue), and an open-only query mints an orphan
                   on every re-ship
                 - a ListIssues hit whose labels lack `dross` is NOT adopted
                 - board.json points at PROJ-99 which 404s while PROJ-7 carries the
                   label: sync adopts PROJ-7, zero creates, board.json rewritten
                   (board.Board gains DeletePhase)  [see D5]
                 - a legacy issue carrying the marker label and the exact
                   `<id> — <title>` summary but no phase label is adopted and
                   back-filled with the phase label  [see D6]
                 - two issues carrying the same phase label produce one update on
                   the lower key plus a stderr duplicate warning, never two writes
                 - an empty query result yields exactly one create — proof the
                   resolver did not turn "no issue yet" into "never create"
                 - two consecutive syncs on a fresh repo leave exactly one issue

  t-10 Sync routed deferred items with a target label
       files:    internal/cmd/issue.go, internal/cmd/deferred_add.go,
                 internal/cmd/deferred_routed_sync_test.go
       covers:   c-6
       depends:  t-6
       origin:   [verification] skeleton; dismissed guard [risk+verification];
                 re-route relabel [all three]
       contract: - a spec with one routed and one someday item produces TWO
                   creates; today's `Target != ""` skip produces one and fails
                 - the routed item's create carries `dross/target:host`
                 - edit the item's text and re-sync: ONE update on the same key,
                   zero creates (c-6's "stays current" half)
                 - re-route to another slug and re-sync: the same issue key ends
                   carrying `dross/target:other` and not `dross/target:host`
                   (relies on t-6's replace semantics)
                 - a dismissed routed item produces no issue — proof the skip
                   removal was scoped to routed items only
                 - the stale "left to board-sync-truth" note in mirrorDeferredAdd's
                   doc comment is removed

Wave 3 (depends on wave 2)
  t-11 Resolve on close and verify the read-back
       files:    internal/forge/forge.go, internal/forge/youtrack.go,
                 internal/forge/jira.go, internal/cmd/issue.go,
                 internal/cmd/issue_close_truth_test.go,
                 internal/forge/youtrack_test.go, internal/forge/jira_test.go
       covers:   c-5
       depends:  t-8
       origin:   [verification] skeleton; Issue.Resolved + CloseIssue-must-not-lie
                 + lenient-path-preserved [risk]
       note:     verified in source — YouTrackClient.CloseIssue is `return nil`
       contract: - GetIssue on `"resolved":1700000000000` yields Resolved true;
                   `"resolved":null` yields false; a Jira statusCategory key "done"
                   yields Resolved true and State "closed"
                 - YouTrackClient.CloseIssue no longer returns a bare nil — either
                   it resolves for real or it errors naming the state write; a nil
                   return fails, because that silent nil IS the c-5 bug
                 - `phase-sync 01-x --status complete --close` makes the fake
                   observe a State write of the mapped value AND a follow-up
                   read-back GET, then prints the closed line  [see D4]
                 - state_map.complete = "": the command returns a NON-NIL error
                   naming the status and stdout contains no "closed" line; today's
                   warn-and-continue exits 0 and fails both halves
                 - the fake accepts the State write but reports `"resolved":null`:
                   the command errors. Without the read-back this test cannot fail
                 - the close-on-create edge (issue minted then closed in one run)
                   takes the same path — that branch is the one that regressed
                 - a Jira board with no done-category transition errors instead of
                   printing the closed line
                 - phase-sync WITHOUT --close on an unmapped status still warns and
                   exits 0 — the lenient path is preserved

  t-12 Link routed items to their target phase issue
       files:    internal/cmd/issue.go, internal/cmd/deferred_link_test.go
       covers:   c-7
       depends:  t-7, t-9, t-10
       origin:   [verification] skeleton; ErrNoLinkType arm [risk];
                 label-carries-destination framing [mvp]
       contract: - target phase already has an issue: backlog-sync records exactly
                   one link call naming both keys
                 - no target phase issue: ZERO link calls, exit 0, stderr names the
                   target slug, and the created issue still carries
                   `dross/target:<slug>`. Erroring fails the exit-0 assertion;
                   silence fails the stderr assertion
                 - sync (warn, no link) → create the phase issue → re-sync: exactly
                   one link call. This is c-7's "established on a later sync" clause
                   and it cannot pass unless the warn path leaves the item relinkable
                 - a GitHub board records zero link attempts, warns once, exits 0 —
                   the capability check must be an interface assertion, not a
                   provider-string switch
                 - ErrNoLinkType from the linker warns and continues rather than
                   aborting the whole backlog sync
                 - the link call 500s: backlog-sync still exits 0 with the issue
                   created and labelled
```

Coverage: c-1 → t-1, t-2, t-3 · c-2 → t-3, t-4 · c-3 → t-5, t-8 · c-4 → t-6, t-9 ·
c-5 → t-11 · c-6 → t-6, t-10 · c-7 → t-7, t-12. 7/7, no criterion-less task.

## Disagreements

**D1 — Where unknown-label filtering lives.**
`verification` puts it inside forge (one helper, called by three ListIssues impls), arguing `dross watch` and `dross status` reach `collectInbound` on the same path and a command-layer filter would leave them querying unknown labels. `mvp` puts it in the command layer behind a `LabelLister` type assertion — one drop-and-warn implementation instead of three, leaving the out-of-scope forge/gitlab backends untouched. `risk` splits it: `PruneUnknownLabels` in forge, called from `collectInbound` in cmd.
*Provisional default:* forge placement (t-3), with the shared helper exported so the cmd layer could reuse it.
*Why it matters:* placement decides whether `dross watch`/`dross status` inherit the fix for free or silently keep the old behaviour, and it decides whether the REST backends change semantics without a test proving they should.

**D2 — What a failed label-index read means.**
`verification` degrades: the label endpoint 500s, ListIssues still returns issues for the requested labels with a nil error. `risk` refuses: a 500 becomes an envelope error with zero ListIssues calls, arguing a degraded query is "the entire board lands in triage — c-2's silent lie wearing a different hat". `mvp` sits between: KnownLabels must propagate the 500 as an error rather than an empty list, but doesn't say what pull then does.
*Provisional default:* `risk` — surface it as an envelope error, no ListIssues call.
*Why it matters:* the two behaviours are indistinguishable to a passing test suite but opposite in production — one shows the user a network blip, the other shows them a triage queue full of unrelated issues that looks like a successful pull.

**D3 — When every requested label is unknown.**
`mvp` reads the locked `unknown_label` decision as "query with no label clause rather than erroring" — the whole board. `risk` reads it as zero issues plus stderr naming every dropped label. `verification` doesn't specify; its helper returning an empty `kept` set would fall through to an unfiltered query.
*Provisional default:* `risk` — return zero issues, name all dropped labels on stderr.
*Why it matters:* the locked text is "query only the labels the tracker knows, return matches for those" — with no known labels, "matches for those" is empty. `mvp`'s reading turns a wholly-typo'd filter into a full-board dump into triage, which is the louder failure but also the one that destroys the user's inbox.

**D4 — Read-back verification after `--close`, in production or only in tests.**
`verification` and `risk` both issue a real GET after the state write and fail the command unless `resolved` is set. `mvp` rejects it as speculative: "resolved when read back" is proven by the test asserting the stub's stored state, not by an extra request on every close.
*Provisional default:* keep the production read-back (t-11).
*Why it matters:* c-5's wording is "leaves the issue resolved on the tracker **when read back**". Without a real read-back, the `TestPhaseSyncCloseFailsWhenReadBackIsUnresolved` arm cannot exist at all — there is nothing to make it fail. The cost is one GET per phase ship, on a path that runs a handful of times a milestone.

**D5 — Is a board.json cache hit verified against the tracker?**
`risk` verifies every hit (a 404'd entry is re-resolved and rewritten). `verification` verifies on a weaker condition — when the key no longer resolves to an issue carrying the phase label. `mvp` explicitly rejects verification: c-4's real failure is a *lost* mapping, which the miss→query path already fixes; verifying every hit costs a GetIssue on every phase-sync for a fault nobody has hit.
*Provisional default:* verify (one GetIssue on the hit path), per `verification`'s condition.
*Why it matters:* `mvp` is right that it's an extra request, but board.json is git-tracked and the locked decision demotes it to a cache that "self-heals when stale or missing" — a cache that is trusted blindly on hit does not self-heal when stale, only when missing, so the locked decision is only half-implemented without it.

**D6 — Legacy issues that predate the `dross/phase:` label.**
`risk` keeps an exact-title fallback (`<id> — <title>` among marker-labelled issues) before creating, arguing every issue on the real board today predates the label, so a label-only resolver mints exactly the orphan c-4 exists to kill — once per live phase. `mvp` and `verification` are label-only: miss → create.
*Provisional default:* include the fallback (grafted into t-9).
*Why it matters:* this is the difference between the fix being clean on the author's live DRO board and the fix's first run duplicating every existing phase issue. It costs one extra request only on the miss path. It is also the single most likely place for a false adoption, so its contract must assert the marker label is verified on the matched issue.

**D7 — How YouTrack tag writes reach the wire.**
`mvp`: one `/api/commands` call carrying `add tag {x}` / `remove tag {y}` for the diff. `verification`: POST `/api/issues/{id}/tags` per name, creating the tag when absent, non-nil patch = full replace. `risk`: ensure each tag entity exists via `/api/issueTags`, then set the issue's tags to exactly the patch set.
*Provisional default:* `verification`'s per-issue tags endpoint, with `risk`'s explicit ensure-tag-exists step folded in.
*Why it matters:* all three agree on the *semantics* (replace, so removals remove), but the contracts assert different requests, so the choice must be made before the tests are written. The commands API is one call but expresses the diff as text; the tags endpoint is N calls but is idempotent per tag and easier to assert against a fake.

**D8 — Granularity: 12 tasks or 5.**
`mvp` fuses the forge label surface (queries + label index + tag writes) into one task and c-6+c-7 into another, arguing splitting produces tasks that both edit the same label code and a t-5b that is a 20-line addition to what t-5a just wrote. `risk` and `verification` split at the forge/cmd seam — capability in wave 1, policy in wave 2/3 — so a failing test says which half is wrong.
*Provisional default:* 12 tasks.
*Why it matters:* `mvp`'s t-1 spans 7 files and three independent failure modes, so a red test there doesn't localise and the commit can't be reverted narrowly. Against that, the split does produce two tasks that touch `youtrack.go`'s label code (t-1 and t-6) and two that touch `issue.go`'s backlog sync (t-10, t-12) — real serialisation cost, accepted deliberately.

**D9 — Surface naming (minor, but must be fixed before tests are written).**
Fields key: `risk` uses `board.fields.version`; `mvp` and `verification` use `board.fields.fix_versions`. Jira OR syntax: `risk`/`verification` emit `labels IN (...)`, `mvp` emits a parenthesised `(labels = "a" OR labels = "b")` group.
*Provisional default:* `fix_versions` (mirrors YouTrack's literal "Fix versions" default, 2-1) and `labels IN (...)` (2-1).
*Why it matters:* only that both appear verbatim in contract assertions, so a late change invalidates written tests. Semantically the JQL forms are equivalent; the config key is user-facing and effectively permanent once a project.toml carries it.

## Out-of-scope note carried forward

`risk` flags that the forge REST backends (forgejo/gitea/gitlab) keep today's label semantics — c-1 names only youtrack, jira and github, and widening would add guesses untestable against a real server. Recorded as a deliberate, known inconsistency, not a gap. Neither other draft mentions it.
