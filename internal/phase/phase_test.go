package phase

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Meal Tagging System":      "meal-tagging-system",
		"  Auth   middleware  ":    "auth-middleware",
		"v1.0 — Bootstrap!":        "v1-0-bootstrap",
		"already-slug":             "already-slug",
		"with/slashes\\and:colons": "with-slashes-and-colons",
		"":                         "",
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

// TestDeferredTargetRoundTrip pins the optional Target field: a routed entry
// reads its slug back, and a target-less ("someday") entry must NOT emit a
// `target =` key — dropping omitempty would rewrite every legacy spec.
func TestDeferredTargetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.toml")

	original := &Spec{
		Phase:    SpecPhase{ID: "host-phase", Title: "Host"},
		Criteria: []Criterion{{ID: "c-1", Text: "does a thing"}},
		Deferred: []Deferred{
			{Text: "routed idea", Target: "target-phase"},
			{Text: "someday idea"},
		},
	}
	if err := original.Save(path); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Exactly one `target =` line — the routed entry only; the someday entry
	// must omit it (omitempty back-compat).
	if got := strings.Count(string(raw), "target ="); got != 1 {
		t.Errorf("expected exactly 1 `target =` line (routed entry only), got %d:\n%s", got, raw)
	}

	loaded, err := LoadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Deferred[0].Target != "target-phase" {
		t.Errorf("routed target not read back: %+v", loaded.Deferred[0])
	}
	if loaded.Deferred[1].Target != "" {
		t.Errorf("someday entry should have empty target, got %q", loaded.Deferred[1].Target)
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

func TestStripLegacyPrefix(t *testing.T) {
	cases := map[string]string{
		"03-fix-foo":    "fix-foo",
		"fix-foo":       "fix-foo",
		"12-onboarding": "onboarding",
		"onboarding":    "onboarding",
		"123":           "123",          // no dash, untouched
		"-foo":          "-foo",         // leading dash, no ordinal
		"v1-bootstrap":  "v1-bootstrap", // non-numeric leading segment
	}
	for in, want := range cases {
		if got := StripLegacyPrefix(in); got != want {
			t.Errorf("StripLegacyPrefix(%q): got %q want %q", in, got, want)
		}
	}
}

func TestDirResolvesLegacy(t *testing.T) {
	// Only the bare-slug dir exists: a legacy id resolves to it, the bare id
	// resolves to it, and a non-existent id returns the literal unchanged.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "phases", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	wantFoo := filepath.Join(root, "phases", "foo")
	if got := Dir(root, "03-foo"); got != wantFoo {
		t.Errorf("Dir legacy→slug: got %q want %q", got, wantFoo)
	}
	if got := Dir(root, "foo"); got != wantFoo {
		t.Errorf("Dir slug: got %q want %q", got, wantFoo)
	}
	if got, want := Dir(root, "nope"), filepath.Join(root, "phases", "nope"); got != want {
		t.Errorf("Dir non-existent should be literal: got %q want %q", got, want)
	}

	// When the legacy dir itself still exists (un-migrated), Dir returns it
	// verbatim rather than stripping the prefix.
	root2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root2, "phases", "03-foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := Dir(root2, "03-foo"), filepath.Join(root2, "phases", "03-foo"); got != want {
		t.Errorf("Dir literal-exists: got %q want %q", got, want)
	}
}

func TestOrdered(t *testing.T) {
	got := Ordered([]string{"gamma", "alpha"}, []string{"alpha", "gamma", "orphan"})
	want := []string{"gamma", "alpha", "orphan"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Ordered: got %v want %v", got, want)
	}
	// A stale array entry (no dir on disk) is skipped, not emitted.
	got = Ordered([]string{"missing", "alpha"}, []string{"alpha"})
	if !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Errorf("Ordered stale: got %v want [alpha]", got)
	}
}

func TestDisplayNumber(t *testing.T) {
	order := []string{"alpha", "beta", "gamma"}
	if got := DisplayNumber(order, "beta"); got != 2 {
		t.Errorf("DisplayNumber beta: got %d want 2", got)
	}
	// Reordering changes the number.
	if got := DisplayNumber([]string{"gamma", "beta", "alpha"}, "alpha"); got != 3 {
		t.Errorf("DisplayNumber after reorder: got %d want 3", got)
	}
	if got := DisplayNumber(order, "missing"); got != 0 {
		t.Errorf("DisplayNumber missing: got %d want 0", got)
	}
}

