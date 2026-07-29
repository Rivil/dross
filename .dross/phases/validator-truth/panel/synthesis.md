# validator-truth — panel synthesis

Judge notes: I authored none of the three drafts. Where a claim was checkable
against the repo I checked it, and those checks are cited inline — they are what
broke several ties.

## Scores

Scale: ✅ strong / ◑ adequate / ✗ weak.

| dimension | risk (10 tasks / 3 waves) | mvp (4 / 3) | verification (10 / 3) |
|---|---|---|---|
| **criteria coverage** | ✅ all six; richest c-6 spread (t-2/t-3/t-4/t-8/t-9), but c-1 and c-3 each hang on one task, and no task teaches ship's own Basic-auth *and* forge's — it is the only draft that covers forge Basic auth at all (t-5) | ◑ all six on paper, but c-1/c-2/c-4/c-5/c-6 all land in the single task t-3, so one commit is the sole evidence for five criteria; no forge Basic auth despite locked `bitbucket_auth` | ✅ all six, and the only draft with genuine double coverage (c-1 = t-1+t-6, c-3 = t-1+t-10); adds a jira-without-`board.auth_user` warning grounded in `internal/forge/jira.go:55`. Gap: also never teaches forge Basic auth |
| **test-contract specificity** | ✅ most adversarial: negative-header assertions ("no `PRIVATE-TOKEN`, no `Authorization: token`"), reviewer-4xx must return *both* a non-nil `*OpenResult` and an error, a synthetic provider that MUST return the sentinel. Weakness: contracts describe assertions but rarely name a test function or file, so "done" is not nameable | ◑ best *format* discipline — every bullet is "doing X fails `TestY` (file)". But density is far too low for the task size: an entire three-verb Bitbucket backend plus a cross-package de-literalisation is pinned by five bullets | ✅ strongest overall: names test, file, assertion, *and* the current source line that proves it fails today (`doctor.go:136`, `youtrack.go:185`, `jira.go:231`, `forge.go:140`). Uniquely guards its own guard (`TestProviderCasesInParsesFixture`, `TestGuardsSeeNonEmptySets`) |
| **granularity** | ◑ one failure mode per task is honestly applied, but t-2 and t-4 carry five files each and t-2 reaches into `internal/ship/open.go`+`comment.go` in wave 1 — files t-3 and t-4 also own | ✗ weakest. t-2 = a whole ship backend across three verbs *plus* a forge/ship refactor; t-3 = doctor + issue + five criteria. Neither has an atomic-commit seam, which is what `dross-execute` commits on | ✅ best calibrated: no task exceeds four files; the smallest (t-5, thread `auth_user` through the opt builders) earns its place by naming the exact production bug it catches |
| **wave correctness** | ◑ wave 1 parallelism is real (registry ∥ Bitbucket wire), but three tasks across two waves write `internal/ship/open.go`/`comment.go` — t-2, t-3, t-4 | ◑ dependency arrows are correct; the wave structure buys nothing, since a wave of two same-layer tasks has no parallelism to exploit | ✅ cleanest file ownership. Wave 1 is three genuinely independent tasks; the two `AuthUser` fields (`project.Remote` in t-2, `ship.OpenOpts` in t-3) are deliberately split into different packages and *joined* at t-5, whose contract fails if they never meet |

**Skeleton: `verification`.** It scores highest on three of four dimensions and
ties on the fourth. Two things decided it beyond the table. First, its contracts
are anchored to real line numbers, and every anchor I spot-checked held —
`doctor.go:117` really does accept only `forgejo | gitea | gitlab | youtrack`,
`doctor.go:96` really does accept only `private-token | bearer`. A plan written
against the actual source is cheaper to execute than one written against a
remembered one. Second, it is the only draft that treats its own divergence
guard as capable of passing vacuously and pins that shut — and I confirmed the
subtle contract it depends on is correct: `forge.NewBoard` has *no* default arm
(it falls through to `New`), so the guard must compare the **union** of `New`'s
and `NewBoard`'s case literals against `BoardProviders`, which is exactly what
its t-10 says. That is not a detail a plan gets right by accident.

