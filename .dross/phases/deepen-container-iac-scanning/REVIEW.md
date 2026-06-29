# Plan Review — deepen-container-iac-scanning

Reviewed: 2026-06-27 (re-review)
Plan: 11 tasks across 3 waves

## BLOCKING
- (none)

## FLAG
- (none)

## NOTE
- [prior-blocking / resolved] The prior BLOCKING (deferred guard-flip → red-commit
  window) is genuinely fixed. Verified against internal/stack/embed_test.go: the
  forbidding assertions live at lines 52-54 (TestEmbeddedDocker: errors if loadout
  ships `dockle`/`checkov`) and 115-117 (TestEmbeddedTerraform: errors if it ships
  `checkov`/`dockle`). t-5 now (a) lists internal/stack/embed_test.go in `files`,
  (b) states in its description that it flips both assertions to *require* the new
  tools in the SAME commit as the terraform.toml/docker.toml additions, and (c) adds
  the test-contract row "if the assertions are added ... without the .toml change in
  the same commit, go test fails inside this task's own commit." t-5 adds checkov to
  terraform.toml and dockle to docker.toml in that same commit, so the flipped
  present-assertions pass and t-5's commit is green standalone. No test in waves 1→3
  is left asserting the old (forbidding) contract. Window closed.
- [wave-order / serialization] t-5 (wave 1) and t-7 (wave 3) both edit
  embed_test.go, and t-7 `depends_on` includes t-5 — confirmed safe. t-5 flips the
  two *existing* functions (TestEmbeddedDocker/Terraform); t-7 *appends* two new
  functions (TestEmbeddedKubernetes/CloudFormation). Different regions of the file,
  serialized by the dependency, and no other wave-2 task (t-3 edits only
  kubernetes.toml, t-4 only cloudformation.toml) touches embed_test.go. No
  conflict, no ordering hazard. The t-5→t-7 dependency is correctly justified
  precisely by this shared-file serialization, not just by the new profiles.
- [coverage / c-3 resolved] The prior c-3 FLAG (no recon/manifest-level dockle
  assertion) is addressed: t-8 now `covers` c-3 and adds the row "if dockle stops
  appearing in the docker security manifest, TestBuildManifestMarkerDockerDockle
  (installed-vs-missing) fails," explicitly mirroring TestBuildManifestMarkerDocker
  (recon_test.go:105-119, which asserts hadolint/trivy config presence). The
  surfacing (recon, t-8 + the t-5 profile entry) and the no-image skip-with-reason
  (scan path, t-2 TestDockleNoImageSkipsWithReason + t-6) now both have tests. By
  design these live in different layers: the manifest reports dockle
  installed-vs-missing by binary presence, while the "installed-but-no-image →
  skipped-with-reason" semantics are exercised only at the decision-fn/run layer.
  That split is consistent with c-3's wording and acceptable.
- [intermediate-state] t-5 (wave 1) registers dockle as a plain docker.toml scanner,
  but the no-image scan-path guard isn't wired until t-6 (wave 2). Between those
  commits, `dross security run` would treat dockle like any scanner (no no-image
  skip). This breaks no test gate (no run-path test asserts dockle behavior before
  t-2/t-6 add them) and the phase only ships after all waves, so it is not a
  red-commit window — just a transient that resolves at t-6. Flagged for awareness,
  not action.
- [accuracy / resolved] The prior off-by-one NOTE is gone: t-5's references
  ("embed_test.go:115-117" and ":52-54") match the actual blocks exactly, and t-7 no
  longer cites line numbers.
- [granularity] t-11 still touches 5 files (deployment.yaml, template.yaml,
  Dockerfile, RUN.md, expected-finding.txt). Mechanical 5+ threshold triggers, but it
  is one cohesive fixture directory mirroring fixtures/terraform-c3 — no split
  warranted. Recorded only because the heuristic fires.
- [coverage] Every criterion c-1..c-8 appears in at least one task's `covers`
  (c-1: t-1/t-3/t-7/t-8; c-2: t-1/t-4/t-7/t-8; c-3: t-2/t-5/t-8; c-4:
  t-3/t-4/t-5/t-7/t-8; c-5: t-11; c-6: t-10; c-7: t-3/t-4/t-9; c-8: t-2/t-6).
- [locked-decisions] No conflicts. content-sniff `all=[apiVersion,kind]` (t-3) vs
  `any=[AWSTemplateFormatVersion,Resources]` (t-4); `trivy config` reused with a
  distinct Name for dedup survival; dockle-never-builds (t-2 TestDockleNeverBuilds);
  checkov kept side-by-side with trivy config (t-8) — all match spec.toml decisions.
- [forbidden-actions] Only rules.toml r-01 (make install before relying on changes)
  applies; t-11's RUN.md encodes the make-install manual-run discipline. No
  violations.
- [strength] Test contracts remain unusually specific — named functions and precise
  failure conditions throughout, the opposite of the "tests pass" antipattern.

## Summary
The prior BLOCKING is resolved — t-5 now flips the terraform/docker forbidding
guards atomically with the .toml additions (verified against embed_test.go:52-54 and
115-117), the t-5↔t-7 shared-file edits are safely serialized by dependency, and the
prior c-3 FLAG is closed by t-8's manifest-level dockle assertion; no new red-commit
window or inconsistency was introduced, so the plan is ready to execute.
