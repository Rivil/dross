package cmd

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
)

// The direct-git burn-down gate. Scoped BY ORIGIN to the seven internal/cmd
// files that spawn git themselves rather than through gitRun/gitTrim/gitRead —
// wherever the value escapes. Values only compared or counted need nothing; a
// SHA, branch or path sliced out is marked at that conversion; everything else
// is kept off errors and persisted records.

var gitDirectOriginFiles = []string{
	"internal/cmd/cleantree.go",
	"internal/cmd/init.go",
	"internal/cmd/milestone_stale.go",
	"internal/cmd/pause.go",
	"internal/cmd/phase.go",
	"internal/cmd/statusline.go",
	"internal/cmd/worktree_files.go",
}

// gitDirectMarkerFiles are where this burn-down's markers live: the seven
// files, and project's remote parser, which drops userinfo from init's URL.
var gitDirectMarkerFiles = append(append([]string(nil), gitDirectOriginFiles...), "internal/project/remote.go")

// TestNoDirectGitOutputEscapes is the gate.
func TestNoDirectGitOutputEscapes(t *testing.T) {
	taint, markers := execTaintScan(liveView(t))
	root := sourceProgram(t).Root
	for _, f := range taint {
		for _, o := range f.Origins {
			rel, _ := filepath.Rel(root, o.Filename)
			if containsString(gitDirectOriginFiles, filepath.ToSlash(rel)) {
				t.Errorf("%s", execTaintMessage(f))
				break
			}
		}
	}
	for _, m := range markers {
		rel, _ := filepath.Rel(root, m.Escape.Filename)
		if containsString(gitDirectMarkerFiles, filepath.ToSlash(rel)) {
			t.Error(m.String())
		}
	}
}

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

// TestInitRawRemoteURLCarriesNoMarker: the raw URL gitRemoteOriginURL returns
// can hold a token, so it stays tainted — no marker may clear it. The clearing
// happens in project.parseGitRemote, after the userinfo is cut off.
func TestInitRawRemoteURLCarriesNoMarker(t *testing.T) {
	v := liveView(t)
	for _, p := range v.Pkgs {
		if p.Path != modulePath+"/internal/cmd" {
			continue
		}
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Name.Name != "gitRemoteOriginURL" {
					continue
				}
				from, to := v.Fset.Position(fd.Pos()).Line, v.Fset.Position(fd.End()).Line
				for bound := range directiveMarkers(v.Fset, f, taintClearedMarker) {
					if bound >= from && bound <= to {
						t.Errorf("gitRemoteOriginURL carries a taint-cleared marker binding line %d — the raw URL must stay tainted", bound)
					}
				}
				return
			}
		}
	}
	t.Fatal("gitRemoteOriginURL not found in internal/cmd")
}
