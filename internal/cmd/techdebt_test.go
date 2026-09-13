package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/findings"
	"github.com/Rivil/dross/internal/techdebt"
)

// TestTechdebtCommandRegistered guards reachability: Techdebt() must be wired
// into the real root tree in cmd/dross/main.go, or `dross techdebt` would 404.
func TestTechdebtCommandRegistered(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "cmd", "dross", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(b), "cmd.Techdebt()") {
		t.Error("cmd.Techdebt() is not registered in cmd/dross/main.go root.AddCommand — `dross techdebt` would be unreachable")
	}
}

// TestTechdebtRunNoGitFallback fails if the command can't run outside a git repo:
// it must fall back to a tree walk, complete with a "nogit" run id, still scan
// the walked files, and stamp last_run (otherwise the area reads "never run"
// right after a run).
func TestTechdebtRunNoGitFallback(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "debt.go"), "package main // FIXME real debt\n")

	if err := runCmd(t, Techdebt()); err != nil {
		t.Fatalf("techdebt hard-errored outside a git repo (should fall back): %v", err)
	}

	tdDir := filepath.Join(dir, ".dross", "techdebt")
	runDir := soleRunDir(t, tdDir)
	if !strings.Contains(runDir, "nogit") {
		t.Errorf("run id = %q, want a nogit id outside a git repo", runDir)
	}
	report, err := os.ReadFile(filepath.Join(tdDir, runDir, techdebt.ReportName))
	if err != nil {
		t.Fatalf("run did not write a report: %v", err)
	}
	if !strings.Contains(string(report), "debt.go") {
		t.Errorf("tree-walk scan missed the walked file's marker:\n%s", report)
	}
	store, err := findings.LoadStore(techdebt.StatePath(filepath.Join(dir, ".dross")))
	if err != nil {
		t.Fatal(err)
	}
	if store.NeverRun() {
		t.Fatal("techdebt run did not stamp last_run; the area would read 'never run' right after a run")
	}
}

// TestTechdebtEnumeratesTrackedFiles fails if the command scans untracked files:
// the locked decision is "across tracked files", so git ls-files enumeration must
// include a tracked marker and exclude an untracked one.
func TestTechdebtEnumeratesTrackedFiles(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "init")
	mustWrite(t, filepath.Join(dir, "tracked.go"), "package x // TODO tracked debt\n")
	gitRun(t, dir, "add", "tracked.go")
	// Staged but not the untracked one — ls-files reads the index, so this is
	// "tracked" even without a commit.
	mustWrite(t, filepath.Join(dir, "untracked.go"), "package x // FIXME untracked debt\n")

	if err := runCmd(t, Techdebt()); err != nil {
		t.Fatalf("techdebt: %v", err)
	}

	tdDir := filepath.Join(dir, ".dross", "techdebt")
	report, err := os.ReadFile(filepath.Join(tdDir, soleRunDir(t, tdDir), techdebt.ReportName))
	if err != nil {
		t.Fatal(err)
	}
	s := string(report)
	if !strings.Contains(s, "tracked.go") {
		t.Errorf("report missing the tracked file's marker:\n%s", s)
	}
	if strings.Contains(s, "untracked.go") {
		t.Errorf("report scanned an untracked file (violates the tracked-files decision):\n%s", s)
	}
}

// gitRun runs a git subcommand in dir and fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestTrackedFilesExcludesDrossDir pins the ls-files path's .dross
// exclusion (v1.0 self-audit): planning artefacts are generated workflow
// state and must not enter the tech-debt scan — on this repo they
// outnumbered real code findings 5:1 before the filter.
func TestTrackedFilesExcludesDrossDir(t *testing.T) {
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q", "-b", "main")
	mustWrite(t, filepath.Join(dir, "code.go"), "package x\n")
	mustWrite(t, filepath.Join(dir, ".dross/state.json"), "{}\n")
	mustWrite(t, filepath.Join(dir, ".dross/phases/p/plan.toml"), "x\n")
	mustGit(t, dir, "add", "-f", ".")
	mustGit(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "seed")

	paths, err := trackedFiles(dir)
	if err != nil {
		t.Fatalf("trackedFiles: %v", err)
	}
	sawCode := false
	for _, p := range paths {
		// A SEGMENT match, not a substring one. `strings.Contains` here read any
		// path merely containing the text as a leak, so a run whose temp
		// directory sat under `.dross-cache` failed for its own working
		// directory's name rather than for anything trackedFiles did — measured
		// on helicon, 2026-08-17.
		if hasPathSegment(p, RootDirName) {
			t.Errorf(".dross path leaked into the scan set: %s", p)
		}
		if strings.HasSuffix(p, "code.go") {
			sawCode = true
		}
	}
	if !sawCode {
		t.Error("tracked code file missing from scan set")
	}
}

