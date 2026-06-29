# Synthesis — deepen-container-iac-scanning

Cold judge over three independently-drafted plans (risk / mvp / verification). I authored
none of them. Source paths and guard tests were validated against the live tree before scoring.

## Scores

| Draft        | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|--------------|-------------------|---------------------------|-------------|------------------|
| risk (11/3)        | all c-1..c-8, plus extra failure-mode coverage (read-cap, FP, dedup) — strongest | names tests + the exact regression each catches per task — strong | very fine; arguably over-split (dockle decision vs flag; tf/docker edits standalone) but every file/risk has exactly one owner | clean: t-1/t-2/t-5 wave1, t-3/t-4/t-6 wave2, t-7..t-11 wave3; no same-file cross-wave collision |
| mvp (7/3)          | all c-1..c-8 — fine | failure modes described well but fewer exact test names — adequate | coarse; t-3 spans docker.toml+catalog.go+recon.go+cmd (4 files, mixed concerns) — half-delivery risk | sound, and uniquely collision-free: image schema folded into engine t-1 (single profile.go owner) |
| verification (9/3) | all c-1..c-8 — fine | highest: exact test fn names + an end-to-end embedded content test — strongest | balanced engine/data/assert split | flawed: profile.go edited by both t-1 and t-2 in wave1; detect_test.go by t-1+t-3; recon_test.go by t-2+t-5+t-7 — same-file cross-wave/parallel collisions |

Skeleton: **risk**. It has the fullest coverage, the only consistently single-owner-per-file
wave layout, and the sharpest treatment of the two guard-flips this phase turns on (embed_test).
Its weaknesses (over-granularity, a less idiomatic dockle mechanism) are repaired by grafts from
the other two below.

## Merged plan

11 tasks, 3 waves. Origin tags mark each task's source; grafts are noted inline.

### Wave 1

- **t-1** [risk; +mvp] — Add content-matcher capability to the marker engine
  - files: internal/stack/profile.go, internal/stack/detect.go, internal/stack/profile_test.go, internal/stack/detect_test.go
  - covers: c-1, c-2
  - contract: add `Signals.Content{All,Any []string}`; after a glob candidate matches, read the file at a CAPPED size (~64 KiB) and confirm CASE-SENSITIVE — All=AND, Any=OR. A profile with no `[signals.content]` keeps the pure-glob fast path (docker/terraform unchanged). Engine carries no profile-id literal (TestNoDockerHardcode stays green). Failure modes: OR-where-AND-needed → TestContentMatchAllSemantics; case-insensitive → TestContentMatchCaseSensitive (lowercase `resources:` must NOT match cfn `Resources`); glob-only profile reading bodies → fast-path regression row; cap removed → TestContentMatchReadCap (marker planted past cap, must NOT match); binary/unreadable candidate panics → TestContentMatchUnreadableSkipped. **Graft (verification):** add TestMarkerContentSniffEmbedded — through Embedded()+real read, an apiVersion+kind file resolves "kubernetes" and an AWSTemplateFormatVersion file resolves "cloudformation" end-to-end.
  - depends_on: —

