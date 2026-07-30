package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// c-5 says --json must emit "the same data its default rendering shows". These
// tests hold each config-document `show` to that, and to the locked json_shape:
// the bare document, no envelope, and none of the `# <path>` header line the
// toml rendering prints.

// firstNonSpace returns the first non-whitespace byte of s, or 0 if there is
// none — the cheapest way to prove nothing was printed ahead of the document.
func firstNonSpace(s string) byte {
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune(" \t\r\n", rune(s[i])) {
			return s[i]
		}
	}
	return 0
}

// assertBareJSONDocument is the shared json_shape assertion: the output starts
// with an object, carries no comment header, and parses.
func assertBareJSONDocument(t *testing.T, label, out string) map[string]any {
	t.Helper()
	if got := firstNonSpace(out); got != '{' {
		t.Errorf("%s: first non-space byte is %q, want '{' — something is printed ahead of the document:\n%s", label, got, out)
	}
	for i, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "#") {
			t.Errorf("%s: line %d is a `#` header, which is not JSON: %q", label, i+1, line)
		}
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%s: output does not parse as a JSON object: %v\n%s", label, err, out)
	}
	return doc
}

// dig walks a decoded JSON document by key path, returning "" when any hop is
// absent — so a missing block reports as a value mismatch rather than panicking.
func dig(doc map[string]any, path ...string) string {
	var cur any = doc
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[k]
	}
	s, _ := cur.(string)
	return s
}

// TestProjectShowJSONMatchesTheDocument proves c-5 for `project show`: the
// payload is the same document the toml rendering prints, read through the
// same accessors the CLI already exposes.
func TestProjectShowJSONMatchesTheDocument(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustRunSet(t, "repo.git_main_branch", "trunk")
	mustRunSet(t, "runtime.test_command", "go test ./...")

	var jsonOut, tomlOut string
	jsonOut = captureStdout(t, func() {
		if err := runCmd(t, Project(), "show", "--json"); err != nil {
			t.Fatalf("project show --json: %v", err)
		}
	})
	tomlOut = captureStdout(t, func() {
		if err := runCmd(t, Project(), "show"); err != nil {
			t.Fatalf("project show: %v", err)
		}
	})

	doc := assertBareJSONDocument(t, "project show --json", jsonOut)

	// The toml rendering still carries its header; only --json drops it.
	if !strings.Contains(tomlOut, "# ") {
		t.Errorf("the default rendering lost its `# <path>` header — --json must be the only thing that drops it:\n%s", tomlOut)
	}
	if got := dig(doc, "repo", "git_main_branch"); got != "trunk" {
		t.Errorf("repo.git_main_branch = %q, want trunk", got)
	}
	if !strings.Contains(tomlOut, `git_main_branch = "trunk"`) {
		t.Errorf("the toml rendering disagrees with the payload:\n%s", tomlOut)
	}

	// Cross-check one nested field against the accessor a prompt would
	// otherwise have to shell out to.
	getOut := captureStdout(t, func() {
		if err := runCmd(t, Project(), "get", "runtime.test_command"); err != nil {
			t.Fatalf("project get: %v", err)
		}
	})
	if got, want := dig(doc, "runtime", "test_command"), strings.TrimSpace(getOut); got != want {
		t.Errorf("runtime.test_command: payload %q != `project get` %q", got, want)
	}
}

// TestMilestoneAndDefaultsShowJSONDropTheHeader covers the two remaining
// documents whose default rendering prints a `# <path>` line.
func TestMilestoneAndDefaultsShowJSONDropTheHeader(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "milestones", "v1.1.toml"), `
[milestone]
version = "v1.1"
title = "Friction pass"

[scope]
success_criteria = ["the logs stop repeating themselves"]
`)
	// The table is remote_defaults, not remote — the toml name is what --json
	// must echo, which is the whole point of t-2's tag mirroring.
	mustWrite(t, filepath.Join(home, ".claude", "dross", "defaults.toml"),
		"[remote_defaults]\nprovider = \"github\"\n")

	out := captureStdout(t, func() {
		if err := runCmd(t, Milestone(), "show", "v1.1", "--json"); err != nil {
			t.Fatalf("milestone show --json: %v", err)
		}
	})
	doc := assertBareJSONDocument(t, "milestone show --json", out)
	if got := dig(doc, "milestone", "title"); got != "Friction pass" {
		t.Errorf("milestone.title = %q, want Friction pass", got)
	}

	out = captureStdout(t, func() {
		if err := runCmd(t, Defaults(), "show", "--json"); err != nil {
			t.Fatalf("defaults show --json: %v", err)
		}
	})
	doc = assertBareJSONDocument(t, "defaults show --json", out)
	if got := dig(doc, "remote_defaults", "provider"); got != "github" {
		t.Errorf("remote_defaults.provider = %q, want github (payload keys must be the toml names, not the Go field names)", got)
	}
}

