// Package gate is the engine behind dross's Claude Code tool-call gates: the
// PreToolUse check that can refuse a call before it runs, and the PostToolUse
// recorders that note what a finished call established.
//
// A Gate claims the calls it has an opinion on and judges each one. Check runs
// every registered gate over a payload and joins the refusals; a refused call
// never executes, and Claude reads the refusal — the rule it broke and the
// sanctioned path — as the call's result.
//
// Two scopes (guard_scope). AlwaysOn gates — the secret guards — fire in every
// directory, because the leaks they exist for happened outside dross repos.
// Workflow gates fire only when the call targets a dross repo, and allow
// everything elsewhere.
//
// Error posture (gate_error_posture). A call a gate claims but cannot judge —
// state unreadable, git failing, the gate itself panicking — is refused naming
// the error: a gate that fails open stops protecting without anyone noticing.
// A payload that cannot be read at all passes with a warning: failing closed
// there would block every tool call the day the hook schema changes. A human
// lifts a misbehaving liftable gate with `dross gate off`, which the agent
// itself cannot run (gate_override).
package gate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/shellscan"
)

// Scope says where a gate fires.
type Scope int

const (
	// AlwaysOn gates fire in every directory.
	AlwaysOn Scope = iota
	// Workflow gates fire only for calls that target a dross repo.
	Workflow
)

func (s Scope) String() string {
	if s == Workflow {
		return "workflow"
	}
	return "always-on"
}

// Gate is one rule over tool calls.
type Gate struct {
	Name  string
	Scope Scope
	// Liftable gates can be lifted by a human with `dross gate off`; the
	// guards that protect the off switch itself cannot.
	Liftable bool
	// Extensible gates match against lists gates.toml extends. While that
	// file is malformed they refuse every call they claim, naming it.
	Extensible bool
	// Claims reports whether the gate has an opinion on the call. It must be
	// cheap and must not touch the filesystem or spawn anything: it runs for
	// every tool call.
	Claims func(*Call) bool
	// Judge decides a claimed call: nil allows, a Refusal refuses, and an
	// error refuses naming it.
	Judge func(*Call) (*Refusal, error)
}

// Recorder notes what a finished tool call established. It never blocks: a
// recorder error is a warning.
type Recorder struct {
	Name   string
	Scope  Scope
	Claims func(*Call) bool
	Record func(*Call) error
}

var (
	registry  []Gate
	recorders []Recorder
)

// Register adds a gate. It is called from init, so it never panics: a
// malformed gate is caught by Validate in the tests, and at run time by the
// per-gate recovery in Check.
func Register(g Gate) { registry = append(registry, g) }

// RegisterRecorder adds a recorder.
func RegisterRecorder(r Recorder) { recorders = append(recorders, r) }

// All returns the registered gates in registration order.
func All() []Gate { return append([]Gate(nil), registry...) }

// Recorders returns the registered recorders.
func Recorders() []Recorder { return append([]Recorder(nil), recorders...) }

// Lookup finds a registered gate by name.
func Lookup(name string) (Gate, bool) {
	for _, g := range registry {
		if g.Name == name {
			return g, true
		}
	}
	return Gate{}, false
}

// Validate reports every malformed gate: an empty or duplicate name, or a
// missing Claims or Judge.
func Validate(gates []Gate) []error {
	var errs []error
	seen := map[string]bool{}
	for i, g := range gates {
		switch {
		case g.Name == "":
			errs = append(errs, fmt.Errorf("gate %d has no name", i))
		case seen[g.Name]:
			errs = append(errs, fmt.Errorf("gate %q registered twice", g.Name))
		}
		seen[g.Name] = true
		if g.Claims == nil || g.Judge == nil {
			errs = append(errs, fmt.Errorf("gate %q is missing Claims or Judge", g.Name))
		}
	}
	return errs
}

// Refusal is why a call may not run, and what to do instead.
type Refusal struct {
	// Gate is the refusing gate's name; Check fills it.
	Gate string
	// Rule is the rule the call broke.
	Rule string
	// Remedy is the sanctioned path.
	Remedy string
	// liftable adds the human escape hint; Check sets it from the gate.
	liftable bool
}

