package consent

// The test_exempt grant: one consent over the whole runtime.test_exempt list.
// A path matching it commits with no test run at all, and the list lives in
// TRACKED project.toml, so an edit to it is an edit to what the commit gate
// lets through unmeasured. The grant is what makes that edit a human's to make.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrNoTestExempt is returned for a repo declaring no exempt globs. Distinct
// from ErrNoConsent because there is nothing to consent to and no grant is
// needed: a caller reading the two as one would ask the user to trust a list
// that does not exist.
var ErrNoTestExempt = errors.New("no runtime.test_exempt is configured")

// ExemptFrame is the domain separator for an exempt grant.
//
// A NUL cannot appear in an argv element, so no command line a user could run
// can spell this prefix: a test command's fingerprint and an exempt list's can
// never be the same value, whichever key they are stored under.
const ExemptFrame = "dross:test-exempt:v1\x00"

// SortedExempt returns a sorted copy of globs, in the order a grant is taken
// over and displayed. A copy, because the caller's slice is project.toml's
// document order.
func SortedExempt(globs []string) []string {
	sorted := slices.Clone(globs)
	slices.Sort(sorted)
	return sorted
}

// ExemptLine returns the exact byte string the exempt grant is taken over.
//
// Sorted, because the list is a set: reordering it exempts nothing new, and a
// grant that went stale on a reorder would teach the user to re-consent without
// reading. Every byte of every glob still counts, as it does for a command —
// there is no normalizer, so a duplicated or respaced glob stales the grant.
//
// Each glob is length-framed rather than joined. A join on any separator
// collides ["a,b"] with ["a","b"], and a path can legally carry the separator.
//
// An empty list returns the empty string: there is nothing to bind to, and
// ExemptConsented's NotApplicable arm is keyed on that emptiness.
func ExemptLine(globs []string) string {
	if len(globs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(ExemptFrame)
	for _, glob := range SortedExempt(globs) {
		fmt.Fprintf(&b, "%d\x00%s\x00", len(glob), glob)
	}
	return b.String()
}

// ExemptConsented reports what this machine has said about the exempt list.
//
// Same ladder as LaneConsented, with one difference: the empty check comes
// BEFORE the tracked-store refusal. A repo declaring no exempt globs needs no
// grant, so it has no reason to touch the store or spawn git, and a tracked
// local.toml is reported by the grants that do read it.
//
// NotApplicable carries ErrNoTestExempt like every other non-Granted state
// carries an error, but it is not a refusal: with no globs nothing is exempt,
// and every path still needs its own green.
func ExemptConsented(store Store, repoDir string, globs []string) (State, error) {
	if len(globs) == 0 {
		return NotApplicable, ErrNoTestExempt
	}
	if err := RefuseTrackedLocal(repoDir); err != nil {
		return Refused, err
	}
	g, err := store.Load()
	if err != nil {
		// An unparseable store is not consent. Fail closed, and say why.
		return Absent, fmt.Errorf("%w: %v", ErrNoConsent, err)
	}
	if g.TrustedTestExempt == "" {
		return Absent, ErrNoConsent
	}
	if g.TrustedTestExempt != Fingerprint(ExemptLine(globs)) {
		return Stale, ErrStaleConsent
	}
	return Granted, nil
}

// GrantExemptConsent records consent for globs, storing only the fingerprint
// and leaving every other grant exactly as it was.
func GrantExemptConsent(store Store, globs []string) error {
	g, err := store.Load()
	if err != nil {
		return err
	}
	g.TrustedTestExempt = Fingerprint(ExemptLine(globs))
	return store.Save(g)
}

// ExemptRefusal turns the exempt grant's state into the message the user acts
// on, showing every glob the grant would cover: re-consenting to a list the
// message left out is the rubber stamp the binding exists to prevent.
//
// The remedy names a human on purpose. An agent's `dross trust` is refused, so
// a message reading like a step in the agent's own flow sends it into that
// refusal.
func ExemptRefusal(globs []string, state State, cerr error) error {
	var lines strings.Builder
	for _, glob := range SortedExempt(globs) {
		lines.WriteString("    " + glob + "\n")
	}
	switch state {
	case Refused, NotApplicable:
		return cerr
	case Stale:
		return fmt.Errorf(
			"refusing to treat runtime.test_exempt as trusted: the list has CHANGED since\n"+
				"you trusted it — the recorded consent is stale.\n\n"+
				"%s\n"+
				"Paths matching these globs commit with no test run. Read them above; if that is\n"+
				"what you meant, a human re-consents in their own terminal:\n\n"+
				"    dross trust --test-exempt\n\n%w", lines.String(), cerr)
	default:
		return fmt.Errorf(
			"refusing to treat runtime.test_exempt as trusted: this machine has not trusted\n"+
				"the list.\n\n"+
				"%s\n"+
				"Paths matching these globs commit with no test run, and the list comes from the\n"+
				"repo's tracked project.toml. Read it above, then a human consents in their own\n"+
				"terminal:\n\n"+
				"    dross trust --test-exempt\n\n%w", lines.String(), cerr)
	}
}
