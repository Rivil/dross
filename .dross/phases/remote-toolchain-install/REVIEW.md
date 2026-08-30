# Plan Review — remote-toolchain-install

Reviewed: 2026-08-29
Plan: 9 tasks across 5 waves

## BLOCKING
(none)

## FLAG

- [antipattern:same-symbol] t-2's description claims it "is the SOLE owner of both install exec
  seams — the remote one and a new `localInstallFn` argv seam". The remote seam already exists:
  `var remoteExecFn = remote.Exec` at `internal/cmd/remote_bootstrap_cmd.go:20`, in a file t-2 does
  not list. An executor reading "sole owner" literally either edits an unlisted file that t-7 owns,
  or declares a second remote seam in `lane_install.go` — at which point bootstrap's tests stub one
  seam and the lane verb's tests stub the other, and t-3's "BOTH of t-2's install seams' call
  counters are 0" proof for c-5 is asserted against the wrong var. t-6 and t-7 both name
  `remoteExecFn`, so the intent is clearly reuse; the sentence contradicts it.
  Suggestion: reword t-2 to own `localInstallFn` and the single `["sh","-c",line]` renderer, and to
  REUSE the existing `remoteExecFn`. If the seam is genuinely meant to move, add
  `internal/cmd/remote_bootstrap_cmd.go` to t-2's files.

- [coverage:docs] t-8 documents only `README.md`. Its own named precedent,
  `lane_toolchain_docs_test.go`, checks README *and* `assets/prompts/options.md`
  (`TestOptionsNamesToolchainAndTheSplit`, line 78). Separately,
  `options_docs_test.go:TestOptionsCoversTheConsentVerbs` enumerates exactly this family —
  `dross trust --lane`, `trusted_lane_commands`, `dross test lane add` — with the comment that a
  per-lane grant is "the one a settings surface is most likely to miss". This phase adds a second
  consent verb (`dross trust --lane-install`) and a second non-settable grant key
  (`trusted_lane_installs`), and neither will appear in options.md. It will NOT fail any existing
  test: that list is hand-typed, and `TestOptionsCoversEveryLocalKey` walks `localKeys`, which t-4
  deliberately keeps the key out of. Silent drift on the surface that claims to reach every setting.
  Suggestion: add `assets/prompts/options.md` to t-8's files and a contract line pinning
  `--lane-install` / `trusted_lane_installs` there, plus the negative arm the precedent has
  (options.md must not tell the user `local set trusted_lane_installs`).

- [granularity] t-7 touches 5 files and carries three separable concerns: widening
  `remoteProbeTools`' return in `doctor.go` (which changes a shared signature and its one caller at
  `doctor.go:1524`), the planner in `remote_bootstrap.go`, and the counters/exit ladder inlined in
  `remoteBootstrap`'s RunE (`remote_bootstrap_cmd.go:84-118`). Its 10 contract lines are the longest
  in the plan. The doctor widening is independently testable and is what both other halves sit on.
  Suggestion: consider splitting the `remoteProbeTools` lane-attribution widening (plus the
  "doctor's Remote output is unchanged" assertion) into its own wave-3 task.

- [test-contract:c-6] The both-machines-missing case is unspecified. t-6's rule is "gap on the
  granted host → install there; host fine, or no grant, or --local → install here", which reads on
  the host-missing branch — but the repo already distinguishes that case explicitly:
  `toolchainFailure` (`internal/cmd/lane_locality.go:~192`) has dedicated wording for "neither host
  has %s — it is missing on %s and on this machine". None of t-6's ten contract lines names it, so
  the behaviour is whatever falls out of the branch order.
  Suggestion: add a contract line fixing the both-missing outcome (install on the host, install on
  both, or refuse and say so) — it is the case a user is most likely to hit first.

- [coverage:attribution] t-7 asserts c-5's bootstrap half — "if the dry-run default leaks, a test
  asserting no lane step reaches remoteExecFn without --apply fails (c-5)" — but does not list c-5
  in `covers`. c-5 says "BOTH install surfaces are dry-run by default"; only t-6's surface claims it.
  Suggestion: add `c-5` to t-7's covers.

- [wave-order] t-3 is in wave 2 depending on t-2, and the plan is honest that this is for a test
  stub only ("the zero-call assertion is made against t-2's exec seams"). Its production change —
  appending the invocation string to `laneFallbackLine` — needs nothing t-2 creates. It could run in
  wave 1 with the zero-call assertion moved to t-6 or t-9.
  Suggestion: minor; leave it if you prefer the assertion co-located with the change it guards.

