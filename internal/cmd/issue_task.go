package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/phase"
)

// `dross issue task-sync` — the board mirror at task granularity.
//
// Before it, the tracker saw a phase and a rendered checklist inside its body.
// Nothing moved when one task of eight was picked up, so a board that claimed
// to show where work was could only ever show which phase it was in. This gives
// each plan task its own issue, related to the phase's, and drives it through
// the tracker's OWN workflow field rather than a dross label — a label is
// dross talking to itself; the state field is what the tracker's columns read.
//
// It is a no-op when board sync is off, like every other `dross issue` verb, so
// the loop prompts call it unconditionally.

// taskLabel marks an issue as mirroring one plan task. The pair is in the label
// because task ids are unique only within a phase.
func taskLabel(phaseID, taskID string) string { return "dross/task:" + phaseID + "/" + taskID }

func issueTaskSync() *cobra.Command {
	var status string
	c := &cobra.Command{
		Use:   "task-sync <phase-id> [task-id]",
		Short: "Mirror a phase's plan tasks as issues, one per task",
		Long: `Create or update one board issue per plan task, related to the phase's issue.

With a task id, only that task is synced — which is what the execute loop
does at each edge, so a commit does not rewrite the other seven issues.

--status drives the tracker's own workflow field (YouTrack's State, a Jira
transition), not a dross label. A backend that cannot relate issues warns
once and continues with the label.

A no-op when board sync is off.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				return nil
			}
			phaseID := args[0]
			only := ""
			if len(args) == 2 {
				only = args[1]
			}
			return syncTasks(ctx, phaseID, only, status)
		},
	}
	c.Flags().StringVar(&status, "status", "", "lifecycle status to drive the task's state (task-in-progress | task-in-review)")
	return c
}

// syncTasks mirrors one phase's plan tasks.
func syncTasks(ctx *boardCtx, phaseID, only, status string) error {
	dir := phase.Dir(ctx.root, phaseID)
	plan, err := phase.LoadPlan(filepath.Join(dir, "plan.toml"))
	if err != nil {
		return fmt.Errorf("load plan for %s: %w", phaseID, err)
	}

	// The phase's own issue is the parent. Resolved once — not per task —
	// because every child relates to the same one, and re-resolving would be
	// a tracker round trip per task for an answer that cannot change mid-run.
	parent, ok := ctx.board.PhaseIssue(phaseID)
	if !ok {
		return fmt.Errorf("phase %s has no board issue yet — run `dross issue phase-sync %s` first", phaseID, phaseID)
	}

	linker, canLink := ctx.client.(forge.IssueLinker)
	warnedNoLink := false

	for _, t := range plan.Task {
		if only != "" && t.ID != only {
			continue
		}
		key, err := syncOneTask(ctx, phaseID, parent, t, status)
		if err != nil {
			return err
		}
		if !canLink {
			// ONCE per run, not once per task: an eight-task phase would
			// otherwise print eight identical lines, which is how a warning
			// becomes scrollback.
			if !warnedNoLink {
				fmt.Fprintf(os.Stderr, "warning: %s cannot relate issues — task issues carry %s and the phase label instead of a link\n",
					ctx.proj.Board.Provider, taskLabel(phaseID, "<task>"))
				warnedNoLink = true
			}
			continue
		}
		if err := linker.LinkIssues(parent, key); err != nil {
			// A capability gap the tracker reports at call time rather than
			// through the interface. Same floor: say it once, keep going —
			// the issues themselves are the substance, and the link is how
			// they are grouped.
			if !warnedNoLink {
				fmt.Fprintf(os.Stderr, "warning: could not relate task issues to %s (%v) — they carry the phase label instead\n", parent, err)
				warnedNoLink = true
			}
		}
	}
	return ctx.board.Save(ctx.boardPath)
}

// syncOneTask creates or updates the issue for one task and returns its key.
func syncOneTask(ctx *boardCtx, phaseID, parent string, t phase.Task, status string) (string, error) {
	title := fmt.Sprintf("%s/%s — %s", phaseID, t.ID, t.Title)
	body := renderTaskBody(phaseID, parent, t)
	labels := []string{labelMarker, phaseLabel(phaseID), taskLabel(phaseID, t.ID)}
	if status != "" {
		labels = append(labels, statusLabel(status))
	}

	key, err := resolveTaskIssue(ctx, phaseID, t.ID)
	if err != nil {
		return "", err
	}
	if key == "" {
		iss, err := ctx.client.CreateIssue(forge.IssueInput{Title: title, Body: body, Labels: labels})
		if err != nil {
			return "", wrapBoard(err)
		}
		key = iss.Key
	} else {
		patch := forge.IssuePatch{Title: &title, Body: &body, Labels: &labels}
		if _, err := ctx.client.UpdateIssue(key, patch); err != nil {
			return "", wrapBoard(err)
		}
	}
	ctx.board.SetTask(phaseID, t.ID, key)

	// The tracker's own field, not the label above. The label is dross talking
	// to itself; the state field is what the board's columns read, and moving
	// it is the whole point of mirroring a task at all.
	if status != "" {
		if err := setBoardState(ctx, key, status); err != nil {
			return "", err
		}
	}
	return key, nil
}

// resolveTaskIssue finds an existing issue for a task: the board cache first,
// then the tracker by label.
//
// Same two-step as resolvePhaseIssue, and for the same reason: board.json is a
// cache, so an entry that no longer resolves must not shadow the live issue,
// and a re-clone with no cache must still find what is already there rather
// than creating a second issue for every task.
func resolveTaskIssue(ctx *boardCtx, phaseID, taskID string) (string, error) {
	if key, ok := ctx.board.TaskIssue(phaseID, taskID); ok {
		if iss, err := ctx.client.GetIssue(key); err == nil && iss != nil && hasMarker(*iss) {
			return key, nil
		}
		fmt.Fprintf(os.Stderr, "warning: board.json points %s at %s, which no longer resolves — re-resolving from the tracker\n",
			board.TaskKey(phaseID, taskID), key)
		delete(ctx.board.Tasks, board.TaskKey(phaseID, taskID))
	}
	found, err := ctx.client.ListIssues(forge.IssueFilter{State: "all", Labels: []string{taskLabel(phaseID, taskID)}})
	if err != nil {
		return "", wrapBoard(err)
	}
	var matches []string
	for _, iss := range found {
		if hasMarker(iss) {
			matches = append(matches, iss.Key)
		}
	}
	if len(matches) == 0 {
		return "", nil
	}
	sort.Strings(matches)
	if len(matches) > 1 {
		fmt.Fprintf(os.Stderr, "warning: %d issues carry %s (%s) — updating %s and leaving the rest\n",
			len(matches), taskLabel(phaseID, taskID), strings.Join(matches, ", "), matches[0])
	}
	return matches[0], nil
}

// renderTaskBody is what a reader sees on the tracker. It names the phase issue
// so a card opened from a board column can be traced back even where the
// tracker could not express the link.
func renderTaskBody(phaseID, parent string, t phase.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task `%s` of phase `%s` (%s).\n\n", t.ID, phaseID, parent)
	if t.Title != "" {
		fmt.Fprintf(&b, "**%s**\n\n", t.Title)
	}
	if t.Description != "" {
		fmt.Fprintf(&b, "%s\n", strings.TrimSpace(t.Description))
	}
	if len(t.Files) > 0 {
		fmt.Fprintf(&b, "\nFiles: %s\n", strings.Join(t.Files, ", "))
	}
	if len(t.TestContract) > 0 {
		b.WriteString("\nTest contract:\n")
		for _, tc := range t.TestContract {
			fmt.Fprintf(&b, "- %s\n", tc)
		}
	}
	b.WriteString("\n_Mirrored by dross. Edits here do not travel back to plan.toml._\n")
	return b.String()
}

// setBoardState drives whichever workflow field the backend exposes.
//
// One place, so the phase sync and the task sync cannot come to disagree about
// how a state reaches a tracker — which is the class of bug this milestone has
// spent several phases closing.
func setBoardState(ctx *boardCtx, key, status string) error {
	switch c := ctx.client.(type) {
	case *forge.YouTrackClient:
		if err := c.SetState(key, status, ctx.proj.Board.StateMap); err != nil {
			return wrapBoard(err)
		}
	case *forge.JiraClient:
		if err := c.SetState(key, status, ctx.proj.Board.StateMap); err != nil {
			return wrapBoard(err)
		}
	}
	// Every other backend has no state field. The status label is already on
	// the issue, which is the honest floor rather than a silent no-op.
	return nil
}
