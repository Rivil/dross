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
// ready with the given core count.
func probeScript(t *testing.T, unreachable, broken map[string]bool, cores int) *[]string {
	t.Helper()
	var probed []string
	orig := remoteProbeFn
	remoteProbeFn = func(tg remote.Target, _ []string) (remote.Readiness, error) {
		probed = append(probed, tg.Host)
		if unreachable[tg.Host] {
			return remote.Readiness{}, fmt.Errorf("dial %s: %w", tg.Host, remote.ErrTransport)
		}
		if broken[tg.Host] {
			return remote.Readiness{}, fmt.Errorf("boom on %s: %w", tg.Host, remote.ErrRemoteCommand)
		}
		return remote.Readiness{Cores: cores}, nil
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
		got, pf, err := selectRemoteTarget(targets("a", "b"), nil)
		if err != nil {
			t.Fatalf("select: %v", err)
		}
		if got == nil || got.Host != "b" {
			t.Fatalf("chose %v, want b", got)
		}
		if pf.Fallback {
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

// TestSelectStopsAtTheFirstReady: a later host must not be probed once one has
// answered — probing costs a round trip and the order is a preference.
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
		got, pf, err := selectRemoteTarget(targets("a", "b"), nil)
		if err != nil {
			t.Fatalf("an unreachable pool must fall back, not error: %v", err)
		}
		if got != nil {
			t.Errorf("chose %v with nothing reachable", got)
		}
		if !pf.Fallback || pf.Why == "" {
			t.Errorf("the fallback must carry a reason: %+v", pf)
		}
	})
}

// TestSelectSingleHostIsUnchanged pins c-3: one candidate must behave exactly
// as it did before the pool existed, including printing nothing extra.
func TestSelectSingleHostIsUnchanged(t *testing.T) {
	probeScript(t, nil, nil, 12)
	out := captureStdout(t, func() {
		got, pf, err := selectRemoteTarget(targets("solo"), nil)
		if err != nil || got == nil || got.Host != "solo" {
			t.Fatalf("got %v/%v", got, err)
		}
		if pf.Ready.Cores != 12 {
			t.Errorf("cores = %d, want the probe's answer carried through", pf.Ready.Cores)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("a single-host run must print nothing extra, got: %q", out)
	}
}

func TestSelectNoCandidatesIsNotRemote(t *testing.T) {
	got, pf, err := selectRemoteTarget(nil, nil)
	if err != nil || got != nil || pf.Fallback {
		t.Errorf("no grant must mean no remote and no fallback narration: %v/%v/%+v", got, err, pf)
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
