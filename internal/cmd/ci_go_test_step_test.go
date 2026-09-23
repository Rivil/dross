package cmd

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every CI `go test` pipes its -json stream through cmd/testsummary (phase
// cmd-test-duration, criterion c-3): the step log reads as plain go test output
// and the step summary carries the slowest-tests table, so the next regression
// is diagnosable from the run that caught it. YAML has no mutation adapter, so
// a content guard is the regression check — a pure checker driven by synthetic
// workflows, and a live sweep over the real ones.

// wfStep is one step of a workflow job, as the line scanner sees it.
type wfStep struct {
	job   string
	uses  string
	shell string
	run   []wfLine // the run: value, line by line; a single-line run is one entry
}

type wfLine struct {
	n    int // 1-based line in the workflow
	text string
}

var jobKey = regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)

// workflowSteps walks a workflow by line into its jobs' steps. Line-based on
// purpose (the repo carries no YAML dependency): a job is an indent-2 key
// under the top-level `jobs:`, a step opens at a `- ` item inside it and runs
// until the next line indented at or above that dash, and a `run: |` / `run: >`
// block holds every following line that is blank or indented past its `run:`
// key (the dash, for a `- run: |` item) — kept verbatim, since a `#` there is
// shell, not YAML.
func workflowSteps(workflow string) []wfStep {
	var (
		steps      []wfStep
		cur        *wfStep
		job        string
		inJobs     bool
		stepIndent = -1
		runIndent  int
		inRun      bool
	)
	flush := func() {
		if cur != nil {
			steps = append(steps, *cur)
			cur = nil
		}
	}
	for i, raw := range strings.Split(workflow, "\n") {
		trimmed := strings.TrimSpace(raw)
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if inRun {
			if trimmed == "" || indent > runIndent {
				if trimmed != "" {
					cur.run = append(cur.run, wfLine{i + 1, trimmed})
				}
				continue
			}
			inRun = false
		}
		value := stripYAMLComment(raw)
		key := strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if cur != nil && indent <= stepIndent && !strings.HasPrefix(key, "- ") {
			flush()
		}
		if indent == 0 {
			flush()
			inJobs, job = key == "jobs:", ""
			continue
		}
		if !inJobs {
			continue
		}
		if m := jobKey.FindStringSubmatch(value); m != nil {
			flush()
			job = m[1]
			continue
		}
		if job == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(key, "- "); ok {
			flush()
			cur = &wfStep{job: job}
			stepIndent = indent
			key = rest
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(key, "uses:"):
			cur.uses = strings.TrimSpace(strings.TrimPrefix(key, "uses:"))
		case strings.HasPrefix(key, "shell:"):
			cur.shell = strings.TrimSpace(strings.TrimPrefix(key, "shell:"))
		case strings.HasPrefix(key, "run:"):
			// the run value is read from the raw line: a ` #` in a shell
			// command is not a YAML comment's business here
			_, v, _ := strings.Cut(raw, "run:")
			v = strings.TrimSpace(v)
			if isBlockScalarIndicator(v) {
				inRun, runIndent = true, indent
			} else {
				cur.run = append(cur.run, wfLine{i + 1, v})
			}
		}
	}
	flush()
	return steps
}

// goTestInvocation is one `go test` command in a run: value.
type goTestInvocation struct {
	line  int
	job   string
	shell string
	run   string // the step's whole run text, for a `set -o pipefail` earlier in it
	cmd   string // the command's own line
}

var (
	goTestCmd = regexp.MustCompile(`(^|[\s;&|(])go test(\s|$)`)
	teeCmd    = regexp.MustCompile(`\|\s*tee(\s|$)`)
)

// goTestSteps returns every `go test` command in a workflow's run: steps. A
// shell comment line is not a command, and a `name: go test` is not a run.
func goTestSteps(workflow string) []goTestInvocation {
	var out []goTestInvocation
	for _, s := range workflowSteps(workflow) {
		var texts []string
		for _, l := range s.run {
			texts = append(texts, l.text)
		}
		for _, l := range s.run {
			if strings.HasPrefix(l.text, "#") || !goTestCmd.MatchString(l.text) {
				continue
			}
			out = append(out, goTestInvocation{line: l.n, job: s.job, shell: s.shell, run: strings.Join(texts, "\n"), cmd: l.text})
		}
	}
	return out
}

