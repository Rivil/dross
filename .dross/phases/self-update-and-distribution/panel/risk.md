# Risk-lens decomposition — self-update-and-distribution

Bias: failure modes drive the graph. Each scary surface (partial swap, MITM/checksum,
wrong tarball, stale prune, symlink-vs-copy, dev-version misfire, half-failed bootstrap)
is owned and tested by exactly one task. The wave-1 primitives isolate each risk behind
a pure, hammerable unit test before any orchestration touches a real binary or real ~/.claude.

Phase self-update-and-distribution — 9 tasks across 4 waves

Wave 1
  t-1  Embed all commands+prompts with drift guard
       files:    assets/embed.go, assets/embed_test.go
       covers:   c-1
       contract: Embedding becomes a `//go:embed commands prompts` embed.FS (keeping
                 InteractionPlaybook). Guard test walks on-disk assets/commands/*.md +
                 assets/prompts/*.md and compares names+bytes to the embedded FS.
                 contract: add/rename/delete a file under assets/commands without
                 re-embedding -> the walk finds a name-set or byte mismatch and the
                 guard test fails; deleting prompts/_interaction.md from the FS but not
                 disk also fails it.

  t-2  Update preflight: platform + version decision
       files:    internal/cmd/update_preflight.go, internal/cmd/update_preflight_test.go
       covers:   c-3
       contract: Pure functions: tarballName(goos,goarch,ver) -> "dross_<ver>_<os>_<arch>.tar.gz"
                 matching .goreleaser name_template, and decideUpdate(running, latest)
                 -> {up-to-date | newer | downgrade | needs-confirm}.
                 contract A (wrong tarball): GOOS=darwin/GOARCH=arm64 must yield the
                 darwin_arm64 name and GOOS=windows must return an unsupported-platform
                 error — a regression that drops the arch segment fails the name test.
                 contract B (stale/dev misfire): latest==running -> no update; latest
                 older -> refuse as downgrade; running == the un-overridden dev default
                 ("0.1.0.0"/Commit=="unknown") -> needs-confirm; a flip that treats dev
                 as "newer-than-everything" or auto-downgrades fails the decision table.

  t-3  Checksum verify with hard-refuse
       files:    internal/cmd/update_checksum.go, internal/cmd/update_checksum_test.go
       covers:   c-3
       contract: Parse checksums.txt (sha256<space>filename lines), select the row for
                 the platform tarball, SHA-256 the downloaded bytes, return
                 ErrChecksumMismatch on any difference and the asset's own line missing.
                 contract: flip one byte of the tarball -> verify returns
                 ErrChecksumMismatch (never nil); a checksums.txt that omits our
                 filename -> error, not a silent pass. No swap is reachable past a
                 non-nil return.

  t-4  Atomic binary self-replace
       files:    internal/cmd/update_swap.go, internal/cmd/update_swap_test.go
       covers:   c-3
       contract: Replace the running binary via write-to-temp-in-same-dir + chmod +
                 rename-over (EXDEV/cross-device fallback to copy+rename), so an
                 interrupted write never yields a truncated on-PATH binary.
                 contract: inject a failure between temp-write and rename -> target path
                 still holds the original bytes (byte-equal), zero-length/partial temp
                 is discarded; an in-place os.WriteFile implementation fails this test.
                 Verify resulting file is executable (0o755).

Wave 2 (depends t-1)
  t-5  dross install: symlink-vs-copy + scoped prune
       files:    internal/cmd/install.go, internal/cmd/install_test.go, cmd/dross/main.go
       covers:   c-2
       depends:  t-1
       description: New `dross install` cobra command. Auto-detect source checkout
                 (assets/ present next to cwd/repo root) -> symlink assets/commands ->
                 ~/.claude/skills/dross-<name>/SKILL.md and assets/prompts ->
                 ~/.claude/dross/prompts (live edits); else write real-file copies from
                 t-1's embedded FS. Explicit --copy / --link override. Clean-sync the
                 dross-* namespace: prune dross-* skills/prompts not in this version.
                 Registered in main.go.
       contract: against a temp HOME —
                 (symlink-vs-copy misfire) in a source checkout install yields symlinks
                 whose readlink targets assets/; with --copy (or no checkout) it yields
                 regular files whose bytes equal the embedded FS and zero symlinks — a
                 detection flip is caught by asserting link-vs-regular per mode.
                 (prune deletes non-dross): pre-seed ~/.claude/skills/dross-obsolete/ +
                 a non-dross skill foo/ containing a real user file; after install,
                 dross-obsolete is gone, foo/ AND its file are untouched — a prune that
                 globs beyond dross-* deletes foo and fails the test.

Wave 3
  t-6  dross update command (orchestration)
       files:    internal/cmd/update.go, internal/cmd/update_test.go, cmd/dross/main.go
       covers:   c-3
       depends:  t-2, t-3, t-4, t-5
       description: `dross update` wires preflight (t-2) over a configurable GitHub API
                 base: fetch releases/latest, version-gate, download the platform tarball,
                 verify checksum (t-3), atomically swap (t-4), then re-sync assets via the
                 t-5 install engine. Flags: --check (report only), --force (reinstall
                 regardless). API base + install dir injectable for tests.
       contract: against an httptest server mocking /releases/latest + the tarball asset
                 + checksums.txt —
                 (--check is read-only) --check on a newer release prints the version and
                 leaves the target binary byte-unchanged and assets untouched.
                 (MITM/corruption refuse) mock serves a checksums.txt that doesn't match
                 the served tarball -> update exits non-zero, target binary unchanged
                 (no swap reached) — proves t-3's refuse is honored end-to-end.
                 (semver gate) equal/older latest -> "up to date", no swap; --force on
                 equal -> swap happens. newer -> swap + asset re-sync runs.

  t-7  make install delegates to dross install
       files:    Makefile, internal/cmd/install_symlink_test.go
       covers:   c-2
       depends:  t-5
       description: Rewrite the `install:` target to `build` then `./dross install --link`
                 (source-checkout symlink mode), dropping the inline ln/loop. Extend the
                 existing temp-HOME test to assert every command links, not just
                 _interaction.md.
       contract: extend TestMakeInstall* against temp BIN/SKILLS/PROMPTS — after make
                 install, EVERY assets/commands/dross-*.md has a
                 skills/dross-<name>/SKILL.md symlink resolving to its source file, and
                 the prompts symlink still resolves _interaction.md to source. If the
                 target stops delegating (or dross install regresses to copy in a
                 checkout) a command's SKILL.md is missing or is a regular file and the
                 test fails.

  t-8  curl|sh bootstrap install.sh
       files:    install.sh, .github/workflows/ci.yml
       covers:   c-4
       depends:  t-5
       description: POSIX `set -eu` script: detect uname os/arch -> release tarball name,
                 download latest binary + checksums to a temp dir, verify, install binary
                 to ~/.local/bin, then run `dross install`. Add a shellcheck step to
                 ci.yml. No Go/git assumed.
       contract: (half-way failure) `shellcheck install.sh` passes clean in the new CI
                 step; running the script with an unreachable/404 download URL exits
                 non-zero and leaves NO partial/zero-byte dross on the target bin path
                 (download stages in a temp dir, mv only after checksum) — a script that
                 curls straight onto the bin path would leave a stub and fail this check.
                 Documented smoke run (env-overridable base URL against a local fixture)
                 ends with a working `dross install`.

Wave 4 (depends t-6, t-7, t-8)
  t-9  Document curl|sh + dross update in README
       files:    README.md, internal/cmd/readme_install_test.go
       covers:   c-5
       depends:  t-6, t-7, t-8
       description: Replace the "binary only / needs a checkout" caveat with the
                 supported flow: the curl|sh one-liner and `dross update` (with --check)
                 as the install+update path on any machine.
       contract: a guard test greps README.md and asserts both the
                 `curl -fsSL .../install.sh | sh` one-liner and a `dross update` section
                 are present; if the install entrypoint is renamed without updating docs,
                 the grep test fails (docs can't silently drift from the shipped command).

## Coverage
- c-1: t-1
- c-2: t-5, t-7
- c-3: t-2, t-3, t-4, t-6
- c-4: t-8
- c-5: t-9

## Judgment calls
- Split c-3 into four tasks (preflight / checksum / swap / orchestration) instead of one
  `update` command: rejected the monolith because the four scariest failure modes
  (wrong tarball, MITM, truncated binary, downgrade) each deserve a pure unit test that
  runs without a network or a real binary; t-6 only owns the wiring + httptest path.
- Kept checksum (t-3) and atomic swap (t-4) as separate tasks rather than one
  "download-apply" task: they are the two highest-severity, independently-testable risks
  (corruption-refuse vs partial-write), and a tampered-tarball test and an
  interrupted-swap test exercise disjoint code — merging would blur ownership.
- `dross install` (t-5) depends on t-1 (embedded FS) and is wave 2, not wave 1: its
  copy mode reads the embedded FS, so it can't be authored as a pure wave-1 task; the
  symlink mode alone wouldn't satisfy c-2's copy half.
- make-install (t-7), install.sh (t-8), and update (t-6) are all wave 3 in parallel:
  t-7 and t-8 need only `dross install` (t-5), not `dross update`, so I dropped them out
  of update's wave rather than chaining them behind t-6.
- README (t-9) carries a real grep guard test rather than "doc-only, no test": chose a
  drift-catching test over an untestable criterion so a renamed entrypoint can't leave
  docs lying — matches the repo's existing parity-test idiom.
- install.sh emits a shellcheck CI step in the existing ci.yml rather than a new
  workflow: reuses the established CI surface and satisfies c-4's "passes shellcheck"
  with enforcement, not a one-off local run.
