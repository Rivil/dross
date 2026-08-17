package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// Task registers `dross task {next,list,show,status,add,remove,edit,move}`.
func Task() *cobra.Command {
	c := &cobra.Command{
		Use:   "task",
		Short: "Inspect and update tasks within a phase plan",
	}
	c.AddCommand(taskNext(), taskList(), taskShow(), taskStatus(), taskAdd(), taskRemove(), taskEdit(), taskMove())
	return c
}

// taskListRow is the wire shape of `task list --json`: the four columns the
// table prints, with the same orPending normalisation applied, so the human and
// machine views can never disagree about a task's status.
type taskListRow struct {
	ID     string `json:"id"`
	Wave   int    `json:"wave"`
	Status string `json:"status"`
	Title  string `json:"title"`
}

// taskList prints every task in a phase plan so the plan is readable without
// opening plan.toml. The phase-id argument is optional and defaults to
// state.current_phase (locked task_list_output).
func taskList() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list [phase-id]",
		Short: "List every task in a phase plan (id, wave, status, title)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			phaseID, err := resolveTaskPhaseID(args)
			if err != nil {
				return err
			}
			plan, _, err := loadPhasePlan(phaseID)
			if err != nil {
				return err
			}
			rows := make([]taskListRow, 0, len(plan.Task))
			for _, t := range plan.Task {
				rows = append(rows, taskListRow{ID: t.ID, Wave: t.Wave, Status: orPending(t.Status), Title: t.Title})
			}

			if asJSON {
				out, err := json.Marshal(rows)
				if err != nil {
					return err
				}
				Print(string(out))
				return nil
			}
			if len(rows) == 0 {
				Print("(no tasks)")
				return nil
			}
			Printf("%-8s %4s %-12s %s\n", "ID", "WAVE", "STATUS", "TITLE")
			for _, r := range rows {
				Printf("%-8s %4d %-12s %s\n", r.ID, r.Wave, r.Status, r.Title)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "emit the task array as JSON (for prompt consumption)")
	return c
}

// resolveTaskPhaseID resolves an optional phase-id argument against
// state.current_phase. An empty id is an error, never a path — otherwise the
// fallback would silently try to load `.dross/phases//plan.toml`.
func resolveTaskPhaseID(args []string) (string, error) {
	if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
		return args[0], nil
	}
	root, err := FindRoot()
	if err != nil {
		return "", err
	}
	s, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		return "", err
	}
	if s.CurrentPhase == "" {
		return "", errors.New("no phase id given and state has no current_phase")
	}
	return s.CurrentPhase, nil
}

