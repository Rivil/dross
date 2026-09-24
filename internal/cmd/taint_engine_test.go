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
// The engine is a worklist over SSA values. Each tainted value carries its
// ORIGINS: the source operations it derives from. Taint moves two ways:
//
//   - forward, along def-use: a conversion, slice, field read, load, concat or
//     extract of a tainted value is tainted, unless the result is an int or a
//     bool (clearance_model: a number or a truth value cannot carry text).
//     byte and rune are integers that DO carry text and never clear.
//   - reverse, into memory: writing taint INTO an object — a Store, a
//     WriteString on a builder, an Fprint into a buffer — taints the object,
//     found by walking the written-to pointer back to where it was allocated.
//     Every later read of that object is then tainted.
//
// External callees (no SSA body: the standard library, cobra) are judged by
// what they are, never by which binary produced the value:
//
//   - terminal: fmt.Print*, and a write to os.Stdout, os.Stderr, or cobra's
//     OutOrStdout/ErrOrStderr (terminal_definition). The value is on the
//     user's screen and goes no further.
//   - transform: a fixed set of standard-library packages that compute on
//     text. Results stay tainted (int/bool excepted); taint flows into the
//     objects the call writes to.
//   - everything else is an ESCAPE: errors.New, fmt.Errorf, os.WriteFile,
//     os.Setenv, the mutation recorder with text in it. An error is never
//     terminal.
//
// Module callees (with a body) receive the taint on their parameters and are
// followed. What this task does NOT yet follow is taint leaving a function by
// return, or by a write into memory its caller owns: both are reported as
// escapes — fail closed — until t-9 carries them across the boundary.

// taintOrigins is the set of source positions a value derives from.
type taintOrigins map[token.Pos]bool

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
}

// taintEngine is one run of a policy over a view.
type taintEngine struct {
	view    *srcView
	pol     *taintPolicy
	ids     taintIDs
	callees map[ssa.CallInstruction][]*ssa.Function
	scanned map[*ssa.Function]bool

	tainted     map[ssa.Value]taintOrigins
	queue       []ssa.Value
	reversed    map[ssa.Value]bool
	reversedPhi map[reverseKey]bool
	findings    map[string]*taintFinding
}

type reverseKey struct {
	v    ssa.Value
	deep bool
}

// taintIDs are the library objects the engine and the exec policy recognise,
// resolved by identity once per program. A nil entry means the program does
// not import that package, so nothing can match it.
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
	e := &taintEngine{
		view:        v,
		pol:         pol,
		ids:         resolveTaintIDs(v.Prog),
		callees:     taintCallees(v),
		scanned:     scannedFuncs(v),
		tainted:     map[ssa.Value]taintOrigins{},
		reversed:    map[ssa.Value]bool{},
		reversedPhi: map[reverseKey]bool{},
		findings:    map[string]*taintFinding{},
	}
	fns := make([]*ssa.Function, 0, len(e.scanned))
	for fn := range e.scanned {
		fns = append(fns, fn)
	}
	sort.Slice(fns, func(i, j int) bool { return fns[i].Pos() < fns[j].Pos() })
	for _, fn := range fns {
		pol.Seed(e, fn)
	}
	for len(e.queue) > 0 {
		v := e.queue[0]
		e.queue = e.queue[1:]
		e.forward(v)
	}
	out := make([]taintFinding, 0, len(e.findings))
	for _, f := range e.findings {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Escape, out[j].Escape
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return out[i].What < out[j].What
	})
	return out
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

// taint adds origins to v and queues it when it grew. A value of a clearing
// type, or one a marker clears, is never tainted.
func (e *taintEngine) taint(v ssa.Value, from taintOrigins) {
	if v == nil || len(from) == 0 || clearsType(v.Type()) {
		return
	}
	if e.pol.ClearAt != nil && e.pol.ClearAt(e.view.Fset.Position(defPos(v))) {
		return
	}
	cur := e.tainted[v]
	if cur == nil {
		cur = taintOrigins{}
		e.tainted[v] = cur
	}
	grew := false
	for o := range from {
		if !cur[o] {
			cur[o] = true
			grew = true
		}
	}
	if grew {
		e.queue = append(e.queue, v)
	}
}

// source taints v with a new origin at pos.
func (e *taintEngine) source(v ssa.Value, pos token.Pos) {
	e.taint(v, taintOrigins{pos: true})
}

// sourceInto marks the object v refers to as receiving the stream at pos.
func (e *taintEngine) sourceInto(v ssa.Value, pos token.Pos, at ssa.Instruction) {
	e.reverse(v, taintOrigins{pos: true}, at)
}

