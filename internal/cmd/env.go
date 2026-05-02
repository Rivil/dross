package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Env manages dross-relevant environment variables stored in
// ~/.claude/settings.json under the .env block. Token values are NEVER
// echoed back — `dross env set` reads from the tty with input hidden,
// and `dross env list` shows only key + length.
//
// Tokens are typed in the user's own shell, not via Claude Code chat,
// so secrets never enter Claude's conversation context.
func Env() *cobra.Command {
	c := &cobra.Command{
		Use:   "env",
		Short: "Manage env keys in ~/.claude/settings.json (values never displayed)",
	}
	c.AddCommand(envList(), envSet(), envUnset())
	return c
}

const settingsRelPath = ".claude/settings.json"

func settingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, settingsRelPath), nil
}

func envList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List env keys present in ~/.claude/settings.json (values masked)",
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := settingsPath()
			if err != nil {
				return err
			}
			envMap, err := readEnvMap(path)
			if err != nil {
				return err
			}

			keys := make([]string, 0, len(envMap))
			for k := range envMap {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			Printf("# %s\n", path)
			if len(keys) == 0 {
				Print("  (no env keys)")
			} else {
				for _, k := range keys {
					v, _ := envMap[k].(string)
					Printf("  %-30s set (length %d)\n", k, len(v))
				}
			}

			// Surface project's [remote].auth_env if it points to a key
			// that's missing from settings.json — common cause of "doctor
			// says auth_env not set" surprises.
			if p, _, err := loadProject(); err == nil && p.Remote.AuthEnv != "" {
				if _, ok := envMap[p.Remote.AuthEnv]; !ok {
					Printf("  %-30s NOT SET (project [remote].auth_env)\n", p.Remote.AuthEnv)
				}
			}
			return nil
		},
	}
}

func envSet() *cobra.Command {
	return &cobra.Command{
		Use:   "set <KEY>",
		Short: "Prompt for value (hidden input) and write to settings.json",
		Long: `Reads a value from the controlling terminal with input hidden, then
writes it to ~/.claude/settings.json under .env[KEY]. Run this in your
own shell — never paste tokens into Claude Code chat.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			if key == "" {
				return errors.New("KEY must be non-empty")
			}
			fmt.Fprintf(os.Stderr, "Value for %s (input hidden, Enter to abort): ", key)
			value, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			if len(value) == 0 {
				return errors.New("aborted: empty value")
			}
			path, err := settingsPath()
			if err != nil {
				return err
			}
			if err := mutateSettings(path, func(doc map[string]any) {
				envMap, _ := doc["env"].(map[string]any)
				if envMap == nil {
					envMap = map[string]any{}
				}
				envMap[key] = string(value)
				doc["env"] = envMap
			}); err != nil {
				return err
			}
			Printf("Updated %s in %s (length %d)\n", key, path, len(value))
			return nil
		},
	}
}

func envUnset() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <KEY>",
		Short: "Remove a key from settings.json env",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path, err := settingsPath()
			if err != nil {
				return err
			}
			removed := false
			if err := mutateSettings(path, func(doc map[string]any) {
				envMap, _ := doc["env"].(map[string]any)
				if envMap == nil {
					return
				}
				if _, ok := envMap[args[0]]; ok {
					delete(envMap, args[0])
					removed = true
				}
			}); err != nil {
				return err
			}
			if removed {
				Printf("Removed %s from %s\n", args[0], path)
			} else {
				Printf("%s not present in %s — nothing to do\n", args[0], path)
			}
			return nil
		},
	}
}

func readEnvMap(path string) (map[string]any, error) {
	doc, err := readSettings(path)
	if err != nil {
		return nil, err
	}
	envMap, _ := doc["env"].(map[string]any)
	if envMap == nil {
		return map[string]any{}, nil
	}
	return envMap, nil
}

func readSettings(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return doc, nil
}

// mutateSettings reads, applies fn, writes back atomically with mode
// 0o600 since the file holds tokens. JSON map order is alphabetical
// after marshal — acceptable since JSON has no ordering semantics.
func mutateSettings(path string, fn func(map[string]any)) error {
	doc, err := readSettings(path)
	if err != nil {
		return err
	}
	fn(doc)
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
