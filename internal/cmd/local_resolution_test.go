package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// firstRemoteGrant is the scalar view of readRemoteGrants, for tests that are
// about the STORE — alias resolution, provenance refusal, workdir validation —
// rather than about which candidate a run picks.
//
// It reads through the ONE surviving reader on purpose. A test helper that
// parsed local.toml itself would be a third implementation of the resolution
// this phase collapsed to one, and it would keep passing after the real one
// drifted.
func firstRemoteGrant(root, repoDir string) (*remote.Target, error) {
	ts, err := readRemoteGrants(root, repoDir)
	if err != nil || len(ts) == 0 {
		return nil, err
	}
	return ts[0], nil
}

// TestScalarGrantResolutionIsGone is the structural half of locked
// resolution_has_one_implementation: c-2 is satisfied by DELETING the
// scalar-only path, not by teaching each caller to walk the pool.
//
// Asserted against the source rather than against behaviour because behaviour
// cannot see it: a second resolver that happens to agree today passes every
// output assertion and is one refactor away from being the second WRONG one.
// Four callers each doing their own resolution is how the divergence arose.
func TestScalarGrantResolutionIsGone(t *testing.T) {
	// Located from this file rather than from the working directory: other
	// tests in this package chdir into temp repos, and a scan of "." would
	// quietly find no files and pass.
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test's own source")
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, filepath.Dir(self), func(fi os.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go")
	}, 0)
	if err != nil {
		t.Fatalf("parse internal/cmd: %v", err)
	}
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil {
					continue
				}
				if fn.Name.Name == "readRemoteGrant" {
					t.Errorf("%s defines readRemoteGrant — the scalar-only resolution is back, "+
						"and the surfaces that call it can name a host the next run would not use",
						filepath.Base(path))
				}
			}
		}
	}
}

