package mutation

import (
	"math"
	"testing"
)

// TestTwoUnequalLegsPoolRatherThanAverage is the case that killed the mean.
//
// Legs of 1/1 and 0/9 are a suite that caught one mutant in ten. The mean over
// legs called that 0.50 — a factor of five, arrived at by handing the
// one-mutant leg the same vote as the nine-mutant one.
func TestTwoUnequalLegsPoolRatherThanAverage(t *testing.T) {
	legs := []*Report{
		{Tool: "stryker", Killed: 1, Survived: 0},
		{Tool: "gremlins", Killed: 0, Survived: 9},
	}
	score, killed, survived, timeout := PoolReports(legs)

	if killed != 1 || survived != 9 || timeout != 0 {
		t.Fatalf("totals = %d/%d/%d, want 1/9/0", killed, survived, timeout)
	}
	if math.Abs(score-0.1) > 1e-9 {
		t.Errorf("pooled score = %v, want 0.1 — the mean reported 0.5 for this exact shape", score)
	}
	if math.Abs(score-0.5) < 1e-9 {
		t.Error("the mean is back")
	}
}

// TestTimeoutCountsAgainstTheScore: a mutant that timed out was not killed.
// Excluding it lets a suite that HANGS on its hardest mutants outscore one that
// merely fails them, which inverts what the score is for.
func TestTimeoutCountsAgainstTheScore(t *testing.T) {
	withTimeout := PooledScore(5, 0, 5)
	if math.Abs(withTimeout-0.5) > 1e-9 {
		t.Errorf("PooledScore(5,0,5) = %v, want 0.5 — timeouts belong in the denominator", withTimeout)
	}
	if math.Abs(withTimeout-1.0) < 1e-9 {
		t.Error("timeouts left the denominator: a suite that hung on half its mutants scored a perfect 1.00")
	}
	// And the two shapes must be distinguishable: 5 killed of 10 attempted is
	// the same score whether the other five survived or hung.
	if PooledScore(5, 5, 0) != PooledScore(5, 0, 5) {
		t.Error("a survived mutant and a timed-out mutant score differently — both are mutants the suite did not kill")
	}
}

// TestEmptyRunScoresZeroNotNaN: an out-of-scope run has no mutants at all, and
// every prompt-only phase in this milestone hits it. NaN would propagate into
// verify.toml, telemetry and every comparison downstream.
func TestEmptyRunScoresZeroNotNaN(t *testing.T) {
	got := PooledScore(0, 0, 0)
	if math.IsNaN(got) {
		t.Fatal("PooledScore(0,0,0) is NaN — it would reach verify.toml and every comparison after it")
	}
	if got != 0 {
		t.Errorf("PooledScore(0,0,0) = %v, want 0", got)
	}
	if score, _, _, _ := PoolReports(nil); score != 0 {
		t.Errorf("PoolReports(nil) = %v, want 0", score)
	}
	if score, _, _, _ := PoolReports([]*Report{nil, nil}); score != 0 {
		t.Errorf("PoolReports of nil legs = %v, want 0 — a leg that failed to run contributes nothing, it does not panic", score)
	}
}

// TestPerfectAndZeroAreReachable is the sanity floor: a formula that could not
// express either end would make every assertion above meaningless.
func TestPerfectAndZeroAreReachable(t *testing.T) {
	if got := PooledScore(10, 0, 0); got != 1 {
		t.Errorf("an all-killed run scored %v, want 1", got)
	}
	if got := PooledScore(0, 10, 0); got != 0 {
		t.Errorf("an all-survived run scored %v, want 0", got)
	}
}

// TestPoolReportsReturnsItsDenominator: the totals come back with the ratio
// because a score without its denominator is not a measurement anyone can act
// on — 0.90 over 10 mutants and over 400 are the same number and not the same
// evidence.
func TestPoolReportsReturnsItsDenominator(t *testing.T) {
	score, killed, survived, timeout := PoolReports([]*Report{
		{Killed: 9, Survived: 1, Timeout: 0},
		{Killed: 81, Survived: 9, Timeout: 0},
	})
	if killed+survived+timeout != 100 {
		t.Errorf("denominator = %d, want 100", killed+survived+timeout)
	}
	if math.Abs(score-0.9) > 1e-9 {
		t.Errorf("score = %v, want 0.9", score)
	}
}
