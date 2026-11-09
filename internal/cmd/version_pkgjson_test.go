package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/gitrun"
)

// pkgFixture is executeFixture made a git repo, with each package.json in
// files written and — unless its path starts with "untracked/" — tracked.
func pkgFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := executeFixture(t)
	if err := gitrun.Run(dir, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	for rel, body := range files {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(rel)), body)
		if !strings.HasPrefix(rel, "untracked/") {
			if err := gitrun.Run(dir, "add", "--", rel); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func readRel(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBeginSyncsTrackedPackageJSONVersions(t *testing.T) {
	const web = "{\n  \"name\": \"web\",\n  \"engines\": { \"version\": \"9.9.9\" },\n  \"version\":   \"0.0.1\",\n\t\"private\": true\n}\n"
	untouched := map[string]string{
		"fixtures/app/package.json":  `{"version": "0.0.1"}`,
		"x/testdata/package.json":    `{"version": "0.0.1"}`,
		"vendor/lib/package.json":    `{"version": "0.0.1"}`,
		"noversion/package.json":     `{"name": "nv", "private": true}`,
		"nested-only/package.json":   `{"engines": {"version": "0.0.1"}}`,
		"untracked/web/package.json": `{"version": "0.0.1"}`,
	}
	files := map[string]string{
		"package.json":     `{"name":"root","version":"1.8.1"}`,
		"web/package.json": web,
	}
	for k, v := range untouched {
		files[k] = v
	}
	dir := pkgFixture(t, files)
	writeBeginMilestone(t, dir, "v1.8", "a", "p")
	mustRunState(t, "current_milestone", "v1.8")
	mustRunState(t, "version", "1.8.1.3")

	if _, err := beginOut(t, "p"); err != nil {
		t.Fatal(err)
	}
	if got, want := readRel(t, dir, "package.json"), `{"name":"root","version":"1.8.2"}`; got != want {
		t.Errorf("root package.json = %q, want %q", got, want)
	}
	if got, want := readRel(t, dir, "web/package.json"), strings.Replace(web, `"0.0.1"`, `"1.8.2"`, 1); got != want {
		t.Errorf("web/package.json = %q, want %q (only the top-level version bytes may change)", got, want)
	}
	for rel, body := range untouched {
		if got := readRel(t, dir, rel); got != body {
			t.Errorf("%s was rewritten to %q, want it left as %q", rel, got, body)
		}
	}
}

// An internal bump keeps major.minor.patch, so an in-step package.json stays
// byte-identical — and a drifted one is healed by the next write.
func TestStateSetVersionSyncsPackageJSON(t *testing.T) {
	dir := pkgFixture(t, map[string]string{
		"a/package.json": `{"version": "1.8.2"}`,
		"b/package.json": `{"version": "1.8.0"}`,
	})
	mustRunState(t, "version", "1.8.2.1")
	if got := readRel(t, dir, "a/package.json"); got != `{"version": "1.8.2"}` {
		t.Errorf("in-step a/package.json changed to %q", got)
	}
	if got := readRel(t, dir, "b/package.json"); got != `{"version": "1.8.2"}` {
		t.Errorf("drifted b/package.json = %q, want version 1.8.2", got)
	}
}

// A version the sync cannot rewrite safely fails the write before state.json
// moves, so state never runs ahead of the tracked files.
func TestEscapedPackageVersionFailsBeforeStateMoves(t *testing.T) {
	dir := pkgFixture(t, nil)
	mustRunState(t, "version", "1.8.1.0")
	mustWrite(t, filepath.Join(dir, "package.json"), `{"version": "1.8.`+string(rune(92))+`u0031"}`) // 92 is '\': a JSON escape for "1"
	if err := gitrun.Run(dir, "add", "--", "package.json"); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, State(), "set", "version", "1.8.2.0")
	if err == nil || !strings.Contains(err.Error(), "package.json") {
		t.Fatalf("state set = %v, want an error naming package.json", err)
	}
	if v := loadStateAt(t, dir).Version; v != "1.8.1.0" {
		t.Errorf("state.json version = %q after a failed sync, want 1.8.1.0", v)
	}
}
