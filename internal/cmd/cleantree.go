package cmd

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/Rivil/dross/internal/secretscan"
)

// autoCommitDrossDirt is the shared dirty-tree gate behind ship, phase
// complete, and phase create/start (the autocommit_coverage decision):
// bookkeeping-only dirt under .dross/ (a pause state-touch, a stray changes
// record) must never block a gate, while real code dirt still refuses loudly.
//
// It partitions `git status --porcelain`:
//   - clean tree → no-op
//   - every path under .dross/ → stage .dross/ and write one chore(dross)
//     commit, then proceed (committed=true)
//   - any path outside .dross/ → dirtyTreeError, staging nothing
func autoCommitDrossDirt(repoDir, action string) (committed bool, err error) {
	status, err := gitStatusRaw(repoDir)
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	if status == "" {
		return false, nil
	}
	for _, line := range strings.Split(status, "\n") {
		for _, p := range porcelainPaths(line) {
			if !underDross(p) {
				return false, dirtyTreeError(action, status)
			}
		}
	}
	// Secret gate, AFTER the code-dirt refusal and BEFORE `git add`: this is
	// the primitive that turns an untracked note into a commit, so it is the
	// one that refuses — which covers ship, phase complete, record completion,
	// phase start and milestone prune in one place. Same scanner as validate
	// and ship's pre-flight (the ship_gate_scope decision); a scan error is a
	// refusal too, since an artifact it could not read is one it cannot vouch
	// for.
	hits, err := scanDrossArtifacts(repoDir)
	if err != nil {
		return false, fmt.Errorf("refusing to auto-commit .dross: %w", err)
	}
	if len(hits) > 0 {
		return false, fmt.Errorf("refusing to auto-commit .dross: %w", &secretscan.ErrHit{Hits: hits})
	}
	if err := gitRun(repoDir, gitPathArgs("add", nil, ".dross")...); err != nil {
		return false, fmt.Errorf("git add .dross: %w", err)
	}
	// Empty-commit guard: a status entry can stage to nothing (e.g. a change
	// already reverted); nil means no staged diff, so there is nothing to commit.
	if gitNoOut(repoDir, "diff", "--cached", "--quiet") == nil {
		return false, nil
	}
	msg := fmt.Sprintf("chore(dross): auto-commit bookkeeping before %s", action)
	if err := gitRun(repoDir, "commit", "-m", msg); err != nil {
		return false, fmt.Errorf("git commit: %w", err)
	}
	return true, nil
}

// gitStatusRaw returns `git status --porcelain` without trimming leading
// whitespace — gitTrim would eat the first line's leading status column
// (" M path") and break positional parsing.
func gitStatusRaw(repoDir string) (string, error) {
	//dross:exec-exempt git status --porcelain reads the working tree and runs no repo-authored line; no hook fires for it
	out, err := exec.Command("git", "-C", repoDir, "status", "--porcelain").Output()
	if err != nil {
		return "", err
	}
	//dross:taint-cleared status --porcelain prints two status letters and a repo path per line, never file content
	return strings.TrimRight(string(out), "\n"), nil
}

// porcelainPaths extracts the path(s) named by one `git status --porcelain`
// line. Rename/copy lines ("R  old -> new") carry two paths and both sides
// count; quoted paths (tabs, non-ASCII) are unquoted before matching.
func porcelainPaths(line string) []string {
	if len(line) < 4 {
		return nil
	}
	xy, rest := line[:2], line[3:]
	if strings.ContainsAny(xy, "RC") {
		if i := strings.Index(rest, " -> "); i >= 0 {
			return []string{unquotePath(rest[:i]), unquotePath(rest[i+4:])}
		}
	}
	return []string{unquotePath(rest)}
}

// unquotePath undoes git's C-style quoting of unusual paths; a plain path
// passes through unchanged.
func unquotePath(p string) string {
	if strings.HasPrefix(p, `"`) {
		if u, err := strconv.Unquote(p); err == nil {
			return u
		}
	}
	return p
}

// underDross reports whether p is .dross itself or inside it. Matching on the
// "/"-terminated prefix (not a bare string prefix) keeps siblings like
// .drosszz/x or notes-.dross.txt outside.
func underDross(p string) bool {
	p = strings.TrimSuffix(p, "/")
	return p == ".dross" || strings.HasPrefix(p, ".dross/")
}
