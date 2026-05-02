package phase

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Meal Tagging System":        "meal-tagging-system",
		"  Auth   middleware  ":      "auth-middleware",
		"v1.0 — Bootstrap!":          "v1-0-bootstrap",
		"already-slug":               "already-slug",
		"with/slashes\\and:colons":   "with-slashes-and-colons",
		"":                           "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q): got %q want %q", in, got, want)
		}
	}
}

func TestList(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"01-auth", "02-billing", "10-stretch"} {
		if err := os.MkdirAll(filepath.Join(root, "phases", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// File alongside dirs should be ignored.
	_ = os.WriteFile(filepath.Join(root, "phases", "stray.txt"), nil, 0o644)

	got, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"01-auth", "02-billing", "10-stretch"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestListEmptyDir(t *testing.T) {
	got, err := List(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestSpecRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.toml")

	original := &Spec{
		Phase: SpecPhase{ID: "03-meal-tagging", Title: "Meal tagging", Milestone: "v1.2"},
		Criteria: []Criterion{
			{ID: "c-1", Text: "User can attach up to 10 tags per meal"},
			{ID: "c-2", Text: "Tags are case-insensitive on lookup"},
		},
		Decisions: []Decision{
			{Key: "tag_storage", Choice: "junction_table", Why: "many-to-many", Locked: true},
		},
		Deferred: []Deferred{
			{Text: "embedding-based suggestions", Why: "premature"},
		},
	}
	if err := original.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, loaded) {
		t.Errorf("spec round-trip drift:\norig: %+v\nload: %+v", original, loaded)
	}
}

func TestPlanRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.toml")

	original := &Plan{
		Phase: PlanPhase{ID: "03-meal-tagging"},
		Task: []Task{
			{
				ID: "t-1", Wave: 1, Title: "Schema",
				Files:        []string{"db/schema.ts"},
				Description:  "drizzle schema",
				Covers:       []string{"c-1", "c-2"},
				TestContract: []string{"unique constraint"},
			},
			{
				ID: "t-2", Wave: 2, Title: "API",
				Files:     []string{"src/routes/api/tags/+server.ts"},
				DependsOn: []string{"t-1"},
				Covers:    []string{"c-1"},
			},
		},
	}
	if err := original.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, loaded) {
		t.Errorf("plan round-trip drift:\norig: %+v\nload: %+v", original, loaded)
	}
}

func TestDir(t *testing.T) {
	got := Dir(".dross", "01-auth")
	want := filepath.Join(".dross", "phases", "01-auth")
	if got != want {
		t.Errorf("Dir: got %q want %q", got, want)
	}
}
