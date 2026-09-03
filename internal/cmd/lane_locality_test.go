package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// lanesFor builds the matchedLane slice laneLocality takes, with indexes in
// declaration order — the shape runTestLanes hands it.
func lanesFor(lanes ...project.TestLane) []matchedLane {
	out := make([]matchedLane, 0, len(lanes))
	for i, lane := range lanes {
		out = append(out, matchedLane{index: i, lane: lane})
	}
	return out
}

// haveTools is a lookPath double: the named tools resolve, everything else does
// not. Injected rather than calling exec.LookPath, so the local-absence rule is
// exercised without the result depending on what happens to be installed on the
// machine running the suite.
func haveTools(tools ...string) func(string) (string, error) {
	present := map[string]bool{}
	for _, t := range tools {
		present[t] = true
	}
	return func(bin string) (string, error) {
		if present[bin] {
			return "/usr/bin/" + bin, nil
		}
		return "", exec.ErrNotFound
	}
}

// TestToolchainRankSitsBetweenPrepareAndRed is the one inequality the new code
// exists to hold. Above red because the lane never spawned, so reporting a
// neighbour's failing test would tell the user their code is broken while the
// lane that measured nothing stayed invisible; below prepare because a broken
// bootstrap is a fact about this repo's own line.
func TestToolchainRankSitsBetweenPrepareAndRed(t *testing.T) {
	if exitRank(exitToolchainMissing) <= exitRank(exitSuiteFailed) {
		t.Errorf("toolchain-missing ranks %d, not above red's %d — a red lane would hide a lane that never ran",
			exitRank(exitToolchainMissing), exitRank(exitSuiteFailed))
	}
	if exitRank(exitToolchainMissing) >= exitRank(exitPrepareFailed) {
		t.Errorf("toolchain-missing ranks %d, not below prepare's %d",
			exitRank(exitToolchainMissing), exitRank(exitPrepareFailed))
	}
}

// TestExitTaxonomyOrderSurvivesTheInsertion walks the whole chain rather than
// the new pair alone. Inserting a rank means renumbering the ones above it, and
// a renumber that collapsed two existing outcomes into the same rank would make
// worseOutcome's answer depend on argument order — which the new pair's own
// assertion would never notice.
func TestExitTaxonomyOrderSurvivesTheInsertion(t *testing.T) {
	chain := []struct {
		name string
		code int
	}{
		{"transport", exitTransport},
		{"partial", exitPartial},
		{"prepare", exitPrepareFailed},
		{"toolchain", exitToolchainMissing},
		{"red", exitSuiteFailed},
		{"refused", exitLaneRefused},
		{"nothing measured", exitNothingMeasured},
	}
	for i := 0; i+1 < len(chain); i++ {
		hi, lo := chain[i], chain[i+1]
		if exitRank(hi.code) <= exitRank(lo.code) {
			t.Errorf("%s (%d) ranks %d, want strictly above %s (%d) at %d",
				hi.name, hi.code, exitRank(hi.code), lo.name, lo.code, exitRank(lo.code))
		}
	}
	if exitRank(exitNothingMeasured) <= exitRank(0) {
		t.Error("nothing-measured does not outrank success — a run that measured nothing would report green")
	}
}

// TestWorseOutcomeKeepsTheLaneThatNeverRan: a lane that never spawned must not
// be hidden behind a neighbour's red. Both orders are asserted, because a
// worst-wins fold that got the comparison backwards passes one of them.
func TestWorseOutcomeKeepsTheLaneThatNeverRan(t *testing.T) {
	red := &ExitCodeError{Code: exitSuiteFailed, Err: errors.New("test lane \"go\" failed")}
	gone := toolchainFailure(project.TestLane{Name: "web", Command: "pnpm test"}, "pnpm", []string{"helicon"})

	for _, tc := range []struct {
		name string
		a, b error
	}{
		{"red first", red, gone},
		{"toolchain first", gone, red},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCode(worseOutcome(tc.a, tc.b)); got != exitToolchainMissing {
				t.Errorf("worseOutcome kept exit %d, want %d — the lane that never ran was hidden",
					got, exitToolchainMissing)
			}
		})
	}
}

