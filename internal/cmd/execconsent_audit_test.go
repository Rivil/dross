package cmd

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
}

func (f execFinding) String() string {
	return fmt.Sprintf("%s: %s(…) %s — gate it or mark it exempt with %s <reason>",
		f.Pos, f.Call, f.Why, execExemptMarker)
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

// auditExecConsentFile returns the findings for one parsed file plus the number
// of spawn sites it saw. The count is what makes a vacuous pass detectable.
func auditExecConsentFile(fset *token.FileSet, f *ast.File) ([]execFinding, int) {
	markers := execExemptMarkers(fset, f)
	var out []execFinding
	sites := 0

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, _, _, _, isSpawn := spawnArgvOf(call)
		// spawnArgvOf also recognises the git/gh helper wrappers, which are not
		// os/exec constructions. The locked spawn_surface is the construction
		// itself, so only the exec.* shapes count here.
		if !isSpawn || !strings.HasPrefix(name, "exec.") {
			return true
		}
		sites++
		pos := fset.Position(call.Pos())
		finding := func(why string) {
			out = append(out, execFinding{Pos: pos.String(), Call: name, Why: why})
		}
		marker, marked := markers[pos.Line]
		switch {
		case !marked:
			finding("is not behind a consent gate and carries no exemption marker")
		case marker.Reason == "":
			finding("carries a reasonless " + execExemptMarker + " marker")
		case len([]rune(marker.Reason)) < execExemptMinReason:
			finding(fmt.Sprintf(
				"carries a %s marker whose reason is %d characters; state in at least %d why this call cannot reach repo-authored code",
				execExemptMarker, len([]rune(marker.Reason)), execExemptMinReason))
		}
		return true
	})
	return out, sites
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
			fs, n := auditExecConsentFile(fset, f)
			findings = append(findings, fs...)
			sites += n
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", r, err)
		}
	}
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
