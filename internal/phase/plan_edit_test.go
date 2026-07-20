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
