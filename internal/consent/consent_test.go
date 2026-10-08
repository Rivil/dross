package consent

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func contains(s, sub string) bool { return strings.Contains(s, sub) }

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
	store, _, _ := consentFixture(t)
	const cmd = "make test-everything"
	if err := GrantConsent(store, cmd); err != nil {
		t.Fatalf("GrantConsent: %v", err)
	}
	b, err := os.ReadFile(store.path)
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
// than a table because the tracked-file arm needs git state the others must not
// share.
func TestConsentStates(t *testing.T) {
	const cmd = "go test -count=1 ./..."

	t.Run("no local.toml", func(t *testing.T) {
		store, _, repoDir := consentFixture(t)
		state, err := CheckConsent(store, repoDir, cmd)
		if state != Absent {
			t.Errorf("state = %v, want absent", state)
		}
		if !errors.Is(err, ErrNoConsent) {
			t.Errorf("err = %v, want ErrNoConsent", err)
		}
	})

	t.Run("store without the key", func(t *testing.T) {
		store, _, repoDir := consentFixture(t)
		if err := store.Save(&Grants{TrustedRunCommands: Fingerprint("something else")}); err != nil {
			t.Fatal(err)
		}
		state, err := CheckConsent(store, repoDir, cmd)
		if state != Absent {
			t.Errorf("state = %v, want absent", state)
		}
		if !errors.Is(err, ErrNoConsent) {
			t.Errorf("err = %v, want ErrNoConsent", err)
		}
	})

	t.Run("key holding a different hash", func(t *testing.T) {
		store, _, repoDir := consentFixture(t)
		// The attack the binding exists for: a repo trusted once, whose
		// test_command a later pull rewrote.
		if err := GrantConsent(store, "a"); err != nil {
			t.Fatal(err)
		}
		state, err := CheckConsent(store, repoDir, "b")
		if state != Stale {
			t.Errorf("state = %v, want stale", state)
		}
		if !errors.Is(err, ErrStaleConsent) {
			t.Errorf("err = %v, want ErrStaleConsent", err)
		}
		if errors.Is(err, ErrNoConsent) {
			t.Error("stale consent also reads as never-trusted — the two sentinels must stay distinct")
		}
		if state, err := CheckConsent(store, repoDir, "a"); state != Granted || err != nil {
			t.Errorf("the trusted command itself: state=%v err=%v, want granted", state, err)
		}
	})

	t.Run("tracked local.toml", func(t *testing.T) {
		store, _, repoDir := consentFixture(t)
		if err := GrantConsent(store, cmd); err != nil {
			t.Fatal(err)
		}
		git(t, repoDir, "add", "-f", RelPath)
		state, err := CheckConsent(store, repoDir, cmd)
		if state != Refused {
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
		store, _, repoDir := consentFixture(t)
		if err := GrantConsent(store, cmd); err != nil {
			t.Fatal(err)
		}
		state, err := CheckConsent(store, repoDir, cmd)
		if state != Granted {
			t.Errorf("state = %v, want granted", state)
		}
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("unparseable store fails closed", func(t *testing.T) {
		store, _, repoDir := consentFixture(t)
		if err := os.WriteFile(store.path, []byte("this = = is not toml\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		state, err := CheckConsent(store, repoDir, cmd)
		if state != Absent || !errors.Is(err, ErrNoConsent) {
			t.Errorf("unparseable store: state=%v err=%v, want absent + ErrNoConsent", state, err)
		}
	})
}

// TestConsentRefusesTrackedLocalToml is the store-provenance half, asserted on
// its own because it is what stops a hostile repo from shipping its own
// consent: a committed local.toml is refused UNREAD even when it holds the
// right hash.
func TestConsentRefusesTrackedLocalToml(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	const cmd = "go test ./..."
	// The hostile shape: the repo commits a local.toml pre-trusting its own
	// test_command, so a clone would arrive already consented.
	if err := GrantConsent(store, cmd); err != nil {
		t.Fatal(err)
	}
	git(t, repoDir, "add", "-f", RelPath)

	_, err := CheckConsent(store, repoDir, cmd)
	if err == nil {
		t.Fatal("a tracked local.toml was honoured rather than refused")
	}
	for _, want := range []string{"refusing to read", RelPath, "tracked", "git rm --cached"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %v", want, err)
		}
	}

	// Untracked, the same file is honoured — the refusal is about provenance,
	// not about the value.
	git(t, repoDir, "rm", "--cached", "-q", RelPath)
	state, err := CheckConsent(store, repoDir, cmd)
	if err != nil || state != Granted {
		t.Fatalf("an untracked consented store must grant: state=%v err=%v", state, err)
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
	store, _, repoDir := consentFixture(t)

	state, err := CheckConsent(store, repoDir, "")
	if state != NotApplicable {
		t.Errorf("state = %v, want not-applicable", state)
	}
	if err == nil {
		t.Error("an empty test_command returned nil error — empty is a refusal, not a bypass")
	}
	if !errors.Is(err, ErrNoTestCommand) {
		t.Errorf("err = %v, want ErrNoTestCommand", err)
	}

	state, err = CheckConsent(store, repoDir, "go test ./...")
	if state != Absent {
		t.Errorf("state = %v, want absent once a command is configured", state)
	}
	if !errors.Is(err, ErrNoConsent) {
		t.Errorf("err = %v, want ErrNoConsent", err)
	}
}

// TestReplayAndRunSetsAreIndependent: the replay and run grants are separate
// sets, each additive — granting one line never revokes another, and neither
// touches the test-command grant.
func TestReplayAndRunSetsAreIndependent(t *testing.T) {
	store, _, _ := consentFixture(t)
	if err := GrantConsent(store, "go test ./..."); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"go test ./a", "go test ./b"} {
		if err := GrantReplayConsent(store, line); err != nil {
			t.Fatal(err)
		}
	}
	if err := GrantRunConsent(store, "pnpm dev"); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"go test ./a", "go test ./b", "  go test ./b  "} {
		if ok, err := ReplayConsented(store, line); err != nil || !ok {
			t.Errorf("ReplayConsented(%q) = %v, %v; want granted", line, ok, err)
		}
	}
	if ok, _ := ReplayConsented(store, "pnpm dev"); ok {
		t.Error("a run grant read as a replay grant")
	}
	if ok, _ := RunConsented(store, "go test ./a"); ok {
		t.Error("a replay grant read as a run grant")
	}
	if ok, _ := RunConsented(store, "pnpm dev"); !ok {
		t.Error("the run grant did not read back")
	}
	if ok, _ := RunConsented(store, ""); ok {
		t.Error("an empty line reads as consented")
	}
	g, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if g.TrustedTestCommand != Fingerprint("go test ./...") {
		t.Error("granting replay/run lines disturbed the test-command grant")
	}
	// Re-granting is idempotent: the set holds each fingerprint once.
	if err := GrantReplayConsent(store, "go test ./a"); err != nil {
		t.Fatal(err)
	}
	g, _ = store.Load()
	if n := strings.Count(g.TrustedReplayCommands, Fingerprint("go test ./a")); n != 1 {
		t.Errorf("fingerprint present %d times after a re-grant", n)
	}
}

// TestStateStrings: every state renders, and the unknown value says so.
func TestStateStrings(t *testing.T) {
	want := map[State]string{Refused: "refused", NotApplicable: "not-applicable", Absent: "absent", Stale: "stale", Granted: "granted", State(99): "unknown"}
	for s, w := range want {
		if s.String() != w {
			t.Errorf("%d.String() = %q, want %q", int(s), s.String(), w)
		}
	}
}

// TestRefusalWording: each arm names what the user must do, and the sentinel
// is wrapped so callers can still match it.
func TestRefusalWording(t *testing.T) {
	const cmd = "go test ./..."
	if err := Refusal(Refused, ErrNoConsent, cmd); err != ErrNoConsent {
		t.Errorf("Refused must return the tracked-store error verbatim, got %v", err)
	}
	na := Refusal(NotApplicable, ErrNoTestCommand, "")
	if !errors.Is(na, ErrNoTestCommand) || !strings.Contains(na.Error(), "no runtime.test_command is configured") || !strings.Contains(na.Error(), "dross trust") {
		t.Errorf("not-applicable refusal = %v", na)
	}
	stale := Refusal(Stale, ErrStaleConsent, cmd)
	if !errors.Is(stale, ErrStaleConsent) || !strings.Contains(stale.Error(), "CHANGED") || !strings.Contains(stale.Error(), cmd) {
		t.Errorf("stale refusal = %v", stale)
	}
	absent := Refusal(Absent, ErrNoConsent, cmd)
	if !errors.Is(absent, ErrNoConsent) || !strings.Contains(absent.Error(), "has not been trusted") || !strings.Contains(absent.Error(), cmd) {
		t.Errorf("absent refusal = %v", absent)
	}
}
