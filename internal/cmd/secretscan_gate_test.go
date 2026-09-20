package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/secretscan"
	"github.com/Rivil/dross/internal/state"
)

// commitSecretNote drops a notes.md carrying a synthesized token on line 3
// into the fixture phase "x" and COMMITS it, so the tree is clean and the only
// thing between the token and origin is ship's pre-flight scan. Returns the
// token so callers can assert it is never echoed.
func commitSecretNote(t *testing.T, dir string) string {
	t.Helper()
	tok := synthGitlabToken()
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "x", "notes.md"),
		"# notes\n\nglpat: "+tok+"\n")
	gitCommit(t, dir, "chore(dross): agent note")
	if st := mustGit(t, dir, "status", "--porcelain"); st != "" {
		t.Fatalf("fixture tree must be clean, got: %q", st)
	}
	return tok
}

// TestShipRefusesCommittedSecretOnCleanTree pins that the ship gate does not
// live only inside autoCommitDrossDirt: that helper is a no-op on a clean
// tree, so a token already committed into a .dross artifact would ride
// straight to the push. Ship must refuse before any push and before the
// provider sees a request.
func TestShipRefusesCommittedSecretOnCleanTree(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	tok := commitSecretNote(t, dir)
	cap, remoteDir := shipMockFlowRemote(t, dir)

	err := runCmd(t, Ship())
	if err == nil {
		t.Fatal("ship shipped over a committed secret on a clean tree")
	}
	msg := err.Error()
	if !strings.Contains(msg, "gitlab-pat at .dross/phases/x/notes.md:3") {
		t.Fatalf("refusal must name the artifact and line: %v", err)
	}
	if !strings.Contains(msg, "secret in tracked .dross artifact") || !strings.Contains(msg, secretscan.AllowMarker) {
		t.Fatalf("refusal must carry the ship wording and the remedy marker: %v", err)
	}
	if _, ok := secretscan.AsErrHit(err); !ok {
		t.Fatalf("refusal must wrap *secretscan.ErrHit: %v", err)
	}
	assertNoTokenEcho(t, msg, tok, "glpat-")

	if cap.posts != 0 {
		t.Errorf("provider received %d PR-open requests; want 0", cap.posts)
	}
	if gitNoOut(remoteDir, "rev-parse", "--verify", "refs/heads/phase/x") == nil {
		t.Error("phase/x was pushed to origin despite the refusal")
	}
}

// TestShipPrintBodyStillScans pins the gate's placement ahead of the
// --print-body early return: a dry run that renders the body is still a run
// over a tree carrying a secret, and it errors instead of printing.
func TestShipPrintBodyStillScans(t *testing.T) {
	dir := shipFixture(t, "https://forge.example/me/p.git")
	commitSecretNote(t, dir)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, Ship(), "--print-body") })
	if err == nil {
		t.Fatalf("--print-body printed over a committed secret:\n%s", out)
	}
	if !strings.Contains(err.Error(), "notes.md:3") {
		t.Fatalf("refusal must name the artifact line: %v", err)
	}
	if out != "" {
		t.Fatalf("nothing should print before the refusal, got:\n%s", out)
	}
}

