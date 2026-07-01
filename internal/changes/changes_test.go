package changes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"), "01-foo")
	if err != nil {
		t.Fatalf("missing file should be ok: %v", err)
	}
	if c.Phase != "01-foo" {
		t.Errorf("phase id should default from arg, got %q", c.Phase)
	}
	if c.Tasks == nil {
		t.Error("Tasks should be initialised even for missing file")
	}
}

func TestRecordAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changes.json")

	c := New("01-meal-tagging")
	c.Record("t-1", []string{"db/schema.ts", "db/migrations/0042.sql"}, "abc1234", "", nil)
	c.Record("t-2", []string{"src/api/tags.ts"}, "def5678", "tagged with helper notes", nil)

	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path, "01-meal-tagging")
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != "01-meal-tagging" {
		t.Errorf("phase: got %q", got.Phase)
	}
	if len(got.Tasks) != 2 {
		t.Fatalf("tasks: %d", len(got.Tasks))
	}
	if got.Tasks["t-1"].Commit != "abc1234" {
		t.Errorf("t-1 commit: %q", got.Tasks["t-1"].Commit)
	}
	if len(got.Tasks["t-1"].Files) != 2 {
		t.Errorf("t-1 files: %v", got.Tasks["t-1"].Files)
	}
	if got.Tasks["t-2"].Notes != "tagged with helper notes" {
		t.Errorf("t-2 notes: %q", got.Tasks["t-2"].Notes)
	}
	if got.Tasks["t-1"].CompletedAt.IsZero() {
		t.Error("CompletedAt should be set")
	}
}

// TestPRFieldRoundTrips pins the phase-scoped PR number through
// marshal/unmarshal — `dross phase complete` looks it up to gate on the
// provider's merge status, so dropping or renaming the field would silently
// disable the authoritative gate.
func TestPRFieldRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changes.json")

	c := New("phase-x")
	c.PR = 99
	c.Record("t-1", []string{"a.go"}, "abc1234", "", nil)
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path, "phase-x")
	if err != nil {
		t.Fatal(err)
	}
	if got.PR != 99 {
		t.Errorf("PR lost through round-trip: got %d want 99", got.PR)
	}
}

