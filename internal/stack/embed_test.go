package stack

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEmbeddedIncludesGo(t *testing.T) {
	emb, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	if ByID(emb, "go") == nil {
		t.Fatal("embedded profiles must include the Go profile")
	}
}

// TestEmbeddedDocker proves the docker marker profile ships with exactly the locked
// loadout: hadolint as both a scanner and an analyzer, a distinctly-named trivy config
// scanner, and the dockle image-layer scanner — and no checkov (an IaC-family scanner,
// not a container-image one).
func TestEmbeddedDocker(t *testing.T) {
	emb, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	d := ByID(emb, "docker")
	if d == nil {
		t.Fatal("embedded profiles must include the docker profile")
	}
	// It is a marker stack: pattern signals, no source extensions.
	if len(d.Signals.FilePatterns) == 0 {
		t.Error("docker profile must declare file_patterns (marker stack)")
	}
	if len(d.Signals.Exts) != 0 {
		t.Errorf("docker profile must declare no exts (marker stack), got %v", d.Signals.Exts)
	}

	// Group tools by kind and assert the exact loadout.
	scanners, analyzers := map[string]Tool{}, map[string]Tool{}
	for _, tool := range d.Tools {
		switch tool.Kind {
		case "scanner":
			scanners[tool.Name] = tool
		case "analyzer":
			analyzers[tool.Name] = tool
		default:
			t.Errorf("tool %q has unexpected kind %q", tool.Name, tool.Kind)
		}
		if tool.Name == "checkov" {
			t.Errorf("docker loadout must not ship %q (checkov is an IaC-family scanner, not a container-image one)", tool.Name)
		}
	}

	// dockle (container image-layer CIS) now ships in the docker loadout — surfaced in
	// detect; its no-image skip-with-reason is enforced on the run path (DecideDockle).
	if dk, ok := scanners["dockle"]; !ok {
		t.Error("docker loadout must ship the dockle scanner")
	} else if dk.EffectiveBin("") != "dockle" {
		t.Errorf("dockle must resolve to the dockle binary, got %q", dk.EffectiveBin(""))
	}

	// hadolint must appear as BOTH a scanner and an analyzer.
	if _, ok := scanners["hadolint"]; !ok {
		t.Error("docker loadout missing the hadolint scanner")
	}
	if _, ok := analyzers["hadolint"]; !ok {
		t.Error("docker loadout missing the hadolint analyzer")
	}
	// The compose-covering scanner must be named distinctly from the agnostic
	// "trivy" scanner, or the manifest dedup would collapse it away.
	tc, ok := scanners["trivy config"]
	if !ok {
		t.Error(`docker loadout missing the "trivy config" scanner (distinct from agnostic "trivy")`)
	} else if tc.EffectiveBin("") != "trivy" {
		t.Errorf(`"trivy config" must resolve to the trivy binary, got %q`, tc.EffectiveBin(""))
	}

	// The hadolint analyzer's dimension must be in the substantive allowlist, or the
	// existing quality TestCatalogExcludesCosmetic guard goes red once docker embeds.
	if got := analyzers["hadolint"].Dimension; got != "error-handling" {
		t.Errorf("hadolint analyzer dimension = %q, want a substantive dimension (error-handling)", got)
	}
}

