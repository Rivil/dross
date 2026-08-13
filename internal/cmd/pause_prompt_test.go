package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pausePromptContent loads assets/prompts/pause.md and normalises it —
// lowercased, with markdown emphasis and backticks stripped — so assertions
// test the presence of a rule, not its exact formatting.
func pausePromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "pause.md"))
	if err != nil {
		t.Fatalf("read pause.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
}

// TestPausePromptRefusesWithoutRoot (c-4) content-gates the §0 refusal:
// /dross-pause in a repo with no initialised root must write nothing and say
// so. Each needle is its own subtest, so dropping one part of the gate fails
// exactly that row rather than one opaque assertion.
func TestPausePromptRefusesWithoutRoot(t *testing.T) {
	content := pausePromptContent(t)
	cases := []struct {
		name    string
		needles []string
	}{
		{"probe command", []string{"dross state show"}},
		{"refusal names the condition", []string{"not a dross repo"}},
		{"refusal writes nothing", []string{"write nothing"}},
		{"repair pointer", []string{"dross onboard"}},
		// Locked pause_refusal: absent and incomplete are one concept. Both
		// words must appear in the gate or the prompt only covers half of it.
		{"absent root covered", []string{"absent"}},
		{"incomplete root covered", []string{"incomplete"}},
		// The prompt names the repair; it never performs it.
		{"never runs the repair itself", []string{"never run", "dross onboard or dross init"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range tc.needles {
				if !strings.Contains(content, n) {
					t.Errorf("pause.md is missing the required phrase %q for %s", n, tc.name)
				}
			}
		})
	}
}

// TestPausePromptHardRuleNoWrites pins the second half of c-4 independently of
// the §0 gate: the Hard rules section must forbid creating .dross and writing
// handoff.md. Removing only the hard rule, leaving the pre-flight in place,
// fails here — so both halves are held separately.
func TestPausePromptHardRuleNoWrites(t *testing.T) {
	content := pausePromptContent(t)
	idx := strings.Index(content, "## hard rules")
	if idx < 0 {
		t.Fatal("pause.md has no Hard rules section")
	}
	rules := content[idx:]
	for _, n := range []string{"never create .dross", "handoff.md"} {
		if !strings.Contains(rules, n) {
			t.Errorf("pause.md Hard rules section is missing %q", n)
		}
	}
}

// TestPausePromptGatePrecedesDrafting pins placement, not just presence: the
// gate has to sit ahead of the drafting step, or the prompt would compose a
// handoff for a repo it is about to refuse. Anchoring to the "## 1. Draft the
// handoff" heading (rather than to the §3 write step) keeps the row from being
// near-vacuous — almost any placement would precede the write.
func TestPausePromptGatePrecedesDrafting(t *testing.T) {
	content := pausePromptContent(t)
	gate := strings.Index(content, "dross state show")
	if gate < 0 {
		t.Fatal("pause.md has no `dross state show` probe")
	}
	draft := strings.Index(content, "## 1. draft the handoff")
	if draft < 0 {
		t.Fatal("pause.md has no `## 1. Draft the handoff` heading")
	}
	if gate >= draft {
		t.Errorf("the root gate is at byte %d, after the drafting heading at %d — it must precede it", gate, draft)
	}
}

// TestPausePromptFilesFindingsInsteadOfParking: the writer of handoff.md is
// what manufactures homeless findings — a bug parked as an "Open loops" bullet
// lives in a gitignored file that the next pause rewrites. The prompt must route
// a finding to `dross deferred add` instead.
func TestPausePromptFilesFindingsInsteadOfParking(t *testing.T) {
	content := pausePromptContent(t)
	for _, want := range []string{
		"dross deferred add",            // the verb itself
		"a finding is not an open loop", // the rule, stated
		"--target",                      // and the routed form
	} {
		if !strings.Contains(content, want) {
			t.Errorf("pause.md does not teach the finding path: missing %q", want)
		}
	}
}
