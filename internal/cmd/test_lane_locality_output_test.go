package cmd

// What a split run SAYS. laneLocality's tests are about the decision and the
// wiring tests are about which machine each lane reached; these are about the
// transcript — the only durable record of which host produced which numbers.
//
// Two runs measured on different candidates are not interchangeable evidence:
// different core counts, different toolchain versions. A run that moved a lane
// and said nothing leaves that invisible at exactly the moment it matters
// (locked routing_is_announced_never_silent, c-1, c-4).

import (
	"io"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// twoHostLaneFixture is a repo with a go lane and a web lane, and TWO granted
// hosts: alpha holds go and not pnpm, beta holds pnpm and not go.
//
// It is the shape the whole phase exists for. Neither machine can run both
// lanes, so a run that keeps every lane on one host has to send one of them
// home for a tool the pool is holding.
func twoHostLaneFixture(t *testing.T) *wireLog {
	t.Helper()
	filesFixture(t, goAndWebLanes)
	grantAllLanes(t)
	// Granting probes; the real seam is installed below.
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8}, nil
	})
	if err := runCmd(t, Remote(), "grant", "alpha", "/srv/dross"); err != nil {
		t.Fatalf("dross remote grant alpha: %v", err)
	}
	if err := runCmd(t, Remote(), "grant", "beta", "/srv/dross", "--add"); err != nil {
		t.Fatalf("dross remote grant beta --add: %v", err)
	}

	log := &wireLog{}
	fakeProbe(t, func(tgt remote.Target, tools []string) (remote.Readiness, error) {
		log.probed = append(log.probed, tgt.Host)
		held := map[string]bool{"go": true, "pnpm": false}
		if tgt.Host == "beta" {
			held = map[string]bool{"go": false, "pnpm": true}
		}
		r := remote.Readiness{Cores: 8}
		for _, tool := range tools {
			if !held[tool] {
				r.Missing = append(r.Missing, tool)
			}
		}
		return r, nil
	})
	log.spawnSeam(t)
	installSpawnRecorder(t, nil)
	return log
}

// wireLog records every remote leg with the host it named, so a test can ask
// which machine each lane actually reached rather than only that something
// went over the wire.
type wireLog struct {
	probed []string
	legs   []string // "<verb> <host>"
	lines  []string // the command line each ssh carried
}

func (l *wireLog) spawnSeam(t *testing.T) {
	t.Helper()
	orig := spawnRemote
	t.Cleanup(func() { spawnRemote = orig })
	spawnRemote = func(argv []string, stdin string, _, _ io.Writer) error {
		if len(argv) == 0 {
			return nil
		}
		host := ""
		for _, a := range argv {
			// The target appears as a bare host or as host:path, and never as
			// a flag — matched positionally rather than by index because the
			// argv builders differ between rsync and ssh.
			if strings.HasPrefix(a, "-") {
				continue
			}
			if a == "alpha" || a == "beta" || strings.HasPrefix(a, "alpha:") || strings.HasPrefix(a, "beta:") {
				host = strings.SplitN(a, ":", 2)[0]
			}
		}
		l.legs = append(l.legs, argv[0]+" "+host)
		if argv[0] == "ssh" {
			l.lines = append(l.lines, stdin)
		}
		return nil
	}
}

func (l *wireLog) has(leg string) bool {
	for _, e := range l.legs {
		if e == leg {
			return true
		}
	}
	return false
}

// hostRunning is the host whose ssh leg carried a line containing want.
func (l *wireLog) hostRunning(want string) string {
	for i, line := range l.lines {
		if !strings.Contains(line, want) {
			continue
		}
		// The nth ssh leg pairs with the nth recorded line.
		n := 0
		for _, e := range l.legs {
			if strings.HasPrefix(e, "ssh ") {
				if n == i {
					return strings.TrimPrefix(e, "ssh ")
				}
				n++
			}
		}
	}
	return ""
}

