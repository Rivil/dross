package cmd

import (
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// The taint engine. A value derived from a spawned process's output must end
// at the terminal, or at an in-source marker at the conversion that turns it
// into something that is no longer the stream — never in an error, a file, an
// environment variable, or any other place that outlives the run.
//
// Within a function the engine is a worklist over SSA values. Each tainted
// value carries LABELS: the concrete source positions it derives from (its
// ORIGINS), and symbolic parameter labels standing for "whatever the caller
// passed here". Taint moves two ways:
//
//   - forward, along def-use: a conversion, slice, field read, load, concat or
//     extract of a tainted value is tainted, unless the result is an int or a
//     bool (clearance_model: a number or a truth value cannot carry text).
//     byte and rune are integers that DO carry text and never clear.
//   - reverse, into memory: writing taint INTO an object — a Store, a
//     WriteString on a builder, an Fprint into a buffer — taints the object,
//     found by walking the written-to pointer back to where it came from.
//
// Across functions, each function is summarised in terms of its parameters:
// which reach which results, which objects a parameter refers to are written
// into, which struct fields a parameter's taint is stored in, and which escapes
// fire only if a parameter is tainted. A call site applies its callee's summary
// to ITS arguments, so a helper called with output at one site and with a
// literal at another taints only the first (context-sensitive). Summaries are
// recomputed over the whole program until nothing changes, so recursion
// reaches a fixpoint.
//
// Memory that outlives a function is tracked concretely, program-wide: a struct
// field by its types.Var (a store to res.Other never taints res.Log), an
// allocation by its allocation site (a channel a goroutine sends on, a buffer a
// closure writes into), a closure's captured variable. An object plugged in
// where the stream is written — Cmd.Stdout, an io.MultiWriter — makes the
// []byte parameter of its own Write method a source.
//
// External callees (no SSA body: the standard library, cobra) are judged by
// what they are, never by which binary produced the value:
//
//   - terminal: fmt.Print*, and a write to os.Stdout, os.Stderr, cobra's
//     OutOrStdout/ErrOrStderr, or a package variable or struct field that only
//     ever holds one of those (terminal_definition). A generic io.Writer
//     parameter is NOT terminal: its taint is followed to each caller's
//     argument.
//   - transform: a fixed set of standard-library packages that compute on
//     text. Results stay tainted (int/bool excepted); taint flows into the
//     objects the call writes to.
//   - everything else is an ESCAPE: errors.New, fmt.Errorf, os.WriteFile,
//     os.Setenv, the mutation recorder with text in it. An error is never
//     terminal.
//
// Fail closed at the program's edge: a tainted value returned from a function
// nothing in the program calls — a cobra RunE closure, an unused export — or
// written into the parameter of one, is an escape, because whatever calls it
// is code this analysis cannot see.

// taintOrigins is the set of source positions a value derives from.
type taintOrigins map[token.Pos]bool

// tlabel is one taint label: a concrete origin, or (param >= 0) the symbolic
// "whatever the caller passed as parameter param". A filtered parameter label
// stands for only the part of the caller's taint the policy does not clear —
// what survives a clearing call (pathfence.Contain) inside a helper, resolved
// per call site.
type tlabel struct {
	origin   token.Pos
	param    int
	filtered bool
}

func originLabel(p token.Pos) tlabel { return tlabel{origin: p, param: -1} }
func paramLabel(i int) tlabel        { return tlabel{param: i} }

// tset is a set of labels.
type tset map[tlabel]bool

func fromOrigins(o taintOrigins) tset {
	s := tset{}
	for p := range o {
		s[originLabel(p)] = true
	}
	return s
}

// concrete is the set's origins.
func (s tset) concrete() taintOrigins {
	o := taintOrigins{}
	for l := range s {
		if l.param < 0 {
			o[l.origin] = true
		}
	}
	return o
}

// params is the set's parameter labels, sorted.
func (s tset) params() []int {
	var ps []int
	for l := range s {
		if l.param >= 0 {
			ps = append(ps, l.param)
		}
	}
	sort.Ints(ps)
	return ps
}

// add merges o into s and reports whether s grew.
func (s tset) add(o tset) bool {
	grew := false
	for l := range o {
		if !s[l] {
			s[l] = true
			grew = true
		}
	}
	return grew
}

func (s tset) key() string {
	var parts []string
	for l := range s {
		parts = append(parts, fmt.Sprintf("%d/%d/%v", l.origin, l.param, l.filtered))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// addOrigins merges o into dst[k] and reports whether it grew.
func addOrigins[K comparable](dst map[K]taintOrigins, k K, o taintOrigins) bool {
	if len(o) == 0 {
		return false
	}
	cur := dst[k]
	if cur == nil {
		cur = taintOrigins{}
		dst[k] = cur
	}
	grew := false
	for p := range o {
		if !cur[p] {
			cur[p] = true
			grew = true
		}
	}
	return grew
}

// taintFinding is one escape. It deliberately carries NO binary field: the
// verdict never sees which program was spawned, and a struct with nowhere to
// put that fact is a stronger guarantee than a comment saying so.
type taintFinding struct {
	Escape  token.Position
	What    string
	Origins []token.Position
}

func (f taintFinding) String() string {
	var origins []string
	for _, o := range f.Origins {
		origins = append(origins, fmt.Sprintf("%s:%d", o.Filename, o.Line))
	}
	return fmt.Sprintf("%s:%d: spawned-process output %s (origin %s)",
		f.Escape.Filename, f.Escape.Line, f.What, strings.Join(origins, ", "))
}

// taintPolicy is what varies between scans run on the engine.
type taintPolicy struct {
	// Seed marks the policy's sources in one function, calling e.source for a
	// value that carries taint and e.sourceInto for an object that receives it.
	Seed func(e *taintEngine, fn *ssa.Function)
	// ClearAt reports whether a value defined at pos is cleared by an
	// in-source marker. Nil clears nothing.
	ClearAt func(pos token.Position) bool
	// Remedy is appended to every finding's rendering by the scan's tests.
	Remedy string

	// The hooks below let a policy other than exec-output judge calls. Each
	// is optional; the exec policy sets none of them.

	// Opaque names functions the policy judges itself: the engine never
	// follows into them, and hands every tainted call to Call instead.
	Opaque func(obj *types.Func) bool
	// Call judges a tainted value reaching an Opaque function.
	Call func(e *taintEngine, instr ssa.CallInstruction, obj *types.Func, argIdx []int, isRecv bool, from tset)
	// External, when set, sees every call into a bodiless function first and
	// reports whether it judged it; false falls through to the engine's own
	// classification (terminal / transform / escape).
	External func(e *taintEngine, instr ssa.CallInstruction, obj *types.Func, argIdx []int, isRecv bool, from tset) bool
	// Clears reports whether a concrete origin is cleared by the policy's
	// clearing call; filtered labels drop exactly those origins.
	Clears func(origin token.Pos) bool
	// OnSource, when set, is told every origin the policy seeds — the source
	// census a gate's floor counts.
	OnSource func(pos token.Pos)
	// OnlySinks silences the engine's own structural escapes — a return or
	// write at the program's edge, a package variable, a panic, an
	// unresolved call. Only what the policy reports through e.report counts.
	OnlySinks bool
}

// condEscape is an escape site inside a function that fires only when the
// parameter it is filed under is tainted.
type condEscape struct {
	pos  token.Pos
	fn   *ssa.Function
	what string
	// filtered: only the caller's non-clearing taint fires it.
	filtered bool
}

// fnSummary is what a function does with taint, in terms of its parameters.
type fnSummary struct {
	results []tset
	writers map[int]tset
	writeAt map[int]token.Pos
	fields  map[int]map[*types.Var]bool
	fvs     map[int]map[*ssa.FreeVar]bool
	escapes map[int][]condEscape
}

func newSummary(nResults int) *fnSummary {
	s := &fnSummary{
		results: make([]tset, nResults),
		writers: map[int]tset{},
		writeAt: map[int]token.Pos{},
		fields:  map[int]map[*types.Var]bool{},
		fvs:     map[int]map[*ssa.FreeVar]bool{},
		escapes: map[int][]condEscape{},
	}
	for i := range s.results {
		s.results[i] = tset{}
	}
	return s
}

// key is a canonical rendering, for change detection between rounds.
func (s *fnSummary) key() string {
	var b strings.Builder
	for j, r := range s.results {
		fmt.Fprintf(&b, "r%d=%s;", j, r.key())
	}
	var ks []int
	for k := range s.writers {
		ks = append(ks, k)
	}
	sort.Ints(ks)
	for _, k := range ks {
		fmt.Fprintf(&b, "w%d=%s;", k, s.writers[k].key())
	}
	ks = ks[:0]
	for k := range s.fields {
		ks = append(ks, k)
	}
	sort.Ints(ks)
	for _, k := range ks {
		var fs []string
		for f := range s.fields[k] {
			fs = append(fs, fmt.Sprintf("%p", f))
		}
		sort.Strings(fs)
		fmt.Fprintf(&b, "f%d=%s;", k, strings.Join(fs, ","))
	}
	ks = ks[:0]
	for k := range s.fvs {
		ks = append(ks, k)
	}
	sort.Ints(ks)
	for _, k := range ks {
		var fs []string
		for f := range s.fvs[k] {
			fs = append(fs, fmt.Sprintf("%p", f))
		}
		sort.Strings(fs)
		fmt.Fprintf(&b, "v%d=%s;", k, strings.Join(fs, ","))
	}
	ks = ks[:0]
	for k := range s.escapes {
		ks = append(ks, k)
	}
	sort.Ints(ks)
	for _, k := range ks {
		var es []string
		for _, e := range s.escapes[k] {
			es = append(es, fmt.Sprintf("%d:%s:%v", e.pos, e.what, e.filtered))
		}
		sort.Strings(es)
		fmt.Fprintf(&b, "e%d=%s;", k, strings.Join(es, ","))
	}
	return b.String()
}

// taintEngine is one run of a policy over a view.
type taintEngine struct {
	view    *srcView
	pol     *taintPolicy
	ids     taintIDs
	callees map[ssa.CallInstruction][]*ssa.Function
	callers map[*ssa.Function]int
	funcs   []*ssa.Function

	summaries map[*ssa.Function]*fnSummary
	sumKeys   map[*ssa.Function]string

	// Program-wide concrete memory.
	objTaint   map[ssa.Value]taintOrigins
	fieldTaint map[*types.Var]taintOrigins
	fvTaint    map[*ssa.FreeVar]taintOrigins
	fvWrite    map[*ssa.FreeVar]taintOrigins
	paramSeed  map[*ssa.Parameter]taintOrigins

	termGlobals map[*ssa.Global]bool
	termFields  map[*types.Var]bool

	changed  bool
	findings map[string]*taintFinding
	// exact holds each finding's origins as positions, column and all: a
	// finding keeps one origin per line, and a policy that names what was
	// read at an origin needs to tell two reads on one line apart.
	exact map[string]taintOrigins
	st    *fnState
}

// fnState is one function's analysis in one round.
type fnState struct {
	fn       *ssa.Function
	tainted  map[ssa.Value]tset
	queue    []ssa.Value
	reversed map[reverseKey]bool
	sum      *fnSummary
}

type reverseKey struct {
	v    ssa.Value
	deep bool
}

type taintIDs struct {
	output, combinedOutput, stdoutPipe, stderrPipe *types.Func
	cmdStdout, cmdStderr, exitErrStderr            *types.Var
	osStdout, osStderr                             *types.Var
	cobraOut, cobraErr                             *types.Func
}

func resolveTaintIDs(prog *ssa.Program) taintIDs {
	var ids taintIDs
	for _, p := range prog.AllPackages() {
		switch p.Pkg.Path() {
		case "os/exec":
			cmd := namedIn(p.Pkg, "Cmd")
			ids.output = methodOf(cmd, "Output")
			ids.combinedOutput = methodOf(cmd, "CombinedOutput")
			ids.stdoutPipe = methodOf(cmd, "StdoutPipe")
			ids.stderrPipe = methodOf(cmd, "StderrPipe")
			ids.cmdStdout = fieldOf(cmd, "Stdout")
			ids.cmdStderr = fieldOf(cmd, "Stderr")
			ids.exitErrStderr = fieldOf(namedIn(p.Pkg, "ExitError"), "Stderr")
		case "os":
			ids.osStdout, _ = p.Pkg.Scope().Lookup("Stdout").(*types.Var)
			ids.osStderr, _ = p.Pkg.Scope().Lookup("Stderr").(*types.Var)
		case "github.com/spf13/cobra":
			cmd := namedIn(p.Pkg, "Command")
			ids.cobraOut = methodOf(cmd, "OutOrStdout")
			ids.cobraErr = methodOf(cmd, "ErrOrStderr")
		}
	}
	return ids
}

func namedIn(pkg *types.Package, name string) *types.Named {
	tn, _ := pkg.Scope().Lookup(name).(*types.TypeName)
	if tn == nil {
		return nil
	}
	n, _ := tn.Type().(*types.Named)
	return n
}

func methodOf(n *types.Named, name string) *types.Func {
	if n == nil {
		return nil
	}
	for i := 0; i < n.NumMethods(); i++ {
		if m := n.Method(i); m.Name() == name {
			return m
		}
	}
	return nil
}

func fieldOf(n *types.Named, name string) *types.Var {
	if n == nil {
		return nil
	}
	st, _ := n.Underlying().(*types.Struct)
	if st == nil {
		return nil
	}
	for i := 0; i < st.NumFields(); i++ {
		if f := st.Field(i); f.Name() == name {
			return f
		}
	}
	return nil
}

// taintCallees indexes the view's VTA graph by call site: what each dynamic
// call can reach. Static calls are read off the instruction itself.
func taintCallees(v *srcView) map[ssa.CallInstruction][]*ssa.Function {
	out := map[ssa.CallInstruction][]*ssa.Function{}
	for _, n := range v.VTA.Nodes {
		for _, e := range n.Out {
			if e.Site != nil {
				out[e.Site] = append(out[e.Site], e.Callee.Func)
			}
		}
	}
	return out
}

// scannedFuncs is every function with a body that belongs to one of the view's
// examined packages, closures included; generic origins are left to their
// instances.
func scannedFuncs(v *srcView) map[*ssa.Function]bool {
	pkgs := map[*ssa.Package]bool{}
	for _, p := range v.Pkgs {
		pkgs[p.SSA] = true
	}
	out := map[*ssa.Function]bool{}
	for fn := range ssautil.AllFunctions(v.Prog) {
		if fn.Blocks == nil || fn.Pkg == nil || !pkgs[fn.Pkg] {
			continue
		}
		if fn.TypeParams().Len() > 0 && len(fn.TypeArgs()) == 0 {
			continue
		}
		out[fn] = true
	}
	return out
}

// runTaint runs one policy over a view and returns its findings, sorted.
func runTaint(v *srcView, pol *taintPolicy) []taintFinding {
	fs, _ := runTaintOrigins(v, pol)
	return fs
}

// runTaintOrigins is runTaint that also returns, index for index, the exact
// origin positions behind each finding.
func runTaintOrigins(v *srcView, pol *taintPolicy) ([]taintFinding, []taintOrigins) {
	e := &taintEngine{
		view:        v,
		pol:         pol,
		ids:         resolveTaintIDs(v.Prog),
		callees:     taintCallees(v),
		callers:     map[*ssa.Function]int{},
		summaries:   map[*ssa.Function]*fnSummary{},
		sumKeys:     map[*ssa.Function]string{},
		objTaint:    map[ssa.Value]taintOrigins{},
		fieldTaint:  map[*types.Var]taintOrigins{},
		fvTaint:     map[*ssa.FreeVar]taintOrigins{},
		fvWrite:     map[*ssa.FreeVar]taintOrigins{},
		paramSeed:   map[*ssa.Parameter]taintOrigins{},
		termGlobals: map[*ssa.Global]bool{},
		termFields:  map[*types.Var]bool{},
		findings:    map[string]*taintFinding{},
		exact:       map[string]taintOrigins{},
	}
	for fn := range ssautil.AllFunctions(v.Prog) {
		if fn.Blocks == nil || (fn.TypeParams().Len() > 0 && len(fn.TypeArgs()) == 0) {
			continue
		}
		e.funcs = append(e.funcs, fn)
	}
	sort.Slice(e.funcs, func(i, j int) bool { return e.funcs[i].String() < e.funcs[j].String() })
	for fn, n := range v.VTA.Nodes {
		if fn == nil {
			continue
		}
		for _, in := range n.In {
			if in.Caller != nil && in.Caller.Func != nil && in.Caller.Func != fn {
				e.callers[fn]++
			}
		}
	}
	e.precomputeTerminal()

	const maxRounds = 200
	for round := 0; ; round++ {
		if round == maxRounds {
			panic(fmt.Sprintf("taint engine did not reach a fixpoint in %d rounds", maxRounds))
		}
		e.changed = false
		for _, fn := range e.funcs {
			e.analyze(fn)
		}
		if !e.changed {
			break
		}
	}

	// Fail closed at the program's edge: a write into the parameter of a
	// function nothing in the program calls reaches code this analysis cannot
	// see.
	for _, fn := range e.funcs {
		if e.callers[fn] > 0 || e.pol.OnlySinks {
			continue
		}
		s := e.summaries[fn]
		if s == nil {
			continue
		}
		for k, w := range s.writers {
			if o := w.concrete(); len(o) > 0 && k < len(fn.Params) {
				e.finding(s.writeAt[k], fn, "is written into parameter "+fn.Params[k].Name()+" of "+funcLabel(fn)+
					", which nothing in the program calls", o)
			}
		}
	}

	keys := make([]string, 0, len(e.findings))
	for k := range e.findings {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		fi, fj := e.findings[keys[i]], e.findings[keys[j]]
		a, b := fi.Escape, fj.Escape
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return fi.What < fj.What
	})
	out := make([]taintFinding, 0, len(keys))
	exact := make([]taintOrigins, 0, len(keys))
	for _, k := range keys {
		out = append(out, *e.findings[k])
		exact = append(exact, e.exact[k])
	}
	return out, exact
}

// analyze runs one function's round: seed, propagate, and store the summary.
func (e *taintEngine) analyze(fn *ssa.Function) {
	st := &fnState{
		fn:       fn,
		tainted:  map[ssa.Value]tset{},
		reversed: map[reverseKey]bool{},
		sum:      newSummary(fn.Signature.Results().Len()),
	}
	e.st = st

	for i, p := range fn.Params {
		e.taint(p, tset{paramLabel(i): true})
		if o := e.paramSeed[p]; len(o) > 0 {
			e.taint(p, fromOrigins(o))
		}
	}
	for _, fv := range fn.FreeVars {
		if o := e.fvTaint[fv]; len(o) > 0 {
			e.taint(fv, fromOrigins(o))
		}
	}
	e.pol.Seed(e, fn)
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch in := instr.(type) {
			case *ssa.Alloc, *ssa.MakeChan, *ssa.MakeSlice, *ssa.MakeMap:
				if o := e.objTaint[in.(ssa.Value)]; len(o) > 0 {
					e.taint(in.(ssa.Value), fromOrigins(o))
				}
			case *ssa.FieldAddr:
				if o := e.fieldTaint[fieldVar(in.X.Type(), in.Field)]; len(o) > 0 {
					e.taint(in, fromOrigins(o))
				}
			case *ssa.Field:
				if o := e.fieldTaint[fieldVar(in.X.Type(), in.Field)]; len(o) > 0 {
					e.taint(in, fromOrigins(o))
				}
			case *ssa.MakeClosure:
				cl := in.Fn.(*ssa.Function)
				for i, b := range in.Bindings {
					if i < len(cl.FreeVars) {
						if o := e.fvWrite[cl.FreeVars[i]]; len(o) > 0 {
							e.reverse(b, fromOrigins(o), in)
						}
					}
				}
			case ssa.CallInstruction:
				e.applyCall(in)
			}
		}
	}
	for len(st.queue) > 0 {
		v := st.queue[0]
		st.queue = st.queue[1:]
		e.forward(v)
	}

	if k := st.sum.key(); e.sumKeys[fn] != k || e.summaries[fn] == nil {
		e.sumKeys[fn] = k
		e.summaries[fn] = st.sum
		e.changed = true
	}
}

