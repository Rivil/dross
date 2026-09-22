package boardsync

import (
	"testing"

	"github.com/Rivil/dross/internal/forge"
)

// TestJournalledPriorStateFallsBackToTheOpenClosedState: a forge or GitLab
// board has no column model, so WorkflowState is empty on every card — and
// those backends ARE StateWriters, so undo really does run against them.
// Journalling WorkflowState alone would record "" and make every restore write
// an empty state and fail its read-back.
func TestJournalledPriorStateFallsBackToTheOpenClosedState(t *testing.T) {
	for _, tc := range []struct {
		name string
		iss  forge.Issue
		want string
	}{
		{"a board with a column model", forge.Issue{WorkflowState: "In Review", State: "open"}, "In Review"},
		{"a flat open/closed board", forge.Issue{State: "open"}, "open"},
	} {
		if got := priorStateOf(&tc.iss); got != tc.want {
			t.Errorf("%s: journalled prior state %q, want %q", tc.name, got, tc.want)
		}
	}
}
