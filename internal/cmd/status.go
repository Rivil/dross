package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/findings"
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

			// Stale-completion guard. On a shipped-but-unmerged phase branch,
			// `dross ship` has already folded the `completed <id>` record into
			// branch-local state, but origin/<main> hasn't received the squash
			// yet — so the phase reads "done" while its PR is still open. Warn
			// only; never mutate (the user reconciles from origin or abandons
			// the branch).
			mainBranch := proj.Repo.GitMainBranch
			if mainBranch == "" {
				mainBranch = "main"
			}
			if pid, ok := staleCompletedState(filepath.Dir(root), st, mainBranch); ok {
				Printf("stale:     on phase/%s but state reads completed — PR not merged on origin/%s\n", pid, mainBranch)
				Printf("           reconcile: re-sync from origin/%s or abandon the branch (status never auto-mutates)\n", mainBranch)
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
				ranked := rankAreas(loadAreaSignals(root, actionCatalog))
				if lines := renderActionAreas(ranked, time.Now().UTC()); len(lines) > 0 {
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
	pos := phasePosition(root, st)
	planPath := filepath.Join(phase.Dir(root, st.CurrentPhase), "plan.toml")
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		// plan.toml missing — phase exists but isn't planned yet
		statusStr := st.CurrentPhaseStatus
		if statusStr == "" {
			statusStr = "spec / planning"
		}
		Printf("phase:     %s (%s)%s\n", st.CurrentPhase, statusStr, pos)
		return
	}
	pending, inProgress, done, failed := plan.Summary()
	total := pending + inProgress + done + failed
	statusStr := st.CurrentPhaseStatus
	if statusStr == "" {
		statusStr = "in plan"
	}
	bar := progressBar(done, total, 20)
	Printf("phase:     %s (%s)%s\n", st.CurrentPhase, statusStr, pos)
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

// phasePosition returns a " · N of M" suffix locating the current phase within
// its milestone's ordered phases array (N from phase.DisplayNumber, M the array
// length). Empty when there's no current milestone, no scoped array, or the
// phase isn't in it — the number is derived from array position, so it stays
// correct after a reorder.
func phasePosition(root string, st *state.State) string {
	if st.CurrentMilestone == "" {
		return ""
	}
	m, err := milestone.Load(milestone.FilePath(root, st.CurrentMilestone))
	if err != nil || len(m.Phases) == 0 {
		return ""
	}
	n := phase.DisplayNumber(m.Phases, st.CurrentPhase)
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" · %d of %d", n, len(m.Phases))
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
// (security, quality, tech-debt). The catalog is fixed in code — no config.
// stateDir names the area's subdir under .dross holding its state.toml, the
// source of the store-level last_run signal status ranks by.
type actionArea struct {
	label     string
	command   string // slash command or CLI hint; "" when none exists yet
	stateDir  string // subdir under .dross with this area's state.toml; "" = no signal
	available bool
}

// actionCatalog is the fixed set of non-spine areas. All three are now runnable:
// /dross-secure and /dross-quality are slash commands, `dross techdebt` is a CLI
// command. Each carries a state.toml under .dross/<stateDir>/ that records its
// last run, so status can rank them by staleness.
var actionCatalog = []actionArea{
	{label: "security", command: "/dross-secure", stateDir: "security", available: true},
	{label: "quality", command: "/dross-quality", stateDir: "quality", available: true},
	{label: "tech-debt", command: "dross techdebt", stateDir: "techdebt", available: true},
}

// areaSignal pairs an action area with its store-level last_run. A zero lastRun
// means the area has never run.
type areaSignal struct {
	area    actionArea
	lastRun time.Time
}

// loadAreaSignals reads each area's store-level last_run from
// .dross/<stateDir>/state.toml. A missing or garbled ledger degrades to a zero
// time (never-run) so status always renders — it never fails on a corrupt store.
func loadAreaSignals(root string, areas []actionArea) []areaSignal {
	out := make([]areaSignal, 0, len(areas))
	for _, a := range areas {
		var lr time.Time
		if a.stateDir != "" {
			if s, err := findings.LoadStore(filepath.Join(root, a.stateDir, "state.toml")); err == nil {
				lr = s.LastRun
			}
		}
		out = append(out, areaSignal{area: a, lastRun: lr})
	}
	return out
}

// rankAreas orders areas by run signal: never-run areas first (kept in stable
// catalog order), then ran areas oldest-last-run first — so the most-neglected
// area sits at the top. Identical timestamps keep catalog order (stable sort).
func rankAreas(sigs []areaSignal) []areaSignal {
	out := make([]areaSignal, len(sigs))
	copy(out, sigs)
	sort.SliceStable(out, func(i, j int) bool {
		zi, zj := out[i].lastRun.IsZero(), out[j].lastRun.IsZero()
		if zi != zj {
			return zi // never-run sorts before any ran area
		}
		if zi {
			return false // both never-run: preserve catalog order
		}
		return out[i].lastRun.Before(out[j].lastRun) // both ran: oldest first
	})
	return out
}

// formatRunSignal renders an area's run signal relative to now: "never run" for a
// zero time, otherwise "last run <age>". A future timestamp (clock skew) clamps
// to "just now" rather than rendering a negative age.
func formatRunSignal(now, lastRun time.Time) string {
	if lastRun.IsZero() {
		return "never run"
	}
	d := now.Sub(lastRun)
	if d < time.Minute {
		return "last run just now"
	}
	return "last run " + humanizeAge(d)
}

// humanizeAge renders a positive duration (>= 1 minute) as a coarse "<n>m/h/d ago".
func humanizeAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours())/24)
	}
}

