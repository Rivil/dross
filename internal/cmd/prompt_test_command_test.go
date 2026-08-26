package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// suiteRunningPrompts are the three commands that gate on the test suite. They
// are the reason `dross test` exists: each of them used to tell the agent to
// interpolate runtime.test_command into Bash, which is why dross had no
// execution site of its own to point at another machine.
var suiteRunningPrompts = []string{"execute.md", "quick.md", "verify.md"}

// rawInterpolationRE is the shape being forbidden: a fenced line that IS the
// placeholder, which is how these prompts used to tell the agent to run the
// suite. A prompt may still MENTION `runtime.test_command` — all three do, when
// telling the user which line to read before granting trust — so a bare grep
// for the field name would forbid the honest use along with the dishonest one.
var rawInterpolationRE = regexp.MustCompile(`(?m)^\s*<runtime\.test_command>\s*$`)

func promptBody(t *testing.T, name string) string {
	t.Helper()
	root := repoRootForHybridTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// TestPromptsRunDrossTest is c-6, in both directions.
//
// Forbidding the raw interpolation alone would be satisfied by a prompt that
// simply stopped running the suite, so each file must also name the command it
// runs instead. A guard that passes when the behaviour is deleted is not a
// guard.
func TestPromptsRunDrossTest(t *testing.T) {
	for _, name := range suiteRunningPrompts {
		t.Run(name, func(t *testing.T) {
			body := promptBody(t, name)
			if loc := rawInterpolationRE.FindString(body); loc != "" {
				t.Errorf("%s still tells the agent to interpolate the raw command (%q) — the run must go through `dross test`, which is the one consent-gated execution site and the only one that can be pointed at a remote", name, strings.TrimSpace(loc))
			}
			if !strings.Contains(body, "dross test") {
				t.Errorf("%s does not name `dross test` — the suite has to be run by something", name)
			}
		})
	}
}

// TestNoPromptGrantsTrustForTheUser: the consent gate exists so a human reads
// the line the repo supplied. A prompt that told the agent to run `dross trust`
// would defeat it entirely, and it would do so while looking helpful.
func TestNoPromptGrantsTrustForTheUser(t *testing.T) {
	root := repoRootForHybridTest(t)
	dir := filepath.Join(root, "assets", "prompts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read prompts: %v", err)
	}
	// `dross trust --check` is the read-only probe and is exactly what the
	// prompts SHOULD run; the bare granting form is what must not appear as an
	// instruction.
	grantRE := regexp.MustCompile("(?m)^\\s*(?:```)?\\s*dross trust\\s*$")
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body := promptBody(t, e.Name())
		for _, line := range strings.Split(body, "\n") {
			if grantRE.MatchString(line) {
				t.Errorf("%s instructs a bare `dross trust` — consent must be granted by the user, not on their behalf: %q", e.Name(), strings.TrimSpace(line))
			}
		}
	}
}

// TestPromptsExplainTheTransportExitCodes: the codes only do their job if the
// caller reading them knows what they mean. A 3 read as "tests failed" is a
// wrong diagnosis; a 3 read as a pass is worse.
func TestPromptsExplainTheTransportExitCodes(t *testing.T) {
	for _, name := range []string{"execute.md", "quick.md"} {
		t.Run(name, func(t *testing.T) {
			body := promptBody(t, name)
			if !strings.Contains(body, "did not happen") && !strings.Contains(body, "did not run") {
				t.Errorf("%s does not say that a transport exit means the run did not happen — without it, a 3 reads as a verdict", name)
			}
		})
	}
}

// TestDoctorReportsOneRemoteSection: one grant, one section. Reporting it under
// a mutation-specific heading invites the reader to think there is a second
// grant somewhere for tests — and to withdraw this one believing it only
// covered mutation.
func TestDoctorReportsOneRemoteSection(t *testing.T) {
	root := repoRootForHybridTest(t)
	b, err := os.ReadFile(filepath.Join(root, "internal", "cmd", "doctor.go"))
	if err != nil {
		t.Fatalf("read doctor.go: %v", err)
	}
	body := string(b)
	if strings.Contains(body, `Print("Remote mutation:")`) {
		t.Error("doctor still prints a mutation-specific remote section — one grant serves mutation and `dross test`")
	}
	if !strings.Contains(body, `Print("Remote:")`) {
		t.Error("doctor prints no Remote section at all")
	}
	// The remedy lines must name the current verb, or the user is sent to a
	// command whose name says the grant is only about mutation.
	if strings.Contains(body, "dross mutation remote grant <host> <workdir>") {
		t.Error("doctor's grant hint still names the deprecated verb")
	}
}

// TestExecutePromptPassesTaskFilesToTest is c-7: the pre-commit gate has to
// scope itself to the task's OWN declared files, not to the whole repo.
//
// It reads assets/prompts/execute.md directly rather than the installed copy
// under ~/.claude. A prompt edit is not live until `make install` re-links it
// (rule r-01), so a guard reading the installed symlink would pass on a
// developer's machine and say nothing about what the repo actually ships.
func TestExecutePromptPassesTaskFilesToTest(t *testing.T) {
	body := promptBody(t, "execute.md")
	if !strings.Contains(body, "dross test --files") {
		t.Error("execute.md's test gate does not run `dross test --files` — the gate reverted to running the whole suite for every task")
	}
	if !strings.Contains(body, "task.files") {
		t.Error("execute.md does not name task.files as the source of the --files argument — an agent left to choose the paths will reach for the git diff, which covers work the plan never declared")
	}
}

// TestExecutePromptDocumentsUnmatchedExit: an agent that reads a non-zero exit
// it has never been told about has two options, and one of them is to commit
// anyway. Every code the gate can return must be named with its meaning.
//
// The "did not run" phrasing is asserted as well as the numbers, because the
// numbers alone do not carry the distinction that matters: 1 is a verdict about
// the code and the rest are the absence of one.
func TestExecutePromptDocumentsUnmatchedExit(t *testing.T) {
	body := promptBody(t, "execute.md")
	for _, want := range []string{
		"**1**", "**2**", "**3**", "**4**", "**5**", "**6**",
		"nothing was measured",
		"dross trust --lane",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("execute.md does not document %q in its exit-status list", want)
		}
	}
	if !strings.Contains(body, "the run did not happen") {
		t.Error("execute.md does not say the non-verdict exits mean the run did not happen — without it, a 5 or a 6 reads as a test failure and sends the agent hunting a bug in code that never executed")
	}
}
