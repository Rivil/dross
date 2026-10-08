package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The prompts used to call board sync "a no-op unless `[remote].board_sync` is
// on — safe to always run". The key is `[board].enabled`, and "safe to always
// run" taught agents to read any non-zero exit — `$YOUTRACK_TOKEN is not set`
// — as that no-op, so a phase never got its card (c-10). These tests hold the
// prompts to the real switch and to surfacing a failure.
//
// The files are read RAW: promptContent normalises away underscores, so a
// scan through it could not see `board_sync` at all.

// boardCallRe matches a prompt step that calls the board.
var boardCallRe = regexp.MustCompile(`dross issue (phase sync|task sync|milestone sync|backlog sync|quick)`)

// boardFailureSentence is the phrase every board-calling prompt must carry: a
// non-zero exit is a failure, not the disabled no-op.
const boardFailureSentence = "a non-zero exit is a board failure"

// rawPrompts returns every assets/prompts/*.md as name → raw bytes.
func rawPrompts(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join(repoRootFromTest(t), "assets", "prompts")
	paths, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(p)] = string(b)
	}
	return out
}

// TestNoPromptNamesBoardSyncKey: `[remote].board_sync` names no key dross
// reads; the board's switch is `[board].enabled`.
func TestNoPromptNamesBoardSyncKey(t *testing.T) {
	for name, src := range rawPrompts(t) {
		for i, line := range strings.Split(src, "\n") {
			if strings.Contains(line, "board_sync") {
				t.Errorf("%s:%d names board_sync — the switch is `[board].enabled`: %q", name, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// TestBoardCallingPromptsSurfaceFailure: every prompt that calls the board
// names the real switch and says a non-zero exit is a failure to surface.
func TestBoardCallingPromptsSurfaceFailure(t *testing.T) {
	var callers []string
	for name, src := range rawPrompts(t) {
		if !boardCallRe.MatchString(src) {
			continue
		}
		callers = append(callers, name)
		flat := strings.Join(strings.Fields(src), " ")
		if !strings.Contains(flat, "`[board].enabled`") {
			t.Errorf("%s calls the board but never names `[board].enabled` as the switch", name)
		}
		if !strings.Contains(flat, boardFailureSentence) {
			t.Errorf("%s calls the board but never says %q — a failed call reads as the disabled no-op", name, boardFailureSentence)
		}
	}
	sort.Strings(callers)
	// Anti-vacuity: six prompts call the board today. Fewer means the match is
	// broken, not that the prompts got quieter.
	if len(callers) < 6 {
		t.Fatalf("found %d board-calling prompts (%v), want at least 6 — the scan is not seeing them", len(callers), callers)
	}
}
