# secret-detection — verification-lens draft

Lens: every criterion's ideal test contract was written first; each task is
the smallest change that makes its contracts satisfiable. Where the contract
forced a design choice (where the gate sits, what the walk enumerates), the
choice that yields the sharper test won — see "Judgment calls".

Concrete facts the contracts rest on (read from the tree, 2026-09-19):

- No detector exists. `internal/redact` scrubs a *known* token out of outbound
  text; it cannot recognise an unknown one. New package: `internal/secretscan`.
- Publish transports: ship has 8 (`openGitHubPR`, `openForgejoPR`,
  `openGitLabPR`, `openBitbucketPR`, `postGitHubComment`, `postForgejoComment`,
  `postGitLabComment`, `postBitbucketComment`); forge has 12 (`CreateIssue`,
  `UpdateIssue`, `EnsureMilestone` on `*Client`, `*GitHubClient`, `*JiraClient`,
  `*YouTrackClient`). Every one holds its own transport primitive (`c.do`,
  `jsonPost`, `gitlabReq`, `bbRequest`, `ghCommand`, `http.NewRequest`).
  `internal/cmd` type-asserts the board client to concrete types in 11 places,
  so a wrapping decorator over `forge.BoardClient` is not an option — the gate
  goes inside each transport function.
- `dross ship` does NOT call validate today (`internal/cmd/ship.go:151` "3)
  Pre-flight gates" — remote, verify verdict, branch). `autoCommitDrossDirt`
  (`cleantree.go:35`) runs `git add .dross`, which stages UNTRACKED files too.
- 961 tracked files under `.dross/` (378 toml, 348 md, 235 json). Agent-authored
  `.md` (panel/*.md, REVIEW.md, pr-body.md, proof.md) have no Go writer.
  `.dross/security/**` (gitleaks.json — carries matched secrets) is gitignored.
- Shapes already in the tree that a naive ruleset would flag: AWS's documented
  example key `AKIAIOSFODNN7EXAMPLE` (10 lines, 4 files incl. 3 tracked .dross
  artifacts); `token = "08ec1d7666c48b32"` (16-hex identity id, already the
  gitleaks carve-out in `internal/security/gitleaks.go:23`); two test fixture
  constants `"s3cr3t-fixture-token-do-not-leak"` / `"s3cr3t-sentinel-do-not-leak"`
  (`internal/forge/hostile_config_test.go:60`, `hostallow_test.go:25`).
- Walker helpers to reuse: `goFilesIn`, `funcDecls`, `readCmdSource`
  (`internal/cmd/toolfence_composer_test.go:422-463`); fixture convention
  `internal/cmd/testdata/<scan>/<name>.go.txt`; ship harness `shipFixture` +
  `shipMockFlow` + `mustWrite` (`ship_test.go`, `cmd_test.go`).

```
Phase secret-detection — 7 tasks across 3 waves

Wave 1
  t-1  Ruleset, scanner core, fingerprinted refusal
       files:    internal/secretscan/rules.go
                 internal/secretscan/scan.go
                 internal/secretscan/scan_test.go
                 internal/secretscan/corpus_test.go
       covers:   c-1, c-5
       contract: TestEveryRuleHasAHitCase — the set of Rules() names equals the
                 corpus map's key set, and each key's CONSTRUCTED hit line
                 ("ghp_"+repeat("a",36), "AKIA"+16 upper/digits, "-----BEGIN RSA
                 PRIVATE KEY-----", "Authorization: Bearer "+40 base62,
                 "password = "+32 base62, ...) yields exactly one Finding whose
                 Rule is that key. A rule added without a corpus entry fails; a
                 regex edit that stops matching its own shape fails.
                 TestBenignCorpusIsSilent — 16-hex identity id in id/key AND
                 token context, 40-hex commit SHA, UUID, a 200-char base64 line
                 lifted from tests.json, `Authorization: Bearer <token>`
                 placeholder, `Token: "--end-of-options"`, `AKIAIOSFODNN7EXAMPLE`:
                 zero findings. Non-vacuity twin: each benign line's "one-char
                 mutation" (17-hex, AKIA+16 not ending EXAMPLE) DOES fire, so
                 the carve-out is proven to be doing the work.
                 TestKeyContextValueRules — `token = <16-hex>` silent;
                 `token = <17-hex>` fires as key-context; `absentToken =
                 "DROSS_TEST_ABSENT_TOKEN"` silent (key needs a word boundary
                 AND value entropy < floor); `secret = <32-hex>` fires;
                 `id = <40 random base62>` silent (locks entropy_rules: no bare
                 entropy rule, key set is password|passwd|token|secret|api_key|
                 apikey|authorization only).
                 TestPEMBlockReportedOnceAtBeginLine — a 5-line PEM block gives
                 one Finding at the BEGIN line.
                 TestAllowMarkerSilencesOnlyItsLine — `dross:allow-secret` on
                 the hit line → 0; on the line above → 1; two hits on one
                 marked line → 0 (line-only semantics, locked allowlist_route).
                 TestFingerprintNeverEchoesTheValue — for every corpus hit,
                 Finding.String() and Refusal(surface, findings).Error() contain
                 the rule name, `<file>:<line>`, `len=N`, a prefix of at most
                 4 chars, and NO 8-char window of the matched value.

  t-2  Declare publish sinks + .dross artifact registry
       files:    internal/secretscan/sinks.go
                 internal/secretscan/sinks_test.go
       covers:   c-4 (registry half)
       contract: TestRegistryValidateRejectsMalformed — synthetic entries fed
                 to Validate(): a PublishSink with empty Func, an Artifact with
                 no disposition, one with two dispositions (Scanned+Untracked),
                 an Untracked with empty Why, a duplicate Path — each produces
                 its own error line; the real registry produces none
                 (TestRealRegistryIsWellFormed).
                 TestPublishSinkFuncsAreQualified — every Func is
                 "pkg.Recv.Method" or "pkg.func" over exactly {ship, forge};
                 count is 20 (8 ship + 12 forge) so a silently shrunk list
                 fails here before the AST arm in t-6 ever runs.

Wave 2 (depends t-1)
  t-3  Gate ship PR-body and comment transports
       files:    internal/ship/open.go
                 internal/ship/forgejo.go
                 internal/ship/gitlab.go
                 internal/ship/bitbucket.go
                 internal/ship/comment.go
                 internal/ship/secretgate_test.go
       covers:   c-2 (PR body, PR comment)
       contract: TestOpenPRRefusesHitBeforeTransport/{github,forgejo,gitlab,
                 bitbucket} — body carries a constructed ghp_ hit; github: the
                 ghCommand stub's invocation counter stays 0; the other three:
                 an httptest server's request counter stays 0; the error names
                 `github-pat` and `PR body:<line>` and contains no 8-char window
                 of the token. Same table for PostComment ×4 with surface
                 `PR comment`. A Title hit is refused on github (cheapest
                 backend) so the guard is proven to cover both fields.
                 TestOpenPRBenignBodyStillPosts — a body holding a 16-hex id, a
                 40-hex SHA and a UUID reaches the transport (counter == 1):
                 the gate cannot break ordinary shipping.

  t-4  Gate forge issue and milestone publishers
       files:    internal/forge/forge.go
                 internal/forge/github.go
                 internal/forge/jira.go
                 internal/forge/youtrack.go
                 internal/forge/secretgate_test.go
       covers:   c-2 (board issue body)
       contract: TestBoardPublishRefusesHitBeforeTransport — table over
                 {Client, GitHubClient, JiraClient, YouTrackClient} ×
                 {CreateIssue.Body, UpdateIssue.Body, EnsureMilestone.desc}:
                 httptest request counter == 0, error names the rule and
                 `issue body:<line>` / `milestone description:<line>`, no value
                 window. UpdateIssue with Body=nil and a Labels-only patch is
                 NOT refused (counter == 1) — the guard reads only what is
                 being sent.
                 TestBoardPublishBenignBodyPosts — renderPhaseBody-shaped text
                 with identity ids posts (counter == 1) on every backend.

  t-5  Artifact walk in validate and ship pre-flight
       files:    internal/secretscan/tree.go
                 internal/cmd/secretgate.go
                 internal/cmd/validate.go
                 internal/cmd/ship.go
                 internal/cmd/secretgate_test.go
       covers:   c-3, c-5
       contract: TestValidateFailsOnATrackedNoteCarryingAToken — temp repo,
                 `.dross/phases/x/notes.md` line 3 = constructed ghp_ hit,
                 committed; `dross validate` returns "1 problem(s) found" and
                 stdout carries `✗ .dross/phases/x/notes.md:3: github-pat
                 (len=40, prefix=ghp_)` and no window of the value.
                 TestValidateScansUntrackedUnignoredDrossFiles — same file,
                 never `git add`ed → still fails (it is what `git add .dross`
                 in autoCommitDrossDirt would stage next).
                 TestValidateSkipsGitignoredDrossFiles — a hit in
                 `.dross/security/r/gitleaks.json` with `.dross/security/`
                 ignored → validate green (gitleaks output legitimately holds
                 matched secrets; scanning it would make validate permanently
                 red on any repo that ran dross secure).
                 TestValidateAllowMarkerSilences — marker on the line → green.
                 TestShipRefusesBeforeAutoCommittingAHit — shipFixture +
                 shipMockFlow, hit written (uncommitted) to
                 `.dross/phases/x/notes.md`; Ship() errors naming the rule and
                 path; `git log` has NO "chore(dross): auto-commit bookkeeping"
                 entry; the mock remote has no `phase/x` ref; the error carries
                 no value window. Placement: the scan runs after the verdict
                 gate and before repointDoomedRedProofs/autoCommitDrossDirt.
                 TestShipAndValidateShareOneScanner — AST over validate.go and
                 ship.go: both RunE bodies call `scanDrossArtifacts`; a second
                 inline walk in either file fails (one ruleset, one candidate
                 set, no drift).

Wave 3 (depends t-2, t-3, t-4, t-5)
  t-6  Registry-plus-residual enumeration tests
       files:    internal/cmd/secretfence_enum_test.go
                 internal/cmd/testdata/secretfence/leaky_publisher.go.txt
                 internal/cmd/testdata/secretfence/leaky_writer.go.txt
       covers:   c-4
       contract: TestEveryPublishTransportIsGuarded (residual arm) — walk
                 ../ship and ../forge non-test files; a FuncDecl is a publish
                 transport when it has a parameter typed OpenOpts/CommentOpts/
                 IssueInput/IssuePatch or is named EnsureMilestone, AND its
                 body calls a transport primitive (do, doRaw, jsonPost,
                 gitlabReq, bbRequest, ghCommand, http.NewRequest). Each must
                 (a) appear in secretscan.PublishSinks() and (b) contain a
                 `secretscan.Guard(` call whose token.Pos precedes the first
                 primitive call's Pos. Vacuity floor: ≥ 8 ship sites, ≥ 12
                 forge sites, and GetIssue/ListIssues (no body param) resolve
                 as NON-sites.
                 TestNoStalePublishSink — every registry Func resolves to a
                 live FuncDecl; a renamed backend method fails here.
                 TestPublishScanTripsOnTheLeakyFixture — fixture functions:
                 guardAfterDo (trips: order), noGuard (trips: missing),
                 guardBeforeDo (clean), getShaped (not a site). All four
                 answers asserted, so the walker is calibrated on a
                 written-down truth rather than on the post-fix tree.
                 TestEveryDrossArtifactWriterIsDeclared (residual arm,
                 writers) — walk internal/ non-test files for string literals
                 ending .toml/.json/.jsonl/.md that are a `File`-style const or
                 a filepath.Join / pathfence.Contain argument; each basename
                 must match an Artifacts() entry (Scanned, Untracked or
                 OutOfScope). Vacuity floor ≥ 15 distinct literals; fixture
                 with an undeclared `"ledger.json"` join trips.
                 TestEveryScannedArtifactIsReached (live proof) — temp git
                 repo; for each Scanned entry, materialise one concrete
                 instance of its Path glob (phases/x/spec.toml, phases/x/
                 panel/risk.md, board.json, ...) holding a constructed hit,
                 `git add`; scanDrossArtifacts must report ≥ 1 finding AT that
                 path. For each Untracked entry, `git check-ignore` under
                 ensureDrossGitignore's seeded lines (or dross's own
                 .gitignore for the security/quality/techdebt dirs) says
                 ignored — else the registry misdescribes a file that would be
                 scanned by accident. OutOfScope entries (ARCHITECTURE.md,
                 ~/.claude/dross/telemetry.jsonl, defaults.toml) must not sit
                 under `.dross/`.
                 TestNoStaleArtifactDeclaration — every Go-written entry's
                 basename appears as a literal in internal/; agent-authored
                 entries (Writers: ["agent"], the CriterionResult.Notes
                 precedent) are exempt from resolution.

Wave 3 (depends t-1, t-5)
  t-7  Self-scan of the dross tree, fixture hygiene
       files:    internal/cmd/secretscan_selfscan_test.go
                 internal/forge/hostile_config_test.go
                 internal/forge/hostallow_test.go
       covers:   c-6
       contract: TestDrossTreeHasNoSecretFindings — `git ls-files` at the repo
                 root (source AND .dross, ≥ 1000 paths, ≥ 900 under .dross/ as
                 the vacuity floor), routed through pathfence.Contain and
                 secretscan.ScanFiles: zero findings. On failure the test
                 prints Finding.String() only — never the line.
                 TestSelfScanIsNotVacuous — the same list plus one temp file
                 holding a constructed hit yields exactly one finding, at that
                 file, so a walker that silently read nothing cannot pass the
                 test above.
                 Fixture hygiene: the two `s3cr3t-…-do-not-leak` constants sit
                 in key context with entropy above the floor and get a
                 `// dross:allow-secret` marker on their line; the AKIA
                 example key and the 16-hex `token =` line are rule carve-outs
                 (t-1) and need no edit. If the self-scan surfaces anything
                 else, the fix is a marker on that line or a carve-out with a
                 benign-corpus entry — never a rule deletion.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-3 (PR body, PR comment), t-4 (board issue body, milestone description) |
| c-3 | t-5 |
| c-4 | t-2 (registry), t-6 (residual + stale + live-reach arms) |
| c-5 | t-1 (renderer + never-echoes contract), t-5 (validate/ship problem lines go through the same renderer) |
| c-6 | t-7 |

Locked decisions, where each lands: hit_disposition → t-1 `Refusal` returns
an error, nothing rewrites text (t-3/t-4/t-5 all return it); pattern_source →
t-1 compiles regexes into the binary, no PATH lookup anywhere; entropy_rules →
t-1 TestKeyContextValueRules pins the key set and the no-bare-entropy side;
allowlist_route → t-1 marker test is line-only, identity-id is a fixed
carve-out, no project.toml key touched; scan_scope → t-5 walks only `.dross/`
and t-3/t-4 gate only bodies; t-7's whole-tree scan is a TEST of c-6, not a
gate.

## Judgment calls

- **Gate inside each transport function (20 sites), not at the two ship
  dispatchers or a BoardClient decorator.** Rejected decorator: cmd asserts
  `ctx.client.(*forge.YouTrackClient)` etc. in 11 places, a wrapper breaks
  them all. Rejected dispatcher-only for ship: the residual test would then
  need a two-hop "only reachable via a guarded caller" proof; a uniform
  "guard precedes primitive in the function that holds the primitive" is one
  positional check and calibrates on a 4-case fixture. Cost: t-3 touches 5
  source files with one line each — accepted, it is one layer.
- **Ship pre-flight calls the shared `scanDrossArtifacts`, not the whole
  `dross validate`.** c-3 says ship "runs it"; ship does not run validate
  today, and pulling all of validate into ship would newly fail ships on
  schema warnings — a scope change. One function, two callers, and
  TestShipAndValidateShareOneScanner pins that they cannot diverge.
- **Validate's candidate set is tracked ∪ untracked-unignored under `.dross/`,
  not tracked-only.** c-3 says "tracked", but `autoCommitDrossDirt` runs
  `git add .dross`, so an untracked note is one ship away from committed. The
  gitignored set is skipped on purpose and tested (gitleaks.json holds real
  matched secrets by design).
- **Fixed carve-outs: exactly-16-hex value in key context, and AWS's published
  example key `AKIAIOSFODNN7EXAMPLE`.** The first is the locked identity-id
  carve-out (same shape gitleaks.go already allowlists). The second is a
  documented placeholder gitleaks also allowlists; it already sits in 3
  tracked .dross artifacts, and the alternative (marking 10 historical panel
  lines) edits artifacts to satisfy a scanner. Every carve-out has a
  one-char-mutation twin in the benign corpus so it cannot be widened
  silently.
- **Hit corpus is constructed, not literal.** `"ghp_"+strings.Repeat("a",36)`
  rather than a 40-char literal, so t-7's whole-tree self-scan stays at zero
  without sprinkling allow-markers through test files. Two existing fixture
  constants still need a marker; that is the allow route working as designed.
- **Key-context rule requires an entropy floor as well as ≥16 chars.**
  Without it `Token: "--end-of-options"` (argfence/policy.go) and
  `DROSS_TEST_ABSENT_TOKEN` fire in the self-scan; the floor is pinned
  two-sided (32-hex fires, constant-name strings do not). Accepted limit: a
  low-entropy real password is missed — the locked entropy_rules decision
  already names missing unknown shapes as the honest limit.
- **Publish-surface line numbers.** A refusal on a body has no file, so the
  renderer prints `<surface>:<line>` (`PR body:12`) — the same shape as
  `file:line`, one renderer, one c-5 contract covering both.
- **Registry (t-2) is wave 1, before the code it describes exists.** It is the
  specification of which functions must be guarded; t-3/t-4 make it true and
  t-6 proves both directions. Writing it after the gates would make it a
  transcript of the tree instead of a claim the tree is judged against.
