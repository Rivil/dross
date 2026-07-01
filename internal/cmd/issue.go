package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
)

// Board label vocabulary. A single marker label identifies dross-managed
// issues; one status label tracks lifecycle stage. Keeping it to two labels
// avoids cluttering the board's label list.
const (
	labelMarker = "dross"
	labelQuick  = "dross/quick"

	statusPlanning   = "planning"
	statusInProgress = "in-progress"
	statusVerifying  = "verifying"
)

func statusLabel(s string) string { return "dross/status:" + s }

// Issue registers `dross issue …` — mirroring dross planning artefacts onto
// the repo's issue-tracker board, and pulling inbound issues for triage.
func Issue() *cobra.Command {
	c := &cobra.Command{
		Use:   "issue",
		Short: "Mirror planning onto the issue board and pull inbound issues",
	}
	c.AddCommand(
		issueEnable(),
		issueDisable(),
		issueMilestoneSync(),
		issueBacklogSync(),
		issuePhaseSync(),
		issueQuick(),
		issuePull(),
		issueDismiss(),
		issueLink(),
		issueList(),
	)
	return c
}

// boardCtx bundles everything a board operation needs.
type boardCtx struct {
	client    forge.BoardClient
	board     *board.Board
	proj      *project.Project
	root      string
	boardPath string
}

// openBoard loads project + board.json + a board client, resolved SOLELY from
// the [board] config block (never [remote] — a repo can ship code to one host
// and track issues on another). When board sync is disabled it returns
// enabled=false and no error, so the workflow prompts can call `dross issue …`
// unconditionally and have it be a silent no-op for anyone who hasn't opted in.
func openBoard() (ctx *boardCtx, enabled bool, err error) {
	proj, _, err := loadProject()
	if err != nil {
		return nil, false, err
	}
	if !proj.Board.Enabled {
		return nil, false, nil
	}
	root, err := FindRoot()
	if err != nil {
		return nil, false, err
	}
	client, err := forge.NewBoard(boardConfig(proj.Board))
	if err != nil {
		return nil, false, err
	}
	bd, err := board.Load(filepath.Join(root, board.File))
	if err != nil {
		return nil, false, err
	}
	return &boardCtx{
		client:    client,
		board:     bd,
		proj:      proj,
		root:      root,
		boardPath: filepath.Join(root, board.File),
	}, true, nil
}

// boardConfig maps a [board] block onto a forge.Config. base_url is the API
// base; project is the tracker-native identifier — a "owner/repo" path for
// forge backends, the numeric/path project ref for GitLab, the short-name for
// YouTrack. For the forge backends a synthetic URL carries owner/repo to the
// client (the real host is irrelevant — every call targets base_url).
func boardConfig(b project.Board) forge.Config {
	cfg := forge.Config{
		Provider: b.Provider,
		APIBase:  b.BaseURL,
		AuthEnv:  b.AuthEnv,
		AuthUser: b.AuthUser,
		Project:  b.Project,
		BoardID:  b.GitHubProject,
		URL:      "https://board.local/" + b.Project,
	}
	if strings.ToLower(b.Provider) == "gitlab" {
		cfg.ProjectID = b.Project
	}
	return cfg
}

// wrapBoard tags operational forge errors so telemetry buckets them as
// "board" instead of generic network/other.
func wrapBoard(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("board: %w", err)
}

// --- enable / disable ---

func issueEnable() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Turn on issue-board sync for this project",
		RunE: func(_ *cobra.Command, _ []string) error {
			p, path, err := loadProject()
			if err != nil {
				return err
			}
			p.Board.Enabled = true
			if err := p.Save(path); err != nil {
				return err
			}
			Print("board sync enabled")
			switch strings.ToLower(p.Board.Provider) {
			case "forgejo", "gitea", "gitlab", "youtrack", "jira", "github":
			case "":
				Print("note: [board].provider is unset — set it to forgejo/gitea/gitlab/youtrack/jira/github")
			default:
				Printf("note: provider %q has no board backend (forgejo/gitea/gitlab/youtrack/jira/github)\n", p.Board.Provider)
			}
			if p.Board.BaseURL == "" {
				Print("note: [board].base_url is unset — needed for the board API")
			}
			if p.Board.AuthEnv == "" {
				Print("note: [board].auth_env is unset — needed for the board token")
			}
			if p.Board.Project == "" {
				Print("note: [board].project is unset — needed to scope board issues")
			}
			return nil
		},
	}
}

