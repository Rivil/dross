package review

import "testing"

func pass(tree string) Round { return Round{Outcome: OutcomePass, Tree: tree} }
func block() Round           { return Round{Outcome: OutcomeBlock} }
func stale() Round           { return Round{Outcome: OutcomeStale} }
func unavailable(cause string) Round {
	return Round{Outcome: OutcomeUnavailable, Cause: cause}
}

func TestLedgerState(t *testing.T) {
	cases := []struct {
		name   string
		rounds []Round
		want   State
	}{
		{"none", nil, State{Status: StatusNone}},
		{"pass", []Round{pass("T1")}, State{Status: StatusPass, Tree: "T1"}},
		{"blocked", []Round{block()}, State{Status: StatusBlocked}},
		{"fixed in the fix round", []Round{block(), pass("T2")}, State{Status: StatusPass, Tree: "T2"}},
		{"two blocks exhaust", []Round{block(), block()}, State{Status: StatusExhausted, Cause: "still blocked after the one fix round"}},
		{"a pass does not reset the cap", []Round{pass("T1"), block(), block()}, State{Status: StatusExhausted, Cause: "still blocked after the one fix round"}},
		{"stale neither counts nor resets", []Round{block(), stale(), block()}, State{Status: StatusExhausted, Cause: "still blocked after the one fix round"}},
		{"exhausted is sticky", []Round{block(), block(), pass("T3")}, State{Status: StatusExhausted, Cause: "still blocked after the one fix round"}},
		{"unavailable is sticky", []Round{unavailable("no fence"), pass("T1")}, State{Status: StatusUnavailable, Cause: "no fence"}},
		{"stale alone is none", []Round{stale()}, State{Status: StatusNone}},
		{"stale after a pass keeps it", []Round{pass("T1"), stale()}, State{Status: StatusPass, Tree: "T1"}},
		{"pass then one block is blocked", []Round{pass("T1"), block()}, State{Status: StatusBlocked}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StateOf(c.rounds); got != c.want {
				t.Fatalf("StateOf = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	b1 := Finding{Criterion: "c-1", Severity: Blocking, Text: "B1"}
	f1 := Finding{Severity: Flag, Text: "F1"}
	n1 := Finding{Severity: Note, Text: "N1"}
	q1 := Finding{Severity: Blocking, Text: "Q1"}

	got := Resolve([]Round{
		{Outcome: OutcomeBlock, Spec: []Finding{b1}, Quality: []Finding{f1, q1}},
		{Outcome: OutcomePass, Tree: "T2", Quality: []Finding{n1}},
	})
	want := []Resolved{
		{1, KindSpec, b1, ResolvedFixed},
		{1, KindQuality, f1, ResolvedLeft},
		{1, KindQuality, q1, ResolvedFixed},
		{2, KindQuality, n1, ResolvedLeft},
	}
	assertResolved(t, got, want)

	b2 := Finding{Criterion: "c-3", Severity: Blocking, Text: "B2"}
	got = Resolve([]Round{
		{Outcome: OutcomeBlock, Spec: []Finding{b1}},
		{Outcome: OutcomeBlock, Spec: []Finding{b2}, Quality: []Finding{f1}},
	})
	want = []Resolved{
		{1, KindSpec, b1, ResolvedUnresolved},
		{2, KindSpec, b2, ResolvedUnresolved},
		{2, KindQuality, f1, ResolvedLeft},
	}
	assertResolved(t, got, want)
}

func assertResolved(t *testing.T, got, want []Resolved) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Resolve returned %d findings, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("finding %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
