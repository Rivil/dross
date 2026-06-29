package security

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/stack"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectLanguagesContextFree(t *testing.T) {
	// HOME-isolate so DetectLanguages (now profile-derived via LoadAll, reading
	// ~/.claude/dross/profiles) is independent of any real user overlay (flag #3).
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main")
	// A planted .dross/ holding a python file + planning artifacts. Detection must
	// NOT descend into .dross/ — if it did it would surface "python" and would be
	// reading planning artifacts, violating context-free.
	writeFile(t, filepath.Join(root, ".dross", "evil.py"), "import os")
	writeFile(t, filepath.Join(root, ".dross", "rules.toml"), "[[rule]]\ntext = 'x'")

	langs, err := DetectLanguages(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range langs {
		if l == "python" {
			t.Fatalf("detection descended into .dross/ (found python) — audit is not context-free; langs=%v", langs)
		}
	}
	if !contains(langs, "go") {
		t.Fatalf("expected go detected from main.go, got %v", langs)
	}
}

func TestDetectLanguagesUnknownExt(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // overlay-independent: see TestDetectLanguagesContextFree (flag #3)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "data.xyz"), "blob")
	writeFile(t, filepath.Join(root, "notes.unknownext"), "blob")

	langs, err := DetectLanguages(root)
	if err != nil {
		t.Fatalf("unknown extension crashed detection: %v", err)
	}
	if len(langs) != 0 {
		t.Fatalf("unknown extensions yielded languages: %v", langs)
	}
}

func TestBuildManifestRecordsMissing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main")
	allMissing := func(string) (string, error) { return "", errors.New("not found") }

	m, err := BuildManifest(root, allMissing)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Ran()) != 0 {
		t.Errorf("Ran() non-empty under an all-missing lookup: %v", m.Ran())
	}
	if len(m.Skipped()) == 0 {
		t.Fatal("manifest omits detected-but-uninstalled scanners; Skipped() is empty")
	}
	names := map[string]bool{}
	for _, s := range m.Skipped() {
		names[s.Name] = true
	}
	for _, want := range []string{"govulncheck", "gitleaks"} {
		if !names[want] {
			t.Errorf("manifest Skipped() missing %q (main.go → go, so the Go core + agnostic tools must appear)", want)
		}
	}
}

// toolNames counts how many times each scanner name appears in the manifest.
func toolNames(m Manifest) map[string]int {
	counts := map[string]int{}
	for _, t := range m.Tools {
		counts[t.Name]++
	}
	return counts
}

func allMissingLookup(string) (string, error) { return "", errors.New("not found") }

// TestBuildManifestMarkerDocker proves the manifest-path blind spot is closed: a repo
// whose only Docker signal is a marker file (no source extension) surfaces the Docker
// scanners. Before this, DetectLanguages was extension-only and missed it entirely.
func TestBuildManifestMarkerDocker(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Dockerfile"), "FROM scratch\n")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatal(err)
	}
	names := toolNames(m)
	for _, want := range []string{"hadolint", "trivy config"} {
		if names[want] == 0 {
			t.Errorf("Dockerfile-only repo: manifest missing %q — marker-stack scanners must surface (c-2); tools=%v", want, names)
		}
	}
}

// TestBuildManifestNoMarkerRegression guards c-6: a marker-less repo gets exactly the
// language + agnostic set and no Docker tools leak in.
func TestBuildManifestNoMarkerRegression(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatal(err)
	}
	names := toolNames(m)
	for _, want := range []string{"govulncheck", "gitleaks", "trivy"} {
		if names[want] == 0 {
			t.Errorf("Go repo: expected the Go core + agnostic scanner %q, got tools=%v", want, names)
		}
	}
	for _, absent := range []string{"hadolint", "trivy config"} {
		if names[absent] != 0 {
			t.Errorf("marker-less repo surfaced %q — no Docker tools must leak in (c-6)", absent)
		}
	}
}

