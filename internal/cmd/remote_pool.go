package cmd

import (
	"github.com/Rivil/dross/internal/remote"
)

// poolCandidate is one authorized host that ANSWERED a probe, and what it said.
//
// Ready is that host's own Readiness — its core count and which of the probed
// tools it lacks. A candidate that could not be reached is not one of these at
// all: it told us nothing about its toolchain, and an empty Missing must never
// be read as "it has everything".
type poolCandidate struct {
	Target *remote.Target
	Ready  remote.Readiness
}

// remotePool is what the single pre-sync probe learned about the whole pool.
//
// It exists because "which host does this run use" stopped being one question
// the moment lanes route by toolchain: a run with a `go` lane and a `pnpm` lane
// may legitimately need two answers. So the walk reports every reachable
// candidate's readiness rather than only the one it would have chosen, and the
// per-lane decision is made from this set (c-1).
type remotePool struct {
	// Candidates is every host that answered, in the order the user declared
	// them. Declared order is the preference: earlier is better, all else
	// equal.
	Candidates []poolCandidate
	// Notices is the narration for what the walk did — every skipped host, in
	// probe order. Data rather than stdout, because `dross test lane preview
	// --json` asks this exact question and must still emit a bare document; a
	// Printf here would put "remote: ..." on the first line of the payload.
	Notices []string
	// Fallback reports that NO candidate answered, so the run comes home. Why
	// is the last candidate's transport failure, which is the reason the caller
	// prints — a pool where nothing is reachable reads the same as a single
	// host that was not.
	Fallback bool
	Why      string
	// Failed is the candidate whose probe returned an answer that was a
	// failure, set only alongside a non-nil error. The walk bails there rather
	// than at the end, so a caller that wants to name the machine cannot
	// recover it from the slice.
	Failed *remote.Target
}

// probeRemotePool probes the authorized candidates for tools, in the order the
// user declared them, and prints NOTHING.
//
// The rule is the single-host rule, applied down a list: a candidate that
// cannot be REACHED is skipped, and anything else is that host's answer. A
// reachable machine missing its toolchain is deliberately NOT skipped — that is
// a fixable configuration hole, `dross remote bootstrap` exists to fix it in
// place, and dropping it would lose the one candidate a later lane may still be
// able to use.
//
// It walks PAST the first host that answers, because the first host answering
// is no longer the end of the question: a pool whose second machine alone has
// `pnpm` must report that machine, or the lane needing it comes home for a tool
// the pool actually holds. But it stops as soon as the answer is complete —
// once some reachable candidate covers every probed tool, a further round trip
// could not change any lane's destination, and a run that asks for nothing (the
// whole-suite case, tools nil) still costs exactly one probe as it always did.
//
// Every skip is still announced, by the caller. Two runs measured on different
// machines are not interchangeable evidence — different core counts, different
// toolchain versions — so a pool that quietly moved would make that invisible
// at exactly the moment it matters (locked routing_is_announced_never_silent).
//
// One probe per candidate, for the UNION of every matched lane's toolchain, and
// before the tree is pushed (locked one_probe_not_per_lane_probes). Probing per
// lane would multiply ssh round trips by the lane count on every gate run for
// information that is the same either way.
func probeRemotePool(targets []*remote.Target, tools []string) (remotePool, error) {
	var pool remotePool
	if len(targets) == 0 {
		return pool, nil
	}
	// The tools no reachable candidate has accounted for yet. It starts as the
	// whole union and shrinks as hosts answer; empty means a further probe
	// cannot move a lane, so the walk is done.
	unmet := map[string]bool{}
	for _, tool := range tools {
		unmet[tool] = true
	}
	for _, t := range targets {
		pf, err := preflightRemote(*t, tools)
		if err != nil {
			// The host answered and the answer was a failure. Re-trying
			// elsewhere in the hope of a different one is how a real failure
			// gets laundered into a pass.
			pool.Failed = t
			return pool, err
		}
		if pf.Fallback {
			// Recorded per candidate rather than only at the end: "tried A,
			// using B" is the fact that makes two runs' numbers comparable or
			// not.
			pool.Notices = append(pool.Notices, pf.Why)
			pool.Fallback = true
			pool.Why = pf.Why
			continue
		}
		pool.Candidates = append(pool.Candidates, poolCandidate{Target: t, Ready: pf.Ready})
		for _, tool := range tools {
			if !containsTool(pf.Ready.Missing, tool) {
				delete(unmet, tool)
			}
		}
		if len(unmet) == 0 {
			// Every probed tool is on some reachable candidate. Another round
			// trip cannot change where any lane lands.
			break
		}
	}
	if len(pool.Candidates) > 0 {
		// Something answered, so the run is not coming home. The last skip's
		// reason stays in Notices, where it reads as the skip it was rather
		// than as a whole-run fallback that did not happen.
		pool.Fallback = false
		pool.Why = ""
		return pool, nil
	}
	// Nothing answered. The final failure is the fallback reason the caller
	// prints, so it must not ALSO be narrated as a skip — a single unreachable
	// host would otherwise say the same thing twice.
	if n := len(pool.Notices); n > 0 {
		pool.Notices = pool.Notices[:n-1]
	}
	return pool, nil
}

// containsTool reports whether tool is in list. Linear on purpose: a lane's
// toolchain is two or three binaries, and a map here would cost more to build
// than the scan it replaces.
func containsTool(list []string, tool string) bool {
	for _, t := range list {
		if t == tool {
			return true
		}
	}
	return false
}

// pickRemoteTarget picks the host a run would use from the authorized
// candidates, and prints NOTHING.
//
// It is the whole-run view of the pool above: the first candidate that
// answered, in declared order. A run with no lanes — or one whose lanes all
// share the chosen host's toolchain — needs no more than this, and it is the
// host every non-lane surface reports.
//
// The announcements come back as data rather than going to stdout, because
// `dross test lane preview --json` has to be able to ask this exact question
// and still emit a bare document.
func pickRemoteTarget(targets []*remote.Target, tools []string) (*remote.Target, preflight, []string, error) {
	pool, err := probeRemotePool(targets, tools)
	if err != nil {
		return pool.Failed, preflight{}, pool.Notices, err
	}
	if len(pool.Candidates) == 0 {
		if pool.Fallback {
			return nil, preflight{Fallback: true, Why: pool.Why}, pool.Notices, nil
		}
		return nil, preflight{}, pool.Notices, nil
	}
	chosen := pool.Candidates[0]
	notices := pool.Notices
	if chosen.Target != targets[0] {
		// The swap must be visible, or two runs' numbers are silently
		// incomparable.
		notices = append(notices, "using "+chosen.Target.Host)
	}
	return chosen.Target, preflight{Ready: chosen.Ready}, notices, nil
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
