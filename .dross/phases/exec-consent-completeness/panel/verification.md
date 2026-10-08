# Panel draft — verification lens

Designed backward from the test contracts. For each criterion I wrote the ideal
failing test first, then derived the smallest change that makes it satisfiable.
Two contracts drove the whole shape:

- **"zero findings over internal/ + cmd/" is all-or-nothing.** No partial sweep
  can satisfy it, so the sweep is exactly one task however many files it touches.
- **A gate test that cannot fail is not a gate.** Every enumerator task carries a
  self-test (it must still flag a synthetic ungated spawn) because the failure
  mode of an AST audit is degrading silently into "returns no findings" — the
  precedent at `internal/cmd/subprocargs_audit_test.go:388` (`TestAuditFlagsItsOwnSnippets`)
  exists for exactly that reason and is reused here.

```
Phase exec-consent-completeness — 6 tasks across 3 waves

Wave 1
  t-1  Build the spawn-site enumerator test
       files:    internal/cmd/execgate_audit_test.go
                 internal/cmd/testdata/execgate/snippets.txt
                 internal/cmd/testdata/execgate/ungated.go.txt
       covers:   c-1, c-2, c-4
       contract: exec.Command("cargo", pkg) in an unmarked, ungated snippet is
                 flagged with the same finding shape as a git one — no binary
                 name appears in any table the verdict reads
       contract: a bare `//dross:exec-exempt` with no prose after it still
                 produces a finding whose Why names the marker as reasonless
       contract: the finding rendered for testdata/execgate/ungated.go.txt
                 contains "ungated.go.txt:7", the literal "//dross:exec-exempt"
                 and the word "gate" — file, line, and both remedies
       contract: the walk over internal/ + cmd/ reports scanned > 0 and
                 sites >= 30; a scan that found nothing t.Fatals rather than
                 passing vacuously

  t-2  Gate `survivor drain` behind exec consent
       files:    internal/cmd/survivor_drain.go
                 internal/cmd/trust.go
                 internal/cmd/trust_test.go
                 internal/cmd/survivor_drain_consent_test.go
       covers:   c-3
       contract: `dross survivor drain` in the untrusted gatedFixture returns an
                 error containing "dross trust"; after GrantConsent the same run
                 fails for some other reason or succeeds, never with "dross trust"
       contract: with consent absent, the `go list -f {{.Dir}}` spawn at
                 survivor_drain.go:38 and the `go test -coverprofile` spawn at
                 :189 both record ZERO invocations — the refusal precedes the
                 spawn rather than following it
       contract: TestExecGatedSetIsExplicit's want list is 7 entries including
                 "survivor drain", and its size assertion reads 7 — dropping
                 drain from execGatedCommands fails it

  t-3  Prove lane consent precedes any host contact
       files:    internal/cmd/test_remote_consent_test.go
                 internal/cmd/lane_preview_locality.go
       covers:   c-7
       contract: with spawnRemote (test.go:742) and remoteProbeFn (doctor.go:1396)
                 replaced by counting stubs, a lane whose grant is STALE produces
                 zero spawnRemote calls and zero probe calls, and the refusal
                 names the lane and `dross trust --lane <name>`
       contract: the same with the grant ABSENT — the ssh preflight in
                 resolveTestTarget (test.go:463) and the rsync in syncTreeTo
                 (test.go:500) are both unreached, proven by counter == 0 rather
                 than by output text
       contract: `dross test lane preview --probe` on a lane with no grant does
                 not call pickRemoteTarget (lane_preview_locality.go:110) — a
                 preview that opens an ssh connection is execution on the host

Wave 2 (depends t-1)
  t-4  Add reach attribution to the enumerator
       files:    internal/cmd/execgate_reach_test.go
                 internal/cmd/testdata/execgate/reach/root.go.txt
                 internal/cmd/testdata/execgate/reach/helper.go.txt
       covers:   c-6
       contract: the attribution set for internal/mutation/gremlins.go's
                 exec.Command contains "verify" — the three-hop path
                 verify RunE -> gremlinsAdapter/Adapter.Run -> gremlins.go
                 is followed, so wrapping a spawn in a new package does not
                 hide it
       contract: RECALL, synthetic: root.go.txt's RunE reaches helper.go.txt's
                 exec.Command through two named hops in a different package, and
                 the finding is attributed to root's `Use:` string
       contract: PRECISION: the reach set for `dross doctor` does NOT contain
                 internal/mutation/gremlins.go's spawn. Without this the graph
                 degenerates into "everything reaches everything", every site
                 needs a marker, and the gate half of the verdict is dead code
       contract: a function is gating iff it calls requireExecConsent or an
                 identifier ending in "Consented" — a snippet calling a
                 newly-invented FooConsented gates its callee without any edit
                 to the enumerator

