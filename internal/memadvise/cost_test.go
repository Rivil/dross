package memadvise

import (
	"math"
	"testing"
)

func TestMonthlyCostFormula(t *testing.T) {
	got := MonthlyUSD(400, 1000)
	want := 100.0 * 1000 * CacheReadUSDPerMTok / 1e6
	if got != want {
		t.Errorf("MonthlyUSD(400, 1000) = %v, want %v", got, want)
	}
	if got := MonthlyUSD(400, 0); got != 0 {
		t.Errorf("0 turns cost %v, want 0", got)
	}
	// ceil(bytes/4): 401 bytes is 101 tokens, not 100.25 or 100.
	if got, want := MonthlyUSD(401, 1000), 101.0*1000*CacheReadUSDPerMTok/1e6; got != want {
		t.Errorf("MonthlyUSD(401, 1000) = %v, want %v", got, want)
	}
	if Tokens(0) != 0 || Tokens(-5) != 0 || Tokens(1) != 1 || Tokens(4) != 1 || Tokens(5) != 2 {
		t.Error("Tokens is not ceil(n/4) clamped at 0")
	}
	if CacheReadUSDPerMTok != 0.20 {
		t.Errorf("CacheReadUSDPerMTok = %v, want the published 0.20", CacheReadUSDPerMTok)
	}
}

func TestShareZeroTotal(t *testing.T) {
	if got := ShareText(TurnCounts{Repo: 0, All: 0}); got != "n/a" {
		t.Errorf("ShareText with no turns = %q, want n/a", got)
	}
	if got := ShareText(TurnCounts{Repo: 1, All: 4}); got != "25%" {
		t.Errorf("ShareText(1 of 4) = %q, want 25%%", got)
	}
	if v := (TurnCounts{}).RepoShare(); math.IsNaN(v) {
		t.Error("RepoShare with no turns is NaN")
	}
}
