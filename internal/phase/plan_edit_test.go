package phase

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestAddTask(t *testing.T) {
	// Insert after an anchor: existing tasks keep their id/fields and order.
	pre := []Task{
		{ID: "t-1", Wave: 1, Title: "one"},
		{ID: "t-2", Wave: 1, Title: "two"},
		{ID: "t-3", Wave: 2, Title: "three"},
	}
	p := &Plan{TaskSeq: 3, Task: slices.Clone(pre)}
	got, err := p.AddTask(NewTask{Title: "new", Covers: []string{"c-1"}}, "t-1", false)
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"t-1", "t-4", "t-2", "t-3"}
	var order []string
	for _, tk := range p.Task {
		order = append(order, tk.ID)
	}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Errorf("order = %v, want %v", order, wantOrder)
	}
	if got.ID != "t-4" {
		t.Errorf("new id = %s, want t-4", got.ID)
	}
	if p.TaskSeq != 4 {
		t.Errorf("high-water = %d, want 4 (advanced)", p.TaskSeq)
	}
	// Existing tasks byte-equal to their pre-image (placement never renumbers).
	for _, want := range pre {
		if fresh := p.FindTask(want.ID); !reflect.DeepEqual(*fresh, want) {
			t.Errorf("existing task %s mutated: got %+v, want %+v", want.ID, *fresh, want)
		}
	}

	// No anchor -> tail append.
	tail := &Plan{TaskSeq: 3, Task: slices.Clone(pre)}
	if _, err := tail.AddTask(NewTask{Title: "last"}, "", false); err != nil {
		t.Fatal(err)
	}
	if last := tail.Task[len(tail.Task)-1]; last.ID != "t-4" || last.Title != "last" {
		t.Errorf("tail append = %+v, want id t-4 title last at end", last)
	}
}

func TestRemoveTask(t *testing.T) {
	newPlan := func() *Plan {
		return &Plan{TaskSeq: 2, Task: []Task{
			{ID: "t-1", Wave: 1},
			{ID: "t-2", Wave: 2, DependsOn: []string{"t-1"}},
		}}
	}

	// force=false with a dependent refuses and mutates nothing.
	p := newPlan()
	err := p.RemoveTask("t-1", false)
	if err == nil {
		t.Fatal("expected refusal when t-1 is depended on")
	}
	if len(p.Task) != 2 || !slices.Contains(p.FindTask("t-2").DependsOn, "t-1") {
		t.Errorf("refusal mutated the plan: %+v", p.Task)
	}

	// force=true drops t-1 and strips the dangling dep, wave unchanged, no reflow.
	p = newPlan()
	if err := p.RemoveTask("t-1", true); err != nil {
		t.Fatal(err)
	}
	if p.FindTask("t-1") != nil {
		t.Error("t-1 not removed under force")
	}
	t2 := p.FindTask("t-2")
	if slices.Contains(t2.DependsOn, "t-1") {
		t.Error("no dangling dep: t-1 must be stripped from t-2.depends_on")
	}
	if t2.Wave != 2 {
		t.Errorf("t-2 wave reflowed to %d, want 2 (no reflow)", t2.Wave)
	}
}

func TestRemoveTaskBackfillsHighWater(t *testing.T) {
	// Fresh plan (TaskSeq unset): removing the HIGHEST task must still advance
	// the high-water so the freed id is never reissued (locked id_scheme).
	p := &Plan{Task: []Task{{ID: "t-1"}, {ID: "t-2"}, {ID: "t-3"}}}
	if err := p.RemoveTask("t-3", false); err != nil {
		t.Fatal(err)
	}
	if p.TaskSeq != 3 {
		t.Errorf("high-water = %d, want 3 (backfilled from the removed highest)", p.TaskSeq)
	}
	if got := p.NextTaskID(); got != "t-4" {
		t.Errorf("NextTaskID after removing highest = %s, want t-4 (freed id not reused)", got)
	}

	// A refused remove must not backfill either — it mutates nothing.
	q := &Plan{Task: []Task{{ID: "t-1"}, {ID: "t-2", DependsOn: []string{"t-1"}}}}
	if err := q.RemoveTask("t-1", false); err == nil {
		t.Fatal("expected refusal when t-1 is depended on")
	}
	if q.TaskSeq != 0 {
		t.Errorf("refused remove bumped high-water to %d, want 0 (mutates nothing)", q.TaskSeq)
	}
}

