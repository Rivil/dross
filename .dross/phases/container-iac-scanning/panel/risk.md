# RISK lens — container-iac-scanning

Bias: failure modes drive the graph. The thing that can actually break here is
not "does a new TOML parse" — it is **matching**: a glob that over-fires
(`*.hcl` catching Packer, `*.tf` accidentally swallowing `*.tfstate`), a glob
that under-fires (`*.tf.json` silently never matched because someone assumed
`*.tf` covers it), a dedup that eats `trivy config` because it collides with the
agnostic `trivy`, and a missing tool that reads as a clean all-clear instead of
"skipped, install X". Each of those is owned by exactly one task below.

```
Phase container-iac-scanning — 5 tasks across 2 waves

Wave 1
  t-1  Author terraform profile + README entry
       files:    internal/stack/profiles/terraform.toml
                 internal/stack/profiles/README.md
       desc:     New marker-file profile id="terraform". signals.file_patterns =
                 ["*.tf","*.tf.json","*.tfvars","*.tfvars.json","*.hcl"]. One
                 scanner [[tools]] name="trivy config" bin="trivy" kind="scanner"
                 core=true; one analyzer [[tools]] name="tflint" kind="analyzer"
                 dimension="error-handling" core=true. Header comment mirroring
                 docker.toml: marker-file stack, *.hcl's accepted Packer/Nomad
                 false-positive, why checkov/tfsec are out of scope. Add a
                 terraform row to README.md.
       covers:   c-4
       contract: Decode(terraform.toml) is rejected by stack.Validate / dropped
                 from Embedded() if the id is empty or any [[tools]] entry is
                 malformed; t-2's TestTerraformProfileLoads fails if id!="terraform",
                 any of the 5 file_patterns is missing, the scanner's bin!="trivy",
                 or tflint.dimension!="error-handling". README guard: terraform must
                 appear in the profiles README list (asserted in t-2).

Wave 2 (all depend t-1)
  t-2  Marker-detection + load guards (glob / false-positive risk)
       files:    internal/stack/detect_test.go
       desc:     Add TestTerraformProfileLoads (Embedded()/ByID resolves terraform;
                 assert scanner trivy-config bin=trivy, analyzer tflint dim=
                 error-handling) and TestTerraformMarkerPatterns over the real
                 Embedded() set via MarkerProfiles.
       covers:   c-1
       contract: - one sub-row per pattern: a tree with exactly one of main.tf /
                   vars.tf.json / prod.tfvars / prod.tfvars.json / providers.hcl
                   surfaces "terraform" — dropping any single pattern from the TOML
                   fails that row (proves *.tf does NOT silently cover *.tf.json).
                 - false-positive guard: main.tfstate, notes.txt, and module.tf.bak
                   surface NO terraform (filepath.Match("*.tf", "x.tfstate") must
                   not fire); a marker-less Go repo surfaces nothing.
                 - case-insensitive: a file "Main.TF" still surfaces terraform
                   (regresses if MatchesFile stops lower-casing).
                 - directory guard: a *directory* named "stuff.tf" surfaces nothing
                   (only files count — guards the d.IsDir() skip in the walk).

  t-3  Security manifest: scanner surface + dedup + missing (dedup-collision risk)
       files:    internal/security/recon_test.go
       desc:     Mirror TestBuildManifestMarkerDocker/Dedup for terraform.
                 TestBuildManifestMarkerTerraform, a dedup case, and a
                 missing-skipped case.
       covers:   c-1, c-3
       contract: - a *.tf-only repo's manifest contains "trivy config".
                 - dedup: a go.mod + main.go + main.tf repo yields "trivy" AND
                   "trivy config" each EXACTLY once, and keeps govulncheck/gitleaks
                   — renaming the profile scanner to bare "trivy" would collapse it
                   into the agnostic entry and fail the count.
                 - under an all-missing lookup "trivy config" is in Skipped() with a
                   non-empty Install hint and BuildManifest returns nil (no abort) —
                   a missing trivy reads as "skipped, install X", never all-clear.

  t-4  Quality manifest: tflint analyzer + dimension + missing (dimension/partial-failure risk)
       files:    internal/quality/recon_test.go
       desc:     Mirror TestQualityManifestMarkerDocker for terraform.
                 TestQualityManifestMarkerTerraform plus a missing-skipped case.
       covers:   c-2, c-3
       contract: - a *.tf-only repo's manifest contains "tflint" with Dimension==
                   ErrorHandling, ON TOP of scc + jscpd (which must remain) — so a
                   .tf repo is not left with only the agnostic fallback; flipping
                   tflint's dimension in the TOML fails the dimension assertion.
                 - under an all-missing lookup tflint is in Skipped() with a
                   non-empty Install hint and BuildManifest returns nil, not an
                   error — a missing tflint degrades, never aborts the run.

  t-5  Commit c-3 IaC fixture + manual-run record (proof-of-finding risk)
       files:    fixtures/iac-c3/tf-misconfig/main.tf
                 fixtures/iac-c3/RUN.md
                 fixtures/iac-c3/expected-finding.txt
       desc:     Minimal terraform fixture with one planted misconfig (e.g. an
                 aws_security_group ingress open to 0.0.0.0/0, or an unencrypted
                 aws_s3_bucket). RUN.md records a real run (after `make install`,
                 rule r-01): `dross security detect` / `dross quality detect` list
                 trivy-config / tflint with installed-vs-missing + hint; `trivy
                 config` (and/or tflint) surfaces the misconfig; the agnostic
                 fallback (trivy fs / gitleaks / scc / jscpd) is blind to it.
                 expected-finding.txt pins the flagged AVD/rule id + line. Mirrors
                 fixtures/multilang-c3/. (verify.toml citation is added by
                 dross-verify, not here.)
       covers:   c-3
       contract: expected-finding.txt pins the exact `trivy config` finding (rule
                 id @ main.tf:line) that the planted misconfig produces; if the
                 misconfig is removed/renamed the recorded run in RUN.md no longer
                 reproduces and the pinned line no longer matches — the same
                 fixture+record proof the prior phase used for its c-3.
```

