# MVP draft — container-iac-scanning

Lens: smallest task set that satisfies every criterion. The locked decision says
the profile is a data-driven drop-in `.toml` requiring NO Go code change — the
detection/manifest plumbing (`MarkerProfiles`, `BuildManifest`, `profileScanners`,
`profileAnalyzers`) is already generic and proven by the docker sibling. So the
production change is exactly one TOML file; everything else is tests, a fixture,
and docs. No new Go production code is justified by any criterion, so none is
planned.

```
Phase container-iac-scanning — Container & IaC scanning — 3 tasks across 2 waves

Wave 1
  t-1  Add + document terraform marker profile
       files:    internal/stack/profiles/terraform.toml
                 internal/stack/embed_test.go
                 internal/stack/profiles/README.md
       covers:   c-1, c-2, c-4
       desc:     New marker profile mirroring docker.toml — header comment (marker
                 stack; dockle/checkov out of scope), file_patterns = *.tf,
                 *.tf.json, *.tfvars, *.tfvars.json, *.hcl; a `trivy config`
                 scanner (bin=trivy, distinct Name) and a `tflint` analyzer with
                 dimension=error-handling. Add TestEmbeddedTerraform mirroring
                 TestEmbeddedDocker. Add a terraform entry to README.md.
       contract: TestEmbeddedTerraform fails if terraform.toml omits the
                 `trivy config` scanner, declares any [signals].exts (which would
                 regress the marker stack into a primary detected language), or
                 sets tflint's dimension to anything but "error-handling".

Wave 2 (depends t-1)
  t-2  Assert terraform tools surface in both recon manifests
       files:    internal/security/recon_test.go
                 internal/quality/recon_test.go
       covers:   c-1, c-2, c-3
       desc:     Mirror the docker marker recon tests for terraform.
                 Security: TestBuildManifestMarkerTerraform — a *.tf-only repo's
                 manifest contains `trivy config` distinct from the agnostic
                 `trivy`. Quality: TestQualityManifestMarkerTerraform — a *.tf
                 repo surfaces the `tflint` analyzer on top of scc/jscpd.
                 Skip path (c-3 Go half): under an all-missing lookup, tflint /
                 trivy config land in Skipped() each with a non-empty Install
                 hint and BuildManifest returns nil (no abort).
       contract: security test fails if a *.tf tree's manifest lacks `trivy config`
                 or the name-dedup collapses it into agnostic `trivy`; quality test
                 fails if `tflint` is absent or the agnostic scc/jscpd were dropped
                 by the marker union; skip test fails if a missing tflint carries an
                 empty Install hint or BuildManifest returns a non-nil error.
       depends:  t-1

  t-3  Commit IaC fixture + manual-run record
       files:    fixtures/container-iac-c3/main.tf
                 fixtures/container-iac-c3/RUN.md
       covers:   c-3
       desc:     Minimal Terraform fixture with one tflint-catchable defect scc/
                 jscpd are blind to (an unused variable declaration / deprecated
                 "${var.x}" interpolation — default tflint ruleset, no cloud
                 plugin). RUN.md records the real `dross security detect` /
                 `dross quality detect` output listing `trivy config` + `tflint`
                 with installed-vs-missing status and install hint, plus tflint
                 surfacing the planted finding while scc/jscpd report nothing.
                 Mirror fixtures/multilang-c3/RUN.md. (make install first, per r-01.)
       contract: RUN.md is reproducible against the committed fixture — re-running
                 `dross quality detect` on fixtures/container-iac-c3 must still list
                 tflint with its install hint; if terraform.toml stops surfacing the
                 IaC tools, the recorded detect output no longer matches the rerun.
       depends:  t-1
```

## Coverage

| Criterion | Tasks | How |
| --------- | ----- | --- |
| c-1 | t-1, t-2 | t-1 ships the profile with `trivy config` + marker shape (TestEmbeddedTerraform); t-2 proves both recon manifests surface `trivy config` for a *.tf repo, distinct from agnostic trivy. |
| c-2 | t-1, t-2 | t-1 declares `tflint` analyzer at dimension error-handling (embed test guards the dimension); t-2 proves quality recon surfaces tflint on top of scc/jscpd. |
| c-3 | t-2, t-3 | t-2 is the Go-testable half (missing trivy/tflint → Skipped() with install hint, never a silent all-clear); t-3 is the committed fixture + documented manual-run record (mirrors multilang-analyzer-catalogs c-3). |
| c-4 | t-1 | Header comment in terraform.toml (marker stack; dockle/checkov out of scope) + a discoverability entry in profiles/README.md. |

Every criterion c-1..c-4 is covered.

## Judgment calls

- Zero Go production tasks: chose to treat the profile as the only production
  artifact (locked decision) — rejected adding any helper/registration code, since
  no criterion needs it and the docker sibling proves the generic path works.
- Folded the embed-shape guard into t-1 rather than a 4th wave-2 task: chose to
  keep the profile and its guard test together (the test asserts the exact thing
  t-1 authors) — rejected a standalone "shape test" task as redundant ceremony.
- Folded README into t-1 (3 files, wave 1) instead of its own wave-1 task: chose
  this over a <10-min standalone doc task (too-small rule) — the README entry and
  the toml header are the same c-4 deliverable, so they ship together.
- Merged the c-3 Go assertion into the same task as the marker surfacing tests
  (t-2): chose one recon-test task covering surfacing + skip-path across both
  packages — rejected splitting security and quality into separate tasks (same
  layer, two files, no dependency between them; splitting buys nothing).
- One combined fixture for both security and quality detect (t-3): chose a single
  *.tf fixture exercised by both detect commands — rejected separate fixtures per
  command; the same tree drives both manifests.
- README lists terraform (and mentions docker for parity) under a brief
  marker-profile note: chose the minimum that makes the drop-in discoverable per
  c-4 — rejected authoring a full marker-profile catalog section (speculative).
