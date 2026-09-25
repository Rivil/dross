package cmd

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/txtar"
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
// subcommand restated, which is what the marker already sits next to. It is
// the shared directive floor (directive_test.go).
const execExemptMinReason = directiveMinReason

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
type execExemption = directive

// execExemptMarkers maps the line a marker EXEMPTS — the one directly below it —
// to the marker's prose. The grammar is the shared directive parser's
// (directive_test.go), so exec-exempt and taint-cleared cannot drift apart.
func execExemptMarkers(fset *token.FileSet, f *ast.File) map[int]execExemption {
	return directiveMarkers(fset, f, execExemptMarker)
}

// execFixtureGraph type-checks sources as a fixture program (on the shared
// load's dependency types) and builds the graph over it. Reach needs types and
// a call graph, so even a one-file snippet is a real, type-checked program.
func execFixtureGraph(t testing.TB, srcs ...fixtureSource) *execGraph {
	t.Helper()
	fx, err := typecheckFixture(liveImportable(t), srcs)
	if err != nil {
		t.Fatal(err)
	}
	return buildExecGraph(fx.srcView)
}

// auditExecSources is the audit over a fixture: its findings and the number of
// spawn sites it saw.
func auditExecSources(t testing.TB, srcs ...fixtureSource) ([]execFinding, int) {
	t.Helper()
	g := execFixtureGraph(t, srcs...)
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

// sweepExecConsent sweeps roots under root and audits every non-test .go file,
// mirroring subprocargs_audit_test.go's runAudit down to the _test.go skip.
func sweepExecConsent(t *testing.T, root string, roots []string) ([]execFinding, int) {
	t.Helper()
	g := sweepExecGraph(t, root, roots)
	return g.findings(), len(g.sites)
}

// sweepExecGraph is the same sweep, handing back the whole graph — what the
// repo-wide gate needs so it can measure its own coverage as well as read its
// verdicts. Over this repository it is the shared program narrowed to the
// packages under roots; over any other tree it type-checks the files found.
// Either way ONE graph over the whole set, never a graph per file: reach
// crosses packages.
func sweepExecGraph(t *testing.T, root string, roots []string) *execGraph {
	t.Helper()
	if p := sourceProgram(t); filepath.Clean(root) == filepath.Clean(p.Root) {
		return buildExecGraph(liveViewUnder(t, root, roots))
	}
	var srcs []fixtureSource
	for _, r := range roots {
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
			srcs = append(srcs, fixtureSource{Name: path, Src: body})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", r, err)
		}
	}
	return execFixtureGraph(t, srcs...)
}

// liveViewUnder is the shared view with its examined packages narrowed to those
// under root/roots. The program and call graphs stay whole.
func liveViewUnder(t *testing.T, root string, roots []string) *srcView {
	t.Helper()
	live := liveView(t)
	v := *live
	v.Pkgs = nil
	for _, p := range live.Pkgs {
		if len(p.Syntax) == 0 {
			continue
		}
		file := live.Fset.Position(p.Syntax[0].Pos()).Filename
		for _, r := range roots {
			if strings.HasPrefix(file, filepath.Join(root, r)+string(filepath.Separator)) {
				v.Pkgs = append(v.Pkgs, p)
				break
			}
		}
	}
	return &v
}

// The discovery floor. A walk that quietly stopped matching reports zero
// findings, which is indistinguishable from success, so the gate also has to
// find ENOUGH — measured today at 41 sites across 24 files, with the floor set
// roughly a quarter under both so ordinary churn does not trip it and a dropped
// scan root does.
const (
	execConsentMinSites = 30
	execConsentMinFiles = 20
)

// execConsentFloor is the coverage check, factored out so it can be exercised
// directly rather than only observed passing.
func execConsentFloor(sites, files int) error {
	if sites < execConsentMinSites {
		return fmt.Errorf("found only %d spawn sites, want at least %d — the walk has narrowed", sites, execConsentMinSites)
	}
	if files < execConsentMinFiles {
		return fmt.Errorf("found spawn sites in only %d files, want at least %d — a scan root has been dropped", files, execConsentMinFiles)
	}
	return nil
}

// execConsentDistinctFiles counts the files a sweep found spawn sites in.
func execConsentDistinctFiles(g *execGraph) int {
	seen := map[string]bool{}
	for _, s := range g.sites {
		seen[s.pos.Filename] = true
	}
	return len(seen)
}

// TestEverySpawnSiteGatedOrExempt is the gate, over this repository's own
// source. trust.go's package comment and doctor's consent section point at it
// by name, so renaming it is a documented break rather than a quiet one.
//
// Three things have to hold together, and each covers a way the other two can
// lie: zero findings (nothing ungated), a non-zero site count (the walk still
// matches), and a floor under both the site and file counts (the walk still
// matches ENOUGH).
func TestEverySpawnSiteGatedOrExempt(t *testing.T) {
	g := repoExecGraph(t)
	if err := execConsentVacuity(len(g.sites)); err != nil {
		t.Fatal(err)
	}
	if err := execConsentFloor(len(g.sites), execConsentDistinctFiles(g)); err != nil {
		t.Fatal(err)
	}
	for _, f := range g.findings() {
		t.Error(f.String())
	}
}

// TestExecConsentFloorCatchesANarrowedWalk exercises the floor in both
// directions. Asserting it only on the live tree would leave the rule itself
// unproven — a check that always returned nil would pass identically.
func TestExecConsentFloorCatchesANarrowedWalk(t *testing.T) {
	if err := execConsentFloor(execConsentMinSites, execConsentMinFiles); err != nil {
		t.Errorf("the floor rejects its own minimum: %v", err)
	}
	if err := execConsentFloor(execConsentMinSites-1, execConsentMinFiles); err == nil {
		t.Error("a narrowed site count passed the floor")
	}
	if err := execConsentFloor(execConsentMinSites, execConsentMinFiles-1); err == nil {
		t.Error("a narrowed file count passed the floor")
	}
}

