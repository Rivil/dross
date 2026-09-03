package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// probeScript fakes remoteProbeFn per host: a host named in unreachable fails
// as transport, a host in broken fails as a remote command, everything else is
// ready with the given core count and every tool it was asked for.
func probeScript(t *testing.T, unreachable, broken map[string]bool, cores int) *[]string {
	t.Helper()
	return probeScriptTools(t, unreachable, broken, nil, cores)
}

// probeScriptTools is probeScript with a per-host toolchain: has maps a host to
// the tools it actually holds, and anything asked for that is not listed comes
// back in Missing. A host absent from has holds everything — the shape the
// pre-affinity tests assume.
//
// It fakes the SAME seam doctor probes through, so a pool walk in a test asks
// the question a run asks. A second fake would drift from the real one exactly
// where the drift is invisible.
func probeScriptTools(t *testing.T, unreachable, broken map[string]bool, has map[string][]string, cores int) *[]string {
	t.Helper()
	var probed []string
	orig := remoteProbeFn
	remoteProbeFn = func(tg remote.Target, tools []string) (remote.Readiness, error) {
		probed = append(probed, tg.Host)
		if unreachable[tg.Host] {
			return remote.Readiness{}, fmt.Errorf("dial %s: %w", tg.Host, remote.ErrTransport)
		}
		if broken[tg.Host] {
			return remote.Readiness{}, fmt.Errorf("boom on %s: %w", tg.Host, remote.ErrRemoteCommand)
		}
		ready := remote.Readiness{Cores: cores}
		held, scripted := has[tg.Host]
		if !scripted {
			return ready, nil
		}
		for _, want := range tools {
			if !containsTool(held, want) {
				ready.Missing = append(ready.Missing, want)
			}
		}
		return ready, nil
	}
	t.Cleanup(func() { remoteProbeFn = orig })
	return &probed
}

func targets(hosts ...string) []*remote.Target {
	var out []*remote.Target
	for _, h := range hosts {
		out = append(out, &remote.Target{Host: h, Workdir: "/w"})
	}
	return out
}

// TestSelectSkipsUnreachableAndUsesTheNext is the feature: with the first host
// down, the work still happens remotely instead of falling back to local.
func TestSelectSkipsUnreachableAndUsesTheNext(t *testing.T) {
	probed := probeScript(t, map[string]bool{"a": true}, nil, 8)
	out := captureStdout(t, func() {
		got, pool, err := selectRemoteTarget(targets("a", "b"), nil)
		if err != nil {
			t.Fatalf("select: %v", err)
		}
		if got == nil || got.Host != "b" {
			t.Fatalf("chose %v, want b", got)
		}
		if pool.Fallback {
			t.Error("a usable host must not read as a fallback")
		}
	})
	if len(*probed) != 2 || (*probed)[0] != "a" {
		t.Errorf("probed %v, want a first then b — declared order is the preference", *probed)
	}
	// c-2: the swap must be visible, or two runs' numbers are silently
	// incomparable.
	if !strings.Contains(out, "a") || !strings.Contains(out, "b") {
		t.Errorf("the skip and the choice must both be announced, got: %s", out)
	}
}

