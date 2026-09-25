package localstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readers_test.go holds the tests of the store's readers — ReadKey,
// ReadAllowHosts, ReadMutationTuning, ResolveRemoteEnv and the grant reader —
// moved here from internal/cmd's local_test.go so the package that owns them is
// the one whose tests cover them. Assertions are unchanged. The fixture is local:
// storeRoot or a git-initialised temp dir in place of a full dross root, and a
// direct store write where the cmd test went through `local set`. The tests
// that drive `local set` end to end stay in internal/cmd.

// TestReadLocalKeyIsBestEffort pins the reader other commands use: the store
// is a reconciliation hint, never a gate, so a missing store or an unknown key
// yields "" instead of failing the command that asked.
func TestReadLocalKeyIsBestEffort(t *testing.T) {
	root := storeRoot(t)

	if got := ReadKey(root, "quick_base"); got != "" {
		t.Errorf("missing store should read empty, got %q", got)
	}
	writeLocalStore(t, root, "quick_base = \"main\"\n")
	if got := ReadKey(root, "quick_base"); got != "main" {
		t.Errorf("ReadKey: got %q want %q", got, "main")
	}
	if got := ReadKey(root, "nope"); got != "" {
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

	// The hostile shape: a committed local.toml naming the very host the repo's
	// api_base points at.
	local := filepath.Join(root, File)
	if err := os.WriteFile(local, []byte("allow_hosts = \"attacker.example\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-f", ".dross/"+File)

	hosts, err := ReadAllowHosts(root, dir)
	if err == nil {
		t.Fatal("a tracked local.toml was read rather than refused")
	}
	if hosts != nil {
		t.Errorf("hosts must be nil on refusal, got %v", hosts)
	}
	msg := err.Error()
	for _, want := range []string{"refusing to read", ".dross/" + File, "tracked", "git rm --cached"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal does not mention %q: %v", want, err)
		}
	}
	if strings.Contains(msg, "attacker.example") {
		t.Errorf("the refusal echoed the file's contents — it must not be parsed: %v", err)
	}

	// Untracked, the same file is honoured: the refusal is about provenance,
	// not about the value.
	mustGit(t, dir, "rm", "--cached", "-q", ".dross/"+File)
	hosts, err = ReadAllowHosts(root, dir)
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

	hosts, err := ReadAllowHosts(root, dir)
	if err != nil {
		t.Fatalf("a missing local.toml must not error: %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("want no hosts, got %v", hosts)
	}
}

// TestMutationTuningUnsetIsZeroNotDefaulted pins the difference between "unset"
// and "zero". Unset must reach the adapters as 0 so THEY apply their own
// default — the remote-derived one for a remote run. A reader that substituted
// a local default here would size a 32-core host's run by this laptop.
func TestMutationTuningUnsetIsZeroNotDefaulted(t *testing.T) {
	root := storeRoot(t)

	workers, testCPU, err := ReadMutationTuning(root)
	if err != nil {
		t.Fatalf("ReadMutationTuning on an empty store: %v", err)
	}
	if workers != 0 || testCPU != 0 {
		t.Errorf("ReadMutationTuning = (%d, %d), want (0, 0) for an unset store", workers, testCPU)
	}
}

// TestMutationTuningRefusesAValueThatDidNotTake: a typo'd knob must not resolve
// to the default silently. The user typed something; if it cannot be honoured
// they need to hear which key.
func TestMutationTuningRefusesAValueThatDidNotTake(t *testing.T) {
	root := storeRoot(t)

	// Written straight into the store rather than through `local set`: cobra
	// reads "-4" as a shorthand flag and never reaches the value, and a
	// hand-edited local.toml is a supported way to set these anyway.
	for _, bad := range []string{"eight", "0", "-4", "8 workers"} {
		t.Run(bad, func(t *testing.T) {
			body := fmt.Sprintf("mutation_workers = %q\n", bad)
			if err := os.WriteFile(filepath.Join(root, File), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			w, _, err := ReadMutationTuning(root)
			if err == nil {
				t.Fatalf("mutation_workers = %q was accepted as %d", bad, w)
			}
			if !strings.Contains(err.Error(), "mutation_workers") {
				t.Errorf("error does not name the key: %v", err)
			}
		})
	}
}

// TestMutationTuningAcceptsOne pins the floor from the other side. The refusal
// test above proves 0 is rejected; this proves 1 — a serial run, the smallest
// honest value — is accepted by both knobs, so a floor that drifted to "at least
// 2" fails here. Each knob is read back as a distinct non-zero value: the unset
// store reads (0, 0) whether or not either value was parsed, so it cannot tell
// a reader that dropped one from one that kept both. And test_cpu refuses a bad
// value under its own name, as workers does above.
func TestMutationTuningAcceptsOne(t *testing.T) {
	root := storeRoot(t)

	for _, tc := range []struct {
		body                  string
		wantWorkers, wantTest int
	}{
		{"mutation_workers = \"1\"\nmutation_test_cpu = \"3\"\n", 1, 3},
		{"mutation_workers = \"3\"\nmutation_test_cpu = \"1\"\n", 3, 1},
	} {
		writeLocalStore(t, root, tc.body)
		workers, testCPU, err := ReadMutationTuning(root)
		if err != nil {
			t.Fatalf("ReadMutationTuning(%q): %v", tc.body, err)
		}
		if workers != tc.wantWorkers || testCPU != tc.wantTest {
			t.Errorf("ReadMutationTuning(%q) = (%d, %d), want (%d, %d)",
				tc.body, workers, testCPU, tc.wantWorkers, tc.wantTest)
		}
	}

	writeLocalStore(t, root, "mutation_workers = \"1\"\nmutation_test_cpu = \"0\"\n")
	_, _, err := ReadMutationTuning(root)
	if err == nil {
		t.Fatal("mutation_test_cpu = \"0\" was accepted")
	}
	if !strings.Contains(err.Error(), "mutation_test_cpu") {
		t.Errorf("error does not name the key: %v", err)
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

	if err := os.WriteFile(filepath.Join(root, File),
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
	if err := os.Remove(filepath.Join(root, File)); err != nil {
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

	for _, tc := range []struct{ name, body, wantIn string }{
		{"no workdir", "mutation_remote_host = \"helicon\"\n", "helicon"},
		{"relative workdir", "mutation_remote_host = \"helicon\"\nmutation_remote_workdir = \"srv/x\"\n", "srv/x"},
		{"shell metacharacter", "mutation_remote_host = \"helicon\"\nmutation_remote_workdir = \"/srv/x; id\"\n", "/srv/x; id"},
		{"option-shaped host", "mutation_remote_host = \"-oProxyCommand=id\"\nmutation_remote_workdir = \"/srv/x\"\n", "-oProxyCommand=id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, File), []byte(tc.body), 0o644); err != nil {
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

// TestRemoteEnvRefusesAnUnsetName: an empty export is not an absent one. A
// DATABASE_URL that is absent and one that is empty select different code paths
// — different suites load — so an empty export would silently change WHAT gets
// measured rather than failing.
func TestRemoteEnvRefusesAnUnsetName(t *testing.T) {
	os.Unsetenv("DEFINITELY_NOT_SET_ANYWHERE")

	env, err := ResolveRemoteEnv("DEFINITELY_NOT_SET_ANYWHERE")
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
	if _, err := ResolveRemoteEnv("DEFINITELY_NOT_SET_ANYWHERE"); err != nil {
		t.Errorf("a set-but-empty name was refused: %v", err)
	}
}

// TestRemoteEnvForwardsOnlyAllowlistedNames: dross's own environment carries
// GITHUB_TOKEN and YOUTRACK_TOKEN. "Send everything" would put dross's
// credentials on the mutation host, so only names the user asked for cross.
func TestRemoteEnvForwardsOnlyAllowlistedNames(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_secret")
	t.Setenv("YOUTRACK_TOKEN", "yt_secret")
	t.Setenv("NODE_ENV", "test")

	env, err := ResolveRemoteEnv("NODE_ENV")
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 1 || env[0].Name != "NODE_ENV" {
		t.Fatalf("ResolveRemoteEnv = %+v, want only NODE_ENV", env)
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
	t.Setenv("A_VAR", "1")
	t.Setenv("B_VAR", "2")

	env, err := ResolveRemoteEnv(" A_VAR , B_VAR ,")
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 2 || env[0].Name != "A_VAR" || env[1].Name != "B_VAR" {
		t.Errorf("ResolveRemoteEnv = %+v", env)
	}
	if env, err := ResolveRemoteEnv(""); err != nil || env != nil {
		t.Errorf("an unset allowlist = (%+v, %v), want (nil, nil) — env is optional", env, err)
	}
}

// TestRemoteGrantCarriesTheResolvedEnv: the grant reader is where the names
// become values, so every remote command in the run inherits the same
// environment rather than each call site resolving it separately.
func TestRemoteGrantCarriesTheResolvedEnv(t *testing.T) {
	root := storeRoot(t)
	t.Setenv("NODE_ENV", "test")

	body := "mutation_remote_host = \"helicon\"\nmutation_remote_workdir = \"/srv/dross\"\nmutation_remote_env = \"NODE_ENV\"\n"
	if err := os.WriteFile(filepath.Join(root, File), []byte(body), 0o644); err != nil {
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
	if err := os.WriteFile(filepath.Join(root, File), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := firstRemoteGrant(root, filepath.Dir(root)); err == nil {
		t.Fatal("a grant naming an unset variable resolved anyway")
	}
}
