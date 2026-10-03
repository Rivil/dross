package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/treefp"
)

const testsToml = "[runtime]\n  test_command = \"go test ./...\"\n"

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// commitRepo is a committed git repo at dir whose .dross/project.toml holds
// projectToml ("" for a repo with no .dross at all).
func commitRepo(t *testing.T, dir, projectToml string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	for _, kv := range [][2]string{{"user.email", "t@example.com"}, {"user.name", "T"}, {"commit.gpgsign", "false"}, {"gc.auto", "0"}} {
		gitIn(t, dir, "config", kv[0], kv[1])
	}
	if projectToml != "" {
		put(t, dir, ".dross/project.toml", projectToml)
	}
	put(t, dir, "a.go", "package a\n")
	put(t, dir, "b.go", "package b\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	return dir
}

// recordGreen records a full green for dir's work tree as it stands.
func recordGreen(t *testing.T, dir string) string {
	t.Helper()
	tree, err := treefp.WorkingTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := gatestate.SaveGreen(dir, gatestate.Green{Tree: tree, Runner: "local"}); err != nil {
		t.Fatal(err)
	}
	return tree
}

func commitCheck(t *testing.T, line, cwd string) Result {
	t.Helper()
	g, ok := Lookup("commit-green")
	if !ok {
		t.Fatal("commit-green is not registered")
	}
	return Check(bash(t, line, cwd), Env{Home: t.TempDir(), Gates: []Gate{g}})
}

func TestCommitGreenMatch(t *testing.T) {
	dir := commitRepo(t, t.TempDir(), testsToml)
	put(t, dir, "a.go", "package a // changed\n")
	gitIn(t, dir, "add", "a.go")
	recordGreen(t, dir)
	if res := commitCheck(t, "git commit -m x", dir); !res.Allowed() {
		t.Fatalf("a commit of the green tree was refused: %q", res.Text())
	}
	put(t, dir, "a.go", "package a // changed!\n")
	gitIn(t, dir, "add", "a.go")
	res := commitCheck(t, "git commit -m x", dir)
	if res.Allowed() || !strings.Contains(res.Text(), "dross test") || !strings.Contains(res.Text(), "raw `go test`") {
		t.Errorf("a one-byte change after the green: %q, want a refusal naming a full dross test and that a raw go test does not count", res.Text())
	}
}

func TestAddThenCommitUsesReplay(t *testing.T) {
	dir := commitRepo(t, t.TempDir(), testsToml)
	put(t, dir, "b.go", "package b // staged\n")
	gitIn(t, dir, "add", "b.go")
	put(t, dir, "a.go", "package a // changed\n")
	recordGreen(t, dir)
	if res := commitCheck(t, "git add a.go && git commit -m x", dir); !res.Allowed() {
		t.Errorf("git add a.go && git commit was judged without the add: %q", res.Text())
	}
	// Judged against the pre-add index (b.go only), the same commit differs
	// from the green.
	if res := commitCheck(t, "git commit -m x", dir); res.Allowed() {
		t.Error("a commit of b.go alone was admitted against a green that also holds a.go")
	}
}

func TestHeredocCommitAfterAdd(t *testing.T) {
	dir := commitRepo(t, t.TempDir(), testsToml)
	// b.go is already staged, so a judgement that skipped the add (index
	// without a.go) would differ from the green rather than pass as empty.
	put(t, dir, "b.go", "package b // staged\n")
	gitIn(t, dir, "add", "b.go")
	put(t, dir, "a.go", "package a // changed\n")
	recordGreen(t, dir)
	line := "git add a.go && git commit -m \"$(cat <<'EOF'\nfeat(x): change a (and more)\n\nCo-Authored-By: someone\nEOF\n)\""
	if res := commitCheck(t, line, dir); !res.Allowed() {
		t.Errorf("Claude Code's own commit shape was refused: %q", res.Text())
	}
}

func TestCommitUnpredictablePredecessor(t *testing.T) {
	dir := commitRepo(t, t.TempDir(), testsToml)
	recordGreen(t, dir)
	res := commitCheck(t, "sed -i s/a/b/ a.go && git add a.go && git commit -m x", dir)
	if res.Allowed() || !strings.Contains(res.Text(), "`sed` runs ahead") {
		t.Errorf("sed ahead of the commit: %q, want a refusal naming it", res.Text())
	}
}

func TestCommitUngated(t *testing.T) {
	dir := commitRepo(t, t.TempDir(), testsToml)
	put(t, dir, ".dross/phases/p/plan.toml", "x = 1\n")
	gitIn(t, dir, "add", ".dross/phases/p/plan.toml")
	if res := commitCheck(t, "git commit -m chore", dir); !res.Allowed() {
		t.Errorf("a .dross-only commit with no green was refused: %q", res.Text())
	}
	gitIn(t, dir, "commit", "-q", "-m", "plan")
	put(t, dir, ".dross/project.toml", testsToml+"# edited\n")
	if res := commitCheck(t, "git commit -am chore", dir); !res.Allowed() {
		t.Errorf("commit -am with only tracked .dross edits was refused: %q", res.Text())
	}
	gitIn(t, dir, "checkout", "--", ".dross/project.toml")
	put(t, dir, "a.go", "package a // code\n")
	gitIn(t, dir, "add", "a.go")
	if res := commitCheck(t, "git commit -m code", dir); res.Allowed() {
		t.Error("a staged code file with no green.json was admitted")
	}
}

func TestCommitDir(t *testing.T) {
	base := t.TempDir()
	repo := commitRepo(t, filepath.Join(base, "repo"), testsToml)
	recordGreen(t, repo)
	other := commitRepo(t, filepath.Join(base, "other"), testsToml)
	put(t, other, "a.go", "package a // other\n")
	gitIn(t, other, "add", "a.go")
	for _, line := range []string{"git -C ../other commit -m x", "cd ../other && git commit -m x"} {
		if res := commitCheck(t, line, repo); res.Allowed() {
			t.Errorf("%q judged the cwd repo (which has a green) instead of ../other", line)
		}
	}

	var argv [][]string
	prev := gitrun.ArgvRecorder
	gitrun.ArgvRecorder = func(a []string) { argv = append(argv, a) }
	t.Cleanup(func() { gitrun.ArgvRecorder = prev })
	plain := commitRepo(t, filepath.Join(base, "plain"), "")
	argv = nil
	put(t, plain, "a.go", "package a // plain\n")
	if res := commitCheck(t, "cd ../plain && git commit -am x", repo); !res.Allowed() {
		t.Errorf("a commit in a repo with no .dross was gated: %q", res.Text())
	}
	if len(argv) != 0 {
		t.Errorf("judging a non-dross repo's commit spawned git: %q", argv)
	}
}

func TestCommitUnjudgeableForms(t *testing.T) {
	dir := commitRepo(t, t.TempDir(), testsToml)
	recordGreen(t, dir)
	for _, line := range []string{"git commit a.go -m x", "git commit -p", "git commit -o -m x", "git commit --include -m x", "git add -p && git commit -m x", "git commit -m x -- a.go"} {
		res := commitCheck(t, line, dir)
		if res.Allowed() || !strings.Contains(res.Text(), "stage with `git add`, then run a plain `git commit`") {
			t.Errorf("%q: %q, want the stage-plainly refusal", line, res.Text())
		}
	}
	for _, line := range []string{"git commit -am x", "git commit -m 'a.go is fixed'", "git commit --no-verify -F msg.txt", "git commit -S -m x"} {
		if res := commitCheck(t, line, dir); strings.Contains(res.Text(), "stage with") {
			t.Errorf("%q was read as an unjudgeable form: %q", line, res.Text())
		}
	}
}

func TestCommitClaims(t *testing.T) {
	g, _ := Lookup("commit-green")
	for _, c := range []struct {
		line string
		want bool
	}{
		{"git commit -m x", true}, {"FOO=1 git -C x commit", true}, {"git -c user.name=x commit", true},
		{"git commit-tree HEAD^{tree}", false}, {"echo git commit", false}, {"git log --grep commit", false},
		{"ls -la", false}, {`git commit -m "half`, true}, {`echo "half`, false},
	} {
		p, err := Decode(bash(t, c.line, "/w"))
		if err != nil {
			t.Fatal(err)
		}
		if got := g.Claims(&Call{Payload: p, Lists: Defaults()}); got != c.want {
			t.Errorf("Claims(%q) = %v, want %v", c.line, got, c.want)
		}
	}
	var argv [][]string
	prev := gitrun.ArgvRecorder
	gitrun.ArgvRecorder = func(a []string) { argv = append(argv, a) }
	t.Cleanup(func() { gitrun.ArgvRecorder = prev })
	if res := commitCheck(t, "ls -la", droot(t)); !res.Allowed() || len(argv) != 0 {
		t.Errorf("ls -la: %q, git argv %q; want silence and no git", res.Text(), argv)
	}
}

func TestDivergedCandidateNamesPaths(t *testing.T) {
	dir := commitRepo(t, t.TempDir(), testsToml)
	put(t, dir, "a.go", "package a // changed\n")
	put(t, dir, "new.go", "package n\n")
	recordGreen(t, dir)
	gitIn(t, dir, "add", "a.go")
	res := commitCheck(t, "git commit -m x", dir)
	if res.Allowed() || !strings.Contains(res.Text(), "new.go") || !strings.Contains(res.Text(), "stage or clean them") {
		t.Errorf("%q, want a refusal naming new.go with \"stage or clean them\"", res.Text())
	}
	if strings.Contains(res.Text(), "run a bare `dross test`") {
		t.Errorf("the refusal sends the agent back to dross test, which cannot converge: %q", res.Text())
	}
}

func TestCommitPartialPosture(t *testing.T) {
	dir := commitRepo(t, t.TempDir(), testsToml)
	recordGreen(t, dir)
	res := commitCheck(t, `git commit -m "half`, dir)
	if res.Allowed() || !strings.Contains(res.Text(), "unterminated double quote") {
		t.Errorf("a partial commit line: %q, want a refusal naming the parse problem", res.Text())
	}
}

func TestCommitNoTestsCarveOut(t *testing.T) {
	quiet := commitRepo(t, t.TempDir(), "[project]\n  name = \"x\"\n")
	put(t, quiet, "a.go", "package a // changed\n")
	for _, line := range []string{"git commit -am x", "sed -i x a.go && git commit -am x", "git commit -p"} {
		if res := commitCheck(t, line, quiet); !res.Allowed() {
			t.Errorf("%q in a repo with no tests was refused: %q", line, res.Text())
		}
	}
	lanes := commitRepo(t, t.TempDir(), "[[runtime.test_lane]]\n  name = \"go\"\n  match = [\"**\"]\n  command = \"go test ./...\"\n")
	put(t, lanes, "a.go", "package a // changed\n")
	if res := commitCheck(t, "git commit -am x", lanes); res.Allowed() {
		t.Error("a lanes-only repo (no test_command) was not gated")
	}
}

func TestCommitClosedPosture(t *testing.T) {
	dir := commitRepo(t, t.TempDir(), testsToml)
	recordGreen(t, dir)
	if res := commitCheck(t, "git add nope.go && git commit -m x", dir); res.Allowed() || !strings.Contains(res.Text(), "could not judge") {
		t.Errorf("a failing git add replay: %q, want a refusal naming the error", res.Text())
	}

	corrupt := commitRepo(t, t.TempDir(), testsToml)
	recordGreen(t, corrupt)
	if err := os.WriteFile(filepath.Join(corrupt, ".git", "index"), []byte("not an index"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := commitCheck(t, "git commit -m x", corrupt); res.Allowed() || !strings.Contains(res.Text(), "could not judge") {
		t.Errorf("a corrupt index: %q, want a refusal naming the error", res.Text())
	}

	bad := commitRepo(t, t.TempDir(), testsToml)
	put(t, bad, "a.go", "package a // changed\n")
	gitIn(t, bad, "add", "a.go")
	put(t, bad, ".dross/gate/green.json", `{"tree":`)
	if res := commitCheck(t, "git commit -m x", bad); res.Allowed() || !strings.Contains(res.Text(), "green.json") {
		t.Errorf("an unreadable green.json: %q, want a refusal naming it", res.Text())
	}
}
