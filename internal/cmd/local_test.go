package cmd

import (
	"fmt"
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

// TestMutationTuningUnsetIsZeroNotDefaulted pins the difference between "unset"
// and "zero". Unset must reach the adapters as 0 so THEY apply their own
// default — the remote-derived one for a remote run. A reader that substituted
// a local default here would size a 32-core host's run by this laptop.
func TestMutationTuningUnsetIsZeroNotDefaulted(t *testing.T) {
	root := chdirDross(t)

	workers, testCPU, err := readMutationTuning(root)
	if err != nil {
		t.Fatalf("readMutationTuning on an empty store: %v", err)
	}
	if workers != 0 || testCPU != 0 {
		t.Errorf("readMutationTuning = (%d, %d), want (0, 0) for an unset store", workers, testCPU)
	}
}

// TestMutationTuningRefusesAValueThatDidNotTake: a typo'd knob must not resolve
// to the default silently. The user typed something; if it cannot be honoured
// they need to hear which key.
func TestMutationTuningRefusesAValueThatDidNotTake(t *testing.T) {
	root := chdirDross(t)

	// Written straight into the store rather than through `local set`: cobra
	// reads "-4" as a shorthand flag and never reaches the value, and a
	// hand-edited local.toml is a supported way to set these anyway.
	for _, bad := range []string{"eight", "0", "-4", "8 workers"} {
		t.Run(bad, func(t *testing.T) {
			body := fmt.Sprintf("mutation_workers = %q\n", bad)
			if err := os.WriteFile(filepath.Join(root, LocalFile), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			w, _, err := readMutationTuning(root)
			if err == nil {
				t.Fatalf("mutation_workers = %q was accepted as %d", bad, w)
			}
			if !strings.Contains(err.Error(), "mutation_workers") {
				t.Errorf("error does not name the key: %v", err)
			}
		})
	}
}

// TestReadRemoteGrantRefusesTrackedLocal is c-2's machine-local half.
//
// A committed local.toml naming a remote host is a repo shipping the machine it
// wants your working tree rsync'd to and your test suite run on. The refusal
// fires UNREAD — the same provenance check allow_hosts and the exec consent gate
// go through, not a second one that could drift.
func TestReadRemoteGrantRefusesTrackedLocal(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompleteRoot(t, root)

	// A VALID grant, so the test proves provenance is what refuses it rather
	// than the value happening to be malformed.
	body := "mutation_remote_host = \"attacker.example\"\nmutation_remote_workdir = \"/srv/dross\"\n"
	local := filepath.Join(root, LocalFile)
	if err := os.WriteFile(local, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-f", ".dross/"+LocalFile)

	got, err := firstRemoteGrant(root, dir)
	if err == nil {
		t.Fatal("a tracked local.toml was read rather than refused")
	}
	if got != nil {
		t.Errorf("grant must be nil on refusal, got %+v", got)
	}
	if strings.Contains(err.Error(), "attacker.example") {
		t.Errorf("the refusal echoed the file's contents — it must not be parsed: %v", err)
	}
	for _, want := range []string{"refusing to read", ".dross/" + LocalFile, "tracked"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %v", want, err)
		}
	}

	// Untracked, the same file grants: the refusal is about provenance, not
	// about the value.
	mustGit(t, dir, "rm", "--cached", "-q", ".dross/"+LocalFile)
	got, err = firstRemoteGrant(root, dir)
	if err != nil {
		t.Fatalf("an untracked local.toml must be readable: %v", err)
	}
	if got == nil || got.Host != "attacker.example" || got.Workdir != "/srv/dross" {
		t.Errorf("grant did not parse: %+v", got)
	}
}

// TestReadRemoteGrantWorkdirAloneIsNoGrant: the HOST is the authorization. A
// leftover workdir with no host is not half a grant — it is nothing, and
// treating it as authorization would be reading intent into a stale value.
func TestReadRemoteGrantWorkdirAloneIsNoGrant(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompleteRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, LocalFile),
		[]byte("mutation_remote_workdir = \"/srv/dross\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := firstRemoteGrant(root, dir)
	if err != nil {
		t.Fatalf("a workdir with no host must not error: %v", err)
	}
	if got != nil {
		t.Errorf("a workdir alone granted %+v", got)
	}

	// A missing store is likewise no grant, not a failure.
	if err := os.Remove(filepath.Join(root, LocalFile)); err != nil {
		t.Fatal(err)
	}
	got, err = firstRemoteGrant(root, dir)
	if err != nil || got != nil {
		t.Errorf("a missing store = (%+v, %v), want (nil, nil)", got, err)
	}
}

// TestReadRemoteGrantRefusesAnUnusableHost: a host WITH no usable workdir is
// the opposite case — something was authorized and cannot be honoured, so it is
// named rather than quietly dropped back to a local run.
func TestReadRemoteGrantRefusesAnUnusableHost(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompleteRoot(t, root)

	for _, tc := range []struct{ name, body, wantIn string }{
		{"no workdir", "mutation_remote_host = \"helicon\"\n", "helicon"},
		{"relative workdir", "mutation_remote_host = \"helicon\"\nmutation_remote_workdir = \"srv/x\"\n", "srv/x"},
		{"shell metacharacter", "mutation_remote_host = \"helicon\"\nmutation_remote_workdir = \"/srv/x; id\"\n", "/srv/x; id"},
		{"option-shaped host", "mutation_remote_host = \"-oProxyCommand=id\"\nmutation_remote_workdir = \"/srv/x\"\n", "-oProxyCommand=id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, LocalFile), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := firstRemoteGrant(root, dir)
			if err == nil {
				t.Fatalf("unusable grant was accepted: %+v", got)
			}
			if got != nil {
				t.Errorf("grant must be nil alongside an error, got %+v", got)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error does not name the offending value %q: %v", tc.wantIn, err)
			}
		})
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

// TestRemoteEnvRefusesAnUnsetName: an empty export is not an absent one. A
// DATABASE_URL that is absent and one that is empty select different code paths
// — different suites load — so an empty export would silently change WHAT gets
// measured rather than failing.
func TestRemoteEnvRefusesAnUnsetName(t *testing.T) {
	chdirDross(t)
	os.Unsetenv("DEFINITELY_NOT_SET_ANYWHERE")

	env, err := resolveRemoteEnv("DEFINITELY_NOT_SET_ANYWHERE")
	if err == nil {
		t.Fatalf("an unset name was exported anyway: %+v", env)
	}
	if env != nil {
		t.Errorf("a refusal still returned %+v", env)
	}
	if !strings.Contains(err.Error(), "DEFINITELY_NOT_SET_ANYWHERE") {
		t.Errorf("the refusal does not name the variable: %v", err)
	}

	// An empty-but-SET name is fine: the user said it should cross, and empty is
	// a value they chose.
	t.Setenv("DEFINITELY_NOT_SET_ANYWHERE", "")
	if _, err := resolveRemoteEnv("DEFINITELY_NOT_SET_ANYWHERE"); err != nil {
		t.Errorf("a set-but-empty name was refused: %v", err)
	}
}

// TestRemoteEnvForwardsOnlyAllowlistedNames: dross's own environment carries
// GITHUB_TOKEN and YOUTRACK_TOKEN. "Send everything" would put dross's
// credentials on the mutation host, so only names the user asked for cross.
func TestRemoteEnvForwardsOnlyAllowlistedNames(t *testing.T) {
	chdirDross(t)
	t.Setenv("GITHUB_TOKEN", "ghp_secret")
	t.Setenv("YOUTRACK_TOKEN", "yt_secret")
	t.Setenv("NODE_ENV", "test")

	env, err := resolveRemoteEnv("NODE_ENV")
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 1 || env[0].Name != "NODE_ENV" {
		t.Fatalf("resolveRemoteEnv = %+v, want only NODE_ENV", env)
	}
	for _, e := range env {
		if strings.Contains(e.Name, "TOKEN") || strings.Contains(e.Value, "secret") {
			t.Errorf("a non-allowlisted credential crossed: %+v", e)
		}
	}
}

// TestRemoteEnvTrimsAndIgnoresBlanks covers the comma-separated form a human
// would actually type, including a trailing comma.
func TestRemoteEnvTrimsAndIgnoresBlanks(t *testing.T) {
	chdirDross(t)
	t.Setenv("A_VAR", "1")
	t.Setenv("B_VAR", "2")

	env, err := resolveRemoteEnv(" A_VAR , B_VAR ,")
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 2 || env[0].Name != "A_VAR" || env[1].Name != "B_VAR" {
		t.Errorf("resolveRemoteEnv = %+v", env)
	}
	if env, err := resolveRemoteEnv(""); err != nil || env != nil {
		t.Errorf("an unset allowlist = (%+v, %v), want (nil, nil) — env is optional", env, err)
	}
}

// TestRemoteGrantCarriesTheResolvedEnv: the grant reader is where the names
// become values, so every remote command in the run inherits the same
// environment rather than each call site resolving it separately.
func TestRemoteGrantCarriesTheResolvedEnv(t *testing.T) {
	root := chdirDross(t)
	t.Setenv("NODE_ENV", "test")

	body := "mutation_remote_host = \"helicon\"\nmutation_remote_workdir = \"/srv/dross\"\nmutation_remote_env = \"NODE_ENV\"\n"
	if err := os.WriteFile(filepath.Join(root, LocalFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	target, err := firstRemoteGrant(root, filepath.Dir(root))
	if err != nil {
		t.Fatalf("firstRemoteGrant: %v", err)
	}
	if target == nil {
		t.Fatal("no grant")
	}
	if len(target.Env) != 1 || target.Env[0].Name != "NODE_ENV" || target.Env[0].Value != "test" {
		t.Errorf("the grant did not carry the resolved env: %+v", target.Env)
	}

	// An unset allowlisted name refuses the whole grant, so a run cannot start
	// with a half-populated environment.
	body = strings.Replace(body, "NODE_ENV\"", "NODE_ENV,NOT_SET_ANYWHERE_AT_ALL\"", 1)
	if err := os.WriteFile(filepath.Join(root, LocalFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := firstRemoteGrant(root, filepath.Dir(root)); err == nil {
		t.Fatal("a grant naming an unset variable resolved anyway")
	}
}