// TestDroppingAScanRootFailsTheFloor: the scanned set is derived from the
// shared load, not listed, so the input nobody would notice shrinking is the
// program itself. internal/ holds the packages that actually spawn —
// mutation, remote, codex, ship — so the program restricted to cmd/ must fall
// under the floor rather than reporting a clean, much smaller tree.
func TestDroppingAScanRootFailsTheFloor(t *testing.T) {
	g := buildExecGraph(liveViewUnder(t, sourceProgram(t).Root, []string{"cmd"}))
	if err := execConsentFloor(len(g.sites), execConsentDistinctFiles(g)); err == nil {
		t.Errorf("the program restricted to cmd/ found %d sites in %d files and passed the floor — losing internal/ is invisible",
			len(g.sites), execConsentDistinctFiles(g))
	}
}

// assertExecConsentCovers pins one file inside the audited program by path. A
// package that left the load would otherwise show up as a smaller, cleaner
// sweep.
func assertExecConsentCovers(t *testing.T, rel string) {
	t.Helper()
	target := filepath.Join(sourceProgram(t).Root, filepath.FromSlash(rel))
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected %s to exist: %v", rel, err)
	}
	for _, s := range repoExecGraph(t).sites {
		if s.pos.Filename == target {
			return
		}
	}
	t.Errorf("%s holds no spawn site the audit sees — it has left the audited program", rel)
}

// TestExecConsentScansTheSpawningPackages: the four packages outside
// internal/cmd where dross actually shells out. Asserted by path rather than
// assumed, because the failure mode is silent.
func TestExecConsentScansTheSpawningPackages(t *testing.T) {
	for _, rel := range []string{
		"internal/mutation/gremlins.go",
		"internal/remote/remote.go",
		"internal/codex/git.go",
		"internal/ship/open.go",
	} {
		assertExecConsentCovers(t, rel)
	}
}

// TestExecConsentSweepIsGreenOverTheFixtureCorpus keeps the synthetic corpus
// exercised now that the gate above scans the real tree.
//
// It is not redundant with the snippet table: that runs each PASS row alone,
// while this materialises them into one file and puts the whole SWEEP over it —
// the WalkDir, the _test.go skip, the graph build. A sweep that could only find
// sites in this repository's own layout would be a gate that stopped working
// the moment it was pointed anywhere else.
func TestExecConsentSweepIsGreenOverTheFixtureCorpus(t *testing.T) {
	root, roots := execConsentFixtureCorpus(t)
	findings, sites := sweepExecConsent(t, root, roots)
	if err := execConsentVacuity(sites); err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f.String())
	}
}

