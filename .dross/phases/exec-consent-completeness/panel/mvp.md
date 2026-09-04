# MVP lens — exec-consent-completeness

Phase exec-consent-completeness — 5 tasks across 2 waves

Wave 1
  t-1  Enumerate spawn sites with reach attribution
       files:    internal/cmd/execspawn_audit_test.go,
                 internal/cmd/testdata/execspawn_audit/snippets.txt
       covers:   c-1, c-2, c-4, c-6
       desc:     New go/ast walk over auditRoots ("internal", "cmd") reusing package cmd's
                 existing spawnArgvOf/stringLit/exprText/repoRootForDocs helpers. Collects every
                 exec.Command/exec.CommandContext construction, builds a func->func edge set from
                 the same ASTs, walks back from each cobra RunE literal to attribute each site to
                 the command(s) that reach it, and reports a site that is neither reached only by
                 execGatedCommands members nor carries `//dross:exec-exempt <reason>`.
                 snippets.txt is the FLAG/PASS fixture table, parsed the way
                 subprocargs_audit_test.go parses its own.
       contract: - TestExecSpawnFixtureIsFlagged: the fixture's ungated `exec.Command("curl", url)`
                   snippet is reported with its file, its line, and the remedy string
                   "gate it or mark it exempt"; deleting either the file:line or the remedy from
                   the message fails it.
                 - TestExemptMarkerRequiresReason: `//dross:exec-exempt` with no prose after it
                   still produces a finding; `//dross:exec-exempt git plumbing, dross-authored argv`
                   does not.
                 - TestReachAttributesThroughHelper: a spawn moved into a helper func called only
                   from verify's RunE is still attributed to "verify"; severing the edge-set build
                   (attributing direct calls only) fails it.
                 - TestAmbiguousReachFailsClosed: a helper name resolving to two different packages
                   is reported, not silently attributed to the gated one.
                 - TestExecSpawnEnumeratorCoversPackages: fails if internal/mutation, internal/remote,
                   internal/ship or internal/codex leaves the scanned roots.
                 - TestExecSpawnEnumeratorSawFiles: fails when the walk scans zero files, so the gate
                   cannot pass vacuously.
       depends:  —

  t-2  Gate survivor drain on test-command consent
       files:    internal/cmd/survivor_drain.go, internal/cmd/trust.go,
                 internal/cmd/trust_test.go, internal/cmd/survivor_drain_consent_test.go
       covers:   c-3
       desc:     Add `"survivor drain"` to execGatedCommands and call requireExecConsent as the
                 first statement of survivorDrain's RunE, before FindRoot's siblings and before
                 gatherDrainSurvivors. Update the pinned set in TestExecGatedSetIsExplicit
                 (want list + the len == 6 assertion) and add the matching TestGatedCommandsRefuse row.
       contract: - TestSurvivorDrainRefusesUntrusted: in a tree with a configured runtime.test_command
                   and no consent, `dross survivor drain` returns the untrusted-command refusal AND
                   a recording goListDirs seam records zero calls — so `go list` / `go test` never
                   spawn. Removing requireExecConsent from the RunE fails it on the seam count.
                 - TestExecGatedSetIsExplicit fails if "survivor drain" leaves execGatedCommands.
       depends:  —

  t-3  Refuse remote lanes before any transfer
       files:    internal/cmd/test_remote_consent_order_test.go, internal/cmd/test.go
       covers:   c-7
       desc:     Pin the ordering runTestLanes already has: the per-lane LaneConsented loop resolves
                 every matched lane before resolveTestTarget probes a host and before syncTreeTo
                 pushes the tree. Recording seams over remote.commandFn, spawnRemote and syncTreeTo
                 assert nothing left the machine. Fix test.go only if the proof shows a path
                 (probe, prepare, or fallback) that reaches a host ahead of the loop.
       contract: - TestRemoteLaneRefusedBeforeTransfer: with a granted allowlisted host and one
                   unconsented lane, the run exits exitLaneRefused and the recording commandFn,
                   spawnRemote and syncTreeTo seams each record zero invocations. Moving the consent
                   loop below resolveTestTarget fails it on the commandFn count (the ssh probe).
                 - TestRemoteLaneStaleRefusedBeforeTransfer: same, for a lane whose command changed
                   after its grant — the refusal names ConsentStale and still transfers nothing.
                 - TestRemoteLanePrepareNeedsGrant: a lane with a prepare line and no grant never
                   reaches runLanePrepare (spawnRemote records zero calls), since laneConsentLine
                   frames prepare and command together.
       depends:  —

