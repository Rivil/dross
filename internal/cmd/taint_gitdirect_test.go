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

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
)

// The direct-git burn-down's guards: the internal/cmd files that used to spawn
// git themselves and now read through gitrun — status and ls-files through Raw,
// symbolic-ref through Trim, git show and the origin URL through Read. Values
// only compared or counted need nothing; a SHA, branch or path sliced out is
// marked at that conversion; everything else is kept off errors and persisted
// records. The zero-findings gate is retired into TestNoSpawnOutputEscapes
// (taint_audit_test.go); these are the behaviour guards beside it.

// TestStaleDiffBufferNeverReachesAnError: a copy of the live patchIDOfDiff is
// clean, and the regressed shape — the diff buffer quoted into an error —
// trips.
func TestStaleDiffBufferNeverReachesAnError(t *testing.T) {
	fx := loadFixture(t, fixturePath("taint", "stale_diff.go.txt"))
	assertCorpus(t, fx, "taint", taintHits(runTaint(fx.srcView, execTaintPolicy())))
}

// stubGitOnPath puts an executable git running script first on PATH.
func stubGitOnPath(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestPhaseGitShowOutputStaysOffTheError: git show failing with its own text
// on both streams leaves that text out of the refusal resolveCompleteBase
// returns.
func TestPhaseGitShowOutputStaysOffTheError(t *testing.T) {
	trace := filepath.Join(t.TempDir(), "argv")
	t.Setenv("STUB_TRACE", trace)
	stubGitOnPath(t, `echo "$@" >> "$STUB_TRACE"`+"\necho CANARY-SHOW\necho CANARY-SHOW >&2\nexit 128")
	p := &project.Project{}
	p.Repo.GitMainBranch = "main"
	_, err := resolveCompleteBase(t.TempDir(), t.TempDir(), p, &state.State{}, "some-phase", "")
	if err == nil {
		t.Fatal("no recorded base and a failing git resolved a base anyway")
	}
	if strings.Contains(err.Error(), "CANARY-SHOW") {
		t.Errorf("git show's output reached the error: %v", err)
	}
	if !strings.Contains(err.Error(), "no recorded forked-from base") {
		t.Errorf("the refusal is not the no-recorded-base one: %v", err)
	}
	argv, _ := os.ReadFile(trace)
	if !strings.Contains(string(argv), " show ") {
		t.Errorf("the stubbed git never ran show — the canary proves nothing; it saw:\n%s", argv)
	}
}

// TestRemoteURLUserinfoNeverPersists: a remote carrying a token yields a
// detected remote — what init writes to project.toml — with no trace of it.
func TestRemoteURLUserinfoNeverPersists(t *testing.T) {
	stubGitOnPath(t, "echo 'https://u:CANARY-TOK@host.example/o/r.git'")
	t.Setenv("HOME", t.TempDir())
	r, got := seedRemote(t.TempDir())
	if !got {
		t.Fatal("the stubbed remote was not detected")
	}
	if r.URL != "https://host.example/o/r" {
		t.Errorf("detected URL = %q, want https://host.example/o/r", r.URL)
	}
	if s := fmt.Sprintf("%+v", r); strings.Contains(s, "CANARY-TOK") {
		t.Errorf("the remote's userinfo reached the detected remote: %s", s)
	}
}

// remoteURLReads returns every gitrun.Read call in internal/cmd that reads
// `remote get-url`, as file and line, with the function it sits in.
func remoteURLReads(v *srcView) map[token.Position]string {
	out := map[token.Position]string{}
	for _, p := range v.Pkgs {
		if p.Path != modulePath+"/internal/cmd" {
			continue
		}
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Read" {
						return true
					}
					if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "gitrun" {
						return true
					}
					for _, a := range call.Args {
						if lit, ok := stringLit(a); ok && lit == "get-url" {
							pos := v.Fset.Position(call.Pos())
							out[token.Position{Filename: pos.Filename, Line: pos.Line}] = fd.Name.Name
						}
					}
					return true
				})
			}
		}
	}
	return out
}

