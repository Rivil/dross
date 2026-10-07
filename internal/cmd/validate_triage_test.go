package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// triageRepo is an initialised repo with phase p: a spec parking deferred id
// abc and a plan holding task t-1.
func triageRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")
	p := filepath.Join(dir, ".dross", "phases", "p")
	mustWrite(t, filepath.Join(p, "spec.toml"), `[phase]
id = "p"
title = "P"

[[criteria]]
id = "c-1"
text = "does a thing"

[[deferred]]
id = "abc"
text = "later"
target = "p"
`)
	mustWrite(t, filepath.Join(p, "plan.toml"), `[phase]
id = "p"

[[task]]
id = "t-1"
wave = 1
title = "a task"
files = ["x.go"]
covers = ["c-1"]
test_contract = ["it works"]
status = "pending"
`)
	return dir
}

// triageEntry is one resolution, with the fields every entry needs and extra
// appended verbatim.
func triageEntry(id, kind, extra string) string {
	return `[[resolution]]
id = "` + id + `"
kind = "` + kind + `"
pr = 7
url = "https://github.com/acme/w/pull/7#c"
author = "bob"
digest = "` + strings.Repeat("ab", 32) + `"
` + extra + "\n"
}

func writeTriage(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, ".dross", "phases", "p", "pr-triage.toml")
	mustWrite(t, path, body)
	return path
}

func triageLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "✗ ") && strings.Contains(l, "pr-triage.toml") {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestValidateTriageRecord(t *testing.T) {
	clean := triageEntry("c1", "conversation", `verdict = "reject"
reason = "bounded by maxPages"
at = "x.go:1"`) + triageEntry("i2", "inline", `verdict = "accept"
task = "t-1"
at = "x.go:2"`) + triageEntry("r3#1", "review", `verdict = "route"
deferred = "abc"
target = "p"
at = "x.go:3"`)

	t.Run("a clean record", func(t *testing.T) {
		dir := triageRepo(t)
		writeTriage(t, dir, clean)
		out, err := validateOut(t)
		if err != nil || !strings.Contains(out, "✓") {
			t.Fatalf("a clean record: %v\n%s", err, out)
		}
	})

	t.Run("no record adds no line", func(t *testing.T) {
		triageRepo(t)
		out, err := validateOut(t)
		if err != nil || len(triageLines(out)) != 0 {
			t.Fatalf("no record: %v\n%s", err, out)
		}
	})

	t.Run("one line per problem", func(t *testing.T) {
		dir := triageRepo(t)
		path := writeTriage(t, dir, triageEntry("c1", "conversation", `verdict = "reject"
at = "x.go:1"`)+triageEntry("c2", "conversation", `verdict = "accept"
task = "t-99"
at = "x.go:1"`)+triageEntry("c3", "conversation", `verdict = "reject"
reason = "r"
at = "../../x:1"`)+triageEntry("c3", "conversation", `verdict = "reject"
reason = "dup"
at = "x.go:1"`))
		out, err := validateOut(t)
		if err == nil {
			t.Fatalf("validate passed a broken record:\n%s", out)
		}
		lines := triageLines(out)
		for _, want := range []string{"resolution c1 reason", "resolution c2 task", "resolution c3 evidence", "resolution c3 id"} {
			n := 0
			for _, l := range lines {
				if strings.Contains(l, want) && strings.Contains(l, path) {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%d ✗ lines naming %q in %s, want 1:\n%s", n, want, path, out)
			}
		}
		if len(lines) != 4 {
			t.Errorf("%d ✗ lines for 4 problems:\n%s", len(lines), out)
		}
	})

	t.Run("a plan that does not decode stands the record down", func(t *testing.T) {
		dir := triageRepo(t)
		writeTriage(t, dir, clean)
		mustWrite(t, filepath.Join(dir, ".dross", "phases", "p", "plan.toml"), "[[task]\nnot toml")
		out, err := validateOut(t)
		if err == nil || len(triageLines(out)) != 0 {
			t.Fatalf("a broken plan: %v, triage lines %v\n%s", err, triageLines(out), out)
		}
	})

	t.Run("each line names the file once", func(t *testing.T) {
		dir := triageRepo(t)
		writeTriage(t, dir, triageEntry("c1", "conversation", `verdict = "reject"
at = "x.go:1"`))
		out, _ := validateOut(t)
		for _, l := range triageLines(out) {
			if strings.Count(l, "pr-triage.toml") != 1 {
				t.Errorf("line names the file twice: %q", l)
			}
		}
	})

	t.Run("a body key", func(t *testing.T) {
		dir := triageRepo(t)
		path := writeTriage(t, dir, triageEntry("c1", "conversation", `verdict = "reject"
reason = "r"
at = "x.go:1"
body = "pasted comment text"`))
		out, err := validateOut(t)
		lines := triageLines(out)
		if err == nil || len(lines) != 1 || !strings.Contains(lines[0], path) || !strings.Contains(lines[0], "body") {
			t.Fatalf("a body key: %v, lines %v\n%s", err, lines, out)
		}
	})
}
