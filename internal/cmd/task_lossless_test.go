package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/phase"
)

// A hand-annotated plan.toml: the header and per-task comments, inline and
// trailing comments, and a hand-wrapped test_contract that a whole-file
// re-encode used to destroy on every `dross task` write.
const losslessPlan = `# plan header: hand-written notes a re-encode would drop
task_seq = 5

[phase]
  id = "lp"

[[task]]
  id = "t-1"
  wave = 1
  title = "one"
  covers = ["c-1"]
  status = "done"  # first done

[[task]]
  id = "t-2"
  wave = 1
  title = "two"
  covers = ["c-1"]
  status = "pending"
  # trailing note inside t-2

# about t-3
[[task]]
  id = "t-3"
  wave = 2
  title = "three"
  covers = ["c-1"]
  depends_on = ["t-1"]
  test_contract = [
    "if three breaks, TestThree fails",  # hand-wrapped
    "and a second line",
  ]
  status = "pending"

# about t-4
[[task]]
  id = "t-4"
  wave = 2
  title = "four"
  covers = ["c-1"]
  status = "pending"

[[task]]
  id = "t-5"
  wave = 3
  title = "five"
  covers = ["c-1"]
  depends_on = ["t-3", "t-4"]
  status = "pending"
`

const (
	t2Block = "[[task]]\n  id = \"t-2\"\n  wave = 1\n  title = \"two\"\n  covers = [\"c-1\"]\n  status = \"pending\"\n  # trailing note inside t-2\n"
	t3Block = "# about t-3\n[[task]]\n  id = \"t-3\"\n  wave = 2\n  title = \"three\"\n  covers = [\"c-1\"]\n  depends_on = [\"t-1\"]\n  test_contract = [\n    \"if three breaks, TestThree fails\",  # hand-wrapped\n    \"and a second line\",\n  ]\n  status = \"pending\"\n"
	t4Block = "# about t-4\n[[task]]\n  id = \"t-4\"\n  wave = 2\n  title = \"four\"\n  covers = [\"c-1\"]\n  status = \"pending\"\n"
)

// losslessPlanRepo is an initialised repo whose phase lp carries losslessPlan
// and a spec with c-1. It returns the plan's path.
func losslessPlanRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")
	writeSpec(t, dir, "lp", "[phase]\nid = \"lp\"\ntitle = \"LP\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
	writePlan(t, dir, "lp", losslessPlan)
	return filepath.Join(dir, ".dross", "phases", "lp", "plan.toml")
}

// changedLineSet returns the lines of got that differ from want position by
// position; the two must have the same number of lines.
func changedLineSet(t *testing.T, want, got string) []string {
	t.Helper()
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	if len(wl) != len(gl) {
		t.Fatalf("line count changed %d → %d:\n%s", len(wl), len(gl), got)
	}
	var out []string
	for i := range wl {
		if wl[i] != gl[i] {
			out = append(out, gl[i])
		}
	}
	return out
}

func TestTaskStatusChangesOneLine(t *testing.T) {
	path := losslessPlanRepo(t)
	if err := runCmd(t, Task(), "status", "lp", "t-2", "done"); err != nil {
		t.Fatal(err)
	}
	if got := changedLineSet(t, losslessPlan, mustRead(t, path)); len(got) != 1 || got[0] != `  status = "done"` {
		t.Errorf("changed = %q, want only t-2's status line", got)
	}
	if !strings.Contains(mustRead(t, path), "  # trailing note inside t-2\n") {
		t.Error("t-2's trailing comment did not survive")
	}
}

func TestTaskAddInsertsInPlace(t *testing.T) {
	path := losslessPlanRepo(t)
	if err := runCmd(t, Task(), "add", "lp", "--title", "mid", "--covers", "c-1", "--after", "t-1"); err != nil {
		t.Fatal(err)
	}
	got := mustRead(t, path)
	start := strings.Index(got, "[[task]]\n  id = \"t-6\"")
	if start < 0 {
		t.Fatalf("no t-6 block:\n%s", got)
	}
	if start > strings.Index(got, `id = "t-2"`) || start < strings.Index(got, `id = "t-1"`) {
		t.Errorf("t-6 did not land between t-1 and t-2:\n%s", got)
	}
	end := strings.Index(got[start:], "\n\n")
	if end < 0 {
		t.Fatalf("the new block has no separator:\n%s", got)
	}
	rest := got[:start] + got[start+end+2:]
	if changed := changedLineSet(t, losslessPlan, rest); len(changed) != 1 || changed[0] != "task_seq = 6" {
		t.Errorf("outside the new block, changed = %q; want only task_seq", changed)
	}
}

