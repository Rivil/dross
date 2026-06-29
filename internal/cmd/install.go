package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/assets"
)

// Install registers `dross install` — the single installer for dross slash commands
// and prompts. It materializes the embedded command skills into
// ~/.claude/skills/dross-<name>/SKILL.md and the prompts into ~/.claude/dross/prompts.
//
// Off a source checkout it symlinks assets/ (live dev edits apply immediately);
// otherwise it writes real-file copies from the embedded FS so an end-user install
// needs no Go toolchain or git checkout. --copy / --link force the mode. The sync is
// scoped to the dross-* namespace: stale dross skills/prompts this version dropped are
// pruned, and non-dross skills are never touched.
func Install() *cobra.Command {
	var copyMode, linkMode bool
	c := &cobra.Command{
		Use:   "install",
		Short: "Install dross slash commands + prompts into ~/.claude",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if copyMode && linkMode {
				return errors.New("--copy and --link are mutually exclusive")
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home: %w", err)
			}
			in := &installer{home: home, out: cmd.OutOrStdout()}
			switch {
			case linkMode:
				src, ok := detectSourceDir()
				if !ok {
					return errors.New("--link requires a source checkout (no assets/commands found in the working directory)")
				}
				in.link, in.sourceDir = true, src
			case copyMode:
				in.link = false
			default:
				if src, ok := detectSourceDir(); ok {
					in.link, in.sourceDir = true, src
				}
			}
			return in.run()
		},
	}
	c.Flags().BoolVar(&copyMode, "copy", false, "write real-file copies from the embedded assets (end-user install)")
	c.Flags().BoolVar(&linkMode, "link", false, "symlink the source assets/ for live dev edits (requires a source checkout)")
	return c
}

// detectSourceDir reports the absolute assets/ directory when dross is run from a
// source checkout (make install runs `./dross install` from the repo root, so
// assets/commands sits in the working directory).
func detectSourceDir() (string, bool) {
	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	assetsDir := filepath.Join(wd, "assets")
	if st, err := os.Stat(filepath.Join(assetsDir, "commands")); err == nil && st.IsDir() {
		return assetsDir, true
	}
	return "", false
}

// installer performs the materialize+prune. Fields are exported to the package so
// install_test.go can drive it directly against a temp HOME without cobra plumbing.
type installer struct {
	home      string    // base whose .claude/ subtree is written
	link      bool      // true = symlink source assets/; false = copy from embed.FS
	sourceDir string    // absolute assets/ path; required when link is true
	out       io.Writer // progress output (nil = discard)
}

func (in *installer) skillsDir() string  { return filepath.Join(in.home, ".claude", "skills") }
func (in *installer) promptsDir() string { return filepath.Join(in.home, ".claude", "dross", "prompts") }

func (in *installer) logf(format string, args ...any) {
	if in.out != nil {
		fmt.Fprintf(in.out, format, args...)
	}
}

func (in *installer) run() error {
	if in.link && in.sourceDir == "" {
		return errors.New("link mode requires a source directory")
	}
	if err := in.syncSkills(); err != nil {
		return err
	}
	if err := in.syncPrompts(); err != nil {
		return err
	}
	return nil
}

// commandSkills returns the skill directory names (dross-<name>) for every embedded
// command, plus the source filename for each.
func commandSkills() (map[string]string, error) {
	entries, err := fs.ReadDir(assets.FS, "commands")
	if err != nil {
		return nil, fmt.Errorf("read embedded commands: %w", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md") // e.g. dross-execute
		out[name] = e.Name()
	}
	return out, nil
}

func (in *installer) syncSkills() error {
	skills, err := commandSkills()
	if err != nil {
		return err
	}
	skillsDir := in.skillsDir()
	for name, file := range skills {
		skillDir := filepath.Join(skillsDir, name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", skillDir, err)
		}
		skillMD := filepath.Join(skillDir, "SKILL.md")
		// Replace whatever is there (symlink or stale copy) so mode switches cleanly.
		if err := os.Remove(skillMD); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clear %s: %w", skillMD, err)
		}
		if in.link {
			target := filepath.Join(in.sourceDir, "commands", file)
			if err := os.Symlink(target, skillMD); err != nil {
				return fmt.Errorf("symlink %s: %w", skillMD, err)
			}
		} else {
			b, err := assets.FS.ReadFile("commands/" + file)
			if err != nil {
				return fmt.Errorf("read embedded %s: %w", file, err)
			}
			if err := os.WriteFile(skillMD, b, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", skillMD, err)
			}
		}
		in.logf("skill   → %s\n", skillMD)
	}
	return in.pruneStaleSkills(skillsDir, skills)
}

// pruneStaleSkills removes dross-* skill directories this version no longer ships,
// and never touches a skill that isn't in the dross-* namespace.
func (in *installer) pruneStaleSkills(skillsDir string, want map[string]string) error {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read skills dir: %w", err)
	}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, "dross-") {
			continue // never touch non-dross skills
		}
		if _, keep := want[n]; keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(skillsDir, n)); err != nil {
			return fmt.Errorf("prune stale skill %s: %w", n, err)
		}
		in.logf("prune   → %s\n", filepath.Join(skillsDir, n))
	}
	return nil
}

func (in *installer) syncPrompts() error {
	promptsDir := in.promptsDir()
	if err := os.MkdirAll(filepath.Dir(promptsDir), 0o755); err != nil {
		return fmt.Errorf("mkdir prompts parent: %w", err)
	}
	if in.link {
		// Symlink the whole prompts directory; clear any prior real dir or stale link.
		if err := clearPath(promptsDir); err != nil {
			return err
		}
		target := filepath.Join(in.sourceDir, "prompts")
		if err := os.Symlink(target, promptsDir); err != nil {
			return fmt.Errorf("symlink prompts: %w", err)
		}
		in.logf("prompts → %s → %s\n", promptsDir, target)
		return nil
	}

	// Copy mode: ensure promptsDir is a real directory (drop a prior symlink).
	if fi, err := os.Lstat(promptsDir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(promptsDir); err != nil {
			return fmt.Errorf("clear prompts symlink: %w", err)
		}
	}
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir prompts: %w", err)
	}
	entries, err := fs.ReadDir(assets.FS, "prompts")
	if err != nil {
		return fmt.Errorf("read embedded prompts: %w", err)
	}
	want := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		want[e.Name()] = true
		b, err := assets.FS.ReadFile("prompts/" + e.Name())
		if err != nil {
			return fmt.Errorf("read embedded prompt %s: %w", e.Name(), err)
		}
		dst := filepath.Join(promptsDir, e.Name())
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
		in.logf("prompt  → %s\n", dst)
	}
	return in.pruneStalePrompts(promptsDir, want)
}

// pruneStalePrompts removes prompt files this version no longer ships. The prompts
// directory is wholly dross-owned (~/.claude/dross/prompts), so every stale *.md is
// fair game; copy-mode installs would otherwise accumulate dropped prompts.
func (in *installer) pruneStalePrompts(promptsDir string, want map[string]bool) error {
	entries, err := os.ReadDir(promptsDir)
	if err != nil {
		return fmt.Errorf("read prompts dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || want[e.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(promptsDir, e.Name())); err != nil {
			return fmt.Errorf("prune stale prompt %s: %w", e.Name(), err)
		}
		in.logf("prune   → %s\n", filepath.Join(promptsDir, e.Name()))
	}
	return nil
}

// clearPath removes a file/symlink/dir at p so it can be recreated. A symlink is
// unlinked (never its target); a real directory is removed recursively.
func clearPath(p string) error {
	fi, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return os.Remove(p)
	}
	return os.RemoveAll(p)
}