// TestToolchainFailureNamesBothHosts is locked local_absence's wording. A
// message naming one side leaves the reader installing the binary on the
// machine that already has it, and one worded as a suite failure sends them
// looking for a bug in code no runner ever loaded.
func TestToolchainFailureNamesBothHosts(t *testing.T) {
	err := toolchainFailure(project.TestLane{Name: "web", Command: "pnpm test"}, "pnpm", []string{"helicon"})
	msg := err.Error()

	for _, want := range []string{"web", "pnpm", "helicon", "this machine", "neither"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not name %q: %s", want, msg)
		}
	}
	if strings.Contains(msg, "test suite failed") {
		t.Errorf("a missing binary is worded as a red suite: %s", msg)
	}
	if got := ExitCode(err); got != exitToolchainMissing {
		t.Errorf("exit = %d, want %d — a missing binary must not arrive as a failing gate", got, exitToolchainMissing)
	}
}

// TestToolchainFailureWithoutARemoteNamesOnlyThisMachine: on a --local or
// ungranted run there is no second host, and claiming "neither host" would
// invent one the user never granted.
func TestToolchainFailureWithoutARemoteNamesOnlyThisMachine(t *testing.T) {
	msg := toolchainFailure(project.TestLane{Name: "web"}, "pnpm", nil).Error()
	if !strings.Contains(msg, "this machine") || !strings.Contains(msg, "pnpm") {
		t.Errorf("the local refusal does not name the tool and this machine: %s", msg)
	}
	if strings.Contains(msg, "neither") {
		t.Errorf("a run with no remote claims two hosts: %s", msg)
	}
}

// TestLaneLocalityIsPerLane is c-3 at the decision itself: one probe answer,
// two lanes, two different destinations. A per-run decision passes any test
// that looks at one lane at a time.
func TestLaneLocalityIsPerLane(t *testing.T) {
	lanes := lanesFor(
		project.TestLane{Name: "go", Command: "go test ./..."},
		project.TestLane{Name: "web", Command: "pnpm test"},
	)
	got := laneLocality(lanes, singleLaneCandidate("helicon", []string{"pnpm"}), haveTools("go", "pnpm"))

	if got[0].Site != siteRemote {
		t.Errorf("the go lane went %v, want remote — the host has go", got[0].Site)
	}
	if got[1].Site != siteLocal {
		t.Errorf("the web lane went %v, want local — the host has no pnpm", got[1].Site)
	}
	if got[0].Announce != "" {
		t.Errorf("a lane that went where the run went announced a fallback: %q", got[0].Announce)
	}
}

// TestFallbackLineNamesLaneBinaryAndHost is c-2. All three in ONE line: a
// transcript is read in fragments, and "running locally instead" without the
// binary is a fact with no remedy attached.
func TestFallbackLineNamesLaneBinaryAndHost(t *testing.T) {
	got := laneLocality(
		lanesFor(project.TestLane{Name: "web", Command: "pnpm test"}),
		singleLaneCandidate("helicon", []string{"pnpm"}), haveTools("pnpm"))

	line := got[0].Announce
	if line == "" {
		t.Fatal("a fallen-back lane announced nothing — it is indistinguishable from one that ran remotely")
	}
	if strings.Contains(strings.TrimSuffix(line, "\n"), "\n") {
		t.Errorf("the announcement spans more than one line: %q", line)
	}
	for _, want := range []string{"web", "pnpm", "helicon"} {
		if !strings.Contains(line, want) {
			t.Errorf("the announcement does not name %q: %q", want, line)
		}
	}
}

// TestPrepareToolSendsTheWholeLaneLocal is locked prepare_toolchain. The
// prepare's tool and the command's tool are one requirement set — a lane whose
// bootstrap ran on the remote for a suite that ran here would bootstrap the
// wrong machine.
func TestPrepareToolSendsTheWholeLaneLocal(t *testing.T) {
	lane := project.TestLane{Name: "web", Command: "node run.js", Prepare: "pnpm install"}
	got := laneLocality(lanesFor(lane), singleLaneCandidate("helicon", []string{"pnpm"}), haveTools("node", "pnpm"))

	if got[0].Site != siteLocal {
		t.Errorf("a lane whose PREPARE tool is missing went %v, want local in full", got[0].Site)
	}
	if !strings.Contains(got[0].Announce, "pnpm") {
		t.Errorf("the fallback does not name the prepare's binary: %q", got[0].Announce)
	}
}

