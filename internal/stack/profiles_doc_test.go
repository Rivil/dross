package stack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTerraformProfileDocumented pins c-4: the terraform marker profile is
// discoverable (the README lists it) and self-documenting (its comments mark it a
// marker-file stack and name the out-of-scope tools). Deleting the README row or
// stripping the terraform.toml comments fails this test.
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
			t.Errorf("terraform.toml comments must name out-of-scope tool %q", tool)
		}
	}
}
