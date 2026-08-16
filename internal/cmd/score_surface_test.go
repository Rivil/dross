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
	// 172 killed, 19 survived — and all 19 uncoverable. The suite killed
	// everything it could reach.
	out := captureSummary(t, measured(172, 19, 19))

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
