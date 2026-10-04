package gate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCapturedAskUserQuestionFixture pins the AskUserQuestion PostToolUse shape
// the pair-approval recorder reads. testdata/askuserquestion_post.json is not
// hand-written: it was captured byte for byte from a live Claude Code 2.1.288
// session through a temporary stdin-dump hook, with the human picking
// "approve t-3". The chosen label must be readable from tool_response alone —
// tool_input carries the answers too, but only the response is the tool's own
// report of what the human chose.
func TestCapturedAskUserQuestionFixture(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "askuserquestion_post.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if p.Event != "PostToolUse" || p.ToolName != "AskUserQuestion" {
		t.Fatalf("fixture is %q/%q, want PostToolUse/AskUserQuestion", p.Event, p.ToolName)
	}

	// A real capture carries the envelope Claude Code wraps around every hook
	// call; a hand-written stand-in has no reason to.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"session_id", "transcript_path", "tool_use_id"} {
		if str(top, key) == "" {
			t.Errorf("fixture lacks %s: not a real captured payload", key)
		}
	}
	if id := str(top, "tool_use_id"); !strings.HasPrefix(id, "toolu_") {
		t.Errorf("tool_use_id %q is not a Claude Code tool-use id: not a real captured payload", id)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(p.Response, &resp); err != nil {
		t.Fatalf("tool_response is not an object: %v", err)
	}
	var answers map[string]string
	_ = json.Unmarshal(resp["answers"], &answers)
	if len(answers) == 0 {
		t.Fatal("tool_response carries no readable answers: the recorder cannot see the human's choice — " +
			"stop and re-decide pair_approval_signal before building on this shape")
	}
	if len(answers) != 1 {
		t.Fatalf("tool_response has %d answers, the capture answered one question: %v", len(answers), answers)
	}

	var questions []struct {
		Question string
		Options  []struct{ Label string }
	}
	if err := json.Unmarshal(resp["questions"], &questions); err != nil || len(questions) != 1 {
		t.Fatalf("tool_response questions = %d (err %v), want the one question asked", len(questions), err)
	}
	q := questions[0]
	chosen, ok := answers[q.Question]
	if !ok {
		t.Fatalf("tool_response answers %v are not keyed by the question text %q", answers, q.Question)
	}
	if chosen != "approve t-3" {
		t.Fatalf("captured answer = %q, want the human's chosen label %q", chosen, "approve t-3")
	}
	offered := false
	for _, o := range q.Options {
		offered = offered || o.Label == chosen
	}
	if !offered {
		t.Fatalf("answer %q is not one of the offered option labels %+v", chosen, q.Options)
	}
}

// The Agent fixtures below were captured byte for byte from headless
// `claude -p` sessions (Claude Code 2.1.289, subscription OAuth) in a scratch
// repo carrying a canary CLAUDE.md, a scratch dross-task-reviewer definition
// (omitClaudeMd: true, tools Read) and a temporary stdin-dump PostToolUse hook.
// They are the go/no-go for review_pass_signal: the solo-review recorder reads
// the reviewer's verdict from the Agent call's own PostToolUse payload.

// loadCapture decodes a captured Agent payload and checks it carries the
// envelope only a real Claude Code hook call has.
func loadCapture(t *testing.T, name string) (Payload, map[string]any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if p.Event != "PostToolUse" || (p.ToolName != "Agent" && p.ToolName != "Task") {
		t.Fatalf("%s is %q/%q, want PostToolUse/Agent (or its older name Task)", name, p.Event, p.ToolName)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"session_id", "transcript_path", "tool_use_id"} {
		if str(top, key) == "" {
			t.Errorf("%s lacks %s: not a real captured payload", name, key)
		}
	}
	if id := str(top, "tool_use_id"); !strings.HasPrefix(id, "toolu_") {
		t.Errorf("%s tool_use_id %q is not a Claude Code tool-use id: not a real captured payload", name, id)
	}
	if got := p.Field("subagent_type"); got != "dross-task-reviewer" {
		t.Fatalf("%s subagent_type = %q, want dross-task-reviewer", name, got)
	}
	var resp map[string]any
	if err := json.Unmarshal(p.Response, &resp); err != nil {
		t.Fatalf("%s tool_response is not an object: %v", name, err)
	}
	return p, resp
}

// replyText joins the text blocks of a completed Agent call's tool_response.
func replyText(resp map[string]any) string {
	var b strings.Builder
	blocks, _ := resp["content"].([]any)
	for _, blk := range blocks {
		m, _ := blk.(map[string]any)
		if m["type"] == "text" {
			s, _ := m["text"].(string)
			b.WriteString(s)
		}
	}
	return b.String()
}

