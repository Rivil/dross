package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
)

// Status registers `dross status` — the situational-awareness command.
func Status() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Where am I — project, milestone, phase, last activity, next task",
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}

			proj, err := project.Load(filepath.Join(root, project.File))
			if err != nil {
				return err
			}
			st, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}

			// Header — project + version
			name := proj.Project.Name
			if name == "" {
				name = "(unnamed)"
			}
			Printf("project:   %s  v%s\n", name, st.Version)

			if st.CurrentMilestone != "" {
				Printf("milestone: %s\n", st.CurrentMilestone)
			}

			// Phase block
			if st.CurrentPhase != "" {
				renderPhase(root, st)
			} else {
				Print("phase:     (none — try `/dross-spec --new \"<title>\"`)")
			}

			// Last activity
			if !st.LastActivity.IsZero() {
				when := st.LastActivity.Format("2006-01-02 15:04 UTC")
				if st.LastAction != "" {
					Printf("activity:  %s — %s\n", when, st.LastAction)
				} else {
					Printf("activity:  %s\n", when)
				}
			}

			// Next suggested action — heuristic from current state
			Print("")
			Printf("next:      %s\n", suggestNext(root, proj, st))
			return nil
		},
	}
}

// renderPhase prints the phase line plus task progress if a plan exists.
func renderPhase(root string, st *state.State) {
	planPath := filepath.Join(phase.Dir(root, st.CurrentPhase), "plan.toml")
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		// plan.toml missing — phase exists but isn't planned yet
		statusStr := st.CurrentPhaseStatus
		if statusStr == "" {
			statusStr = "spec / planning"
		}
		Printf("phase:     %s (%s)\n", st.CurrentPhase, statusStr)
		return
	}
	pending, inProgress, done, failed := plan.Summary()
	total := pending + inProgress + done + failed
	statusStr := st.CurrentPhaseStatus
	if statusStr == "" {
		statusStr = "in plan"
	}
	bar := progressBar(done, total, 20)
	Printf("phase:     %s (%s)\n", st.CurrentPhase, statusStr)
	Printf("           %s %d/%d done", bar, done, total)
	if inProgress > 0 {
		Printf(", %d in progress", inProgress)
	}
	if failed > 0 {
		Printf(", %d failed", failed)
	}
	if pending > 0 {
		Printf(", %d pending", pending)
	}
	Print("")
	if next := plan.NextRunnable(); next != nil {
		Printf("           next runnable: %s — %s\n", next.ID, next.Title)
	}
}

// suggestNext returns a one-line hint for "what to do now" based on state.
func suggestNext(root string, proj *project.Project, st *state.State) string {
	if proj.Project.Name == "" || proj.Runtime.Mode == "" {
		return "/dross-init or /dross-onboard — project.toml is incomplete"
	}
	if st.CurrentMilestone == "" {
		return "/dross-milestone v0.1 — scope the first milestone before clarifying phases"
	}
	if st.CurrentPhase == "" {
		return "/dross-spec --new \"<title>\" — clarify the first phase"
	}
	dir := phase.Dir(root, st.CurrentPhase)
	hasSpec := fileExists(filepath.Join(dir, "spec.toml"))
	hasPlan := fileExists(filepath.Join(dir, "plan.toml"))
	hasVerify := fileExists(filepath.Join(dir, "verify.toml"))

	if !hasSpec {
		return "/dross-spec — clarify the current phase"
	}
	if !hasPlan {
		return "/dross-plan — break the spec into tasks"
	}
	plan, err := phase.LoadPlan(filepath.Join(dir, "plan.toml"))
	if err == nil {
		_, _, _, failed := plan.Summary()
		if plan.NextRunnable() != nil {
			return "/dross-execute — run the next task"
		}
		if failed > 0 {
			return fmt.Sprintf("review %d failed task(s) — `dross task show %s <id>`", failed, st.CurrentPhase)
		}
	}
	if !hasVerify {
		return "/dross-verify — check criterion coverage and test efficacy"
	}
	// Read verify verdict to refine the hint.
	if verdict := readVerifyVerdict(filepath.Join(dir, "verify.toml")); verdict != "" {
		switch verdict {
		case "fail", "partial":
			return "verify is " + verdict + " — open " + filepath.Join(".dross/phases", st.CurrentPhase, "verify.toml") + " for findings"
		case "pass":
			// recorded changes? at least confirm there are some
			ch, _ := changes.Load(changes.FilePath(root, st.CurrentPhase), st.CurrentPhase)
			if ch != nil && len(ch.Tasks) > 0 {
				return "phase verified — start a new phase or move on"
			}
		}
	}
	return "phase looks complete — start a new phase or move on"
}

func progressBar(done, total, width int) string {
	if total == 0 {
		return "[" + strings.Repeat("·", width) + "]"
	}
	filled := done * width / total
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("·", width-filled) + "]"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readVerifyVerdict extracts the verdict field from verify.toml without
// pulling in the full verify package (which would create an import cycle
// risk later if verify ever imports cmd).
func readVerifyVerdict(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "verdict") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			v := strings.Trim(strings.TrimSpace(parts[1]), `"`)
			return v
		}
	}
	return ""
}
