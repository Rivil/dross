// Package cmd defines the cobra subcommand handlers for the dross CLI.
package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const RootDirName = ".dross"

// FindRoot walks up from the current directory looking for a .dross dir.
// Returns the absolute path to the .dross dir, or ErrNoRoot if none found.
var ErrNoRoot = errors.New("no .dross directory found in current dir or any parent — run `dross init` or `dross onboard`")

// RepairHint is the single repair instruction every not-a-dross-repo message
// points at. Shared so root.go, doctor and rule can't drift to two strings.
const RepairHint = "run `dross onboard` to adopt this directory into dross"

// RequiredRootFiles are the files a .dross directory must contain before it
// counts as an initialised root. Existence only — a file that exists but fails
// to parse is a real error reported by whoever loads it, not an uninitialised
// root (locked decision: completeness_check).
var RequiredRootFiles = []string{"project.toml", "state.json"}

// IncompleteRootError reports a .dross directory that exists but is missing at
// least one required file. It unwraps to ErrNoRoot so the hook targets, which
// already treat "not a dross repo" as a silent exit 0, stay silent (c-1).
type IncompleteRootError struct {
	// Root is the absolute path to the incomplete .dross directory.
	Root string
	// Missing holds the display paths (".dross/state.json") of absent files.
	Missing []string
}

func (e *IncompleteRootError) Error() string {
	return fmt.Sprintf("not a dross repo: %s is incomplete — missing %s — %s",
		e.Root, strings.Join(e.Missing, ", "), RepairHint)
}

func (e *IncompleteRootError) Unwrap() error { return ErrNoRoot }

// MissingRootFiles reports which of RequiredRootFiles are absent from the given
// .dross directory. It does not walk: the answer is about rootDir and nothing
// else, which is the seam `dross onboard` needs to stay cwd-scoped (locked
// decision: walk_stop). Returned paths are display-relative (".dross/state.json").
// A stat failure that isn't "not exist" (an unreadable directory, say) is
// returned as a real error rather than being misread as an absent file.
func MissingRootFiles(rootDir string) ([]string, error) {
	var missing []string
	for _, name := range RequiredRootFiles {
		_, err := os.Stat(filepath.Join(rootDir, name))
		switch {
		case err == nil:
		case errors.Is(err, fs.ErrNotExist):
			missing = append(missing, filepath.Join(RootDirName, name))
		default:
			return nil, err
		}
	}
	return missing, nil
}

// LocateRoot walks up from the current directory to the first .dross dir and
// stops there — complete or not (locked decision: walk_stop). It reports which
// required files are missing without turning that into an error, which is the
// surface `dross doctor` needs to diagnose a half-built root, and the one
// `ship recover` needs to heal one.
func LocateRoot() (string, []string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}
	dir := cwd
	for {
		candidate := filepath.Join(dir, RootDirName)
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			missing, err := MissingRootFiles(candidate)
			if err != nil {
				return "", nil, err
			}
			return candidate, missing, nil
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", nil, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil, ErrNoRoot
		}
		dir = parent
	}
}

// FindRoot resolves the .dross root, treating an incomplete one as no root at
// all (c-1): callers get an error that errors.Is(err, ErrNoRoot) matches, so
// the absent and half-built cases are indistinguishable to them.
func FindRoot() (string, error) {
	root, missing, err := LocateRoot()
	if err != nil {
		return "", err
	}
	if len(missing) > 0 {
		return "", &IncompleteRootError{Root: root, Missing: missing}
	}
	return root, nil
}

// GlobalDir returns ~/.claude/dross.
func GlobalDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "dross"), nil
}

// MustWriteFile writes data to path, creating parents.
func MustWriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Print is a thin wrapper so handlers can be tested by capturing stdout.
func Print(a ...any) { fmt.Println(a...) }

// Printf is a thin wrapper.
func Printf(format string, a ...any) { fmt.Printf(format, a...) }
