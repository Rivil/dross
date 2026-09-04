package cmd

import (
	"errors"
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

// A repo-wide gate on the property this phase buys: every process dross can
// spawn is either behind a consent gate or carries an in-source marker saying
// why it cannot reach repo-authored code.
//
// It is DERIVED, not declared. The set of spawn sites is read out of the source
// tree by walking its ASTs, so a site added tomorrow is in scope the day it is
// written — there is no list of command names to keep in step, which is exactly
// the failure mode trust.go's closed set of six had. Nothing here consults a
// binary name: `cargo` and `git` get the same verdict from the same rule,
// because a classifier that decides which binaries "look dangerous" is the
// recurring vulnerability, not the fix.
//
// THE RULE, in this task:
//
//   - Every exec.Command / exec.CommandContext construction in non-test source
//     is a spawn site. The _test.go boundary is the locked spawn_surface rule
//     and matches subprocargs_audit_test.go:345: a spawn inside a test file only
//     runs under `go test`, which is itself the command the consent gate
//     authorises, so gating it would gate a run already consented to.
//   - A site passes when it carries a //dross:exec-exempt marker with prose on
//     the line IMMEDIATELY above it. Position, not proximity: a marker two lines
//     up belongs to whatever sits between them.
//   - Prose is required and must say something. A bare marker and a marker whose
//     reason is only the subcommand name are both findings, because an
//     exemption nobody had to justify is a list of command names again, spelled
//     differently.
//
// t-4 adds the other half of the verdict — reach — so a site the consent gate
// already covers passes without a marker, and a marker on such a site becomes a
// finding of its own. Until then the sweep is scoped to the fixture corpus that
// execConsentRoots materialises; t-10 widens it to internal/ and cmd/.

// execExemptMarker is the in-source escape hatch. It is a Go directive-shaped
// comment on purpose: no space after the slashes, so an ordinary prose comment
// that happens to mention the marker cannot exempt anything.
const execExemptMarker = "//dross:exec-exempt"

// execExemptMinReason is the floor on marker prose. A reason has to be long
// enough to name why the call cannot reach repo-authored code; "status" is the
// subcommand restated, which is what the marker already sits next to.
const execExemptMinReason = 20

// execFinding is one spawn site that is neither gated nor properly exempt.
//
// It deliberately carries NO binary field. The verdict never sees which program
// is being spawned, and a struct with nowhere to put that fact is a stronger
// guarantee of it than a comment saying so.
type execFinding struct {
	Pos  string
	Call string
	Why  string
	// NoRemedy suppresses the "gate it or mark it exempt" tail for findings
	// where neither move is the fix.
	NoRemedy bool
}

func (f execFinding) String() string {
	s := fmt.Sprintf("%s: %s(…) %s", f.Pos, f.Call, f.Why)
	if f.NoRemedy {
		// The remedy is omitted where it would be wrong advice. A site the
		// gate already covers must not be told to mark itself exempt — that
		// is the very move rule 9 exists to refuse.
		return s
	}
	return s + " — gate it or mark it exempt with " + execExemptMarker + " <reason>"
}

// execExemption is a parsed marker. Reason is empty for a bare marker, which is
// a different finding from no marker at all.
type execExemption struct {
	Reason string
}

// execExemptMarkers maps the line a marker EXEMPTS — the one directly below it —
// to the marker's prose.
func execExemptMarkers(fset *token.FileSet, f *ast.File) map[int]execExemption {
	out := map[int]execExemption{}
	for _, group := range f.Comments {
		for _, c := range group.List {
			rest, ok := strings.CutPrefix(c.Text, execExemptMarker)
			if !ok {
				continue
			}
			// The reason must be separated from the directive. Without this
			// `//dross:exec-exemptanything` would parse as a marker whose reason
			// is glued to it, which is a typo passing as an exemption.
			if rest != "" && !strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "\t") {
				continue
			}
			out[fset.Position(c.End()).Line+1] = execExemption{Reason: strings.TrimSpace(rest)}
		}
	}
	return out
}

// auditExecConsentFile is the single-file view of the audit, kept for the tests
// whose subject is one file. It is the graph over a file set of one — reach
// still runs, and a file with no command in it simply reaches nothing.
func auditExecConsentFile(fset *token.FileSet, f *ast.File) ([]execFinding, int) {
	return auditExecFiles(fset, []*ast.File{f})
}

// auditExecFiles is the audit: build the reach graph over every file, then read
// each spawn site's verdict off it.
func auditExecFiles(fset *token.FileSet, files []*ast.File) ([]execFinding, int) {
	g := buildExecGraph(fset, files)
	return g.findings(), len(g.sites)
}

// execConsentVacuity is the discovery floor, factored out of the sweep so it can
// be exercised directly. A gate that stopped matching anything reports zero
// findings, which is indistinguishable from success unless the site count is
// checked too.
func execConsentVacuity(sites int) error {
	if sites == 0 {
		return errors.New("found no spawn sites — the gate would pass vacuously")
	}
	return nil
}

// sweepExecConsent walks roots under root and audits every non-test .go file,
// mirroring subprocargs_audit_test.go's runAudit down to the _test.go skip.
func sweepExecConsent(t *testing.T, root string, roots []string) ([]execFinding, int) {
	t.Helper()
	fset := token.NewFileSet()
	var findings []execFinding
	sites := 0

	var files []*ast.File
	for _, r := range roots {
		err := filepath.WalkDir(filepath.Join(root, r), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if perr != nil {
				return perr
			}
			files = append(files, f)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", r, err)
		}
	}
	// ONE graph over the whole set, never a graph per file: reach crosses
	// packages, and a per-file audit would report every cross-package call as
	// leading nowhere.
	findings, sites = auditExecFiles(fset, files)
	return findings, sites
}

// execConsentRoots returns the tree the repo-wide gate sweeps and the roots
// inside it.
//
// This task scopes it to the fixture corpus — the PASS rows of snippets.txt,
// materialised as real source in a temp tree — because the repo's own spawn
// sites are not gated or marked yet. Widening it to internal/ and cmd/ is t-10,
// and it is a one-function change by design.
func execConsentRoots(t *testing.T) (string, []string) {
	t.Helper()
	rows, _ := parseExecSnippetTable(t)
	var body []string
	for _, row := range rows {
		if row.Flag {
			continue
		}
		body = append(body, row.Src...)
	}
	dir := t.TempDir()
	src := execSnippetPreamble + strings.Join(body, "\n") + "\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "corpus.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, []string{"."}
}

// TestEverySpawnSiteGatedOrExempt is the gate. trust.go's package comment and
// doctor's consent section point at it by name, so renaming it is a documented
// break rather than a quiet one.
func TestEverySpawnSiteGatedOrExempt(t *testing.T) {
	root, roots := execConsentRoots(t)
	findings, sites := sweepExecConsent(t, root, roots)
	if err := execConsentVacuity(sites); err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f.String())
	}
}

// TestExecConsentFailsVacuously exercises the floor directly. Asserting it
// inside the gate above would only prove it on a tree that already has sites.
func TestExecConsentFailsVacuously(t *testing.T) {
	err := execConsentVacuity(0)
	if err == nil {
		t.Fatal("zero spawn sites passed — the gate would be vacuous")
	}
	if !strings.Contains(err.Error(), "found no spawn sites") {
		t.Errorf("vacuity error does not name the cause: %v", err)
	}
	if err := execConsentVacuity(1); err != nil {
		t.Errorf("a single site tripped the floor: %v", err)
	}
}

// TestExecConsentFindingNamesFileLineAndRemedy: a finding that does not say
// where and what to do is a failing test nobody can act on. The fixture's line
// is looked up rather than written down, so the assertion survives the fixture
// gaining a comment.
func TestExecConsentFindingNamesFileLineAndRemedy(t *testing.T) {
	root := repoRootForDocs(t)
	path := filepath.Join(root, "internal", "cmd", "testdata", "exec_consent", "ungated.go.txt")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantLine := 0
	for i, line := range strings.Split(string(body), "\n") {
		if strings.Contains(line, `exec.Command("go", "build"`) {
			wantLine = i + 1
		}
	}
	if wantLine == 0 {
		t.Fatal("the ungated fixture no longer holds the spawn this test is about")
	}

	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, body, parser.ParseComments)
	if perr != nil {
		t.Fatal(perr)
	}
	findings, sites := auditExecConsentFile(fset, f)
	if sites != 2 {
		t.Fatalf("saw %d spawn sites in the fixture, want 2", sites)
	}
	if len(findings) != 1 {
		t.Fatalf("want exactly one finding, got %d: %v", len(findings), findings)
	}
	text := findings[0].String()
	for _, want := range []string{
		"ungated.go.txt",
		fmt.Sprintf(":%d:", wantLine),
		"gate it or mark it exempt",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("finding text lost its anchor %q:\n%s", want, text)
		}
	}
}

// TestExecConsentVerdictIgnoresBinaryName: the moment the verdict knows which
// binary it is looking at, it has a list of command names in it again. Two
// ungated sites that differ only in the program spawned must produce the same
// finding.
func TestExecConsentVerdictIgnoresBinaryName(t *testing.T) {
	git, _ := auditExecSnippet(t, "\texec.Command(\"git\", \"status\")")
	cargo, _ := auditExecSnippet(t, "\texec.Command(\"cargo\", \"test\", pkg)")
	if len(git) != 1 || len(cargo) != 1 {
		t.Fatalf("want one finding each, got git=%d cargo=%d", len(git), len(cargo))
	}
	if git[0].Why != cargo[0].Why {
		t.Errorf("the verdict differs by binary:\n git:   %s\n cargo: %s", git[0].Why, cargo[0].Why)
	}
}

