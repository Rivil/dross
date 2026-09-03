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
	"strings"

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
	// Host is the candidate this lane goes to, set only for siteRemote. It is
	// not always the host the RUN chose: a lane whose toolchain the first
	// candidate lacks goes to one that has it, and the caller looks its target
	// up by this name. Empty for every other site.
	Host string
	// Announce is the line to print BEFORE the lane runs, empty when there is
	// nothing to announce. It is set for a toolchain fallback and for a MOVE
	// to a later candidate: a lane that went where the run went needs no
	// explanation, and a run with no remote at all never fell back from
	// anything — but a lane that landed somewhere else must never do so
	// silently (locked routing_is_announced_never_silent).
	Announce string
	// Err is the refusal, set only for siteRefused. It carries
	// exitToolchainMissing.
	Err error
}

// laneCandidate is one machine a lane may be routed to, as the decision sees
// it: a name and the probed tools it lacks.
//
// A name and a gap, not a remote.Target: the decision is about toolchains, and
// giving it a transport would let it start caring about workdirs and env. The
// caller maps the chosen name back to its target.
type laneCandidate struct {
	Host    string
	Missing []string
}

// laneCandidatesOf narrows a probed pool to what the locality decision needs.
//
// Only REACHABLE candidates appear, because only they told us anything: a host
// that never answered has an empty Missing, and reading that as "it has
// everything" would route a lane into a machine that is down.
func laneCandidatesOf(pool remotePool) []laneCandidate {
	out := make([]laneCandidate, 0, len(pool.Candidates))
	for _, c := range pool.Candidates {
		out = append(out, laneCandidate{Host: c.Target.Host, Missing: c.Ready.Missing})
	}
	return out
}

