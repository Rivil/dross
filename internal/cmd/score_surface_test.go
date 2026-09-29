package cmd

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/verify"
)

var scoreLineRE = regexp.MustCompile(`score: (\d\.\d\d) over (\d+) in-scope mutant`)

func captureSummary(t *testing.T, v *verify.Verify) string {
	t.Helper()
	return captureStdout(t, func() { printOverallScore(v) })
}

func measured(killed, survived, notCovered int) *verify.Verify {
	v := &verify.Verify{}
	v.Summary.MutationStatus = verify.MutationMeasured
	v.Summary.MutantsKilled = killed
	v.Summary.MutantsSurvived = survived
	v.Summary.MutantsNotCovered = notCovered
	v.Summary.MutantsInScope = killed + survived
	v.Summary.MutationScore = float64(killed) / float64(killed+survived)
	return v
}

// TestScoreIsPrintedWithItsDenominator is c-5: 0.90 over 10 mutants and 0.90
// over 400 are the same number and not the same evidence, and a reader acting
// on the ratio alone cannot tell them apart.
func TestScoreIsPrintedWithItsDenominator(t *testing.T) {
	small := captureSummary(t, measured(9, 1, 0))
	large := captureSummary(t, measured(360, 40, 0))

	for _, out := range []string{small, large} {
		m := scoreLineRE.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("no score line with a denominator:\n%s", out)
		}
		if m[1] != "0.90" {
			t.Errorf("score = %s, want 0.90:\n%s", m[1], out)
		}
	}
	smallN := scoreLineRE.FindStringSubmatch(small)[2]
	largeN := scoreLineRE.FindStringSubmatch(large)[2]
	if smallN == largeN {
		t.Errorf("two runs of very different size reported the same denominator (%s) — the whole point is that they read differently", smallN)
	}
	if smallN != "10" || largeN != "400" {
		t.Errorf("denominators = %s and %s, want 10 and 400", smallN, largeN)
	}
}

// TestUncoverableCountIsSurfaced: a survivor the tooling cannot reach is a
// different fact from one the tests missed. Reporting only the blended number
// makes an attribution ceiling look like a weak suite.
func TestUncoverableCountIsSurfaced(t *testing.T) {
	// 172 killed, 19 survived — and all 19 uncoverable: no coverage block
	// holds their lines. The suite killed everything it could reach.
	v := measured(172, 19, 19)
	v.Summary.MutantsNoBlock = 19
	out := captureSummary(t, v)

	if !strings.Contains(out, "19 uncoverable") {
		t.Errorf("the uncoverable count is missing:\n%s", out)
	}
	if !strings.Contains(out, "1.00") {
		t.Errorf("the efficacy over reachable mutants is not reported as 1.00 — a suite that killed everything it could reach reads as 0.90 without it:\n%s", out)
	}
	if !strings.Contains(out, "172 reachable") {
		t.Errorf("the reachable denominator is not named:\n%s", out)
	}
}

// TestCountZeroNotCoveredIsATestGap is c-5: a NOT COVERED mutant in a count-0
// block is reachable code no test ran. Calling it uncoverable printed a real
// test gap as efficacy 1.00 over everything reachable.
func TestCountZeroNotCoveredIsATestGap(t *testing.T) {
	v := measured(26, 6, 6)
	v.Summary.MutantsTestGap = 6
	out := captureSummary(t, v)

	if strings.Contains(out, "uncoverable") || strings.Contains(out, "reachable =") {
		t.Errorf("count-0 mutants were called uncoverable or left the denominator:\n%s", out)
	}
	if !strings.Contains(out, "of which 6 NOT COVERED in a coverage block no test ran — a test gap") {
		t.Errorf("the 6 count-0 mutants are not named as a test gap:\n%s", out)
	}
	if m := scoreLineRE.FindStringSubmatch(out); m == nil || m[1] != "0.81" || m[2] != "32" {
		t.Errorf("score line = %v, want 0.81 over 32:\n%s", m, out)
	}
}

