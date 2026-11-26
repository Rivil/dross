package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// The test job's go test keeps a timing history across CI runs (phase
// ci-timing-trend, criteria c-1 and c-6, locked decision history_store): an
// actions/cache/restore step before it fetches the newest test-timing- cache,
// the go test step hands that file to cmd/testsummary as TESTSUMMARY_HISTORY,
// and an actions/cache/save step after it stores the result under
// test-timing-<sha> — on a first-attempt main push only, since a cache key is
// immutable and a re-run's save would collide. YAML has no mutation adapter, so
// a content guard is the regression check — a pure checker driven by synthetic
// workflows, and a live sweep over ci.yml.

const (
	// timingHistoryEnv is the variable cmd/testsummary reads its history path from.
	timingHistoryEnv = "TESTSUMMARY_HISTORY"
	// timingCacheKey is the restore and save key: one cache per commit.
	timingCacheKey = "test-timing-${{ github.sha }}"
	// timingRestoreKeys falls back to the newest cache of any commit.
	timingRestoreKeys = "test-timing-"
	// runnerTempExpr is how the path must open: an expression GitHub expands.
	runnerTempExpr = "${{ runner.temp }}/"
)

var (
	// literalRunnerTemp is the shell spelling, which env: and with: never
	// expand — the action would cache a directory literally named $RUNNER_TEMP.
	literalRunnerTemp = regexp.MustCompile(`\$\{?RUNNER_TEMP`)
	// runAttemptOne is the save gate's first-attempt arm.
	runAttemptOne = regexp.MustCompile(`github\.run_attempt\s*==\s*'?1'?(\s|\)|$)`)
)

// stepRunsGoTest reports whether a step's run: holds a go test command; a
// shell comment line is not one.
func stepRunsGoTest(s wfStep) bool {
	for _, l := range s.run {
		if !strings.HasPrefix(l.text, "#") && goTestCmd.MatchString(l.text) {
			return true
		}
	}
	return false
}

// timingCacheProblems reports every way a workflow breaks the timing-history
// wiring, one problem per break.
func timingCacheProblems(workflow string) []string {
	steps := workflowSteps(workflow)
	testAt, restoreAt, saveAt := -1, -1, -1
	for i, s := range steps {
		if s.job != "test" {
			continue
		}
		switch {
		case testAt < 0 && stepRunsGoTest(s):
			testAt = i
		case restoreAt < 0 && strings.HasPrefix(s.uses, "actions/cache/restore@"):
			restoreAt = i
		case saveAt < 0 && strings.HasPrefix(s.uses, "actions/cache/save@"):
			saveAt = i
		}
	}
	if testAt < 0 {
		return []string{"the test job has no go test step — there is nothing for a timing history to feed"}
	}

	var problems []string
	switch {
	case restoreAt < 0:
		problems = append(problems, "the test job has no actions/cache/restore step — every run starts with no timing history")
	case restoreAt > testAt:
		problems = append(problems, "the actions/cache/restore step runs after go test — the history it fetches arrives too late to read")
	}
	if restoreAt >= 0 {
		with := steps[restoreAt].with
		if with["key"] != timingCacheKey {
			problems = append(problems, fmt.Sprintf("the restore step's key is %q, want %q", with["key"], timingCacheKey))
		}
		if with["restore-keys"] != timingRestoreKeys {
			problems = append(problems, fmt.Sprintf("the restore step's restore-keys is %q, want %q — without the prefix no other commit's history is found", with["restore-keys"], timingRestoreKeys))
		}
	}

	switch {
	case saveAt < 0:
		problems = append(problems, "the test job has no actions/cache/save step — no run's timings are ever kept")
	case saveAt < testAt:
		problems = append(problems, "the actions/cache/save step runs before go test — it saves the history this run has not written yet")
	}
	if saveAt >= 0 {
		s := steps[saveAt]
		if s.with["key"] != timingCacheKey {
			problems = append(problems, fmt.Sprintf("the save step's key is %q, want %q", s.with["key"], timingCacheKey))
		}
		if !strings.Contains(s.ifCond, "always()") {
			problems = append(problems, fmt.Sprintf("the save step's if: %q lacks always() — a red run's timings would be dropped", s.ifCond))
		}
		if !strings.Contains(s.ifCond, "github.event_name == 'push'") || !strings.Contains(s.ifCond, "github.ref == 'refs/heads/main'") || strings.Contains(s.ifCond, "||") {
			problems = append(problems, fmt.Sprintf("the save step's if: %q is not gated to a push on refs/heads/main — history is main's runs only", s.ifCond))
		}
		if !runAttemptOne.MatchString(s.ifCond) {
			problems = append(problems, fmt.Sprintf("the save step's if: %q lacks the github.run_attempt == 1 arm — a re-run's save collides with the immutable key", s.ifCond))
		}
	}

	hist := steps[testAt].env[timingHistoryEnv]
	if hist == "" {
		problems = append(problems, "the test job's go test sets no "+timingHistoryEnv+" — testsummary reads and writes no history")
	}
	paths := []struct{ where, path string }{{"the go test step's " + timingHistoryEnv, hist}}
	if restoreAt >= 0 {
		paths = append(paths, struct{ where, path string }{"the restore step's path", steps[restoreAt].with["path"]})
	}
	if saveAt >= 0 {
		paths = append(paths, struct{ where, path string }{"the save step's path", steps[saveAt].with["path"]})
	}
	for _, p := range paths {
		switch {
		case p.path == "":
		case literalRunnerTemp.MatchString(p.path):
			problems = append(problems, fmt.Sprintf("%s %q spells a literal $RUNNER_TEMP — env: and with: are not shell-expanded; use ${{ runner.temp }}", p.where, p.path))
		case !strings.HasPrefix(p.path, runnerTempExpr):
			problems = append(problems, fmt.Sprintf("%s %q is not under %s", p.where, p.path, runnerTempExpr))
		}
	}
	if hist != "" {
		for _, p := range paths[1:] {
			if p.path != hist {
				problems = append(problems, fmt.Sprintf("%s %q differs from the go test step's %s %q", p.where, p.path, timingHistoryEnv, hist))
			}
		}
	}

	// Only the test job's go test carries the history: mutation-ts runs a
	// narrow selection whose totals would poison every baseline.
	carried := 0
	for i, s := range steps {
		if _, ok := s.env[timingHistoryEnv]; ok {
			carried++
			if i != testAt {
				problems = append(problems, fmt.Sprintf("a step in job %s carries %s in its env: — only the test job's go test keeps the history", s.job, timingHistoryEnv))
			}
		}
		if s.job != "test" && stepRunsGoTest(s) {
			for _, l := range s.run {
				if strings.Contains(l.text, timingHistoryEnv) {
					problems = append(problems, fmt.Sprintf("job %s's go test sets %s in its run: — only the test job's go test keeps the history", s.job, timingHistoryEnv))
					break
				}
			}
		}
	}
	keyLines := 0
	for _, raw := range strings.Split(workflow, "\n") {
		if strings.HasPrefix(strings.TrimSpace(stripYAMLComment(raw)), timingHistoryEnv+":") {
			keyLines++
		}
	}
	if keyLines > carried {
		problems = append(problems, fmt.Sprintf("%s is set in a job- or workflow-level env: — every go test under it would write history", timingHistoryEnv))
	}
	return problems
}

