# secret-detection — risk-lens draft

Lens: failure modes drive the graph. Each task owns exactly one way the gate can
be bypassed, fire wrongly, or leak the thing it is guarding.

The bypass inventory this plan is shaped around (each → owning task):

| # | How the gate breaks | Owner |
|---|---|---|
| B1 | A rule misses a real shape, or fires on an identity id / SHA / UUID / base64 fixture | t-1 |
| B2 | The hit report, the returned error, or the telemetry `err_detail` echoes the value | t-1 (property test), t-8 (live surfaces) |
| B3 | A line longer than bufio's 64 KB default silently truncates the scan | t-1 |
| B4 | The note is UNTRACKED when ship runs; `git add .dross` stages it after validate looked only at tracked files | t-2 (scope), t-5 (gate placement) |
| B5 | `.dross/security/<run>/` holds gitleaks' real findings; scanning ignored dirs makes every secure run refuse ship | t-2 |
| B6 | validate runs in a non-git temp dir (every existing validate test) and the walker errors instead of falling back | t-2 |
| B7 | The token is already committed, the tree is clean, `autoCommitDrossDirt` no-ops → ship must still refuse | t-5 |
| B8 | Board has FOUR backends behind concrete type assertions (`ctx.client.(*forge.YouTrackClient)`), so a BoardClient wrapper breaks the build; gating per method hand-lists 14 sites | t-3 (transport seam), t-6 (residual) |
| B9 | `gh pr create --body` is argv, not HTTP — an HTTP-only gate misses the GitHub PR path entirely | t-4 |
| B10 | Jira wraps the body in ADF nodes; a scan of the pre-marshal `Body` string misses a composer that builds the map directly | t-3 (scan the marshalled leaves) |
| B11 | A new transport / a new `.dross` writer lands outside the registry | t-6, t-7 |
| B12 | The hit corpus is literal in a `_test.go`, so c-6's self-scan trips on the tests | t-1 (synthesize), t-8 (pinned marker set) |
| B13 | The tree already carries secret-shaped fixtures (AWS doc placeholder `AKIA…EXAMPLE` ×9, password-context 16-hex ×4, env fixture sentinels ×5) | t-1 (EXAMPLE carve-out), t-8 (markers, pinned count) |

```
Phase secret-detection — 8 tasks across 4 waves

Wave 1
  t-1  Build secretscan rules, scanner, echo-free report
       files:    internal/secretscan/rules.go,
                 internal/secretscan/secretscan.go,
                 internal/secretscan/payload.go,
                 internal/secretscan/secretscan_test.go
       covers:   c-1, c-5
       contract: see below

Wave 2 (depends t-1)
  t-2  Walk .dross artifacts; fail validate on hit
       files:    internal/cmd/secretscan.go,
                 internal/cmd/validate.go,
                 internal/cmd/secretscan_scope_test.go
       covers:   c-3
  t-3  Screen forge transports before every request
       files:    internal/forge/forge.go, internal/forge/github.go,
                 internal/forge/jira.go, internal/forge/youtrack.go,
                 internal/forge/secretscan_test.go
       covers:   c-2
  t-4  Screen ship HTTP and gh argv transports
       files:    internal/ship/open.go, internal/ship/bitbucket.go,
                 internal/ship/gitlab.go, internal/ship/secretscan_test.go
       covers:   c-2

Wave 3
  t-5  Gate auto-commit and ship pre-flight     (depends t-2)
       files:    internal/cmd/cleantree.go, internal/cmd/ship.go,
                 internal/cmd/secretscan_gate_test.go
       covers:   c-3
  t-6  Transport registry + residual scan        (depends t-3, t-4)
       files:    internal/secretscan/sinks.go,
                 internal/secretscan/sinks_test.go,
                 internal/cmd/secretscan_transport_enum_test.go,
                 internal/cmd/testdata/secretscan/leaky_transport.go.txt
       covers:   c-4
  t-7  Writer registry + residual scan           (depends t-2)
       files:    internal/secretscan/writers.go,
                 internal/cmd/secretscan_writer_enum_test.go,
                 internal/cmd/testdata/secretscan/unregistered_writer.go.txt
       covers:   c-4

Wave 4 (depends t-1..t-7)
  t-8  Self-scan the tree; pin the marker set
       files:    internal/cmd/secretscan_selfscan_test.go,
                 internal/cmd/env_test.go, internal/cmd/hermetic_env_test.go,
                 internal/forge/hostallow_test.go,
                 internal/forge/hostile_config_test.go,
                 internal/argfence/policy.go,
                 internal/security/gitleaks_test.go,
                 .dross/phases/scanner-self-exclusion/panel/risk.md,
                 .dross/phases/scanner-self-exclusion/panel/synthesis.md
       covers:   c-6
```

