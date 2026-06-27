package quality

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Rivil/dross/internal/findings"
)

// StatePath returns the durable findings state ledger for the quality audit:
// .dross/quality/state.toml. It sits at the top of the quality dir, beside the
// timestamped run dirs but not inside any of them, so it survives run-dir
// pruning. root is the .dross dir (as returned by cmd.FindRoot).
func StatePath(root string) string {
	return filepath.Join(QualityDir(root), "state.toml")
}

// Items adapts the per-run ledger into the shared reconcile Items. The quality
// fingerprint keys on Dimension (the maintainability axis) — deliberately NOT
// Risk, which is a contextual, run-to-run ranking; keying on it would split one
// finding into two when the panel re-scores it.
func (l Ledger) Items() []findings.Item {
	out := make([]findings.Item, 0, len(l.Findings))
	for _, f := range l.Findings {
		out = append(out, findings.Item{Class: string(f.Dimension), File: f.File, Title: f.Title})
	}
	return out
}

// LatestRun returns the most recent run dir under .dross/quality. Run ids are
// fixed-width, lexically-sortable timestamps, so the greatest name is the
// newest run. It errors when there are no runs yet.
func LatestRun(root string) (string, error) {
	dir := QualityDir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read quality runs: %w", err)
	}
	var latest string
	for _, e := range entries {
		if e.IsDir() && e.Name() > latest {
			latest = e.Name()
		}
	}
	if latest == "" {
		return "", fmt.Errorf("no quality runs under %s", dir)
	}
	return filepath.Join(dir, latest), nil
}

// ResolveItem maps a per-run finding id (e.g. "f-3") from the most recent run's
// ledger to its reconcile Item, so `dross quality findings <id> --state` can
// derive the durable fingerprint from the id a user read off the latest report.
func ResolveItem(root, id string) (findings.Item, error) {
	runDir, err := LatestRun(root)
	if err != nil {
		return findings.Item{}, err
	}
	ledger, err := Load(filepath.Join(runDir, "findings.toml"))
	if err != nil {
		return findings.Item{}, err
	}
	for _, f := range ledger.Findings {
		if f.ID == id {
			return findings.Item{Class: string(f.Dimension), File: f.File, Title: f.Title}, nil
		}
	}
	return findings.Item{}, fmt.Errorf("no finding %q in latest run %s", id, filepath.Base(runDir))
}