// TestNeitherHostRefusesRatherThanFallingBack is locked local_absence. Falling
// back into a machine that also lacks the tool produces the failing gate c-1
// exists to prevent, just on the other side of the wire.
func TestNeitherHostRefusesRatherThanFallingBack(t *testing.T) {
	got := laneLocality(
		lanesFor(project.TestLane{Name: "web", Command: "pnpm test"}),
		singleLaneCandidate("helicon", []string{"pnpm"}), haveTools())

	if got[0].Site != siteRefused {
		t.Fatalf("a lane no machine can run went %v, want refused", got[0].Site)
	}
	if code := ExitCode(got[0].Err); code != exitToolchainMissing {
		t.Errorf("the refusal carries exit %d, want %d", code, exitToolchainMissing)
	}
	if !strings.Contains(got[0].Err.Error(), "neither") {
		t.Errorf("the refusal does not say both hosts lack it: %v", got[0].Err)
	}
}

// TestLocalRunStillConsultsLookPath is c-9. A missing binary is not a red suite
// wherever the run was headed, so --local and an ungranted repo take the same
// refusal rather than spawning `pnpm test` into a machine without pnpm.
func TestLocalRunStillConsultsLookPath(t *testing.T) {
	asked := []string{}
	look := func(bin string) (string, error) {
		asked = append(asked, bin)
		return haveTools("go")(bin)
	}
	got := laneLocality(
		lanesFor(
			project.TestLane{Name: "go", Command: "go test ./..."},
			project.TestLane{Name: "web", Command: "pnpm test"},
		),
		nil, look)

	if len(asked) == 0 {
		t.Fatal("a run with no remote consulted lookPath for nothing")
	}
	if got[0].Site != siteLocal {
		t.Errorf("the go lane went %v, want local — this machine has go", got[0].Site)
	}
	if got[1].Site != siteRefused {
		t.Fatalf("the web lane went %v, want refused — this machine has no pnpm", got[1].Site)
	}
	if code := ExitCode(got[1].Err); code != exitToolchainMissing {
		t.Errorf("the local refusal carries exit %d, want %d — a missing tool must never read as a red suite",
			code, exitToolchainMissing)
	}
	if got[0].Announce != "" || got[1].Announce != "" {
		t.Errorf("a run that never had a remote announced a fallback: %q %q", got[0].Announce, got[1].Announce)
	}
}

// TestUnreachableHostPrintsNoToolchainLine is the c-5 split at the decision. A
// host that was never reached told us nothing about its toolchain: the run
// arrives here with an empty host, and no lane may claim a binary is absent
// from a machine that never answered.
func TestUnreachableHostPrintsNoToolchainLine(t *testing.T) {
	got := laneLocality(
		lanesFor(project.TestLane{Name: "web", Command: "pnpm test"}),
		nil, haveTools("pnpm"))

	if got[0].Site != siteLocal {
		t.Errorf("the lane went %v, want local", got[0].Site)
	}
	if got[0].Announce != "" {
		t.Errorf("a lane blamed a host that was never reached: %q", got[0].Announce)
	}
}

// TestLaneToolUnionDedupes: the union is what the probe asks, one `command -v`
// per entry over ssh. Two Go lanes asking `go` twice doubles the round trips a
// run pays for before it has measured anything.
func TestLaneToolUnionDedupes(t *testing.T) {
	got := laneToolUnion([]project.TestLane{
		{Name: "go", Command: "go test ./...", Prepare: "go build ./..."},
		{Name: "cmd", Command: "go test ./internal/cmd/..."},
		{Name: "web", Command: "pnpm test", Prepare: "make deps"},
	})
	want := []string{"go", "pnpm", "make"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("union = %v, want %v — deduped, command token before prepare token, in lane order", got, want)
	}
}

// TestLaneToolUnionHonoursTheOverride: the union is what gets probed, so a lane
// whose override says `mise` must not also cost a probe for the `go` it wraps.
func TestLaneToolUnionHonoursTheOverride(t *testing.T) {
	got := laneToolUnion([]project.TestLane{
		{Name: "go", Command: "go test ./...", Toolchain: []string{"mise"}},
	})
	if fmt.Sprint(got) != fmt.Sprint([]string{"mise"}) {
		t.Errorf("union = %v, want [mise] — the override replaces the derived token", got)
	}
}