## NOTE

- [claim-check] t-7 names `reportBootstrap`. No such function exists — the
  `installed, refused, failed` counters and the exit ladder are inline in `remoteBootstrap`'s RunE
  (`remote_bootstrap_cmd.go:84-118`). t-7 does list that file, so the work lands correctly; only the
  symbol is invented.

- [claim-check] t-1 cites "validate.go:227" for the prepare rule; it is at `validate.go:236`. Also,
  `laneProblems`' precedent is mixed rather than uniform: prepare is inline (236), but toolchain
  goes through `laneToolchainProblems` (239). The inline choice is defensible — the whitespace rule
  is one line — but "the prepare precedent is inline" is a selective reading of a file that shows
  both shapes.

- [locked-decisions] `install_transport`, cited by t-2, is not in this phase's spec — it is locked at
  `.dross/phases/remote-toolchain-bootstrap/spec.toml:47` and is already honoured in the existing
  `remoteExecFn` comment. Legitimate inherited lock; noted so a cold reader does not hunt for it here.

- [claim-check] Four load-bearing claims verified true against the tree:
  `RevokeLaneConsent`'s `if _, ok := l.TrustedLaneCommands[name]; !ok { return nil }` short-circuit
  (`trust.go:389`) is real, so t-5's install-grant-only asymmetric case is a genuine latent bug, not a
  hypothetical; `localKeys` really does omit `trusted_lane_commands` with a stated reason
  (`local.go:258-262`), so t-4's precedent holds; `remoteProbeTools` (`doctor.go:1435`) really does
  union `laneToolUnion` already, so t-7's "widen THERE, never re-derive" is the correct shape; and
  `laneVerdict.Announce` plus `laneFallbackLine`'s two silent arms (`lane_locality.go:141,165-170`)
  match t-3's contract wording exactly.

- [coverage] All nine criteria appear in at least one `covers`: c-1(t-3), c-2(t-7,t-8), c-3(t-2,t-7),
  c-4(t-1,t-2), c-5(t-3,t-6), c-6(t-6,t-8), c-7(t-1,t-4,t-5), c-8(t-2,t-5,t-6,t-7), c-9(t-9).

- [locked-decisions] All seven locked decisions are honoured, and six of them have a named guard test
  rather than a prose promise — `install_scope_for_lanes` by the runtime-refusal-row test plus the
  structural no-apt/brew/curl sweep, `offer_scope` by the "never contains `dross remote bootstrap`"
  assertion, `undeclared_exit` at both the type level (t-2) and the exit level (t-6, t-7),
  `install_local_override` by the `--local`-on-a-host-gap test, `install_consent` by t-4's
  byte-identity assertion. No task description, file, or contract contradicts a lock.

- [forbidden-actions] `.dross/rules.toml` holds one rule (r-01, `make install` before relying on a
  prompt or binary change). Nothing in the plan implies a violation; t-8's asset edit is a README
  row, not a prompt symlink. No global rules file exists at `~/.claude/dross/rules.toml`.

- [strength] The test contracts are unusually falsifiable: call counters rather than prose ("BOTH
  seams called ZERO times", "remoteExecFn once and localInstallFn zero"), byte-identity rather than
  "unchanged" (t-4's `laneConsentLine` assertion, t-9's local.toml/project.toml comparison), and two
  structural sweeps that survive rows being added (t-2's no-installer-prefix sweep, its
  never-both-Unknown-and-Refusal table walk).

- [strength] t-6's last contract line — resolving t-3's offered invocation through `rootCmd.Find` —
  closes the gap where a printed remedy outlives the command it names. That is the failure mode c-1
  invites, and it is caught in the task that owns the command rather than assumed.

- [strength] t-9 exists at all. c-9 is a negative invariant ("nothing is written"), the kind usually
  left implicit; asserting it across both surfaces, including that locality does not consult the
  install grant, is the right shape for it. It is a single test file, which normally reads as a merge
  candidate, but it spans t-6's and t-7's surfaces and so cannot fold cleanly into either.

## Summary
No blockers; the plan is well-grounded in the actual tree, but t-2's claim to own a `remoteExecFn`
that already lives in t-7's file and t-8's omission of `assets/prompts/options.md` should both be
fixed before execution.
