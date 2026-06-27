# c-3 findings proof — dedicated IaC tools surface what the agnostic fallback misses

This is the documented manual run backing acceptance criterion **c-3**: on a
repo whose only signal is Terraform/IaC marker files, the dedicated tools
(`trivy config`, `tflint`) are listed installed-vs-missing-with-hint (never a
silent all-clear), and each surfaces a real finding the language-agnostic
fallback (gitleaks / semgrep / scc / jscpd) cannot.

Like the `multilang-c3` precedent, this is **not** a `go test` gate — shelling
`trivy`/`tflint` out of `go test` fights the single-static-binary ethos. It is a
committed fixture (`main.tf`) plus this reproducible record.

## Fixture

`main.tf` plants two defects, one per dedicated tool:

1. **trivy config (security):** an `aws_security_group` with a `0.0.0.0/0` SSH
   ingress — a misconfiguration. A secret scanner (gitleaks) and a complexity/LOC
   tool (scc) cannot see it.
2. **tflint (quality / error-handling):** a declared-but-unused `variable "unused"`
   — `terraform_unused_declarations`. The agnostic fallback has no concept of it.

## Reproduce

Pre-req (rule r-01): `make install` first so the `terraform` marker profile (which
wires `trivy config` + `tflint`) is live in the installed `dross` binary.

```
cd fixtures/terraform-c3

# 1. detect — the IaC tools are listed installed-vs-missing-with-hint
dross security detect
dross quality detect

# 2. dedicated tools surface the findings
trivy config .          # security: the open-ingress misconfig
tflint                  # quality: the unused declaration (install: brew install tflint)

# 3. agnostic fallback is blind to both
scc .                   # complexity/LOC only
gitleaks detect -s .    # secrets only
```

## Recorded output (2026-06-27, trivy 0.70.0)

### 1. `dross security detect` / `dross quality detect` — OBSERVED

```
$ dross security detect
languages:
scanners:
  [installed] gitleaks
  [installed] semgrep
  [installed] trivy
  [installed] trivy config        # ← terraform marker profile (distinct from agnostic trivy)

$ dross quality detect
languages:
analyzers:
  [installed] scc
  [missing]   jscpd  — npm install -g jscpd  (or see github.com/kucherenko/jscpd)
  [missing]   tflint — brew install tflint   (or see github.com/terraform-linters/tflint)
```

`languages:` is empty because Terraform is a **marker-file stack** — surfaced
additively, never as a primary language. `trivy config` and `tflint` appear purely
because the marker profile detected `main.tf`. A missing `tflint` reads as
`[missing]` with an install hint — never a silent all-clear (c-3).

### 2a. `trivy config .` — the dedicated security finding (OBSERVED)

```
main.tf (terraform)
Tests: 3 (SUCCESSES: 0, FAILURES: 3)
Failures: 3 (LOW: 2, HIGH: 1)

AWS-0107 (HIGH): Security group rule allows unrestricted ingress from any IP address.
 main.tf:22
   via main.tf:18-23 (ingress)
    via main.tf:15-24 (aws_security_group.open)
```

✅ trivy config surfaces the open-ingress misconfig (AVD-AWS-0107). Pinned in
`expected-finding.txt`. (Also AWS-0099 / AWS-0124, both LOW — missing descriptions.)

### 2b. `tflint` — the dedicated quality finding (EXPECTED, not observed here)

`tflint` is **not installed** in this recording session (`dross quality detect`
lists it `[missing]` above), so its output is **not** recorded as observed. Per
tflint's default ruleset, running it after `brew install tflint` flags:

```
Warning: variable "unused" is declared but not used (terraform_unused_declarations)
  on main.tf line 7
```

Pinned as the EXPECTED finding in `expected-finding.txt`. Re-run this section once
`tflint` is installed to convert it from expected to observed.

### 3. agnostic fallback — blind to both (OBSERVED)

```
$ scc .
Terraform   1 file   24 lines   13 code   complexity 0
```

scc reports line counts and complexity — no concept of a misconfiguration or an
unused declaration.

```
$ gitleaks detect -s .
INF no leaks found
```

gitleaks scans for secrets — nothing to find here.

## Conclusion

On a Terraform-only repo the dedicated IaC tools surface real findings — an
open-ingress misconfiguration (`trivy config`, observed) and an unused declaration
(`tflint`, expected) — that the agnostic fallback (scc complexity/LOC, jscpd
duplication, gitleaks secrets) is structurally blind to. That is the c-3 delta for
the container/IaC marker surface.
