package cmd

import (
	"strings"
	"testing"
	"testing/fstest"
)

// seedPromptFS swaps the tree `dross prompt` reads for a MapFS for one test.
func seedPromptFS(t *testing.T, m fstest.MapFS) {
	t.Helper()
	old := promptFS
	promptFS = m
	t.Cleanup(func() { promptFS = old })
}

// A partial with no trailing newline and a blank line inside: printed as is.
const seededPartial = "## Solo\n\nsolo-only text, no trailing newline"

func partialTree() fstest.MapFS {
	return fstest.MapFS{
		"prompts/execute.md":              {Data: []byte("# execute\n**When** `mode=solo` — solo: `dross prompt partial execute solo`\n")},
		"prompts/verify.md":               {Data: []byte("# verify\n")},
		"prompts/_interaction.md":         {Data: []byte("# Interaction playbook\n")},
		"prompts/_execute.solo.md":        {Data: []byte(seededPartial)},
		"prompts/_execute.board-moves.md": {Data: []byte("moves\n")},
	}
}

func TestPromptPartialVerbatim(t *testing.T) {
	seedPromptFS(t, partialTree())
	var out string
	if err := runCmdCapturing(t, &out, Prompt(), "partial", "execute", "solo"); err != nil {
		t.Fatal(err)
	}
	if out != seededPartial {
		t.Errorf("printed %q, want the partial byte for byte %q", out, seededPartial)
	}
}

// TestPromptPartialNeverDedupes: a re-fetch after a compaction prints the
// whole partial again, unlike `interaction show`.
func TestPromptPartialNeverDedupes(t *testing.T) {
	seedPromptFS(t, partialTree())
	t.Setenv("CLAUDE_CODE_SESSION_ID", "same-session")
	for i := 0; i < 2; i++ {
		var out string
		if err := runCmdCapturing(t, &out, Prompt(), "partial", "execute", "solo"); err != nil {
			t.Fatal(err)
		}
		if out != seededPartial {
			t.Fatalf("fetch %d printed %q, want the full partial", i+1, out)
		}
	}
}

func TestPromptPartialUnknownNameListsValid(t *testing.T) {
	seedPromptFS(t, partialTree())
	var out string
	err := runCmdCapturing(t, &out, Prompt(), "partial", "execute", "ghost")
	if err == nil {
		t.Fatalf("an unknown partial printed %q and exited 0", out)
	}
	for _, want := range []string{`"ghost"`, "board-moves", "solo"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	if out != "" {
		t.Errorf("an unknown partial printed %q", out)
	}
}

func TestPromptPartialRefusesBadNames(t *testing.T) {
	seedPromptFS(t, partialTree())
	for _, args := range [][]string{{"execute", "../verify"}, {"interaction", "x"}, {"execute.md", "solo"}} {
		var out string
		if err := runCmdCapturing(t, &out, Prompt(), append([]string{"partial"}, args...)...); err == nil {
			t.Errorf("partial %v printed %q and exited 0", args, out)
		}
	}
}
