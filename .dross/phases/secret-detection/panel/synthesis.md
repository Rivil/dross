# secret-detection — synthesis (cold judge)

Every codebase claim below was checked against `internal/` on 2026-09-19 before
grafting. Corrections to the drafts' facts, where they matter to the plan:

- Forge: four `doRaw` (`forge.go:837`, `github.go:368`, `jira.go:689`,
  `youtrack.go:927`), each the only `http.NewRequest` in its backend, wrapped by
  `do` → `redact.Err`. Jira `EnsureMilestone` delegates to `ensureVersion`;
  YouTrack `EnsureMilestone` is a no-op returning `"", nil`. Verification's
  "12 forge publish transports named EnsureMilestone / carrying a body param
  that call a primitive" therefore does not resolve to 12 on the live tree.
- Ship: `jsonPost` (open.go:132), `bbRequest` (bitbucket.go:51), `gitlabReq`
  (gitlab.go:236), `jsonGet` (forgejo.go:202, GET/nil only), `ghCommand` var
  (open.go:69) with FIVE call sites (open.go:95, comment.go:68 publish;
  basepr.go:64, headpr.go:53, merged.go:72 read-only) — risk and mvp both said
  four.
- `internal/cmd/ship.go` does not run validate; "3) Pre-flight gates" at :151;
  `--print-body` and `--no-push` return BEFORE `autoCommitDrossDirt` (:248).
  `autoCommitDrossDirt` (cleantree.go:20) has five callers: ship, phase.go
  ×3 (complete, record completion, start), milestone.go (prune).
- `issue.go` has 6 explicit `.(*forge.X)` assertions plus one `.(type)` switch
  (risk said six; mvp eight; verification eleven).
- Validate tests run in non-git `t.TempDir()` (B6 is real). `.gitignore` seeds
  `.dross/{security,quality,techdebt}/`, `handoff.md`, `local.toml`,
  `state.json`, `reap-log.json`.
- `security.IdentityIDAllowlist` (gitleaks.go:28) is id|key context only;
  `gitleaks_test.go:86-87` pins `"password":`/`token =` 16-hex as NOT the
  identity shape. AKIA placeholder: 8 lines in 4 files (3 tracked .dross, 1
  test). 16-hex password/token-context: 4 lines (gitleaks_test ×2, two
  scanner-self-exclusion panel notes). 34 non-test writer files under
  internal/. 15 `*File = "…"` consts (mvp said 13).
- All helpers the drafts lean on exist: `funcDecls`/`goFilesIn`/
  `readCmdSource` (toolfence_composer_test.go:422-463), `shipFixture`
  (ship_test.go:110), `shipMockFlow` (:639), `mustWrite` (cmd_test.go:463),
  `pathfence.Contain`/`WriteFile`, `dirtyTreeError` (phase.go:1059),
  `ensureDrossGitignore` (gitignore.go:79), `CriterionResult.Notes`
  (verify.go:533). Fixture lines cited by risk/verification are at the stated
  line numbers.

## Scores

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| risk | 5/5 — every criterion, plus the three ways c-3 is bypassed (untracked note, clean tree, phase-complete path) each owned by a named test | 5/5 — every contract names the mutation that would fail it; long-line, argv flag naming, ordering-by-Pos, marker-set pinning | 4/5 — 8 tasks, cohesive; t-8 touches 9 files incl. two historical panel notes; t-7 registers ~34 writer files | 5/5 — 4 waves, every dep real (t-5←t-2, t-6←t-3+t-4, t-7←t-2, t-8←all) |
| mvp | 3/5 — c-6 lives as a test inside t-2 but no task marks the two `s3cr3t-…` fixture constants its key-context rule fires on, so the test goes red with no owner; phase-complete auto-commit path uncovered | 4/5 — sharp on the enumeration (fixture calibration, vacuity floors) and on ship-clean-tree; no long-line, no non-vacuity twins for carve-outs | 3/5 — 5 tasks; t-2 carries c-3+c-6, t-4 six files, t-5 four walkers in one task | 4/5 — 3 waves, deps correct; registry after gates |
| verification | 4/5 — all six, fixture hygiene owned (t-7), registry validation owned (t-2); misses the phase-complete auto-commit path and long lines | 5/5 — non-vacuity twins per carve-out, live-reach proof per Scanned artifact, PEM-once, marker line-only semantics, benign-body-still-posts | 3/5 — 7 tasks but t-3 edits five ship files for 8 per-provider gates and t-6 is four residual arms in one task | 3/5 — registry in wave 1 is defensible, but its 20-entry content rests on a per-method predicate the tree does not satisfy (Jira/YouTrack EnsureMilestone delegate or no-op) |

