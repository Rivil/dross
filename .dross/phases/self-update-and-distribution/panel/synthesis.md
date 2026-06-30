# Synthesis — self-update-and-distribution

## Scores

Grades: A (strong) / B (adequate) / C (weak). One verdict per draft per dimension.

| Dimension | risk (9t/4w) | mvp (5t/3w) | verification (7t/4w) |
| --- | --- | --- | --- |
| Criteria coverage | A — all c-1..c-5, each owned once | A — all c-1..c-5, one task per criterion | A — all c-1..c-5, c-2 & c-3 each split across 2 |
| Test-contract specificity | A — sharpest failure-injection (partial-swap byte-equal, glob-overreach prune with a real user file, 404 leaves no stub) | B — real contracts but coarse; one bundle contract for install+Makefile+prune, one for all of update | A — named test fns (TestEmbedDrift/AssetName/VerifyChecksum/AtomicReplace), httptest + Lstat-mode checks |
| Granularity | B — over-split: c-3 into 4 `internal/cmd/*` files, no package boundary | C — under-split: t-2 fuses install+Makefile+prune; t-3 fuses all of c-3 | A — balanced: pure `internal/update` pkg vs thin cobra wrapper; make-install its own task |
| Wave correctness | A — w1 primitives pure & parallel, deps honored | C — **bug**: w2 holds t-2,t-3,t-4 but t-3/t-4 both `depends t-2` (must be a later wave) | A — t-1 & t-4 both pure w1; all deps satisfied by prior wave |

**Skeleton: verification.** It has the cleanest granularity (the `internal/update` package isolates the load-bearing checksum/semver/swap contract from cobra so it tests against an httptest mock with no CLI plumbing), correct waves, and named test contracts. Risk is the close runner-up and supplies the sharpest failure-injection assertions, which are grafted in. MVP contributes a concrete README anchor but loses on the wave bug and coarse bundling.

## Merged plan

Phase self-update-and-distribution — 7 tasks across 4 waves. Paths repo-relative.

### Wave 1

```
t-1  Embed all commands+prompts; drift guard            [mvp+risk+verification]
     files:      assets/embed.go, assets/embed_test.go
     covers:     c-1
     depends_on: []
     contract:   Replace the single //go:embed prompts/_interaction.md with an
                 embed.FS over commands/dross-*.md + prompts/*.md (InteractionPlaybook
                 re-derives from the FS; a sub-assert reads prompts/_interaction.md
                 through it). TestEmbedDrift walks os.ReadDir(assets/commands) +
                 os.ReadDir(assets/prompts) and asserts, per *.md, embedded bytes ==
                 on-disk bytes, plus embedded file-set == on-disk file-set. Failure
                 modes (from risk): add/rename a dross-foo.md on disk but not in the
                 glob -> set-equality fails naming the drifted path; edit a byte without
                 re-embedding -> content-equality fails; delete prompts/_interaction.md
                 from disk but not the FS -> also fails.

t-2  internal/update: release client + verify           [verification+risk]
     files:      internal/update/update.go, internal/update/update_test.go
     covers:     c-3
     depends_on: []
     contract:   Pure package, no cobra. Against an httptest.Server standing in for
                 api.github.com + the asset host:
                 (a) TestAssetName: runtime GOOS/GOARCH (darwin/linux x arm64/amd64) ->
                     dross_{Version}_{Os}_{Arch}.tar.gz exactly, matching the
                     .goreleaser name_template; an unsupported pair (e.g. windows)
                     returns an unsupported-platform error -- a regression dropping the
                     arch segment fails the name test. [risk graft]
                 (b) TestVerifyChecksum: parse checksums.txt (sha256<space>filename),
                     select our platform row, sha256 the bytes -> nil on match; flip one
                     byte -> ErrChecksumMismatch (never nil); checksums.txt that omits
                     our filename -> error, not a silent pass. [risk graft]
                 (c) TestNewerOnly: latest strictly-newer -> "update available"; equal or
                     older -> "up to date" / refuse-downgrade; dev/unknown running
                     version ("0.1.0.0" / Commit=="unknown") -> needs-confirm/offer
                     anyway. semver via golang.org/x/mod/semver (build-time dep only,
                     consistent with the static-binary lock). [risk graft on dev-default]
                 (d) TestAtomicReplace: write to a temp file in the target dir, chmod
                     0o755, rename over; inject a failure between temp-write and rename ->
                     target still holds the original bytes (byte-equal), partial temp
                     discarded; result file is executable. [risk graft]
```

