# Synthesis — container-iac-scanning

Judging three independently-drafted decompositions (risk / mvp / verification).
I authored none. Skeleton picked on contract quality; grafts pulled from the
runners-up; genuine divergences recorded rather than papered over. All three
respect the locked decisions (dedicated `terraform` profile; the 5 marker
patterns; tflint→error-handling; data-only drop-in, no Go production code; c-3 =
committed fixture + manual-run record, with the detect-output assertion go-tested
against the manifest).

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
| --- | --- | --- | --- | --- |
| risk | A — all c-1..c-4 owned; explicit per-criterion map; each failure mode has exactly one owner | A — per-pattern sub-rows, dedup-count + keeps-govulncheck/gitleaks, case-insensitive + directory + .tfstate/.tf.bak guards | A — balanced 5 tasks; detection split from recon by blast radius | A — 2 waves, t-2..t-5 parallel on t-1, no artificial ladder |
| mvp | B+ — all covered but 3 criteria lean on a single t-1; coarser mapping | B — correct surfaces named but combined/coarse contracts; no per-pattern or guard rows | B- — 3 tasks; bundles security+quality recon (2 pkgs, disjoint blast radius) into one; folds doc into t-1 | A — 2 waves, t-2/t-3 parallel on t-1 |
| verification | A — all covered; each criterion mapped to multiple tasks; cleanest c-3 two-surface split | A+ — exact test names, per-row failure modes, EffectiveBin("") , checkov/terrascan absence, locked c-3 manual-vs-go split spelled out | A- — 6 tasks; layer-clean, but the standalone doc-test task (t-5) is the one debatable split | A — 2 waves, t-2..t-6 parallel on t-1 |

**Skeleton: `verification`.** Its contracts are the most directly test-translatable
(named functions, one failure mode per row) and it gets the c-3 split exactly
right per the spec (go-test the detect-output half in t-3(c)/t-4(b); manual-run
record for the finding proof in t-6). Its two gaps — thinner false-positive
guards and a dedup row that doesn't assert the agnostic scanners survive — are
filled by grafting from `risk`.

## Merged plan

