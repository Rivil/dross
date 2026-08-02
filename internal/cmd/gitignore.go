package cmd

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// drossStateIgnorePath is the path the ignore rule has to cover. It is also
// what an existing broader pattern is tested against, so the two can't drift.
const drossStateIgnorePath = ".dross/state.json"

// drossGitignoreBlock is the comment + pattern written into .gitignore.
//
// state.json holds machine-local position: current phase, version, activity
// history. Tracking it means a checkout can replace the live file with whatever
// copy the branch being checked out happens to carry — which is exactly how a
// long-lived branch wound up clobbering a 40-entry history with a 2-entry one.
// Untracking removes the copy from every future commit, so there is nothing
// left to replay (locked decision: state_tracking).
//
// It does NOT make git refuse: git declines to overwrite an untracked
// working-tree file only when that file is not ignored, and overwrites an
// ignored one silently. Refs cut before this line landed still carry a copy, and
// the guard in switchbranch.go is what stops dross replaying it.
const drossGitignoreBlock = "# dross machine-local position + version state — deliberately untracked: no\n" +
	"# ordinary commit carries a copy, so nothing can replay one over the live file.\n" +
	"# (On refs cut before this line landed, dross itself refuses the switch — git\n" +
	"# overwrites an *ignored* file without complaint.) Locked state_tracking.\n" +
	drossStateIgnorePath

// ensureDrossGitignore writes <repoDir>/.gitignore (or appends to it) so
// .dross/state.json is ignored. Idempotent: a second call is a no-op once the
// path is covered, and a .gitignore whose existing patterns already cover it —
// a broader `.dross/*.json`, say — is left byte-for-byte alone rather than
// gaining a redundant line.
//
// Mirrors ensureDrossGitattributes: same append-don't-overwrite contract, same
// call sites in init and onboard.
func ensureDrossGitignore(repoDir string) error {
	file := filepath.Join(repoDir, ".gitignore")
	existing, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return os.WriteFile(file, []byte(drossGitignoreBlock+"\n"), 0o644)
	}
	if err != nil {
		return err
	}
	if ignoresDrossState(string(existing)) {
		return nil
	}
	suffix := "\n" + drossGitignoreBlock + "\n"
	if len(existing) == 0 || strings.HasSuffix(string(existing), "\n\n") {
		suffix = drossGitignoreBlock + "\n"
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(suffix)
	return err
}

// ignoresDrossState reports whether any pattern in body already covers
// .dross/state.json — the exact path, a glob like `.dross/*.json`, or a
// directory pattern like `.dross/`.
//
// Negations are skipped rather than interpreted: a `!` line does not cover
// anything, and a repo that deliberately re-includes state.json has made a
// choice this helper should not silently undo by appending the rule again.
func ignoresDrossState(body string) bool {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		pattern := strings.TrimPrefix(line, "/")
		if strings.HasSuffix(pattern, "/") && strings.HasPrefix(drossStateIgnorePath, pattern) {
			return true
		}
		if pattern == drossStateIgnorePath {
			return true
		}
		if ok, err := path.Match(pattern, drossStateIgnorePath); err == nil && ok {
			return true
		}
	}
	return false
}