func issueDisable() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Turn off issue-board sync for this project",
		RunE: func(_ *cobra.Command, _ []string) error {
			p, path, err := loadProject()
			if err != nil {
				return err
			}
			p.Board.Enabled = false
			if err := p.Save(path); err != nil {
				return err
			}
			Print("board sync disabled")
			return nil
		},
	}
}

// --- milestone sync ---

func issueMilestoneSync() *cobra.Command {
	return &cobra.Command{
		Use:   "milestone-sync <version>",
		Short: "Ensure a board milestone exists for a dross milestone",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				return nil // no-op when board sync is off
			}
			id, err := ensureMilestoneLink(ctx, args[0])
			if err != nil {
				return err
			}
			if id == "" {
				return fmt.Errorf("milestone %q not found under .dross/milestones/", args[0])
			}
			if err := ctx.board.Save(ctx.boardPath); err != nil {
				return err
			}
			Printf("milestone %s -> board %s\n", args[0], id)
			return nil
		},
	}
}

// ensureMilestoneLink returns the board milestone id for a dross milestone
// version, creating the board milestone (and storing the link) if needed.
// Returns 0 (no error) when the milestone toml doesn't exist locally, so
// callers can treat "no milestone" as "skip assignment".
func ensureMilestoneLink(ctx *boardCtx, version string) (string, error) {
	if id, ok := ctx.board.MilestoneID(version); ok {
		return id, nil
	}
	m, err := milestone.Load(milestone.FilePath(ctx.root, version))
	if err != nil {
		return "", nil // not found locally — nothing to link
	}
	title := m.Milestone.Title
	if title == "" {
		title = version
	}
	desc := strings.Join(m.Scope.SuccessCriteria, "\n")
	var id string
	switch c := ctx.client.(type) {
	case *forge.YouTrackClient:
		// YouTrack maps a milestone to an entity per [board].milestone_mode
		// (version bundle / agile board / epic), not a forge-style milestone.
		id, err = c.EnsureMilestoneEntity(ctx.proj.Board.MilestoneMode, version, desc)
	case *forge.JiraClient:
		// Jira maps a milestone to a project VERSION (string id via the concrete
		// path). The returned id is numeric, so the phase-sync int-milestone path
		// still attaches it (as a Fix Version) downstream.
		id, err = c.EnsureMilestoneEntity(ctx.proj.Board.MilestoneMode, version, desc)
	default:
		id, err = ctx.client.EnsureMilestone(version, milestoneBody(title, desc))
	}
	if err != nil {
		return "", wrapBoard(err)
	}
	if id == "" {
		return "", nil // backend ensured no entity (e.g. youtrack mode dispatch lands later)
	}
	ctx.board.SetMilestone(version, id)
	return id, nil
}

func milestoneBody(title, criteria string) string {
	if criteria == "" {
		return title
	}
	return title + "\n\nSuccess criteria:\n" + criteria
}

// --- backlog sync ---

func issueBacklogSync() *cobra.Command {
	return &cobra.Command{
		Use:   "backlog-sync <version>",
		Short: "Sync the milestone backlog (unscaffolded slugs + someday ideas) to the board",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				return nil
			}
			return syncBacklog(ctx, args[0])
		},
	}
}

// backlogItem is one milestone-backlog entry to mirror onto the board.
type backlogItem struct{ key, title, body string }