// TestExecConsentMarkerGrammar pins the two ways a marker can be present and
// still be wrong. The messages are asserted, not just the count: "reasonless"
// and the length floor are what tell an author which mistake they made.
func TestExecConsentMarkerGrammar(t *testing.T) {
	bare, _ := auditExecSnippet(t,
		"\t"+execExemptMarker,
		"\texec.Command(\"git\", \"status\")")
	if len(bare) != 1 {
		t.Fatalf("a bare marker exempted the call: %v", bare)
	}
	if !strings.Contains(bare[0].Why, "reasonless") {
		t.Errorf("bare-marker finding does not name the marker as reasonless: %s", bare[0].Why)
	}

	short, _ := auditExecSnippet(t,
		"\t"+execExemptMarker+" status",
		"\texec.Command(\"git\", \"status\")")
	if len(short) != 1 {
		t.Fatalf("a one-word reason exempted the call: %v", short)
	}
	if !strings.Contains(short[0].Why, fmt.Sprint(execExemptMinReason)) {
		t.Errorf("short-reason finding does not name the floor: %s", short[0].Why)
	}

	good, sites := auditExecSnippet(t,
		"\t"+execExemptMarker+" git status reads the working tree and runs no repo-authored code",
		"\texec.Command(\"git\", \"status\")")
	if len(good) != 0 {
		t.Errorf("prose that says why was rejected: %v", good)
	}
	if sites != 1 {
		t.Errorf("saw %d spawn sites, want 1", sites)
	}
}

// TestExecConsentSkipsTestFiles: the locked spawn_surface puts _test.go outside
// the walk. Both files are written into one tree so the assertion proves the
// skip is by file name and not by the walk having gone blind.
func TestExecConsentSkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	src := "package fixture\n\nimport \"os/exec\"\n\nvar c = exec.Command(\"git\", \"status\")\n"
	for _, name := range []string{"probe.go", "probe_test.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	findings, sites := sweepExecConsent(t, dir, []string{"."})
	if sites != 1 {
		t.Fatalf("saw %d spawn sites, want 1 — the _test.go file is inside the surface", sites)
	}
	if len(findings) != 1 {
		t.Fatalf("want one finding, got %d: %v", len(findings), findings)
	}
	if strings.Contains(findings[0].Pos, "_test.go") {
		t.Errorf("flagged a spawn inside a test file: %s", findings[0].Pos)
	}
}

// execSnippetPreamble wraps snippet lines into a parseable file. The parameters
// are the names snippets.txt may use; unused ones are legal and keep the table
// free to grow.
const execSnippetPreamble = `package cmd

import (
	"context"
	"os/exec"
)

var _ = exec.Command

func execConsentSnippet(ctx context.Context, pkg, dir, ref, host string) {
`

// auditExecSnippet parses one or more lines of Go inside a synthetic function
// and returns what the enumerator makes of them, plus the sites it saw.
func auditExecSnippet(t *testing.T, lines ...string) ([]execFinding, int) {
	t.Helper()
	fset := token.NewFileSet()
	src := execSnippetPreamble + strings.Join(lines, "\n") + "\n}\n"
	f, err := parser.ParseFile(fset, "snippet.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("snippet does not parse: %v\n%s", err, strings.Join(lines, "\n"))
	}
	return auditExecConsentFile(fset, f)
}

// execSnippetRow is one FLAG/PASS block of snippets.txt.
type execSnippetRow struct {
	Name string
	Flag bool
	Src  []string
}

// parseExecSnippetTable returns the rows plus the number of FLAG/PASS HEADERS it
// saw. The two are counted separately on purpose: a parser that dropped its last
// row would otherwise agree with itself.
func parseExecSnippetTable(t *testing.T) ([]execSnippetRow, int) {
	t.Helper()
	root := repoRootForDocs(t)
	path := filepath.Join(root, "internal", "cmd", "testdata", "exec_consent", "snippets.txt")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var (
		rows    []execSnippetRow
		headers int
		cur     *execSnippetRow
	)
	flush := func() {
		if cur != nil {
			rows = append(rows, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "FLAG ") || strings.HasPrefix(trimmed, "PASS ") {
			headers++
			flush()
			cur = &execSnippetRow{Name: strings.TrimSpace(trimmed[5:]), Flag: strings.HasPrefix(trimmed, "FLAG ")}
			continue
		}
		if cur != nil {
			cur.Src = append(cur.Src, line)
		}
	}
	flush()
	return rows, headers
}

// TestExecConsentFlagsItsOwnSnippets checks the checker, with a floor derived
// from the table rather than chosen in advance — mirroring
// TestAuditFlagsItsOwnSnippets, so a row cannot be deleted or skipped quietly.
func TestExecConsentFlagsItsOwnSnippets(t *testing.T) {
	rows, headers := parseExecSnippetTable(t)
	if headers == 0 {
		t.Fatal("parsed no FLAG/PASS rows — the table or the parser is broken")
	}
	if len(rows) != headers {
		t.Fatalf("parsed %d headers but built %d rows — the table is being silently truncated", headers, len(rows))
	}

	checked := 0
	for _, row := range rows {
		t.Run(row.Name, func(t *testing.T) {
			got, _ := auditExecSnippet(t, row.Src...)
			if row.Flag && len(got) == 0 {
				t.Errorf("expected a finding, got none:\n%s", strings.Join(row.Src, "\n"))
			}
			if !row.Flag && len(got) > 0 {
				t.Errorf("false positive %v on:\n%s", got, strings.Join(row.Src, "\n"))
			}
		})
		checked++
	}
	if checked != headers {
		t.Fatalf("parsed %d rows but exercised %d — the table is being silently truncated", headers, checked)
	}
}

// --- reach ---
//
// The other half of the verdict. t-1's rule was "marked or flagged", which any
// author can satisfy by marking everything; that would produce a green sweep
// proving nothing. What makes a marker meaningful is that most sites should not
// need one — they are already behind the consent gate — and the only way to
// know which is to follow the calls.
//
// The graph is built from the same ASTs the sites come from, with no type
// checker, so its resolution rules are stated rather than inferred:
//
//   - `foo(…)`        resolves in the CALLER'S package only, which is what Go
//                     itself does for an unqualified name.
//   - `pkg.Foo(…)`    resolves through the file's imports to that package's
//                     declaration. Deterministic.
//   - `x.Foo(…)`      needs the receiver's type, so a small local inference
//                     runs: parameters, receivers, `var` declarations, `:=`
//                     bindings, range variables, and the RESULT TYPES of any
//                     function in the file set. A receiver whose type is not
//                     inferable resolves to NOTHING, which is the safe
//                     direction — a spawn reachable from no command is itself a
//                     finding, so a lost edge fails closed rather than open.
//                     When the inferred type is an INTERFACE, the call fans out
//                     to every type in the file set whose method set contains
//                     the interface's, which is how `adapter.Run()` finds every
//                     mutation adapter without `verify` ever naming Gremlins.
//                     When those implementers span more than one package and
//                     disagree about reaching a spawn, the ambiguity is
//                     REPORTED — that is the case where the union is covering
//                     for a resolution nobody can verify by reading.
//   - `var f = g`     is an alias edge, and `var f = func(){…}` is a node with
//                     the literal's body. Both are how this codebase makes a
//                     subprocess substitutable in tests, so an edge set that
//                     stopped at package-level vars would lose most of the
//                     interesting reach.
//
// The inference is what keeps the graph honest at this size. Resolving a method
// by NAME alone — every `Run` in every imported package — put `exec.Cmd.Run` in
// verify.go next to `mutation.Adapter.Run` and dragged every mutation spawn
// into the reach of half the binary.
//
// AddCommand is deliberately NOT a reach edge. `Survivor()` constructs its
// children, so following those calls would make every parent reach every
// child's spawns — and `survivor drain`'s gated spawn would come out MIXED,
// reached by the ungated container that merely built it. The AddCommand
// arguments are recorded as TREE edges instead, which is what turns a
// constructor into the path `survivor drain`.
//
// GATING is a name rule plus a use rule, and it needs both. The name rule alone
// (`requireExecConsent`, or any identifier ending in `Consented`) would mark
// doctor as gating on the strength of reportLaneConsent, which reads a lane's
// consent state to PRINT it. So a function gates only when the call's result
// reaches a branch that STOPS: an `if` over a value the call bound, or over the
// call itself, whose body returns, continues or breaks. Nothing consults a
// roster of known helpers — c-1 kills hand-maintained lists on both sides of
// the verdict, and a `FooConsented` invented tomorrow gates its caller with no
// edit here.

// execPart is one body belonging to a node, with the file and (for a method or
// function) the declaration whose signature seeds the type environment.
type execPart struct {
	file *ast.File
	decl *ast.FuncDecl
	body ast.Node
}

// execFunc is one node: a declared function, a method, or a package-level var
// holding a function.
type execFunc struct {
	key   string
	pkg   string
	parts []execPart
	calls map[string]bool
	// children are AddCommand targets — the command TREE, not reach.
	children []string
	gates    bool
	// use is the cobra Use string's first word, empty for a non-command.
	use string
}

// execCommand is a cobra command with its full path and everything it reaches.
type execCommand struct {
	key   string
	use   string
	path  string
	gates bool
	reach map[string]bool
}

// execSite is one spawn site with the function it sits in.
type execSite struct {
	pos    token.Position
	call   string
	owner  string
	marker execExemption
	marked bool
	class  string
	// gatedVia and ungatedVia are EVERY command that reaches this site, split
	// by whether it gates. Both are kept whole rather than reduced to one
	// name: a site can be gated by two commands and ungated by a third, and a
	// test asking "is verify among them" must not depend on sort order.
	gatedVia   []string
	ungatedVia []string
}

// gatedBy is the first gated command reaching the site, for rendering.
func (s *execSite) gatedBy() string { return execFirst(s.gatedVia) }

// ungated is the first ungated command reaching the site, for rendering.
func (s *execSite) ungated() string { return execFirst(s.ungatedVia) }

func execFirst(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[0]
}

// reaches reports whether the named command reaches this site at all.
func (s *execSite) reaches(cmd string) bool {
	for _, c := range append(append([]string{}, s.gatedVia...), s.ungatedVia...) {
		if c == cmd {
			return true
		}
	}
	return false
}

// Verdict is the site's reach class, rendered. Tests read this rather than
// re-deriving it, so "gated via verify" means one thing everywhere.
func (s *execSite) Verdict() string {
	switch s.class {
	case execReachGated:
		return "gated via " + strings.Join(s.gatedVia, ", ")
	case execReachMixed:
		return "mixed: gated via " + strings.Join(s.gatedVia, ", ") + ", ungated via " + strings.Join(s.ungatedVia, ", ")
	case execReachUngated:
		return "ungated via " + strings.Join(s.ungatedVia, ", ")
	default:
		return execReachNone
	}
}

const (
	execReachGated   = "gated"
	execReachMixed   = "mixed"
	execReachUngated = "ungated"
	execReachNone    = "unreachable"
)

// execGraph is the whole analysis over one file set.
type execGraph struct {
	fset *token.FileSet
	// scope maps package name -> declared name -> node key, for resolving an
	// unqualified call and a qualified one alike.
	scope map[string]map[string]string
	// methods maps a bare method name -> every node key declaring it.
	methods map[string][]string
	// typeMethods maps "pkg.Type" -> its method names, for deciding which
	// concrete types satisfy an interface.
	typeMethods map[string]map[string]bool
	// ifaces maps "pkg.Iface" -> its method set.
	ifaces map[string]map[string]bool
	// results maps a node key -> its declared result types, normalised.
	results map[string][]string
	funcs   map[string]*execFunc
	sites   []*execSite
	cmds    []*execCommand
	// ambiguous names an interface fan-out whose implementers span packages
	// and disagree about reaching a spawn.
	ambiguous []execFinding
	markers   map[string]map[int]execExemption
}

func (g *execGraph) node(key, pkg string) *execFunc {
	if n, ok := g.funcs[key]; ok {
		return n
	}
	n := &execFunc{key: key, pkg: pkg, calls: map[string]bool{}}
	g.funcs[key] = n
	return n
}

func (g *execGraph) declare(pkg, name, key string) {
	if g.scope[pkg] == nil {
		g.scope[pkg] = map[string]string{}
	}
	// First declaration wins. A duplicate within one package is a receiver
	// distinction this map cannot express; the method table carries those.
	if _, ok := g.scope[pkg][name]; !ok {
		g.scope[pkg][name] = key
	}
}

// buildExecGraph parses the file set into nodes, edges, commands and sites.
func buildExecGraph(fset *token.FileSet, files []*ast.File) *execGraph {
	g := &execGraph{
		fset:        fset,
		scope:       map[string]map[string]string{},
		methods:     map[string][]string{},
		typeMethods: map[string]map[string]bool{},
		ifaces:      map[string]map[string]bool{},
		results:     map[string][]string{},
		funcs:       map[string]*execFunc{},
		markers:     map[string]map[int]execExemption{},
	}

	// Pass 1: interfaces, so a fan-out has something to fan out over.
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				it, ok := ts.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				set := map[string]bool{}
				for _, m := range it.Methods.List {
					for _, nm := range m.Names {
						set[nm.Name] = true
					}
				}
				g.ifaces[f.Name.Name+"."+ts.Name.Name] = set
			}
		}
	}

	// Pass 2: every declaration, before any call is resolved. A single pass
	// would resolve forward references to nothing.
	for _, f := range files {
		pkg := f.Name.Name
		g.markers[fset.Position(f.Pos()).Filename] = execExemptMarkers(fset, f)
		imports := execImports(f)
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				key := pkg + "." + d.Name.Name
				if d.Recv != nil {
					recv := execTypeString(d.Recv.List[0].Type, pkg, imports)
					key = recv + "." + d.Name.Name
					g.methods[d.Name.Name] = append(g.methods[d.Name.Name], key)
					if g.typeMethods[recv] == nil {
						g.typeMethods[recv] = map[string]bool{}
					}
					g.typeMethods[recv][d.Name.Name] = true
				} else {
					g.declare(pkg, d.Name.Name, key)
				}
				g.results[key] = execResultTypes(d.Type, pkg, imports)
				n := g.node(key, pkg)
				if d.Body != nil {
					n.parts = append(n.parts, execPart{file: f, decl: d, body: d.Body})
				}
			case *ast.GenDecl:
				if d.Tok != token.VAR {
					continue
				}
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, nm := range vs.Names {
						if i >= len(vs.Values) || nm.Name == "_" {
							continue
						}
						key := pkg + "." + nm.Name
						g.declare(pkg, nm.Name, key)
						n := g.node(key, pkg)
						n.parts = append(n.parts, execPart{file: f, body: vs.Values[i]})
					}
				}
			}
		}
	}

	// Pass 3: walk every node's bodies for edges, sites, gating and commands.
	for _, n := range g.funcs {
		for _, part := range n.parts {
			g.walk(n, part)
		}
		g.detectGating(n)
	}

	g.buildCommands()
	g.classify()
	return g
}

