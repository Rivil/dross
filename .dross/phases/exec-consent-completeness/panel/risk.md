# risk lens — exec-consent-completeness

Failure modes first. Each risk below is owned by exactly one task, and each task
carries the test that turns that risk red.

| # | Risk | Owner |
|---|---|---|
| R1 | The enumerator goes blind — a walk that stops seeing spawn sites passes forever and reads as coverage | t-1, t-10 |
| R2 | The exemption marker becomes a rubber stamp — bare, mis-positioned, or pasted onto a site that should be gated | t-1 |
| R3 | Reach is incomplete — a var seam, a func literal, a method or a cross-package hop hides a spawn from attribution | t-4 |
| R4 | `survivor drain` runs the repo's suite in a tree nobody trusted | t-2 |
| R5 | A remote lane transfers or executes before its grant is checked; the refusal happens on the host, not here | t-3 |
| R6 | Docs keep describing a closed set of six, so the description drifts the day the set changes | t-5 |
| R7 | Bulk annotation gates the wrong thing — a marker on a site that reaches repo code, or a gate that bricks `doctor`/`status` | t-6, t-7, t-8, t-9 |
| R8 | The whole-repo gate lands red across a multi-task window, so the suite stops being a signal mid-phase | wave order: gate is switched on last, in t-10 |

Phase exec-consent-completeness — 10 tasks across 4 waves

Wave 1
  t-1  Enumerate spawn sites and parse exemption markers
       files:    internal/cmd/execconsent_audit_test.go,
                 internal/cmd/testdata/exec_consent/snippets.txt,
                 internal/cmd/testdata/exec_consent/ungated_fixture.txt
       covers:   c-1, c-2, c-4
       desc:     go/ast walk over internal/ and cmd/ collecting every
                 exec.Command / exec.CommandContext site, plus the
                 //dross:exec-exempt <reason> grammar (comment on the line
                 immediately above, non-empty prose required). Fixture-driven
                 only — the repo-wide assertion is t-10's.
       contract: - the ungated_fixture.txt spawn produces exactly one finding whose
                   text contains the fixture's file, its line number, and the
                   literal remedy "gate it or mark it exempt"
                 - a bare `//dross:exec-exempt` with no prose is a finding saying the
                   marker carries no reason; the same marker with prose is not
                 - a marker two lines above the call does NOT exempt it (position is
                   part of the grammar, not proximity)
                 - a FLAG/PASS snippet table asserts rows-parsed == rows-exercised,
                   so a silently truncated table fails rather than shrinking coverage
                 - a walk that returns zero sites over the fixture set fails with
                   "found no spawn sites" rather than passing vacuously

  t-2  Gate `survivor drain` behind exec consent
       files:    internal/cmd/survivor_drain.go, internal/cmd/trust.go,
                 internal/cmd/trust_test.go, internal/cmd/survivor_drain_test.go
       covers:   c-3
       desc:     Add "survivor drain" to execGatedCommands and call
                 requireExecConsent at the top of its RunE, before
                 gatherDrainSurvivors. Existing drain fixtures gain trustFixture.
       contract: - `dross survivor drain` in an unconsented fixture returns an error
                   naming `dross trust`, and the goListDirs seam records zero calls
                 - the same refusal holds with `--report`, where coverageProfileFn
                   would still spawn `go test -coverprofile` over the repo
                 - TestExecGatedSetIsExplicit's want list contains "survivor drain"
                   and its size assertion reads 7, so the set cannot grow silently
                 - `dross survivor list` and `dross doctor` still succeed unconsented
                   (the gate must not spread into read-only diagnosis)

  t-3  Refuse a remote lane before anything reaches the host
       files:    internal/cmd/test.go, internal/cmd/remote_bootstrap.go,
                 internal/cmd/lane_remote_consent_test.go
       covers:   c-7
       desc:     Prove — and where the order is wrong, fix — that every remote
                 dispatch path resolves consent before probe, sync or exec:
                 runTestLanes, runLanePrepare, syncTreeTo, and bootstrap's install
                 leg. Assertions ride the spawnRemote / spawnLocal / probe seams.
       contract: - with a granted host and a STALE lane grant, `dross test --files x`
                   records zero spawnRemote calls — no rsync argv and no ssh argv —
                   and exits the lane-refused code
                 - a lane granted for its command alone, then given a `prepare`, is
                   refused locally: the prepare line never appears in the recorded
                   remote argv
                 - when every matched lane is ungranted, the host probe seam records
                   no call at all, so an unreachable host cannot mask the refusal
                 - `dross remote bootstrap --apply` with an ungranted lane install
                   line records zero remoteExecFn install calls for that lane

