package cmd

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"

	"github.com/Rivil/dross/internal/pathfence"
)

// The path-field taint scan: the second policy on the taint engine (c-4, c-7).
//
// The residual scan (pathfence_enum_test.go) looks at the ARGUMENT of an os.*
// call and asks whether it came out of a Contained. That cannot see a declared
// field copied into a local, joined onto a root, stored into another struct two
// functions away and opened there. This traces the value instead.
//
// Sources: every read — a Field or FieldAddr — of a field pathfence.Fields()
// declares a path, AND of every not_paths.txt row. The ledger's promise that a
// field is not a path is held to the same trace as the registry's. Each entry
// must resolve to a types.Var; one that does not is an "unresolved
// declaration", never a silent contribution of zero reads.
//
// Sinks: the string parameters of every package-level os function (bar the
// environment ones, whose strings are variable names and values), and opening a
// value through the pathfence seam — ReadFile, WriteFile, Stat.
//
// Clearing: pathfence.Contain clears a Consumed field, and nothing else does.
// A NotConsumed field or a ledger row keeps its taint through Contain, so
// opening it — even through the seam — is a finding at the opener's line.
// There is no marker: the fix is a Contained, never a comment, and never
// redeclaring the field NotConsumed or filing it in the ledger.
//
// Everything else a path touches is silent: an error, a return, a global, a
// print. Paths are meant to be shown; only opening one counts.

// pathTaintScanName is the WANT annotation this scan's corpora carry.
const pathTaintScanName = "pathtaint"

// pathReadFloor is ~25% under the declared-field reads the live tree holds
// today. A scan whose sources silently stopped matching would pass the gate by
// finding nothing; this fails first and says why.
const pathReadFloor = 1050

// pathTaintRemedy is what a finding tells its author to do.
const pathTaintRemedy = "construct a pathfence.Contained via pathfence.Contain and open it through " +
	"pathfence.ReadFile/WriteFile/Stat; a field declared NotConsumed or filed in " + notPathsLedger +
	" is a path after all — declare it Consumed in pathfence.Fields() with the Contained that carries it"

// pathClass is how a traced field is declared.
type pathClass int

const (
	pathConsumed    pathClass = iota // pathfence.Fields(), routed through Contain
	pathNotConsumed                  // pathfence.Fields(), declared never opened
	pathLedger                       // not_paths.txt, declared not a path
)

// pathSource is one declaration the scan traces.
type pathSource struct {
	Name       string // "changes.TaskRecord.Files"
	Class      pathClass
	LedgerLine int // not_paths.txt line, for a ledger row
}

func (s pathSource) String() string {
	switch s.Class {
	case pathConsumed:
		return "declared path field " + s.Name
	case pathNotConsumed:
		return "path field " + s.Name + " (declared NotConsumed: nothing may open it)"
	default:
		return fmt.Sprintf("field %s (filed at %s:%d as not a path)", s.Name, notPathsLedger, s.LedgerLine)
	}
}