func TestEditTask(t *testing.T) {
	pre := Task{ID: "t-2", Wave: 2, Title: "old", Covers: []string{"c-1"}, DependsOn: []string{"t-1"}, Files: []string{"a.go"}, Status: StatusDone}
	p := &Plan{Task: []Task{{ID: "t-1", Wave: 1}, pre}}

	if err := p.EditTask("t-2", TaskEdit{Title: ptr("new")}); err != nil {
		t.Fatal(err)
	}
	t2 := p.FindTask("t-2")
	if t2.Title != "new" {
		t.Errorf("title = %q, want new", t2.Title)
	}
	// Every other field equal to the pre-image.
	want := pre
	want.Title = "new"
	if !reflect.DeepEqual(*t2, want) {
		t.Errorf("edit changed more than Title: got %+v, want %+v", *t2, want)
	}
}

// snapshotPlan deep-copies a plan so tests can assert a rejected move mutated
// nothing (Task is cloned; nested slices are shared but never written by a
// rejected move, and reflect.DeepEqual still catches any in-place write).
func snapshotPlan(p *Plan) *Plan {
	return &Plan{TaskSeq: p.TaskSeq, Phase: p.Phase, Task: slices.Clone(p.Task)}
}

func taskOrder(p *Plan) []string {
	var order []string
	for _, t := range p.Task {
		order = append(order, t.ID)
	}
	return order
}

func TestMoveTaskRejectsBeforeDependency(t *testing.T) {
	p := &Plan{Task: []Task{
		{ID: "t-1", Wave: 1, Status: StatusPending},
		{ID: "t-2", Wave: 2, Status: StatusPending, DependsOn: []string{"t-1"}},
	}}
	pre := snapshotPlan(p)
	err := p.MoveTask("t-2", "t-1", true)
	if err == nil {
		t.Fatal("expected rejection: t-2 moved before its dependency t-1")
	}
	if !strings.Contains(err.Error(), "t-2") || !strings.Contains(err.Error(), "t-1") {
		t.Errorf("error must name both ids, got %q", err)
	}
	if !reflect.DeepEqual(p, pre) {
		t.Errorf("rejected move mutated the plan: got %+v, want %+v", p, pre)
	}
}

func TestMoveTaskRejectsAfterDependent(t *testing.T) {
	p := &Plan{Task: []Task{
		{ID: "t-1", Wave: 1, Status: StatusPending},
		{ID: "t-2", Wave: 2, Status: StatusPending, DependsOn: []string{"t-1"}},
	}}
	pre := snapshotPlan(p)
	err := p.MoveTask("t-1", "t-2", false)
	if err == nil {
		t.Fatal("expected rejection: t-1 moved after its dependent t-2")
	}
	if !reflect.DeepEqual(p, pre) {
		t.Errorf("rejected move mutated the plan: got %+v, want %+v", p, pre)
	}
}

func TestMoveTaskExecutionGuard(t *testing.T) {
	// Moving a done task errors.
	p := &Plan{Task: []Task{
		{ID: "t-1", Wave: 1, Status: StatusDone},
		{ID: "t-2", Wave: 1, Status: StatusPending},
	}}
	pre := snapshotPlan(p)
	if err := p.MoveTask("t-1", "t-2", false); err == nil {
		t.Error("expected rejection: t-1 is done, only pending tasks move")
	}
	if !reflect.DeepEqual(p, pre) {
		t.Errorf("rejected move mutated the plan: %+v", p)
	}

	// Moving a pending task before an in_progress task errors.
	q := &Plan{Task: []Task{
		{ID: "t-1", Wave: 1, Status: StatusInProgress},
		{ID: "t-2", Wave: 1, Status: StatusPending},
	}}
	qpre := snapshotPlan(q)
	if err := q.MoveTask("t-2", "t-1", true); err == nil {
		t.Error("expected rejection: pending t-2 moved before in_progress t-1")
	}
	if !reflect.DeepEqual(q, qpre) {
		t.Errorf("rejected move mutated the plan: %+v", q)
	}
}

