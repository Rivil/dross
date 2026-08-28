package cmd

// Per-lane locality: which machine each matched lane actually runs on.
//
// The host for a run is chosen once (selectRemoteTarget), and until now that
// choice was the whole answer — every lane went wherever the run went. That is
// wrong the moment a granted host holds one lane's toolchain and not another's:
// a `pnpm` lane sent to a host with no node reports `command not found`, which
// arrives at the caller as exit 1, which reads as "your code is broken". The
// run measured nothing about the code and said the opposite.
//
// So locality is decided PER LANE, from one probe of the union of every
// matched lane's toolchain, before the tree is pushed. A lane whose tools the
// host lacks runs here instead and reports its own suite result; a lane whose
// tools are missing from BOTH machines does not spawn at all and takes its own
// exit code rather than being laundered into a red suite (locked
// local_absence).

import (
	"fmt"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/testlane"
)

// laneSite is where one lane ends up.
type laneSite int

const (
	// siteRemote: the granted host has this lane's toolchain; it runs there.
	siteRemote laneSite = iota
	// siteLocal: this lane runs on this machine — either because there is no
	// remote in play at all, or because the remote lacks its toolchain.
	siteLocal
	// siteRefused: neither machine can run it. It does not spawn, and it
	// carries exitToolchainMissing rather than a suite failure.
	siteRefused
)

// laneVerdict is one lane's locality decision.
type laneVerdict struct {
	// Site is where the lane runs, or that it does not.
	Site laneSite
	// Announce is the line to print BEFORE the lane runs, empty when there is
	// nothing to announce. It is only ever set for a toolchain fallback: a
	// lane that went where the run went needs no explanation, and a run with
	// no remote at all never fell back from anything.
	Announce string
	// Err is the refusal, set only for siteRefused. It carries
	// exitToolchainMissing.
	Err error
}

// laneToolUnion is every tool the given lanes need, deduped, in lane order.
//
// One list for one probe. The probe is an ssh round trip per tool, so asking
// per lane would pay for the shared `go` of three Go lanes three times — and
// worse, would spread the questions across the run, which is how a lane
// discovers a missing binary mid-flight after the tree has already been pushed
// (c-4).
//
// Exported to doctor's Remote section too (c-8), because doctor and the run
// must never disagree about what the host has: two derivations of the same
// question drift, and the drift shows up as doctor passing on a host the run
// then falls back from.
func laneToolUnion(lanes []project.TestLane) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, lane := range lanes {
		for _, tool := range testlane.Toolchain(lane.Command, lane.Prepare, lane.Toolchain) {
			if seen[tool] {
				continue
			}
			seen[tool] = true
			out = append(out, tool)
		}
	}
	return out
}

// laneLocality decides where each lane runs, from one probe's answer.
//
// Pure: it asks nothing over a network and reads no configuration. `missing` is
// what the single preflight probe already reported for the union, and lookPath
// is this machine's resolver — injected rather than called directly so the
// local-absence rule is reachable from a test without the test's outcome
// depending on what happens to be installed on the machine running it.
//
// host is the granted host's name, empty when the run is local anyway (--local,
// or no grant, or a transport fallback). That emptiness is the c-5 split: a
// host that was never reached told us nothing about its toolchain, so no lane
// may print a toolchain fallback line for it — the transport fallback already
// said, once, that the whole run came home.
//
// Local absence is consulted on EVERY path, including the local-anyway one
// (c-9). A missing binary is not a red suite wherever the run was headed, and a
// lane that spawns `pnpm test` into a machine without pnpm produces exactly the
// misleading exit 1 this file exists to prevent.
func laneLocality(lanes []matchedLane, host string, missing []string, lookPath func(string) (string, error)) []laneVerdict {
	gone := map[string]bool{}
	for _, tool := range missing {
		gone[tool] = true
	}
	out := make([]laneVerdict, 0, len(lanes))
	for _, m := range lanes {
		out = append(out, oneLaneLocality(m.lane, host, gone, lookPath))
	}
	return out
}