// execConsentFixtureCorpus materialises snippets.txt's PASS rows as real source
// in a temp tree, and returns it as a root the sweep can walk.
func execConsentFixtureCorpus(t *testing.T) (string, []string) {
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

	findings, sites := auditExecSources(t, fixtureSource{Name: "ungated.go.txt", Src: body})
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

// auditExecSnippet type-checks one or more lines of Go inside a synthetic
// function and returns what the enumerator makes of them, plus the sites it
// saw.
func auditExecSnippet(t *testing.T, lines ...string) ([]execFinding, int) {
	t.Helper()
	src := execSnippetPreamble + strings.Join(lines, "\n") + "\n}\n"
	return auditExecSources(t, fixtureSource{Name: "snippet.go", Src: []byte(src)})
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
// The facts a reader can see come from the type-checked syntax: the spawn
// sites, the markers, the cobra commands and their AddCommand tree, and which
// functions act on a consent verdict. Every REACH edge comes from the program's
// VTA call graph (srcprog_test.go for the live tree, ssafixture_test.go for a
// fixture), joined to the syntax by position:
//
//   - a static call, a method on a concrete type, a package-level var seam
//     (`var f = g`, `var f = func(){…}`) and a method expression all resolve
//     the way the compiler resolves them — there is no local type inference
//     to keep in step with the language.
//   - an interface call resolves to the implementers whose concrete types
//     actually FLOW to it. A spawning implementer that is never passed in is
//     not attributed to the command, which is what the old union over every
//     implementer in the file set could not tell apart.
//   - a dispatch VTA resolves to NOTHING, while a CHA candidate would reach a
//     spawn, is REPORTED — never unioned. That is the case where the program
//     itself does not say what runs, so neither may the audit.
//
// AddCommand is deliberately NOT a reach edge. `Survivor()` constructs its
// children, so following those calls would make every parent reach every
// child's spawns — and `survivor drain`'s gated spawn would come out MIXED,
// reached by the ungated container that merely built it. The AddCommand
// arguments are recorded as TREE edges instead, which is what turns a
// constructor into the path `survivor drain`; their call sites are dropped from
// the reach edges.
//
// A command's reach starts at its constructor AND every function value the
// constructor hands to cobra — its RunE closure above all. cobra calls those,
// and cobra is outside the program, so no call edge leads into them.
//
// GATING is a name rule plus a use rule, and it needs both. The name rule alone
// (`requireExecConsent`, or any identifier ending in `Consented`) would mark
// doctor as gating on the strength of diag.LaneConsent, which reads a lane's
// consent state to PRINT it. So a function gates only when the call's result
// reaches a branch that STOPS: an `if` over a value the call bound, or over the
// call itself, whose body returns, continues or breaks. Nothing consults a
// roster of known helpers — c-1 kills hand-maintained lists on both sides of
// the verdict, and a `FooConsented` invented tomorrow gates its caller with no
// edit here.

// execFunc is one function with a body, and whether it acts on a consent
// verdict.
type execFunc struct {
	key   string
	fn    *ssa.Function
	pkg   string
	gates bool
	// body is the function's syntax, kept so a surgery can re-decide gating
	// with one consent call excluded.
	body ast.Node
}

// execCommand is a cobra command with its full path and everything it reaches.
type execCommand struct {
	key      string
	use      string
	path     string
	gates    bool
	reach    map[string]bool
	ctor     *ssa.Function
	children []string
}

// execSite is one spawn site with the function it sits in.
type execSite struct {
	pos     token.Position
	call    string
	owner   string
	ownerFn *ssa.Function
	marker  execExemption
	marked  bool
	class   string
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

// execGraph is the whole analysis over one program view.
type execGraph struct {
	fset  *token.FileSet
	view  *srcView
	funcs map[string]*execFunc
	// edges are the VTA call edges minus AddCommand tree edges.
	edges map[*ssa.Function][]*ssa.Function
	sites []*execSite
	cmds  []*execCommand
	// ambiguous names every dispatch the call graph resolves to nothing while
	// a CHA candidate would reach a spawn.
	ambiguous []execFinding
	markers   map[string]map[int]execExemption
}

// execKey names a function the way findings and tests spell it: pkg.Func or
// pkg.Type.Method for a declared function, the SSA name for anything else.
func execKey(fn *ssa.Function) string {
	if fn.Parent() == nil && fn.Object() != nil && fn.Pkg != nil && fn.Synthetic == "" {
		name := fn.Name()
		if recv := fn.Signature.Recv(); recv != nil {
			t := recv.Type()
			if p, ok := t.(*types.Pointer); ok {
				t = p.Elem()
			}
			if n, ok := t.(*types.Named); ok {
				name = n.Obj().Name() + "." + name
			}
		}
		return fn.Pkg.Pkg.Name() + "." + name
	}
	return fn.String()
}

// execOutermost is the top-level function a closure is nested in.
func execOutermost(fn *ssa.Function) *ssa.Function {
	for fn.Parent() != nil {
		fn = fn.Parent()
	}
	return fn
}

// buildExecGraph derives sites, commands, gating and reach over one view.
func buildExecGraph(v *srcView) *execGraph {
	g := &execGraph{
		fset:    v.Fset,
		view:    v,
		funcs:   map[string]*execFunc{},
		edges:   map[*ssa.Function][]*ssa.Function{},
		markers: map[string]map[int]execExemption{},
	}
	bySyntax := map[ast.Node]*ssa.Function{}
	for fn := range scannedFuncs(v) {
		ef := &execFunc{key: execKey(fn), fn: fn, pkg: fn.Pkg.Pkg.Name()}
		if syn := fn.Syntax(); syn != nil {
			bySyntax[syn] = fn
			switch s := syn.(type) {
			case *ast.FuncDecl:
				if s.Body != nil {
					ef.body = s.Body
				}
			case *ast.FuncLit:
				ef.body = s.Body
			}
			if ef.body != nil {
				ef.gates = execGates(ef.body, nil)
			}
		}
		g.funcs[ef.key] = ef
	}

	treeSites := map[token.Pos]bool{}
	ctorOf := map[string]*execCommand{}
	for _, p := range v.Pkgs {
		init := p.SSA.Func("init")
		for _, f := range p.Syntax {
			g.markers[v.Fset.Position(f.Pos()).Filename] = execExemptMarkers(v.Fset, f)
			g.walkSyntax(p, f, init, bySyntax, treeSites, ctorOf)
		}
	}

	for fn, n := range v.VTA.Nodes {
		if fn == nil {
			continue
		}
		for _, e := range n.Out {
			if e.Site != nil && treeSites[e.Site.Pos()] {
				continue
			}
			g.edges[fn] = append(g.edges[fn], e.Callee.Func)
		}
	}

	g.buildCommands(ctorOf)
	g.classify()
	g.reportUnresolvedDispatch()
	return g
}

// walkSyntax reads one file's facts: spawn sites, cobra commands and the
// AddCommand tree. Each is attributed to the SSA function whose syntax
// encloses it — a package-level initializer belongs to the package's init.
func (g *execGraph) walkSyntax(p *srcPkg, f *ast.File, init *ssa.Function, bySyntax map[ast.Node]*ssa.Function,
	treeSites map[token.Pos]bool, ctorOf map[string]*execCommand) {
	var stack []*ssa.Function
	enclosing := func() *ssa.Function {
		if len(stack) == 0 {
			return init
		}
		return stack[len(stack)-1]
	}
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			fn := bySyntax[n]
			if fn == nil {
				return false // a generic origin: its instances carry the body
			}
			stack = append(stack, fn)
			var body *ast.BlockStmt
			if d, ok := n.(*ast.FuncDecl); ok {
				body = d.Body
			} else {
				body = n.(*ast.FuncLit).Body
			}
			if body != nil {
				ast.Inspect(body, visit)
			}
			stack = stack[:len(stack)-1]
			return false
		case *ast.CompositeLit:
			if use := execCobraUse(p.Info, n); use != "" && enclosing() != nil {
				ctor := execOutermost(enclosing())
				key := execKey(ctor)
				if ctorOf[key] == nil {
					ctorOf[key] = &execCommand{key: key, ctor: ctor}
				}
				ctorOf[key].use = use
			}
		case *ast.CallExpr:
			obj := execCalleeObject(p.Info, n)
			if obj == nil {
				return true
			}
			if obj.Pkg() != nil && obj.Pkg().Path() == "os/exec" && (obj.Name() == "Command" || obj.Name() == "CommandContext") {
				owner := enclosing()
				if owner == nil {
					return true
				}
				pos := g.fset.Position(n.Pos())
				m, marked := g.markers[pos.Filename][pos.Line]
				g.sites = append(g.sites, &execSite{
					pos: pos, call: "exec." + obj.Name(), owner: execKey(owner), ownerFn: owner, marker: m, marked: marked,
				})
				return true
			}
			if obj.Name() == "AddCommand" && execIsCobraMethod(obj) && enclosing() != nil {
				// TREE edges: recorded, and their call sites dropped from reach
				// so a constructor does not inherit its children's spawns.
				parent := execKey(execOutermost(enclosing()))
				for _, a := range n.Args {
					child, ok := a.(*ast.CallExpr)
					if !ok {
						continue
					}
					treeSites[child.Lparen] = true
					if cobj, ok := execCalleeObject(p.Info, child).(*types.Func); ok && cobj != nil {
						if fn := g.view.Prog.FuncValue(cobj); fn != nil {
							if ctorOf[parent] == nil {
								ctorOf[parent] = &execCommand{key: parent}
							}
							ctorOf[parent].children = append(ctorOf[parent].children, execKey(fn))
						}
					}
				}
			}
		}
		return true
	}
	ast.Inspect(f, visit)
}

// execCalleeObject is the declared function or method a call names, or nil.
func execCalleeObject(info *types.Info, call *ast.CallExpr) types.Object {
	switch fn := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		return info.Uses[fn]
	case *ast.SelectorExpr:
		return info.Uses[fn.Sel]
	}
	return nil
}

