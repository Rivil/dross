package consent

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// The two set lines the tests grant and edit. Their exact shape is the
// caller's (phase.PreconditionSetLine); this package treats them as bytes.
const (
	pcLine       = `[{"preconditions":[{"max":"5","min":null,"run":"echo 3","timeout":"1m0s","why":""}],"task":"t-1"}]`
	pcLineEdited = `[{"preconditions":[{"max":"6","min":null,"run":"echo 3","timeout":"1m0s","why":""}],"task":"t-1"}]`
)

// loadCountingStore counts Load calls, for the case where reaching the store
// at all is the bug.
type loadCountingStore struct {
	Store
	loads int
}

func (s *loadCountingStore) Load() (*Grants, error) {
	s.loads++
	return s.Store.Load()
}

func mustGrantPreconditions(t *testing.T, store Store, phase, line string) {
	t.Helper()
	if err := GrantPreconditionsConsent(store, phase, line); err != nil {
		t.Fatalf("grant %s: %v", phase, err)
	}
}

// TestPreconditionGrantIsPhaseAndLineBound is the ladder, per phase: the
// granted set reads Granted, an edited set Stale, and another phase Absent —
// even when that phase carries the very same set, because consent is to run
// THIS phase's preconditions, not to a set of bytes wherever they appear.
func TestPreconditionGrantIsPhaseAndLineBound(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	mustGrantPreconditions(t, store, "alpha", pcLine)

	for _, tc := range []struct {
		name, phase, line string
		want              State
		wantErr           error
	}{
		{"granted set", "alpha", pcLine, Granted, nil},
		{"edited set", "alpha", pcLineEdited, Stale, ErrStaleConsent},
		{"other phase, same set", "beta", pcLine, Absent, ErrNoConsent},
		{"other phase, other set", "beta", pcLineEdited, Absent, ErrNoConsent},
	} {
		state, err := PreconditionsConsented(store, repoDir, tc.phase, tc.line)
		if state != tc.want {
			t.Errorf("%s: state = %v, want %v", tc.name, state, tc.want)
		}
		if tc.wantErr == nil && err != nil {
			t.Errorf("%s: err = %v, want nil", tc.name, err)
		}
		if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.wantErr)
		}
	}
}

// TestPreconditionGrantStoresAFramedHash: the store holds a fingerprint, never
// the set, and the fingerprint is the framed one — a bare Fingerprint of the
// line, the value a test command with those bytes would carry, must not grant.
func TestPreconditionGrantStoresAFramedHash(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	mustGrantPreconditions(t, store, "alpha", pcLine)

	body, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "echo 3") {
		t.Errorf("the store carries the set itself, not its fingerprint:\n%s", body)
	}
	g, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := g.TrustedPreconditions["alpha"], Fingerprint(PreconditionsFrame+pcLine); got != want {
		t.Errorf("stored grant = %q, want the framed fingerprint %q", got, want)
	}

	if err := store.Save(&Grants{TrustedPreconditions: map[string]string{"alpha": Fingerprint(pcLine)}}); err != nil {
		t.Fatal(err)
	}
	if state, _ := PreconditionsConsented(store, repoDir, "alpha", pcLine); state == Granted {
		t.Error("an unframed fingerprint of the set granted it")
	}
}

// TestGrantingOnePhaseLeavesEveryOtherGrantAlone: granting beta must not
// rewrite alpha's grant, nor any other grant in the store — a grant that
// replaced the map would make trusting one phase revoke the last.
func TestGrantingOnePhaseLeavesEveryOtherGrantAlone(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	seed := &Grants{
		TrustedTestCommand:    Fingerprint("go test ./..."),
		TrustedReplayCommands: "rrrr",
		TrustedLaneCommands:   map[string]string{"go": "llll"},
		TrustedTestExempt:     "eeee",
	}
	if err := store.Save(seed); err != nil {
		t.Fatal(err)
	}
	mustGrantPreconditions(t, store, "alpha", pcLine)
	before, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	alphaBytes := before.TrustedPreconditions["alpha"]

	mustGrantPreconditions(t, store, "beta", pcLineEdited)

	after, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if after.TrustedPreconditions["alpha"] != alphaBytes {
		t.Errorf("granting beta changed alpha's grant: %q -> %q", alphaBytes, after.TrustedPreconditions["alpha"])
	}
	if state, err := PreconditionsConsented(store, repoDir, "alpha", pcLine); state != Granted || err != nil {
		t.Errorf("alpha after granting beta = %v, %v; want granted", state, err)
	}
	if state, err := PreconditionsConsented(store, repoDir, "beta", pcLineEdited); state != Granted || err != nil {
		t.Errorf("beta after its grant = %v, %v; want granted", state, err)
	}
	if after.TrustedTestCommand != seed.TrustedTestCommand || after.TrustedReplayCommands != seed.TrustedReplayCommands ||
		after.TrustedLaneCommands["go"] != "llll" || after.TrustedTestExempt != seed.TrustedTestExempt {
		t.Errorf("a precondition grant disturbed a sibling grant: %+v", after)
	}
}

