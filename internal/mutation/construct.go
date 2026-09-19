package mutation

import "errors"

// Construct is one top-level syntactic construct of a source file — a
// function, class, statement or declaration directly under the file's root —
// as a line span plus the label a reader would recognise it by.
//
// WHY THE TOP LEVEL. A mutation tool keeps a mutant only when the mutant's
// whole location lies inside a requested range (Stryker's shouldMutate is
// locationIncluded, not overlap). Every ancestor of a changed line up to the
// file root can carry a mutant — the function body's block statement, the
// enclosing conditional — so the only range that guarantees no mutant covering
// that line goes ungenerated is the full span of the OUTERMOST construct below
// the root. Anything narrower is the line-pad heuristic again under another
// name.
type Construct struct {
	Start int    // first line, 1-based, inclusive
	End   int    // last line, 1-based, inclusive
	Kind  string // AST node kind, e.g. "FunctionDeclaration"
	Name  string // identifier where the node has one, else ""
}

// Label is the construct as it appears in provenance: "Kind Name", or the bare
// Kind when the node has no identifier (an expression statement, an anonymous
// default export). Never "Kind " with a trailing space — the record is read
// back by tests and by people.
func (c Construct) Label() string {
	if c.Name == "" {
		return c.Kind
	}
	return c.Kind + " " + c.Name
}

// ErrASTUnavailable is the sentinel every construct-resolution failure wraps:
// no parser on this machine, a file the parser refuses, a resolver that timed
// out. The verify layer matches it with errors.Is and falls the file open to
// whole-file mutation under one closed reason; the wrapped detail is what the
// degraded line prints.
var ErrASTUnavailable = errors.New("AST unavailable")

// ConstructResolver is the OPTIONAL half beside RangeRunner: an adapter that
// can say where a file's top-level constructs begin and end. A RangeRunner
// without it can still narrow — but only to the raw hunk, which is exactly
// the ungenerated-mutant hole AST-aware ranges exist to close, so verify
// treats that pairing as ErrASTUnavailable rather than ranging on bare hunks.
//
// file is repo-relative with forward slashes, the same key the scope's hunks
// use. The returned constructs are sorted by Start and pairwise disjoint; an
// empty slice is a legal answer for a file with no top-level nodes.
type ConstructResolver interface {
	Adapter
	Constructs(file string) ([]Construct, error)
}
