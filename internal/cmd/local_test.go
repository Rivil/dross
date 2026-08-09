package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLocalSetGetRoundTrips is the store's basic contract: what `local set`
// writes, `local get` reads back on the next process.
func TestLocalSetGetRoundTrips(t *testing.T) {
	chdirDross(t)

	if err := runCmd(t, Local(), "set", "quick_base", "main"); err != nil {
		t.Fatalf("local set: %v", err)
	}
	var out string
	if err := runCmdCapturing(t, &out, Local(), "get", "quick_base"); err != nil {
		t.Fatalf("local get: %v", err)
	}
	if strings.TrimSpace(out) != "main" {
		t.Errorf("quick_base did not round-trip: got %q want %q", strings.TrimSpace(out), "main")
	}
}

// TestLocalStoreIsUntracked is the property the whole store exists for: the
// recorded base must never enter cumulative history. state.json rides the
// squash onto the base, so a value kept there is inherited by every later
// tree — the drag-forward this milestone is removing. A tracked local.toml
// would reintroduce it.
//
// Behavioural check via `git check-ignore` (the idiom the security/quality/
// techdebt artifact guards use): it catches a wrong pattern that a string
// match on the .gitignore line would miss. Exit 0 = ignored, 1 = not.
func TestLocalStoreIsUntracked(t *testing.T) {
	root := repoRootFromTest(t)
	if err := exec.Command("git", "-C", root, "check-ignore", ".dross/local.toml").Run(); err != nil {
		t.Fatalf("git check-ignore reports .dross/local.toml is NOT ignored (err=%v); "+
			"the machine-local store must stay out of cumulative history, or a stale "+
			"quick_base gets dragged forward onto every later tree", err)
	}
}

// TestLocalSetCreatesStoreOnDemand pins create-on-write: the file is
// gitignored, so a fresh clone has none and the first writer must make it.
func TestLocalSetCreatesStoreOnDemand(t *testing.T) {
	root := chdirDross(t)

	path := filepath.Join(root, "local.toml")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("fixture should start with no local.toml, stat err = %v", err)
	}
	if err := runCmd(t, Local(), "set", "quick_base", "milestone/v1.2"); err != nil {
		t.Fatalf("local set on a root with no local.toml: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("local.toml not created: %v", err)
	}
	if !strings.Contains(string(b), "milestone/v1.2") {
		t.Errorf("local.toml missing the written value: %s", b)
	}
}

