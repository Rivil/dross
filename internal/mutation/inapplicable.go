package mutation

// Mutants that cannot be a real result, dropped before they cost anyone a
// decision.
//
// A const initializer has no coverage block at all — `TestConstInitializerHasNoCoverageBlock`
// in ceiling_test.go proves it against a live `go test -coverprofile` run. So
// gremlins reports an ARITHMETIC_BASE mutant there as NOT COVERED forever: no
// test can execute a line the coverage tool does not attribute, which means no
// test can ever kill it. Reported as a survivor it inflates the denominator and
// spends a human decision on something that has no other outcome available.
//
// The scope is deliberately narrow, and the narrowing is the load-bearing part.
// The idea that reached this phase asked to drop string-concatenation
// ARITHMETIC_BASE mutants too — and this repo's own evidence refutes that half.
// `TestStringConcatArithmeticMutantIsNoOp` shows the `+`→`-` swap on string
// operands does not compile, AND that gremlins only builds mutants on COVERED
// lines: cover a concat line and the mutant is compiled, fails, and comes back
// KILLED. A string-concat ARITHMETIC_BASE SURVIVOR is therefore always a
// coverage gap that covering the line would close — and dropping it would
// silence a real gap under the banner of removing a no-op.
//
// The rule this file follows, then: drop only what NO test could ever kill.
// Anything a test could kill stays, however unlikely it looks.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sync"
)

// DropInapplicable removes mutants that cannot be killed by any test, and
// recomputes the report's counters so every downstream surface agrees.
//
// root is the tree the report's paths are relative to. An empty root, an
// unreadable file or a file that does not parse leaves the report ALONE: the
// filter's job is to remove things it is certain about, and "I could not read
// the source" is not certainty. Silently dropping a mutant on a guess would be
// the same lie as reporting an unkillable one, pointing the other way.
func DropInapplicable(r *Report, root string) {
	if r == nil || root == "" || len(r.Surviving) == 0 {
		return
	}
	c := &constLineCache{root: root}
	kept := r.Surviving[:0]
	dropped := map[string]int{}
	for _, m := range r.Surviving {
		if isInapplicable(c, m) {
			dropped[m.File]++
			continue
		}
		kept = append(kept, m)
	}
	if len(dropped) == 0 {
		return
	}
	r.Surviving = kept
	for file, n := range dropped {
		r.Survived -= n
		r.NotCovered -= n
		// The per-file rows move with the totals. A row left behind would make
		// the file's own numbers disagree with the report's, which is how
		// scoping comes to score a mutant the list no longer holds.
		if s, ok := r.Files[file]; ok {
			s.Survived -= n
			s.NotCovered -= n
			r.Files[file] = s
		}
	}
	if r.Survived < 0 {
		r.Survived = 0
	}
	if r.NotCovered < 0 {
		r.NotCovered = 0
	}
	r.Score = PooledScore(r.Killed, r.Survived, r.Timeout)
}

// isInapplicable is the whole rule: an ARITHMETIC_BASE mutant sitting on a line
// inside a const declaration.
//
// Keyed on the OPERATOR AND the line's shape, never on the operator alone —
// ARITHMETIC_BASE mutants on ordinary arithmetic are exactly the mutants this
// tooling exists to surface, and a filter that dropped them by type would
// silence the signal while looking like housekeeping.
func isInapplicable(c *constLineCache, m Mutant) bool {
	if m.Op != "ARITHMETIC_BASE" {
		return false
	}
	return c.isConstLine(m.File, m.Line)
}

// constLineCache answers "is this line inside a const declaration?" by parsing
// each file once.
//
// go/ast rather than a regex on purpose: a const block spans lines, so the
// interesting case (`x = a + b` indented inside `const (`) carries no `const`
// token of its own, and a line-local regex cannot see the block it belongs to.
type constLineCache struct {
	root   string
	mu     sync.Mutex
	byFile map[string]map[int]bool
}

func (c *constLineCache) isConstLine(file string, line int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byFile == nil {
		c.byFile = map[string]map[int]bool{}
	}
	lines, ok := c.byFile[file]
	if !ok {
		lines = constLines(filepath.Join(c.root, file))
		c.byFile[file] = lines
	}
	return lines[line]
}

// constLines returns the set of source lines covered by const declarations.
// A file that cannot be read or parsed yields an empty set, which drops
// nothing — see DropInapplicable's note on certainty.
func constLines(path string) map[int]bool {
	out := map[int]bool{}
	src, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return out
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		start := fset.Position(gd.Pos()).Line
		end := fset.Position(gd.End()).Line
		for l := start; l <= end; l++ {
			out[l] = true
		}
	}
	return out
}