Wave 2 (depends t-1, t-2)
  t-4  Attribute spawn sites by call reach
       files:    internal/cmd/execconsent_audit_test.go,
                 internal/cmd/testdata/exec_consent/reach.txt
       covers:   c-6, c-2
       depends:  t-1
       desc:     Build a function->function edge set from the same ASTs, including
                 package-level var seams and func literals, plus the cobra tree
                 (Use + AddCommand) so a site is attributed to the full command
                 path. Verdict rule: a site passes if it is marked, or if every
                 command reaching it is in execGatedCommands.
       contract: - a fixture command whose RunE calls helper.Run(), which calls
                   exec.Command, attributes the site to that command's full path and
                   flags it when the command is not in execGatedCommands
                 - an edge through `var runFn = doSpawn` is followed; rewriting the
                   seam as `var runFn = func(){ … }` keeps the same attribution
                 - a site reachable from NO command is a finding (fail-closed), not
                   a pass — an unresolvable edge cannot read as "unreachable"
                 - a two-hop path (cmd -> internal/x -> internal/y) is attributed to
                   the command, so wrapping a spawn in a new package does not clear it
                 - a site reachable from both a gated and an ungated command is
                   flagged, and the finding names the ungated command

  t-5  Describe the enumerated surface, not six names
       files:    internal/cmd/trust.go, internal/cmd/doctor.go,
                 assets/prompts/verify.md, assets/prompts/execute.md,
                 assets/prompts/quick.md, internal/cmd/execconsent_docs_test.go
       covers:   c-5
       depends:  t-1, t-2
       desc:     Rewrite execGatedCommands' comment (it currently opens "the CLOSED
                 set"), doctor's `Exec consent:` section and the three prompts to
                 describe the enumerated surface and the exemption marker. A docs
                 test pins the wording to reality.
       contract: - the docs test fails if trust.go's execGatedCommands comment
                   contains "CLOSED set" or a spelled-out command count
                 - the docs test fails if any of the three prompts describes the gate
                   as a fixed list of command names
                 - `dross doctor` output under `Exec consent:` contains the marker
                   token `//dross:exec-exempt`, so the reader learns the escape hatch
                   from the same place they learn the state
                 - the comment names the enumerating test by function name, and the
                   docs test fails when that function no longer exists

