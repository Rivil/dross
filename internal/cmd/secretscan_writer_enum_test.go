package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/secretscan"
)

// c-4, the writer half: every non-test file under internal/ that reaches a
// file-write verb is DECLARED in secretscan.Writers(), and each declaration is
// judged — an UnderDross artifact is actually reached by scanDrossArtifacts in
// both walker modes, a MachineLocal path is actually ignored by the seed it
// names, an OutsideDross path does not resolve under .dross/ — and every
// declaration still names a file that exists and still writes.
//
// The matching rule, written down: a write verb is os.WriteFile, os.Create,
// os.CreateTemp, pathfence.WriteFile, or os.OpenFile whose flag expression
// names a write flag (O_WRONLY, O_RDWR, O_APPEND, O_CREATE, O_TRUNC). Reads
// (os.ReadFile, os.Open, os.OpenFile with O_RDONLY) do not count. CreateTemp
// is in the set because survivors.toml, state.toml and project.toml reach disk
// only through a temp file + rename, and a walker without it would pass while
// never seeing those writers.

var writeFlags = map[string]bool{"O_WRONLY": true, "O_RDWR": true, "O_APPEND": true, "O_CREATE": true, "O_TRUNC": true}

// verbSite is one write-verb call.
type verbSite struct {
	verb string
	pos  token.Pos
}

// writeVerb classifies a call as a write verb, or "" if it is not one.
func writeVerb(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	switch x.Name + "." + sel.Sel.Name {
	case "os.WriteFile", "os.Create", "os.CreateTemp", "pathfence.WriteFile":
		return x.Name + "." + sel.Sel.Name
	case "os.OpenFile":
		if len(call.Args) < 2 {
			return ""
		}
		write := false
		ast.Inspect(call.Args[1], func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && writeFlags[id.Name] {
				write = true
			}
			return true
		})
		if write {
			return "os.OpenFile(write)"
		}
	}
	return ""
}

func writeVerbsInSource(t *testing.T, fset *token.FileSet, name, src string) []verbSite {
	t.Helper()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	var out []verbSite
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if v := writeVerb(c); v != "" {
				out = append(out, verbSite{v, c.Pos()})
			}
		}
		return true
	})
	return out
}

