package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The completion record does not ride the squash-merge and never did reach
// main: `.dross/state.json` is machine-local and gitignored, and since
// completion-state-truth the record is written by `dross phase complete` after
// it confirms the merge — not folded in by `dross ship` before the PR exists.
//
// That claim was spread across code comments, CLI narration, prompts and
// ARCHITECTURE.md, where it rotted quietly for two milestones because nothing
// executable read it. These guards make the prose fail the build.

// retiredSquashClaims is the phrase family the old claim used, in lowercase.
// It lives here, in the file the scan skips, so that every OTHER test asserting
// the absence of one of these strings can reference the slice instead of
// spelling the phrase out — a negative assertion elsewhere would otherwise trip
// the scan by naming what it forbids.
var retiredSquashClaims = []string{
	"folds the completion",
	"folded into the squash",
	"folded into state history by ship",
	"folds the cleared current_phase",
	"folded the cleared current_phase",
	"completion record folded",
	"squash-merge will land it",
	"carries the completion record to main",
	"records the merge in state.json with a chore commit",
	"branch-local state records `completed",
	"branch-local state records completed",
}

// squashClaimPaths is the fixed set of files scanned. A fixed list, not a walk:
// a walk would silently start covering (or stop covering) files as the tree
// changes, and the point is to know exactly which surfaces are pinned.
func squashClaimPaths(t *testing.T) []string {
	t.Helper()
	repo := repoRootFromTest(t)
	var paths []string
	for _, glob := range []string{
		filepath.Join(repo, "internal", "cmd", "*.go"),
		filepath.Join(repo, "internal", "watch", "*.go"),
		filepath.Join(repo, "assets", "prompts", "*.md"),
	} {
		matches, err := filepath.Glob(glob)
		if err != nil {
			t.Fatalf("glob %s: %v", glob, err)
		}
		if len(matches) == 0 {
			t.Fatalf("glob %s matched nothing — the scan path is wrong", glob)
		}
		paths = append(paths, matches...)
	}
	paths = append(paths,
		filepath.Join(repo, "internal", "changes", "changes.go"),
		filepath.Join(repo, "internal", "telemetry", "telemetry.go"),
		filepath.Join(repo, "ARCHITECTURE.md"),
		filepath.Join(repo, "README.md"),
	)
	return paths
}

// TestNoSurfaceClaimsCompletionRidesTheSquash (c-3) scans the phrase family the
// retired claim used. Whitespace is collapsed before scanning: the two worst
// offenders were wrapped comment blocks where no needle matched any single
// line, so a line-wise scan would let them be re-added verbatim.
func TestNoSurfaceClaimsCompletionRidesTheSquash(t *testing.T) {
	for _, path := range squashClaimPaths(t) {
		// This file names every needle by construction.
		if filepath.Base(path) == "squash_claim_guard_test.go" {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		// Collapse runs of whitespace (incl. newlines and comment-leader
		// indentation) so a needle matches across a wrapped comment block.
		content := strings.ToLower(strings.Join(strings.Fields(string(b)), " "))
		for _, n := range retiredSquashClaims {
			if strings.Contains(content, n) {
				rel, _ := filepath.Rel(repoRootFromTest(t), path)
				t.Errorf("%s still claims the completion record rides the squash: %q\n"+
					"`dross phase complete` writes it into the machine-local, gitignored state.json "+
					"after the merge is confirmed; `dross ship` only marks the phase shipped.", rel, n)
			}
		}
	}
}

// TestCompleteHelpNamesRecordOwner (c-3) pins the POSITIVE claim, not just the
// absence of the old one. A guard that only forbids phrases lets the help text
// go silent about ownership, which is how the old claim rotted unnoticed.
func TestCompleteHelpNamesRecordOwner(t *testing.T) {
	long := strings.Join(strings.Fields(strings.ToLower(phaseComplete().Long)), " ")

	for _, needle := range []string{
		"writes the completed-state transition",
		"machine-local",
	} {
		if !strings.Contains(long, needle) {
			t.Errorf("`dross phase complete --help` must say where the record lives and who writes it: missing %q", needle)
		}
	}
	if strings.Contains(long, "'dross ship' folds") {
		t.Error("`dross phase complete --help` still says ship folds the record in")
	}
}

// TestQuickPromptHandlesShippedWindow (c-3): ship now leaves current_phase set
// until a merge is confirmed, so a phase can read `shipped` while its branch is
// about to be deleted by `dross phase complete`. quick.md's in-phase rule would
// otherwise route a quick fix straight onto that branch.
func TestQuickPromptHandlesShippedWindow(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "assets", "prompts", "quick.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Backticks and emphasis stripped, but NOT underscores: the field names
	// under test are `current_phase_status` and friends.
	content := strings.ToLower(strings.Join(strings.Fields(string(b)), " "))
	content = strings.NewReplacer("`", "", "*", "").Replace(content)

	for _, needle := range []string{
		"current_phase_status",
		"shipped",
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("quick.md's branch routing must account for the shipped window: missing %q", needle)
		}
	}
	// The bare rule, with no shipped exception, is what must not come back.
	if strings.Contains(content, "in-phase (current_phase set): branch must be phase/<current_phase>") {
		t.Error("quick.md reverted to the unconditional in-phase branch rule — a shipped phase's branch is about to be deleted")
	}
}