// TestBuildManifestMarkerDedup proves the additive union dedups by name and that the
// Docker "trivy config" scanner survives distinctly alongside the agnostic "trivy".
func TestBuildManifestMarkerDedup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module x\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main")
	writeFile(t, filepath.Join(root, "Dockerfile"), "FROM scratch\n")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatal(err)
	}
	names := toolNames(m)
	for name, n := range names {
		if n != 1 {
			t.Errorf("scanner %q appears %d times — the seen dedup failed on the marker union", name, n)
		}
	}
	if names["trivy"] == 0 {
		t.Error("agnostic trivy scanner missing")
	}
	if names["trivy config"] == 0 {
		t.Error(`"trivy config" was deduped away — it must surface distinctly from the agnostic "trivy" (c-2)`)
	}
}

// TestBuildManifestMarkerTerraform proves a repo whose only IaC signal is a *.tf
// marker file surfaces the terraform scanner (trivy config) — the manifest blind
// spot closed for Terraform/IaC (c-1).
func TestBuildManifestMarkerTerraform(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.tf"), "resource \"null_resource\" \"x\" {}\n")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatal(err)
	}
	names := toolNames(m)
	if names["trivy config"] == 0 {
		t.Errorf("*.tf-only repo: manifest missing %q — terraform marker scanner must surface (c-1); tools=%v", "trivy config", names)
	}
}

// TestBuildManifestTerraformDedup proves the terraform "trivy config" scanner survives
// distinctly alongside the agnostic "trivy" each exactly once, and the Go core scanners
// remain (c-1).
func TestBuildManifestTerraformDedup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module x\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main")
	writeFile(t, filepath.Join(root, "main.tf"), "resource \"null_resource\" \"x\" {}\n")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatal(err)
	}
	names := toolNames(m)
	for name, n := range names {
		if n != 1 {
			t.Errorf("scanner %q appears %d times — the seen dedup failed on the terraform marker union", name, n)
		}
	}
	if names["trivy"] == 0 {
		t.Error("agnostic trivy scanner missing")
	}
	if names["trivy config"] == 0 {
		t.Error(`"trivy config" was deduped away — it must surface distinctly from the agnostic "trivy" (c-1)`)
	}
	for _, want := range []string{"govulncheck", "gitleaks"} {
		if names[want] == 0 {
			t.Errorf("Go+TF repo missing %q — the Go core + agnostic scanners must remain alongside the marker tools", want)
		}
	}
}

// TestBuildManifestTerraformMissingScannerSkipped proves c-3's go-testable half: under
// an all-missing lookup, "trivy config" is recorded skipped (keeping its install hint)
// and BuildManifest still returns nil — a missing trivy reads as "skipped, install X".
func TestBuildManifestTerraformMissingScannerSkipped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.tf"), "resource \"null_resource\" \"x\" {}\n")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatalf("BuildManifest aborted on a missing tool: %v", err)
	}
	var tc *ToolStatus
	for i := range m.Skipped() {
		if m.Skipped()[i].Name == "trivy config" {
			s := m.Skipped()[i]
			tc = &s
		}
	}
	if tc == nil {
		t.Fatalf(`"trivy config" not in Skipped() under all-missing lookup; skipped=%v`, m.Skipped())
	}
	if tc.Install == "" {
		t.Error(`skipped "trivy config" has no install hint`)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestSecurityReconDelegatesToStack(t *testing.T) {
	// Polyglot fixture plus a non-Go-only fixture — both must match stack.Detect-
	// Languages exactly, proving recon owns no second ext->lang map.
	for _, files := range [][]string{
		{"main.go", "app.py"}, // polyglot
		{"app.py", "lib.rb"},  // non-Go only
	} {
		root := t.TempDir()
		for _, f := range files {
			writeFile(t, filepath.Join(root, f), "x")
		}
		got, err := DetectLanguages(root)
		if err != nil {
			t.Fatal(err)
		}
		want, err := stack.DetectLanguages(root)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("security recon DetectLanguages=%v, stack.DetectLanguages=%v — recon is not delegating", got, want)
		}
	}
}