// TestPRZeroOmitted proves the omitempty tag keeps a zero PR out of the JSON,
// so a phase that never shipped carries no misleading `"pr":0`.
func TestPRZeroOmitted(t *testing.T) {
	b, err := json.Marshal(New("p"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"pr"`) {
		t.Errorf("PR:0 should be omitted from JSON, got %s", b)
	}
}

// TestSetPRPersists proves the load/set/save helper records the PR into the
// phase's changes.json at the canonical FilePath, creating it when absent.
func TestSetPRPersists(t *testing.T) {
	root := t.TempDir()
	if err := SetPR(root, "phase-x", 42); err != nil {
		t.Fatalf("SetPR: %v", err)
	}
	got, err := Load(FilePath(root, "phase-x"), "phase-x")
	if err != nil {
		t.Fatal(err)
	}
	if got.PR != 42 {
		t.Errorf("SetPR did not persist the PR number: got %d want 42", got.PR)
	}
}

func TestRecordOverwritesOnRerun(t *testing.T) {
	c := New("p")
	c.Record("t-1", []string{"a.ts"}, "old", "", nil)
	first := c.Tasks["t-1"].CompletedAt
	time.Sleep(2 * time.Millisecond)
	c.Record("t-1", []string{"a.ts", "b.ts"}, "new", "", nil)
	if c.Tasks["t-1"].Commit != "new" {
		t.Errorf("expected overwrite, got commit %q", c.Tasks["t-1"].Commit)
	}
	if !c.Tasks["t-1"].CompletedAt.After(first) {
		t.Error("CompletedAt should advance on rerun")
	}
}

func TestSaveCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "a", "b", "c", "changes.json")
	c := New("p")
	c.Record("t-1", []string{"x"}, "", "", nil)
	if err := c.Save(deep); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(deep); err != nil {
		t.Errorf("file not at deep path: %v", err)
	}
}

func TestFilePath(t *testing.T) {
	got := FilePath(".dross", "01-foo")
	want := filepath.Join(".dross", "phases", "01-foo", "changes.json")
	if got != want {
		t.Errorf("FilePath: got %q, want %q", got, want)
	}
}

func TestLoadMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changes.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, "p"); err == nil {
		t.Error("expected unmarshal error for malformed JSON, got nil")
	}
}

func TestLoadReadErrorWhenPathIsDir(t *testing.T) {
	// ReadFile on a directory returns an error that is not fs.ErrNotExist,
	// exercising the generic-error branch in Load.
	dir := t.TempDir()
	if _, err := Load(dir, "p"); err == nil {
		t.Error("expected read error when path is a directory, got nil")
	}
}

func TestLoadAppliesDefaultsForLegacyShape(t *testing.T) {
	// JSON with no "phase" or "tasks" keys should still produce a usable Changes.
	dir := t.TempDir()
	path := filepath.Join(dir, "changes.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, "01-foo")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Phase != "01-foo" {
		t.Errorf("phase default not applied: %q", c.Phase)
	}
	if c.Tasks == nil {
		t.Error("Tasks nil after load — default not applied")
	}
}

func TestSaveFailsWhenParentIsAFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Save target sits "inside" a regular file → MkdirAll must fail.
	target := filepath.Join(blocker, "sub", "changes.json")
	c := New("p")
	if err := c.Save(target); err == nil {
		t.Error("expected MkdirAll to fail when parent path is a file")
	}
}

func TestRecordInitialisesNilTaskMap(t *testing.T) {
	c := &Changes{Phase: "p"} // no New(), Tasks is nil
	c.Record("t-1", []string{"a"}, "abc", "note", nil)
	if len(c.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(c.Tasks))
	}
	if c.Tasks["t-1"].Notes != "note" {
		t.Errorf("notes round-trip: %q", c.Tasks["t-1"].Notes)
	}
}

func TestParseLandmark(t *testing.T) {
	// Contract: value splits on the FIRST '=' only, so '=' and '·' survive.
	lm, err := ParseLandmark("what=a=b · c")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if lm.What != "a=b · c" {
		t.Errorf("value lost its = or ·: got %q, want %q", lm.What, "a=b · c")
	}
	if lm.Feature != "" || lm.Symbol != "" || lm.Loc != "" {
		t.Errorf("only What should be set, got %+v", lm)
	}

	// All four fields in one --landmark value, comma-separated.
	full, err := ParseLandmark("feature=Phase lifecycle, symbol=Insert, loc=internal/cmd/insert.go:42, what=inserts a phase")
	if err != nil {
		t.Fatalf("parse full: %v", err)
	}
	if full.Feature != "Phase lifecycle" || full.Symbol != "Insert" ||
		full.Loc != "internal/cmd/insert.go:42" || full.What != "inserts a phase" {
		t.Errorf("full landmark fields wrong: %+v", full)
	}

	// A pair with no '=' is an error — never a silent empty-key entry.
	if _, err := ParseLandmark("feature"); err == nil {
		t.Error("expected error for a pair with no '=', got nil")
	}
	// Unknown key is rejected.
	if _, err := ParseLandmark("color=blue"); err == nil {
		t.Error("expected error for unknown landmark key, got nil")
	}
	// An empty value parses nothing → error.
	if _, err := ParseLandmark("   "); err == nil {
		t.Error("expected error for empty landmark, got nil")
	}
}

func TestLandmarkRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changes.json")

	c := New("01-arch")
	c.Record("t-1", []string{"a.go"}, "abc1234", "",
		[]Landmark{
			{Feature: "architecture doc", Symbol: "ParseDoc", Loc: "internal/architecture/links.go:10", What: "parses entries"},
			{What: "a=b · c"},
		})
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path, "01-arch")
	if err != nil {
		t.Fatal(err)
	}
	lms := got.Tasks["t-1"].Landmarks
	if len(lms) != 2 {
		t.Fatalf("expected 2 landmarks after round-trip, got %d", len(lms))
	}
	if lms[0].Symbol != "ParseDoc" || lms[0].Loc != "internal/architecture/links.go:10" {
		t.Errorf("landmark[0] fields lost: %+v", lms[0])
	}
	if lms[1].What != "a=b · c" {
		t.Errorf("landmark[1] value lost its = or · through JSON: %q", lms[1].What)
	}
}