// TestCITimingCacheLive runs the checker over the live workflow.
func TestCITimingCacheLive(t *testing.T) {
	for _, p := range timingCacheProblems(readRepoFile(t, ".github/workflows/ci.yml")) {
		t.Error(".github/workflows/ci.yml: " + p)
	}
}

// The wired fixture, in parts, so a case can drop or reorder whole steps.
const (
	timingHead = `name: ci
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@aaaa  # v4.2.2
`
	timingRestore = `      - name: restore test timing history
        uses: actions/cache/restore@bbbb  # v6.1.0
        with:
          path: ${{ runner.temp }}/test-timing/history.json
          key: test-timing-${{ github.sha }}
          restore-keys: test-timing-
`
	timingGoTest = `      - name: go test
        shell: bash
        env:
          TESTSUMMARY_HISTORY: ${{ runner.temp }}/test-timing/history.json
        run: set -o pipefail; go test -race -count=1 -json ./... | go run ./cmd/testsummary
`
	timingSave = `      - name: save test timing history
        if: always() && github.event_name == 'push' && github.ref == 'refs/heads/main' && github.run_attempt == 1
        uses: actions/cache/save@bbbb  # v6.1.0
        with:
          path: ${{ runner.temp }}/test-timing/history.json
          key: test-timing-${{ github.sha }}
`
	timingMutation = `  mutation-ts:
    runs-on: ubuntu-latest
    env:
      DROSS_REQUIRE_E2E: '1'
    steps:
      - name: stryker
        shell: bash
        run: set -o pipefail; go test -count=1 -json -run 'TestX' ./internal/mutation/ | go run ./cmd/testsummary
`
	wiredTimingWorkflow = timingHead + timingRestore + timingGoTest + timingSave + timingMutation
)