// hasPathSegment reports whether name appears as a whole element of p, under
// either separator. `.dross-cache` is not `.dross`.
func hasPathSegment(p, name string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == name {
			return true
		}
	}
	return false
}

// TestTrackedFilesSegmentMatchIsNotSubstring pins the distinction the loose
// version missed: `.dross-cache` is a different directory from `.dross`, and a
// guard that cannot tell them apart fails for the shape of a path rather than
// for anything the code did.
func TestTrackedFilesSegmentMatchIsNotSubstring(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{".dross/state.json", true},
		{"repo/.dross/phases/p/plan.toml", true},
		{"/home/rivil/.dross-cache/dross/code.go", false},
		{"/home/rivil/.drossy/code.go", false},
		{"src/dross/main.go", false},
		{"code.go", false},
	} {
		if got := hasPathSegment(tc.path, RootDirName); got != tc.want {
			t.Errorf("hasPathSegment(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// skipSetFixtureRepo lays out the tree TestTrackedFilesSkipsFixtureDirs enumerates:
// two fixture-dir files that trip the size heuristics, a build/ script with a
// marker, and two files that must survive — one under a directory whose name
// merely starts with "testdata".
func skipSetFixtureRepo(t *testing.T) (dir string, skipped, kept []string) {
	t.Helper()
	dir = t.TempDir()
	big := strings.Repeat("line\n", 700)
	wide := strings.Repeat("x", 500) + "\n"
	files := map[string]string{
		"fixtures/big.txt":      big,
		"testdata/wide.txt":     wide,
		"build/x.sh":            "#!/bin/sh\n# TODO remove\n",
		"testdata-like/keep.go": "package keep\n",
		"src/ok.go":             "package ok\n",
	}
	for rel, body := range files {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(rel)), body)
	}
	for _, rel := range []string{"fixtures/big.txt", "testdata/wide.txt", "build/x.sh"} {
		skipped = append(skipped, filepath.Join(dir, filepath.FromSlash(rel)))
	}
	for _, rel := range []string{"testdata-like/keep.go", "src/ok.go"} {
		kept = append(kept, filepath.Join(dir, filepath.FromSlash(rel)))
	}
	return dir, skipped, kept
}

// TestTrackedFilesSkipsFixtureDirs pins the shared skip set onto BOTH
// enumeration branches (c-3): fixture files that would fire the size
// heuristics, and a build/ script with a marker, never reach the scan, while a
// directory that merely starts with "testdata" survives (segment, not
// substring). The raw set is scanned too, proving the thresholds would have
// fired had the files been admitted.
func TestTrackedFilesSkipsFixtureDirs(t *testing.T) {
	check := func(t *testing.T, dir string, skipped, kept []string) {
		t.Helper()
		got, err := trackedFiles(dir)
		if err != nil {
			t.Fatalf("trackedFiles: %v", err)
		}
		gotSet := map[string]bool{}
		for _, p := range got {
			gotSet[p] = true
		}
		for _, p := range kept {
			if !gotSet[p] {
				t.Errorf("trackedFiles dropped %s — a substring match on the skip set, or a missing branch", p)
			}
		}
		for _, p := range skipped {
			if gotSet[p] {
				t.Errorf("trackedFiles admitted %s — the shared skip set is not applied on this branch", p)
			}
		}
		if len(got) != len(kept) {
			t.Errorf("trackedFiles = %v, want exactly %v", got, kept)
		}
		if fs := techdebt.Scan(got, techdebt.DefaultThresholds); len(fs) != 0 {
			t.Errorf("scan over the enumerated set yielded %d findings, want 0: %+v", len(fs), fs)
		}
		raw := techdebt.Scan(append(append([]string{}, skipped...), kept...), techdebt.DefaultThresholds)
		want := map[string]int{techdebt.ClassOversizedFile: 1, techdebt.ClassLongLine: 1, techdebt.ClassMarker: 1}
		for class, n := range want {
			c := 0
			for _, f := range raw {
				if f.Class == class {
					c++
				}
			}
			if c != n {
				t.Errorf("raw scan: %d %s findings, want %d (the fixtures must trip the thresholds for the test to prove anything)", c, class, n)
			}
		}
	}

	t.Run("git ls-files branch", func(t *testing.T) {
		dir, skipped, kept := skipSetFixtureRepo(t)
		mustGit(t, dir, "init", "-q", "-b", "main")
		mustGit(t, dir, "add", "-f", ".")
		check(t, dir, skipped, kept)
	})
	t.Run("no-git walk branch", func(t *testing.T) {
		dir, skipped, kept := skipSetFixtureRepo(t)
		check(t, dir, skipped, kept)
	})
}

// techdebtExcludeRepo inits a dross repo whose project.toml carries the given
// [techdebt] exclude list, plus one marker file inside and one outside it.
func techdebtExcludeRepo(t *testing.T, exclude string) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, ".dross", "project.toml"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n[techdebt]\n  exclude = [" + exclude + "]\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "skipme", "a.go"), "package a // FIXME inside the exclude\n")
	mustWrite(t, filepath.Join(dir, "keep.go"), "package k // TODO outside it\n")
	return dir
}