## Merged plan

10 tasks across 3 waves. All `status: pending`. Origin tag = which lens the task
and its grafted contracts came from.

### Wave 1

**t-1 — Add `internal/configenum` package of normalising enum sets** `[verification+risk]`
- files: `internal/configenum/configenum.go`, `internal/configenum/configenum_test.go`
- covers: c-2, c-4 · depends: —
- Leaf package, zero internal imports. Exports `Set` (`Values`, `Normalize`, `Has`, `List`) plus `BoardProviders {forgejo,gitea,gitlab,youtrack,jira,github}`, `MilestoneModes {version,agile,epic}`, `AuthSchemes {private-token,bearer,basic}`, `ShipProviders {github,forgejo,gitea,gitlab,bitbucket}`, and the two derived helpers `MilestoneModesFor(provider)` and `BoardRequiresBaseURL(provider)`.
- contract (verification): `TestSetNormalizeTrimsAndLowercases`, `TestEmptyIsAcceptedAsDefault` (the three empty-value policies pinned *separately* — collapsing them into one rule fails), `TestAuthSchemesIncludeBasic`, `TestShipProvidersIncludeBitbucket`, `TestMilestoneModesForProvider` (`jira → ["version"]`, `youtrack →` all three, `gitlab → nil`), `TestBoardRequiresBaseURL` (false for github only).
- **grafted from risk:** `TestSetListRendersPipeJoined` asserts the exact string `"forgejo | gitea | gitlab | youtrack | jira | github"` — the literal that doctor/issue/forge messages interpolate, so no call site can hand-type the set back in.

**t-2 — Add `remote.auth_user` and the `basic` auth scheme to the schema** `[verification]`
- files: `internal/project/project.go`, `internal/cmd/project.go`, `internal/cmd/project_test.go`
- covers: c-2 · depends: —
- `AuthUser string \`toml:"auth_user,omitempty"\`` on `project.Remote`, mirroring the existing `Board.AuthUser` (`project.go:117`), plus `remote.auth_user` arms in `readDotted`/`writeDotted` alongside the existing `board.auth_user` arms at `cmd/project.go:193,350`. Doc comments on `Remote.Provider`/`Remote.AuthScheme` updated to the real sets.
- contract: `TestRemoteAuthUserRoundTrip`; `TestRemoteAuthSchemeBasicRoundTrips`; `TestUnknownRemoteDottedKeyStillRejected` (`remote.auth_users` typo still errors — proves an arm was added, not a catch-all).

**t-3 — Add Bitbucket PR-open backend over Basic auth** `[verification+risk]`
- files: `internal/ship/bitbucket.go`, `internal/ship/open.go`, `internal/ship/bitbucket_test.go`
- covers: c-6 · depends: —
- New `bitbucket.go` transport: `bbBasicAuth`, `bbRepoRef(url) → workspace/slug`, `bbRequest`, `openBitbucketPR`. `open.go` gains `AuthUser` on `OpenOpts`, a `case "bitbucket"` arm, and bitbucket in its default-branch message.
- Endpoints are **relative to `api_base`**, which `internal/project/remote.go:44` already autodetects as `https://api.bitbucket.org/2.0` — so `/repositories/{ws}/{repo}/pullrequests`, never `/2.0/repositories/...`.
- contract (verification): `TestOpenBitbucketPRHappyPath` (asserts `source.branch.name`/`destination.branch.name` nesting — GitHub-style flat `head`/`base` fails; result read from `links.html.href`, not `html_url`), `TestOpenBitbucketPRDraftPrefix`, `TestOpenBitbucketPRMissingAuthUser` (error names `[remote].auth_user`, **zero HTTP requests**), `TestOpenBitbucketPRMissingToken`, `TestOpenBitbucketPRSurfacesHTTPError`, `TestBBRepoRef`, `TestOpenPRRejectsUnknownProvider` extended.
- **grafted from risk:** two contracts verification omits — (a) the Authorization header assertion is *negative as well as positive*: the decoded credential is `user:token` **and** no `PRIVATE-TOKEN` / `Authorization: token …` header is sent; (b) a reviewer assignment that 4xxs returns **both** a non-nil `*OpenResult` and a non-nil error, mirroring the existing forgejo/gitlab non-fatal contract, so a reviewer failure cannot lose an already-open PR.

