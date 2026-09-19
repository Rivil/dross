# MVP lens — secret-detection

Bias applied: smallest task set that satisfies every criterion. One detector
package, one file-walk gate shared by validate and ship, one publish gate per
transport chokepoint, one enumeration test. Nothing speculative.

Key structural finding that shapes the whole plan: **ship's Go pre-flight does
not run `dross validate` today** (`internal/cmd/ship.go` gates on verify.toml,
branch, remote, then `autoCommitDrossDirt` — no validate call; the prompt runs
`dross doctor`, not validate). c-3 therefore needs a real wiring change, not
just a new validate check — and it must run *unconditionally*, not inside
`autoCommitDrossDirt`, because that helper returns early on a clean tree and a
token already committed on the phase branch would ship.

Second finding: the board client has **no single method-level chokepoint**
(`forge.BoardClient` has four implementations, and `internal/cmd/issue.go`
type-switches on the concrete `*forge.YouTrackClient` / `*forge.JiraClient`,
so a gating wrapper breaks eight assertion sites). But every backend funnels
outbound bodies through exactly one `doRaw(method, endpoint string, body, out
any)` — `forge.go:837`, `youtrack.go:927`, `jira.go:689`, `github.go:368` —
with the body still a Go value (pre-JSON). That is the gate site: four
one-line edits cover CreateIssue, UpdateIssue, EnsureMilestone(Entity),
labels, comments, and every future method. Ship's egress is scattered
(`jsonPost`, bitbucket/gitlab own `http.NewRequest`, four `ghCommand` sites),
but cmd reaches it only via `ship.OpenPR` / `ship.PostComment`, so those two
dispatchers are ship's gate.

```
Phase secret-detection — 5 tasks across 3 waves

Wave 1
  t-1  Add secretscan detector package
       files:    internal/secretscan/secretscan.go
                 internal/secretscan/secretscan_test.go
       covers:   c-1, c-5
       contract: TestHitCorpusEveryShapeCaught — a table of one line per rule
                 (ghp_, github_pat_, glpat-, ATATT, AKIA, xoxb-, sk-, a PEM
                 "-----BEGIN RSA PRIVATE KEY-----" block, "Authorization: Basic
                 <b64>", "Authorization: Bearer <tok>", `password = "<20 chars>"`,
                 `token: '<24 chars>'`, `api_key=<32 chars>`), every value built
                 by string concatenation at runtime so the corpus never sits on
                 disk as a matchable line; dropping any rule leaves its row with
                 zero hits and the test names the missing rule.
                 TestBenignCorpusZeroHits — `"key": "b91bfa24fdf586c0"`,
                 `id = 30dcd7db2eecf398`, `token = "08ec1d7666c48b32"` (16-hex
                 under a key-context word: the locked identity-id carve-out), a
                 40-hex commit SHA, a UUID, a 200-char base64 fixture on an
                 unlabelled line, `PRIVATE-TOKEN: <token>`, `token = %q`,
                 `password: ${DB_PASSWORD}`; any hit fails naming the line and
                 rule. Widening a rule to bare entropy fails the base64 row
                 (locked entropy_rules).
                 TestAllowMarkerSilencesThatLineOnly — two hit lines, the first
                 suffixed `# dross:allow-secret`; exactly one hit, on line 2.
                 TestHitReportNeverEchoesValue — for every corpus hit,
                 Hit.String() does not contain the matched value nor any
                 8-char window of it; it does contain the rule name, `<path>:<line>`,
                 `len=<n>` and a ≤4-char prefix. A report that prints the value
                 fails on the first row.