// TestTechdebtAppliesProjectExclude (c-2): the command honours [techdebt]
// exclude on both enumeration branches — the excluded marker never reaches the
// report, the kept one does.
func TestTechdebtAppliesProjectExclude(t *testing.T) {
	check := func(t *testing.T, dir string) {
		t.Helper()
		if err := runCmd(t, Techdebt()); err != nil {
			t.Fatalf("techdebt: %v", err)
		}
		tdDir := filepath.Join(dir, ".dross", "techdebt")
		report, err := os.ReadFile(filepath.Join(tdDir, soleRunDir(t, tdDir), techdebt.ReportName))
		if err != nil {
			t.Fatal(err)
		}
		s := string(report)
		if !strings.Contains(s, "keep.go") {
			t.Errorf("report lost the kept file's marker:\n%s", s)
		}
		if strings.Contains(s, "skipme") {
			t.Errorf("report contains the excluded file — [techdebt] exclude not applied:\n%s", s)
		}
	}
	t.Run("git ls-files branch", func(t *testing.T) {
		dir := techdebtExcludeRepo(t, `"skipme/"`)
		mustGit(t, dir, "init", "-q", "-b", "main")
		mustGit(t, dir, "add", "-f", ".")
		check(t, dir)
	})
	t.Run("no-git walk branch", func(t *testing.T) {
		check(t, techdebtExcludeRepo(t, `"skipme/"`))
	})
}

// TestTechdebtBadExcludeErrors: a pattern that does not compile is the command's
// error, naming the entry, and no run dir is written — a typo must never widen
// the scan silently or leave a half-run behind.
func TestTechdebtBadExcludeErrors(t *testing.T) {
	dir := techdebtExcludeRepo(t, `"["`)
	err := runCmd(t, Techdebt())
	if err == nil {
		t.Fatal("techdebt succeeded with an uncompilable exclude pattern")
	}
	if !strings.Contains(err.Error(), `"["`) {
		t.Fatalf("error %q does not name the bad entry", err)
	}
	if entries, rerr := os.ReadDir(filepath.Join(dir, ".dross", "techdebt")); rerr == nil {
		for _, e := range entries {
			if e.IsDir() {
				t.Errorf("run dir %s was written despite the exclude error", e.Name())
			}
		}
	}
}
