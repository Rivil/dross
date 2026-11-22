package consent

// The precondition grant: one consent per phase over that phase's whole set of
// task preconditions. A precondition is a shell command from plan.toml, which
// is TRACKED, so a pull can add or rewrite one; the grant is what makes running
// it a human's decision rather than the plan author's.

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoPreconditions is returned for a phase declaring no preconditions.
// Distinct from ErrNoConsent because there is nothing to consent to: a caller
// reading the two as one would ask the user to trust a set that does not exist.
var ErrNoPreconditions = errors.New("this phase declares no preconditions")

// PreconditionsFrame is the domain separator for a precondition grant.
//
// A NUL cannot appear in an argv element, so no command line a user could run
// can spell this prefix: a test command's fingerprint and a precondition set's
// can never be the same value, whichever key they are stored under.
const PreconditionsFrame = "dross:preconditions:v1\x00"

// preconditionsFingerprint is the value a phase's grant records and is
// compared against: the framed set line, hashed. One function for both halves,
// so the grant and the check cannot frame differently.
func preconditionsFingerprint(line string) string {
	return Fingerprint(PreconditionsFrame + line)
}

// PreconditionsConsented reports what this machine has said about ONE phase's
// precondition set, with line the set's canonical form (the caller builds it;
// this package never reads a plan).
//
// Same ladder as ExemptConsented, empty check first: a phase with no
// preconditions runs nothing, so it has no reason to touch the store or spawn
// git, and NotApplicable here is not a refusal. Every other state but Granted
// is. Stale and Absent stay distinct for the reason they do everywhere: a set
// rewritten since it was trusted is the case the binding exists for, and
// reporting it as a first run would hide it.
//
// The grant is looked up under phase and nowhere else, so a grant for one phase
// never satisfies another even when the two sets are byte-identical.
func PreconditionsConsented(store Store, repoDir, phase, line string) (State, error) {
	if strings.TrimSpace(line) == "" {
		return NotApplicable, fmt.Errorf("%w: %s", ErrNoPreconditions, phase)
	}
	if err := RefuseTrackedLocal(repoDir); err != nil {
		return Refused, err
	}
	g, err := store.Load()
	if err != nil {
		// An unparseable store is not consent. Fail closed, and say why.
		return Absent, fmt.Errorf("%w: %v", ErrNoConsent, err)
	}
	got, ok := g.TrustedPreconditions[phase]
	if !ok || got == "" {
		return Absent, ErrNoConsent
	}
	if got != preconditionsFingerprint(line) {
		return Stale, ErrStaleConsent
	}
	return Granted, nil
}

// GrantPreconditionsConsent records consent for one phase's precondition set,
// storing only its fingerprint and leaving every other phase's grant — and
// every other grant in the store — exactly as it was.
//
// An empty phase or set is refused rather than recorded. A grant over an empty
// set would sit in the file as consent to nothing, and the first precondition
// later added would then read Stale — "changed since you trusted it" — about a
// set nobody was ever shown.
func GrantPreconditionsConsent(store Store, phase, line string) error {
	if strings.TrimSpace(phase) == "" {
		return errors.New("a precondition grant needs a phase")
	}
	if strings.TrimSpace(line) == "" {
		return fmt.Errorf("%w: %s", ErrNoPreconditions, phase)
	}
	g, err := store.Load()
	if err != nil {
		return err
	}
	if g.TrustedPreconditions == nil {
		g.TrustedPreconditions = map[string]string{}
	}
	g.TrustedPreconditions[phase] = preconditionsFingerprint(line)
	return store.Save(g)
}

// PreconditionsRefusal turns one phase's precondition consent state into the
// message the user acts on, naming the phase and the exact command that
// re-consents in every refusing arm.
//
// The remedy names a human on purpose, on ExemptRefusal's precedent: an
// agent's `dross trust` is refused, so a message reading like a step in the
// agent's own flow sends it into that refusal. The commands themselves are not
// repeated here — `dross trust --preconditions` prints every one before it
// records anything, and that listing is what the human reads.
func PreconditionsRefusal(phase string, state State, cerr error) error {
	switch state {
	case Refused, NotApplicable:
		return cerr
	case Stale:
		return fmt.Errorf(
			"refusing to run phase %s's preconditions: the set has CHANGED since you\n"+
				"trusted it — the recorded consent is stale.\n\n"+
				"A precondition was added, removed or edited after the grant. That is the case\n"+
				"this gate exists for: plan.toml is tracked, so a pull can rewrite what a\n"+
				"precondition runs. A human reviews the listing and re-consents in their own\n"+
				"terminal:\n\n"+
				"    dross trust --preconditions %s\n\n%w", phase, phase, cerr)
	default:
		return fmt.Errorf(
			"refusing to run phase %s's preconditions: this machine has not trusted them.\n\n"+
				"Each precondition is a shell command from the phase's tracked plan.toml, so a\n"+
				"clone carries whatever its author wrote. A human reviews the listing and\n"+
				"consents in their own terminal:\n\n"+
				"    dross trust --preconditions %s\n\n%w", phase, phase, cerr)
	}
}