Wave 2 (depends t-1)
  t-2  Wire artifact scan into validate and ship
       files:    internal/secretscan/tree.go
                 internal/cmd/validate.go
                 internal/cmd/ship.go
                 internal/cmd/secretscan_gate_test.go
       covers:   c-3, c-6
       contract: TestValidateFailsOnTokenInAnyDrossFile — Init() fixture plus
                 an arbitrary NEW path `.dross/phases/p/panel/notes.md` (not any
                 name validate hand-lists) holding a concat-built AKIA key;
                 validate returns "1 problem(s)" whose text has the rule name
                 and `.dross/phases/p/panel/notes.md:3` and NOT the key.
                 TestShipRefusesTokenBeforePushEvenOnCleanTree — shipFixture
                 with the note COMMITTED on phase/<id> (tree clean, so
                 autoCommitDrossDirt would no-op); ship returns the hit before
                 any push: the mock provider records zero requests and the
                 origin bare repo has no phase ref. Moving the scan inside
                 autoCommitDrossDirt fails this test.
                 TestTreeWalkSkipsOnlyScannerRunDirs — ScanTree over a temp
                 .dross with a token in `security/<run>/report.json` and one in
                 `handoff.md`: security/ is skipped (declared, see t-5),
                 handoff.md is reported — the walk is filesystem-enumerated,
                 not gitignore- or list-driven.
                 TestDrossOwnTreeIsClean (c-6) — ScanTree over `git ls-files`
                 of the repo root plus the whole `.dross/`: zero hits, printed
                 with rule + file:line on failure. This is the test that forces
                 t-1's corpus to be concat-built and the key-context rule to
                 carve out placeholders (`<token>`, `%q`, `$VAR`) — the tree
                 holds `PRIVATE-TOKEN: <token>` and `token = %q` today.

  t-3  Gate ship publish dispatchers
       files:    internal/ship/open.go
                 internal/ship/comment.go
                 internal/ship/secretgate_test.go
       covers:   c-2
       contract: TestOpenPRRefusesBodyWithSecret — for each of the four
                 providers, OpenPR with a Body carrying a concat-built ghp_ token
                 returns the hit (rule name, `pr body:<line>`, fingerprint, no
                 value) and the httptest server / ghCommand stub records zero
                 calls. TestPostCommentRefusesBodyWithSecret — same for
                 PostComment. TestOpenPRTitleIsScannedToo — a token in Title
                 with a clean Body is refused. Removing the gate from either
                 dispatcher makes the stub record one call.

  t-4  Gate forge outbound bodies at doRaw
       files:    internal/secretscan/value.go
                 internal/forge/forge.go
                 internal/forge/youtrack.go
                 internal/forge/jira.go
                 internal/forge/github.go
                 internal/forge/secretgate_test.go
       covers:   c-2
       contract: TestScanValueWalksNestedBodies — ScanValue on
                 map[string]any{"fields": map[string]any{"description":
                 map[string]any{"content": []any{map[string]any{"text":
                 "<line1>\n<AKIA…>"}}}}} (the Jira ADF shape) reports one hit
                 located at `body.fields.description.content[0].text:2`; a struct
                 with a json-tagged string field and a []string are walked too.
                 Scanning post-marshal JSON instead fails the key-context row
                 (the `\"` escaping breaks the value capture — the reason the
                 gate sits before encoding).
                 TestCreateIssueRefusesBeforeNetwork_{Forge,YouTrack,Jira,GitHub}
                 — each backend via its httptest server: CreateIssue with a
                 token in Body returns the hit and the server's request counter
                 is 0; UpdateIssue with Body patch likewise; EnsureMilestone
                 with a token in description likewise (YouTrack). Removing the
                 gate from any one backend's doRaw fails that backend's test.
                 TestDoRawGetIsUntouched — a nil body passes through (GET paths
                 unchanged).

