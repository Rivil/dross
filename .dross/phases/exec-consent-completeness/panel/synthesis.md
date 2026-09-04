# Panel synthesis — exec-consent-completeness

Judged cold: I authored none of the three drafts. Claims were spot-checked
against the tree (41 `exec.Command`/`CommandContext` sites across 27 non-test
files in `internal/` + `cmd/`; `execGatedCommands` holds 6 entries and its
comment does open "the CLOSED set"; `trust_test.go:362` asserts `len == 6`;
`survivor_drain.go:38` and `:189` are the two drain spawns; `test.go:428`
(LaneConsented) does precede `:463` (resolveTestTarget) and `:500` (syncTreeTo);
`verify.go:49` holds requireExecConsent; `run.go:262` is the `sh` spawn). Every
file path and nearly every line number cited by the three drafts is real —
`TestAuditFlagsItsOwnSnippets` is at `subprocargs_audit_test.go:417`, not `:388`
as verification says, and `pickRemoteTarget` is called at
`lane_preview_locality.go:108`, not `:110`. No draft cites a file that does not
exist.

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| **risk** (10 tasks / 4 waves) | 5 — c-2 owned by 7 tasks and c-6 by 3; every criterion has a task that can turn red alone | 4 — named test functions, vacuity floors, "delete one marker → exactly one finding"; but never proves the walk is binary-name-blind, which is c-1's core claim | 5 — the only draft that splits the sweep by verdict class (exempt / gated-by-reach / mutation) rather than by file count; admits t-6's 10 files break its own rule and says why | 5 — per-task `depends`, and the repo-wide assertion is deliberately switched on last so no intermediate commit is red |
| **verification** (6 / 3) | 5 — every criterion carries a *negative* control as well as a positive one; the only draft with a c-6 precision contract | 5 — sharpest of the three: real file:line anchors, counter-based (not text-based) remote assertions, "delete `requireExecConsent` from verify.go:49 and the mutation spawns must turn red" | 3.5 — enumerator/reach split is right, but the sweep is one 27-file task | 3.5 — wave 3 depends on t-1..t-4 when t-5 needs nothing from t-3; the docs task is stranded behind the whole sweep for no dependency reason |
| **mvp** (5 / 2) | 4 — 7/7 nominally, but c-6 is absorbed into t-1 and c-2's sweep carries only two contracts for 27 files | 3.5 — test functions are named and the seam-count idea is right, but the sweep task's contract is one example (`doctor.go`'s `git --version`) standing in for 41 sites | 3 — t-1 bundles enumeration + edge set + cobra attribution + verdict rule; t-4 is the same 27-file monster | 2.5 — t-1 lands the repo-wide gate red *by design* ("t-1 lands failing-by-design against the real tree"), so an intermediate commit is knowingly red |

**Skeleton: risk.** Not because it has the most tasks, but because its wave order
dissolves the objection the other two build their structure around.
Verification and mvp both argue the sweep is indivisible — "`zero findings over
internal/ + cmd/` is all-or-nothing, any split leaves an intermediate commit with
the gate red". That is only true if the repo-wide assertion is live during the
sweep. Risk switches it on in t-10, *after* the sweep, and gives each sweep task
a file-scoped zero-findings assertion instead. That yields four green commits
where the others yield one, and it is the only structure compatible with this
repo's commit-gating discipline (run the check, read it, then commit). Neither
runner-up considered that option. Risk's contracts are then upgraded wholesale
from verification, which out-writes it on every individual assertion.

## Merged plan

Phase exec-consent-completeness — 10 tasks across 4 waves

Wave 1

```
  t-1  Enumerate spawn sites and parse exemption markers            [risk + verification + mvp]
       files:    internal/cmd/execconsent_audit_test.go,
                 internal/cmd/testdata/exec_consent/snippets.txt,
                 internal/cmd/testdata/exec_consent/ungated.go.txt
       covers:   c-1, c-2, c-4
       desc:     go/ast walk over auditRoots ("internal", "cmd") collecting every
                 exec.Command / exec.CommandContext construction, plus the
                 //dross:exec-exempt <reason> grammar (comment on the line
                 immediately above; non-empty prose required). New file, NOT an
                 extension of subprocargs_audit_test.go — the two gates answer
                 different questions and a merged walk makes one gate's red read
                 as the other's — but same package, so spawnArgvOf, stringLit,
                 exprText, repoRootForDocs and auditRoots are called, not copied.
                 Fixture-driven only; the repo-wide assertion is t-10's.  [mvp]
       contract: - the ungated fixture spawn produces exactly one finding whose
                   text contains the fixture's file, its line, and the literal
                   remedy "gate it or mark it exempt"                      [risk]
                 - an ungated `exec.Command("cargo", pkg)` is flagged with the
                   same finding shape as a git one; no binary name appears in
                   any table the verdict consults              [verification] ★
                 - a bare `//dross:exec-exempt` with no prose is a finding naming
                   the marker as reasonless; the same marker with prose is not
                 - a marker two lines above the call does NOT exempt it —
                   position is part of the grammar, not proximity           [risk]
                 - the FLAG/PASS snippet table asserts rows-parsed ==
                   rows-exercised, mirroring subprocargs_audit_test.go:417, so a
                   silently truncated table fails rather than shrinking coverage
                 - a walk returning zero sites over the fixture set fails with
                   "found no spawn sites" rather than passing vacuously
