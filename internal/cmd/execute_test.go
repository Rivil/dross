package cmd

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/gatestate"
)

// executeFixture is a dross repo with phase p planned.
func executeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "p", "plan.toml"), "[phase]\n  id = \"p\"\n")
	return repoDirHere(t)
}

func executeRecord(t *testing.T, dir string) (*gatestate.Execute, []byte) {
	t.Helper()
	rec, err := gatestate.LoadExecute(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(gatestate.Path(dir, gatestate.ExecuteFile))
	return rec, b
}

func TestExecuteBeginRecordsMode(t *testing.T) {
	dir := executeFixture(t)
	if err := runCmd(t, Execute(), "begin", "p"); err != nil {
		t.Fatal(err)
	}
	if rec, _ := executeRecord(t, dir); rec == nil || rec.Mode != "pair" || rec.Phase != "p" {
		t.Fatalf("begin p recorded %+v, want pair for p", rec)
	}
	if err := runCmd(t, Execute(), "begin", "p", "--solo"); err != nil {
		t.Fatal(err)
	}
	rec, before := executeRecord(t, dir)
	if rec == nil || rec.Mode != "solo" {
		t.Fatalf("begin p --solo recorded %+v, want solo", rec)
	}
	var out string
	if err := runCmdCapturing(t, &out, Execute(), "begin", "p", "--solo"); err != nil {
		t.Fatal(err)
	}
	if _, after := executeRecord(t, dir); string(after) != string(before) {
		t.Errorf("a same-mode re-run rewrote execute.json:\n%s\n->\n%s", before, after)
	}
	if !strings.Contains(out, "solo") {
		t.Errorf("begin printed %q, want the recorded mode", out)
	}
}

func TestExecuteBeginPreconditions(t *testing.T) {
	dir := executeFixture(t)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "unplanned", "spec.toml"), "[phase]\n")
	for _, id := range []string{"nope", "unplanned", "../p"} {
		if err := runCmd(t, Execute(), "begin", id); err == nil {
			t.Errorf("begin %s succeeded", id)
		}
	}
	if _, err := os.Stat(gatestate.Path(dir, gatestate.ExecuteFile)); !os.IsNotExist(err) {
		t.Errorf("a refused begin wrote execute.json (err=%v)", err)
	}
}

func TestExecuteBeginLeavesStateAlone(t *testing.T) {
	dir := executeFixture(t)
	statePath := filepath.Join(dir, ".dross", "state.json")
	sum := func() [32]byte {
		b, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		return sha256.Sum256(b)
	}
	before := sum()
	for _, args := range [][]string{{"begin", "p"}, {"begin", "p", "--solo"}} {
		if err := runCmd(t, Execute(), args...); err != nil {
			t.Fatal(err)
		}
	}
	if sum() != before {
		t.Error("dross execute begin changed state.json")
	}
}
