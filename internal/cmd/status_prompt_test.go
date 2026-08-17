package cmd

import (
	"strings"
	"testing"
)

// These assertions read assets/prompts/ directly through promptContent (see
// ship_prompt_test.go) — r-01: a prompt edit is only live after `make install`,
// so the installed copy is the wrong thing to test.

// TestPullConsumersReadTheEnvelope proves the consumer half of c-2. The CLI
// emitting {issues, error} buys nothing if the prompts still read the result as
// an array — they would index a JSON object, find nothing, and report zero.
func TestPullConsumersReadTheEnvelope(t *testing.T) {
	for _, name := range []string{"status.md", "inbox.md"} {
		t.Run(name, func(t *testing.T) {
			content := promptContent(t, name)
			if !strings.Contains(content, ".issues") {
				t.Errorf("%s must read the envelope's .issues field", name)
			}
			if !strings.Contains(content, ".error") {
				t.Errorf("%s must check the envelope's .error field", name)
			}
			if !strings.Contains(content, "unreachable") {
				t.Errorf("%s must report an unreachable board in those words", name)
			}
			if strings.Contains(content, "emits [] when board sync is off") {
				t.Errorf("%s still describes pull --json as emitting a bare array", name)
			}
		})
	}
}

// TestStatusPromptDropsTheSilentSkip pins the line that actually caused the
// fault. The array shape was the mechanism; "skip that source's contribution
// silently" was the instruction that turned an unreachable board into a
// confident zero. Removing one without the other leaves the bug.
func TestStatusPromptDropsTheSilentSkip(t *testing.T) {
	content := promptContent(t, "status.md")
	if strings.Contains(content, "skip that sources contribution silently") {
		t.Error("status.md still tells the model to swallow a board error silently")
	}
	// The wider shape too — any "on an error, skip it quietly" phrasing about
	// an intake source reintroduces the same confident zero.
	if strings.Contains(content, "skip") && strings.Contains(content, "contribution silently") {
		t.Error("status.md must not instruct any silent skip of an intake source")
	}
	if !strings.Contains(content, "board unreachable") {
		t.Error("status.md must have wording for reporting the board as unreachable")
	}
}
