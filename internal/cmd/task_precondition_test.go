package cmd

import (
	"bytes"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/localstore"
	"github.com/Rivil/dross/internal/phase"
)

// precondTask is one task's TOML: a header plus any precondition blocks.
type precondTask struct {
	id, status string
	pres       []string // each a [[task.preconditions]] body
}

func (pt precondTask) toml() string {
	var b strings.Builder
	b.WriteString("\n[[task]]\nid = \"" + pt.id + "\"\nwave = 1\ntitle = \"x\"\nfiles = [\"a.go\"]\ncovers = [\"c-1\"]\n")
	if pt.status != "" {
		b.WriteString("status = \"" + pt.status + "\"\n")
	}
	for _, p := range pt.pres {
		b.WriteString("\n[[task.preconditions]]\n" + p + "\n")
	}
	return b.String()
}

// precondRun is a precondition body running line, quoted as a TOML basic string.
func precondRun(line string, extra ...string) string {
	return strings.Join(append([]string{"run = " + strconv.Quote(line)}, extra...), "\n")
}

func precondPlan(tasks ...precondTask) string {
	s := "[phase]\nid = \"01-test\"\n"
	for _, t := range tasks {
		s += t.toml()
	}
	return s
}

func skipPrecondOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("preconditions run under sh")
	}
}

// precondFixture scaffolds a trusted repo whose phase 01-test carries plan,
// granting the phase's precondition set when grant is true.
func precondFixture(t *testing.T, plan string, grant bool) {
	t.Helper()
	skipPrecondOnWindows(t)
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", plan)
	if grant {
		grantPreconditions(t)
	}
}

func grantPreconditions(t *testing.T) {
	t.Helper()
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	p, err := phase.LoadPlan(reasonPlanPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := consent.GrantPreconditionsConsent(localstore.GrantStore(root), "01-test", phase.PreconditionSetLine(p)); err != nil {
		t.Fatal(err)
	}
}

// runTaskVerb runs `dross task <args>`, returning what it wrote to the
// command's stderr (the verdict, and cobra's error line) and its error.
func runTaskVerb(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := Task()
	var stderr bytes.Buffer
	c.SetArgs(args)
	c.SetOut(new(bytes.Buffer))
	c.SetErr(&stderr)
	var err error
	captureStdout(t, func() { err = c.Execute() })
	return stderr.String(), err
}

func planBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(reasonPlanPath())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func taskStatusOf(t *testing.T, id string) string {
	t.Helper()
	return orPending(loadReasonTask(t, id).Status)
}

func markerExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

func TestFailingPreconditionRefusesWithExit3(t *testing.T) {
	precondFixture(t, precondPlan(precondTask{id: "t-1", pres: []string{precondRun("echo boom; exit 7")}}), true)
	before := planBytes(t)
	out, err := runTaskVerb(t, "status", "01-test", "t-1", "in_progress")
	if ExitCode(err) != exitPreconditionRefused {
		t.Fatalf("exit = %d (%v), want %d — never the precondition's own 7", ExitCode(err), err, exitPreconditionRefused)
	}
	for _, want := range []string{"precondition 1", "exit 7", "boom"} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr lacks %q:\n%s", want, out)
		}
	}
	if !bytes.Equal(planBytes(t), before) {
		t.Error("a refused start changed plan.toml")
	}
}

func TestRefusedRetryKeepsAFailedTaskAsItWas(t *testing.T) {
	precondFixture(t, precondPlan(precondTask{id: "t-1", status: "failed", pres: []string{precondRun("exit 1")}}), true)
	if _, err := runTaskVerb(t, "status", "01-test", "t-1", "failed", "--reason", "first try"); err != nil {
		t.Fatal(err)
	}
	grantPreconditions(t)
	before := planBytes(t)
	if _, err := runTaskVerb(t, "status", "01-test", "t-1", "in_progress"); ExitCode(err) != exitPreconditionRefused {
		t.Fatalf("exit = %d (%v), want %d", ExitCode(err), err, exitPreconditionRefused)
	}
	got := loadReasonTask(t, "t-1")
	if got.Status != phase.StatusFailed || got.Reason != "first try" || !bytes.Equal(planBytes(t), before) {
		t.Errorf("t-1 = %s / %q, want failed / \"first try\" and plan.toml unchanged", got.Status, got.Reason)
	}
}

