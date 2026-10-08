package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/ctxnudge"
	"github.com/Rivil/dross/internal/telemetry"
)

// nudgeFixture is a dross repo with a planned phase and a runnable task, cwd'd
// into, with HOME isolated so defaults.toml is the test's own.
func nudgeFixture(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
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
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// setThreshold writes ~/.claude/dross/defaults.toml with body under [context].
func setThreshold(t *testing.T, body string) {
	t.Helper()
	dir, err := GlobalDir()
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "defaults.toml"), "[context]\n"+body+"\n")
}

// nudgePayload is a main-agent PostToolUse payload for session, its transcript
// a single main-agent turn of tokens.
func nudgePayload(t *testing.T, cwd, session string, tokens int64) string {
	t.Helper()
	tr := filepath.Join(t.TempDir(), "session.jsonl")
	line := `{"type":"assistant","isSidechain":false,"message":{"model":"claude-opus-5-5","usage":{"input_tokens":0,"cache_read_input_tokens":` +
		jsonInt(tokens) + `,"cache_creation_input_tokens":0}}}`
	mustWrite(t, tr, line+"\n")
	b, err := json.Marshal(map[string]string{
		"hook_event_name": "PostToolUse",
		"session_id":      session,
		"transcript_path": tr,
		"cwd":             cwd,
		"tool_name":       "Bash",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// runNudge runs `dross hooks nudge` with stdin and returns stdout, stderr and
// the error RunE returned.
func runNudge(t *testing.T, stdin string) (string, string, error) {
	t.Helper()
	c := Hooks()
	var out, errb bytes.Buffer
	c.SetIn(strings.NewReader(stdin))
	c.SetOut(&out)
	c.SetErr(&errb)
	c.SetArgs([]string{"nudge"})
	err := c.Execute()
	return out.String(), errb.String(), err
}

// nudgeLine decodes the hook envelope and returns its line.
func nudgeLine(t *testing.T, out string) string {
	t.Helper()
	var env struct {
		SystemMessage string `json:"systemMessage"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("nudge stdout is not the hook envelope: %v\n%s", err, out)
	}
	return env.SystemMessage
}

// TestNudgeHookNeverFails: whatever reaches it, the hook exits 0 and prints
// nothing — no stdout, and no stderr a hook runner could surface.
func TestNudgeHookNeverFails(t *testing.T) {
	root := nudgeFixture(t)
	over := nudgePayload(t, root, "s", 900_000)

	incomplete := t.TempDir()
	mustWrite(t, filepath.Join(incomplete, ".dross", "state.json"), "{}")

	for _, c := range []struct {
		name  string
		stdin string
		setup func(t *testing.T)
	}{
		{"empty stdin", "", nil},
		{"garbage stdin", "\x00\xffnot json at all", nil},
		{"undecodable defaults.toml", over, func(t *testing.T) { setThreshold(t, `threshold = "150k"`) }},
		{"negative threshold", over, func(t *testing.T) { setThreshold(t, "threshold = -5") }},
		{"panicking seam", over, func(t *testing.T) {
			orig := nudgeRun
			nudgeRun = func([]byte, ctxnudge.Env) []byte { panic("boom") }
			t.Cleanup(func() { nudgeRun = orig })
		}},
		// A live threshold, so only the incomplete root can keep it silent —
		// the earlier subtests leave an invalid one behind in the shared HOME.
		{"incomplete .dross", nudgePayload(t, incomplete, "s", 900_000), func(t *testing.T) { setThreshold(t, "threshold = 1") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.setup != nil {
				c.setup(t)
			}
			out, errOut, err := runNudge(t, c.stdin)
			if err != nil || out != "" || errOut != "" {
				t.Errorf("err=%v stdout=%q stderr=%q; want nil and both empty", err, out, errOut)
			}
		})
	}
}

// TestNudgeVerbSkipsTelemetry: the hook runs on every tool call, so it never
// writes a usage event — while an ordinary command still does.
func TestNudgeVerbSkipsTelemetry(t *testing.T) {
	telemetryCovEnable(t)
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Hooks(), Status())
	find := func(args ...string) *cobra.Command {
		c, _, err := root.Find(args)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	for range 3 {
		RecordCLIEvent(find("hooks", "nudge"), time.Millisecond, nil)
	}
	evs, err := telemetry.Load(telemetryPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(evs) != 0 {
		t.Fatalf("the nudge hook wrote %d telemetry events", len(evs))
	}
	RecordCLIEvent(find("status"), time.Millisecond, nil)
	if evs, _ := telemetry.Load(telemetryPath()); len(evs) != 1 {
		t.Errorf("dross status wrote %d events, want 1 — the skip is too wide", len(evs))
	}
}

// reentryNext is the "next: …" segment `dross reentry` prints right now.
func reentryNext(t *testing.T) string {
	t.Helper()
	out := captureStdout(t, func() {
		if err := runCmd(t, Reentry()); err != nil {
			t.Fatal(err)
		}
	})
	_, next, ok := strings.Cut(decodeReentry(t, out), " — next: ")
	if !ok {
		t.Fatalf("the re-entry line has no next segment: %s", out)
	}
	return next
}

// TestNudgeNamesSessionStartReentry: the nudge names exactly the command the
// SessionStart line will print after /clear (locked nudge_content).
func TestNudgeNamesSessionStartReentry(t *testing.T) {
	root := nudgeFixture(t)
	want := reentryNext(t)
	out, _, _ := runNudge(t, nudgePayload(t, root, "s", 160_000))
	line := nudgeLine(t, out)
	if !strings.Contains(line, "/clear, then "+want+" · ") {
		t.Errorf("nudge %q does not name the re-entry %q", line, want)
	}
	if !strings.Contains(want, "/dross-execute") {
		t.Errorf("fixture's re-entry %q is not the runnable-task command — the comparison proves nothing", want)
	}
}

// TestNudgeNamesDebugReentry: an open debug session rides the re-entry, as it
// does on the SessionStart line.
func TestNudgeNamesDebugReentry(t *testing.T) {
	root := nudgeFixture(t)
	debugSessionFile(t, root, "flaky-push", "open", nil)
	want := reentryNext(t)
	if !strings.Contains(want, "/dross-debug flaky-push") {
		t.Fatalf("`dross reentry` does not name the open session: %q", want)
	}
	out, _, _ := runNudge(t, nudgePayload(t, root, "s", 160_000))
	if line := nudgeLine(t, out); !strings.Contains(line, want) {
		t.Errorf("nudge %q lacks the re-entry %q", line, want)
	}
}

// TestNudgeCadenceEndToEnd: one session — the threshold nudges, the same band
// stays quiet, the next 50k nudges again (locked nudge_cadence).
func TestNudgeCadenceEndToEnd(t *testing.T) {
	root := nudgeFixture(t)
	for _, c := range []struct {
		tokens int64
		emits  bool
	}{{150_000, true}, {160_000, false}, {200_000, true}} {
		out, _, err := runNudge(t, nudgePayload(t, root, "one-session", c.tokens))
		if err != nil {
			t.Fatal(err)
		}
		if (out != "") != c.emits {
			t.Errorf("at %d: emitted %q, want emit=%v", c.tokens, out, c.emits)
		}
	}
}

// TestNudgeOffSwitch: threshold 0 is off — no output, and nothing written
// under .dross/gate, however large the context.
func TestNudgeOffSwitch(t *testing.T) {
	root := nudgeFixture(t)
	setThreshold(t, "threshold = 0")
	out, errOut, err := runNudge(t, nudgePayload(t, root, "s", 900_000))
	if err != nil || out != "" || errOut != "" {
		t.Errorf("threshold 0: err=%v stdout=%q stderr=%q", err, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(root, ".dross", "gate", "nudge")); !os.IsNotExist(err) {
		t.Errorf("threshold 0 wrote under .dross/gate/nudge (stat err = %v)", err)
	}
}

// TestNudgeHookConstant: the wired string is the verb's command path.
func TestNudgeHookConstant(t *testing.T) {
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Hooks())
	c, _, err := root.Find([]string{"hooks", "nudge"})
	if err != nil || c.CommandPath() != NudgeHook {
		t.Errorf("hooks nudge resolves to %q (%v), want %q", c.CommandPath(), err, NudgeHook)
	}
}