func execIsCobraMethod(obj types.Object) bool {
	return obj.Pkg() != nil && obj.Pkg().Path() == "github.com/spf13/cobra"
}

// execCobraUse returns a cobra.Command literal's Use string, first word only.
func execCobraUse(info *types.Info, lit *ast.CompositeLit) string {
	t := info.TypeOf(lit)
	if t == nil {
		return ""
	}
	n, ok := t.(*types.Named)
	if !ok || n.Obj().Name() != "Command" || !execIsCobraMethod(n.Obj()) {
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
		if use, ok := stringLit(kv.Value); ok && strings.TrimSpace(use) != "" {
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

// execGates decides whether a body acts on a consent verdict.
//
// Two steps, because the interesting cases bind first and branch later: collect
// the identifiers a consent call bound, then look for a branch that STOPS on
// one of them — a return, a continue, a break. A switch that prints does not
// count, which is what keeps doctor out of the gated set even though
// diag.LaneConsent calls LaneConsented and binds its error.
//
// excluded, when set, names consent calls to treat as absent — how a surgery
// asks what the verdict would be without one gate.
func execGates(body ast.Node, excluded func(*ast.CallExpr) bool) bool {
	isConsent := func(call *ast.CallExpr) bool {
		return isExecConsentCall(call) && (excluded == nil || !excluded(call))
	}
	bound := map[string]bool{}
	ast.Inspect(body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok || !isConsent(call) {
			return true
		}
		for _, lhs := range assign.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
				bound[id.Name] = true
			}
		}
		return true
	})

	gates := false
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.ReturnStmt:
			for _, r := range node.Results {
				if call, ok := r.(*ast.CallExpr); ok && isConsent(call) {
					gates = true
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
						gates = true
					}
				case *ast.CallExpr:
					if isConsent(c) {
						gates = true
					}
				}
				return true
			})
		}
		return true
	})
	return gates
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

// commandRoots is where a command's reach starts: its constructor, every
// closure nested in it, and every function value it hands on without calling
// — cobra calls RunE, and cobra is outside the program.
func commandRoots(ctor *ssa.Function) []*ssa.Function {
	seen := map[*ssa.Function]bool{}
	var out []*ssa.Function
	var add func(fn *ssa.Function)
	add = func(fn *ssa.Function) {
		if fn == nil || seen[fn] {
			return
		}
		seen[fn] = true
		out = append(out, fn)
		for _, anon := range fn.AnonFuncs {
			add(anon)
		}
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				var callee ssa.Value
				if ci, ok := instr.(ssa.CallInstruction); ok {
					callee = ci.Common().Value
				}
				for _, op := range instr.Operands(nil) {
					if op == nil || *op == nil || *op == callee {
						continue
					}
					if f, ok := (*op).(*ssa.Function); ok {
						add(f)
					}
				}
			}
		}
	}
	add(ctor)
	return out
}

// closureFrom is every function reachable from roots over reach edges.
func (g *execGraph) closureFrom(roots []*ssa.Function) map[*ssa.Function]bool {
	out := map[*ssa.Function]bool{}
	stack := append([]*ssa.Function(nil), roots...)
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if out[cur] {
			continue
		}
		out[cur] = true
		for _, callee := range g.edges[cur] {
			if !out[callee] {
				stack = append(stack, callee)
			}
		}
	}
	return out
}

