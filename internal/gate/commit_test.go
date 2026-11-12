package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/localstore"
	"github.com/Rivil/dross/internal/shellscan"
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

// TestCommitLongAll: `--all` is `-a` spelled out, so the candidate includes
// the unstaged tracked edit. Read as a plain commit it would be the index
// alone — and differ from a green taken over the edited tree.
func TestCommitLongAll(t *testing.T) {
	dir := commitRepo(t, t.TempDir(), testsToml)
	put(t, dir, "a.go", "package a // unstaged\n")
	if res := commitCheck(t, "git commit --all -m x", dir); res.Allowed() {
		t.Error("git commit --all with an unstaged code edit and no green was admitted")
	}
	recordGreen(t, dir)
	if res := commitCheck(t, "git commit --all -m x", dir); !res.Allowed() {
		t.Errorf("git commit --all of the green tree was refused: %q", res.Text())
	}
}

// TestCommitMixedDrossAndCode: the .dross carve-out needs every staged path
// under .dross/ — one code file among them puts the commit back under the gate.
func TestCommitMixedDrossAndCode(t *testing.T) {
	dir := commitRepo(t, t.TempDir(), testsToml)
	put(t, dir, ".dross/phases/p/plan.toml", "x = 1\n")
	put(t, dir, "a.go", "package a // code\n")
	gitIn(t, dir, "add", ".dross/phases/p/plan.toml", "a.go")
	if res := commitCheck(t, "git commit -m x", dir); res.Allowed() {
		t.Error("a commit staging a code file beside a .dross file, with no green, was admitted")
	}
}

// TestCommitAt: the shared reading of where a line's commit is — -1 when it
// has none, else its index among the line's commands.
func TestCommitAt(t *testing.T) {
	if at, _ := commitAt(shellscan.Scan("ls -la && echo git commit", shellscan.Options{Dir: "/w"})); at != -1 {
		t.Errorf("a line with no commit: commitAt = %d, want -1", at)
	}
	at, g := commitAt(shellscan.Scan("git add a.go && git commit -m x", shellscan.Options{Dir: "/w"}))
	if at != 1 || g.Sub != "commit" {
		t.Errorf("git add && git commit: commitAt = %d (%q), want 1 (commit)", at, g.Sub)
	}
}

// exemptToml declares the exempt globs under testsToml's [runtime] table.
const exemptToml = "  test_exempt = [\"docs/**\", \"*.json\"]\n"

// vouchLane is one vouching lane, as t-3's rules require it.
func vouchLane(name, match, command string) string {
	return "[[runtime.test_lane]]\n  name = \"" + name + "\"\n  match = [\"" + match + "\"]\n  command = \"" + command +
		"\"\n  selector = \"path\"\n  empty_exit = [3]\n  relate = \"npx vitest related\"\n  vouch = true\n"
}

// webLane vouches for src/** and is the lane most scoped tests run under.
var webLane = vouchLane("web", "src/**", "npx vitest run")

// grantRepo is a human's `dross trust` over project.toml as it stands: every
// lane, and the exempt list when there is one.
func grantRepo(t *testing.T, dir string) {
	t.Helper()
	p, err := declaredProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := localstore.GrantStore(filepath.Join(dir, ".dross"))
	for _, l := range p.Runtime.TestLane {
		if err := consent.GrantLaneConsent(store, l.Name, consent.LaneLine(l)); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.Runtime.TestExempt) > 0 {
		if err := consent.GrantExemptConsent(store, p.Runtime.TestExempt); err != nil {
			t.Fatal(err)
		}
	}
}

// vouchRepo is a committed repo holding src/a.ts, src/b.ts and docs/x.md, with
// projectToml granted in full.
func vouchRepo(t *testing.T, projectToml string) string {
	t.Helper()
	dir := commitRepo(t, t.TempDir(), projectToml)
	put(t, dir, "src/a.ts", "a\n")
	put(t, dir, "src/b.ts", "b\n")
	put(t, dir, "docs/x.md", "x\n")
	gitIn(t, dir, "add", "src", "docs")
	gitIn(t, dir, "commit", "-q", "-m", "seed")
	grantRepo(t, dir)
	return dir
}

