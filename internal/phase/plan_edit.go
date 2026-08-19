package phase

import (
	"fmt"
	"slices"
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

// NewTask carries the caller-supplied fields for AddTask. The id and wave are
// assigned by AddTask (id from the high-water counter, wave via deriveWave), so
// they are not part of the input. A zero Wave means "derive from depends_on".
type NewTask struct {
	Title        string
	Files        []string
	Covers       []string
	DependsOn    []string
	Description  string
	TestContract []string
	Wave         int
}

// AddTask builds a new task from nt, assigning it the next high-water id and a
// derived wave, then inserts it into the plan. When anchor is non-empty the
// task lands immediately before/after that task (before=true → before);
// an empty anchor appends at the tail. Placement never renumbers existing
// tasks, and the high-water mark (TaskSeq) advances past the id just consumed
// so a later remove-then-add can't reuse it. Returns the created task, or an
// error (mutating nothing) when a non-empty anchor is not found.
func (p *Plan) AddTask(nt NewTask, anchor string, before bool) (*Task, error) {
	pos := len(p.Task)
	if anchor != "" {
		i := slices.IndexFunc(p.Task, func(t Task) bool { return t.ID == anchor })
		if i < 0 {
			return nil, fmt.Errorf("anchor task not found: %s", anchor)
		}
		pos = i + 1
		if before {
			pos = i
		}
	}
	id := p.NextTaskID()
	t := Task{
		ID:           id,
		Wave:         deriveWave(nt.Wave, nt.DependsOn, p.Task),
		Title:        nt.Title,
		Files:        nt.Files,
		Description:  nt.Description,
		Covers:       nt.Covers,
		DependsOn:    nt.DependsOn,
		TestContract: nt.TestContract,
		Status:       StatusPending,
	}
	p.Task = slices.Insert(p.Task, pos, t)
	p.TaskSeq = taskIDNum(id) // advance high-water past the consumed id
	return &p.Task[pos], nil
}

// RemoveTask deletes the task with the given id. It is dependency-safe: when
// another task lists id in its depends_on it refuses (mutating nothing) unless
// force is set, in which case id is also stripped from every dependent's
// depends_on so no dangling dependency is left. Waves are never reflowed and
// TaskSeq is left untouched, so the freed id is never reissued. Errors (mutating
// nothing) if id is not found, or if it is depended on and force is false.
func (p *Plan) RemoveTask(id string, force bool) error {
	i := slices.IndexFunc(p.Task, func(t Task) bool { return t.ID == id })
	if i < 0 {
		return fmt.Errorf("task not found: %s", id)
	}
	var dependents []string
	for _, t := range p.Task {
		if t.ID != id && slices.Contains(t.DependsOn, id) {
			dependents = append(dependents, t.ID)
		}
	}
	if len(dependents) > 0 && !force {
		return fmt.Errorf("cannot remove %s: depended on by %s (use --force to strip)", id, strings.Join(dependents, ", "))
	}
	// Backfill the high-water from the current ids BEFORE deleting — placed
	// after the refusal check so a refused remove still mutates nothing. This
	// captures the removed task's ordinal even on a plan that predates the
	// counter (TaskSeq unset), so a freed HIGHEST id is never reissued.
	for _, t := range p.Task {
		if n := taskIDNum(t.ID); n > p.TaskSeq {
			p.TaskSeq = n
		}
	}
	p.Task = slices.Delete(p.Task, i, i+1)
	if force {
		for j := range p.Task {
			p.Task[j].DependsOn = slices.DeleteFunc(p.Task[j].DependsOn, func(d string) bool { return d == id })
		}
	}
	return nil
}

// MoveTask repositions the task with the given id immediately before or after
// the anchor task (before=true → before), preserving the relative order of
// every other task. Position is the only thing a move changes about the plan's
// shape: ids and TaskSeq are never touched, so history/telemetry references
// stay valid.
//
// Wave semantics (locked move_wave_semantics): the mover adopts the anchor's
// wave, and the waves of its PENDING dependents reflow transitively so every
// dependent stays in a wave strictly greater than its dependency's. Waves of
// done/in_progress tasks are frozen — execution history is never rewritten.
//
// A move is rejected — mutating nothing — when it would:
//   - place the mover before one of its dependencies, or after a dependent;
//   - give the mover a wave <= its deepest dependency's wave;
//   - move a non-pending task (locked move_execution_guard);
//   - land the mover before a done/in_progress task.
//
// Unknown task/anchor ids and anchor==self error likewise without mutating.
// Moving a task to the position it already holds is a no-op returning nil.
func (p *Plan) MoveTask(id, anchor string, before bool) error {
	from := slices.IndexFunc(p.Task, func(t Task) bool { return t.ID == id })
	if from < 0 {
		return fmt.Errorf("task not found: %s", id)
	}
	if id == anchor {
		return fmt.Errorf("cannot move %s relative to itself", id)
	}
	ai := slices.IndexFunc(p.Task, func(t Task) bool { return t.ID == anchor })
	if ai < 0 {
		return fmt.Errorf("anchor task not found: %s", anchor)
	}
	mover := p.Task[from]
	if mover.Status != "" && mover.Status != StatusPending {
		return fmt.Errorf("cannot move %s: status is %s (only pending tasks can be moved)", id, mover.Status)
	}

	// Build the candidate order on a clone; p.Task is untouched until every
	// guard has passed.
	next := slices.Delete(slices.Clone(p.Task), from, from+1)
	mi := slices.IndexFunc(next, func(t Task) bool { return t.ID == anchor })
	if !before {
		mi++
	}
	next = slices.Insert(next, mi, mover)

	// Dependency-order guard: every dependency stays before the mover, every
	// dependent stays after it.
	for i, t := range next {
		if i > mi && slices.Contains(mover.DependsOn, t.ID) {
			return fmt.Errorf("cannot move %s before its dependency %s", id, t.ID)
		}
		if i < mi && slices.Contains(t.DependsOn, id) {
			return fmt.Errorf("cannot move %s after its dependent %s", id, t.ID)
		}
	}

	// Execution guard: a pending task never lands before a done/in_progress
	// task — history stays frozen. Checked before the no-op short-circuit so
	// that requesting a placement ahead of frozen history is rejected loudly
	// even when the mover already sits there (out-of-order history).
	for i := mi + 1; i < len(next); i++ {
		if s := next[i].Status; s == StatusDone || s == StatusInProgress {
			return fmt.Errorf("cannot move %s before %s: %s is %s and execution history stays frozen", id, next[i].ID, next[i].ID, s)
		}
	}

	if mi == from {
		return nil // already in place — no-op
	}

	// Wave adoption: the mover takes the anchor's wave, which must stay
	// strictly after its deepest dependency's wave.
	newWave := p.Task[ai].Wave
	for _, dep := range mover.DependsOn {
		for _, t := range p.Task {
			if t.ID == dep && t.Wave >= newWave {
				return fmt.Errorf("cannot move %s: adopted wave %d is not after its dependency %s (wave %d)", id, newWave, t.ID, t.Wave)
			}
		}
	}
	next[mi].Wave = newWave

	// Reflow PENDING dependents transitively: every dependent's wave must stay
	// strictly greater than its dependency's. Done/in_progress waves are
	// frozen. The step cap is a defensive bound: on a (never valid) dependency
	// cycle the bump chain would otherwise not terminate.
	queue := []string{id}
	steps, limit := 0, len(next)*len(next)
	for len(queue) > 0 && steps <= limit {
		cur := queue[0]
		queue = queue[1:]
		ci := slices.IndexFunc(next, func(t Task) bool { return t.ID == cur })
		for i := range next {
			t := &next[i]
			if !slices.Contains(t.DependsOn, cur) {
				continue
			}
			if t.Status != "" && t.Status != StatusPending {
				continue // frozen
			}
			if t.Wave > next[ci].Wave {
				continue
			}
			t.Wave = next[ci].Wave + 1
			queue = append(queue, t.ID)
			steps++
		}
	}

	p.Task = next
	return nil
}

// TaskEdit carries the fields EditTask may change. A nil pointer means "leave
// unchanged"; a non-nil pointer (even to an empty value) replaces the current
// field. Status is intentionally absent — it is owned by `dross task status`,
// which enforces the lifecycle a free-form edit would let a caller skip.
//
// Files used to be absent too, on the reasoning that edit does not repoint a
// task's files. That left files the one plan.toml field with no CLI edit
// surface, so changing it meant hand-editing the file this subsystem exists to
// make hand-editing unnecessary — and a task's file list is exactly the kind of
// thing execution corrects. It replaces the whole list, like Covers and
// DependsOn.
type TaskEdit struct {
	Title        *string
	Description  *string
	Files        *[]string
	Covers       *[]string
	DependsOn    *[]string
	TestContract *[]string
	Wave         *int
}

// EditTask applies a partial update to the task with the given id: only the
// non-nil fields of e change, every other field is preserved. It does not
// validate — callers pair it with ValidatePlan (via saveIfValid) before saving.
// Errors (mutating nothing) if id is not found.
func (p *Plan) EditTask(id string, e TaskEdit) error {
	t := p.FindTask(id)
	if t == nil {
		return fmt.Errorf("task not found: %s", id)
	}
	if e.Title != nil {
		t.Title = *e.Title
	}
	if e.Description != nil {
		t.Description = *e.Description
	}
	if e.Files != nil {
		t.Files = *e.Files
	}
	if e.Covers != nil {
		t.Covers = *e.Covers
	}
	if e.DependsOn != nil {
		t.DependsOn = *e.DependsOn
	}
	if e.TestContract != nil {
		t.TestContract = *e.TestContract
	}
	if e.Wave != nil {
		t.Wave = *e.Wave
	}
	return nil
}