// TestFallbackLineOffersTheLaneScopedInstall is c-1: the remedy reaches the
// user at the moment the fallback is paid for, rather than sitting in a doc
// they would have to already know to go and read.
//
// One Announce, carrying that lane's OWN name — in a multi-lane run an offer
// attributable to no lane is an offer the reader cannot act on.
func TestFallbackLineOffersTheLaneScopedInstall(t *testing.T) {
	got := laneLocality(
		lanesFor(project.TestLane{Name: "web", Command: "pnpm test"}),
		singleLaneCandidate("helicon", []string{"pnpm"}), haveTools("pnpm"))

	line := got[0].Announce
	if line == "" {
		t.Fatal("a fallen-back lane announced nothing")
	}
	if !strings.Contains(line, "helicon has no pnpm") {
		t.Errorf("the fallback fact was lost: %q", line)
	}
	if !strings.Contains(line, "dross test lane install web") {
		t.Errorf("the fallback names no remedy: %q", line)
	}
}

// TestFallbackOfferIsLaneScopedNotWholeHost is locked offer_scope. The offer's
// blast radius must match what the transcript justified: one lane fell back, so
// the command on offer installs one lane's tool. `dross remote bootstrap` would
// provision tools for lanes and adapters this run never touched.
func TestFallbackOfferIsLaneScopedNotWholeHost(t *testing.T) {
	got := laneLocality(
		lanesFor(project.TestLane{Name: "web", Command: "pnpm test"}),
		singleLaneCandidate("helicon", []string{"pnpm"}), haveTools("pnpm"))

	if strings.Contains(got[0].Announce, "dross remote bootstrap") {
		t.Errorf("the offer widened to the whole host: %q", got[0].Announce)
	}
}

// TestFallbackOfferIsBare: --apply in the offer would install on sight for a
// user who pasted the line, which is the one thing the dry-run default exists
// to prevent. The verb's own dry run ends with the --apply hint, so the user
// reads what it would do before authorizing it.
func TestFallbackOfferIsBare(t *testing.T) {
	got := laneLocality(
		lanesFor(project.TestLane{Name: "web", Command: "pnpm test"}),
		singleLaneCandidate("helicon", []string{"pnpm"}), haveTools("pnpm"))

	if strings.Contains(got[0].Announce, "--apply") {
		t.Errorf("the offered verb carries --apply: %q", got[0].Announce)
	}
}

// TestFallbackOfferKeepsTheSilentArmsSilent: the offer must not turn either
// no-fallback case into a line. A lane that went where the run went, and a run
// with no remote at all, both have nothing to remedy — and an offer printed
// there teaches the reader that the line carries no information.
func TestFallbackOfferKeepsTheSilentArmsSilent(t *testing.T) {
	lanes := lanesFor(project.TestLane{Name: "web", Command: "pnpm test"})

	t.Run("lane went remote", func(t *testing.T) {
		got := laneLocality(lanes, singleLaneCandidate("helicon", nil), haveTools("pnpm"))
		if got[0].Announce != "" {
			t.Errorf("a lane that went remote announced: %q", got[0].Announce)
		}
	})

	t.Run("no remote granted", func(t *testing.T) {
		got := laneLocality(lanes, nil, haveTools("pnpm"))
		if got[0].Announce != "" {
			t.Errorf("a run with no granted host announced: %q", got[0].Announce)
		}
	})
}

// twoHosts is the affinity fixture: a first candidate holding `go` and not
// `pnpm`, a second holding `pnpm` and not `go`. It is the shape the whole
// phase exists for — one pool, two toolchains, no machine with both.
func twoHosts() []laneCandidate {
	return []laneCandidate{
		{Host: "alpha", Missing: []string{"pnpm"}},
		{Host: "beta", Missing: []string{"go"}},
	}
}

// TestLanePrefersACandidateThatHasItsToolchain is c-1. Before this, a lane
// whose tool the chosen host lacked came home — even when the pool held a
// machine that had it. The run was then slower AND measured on the wrong
// machine, for a tool that was one candidate away.
func TestLanePrefersACandidateThatHasItsToolchain(t *testing.T) {
	got := laneLocality(
		lanesFor(project.TestLane{Name: "web", Command: "pnpm test"}),
		twoHosts(), haveTools("pnpm"))

	// This machine HAS pnpm, so a walk that stopped at the first candidate
	// produces a perfectly plausible local run — which is why the regression
	// has to be asserted as the destination, not as an error.
	if got[0].Site != siteRemote || got[0].Host != "beta" {
		t.Fatalf("the lane went to %q (%v), want remote on beta — alpha has no pnpm and beta does",
			got[0].Host, got[0].Site)
	}
}