// execImports maps the identifier a file uses for each import to the package
// name we key on — the last path element, unless the file gave an alias.
func execImports(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		parts := strings.Split(path, "/")
		name := parts[len(parts)-1]
		if imp.Name != nil {
			out[imp.Name.Name] = name
			continue
		}
		out[name] = name
	}
	return out
}

// execTypeString normalises a type expression to "pkg.Type", with `[]` kept as
// a prefix and a map reduced to its VALUE type — the only part an index
// expression can produce. An unresolvable type is the empty string, which every
// caller reads as "do not guess".
func execTypeString(e ast.Expr, pkg string, imports map[string]string) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return execTypeString(t.X, pkg, imports)
	case *ast.ParenExpr:
		return execTypeString(t.X, pkg, imports)
	case *ast.Ident:
		return pkg + "." + t.Name
	case *ast.SelectorExpr:
		x, ok := t.X.(*ast.Ident)
		if !ok {
			return ""
		}
		target, isPkg := imports[x.Name]
		if !isPkg {
			return ""
		}
		return target + "." + t.Sel.Name
	case *ast.ArrayType:
		inner := execTypeString(t.Elt, pkg, imports)
		if inner == "" {
			return ""
		}
		return "[]" + inner
	case *ast.MapType:
		inner := execTypeString(t.Value, pkg, imports)
		if inner == "" {
			return ""
		}
		return "[]" + inner
	}
	return ""
}

// execResultTypes is a signature's result types, normalised.
func execResultTypes(ft *ast.FuncType, pkg string, imports map[string]string) []string {
	if ft.Results == nil {
		return nil
	}
	var out []string
	for _, f := range ft.Results.List {
		typ := execTypeString(f.Type, pkg, imports)
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			out = append(out, typ)
		}
	}
	return out
}

// execScope is one body's local type environment.
type execScope struct {
	g       *execGraph
	pkg     string
	imports map[string]string
	vars    map[string]string
}

// walk collects one node's edges, spawn sites and command shape.
func (g *execGraph) walk(owner *execFunc, part execPart) {
	imports := execImports(part.file)
	body := part.body

	// A package-level `var f = g` is an ALIAS, not a body: the value IS the
	// function. Walking it as an expression would find no call and drop the
	// edge that makes the seam followable.
	switch v := body.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		sc := &execScope{g: g, pkg: owner.pkg, imports: imports, vars: map[string]string{}}
		for _, key := range sc.resolveCallee(v.(ast.Expr)) {
			owner.calls[key] = true
		}
		return
	case *ast.FuncLit:
		body = v.Body
	}

	sc := &execScope{g: g, pkg: owner.pkg, imports: imports, vars: map[string]string{}}
	sc.seed(part.decl)
	// A package-level `var f = func(x T){…}` carries its parameters on the
	// literal, not on a declaration. Missing them left every receiver inside
	// the seam untyped, which silently unhooked the var seams this codebase
	// uses for exactly the spawns this audit is about.
	if lit, ok := part.body.(*ast.FuncLit); ok {
		sc.seedFuncType(lit.Type)
	}
	sc.collect(body)

	skip := map[ast.Node]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CompositeLit:
			if use := execCobraUse(n, imports); use != "" {
				owner.use = use
			}
		case *ast.CallExpr:
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "AddCommand" {
				// TREE edges. Marked for skipping so the constructor does not
				// also inherit its children's reach.
				for _, a := range n.Args {
					child, ok := a.(*ast.CallExpr)
					if !ok {
						continue
					}
					skip[child] = true
					for _, key := range sc.resolveCallee(child.Fun) {
						owner.children = append(owner.children, key)
					}
				}
				return true
			}
			if name, _, _, _, isSpawn := spawnArgvOf(n); isSpawn && strings.HasPrefix(name, "exec.") {
				pos := g.fset.Position(n.Pos())
				m, marked := g.markers[pos.Filename][pos.Line]
				g.sites = append(g.sites, &execSite{
					pos: pos, call: name, owner: owner.key, marker: m, marked: marked,
				})
				return true
			}
			if skip[n] {
				return true
			}
			for _, key := range sc.resolveCallee(n.Fun) {
				owner.calls[key] = true
			}
		}
		return true
	})
}