**Skeleton: risk.** It is the only draft whose gate placement closes all three
c-3 bypasses (B4 untracked, B7 clean tree, phase-complete auto-commit), whose
residual predicate ("the function holding http.NewRequest / exec.Command /
ghCommand") resolves exactly against the tree (9 transports), whose c-1 rules
reuse the one existing definition of the identity-id shape, and which owns the
fixture hygiene c-6 needs. Verification is the strongest runner-up and
supplies most of the grafts.

## Merged plan

```
Phase secret-detection — 8 tasks across 4 waves

Wave 1
  t-1  Build secretscan rules, scanner, echo-free report          [risk+verification]
Wave 2 (depends t-1)
  t-2  Walk .dross artifacts; fail validate on hit                 [risk+verification]
  t-3  Screen forge transports at doRaw                           [risk+mvp+verification]
  t-4  Screen ship HTTP and gh argv transports                    [risk+verification]
Wave 3
  t-5  Gate auto-commit and ship pre-flight        (depends t-2)  [risk+mvp+verification]
  t-6  Transport registry + residual scan          (depends t-3, t-4)  [risk+verification]
  t-7  Writer/artifact registry + residual scan    (depends t-2)  [risk+verification+mvp]
Wave 4 (depends t-1..t-7)
  t-8  Self-scan the tree; pin the marker set                     [risk+verification]
```

### t-1 — Build secretscan rules, scanner, echo-free report  [risk+verification]
- wave: 1 · depends_on: [] · covers: c-1, c-5
- files: `internal/secretscan/rules.go`, `internal/secretscan/secretscan.go`, `internal/secretscan/payload.go`, `internal/secretscan/secretscan_test.go`
- description: New package. `rules.go`: fixed table of `Rule{Name, Regex, Prefix}` — github (ghp_/gho_/ghu_/ghs_/ghr_ + 36, github_pat_ + 22..), gitlab (glpat- + 20), atlassian (ATATT + 20..), aws (AKIA|ASIA + 16 upper/digit, carve-out: match ending `EXAMPLE`), slack (xox[abprs]- + 10..), openai/anthropic (sk- optional proj-/ant- + 20..), pem (BEGIN…PRIVATE KEY header line, reported once at the BEGIN line), authorization-header (`authorization` then Basic|Bearer then ≥8 value chars), key-context (key ending in password|passwd|pwd|secret|api_key|access_key|token|authorization, `:` or `=`, value ≥16 chars of `[A-Za-z0-9+/=_.~@!#%^*-]` — `$` and `<>` outside the class so `$GITHUB_TOKEN` and `<your-token>` never match — Shannon entropy ≥ 3.0 bits/char, and the line does NOT match `security.IdentityIDAllowlist`). `secretscan.go`: `const AllowMarker = "dross:allow-secret"`; `Scan(name string, r io.Reader) ([]Hit, error)` line-oriented over `bufio.Reader.ReadBytes('\n')` (no 64 KB ceiling; CRLF tolerated); a line containing AllowMarker yields no hits; `Hit{Rule, Location, Line, Length, Prefix}`; `(Hit) String()` = `<rule> at <location>:<line> (len=<n>, prefix=<fixed>)`; `Report(hits)` adds one remedy line naming AllowMarker; `ErrHit` typed error carrying `[]Hit` whose `Error()` is `Report`. Prefix is the RULE's fixed literal (ghp_, AKIA, Bearer, -----BEGIN), never bytes of the match; key-context hits carry an empty prefix. `payload.go`: `ScanPayload(surface string, body any) error` — `json.Marshal` then `json.Unmarshal` into `any`, walk every string leaf through `Scan(surface+":"+jsonPath, …)`, return `*ErrHit` on the first non-empty hit set, nil body → nil; `ScanArgv(tool string, args []string) error` scans each element with location `<tool> <preceding --flag>` when there is one, else `<tool> argv[i]`. Both pure, so t-3/t-4 are wiring only.
- test_contract:
  - `TestEveryRuleHasAHitCase` [verification]: the set of rule names equals the hit-corpus map's key set, and each key's RUNTIME-BUILT line (`"ghp_" + strings.Repeat("A1b2", 9)` style; PEM header assembled from parts) yields exactly one Hit whose `Rule` equals the key — a rule added without a corpus entry fails; a regex edit that stops matching its own shape fails; no literal token shape lives in the tree.
  - `TestBenignCorpusZeroHits` [risk]: runtime-built table of a 16-hex under `"key":`/`id =`/`"Key":` (the identity carve-out), a bare 40-hex commit SHA and one under `sha = `, a UUID bare and under `id = `, a 60-char base64 run under `"snippet":` and `expected =`, `auth_env = "GITHUB_TOKEN"`, `token = "$GITHUB_TOKEN"`, `Authorization: Bearer <token>`, `[redacted $GITHUB_TOKEN]`, the AWS doc placeholder (`AKIA…EXAMPLE`), a 21-char repetitive value (`hunter2` ×3, entropy < 3.0) under `password =`, and a 15-char value under `secret =`; any hit on any row fails naming row and rule.
  - `TestCarveOutsAreNotVacuous` [verification]: each benign carve-out row's one-char mutation DOES fire — 17-hex under `"key":` fires key-context (ok: it is `key =` + 17 chars ≥16 with entropy ≥3.0), AKIA+16 not ending `EXAMPLE` fires aws, `hunter2hunter2hunter2x1` (entropy pushed ≥3.0) fires — so each carve-out is proven to be doing the work rather than the rule being dead.
  - `TestKeyContextIdentityCarveOutIsKeyAware` [risk]: `"password": "<16hex>"` and `token = "<16hex>"` DO hit; `"key": "<16hex>"` does not — pins reuse of `security.IdentityIDAllowlist` rather than a value-only 16-hex exemption; a value-only carve-out fails the first two rows.
  - `TestPEMBlockReportedOnceAtBeginLine` [verification]: a 5-line PEM block gives one Hit, at the BEGIN line.
  - `TestAllowMarkerSilencesOnlyItsLine` [risk+verification]: 3-line reader with a synthesized github token on lines 1 and 3 and the marker on line 1 only reports line 3; the marker on a line with two shapes silences both; a marker on the line ABOVE a hit changes nothing; a marker on a clean line changes nothing.
  - `TestLongLineIsScannedToTheEnd` [risk]: a single 200 KB line with a synthesized github token in its last 60 bytes is reported at `:1` — a `bufio.Scanner` with default buffer fails this with `token too long` or a missed hit.
  - `TestReportNeverEchoesTheValue` [risk] (property): for each rule, 50 random values of the rule's shape; the concatenation of `Hit.String()`, `Report()` and `ErrHit.Error()` contains no 6-byte window of the value's VARIABLE part; the fixed prefix is allowed and must be present for prefix rules; key-context reports contain `prefix=` followed by nothing; a report that prints `%q` of the line or the match fails on every iteration.
  - `TestHitLocationIsFileLine` [risk]: `Scan("notes.md", …)` with the token on line 7 yields `Location=="notes.md"`, `Line==7`, and `String()` contains `notes.md:7`.
  - `TestScanPayloadWalksNestedLeaves` [risk]: `ScanPayload("POST /issue", map[string]any{"fields": map[string]any{"description": map[string]any{"content": []any{map[string]any{"text": "<synth token>"}}}}})` reports location `POST /issue:fields.description.content[0].text`; a struct body with a json-tagged `Body` field is reported under the tag name; `ScanPayload("x", nil)` is nil; `[]string{"clean", "<token>"}` reports `x:[1]` — a walk that stops at the top level fails the first row.
  - `TestScanArgvNamesTheFlag` [risk]: `ScanArgv("gh", []string{"pr","create","--title","t","--body","l1\n<token>"})` reports `gh --body:2`; a token in a positional (`[]string{"pr","comment","--","<token>"}`) reports `gh argv[3]`.

### t-2 — Walk .dross artifacts; fail validate on hit  [risk+verification]
- wave: 2 · depends_on: [t-1] · covers: c-3
- files: `internal/cmd/secretscan.go`, `internal/cmd/validate.go`, `internal/cmd/secretscan_scope_test.go`
- description: `listDrossArtifacts(root) ([]string, error)`: when `git rev-parse --is-inside-work-tree` succeeds, `git ls-files -z --cached --others --exclude-standard -- .dross` (tracked PLUS stageable-untracked, honouring .gitignore); otherwise `filepath.WalkDir(.dross)` of every regular file. `scanDrossArtifacts(root) ([]secretscan.Hit, error)` opens each via `pathfence.Contain(root, "dross-artifact", rel)`, streams it through `secretscan.Scan` with the root-relative path as location; a read or scan error is a refusal, not a skip. validate.go: after the phase loop, every hit's `String()` is appended to `problems` under a `secret:` prefix so the existing `✗`/exit-1 path fires.
- test_contract:
  - `TestValidateRefusesOnArtifactSecret` [risk]: temp dir (no git), `Init()`, then `.dross/phases/p/notes.md` (a path validate hand-lists nowhere) with a synthesized gitlab token on line 4 → `runCmd(Validate())` errors, output contains `gitlab-pat at .dross/phases/p/notes.md:4 (len=` and does NOT contain any 6-byte window of the token — proves the non-git fallback walk and the location format together.
  - `TestScopeIsTrackedPlusStageable` [risk]: temp git repo with `ensureDrossGitignore` and the security/quality ignore seeds applied; three files: tracked `.dross/board.json` clean, UNTRACKED `.dross/notes.md` with a token, IGNORED `.dross/security/run1/gitleaks.json` with a token → exactly one hit, at `.dross/notes.md` — drop `--others` and the untracked hit disappears; drop `--exclude-standard` and the ignored one appears.
  - `TestValidateAllowMarkerHonouredInToml` [risk]: `.dross/phases/p/spec.toml` line `text = "<synth token>" # dross:allow-secret` → validate exits 0 and prints no `secret:` line; the same file without the marker fails.
  - `TestScanErrorIsARefusalNotASkip` [risk]: a `.dross/x.toml` the walker cannot read (mode 000 on unix, skipped on Windows) → validate reports `secret scan: … : permission denied` as a problem rather than exiting 0.
  - `TestValidateSecretProblemsAreLast` [risk]: a malformed spec.toml AND a token in another phase → both problems print; the secret line is present, so the scan runs even when earlier checks already failed.

### t-3 — Screen forge transports at doRaw  [risk+mvp+verification]
- wave: 2 · depends_on: [t-1] · covers: c-2
- files: `internal/forge/forge.go`, `internal/forge/github.go`, `internal/forge/jira.go`, `internal/forge/youtrack.go`, `internal/forge/secretscan_test.go`
- description: Wiring only. Insert `if err := secretscan.ScanPayload(method+" "+endpoint, body); err != nil { return err }` as the FIRST statement of `(*Client).doRaw` (forge.go:837), `(*GitHubClient).doRaw` (github.go:368), `(*JiraClient).doRaw` (jira.go:689), `(*YouTrackClient).doRaw` (youtrack.go:927), ahead of the encoder and `http.NewRequest`. The wrapping `do` still applies `redact.Err`. Four one-line edits cover CreateIssue, UpdateIssue, EnsureMilestone(Entity), labels, comments and every future method.
- test_contract:
  - `TestCreateIssueRefusedBeforeRequest` [risk] (×4 backends, httptest server counting requests): `CreateIssue(IssueInput{Body: "line1\n<synth atlassian token>"})` returns an error that `errors.As` a `*secretscan.ErrHit`, whose message names `atlassian-token` and `:body` (forge/github/youtrack) or `:fields.description` (jira, inside ADF), and the server's request counter is 0 — move the screen after `http.NewRequest`/`client.Do` and the counter reads 1.
  - `TestUpdateIssueBodyPatchRefused` [risk] (×4): `UpdateIssue(key, IssuePatch{Body: &tok})` → refused, 0 requests; `UpdateIssue(key, IssuePatch{Labels: …})` with clean labels → the request goes out.
  - `TestEnsureMilestoneDescriptionRefused` [risk]: YouTrack `EnsureMilestoneEntity("epic", "v9", "<synth pem header line>")` and Jira `EnsureMilestoneEntity("version", …)` refused with `pem-private-key`, 0 requests — a publish path with no `Body` field involved.
  - `TestBoardPublishBenignBodyPosts` [verification]: renderPhaseBody-shaped text carrying a 16-hex identity id, a 40-hex SHA and a UUID posts (counter == 1) on every backend — the gate cannot break ordinary board sync.
  - `TestDoRawNilBodyIsUntouched` [mvp]: a nil body passes through with counter == 1 (GET paths unchanged).
  - `TestGetAndListAreNotScreened` [risk]: `GetIssue`/`ListIssues` against a server whose RESPONSE body carries a synthesized token succeed and return the issue — the screen is on outbound payloads only.
  - `TestPayloadErrorNeverEchoes` [risk]: the refusal error string from each backend contains no 6-byte window of the token's variable part; the assertion runs on `err.Error()` after `redact.Err` wrapping, i.e. on the string that would reach telemetry `err_detail`.

### t-4 — Screen ship HTTP and gh argv transports  [risk+verification]
- wave: 2 · depends_on: [t-1] · covers: c-2
- files: `internal/ship/open.go`, `internal/ship/comment.go`, `internal/ship/basepr.go`, `internal/ship/headpr.go`, `internal/ship/merged.go`, `internal/ship/bitbucket.go`, `internal/ship/gitlab.go`, `internal/ship/secretscan_test.go`
- description: `jsonPost` (open.go:132): `ScanPayload("POST "+endpoint, body)` before the encoder. `bbRequest` (bitbucket.go:51) and `gitlabReq` (gitlab.go:236): same, first statement. gh argv: `open.go` gains a package-level `screenedGH(args ...string) (*exec.Cmd, error)` that calls `secretscan.ScanArgv("gh", args)` and only then `ghCommand(args...)`; all five `ghCommand(` call sites (open.go:95, comment.go:68, basepr.go:64, headpr.go:53, merged.go:72) switch to it. The `ghCommand` var stays as the test seam. `jsonGet` (forgejo.go:202) is GET/nil and is not edited.
- test_contract:
  - `TestOpenPRGitHubRefusedBeforeGh` [risk]: `ghCommand` stubbed to record invocations; `OpenPR(OpenOpts{Provider: "github", Body: "<synth github token on line 3>"})` returns `*secretscan.ErrHit` naming `github-token` and `gh --body:3`, and the stub recorded zero invocations — scan after exec and the stub records one.
  - `TestOpenPRTitleIsScreenedToo` [risk]: same with the token in `Title` → refused at `gh --title:1`; a gate that screens only Body fails.
  - `TestPostCommentRefused` [risk] (github via stub; forgejo, gitlab, bitbucket via httptest counters): `PostComment(CommentOpts{Body: pem header})` → `pem-private-key`, zero requests/invocations on every provider.
  - `TestOpenPRForgejoGitLabBitbucketRefused` [risk]: `OpenPR` per HTTP provider with a slack-shaped body → refused, request counter 0 — proves each of the three request funcs carries the screen, not just `jsonPost`.
  - `TestOpenPRBenignBodyStillPosts` [verification]: a body holding a 16-hex id, a 40-hex SHA and a UUID reaches the transport on every provider (counter/stub == 1): the gate cannot break ordinary shipping.
  - `TestNoRawGhCommandCallOutsideTheSeam` [risk] (AST over `internal/ship` non-test files): the only call expression whose callee is the identifier `ghCommand` sits inside `screenedGH`; any other fails naming file:line — the local residual for the argv path, which t-6 later folds into the registry.
  - `TestAllowMarkerOnBodyLine` [risk]: a PR body whose token line ends with `dross:allow-secret` posts (stub invoked once) — the marker route works on composed bodies, not only on files.

### t-5 — Gate auto-commit and ship pre-flight  [risk+mvp+verification]
- wave: 3 · depends_on: [t-2] · covers: c-3
- files: `internal/cmd/cleantree.go`, `internal/cmd/ship.go`, `internal/cmd/secretscan_gate_test.go`
- description: `autoCommitDrossDirt` calls `scanDrossArtifacts(repoDir)` BEFORE `git add .dross` and returns `*secretscan.ErrHit` wrapped as `refusing to auto-commit .dross: …` on a hit — the primitive that turns an untracked note into a commit is the one that refuses, which also covers phase complete / record completion / phase start / milestone prune. ship.go: a new pre-flight step at the top of "3) Pre-flight gates" (before the verdict switch, before the `--print-body`/`--no-push` returns, before `repointDoomedRedProofs`/`autoCommitDrossDirt`) calls `scanDrossArtifacts(root)` and refuses with `secret in tracked .dross artifact — fix it by hand or mark the line dross:allow-secret, then re-run ship`, so a CLEAN tree with an already-committed token still refuses. Ship calls the shared scanner, not the whole `dross validate` (pulling all of validate into ship would newly fail ships on schema problems — a scope change).
- test_contract:
  - `TestShipRefusesCommittedSecretOnCleanTree` [risk+mvp]: `shipFixture` + `shipMockFlow` with a verified phase whose `.dross/phases/p/notes.md` carrying a synthesized token is COMMITTED (tree clean) → `dross ship p` errors naming `notes.md:<line>` before any push: the mock provider records zero requests AND the origin bare repo has no `phase/p` ref — a gate placed only inside `autoCommitDrossDirt` passes this test wrongly because the no-dirt early return skips it.
  - `TestAutoCommitRefusesUntrackedSecretNote` [risk]: repo with an untracked `.dross/phases/p/notes.md` carrying a token; `autoCommitDrossDirt(repoDir, "shipping")` returns an error, `git status --porcelain` still lists the note as untracked, `git log` has no new `chore(dross)` commit — reorder the scan after `git add` and the index contains the note.
  - `TestAutoCommitStillRefusesCodeDirtFirst` [risk]: dirty `main.go` plus a secret note → the error is the existing `dirtyTreeError`; with only the note → the secret refusal.
  - `TestPhaseCompleteRefusesOnSecret` [risk]: `dross phase complete p` on a repo whose `.dross/phases/p/changes.json` carries a token → refused, `state.json` unchanged, no completion event recorded — proves the cleantree placement reaches the non-ship callers.
  - `TestShipPrintBodyStillScans` [risk]: `dross ship p --print-body` with the committed-secret fixture errors instead of printing the body.
  - `TestShipGateOrderingIsBeforeAutoCommit` [risk] (AST over ship.go's RunE): the call to `scanDrossArtifacts` has a lower `token.Pos` than the call to `autoCommitDrossDirt` and than every `pushPhaseBranch`/`ship.OpenPR` call.
  - `TestShipAndValidateShareOneScanner` [verification] (AST over validate.go, ship.go, cleantree.go): each RunE/helper body calls `scanDrossArtifacts`; a second inline walk or a second `secretscan.Scan` call in any of the three files fails (one ruleset, one candidate set, no drift).

### t-6 — Transport registry + residual scan  [risk+verification]
- wave: 3 · depends_on: [t-3, t-4] · covers: c-4
- files: `internal/secretscan/sinks.go`, `internal/secretscan/sinks_test.go`, `internal/cmd/secretscan_transport_enum_test.go`, `internal/cmd/testdata/secretscan/leaky_transport.go.txt`
- description: `sinks.go`: `Transport{Package, Func, Screened *Screened{Call string}, ReadOnly *ReadOnly{Why}}` and `Transports()` listing the nine outbound seams in the tree: `forge.(*Client).doRaw`, `forge.(*GitHubClient).doRaw`, `forge.(*JiraClient).doRaw`, `forge.(*YouTrackClient).doRaw`, `ship.jsonPost`, `ship.bbRequest`, `ship.gitlabReq`, `ship.screenedGH` (Screened, Call = `ScanPayload`/`ScanArgv`), `ship.jsonGet` (ReadOnly: method literal "GET", nil body). `Validate([]Transport) []error` in the toolfence shape. The enum test walks `../ship` and `../forge` non-test files for every call to `http.NewRequest`, `http.NewRequestWithContext`, `http.Post`, `http.Get`, `(*http.Client).Do`, `exec.Command`, and the `ghCommand` identifier; each site's enclosing function must be a registered Transport; Screened must contain a `secretscan.ScanPayload`/`ScanArgv` call whose `Pos()` precedes the request call; ReadOnly must have a literal `"GET"` method and nil body. Stale arm: every registered Func resolves to a FuncDecl.
- test_contract:
  - `TestEveryOutboundSeamIsRegistered` [risk]: over the live tree, the walk resolves ≥ 9 transport sites across BOTH `../ship` and `../forge` (per-package vacuity floor, as toolfence_composer does per group) and reports none undeclared — add `http.Post(…)` anywhere in ship or forge and this names the file:line.
  - `TestScreenPrecedesRequestInEveryScreenedTransport` [risk]: for each Screened entry, `Pos(ScanPayload call) < Pos(http.NewRequest | ghCommand call)` inside that FuncDecl — swap the two statements in any doRaw and this fails naming the function.
  - `TestReadOnlyTransportsSendNoBody` [risk]: `jsonGet`'s `http.NewRequest` call has a `"GET"` literal first arg and `nil` third arg — change either and the ReadOnly declaration is rejected as false.
  - `TestTransportScanTripsOnTheLeakyFixture` [risk+verification]: `leaky_transport.go.txt` holds (a) a func calling `http.NewRequest("POST", …)` with no screen, (b) a func with the screen AFTER the request, (c) a clean screened func, (d) a `ghCommand(` call outside `screenedGH`, (e) a GET-shaped func with nil body declared ReadOnly; the scan must flag a, b and d and pass c and e — calibrated on a written-down answer, not on the tree's post-fix state.
  - `TestStaleTransportDeclarationFails` [risk+verification]: a registry entry naming a func that does not exist is reported by the stale arm (fed synthetically); a renamed backend method fails here.
  - `TestTransportRegistryValidate` [risk+verification] (in `sinks_test.go`): synthetic entries with no disposition, both dispositions, empty Func, empty ReadOnly.Why, and a duplicate each produce one specific error; the real registry produces none.
  - `TestPublishSinkFuncsAreQualified` [verification]: every Func is `pkg.Recv.Method` or `pkg.func` over exactly {ship, forge}; count is 9, so a silently shrunk list fails here before the AST arm runs.

### t-7 — Writer/artifact registry + residual scan  [risk+verification+mvp]
- wave: 3 · depends_on: [t-2] · covers: c-4
- files: `internal/secretscan/writers.go`, `internal/cmd/secretscan_writer_enum_test.go`, `internal/cmd/testdata/secretscan/unregistered_writer.go.txt`
- description: `writers.go`: `Writer{File string; UnderDross *UnderDross{Artifacts []string}; MachineLocal *MachineLocal{IgnoreSeed, Why}; OutsideDross *OutsideDross{Why}}` and `Writers()` — one entry per non-test file in `internal/` that calls `os.WriteFile`, `os.Create`, `os.OpenFile` (write flags), or `pathfence.WriteFile` (34 files today). UnderDross entries are covered by t-2's location-based scope; MachineLocal entries (state.json, local.toml, handoff.md, reap-log.json, `security/`, `quality/`, `techdebt/`) name the ignore seed that keeps them out of git; OutsideDross entries (ARCHITECTURE.md, README, ~/.claude, hooks settings, telemetry.jsonl, install symlinks, gitignore/gitattributes) say why they are not a `.dross` artifact. Agent-authored artifacts with no Go writer (panel/*.md, REVIEW.md, pr-body.md, proof.md, CriterionResult.Notes) are declared with `Writers: ["agent"]` and exempt from the writer-resolution arm. Enum test walks `internal/` for the write verbs and requires the enclosing FILE to be declared; stale arm; MachineLocal arm via `git check-ignore`; UnderDross arm via live reach.
- test_contract:
  - `TestEveryDrossWriterIsDeclared` [risk]: live walk resolves ≥ 30 writer files and every one is in `Writers()`; add `os.WriteFile` to any undeclared file and this names it.
  - `TestMachineLocalWritersAreReallyIgnored` [risk+verification]: for each MachineLocal entry, `git check-ignore -q .dross/<path>` exits 0 in a temp repo seeded by `ensureDrossGitignore` + the security/quality/techdebt scaffolds — remove `handoff.md` from the seeded ignore block and its entry fails as a false claim.
  - `TestEveryScannedArtifactIsReached` [verification+risk]: for each UnderDross artifact name (board.json, changes.json, spec.toml, plan.toml, verify.toml, tests.json, survivors.toml, deferred.toml, milestones/*.toml, rules.toml, project.toml, red-proof docs, watch.state.json, panel/*.md …), materialise one concrete instance at a plausible depth in a temp repo holding a synthesized token, `git add`; `scanDrossArtifacts` reports ≥ 1 finding AT that path, in BOTH the git and non-git walker modes — an artifact name the walker filtered out (e.g. by extension) fails here.
  - `TestOutOfScopeEntriesAreNotUnderDross` [verification]: OutsideDross entries (ARCHITECTURE.md, telemetry.jsonl, defaults.toml …) must not resolve to a path under `.dross/`.
  - `TestWriterScanTripsOnTheFixture` [risk]: `unregistered_writer.go.txt` with an `os.WriteFile` in a file name absent from the registry is flagged; the fixture's second func using `os.ReadFile` only is not.
  - `TestStaleWriterDeclarationFails` [risk+verification]: a synthetic entry naming `internal/nothing/here.go` is reported by the stale arm; agent-authored entries are exempt from resolution.
  - `TestEveryFileConstIsDeclared` [mvp]: every `*File = "…"` const under internal/ (15 today) matches an artifact name in some `Writers()` entry — a new persisted artifact name with no registry line fails.

### t-8 — Self-scan the tree; pin the marker set  [risk+verification]
- wave: 4 · depends_on: [t-1, t-2, t-3, t-4, t-5, t-6, t-7] · covers: c-6
- files: `internal/cmd/secretscan_selfscan_test.go`, `internal/cmd/env_test.go`, `internal/cmd/hermetic_env_test.go`, `internal/forge/hostallow_test.go`, `internal/forge/hostile_config_test.go`, `internal/argfence/policy.go`, `internal/security/gitleaks_test.go`, `.dross/phases/scanner-self-exclusion/panel/risk.md`, `.dross/phases/scanner-self-exclusion/panel/synthesis.md`
- description: Append ` // dross:allow-secret` (Go) or ` dross:allow-secret` (md) to the lines that are genuinely secret-shaped fixtures and that t-1's rules fire on: `env_test.go:31` (FORGEJO_TOKEN fixture), `hermetic_env_test.go:146` (absent-token sentinel), `hostallow_test.go:25`, `hostile_config_test.go:60` (sentinel tokens), `argfence/policy.go:69` (`Token: "--end-of-options"` — exactly 16 chars, entropy 3.08), `gitleaks_test.go:86-87` (password/token-context 16-hex rows), and the two scanner-self-exclusion panel notes carrying the same rows. The AWS placeholder sites (8 lines, 4 files) need no marker (t-1's EXAMPLE carve-out). If the self-scan surfaces anything else, the fix is a marker on that line or a carve-out with a benign-corpus entry — never a rule deletion. The self-scan test runs `git ls-files -z` at the repo root (skip with a message if not a git checkout), scans every file through `secretscan.Scan`, asserts zero hits; a second assertion collects every line where the marker actually SILENCED a hit (re-scan with the marker stripped; inert markers in prose do not count) and compares the multiset of `<basename>` → count against a pinned table.
- test_contract:
  - `TestDrossTreeHasNoUnmarkedHits` [risk+verification]: every tracked file scans clean; the failure message lists `file:line rule (len, prefix)` for each hit and never the line — add a literal token-shaped string to any test and this fails; remove the EXAMPLE carve-out and it fails on the AWS placeholder sites.
  - `TestSelfScanIsNotVacuous` [verification]: the same file list plus one temp file holding a synthesized hit yields exactly one finding, at that file — a walker that silently read nothing cannot pass the test above.
  - `TestAllowMarkerSitesArePinned` [risk]: silencing markers found (marker stripped → line hits) == {env_test.go:1, hermetic_env_test.go:1, hostallow_test.go:1, hostile_config_test.go:1, policy.go:1, gitleaks_test.go:2, risk.md:1, synthesis.md:1} plus the ones this phase's own artifacts need; one more or one fewer fails naming the file.
  - `TestMarkerNeverAppearsInNonTestGoOutsideArgfence` [risk]: the only non-test `.go` file carrying the marker is `internal/argfence/policy.go`.
  - `TestSelfScanCoversTheWholeTreeNotJustDross` [risk+verification]: the scan visited ≥ 1500 files, ≥ 900 under `.dross/`, and ≥ 1 file outside `.dross/` (tree has 1770 tracked, 961 under .dross today).
  - `TestValidateOnDrossItself` [risk]: `runCmd(Validate())` from the real repo root exits 0 with no `secret:` line — the live gate agrees with the test walk.

### Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-3 (board issue body/patch/milestone description, four backends), t-4 (PR body, PR title, PR comment, gh argv, three HTTP providers) |
| c-3 | t-2 (validate + scope), t-5 (auto-commit + ship pre-flight, clean-tree and phase-complete cases) |
| c-4 | t-6 (publish transports), t-7 (.dross writers + artifact reach) |
| c-5 | t-1 (report format + echo property), t-3/t-4 (error strings after redact), t-8 (live failure messages) |
| c-6 | t-8 |

Locked decisions, where each lands: hit_disposition → t-1 `ErrHit` is returned, nothing rewrites text (t-2/t-3/t-4/t-5 all return it); pattern_source → t-1 compiles regexes into the binary, no PATH lookup anywhere; entropy_rules → t-1 pins the key set and no bare-entropy rule (base64 benign row); allowlist_route → t-1 marker is line-only, identity-id is `security.IdentityIDAllowlist`, no project.toml key; scan_scope → t-2 walks only `.dross/` and t-3/t-4 gate only outbound bodies; t-8's whole-tree scan is a TEST of c-6, not a gate.

## Disagreements

### D1 — Forge gate seam: `doRaw` ×4 vs per-method ×12
- risk, mvp: gate as the first statement of each backend's `doRaw`; residual predicate = "the function holding `http.NewRequest`".
- verification: gate inside each of CreateIssue/UpdateIssue/EnsureMilestone ×4 (12 sites) so the residual is a uniform "guard precedes primitive in the function that holds the primitive" check; registry lists 20 per-method funcs.
- **Default: doRaw (risk/mvp).** Verified against the tree: Jira `EnsureMilestone` delegates to `ensureVersion` and YouTrack `EnsureMilestone` returns `"", nil`, so verification's "12 forge sites" predicate resolves to fewer and would need sub-function entries (ensureVersion, ensureEpic, …) to hold. doRaw is where every backend's `http.NewRequest` sits, the predicate is exact, and four edits cover milestone/label/comment/future methods. Cost: the location string in a refusal is `POST /issue:fields.description…` rather than `issue body:<line>` (see D8).
- Why it matters: c-4 forbids a hand-list; the predicate must resolve exactly or the residual test is either vacuous or permanently red.

### D2 — Ship gate seam: transport primitives + `screenedGH` vs the two dispatchers vs 8 per-provider funcs
- risk: gate `jsonPost`/`bbRequest`/`gitlabReq` first-statement, and wrap `ghCommand` in `screenedGH` with an AST test that no raw `ghCommand(` call survives outside it.
- mvp: gate only `ship.OpenPR` and `ship.PostComment` (2 sites); read-only funcs declared in the registry with a "never selects .Body/.Title" assertion.
- verification: gate inside each of the 8 `open*PR`/`post*Comment` funcs, one line each.
- **Default: risk.** The gh argv path is `exec.Command`, not HTTP (verified: `ghCommand` var open.go:69, 5 call sites); only risk treats it as a first-class transport. Dispatcher-only leaves `openGitHubPR` reachable ungated after any refactor that calls it directly (comment.go already says this of `postGitHubComment`), and its residual needs a two-hop reachability proof. Per-provider (verification) is a hand-list of 8 where the primitive seam is 4 + 1.
- Why it matters: same as D1 — the residual predicate has to be one positional check against the function holding the egress.

### D3 — Artifact scope: git-enumerated (tracked ∪ stageable-untracked, `.gitignore` honoured) vs filesystem walk minus a declared skip set
- risk, verification: `git ls-files --cached --others --exclude-standard -- .dross`, with `filepath.WalkDir` fallback when not in a git work tree.
- mvp: `WalkDir(.dross)` always, skipping only `security/`, `quality/`, `techdebt/` as registry-declared dirs; argues validate's tests run without git so a git walk needs a fallback and a second test set.
- **Default: risk.** Verified: validate tests do run in non-git `t.TempDir()`, so the fallback is required either way; but the seeded `.gitignore` also excludes `handoff.md`, `local.toml`, `state.json`, `reap-log.json`, which mvp's walk would scan. Git's ignore rules are the single source of truth for "what `git add .dross` will stage"; a hand-kept skip set drifts from the seeded gitignore, and t-7's MachineLocal arm can check `git check-ignore` only if the scope is git-defined. Cost: two walker modes, both exercised by t-7's reach test.
- Why it matters: c-3 says "tracked"; B4 (untracked note staged by auto-commit) and B5 (gitleaks output holding real secrets) are both settled by the same `--others --exclude-standard` pair.

### D4 — Identity-id carve-out: id|key context (`security.IdentityIDAllowlist`) vs any key-context 16-hex
- risk: reuse `security.IdentityIDAllowlist` — `"key":`/`id =` 16-hex is silent; `token =`/`"password":` 16-hex fires. Cost: markers on `gitleaks_test.go:86-87` and two old panel notes.
- mvp, verification: 16-hex under ANY key-context word (including token/password) is silent — marker-free, but exempts a real 16-hex password.
- **Default: risk.** Verified: `gitleaks_test.go:86-87` already pins `"password":`/`token =` 16-hex as NOT the identity shape (`want false`), and the locked allowlist_route says "the built-in identity-id shape stays a fixed rule carve-out" — the tree has exactly one definition of that shape. Two definitions (gate vs `dross secure`) would disagree on the same line. Four markers is the allowlist route working as designed.
- Why it matters: the gate and gitleaks must agree on what an identity id is, or `dross secure` flags what validate passes.

### D5 — Entropy floor on key-context values: 3.0 bits/char + markers vs a floor tuned to silence the tree's sentinel constants
- risk: floor 3.0 (kills `hunter2hunter2hunter2`-class placeholders); the six secret-shaped fixture lines (3.6–4.0 bits) get markers, including one in production code (`argfence/policy.go:69`), guarded by `TestMarkerNeverAppearsInNonTestGoOutsideArgfence`.
- verification: floor set so `Token: "--end-of-options"` and `DROSS_TEST_ABSENT_TOKEN` are silent without markers; `s3cr3t-…` constants still get markers.
- mvp: no entropy floor stated; placeholder carve-outs (`<token>`, `%q`, `${VAR}`) only.
- **Default: risk.** A floor high enough to silence `DROSS_TEST_ABSENT_TOKEN` (~3.5) also silences `MyP@ssw0rd!2024xyz` (3.73) — a real-password shape. The floor exists to kill repetition, not vocabulary; secret-shaped fixtures are exactly what the marker is for, and the pinned marker table makes each one a counted, reviewable exemption.
- Why it matters: this is the only knob that trades c-1 recall against c-6 marker count; the choice should be visible, not buried in a constant.

### D6 — Gate `autoCommitDrossDirt` in addition to validate + ship
- risk: yes — the commit-creating primitive refuses before `git add .dross`, covering phase complete / record completion / phase start / milestone prune (five callers verified).
- mvp: rejects placement INSIDE `autoCommitDrossDirt` as the ship gate (no-op on a clean tree) and does not add it elsewhere; phase-complete is unprotected.
- verification: ship scans before `autoCommitDrossDirt`; silent on the other four callers.
- **Default: risk (both placements).** mvp's objection is to the helper as the SOLE gate, which risk also rejects (B7 → explicit ship pre-flight). Leaving the helper ungated means `dross phase complete` commits a token note that `dross ship` then refuses one command later — the token is already in history by then.
- Why it matters: c-3 says "cannot be committed"; without this, the commit happens and only the ship is blocked.

### D7 — Registry timing: wave 1 (spec before code) vs wave 3 (after the gates)
- verification: `sinks.go` in wave 1 so the registry is a claim the tree is judged against, not a transcript of it.
- risk, mvp: registry lands with the enumeration tests in wave 3, after the gates exist.
- **Default: wave 3 (risk).** Verification's argument is sound in principle, but its wave-1 content (20 per-method entries) is the thing D1 rejects; a wave-1 registry of the 9 transports would have to name `screenedGH` before t-4 creates it. The fixture-calibrated residual test (`TestTransportScanTripsOnTheLeakyFixture`) is what keeps the wave-3 registry honest — it fails on a written-down answer, not the post-fix tree.
- Why it matters: only ordering; the test content is the same either way.

### D8 — Refusal location on publish surfaces: transport-derived vs composer-named
- risk: `gh --body:3`, `POST /issue:fields.description.content[0].text` — what the transport seam knows.
- verification, mvp: `PR body:12`, `issue body:<line>`, `milestone description:<line>` — what the user recognises.
- **Default: risk.** Follows from D1/D2: the gate sits below the composer and cannot name it without a hand-list. The transport location is still `surface:line`, so c-5's one renderer covers files and bodies alike.
- Why it matters: cosmetic for the user, structural for c-4 — a composer-named location would require the composer to pass its name down, which is a second hand-list.

### D9 — c-4 shape: two registries in two tasks vs one task
- risk: t-6 (transports) and t-7 (writers) as separate tasks with independent deps.
- mvp: one task (t-5) with four walkers; verification: t-2 registry + t-6 with four residual arms.
- **Default: two tasks (risk).** t-6 depends on t-3+t-4 and t-7 on t-2, so they run in parallel; one task would serialise a ~34-file writer registry behind the transport gates for no reason.
- Why it matters: wave width only.

### D10 — Marker-set pinning
- risk: `TestAllowMarkerSitesArePinned` compares the multiset of silencing markers against a table; any new marker is a test edit.
- mvp, verification: no pin.
- **Default: pin (risk).** A silencing marker is a disabled finding; the locked allowlist_route rejects regex allowlists precisely because they can grow silently. The pin is the same guard applied to the marker route. Cost: one table edit per new legitimate marker.
- Why it matters: without it the marker route becomes the repo-wide allowlist the spec rejected, one line at a time.
