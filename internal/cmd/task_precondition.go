package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/localstore"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/precond"
)

// Exit statuses a precondition gate refuses with, so a prompt can tell the two
// remedies apart without parsing prose: a refusal is fixed in the plan or the
// tree, a consent problem only by a human running `dross trust`.
const (
	// exitPreconditionRefused: a precondition is malformed, or ran and failed.
	exitPreconditionRefused = 3
	// exitPreconditionConsent: the phase's precondition grant is missing or
	// stale, so nothing ran.
	exitPreconditionConsent = 4
)

// runTaskPreconditions is the one gate behind `task status … in_progress` and
// `task check`: it judges the task's preconditions' shape, then the phase's
// consent, and only then runs them, writing the verdict to the command's
// stderr. It returns the set line the consent was checked against, so a caller
// that saves afterwards can tell whether the set moved while they ran.
//
// A task with no preconditions passes without touching consent or spawning.
// There is no way to skip one: a wrong precondition is fixed with
// `dross task edit`, where the change is visible in the plan.
func runTaskPreconditions(c *cobra.Command, root, phaseID string, plan *phase.Plan, task *phase.Task) (string, error) {
	if len(task.Preconditions) == 0 {
		return "", nil
	}
	if problems := taskPreconditionProblems(plan, task); len(problems) > 0 {
		return "", &ExitCodeError{Code: exitPreconditionRefused, Err: fmt.Errorf(
			"%s/%s has malformed preconditions, so none ran — fix them with `dross task edit %s %s --precondition …`:\n  %s",
			phaseID, task.ID, phaseID, task.ID, strings.Join(problems, "\n  "))}
	}
	repoDir := filepath.Dir(root)
	line := phase.PreconditionSetLine(plan)
	state, cerr := consent.PreconditionsConsented(localstore.GrantStore(root), repoDir, phaseID, line)
	if cerr != nil {
		return "", &ExitCodeError{Code: exitPreconditionConsent, Err: consent.PreconditionsRefusal(phaseID, state, cerr)}
	}
	ctx := c.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	v := precond.Evaluate(ctx, repoDir, task.Preconditions)
	v.Render(c.ErrOrStderr())
	if !v.OK() {
		return "", &ExitCodeError{Code: exitPreconditionRefused, Err: fmt.Errorf(
			"%s/%s did not start: a precondition failed (above)", phaseID, task.ID)}
	}
	return line, nil
}

// taskPreconditionProblems is every shape defect and unknown key in one task's
// preconditions, in PreconditionProblems' form.
func taskPreconditionProblems(plan *phase.Plan, task *phase.Task) []string {
	problems := phase.PreconditionProblems(&phase.Plan{Task: []phase.Task{*task}})
	for _, u := range plan.UnknownPreconditionKeys {
		if u.Task == task.ID {
			problems = append(problems, u.String())
		}
	}
	return problems
}

// beforePreconditionSave runs between a passing precondition run and the
// re-load that precedes the save: the seam the stale-save test uses to change
// plan.toml while the preconditions "ran".
var beforePreconditionSave = func() {}

// reloadAfterPreconditions re-reads the plan once the preconditions passed and
// refuses if what they were judged against moved meanwhile: another task went
// in_progress, this task's status changed, or the precondition set changed.
// Saving the plan loaded before the run would silently undo any such write.
func reloadAfterPreconditions(planPath, phaseID, taskID, wasStatus, line string) (*phase.Plan, error) {
	beforePreconditionSave()
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		return nil, err
	}
	t := plan.FindTask(taskID)
	if t == nil {
		return nil, fmt.Errorf("%s/%s was removed while its preconditions ran — nothing was started", phaseID, taskID)
	}
	if t.Status != wasStatus {
		return nil, fmt.Errorf("%s/%s changed to %s while its preconditions ran — nothing was started; re-run to start it",
			phaseID, taskID, orPending(t.Status))
	}
	if err := refuseSecondInProgress(plan, phaseID, taskID); err != nil {
		return nil, fmt.Errorf("while %s's preconditions ran: %w", taskID, err)
	}
	if phase.PreconditionSetLine(plan) != line {
		return nil, fmt.Errorf("%s's preconditions changed while they ran — nothing was started; re-run to judge the new set", phaseID)
	}
	return plan, nil
}