// buildCommands turns command constructors into paths and reach sets.
//
// The top-level container's own Use is dropped from its descendants' paths, so
// a path reads `survivor drain` rather than `dross survivor drain` — the same
// spelling execGatedCommands uses, which is what lets a reader compare them.
func (g *execGraph) buildCommands(ctorOf map[string]*execCommand) {
	var keys []string
	for key := range ctorOf {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	byKey := map[string]*execCommand{}
	isChild := map[string]bool{}
	for _, key := range keys {
		c := ctorOf[key]
		for _, child := range c.children {
			isChild[child] = true
		}
		if c.use == "" {
			continue
		}
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
		if c := ctorOf[key]; c != nil {
			for _, child := range c.children {
				assign(child, prefix)
			}
		}
	}
	for _, key := range keys {
		if isChild[key] {
			continue
		}
		c := ctorOf[key]
		// A root that CONTAINS commands is a container: its Use names the
		// binary, and repeating it in every descendant's path would say the
		// same word forty times. A root with no children is a command in its
		// own right and keeps its Use.
		if byKey[key] != nil && len(c.children) > 0 {
			c.path = c.use
			seen[key] = true
			for _, child := range c.children {
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
		c.reach = map[string]bool{}
		for fn := range g.closureFrom(commandRoots(c.ctor)) {
			c.reach[execKey(fn)] = true
		}
	}
	g.decideCommandGates()
}

// decideCommandGates marks a command gating when any function it reaches acts
// on a consent verdict.
func (g *execGraph) decideCommandGates() {
	for _, c := range g.cmds {
		c.gates = false
		for key := range c.reach {
			if ef := g.funcs[key]; ef != nil && ef.gates {
				c.gates = true
				break
			}
		}
	}
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
}

// reportUnresolvedDispatch names every dynamic call the VTA graph resolves to
// no callee while a CHA candidate would reach a spawn.
//
// Only that case. A dispatch VTA resolves is attributed exactly as resolved —
// no union with the implementers nobody passes in. A dispatch it cannot
// resolve, whose candidates cannot spawn, cannot change a verdict. What is
// left is a call the program itself does not pin down, next to a spawn: the
// audit says so rather than guessing either way.
func (g *execGraph) reportUnresolvedDispatch() {
	spawners := map[*ssa.Function]bool{}
	for _, s := range g.sites {
		spawners[s.ownerFn] = true
	}
	callers := map[*ssa.Function][]*ssa.Function{}
	for from, tos := range g.edges {
		for _, to := range tos {
			callers[to] = append(callers[to], from)
		}
	}
	reaches := map[*ssa.Function]bool{}
	var stack []*ssa.Function
	for fn := range spawners {
		stack = append(stack, fn)
	}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if reaches[cur] {
			continue
		}
		reaches[cur] = true
		stack = append(stack, callers[cur]...)
	}

	vtaAt := map[ssa.CallInstruction]bool{}
	for _, n := range g.view.VTA.Nodes {
		for _, e := range n.Out {
			if e.Site != nil {
				vtaAt[e.Site] = true
			}
		}
	}
	chaAt := map[ssa.CallInstruction][]*ssa.Function{}
	for _, n := range g.view.CHA.Nodes {
		for _, e := range n.Out {
			if e.Site != nil {
				chaAt[e.Site] = append(chaAt[e.Site], e.Callee.Func)
			}
		}
	}

	examined := map[*ssa.Package]bool{}
	for _, p := range g.view.Pkgs {
		examined[p.SSA] = true
	}
	escaped := g.escapedToExternal(examined)
	for fn := range scannedFuncs(g.view) {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				ci, ok := instr.(ssa.CallInstruction)
				if !ok || ci.Common().StaticCallee() != nil || vtaAt[ci] {
					continue
				}
				if _, builtin := ci.Common().Value.(*ssa.Builtin); builtin {
					continue
				}
				cands := chaAt[ci]
				// A value an external call handed back can only be module code
				// that was handed to external code first: a context's cancel
				// func is the library's, not any func() this module declares.
				if externalOrigin(ci.Common().Value, examined, map[ssa.Value]bool{}) {
					var kept []*ssa.Function
					for _, c := range cands {
						if escaped.has(c) {
							kept = append(kept, c)
						}
					}
					cands = kept
				}
				pkgs := map[string]bool{}
				var names, spawning []string
				for _, c := range cands {
					if c.Pkg != nil {
						pkgs[c.Pkg.Pkg.Path()] = true
					}
					names = append(names, execKey(c))
					if reaches[c] {
						spawning = append(spawning, execKey(c))
					}
				}
				if len(spawning) == 0 {
					continue
				}
				sort.Strings(names)
				sort.Strings(spawning)
				call := "func value"
				if ci.Common().IsInvoke() {
					call = ci.Common().Method.Name()
				}
				g.ambiguous = append(g.ambiguous, execFinding{
					Pos:      g.fset.Position(ci.Pos()).String(),
					Call:     call,
					NoRemedy: true,
					Why: fmt.Sprintf("is a dispatch the call graph resolves to no callee; its CHA candidates %v across %d packages "+
						"include %v, which reach a spawn — this audit will not take the union, so make the callee resolvable",
						names, len(pkgs), spawning),
				})
			}
		}
	}
	sort.Slice(g.ambiguous, func(i, j int) bool { return g.ambiguous[i].Pos < g.ambiguous[j].Pos })
}

// isLibrary reports whether fn is library code: bodiless and outside the
// examined packages. A bodiless function INSIDE them is the program declining
// to say what it does, not a library.
func isLibrary(fn *ssa.Function, examined map[*ssa.Package]bool) bool {
	return fn != nil && fn.Blocks == nil && !examined[fn.Pkg]
}

// externalOrigin reports whether v is, through conversions and merges, a
// result a library function returned.
func externalOrigin(v ssa.Value, examined map[*ssa.Package]bool, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return true
	}
	seen[v] = true
	switch x := v.(type) {
	case *ssa.Call:
		return isLibrary(x.Call.StaticCallee(), examined)
	case *ssa.Extract:
		return externalOrigin(x.Tuple, examined, seen)
	case *ssa.ChangeType:
		return externalOrigin(x.X, examined, seen)
	case *ssa.ChangeInterface:
		return externalOrigin(x.X, examined, seen)
	case *ssa.TypeAssert:
		return externalOrigin(x.X, examined, seen)
	case *ssa.Phi:
		for _, e := range x.Edges {
			if !externalOrigin(e, examined, seen) {
				return false
			}
		}
		return len(x.Edges) > 0
	}
	return false
}

// escapeSet is the module code handed to external functions: function values
// passed as arguments, and the concrete types of interface values passed.
type escapeSet struct {
	funcs map[*ssa.Function]bool
	types map[string]bool
	// anyFunc / anyIface: an external call received a func or interface value
	// whose provenance is not visible here, so any candidate may have escaped.
	anyFunc, anyIface bool
}

func (e escapeSet) has(fn *ssa.Function) bool {
	if e.funcs[fn] {
		return true
	}
	if recv := fn.Signature.Recv(); recv != nil {
		return e.anyIface || e.types[types.TypeString(recv.Type(), nil)]
	}
	return e.anyFunc
}

