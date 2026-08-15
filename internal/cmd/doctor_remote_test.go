package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// doctorRemoteFixture builds a repo doctor can run against, optionally with a
// remote grant and an [mutation] adapter allowlist already written.
//
// The grant is written straight into local.toml rather than through the verb:
// this file is testing what doctor READS, and going through `dross mutation
// remote grant` would make every case here depend on that command's own
// behaviour.
func doctorRemoteFixture(t *testing.T, host, workdir string, adapters []string) {
	t.Helper()
	dir := t.TempDir()
	// A real origin URL so `dross init` captures [remote] from it: an origin
	// with no [remote] is its own doctor finding, and a fixture that starts one
	// issue in the hole cannot show that a healthy remote adds none.
	gitInit(t, dir, "git@github.com:Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	mustRunSet(t, "runtime.test_command", "go test ./...")

	if len(adapters) > 0 {
		path := filepath.Join(dir, ".dross", "project.toml")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		body := string(b) + "\n[mutation]\n  adapters = [\"" + strings.Join(adapters, "\", \"") + "\"]\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if host != "" {
		local := "mutation_remote_host = \"" + host + "\"\nmutation_remote_workdir = \"" + workdir + "\"\n"
		if err := os.WriteFile(filepath.Join(dir, ".dross", LocalFile), []byte(local), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// fakeProbe swaps the readiness seam and returns a pointer to the call count.
func fakeProbe(t *testing.T, fn func(remote.Target, []string) (remote.Readiness, error)) *int {
	t.Helper()
	calls := 0
	orig := remoteProbeFn
	remoteProbeFn = func(tgt remote.Target, tools []string) (remote.Readiness, error) {
		calls++
		return fn(tgt, tools)
	}
	t.Cleanup(func() { remoteProbeFn = orig })
	return &calls
}

// doctorIssues runs doctor and returns its output plus the issue count parsed
// out of the returned error. A nil error means zero issues.
func doctorIssues(t *testing.T, out *string) int {
	t.Helper()
	err := runCmdCapturing(t, out, Doctor())
	if err == nil {
		return 0
	}
	msg := err.Error()
	idx := strings.Index(msg, " project-level issue(s) found")
	if idx < 0 {
		t.Fatalf("doctor failed for a reason other than issue count: %v\n%s", err, *out)
	}
	n := 0
	for _, c := range msg[:idx] {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

// TestDoctorRemoteUngrantedIsAdvisory: most repos have no remote and never
// will. Failing doctor on that would make a clean local clone look broken,
// which is how a diagnostic gets ignored — so the line is printed and the
// issue count does not move.
func TestDoctorRemoteUngrantedIsAdvisory(t *testing.T) {
	doctorRemoteFixture(t, "", "", nil)
	calls := fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		t.Fatal("doctor probed a host with no grant")
		return remote.Readiness{}, nil
	})

	var withoutSection string
	base := doctorIssues(t, &withoutSection)

	if !strings.Contains(withoutSection, "Remote mutation:") {
		t.Fatalf("doctor printed no Remote mutation section:\n%s", withoutSection)
	}
	if !strings.Contains(withoutSection, "no remote granted") {
		t.Errorf("the advisory line is missing:\n%s", withoutSection)
	}
	if !strings.Contains(withoutSection, "dross mutation remote grant") {
		t.Errorf("the advisory does not name the verb that fixes it:\n%s", withoutSection)
	}
	if *calls != 0 {
		t.Errorf("probe called %d times with no grant", *calls)
	}

	// Now grant a healthy remote in the same shape of repo: the issue count
	// must be identical, which is what "unchanged" means here.
	doctorRemoteFixture(t, "helicon", "/srv/dross", nil)
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 32}, nil
	})
	var withSection string
	if got := doctorIssues(t, &withSection); got != base {
		t.Errorf("a healthy remote changed the issue count: %d -> %d\n%s", base, got, withSection)
	}
}

// TestDoctorRemoteTransportFailureIsAnIssue: an unreachable host must move the
// exit code, not just print. The whole value of c-5 is finding this out before
// a verify has pushed a tree and produced a report that is empty for reasons it
// cannot distinguish from "nothing to measure".
func TestDoctorRemoteTransportFailureIsAnIssue(t *testing.T) {
	doctorRemoteFixture(t, "helicon", "/srv/dross", nil)
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{}, remote.Classify("ssh", "helicon", 255)
	})

	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if err == nil {
		t.Fatalf("doctor exited 0 with an unreachable mutation host:\n%s", out)
	}
	if !strings.Contains(out, "helicon") {
		t.Errorf("the finding does not name the host:\n%s", out)
	}
	if !strings.Contains(out, "✗") || !strings.Contains(out, "not usable") {
		t.Errorf("the finding is not reported as a failure:\n%s", out)
	}
}

