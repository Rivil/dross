package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/debugsession"
)

// debugNudgeFixture is a dross repo in cwd with a runnable plan task, so the
// phase's own next step (/dross-execute) is visible beside any debug clause.
func debugNudgeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	scaffoldPhaseWithSpecAndPlan(t, "01-auth", `[phase]
id = "01-auth"
[[task]]
id = "t-1"
wave = 1
title = "schema"
files = ["x.ts"]
covers = ["c-1"]
`)
	return dir
}

// debugAt writes a session and pins its mtime, which orders the nudges.
func debugAt(t *testing.T, dir, slug, status string, bodies map[string]string, at time.Time) string {
	t.Helper()
	p := debugSessionFile(t, dir, slug, status, bodies)
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
	return p
}

func runStatus(t *testing.T) string {
	t.Helper()
	return captureStdout(t, func() {
		if err := runCmd(t, Status()); err != nil {
			t.Fatalf("status: %v", err)
		}
	})
}

func runReentry(t *testing.T) string {
	t.Helper()
	return decodeReentry(t, captureStdout(t, func() {
		if err := runCmd(t, Reentry()); err != nil {
			t.Fatalf("reentry: %v", err)
		}
	}))
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

func debugLines(status string) []string {
	var out []string
	for _, l := range strings.Split(status, "\n") {
		if strings.HasPrefix(l, "debug:") {
			out = append(out, l)
		}
	}
	return out
}

var debugBase = time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)

func TestStatusDebugNothingOpen(t *testing.T) {
	dir := debugNudgeFixture(t)
	bare, bareReentry := runStatus(t), runReentry(t)
	if len(debugLines(bare)) != 0 || strings.Contains(bareReentry, "debug:") {
		t.Fatalf("a nudge with no sessions:\n%s\n%s", bare, bareReentry)
	}
	debugAt(t, dir, "fixed-it", "resolved", nil, debugBase)
	debugAt(t, dir, "gave-up", "abandoned", nil, debugBase.Add(time.Hour))
	if got := runStatus(t); got != bare {
		t.Errorf("closed sessions changed status:\n--- without\n%s--- with\n%s", bare, got)
	}
	if got := runReentry(t); got != bareReentry {
		t.Errorf("closed sessions changed reentry: %q vs %q", got, bareReentry)
	}
}

func TestStatusDebugOneLine(t *testing.T) {
	dir := debugNudgeFixture(t)
	replan := map[string]string{debugsession.HeadingFixAttempts: "- [failed] a\n- [failed] b\n- [failed] c\n"}
	debugAt(t, dir, "cache-miss", "open", replan, debugBase)
	debugAt(t, dir, "flaky-login", "open", nil, debugBase.Add(time.Hour))

	out := runStatus(t)
	lines := debugLines(out)
	if len(lines) != 1 {
		t.Fatalf("debug lines = %q, want exactly one", lines)
	}
	want := "debug:     flaky-login — /dross-debug flaky-login; cache-miss (needs-replan) — /dross-debug cache-miss"
	if lines[0] != want {
		t.Fatalf("debug line = %q, want %q", lines[0], want)
	}
	if !strings.Contains(lastLine(out), "/dross-execute") {
		t.Errorf("the phase's own next step moved: %q", lastLine(out))
	}

	replan[debugsession.HeadingFixAttempts] += "- [replan] it is the cache key\n"
	debugAt(t, dir, "cache-miss", "open", replan, debugBase)
	if lines := debugLines(runStatus(t)); len(lines) != 1 || strings.Contains(lines[0], "needs-replan") || !strings.Contains(lines[0], "cache-miss — /dross-debug cache-miss") {
		t.Fatalf("after a re-plan entry: %q", lines)
	}
}

func TestReentryNamesDebugSession(t *testing.T) {
	dir := debugNudgeFixture(t)
	debugAt(t, dir, "flaky-login", "open", nil, debugBase)
	line := runReentry(t)
	if !strings.HasSuffix(line, " · debug: flaky-login open — /dross-debug flaky-login") {
		t.Fatalf("reentry = %q, want the open session and its command at the end", line)
	}
	if !strings.Contains(line, "next: /dross-execute") {
		t.Errorf("the debug clause displaced the phase's next step: %q", line)
	}

	replan := map[string]string{debugsession.HeadingFixAttempts: "- [failed] a\n- [failed] b\n- [failed] c\n"}
	debugAt(t, dir, "cache-miss", "open", nil, debugBase.Add(time.Hour))
	debugAt(t, dir, "newest", "open", replan, debugBase.Add(2*time.Hour))
	line = runReentry(t)
	if !strings.HasSuffix(line, " · debug: newest needs-replan — /dross-debug newest (+2 more)") {
		t.Fatalf("reentry = %q, want the newest session, labelled needs-replan, plus two more", line)
	}
	if strings.Contains(line, "flaky-login") || strings.Contains(line, "cache-miss") {
		t.Errorf("reentry names more than the newest session: %q", line)
	}
}

