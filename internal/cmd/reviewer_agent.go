package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rivil/dross/assets"
	"github.com/Rivil/dross/internal/review"
)

// reviewerFile is the solo task reviewer's definition file name, the same in
// the embedded assets/agents/ and in the agents directory it installs into.
const reviewerFile = review.ReviewerAgent + ".md"

// userAgentsDir is where Claude Code reads user-level agent definitions:
// $CLAUDE_CONFIG_DIR/agents when set, else ~/.claude/agents. Install writes
// there and every readiness check reads there, so the two cannot disagree.
func userAgentsDir(home string) string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "agents")
	}
	return filepath.Join(home, ".claude", "agents")
}

// embeddedAgents returns every agent definition dross ships, by file name.
func embeddedAgents() (map[string][]byte, error) {
	entries, err := fs.ReadDir(assets.FS, "agents")
	if err != nil {
		return nil, fmt.Errorf("read embedded agents: %w", err)
	}
	out := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := assets.FS.ReadFile("agents/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read embedded agent %s: %w", e.Name(), err)
		}
		out[e.Name()] = b
	}
	return out, nil
}

// syncAgents installs the shipped agent definitions into dir — a symlink to
// sourceDir/agents/<file> in link mode, the embedded bytes otherwise — and
// prunes dross-* definitions this version no longer ships. Definitions
// outside the dross-* namespace are never touched.
func syncAgents(dir string, link bool, sourceDir string, logf func(string, ...any)) error {
	agents, err := embeddedAgents()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	for name, b := range agents {
		dst := filepath.Join(dir, name)
		// Replace whatever is there (symlink or stale copy) so a mode switch
		// leaves no trace of the old form.
		if err := os.Remove(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("clear %s: %w", dst, err)
		}
		if link {
			if err := os.Symlink(filepath.Join(sourceDir, "agents", name), dst); err != nil {
				return fmt.Errorf("symlink %s: %w", dst, err)
			}
		} else if err := os.WriteFile(dst, b, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
		logf("agent   → %s\n", dst)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read agents dir: %w", err)
	}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, "dross-") || !strings.HasSuffix(n, ".md") {
			continue
		}
		if _, keep := agents[n]; keep {
			continue
		}
		if err := os.Remove(filepath.Join(dir, n)); err != nil {
			return fmt.Errorf("prune stale agent %s: %w", n, err)
		}
		logf("prune   → %s\n", filepath.Join(dir, n))
	}
	return nil
}

// reviewerState is the solo reviewer's readiness.
type reviewerState string

const (
	reviewerOK       reviewerState = "ok"
	reviewerMissing  reviewerState = "missing"
	reviewerStale    reviewerState = "stale"
	reviewerShadowed reviewerState = "shadowed"
)

// reviewerReadiness is what doctor and a solo begin both judge by.
type reviewerReadiness struct {
	State reviewerState
	// Path is the installed definition doctor and begin name.
	Path string
	// Shadow is the repo-level definition that wins over Path, when shadowed.
	Shadow string
	// Linked reports Path is a symlink (a source-checkout install).
	Linked bool
}

// reviewerStatusIn judges the reviewer definition installed in dir against
// the embedded one, and whether repoRoot ("" for none) carries a project-level
// definition of the same name — which Claude Code prefers over the user's, so
// the reviewer that runs would not be the one dross shipped.
//
// Missing covers a dangling symlink; stale is any byte difference read through
// a link, so a source file edited past the installed binary reads stale too.
func reviewerStatusIn(dir, repoRoot string) reviewerReadiness {
	r := reviewerReadiness{Path: filepath.Join(dir, reviewerFile)}
	if fi, err := os.Lstat(r.Path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		r.Linked = true
	}
	got, err := os.ReadFile(r.Path)
	if err != nil {
		r.State = reviewerMissing
		return r
	}
	want, err := assets.FS.ReadFile("agents/" + reviewerFile)
	if err != nil || !bytes.Equal(got, want) {
		r.State = reviewerStale
		return r
	}
	if repoRoot != "" {
		shadow := filepath.Join(repoRoot, ".claude", "agents", reviewerFile)
		if _, err := os.Lstat(shadow); err == nil {
			r.State, r.Shadow = reviewerShadowed, shadow
			return r
		}
	}
	r.State = reviewerOK
	return r
}

// Remedy names the fix for a reviewer that is not ok.
func (r reviewerReadiness) Remedy() string {
	switch r.State {
	case reviewerMissing:
		return "run `dross install` to install it"
	case reviewerStale:
		if r.Linked {
			return "run `make install` in the dross checkout — the linked source changed past the installed binary"
		}
		return "run `dross install` to refresh it"
	case reviewerShadowed:
		return fmt.Sprintf("remove %s — a project-level definition of the same name replaces the one dross ships", r.Shadow)
	}
	return ""
}

// Problem describes a reviewer that is not ok, or "".
func (r reviewerReadiness) Problem() string {
	switch r.State {
	case reviewerMissing:
		return fmt.Sprintf("the solo reviewer definition %s is missing", r.Path)
	case reviewerStale:
		return fmt.Sprintf("the solo reviewer definition %s differs from the one this dross ships", r.Path)
	case reviewerShadowed:
		return fmt.Sprintf("the solo reviewer definition is shadowed by %s", r.Shadow)
	}
	return ""
}
