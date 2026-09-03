package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// TestRemoteGrantPrintsBeforeItWrites is the consent_model decision's ordering
// half, executed against the current verb.
//
// The verb exists because `dross local set` would let an agent authorize code
// execution on another machine without ever showing the user what for. That
// only holds if the banner reaches the user BEFORE the authorization lands — a
// grant that wrote first and printed after has already authorized the host by
// the time its name is on screen, and the print is then a receipt rather than a
// consent.
//
// Injecting a failing write is the only way to observe the order: on the happy
// path both happen and the sequence is invisible.
func TestRemoteGrantPrintsBeforeItWrites(t *testing.T) {
	root := chdirDross(t)

	orig := grantRemoteWrite
	t.Cleanup(func() { grantRemoteWrite = orig })
	grantRemoteWrite = func(_, _, _ string) error {
		return errors.New("injected write failure")
	}

	var out string
	err := runCmdCapturing(t, &out, Remote(), "grant", "helicon", "/srv/dross")
	if err == nil {
		t.Fatal("the injected write failure did not surface — the command reported success")
	}

	for _, want := range []string{"helicon", "/srv/dross"} {
		if !strings.Contains(out, want) {
			t.Errorf("the banner does not name %q — it must print what it authorizes before it writes it:\n%s", want, out)
		}
	}

	if _, statErr := os.Stat(filepath.Join(root, LocalFile)); !os.IsNotExist(statErr) {
		t.Errorf("a failed grant left a store behind, stat err = %v", statErr)
	}
}

// TestMutationRemoteAliasStillGrants: the old path keeps working.
//
// The README documents `dross mutation remote grant` and it is in muscle
// memory; a rename that broke it would make a working setup fail for a name.
func TestMutationRemoteAliasStillGrants(t *testing.T) {
	root := chdirDross(t)

	if err := runCmd(t, Mutation(), "remote", "grant", "helicon", "/srv/dross"); err != nil {
		t.Fatalf("dross mutation remote grant: %v", err)
	}
	target, err := firstRemoteGrant(root, filepath.Dir(root))
	if err != nil {
		t.Fatalf("firstRemoteGrant: %v", err)
	}
	if target == nil || target.Host != "helicon" || target.Workdir != "/srv/dross" {
		t.Fatalf("the alias did not grant: %+v", target)
	}

	if err := runCmd(t, Mutation(), "remote", "revoke"); err != nil {
		t.Fatalf("dross mutation remote revoke: %v", err)
	}
	target, err = firstRemoteGrant(root, filepath.Dir(root))
	if err != nil {
		t.Fatalf("firstRemoteGrant after revoke: %v", err)
	}
	if target != nil {
		t.Errorf("the alias's revoke left %+v", target)
	}
}

// TestBothVerbsWriteTheSameKeys is what stops the alias from becoming a second
// implementation. Two spellings of one consent decision must produce one store.
func TestBothVerbsWriteTheSameKeys(t *testing.T) {
	read := func(t *testing.T, grant func(*testing.T)) string {
		t.Helper()
		root := chdirDross(t)
		grant(t)
		b, err := os.ReadFile(filepath.Join(root, LocalFile))
		if err != nil {
			t.Fatalf("read local.toml: %v", err)
		}
		return string(b)
	}

	viaNew := read(t, func(t *testing.T) {
		if err := runCmd(t, Remote(), "grant", "helicon", "/srv/dross"); err != nil {
			t.Fatalf("dross remote grant: %v", err)
		}
	})
	viaOld := read(t, func(t *testing.T) {
		if err := runCmd(t, Mutation(), "remote", "grant", "helicon", "/srv/dross"); err != nil {
			t.Fatalf("dross mutation remote grant: %v", err)
		}
	})

	if viaNew != viaOld {
		t.Errorf("the two verbs wrote different stores:\n--- dross remote ---\n%s\n--- dross mutation remote ---\n%s", viaNew, viaOld)
	}
	// And it is the current spelling that lands, not the deprecated one — a
	// grant issued today should not leave a legacy key for the next reader to
	// arbitrate.
	if !strings.Contains(viaNew, "remote_host") {
		t.Errorf("the grant did not write remote_host:\n%s", viaNew)
	}
	if strings.Contains(viaNew, "mutation_remote_host") {
		t.Errorf("a fresh grant wrote the deprecated key:\n%s", viaNew)
	}
}

