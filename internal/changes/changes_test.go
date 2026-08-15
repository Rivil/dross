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

// TestBaseFieldRoundTrips pins the forked-from base through marshal/unmarshal.
// `dross phase complete` reconciles against this value instead of re-deriving a
// base from current_milestone, so dropping the field or its json tag would send
// completion back to the inference that fast-forwards a stale milestone branch.
func TestBaseFieldRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changes.json")

	c := New("phase-x")
	c.PR = 42
	c.Base = "main"
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path, "phase-x")
	if err != nil {
		t.Fatal(err)
	}
	if got.Base != "main" {
		t.Errorf("Base lost through round-trip: got %q want %q", got.Base, "main")
	}
	if got.PR != 42 {
		t.Errorf("PR lost alongside Base: got %d want 42", got.PR)
	}
}

// TestSetBaseCoexistsWithSetPR proves the two load-set-save helpers compose
// rather than truncate: ship writes both into one record, so a SetBase that
// started from a fresh Changes would silently drop the PR the merge gate reads.
func TestSetBaseCoexistsWithSetPR(t *testing.T) {
	root := t.TempDir()
	if err := SetPR(root, "phase-x", 42); err != nil {
		t.Fatalf("SetPR: %v", err)
	}
	if err := SetBase(root, "phase-x", "main"); err != nil {
		t.Fatalf("SetBase: %v", err)
	}
	got, err := Load(FilePath(root, "phase-x"), "phase-x")
	if err != nil {
		t.Fatal(err)
	}
	if got.PR != 42 {
		t.Errorf("SetBase truncated the record: PR is %d want 42", got.PR)
	}
	if got.Base != "main" {
		t.Errorf("SetBase did not persist: got %q want %q", got.Base, "main")
	}
}

// TestSetBaseCreatesMissingFile proves the base can be recorded at fork time,
// before execute has written any task record — that's the only moment the
// branch actually forked from is known.
func TestSetBaseCreatesMissingFile(t *testing.T) {
	root := t.TempDir()
	if err := SetBase(root, "phase-x", "milestone/v1.2"); err != nil {
		t.Fatalf("SetBase: %v", err)
	}
	got, err := Load(FilePath(root, "phase-x"), "phase-x")
	if err != nil {
		t.Fatal(err)
	}
	if got.Base != "milestone/v1.2" {
		t.Errorf("SetBase on a phase with no changes.json: got %q want %q", got.Base, "milestone/v1.2")
	}
}