// escape records a finding at pos.
func (e *taintEngine) escape(pos token.Pos, fallback *ssa.Function, what string, from taintOrigins) {
	if !pos.IsValid() && fallback != nil {
		pos = fallback.Pos()
	}
	p := e.view.Fset.Position(pos)
	key := fmt.Sprintf("%s:%d:%s", p.Filename, p.Line, what)
	f := e.findings[key]
	if f == nil {
		f = &taintFinding{Escape: p, What: what}
		e.findings[key] = f
	}
	seen := map[string]bool{}
	for _, o := range f.Origins {
		seen[fmt.Sprintf("%s:%d", o.Filename, o.Line)] = true
	}
	for o := range from {
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
	from := e.tainted[v]
	refs := v.Referrers()
	if refs == nil {
		return
	}
	for _, instr := range *refs {
		switch in := instr.(type) {
		case *ssa.UnOp, *ssa.BinOp, *ssa.Convert, *ssa.ChangeType, *ssa.ChangeInterface,
			*ssa.MakeInterface, *ssa.SliceToArrayPointer, *ssa.MultiConvert, *ssa.TypeAssert,
			*ssa.Slice, *ssa.Field, *ssa.FieldAddr, *ssa.Index, *ssa.IndexAddr, *ssa.Lookup,
			*ssa.Phi, *ssa.Range, *ssa.Next:
			e.taint(instr.(ssa.Value), from)
		case *ssa.Extract:
			e.taint(in, from)
		case *ssa.MakeClosure:
			fn := in.Fn.(*ssa.Function)
			for i, b := range in.Bindings {
				if b == v && i < len(fn.FreeVars) {
					e.taint(fn.FreeVars[i], from)
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
		case *ssa.Return:
			e.escape(in.Pos(), in.Parent(), "is returned from "+funcLabel(in.Parent())+" (not yet followed to callers)", from)
		case *ssa.Panic:
			e.escape(in.Pos(), in.Parent(), "is a panic value", from)
		case ssa.CallInstruction:
			e.call(in, v, from)
		}
	}
}

// reverse records that taint is written INTO the object p refers to, walking p
// back to where that object came from so every other route to it reads taint.
func (e *taintEngine) reverse(p ssa.Value, from taintOrigins, at ssa.Instruction) {
	e.reverseMode(p, from, at, false)
}

// reverseThrough is reverse for an aggregate whose ELEMENTS are written into —
// io.MultiWriter's writers — rather than one that merely holds the value.
func (e *taintEngine) reverseThrough(p ssa.Value, from taintOrigins, at ssa.Instruction) {
	e.reverseMode(p, from, at, true)
}

// reverseMode walks p back to its object. With deep set, an allocated
// aggregate passes the write on to every object it holds a pointer to; without
// it, storing a tainted value into an argument array would taint everything
// else the array points at, the stream itself included.
func (e *taintEngine) reverseMode(p ssa.Value, from taintOrigins, at ssa.Instruction, deep bool) {
	if p == nil || e.isTerminal(p, map[ssa.Value]bool{}) {
		return
	}
	e.taint(p, from)
	switch x := p.(type) {
	case *ssa.Alloc:
		if deep {
			e.reverseContents(x, from, at)
		}
	case *ssa.FieldAddr:
		e.reverseMode(x.X, from, at, deep)
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
		key := reverseKey{x, deep}
		if e.reversedPhi[key] {
			return
		}
		e.reversedPhi[key] = true
		for _, edge := range x.Edges {
			e.reverseMode(edge, from, at, deep)
		}
	case *ssa.Extract:
		e.reverseMode(x.Tuple, from, at, deep)
	case *ssa.Call:
		e.reverseIntoResult(x, from, at)
	case *ssa.FreeVar:
		e.reverseFreeVar(x, from, at)
	case *ssa.Parameter:
		e.escape(at.Pos(), at.Parent(), "is written into parameter "+x.Name()+" of "+funcLabel(x.Parent())+" (not yet followed to callers)", from)
	case *ssa.Global:
		e.escape(at.Pos(), at.Parent(), "is stored in package variable "+x.RelString(nil), from)
	}
}

// reverseContents follows taint written into an allocated aggregate out to the
// objects it holds pointers to: taint written into io.MultiWriter's argument
// array reaches each writer stored in it.
func (e *taintEngine) reverseContents(a *ssa.Alloc, from taintOrigins, at ssa.Instruction) {
	if e.reversed[a] {
		return
	}
	e.reversed[a] = true
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
func (e *taintEngine) reverseIntoResult(call *ssa.Call, from taintOrigins, at ssa.Instruction) {
	for _, fn := range e.calleesOf(call) {
		if fn.Blocks != nil {
			continue
		}
		obj := calleeObject(fn)
		if obj == nil {
			continue
		}
		if !isTransformFunc(obj) {
			e.escape(at.Pos(), at.Parent(), "is written into the value returned by "+obj.FullName(), from)
			continue
		}
		e.reverseIntoReceivers(call.Common(), obj, from, at, nil)
	}
}

// reverseFreeVar maps taint written into a closure's captured variable back to
// the value each MakeClosure bound there.
func (e *taintEngine) reverseFreeVar(fv *ssa.FreeVar, from taintOrigins, at ssa.Instruction) {
	fn := fv.Parent()
	idx := -1
	for i, f := range fn.FreeVars {
		if f == fv {
			idx = i
		}
	}
	refs := fn.Referrers()
	if idx < 0 || refs == nil {
		return
	}
	for _, instr := range *refs {
		if mc, ok := instr.(*ssa.MakeClosure); ok && mc.Fn == fn && idx < len(mc.Bindings) {
			e.reverse(mc.Bindings[idx], from, at)
		}
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

// isTerminal reports whether v is a terminal writer: os.Stdout or os.Stderr,
// or cobra's OutOrStdout/ErrOrStderr, through any conversion or merge.
func (e *taintEngine) isTerminal(v ssa.Value, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return true
	}
	seen[v] = true
	switch x := v.(type) {
	case *ssa.Global:
		obj := x.Object()
		return obj != nil && (obj == types.Object(e.ids.osStdout) || obj == types.Object(e.ids.osStderr))
	case *ssa.UnOp:
		return x.Op == token.MUL && e.isTerminalGlobal(x.X)
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

func (e *taintEngine) isTerminalGlobal(v ssa.Value) bool {
	g, ok := v.(*ssa.Global)
	if !ok {
		return false
	}
	obj := g.Object()
	return obj != nil && (obj == types.Object(e.ids.osStdout) || obj == types.Object(e.ids.osStderr))
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
func (e *taintEngine) call(instr ssa.CallInstruction, v ssa.Value, from taintOrigins) {
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
		e.escape(instr.Pos(), instr.Parent(), "is passed to a call no callee could be resolved for", from)
		return
	}
	for _, fn := range callees {
		if fn.Blocks != nil {
			e.intoBody(fn, common, isRecv, argIdx, from)
			continue
		}
		e.external(instr, fn, isRecv, argIdx, from)
	}
}

// intoBody taints the parameters of a callee with a body that the tainted
// value binds to.
func (e *taintEngine) intoBody(fn *ssa.Function, common *ssa.CallCommon, isRecv bool, argIdx []int, from taintOrigins) {
	offset := 0
	if common.IsInvoke() {
		offset = 1
		if isRecv && len(fn.Params) > 0 {
			e.taint(fn.Params[0], from)
		}
	}
	for _, i := range argIdx {
		if j := i + offset; j < len(fn.Params) {
			e.taint(fn.Params[j], from)
		}
	}
}

// external judges a call into a function with no body.
func (e *taintEngine) external(instr ssa.CallInstruction, fn *ssa.Function, isRecv bool, argIdx []int, from taintOrigins) {
	obj := calleeObject(fn)
	if obj == nil {
		e.escape(instr.Pos(), instr.Parent(), "is passed to "+fn.String(), from)
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
		e.escape(instr.Pos(), instr.Parent(), "is passed to "+obj.FullName(), from)
		return
	}
	e.taintResults(instr, from)
	e.reverseIntoReceivers(common, obj, from, instr, argIdx)
}

// receiverOnly handles a method called ON a tainted object: its results carry
// the object's content, and anything it writes into receives it.
func (e *taintEngine) receiverOnly(instr ssa.CallInstruction, from taintOrigins) {
	e.taintResults(instr, from)
	common := instr.Common()
	obj := common.Method
	args := common.Args
	if !common.IsInvoke() {
		fn := common.StaticCallee()
		if fn == nil {
			return
		}
		if obj = calleeObject(fn); obj == nil || len(args) == 0 {
			return
		}
		args = args[1:]
	}
	for i, a := range args {
		if receivesData(obj, i, true) {
			e.reverse(a, from, instr)
		}
	}
}

// taintResults taints a call's results, component by component for a tuple:
// int and bool components clear, everything else — error included — carries.
func (e *taintEngine) taintResults(instr ssa.CallInstruction, from taintOrigins) {
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
func (e *taintEngine) reverseIntoReceivers(common *ssa.CallCommon, obj *types.Func, from taintOrigins, at ssa.Instruction, skip []int) {
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
func (e *taintEngine) builtin(instr ssa.CallInstruction, b *ssa.Builtin, v ssa.Value, from taintOrigins) {
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

// TestTaintedReturnFailsClosed: until taint is carried across function
// boundaries, a tainted return is itself an escape — never a silent drop.
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