// TestSelectStopsAtTheFirstReady: with nothing to ask for, a later host must not
// be probed once one has answered — probing costs a round trip and the order is
// a preference. This is the whole-suite run, which asks for no toolchain at all,
// and it must cost exactly what it cost before per-lane affinity existed.
func TestSelectStopsAtTheFirstReady(t *testing.T) {
	probed := probeScript(t, nil, nil, 4)
	got, _, err := selectRemoteTarget(targets("a", "b", "c"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "a" {
		t.Errorf("chose %s, want the first declared", got.Host)
	}
	if len(*probed) != 1 {
		t.Errorf("probed %v, want only the first", *probed)
	}
}

// TestSelectDoesNotSkipARemoteCommandFailure: a host that ANSWERED and failed
// is an answer. Trying elsewhere for a different one is how a real failure gets
// laundered into a pass.
func TestSelectDoesNotSkipARemoteCommandFailure(t *testing.T) {
	probed := probeScript(t, nil, map[string]bool{"a": true}, 4)
	_, _, err := selectRemoteTarget(targets("a", "b"), nil)
	if err == nil {
		t.Fatal("a remote-command failure must abort, not move to the next host")
	}
	if !errors.Is(err, remote.ErrRemoteCommand) {
		t.Errorf("err = %v, want the host's own failure", err)
	}
	if len(*probed) != 1 {
		t.Errorf("probed %v — the second host must not be tried", *probed)
	}
}

// TestSelectAllUnreachableFallsBackLocally: a pool where nothing answers reads
// exactly like a single host that did not.
func TestSelectAllUnreachableFallsBackLocally(t *testing.T) {
	probeScript(t, map[string]bool{"a": true, "b": true}, nil, 4)
	captureStdout(t, func() {
		got, pool, err := selectRemoteTarget(targets("a", "b"), nil)
		if err != nil {
			t.Fatalf("an unreachable pool must fall back, not error: %v", err)
		}
		if got != nil {
			t.Errorf("chose %v with nothing reachable", got)
		}
		if !pool.Fallback || pool.Why == "" {
			t.Errorf("the fallback must carry a reason: %+v", pool)
		}
	})
}

// TestSelectSingleHostIsUnchanged pins c-3: one candidate must behave exactly
// as it did before the pool existed, including printing nothing extra.
func TestSelectSingleHostIsUnchanged(t *testing.T) {
	probeScript(t, nil, nil, 12)
	out := captureStdout(t, func() {
		got, pool, err := selectRemoteTarget(targets("solo"), nil)
		if err != nil || got == nil || got.Host != "solo" {
			t.Fatalf("got %v/%v", got, err)
		}
		if pool.Candidates[0].Ready.Cores != 12 {
			t.Errorf("cores = %d, want the probe's answer carried through", pool.Candidates[0].Ready.Cores)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("a single-host run must print nothing extra, got: %q", out)
	}
}

func TestSelectNoCandidatesIsNotRemote(t *testing.T) {
	got, pool, err := selectRemoteTarget(nil, nil)
	if err != nil || got != nil || pool.Fallback {
		t.Errorf("no grant must mean no remote and no fallback narration: %v/%v/%+v", got, err, pool)
	}
}

// TestGrantAddKeepsTheExistingHost: --add must authorize an ADDITIONAL host,
// not replace the one already granted.
func TestGrantAddKeepsTheExistingHost(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, ".dross")

	if err := writeRemoteGrant(root, "first", "/w1"); err != nil {
		t.Fatal(err)
	}
	if err := appendRemoteGrant(root, "second", "/w2"); err != nil {
		t.Fatal(err)
	}
	got, err := readRemoteGrants(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Host != "first" || got[1].Host != "second" {
		t.Fatalf("grants = %v, want first then second — the scalar grant is candidate zero", hostsOf(got))
	}
}

// TestGrantAddIsIdempotent: re-adding the same host must not make it get
// probed twice.
func TestGrantAddIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, ".dross")
	for i := 0; i < 3; i++ {
		if err := appendRemoteGrant(root, "same", "/w"); err != nil {
			t.Fatal(err)
		}
	}
	if err := appendRemoteGrant(root, "same", "/w"); err != nil {
		t.Fatal(err)
	}
	got, err := readRemoteGrants(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("grants = %v, want one entry", hostsOf(got))
	}
}

// TestRevokeClearsThePool: leaving extra hosts behind would make "withdraw it"
// mean withdrawn-from-one-machine, which is worse than not revoking — the user
// believes nothing is authorized while a run still has somewhere to go.
func TestRevokeClearsThePool(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, ".dross")
	if err := writeRemoteGrant(root, "first", "/w1"); err != nil {
		t.Fatal(err)
	}
	if err := appendRemoteGrant(root, "second", "/w2"); err != nil {
		t.Fatal(err)
	}
	if err := writeRemoteGrant(root, "", ""); err != nil { // the revoke path
		t.Fatal(err)
	}
	got, err := readRemoteGrants(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("after revoke the pool still authorizes %v", hostsOf(got))
	}
}

func hostsOf(ts []*remote.Target) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Host)
	}
	return out
}

