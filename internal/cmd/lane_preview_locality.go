package cmd

// Where previewed lanes WOULD run.
//
// The run answers this by probing the granted host and then committing: it
// syncs the tree, spawns, and reports what happened. Preview has to answer the
// same question and commit to nothing, which makes the honest answer
// three-valued rather than two — a host can be granted and reachable, granted
// and unasked, or granted and unreachable, and only the first of those proves
// anything about where a lane would land.
//
// The failure this file exists to prevent is the confident wrong answer.
// Reporting `local` for a host nobody probed claims a fallback the probe never
// proved; reporting `refused` for a lane whose tool is merely missing HERE
// convicts a machine that was never asked. Both would be exactly as wrong as a
// silent one, and harder to notice — so an unasked or unreachable host is
// rendered as `unresolved`, in as many words, with the configured host named.

import (
	"fmt"
	"strings"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/testlane"
)

// previewHostState is what preview knows about the host.
type previewHostState string

const (
	// hostNone: no grant at all. Every lane runs here, and that is a resolved
	// answer rather than an absent one.
	hostNone previewHostState = "none"
	// hostProbed: a granted host answered. Per-lane verdicts are real.
	hostProbed previewHostState = "probed"
	// hostUnprobed: a host is granted and --no-probe said not to ask.
	hostUnprobed previewHostState = "unprobed"
	// hostUnresolved: a host is granted and could not be reached. NOT a
	// failure — preview describes, and a dead network is a fact about the
	// host, not about the file set the user asked about.
	hostUnresolved previewHostState = "unresolved"
)

// previewLocality is the whole locality answer for one preview.
type previewLocality struct {
	// State classifies what the host told us, or that it was never asked.
	State previewHostState
	// Host is the granted host this answer is about, empty only for hostNone.
	Host string
	// Why is the reason the host is unresolved, empty in every other state.
	Why string
	// Lanes are the per-lane answers, in the order the lanes were given.
	Lanes []laneLocalityLine
}

// laneLocalityLine is one previewed lane's locality.
type laneLocalityLine struct {
	// Site is what laneLocality decided. Meaningful ONLY when Resolved is
	// true — in the unprobed and unresolved states it was computed against an
	// empty host, so a siteRefused there means "absent from this machine",
	// not "absent from both".
	Site laneSite
	// Resolved reports whether Site is a claim about where the lane would run.
	Resolved bool
	// Text is the rendered locality: the host's name, `local`, `refused`, or
	// the unresolved banner naming the host.
	Text string
	// Note is the fallback line — lane, binary, host and remedy — set only for
	// a PROVEN fallback. Empty when the host was never asked, because a
	// fallback nobody measured is not one to announce.
	Note string
	// Err is the toolchain refusal, carrying exitToolchainMissing. Set only in
	// the probed state: it is a verdict about two machines, and only a probe
	// can convict the second one.
	Err error
}

// previewHost resolves where the given lanes would run, without committing to
// a run.
//
// It reads the grants the run reads, walks them through the run's own
// pickRemoteTarget so preview lands on the GRANT the run would land on, and
// probes through preflightRemote — the same remoteProbeFn seam doctor and the
// run use. A second probe path would drift, and the drift would show up as
// preview naming one host while the run used another.
//
// It never syncs the tree. Locality is decided from a probe and from this
// machine's resolver, both of which the run also does before any transfer.
//
// The probe is gated: see probeBlockedBy, checked before pickRemoteTarget,
// because that is where the ssh is.
//
// The error return is for a REAL fault only — an unreadable local store, a
// malformed grant. An unreachable host comes back as hostUnresolved with a nil
// error, which is what lets preview honour locked preview_exit_status.
func previewHost(root, repoDir string, lanes []matchedLane, probe bool) (previewLocality, error) {
	targets, err := readRemoteGrants(root, repoDir)
	if err != nil {
		return previewLocality{}, err
	}
	if len(targets) == 0 {
		return previewResolved(hostNone, "", "", lanes, "", nil), nil
	}
	if !probe {
		// Not one connection. --no-probe's whole promise is the instant
		// offline read, and a probe "just to name the host" would break it
		// while proving nothing the configured name does not already say.
		return previewUnresolved(hostUnprobed, targets[0].Host, "not probed", lanes), nil
	}
	if blocked := probeBlockedBy(root, repoDir, lanes); blocked != "" {
		// The consent refusal, taken BEFORE pickRemoteTarget rather than
		// after: the probe is an ssh to a machine, so a refusal that arrived
		// afterwards would have already done the thing it was declining to
		// authorize. It is hostUnresolved rather than hostUnprobed because the
		// user did not ask for an offline read — they asked a question this
		// grant does not let preview answer, and the reason says which.
		return previewUnresolved(hostUnresolved, targets[0].Host, blocked, lanes), nil
	}

	chosen, pf, _, perr := pickRemoteTarget(targets, laneToolUnion(runnableLanes(lanes)))
	// The pool's own notices are dropped rather than printed. They belong to a
	// run's transcript, where they explain which machine produced the numbers;
	// preview produced no numbers, and the state below already says which host
	// it landed on and whether it answered.
	if perr != nil {
		// A host that RAN something and failed is still not a preview
		// failure. Preview describes: the fault is reported as the host being
		// unresolved, named, with the reason carried — turning it into a
		// non-zero exit would make a verb that measures nothing wireable as a
		// CI gate. (The probe itself IS a subprocess — that is why the branch
		// above refuses it for an ungranted lane — but it runs no test, and an
		// exit status is a verdict about tests.)
		//
		// The host named is the one pickRemoteTarget BAILED on, which it hands
		// back beside the error. It is not necessarily the last configured
		// grant: the walk stops at the failure rather than running on, so
		// naming targets[len(targets)-1] here would print a machine that was
		// never contacted, above a reason quoting a different machine's error.
		return previewUnresolved(hostUnresolved, chosen.Host, "did not answer: "+perr.Error(), lanes), nil
	}
	if chosen == nil {
		// pf.Why is deliberately NOT reused. It is the RUN's fallback wording
		// — "running locally instead" — and preview is not running anything,
		// so borrowing it would claim the very fallback this state exists to
		// refuse to claim.
		return previewUnresolved(hostUnresolved, targets[len(targets)-1].Host, "could not be reached", lanes), nil
	}
	return previewResolved(hostProbed, chosen.Host, "", lanes, chosen.Host, pf.Ready.Missing), nil
}

