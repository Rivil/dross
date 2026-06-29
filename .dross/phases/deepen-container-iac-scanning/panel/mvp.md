Phase deepen-container-iac-scanning — 7 tasks across 3 waves

Wave 1
  t-1  Extend marker engine: content-sniff + image schema
       files:    internal/stack/profile.go, internal/stack/detect.go, internal/stack/detect_test.go
       covers:   c-1, c-2, c-8
       contract: if MarkerProfiles surfaces a content-sniff profile on a *.yaml file
                 that lacks its content markers, the new no-false-positive unit test
                 fails; if the kubernetes matcher does not require both apiVersion AND
                 kind, a plain values.yaml / docker-compose.yaml surfaces "kubernetes"
                 and the collision-guard row fails; if Tool.RequiresImage is dropped
                 from the schema, docker.toml's `requires_image` stops decoding and
                 t-3's disposition test fails.

Wave 2 (depends t-1)
  t-2  Author k8s + cfn profiles; add cross-family checkov
       files:    internal/stack/profiles/kubernetes.toml, internal/stack/profiles/cloudformation.toml,
                 internal/stack/profiles/terraform.toml, internal/stack/profiles/README.md
       covers:   c-1, c-2, c-4, c-6, c-7
       contract: if kubernetes.toml/cloudformation.toml omits its "trivy config"
                 scanner, t-5's per-family trivy-config manifest row empties; if checkov
                 is not declared on all three IaC profiles (tf/k8s/cfn), t-5's
                 checkov-cross-family rows fail; if kube-linter/cfn-lint is missing or
                 not tagged dimension="error-handling", t-4/t-6 dimension rows fail; if
                 the README k8s/cfn rows or a profile header comment are deleted, t-4's
                 doc-guard fails.

  t-3  Surface dockle with skip-with-hint + supplied-image
       files:    internal/stack/profiles/docker.toml, internal/security/catalog.go,
                 internal/security/recon.go, internal/cmd/security.go
       covers:   c-3, c-8
       contract: if dockle is omitted from docker.toml, t-5's dockle manifest row
                 empties; if a no-image dockle renders [installed]/ran instead of
                 skipped-with-reason, the no-image disposition test fails; if a supplied
                 --image (or the DROSS_IMAGE config fallback) is ignored, the
                 supplied-image dockle-will-scan test fails; if the skip-reason text
                 introduces the literal "docker" into recon.go, the existing
                 TestNoDockerHardcode goes red.

Wave 3 (depends t-2, t-3)
  t-4  Pin embedded loadouts + doc guard
       files:    internal/stack/embed_test.go, internal/stack/profiles_doc_test.go
       covers:   c-1, c-2, c-4, c-6, c-7
       depends:  t-2, t-3
       contract: if the kubernetes/cloudformation embedded profile loses its content
                 matcher, "trivy config", checkov, or its kube-linter/cfn-lint analyzer,
                 TestEmbeddedKubernetes/TestEmbeddedCloudFormation fails; if the
                 must-NOT-ship-checkov assertion in TestEmbeddedTerraform and the
                 must-NOT-ship-dockle assertion in TestEmbeddedDocker are not flipped
                 once those tools are added, those tests stay red; if a k8s/cfn header
                 comment or README row is stripped, the profiles_doc guard fails.

  t-5  Assert security recon surfaces k8s/cfn/checkov/dockle + skips
       files:    internal/security/recon_test.go
       covers:   c-1, c-2, c-3, c-4, c-5, c-8
       depends:  t-2, t-3
       contract: if the k8s/cfn marker scanner is dropped, TestBuildManifestMarker-
                 Kubernetes/CloudFormation's trivy-config row empties; if checkov is
                 omitted or its install hint omits the Python/pipx dependency, the
                 checkov installed-vs-missing-with-hint row fails; if a missing
                 dockle/checkov is silently dropped instead of landing in Skipped() with
                 a hint, the skip assertion fails; if dockle reads as ran/all-clear with
                 no image — or ignores a supplied image — the dockle disposition rows fail.

  t-6  Assert quality recon surfaces kube-linter + cfn-lint + skips
       files:    internal/quality/recon_test.go
       covers:   c-5, c-7
       depends:  t-2
       contract: if kube-linter (k8s) or cfn-lint (cfn) is dropped, that family's
                 quality manifest row empties; if marker analyzers replace rather than
                 union scc/jscpd, the agnostic-survival rows fail; if a missing
                 kube-linter/cfn-lint is silently omitted instead of Skipped-with-hint,
                 the skip assertion fails; if either analyzer leaks into a marker-less
                 Go-only repo, the no-leak assertion fails.

  t-7  Commit multi-IaC fixture + manual-run record
       files:    fixtures/multi-iac-c5/k8s-deployment.yaml, fixtures/multi-iac-c5/template.cfn.yaml,
                 fixtures/multi-iac-c5/RUN.md, fixtures/multi-iac-c5/expected-finding.txt
       covers:   c-5
       depends:  t-2, t-3
       contract: if the k8s manifest's planted misconfig is fixed, RUN.md stops
                 reproducing and expected-finding.txt no longer matches the
                 trivy-config/checkov/kube-linter finding; if the CFN template's planted
                 misconfig is fixed, the cfn finding in expected-finding.txt no longer
                 matches; RUN.md must record `dross security detect`/`dross quality
                 detect` listing every new tool (k8s+cfn trivy config, checkov, dockle,
                 kube-linter, cfn-lint) as installed-vs-missing-with-hint.

