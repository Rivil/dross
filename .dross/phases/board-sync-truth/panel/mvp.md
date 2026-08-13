# MVP lens — board-sync-truth

Phase board-sync-truth — 5 tasks across 3 waves

## Wave 1

```
t-1  Fix the forge label surface (query + write)
     files:    internal/forge/forge.go
               internal/forge/youtrack.go
               internal/forge/jira.go
               internal/forge/github.go
               internal/forge/youtrack_test.go
               internal/forge/jira_test.go
               internal/forge/github_test.go
     covers:   c-1, c-4
     depends:  —
     what:     Label filters become OR on all three backends: YouTrack emits one
               comma-joined `tag:` clause, Jira wraps the label clauses in a
               parenthesised OR group inside the AND chain, GitHub issues one
               request per label and returns the union deduped by issue number.
               Add `KnownLabels() ([]string, error)` to each of the three (YouTrack
               GET /api/issueTags, Jira GET /rest/api/3/label, GitHub GET
               /repos/:o/:r/labels?per_page=100) plus a `LabelLister` interface in
               forge.go so the command layer can drop unknown labels without a
               provider switch. Also make YouTrack able to WRITE tags — it silently
               ignores IssueInput.Labels and IssuePatch.Labels today, so no dross
               marker ever reaches a YouTrack issue: add `SetTags(key string, names
               []string) error` (GetIssue for the current tags, then one
               /api/commands call carrying `add tag {x}` / `remove tag {y}` for the
               diff) and route CreateIssue, CreateBacklogItem and UpdateIssue
               through it.
     contract: - YouTrack ListIssues with Labels {"bug","enh"} sends
                 `query=project: DRO #Unresolved tag: bug, enh`; the space-joined
                 two-`tag:`-clause form (today's AND) fails the assertion.
               - Jira buildJQL with two labels produces
                 `project = "X" AND statusCategory != Done AND (labels = "bug" OR
                 labels = "enh") ORDER BY created DESC`; two AND-ed `labels =`
                 clauses fail.
               - GitHub ListIssues with two labels makes two GET /issues requests
                 and returns an issue carrying both labels exactly once.
               - KnownLabels on each backend returns the names its stub server
                 publishes, and propagates a 500 as an error rather than an empty
                 list (an empty list would silently drop every label downstream).
               - YouTrack CreateIssue with Labels {"dross","dross/phase:x"} issues a
                 /api/commands request whose query contains `add tag {dross}` and
                 `add tag {dross/phase:x}`; UpdateIssue with a Labels patch that
                 omits a currently-set tag emits `remove tag {...}` for it.

t-2  Add [board.fields] config, thread to forge
     files:    internal/project/project.go
               internal/cmd/project.go
               internal/cmd/issue.go
               internal/forge/forge.go
               internal/forge/youtrack.go
               internal/cmd/project_test.go
               internal/forge/youtrack_test.go
     covers:   c-3
     depends:  —
     what:     `Board.Fields map[string]string` (toml `fields`) alongside StateMap;
               `board.fields.<key>` becomes an addressable get/set/unset leaf in
               cmd/project.go exactly as `board.state_map.<key>` is; `Config.Fields`
               carries it into forge and boardConfig() populates it. YouTrack gains
               `field(key, fallback string) string` and uses it for the three
               tracker-native names it hardcodes today: `state` (default "State", in
               SetState), `type` (default "Type", in ensureEpic) and `fix_versions`
               (default "Fix versions", in CreateBacklogItem). Unknown keys are
               rejected by `project set` naming the accepted set.
     contract: - With no [board.fields] table, SetState still POSTs a customField
                 named "State" and CreateBacklogItem still posts "Fix versions" —
                 defaults are the literals in the code today.
               - With `[board.fields] state = "Status"`, the SetState POST body
                 carries customFields[0].name == "Status" and no "State" key.
               - `dross project set board.fields.type Kind` then
                 `dross project get board.fields.type` round-trips "Kind", and
                 `--unset` removes the entry leaving the other fields.entries intact.
               - `dross project set board.fields.bogus x` errors naming the accepted
                 keys (state | type | fix_versions).
```

## Wave 2 (depends on wave 1)

```
t-3  Envelope `issue pull` and drop unknown labels
     files:    internal/cmd/issue.go
               assets/prompts/status.md
               assets/prompts/inbox.md
               internal/cmd/issue_test.go
               internal/cmd/status_test.go
               internal/cmd/inbox_prompt_test.go
     covers:   c-1, c-2
     depends:  t-1
     what:     `--json` emits `{"issues":[...],"error":null}` (locked
               pull_failure_signal) — including the board-disabled path, which
               prints `[]` today. Any error from openBoard or collectInbound is
               caught, rendered into `error` and exit stays 0; without --json the
               same failure prints `board unreachable: <err>` on stderr and skips
               the listing instead of printing "no new issues on the board". Before
               listing, when the client implements LabelLister, `--labels` is
               intersected with KnownLabels(); dropped names are printed on stderr
               (`ignoring unknown label(s): x, y`) and the surviving ones are
               queried — an all-unknown filter queries with no label clause rather
               than erroring (locked unknown_label). status.md step 4 reads
               `.issues`/`.error` and prints the board half as
               `board unreachable` instead of contributing 0; inbox.md's
               `dross issue pull --mark --json` consumption moves to `.issues`.
     contract: - `issue pull --json` against a stub that 500s prints
                 `{"issues":[],"error":"..."}` on stdout and exits 0; an assertion
                 on exit code alone, or on a bare `[]`, fails.
               - `issue pull --json` with board sync disabled prints
                 `{"issues":[],"error":null}` — the pre-envelope bare `[]` fails.
               - `issue pull --labels bug,nope` against a stub whose label list
                 holds only "bug" queries with Labels == ["bug"] and writes
                 "nope" to stderr; the request must not carry "nope".
               - status.md contains an unreachable/error branch for the board
                 source and no longer implies `[]` is the only shape.

t-4  Resolve the phase issue from the tracker
     files:    internal/cmd/issue.go
               internal/forge/youtrack.go
               internal/cmd/issue_test.go
               internal/forge/youtrack_test.go
     covers:   c-4, c-5
     depends:  t-1
     what:     Phase issues gain a `dross/phase:<id>` label beside `dross`.
               resolvePhaseIssue(ctx, phaseID): board.json cache first; on a miss
               query ListIssues{State:"all", Labels:["dross/phase:<id>"]} (a single
               label, so the new OR semantics can't widen it), keep the first hit
               that also carries `dross`, write it back to board.json, and only
               create when the query comes back empty — board.json demoted to a
               self-healing cache (locked mapping_authority). Separately, the close
               path stops relying on `IssuePatch.State` (YouTrack drops it): both
               the created and the already-linked branch fall through to one
               closePhaseIssue call. For YouTrack that resolves the State custom
               field through the state map and returns an error — not the current
               stderr warning — when the status maps to nothing, so `--close`
               cannot report success on an unresolved issue.
     contract: - phase-sync run twice with board.json deleted between runs issues
                 exactly ONE CreateIssue: the second run's ListIssues stub returns
                 the first run's issue and the create endpoint is never hit again.
               - The resolve query carries exactly one label, `dross/phase:<id>`;
                 a query carrying two labels fails (it would OR-match every dross
                 issue after t-1).
               - A ListIssues hit whose labels lack `dross` is NOT adopted — the
                 run creates a fresh issue instead of hijacking a foreign one.
               - `phase-sync x --status complete --close` on YouTrack POSTs the
                 State custom field with the mapped resolved value, and a GetIssue
                 against the stub afterwards reports that value.
               - `phase-sync x --status complete --close` with
                 `[board].state_map.complete = ""` exits non-zero and prints no
                 `-> board ... (closed)` success line.
```

## Wave 3 (depends on wave 2)

```
t-5  Sync routed deferred items and link them
     files:    internal/cmd/issue.go
               internal/forge/forge.go
               internal/cmd/issue_test.go
               internal/cmd/deferred_board_test.go
     covers:   c-6, c-7
     depends:  t-4
     what:     syncBacklog stops skipping `d.Target != ""`: routed, non-dismissed
               items become backlog items too, carrying `dross`, and
               `dross/target:<slug>` (locked routed_repr). pushBacklogItems sends
               Labels on the update patch as well as on create, so a re-route
               replaces the old target label and a text edit updates the same
               issue. After each routed item is pushed, resolve the target phase's
               issue with t-4's resolvePhaseIssue and, when the client implements
               `IssueLinker` (a 3-line interface in forge.go that YouTrack's
               existing LinkSubtask already satisfies), link the item's issue to
               it; a provider with no linker, or a target phase with no issue yet,
               prints one warning and continues — the label alone carries the
               destination and a later sync establishes the link.
     contract: - backlog-sync on a milestone holding one routed item creates an
                 issue for it; today's skip means zero creates, which fails.
               - Re-running after `dross deferred route` changes the target
                 UPDATES the same issue key with labels containing
                 `dross/target:<new>` and not `dross/target:<old>`.
               - Editing a routed item's text and re-syncing sends an UpdateIssue
                 for its recorded key, not a CreateIssue.
               - When the target phase has an issue, the run makes a link call
                 naming the phase issue as parent and the item's issue as child.
               - When the target phase has NO issue, the run exits 0, writes a
                 warning naming the target slug to stderr, and the item's labels
                 still contain `dross/target:<slug>`; a later run, once the phase
                 issue exists, makes the link call.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (OR labels, unknown label tolerated) | t-1, t-3 |
| c-2 (fetch failure ≠ empty board) | t-3 |
| c-3 (YouTrack field names from config) | t-2 |
| c-4 (exactly one issue per phase) | t-1, t-4 |
| c-5 (--close leaves it resolved / reports failure) | t-4 |
| c-6 (routed deferred items mirrored + updated) | t-5 |
| c-7 (routed item linked to target phase issue) | t-5 |

7/7 criteria covered.

## Judgment calls

- **YouTrack tag writes folded into t-1, not given their own task.** YouTrack's
  CreateIssue/UpdateIssue drop Labels on the floor today, so no marker label has
  ever reached a YouTrack issue and c-4/c-6/c-7 are all unbuildable without it. I
  merged it into the forge label task rather than minting a sixth task: it is the
  same file and the same concept (the label surface), and splitting query-labels
  from write-labels would produce two tasks that both edit youtrack.go's label code.
- **Phase resolution keys on a single `dross/phase:<id>` label, not marker+id as a
  two-label filter.** t-1 makes multi-label filters OR, so a `["dross","dross/phase:x"]`
  filter would match every dross issue on the board. One unique label plus a
  client-side marker check gets the locked mapping_authority semantics with no
  provider-specific AND syntax.
- **board.json cache is consulted before the query, not verified against it.** The
  locked decision demotes it to a self-healing cache; c-4's actual failure is a
  LOST mapping, which the miss→query path fixes. Verifying every cache hit costs a
  GetIssue on every phase-sync for a fault nobody has hit. Rejected as speculative.
- **No read-back verification call in production for c-5.** "Resolved when read
  back" is proven by the test asserting the stub's stored state, not by dross
  issuing an extra GET after every close. A verify-after-write is one more request
  on the hot path guarding against a tracker lying about a 2xx.
- **GitHub OR = one request per label, not the search API.** `/search/issues`
  supports comma-OR in a single call, but it is a second response shape, a second
  rate-limit bucket and a second pagination model. N requests over the endpoint
  the backend already speaks is smaller and provably OR. Chosen over the search API.
- **`board.fields.<key>` get/set kept in t-2 despite the criterion only requiring
  a READ.** The locked field_config_shape decision justifies the table shape by
  "same `dross project set` path as the state map", so omitting the leaf would
  leave the decision half-implemented. It costs ~15 lines mirroring
  stateMapKey/board.state_map. This is why t-2 lands on 5 source files; splitting
  it would leave a task that is only a struct field.
- **The unknown-label filter lives in the command layer, gated on a
  `LabelLister` type assertion, not in every backend.** One drop-and-warn
  implementation instead of three, and the forge/gitlab backends (out of c-1's
  scope) keep today's behaviour untouched with no code written for them.
- **c-6 and c-7 are one task.** Both edit syncBacklog/pushBacklogItems, and c-7's
  fallback ("keeps its target label, warns, continues") IS c-6's output. Splitting
  would make t-5b a 20-line addition to code t-5a just wrote.
- **inbox.md is edited alongside status.md in t-3** even though only status.md is
  named in c-2: inbox.md consumes `dross issue pull --mark --json` and would break
  on the envelope. Not scope creep — it is the same change's blast radius.
