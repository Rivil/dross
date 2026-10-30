package cmd

import (
	"errors"
	"testing"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/phase"
)

// TestWriterRecordClearedOnRetry: a reset to pending is a retry, so it
// releases the writer record naming that task — else the retried task's
// writer spawn is refused as a second writer and the main loop stays fenced
// out forever. `failed` keeps the record (it fences a zombie writer out of the
// failed task), and a reset of another task leaves it alone.
func TestWriterRecordClearedOnRetry(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	scaffoldPhaseWithPlan(t, "01-test", reasonPlan)
	rec := gatestate.Writer{Phase: "01-test", Task: "t-1", AgentID: "abd48e82da367fc86", State: gatestate.WriterLaunched}
	if err := gatestate.SaveWriter(dir, rec); err != nil {
		t.Fatal(err)
	}
	present := func(step string) bool {
		t.Helper()
		w, err := gatestate.LoadWriter(dir)
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		return w != nil
	}

	if err := runCmd(t, Task(), "status", "01-test", "t-1", "failed", "--reason", "writer: red"); err != nil {
		t.Fatal(err)
	}
	if !present("after failed") {
		t.Fatal("marking the writer's task failed removed its record — a zombie writer would no longer be fenced")
	}
	if err := runCmd(t, Task(), "status", "01-test", "t-2", "pending"); err != nil {
		t.Fatal(err)
	}
	if !present("after resetting t-2") {
		t.Fatal("resetting another task removed t-1's writer record")
	}
	if err := runCmd(t, Task(), "status", "01-test", "t-1", "pending"); err != nil {
		t.Fatal(err)
	}
	if present("after resetting t-1") {
		t.Fatal("resetting t-1 to pending left its writer record — the retry's writer spawn would be refused as a second writer")
	}
}

// TestWriterRecordKeptOnSaveFailure: the record is released only once the
// plan has saved — a reset whose save fails leaves the task where it was, so
// its writer record (the zombie fence) must stay with it.
func TestWriterRecordKeptOnSaveFailure(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	scaffoldPhaseWithPlan(t, "01-test", reasonPlan)
	if err := gatestate.SaveWriter(dir, gatestate.Writer{Phase: "01-test", Task: "t-1", AgentID: "abd48e82da367fc86", State: gatestate.WriterLaunched}); err != nil {
		t.Fatal(err)
	}
	orig := savePlan
	t.Cleanup(func() { savePlan = orig })
	savePlan = func(*phase.Plan, string) error { return errors.New("disk full") }

	if err := runCmd(t, Task(), "status", "01-test", "t-1", "pending"); err == nil {
		t.Fatal("a reset whose plan save failed reported success")
	}
	if w, err := gatestate.LoadWriter(dir); err != nil || w == nil {
		t.Fatalf("a failed plan save released the writer record (%v, %v): the zombie fence dropped for a task never reset", w, err)
	}
}