- **t-2** [risk; +mvp] — Add dockle scan-path decision (no-image skip / supplied-image run)
  - files: internal/security/dockle.go, internal/security/dockle_test.go
  - covers: c-3, c-8
  - contract: pure decision fn. image=="" → Skipped with non-empty Reason ("dockle needs a built image; run docker build or supply --image"), never all-clear. dockle missing → Skipped with the install hint (distinct from the no-image reason). image set → a run plan targeting the supplied ref; never emits `docker build`. Failure modes: no-image not-skipped → TestDockleNoImageSkipsWithReason; supplied plan shells out to build → TestDockleNeverBuilds (must reference image, never contain "build"); missing-bin reported as no-image skip → TestDockleMissingBinHint. **Graft (mvp):** if any skip-reason string ever moves into recon.go, it must not contain the literal "docker" (TestNoDockerHardcode); keeping it in dockle.go (not in that test's path) sidesteps the trap.
  - depends_on: —

- **t-5** [risk] — Add checkov to terraform + dockle to docker loadouts
  - files: internal/stack/profiles/terraform.toml, internal/stack/profiles/docker.toml
  - covers: c-3, c-4
  - contract: terraform.toml gains a "checkov" scanner (install hint names the Python toolchain, e.g. "pipx install checkov"); docker.toml gains a "dockle" scanner. Both header comments updated — checkov/dockle no longer "out of scope". This is the single owner of the guard-flip risk that t-7 (and the doc comments t-10 touches) must follow. Failure modes: checkov absent from terraform.toml → t-7 TestEmbeddedTerraform checkov-present row; dockle absent from docker.toml → t-7 TestEmbeddedDocker dockle-present row; checkov hint omits Python → t-8 c-4 python-hint row.
  - depends_on: —

### Wave 2 (depends t-1, t-2)

- **t-3** [risk] — Add kubernetes marker profile (content-sniff)
  - files: internal/stack/profiles/kubernetes.toml
  - covers: c-1, c-4, c-7
  - contract: no exts; file_patterns *.yaml/*.yml/*.json; `[signals.content].all = ["apiVersion","kind"]`. scanners "trivy config" (bin trivy) + "checkov" (Python hint). analyzer "kube-linter" (dimension error-handling). Header comment explains the marker + content-sniff stack. Failure modes: drop apiVersion/kind → t-8 TestBuildManifestMarkerKubernetes fixture stops surfacing; kube-linter dimension ≠ error-handling → t-7/t-9 dimension rows; checkov dropped → t-8 checkov row empties.
  - depends_on: t-1

- **t-4** [risk] — Add cloudformation marker profile (content-sniff)
  - files: internal/stack/profiles/cloudformation.toml
  - covers: c-2, c-4, c-7
  - contract: no exts; file_patterns *.yaml/*.yml/*.json; `[signals.content].any = ["AWSTemplateFormatVersion","Resources"]`. scanners "trivy config" + "checkov". analyzer "cfn-lint" (dimension error-handling, Python hint). Header notes the case-sensitive any-match guards the `Resources` false-positive. Failure modes: drop AWSTemplateFormatVersion → t-8 TestBuildManifestMarkerCloudformation row; cfn-lint dimension wrong → t-7/t-9 rows; any-match collapsed to require both → a template with only AWSTemplateFormatVersion stops matching.
  - depends_on: t-1

- **t-6** [risk; +mvp] — Wire --image flag/config into `dross security run`
  - files: internal/cmd/security.go, internal/cmd/security_test.go
  - covers: c-3, c-8
  - contract: add an --image flag feeding t-2's decision; report shows dockle running against the ref when supplied, skipped-with-reason when not; empty flag treated as no-image. **Graft (mvp):** the "config" channel for c-8 is a `DROSS_IMAGE` env-var fallback to the flag (no new project.toml [security] field — smallest surface). Failure modes: --image ignored → TestSecurityRunImageFlagRunsDockle; no --image still reports ran/all-clear → TestSecurityRunNoImageSkipsDockle; empty --image treated as real ref → empty-flag guard.
  - depends_on: t-2

### Wave 3 (depends wave-2 profiles + t-5)

- **t-7** [risk; +verification] — Pin embedded loadouts for new + changed profiles
  - files: internal/stack/embed_test.go
  - covers: c-1, c-2, c-4
  - contract: add TestEmbeddedKubernetes/TestEmbeddedCloudFormation (content markers decode non-empty — **graft (verification):** pin the matcher with reflect.DeepEqual; trivy config bin==trivy; checkov present; analyzer dimension error-handling; no exts). FLIP TestEmbeddedTerraform (now ships checkov) and TestEmbeddedDocker (now ships dockle) — the existing NotContains assertions at embed_test.go:52-53 and :115-116 are inverted. Failure modes: content schema renamed so kubernetes.toml stops decoding → empty-markers fail; tf/docker regress to dropping the new tools → flipped present-assertions fail.
  - depends_on: t-3, t-4, t-5

- **t-8** [risk; +verification] — Assert security recon surfaces, dedups, and skips IaC scanners
  - files: internal/security/recon_test.go
  - covers: c-1, c-2, c-4
  - contract: k8s/cfn content fixtures surface "trivy config" + "checkov"; a multi-IaC repo keeps "trivy", "trivy config", "checkov" each exactly once AND still keeps govulncheck/gitleaks; a missing checkov is in Skipped() with a non-empty Python install hint. **Graft (verification):** name the dedup test TestBuildManifestCheckovKeptBesideTrivyConfig and the hint test TestBuildManifestCheckovSkipHint (hint contains "pip"/"pipx", BuildManifest returns no error). Failure modes: dedup collapses checkov into trivy config → two-distinct-Name row; missing checkov silently omitted → skip row; unioning IaC markers drops agnostic tools → agnostic-still-present row.
  - depends_on: t-3, t-4, t-5

- **t-9** [risk; +verification] — Assert quality recon surfaces kube-linter/cfn-lint analyzers
  - files: internal/quality/recon_test.go
  - covers: c-7
  - contract: k8s fixture surfaces "kube-linter" (error-handling) and still carries scc/jscpd (union, not replace); cfn fixture surfaces "cfn-lint" (error-handling); missing analyzers Skipped-with-hint; a marker-less Go repo surfaces neither. **Graft (verification):** name the leak guard TestQualityIaCNoLeak and the skip guard TestQualityIaCMissingAnalyzerSkipped. Failure modes: kube-linter dropped → its row empties; marker analyzers replace rather than union → scc/jscpd rows; cfn-lint leaks into Go-only repo → no-leak row.
  - depends_on: t-3, t-4

- **t-10** [risk] — Document new profiles + update out-of-scope notes + doc guard
  - files: internal/stack/profiles/README.md, internal/stack/profiles_doc_test.go
  - covers: c-6
  - contract: add README rows for kubernetes/cloudformation; update the terraform/docker header notes (checkov/dockle now in scope); new guard: kubernetes.toml/cloudformation.toml headers contain "marker" and explain content-sniff. Failure modes: README loses the new rows → TestNewIaCProfilesDocumented; new headers omit "marker"/content-sniff → header assertion. **Finding (correction):** the existing TestTerraformProfileDocumented only asserts the strings "checkov"/"dockle" are *present* in terraform.toml comments — it does NOT assert "out of scope" — so it will still PASS after t-5 adds the tools. The comment text should be updated for accuracy, but no terraform/docker doc-guard actually fails (correcting risk-t10 and verification-t8, which both describe flipping a doc-guard that does not break).
  - depends_on: t-3, t-4, t-5

- **t-11** [risk; +verification] — Commit multi-IaC fixture + manual-run record
  - files: fixtures/iac-multi-c5/deployment.yaml, fixtures/iac-multi-c5/template.yaml, fixtures/iac-multi-c5/Dockerfile, fixtures/iac-multi-c5/RUN.md, fixtures/iac-multi-c5/expected-finding.txt
  - covers: c-5
  - contract: mirror fixtures/terraform-c3. A k8s manifest, a CFN template, and a Dockerfile each plant a deterministic misconfig the agnostic fallback is blind to. RUN.md is a locked MANUAL-run record (not a go-test shelling out): after `make install`, `dross security detect` + `dross quality detect` list every new tool (k8s+cfn trivy config, checkov, dockle, kube-linter, cfn-lint) as installed-vs-missing-with-hint — a missing tool reads "skipped, install X", never a silent all-clear — then each surfaces its finding while trivy fs/gitleaks/scc/jscpd stay blind. **Graft (verification):** the k8s/cfn fixtures must carry the exact content tokens (apiVersion+kind / AWSTemplateFormatVersion) t-1/t-3/t-4 sniff — remove a token and the fixture stops surfacing its profile and RUN.md stops reproducing. Failure modes: misconfig fixed → expected-finding.txt no longer matches the corresponding tool's finding.
  - depends_on: t-3, t-4, t-5

### Coverage check
c-1: t-1, t-3, t-7, t-8 · c-2: t-1, t-4, t-7, t-8 · c-3: t-2, t-5, t-6, t-11 ·
c-4: t-3, t-4, t-5, t-7, t-8 · c-5: t-11 · c-6: t-10 · c-7: t-3, t-4, t-9 · c-8: t-2, t-6.
All eight criteria owned; all four locked decisions honoured (content-sniff hybrid in t-1/t-3/t-4;
trivy config reused per family + dedicated kube-linter/cfn-lint quality analyzers; dross never
builds the image; checkov kept side-by-side with trivy config, never deduped away).

## Disagreements

1. **Dockle mechanism — where the "skipped-with-reason" third state lives.**
   - risk: a pure decision fn in a new `internal/security/dockle.go` + cmd run-wiring (t-2/t-6); dockle surfaces in the *detect* manifest as an ordinary docker.toml scanner, and the no-image skip-with-reason is a *run-path* concern.
   - verification: data-driven `Tool.RequiresImage` + `ToolStatus.SkipReason` + a new `BuildManifestWithImage(root,lookPath,image)` wrapper (BuildManifest delegates image=""), so the *detect* manifest itself shows the third state.
   - mvp: fold `requires_image` into docker.toml + catalog.go + recon.go + cmd in one task (t-3), with a generic skip-reason string in recon.go.
   - Provisional default: **risk's**. It keeps single-owner-per-file (the verification/mvp variants edit profile.go in both t-1 and t-2, and recon_test.go across waves — real collisions), and dockle.go is outside TestNoDockerHardcode's scanned set so the skip-reason wording is unconstrained.
   - Why it matters: it decides whether `dross security detect` renders the skip-reason inline (verification/mvp) or only `dross security run` does (risk), and whether the manifest schema grows a RequiresImage/SkipReason surface. c-3 only demands the *scan path* skip-with-reason, which the run-path satisfies; if the team wants the third state visible in `detect` itself, switch to verification's wrapper and accept the profile.go co-edit.

2. **Profile-authoring granularity — one task or two.**
   - risk: separate kubernetes (t-3) and cloudformation (t-4) tasks, because their content semantics differ (AND on apiVersion+kind vs Any on AWSTemplateFormatVersion/Resources, with the Resources false-positive).
   - mvp + verification: a single combined profile-authoring task.
   - Provisional default: **split (risk)**. The cfn AnyOf "Resources" false-positive is a distinct, locked detection risk that earns its own owner and contract.
   - Why it matters: a combined task lets a cfn-matching regression and a k8s-matching regression land in the same commit, blurring which family broke; the split keeps a red contract pointing at one family.

3. **checkov placement across the three IaC profiles.**
   - risk: checkov declared inside each profile's own task (k8s in t-3, cfn in t-4, terraform in t-5).
   - mvp: one task adds checkov to all three profiles at once ("one decision, same TOML edit").
   - Provisional default: **per-profile (risk)**, to preserve single-owner-per-.toml.
   - Why it matters: mvp's framing is correct that checkov_role is one decision, but three files in one task widens the blast radius; per-file keeps each .toml edit independently revertible. (The locked checkov_role decision is satisfied either way — checkov as a distinct-Name scanner on all three families.)

4. **Documentation — standalone task or folded into authoring.**
   - risk + verification: a standalone doc + doc-guard task (t-10 / t-8).
   - mvp: README rows + header comments folded into the profile-authoring task, with only the doc-GUARD test separated.
   - Provisional default: **standalone (risk)**. c-6 is a discrete deliverable and README.md + the doc-guard test then have a single owner.
   - Why it matters: folding docs into authoring (mvp) is more cohesive but makes the profile task touch README.md too, and risks the doc rows being treated as an afterthought; a standalone task forces c-6 to be verified on its own.

5. **The guard-flip — which existing tests truly break, and where the flip lives.**
   - risk: isolates the terraform/docker loadout edits in t-5 (wave 1) as the single owner of the "flip an out-of-scope guard" risk.
   - verification: folds the terraform-checkov edit into t-3 and the docker-dockle edit into t-4.
   - mvp: folds them into t-2/t-3.
   - Provisional default: **risk's isolated t-5**, so the one risky regression (inverting an existing assertion) has exactly one owner.
   - Why it matters — and a hard finding: only `internal/stack/embed_test.go` genuinely breaks — `TestEmbeddedDocker` (lines 52-53) and `TestEmbeddedTerraform` (lines 115-116) error on dockle/checkov and MUST be flipped in t-7. By contrast `internal/stack/profiles_doc_test.go`'s `TestTerraformProfileDocumented` only checks the strings "checkov"/"dockle" are present in the comments; it will keep passing after the tools are added. Plans that describe "flipping the terraform/docker doc-guard that asserts out-of-scope" are over-claiming a break that does not exist — t-10 should update the comment prose for accuracy but does not need to satisfy a failing doc-guard there.