func TestNoDuplicateExtLangMap(t *testing.T) {
	// A standalone ext->lang map literal must not reappear in either recon source —
	// the whole point of the de-dup is one canonical map in internal/stack.
	for _, path := range []string{"recon.go", filepath.Join("..", "quality", "recon.go")} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		src := string(data)
		for _, marker := range []string{"extLang = map[string]string", `".swift": "swift"`} {
			if strings.Contains(src, marker) {
				t.Errorf("%s still carries a standalone ext->lang map (found %q) — delegate to stack instead", path, marker)
			}
		}
	}
}

// TestBuildManifest_svelteRepo_listsOsvScanner proves c-1's "dross security detect
// lists it for a repo of that language": a tree whose only source is one .svelte
// file surfaces the dedicated osv-scanner in the manifest. A .svelte file maps
// unambiguously to svelte (a bare .ts would resolve to svelte AND typescript).
func TestBuildManifest_svelteRepo_listsOsvScanner(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // embedded profiles only, no user overlay
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "widget.svelte"), "<script>let x = 1</script>")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatal(err)
	}
	if toolNames(m)["osv-scanner"] == 0 {
		t.Errorf("svelte repo: manifest missing dedicated osv-scanner; tools=%v", toolNames(m))
	}
}

func TestBuildManifest_dartRepo_listsOsvScanner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo.dart"), "void main() {}")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatal(err)
	}
	if toolNames(m)["osv-scanner"] == 0 {
		t.Errorf("dart repo: manifest missing dedicated osv-scanner; tools=%v", toolNames(m))
	}
}

// TestBuildManifestMarkerKubernetes proves a repo whose only signal is a content-
// confirmed Kubernetes manifest surfaces the k8s scanners (trivy config + checkov).
// The apiVersion+kind tokens are the content gate; without them the profile would not
// surface (a plain YAML must not match).
func TestBuildManifestMarkerKubernetes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "deployment.yaml"), "apiVersion: apps/v1\nkind: Deployment\n")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatal(err)
	}
	names := toolNames(m)
	for _, want := range []string{"trivy config", "checkov"} {
		if names[want] == 0 {
			t.Errorf("k8s manifest repo: manifest missing %q — content-sniff k8s scanners must surface (c-1); tools=%v", want, names)
		}
	}
}

// TestBuildManifestMarkerKubernetesPlainYAMLIgnored guards the content gate: a plain
// YAML file (no apiVersion+kind) must NOT surface the kubernetes scanners.
func TestBuildManifestMarkerKubernetesPlainYAMLIgnored(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "config.yaml"), "name: my-app\nport: 8080\n")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatal(err)
	}
	// checkov is unique to the IaC marker profiles; a plain YAML must not pull it in.
	if toolNames(m)["checkov"] != 0 {
		t.Errorf("plain YAML surfaced checkov — content gate failed to keep non-manifest YAML out; tools=%v", toolNames(m))
	}
}

// TestBuildManifestMarkerCloudformation proves a repo whose only signal is a content-
// confirmed CloudFormation template surfaces the cfn scanners (trivy config + checkov).
func TestBuildManifestMarkerCloudformation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "template.yaml"), "AWSTemplateFormatVersion: '2010-09-09'\nResources: {}\n")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatal(err)
	}
	names := toolNames(m)
	for _, want := range []string{"trivy config", "checkov"} {
		if names[want] == 0 {
			t.Errorf("CFN template repo: manifest missing %q — content-sniff cfn scanners must surface (c-2); tools=%v", want, names)
		}
	}
}