Wave 3 (depends t-1, t-2, t-3, t-4)
  t-5  Resolve every spawn site: gate or mark
       files:    internal/cmd/cleantree.go, internal/cmd/doctor.go,
                 internal/cmd/init.go, internal/cmd/lane_install.go,
                 internal/cmd/milestone_stale.go, internal/cmd/pause.go,
                 internal/cmd/phase.go, internal/cmd/run.go,
                 internal/cmd/ship_recover.go, internal/cmd/statusline.go,
                 internal/cmd/survivor_drain.go, internal/cmd/techdebt.go,
                 internal/cmd/test.go, internal/cmd/update.go,
                 internal/cmd/verify.go, internal/cmd/worktree_files.go,
                 internal/codex/git.go, internal/codex/ast_grep.go,
                 internal/mutation/gremlins.go, internal/mutation/launcher.go,
                 internal/mutation/stryker.go, internal/mutation/stryker_net.go,
                 internal/quality/run.go, internal/remote/remote.go,
                 internal/security/run.go, internal/ship/open.go,
                 internal/techdebt/run.go
       covers:   c-2
       contract: TestEverySpawnSiteGatedOrExempt reports zero findings across
                 internal/ and cmd/ — the single all-or-nothing gate
       contract: every `//dross:exec-exempt` in the tree carries >= 20 characters
                 of prose; a marker reduced to its bare form re-flags exactly
                 its own file:line and nothing else
       contract: internal/mutation/gremlins.go's exec.Command carries NO exempt
                 marker — it is green by reach through verify/test/drain's gate.
                 A marker there would prove the sweep papered over the gate
                 instead of exercising it
       contract: deleting `requireExecConsent()` from verify.go:49 turns
                 the mutation runners' spawns into findings — the gate half of
                 the verdict is load-bearing, not decorative

  t-6  Rewrite the gate's documentation to the enumerated surface
       files:    internal/cmd/trust.go
                 internal/cmd/doctor.go
                 internal/cmd/execgate_doc_test.go
       covers:   c-5
       contract: trust.go's comment above execGatedCommands (currently :698-728)
                 contains neither "CLOSED set" nor "the other four", and names
                 execgate_audit_test.go as the thing that decides the surface —
                 asserted by reading the source file, as redproof_doc_test.go
                 already does for its own prose
       contract: `dross doctor`'s "Exec consent:" section output (doctor.go:1212)
                 describes the surface as every spawn site the enumerator finds,
                 and mentions `dross survivor drain` among what the grant covers
       contract: no .go or assets/*.md file states a count of gated commands as
                 a word or numeral — a doc test greps for "six commands" /
                 "6 commands" and fails on a hit, so the next command joining
                 the set cannot silently falsify the prose
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 enumerator derives sites from source, no command-name list | t-1 |
| c-2 every site gated or marked; unmarked+ungated fails | t-1 (rule), t-5 (tree green) |
| c-3 `survivor drain` refuses in an untrusted tree | t-2 |
| c-4 failure names file, line, remedy, proven by a fixture | t-1 |
| c-5 docs describe the enumerated surface | t-6 |
| c-6 reach, not just direct calls | t-4 |
| c-7 remote lane consent-checked before transfer or execution | t-3 |

All 7 accounted for.

## Judgment calls

- **The sweep is ONE task at 27 files, not three at nine.** Chose atomicity over
  the granularity rule. `TestEverySpawnSiteGatedOrExempt` is a zero-findings
  assertion: any split leaves an intermediate commit with the gate red, which
  collides head-on with this repo's commit-gating discipline (run the check,
  read it, then commit). A task that cannot end green is not a task. Rejected:
  splitting by package, splitting by binary.

- **Enumerator and reach layer are two tasks, not one.** t-1's contracts (flag a
  bare spawn, reject an empty marker, name file/line/remedy) are all satisfiable
  with direct calls only and no call graph. Splitting means t-1 can go green and
  commit before the graph exists, and t-4's precision contract has a working
  enumerator to be measured against. Rejected: one big enumerator task, which
  would have had no green intermediate state.

- **Gating is detected by a NAME RULE, not a roster.** A function gates if it
  calls `requireExecConsent` or any identifier ending in `Consented`. Rejected a
  literal list of the five consent functions: c-1 kills hand-maintained lists,
  and a rule that a newly-invented `FooConsented` satisfies without editing the
  enumerator is testable (t-4's fourth contract) in a way a roster is not.

- **The reach graph resolves callees by name and over-approximates.** No
  go/types, per the locked `enumerator_mechanism` decision — so `a.Run(files)`
  through `mutation.Adapter` cannot be resolved precisely. Over-approximation is
  the safe direction (it demands more markers, never fewer), but unbounded it
  collapses into "everything reaches everything" and kills the gate half of the
  verdict. That is why t-4 carries a PRECISION contract (doctor must not reach
  gremlins.go) alongside the recall one — the two together are what make the
  graph mean something. Rejected: recall-only contracts, which a graph returning
  the full site set for every root would pass.

- **The mutation runners must stay UNMARKED.** Made this an explicit contract
  rather than leaving it to the sweep's judgment. If the easiest way through
  t-5 is marking every site exempt, the phase ships a green test that proves
  nothing. Pinning gremlins.go as marker-free, plus the "delete
  requireExecConsent from verify.go and watch it turn red" contract, is what
  makes the sweep prove the gate works rather than route around it.

- **`lane preview --probe` is inside c-7.** It is not lane dispatch, but
  `pickRemoteTarget` runs `command -v` on the host through
  `remote.Exec` — that is execution on the host, from a command carrying no lane
  grant. Read c-7's "or executed on that host" literally and put it in t-3
  rather than letting the sweep bury it under an exemption marker.

- **c-7 is a proof task, not a rewrite.** I read `runTestLanes`: the consent loop
  (test.go:420-437) already precedes `resolveTestTarget` (:463) and `syncTreeTo`
  (:500). The property holds; nothing pins it, so a refactor that moved the
  probe above the loop would go unnoticed. t-3's contracts are counter-based
  (zero spawns) rather than text-based, because a test asserting only the
  refusal message would still pass on a run that probed the host first and then
  refused.

- **Ordering ("the gate runs FIRST") stays behavioural.** The enumerator says a
  gate exists on the path; it does not say it runs before the spawn. Statically
  proving order needs flow analysis the locked mechanism rules out. Covered
  instead by t-2's and t-3's zero-invocation counters at the specific sites.
  Rejected: attempting a statement-order check in the AST walk, which would be
  right for the trivial `if err := gate(); err != nil` shape at the top of a
  RunE and silently wrong everywhere else.
