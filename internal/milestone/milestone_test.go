package milestone

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A milestone toml with no acks must load a nil Closeout and re-save without
// any closeout_ack key — every milestone already on disk stays byte-stable.
func TestNoAcksStaysAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.1.toml")
	body := "[milestone]\n  version = \"v1.1\"\n\nphases = [\"one\"]\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Closeout != nil {
		t.Errorf("Closeout = %#v, want nil for a toml with no acks", m.Closeout)
	}
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "closeout_ack") {
		t.Errorf("re-saved toml grew a closeout_ack key:\n%s", out)
	}
}

func TestAckRefusesBlankReason(t *testing.T) {
	m := &Milestone{}
	t1 := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	if err := m.Ack("phase:p", "kept", t1); err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"", "   ", "\t\n"} {
		if err := m.Ack("phase:p", reason, t1); err == nil {
			t.Errorf("Ack accepted reason %q", reason)
		}
		if err := m.Ack("phase:q", reason, t1); err == nil {
			t.Errorf("Ack accepted reason %q for a new item", reason)
		}
	}
	if len(m.Closeout) != 1 || m.Closeout[0].Reason != "kept" {
		t.Errorf("a refused ack changed the records: %+v", m.Closeout)
	}
}

func TestAckUpsertsByItem(t *testing.T) {
	m := &Milestone{}
	t1 := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	item := "deferred:p1#2"
	if err := m.Ack(item, "r1", t1); err != nil {
		t.Fatal(err)
	}
	if err := m.Ack("phase:other", "unrelated", t1); err != nil {
		t.Fatal(err)
	}
	if err := m.Ack(item, "r2", t2); err != nil {
		t.Fatal(err)
	}
	var hits []CloseoutAck
	for _, a := range m.Closeout {
		if a.Item == item {
			hits = append(hits, a)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("re-ack left %d records for %s, want 1: %+v", len(hits), item, m.Closeout)
	}
	if hits[0].Reason != "r2" || !hits[0].At.Equal(t2) {
		t.Errorf("re-ack record = %+v, want reason r2 at %v", hits[0], t2)
	}
	if len(m.Closeout) != 2 {
		t.Errorf("records = %d, want 2 (re-ack must not append): %+v", len(m.Closeout), m.Closeout)
	}
}

func TestParseAckItem(t *testing.T) {
	accepted := []struct {
		id   string
		want AckItem
	}{
		{"phase:built", AckItem{Kind: AckPhase, Payload: "built"}},
		{"deferred:_project#0", AckItem{Kind: AckDeferred, Payload: "_project#0", Source: "_project", Index: 0}},
		{"deferred:src#12", AckItem{Kind: AckDeferred, Payload: "src#12", Source: "src", Index: 12}},
		{"survivor:abc123", AckItem{Kind: AckSurvivor, Payload: "abc123"}},
		// A keyless survivor id keeps its inner colons: the kind splits on
		// the first colon only.
		{"survivor:f.go:12:op", AckItem{Kind: AckSurvivor, Payload: "f.go:12:op"}},
	}
	for _, c := range accepted {
		got, err := ParseAckItem(c.id)
		if err != nil {
			t.Errorf("ParseAckItem(%q) refused: %v", c.id, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseAckItem(%q) = %+v, want %+v", c.id, got, c.want)
		}
		if f := FormatAckItem(got); f != c.id {
			t.Errorf("FormatAckItem(ParseAckItem(%q)) = %q, want the id back", c.id, f)
		}
		again, err := ParseAckItem(FormatAckItem(got))
		if err != nil || again != got {
			t.Errorf("Parse(Format(%+v)) = %+v, %v; want a round-trip", got, again, err)
		}
	}

	for _, id := range []string{
		"", "bogus", "phase:", "deferred:src", "deferred:src#x", "deferred:x#-1",
		"deferred:#1", "deferred:src#", "deferred:src#+1", "deferred:src#01",
		"survivor:", "board:DRO-1", ":phase",
	} {
		_, err := ParseAckItem(id)
		if err == nil {
			t.Errorf("ParseAckItem(%q) accepted a malformed id", id)
			continue
		}
		if !strings.Contains(err.Error(), "phase:<slug>") || !strings.Contains(err.Error(), "deferred:<source>#<index>") || !strings.Contains(err.Error(), "survivor:<key>") {
			t.Errorf("ParseAckItem(%q) error %q does not list the three forms", id, err)
		}
	}
}

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
		Closeout: []CloseoutAck{
			{Item: "phase:03-tagging", Reason: "moved to v2.0", At: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)},
			{Item: "survivor:f.go:12:op", Reason: "equivalent mutant", At: time.Date(2026, 11, 15, 11, 30, 0, 0, time.UTC)},
		},
	}
	original.Milestone.Base = "milestone/v1.1"
	if err := original.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Times are compared with Equal below; the decoder's zone pointer need
	// not be the same *time.Location, so the rest is DeepEqual'd without them.
	if len(loaded.Closeout) != len(original.Closeout) {
		t.Fatalf("closeout acks: loaded %d, want %d: %+v", len(loaded.Closeout), len(original.Closeout), loaded.Closeout)
	}
	for i, want := range original.Closeout {
		got := loaded.Closeout[i]
		if got.Item != want.Item || got.Reason != want.Reason || !got.At.Equal(want.At) {
			t.Errorf("closeout_ack[%d] = %+v, want %+v", i, got, want)
		}
	}
	origRest, loadRest := *original, *loaded
	origRest.Closeout, loadRest.Closeout = nil, nil
	if !reflect.DeepEqual(origRest, loadRest) {
		t.Errorf("round-trip drift:\norig: %+v\nload: %+v", origRest, loadRest)
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

// TestMilestoneVersionIsContained: a version read back from state.json or a
// spec is a path segment; one that would escape milestones/ resolves to the
// refused segment, never outside.
func TestMilestoneVersionIsContained(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".dross")
	milestones := filepath.Join(root, "milestones")
	if got := FilePath(root, "v1.0"); got != filepath.Join(milestones, "v1.0.toml") {
		t.Errorf("FilePath(v1.0) = %s", got)
	}
	for _, v := range []string{"../../x", "../state", "/etc/passwd"} {
		if got := FilePath(root, v); got != filepath.Join(milestones, "_refused.toml") {
			t.Errorf("FilePath(%q) = %s, want the refused segment inside milestones/", v, got)
		}
		if _, err := ContainVersion(root, v); err == nil {
			t.Errorf("ContainVersion(%q) accepted an escaping version", v)
		}
	}
}
