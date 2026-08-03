package milestone

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v1.0.toml")

	original := &Milestone{
		Milestone: Meta{
			Version: "v1.0",
			Title:   "First release",
			Status:  "active",
			Started: "2026-05-02",
		},
		Scope: Scope{
			SuccessCriteria: []string{"users can sign up", "meal CRUD works"},
			NonGoals:        []string{"realtime collab"},
		},
		Phases: []string{"01-auth", "02-meals", "03-tagging"},
	}
	original.Milestone.Base = "milestone/v1.1"
	if err := original.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, loaded) {
		t.Errorf("round-trip drift:\norig: %+v\nload: %+v", original, loaded)
	}
	// Called out separately from the DeepEqual so a dropped `base` toml tag
	// names itself instead of printing two whole structs.
	if loaded.Milestone.Base != "milestone/v1.1" {
		t.Errorf("base read back as %q, want %q — is the toml tag present?", loaded.Milestone.Base, "milestone/v1.1")
	}
}

func TestBaseOr(t *testing.T) {
	cases := []struct {
		name       string
		recorded   string
		mainBranch string
		want       string
	}{
		{"no recorded base reads as main", "", "main", "main"},
		{"no recorded base honours the repo's main branch", "", "master", "master"},
		{"recorded base wins over the fallback", "milestone/v1.1", "main", "milestone/v1.1"},
		{"recorded base wins on a master repo too", "milestone/v1.1", "master", "milestone/v1.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &Milestone{Milestone: Meta{Version: "v1.2", Base: c.recorded}}
			if got := m.BaseOr(c.mainBranch); got != c.want {
				t.Errorf("BaseOr(%q) with base %q = %q, want %q", c.mainBranch, c.recorded, got, c.want)
			}
		})
	}
}

// A milestone toml written before v1.2 carries no `base` key at all. Decoding
// one must stay a no-error path — making the field required would break every
// milestone already on disk.
func TestLoadPreV12TomlWithoutBase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.1.toml")
	body := `[milestone]
  version = "v1.1"
  title = "Older milestone"
  status = "complete"
  started = "2026-06-01"

[scope]
  success_criteria = ["it works"]

phases = ["one", "two"]
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatalf("pre-v1.2 milestone failed to decode: %v", err)
	}
	if m.Milestone.Base != "" {
		t.Errorf("base = %q, want empty", m.Milestone.Base)
	}
	if got := m.BaseOr("main"); got != "main" {
		t.Errorf("BaseOr on a pre-v1.2 milestone = %q, want %q", got, "main")
	}
}

func TestLoadAll(t *testing.T) {
	root := t.TempDir()
	mDir := filepath.Join(root, "milestones")
	if err := os.MkdirAll(mDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(mDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("v1.1.toml", "[milestone]\n  version = \"v1.1\"\n")
	write("v1.2.toml", "[milestone]\n  version = \"v1.2\"\n  base = \"milestone/v1.1\"\n")

	all, err := LoadAll(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("loaded %d milestones, want 2: %v", len(all), all)
	}
	if got := all["v1.2"].Milestone.Base; got != "milestone/v1.1" {
		t.Errorf("v1.2 base = %q, want %q", got, "milestone/v1.1")
	}
	if got := all["v1.1"].BaseOr("main"); got != "main" {
		t.Errorf("v1.1 BaseOr = %q, want %q", got, "main")
	}
}

// LoadAll is a delete gate's input: a file it cannot read must fail the whole
// call, never quietly shrink the map into a "no dependents" answer.
func TestLoadAllFailsClosedOnBrokenToml(t *testing.T) {
	root := t.TempDir()
	mDir := filepath.Join(root, "milestones")
	if err := os.MkdirAll(mDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mDir, "v1.1.toml"), []byte("[milestone]\n  version = \"v1.1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mDir, "v1.2.toml"), []byte("[milestone\n  version = ***\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	all, err := LoadAll(root)
	if err == nil {
		t.Fatalf("broken toml was skipped silently; got %d milestones", len(all))
	}
	if all != nil {
		t.Errorf("expected a nil map alongside the error, got %v", all)
	}
	if !strings.Contains(err.Error(), "v1.2.toml") {
		t.Errorf("error %q does not name the unreadable file", err)
	}
}

func TestLoadAllEmpty(t *testing.T) {
	all, err := LoadAll(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("expected no milestones, got %v", all)
	}
}

func TestList(t *testing.T) {
	root := t.TempDir()
	mDir := filepath.Join(root, "milestones")
	_ = os.MkdirAll(mDir, 0o755)
	for _, v := range []string{"v0.1.toml", "v1.0.toml", "v2.0.toml"} {
		_ = os.WriteFile(filepath.Join(mDir, v), []byte(""), 0o644)
	}
	// Subdirectory should be ignored.
	_ = os.MkdirAll(filepath.Join(mDir, "v1.0"), 0o755)
	// Non-toml should be ignored.
	_ = os.WriteFile(filepath.Join(mDir, "notes.md"), []byte(""), 0o644)

	got, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"v0.1", "v1.0", "v2.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestListEmpty(t *testing.T) {
	got, err := List(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestFilePath(t *testing.T) {
	got := FilePath(".dross", "v1.0")
	want := filepath.Join(".dross", "milestones", "v1.0.toml")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
