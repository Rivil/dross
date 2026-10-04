package project

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

// doorDoc is a non-Project document: a root scalar, one table and an
// array-of-tables — the shapes plan.toml and survivors.toml carry.
type doorDoc struct {
	Seq   int        `toml:"seq"`
	Name  string     `toml:"name"`
	Table doorTable  `toml:"table"`
	Items []doorItem `toml:"item"`
}

type doorTable struct {
	Mode  string `toml:"mode"`
	Count int    `toml:"count"`
}

type doorItem struct {
	ID     string   `toml:"id"`
	Status string   `toml:"status"`
	Tags   []string `toml:"tags,omitempty"`
}

const doorSrc = `# a header comment a whole-file encode would drop
seq = 2
name = "doc"  # trailing on a root scalar

[table]
  mode = "a"
  count = 2

[[item]]
  id = "t-1"
  status = "pending"  # first item
  tags = [
    "x",  # hand-wrapped
    "y",
  ]

[[item]]
  id = "t-2"
  status = "pending"
  tags = ["z"]  # dross:allow-secret
`

func loadDoorDoc(t *testing.T, path string) *doorDoc {
	t.Helper()
	var d doorDoc
	if _, err := toml.DecodeFile(path, &d); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return &d
}

func seedDoor(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.toml")
	if err := os.WriteFile(path, []byte(doorSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// doorChangedLines returns the lines of want and got that differ, position by
// position; both must have the same line count.
func doorChangedLines(t *testing.T, want, got []byte) []string {
	t.Helper()
	wl, gl := strings.Split(string(want), "\n"), strings.Split(string(got), "\n")
	if len(wl) != len(gl) {
		t.Fatalf("line count changed %d → %d:\n%s", len(wl), len(gl), got)
	}
	var out []string
	for i := range wl {
		if wl[i] != gl[i] {
			out = append(out, gl[i])
		}
	}
	return out
}

// TestSaveTOMLChangesOneLine: one scalar changed in the second array-of-tables
// element rewrites exactly that line — comments, the hand-wrapped array and the
// trailing marker all survive.
func TestSaveTOMLChangesOneLine(t *testing.T) {
	path := seedDoor(t)
	d := loadDoorDoc(t, path)
	d.Items[1].Status = "done"
	if err := SaveTOML(path, d); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	changed := doorChangedLines(t, []byte(doorSrc), got)
	if len(changed) != 1 || !strings.Contains(changed[0], `status = "done"`) {
		t.Errorf("changed lines = %q, want exactly t-2's status line", changed)
	}
	// And it is t-2's line, not t-1's: the status line after `id = "t-2"`.
	lines := strings.Split(string(got), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == `id = "t-2"` {
			if i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) != `status = "done"` {
				t.Errorf("the line after t-2's id is %q, want the changed status", lines[i+1])
			}
		}
		if strings.TrimSpace(l) == `id = "t-1"` && !strings.Contains(lines[i+1], `status = "pending"  # first item`) {
			t.Errorf("t-1's status line changed: %q", lines[i+1])
		}
	}
	if !bytes.Contains(got, []byte(`tags = ["z"]  # dross:allow-secret`)) {
		t.Errorf("the trailing marker did not survive:\n%s", got)
	}
}

// TestSaveTOMLRefusesCanonicalMismatch: an op of the right type but the wrong
// value loads cleanly yet not as the saved value. The verify step's canonical
// comparison refuses it and names the differing line.
func TestSaveTOMLRefusesCanonicalMismatch(t *testing.T) {
	path := seedDoor(t)
	d := loadDoorDoc(t, path)
	d.Items[1].Status = "done"

	orig := doorOps
	doorOps = func(old, new any) ([]op, error) {
		ops, err := diff(old, new)
		if err != nil {
			return nil, err
		}
		ops[0].value = "wrong"
		return ops, nil
	}
	defer func() { doorOps = orig }()

	err := SaveTOML(path, d)
	if err == nil || !strings.Contains(err.Error(), "would not load as saved at") || !strings.Contains(err.Error(), "wrong") {
		t.Fatalf("err = %v, want the canonical mismatch named at the wrong value", err)
	}
	if got, _ := os.ReadFile(path); string(got) != doorSrc {
		t.Errorf("original bytes were replaced:\n%s", got)
	}
}

// TestSaveTOMLRootScalar: a root key before the first table patches in place.
func TestSaveTOMLRootScalar(t *testing.T) {
	path := seedDoor(t)
	d := loadDoorDoc(t, path)
	d.Seq = 3
	if err := SaveTOML(path, d); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if changed := doorChangedLines(t, []byte(doorSrc), got); len(changed) != 1 || changed[0] != "seq = 3" {
		t.Errorf("changed lines = %q, want [seq = 3]", changed)
	}
}

// TestSaveTOMLNoopWritesNothing: a save that changes nothing does not touch
// the file — not even its mtime.
func TestSaveTOMLNoopWritesNothing(t *testing.T) {
	path := seedDoor(t)
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	if err := SaveTOML(path, loadDoorDoc(t, path)); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(past) {
		t.Errorf("mtime moved %v → %v on a no-op save", past, st.ModTime())
	}
}

// TestSaveTOMLRefusesUnverifiedPatch: an op the patcher renders wrongly,
// forced through the door's own seam, is caught by the verify step — the file
// stays byte-identical and the error names its path.
func TestSaveTOMLRefusesUnverifiedPatch(t *testing.T) {
	path := seedDoor(t)
	d := loadDoorDoc(t, path)
	d.Items[1].Status = "done"

	orig := doorOps
	doorOps = func(old, new any) ([]op, error) {
		ops, err := diff(old, new)
		if err != nil {
			return nil, err
		}
		ops[0].value = int64(42)
		return ops, nil
	}
	defer func() { doorOps = orig }()

	err := SaveTOML(path, d)
	if err == nil {
		t.Fatal("a mis-typed patch was written")
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "left untouched") {
		t.Errorf("err = %v, want the path named and the file left untouched", err)
	}
	if got, _ := os.ReadFile(path); string(got) != doorSrc {
		t.Errorf("original bytes were replaced:\n%s", got)
	}
}

// TestSaveTOMLFreshEncode: an absent path gets the encoder's document, which
// loads back as the value saved.
func TestSaveTOMLFreshEncode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.toml")
	d := &doorDoc{Seq: 1, Name: "n", Items: []doorItem{{ID: "t-1", Status: "pending"}}}
	if err := SaveTOML(path, d); err != nil {
		t.Fatal(err)
	}
	back := loadDoorDoc(t, path)
	if back.Seq != 1 || back.Name != "n" || len(back.Items) != 1 || back.Items[0].ID != "t-1" {
		t.Errorf("fresh file loaded as %+v", back)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o644 {
		t.Errorf("fresh file mode = %v, want 0644", st.Mode().Perm())
	}
}

// TestSaveTOMLRefusesNonPointer: the door needs a pointer to a struct to
// decode the file into; anything else is refused before a byte is written.
func TestSaveTOMLRefusesNonPointer(t *testing.T) {
	path := seedDoor(t)
	if err := SaveTOML(path, *loadDoorDoc(t, path)); err == nil || !strings.Contains(err.Error(), "pointer to a struct") {
		t.Errorf("err = %v, want a pointer-to-struct refusal", err)
	}
	if got, _ := os.ReadFile(path); string(got) != doorSrc {
		t.Errorf("file changed:\n%s", got)
	}

	// The fresh-encode branch refuses too: a struct value would write a file
	// that every later save then fails to decode back into.
	fresh := filepath.Join(t.TempDir(), "fresh.toml")
	if err := SaveTOML(fresh, doorDoc{Seq: 1}); err == nil || !strings.Contains(err.Error(), "pointer to a struct") {
		t.Errorf("fresh path: err = %v, want a pointer-to-struct refusal", err)
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Errorf("fresh path: a file was written (stat err = %v)", err)
	}
	var nilDoc *doorDoc
	if err := SaveTOML(fresh, nilDoc); err == nil || !strings.Contains(err.Error(), "pointer to a struct") {
		t.Errorf("nil pointer: err = %v, want a pointer-to-struct refusal", err)
	}
}