```

```
  t-2  Gate `survivor drain` behind exec consent          [risk + mvp + verification]
       files:    internal/cmd/survivor_drain.go, internal/cmd/trust.go,
                 internal/cmd/trust_test.go,
                 internal/cmd/survivor_drain_consent_test.go
       covers:   c-3
       desc:     Add "survivor drain" to execGatedCommands; call
                 requireExecConsent as the first statement of drain's RunE,
                 before FindRoot's siblings and before gatherDrainSurvivors
                 (survivor_drain.go:337). Existing drain fixtures gain
                 trustFixture. Gate at the RunE top, not at the two seams —
                 drain has two spawn seams that would each need it.        [mvp]
       contract: - unconsented `dross survivor drain` returns an error naming
                   `dross trust`, and the goListDirs seam (survivor_drain.go:38)
                   records ZERO calls — refusal precedes the spawn
                 - the same refusal holds with `--report`, where coverageProfileFn
                   (:189, called at :398) would still spawn
                   `go test -coverprofile` over the repo                    [risk]
                 - after GrantConsent the same run fails for some other reason or
                   succeeds, never with "dross trust"            [verification] ★
                 - TestExecGatedSetIsExplicit's want list holds 7 entries
                   including "survivor drain" and its size assertion reads 7
                   (today trust_test.go:362 reads 6), so the set cannot grow
                   or shrink silently
                 - `dross survivor list` and `dross doctor` still succeed
                   unconsented — the gate must not spread into read-only
                   diagnosis                                                [risk]
```

```
  t-3  Refuse a remote lane before anything reaches the host   [risk + verification + mvp]
       files:    internal/cmd/test.go, internal/cmd/remote_bootstrap.go,
                 internal/cmd/lane_preview_locality.go,
                 internal/cmd/lane_remote_consent_test.go
       covers:   c-7
       desc:     Proof-first with a conditional fix. runTestLanes already
                 resolves consent (test.go:428) before resolveTestTarget (:463)
                 and syncTreeTo (:500); nothing pins that order, so a refactor
                 moving the probe above the loop goes unnoticed. Fix only where
                 the proof shows a path — probe, prepare, bootstrap install, or
                 preview — that reaches a host ahead of its grant. All assertions
                 ride counting stubs over spawnRemote (test.go:742) and
                 remoteProbeFn (doctor.go:1396), never output text: a test
                 asserting only the refusal message still passes on a run that
                 probed first and refused second.                [verification]
       contract: - granted host + STALE lane grant: `dross test --files x`
                   records zero spawnRemote calls — no rsync argv, no ssh argv —
                   and exits the lane-refused code; the refusal names the lane
                   and `dross trust --lane <name>`
                 - grant ABSENT: the ssh preflight in resolveTestTarget and the
                   rsync in syncTreeTo are both unreached, proven by counter == 0
                 - a lane granted for its command alone, then given a `prepare`,
                   is refused locally — the prepare line never appears in the
                   recorded remote argv (laneConsentLine frames the two together)
                 - when every matched lane is ungranted the host probe seam
                   records no call, so an unreachable host cannot mask the refusal
                 - `dross remote bootstrap --apply` with an ungranted lane install
                   line records zero remoteExecFn install calls for that lane
                   (remote_bootstrap.go:231)                                [risk]
                 - `dross test lane preview --probe` on an ungranted lane does not
                   reach pickRemoteTarget (lane_preview_locality.go:108) — see
                   disagreement D4; this contract is provisional  [verification] ★
