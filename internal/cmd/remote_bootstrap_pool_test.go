package cmd

// Bootstrap across a POOL: every granted candidate provisioned, each reported
// on its own.
//
// Per-lane routing assumes the pool is in the state bootstrap is supposed to
// put it in. A verb that brought up only the host a run happens to choose
// leaves every other candidate exactly as it was — and the next run's lane
// still comes home for a tool nothing installed (c-3).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// poolBootstrapFixture is a repo granting alpha and beta, with the gremlins
// adapter and a pnpm lane.
func poolBootstrapFixture(t *testing.T) {
	t.Helper()
	bootstrapLaneFixture(t, `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "pnpm test"`, "gremlins")
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	// Written straight into the store: the grant VERB probes, and these tests
	// install their own probe below.
	body := "remote_host = \"alpha\"\nremote_workdir = \"/srv/dross\"\n" +
		"[[remote_pool]]\nhost = \"beta\"\nworkdir = \"/srv/dross\"\n"
	if err := os.WriteFile(filepath.Join(root, LocalFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// hostExecRecorder records every install with the machine it was aimed at.
// execRecorder keeps only the argv, which cannot answer "did beta get it".
type hostExecRecorder struct {
	calls []string // "<host> <argv[0]> <argv[1]>..."
	errOn map[string]error
}

func (r *hostExecRecorder) install(t *testing.T) {
	t.Helper()
	orig := remoteExecFn
	remoteExecFn = func(tgt remote.Target, argv []string) (string, error) {
		r.calls = append(r.calls, tgt.Host+" "+strings.Join(argv, " "))
		return "", r.errOn[tgt.Host]
	}
	t.Cleanup(func() { remoteExecFn = orig })
}

func (r *hostExecRecorder) hostsTouched() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range r.calls {
		h := strings.SplitN(c, " ", 2)[0]
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// poolProbe stubs the shared seam per host: missing[host] is what that machine
// lacks, and a host in dead fails as transport.
func poolProbe(t *testing.T, missing map[string][]string, dead map[string]bool) *[]string {
	t.Helper()
	var probed []string
	fakeProbe(t, func(tgt remote.Target, tools []string) (remote.Readiness, error) {
		probed = append(probed, tgt.Host)
		if dead[tgt.Host] {
			return remote.Readiness{}, fmt.Errorf("dial %s: %w", tgt.Host, remote.ErrTransport)
		}
		gone := map[string]bool{}
		for _, m := range missing[tgt.Host] {
			gone[m] = true
		}
		r := remote.Readiness{Cores: 8}
		for _, tool := range tools {
			if gone[tool] {
				r.Missing = append(r.Missing, tool)
			}
		}
		return r, nil
	})
	return &probed
}

// TestBootstrapProvisionsEveryCandidate is c-3. Bringing up only the host a run
// would choose leaves the rest of the pool exactly as unprovisioned as before,
// and per-lane routing then sends a lane to a machine bootstrap said it had
// handled.
func TestBootstrapProvisionsEveryCandidate(t *testing.T) {
	poolBootstrapFixture(t)
	probed := poolProbe(t, map[string][]string{
		"alpha": {"gremlins"},
		"beta":  {"gremlins"},
	}, nil)
	rec := &hostExecRecorder{}
	rec.install(t)

	out, err := runBootstrap(t, "--apply")
	if err != nil {
		t.Fatalf("bootstrap --apply: %v\n%s", err, out)
	}
	if len(*probed) != 2 {
		t.Errorf("probed %v, want both candidates — a walk that stopped at the first leaves beta alone", *probed)
	}
	got := rec.hostsTouched()
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Errorf("installed on %v, want alpha then beta:\n%s", got, out)
	}
}

// TestBootstrapReportsEachHostSeparately: a tool installed on alpha and refused
// on beta is two situations with two remedies. One merged outcome hides
// whichever is worse behind whichever is louder.
func TestBootstrapReportsEachHostSeparately(t *testing.T) {
	poolBootstrapFixture(t)
	poolProbe(t, map[string][]string{
		"alpha": {"gremlins"},       // installable: go is present
		"beta":  {"gremlins", "go"}, // refused: no runtime to install into
	}, nil)
	rec := &hostExecRecorder{}
	rec.install(t)

	out, err := runBootstrap(t, "--apply")
	if err == nil {
		t.Fatalf("a refusal on one host exited 0:\n%s", out)
	}
	alpha := sectionFor(t, out, "alpha")
	beta := sectionFor(t, out, "beta")
	if !strings.Contains(alpha, "installing") {
		t.Errorf("alpha's section does not report the install:\n%s", alpha)
	}
	// The step marker, not the word: every section's summary counts refusals,
	// so "refused" appears in a clean one too.
	if !strings.Contains(beta, ") refused —") {
		t.Errorf("beta's section does not report the refusal:\n%s", beta)
	}
	if strings.Contains(alpha, ") refused —") {
		t.Errorf("beta's refusal bled into alpha's section:\n%s", alpha)
	}
	// And the exit names the host that is not provisioned, not just "something
	// failed" — the remedy is about that machine.
	if !strings.Contains(err.Error(), "beta") {
		t.Errorf("the failure does not name the unprovisioned host: %v", err)
	}
}

// TestBootstrapReportsWhatWasAlreadyPresentPerHost: c-3 names three outcomes,
// and "already installed" is the one that tells the user a host needs nothing.
// Reported per machine, because a pool where one host has the tool and another
// does not is the normal state bootstrap exists to end.
func TestBootstrapReportsWhatWasAlreadyPresentPerHost(t *testing.T) {
	poolBootstrapFixture(t)
	poolProbe(t, map[string][]string{
		"alpha": nil, // has everything
		"beta":  {"gremlins"},
	}, nil)
	rec := &hostExecRecorder{}
	rec.install(t)

	out, err := runBootstrap(t, "--apply")
	if err != nil {
		t.Fatalf("bootstrap --apply: %v\n%s", err, out)
	}
	if !strings.Contains(sectionFor(t, out, "alpha"), "already installed") {
		t.Errorf("alpha's section does not say the tool was already there:\n%s", out)
	}
	if got := rec.hostsTouched(); len(got) != 1 || got[0] != "beta" {
		t.Errorf("installed on %v, want beta alone — alpha needed nothing", got)
	}
}

// TestAnUnreachableCandidateDoesNotStopTheRest: aborting on a dead host leaves
// the live ones exactly as unprovisioned as the dead one, which is the opposite
// of what the user asked for.
func TestAnUnreachableCandidateDoesNotStopTheRest(t *testing.T) {
	poolBootstrapFixture(t)
	poolProbe(t, map[string][]string{"beta": {"gremlins"}}, map[string]bool{"alpha": true})
	rec := &hostExecRecorder{}
	rec.install(t)

	out, err := runBootstrap(t, "--apply")
	if got := rec.hostsTouched(); len(got) != 1 || got[0] != "beta" {
		t.Errorf("installed on %v, want beta — a dead first host stopped the run:\n%s", got, out)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "could not be reached") {
		t.Errorf("the unreachable candidate is not reported by name:\n%s", out)
	}
	// Reported AND non-zero: a pool with a host nobody could reach is not
	// provisioned, and exiting 0 would let a script believe it is.
	if err == nil {
		t.Errorf("an unreachable candidate exited 0:\n%s", out)
	}
}

// TestASingleGrantedHostIsUnchanged: the overwhelmingly common shape is one
// host, and it must read exactly as it did — one section, one summary, no
// pool vocabulary.
func TestASingleGrantedHostIsUnchanged(t *testing.T) {
	bootstrapFixture(t, "gremlins")
	probeMissing(t, "gremlins")
	rec := &execRecorder{}
	rec.install(t)

	out, err := runBootstrap(t)
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if n := strings.Count(out, "remote helicon"); n != 1 {
		t.Errorf("the host header appears %d time(s), want 1:\n%s", n, out)
	}
	if strings.Contains(out, "could not be reached") || strings.Contains(out, "not fully provisioned") {
		t.Errorf("a single-host run picked up pool vocabulary:\n%s", out)
	}
}

// TestEveryGrantedHostAnsweringNothingIsStillAnError: with no candidate
// reachable there is nothing to provision, and that is not the same as having
// no grant — the remedies differ, and telling the user to re-grant a host that
// is merely down sends them to withdraw an authorization they still want.
func TestEveryGrantedHostAnsweringNothingIsStillAnError(t *testing.T) {
	poolBootstrapFixture(t)
	poolProbe(t, nil, map[string]bool{"alpha": true, "beta": true})

	out, err := runBootstrap(t)
	if err == nil {
		t.Fatalf("an unreachable pool exited 0:\n%s", out)
	}
	if strings.Contains(err.Error(), "no remote granted") {
		t.Errorf("an unreachable pool was reported as ungranted: %v", err)
	}
}

// sectionFor is the slice of out belonging to host's report — from its header
// to the next host's, or the end.
func sectionFor(t *testing.T, out, host string) string {
	t.Helper()
	start := strings.Index(out, "remote "+host)
	if start < 0 {
		t.Fatalf("no section for %s:\n%s", host, out)
	}
	rest := out[start+len("remote "+host):]
	if next := strings.Index(rest, "\nremote "); next >= 0 {
		return rest[:next]
	}
	return rest
}