// syncBacklog mirrors a milestone's backlog — its unscaffolded roadmap phase
// slugs and unrouted `someday` deferred ideas — onto the board as Open issues
// attached to the milestone entity, recorded in board.json's backlog map.
// Idempotent: re-running updates the same items by their readable-id link.
func syncBacklog(ctx *boardCtx, version string) error {
	m, err := milestone.Load(milestone.FilePath(ctx.root, version))
	if err != nil {
		return fmt.Errorf("load milestone %q: %w", version, err)
	}

	// Ensure the milestone entity the backlog attaches to (version value / epic
	// / agile board). Version mode tags each item's Fix versions with it.
	entityID, err := ensureMilestoneLink(ctx, version)
	if err != nil {
		return err
	}
	// Per milestone_mode, attach each backlog item to the entity: version mode
	// sets the item's Fix versions to the bundle value; epic mode links it as a
	// subtask of the Epic; agile boards are query/project-based, so an item
	// created in the project already appears on the board (no per-item call).
	mode := strings.ToLower(ctx.proj.Board.MilestoneMode)
	fixVersion := ""
	if mode == "" || mode == "version" {
		fixVersion = entityID
	}

	var items []backlogItem
	// Unscaffolded roadmap slugs: in milestone.phases with no phase directory.
	for _, slug := range m.Phases {
		if _, err := os.Stat(phase.Dir(ctx.root, slug)); err == nil {
			continue // scaffolded — tracked by its own phase issue
		}
		items = append(items, backlogItem{
			key:   "slug:" + slug,
			title: "[backlog] " + slug,
			body:  fmt.Sprintf("Roadmap phase `%s` in milestone %s — not yet scaffolded.\n\n_Tracked by dross._", slug, version),
		})
	}
	// Unrouted `someday` deferred ideas (no target, not dismissed).
	deferredItems, err := collectDeferred(ctx.root)
	if err != nil {
		return err
	}
	for _, d := range deferredItems {
		if d.Target != "" || d.Dismissed {
			continue
		}
		items = append(items, backlogItem{
			key:   fmt.Sprintf("someday:%s#%d", d.Source, d.Index),
			title: "[someday] " + d.Text,
			body:  fmt.Sprintf("Someday idea (from phase `%s`): %s\n\n_Tracked by dross._", d.Source, d.Text),
		})
	}

	created, updated := 0, 0
	for _, it := range items {
		if key, ok := ctx.board.BacklogID(it.key); ok {
			title, body := it.title, it.body
			if _, err := ctx.client.UpdateIssue(key, forge.IssuePatch{Title: &title, Body: &body}); err != nil {
				return wrapBoard(err)
			}
			updated++
			continue
		}
		var iss *forge.Issue
		if yt, ok := ctx.client.(*forge.YouTrackClient); ok {
			iss, err = yt.CreateBacklogItem(it.title, it.body, fixVersion)
			if err == nil && mode == "epic" && entityID != "" {
				// Attach to the Epic entity as a subtask.
				err = yt.LinkSubtask(entityID, iss.Key)
			}
		} else {
			ms, _ := strconv.Atoi(entityID)
			iss, err = ctx.client.CreateIssue(forge.IssueInput{
				Title:     it.title,
				Body:      it.body,
				Labels:    []string{labelMarker},
				Milestone: ms,
			})
		}
		if err != nil {
			return wrapBoard(err)
		}
		ctx.board.SetBacklog(it.key, iss.Key)
		created++
	}
	if err := ctx.board.Save(ctx.boardPath); err != nil {
		return err
	}
	Printf("backlog %s -> %d created, %d updated\n", version, created, updated)
	return nil
}

// --- phase sync ---

func issuePhaseSync() *cobra.Command {
	var status string
	var doClose bool
	c := &cobra.Command{
		Use:   "phase-sync <phase-id>",
		Short: "Create or update the board issue for a phase (idempotent)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				return nil
			}
			return syncPhase(ctx, args[0], status, doClose)
		},
	}
	c.Flags().StringVar(&status, "status", "", "lifecycle status label (planning|in-progress|verifying); derived from the plan if unset")
	c.Flags().BoolVar(&doClose, "close", false, "close the issue (use on ship)")
	return c
}