## Coverage
- c-1 (kubernetes profile detected + scanner surfaced): t-1 (content-sniff engine), t-2 (data), t-4 (embed pin), t-5 (security recon assert)
- c-2 (cloudformation profile detected + scanner surfaced): t-1, t-2, t-4, t-5
- c-3 (dockle surfaced + no-image skip-with-reason): t-3 (impl), t-5 (assert)
- c-4 (checkov cross-family, installed-vs-missing, Python hint): t-2 (data on tf/k8s/cfn), t-5 (assert)
- c-5 (multi-IaC fixture + manual-run record lists every new tool): t-7 (artifact), t-5 + t-6 (detect-lists assertions)
- c-6 (profiles documented: header comments + README): t-2 (comments + README rows), t-4 (doc guard)
- c-7 (kube-linter/cfn-lint quality analyzers under error-handling): t-2 (data), t-4 (loadout dimension pin), t-6 (quality recon assert)
- c-8 (supplied-image runs dockle; no image skips; never builds): t-1 (requires_image schema), t-3 (flag/config + disposition), t-5 (assert)

## Judgment calls
- checkov folded into t-2 (added to all three IaC profiles at once) rather than a per-family task: it is one decision (checkov_role) and the same TOML edit, so three separate tasks would be speculative structure. Rejected splitting per family.
- Profile docs (README + header comments, c-6) merged into the profile-authoring task t-2 rather than a standalone doc task: "ship a discoverable profile" is one cohesive deliverable. Only the doc-GUARD test is separated (into t-4, beside the embed tests, same package). Rejected the predecessor's standalone doc task.
- dockle's whole story (surface + skip-with-hint + supplied-image) kept as ONE task t-3 despite spanning docker.toml + catalog.go + recon.go + cmd: a half-delivery (dockle that surfaces but never skips-with-reason) satisfies no criterion independently. Rejected splitting surface from disposition.
- No catalog.go/recon.go change for k8s/cfn/checkov: they ride the existing data-driven profileScanners/profileAnalyzers + MarkerProfiles union, so the only new mechanism is content-sniff (t-1) and the dockle image-disposition (t-3). Rejected adding a kubernetes/cloudformation special-case to recon.
- "config" channel for the image ref implemented as a DROSS_IMAGE env-var fallback to the --image flag, kept inside the cmd layer. Rejected adding a new project.toml [security] field — that is project-package plumbing no criterion requires; flag + env satisfies "flag/config" (c-8) with the smallest surface.
- dockle skip-reason text is generic ("requires a built image; supply one with --image — dross never builds it") and lives in recon.go, while the per-family install hint stays in docker.toml. Rejected the literal locked hint wording inside recon.go because it contains "docker" and would trip the existing TestNoDockerHardcode grep.
- Content-sniff mechanism unit test co-located in t-1 (detect_test.go, in-memory profiles); embedded-loadout pinning isolated to t-4 (embed_test.go). This keeps detect_test.go owned by one task and avoids a cross-wave edit collision on the same file.