// TestGrantMigratesALegacyStore: re-granting on a machine that holds the old
// keys must leave one generation behind, not two.
func TestGrantMigratesALegacyStore(t *testing.T) {
	root := chdirDross(t)
	writeLocalStore(t, root, "mutation_remote_host = \"oldbox\"\nmutation_remote_workdir = \"/srv/old\"\nmutation_workers = \"8\"\n")

	if err := runCmd(t, Remote(), "grant", "newbox", "/srv/new"); err != nil {
		t.Fatalf("dross remote grant: %v", err)
	}

	l, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if l.MutationRemoteHost != "" || l.MutationRemoteWorkdir != "" {
		t.Errorf("the legacy keys survived a re-grant: host=%q workdir=%q", l.MutationRemoteHost, l.MutationRemoteWorkdir)
	}
	if l.RemoteHost != "newbox" || l.RemoteWorkdir != "/srv/new" {
		t.Errorf("the new grant did not land: host=%q workdir=%q", l.RemoteHost, l.RemoteWorkdir)
	}
	// The tuning knob is not authorization and must not be touched.
	if l.MutationWorkers != "8" {
		t.Errorf("the re-grant changed mutation_workers to %q, want 8", l.MutationWorkers)
	}
}

// TestRevokeClearsHostAndWorkdir — in both spellings.
//
// A workdir left behind after its host is gone is a leftover the next grant
// inherits, and a host left under the deprecated key after a revoke would mean
// "withdrawn" only withdrew one spelling.
func TestRevokeClearsHostAndWorkdir(t *testing.T) {
	root := chdirDross(t)
	writeLocalStore(t, root, strings.Join([]string{
		`remote_host = "newbox"`,
		`remote_workdir = "/srv/new"`,
		`mutation_remote_host = "oldbox"`,
		`mutation_remote_workdir = "/srv/old"`,
		`mutation_workers = "8"`,
		"",
	}, "\n"))

	if err := runCmd(t, Remote(), "revoke"); err != nil {
		t.Fatalf("dross remote revoke: %v", err)
	}

	l, err := loadLocal(localPath(root))
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{
		"remote_host":             l.RemoteHost,
		"remote_workdir":          l.RemoteWorkdir,
		"mutation_remote_host":    l.MutationRemoteHost,
		"mutation_remote_workdir": l.MutationRemoteWorkdir,
	} {
		if got != "" {
			t.Errorf("revoke left %s = %q", name, got)
		}
	}
	if l.MutationWorkers != "8" {
		t.Errorf("revoke changed mutation_workers to %q, want 8 — withdrawing authorization is not a reason to reset a tuning knob", l.MutationWorkers)
	}
}

// TestRemoteStatusReportsBothGenerations: status resolves through the same
// pool walk a run does, so it can never report a grant the run would
// refuse — including a legacy one.
func TestRemoteStatusReportsBothGenerations(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"current keys", "remote_host = \"helicon\"\nremote_workdir = \"/srv/dross\"\n"},
		{"deprecated aliases", "mutation_remote_host = \"helicon\"\nmutation_remote_workdir = \"/srv/dross\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := chdirDross(t)
			writeLocalStore(t, root, tc.body)
			// Status resolves by PROBING now, so the walk has to be stubbed —
			// a status that reported the config value without asking is
			// exactly the divergence c-2 removes.
			fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
				return remote.Readiness{Cores: 8}, nil
			})

			var out string
			if err := runCmdCapturing(t, &out, Remote(), "status"); err != nil {
				t.Fatalf("dross remote status: %v", err)
			}
			if !strings.Contains(out, "helicon") || !strings.Contains(out, "/srv/dross") {
				t.Errorf("status does not report the grant a run would use:\n%s", out)
			}
		})
	}
}
