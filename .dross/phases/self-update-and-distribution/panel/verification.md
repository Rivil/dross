# self-update-and-distribution — verification lens

Designed backward from the test contract for each criterion: the contract names the
surface, the mock, and the failing assertion; the task is the smallest change that
makes that contract satisfiable. Everything here is provable in CI with no real
GitHub release — the GitHub API + asset is an `httptest.Server`, install/prune runs
against a temp HOME, embed drift is a bytes-equal walk, and install.sh is gated by
shellcheck.

```
Phase self-update-and-distribution — 7 tasks across 4 waves

Wave 1
  t-1  Embed all commands+prompts; drift guard
       files:    assets/embed.go, assets/embed_test.go
       covers:   c-1
       contract: TestEmbedDrift walks os.ReadDir(assets/commands) + os.ReadDir(assets/prompts)
                 on disk and, for each *.md, reads the same path from the embedded embed.FS and
                 asserts bytes-equal; it also asserts the embedded file set == the on-disk set.
                 Add/rename/edit an asset without re-embedding (or drop a file from the FS glob)
                 and the test fails naming the drifted path. InteractionPlaybook still resolves
                 from the FS (a sub-assert reads prompts/_interaction.md through it).

  t-4  internal/update: release client + verify
       files:    internal/update/update.go, internal/update/update_test.go
       covers:   c-3
       contract: Against an httptest.Server standing in for api.github.com + the asset host:
                 (a) TestAssetName maps runtime GOOS/GOARCH (darwin/linux × arm64/amd64) to
                 dross_{Version}_{Os}_{Arch}.tar.gz exactly; an unsupported pair errors.
                 (b) TestVerifyChecksum: server serves a tarball + a checksums.txt whose line
                 matches sha256(tarball) → verify returns nil; flip one byte of the served
                 tarball → verify returns a mismatch error and reports no swap.
                 (c) TestNewerOnly: latest=v0.2.0 vs running 0.1.0 → "update available"; equal
                 or older → "up to date"; a dev/unknown running version → "unknown, offer anyway".
                 (d) TestAtomicReplace: replace writes to a temp file in the target dir then
                 os.Rename over a fake binary; on verify failure the original bytes are intact.

Wave 2 (depends t-1)
  t-2  Add `dross install` command
       files:    internal/cmd/install.go, internal/cmd/install_test.go, cmd/dross/main.go
       covers:   c-2
       depends:  t-1
       contract: All against HOME=t.TempDir():
                 (a) TestInstallCopyMode (no source checkout, force copy): ~/.claude/skills/
                 dross-<name>/SKILL.md exists as a REAL file (os.Lstat: mode&Symlink==0) for
                 every embedded command, content == embed.FS bytes; ~/.claude/dross/prompts/
                 <name>.md likewise.
                 (b) TestInstallSymlinkMode (source checkout): SKILL.md is a symlink whose
                 readlink target is the absolute assets/commands/dross-<name>.md.
                 (c) TestInstallPrunesStaleDross: pre-seed ~/.claude/skills/dross-obsolete/
                 SKILL.md → after install it is gone.
                 (d) TestInstallSparesNonDross: pre-seed ~/.claude/skills/my-other/SKILL.md →
                 after install it is untouched (still present, unchanged bytes).

Wave 3
  t-3  Make `make install` delegate to `dross install`
       files:    Makefile, internal/cmd/install_symlink_test.go
       covers:   c-2
       depends:  t-2
       contract: TestMakeInstallDelegates runs `make install` with BIN/SKILLS/PROMPTS pointed at
                 a temp HOME and asserts ~/.claude/skills/dross-*/SKILL.md are symlinks into the
                 checkout (dev result preserved) — proving the recipe shells out to `dross install`
                 rather than its own cp/ln loop. Existing TestMakeInstallLinksInteractionSnippet
                 must still pass (same symlink result through the new path).

  t-5  Add `dross update` command (--check/--force)
       files:    internal/cmd/update.go, internal/cmd/update_test.go, cmd/dross/main.go
       covers:   c-3
       depends:  t-2, t-4
       contract: With internal/update pointed at an httptest base URL (injected via unexported
                 field / functional option):
                 (a) TestUpdateCheckNoApply: `--check` against a newer mock release prints the
                 available version and the fake binary on disk is byte-identical afterward (no swap).
                 (b) TestUpdateAppliesAndResyncs: newer release + valid checksum → binary bytes
                 replaced AND install sync ran (a temp-HOME skill file is now present).
                 (c) TestUpdateForce: `--force` with an equal/older release still applies.
                 (d) TestUpdateRefusesOnBadChecksum: tampered asset → command returns non-nil
                 error and the binary is NOT replaced.

  t-6  Add install.sh bootstrap + shellcheck gate
       files:    install.sh, .github/workflows/ci.yml
       covers:   c-4
       depends:  t-2
       contract: A `shellcheck` job in ci.yml runs `shellcheck install.sh` and fails on any SC
                 finding (script must be clean: quoted vars, `set -euo pipefail`, no SC2086). The
                 script resolves OS/arch to the dross_{Version}_{Os}_{Arch}.tar.gz asset, downloads
                 it from the latest release, places dross on PATH, then execs `dross install`; the
                 documented smoke run (commented in the script header) is the manual half of c-4.

Wave 4 (depends t-5, t-6)
  t-7  Document curl|sh + `dross update` in README
       files:    README.md, internal/cmd/readme_doc_test.go
       covers:   c-5
       depends:  t-5, t-6
       contract: TestReadmeDocumentsInstallAndUpdate reads the repo-root README.md and asserts it
                 contains the install.sh curl one-liner (the raw githubusercontent .../install.sh
                 URL piped to sh) AND a `dross update` invocation. Drop either from the docs and
                 the test fails — keeping prose honest about the supported install/update path.
```