// singleLaneCandidate is the one-host candidate set, for callers that have
// resolved a single machine rather than a probed pool.
//
// An empty host produces NO candidates, not one nameless one: a host that was
// never reached told us nothing about its toolchain, and an empty Missing read
// as "it has everything" would route a lane into a machine that is down.
func singleLaneCandidate(host string, missing []string) []laneCandidate {
	if host == "" {
		return nil
	}
	return []laneCandidate{{Host: host, Missing: missing}}
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
// Pure: it asks nothing over a network and reads no configuration. `candidates`
// is what the single pre-sync pool probe already reported — every REACHABLE
// granted host and the union tools it lacks, in declared order — and lookPath
// is this machine's resolver, injected rather than called directly so the
// local-absence rule is reachable from a test without the test's outcome
// depending on what happens to be installed on the machine running it.
//
// An EMPTY candidate list is the run that has no remote in play at all:
// --local, no grant, or a transport fallback where nothing answered. That
// emptiness is the c-5 split — a host that was never reached told us nothing
// about its toolchain, so no lane may print a toolchain fallback line for it;
// the transport fallback already said, once, that the whole run came home.
//
// The list is walked rather than read at index zero. A lane whose toolchain the
// first candidate lacks goes to one that HAS it, because coming home for a tool
// the pool holds is a slower run measured on the wrong machine — and the pool
// was granted precisely so runs leave the laptop. Local is the last resort, not
// the second option (c-1).
//
// Local absence is consulted on EVERY path, including the local-anyway one
// (c-9). A missing binary is not a red suite wherever the run was headed, and a
// lane that spawns `pnpm test` into a machine without pnpm produces exactly the
// misleading exit 1 this file exists to prevent.
func laneLocality(lanes []matchedLane, candidates []laneCandidate, lookPath func(string) (string, error)) []laneVerdict {
	gone := make([]map[string]bool, 0, len(candidates))
	for _, c := range candidates {
		set := map[string]bool{}
		for _, tool := range c.Missing {
			set[tool] = true
		}
		gone = append(gone, set)
	}
	out := make([]laneVerdict, 0, len(lanes))
	for _, m := range lanes {
		out = append(out, oneLaneLocality(m.lane, candidates, gone, lookPath))
	}
	return out
}

// oneLaneLocality is laneLocality for a single lane.
func oneLaneLocality(lane project.TestLane, candidates []laneCandidate, gone []map[string]bool, lookPath func(string) (string, error)) laneVerdict {
	tools := testlane.Toolchain(lane.Command, lane.Prepare, lane.Toolchain)

	// The prepare's tool and the command's tool are ONE requirement set
	// (locked prepare_toolchain): prepare_locality already pins the bootstrap
	// to its command's host, so a split would bootstrap one machine for a
	// suite that ran on another.
	//
	// Declared order is the preference, so the first candidate holding the
	// whole set wins. Moving is announced, never silent: two runs measured on
	// different machines are not interchangeable evidence, so a transcript
	// that did not say which one produced the numbers would make that
	// invisible at exactly the moment it matters.
	for i, c := range candidates {
		if len(absentTools(tools, gone[i])) > 0 {
			continue
		}
		v := laneVerdict{Site: siteRemote, Host: c.Host}
		if i > 0 {
			v.Announce = laneRoutedLine(lane, c.Host, absentTools(tools, gone[0]), candidates[0].Host)
		}
		return v
	}

	// No candidate has the whole set. From here the lane runs here, or nowhere.
	// Every tool is checked against this machine before it is allowed to spawn
	// — falling back into a host that also lacks the binary produces the same
	// misleading red, just on the other side of the wire.
	absent := absentTools(tools, unionGone(gone))
	for _, tool := range tools {
		if _, err := lookPath(tool); err == nil {
			continue
		}
		// The hosts are named only when they genuinely answered and lacked it
		// too. Claiming a machine is missing a tool it actually has would send
		// the reader to the wrong box.
		named := []string{}
		for i, c := range candidates {
			if gone[i][tool] {
				named = append(named, c.Host)
			}
		}
		return laneVerdict{Site: siteRefused, Err: toolchainFailure(lane, tool, named)}
	}
	return laneVerdict{Site: siteLocal, Announce: laneFallbackLine(lane, hostsOfCandidates(candidates), absent)}
}

// unionGone is the tools missing from EVERY candidate that answered.
//
// The intersection, not the union of the maps: a tool one candidate lacks is
// not a gap in the pool if another has it. It is only ever consulted after the
// walk above failed to place the lane, so it names what the whole pool cannot
// do rather than what one machine cannot.
func unionGone(gone []map[string]bool) map[string]bool {
	if len(gone) == 0 {
		return map[string]bool{}
	}
	out := map[string]bool{}
	for tool := range gone[0] {
		everywhere := true
		for _, set := range gone[1:] {
			if !set[tool] {
				everywhere = false
				break
			}
		}
		if everywhere {
			out[tool] = true
		}
	}
	return out
}

// hostsOfCandidates names the reachable candidates, in declared order.
func hostsOfCandidates(candidates []laneCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.Host)
	}
	return out
}

