package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// preflightTarget is the host every case here probes. It is a valid target so
// nothing under test is rejected by remote.Target.Validate before the probe.
var preflightTarget = remote.Target{Host: "helicon", Workdir: "/home/rivil/dross"}

// TestPreflightFallsBackOnTransport: a host we could not REACH told us nothing,
// and the local machine can still produce an answer. Aborting the run instead
// is the failure this exists to prevent — it is what forced `dross remote
// revoke` as a workaround when helicon was down for hours.
func TestPreflightFallsBackOnTransport(t *testing.T) {
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{}, fmt.Errorf("ssh: connect to host helicon port 22: %w", remote.ErrTransport)
	})

	got, err := preflightRemote(preflightTarget, []string{"gremlins"})
	if err != nil {
		t.Fatalf("a transport failure returned an error instead of a fallback: %v", err)
	}
	if !got.Fallback {
		t.Error("a transport failure did not request a fallback")
	}
	if !strings.Contains(got.Why, preflightTarget.Host) {
		t.Errorf("the fallback reason does not name the host: %q", got.Why)
	}
	if got.Ready.Cores != 0 || len(got.Ready.Missing) != 0 {
		t.Errorf("a host that was never reached reported readiness: %+v", got.Ready)
	}
}

// TestPreflightDoesNotFallBackOnRemoteFailure: the other half of the locked
// fallback_policy. A host that RAN something and failed has given an answer;
// re-running it locally in the hope of a different one launders a real failure
// into a pass.
func TestPreflightDoesNotFallBackOnRemoteFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"remote command failed", fmt.Errorf("getconf: %w", remote.ErrRemoteCommand)},
		{"incomplete transfer", fmt.Errorf("rsync: %w", remote.ErrPartial)},
		{"unclassified failure", errors.New("something else entirely")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
				return remote.Readiness{}, tc.err
			})

			got, err := preflightRemote(preflightTarget, []string{"gremlins"})
			if err == nil {
				t.Fatal("a non-transport failure was swallowed")
			}
			if !errors.Is(err, tc.err) {
				t.Errorf("err = %v, want the probe's own error", err)
			}
			if got.Fallback {
				t.Error("a non-transport failure requested a local fallback")
			}
		})
	}
}

// TestPreflightMissingToolsIsNotFallback: a reachable host missing a tool is a
// fixable toolchain hole, not an unreachable machine. Reporting it as a
// fallback would hide it behind a silent local run — and hiding it is exactly
// what `dross remote bootstrap` exists to make unnecessary.
func TestPreflightMissingToolsIsNotFallback(t *testing.T) {
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 10, Missing: []string{"gremlins"}}, nil
	})

	got, err := preflightRemote(preflightTarget, []string{"gremlins", "npx"})
	if err != nil {
		t.Fatalf("a reachable host errored: %v", err)
	}
	if got.Fallback {
		t.Error("a reachable host with a missing tool requested a fallback")
	}
	if got.Why != "" {
		t.Errorf("a non-fallback carries a reason: %q", got.Why)
	}
	if got.Ready.Cores != 10 {
		t.Errorf("cores = %d, want the probe's 10 — the caller sizes the run from it", got.Ready.Cores)
	}
	if len(got.Ready.Missing) != 1 || got.Ready.Missing[0] != "gremlins" {
		t.Errorf("missing = %v, want the probe's list passed through", got.Ready.Missing)
	}
}

// TestPreflightWritesNothing: the fallback is per-run and never sticky. One
// flaky network minute must not retire a grant the user made deliberately —
// the next run probes again.
func TestPreflightWritesNothing(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "git@github.com:Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, ".dross")
	localFile := filepath.Join(root, LocalFile)
	if err := os.WriteFile(localFile, []byte("mutation_remote_host = \"helicon\"\nmutation_remote_workdir = \"/home/rivil/dross\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(localFile)
	if err != nil {
		t.Fatal(err)
	}

	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{}, fmt.Errorf("dial: %w", remote.ErrTransport)
	})
	if _, err := preflightRemote(preflightTarget, []string{"gremlins"}); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(localFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("a fallback rewrote local.toml:\n before: %q\n after:  %q", before, after)
	}
	target, err := firstRemoteGrant(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	if target == nil || target.Host != "helicon" {
		t.Errorf("the grant did not survive a fallback: %+v", target)
	}
}

// TestPreflightProbesThroughTheSharedSeam: doctor and the preflight must ask
// the SAME question. A preflight with its own slightly different check would
// pass on a host the run then fails on — the worst kind of green.
func TestPreflightProbesThroughTheSharedSeam(t *testing.T) {
	var gotTarget remote.Target
	var gotTools []string
	calls := fakeProbe(t, func(tgt remote.Target, tools []string) (remote.Readiness, error) {
		gotTarget, gotTools = tgt, tools
		return remote.Readiness{Cores: 4}, nil
	})

	want := []string{"gremlins", "npx", "dotnet"}
	if _, err := preflightRemote(preflightTarget, want); err != nil {
		t.Fatal(err)
	}

	if *calls != 1 {
		t.Errorf("the shared probe seam was called %d time(s), want exactly 1", *calls)
	}
	if gotTarget.Host != preflightTarget.Host || gotTarget.Workdir != preflightTarget.Workdir {
		t.Errorf("probed %+v, want %+v", gotTarget, preflightTarget)
	}
	if strings.Join(gotTools, ",") != strings.Join(want, ",") {
		t.Errorf("probed tools %v, want %v — the caller's list must reach the seam intact", gotTools, want)
	}
}
