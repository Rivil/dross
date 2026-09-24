package cmd

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"testing"
)

// The git plumbing burn-down gate. Scoped BY ORIGIN to one spawn: the
// exec.Command in gitRun, the effect-only helper whose output goes to stderr.
// gitTrim's spawn sits in the same file and feeds the same callers, but its
// findings are t-19's burn-down, not this one's — so this gate keys on the
// origin's exact line, never on the file.

// gitHelperSpawnLines returns the file and line(s) of the exec.Command call
// inside each named helper function in internal/cmd.
func gitHelperSpawnLines(t *testing.T, helpers ...string) map[token.Position]bool {
	t.Helper()
	v := liveView(t)
	want := map[string]bool{}
	for _, h := range helpers {
		want[h] = true
	}
	out := map[token.Position]bool{}
	for _, p := range v.Pkgs {
		if p.Path != modulePath+"/internal/cmd" {
			continue
		}
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv != nil || !want[fd.Name.Name] || fd.Body == nil {
					continue
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					if obj := execCalleeObject(p.Info, call); obj != nil && obj.Pkg() != nil &&
						obj.Pkg().Path() == "os/exec" && obj.Name() == "Command" {
						pos := v.Fset.Position(call.Pos())
						out[token.Position{Filename: pos.Filename, Line: pos.Line}] = true
					}
					return true
				})
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("found no exec.Command inside %v — the gate would see nothing", helpers)
	}
	return out
}

// findingsFromLines keeps the findings with an origin on one of lines.
func findingsFromLines(fs []taintFinding, lines map[token.Position]bool) []taintFinding {
	var out []taintFinding
	for _, f := range fs {
		for _, o := range f.Origins {
			if lines[token.Position{Filename: o.Filename, Line: o.Line}] {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// TestNoGitRunOutputEscapes is the gate: nothing gitRun's spawn printed
// escapes.
func TestNoGitRunOutputEscapes(t *testing.T) {
	taint, _ := execTaintScan(liveView(t))
	for _, f := range findingsFromLines(taint, gitHelperSpawnLines(t, "gitRun")) {
		t.Errorf("%s — %s", f, execTaintRemedy)
	}
}

// TestGitRunGateIsScopedByOrigin: a finding whose origin is gitTrim's spawn —
// in the same file, reaching the same kind of error — is not this gate's; one
// whose origin is gitRun's is.
func TestGitRunGateIsScopedByOrigin(t *testing.T) {
	runLines := gitHelperSpawnLines(t, "gitRun")
	trimLines := gitHelperSpawnLines(t, "gitTrim")
	var run, trim token.Position
	for p := range runLines {
		run = p
	}
	for p := range trimLines {
		trim = p
	}
	if filepath.Base(run.Filename) != filepath.Base(trim.Filename) {
		t.Logf("gitRun and gitTrim now live in different files (%s, %s)", run.Filename, trim.Filename)
	}
	fs := []taintFinding{
		{Escape: token.Position{Filename: "x.go", Line: 1}, What: "is passed to fmt.Errorf", Origins: []token.Position{trim}},
		{Escape: token.Position{Filename: "x.go", Line: 2}, What: "is passed to fmt.Errorf", Origins: []token.Position{run}},
	}
	got := findingsFromLines(fs, runLines)
	if len(got) != 1 || got[0].Escape.Line != 2 {
		t.Errorf("the gate kept %v, want only the gitRun-origin finding", got)
	}
}