// TestInitRawRemoteURLCarriesNoMarker: the raw origin URL can hold a token, so
// it stays tainted — no marker may clear it where it is read. That read is a
// gitrun.Read in seedRemote (init) and in doctor's Remote: check; the clearing
// happens in project.parseGitRemote, after the userinfo is cut off. And the
// read cannot move to Trim to borrow its marker: the verb pin refuses it.
func TestInitRawRemoteURLCarriesNoMarker(t *testing.T) {
	v := liveView(t)
	reads := remoteURLReads(v)
	byFunc := map[string]bool{}
	for pos, fn := range reads {
		byFunc[fn] = true
		for _, m := range taintMarkersIn(t, "internal/cmd") {
			if m.file == pos.Filename && m.bound == pos.Line {
				t.Errorf("%s:%d (%s) reads the origin URL under a taint-cleared marker — the raw URL must stay tainted",
					filepath.Base(pos.Filename), pos.Line, fn)
			}
		}
	}
	if !byFunc["seedRemote"] {
		t.Error("seedRemote no longer reads the origin through gitrun.Read")
	}
	if len(reads) < 2 {
		t.Errorf("found %d origin-URL reads, want seedRemote's and doctor's: %v", len(reads), reads)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "internal/cmd/x.go", "package cmd\n\nfunc f() {\n\t_, _ = gitrun.Trim(dir, \"remote\", \"get-url\", \"origin\")\n}\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := gitTrimProblems(fset, []*ast.File{f}); len(got) != 1 {
		t.Errorf("routing `remote get-url` through Trim was not refused by the verb pin: %v", got)
	}
}

// commitFile writes body to name in dir and commits it.
func commitFile(t *testing.T, dir, name, body string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, name), body)
	mustGit(t, dir, "add", "--", name)
	mustGit(t, dir, "commit", "-q", "-m", "add "+name)
}

// TestPorcelainKeepsTheFirstStatusColumn: an unstaged modification is the
// porcelain line " M a.go". Every status reader goes through gitrun.Raw
// untrimmed, so the leading space survives and the path slices out whole.
func TestPorcelainKeepsTheFirstStatusColumn(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	commitFile(t, dir, "a.go", "package a\n")
	mustWrite(t, filepath.Join(dir, "a.go"), "package a // changed\n")

	files, err := worktreeChangedFiles(dir)
	if err != nil || len(files) != 1 || files[0] != "a.go" {
		t.Errorf("worktreeChangedFiles = %q, %v; want [a.go]", files, err)
	}
	if got := dirtySummary(dir); got != "1 file(s): a.go" {
		t.Errorf("dirtySummary = %q, want \"1 file(s): a.go\"", got)
	}
	_, err = autoCommitDrossDirt(dir, "testing")
	if err == nil || !strings.Contains(err.Error(), " M a.go") {
		t.Errorf("autoCommitDrossDirt = %v, want a dirty-tree refusal quoting \" M a.go\"", err)
	}
}

// TestTrackedFilesKeepsAwkwardNames: techdebt's tracked-file list splits
// `ls-files -z` on NUL with nothing trimmed, so a name with a space or a
// newline in it comes back whole.
func TestTrackedFilesKeepsAwkwardNames(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	commitFile(t, dir, "with space.go", "package a\n")
	commitFile(t, dir, "line\nbreak.go", "package a\n")
	paths, err := trackedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{filepath.Join(dir, "with space.go"): true, filepath.Join(dir, "line\nbreak.go"): true}
	if len(paths) != len(want) {
		t.Fatalf("trackedFiles = %q, want the two awkward names", paths)
	}
	for _, p := range paths {
		if !want[p] {
			t.Errorf("trackedFiles returned %q, which is not one of the committed names", p)
		}
	}
}