// TestTwoLanesLandOnDifferentHosts is the same rule stated as the outcome that
// makes it worth having: one invocation, two lanes, two machines. A decision
// that put both on one host passes every single-lane assertion.
func TestTwoLanesLandOnDifferentHosts(t *testing.T) {
	got := laneLocality(
		lanesFor(
			project.TestLane{Name: "go", Command: "go test ./..."},
			project.TestLane{Name: "web", Command: "pnpm test"},
		),
		twoHosts(), haveTools("go", "pnpm"))

	if got[0].Site != siteRemote || got[0].Host != "alpha" {
		t.Errorf("the go lane went to %q (%v), want alpha", got[0].Host, got[0].Site)
	}
	if got[1].Site != siteRemote || got[1].Host != "beta" {
		t.Errorf("the web lane went to %q (%v), want beta", got[1].Host, got[1].Site)
	}
	if got[0].Host == got[1].Host {
		t.Errorf("both lanes landed on %q — the pool's second toolchain went unused", got[0].Host)
	}
}

// TestAMovedLaneIsAnnouncedWithItsReason is locked
// routing_is_announced_never_silent. Two runs measured on different machines
// are not interchangeable evidence, so a lane that landed elsewhere has to say
// so — with the binary that decided it, or the reader has a host name and no
// way to tell whether it was a choice or a fault.
func TestAMovedLaneIsAnnouncedWithItsReason(t *testing.T) {
	got := laneLocality(
		lanesFor(project.TestLane{Name: "web", Command: "pnpm test"}),
		twoHosts(), haveTools("pnpm"))

	line := got[0].Announce
	if line == "" {
		t.Fatal("a lane moved to a second candidate silently — the transcript reads as a single-host run")
	}
	for _, want := range []string{"web", "beta", "alpha", "pnpm"} {
		if !strings.Contains(line, want) {
			t.Errorf("the announcement does not name %q: %q", want, line)
		}
	}
	if strings.Contains(strings.TrimSuffix(line, "\n"), "\n") {
		t.Errorf("the announcement spans more than one line: %q", line)
	}
}

// TestALaneOnTheChosenHostStillAnnouncesNothing: the announcement is for a
// MOVE. A line printed for every lane would make the transcript unreadable at
// exactly the point a move matters.
func TestALaneOnTheChosenHostStillAnnouncesNothing(t *testing.T) {
	got := laneLocality(
		lanesFor(project.TestLane{Name: "go", Command: "go test ./..."}),
		twoHosts(), haveTools("go"))

	if got[0].Host != "alpha" {
		t.Fatalf("the go lane went to %q, want the first candidate", got[0].Host)
	}
	if got[0].Announce != "" {
		t.Errorf("a lane that went where the run went announced: %q", got[0].Announce)
	}
}

// TestLocalIsTheLastResortNotTheSecondOption: the pool is walked to the end
// before this machine is considered. A lane that came home while a granted
// candidate held its toolchain is the bug c-1 names.
func TestLocalIsTheLastResortNotTheSecondOption(t *testing.T) {
	candidates := []laneCandidate{
		{Host: "alpha", Missing: []string{"pnpm"}},
		{Host: "beta", Missing: []string{"pnpm"}},
		{Host: "gamma", Missing: nil},
	}
	got := laneLocality(
		lanesFor(project.TestLane{Name: "web", Command: "pnpm test"}),
		candidates, haveTools("pnpm"))

	if got[0].Site != siteRemote || got[0].Host != "gamma" {
		t.Errorf("the lane went to %q (%v), want gamma — it is the only candidate with pnpm",
			got[0].Host, got[0].Site)
	}
}

// TestNoCandidateHasItFallsBackNamingThePool: with the tool on no granted host
// the lane still comes home, and the line must not blame one machine for a gap
// the whole pool has — the reader would install pnpm on alpha and watch the
// lane come home again.
func TestNoCandidateHasItFallsBackNamingThePool(t *testing.T) {
	candidates := []laneCandidate{
		{Host: "alpha", Missing: []string{"pnpm"}},
		{Host: "beta", Missing: []string{"pnpm"}},
	}
	got := laneLocality(
		lanesFor(project.TestLane{Name: "web", Command: "pnpm test"}),
		candidates, haveTools("pnpm"))

	if got[0].Site != siteLocal {
		t.Fatalf("the lane went %v, want local — no candidate has pnpm", got[0].Site)
	}
	line := got[0].Announce
	for _, want := range []string{"alpha", "beta", "pnpm", "dross test lane install web"} {
		if !strings.Contains(line, want) {
			t.Errorf("the fallback does not name %q: %q", want, line)
		}
	}
}

