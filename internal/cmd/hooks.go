package cmd

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/hooks"
)

// Hooks exposes the user-level hook wiring as its own verb. init and onboard
// ensure the hooks as a side effect, but installs that predate the hooks (or
// users who never re-run init/onboard) need a standalone way in.
func Hooks() *cobra.Command {
	root := &cobra.Command{
		Use:   "hooks",
		Short: "Manage the dross-owned Claude Code hooks",
	}
	root.AddCommand(&cobra.Command{
		Use:   "ensure",
		Short: "Idempotently wire the dross PreCompact + SessionStart hooks into user-level settings.json",
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := userSettingsPath()
			if err != nil {
				return err
			}
			before, err := os.ReadFile(path)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if err := ensureUserHooks(); err != nil {
				return err
			}
			after, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.Equal(before, after) {
				Printf("hooks already wired → %s\n", path)
				return nil
			}
			Printf("hooks ensured: PreCompact (%s) + SessionStart (%s) → %s\n", preCompactHookCommand, sessionStartHookCommand, path)
			return nil
		},
	})
	return root
}

// Hook command strings are plan-fixed constants (the hook_scope locked
// decision): user-level hooks fire in every repo, and both verbs no-op
// instantly outside a dross repo, so one wiring covers everything.
const (
	preCompactHookCommand   = "dross pause --auto"
	sessionStartHookCommand = "dross reentry"
)

// ensureUserHooks idempotently wires the dross PreCompact and SessionStart
// hooks into the user-level Claude settings.json via hooks.MergeHook. Already
// wired → no write at all (byte-stable). init and onboard both call this, so
// whichever runs first does the wiring and the other confirms it.
func ensureUserHooks() error {
	path, err := userSettingsPath()
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	merged := existing
	for _, h := range []struct{ event, command string }{
		{hooks.EventPreCompact, preCompactHookCommand},
		{hooks.EventSessionStart, sessionStartHookCommand},
	} {
		if merged, err = hooks.MergeHook(merged, h.event, h.command); err != nil {
			return err
		}
	}
	if bytes.Equal(merged, existing) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, merged, 0o644)
}

// userSettingsPath resolves the user-level Claude settings.json, honouring
// CLAUDE_CONFIG_DIR the same way Claude Code does.
func userSettingsPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}