func TestBoundedPreconditionsGateTheStart(t *testing.T) {
	for _, c := range []struct {
		name, pre string
		start     bool
		want      string
	}{
		{"over max", precondRun("echo 7", "max = 5"), false, "measured 7, bound max 5"},
		{"at max", precondRun("echo 5", "max = 5"), true, ""},
		{"unparseable", precondRun("echo '12 ms'", "max = 5"), false, "unparseable"},
		{"timeout", precondRun("sleep 30", `timeout = "200ms"`), false, "timed out after 200ms"},
	} {
		t.Run(c.name, func(t *testing.T) {
			precondFixture(t, precondPlan(precondTask{id: "t-1", pres: []string{c.pre}}), true)
			out, err := runTaskVerb(t, "status", "01-test", "t-1", "in_progress")
			if c.start {
				if err != nil || taskStatusOf(t, "t-1") != phase.StatusInProgress {
					t.Fatalf("err = %v, status %s; want in_progress\n%s", err, taskStatusOf(t, "t-1"), out)
				}
				return
			}
			if ExitCode(err) != exitPreconditionRefused || !strings.Contains(out, c.want) {
				t.Errorf("exit %d, stderr:\n%s\nwant exit %d naming %q", ExitCode(err), out, exitPreconditionRefused, c.want)
			}
			if taskStatusOf(t, "t-1") != phase.StatusPending {
				t.Errorf("a refused task is %s", taskStatusOf(t, "t-1"))
			}
		})
	}
}

func TestUnconsentedPreconditionsNeverRun(t *testing.T) {
	plan := precondPlan(precondTask{id: "t-1", pres: []string{precondRun("touch ran")}})
	t.Run("absent", func(t *testing.T) {
		precondFixture(t, plan, false)
		_, err := runTaskVerb(t, "status", "01-test", "t-1", "in_progress")
		if ExitCode(err) != exitPreconditionConsent || !strings.Contains(err.Error(), "dross trust --preconditions 01-test") {
			t.Errorf("exit %d (%v), want %d naming `dross trust --preconditions 01-test`", ExitCode(err), err, exitPreconditionConsent)
		}
		if markerExists("ran") {
			t.Error("an unconsented precondition ran")
		}
	})
	t.Run("stale", func(t *testing.T) {
		precondFixture(t, plan, true)
		edited := strings.Replace(string(planBytes(t)), `"touch ran"`, `"touch ran; true"`, 1)
		mustWrite(t, reasonPlanPath(), edited)
		_, err := runTaskVerb(t, "status", "01-test", "t-1", "in_progress")
		if ExitCode(err) != exitPreconditionConsent || !strings.Contains(err.Error(), "dross trust --preconditions 01-test") {
			t.Errorf("exit %d (%v), want %d naming `dross trust --preconditions 01-test`", ExitCode(err), err, exitPreconditionConsent)
		}
		if markerExists("ran") {
			t.Error("a precondition edited since its grant ran")
		}
	})
}

func TestMalformedPreconditionsRefuseBeforeConsent(t *testing.T) {
	for name, pre := range map[string]string{
		"empty run":   precondRun(""),
		"unknown key": precondRun("touch ran", "bogus = 1"),
		"bad timeout": precondRun("touch ran", `timeout = "soon"`),
	} {
		t.Run(name, func(t *testing.T) {
			// No grant: a malformed precondition must be refused for its
			// shape, not reach the consent check (exit 4) or a spawn.
			precondFixture(t, precondPlan(precondTask{id: "t-1", pres: []string{pre}}), false)
			_, err := runTaskVerb(t, "status", "01-test", "t-1", "in_progress")
			if ExitCode(err) != exitPreconditionRefused || !strings.Contains(err.Error(), "precondition 1") {
				t.Errorf("exit %d (%v), want %d naming the malformed precondition", ExitCode(err), err, exitPreconditionRefused)
			}
			if markerExists("ran") {
				t.Error("a malformed precondition ran")
			}
		})
	}
}

