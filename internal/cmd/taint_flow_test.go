package cmd

import "testing"

// The cross-function channels of the taint engine, each pinned by a corpus
// whose must-trip and must-not-trip lines are written down beside the code.

// TestTaintCrossesReturns: results, helpers, generics, closures, channels and
// recursion — and a summary applied per call site, so a helper handed a
// literal stays clean.
func TestTaintCrossesReturns(t *testing.T) {
	fx := loadFixture(t, fixturePath("taint", "channels_return.go.txt"))
	assertCorpus(t, fx, "taint", taintHits(runTaint(fx.srcView, execTaintPolicy())))
}

// TestTaintIsKeyedByField: a field carries taint between functions, and only
// that field does.
func TestTaintIsKeyedByField(t *testing.T) {
	fx := loadFixture(t, fixturePath("taint", "channels_field.go.txt"))
	assertCorpus(t, fx, "taint", taintHits(runTaint(fx.srcView, execTaintPolicy())))
}

// TestTaintFollowsWriters: a writer parameter's taint lands in each caller's
// argument; terminal writers — including a variable or field only ever
// holding one — end it; an uncalled writer parameter fails closed; and an
// object teed in through io.MultiWriter receives the stream via its Write.
func TestTaintFollowsWriters(t *testing.T) {
	fx := loadFixture(t, fixturePath("taint", "channels_writer.go.txt"))
	assertCorpus(t, fx, "taint", taintHits(runTaint(fx.srcView, execTaintPolicy())))
}
