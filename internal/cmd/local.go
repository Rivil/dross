package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
)

// Local manages .dross/local.toml — machine-local values that must NOT ride
// cumulative history.
//
// It exists because state.json USED to ride it: state.json was committed and
// every phase squash-merged it onto the base, so a value written there was
// inherited by every later tree on that branch. For a "which branch did this
// work fork from?" record that is exactly wrong — a stale answer from one
// machine's standalone quick task would be dragged forward and later
// reconciled against.
//
// state.json is gitignored now too (locked state_tracking), for the same
// reason plus a worse one: a checkout could replay a branch's stale copy over
// the live file. The two stores stay separate anyway — local.toml is typed,
// hand-editable config for machine-local values, state.json is position data
// dross owns.
//
// Phase work does not use this store — a phase's forked-from base lives in its
// phase-scoped changes.json, which cannot be dragged forward either.
func Local() *cobra.Command {
	c := &cobra.Command{
		Use:   "local",
		Short: "Read and write .dross/local.toml (gitignored, machine-local values)",
	}
	c.AddCommand(localGet(), localSet())
	return c
}

// LocalFile is the store's name under .dross/.
const LocalFile = "local.toml"

// localStore is the closed set of machine-local keys. Adding a key here is the
// only way to add one to the store — an unknown key is an error, never a
// silently-written entry that no reader will ever look for.
type localStore struct {
	// QuickBase is the branch a standalone `/dross-quick` forked from and
	// committed to. ship and complete reconcile it alongside the phase base so
	// an unpushed .dross chore left on it can't re-seed divergence.
	QuickBase string `toml:"quick_base,omitempty"`
}

// localKeys maps each key to its accessors, keeping `local get` and
// `local set` agreeing on the key set by construction.
var localKeys = map[string]struct {
	get func(*localStore) string
	set func(*localStore, string)
}{
	"quick_base": {
		get: func(l *localStore) string { return l.QuickBase },
		set: func(l *localStore, v string) { l.QuickBase = v },
	},
}

func localKeyNames() string {
	names := make([]string, 0, len(localKeys))
	for k := range localKeys {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func localPath(root string) string { return filepath.Join(root, LocalFile) }

// loadLocal reads the store. A missing file is an empty store, not an error —
// the file is gitignored, so a fresh clone legitimately has none.
func loadLocal(path string) (*localStore, error) {
	var l localStore
	_, err := toml.DecodeFile(path, &l)
	if errors.Is(err, fs.ErrNotExist) {
		return &localStore{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &l, nil
}

// save writes the store, creating it on demand.
func (l *localStore) save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	enc.Indent = "  "
	if err := enc.Encode(l); err != nil {
		return fmt.Errorf("encode local: %w", err)
	}
	return nil
}

// readLocalKey is the reader other commands use. Any failure to read the
// store yields an empty value rather than an error: the store is an
// optimisation for reconciliation, never a gate.
func readLocalKey(root, key string) string {
	l, err := loadLocal(localPath(root))
	if err != nil {
		return ""
	}
	acc, ok := localKeys[key]
	if !ok {
		return ""
	}
	return acc.get(l)
}

func localGet() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print a machine-local value (empty when unset)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			acc, ok := localKeys[args[0]]
			if !ok {
				return fmt.Errorf("unknown local key %q (want %s)", args[0], localKeyNames())
			}
			l, err := loadLocal(localPath(root))
			if err != nil {
				return err
			}
			// An unset key prints nothing and exits 0 — callers branch on
			// empty output, so a missing value must not look like a failure.
			if v := acc.get(l); v != "" {
				Print(v)
			}
			return nil
		},
	}
}

func localSet() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Write a machine-local value to .dross/local.toml",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			acc, ok := localKeys[args[0]]
			if !ok {
				return fmt.Errorf("unknown local key %q (want %s)", args[0], localKeyNames())
			}
			path := localPath(root)
			l, err := loadLocal(path)
			if err != nil {
				return err
			}
			acc.set(l, args[1])
			if err := l.save(path); err != nil {
				return err
			}
			Printf("%s = %s\n", args[0], args[1])
			return nil
		},
	}
}
