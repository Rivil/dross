package diag

import (
	"errors"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// TestMutationToolchainNamesTheAdapter: a missing tool yields one Warn naming
// the tool, the adapter and the language, followed by a Note carrying the
// install hint; every tool present yields no lines at all.
func TestMutationToolchainNamesTheAdapter(t *testing.T) {
	p := &project.Project{}
	p.Mutation.Adapters = []string{"gremlins"}
	absent := func(string) (string, error) { return "", errors.New("not found") }
	present := func(tool string) (string, error) { return "/usr/bin/" + tool, nil }

	var lines []Line
	silent(t, func() { lines = MutationToolchain(p, absent) })
	if len(lines) != 2 || lines[0].Level != Warn || lines[1].Level != Note {
		t.Fatalf("one missing tool = %+v", lines)
	}
	for _, want := range []string{"gremlins is not installed", "the gremlins adapter", "Go files"} {
		if !strings.Contains(lines[0].Text, want) {
			t.Errorf("warning lacks %q: %q", want, lines[0].Text)
		}
	}
	if !strings.Contains(lines[1].Text, "go install github.com/go-gremlins/gremlins") || !strings.HasPrefix(lines[1].Text, "    ") {
		t.Errorf("install note = %q", lines[1].Text)
	}
	if got := MutationToolchain(p, present); len(got) != 0 {
		t.Errorf("every tool present still reported %+v", got)
	}
	// Every adapter absent: three warnings, roster order, no Issue anywhere.
	all := MutationToolchain(&project.Project{}, absent)
	if len(all) != 6 || !strings.HasPrefix(all[0].Text, "npx ") || !strings.HasPrefix(all[2].Text, "gremlins ") || !strings.HasPrefix(all[4].Text, "dotnet ") {
		t.Errorf("all-absent = %+v", all)
	}
	if CountIssues(all) != 0 {
		t.Error("the toolchain check is advisory and must never raise an Issue")
	}
}
