package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/phase"
)

// Task registers `dross task {next,show,status}`.
func Task() *cobra.Command {
	c := &cobra.Command{
		Use:   "task",
		Short: "Inspect and update tasks within a phase plan",
	}
	c.AddCommand(taskNext(), taskShow(), taskStatus())
	return c
}

// taskNext prints the id of the next runnable task, or nothing.
// Designed for shell use: `if id=$(dross task next $PHASE); then ... fi`
func taskNext() *cobra.Command {
	return &cobra.Command{
		Use:   "next <phase-id>",
		Short: "Print the id of the next runnable task (or nothing if none)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			plan, _, err := loadPhasePlan(args[0])
			if err != nil {
				return err
			}
			next := plan.NextRunnable()
			if next == nil {
				return nil // empty stdout, exit 0
			}
			Print(next.ID)
			return nil
		},
	}
}

func taskShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show <phase-id> <task-id>",
		Short: "Print one task's record from plan.toml",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			plan, _, err := loadPhasePlan(args[0])
			if err != nil {
				return err
			}
			t := plan.FindTask(args[1])
			if t == nil {
				return fmt.Errorf("task not found: %s", args[1])
			}
			Printf("id:           %s\n", t.ID)
			Printf("title:        %s\n", t.Title)
			Printf("wave:         %d\n", t.Wave)
			Printf("status:       %s\n", orPending(t.Status))
			Printf("files:        %s\n", strings.Join(t.Files, ", "))
			if len(t.Covers) > 0 {
				Printf("covers:       %s\n", strings.Join(t.Covers, ", "))
			}
			if len(t.DependsOn) > 0 {
				Printf("depends_on:   %s\n", strings.Join(t.DependsOn, ", "))
			}
			if len(t.TestContract) > 0 {
				Print("test_contract:")
				for _, line := range t.TestContract {
					Printf("  - %s\n", line)
				}
			}
			if t.Description != "" {
				Print("description:")
				for _, line := range strings.Split(strings.TrimRight(t.Description, "\n"), "\n") {
					Printf("  %s\n", line)
				}
			}
			return nil
		},
	}
}

func taskStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status <phase-id> <task-id> <pending|in_progress|done|failed>",
		Short: "Set a task's status in plan.toml",
		Args:  cobra.ExactArgs(3),
		RunE: func(_ *cobra.Command, args []string) error {
			status := args[2]
			switch status {
			case phase.StatusPending, phase.StatusInProgress, phase.StatusDone, phase.StatusFailed:
			default:
				return fmt.Errorf("invalid status: %s (want pending|in_progress|done|failed)", status)
			}

			plan, planPath, err := loadPhasePlan(args[0])
			if err != nil {
				return err
			}
			if !plan.SetTaskStatus(args[1], status) {
				return fmt.Errorf("task not found: %s", args[1])
			}
			if err := plan.Save(planPath); err != nil {
				return err
			}
			Printf("%s/%s -> %s\n", args[0], args[1], status)
			return nil
		},
	}
}

func orPending(s string) string {
	if s == "" {
		return phase.StatusPending
	}
	return s
}

// loadPhasePlan reads plan.toml for a phase relative to the current .dross root.
func loadPhasePlan(phaseID string) (*phase.Plan, string, error) {
	root, err := FindRoot()
	if err != nil {
		return nil, "", err
	}
	planPath := filepath.Join(phase.Dir(root, phaseID), "plan.toml")
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		return nil, "", err
	}
	return plan, planPath, nil
}
