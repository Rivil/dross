package survivor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// The two structural reasons an acceptance can go stale. Both are statements
// about the subject, never about time: the locked no_acceptance_ttl decision
// rules out expiry by age, which would re-flood the backlog this milestone
// exists to drain, on a timer, with no new information.
const (
	// ReasonFileGone — the file the acceptance names no longer exists.
	ReasonFileGone = "file no longer exists"
	// ReasonTextGone — the file is still there, but the exact source text the
	// acceptance was recorded against is nowhere in it, so the mutant it
	// silenced can no longer be produced.
	ReasonTextGone = "mutant no longer produced"
	// ReasonSurvivorGone — the source text is still there, but the latest run
	// produced no survivor with this key. The mutant is killed now, or the
	// adapter stopped emitting it.
	//
	// This is the case the other two structurally cannot see: they ask whether
	// the SOURCE still exists, and here it does. An acceptance in this state
	// could previously only be retired by hand-supplied key, which meant the
	// verb that exists to find stale entries could not find them.
	ReasonSurvivorGone = "survivor no longer reported"
)

// Stale is one acceptance whose subject is gone, with why.
type Stale struct {
	Acceptance
	Reason string
}

// Unverifiable is an acceptance whose subject could not be checked — an
// unreadable file, or an entry carrying no recorded text. Deliberately separate
// from Stale: "I could not look" is not "it is gone", and collapsing the two
// would let a permissions blip retire live acceptances.
type Unverifiable struct {
	Acceptance
	Err error
}

// Report is a staleness pass's result.
type Report struct {
	Stale        []Stale
	Unverifiable []Unverifiable
}

// StaleAcceptances reports which of the store's acceptances have lost their
// subject, resolving each entry's file relative to root.
//
// It is read-only and clock-free by design (c-5): staleness is detected
// structurally, and the store is never touched. An acceptance whose text merely
// moved to a different line is NOT stale — that is the whole point of a
// text-derived key, and reporting it would make every unrelated edit look like
// a retirement.
//
// The signature deliberately takes no clock, deadline or TTL. If one ever
// appears here, the no_acceptance_ttl lock has been broken.
func StaleAcceptances(root string, s *Store) Report {
	return StaleAcceptancesAgainst(root, s, nil)
}

// StaleAcceptancesAgainst is StaleAcceptances plus the survivor-gone check.
//
// observed is the set of survivor keys the latest run actually reported. A NIL
// observed skips that check entirely — with no run to compare against, every
// acceptance would look orphaned, and retiring the store because nobody has run
// verify lately is the worst possible reading of "stale". Callers say so out
// loud rather than reporting a confident zero.
//
// An EMPTY (non-nil) observed is a real answer: a run happened and reported no
// survivors at all, so every acceptance in the store is orphaned.
func StaleAcceptancesAgainst(root string, s *Store, observed map[string]bool) Report {
	var rep Report
	if s == nil {
		return rep
	}
	// Acceptances cluster per file (a shared category usually means a shared
	// file), so read each file at most once per pass.
	type cached struct {
		src []byte
		err error
	}
	files := map[string]cached{}

	for _, a := range s.Accepted {
		if a.Text == "" {
			rep.Unverifiable = append(rep.Unverifiable, Unverifiable{
				Acceptance: a,
				Err:        errors.New("acceptance records no source text — its subject cannot be located"),
			})
			continue
		}
		c, ok := files[a.File]
		if !ok {
			src, err := os.ReadFile(filepath.Join(root, a.File))
			c = cached{src: src, err: err}
			files[a.File] = c
		}
		switch {
		case c.err == nil:
			if Occurrences(c.src, a.Text) == 0 {
				rep.Stale = append(rep.Stale, Stale{Acceptance: a, Reason: ReasonTextGone})
				continue
			}
			// The source is intact, so the two structural reasons above have
			// nothing to say. Only the run can answer whether the survivor is
			// still there.
			if observed != nil && !observed[a.Key] {
				rep.Stale = append(rep.Stale, Stale{Acceptance: a, Reason: ReasonSurvivorGone})
			}
		case errors.Is(c.err, os.ErrNotExist):
			rep.Stale = append(rep.Stale, Stale{Acceptance: a, Reason: ReasonFileGone})
		default:
			rep.Unverifiable = append(rep.Unverifiable, Unverifiable{
				Acceptance: a,
				Err:        fmt.Errorf("read %s: %w", a.File, c.err),
			})
		}
	}
	return rep
}