Wave 3 (depends t-2, t-3, t-4)
  t-5  Registry-plus-residual enumeration of gates
       files:    internal/secretscan/registry.go
                 internal/cmd/secretscan_enum_test.go
                 internal/cmd/testdata/secretscan/ungated_egress.go.txt
       covers:   c-4
       contract: Registry (data, two kinds, one Why per non-gated entry):
                 Publish gates — ship.OpenPR, ship.PostComment, forge
                 (*Client|*YouTrackClient|*JiraClient|*GitHubClient).doRaw;
                 ship ReadOnly — OpenPRsTargeting, FindOpenPRByHead,
                 GetPRStatus {Why}; Artifact skip dirs — security/, quality/,
                 techdebt/ {Why: scanner output that quotes secrets by nature}.
                 TestEveryForgeEgressIsAGatedPrimitive — AST over ../forge:
                 every FuncDecl containing an `http.NewRequest` call must be a
                 registered doRaw AND contain a `secretscan.` call before it;
                 a registered entry with no such decl fails as stale.
                 TestEveryShipEntrypointIsGatedOrDeclaredReadOnly — AST over
                 ../ship: every exported func with an OpenOpts/CommentOpts
                 param is either a registered gate (calls secretscan on the
                 param first) or a declared ReadOnly whose body never selects
                 `.Body`/`.Title` on that param; undeclared → fail, declared but
                 selecting `.Body` → fail, declared but missing → stale.
                 TestEveryDrossArtifactIsWalkedOrDeclared — union of (a) every
                 top-level entry in the dross repo's own `.dross/` and (b) every
                 `*File = "…"` const under internal/ (13 today: board.json,
                 changes.json, tests.json, verify.toml, …): each is reached by
                 t-2's walk (not in the skip set) or is a declared skip; a
                 declared skip with no entry on disk is stale.
                 TestEnumTripsOnUngatedFixture — the .go.txt fixture holds one
                 func calling http.NewRequest with no secretscan call and one
                 exported func taking OpenOpts that reads opts.Body; the walker
                 reports both by name. A walker that resolves nothing passes
                 the live tree and fails here.
                 Vacuity floors: forge resolves exactly 4 egress funcs, ship
                 resolves ≥5 entrypoints.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-3, t-4 |
| c-3 | t-2 |
| c-4 | t-5 |
| c-5 | t-1 (Hit.String fingerprint; every gate prints Hit.String) |
| c-6 | t-2 (TestDrossOwnTreeIsClean) |

## Judgment calls

- **Forge gate at `doRaw`, not at 14 method sites.** Rejected method-level
  gating (CreateIssue/UpdateIssue/EnsureMilestone × 4 backends) and a
  BoardClient wrapper (breaks eight concrete-type assertions in
  `internal/cmd/issue.go`). Four `doRaw` edits cover every method now and
  later; the residual predicate ("a func that calls http.NewRequest") is crisp.
- **Scan pre-marshal Go values, not JSON bytes.** encoding/json escapes `"`
  into `\"`, which breaks the key-context value capture (`password = \"…\"`
  captures a lone `\`). A reflective walker over map/slice/struct string leaves
  keeps the rules identical across artifacts and bodies and keeps line numbers.
- **Ship gate at the two dispatchers, not at egress.** Ship's egress is
  scattered across jsonPost, two provider-private `http.NewRequest`s and four
  `ghCommand` sites (three read-only). cmd only reaches ship through OpenPR /
  PostComment; gating there is 2 sites and the read-only funcs are declared in
  the registry with a "never selects .Body" assertion instead of a gate.
- **Artifact walk is a filesystem walk of `.dross/`, not `git ls-files`.**
  validate's tests run on `Init()` temp dirs with no git; a git-enumerated walk
  needs a fallback path and a second set of tests. Walking everything except
  the three scanner-run dirs is a strict superset of "tracked" (it also catches
  handoff.md), needs no git, and the skip set is the registry c-4 wants.
- **Ship runs the scan unconditionally in pre-flight, not inside
  `autoCommitDrossDirt`.** One-site placement inside the autocommit helper was
  tempting (covers phase complete/create too) but it no-ops on a clean tree, so
  a token already committed on the phase branch would ship. Two call sites of
  one helper (validate, ship) is the minimum that holds c-3 as written.
- **c-6 is a test, not a command.** "Running the detector against dross's own
  tree" is `TestDrossOwnTreeIsClean` over `git ls-files` + `.dross/`; it is
  what forces the hit corpus to be concat-built and the placeholder carve-outs
  (`<token>`, `%q`, `${VAR}`) into the key-context rule — the tree carries all
  three shapes today. No `dross secretscan` subcommand: nothing in the criteria
  needs one.
- **No separate rules file, no per-rule struct package.** Rules are a table in
  `secretscan.go`; the corpus tests are the spec. Splitting rules/hit/walk into
  files-per-concern adds nothing a criterion asks for.
- **t-4 touches 6 files.** Four of them are identical one-line insertions at
  the top of `doRaw`; splitting per backend would make four sub-10-minute tasks.