// NewRefusal builds a refusal. Both halves are required: a refusal without a
// rule tells Claude nothing, and one without a remedy leaves it guessing — the
// shape that sends an agent hunting for a way around the gate.
func NewRefusal(rule, remedy string) (*Refusal, error) {
	if strings.TrimSpace(rule) == "" || strings.TrimSpace(remedy) == "" {
		return nil, fmt.Errorf("a refusal needs both a rule and a remedy (got rule %q, remedy %q)", rule, remedy)
	}
	return &Refusal{Rule: rule, Remedy: remedy}, nil
}

// String is the text Claude reads in place of the call's result.
func (r Refusal) String() string {
	s := fmt.Sprintf("dross gate %s refused this call: %s\nUse: %s", r.Gate, r.Rule, r.Remedy)
	if r.liftable {
		s += fmt.Sprintf("\nIf this gate is wrong here, a human can run `dross gate off %s` in their own terminal.", r.Gate)
	}
	return s
}

// Call is one tool call under judgement.
type Call struct {
	Payload
	// Lists are the defaults unioned with gates.toml.
	Lists Lists
	// Home is the user's home directory ("" when unknown).
	Home string
	// Now is when the call is judged.
	Now time.Time

	scanned  bool
	script   shellscan.Script
	rootDone bool
	root     string
	rootErr  error
}

// Script is a Bash call's command line, scanned once.
func (c *Call) Script() shellscan.Script {
	if !c.scanned {
		c.scanned = true
		c.script = shellscan.Scan(c.Command(), shellscan.Options{Dir: c.Cwd, Home: c.Home})
	}
	return c.script
}

// Targets are the directories the call acts on: a file tool's file's
// directory; for Bash the working directory plus every directory the line
// moves into (cd, git -C).
func (c *Call) Targets() []string {
	if f := c.FilePath(); f != "" {
		return []string{filepath.Dir(f)}
	}
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	add(c.Cwd)
	if c.ToolName == "Bash" {
		for _, cmd := range c.Script().Commands {
			add(cmd.Dir)
			if g, ok := cmd.Git(); ok {
				add(g.Dir)
			}
		}
	}
	return dirs
}

// Root is the dross repo the call targets — the first of Targets inside one —
// or "" when it targets none. Located once, read-only.
func (c *Call) Root() (string, error) {
	if !c.rootDone {
		c.rootDone = true
		for _, d := range c.Targets() {
			r, err := LocateRoot(d)
			if err != nil {
				c.rootErr = err
				break
			}
			if r != "" {
				c.root = r
				break
			}
		}
	}
	return c.root, c.rootErr
}