// TestAutoCommitRefusesUntrackedSecretNote pins the scan's position BEFORE
// `git add .dross`: the untracked note is still untracked afterwards and no
// chore(dross) commit was written.
func TestAutoCommitRefusesUntrackedSecretNote(t *testing.T) {
	dir := initWithGit(t)
	tok := synthGitlabToken()
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "p", "notes.md"), "t = "+tok+"\n")
	before := commitCount(t, dir)

	committed, err := autoCommitDrossDirt(dir, "shipping")
	if err == nil {
		t.Fatal("auto-commit swallowed an untracked note carrying a token")
	}
	if committed {
		t.Error("committed must be false on refusal")
	}
	if !strings.HasPrefix(err.Error(), "refusing to auto-commit .dross:") {
		t.Errorf("refusal wording: %v", err)
	}
	if _, ok := secretscan.AsErrHit(err); !ok {
		t.Fatalf("refusal must wrap *secretscan.ErrHit: %v", err)
	}
	if !strings.Contains(err.Error(), "gitlab-pat at .dross/phases/p/notes.md:1") {
		t.Errorf("refusal must name the note and line: %v", err)
	}
	assertNoTokenEcho(t, err.Error(), tok, "glpat-")

	// -uall: porcelain collapses a wholly-untracked dir to `?? .dross/phases/`.
	st := mustGit(t, dir, "status", "--porcelain", "--untracked-files=all")
	if !strings.Contains(st, "?? .dross/phases/p/notes.md") {
		t.Errorf("note must still be untracked after the refusal, status:\n%s", st)
	}
	if err := gitNoOut(dir, "diff", "--cached", "--quiet"); err != nil {
		t.Error("refusal must stage nothing, but the index has staged changes")
	}
	if after := commitCount(t, dir); after != before {
		t.Errorf("refusal must create zero commits: %s -> %s", before, after)
	}
	if log := mustGit(t, dir, "log", "--format=%s"); strings.Contains(log, "chore(dross)") {
		t.Errorf("no chore(dross) commit may land on a refusal:\n%s", log)
	}
}

// TestAutoCommitStillRefusesCodeDirtFirst pins the order of the two refusals:
// real code dirt is the existing dirtyTreeError, even when a secret note is
// also present; the secret refusal is reached only once the tree is
// .dross-only.
func TestAutoCommitStillRefusesCodeDirtFirst(t *testing.T) {
	dir := initWithGit(t)
	tok := synthGitlabToken()
	note := filepath.Join(dir, ".dross", "phases", "p", "notes.md")
	mustWrite(t, note, "t = "+tok+"\n")
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n")

	_, err := autoCommitDrossDirt(dir, "shipping")
	if err == nil {
		t.Fatal("expected a refusal on mixed dirt")
	}
	if !strings.Contains(err.Error(), "working tree is dirty") || !strings.Contains(err.Error(), "main.go") {
		t.Fatalf("mixed dirt must be the dirtyTreeError naming the code file: %v", err)
	}
	if _, ok := secretscan.AsErrHit(err); ok {
		t.Fatalf("code dirt must pre-empt the secret refusal: %v", err)
	}

	if err := os.Remove(filepath.Join(dir, "main.go")); err != nil {
		t.Fatal(err)
	}
	_, err = autoCommitDrossDirt(dir, "shipping")
	if _, ok := secretscan.AsErrHit(err); !ok {
		t.Fatalf("with only the note dirty, the refusal must be the secret one: %v", err)
	}
}

// TestPhaseCompleteRefusesOnSecret pins that placing the gate in the shared
// auto-commit helper reaches the non-ship callers: `phase complete` over a
// changes.json carrying a token refuses, writes nothing to state.json, and
// records no completion.
func TestPhaseCompleteRefusesOnSecret(t *testing.T) {
	dir := initWithGit(t)
	tok := synthGitlabToken()
	// A parseable record with a base, so the base resolver succeeds and the
	// run reaches the auto-commit gate rather than the no-base refusal.
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "p", "changes.json"),
		`{"phase":"p","base":"main","tasks":{"t1":{"files":["a.go"],"notes":"`+tok+`"}}}`+"\n")
	statePath := filepath.Join(dir, ".dross", state.File)
	stateBefore := mustRead(t, statePath)
	before := commitCount(t, dir)

	err := runCmd(t, Phase(), "complete", "p")
	if err == nil {
		t.Fatal("phase complete proceeded over a changes.json carrying a token")
	}
	if _, ok := secretscan.AsErrHit(err); !ok {
		t.Fatalf("refusal must wrap *secretscan.ErrHit: %v", err)
	}
	if !strings.Contains(err.Error(), "gitlab-pat at .dross/phases/p/changes.json:1") {
		t.Errorf("refusal must name the record and line: %v", err)
	}
	assertNoTokenEcho(t, err.Error(), tok, "glpat-")

	if got := mustRead(t, statePath); got != stateBefore {
		t.Errorf("state.json changed across the refusal:\n--- before\n%s\n--- after\n%s", stateBefore, got)
	}
	s, lerr := state.Load(statePath)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if historyHasAction(s, "completed p") {
		t.Error("a completion event was recorded despite the refusal")
	}
	if after := commitCount(t, dir); after != before {
		t.Errorf("refusal must create zero commits: %s -> %s", before, after)
	}
}

