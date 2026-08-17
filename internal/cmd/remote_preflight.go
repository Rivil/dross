package cmd

import (
	"errors"
	"fmt"

	"github.com/Rivil/dross/internal/remote"
)

// preflight is what a pre-run probe learned about the granted host.
//
// It exists so a run finds out the host is unreachable BEFORE it pays for the
// transfer. Without it the discovery happens part-way through — after the tree
// has been pushed, with a report that is empty for reasons the run cannot
// distinguish from "nothing to measure".
type preflight struct {
	// Ready is the probe's answer when it got one: the host's core count and
	// the requested tools it lacks. Zero-valued on a fallback, because a host
	// that could not be reached told us nothing about its toolchain.
	Ready remote.Readiness
	// Fallback reports that the caller should run locally instead. It is set
	// ONLY for a transport failure — see preflightRemote.
	Fallback bool
	// Why is the human-readable reason for a fallback, naming the host. Empty
	// when Fallback is false. Callers must print it: a fallback the output does
	// not mention leaves a local result indistinguishable from a remote one.
	Why string
}

// preflightRemote probes t for tools before a run commits to using it.
//
// It goes through remoteProbeFn — the same seam doctor probes through — rather
// than calling remote.Probe directly. A preflight that asked a slightly
// different question would be the worst kind of green: doctor would pass on a
// host the run then fails on, which is the mid-run discovery this exists to
// prevent.
//
// The transport/remote-command split is the whole decision (locked
// fallback_policy):
//
//   - A TRANSPORT failure means we never got an answer. Falling back to the
//     local machine is strictly better than failing the run, because the local
//     machine can still produce one.
//   - A REMOTE COMMAND failure means the host ran something and it failed.
//     That IS an answer, and re-running it locally in the hope of a different
//     one is how a real failure gets laundered into a pass.
//
// A reachable host that is merely missing tools is neither: it comes back as a
// normal Readiness carrying Missing, and the caller decides what to do about
// the gap. Reporting it as a fallback would hide a fixable toolchain hole
// behind a silent local run.
//
// Nothing here writes. The fallback is per-run and never sticky — one flaky
// network minute must not retire a grant the user made deliberately.
func preflightRemote(t remote.Target, tools []string) (preflight, error) {
	ready, err := remoteProbeFn(t, tools)
	if err == nil {
		return preflight{Ready: ready}, nil
	}
	if errors.Is(err, remote.ErrTransport) {
		return preflight{
			Fallback: true,
			Why:      fmt.Sprintf("could not reach %s (%v) — running locally instead", t.Host, err),
		}, nil
	}
	return preflight{}, err
}