func TestMoveTaskOutOfOrderHistory(t *testing.T) {
	// Done tasks mid-array: a pending task may not land before either of them,
	// but moving it past the later one is legal.
	newPlan := func() *Plan {
		return &Plan{Task: []Task{
			{ID: "t-1", Wave: 1, Status: StatusDone},
			{ID: "t-2", Wave: 1, Status: StatusPending},
			{ID: "t-3", Wave: 1, Status: StatusDone},
		}}
	}
	p := newPlan()
	if err := p.MoveTask("t-2", "t-3", true); err == nil {
		t.Error("expected rejection: t-2 before done t-3")
	}
	p = newPlan()
	if err := p.MoveTask("t-2", "t-3", false); err != nil {
		t.Fatalf("move after the last done task must succeed, got %v", err)
	}
	if want := []string{"t-1", "t-3", "t-2"}; !reflect.DeepEqual(taskOrder(p), want) {
		t.Errorf("order = %v, want %v", taskOrder(p), want)
	}
}

func TestMoveTaskAdoptsAnchorWaveAndReflows(t *testing.T) {
	// t-4 is a done transitive dependent (via t-3): reflow must leave its wave
	// frozen even as t-3's wave bumps past it.
	p := &Plan{Task: []Task{
		{ID: "t-4", Wave: 2, Status: StatusDone, DependsOn: []string{"t-3"}},
		{ID: "t-1", Wave: 1, Status: StatusPending},
		{ID: "t-2", Wave: 2, Status: StatusPending},
		{ID: "t-3", Wave: 2, Status: StatusPending, DependsOn: []string{"t-1"}},
	}}
	if err := p.MoveTask("t-1", "t-2", false); err != nil {
		t.Fatal(err)
	}
	if want := []string{"t-4", "t-2", "t-1", "t-3"}; !reflect.DeepEqual(taskOrder(p), want) {
		t.Errorf("order = %v, want %v", taskOrder(p), want)
	}
	if got := p.FindTask("t-1").Wave; got != 2 {
		t.Errorf("mover wave = %d, want 2 (adopted from anchor)", got)
	}
	if got := p.FindTask("t-3").Wave; got != 3 {
		t.Errorf("pending dependent wave = %d, want 3 (bumped past mover)", got)
	}
	if got := p.FindTask("t-4").Wave; got != 2 {
		t.Errorf("done transitive dependent wave = %d, want 2 (frozen)", got)
	}
	if err := ValidatePlan(p, nil); err != nil {
		t.Errorf("plan invalid after legal move: %v", err)
	}
}

func TestMoveTaskRejectsWaveIllegalAdoption(t *testing.T) {
	// t-3's dependency sits in wave 2; moving it before the wave-1 anchor t-2
	// would adopt wave 1 <= 2 — rejected even though the array order is legal.
	p := &Plan{Task: []Task{
		{ID: "t-1", Wave: 2, Status: StatusDone},
		{ID: "t-2", Wave: 1, Status: StatusPending},
		{ID: "t-3", Wave: 3, Status: StatusPending, DependsOn: []string{"t-1"}},
	}}
	pre := snapshotPlan(p)
	if err := p.MoveTask("t-3", "t-2", true); err == nil {
		t.Fatal("expected rejection: adopted wave 1 is not after dependency wave 2")
	}
	if !reflect.DeepEqual(p, pre) {
		t.Errorf("rejected move mutated the plan: %+v", p)
	}
}

func TestMoveTaskKeepsIDsStable(t *testing.T) {
	p := &Plan{TaskSeq: 3, Task: []Task{
		{ID: "t-1", Wave: 1, Status: StatusPending},
		{ID: "t-2", Wave: 1, Status: StatusPending},
		{ID: "t-3", Wave: 1, Status: StatusPending},
	}}
	preIDs := taskOrder(p)
	preSeq := p.TaskSeq
	preNext := p.NextTaskID()
	if err := p.MoveTask("t-3", "t-1", true); err != nil {
		t.Fatal(err)
	}
	gotIDs := taskOrder(p)
	slices.Sort(preIDs)
	slices.Sort(gotIDs)
	if !reflect.DeepEqual(gotIDs, preIDs) {
		t.Errorf("id set changed: %v vs %v", gotIDs, preIDs)
	}
	if p.TaskSeq != preSeq {
		t.Errorf("TaskSeq = %d, want %d (untouched)", p.TaskSeq, preSeq)
	}
	if got := p.NextTaskID(); got != preNext {
		t.Errorf("NextTaskID = %s, want %s (unchanged)", got, preNext)
	}
}

