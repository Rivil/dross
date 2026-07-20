package phase

import (
	"fmt"
	"strconv"
	"strings"
)

// taskIDNum parses the numeric ordinal from a task id of the form "t-<n>".
// It returns 0 for any id that does not match that shape, so a malformed id
// never inflates the high-water mark.
func taskIDNum(id string) int {
	s, ok := strings.CutPrefix(id, "t-")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// NextTaskID returns the id to assign to the next task, as "t-<n>".
//
// The ordinal is one past the effective high-water mark: the larger of the
// persisted Plan.TaskSeq and the maximum ordinal among the current tasks. The
// max-of-current term backfills TaskSeq for pre-existing plans that predate the
// counter (TaskSeq == 0); once AddTask advances TaskSeq past every live id, a
// removed task's id — even the highest — is never reissued.
//
// NextTaskID is a pure query: it does not mutate the plan. AddTask is
// responsible for advancing TaskSeq when it consumes an id.
func (p *Plan) NextTaskID() string {
	hw := p.TaskSeq
	for _, t := range p.Task {
		if n := taskIDNum(t.ID); n > hw {
			hw = n
		}
	}
	return fmt.Sprintf("t-%d", hw+1)
}

// deriveWave computes the wave for a new or edited task.
//
//   - An explicit wave (> 0) wins outright.
//   - Otherwise the wave is one past the deepest wave among its depends_on
//     tasks (an unknown dep contributes nothing).
//   - With no explicit wave and no resolvable deps it defaults to wave 1.
//
// This ties the wave label to dependencies rather than to display position, so
// --after/--before never change it.
func deriveWave(explicitWave int, dependsOn []string, tasks []Task) int {
	if explicitWave > 0 {
		return explicitWave
	}
	deepest := 0
	for _, dep := range dependsOn {
		for _, t := range tasks {
			if t.ID == dep && t.Wave > deepest {
				deepest = t.Wave
			}
		}
	}
	if deepest == 0 {
		return 1
	}
	return deepest + 1
}

// ValidatePlan checks a plan for the integrity invariants the task-lifecycle
// mutators must preserve. It is a superset of `dross validate`'s plan checks:
// it keeps the covers->criterion rule in parity (identical "covers unknown
// criterion" phrasing, see internal/cmd/validate.go) and adds the duplicate-id
// and unknown-depends_on checks that validator lacks. A nil spec skips only the
// covers->criterion check, so callers without a spec still get the structural
// id/dependency guards.
//
// Every defect is reported together in one error, each naming the offending id,
// so a single call surfaces all problems at once.
func ValidatePlan(plan *Plan, spec *Spec) error {
	var problems []string

	// Duplicate task ids.
	ids := map[string]bool{}
	for _, t := range plan.Task {
		if ids[t.ID] {
			problems = append(problems, fmt.Sprintf("duplicate task id %s", t.ID))
		}
		ids[t.ID] = true
	}

	// depends_on must reference a task that exists in the plan.
	for _, t := range plan.Task {
		for _, dep := range t.DependsOn {
			if !ids[dep] {
				problems = append(problems, fmt.Sprintf("task %s depends on unknown task %s", t.ID, dep))
			}
		}
	}

	// covers must reference a criterion in the spec — parity with `dross validate`.
	if spec != nil {
		crit := map[string]bool{}
		for _, c := range spec.Criteria {
			crit[c.ID] = true
		}
		for _, t := range plan.Task {
			for _, cov := range t.Covers {
				if !crit[cov] {
					problems = append(problems, fmt.Sprintf("task %s covers unknown criterion %s", t.ID, cov))
				}
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("invalid plan: %s", strings.Join(problems, "; "))
}
