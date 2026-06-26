package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// inboxPromptContent loads assets/prompts/inbox.md normalised (lowercased,
// backticks/emphasis stripped). (r-01: the prompt edit is only live after
// `make install`; this reads the assets/ source directly.)
func inboxPromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "inbox.md"))
	if err != nil {
		t.Fatalf("read inbox.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
}

// TestInboxPromptDeferredSecondSource proves c-6: inbox pulls someday/unrouted
// deferred items as a second triage source via the CLI.
func TestInboxPromptDeferredSecondSource(t *testing.T) {
	content := inboxPromptContent(t)
	if !strings.Contains(content, "dross deferred list --someday --json") {
		t.Error("inbox.md must read someday deferred items via `dross deferred list --someday --json`")
	}
}

// TestInboxPromptDeferredFunnelCoverage proves c-6: deferred items route through
// the same new-phase / milestone-backlog / quick / dismiss funnel as board issues.
func TestInboxPromptDeferredFunnelCoverage(t *testing.T) {
	content := inboxPromptContent(t)
	if !strings.Contains(content, "deferred someday ideas route through the same funnel") {
		t.Error("inbox.md must route deferred items through the shared triage funnel")
	}
	for _, dest := range []string{"new phase", "milestone backlog", "quick", "dismiss"} {
		if !strings.Contains(content, dest) {
			t.Errorf("inbox.md deferred funnel missing destination %q", dest)
		}
	}
	if !strings.Contains(content, "dross deferred route") {
		t.Error("inbox.md deferred triage must stamp routing via `dross deferred route`")
	}
}