### Wave 2 (depends t-1)

```
t-3  Add `dross install` command                        [mvp+risk+verification]
     files:      internal/cmd/install.go, internal/cmd/install_test.go,
                 cmd/dross/main.go
     covers:     c-2
     depends_on: [t-1]
     contract:   Registered in main.go. Auto-detect source checkout (assets/ present)
                 -> symlink assets/commands -> ~/.claude/skills/dross-<name>/SKILL.md and
                 assets/prompts -> ~/.claude/dross/prompts (live edits); else write
                 real-file copies from t-1's embedded FS. Explicit --copy / --link
                 override detection. [risk+mvp graft] Clean-sync the dross-* namespace.
                 All against HOME=t.TempDir():
                 (a) TestInstallCopyMode (force copy): SKILL.md is a REAL file
                     (Lstat mode&Symlink==0), bytes == embed.FS bytes; prompts likewise.
                 (b) TestInstallSymlinkMode (checkout): SKILL.md is a symlink whose
                     readlink target is the absolute assets/commands/dross-<name>.md.
                 (c) TestInstallPrunesStaleDross: pre-seed dross-obsolete/ -> gone.
                 (d) TestInstallSparesNonDross: pre-seed a NON-dross skill foo/ containing
                     a real user file -> after install foo/ AND its file are untouched;
                     a prune that globs beyond dross-* deletes foo and fails. [risk graft:
                     assert the inner file, not just the dir]
```

### Wave 3

```
t-4  make install delegates to `dross install`          [risk+verification]
     files:      Makefile, internal/cmd/install_symlink_test.go
     covers:     c-2
     depends_on: [t-3]
     contract:   Rewrite install: to `build` + `./dross install --link`, dropping the
                 inline ln/loop. TestMakeInstallDelegates runs `make install` with
                 BIN/SKILLS/PROMPTS pointed at a temp HOME and asserts EVERY
                 assets/commands/dross-*.md has a skills/dross-<name>/SKILL.md symlink
                 resolving to its source file (not just _interaction.md) [risk graft] --
                 proving the recipe shells out rather than running its own cp/ln loop.
                 Existing TestMakeInstallLinksInteractionSnippet must still pass.

t-5  Add `dross update` command (--check/--force)        [verification+risk]
     files:      internal/cmd/update.go, internal/cmd/update_test.go,
                 cmd/dross/main.go
     covers:     c-3
     depends_on: [t-2, t-3]
     contract:   Thin cobra wrapper over t-2's internal/update, with the API base URL
                 injectable via functional option / unexported field. Re-syncs assets via
                 t-3's install engine after swap. Against an httptest mock of
                 /releases/latest + tarball + checksums.txt:
                 (a) TestUpdateCheckNoApply: --check on a newer release prints the version
                     and leaves the target binary byte-identical AND assets untouched
                     (no swap). [risk graft: assets-untouched]
                 (b) TestUpdateAppliesAndResyncs: newer + valid checksum -> binary bytes
                     replaced AND install sync ran (temp-HOME skill now present).
                 (c) TestUpdateForce: --force on equal/older still applies.
                 (d) TestUpdateRefusesOnBadChecksum / MITM: checksums.txt not matching the
                     served tarball -> non-zero, target binary unchanged, no swap reached
                     end-to-end. [risk graft: end-to-end MITM]

t-6  install.sh bootstrap + shellcheck gate             [risk+verification]
     files:      install.sh, .github/workflows/ci.yml
     covers:     c-4
     depends_on: [t-3]
     contract:   POSIX `set -euo pipefail` script: uname-detect os/arch -> the
                 dross_{Version}_{Os}_{Arch}.tar.gz asset (pattern must match the
                 .goreleaser name_template), download latest binary + checksums to a TEMP
                 dir, verify, mv onto ~/.local/bin only after checksum, then exec
                 `dross install`. A shellcheck job in ci.yml runs `shellcheck install.sh`
                 and fails on any SC finding (quoted vars, no SC2086).
                 Failure mode (risk graft): running with an unreachable/404 download URL
                 exits non-zero and leaves NO partial/zero-byte dross on the bin path
                 (staging-in-temp proves it) -- a script that curls straight onto the bin
                 path would leave a stub and fail. Documented smoke run uses an
                 env-overridable base URL against a local fixture and ends in a working
                 `dross install`.
                 NOTE for execution: editing ci.yml triggers the CI supply-chain
                 hardening checklist (pin action SHAs, minimal permissions:) per global
                 rules -- audit the new job, do not just append it.
```

