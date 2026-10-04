package gate

import (
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/review"
)

// soloRepo is pairRepo with the execute run recorded as solo.
func soloRepo(t *testing.T, statuses ...string) string {
	t.Helper()
	dir := pairRepo(t, statuses...)
	setMode(t, dir, "solo")
	return dir
}

func setMode(t *testing.T, dir, mode string) {
	t.Helper()
	if err := gatestate.SaveExecute(dir, gatestate.Execute{Phase: "p", Mode: mode, At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

func mustScope(t *testing.T, dir string) *ReviewScope {
	t.Helper()
	s, err := ArmedScope(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func asLedger(s *ReviewScope) *gatestate.Review {
	return &gatestate.Review{Kind: s.Kind, Phase: s.Phase, Task: s.Task, Attempt: s.Attempt}
}

func TestArmedScope(t *testing.T) {
	dir := soloRepo(t, "done", "in_progress", "")
	t2 := mustScope(t, dir)
	if t2 == nil || t2.Kind != review.KindTask || t2.Phase != "p" || t2.Task != "t-2" || t2.Attempt == "" {
		t.Fatalf("solo t-2 in progress: scope = %+v", t2)
	}

	// t-2's ledger, then t-3 in progress: a fresh record.
	put(t, dir, ".dross/phases/p/plan.toml", planWith("done", "done", "in_progress"))
	if t3 := mustScope(t, dir); t3.Task != "t-3" || t3.Matches(asLedger(t2)) {
		t.Fatalf("t-3 in progress reuses t-2's ledger: %+v", t3)
	}

	// A .dross/-only commit and an empty commit keep the same ledger.
	put(t, dir, ".dross/phases/p/plan.toml", planWith("done", "in_progress", ""))
	put(t, dir, ".dross/notes.md", "bookkeeping\n")
	gitIn(t, dir, "add", ".dross/notes.md")
	gitIn(t, dir, "commit", "-q", "-m", "bookkeeping")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "empty")
	if same := mustScope(t, dir); !same.Matches(asLedger(t2)) {
		t.Fatalf(".dross/-only and empty commits started a fresh ledger: %+v vs %+v", same, t2)
	}

	// A code commit for the same task moves its base: a fresh record.
	put(t, dir, "a.go", "package a // committed\n")
	gitIn(t, dir, "add", "a.go")
	gitIn(t, dir, "commit", "-q", "-m", "code")
	moved := mustScope(t, dir)
	if moved.Task != "t-2" || moved.Matches(asLedger(t2)) {
		t.Fatalf("a code commit kept the old ledger: %+v", moved)
	}

	// A solo quick at HEAD wins; a pair quick does not.
	head := headSHA(t, dir)
	if err := gatestate.SaveQuick(dir, gatestate.Quick{Mode: "pair", Description: "x", Head: head, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if s := mustScope(t, dir); s.Kind != review.KindTask || s.Task != "t-2" {
		t.Fatalf("a pair quick displaced the solo task: %+v", s)
	}
	if err := gatestate.SaveQuick(dir, gatestate.Quick{Mode: "solo", Description: "fix the thing", Head: head, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	q := mustScope(t, dir)
	if q.Kind != review.KindQuick || q.Description != "fix the thing" || q.Phase != "" || q.Task != "" {
		t.Fatalf("solo quick at HEAD: scope = %+v", q)
	}
	// Once the quick's commit moves HEAD, it is no longer armed.
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "quick landed")
	if s := mustScope(t, dir); s.Kind != review.KindTask {
		t.Fatalf("a quick past its HEAD is still armed: %+v", s)
	}

	// Two tasks in progress: which one is under review is ambiguous.
	both := soloRepo(t, "in_progress", "", "in_progress")
	_, err := ArmedScope(both)
	if err == nil || !strings.Contains(err.Error(), "t-1") || !strings.Contains(err.Error(), "t-3") {
		t.Fatalf("two tasks in progress = %v, want an error naming both", err)
	}
}

func TestArmedScopePairSilent(t *testing.T) {
	pair := pairRepo(t, "in_progress")
	setMode(t, pair, "pair")
	absent := pairRepo(t, "in_progress")
	idle := soloRepo(t, "done", "")
	off := soloRepo(t, "in_progress")
	gitIn(t, off, "checkout", "-q", "main")
	for name, dir := range map[string]string{"pair": pair, "no execute record": absent, "no task in progress": idle, "HEAD off phase/p": off} {
		s, err := ArmedScope(dir)
		if err != nil || s != nil {
			t.Errorf("%s: scope = %+v, %v; want nil, nil", name, s, err)
		}
	}
}

func TestArmedScopeClosedPosture(t *testing.T) {
	for _, c := range []struct{ rel, want string }{
		{".dross/gate/execute.json", ".dross/gate/execute.json"},
		{".dross/gate/quick.json", ".dross/gate/quick.json"},
		{".dross/phases/p/plan.toml", ".dross/phases/p/plan.toml"},
	} {
		dir := soloRepo(t, "in_progress")
		put(t, dir, c.rel, "{ not valid")
		s, err := ArmedScope(dir)
		if err == nil || s != nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("damaged %s: scope = %+v, %v; want an error naming it", c.rel, s, err)
		}
	}
}

func TestQuickAttemptKey(t *testing.T) {
	dir := soloRepo(t, "")
	head := headSHA(t, dir)
	at := time.Date(2026, 10, 4, 12, 0, 0, 1, time.UTC)
	begin := func(at time.Time) *ReviewScope {
		t.Helper()
		if err := gatestate.SaveQuick(dir, gatestate.Quick{Mode: "solo", Description: "x", Head: head, At: at}); err != nil {
			t.Fatal(err)
		}
		return mustScope(t, dir)
	}
	first := begin(at)
	if again := mustScope(t, dir); !again.Matches(asLedger(first)) {
		t.Fatal("one quick begin's scope changed between reads")
	}
	if second := begin(at.Add(time.Second)); second.Matches(asLedger(first)) {
		t.Fatal("two quick begins at the same HEAD share one ledger")
	}
}

func TestContextScopeLoads(t *testing.T) {
	dir := soloRepo(t, "in_progress")
	put(t, dir, ".dross/phases/p/spec.toml", "[phase]\n  id = \"p\"\n\n[[criteria]]\n  id = \"c-1\"\n  text = \"C1\"\n\n[[decisions]]\n  key = \"k\"\n  choice = \"K\"\n  locked = true\n")
	put(t, dir, ".dross/rules.toml", "[[rule]]\n  id = \"r-1\"\n  text = \"R1\"\n  severity = \"hard\"\n")
	s := mustScope(t, dir)
	cs, err := ContextScope(dir, t.TempDir(), s)
	if err != nil {
		t.Fatal(err)
	}
	if cs.Task.ID != "t-1" || len(cs.Criteria) != 1 || len(cs.Decisions) != 1 || len(cs.Rules) != 1 || cs.Rules[0].ID != "r-1" {
		t.Fatalf("context scope = %+v", cs)
	}
	q, err := ContextScope(dir, "", &ReviewScope{Kind: review.KindQuick, Description: "D"})
	if err != nil || q.Description != "D" || q.Task.ID != "" || len(q.Rules) != 1 {
		t.Fatalf("quick context scope = %+v, %v", q, err)
	}
}