// TestCapturedAgentReviewerFixture pins the shape the recorder reads: a
// foreground spawn (run_in_background explicitly false) completes inside the
// tool call, so its PostToolUse tool_response carries the reviewer's whole
// reply — and exactly one dross-verdict fence — with no transcript to open.
func TestCapturedAgentReviewerFixture(t *testing.T) {
	p, resp := loadCapture(t, "agent_reviewer_post.json")
	if bg, ok := p.Input["run_in_background"].(bool); !ok || bg {
		t.Fatalf("run_in_background = %v (present %v), want an explicit false: the capture is the foreground spawn", bg, ok)
	}
	if resp["status"] != "completed" {
		t.Fatalf("tool_response status = %v, want completed: a foreground spawn returns only when the reviewer has finished", resp["status"])
	}
	if resp["agentType"] != "dross-task-reviewer" {
		t.Errorf("tool_response agentType = %v, want dross-task-reviewer", resp["agentType"])
	}
	reply := replyText(resp)
	if reply == "" {
		t.Fatal("tool_response carries no reply text: the recorder cannot see the verdict — " +
			"stop and re-decide review_pass_signal before building on this shape")
	}
	if n := strings.Count(reply, "```dross-verdict"); n != 1 {
		t.Fatalf("reply carries %d dross-verdict fences, want exactly 1:\n%s", n, reply)
	}
}

// TestCapturedReviewerIsolation is the omitClaudeMd canary. The scratch repo's
// CLAUDE.md told every reader to repeat CANARY-7f3a9c41; the main session did
// (its transcript carries the token), the reviewer must not. The reviewer
// answered yes/no probes rather than quoting its context, so a failed
// isolation could never publish the user's own CLAUDE.md through this file.
func TestCapturedReviewerIsolation(t *testing.T) {
	_, resp := loadCapture(t, "agent_reviewer_post.json")
	reply := replyText(resp)
	for _, probe := range []string{"probe-instructions", "probe-canary", "probe-memory"} {
		if !strings.Contains(reply, probe+": no") {
			t.Errorf("reply does not answer %s: no — omitClaudeMd not honoured (or memory inherited); re-decide reviewer_isolation:\n%s", probe, reply)
		}
		if strings.Contains(reply, probe+": yes") {
			t.Errorf("reply answers %s: yes — the reviewer saw context it must not", probe)
		}
	}
	if strings.Contains(reply, "CANARY-7f3a9c41") {
		t.Error("reply quotes the scratch CLAUDE.md canary: the project CLAUDE.md reached the reviewer")
	}
}

// TestCapturedBackgroundAgentFixture pins why the reviewer must be a
// foreground spawn: a background launch's PostToolUse is only the launch
// acknowledgement, so it carries no verdict for the recorder to read.
func TestCapturedBackgroundAgentFixture(t *testing.T) {
	p, resp := loadCapture(t, "agent_background_post.json")
	if bg, _ := p.Input["run_in_background"].(bool); !bg {
		t.Fatal("background fixture's tool_input lacks run_in_background: true")
	}
	if resp["status"] != "async_launched" {
		t.Errorf("tool_response status = %v, want async_launched", resp["status"])
	}
	if strings.Contains(string(p.Response), "dross-verdict") {
		t.Fatal("a background launch's tool_response carries a dross-verdict fence: the foreground requirement may be obsolete — re-check the recorder")
	}
}

// TestCapturedDefaultAgentIsAsync pins the Claude Code 2.1.289 default: an
// Agent call that does not set run_in_background launches asynchronously too.
// Leaving the flag out is therefore not a foreground spawn — the prompts must
// pass run_in_background: false explicitly.
func TestCapturedDefaultAgentIsAsync(t *testing.T) {
	p, resp := loadCapture(t, "agent_default_async_post.json")
	if _, set := p.Input["run_in_background"]; set {
		t.Fatal("default-async fixture sets run_in_background; it must be the call that left it out")
	}
	if resp["status"] != "async_launched" {
		t.Errorf("tool_response status = %v, want async_launched: the default spawn launched in the background", resp["status"])
	}
	if strings.Contains(string(p.Response), "dross-verdict") {
		t.Fatal("a default spawn's tool_response carries a verdict: omitting run_in_background may be foreground again — revisit the prompts' explicit false")
	}
}
