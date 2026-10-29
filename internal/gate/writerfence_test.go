package gate

import (
	"path/filepath"
	"testing"

	"github.com/Rivil/dross/internal/gatestate"
)

// fencedRepo is a solo repo with t-1 (files a.go) in progress and handed to
// the captured writer, in state.
func fencedRepo(t *testing.T, state string) string {
	t.Helper()
	dir := soloRepo(t, "in_progress")
	if err := gatestate.SaveWriter(dir, gatestate.Writer{Phase: "p", Task: "t-1", AgentID: capturedWriterID, State: state}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writerBash is the captured writer Bash call (writer_pre_bash.json) moved
// into dir, running line.
func writerBash(t *testing.T, dir, line string) []byte {
	t.Helper()
	return bgPost(t, "writer_pre_bash.json", dir, func(m map[string]any) {
		m["tool_input"].(map[string]any)["command"] = line
	})
}

// fixtureEdit is a captured Edit (writer's or main loop's) moved into dir,
// editing rel.
func fixtureEdit(t *testing.T, name, dir, rel string) []byte {
	t.Helper()
	return bgPost(t, name, dir, func(m map[string]any) {
		m["tool_input"].(map[string]any)["file_path"] = filepath.Join(dir, filepath.FromSlash(rel))
	})
}

func admits(t *testing.T, name string, res Result) {
	t.Helper()
	if !res.Allowed() {
		t.Errorf("%s: refused: %s", name, res.Text())
	}
}

// TestWriterBashAllowlist pins c-1: the writer runs read-only git, scoped
// dross and the toolchain's checks, and none of the verbs that commit, set
// task status, finalize or touch the forge.
func TestWriterBashAllowlist(t *testing.T) {
	dir := fencedRepo(t, gatestate.WriterLaunched)
	for _, line := range []string{
		"git commit -m x",
		"git stash",
		"git reset --hard",
		"git push",
		"dross task status p t-3 done",
		"dross changes record p t-3 --files a.go",
		"dross phase complete p",
		"gh pr create",
		"dross test",
		"rm b.go",
		"go mod tidy",
		"gofmt -w a.go",
		"echo x > b.go",
		`dross task status p t-1 failed --reason show`,
		"dross rule add x --text show",
		"sed -i s/a/b/ a.go",
		"sed -i.bak s/a/b/ a.go",
		"perl -i -pe s/a/b/ a.go",
		"find . -name '*.go' -exec cat {} +",
		"find . -execdir cat {} +",
		"find . -name x -delete",
		"find . -ok cat {} +",
	} {
		if res := writerCheck(t, writerBash(t, dir, line)); res.Allowed() {
			t.Errorf("the writer ran %q", line)
		}
	}
	for _, line := range []string{
		"dross test --files a.go",
		"git diff",
		"git -C " + dir + " status --short",
		"go vet ./...",
		"gofmt -l .",
		"dross task show p t-1",
		"ls internal && grep -n Foo a.go",
		"sed -n 1p a.go",
		"find . -name '*.go'",
	} {
		admits(t, line, writerCheck(t, writerBash(t, dir, line)))
	}
}

// TestFenceFailsClosedOnOpaqueShell: a commit hidden in a nested shell is
// refused, not waved through unread.
func TestFenceFailsClosedOnOpaqueShell(t *testing.T) {
	dir := fencedRepo(t, gatestate.WriterLaunched)
	for _, line := range []string{
		"sh -c 'git commit -m x'",
		`eval "git commit -m x"`,
		`bash -c "git commit -am x"`,
		"echo git commit | xargs -I{} sh -c {}",
	} {
		if res := writerCheck(t, writerBash(t, dir, line)); res.Allowed() {
			t.Errorf("the writer ran %q", line)
		}
	}
}

// TestWriterSpawnsNoAgent pins c-1: a writer does its task itself, so any
// Agent or Task spawn it makes — another writer included — is refused.
func TestWriterSpawnsNoAgent(t *testing.T) {
	dir := fencedRepo(t, gatestate.WriterLaunched)
	for _, tool := range []string{"Agent", "Task"} {
		for _, sub := range []string{"Explore", "dross-task-writer"} {
			in := bgPost(t, "writer_pre_bash.json", dir, func(m map[string]any) {
				m["tool_name"] = tool
				m["tool_input"] = map[string]any{"subagent_type": sub, "prompt": "x", "run_in_background": true}
			})
			refusedWith(t, tool+" "+sub, writerCheck(t, in), "never spawns another agent")
		}
	}
}

// TestWriterPartialLineRefused: a writer line the scanner reads only in part
// is refused whole, never judged on its readable prefix — past an
// unterminated quote the scanner sees only a harmless `echo`.
func TestWriterPartialLineRefused(t *testing.T) {
	dir := fencedRepo(t, gatestate.WriterLaunched)
	for _, line := range []string{`echo "unterminated`, `echo "oops; git commit -m x`} {
		refusedWith(t, line, writerCheck(t, writerBash(t, dir, line)), "could not be read with confidence")
	}
}

// TestWriterEditScope pins c-1's edit scope: the task's files, never .dross/.
func TestWriterEditScope(t *testing.T) {
	dir := fencedRepo(t, gatestate.WriterLaunched)
	refusedWith(t, "plan.toml", writerCheck(t, fixtureEdit(t, "writer_pre_edit.json", dir, ".dross/phases/p/plan.toml")), "under .dross/")
	refusedWith(t, "b.go", writerCheck(t, fixtureEdit(t, "writer_pre_edit.json", dir, "b.go")), "is not one of t-1's files")
	admits(t, "a.go", writerCheck(t, fixtureEdit(t, "writer_pre_edit.json", dir, "a.go")))
}

// TestZombieWriterFenced: a writer the record is not bound to, or one whose
// task is no longer in progress, is refused whatever it runs.
func TestZombieWriterFenced(t *testing.T) {
	other := soloRepo(t, "in_progress")
	if err := gatestate.SaveWriter(other, gatestate.Writer{Phase: "p", Task: "t-1", AgentID: "a-later-writer", State: gatestate.WriterLaunched}); err != nil {
		t.Fatal(err)
	}
	refusedWith(t, "unbound agent", writerCheck(t, fixtureEdit(t, "writer_pre_edit.json", other, "a.go")), "is not the one bound")

	failed := fencedRepo(t, gatestate.WriterLaunched)
	put(t, failed, ".dross/phases/p/plan.toml", planWith("failed"))
	refusedWith(t, "failed task", writerCheck(t, fixtureEdit(t, "writer_pre_edit.json", failed, "a.go")), "no longer in progress")
	refusedWith(t, "failed task bash", writerCheck(t, writerBash(t, failed, "git diff")), "no longer in progress")
}

// TestNoInlineTakeover pins c-5: the main loop never edits a writer-owned
// task's code; the way out is the stash and `failed --reason`, which pass.
func TestNoInlineTakeover(t *testing.T) {
	for _, state := range []string{gatestate.WriterLaunched, gatestate.WriterStopped} {
		dir := fencedRepo(t, state)
		res := writerCheck(t, fixtureEdit(t, "main_pre_edit.json", dir, "a.go"))
		refusedWith(t, state+" main-loop edit", res, "git stash push -u -- . ':(exclude).dross'")
		refusedWith(t, state+" main-loop edit", res, "failed --reason")
		admits(t, state+" .dross edit", writerCheck(t, fixtureEdit(t, "main_pre_edit.json", dir, ".dross/x")))
		for _, line := range []string{"git stash push -u -- . ':(exclude).dross'", `dross task status p t-1 failed --reason "writer: red"`} {
			admits(t, state+" "+line, writerCheck(t, bash(t, line, dir)))
		}
	}
}

// TestMainLoopWaitsForWriter pins c-3: while the writer is launched the main
// loop does not run the gates that certify the tree.
func TestMainLoopWaitsForWriter(t *testing.T) {
	launched := fencedRepo(t, gatestate.WriterLaunched)
	stopped := fencedRepo(t, gatestate.WriterStopped)
	for _, line := range []string{"dross test", "dross review context", "git commit -m x", "sh -c 'git commit -m x'"} {
		refusedWith(t, line, writerCheck(t, bash(t, line, launched)), "wait for the writer's completion notice")
		admits(t, "stopped: "+line, writerCheck(t, bash(t, line, stopped)))
	}
}

// TestFenceSilentUnarmed pins c-4: with no writer record, or in pair mode,
// the fence refuses nothing.
func TestFenceSilentUnarmed(t *testing.T) {
	none := soloRepo(t, "in_progress")
	pair := fencedRepo(t, gatestate.WriterLaunched)
	setMode(t, pair, "pair")
	for name, dir := range map[string]string{"no record": none, "pair mode": pair} {
		admits(t, name+" writer commit", writerCheck(t, writerBash(t, dir, "git commit -m x")))
		admits(t, name+" writer edit", writerCheck(t, fixtureEdit(t, "writer_pre_edit.json", dir, "b.go")))
		admits(t, name+" main edit", writerCheck(t, fixtureEdit(t, "main_pre_edit.json", dir, "a.go")))
		admits(t, name+" main test", writerCheck(t, bash(t, "dross test", dir)))
	}
}
