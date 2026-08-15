package cmd

import (
	"os"
	"path/filepath"
	"strings"
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

	target, err := readRemoteGrant(root, filepath.Dir(root))
	if err != nil {
		t.Fatalf("readRemoteGrant: %v", err)
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

// TestNewKeyWinsOverAlias pins the direction of the fallback.
//
// A store carrying both generations is half-migrated — someone re-granted
// through the new verb while the old keys were still on disk — and the value
// they most recently authorized is the new one. Resolving the other way would
// run their code on a box they had already moved off.
func TestNewKeyWinsOverAlias(t *testing.T) {
	root := chdirDross(t)
	writeLocalStore(t, root, strings.Join([]string{
		`remote_host = "newbox"`,
		`remote_workdir = "/srv/new"`,
		`mutation_remote_host = "oldbox"`,
		`mutation_remote_workdir = "/srv/old"`,
		"",
	}, "\n"))

	target, err := readRemoteGrant(root, filepath.Dir(root))
	if err != nil {
		t.Fatalf("readRemoteGrant: %v", err)
	}
	if target == nil {
		t.Fatal("a store carrying both key generations resolved to no target")
	}
	if target.Host != "newbox" {
		t.Errorf("Host = %q, want newbox — the newer grant is the one the user authorized last", target.Host)
	}
	if target.Workdir != "/srv/new" {
		t.Errorf("Workdir = %q, want /srv/new — host and workdir resolve as a pair, or a path from one generation lands on a machine from the other", target.Workdir)
	}
}

// TestRemoteGrantPairResolvesTogether is the mixed-file case the pair rule
// exists for: new host, old workdir. Taking the workdir from the alias would
// point the run at a path on a machine that was never granted with it.
//
// Refusing is the correct outcome, not merely an acceptable one — the pair
// resolves from one generation, so a new host with no new workdir has no
// workdir at all, and `Target.Validate` says so by name. What must never happen
// is a target that silently splices the two halves together.
func TestRemoteGrantPairResolvesTogether(t *testing.T) {
	root := chdirDross(t)
	writeLocalStore(t, root, strings.Join([]string{
		`remote_host = "newbox"`,
		`mutation_remote_workdir = "/srv/old"`,
		"",
	}, "\n"))

	target, err := readRemoteGrant(root, filepath.Dir(root))
	if target != nil && target.Workdir == "/srv/old" {
		t.Fatalf("a new host was paired with the deprecated workdir %q — the pair must resolve from one generation", target.Workdir)
	}
	if err == nil {
		t.Fatalf("a half-migrated store resolved cleanly to %+v — it must refuse rather than guess which half is current", target)
	}
	if !strings.Contains(err.Error(), "workdir") {
		t.Errorf("the refusal does not name the missing half: %v", err)
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

// TestUnreadableStoreIsNotASilentLocalRun: every other reader of local.toml
// treats a decode failure as "no value". A trust-bearing key cannot — "I could
// not read your config" must never resolve to a local run the user thought was
// remote.
func TestUnreadableStoreIsNotASilentLocalRun(t *testing.T) {
	root := chdirDross(t)
	writeLocalStore(t, root, "this is not ][ valid toml\n")

	target, err := readRemoteGrant(root, filepath.Dir(root))
	if err == nil {
		t.Fatalf("an unparseable local.toml resolved cleanly to target=%v — a broken grant must error, not degrade to a local run", target)
	}
	if target != nil {
		t.Errorf("a failed read returned a target: %+v", target)
	}
}
