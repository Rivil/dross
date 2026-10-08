package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// writeLocalStore drops a raw local.toml body at the fixture root. Raw TOML
// rather than the typed writer on purpose: these tests are about what an
// on-disk file from a PREVIOUS version of dross resolves to, and a file only
// the current writer can produce would prove nothing about the one already on
// the user's machine.
func writeLocalStore(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, LocalFile), []byte(body), 0o600); err != nil {
		t.Fatalf("write local.toml: %v", err)
	}
}

// TestExistingMutationGrantStillResolves is the compatibility half of the
// alias.
//
// The grant lives in an untracked file, so a clean rename would silently stop
// resolving on every machine that had already granted a host — and the failure
// would present as a LOCAL run the user believed was remote, which is the one
// outcome this whole area is built to prevent.
func TestExistingMutationGrantStillResolves(t *testing.T) {
	root := chdirDross(t)
	writeLocalStore(t, root, "mutation_remote_host = \"helicon\"\nmutation_remote_workdir = \"/home/rivil/dross\"\n")

	target, err := firstRemoteGrant(root, filepath.Dir(root))
	if err != nil {
		t.Fatalf("firstRemoteGrant: %v", err)
	}
	if target == nil {
		t.Fatal("a pre-existing mutation_remote_* grant resolved to no target — the machine that granted it would now run locally without saying so")
	}
	if target.Host != "helicon" {
		t.Errorf("Host = %q, want helicon", target.Host)
	}
	if target.Workdir != "/home/rivil/dross" {
		t.Errorf("Workdir = %q, want /home/rivil/dross", target.Workdir)
	}
}

// TestRemoteGrantKeysAreNotGenericallySettable is the consent model, tested on
// the new key names.
//
// `dross local set` is a generic key-writer: anything it can write, an agent
// can write without ever showing the user what it is authorizing. Granting code
// execution on another machine is not something that may travel that path.
func TestRemoteGrantKeysAreNotGenericallySettable(t *testing.T) {
	for _, key := range []string{
		"remote_host",
		"remote_workdir",
		"mutation_remote_host",
		"mutation_remote_workdir",
	} {
		t.Run(key, func(t *testing.T) {
			chdirDross(t)
			if err := runCmd(t, Local(), "set", key, "helicon"); err == nil {
				t.Fatalf("dross local set %s succeeded — the generic key-writer must not be able to grant a remote", key)
			}
			if _, ok := localKeys[key]; ok {
				t.Errorf("%s is in localKeys", key)
			}
		})
	}
}
