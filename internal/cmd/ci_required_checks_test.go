package cmd

import (
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/protect"
)

// ci.yml is main's whole merge gate (phase main-branch-protection, c-2): every
// job in it is a required check on main's ruleset, and nothing lets a required
// check go green without running. These sweeps read the live workflows; each
// rule's checker is also driven over a fixture that breaks it, so a sweep
// cannot pass by matching nothing.

const ciWorkflow = ".github/workflows/ci.yml"

// liveWorkflows reads every .github/workflows/*.{yml,yaml} in the repo, keyed
// by repo-relative path.
func liveWorkflows(t *testing.T) map[string]string {
	t.Helper()
	root := repoRootFromTest(t)
	out := map[string]string{}
	for _, pattern := range []string{".github/workflows/*.yml", ".github/workflows/*.yaml"} {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			rel, _ := filepath.Rel(root, m)
			out[filepath.ToSlash(rel)] = readRepoFile(t, filepath.ToSlash(rel))
		}
	}
	if len(out) == 0 {
		t.Fatal("no workflows found")
	}
	return out
}

// requiredWorkflows returns the workflows that contribute required checks —
// the ones protect reads pull_request jobs from — mapped to their job ids.
func requiredWorkflows(t *testing.T, workflows map[string]string) map[string][]string {
	t.Helper()
	jobs, err := protect.PullRequestJobs(workflows)
	if err != nil {
		t.Fatalf("protect refuses the live workflows: %v", err)
	}
	out := map[string][]string{}
	for _, j := range jobs {
		out[j.Workflow] = append(out[j.Workflow], j.ID)
	}
	return out
}

// mainRulesetContexts builds the main ruleset from workflows the way
// `dross protect` does and returns the contexts it requires.
func mainRulesetContexts(t *testing.T, workflows map[string]string) []string {
	t.Helper()
	jobs, err := protect.PullRequestJobs(workflows)
	if err != nil {
		t.Fatalf("protect refuses the workflows: %v", err)
	}
	rs, err := protect.MainRuleset("main", protect.Contexts(jobs))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range rs.Rules {
		if p, ok := r.Parameters.(protect.StatusCheckParams); ok {
			for _, c := range p.RequiredStatusChecks {
				out = append(out, c.Context)
			}
		}
	}
	return out
}

// ciJobIDs is this file's own scan of a workflow's job ids: every indent-2
// key under the top-level `jobs:`. Deliberately not protect's parser, so the
// two can disagree.
func ciJobIDs(workflow string) []string {
	var ids []string
	inJobs := false
	for _, raw := range strings.Split(workflow, "\n") {
		if raw == "jobs:" {
			inJobs = true
			continue
		}
		if !inJobs {
			continue
		}
		if raw != "" && raw[0] != ' ' && raw[0] != '#' {
			break
		}
		if m := jobKey.FindStringSubmatch(stripYAMLComment(raw)); m != nil {
			ids = append(ids, m[1])
		}
	}
	return ids
}

// Every ci.yml job is required on main. The ids come from this file's scan of
// ci.yml, the contexts from the ruleset `dross protect` would build.
func TestEveryCIJobIsRequired(t *testing.T) {
	workflows := liveWorkflows(t)
	ids := ciJobIDs(workflows[ciWorkflow])
	if len(ids) < 4 {
		t.Fatalf("scanned %d jobs in %s (%v) — the scan has stopped matching", len(ids), ciWorkflow, ids)
	}
	contexts := mainRulesetContexts(t, workflows)
	for _, id := range ids {
		if !contains(contexts, id) {
			t.Errorf("ci.yml job %q is not a required check (required: %v)", id, contexts)
		}
	}

	// Red-proof: a job appended at the end of ci.yml is seen by both sides —
	// the scan, and the ruleset — so neither can quietly drop the last job.
	extended := map[string]string{}
	for k, v := range workflows {
		extended[k] = v
	}
	extended[ciWorkflow] = strings.TrimRight(workflows[ciWorkflow], "\n") + "\n\n  extra:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n"
	if got := ciJobIDs(extended[ciWorkflow]); len(got) == 0 || got[len(got)-1] != "extra" {
		t.Errorf("the scan does not see a job appended to ci.yml: %v", got)
	}
	if !contains(mainRulesetContexts(t, extended), "extra") {
		t.Error("the main ruleset does not require a job appended to ci.yml")
	}
}