// oneLaneLocality is laneLocality for a single lane.
func oneLaneLocality(lane project.TestLane, host string, gone map[string]bool, lookPath func(string) (string, error)) laneVerdict {
	tools := testlane.Toolchain(lane.Command, lane.Prepare, lane.Toolchain)

	// The prepare's tool and the command's tool are ONE requirement set
	// (locked prepare_toolchain): prepare_locality already pins the bootstrap
	// to its command's host, so a split would bootstrap one machine for a
	// suite that ran on another.
	absent := absentTools(tools, gone)
	if host != "" && len(absent) == 0 {
		return laneVerdict{Site: siteRemote}
	}

	// From here the lane runs here, or nowhere. Every tool is checked against
	// this machine before it is allowed to spawn — falling back into a host
	// that also lacks the binary produces the same misleading red, just on the
	// other side of the wire.
	for _, tool := range tools {
		if _, err := lookPath(tool); err == nil {
			continue
		}
		// "neither host" only when the remote genuinely answered and lacked it
		// too. Claiming both machines are missing a tool the remote actually
		// has would send the reader to the wrong box.
		named := ""
		if gone[tool] {
			named = host
		}
		return laneVerdict{Site: siteRefused, Err: toolchainFailure(lane, tool, named)}
	}
	return laneVerdict{Site: siteLocal, Announce: laneFallbackLine(lane, host, absent)}
}

// absentTools is the lane's tools the probe reported missing, in lane order.
func absentTools(tools []string, gone map[string]bool) []string {
	out := []string{}
	for _, tool := range tools {
		if gone[tool] {
			out = append(out, tool)
		}
	}
	return out
}

// laneFallbackLine announces one lane coming home, or nothing.
//
// It names the lane, the binary and the host in ONE line, because all three are
// needed to act on it and a transcript is read in fragments: "running locally
// instead" without the binary is a fact with no remedy attached, and without
// the lane it cannot be attributed in a multi-lane run (c-2).
//
// Empty when there was no fallback — either no remote at all, or a remote that
// had everything. A line printed for a lane that went where the run went would
// make the transcript unreadable at exactly the point it matters.
func laneFallbackLine(lane project.TestLane, host string, absent []string) string {
	if host == "" || len(absent) == 0 {
		return ""
	}
	return fmt.Sprintf("lane %s fallback: %s has no %s — running this lane here instead",
		lane.Name, host, joinTools(absent))
}

// joinTools renders a tool list for a message. Comma-separated rather than
// space-separated so a two-tool gap does not read as one binary with an
// argument.
func joinTools(tools []string) string {
	out := ""
	for i, tool := range tools {
		if i > 0 {
			out += ", "
		}
		out += tool
	}
	return out
}

// toolchainFailure is the refusal for a lane no machine can run.
//
// It is deliberately NOT worded as a suite failure and does not carry
// exitSuiteFailed. That is the whole point of the code: a missing binary is a
// fact about the machines, and a caller who read it as a red suite would go
// looking for a bug in code that was never executed.
//
// host is the granted host when the probe found the tool missing there too, and
// empty otherwise. Both hosts are named in the first case (locked
// local_absence's "neither host" wording), because a message naming one side
// leaves the reader installing the binary on the machine that already has it.
func toolchainFailure(lane project.TestLane, tool, host string) error {
	if host == "" {
		return &ExitCodeError{Code: exitToolchainMissing, Err: fmt.Errorf(
			"test lane %q did not run: %s is not on this machine, so it measured nothing",
			lane.Name, tool)}
	}
	return &ExitCodeError{Code: exitToolchainMissing, Err: fmt.Errorf(
		"test lane %q did not run: neither host has %s — it is missing on %s and on this machine",
		lane.Name, tool, host)}
}

// runnableLanes is the lanes out of a matchedLane set, in order.
//
// The index a matchedLane carries is the key its selector paths live under, and
// nothing about a toolchain needs it — but the lane order does matter, because
// it is the order the probe reports missing tools in and the order a transcript
// is read in.
func runnableLanes(matched []matchedLane) []project.TestLane {
	out := make([]project.TestLane, 0, len(matched))
	for _, m := range matched {
		out = append(out, m.lane)
	}
	return out
}