// TestPhaseRefRecordedBaseRefusesGarbage: the phase ref's committed
// changes.json is decoded by changes.Decode; a blob that is not a changes
// record yields no base rather than a guess.
func TestPhaseRefRecordedBaseRefusesGarbage(t *testing.T) {
	stubGitOnPath(t, "echo 'not json at all'")
	if got := phaseRefRecordedBase(t.TempDir(), "p"); got != "" {
		t.Errorf("a garbage blob resolved a base: %q", got)
	}
	stubGitOnPath(t, `echo '{"phase":"p","base":"milestone/v9","pr":42,"tasks":{}}'`)
	if got := phaseRefRecordedBase(t.TempDir(), "p"); got != "milestone/v9" {
		t.Errorf("a recorded base read back as %q, want milestone/v9", got)
	}
	if got := originRecordedPR(t.TempDir(), "main", "p"); got != 42 {
		t.Errorf("a recorded PR read back as %d, want 42", got)
	}
}

// TestDirectGitWrappersAreGone: the two cmd wrappers the direct reads used to
// go through are deleted, not left beside gitrun as a second way in.
func TestDirectGitWrappersAreGone(t *testing.T) {
	v := liveView(t)
	for _, p := range v.Pkgs {
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && (fd.Name.Name == "gitStatusRaw" || fd.Name.Name == "gitRemoteOriginURL") {
					t.Errorf("%s declares %s — status and origin reads go through gitrun", v.Fset.Position(fd.Pos()), fd.Name.Name)
				}
			}
		}
	}
}

// TestCleantreePorcelainMarkerIsLoadBearing: autoCommitDrossDirt's marker is
// the one place Raw's porcelain status is cleared; deleted, the status reaches
// the dirty-tree refusal (dirtyTreeError, in phase.go) as an escape that starts
// at Raw's spawn.
func TestCleantreePorcelainMarkerIsLoadBearing(t *testing.T) {
	var marker *taintMarker
	for _, m := range taintMarkersIn(t, "internal/cmd") {
		if filepath.Base(m.file) == "cleantree.go" && strings.Contains(m.Reason, "porcelain") {
			m := m
			marker = &m
		}
	}
	if marker == nil {
		t.Fatal("cleantree.go carries no porcelain taint-cleared marker")
	}
	raw := spawnLineIn(t, gitrunPath, "Raw")
	taint, _ := execTaintScan(viewWithoutComment(liveView(t), marker.file, marker.line))
	for _, f := range taint {
		if filepath.Base(f.Escape.Filename) != "phase.go" {
			continue
		}
		for _, o := range f.Origins {
			if filepath.Base(o.Filename) == "gitrun.go" && o.Line == raw {
				return
			}
		}
	}
	t.Errorf("removing cleantree.go's porcelain marker at line %d reported no escape from gitrun.Raw's spawn (line %d) into the dirty-tree refusal:\n%v",
		marker.line, raw, taint)
}

// TestDecodeRefusesWithoutQuotingTheBlob: changes.Decode reads a blob out of a
// ref's tree, so its refusal names no byte of it — a JSON syntax error would
// quote the character it choked on.
func TestDecodeRefusesWithoutQuotingTheBlob(t *testing.T) {
	_, err := changes.Decode([]byte("CANARY-BLOB"))
	if !errors.Is(err, changes.ErrNotARecord) || err.Error() != changes.ErrNotARecord.Error() {
		t.Errorf("Decode(garbage) = %v, want the bare ErrNotARecord — a wrapped decoder message quotes the blob", err)
	}
	ch, err := changes.Decode([]byte(`{"phase":"p","base":"main","pr":7}`))
	if err != nil || ch.Base != "main" || ch.PR != 7 || ch.Tasks == nil {
		t.Errorf("Decode(record) = %+v, %v; want base main, PR 7 and a non-nil task map", ch, err)
	}
}
