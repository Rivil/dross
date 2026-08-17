package cmd

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/verify"
)

// gateCountRE reads the number stdout claims is undispositioned in scope — the
// one the verdict fails on.
var gateCountRE = regexp.MustCompile(`(\d+) undispositioned in scope`)

// TestStdoutAndVerifyTomlAgreeOnUnclassified is the criterion, run as ONE
// verify with both surfaces read from that single run.
//
// Comparing two separate runs would prove nothing about the disagreement: the
// bug was that the same run derived the same fact twice, by two routes, and got
// two answers — stdout printing "0 unclassified" while the file it had just
// written recorded unclassified_in_scope = 1.
func TestStdoutAndVerifyTomlAgreeOnUnclassified(t *testing.T) {
	dir := lifecycleRepo(t, "vocab")

	out := runVerifyCapturing(t, "01-vocab")

	m := gateCountRE.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("stdout never states the undispositioned-in-scope count:\n%s", out)
	}
	printed, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}

	_, vPath := verify.FilePaths(filepath.Join(dir, ".dross"), "01-vocab")
	v, err := verify.LoadVerify(vPath)
	if err != nil {
		t.Fatalf("load verify.toml: %v", err)
	}

	if printed != v.Summary.UnclassifiedInScope {
		t.Errorf("stdout says %d undispositioned in scope, verify.toml says unclassified_in_scope = %d — one run, one fact, two answers",
			printed, v.Summary.UnclassifiedInScope)
	}
}

// TestStdoutSaysTheGateIsOpen: the fixture's in-diff survivor carries no
// disposition, so the verdict gate fails. The line a human reads must say so —
// the failure being fixed is a screen that reads all-clear beside a file that
// records the gate open.
func TestStdoutSaysTheGateIsOpen(t *testing.T) {
	lifecycleRepo(t, "gateopen")

	out := runVerifyCapturing(t, "01-gateopen")

	if !strings.Contains(out, "VERDICT GATE IS OPEN") {
		t.Errorf("an undispositioned in-diff survivor did not announce the gate:\n%s", out)
	}
	if strings.Contains(out, "the verdict gate is clear") {
		t.Errorf("stdout reported the gate clear while a survivor was undispositioned:\n%s", out)
	}
	// And it must say what to do about it — a gate with no remedy named is a
	// dead end the reader has to leave the tool to resolve.
	for _, want := range []string{"dross survivor accept", "dross survivor route"} {
		if !strings.Contains(out, want) {
			t.Errorf("the open gate does not name %q:\n%s", want, out)
		}
	}
}

// TestAcceptedAndRoutedStayOutOfTheCount: accepted and routed survivors are
// silent by decision. Folding them into the gate would fail every phase that
// ever accepted one — including every phase in this milestone, which carry
// dozens of the attribution-ceiling acceptances.
func TestAcceptedAndRoutedStayOutOfTheCount(t *testing.T) {
	dir := lifecycleRepo(t, "dispositioned")

	// Accept the in-diff survivor, which is the one the gate counts.
	if err := runCmd(t, Survivor(), "accept", "a.go:3", "--op", "CONDITIONALS_BOUNDARY",
		"--reason", "the fixture's survivor, accepted so the gate has nothing left to count"); err != nil {
		t.Fatalf("survivor accept: %v", err)
	}

	out := runVerifyCapturing(t, "01-dispositioned")

	m := gateCountRE.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("stdout never states the undispositioned-in-scope count:\n%s", out)
	}
	if m[1] != "0" {
		t.Errorf("an accepted survivor still counts against the gate (%s undispositioned):\n%s", m[1], out)
	}
	if !strings.Contains(out, "the verdict gate is clear") {
		t.Errorf("with every in-scope survivor dispositioned, stdout does not say the gate is clear:\n%s", out)
	}

	_, vPath := verify.FilePaths(filepath.Join(dir, ".dross"), "01-dispositioned")
	v, err := verify.LoadVerify(vPath)
	if err != nil {
		t.Fatal(err)
	}
	if v.Summary.UnclassifiedInScope != 0 {
		t.Errorf("verify.toml counts %d unclassified in scope after an acceptance", v.Summary.UnclassifiedInScope)
	}
}

// TestGateLineIsAlwaysPrinted: "the gate is clear" is the fact a reader is
// looking for, and its ABSENCE is not the same statement — a run that printed
// the line only when the gate was open would make a clear gate and a missing
// section look identical.
func TestGateLineIsAlwaysPrinted(t *testing.T) {
	lifecycleRepo(t, "always")
	out := runVerifyCapturing(t, "01-always")
	if !gateCountRE.MatchString(out) && !strings.Contains(out, "the verdict gate is clear") {
		t.Errorf("no gate line at all:\n%s", out)
	}
}
