# Plan Review — supply-chain-currency

Reviewed: 2026-09-14
Plan: 4 tasks across 2 waves

## BLOCKING
(none)

## FLAG
- [antipattern] t-1 assumes `go-version-file: go.mod` makes setup-go install the `toolchain` line. It does not on the pinned action. `actions/setup-go@3041bf5` (v5.2.0) `parseGoVersionFile` matches only `^go (\d+...)` — verified against `src/installer.ts` at that SHA. With `go 1.27.0` + `toolchain go1.27.1`, CI installs go1.27.0 and go1.27.1 arrives on first `go` invocation via GOTOOLCHAIN=auto from the pinned GOPROXY (checksum-verified through GOSUMDB, so still hardened — but it is exactly the "declared version is a fiction" mechanism the current ci.yml comment argues against). Newer setup-go (main; latest tag v7.0.0) does read `toolchain` under the same `go-version-file` key. t-2 declares action pin bumps out of scope, so as planned the two-stage mechanism ships and the rewritten comments must not claim otherwise.
  Suggestion: pick one in t-1 — (a) bump the setup-go SHA in both workflows as part of t-1 (a one-line, checklist-audited pin edit; the spec's deferred item is Dependabot, not individual pins, so nothing locked forbids it), or (b) keep v5.2.0 and have the rewritten comments state precisely that setup-go installs the `go` directive and `toolchain` takes effect via GOTOOLCHAIN=auto. Either way, t-1's c-1/c-2 evidence should include the CI log's govulncheck banner (`Using go1.27.1 and govulncheck@...`) or a `go version` step — otherwise nothing observed proves 1.27.1 was the scanned toolchain.

- [antipattern] t-2's "four direct deps" is the wrong set. go.mod has five direct requires (aead.dev/minisign v0.3.0 omitted — already latest, harmless), and `go mod tidy -diff` on today's tree moves `github.com/spf13/pflag` from indirect to direct because `internal/cmd/flag_hint.go` imports it. cobra v1.10.2's go.mod requires pflag v1.0.9; latest is v1.0.10 (`go list -m -u all`). Executing the enumerated list + tidy leaves pflag at v1.0.9 as a direct dep, and c-3 ("direct dependencies are at their latest release") is unmet.
  Suggestion: add `github.com/spf13/pflag v1.0.10` to t-2's bump list, and record `go list -m -u all` showing no bracketed update on any direct dep as the c-3 evidence — `go mod tidy -diff` alone cannot detect a tidy-but-stale dep.

- [test-contract] t-2 contract 2 names the wrong surface. `commands_parity_test.go` is an assets/commands ↔ assets/prompts filesystem parity check and the `*_prompt_test.go` files assert on prompt markdown — neither renders anything through cobra. No test in internal/cmd asserts on cobra-rendered `Usage:` text. What would actually break on cobra 1.8→1.10 is `flag_hint_test.go` (asserts on cobra-owned error strings: "unknown flag", "Did you mean") plus the seven tests that `Execute()` a command tree.
  Suggestion: reword to "if cobra 1.8→1.10 changes unknown-flag error wording or command dispatch, flag_hint_test.go and the Execute()-driven tests under internal/cmd fail".

- [antipattern] t-3's description edits ci.yml ("pinned to the same tag ci.yml uses — bump both to the current latest") but `files` lists only release.yml, .goreleaser.yaml and the new test. Latest golang.org/x/vuln is v1.8.0; ci.yml pins v1.6.0.
  Suggestion: add `.github/workflows/ci.yml` to t-3's files so the atomic commit scope matches the description and TestGovulncheckPinAgrees is green on t-3's own commit.

- [forbidden-actions] Global CLAUDE.md requires auditing *every* CI workflow edit against `~/.claude/memory/reference_ci_supply_chain_hardening.md`. Only t-1 says so; t-2 (ci.yml), t-3 (release.yml + ci.yml) and t-4 (release.yml) all edit workflows too. Not a violation — the rule applies at execute time regardless — but the plan reads as if only t-1 needs the audit.
  Suggestion: one line in t-2/t-3/t-4 descriptions, or drop the line from t-1 so no task implies the others are exempt.

## NOTE
- [wave-order] Wave 1's three tasks all edit release.yml (t-1: setup-go block; t-3: steps inserted right after it; t-4: the tag steps above it) and t-1/t-3 both edit ci.yml. Fine under `dross task next` sequential execution, but wave 1 is not fan-out-parallelisable — a worktree fan-out would conflict on the setup-go block. Waves here are checkpoint boundaries, not parallelism claims.
- [wave-order] t-2's dependency on t-1 is genuinely required (tidy must run against the final `go` directive) — not an artificial serialisation.
- [antipattern] The tree is untidy today (pflag indirect→direct). t-3 removes goreleaser's `go mod tidy` hook and sets `-mod=readonly` in wave 1, before t-2 tidies in wave 2. Safe: `-mod=readonly` only rejects a go.mod that needs *requirement* changes to build, not `// indirect` comment drift, and release only fires on main. But `go mod tidy -diff` is red on the branch until t-2 lands — the executor should not read that as a t-3 regression.
- [test-contract] `GOFLAGS: -mod=readonly` at job level does not break the new `go mod tidy -diff` step — verified locally: it exits 1 with the diff, not a "flag not defined" error. `go install pkg@version` under the same env is already proven by ci.yml.
- [test-contract] c-1 has no in-repo regression test; its contract is "CI step + executor's observed local exit 0". Honest and acceptable — govulncheck needs network and a vuln DB, so a hermetic Go test can't cover it. The local run must happen with go.mod's toolchain switch active (local machine is go1.26.5): record govulncheck's "Using goX.Y.Z" banner, not just the exit code.
- [antipattern] go1.27.1 exists on proxy.golang.org (`golang.org/toolchain/@v/list` carries `v0.0.1-go1.27.1.linux-amd64`) — t-1 is executable as written.
- [forbidden-actions] rules.toml r-01: t-2 changes the compiled binary's deps; `make install` (rm `~/.local/bin/dross` first — in-place overwrite SIGKILLs) before any dross-driven verify step. helicon's gremlins host (go1.26.1) will download go1.27.1 once via GOTOOLCHAIN=auto on the first mutation leg; that leg already runs close to the harness reaper, so launch it detached.
- [strengths] Every contract names a test function and the exact mutation that trips it — no "tests pass" anywhere. t-3's TestGovulncheckPinAgrees and t-1's TestToolchainSingleSource guard the *two-source drift* the phase exists to kill, not just today's values. t-4's line-based scanner with an explicit env:/with: false-positive contract fits a repo with no YAML dependency and matches the existing readRepoFile content-guard idiom in release_signing_test.go. Scoping GitHub Action pins out (t-2) keeps the phase point-in-time as the spec's deferred item intends — with the one exception flagged above where the setup-go pin is load-bearing for c-2's mechanism.

## Summary
No blocking issues; coverage and locked decisions are clean, but t-1 rests on a setup-go capability the pinned v5.2.0 lacks (toolchain directive is ignored — 1.27.1 arrives via GOTOOLCHAIN auto-switch, not the action) and t-2's dep enumeration misses pflag, which as written leaves c-3 unmet.