// TestResolveKeepsTheScalarGrantAsCandidateZero: a repo with only the old
// host/workdir pair must resolve exactly as it always did. If the scalar grant
// stopped being candidate zero, such a repo would resolve to no host at all.
func TestResolveKeepsTheScalarGrantAsCandidateZero(t *testing.T) {
	root := chdirDross(t)
	writeLocalStore(t, root, "remote_host = \"solo\"\nremote_workdir = \"/srv/dross\"\n")
	probed := probeScript(t, nil, nil, 8)

	target, pool, err := resolveRemoteHost(root, filepath.Dir(root), nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if target == nil || target.Host != "solo" {
		t.Fatalf("resolved to %v, want the scalar grant", target)
	}
	if target.Workdir != "/srv/dross" {
		t.Errorf("workdir = %q, want the granted one", target.Workdir)
	}
	if pool.Fallback {
		t.Error("a reachable scalar grant must not read as a fallback")
	}
	if len(*probed) != 1 {
		t.Errorf("probed %v, want exactly the one granted host", *probed)
	}
}

// TestResolveAgreesWithARunWhenTheScalarHostIsDown is c-2's own fixture: a pool
// declared, the scalar host down. Every surface must name the machine the next
// run would actually use, which is the SECOND one.
//
// Before this phase the four non-run surfaces read the scalar grant without
// probing, so they all named a host that was down while the run used another —
// and the disagreement was silent.
func TestResolveAgreesWithARunWhenTheScalarHostIsDown(t *testing.T) {
	root := chdirDross(t)
	writeLocalStore(t, root,
		"remote_host = \"down\"\nremote_workdir = \"/srv/dross\"\n"+
			"[[remote_pool]]\nhost = \"up\"\nworkdir = \"/srv/dross\"\n")
	probeScript(t, map[string]bool{"down": true}, nil, 8)

	resolved, _, err := resolveRemoteHost(root, filepath.Dir(root), nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved == nil || resolved.Host != "up" {
		t.Fatalf("resolved to %v, want up — down is unreachable", resolved)
	}

	// The run's own choice, made through the run's own entry point. Comparing
	// against a re-derivation here would prove only that the test agrees with
	// itself.
	targets, err := readRemoteGrants(root, filepath.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	var chosen *remote.Target
	captureStdout(t, func() {
		chosen, _, err = selectRemoteTarget(targets, nil)
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if chosen == nil || chosen.Host != resolved.Host {
		t.Errorf("the run would use %v while the reporting surfaces say %v — they must not disagree", chosen, resolved)
	}
}

// TestRemoteStatusReportsTheHostTheRunWouldUse: `dross remote status` is the
// surface a user checks BEFORE a run, so it reporting a different machine from
// the one the run picks is the worst version of this bug — the user reads it,
// believes it, and the run goes elsewhere.
func TestRemoteStatusReportsTheHostTheRunWouldUse(t *testing.T) {
	root := chdirDross(t)
	writeLocalStore(t, root,
		"remote_host = \"down\"\nremote_workdir = \"/srv/one\"\n"+
			"[[remote_pool]]\nhost = \"up\"\nworkdir = \"/srv/two\"\n")
	probeScript(t, map[string]bool{"down": true}, nil, 8)

	var out string
	if err := runCmdCapturing(t, &out, Remote(), "status"); err != nil {
		t.Fatalf("dross remote status: %v", err)
	}
	if !strings.Contains(out, "up") || !strings.Contains(out, "/srv/two") {
		t.Errorf("status does not name the host the next run would use:\n%s", out)
	}
	// The skip is named too. A status that showed only the winner would hide
	// the first host being down — the fault the user opened status to find.
	if !strings.Contains(out, "down") {
		t.Errorf("status hid the unreachable candidate:\n%s", out)
	}
}

// TestRemoteStatusSeparatesUnreachableFromUngranted: the authorization still
// stands when nothing answers, and revoking it is a different decision from
// waiting for the host to come back. Reporting "not granted" would send the
// user to re-grant a machine that is already authorized.
func TestRemoteStatusSeparatesUnreachableFromUngranted(t *testing.T) {
	root := chdirDross(t)
	writeLocalStore(t, root, "remote_host = \"down\"\nremote_workdir = \"/srv/one\"\n")
	probeScript(t, map[string]bool{"down": true}, nil, 8)

	var out string
	if err := runCmdCapturing(t, &out, Remote(), "status"); err != nil {
		t.Fatalf("an unreachable grant must report, not fail: %v\n%s", err, out)
	}
	if strings.Contains(out, "not granted") {
		t.Errorf("an unreachable grant was reported as ungranted:\n%s", out)
	}
	if !strings.Contains(out, "down") {
		t.Errorf("the report does not name the host that did not answer:\n%s", out)
	}
}

// TestACommandFailureNamesTheMachineThatFailed covers the ERROR branch of the
// resolution this task collapsed onto one implementation: a host that ANSWERED
// and failed is an answer, and the caller has to be able to say which machine
// it was.
//
// It is a different failure from the one TestFailedProbeInstallsNowhere pins. A
// TRANSPORT error is turned into a fallback by preflightRemote, so that run
// leaves through pool.Why with the host never having failed at all. Only a
// remote-COMMAND failure aborts the walk with pool.Failed set, which is the
// branch that turns a target into a name.
//
// The probe's own error deliberately carries no host, so "helicon" in the
// message can only have come from pool.Failed — a resolution that lost the
// failing target falls back to the anonymous "the granted host" and this goes
// red rather than passing on a plausible-looking string.
func TestACommandFailureNamesTheMachineThatFailed(t *testing.T) {
	grantedLaneFixture(t, pnpmLane)
	installLaneLookPath(t, "pnpm")
	fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{}, fmt.Errorf("getconf: %w", remote.ErrRemoteCommand)
	})
	rn, ln := installSeamRecorder(t)

	err := runCmd(t, Test(), "lane", "install", "web", "--apply")
	if err == nil {
		t.Fatal("a host that answered with a failure was treated as an answer")
	}
	if *rn != 0 || *ln != 0 {
		t.Errorf("a failed resolution still installed: remote=%d local=%d", *rn, *ln)
	}
	if !strings.Contains(err.Error(), "helicon") {
		t.Errorf("the failure does not name the machine that failed: %v", err)
	}
	if strings.Contains(err.Error(), "the granted host") {
		t.Errorf("the failing target was lost and the message went anonymous: %v", err)
	}
}
