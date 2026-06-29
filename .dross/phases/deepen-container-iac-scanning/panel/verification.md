# Plan — deepen-container-iac-scanning (VERIFICATION lens)

Built backward from each criterion's ideal test contract. Every task names the
exact test that fails if its surface regresses; no task ships without one.

Phase deepen-container-iac-scanning — 9 tasks across 3 waves

Wave 1
  t-1  Add content-sniff matcher to marker engine
       files:    internal/stack/profile.go, internal/stack/detect.go, internal/stack/detect_test.go
       covers:   c-1, c-2
       contract: TestMarkerContentSniff (new, mirrors TestMarkerProfiles, in-memory profiles):
                 (a) a profile with Content.AllOf=["apiVersion","kind"] surfaces for a *.yaml
                 containing BOTH but NOT for a *.yaml with only apiVersion — if MatchesContent
                 treats AllOf as any-of, the partial-content row fails; (b) a plain *.yaml with
                 neither token surfaces nothing — if MarkerProfiles surfaces a content-gated
                 profile on filename alone (ignoring Content), this no-false-positive row fails
                 (the compose/svelte collision the locked decision guards against); (c) a
                 pattern-only profile (no Content) still surfaces on name alone — if the walk
                 forces a content read on every profile, the docker/terraform regression row fails.

  t-2  Add image-gated scanner status to security engine
       files:    internal/stack/profile.go, internal/security/catalog.go, internal/security/recon.go, internal/security/recon_test.go
       covers:   c-3, c-8
       contract: TestDockleNoImageSkipsWithReason / TestDockleSuppliedImageRunnable (new,
                 in-memory scanner with RequiresImage=true): with no image, the dockle ToolStatus
                 carries a non-empty SkipReason naming "image" and is returned by Skipped() (NOT
                 Ran()) even though Installed==true — if Ran()/Skipped() ignore SkipReason, an
                 installed-but-imageless dockle reads as a silent all-clear and the skip row
                 fails; with an image ref threaded through BuildManifestWithImage, the same dockle
                 has an empty SkipReason and appears in Ran() — if RequiresImage isn't carried
                 Tool→Scanner or the image isn't threaded, the runnable row fails. BuildManifest
                 (no image) delegates to BuildManifestWithImage("") so existing callers compile.

