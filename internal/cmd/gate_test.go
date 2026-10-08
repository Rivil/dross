package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/gate"
	"github.com/Rivil/dross/internal/telemetry"
)

// runGateVerb runs `dross gate <verb>` with stdin, returning what it wrote and
// its error — the cobra streams, not the process's, so nothing leaks between
// tests.
func runGateVerb(t *testing.T, verb, stdin string) (string, string, error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir()) // gates.toml is read from HOME
	c := Gate()
	var out, errb bytes.Buffer
	c.SetArgs([]string{verb})
	c.SetIn(strings.NewReader(stdin))
	c.SetOut(&out)
	c.SetErr(&errb)
	err := c.Execute()
	return out.String(), errb.String(), err
}

func bashPayload(command string) string {
	return `{"hook_event_name":"PreToolUse","session_id":"s","tool_name":"Bash","tool_input":{"command":` +
		strings.ReplaceAll(`"`+command+`"`, `\`, `\\`) + `},"cwd":"` + os.TempDir() + `"}`
}

// TestGateCheckRefusalExitsTwo: Claude Code blocks a PreToolUse call only on
// exit 2 — exit 1 is a non-blocking error, which would let the call run. The
// refusal reaches stderr once: main prints the error, and the verb keeps
// cobra from printing it a second time.
func TestGateCheckRefusalExitsTwo(t *testing.T) {
	out, errOut, err := runGateVerb(t, "check", bashPayload("pass-cli item view x | head"))
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit = %d (%v), want 2", code, err)
	}
	if out != "" || errOut != "" {
		t.Errorf("the verb printed the refusal itself (stdout %q, stderr %q) — main prints it, once", out, errOut)
	}
	msg := err.Error()
	if !strings.Contains(msg, "secret-stream") || strings.Count(msg, "Use:") != 1 {
		t.Errorf("refusal = %q, want the rule and one remedy", msg)
	}
}

func TestGateCheckSilentAllow(t *testing.T) {
	for _, p := range []string{
		bashPayload("ls -la"),
		`{"tool_name":"Glob","tool_input":{"pattern":"**/*.go"},"cwd":"/"}`,
	} {
		out, errOut, err := runGateVerb(t, "check", p)
		if err != nil || out != "" || errOut != "" {
			t.Errorf("allowed payload %s: err=%v stdout=%q stderr=%q, want silence", p, err, out, errOut)
		}
	}
}

func TestGateCheckUnparseablePayload(t *testing.T) {
	for _, in := range []string{"{", "", "not json at all", `{"tool_input":{}}`} {
		out, errOut, err := runGateVerb(t, "check", in)
		if err != nil || out != "" || strings.Count(errOut, "\n") != 1 {
			t.Errorf("payload %q: err=%v stdout=%q stderr=%q, want exit 0 and one warning line", in, err, out, errOut)
		}
	}
}

func TestGateCheckRecoversPanics(t *testing.T) {
	orig := GateEngine.Check
	t.Cleanup(func() { GateEngine.Check = orig })
	GateEngine.Check = func([]byte, gate.Env) gate.Result { panic("decode exploded") }
	_, errOut, err := runGateVerb(t, "check", bashPayload("ls"))
	if err != nil || strings.Count(errOut, "\n") != 1 || !strings.Contains(errOut, "allowing the call") {
		t.Errorf("a panic outside any gate: err=%v stderr=%q, want exit 0 and one warning", err, errOut)
	}
}

func TestGateRecordNeverBlocks(t *testing.T) {
	orig := GateEngine.Record
	t.Cleanup(func() { GateEngine.Record = orig })
	for _, c := range []struct {
		name     string
		record   func([]byte, gate.Env) []string
		in       string
		wantWarn bool
	}{
		{"the real recorders", orig, bashPayload("ls"), false},
		{"garbage", orig, "{", true},
		{"a recorder error", func([]byte, gate.Env) []string { return []string{"dross gate record x: disk full"} }, bashPayload("ls"), true},
		{"a panic", func([]byte, gate.Env) []string { panic("boom") }, bashPayload("ls"), true},
	} {
		GateEngine.Record = c.record
		out, errOut, err := runGateVerb(t, "record", c.in)
		if err != nil || out != "" {
			t.Errorf("%s: err=%v stdout=%q, want exit 0 and nothing on stdout", c.name, err, out)
		}
		if got := errOut != ""; got != c.wantWarn {
			t.Errorf("%s: stderr=%q, want a warning=%v", c.name, errOut, c.wantWarn)
		}
	}
}

// TestGateVerbsSkipTelemetry: the hook verbs run before every tool call, and a
// refusal's text can name a secret path.
func TestGateVerbsSkipTelemetry(t *testing.T) {
	telemetryCovEnable(t)
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Gate(), Status())
	find := func(args ...string) *cobra.Command {
		c, _, err := root.Find(args)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	for i := 0; i < 3; i++ {
		RecordCLIEvent(find("gate", "check"), time.Millisecond, errors.New("dross gate secret-read refused: ~/.ssh/id_ed25519"))
	}
	RecordCLIEvent(find("gate", "record"), time.Millisecond, nil)
	evs, err := telemetry.Load(telemetryPath())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(evs) != 0 {
		t.Fatalf("the gate verbs wrote %d telemetry events", len(evs))
	}
	RecordCLIEvent(find("status"), time.Millisecond, nil)
	if evs, _ := telemetry.Load(telemetryPath()); len(evs) != 1 {
		t.Errorf("dross status wrote %d events, want 1 — the skip is too wide", len(evs))
	}
}

func TestGateHookConstants(t *testing.T) {
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Gate())
	for want, args := range map[string][]string{GateCheckHook: {"gate", "check"}, GateRecordHook: {"gate", "record"}} {
		c, _, err := root.Find(args)
		if err != nil || c.CommandPath() != want {
			t.Errorf("%v resolves to %q (%v), want the hook constant %q", args, c.CommandPath(), err, want)
		}
	}
}

// TestGateRecordSubagentStopNeverBlocks: the SubagentStop hook runs the same
// verb, and a finished subagent's payload — which names no tool — must never
// fail the hook or print to stdout.
func TestGateRecordSubagentStopNeverBlocks(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "internal", "gate", "testdata", "reviewer_subagent_stop.json"))
	if err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runGateVerb(t, "record", string(b))
	if err != nil || out != "" {
		t.Fatalf("err=%v stdout=%q, want exit 0 and nothing on stdout (stderr %q)", err, out, errOut)
	}
}
