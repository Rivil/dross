package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
)

const reasonPlan = `[phase]
id = "01-test"
[[task]]
id = "t-1"
wave = 1
title = "x"
files = ["a.go"]
covers = ["c-1"]
[[task]]
id = "t-2"
wave = 1
title = "y"
files = ["b.go"]
covers = ["c-1"]
`

func reasonPlanPath() string { return filepath.Join(".dross", "phases", "01-test", "plan.toml") }

func loadReasonTask(t *testing.T, id string) phase.Task {
	t.Helper()
	plan, err := phase.LoadPlan(reasonPlanPath())
	if err != nil {
		t.Fatal(err)
	}
	task := plan.FindTask(id)
	if task == nil {
		t.Fatalf("no task %s", id)
	}
	return *task
}

// TestTaskFailedReason: the reason survives plan.toml's round trip, hostile
// characters included, and `task show` prints it.
func TestTaskFailedReason(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", reasonPlan)
	reason := "unresolved spec finding (c-1): \"pair mode\" path untested\nsee review ]] [[task]] id = \"t-9\""
	if err := runCmd(t, Task(), "status", "01-test", "t-2", "failed", "--reason", reason); err != nil {
		t.Fatal(err)
	}
	got := loadReasonTask(t, "t-2")
	if got.Status != phase.StatusFailed || got.Reason != reason {
		t.Fatalf("t-2 = %q / %q, want failed / %q", got.Status, got.Reason, reason)
	}
	plan, err := phase.LoadPlan(reasonPlanPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Task) != 2 {
		t.Fatalf("the reason's ]] [[task]] broke out of its string: %d tasks", len(plan.Task))
	}
	out := captureStdout(t, func() {
		if err := runCmd(t, Task(), "show", "01-test", "t-2"); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "reason:       unresolved spec finding (c-1)") {
		t.Fatalf("task show omits the reason:\n%s", out)
	}
	js := captureStdout(t, func() {
		if err := runCmd(t, Task(), "show", "01-test", "t-2", "--json"); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(js, `"reason"`) {
		t.Fatalf("task show --json omits the reason:\n%s", js)
	}
}

// TestReasonClearedOnRetry: a reason belongs to the failure it explains.
func TestReasonClearedOnRetry(t *testing.T) {
	for _, next := range []string{phase.StatusPending, phase.StatusInProgress, phase.StatusDone} {
		t.Run(next, func(t *testing.T) {
			chdir(t, t.TempDir())
			scaffoldPhaseWithPlan(t, "01-test", reasonPlan)
			if err := runCmd(t, Task(), "status", "01-test", "t-2", "failed", "--reason", "red suite"); err != nil {
				t.Fatal(err)
			}
			if err := runCmd(t, Task(), "status", "01-test", "t-2", next); err != nil {
				t.Fatal(err)
			}
			if got := loadReasonTask(t, "t-2"); got.Reason != "" {
				t.Fatalf("t-2 set to %s kept reason %q", next, got.Reason)
			}
		})
	}
}

// TestReasonOnlyWithFailed: --reason explains a failure and nothing else.
func TestReasonOnlyWithFailed(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", reasonPlan)
	for _, status := range []string{phase.StatusDone, phase.StatusPending} {
		err := runCmd(t, Task(), "status", "01-test", "t-1", status, "--reason", "x")
		if err == nil || !strings.Contains(err.Error(), "--reason") {
			t.Errorf("status %s --reason = %v, want an error naming --reason", status, err)
		}
	}
	if got := loadReasonTask(t, "t-1"); got.Status != "" || got.Reason != "" {
		t.Fatalf("a refused --reason still wrote t-1: %+v", got)
	}
}

// TestReasonOmitEmpty: a plan nobody failed gains no reason key.
func TestReasonOmitEmpty(t *testing.T) {
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", reasonPlan)
	if err := runCmd(t, Task(), "status", "01-test", "t-1", "done"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Task(), "status", "01-test", "t-2", "failed"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(reasonPlanPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "reason") {
		t.Fatalf("a reason-less status write added a reason key:\n%s", b)
	}
}