// escapedToExternal scans every call into a bodiless function for the module
// code it hands over.
func (g *execGraph) escapedToExternal(examined map[*ssa.Package]bool) escapeSet {
	es := escapeSet{funcs: map[*ssa.Function]bool{}, types: map[string]bool{}}
	var note func(v ssa.Value, seen map[ssa.Value]bool)
	note = func(v ssa.Value, seen map[ssa.Value]bool) {
		if seen[v] {
			return
		}
		seen[v] = true
		switch x := v.(type) {
		case *ssa.Function:
			es.funcs[x] = true
		case *ssa.MakeClosure:
			es.funcs[x.Fn.(*ssa.Function)] = true
		case *ssa.MakeInterface:
			t := x.X.Type()
			es.types[types.TypeString(t, nil)] = true
			if p, ok := t.(*types.Pointer); ok {
				es.types[types.TypeString(p.Elem(), nil)] = true
			} else {
				es.types[types.TypeString(types.NewPointer(t), nil)] = true
			}
			if _, isFunc := t.Underlying().(*types.Signature); isFunc {
				note(x.X, seen)
			}
		case *ssa.ChangeType:
			note(x.X, seen)
		case *ssa.Const:
		default:
			switch v.Type().Underlying().(type) {
			case *types.Signature:
				es.anyFunc = true
			case *types.Interface:
				es.anyIface = true
			}
		}
	}
	for fn := range scannedFuncs(g.view) {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				ci, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				if !isLibrary(ci.Common().StaticCallee(), examined) {
					continue
				}
				for _, a := range ci.Common().Args {
					switch a.Type().Underlying().(type) {
					case *types.Signature, *types.Interface:
						note(a, map[ssa.Value]bool{})
					}
				}
			}
		}
	}
	return es
}

// findings is the verdict for every site, plus the reported dispatches.
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

// reachFixture type-checks the two-package reach fixture, applying any source
// rewrites first. Rewriting rather than adding a third file is deliberate: the
// claims below are about a SHAPE changing — a var seam becoming a literal, a
// marker appearing — and a separate fixture per shape drifts from the original
// the moment either is edited.
func reachFixture(t *testing.T, rewrites ...[2]string) *reachFx {
	t.Helper()
	root := repoRootForDocs(t)
	dir := filepath.Join(root, "internal", "cmd", "testdata", "exec_consent", "reach")
	fx := &reachFx{lines: map[string][]string{}}
	var srcs []fixtureSource
	for _, name := range []string{"root.go.txt", "helper.go.txt"} {
		body, err := os.ReadFile(filepath.Join(dir, name))
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
		// The REWRITTEN text, kept so a site can be found by what it spawns.
		// Re-reading the file from disk would index the original, and every
		// rewrite that adds a line would silently look one line off.
		fx.lines[name] = strings.Split(src, "\n")
		srcs = append(srcs, fixtureSource{Name: name, Src: []byte(src)})
	}
	fx.g = execFixtureGraph(t, srcs...)
	return fx
}

// reachFx is a type-checked reach fixture: the graph plus the source it was
// built from, which is not the source on disk once a rewrite has been applied.
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

var (
	repoExecGraphOnce sync.Once
	repoExecGraphVal  *execGraph
)

// repoExecGraph is the graph over this repository's own non-test source: the
// shared program (srcprog_test.go), built once. Nothing here re-parses or
// re-type-checks — a test that needs a different verdict gets one by surgery
// on a COPY of this graph (withSurgery).
func repoExecGraph(t *testing.T) *execGraph {
	t.Helper()
	v := liveView(t)
	repoExecGraphOnce.Do(func() { repoExecGraphVal = buildExecGraph(v) })
	return repoExecGraphVal
}

// execSurgery is one edit to a copy of the graph, at an anchored line: the
// line where anchor (which may span lines, and must occur exactly once in the
// file) begins.
//
//   - ungate: the consent call on the anchored line stops counting — the
//     question "what if this gate were deleted", asked without deleting it.
//   - unmark: the exemption marker on the anchored line is gone.
//   - mark:   the spawn on the anchored line carries a marker with reason.
type execSurgery struct {
	file   string // repo-relative
	anchor string
	op     string
	reason string
}

