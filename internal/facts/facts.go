package facts

import (
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/Rivil/dross/internal/architecture"
	"github.com/Rivil/dross/internal/phase"
)

// Finding is one citation in a phase's spec or plan that the tree does not
// back. Field names where it was cited ("criteria c-2", "task t-3
// description"); Kind is the fixed vocabulary: "missing" (a path), "moved" or
// "gone" (a file:line's symbol), "unresolved" (a symbol). Citation is the
// citation's canonical display form. NewLine is set only for "moved".
type Finding struct {
	Field    string
	Kind     string
	Citation string
	Status   architecture.Status
	NewLine  int
}

// String renders "<field>: <kind> <citation>[ now at line N]".
func (f Finding) String() string {
	s := fmt.Sprintf("%s: %s %s", f.Field, f.Kind, f.Citation)
	if f.Kind == KindMoved {
		s += fmt.Sprintf(" now at line %d", f.NewLine)
	}
	return s
}

// The fixed Finding.Kind vocabulary.
const (
	KindMissing    = "missing"
	KindMoved      = "moved"
	KindGone       = "gone"
	KindUnresolved = "unresolved"
)

// Report is the outcome of checking one phase. Unresolved is in spec order
// then plan order. Degraded means the symbol index could not cover every
// source file, so unmatched symbols were skipped rather than reported;
// DegradedWhy names the first file that caused it.
type Report struct {
	Unresolved  []Finding
	Degraded    bool
	DegradedWhy string
}

// scanned is one present-tense field's text and its display label.
type scanned struct {
	field, text string
}

// scannedFields lists the locked scanned_fields: spec criteria text and
// decision choices; plan task descriptions and test_contract lines. Every
// `why`, [[deferred]] entry and edge reason is history or future work and is
// never scanned. A task's `files` are the planned set, not claims.
func scannedFields(spec *phase.Spec, plan *phase.Plan) []scanned {
	var out []scanned
	for _, c := range spec.Criteria {
		out = append(out, scanned{"criteria " + c.ID, c.Text})
	}
	for _, d := range spec.Decisions {
		out = append(out, scanned{"decisions " + d.Key + " choice", d.Choice})
	}
	if plan == nil {
		return out
	}
	for _, t := range plan.Task {
		out = append(out, scanned{"task " + t.ID + " description", t.Description})
		for i, tc := range t.TestContract {
			out = append(out, scanned{"task " + t.ID + " test_contract[" + strconv.Itoa(i) + "]", tc})
		}
	}
	return out
}

// known is the set of citations the phase vouches for: every task's planned
// files, and the spec's `new` list. Keys are "p:<path>", "l:<path>:<line>"
// and "s:<symbol>", paths cleaned.
type known map[string]bool

func knownSet(spec *phase.Spec, plan *phase.Plan) known {
	k := known{}
	if plan != nil {
		for _, t := range plan.Task {
			for _, f := range t.Files {
				k["p:"+path.Clean(f)] = true
			}
		}
	}
	for _, e := range spec.New {
		k[newKey(e)] = true
	}
	return k
}

// newKey reads one spec `new` entry as the single key it vouches for: a
// file:line, a path, or else a symbol. It works on strings alone and never
// through Extract: the path-taint gate models a struct field program-wide, so
// an entry stored into a Citation (or a token) would read as the Path of every
// prose citation Check stats, and Spec.New is declared compared, never opened.
func newKey(e string) string {
	s := trimWrap(strings.TrimSuffix(strings.Trim(strings.TrimSpace(e), "`"), "()"))
	if m := lineRe.FindStringSubmatch(s); m != nil && isPath(m[1]) {
		if n, err := strconv.Atoi(m[2]); err == nil && n != 0 {
			return "l:" + path.Clean(m[1]) + ":" + strconv.Itoa(n)
		}
	}
	if isPath(s) {
		return "p:" + path.Clean(s)
	}
	return "s:" + s
}

func (k known) covers(c Citation) bool {
	switch c.Kind {
	case KindPath:
		return k["p:"+path.Clean(c.Path)]
	case KindLine:
		p := path.Clean(c.Path)
		return k["p:"+p] || k["l:"+p+":"+strconv.Itoa(c.Line)]
	case KindSymbol:
		return k["s:"+c.Symbol]
	}
	return false
}

// kindFor maps a citation's kind and resolved status to the Finding.Kind
// vocabulary, or "" when the status is not a finding (ok, ambiguous, skipped).
func kindFor(k Kind, st architecture.Status) string {
	if st != architecture.StatusMoved && st != architecture.StatusUnresolved {
		return ""
	}
	switch {
	case k == KindPath:
		return KindMissing
	case k == KindLine && st == architecture.StatusMoved:
		return KindMoved
	case k == KindLine:
		return KindGone
	case k == KindSymbol && st == architecture.StatusUnresolved:
		return KindUnresolved
	}
	return ""
}

// Check verifies every citation in the phase's scanned fields against the
// tree at repoDir. plan may be nil (a phase not yet planned): the spec is
// checked alone. It reads the tree and never writes.
func Check(repoDir string, spec *phase.Spec, plan *phase.Plan) (Report, error) {
	if spec == nil {
		return Report{}, errors.New("facts: Check needs a spec")
	}
	r := NewResolver(repoDir)
	k := knownSet(spec, plan)
	var rep Report

	for _, sc := range scannedFields(spec, plan) {
		for _, c := range Extract(sc.text) {
			if k.covers(c) {
				continue
			}
			res, err := r.check(c)
			if err != nil {
				return Report{}, fmt.Errorf("%s: %s: %w", sc.field, c.Raw, err)
			}
			if kind := kindFor(c.Kind, res.Status); kind != "" {
				if c.Kind == KindLine && res.Status == architecture.StatusUnresolved && res.fileMissing {
					kind = KindMissing
				}
				rep.Unresolved = append(rep.Unresolved, Finding{
					Field: sc.field, Kind: kind, Citation: c.Raw,
					Status: res.Status, NewLine: res.NewLine,
				})
			}
		}
	}
	rep.Degraded, rep.DegradedWhy = r.Degraded()
	return rep, nil
}

// checked is a Resolved plus whether a line citation failed on its file.
type checked struct {
	Resolved
	fileMissing bool
}

// check resolves one citation: a path or file:line first by the file's
// existence (a missing file is reported as missing whatever else it claims),
// then a paired line or a symbol through the resolver.
func (r *Resolver) check(c Citation) (checked, error) {
	if c.Kind == KindPath || c.Kind == KindLine {
		if _, err := containCitation(r.root, c.Path, r.Files); err != nil {
			if errors.Is(err, ErrMissing) {
				return checked{Resolved{Status: architecture.StatusUnresolved}, true}, nil
			}
			return checked{}, err
		}
		if c.Kind == KindPath {
			return checked{Resolved: Resolved{Status: architecture.StatusOK}}, nil
		}
		// The file exists, but a gitignored one is never in the indexed tree:
		// it is outside the index, and an unindexed file is never reported.
		files, err := r.Files()
		if err != nil {
			return checked{}, err
		}
		if len(locate(files, c.Path)) == 0 {
			return checked{Resolved: Resolved{Status: architecture.StatusSkipped}}, nil
		}
		res, err := r.Line(c)
		return checked{Resolved: res}, err
	}
	res, err := r.Symbol(c)
	return checked{Resolved: res}, err
}