```

Wave 2 (t-4 depends t-1; t-5 depends t-1, t-2)

```
  t-4  Attribute spawn sites by call reach                [risk + verification + mvp]
       files:    internal/cmd/execconsent_audit_test.go,
                 internal/cmd/testdata/exec_consent/reach/root.go.txt,
                 internal/cmd/testdata/exec_consent/reach/helper.go.txt
       covers:   c-6, c-2
       depends:  t-1
       desc:     Build a function→function edge set from the same ASTs —
                 package-level var seams, func literals, methods — plus the cobra
                 tree (Use + AddCommand) so a site is attributed to a full command
                 path. Verdict rule: a site passes if it is marked, or if every
                 command reaching it is in execGatedCommands. Gating is detected
                 by NAME RULE — a function gates if it calls requireExecConsent or
                 any identifier ending in "Consented" — not by a roster of the
                 five known helpers, because c-1 kills hand-maintained lists on
                 both sides of the verdict.                     [verification] ★
       contract: - RECALL: a fixture RunE reaching exec.Command through two named
                   hops in a different package is attributed to root's `Use:`
                   string, and flagged when that command is not gated
                 - PRECISION: the reach set for `dross doctor` does NOT contain
                   internal/mutation/gremlins.go's spawn. Without this the graph
                   degenerates into "everything reaches everything", every site
                   needs a marker, and the gate half of the verdict is dead code
                                                                [verification] ★
                 - an edge through `var runFn = doSpawn` is followed; rewriting
                   the seam as `var runFn = func(){…}` keeps the attribution
                 - a helper name resolving to two different packages is REPORTED,
                   not silently attributed to the gated one — over-attribution
                   turns an ungated site green, the one direction the gate must
                   not err in                                             [mvp] ★
                 - a site reachable from NO command is a finding (fail-closed):
                   an unresolvable edge must not read as "unreachable"     [risk]
                 - a site reachable from both a gated and an ungated command is
                   flagged, and the finding names the ungated command      [risk]
                 - a snippet calling a newly-invented `FooConsented` gates its
                   callee with no edit to the enumerator          [verification]