// withSurgery returns a copy of g with the surgeries applied and every verdict
// re-derived, failing the test if a surgery cannot land. g itself is never
// touched: the shared graph serves every test in the binary, in any order.
func (g *execGraph) withSurgery(t *testing.T, ops ...execSurgery) *execGraph {
	t.Helper()
	c, err := g.surgery(sourceProgram(t).Root, ops...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// surgery is withSurgery's error-returning core.
func (g *execGraph) surgery(root string, ops ...execSurgery) (*execGraph, error) {
	c := g.clone()
	for _, op := range ops {
		path := filepath.Join(root, filepath.FromSlash(op.file))
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		switch n := strings.Count(string(body), op.anchor); n {
		case 0:
			return nil, fmt.Errorf("surgery on %s: anchor never matched %q — the test is asserting against source that moved", op.file, op.anchor)
		case 1:
		default:
			return nil, fmt.Errorf("surgery on %s: anchor %q matched %d times — it must name one line", op.file, op.anchor, n)
		}
		line := strings.Count(string(body)[:strings.Index(string(body), op.anchor)], "\n") + 1
		switch op.op {
		case "ungate":
			excluded := func(call *ast.CallExpr) bool {
				p := c.fset.Position(call.Pos())
				return p.Filename == path && p.Line == line
			}
			hit := false
			for key, ef := range c.funcs {
				if ef.body == nil {
					continue
				}
				from, to := c.fset.Position(ef.body.Pos()), c.fset.Position(ef.body.End())
				if from.Filename != path || line < from.Line || line > to.Line {
					continue
				}
				cp := *ef
				cp.gates = execGates(ef.body, excluded)
				c.funcs[key] = &cp
				hit = true
			}
			if !hit {
				return nil, fmt.Errorf("surgery on %s:%d: no function body holds the anchored consent call", op.file, line)
			}
		case "unmark", "mark":
			target := line
			if op.op == "unmark" {
				target = line + 1 // a marker binds the line below it
			}
			hit := false
			for i, s := range c.sites {
				if s.pos.Filename != path || s.pos.Line != target {
					continue
				}
				cp := *s
				cp.marked = op.op == "mark"
				cp.marker = execExemption{}
				if cp.marked {
					cp.marker = execExemption{Reason: op.reason}
				}
				c.sites[i] = &cp
				hit = true
			}
			if !hit {
				return nil, fmt.Errorf("surgery on %s:%d: no spawn site on the anchored line", op.file, target)
			}
		default:
			return nil, fmt.Errorf("unknown surgery %q", op.op)
		}
	}
	c.decideCommandGates()
	c.classify()
	return c, nil
}

// clone copies everything a surgery can change — functions' gating, sites'
// markers and verdicts, commands' gating — and shares what it cannot: the
// reach edges and each command's reach set.
func (g *execGraph) clone() *execGraph {
	c := &execGraph{fset: g.fset, view: g.view, edges: g.edges, markers: g.markers,
		funcs: make(map[string]*execFunc, len(g.funcs))}
	for k, f := range g.funcs {
		c.funcs[k] = f
	}
	for _, s := range g.sites {
		cp := *s
		c.sites = append(c.sites, &cp)
	}
	for _, cmd := range g.cmds {
		cp := *cmd
		c.cmds = append(c.cmds, &cp)
	}
	c.ambiguous = append(c.ambiguous, g.ambiguous...)
	return c
}

// execGraphFingerprint renders every verdict and the reach-edge count, so a
// test can prove a surgery left the shared graph as it found it.
func execGraphFingerprint(g *execGraph) string {
	edges := 0
	for _, tos := range g.edges {
		edges += len(tos)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "edges=%d\n", edges)
	for _, s := range g.sites {
		fmt.Fprintf(&b, "%s %v %s\n", s.pos, s.marked, s.Verdict())
	}
	for _, c := range g.cmds {
		fmt.Fprintf(&b, "%s %v\n", c.path, c.gates)
	}
	return b.String()
}

// execLiveSurgeries are the four live proofs' surgeries, in one place so the
// isolation test runs every one of them.
var execLiveSurgeries = []execSurgery{
	{file: "internal/cmd/run.go", op: "ungate", anchor: "consented, err := consent.RunConsented(grantStore(root), line)"},
	{file: "internal/cmd/verify.go", op: "ungate",
		anchor: "if err := requireExecConsent(); err != nil {\n\t\t\t\treturn err\n\t\t\t}\n\t\t\tphaseID := args[0]"},
	{file: "internal/ship/open.go", op: "unmark", anchor: "//dross:exec-exempt gh is the forge API client"},
	{file: "internal/mutation/gremlins.go", op: "mark", anchor: "		return exec.Command(args[0], args[1:]...)",
		reason: "an exemption nobody needed, added by the test to prove it is refused"},
}

// TestSurgeryLeavesTheSharedGraphAlone: every surgery works on a copy. The
// shared graph's verdicts, gating and reach-edge count are exactly what they
// were — the property that lets these tests run in any order.
func TestSurgeryLeavesTheSharedGraphAlone(t *testing.T) {
	g := repoExecGraph(t)
	before := execGraphFingerprint(g)
	after := g.withSurgery(t, execLiveSurgeries...)
	if got := execGraphFingerprint(g); got != before {
		t.Error("a surgery changed the shared graph")
	}
	if execGraphFingerprint(after) == before {
		t.Error("the surgeries changed nothing — they did not land")
	}
}

// TestSurgeryAnchorMustMatch: an anchor absent from source is an error naming
// it, never a no-op surgery that leaves a green proof standing.
func TestSurgeryAnchorMustMatch(t *testing.T) {
	g := repoExecGraph(t)
	_, err := g.surgery(sourceProgram(t).Root, execSurgery{file: "internal/cmd/run.go", op: "ungate", anchor: "no such line in run.go"})
	if err == nil || !strings.Contains(err.Error(), "anchor never matched") {
		t.Errorf("surgery with a missing anchor = %v, want \"anchor never matched\"", err)
	}
	_, err = g.surgery(sourceProgram(t).Root, execSurgery{file: "internal/cmd/verify.go", op: "ungate", anchor: "if err := requireExecConsent(); err != nil {"})
	if err == nil || !strings.Contains(err.Error(), "matched 2 times") {
		t.Errorf("surgery with an ambiguous anchor = %v, want it refused", err)
	}
}

// TestLiveTreeHasNoUnresolvedDispatch: the move onto the call graph left no
// dispatch next to a spawn that the program does not pin down — and no marker
// was added to get there.
func TestLiveTreeHasNoUnresolvedDispatch(t *testing.T) {
	for _, f := range repoExecGraph(t).ambiguous {
		t.Error(f.String())
	}
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
	g := execFixtureGraph(t, fixtureSource{Name: "invented.go", Src: []byte(src)})
	if !g.funcs["cmd.gatedByANameNobodyListed"].gates {
		t.Error("an identifier ending in Consented, acted on, did not gate its caller")
	}
}

// TestExecGatingRequiresActingOnTheResult is the other half of the name rule,
// and the reason it needs a second half. doctor's diag.LaneConsent calls
// LaneConsented and binds its error — to PRINT it. A rule that stopped at the
// name would mark doctor as gating and green every site doctor reaches.
func TestExecGatingRequiresActingOnTheResult(t *testing.T) {
	g := repoExecGraph(t)
	if g.funcs["diag.LaneConsent"].gates {
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
	var srcs []fixtureSource
	for _, name := range []string{"iface.go", "other.go"} {
		srcs = append(srcs, fixtureSource{Name: name, Src: []byte(files[name])})
	}
	g := execFixtureGraph(t, srcs...)
	if len(g.ambiguous) == 0 {
		t.Fatal("a cross-package fan-out where only one candidate spawns was resolved silently")
	}
	if !strings.Contains(g.ambiguous[0].Why, "across 2 packages") {
		t.Errorf("the report does not say what is ambiguous: %s", g.ambiguous[0].Why)
	}
	var reported bool
	for _, f := range g.findings() {
		if strings.HasPrefix(f.Pos, "iface.go:") && strings.Contains(f.Why, "resolves to no callee") {
			reported = true
		}
	}
	if !reported {
		t.Error("the ambiguity was recorded but never reported as a finding")
	}
}

// execTxtarGraph type-checks a multi-package txtar fixture from
// testdata/exec_consent, applying rewrites first; every rewrite must match.
func execTxtarGraph(t *testing.T, name string, rewrites ...[2]string) (*execGraph, map[string][]string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRootForDocs(t), "internal", "cmd", "testdata", "exec_consent", name))
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	for _, r := range rewrites {
		if !strings.Contains(src, r[0]) {
			t.Fatalf("%s: rewrite never matched %q", name, r[0])
		}
		src = strings.Replace(src, r[0], r[1], 1)
	}
	lines := map[string][]string{}
	var srcs []fixtureSource
	for _, f := range txtar.Parse([]byte(src)).Files {
		n := name + "/" + f.Name
		lines[n] = strings.Split(string(f.Data), "\n")
		srcs = append(srcs, fixtureSource{Name: n, Src: f.Data})
	}
	return execFixtureGraph(t, srcs...), lines
}

// TestExecDispatchFollowsTheFlow: an interface call reaches the implementers
// whose values actually flow to it. The command hands Drive only a Quiet, so
// Loud's spawn — a satisfying implementer in another package — is not the
// command's; the twin that hands it a Loud reaches the spawn.
func TestExecDispatchFollowsTheFlow(t *testing.T) {
	loudSite := func(g *execGraph) *execSite {
		t.Helper()
		for _, s := range g.sites {
			if strings.HasSuffix(s.pos.Filename, "/loud.go") {
				return s
			}
		}
		t.Fatal("dispatch.go.txt's Loud spawn is not a site")
		return nil
	}

	g, _ := execTxtarGraph(t, "dispatch.go.txt")
	if s := loudSite(g); s.reaches("quietly") || s.class != execReachNone {
		t.Errorf("Loud's spawn is %s — a command that passes only Quiet{} was attributed an implementer nobody passed in", s.Verdict())
	}
	if len(g.ambiguous) != 0 {
		t.Errorf("a dispatch the call graph resolves was reported as ambiguous: %v", g.ambiguous)
	}

	twin, _ := execTxtarGraph(t, "dispatch.go.txt",
		[2]string{"dispatchlib.Drive(dispatchlib.Quiet{})", "dispatchlib.Drive(dispatchloud.Loud{})"},
		[2]string{`_ "example.com/fixture/dispatchloud"`, `"example.com/fixture/dispatchloud"`})
	if s := loudSite(twin); !s.reaches("quietly") || s.class != execReachUngated {
		t.Errorf("handed a Loud{}, the command does not reach its spawn: %s", s.Verdict())
	}
}

// TestUnresolvedDispatchIsReported: a dispatch on a value the program cannot
// see into — a Runner returned by a bodiless function — while a candidate
// implementer spawns, is a finding naming the method and the call site. Never
// a union, never silence.
func TestUnresolvedDispatchIsReported(t *testing.T) {
	g, lines := execTxtarGraph(t, "unresolved.go.txt")
	const file = "unresolved.go.txt/cmd.go"
	line := 0
	for i, l := range lines[file] {
		if strings.Contains(l, "return r.Go()") {
			line = i + 1
		}
	}
	if line == 0 {
		t.Fatal("unresolved.go.txt no longer holds the r.Go() dispatch")
	}
	var found *execFinding
	for _, f := range g.findings() {
		if strings.HasPrefix(f.Pos, fmt.Sprintf("%s:%d:", file, line)) && f.Call == "Go" {
			f := f
			found = &f
		}
	}
	if found == nil {
		t.Fatalf("the unresolved r.Go() dispatch at %s:%d is not a finding:\n%v", file, line, g.findings())
	}
	if !strings.Contains(found.Why, "resolves to no callee") || !strings.Contains(found.Why, "Loud.Go") {
		t.Errorf("the finding does not say the dispatch is unresolved and which candidate spawns: %s", found.Why)
	}
	if !strings.Contains(found.String(), "Go(…)") {
		t.Errorf("the rendered finding does not name the method: %s", found.String())
	}
}

// execSnippetHeaders is the calibration table's FLAG/PASS header lines, byte
// for byte. Moving the audit onto a new call graph must not move a verdict, and
// the easiest way to hide a moved verdict is to edit the table to agree.
const execSnippetHeaders = `FLAG unmarked-git-spawn
FLAG unmarked-unknown-binary
FLAG unmarked-command-context
FLAG reasonless-marker
FLAG reason-is-only-the-subcommand
FLAG marker-glued-to-its-reason
FLAG spaced-comment-is-not-a-directive
FLAG marker-two-lines-above
FLAG marker-below-the-call
PASS marked-with-prose
PASS marked-command-context
PASS marked-unknown-binary
PASS marker-with-tab-before-its-reason
PASS not-a-spawn-at-all`

// TestExecConsentCalibrationIsUnchanged pins those header lines.
func TestExecConsentCalibrationIsUnchanged(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRootForDocs(t), "internal", "cmd", "testdata", "exec_consent", "snippets.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var headers []string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "FLAG ") || strings.HasPrefix(line, "PASS ") {
			headers = append(headers, line)
		}
	}
	if got := strings.Join(headers, "\n"); got != execSnippetHeaders {
		t.Errorf("snippets.txt header lines changed:\n%s\nwant:\n%s", got, execSnippetHeaders)
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
	// The CALL stops counting, not just the branch under it: surgery on a copy
	// of the shared graph re-decides gating with that consent call excluded,
	// which is what deleting it would leave.
	g := repoExecGraph(t).withSurgery(t, execSurgery{
		file: "internal/cmd/run.go", op: "ungate",
		anchor: "consented, err := consent.RunConsented(grantStore(root), line)",
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
	g := repoExecGraph(t).withSurgery(t, execSurgery{
		file: "internal/ship/open.go", op: "unmark",
		anchor: "//dross:exec-exempt gh is the forge API client",
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
	g := repoExecGraph(t).withSurgery(t, execSurgery{
		file: "internal/mutation/gremlins.go", op: "mark",
		anchor: "		return exec.Command(args[0], args[1:]...)",
		reason: "an exemption nobody needed, added by the test to prove it is refused",
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

	g := before.withSurgery(t, execSurgery{
		file: "internal/cmd/verify.go", op: "ungate",
		anchor: "if err := requireExecConsent(); err != nil {\n\t\t\t\treturn err\n\t\t\t}\n\t\t\tphaseID := args[0]",
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
