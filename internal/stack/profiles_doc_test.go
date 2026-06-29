package stack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTerraformProfileDocumented pins c-4: the terraform marker profile is
// discoverable (the README lists it) and self-documenting (its comments mark it a
// marker-file stack and account for the checkov/dockle tools — checkov now ships
// here, dockle is noted as belonging to the docker profile). Deleting the README row
// or stripping the terraform.toml comments fails this test.
func TestTerraformProfileDocumented(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("profiles", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if !strings.Contains(string(readme), "terraform") {
		t.Error("profiles/README.md must mention the terraform profile so it is discoverable")
	}

	prof, err := os.ReadFile(filepath.Join("profiles", "terraform.toml"))
	if err != nil {
		t.Fatalf("read terraform.toml: %v", err)
	}
	doc := strings.ToLower(string(prof))
	if !strings.Contains(doc, "marker") {
		t.Error("terraform.toml comments must explain it is a marker-file stack")
	}
	for _, tool := range []string{"checkov", "dockle"} {
		if !strings.Contains(doc, tool) {
			t.Errorf("terraform.toml comments must account for the tool %q", tool)
		}
	}
}

// TestNewIaCProfilesDocumented pins c-6 for the deepened loadout: the kubernetes and
// cloudformation marker profiles are discoverable (README rows) and self-documenting
// (their comments mark them marker stacks and explain the content-sniff gate).
// Deleting a README row or stripping a profile's header comment fails this test.
func TestNewIaCProfilesDocumented(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("profiles", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	rd := string(readme)
	for _, id := range []string{"kubernetes", "cloudformation"} {
		if !strings.Contains(rd, id) {
			t.Errorf("profiles/README.md must mention the %q profile so it is discoverable", id)
		}
	}

	for _, f := range []string{"kubernetes.toml", "cloudformation.toml"} {
		prof, err := os.ReadFile(filepath.Join("profiles", f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		doc := strings.ToLower(string(prof))
		if !strings.Contains(doc, "marker") {
			t.Errorf("%s comments must explain it is a marker-file stack", f)
		}
		if !strings.Contains(doc, "content") {
			t.Errorf("%s comments must explain the content-sniff gate", f)
		}
	}
}