```
Phase container-iac-scanning — Container & IaC scanning — 6 tasks across 2 waves

Wave 1
  t-1  Add terraform.toml marker profile                                  [verification]
       files:    internal/stack/profiles/terraform.toml
       covers:   c-1, c-2, c-4
       contract: Mirror docker.toml. id="terraform", title="Terraform";
                 [signals].file_patterns = the LOCKED set (*.tf, *.tf.json, *.tfvars,
                 *.tfvars.json, *.hcl) and NO exts (marker stack). Loadout: scanner
                 "trivy config" (bin="trivy", core) DISTINCT from agnostic "trivy";
                 analyzer "tflint" (dimension="error-handling", core). Header comment
                 marks it a marker-file stack, notes the accepted *.hcl Packer/Nomad
                 false positive, and states checkov/terrascan are out of scope. Pure
                 data, no logic — its guards are the assertions in t-2..t-6. (README
                 entry lives in t-5; see Disagreement 1a.)

Wave 2 (every task strictly needs t-1's terraform.toml; none consumes another's output)
  t-2  Pin terraform patterns + embedded loadout                         [verification + risk]
       files:    internal/stack/profile_test.go
                 internal/stack/embed_test.go
       covers:   c-1, c-2
       depends:  t-1
       contract: (a) TestTerraformFilePatternMatch (mirror TestFilePatternMatch):
                 "main.tf","vars.tf.json","prod.tfvars","x.tfvars.json","packer.hcl"
                 MUST match — one row per pattern, so dropping ".tf.json" or ".hcl"
                 fails that row (proves *.tf does NOT silently cover *.tf.json). [verification+risk]
                 MUST NOT match: "main.go","README.md","notfile.tfx", and the risk
                 false-positive guards — "main.tfstate" (filepath.Match("*.tf",
                 "x.tfstate") must not fire), "module.tf.bak", a *directory* named
                 "stuff.tf" surfaces nothing (d.IsDir() skip), and "Main.TF" still
                 matches (case-insensitive regression guard). [risk graft]
                 (b) TestEmbeddedTerraform (mirror TestEmbeddedDocker): ByID(Embedded(),
                 "terraform") non-nil; FilePatterns non-empty AND Exts empty;
                 scanners["trivy config"].EffectiveBin("")=="trivy"; analyzers["tflint"].
                 Dimension=="error-handling"; neither "checkov" nor "terrascan" appears.
                 Renaming the id, collapsing "trivy config" into "trivy", or retagging
                 tflint's dimension each fails a distinct row. [verification]

  t-3  Assert terraform scanner surfaces & skips (security)              [verification + risk]
       files:    internal/security/recon_test.go
       covers:   c-1, c-3
       depends:  t-1
       contract: (a) TestBuildManifestMarkerTerraform (mirror MarkerDocker): a *.tf-only
                 repo (main.tf) yields a manifest containing "trivy config"; removing the
                 scanner from terraform.toml empties that row. [verification]
                 (b) TestBuildManifestTerraformDedup (mirror MarkerDedup): a go.mod +
                 main.go + main.tf repo keeps BOTH agnostic "trivy" AND "trivy config"
                 each EXACTLY once, and STILL keeps govulncheck + gitleaks — renaming the
                 profile scanner to bare "trivy" collapses it into the agnostic entry and
                 fails the count. [verification + risk graft: keeps-other-scanners]
                 (c) TestBuildManifest_terraform_missingScannerSkipped (c-3 go-testable
                 half): under an all-missing lookPath, "trivy config" is in m.Skipped()
                 with a non-empty Install hint and BuildManifest returns nil — a missing
                 trivy reads as "skipped, install X", never a silent all-clear. [verification]

  t-4  Assert tflint analyzer surfaces & skips (quality)                 [verification]
       files:    internal/quality/recon_test.go
       covers:   c-2, c-3
       depends:  t-1
       contract: (a) TestQualityManifestMarkerTerraform (mirror MarkerDocker): a *.tf-only
                 repo surfaces "tflint" with Dimension==ErrorHandling AND still carries the
                 agnostic "scc"/"jscpd" — so a .tf repo gets an IaC-specific analyzer, not
                 only the agnostic fallback. Dropping tflint fails the tflint row; a
                 regression that replaces (not unions) the marker analyzers fails the
                 scc/jscpd rows; flipping tflint's dimension fails the dimension row.
                 (b) TestBuildManifest_terraform_missingAnalyzerSkipped (c-3 go-testable
                 half): under an all-missing lookup, "tflint" is in m.Skipped() with a
                 non-empty Install hint and BuildManifest returns nil — degrades, never
                 aborts.

  t-5  Document terraform profile + doc-presence guard                   [verification]
       files:    internal/stack/profiles/README.md
                 internal/stack/profiles_doc_test.go
       covers:   c-4
       depends:  t-1
       contract: Add a terraform row to profiles/README.md, then
                 TestTerraformProfileDocumented (os.ReadFile, mirroring the
                 TestNoDockerHardcode read-the-source idiom): README.md contains
                 "terraform"; terraform.toml's leading comment block contains "marker"
                 AND names the out-of-scope tools ("checkov"/"terrascan"). Deleting the
                 README row, or stripping the header comment, fails its assertion.
                 (See Disagreement 1a — risk/mvp fold this into t-1 without a test.)

  t-6  Commit IaC fixture + manual-run record                           [verification + risk]
       files:    fixtures/terraform-c3/main.tf
                 fixtures/terraform-c3/RUN.md
                 fixtures/terraform-c3/expected-finding.txt
       covers:   c-3
       depends:  t-1
       contract: Mirror fixtures/multilang-c3 (the prior phase's findings_proof pattern).
                 main.tf plants ONE deterministic defect the agnostic fallback is blind to:
                 a declared-but-unused variable "unused" {} that tflint flags as
                 terraform_unused_declarations (plugin-free, default ruleset — the IaC
                 analogue of knip's unused export). [verification — see Disagreement 2]
                 expected-finding.txt PINS that exact warning (rule id @ main.tf:line).
                 RUN.md records the manual run after `make install` (rule r-01):
                 `dross security detect` / `dross quality detect` list trivy-config + tflint
                 installed-vs-missing-with-hint, then tflint surfaces the finding while the
                 agnostic fallback (trivy fs / gitleaks / scc / jscpd) is blind to it. [risk
                 graft: assert the fallback's blindness]. This is the LOCKED manual-run
                 record, NOT a go-test that shells out to tflint/trivy — remove/rename the
                 planted variable and RUN.md stops reproducing + expected-finding.txt no
                 longer matches. (verify.toml citation is produced at verify time, not here.)
```

