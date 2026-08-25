package board

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	b, err := Load(filepath.Join(t.TempDir(), "board.json"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if b.Milestones == nil || b.Phases == nil || b.Quicks == nil {
		t.Error("maps should be initialised on a fresh board")
	}
	if len(b.Phases) != 0 {
		t.Error("fresh board should have no links")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.json")
	b := New()
	b.SetMilestone("v0.2", "7")
	b.SetPhase("02-auth", "12")
	b.SetQuick("0.2.3.5", "18")
	b.Dismiss("21")
	b.MarkPulled()
	if err := b.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id, ok := got.MilestoneID("v0.2"); !ok || id != "7" {
		t.Errorf("milestone v0.2 = %q,%v", id, ok)
	}
	if n, ok := got.PhaseIssue("02-auth"); !ok || n != "12" {
		t.Errorf("phase 02-auth = %q,%v", n, ok)
	}
	if n, ok := got.QuickIssue("0.2.3.5"); !ok || n != "18" {
		t.Errorf("quick = %q,%v", n, ok)
	}
	if !got.IsDismissed("21") {
		t.Error("dismissed 21 not persisted")
	}
	if got.LastPull.IsZero() {
		t.Error("last_pull not persisted")
	}
}

// TestBoardReadableIDRoundTrip proves c-1/c-4: links are keyed by readable
// string issue ids (e.g. YouTrack "PROJ-123") across Phases, Quicks AND
// Milestones, with Dismissed a string set — all surviving Save→Load.
func TestBoardReadableIDRoundTrip(t *testing.T) {
	b := New()
	b.SetPhase("02-auth", "PROJ-123")
	b.SetQuick("0.2.3.5", "PROJ-200")
	b.SetMilestone("v0.2", "PROJ-9")
	b.Dismiss("PROJ-300")

	check := func(b *Board, when string) {
		if v, ok := b.PhaseIssue("02-auth"); !ok || v != "PROJ-123" {
			t.Errorf("%s: phase = %q,%v want PROJ-123", when, v, ok)
		}
		if v, ok := b.QuickIssue("0.2.3.5"); !ok || v != "PROJ-200" {
			t.Errorf("%s: quick = %q,%v want PROJ-200", when, v, ok)
		}
		if v, ok := b.MilestoneID("v0.2"); !ok || v != "PROJ-9" {
			t.Errorf("%s: milestone = %q,%v want PROJ-9", when, v, ok)
		}
		if !b.IsLinked("PROJ-123") || !b.IsLinked("PROJ-200") {
			t.Errorf("%s: phase/quick readable ids should be linked", when)
		}
		// A YouTrack epic id IS an issue id, so it is linked — the epic is a
		// dross-authored mirror and belongs out of the inbound feed. See
		// TestIsLinkedGatesMilestonesByIssueShape for the mode split.
		if !b.IsLinked("PROJ-9") {
			t.Errorf("%s: an issue-shaped milestone id should be linked", when)
		}
		if !b.IsDismissed("PROJ-300") {
			t.Errorf("%s: dismissed readable id not tracked", when)
		}
	}

	check(b, "in-memory")

	path := filepath.Join(t.TempDir(), File)
	if err := b.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	check(loaded, "round-tripped")
}

func TestMissingLookups(t *testing.T) {
	b := New()
	if _, ok := b.MilestoneID("nope"); ok {
		t.Error("unset milestone reported as linked")
	}
	if _, ok := b.PhaseIssue("nope"); ok {
		t.Error("unset phase reported as linked")
	}
	if _, ok := b.QuickIssue("nope"); ok {
		t.Error("unset quick reported as linked")
	}
}

func TestDismissIdempotent(t *testing.T) {
	b := New()
	b.Dismiss("5")
	b.Dismiss("5")
	if len(b.Dismissed) != 1 {
		t.Errorf("Dismissed = %v, want one entry", b.Dismissed)
	}
	if !b.IsDismissed("5") || b.IsDismissed("6") {
		t.Error("IsDismissed wrong")
	}
}

// TestIsLinkedCoversEveryNamespace pins the exclusion_basis lock: every place
// board.json records a dross-authored mirror is a place IsLinked matches, so
// none of them reach the inbound triage feed. It replaces the phases+quicks
// pair the filter used to walk.
func TestIsLinkedCoversEveryNamespace(t *testing.T) {
	for _, tc := range []struct {
		namespace string
		seed      func(*Board)
	}{
		{"phases", func(b *Board) { b.SetPhase("02-auth", "PROJ-9") }},
		{"quicks", func(b *Board) { b.SetQuick("1.2.3.4", "PROJ-9") }},
		{"backlog", func(b *Board) { b.SetBacklog("slug:future-x", "PROJ-9") }},
		{"tasks", func(b *Board) { b.SetTask("02-auth", "t-1", "PROJ-9") }},
		{"milestones", func(b *Board) { b.SetMilestone("v0.2", "PROJ-9") }},
	} {
		t.Run(tc.namespace, func(t *testing.T) {
			b := New()
			tc.seed(b)
			if !b.IsLinked("PROJ-9") {
				t.Errorf("an issue linked only in %s must count as linked", tc.namespace)
			}
			if b.IsLinked("PROJ-99") {
				t.Error("unrelated issue reported as linked")
			}
		})
	}
}

// TestIsLinkedMatchesTasksByIssue proves the tasks walk reads .Issue rather
// than the record: a TaskLink also carries the agreement point, and neither
// half of that pair is an issue id.
func TestIsLinkedMatchesTasksByIssue(t *testing.T) {
	b := New()
	b.SetTaskSynced("02-auth", "t-1", "PROJ-9", "done", "Verified")
	if !b.IsLinked("PROJ-9") {
		t.Error("task issue id should be linked")
	}
	if b.IsLinked("done") || b.IsLinked("Verified") {
		t.Error("the agreement point is not an issue id and must never match")
	}
}

// TestIsLinkedGatesMilestonesByIssueShape covers the one namespace whose value
// is not always an issue id. The three cases fail differently:
//
//   - "DRO-134" is a YouTrack epic idReadable — a real dross-authored issue,
//     which must be excluded from the feed.
//   - "7" is a REST-forge/GitHub numeric milestone id, sharing an id space with
//     those backends' own issue keys; matching it would suppress human issue #7.
//   - "v1.5" is a YouTrack version bundle name, colliding with nothing — it is
//     here to prove the predicate is a shape, not "contains a dash".
func TestIsLinkedGatesMilestonesByIssueShape(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{"youtrack epic idReadable", "DRO-134", true},
		{"forge numeric milestone id", "7", false},
		{"youtrack version bundle name", "v1.5", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := New()
			b.SetMilestone("v1.5", tc.value)
			if got := b.IsLinked(tc.value); got != tc.want {
				t.Errorf("IsLinked(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestIsLinkedNeverMatchesAnEmptyId guards the degenerate meeting of two
// empties: a board.json task entry with no issue id, and a tracker issue whose
// Key did not parse. If IsLinked("") returned true the whole inbound feed would
// blank — see TestEmptyIdNeverMatches in internal/cmd for the feed half.
func TestIsLinkedNeverMatchesAnEmptyId(t *testing.T) {
	b := New()
	b.SetTask("02-auth", "t-1", "")
	b.SetPhase("03-x", "")
	b.SetMilestone("v0.2", "")
	if b.IsLinked("") {
		t.Error(`IsLinked("") must be false however many empty ids are recorded`)
	}
}

func TestLoadUnmarshalsNilMapsSafely(t *testing.T) {
	// A board.json written with only some keys (or by an older version)
	// must still come back with usable maps.
	path := filepath.Join(t.TempDir(), "board.json")
	if err := os.WriteFile(path, []byte(`{"phases":{"01-x":"3"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	b.SetMilestone("v1", "1") // would panic on a nil map
	if n, ok := b.PhaseIssue("01-x"); !ok || n != "3" {
		t.Errorf("phase 01-x = %q,%v", n, ok)
	}
}

// TestTaskKeyIsPhaseSlashTask pins the exact key shape. TaskKey exists so no
// caller re-derives it slightly differently and orphans an existing mapping,
// which only holds if the shape itself is asserted somewhere — and the shape is
// what a stored board.json is already written in, so it cannot drift silently.
func TestTaskKeyIsPhaseSlashTask(t *testing.T) {
	if got := TaskKey("01-auth", "t-1"); got != "01-auth/t-1" {
		t.Errorf("TaskKey = %q, want %q", got, "01-auth/t-1")
	}
	// Order matters: a key built the other way round would still look like a
	// pair and still round-trip against itself, and would still be wrong.
	if got := TaskKey("t-1", "01-auth"); got != "t-1/01-auth" {
		t.Errorf("TaskKey = %q, want the phase first", got)
	}
}

// TestTaskLookupsAreScopedToTheirPhase is why the key carries the phase at all:
// task ids are unique only within a phase, so "t-1" of two phases must be two
// mappings, not one that overwrites the other.
func TestTaskLookupsAreScopedToTheirPhase(t *testing.T) {
	b := New()
	b.SetTask("01-auth", "t-1", "PROJ-1")
	b.SetTask("02-api", "t-1", "PROJ-2")

	if n, ok := b.TaskIssue("01-auth", "t-1"); !ok || n != "PROJ-1" {
		t.Errorf("01-auth/t-1 = %q,%v, want PROJ-1", n, ok)
	}
	if n, ok := b.TaskIssue("02-api", "t-1"); !ok || n != "PROJ-2" {
		t.Errorf("02-api/t-1 = %q,%v, want PROJ-2 — the phase is not in the key", n, ok)
	}
	if _, ok := b.TaskIssue("01-auth", "t-2"); ok {
		t.Error("an unset task reported as linked")
	}
	if _, ok := b.Tasks["01-auth/t-1"]; !ok {
		t.Errorf("stored keys = %v, want the phase/task shape on disk", b.Tasks)
	}
}