func syncPhase(ctx *boardCtx, phaseID, status string, doClose bool) error {
	dir := phase.Dir(ctx.root, phaseID)
	spec, err := phase.LoadSpec(filepath.Join(dir, "spec.toml"))
	if err != nil {
		return fmt.Errorf("load spec for %s: %w", phaseID, err)
	}
	// plan.toml may not exist yet (sync at spec time) — tolerate it.
	plan, _ := phase.LoadPlan(filepath.Join(dir, "plan.toml"))

	if status == "" {
		status = derivePhaseStatus(plan)
	}

	title := fmt.Sprintf("%s — %s", phaseID, spec.Phase.Title)
	body := renderPhaseBody(phaseID, spec, plan)
	labels := []string{labelMarker, statusLabel(status)}

	// Assign to the milestone if the phase declares one and it's syncable.
	// IssueInput.Milestone is the forge int id; the board stores it as a string.
	milestoneID := 0
	if spec.Phase.Milestone != "" {
		id, err := ensureMilestoneLink(ctx, spec.Phase.Milestone)
		if err != nil {
			return err
		}
		milestoneID, _ = strconv.Atoi(id) // "" / non-numeric (youtrack entity) → 0, unassigned
	}

	key, linked := ctx.board.PhaseIssue(phaseID)
	if !linked {
		iss, err := ctx.client.CreateIssue(forge.IssueInput{
			Title:     title,
			Body:      body,
			Labels:    labels,
			Milestone: milestoneID,
		})
		if err != nil {
			return wrapBoard(err)
		}
		ctx.board.SetPhase(phaseID, iss.Key)
		key = iss.Key
	} else {
		patch := forge.IssuePatch{Title: &title, Body: &body, Labels: &labels}
		if milestoneID > 0 {
			patch.Milestone = &milestoneID
		}
		if doClose {
			closed := "closed"
			patch.State = &closed
		}
		if _, err := ctx.client.UpdateIssue(key, patch); err != nil {
			return wrapBoard(err)
		}
	}
	// YouTrack tracks lifecycle on the State custom field (not a status label),
	// mapped via the default map overridden by [board].state_map. An unmapped
	// state warns and skips inside SetState without failing the sync.
	if yt, ok := ctx.client.(*forge.YouTrackClient); ok && status != "" {
		if err := yt.SetState(key, status, ctx.proj.Board.StateMap); err != nil {
			return wrapBoard(err)
		}
	}
	// Jira tracks lifecycle by moving the issue through a workflow transition,
	// mapped via the default map overridden by [board].state_map. An unmapped
	// state (or a target with no available transition) warns and skips inside
	// SetState without failing the sync.
	if jr, ok := ctx.client.(*forge.JiraClient); ok && status != "" {
		if err := jr.SetState(key, status, ctx.proj.Board.StateMap); err != nil {
			return wrapBoard(err)
		}
	}
	// Close-on-create edge: created above then asked to close.
	if doClose && !linked {
		if err := ctx.client.CloseIssue(key); err != nil {
			return wrapBoard(err)
		}
	}
	if err := ctx.board.Save(ctx.boardPath); err != nil {
		return err
	}

	state := status
	if doClose {
		state = "closed"
	}
	Printf("phase %s -> board %s (%s)\n", phaseID, key, state)
	return nil
}

// derivePhaseStatus maps plan progress onto a lifecycle label.
func derivePhaseStatus(plan *phase.Plan) string {
	if plan == nil || len(plan.Task) == 0 {
		return statusPlanning
	}
	_, inProgress, done, failed := plan.Summary()
	if done == 0 && inProgress == 0 && failed == 0 {
		return statusPlanning
	}
	return statusInProgress
}

// renderPhaseBody builds the issue body: a header, then a task checklist
// mirroring plan.toml task statuses, then a "managed by dross" footer.
func renderPhaseBody(phaseID string, spec *phase.Spec, plan *phase.Plan) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("**Phase:** `%s`", phaseID))
	if spec.Phase.Milestone != "" {
		b.WriteString(fmt.Sprintf("  ·  **Milestone:** %s", spec.Phase.Milestone))
	}
	b.WriteString("\n")

	if len(spec.Criteria) > 0 {
		b.WriteString("\n### Acceptance criteria\n")
		for _, cr := range spec.Criteria {
			b.WriteString(fmt.Sprintf("- %s\n", cr.Text))
		}
	}

	if plan != nil && len(plan.Task) > 0 {
		b.WriteString("\n### Tasks\n")
		for _, t := range plan.Task {
			box := " "
			if t.Status == phase.StatusDone {
				box = "x"
			}
			b.WriteString(fmt.Sprintf("- [%s] %s — %s\n", box, t.ID, t.Title))
		}
	}

	b.WriteString("\n---\n_Tracked by dross. Edit the phase artefacts under ")
	b.WriteString(fmt.Sprintf("`.dross/phases/%s/`, not this issue body._\n", phaseID))
	return b.String()
}

// --- quick ---

func issueQuick() *cobra.Command {
	var doClose bool
	c := &cobra.Command{
		Use:   "quick <ref> [title]",
		Short: "Open (or --close) a standalone issue for a quick task",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				return nil
			}
			ref := args[0]

			if doClose {
				key, ok := ctx.board.QuickIssue(ref)
				if !ok {
					return fmt.Errorf("no board issue linked to quick ref %q", ref)
				}
				if err := ctx.client.CloseIssue(key); err != nil {
					return wrapBoard(err)
				}
				Printf("quick %s -> board %s (closed)\n", ref, key)
				return nil
			}

			if len(args) < 2 {
				return fmt.Errorf("a title is required to open a quick issue")
			}
			iss, err := ctx.client.CreateIssue(forge.IssueInput{
				Title:  args[1],
				Body:   fmt.Sprintf("Quick task `%s`.\n\n_Tracked by dross._", ref),
				Labels: []string{labelMarker, labelQuick},
			})
			if err != nil {
				return wrapBoard(err)
			}
			ctx.board.SetQuick(ref, iss.Key)
			if err := ctx.board.Save(ctx.boardPath); err != nil {
				return err
			}
			Printf("quick %s -> board %s\n", ref, iss.Key)
			return nil
		},
	}
	c.Flags().BoolVar(&doClose, "close", false, "close the quick issue linked to <ref>")
	return c
}