Coverage (merged): c-1 → t-1,t-2,t-3 · c-2 → t-1,t-2,t-4 · c-3 → t-3,t-4,t-6 ·
c-4 → t-1,t-5. Every criterion owned, each with at least one enforceable contract.

## Disagreements

**1a. Does c-4's README get its own test task, or fold into t-1?**
- verification: standalone t-5 (README row + `TestTerraformProfileDocumented` os.ReadFile guard).
- risk: README authored inside t-1, no doc test.
- mvp: README inside t-1, explicitly rejects a doc task as "redundant ceremony."
- **Provisional default: keep verification's separate, tested t-5.**
- Why it matters: a folded README/header has no failing-test guard, so c-4 silently
  rots if the comment or row is dropped. The cost is one extra task that mvp would
  call ceremony. I favoured enforceability — every other criterion here is
  test-pinned, and c-4 *can* be pinned cheaply by an os.ReadFile assertion.

**1b. Security + quality recon — one task or two?**
- risk & verification: two tasks (t-3 security, t-4 quality) — different packages,
  different blast radius (dedup-collision vs analyzer-dimension), one owner each.
- mvp: one combined t-2 spanning both recon_test.go files; argues same layer, no
  inter-dependency, "splitting buys nothing."
- **Provisional default: two tasks (risk/verification, 2 lenses).**
- Why it matters: the two failure modes are genuinely different surfaces (a
  trivy-config dedup collision vs a tflint dimension regression). Two atomic commits
  localise a future revert. mvp's merge is defensible and lighter, but loses the
  one-owner-per-failure-mode property.

**2. Fixture defect — tflint analyzer finding vs trivy-config scanner misconfig?** (most consequential)
- verification & mvp: a tflint `terraform_unused_declarations` (unused variable) —
  plugin-free, deterministic, no cloud ruleset.
- risk: a trivy-config AWS misconfig (open security group / unencrypted S3) pinned
  to its AVD rule id.
- **Provisional default: tflint unused-variable (verification/mvp, 2 lenses).**
- Why it matters: the tflint defect is deterministic and dependency-light, but it
  exercises the *quality/analyzer* path — the c-3 fixture then demonstrates tflint,
  NOT `trivy config`. The scanner's real-world finding (c-1's headline tool) is left
  proven only by the manifest test (t-3), never by an end-to-end run. risk's choice
  would close that gap at the cost of a heavier trivy ruleset and a less stable
  pinned finding. If reviewers want the scanner exercised end-to-end too, t-6's
  fixture should be revisited to add a trivy-config block (still inside risk's draft,
  so no invented task).

**3. Does the fixture commit an `expected-finding.txt`?**
- risk & verification: yes — pins the exact rule id @ line so RUN.md reproducibility
  is mechanically checkable.
- mvp: no — only main.tf + RUN.md, relying on RUN.md prose.
- **Provisional default: include expected-finding.txt (risk/verification, 2 lenses).**
- Why it matters: it converts "RUN.md reproduces" from a prose claim into a pinned
  comparison; mirrors the prior multilang-c3 proof. Marginal extra file, real
  regression signal.

**4. Fixture path + stack-test file layout (minor).**
- Fixture dir: `fixtures/terraform-c3` (verification) vs `fixtures/container-iac-c3`
  (mvp) vs `fixtures/iac-c3/tf-misconfig` (risk). **Default: `fixtures/terraform-c3`**
  — names the real tech, closest mirror to the `fixtures/multilang-c3` precedent.
- Stack test file: `profile_test.go` + `embed_test.go` (verification) vs
  `detect_test.go` (risk) vs `embed_test.go` only (mvp). **Default: verification's
  two-file split** — separates the pattern-match test from the embedded-loadout test.
- Why it matters: low stakes (naming/placement), but flagged so the executor follows
  one convention rather than mixing all three at commit time.
```
synthesis: 6 tasks across 2 waves, 4 disagreements
```