// TestPoolWalksPastAHostMissingATool is the feature: the walk must not stop at
// the first host that ANSWERS, because answering is no longer the whole
// question. A pool whose second machine alone has pnpm has to report that
// machine, or the lane needing it comes home for a tool the pool actually holds.
func TestPoolWalksPastAHostMissingATool(t *testing.T) {
	probed := probeScriptTools(t, nil, nil, map[string][]string{
		"a": {"go"},
		"b": {"pnpm"},
	}, 8)
	pool, err := probeRemotePool(targets("a", "b"), []string{"go", "pnpm"})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(*probed) != 2 {
		t.Fatalf("probed %v, want both — stopping at the first reachable host loses b's pnpm", *probed)
	}
	if len(pool.Candidates) != 2 {
		t.Fatalf("candidates = %v, want both reachable hosts", candidateHosts(pool))
	}
	if h := hostHolding(pool, "pnpm"); h != "b" {
		t.Errorf("pnpm resolves to %q, want b — the pool must report a candidate for every tool it holds", h)
	}
	if h := hostHolding(pool, "go"); h != "a" {
		t.Errorf("go resolves to %q, want a — declared order is the preference", h)
	}
}

// TestPoolStopsOnceEveryToolIsAccountedFor: the walk is widened, not made
// unconditional. Once a reachable candidate covers every probed tool, another
// round trip cannot change any lane's destination, so it must not be paid for.
func TestPoolStopsOnceEveryToolIsAccountedFor(t *testing.T) {
	probed := probeScriptTools(t, nil, nil, map[string][]string{
		"a": {"go", "pnpm"},
		"b": {"go", "pnpm"},
	}, 8)
	pool, err := probeRemotePool(targets("a", "b"), []string{"go", "pnpm"})
	if err != nil {
		t.Fatal(err)
	}
	if len(*probed) != 1 || (*probed)[0] != "a" {
		t.Errorf("probed %v, want only a — it already holds the whole union", *probed)
	}
	if len(pool.Candidates) != 1 || pool.Candidates[0].Target.Host != "a" {
		t.Errorf("candidates = %v, want a alone", candidateHosts(pool))
	}
}

// TestPoolProbesEachHostOnceForTheUnion pins locked
// one_probe_not_per_lane_probes: a two-lane run asks each candidate ONE
// question covering both lanes' tools. Probing per lane would multiply ssh
// round trips by the lane count on every gate run.
func TestPoolProbesEachHostOnceForTheUnion(t *testing.T) {
	var asked [][]string
	orig := remoteProbeFn
	remoteProbeFn = func(tg remote.Target, tools []string) (remote.Readiness, error) {
		asked = append(asked, append([]string{tg.Host}, tools...))
		return remote.Readiness{Cores: 4, Missing: []string{"pnpm"}}, nil
	}
	t.Cleanup(func() { remoteProbeFn = orig })

	if _, err := probeRemotePool(targets("a", "b"), []string{"go", "pnpm"}); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 2 {
		t.Fatalf("probe calls = %d, want one per candidate: %v", len(asked), asked)
	}
	for _, call := range asked {
		if len(call) != 3 || call[1] != "go" || call[2] != "pnpm" {
			t.Errorf("probe %v asked for something other than the whole union in one call", call)
		}
	}
}

// TestPoolSkipsAnUnreachableHostAndKeepsWalking: a dead FIRST host must not
// truncate the pool. Before per-lane affinity the walk stopped at the first
// answer, so a dead host cost one candidate; now it must cost none.
func TestPoolSkipsAnUnreachableHostAndKeepsWalking(t *testing.T) {
	probed := probeScriptTools(t, map[string]bool{"a": true}, nil, map[string][]string{
		"b": {"go"},
		"c": {"pnpm"},
	}, 8)
	pool, err := probeRemotePool(targets("a", "b", "c"), []string{"go", "pnpm"})
	if err != nil {
		t.Fatalf("an unreachable candidate must be skipped, not fatal: %v", err)
	}
	if len(*probed) != 3 {
		t.Errorf("probed %v, want all three — a dead first host must not end the walk", *probed)
	}
	if got := candidateHosts(pool); len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Errorf("candidates = %v, want b then c", got)
	}
	if pool.Fallback {
		t.Error("a pool with two reachable hosts must not read as a whole-run fallback")
	}
	// Announced, never silent (locked routing_is_announced_never_silent).
	if len(pool.Notices) != 1 || !strings.Contains(pool.Notices[0], "a") {
		t.Errorf("notices = %v, want the skip of a named", pool.Notices)
	}
}

