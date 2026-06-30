Phase self-update-and-distribution — 5 tasks across 3 waves

Wave 1
  t-1  Embed all commands+prompts with drift guard
       files:    /Users/rivil/Development/dross/assets/embed.go
                 /Users/rivil/Development/dross/internal/cmd/embed_assets_test.go
       covers:   c-1
       contract: Replace the single `//go:embed prompts/_interaction.md` with an
                 `embed.FS` over `commands/dross-*.md` + `prompts/*.md` (keep
                 InteractionPlaybook deriving from it). embed_assets_test.go walks
                 the embedded FS and the on-disk assets/commands + assets/prompts
                 trees: if a new dross-foo.md is added on disk but not embedded
                 (wrong glob), the file-set equality assertion fails; if any
                 embedded byte content drifts from its on-disk file, the
                 per-file content-equality assertion fails.

Wave 2 (depends t-1)
  t-2  Add `dross install`; make install delegates
       files:    /Users/rivil/Development/dross/internal/cmd/install.go
                 /Users/rivil/Development/dross/cmd/dross/main.go
                 /Users/rivil/Development/dross/Makefile
                 /Users/rivil/Development/dross/internal/cmd/install_symlink_test.go
       covers:   c-2
       contract: `dross install` (registered in main.go) detects a source checkout
                 (assets/ present beside the binary) → symlinks
                 ~/.claude/skills/dross-*/SKILL.md + ~/.claude/dross/prompts; else
                 writes real-file copies from the embedded FS; `--copy`/`--link`
                 overrides detection; it prunes dross-* skills/prompts not in the
                 current set and leaves non-dross entries. Makefile `install:`
                 becomes `build` + `./dross install`. Tests against a temp HOME:
                 (a) checkout mode → SKILL.md readlink points at assets/commands/*.md;
                 (b) a stale ~/.claude/skills/dross-removed/ is pruned while a
                 ~/.claude/skills/other/ survives — if prune scope widens this fails;
                 (c) copy mode with assets/ absent → SKILL.md is a real file whose
                 bytes equal the embedded asset; (d) temp-HOME `make install`
                 produces the same symlinked skills as `dross install` — if the
                 Makefile stops delegating, the symlink assertion fails.

  t-3  Add `dross update` self-updater
       files:    /Users/rivil/Development/dross/internal/cmd/update.go
                 /Users/rivil/Development/dross/cmd/dross/main.go
                 /Users/rivil/Development/dross/internal/cmd/update_test.go
       covers:   c-3
       contract: `dross update` maps runtime.GOOS/GOARCH to the
                 dross_<ver>_<os>_<arch>.tar.gz asset, fetches the latest release
                 from api_base, downloads the tarball + checksums.txt over HTTPS,
                 SHA-256-verifies, atomically replaces the running binary, then
                 calls t-2's asset-sync. Reuses the install sync function (depends
                 t-2). Tests against an httptest mock of the GitHub releases API +
                 tarball + checksums.txt: a tampered tarball (checksum mismatch)
                 aborts with the binary unchanged — if the verify-and-refuse path
                 breaks, the test observes a swapped binary; a not-strictly-newer
                 latest tag is a no-op, `--check` prints the available version
                 without swapping, `--force` swaps regardless — if semver-newer-only
                 logic breaks these assertions fail; GOOS/GOARCH→asset-name mapping
                 must match .goreleaser.yaml name_template or asset selection fails.

  t-4  Write curl|sh install.sh bootstrap
       files:    /Users/rivil/Development/dross/install.sh
                 /Users/rivil/Development/dross/internal/cmd/install_sh_test.go
       covers:   c-4
       contract: install.sh uname-detects os/arch, downloads the matching
                 dross_*_<os>_<arch>.tar.gz from the latest Rivil/dross release,
                 places dross on PATH, and runs `dross install` (depends t-2 so the
                 smoke run is real). install_sh_test.go runs `shellcheck install.sh`
                 (skips if shellcheck absent) and fails on any SC violation, and
                 asserts the script's asset-name pattern matches the
                 .goreleaser.yaml name_template — if the archive naming drifts, the
                 download URL the script builds would 404 and this assertion fails.

Wave 3 (depends t-2, t-3, t-4)
  t-5  Document install + update in README
       files:    /Users/rivil/Development/dross/README.md
                 /Users/rivil/Development/dross/internal/cmd/readme_install_test.go
       covers:   c-5
       contract: README's install section documents the `curl ... install.sh | sh`
                 one-liner and a `dross update` section as the supported install/
                 update path (superseding the "needs a checkout" note at README:174).
                 readme_install_test.go asserts the README contains the curl|sh
                 one-liner whose raw URL path matches the Rivil/dross repo and
                 mentions `dross update` — if either the install one-liner or the
                 update section is dropped, the guard assertion fails.

## Coverage
- c-1 → t-1
- c-2 → t-2
- c-3 → t-3
- c-4 → t-4
- c-5 → t-5

## Judgment calls
- One task per criterion (5 total): each criterion has a single distinct deliverable surface; no scaffolding task earns its place under the MVP lens. Rejected splitting t-2 into separate "install command" / "Makefile wrapper" / "prune logic" tasks — they share one file set and one test harness, so splitting would breach the too-small rule.
- Folded the c-1 guard test into the embed change itself rather than a follow-up task — the test is meaningless without the FS extension and vice versa; they ship as one atomic change.
- Made t-3 (update) depend on t-2 rather than reimplement asset re-sync — the "re-sync embedded assets" clause of c-3 reuses install's sync function, so t-2 must land first; this is a real output dependency, not hedging.
- Placed install.sh (t-4) in wave 2 depending on t-2, not wave 1: writing+shellchecking the script is independent, but c-4's "documented smoke run" strictly needs `dross install` to exist, so the task as a whole can't complete until t-2 lands.
- README (t-5) is its own wave-3 task, not folded into t-3/t-4: it documents both install.sh (t-4) and `dross update` (t-3), so it can only be accurate after both exist. Rejected merging it into either to avoid a doc that documents a sibling task's unfinished surface.
- Gave each doc/script task a real automatable contract (shellcheck for t-4, a README grep-guard for t-5) instead of a "documented" hand-wave — the rules forbid generic contracts even for non-Go deliverables.
