package mutationcfg

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// lookPathPresent answers the PATH lookup from a fixed set.
func lookPathPresent(present map[string]bool) func(string) (string, error) {
	return func(tool string) (string, error) {
		if present[tool] {
			return "/usr/bin/" + tool, nil
		}
		return "", errors.New("not found")
	}
}

// TestToolsFollowRosterOrder: the tool list is stable run to run (doctor's
// output is diffed), scoped to the allowlist, and names the adapter that
// needs each tool.
func TestToolsFollowRosterOrder(t *testing.T) {
	p := &project.Project{}
	tools, needBy := Tools(p)
	if want := []string{"npx", "gremlins", "dotnet"}; !reflect.DeepEqual(tools, want) {
		t.Errorf("Tools(all) = %v, want %v", tools, want)
	}
	if needBy["npx"] != "stryker" || needBy["gremlins"] != "gremlins" || needBy["dotnet"] != "stryker-net" {
		t.Errorf("needBy = %v", needBy)
	}

	p.Mutation.Adapters = []string{"stryker-net", "gremlins"} // declared out of roster order
	tools, needBy = Tools(p)
	if want := []string{"gremlins", "dotnet"}; !reflect.DeepEqual(tools, want) {
		t.Errorf("Tools(allowlist) = %v, want roster order %v", tools, want)
	}
	if _, leaked := needBy["npx"]; leaked {
		t.Error("an adapter outside the allowlist contributed a tool")
	}
}

// TestMissingNamesGapAndFix: every gap carries the tool, the adapter that
// wanted it, what goes unmeasured, and the install line — and only the
// configured adapters are probed.
func TestMissingNamesGapAndFix(t *testing.T) {
	p := &project.Project{}
	gaps := Missing(p, lookPathPresent(map[string]bool{"gremlins": true}))
	if len(gaps) != 2 {
		t.Fatalf("want npx and dotnet missing, got %+v", gaps)
	}
	if gaps[0].Tool != "npx" || gaps[0].Adapter != "stryker" || gaps[0].Language != "TypeScript/JavaScript/Svelte" || gaps[0].Install == "" {
		t.Errorf("first gap = %+v", gaps[0])
	}
	if gaps[1].Tool != "dotnet" || gaps[1].Adapter != "stryker-net" || gaps[1].Language != "C#" || gaps[1].Install == "" {
		t.Errorf("second gap = %+v", gaps[1])
	}

	p.Mutation.Adapters = []string{"gremlins"}
	if gaps := Missing(p, lookPathPresent(nil)); len(gaps) != 1 || gaps[0].Tool != "gremlins" || gaps[0].Language != "Go" {
		t.Errorf("Go-only repo: gaps = %+v, want gremlins alone", gaps)
	}
	if gaps := Missing(p, lookPathPresent(map[string]bool{"gremlins": true})); len(gaps) != 0 {
		t.Errorf("every tool present still reported %+v", gaps)
	}
}

// TestLookPathDefaultsToExec: the seam starts as the real lookup, so a
// production binary that never rebinds it probes the real PATH.
func TestLookPathDefaultsToExec(t *testing.T) {
	want, wantErr := exec.LookPath("go")
	got, err := LookPath("go")
	if got != want || (err == nil) != (wantErr == nil) {
		t.Errorf("LookPath does not default to exec.LookPath: got (%q,%v) want (%q,%v)", got, err, want, wantErr)
	}
}