// goTestArgs splits a go test command line into the go test half's fields and
// what it pipes into.
func goTestArgs(cmd string) (args []string, pipedTo string) {
	loc := goTestCmd.FindStringIndex(cmd)
	test, piped := splitPipe(cmd[loc[0]:])
	return strings.Fields(test), strings.TrimSpace(piped)
}

// splitPipe cuts s at its first unquoted `|`: mutation-ts selects tests with
// -run 'A|B', and that `|` is regex, not shell.
func splitPipe(s string) (before, after string) {
	var quote byte
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '|':
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

// goTestStepProblems reports every way one go test invocation breaks the
// summary wiring — and, in the test job, the coverage_preserved decision.
func goTestStepProblems(inv goTestInvocation) []string {
	var problems []string
	args, pipedTo := goTestArgs(inv.cmd)
	if !strings.Contains(inv.run, "set -o pipefail") && inv.shell != "bash" {
		problems = append(problems, "go test is piped without `set -o pipefail` or `shell: bash` — the step would take testsummary's exit status alone")
	}
	if !hasFlag(args, "-json") {
		problems = append(problems, "go test lacks -json — testsummary has no events to read")
	}
	if !strings.HasPrefix(pipedTo, "go run ./cmd/testsummary") {
		problems = append(problems, fmt.Sprintf("go test pipes into %q, not `go run ./cmd/testsummary` — no plain log, no timing table", pipedTo))
	}
	if inv.job == "test" {
		for _, f := range []string{"-short", "-skip", "-run"} {
			if hasFlag(args, f) {
				problems = append(problems, fmt.Sprintf("the test job's go test carries %s — coverage_preserved: every test runs on every PR", f))
			}
		}
		for _, f := range []string{"-race", "-count=1", "./..."} {
			if !hasFlag(args, f) {
				problems = append(problems, fmt.Sprintf("the test job's go test lacks %s — coverage_preserved: every test runs under -race on every PR", f))
			}
		}
	}
	return problems
}

// TestCIGoTestStepsLive sweeps every workflow: exactly the two known go test
// invocations, both clean, no raw -json kept, and the tool they pipe into
// present.
func TestCIGoTestStepsLive(t *testing.T) {
	root := repoRootFromTest(t)
	workflows, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := map[string]int{}
	for _, wf := range workflows {
		rel, _ := filepath.Rel(root, wf)
		text := readRepoFile(t, rel)
		testJobs := map[string]bool{}
		for _, inv := range goTestSteps(text) {
			jobs[inv.job]++
			testJobs[inv.job] = true
			for _, p := range goTestStepProblems(inv) {
				t.Errorf("%s:%d %s", rel, inv.line, p)
			}
			if teeCmd.MatchString(inv.cmd) {
				t.Errorf("%s:%d go test tees its stream — the raw -json is not retained (timing_surface)", rel, inv.line)
			}
		}
		for _, s := range workflowSteps(text) {
			if testJobs[s.job] && strings.HasPrefix(s.uses, "actions/upload-artifact") {
				t.Errorf("%s: job %s uploads an artifact beside its go test — the raw -json is not retained (timing_surface)", rel, s.job)
			}
		}
	}
	if len(jobs) != 2 || jobs["test"] != 1 || jobs["mutation-ts"] != 1 {
		t.Errorf("go test invocations by job = %v, want exactly one in test and one in mutation-ts", jobs)
	}
	if src := readRepoFile(t, "cmd/testsummary/main.go"); !strings.Contains(src, "package main") || !strings.Contains(src, "func main()") {
		t.Error("cmd/testsummary/main.go is not a main package — every CI go test pipes into it")
	}
}

// TestGoTestStepScanner pins the scanner against an inline fixture so the
// sweep cannot go green by matching nothing.
func TestGoTestStepScanner(t *testing.T) {
	const wf = `name: ci
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      # - run: go test ./commented-yaml/...
      - name: go test
        run: go test -race ./...
      - name: block
        shell: bash
        run: |
          # go test ./commented-shell/...
          set -o pipefail
          go test -count=1 -json ./x/... | go run ./cmd/testsummary
        working-directory: sub
      - name: go vet
        run: go vet ./...
  other:
    steps:
      - uses: actions/checkout@aaaa  # v4
`
	got := goTestSteps(wf)
	if len(got) != 2 {
		t.Fatalf("got %d invocations %+v, want 2", len(got), got)
	}
	if got[0].line != 9 || got[0].job != "test" || got[0].cmd != "go test -race ./..." {
		t.Errorf("first = %+v, want line 9 in job test", got[0])
	}
	if got[1].line != 15 || got[1].shell != "bash" || !strings.Contains(got[1].run, "set -o pipefail") {
		t.Errorf("second = %+v, want line 15, shell bash, pipefail in its run text", got[1])
	}
	steps := workflowSteps(wf)
	if len(steps) != 4 || steps[3].job != "other" || !strings.HasPrefix(steps[3].uses, "actions/checkout") {
		t.Fatalf("steps = %+v, want 4 with the last a checkout in job other", steps)
	}
	if n := len(steps[1].run); n != 3 {
		t.Errorf("the block step's run holds %d lines, want 3 — the working-directory key after it is not run text", n)
	}
}

// TestGoTestStepProblems drives each rejection branch with a workflow that
// must fail and the real shape with one that must pass. Each want entry is a
// substring naming one problem, in check order; the count must match exactly.
func TestGoTestStepProblems(t *testing.T) {
	const (
		pipefail = "without `set -o pipefail` or `shell: bash`"
		noJSON   = "lacks -json"
		noPipe   = "not `go run ./cmd/testsummary`"
		short    = "carries -short"
		skip     = "carries -skip"
		runFlag  = "carries -run"
		noRace   = "lacks -race"
		noCount  = "lacks -count=1"
		noAll    = "lacks ./..."
	)
	step := func(job, shell, run string) string {
		s := "jobs:\n  " + job + ":\n    steps:\n      - name: go test\n"
		if shell != "" {
			s += "        shell: " + shell + "\n"
		}
		return s + "        run: " + run + "\n"
	}
	const good = "set -o pipefail; go test -race -count=1 -json ./... | go run ./cmd/testsummary"
	for _, tc := range []struct {
		name, wf string
		want     []string
	}{
		{"the real test-job shape", step("test", "bash", good), nil},
		{"pipefail alone is enough", step("test", "", good), nil},
		{"shell: bash alone is enough", step("test", "bash", "go test -race -count=1 -json ./... | go run ./cmd/testsummary"), nil},
		{"a narrowed run outside the test job", step("mutation-ts", "", "set -o pipefail; go test -count=1 -json -run 'TestX|TestY' ./internal/mutation/ | go run ./cmd/testsummary"), nil},
		{"no pipefail", step("test", "", "go test -race -count=1 -json ./... | go run ./cmd/testsummary"), []string{pipefail}},
		{"no -json", step("test", "bash", "set -o pipefail; go test -race -count=1 ./... | go run ./cmd/testsummary"), []string{noJSON}},
		{"not piped", step("test", "bash", "go test -race -count=1 -json ./..."), []string{noPipe}},
		{"piped elsewhere", step("test", "bash", "go test -race -count=1 -json ./... | tee out.json"), []string{noPipe}},
		{"-short", step("test", "bash", "go test -short -race -count=1 -json ./... | go run ./cmd/testsummary"), []string{short}},
		{"-skip=", step("test", "bash", "go test -skip=TestSlow -race -count=1 -json ./... | go run ./cmd/testsummary"), []string{skip}},
		{"-run", step("test", "bash", "go test -run TestA -race -count=1 -json ./... | go run ./cmd/testsummary"), []string{runFlag}},
		{"lost -race, -count=1 and ./...", step("test", "bash", "go test -json ./internal/... | go run ./cmd/testsummary"), []string{noRace, noCount, noAll}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invs := goTestSteps(tc.wf)
			if len(invs) != 1 {
				t.Fatalf("fixture yields %d invocations, want 1", len(invs))
			}
			got := goTestStepProblems(invs[0])
			if len(got) != len(tc.want) {
				t.Fatalf("got %d problems %q, want %d matching %q", len(got), got, len(tc.want), tc.want)
			}
			for i, w := range tc.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("problem %d = %q, want it to contain %q", i, got[i], w)
				}
			}
		})
	}
}