Wave 2 (depends t-1, t-2)
  t-3  Author kubernetes/cloudformation profiles + terraform checkov
       files:    internal/stack/profiles/kubernetes.toml, internal/stack/profiles/cloudformation.toml, internal/stack/profiles/terraform.toml, internal/stack/embed_test.go, internal/stack/detect_test.go
       covers:   c-1, c-2, c-4, c-6, c-7
       depends:  t-1
       contract: TestEmbeddedKubernetes / TestEmbeddedCloudFormation (new, mirror
                 TestEmbeddedTerraform): each profile is Exts-empty, declares the locked Content
                 matcher (k8s = AllOf apiVersion+kind; cfn = AnyOf AWSTemplateFormatVersion +
                 Resources) pinned by reflect.DeepEqual, ships scanner "trivy config"(bin trivy)
                 + scanner "checkov" (distinct Name), and analyzer kube-linter / cfn-lint with
                 dimension "error-handling" — drop/rename any and its row fails. TestEmbeddedTerraform
                 is FLIPPED: terraform must now SHIP checkov as a scanner (the old "must not ship
                 checkov" assertion is inverted) with a Python install hint. TestMarkerContentSniffEmbedded
                 (in detect_test.go, through Embedded()+real content read): an apiVersion+kind
                 manifest surfaces "kubernetes" and an AWSTemplateFormatVersion template surfaces
                 "cloudformation" — proving the shipped Content signals resolve end-to-end.
       note:     kubernetes.toml/cloudformation.toml carry a terraform.toml-style header comment
                 (marker stack, content-sniff rationale, tool choices) — asserted by t-8's guard.

  t-4  Declare dockle in docker profile (requires_image)
       files:    internal/stack/profiles/docker.toml, internal/stack/embed_test.go
       covers:   c-3, c-8
       depends:  t-2
       contract: TestEmbeddedDocker is FLIPPED: docker must now SHIP a "dockle" scanner with
                 requires_image=true (the old "must not ship dockle" assertion is inverted) — if
                 dockle is absent or requires_image is dropped, the assertion fails. Header comment
                 updated so dockle is no longer described as out-of-scope.

Wave 3 (depends t-3, t-4)
  t-5  Assert k8s/cfn/checkov surface & skip (security)
       files:    internal/security/recon_test.go
       covers:   c-1, c-2, c-4
       depends:  t-3
       contract: TestBuildManifestMarkerKubernetes / CloudFormation (mirror MarkerTerraform): an
                 apiVersion+kind repo / an AWSTemplateFormatVersion repo yields a manifest
                 containing "trivy config" AND "checkov". TestBuildManifestCheckovKeptBesideTrivyConfig:
                 on a terraform+k8s repo both "trivy config" and "checkov" appear, each exactly
                 once, alongside agnostic "trivy"/"gitleaks" — if checkov is deduped away in
                 favour of trivy config, the count fails. TestBuildManifestCheckovSkipHint: under
                 an all-missing lookPath, "checkov" is in Skipped() with an Install hint that
                 contains "pip"/"pipx" (its Python dependency) and BuildManifest returns no error.

  t-6  Assert kube-linter/cfn-lint surface & skip (quality)
       files:    internal/quality/recon_test.go
       covers:   c-1, c-2, c-7
       depends:  t-3
       contract: TestQualityManifestMarkerKubernetes / CloudFormation (mirror MarkerTerraform): an
                 apiVersion+kind repo surfaces "kube-linter" (dimension error-handling) AND still
                 carries agnostic "scc"/"jscpd" (union, not replace); a cfn repo surfaces
                 "cfn-lint". TestQualityIaCMissingAnalyzerSkipped: missing kube-linter/cfn-lint
                 land in Skipped() with a non-empty Install hint. TestQualityIaCNoLeak: a
                 marker-less Go-only repo's manifest contains neither kube-linter nor cfn-lint —
                 if a marker analyzer leaks into a plain repo, this fails.

  t-7  Thread --image flag, render dockle skip-with-reason
       files:    internal/cmd/security.go, internal/cmd/security_test.go, internal/security/recon_test.go
       covers:   c-3, c-8
       depends:  t-2, t-4
       contract: TestBuildManifestDockleDockerRepo (recon_test): a Dockerfile repo with dockle
                 installed but no image yields dockle in Skipped() with its no-image reason — never
                 silently absent or counted clean. TestSecurityDetectImageFlag (security_test):
                 `dross security detect --image alpine:3` renders dockle as scanning the supplied
                 image, while `dross security detect` (no flag) renders dockle as "skipped — needs
                 an image" with the build hint; report.md mirrors the two states. If the flag isn't
                 threaded into BuildManifestWithImage, the with-image row still reads skipped and
                 fails. dross shells no `docker build` — the image is an opaque ref (the existing
                 TestNoDockerHardcode already forbids a "docker" literal in recon.go).

  t-8  Document new profiles + doc-presence guard
       files:    internal/stack/profiles/README.md, internal/stack/profiles_doc_test.go
       covers:   c-6
       depends:  t-3, t-4
       contract: TestKubernetesProfileDocumented / TestCloudFormationProfileDocumented (mirror
                 TestTerraformProfileDocumented, os.ReadFile): README.md mentions "kubernetes" and
                 "cloudformation"; each new <id>.toml's header comment contains "marker" and the
                 word "content" (the content-sniff rationale). Deleting a README row or stripping a
                 header comment fails the matching row.

  t-9  Commit multi-IaC fixture + manual-run record
       files:    fixtures/iac-multi-c5/k8s-deployment.yaml, fixtures/iac-multi-c5/template.yaml, fixtures/iac-multi-c5/RUN.md
       covers:   c-5
       depends:  t-3, t-4
       contract: RUN.md is a locked manual-run record (mirroring fixtures/terraform-c3/RUN.md, NOT
                 a go-test): after `make install`, `dross security detect` + `dross quality detect`
                 over the fixture list every new tool — kubernetes/cfn trivy config, checkov,
                 dockle, kube-linter, cfn-lint — each as installed-vs-missing with an install hint,
                 and a missing one reads "skipped, install X" not a silent all-clear. The k8s and
                 cfn fixtures carry the exact content tokens (apiVersion+kind / AWSTemplateFormatVersion)
                 that t-3's content-sniff matches — if a token is removed, the fixture stops
                 surfacing its profile and RUN.md stops reproducing.

## Coverage

- c-1 (k8s detected + scanner surfaced, security & quality): t-1, t-3, t-5, t-6
- c-2 (cloudformation detected + scanner surfaced, security & quality): t-1, t-3, t-5, t-6
- c-3 (dockle surfaced; no-image skips-with-reason, never silent all-clear): t-2, t-4, t-7
- c-4 (checkov cross-family, installed-vs-missing, Python install hint): t-3, t-5
- c-5 (committed multi-IaC fixture lists every new tool; manual-run record): t-9
- c-6 (new profiles self-documented + README entries): t-3 (headers), t-8 (README + guard)
- c-7 (kube-linter/cfn-lint per family, error-handling dimension): t-3 (declared), t-6 (surfaced)
- c-8 (supplied-image dockle run; no-image skip-with-hint; never builds): t-2, t-4, t-7

All of c-1..c-8 accounted for.

## Judgment calls

- Generic `Content.AllOf/AnyOf` on Signals (chosen) over a k8s/cfn-specific code branch in
  detect.go (rejected): the locked content-sniff decision plus the existing TestNoDockerHardcode
  no-hardcode ethos require the matcher to be data, so a drop-in IaC profile works with zero code
  change — and the matcher stays unit-testable with in-memory profiles.
- Generic `Tool.RequiresImage` + `ToolStatus.SkipReason` (chosen) over hardcoding dockle's
  no-image behaviour in recon.go (rejected): keeps the "never silent all-clear" gate a data-driven,
  testable third state (installed-but-skipped) instead of a special case, and respects the
  no-"docker"-literal source guard.
- New `BuildManifestWithImage(root, lookPath, image)` with `BuildManifest` delegating image="" 
  (chosen) over changing BuildManifest's signature (rejected): the latter would ripple through every
  existing security/quality caller and test; the wrapper keeps them compiling while threading the flag.
- Split engine (t-1/t-2) from profile data (t-3/t-4) from recon assertions (t-5/t-6/t-7) (chosen)
  over one profile-per-task vertical slice (rejected): each contract targets a distinct surface
  (matcher / TOML loadout / manifest), so a red test points at one layer — and the two flipped
  existing tests (TestEmbeddedTerraform, TestEmbeddedDocker) live with the data change that breaks them.
- dockle's "run against supplied image" lands at the manifest+flag+report.md layer (chosen) over
  driving an actual dockle exec in Go (rejected): the Go layer only builds the coverage manifest —
  scanners are run by secure.md — so the testable, never-builds guarantee is the threaded image ref
  and the rendered skipped-vs-runnable state, not a daemon call.
