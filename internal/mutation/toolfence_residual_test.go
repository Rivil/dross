package mutation

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The residual guard behind c-1.
//
// The type system carries most of the guarantee: printHead takes an io.Writer
// and returns nothing, so there is no string for an error constructor to
// swallow, and the record's parameter list has no slot tool text could enter.
// One gap is left that no type closes — `head.buf` is a bytes.Buffer in the same
// package, so `errors.New(head.buf.String())` compiles today and would put the
// whole retained head straight back into a persisted string.
//
// This file bans both shapes inside an error constructor. It is its OWN task
// rather than folded into the adapter routing: folded, it would be red until the
// second adapter landed, and a guard that is red while the work is in progress
// gets loosened until it passes against the first.
//
// WHAT THIS DOES NOT CATCH, so c-1 is not read as wider than it is: a value
// laundered through an intermediate variable — `s := head.buf.String()` on one
// line and `errors.New(s)` on the next. That is not a hypothetical: it is the
// EXACT shape this phase removed from stryker.go, which rendered the head into
// a strings.Builder and passed quoted.String() to fmt.Errorf on the next line.
// Re-introducing that branch verbatim passes this guard (proven at verify);
// it is the behavioural canaries — TestStrykerReportlessSplitsTerminalFromError
// and TestFailedLegReachesDiskClean — that catch it. Deciding that an error
// argument traces back to the head buffer is dataflow over resolved types,
// which is the source-side enumeration this phase deferred to secret-detection.

// errorConstructors are the calls whose arguments become a persisted string.
var errorConstructors = map[string]bool{
	"errors.New": true, "fmt.Errorf": true, "fmt.Sprintf": true,
}

// TestNoToolOutputInsideAnErrorConstructor runs the ban over the live tree.
func TestNoToolOutputInsideAnErrorConstructor(t *testing.T) {
	files := mutationSources(t)
	if len(files) < 5 {
		t.Fatalf("the scan found %d non-test sources in internal/mutation; it is not walking the package", len(files))
	}

	saw := 0
	for _, f := range files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		ctors, hits := scanErrorConstructors(fset, file)
		saw += ctors
		for _, h := range hits {
			t.Errorf("%s: %s — the head is the LIVE diagnostic: print it to os.Stderr at the failure point and return a record, never build a string out of it", f, h)
		}
	}
	// Vacuity floor: a scanner that stopped recognising error constructors
	// would report nothing and pass.
	if saw < 20 {
		t.Errorf("the scan visited only %d error constructors in internal/mutation; it is passing because it found nothing to check", saw)
	}
}

// TestErrorConstructorScannerIsCalibrated drives the same scanner over a fixture
// whose answers are written down, so a scanner that quietly stopped matching
// fails here rather than passing vacuously against a tree that is already clean.
//
// The fixture is parsed, never compiled: printHead returns nothing, so
// errors.New(h.printHead(...)) does not build — which is the point. The ban has
// to hold against a printHead that someone gave a string return tomorrow, and
// against the buf read that compiles today.
func TestErrorConstructorScannerIsCalibrated(t *testing.T) {
	const fixture = `package mutation

import (
	"errors"
	"fmt"
	"os"
)

// MUST TRIP: the head rendered into an error.
func mustTripPrintHead(h *headBuffer, path string) error {
	return errors.New(h.printHead(os.Stderr, "stryker", 40))
}

// MUST TRIP: the retained bytes read straight out of the buffer.
func mustTripBufRead(h *headBuffer, path string) error {
	return fmt.Errorf("stryker did not write a report at %s:\n%s", path, h.buf.String())
}

// MUST TRIP: the same read one level down, inside a Sprintf argument.
func mustTripNestedBufRead(h *headBuffer) error {
	return errors.New(fmt.Sprintf("head was: %s", string(h.buf.Bytes())))
}

// MUST NOT TRIP: the head goes to the terminal, which is where it belongs.
func mustNotTripPrintToStderr(h *headBuffer) error {
	h.printHead(os.Stderr, "stryker", 40)
	fmt.Fprintln(os.Stderr)
	return errors.New("stryker did not write a report")
}

// MUST NOT TRIP: a fact ABOUT the output is not the output.
func mustNotTripRecord(h *headBuffer) error {
	return fmt.Errorf("wrapped: %w", RecordToolFailure("stryker", 3, Observed(h.observed())))
}

// MUST NOT TRIP: keying dross's own hint off what the tool said, without
// putting a byte of it in the message.
func mustNotTripContains(h *headBuffer) error {
	if h.contains("did not result in any files") {
		return errors.New("stryker said so itself")
	}
	return nil
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", fixture, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	ctors, hits := scanErrorConstructors(fset, file)
	if ctors < 6 {
		t.Errorf("the scanner visited %d error constructors in the fixture, want at least 6", ctors)
	}

	tripped := map[string]bool{}
	for _, h := range hits {
		tripped[strings.SplitN(h, ":", 2)[0]] = true
	}
	for _, want := range []string{"mustTripPrintHead", "mustTripBufRead", "mustTripNestedBufRead"} {
		if !tripped[want] {
			t.Errorf("the scanner did not trip on %s — hits: %v", want, hits)
		}
	}
	for _, notWant := range []string{"mustNotTripPrintToStderr", "mustNotTripRecord", "mustNotTripContains"} {
		if tripped[notWant] {
			t.Errorf("the scanner tripped on %s, which is the correct shape — hits: %v", notWant, hits)
		}
	}
}

// scanErrorConstructors returns how many error constructors it visited and one
// report per banned read inside their arguments. The count is returned so a
// caller can fail on a scan that matched nothing.
func scanErrorConstructors(fset *token.FileSet, file *ast.File) (int, []string) {
	var hits []string
	visited := 0
	fn := ""

	ast.Inspect(file, func(n ast.Node) bool {
		if fd, ok := n.(*ast.FuncDecl); ok {
			fn = fd.Name.Name
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !errorConstructors[calleeName(call.Fun)] {
			return true
		}
		visited++
		for _, arg := range call.Args {
			for _, banned := range bannedReads(arg) {
				hits = append(hits, fmt.Sprintf("%s:%d: %s inside %s",
					fn, fset.Position(arg.Pos()).Line, banned, calleeName(call.Fun)))
			}
		}
		return true
	})
	sort.Strings(hits)
	return visited, hits
}

// bannedReads finds every rendering of the retained head inside one expression:
// a call to printHead, or any read of the buffer field it retains into.
func bannedReads(e ast.Expr) []string {
	var out []string
	ast.Inspect(e, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "printHead":
			out = append(out, "a call to headBuffer.printHead")
		case "buf":
			out = append(out, "a read of headBuffer.buf")
		}
		return true
	})
	return out
}

// calleeName renders a call's function as "pkg.Name" or "Name".
func calleeName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name + "." + x.Sel.Name
		}
		return x.Sel.Name
	}
	return ""
}

func mutationSources(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(".", n))
	}
	sort.Strings(out)
	return out
}