// clearsType is the clearance model: an int or a bool cannot carry text.
// byte (uint8) and rune (int32) can — they are what a decode loop rebuilds the
// stream from — so they never clear.
func clearsType(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		return false
	}
	switch b.Kind() {
	case types.Uint8, types.Int32:
		return false
	}
	return b.Info()&(types.IsBoolean|types.IsInteger) != 0
}

// defPos is where a value is defined, for the marker hook: the value's own
// position, else its tuple's, else its function's.
func defPos(v ssa.Value) token.Pos {
	if v.Pos().IsValid() {
		return v.Pos()
	}
	if ex, ok := v.(*ssa.Extract); ok && ex.Tuple.Pos().IsValid() {
		return ex.Tuple.Pos()
	}
	if instr, ok := v.(ssa.Instruction); ok && instr.Parent() != nil {
		return instr.Parent().Pos()
	}
	return token.NoPos
}

// taint adds labels to v in the current function and queues it when it grew.
// A value of a clearing type, or one a marker clears, is never tainted.
func (e *taintEngine) taint(v ssa.Value, from tset) bool {
	if v == nil || len(from) == 0 || clearsType(v.Type()) {
		return false
	}
	if e.pol.ClearAt != nil && e.pol.ClearAt(e.view.Fset.Position(defPos(v))) {
		return false
	}
	st := e.st
	cur := st.tainted[v]
	if cur == nil {
		cur = tset{}
		st.tainted[v] = cur
	}
	if cur.add(from) {
		st.queue = append(st.queue, v)
		return true
	}
	return false
}

