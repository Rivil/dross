package cmd

import (
	"strings"
	"testing"
)

// nextPhaseSection returns verify.md's §5 next-phase resolution block, which is
// the only place in that prompt where the ordering source matters.
func nextPhaseSection(t *testing.T) string {
	t.Helper()
	body := promptBody(t, "verify.md")
	start := strings.Index(body, "If verdict is `pass`:")
	if start < 0 {
		t.Fatal("verify.md no longer has the pass branch — the next-phase block moved")
	}
	rest := body[start:]
	if end := strings.Index(rest, "If `partial` or `fail`"); end > 0 {
		rest = rest[:end]
	}
	return rest
}

// TestVerifyPromptReadsTheMilestoneArray is c-5.
//
// `dross phase list` is a directory listing: it prints phases that have been
// SCAFFOLDED, and a phase is scaffolded only once someone starts it. Using it
// as the ordering source makes every unstarted successor invisible — which is
// how verifying phase 9 of 14 announced the milestone feature-complete on
// 2026-08-13 and sent the user to the wrong next command.
func TestVerifyPromptReadsTheMilestoneArray(t *testing.T) {
	section := nextPhaseSection(t)

	if !strings.Contains(section, "phases` array") && !strings.Contains(section, "phases array") {
		t.Errorf("the next-phase block does not name the milestone's phases array as the ordering source:\n%s", section)
	}
	// The prompt must say WHY, or the next reader re-derives the bug from
	// first principles — `phase list` is the obvious-looking source.
	// Scoped to the BARE listing since `--milestone` walks the whole roadmap:
	// warning off that reading too would forbid a correct ordering source.
	if !strings.Contains(section, "Do not use a bare `dross phase list`") {
		t.Errorf("the block does not warn off the bare directory listing:\n%s", section)
	}
	if !strings.Contains(section, "scaffolded") {
		t.Errorf("the block does not explain that phase list only shows scaffolded phases:\n%s", section)
	}
}

// TestVerifyPromptGatesTheLastPhaseClaim: "this is the last phase in the
// milestone" is the sentence that was wrong, so it needs a stated condition
// rather than being left to fall out of whichever list was consulted.
func TestVerifyPromptGatesTheLastPhaseClaim(t *testing.T) {
	section := nextPhaseSection(t)
	if !strings.Contains(section, "last entry of that array") {
		t.Errorf("the last-phase claim is not tied to the milestone array:\n%s", section)
	}
}

// TestVerifyPromptKeepsTheNoMilestoneFallback: a repo with no active milestone
// still needs an answer, and there `dross phase list` is the only ordering
// there is.
func TestVerifyPromptKeepsTheNoMilestoneFallback(t *testing.T) {
	section := nextPhaseSection(t)
	if !strings.Contains(section, "no active milestone") {
		t.Errorf("the no-milestone case lost its fallback:\n%s", section)
	}
	if !strings.Contains(section, "fall back to the bare `dross phase list`") {
		t.Errorf("the fallback does not name what to fall back to:\n%s", section)
	}
}