// TestEmbeddedTerraform proves the terraform marker profile ships with exactly the
// locked loadout: the exact 5-glob marker set pinned against the shipped TOML, a
// distinctly-named trivy config scanner, the cross-family checkov scanner, and a
// tflint analyzer under error-handling — and no dockle (which scans container images,
// not IaC source).
func TestEmbeddedTerraform(t *testing.T) {
	emb, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	tf := ByID(emb, "terraform")
	if tf == nil {
		t.Fatal("embedded profiles must include the terraform profile")
	}
	// It is a marker stack: pattern signals, no source extensions.
	if len(tf.Signals.Exts) != 0 {
		t.Errorf("terraform profile must declare no exts (marker stack), got %v", tf.Signals.Exts)
	}
	// The exact locked marker set, pinned against the shipped terraform.toml — a
	// dropped/renamed/reordered pattern fails this assertion (flag-1 enforcement).
	wantPatterns := []string{"*.tf", "*.tf.json", "*.tfvars", "*.tfvars.json", "*.hcl"}
	if !reflect.DeepEqual(tf.Signals.FilePatterns, wantPatterns) {
		t.Errorf("terraform file_patterns = %v, want the locked set %v", tf.Signals.FilePatterns, wantPatterns)
	}

	// Group tools by kind and assert the exact loadout.
	scanners, analyzers := map[string]Tool{}, map[string]Tool{}
	for _, tool := range tf.Tools {
		switch tool.Kind {
		case "scanner":
			scanners[tool.Name] = tool
		case "analyzer":
			analyzers[tool.Name] = tool
		default:
			t.Errorf("tool %q has unexpected kind %q", tool.Name, tool.Kind)
		}
		if tool.Name == "dockle" {
			t.Errorf("terraform loadout must not ship %q (dockle scans container images, not IaC source)", tool.Name)
		}
	}

	// The IaC-misconfig scanner must be named distinctly from the agnostic "trivy"
	// scanner, or the manifest dedup would collapse it away.
	tc, ok := scanners["trivy config"]
	if !ok {
		t.Error(`terraform loadout missing the "trivy config" scanner (distinct from agnostic "trivy")`)
	} else if tc.EffectiveBin("") != "trivy" {
		t.Errorf(`"trivy config" must resolve to the trivy binary, got %q`, tc.EffectiveBin(""))
	}

	// checkov (cross-family IaC misconfiguration) now ships alongside trivy config,
	// kept distinct so the manifest dedup surfaces both. Its install hint must name the
	// Python toolchain.
	if ck, ok := scanners["checkov"]; !ok {
		t.Error("terraform loadout must ship the checkov scanner")
	} else if !strings.Contains(strings.ToLower(ck.Install), "pip") {
		t.Errorf("checkov install hint must name the Python toolchain (pip/pipx), got %q", ck.Install)
	}

	// tflint is the quality analyzer; its dimension must be error-handling (matching
	// the locked decision and the substantive-dimension allowlist).
	tfl, ok := analyzers["tflint"]
	if !ok {
		t.Error("terraform loadout missing the tflint analyzer")
	} else if tfl.Dimension != "error-handling" {
		t.Errorf("tflint analyzer dimension = %q, want error-handling", tfl.Dimension)
	}
}

// TestEmbeddedKubernetes proves the kubernetes marker profile ships with the locked
// content-sniff loadout: no exts, the exact glob set, an AND content gate of
// apiVersion+kind (pinned with reflect.DeepEqual), trivy config + checkov scanners,
// and a kube-linter analyzer under error-handling.
func TestEmbeddedKubernetes(t *testing.T) {
	emb, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	k := ByID(emb, "kubernetes")
	if k == nil {
		t.Fatal("embedded profiles must include the kubernetes profile")
	}
	if len(k.Signals.Exts) != 0 {
		t.Errorf("kubernetes profile must declare no exts (marker stack), got %v", k.Signals.Exts)
	}
	if want := []string{"*.yaml", "*.yml", "*.json"}; !reflect.DeepEqual(k.Signals.FilePatterns, want) {
		t.Errorf("kubernetes file_patterns = %v, want %v", k.Signals.FilePatterns, want)
	}
	// The content gate must decode non-empty and match the locked AND tokens — a
	// renamed [signals.content] schema would leave this empty and fail here.
	if want := []string{"apiVersion", "kind"}; !reflect.DeepEqual(k.Signals.Content.All, want) {
		t.Errorf("kubernetes content.all = %v, want %v (AND fingerprint)", k.Signals.Content.All, want)
	}

	scanners, analyzers := map[string]Tool{}, map[string]Tool{}
	for _, tool := range k.Tools {
		switch tool.Kind {
		case "scanner":
			scanners[tool.Name] = tool
		case "analyzer":
			analyzers[tool.Name] = tool
		default:
			t.Errorf("tool %q has unexpected kind %q", tool.Name, tool.Kind)
		}
	}
	if tc, ok := scanners["trivy config"]; !ok {
		t.Error(`kubernetes loadout missing the "trivy config" scanner`)
	} else if tc.EffectiveBin("") != "trivy" {
		t.Errorf(`"trivy config" must resolve to the trivy binary, got %q`, tc.EffectiveBin(""))
	}
	if _, ok := scanners["checkov"]; !ok {
		t.Error("kubernetes loadout missing the checkov scanner")
	}
	if kl, ok := analyzers["kube-linter"]; !ok {
		t.Error("kubernetes loadout missing the kube-linter analyzer")
	} else if kl.Dimension != "error-handling" {
		t.Errorf("kube-linter analyzer dimension = %q, want error-handling", kl.Dimension)
	}
}