// LocateRoot walks up from dir to the first directory holding a .dross/ and
// returns it when that .dross/ holds a project.toml. A .dross/ without one is
// no root, and the walk stops there all the same (walk_stop): a half-built
// root never borrows a parent's. It only stats — it never writes, and never
// materializes the state.json cmd.FindRoot would.
func LocateRoot(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	dir = filepath.Clean(dir)
	for {
		found, err := probe(dir, ".dross", true)
		if err != nil {
			return "", err
		}
		if found {
			ok, err := probe(dir, filepath.Join(".dross", "project.toml"), false)
			if err != nil {
				return "", err
			}
			if ok {
				return dir, nil
			}
			return "", nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// probe reports whether rel exists under dir as a directory (wantDir) or a
// regular file. Absence — including a path component that is a file — is
// false; any other failure is an error, so a gate refuses rather than guess.
func probe(dir, rel string, wantDir bool) (bool, error) {
	c, err := pathfence.Contain(dir, "dross root probe", rel)
	if err != nil {
		return false, err
	}
	info, err := pathfence.Stat(c)
	switch {
	case err == nil:
		if wantDir {
			return info.IsDir(), nil
		}
		return info.Mode().IsRegular(), nil
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		return false, nil
	}
	return false, fmt.Errorf("locate the dross root: %w", err)
}

// ContainIn contains an absolute path from a payload against root. ok is
// false — with no error — when the path lies outside root.
func ContainIn(root, p string) (c pathfence.Contained, ok bool, err error) {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return pathfence.Contained{}, false, nil
	}
	c, err = pathfence.Contain(root, "tool call path", rel)
	switch {
	case err == nil:
		return c, true, nil
	case errors.Is(err, pathfence.ErrEscapes) || errors.Is(err, pathfence.ErrAbsolute):
		return pathfence.Contained{}, false, nil
	}
	return pathfence.Contained{}, false, err
}

// Env is what a check runs against. The zero value is production: the user's
// home, the clock, the registry, and nothing lifted.
type Env struct {
	Home string
	Now  func() time.Time
	// Lifted reports whether a human lifted g for root ("" outside a repo).
	Lifted func(g Gate, root string) bool
	// Gates and Recorders replace the registry when non-nil.
	Gates     []Gate
	Recorders []Recorder
}

func (env Env) home() string {
	if env.Home != "" {
		return env.Home
	}
	h, _ := os.UserHomeDir()
	return h
}

func (env Env) call(p Payload) *Call {
	if p.Cwd == "" {
		p.Cwd, _ = os.Getwd()
	}
	now := time.Now
	if env.Now != nil {
		now = env.Now
	}
	home := env.home()
	return &Call{Payload: p, Lists: LoadLists(home), Home: home, Now: now()}
}

// Result is a check's outcome.
type Result struct {
	Refusals []Refusal
	// Warnings are for the human; they never block.
	Warnings []string
}

// Allowed reports whether no gate refused.
func (r Result) Allowed() bool { return len(r.Refusals) == 0 }

// Text joins the refusals for Claude.
func (r Result) Text() string {
	parts := make([]string, len(r.Refusals))
	for i, ref := range r.Refusals {
		parts[i] = ref.String()
	}
	return strings.Join(parts, "\n\n")
}

// Check judges a PreToolUse payload with every gate in env.
func Check(input []byte, env Env) Result {
	p, err := Decode(input)
	if err != nil {
		return Result{Warnings: []string{fmt.Sprintf("dross gate: %v — allowing the call unjudged", err)}}
	}
	c := env.call(p)
	gates := env.Gates
	if gates == nil {
		gates = registry
	}
	var res Result
	for _, g := range gates {
		if r := env.judge(g, c); r != nil {
			res.Refusals = append(res.Refusals, *r)
		}
	}
	return res
}

// judge runs one gate over the call, turning any panic into a refusal: an
// uncaught panic would exit 2, which PreToolUse reads as "block" — for every
// call, not just the claimed ones.
func (env Env) judge(g Gate, c *Call) (ref *Refusal) {
	refuse := func(rule, remedy string) *Refusal {
		return &Refusal{Gate: g.Name, Rule: rule, Remedy: remedy, liftable: g.Liftable}
	}
	defer func() {
		if v := recover(); v != nil {
			ref = refuse(fmt.Sprintf("internal error in the %s gate: %v", g.Name, v),
				"this is a dross bug — report it with the call that triggered it")
		}
	}()
	if !g.Claims(c) {
		return nil
	}
	root := ""
	if g.Scope == Workflow {
		r, err := c.Root()
		if err != nil {
			return refuse(fmt.Sprintf("the %s gate cannot tell whether this call targets a dross repo: %v", g.Name, err),
				"fix the error above, then retry the call")
		}
		if r == "" {
			return nil
		}
		root = r
	}
	if g.Liftable && env.Lifted != nil && env.Lifted(g, root) {
		return nil
	}
	if g.Extensible && c.Lists.Err != nil {
		return refuse(fmt.Sprintf("the %s gate's lists cannot be loaded: %v", g.Name, c.Lists.Err),
			fmt.Sprintf("fix or remove %s, then retry the call", c.Lists.Path))
	}
	r, err := g.Judge(c)
	if err != nil {
		return refuse(fmt.Sprintf("the %s gate could not judge this call: %v", g.Name, err),
			"fix the error above, then retry the call")
	}
	if r == nil {
		return nil
	}
	r.Gate, r.liftable = g.Name, g.Liftable
	return r
}

// Record runs every recorder in env over a PostToolUse payload. It returns
// warnings only: a recorder can never block.
func Record(input []byte, env Env) []string {
	p, err := Decode(input)
	if err != nil {
		return []string{fmt.Sprintf("dross gate record: %v — nothing recorded", err)}
	}
	c := env.call(p)
	recs := env.Recorders
	if recs == nil {
		recs = recorders
	}
	var warns []string
	for _, r := range recs {
		if err := record(r, c); err != nil {
			warns = append(warns, fmt.Sprintf("dross gate record %s: %v", r.Name, err))
		}
	}
	return warns
}

func record(r Recorder, c *Call) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("internal error: %v", v)
		}
	}()
	if !r.Claims(c) {
		return nil
	}
	if r.Scope == Workflow {
		root, err := c.Root()
		if err != nil || root == "" {
			return err
		}
	}
	return r.Record(c)
}
