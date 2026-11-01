package memadvise

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/gitrun"
)

// suggestion advises on mem and returns the suggestion with id.
func suggestion(t *testing.T, mem, id string, known map[string]bool) Suggestion {
	t.Helper()
	all, err := Advise(Inputs{Dir: mem, Docs: docsWithReadme(), Today: adviseToday, Known: known, Turns: TurnCounts{Repo: 10, All: 20}})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := byID(all)[id]
	if !ok {
		t.Fatalf("no suggestion %s in %+v", id, all)
	}
	return s
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const threeIndex = "- [Dup](dup.md) — restates the readme\n- [Keep](keep.md) — stays; next: foo\n- [Other](other.md) — also stays\n"

func threeMemories(t *testing.T) string {
	return memFixture(t, map[string]string{
		IndexName:  threeIndex,
		"dup.md":   readme,
		"keep.md":  "keep body\n",
		"other.md": "other body\n",
	})
}

func TestApplyDeleteRemovesFileAndLine(t *testing.T) {
	mem := threeMemories(t)
	keepBefore, otherBefore := readFile(t, filepath.Join(mem, "keep.md")), readFile(t, filepath.Join(mem, "other.md"))
	if _, err := Apply(t.TempDir(), mem, suggestion(t, mem, "delete-dup", nil), adviseToday); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(mem, "dup.md")); !os.IsNotExist(err) {
		t.Errorf("dup.md still exists: %v", err)
	}
	want := "- [Keep](keep.md) — stays; next: foo\n- [Other](other.md) — also stays\n"
	if got := readFile(t, filepath.Join(mem, IndexName)); got != want {
		t.Errorf("MEMORY.md = %q, want %q", got, want)
	}
	// The whole tree, not just the named files: a stray temp file fails this.
	expected := memFixture(t, map[string]string{IndexName: want, "keep.md": keepBefore, "other.md": otherBefore})
	if dirHash(t, mem) != dirHash(t, expected) {
		entries, _ := os.ReadDir(mem)
		t.Errorf("memory dir after apply differs from the expected tree: %v", entries)
	}
}

func TestApplyRewordTouchesOneLine(t *testing.T) {
	mem := threeMemories(t)
	s := suggestion(t, mem, "reword-keep", map[string]bool{"foo": true})
	if s.NewHook != "stays" {
		t.Fatalf("NewHook = %q", s.NewHook)
	}
	if _, err := Apply(t.TempDir(), mem, s, adviseToday); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(threeIndex, "stays; next: foo", "stays", 1)
	if got := readFile(t, filepath.Join(mem, IndexName)); got != want {
		t.Errorf("MEMORY.md = %q, want %q", got, want)
	}

	// CRLF and a missing trailing newline survive a reword.
	crlf := memFixture(t, map[string]string{
		IndexName: "- [A](a.md) — a\r\n- [B](b.md) — b; until 2026-01-01",
		"a.md":    "a\n",
		"b.md":    "b\n",
	})
	if _, err := Apply(t.TempDir(), crlf, suggestion(t, crlf, "reword-b", nil), adviseToday); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(crlf, IndexName)); got != "- [A](a.md) — a\r\n- [B](b.md) — b" {
		t.Errorf("CRLF MEMORY.md = %q", got)
	}
}