// source taints v with a new origin at pos.
func (e *taintEngine) source(v ssa.Value, pos token.Pos) {
	if e.pol.OnSource != nil {
		e.pol.OnSource(pos)
	}
	e.taint(v, tset{originLabel(pos): true})
}

// sourceInto marks the object v refers to as receiving the stream at pos.
func (e *taintEngine) sourceInto(v ssa.Value, pos token.Pos, at ssa.Instruction) {
	if e.pol.OnSource != nil {
		e.pol.OnSource(pos)
	}
	e.reverse(v, tset{originLabel(pos): true}, at)
}

// escape reports one of the engine's own structural escapes. A policy with
// OnlySinks set does not count them.
func (e *taintEngine) escape(pos token.Pos, what string, from tset) {
	if e.pol.OnlySinks {
		return
	}
	e.report(pos, what, from)
}

// report records an escape at pos: at once for the concrete origins, and as a
// conditional escape in the summary for each parameter label.
func (e *taintEngine) report(pos token.Pos, what string, from tset) {
	st := e.st
	if o := from.concrete(); len(o) > 0 {
		e.finding(pos, st.fn, what, o)
	}
	for l := range from {
		if l.param >= 0 {
			e.addCondEscape(l.param, condEscape{pos: pos, fn: st.fn, what: what, filtered: l.filtered})
		}
	}
}

func (e *taintEngine) addCondEscape(p int, ce condEscape) {
	for _, have := range e.st.sum.escapes[p] {
		if have.pos == ce.pos && have.what == ce.what && have.filtered == ce.filtered {
			return
		}
	}
	e.st.sum.escapes[p] = append(e.st.sum.escapes[p], ce)
}

// clears reports whether the policy clears a concrete origin.
func (e *taintEngine) clears(origin token.Pos) bool {
	return e.pol.Clears != nil && e.pol.Clears(origin)
}