// TestPoolCarriesEachCandidatesOwnMissing: the per-lane decision is made from
// this, so a candidate's Missing must be ITS OWN. Collapsing them would send a
// lane to a host that lacks its toolchain.
func TestPoolCarriesEachCandidatesOwnMissing(t *testing.T) {
	probeScriptTools(t, nil, nil, map[string][]string{
		"a": {"go"},
		"b": {"pnpm"},
	}, 8)
	pool, err := probeRemotePool(targets("a", "b"), []string{"go", "pnpm"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pool.Candidates) != 2 {
		t.Fatalf("candidates = %v", candidateHosts(pool))
	}
	if got := pool.Candidates[0].Ready.Missing; len(got) != 1 || got[0] != "pnpm" {
		t.Errorf("a is missing %v, want pnpm alone", got)
	}
	if got := pool.Candidates[1].Ready.Missing; len(got) != 1 || got[0] != "go" {
		t.Errorf("b is missing %v, want go alone", got)
	}
}

// TestPoolAllUnreachableReportsNoCandidates: with nothing reachable there is no
// candidate for any tool, and the run comes home with a reason — the same shape
// a single unreachable host has always had.
func TestPoolAllUnreachableReportsNoCandidates(t *testing.T) {
	probed := probeScriptTools(t, map[string]bool{"a": true, "b": true}, nil, nil, 4)
	pool, err := probeRemotePool(targets("a", "b"), []string{"go"})
	if err != nil {
		t.Fatalf("an unreachable pool must fall back, not error: %v", err)
	}
	if len(*probed) != 2 {
		t.Errorf("probed %v, want both tried", *probed)
	}
	if len(pool.Candidates) != 0 {
		t.Errorf("candidates = %v with nothing reachable", candidateHosts(pool))
	}
	if !pool.Fallback || pool.Why == "" {
		t.Errorf("the fallback must carry a reason: %+v", pool)
	}
	// The last failure IS the fallback reason the caller prints; narrating it
	// again as a skip would say the same thing twice.
	if len(pool.Notices) != 1 {
		t.Errorf("notices = %v, want only the non-final skip", pool.Notices)
	}
}

// TestPoolRemoteCommandFailureNamesTheHost: a host that ANSWERED and failed is
// an answer, and the walk stops there. The failing target comes back so the
// caller can name the machine.
func TestPoolRemoteCommandFailureNamesTheHost(t *testing.T) {
	probed := probeScriptTools(t, nil, map[string]bool{"a": true}, nil, 4)
	pool, err := probeRemotePool(targets("a", "b"), []string{"go"})
	if err == nil {
		t.Fatal("a remote-command failure must abort, not move to the next host")
	}
	if !errors.Is(err, remote.ErrRemoteCommand) {
		t.Errorf("err = %v, want the host's own failure", err)
	}
	if len(*probed) != 1 {
		t.Errorf("probed %v — the second host must not be tried", *probed)
	}
	if pool.Failed == nil || pool.Failed.Host != "a" {
		t.Errorf("failed target = %v, want a", pool.Failed)
	}
}

// candidateHosts names a pool's reachable candidates, in order.
func candidateHosts(p remotePool) []string {
	var out []string
	for _, c := range p.Candidates {
		out = append(out, c.Target.Host)
	}
	return out
}

// hostHolding is the first candidate that is NOT missing tool, or "".
func hostHolding(p remotePool, tool string) string {
	for _, c := range p.Candidates {
		if !containsTool(c.Ready.Missing, tool) {
			return c.Target.Host
		}
	}
	return ""
}
