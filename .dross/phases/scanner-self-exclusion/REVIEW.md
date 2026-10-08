# Plan Review — scanner-self-exclusion

Reviewed: 2026-09-12
Plan: 7 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [antipattern: description contradicts own contract] t-3's regex as written in the description — `(?i)\b(id|key)\b["']?\s*[:=]\s*["']?[0-9a-f]{16}["']?` — has no boundary after the 16 hex chars, so a 17/18/32-hex value matches too (the first 16 chars satisfy `{16}` and the trailing quote is optional). Verified live on the installed gitleaks 8.30.1: a tree with `"key": "b91bfa24fdf586c0a"` (17-hex) is allowlisted by that config (exit 0). The contract's MUST-NOT rows for 17-hex, 18-hex and 32-hex will therefore fail against the regex the executor is told to write.
  Suggestion: change the description's pattern to `…[0-9a-f]{16}\b["']?`. Verified: with `\b` the 17-hex line is reported and the 16-hex line is still allowlisted. The contract is right; the description is wrong.

- [test-contract: TestSkipDirsSingleDefinition is not implementable as a grep] The literal `"testdata"` already appears in a non-test .go file outside detect.go: `internal/verify/verify.go:359` (`if seg == "testdata"` in `IsTestdataPath`). A plain grep for the literal fails on day one. The contract's qualifier "in a map/slice literal" is doing all the work, and a text regex that distinguishes `"testdata":` / `"testdata",` from `== "testdata"` is fragile.
  Suggestion: state the mechanism in the contract — a `go/ast` walk over `internal/**/*.go` (non-test) looking for the string literal inside a `CompositeLit`, the same technique `internal/cmd/hermetic_dross_read_test.go` already uses — and name `verify.IsTestdataPath` as the known, out-of-scope sibling (mutation-scope rule, Go `testdata` semantics only, no `fixtures`).

- [scope widening not named] t-6 replaces trackedFiles' `.git`/`.dross` check with `stack.SkipDir`, so the tech-debt scan also drops `build`, `dist`, `.idea`, `.vscode`, `node_modules` and `vendor`. c-3 names only testdata/fixtures. This is mandated by the locked `skip_dir_set` (one shared definition), so it is not a conflict — but tracked `build/` content in adopter repos (build scripts, Dockerfiles) silently leaves the scan, and no test or doc in the plan pins that this is intentional.
  Suggestion: name the full set in t-6's description and in the trackedFiles doc comment; add a row to TestTrackedFilesSkipsFixtureDirs asserting a tracked `build/x.sh` is dropped, so the behaviour is deliberate rather than incidental.

- [wave-order] t-7 sits in wave 3 depending on t-5, but its only test is a text-needle check on secure.md; nothing it produces needs t-5's compiled output. It could run in wave 2 alongside t-5/t-6.
  Suggestion: drop to wave 2 unless the author specifically wants the prompt wording to trail detect's final output string (`exclusions:`), in which case keep it and say so in the description.

## NOTE
- [coverage] All five criteria are covered: c-1 (t-1), c-2 (t-2, t-4, t-6), c-3 (t-1, t-6), c-4 (t-3, t-5, t-7), c-5 (t-5, t-7).
- [locked-decisions] No conflicts. The apparent tension between `skip_dir_set` ("the gitleaks allowlist emitter consumes the shared definition") and `allowlist_scope` ("no path exclusion") is resolved by t-3 using the set only in the allowlist `description` string, with TestGitleaksConfigNotPathExcluded pinning that no `paths` key exists. The executor should not "improve" this into a `paths` entry.
- [forbidden-actions] r-01 (`make install` before relying on prompt/binary changes) is called out in t-7. runtime.mode is native; `go test` directly is permitted. No violations.
- [files] `internal/security/gitleaks.go`, `internal/techdebt/filter.go`, `internal/cmd/techdebt_selfscan_test.go` (and the two `_test.go` siblings) do not exist; each is created by its own task. t-3 says "New file"; t-4 and t-6 do not — cosmetic.
- [line-refs] Every line reference checked is current: detect.go:27 skipDirs, project.go:299 Mutation, recon.go:16 Manifest, security.go:193 writeRunReport, techdebt.go:68 trackedFiles, scan.go:47 markerRe, security_test.go:343, secure_prompt_test.go:29, json_tag_parity_test.go:48, and the three existing trackedFiles tests. `pathfence.WriteFile` and `pathfence.Contain` exist with the shapes t-3 assumes. `loadProject()` (cmd/project.go:123) resolves via FindRoot, matching how the techdebt RunE already finds its root.
- [thresholds] t-6's synthetic fixtures (700 lines, one 500-char line) do trip DefaultThresholds (600 / 400).
- [entropy margin] t-3's live-test sample `b91bfa24fdf586c0` has Shannon entropy 3.578 — only 0.08 above gitleaks' generic-api-key threshold of 3.5 (the contract's "does NOT fire" sample `50919a010c495368` is 3.156, confirmed). The positive control will fire on 8.30.1, but the margin is thin; an id with 16 distinct hex chars (entropy 4.0) would be a sturdier positive control. Failure direction is safe either way (the no-config positive control goes red, not silently green).
- [gitleaks syntax] `[extend] useDefault = true` + top-level `[[allowlists]]` + `regexTarget = "line"` verified working on 8.30.1; the `gitleaks git … -- .` fenced-operand form t-7 prescribes is also accepted. Older gitleaks (<8.21) used singular `[allowlist]`; no version floor is recorded anywhere, and the live test only guards whatever version is on PATH.
- [test cost] TestGitleaksDrossTreeNoIdentityHits runs `gitleaks git` over the full 440-commit history from `go test`. It is `-short`-gated and PATH-gated as written; given the laptop memory-pressure history on this repo's suite, keep it that way.
- [c-1 feasibility] Checked: outside the skip set, the only tracked extensions are go/md/toml/yml/sh/ps1/etc., and no profile declares sh, ps1, yml or md as a signal ext, so "exactly [go]" is reachable by skipping testdata/fixtures alone.
- [strengths] (1) Contracts are unusually concrete: red-today evidence, positive controls (gitleaks without `--config` must still report), and over-exclusion bounds in the self-scan test so a match-all exclude cannot pass vacuously. (2) Every locked decision has a pinning test — single-definition grep, no `paths` key, project.toml knob round-trip, run-dir delivery checked by `os.Stat` on the path the report names. (3) t-3 and t-4 are pure (no `stack` import), which keeps wave 1 genuinely parallel and puts the wiring in exactly two wave-2 tasks.

## Summary
Plan is sound and unusually well-pinned; fix t-3's regex boundary (the description's pattern fails its own MUST-NOT rows — verified against the real tool) and restate t-1's single-definition test as an AST check before executing.