// pathSources is every declaration to trace: the registry, then the ledger.
func pathSources(registry []pathfence.Field, ledger map[string]ledgerRow) []pathSource {
	var out []pathSource
	for _, f := range registry {
		c := pathConsumed
		if f.Consumed == nil {
			c = pathNotConsumed
		}
		out = append(out, pathSource{Name: f.Name(), Class: c})
	}
	for name, row := range ledger {
		out = append(out, pathSource{Name: name, Class: pathLedger, LedgerLine: row.Line})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// livePathSources is the live registry plus the live ledger.
func livePathSources(t *testing.T) []pathSource {
	t.Helper()
	return pathSources(pathfence.Fields(), readNotPaths(t))
}

// resolvePathSources maps each declaration to the struct field it names, by
// package NAME — the spelling the registry and ledger use — among the module's
// own packages (a fixture's included). A declaration naming nothing is an
// error: an entry that resolves to no types.Var would contribute zero reads and
// look exactly like a field nobody touches.
func resolvePathSources(prog *ssa.Program, srcs []pathSource) (map[*types.Var][]pathSource, []string) {
	byName := map[string][]*types.Package{}
	for _, p := range prog.AllPackages() {
		path := p.Pkg.Path()
		if path == modulePath || strings.HasPrefix(path, modulePath+"/") ||
			path == fixtureModule || strings.HasPrefix(path, fixtureModule+"/") {
			byName[p.Pkg.Name()] = append(byName[p.Pkg.Name()], p.Pkg)
		}
	}
	out := map[*types.Var][]pathSource{}
	var errs []string
	for _, s := range srcs {
		parts := strings.Split(s.Name, ".")
		if len(parts) != 3 {
			errs = append(errs, fmt.Sprintf("unresolved declaration %s: not <pkg>.<Type>.<Field>", s.Name))
			continue
		}
		found := false
		for _, pkg := range byName[parts[0]] {
			tn, ok := pkg.Scope().Lookup(parts[1]).(*types.TypeName)
			if !ok {
				continue
			}
			st, ok := tn.Type().Underlying().(*types.Struct)
			if !ok {
				continue
			}
			for i := 0; i < st.NumFields(); i++ {
				if f := st.Field(i); f.Name() == parts[2] {
					out[f] = append(out[f], s)
					found = true
				}
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("unresolved declaration %s: no such struct field in the module's packages, "+
				"so it would contribute zero reads to the scan", s.Name))
		}
	}
	sort.Strings(errs)
	return out, errs
}

// pathScan is one run of the path policy.
type pathScan struct {
	vars  map[*types.Var][]pathSource
	seeds map[token.Pos][]pathSource
}

// pathfencePkg is the seam's import path; its functions are judged, never
// followed.
const pathfencePkg = modulePath + "/internal/pathfence"

// osNotPathFuncs are the package-level os functions whose strings are not
// paths: environment variable names and values, a syscall name, a file label.
var osNotPathFuncs = map[string]bool{
	"Getenv": true, "LookupEnv": true, "Setenv": true, "Unsetenv": true,
	"ExpandEnv": true, "Expand": true, "NewSyscallError": true, "NewFile": true,
}

func (s *pathScan) policy() *taintPolicy {
	return &taintPolicy{
		Seed:      s.seed,
		Remedy:    pathTaintRemedy,
		OnlySinks: true,
		Opaque: func(obj *types.Func) bool {
			return obj.Pkg() != nil && obj.Pkg().Path() == pathfencePkg
		},
		Call:     s.call,
		External: s.external,
		Clears:   s.clears,
	}
}

// seed makes every read of a traced field a source. A FieldAddr used only as
// the address of a store is a write, not a read.
func (s *pathScan) seed(e *taintEngine, fn *ssa.Function) {
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			var fv *types.Var
			var val ssa.Value
			switch in := instr.(type) {
			case *ssa.FieldAddr:
				if storeOnly(in) {
					continue
				}
				fv, val = fieldVar(in.X.Type(), in.Field), in
			case *ssa.Field:
				fv, val = fieldVar(in.X.Type(), in.Field), in
			default:
				continue
			}
			if fv == nil {
				continue
			}
			srcs := s.vars[fv.Origin()]
			if len(srcs) == 0 {
				continue
			}
			pos := val.Pos()
			if !pos.IsValid() {
				pos = fn.Pos()
			}
			for _, src := range srcs {
				if !hasPathSource(s.seeds[pos], src.Name) {
					s.seeds[pos] = append(s.seeds[pos], src)
				}
			}
			e.source(val, pos)
		}
	}
}

func hasPathSource(ss []pathSource, name string) bool {
	for _, s := range ss {
		if s.Name == name {
			return true
		}
	}
	return false
}

// storeOnly reports whether every use of a field's address stores into it.
func storeOnly(fa *ssa.FieldAddr) bool {
	refs := fa.Referrers()
	if refs == nil || len(*refs) == 0 {
		return false
	}
	for _, r := range *refs {
		if st, ok := r.(*ssa.Store); !ok || st.Addr != fa {
			return false
		}
	}
	return true
}

// clears: Contain clears an origin only when every field read there is
// Consumed.
func (s *pathScan) clears(origin token.Pos) bool {
	srcs := s.seeds[origin]
	if len(srcs) == 0 {
		return false
	}
	for _, src := range srcs {
		if src.Class != pathConsumed {
			return false
		}
	}
	return true
}