func TestTaskEditChangesOneLine(t *testing.T) {
	path := losslessPlanRepo(t)
	if err := runCmd(t, Task(), "edit", "lp", "t-3", "--title", "X"); err != nil {
		t.Fatal(err)
	}
	if got := changedLineSet(t, losslessPlan, mustRead(t, path)); len(got) != 1 || got[0] != `  title = "X"` {
		t.Errorf("changed = %q, want only t-3's title line", got)
	}
}

// TestTaskMoveRelocatesTheBlock: t-4 moves with its comment, t-2 and t-3 stay
// byte-identical, and nothing but wave lines changes anywhere else.
func TestTaskMoveRelocatesTheBlock(t *testing.T) {
	path := losslessPlanRepo(t)
	if err := runCmd(t, Task(), "move", "lp", "t-4", "--before", "t-2"); err != nil {
		t.Fatal(err)
	}
	got := mustRead(t, path)
	for _, block := range []string{t2Block, t3Block} {
		if !strings.Contains(got, block) {
			t.Errorf("a block that did not move changed; missing:\n%s", block)
		}
	}
	moved := strings.Index(got, "# about t-4\n[[task]]\n  id = \"t-4\"")
	if moved < 0 || moved > strings.Index(got, `id = "t-2"`) {
		t.Fatalf("t-4 (with its comment) is not ahead of t-2:\n%s", got)
	}
	p, err := phase.LoadPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, tk := range p.Task {
		order = append(order, tk.ID)
	}
	if strings.Join(order, ",") != "t-1,t-4,t-2,t-3,t-5" {
		t.Errorf("order = %v", order)
	}
	// Cut t-4's block out of both sides: what remains may differ only in
	// wave lines (the moved task's own, and any reflowed dependent's).
	movedEnd := moved + strings.Index(got[moved:], "\n\n") + 2
	before := strings.Replace(losslessPlan, "\n"+t4Block, "", 1)
	after := got[:moved] + got[movedEnd:]
	for _, l := range changedLineSet(t, before, after) {
		if !strings.HasPrefix(l, "  wave = ") {
			t.Errorf("a move rewrote a non-wave line: %q", l)
		}
	}
}

func TestTaskRemoveTakesItsBlock(t *testing.T) {
	path := losslessPlanRepo(t)
	if err := runCmd(t, Task(), "remove", "lp", "t-3", "--force"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(losslessPlan, "\n"+t3Block, "", 1)
	want = strings.Replace(want, `depends_on = ["t-3", "t-4"]`, `depends_on = ["t-4"]`, 1)
	if got := mustRead(t, path); got != want {
		t.Errorf("remove wrote:\n%s\nwant:\n%s", got, want)
	}
}

func TestTaskNoopStatusWritesNothing(t *testing.T) {
	path := losslessPlanRepo(t)
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Task(), "status", "lp", "t-1", "done"); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(past) {
		t.Error("a done→done status write rewrote plan.toml")
	}
}

// TestTaskStatusRefusesUnprovablePatch: a plan the lossless writer cannot
// match by id (two tasks sharing one) is refused — plan.toml byte-identical,
// the error naming it.
func TestTaskStatusRefusesUnprovablePatch(t *testing.T) {
	path := losslessPlanRepo(t)
	dup := strings.Replace(losslessPlan, `id = "t-5"`, `id = "t-4"`, 1)
	mustWrite(t, path, dup)
	err := runCmd(t, Task(), "status", "lp", "t-2", "done")
	if err == nil || !strings.Contains(err.Error(), "plan.toml") {
		t.Fatalf("err = %v, want a refusal naming plan.toml", err)
	}
	if got := mustRead(t, path); got != dup {
		t.Errorf("a refused write changed plan.toml:\n%s", got)
	}
}