// TestPreconditionConsentRefusesTrackedLocal: a committed local.toml is a repo
// authorizing its own preconditions. It is refused through RefuseTrackedLocal,
// UNREAD — and a valid grant is planted so only provenance can refuse it.
func TestPreconditionConsentRefusesTrackedLocal(t *testing.T) {
	fs, _, repoDir := consentFixture(t)
	mustGrantPreconditions(t, fs, "alpha", pcLine)
	git(t, repoDir, "add", "-f", RelPath)

	store := &loadCountingStore{Store: fs}
	state, err := PreconditionsConsented(store, repoDir, "alpha", pcLine)
	if state != Refused {
		t.Errorf("state over a tracked local.toml = %v, want refused", state)
	}
	want := RefuseTrackedLocal(repoDir)
	if want == nil || err == nil || err.Error() != want.Error() {
		t.Errorf("err = %v, want RefuseTrackedLocal's own refusal %v", err, want)
	}
	if store.loads != 0 {
		t.Errorf("a tracked local.toml was read %d time(s) before being refused", store.loads)
	}
	if got := PreconditionsRefusal("alpha", state, err); got != err {
		t.Errorf("PreconditionsRefusal rewrote the tracked-store refusal: %v", got)
	}
}

// TestUnparseableStoreIsNotPreconditionConsent: "I could not read the store"
// must fail closed, wrapping ErrNoConsent so a caller can still tell it from a
// refusal to run.
func TestUnparseableStoreIsNotPreconditionConsent(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	if err := os.WriteFile(store.path, []byte("this is not ][ valid toml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := PreconditionsConsented(store, repoDir, "alpha", pcLine)
	if state == Granted {
		t.Fatal("an unparseable local.toml granted the preconditions")
	}
	if state != Absent {
		t.Errorf("state = %v, want absent", state)
	}
	if !errors.Is(err, ErrNoConsent) {
		t.Errorf("err = %v, want it to wrap ErrNoConsent", err)
	}
}

// TestNoPreconditionsNeedNoGrant: an empty set runs nothing, so it is
// NotApplicable without touching the store, and it cannot be granted — a
// grant over nothing would make the first precondition added read Stale.
func TestNoPreconditionsNeedNoGrant(t *testing.T) {
	fs, _, repoDir := consentFixture(t)
	store := &loadCountingStore{Store: fs}
	for _, line := range []string{"", "  \n"} {
		state, err := PreconditionsConsented(store, repoDir, "alpha", line)
		if state != NotApplicable || !errors.Is(err, ErrNoPreconditions) {
			t.Errorf("empty set %q = %v, %v; want not-applicable with ErrNoPreconditions", line, state, err)
		}
	}
	if store.loads != 0 {
		t.Errorf("the store was read %d time(s) for a phase with no preconditions", store.loads)
	}

	if err := GrantPreconditionsConsent(fs, "alpha", ""); !errors.Is(err, ErrNoPreconditions) {
		t.Errorf("granting an empty set = %v, want ErrNoPreconditions", err)
	}
	if err := GrantPreconditionsConsent(fs, " ", pcLine); err == nil {
		t.Error("a grant with no phase was accepted")
	}
	if _, err := os.Stat(fs.path); !os.IsNotExist(err) {
		t.Errorf("a refused grant wrote the store (stat err %v)", err)
	}
}

// TestPreconditionsRefusalNamesThePhaseAndTheHumanStep: every refusing arm
// carries the exact re-consent command for this phase, stale and absent read
// differently, and the sentinel survives for errors.Is.
func TestPreconditionsRefusalNamesThePhaseAndTheHumanStep(t *testing.T) {
	stale := PreconditionsRefusal("alpha", Stale, ErrStaleConsent)
	absent := PreconditionsRefusal("alpha", Absent, ErrNoConsent)

	for name, err := range map[string]error{"stale": stale, "absent": absent} {
		if err == nil {
			t.Fatalf("%s: no refusal", name)
		}
		for _, want := range []string{"dross trust --preconditions alpha", "phase alpha", "human"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s refusal does not mention %q:\n%v", name, want, err)
			}
		}
	}
	if stale.Error() == absent.Error() {
		t.Error("the stale and absent refusals read the same")
	}
	if !strings.Contains(stale.Error(), "CHANGED") || strings.Contains(absent.Error(), "CHANGED") {
		t.Errorf("only the stale refusal should say the set CHANGED:\nstale: %v\nabsent: %v", stale, absent)
	}
	if !errors.Is(stale, ErrStaleConsent) || !errors.Is(absent, ErrNoConsent) {
		t.Errorf("a refusal dropped its sentinel: stale=%v absent=%v", stale, absent)
	}

	na := PreconditionsRefusal("alpha", NotApplicable, ErrNoPreconditions)
	if na != ErrNoPreconditions {
		t.Errorf("NotApplicable refusal = %v, want the cause unchanged", na)
	}
}
