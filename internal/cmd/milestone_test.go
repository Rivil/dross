package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMilestoneCreateAndList(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}

	for _, v := range []string{"v0.1", "v0.2", "v1.0"} {
		if err := runCmd(t, Milestone(), "create", v); err != nil {
			t.Fatalf("create %s: %v", v, err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".dross/milestones", v+".toml")); err != nil {
			t.Errorf("toml not written for %s: %v", v, err)
		}
	}

	out := captureStdout(t, func() {
		runCmd(t, Milestone(), "list")
	})
	for _, want := range []string{"v0.1", "v0.2", "v1.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %s:\n%s", want, out)
		}
	}
}

func TestMilestoneCreateRefusesIfExists(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, Milestone(), "create", "v0.1")
	if err == nil {
		t.Fatal("expected error on duplicate create")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention existence: %v", err)
	}
}

func TestMilestoneShowPrintsToml(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		runCmd(t, Milestone(), "show", "v0.1")
	})
	for _, want := range []string{
		"v0.1.toml",
		`version = "v0.1"`,
		`status = "planning"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show missing %q:\n%s", want, out)
		}
	}
}

func TestMilestoneListEmpty(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		runCmd(t, Milestone(), "list")
	})
	if !strings.Contains(out, "no milestones") {
		t.Errorf("expected 'no milestones' on empty list:\n%s", out)
	}
}

func TestMilestoneSetGet(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Milestone(), "set", "v0.1", "milestone.title", "First release"); err != nil {
		t.Fatalf("set title: %v", err)
	}
	if err := runCmd(t, Milestone(), "set", "v0.1", "milestone.status", "active"); err != nil {
		t.Fatalf("set status: %v", err)
	}

	out := captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "v0.1", "milestone.title")
	})
	if !strings.Contains(out, "First release") {
		t.Errorf("get title returned %q", out)
	}

	out = captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "v0.1", "milestone.status")
	})
	if !strings.Contains(out, "active") {
		t.Errorf("get status returned %q", out)
	}
}

func TestMilestoneAddListsAreIdempotent(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}

	for _, c := range []string{
		"perft suite passes at depth 5",
		"mutation score >= 0.80",
		"perft suite passes at depth 5", // duplicate — should be ignored
	} {
		if err := runCmd(t, Milestone(), "add", "v0.1", "scope.success_criteria", c); err != nil {
			t.Fatalf("add criterion %q: %v", c, err)
		}
	}
	for _, ng := range []string{"no engine", "no UCI"} {
		if err := runCmd(t, Milestone(), "add", "v0.1", "scope.non_goals", ng); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"01-board-fen", "02-pseudolegal-moves"} {
		if err := runCmd(t, Milestone(), "add", "v0.1", "phases", p); err != nil {
			t.Fatal(err)
		}
	}

	out := captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "v0.1", "scope.success_criteria")
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 unique success criteria (dup ignored), got %d:\n%s", len(lines), out)
	}

	out = captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "v0.1", "phases")
	})
	if !strings.Contains(out, "01-board-fen") || !strings.Contains(out, "02-pseudolegal-moves") {
		t.Errorf("phases get missing entries:\n%s", out)
	}
}

func TestMilestoneAddAcceptsFieldAliases(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}

	// Bare spellings and scope.phases should all resolve to the canonical
	// field rather than failing with "not a list field".
	aliases := map[string][2]string{
		"success_criteria": {"perft passes", "scope.success_criteria"},
		"non_goals":        {"no engine", "scope.non_goals"},
		"scope.phases":     {"01-board", "phases"},
	}
	for alias, want := range aliases {
		if err := runCmd(t, Milestone(), "add", "v0.1", alias, want[0]); err != nil {
			t.Fatalf("add via alias %q: %v", alias, err)
		}
		out := captureStdout(t, func() {
			runCmd(t, Milestone(), "get", "v0.1", want[1])
		})
		if !strings.Contains(out, want[0]) {
			t.Errorf("alias %q did not append to %s; get returned:\n%s", alias, want[1], out)
		}
	}
}

func TestMilestoneAddUnknownFieldListsValid(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, Milestone(), "add", "v0.1", "bogus", "x")
	if err == nil || !strings.Contains(err.Error(), "not a list field") ||
		!strings.Contains(err.Error(), "scope.success_criteria") {
		t.Errorf("expected actionable error listing valid fields, got: %v", err)
	}
}

func TestMilestoneSetRejectsListPaths(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, Milestone(), "set", "v0.1", "scope.success_criteria", "x")
	if err == nil || !strings.Contains(err.Error(), "use `dross milestone add`") {
		t.Errorf("expected helpful error pointing at add, got: %v", err)
	}
}

func TestMilestoneGetRejectsUnknownField(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, Milestone(), "get", "v0.1", "nonsense.field")
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("expected unknown-field error, got: %v", err)
	}
}

func TestMilestoneVersionDefaultsToCurrent(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, State(), "set", "current_milestone", "v0.1"); err != nil {
		t.Fatal(err)
	}

	// set/add without a version target the current milestone.
	if err := runCmd(t, Milestone(), "set", "milestone.title", "Defaulted"); err != nil {
		t.Fatalf("set without version: %v", err)
	}
	if err := runCmd(t, Milestone(), "add", "scope.success_criteria", "c-1 holds"); err != nil {
		t.Fatalf("add without version: %v", err)
	}

	// get without a version reads the current milestone.
	out := captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "milestone.title")
	})
	if !strings.Contains(out, "Defaulted") {
		t.Errorf("get without version returned %q", out)
	}
	out = captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "scope.success_criteria")
	})
	if !strings.Contains(out, "c-1 holds") {
		t.Errorf("add/get without version round-trip failed:\n%s", out)
	}

	// The explicit-version form still works and points at the same milestone.
	out = captureStdout(t, func() {
		runCmd(t, Milestone(), "get", "v0.1", "milestone.title")
	})
	if !strings.Contains(out, "Defaulted") {
		t.Errorf("explicit-version get returned %q", out)
	}
}

func TestMilestoneNoVersionNoCurrentErrors(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v0.1"); err != nil {
		t.Fatal(err)
	}
	// No current_milestone set — omitting the version must fail clearly.
	err := runCmd(t, Milestone(), "get", "milestone.title")
	if err == nil || !strings.Contains(err.Error(), "current_milestone") {
		t.Errorf("expected current_milestone error, got: %v", err)
	}
}