// seed puts a declaration's receiver, parameters and named results into scope.
func (sc *execScope) seed(decl *ast.FuncDecl) {
	if decl == nil {
		return
	}
	add := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			typ := execTypeString(f.Type, sc.pkg, sc.imports)
			for _, nm := range f.Names {
				sc.vars[nm.Name] = typ
			}
		}
	}
	add(decl.Recv)
	sc.seedFuncType(decl.Type)
}

// seedFuncType puts a signature's parameters and named results into scope.
func (sc *execScope) seedFuncType(ft *ast.FuncType) {
	for _, fl := range []*ast.FieldList{ft.Params, ft.Results} {
		if fl == nil {
			continue
		}
		for _, f := range fl.List {
			typ := execTypeString(f.Type, sc.pkg, sc.imports)
			for _, nm := range f.Names {
				sc.vars[nm.Name] = typ
			}
		}
	}
}

// collect walks a body for every binding whose type can be inferred.
//
// Flat rather than block-scoped on purpose: a shadowed name would be resolved
// to whichever binding this walk saw last, and the cost of that is an edge that
// may not exist. An edge that may not exist can only ADD ungated reach, which
// is the direction that fails closed.
func (sc *execScope) collect(body ast.Node) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			for _, f := range n.Type.Params.List {
				typ := execTypeString(f.Type, sc.pkg, sc.imports)
				for _, nm := range f.Names {
					sc.vars[nm.Name] = typ
				}
			}
		case *ast.DeclStmt:
			gd, ok := n.Decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				return true
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, nm := range vs.Names {
					switch {
					case vs.Type != nil:
						sc.vars[nm.Name] = execTypeString(vs.Type, sc.pkg, sc.imports)
					case i < len(vs.Values):
						sc.vars[nm.Name] = sc.infer(vs.Values[i])
					}
				}
			}
		case *ast.AssignStmt:
			sc.bind(n.Lhs, n.Rhs)
		case *ast.RangeStmt:
			if n.Value == nil {
				return true
			}
			id, ok := n.Value.(*ast.Ident)
			if !ok {
				return true
			}
			sc.vars[id.Name] = strings.TrimPrefix(sc.infer(n.X), "[]")
		}
		return true
	})
}

// bind records the types an assignment produces.
func (sc *execScope) bind(lhs, rhs []ast.Expr) {
	if len(rhs) == 1 && len(lhs) > 1 {
		call, ok := rhs[0].(*ast.CallExpr)
		if !ok {
			return
		}
		results := sc.callResults(call)
		for i, l := range lhs {
			id, ok := l.(*ast.Ident)
			if !ok || i >= len(results) {
				continue
			}
			sc.vars[id.Name] = results[i]
		}
		return
	}
	for i, l := range lhs {
		id, ok := l.(*ast.Ident)
		if !ok || i >= len(rhs) {
			continue
		}
		if typ := sc.infer(rhs[i]); typ != "" {
			sc.vars[id.Name] = typ
		}
	}
}

// callResults is a call's result types, empty when the callee is outside the
// file set.
func (sc *execScope) callResults(call *ast.CallExpr) []string {
	keys := sc.resolveCallee(call.Fun)
	if len(keys) != 1 {
		return nil
	}
	return sc.g.results[keys[0]]
}

// infer is the local type inference: enough to name a receiver, and silent
// about everything else.
func (sc *execScope) infer(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		if typ, ok := sc.vars[v.Name]; ok && typ != "" {
			return typ
		}
		// A bare type name in value position is a METHOD EXPRESSION —
		// `(*Gremlins).buildCmd`, which this codebase uses to make a spawn
		// substitutable. Reading it as an unknown variable severed every such
		// seam and left the adapter spawns reachable from nothing.
		return sc.g.knownType(sc.pkg + "." + v.Name)
	case *ast.UnaryExpr:
		return sc.infer(v.X)
	case *ast.ParenExpr:
		return sc.infer(v.X)
	case *ast.StarExpr:
		return sc.infer(v.X)
	case *ast.CompositeLit:
		return execTypeString(v.Type, sc.pkg, sc.imports)
	case *ast.TypeAssertExpr:
		if v.Type == nil {
			return ""
		}
		return execTypeString(v.Type, sc.pkg, sc.imports)
	case *ast.IndexExpr:
		return strings.TrimPrefix(sc.infer(v.X), "[]")
	case *ast.SelectorExpr:
		x, ok := v.X.(*ast.Ident)
		if !ok {
			return ""
		}
		if target, isPkg := sc.imports[x.Name]; isPkg {
			return sc.g.knownType(target + "." + v.Sel.Name)
		}
		return ""
	case *ast.CallExpr:
		res := sc.callResults(v)
		if len(res) == 0 {
			return ""
		}
		return res[0]
	}
	return ""
}

// knownType returns name if the file set declares it as a type with methods or
// as an interface, and the empty string otherwise.
func (g *execGraph) knownType(name string) string {
	if _, ok := g.typeMethods[name]; ok {
		return name
	}
	if _, ok := g.ifaces[name]; ok {
		return name
	}
	return ""
}

// resolveCallee turns a called expression into the node keys it may reach.
func (sc *execScope) resolveCallee(e ast.Expr) []string {
	switch fn := e.(type) {
	case *ast.Ident:
		if key, ok := sc.g.scope[sc.pkg][fn.Name]; ok {
			return []string{key}
		}
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			if target, isPkg := sc.imports[x.Name]; isPkg && sc.vars[x.Name] == "" {
				if key, ok := sc.g.scope[target][fn.Sel.Name]; ok {
					return []string{key}
				}
				return nil
			}
		}
		return sc.g.methodsOn(sc.infer(fn.X), fn.Sel.Name)
	}
	return nil
}

// methodsOn resolves a method call on a known receiver type.
//
// A concrete type resolves to exactly one method. An INTERFACE fans out to
// every type whose method set contains the interface's — the union, never a
// pick, because picking would have to choose and choosing the gated candidate
// is how an ungated site turns green.
func (g *execGraph) methodsOn(recv, sel string) []string {
	if recv == "" {
		return nil
	}
	recv = strings.TrimPrefix(recv, "[]")
	if set, ok := g.ifaces[recv]; ok {
		if !set[sel] {
			return nil
		}
		return g.implementers(recv, sel)
	}
	key := recv + "." + sel
	if _, ok := g.funcs[key]; ok {
		return []string{key}
	}
	return nil
}

// implementers is every method named sel on a type satisfying iface.
func (g *execGraph) implementers(iface, sel string) []string {
	want := g.ifaces[iface]
	var out []string
	for typ, have := range g.typeMethods {
		if !have[sel] {
			continue
		}
		ok := true
		for m := range want {
			if !have[m] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, typ+"."+sel)
		}
	}
	sort.Strings(out)
	return out
}

// execCobraUse returns a cobra.Command literal's Use string, first word only.
func execCobraUse(lit *ast.CompositeLit, imports map[string]string) string {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Command" {
		return ""
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok || imports[x.Name] != "cobra" {
		return ""
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if k, ok := kv.Key.(*ast.Ident); !ok || k.Name != "Use" {
			continue
		}
		if use, ok := stringLit(kv.Value); ok {
			return strings.Fields(use)[0]
		}
	}
	return ""
}

// isExecConsentCall reports whether a call is a consent check BY NAME.
//
// By the SHAPE of the name rather than by a list: `requireExecConsent` plus
// anything ending in `Consented`. A roster of known helpers here would be the
// hand-maintained list c-1 exists to kill, one level down.
func isExecConsentCall(call *ast.CallExpr) bool {
	name := ""
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		name = fn.Name
	case *ast.SelectorExpr:
		name = fn.Sel.Name
	}
	return name == "requireExecConsent" || strings.HasSuffix(name, "Consented")
}

// detectGating decides whether a node acts on a consent verdict.
//
// Two steps, because the interesting cases bind first and branch later: collect
// the identifiers a consent call bound, then look for a branch that STOPS on
// one of them — a return, a continue, a break. A switch that prints does not
// count, which is what keeps doctor out of the gated set even though
// reportLaneConsent calls LaneConsented and binds its error.
func (g *execGraph) detectGating(n *execFunc) {
	bound := map[string]bool{}
	for _, part := range n.parts {
		ast.Inspect(part.body, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok || len(assign.Rhs) != 1 {
				return true
			}
			call, ok := assign.Rhs[0].(*ast.CallExpr)
			if !ok || !isExecConsentCall(call) {
				return true
			}
			for _, lhs := range assign.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
					bound[id.Name] = true
				}
			}
			return true
		})
	}

	for _, part := range n.parts {
		ast.Inspect(part.body, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.ReturnStmt:
				for _, r := range node.Results {
					if call, ok := r.(*ast.CallExpr); ok && isExecConsentCall(call) {
						n.gates = true
					}
				}
			case *ast.IfStmt:
				if !execHasJump(node.Body) {
					return true
				}
				ast.Inspect(node.Cond, func(c ast.Node) bool {
					switch c := c.(type) {
					case *ast.Ident:
						if bound[c.Name] {
							n.gates = true
						}
					case *ast.CallExpr:
						if isExecConsentCall(c) {
							n.gates = true
						}
					}
					return true
				})
			}
			return true
		})
	}
}

// execHasJump reports whether a block can stop the flow it is in.
func execHasJump(block *ast.BlockStmt) bool {
	found := false
	ast.Inspect(block, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.ReturnStmt, *ast.BranchStmt:
			found = true
		}
		return true
	})
	return found
}