func TestApplyKeepsEachLineEnding(t *testing.T) {
	a, b, c := "- [A](a.md) — a", "- [B](b.md) — b; until 2026-01-01", "- [Dup](dup.md) — c"
	for _, tc := range []struct{ id, want string }{
		{"reword-b", a + "\r\n- [B](b.md) — b\n" + c + "\r\n"},
		{"delete-dup", a + "\r\n" + b + "\n"},
	} {
		mem := memFixture(t, map[string]string{
			IndexName: a + "\r\n" + b + "\n" + c + "\r\n",
			"a.md":    "a\n",
			"b.md":    "b\n",
			"dup.md":  readme,
		})
		if _, err := Apply(t.TempDir(), mem, suggestion(t, mem, tc.id, nil), adviseToday); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, filepath.Join(mem, IndexName)); got != tc.want {
			t.Errorf("%s on a mixed LF/CRLF index: MEMORY.md = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestApplySnapshotsLargeFiles(t *testing.T) {
	mem := threeMemories(t)
	big := strings.Repeat("x", maxFileBytes+1)
	for _, name := range []string{"big.md", "big.log"} {
		if err := os.WriteFile(filepath.Join(mem, name), []byte(big), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := dirHash(t, mem)
	res, err := Apply(t.TempDir(), mem, suggestion(t, mem, "delete-dup", nil), adviseToday)
	if err != nil {
		t.Fatalf("Apply with a file over %d bytes in the dir: %v", maxFileBytes, err)
	}
	if dirHash(t, res.Snapshot) != before {
		t.Error("the snapshot does not hold the large files byte for byte")
	}
}

func TestApplySnapshotsFirst(t *testing.T) {
	mem := threeMemories(t)
	before := dirHash(t, mem)
	root := t.TempDir()
	res, err := Apply(root, mem, suggestion(t, mem, "delete-dup", nil), adviseToday)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Snapshot, filepath.Join(root, filepath.FromSlash(SnapshotsDir))) {
		t.Errorf("snapshot %s is not under %s", res.Snapshot, SnapshotsDir)
	}
	if got := dirHash(t, res.Snapshot); got != before {
		t.Error("the snapshot does not match the pre-apply memory dir")
	}
}

func TestApplyAbortsWhenSnapshotFails(t *testing.T) {
	mem := threeMemories(t)
	root := t.TempDir()
	// A file where the state dir goes: the snapshot cannot be taken.
	if err := os.MkdirAll(filepath.Join(root, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(StateDir)), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := dirHash(t, mem)
	if _, err := Apply(root, mem, suggestion(t, mem, "delete-dup", nil), adviseToday); err == nil {
		t.Fatal("Apply succeeded without a snapshot")
	}
	if dirHash(t, mem) != before {
		t.Error("the memory dir changed though the snapshot failed")
	}
}

func TestApplyRefusesGuardedDelete(t *testing.T) {
	mem := threeMemories(t)
	s := suggestion(t, mem, "delete-dup", nil)
	// The memory gained a guard phrase after the advice was made.
	if err := os.WriteFile(filepath.Join(mem, "dup.md"), []byte("DECOY\n"+readme), 0o644); err != nil {
		t.Fatal(err)
	}
	before := dirHash(t, mem)
	_, err := Apply(t.TempDir(), mem, s, adviseToday)
	if err == nil || !strings.Contains(err.Error(), "guard") {
		t.Fatalf("Apply = %v, want a guard refusal", err)
	}
	if dirHash(t, mem) != before {
		t.Error("the memory dir changed on a refused delete")
	}
}

func TestApplyRefusesReportOnly(t *testing.T) {
	mem := memFixture(t, map[string]string{
		IndexName: "- [A](a.md) — a\n",
		"a.md":    "a\n",
		"lost.md": "orphan\n",
	})
	s := suggestion(t, mem, "delete-lost", nil)
	before := dirHash(t, mem)
	root := t.TempDir()
	if _, err := Apply(root, mem, s, adviseToday); err == nil || !strings.Contains(err.Error(), "report-only") {
		t.Fatalf("Apply on an orphan = %v, want a report-only refusal", err)
	}
	if dirHash(t, mem) != before {
		t.Error("the memory dir changed")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(SnapshotsDir))); !os.IsNotExist(err) {
		t.Error("a refused report-only apply still took a snapshot")
	}
}

func TestApplyNeverDeletesOutsideDir(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}
	mem := memFixture(t, map[string]string{
		IndexName: "- [Link](link.md) — a symlink out\n- [Esc](../x.md) — escapes\n",
	})
	if err := os.Symlink(outside, filepath.Join(mem, "link.md")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"delete-link", "delete-x"} {
		if _, err := Apply(t.TempDir(), mem, suggestion(t, mem, id, nil), adviseToday); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(mem, "link.md")); err != nil {
		t.Errorf("the symlink was removed: %v", err)
	}
	if got := readFile(t, outside); got != readme {
		t.Error("the symlink's target changed")
	}
	if got := readFile(t, filepath.Join(mem, IndexName)); got != "" {
		t.Errorf("MEMORY.md = %q, want both dead lines gone", got)
	}
}

func TestApplyAbortsOnConcurrentIndexEdit(t *testing.T) {
	mem := threeMemories(t)
	s := suggestion(t, mem, "delete-dup", nil)
	planned := []byte(threeIndex)
	// A session saved a memory between the plan and the rename.
	edited := threeIndex + "- [New](new.md) — just saved\n"
	if err := os.WriteFile(filepath.Join(mem, IndexName), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := applyPlanned(t.TempDir(), mem, s, planned, adviseToday)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("applyPlanned = %v, want a concurrent-edit refusal", err)
	}
	if got := readFile(t, filepath.Join(mem, IndexName)); got != edited {
		t.Errorf("the concurrent edit was overwritten: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(mem, "dup.md")); err != nil {
		t.Errorf("dup.md was removed: %v", err)
	}
}

func TestApplyRechecksIntegrity(t *testing.T) {
	mem := memFixture(t, map[string]string{
		IndexName:   "- [Dup](dup.md) — restates the readme\n- [Linker](linker.md) — links it\n",
		"dup.md":    "---\nname: the-dup\n---\n" + readme,
		"linker.md": "see [[the-dup]]\n",
		"lost.md":   "a pre-existing orphan\n",
	})
	res, err := Apply(t.TempDir(), mem, suggestion(t, mem, "delete-dup", nil), adviseToday)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Dangling["the-dup"]; !reflect.DeepEqual(got, []string{"linker.md"}) {
		t.Errorf("Dangling[the-dup] = %v, want [linker.md]", got)
	}
	if !reflect.DeepEqual(res.Orphans, []string{"lost.md"}) {
		t.Errorf("Orphans = %v, want [lost.md]", res.Orphans)
	}
	if len(res.DeadLinks) != 0 {
		t.Errorf("DeadLinks = %+v", res.DeadLinks)
	}
}

func TestSnapshotSelfIgnores(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := gitrun.Run(root, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	mem := threeMemories(t)
	snap, err := Snapshot(root, mem, adviseToday)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(snap)
	if err != nil || len(entries) != 4 {
		t.Fatalf("snapshot holds %d entries (%v), want 4", len(entries), err)
	}
	for _, e := range entries {
		rel, _ := filepath.Rel(root, filepath.Join(snap, e.Name()))
		if err := gitrun.Quiet(root, "check-ignore", "-q", "--", filepath.ToSlash(rel)); err != nil {
			t.Errorf("%s is not ignored: %v", rel, err)
		}
	}
}

// A symlink at any component of the snapshot path — .dross, .dross/memory or
// snapshots/ — is refused before anything is copied, so a link aimed at the
// memory dir (or anywhere else) never receives a snapshot.
func TestSnapshotRefusesLinkedStatePath(t *testing.T) {
	for name, link := range map[string]string{
		"dross":     ".dross",
		"state":     ".dross/memory",
		"snapshots": ".dross/memory/snapshots",
	} {
		t.Run(name, func(t *testing.T) {
			mem := threeMemories(t)
			out := t.TempDir()
			root := t.TempDir()
			at := filepath.Join(root, filepath.FromSlash(link))
			if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
				t.Fatal(err)
			}
			// The deepest link aims at the memory dir itself; the parent
			// links aim at a bystander dir.
			target := out
			if name == "snapshots" {
				target = mem
			}
			if err := os.Symlink(target, at); err != nil {
				t.Fatal(err)
			}
			memBefore, outBefore := memFingerprint(t, mem), memFingerprint(t, out)

			_, err := Snapshot(root, mem, adviseToday)
			if err == nil || !strings.Contains(err.Error(), "is not a directory") {
				t.Fatalf("Snapshot through a linked %s: err = %v, want a not-a-directory refusal", link, err)
			}
			if after := memFingerprint(t, mem); after != memBefore {
				t.Errorf("a linked %s let the snapshot into the memory dir:\nbefore %q\nafter  %q", link, memBefore, after)
			}
			if after := memFingerprint(t, out); after != outBefore {
				t.Errorf("a linked %s let the snapshot write through it:\nbefore %q\nafter  %q", link, outBefore, after)
			}
		})
	}
}
