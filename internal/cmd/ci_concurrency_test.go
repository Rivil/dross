package cmd

import (
	"fmt"
	"strings"
	"testing"
)

// ci.yml cancels a superseded pull_request run and never a main one (phase
// cmd-test-duration, criterion c-4, locked decision concurrency_scope). YAML has
// no mutation adapter, so a content guard is the regression check — split, as
// setupGoStepProblems is, into a pure checker driven by synthetic workflows and
// a live sweep over the real file.

const (
	// ciConcurrencyPRRef is the group's PR arm: runs of one PR share a group.
	ciConcurrencyPRRef = "github.event_name == 'pull_request' && github.ref"
	// ciConcurrencyCancel cancels in progress on pull_request events only.
	ciConcurrencyCancel = "${{ github.event_name == 'pull_request' }}"
)

// concurrencyProblems reports every way a workflow's concurrency wiring
// breaks the PR-only cancel. Line-based on purpose (the repo carries no YAML
// dependency): an indent-0 `concurrency:` key opens the top-level block, whose
// `group:` and `cancel-in-progress:` are read up to the next indent-0 key; a
// `concurrency:` key at any deeper indent is a job-level override.
func concurrencyProblems(workflow string) []string {
	var (
		problems      []string
		top           []int
		group, cancel string
		inTop         bool
	)
	for i, raw := range strings.Split(workflow, "\n") {
		line := stripYAMLComment(raw)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			inTop = false
		}
		if strings.HasPrefix(trimmed, "concurrency:") {
			if indent == 0 {
				top = append(top, i+1)
				inTop = true
			} else {
				problems = append(problems, fmt.Sprintf("line %d: job-level `concurrency:` — a job's own group escapes the run's, so its PR runs are never superseded", i+1))
			}
			continue
		}
		if !inTop {
			continue
		}
		if v, ok := strings.CutPrefix(trimmed, "group:"); ok {
			group = strings.TrimSpace(v)
		} else if v, ok := strings.CutPrefix(trimmed, "cancel-in-progress:"); ok {
			cancel = strings.TrimSpace(v)
		}
	}
	switch {
	case len(top) == 0:
		return append(problems, "no top-level `concurrency:` block — a newer push to a PR cannot cancel the run it supersedes")
	case len(top) > 1:
		problems = append(problems, fmt.Sprintf("%d top-level `concurrency:` blocks at lines %v; want exactly one", len(top), top))
	}
	if !strings.HasPrefix(group, "ci-") {
		problems = append(problems, fmt.Sprintf("group %q lacks the `ci-` prefix — another workflow's runs could share its group", group))
	}
	if !strings.Contains(group, "github.run_id") {
		problems = append(problems, fmt.Sprintf("group %q has no github.run_id arm — main pushes would share one group, and GitHub replaces a pending run in a shared group even without cancel-in-progress", group))
	}
	if !strings.Contains(group, ciConcurrencyPRRef) {
		problems = append(problems, fmt.Sprintf("group %q does not group PR runs by ref (%s) — a newer push cannot find the run it supersedes", group, ciConcurrencyPRRef))
	}
	if cancel != ciConcurrencyCancel {
		problems = append(problems, fmt.Sprintf("cancel-in-progress is %q; want %s — only PR runs may be cancelled: main auto-releases, and each main commit keeps its own verdict", cancel, ciConcurrencyCancel))
	}
	return problems
}

func TestCIConcurrencyLive(t *testing.T) {
	const rel = ".github/workflows/ci.yml"
	for _, p := range concurrencyProblems(readRepoFile(t, rel)) {
		t.Errorf("%s: %s", rel, p)
	}
}

// TestConcurrencyProblems drives each rejection branch with a workflow that
// must fail, and the correct block with one that must pass. Each want entry is
// a substring naming one problem, in check order; the count must match
// exactly, so a dropped check and a spurious extra one both fail.
func TestConcurrencyProblems(t *testing.T) {
	const (
		noTop    = "no top-level `concurrency:` block"
		twoTop   = "want exactly one"
		jobLevel = "job-level `concurrency:`"
		prefix   = "lacks the `ci-` prefix"
		runID    = "no github.run_id arm"
		prRef    = "does not group PR runs by ref"
		cancelPR = "only PR runs may be cancelled"
	)
	const good = "ci-${{ github.event_name == 'pull_request' && github.ref || github.run_id }}"
	block := func(group, cancel string) string {
		return "concurrency:\n  group: " + group + "\n  cancel-in-progress: " + cancel + "\n"
	}
	const jobs = "jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: go test ./...\n"
	for _, tc := range []struct {
		name, wf string
		want     []string
	}{
		{"the correct block", "name: ci\n" + block(good, ciConcurrencyCancel) + jobs, nil},
		{"trailing comments are not values", "name: ci\nconcurrency:  # why\n  group: " + good + "  # per ref\n  cancel-in-progress: " + ciConcurrencyCancel + "  # PR only\n" + jobs, nil},
		{"cancel-in-progress: true", block(good, "true") + jobs, []string{cancelPR}},
		{"no cancel-in-progress", "concurrency:\n  group: " + good + "\n" + jobs, []string{cancelPR}},
		{"bare ref group", block("ci-${{ github.ref }}", ciConcurrencyCancel) + jobs, []string{runID, prRef}},
		{"no ci- prefix", block("${{ github.event_name == 'pull_request' && github.ref || github.run_id }}", ciConcurrencyCancel) + jobs, []string{prefix}},
		{"job-level only", jobs + "    concurrency:\n      group: " + good + "\n      cancel-in-progress: " + ciConcurrencyCancel + "\n", []string{jobLevel, noTop}},
		{"top-level plus a job-level override", block(good, ciConcurrencyCancel) + jobs + "    concurrency: x\n", []string{jobLevel}},
		{"two top-level blocks", block(good, ciConcurrencyCancel) + block(good, ciConcurrencyCancel) + jobs, []string{twoTop}},
		{"commented-out block", "# concurrency:\n#   group: " + good + "\n" + jobs, []string{noTop}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := concurrencyProblems(tc.wf)
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