// buildCommands turns command constructors into paths and reach sets.
//
// The top-level container's own Use is dropped from its descendants' paths, so
// a path reads `survivor drain` rather than `dross survivor drain` — the same
// spelling execGatedCommands uses, which is what lets a reader compare them.
func (g *execGraph) buildCommands() {
	isChild := map[string]bool{}
	for _, n := range g.funcs {
		for _, c := range n.children {
			isChild[c] = true
		}
	}

	byKey := map[string]*execCommand{}
	var keys []string
	for key := range g.funcs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		n := g.funcs[key]
		if n.use == "" {
			continue
		}
		c := &execCommand{key: key, use: n.use}
		byKey[key] = c
		g.cmds = append(g.cmds, c)
	}

	seen := map[string]bool{}
	var assign func(key, prefix string)
	assign = func(key, prefix string) {
		if seen[key] {
			return
		}
		seen[key] = true
		if c := byKey[key]; c != nil {
			c.path = strings.TrimSpace(prefix + " " + c.use)
			prefix = c.path
		}
		n, ok := g.funcs[key]
		if !ok {
			return
		}
		for _, child := range n.children {
			assign(child, prefix)
		}
	}
	for _, key := range keys {
		if isChild[key] {
			continue
		}
		n := g.funcs[key]
		// A root that CONTAINS commands is a container: its Use names the
		// binary, and repeating it in every descendant's path would say the
		// same word forty times. A root with no children is a command in its
		// own right and keeps its Use.
		if c := byKey[key]; c != nil && len(n.children) > 0 {
			c.path = c.use
			seen[key] = true
			for _, child := range n.children {
				assign(child, "")
			}
			continue
		}
		assign(key, "")
	}
	for _, c := range g.cmds {
		if c.path == "" {
			c.path = c.use
		}
		c.reach = g.closure(c.key)
		for key := range c.reach {
			if g.funcs[key].gates {
				c.gates = true
				break
			}
		}
	}
}

// closure is every node reachable from key over CALL edges.
func (g *execGraph) closure(key string) map[string]bool {
	out := map[string]bool{}
	stack := []string{key}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if out[cur] {
			continue
		}
		out[cur] = true
		n, ok := g.funcs[cur]
		if !ok {
			continue
		}
		for callee := range n.calls {
			if !out[callee] {
				stack = append(stack, callee)
			}
		}
	}
	return out
}

// classify assigns every site its reach class.
func (g *execGraph) classify() {
	for _, s := range g.sites {
		var gated, ungated []string
		for _, c := range g.cmds {
			if !c.reach[s.owner] {
				continue
			}
			if c.gates {
				gated = append(gated, c.path)
			} else {
				ungated = append(ungated, c.path)
			}
		}
		sort.Strings(gated)
		sort.Strings(ungated)
		s.gatedVia, s.ungatedVia = gated, ungated
		switch {
		case len(gated) == 0 && len(ungated) == 0:
			s.class = execReachNone
		case len(ungated) == 0:
			s.class = execReachGated
		case len(gated) == 0:
			s.class = execReachUngated
		default:
			s.class = execReachMixed
		}
	}
	g.reportAmbiguities()
}

// reportAmbiguities names every interface fan-out whose implementers span
// packages AND disagree about reaching a spawn.
//
// Only that case. A shared method name whose candidates neither reach a
// subprocess cannot change a verdict, and reporting it would bury the one that
// can — while a fan-out inside a single package is the ordinary shape of an
// adapter set, resolved the same way whichever member is meant.
func (g *execGraph) reportAmbiguities() {
	spawns := map[string]bool{}
	for _, s := range g.sites {
		spawns[s.owner] = true
	}
	reaches := map[string]bool{}
	for key := range g.funcs {
		for k := range g.closure(key) {
			if spawns[k] {
				reaches[key] = true
				break
			}
		}
	}
	var ifaceNames []string
	for name := range g.ifaces {
		ifaceNames = append(ifaceNames, name)
	}
	sort.Strings(ifaceNames)
	for _, iface := range ifaceNames {
		for sel := range g.ifaces[iface] {
			keys := g.implementers(iface, sel)
			if len(keys) < 2 {
				continue
			}
			pkgs := map[string]bool{}
			var with, without int
			for _, k := range keys {
				pkgs[g.funcs[k].pkg] = true
				if reaches[k] {
					with++
				} else {
					without++
				}
			}
			if len(pkgs) < 2 || with == 0 || without == 0 {
				continue
			}
			g.ambiguous = append(g.ambiguous, execFinding{
				Pos:      "interface " + iface + "." + sel,
				Call:     sel,
				NoRemedy: true,
				Why: fmt.Sprintf("fans out to %v across %d packages, and only some of them reach a spawn — "+
					"this walk takes the union rather than choosing, but a reader cannot verify which is meant",
					keys, len(pkgs)),
			})
		}
	}
	sort.Slice(g.ambiguous, func(i, j int) bool { return g.ambiguous[i].Pos < g.ambiguous[j].Pos })
}

// findings is the verdict for every site, plus the reported ambiguities.
func (g *execGraph) findings() []execFinding {
	var out []execFinding
	for _, s := range g.sites {
		finding := func(why string, noRemedy bool) {
			out = append(out, execFinding{Pos: s.pos.String(), Call: s.call, Why: why, NoRemedy: noRemedy})
		}
		if s.class == execReachGated {
			// A marker HERE is itself the finding. Without this the whole
			// sweep can be cleared by marking every site, and the gate half of
			// the verdict becomes dead code nobody notices.
			if s.marked {
				finding("a site reached only by gated commands needs no exemption — it is "+s.Verdict()+"; delete the marker", true)
			}
			continue
		}
		if !s.marked {
			switch s.class {
			case execReachMixed:
				finding("is reachable from ungated command "+quote(s.ungated())+" as well as gated "+quote(s.gatedBy()), false)
			case execReachUngated:
				finding("is reachable from ungated command "+quote(s.ungated())+" and no gated one", false)
			default:
				finding("is reachable from no command at all, so no gate can cover it", false)
			}
			continue
		}
		switch {
		case s.marker.Reason == "":
			finding("carries a reasonless "+execExemptMarker+" marker", false)
		case len([]rune(s.marker.Reason)) < execExemptMinReason:
			finding(fmt.Sprintf(
				"carries a %s marker whose reason is %d characters; state in at least %d why this call cannot reach repo-authored code",
				execExemptMarker, len([]rune(s.marker.Reason)), execExemptMinReason), false)
		}
	}
	return append(out, g.ambiguous...)
}

// siteAt returns the site at a file's line, for tests asserting one verdict.
func (g *execGraph) siteAt(t *testing.T, base string, line int) *execSite {
	t.Helper()
	for _, s := range g.sites {
		if filepath.Base(s.pos.Filename) == base && s.pos.Line == line {
			return s
		}
	}
	t.Fatalf("no spawn site at %s:%d", base, line)
	return nil
}

// commandNamed returns the command whose path matches.
func (g *execGraph) commandNamed(t *testing.T, path string) *execCommand {
	t.Helper()
	for _, c := range g.cmds {
		if c.path == path {
			return c
		}
	}
	var paths []string
	for _, c := range g.cmds {
		paths = append(paths, c.path)
	}
	sort.Strings(paths)
	t.Fatalf("no command %q; found %v", path, paths)
	return nil
}

// --- reach tests ---

// reachFixture parses the two-package reach fixture, applying any source
// rewrites first. Rewriting rather than adding a third file is deliberate: the
// claims below are about a SHAPE changing — a var seam becoming a literal, a
// marker appearing — and a separate fixture per shape drifts from the original
// the moment either is edited.
func reachFixture(t *testing.T, rewrites ...[2]string) *reachFx {
	t.Helper()
	root := repoRootForDocs(t)
	dir := filepath.Join(root, "internal", "cmd", "testdata", "exec_consent", "reach")
	fset := token.NewFileSet()
	fx := &reachFx{lines: map[string][]string{}}
	var files []*ast.File
	for _, name := range []string{"root.go.txt", "helper.go.txt"} {
		path := filepath.Join(dir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(body)
		for _, r := range rewrites {
			if !strings.Contains(src, r[0]) {
				continue
			}
			src = strings.Replace(src, r[0], r[1], 1)
		}
		f, perr := parser.ParseFile(fset, path, src, parser.ParseComments)
		if perr != nil {
			t.Fatalf("%s: %v", name, perr)
		}
		// The REWRITTEN text, kept so a site can be found by what it spawns.
		// Re-reading the file from disk would index the original, and every
		// rewrite that adds a line would silently look one line off.
		fx.lines[path] = strings.Split(src, "\n")
		files = append(files, f)
	}
	fx.g = buildExecGraph(fset, files)
	return fx
}

// reachFx is a parsed reach fixture: the graph plus the source it was built
// from, which is not the source on disk once a rewrite has been applied.
type reachFx struct {
	g     *execGraph
	lines map[string][]string
}

// site finds a fixture spawn site by the argv it spawns, so an assertion
// survives the fixture gaining a line.
func (fx *reachFx) site(t *testing.T, marker string) *execSite {
	t.Helper()
	for _, s := range fx.g.sites {
		lines := fx.lines[s.pos.Filename]
		if s.pos.Line-1 >= len(lines) {
			continue
		}
		if strings.Contains(lines[s.pos.Line-1], marker) {
			return s
		}
	}
	t.Fatalf("no fixture spawn site whose line contains %q", marker)
	return nil
}

// repoExecGraph is the graph over this repository's own non-test source. The
// repo-wide zero-findings assertion is t-10's; what it is for HERE is the
// claims that are only meaningful against real code — that doctor does not
// reach the mutation runner, and that the gate half of the verdict tells
// doctor's display-only consent read from verify's acted-on one.
func repoExecGraph(t *testing.T, rewrites ...[3]string) *execGraph {
	t.Helper()
	root := repoRootForDocs(t)
	fset := token.NewFileSet()
	var files []*ast.File
	applied := map[string]bool{}
	for _, r := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, r), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			src := string(body)
			// A rewrite is {file base, before, after}. It is how a test can ask
			// "what would the enumerator say if this gate were deleted"
			// without deleting it — the only way to prove the gate half of the
			// verdict is load-bearing rather than decorative.
			for _, rw := range rewrites {
				if filepath.Base(path) != rw[0] || !strings.Contains(src, rw[1]) {
					continue
				}
				src = strings.Replace(src, rw[1], rw[2], 1)
				applied[rw[0]+rw[1]] = true
			}
			f, perr := parser.ParseFile(fset, path, src, parser.ParseComments)
			if perr != nil {
				return perr
			}
			files = append(files, f)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", r, err)
		}
	}
	for _, rw := range rewrites {
		if !applied[rw[0]+rw[1]] {
			t.Fatalf("rewrite of %s never matched %q — the test is asserting against source that moved", rw[0], rw[1])
		}
	}
	return buildExecGraph(fset, files)
}