```

```
  t-5  Describe the enumerated surface, not six names       [risk + mvp + verification]
       files:    internal/cmd/trust.go, internal/cmd/doctor.go,
                 assets/prompts/verify.md, assets/prompts/execute.md,
                 assets/prompts/quick.md,
                 internal/cmd/execconsent_docs_test.go
       covers:   c-5
       depends:  t-1, t-2
       desc:     Rewrite execGatedCommands' comment (trust.go:698 opens "the
                 CLOSED set" and :716 says "The other four"), doctor's
                 `Exec consent:` section (doctor.go:1212) and the three prompts
                 that describe the gated surface — quick.md:83 "the loop commands
                 below refuse", verify.md:22 "the one consent-gated execution
                 site". A docs test pins the wording, asserted by reading the
                 source file the way redproof_doc_test.go already does.
       contract: - the docs test fails if trust.go's comment contains "CLOSED set",
                   "the other four", or a spelled-out command count
                 - no .go or assets/*.md file states a count of gated commands as
                   word or numeral — a grep for "six commands" / "6 commands"
                   fails on a hit                                [verification] ★
                 - the docs test fails if any of the three prompts describes the
                   gate as a fixed list of command names                   [risk]
                 - `dross doctor` under `Exec consent:` names `survivor drain`
                   among what the grant authorizes                          [mvp]
                 - `dross doctor` under `Exec consent:` contains the token
                   `//dross:exec-exempt`, so the reader learns the escape hatch
                   where they learn the state                               [risk]
                 - the comment names the enumerating test by function name, and
                   the docs test fails when that function no longer exists  [risk]
       note:     assets/prompts/options.md also carries consent prose (:170-:204)
                 but describes grant *mechanics*, not the gated set — no draft
                 lists it and it appears not to need the rewrite. Confirm during
                 execution rather than adding it blind.
```

Wave 3 (depends t-4; t-7 also depends t-2)

```
  t-6  Mark git-plumbing spawn sites exempt                              [risk]
       files:    internal/cmd/cleantree.go, internal/cmd/pause.go,
                 internal/cmd/phase.go, internal/cmd/ship_recover.go,
                 internal/cmd/statusline.go, internal/cmd/techdebt.go,
                 internal/cmd/worktree_files.go, internal/cmd/init.go,
                 internal/cmd/milestone_stale.go, internal/cmd/doctor.go
       covers:   c-2
       depends:  t-4
       desc:     One //dross:exec-exempt line per git spawn (status, show,
                 ls-files, rev-parse, patch-id, --version), each stating why that
                 subcommand cannot reach repo-authored code. Ten files, over the
                 split rule, because it is one layer, one reason class and
                 comment-only — splitting produces two tasks that must agree on
                 identical prose with nothing to enforce that.
       contract: - the enumerator, scoped to these ten files, reports zero findings
                 - deleting any one marker reintroduces exactly one finding naming
                   that file and line, asserted by the fixture harness
                 - every marker in the tree carries >= 20 characters of prose; a
                   marker whose prose is only the subcommand name is still a
                   finding                            [risk + verification] ★
```

```
  t-7  Prove cmd toolchain spawns reach a gate            [risk + mvp + verification]
       files:    internal/cmd/run.go, internal/cmd/test.go,
                 internal/cmd/verify.go, internal/cmd/lane_install.go,
                 internal/cmd/update.go, internal/cmd/survivor_drain.go
       covers:   c-2, c-6
       depends:  t-2, t-4
       desc:     These spawn repo- or user-supplied lines. Each must resolve as
                 GATED through reach (requireExecConsent, RunConsented,
                 ReplayConsented, LaneConsented, LaneInstallConsented); any that
                 does not is gated here rather than marked. survivor_drain.go is
                 included: t-2 gates its RunE, and this is where the enumerator
                 confirms both its spawns (:38, :189) resolve as gated rather
                 than needing markers.                    [mvp + verification] ★
       contract: - the enumerator reports zero findings for these six files, and
                   each verdict is "gated", never "exempt"
                 - removing `dross run`'s RunConsented check makes the enumerator
                   flag run.go:262 by name                                 [risk]
                 - update.go:197's self-exec of the verified new binary is the one
                   site here allowed a marker, and its reason names signature
                   verification                                            [risk]
                 - `dross run <slot>` unconsented still refuses with a message
                   naming the slot, so the reach proof did not replace the runtime
                   refusal                                                 [risk]
```

```
  t-8  Mark helper-package spawn sites exempt                            [risk]
       files:    internal/codex/git.go, internal/codex/ast_grep.go,
                 internal/quality/run.go, internal/security/run.go,
                 internal/techdebt/run.go, internal/ship/open.go,
                 internal/remote/remote.go
       covers:   c-2
       depends:  t-4
       desc:     Markers for the scan/report/transport spawns. remote.go's
                 buildCommand states that its argv is ssh/rsync built by
                 SSHArgs/SyncArgs against the host allowlist, and that any
                 repo-authored line it carries is consent-checked by the caller
                 (t-3).
       contract: - the enumerator, scoped to these seven files, reports zero
                   findings
                 - remote.go's marker is REJECTED if its prose does not mention the
                   caller-side check, so the two halves of c-7 cannot drift apart
                 - deleting ship/open.go's marker on the ghCommand literal produces
                   a finding attributed through the var seam, proving t-4's edge
                   set still covers a func-literal spawn
```

```
  t-9  Attribute mutation spawns to `verify`                    [risk + verification]
       files:    internal/mutation/gremlins.go, internal/mutation/stryker.go,
                 internal/mutation/stryker_net.go, internal/mutation/launcher.go
       covers:   c-2, c-6
       depends:  t-4
       desc:     These run the repo's own suite. They must resolve as gated by
                 reach from `verify` — not marked exempt — which is the concrete
                 case c-6 names (verify → internal/mutation → gremlins). If the
                 easy way through the sweep is marking every site exempt, the
                 phase ships a green test that proves nothing.
       contract: - the verdict for all four files is "gated via verify"; a marker
                   placed on any of them is itself a finding ("a site reached by a
                   gated command needs no exemption")                      [risk]
                 - deleting `requireExecConsent()` from verify.go:49 turns all four
                   mutation spawns into findings — the gate half of the verdict is
                   load-bearing, not decorative               [verification] ★
                 - severing the verify → mutation edge in a fixture leaves the four
                   sites reachable from no command and therefore flagged   [risk]
                 - a new package inserted between verify and gremlins in the
                   fixture keeps the attribution                           [risk]
```

Wave 4 (depends t-4, t-6, t-7, t-8, t-9)

```
  t-10 Turn the repo-wide gate on with a floor                           [risk]
       files:    internal/cmd/execconsent_audit_test.go
       covers:   c-1, c-2
       depends:  t-4, t-6, t-7, t-8, t-9
       desc:     Switch the enumerator from fixtures to internal/ and cmd/, assert
                 zero findings, and pin a discovery floor so a narrowed walk fails
                 instead of passing quietly.
       contract: - TestEverySpawnSiteGatedOrExempt reports zero findings across
                   internal/ and cmd/; each finding, if any, prints file:line, the
                   reaching command, and the remedy
                 - the run fails below 30 spawn sites or 20 distinct files — the
                   vacuity floor. (Measured today: 41 sites across 27 files, so
                   the floor sits ~27% under the real count.)
                 - dropping "internal" from the scan roots drops the count below
                   the floor and fails
                 - internal/mutation, internal/remote, internal/codex and
                   internal/ship are asserted inside the scan roots by path,
                   mirroring subprocargs_audit_test.go:548's assertAuditCovers
```

★ = grafted from a runner-up onto the risk skeleton.

Criterion coverage of the merged plan: c-1 → t-1, t-10 · c-2 → t-1, t-4, t-6,
t-7, t-8, t-9, t-10 · c-3 → t-2 · c-4 → t-1 · c-5 → t-5 · c-6 → t-4, t-7, t-9 ·
c-7 → t-3.

## Disagreements

**D1 — Sweep granularity, and when the repo-wide assertion turns on.**
The widest split in the panel, and the reason the task counts range 5 to 10.
Verification and mvp both make the sweep ONE task over 27 files, and both justify
it identically: "`zero findings over internal/ + cmd/` is all-or-nothing… any
split leaves an intermediate commit with the gate red… a task that cannot end
green is not a task." Verification adds that it chose atomicity over the
granularity rule deliberately. Risk splits the sweep four ways by verdict class
(git plumbing / toolchain-gated / helper packages / mutation) and defers the
repo-wide assertion to t-10. mvp goes further the other way: its t-1 lands the
repo-wide gate red on purpose, so the sweep has something to clear.
*Provisional default:* risk's split, with the repo-wide assertion last (t-10) and
each sweep task carrying a file-scoped zero-findings assertion.
*Why it matters:* the runners-up's premise — that splitting forces a red
intermediate — only holds if the repo-wide assertion is already live. It need not
be. Risk's structure gives four independently green commits where theirs gives
one 27-file commit, and mvp's deliberately-red intermediate collides head-on with
this repo's commit-gating discipline. The cost of the default is real and should
be named: between t-1 and t-10 the enumerator never runs against the whole tree,
so a spawn site in a file no sweep task lists would go unnoticed until t-10.
The 27-file inventory above is what closes that gap — t-6 (10) + t-7 (6) + t-8
(7) + t-9 (4) = 27, matching exactly.

**D2 — One enumerator task or two.**
mvp folds enumeration, the edge set, cobra attribution and the verdict rule into
a single t-1 covering c-1/c-2/c-4/c-6. Risk and verification split discovery
(t-1) from reach (t-4); verification's reasoning is the sharper of the two —
t-1's contracts are all satisfiable with direct calls and no graph, so t-1 can go
green and commit before the graph exists.
*Provisional default:* two tasks.
*Why it matters:* a blind walk and a blind graph are different failure modes with
different red tests. Merged, the first green run cannot distinguish them, and the
precision contract (D3) has no working baseline to be measured against.

**D3 — Whether the reach graph needs a precision contract.**
Only verification carries one ("the reach set for `dross doctor` does NOT contain
gremlins.go's spawn"). Risk's t-4 contracts are all recall-shaped — follow the
seam, follow the two-hop path, fail closed on unreachable. mvp has a partial
precision guard (ambiguous name → report, don't attribute).
*Provisional default:* include verification's precision contract alongside risk's
recall ones, plus mvp's ambiguity contract.
*Why it matters:* the locked `enumerator_mechanism` forbids go/types, so callees
resolve by name and the graph over-approximates. Over-approximation is the safe
direction, but unbounded it collapses into "everything reaches everything" —
every site then needs a marker, the gate half of the verdict never fires, and a
recall-only contract set passes on a graph that returns the full site set for
every root. Risk would have shipped that.

**D4 — Is `dross test lane preview --probe` inside c-7?**
Only verification says yes, and it is right about the mechanism: `previewHost`
with `--probe` calls `pickRemoteTarget` (lane_preview_locality.go:108), which
probes through `preflightRemote` → `remoteProbeFn` → ssh `command -v` on the
host, while `previewConsent` (lane_preview.go:194) reads the lane's grant purely
as an annotation and *deliberately discards* the refusal. Risk and mvp both scope
c-7 to lane dispatch only and never mention preview.
*Provisional default:* include as a contract in t-3, marked provisional.
*Why it matters:* on c-7's literal text — "consent-checked before anything is
transferred to or executed on that host" — a `command -v` over ssh is execution
on the host from a command carrying no grant. Against that, preview dispatches no
lane, and lane_preview.go's own comment makes non-gating an explicit design
choice ("preview spawns nothing, so carrying it would turn an annotation into a
gate") tied to the locked `preview_exit_status`. That comment is now factually
wrong — probe does spawn — which is itself worth surfacing. If gating preview
turns out to collide with `preview_exit_status`, the fallback is an exempt marker
on the probe path whose reason names the host allowlist; that fallback is a spec
question, not an execution-time judgement call.

**D5 — How the enumerator decides a function is gating.**
Verification uses a name rule: a function gates iff it calls `requireExecConsent`
or any identifier ending in `Consented`, and makes it testable (an invented
`FooConsented` gates without editing the enumerator). Risk implies a roster —
t-7 lists the five helpers by name. mvp does not specify.
*Provisional default:* the name rule.
*Why it matters:* c-1 kills hand-maintained lists of command names; a roster of
consent-function names is the same object wearing a different hat. One caveat
found in the source that no draft raises: `doctor.go:1267` calls `LaneConsented`
for display only, so a pure name rule marks `doctor` as gating. Since `doctor` is
deliberately *outside* the gated set, that is a false-gate the rule will need to
handle — likely by requiring the result to be acted on, not merely called.

**D6 — Does c-7 cover the remote bootstrap install leg?**
Risk's t-3 includes `remote_bootstrap.go` and contracts that an ungranted lane
install line records zero `remoteExecFn` calls. Verification and mvp confine c-7
to `runTestLanes` and (for verification) preview.
*Provisional default:* include it.
*Why it matters:* `remote_bootstrap.go:231` does call `LaneInstallConsented`, so
the property probably already holds — but it is a second path that transfers to
and executes on a host, and pinning it costs one more seam recorder against a
test harness t-3 already builds. Leaving it out means a refactor of the bootstrap
leg is unguarded by anything.

**D7 — How much prompt text the docs task owns.**
Risk edits three prompts (verify.md, execute.md, quick.md) plus trust.go and
doctor.go. mvp edits two (execute.md, quick.md). Verification edits no prompt at
all, and instead adds a repo-wide grep contract that fails on any stated count of
gated commands in `.go` or `assets/*.md`.
*Provisional default:* risk's three files plus verification's grep contract.
*Why it matters:* the two are complementary, not competing — the edits fix
today's prose (quick.md:83 "the loop commands below refuse"; verify.md:22 "the
one consent-gated execution site", which stops being true the moment drain
joins), and the grep stops the next author restating a count. Verification's
contract alone leaves the current wrong sentences standing, since neither states
a numeral.

**Note, not a disagreement:** the three drafts name the enumerator file
`execconsent_audit_test.go` / `execspawn_audit_test.go` / `execgate_audit_test.go`
and its testdata dir correspondingly. Defaulted to risk's names as skeleton;
no substance rides on it.
