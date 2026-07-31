package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
)

// autoSnapshotHeading is the one section of handoff.md that `pause --auto`
// owns. Everything outside it is the user's hand-written working memory and is
// never touched (the auto_handoff_merge locked decision).
const autoSnapshotHeading = "## Auto-snapshot"

func Pause() *cobra.Command {
	var auto bool
	c := &cobra.Command{
		Use:   "pause",
		Short: "Handoff helpers — `--auto` merges a mechanical snapshot into .dross/handoff.md",
		RunE: func(_ *cobra.Command, _ []string) error {
			if !auto {
				return errors.New("interactive pause is the /dross-pause slash command; the CLI only implements `dross pause --auto`")
			}
			return pauseAuto(time.Now().UTC())
		},
	}
	c.Flags().BoolVar(&auto, "auto", false,
		"non-interactive: write the mechanical snapshot section of .dross/handoff.md (PreCompact hook target)")
	return c
}

// pauseAuto captures the mechanical snapshot. Outside a dross repo it exits
// clean and silent — the PreCompact hook is user-level and fires in every
// repo, so a non-dross cwd is the normal case, not an error. It never reads
// stdin and never prompts.
func pauseAuto(now time.Time) error {
	root, err := FindRoot()
	if errors.Is(err, ErrNoRoot) {
		return nil
	}
	if err != nil {
		return err
	}

	// Render before touching the file: a broken `.dross/` must fail without
	// leaving a half-written handoff behind, and must leave an existing one
	// byte-identical.
	section, err := autoSnapshot(root, now)
	if err != nil {
		return err
	}

	path := filepath.Join(root, "handoff.md")
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	merged := mergeAutoSnapshot(existing, section)
	return os.WriteFile(path, merged, 0o644)
}

// autoSnapshot renders the full auto-owned section, trailing newline included.
//
// Two classes of failure, deliberately handled differently. A missing git repo
// is an environment fact — every git line degrades to "(no git)" and the hook
// still writes. A project.toml or state.json that won't decode is *broken
// state* (locked completeness_check): degrading past it would make the
// PreCompact hook quietly emit a snapshot with the phase line missing, so it
// is returned as an error instead.
func autoSnapshot(root string, now time.Time) (string, error) {
	repoDir := filepath.Dir(root)

	var b strings.Builder
	b.WriteString(autoSnapshotHeading + "\n")
	fmt.Fprintf(&b, "- captured: %s\n", now.Format("2006-01-02 15:04 UTC"))

	branch := "(no git)"
	if out, err := exec.Command("git", "-C", repoDir, "symbolic-ref", "--short", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}
	fmt.Fprintf(&b, "- branch: %s\n", branch)
	fmt.Fprintf(&b, "- dirty: %s\n", dirtySummary(repoDir))

	st, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		return "", err
	}
	phase := "(none)"
	if st.CurrentPhase != "" {
		phase = st.CurrentPhase
		if st.CurrentPhaseStatus != "" {
			phase += " (" + st.CurrentPhaseStatus + ")"
		}
	}
	fmt.Fprintf(&b, "- phase: %s · v%s\n", phase, st.Version)

	proj, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "- next: %s\n", suggestNext(root, proj, st))
	return b.String(), nil
}

// dirtySummary renders `git status --porcelain` as one line: "clean", or a
// count plus the first few paths.
func dirtySummary(repoDir string) string {
	out, err := exec.Command("git", "-C", repoDir, "status", "--porcelain").Output()
	if err != nil {
		return "(no git)"
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return "clean"
	}
	paths := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(l) > 3 {
			paths = append(paths, strings.TrimSpace(l[3:]))
		}
	}
	const show = 5
	summary := fmt.Sprintf("%d file(s): %s", len(paths), strings.Join(paths[:min(show, len(paths))], ", "))
	if len(paths) > show {
		summary += fmt.Sprintf(" +%d more", len(paths)-show)
	}
	return summary
}

// mergeAutoSnapshot replaces the "## Auto-snapshot" section of a handoff
// document with section, leaving every byte outside that block untouched. The
// owned block runs from its heading line to the next "## " heading (exclusive)
// or EOF. Absent the heading, the section is appended after a blank line; an
// empty document becomes just the section. Deterministic for a fixed section,
// so repeated merges are byte-stable.
func mergeAutoSnapshot(existing []byte, section string) []byte {
	text := string(existing)
	if strings.TrimSpace(text) == "" {
		return []byte(section)
	}

	start := headingStart(text)
	if start == -1 {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		return []byte(text + "\n" + section)
	}

	end := len(text)
	afterHeading := start + len(autoSnapshotHeading)
	if i := strings.Index(text[afterHeading:], "\n## "); i != -1 {
		end = afterHeading + i + 1 // line start of the next section's heading
	}

	replacement := section
	if end < len(text) {
		replacement += "\n" // keep a blank line before the section that follows
	}
	return []byte(text[:start] + replacement + text[end:])
}

// headingStart returns the byte offset of the auto-snapshot heading at a line
// start, or -1.
func headingStart(text string) int {
	if strings.HasPrefix(text, autoSnapshotHeading) {
		return 0
	}
	if i := strings.Index(text, "\n"+autoSnapshotHeading); i != -1 {
		return i + 1
	}
	return -1
}