// TestLocalGetUnsetKeyIsEmptyAndClean pins the unset case: callers branch on
// empty output, so "no value recorded" must exit 0 with nothing printed
// rather than erroring.
func TestLocalGetUnsetKeyIsEmptyAndClean(t *testing.T) {
	chdirDross(t)

	var out string
	if err := runCmdCapturing(t, &out, Local(), "get", "quick_base"); err != nil {
		t.Fatalf("get on an unset key should succeed, got %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("unset key should print nothing, got %q", out)
	}
}

// TestLocalRejectsUnknownKey keeps the key set closed — a typo'd key must not
// become a silently-written entry no reader ever looks for.
func TestLocalRejectsUnknownKey(t *testing.T) {
	root := chdirDross(t)

	for _, args := range [][]string{
		{"set", "quik_base", "main"},
		{"get", "quik_base"},
	} {
		err := runCmd(t, Local(), args...)
		if err == nil {
			t.Fatalf("expected an error for `local %s` on an unknown key", strings.Join(args, " "))
		}
		if !strings.Contains(err.Error(), "quick_base") {
			t.Errorf("error should name the valid keys: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "local.toml")); !os.IsNotExist(err) {
		t.Errorf("a rejected key must not create the store, stat err = %v", err)
	}
}

// TestReadLocalKeyIsBestEffort pins the reader other commands use: the store
// is a reconciliation hint, never a gate, so a missing store or an unknown key
// yields "" instead of failing the command that asked.
func TestReadLocalKeyIsBestEffort(t *testing.T) {
	root := chdirDross(t)

	if got := readLocalKey(root, "quick_base"); got != "" {
		t.Errorf("missing store should read empty, got %q", got)
	}
	if err := runCmd(t, Local(), "set", "quick_base", "main"); err != nil {
		t.Fatalf("local set: %v", err)
	}
	if got := readLocalKey(root, "quick_base"); got != "main" {
		t.Errorf("readLocalKey: got %q want %q", got, "main")
	}
	if got := readLocalKey(root, "nope"); got != "" {
		t.Errorf("unknown key should read empty, got %q", got)
	}
}

// TestReadAllowHostsRefusesTrackedLocal is the half of c-7 that holds for
// repos already onboarded. init and onboard never run again, so an existing
// repo gains the .gitignore line only when someone acts on doctor's finding —
// which means the seeded ignore rule cannot be what carries the guarantee.
//
// This is what carries it: local.toml is the one input the derived host
// allowlist trusts, precisely because it is machine-local and never cloned. A
// tracked copy breaks that assumption, so it is refused UNREAD. Parsing it and
// dropping only allow_hosts would still let a cloned quick_base ride history —
// the thing this store was created to stop.
func TestReadAllowHostsRefusesTrackedLocal(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompleteRoot(t, root)

	// The hostile shape: a committed local.toml naming the very host the repo's
	// api_base points at.
	local := filepath.Join(root, LocalFile)
	if err := os.WriteFile(local, []byte("allow_hosts = \"attacker.example\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-f", ".dross/"+LocalFile)

	hosts, err := readAllowHosts(root, dir)
	if err == nil {
		t.Fatal("a tracked local.toml was read rather than refused")
	}
	if hosts != nil {
		t.Errorf("hosts must be nil on refusal, got %v", hosts)
	}
	msg := err.Error()
	for _, want := range []string{"refusing to read", ".dross/" + LocalFile, "tracked", "git rm --cached"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal does not mention %q: %v", want, err)
		}
	}
	if strings.Contains(msg, "attacker.example") {
		t.Errorf("the refusal echoed the file's contents — it must not be parsed: %v", err)
	}

	// Untracked, the same file is honoured: the refusal is about provenance,
	// not about the value.
	mustGit(t, dir, "rm", "--cached", "-q", ".dross/"+LocalFile)
	hosts, err = readAllowHosts(root, dir)
	if err != nil {
		t.Fatalf("an untracked local.toml must be readable: %v", err)
	}
	if len(hosts) != 1 || hosts[0] != "attacker.example" {
		t.Errorf("allow_hosts did not parse: %v", hosts)
	}
}

// TestReadAllowHostsMissingFileIsEmpty: the store is optional. A fresh clone
// has none, and that must read as "no additions", not as an error that blocks
// every forge call.
func TestReadAllowHostsMissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompleteRoot(t, root)

	hosts, err := readAllowHosts(root, dir)
	if err != nil {
		t.Fatalf("a missing local.toml must not error: %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("want no hosts, got %v", hosts)
	}
}

// TestAllowHostsSplitsAndTrims covers the comma-separated form `dross local set
// allow_hosts` writes — doctor names that exact command, so a value with the
// spaces a human would type must work.
func TestAllowHostsSplitsAndTrims(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompleteRoot(t, root)
	chdir(t, dir)

	if err := runCmd(t, Local(), "set", "allow_hosts", " git.corp.internal , odd.example:8443 ,"); err != nil {
		t.Fatalf("local set allow_hosts: %v", err)
	}
	hosts, err := readAllowHosts(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"git.corp.internal", "odd.example:8443"}
	if len(hosts) != len(want) {
		t.Fatalf("hosts = %v, want %v", hosts, want)
	}
	for i := range want {
		if hosts[i] != want[i] {
			t.Errorf("hosts[%d] = %q, want %q", i, hosts[i], want[i])
		}
	}
}

// TestDocsCoverAllowHosts is the only gate on the documentation half of this
// change. A machine-local escape hatch nobody can find is not an escape hatch —
// the refusal path assumes the user can look up what allow_hosts is and where
// it lives, and neither README nor the man page said so before.
func TestDocsCoverAllowHosts(t *testing.T) {
	root := repoRootForDocs(t)
	for _, tc := range []struct {
		file string
		want []string
	}{
		{"README.md", []string{"allow_hosts", ".dross/local.toml"}},
		{"docs/dross.1", []string{"allow_hosts", ".dross/local.toml"}},
	} {
		b, err := os.ReadFile(filepath.Join(root, tc.file))
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		for _, want := range tc.want {
			if !strings.Contains(string(b), want) {
				t.Errorf("%s does not mention %q", tc.file, want)
			}
		}
	}
}

// repoRootForDocs walks up from the package dir to the module root, so the doc
// assertions do not depend on the test's working directory.
func repoRootForDocs(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not locate the module root from the test working directory")
	return ""
}