// internalGoFiles lists every non-test .go file under internal/, as
// repo-relative slash paths ("internal/cmd/ship.go"), skipping testdata.
func internalGoFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir("..", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		n := d.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel("..", p)
		if err != nil {
			return err
		}
		out = append(out, path.Join("internal", filepath.ToSlash(rel)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// liveWriterFiles walks internal/ and returns file → write-verb sites for
// every file that has at least one, plus the total files visited.
func liveWriterFiles(t *testing.T, fset *token.FileSet) (map[string][]verbSite, int) {
	t.Helper()
	files := internalGoFiles(t)
	out := map[string][]verbSite{}
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if sites := writeVerbsInSource(t, fset, rel, string(b)); len(sites) > 0 {
			out[rel] = sites
		}
	}
	return out, len(files)
}

func writersByFile(reg []secretscan.Writer) map[string]secretscan.Writer {
	m := map[string]secretscan.Writer{}
	for _, w := range reg {
		m[w.File] = w
	}
	return m
}

// judgeWriters runs the undeclared and stale arms: a file with a write verb and
// no entry, and an entry (other than the agent sentinel) whose file has no
// write verb — deleted, or no longer writing. Returned as fn → message.
func judgeWriters(fset *token.FileSet, live map[string][]verbSite, reg []secretscan.Writer) map[string]string {
	out := map[string]string{}
	declared := writersByFile(reg)
	for file, sites := range live {
		if _, ok := declared[file]; !ok {
			out[file] = "calls " + sites[0].verb + " at " + fset.Position(sites[0].pos).String() + " with no secretscan.Writers() entry"
		}
	}
	for _, w := range reg {
		if w.File == secretscan.AgentAuthored {
			continue
		}
		if _, ok := live[w.File]; !ok {
			out[w.File] = "stale declaration — the file is gone or no longer reaches a write verb"
		}
	}
	return out
}

// placeholders fills the registry's <id>/<v>/<run>/<name> slots with one
// plausible value each.
func placeholders(artifact string) string {
	r := strings.NewReplacer("<id>", "p", "<v>", "v1.0", "<run>", "2026-01-01T00-00-00Z", "<name>", "risk")
	return r.Replace(artifact)
}

// --- live arms --------------------------------------------------------------

// TestEveryDrossWriterIsDeclared is the undeclared arm over the live tree,
// with a vacuity floor of 30 writer files.
func TestEveryDrossWriterIsDeclared(t *testing.T) {
	fset := token.NewFileSet()
	reg := secretscan.Writers()
	if errs := secretscan.ValidateWriters(reg); len(errs) != 0 {
		t.Fatalf("registry malformed: %v", errs)
	}
	live, visited := liveWriterFiles(t, fset)
	if visited < 100 {
		t.Fatalf("the walk visited %d files under internal/, want at least 100 — it is not covering the tree", visited)
	}
	if len(live) < 30 {
		t.Errorf("the walk resolved %d writer files, want at least 30 — it is passing because it found nothing to check", len(live))
	}
	for file, msg := range judgeWriters(fset, live, reg) {
		t.Errorf("%s: %s", file, msg)
	}
}

// TestMachineLocalWritersAreReallyIgnored proves each MachineLocal claim with
// git check-ignore in a repo prepared by the seed the entries name, and proves
// the arm can fail: handoff.md is NOT seeded by ensureDrossGitignore (only this
// repo's own .gitignore covers it), so a synthetic MachineLocal entry for it
// must be rejected.
func TestMachineLocalWritersAreReallyIgnored(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	if err := ensureDrossGitignore(dir); err != nil {
		t.Fatal(err)
	}
	ignored := func(p string) bool {
		return gitNoOut(dir, "check-ignore", "-q", "--", ".dross/"+p) == nil
	}

	checked := 0
	for _, w := range secretscan.Writers() {
		if w.MachineLocal == nil {
			continue
		}
		checked++
		if !ignored(w.MachineLocal.Path) {
			t.Errorf("%s: declares .dross/%s machine-local via %q, but the seed does not ignore it", w.File, w.MachineLocal.Path, w.MachineLocal.IgnoreSeed)
		}
	}
	if checked == 0 {
		t.Fatal("no MachineLocal entry — the arm asserts nothing")
	}
	if ignored("handoff.md") {
		t.Fatal("handoff.md is ignored by the seed — the non-vacuity probe below no longer probes anything; pick another unseeded path")
	}
}

// TestEveryScannedArtifactIsReached plants a synthesized token in one concrete
// instance of every UnderDross artifact and requires scanDrossArtifacts to
// report a hit AT that path — in the git walker (after `git add .dross`) and
// in the non-git walker.
func TestEveryScannedArtifactIsReached(t *testing.T) {
	var want []string
	for _, w := range secretscan.Writers() {
		if w.UnderDross == nil {
			continue
		}
		for _, a := range w.UnderDross.Artifacts {
			want = append(want, ".dross/"+placeholders(a))
		}
	}
	if len(want) < 20 {
		t.Fatalf("only %d UnderDross artifacts declared — the arm is thin", len(want))
	}
	tok := synthGitlabToken()

	plant := func(dir string) {
		for _, p := range want {
			mustWrite(t, filepath.Join(dir, filepath.FromSlash(p)), "x = \""+tok+"\"\n")
		}
	}
	check := func(mode, dir string) {
		hits, err := scanDrossArtifacts(dir)
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		got := map[string]bool{}
		for _, h := range hits {
			got[h.Location] = true
		}
		for _, p := range want {
			if !got[p] {
				t.Errorf("%s walker: no hit at %s — the artifact is declared UnderDross but the scan does not reach it", mode, p)
			}
		}
	}

	t.Run("git", func(t *testing.T) {
		dir := t.TempDir()
		gitInit(t, dir, "")
		initProject(t, dir)
		if err := ensureDrossGitignore(dir); err != nil {
			t.Fatal(err)
		}
		plant(dir)
		mustGit(t, dir, "add", ".dross")
		check("git", dir)
	})
	t.Run("walk", func(t *testing.T) {
		dir := t.TempDir()
		initProject(t, dir)
		plant(dir)
		check("non-git", dir)
	})
}

// TestOutOfScopeEntriesAreNotUnderDross: no OutsideDross path resolves under
// .dross/, and the three the phase names are declared that way.
func TestOutOfScopeEntriesAreNotUnderDross(t *testing.T) {
	outside := map[string]bool{}
	for _, w := range secretscan.Writers() {
		if w.OutsideDross == nil {
			continue
		}
		for _, p := range w.OutsideDross.Paths {
			outside[path.Base(p)] = true
			if strings.HasPrefix(p, ".dross/") || strings.Contains(p, "/.dross/") || p == ".dross" {
				t.Errorf("%s: OutsideDross path %q resolves under .dross/", w.File, p)
			}
		}
	}
	for _, name := range []string{"ARCHITECTURE.md", "telemetry.jsonl", "defaults.toml"} {
		if !outside[name] {
			t.Errorf("%s is not declared as an OutsideDross path", name)
		}
	}
}

// TestEveryFileConstIsDeclared: every `*File = "<name.ext>"` const under
// internal/ names an artifact some Writers() entry declares. The rule is the
// identifier suffix plus a dotted literal — ClassOversizedFile = "oversized-file"
// is a class label, not a file, and stays out by the dot.
func TestEveryFileConstIsDeclared(t *testing.T) {
	fset := token.NewFileSet()
	names := map[string]bool{}
	for _, a := range secretscan.ArtifactNames(secretscan.Writers()) {
		names[path.Base(a)] = true
	}
	found := 0
	for _, rel := range internalGoFiles(t) {
		b, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, rel, string(b), parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, s := range gd.Specs {
				vs := s.(*ast.ValueSpec)
				for i, id := range vs.Names {
					if !strings.HasSuffix(id.Name, "File") || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING || !strings.Contains(lit.Value, ".") {
						continue
					}
					val := strings.Trim(lit.Value, "`\"")
					found++
					if !names[val] {
						t.Errorf("%s: const %s = %q names a persisted file no Writers() entry declares", rel, id.Name, val)
					}
				}
			}
		}
	}
	if found < 12 {
		t.Errorf("found %d *File consts, want at least 12 — the const walk is not seeing them", found)
	}
}

// --- calibration -----------------------------------------------------------

// TestWriterScanTripsOnTheFixture: the verb matcher sees os.WriteFile and not
// os.ReadFile or a read-only OpenFile, and the arm flags the undeclared file.
func TestWriterScanTripsOnTheFixture(t *testing.T) {
	fset := token.NewFileSet()
	name := filepath.Join("testdata", "secretscan", "unregistered_writer.go.txt")
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	sites := writeVerbsInSource(t, fset, "internal/fixture/unregistered_writer.go", string(b))
	if len(sites) != 1 || sites[0].verb != "os.WriteFile" {
		t.Fatalf("want exactly one write verb (os.WriteFile), got %v", sites)
	}
	if fset.Position(sites[0].pos).Line != 10 {
		t.Errorf("the verb was found at line %d, want 10 (writeNote)", fset.Position(sites[0].pos).Line)
	}
	live := map[string][]verbSite{"internal/fixture/unregistered_writer.go": sites}
	findings := judgeWriters(fset, live, secretscan.Writers())
	if msg, ok := findings["internal/fixture/unregistered_writer.go"]; !ok || !strings.Contains(msg, "no secretscan.Writers() entry") {
		t.Errorf("the undeclared arm did not flag the fixture: %v", findings)
	}
}

// TestStaleWriterDeclarationFails: a synthetic entry naming a file that does
// not exist is reported; the agent-authored sentinel is exempt.
func TestStaleWriterDeclarationFails(t *testing.T) {
	fset := token.NewFileSet()
	live, _ := liveWriterFiles(t, fset)
	reg := append(secretscan.Writers(), secretscan.Writer{
		File: "internal/nothing/here.go", OutsideDross: &secretscan.OutsideDross{Why: "synthetic"},
	})
	findings := judgeWriters(fset, live, reg)
	if msg, ok := findings["internal/nothing/here.go"]; !ok || !strings.Contains(msg, "stale") {
		t.Errorf("the stale arm did not report the synthetic entry: %v", findings)
	}
	if _, ok := findings[secretscan.AgentAuthored]; ok {
		t.Error("the agent-authored sentinel was reported stale; it is exempt from resolution")
	}
	if len(findings) != 1 {
		t.Errorf("only the synthetic entry should be reported, got %v", findings)
	}
}

// TestWriterRegistryValidate feeds ValidateWriters malformed entries.
func TestWriterRegistryValidate(t *testing.T) {
	cases := []struct {
		name string
		in   []secretscan.Writer
		want string
	}{
		{"no disposition", []secretscan.Writer{{File: "internal/x.go"}}, "no disposition"},
		{"two dispositions", []secretscan.Writer{{File: "internal/x.go", UnderDross: &secretscan.UnderDross{Artifacts: []string{"a"}}, OutsideDross: &secretscan.OutsideDross{Why: "w"}}}, "2 dispositions"},
		{"empty artifacts", []secretscan.Writer{{File: "internal/x.go", UnderDross: &secretscan.UnderDross{}}}, "no Artifacts"},
		{"absolute artifact", []secretscan.Writer{{File: "internal/x.go", UnderDross: &secretscan.UnderDross{Artifacts: []string{".dross/a"}}}}, "must be .dross-relative"},
		{"no seed", []secretscan.Writer{{File: "internal/x.go", MachineLocal: &secretscan.MachineLocal{Path: "a", Why: "w"}}}, "no IgnoreSeed"},
		{"no why", []secretscan.Writer{{File: "internal/x.go", OutsideDross: &secretscan.OutsideDross{}}}, "no Why"},
		{"agent not under dross", []secretscan.Writer{{File: secretscan.AgentAuthored, OutsideDross: &secretscan.OutsideDross{Why: "w"}}}, "under .dross by definition"},
		{"duplicate", []secretscan.Writer{
			{File: "internal/x.go", OutsideDross: &secretscan.OutsideDross{Why: "w"}},
			{File: "internal/x.go", OutsideDross: &secretscan.OutsideDross{Why: "w"}},
		}, "declared twice"},
	}
	for _, c := range cases {
		errs := secretscan.ValidateWriters(c.in)
		if len(errs) != 1 {
			t.Errorf("%s: want exactly one error, got %d: %v", c.name, len(errs), errs)
			continue
		}
		if !strings.Contains(errs[0].Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", c.name, errs[0], c.want)
		}
	}
}