Wave 2 (depends t-1, t-2)
  t-4  Gate or mark every enumerated spawn site
       files:    internal/cmd/statusline.go, internal/cmd/ship_recover.go, internal/cmd/run.go,
                 internal/cmd/update.go, internal/cmd/phase.go, internal/cmd/cleantree.go,
                 internal/cmd/milestone_stale.go, internal/cmd/pause.go,
                 internal/cmd/worktree_files.go, internal/cmd/lane_install.go,
                 internal/cmd/doctor.go, internal/cmd/init.go, internal/cmd/techdebt.go,
                 internal/cmd/test.go, internal/cmd/verify.go, internal/cmd/survivor_drain.go,
                 internal/techdebt/run.go, internal/quality/run.go, internal/security/run.go,
                 internal/codex/git.go, internal/codex/ast_grep.go, internal/ship/open.go,
                 internal/remote/remote.go, internal/mutation/gremlins.go,
                 internal/mutation/stryker.go, internal/mutation/stryker_net.go,
                 internal/mutation/launcher.go
       covers:   c-2
       desc:     Run the t-1 enumerator and clear it to zero findings: each site is either already
                 reached only by a gated command, or gets a `//dross:exec-exempt <reason>` line above
                 the call stating why it cannot reach repo-authored code (dross-authored git plumbing
                 argv, self-exec of a verified binary, etc.). Any site whose reason cannot be written
                 honestly gets a requireExecConsent gate instead.
       contract: - The repo-wide TestEverySpawnSiteGatedOrExempt reports zero findings; deleting the
                   marker above doctor.go's `exec.Command("git", "--version")` re-flags that exact
                   file:line.
                 - Each written reason is non-empty prose, so t-1's TestExemptMarkerRequiresReason
                   stays green across the whole tree rather than only on the fixture.
       depends:  t-1, t-2

  t-5  Rewrite gate docs around the enumerated surface
       files:    internal/cmd/trust.go, internal/cmd/doctor.go,
                 assets/prompts/execute.md, assets/prompts/quick.md,
                 internal/cmd/consent_surface_test.go
       covers:   c-5
       desc:     Replace "the CLOSED set of commands" framing in execGatedCommands' comment with the
                 enumerated-surface description (the set is what the enumerator holds to; every spawn
                 site is gated or marked exempt), name survivor drain in doctor's Exec consent
                 section, and update the prompt text that describes what consent covers.
       contract: - TestConsentDocsDescribeEnumeratedSurface: fails if trust.go's execGatedCommands
                   comment reverts to describing a closed set of six command names, or drops the
                   reference to the enumerating test.
                 - TestDoctorConsentSectionCoversDrain: doctor's "Exec consent:" block names
                   `survivor drain` among what the grant authorizes; removing it fails.
                 - TestPromptsDescribeEnumeratedSurface: the execute/quick prompt consent preflight
                   no longer enumerates six command names as the gated surface.
       depends:  t-1, t-2

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-1, t-4 |
| c-3 | t-2 |
| c-4 | t-1 |
| c-5 | t-5 |
| c-6 | t-1 |
| c-7 | t-3 |

7/7 criteria covered.

## Judgment calls

- New `execspawn_audit_test.go` rather than extending `subprocargs_audit_test.go`: the two gates answer different questions (argv fencing vs consent reach) and a merged walk would make one gate's red read as the other's. Rejected a shared file; kept the reuse where it is free — same package, so spawnArgvOf/stringLit/exprText/repoRootForDocs/auditRoots are called directly, not copied.
- Reach resolved by name-based func->func edges over the already-parsed ASTs, with ambiguity FLAGGED rather than resolved to the gated caller. Rejected importing a real call-graph library (locked enumerator_mechanism forbids new deps) and rejected best-effort name matching, because over-attribution turns an ungated site green — the one direction the gate must not err in.
- Spawn sites are collected including those inside `var x = func(...)` seams (goListDirs, ghCommand, commandFn), per locked spawn_surface. Rejected treating a seam as "not a real call site" — the seam is exactly where a spawn would hide.
- The marker sweep is its own task (t-4) rather than folded into t-1. A single task that both writes the flagger and silences every flag can never show a red; splitting means t-1 lands failing-by-design against the real tree and t-4 is the commit that clears it.
- `survivor drain` gated at the RunE top, not at goListDirs/runCoverageProfile. Gating the seams would refuse after state has been loaded and the phase resolved; requireExecConsent's contract is "first, before any I/O", and drain has two spawn seams that would each need it.
- t-3 written as a proof-first task with a conditional fix. Reading runTestLanes, the consent loop already precedes resolveTestTarget and syncTreeTo, so the deliverable c-7 actually needs is the test that pins it; budgeting a rewrite would be speculative structure.
- No new grant family for drain and no CLI surface for the enumerator — both locked (drain_grant, check_surface), so no task proposes either.