// filter is what survives a clearing call: concrete origins the policy does
// not clear, and every parameter label marked filtered, to be resolved per
// call site.
func (e *taintEngine) filter(from tset) tset {
	out := tset{}
	for l := range from {
		switch {
		case l.param >= 0:
			out[tlabel{param: l.param, filtered: true}] = true
		case !e.clears(l.origin):
			out[l] = true
		}
	}
	return out
}

// finding records an escape at pos with its origins.
func (e *taintEngine) finding(pos token.Pos, fn *ssa.Function, what string, origins taintOrigins) {
	if !pos.IsValid() && fn != nil {
		pos = fn.Pos()
	}
	p := e.view.Fset.Position(pos)
	key := fmt.Sprintf("%s:%d:%s", p.Filename, p.Line, what)
	f := e.findings[key]
	if f == nil {
		f = &taintFinding{Escape: p, What: what}
		e.findings[key] = f
		e.exact[key] = taintOrigins{}
	}
	for o := range origins {
		e.exact[key][o] = true
	}
	seen := map[string]bool{}
	for _, o := range f.Origins {
		seen[fmt.Sprintf("%s:%d", o.Filename, o.Line)] = true
	}
	for o := range origins {
		op := e.view.Fset.Position(o)
		k := fmt.Sprintf("%s:%d", op.Filename, op.Line)
		if !seen[k] {
			seen[k] = true
			f.Origins = append(f.Origins, op)
		}
	}
	sort.Slice(f.Origins, func(i, j int) bool {
		a, b := f.Origins[i], f.Origins[j]
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Line < b.Line
	})
}

// forward propagates v's taint to every instruction that uses it.
func (e *taintEngine) forward(v ssa.Value) {
	st := e.st
	from := st.tainted[v]
	refs := v.Referrers()
	if refs == nil {
		return
	}
	for _, instr := range *refs {
		switch in := instr.(type) {
		case *ssa.UnOp, *ssa.BinOp, *ssa.Convert, *ssa.ChangeType, *ssa.ChangeInterface,
			*ssa.MakeInterface, *ssa.SliceToArrayPointer, *ssa.MultiConvert, *ssa.TypeAssert,
			*ssa.Slice, *ssa.Field, *ssa.FieldAddr, *ssa.Index, *ssa.IndexAddr, *ssa.Lookup,
			*ssa.Phi, *ssa.Range, *ssa.Next, *ssa.Extract:
			e.taint(instr.(ssa.Value), from)
		case *ssa.MakeClosure:
			cl := in.Fn.(*ssa.Function)
			for i, b := range in.Bindings {
				if b == v && i < len(cl.FreeVars) {
					e.addFreeVar(cl.FreeVars[i], from)
				}
			}
		case *ssa.Store:
			// Plugging a writer into Cmd.Stdout/Stderr is the source, not a
			// write into the Cmd: the Cmd writes into that object.
			if in.Val == v && !e.isCmdStream(in.Addr) {
				e.reverse(in.Addr, from, in)
			}
		case *ssa.MapUpdate:
			if in.Key == v || in.Value == v {
				e.reverse(in.Map, from, in)
			}
		case *ssa.Send:
			if in.X == v {
				e.reverse(in.Chan, from, in)
			}
		case *ssa.Select:
			for _, s := range in.States {
				if s.Dir == types.SendOnly && s.Send == v {
					e.reverse(s.Chan, from, in)
				}
			}
		case *ssa.Return:
			for j, r := range in.Results {
				if r == v && j < len(st.sum.results) {
					st.sum.results[j].add(from)
				}
			}
			if e.callers[st.fn] == 0 {
				e.escape(in.Pos(), "is returned from "+funcLabel(st.fn)+" to a caller outside the program", from)
			}
		case *ssa.Panic:
			e.escape(in.Pos(), "is a panic value", from)
		case ssa.CallInstruction:
			e.call(in, v, from)
		}
	}
}

// reverse records that taint is written INTO the object p refers to, walking p
// back to where that object came from so every other route to it reads taint.
func (e *taintEngine) reverse(p ssa.Value, from tset, at ssa.Instruction) {
	e.reverseMode(p, from, at, false)
}

// reverseThrough is reverse for an aggregate whose ELEMENTS are written into —
// io.MultiWriter's writers — rather than one that merely holds the value.
func (e *taintEngine) reverseThrough(p ssa.Value, from tset, at ssa.Instruction) {
	e.reverseMode(p, from, at, true)
}

// reverseMode walks p back to its object. With deep set, an allocated
// aggregate passes the write on to every object it holds a pointer to; without
// it, storing a tainted value into an argument array would taint everything
// else the array points at, the stream itself included.
func (e *taintEngine) reverseMode(p ssa.Value, from tset, at ssa.Instruction, deep bool) {
	if p == nil || e.isTerminal(p, map[ssa.Value]bool{}) {
		return
	}
	st := e.st
	grew := e.taint(p, from)
	key := reverseKey{p, deep}
	if !grew && st.reversed[key] {
		return
	}
	st.reversed[key] = true
	e.seedWriteMethods(p.Type(), from.concrete())

	switch x := p.(type) {
	case *ssa.Alloc:
		e.addObject(x, from)
		if deep {
			e.reverseContents(x, from, at)
		}
	case *ssa.MakeChan:
		e.addObject(x, from)
	case *ssa.MakeSlice:
		e.addObject(x, from)
	case *ssa.MakeMap:
		e.addObject(x, from)
	case *ssa.FieldAddr:
		// Keyed by field: a write into res.Log taints every read of Log and
		// never res.Other.
		e.addField(fieldVar(x.X.Type(), x.Field), from)
	case *ssa.IndexAddr:
		e.reverseMode(x.X, from, at, deep)
	case *ssa.Slice:
		e.reverseMode(x.X, from, at, deep)
	case *ssa.Field:
		e.reverseMode(x.X, from, at, deep)
	case *ssa.Index:
		e.reverseMode(x.X, from, at, deep)
	case *ssa.Lookup:
		e.reverseMode(x.X, from, at, deep)
	case *ssa.MakeInterface:
		e.reverseMode(x.X, from, at, deep)
	case *ssa.ChangeInterface:
		e.reverseMode(x.X, from, at, deep)
	case *ssa.ChangeType:
		e.reverseMode(x.X, from, at, deep)
	case *ssa.Convert:
		e.reverseMode(x.X, from, at, deep)
	case *ssa.TypeAssert:
		e.reverseMode(x.X, from, at, deep)
	case *ssa.SliceToArrayPointer:
		e.reverseMode(x.X, from, at, deep)
	case *ssa.UnOp:
		if x.Op == token.MUL {
			e.reverseMode(x.X, from, at, deep)
		}
	case *ssa.Phi:
		for _, edge := range x.Edges {
			e.reverseMode(edge, from, at, deep)
		}
	case *ssa.Extract:
		e.reverseMode(x.Tuple, from, at, deep)
	case *ssa.Call:
		e.reverseIntoResult(x, from, at)
	case *ssa.FreeVar:
		// Written back into the captured variable: the function that made
		// the closure applies it at its MakeClosure next round.
		if addOrigins(e.fvWrite, x, from.concrete()) {
			e.changed = true
		}
	case *ssa.Parameter:
		idx := -1
		for i, prm := range st.fn.Params {
			if prm == x {
				idx = i
			}
		}
		if idx < 0 {
			return
		}
		w := st.sum.writers[idx]
		if w == nil {
			w = tset{}
			st.sum.writers[idx] = w
		}
		w.add(from)
		if _, ok := st.sum.writeAt[idx]; !ok {
			st.sum.writeAt[idx] = at.Pos()
		}
	case *ssa.Global:
		e.escape(at.Pos(), "is stored in package variable "+x.RelString(nil), from)
	}
}

// addObject records concrete taint in an allocation, program-wide.
func (e *taintEngine) addObject(v ssa.Value, from tset) {
	if addOrigins(e.objTaint, v, from.concrete()) {
		e.changed = true
	}
}