## Coverage

| Criterion | Tasks |
| --------- | ----- |
| c-1 | t-1 |
| c-2 | t-2, t-3 |
| c-3 | t-4, t-5 |
| c-4 | t-6 |
| c-5 | t-7 |

All of c-1..c-5 covered.

## Judgment calls

- Split c-3 into a pure `internal/update` package (t-4) + a thin cobra wrapper (t-5) rather than one fat `update.go`. Reason: the checksum/semver/platform assertions are the load-bearing contract and must run against an `httptest` mock with no filesystem or cobra noise; isolating them in a package keeps the mock injection trivial. Rejected: testing all of c-3 through the command, which forces the httptest base URL through cobra flags and couples unit assertions to CLI plumbing.
- Embed via `embed.FS` over a glob (`//go:embed commands prompts`) rather than one `//go:embed` var per file. Reason: the drift guard can only assert "embedded set == on-disk set" if the FS is enumerable; per-file vars can silently omit a new command and no test would catch it. InteractionPlaybook is re-derived as an FS read so its existing callers keep working.
- t-1 and t-4 are both pure wave 1: the update package needs neither the embed FS nor the install command (asset re-sync is wired in the t-5 wrapper, not the package), so it parallelizes with the embed work instead of waiting.
- Made c-5 testable with a small README-content test (t-7) instead of leaving it as unverifiable prose. Reason: the decomposition rules reject "covered by docs"; a grep-style assert for the curl one-liner + `dross update` gives c-5 a real failing condition. Rejected: a link-checker that dials GitHub (flaky in CI, and the install.sh URL won't 200 until the branch merges).
- semver "strictly newer" comparison (t-4) uses `golang.org/x/mod/semver` (a build-time dep, not a user runtime dep — consistent with the single-static-binary lock) rather than a hand-rolled parser. Rejected: hand-rolling, which risks pre-release/`v` -prefix edge cases the contract's TestNewerOnly would have to re-encode anyway.
- t-3 (Makefile delegation) is kept distinct from t-2 because the locked install_unification decision is only proven by a test that runs `make install` and still sees the symlink dev result — that asserts delegation, which can't be verified from inside the Go `dross install` test alone.
