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

// TestRemoteGrantKeysAreNotSettable is the consent_model decision as a test.
//
// Configuring a remote is code execution on a machine of the config's choosing.
// `dross local set` is a generic key-writer: anything it can write, an agent can
// write without ever showing the user what it is authorizing. So the two grant
// keys are excluded from localKeys exactly as trusted_test_command is, and this
// fails the moment someone adds them "for symmetry".
func TestRemoteGrantKeysAreNotSettable(t *testing.T) {
	root := chdirDross(t)

	for _, key := range []string{"mutation_remote_host", "mutation_remote_workdir", "trusted_test_command"} {
		t.Run(key, func(t *testing.T) {
			err := runCmd(t, Local(), "set", key, "helicon")
			if err == nil {
				t.Fatalf("`local set %s` succeeded — the grant must come from a verb that shows what it authorizes", key)
			}
			if !strings.Contains(err.Error(), "unknown local key") {
				t.Errorf("error should say the key is unknown: %v", err)
			}
			// The printed key list is what a user reads to find out what they
			// CAN set. A grant key appearing there is an invitation.
			if strings.Contains(err.Error(), key) && strings.Contains(err.Error(), "want ") {
				after := err.Error()[strings.Index(err.Error(), "want "):]
				if strings.Contains(after, key) {
					t.Errorf("the suggested key list advertises %q: %v", key, err)
				}
			}
			if err := runCmd(t, Local(), "get", key); err == nil {
				t.Errorf("`local get %s` succeeded — the key set must be closed in both directions", key)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, LocalFile)); !os.IsNotExist(err) {
		t.Errorf("a refused key must not create the store, stat err = %v", err)
	}
}

// TestMutationTuningKeysRoundTrip: workers and test-cpu ARE settable. They are
// performance knobs, not authorization — the worst a wrong value does is make a
// run slow, which is a different category from granting code execution.
func TestMutationTuningKeysRoundTrip(t *testing.T) {
	root := chdirDross(t)

	for key, val := range map[string]string{"mutation_workers": "8", "mutation_test_cpu": "2"} {
		if err := runCmd(t, Local(), "set", key, val); err != nil {
			t.Fatalf("local set %s: %v", key, err)
		}
		var out string
		if err := runCmdCapturing(t, &out, Local(), "get", key); err != nil {
			t.Fatalf("local get %s: %v", key, err)
		}
		if strings.TrimSpace(out) != val {
			t.Errorf("%s did not round-trip: got %q want %q", key, strings.TrimSpace(out), val)
		}
	}

	workers, testCPU, err := readMutationTuning(root)
	if err != nil {
		t.Fatalf("readMutationTuning: %v", err)
	}
	if workers != 8 || testCPU != 2 {
		t.Errorf("readMutationTuning = (%d, %d), want (8, 2)", workers, testCPU)
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

// --- mutation_remote_env (c-8) ---

// TestRemoteEnvKeyIsSettableAndHoldsOnlyNames.
//
// mutation_remote_env IS in localKeys, unlike the two grant keys, and the
// name/value split is why: names are not secrets, so the key needs none of the
// grant verb's ceremony. The property that makes it safe is that dross stores
// no value anywhere — asserted here by reading the file back after a resolve
// and checking the VALUE is absent from it.
func TestRemoteEnvKeyIsSettableAndHoldsOnlyNames(t *testing.T) {
	root := chdirDross(t)
	const secret = "postgres://user:hunter2@db.internal/app"
	t.Setenv("DATABASE_URL", secret)
	t.Setenv("NODE_ENV", "test")

	if err := runCmd(t, Local(), "set", "mutation_remote_env", "DATABASE_URL,NODE_ENV"); err != nil {
		t.Fatalf("local set mutation_remote_env: %v", err)
	}
	var out string
	if err := runCmdCapturing(t, &out, Local(), "get", "mutation_remote_env"); err != nil {
		t.Fatalf("local get: %v", err)
	}
	if strings.TrimSpace(out) != "DATABASE_URL,NODE_ENV" {
		t.Errorf("did not round-trip: got %q", strings.TrimSpace(out))
	}

	env, err := resolveRemoteEnv("DATABASE_URL,NODE_ENV")
	if err != nil {
		t.Fatalf("resolveRemoteEnv: %v", err)
	}
	if len(env) != 2 || env[0].Name != "DATABASE_URL" || env[0].Value != secret {
		t.Fatalf("resolveRemoteEnv = %+v", env)
	}

	b, err := os.ReadFile(filepath.Join(root, LocalFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) || strings.Contains(string(b), "hunter2") {
		t.Errorf("a VALUE was written to the store — it holds names only:\n%s", b)
	}
}
