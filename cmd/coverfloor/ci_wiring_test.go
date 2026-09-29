package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The floor is only a guard if CI runs it, over the profile CI's own test run
// wrote, with the flags the self-test proved the checker against. YAML has no
// mutation adapter, so this content check is the regression test: a pure
// checker over workflow text, pinned against synthetic workflows and then run
// over the live one.

// ciStep is one step of the `test` job, as far as the wiring cares.
type ciStep struct {
	run             string
	continueOnError bool
}

var ciJobKey = regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)

// testJobSteps returns the steps of the workflow's `test` job, in order.
// Line-based like internal/cmd's workflow scanner (the repo carries no YAML
// dependency): a job is an indent-2 key under `jobs:`, a step opens at a `- `
// item, and only single-line `run:` values are read — both lines this check
// looks for are single-line.
func testJobSteps(workflow string) []ciStep {
	var steps []ciStep
	inJobs, inTest := false, false
	for _, raw := range strings.Split(workflow, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if indent == 0 {
			inJobs, inTest = trimmed == "jobs:", false
			continue
		}
		if !inJobs {
			continue
		}
		if m := ciJobKey.FindStringSubmatch(raw); m != nil {
			inTest = m[1] == "test"
			continue
		}
		if !inTest {
			continue
		}
		key := trimmed
		if rest, ok := strings.CutPrefix(key, "- "); ok {
			steps = append(steps, ciStep{})
			key = rest
		}
		if len(steps) == 0 {
			continue
		}
		cur := &steps[len(steps)-1]
		switch {
		case strings.HasPrefix(key, "run:"):
			cur.run = strings.TrimSpace(strings.TrimPrefix(key, "run:"))
		case strings.HasPrefix(key, "continue-on-error:"):
			cur.continueOnError = strings.TrimSpace(strings.TrimPrefix(key, "continue-on-error:")) != "false"
		}
	}
	return steps
}

var goTestLine = regexp.MustCompile(`(^|[\s;&|(])go test(\s|$)`)

// unquote strips one layer of shell double or single quotes.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// ciWiringProblems reports every way the test job fails to run the floor over
// its own profile.
func ciWiringProblems(workflow string) []string {
	steps := testJobSteps(workflow)
	testAt, profile := -1, ""
	var problems []string
	for i, s := range steps {
		loc := goTestLine.FindStringIndex(s.run)
		if loc == nil {
			continue
		}
		testAt = i
		cmd, _, _ := strings.Cut(s.run[loc[0]:], "|")
		var flags []string
		for _, f := range strings.Fields(cmd)[2:] {
			switch {
			case strings.HasPrefix(f, "-coverprofile="):
				profile = unquote(strings.TrimPrefix(f, "-coverprofile="))
			case strings.HasPrefix(f, "-coverpkg"):
				problems = append(problems, "the test job's go test carries -coverpkg — the floor measures OWN-package coverage")
			case f == "./...":
			default:
				flags = append(flags, f)
			}
		}
		want := append([]string(nil), fixtureCoverFlags...)
		sort.Strings(flags)
		sort.Strings(want)
		if !reflect.DeepEqual(flags, want) {
			problems = append(problems, "the test job's go test flags "+strings.Join(flags, " ")+" differ from fixtureCoverFlags "+strings.Join(want, " ")+" — the self-test would prove a different run than CI's")
		}
		break
	}
	if testAt < 0 {
		return append(problems, "the test job has no go test step")
	}
	if profile == "" {
		return append(problems, "the test job's go test writes no -coverprofile")
	}
	for _, s := range steps[testAt+1:] {
		rest, ok := strings.CutPrefix(s.run, "go run ./cmd/coverfloor ")
		if !ok {
			continue
		}
		if f := strings.Fields(rest); len(f) != 1 || unquote(f[0]) != profile {
			problems = append(problems, "the coverfloor step reads "+rest+", not the profile go test wrote ("+profile+")")
		}
		if s.continueOnError {
			problems = append(problems, "the coverfloor step carries continue-on-error — a failed floor would not fail CI")
		}
		if strings.Contains(s.run, "|| true") {
			problems = append(problems, "the coverfloor step swallows its exit status with || true")
		}
		return problems
	}
	return append(problems, "no step after the test job's go test runs `go run ./cmd/coverfloor <profile>`")
}