func TestTaskWithoutPreconditionsNeedsNoGrant(t *testing.T) {
	precondFixture(t, precondPlan(
		precondTask{id: "t-1"},
		precondTask{id: "t-2", pres: []string{precondRun("exit 1")}},
	), false)
	if _, err := runTaskVerb(t, "status", "01-test", "t-1", "in_progress"); err != nil {
		t.Fatalf("an ungated task was refused: %v", err)
	}
	if taskStatusOf(t, "t-1") != phase.StatusInProgress {
		t.Errorf("t-1 is %s", taskStatusOf(t, "t-1"))
	}
}

func TestPreconditionsGateOnlyTheTransition(t *testing.T) {
	t.Run("already in_progress skips them", func(t *testing.T) {
		precondFixture(t, precondPlan(precondTask{id: "t-1", status: "in_progress", pres: []string{precondRun("touch ran; exit 1")}}), false)
		if _, err := runTaskVerb(t, "status", "01-test", "t-1", "in_progress"); err != nil {
			t.Fatalf("re-setting an in_progress task was refused: %v", err)
		}
		if markerExists("ran") {
			t.Error("re-setting an in_progress task ran its preconditions")
		}
	})
	for _, from := range []string{"pending", "failed"} {
		t.Run("retry from "+from+" re-runs them", func(t *testing.T) {
			precondFixture(t, precondPlan(precondTask{id: "t-1", status: from, pres: []string{precondRun("touch ran")}}), true)
			if _, err := runTaskVerb(t, "status", "01-test", "t-1", "in_progress"); err != nil {
				t.Fatal(err)
			}
			if !markerExists("ran") {
				t.Errorf("a start from %s did not run the preconditions", from)
			}
		})
	}
}

func TestSecondInProgressRefusesBeforeAnySpawn(t *testing.T) {
	precondFixture(t, precondPlan(
		precondTask{id: "t-1", status: "in_progress"},
		precondTask{id: "t-2", pres: []string{precondRun("touch ran")}},
	), true)
	_, err := runTaskVerb(t, "status", "01-test", "t-2", "in_progress")
	if err == nil || !strings.Contains(err.Error(), "one task at a time") {
		t.Fatalf("err = %v, want the one-task refusal", err)
	}
	if markerExists("ran") {
		t.Error("a precondition ran before the one-task refusal")
	}
}

func TestConcurrentStartBetweenRunAndSaveRefuses(t *testing.T) {
	precondFixture(t, precondPlan(
		precondTask{id: "t-1", pres: []string{precondRun("true")}},
		precondTask{id: "t-2"},
	), true)
	prev := beforePreconditionSave
	t.Cleanup(func() { beforePreconditionSave = prev })
	beforePreconditionSave = func() {
		p, err := phase.LoadPlan(reasonPlanPath())
		if err != nil {
			t.Fatal(err)
		}
		p.SetTaskStatus("t-2", phase.StatusInProgress)
		if err := p.Save(reasonPlanPath()); err != nil {
			t.Fatal(err)
		}
	}
	_, err := runTaskVerb(t, "status", "01-test", "t-1", "in_progress")
	if err == nil {
		t.Fatal("a start that raced another task's start was saved")
	}
	if got := taskStatusOf(t, "t-2"); got != phase.StatusInProgress {
		t.Errorf("the concurrent write was lost: t-2 is %s", got)
	}
	if got := taskStatusOf(t, "t-1"); got != phase.StatusPending {
		t.Errorf("t-1 is %s, want it left pending", got)
	}
}

func TestPreconditionsRunAtTheRepoRoot(t *testing.T) {
	precondFixture(t, precondPlan(precondTask{id: "t-1", pres: []string{precondRun("test -f go.mod")}}), true)
	mustWrite(t, "go.mod", "module x\n")
	if err := os.Mkdir("sub", 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir("sub")
	if out, err := runTaskVerb(t, "status", "01-test", "t-1", "in_progress"); err != nil {
		t.Fatalf("`test -f go.mod` failed from a subdirectory: %v\n%s", err, out)
	}
}

func TestTaskStatusHasNoBypassFlag(t *testing.T) {
	var flags []string
	taskStatus().Flags().VisitAll(func(f *pflag.Flag) { flags = append(flags, f.Name) })
	sort.Strings(flags)
	if strings.Join(flags, ",") != "reason" {
		t.Errorf("task status flags = %v, want exactly [reason]", flags)
	}
}