// jobLevelIfs returns "<workflow>/<job>" for every job of workflow that
// carries a job-level `if:`.
func jobLevelIfs(name, workflow string, ids []string) []string {
	var out []string
	for _, id := range ids {
		lines, _, ok := jobLines(workflow, id)
		if !ok {
			continue
		}
		for _, raw := range lines {
			if strings.HasPrefix(stripYAMLComment(raw), "    if:") {
				out = append(out, name+"/"+id)
			}
		}
	}
	return out
}

// A skipped job reports success, so a required check behind a job-level if:
// passes without running.
func TestCIJobsHaveNoJobLevelIf(t *testing.T) {
	workflows := liveWorkflows(t)
	for wf, ids := range requiredWorkflows(t, workflows) {
		for _, hit := range jobLevelIfs(wf, workflows[wf], ids) {
			t.Errorf("%s has a job-level if: — a skipped required check reports success", hit)
		}
	}
	fixture := "on: pull_request\njobs:\n  test:\n    if: github.actor != 'dependabot[bot]'\n    runs-on: x\n"
	if got := jobLevelIfs("f.yml", fixture, []string{"test"}); len(got) != 1 {
		t.Errorf("the checker misses a job-level if: %v", got)
	}
}

// pullRequestTriggerKeys returns the keys under a workflow's block-map
// `pull_request:` trigger, plus "<inline>" when the trigger carries an inline
// value.
func pullRequestTriggerKeys(workflow string) (keys []string, found bool) {
	inOn, inPR := false, false
	for _, raw := range strings.Split(workflow, "\n") {
		line := stripYAMLComment(raw)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		switch {
		case indent == 0:
			inOn, inPR = trimmed == "on:", false
		case inOn && indent == 2:
			inPR = strings.HasPrefix(trimmed, "pull_request:")
			if inPR {
				found = true
				if v := strings.TrimSpace(strings.TrimPrefix(trimmed, "pull_request:")); v != "" {
					keys = append(keys, "<inline>")
				}
			}
		case inPR && indent == 4:
			k, _, _ := strings.Cut(trimmed, ":")
			keys = append(keys, k)
		}
	}
	return keys, found
}

// A filter on ci.yml's pull_request trigger would skip ci.yml on some PRs,
// and a required check that never reports blocks them forever.
func TestCIPullRequestTriggerUnfiltered(t *testing.T) {
	keys, found := pullRequestTriggerKeys(readRepoFile(t, ciWorkflow))
	if !found {
		t.Fatal("ci.yml has no block-map pull_request trigger")
	}
	if len(keys) > 0 {
		t.Errorf("ci.yml's pull_request trigger carries %v — no filter, no types", keys)
	}
	for _, f := range []string{"paths: ['**.go']", "paths-ignore: [docs/**]", "branches: [main]", "branches-ignore: [wip]", "types: [opened]"} {
		fixture := "on:\n  push:\n  pull_request:\n    " + f + "\njobs:\n"
		if keys, _ := pullRequestTriggerKeys(fixture); len(keys) != 1 {
			t.Errorf("the checker misses %q: %v", f, keys)
		}
	}
	if keys, _ := pullRequestTriggerKeys("on:\n  pull_request: {branches: [main]}\n"); len(keys) != 1 {
		t.Errorf("the checker misses an inline filter: %v", keys)
	}
}

// secretRef matches a `secrets.X` or `secrets['X']` reference.
var secretRef = regexp.MustCompile(`secrets(?:\.([A-Za-z0-9_]+)|\[\s*['"]([^'"]+)['"]\s*\])`)

// secretsReadBy returns every secret a workflow references, GITHUB_TOKEN
// excepted.
func secretsReadBy(workflow string) []string {
	var out []string
	for _, m := range secretRef.FindAllStringSubmatch(workflow, -1) {
		name := m[1] + m[2]
		if name != "GITHUB_TOKEN" {
			out = append(out, name)
		}
	}
	return out
}