// addField records taint stored into field f: concrete origins program-wide,
// parameter labels in the summary; and taints this function's other reads of
// the same field at once.
func (e *taintEngine) addField(f *types.Var, from tset) {
	if f == nil {
		return
	}
	if addOrigins(e.fieldTaint, f, from.concrete()) {
		e.changed = true
	}
	st := e.st
	for _, p := range from.params() {
		if st.sum.fields[p] == nil {
			st.sum.fields[p] = map[*types.Var]bool{}
		}
		st.sum.fields[p][f] = true
	}
	for _, b := range st.fn.Blocks {
		for _, instr := range b.Instrs {
			switch in := instr.(type) {
			case *ssa.FieldAddr:
				if fieldVar(in.X.Type(), in.Field) == f {
					e.taint(in, from)
				}
			case *ssa.Field:
				if fieldVar(in.X.Type(), in.Field) == f {
					e.taint(in, from)
				}
			}
		}
	}
}

// addFreeVar records taint bound into a closure's captured variable.
func (e *taintEngine) addFreeVar(fv *ssa.FreeVar, from tset) {
	if addOrigins(e.fvTaint, fv, from.concrete()) {
		e.changed = true
	}
	st := e.st
	for _, p := range from.params() {
		if st.sum.fvs[p] == nil {
			st.sum.fvs[p] = map[*ssa.FreeVar]bool{}
		}
		st.sum.fvs[p][fv] = true
	}
}

// seedWriteMethods: an object the stream is written into has its own Write
// (or WriteString) called with the stream by whoever writes — exec's copying
// goroutine, io.MultiWriter — so that method's data parameter is a source.
func (e *taintEngine) seedWriteMethods(t types.Type, origins taintOrigins) {
	if len(origins) == 0 {
		return
	}
	if _, isIface := t.Underlying().(*types.Interface); isIface {
		return
	}
	cands := []types.Type{t}
	if _, isPtr := t.Underlying().(*types.Pointer); !isPtr {
		cands = append(cands, types.NewPointer(t))
	}
	for _, c := range cands {
		ms := e.view.Prog.MethodSets.MethodSet(c)
		for _, name := range []string{"Write", "WriteString"} {
			sel := ms.Lookup(nil, name)
			if sel == nil {
				continue
			}
			fn := e.view.Prog.MethodValue(sel)
			if fn == nil || fn.Blocks == nil || len(fn.Params) < 2 {
				continue
			}
			if addOrigins(e.paramSeed, fn.Params[1], origins) {
				e.changed = true
			}
		}
	}
}

// reverseContents follows taint written into an allocated aggregate out to the
// objects it holds pointers to: taint written into io.MultiWriter's argument
// array reaches each writer stored in it.
func (e *taintEngine) reverseContents(a *ssa.Alloc, from tset, at ssa.Instruction) {
	var walk func(v ssa.Value)
	walk = func(v ssa.Value) {
		refs := v.Referrers()
		if refs == nil {
			return
		}
		for _, instr := range *refs {
			switch in := instr.(type) {
			case *ssa.FieldAddr:
				walk(in)
			case *ssa.IndexAddr:
				walk(in)
			case *ssa.Store:
				if in.Addr == v && refersToMemory(in.Val.Type()) {
					e.reverse(in.Val, from, at)
				}
			}
		}
	}
	walk(a)
}

// reverseIntoResult handles taint written into the value a call returned. A
// transform-set constructor (io.MultiWriter, bufio.NewWriter) wraps its
// arguments, so the write reaches them; a module function's result is followed
// forward only; any other external result — an *os.File from os.Create — is
// somewhere the stream must not go.
func (e *taintEngine) reverseIntoResult(call *ssa.Call, from tset, at ssa.Instruction) {
	for _, fn := range e.calleesOf(call) {
		if fn.Blocks != nil {
			continue
		}
		obj := calleeObject(fn)
		if obj == nil {
			continue
		}
		if !isTransformFunc(obj) {
			e.escape(at.Pos(), "is written into the value returned by "+obj.FullName(), from)
			continue
		}
		e.reverseIntoReceivers(call.Common(), obj, from, at, nil)
	}
}

// refersToMemory reports whether a value of type t can carry a reference to
// another object that a write could reach.
func refersToMemory(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Interface, *types.Slice, *types.Map, *types.Chan:
		return true
	}
	return false
}

// precomputeTerminal finds the package variables and struct fields that only
// ever hold a terminal writer — `var out io.Writer = os.Stderr` never
// reassigned, an options struct's Out always set from cmd.OutOrStdout(). One
// store of anything else and the variable or field is not terminal.
func (e *taintEngine) precomputeTerminal() {
	gStores := map[*ssa.Global][]ssa.Value{}
	fStores := map[*types.Var][]ssa.Value{}
	for _, fn := range e.funcs {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				st, ok := instr.(*ssa.Store)
				if !ok {
					continue
				}
				switch a := st.Addr.(type) {
				case *ssa.Global:
					gStores[a] = append(gStores[a], st.Val)
				case *ssa.FieldAddr:
					if f := fieldVar(a.X.Type(), a.Field); f != nil {
						fStores[f] = append(fStores[f], st.Val)
					}
				}
			}
		}
	}
	allTerminal := func(vals []ssa.Value) bool {
		for _, v := range vals {
			if !e.isTerminal(v, map[ssa.Value]bool{}) {
				return false
			}
		}
		return len(vals) > 0
	}
	for changed := true; changed; {
		changed = false
		for g, vals := range gStores {
			if !e.termGlobals[g] && allTerminal(vals) {
				e.termGlobals[g] = true
				changed = true
			}
		}
		for f, vals := range fStores {
			if !e.termFields[f] && allTerminal(vals) {
				e.termFields[f] = true
				changed = true
			}
		}
	}
}

// isTerminal reports whether v is a terminal writer: os.Stdout or os.Stderr,
// cobra's OutOrStdout/ErrOrStderr, or a variable or field that only ever holds
// one, through any conversion or merge.
func (e *taintEngine) isTerminal(v ssa.Value, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return true
	}
	seen[v] = true
	switch x := v.(type) {
	case *ssa.Global:
		return e.isTerminalGlobal(x)
	case *ssa.UnOp:
		if x.Op != token.MUL {
			return false
		}
		switch addr := x.X.(type) {
		case *ssa.Global:
			return e.isTerminalGlobal(addr)
		case *ssa.FieldAddr:
			return e.termFields[fieldVar(addr.X.Type(), addr.Field)]
		}
	case *ssa.Field:
		return e.termFields[fieldVar(x.X.Type(), x.Field)]
	case *ssa.Call:
		if fn := x.Call.StaticCallee(); fn != nil {
			obj := calleeObject(fn)
			return obj != nil && (obj == types.Object(e.ids.cobraOut) || obj == types.Object(e.ids.cobraErr))
		}
	case *ssa.MakeInterface:
		return e.isTerminal(x.X, seen)
	case *ssa.ChangeInterface:
		return e.isTerminal(x.X, seen)
	case *ssa.ChangeType:
		return e.isTerminal(x.X, seen)
	case *ssa.TypeAssert:
		return e.isTerminal(x.X, seen)
	case *ssa.Phi:
		for _, edge := range x.Edges {
			if !e.isTerminal(edge, seen) {
				return false
			}
		}
		return len(x.Edges) > 0
	}
	return false
}

func (e *taintEngine) isTerminalGlobal(g *ssa.Global) bool {
	obj := g.Object()
	if obj != nil && (obj == types.Object(e.ids.osStdout) || obj == types.Object(e.ids.osStderr)) {
		return true
	}
	return e.termGlobals[g]
}

// isCmdStream reports whether addr is &cmd.Stdout or &cmd.Stderr of an
// exec.Cmd.
func (e *taintEngine) isCmdStream(addr ssa.Value) bool {
	fa, ok := addr.(*ssa.FieldAddr)
	if !ok {
		return false
	}
	f := fieldVar(fa.X.Type(), fa.Field)
	return f != nil && (f == e.ids.cmdStdout || f == e.ids.cmdStderr)
}