// TestDebugClauseStaysOutOfSuggestNext pins where the clause lives. Reentry
// reads the same whether it is appended in reentryLine or at the end of
// suggestNext, so only suggestNext's other consumer can tell: the pause
// snapshot's `- next:` line must stay the phase's own next step.
func TestDebugClauseStaysOutOfSuggestNext(t *testing.T) {
	dir := debugNudgeFixture(t)
	debugAt(t, dir, "flaky-login", "open", nil, debugBase)
	if err := runCmd(t, Pause(), "--auto"); err != nil {
		t.Fatalf("pause --auto: %v", err)
	}
	var next string
	for _, l := range strings.Split(string(debugBytes(t, filepath.Join(dir, ".dross", "handoff.md"))), "\n") {
		if strings.HasPrefix(l, "- next: ") {
			next = l
		}
	}
	if !strings.HasPrefix(next, "- next: /dross-execute") {
		t.Fatalf("pause's next line = %q, want the phase's own next step", next)
	}
	if strings.Contains(next, "debug") || strings.Contains(next, "flaky-login") {
		t.Fatalf("the debug clause leaked into suggestNext: %q", next)
	}
	if want := strings.TrimPrefix(next, "- next: ") + " · debug: flaky-login open — /dross-debug flaky-login"; !strings.HasSuffix(runReentry(t), want) {
		t.Fatalf("reentry = %q, want suggestNext's text followed by the debug clause %q", runReentry(t), want)
	}
}

func TestDebugCloseDropsNudges(t *testing.T) {
	dir := debugNudgeFixture(t)
	p := debugAt(t, dir, "drop-me", "open", nil, debugBase)
	if !strings.Contains(runStatus(t), "drop-me") || !strings.Contains(runReentry(t), "drop-me") {
		t.Fatal("the open session is not nudged")
	}
	raw := debugBytes(t, p)
	out, err := debugsession.CloseText(raw, debugsession.StateAbandoned, "gave up", debugBase)
	if err != nil {
		t.Fatal(err)
	}
	if err := debugsession.Rewrite(dir, "drop-me", raw, out); err != nil {
		t.Fatal(err)
	}
	if s := runStatus(t); strings.Contains(s, "drop-me") {
		t.Errorf("status still names the closed session:\n%s", s)
	}
	if r := runReentry(t); strings.Contains(r, "drop-me") {
		t.Errorf("reentry still names the closed session: %q", r)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("closing removed the session: %v", err)
	}
}

func TestStatusDebugByteEqualReentry(t *testing.T) {
	dir := debugNudgeFixture(t)
	debugAt(t, dir, "flaky-login", "open", nil, debugBase)
	status, reentry := runStatus(t), runReentry(t) // decodeReentry pins systemMessage == additionalContext
	if lastLine(status) != reentry {
		t.Fatalf("status's last line %q != the hook's %q", lastLine(status), reentry)
	}
	if !strings.Contains(reentry, "/dross-debug flaky-login") {
		t.Fatalf("the shared line lacks the debug clause: %q", reentry)
	}
}

func TestDebugNudgeHookSafety(t *testing.T) {
	t.Run("an unreadable store nudges nothing and breaks nothing", func(t *testing.T) {
		dir := debugNudgeFixture(t)
		bare, bareReentry := runStatus(t), runReentry(t)
		debugDir := filepath.Join(dir, ".dross", "debug")

		mustWrite(t, debugDir, "not a directory\n")
		if got := runStatus(t); got != bare {
			t.Errorf(".dross/debug as a file changed status:\n%s", got)
		}
		if got := runReentry(t); got != bareReentry {
			t.Errorf(".dross/debug as a file changed reentry: %q", got)
		}

		if os.Geteuid() == 0 {
			t.Skip("root lists a chmod-000 directory")
		}
		if err := os.Remove(debugDir); err != nil {
			t.Fatal(err)
		}
		debugAt(t, dir, "hidden", "open", nil, debugBase)
		if err := os.Chmod(debugDir, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(debugDir, 0o755) })
		if got := runStatus(t); got != bare {
			t.Errorf("a chmod-000 .dross/debug changed status:\n%s", got)
		}
		if got := runReentry(t); got != bareReentry {
			t.Errorf("a chmod-000 .dross/debug changed reentry: %q", got)
		}
	})

	t.Run("a mangled status line still nudges", func(t *testing.T) {
		dir := debugNudgeFixture(t)
		p := debugAt(t, dir, "no-status", "open", nil, debugBase)
		mustWrite(t, p, strings.Replace(string(debugBytes(t, p)), "status: open\n", "", 1))
		if err := os.Chtimes(p, debugBase, debugBase); err != nil {
			t.Fatal(err)
		}
		debugAt(t, dir, "odd-status", "fixed", nil, debugBase.Add(time.Hour))
		status := runStatus(t)
		lines := debugLines(status)
		if len(lines) != 1 || !strings.Contains(lines[0], "/dross-debug no-status") || !strings.Contains(lines[0], "/dross-debug odd-status") {
			t.Fatalf("debug lines = %q, want both mangled sessions named", lines)
		}
		if !strings.HasPrefix(lastLine(status), "you are here: ") {
			t.Fatalf("status lost its footer: %q", lastLine(status))
		}
		if r := runReentry(t); !strings.Contains(r, "debug: odd-status open — /dross-debug odd-status (+1 more)") {
			t.Fatalf("reentry = %q", r)
		}
	})
}
