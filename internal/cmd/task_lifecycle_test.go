package cmd

import (
	"testing"

	"github.com/Rivil/dross/internal/boardsync"
)

// TestTerminalTaskStatusIsReadableButNeverDerived moved with the mapping to
// internal/boardsync (lifecycle_test.go); what stays here drives the command.

// TestTerminalRunClearsTheReviewLabel is c-2's headline, end to end: after the
// finalize emission, no mirrored task card still carries
// dross/status:task-in-review, and each one reads back done on its provider.
//
// This is the assertion `task-complete` had to exist for — it is not a valid
// --status until this task adds it, so before now the run under test could not
// even be spelled.
//
// The done verdict is asserted by the run exiting zero rather than by a second
// read: boardsync.CloseIssue re-reads every card and fails the command unless the
// tracker agrees, so a fake that closed nothing could not reach this point.
func TestTerminalRunClearsTheReviewLabel(t *testing.T) {
	f := newTaskCloseFake(t)
	dir := taskCloseRepo(t, "forgejo", f, "t-1", "t-2")

	// The execute loop's state: every card mirrored and sitting in review.
	if err := runCmd(t, Issue(), "task", "sync", "01-auth", "--status", boardsync.StatusTaskInReview); err != nil {
		t.Fatalf("seed task-sync: %v", err)
	}
	for _, id := range []string{"t-1", "t-2"} {
		key, _ := loadBoardFile(t, dir).TaskIssue("01-auth", id)
		if !slicesHas(f.labelsOf(key), boardsync.StatusLabel(boardsync.StatusTaskInReview)) {
			t.Fatalf("%s does not start in review: %v", id, f.labelsOf(key))
		}
	}

	// Ship finalize.
	_ = captureStdout(t, func() {
		_ = captureStderr(t, func() {
			if err := runCmd(t, Issue(), "task", "sync", "01-auth", "--status", boardsync.StatusTaskComplete, "--close"); err != nil {
				t.Fatalf("terminal task-sync: %v", err)
			}
		})
	})

	bd := loadBoardFile(t, dir)
	for _, id := range []string{"t-1", "t-2"} {
		key, ok := bd.TaskIssue("01-auth", id)
		if !ok {
			t.Fatalf("%s lost its link", id)
		}
		labels := f.labelsOf(key)
		if slicesHas(labels, boardsync.StatusLabel(boardsync.StatusTaskInReview)) {
			t.Errorf("%s still carries %s after finalize: %v", id, boardsync.StatusLabel(boardsync.StatusTaskInReview), labels)
		}
		if !slicesHas(labels, boardsync.StatusLabel(boardsync.StatusTaskComplete)) {
			t.Errorf("%s does not carry %s: %v", id, boardsync.StatusLabel(boardsync.StatusTaskComplete), labels)
		}
		if f.closeCount(key) != 1 {
			t.Errorf("%s closes = %d, want 1", id, f.closeCount(key))
		}
	}
}
