Phase deepen-container-iac-scanning — 11 tasks across 3 waves

Lens: RISK. The graph starts from what can break — content-sniff false positives,
unbounded file reads, the dockle silent-all-clear, dedup collapsing checkov into
trivy, and the existing "out-of-scope" guards that this deepening must flip. Each
failure mode is owned and tested by exactly one task.

Wave 1
  t-1  Add content-matcher capability to marker engine
       files:    internal/stack/profile.go, internal/stack/detect.go,
                 internal/stack/profile_test.go, internal/stack/detect_test.go
       covers:   c-1, c-2
       desc:     Add Signals.Content{All,Any []string}. MarkerProfiles, after a
                 glob candidate matches, reads the file at a CAPPED size (~64 KiB)
                 and confirms markers: All = every substring present (AND), Any =
                 at least one (OR), CASE-SENSITIVE. A profile with no [signals.content]
                 keeps the pure-glob fast path (docker/terraform behaviour unchanged).
                 Engine stays free of profile-id literals (TestNoDockerHardcode).
       contract: if the matcher uses OR where k8s needs AND, a YAML with `kind:` but
                 no `apiVersion:` falsely matches and TestContentMatchAllSemantics fails;
                 if matching is case-insensitive, a file with lowercase `resources:`
                 falsely matches the cfn `Resources` marker and TestContentMatchCaseSensitive fails;
                 if a glob-only profile starts reading bodies, the pure-glob fast-path
                 assertion (docker/terraform unaffected) fails;
                 if the read cap is removed, TestContentMatchReadCap (marker planted
                 past the cap, expected NOT to match) fails;
                 if an unreadable/binary candidate panics instead of being skipped,
                 TestContentMatchUnreadableSkipped fails.

  t-2  Add dockle scan-path decision (no-image skip / supplied-image run)
       files:    internal/security/dockle.go, internal/security/dockle_test.go
       covers:   c-3, c-8
       desc:     Pure decision function. image=="" → Skipped with a non-empty Reason
                 ("dockle needs a built image; run docker build or supply --image"),
                 never an all-clear. dockle missing → Skipped with the install hint
                 (distinct from the no-image reason). image set → a run plan whose
                 command targets the supplied ref; never emits `docker build`.
       contract: if the no-image path returns not-skipped, TestDockleNoImageSkipsWithReason
                 fails (Reason must be non-empty);
                 if the supplied-image plan shells out to build, TestDockleNeverBuilds
                 fails (command must reference the image and never contain "build");
                 if a missing dockle is reported as a no-image skip rather than an
                 install-hint skip, TestDockleMissingBinHint fails.

  t-5  Add checkov to terraform + dockle to docker loadouts
       files:    internal/stack/profiles/terraform.toml, internal/stack/profiles/docker.toml
       covers:   c-3, c-4
       desc:     terraform.toml gains a "checkov" scanner (install hint names the
                 Python toolchain: "pipx install checkov"); docker.toml gains a
                 "dockle" scanner. Both header comments updated — checkov/dockle are
                 no longer "out of scope". This flips guards that t-7/t-10 must follow.
       contract: if checkov is not added to terraform.toml, t-7's new checkov-present
                 assertion in TestEmbeddedTerraform fails;
                 if dockle is not added to docker.toml, t-7's dockle-present assertion
                 in TestEmbeddedDocker fails;
                 if checkov's install hint omits the Python toolchain, t-8's c-4
                 python-hint assertion fails.

