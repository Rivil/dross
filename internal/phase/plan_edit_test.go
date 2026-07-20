package phase

import (
	"strings"
	"testing"
)

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