## Coverage

| Criterion | Tasks |
| --------- | ----- |
| c-1 | t-2 (marker detection of *.tf etc.), t-3 (trivy config in security manifest) |
| c-2 | t-4 (tflint analyzer under error-handling, not just scc/jscpd fallback) |
| c-3 | t-3 (security skipped+hint detect-output), t-4 (quality skipped+hint detect-output), t-5 (committed fixture + manual-run record) |
| c-4 | t-1 (header comment in terraform.toml + README.md entry) |

Every criterion c-1..c-4 is owned.

## Judgment calls

- **Split detection (t-2) from the two recon tests (t-3/t-4)** rather than one
  "add terraform everywhere" task: the glob/false-positive failure mode lives in
  the stack layer and the dedup/missing-tool failure modes live in the recon
  layer — different surfaces, different blast radius, so each gets one owner.
- **Folded the c-3 Go-testable half into t-3/t-4** (Skipped()+Install-hint
  assertions) and kept only the finding-proof in t-5. Rejected a standalone
  cmd-layer detect test: the manifest assertion is the same surface the spec says
  CAN be a Go test, and the cmd printer is already covered by existing
  security_test/quality_test.
- **Per-pattern sub-rows in t-2's contract** instead of one "terraform detected"
  assertion. The real risk is a developer assuming `*.tf` covers `*.tf.json` /
  `*.tfvars`; only an independent row per pattern catches a dropped glob, so the
  contract names all five.
- **t-1 ships pure data + docs only**, with its validation/pattern guards living
  in t-2. Rejected putting a guard test in t-1 too — that would split detection-
  layer test ownership across two tasks and blur who owns a glob regression.
- **No exclusion logic for the *.hcl Packer/Nomad false positive.** It is a
  locked, accepted trade-off; t-2 therefore does NOT assert *.hcl is rejected
  (that would contradict the marker_patterns decision) — it only guards the
  genuinely-wrong matches (.tfstate, .tf.bak, dirs).
- **t-5 writes fixtures + RUN.md + expected-finding.txt but NOT verify.toml** —
  mirroring multilang-analyzer-catalogs, the verify.toml citation is produced at
  verify time, so authoring it now would collide with dross-verify.
- **All of t-2..t-5 sit in wave 2, not a chain.** Each needs only terraform.toml
  (t-1); none consumes another's output, so they run in parallel rather than an
  artificial 4-deep ladder.
