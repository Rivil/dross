package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/project"
)

// TestNothingOffersHybrid is c-3 as a grep over every surface a user reads to
// decide what to type.
//
// The prose in configenum.go is deliberately exempt: it explains why hybrid is
// ABSENT, which is the opposite of offering it. Exempting the file that removed
// the value — and only that file — is what keeps this test from either passing
// vacuously or forbidding the explanation.
func TestNothingOffersHybrid(t *testing.T) {
	root := repoRootForHybridTest(t)

	// Every file listed here is one a user reads a value out of.
	for _, rel := range []string{
		"assets/prompts/init.md",
		"assets/prompts/options.md",
		"internal/project/project.go",
	} {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		// project.go's comment now explains the removal, so the bare word is
		// allowed there only inside a quoted historical mention. Offering it as
		// a pipe-separated choice is what must be gone.
		for _, offer := range []string{"docker|native|hybrid", "docker | native | hybrid", "<docker|native|hybrid>"} {
			if strings.Contains(string(body), offer) {
				t.Errorf("%s still offers hybrid as a choice (%q)", rel, offer)
			}
		}
	}

	// The validate message is built from the Set, so it can only offer what the
	// Set holds — assert that directly rather than grepping the source.
	if strings.Contains(configenum.RuntimeModes.List(), "hybrid") {
		t.Errorf("RuntimeModes.List() still names hybrid: %q", configenum.RuntimeModes.List())
	}
	if got := configenum.RuntimeModes.List(); got != "docker | native" {
		t.Errorf("RuntimeModes.List() = %q, want exactly \"docker | native\" — the validate message interpolates it", got)
	}
}

// TestHybridIsRejectedNotReinterpreted: a repo that had set hybrid gets told,
// not silently migrated.
//
// Those repos were running native semantics the whole time (`Mode != "docker"`),
// so quietly rewriting the field to native would be dross deciding what the user
// meant. The value survives on disk exactly as written; what changes is that
// validate now says so.
func TestHybridIsRejectedNotReinterpreted(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")

	path := filepath.Join(dir, ".dross", project.File)
	p, err := project.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	p.Runtime.Mode = "hybrid"
	if err := p.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	var out string
	if err := runCmdCapturing(t, &out, Validate()); err == nil {
		t.Fatalf("validate passed runtime.mode = hybrid:\n%s", out)
	}
	if !strings.Contains(out, "hybrid") {
		t.Errorf("problem does not name the offending value:\n%s", out)
	}
	// The reader has to be told what to pick instead.
	if !strings.Contains(out, "docker") || !strings.Contains(out, "native") {
		t.Errorf("problem does not name the two survivors:\n%s", out)
	}

	// No silent migration: the field still holds what was written.
	reloaded, err := project.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Runtime.Mode != "hybrid" {
		t.Errorf("runtime.mode was rewritten to %q on load — a rejected value must survive as written, not be reinterpreted", reloaded.Runtime.Mode)
	}
}

// TestHybridIsNotSettable closes the other half: the setter refuses it too, so
// no new repo can reach the state above through the CLI.
func TestHybridIsNotSettable(t *testing.T) {
	p := &project.Project{}
	p.Runtime.Mode = "native"
	if err := writeDotted(p, "runtime.mode", "hybrid"); err == nil {
		t.Fatal("project set accepted runtime.mode = hybrid")
	}
	if p.Runtime.Mode != "native" {
		t.Errorf("a refused hybrid still changed the field to %q", p.Runtime.Mode)
	}
}

// repoRootForHybridTest walks up from the package dir to the module root, so
// the grep above reads the real assets/ and internal/ rather than a fixture.
func repoRootForHybridTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not find module root from the package dir")
	return ""
}