// calleesOf is what a call can reach: its static callee, else the VTA graph's
// callees for the site.
func (e *taintEngine) calleesOf(instr ssa.CallInstruction) []*ssa.Function {
	if fn := instr.Common().StaticCallee(); fn != nil {
		return []*ssa.Function{fn}
	}
	return e.callees[instr]
}

// calleeObject is the declared function or method a callee implements.
func calleeObject(fn *ssa.Function) *types.Func {
	if obj, ok := fn.Object().(*types.Func); ok {
		return obj
	}
	if o := fn.Origin(); o != nil {
		if obj, ok := o.Object().(*types.Func); ok {
			return obj
		}
	}
	return nil
}

func funcLabel(fn *ssa.Function) string {
	if fn == nil {
		return "a function"
	}
	return fn.RelString(nil)
}

// call handles a tainted value reaching a call, as receiver or argument.
func (e *taintEngine) call(instr ssa.CallInstruction, v ssa.Value, from tset) {
	common := instr.Common()
	if b, ok := common.Value.(*ssa.Builtin); ok {
		e.builtin(instr, b, v, from)
		return
	}
	isRecv := common.IsInvoke() && common.Value == v
	argIdx := []int{}
	for i, a := range common.Args {
		if a == v {
			argIdx = append(argIdx, i)
		}
	}
	if !isRecv && len(argIdx) == 0 {
		return // v is the function value being called
	}

	// A write to a terminal writer ends the stream on the user's screen,
	// whatever sits behind the interface.
	if common.IsInvoke() && !isRecv && isWriteMethod(common.Method.Name()) && e.isTerminal(common.Value, map[ssa.Value]bool{}) {
		return
	}

	callees := e.calleesOf(instr)
	if len(callees) == 0 {
		if common.IsInvoke() && isRecv {
			e.receiverOnly(instr, from)
			return
		}
		if common.IsInvoke() && isWriteMethod(common.Method.Name()) {
			e.reverse(common.Value, from, instr)
			return
		}
		e.escape(instr.Pos(), "is passed to a call no callee could be resolved for", from)
		return
	}
	withBody := false
	for _, fn := range callees {
		if obj := calleeObject(fn); obj != nil && e.pol.Opaque != nil && e.pol.Opaque(obj) {
			e.pol.Call(e, instr, obj, argIdx, isRecv, from)
			continue
		}
		if fn.Blocks != nil {
			withBody = true
			continue
		}
		e.external(instr, fn, isRecv, argIdx, from)
	}
	if withBody {
		e.applyCall(instr)
	}
}

// applyCall applies every body-bearing callee's summary to this call's
// arguments: results, writes into argument objects, field stores, captured
// variables, and the escapes that fire for a tainted argument.
func (e *taintEngine) applyCall(instr ssa.CallInstruction) {
	common := instr.Common()
	if _, ok := common.Value.(*ssa.Builtin); ok {
		return
	}
	args := common.Args
	if common.IsInvoke() {
		args = append([]ssa.Value{common.Value}, common.Args...)
	}
	st := e.st
	argL := func(p int) tset {
		if p < len(args) {
			return st.tainted[args[p]]
		}
		return nil
	}
	resolve := func(ts tset) tset {
		out := tset{}
		for l := range ts {
			if l.param < 0 {
				out[l] = true
				continue
			}
			for k := range argL(l.param) {
				switch {
				case !l.filtered:
					out[k] = true
				case k.param >= 0:
					out[tlabel{param: k.param, filtered: true}] = true
				case !e.clears(k.origin):
					out[k] = true
				}
			}
		}
		return out
	}
	for _, fn := range e.calleesOf(instr) {
		if fn.Blocks == nil {
			continue
		}
		if obj := calleeObject(fn); obj != nil && e.pol.Opaque != nil && e.pol.Opaque(obj) {
			continue
		}
		s := e.summaries[fn]
		if s == nil {
			continue
		}
		for j, rs := range s.results {
			if r := resolve(rs); len(r) > 0 {
				e.taintResult(instr, j, len(s.results), r)
			}
		}
		for k, ws := range s.writers {
			if k < len(args) {
				if r := resolve(ws); len(r) > 0 {
					e.reverse(args[k], r, instr)
				}
			}
		}
		for p, fs := range s.fields {
			if l := argL(p); len(l) > 0 {
				for f := range fs {
					e.addField(f, l)
				}
			}
		}
		for p, fvs := range s.fvs {
			if l := argL(p); len(l) > 0 {
				for fv := range fvs {
					e.addFreeVar(fv, l)
				}
			}
		}
		for p, es := range s.escapes {
			l := argL(p)
			if len(l) == 0 {
				continue
			}
			for _, ce := range es {
				o := taintOrigins{}
				for k := range l {
					switch {
					case k.param >= 0:
						e.addCondEscape(k.param, condEscape{pos: ce.pos, fn: ce.fn, what: ce.what, filtered: ce.filtered || k.filtered})
					case ce.filtered && e.clears(k.origin):
					default:
						o[k.origin] = true
					}
				}
				if len(o) > 0 {
					e.finding(ce.pos, ce.fn, ce.what, o)
				}
			}
		}
	}
}

// taintResult taints result j of a call with n results.
func (e *taintEngine) taintResult(instr ssa.CallInstruction, j, n int, from tset) {
	call, ok := instr.(*ssa.Call)
	if !ok {
		return
	}
	if n == 1 {
		e.taint(call, from)
		return
	}
	if refs := call.Referrers(); refs != nil {
		for _, r := range *refs {
			if ex, ok := r.(*ssa.Extract); ok && ex.Index == j {
				e.taint(ex, from)
			}
		}
	}
}

// external judges a call into a function with no body.
func (e *taintEngine) external(instr ssa.CallInstruction, fn *ssa.Function, isRecv bool, argIdx []int, from tset) {
	obj := calleeObject(fn)
	if obj == nil {
		e.escape(instr.Pos(), "is passed to "+fn.String(), from)
		return
	}
	if e.pol.External != nil && e.pol.External(e, instr, obj, argIdx, isRecv, from) {
		return
	}
	common := instr.Common()
	sig := obj.Type().(*types.Signature)
	recv := receiverValue(common, sig)
	onlyRecv := isRecv || (sig.Recv() != nil && !common.IsInvoke() && len(argIdx) == 1 && argIdx[0] == 0)

	if e.terminalCall(obj, common, recv) {
		return
	}
	if onlyRecv {
		e.receiverOnly(instr, from)
		return
	}
	if !isTransformFunc(obj) {
		e.escape(instr.Pos(), "is passed to "+obj.FullName(), from)
		return
	}
	// A write returns a count and an error about the write — never the bytes
	// written. The data lands in the destination, which reverse follows; the
	// call's own results stay clean, or every Write method that forwards to a
	// builder would return the stream it was handed.
	if !isWriteCall(obj) {
		e.taintResults(instr, from)
	}
	e.reverseIntoReceivers(common, obj, from, instr, argIdx)
}

// isWriteCall reports whether obj writes its data arguments into a
// destination: a Write-family method, or a writer-first fmt/io function.
func isWriteCall(obj *types.Func) bool {
	if obj.Type().(*types.Signature).Recv() != nil {
		return isWriteMethod(obj.Name())
	}
	if obj.Pkg() == nil {
		return false
	}
	switch obj.Pkg().Path() + "." + obj.Name() {
	case "fmt.Fprint", "fmt.Fprintf", "fmt.Fprintln", "io.WriteString", "io.Copy", "io.CopyN", "io.CopyBuffer":
		return true
	}
	return false
}

// receiverOnly handles a method called ON a tainted object: its results carry
// the object's content, and anything it writes into receives it.
func (e *taintEngine) receiverOnly(instr ssa.CallInstruction, from tset) {
	common := instr.Common()
	obj := common.Method
	args := common.Args
	if !common.IsInvoke() {
		fn := common.StaticCallee()
		if fn == nil {
			e.taintResults(instr, from)
			return
		}
		if obj = calleeObject(fn); obj == nil || len(args) == 0 {
			e.taintResults(instr, from)
			return
		}
		args = args[1:]
	}
	if !isWriteCall(obj) {
		e.taintResults(instr, from)
	}
	for i, a := range args {
		if receivesData(obj, i, true) {
			e.reverse(a, from, instr)
		}
	}
}

