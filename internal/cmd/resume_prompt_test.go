package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resumePromptContent loads assets/prompts/resume.md and normalises it —
// lowercased, with markdown emphasis and backticks stripped — so assertions
// test the presence of a rule, not its exact formatting.
func resumePromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "resume.md"))
	if err != nil {
		t.Fatalf("read resume.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
}

// TestResumePromptStaleStateSection (c-4) content-gates the stale-completion
// drift case resume.md must carry: the stale-completed phrase, a reconcile
// pointer (origin), and the never-auto-mutate caveat. Each is an
// individually-failing sub-assertion, so dropping the section fails exactly
// those needles.
func TestResumePromptStaleStateSection(t *testing.T) {
	content := resumePromptContent(t)
	cases := []struct {
		name    string
		needles []string
	}{
		{"stale-completed phrase", []string{"stale", "completed"}},
		{"reconcile pointer", []string{"reconcile", "origin"}},
		{"never-auto-mutate caveat", []string{"never auto-mutate"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range tc.needles {
				if !strings.Contains(content, n) {
					t.Errorf("resume.md is missing the required phrase %q for %s", n, tc.name)
				}
			}
		})
	}
}

// rewritesWholesale reports whether a prompt line tells the agent to replace a
// file wholesale — a rewrite, an overwrite, or a Write — rather than forbid it
// or reserve it for creating the file. Headings are never instructions.
func rewritesWholesale(line string) bool {
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return false
	}
	l := strings.ToLower(line)
	if !strings.Contains(l, "rewrite") && !strings.Contains(l, "overwrite") && !strings.Contains(line, "Write") {
		return false
	}
	return !strings.Contains(l, "never") && !strings.Contains(l, "create")
}

// TestResumePromptPrunesWithEdit: §2 prunes .dross/handoff.md with Edit. A
// pruned handoff is smaller than the one read, so a whole-file rewrite trips
// the curated-shrink gate (c-8) and the routine resume would stop working the
// day the gates arm.
func TestResumePromptPrunesWithEdit(t *testing.T) {
	for line, want := range map[string]bool{
		"Then rewrite `.dross/handoff.md` with only what's left:": true,
		"Write the pruned list back to the file.":                 true,
		"Never replace the whole file with `Write`.":              false,
		"- **No handoff yet** → create it with `Write`.":          false,
		"## 3. Write + ignore":                                    false,
		"- A **done** item: `Edit` its line out.":                 false,
	} {
		if got := rewritesWholesale(line); got != want {
			t.Fatalf("the detector is miscalibrated on %q: %v, want %v", line, got, want)
		}
	}
	sec := sectionOf(promptBody(t, "resume.md"), "## 2. Prune", "## ")
	if sec == "" {
		t.Fatal("resume.md has no \"## 2. Prune\" section")
	}
	if !strings.Contains(sec, "in place with `Edit`") {
		t.Error("resume.md §2 no longer prunes .dross/handoff.md in place with `Edit`")
	}
	for _, line := range strings.Split(sec, "\n") {
		if rewritesWholesale(line) {
			t.Errorf("resume.md §2 tells the agent to rewrite the handoff wholesale: %q", strings.TrimSpace(line))
		}
	}
}

// TestResumePromptReadsTheStaleMarker: `dross status` prints a `stale:` line
// again, for a passing verdict that predates changed files. resume.md must not
// still say there is none, and must route the marker to /dross-verify.
func TestResumePromptReadsTheStaleMarker(t *testing.T) {
	content := resumePromptContent(t)
	if strings.Contains(content, "there is no stale: line") {
		t.Error("resume.md still says there is no stale: line to look for")
	}
	var bullet string
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "stale verdict") {
			bullet = line
			break
		}
	}
	if bullet == "" {
		t.Fatal("resume.md has no Stale verdict reconciliation bullet")
	}
	for _, needle := range []string{"stale:", "/dross-verify"} {
		if !strings.Contains(bullet, needle) {
			t.Errorf("resume.md's stale-verdict bullet lacks %q:\n%s", needle, bullet)
		}
	}
}