// TestRefusedSurvivesThePoolWalk is locked local_absence with more than one
// candidate. A tool on no granted host AND not here must still refuse rather
// than spawn — the refusal is the whole reason a missing binary does not arrive
// as a failing gate.
func TestRefusedSurvivesThePoolWalk(t *testing.T) {
	candidates := []laneCandidate{
		{Host: "alpha", Missing: []string{"pnpm"}},
		{Host: "beta", Missing: []string{"pnpm"}},
	}
	got := laneLocality(
		lanesFor(project.TestLane{Name: "web", Command: "pnpm test"}),
		candidates, haveTools())

	if got[0].Site != siteRefused {
		t.Fatalf("a lane no machine can run went %v, want refused", got[0].Site)
	}
	if code := ExitCode(got[0].Err); code != exitToolchainMissing {
		t.Errorf("the refusal carries exit %d, want %d", code, exitToolchainMissing)
	}
	msg := got[0].Err.Error()
	for _, want := range []string{"alpha", "beta", "this machine", "pnpm"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not name %q: %s", want, msg)
		}
	}
}

// TestRefusalNamesOnlyTheHostsThatLackIt: a candidate that HAS the tool must
// not appear in the refusal. It cannot be the reason the lane did not run, and
// naming it sends the reader to install a binary that is already there.
func TestRefusalNamesOnlyTheHostsThatLackIt(t *testing.T) {
	candidates := []laneCandidate{
		{Host: "alpha", Missing: []string{"pnpm", "node"}},
		{Host: "beta", Missing: []string{"pnpm"}},
	}
	// The lane needs node then pnpm; alpha lacks both, beta lacks only pnpm,
	// and this machine has neither. The refusal is raised on the first tool
	// this machine lacks in lane order — node — which only alpha is missing.
	got := laneLocality(
		lanesFor(project.TestLane{Name: "web", Command: "node run.js", Prepare: "pnpm install"}),
		candidates, haveTools())

	if got[0].Site != siteRefused {
		t.Fatalf("the lane went %v, want refused", got[0].Site)
	}
	msg := got[0].Err.Error()
	if !strings.Contains(msg, "node") || !strings.Contains(msg, "alpha") {
		t.Errorf("the refusal does not name the tool and the host lacking it: %s", msg)
	}
	if strings.Contains(msg, "beta") {
		t.Errorf("the refusal blames beta, which has node — the reader is sent to the wrong box: %s", msg)
	}
}

// TestLaneCandidatesOfSkipsUnreachableCandidates: only hosts that ANSWERED may
// route a lane. A candidate that was never reached has an empty Missing, and
// reading that as "it has everything" would send every lane into a machine that
// is down.
func TestLaneCandidatesOfSkipsUnreachableCandidates(t *testing.T) {
	probeScriptTools(t, map[string]bool{"dead": true}, nil, map[string][]string{
		"live": {"go"},
	}, 8)
	pool, err := probeRemotePool(targets("dead", "live"), []string{"go"})
	if err != nil {
		t.Fatal(err)
	}
	got := laneCandidatesOf(pool)
	if len(got) != 1 || got[0].Host != "live" {
		t.Fatalf("candidates = %+v, want live alone — dead never answered", got)
	}
	if len(got[0].Missing) != 0 {
		t.Errorf("live is missing %v, want nothing", got[0].Missing)
	}
}

// TestSingleLaneCandidateIsUnchangedFromTheOneHostRun: the overwhelmingly
// common shape is still one granted host, and it must decide exactly as it did
// before the pool was walked — including the message wording, which names the
// host rather than "no granted host".
func TestSingleLaneCandidateIsUnchangedFromTheOneHostRun(t *testing.T) {
	got := laneLocality(
		lanesFor(project.TestLane{Name: "web", Command: "pnpm test"}),
		singleLaneCandidate("helicon", []string{"pnpm"}), haveTools("pnpm"))

	if got[0].Site != siteLocal {
		t.Fatalf("the lane went %v, want local", got[0].Site)
	}
	if !strings.Contains(got[0].Announce, "helicon has no pnpm") {
		t.Errorf("the single-host wording changed: %q", got[0].Announce)
	}
	if singleLaneCandidate("", nil) != nil {
		t.Error("an empty host produced a candidate — a machine that never answered must route nothing")
	}
}
