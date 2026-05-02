// Package cmd defines the cobra subcommand handlers for the dross CLI.
package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const RootDirName = ".dross"

// FindRoot walks up from the current directory looking for a .dross dir.
// Returns the absolute path to the .dross dir, or ErrNoRoot if none found.
var ErrNoRoot = errors.New("no .dross directory found in current dir or any parent — run `dross init` or `dross onboard`")

func FindRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		candidate := filepath.Join(dir, RootDirName)
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, nil
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNoRoot
		}
		dir = parent
	}
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