### Wave 2

**t-4 — Add Bitbucket comment and merge-status backends** `[verification+risk]`
- files: `internal/ship/comment.go`, `internal/ship/merged.go`, `internal/ship/bitbucket_comment_test.go`
- covers: c-6 · depends: t-3
- `postBitbucketComment` and `bitbucketPRMerged` reuse t-3's `bbRequest`/`bbBasicAuth`. `merged.go` answers authoritatively — it does **not** join the `ErrMergeStatusUnsupported` arm at `merged.go:27`.
- contract (verification): `TestPostBitbucketCommentHappyPath` (`{"content":{"raw":…}}`; a flat `{"body":…}` or a Forgejo-shaped `/issues/{n}/comments` path fails), `TestPostBitbucketCommentMissingAuthUser`, `TestPRMergedBitbucketTrue`, `TestPRMergedBitbucketNeedsPRNumber`, `TestPRMergedUnsupportedProvider` extended so bitbucket is *absent* from the unsupported table.
- **grafted from risk:** the merged-state assertion is enumerated, not binary — `true` only for `"MERGED"`, `false` for `"OPEN"`, `"DECLINED"` and `"SUPERSEDED"`. A naive `!= "OPEN"` implementation passes verification's two-case test and fails this one.

**t-5 — Thread `auth_user` through the ship opt builders** `[verification]`
- files: `internal/cmd/ship.go`, `internal/cmd/ship_test.go`
- covers: c-6 · depends: t-2, t-3
- `buildOpenOpts`/`buildCommentOpts` copy `p.Remote.AuthUser` onto `OpenOpts.AuthUser`/`CommentOpts.AuthUser` beside the existing `AuthScheme`/`ProjectID` wiring.
- contract: `TestBuildOpenOptsCarriesAuthUser`, `TestBuildCommentOptsCarriesAuthUser`. This is the join point for the two independently-added `AuthUser` fields; dropping it is exactly the bug where every ship-package test passes and every real Bitbucket ship 401s.

**t-6 — Route doctor's enum checks through configenum** `[verification+mvp]`
- files: `internal/cmd/doctor.go`, `internal/cmd/doctor_test.go`
- covers: c-1, c-2, c-4 · depends: t-1
- Replaces the three inline literal switches (`doctor.go:96` auth_scheme, `:117` board provider, `:140` milestone_mode) with `configenum.Set.Has` after `Normalize`, and the hand-typed `(expected a | b)` strings with `Set.List()`. `base_url` required only when `BoardRequiresBaseURL(provider)`.
- contract (verification): `TestDoctorAcceptsEveryDispatchableBoardProvider` (table over `BoardProviders.Values`; fails today for `jira` and `github` — direct proof of c-1, and a seventh backend added without teaching doctor also fails here), `TestDoctorBoardBaseURLOptionalForGitHub` (github empty base_url → exit 0; youtrack empty base_url → still ✗ and non-zero, so the relaxation cannot leak), `TestDoctorNormalisesMilestoneMode`, `TestDoctorNormalisesAuthScheme` (`basic` → 0, `token` → non-zero, message contains `private-token | bearer | basic`), `TestDoctorEnumMessagesComeFromConfigenum`.
- **grafted from mvp:** `TestDoctorCarriesNoProviderLiterals` — asserts `doctor.go` contains no residual provider/mode/scheme string literal. Stronger than "the message contains `List()`": it fails a partial migration that leaves one switch behind while the message is already derived.