// stage writes body to each path and stages it.
func stage(t *testing.T, dir, body string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		put(t, dir, p, body)
		gitIn(t, dir, "add", p)
	}
}

// laneFingerprints is what a run under dir's declared lanes records. A repo with
// no lanes gets a placeholder, because a record naming none cannot be saved.
func laneFingerprints(t *testing.T, dir string, names ...string) []string {
	t.Helper()
	p, err := declaredProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range p.Runtime.TestLane {
		if len(names) == 0 || slices.Contains(names, l.Name) {
			out = append(out, consent.Fingerprint(consent.LaneLine(l)))
		}
	}
	if len(out) == 0 {
		return []string{"no-lane"}
	}
	return out
}

// recordScopedOn records a scoped green for dir's work tree as it stands, run on
// the given lane fingerprints.
func recordScopedOn(t *testing.T, dir string, lanes []string, covered ...string) {
	t.Helper()
	tree, err := treefp.WorkingTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec := gatestate.Scoped{Tree: tree, Covered: covered, Lanes: lanes, At: time.Now().UTC(), Runner: "local"}
	if err := gatestate.SaveScoped(dir, rec); err != nil {
		t.Fatal(err)
	}
}

// recordScoped records a scoped green run on every declared lane.
func recordScoped(t *testing.T, dir string, covered ...string) {
	t.Helper()
	recordScopedOn(t, dir, laneFingerprints(t, dir), covered...)
}

// commitAdmitted fails unless a plain commit in dir is admitted.
func commitAdmitted(t *testing.T, dir string) {
	t.Helper()
	if res := commitCheck(t, "git commit -m x", dir); !res.Allowed() {
		t.Errorf("the commit was refused: %q", res.Text())
	}
}

// commitRefused fails unless a plain commit in dir is refused with text holding
// every string in named and none in unnamed, and returns the text.
func commitRefused(t *testing.T, dir string, named, unnamed []string) string {
	t.Helper()
	res := commitCheck(t, "git commit -m x", dir)
	if res.Allowed() {
		t.Fatalf("the commit was admitted, want a refusal naming %q", named)
	}
	text := res.Text()
	for _, n := range named {
		if !strings.Contains(text, n) {
			t.Errorf("the refusal does not name %q: %q", n, text)
		}
	}
	for _, n := range unnamed {
		if strings.Contains(text, n) {
			t.Errorf("the refusal names %q: %q", n, text)
		}
	}
	return text
}