// taskNext prints the id of the next runnable task, or nothing.
// Designed for shell use: `if id=$(dross task next $PHASE); then ... fi`
func taskNext() *cobra.Command {
	return &cobra.Command{
		Use:   "next <phase-id>",
		Short: "Print the id of the next runnable task (or nothing if none)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			// A loop-step boundary: refusing here stops an execute run before it
			// reaches the step that runs the repo's tests.
			if err := requireExecConsent(); err != nil {
				return err
			}
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
	var asJSON bool
	c := &cobra.Command{
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
			if asJSON {
				// Marshal a copy with Status normalized the same way the text
				// path renders it. Status is toml:"status,omitempty", so json
				// mirrors the omitempty and a bare marshal would drop the very
				// key the aligned rendering always prints as "pending".
				rec := *t
				rec.Status = orPending(rec.Status)
				return emitJSON(rec)
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
	c.Flags().BoolVar(&asJSON, "json", false, jsonFlagUsage)
	return c
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
			// Only in_progress. `done` and `failed` are post-hoc records of work
			// that already happened — gating them would leave a half-run phase
			// unrecordable, which is a worse state than the one being prevented.
			if status == phase.StatusInProgress {
				if err := requireExecConsent(); err != nil {
					return err
				}
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
	var covers, dependsOn, files, testContract []string
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
				Title:        title,
				Files:        files,
				Covers:       covers,
				DependsOn:    dependsOn,
				Description:  description,
				TestContract: testContract,
				Wave:         wave,
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
	// StringArray (not StringSlice): a test contract entry is one prose
	// statement — "if X breaks, TestY fails" — and those routinely contain
	// commas. StringSlice would split one statement into two, quietly turning a
	// contract into nonsense nobody reads closely enough to catch. Same reason
	// --landmark is a StringArray (changes.go).
	c.Flags().StringArrayVar(&testContract, "test-contract", nil,
		`one "if X breaks, TestY fails" statement (repeatable; not split on commas)`)
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

// taskEdit wires `dross task edit`: a partial update where only the flags
// actually passed change the task, all other fields preserved. It deliberately
// exposes no --status flag — `dross task status` stays the sole status owner.
func taskEdit() *cobra.Command {
	var title, description string
	var wave int
	var covers, dependsOn, testContract, addTestContract []string
	c := &cobra.Command{
		Use:   "edit <phase-id> <task-id>",
		Short: "Update an existing task's fields (partial; status not editable)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Replace and append disagree about what the result should be, and
			// either way of resolving the pair drops something the user asked
			// for. Refuse instead of picking.
			if cmd.Flags().Changed("test-contract") && cmd.Flags().Changed("add-test-contract") {
				return errors.New("--test-contract replaces the whole contract and --add-test-contract appends to it; pass one, not both")
			}
			plan, spec, planPath, err := loadPhasePlanAndSpec(args[0])
			if err != nil {
				return err
			}
			// Only flags the user actually set become non-nil, so unset flags
			// leave their fields untouched (partial update).
			var e phase.TaskEdit
			if cmd.Flags().Changed("title") {
				e.Title = &title
			}
			if cmd.Flags().Changed("description") {
				e.Description = &description
			}
			if cmd.Flags().Changed("test-contract") {
				e.TestContract = &testContract
			}
			if cmd.Flags().Changed("add-test-contract") {
				// Composed here rather than in TaskEdit, which stays a pure
				// replace struct: the existing entries come first, so appending
				// never reorders a contract someone already reviewed.
				cur := plan.FindTask(args[1])
				if cur == nil {
					return fmt.Errorf("task not found: %s", args[1])
				}
				merged := append(slices.Clone(cur.TestContract), addTestContract...)
				e.TestContract = &merged
			}
			if cmd.Flags().Changed("covers") {
				e.Covers = &covers
			}
			if cmd.Flags().Changed("depends-on") {
				e.DependsOn = &dependsOn
			}
			if cmd.Flags().Changed("wave") {
				e.Wave = &wave
			}
			if err := plan.EditTask(args[1], e); err != nil {
				return err
			}
			if err := saveIfValid(plan, spec, planPath); err != nil {
				return err
			}
			Printf("edited %s in %s\n", args[1], args[0])
			return nil
		},
	}
	c.Flags().StringVar(&title, "title", "", "new task title")
	c.Flags().StringVar(&description, "description", "", "new task description")
	c.Flags().IntVar(&wave, "wave", 0, "new wave")
	c.Flags().StringSliceVar(&covers, "covers", nil, "replace covered criterion ids (comma-separated)")
	c.Flags().StringSliceVar(&dependsOn, "depends-on", nil, "replace depends_on task ids (comma-separated)")
	// StringArray for both, for the reason given on `task add --test-contract`:
	// a contract statement is prose and must not be split on its commas.
	c.Flags().StringArrayVar(&testContract, "test-contract", nil,
		"replace the whole test contract (repeatable; not split on commas)")
	c.Flags().StringArrayVar(&addTestContract, "add-test-contract", nil,
		"append one statement to the existing test contract (repeatable; not split on commas)")
	return c
}

// taskMove wires `dross task move`: reposition an existing task immediately
// before/after an anchor task. Flag validation via resolveAnchor (exactly one
// of --after/--before required), mutation via Plan.MoveTask (which owns every
// guard: dependency order, frozen history, wave adoption), persistence via
// saveIfValid — a rejected move errors before Save, so plan.toml stays
// byte-unchanged.
func taskMove() *cobra.Command {
	var after, before string
	c := &cobra.Command{
		Use:   "move <phase-id> <task-id>",
		Short: "Reposition a task relative to another (--after/--before)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			anchor, isBefore, err := resolveAnchor(after, before)
			if err != nil {
				return err
			}
			plan, spec, planPath, err := loadPhasePlanAndSpec(args[0])
			if err != nil {
				return err
			}
			if err := plan.MoveTask(args[1], anchor, isBefore); err != nil {
				return err
			}
			if err := saveIfValid(plan, spec, planPath); err != nil {
				return err
			}
			rel := "after"
			if isBefore {
				rel = "before"
			}
			Printf("moved %s %s %s in %s (wave %d)\n", args[1], rel, anchor, args[0], plan.FindTask(args[1]).Wave)
			return nil
		},
	}
	c.Flags().StringVar(&after, "after", "", "move immediately after this task id")
	c.Flags().StringVar(&before, "before", "", "move immediately before this task id")
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
