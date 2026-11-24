package cmd

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// `dross task check` is c-8: the precondition gate of `task status …
// in_progress`, minus the transition. These tests hold it to the same verdict
// and exit codes as that gate, and to never writing the plan.

// TestTaskCheckMatchesTheStatusGate: on t-9's failing fixture, check and the
// status gate print the same thing and exit the same way — 3, never the
// precondition's own 7.
func TestTaskCheckMatchesTheStatusGate(t *testing.T) {
	precondFixture(t, precondPlan(precondTask{id: "t-1", pres: []string{precondRun("echo boom; exit 7")}}), true)
	checkOut, checkErr := runTaskVerb(t, "check", "01-test", "t-1")
	statusOut, statusErr := runTaskVerb(t, "status", "01-test", "t-1", "in_progress")
	if ExitCode(checkErr) != exitPreconditionRefused {
		t.Fatalf("check exit = %d (%v), want %d — never the precondition's own 7", ExitCode(checkErr), checkErr, exitPreconditionRefused)
	}
	if ExitCode(checkErr) != ExitCode(statusErr) {
		t.Errorf("check exits %d, status exits %d", ExitCode(checkErr), ExitCode(statusErr))
	}
	if checkOut != statusOut {
		t.Errorf("check and status disagree:\n--- check ---\n%s--- status ---\n%s", checkOut, statusOut)
	}
	for _, want := range []string{"precondition 1", "exit 7", "boom"} {
		if !strings.Contains(checkOut, want) {
			t.Errorf("check's output lacks %q:\n%s", want, checkOut)
		}
	}
}

// TestTaskCheckNeverWritesThePlan: a pass, a fail, and a check on a task that
// is already in_progress or done all leave plan.toml byte-for-byte as it was —
// and the in_progress and done checks still run the preconditions.
func TestTaskCheckNeverWritesThePlan(t *testing.T) {
	for _, c := range []struct {
		name, status, run string
		exit              int
	}{
		{"pass", "", "touch ran", 0},
		{"fail", "", "touch ran; exit 1", exitPreconditionRefused},
		{"in_progress", "in_progress", "touch ran", 0},
		{"done", "done", "touch ran", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			precondFixture(t, precondPlan(precondTask{id: "t-1", status: c.status, pres: []string{precondRun(c.run)}}), true)
			before := planBytes(t)
			out, err := runTaskVerb(t, "check", "01-test", "t-1")
			if ExitCode(err) != c.exit {
				t.Fatalf("exit = %d (%v), want %d\n%s", ExitCode(err), err, c.exit, out)
			}
			if !markerExists("ran") {
				t.Errorf("check on a %s task did not run its preconditions", orPending(c.status))
			}
			if !bytes.Equal(planBytes(t), before) {
				t.Errorf("check on a %s task changed plan.toml", orPending(c.status))
			}
		})
	}
}

// TestTaskCheckNeverRunsUnconsented: with no grant, check refuses with exit 4
// naming the trust command, and the precondition does not run.
func TestTaskCheckNeverRunsUnconsented(t *testing.T) {
	precondFixture(t, precondPlan(precondTask{id: "t-1", pres: []string{precondRun("touch ran")}}), false)
	_, err := runTaskVerb(t, "check", "01-test", "t-1")
	if ExitCode(err) != exitPreconditionConsent || !strings.Contains(err.Error(), "dross trust --preconditions 01-test") {
		t.Errorf("exit %d (%v), want %d naming `dross trust --preconditions 01-test`", ExitCode(err), err, exitPreconditionConsent)
	}
	if markerExists("ran") {
		t.Error("check ran an unconsented precondition")
	}
}

// TestTaskCheckWithoutPreconditionsPasses: nothing to check is a pass, said so,
// and needs no grant.
func TestTaskCheckWithoutPreconditionsPasses(t *testing.T) {
	precondFixture(t, precondPlan(precondTask{id: "t-1"}), false)
	var err error
	out := captureStdout(t, func() {
		c := Task()
		c.SetArgs([]string{"check", "01-test", "t-1"})
		c.SetOut(new(bytes.Buffer))
		c.SetErr(new(bytes.Buffer))
		err = c.Execute()
	})
	if err != nil {
		t.Fatalf("a task with no preconditions failed its check: %v", err)
	}
	if !strings.Contains(out, "t-1 has no preconditions") {
		t.Errorf("check did not say the task has none:\n%s", out)
	}
}

// TestTaskCheckIsGatedByThePreconditionGrant: the exec audit sees `task check`
// reach the testlane spawn that runs a precondition, and sees it gate — every
// command reaching that site gates. It reaches it through the precondition
// grant, not the test_command roster, so it must stay out of
// execGatedCommands (TestExecGatedSetIsExplicit pins the roster itself).
func TestTaskCheckIsGatedByThePreconditionGrant(t *testing.T) {
	g := repoExecGraph(t)
	if !g.commandNamed(t, "task check").gates {
		t.Error("the exec audit does not see `task check` gate")
	}
	const spawn = "internal/testlane/spawn.go"
	var reached bool
	for _, s := range g.sitesIn(spawn) {
		if !s.reaches("task check") {
			continue
		}
		reached = true
		where := filepath.Base(s.pos.Filename)
		if !slices.Contains(s.gatedVia, "task check") {
			t.Errorf("%s:%d: `task check` is not among its gated commands: %s", where, s.pos.Line, s.Verdict())
		}
		if len(s.ungatedVia) != 0 {
			t.Errorf("%s:%d is reached ungated: %s", where, s.pos.Line, s.Verdict())
		}
	}
	if !reached {
		t.Errorf("`task check` reaches no spawn site in %s — it no longer runs preconditions", spawn)
	}
	if slices.Contains(execGatedCommands, "task check") {
		t.Error("`task check` joined execGatedCommands; it is gated by the precondition grant, not test_command's")
	}
}

// TestTaskCheckIsDocumented: the CLI golden carries the verb, and README's
// task row lists it.
func TestTaskCheckIsDocumented(t *testing.T) {
	tree := readRepoFile(t, filepath.Join("cmd", "dross", "testdata", "cli_tree.txt"))
	if !strings.Contains(tree, "\ndross task check | ") {
		t.Error("cli_tree.txt has no `dross task check` row")
	}
	if !hasVerb(readmeTaskVerbs(t), "check") {
		t.Error("README's `dross task {…}` row does not list the `check` verb")
	}
}

// readmeTaskVerbs returns the verbs README's `dross task {…}` table row lists.
// The head carries the row's leading pipe so prose naming another
// `dross task {…}` list elsewhere is never read instead.
func readmeTaskVerbs(t *testing.T) []string {
	t.Helper()
	doc := docText(t, "README.md")
	const head = "| dross task {"
	at := strings.Index(doc, head)
	if at < 0 {
		t.Fatal("README.md has no `dross task {…}` row")
	}
	rest := doc[at+len(head):]
	end := strings.Index(rest, "}")
	if end < 0 {
		t.Fatal("README.md's `dross task {…}` row never closes its brace list")
	}
	return strings.Split(rest[:end], ",")
}
