package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/milestone"
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
				renderMilestone(root, st.CurrentMilestone)
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

			// Pending verify verdicts across all phases. Passive nudge for
			// the forget-to-finalize hole — `dross verify` writes verify.toml
			// with verdict="" or "pending", and the user is supposed to
			// resolve it and run `dross verify finalize` later. Easy to
			// forget. Surface them here so every status check reminds.
			if pending := pendingVerdicts(root); len(pending) > 0 {
				Printf("pending:   %d unfinalized verdict(s): %s\n", len(pending), strings.Join(pending, ", "))
				Printf("           run `dross verify finalize <phase>` once verify.toml's verdict is filled in\n")
			}

			// Open handoff. /dross-pause leaves a living note at
			// .dross/handoff.md; surface it so you don't forget you paused.
			if hand := openHandoff(root); hand != "" {
				Printf("handoff:   %s\n", hand)
			}

			// Non-spine action areas — surfaced only when the spine is idle
			// (between phases, or the current phase is done with nothing left
			// to do), so live work isn't cluttered (c-1, c-3).
			if spineIdle(root, proj, st) {
				if lines := renderActionAreas(actionCatalog); len(lines) > 0 {
					Printf("actions:   %s\n", lines[0])
					for _, l := range lines[1:] {
						Printf("           %s\n", l)
					}
				}
			}

			// Next suggested action — heuristic from current state
			Print("")
			Printf("next:      %s\n", suggestNext(root, proj, st))
			return nil
		},
	}
}

// renderMilestone prints the milestone line, augmented with phase-level
// progress — how many of the milestone's phases are verified (verdict="pass")
// out of the total it lists. This is the milestone view (N/M phases), distinct
// from the phase block below it (which shows the current phase's task count).
// Falls back to the bare name when the milestone toml is missing or lists no
// phases (e.g. a freshly-set current_milestone with no scoped toml yet).
func renderMilestone(root, version string) {
	m, err := milestone.Load(milestone.FilePath(root, version))
	if err != nil || len(m.Phases) == 0 {
		Printf("milestone: %s\n", version)
		return
	}
	done := 0
	for _, id := range m.Phases {
		if readVerifyVerdict(filepath.Join(phase.Dir(root, id), "verify.toml")) == "pass" {
			done++
		}
	}
	total := len(m.Phases)
	if title := strings.TrimSpace(m.Milestone.Title); title != "" {
		Printf("milestone: %s — %s\n", version, title)
	} else {
		Printf("milestone: %s\n", version)
	}
	Printf("           %s %d/%d phases\n", progressBar(done, total, 20), done, total)
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

// spineIdle reports whether the spec→ship spine has no actionable step left —
// the moment to surface non-spine action areas (c-1/c-3). It derives the answer
// from the same real signals suggestNext uses (NextRunnable, failed tasks,
// verify verdict), NOT from the free-text CurrentPhaseStatus field, which isn't
// reliably set to a terminal value.
func spineIdle(root string, proj *project.Project, st *state.State) bool {
	// Project/milestone setup still pending → the spine has a setup step.
	if proj.Project.Name == "" || proj.Runtime.Mode == "" {
		return false
	}
	if st.CurrentMilestone == "" {
		return false
	}
	// Between phases — the canonical idle moment.
	if st.CurrentPhase == "" {
		return true
	}
	dir := phase.Dir(root, st.CurrentPhase)
	if !fileExists(filepath.Join(dir, "spec.toml")) || !fileExists(filepath.Join(dir, "plan.toml")) {
		return false // spec/plan step still pending
	}
	if plan, err := phase.LoadPlan(filepath.Join(dir, "plan.toml")); err == nil {
		_, _, _, failed := plan.Summary()
		if plan.NextRunnable() != nil || failed > 0 {
			return false // execute / fix-failed step still pending
		}
	}
	// No runnable task and nothing failed, but verify is still a spine step
	// until it passes. A missing verify.toml means verify hasn't run; any
	// non-"pass" verdict (empty/pending/partial/fail) leaves work to do. Only a
	// finalized pass counts as in-phase idle. (Between-phases idle is handled
	// above by the empty current_phase.)
	vp := filepath.Join(dir, "verify.toml")
	if !fileExists(vp) {
		return false // verify step still pending
	}
	return readVerifyVerdict(vp) == "pass"
}

// openHandoff returns a one-line nudge if an unresolved handoff note exists
// (.dross/handoff.md, written by /dross-pause and pruned by /dross-resume).
// Returns "" when there's no handoff — the common case. Mirrors the
// pending-verdicts nudge: status is the natural place to remember you paused.
func openHandoff(root string) string {
	path := filepath.Join(root, "handoff.md")
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(b)

	// "Paused when": prefer the timestamp the handoff header records
	// ("# Handoff — paused <ts>"); fall back to the file's mtime.
	when := info.ModTime().Format("2006-01-02 15:04")
	first, _, _ := strings.Cut(text, "\n")
	if _, after, found := strings.Cut(first, "paused "); found {
		if ts := strings.TrimSpace(after); ts != "" {
			when = ts
		}
	}

	// Count still-open checklist items so the nudge reflects remaining work.
	line := fmt.Sprintf("⏸ open handoff (paused %s)", when)
	if open := strings.Count(text, "- [ ]"); open > 0 {
		line += fmt.Sprintf(", %d item(s) left", open)
	}
	return line + " — /dross-resume"
}

// actionArea is one non-spine area of work surfaced when the spine is idle
// (security, quality, tech-debt). The catalog is fixed in code — no config —
// and each later phase flips its area `available` once the backing command
// ships.
type actionArea struct {
	label     string
	command   string // slash command or short hint; "" when none exists yet
	available bool
}

// actionCatalog is the fixed set of non-spine areas. All are not-yet-available
// in this phase: security/quality land their commands in later phases and the
// tech-debt scanner is deferred, so each renders with a "(planned)" marker.
var actionCatalog = []actionArea{
	{label: "security", command: "/dross-secure", available: false},
	{label: "quality", command: "/dross-quality", available: false},
	{label: "tech-debt", command: "", available: false},
}

// renderActionAreas formats the `actions:` block body for the given areas.
// An available area emits its runnable command; an unavailable one is marked
// "(planned)" and is never presented as runnable. Returns nil for no areas.
func renderActionAreas(areas []actionArea) []string {
	if len(areas) == 0 {
		return nil
	}
	// Align labels into a column so the block scans like the other status rows.
	width := 0
	for _, a := range areas {
		if len(a.label) > width {
			width = len(a.label)
		}
	}
	var lines []string
	for _, a := range areas {
		label := fmt.Sprintf("%-*s", width, a.label)
		var detail string
		switch {
		case a.available && a.command != "":
			detail = a.command
		case a.command != "":
			detail = a.command + " (planned)"
		default:
			detail = "(planned)"
		}
		lines = append(lines, fmt.Sprintf("%s — %s", label, detail))
	}
	return lines
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

// pendingVerdicts returns phase IDs whose verify.toml exists but has an
// unresolved verdict (empty or "pending"). Used by status to surface
// the forget-to-finalize hole. Returns nil if there are no phases or no
// verify.toml files yet — the empty case is the common one.
func pendingVerdicts(root string) []string {
	phases, err := phase.List(root)
	if err != nil || len(phases) == 0 {
		return nil
	}
	var pending []string
	for _, id := range phases {
		vp := filepath.Join(phase.Dir(root, id), "verify.toml")
		if !fileExists(vp) {
			continue
		}
		v := readVerifyVerdict(vp)
		if v == "" || v == "pending" {
			pending = append(pending, id)
		}
	}
	return pending
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
