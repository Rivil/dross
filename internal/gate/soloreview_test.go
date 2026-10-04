package gate

import (
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/review"
	"github.com/Rivil/dross/internal/treefp"
)

func soloCheck(t *testing.T, line, dir string) Result {
	t.Helper()
	g, ok := Lookup("solo-review")
	if !ok {
		t.Fatal("solo-review is not registered")
	}
	return Check(bash(t, line, dir), Env{Home: t.TempDir(), Gates: []Gate{g}})
}

// stagedSolo is a solo repo with t-1 in progress and a.go changed and staged;
// it returns the repo and the tree the commit would record.
func stagedSolo(t *testing.T) (string, string) {
	t.Helper()
	dir := soloRepo(t, "in_progress")
	put(t, dir, "a.go", "package a // reviewed\n")
	gitIn(t, dir, "add", "a.go")
	snap, err := treefp.Candidate(dir, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	return dir, snap.Tree
}

func saveScopeLedger(t *testing.T, dir string, rounds ...review.Round) {
	t.Helper()
	scope, err := ArmedScope(dir)
	if err != nil || scope == nil {
		t.Fatalf("scope = %+v, %v", scope, err)
	}
	if err := gatestate.SaveReview(dir, gatestate.Review{Kind: scope.Kind, Phase: scope.Phase, Task: scope.Task, Attempt: scope.Attempt, Rounds: rounds}); err != nil {
		t.Fatal(err)
	}
}

func TestSoloReviewGate(t *testing.T) {
	dir, tree := stagedSolo(t)

	res := soloCheck(t, "git commit -m x", dir)
	if res.Allowed() || !strings.Contains(res.Text(), "dross review context") || !strings.Contains(res.Text(), review.ReviewerAgent) {
		t.Errorf("no review recorded: %q, want a refusal naming `dross review context` and %s", res.Text(), review.ReviewerAgent)
	}

	saveScopeLedger(t, dir, review.Round{Outcome: review.OutcomePass, Tree: tree})
	if res := soloCheck(t, "git commit -m x", dir); !res.Allowed() {
		t.Errorf("a pass for the candidate tree was refused: %q", res.Text())
	}

	put(t, dir, "a.go", "package a // reviewed, then edited\n")
	gitIn(t, dir, "add", "a.go")
	if res := soloCheck(t, "git commit -m x", dir); res.Allowed() || !strings.Contains(res.Text(), "tree changed since the review") {
		t.Errorf("a pass for an older tree: %q, want \"tree changed since the review\"", res.Text())
	}

	dir, tree = stagedSolo(t)
	saveScopeLedger(t, dir, review.Round{Outcome: review.OutcomeBlock})
	if res := soloCheck(t, "git commit -m x", dir); res.Allowed() || !strings.Contains(res.Text(), "one fix round") {
		t.Errorf("blocked: %q, want the fix-round remedy", res.Text())
	}
	saveScopeLedger(t, dir, review.Round{Outcome: review.OutcomeBlock}, review.Round{Outcome: review.OutcomeBlock})
	if res := soloCheck(t, "git commit -m x", dir); res.Allowed() || !strings.Contains(res.Text(), "dross task status p t-1 failed") || !strings.Contains(res.Text(), "still blocked after the one fix round") {
		t.Errorf("exhausted: %q, want the failed-task remedy and the cause", res.Text())
	}
	saveScopeLedger(t, dir, review.Round{Outcome: review.OutcomeUnavailable, Cause: "VERDICT-GARBLED"}, review.Round{Outcome: review.OutcomePass, Tree: tree})
	if res := soloCheck(t, "git commit -m x", dir); res.Allowed() || !strings.Contains(res.Text(), "VERDICT-GARBLED") || !strings.Contains(res.Text(), "failed") {
		t.Errorf("unavailable: %q, want the failed-task remedy repeating the cause", res.Text())
	}

	if err := gatestate.SaveReview(dir, gatestate.Review{Kind: review.KindTask, Phase: "p", Task: "t-2", Attempt: "x", Rounds: []review.Round{{Outcome: review.OutcomePass, Tree: tree}}}); err != nil {
		t.Fatal(err)
	}
	if res := soloCheck(t, "git commit -m x", dir); res.Allowed() {
		t.Error("a pass recorded for t-2 admitted t-1's commit")
	}
}

func treefpArgv(argv [][]string) [][]string {
	var out [][]string
	for _, a := range argv {
		for _, w := range a {
			if w == "write-tree" || w == "read-tree" || w == "diff-tree" {
				out = append(out, a)
				break
			}
		}
	}
	return out
}

func TestSoloReviewSilent(t *testing.T) {
	dir, _ := stagedSolo(t)
	gitIn(t, dir, "reset", "-q", "a.go")
	put(t, dir, ".dross/notes.md", "bookkeeping\n")
	if res := soloCheck(t, "git add .dross/notes.md && git commit -m x", dir); !res.Allowed() {
		t.Errorf("a .dross/-only commit while armed was refused: %q", res.Text())
	}

	var argv [][]string
	prev := gitrun.ArgvRecorder
	gitrun.ArgvRecorder = func(a []string) { argv = append(argv, a) }
	t.Cleanup(func() { gitrun.ArgvRecorder = prev })

	pair, _ := stagedSolo(t)
	setMode(t, pair, "pair")
	idle, _ := stagedSolo(t)
	put(t, idle, ".dross/phases/p/plan.toml", planWith("done"))
	off, _ := stagedSolo(t)
	gitIn(t, off, "checkout", "-q", "main")
	for name, d := range map[string]string{"pair": pair, "no task in progress": idle, "HEAD off phase/p": off} {
		argv = nil
		if res := soloCheck(t, "git commit -m x", d); !res.Allowed() {
			t.Errorf("%s: refused %q", name, res.Text())
		}
		if got := treefpArgv(argv); len(got) != 0 {
			t.Errorf("%s: judging an unarmed commit spawned treefp git: %q", name, got)
		}
	}
}

func TestSoloReviewQuick(t *testing.T) {
	dir := soloRepo(t, "")
	head := headSHA(t, dir)
	if err := gatestate.SaveQuick(dir, gatestate.Quick{Mode: "solo", Description: "fix x", Head: head, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "a.go", "package a // quick\n")
	gitIn(t, dir, "add", "a.go")
	snap, err := treefp.Candidate(dir, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if res := soloCheck(t, "git commit -m x", dir); res.Allowed() {
		t.Error("a solo quick with no review committed")
	}
	if err := gatestate.SaveReview(dir, gatestate.Review{Kind: review.KindQuick, Attempt: "quick@another-attempt", Rounds: []review.Round{{Outcome: review.OutcomePass, Tree: snap.Tree}}}); err != nil {
		t.Fatal(err)
	}
	if res := soloCheck(t, "git commit -m x", dir); res.Allowed() {
		t.Error("a pass from another quick attempt admitted this one")
	}
	saveScopeLedger(t, dir, review.Round{Outcome: review.OutcomePass, Tree: snap.Tree})
	if res := soloCheck(t, "git commit -m x", dir); !res.Allowed() {
		t.Errorf("a solo quick with a pass was refused: %q", res.Text())
	}
	saveScopeLedger(t, dir, review.Round{Outcome: review.OutcomeBlock}, review.Round{Outcome: review.OutcomeBlock})
	if res := soloCheck(t, "git commit -m x", dir); res.Allowed() || !strings.Contains(res.Text(), "dross quick end") {
		t.Errorf("an exhausted quick: %q, want the discard + quick end remedy", res.Text())
	}

	if err := gatestate.SaveQuick(dir, gatestate.Quick{Mode: "pair", Description: "x", Head: head, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if res := soloCheck(t, "git commit -m x", dir); !res.Allowed() {
		t.Errorf("a pair quick was gated: %q", res.Text())
	}

	if err := gatestate.SaveQuick(dir, gatestate.Quick{Mode: "solo", Description: "x", Head: head, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-q", "-m", "the quick landed")
	put(t, dir, "b.go", "package b // later\n")
	gitIn(t, dir, "add", "b.go")
	if res := soloCheck(t, "git commit -m y", dir); !res.Allowed() {
		t.Errorf("a solo quick past its HEAD still gated: %q", res.Text())
	}
}

// TestSharedCandidate: commit-green and solo-review judge one candidate — a
// green and a pass for the same tree admit the same commit, and both see the
// same chained `git add`.
func TestSharedCandidate(t *testing.T) {
	dir := soloRepo(t, "in_progress")
	put(t, dir, "a.go", "package a // both\n")
	put(t, dir, "b.go", "package b // left unstaged\n")
	snap, err := treefp.Candidate(dir, []treefp.Add{{Args: []string{"a.go"}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := gatestate.SaveGreen(dir, gatestate.Green{Tree: snap.Tree, At: time.Now(), Runner: "local"}); err != nil {
		t.Fatal(err)
	}
	saveScopeLedger(t, dir, review.Round{Outcome: review.OutcomePass, Tree: snap.Tree})
	cg, _ := Lookup("commit-green")
	sr, _ := Lookup("solo-review")
	line := "git add a.go && git commit -m x"
	for _, g := range []Gate{cg, sr} {
		if res := Check(bash(t, line, dir), Env{Home: t.TempDir(), Gates: []Gate{g}}); !res.Allowed() {
			t.Errorf("%s refused the candidate the other admits: %q", g.Name, res.Text())
		}
	}
	line = "git add a.go b.go && git commit -m x"
	for _, g := range []Gate{cg, sr} {
		if res := Check(bash(t, line, dir), Env{Home: t.TempDir(), Gates: []Gate{g}}); res.Allowed() {
			t.Errorf("%s admitted a candidate (with b.go) neither the green nor the pass covers", g.Name)
		}
	}
}

func TestSoloReviewClosedPosture(t *testing.T) {
	for _, c := range []struct{ rel, body string }{
		{".dross/gate/review.json", "{ not valid"},
		{".dross/gate/quick.json", "{ not valid"},
		{".dross/phases/p/plan.toml", "{ not valid"},
	} {
		dir, _ := stagedSolo(t)
		put(t, dir, c.rel, c.body)
		res := soloCheck(t, "git commit -m x", dir)
		if res.Allowed() || !strings.Contains(res.Text(), "solo-review") {
			t.Errorf("damaged %s: %q, want a solo-review refusal", c.rel, res.Text())
		}
	}
	dir, _ := stagedSolo(t)
	if res := soloCheck(t, `git commit -m "half`, dir); res.Allowed() {
		t.Error("an unreadable commit line while armed was admitted")
	}
}
