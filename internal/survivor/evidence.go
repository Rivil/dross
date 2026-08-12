package survivor

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Evidence answers the only two questions that decide a survivor's fate, and
// answers them from the code rather than from judgement:
//
//  1. Does a go-cover profile show this line executing?
//  2. Can this mutation operator alter this line's source at all?
//
// The cross product is the whole decision table, and it is worth stating
// because the wrong cell is how a coverage gap gets laundered into an
// "acceptable" survivor:
//
//	covered + applicable    → KILLABLE. A test can distinguish the mutant.
//	                          Accepting one of these is the laundering c-3
//	                          forbids: it silences a real gap with prose.
//	covered + inapplicable  → the operator does not apply. No test can kill it.
//	not covered + applicable → a plain coverage gap. Write the test.
//	not covered + inapplicable → nothing to kill and nothing to cover.
//
// Ceiling eligibility is a fifth answer layered on top: gremlins reported the
// line NOT COVERED while go-cover shows it executing. That contradiction — not
// the absence of coverage — is what the attribution-ceiling category rests on,
// so it REQUIRES coverage rather than being an excuse for the lack of it.
type Evidence struct {
	File string
	Line int
	Op   string

	// Coverage is what a go-cover profile says about this line.
	Coverage Coverage
	// Count is the highest execution count of any block covering the line.
	// Meaningless unless Coverage is CoverageCovered.
	Count int

	// Applicable is whether Op can alter this line's source text.
	Applicable Applicability

	// Killable is covered AND applicable: a test could tell the mutant apart.
	Killable bool

	// CeilingEligible marks the attribution-ceiling shape — the line runs, yet
	// the mutation tool reported it NOT COVERED. Only ever true for a line with
	// real coverage.
	CeilingEligible bool

	// Why is a one-line human summary, so a drain's output is readable without
	// the caller re-deriving the same sentence.
	Why string
}

// Coverage is a go-cover verdict for one line.
type Coverage string

const (
	// CoverageUnknown — no profile was available. Deliberately distinct from
	// CoverageNotCovered: "I did not look" must never read as evidence that a
	// line is uncoverable, which is exactly the shape of a false acceptance.
	CoverageUnknown Coverage = "unknown"
	// CoverageCovered — a profile block containing the line ran at least once.
	CoverageCovered Coverage = "covered"
	// CoverageNotCovered — the line sits in a coverage block that never ran,
	// or its file is absent from the profile entirely. Either way nothing
	// executed it, and a test could.
	CoverageNotCovered Coverage = "not covered"
	// CoverageNoBlock — the file IS in the profile but the line belongs to no
	// block at all. go-cover instruments statements, so a const initializer or
	// a declaration is not merely uncovered, it is uninstrumentable: no test
	// can ever make it covered, so the mutation tool will never build its
	// mutant and no test can ever kill it.
	//
	// Distinct from CoverageNotCovered because they demand opposite actions —
	// write a test, versus accept it — and collapsing them sends someone to
	// write a test that cannot possibly work.
	CoverageNoBlock Coverage = "no coverage block"
)

// Applicability is whether a mutation operator can change a line's source.
type Applicability string

const (
	// Applicable — the operator has something on this line to rewrite.
	Applicable Applicability = "applicable"
	// Inapplicable — the operator's swap does not produce valid, different
	// code here. ARITHMETIC_BASE over string concatenation is the case this
	// exists for: `+` → `-` on strings does not compile, so no test can ever
	// distinguish the mutant.
	Inapplicable Applicability = "inapplicable"
	// ApplicabilityUnknown — the line could not be parsed, or its operand
	// types are not decidable from the source alone. Never treated as
	// inapplicable: an unknown must not become an excuse.
	ApplicabilityUnknown Applicability = "unknown"
)

// Profile is a parsed go-cover profile: file path → covering blocks.
//
// A nil Profile is the "no profile" case and yields CoverageUnknown for every
// line, rather than the CoverageNotCovered that a zero-value map would.
type Profile struct {
	blocks map[string][]block
}

type block struct {
	startLine, endLine, count int
}

// ParseProfile reads a `go test -coverprofile` file. A missing file is not an
// error: it yields a nil Profile, and every lookup against it is unknown.
func ParseProfile(path string) (*Profile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	p := &Profile{blocks: map[string][]block{}}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		colon := strings.LastIndex(fields[0], ":")
		if colon < 0 {
			continue
		}
		file, span := fields[0][:colon], fields[0][colon+1:]
		from, to, ok := strings.Cut(span, ",")
		if !ok {
			continue
		}
		start, err1 := lineOf(from)
		end, err2 := lineOf(to)
		count, err3 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		p.blocks[file] = append(p.blocks[file], block{startLine: start, endLine: end, count: count})
	}
	return p, nil
}

func lineOf(s string) (int, error) {
	head, _, _ := strings.Cut(s, ".")
	return strconv.Atoi(head)
}