// TestDoctorRemoteMissingToolNamesTheAdapter covers the case a user can act on
// only if the message says which toolchain to install.
//
// The second half is the one that matters more: a repo whose [mutation]
// adapters list excludes gremlins must not be probed for it at all. A Go-less
// project failing doctor because the mutation host has no gremlins is a finding
// with no correct action.
func TestDoctorRemoteMissingToolNamesTheAdapter(t *testing.T) {
	doctorRemoteFixture(t, "helicon", "/srv/dross", nil)
	var asked []string
	fakeProbe(t, func(_ remote.Target, tools []string) (remote.Readiness, error) {
		asked = append([]string(nil), tools...)
		return remote.Readiness{Cores: 8, Missing: []string{"gremlins"}}, nil
	})

	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if err == nil {
		t.Fatalf("a missing remote toolchain did not move the exit code:\n%s", out)
	}
	if !strings.Contains(out, "gremlins is not installed on helicon") {
		t.Errorf("the finding does not name the missing binary and the host:\n%s", out)
	}
	if !strings.Contains(out, "the gremlins adapter needs it there") {
		t.Errorf("the finding does not name the adapter that wanted it:\n%s", out)
	}
	// Every adapter runs when the allowlist is empty, so all three toolchains
	// are asked about.
	for _, want := range []string{"npx", "gremlins", "dotnet"} {
		if !contains(asked, want) {
			t.Errorf("probe was not asked about %q: %v", want, asked)
		}
	}

	// Same missing binary, an allowlist that excludes its adapter: no finding.
	doctorRemoteFixture(t, "helicon", "/srv/dross", []string{"stryker"})
	asked = nil
	fakeProbe(t, func(_ remote.Target, tools []string) (remote.Readiness, error) {
		asked = append([]string(nil), tools...)
		// The host still lacks gremlins; nobody asked, so nobody reports it.
		var missing []string
		for _, tool := range tools {
			if tool == "gremlins" {
				missing = append(missing, tool)
			}
		}
		return remote.Readiness{Cores: 8, Missing: missing}, nil
	})
	var scoped string
	if err := runCmdCapturing(t, &scoped, Doctor()); err != nil {
		t.Fatalf("a stryker-only repo failed doctor over gremlins: %v\n%s", err, scoped)
	}
	if contains(asked, "gremlins") {
		t.Errorf("a stryker-only repo probed for gremlins: %v", asked)
	}
	if strings.Contains(scoped, "gremlins is not installed") {
		t.Errorf("a stryker-only repo reported a gremlins finding:\n%s", scoped)
	}
}

// TestDoctorRemoteHealthyPrintsTheProbedCoreCount.
//
// The printed number must be the PROBE's, not a local derivation: it is the
// number the worker default will use, and a doctor that showed this laptop's
// core count while the run sized itself off a 32-core host would be reporting a
// machine nobody is running on.
func TestDoctorRemoteHealthyPrintsTheProbedCoreCount(t *testing.T) {
	doctorRemoteFixture(t, "helicon", "/srv/dross", nil)
	calls := fakeProbe(t, func(tgt remote.Target, _ []string) (remote.Readiness, error) {
		if tgt.Host != "helicon" || tgt.Workdir != "/srv/dross" {
			t.Errorf("probe got %+v, want the granted target", tgt)
		}
		return remote.Readiness{Cores: 32}, nil
	})

	var out string
	if err := runCmdCapturing(t, &out, Doctor()); err != nil {
		t.Fatalf("a healthy remote failed doctor: %v\n%s", err, out)
	}
	for _, want := range []string{"helicon", "/srv/dross", "32 cores"} {
		if !strings.Contains(out, want) {
			t.Errorf("the healthy line does not mention %q:\n%s", want, out)
		}
	}
	// Exactly one call. Doctor probing twice, or probing through a second
	// path of its own, is how it ends up passing on a check the run performs
	// differently.
	if *calls != 1 {
		t.Errorf("probe called %d times, want exactly 1", *calls)
	}
}

// TestDoctorRemoteSurfacesTheTrackedRefusal: a tracked local.toml is refused
// unread, and doctor reports that rather than the host the file names.
func TestDoctorRemoteSurfacesTheTrackedRefusal(t *testing.T) {
	doctorRemoteFixture(t, "attacker.example", "/srv/x", nil)
	mustGit(t, ".", "add", "-f", ".dross/"+LocalFile)
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		t.Fatal("doctor probed a host read from a tracked local.toml")
		return remote.Readiness{}, nil
	})

	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if err == nil {
		t.Fatalf("a tracked local.toml did not move doctor's exit code:\n%s", out)
	}
	if !strings.Contains(out, "refusing to read") {
		t.Errorf("doctor does not surface the refusal:\n%s", out)
	}
}
