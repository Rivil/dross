package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRemoteStatusUngrantedExitsZero: most repos have no remote, and that is
// not a failure. `status` is the command a user reaches for to find out where
// they stand, so it must work — and exit 0 — in exactly the state they are
// most often in.
func TestRemoteStatusUngrantedExitsZero(t *testing.T) {
	chdirDross(t)

	var out string
	if err := runCmdCapturing(t, &out, Mutation(), "remote", "status"); err != nil {
		t.Fatalf("`mutation remote status` on an ungranted repo must exit 0: %v", err)
	}
	if !strings.Contains(out, "not granted") {
		t.Errorf("status does not say \"not granted\":\n%s", out)
	}
}

// TestRemoteStatusSurfacesTheTrackedRefusal: a tracked local.toml is refused
// UNREAD, and status must report that refusal rather than the host the file
// happens to name.
//
// Printing the stored host would be the worst of both worlds — the user would
// read it as the authorized remote when dross will not act on it, and a hostile
// repo would have got its chosen hostname onto the user's screen as if dross
// had accepted it.
func TestRemoteStatusSurfacesTheTrackedRefusal(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompleteRoot(t, root)

	body := "mutation_remote_host = \"attacker.example\"\nmutation_remote_workdir = \"/srv/x\"\n"
	if err := os.WriteFile(filepath.Join(root, LocalFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-f", ".dross/"+LocalFile)
	chdir(t, dir)

	var out string
	err := runCmdCapturing(t, &out, Mutation(), "remote", "status")
	if err == nil {
		t.Fatal("status read a tracked local.toml rather than refusing it")
	}
	for _, want := range []string{"refusing to read", ".dross/" + LocalFile, "tracked"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "attacker.example") || strings.Contains(out, "attacker.example") {
		t.Errorf("the tracked host reached the user's screen:\nerr=%v\nout=%s", err, out)
	}
	if strings.Contains(out, "granted") && !strings.Contains(out, "not granted") {
		t.Errorf("status reported a grant from a refused file:\n%s", out)
	}
}

// TestDocsCoverRemoteMutationKeys is the documentation half of this task, in
// the shape TestDocsCoverAllowHosts established: an escape hatch nobody can
// find is not an escape hatch.
//
// It names the SETTABLE keys deliberately. mutation_remote_host is written by
// the grant verb, but mutation_workers, mutation_test_cpu and
// mutation_remote_env are keys a user types into `dross local set` — an
// undocumented one is a knob that only exists for whoever read the source.
func TestDocsCoverRemoteMutationKeys(t *testing.T) {
	root := repoRootForDocs(t)
	want := []string{
		"mutation remote grant",
		"mutation_remote_host",
		"mutation_workers",
		"mutation_test_cpu",
		"mutation_remote_env",
		".dross/local.toml",
	}
	for _, file := range []string{"README.md", "docs/dross.1"} {
		b, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, w := range want {
			if !strings.Contains(string(b), w) {
				t.Errorf("%s does not mention %q", file, w)
			}
		}
	}
}

// TestDocsSayRemoteEnvHoldsNames pins the one sentence in these docs whose
// absence is actively dangerous rather than merely unhelpful.
//
// mutation_remote_env is an allowlist of variable NAMES; dross reads each value
// from its own process environment at run time and stores none of them. A user
// who read the key as name=value pairs would write their DATABASE_URL into
// .dross/local.toml — turning a store that deliberately holds no secret into
// one that holds theirs, in a file whose whole safety argument is that dross
// never had to protect anything in it.
func TestDocsSayRemoteEnvHoldsNames(t *testing.T) {
	root := repoRootForDocs(t)
	want := []string{
		"variable NAMES",
		"reads their values from the surrounding environment",
		"never name=value pairs",
	}
	for _, file := range []string{"README.md", "docs/dross.1"} {
		b, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, w := range want {
			if !strings.Contains(string(b), w) {
				t.Errorf("%s does not say %q — a reader who takes mutation_remote_env for name=value pairs puts a secret in a file dross never intended to hold one", file, w)
			}
		}
	}
}