### Wave 4 (depends t-5, t-6)

```
t-7  Document curl|sh + `dross update` in README         [mvp+risk+verification]
     files:      README.md, internal/cmd/readme_doc_test.go
     covers:     c-5
     depends_on: [t-5, t-6]
     contract:   Replace the "binary only / needs a checkout" caveat (the note at
                 README:174) [mvp graft] with the supported flow: the
                 `curl -fsSL .../install.sh | sh` one-liner (raw githubusercontent URL
                 matching Rivil/dross) and a `dross update` section incl. --check [risk
                 graft]. TestReadmeDocumentsInstallAndUpdate greps README.md and asserts
                 both the curl one-liner and a `dross update` invocation are present; rename
                 the entrypoint without updating docs -> the grep fails.
```

### Coverage

| Criterion | Tasks |
| --- | --- |
| c-1 | t-1 |
| c-2 | t-3, t-4 |
| c-3 | t-2, t-5 |
| c-4 | t-6 |
| c-5 | t-7 |

## Disagreements

### D1 — How many tasks for c-3 (the updater)?
- **risk**: 4 tasks — preflight (platform+version), checksum, atomic-swap, orchestration — each a separate `internal/cmd/update_*.go` file, no package boundary.
- **mvp**: 1 task — a single `internal/cmd/update.go` doing everything.
- **verification**: 2 tasks — a pure `internal/update` package (asset-name + checksum + newer-only + atomic-replace) plus a thin `internal/cmd/update.go` cobra wrapper.
- **Provisional default**: verification's 2-task split, into the `internal/update` *package* (not risk's four `internal/cmd` files). Risk's four failure-injection assertions are folded in as sub-tests of t-2.
- **Why it matters**: the checksum/semver/swap assertions are the load-bearing safety contract; isolating them in a package keeps httptest injection trivial and off the cobra path (mvp's monolith couples unit assertions to CLI flags). But four separate `internal/cmd` files (risk) over-isolate without a reusable package and blur the "one update surface" boundary. Two tasks is the balance; if execution finds the package contract too dense to gate atomically, fall back toward risk's checksum/swap split.

### D2 — Is `make install` its own task, or folded into `dross install`?
- **mvp**: folded — one task does the install command, the prune, and the Makefile rewrite.
- **risk** & **verification**: separate task for the Makefile delegation.
- **Provisional default**: separate task (t-4), following the 2-1 majority.
- **Why it matters**: the locked `install_unification` decision is only *proven* by a test that runs `make install` and still observes the symlink dev result — a harness that shells out to make, which cannot run inside the Go `dross install` test. Folding it (mvp) would leave delegation asserted only by inspection, and bundle two test harnesses behind one commit gate.

### D3 — Does install.sh need a partial-write failure test?
- **risk**: yes — a 404/unreachable URL must exit non-zero and leave NO partial/zero-byte binary on the bin path (download stages in temp, mv only after checksum).
- **mvp** & **verification**: only shellcheck + asset-name-pattern match; no failure-injection.
- **Provisional default**: include risk's partial-write contract in t-6.
- **Why it matters**: a half-failed bootstrap that drops a truncated `dross` onto PATH is exactly the bricking failure c-4 ("leaving working dross slash commands") guards against; shellcheck alone never exercises it. Low cost to assert, high blast radius if absent.

### D4 — Does the updater core depend on `dross install`, and which wave?
- **mvp**: the whole update task `depends t-2` (install) and sits in Wave 2 — which also produces a wave bug (t-3/t-4 share t-2's wave despite depending on it).
- **risk** & **verification**: extract a pure update layer that depends on neither embed nor install, placing it in Wave 1 (parallel with the embed work); only the *command/orchestration* wrapper depends on install (for the post-swap asset re-sync).
- **Provisional default**: extract the pure layer (t-2) into Wave 1 with no install dependency; only the wrapper (t-5) depends on t-3.
- **Why it matters**: the asset re-sync is wired in the wrapper, not the verify/swap core, so the core has no real dependency on install — pinning it behind install (mvp) serializes unnecessarily and is what produced mvp's wave-ordering error. Keeping it pure shortens the critical path and lets the highest-risk code land and be hammered first.
