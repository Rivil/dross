package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// validatePrecondPlan scaffolds phase 01-test with plan and runs
// `dross validate`, returning its stdout and error.
func validatePrecondPlan(t *testing.T, plan string) (string, error) {
	t.Helper()
	chdir(t, t.TempDir())
	scaffoldPhaseWithPlan(t, "01-test", plan)
	var out string
	err := runCmdCapturing(t, &out, Validate())
	return out, err
}

const inlineBogusPlan = `[phase]
id = "01-test"

[[task]]
id = "t-1"
wave = 1
title = "x"
files = ["a.go"]
covers = ["c-1"]
preconditions = [{ run = "true", bogus = 1 }]
`

func TestValidateReportsMalformedPreconditions(t *testing.T) {
	for _, c := range []struct {
		name, plan, want string
	}{
		{"empty run", precondPlan(precondTask{id: "t-1", pres: []string{precondRun("")}}),
			" task t-1 precondition 1: run is empty"},
		{"unknown key, header form", precondPlan(precondTask{id: "t-1", pres: []string{precondRun("true", "bogus = 1")}}),
			` task t-1 precondition 1: unknown key "bogus"`},
		{"unknown key, inline form", inlineBogusPlan,
			` task t-1 precondition 1: unknown key "bogus"`},
		{"non-finite bound", precondPlan(precondTask{id: "t-1", pres: []string{precondRun("true", "max = nan")}}),
			" task t-1 precondition 1: max = NaN is not a finite number"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := validatePrecondPlan(t, c.plan)
			if err == nil {
				t.Errorf("validate exited zero:\n%s", out)
			}
			root, rerr := FindRoot()
			if rerr != nil {
				t.Fatal(rerr)
			}
			want := "✗ " + filepath.Join(root, "phases", "01-test", "plan.toml") + c.want
			if !strings.Contains(out, want) {
				t.Errorf("validate output lacks %q:\n%s", want, out)
			}
		})
	}
}

func TestValidateNeverRunsAPrecondition(t *testing.T) {
	out, _ := validatePrecondPlan(t, precondPlan(precondTask{id: "t-1", pres: []string{precondRun("touch marker")}}))
	if markerExists("marker") {
		t.Errorf("validate ran a precondition:\n%s", out)
	}
}

func TestValidateAcceptsAWellFormedPrecondition(t *testing.T) {
	out, err := validatePrecondPlan(t, precondPlan(precondTask{id: "t-1", pres: []string{precondRun("go test ./x | wc -l", "max = 3")}}))
	if err != nil || strings.Contains(out, "✗") {
		t.Errorf("a well-formed precondition did not validate clean (%v):\n%s", err, out)
	}
}
