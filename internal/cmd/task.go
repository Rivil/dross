package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/phase"
)

// Task registers `dross task {next,show,status,add,remove,edit}`.
func Task() *cobra.Command {
	c := &cobra.Command{
		Use:   "task",
		Short: "Inspect and update tasks within a phase plan",
	}
	c.AddCommand(taskNext(), taskShow(), taskStatus(), taskAdd(), taskRemove())
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

// taskAdd wires `dross task add`: build a task from flags and splice it into
// plan.toml. It appends at the tail by default and only consults resolveAnchor
// when --after/--before is given — resolveAnchor errors on neither-flag, so it
// can't own the default-append path.
func taskAdd() *cobra.Command {
	var title, description, after, before string
	var wave int
	var covers, dependsOn, files []string
	c := &cobra.Command{
		Use:   "add <phase-id>",
		Short: "Append or insert a new task into a phase plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if strings.TrimSpace(title) == "" {
				return errors.New("--title is required")
			}
			plan, spec, planPath, err := loadPhasePlanAndSpec(args[0])
			if err != nil {
				return err
			}
			var anchor string
			var isBefore bool
			if after != "" || before != "" {
				if anchor, isBefore, err = resolveAnchor(after, before); err != nil {
					return err
				}
			}
			t, err := plan.AddTask(phase.NewTask{
				Title:       title,
				Files:       files,
				Covers:      covers,
				DependsOn:   dependsOn,
				Description: description,
				Wave:        wave,
			}, anchor, isBefore)
			if err != nil {
				return err
			}
			if err := saveIfValid(plan, spec, planPath); err != nil {
				return err
			}
			Printf("added %s (%s) to %s at wave %d\n", t.ID, t.Title, args[0], t.Wave)
			return nil
		},
	}
	c.Flags().StringVar(&title, "title", "", "task title (required)")
	c.Flags().StringVar(&description, "description", "", "task description")
	c.Flags().IntVar(&wave, "wave", 0, "explicit wave (default: derived from --depends-on)")
	c.Flags().StringSliceVar(&covers, "covers", nil, "criterion ids this task covers (comma-separated)")
	c.Flags().StringSliceVar(&dependsOn, "depends-on", nil, "task ids this task depends on (comma-separated)")
	c.Flags().StringSliceVar(&files, "files", nil, "files this task touches (comma-separated)")
	c.Flags().StringVar(&after, "after", "", "insert immediately after this task id")
	c.Flags().StringVar(&before, "before", "", "insert immediately before this task id")
	return c
}

// taskRemove wires `dross task remove`: delete a task, dependency-safe. Without
// --force it refuses when another task depends on the target; with --force it
// strips the removed id from every dependent so the plan stays valid.
func taskRemove() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "remove <phase-id> <task-id>",
		Short: "Remove a task from a phase plan (dependency-safe)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			plan, spec, planPath, err := loadPhasePlanAndSpec(args[0])
			if err != nil {
				return err
			}
			if err := plan.RemoveTask(args[1], force); err != nil {
				return err
			}
			if err := saveIfValid(plan, spec, planPath); err != nil {
				return err
			}
			Printf("removed %s from %s\n", args[1], args[0])
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "strip the removed id from dependents' depends_on instead of refusing")
	return c
}

// saveIfValid runs the pre-write integrity guard (phase.ValidatePlan) and writes
// plan.toml only when the plan is valid, so a rejected mutation leaves the file
// byte-unchanged. spec may be nil (skips the covers->criterion check).
func saveIfValid(plan *phase.Plan, spec *phase.Spec, path string) error {
	if err := phase.ValidatePlan(plan, spec); err != nil {
		return err
	}
	return plan.Save(path)
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

// loadPhasePlanAndSpec loads both plan.toml and spec.toml for a phase — the
// mutating verbs (add/remove/edit) need the spec to run the covers->criterion
// guard in saveIfValid.
func loadPhasePlanAndSpec(phaseID string) (*phase.Plan, *phase.Spec, string, error) {
	root, err := FindRoot()
	if err != nil {
		return nil, nil, "", err
	}
	dir := phase.Dir(root, phaseID)
	planPath := filepath.Join(dir, "plan.toml")
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		return nil, nil, "", err
	}
	spec, err := phase.LoadSpec(filepath.Join(dir, "spec.toml"))
	if err != nil {
		return nil, nil, "", err
	}
	return plan, spec, planPath, nil
}