## Tasks in full

### t-1 — Build secretscan rules, scanner, echo-free report
- wave: 1 · depends_on: [] · covers: c-1, c-5 · status: pending
- files: `internal/secretscan/rules.go`, `internal/secretscan/secretscan.go`, `internal/secretscan/payload.go`, `internal/secretscan/secretscan_test.go`
- description: New package. `rules.go`: a fixed table of `Rule{Name, Regex, Prefix func(match) string}` — github (ghp_/gho_/ghu_/ghs_/ghr_ + 36, github_pat_ + 22..), gitlab (glpat- + 20), atlassian (ATATT + 20..), aws (AKIA|ASIA + 16 upper/digit, carve-out: match ending `EXAMPLE`), slack (xox[abprs]- + 10..), openai/anthropic (sk- optional proj-/ant- + 20..), pem (BEGIN…PRIVATE KEY header line), authorization-header (`authorization` then Basic|Bearer then ≥8 value chars), key-context (key ending in password|passwd|pwd|secret|api_key|access_key|token|authorization, `:` or `=`, value ≥16 chars of `[A-Za-z0-9+/=_.~@!#%^*-]` — `$` and `<>` deliberately outside the class so `$GITHUB_TOKEN` and `<your-token>` never match — Shannon entropy ≥ 3.0 bits/char, and the line does NOT match `security.IdentityIDAllowlist`). `secretscan.go`: `const AllowMarker = "dross:allow-secret"`; `Scan(name string, r io.Reader) ([]Hit, error)` line-oriented over `bufio.Reader.ReadBytes('\n')` (no 64 KB ceiling; CRLF tolerated); a line containing AllowMarker yields no hits; `Hit{Rule, Location, Line, Length, Prefix}`; `(Hit) String()` = `<rule> at <location>:<line> (len=<n>, prefix=<fixed>)`; `Report(hits) string` adds the one remedy line naming AllowMarker; `ErrHit` typed error carrying `[]Hit` whose `Error()` is `Report`. Prefix is the RULE's fixed literal (ghp_, AKIA, Bearer, -----BEGIN) — never bytes of the match; key-context hits carry an empty prefix. `payload.go`: `ScanPayload(surface string, body any) error` — `json.Marshal` then `json.Unmarshal` into `any`, walk every string leaf through `Scan(surface+":"+jsonPath, …)` (a Jira ADF text node reports as `POST /issue:fields.description.content[0].content[0].text`), return `*ErrHit` on the first non-empty hit set, nil body → nil; `ScanArgv(tool string, args []string) error` scans each element with location `<tool> <preceding --flag>` when there is one, else `<tool> argv[i]`. Both are pure functions so t-3/t-4 are wiring only.
- test_contract:
  - `TestHitCorpusEveryShapeCaught`: a table of one line per rule, every value BUILT at runtime (`"ghp_" + strings.Repeat("A1b2", 9)` style, PEM header assembled from parts) so no literal token shape lives in the tree; each row must produce exactly one hit whose `Rule` equals the row's rule name — drop or narrow any rule and its row fails.
  - `TestBenignCorpusZeroHits`: a runtime-built table of a 16-hex under `"key":`/`id =`/`"Key":` (the identity carve-out), a bare 40-hex commit SHA and one under `sha = `, a UUID bare and under `id = `, a 60-char base64 run under `"snippet":` and `expected =`, `auth_env = "GITHUB_TOKEN"`, `token = "$GITHUB_TOKEN"`, `Authorization: Bearer <token>`, `[redacted $GITHUB_TOKEN]`, the AWS doc placeholder (`AKIA…EXAMPLE`), a 21-char repetitive value (`hunter2` ×3, entropy < 3.0) under `password =`, and a 15-char value under `secret =`; any hit on any row fails, and the failure names the row and the rule — widen the aws rule past the EXAMPLE carve-out, drop the entropy floor, or drop the identity carve-out and a specific row fails.
  - `TestKeyContextIdentityCarveOutIsKeyAware`: `"password": "<16hex>"` and `token = "<16hex>"` DO hit (password/token is not id|key context), `"key": "<16hex>"` does not — pins reuse of `security.IdentityIDAllowlist` rather than a value-only 16-hex exemption; a value-only carve-out fails the first two rows.
  - `TestAllowMarkerSilencesOnlyItsLine`: a 3-line reader with a synthesized github token on lines 1 and 3 and the marker on line 1 only reports line 3; the marker on a line with two shapes silences both; a marker on a clean line changes nothing.
  - `TestLongLineIsScannedToTheEnd`: a single 200 KB line with a synthesized github token in its last 60 bytes is reported at `:1` — a `bufio.Scanner` with default buffer fails this with `token too long` or a missed hit.
  - `TestReportNeverEchoesTheValue` (property): for each rule, 50 random values of the rule's shape; the concatenation of `Hit.String()`, `Report()` and `ErrHit.Error()` contains no 6-byte window of the value's VARIABLE part; the fixed prefix is allowed and must be present for prefix rules; key-context reports contain `prefix=` followed by nothing; a report that prints `%q` of the line or the match fails on every iteration.
  - `TestHitLocationIsFileLine`: `Scan("notes.md", …)` with the token on line 7 yields `Location=="notes.md"`, `Line==7`, and `String()` contains `notes.md:7`.
  - `TestScanPayloadWalksNestedLeaves`: `ScanPayload("POST /issue", map[string]any{"fields": map[string]any{"description": map[string]any{"content": []any{map[string]any{"text": "<synth token>"}}}}})` reports location `POST /issue:fields.description.content[0].text`; a struct body with a json-tagged `Body` field is reported under the tag name; `ScanPayload("x", nil)` is nil; a body of `[]string{"clean", "<token>"}` reports `x:[1]` — a walk that stops at the top level fails the first row.
  - `TestScanArgvNamesTheFlag`: `ScanArgv("gh", []string{"pr","create","--title","t","--body","l1\n<token>"})` reports `gh --body:2`; a token in a positional (`[]string{"pr","comment","--","<token>"}`) reports `gh argv[3]`.

