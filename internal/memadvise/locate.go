// Package memadvise reads a repo's Claude Code memory dir and suggests prunes:
// memories whose index line costs tokens on every turn but that duplicate a
// tracked doc, carry an expired time-bound clause, or break index integrity.
// It only ever reads the memory dir; the one writer is Apply, behind an
// explicit confirmation.
package memadvise

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// ClaudeDir is Claude Code's config dir: $CLAUDE_CONFIG_DIR when set, else
// ~/.claude. The home dir is only resolved when the override is absent, so a
// set CLAUDE_CONFIG_DIR never touches ~/.claude.
func ClaudeDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// Slug is the project dir name Claude Code files a repo's transcripts and
// memory under: the repo's path with symlinks resolved and every byte outside
// [A-Za-z0-9] mapped to '-'. A path that does not resolve (it need not exist —
// tests slug synthetic paths) is slugged as cleaned.
func Slug(repo string) string {
	p := filepath.Clean(repo)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	b := []byte(p)
	for i, c := range b {
		if !isAlnum(c) {
			b[i] = '-'
		}
	}
	return string(b)
}

func isAlnum(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9'
}

// Locate returns the memory dir for the repo whose top level is repo. ok is
// false — with a nil error — when there is nothing to advise on: the dir is
// missing, is not a directory, or memory/ is a symlink (a link could point the
// advisor, and later Apply, anywhere on disk). err is reserved for failures
// that are not "absent": an unresolvable home dir or an unreadable parent.
func Locate(repo string) (dir string, ok bool, err error) {
	claude, err := ClaudeDir()
	if err != nil {
		return "", false, err
	}
	dir = filepath.Join(claude, "projects", Slug(repo), "memory")
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		// ENOTDIR: a parent component is a file, so there is no dir below it.
		return dir, false, nil
	case err != nil:
		return dir, false, err
	case !info.IsDir():
		// A symlink's Lstat mode is ModeSymlink, never a dir, so this one arm
		// refuses both a regular file and a linked memory/.
		return dir, false, nil
	}
	return dir, true, nil
}
