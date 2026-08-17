package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// execRecorder replaces the install seam and records every argv it was handed,
// in order. Whether anything was run at all is half of what these tests assert:
// a dry run that "reported" correctly while installing would be the worst
// possible pass.
type execRecorder struct {
	argvs [][]string
	err   error
}

func (r *execRecorder) install(t *testing.T) {
	t.Helper()
	orig := remoteExecFn
	remoteExecFn = func(_ remote.Target, argv []string) (string, error) {
		r.argvs = append(r.argvs, append([]string(nil), argv...))
		return "", r.err
	}
	t.Cleanup(func() { remoteExecFn = orig })
}

// bootstrapFixture is a repo with a remote granted and an adapter allowlist, so
// the tool set under test is exactly the one named.
func bootstrapFixture(t *testing.T, adapters ...string) string {
	t.Helper()
	doctorRemoteFixture(t, "helicon", "/srv/dross", adapters)
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func runBootstrap(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Remote(), append([]string{"bootstrap"}, args...)...) })
	return out, err
}

// TestBootstrapDryRunInstallsNothing: c-2. The default has to be the safe one —
// the command's whole job is to modify a machine that is not this one, and a
// verb that installed on sight makes "show me what you would do" unaskable.
func TestBootstrapDryRunInstallsNothing(t *testing.T) {
	bootstrapFixture(t, "gremlins")
	probeMissing(t, "gremlins") // go present, gremlins absent
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t)
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}

	for _, want := range []string{"gremlins", "go install", "helicon"} {
		if !strings.Contains(out, want) {
			t.Errorf("the dry run does not name %q:\n%s", want, out)
		}
	}
	if len(rec.argvs) != 0 {
		t.Errorf("the dry run ran %d command(s) on the host: %v", len(rec.argvs), rec.argvs)
	}
}

// TestBootstrapDryRunClosingMessage: a dry run that found only installable work
// ends by offering the remedy that would do it.
//
// The two closing messages are not decoration — they are the difference between
// "you can fix this yourself with one flag" and "this needs whoever owns the
// host". A run that printed the wrong one would send the reader to --apply for a
// gap --apply cannot close.
func TestBootstrapDryRunClosingMessage(t *testing.T) {
	bootstrapFixture(t, "gremlins")
	probeMissing(t, "gremlins") // installable: go is present
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t)
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "--apply") {
		t.Errorf("a dry run with installable work does not offer --apply:\n%s", out)
	}
	if strings.Contains(out, "host's owner") {
		t.Errorf("a dry run with no refusals blamed the host's owner:\n%s", out)
	}
}

// TestBootstrapDryRunWithRefusalClosingMessage: the other arm. --apply cannot
// install a language runtime, so a run whose gap is a runtime must not offer it
// as the remedy.
func TestBootstrapDryRunWithRefusalClosingMessage(t *testing.T) {
	bootstrapFixture(t, "gremlins")
	probeMissing(t, "gremlins", "go") // the runtime itself is missing
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t)
	if err == nil {
		t.Fatalf("a dry run carrying a refusal exited 0:\n%s", out)
	}
	if !strings.Contains(out, "host's owner") {
		t.Errorf("a refusal-carrying dry run does not name who has to act:\n%s", out)
	}
	if strings.Contains(out, "Re-run with --apply") {
		t.Errorf("a missing runtime was offered --apply as the fix, which cannot work:\n%s", out)
	}
	if len(rec.argvs) != 0 {
		t.Errorf("a dry run reached the host: %v", rec.argvs)
	}
}

// TestBootstrapApplyInstalls: c-1. --apply runs exactly what the dry run
// promised, in order — a verb whose preview and action differ is worse than one
// with no preview.
func TestBootstrapApplyInstalls(t *testing.T) {
	bootstrapFixture(t, "gremlins")
	probeMissing(t, "gremlins")
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t, "--apply")
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}

	if len(rec.argvs) != 1 {
		t.Fatalf("ran %d command(s), want exactly 1: %v", len(rec.argvs), rec.argvs)
	}
	got := rec.argvs[0]
	if got[0] != "go" || got[1] != "install" {
		t.Errorf("ran %v, want a `go install`", got)
	}
	if strings.HasSuffix(got[len(got)-1], "@latest") {
		t.Errorf("installed an unpinned spec: %v", got)
	}
	if !strings.Contains(out, "1 installed") {
		t.Errorf("the run does not report what it installed:\n%s", out)
	}
}

// TestBootstrapClearsDoctor is c-1 end to end: after the install the thing that
// reported the gap stops reporting it. The probe is what both surfaces read, so
// a host that now has the tool must satisfy doctor without anything else
// changing.
func TestBootstrapClearsDoctor(t *testing.T) {
	root := bootstrapFixture(t, "gremlins")
	repoDir := filepath.Dir(root)

	// Before: gremlins absent, and doctor says so.
	restore := probeMissing(t, "gremlins")
	_ = restore
	var before string
	beforeIssues := captureRemoteDoctor(t, root, repoDir, &before)
	if beforeIssues == 0 || !strings.Contains(before, "gremlins") {
		t.Fatalf("doctor reports no missing gremlins to fix:\n%s", before)
	}

	rec := &execRecorder{}
	rec.install(t)
	if out, err := runBootstrap(t, "--apply"); err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if len(rec.argvs) == 0 {
		t.Fatal("nothing was installed, so this proves nothing")
	}

	// After: the host has it, and doctor's Remote section is clean.
	probeMissing(t)
	var after string
	if n := captureRemoteDoctor(t, root, repoDir, &after); n != 0 {
		t.Errorf("doctor still reports %d remote issue(s) after the install:\n%s", n, after)
	}
}