// TestEmbeddedCloudFormation proves the cloudformation marker profile ships with the
// locked content-sniff loadout: no exts, the exact glob set, an OR content gate of
// AWSTemplateFormatVersion/Resources (pinned with reflect.DeepEqual), trivy config +
// checkov scanners, and a cfn-lint analyzer under error-handling.
func TestEmbeddedCloudFormation(t *testing.T) {
	emb, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	c := ByID(emb, "cloudformation")
	if c == nil {
		t.Fatal("embedded profiles must include the cloudformation profile")
	}
	if len(c.Signals.Exts) != 0 {
		t.Errorf("cloudformation profile must declare no exts (marker stack), got %v", c.Signals.Exts)
	}
	if want := []string{"*.yaml", "*.yml", "*.json"}; !reflect.DeepEqual(c.Signals.FilePatterns, want) {
		t.Errorf("cloudformation file_patterns = %v, want %v", c.Signals.FilePatterns, want)
	}
	if want := []string{"AWSTemplateFormatVersion", "Resources"}; !reflect.DeepEqual(c.Signals.Content.Any, want) {
		t.Errorf("cloudformation content.any = %v, want %v (OR fingerprint)", c.Signals.Content.Any, want)
	}

	scanners, analyzers := map[string]Tool{}, map[string]Tool{}
	for _, tool := range c.Tools {
		switch tool.Kind {
		case "scanner":
			scanners[tool.Name] = tool
		case "analyzer":
			analyzers[tool.Name] = tool
		default:
			t.Errorf("tool %q has unexpected kind %q", tool.Name, tool.Kind)
		}
	}
	if tc, ok := scanners["trivy config"]; !ok {
		t.Error(`cloudformation loadout missing the "trivy config" scanner`)
	} else if tc.EffectiveBin("") != "trivy" {
		t.Errorf(`"trivy config" must resolve to the trivy binary, got %q`, tc.EffectiveBin(""))
	}
	if _, ok := scanners["checkov"]; !ok {
		t.Error("cloudformation loadout missing the checkov scanner")
	}
	if cl, ok := analyzers["cfn-lint"]; !ok {
		t.Error("cloudformation loadout missing the cfn-lint analyzer")
	} else if cl.Dimension != "error-handling" {
		t.Errorf("cfn-lint analyzer dimension = %q, want error-handling", cl.Dimension)
	}
}

// TestMalformedDockerLikeProfileErrors proves the load path surfaces a malformed
// profile as an error rather than silently shipping a broken loadout — the guarantee
// that protects the embedded docker.toml.
func TestMalformedDockerLikeProfileErrors(t *testing.T) {
	if _, err := Decode([]byte("id = \"docker\"\n[signals]\n  file_patterns = [unterminated\n")); err == nil {
		t.Fatal("a malformed profile must surface a decode error")
	}
}

func TestUserDirWinsOnIDCollision(t *testing.T) {
	dir := t.TempDir()
	// A user profile with the same id as the embedded Go profile but a different
	// test command must win.
	writeFile(t, dir, "go.toml", "id = \"go\"\n[runtime.test]\n  run = \"go test -race ./...\"\n")

	merged, err := loadAllFrom(dir)
	if err != nil {
		t.Fatalf("loadAllFrom: %v", err)
	}
	got := ByID(merged, "go")
	if got == nil {
		t.Fatal("go profile missing after merge")
	}
	if got.Runtime.Test.Run != "go test -race ./..." {
		t.Errorf("user dir must win on id collision: got test command %q", got.Runtime.Test.Run)
	}
}

func TestMalformedUserProfileSurfacedNotSwallowed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "malformed.toml", "id = \nthis is not valid toml [[[\n")

	merged, err := loadAllFrom(dir)
	if err == nil {
		t.Fatal("a malformed user profile must surface an error")
	}
	if !strings.Contains(err.Error(), "malformed.toml") {
		t.Errorf("error must name the offending file, got: %v", err)
	}
	// The embedded Go profile must NOT be silently dropped.
	if ByID(merged, "go") == nil {
		t.Error("embedded Go profile was dropped because of a malformed user file")
	}
}

func TestUserDirAbsentFallsBack(t *testing.T) {
	// A home with no profiles/ subdir at all.
	dir := filepath.Join(t.TempDir(), "claude", "dross", "profiles")
	merged, err := loadAllFrom(dir)
	if err != nil {
		t.Fatalf("absent user dir must not error: %v", err)
	}
	if ByID(merged, "go") == nil {
		t.Fatal("embedded Go profile must remain when the user dir is absent")
	}
}

func TestReadmeDocumentsZeroCodeDropIn(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("profiles", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	doc := string(data)
	for _, want := range []string{"single TOML drop-in", "zero code change"} {
		if !strings.Contains(doc, want) {
			t.Errorf("README must document the drop-in: missing %q", want)
		}
	}
}
