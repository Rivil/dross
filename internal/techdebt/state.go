package techdebt

import "path/filepath"

// Dir is the conventional parent for all tech-debt run artifacts: .dross/techdebt.
// root is the .dross dir (as returned by cmd.FindRoot), mirroring security's
// SecurityDir and quality's QualityDir.
func Dir(root string) string {
	return filepath.Join(root, "techdebt")
}

// StatePath returns the durable signal ledger for the tech-debt area:
// .dross/techdebt/state.toml. It sits at the top of the techdebt dir, beside the
// timestamped run dirs but not inside any of them, so the store-level last_run
// signal survives run-dir pruning. root is the .dross dir.
func StatePath(root string) string {
	return filepath.Join(Dir(root), "state.toml")
}