// TestTimingCacheCheckerCatchesEachBreak pins the checker against synthetic
// workflows: the wired one is clean, and each break is reported on its own —
// so the live sweep cannot go green by matching nothing. Each want entry is a
// substring naming one problem, in check order; the count must match exactly.
func TestTimingCacheCheckerCatchesEachBreak(t *testing.T) {
	if p := timingCacheProblems(wiredTimingWorkflow); len(p) != 0 {
		t.Fatalf("the wired fixture reports %q", p)
	}
	edit := func(from, to string) string {
		wf := strings.Replace(wiredTimingWorkflow, from, to, 1)
		if wf == wiredTimingWorkflow {
			t.Fatalf("the fixture edit %q matched nothing", from)
		}
		return wf
	}
	editAll := func(from, to string) string {
		wf := strings.ReplaceAll(wiredTimingWorkflow, from, to)
		if wf == wiredTimingWorkflow {
			t.Fatalf("the fixture edit %q matched nothing", from)
		}
		return wf
	}
	const (
		histLine = "          TESTSUMMARY_HISTORY: ${{ runner.temp }}/test-timing/history.json\n"
		path     = "${{ runner.temp }}/test-timing/history.json"
	)
	for _, tc := range []struct {
		name, wf string
		want     []string
	}{
		{"no go test in the test job", timingHead + timingRestore + timingSave + timingMutation, []string{"no go test step"}},
		{"no restore step", timingHead + timingGoTest + timingSave + timingMutation, []string{"no actions/cache/restore step"}},
		{"restore after go test", timingHead + timingGoTest + timingRestore + timingSave + timingMutation, []string{"restore step runs after go test"}},
		{"restore key per ref", edit("key: test-timing-${{ github.sha }}", "key: test-timing-${{ github.ref }}"), []string{"restore step's key"}},
		{"no restore-keys", edit("          restore-keys: test-timing-\n", ""), []string{"restore-keys"}},
		{"wrong restore-keys", edit("restore-keys: test-timing-", "restore-keys: timing-"), []string{"restore-keys"}},
		{"no save step", timingHead + timingRestore + timingGoTest + timingMutation, []string{"no actions/cache/save step"}},
		{"save before go test", timingHead + timingRestore + timingSave + timingGoTest + timingMutation, []string{"save step runs before go test"}},
		{"save key per run", timingHead + timingRestore + timingGoTest + strings.Replace(timingSave, "github.sha", "github.run_id", 1) + timingMutation, []string{"save step's key"}},
		{"save without always()", edit("if: always() && ", "if: "), []string{"lacks always()"}},
		{"save on pull requests too", edit("github.event_name == 'push' && ", ""), []string{"not gated to a push"}},
		{"save on any branch", edit("github.ref == 'refs/heads/main' && ", ""), []string{"not gated to a push"}},
		{"save gate with an || arm", edit("&& github.ref ==", "|| github.ref =="), []string{"not gated to a push"}},
		{"save on every attempt", edit(" && github.run_attempt == 1", ""), []string{"run_attempt == 1"}},
		{"save on attempt 10", edit("github.run_attempt == 1", "github.run_attempt == 10"), []string{"run_attempt == 1"}},
		{"go test without the history", edit("        env:\n"+histLine, ""), []string{"sets no TESTSUMMARY_HISTORY"}},
		{"restore path drifts", edit("path: "+path, "path: ${{ runner.temp }}/other.json"), []string{"restore step's path \"${{ runner.temp }}/other.json\" differs"}},
		{"save path drifts", timingHead + timingRestore + timingGoTest + strings.Replace(timingSave, "test-timing/history.json", "other.json", 1) + timingMutation, []string{"save step's path \"${{ runner.temp }}/other.json\" differs"}},
		{"literal $RUNNER_TEMP everywhere", editAll("${{ runner.temp }}/test-timing", "$RUNNER_TEMP/test-timing"), []string{
			"go test step's TESTSUMMARY_HISTORY \"$RUNNER_TEMP/test-timing/history.json\" spells a literal $RUNNER_TEMP",
			"restore step's path \"$RUNNER_TEMP/test-timing/history.json\" spells a literal $RUNNER_TEMP",
			"save step's path \"$RUNNER_TEMP/test-timing/history.json\" spells a literal $RUNNER_TEMP",
		}},
		{"literal ${RUNNER_TEMP} in the env alone", edit("TESTSUMMARY_HISTORY: ${{ runner.temp }}", "TESTSUMMARY_HISTORY: ${RUNNER_TEMP}"), []string{
			"go test step's TESTSUMMARY_HISTORY \"${RUNNER_TEMP}/test-timing/history.json\" spells a literal",
			"restore step's path \"${{ runner.temp }}/test-timing/history.json\" differs",
			"save step's path \"${{ runner.temp }}/test-timing/history.json\" differs",
		}},
		{"path outside runner.temp", editAll("${{ runner.temp }}/test-timing", "/tmp/test-timing"), []string{
			"go test step's TESTSUMMARY_HISTORY \"/tmp/test-timing/history.json\" is not under",
			"restore step's path \"/tmp/test-timing/history.json\" is not under",
			"save step's path \"/tmp/test-timing/history.json\" is not under",
		}},
		{"mutation-ts step env carries it", edit("      - name: stryker\n        shell: bash\n", "      - name: stryker\n        shell: bash\n        env:\n          TESTSUMMARY_HISTORY: "+path+"\n"), []string{"job mutation-ts carries TESTSUMMARY_HISTORY in its env:"}},
		{"mutation-ts run sets it", edit("./internal/mutation/ | go run", "./internal/mutation/ | TESTSUMMARY_HISTORY=/tmp/h go run"), []string{"job mutation-ts's go test sets TESTSUMMARY_HISTORY in its run:"}},
		{"mutation-ts job env carries it", edit("      DROSS_REQUIRE_E2E: '1'\n", "      DROSS_REQUIRE_E2E: '1'\n      TESTSUMMARY_HISTORY: "+path+"\n"), []string{"job- or workflow-level env:"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := timingCacheProblems(tc.wf)
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
