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

// drossLocalIgnorePath is the second: the machine-local store.
const drossLocalIgnorePath = ".dross/" + LocalFile

// ignoreEntry is one path the seeded block covers, with the reason it is
// untracked kept next to it. The comment is not decoration — someone will find
// these lines in a diff a year from now and decide whether to delete them.
type ignoreEntry struct {
	path    string
	comment string
}

// drossIgnoreEntries is the ordered set of paths ensureDrossGitignore seeds.
// Each is checked and appended independently, so a repo whose .gitignore
// already covers one through a broader pattern gains only the other.
var drossIgnoreEntries = []ignoreEntry{
	{
		path: drossStateIgnorePath,
		// state.json holds machine-local position: current phase, version,
		// activity history. Tracking it means a checkout can replace the live
		// file with whatever copy the branch being checked out happens to
		// carry — which is exactly how a long-lived branch wound up clobbering
		// a 40-entry history with a 2-entry one. Untracking removes the copy
		// from every future commit, so there is nothing left to replay
		// (locked decision: state_tracking).
		//
		// It does NOT make git refuse: git declines to overwrite an untracked
		// working-tree file only when that file is not ignored, and overwrites
		// an ignored one silently. Refs cut before this line landed still carry
		// a copy, and the guard in switchbranch.go is what stops dross
		// replaying it.
		comment: "# dross machine-local position + version state — deliberately untracked: no\n" +
			"# ordinary commit carries a copy, so nothing can replay one over the live file.\n" +
			"# (On refs cut before this line landed, dross itself refuses the switch — git\n" +
			"# overwrites an *ignored* file without complaint.) Locked state_tracking.",
	},
	{
		path: drossLocalIgnorePath,
		// local.toml is the escape hatch for the API host allowlist: a host the
		// derivation cannot reach is authorized here, by hand, on the machine
		// that needs it. That is only safe while the file is machine-local — a
		// committed local.toml would let a hostile repo authorize its own
		// exfiltration host, which is precisely the self-authorizing loop the
		// derived allowlist exists to avoid.
		//
		// The ignore line is the first half of that guarantee and it only helps
		// repos that run init/onboard from here on. readAllowHosts is the half
		// that holds for repos already onboarded: it refuses a local.toml git
		// reports as tracked rather than reading it.
		comment: "# dross machine-local config — host allowlist additions and the standalone-quick\n" +
			"# base branch. MUST stay untracked: a committed copy would let a cloned repo\n" +
			"# authorize its own API host, which is the loop the derived allowlist avoids.\n" +
			"# dross refuses to read this file if git reports it tracked.",
	},
}

// ensureDrossGitignore writes <repoDir>/.gitignore (or appends to it) so every
// path in drossIgnoreEntries is ignored. Idempotent: a second call is a no-op
// once the paths are covered, and a .gitignore whose existing patterns already
// cover one — a broader `.dross/*.json`, say — keeps that half byte-for-byte
// alone rather than gaining a redundant line.
//
// Mirrors ensureDrossGitattributes: same append-don't-overwrite contract, same
// call sites in init and onboard.
func ensureDrossGitignore(repoDir string) error {
	file := filepath.Join(repoDir, ".gitignore")
	existing, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	missing := drossGitignoreBlock(string(existing))
	if missing == "" {
		return nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return os.WriteFile(file, []byte(missing+"\n"), 0o644)
	}

	suffix := "\n" + missing + "\n"
	if len(existing) == 0 || strings.HasSuffix(string(existing), "\n\n") {
		suffix = missing + "\n"
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(suffix)
	return err
}

// drossGitignoreBlock is the text to append to a .gitignore whose current
// contents are body — the comment + pattern for each entry body does not
// already cover, and the empty string when it covers all of them.
func drossGitignoreBlock(body string) string {
	var blocks []string
	for _, e := range drossIgnoreEntries {
		if ignoresPath(body, e.path) {
			continue
		}
		blocks = append(blocks, e.comment+"\n"+e.path)
	}
	return strings.Join(blocks, "\n\n")
}

// ignoresPath reports whether any pattern in body already covers target — the
// exact path, a glob like `.dross/*.json`, or a directory pattern like
// `.dross/`.
//
// Negations are skipped rather than interpreted: a `!` line does not cover
// anything, and a repo that deliberately re-includes the file has made a choice
// this helper should not silently undo by appending the rule again.
func ignoresPath(body, target string) bool {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		pattern := strings.TrimPrefix(line, "/")
		if strings.HasSuffix(pattern, "/") && strings.HasPrefix(target, pattern) {
			return true
		}
		if pattern == target {
			return true
		}
		if ok, err := path.Match(pattern, target); err == nil && ok {
			return true
		}
	}
	return false
}