### t-2 — Walk .dross artifacts; fail validate on hit
- wave: 2 · depends_on: [t-1] · covers: c-3 · status: pending
- files: `internal/cmd/secretscan.go`, `internal/cmd/validate.go`, `internal/cmd/secretscan_scope_test.go`
- description: `listDrossArtifacts(root) ([]string, error)`: when `git rev-parse --is-inside-work-tree` succeeds, `git ls-files -z --cached --others --exclude-standard -- .dross` (tracked PLUS stageable-untracked, honouring .gitignore — B4, B5); otherwise `filepath.WalkDir(.dross)` of every regular file (B6). `scanDrossArtifacts(root) ([]secretscan.Hit, error)` opens each via `pathfence.Contain(root, "dross-artifact", rel)` and the seam, streams it through `secretscan.Scan` with the root-relative path as location; a read or scan error is a refusal, not a skip. validate.go: after the phase loop, every hit's `String()` is appended to `problems` under a `secret:` prefix so the existing `✗`/exit-1 path fires.
- test_contract:
  - `TestValidateRefusesOnArtifactSecret`: temp dir (no git), `Init()`, then `.dross/phases/p/notes.md` with a synthesized gitlab token on line 4 → `runCmd(Validate())` errors, output contains `gitlab-pat at .dross/phases/p/notes.md:4 (len=` and does NOT contain any 6-byte window of the token — proves the non-git fallback walk (B6) and the location format together.
  - `TestScopeIsTrackedPlusStageable`: temp git repo with `ensureDrossGitignore` and the security/quality ignore seeds applied; three files: tracked `.dross/board.json` clean, UNTRACKED `.dross/notes.md` with a token, IGNORED `.dross/security/run1/gitleaks.json` with a token → exactly one hit, at `.dross/notes.md` — drop `--others` and the untracked hit disappears (B4); drop `--exclude-standard` and the ignored one appears (B5).
  - `TestValidateAllowMarkerHonouredInToml`: `.dross/phases/p/spec.toml` line `text = "<synth token>" # dross:allow-secret` → validate exits 0 and prints no `secret:` line; the same file without the marker fails.
  - `TestScanErrorIsARefusalNotASkip`: a `.dross/x.toml` that is a directory-shaped entry the walker cannot read (mode 000 on unix, skipped on Windows) → validate reports `secret scan: … : permission denied` as a problem rather than exiting 0.
  - `TestValidateSecretProblemsAreLast`: a malformed spec.toml AND a token in another phase → both problems print; the secret line is present, so the scan runs even when earlier checks already failed (a scan gated on "no other problems" would hide the token behind a schema error).

