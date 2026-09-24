package cmd

import (
	"go/ast"
	"path/filepath"
	"strings"
	"testing"
)

// The gh burn-down gate. Scoped BY ORIGIN: a finding belongs here when any of
// its origins is a gh spawn in internal/ship, wherever it escapes — a PR
// record decoded in ship and quoted into an error in internal/cmd is still
// gh's output.

// taintFindingsFrom returns the findings with an origin under any of dirs
// (repo-relative, slash-separated).
func taintFindingsFrom(t *testing.T, fs []taintFinding, dirs ...string) []taintFinding {
	t.Helper()
	root := sourceProgram(t).Root
	var out []taintFinding
	for _, f := range fs {
		for _, o := range f.Origins {
			rel, err := filepath.Rel(root, o.Filename)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			matched := false
			for _, d := range dirs {
				if strings.HasPrefix(rel, d+"/") || rel == d {
					matched = true
				}
			}
			if matched {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// markerFindingsIn returns marker findings bound in files under dirs.
func markerFindingsIn(t *testing.T, ms []taintFinding, dirs ...string) []taintFinding {
	t.Helper()
	root := sourceProgram(t).Root
	var out []taintFinding
	for _, m := range ms {
		rel, _ := filepath.Rel(root, m.Escape.Filename)
		for _, d := range dirs {
			if strings.HasPrefix(filepath.ToSlash(rel), d+"/") {
				out = append(out, m)
			}
		}
	}
	return out
}

// taintMarkersIn lists every //dross:taint-cleared marker in the live files
// under dir, as file and marker line.
func taintMarkersIn(t *testing.T, dir string) []taintMarker {
	t.Helper()
	v := liveView(t)
	root := sourceProgram(t).Root
	var out []taintMarker
	for _, p := range v.Pkgs {
		for _, f := range p.Syntax {
			file := v.Fset.Position(f.Pos()).Filename
			rel, _ := filepath.Rel(root, file)
			if !strings.HasPrefix(filepath.ToSlash(rel), dir+"/") {
				continue
			}
			for bound, d := range directiveMarkers(v.Fset, f, taintClearedMarker) {
				out = append(out, taintMarker{directive: d, file: file, line: v.Fset.Position(d.Pos).Line, bound: bound})
			}
		}
	}
	return out
}

// viewWithoutComment is v with one comment line removed from its syntax — how
// a test asks "what if this marker were deleted" without editing the tree.
// The SSA program is shared; only the comments the marker scan reads differ.
func viewWithoutComment(v *srcView, file string, line int) *srcView {
	cp := *v
	cp.Pkgs = make([]*srcPkg, len(v.Pkgs))
	for i, p := range v.Pkgs {
		cp.Pkgs[i] = p
		for j, f := range p.Syntax {
			if v.Fset.Position(f.Pos()).Filename != file {
				continue
			}
			fc := *f
			fc.Comments = nil
			for _, g := range f.Comments {
				ng := &ast.CommentGroup{}
				for _, c := range g.List {
					if v.Fset.Position(c.Pos()).Line != line {
						ng.List = append(ng.List, c)
					}
				}
				if len(ng.List) > 0 {
					fc.Comments = append(fc.Comments, ng)
				}
			}
			np := *p
			np.Syntax = append([]*ast.File(nil), p.Syntax...)
			np.Syntax[j] = &fc
			cp.Pkgs[i] = &np
		}
	}
	return &cp
}

// TestNoGhOutputEscapes is the gate: nothing a gh spawn printed escapes, and
// every marker in internal/ship is well formed and clears something.
func TestNoGhOutputEscapes(t *testing.T) {
	taint, markers := execTaintScan(liveView(t))
	for _, f := range taintFindingsFrom(t, taint, "internal/ship") {
		t.Errorf("%s — %s", f, execTaintRemedy)
	}
	for _, m := range markerFindingsIn(t, markers, "internal/ship") {
		t.Error(m.String())
	}
}

// TestGhMarkersAreLoadBearing: each marker in internal/ship clears a real gh
// flow — deleting it puts a gh-origin finding back.
func TestGhMarkersAreLoadBearing(t *testing.T) {
	ms := taintMarkersIn(t, "internal/ship")
	if len(ms) == 0 {
		t.Fatal("internal/ship carries no taint-cleared marker — the decoded PR records are not marked")
	}
	for _, m := range ms {
		taint, _ := execTaintScan(viewWithoutComment(liveView(t), m.file, m.line))
		if got := taintFindingsFrom(t, taint, "internal/ship"); len(got) == 0 {
			t.Errorf("removing the marker at %s:%d leaves no gh-origin finding — it clears nothing a gate would see",
				filepath.Base(m.file), m.line)
		}
	}
}

// TestGhFixKeepsTheSurgeryAnchor: open.go carries t-11's exec-consent surgery
// anchor, and this burn-down must not move it.
func TestGhFixKeepsTheSurgeryAnchor(t *testing.T) {
	if _, err := repoExecGraph(t).surgery(sourceProgram(t).Root,
		execSurgery{file: "internal/ship/open.go", op: "unmark", anchor: "//dross:exec-exempt gh is the forge API client"}); err != nil {
		t.Error(err)
	}
}