// TestBuildManifestCheckovKeptBesideTrivyConfig proves the dedup keeps checkov as a
// distinct scanner alongside trivy config across a multi-IaC repo (terraform + k8s +
// cfn + docker, plus Go): trivy / trivy config / checkov each appear exactly once, and
// the Go core + agnostic scanners remain.
func TestBuildManifestCheckovKeptBesideTrivyConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module x\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main")
	writeFile(t, filepath.Join(root, "main.tf"), "resource \"null_resource\" \"x\" {}\n")
	writeFile(t, filepath.Join(root, "deployment.yaml"), "apiVersion: apps/v1\nkind: Deployment\n")
	writeFile(t, filepath.Join(root, "template.yaml"), "AWSTemplateFormatVersion: '2010-09-09'\nResources: {}\n")
	writeFile(t, filepath.Join(root, "Dockerfile"), "FROM scratch\n")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatal(err)
	}
	names := toolNames(m)
	for _, name := range []string{"trivy", "trivy config", "checkov"} {
		if names[name] != 1 {
			t.Errorf("scanner %q appears %d times across the multi-IaC repo, want exactly 1 (dedup must keep it distinct, not collapse or duplicate); tools=%v", name, names[name], names)
		}
	}
	for _, want := range []string{"govulncheck", "gitleaks"} {
		if names[want] == 0 {
			t.Errorf("multi-IaC repo dropped %q — the Go core + agnostic scanners must remain beside the marker tools; tools=%v", want, names)
		}
	}
}

// TestBuildManifestCheckovSkipHint proves a missing checkov is recorded skipped with a
// Python install hint, and BuildManifest still returns nil (a missing tool degrades,
// never aborts) — checkov is never silently omitted.
func TestBuildManifestCheckovSkipHint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "deployment.yaml"), "apiVersion: apps/v1\nkind: Deployment\n")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatalf("BuildManifest aborted on a missing tool: %v", err)
	}
	var ck *ToolStatus
	for i := range m.Skipped() {
		if m.Skipped()[i].Name == "checkov" {
			s := m.Skipped()[i]
			ck = &s
		}
	}
	if ck == nil {
		t.Fatalf("checkov not in Skipped() under all-missing lookup — it must never be silently omitted; skipped=%v", m.Skipped())
	}
	if !strings.Contains(strings.ToLower(ck.Install), "pip") {
		t.Errorf("skipped checkov install hint must name the Python toolchain (pip/pipx), got %q", ck.Install)
	}
}

// TestBuildManifestMarkerDockerDockle proves dockle surfaces in the assembled docker
// security manifest (installed-vs-missing), mirroring how TestBuildManifestMarkerDocker
// asserts trivy config. The image-specific run/skip decision lives on the run path
// (DecideDockle); here we only assert dockle is part of the detect-time loadout.
func TestBuildManifestMarkerDockerDockle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Dockerfile"), "FROM scratch\n")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatal(err)
	}
	if toolNames(m)["dockle"] == 0 {
		t.Errorf("Dockerfile repo: manifest missing %q — dockle must surface in the docker loadout (c-3); tools=%v", "dockle", toolNames(m))
	}
}

// TestBuildManifest_missingDedicatedScannerSkipped proves c-4: under an all-missing
// lookup, svelte's osv-scanner is recorded as skipped (keeping its install hint) and
// BuildManifest still returns nil — a missing tool degrades, never aborts the run.
func TestBuildManifest_missingDedicatedScannerSkipped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "widget.svelte"), "<script>let x = 1</script>")

	m, err := BuildManifest(root, allMissingLookup)
	if err != nil {
		t.Fatalf("BuildManifest aborted on a missing tool: %v", err)
	}
	var osv *ToolStatus
	for i := range m.Skipped() {
		if m.Skipped()[i].Name == "osv-scanner" {
			s := m.Skipped()[i]
			osv = &s
		}
	}
	if osv == nil {
		t.Fatalf("svelte osv-scanner not in Skipped() under all-missing lookup; skipped=%v", m.Skipped())
	}
	if osv.Install == "" {
		t.Error("skipped osv-scanner has no install hint")
	}
}