func TestMoveTaskBadInputs(t *testing.T) {
	newPlan := func() *Plan {
		return &Plan{Task: []Task{
			{ID: "t-1", Wave: 1, Status: StatusPending},
			{ID: "t-2", Wave: 1, Status: StatusPending},
		}}
	}

	// Unknown anchor: exact AddTask-parity wording, plan unchanged.
	p := newPlan()
	pre := snapshotPlan(p)
	err := p.MoveTask("t-1", "t-9", false)
	if err == nil || !strings.Contains(err.Error(), "anchor task not found: t-9") {
		t.Errorf("unknown anchor error = %v, want 'anchor task not found: t-9'", err)
	}
	if !reflect.DeepEqual(p, pre) {
		t.Errorf("unknown anchor mutated the plan: %+v", p)
	}

	// Unknown mover.
	p = newPlan()
	if err := p.MoveTask("t-9", "t-1", false); err == nil {
		t.Error("expected error for unknown task id")
	}

	// anchor == self.
	p = newPlan()
	if err := p.MoveTask("t-1", "t-1", false); err == nil {
		t.Error("expected error for anchor==self")
	}

	// No-op move: nil error, plan unchanged.
	p = newPlan()
	pre = snapshotPlan(p)
	if err := p.MoveTask("t-2", "t-1", false); err != nil {
		t.Fatalf("no-op move must return nil, got %v", err)
	}
	if !reflect.DeepEqual(p, pre) {
		t.Errorf("no-op move mutated the plan: %+v", p)
	}
}

func TestValidatePlan(t *testing.T) {
	spec := &Spec{Criteria: []Criterion{{ID: "c-1"}, {ID: "c-2"}}}
	cases := []struct {
		name       string
		plan       *Plan
		wantErr    bool
		wantNaming string // substring the error must contain when wantErr
	}{
		{
			name:    "clean plan",
			plan:    &Plan{Task: []Task{{ID: "t-1", Covers: []string{"c-1"}}, {ID: "t-2", DependsOn: []string{"t-1"}}}},
			wantErr: false,
		},
		{
			name:       "duplicate id",
			plan:       &Plan{Task: []Task{{ID: "t-2"}, {ID: "t-2"}}},
			wantErr:    true,
			wantNaming: "duplicate task id t-2",
		},
		{
			name:       "unknown depends_on",
			plan:       &Plan{Task: []Task{{ID: "t-1", DependsOn: []string{"t-99"}}}},
			wantErr:    true,
			wantNaming: "t-99",
		},
		{
			name:       "covers unknown criterion",
			plan:       &Plan{Task: []Task{{ID: "t-1", Covers: []string{"c-99"}}}},
			wantErr:    true,
			wantNaming: "c-99",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePlan(tc.plan, spec)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidatePlan() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidatePlan() = %v, want nil", err)
			}
			if tc.wantErr && !strings.Contains(err.Error(), tc.wantNaming) {
				t.Errorf("ValidatePlan() = %q, want it to name %q", err, tc.wantNaming)
			}
		})
	}
}

// TestValidatePlanCoversParity pins ValidatePlan's covers->criterion wording to
// the string `dross validate` (internal/cmd/validate.go) emits, so the c-1
// post-write validate can never disagree with this pre-write guard.
func TestValidatePlanCoversParity(t *testing.T) {
	spec := &Spec{Criteria: []Criterion{{ID: "c-1"}}}
	plan := &Plan{Task: []Task{{ID: "t-1", Covers: []string{"c-99"}}}}
	err := ValidatePlan(plan, spec)
	if err == nil {
		t.Fatal("expected covers error")
	}
	if want := "task t-1 covers unknown criterion c-99"; !strings.Contains(err.Error(), want) {
		t.Errorf("parity: got %q, want it to contain %q", err, want)
	}
}

