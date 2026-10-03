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