// TestExecReachCrossesPackages is RECALL. The fixture's command reaches its
// spawn through two NAMED hops in another package; an edge set that stopped at
// the package boundary would report the site as reachable from nothing, which
// is a finding for the wrong reason and would hide the real one.
func TestExecReachCrossesPackages(t *testing.T) {
	fx := reachFixture(t)
	g := fx.g
	site := fx.site(t, `"git", "log"`)
	if !site.reaches("ungated") {
		t.Fatalf("the site is not attributed to the command that reaches it: %s", site.Verdict())
	}
	if site.class != execReachUngated {
		t.Errorf("class = %s, want %s (%s)", site.class, execReachUngated, site.Verdict())
	}
	var found bool
	for _, f := range g.findings() {
		if strings.Contains(f.Pos, "helper.go.txt") && strings.Contains(f.Why, `"ungated"`) {
			found = true
		}
	}
	if !found {
		t.Errorf("no finding names the ungated command that reaches it: %v", g.findings())
	}
}

// TestExecReachDoesNotDegenerate is PRECISION, and it is the assertion that
// keeps the gate half of the verdict alive. A graph where everything reaches
// everything makes every site MIXED, every site need a marker, and the whole
// sweep clearable by marking. `dross doctor` runs no mutation adapter, so it
// must not reach the one that spawns gremlins.
func TestExecReachDoesNotDegenerate(t *testing.T) {
	g := repoExecGraph(t)
	doctor := g.commandNamed(t, "doctor")
	verify := g.commandNamed(t, "verify")

	var gremlins *execSite
	for _, s := range g.sites {
		if filepath.Base(s.pos.Filename) == "gremlins.go" {
			gremlins = s
			break
		}
	}
	if gremlins == nil {
		t.Fatal("no spawn site in internal/mutation/gremlins.go — the walk stopped covering it")
	}
	if doctor.reach[gremlins.owner] {
		t.Errorf("`dross doctor` reaches %s, which spawns gremlins — the graph has degenerated", gremlins.owner)
	}
	if !verify.reach[gremlins.owner] {
		t.Errorf("`dross verify` does NOT reach %s — the graph lost the edge c-6 is about", gremlins.owner)
	}
}

// TestExecReachFollowsVarSeams: this codebase makes a subprocess substitutable
// by assigning it to a package-level var, so an edge set that stopped at those
// would lose most of the reach worth having. Both spellings must work — the
// alias and the literal — because a refactor between them is not a change in
// what runs.
func TestExecReachFollowsVarSeams(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rewrites [][2]string
	}{
		{"alias", nil},
		{"literal", [][2]string{{
			"var runFn = doSpawn",
			"var runFn = func() error { return doSpawn() }",
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := reachFixture(t, tc.rewrites...)
			site := fx.site(t, `"go", "test"`)
			if !site.reaches("gated") {
				t.Fatalf("the var seam was not followed: %s", site.Verdict())
			}
			if site.class != execReachGated {
				t.Errorf("class = %s, want %s (%s)", site.class, execReachGated, site.Verdict())
			}
		})
	}
}

// TestExecUnreachableIsAFinding: an edge this walk could not resolve looks
// exactly like an absent one, so "reached by no command" must fail closed. The
// alternative reads a resolution failure as "nothing to gate here".
func TestExecUnreachableIsAFinding(t *testing.T) {
	fx := reachFixture(t)
	g := fx.g
	site := fx.site(t, `"cargo", "build"`)
	if site.class != execReachNone {
		t.Fatalf("class = %s, want %s", site.class, execReachNone)
	}
	var found bool
	for _, f := range g.findings() {
		if strings.Contains(f.Pos, site.pos.String()) && strings.Contains(f.Why, "no command at all") {
			found = true
		}
	}
	if !found {
		t.Errorf("a site reachable from nothing was waved through: %v", g.findings())
	}
}

// TestExecMixedReachNeedsAMarker: a site reached by BOTH a gated and an ungated
// command is the shape ship_recover.go's gitTrim and internal/remote are in.
// Unmarked it is a finding naming the ungated reach; marked it passes, which is
// the only green state those files can reach.
func TestExecMixedReachNeedsAMarker(t *testing.T) {
	const spawn = `exec.Command("rsync", "-a", "src", "dst")`
	const marked = "//dross:exec-exempt rsync moves a tree and executes no repo-authored line\n\treturn " + spawn

	fx := reachFixture(t)
	g := fx.g
	site := fx.site(t, `"rsync"`)
	if site.class != execReachMixed {
		t.Fatalf("class = %s, want %s (%s)", site.class, execReachMixed, site.Verdict())
	}
	var named bool
	for _, f := range g.findings() {
		if strings.Contains(f.Pos, site.pos.String()) && strings.Contains(f.Why, `"ungated"`) && strings.Contains(f.Why, `"gated"`) {
			named = true
		}
	}
	if !named {
		t.Errorf("the mixed finding does not name both reaches: %v", g.findings())
	}

	withMarker := reachFixture(t, [2]string{"return " + spawn, marked})
	for _, f := range withMarker.g.findings() {
		if strings.Contains(f.Pos, "helper.go.txt") && strings.Contains(f.Call, "exec.") && strings.Contains(f.Why, "rsync") {
			t.Errorf("a marked mixed-reach site was still flagged: %v", f)
		}
	}
	marker := withMarker.site(t, `"rsync"`)
	if !marker.marked {
		t.Fatal("the rewritten fixture did not take the marker")
	}
	for _, f := range withMarker.g.findings() {
		if strings.Contains(f.Pos, marker.pos.String()) {
			t.Errorf("a marked mixed-reach site has no green state: %v", f)
		}
	}
}

// TestExecGatedOnlySiteRejectsAMarker is rule 9, and without it the sweep is
// clearable by marking every site — after which t-9's attribution proves
// nothing and the gate half of the verdict is dead code.
func TestExecGatedOnlySiteRejectsAMarker(t *testing.T) {
	fx := reachFixture(t, [2]string{
		`	return exec.Command("go", "test", "./...").Run()`,
		"\t//dross:exec-exempt this reason is long enough to pass the prose floor\n\treturn exec.Command(\"go\", \"test\", \"./...\").Run()",
	})
	g := fx.g
	site := fx.site(t, `"go", "test"`)
	if !site.marked {
		t.Fatal("the rewritten fixture did not take the marker")
	}
	if site.class != execReachGated {
		t.Fatalf("class = %s, want %s — the case this rule is about", site.class, execReachGated)
	}
	var found execFinding
	for _, f := range g.findings() {
		if strings.Contains(f.Pos, site.pos.String()) {
			found = f
		}
	}
	if found.Why == "" {
		t.Fatalf("a marker on a gated-only site was accepted: %v", g.findings())
	}
	if !strings.Contains(found.Why, "a site reached only by gated commands needs no exemption") {
		t.Errorf("the finding does not say why the marker is wrong: %s", found.Why)
	}
	if strings.Contains(found.String(), "mark it exempt") {
		t.Errorf("the finding advises the very move it is refusing:\n%s", found.String())
	}
}