// call judges the pathfence seam.
func (s *pathScan) call(e *taintEngine, instr ssa.CallInstruction, obj *types.Func, argIdx []int, isRecv bool, from tset) {
	sig := obj.Type().(*types.Signature)
	n := sig.Results().Len()
	switch {
	case obj.Name() == "Contain" && sig.Recv() == nil:
		// Contain(root, artifact, p): p is what it checks; a tainted root
		// passes straight through.
		for _, i := range argIdx {
			switch i {
			case 0:
				e.taintResult(instr, 0, n, from)
			case 2:
				e.taintResult(instr, 0, n, e.filter(from))
			}
		}
	case obj.Name() == "InTree" && sig.Recv() == nil:
		e.taintResult(instr, 0, n, from)
	case (obj.Name() == "String" || obj.Name() == "Rel") && sig.Recv() != nil:
		if isRecv || containsInt(argIdx, 0) {
			e.taintResult(instr, 0, n, from)
		}
	case obj.Name() == "ReadFile" || obj.Name() == "WriteFile" || obj.Name() == "Stat":
		if sig.Recv() == nil && containsInt(argIdx, 0) {
			e.report(instr.Pos(), "is opened through the pathfence seam (pathfence."+obj.Name()+")", from)
		}
	}
}

// external judges the package-level os functions: a traced value in a string
// parameter is a finding. Everything else falls through to the engine.
func (s *pathScan) external(e *taintEngine, instr ssa.CallInstruction, obj *types.Func, argIdx []int, _ bool, from tset) bool {
	if obj.Pkg() == nil || obj.Pkg().Path() != "os" {
		return false
	}
	sig := obj.Type().(*types.Signature)
	if sig.Recv() != nil || osNotPathFuncs[obj.Name()] {
		return false
	}
	for _, i := range argIdx {
		if i < sig.Params().Len() && types.Identical(sig.Params().At(i).Type().Underlying(), types.Typ[types.String]) {
			e.report(instr.Pos(), "reaches os."+obj.Name(), from)
			break
		}
	}
	return true
}

// pathFinding is one path-field finding: where it was opened, how, and every
// traced field read that flows there.
type pathFinding struct {
	Escape token.Position
	What   string
	Reads  []pathRead
}

// pathRead is one traced field read feeding a finding.
type pathRead struct {
	At     token.Position
	Source pathSource
}

func (f pathFinding) String() string {
	var reads []string
	for _, r := range f.Reads {
		reads = append(reads, fmt.Sprintf("%s (read at %s:%d)", r.Source, r.At.Filename, r.At.Line))
	}
	return fmt.Sprintf("%s:%d: %s %s — %s", f.Escape.Filename, f.Escape.Line,
		strings.Join(reads, ", "), f.What, pathTaintRemedy)
}

// pathTaintResult is one scan's output.
type pathTaintResult struct {
	Findings   []pathFinding
	Reads      int      // distinct traced field reads
	Unresolved []string // declarations naming no field
}

// pathTaintScan runs the path policy over a view.
func pathTaintScan(v *srcView, srcs []pathSource) pathTaintResult {
	vars, unresolved := resolvePathSources(v.Prog, srcs)
	s := &pathScan{vars: vars, seeds: map[token.Pos][]pathSource{}}
	raw, exact := runTaintOrigins(v, s.policy())

	out := pathTaintResult{Reads: len(s.seeds), Unresolved: unresolved}
	for i, f := range raw {
		pf := pathFinding{Escape: f.Escape, What: f.What}
		// By exact position, not by line: a field read beside a traced one
		// on the same line did not flow here, and naming it would send the
		// author to redeclare the wrong field.
		for o := range exact[i] {
			for _, src := range s.seeds[o] {
				pf.Reads = append(pf.Reads, pathRead{At: v.Fset.Position(o), Source: src})
			}
		}
		sort.Slice(pf.Reads, func(a, b int) bool {
			ra, rb := pf.Reads[a], pf.Reads[b]
			if ra.At.Filename != rb.At.Filename {
				return ra.At.Filename < rb.At.Filename
			}
			if ra.At.Offset != rb.At.Offset {
				return ra.At.Offset < rb.At.Offset
			}
			return ra.Source.Name < rb.Source.Name
		})
		out.Findings = append(out.Findings, pf)
	}
	return out
}

// pathTaintHits reduces findings to corpus hits.
func pathTaintHits(fs []pathFinding) []corpusHit {
	out := make([]corpusHit, 0, len(fs))
	for _, f := range fs {
		out = append(out, corpusHit{File: f.Escape.Filename, Line: f.Escape.Line, Msg: f.String()})
	}
	return out
}

