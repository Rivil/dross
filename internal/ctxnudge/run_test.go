package ctxnudge

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// droot is a dross repo root: a directory whose .dross/ holds a project.toml.
func droot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".dross", "project.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// payload loads a captured fixture and points it at this test's repo,
// transcript and session. An empty value leaves the fixture's own.
func payload(t *testing.T, fixture, cwd, transcript, session string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"cwd": cwd, "transcript_path": transcript, "session_id": session} {
		if v != "" {
			m[k] = v
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// stubEnv is a test Env: a fixed threshold (or error) and a re-entry func.
type stubEnv struct {
	threshold int64
	err       error
	reentry   func(string) string
}

func (s stubEnv) Threshold() (int64, error)  { return s.threshold, s.err }
func (s stubEnv) Reentry(root string) string { return s.reentry(root) }

// env is a fixed threshold and a counting re-entry stub.
func env(threshold int64, calls *int) Env {
	return stubEnv{threshold: threshold, reentry: func(string) string {
		if calls != nil {
			*calls++
		}
		return "/dross-execute — run the next task"
	}}
}

// claims lists the nudge claims written under root.
func claims(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".dross", "gate", "nudge"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestSubagentFireNeitherNudgesNorClaims: a call inside a subagent carries the
// main session's transcript_path (the capture shows it), so only agent_id can
// keep it from nudging — and it must not burn the band either.
func TestSubagentFireNeitherNudgesNorClaims(t *testing.T) {
	root := droot(t)
	tr := writeTranscript(t, mainLine(180_000))
	if out := Run(payload(t, "posttooluse_subagent.json", root, tr, "s"), env(150_000, nil)); out != nil {
		t.Errorf("a subagent fire nudged: %s", out)
	}
	if c := claims(t, root); len(c) != 0 {
		t.Errorf("a subagent fire claimed %v", c)
	}
	// The band is still there for the main agent.
	if out := Run(payload(t, "posttooluse_main.json", root, tr, "s"), env(150_000, nil)); out == nil {
		t.Error("the main agent's fire did not nudge after a subagent fire")
	}
}

// TestSidechainTranscriptPathIsSilent: a transcript_path naming a subagent's
// all-sidechain file yields no context, so nothing nudges.
func TestSidechainTranscriptPathIsSilent(t *testing.T) {
	root := droot(t)
	side := filepath.Join("testdata", "subagent_tail.jsonl")
	if out := Run(payload(t, "posttooluse_main.json", root, side, "s"), env(1, nil)); out != nil {
		t.Errorf("an all-sidechain transcript nudged: %s", out)
	}
}

// TestEnvelopeNeverBlocks: the one emit is a single JSON object that shows the
// line to the user and the model alike and carries no blocking key; the same
// band never emits twice.
func TestEnvelopeNeverBlocks(t *testing.T) {
	root := droot(t)
	tr := writeTranscript(t, mainLine(151_409))
	p := payload(t, "posttooluse_main.json", root, tr, "s")
	out := Run(p, env(150_000, nil))
	if out == nil {
		t.Fatal("no nudge over the threshold")
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var top map[string]json.RawMessage
	if err := dec.Decode(&top); err != nil {
		t.Fatalf("output is not a JSON object: %v: %s", err, out)
	}
	if dec.More() {
		t.Errorf("output holds more than one JSON value: %s", out)
	}
	var env2 struct {
		SystemMessage      string `json:"systemMessage"`
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &env2); err != nil {
		t.Fatal(err)
	}
	want := Line(151_409, 150_000, "/dross-execute — run the next task")
	if env2.SystemMessage != want || env2.HookSpecificOutput.AdditionalContext != want {
		t.Errorf("systemMessage %q / additionalContext %q, want both %q",
			env2.SystemMessage, env2.HookSpecificOutput.AdditionalContext, want)
	}
	if env2.HookSpecificOutput.HookEventName != "PostToolUse" {
		t.Errorf("hookEventName = %q, want PostToolUse", env2.HookSpecificOutput.HookEventName)
	}
	for _, key := range []string{"decision", "continue", "permissionDecision", "stopReason"} {
		if strings.Contains(string(out), `"`+key+`"`) {
			t.Errorf("the envelope carries %q, which can block or stop a call: %s", key, out)
		}
	}
	if again := Run(p, env(150_000, nil)); again != nil {
		t.Errorf("the same band nudged twice: %s", again)
	}
}

// TestReadsPayloadTranscriptPath: the transcript is the one the payload names,
// wherever it is — never a path derived from the session id.
func TestReadsPayloadTranscriptPath(t *testing.T) {
	root := droot(t)
	dir := filepath.Join(t.TempDir(), "elsewhere", "entirely")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tr := filepath.Join(dir, "not-the-session-id.jsonl")
	if err := os.WriteFile(tr, []byte(mainLine(160_000)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := Run(payload(t, "posttooluse_main.json", root, tr, "unrelated-session"), env(150_000, nil)); out == nil {
		t.Error("no nudge from the transcript the payload named")
	}
}

// TestSilentPaths: every reason not to nudge is nil output and no claim.
func TestSilentPaths(t *testing.T) {
	over := writeTranscript(t, mainLine(500_000))
	outside := t.TempDir()
	halfBuilt := t.TempDir()
	if err := os.MkdirAll(filepath.Join(halfBuilt, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	withEvent := func(root, event string) []byte {
		var m map[string]any
		_ = json.Unmarshal(payload(t, "posttooluse_main.json", root, over, "s"), &m)
		m["hook_event_name"] = event
		b, _ := json.Marshal(m)
		return b
	}
	broken := stubEnv{err: errors.New("[context] threshold = -1"), reentry: func(string) string { return "x" }}

	for _, c := range []struct {
		name    string
		payload func(root string) []byte
		env     Env
	}{
		{"non-JSON stdin", func(string) []byte { return []byte("not json") }, env(1, nil)},
		{"a JSON array", func(string) []byte { return []byte(`[{"hook_event_name":"PostToolUse"}]`) }, env(1, nil)},
		{"cwd outside a dross repo", func(string) []byte { return payload(t, "posttooluse_main.json", outside, over, "s") }, env(1, nil)},
		{"cwd in a .dross with no project.toml", func(string) []byte { return payload(t, "posttooluse_main.json", halfBuilt, over, "s") }, env(1, nil)},
		{"empty session_id", func(root string) []byte {
			var m map[string]any
			_ = json.Unmarshal(payload(t, "posttooluse_main.json", root, over, ""), &m)
			m["session_id"] = ""
			b, _ := json.Marshal(m)
			return b
		}, env(1, nil)},
		{"missing transcript", func(root string) []byte {
			return payload(t, "posttooluse_main.json", root, filepath.Join(root, "gone.jsonl"), "s")
		}, env(1, nil)},
		{"threshold error", func(root string) []byte { return payload(t, "posttooluse_main.json", root, over, "s") }, broken},
		{"UserPromptSubmit", func(root string) []byte { return withEvent(root, "UserPromptSubmit") }, env(1, nil)},
		{"SessionStart", func(root string) []byte { return withEvent(root, "SessionStart") }, env(1, nil)},
		{"below the threshold", func(root string) []byte { return payload(t, "posttooluse_main.json", root, over, "s") }, env(600_000, nil)},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := droot(t)
			if out := Run(c.payload(root), c.env); out != nil {
				t.Errorf("emitted %s", out)
			}
			if cl := claims(t, root); len(cl) != 0 {
				t.Errorf("claimed %v", cl)
			}
		})
	}

	// The off switch never opens the transcript: a FIFO there would block a
	// read, so threshold 0 must return before reaching it.
	if runtime.GOOS == "windows" {
		return
	}
	if _, err := exec.LookPath("mkfifo"); err != nil {
		return
	}
	root := droot(t)
	fifo := filepath.Join(t.TempDir(), "fifo.jsonl")
	if out, err := exec.Command("mkfifo", fifo).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}
	done := make(chan []byte, 1)
	go func() { done <- Run(payload(t, "posttooluse_main.json", root, fifo, "s"), env(0, nil)) }()
	select {
	case out := <-done:
		if out != nil {
			t.Errorf("threshold 0 emitted %s", out)
		}
	case <-time.After(time.Second):
		t.Fatal("threshold 0 blocked on the transcript — the off switch opened it")
	}
}

// TestReentryOnlyWhenDue: Reentry runs git, so it is never called on the hot
// path — below the threshold, or when the band is already claimed.
func TestReentryOnlyWhenDue(t *testing.T) {
	root := droot(t)
	calls := 0
	below := writeTranscript(t, mainLine(100_000))
	Run(payload(t, "posttooluse_main.json", root, below, "s"), env(150_000, &calls))
	if calls != 0 {
		t.Errorf("Reentry called %d times below the threshold", calls)
	}
	over := writeTranscript(t, mainLine(160_000))
	Run(payload(t, "posttooluse_main.json", root, over, "s"), env(150_000, &calls))
	if calls != 1 {
		t.Fatalf("Reentry called %d times for the one due nudge, want 1", calls)
	}
	Run(payload(t, "posttooluse_main.json", root, over, "s"), env(150_000, &calls))
	if calls != 1 {
		t.Errorf("Reentry called again (%d) for an already-claimed band", calls)
	}
}

// TestFailedComposeKeepsBucket: a compose that panics claims nothing, so the
// next fire still nudges that band.
func TestFailedComposeKeepsBucket(t *testing.T) {
	root := droot(t)
	tr := writeTranscript(t, mainLine(160_000))
	p := payload(t, "posttooluse_main.json", root, tr, "s")
	panicky := stubEnv{threshold: 150_000, reentry: func(string) string { panic("git exploded") }}
	if out := Run(p, panicky); out != nil {
		t.Errorf("a panicking compose emitted %s", out)
	}
	if c := claims(t, root); len(c) != 0 {
		t.Fatalf("a failed compose claimed %v", c)
	}
	if out := Run(p, env(150_000, nil)); out == nil {
		t.Error("the band was lost to the failed compose")
	}
}

var uuidShaped = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// TestCapturedFixturesScrubbed: the captured payloads keep their shape but no
// home path or real session id.
func TestCapturedFixturesScrubbed(t *testing.T) {
	for _, name := range []string{"posttooluse_main.json", "posttooluse_subagent.json"} {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, leak := range []string{"/Users/", "/home/"} {
			if strings.Contains(s, leak) {
				t.Errorf("%s carries a home path (%s)", name, leak)
			}
		}
		if m := uuidShaped.FindString(s); m != "" {
			t.Errorf("%s carries a UUID-shaped id", name)
		}
	}
}

// TestRunUnder50ms: the in-process hook path keeps c-7's budget on a 50 MB
// transcript, both when it stays silent and when it emits.
func TestRunUnder50ms(t *testing.T) {
	tr := bigTranscript(t) // newest main entry: 175,000 tokens
	for _, c := range []struct {
		name      string
		threshold int64
		emits     bool
	}{
		{"below threshold", 200_000, false},
		{"emit", 150_000, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := droot(t)
			best := time.Duration(math.MaxInt64)
			for i := range 5 {
				p := payload(t, "posttooluse_main.json", root, tr, fmt.Sprintf("s%d", i))
				if len(p) > 64<<10 {
					t.Fatalf("payload is %d bytes; the budget is for ≤ 64 KiB", len(p))
				}
				start := time.Now()
				out := Run(p, env(c.threshold, nil))
				best = min(best, time.Since(start))
				if (out != nil) != c.emits {
					t.Fatalf("emitted = %v, want %v", out != nil, c.emits)
				}
			}
			if best >= 50*time.Millisecond {
				t.Errorf("best of 5 runs took %v; budget is 50ms", best)
			}
		})
	}
}