// taintResults taints a call's results, component by component for a tuple:
// int and bool components clear, everything else — error included — carries.
func (e *taintEngine) taintResults(instr ssa.CallInstruction, from tset) {
	call, ok := instr.(*ssa.Call)
	if !ok {
		return
	}
	if _, isTuple := call.Type().(*types.Tuple); !isTuple {
		e.taint(call, from)
		return
	}
	if refs := call.Referrers(); refs != nil {
		for _, r := range *refs {
			if ex, ok := r.(*ssa.Extract); ok {
				e.taint(ex, from)
			}
		}
	}
}

// reverseIntoReceivers sends taint into the objects a transform call writes
// to: a writer receiver, and every argument whose parameter receives data.
// skip lists the argument indices the taint came in on.
func (e *taintEngine) reverseIntoReceivers(common *ssa.CallCommon, obj *types.Func, from tset, at ssa.Instruction, skip []int) {
	sig := obj.Type().(*types.Signature)
	args := common.Args
	var recv ssa.Value
	switch {
	case common.IsInvoke():
		recv = common.Value
	case sig.Recv() != nil && len(args) > 0:
		recv = args[0]
		args = args[1:]
		shifted := make([]int, 0, len(skip))
		for _, s := range skip {
			shifted = append(shifted, s-1)
		}
		skip = shifted
	}
	if recv != nil && (hasWriteMethod(recv.Type()) || obj.Name() == "Encode") {
		e.reverse(recv, from, at)
	}
	for i, a := range args {
		if containsInt(skip, i) || !receivesData(obj, i, false) {
			continue
		}
		if sig.Variadic() && i >= sig.Params().Len()-1 {
			e.reverseThrough(a, from, at)
			continue
		}
		e.reverse(a, from, at)
	}
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// receivesData reports whether argument i of a transform call is written
// into. A writer (anything with a Write method) or a pointer is; so is a
// variadic of writers (io.MultiWriter). Decode targets and read buffers are
// named: an `any` or []byte parameter is otherwise only read, and treating
// every one as a destination would taint errors.Is's sentinel or a Sprintf
// argument's owner. fromRecv is set when the taint came in on the receiver.
func receivesData(obj *types.Func, i int, fromRecv bool) bool {
	sig := obj.Type().(*types.Signature)
	params := sig.Params()
	if i >= params.Len() {
		if !sig.Variadic() || params.Len() == 0 {
			return false
		}
		i = params.Len() - 1
	}
	name := obj.FullName()
	switch name {
	case "encoding/json.Unmarshal", "errors.As", "io.ReadFull", "io.ReadAtLeast":
		return i == 1
	case "(*encoding/json.Decoder).Decode":
		return i == 0
	case "fmt.Sscan", "fmt.Sscanf", "fmt.Sscanln", "fmt.Fscan", "fmt.Fscanf", "fmt.Fscanln":
		return i == params.Len()-1
	}
	if fromRecv && (obj.Name() == "Read" || obj.Name() == "ReadAt") {
		return i == 0
	}
	t := params.At(i).Type()
	if sig.Variadic() && i == params.Len()-1 {
		if s, ok := t.Underlying().(*types.Slice); ok {
			t = s.Elem()
		}
	}
	if _, ok := t.Underlying().(*types.Pointer); ok {
		return true
	}
	return hasWriteMethod(t)
}

// hasWriteMethod reports whether t's method set includes Write.
func hasWriteMethod(t types.Type) bool {
	return types.NewMethodSet(t).Lookup(nil, "Write") != nil
}

func receiverValue(common *ssa.CallCommon, sig *types.Signature) ssa.Value {
	if common.IsInvoke() {
		return common.Value
	}
	if sig.Recv() != nil && len(common.Args) > 0 {
		return common.Args[0]
	}
	return nil
}

func isWriteMethod(name string) bool {
	switch name {
	case "Write", "WriteString", "WriteByte", "WriteRune":
		return true
	}
	return false
}

// terminalCall reports whether an external call puts its data on the user's
// screen: fmt.Print* (implicit os.Stdout), a writer-first call whose writer is
// terminal, or a write method on a terminal receiver.
func (e *taintEngine) terminalCall(obj *types.Func, common *ssa.CallCommon, recv ssa.Value) bool {
	pkg := ""
	if obj.Pkg() != nil {
		pkg = obj.Pkg().Path()
	}
	sig := obj.Type().(*types.Signature)
	if sig.Recv() == nil {
		switch pkg + "." + obj.Name() {
		case "fmt.Print", "fmt.Printf", "fmt.Println":
			return true
		case "fmt.Fprint", "fmt.Fprintf", "fmt.Fprintln", "io.WriteString", "io.Copy", "io.CopyN", "io.CopyBuffer":
			return len(common.Args) > 0 && e.isTerminal(common.Args[0], map[ssa.Value]bool{})
		}
		return false
	}
	return isWriteMethod(obj.Name()) && recv != nil && e.isTerminal(recv, map[ssa.Value]bool{})
}

// builtin handles len/cap (an int: cleared by type), append, copy and the
// print builtins.
func (e *taintEngine) builtin(instr ssa.CallInstruction, b *ssa.Builtin, v ssa.Value, from tset) {
	switch b.Name() {
	case "print", "println":
		return // the runtime writes these to stderr
	case "copy":
		if args := instr.Common().Args; len(args) == 2 && args[1] == v {
			e.reverse(args[0], from, instr)
		}
	default:
		if call, ok := instr.(*ssa.Call); ok {
			e.taint(call, from)
		}
	}
}

// taintTransformPkgs is the fixed standard-library set whose functions compute
// on text: taint flows through them rather than out. Every package here must
// be the callee package of some corpus line (TestTaintTransformSetIsExercised),
// so an entry nobody proved stays out.
var taintTransformPkgs = map[string]bool{
	"strings":       true,
	"bytes":         true,
	"strconv":       true,
	"fmt":           true,
	"bufio":         true,
	"io":            true,
	"unicode":       true,
	"path/filepath": true,
	"sort":          true,
	"slices":        true,
	"regexp":        true,
	"encoding/json": true,
	"errors":        true,
}

// taintEscapeFuncs are transform-package functions that BUILD what outlives the
// run: an error is never terminal.
var taintEscapeFuncs = map[string]bool{
	"fmt.Errorf":  true,
	"errors.New":  true,
	"errors.Join": true,
}

func isTransformFunc(obj *types.Func) bool {
	if obj.Pkg() == nil {
		return false
	}
	path := obj.Pkg().Path()
	if !taintTransformPkgs[path] {
		return false
	}
	if obj.Type().(*types.Signature).Recv() == nil && taintEscapeFuncs[path+"."+obj.Name()] {
		return false
	}
	return true
}

// execTaintRemedy is what a finding tells its author to do.
const execTaintRemedy = "print it to os.Stderr (or the command's ErrOrStderr) and return fixed prose, " +
	"or mark the conversion with " + taintClearedMarker + " <reason>"

// execTaintPolicy is the exec-output policy. Every exec.Cmd is a source — no
// binary is classed safe (taint_sources): its Output/CombinedOutput bytes, its
// StdoutPipe/StderrPipe readers, the Stderr an *exec.ExitError carries, and
// whatever object is plugged into Cmd.Stdout or Cmd.Stderr.
func execTaintPolicy() *taintPolicy {
	return &taintPolicy{Seed: seedExecSources, Remedy: execTaintRemedy}
}

func seedExecSources(e *taintEngine, fn *ssa.Function) {
	ids := e.ids
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch in := instr.(type) {
			case ssa.CallInstruction:
				call, ok := in.(*ssa.Call)
				if !ok {
					continue
				}
				for _, callee := range e.calleesOf(in) {
					obj := types.Object(calleeObject(callee))
					if obj == nil {
						continue
					}
					switch obj {
					case types.Object(ids.output), types.Object(ids.combinedOutput),
						types.Object(ids.stdoutPipe), types.Object(ids.stderrPipe):
						// Only the first result is the stream: an Output
						// error renders "exit status N"; its Stderr is read
						// separately below.
						seedFirstResult(e, call)
					}
				}
			case *ssa.FieldAddr:
				if fieldVar(in.X.Type(), in.Field) == ids.exitErrStderr && ids.exitErrStderr != nil {
					e.source(in, in.Pos())
				}
			case *ssa.Field:
				if fieldVar(in.X.Type(), in.Field) == ids.exitErrStderr && ids.exitErrStderr != nil {
					e.source(in, in.Pos())
				}
			case *ssa.Store:
				fa, ok := in.Addr.(*ssa.FieldAddr)
				if !ok {
					continue
				}
				if f := fieldVar(fa.X.Type(), fa.Field); f != nil && (f == ids.cmdStdout || f == ids.cmdStderr) {
					pos := in.Pos()
					if !pos.IsValid() {
						pos = fa.Pos()
					}
					e.sourceInto(in.Val, pos, in)
				}
			}
		}
	}
}