// TestSplitRunSendsEachLaneToTheHostThatHasItsToolchain is c-1 end to end: the
// go lane reaches alpha, the web lane reaches beta, and neither comes home for
// a tool the pool is holding.
func TestSplitRunSendsEachLaneToTheHostThatHasItsToolchain(t *testing.T) {
	log := twoHostLaneFixture(t)
	local := installSpawnRecorder(t, nil)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go", "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v\n%s", err, out)
	}
	if got := log.hostRunning("go test"); got != "alpha" {
		t.Errorf("the go lane ran on %q, want alpha: %v / %v", got, log.legs, log.lines)
	}
	if got := log.hostRunning("pnpm test"); got != "beta" {
		t.Errorf("the web lane ran on %q, want beta — it came home for a tool the pool holds: %v / %v",
			got, log.legs, log.lines)
	}
	if local.count() != 0 {
		t.Errorf("%d lane(s) ran here while the pool had both toolchains: %v", local.count(), local.lines)
	}
}

// TestSplitRunPushesTheTreeToEveryHostItUses: a lane measures the tree it
// finds, so a host running a lane against a checkout nothing pushed reports on
// the previous run's code — a green about something else.
func TestSplitRunPushesTheTreeToEveryHostItUses(t *testing.T) {
	log := twoHostLaneFixture(t)

	if err := runCmd(t, Test(), "--files", "internal/a.go", "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	for _, want := range []string{"rsync alpha", "rsync beta"} {
		if !log.has(want) {
			t.Errorf("no %q — a lane ran against a tree nobody pushed: %v", want, log.legs)
		}
	}
	// And exactly once each: a per-lane sync would re-push an unchanged
	// checkout for every lane that host runs.
	n := 0
	for _, e := range log.legs {
		if strings.HasPrefix(e, "rsync ") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("the tree was pushed %d time(s), want one per host in use: %v", n, log.legs)
	}
}

// TestMovedLaneNamesItsHostAndTheToolchainThatDecidedIt is c-1's announcement
// half. A host name with no reason cannot be told apart from a fault, and a
// reason with no host cannot be acted on.
func TestMovedLaneNamesItsHostAndTheToolchainThatDecidedIt(t *testing.T) {
	twoHostLaneFixture(t)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go", "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v\n%s", err, out)
	}
	line := lineContaining(t, out, "lane web -> ")
	for _, want := range []string{"web", "beta", "alpha", "pnpm"} {
		if !strings.Contains(line, want) {
			t.Errorf("the routing line does not name %q: %q", want, line)
		}
	}
}

// TestSplitRunIsFlaggedAsSplit is c-4. Without it a run measured across two
// machines reads exactly like one measured on a single host, and the two are
// indistinguishable after the fact — which is what makes their numbers
// silently incomparable.
func TestSplitRunIsFlaggedAsSplit(t *testing.T) {
	twoHostLaneFixture(t)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go", "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v\n%s", err, out)
	}
	line := lineContaining(t, out, "split across")
	for _, want := range []string{"go on alpha", "web on beta"} {
		if !strings.Contains(line, want) {
			t.Errorf("the split line does not say %q: %q", want, line)
		}
	}
}

// TestAnUnsplitRunIsNotFlagged: the flag must stay off the common path. A line
// printed for every run is a line nobody reads, and the moment it appears on a
// single-host run it stops carrying the one fact it exists to carry.
func TestAnUnsplitRunIsNotFlagged(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	log := &runLog{}
	log.probeSeam(t, nil, nil) // helicon has everything
	log.spawnSeam(t)
	installSpawnRecorder(t, nil)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go", "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v\n%s", err, out)
	}
	if strings.Contains(out, "split across") {
		t.Errorf("a single-host run was flagged as split:\n%s", out)
	}
	if strings.Contains(out, " -> ") {
		t.Errorf("a run where no lane moved announced a move:\n%s", out)
	}
}

// TestAMixedLocalAndRemoteRunIsSplitToo: "more than one machine" counts this
// one. The per-lane fallback line explains the lane that came home, not that
// the run's numbers now come from two places.
func TestAMixedLocalAndRemoteRunIsSplitToo(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	log := &runLog{}
	log.probeSeam(t, []string{"pnpm"}, nil) // helicon has go, not pnpm
	log.spawnSeam(t)
	installSpawnRecorder(t, nil)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go", "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v\n%s", err, out)
	}
	line := lineContaining(t, out, "split across")
	for _, want := range []string{"go on helicon", "web on this machine"} {
		if !strings.Contains(line, want) {
			t.Errorf("the split line does not say %q: %q", want, line)
		}
	}
}