// TestNoBlockSplitSetsTheReachableDenominator: only no-block mutants leave the
// denominator. 2 no-block + 4 count-0 of 32 is efficacy 26/30, not 26/26.
func TestNoBlockSplitSetsTheReachableDenominator(t *testing.T) {
	v := measured(26, 6, 6)
	v.Summary.MutantsNoBlock, v.Summary.MutantsTestGap = 2, 4
	out := captureSummary(t, v)

	if !strings.Contains(out, "of which 2 uncoverable by construction") {
		t.Errorf("the no-block count is not the uncoverable count:\n%s", out)
	}
	if !strings.Contains(out, "efficacy over the 30 reachable = 0.87") {
		t.Errorf("efficacy is not 26/30 over the 30 reachable:\n%s", out)
	}
	if !strings.Contains(out, "of which 4 NOT COVERED in a coverage block no test ran") {
		t.Errorf("the count-0 remainder is not named as a test gap:\n%s", out)
	}
	if strings.Contains(out, "unplaced") {
		t.Errorf("every NOT COVERED mutant was placed, yet an unplaced line printed:\n%s", out)
	}
}

// TestUnplacedNotCoveredStaysReachable: NOT COVERED with no profile to place it
// is neither uncoverable nor a proven gap — it is named, and it stays in the
// denominator. "I did not look" must never read as uncoverable.
func TestUnplacedNotCoveredStaysReachable(t *testing.T) {
	v := measured(26, 6, 6)
	v.Summary.MutantsNoBlock, v.Summary.MutantsTestGap = 1, 2
	out := captureSummary(t, v)

	if !strings.Contains(out, "of which 3 NOT COVERED unplaced") {
		t.Errorf("the 3 unplaced mutants are not named:\n%s", out)
	}
	if !strings.Contains(out, "efficacy over the 31 reachable") {
		t.Errorf("unplaced mutants left the reachable denominator:\n%s", out)
	}
}

// TestNoNotCoveredLinesWhenNoneAreNotCovered: the test-gap and unplaced lines
// are held to the same rule as the uncoverable one — "0 NOT COVERED" is not
// news either.
func TestNoNotCoveredLinesWhenNoneAreNotCovered(t *testing.T) {
	out := captureSummary(t, measured(9, 1, 0))
	if strings.Contains(out, "NOT COVERED") || strings.Contains(out, "reachable") {
		t.Errorf("a NOT COVERED line printed with nothing to report:\n%s", out)
	}
}

// TestNoUncoverableLineWhenThereAreNone: a line that is always there stops
// being read, and "0 uncoverable" is not news.
func TestNoUncoverableLineWhenThereAreNone(t *testing.T) {
	out := captureSummary(t, measured(9, 1, 0))
	if strings.Contains(out, "uncoverable") {
		t.Errorf("the uncoverable line printed with nothing to report:\n%s", out)
	}
}

// TestUnmeasuredRunsGetNoScoreLine: the other statuses print their own line
// explaining the score is 0/0 and why. Giving a meaningless number a
// denominator dresses it up as a measurement.
func TestUnmeasuredRunsGetNoScoreLine(t *testing.T) {
	for _, status := range []string{verify.MutationOutOfScope, verify.MutationUnmeasurable, verify.MutationSkipped} {
		v := &verify.Verify{}
		v.Summary.MutationStatus = status
		out := captureSummary(t, v)
		if strings.Contains(out, "score:") {
			t.Errorf("status %q printed a score line:\n%s", status, out)
		}
	}
}

// TestPromptCarriesTheDenominatorGuidance: the CLI printing it is half the job.
// The report a human reads is written from the prompt, and the prompt used to
// leave the denominator to the reader's discretion — which is why it was being
// typed into verify.toml notes by hand, run after run.
func TestPromptCarriesTheDenominatorGuidance(t *testing.T) {
	body := promptBody(t, "verify.md")
	if !strings.Contains(body, "Read the score with its denominator") {
		t.Error("verify.md does not tell the reporter to carry the denominator")
	}
	if !strings.Contains(body, "uncoverable") {
		t.Error("verify.md does not mention the uncoverable count")
	}
}