// CoverageAt reports the profile's verdict for a repo-relative file and line.
//
// Profile keys are import paths ("github.com/x/y/internal/cmd/doctor.go") while
// survivors are repo-relative ("internal/cmd/doctor.go"), so the match is on
// path suffix. A nil Profile is unknown, never uncovered.
func (p *Profile) CoverageAt(file string, line int) (Coverage, int) {
	if p == nil {
		return CoverageUnknown, 0
	}
	file = filepath.ToSlash(file)
	best, found, fileKnown := 0, false, false
	for key, blocks := range p.blocks {
		key = filepath.ToSlash(key)
		if key != file && !strings.HasSuffix(key, "/"+file) {
			continue
		}
		fileKnown = true
		for _, b := range blocks {
			if line < b.startLine || line > b.endLine {
				continue
			}
			found = true
			if b.count > best {
				best = b.count
			}
		}
	}
	switch {
	case found && best > 0:
		return CoverageCovered, best
	case found:
		// A real block that never ran: a coverage gap a test can close.
		return CoverageNotCovered, 0
	case fileKnown:
		// The file was instrumented, yet this line is in no block — go-cover
		// emits blocks for statements, so this is a const initializer or a
		// declaration. Uninstrumentable, not untested.
		return CoverageNoBlock, 0
	default:
		// The file is absent from the profile: its package was not tested at
		// all. Nothing ran the line, and a test could.
		return CoverageNotCovered, 0
	}
}

// ApplicabilityAt reports whether op can rewrite the given line's source.
//
// Only ARITHMETIC_BASE is decidable this way, and only for the case that
// matters: gremlins swaps `+` for `-`, and `-` is not defined on strings, so an
// additive expression with a string operand yields a mutant that cannot
// compile — never a behaviour any test could distinguish. Everything else
// answers Applicable (a conditional operator always has something to negate) or
// ApplicabilityUnknown when the line will not parse.
func ApplicabilityAt(repoRoot, file string, line int, op string) (Applicability, error) {
	if op != "ARITHMETIC_BASE" {
		return Applicable, nil
	}
	src, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(file)))
	if err != nil {
		return ApplicabilityUnknown, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		return ApplicabilityUnknown, fmt.Errorf("parse %s: %w", file, err)
	}

	verdict := ApplicabilityUnknown
	ast.Inspect(f, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok || fset.Position(bin.OpPos).Line != line {
			return true
		}
		switch bin.Op {
		case token.ADD:
			// A string operand anywhere in the additive chain settles it: Go
			// forbids mixing string and numeric operands, so one string proves
			// the whole chain is concatenation.
			if hasStringOperand(bin) {
				verdict = Inapplicable
				return true
			}
			if hasNumericOperand(bin) {
				verdict = Applicable
			}
		case token.SUB, token.MUL, token.QUO, token.REM:
			// Never string operations — `-`, `*`, `/`, `%` are numeric only,
			// so the operator applies by construction.
			verdict = Applicable
		}
		return true
	})
	return verdict, nil
}

// hasStringOperand reports whether the additive chain contains a string
// literal. Literals are the only operand type decidable without full type
// checking, which is the honest limit of this analysis: an all-identifier
// expression answers unknown rather than guessing.
func hasStringOperand(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if lit, ok := x.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			found = true
		}
		return !found
	})
	return found
}

func hasNumericOperand(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if lit, ok := x.(*ast.BasicLit); ok && (lit.Kind == token.INT || lit.Kind == token.FLOAT) {
			found = true
		}
		return !found
	})
	return found
}

// Derive attaches both facts to one survivor and reduces them to the two
// booleans a drain acts on.
//
// reportedNotCovered is what the MUTATION tool said about the line — gremlins'
// NOT COVERED status. It is deliberately a separate input from the go-cover
// profile: the attribution ceiling IS the disagreement between the two, and a
// deriver that only saw one of them could not detect it.
func Derive(repoRoot, file string, line int, op string, prof *Profile, reportedNotCovered bool) Evidence {
	e := Evidence{File: file, Line: line, Op: op}
	e.Coverage, e.Count = prof.CoverageAt(file, line)

	applicable, err := ApplicabilityAt(repoRoot, file, line, op)
	if err != nil {
		applicable = ApplicabilityUnknown
	}
	e.Applicable = applicable

	covered := e.Coverage == CoverageCovered
	e.CeilingEligible = covered && reportedNotCovered
	// The ceiling DOMINATES killability, and getting this backwards is the
	// whole trap. A ceiling-eligible mutant is covered and its operator does
	// apply, so it looks killable — but the mutation tool believes the line
	// never ran, and on that belief it never builds the mutant at all. No test
	// can change that: the tests already run the line. Reporting it killable
	// would send someone to write tests that cannot possibly move it.
	e.Killable = covered && e.Applicable == Applicable && !e.CeilingEligible

	switch {
	case e.CeilingEligible:
		e.Why = fmt.Sprintf("attribution ceiling: go-cover shows count %d, but the mutation tool reported "+
			"NOT COVERED and so never built the mutant — no test can kill it; accept it into the ceiling category",
			e.Count)
	case e.Killable:
		e.Why = fmt.Sprintf("killable: line runs (count %d) and %s applies — write the test, do not accept it", e.Count, op)
	case e.Applicable == Inapplicable:
		e.Why = op + " does not apply to this line (string operands) — no test can kill it"
	case e.Coverage == CoverageUnknown:
		e.Why = "no coverage profile — unknown, which is not evidence for anything"
	case e.Coverage == CoverageNoBlock:
		e.Why = "no coverage block: a const initializer or declaration, which go-cover never " +
			"instruments — the mutant can never be built, so no test can kill it; accept it"
	case e.Coverage == CoverageNotCovered:
		e.Why = "line never executes — this is a coverage gap, so cover it"
	default:
		e.Why = fmt.Sprintf("line runs (count %d) but %s applicability is undecidable from the source", e.Count, op)
	}
	return e
}
