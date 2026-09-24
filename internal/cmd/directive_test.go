package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// The //dross: directive grammar, shared by every in-source marker a source
// scan honours:
//
//   - //dross:exec-exempt   — a spawn site that cannot reach repo-authored code
//     (exec-consent, execconsent_audit_test.go)
//   - //dross:taint-cleared — a conversion of spawn output into a value that no
//     longer carries the stream (exec-output taint)
//
// One parser, so the two cannot drift into different ideas of what a marker is.
// A directive is a Go directive-shaped comment: no space after the slashes, so
// ordinary prose that mentions a marker cannot trigger it. It binds the line
// IMMEDIATELY below it — position, not proximity. Its reason must be separated
// from the name by whitespace and must say something.

// taintClearedMarker clears exec-output taint at a conversion. Same grammar as
// execExemptMarker (the phase's marker_grammar lock); the two are separate
// names, and neither satisfies a scan that reads the other.
const taintClearedMarker = "//dross:taint-cleared"

// directiveMinReason is the floor on marker prose. A reason has to be long
// enough to say why; "status" is the subcommand restated, which is what the
// marker already sits next to.
const directiveMinReason = 20

// directive is one parsed marker. Reason is empty for a bare marker, which is a
// different finding from no marker at all. Pos is the marker comment itself, so
// a finding about the marker can point at it rather than at the line it binds.
type directive struct {
	Reason string
	Pos    token.Pos
}

// directiveMarkers maps the line each `name` directive binds — the one
// directly below it — to the parsed marker.
func directiveMarkers(fset *token.FileSet, f *ast.File, name string) map[int]directive {
	out := map[int]directive{}
	for _, group := range f.Comments {
		for _, c := range group.List {
			rest, ok := strings.CutPrefix(c.Text, name)
			if !ok {
				continue
			}
			// The reason must be separated from the directive. Without this
			// `//dross:exec-exemptanything` would parse as a marker whose reason
			// is glued to it, which is a typo passing as a marker — and a
			// longer directive name sharing the prefix would parse as this one.
			if rest != "" && !strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "\t") {
				continue
			}
			out[fset.Position(c.End()).Line+1] = directive{Reason: strings.TrimSpace(rest), Pos: c.Pos()}
		}
	}
	return out
}

// directiveVerdict classifies what a line's directive does for it: "unmarked"
// (no directive binds it), "reasonless", "short" (prose under the floor), or
// "valid".
func directiveVerdict(markers map[int]directive, line int) string {
	m, ok := markers[line]
	switch {
	case !ok:
		return "unmarked"
	case m.Reason == "":
		return "reasonless"
	case len([]rune(m.Reason)) < directiveMinReason:
		return "short"
	}
	return "valid"
}

// TestDirectiveGrammarIsShared runs every marker row of the exec-consent
// snippet table through the parser under BOTH directive names and requires the
// same verdict from each — and the verdict the table's FLAG/PASS header
// implies. A second, diverging parser for either name fails here naming the row
// and the directive.
func TestDirectiveGrammarIsShared(t *testing.T) {
	rows, _ := parseExecSnippetTable(t)
	required := map[string]bool{
		"reasonless-marker":                 false,
		"reason-is-only-the-subcommand":     false,
		"marker-glued-to-its-reason":        false,
		"spaced-comment-is-not-a-directive": false,
		"marker-two-lines-above":            false,
		"marker-below-the-call":             false,
		"marker-with-tab-before-its-reason": false,
	}
	const execName = "dross:exec-exempt"
	checked := 0
	for _, row := range rows {
		src := strings.Join(row.Src, "\n")
		if !strings.Contains(src, execName) {
			continue
		}
		if _, ok := required[row.Name]; ok {
			required[row.Name] = true
		}
		checked++
		verdicts := map[string]string{}
		for _, name := range []string{execExemptMarker, taintClearedMarker} {
			rewritten := strings.ReplaceAll(src, execName, strings.TrimPrefix(name, "//"))
			verdicts[name] = snippetDirectiveVerdict(t, rewritten, name)
		}
		exec, taint := verdicts[execExemptMarker], verdicts[taintClearedMarker]
		if exec != taint {
			t.Errorf("row %s: %s reads %q but %s reads %q — the directives must share one grammar",
				row.Name, execExemptMarker, exec, taintClearedMarker, taint)
		}
		if row.Flag == (exec == "valid") {
			t.Errorf("row %s is a %s row but %s reads the marker as %q",
				row.Name, map[bool]string{true: "FLAG", false: "PASS"}[row.Flag], execExemptMarker, exec)
		}
	}
	for name, seen := range required {
		if !seen {
			t.Errorf("marker-grammar row %s is missing from snippets.txt", name)
		}
	}
	if checked < len(required) {
		t.Errorf("checked %d marker rows, want at least %d", checked, len(required))
	}
}

// snippetDirectiveVerdict parses snippet lines the way the exec-consent table
// wraps them and returns name's verdict at the snippet's spawn line.
func snippetDirectiveVerdict(t *testing.T, src, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "snippet.go", execSnippetPreamble+src+"\n}\n", parser.ParseComments)
	if err != nil {
		t.Fatalf("snippet does not parse: %v\n%s", err, src)
	}
	line := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || line != 0 || !isExecConstruction(call) {
			return true
		}
		line = fset.Position(call.Pos()).Line
		return false
	})
	if line == 0 {
		t.Fatalf("snippet has no exec.Command call:\n%s", src)
	}
	return directiveVerdict(directiveMarkers(fset, f, name), line)
}

// isExecConstruction reports whether call is exec.Command or
// exec.CommandContext, by the snippet preamble's import name.
func isExecConstruction(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext")
}

// TestDirectivesAreNotInterchangeable: a well-formed taint-cleared marker is
// not an exec exemption, and neither name's parser sees the other's markers.
// Conflating them would let one marker silence two different scans.
func TestDirectivesAreNotInterchangeable(t *testing.T) {
	const prose = " git status reads the working tree and runs no repo-authored code"
	findings, sites := auditExecSnippet(t,
		"\t"+taintClearedMarker+prose,
		"\texec.Command(\"git\", \"status\")")
	if sites != 1 || len(findings) != 1 {
		t.Fatalf("a %s marker above a spawn left %d finding(s) over %d site(s); want the exec-consent finding in place",
			taintClearedMarker, len(findings), sites)
	}

	for _, tc := range []struct{ written, read string }{
		{execExemptMarker, taintClearedMarker},
		{taintClearedMarker, execExemptMarker},
	} {
		src := execSnippetPreamble + "\t" + tc.written + prose + "\n\texec.Command(\"git\", \"status\")\n}\n"
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "snippet.go", src, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		if got := directiveMarkers(fset, f, tc.read); len(got) != 0 {
			t.Errorf("parsing for %s found %d marker(s) in a file carrying only %s", tc.read, len(got), tc.written)
		}
		if got := directiveMarkers(fset, f, tc.written); len(got) != 1 {
			t.Errorf("parsing for %s found %d marker(s), want 1", tc.written, len(got))
		}
	}
}
