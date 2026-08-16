package mutation

// The one mutation-score formula.
//
// There used to be three, and they disagreed. verify.toml took the MEAN across
// language legs, telemetry POOLED killed/(killed+survived) with timeouts
// excluded, Report.Score divided by killed+survived+timeout — and adapter.go's
// struct doc claimed a fourth thing none of them did. A two-leg run of 1/1 and
// 0/9 reported 0.50 or 0.10 depending on which surface you happened to read,
// and every phase's judgement rested on whichever one you saw.
//
// Both choices below are the honest reading rather than the convenient one:
//
//   - POOLED, not the mean. A mean over legs of unequal size is not a property
//     of the code under test — it hands the smaller leg the louder vote. 1/1
//     beside 0/9 is a suite that killed one mutant in ten; calling it 0.50
//     flatters it by a factor of five.
//
//   - TIMEOUTS IN THE DENOMINATOR. A mutant that timed out was not killed.
//     Excluding it lets a suite that hangs on its hardest mutants outscore one
//     that merely fails them, which inverts the thing the score is for.

// PooledScore is killed / (killed + survived + timeout), the fraction of
// mutants the suite actually caught.
//
// A denominator of zero returns 0 rather than NaN: an out-of-scope run — every
// prompt-only phase — has no mutants at all, and NaN would propagate into
// verify.toml, telemetry and every comparison downstream. Callers distinguish
// "nothing to measure" by the mutant COUNT, which is reported beside the score
// precisely so a 0.00 cannot be mistaken for a failing suite.
func PooledScore(killed, survived, timeout int) float64 {
	denom := killed + survived + timeout
	if denom <= 0 {
		return 0
	}
	return float64(killed) / float64(denom)
}

// PoolReports sums the counters across reports and returns the pooled score
// with the totals it was computed from.
//
// It returns the totals rather than only the ratio because a score without its
// denominator is not a measurement anyone can act on: 0.90 over 10 mutants and
// 0.90 over 400 are the same number and not the same evidence. Every surface
// that prints the score prints the count beside it, and this is what makes that
// cheap enough to always do.
func PoolReports(reports []*Report) (score float64, killed, survived, timeout int) {
	for _, r := range reports {
		if r == nil {
			continue
		}
		killed += r.Killed
		survived += r.Survived
		timeout += r.Timeout
	}
	return PooledScore(killed, survived, timeout), killed, survived, timeout
}