// refusedAsToday pins c-4: the refusal is judgeGreen's own, and judgeGreen says
// rule, so the comparison cannot pass by both sides having moved.
func refusedAsToday(t *testing.T, dir, rule string) {
	t.Helper()
	res := commitCheck(t, "git commit -m x", dir)
	snap, err := treefp.Candidate(dir, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	want, err := judgeGreen(shellscan.GitCall{Dir: dir}, snap)
	if err != nil || want == nil {
		t.Fatalf("judgeGreen: %v, %v; want a refusal", want, err)
	}
	if want.Rule != rule {
		t.Errorf("judgeGreen says %q, want %q", want.Rule, rule)
	}
	if len(res.Refusals) != 1 || res.Refusals[0].Rule != want.Rule || res.Refusals[0].Remedy != want.Remedy {
		t.Errorf("refusals %+v, want exactly judgeGreen's %+v", res.Refusals, *want)
	}
}

// TestCommitWithoutVouchingLaneIsToday pins c-4 and scoped_gate_optin: a matching
// scoped.json and a granted exempt list change nothing until a lane vouches.
func TestCommitWithoutVouchingLaneIsToday(t *testing.T) {
	plainLane := "[[runtime.test_lane]]\n  name = \"web\"\n  match = [\"**\"]\n  command = \"npx vitest run\"\n"
	for name, lanes := range map[string]string{"no lanes": "", "lanes, none vouching": plainLane} {
		for _, path := range []string{"src/a.ts", "docs/x.md"} {
			t.Run(name+" "+path, func(t *testing.T) {
				dir := vouchRepo(t, testsToml+exemptToml+lanes)
				stage(t, dir, "changed\n", path)
				recordScoped(t, dir, path)

				refusedAsToday(t, dir, "no full green `dross test` is recorded for this repo, so nothing vouches for the tree this commit records")
				if err := gatestate.SaveGreen(dir, gatestate.Green{Tree: "another-tree", Runner: "local"}); err != nil {
					t.Fatal(err)
				}
				refusedAsToday(t, dir, "the tree changed since the last full green `dross test`, so the commit would record code no full run has measured")

				recordGreen(t, dir)
				commitAdmitted(t, dir)
			})
		}
	}
}

// TestCommitNoTestRepoSpawnsNoGit pins no_test_repo through the new routing:
// deciding which rule applies must not cost a repo with no tests a git spawn.
func TestCommitNoTestRepoSpawnsNoGit(t *testing.T) {
	quiet := commitRepo(t, t.TempDir(), "[project]\n  name = \"x\"\n")
	put(t, quiet, "a.go", "package a // changed\n")
	var argv [][]string
	prev := gitrun.ArgvRecorder
	gitrun.ArgvRecorder = func(a []string) { argv = append(argv, a) }
	t.Cleanup(func() { gitrun.ArgvRecorder = prev })
	if res := commitCheck(t, "git commit -am x", quiet); !res.Allowed() {
		t.Errorf("a repo with no tests was refused: %q", res.Text())
	}
	if len(argv) != 0 {
		t.Errorf("judging a repo with no tests spawned git: %q", argv)
	}
}

// TestScopedGreenAdmits pins c-2: every changed path covered on the exact tree
// admits with no green.json, from the repo root and from below it.
func TestScopedGreenAdmits(t *testing.T) {
	dir := vouchRepo(t, testsToml+webLane)
	stage(t, dir, "changed\n", "src/a.ts", "src/b.ts")
	recordScoped(t, dir, "src/a.ts", "src/b.ts")
	commitAdmitted(t, dir)
	commitAdmitted(t, filepath.Join(dir, "src"))
	if g, err := gatestate.LoadGreen(dir); err != nil || g != nil {
		t.Errorf("green.json = %+v, %v; the admission must not rest on one", g, err)
	}
}

// TestScopedRefusalNamesOnlyUncovered pins c-3 and c-9.
func TestScopedRefusalNamesOnlyUncovered(t *testing.T) {
	dir := vouchRepo(t, testsToml+webLane)
	stage(t, dir, "changed\n", "src/a.ts", "src/b.ts")
	recordScoped(t, dir, "src/a.ts")
	commitRefused(t, dir, []string{"src/b.ts"}, []string{"src/a.ts"})
}

// TestNoScopedRecordNamesEveryPath: a missing record loses no names.
func TestNoScopedRecordNamesEveryPath(t *testing.T) {
	dir := vouchRepo(t, testsToml+webLane)
	stage(t, dir, "changed\n", "src/a.ts", "src/b.ts")
	commitRefused(t, dir, []string{"src/a.ts", "src/b.ts", "dross test --files"}, nil)
}

// TestUnreadableScopedRecordRefuses: a record that does not decode is no
// admission, and the refusal says which file.
func TestUnreadableScopedRecordRefuses(t *testing.T) {
	dir := vouchRepo(t, testsToml+webLane)
	stage(t, dir, "changed\n", "src/a.ts")
	put(t, dir, ".dross/gate/scoped.json", `{"tree":`)
	commitRefused(t, dir, []string{"could not judge", "scoped.json"}, nil)
}

// TestStaleCoverRefuses: a covered path re-edited after the run is named, and
// the remedy is a re-run, not staging.
func TestStaleCoverRefuses(t *testing.T) {
	dir := vouchRepo(t, testsToml+webLane)
	stage(t, dir, "changed\n", "src/a.ts")
	recordScoped(t, dir, "src/a.ts")
	stage(t, dir, "changed again\n", "src/a.ts")
	commitRefused(t, dir, []string{"src/a.ts", "dross test --files"}, []string{"stage or clean"})
}

// TestScopedGreenNeedsTheExactTree: an exempt path added after the run moves the
// tree, so the record no longer speaks for the commit.
func TestScopedGreenNeedsTheExactTree(t *testing.T) {
	dir := vouchRepo(t, testsToml+exemptToml+webLane)
	stage(t, dir, "changed\n", "src/a.ts")
	recordScoped(t, dir, "src/a.ts")
	stage(t, dir, "changed\n", "docs/x.md")
	commitRefused(t, dir, []string{"src/a.ts"}, []string{"docs/x.md"})
}

// TestScopedPartialCommitIsStageOrClean: re-running cannot converge on a commit
// that leaves part of the tested work tree out, so it takes the staging remedy.
func TestScopedPartialCommitIsStageOrClean(t *testing.T) {
	dir := vouchRepo(t, testsToml+webLane)
	stage(t, dir, "changed\n", "src/a.ts")
	put(t, dir, "src/b.ts", "unstaged\n")
	recordScoped(t, dir, "src/a.ts", "src/b.ts")
	commitRefused(t, dir,
		[]string{"the last scoped green ran on the work tree", "src/b.ts", "stage or clean them"},
		[]string{"dross test --files", "bare `dross test`"})
}

// TestFullGreenPartialCommitIsStageOrClean: the same holds for a full green in a
// vouching repo, where a re-run would record the same tree again.
func TestFullGreenPartialCommitIsStageOrClean(t *testing.T) {
	dir := vouchRepo(t, testsToml+webLane)
	stage(t, dir, "changed\n", "src/a.ts")
	put(t, dir, "src/b.ts", "unstaged\n")
	recordGreen(t, dir)
	commitRefused(t, dir, []string{"the last full green ran on the work tree", "src/b.ts", "stage or clean them"}, nil)
}

// TestBuildConfigNeverRidesAScopedGreen: listed in Covered, matched by a lane
// and matched by a granted exempt glob, package.json still needs a full run.
func TestBuildConfigNeverRidesAScopedGreen(t *testing.T) {
	dir := vouchRepo(t, testsToml+exemptToml+vouchLane("web", "**", "npx vitest run"))
	stage(t, dir, "{}\n", "package.json")
	recordScoped(t, dir, "package.json")
	commitRefused(t, dir, []string{"package.json", "bare `dross test`"}, nil)
	recordGreen(t, dir)
	commitAdmitted(t, dir)
}

// TestDeletionsAreUncovered: a deletion has nothing a test can relate to, and a
// rename is a deletion of its old side.
func TestDeletionsAreUncovered(t *testing.T) {
	t.Run("deletion", func(t *testing.T) {
		dir := vouchRepo(t, testsToml+webLane)
		stage(t, dir, "changed\n", "src/a.ts")
		gitIn(t, dir, "rm", "-q", "src/b.ts")
		recordScoped(t, dir, "src/a.ts")
		commitRefused(t, dir, []string{"src/b.ts"}, []string{"src/a.ts"})
	})
	t.Run("rename", func(t *testing.T) {
		dir := vouchRepo(t, testsToml+webLane)
		gitIn(t, dir, "mv", "src/b.ts", "src/c.ts")
		recordScoped(t, dir, "src/c.ts")
		commitRefused(t, dir, []string{"src/b.ts"}, []string{"src/c.ts"})
	})
}

// TestLaneEditStopsVouching: project.toml sits outside the tree, so each edit
// here leaves the fingerprint unmoved and only the lane check can refuse.
func TestLaneEditStopsVouching(t *testing.T) {
	edited := vouchLane("web", "src/**", "npx vitest run --changed")
	for name, c := range map[string]struct {
		toml    string
		retrust bool
	}{
		"command edited":              {testsToml + edited, false},
		"command edited, re-trusted":  {testsToml + edited, true},
		"match narrowed":              {testsToml + vouchLane("web", "lib/**", "npx vitest run"), false},
		"another lane took the match": {testsToml + vouchLane("web", "lib/**", "npx vitest run") + vouchLane("api", "src/**", "npx vitest run --api"), true},
	} {
		t.Run(name, func(t *testing.T) {
			dir := vouchRepo(t, testsToml+webLane)
			stage(t, dir, "changed\n", "src/a.ts")
			recordScoped(t, dir, "src/a.ts")
			commitAdmitted(t, dir)

			put(t, dir, ".dross/project.toml", c.toml)
			if c.retrust {
				grantRepo(t, dir)
			}
			commitRefused(t, dir, []string{"src/a.ts"}, nil)
		})
	}
}

// TestUngrantedLaneDoesNotVouch: the record's fingerprint matches, but this
// machine never trusted the lane.
func TestUngrantedLaneDoesNotVouch(t *testing.T) {
	dir := commitRepo(t, t.TempDir(), testsToml+webLane)
	stage(t, dir, "changed\n", "src/a.ts")
	recordScoped(t, dir, "src/a.ts")
	commitRefused(t, dir, []string{"src/a.ts"}, nil)
}

// TestOneStaleLaneSinksTheRecord: Covered does not say which lane reached a path,
// so src/a.ts cannot stand on web once api, which also ran, stops vouching.
func TestOneStaleLaneSinksTheRecord(t *testing.T) {
	api := vouchLane("api", "lib/**", "npx vitest run --api")
	dir := vouchRepo(t, testsToml+webLane+api)
	stage(t, dir, "changed\n", "src/a.ts")
	recordScoped(t, dir, "src/a.ts")
	commitAdmitted(t, dir)

	put(t, dir, ".dross/project.toml", testsToml+webLane+vouchLane("api", "lib/**", "npx vitest run --edited"))
	grantRepo(t, dir)
	commitRefused(t, dir, []string{"src/a.ts"}, nil)
}

// TestLaneThatDidNotRunVouchesNothing: lib/c.ts matches a granted vouching lane,
// but only web ran, so no lane that ran reaches it.
func TestLaneThatDidNotRunVouchesNothing(t *testing.T) {
	dir := vouchRepo(t, testsToml+webLane+vouchLane("api", "lib/**", "npx vitest run --api"))
	stage(t, dir, "changed\n", "lib/c.ts")
	recordScopedOn(t, dir, laneFingerprints(t, dir, "web"), "lib/c.ts")
	commitRefused(t, dir, []string{"lib/c.ts"}, nil)
}

// TestBookkeepingIsNotCode: .dross/ needs no cover and is never named.
func TestBookkeepingIsNotCode(t *testing.T) {
	dir := vouchRepo(t, testsToml+webLane)
	stage(t, dir, "x = 1\n", ".dross/phases/p/plan.toml")
	stage(t, dir, "changed\n", "src/a.ts")
	recordScoped(t, dir, "src/a.ts")
	commitAdmitted(t, dir)

	stage(t, dir, "changed\n", "src/b.ts")
	recordScoped(t, dir, "src/a.ts")
	commitRefused(t, dir, []string{"src/b.ts"}, []string{".dross/phases"})
}

// TestExemptPathsUnderAGrant pins uncovered_paths: a current grant admits exempt
// paths alone and beside a covered path, with no record for the first.
func TestExemptPathsUnderAGrant(t *testing.T) {
	dir := vouchRepo(t, testsToml+exemptToml+webLane)
	stage(t, dir, "changed\n", "docs/x.md")
	commitAdmitted(t, dir)
	if s, err := gatestate.LoadScoped(dir); err != nil || s != nil {
		t.Errorf("scoped.json = %+v, %v; the admission must not rest on one", s, err)
	}

	// docs/x.md is not in Covered, so only the grant reaches it.
	stage(t, dir, "changed\n", "src/a.ts")
	recordScoped(t, dir, "src/a.ts")
	commitAdmitted(t, dir)

	stage(t, dir, "changed\n", "src/b.ts")
	commitRefused(t, dir, []string{"src/a.ts", "src/b.ts"}, []string{"docs/x.md"})
}

// TestExemptWithoutACurrentGrant: the list is tracked, so an edit to it stops
// counting until a human says so, and no test run is offered as the way out.
func TestExemptWithoutACurrentGrant(t *testing.T) {
	human := []string{"docs/x.md", "`dross trust --test-exempt`", "a human"}
	notARun := []string{"bare `dross test`", "dross test --files"}

	t.Run("stale", func(t *testing.T) {
		dir := vouchRepo(t, testsToml+exemptToml+webLane)
		put(t, dir, ".dross/project.toml", testsToml+"  test_exempt = [\"docs/**\", \"*.json\", \"*.txt\"]\n"+webLane)
		stage(t, dir, "changed\n", "docs/x.md")
		commitRefused(t, dir, append(human, "CHANGED"), notARun)
	})
	t.Run("never granted", func(t *testing.T) {
		dir := vouchRepo(t, testsToml+webLane)
		put(t, dir, ".dross/project.toml", testsToml+exemptToml+webLane)
		stage(t, dir, "changed\n", "docs/x.md")
		commitRefused(t, dir, human, notARun)
	})
	t.Run("beside a covered path", func(t *testing.T) {
		dir := vouchRepo(t, testsToml+webLane)
		put(t, dir, ".dross/project.toml", testsToml+exemptToml+webLane)
		stage(t, dir, "changed\n", "docs/x.md", "src/a.ts")
		recordScoped(t, dir, "src/a.ts")
		commitRefused(t, dir, human, append(notARun, "src/a.ts"))
	})
	t.Run("beside an uncovered path", func(t *testing.T) {
		dir := vouchRepo(t, testsToml+webLane)
		put(t, dir, ".dross/project.toml", testsToml+exemptToml+webLane)
		stage(t, dir, "changed\n", "docs/x.md", "src/a.ts")
		commitRefused(t, dir, []string{"docs/x.md", "src/a.ts", "dross test --files"}, []string{"dross trust"})
	})
}

// TestFullGreenBeatsScoped: scoped.json has no say over a full green on the
// candidate tree, even one that does not decode.
func TestFullGreenBeatsScoped(t *testing.T) {
	dir := vouchRepo(t, testsToml+webLane)
	stage(t, dir, "changed\n", "src/a.ts", "src/b.ts")
	recordGreen(t, dir)
	recordScoped(t, dir, "src/a.ts")
	commitAdmitted(t, dir)
	put(t, dir, ".dross/gate/scoped.json", `{"tree":`)
	commitAdmitted(t, dir)
}

// TestTrackedGrantStoreRefuses: a committed local.toml is a repo granting
// itself, so it is refused unread on both the exempt and the lane read.
func TestTrackedGrantStoreRefuses(t *testing.T) {
	want := []string{"could not judge", "git reports it tracked"}
	t.Run("exempt", func(t *testing.T) {
		dir := vouchRepo(t, testsToml+exemptToml+webLane)
		stage(t, dir, "changed\n", "docs/x.md")
		gitIn(t, dir, "add", ".dross/local.toml")
		commitRefused(t, dir, want, nil)
	})
	t.Run("lane", func(t *testing.T) {
		dir := vouchRepo(t, testsToml+webLane)
		stage(t, dir, "changed\n", "src/a.ts")
		recordScoped(t, dir, "src/a.ts")
		gitIn(t, dir, "add", ".dross/local.toml")
		commitRefused(t, dir, want, nil)
	})
}
