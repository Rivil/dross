package phase

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
)

// doneRoot builds a .dross root with one scaffolded phase directory `p` and
// returns the root.
func doneRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".dross")
	if err := os.MkdirAll(filepath.Join(root, "phases", "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestDoneRequiresTerminalStatus: changes.json's status marker is the sole
// authority, and exactly {complete, shipped} count. A verify verdict of
// "pass" with no completion record is NOT done — verified is not shipped.
func TestDoneRequiresTerminalStatus(t *testing.T) {
	for status, want := range map[string]bool{
		changes.StatusComplete: true,
		changes.StatusShipped:  true,
		"":                     false,
		"verified":             false,
		"pass":                 false,
		"planned":              false,
	} {
		root := doneRoot(t)
		if status != "" {
			if err := changes.SetStatus(root, "p", status); err != nil {
				t.Fatal(err)
			}
		}
		if got := Done(root, "p"); got != want {
			t.Errorf("status %q: Done = %v, want %v", status, got, want)
		}
		if got := IsDone(root, "p", true); got != want {
			t.Errorf("status %q: IsDone(scaffolded) = %v, want %v", status, got, want)
		}
	}

	t.Run("a verify pass is not a completion record", func(t *testing.T) {
		root := doneRoot(t)
		if err := os.WriteFile(filepath.Join(root, "phases", "p", "verify.toml"), []byte("[verify]\n  verdict = \"pass\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if Done(root, "p") {
			t.Error("a verify.toml verdict of pass read as done — only changes.json's status marker is evidence")
		}
	})
}

// TestDoneNeedsTheDir: an unscaffolded slug is never done, whatever a record
// under some other name says — a roadmap entry with no phase directory is work
// that was listed and never built.
func TestDoneNeedsTheDir(t *testing.T) {
	root := doneRoot(t)
	if DirExists(root, "ghost") {
		t.Fatal("precondition: ghost must not be scaffolded")
	}
	if !DirExists(root, "p") {
		t.Fatal("precondition: p must be scaffolded")
	}
	if Done(root, "ghost") {
		t.Error("an unscaffolded slug read as done")
	}
	// A completion record with no directory behind it is still not done: the
	// scaffolded arm gates before the record is even read.
	if err := changes.SetStatus(root, "p", changes.StatusComplete); err != nil {
		t.Fatal(err)
	}
	if IsDone(root, "p", false) {
		t.Error("IsDone ignored scaffolded=false and read the record anyway")
	}
	if !IsDone(root, "p", true) {
		t.Error("IsDone with the dir and a complete record must be true")
	}
	// A file at the slug's path is not a directory.
	if err := os.WriteFile(filepath.Join(root, "phases", "flat"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if DirExists(root, "flat") {
		t.Error("DirExists accepted a plain file as a phase directory")
	}
}

// TestDonenessReaderDoesNotReadState is the source-level half of the guard. The
// behavioural tests above catch a fallback that changes an answer; this one
// catches the reader growing a state.State parameter again at all, which is how
// the fallback got in the first time.
func TestDonenessReaderDoesNotReadState(t *testing.T) {
	b, err := os.ReadFile("done.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []string{"internal/state", "state.State", "s.History"} {
		if strings.Contains(src, banned) {
			t.Errorf("the doneness reader references %q — doneness reads changes.json alone", banned)
		}
	}
}
