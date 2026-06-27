package techdebt

import (
	"path/filepath"
	"testing"
)

// TestStatePathIsSiblingOfRunDirs fails if the state ledger drifts off its
// prune-proof location: it must be .dross/techdebt/state.toml, sitting beside
// (not inside) the timestamped run dirs.
func TestStatePathIsSiblingOfRunDirs(t *testing.T) {
	root := "/repo/.dross"
	want := filepath.Join(root, "techdebt", "state.toml")
	if got := StatePath(root); got != want {
		t.Fatalf("StatePath = %q, want %q", got, want)
	}
	// The state file's parent must be the techdebt dir itself, so it is a sibling
	// of any run dir and survives run-dir pruning.
	if got := filepath.Dir(StatePath(root)); got != Dir(root) {
		t.Fatalf("state.toml parent = %q, want the techdebt dir %q (not a run dir)", got, Dir(root))
	}
}
