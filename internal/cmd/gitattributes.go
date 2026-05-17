package cmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// drossGitattributesLine collapses .dross/ files in PR review diffs on
// GitHub and Forgejo. Without it, planning artefacts flood the reviewer's
// "Files changed" tab — that historical pain is why `dross ship` used to
// strip .dross/ from the PR branch entirely, which in turn caused
// origin/main vs local main divergence on every ship.
const drossGitattributesLine = ".dross/** linguist-generated=true"

// ensureDrossGitattributes writes <repoDir>/.gitattributes (or appends to
// it) so the .dross/ tree is marked linguist-generated. Idempotent: a
// second call is a no-op once the line is present. Safe to call on
// repos with a pre-existing .gitattributes (appends, doesn't overwrite).
func ensureDrossGitattributes(repoDir string) error {
	path := filepath.Join(repoDir, ".gitattributes")
	existing, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return os.WriteFile(path, []byte(drossGitattributesLine+"\n"), 0o644)
	}
	if err != nil {
		return err
	}
	if hasDrossLinguistLine(string(existing)) {
		return nil
	}
	suffix := drossGitattributesLine + "\n"
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		suffix = "\n" + suffix
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(suffix)
	return err
}

// hasDrossLinguistLine checks whether any existing .gitattributes entry
// already covers .dross/ with linguist-generated=true. Tolerates extra
// whitespace and comment lines.
func hasDrossLinguistLine(body string) bool {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, ".dross/") && strings.Contains(line, "linguist-generated=true") {
			return true
		}
	}
	return false
}