const wiredWorkflow = `name: ci
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: go test
        shell: bash
        run: set -o pipefail; go test -race -count=1 -json -coverprofile="$RUNNER_TEMP/cover.out" ./... | go run ./cmd/testsummary
      - name: coverage floor
        run: go run ./cmd/coverfloor "$RUNNER_TEMP/cover.out"
  other:
    steps:
      - run: go run ./cmd/coverfloor elsewhere.out
`

// TestCIWiringCheckerCatchesEachBreak pins the checker against synthetic
// workflows: the wired one is clean, and each way of unwiring it is caught —
// so the live check below cannot go green by matching nothing.
func TestCIWiringCheckerCatchesEachBreak(t *testing.T) {
	if p := ciWiringProblems(wiredWorkflow); len(p) != 0 {
		t.Fatalf("the wired fixture reports %v", p)
	}
	for _, tc := range []struct {
		name, from, to, want string
	}{
		{"no coverprofile", ` -coverprofile="$RUNNER_TEMP/cover.out"`, "", "writes no -coverprofile"},
		{"coverpkg", "-json ", "-json -coverpkg=./... ", "-coverpkg"},
		{"flags drift", "-race ", "", "differ from fixtureCoverFlags"},
		{"no floor step", "        run: go run ./cmd/coverfloor \"$RUNNER_TEMP/cover.out\"\n", "        run: echo skipped\n", "no step after"},
		{"floor reads another profile", `coverfloor "$RUNNER_TEMP/cover.out"`, `coverfloor other.out`, "not the profile"},
		{"continue-on-error", "      - name: coverage floor\n", "      - name: coverage floor\n        continue-on-error: true\n", "continue-on-error"},
		{"swallowed status", `coverfloor "$RUNNER_TEMP/cover.out"`, `coverfloor "$RUNNER_TEMP/cover.out" || true`, "|| true"},
	} {
		wf := strings.Replace(wiredWorkflow, tc.from, tc.to, 1)
		if wf == wiredWorkflow {
			t.Fatalf("%s: the fixture edit matched nothing", tc.name)
		}
		p := ciWiringProblems(wf)
		if len(p) == 0 || !strings.Contains(strings.Join(p, "\n"), tc.want) {
			t.Errorf("%s: problems = %v, want one mentioning %q", tc.name, p, tc.want)
		}
	}
}

// floorFirstWorkflow runs the floor BEFORE the go test that writes its
// profile — and in another job — which is no floor at all.
const floorFirstWorkflow = `name: ci
jobs:
  test:
    steps:
      - name: coverage floor
        run: go run ./cmd/coverfloor "$RUNNER_TEMP/cover.out"
      - name: go test
        shell: bash
        run: set -o pipefail; go test -race -count=1 -json -coverprofile="$RUNNER_TEMP/cover.out" ./... | go run ./cmd/testsummary
  other:
    steps:
      - run: go run ./cmd/coverfloor "$RUNNER_TEMP/cover.out"
`

// TestCIWiringNeedsTheFloorAfterTheTest: order is part of the wiring.
func TestCIWiringNeedsTheFloorAfterTheTest(t *testing.T) {
	p := ciWiringProblems(floorFirstWorkflow)
	if len(p) == 0 || !strings.Contains(strings.Join(p, "\n"), "no step after") {
		t.Errorf("problems = %v, want the missing later floor step named", p)
	}
}

// TestCoverFloorIsWiredIntoCI runs the checker over the live workflow.
func TestCoverFloorIsWiredIntoCI(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ciWiringProblems(string(b)) {
		t.Error(".github/workflows/ci.yml: " + p)
	}
}