// TestPathTaintRawOS: each way a declared field reaches an os call trips, and
// the finding names the field.
func TestPathTaintRawOS(t *testing.T) {
	fx := loadFixture(t, fixturePath("pathtaint", "raw_os.go.txt"))
	res := pathTaintScan(fx.srcView, livePathSources(t))
	assertCorpus(t, fx, pathTaintScanName, pathTaintHits(res.Findings))

	want := map[string]string{
		"readTaskFile":      "changes.TaskRecord.Files",
		"statEnvFile":       "project.Env.Files",
		"openJoined":        "changes.TaskRecord.Files",
		"lstatDoc":          "changes.RedProof.Doc",
		"removeCopied":      "survivor.Acceptance.File",
		"readVersionAsFile": "state.State.Version (filed at " + notPathsLedger,
	}
	for fn, field := range want {
		hit := false
		for _, f := range res.Findings {
			if enclosingFunc(fx.srcView, f.Escape) == fn && strings.Contains(f.String(), field) {
				hit = true
			}
		}
		if !hit {
			t.Errorf("%s: no finding names %s", fn, field)
		}
	}
	// Two fields read on one line: only the one that flows is named.
	for _, f := range res.Findings {
		if enclosingFunc(fx.srcView, f.Escape) != "readOneOfTwo" {
			continue
		}
		if !strings.Contains(f.String(), "changes.RedProof.Doc") || strings.Contains(f.String(), "state.State.Version") {
			t.Errorf("readOneOfTwo names the wrong fields: %s", f)
		}
	}
}

// enclosingFunc names the top-level function whose body holds a position.
func enclosingFunc(v *srcView, at token.Position) string {
	for _, p := range v.Pkgs {
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok {
					continue
				}
				s, e := v.Fset.Position(fd.Pos()), v.Fset.Position(fd.End())
				if s.Filename == at.Filename && s.Line <= at.Line && at.Line <= e.Line {
					return fd.Name.Name
				}
			}
		}
	}
	return ""
}

// TestPathTaintContainedOK: a Consumed field through Contain and the seam is
// clean, a printed field is clean, and a NotConsumed field through the same
// route trips at the opener's line.
func TestPathTaintContainedOK(t *testing.T) {
	fx := loadFixture(t, fixturePath("pathtaint", "contained_ok.go.txt"))
	res := pathTaintScan(fx.srcView, livePathSources(t))
	assertCorpus(t, fx, pathTaintScanName, pathTaintHits(res.Findings))
	for _, f := range res.Findings {
		if !strings.Contains(f.String(), "phase.Task.Files") {
			t.Errorf("a finding does not name phase.Task.Files: %s", f)
		}
	}
}

// TestPathTaintUnresolvedDeclaration: a declaration that names no struct field
// is reported, not silently traced as zero reads.
func TestPathTaintUnresolvedDeclaration(t *testing.T) {
	v := liveView(t)
	_, errs := resolvePathSources(v.Prog, []pathSource{
		{Name: "changes.TaskRecord.Files"},
		{Name: "changes.TaskRecord.NoSuchField"},
		{Name: "nosuchpkg.Type.Field"},
		{Name: "malformed"},
	})
	if len(errs) != 3 {
		t.Fatalf("got %d errors, want 3 (the resolvable entry must not be one): %v", len(errs), errs)
	}
	for _, e := range errs {
		if !strings.Contains(e, "unresolved declaration") {
			t.Errorf("%q does not say unresolved declaration", e)
		}
	}
}

// TestNoDeclaredPathFieldReachesOS is the live gate: every declaration
// resolves, the reads stay above the floor, and no traced field reaches an os
// call or the seam uncleared.
func TestNoDeclaredPathFieldReachesOS(t *testing.T) {
	res := pathTaintScan(liveView(t), livePathSources(t))
	for _, u := range res.Unresolved {
		t.Error(u)
	}
	if err := pathReadFloorErr(res.Reads); err != nil {
		t.Error(err)
	}
	for _, f := range res.Findings {
		t.Error(f.String())
	}
	t.Logf("traced %d declared-field reads", res.Reads)
}

// TestPathReadFloor: the floor passes at its minimum and fails one under it.
func TestPathReadFloor(t *testing.T) {
	if pathReadFloorErr(pathReadFloor) != nil || pathReadFloorErr(pathReadFloor-1) == nil {
		t.Error("the floor must pass at its minimum and fail one under it")
	}
}

func pathReadFloorErr(n int) error {
	if n < pathReadFloor {
		return fmt.Errorf("the path-field scan traced only %d declared-field reads, under the floor of %d — "+
			"its sources have stopped matching, and a clean gate over them is vacuous", n, pathReadFloor)
	}
	return nil
}