// TestExecGatingIsANameRuleNotARoster: a consent helper invented tomorrow must
// gate its caller with no edit here. A roster of known helpers would be the
// hand-maintained list c-1 exists to kill, one level down.
func TestExecGatingIsANameRuleNotARoster(t *testing.T) {
	src := `package cmd

import "os/exec"

func FooConsented() error { return nil }

func gatedByANameNobodyListed() error {
	if err := FooConsented(); err != nil {
		return err
	}
	return exec.Command("git", "status").Run()
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "invented.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	g := buildExecGraph(fset, []*ast.File{f})
	if !g.funcs["cmd.gatedByANameNobodyListed"].gates {
		t.Error("an identifier ending in Consented, acted on, did not gate its caller")
	}
}

// TestExecGatingRequiresActingOnTheResult is the other half of the name rule,
// and the reason it needs a second half. doctor's reportLaneConsent calls
// LaneConsented and binds its error — to PRINT it. A rule that stopped at the
// name would mark doctor as gating and green every site doctor reaches.
func TestExecGatingRequiresActingOnTheResult(t *testing.T) {
	g := repoExecGraph(t)
	if g.funcs["cmd.reportLaneConsent"].gates {
		t.Error("doctor's display-only LaneConsented read was counted as a gate")
	}
	if g.commandNamed(t, "doctor").gates {
		t.Error("`dross doctor` was counted as gating — the gate half of the verdict is now decorative")
	}
	if !g.commandNamed(t, "verify").gates {
		t.Error("`dross verify` was NOT counted as gating, though it acts on requireExecConsent")
	}

	// The same distinction in the fixture, where the shape is visible.
	fx := reachFixture(t)
	if fx.g.commandNamed(t, "displaying").gates {
		t.Error("a command that prints a consent state was counted as gating")
	}
	if !fx.g.commandNamed(t, "gated").gates {
		t.Error("a command that returns on requireExecConsent was not counted as gating")
	}
	site := fx.site(t, `"markdownlint"`)
	if site.class != execReachUngated {
		t.Errorf("a site reached only by the display-only command is %s, want %s", site.class, execReachUngated)
	}
}

// TestExecReachReportsAmbiguity: when an interface fan-out lands in more than
// one package and the candidates disagree about reaching a spawn, the union
// this walk takes is covering for a resolution nobody can verify by reading.
// Silently attributing it to the gated candidate is how an ungated site turns
// green, so it is reported instead.
func TestExecReachReportsAmbiguity(t *testing.T) {
	files := map[string]string{
		"iface.go": `package one

type Runner interface{ Go() error }

type Quiet struct{}

func (Quiet) Go() error { return nil }

func Drive(r Runner) error { return r.Go() }
`,
		"other.go": `package two

import "os/exec"

type Loud struct{}

func (Loud) Go() error { return exec.Command("git", "gc").Run() }
`,
	}
	fset := token.NewFileSet()
	var parsed []*ast.File
	for name, src := range files {
		f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, f)
	}
	g := buildExecGraph(fset, parsed)
	if len(g.ambiguous) == 0 {
		t.Fatal("a cross-package fan-out where only one candidate spawns was resolved silently")
	}
	if !strings.Contains(g.ambiguous[0].Why, "across 2 packages") {
		t.Errorf("the report does not say what is ambiguous: %s", g.ambiguous[0].Why)
	}
	var reported bool
	for _, f := range g.findings() {
		if strings.HasPrefix(f.Pos, "interface ") {
			reported = true
		}
	}
	if !reported {
		t.Error("the ambiguity was recorded but never reported as a finding")
	}
}

// --- the toolchain spawns, proven gated by reach ---
//
// These are the sites that run a repo- or user-supplied line: `dross run`'s
// slot command, `dross test`'s local and remote suites, verify's detached
// dispatch and collection, a lane's install line, and the drain's two seams.
// Every one of them must resolve as GATED through the call graph rather than
// carrying an exemption marker — a marker on any of them would be an author
// writing down that the line is safe, which is exactly the judgement the
// consent gate exists to hand to a human instead.
//
// update.go is the single carve-out and the reason is specific: the binary it
// self-execs is the one the updater just downloaded and minisign-verified, so
// the signature — not a grant — is what makes the argv trusted.

// execConsentGatedFiles are the files whose every site must be gated by reach.
//
// Repo-relative, never by base name: `run.go` exists four times in this tree —
// internal/cmd's slot runner and the scan entry points in internal/security,
// internal/quality and internal/techdebt — and matching on the base name pulled
// three of t-8's marked sites into this task's assertion.
var execConsentGatedFiles = []string{
	"internal/cmd/run.go",
	"internal/cmd/test.go",
	"internal/cmd/verify.go",
	"internal/cmd/lane_install.go",
	"internal/cmd/survivor_drain.go",
}

// sitesIn returns every site in the given repo-relative files.
func (g *execGraph) sitesIn(rels ...string) []*execSite {
	var out []*execSite
	for _, s := range g.sites {
		for _, rel := range rels {
			if execSiteIsIn(s, rel) {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// execSiteIsIn reports whether a site sits in the named repo-relative file.
func execSiteIsIn(s *execSite, rel string) bool {
	return strings.HasSuffix(s.pos.Filename, string(filepath.Separator)+filepath.FromSlash(rel))
}

// execFindingIsIn is the same test for a rendered finding, whose Pos carries a
// trailing ":line:col".
func execFindingIsIn(f execFinding, rel string) bool {
	cut := strings.SplitN(f.Pos, ":", 2)[0]
	return strings.HasSuffix(cut, string(filepath.Separator)+filepath.FromSlash(rel))
}

// TestToolchainSpawnsResolveAsGated is c-2 for the half that must NOT be
// marked. A sweep cleared by markers proves nothing, and these are the sites
// where a marker would be furthest from the truth.
func TestToolchainSpawnsResolveAsGated(t *testing.T) {
	g := repoExecGraph(t)

	sites := g.sitesIn(execConsentGatedFiles...)
	if len(sites) == 0 {
		t.Fatal("found no spawn sites in the toolchain files — the walk stopped covering them")
	}
	for _, s := range sites {
		where := filepath.Base(s.pos.Filename)
		if s.class != execReachGated {
			t.Errorf("%s:%d is %s, want %s — this site runs a repo- or user-supplied line",
				where, s.pos.Line, s.Verdict(), execReachGated)
		}
		if s.marked {
			t.Errorf("%s:%d carries an exemption marker; a site the gate already covers must not claim one",
				where, s.pos.Line)
		}
	}
	for _, f := range g.findings() {
		for _, rel := range execConsentGatedFiles {
			if execFindingIsIn(f, rel) {
				t.Errorf("finding in a file that should be wholly gated: %s", f.String())
			}
		}
	}
}

// TestUpdateSelfExecIsTheOnlyMarkerHere pins the carve-out's boundary. A
// self-exec carve-out that widened would let any spawn claim "it is our own
// binary", so the reason must name the thing that makes it true.
func TestUpdateSelfExecIsTheOnlyMarkerHere(t *testing.T) {
	g := repoExecGraph(t)
	sites := g.sitesIn("internal/cmd/update.go")
	if len(sites) != 1 {
		t.Fatalf("update.go has %d spawn sites, want 1 — the carve-out is no longer about one call", len(sites))
	}
	s := sites[0]
	if !s.marked {
		t.Fatal("update.go's self-exec carries no exemption marker")
	}
	if !strings.Contains(strings.ToLower(s.marker.Reason), "verif") {
		t.Errorf("the reason does not name signature verification, which is the only thing that makes it safe: %q", s.marker.Reason)
	}
	for _, f := range g.findings() {
		if execFindingIsIn(f, "internal/cmd/update.go") {
			t.Errorf("update.go's marked self-exec is still a finding: %s", f.String())
		}
	}
}

// TestReachProofIsLoadBearing: without this, "gated via reach" could be a
// verdict the graph hands out to everything and the whole attribution would be
// decorative. Deleting `dross run`'s consent check — in a copy of the source,
// not on disk — must turn its spawn into a finding that names it.
func TestReachProofIsLoadBearing(t *testing.T) {
	// The CALL is removed, not just the branch under it. Leaving
	// `if err != nil { return err }` standing would still be a function acting
	// on a consent result — a weaker gate, but a real one — and the assertion
	// is about what happens when the check is gone.
	g := repoExecGraph(t, [3]string{
		"run.go",
		"consented, err := RunConsented(root, line)",
		"consented, err := true, error(nil)",
	})
	var found bool
	for _, f := range g.findings() {
		if execFindingIsIn(f, "internal/cmd/run.go") && strings.Contains(f.Why, "ungated") {
			found = true
		}
	}
	if !found {
		t.Errorf("deleting `dross run`'s consent check changed nothing — the reach proof is decorative:\n%v", g.findings())
	}
}

// TestRunSlotStillRefusesAtRuntime: the reach proof is a claim about the SHAPE
// of the code. It is not the refusal. A graph that said "gated" over a call
// site whose runtime check had been quietly weakened would be a green audit
// over a broken gate, so the refusal is exercised for real.
func TestRunSlotStillRefusesAtRuntime(t *testing.T) {
	gatedFixture(t)
	mustRunSet(t, "runtime.lint_command", "golangci-lint run")

	err := runCmd(t, Run(), "lint")
	if err == nil {
		t.Fatal("`dross run lint` ran an unconsented slot command")
	}
	if !strings.Contains(err.Error(), "lint") {
		t.Errorf("the refusal does not name the slot: %v", err)
	}
}

// --- the helper packages, marked with their reasons ---
//
// These are the scan, report and transport spawns: codex's git log and
// ast-grep, the three scanners' rev-parse, ship's gh client, and internal/
// remote's single ssh/rsync seam. None of them is reachable only from gating
// commands, and none of them can be — `dross architecture check` and `dross
// techdebt` legitimately shell git without ever running the repo's suite. So
// each carries a marker, and the marker has to earn its place.

// execConsentMarkedFiles are the helper-package files whose sites are exempt by
// marker rather than gated by reach. Repo-relative for the reason
// execConsentGatedFiles is: `run.go` is four different files in this tree.
var execConsentMarkedFiles = []string{
	"internal/codex/git.go",
	"internal/codex/ast_grep.go",
	"internal/quality/run.go",
	"internal/security/run.go",
	"internal/techdebt/run.go",
	"internal/ship/open.go",
	"internal/remote/remote.go",
}

// TestHelperPackageSpawnsAreMarked is c-2 for the half that cannot be gated.
func TestHelperPackageSpawnsAreMarked(t *testing.T) {
	g := repoExecGraph(t)
	sites := g.sitesIn(execConsentMarkedFiles...)
	if len(sites) == 0 {
		t.Fatal("found no spawn sites in the helper packages — the walk stopped covering them")
	}
	for _, f := range g.findings() {
		for _, rel := range execConsentMarkedFiles {
			if execFindingIsIn(f, rel) {
				t.Errorf("helper-package finding: %s", f.String())
			}
		}
	}
	// Every site here is either gated by reach or marked; nothing is left over.
	for _, s := range sites {
		if s.class == execReachGated {
			continue
		}
		if !s.marked {
			t.Errorf("%s:%d is %s and carries no marker", filepath.Base(s.pos.Filename), s.pos.Line, s.class)
		}
	}
}

// remoteMarkerNamesTheCallerCheck is c-7's second half, read off the marker.
//
// internal/remote is the ONE place in the tree whose exemption is conditional
// on something outside it: the transport is safe because its argv is built by
// SSHArgs/SyncArgs against the host allowlist AND because any repo-authored
// line it carries was consent-checked by the caller before dispatch. A marker
// that stated only the first half would be true about this file and wrong about
// the system — and it is exactly the half that t-3's counters exist to hold.
func remoteMarkerNamesTheCallerCheck(reason string) error {
	low := strings.ToLower(reason)
	if !strings.Contains(low, "consent") {
		return errors.New("internal/remote's marker does not name the caller-side consent check, " +
			"so the two halves of c-7 can drift apart with this file still reading as justified")
	}
	return nil
}

// TestRemoteMarkerNamesTheCallerCheck exercises that rule in both directions.
// Asserting only the live prose would leave the rule itself unproven — a check
// that always returned nil would pass just as well.
func TestRemoteMarkerNamesTheCallerCheck(t *testing.T) {
	g := repoExecGraph(t)
	var marked *execSite
	for _, s := range g.sitesIn("internal/remote/remote.go") {
		if s.marked {
			marked = s
			break
		}
	}
	if marked == nil {
		t.Fatal("internal/remote/remote.go's transport seam carries no exemption marker")
	}
	if err := remoteMarkerNamesTheCallerCheck(marked.marker.Reason); err != nil {
		t.Errorf("%v\n  reason: %q", err, marked.marker.Reason)
	}
	if remoteMarkerNamesTheCallerCheck("argv[0] is always ssh or rsync and every operand is allowlisted") == nil {
		t.Error("a marker stating only the transport half was accepted")
	}
}

// TestFuncLiteralSpawnIsAttributedThroughItsVar: ship/open.go's gh client is a
// package-level var holding a func literal, which is the shape an AST walk
// most easily loses — the spawn is not inside any FuncDecl. Deleting its marker
// must produce a finding, and one attributed through the seam rather than
// "reachable from no command".
func TestFuncLiteralSpawnIsAttributedThroughItsVar(t *testing.T) {
	g := repoExecGraph(t, [3]string{
		"open.go",
		"//dross:exec-exempt gh is the forge API client",
		"// (marker removed by the test)",
	})
	var found execFinding
	for _, f := range g.findings() {
		if execFindingIsIn(f, "internal/ship/open.go") {
			found = f
		}
	}
	if found.Why == "" {
		t.Fatalf("removing ghCommand's marker produced no finding — the func-literal seam is invisible to the walk:\n%v", g.findings())
	}
	if strings.Contains(found.Why, "no command at all") {
		t.Errorf("the site was flagged as unreachable rather than attributed through its var seam: %s", found.Why)
	}
	if !strings.Contains(found.Why, "ungated command") {
		t.Errorf("the finding does not name the command that reaches it: %s", found.Why)
	}
}

// --- the mutation runners, attributed rather than marked ---
//
// This is the concrete case c-6 names: verify → internal/mutation → gremlins.
// These four files run the repo's own suite, so if the easy way through the
// sweep were to mark them exempt, the phase would ship a green test proving
// nothing. They must come out GATED BY REACH, and a marker on any of them must
// itself be a finding.

// execConsentMutationFiles are the adapter files whose spawns run the repo's
// own tests.
var execConsentMutationFiles = []string{
	"internal/mutation/gremlins.go",
	"internal/mutation/stryker.go",
	"internal/mutation/stryker_net.go",
	"internal/mutation/launcher.go",
}

// TestMutationSpawnsAreGatedViaVerify is c-6 at its named case.
//
// Every one of these sites has `verify` among the gating commands that reach
// it; gremlins.go and launcher.go additionally have `survivor drain`, which
// gained its grant in this same phase. The assertion is membership rather than
// the rendered string, because a site gated by two commands renders both and a
// substring match on one of them would go red the day the other was added —
// which is the kind of brittleness this audit exists to remove, not create.
func TestMutationSpawnsAreGatedViaVerify(t *testing.T) {
	g := repoExecGraph(t)
	sites := g.sitesIn(execConsentMutationFiles...)
	if len(sites) == 0 {
		t.Fatal("found no spawn sites in internal/mutation — the walk stopped covering the adapters")
	}
	seen := map[string]bool{}
	for _, s := range sites {
		where := filepath.Base(s.pos.Filename)
		seen[where] = true
		if s.class != execReachGated {
			t.Errorf("%s:%d is %s, want %s — these sites run the repo's own suite", where, s.pos.Line, s.Verdict(), execReachGated)
		}
		if !s.reaches("verify") {
			t.Errorf("%s:%d is not reached by `verify` at all: %s", where, s.pos.Line, s.Verdict())
		}
		if s.marked {
			t.Errorf("%s:%d claims an exemption for a site the gate already covers", where, s.pos.Line)
		}
	}
	for _, rel := range execConsentMutationFiles {
		if !seen[filepath.Base(rel)] {
			t.Errorf("%s contributed no spawn site — the adapter set is no longer covered", rel)
		}
	}
}

// TestMarkerOnAMutationSpawnIsAFinding: the sweep must not be clearable by
// marking. These sites are reached only by gating commands, so a marker on one
// is a claim nobody needed to make.
func TestMarkerOnAMutationSpawnIsAFinding(t *testing.T) {
	g := repoExecGraph(t, [3]string{
		"gremlins.go",
		"		return exec.Command(args[0], args[1:]...)",
		"		//dross:exec-exempt an exemption nobody needed, added by the test to prove it is refused\n\t\treturn exec.Command(args[0], args[1:]...)",
	})
	var found execFinding
	for _, f := range g.findings() {
		if execFindingIsIn(f, "internal/mutation/gremlins.go") {
			found = f
		}
	}
	if found.Why == "" {
		t.Fatalf("a marker on a gated-only mutation spawn was accepted:\n%v", g.findings())
	}
	if !strings.Contains(found.Why, "needs no exemption") {
		t.Errorf("the finding does not say why the marker is wrong: %s", found.Why)
	}
}

// TestDeletingVerifysGateFlagsEveryMutationSpawn is the assertion that keeps
// the attribution honest. If "gated via reach" were a verdict the graph handed
// out regardless, deleting the gate would change nothing — and every green
// above would be decorative.
func TestDeletingVerifysGateFlagsEveryMutationSpawn(t *testing.T) {
	before := repoExecGraph(t)
	want := len(before.sitesIn(execConsentMutationFiles...))
	if want == 0 {
		t.Fatal("no mutation spawn sites to flag")
	}

	g := repoExecGraph(t, [3]string{
		"verify.go",
		"if err := requireExecConsent(); err != nil {\n\t\t\t\treturn err\n\t\t\t}\n\t\t\tphaseID := args[0]",
		"phaseID := args[0]",
	})
	got := 0
	for _, f := range g.findings() {
		for _, rel := range execConsentMutationFiles {
			if execFindingIsIn(f, rel) {
				got++
			}
		}
	}
	if got != want {
		t.Errorf("deleting verify's consent check flagged %d of %d mutation spawns — the gate half of the verdict is partly decorative", got, want)
	}
}

// TestSeveringTheGatedEdgeFailsClosed: an edge this walk cannot resolve looks
// identical to one that does not exist, so a site left reachable from nothing
// must be flagged rather than waved through. Proven on the fixture, where the
// edge can be cut cleanly — the repo has two independent paths into
// internal/mutation, and cutting one proves nothing about the rule.
func TestSeveringTheGatedEdgeFailsClosed(t *testing.T) {
	fx := reachFixture(t, [2]string{
		"			if err := helperpkg.GatedOnly(); err != nil {\n\t\t\t\treturn err\n\t\t\t}\n",
		"",
	})
	site := fx.site(t, `"go", "test"`)
	if site.class != execReachNone {
		t.Fatalf("severing the only gated edge left the site %s, want %s", site.Verdict(), execReachNone)
	}
	var found bool
	for _, f := range fx.g.findings() {
		if strings.Contains(f.Pos, site.pos.String()) && strings.Contains(f.Why, "no command at all") {
			found = true
		}
	}
	if !found {
		t.Errorf("a site reachable from nothing was waved through:\n%v", fx.g.findings())
	}
}

// TestAnExtraHopKeepsTheAttribution: wrapping a spawn in one more package is
// the cheapest way to hide it from an audit that only looks one call deep. The
// intermediate is inserted between the gated command and the spawn, and the
// verdict must not move.
func TestAnExtraHopKeepsTheAttribution(t *testing.T) {
	fx := reachFixture(t, [2]string{
		"func GatedOnly() error { return runFn() }",
		"func GatedOnly() error { return newIntermediate() }\n\nfunc newIntermediate() error { return runFn() }",
	})
	site := fx.site(t, `"go", "test"`)
	if site.class != execReachGated || !site.reaches("gated") {
		t.Errorf("an extra hop lost the attribution: %s", site.Verdict())
	}
}