**t-7 — Route issue enable + forge dispatch through configenum, and teach forge Basic auth** `[verification+risk]`
- files: `internal/cmd/issue.go`, `internal/forge/forge.go`, `internal/cmd/issue_test.go`, `internal/forge/forge_test.go`
- covers: c-2, c-6 · depends: t-1
- `issue.go`'s recognition list and its two hard-coded note strings become `BoardProviders.Has`/`.List()`. `forge.New`'s `"expected forgejo | gitea | gitlab"` message (`forge.go:78`) is generated from the set restricted to the REST backends.
- contract (verification): `TestIssueEnableAcceptsEveryBoardProvider`, `TestIssueEnableNoteListsConfigenumSet`, `TestIssueEnableNormalisesProvider`, `TestNewValidation` extended — and note the existing `forge_test.go:43` row already asserts `bitbucket` is *not* a board provider, so a task that mistakenly adds bitbucket to `BoardProviders` fails here for free.
- **grafted from risk (t-5) — the skeleton's one real coverage hole.** Locked decision `bitbucket_auth` says "doctor, forge and ship all learn it", but neither the skeleton nor mvp ever teaches **forge** the `basic` scheme; only risk does. Grafted: `Client.do` sends `Authorization: Basic base64(AuthUser:token)` when `Normalize(authScheme) == "basic"`, with contracts — the httptest server sees the Basic header and **no** `PRIVATE-TOKEN` (flipping the scheme back fails it), and `basic` with an empty `AuthUser` returns a construction error naming `auth_user` rather than silently sending `Basic base64(:token)`.

**t-8 — Correct the remote-provider lists in the init/onboard prompts** `[verification+risk]`
- files: `assets/prompts/init.md`, `assets/prompts/onboard.md`, `README.md`, `internal/cmd/prompt_provider_list_test.go`
- covers: c-6 · depends: t-1
- Confirmed present today: `init.md:54` and `onboard.md:62` both read `github / forgejo / gitea / bitbucket / none` — gitlab missing, bitbucket present. That is the writer/dispatcher inversion c-6 names. Both bullets gain gitlab (bitbucket retained, now implemented) plus the `gitlab.com → gitlab` / `api/v4` autodetect hint. Locked `prompt_provider_lists`.
- contract: `TestPromptProviderListsMatchShipProviders` (parses the bullet, strips `none`, set-equality against `ShipProviders.Values`; fails today), `TestPromptProviderBulletFound` (exactly one bullet per prompt, so a renamed heading fails loudly instead of passing on zero tokens).
- **grafted from risk:** `README.md` added to the file list — but *scoped*, see disagreement D7. Rule r-01: `make install` after the asset edits before any manual re-run.

### Wave 3

**t-9 — Add doctor cross-field combination warnings** `[verification+mvp]`
- files: `internal/cmd/doctor.go`, `internal/cmd/doctor_test.go`
- covers: c-5, c-6 · depends: t-6
- A `warnings` counter reported but never added to `issues`, so exit stays 0 (locked `new_check_severity`). Three checks: (a) milestone_mode outside `MilestoneModesFor(board provider)` → ⚠; (b) `provider=jira` with `board.auth_user` empty → ⚠ (`NewJira` hard-requires it, `jira.go:55`); (c) `[remote].provider` set but outside `ShipProviders` → ⚠.
- contract (verification): `TestDoctorWarnsJiraEpicCombination` (⚠ + **nil error**; youtrack + epic produces no ⚠), `TestDoctorCombinationWarningKeepsExitZero` (fails if the implementation does `issues++` — the regression guard for the locked severity), `TestDoctorWarnsJiraMissingAuthUser`, `TestDoctorWarnsUnshippableRemoteProvider` (bitbucket → no warning after t-3/t-4; sourcehut → ⚠ containing `ShipProviders.List()`), `TestDoctorHardFailureStillNonZero`.
- **grafted from mvp:** empty and `"none"` `[remote].provider` are **silent**, not warned — see D6.

