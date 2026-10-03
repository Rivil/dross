package cmd

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
		Short: "Idempotently wire the dross hooks (PreCompact, SessionStart, and the PreToolUse/PostToolUse tool-call gates) into user-level settings.json",
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
			Printf("hooks ensured: PreCompact (%s) + SessionStart (%s) + PreToolUse (%s) + PostToolUse (%s) → %s\n",
				preCompactHookCommand, sessionStartHookCommand, GateCheckHook, GateRecordHook, path)
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

// userHooks is every hook ensureUserHooks wires, in order: the event and the
// command it runs.
var userHooks = []struct{ event, command string }{
	{hooks.EventPreCompact, preCompactHookCommand},
	{hooks.EventSessionStart, sessionStartHookCommand},
	{hooks.EventPreToolUse, GateCheckHook},
	{hooks.EventPostToolUse, GateRecordHook},
}

// userHooksSummary names each wired hook as "event → command", for the line
// init and onboard print after ensuring them.
func userHooksSummary() string {
	parts := make([]string, len(userHooks))
	for i, h := range userHooks {
		parts[i] = h.event + " → " + h.command
	}
	return strings.Join(parts, ", ")
}

// ensureUserHooks idempotently wires the dross hooks into the user-level
// Claude settings.json via hooks.MergeHook: PreCompact and SessionStart, and
// the tool-call gates' PreToolUse check and PostToolUse record — matcher-less,
// so they see every tool call. Already wired → no write at all (byte-stable).
// init and onboard both call this, so whichever runs first does the wiring and
// the other confirms it.
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
	for _, h := range userHooks {
		if merged, err = hooks.MergeHook(merged, h.event, h.command); err != nil {
			return err
		}
	}
	if bytes.Equal(merged, existing) {
		return nil
	}
	return writeSettingsAtomic(path, merged)
}

// writeSettingsAtomic replaces settings.json through a temp file renamed over
// it, keeping the file's mode (0600 when it is new): settings.json can hold
// tokens in its env block, and a write that failed halfway must leave the
// original bytes, not a truncated file every Claude Code session then reads.
func writeSettingsAtomic(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings.json.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once the rename has moved it
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
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