// probeBlockedBy returns the reason the host must not be contacted, or empty
// when it may be.
//
// The rule mirrors the run's, deliberately. runTestLanes resolves consent for
// every matched lane and RETURNS before resolveTestTarget when none of them is
// runnable, so neither a probe nor a transfer is ever paid for on behalf of
// lanes that were all refused. Preview lands in the same place: one granted
// lane justifies the connection, zero does not.
//
// It reads the grant through previewConsent — the same call the per-lane
// annotation uses — rather than being handed a verdict, so the annotation the
// user reads and the authority the probe runs under cannot disagree.
//
// A preview that matched no lane at all is NOT blocked: there is no ungranted
// lane on whose behalf the probe would be running, and naming the configured
// host for an empty file set is the answer this verb has always given.
func probeBlockedBy(root, repoDir string, lanes []matchedLane) string {
	if len(lanes) == 0 {
		return ""
	}
	var ungranted []string
	for _, ml := range lanes {
		if previewConsent(root, repoDir, ml.lane) == ConsentGranted {
			return ""
		}
		ungranted = append(ungranted, ml.lane.Name)
	}
	return fmt.Sprintf("not probed: no previewed lane is granted on this machine (%s) — read a lane's line with `dross trust --lane %s`",
		strings.Join(ungranted, ", "), ungranted[0])
}

// previewResolved builds the answer for a host that told us something — or for
// no host at all, which tells us just as much.
//
// laneLocality is called, never reimplemented: the run decides locality with it
// and a second implementation here would be a second answer to "where does this
// lane go", diverging on the first rule either gained.
func previewResolved(state previewHostState, host, why string, lanes []matchedLane, probedHost string, missing []string) previewLocality {
	out := previewLocality{State: state, Host: host, Why: why}
	for _, v := range laneLocality(lanes, singleLaneCandidate(probedHost, missing), laneLookPath) {
		line := laneLocalityLine{Site: v.Site, Resolved: true, Note: v.Announce, Err: v.Err}
		switch v.Site {
		case siteRemote:
			line.Text = probedHost
		case siteLocal:
			line.Text = "local"
		case siteRefused:
			line.Text = "refused"
		}
		out.Lanes = append(out.Lanes, line)
	}
	return out
}

// previewUnresolved builds the answer for a host that was never asked, or asked
// and silent.
//
// laneLocality still runs, with an EMPTY host, and its verdict is read for one
// thing only: whether this machine has the lane's tools. That is knowable
// without a network and worth saying — a lane whose binary is missing here is
// going to be a problem wherever the host turns out to be. But it is rendered
// under the unresolved banner and never as a refusal, and the verdict's error
// is deliberately dropped: exitToolchainMissing means "neither machine has it",
// and only a probe can convict the second machine.
//
// siteLocal is flattened for the mirror-image reason. A lane whose every tool
// resolves here would run locally IF the host were absent, and preview does not
// know that it is — printing `local` would claim a fallback nothing proved.
func previewUnresolved(state previewHostState, host, why string, lanes []matchedLane) previewLocality {
	out := previewLocality{State: state, Host: host, Why: why}
	banner := fmt.Sprintf("unresolved — %s %s", host, why)
	for _, m := range lanes {
		line := laneLocalityLine{Resolved: false, Text: banner}
		if absent := localAbsentTools(m.lane); len(absent) > 0 {
			// Named because it is true and actionable regardless of the host:
			// the tool is missing HERE, and saying so is not the same as
			// saying the lane cannot run.
			line.Text = fmt.Sprintf("%s; %s absent from this machine",
				banner, strings.Join(absent, " "))
		}
		out.Lanes = append(out.Lanes, line)
	}
	return out
}

// localAbsentTools is the lane's toolchain this machine cannot resolve, in lane
// order.
//
// It goes through laneLookPath — the seam the run's own local-absence rule uses
// — so a test can inject an answer and preview cannot disagree with the run
// about what this machine has.
func localAbsentTools(lane project.TestLane) []string {
	var out []string
	for _, tool := range testlane.Toolchain(lane.Command, lane.Prepare, lane.Toolchain) {
		if _, err := laneLookPath(tool); err != nil {
			out = append(out, tool)
		}
	}
	return out
}
