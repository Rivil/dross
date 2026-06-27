# c-5 findings proof — the deepened container/IaC loadout across three families

This is the documented manual run backing acceptance criterion **c-5**: on a repo
whose only signals are container/IaC marker files, every tool in the deepened loadout
(`trivy config`, `checkov`, `dockle`, `kube-linter`, `cfn-lint`) is listed
installed-vs-missing-with-hint (never a silent all-clear), and the installed dedicated
tools surface real findings the language-agnostic fallback (gitleaks / semgrep / scc /
jscpd) cannot.

Like the `terraform-c3` / `multilang-c3` precedents this is **not** a `go test` gate —
shelling `trivy`/`checkov`/… out of `go test` fights the single-static-binary ethos.
It is a committed fixture plus this reproducible record.

## Fixture

Three files, one per marker family, each carrying its content-sniff fingerprint and a
planted defect the agnostic fallback is blind to:

- **`deployment.yaml`** (kubernetes — `apiVersion` + `kind`): a **privileged**
  container on an unpinned `:latest` image with no resource limits.
- **`template.yaml`** (cloudformation — `AWSTemplateFormatVersion` + `Resources`): a
  **public-read S3 bucket** and an SSH security group open to `0.0.0.0/0`.
- **`Dockerfile`** (docker): `:latest` base, `apt-get` without cleanup, **no USER**
  (runs as root).

## Reproduce

Pre-req (rule r-01): `make install` first so the `kubernetes` / `cloudformation` /
`docker` marker profiles are live in the installed `dross` binary.

```
cd fixtures/iac-multi-c5

# 1. detect — every new tool listed installed-vs-missing-with-hint
dross security detect
dross quality detect

# 2. installed dedicated tools surface the findings
trivy config .          # security: k8s + cfn + dockerfile misconfigs
hadolint Dockerfile     # docker quality/security
# checkov / kube-linter / cfn-lint: install to observe (see hints below)
# dockle: needs a BUILT image — supply one: dross security run --image <ref>

# 3. agnostic fallback is blind to all of it
scc .                   # complexity/LOC only
gitleaks detect -s .    # secrets only
```

## Recorded output (2026-06-27, trivy 0.70.0, hadolint 2.14.0)

### 1. `dross security detect` / `dross quality detect` — OBSERVED

```
$ dross security detect
languages:
scanners:
  [installed] gitleaks
  [installed] semgrep
  [installed] trivy
  [installed] trivy config
  [missing]   checkov  — pipx install checkov  (or pip install checkov)
  [installed] hadolint
  [missing]   dockle  — brew install goodwithtech/r/dockle  (or see github.com/goodwithtech/dockle)

$ dross quality detect
languages:
analyzers:
  [installed] scc
  [missing]   jscpd  — npm install -g jscpd  (or see github.com/kucherenko/jscpd)
  [missing]   cfn-lint  — pipx install cfn-lint  (or pip install cfn-lint)
  [installed] hadolint
  [missing]   kube-linter  — brew install kube-linter  (or see github.com/stackrox/kube-linter)
```

`languages:` is empty because these are **marker-file stacks** — surfaced additively,
never as a primary language. `trivy config`, `checkov`, `dockle`, `kube-linter`, and
`cfn-lint` appear purely because the marker profiles content-matched the three fixture
files. Every missing tool reads `[missing]` with an install hint — **never a silent
all-clear** (c-5).

### 2. `trivy config .` — dedicated security findings across all three families (OBSERVED)

```
Report Summary
  Dockerfile      dockerfile        4 failures
  deployment.yaml kubernetes       18 failures
  template.yaml   cloudformation   11 failures

deployment.yaml (kubernetes):
  KSV-0017 (HIGH): Container 'web' should set 'securityContext.privileged' to false
  KSV-0012 (MEDIUM): ... runAsNonRoot ; KSV-0014 (HIGH): ... readOnlyRootFilesystem

template.yaml (cloudformation):
  AWS-0086 (HIGH): No public access block so not blocking public acls   (PublicBucket)
  AWS-0087 (HIGH): No public access block so not blocking public policies

Dockerfile (dockerfile):
  DS-0002 (HIGH): Specify at least 1 USER command (non-root)
  DS-0001 (MEDIUM): Specify a tag in the 'FROM' statement
```

✅ One trivy config invocation, three families correctly classified — each surfacing a
HIGH misconfiguration. Pinned in `expected-finding.txt`.

### 3. `hadolint Dockerfile` — docker quality/security (OBSERVED)

```
Dockerfile:11 DL3007 warning: Using latest is prone to errors — pin the version
Dockerfile:12 DL3008 warning: Pin versions in apt get install
Dockerfile:12 DL3009 info: Delete the apt lists after installing something
```

### 4. checkov / kube-linter / cfn-lint — EXPECTED (not installed this session)

`dross … detect` lists all three `[missing]` with install hints above, so their output
is **not** recorded as observed. Once installed they add: checkov (cross-family —
e.g. `CKV_K8S_16` privileged container, S3 public-access on the bucket), kube-linter
(k8s production-readiness on the privileged container), cfn-lint (template structure).
Pinned as EXPECTED in `expected-finding.txt`.

### 5. dockle — EXPECTED, skip-with-reason on a source fixture

dockle scans a **built image**, which dross never builds. On this source-only fixture
`dross security run` records dockle skipped-with-reason; supply a built image to scan:

```
$ dross security run --image <built-image-ref>     # or $DROSS_IMAGE
  dockle: scanning image <built-image-ref>
# with no image:
  dockle: skipped — dockle needs a built image; run docker build or supply --image <ref>
```

Against a built image of this Dockerfile dockle would flag `CIS-DI-0001` (create a
non-root user). EXPECTED, not observed.

### 6. agnostic fallback — blind to all of it (OBSERVED)

```
$ scc .
Dockerfile  1 file   ...   complexity 1
YAML        ...                          # line counts only — no misconfig concept

$ gitleaks detect -s .
INF no leaks found                       # secrets only
```

## Conclusion

On a container/IaC-only repo the deepened loadout surfaces real findings across all
three marker families — a privileged pod and a public bucket and a root container
(`trivy config`, observed; `checkov`/`kube-linter`/`cfn-lint`/`dockle`, expected) —
that the agnostic fallback (scc complexity/LOC, jscpd duplication, gitleaks secrets) is
structurally blind to. Every tool, installed or not, is surfaced with status and a
hint — never a silent all-clear. That is the c-5 delta for the deepened container/IaC
surface.