// --- pull (inbound triage feed) ---

func issuePull() *cobra.Command {
	var labels, state string
	var asJSON, mark bool
	c := &cobra.Command{
		Use:   "pull",
		Short: "List open board issues not yet linked to dross work (inbound triage)",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				if asJSON {
					Print("[]")
				}
				return nil
			}
			filter := forge.IssueFilter{State: state}
			if labels != "" {
				filter.Labels = splitCSV(labels)
			}
			issues, err := ctx.client.ListIssues(filter)
			if err != nil {
				return wrapBoard(err)
			}
			var inbound []forge.Issue
			for _, iss := range issues {
				if ctx.board.IsLinked(iss.Key) || ctx.board.IsDismissed(iss.Key) {
					continue
				}
				inbound = append(inbound, iss)
			}

			// Read-only by default so /dross-status can poll without
			// mutating .dross. --mark stamps last_pull (used by /dross-inbox).
			if mark {
				ctx.board.MarkPulled()
				if err := ctx.board.Save(ctx.boardPath); err != nil {
					return err
				}
			}

			if asJSON {
				out, err := json.Marshal(inbound)
				if err != nil {
					return err
				}
				Print(string(out))
				return nil
			}
			if len(inbound) == 0 {
				Print("no new issues on the board")
				return nil
			}
			Printf("%d new issue(s) to triage:\n", len(inbound))
			for _, iss := range inbound {
				labelStr := ""
				if len(iss.Labels) > 0 {
					labelStr = "  [" + strings.Join(iss.Labels, ", ") + "]"
				}
				Printf("  %s %s%s\n", iss.Key, iss.Title, labelStr)
			}
			return nil
		},
	}
	c.Flags().StringVar(&labels, "labels", "", "only issues with these labels (csv, e.g. bug,enhancement)")
	c.Flags().StringVar(&state, "state", "open", "issue state: open|closed|all")
	c.Flags().BoolVar(&asJSON, "json", false, "emit a JSON array (for prompt consumption)")
	c.Flags().BoolVar(&mark, "mark", false, "record the pull time in board.json (otherwise read-only)")
	return c
}

// --- dismiss ---

func issueDismiss() *cobra.Command {
	return &cobra.Command{
		Use:   "dismiss <issue-id>",
		Short: "Stop an inbound issue from resurfacing in triage",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			root, err := FindRoot()
			if err != nil {
				return err
			}
			path := filepath.Join(root, board.File)
			bd, err := board.Load(path)
			if err != nil {
				return err
			}
			bd.Dismiss(id)
			if err := bd.Save(path); err != nil {
				return err
			}
			Printf("dismissed %s\n", id)
			return nil
		},
	}
}

// --- link (adopt an existing board issue as a phase's tracking issue) ---

func issueLink() *cobra.Command {
	return &cobra.Command{
		Use:   "link <phase-id> <issue-id>",
		Short: "Adopt an existing board issue as a phase's tracking issue",
		Long: "Used by /dross-inbox triage: when an inbound bug/feature issue " +
			"becomes a dross phase, link it so the next `phase-sync` updates that " +
			"issue in place instead of opening a duplicate.",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[1]
			root, err := FindRoot()
			if err != nil {
				return err
			}
			path := filepath.Join(root, board.File)
			bd, err := board.Load(path)
			if err != nil {
				return err
			}
			bd.SetPhase(args[0], id)
			if err := bd.Save(path); err != nil {
				return err
			}
			Printf("linked phase %s -> issue %s\n", args[0], id)
			return nil
		},
	}
}

// --- list (local link introspection) ---

func issueList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show the current artefact<->issue links from board.json",
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			bd, err := board.Load(filepath.Join(root, board.File))
			if err != nil {
				return err
			}
			if len(bd.Milestones) == 0 && len(bd.Phases) == 0 && len(bd.Quicks) == 0 {
				Print("(no board links yet)")
				return nil
			}
			for v, id := range bd.Milestones {
				Printf("milestone %s -> board %s\n", v, id)
			}
			for p, n := range bd.Phases {
				Printf("phase %s -> issue %s\n", p, n)
			}
			for ref, n := range bd.Quicks {
				Printf("quick %s -> issue %s\n", ref, n)
			}
			if len(bd.Dismissed) > 0 {
				Printf("dismissed: %v\n", bd.Dismissed)
			}
			return nil
		},
	}
}