// TestBaseEmptyOmitted proves omitempty keeps a legacy record (written before
// the base field existed) byte-stable through a load/save cycle — a phase with
// no recorded base must stay distinguishable from one recorded as "", since
// completion refuses on the former.
func TestBaseEmptyOmitted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changes.json")
	if err := os.WriteFile(path, []byte(`{"phase":"p","pr":7,"tasks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, "p")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Base != "" {
		t.Errorf("legacy record gained a base: %q", c.Base)
	}
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"base"`) {
		t.Errorf("empty Base should be omitted from JSON, got %s", b)
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

func TestParseLandmarkCommaInValue(t *testing.T) {
	// Contract: a comma joins the value unless it opens a recognised key= pair.
	lm, err := ParseLandmark("what=a, b")
	if err != nil {
		t.Fatalf("comma-in-value should parse: %v", err)
	}
	if lm.What != "a, b" {
		t.Errorf("comma-joined value: got %q, want %q", lm.What, "a, b")
	}

	// An unrecognised key= after a comma joins too — only the four landmark
	// keys open a new pair. This is the boundary rule's discriminator.
	lm, err = ParseLandmark("what=a, y=2")
	if err != nil {
		t.Fatalf("unrecognised key= segment should join, not error: %v", err)
	}
	if lm.What != "a, y=2" {
		t.Errorf("unrecognised key= join: got %q, want %q", lm.What, "a, y=2")
	}

	// Mixed: a real pair boundary after a comma-bearing value.
	lm, err = ParseLandmark("feature=X, what=a, b")
	if err != nil {
		t.Fatalf("mixed landmark should parse: %v", err)
	}
	if lm.Feature != "X" || lm.What != "a, b" {
		t.Errorf("mixed landmark fields: %+v", lm)
	}

	// Interior text is preserved as typed; only the value's ends are trimmed.
	lm, err = ParseLandmark("what=  a,  b  ")
	if err != nil {
		t.Fatalf("padded value should parse: %v", err)
	}
	if lm.What != "a,  b" {
		t.Errorf("interior whitespace must survive: got %q, want %q", lm.What, "a,  b")
	}
}

func TestParseLandmarkDuplicateKey(t *testing.T) {
	// Contract: a duplicate key is a loud parse error, not last-writer-wins.
	if _, err := ParseLandmark("feature=x, feature=y"); err == nil {
		t.Error("expected error for duplicate landmark key, got nil")
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

// TestStatusAbsentFromOldRecordReadsEmpty: every changes.json written before
// this field existed has to keep loading. An empty Status means "unknown", and
// callers treat that as not-done rather than erroring on it.
func TestStatusAbsentFromOldRecordReadsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changes.json")
	if err := os.WriteFile(path, []byte(`{"phase":"old-phase","pr":12,"tasks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path, "old-phase")
	if err != nil {
		t.Fatalf("a record without status must still load: %v", err)
	}
	if got.Status != "" {
		t.Errorf("status = %q, want empty for a pre-field record", got.Status)
	}
	if got.PR != 12 {
		t.Errorf("the rest of the record was lost: %+v", got)
	}
}

// TestSetStatusIsMonotonic: shipping again after completion must not walk the
// marker backwards. Everything that counts finished phases reads this field, so
// a downgrade would make a done phase look outstanding.
func TestSetStatusIsMonotonic(t *testing.T) {
	root := t.TempDir()

	if err := SetStatus(root, "p", StatusShipped); err != nil {
		t.Fatal(err)
	}
	if err := SetStatus(root, "p", StatusComplete); err != nil {
		t.Fatal(err)
	}
	// A second ship over a completed phase.
	if err := SetStatus(root, "p", StatusShipped); err != nil {
		t.Fatal(err)
	}

	got, err := Load(FilePath(root, "p"), "p")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusComplete {
		t.Errorf("status = %q, want %q — a re-ship demoted a completed phase", got.Status, StatusComplete)
	}
}

// TestStatusSurvivesTaskRecord: the task write path is load-record-save, and it
// must carry the phase-level fields through. A Record() that dropped Status
// would erase the marker on the next task of a re-run.
func TestStatusSurvivesTaskRecord(t *testing.T) {
	root := t.TempDir()
	path := FilePath(root, "p")
	if err := SetStatus(root, "p", StatusComplete); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path, "p")
	if err != nil {
		t.Fatal(err)
	}
	c.Record("t-1", []string{"a.go"}, "abc1234", "", nil)
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path, "p")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusComplete {
		t.Errorf("status = %q after recording a task, want %q", got.Status, StatusComplete)
	}
	if _, ok := got.Tasks["t-1"]; !ok {
		t.Error("the task record itself was lost")
	}
}

// TestSetForkRecordsBothFactsTogether covers the helper the red-proof pin
// depends on. SetFork exists because branch and fork-commit are only both known
// at one moment — `dross phase create` — and a pin recorded later would name
// the base branch's tip rather than the phase's fork point.
func TestSetForkRecordsBothFactsTogether(t *testing.T) {
	root := t.TempDir()
	if err := SetFork(root, "phase-x", "milestone/v1.3", "abc123def456"); err != nil {
		t.Fatalf("SetFork: %v", err)
	}
	got, err := Load(FilePath(root, "phase-x"), "phase-x")
	if err != nil {
		t.Fatal(err)
	}
	if got.Base != "milestone/v1.3" {
		t.Errorf("Base = %q, want milestone/v1.3", got.Base)
	}
	if got.BaseCommit != "abc123def456" {
		t.Errorf("BaseCommit = %q, want abc123def456", got.BaseCommit)
	}
}

// TestSetForkPreservesTheRestOfTheRecord is the load-set-save half: SetFork
// starting from a fresh Changes would drop the PR number the merge gate reads,
// exactly as TestSetBaseCoexistsWithSetPR pins for SetBase.
func TestSetForkPreservesTheRestOfTheRecord(t *testing.T) {
	root := t.TempDir()
	if err := SetPR(root, "phase-x", 42); err != nil {
		t.Fatalf("SetPR: %v", err)
	}
	if err := SetFork(root, "phase-x", "main", "deadbeef"); err != nil {
		t.Fatalf("SetFork: %v", err)
	}
	got, err := Load(FilePath(root, "phase-x"), "phase-x")
	if err != nil {
		t.Fatal(err)
	}
	if got.PR != 42 {
		t.Errorf("SetFork truncated the record: PR is %d want 42", got.PR)
	}
	if got.BaseCommit != "deadbeef" {
		t.Errorf("BaseCommit did not persist: %q", got.BaseCommit)
	}
}

// TestSetForkSurfacesAnUnreadableRecord pins the load error path.
//
// SetFork must not silently start from an empty record when the existing one
// cannot be parsed: doing so would overwrite a real fork point with a fresh
// one derived from nothing, which is precisely the rot the field exists to
// survive. The failure has to reach the caller.
func TestSetForkSurfacesAnUnreadableRecord(t *testing.T) {
	root := t.TempDir()
	path := FilePath(root, "phase-x")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetFork(root, "phase-x", "main", "abc123"); err == nil {
		t.Fatal("SetFork overwrote a record it could not read")
	}
}

// TestSetForkAndSetBaseDoNotClobberEachOther.
//
// Ship rewrites the base branch alone, long after the fork. If SetBase also
// cleared BaseCommit — or SetFork were used at ship time — the recorded fork
// point would become the base branch's tip today, and every red proof pinned
// against it would be pinning the wrong commit while still looking recorded.
func TestSetForkAndSetBaseDoNotClobberEachOther(t *testing.T) {
	root := t.TempDir()
	if err := SetFork(root, "phase-x", "milestone/v1.3", "forkpoint1"); err != nil {
		t.Fatalf("SetFork: %v", err)
	}
	if err := SetBase(root, "phase-x", "main"); err != nil {
		t.Fatalf("SetBase: %v", err)
	}
	got, err := Load(FilePath(root, "phase-x"), "phase-x")
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseCommit != "forkpoint1" {
		t.Errorf("a ship-time SetBase destroyed the fork point: BaseCommit = %q", got.BaseCommit)
	}
	if got.Base != "main" {
		t.Errorf("SetBase did not record the PR's base branch: %q", got.Base)
	}
}