func TestUniqueSlug(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"foo", "foo-2"} {
		if err := os.MkdirAll(filepath.Join(root, "phases", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := UniqueSlug(root, "Foo"); got != "foo-3" {
		t.Errorf("UniqueSlug with foo,foo-2 present: got %q want foo-3", got)
	}
	if got := UniqueSlug(t.TempDir(), "Foo"); got != "foo" {
		t.Errorf("UniqueSlug with none present: got %q want foo", got)
	}
}

func TestNextRunnableEmptyPlan(t *testing.T) {
	p := &Plan{}
	if p.NextRunnable() != nil {
		t.Error("empty plan should have nothing runnable")
	}
}

func TestNextRunnableLowestWaveFirst(t *testing.T) {
	p := &Plan{Task: []Task{
		{ID: "t-1", Wave: 2},
		{ID: "t-2", Wave: 1},
		{ID: "t-3", Wave: 1},
	}}
	got := p.NextRunnable()
	if got == nil || got.ID != "t-2" {
		t.Errorf("expected t-2 (wave 1, alphabetic first); got %v", got)
	}
}

func TestNextRunnableSkipsDoneAndInProgress(t *testing.T) {
	p := &Plan{Task: []Task{
		{ID: "t-1", Wave: 1, Status: StatusDone},
		{ID: "t-2", Wave: 1, Status: StatusInProgress},
		{ID: "t-3", Wave: 1, Status: StatusPending},
	}}
	got := p.NextRunnable()
	if got == nil || got.ID != "t-3" {
		t.Errorf("expected t-3, got %v", got)
	}
}

func TestNextRunnableRespectsDeps(t *testing.T) {
	p := &Plan{Task: []Task{
		{ID: "t-1", Wave: 1, Status: StatusPending},
		{ID: "t-2", Wave: 2, Status: StatusPending, DependsOn: []string{"t-1"}},
	}}
	if got := p.NextRunnable(); got == nil || got.ID != "t-1" {
		t.Errorf("expected t-1 (only one with no blocking deps); got %v", got)
	}

	p.SetTaskStatus("t-1", StatusDone)
	if got := p.NextRunnable(); got == nil || got.ID != "t-2" {
		t.Errorf("expected t-2 after t-1 done; got %v", got)
	}
}

func TestNextRunnableNothingWhenAllDone(t *testing.T) {
	p := &Plan{Task: []Task{
		{ID: "t-1", Wave: 1, Status: StatusDone},
		{ID: "t-2", Wave: 1, Status: StatusDone},
	}}
	if got := p.NextRunnable(); got != nil {
		t.Errorf("expected nil; got %v", got)
	}
}

func TestNextRunnableNothingWhenAllBlocked(t *testing.T) {
	p := &Plan{Task: []Task{
		{ID: "t-1", Wave: 2, Status: StatusFailed},
		{ID: "t-2", Wave: 3, Status: StatusPending, DependsOn: []string{"t-1"}},
	}}
	// t-1 failed (not done) — t-2 is blocked. nothing else pending.
	if got := p.NextRunnable(); got != nil {
		t.Errorf("expected nil when all pending tasks are blocked; got %v", got)
	}
}

func TestSetTaskStatusReturnsFalseForMissing(t *testing.T) {
	p := &Plan{Task: []Task{{ID: "t-1"}}}
	if !p.SetTaskStatus("t-1", StatusDone) {
		t.Error("expected true for existing task")
	}
	if p.Task[0].Status != StatusDone {
		t.Errorf("status not set: %q", p.Task[0].Status)
	}
	if p.SetTaskStatus("nope", StatusDone) {
		t.Error("expected false for missing task")
	}
}

func TestFindTask(t *testing.T) {
	p := &Plan{Task: []Task{{ID: "t-1", Title: "x"}, {ID: "t-2", Title: "y"}}}
	if got := p.FindTask("t-2"); got == nil || got.Title != "y" {
		t.Errorf("FindTask: %v", got)
	}
	if got := p.FindTask("nope"); got != nil {
		t.Errorf("FindTask missing: %v", got)
	}
}

func TestSummaryCounts(t *testing.T) {
	p := &Plan{Task: []Task{
		{ID: "t-1", Status: StatusDone},
		{ID: "t-2", Status: StatusDone},
		{ID: "t-3", Status: StatusInProgress},
		{ID: "t-4", Status: StatusFailed},
		{ID: "t-5", Status: StatusPending},
		{ID: "t-6"}, // empty status counts as pending
	}}
	pending, inProgress, done, failed := p.Summary()
	if done != 2 || inProgress != 1 || failed != 1 || pending != 2 {
		t.Errorf("got d=%d ip=%d f=%d p=%d", done, inProgress, failed, pending)
	}
}
