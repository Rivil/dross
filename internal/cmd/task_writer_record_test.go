package cmd

import (
	"testing"

	"github.com/Rivil/dross/internal/gatestate"
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