### t-3 — Screen forge transports before every request
- wave: 2 · depends_on: [t-1] · covers: c-2 · status: pending
- files: `internal/forge/forge.go`, `internal/forge/github.go`, `internal/forge/jira.go`, `internal/forge/youtrack.go`, `internal/forge/secretscan_test.go`
- description: Wiring only (ScanPayload is t-1's). Insert `if err := secretscan.ScanPayload(method+" "+endpoint, body); err != nil { return err }` as the FIRST statement of `(*Client).doRaw`, `(*GitHubClient).doRaw`, `(*JiraClient).doRaw`, `(*YouTrackClient).doRaw`, ahead of the encoder and `http.NewRequest`. The wrapping `do` still applies `redact.Err`, so the surface string can never carry the token from headers (it is never in the body).
- test_contract:
  - `TestCreateIssueRefusedBeforeRequest` (×4 backends, httptest server counting requests): `CreateIssue(IssueInput{Body: "line1\n<synth atlassian token>"})` returns an error that `errors.As` a `*secretscan.ErrHit`, whose message names `atlassian-token` and `:body` (forge/github/youtrack) or `:fields.description` (jira, inside ADF), and the server's request counter is 0 — move the screen after `http.NewRequest`/`client.Do` and the counter reads 1.
  - `TestUpdateIssueBodyPatchRefused` (×4): `UpdateIssue(key, IssuePatch{Body: &tok})` → refused, 0 requests; `UpdateIssue(key, IssuePatch{Labels: …})` with clean labels → the request goes out (the screen must not refuse label-only patches).
  - `TestEnsureMilestoneDescriptionRefused`: YouTrack `EnsureMilestoneEntity("epic", "v9", "<synth pem header line>")` and Jira `EnsureMilestoneEntity("version", …)` refused with `pem-private-key`, 0 requests — the milestone-description path is a publish path even though no `Body` field is involved.
  - `TestGetAndListAreNotScreened`: `GetIssue`/`ListIssues` against a server whose RESPONSE body carries a synthesized token succeed and return the issue — the screen is on outbound payloads only; a screen applied to responses fails this.
  - `TestPayloadErrorNeverEchoes`: the refusal error string from each backend contains no 6-byte window of the token's variable part; the assertion runs on `err.Error()` after `redact.Err` wrapping, i.e. on the string that would reach telemetry `err_detail` (B2).

### t-4 — Screen ship HTTP and gh argv transports
- wave: 2 · depends_on: [t-1] · covers: c-2 · status: pending
- files: `internal/ship/open.go`, `internal/ship/bitbucket.go`, `internal/ship/gitlab.go`, `internal/ship/secretscan_test.go`
- description: `jsonPost` (open.go:132): `ScanPayload("POST "+endpoint, body)` before the encoder. The bitbucket request func (bitbucket.go:60) and the gitlab request func (gitlab.go:245): same, first statement. `ghCommand` argv (B9): `open.go` gains a package-level `screenedGH(args ...string) (*exec.Cmd, error)` that calls `secretscan.ScanArgv("gh", args)` (t-1's) and only then `ghCommand(args...)`; `openGitHubPR`, `postGitHubComment` and every other `ghCommand(` call site in ship switch to it. The `ghCommand` var stays as the test seam.
- test_contract:
  - `TestOpenPRGitHubRefusedBeforeGh`: `ghCommand` stubbed to record invocations; `OpenPR(OpenOpts{Provider: "github", Body: "<synth github token on line 3>"})` returns `*secretscan.ErrHit` naming `github-token` and `gh --body:3`, and the stub recorded zero invocations — scan after exec and the stub records one.
  - `TestOpenPRTitleIsScreenedToo`: same with the token in `Title` → refused at `gh --title:1`; a gate that screens only Body fails.
  - `TestPostCommentRefused` (github via stub; forgejo, gitlab, bitbucket via httptest counters): `PostComment(CommentOpts{Body: pem header})` → `pem-private-key`, zero requests/invocations on every provider.
  - `TestOpenPRForgejoGitLabBitbucketRefused`: `OpenPR` per HTTP provider with a slack-shaped body → refused, request counter 0 — proves each of the three request funcs carries the screen, not just `jsonPost`.
  - `TestNoRawGhCommandCallOutsideTheSeam` (AST over `internal/ship` non-test files): the only call expression whose callee is the identifier `ghCommand` sits inside `screenedGH`; any other fails naming file:line — this is the local residual for the argv path and is what t-6 later folds into the registry.
  - `TestAllowMarkerOnBodyLine`: a PR body whose token line ends with `dross:allow-secret` posts (stub invoked once) — the marker route works on composed bodies, not only on files.

### t-5 — Gate auto-commit and ship pre-flight
- wave: 3 · depends_on: [t-2] · covers: c-3 · status: pending
- files: `internal/cmd/cleantree.go`, `internal/cmd/ship.go`, `internal/cmd/secretscan_gate_test.go`
- description: `autoCommitDrossDirt` calls `scanDrossArtifacts(repoDir)` BEFORE `git add .dross` and returns `*secretscan.ErrHit` wrapped as `refusing to auto-commit .dross: …` on a hit — the primitive that turns an untracked note into a commit is the one that refuses (B4), which also covers phase complete / phase start / milestone prune callers. ship.go: a new pre-flight step at the top of "3) Pre-flight gates", before the verdict switch and before `autoCommitDrossDirt`, calls `scanDrossArtifacts(root)` and refuses with `secret in tracked .dross artifact — fix it by hand or mark the line dross:allow-secret, then re-run ship` so a CLEAN tree with an already-committed token still refuses (B7). `--print-body` and `--no-push` do not skip it.
- test_contract:
  - `TestShipRefusesCommittedSecretOnCleanTree`: temp git repo with a verified phase whose `.dross/phases/p/notes.md` carrying a synthesized token is COMMITTED (tree clean) → `dross ship p` errors naming `notes.md:<line>` before any `git push` (the push seam records nothing) — a gate placed only inside `autoCommitDrossDirt` passes this test wrongly because the no-dirt early return skips it.
  - `TestAutoCommitRefusesUntrackedSecretNote`: repo with an untracked `.dross/phases/p/notes.md` carrying a token; `autoCommitDrossDirt(repoDir, "shipping")` returns an error, `git status --porcelain` still lists the note as untracked, `git log` has no new `chore(dross)` commit — reorder the scan after `git add` and the index contains the note.
  - `TestAutoCommitStillRefusesCodeDirtFirst`: dirty `main.go` plus a secret note → the error is the existing `dirtyTreeError` (code dirt is checked first so the message the user already knows does not change shape); with only the note → the secret refusal.
  - `TestPhaseCompleteRefusesOnSecret`: `dross phase complete p` on a repo whose `.dross/phases/p/changes.json` note carries a token → refused, `state.json` unchanged, no completion event recorded — proves the cleantree placement reaches the non-ship callers.
  - `TestShipPrintBodyStillScans`: `dross ship p --print-body` with the committed-secret fixture errors instead of printing the body.
  - `TestShipGateOrderingIsBeforeAutoCommit` (AST over ship.go's RunE): the call to `scanDrossArtifacts` has a lower `token.Pos` than the call to `autoCommitDrossDirt` and than every `pushPhaseBranch`/`ship.OpenPR` call — a later refactor that moves the scan below the auto-commit fails here even if the behavioural tests are stubbed.

### t-6 — Transport registry + residual scan
- wave: 3 · depends_on: [t-3, t-4] · covers: c-4 · status: pending
- files: `internal/secretscan/sinks.go`, `internal/secretscan/sinks_test.go`, `internal/cmd/secretscan_transport_enum_test.go`, `internal/cmd/testdata/secretscan/leaky_transport.go.txt`
- description: `sinks.go`: `Transport{Package, Func, Screened *Screened{Call string}, ReadOnly *ReadOnly{Why}}` and `Transports()` listing the nine outbound seams found in the tree today: `forge.(*Client).doRaw`, `forge.(*GitHubClient).doRaw`, `forge.(*JiraClient).doRaw`, `forge.(*YouTrackClient).doRaw`, `ship.jsonPost`, `ship.<bitbucket request func>`, `ship.<gitlab request func>`, `ship.screenedGH` (Screened, Call = `ScanPayload`/`ScanArgv`), `ship.<forgejo GET func at forgejo.go:203>` (ReadOnly: method literal is "GET", nil body). `Validate([]Transport) []error` in the toolfence shape (empty fields, both/neither disposition, duplicate). `secretscan_transport_enum_test.go` walks `../ship` and `../forge` non-test files for every call to `http.NewRequest`, `http.NewRequestWithContext`, `http.Post`, `http.Get`, `(*http.Client).Do`, `exec.Command`, and the `ghCommand` identifier; each site's enclosing function must be a registered Transport; a Screened one must contain a call to `secretscan.ScanPayload`/`ScanArgv` whose `Pos()` precedes the request call; a ReadOnly one must have a literal `"GET"` method argument and a nil body argument. Stale arm: every registered Func resolves to a FuncDecl.
- test_contract:
  - `TestEveryOutboundSeamIsRegistered`: over the live tree, the walk resolves ≥ 9 transport sites across BOTH `../ship` and `../forge` (per-package vacuity floor, as toolfence_composer does per group) and reports none undeclared — add `http.Post(…)` anywhere in ship or forge and this names the file:line.
  - `TestScreenPrecedesRequestInEveryScreenedTransport`: for each Screened entry, `Pos(ScanPayload call) < Pos(http.NewRequest | ghCommand call)` inside that FuncDecl — swap the two statements in any doRaw and this fails naming the function.
  - `TestReadOnlyTransportsSendNoBody`: the forgejo GET func's `http.NewRequest` call has a `"GET"` literal first arg and `nil` third arg — change either and the ReadOnly declaration is rejected as false.
  - `TestTransportScanTripsOnTheLeakyFixture`: `leaky_transport.go.txt` holds (a) a func that calls `http.NewRequest("POST", …)` with no screen, (b) a func with the screen AFTER the request, (c) a clean screened func, (d) a `ghCommand(` call outside `screenedGH`; the scan must flag a, b and d and pass c — calibrated on a written-down answer, not on the tree's post-fix state.
  - `TestStaleTransportDeclarationFails`: registry entry naming a func that does not exist is reported by the stale arm (fed synthetically, so the assertion is not vacuous against a clean registry).
  - `TestTransportRegistryValidate` (in `sinks_test.go`): synthetic entries with no disposition, both dispositions, empty Func, and a duplicate each produce one specific error.

### t-7 — Writer registry + residual scan
- wave: 3 · depends_on: [t-2] · covers: c-4 · status: pending
- files: `internal/secretscan/writers.go`, `internal/cmd/secretscan_writer_enum_test.go`, `internal/cmd/testdata/secretscan/unregistered_writer.go.txt`
- description: `writers.go`: `Writer{File string; UnderDross *UnderDross{Artifacts []string}; MachineLocal *MachineLocal{IgnoreSeed, Why}; OutsideDross *OutsideDross{Why}}` and `Writers()` — one entry per non-test file in `internal/` that calls `os.WriteFile`, `os.Create`, `os.OpenFile` (write flags), or `pathfence.WriteFile` (≈35 files today: board, changes, phase, milestone, verify, rules, project, profile, defaults, survivor, reaplog, watch, security/quality scaffold+findings, cmd/{pause,local,init,hooks,env,architecture,statusline,root,install,gitignore,gitattributes,redproof_repoint,security,quality}, telemetry, techdebt, compilefence, pathfence itself). UnderDross entries are covered by t-2's location-based scope; MachineLocal entries (state.json, local.toml, handoff.md, `security/`, `quality/`) name the ignore seed that keeps them out of git; OutsideDross entries (ARCHITECTURE.md, README, ~/.claude, hooks settings, telemetry.jsonl, install symlinks, gitignore/gitattributes) say why they are not a `.dross` artifact. The enum test walks `internal/` for the write verbs and requires the enclosing FILE to be declared; stale arm: a declared file with no write verb fails; MachineLocal arm: in a temp repo after `ensureDrossGitignore` + the security/quality scaffolds, `git check-ignore` accepts each declared path; UnderDross arm: for every declared artifact name, a temp `.dross/<plausible path>/<name>` holding a synthesized token is reported by `scanDrossArtifacts`.
- test_contract:
  - `TestEveryDrossWriterIsDeclared`: live walk resolves ≥ 30 writer files and every one is in `Writers()`; add `os.WriteFile` to any undeclared file and this names it.
  - `TestMachineLocalWritersAreReallyIgnored`: for each MachineLocal entry, `git check-ignore -q .dross/<path>` exits 0 in the seeded temp repo — remove `handoff.md` from the seeded ignore block and its entry fails as a false claim.
  - `TestUnderDrossArtifactsAreInScanScope`: for each UnderDross artifact name (board.json, changes.json, spec.toml, plan.toml, verify.toml, tests.json, survivors.toml, deferred.toml, milestones/*.toml, rules.toml, project.toml, red-proof docs, reap-log.json, watch.state.json…), a file of that name with a token at a plausible depth is reported with `file:line` by `scanDrossArtifacts` in BOTH the git and non-git walker modes — an artifact name the walker filtered out (e.g. by extension) fails here.
  - `TestWriterScanTripsOnTheFixture`: `unregistered_writer.go.txt` with an `os.WriteFile` in a file name absent from the registry is flagged; the fixture's second func using `os.ReadFile` only is not.
  - `TestStaleWriterDeclarationFails`: a synthetic entry naming `internal/nothing/here.go` is reported by the stale arm.

### t-8 — Self-scan the tree; pin the marker set
- wave: 4 · depends_on: [t-1, t-2, t-3, t-4, t-5, t-6, t-7] · covers: c-6 · status: pending
- files: `internal/cmd/secretscan_selfscan_test.go`, `internal/cmd/env_test.go`, `internal/cmd/hermetic_env_test.go`, `internal/forge/hostallow_test.go`, `internal/forge/hostile_config_test.go`, `internal/argfence/policy.go`, `internal/security/gitleaks_test.go`, `.dross/phases/scanner-self-exclusion/panel/risk.md`, `.dross/phases/scanner-self-exclusion/panel/synthesis.md`
- description: Append ` // dross:allow-secret` (Go) or ` dross:allow-secret` (md) to the lines that are genuinely secret-shaped fixtures and that the rules from t-1 fire on: `env_test.go:31` (FORGEJO_TOKEN fixture), `hermetic_env_test.go:146` (absent-token sentinel), `hostallow_test.go:25`, `hostile_config_test.go:60` (sentinel tokens), `argfence/policy.go:69` (the `Token:` field holding the git end-of-options sentinel — exactly 16 chars, entropy 3.08), `gitleaks_test.go:86-87` (password/token-context 16-hex rows), and the two panel notes carrying the same rows. The AWS doc placeholder sites need no marker (t-1's EXAMPLE carve-out). The self-scan test runs `git ls-files -z` at the repo root (skip with a message if not a git checkout), scans every file through `secretscan.Scan`, and asserts zero hits; a second assertion collects every line where the marker actually SILENCED a hit (re-scan the line with the marker stripped; an inert marker in prose — this panel note mentions it five times — does not count) and compares the multiset of `<basename>` → count against a pinned table, so a new silencing marker anywhere is a deliberate edit to the test.
- test_contract:
  - `TestDrossTreeHasNoUnmarkedHits`: every tracked file scans clean; the failure message lists `file:line rule (len, prefix)` for each hit and never the line — add a literal token-shaped string to any test and this fails; remove the EXAMPLE carve-out and it fails on the nine AWS placeholder sites.
  - `TestAllowMarkerSitesArePinned`: silencing markers found (marker stripped → line hits) == {env_test.go:1, hermetic_env_test.go:1, hostallow_test.go:1, hostile_config_test.go:1, policy.go:1, gitleaks_test.go:2, risk.md:1, synthesis.md:1} plus the ones this phase's own artifacts need; one more or one fewer fails naming the file.
  - `TestMarkerNeverAppearsInNonTestGoOutsideArgfence`: the only non-test `.go` file carrying the marker is `internal/argfence/policy.go` — the marker must not become a habit in production code.
  - `TestSelfScanCoversTheWholeTreeNotJustDross`: the scan visited ≥ 1500 files and ≥ 1 file outside `.dross/` (vacuity floor), so the c-6 claim is over "dross's own tree", not the artifact subset.
  - `TestValidateOnDrossItself`: `runCmd(Validate())` from the real repo root exits 0 with no `secret:` line — the live gate agrees with the test walk.

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-3 (board issue body/patch/milestone description, four backends), t-4 (PR body, PR title, PR comment, gh argv) |
| c-3 | t-2 (validate + scope), t-5 (auto-commit + ship pre-flight, clean-tree case) |
| c-4 | t-6 (publish composers, via the transport residual), t-7 (.dross writers) |
| c-5 | t-1 (report format + echo property), t-3/t-4 (error strings after redact), t-8 (live failure messages) |
| c-6 | t-8 |

## Judgment calls

- **Gate at the transport seam, not at composers or a BoardClient wrapper.** Rejected wrapper: `issue.go` type-asserts `*forge.YouTrackClient`/`*forge.JiraClient` at six sites, so a decorator returned from `NewBoard` breaks those paths. Rejected per-method screens: 14 hand-listed sites is exactly what c-4 forbids. `doRaw`/`jsonPost`/`screenedGH` are nine functions, enumerable by "who calls http.NewRequest / exec.Command", and every composer reaches the network through one of them by construction.
- **Scan the marshalled JSON leaves, not the `Body` string.** Jira wraps bodies in ADF and GitHub Projects sends GraphQL; screening `in.Body` would miss a composer that builds the map itself. Round-tripping through `json` costs microseconds and scans titles, descriptions and label names for free.
- **Also gate `autoCommitDrossDirt`, not only validate + ship.** The spec names validate and ship pre-flight; the commit-creating primitive is `autoCommitDrossDirt`, which ship, phase complete, phase start and milestone prune all call, and which stages UNTRACKED `.dross` files. Scanning only tracked files at validate time then letting `git add .dross` stage the note is bypass B4. The locked hit_disposition ("refuses the gate") is honoured — nothing is scrubbed.
- **Ship scans explicitly before the auto-commit, in addition.** `autoCommitDrossDirt` returns early on a clean tree, so a token committed earlier (by hand, or by an agent's own `git commit`) would ship. B7 is the case that matters most for "cannot be shipped".
- **Scope = tracked ∪ untracked-unignored (`ls-files --cached --others --exclude-standard`).** Rejected tracked-only (B4) and walk-everything (B5: `.dross/security/<run>/` holds gitleaks' real findings and would make every secure run refuse ship). Non-git fallback walks everything because every existing validate test runs in a bare temp dir.
- **Identity carve-out reuses `security.IdentityIDAllowlist` (id|key context), not a value-only 16-hex exemption.** One definition of "the identity-id shape" in the tree; gitleaks_test already pins that password-context 16-hex is NOT that shape. Cost: four historical lines (two in gitleaks_test.go, two in old panel notes) need the marker. Value-only would have been marker-free but would exempt a real 16-hex password — the locked decision says the identity shape is the carve-out, not "any 16-hex".
- **AWS rule carves out values ending `EXAMPLE`** (AWS's documented placeholder; gitleaks' default aws rule does the same). Nine sites in the tree carry it. Rejected: nine markers in historical artifacts for a shape that is never a credential.
- **Entropy floor 3.0 bits/char on key-context values, no higher.** A floor that silences the tree's sentinel fixtures (3.6–4.0) also silences `MyP@ssw0rd!2024xyz` (3.73). The floor exists to kill `aaaa…`/`hunter2hunter2` placeholders; secret-shaped fixtures get the marker, which is the locked allowlist route and keeps the exemption next to the evidence.
- **Hit corpus synthesized at runtime, never literal.** Otherwise c-6's self-scan trips on the tests that prove c-1, and the fix would be a testdata carve-out — a path exclusion that then hides a real leak in a fixture file.
- **c-6 reads "own tree" as every tracked file, not just `.dross/`.** The stronger reading is the one worth pinning, and it is what makes the marker set a deliberate, counted list rather than an accretion.
- **Marker sites are pinned by count in the self-scan test.** A marker is a silenced finding; growth without an edit to the pinned table is exactly the "silently disable a rule" failure the allowlist_route lock rejects for regex allowlists.
- **No docs/prompt task.** The refusal message names the marker and the remedy; nothing in the spec asks for README or prompt text, and the phase already has eight tasks.