Wave 2 (depends t-1, t-2)
  t-3  Add kubernetes marker profile (content-sniff)
       files:    internal/stack/profiles/kubernetes.toml
       covers:   c-1, c-4, c-7
       depends:  t-1
       desc:     Marker stack, no exts. file_patterns = *.yaml/*.yml/*.json;
                 [signals.content].all = ["apiVersion","kind"]. scanners: "trivy config"
                 (bin trivy) + "checkov" (Python install hint). analyzer: "kube-linter"
                 (dimension error-handling). Header comment explains the marker +
                 content-sniff stack and tool choices.
       contract: if kubernetes.toml drops the apiVersion/kind content markers, the k8s
                 deployment.yaml fixture stops surfacing in t-8's TestBuildManifestMarkerKubernetes;
                 if kube-linter's dimension is not error-handling, t-7/t-9 dimension rows fail;
                 if checkov is renamed/dropped here, the checkov recon row in t-8 empties.

  t-4  Add cloudformation marker profile (content-sniff)
       files:    internal/stack/profiles/cloudformation.toml
       covers:   c-2, c-4, c-7
       depends:  t-1
       desc:     Marker stack, no exts. file_patterns = *.yaml/*.yml/*.json;
                 [signals.content].any = ["AWSTemplateFormatVersion","Resources"].
                 scanners: "trivy config" + "checkov". analyzer: "cfn-lint"
                 (dimension error-handling, Python install hint). Header comment notes
                 the Resources false-positive risk the case-sensitive any-match guards.
       contract: if cloudformation.toml drops AWSTemplateFormatVersion from its markers,
                 the template.yaml fixture stops surfacing in t-8's TestBuildManifestMarkerCloudformation;
                 if cfn-lint's dimension is not error-handling, t-7/t-9 dimension rows fail;
                 if the any-match collapses to require both markers, a template with only
                 AWSTemplateFormatVersion stops matching and that row fails.

  t-6  Wire --image flag/config into `dross security run`
       files:    internal/cmd/security.go, internal/cmd/security_test.go
       covers:   c-3, c-8
       depends:  t-2
       desc:     Add an --image flag (and project-config fallback) feeding t-2's
                 decision; the run report/output shows dockle running against the ref
                 when supplied, and skipped-with-reason when not. An empty flag is
                 treated as no-image (not a run against "").
       contract: if --image is ignored, TestSecurityRunImageFlagRunsDockle fails
                 (output must show dockle targeting the ref);
                 if no --image still reports dockle as ran/all-clear,
                 TestSecurityRunNoImageSkipsDockle fails (must read skipped-with-reason);
                 if an empty --image is treated as a real ref, the empty-flag guard fails.

Wave 3 (depends wave-2 profiles)
  t-7  Pin embedded loadouts for new + changed profiles
       files:    internal/stack/embed_test.go
       covers:   c-1, c-2, c-4
       depends:  t-3, t-4, t-5
       desc:     Add TestEmbeddedKubernetes/TestEmbeddedCloudformation (content markers
                 decode non-empty, trivy config bin==trivy, checkov present, analyzer
                 dimension error-handling, no exts). UPDATE TestEmbeddedTerraform (now
                 ships checkov) and TestEmbeddedDocker (now ships dockle) — the prior
                 "out of scope" assertions are inverted.
       contract: if the content schema field is renamed so kubernetes.toml's
                 [signals.content] no longer decodes, TestEmbeddedKubernetes sees empty
                 markers and fails;
                 if terraform/docker regress to dropping checkov/dockle, the updated
                 TestEmbeddedTerraform/TestEmbeddedDocker present-assertions fail.

  t-8  Assert security recon surfaces, dedups, and skips IaC scanners
       files:    internal/security/recon_test.go
       covers:   c-1, c-2, c-4
       depends:  t-3, t-4, t-5
       desc:     k8s/cfn content fixtures surface "trivy config" + "checkov"; a
                 multi-IaC repo keeps "trivy", "trivy config", and "checkov" each
                 exactly once AND still keeps govulncheck/gitleaks; a missing checkov
                 is in Skipped() with a non-empty Python install hint.
       contract: if dedup collapses checkov into trivy config, the two-distinct-Name
                 assertion in TestBuildManifestCheckovKeptAlongsideTrivy fails;
                 if a missing checkov is silently omitted instead of Skipped-with-hint,
                 the skip assertion fails;
                 if unioning IaC markers drops govulncheck/gitleaks, the agnostic-still-present row fails.

  t-9  Assert quality recon surfaces kube-linter/cfn-lint analyzers
       files:    internal/quality/recon_test.go
       covers:   c-7
       depends:  t-3, t-4
       desc:     k8s fixture surfaces "kube-linter" (error-handling) and still carries
                 scc/jscpd (union, not replace); cfn fixture surfaces "cfn-lint"
                 (error-handling); missing analyzers are Skipped-with-hint; a
                 marker-less Go repo surfaces neither (no leak).
       contract: if kube-linter is dropped, TestQualityManifestMarkerKubernetes's
                 kube-linter row empties;
                 if marker analyzers replace rather than union the agnostic set, the
                 scc/jscpd rows fail;
                 if cfn-lint leaks into a marker-less Go repo, the no-leak assertion fails.

  t-10 Document new profiles + flip out-of-scope notes + doc guard
       files:    internal/stack/profiles/README.md, internal/stack/profiles_doc_test.go
       covers:   c-6
       depends:  t-3, t-4, t-5
       desc:     Add README rows for kubernetes/cloudformation; UPDATE the terraform/
                 docker notes (checkov/dockle now in scope) and the existing doc_test
                 that asserts they are out of scope. New guard: kubernetes.toml/
                 cloudformation.toml headers contain "marker" and explain content-sniff.
       contract: if README loses the kubernetes/cloudformation rows,
                 TestNewIaCProfilesDocumented fails;
                 if the new profile headers omit "marker"/content-sniff, the header
                 assertion fails;
                 if the terraform/docker doc_test still asserts checkov/dockle out of
                 scope, the updated guard fails to match the new comments.

  t-11 Commit multi-IaC fixture + manual-run record
       files:    fixtures/iac-multi-c5/deployment.yaml, fixtures/iac-multi-c5/template.yaml,
                 fixtures/iac-multi-c5/Dockerfile, fixtures/iac-multi-c5/RUN.md,
                 fixtures/iac-multi-c5/expected-finding.txt
       covers:   c-5
       depends:  t-3, t-4, t-5
       desc:     Mirror fixtures/terraform-c3. A k8s manifest, a CFN template, and a
                 Dockerfile each plant a deterministic misconfig the agnostic fallback
                 is blind to (pinned to rule id @ file:line). RUN.md records the post
                 `make install` run: detect lists k8s/cfn scanners, dockle, checkov,
                 kube-linter, cfn-lint each installed-vs-missing-with-hint, then each
                 surfaces its finding while trivy fs/gitleaks/scc/jscpd stay blind.
                 A locked manual-run record, NOT a go-test shelling out to the tools.
       contract: if the k8s manifest's misconfig is fixed, expected-finding.txt no
                 longer matches the trivy-config/kube-linter finding RUN.md reproduces;
                 if the CFN template's misconfig is removed, the cfn-lint/checkov finding
                 in expected-finding.txt no longer reproduces.

## Coverage
- c-1 (kubernetes profile detected + scanner surfaced): t-1, t-3, t-7, t-8
- c-2 (cloudformation profile detected + scanner surfaced): t-1, t-4, t-7, t-8
- c-3 (docker surfaces dockle; no-image skip-with-reason): t-2, t-5, t-6, t-11
- c-4 (checkov cross-family, installed-vs-missing, Python hint): t-3, t-4, t-5, t-7, t-8
- c-5 (multi-IaC fixture + manual-run record, no silent all-clear): t-11
- c-6 (new profiles documented + headers + README discoverable): t-10
- c-7 (kube-linter/cfn-lint quality analyzers, error-handling): t-3, t-4, t-9
- c-8 (supplied image runs dockle; no image skips; never builds): t-2, t-6

## Judgment calls
- Content matching is CASE-SENSITIVE and bounded to a ~64 KiB read; chose this over
  case-insensitive/whole-file to neutralise the cfn "Resources" false-positive (vs
  lowercase app `resources:`) and to cap cost on huge YAML/JSON — the largest risk
  in this phase. Owned solely by t-1.
- Split the dockle behaviour into a pure decision function (t-2) and the CLI flag
  wiring (t-6) rather than one cmd-layer task; the silent-all-clear and never-build
  failure modes are then unit-testable without a cobra harness, and the flag-precedence/
  empty-flag risk is isolated in t-6.
- Kept checkov as a scanner entry replicated across the three IaC profiles
  (terraform/k8s/cfn) and let recon's name-dedup surface it once, rather than adding a
  cross-family code path. Honours the data-driven seam (no profile-id literals in
  mechanism code) and the locked "distinct Name, kept alongside trivy config" rule.
- Made t-5 (edit terraform/docker loadouts) a wave-1 task with no dependency, because
  it needs neither the content engine nor the dockle logic — only its dependent test
  updates (t-7, t-10) wait on it. Chose this over folding the edits into t-3/t-4 so the
  "flip an existing out-of-scope guard" regression risk has a single owner.
- Separate kubernetes (t-3) and cloudformation (t-4) profile tasks rather than one
  "new IaC profiles" task: their content-sniff semantics differ (AND vs OR / the
  Resources FP), so each family's detection risk is owned and tested independently.