**t-10 — Add the validator/dispatch divergence guard** `[verification+mvp]`
- files: `internal/cmd/enum_divergence.go`, `internal/cmd/enum_divergence_test.go`
- covers: c-3 · depends: t-3, t-4, t-6, t-7
- A `go/ast` walker `providerCasesIn(file, funcName)` collects the string literals of a named function's provider switch; the test compares those sets against configenum. Scans `internal/ship/{open,comment,merged}.go` and `internal/forge/forge.go`, resolving the root via the existing `repoRootFromTest` helper (`commands_parity_test.go:54`). File-pair layout mirrors `interaction_coverage.go`/`_test.go`; note that helper is string-based, so the `go/ast` walker is new machinery in this package.
- contract: `TestShipDispatchMatchesShipProviders` (each of `OpenPR`/`PostComment`/`PRMerged` equals `ShipProviders.Values` as a set — implementing bitbucket in `OpenPR` but forgetting `PostComment` fails *naming the function*), `TestBoardDispatchMatchesBoardProviders` (**union** of `New` and `NewBoard`, because `NewBoard` has no default arm and falls through to `New` — verified at `forge.go:141-148`), `TestProviderCasesInParsesFixture` (the extractor tested on an in-test source string, so a broken walker cannot make both guards pass vacuously), `TestGuardsSeeNonEmptySets` (≥3 literals per scanned function).
- **grafted from mvp:** the guard also asserts the `assets/prompts` bullet sets — deliberately *not* moved here; that assertion stays in t-8 where the prompts are edited. Recorded only to note it was considered and placed.

`dross validate` is untouched by every task, per locked `enum_validator_home`.

## Disagreements

### D1 — Where the seam falls between "consolidate the validators" and "build a Bitbucket backend" (4 vs 10 vs 10 tasks)

This is the headline divergence, and the size spread is a symptom of it rather
than the thing itself. All three drafts agree the phase contains two jobs: a
small, mostly-mechanical enum-consolidation across doctor/issue/forge/ship, and
a from-scratch Bitbucket ship backend that the locked `bitbucket_convergence`
decision itself calls "peer to gitlab-ship-provider".

- **mvp** puts the seam *nowhere*: t-2 fuses the entire three-verb Bitbucket backend with the cross-package de-literalisation of forge and ship dispatch, on the reasoning that "both touch the same switch statements; splitting would mean two commits editing the same lines".
- **risk** cuts the seam *by layer*: the Bitbucket wire (t-3, httptest-only, zero registry dependency) is separated from the dispatch wiring that routes to it (t-4), so the wire work parallelises with the registry in wave 1.
- **verification** cuts the seam *by verb*: Bitbucket open (t-3) then comment + merged (t-4), joined by the shared `bbRequest` transport, with the enum consolidation living entirely in separate tasks.

**Provisional default: keep them as separate task families, split by verb (verification), with risk's layer insight preserved by leaving t-3 dependency-free in wave 1.** mvp's reasoning is the one I actively reject: "two commits editing the same lines" is not a cost in a repo whose execution model is atomic-commit-per-task — it is the normal shape of sequenced work. The real cost of fusing them is that a five-criteria commit has no partial-failure story; if the Bitbucket transport is wrong, the enum consolidation cannot land either.

**Why it matters:** this determines whether a stalled Bitbucket backend blocks c-1/c-2/c-4 — the cheap, high-value truth fixes — from shipping at all. Under the merged plan, t-1/t-2/t-6/t-7 deliver four of six criteria with no Bitbucket dependency whatsoever. If the phase runs long, that is the natural cut line, and it exists only because the seam was cut. **If the executor disagrees, the cheapest alternative is mvp's fusion, and it should be taken deliberately, not by drift.**

### D2 — Home of the enum registry: `internal/project` vs a new leaf package

- **mvp**: `internal/project/enums.go` — "project imports nothing internal, so forge/ship/cmd can all depend on it without a cycle; a new package would be structure with no criterion behind it."
- **risk**: new `internal/enums`.
- **verification**: new `internal/configenum`.

Both halves of the argument are factually right, which is what makes this a real
disagreement rather than an error. I verified that `internal/project` imports
only stdlib + BurntSushi/toml, so mvp is correct that there is no cycle. I also
verified that **neither `internal/forge` nor `internal/ship` currently imports
`internal/project` at all** — zero hits — which is what risk and verification
mean by "they deliberately keep coupling flat" (they duplicate `splitOwnerRepo`
rather than import it).