// renderActionAreas formats the `actions:` block body for the ranked areas. An
// available area emits its runnable command plus its run-signal text; an
// unavailable one is marked "(planned)" and never presented as runnable (kept
// for forward-compat with areas whose command hasn't shipped). Returns nil for
// no areas.
func renderActionAreas(sigs []areaSignal, now time.Time) []string {
	if len(sigs) == 0 {
		return nil
	}
	// Align labels into a column so the block scans like the other status rows.
	width := 0
	for _, s := range sigs {
		if len(s.area.label) > width {
			width = len(s.area.label)
		}
	}
	var lines []string
	for _, s := range sigs {
		label := fmt.Sprintf("%-*s", width, s.area.label)
		var detail string
		switch {
		case s.area.available && s.area.command != "":
			detail = fmt.Sprintf("%s · %s", s.area.command, formatRunSignal(now, s.lastRun))
		case s.area.command != "":
			detail = s.area.command + " (planned)"
		default:
			detail = "(planned)"
		}
		lines = append(lines, fmt.Sprintf("%s — %s", label, detail))
	}
	return lines
}

// staleCompletedState reports whether the working copy is sitting on a
// shipped-but-unmerged phase branch: HEAD is phase/<id>, branch-local state
// already records `completed <id>` (folded in by `dross ship` pre-merge), yet
// origin/<main> carries no such record (the PR hasn't merged). In that window
// the phase reads "done" locally while it isn't actually merged — status warns
// so the stale state isn't silently trusted. Read-only: it shells out to git
// but never mutates. Returns ("", false) on any uncertainty (not a phase
// branch, no completion record, unreadable origin, or genuinely merged) so a
// repo without a remote never produces a false warning.
func staleCompletedState(repoDir string, st *state.State, mainBranch string) (string, bool) {
	cur, err := gitTrim(repoDir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", false
	}
	phaseID, ok := strings.CutPrefix(cur, "phase/")
	if !ok || phaseID == "" {
		return "", false
	}
	if !stateRecordsCompleted(st, phaseID) {
		return "", false
	}
	// origin/<main> must lack the completion. If reading it fails we can't
	// confirm the divergence, so stay silent rather than warn on a guess.
	originState, err := gitTrim(repoDir, "show", "origin/"+mainBranch+":.dross/"+state.File)
	if err != nil {
		return "", false
	}
	if strings.Contains(originState, "completed "+phaseID) {
		return "", false // genuinely merged — not stale
	}
	return phaseID, true
}

// stateRecordsCompleted reports whether state history carries a `completed
// <id>` action — the record `dross ship` folds in when finalizing a phase.
func stateRecordsCompleted(st *state.State, phaseID string) bool {
	for _, a := range st.History {
		if strings.Contains(a.Action, "completed "+phaseID) {
			return true
		}
	}
	return false
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
