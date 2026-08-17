package cmd

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// fakeLookPath replaces the PATH lookup so both arms are drivable without
// depending on what the developer happens to have installed — which is the same
// reason the check exists at all.
func fakeLookPath(t *testing.T, present map[string]bool) {
	t.Helper()
	orig := execLookPath
	t.Cleanup(func() { execLookPath = orig })
	execLookPath = func(tool string) (string, error) {
		if present[tool] {
			return "/usr/bin/" + tool, nil
		}
		return "", errors.New("not found")
	}
}

// toolchainFixture is a repo whose configured mutation adapters are exactly
// those named.
func toolchainFixture(t *testing.T, adapters ...string) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/example/repo.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")
	if len(adapters) > 0 {
		mustWriteAdapters(t, dir, adapters)
	}
	return dir
}

func mustWriteAdapters(t *testing.T, dir string, adapters []string) {
	t.Helper()
	path := dir + "/.dross/project.toml"
	body := mustRead(t, path)
	quoted := make([]string, 0, len(adapters))
	for _, a := range adapters {
		quoted = append(quoted, `"`+a+`"`)
	}
	body += "\n[mutation]\n  adapters = [" + strings.Join(quoted, ", ") + "]\n"
	mustWrite(t, path, body)
}

// TestDoctorReportsMissingMutationToolchain: a stack whose adapter needs Node
// must be told BEFORE a verify run comes back having measured nothing. An empty
// measurement does not announce itself — the phase scores over zero mutants and
// no line says why.
func TestDoctorReportsMissingMutationToolchain(t *testing.T) {
	toolchainFixture(t, "stryker")
	fakeLookPath(t, map[string]bool{}) // nothing installed

	var out string
	_ = runCmdCapturing(t, &out, Doctor())

	if !strings.Contains(out, "Mutation toolchain:") {
		t.Fatalf("doctor printed no mutation-toolchain section:\n%s", out)
	}
	if !strings.Contains(out, "npx") {
		t.Errorf("the warning does not name the missing tool:\n%s", out)
	}
	// Naming the gap without naming the fix sends the reader searching.
	if !strings.Contains(out, "nodejs.org") {
		t.Errorf("the warning does not say how to get it:\n%s", out)
	}
	// And what it costs, not merely what is absent.
	if !strings.Contains(out, "TypeScript") {
		t.Errorf("the warning does not say what goes unmeasured:\n%s", out)
	}
}

// TestMissingToolchainIsAdvisory: failing a clone for lacking a toolchain it
// may never need is how a check gets ignored, and a check people ignore
// protects nothing.
func TestMissingToolchainIsAdvisory(t *testing.T) {
	toolchainFixture(t, "stryker")

	var withAll string
	fakeLookPath(t, map[string]bool{"npx": true, "gremlins": true, "dotnet": true})
	errAll := runCmdCapturing(t, &withAll, Doctor())

	var withNone string
	fakeLookPath(t, map[string]bool{})
	errNone := runCmdCapturing(t, &withNone, Doctor())

	// The presence or absence of the toolchain must not change the verdict.
	if (errAll == nil) != (errNone == nil) {
		t.Errorf("a missing toolchain changed doctor's exit status: with=%v without=%v", errAll, errNone)
	}
	if !strings.Contains(withNone, "⚠") {
		t.Errorf("the missing toolchain was not reported at all:\n%s", withNone)
	}
	if strings.Contains(withNone, "✗ npx") {
		t.Errorf("the missing toolchain is reported as an issue, not an advisory:\n%s", withNone)
	}
}

// TestGoOnlyProjectGetsNoNodeAdvisory: a warning about a toolchain the project
// never needed is noise, and noise trains the reader to skim past the warnings
// that matter.
func TestGoOnlyProjectGetsNoNodeAdvisory(t *testing.T) {
	toolchainFixture(t, "gremlins")
	fakeLookPath(t, map[string]bool{"gremlins": true}) // no npx, no dotnet

	var out string
	_ = runCmdCapturing(t, &out, Doctor())

	if strings.Contains(out, "npx") {
		t.Errorf("a gremlins-only project was warned about Node:\n%s", out)
	}
	if strings.Contains(out, "dotnet") {
		t.Errorf("a gremlins-only project was warned about the .NET SDK:\n%s", out)
	}
	if strings.Contains(out, "Mutation toolchain:") {
		t.Errorf("the section printed with nothing to say:\n%s", out)
	}
}

// TestPresentToolchainIsSilent is the other half of the same rule: doctor's
// output is read by someone looking for problems, so a tool that IS installed
// gets no line.
func TestPresentToolchainIsSilent(t *testing.T) {
	toolchainFixture(t, "stryker", "gremlins")
	fakeLookPath(t, map[string]bool{"npx": true, "gremlins": true})

	var out string
	_ = runCmdCapturing(t, &out, Doctor())
	if strings.Contains(out, "Mutation toolchain:") {
		t.Errorf("doctor reported a toolchain gap when everything was installed:\n%s", out)
	}
}

// TestLookPathSeamDefaultsToRealPATH guards the seam itself: a production
// default that pointed at a stub would make every assertion above meaningless.
func TestLookPathSeamDefaultsToRealPATH(t *testing.T) {
	got, err := execLookPath("go")
	want, wantErr := exec.LookPath("go")
	if (err == nil) != (wantErr == nil) || got != want {
		t.Errorf("execLookPath does not default to exec.LookPath: got (%q,%v) want (%q,%v)", got, err, want, wantErr)
	}
}