**Provisional default: new leaf package, named `internal/configenum` (verification's name).** The deciding factor is not cycles but blast radius: routing forge and ship through `internal/project` would make the two lowest-level wire packages depend on the full TOML schema type for the first time, to obtain six string slices. That is a larger architectural change than this phase's criteria ask for. Name taken from verification over risk's `internal/enums` because `enums` is generic enough to attract unrelated enumerations later; `configenum` states the scope.

**Why it matters:** it is a one-way door in practice. Every task in waves 2-3 imports this package by name, and moving it later is a repo-wide rename touching ~8 files.

### D3 — How the divergence guard (c-3) actually detects divergence

This is the sharpest *technical* disagreement, and both sides identified the
other's failure mode correctly.

- **verification** and **mvp**: scan the source for the `case "…"` literals — verification with a `go/ast` walker, mvp with file reads in the `commands_parity_test.go` style — and compare against configenum. verification's stated rejection of the alternative: a runtime registry "makes the sets agree by construction, which sounds better but silently redefines c-3 into a tautology — the test could never fail."
- **risk**: call the real `ship.OpenPR`/`PostComment`/`PRMerged` and `forge.NewBoard` with deliberately incomplete config and assert on an exported `ErrUnsupportedProvider` sentinel. Its stated rejection of the alternative: "a slice-vs-slice test passes while the switch is wrong."

**Provisional default: verification's `go/ast` source scan.** Two reasons. First, risk's runtime guard depends on every backend tripping a config guard *before* touching the network, and that is fragile in a way the draft does not acknowledge: `PRMerged` for `github` shells out to `gh pr view` via the **unexported** `ghCommand` seam, which a test in package `cmd` cannot stub — it survives only because `PRNumber <= 0` errors first (`merged.go:44`). The guard would silently become an exec-attempting test the day that ordering changes. Second, risk's own critique ("a slice-vs-slice test passes while the switch is wrong") does not land against an *ast scan of the switch itself*, which reads the case labels rather than a parallel slice.

**But risk's anti-tautology point is the correct standard, and the merged plan keeps it** — verification independently satisfies it via `TestProviderCasesInParsesFixture` and `TestGuardsSeeNonEmptySets`, which is what a source scan needs in order to be non-vacuous.

**Why it matters:** c-3 is the whole point of the phase — a guard that cannot fail converts this phase from "stop doctor lying" into "add a test that says doctor never lies". The two mechanisms also have different lifetimes: the ast guard breaks loudly if anyone refactors dispatch to a map (which `TestGuardsSeeNonEmptySets` explicitly wants); the sentinel guard would survive that refactor. **If the codebase later moves to map-driven dispatch, revisit toward risk's approach — and note that doing so requires the exported `ErrUnsupportedProvider` sentinel in both `ship` and `forge`, which only risk's plan adds.**

### D4 — Does forge learn Basic auth in this phase?

- **risk** (t-5): yes — `Client.do` sends `Authorization: Basic base64(AuthUser:token)` when the scheme normalises to `basic`, with a construction error when `AuthUser` is empty.
- **mvp** and **verification**: no task teaches forge the new scheme. mvp adds `basic` to the enum and doctor accepts it; verification adds it to `AuthSchemes` and doctor accepts it. Both leave forge's wire behaviour untouched.

**Provisional default: risk is right — grafted into t-7.** Locked decision `bitbucket_auth` says in as many words: "doctor, forge and ship all learn it." Two of three drafts silently dropped the forge third. Without it, `remote.auth_scheme = "basic"` becomes a value doctor cheerfully accepts and forge cannot dispatch — which is the *precise* failure shape this phase exists to eliminate, reintroduced by the fix.

**Why it matters:** it is the only place where the skeleton, taken as written, would have shipped a new instance of the phase's own bug. It is also the graft with the largest chance of being dropped during execution, because it is the one contract that lives in a file (`internal/forge/forge.go`) that the surrounding task otherwise only touches for a message string.

### D5 — Where `remote.auth_user` lands, and when

- **mvp**: folded into t-1 alongside the enum package.
- **risk**: t-2, bundled with the `ship.OpenOpts`/`CommentOpts` field additions — five files spanning `project`, `cmd` and `ship`. Its stated reasoning: splitting the passthrough out would "split one risk — auth_user never reaches the wire — across two owners, which is exactly what this lens forbids."
- **verification**: split three ways — schema in t-2 (wave 1), `OpenOpts.AuthUser` in t-3 (wave 1), and t-5 as the explicit join whose contract fails if the halves never meet.

**Provisional default: verification's three-way split.** risk's concern is legitimate and verification answers it directly rather than ignoring it: t-5 exists *solely* to own the "never reaches the wire" risk, and its contract names that bug in terms ("this is the test that catches it"). The split additionally lets t-2 and t-3 run in parallel in wave 1, whereas risk's bundling puts `internal/ship/open.go` and `comment.go` under two wave-1 tasks (t-2 and t-3) plus a wave-2 task (t-4) — three owners for two files, the concrete file-ownership problem that cost risk its wave-correctness score.

**Why it matters:** a dropped `AuthUser` passthrough is invisible to every package-local test and fails only against live Bitbucket. Whichever structure is chosen, the join task must exist by name.

### D6 — Should `[remote].provider = "none"` produce a doctor warning?

- **risk** (t-9): yes — `"none"` and `"sourcehut"` each print a ⚠ naming ship.
- **mvp** (t-3): no — explicitly "empty and `none` stay silent"; "a repo with no remote is not misconfigured".
- **verification** (t-9): unspecified — the check reads "`[remote].provider` set but not in `ShipProviders`", which literally includes `"none"`.

**Provisional default: mvp — empty and `"none"` are silent.** `none` is a documented legal value of the field (`project.go:98` lists `forgejo | github | gitea | gitlab | bitbucket | none`) and both prompt bullets offer it as a choice. Warning on a value the tool tells the user to write is the same category of lie this phase is fixing, pointed the other way.

**Why it matters:** it is small but it is the difference between a warning tier people read and one they learn to ignore. It also silently applies to `dross doctor` runs in *every* repo without a remote, which is the widest blast radius of any check in the phase.

### D7 — Scope of the writer-side correction: prompts only, or prompts + README?

- **risk** (t-8): includes `README.md` — "the set of ship providers is user-facing truth and the previous milestone closed on a readme-truth pass; leaving bitbucket undocumented recreates the same class of lie this phase exists to kill."
- **mvp** (t-4) and **verification** (t-8): prompts only.

I checked, and the picture is more mixed than risk's phrasing implies. The
README's ship row (`README.md:204`) carries **no** provider list at all, so
there is nothing there to correct. The provider claims live in
already-checked-off historical roadmap entries — `:256` "provider-aware PR open
(GitHub + Forgejo)", `:312` "GitLab ship provider" — which are accurate *as
records of what those milestones shipped* and should not be retroactively
rewritten.

**Provisional default: include `README.md` in t-8, but scoped to current-state prose only — the ship row and the status paragraph — with historical roadmap checkboxes explicitly out of scope.** risk's principle is right and the memory of the readme-truth pass supports it; its file-level framing just overstates what is there to change.

**Why it matters:** it is the smallest divergence, but it is the one most likely to be resolved wrongly *by accident* — an executor handed "README.md" with a mandate to make provider lists truthful can very plausibly start editing the roadmap log.

### D8 — What `MilestoneModesFor` returns for the forge/github providers

- **verification**: `nil`, meaning "mode is never consulted, never warn" — "returning `["version"]` for them would make `gitlab + epic` a false positive, since those backends never read milestone_mode at all."
- **mvp**: `["version"]` for forge/github.
- **risk**: sidesteps it with `ModeSupported(provider, mode) → reason string`, where `ModeSupported("youtrack","epic")` returns `""`; the forge/github case is not stated.

**Provisional default: verification's `nil`.** It is the only one of the three whose semantics distinguish "this combination is wrong" from "this field is not read here", and c-5's criterion text is about combinations that "will fail at runtime" — a gitlab board with `milestone_mode = "epic"` will not fail at runtime, because nothing reads the field.

**Why it matters:** it decides whether the new c-5 warning fires on configurations that work perfectly today. Under mvp's encoding, every gitlab/forgejo board that has ever had a non-version mode written into it starts emitting a warning for a problem it does not have — and the fastest way to make a new warning tier worthless is to have it be wrong on first contact.