// Dependabot's PRs run with no repository secrets. A ci.yml job that needs
// one goes red on every bot PR, and with it the required check.
func TestCIReadsNoSecrets(t *testing.T) {
	if got := secretsReadBy(readRepoFile(t, ciWorkflow)); len(got) > 0 {
		t.Errorf("ci.yml reads secrets %v; Dependabot PRs get none, so their required checks would fail", got)
	}
	fixture := "env:\n  A: ${{ secrets.GITHUB_TOKEN }}\n  B: ${{ secrets.NPM_TOKEN }}\n  C: ${{ secrets['SIGNING_KEY'] }}\n"
	if got := secretsReadBy(fixture); strings.Join(got, ",") != "NPM_TOKEN,SIGNING_KEY" {
		t.Errorf("the checker reads %v from the fixture, want [NPM_TOKEN SIGNING_KEY]", got)
	}
}

// hasDispatch reports whether a workflow's on: carries workflow_dispatch, in
// block-map or flow-list form.
func hasDispatch(workflow string) bool {
	inOn := false
	for _, raw := range strings.Split(workflow, "\n") {
		line := stripYAMLComment(raw)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			inOn = trimmed == "on:"
			if v, ok := strings.CutPrefix(trimmed, "on:"); ok && strings.Contains(v, "workflow_dispatch") {
				return true
			}
			continue
		}
		if inOn && indent == 2 && strings.HasPrefix(trimmed, "workflow_dispatch:") {
			return true
		}
	}
	return false
}

// The pin-currency bump PR is opened with GITHUB_TOKEN, which triggers no
// pull_request run; its checks arrive only by dispatching each required
// workflow on the bump branch (c-6).
func TestRequiredChecksAreDispatchable(t *testing.T) {
	workflows := liveWorkflows(t)
	required := requiredWorkflows(t, workflows)
	if len(required) == 0 {
		t.Fatal("no workflow contributes required checks")
	}
	for wf := range required {
		if !hasDispatch(workflows[wf]) {
			t.Errorf("%s contributes required checks but has no workflow_dispatch trigger", wf)
		}
	}
	if hasDispatch("on:\n  pull_request:\n  push:\n") || !hasDispatch("on: [pull_request, workflow_dispatch]\n") {
		t.Error("hasDispatch misreads its fixtures")
	}
}

// dispatchedRun matches a `gh workflow run <file>` line.
var dispatchedRun = regexp.MustCompile(`gh workflow run ([A-Za-z0-9._-]+\.ya?ml)`)

// dispatchProblems compares the workflows that contribute required checks
// with the ones pin-currency.yml dispatches on the bump branch.
func dispatchProblems(t *testing.T, workflows map[string]string) []string {
	t.Helper()
	want := map[string]bool{}
	for wf := range requiredWorkflows(t, workflows) {
		want[path.Base(wf)] = true
	}
	got := map[string]bool{}
	for _, m := range dispatchedRun.FindAllStringSubmatch(workflows[pinCurrencyWorkflow], -1) {
		got[m[1]] = true
	}
	var problems []string
	for wf := range want {
		if !got[wf] {
			problems = append(problems, wf+" contributes required checks, but pin-currency.yml never dispatches it on the bump branch")
		}
	}
	for wf := range got {
		if !want[wf] {
			problems = append(problems, "pin-currency.yml dispatches "+wf+", which contributes no required check")
		}
	}
	sort.Strings(problems)
	return problems
}

func TestPinCurrencyDispatchesEveryRequiredWorkflow(t *testing.T) {
	workflows := liveWorkflows(t)
	for _, p := range dispatchProblems(t, workflows) {
		t.Error(p)
	}

	fixture := map[string]string{}
	for k, v := range workflows {
		fixture[k] = v
	}
	fixture[".github/workflows/lint.yml"] = "on:\n  pull_request:\n  workflow_dispatch:\njobs:\n  lint:\n    runs-on: ubuntu-latest\n"
	if got := dispatchProblems(t, fixture); len(got) != 1 || !strings.Contains(got[0], "lint.yml") {
		t.Errorf("a second required workflow with no dispatch line is not caught: %v", got)
	}
}