func seedFirstResult(e *taintEngine, call *ssa.Call) {
	refs := call.Referrers()
	if refs == nil {
		return
	}
	for _, r := range *refs {
		if ex, ok := r.(*ssa.Extract); ok && ex.Index == 0 {
			e.source(ex, call.Pos())
		}
	}
}

// fieldVar is the struct field a Field/FieldAddr selects, through a pointer.
func fieldVar(t types.Type, idx int) *types.Var {
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok || idx >= st.NumFields() {
		return nil
	}
	return st.Field(idx)
}

// taintHits reduces findings to corpus hits.
func taintHits(fs []taintFinding) []corpusHit {
	out := make([]corpusHit, 0, len(fs))
	for _, f := range fs {
		out = append(out, corpusHit{File: f.Escape.Filename, Line: f.Escape.Line, Msg: f.String()})
	}
	return out
}

// TestExecTaintSources: every kind of spawn-output source trips when it
// reaches an error — under an aliased os/exec import and through a promoted
// method too — and a value that only looks like one does not.
func TestExecTaintSources(t *testing.T) {
	fx := loadFixture(t, fixturePath("taint", "sources.go.txt"))
	assertCorpus(t, fx, "taint", taintHits(runTaint(fx.srcView, execTaintPolicy())))
}

// TestExecTaintSinks: terminal, transform and escape classification, the
// clearance model, and the recorder's shape.
func TestExecTaintSinks(t *testing.T) {
	fx := loadFixture(t, fixturePath("taint", "sinks.go.txt"))
	assertCorpus(t, fx, "taint", taintHits(runTaint(fx.srcView, execTaintPolicy())))
}

// TestExecTaintVerdictIgnoresBinaryName: the same corpus spawning "cargo"
// instead of "git" yields byte-identical findings, and the finding type has no
// field a binary name could live in.
func TestExecTaintVerdictIgnoresBinaryName(t *testing.T) {
	live := liveImportable(t)
	render := func(swap bool) []string {
		srcs, err := readFixtureSources(fixturePath("taint", "sources.go.txt"), fixturePath("taint", "sinks.go.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if swap {
			n := 0
			for i := range srcs {
				n += strings.Count(string(srcs[i].Src), `"git"`)
				srcs[i].Src = []byte(strings.ReplaceAll(string(srcs[i].Src), `"git"`, `"cargo"`))
			}
			if n == 0 {
				t.Fatal(`the corpus spawns no "git" — the swap proves nothing`)
			}
		}
		var out []string
		for _, s := range srcs {
			fx, err := typecheckFixture(live, []fixtureSource{s})
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range runTaint(fx.srcView, execTaintPolicy()) {
				out = append(out, f.String())
			}
		}
		return out
	}
	git, cargo := render(false), render(true)
	if strings.Join(git, "\n") != strings.Join(cargo, "\n") {
		t.Errorf("findings depend on the binary name:\ngit:\n%s\ncargo:\n%s", strings.Join(git, "\n"), strings.Join(cargo, "\n"))
	}
	if len(git) == 0 {
		t.Error("the corpus produced no findings — the comparison is vacuous")
	}
	var fields []string
	rt := reflect.TypeOf(taintFinding{})
	for i := 0; i < rt.NumField(); i++ {
		fields = append(fields, rt.Field(i).Name)
	}
	if got := strings.Join(fields, ","); got != "Escape,What,Origins" {
		t.Errorf("taintFinding fields = %s; a new field could carry the binary name", got)
	}
}

// TestTaintTransformSetIsExercised: every transform-set package is the callee
// package of at least one corpus call, so no package sits in the set on
// faith.
func TestTaintTransformSetIsExercised(t *testing.T) {
	fx := loadFixture(t, fixturePath("taint", "sources.go.txt"), fixturePath("taint", "sinks.go.txt"))
	called := map[string]bool{}
	callees := taintCallees(fx.srcView)
	for fn := range scannedFuncs(fx.srcView) {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				ci, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				targets := callees[ci]
				if sc := ci.Common().StaticCallee(); sc != nil {
					targets = []*ssa.Function{sc}
				}
				for _, c := range targets {
					if obj := calleeObject(c); obj != nil && obj.Pkg() != nil {
						called[obj.Pkg().Path()] = true
					}
				}
			}
		}
	}
	for pkg := range taintTransformPkgs {
		if !called[pkg] {
			t.Errorf("transform-set package %s is the callee package of no corpus line", pkg)
		}
	}
}

// TestClearsTypeIsIntAndBoolOnly pins the clearance model's type rule.
func TestClearsTypeIsIntAndBoolOnly(t *testing.T) {
	for _, tc := range []struct {
		t    types.Type
		want bool
	}{
		{types.Typ[types.Int], true},
		{types.Typ[types.Int64], true},
		{types.Typ[types.Bool], true},
		{types.Typ[types.Uint8], false}, // byte
		{types.Typ[types.Int32], false}, // rune
		{types.Typ[types.String], false},
		{types.NewSlice(types.Typ[types.Byte]), false},
		{types.Typ[types.Float64], false},
	} {
		if got := clearsType(tc.t); got != tc.want {
			t.Errorf("clearsType(%s) = %v, want %v", tc.t, got, tc.want)
		}
	}
}

// TestTaintClearAtHook: the marker hook is consulted for every value the
// engine would taint. A hook that clears everything leaves no finding; the
// corpus without it has some.
func TestTaintClearAtHook(t *testing.T) {
	fx := loadFixture(t, fixturePath("taint", "sources.go.txt"))
	if len(runTaint(fx.srcView, execTaintPolicy())) == 0 {
		t.Fatal("the source corpus produced no findings — the hook comparison is vacuous")
	}
	pol := execTaintPolicy()
	pol.ClearAt = func(token.Position) bool { return true }
	if got := runTaint(fx.srcView, pol); len(got) != 0 {
		t.Errorf("a ClearAt that clears every position left %d finding(s): %v", len(got), got)
	}
}

// TestTaintedReturnFailsClosed: a tainted value returned from a function
// nothing in the program calls goes to code this analysis cannot see — a cobra
// RunE closure's caller, an unused export's — so the return is itself an
// escape, never a silent drop. (A function with callers hands its result to
// them instead: TestTaintCrossesReturns.)
func TestTaintedReturnFailsClosed(t *testing.T) {
	src := "package ret\n\nimport (\n\t\"os/exec\"\n\t\"strings\"\n)\n\n" +
		"func rev(c *exec.Cmd) string {\n\tout, _ := c.Output()\n\treturn strings.TrimSpace(string(out))\n}\n"
	fx, err := typecheckFixture(liveImportable(t), []fixtureSource{{Name: "ret.go", Src: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	got := runTaint(fx.srcView, execTaintPolicy())
	if len(got) != 1 || got[0].Escape.Line != 10 || !strings.Contains(got[0].What, "returned") {
		t.Errorf("findings = %v, want one at ret.go:10 naming the return", got)
	}
}
