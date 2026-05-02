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

