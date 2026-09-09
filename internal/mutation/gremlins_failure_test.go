package mutation

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// The gremlins half of c-1: THREE fatal paths, not two. The attached loop's
// invocation-failed and reportless-exit branches, and Collect's detached site —
// which is the one this repo actually runs.
const canaryG = "CANARY-G-8b03"

// noisyRemoteTool swaps the launcher seam for one whose TOOL leg prints out and
// exits code, leaving every other leg (push, fetch, rm) clean.
//
// scriptRemote's exitingCmd prints nothing, which is exactly what a byte-count
// assertion cannot be written against.
//
// The payload is `cat`-ed from a FILE rather than embedded in the argv. dross
// echoes the invocation into the error on this path — legitimately, it is
// dross-composed argv — so a canary inside the stub's own command line would
// arrive there and read as a leak the production path cannot have.
func noisyRemoteTool(t *testing.T, out string, code int) {
	t.Helper()
	payload := filepath.Join(t.TempDir(), "tool-output.txt")
	if err := os.WriteFile(payload, []byte(out+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := launcherCommand
	launcherCommand = func(argv []string, stdin string) *exec.Cmd {
		cp := append(append([]string(nil), argv...), stdin)
		if matchesLeg(cp, "tool") {
			return exec.Command("sh", "-c", fmt.Sprintf("cat %s; exit %d", payload, code))
		}
		return exec.Command("true")
	}
	t.Cleanup(func() { launcherCommand = orig })
}

// TestGremlinsReportlessExitIsRecorded is the attached fatal path that fires on
// a remote run: the tool exited non-zero and wrote nothing.
//
// Three things have to hold at once and each is a separate way to break it —
// the record must be reachable, the refusal's identity must survive so verify
// still grades the leg, and the tool's output must be on the terminal and not
// in the error.
func TestGremlinsReportlessExitIsRecorded(t *testing.T) {
	failLocalSpawns(t)
	noisyRemoteTool(t, "gremlins: gathering "+canaryG, 7)

	g := remoteGremlins(t.TempDir())
	out, _, err := captureStderr(t, func() (*Report, error) { return g.Run([]string{"internal/cmd/verify.go"}) })
	if err == nil {
		t.Fatal("a reportless non-zero exit returned nil error")
	}

	var rec *toolFailure
	if !errors.As(err, &rec) {
		t.Fatalf("the reportless-exit path returned a bare error, not a record: %v", err)
	}
	if !strings.Contains(err.Error(), "exit status 7") {
		t.Errorf("the record does not name the real exit status:\n%v", err)
	}
	// The refusal keeps its identity, or verify stops grading the leg BLOCKING
	// and cmd/test.go stops classifying it.
	if !errors.Is(err, remote.ErrRemoteCommand) {
		t.Errorf("the reportless refusal no longer satisfies errors.Is(remote.ErrRemoteCommand): %v", err)
	}
	if !strings.Contains(err.Error(), "unmeasured rather than clean") {
		t.Errorf("dross's own refusal prose was swallowed by the record:\n%v", err)
	}

	if strings.Contains(err.Error(), canaryG) {
		t.Errorf("tool output reached the error, which is what gets persisted:\n%v", err)
	}
	if !strings.Contains(out, canaryG) {
		t.Errorf("the tool's output never reached the terminal:\n%s", out)
	}
	// The tee is what makes the byte count true. Without it the record reports
	// nothing observed while the tool plainly printed.
	if strings.Contains(err.Error(), " 0 bytes") {
		t.Errorf("the record observed 0 bytes of a run that printed — the attached output is not being teed:\n%v", err)
	}
}

// TestGremlinsInvocationFailureIsRecorded is the attached path where the
// process never started. There is no exit status to name, so the record says so
// rather than reporting -1 as though gremlins had chosen it.
func TestGremlinsInvocationFailureIsRecorded(t *testing.T) {
	orig := gremlinsBuildCmd
	gremlinsBuildCmd = func(_ *Gremlins, _ []string) *exec.Cmd {
		return exec.Command("/nonexistent/dross-gremlins-does-not-exist")
	}
	t.Cleanup(func() { gremlinsBuildCmd = orig })

	g := &Gremlins{ProjectRoot: t.TempDir()}
	_, _, err := captureStderr(t, func() (*Report, error) { return g.Run([]string{"internal/cmd/verify.go"}) })
	if err == nil {
		t.Fatal("a gremlins binary that could not be started returned nil error")
	}

	var rec *toolFailure
	if !errors.As(err, &rec) {
		t.Fatalf("the invocation-failed path returned a bare error, not a record: %v", err)
	}
	if !strings.Contains(err.Error(), "did not start") {
		t.Errorf("a start failure does not say so:\n%v", err)
	}
	if strings.Contains(err.Error(), "exit status -1") {
		t.Errorf("a start failure reports an exit status gremlins never chose:\n%v", err)
	}
	// dross's own context survives — the install hint is the actionable half.
	if !strings.Contains(err.Error(), "is gremlins installed?") {
		t.Errorf("the install hint was swallowed by the record:\n%v", err)
	}
}

// TestGremlinsDetachedCollectRecordsNotCaptured is the path this repo actually
// runs, and the reason `capture` has two shapes.
//
// A detached run tees nothing anywhere this process can see. Recording it as
// `0 bytes observed` would be a structurally false claim that gremlins was
// silent; NotCaptured says nobody was watching, which is the true statement.
func TestGremlinsDetachedCollectRecordsNotCaptured(t *testing.T) {
	root := t.TempDir()
	g := &Gremlins{ProjectRoot: root}
	steps, err := g.DetachSteps([]string{"pkga/x.go"})
	if err != nil {
		t.Fatal(err)
	}
	writeStepExit(t, root, steps[0], "9")

	_, err = g.Collect(steps, "helicon")
	if err == nil {
		t.Fatal("a detached package that exited 9 without a report was collected as merely unmeasured")
	}

	var rec *toolFailure
	if !errors.As(err, &rec) {
		t.Fatalf("Collect's fatal path returned a bare error, not a record: %v", err)
	}
	if !strings.Contains(err.Error(), "exit status 9") {
		t.Errorf("the record does not name the real exit status:\n%v", err)
	}
	if !strings.Contains(err.Error(), "detached") {
		t.Errorf("the record does not say why nothing was captured:\n%v", err)
	}
	if strings.Contains(err.Error(), " 0 bytes") || strings.Contains(err.Error(), "bytes of tool output") {
		t.Errorf("a detached run reported a byte count, claiming the tool was silent:\n%v", err)
	}
	// The pre-existing refusal is unchanged: package, code and measuring host.
	for _, want := range []string{steps[0].Package, "exited 9", "helicon"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal no longer names %q: %v", want, err)
		}
	}
	if !errors.Is(err, remote.ErrRemoteCommand) {
		t.Errorf("the detached refusal no longer satisfies errors.Is(remote.ErrRemoteCommand): %v", err)
	}
}

// TestGremlinsRemoteStartFailureStaysTransport is the boundary the record must
// not swallow. A local ssh that could not be started means NOTHING reached the
// host — verify grades that BLOCKING, not FLAG — and naming gremlins would send
// the user to audit an installation that was never consulted.
func TestGremlinsRemoteStartFailureStaysTransport(t *testing.T) {
	failLocalSpawns(t)
	orig := launcherCommand
	launcherCommand = func(argv []string, stdin string) *exec.Cmd {
		cp := append(append([]string(nil), argv...), stdin)
		if matchesLeg(cp, "tool") {
			return exec.Command("/nonexistent/dross-ssh-does-not-exist")
		}
		return exec.Command("true")
	}
	t.Cleanup(func() { launcherCommand = orig })

	g := remoteGremlins(t.TempDir())
	_, _, err := captureStderr(t, func() (*Report, error) { return g.Run([]string{"internal/cmd/verify.go"}) })
	if err == nil {
		t.Fatal("an ssh that could not be started returned nil error")
	}
	if !errors.Is(err, remote.ErrTransport) {
		t.Errorf("the start failure is no longer a transport error, so verify would grade it FLAG: %v", err)
	}
	if !strings.Contains(err.Error(), "helicon") {
		t.Errorf("the error no longer names the host: %v", err)
	}
	var rec *toolFailure
	if errors.As(err, &rec) {
		t.Errorf("a transport failure was recorded as a gremlins tool failure, which sends the user to the wrong box: %v", err)
	}
}
