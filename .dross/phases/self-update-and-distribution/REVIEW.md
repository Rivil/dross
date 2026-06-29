# Plan Review — self-update-and-distribution

Reviewed: 2026-06-29
Plan: 7 tasks across 4 waves

## BLOCKING
(none)

All five criteria are covered (c-1→t-1, c-2→t-3+t-4, c-3→t-2+t-5, c-4→t-6,
c-5→t-7). No task contradicts a locked decision. No forbidden action: runtime is
`native`, tests use `go test`, t-4 honours r-01 by routing `make install`
through `dross install`, and install.sh is the shipped artifact (not a build
shortcut). No docker/pnpm conflicts.

## FLAG
- [correctness] t-5 says "re-syncs assets via t-3's install engine after swap."
  Run in-process, that engine syncs the assets embedded in the *currently
  running* (old) binary — not the just-downloaded new binary. So after `dross
  update` you'd swap in a new binary but re-install the OLD version's
  skills/prompts. c-3's "then re-syncs the embedded assets" only makes sense if
  the NEW assets land. TestUpdateAppliesAndResyncs ("skill now present") would
  pass either way and not catch this.
  Suggestion: re-sync by exec'ing the freshly-swapped binary (`<newbin>
  install`) rather than calling the install engine in-process, and add a
  contract that the re-synced asset bytes match the NEW binary's embed, not the
  old one.

- [coverage] c-2 and the locked `asset_sync_policy` require pruning stale
  dross-* *prompts* as well as skills, but t-3's test_contract only asserts
  skill pruning (TestInstallPrunesStaleDross seeds `dross-obsolete/`, a skill)
  and non-dross sparing. Nothing asserts a dropped prompt file (e.g. a removed
  `assets/prompts/foo.md`) is pruned from `~/.claude/dross/prompts/` in copy
  mode. In symlink-to-dir mode it's moot, but copy-mode installs can leave stale
  prompt files.
  Suggestion: add a t-3 contract asserting a pre-seeded stale dross prompt file
  is removed after a copy-mode install.

- [test-contract] The asset name template
  (`dross_{Version}_{Os}_{Arch}.tar.gz`) is hand-replicated in three places —
  `.goreleaser.yaml` (the source of truth), t-2's update.go, and t-6's
  install.sh — but no task modifies `.goreleaser.yaml` and no test reads it.
  The contracts say "matching the .goreleaser name_template," yet that match is
  asserted by eye, not by a test. If the template ever changes, update.go and
  install.sh silently break. Related: goreleaser v2 `.Os`/`.Arch` are lowercase
  GOOS/GOARCH (`darwin`,`amd64`), while `uname -s`/`uname -m` yield
  `Darwin`/`x86_64` — install.sh must lowercase and map (x86_64→amd64,
  aarch64→arm64), and t-6's "asset name matches name_template" contract doesn't
  pin that mapping per platform.
  Suggestion: pick one authority for the name (a Go const tested against the
  literal in `.goreleaser.yaml`, or a documented shared format) and add a t-6
  contract exercising the uname→goreleaser os/arch mapping for at least
  darwin/arm64 and linux/amd64.

## NOTE
- [locked-decisions] The dev/unknown sentinel in t-2's TestNewerOnly
  (`Version==0.1.0.0`, `Commit==unknown`) matches the real defaults in
  `internal/cmd/version.go` (Version = "0.1.0.0", Commit = "unknown") and
  goreleaser's ldflags set Version from the tag. The plan correctly keeps the
  binary's release version distinct from the 4-part `.dross` state version, per
  `update_version_semantics`. Note "0.1.0.0" is not valid semver, so the
  comparator must route dev builds via the Commit=="unknown" check, not a semver
  parse — the contract already does.
- [granularity] t-1 changes the type/derivation of `InteractionPlaybook`
  (currently `//go:embed prompts/_interaction.md` → derived from an embed.FS).
  No contract guards that existing consumers (`dross interaction show`) still
  emit it verbatim; rely on the existing interaction tests to catch a
  regression.
- [strength] Test contracts are unusually specific and adversarial — each names
  the surface that breaks and includes a negative mutant ("flip one byte →
  ErrChecksumMismatch (never nil)", "404 leaves no stub on the bin path", "a
  prune globbing beyond dross-* deletes foo and fails", set-equality vs
  byte-equality split in the drift guard). This is exactly the specificity
  check 3 wants.
- [strength] Wave graph is clean and minimal: t-1/t-2 are genuinely independent
  in wave 1; every wave-3 task strictly needs t-3 (install engine) and/or t-2;
  no task sits a wave later than its dependencies force.
- [strength] t-6 explicitly flags the CI supply-chain hardening checklist when
  editing ci.yml (per the global rule) instead of blindly appending a job, and
  the plan reuses existing infra (.goreleaser.yaml name_template, ldflags
  Version/Commit) rather than reinventing it.

## Summary
Solid, well-sequenced plan with excellent test contracts; no blockers, but
confirm `dross update` re-syncs the NEW binary's assets (not the old in-process
embeds) and close the prompt-pruning and asset-name-consistency gaps before
executing.
