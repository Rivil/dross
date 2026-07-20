package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// watchPromptContent loads assets/prompts/watch.md lowercased with backticks
// stripped (underscores/slashes preserved, so `suggested_command` and
// `/dross-*` survive). (r-01: reads the assets/ source directly, since a prompt
// edit is only live after `make install`.)
func watchPromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "watch.md"))
	if err != nil {
		t.Fatalf("read watch.md: %v", err)
	}
	return strings.ToLower(strings.ReplaceAll(string(b), "`", ""))
}

// TestWatchPromptInvokesCommand: the prompt drives the read-only command.
func TestWatchPromptInvokesCommand(t *testing.T) {
	if !strings.Contains(watchPromptContent(t), "dross watch --json") {
		t.Error("watch.md must invoke `dross watch --json`")
	}
}

// TestWatchPromptSuggestionPrecedence (c-3): the prompt states the ranked order
// verify→ship→/dross-inbox→/dross-status and prints the digest's single
// suggested_command verbatim.
func TestWatchPromptSuggestionPrecedence(t *testing.T) {
	content := watchPromptContent(t)
	if !strings.Contains(content, "suggested_command") {
		t.Error("watch.md must reference the suggested_command field")
	}
	if !strings.Contains(content, "verbatim") {
		t.Error("watch.md must instruct printing suggested_command verbatim")
	}
	// Order the ranked list within §3 (after the 'locked precedence' intro), so
	// the §2 board-off mention of /dross-inbox doesn't skew the check.
	i := strings.Index(content, "locked precedence")
	if i < 0 {
		t.Fatal("watch.md must document the locked precedence ranking")
	}
	sub := content[i:]
	order := []string{"/dross-verify", "/dross-ship", "/dross-inbox", "/dross-status"}
	last := -1
	for _, cmd := range order {
		at := strings.Index(sub, cmd)
		if at < 0 {
			t.Fatalf("watch.md precedence list missing %q", cmd)
		}
		if at < last {
			t.Errorf("watch.md precedence out of order at %q (want verify→ship→inbox→status)", cmd)
		}
		last = at
	}
}

// TestWatchPromptBoardOffPath (c-5): the prompt mirrors inbox — announces
// skipping the board source and still renders a drift-only digest when off.
func TestWatchPromptBoardOffPath(t *testing.T) {
	content := watchPromptContent(t)
	for _, want := range []string{"board sync off", "drift only", "skip"} {
		if !strings.Contains(content, want) {
			t.Errorf("watch.md board-off path must mention %q", want)
		}
	}
}

// TestWatchShimNonInteractive: the /dross-watch shim is a broadcast — no
// AskUserQuestion, and it declares only Read + Bash.
func TestWatchShimNonInteractive(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "commands", "dross-watch.md"))
	if err != nil {
		t.Fatalf("read dross-watch.md: %v", err)
	}
	shim := string(b)
	if strings.Contains(shim, "AskUserQuestion") {
		t.Error("dross-watch.md must be non-interactive (no AskUserQuestion)")
	}
	if !strings.Contains(shim, "Read") || !strings.Contains(shim, "Bash") {
		t.Error("dross-watch.md must declare allowed-tools Read + Bash")
	}
}
