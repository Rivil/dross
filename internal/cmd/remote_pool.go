package cmd

import (
	"github.com/Rivil/dross/internal/remote"
)

// pickRemoteTarget picks the host a run would use from the authorized
// candidates, in the order the user declared them, and prints NOTHING.
//
// The rule is the single-host rule, applied down a list: a candidate that
// cannot be REACHED is skipped, and anything else is the host's answer and is
// returned. A reachable machine missing its toolchain is deliberately NOT
// skipped — that is a fixable configuration hole, `dross remote bootstrap`
// exists to fix it in place, and moving silently to another host would hide it.
//
// The announcements come back as data rather than going to stdout, because
// `dross test lane preview --json` has to be able to ask this exact question
// and still emit a bare document. A Printf here would put "remote: ..." on the
// first line of the JSON payload — and preview asking a DIFFERENT question to
// stay quiet would be the divergence c-1 rules out for the derived line and
// c-6 rules out for the host.
//
// Every skip is still announced, by the caller. Two runs measured on different
// machines are not interchangeable evidence — different core counts, different
// toolchain versions — so a pool that quietly moved would make that invisible
// at exactly the moment it matters. The last candidate's failure becomes the
// fallback reason, so a pool where nothing is reachable reads the same as a
// single host that was not.
func pickRemoteTarget(targets []*remote.Target, tools []string) (*remote.Target, preflight, []string, error) {
	if len(targets) == 0 {
		return nil, preflight{}, nil, nil
	}
	var notices []string
	var last preflight
	for i, t := range targets {
		pf, err := preflightRemote(*t, tools)
		if err != nil {
			// The host answered and the answer was a failure. Re-trying
			// elsewhere in the hope of a different one is how a real failure
			// gets laundered into a pass.
			return nil, preflight{}, notices, err
		}
		if !pf.Fallback {
			if i > 0 {
				notices = append(notices, "using "+t.Host)
			}
			return t, pf, notices, nil
		}
		last = pf
		// Recorded per candidate rather than only at the end: "tried A, using
		// B" is the fact that makes two runs' numbers comparable or not.
		if i < len(targets)-1 {
			notices = append(notices, pf.Why)
		}
	}
	return nil, last, notices, nil
}

// selectRemoteTarget is pickRemoteTarget with the announcements printed. It is
// what the RUN calls: a run's transcript is the only record of which machine
// produced its numbers, so the skips reach stdout in the order they happened.
func selectRemoteTarget(targets []*remote.Target, tools []string) (*remote.Target, preflight, error) {
	chosen, pf, notices, err := pickRemoteTarget(targets, tools)
	for _, n := range notices {
		Printf("remote: %s\n", n)
	}
	return chosen, pf, err
}
