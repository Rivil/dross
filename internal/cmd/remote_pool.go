package cmd

import (
	"github.com/Rivil/dross/internal/remote"
)

// selectRemoteTarget picks the host a run will use from the authorized
// candidates, in the order the user declared them.
//
// The rule is the single-host rule, applied down a list: a candidate that
// cannot be REACHED is skipped, and anything else is the host's answer and is
// returned. A reachable machine missing its toolchain is deliberately NOT
// skipped — that is a fixable configuration hole, `dross remote bootstrap`
// exists to fix it in place, and moving silently to another host would hide it.
//
// Every skip is announced. Two runs measured on different machines are not
// interchangeable evidence — different core counts, different toolchain
// versions — so a pool that quietly moved would make that invisible at exactly
// the moment it matters. The last candidate's failure becomes the fallback
// reason, so a pool where nothing is reachable reads the same as a single host
// that was not.
func selectRemoteTarget(targets []*remote.Target, tools []string) (*remote.Target, preflight, error) {
	if len(targets) == 0 {
		return nil, preflight{}, nil
	}
	var last preflight
	for i, t := range targets {
		pf, err := preflightRemote(*t, tools)
		if err != nil {
			// The host answered and the answer was a failure. Re-trying
			// elsewhere in the hope of a different one is how a real failure
			// gets laundered into a pass.
			return nil, preflight{}, err
		}
		if !pf.Fallback {
			if i > 0 {
				Printf("remote: using %s\n", t.Host)
			}
			return t, pf, nil
		}
		last = pf
		// Announced per candidate rather than only at the end: "tried A, using
		// B" is the fact that makes two runs' numbers comparable or not.
		if i < len(targets)-1 {
			Printf("remote: %s\n", pf.Why)
		}
	}
	return nil, last, nil
}
