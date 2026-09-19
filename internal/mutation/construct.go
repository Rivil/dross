package mutation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

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

// constructTimeout bounds one resolver spawn. A parse is milliseconds; the
// bound exists for a node that hangs on startup (a broken install, a
// loader that waits on the network), which would otherwise hold the run
// open indefinitely. A variable so tests can lower it.
var constructTimeout = 30 * time.Second

// astRequest is the prelude every spawn carries: the file's repo-relative
// path (for the plugin choice) and its source. Rides on stdin — never argv,
// which is logged and visible to every local process, and never a temp file
// the script would have to be trusted to find.
type astRequest struct {
	File   string `json:"file"`
	Source string `json:"source"`
}

// astResponse is what astspan.js writes: exactly one of the two.
type astResponse struct {
	Constructs []Construct `json:"constructs"`
	Error      *astError   `json:"error"`
}

type astError struct {
	Message string `json:"message"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

// Constructs resolves file's top-level constructs through @babel/parser out
// of Stryker's own dependency tree, run by node in the adapter's Workdir —
// the same place stryker itself runs, so the same node_modules resolve.
//
// LOCAL, always. This goes through strykerBuildCmd — so a docker Prefix is
// honoured, since that is where the project's node_modules live — and never
// through the launcher: the planning_locus lock keeps range planning on the
// machine that authors the record, and a remote resolver would make the
// record inferred from the host rather than written here.
//
// Every failure wraps ErrASTUnavailable with a dross-authored cause. The
// detail is what lands on the scope's degraded line and in verify.toml, so
// nothing the tool printed goes into it: a parse error is its POSITION, and
// the parser's own message streams to stderr where the live diagnostic
// belongs.
func (s *Stryker) Constructs(file string) ([]Construct, error) {
	if strings.EqualFold(filepath.Ext(file), ".svelte") {
		// Stryker mutates Svelte through its own svelte-parser; Babel alone
		// would fail on the markup with a position inside a <script> block
		// that names nothing. Refused before any process is spawned.
		return nil, fmt.Errorf("%w: no parser for .svelte", ErrASTUnavailable)
	}
	src, err := os.ReadFile(filepath.Join(s.ProjectRoot, filepath.FromSlash(file)))
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read %s", ErrASTUnavailable, file)
	}
	prelude, err := json.Marshal(astRequest{File: file, Source: string(src)})
	if err != nil {
		return nil, fmt.Errorf("%w: cannot encode the request for %s", ErrASTUnavailable, file)
	}

	cmd := strykerBuildCmd(s, []string{"node", "-"})
	cmd.Dir = s.workDir()
	cmd.Stdin = strings.NewReader("const __dross = " + string(prelude) + ";\n" + astspanScript)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	// A child node leaves behind (a loader, a daemon it forked) would hold
	// the stdout pipe open past the kill below; WaitDelay bounds how long
	// Wait keeps the run for it.
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s not on PATH", ErrASTUnavailable, cmd.Args[0])
		}
		return nil, fmt.Errorf("%w: %s could not be started", ErrASTUnavailable, cmd.Args[0])
	}
	var timedOut atomic.Bool
	timer := time.AfterFunc(constructTimeout, func() {
		timedOut.Store(true)
		_ = cmd.Process.Kill()
	})
	waitErr := cmd.Wait()
	timer.Stop()
	if timedOut.Load() {
		return nil, fmt.Errorf("%w: resolver timed out after %s", ErrASTUnavailable, constructTimeout)
	}

	var resp astResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("%w: malformed resolver output", ErrASTUnavailable)
	}
	if resp.Error != nil {
		// The script's own words for its own failures (resolution), the
		// position only for the parser's.
		if strings.HasPrefix(resp.Error.Message, "cannot resolve @babel/parser") {
			return nil, fmt.Errorf("%w: cannot resolve @babel/parser from %s", ErrASTUnavailable, s.workDir())
		}
		if resp.Error.Line > 0 {
			fmt.Fprintf(os.Stderr, "astspan: %s: %s\n", file, resp.Error.Message)
			return nil, fmt.Errorf("%w: parse error at %d:%d", ErrASTUnavailable, resp.Error.Line, resp.Error.Column)
		}
		fmt.Fprintf(os.Stderr, "astspan: %s: %s\n", file, resp.Error.Message)
		return nil, fmt.Errorf("%w: resolver refused %s", ErrASTUnavailable, file)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("%w: resolver exited without a result", ErrASTUnavailable)
	}
	if resp.Constructs == nil {
		resp.Constructs = []Construct{}
	}
	return resp.Constructs, nil
}