// captureRemoteDoctor runs just the Remote section and returns its issue count.
func captureRemoteDoctor(t *testing.T, root, repoDir string, out *string) int {
	t.Helper()
	p := loadWiringProject(t, root)
	n := 0
	*out = captureStdout(t, func() { n = checkRemoteMutation(root, repoDir, p) })
	return n
}

// TestBootstrapRefusesUngranted: with no host there is nothing to provision,
// and the error has to name the grant — the user's next move is that command.
func TestBootstrapRefusesUngranted(t *testing.T) {
	doctorRemoteFixture(t, "", "", []string{"gremlins"})
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t, "--apply")
	if err == nil {
		t.Fatalf("bootstrap ran with no grant:\n%s", out)
	}
	if !strings.Contains(err.Error(), "dross remote grant") {
		t.Errorf("the refusal does not name the grant command: %v", err)
	}
	if len(rec.argvs) != 0 {
		t.Errorf("an ungranted bootstrap reached the host: %v", rec.argvs)
	}
}

// TestBootstrapReportsRefusalDistinctly: c-3. A refusal is not an install
// failure — the remedies differ and only one of them is dross's to perform —
// but the run still exits non-zero so the gap is not lost.
func TestBootstrapReportsRefusalDistinctly(t *testing.T) {
	bootstrapFixture(t, "gremlins")
	probeMissing(t, "gremlins", "go") // the runtime itself is absent
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t, "--apply")
	if err == nil {
		t.Fatalf("a refused host exited 0:\n%s", out)
	}
	if len(rec.argvs) != 0 {
		t.Errorf("bootstrap installed something despite refusing: %v", rec.argvs)
	}
	if !strings.Contains(out, "refused") {
		t.Errorf("the output does not report a refusal:\n%s", out)
	}
	if !strings.Contains(out, "go") {
		t.Errorf("the refusal does not name the runtime the host needs:\n%s", out)
	}
	if strings.Contains(out, "1 installed") {
		t.Errorf("a refusal was counted as an install:\n%s", out)
	}
}

// TestBootstrapSkipsProvisionedHost: re-running must be a no-op. That property
// is what makes --apply safe to repeat from a script.
func TestBootstrapSkipsProvisionedHost(t *testing.T) {
	bootstrapFixture(t, "gremlins")
	probeMissing(t) // nothing missing
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t, "--apply")
	if err != nil {
		t.Fatalf("a provisioned host errored: %v\n%s", err, out)
	}
	if len(rec.argvs) != 0 {
		t.Errorf("a provisioned host was reinstalled: %v", rec.argvs)
	}
	if !strings.Contains(out, "already installed") {
		t.Errorf("the output does not say the tool was already there:\n%s", out)
	}
}

// TestBootstrapContinuesPastFailure: independent tools. Stopping at the first
// failure leaves a host half-provisioned for reasons that had nothing to do
// with the tools never attempted.
func TestBootstrapContinuesPastFailure(t *testing.T) {
	bootstrapFixture(t, "gremlins", "stryker", "stryker-net")
	probeMissing(t, "gremlins", "npx", "dotnet")
	rec := &execRecorder{err: errors.New("install exploded")}
	rec.install(t)

	out, err := runBootstrap(t, "--apply")
	if err == nil {
		t.Fatalf("a run containing a failure exited 0:\n%s", out)
	}
	// gremlins is the only installable one; npx and dotnet are refused
	// runtimes. Every step must still be reported.
	for _, tool := range []string{"gremlins", "npx", "dotnet"} {
		if !strings.Contains(out, tool) {
			t.Errorf("%s was never reported:\n%s", tool, out)
		}
	}
	if !strings.Contains(out, "refused") {
		t.Errorf("the runtime refusals were not reported:\n%s", out)
	}
}

// TestBootstrapTransportFailureIsNotAPlan: an unreachable host must not produce
// a plan proposing to install a whole toolchain onto a machine that is down.
func TestBootstrapTransportFailureIsNotAPlan(t *testing.T) {
	bootstrapFixture(t, "gremlins")
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{}, remote.Classify("ssh", "helicon", 255)
	})
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t, "--apply")
	if err == nil {
		t.Fatalf("an unreachable host produced a bootstrap:\n%s", out)
	}
	if len(rec.argvs) != 0 {
		t.Errorf("an unreachable host was installed onto: %v", rec.argvs)
	}
}

// TestDocsCoverRemoteBootstrap: a verb the docs do not mention is one nobody
// finds, and the README table is the surface a reader scans first.
func TestDocsCoverRemoteBootstrap(t *testing.T) {
	var found bool
	for _, sub := range Remote().Commands() {
		if sub.Name() == "bootstrap" {
			found = true
		}
	}
	if !found {
		t.Error("`bootstrap` does not resolve against the real Remote() tree")
	}

	root := repoRootForDocs(t)
	for _, doc := range []string{"README.md", filepath.Join("docs", "dross.1")} {
		b, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatal(err)
		}
		body := string(b)
		if !strings.Contains(body, "remote bootstrap") {
			t.Errorf("%s does not document `dross remote bootstrap`", doc)
		}
		if !strings.Contains(body, "--apply") {
			t.Errorf("%s does not document the --apply flag", doc)
		}
	}
}
