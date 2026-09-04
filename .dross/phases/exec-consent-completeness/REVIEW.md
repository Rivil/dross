# Plan Review — exec-consent-completeness

Reviewed: 2026-09-04 (second pass, after amendments)
Plan: 10 tasks across 4 waves

## BLOCKING

- [amendment-contradiction] **t-4's newly-exclusive verdict makes at least four
  real spawn sites unreachable-to-green, and t-6 and t-8 both assert zero
  findings over them.** The amendment added "a marker on a site already reached
  by a gated command is itself a finding" on top of the pre-existing row "a site
  reachable from both a gated and an ungated command must be flagged with the
  finding naming the ungated command". Together those two rows leave a
  mixed-reach site with no green state: unmarked it is flagged for the ungated
  reach, marked it is flagged for the gated reach.

  Two mixed-reach sites are in the tree today, both assigned markers by the plan:

  - `internal/cmd/ship_recover.go:265` (`gitTrim`) — reached by the GATED
    `verify` (`verify.go:82` and `:577` → `phaseScope`, `verifyscope.go:36` →
    `gitTrim`) and by roughly fifteen ungated commands, including `status`
    (`status.go:578`, `shippedUnmergedPhase`), `ship`, `milestone`, `phase`,
    `repair`, `topology`, `basebranch` and `forkpoint`. t-6 assigns
    `ship_recover.go` a marker and asserts "the enumerator scoped to these ten
    files must report zero findings".
  - `internal/remote/remote.go:632`, `:677` and `:795` — reached by the GATED
    `test` (`runTestLanes` → `syncTreeTo` → `Sync` → `ignoreRule`; and
    `Exec` → `run` → `commandFn`, the `var commandFn = buildCommand` seam t-4
    explicitly follows) and by the ungated `doctor` (`doctor.go:1396`,
    `remoteProbeFn = remote.Probe` → `Exec`). t-8 assigns all three markers and
    asserts zero findings.

  The escape the plan leaves open — gate the ungated callers — is closed by t-2's
  own contract ("`dross survivor list` and `dross doctor` must still succeed
  unconsented") and by trust.go:706-709's deliberate read-only boundary. So t-6
  contract 1, t-8 contract 1 and t-10 contract 1 ("zero findings across
  internal/ and cmd/") are all unsatisfiable as the plan stands.

  Suggestion: narrow the exclusive rule to sites reached ONLY by gated commands.
  "A marker on a site every reaching command gates is itself a finding" keeps
  the amendment's whole purpose — t-9's four mutation sites are reached only by
  `verify` and (post-t-2) `survivor drain`, both gated, so the rule still bites
  exactly where the sweep could otherwise be cleared by marking — while letting
  a marker resolve a mixed site. Then say in t-4 what a marked mixed site means:
  the marker has to justify the ungated reach, not the gated one.

## FLAG

- [antipatterns] t-7's contract contradicts itself on `update.go`. Row 1: "the
  enumerator must report zero findings for these six files with each verdict
  reading `gated`, never `exempt`". Row 3: "update.go's self-exec of the
  verified new binary must be the only site here allowed a marker". `update.go`
  is one of the six. A marker makes its verdict `exempt`, which row 1 forbids.
  (`dross update` is not in `execGatedCommands`, so under the exclusive verdict
  the marker is the only legal resolution — row 1 is the wrong half.)
  Suggestion: scope row 1 to the five toolchain files and let row 3 own
  `update.go:197`.

- [antipatterns] Command-path membership is undefined: exact match or prefix?
  `execGatedCommands` mixes bare names (`test`, `verify`) with full paths
  (`task status in_progress`, `changes record`), and t-4 attributes a site to
  "a full command path". Whether `test lane preview` counts as gated by the
  entry `test` decides `worktree_files.go:33` — reached only from
  `lane_preview.go:127` — which t-6 marks exempt. Exact match makes the marker
  right; prefix match makes it a finding. The tree supports exact match
  (`test lane preview`'s RunE never calls `requireExecConsent`; only
  `test.go:323`, inside `test`'s own RunE, does), but nothing in the plan says
  so.
  Suggestion: state in t-4 that membership is an exact match on the full
  command path, and add a contract row pinning it.

- [test-contract] (carried, still open) t-2 contract 4 bumps
  `TestExecGatedSetIsExplicit`'s size assertion 6 → 7 but still adds no row to
  `TestGatedCommandsRefuse`, which is the exact remedy that assertion's own
  failure message names (trust_test.go:363). The table holds 5 rows for 6 gated
  commands today (`test` has none); t-2 widens the drift to 2. `trust_test.go`
  is in t-2's `files`, so there is nowhere else this lands.
  Suggestion: add a `survivor drain` row (and a `test` row) to the contract, or
  correct the assertion's message and the stale comment at trust_test.go:360
  ("drives exactly these five").

- [test-contract] (carried, still open) t-5 contract 2 — a repo-wide grep for
  `six commands` / `6 commands` — is vacuous. Neither string exists in the tree;
  the only live count phrasing is `the other four` at trust.go:714, already
  caught by contract 1. It cannot go red on current code, and a future restated
  count will most likely read "seven commands", which this row does not match.
  Suggestion: make it pattern-based (digit or spelled-out number followed by
  `commands` near `execGatedCommands`), or drop it.

- [locked-decision-conflict] (carried, still open) t-5 contract 5 requires
  `dross doctor` to print the token `//dross:exec-exempt`. Locked
  `check_surface`'s *why* rejects a user-facing surface because "a dross user's
  repo has no exec.Command sites to enumerate, so a user-facing surface would be
  dead in every tree but this one" — and the marker syntax is useful only to a
  dross contributor. Not a conflict with the choice text (no new section; the
  existing one at doctor.go:1212 is edited), so still a judgement call.
  Suggestion: keep the marker token in trust.go's package comment and drop the
  row from doctor's section.

- [granularity] (carried, still open) t-3 bundles three separable concerns
  across 5 files: a proof-only leg on dispatch (test.go), a proof-only leg on
  bootstrap (remote_bootstrap.go), and the only real behaviour change —
  gating `--probe` in lane_preview.go / lane_preview_locality.go. A red on the
  preview leg blocks committing the dispatch proofs.
  Suggestion: split the preview `--probe` gate into its own wave-1 task.

- [granularity] (carried, now worse) t-8 spans 8 files across 7 packages and two
  reason classes — scanner/report spawns (codex, quality, security, techdebt,
  ship) and the transport spawn (internal/remote), which carries half of c-7 and
  has its own prose contract. The amendment added `execconsent_audit_test.go`,
  making it an enumerator edit as well. Unlike t-6, the description still does
  not argue for the span.
  Suggestion: split internal/remote (plus its enumerator rule) into its own
  task, or add t-6's "one reason class" justification.

- [antipatterns] (carried, hardened by the amendment) t-8 contract 2 —
  remote.go's marker "must be REJECTED when its prose does not mention the
  caller-side consent check" — is now definitely landing inside the enumerator,
  since t-8 gained `execconsent_audit_test.go`. That is a hand-maintained
  per-site rule of the same family c-1 exists to kill, moved from the
  command-name side to the marker side, and it has no general form.
  Suggestion: enforce it as a fixed docs-test assertion over remote.go's marker
  text (t-5's pattern), not as a grammar rule inside the verdict.

- [forbidden-actions] (carried, still open) Project rule r-01 (`hard`) requires
  `make install` after editing prompts under `assets/`. t-5 edits three prompts
  and no task mentions the re-link. The docs test reads source, not the
  installed copy, so the phase's own gate stays green while the installed
  prompts go stale.
  Suggestion: add `make install` to t-5's completion steps.

## NOTE

- [amendments] Prior blocker 1 is CLOSED. The `spawn_surface` lock now reads
  "Files ending in _test.go are outside the surface", with a why that gives the
  actual reason (a spawn in a test file runs under `go test`, which is the
  command the gate authorizes) and cites subprocargs_audit_test.go:345.
  t-1's description inherits it and contract row 8 pins it. The census is
  unchanged and still exact: 41 non-test spawn sites across 27 files, matching
  t-10's stated measurement to the digit, and t-6 (10) + t-7 (6) + t-8 (7,
  excluding the enumerator) + t-9 (4) = 27 with no file doubled or orphaned.

- [amendments] Prior blocker 2 is CLOSED. The 20-char prose floor is now t-1
  contract row 9 (t-1 owns the enumerator); t-6's row 3 survives but is a claim
  about t-6's own marker prose, which t-6 can control. The
  marker-on-a-gated-site rule is now t-4 contract row 9 (t-4 owns the
  enumerator); t-9 row 1 and t-7 row 1 consume it through their `depends_on`.
  t-8 gained `execconsent_audit_test.go`, so its remote.go prose rule now has a
  file. All three assertions have a home. (What the amendment did NOT do is
  check the new exclusive verdict against the real call graph — see BLOCKING.)

- [antipatterns] Line-number drift, uncorrected from the first pass: t-5 cites
  trust.go:716 for "The other four"; it is at :714. Every other cited line
  resolves exactly — survivor_drain.go:38/:189, test.go:428/:463/:500/:742,
  remote_bootstrap.go:231, doctor.go:1212/:1267, verify.go:49, run.go:262,
  trust.go:698, trust_test.go:362, subprocargs_audit_test.go:345/:417/:548.
  `RunConsented`, `ReplayConsented`, `LaneConsented` and `LaneInstallConsented`
  all exist as named.

- [antipatterns] `auditRoots` (subprocargs_audit_test.go:330) is package-level
  and shared with the existing subprocargs audit. t-10's row "removing
  `internal` from the roots must drop the count below the floor" has to operate
  on a local copy; mutating the shared var would perturb the other audit.

- [antipatterns] `preview_exit_status`, which t-3 cites as locked, is locked in
  `.dross/phases/lane-selector-preview/spec.toml:49`, not in this phase's spec.
  Real, not dangling, but t-3's executor has to go find it.

- [coverage] Every criterion is covered: c-1 (t-1, t-10), c-2 (t-1, t-4, t-6,
  t-7, t-8, t-9, t-10), c-3 (t-2), c-4 (t-1), c-5 (t-5), c-6 (t-4, t-7, t-9),
  c-7 (t-3). No orphans.

- [wave-order] Every dependency is load-bearing. t-4 needs t-1's walk; t-5 needs
  t-2 (doctor must name `survivor drain`) and t-1 (the function name it pins);
  t-6/t-7/t-8/t-9 need t-4's verdict; t-7 additionally needs t-2's gate; t-10
  needs the whole sweep clean. Nothing is sequenced for tidiness.

- [strengths] t-4's PRECISION row is verified against the tree and correct:
  `internal/mutation` is imported only by `verify.go`, `survivor_drain.go` and
  `internal/verify`, and the only caller of `verify.RunScoped` is
  `verify.go:137` — so `doctor` genuinely does not reach gremlins.go's spawn,
  and t-9's "gated via verify" verdict holds. The discarded-result row is also
  true to the code: doctor.go:1267's `LaneConsented` really is display-only and
  verify.go:49's `requireExecConsent` really is acted on.

- [strengths] t-3's contracts assert on counters over seams (`zero spawnRemote
  calls`, `counter == 0`, `zero remoteExecFn install calls`) rather than output
  text — the only shape that proves a refusal landed before the wire rather
  than merely alongside it.

- [strengths] Not one vague test contract in 10 tasks. Every row names the
  mutation it is meant to catch and the surface that breaks, and several
  (t-1's `found no spawn sites`, t-10's discovery floor, t-4's fail-closed row)
  are anti-vacuity guards on the gate itself.

## Summary

Both prior blockers are genuinely closed, but the tightening that closed the
second one introduced a new one: the exclusive verdict plus the mixed-reach rule
leaves `ship_recover.go:265` and `internal/remote/remote.go`'s three sites with
no green state, so t-6, t-8 and t-10 cannot pass as written — narrow the
exclusive rule to sites reached only by gated commands and the plan is ready.
