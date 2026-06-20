package quality

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunID(t *testing.T) {
	now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if got, want := RunID(now, "abc1234"), "20240115T103000-abc1234"; got != want {
		t.Fatalf("RunID = %q, want %q", got, want)
	}
	if got, want := RunID(now, ""), "20240115T103000-nogit"; got != want {
		t.Fatalf("RunID with empty sha = %q, want %q", got, want)
	}
}

func TestShortSHAFallback(t *testing.T) {
	// t.TempDir() lives in the OS temp tree, outside any git repo, so
	// `git rev-parse` fails and ShortSHA must fall back to "nogit".
	if got := ShortSHA(t.TempDir()); got != "nogit" {
		t.Fatalf("ShortSHA(non-repo) = %q, want \"nogit\"", got)
	}
}

func TestNormalizeSHA(t *testing.T) {
	// Empty / whitespace-only git output falls back to "nogit"; a real sha is
	// trimmed and returned. (Covers the empty-output branch ShortSHA can't force.)
	if got := normalizeSHA("   \n"); got != "nogit" {
		t.Errorf("normalizeSHA(blank) = %q, want \"nogit\"", got)
	}
	if got := normalizeSHA(""); got != "nogit" {
		t.Errorf("normalizeSHA(empty) = %q, want \"nogit\"", got)
	}
	if got := normalizeSHA("abc1234\n"); got != "abc1234" {
		t.Errorf("normalizeSHA(sha) = %q, want \"abc1234\"", got)
	}
}

func TestNewRunNoClobber(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	first, err := NewRun(root, now, "abc1234")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRun(root, now, "abc1234") // same second, same commit
	if err != nil {
		t.Fatal(err)
	}

	if first == second {
		t.Fatalf("second run reused the first dir %q — collision not handled", first)
	}
	for _, d := range []string{first, second} {
		if info, err := os.Stat(d); err != nil || !info.IsDir() {
			t.Fatalf("run dir %q missing after NewRun (err=%v)", d, err)
		}
	}
	if got := filepath.Base(first); got != "20240115T103000-abc1234" {
		t.Fatalf("first run dir = %q, want the unsuffixed id", got)
	}
	if got := filepath.Base(second); !strings.HasPrefix(got, "20240115T103000-abc1234-") {
		t.Fatalf("second run dir = %q, want a suffixed variant of the first", got)
	}
}

func TestNewRunWriteBoundary(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	runDir, err := NewRun(root, now, "abc1234")
	if err != nil {
		t.Fatal(err)
	}

	qDir := QualityDir(root)
	if !strings.HasPrefix(runDir, qDir+string(os.PathSeparator)) {
		t.Fatalf("run dir %q escapes the quality dir %q", runDir, qDir)
	}

	// Enumerate every path NewRun created under root and assert each one is the
	// quality dir or lives within it — a real path-set check, not a
	// never-triggered negative.
	err = filepath.Walk(root, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == root || path == qDir {
			return nil
		}
		if !strings.HasPrefix(path, qDir+string(os.PathSeparator)) {
			t.Fatalf("NewRun touched a path outside .dross/quality/: %q", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