Wave 3 (depends t-4)
  t-6  Mark git-plumbing spawn sites exempt
       files:    internal/cmd/cleantree.go, internal/cmd/pause.go,
                 internal/cmd/phase.go, internal/cmd/ship_recover.go,
                 internal/cmd/statusline.go, internal/cmd/techdebt.go,
                 internal/cmd/worktree_files.go, internal/cmd/init.go,
                 internal/cmd/milestone_stale.go, internal/cmd/doctor.go
       covers:   c-2
       depends:  t-4
       desc:     One //dross:exec-exempt line per git spawn (status, show, ls-files,
                 rev-parse, patch-id, --version), each stating why that subcommand
                 cannot reach repo-authored code.
       contract: - the enumerator reports zero findings for these ten files
                 - deleting any one marker reintroduces exactly one finding naming
                   that file and line (asserted by the fixture harness, not by eye)
                 - a marker whose prose is only the subcommand name (no reason) is
                   still a finding — t-1's grammar applies here unchanged

  t-7  Prove cmd toolchain spawns reach a gate
       files:    internal/cmd/run.go, internal/cmd/test.go, internal/cmd/verify.go,
                 internal/cmd/lane_install.go, internal/cmd/update.go
       covers:   c-2, c-6
       depends:  t-2, t-4
       desc:     These five spawn repo- or user-supplied lines. Each must resolve as
                 GATED through reach (requireExecConsent, RunConsented,
                 ReplayConsented, LaneConsented, LaneInstallConsented); any that does
                 not is gated here rather than marked.
       contract: - the enumerator reports zero findings for these five files, and the
                   verdict for each is "gated", never "exempt"
                 - a test that removes `dross run`'s RunConsented check makes the
                   enumerator flag run.go:262 by name
                 - update.go's self-exec of the verified new binary is the one site
                   here allowed a marker, and its reason names signature verification
                 - `dross run <slot>` in an unconsented fixture still refuses with a
                   message naming the slot, so the reach proof did not replace the
                   runtime refusal

  t-8  Mark helper-package spawn sites exempt
       files:    internal/codex/git.go, internal/codex/ast_grep.go,
                 internal/quality/run.go, internal/security/run.go,
                 internal/techdebt/run.go, internal/ship/open.go,
                 internal/remote/remote.go
       covers:   c-2
       depends:  t-4
       desc:     Markers for the scan/report/transport spawns. remote.go's
                 buildCommand states that its argv is ssh/rsync built by
                 SSHArgs/SyncArgs against the host allowlist, and that any
                 repo-authored line it carries is consent-checked by the caller (t-3).
       contract: - the enumerator reports zero findings for these seven files
                 - remote.go's marker is rejected if its prose does not mention the
                   caller-side check, so the two halves of c-7 cannot drift apart
                 - deleting ship/open.go's marker on the ghCommand literal produces a
                   finding attributed through the var seam, proving t-4's edge set
                   still covers a func-literal spawn

  t-9  Attribute mutation spawns to `verify`
       files:    internal/mutation/gremlins.go, internal/mutation/stryker.go,
                 internal/mutation/stryker_net.go, internal/mutation/launcher.go
       covers:   c-2, c-6
       depends:  t-4
       desc:     These run the repo's own suite. They must resolve as gated by reach
                 from `verify` — not marked exempt — which is the concrete case c-6
                 names (verify -> internal/mutation -> gremlins).
       contract: - the enumerator's verdict for all four files is "gated via verify",
                   and a marker placed on any of them is itself a finding ("a site
                   reached by a gated command needs no exemption")
                 - severing the verify -> mutation edge in a fixture leaves the four
                   sites reachable from no command and therefore flagged
                 - a new package inserted between verify and gremlins in the fixture
                   keeps the attribution, so a wrapper hides nothing

Wave 4 (depends t-6, t-7, t-8, t-9)
  t-10 Turn the repo-wide gate on with a floor
       files:    internal/cmd/execconsent_audit_test.go
       covers:   c-1, c-2
       depends:  t-4, t-6, t-7, t-8, t-9
       desc:     Switch the enumerator from fixtures to internal/ and cmd/, assert
                 zero findings, and pin a discovery floor so a narrowed walk fails
                 instead of passing quietly.
       contract: - the repo-wide run reports zero findings; each finding, if any,
                   prints file:line, the reaching command, and the remedy
                 - the run fails when it discovers fewer than 30 spawn sites or fewer
                   than 20 distinct files — the vacuity floor
                 - dropping "internal" from the scan roots drops the count below the
                   floor and fails, so the roots cannot be narrowed silently
                 - a test asserts internal/mutation, internal/remote, internal/codex
                   and internal/ship are inside the scan roots by path, mirroring
                   subprocargs_audit_test.go's assertAuditCovers

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1, t-10 |
| c-2 | t-1, t-4, t-6, t-7, t-8, t-9, t-10 |
| c-3 | t-2 |
| c-4 | t-1 |
| c-5 | t-5 |
| c-6 | t-4, t-7, t-9 |
| c-7 | t-3 |

## Judgment calls

- The repo-wide assertion is switched on LAST (t-10), not in t-1. Landing the
  walk and the gate together would leave the suite red across every annotation
  task, which turns the phase's own test signal off exactly while it is being
  changed. Rejected: an "advisory-only" mode in t-1, which is a hedge that
  someone forgets to remove.
- Discovery (t-1) and reach (t-4) are separate tasks despite sharing a file. They
  are separate risks — a blind walk and a blind graph fail differently — and the
  verdict rule cannot be written before both exist. They are waved, not
  parallelised, precisely because they share the file.
- The verdict rule is "marked OR every reaching command is gated", with
  reach-from-nothing a FINDING. Rejected: treating an unreachable site as
  harmless, which makes an unresolvable AST edge indistinguishable from dead
  code — the exact silence c-6 exists to break.
- Sites that reach repo code get GATED, not marked (t-7, t-9), and a marker on a
  gated site is itself a finding. Without that asymmetry the marker becomes the
  cheap way out of every future gate, which is the hand-maintained list c-1 kills
  wearing a different hat.
- Annotation is split by verdict class (exempt vs gated) and by package boundary,
  not evenly by file count. t-6 touches ten files, over the split rule, because it
  is one layer, one reason class and comment-only; splitting it would produce two
  tasks that must agree on identical prose with no way to enforce that.
- c-7 is one task covering test dispatch AND bootstrap install, because both are
  "authority resolved before the host is touched" and a test proving one is one
  seam recorder away from proving the other. Rejected: folding c-7 into t-3's
  enumerator work, where a transport-ordering bug would hide behind an AST result.
- Gating `survivor drain` covers its `--report` path too, which means classifying
  recorded reports now refuses in an untrusted tree. That follows the locked
  drain_grant decision and is the honest reading: the report path still spawns
  `go test -coverprofile` through coverageProfileFn.
- t-5 pins doc wording with a test rather than trusting a prose edit. A comment
  that describes an enumerated surface is exactly the kind of text that reverts to
  naming a list the next time someone edits the set.
