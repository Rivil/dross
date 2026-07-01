package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangesRecordRoundTripsViaShow(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}

	// First record
	if err := runCmd(t, Changes(),
		"record", "01-test", "t-1",
		"--files", "src/api/tags.ts,src/db/schema.ts",
		"--commit", "abc1234",
		"--notes", "first task",
	); err != nil {
		t.Fatalf("first record: %v", err)
	}

	// Second record for a different task
	if err := runCmd(t, Changes(),
		"record", "01-test", "t-2",
		"--files", "src/api/tags.test.ts",
		"--commit", "def5678",
	); err != nil {
		t.Fatalf("second record: %v", err)
	}

	// changes.json should contain both
	out := captureStdout(t, func() {
		runCmd(t, Changes(), "show", "01-test")
	})
	for _, want := range []string{
		`"phase": "01-test"`,
		`"t-1"`,
		`"t-2"`,
		`"abc1234"`,
		`"def5678"`,
		`"src/api/tags.ts"`,
		`"src/api/tags.test.ts"`,
		`"first task"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("changes show missing %q\n--- output ---\n%s", want, out)
		}
	}

	// Confirm file was actually written
	if _, err := os.Stat(filepath.Join(dir, ".dross", "phases", "01-test", "changes.json")); err != nil {
		t.Errorf("changes.json missing: %v", err)
	}
}

func TestChangesRecordRequiresFiles(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	// Missing --files flag → cobra rejects
	err := runCmd(t, Changes(), "record", "01-test", "t-1")
	if err == nil {
		t.Fatal("expected error when --files missing")
	}
}

func TestChangesRecordEmptyFilesValueIsRejected(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	// --files set to whitespace/empty entries should be rejected (the splitter
	// drops empties; record then sees zero entries)
	err := runCmd(t, Changes(), "record", "01-test", "t-1", "--files", "  ,  ,")
	if err == nil {
		t.Fatal("expected error for effectively-empty files arg")
	}
	if !strings.Contains(err.Error(), "at least one") {
		t.Errorf("error should explain: %v", err)
	}
}

// TestChangesCover_ValidLandmarkRecorded exercises changes.go:39 on the
// err==nil branch: a well-formed --landmark parses cleanly, so record must
// proceed and persist the landmark. If the CONDITIONALS_NEGATION mutant flips
// the guard (`if err == nil { return err }`), the handler returns early before
// recording — no changes.json, so the landmark never appears in show output.
func TestChangesCover_ValidLandmarkRecorded(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Changes(),
		"record", "01-lm", "t-1",
		"--files", "a.ts",
		"--landmark", "feature=Auth, symbol=Login, loc=x.ts:9, what=adds login",
	); err != nil {
		t.Fatalf("record with valid landmark should succeed: %v", err)
	}
	out := captureStdout(t, func() {
		runCmd(t, Changes(), "show", "01-lm")
	})
	for _, want := range []string{`"feature": "Auth"`, `"symbol": "Login"`, `"what": "adds login"`} {
		if !strings.Contains(out, want) {
			t.Errorf("show missing landmark field %q\n%s", want, out)
		}
	}
}

// TestChangesCover_InvalidLandmarkErrors exercises changes.go:39 on the
// err!=nil branch: a malformed --landmark makes ParseLandmark return an error,
// so record must return it. The mutated guard (`if err == nil`) would instead
// swallow the error, append a zero landmark, and succeed — so asserting a
// non-nil error distinguishes real from mutant.
func TestChangesCover_InvalidLandmarkErrors(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, Changes(),
		"record", "01-lm", "t-1",
		"--files", "a.ts",
		"--landmark", "color=blue", // unknown key → ParseLandmark errors
	)
	if err == nil {
		t.Fatal("expected error for a malformed landmark, got nil")
	}
	if !strings.Contains(err.Error(), "landmark") {
		t.Errorf("error should come from ParseLandmark: %v", err)
	}
}

func TestChangesShowEmpty(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	// Show on a phase with no records → prints empty Changes structure
	out := captureStdout(t, func() {
		runCmd(t, Changes(), "show", "01-untouched")
	})
	if !strings.Contains(out, `"phase": "01-untouched"`) {
		t.Errorf("show empty: missing phase line\n%s", out)
	}
}

func TestChangesRecordOverwritesOnRerun(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Changes(), "record", "01-x", "t-1", "--files", "a.ts", "--commit", "old"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Changes(), "record", "01-x", "t-1", "--files", "a.ts,b.ts", "--commit", "new"); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		runCmd(t, Changes(), "show", "01-x")
	})
	if strings.Contains(out, `"old"`) {
		t.Error("old commit should have been overwritten")
	}
	if !strings.Contains(out, `"new"`) {
		t.Errorf("new commit missing\n%s", out)
	}
}