func TestNextTaskID(t *testing.T) {
	cases := []struct {
		name string
		seq  int
		ids  []string
		want string
	}{
		{"seq set over {t-1,t-3}", 3, []string{"t-1", "t-3"}, "t-4"},
		// seq unset (0): backfill from the current max id.
		{"backfill from max when unset", 0, []string{"t-1", "t-3"}, "t-4"},
		// After removing the HIGHEST task (t-3 of {t-1,t-2,t-3}) the plan keeps
		// seq=3, so the freed t-3 is never reissued — next is t-4.
		{"freed highest id not reused", 3, []string{"t-1", "t-2"}, "t-4"},
		{"empty plan", 0, nil, "t-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Plan{TaskSeq: tc.seq}
			for _, id := range tc.ids {
				p.Task = append(p.Task, Task{ID: id})
			}
			if got := p.NextTaskID(); got != tc.want {
				t.Errorf("NextTaskID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDeriveWave(t *testing.T) {
	tasks := []Task{
		{ID: "t-1", Wave: 1},
		{ID: "t-2", Wave: 3},
	}
	cases := []struct {
		name     string
		explicit int
		deps     []string
		want     int
	}{
		{"deepest dep + 1", 0, []string{"t-1", "t-2"}, 4},
		{"explicit wave wins", 2, []string{"t-1", "t-2"}, 2},
		{"no deps defaults to 1", 0, nil, 1},
		{"unknown dep contributes nothing", 0, []string{"t-99"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveWave(tc.explicit, tc.deps, tasks); got != tc.want {
				t.Errorf("deriveWave(%d, %v) = %d, want %d", tc.explicit, tc.deps, got, tc.want)
			}
		})
	}
}

// TestAddTaskCarriesTestContract pins the field that made `dross task add`
// only half a replacement for hand-editing plan.toml: a task could be added
// from the CLI but its test contract could not, so the one field /dross-verify
// reads to map criteria to tests was reachable only by opening the file.
func TestAddTaskCarriesTestContract(t *testing.T) {
	p := &Plan{TaskSeq: 1, Task: []Task{{ID: "t-1", Wave: 1, Title: "one"}}}
	contract := []string{"if A breaks, TestA fails", "if B breaks, TestB fails"}
	got, err := p.AddTask(NewTask{Title: "new", TestContract: contract}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.TestContract, contract) {
		t.Errorf("test_contract = %v, want %v — order included: a contract read in a different order is a different contract", got.TestContract, contract)
	}
}

func TestEditTaskReplacesTestContract(t *testing.T) {
	p := &Plan{TaskSeq: 1, Task: []Task{{
		ID: "t-1", Wave: 1, Title: "one",
		TestContract: []string{"old one", "old two"},
	}}}
	want := []string{"fresh"}
	if err := p.EditTask("t-1", TaskEdit{TestContract: ptr(want)}); err != nil {
		t.Fatal(err)
	}
	if got := p.FindTask("t-1").TestContract; !reflect.DeepEqual(got, want) {
		t.Errorf("test_contract = %v, want %v — a non-nil pointer replaces wholesale, like every other TaskEdit field", got, want)
	}
}

func TestEditTaskSetsDescription(t *testing.T) {
	p := &Plan{TaskSeq: 1, Task: []Task{{ID: "t-1", Wave: 1, Title: "one"}}}
	if err := p.EditTask("t-1", TaskEdit{Description: ptr("what it does")}); err != nil {
		t.Fatal(err)
	}
	if got := p.FindTask("t-1").Description; got != "what it does" {
		t.Errorf("description = %q, want %q — settable on add and previously nowhere else", got, "what it does")
	}
}

// TestEditTaskLeavesUnsetFieldsAlone is what makes a partial edit safe: an edit
// that touches only the title must not disturb the two fields this phase added,
// or every `task edit` becomes a way to silently drop a contract.
func TestEditTaskLeavesUnsetFieldsAlone(t *testing.T) {
	pre := Task{
		ID: "t-1", Wave: 1, Title: "one",
		Description:  "kept",
		TestContract: []string{"kept one", "kept two"},
	}
	p := &Plan{TaskSeq: 1, Task: []Task{pre}}
	if err := p.EditTask("t-1", TaskEdit{Title: ptr("renamed")}); err != nil {
		t.Fatal(err)
	}
	got := p.FindTask("t-1")
	if got.Title != "renamed" {
		t.Errorf("title = %q, want renamed", got.Title)
	}
	if got.Description != pre.Description {
		t.Errorf("description = %q, want it untouched (%q)", got.Description, pre.Description)
	}
	if !reflect.DeepEqual(got.TestContract, pre.TestContract) {
		t.Errorf("test_contract = %v, want it untouched (%v)", got.TestContract, pre.TestContract)
	}
}
