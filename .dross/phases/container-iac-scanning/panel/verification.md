# Verification-lens plan — container-iac-scanning

Designed backward from the test contract for each criterion: the ideal test name
is written first, then the smallest task that makes it satisfiable. Every contract
below names the surface that breaks and is concrete enough to translate directly
into a Go test (or, for c-3's manual half, the pinned fixture assertion). The
mechanism (MarkerProfiles / BuildManifest / profileScanners / profileAnalyzers) is
already generic per the locked drop-in decision, so the data file is the keystone
and every assertion hangs off it.

Phase container-iac-scanning — 6 tasks across 2 waves

Wave 1
  t-1  Add terraform.toml marker profile
       files:    internal/stack/profiles/terraform.toml
       covers:   c-1, c-2, c-4
       contract: Mirror docker.toml. Declare id="terraform", title="Terraform";
                 [signals].file_patterns = the LOCKED set (*.tf, *.tf.json, *.tfvars,
                 *.tfvars.json, *.hcl) and NO exts. Loadout: a scanner named "trivy config"
                 (bin="trivy", core) DISTINCT from the agnostic "trivy"; an analyzer "tflint"
                 (dimension="error-handling", core). Header comment marks it a marker-file
                 stack and states checkov/terrascan are out of scope. This file is pure data —
                 its contracts are the assertions in t-2/t-3/t-4/t-5; it carries no logic.
                 contract: TestEmbeddedTerraform (t-2) fails if the scanner name collides with
                 "trivy" or its bin != "trivy", if Exts is non-empty / FilePatterns is empty,
                 or if tflint's dimension != "error-handling".

Wave 2 (every task strictly needs t-1's terraform.toml)
  t-2  Pin terraform patterns + embedded loadout
       files:    internal/stack/profile_test.go
                 internal/stack/embed_test.go
       covers:   c-1, c-2
       depends_on: t-1
       contract: Two tests.
                 (a) TestTerraformFilePatternMatch (mirror TestFilePatternMatch, with a
                 terraformFilePatterns var pinning the locked set): "main.tf", "vars.tf.json",
                 "prod.tfvars", "x.tfvars.json", "packer.hcl" MUST match; "main.go",
                 "README.md", "notfile.tfx" MUST NOT. Dropping ".hcl" from the pattern set
                 fails the packer.hcl row; an over-broad substring glob fails the README.md row.
                 (b) TestEmbeddedTerraform (mirror TestEmbeddedDocker): ByID(Embedded(),
                 "terraform") is non-nil; FilePatterns non-empty AND Exts empty (marker stack);
                 scanners["trivy config"].EffectiveBin("")=="trivy"; analyzers["tflint"].Dimension
                 =="error-handling"; and neither "checkov" nor "terrascan" appears. Renaming the
                 profile id, collapsing "trivy config" into "trivy", or retagging tflint's
                 dimension each fails a distinct row.

  t-3  Assert terraform scanner surfaces & skips (security)
       files:    internal/security/recon_test.go
       covers:   c-1, c-3
       depends_on: t-1
       contract: Three tests against BuildManifest with the injected lookPath.
                 (a) TestBuildManifestMarkerTerraform (mirror TestBuildManifestMarkerDocker):
                 a *.tf-only repo (main.tf) yields a manifest containing "trivy config".
                 Removing the scanner from terraform.toml empties that row.
                 (b) TestBuildManifestTerraformDedup (mirror TestBuildManifestMarkerDedup):
                 a repo with go.mod + main.go + main.tf keeps BOTH agnostic "trivy" and
                 "trivy config", each exactly once — the marker-union dedup must not collapse
                 "trivy config" into "trivy".
                 (c) TestBuildManifest_terraform_missingScannerSkipped (c-3 detect-output half):
                 under an all-missing lookup, "trivy config" appears in m.Skipped() with a
                 non-empty Install hint and BuildManifest returns nil — a missing trivy reads as
                 "skipped, install X", never a silent all-clear. Returning an error from a
                 missing tool, or omitting the skipped entry, fails this.

  t-4  Assert tflint analyzer surfaces & skips (quality)
       files:    internal/quality/recon_test.go
       covers:   c-2, c-3
       depends_on: t-1
       contract: Two tests against BuildManifest.
                 (a) TestQualityManifestMarkerTerraform (mirror TestQualityManifestMarkerDocker):
                 a *.tf-only repo surfaces "tflint" in the manifest AND still carries the
                 agnostic "scc"/"jscpd" — so a *.tf repo gets an IaC-specific analyzer, not just
                 the agnostic fallback. Dropping tflint from terraform.toml fails the tflint row;
                 a regression that replaces rather than unions the marker analyzers fails the
                 scc/jscpd rows.
                 (b) TestBuildManifest_terraform_missingAnalyzerSkipped (c-3 detect-output half):
                 under an all-missing lookup, "tflint" is in m.Skipped() with a non-empty Install
                 hint and BuildManifest returns nil (no abort).

  t-5  Document terraform profile + doc-presence guard
       files:    internal/stack/profiles/README.md
                 internal/stack/profiles_doc_test.go
       covers:   c-4
       depends_on: t-1
       contract: Add a terraform row to README.md so the drop-in profile is discoverable, then
                 TestTerraformProfileDocumented (os.ReadFile, mirroring the TestNoDockerHardcode
                 read-the-source idiom): assert README.md contains "terraform"; assert
                 terraform.toml's leading comment block contains "marker" AND names the
                 out-of-scope tools ("checkov"/"terrascan"). Deleting the README row, or stripping
                 the header comment, fails its respective assertion.

  t-6  Commit IaC fixture + manual-run record
       files:    fixtures/terraform-c3/main.tf
                 fixtures/terraform-c3/RUN.md
                 fixtures/terraform-c3/expected-finding.txt
       covers:   c-3
       depends_on: t-1
       contract: Mirror fixtures/multilang-c3 exactly (the prior phase's findings_proof pattern).
                 main.tf plants ONE deterministic defect the agnostic fallback is blind to —
                 a declared-but-unused `variable "unused" {}` that tflint flags as
                 terraform_unused_declarations (the IaC analogue of knip's unused export).
                 expected-finding.txt PINS that exact warning line; RUN.md records the manual
                 run (`dross quality detect` / `dross security detect` showing tflint + trivy
                 config installed-vs-missing-with-hint, then `tflint` surfacing the finding while
                 `scc`/`jscpd` do not), with the rule r-01 `make install` pre-req noted.
                 contract: this is the LOCKED manual-run record, NOT a go-test gate — if the
                 planted `variable "unused"` is removed or renamed, the recorded tflint output no
                 longer matches expected-finding.txt and RUN.md stops reproducing. (The go-test
                 half of c-3 — skipped+hint detect output — is t-3(c)/t-4(b).)

## Coverage
- c-1 (terraform marker detected; trivy config scanner in manifest): t-1, t-2, t-3
- c-2 (tflint analyzer under error-handling; surfaces on *.tf repo): t-1, t-2, t-4
- c-3 (committed fixture + manual-run record; detect shows installed-vs-missing-with-hint): t-3, t-4, t-6
- c-4 (header comment + README entry, documented like docker.toml): t-1, t-5

## Judgment calls
- Split c-3 into two surfaces: t-3(c)/t-4(b) carry the Go-testable detect-output half (skipped + install hint, no abort), while t-6 is the locked manual-run fixture record. Rejected one combined "c-3 task" — the spec explicitly authorizes a Go test for the detect-output assertion but FORBIDS a go-test gate that shells out to tflint/trivy, so the two halves live in different layers.
- Chose tflint `terraform_unused_declarations` (unused variable) as the fixture defect over a trivy-config AWS misconfig. Rejected the trivy path because it needs a real cloud-resource block and a heavier ruleset; the unused-variable finding is plugin-free and deterministic, directly mirroring the prior phase's knip dead-code precedent.
- Made c-4 enforceable with a source-reading doc test (TestTerraformProfileDocumented) rather than leaving "documented like docker.toml" unverified. Rejected a no-test c-4 — every task needs a specific contract; a header comment / README row is exactly what an os.ReadFile assertion can pin.
- Put t-2..t-6 all in wave 2 (not staggered): each strictly needs terraform.toml but none needs another wave-2 task's output, so they parallelize. Rejected sequencing the tests after each other — they touch disjoint files (stack / security / quality / docs / fixtures) and share no dependency.
- Kept the loadout minimal: trivy config (scanner) + tflint (analyzer) only. Rejected adding tflint as a scanner too — c-1 names only `trivy config` for the scanner side and c-2 names tflint only as the analyzer; doubling tflint would invent coverage the criteria don't ask for.