// laneRoutedLine announces a lane going to a candidate other than the one the
// run chose.
//
// It names the lane, the host it went to, the host it did NOT go to and the
// binary that decided it — all four, because the point of the line is that the
// reader can tell two runs' numbers apart afterwards. "lane web ran remotely"
// is not that; a run whose lanes landed on two machines has to say which lane
// went where and why, or the transcript reads exactly like a single-host run
// (locked routing_is_announced_never_silent).
//
// absent is what the FIRST candidate lacked — the reason for the move. It is
// never empty at a call site, because a lane the first candidate could run
// never reaches one.
func laneRoutedLine(lane project.TestLane, host string, absent []string, from string) string {
	return fmt.Sprintf("lane %s -> %s: %s has no %s, and %s does — running this lane there",
		lane.Name, host, from, joinTools(absent), host)
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
// It names the lane, the binary, the host and the REMEDY in ONE line, because
// all four are needed to act on it and a transcript is read in fragments:
// "running locally instead" without the binary is a fact with no remedy
// attached, and without the lane it cannot be attributed in a multi-lane run.
//
// The remedy is the LANE-SCOPED invocation, not the whole-host bootstrap
// (locked offer_scope): one lane fell back, so the command on offer installs
// one lane's tool. Pointing at `dross remote bootstrap` would provision tools
// for lanes and adapters this run never touched on the strength of a single
// lane's fallback. It is offered BARE, without --apply — the verb's own dry run
// is what shows the user what it would do, and an offer that installed on sight
// would make "let me see" impossible to ask.
//
// Printed whether or not the tool turns out to be installable. The verb answers
// that question properly, with the tool named and the host's owner told what
// they must do; gating the offer here would mean re-deriving the install
// decision at a site that has not probed for it, and a fallback that mentioned
// no next step at all is the state this replaces.
//
// Empty when there was no fallback — either no remote at all, or a remote that
// had everything. A line printed for a lane that went where the run went would
// make the transcript unreadable at exactly the point it matters.
func laneFallbackLine(lane project.TestLane, hosts []string, absent []string) string {
	if len(hosts) == 0 || len(absent) == 0 {
		return ""
	}
	// One candidate keeps the wording it had — the overwhelmingly common shape
	// is still a single granted host, and a message that said "no granted
	// host" there would read as though a pool were configured. With several,
	// naming one would be false: the lane came home because NONE of them had
	// the binary.
	who := hosts[0]
	if len(hosts) > 1 {
		who = "no granted host (" + joinTools(hosts) + ")"
	}
	return fmt.Sprintf("lane %s fallback: %s has no %s — running this lane here instead (install it: dross test lane install %s)",
		lane.Name, who, joinTools(absent), lane.Name)
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
// hosts are the granted candidates whose probe found the tool missing there
// too, and empty when none did. Every machine is named in the first case
// (locked local_absence's "neither host" wording), because a message naming one
// side leaves the reader installing the binary on a machine that already has
// it — and with a pool, naming one of several is worse still.
func toolchainFailure(lane project.TestLane, tool string, hosts []string) error {
	if len(hosts) == 0 {
		return &ExitCodeError{Code: exitToolchainMissing, Err: fmt.Errorf(
			"test lane %q did not run: %s is not on this machine, so it measured nothing",
			lane.Name, tool)}
	}
	if len(hosts) == 1 {
		return &ExitCodeError{Code: exitToolchainMissing, Err: fmt.Errorf(
			"test lane %q did not run: neither host has %s — it is missing on %s and on this machine",
			lane.Name, tool, hosts[0])}
	}
	return &ExitCodeError{Code: exitToolchainMissing, Err: fmt.Errorf(
		"test lane %q did not run: no host has %s — it is missing on %s and on this machine",
		lane.Name, tool, joinTools(hosts))}
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

// plannedHosts names every machine at least one lane is going to, in the order
// the lanes declared them, without repeats.
//
// It is what decides the syncs. One lane is enough to earn a host its copy of
// the tree — the sync exists so a remote lane measures the code in hand rather
// than the previous run's, and skipping it would produce the worst outcome
// available: a green from code that is not the code in hand. Equally, a host no
// lane is going to gets nothing: paying for a transfer nothing reads is the
// cost the pre-sync decision exists to avoid.
func plannedHosts(plan []laneVerdict) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range plan {
		if v.Site != siteRemote || seen[v.Host] {
			continue
		}
		seen[v.Host] = true
		out = append(out, v.Host)
	}
	return out
}

// splitRunLine says, in one line, that this run's lanes landed on more than one
// machine — and which lane went where.
//
// Empty for a run that did not split, which is nearly all of them: a line
// printed unconditionally would be noise on the common path, and noise on the
// common path is how a line stops being read. But a split run MUST say so
// (c-4): different hosts have different core counts and toolchain versions, so
// two runs of the same phase measured across different candidates are not
// interchangeable evidence, and a transcript that read identically to a
// single-host run would hide that at exactly the moment it matters.
//
// "More than one machine" counts this one. A run with a lane on a host and a
// lane here is split too, and the per-lane fallback line explains only the
// lane that came home — not that the run's numbers now come from two places.
func splitRunLine(lanes []plannedLane, plan []laneVerdict) string {
	seen := map[string]bool{}
	var where []string
	for i, v := range plan {
		site := "this machine"
		if v.Site == siteRemote {
			site = v.Host
		} else if v.Site == siteRefused {
			// A lane that never spawned produced no numbers, so it is not one
			// of the places this run was measured.
			continue
		}
		seen[site] = true
		where = append(where, fmt.Sprintf("%s on %s", lanes[i].lane.Name, site))
	}
	if len(seen) < 2 {
		return ""
	}
	return fmt.Sprintf("this run is split across %d machines — %s", len(seen), strings.Join(where, ", "))
}