// TestMilestoneShowJSONStillFailsWithoutAVersion pins that --json does not turn
// a missing argument into an empty document. The error is the useful answer;
// `{}` would be a lie a prompt would happily consume.
func TestMilestoneShowJSONStillFailsWithoutAVersion(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// dross init leaves current_milestone empty.
	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Milestone(), "show", "--json")
	})
	if err == nil {
		t.Fatal("milestone show --json with no version and no current_milestone must fail")
	}
	if !strings.Contains(err.Error(), "no version given") {
		t.Errorf("error %q lost the existing wording", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("a failed show printed a document anyway:\n%s", out)
	}
}

// TestProfileShowJSONComposesWithScope proves the two flags are independent:
// --json renders whichever profile --scope selected, not the merged default.
func TestProfileShowJSONComposesWithScope(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(home, ".claude", "dross", "profile.toml"),
		"[dimensions]\n[dimensions.communication]\nrating = \"terse\"\nconfidence = \"high\"\ndirective = \"be brief\"\n")
	mustWrite(t, filepath.Join(dir, ".dross", "profile.toml"),
		"[dimensions]\n[dimensions.communication]\nrating = \"verbose\"\nconfidence = \"high\"\ndirective = \"explain fully\"\n")

	global := captureStdout(t, func() {
		if err := runCmd(t, Profile(), "show", "--scope", "global", "--json"); err != nil {
			t.Fatalf("profile show --scope global --json: %v", err)
		}
	})
	project := captureStdout(t, func() {
		if err := runCmd(t, Profile(), "show", "--scope", "project", "--json"); err != nil {
			t.Fatalf("profile show --scope project --json: %v", err)
		}
	})

	assertBareJSONDocument(t, "profile show --scope global --json", global)
	assertBareJSONDocument(t, "profile show --scope project --json", project)

	if !strings.Contains(global, "terse") {
		t.Errorf("--scope global --json did not emit the global profile:\n%s", global)
	}
	if global == project {
		t.Error("--scope global --json and --scope project --json are identical — --json is ignoring --scope and emitting one fixed profile")
	}
	if !strings.Contains(project, "verbose") {
		t.Errorf("--scope project --json did not emit the project profile:\n%s", project)
	}
}

// TestStackShowJSONStillFailsOnUnknownID pins that the not-found error stays
// ahead of the --json branch: marshalling a nil profile would print the JSON
// literal "null" and exit 0, which parses fine and means nothing.
func TestStackShowJSONStillFailsOnUnknownID(t *testing.T) {
	chdir(t, t.TempDir())
	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, Stack(), "show", "nope", "--json")
	})
	if err == nil {
		t.Fatal("stack show nope --json must fail")
	}
	if !strings.Contains(err.Error(), `stack profile "nope" not found`) {
		t.Errorf("error %q lost the existing wording", err)
	}
	if strings.Contains(out, "null") {
		t.Errorf(`unknown id printed "null" instead of failing:\n%s`, out)
	}
}

// TestStackShowJSONEmitsTheProfile is the positive half — a real embedded
// profile round-trips through --json.
func TestStackShowJSONEmitsTheProfile(t *testing.T) {
	chdir(t, t.TempDir())
	out := captureStdout(t, func() {
		if err := runCmd(t, Stack(), "show", "go", "--json"); err != nil {
			t.Fatalf("stack show go --json: %v", err)
		}
	})
	doc := assertBareJSONDocument(t, "stack show go --json", out)
	if got := dig(doc, "id"); got != "go" {
		t.Errorf("id = %q, want go", got)
	}
}
