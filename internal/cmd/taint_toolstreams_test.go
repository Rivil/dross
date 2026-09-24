package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// The tool-stream burn-down gate. Scoped BY ORIGIN to spawns in
// internal/mutation, internal/remote and internal/compilefence, wherever the
// value escapes — the host lock's holder record once travelled from an ssh
// session through BusyError into verify.go, a persisted leg error and a PR
// body.
//
// Tool streams end at the terminal or the recorder, never at a marker: the
// mutation tool runners may not carry //dross:taint-cleared at all. What remote
// marks is PROTOCOL data — the fields of the lock and status records a dross
// script writes on the host, admitted only after a shape check.

var toolStreamOriginDirs = []string{"internal/mutation", "internal/remote", "internal/compilefence"}

// toolRunnerFiles are the mutation adapters whose streams are the tool's own
// output end to end: a marker in them would clear the stream itself.
var toolRunnerFiles = []string{"stryker.go", "gremlins.go", "stryker_net.go"}

// TestNoToolStreamEscapes is the gate: nothing a mutation, remote or
// compilefence spawn printed escapes, and every marker in those packages is
// well formed and clears something.
func TestNoToolStreamEscapes(t *testing.T) {
	taint, markers := execTaintScan(liveView(t))
	for _, f := range taintFindingsFrom(t, taint, toolStreamOriginDirs...) {
		t.Errorf("%s — %s", f, execTaintRemedy)
	}
	for _, m := range markerFindingsIn(t, markers, toolStreamOriginDirs...) {
		t.Error(m.String())
	}
}

// toolRunnerMarkers reports every taint-cleared marker in a tool-runner file
// of internal/mutation.
func toolRunnerMarkers(fset *token.FileSet, files []*ast.File) []string {
	var out []string
	for _, f := range files {
		name := filepath.ToSlash(fset.Position(f.Pos()).Filename)
		if !strings.Contains(name, "internal/mutation/") {
			continue
		}
		banned := false
		for _, b := range toolRunnerFiles {
			if filepath.Base(name) == b {
				banned = true
			}
		}
		if !banned {
			continue
		}
		for bound, d := range directiveMarkers(fset, f, taintClearedMarker) {
			out = append(out, fset.Position(d.Pos).String()+" (binds line "+itoaTS(bound)+")")
		}
	}
	return out
}

func itoaTS(n int) string {
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if s == "" {
		return "0"
	}
	return s
}

// TestToolRunnersCarryNoMarker: a marker in stryker.go, gremlins.go or
// stryker_net.go is itself a finding — whatever it binds there is the tool's
// stream. Proven on a synthetic file as well as the live tree, so a ban that
// matched nothing would fail here.
func TestToolRunnersCarryNoMarker(t *testing.T) {
	var files []*ast.File
	for _, p := range liveView(t).Pkgs {
		files = append(files, p.Syntax...)
	}
	for _, hit := range toolRunnerMarkers(liveView(t).Fset, files) {
		t.Errorf("%s — a tool runner's stream ends at the terminal or the recorder, never at a marker", hit)
	}

	fset := token.NewFileSet()
	src := "package mutation\n\nfunc f(out []byte) string {\n\t" + taintClearedMarker + " a reason long enough to pass the prose floor\n\treturn string(out)\n}\n"
	f, err := parser.ParseFile(fset, "/repo/internal/mutation/stryker.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if got := toolRunnerMarkers(fset, []*ast.File{f}); len(got) != 1 {
		t.Errorf("a marker in internal/mutation/stryker.go was not reported: %v", got)
	}
	g, err := parser.ParseFile(fset, "/repo/internal/mutation/launcher.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if got := toolRunnerMarkers(fset, []*ast.File{g}); len(got) != 0 {
		t.Errorf("the ban reached a file that is not a tool runner: %v", got)
	}
}

// TestToolStreamFixKeepsTheSurgeryAnchor: gremlins.go carries t-11's
// exec-consent surgery anchor, and this burn-down must not move it.
func TestToolStreamFixKeepsTheSurgeryAnchor(t *testing.T) {
	if _, err := repoExecGraph(t).surgery(sourceProgram(t).Root, execSurgery{
		file: "internal/mutation/gremlins.go", op: "mark", anchor: "		return exec.Command(args[0], args[1:]...)",
		reason: "anchor check",
	}); err != nil {
		t.Error(err)
	}
}