// callSite is one CallExpr, named by its bare callee identifier or by the
// selector pkg.name, with its source position for ordering assertions.
type callSite struct {
	name string
	pos  token.Pos
}

func callSitesIn(root ast.Node) []callSite {
	var sites []callSite
	ast.Inspect(root, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			sites = append(sites, callSite{fn.Name, call.Pos()})
		case *ast.SelectorExpr:
			if x, ok := fn.X.(*ast.Ident); ok {
				sites = append(sites, callSite{x.Name + "." + fn.Sel.Name, call.Pos()})
			}
		}
		return true
	})
	return sites
}

func parseCmdFile(t *testing.T, fset *token.FileSet, name string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(fset, name, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func funcDecl(t *testing.T, f *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == name && fn.Body != nil {
			return fn
		}
	}
	t.Fatalf("no func %s", name)
	return nil
}

// TestShipGateOrderingIsBeforeAutoCommit pins the gate's position in ship's
// RunE by source position: the one scanDrossArtifacts call precedes the
// autoCommitDrossDirt call and every pushPhaseBranch / ship.OpenPR call. A
// gate moved past the commit or the push would pass every behavioural test
// on a dirty tree and fail only here.
func TestShipGateOrderingIsBeforeAutoCommit(t *testing.T) {
	fset := token.NewFileSet()
	f := parseCmdFile(t, fset, "ship.go")
	sites := callSitesIn(funcDecl(t, f, "Ship"))

	var scan []token.Pos
	var gated []callSite
	for _, s := range sites {
		switch s.name {
		case "scanDrossArtifacts":
			scan = append(scan, s.pos)
		case "autoCommitDrossDirt", "pushPhaseBranch", "ship.OpenPR":
			gated = append(gated, s)
		}
	}
	if len(scan) != 1 {
		t.Fatalf("want exactly one scanDrossArtifacts call inside Ship, found %d", len(scan))
	}
	want := map[string]bool{"autoCommitDrossDirt": false, "pushPhaseBranch": false, "ship.OpenPR": false}
	for _, g := range gated {
		want[g.name] = true
		if g.pos < scan[0] {
			t.Errorf("%s at %s precedes the secret scan at %s", g.name, fset.Position(g.pos), fset.Position(scan[0]))
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("no %s call found inside Ship — the ordering assertion is vacuous for it", name)
		}
	}
}

// TestShipAndValidateShareOneScanner pins the ship_gate_scope decision: the
// three gates call scanDrossArtifacts, and none of them grows a second walk
// (filepath.Walk*, listDrossArtifacts) or a direct secretscan.Scan* call that
// could drift from the shared one.
func TestShipAndValidateShareOneScanner(t *testing.T) {
	fset := token.NewFileSet()
	for _, name := range []string{"validate.go", "ship.go", "cleantree.go"} {
		sites := callSitesIn(parseCmdFile(t, fset, name))
		shared := 0
		for _, s := range sites {
			switch {
			case s.name == "scanDrossArtifacts":
				shared++
			case s.name == "listDrossArtifacts",
				s.name == "filepath.Walk", s.name == "filepath.WalkDir",
				strings.HasPrefix(s.name, "secretscan.Scan"):
				t.Errorf("%s: %s at %s — a second scanner beside scanDrossArtifacts", name, s.name, fset.Position(s.pos))
			}
		}
		if shared == 0 {
			t.Errorf("%s: no scanDrossArtifacts call", name)
		}
	}
}
