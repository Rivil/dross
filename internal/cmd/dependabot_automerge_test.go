package cmd

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/protect"
)

// Dependabot's minor and patch PRs arm auto-merge and land once main's
// required checks pass; a major waits for a human (phase
// main-branch-protection, c-8). The workflow runs on pull_request_target with
// a write token, so the shape that keeps that safe is pinned here.

const autoMergeWorkflow = ".github/workflows/dependabot-automerge.yml"

// mergeStep returns the automerge job's step that runs `gh pr merge`.
func mergeStep(t *testing.T) []string {
	t.Helper()
	job, _, ok := jobLines(readRepoFile(t, autoMergeWorkflow), "automerge")
	if !ok {
		t.Fatalf("%s has no automerge job", autoMergeWorkflow)
	}
	var found [][]string
	for _, step := range stepBlocks(job) {
		if run, ok := keyValue(step, "run"); ok && strings.Contains(run, "gh pr merge") {
			found = append(found, step)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one step running `gh pr merge`, found %d", len(found))
	}
	return found[0]
}

// A pull_request trigger would make the automerge job a check on the PR's own
// commit — and, through protect, a required one that skips (and so passes) on
// every non-Dependabot PR.
func TestAutoMergeIsNotARequiredCheck(t *testing.T) {
	src := readRepoFile(t, autoMergeWorkflow)
	jobs, err := protect.PullRequestJobs(map[string]string{autoMergeWorkflow: src})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Errorf("%s contributes required checks %v; it must run on pull_request_target only", autoMergeWorkflow, jobs)
	}
	if !strings.Contains("\n"+src, "\non: pull_request_target\n") {
		t.Errorf("%s must trigger on exactly `on: pull_request_target`", autoMergeWorkflow)
	}
}

// pull_request_target runs with a write token; checking out the PR would run
// code its author controls with that token.
func TestAutoMergeNeverChecksOutCode(t *testing.T) {
	for i, raw := range strings.Split(readRepoFile(t, autoMergeWorkflow), "\n") {
		content, _ := splitYAMLComment(raw)
		for _, banned := range []string{"actions/checkout", "git clone", "git fetch", "gh pr checkout", "gh repo clone"} {
			if strings.Contains(content, banned) {
				t.Errorf("%s:%d: %q — this job must never fetch the PR's code", autoMergeWorkflow, i+1, banned)
			}
		}
	}
}

// minorOrPatch is the only disjunct shape the merge step's if: may carry.
var minorOrPatch = regexp.MustCompile(`^steps\.meta\.outputs\.update-type == 'version-update:semver-(minor|patch)'$`)

// The merge step names the update types it merges. A "not major" condition
// would merge anything fetch-metadata can't classify.
func TestAutoMergeSkipsMajor(t *testing.T) {
	cond, ok := keyValue(mergeStep(t), "if")
	if !ok {
		t.Fatal("the merge step has no if: — it would merge every update type, majors included")
	}
	if problems := updateTypeConditionProblems(cond); len(problems) > 0 {
		t.Errorf("merge step if: %q: %s", cond, strings.Join(problems, "; "))
	}

	for _, bad := range []string{
		"steps.meta.outputs.update-type != 'version-update:semver-major'",
		"steps.meta.outputs.update-type == 'version-update:semver-minor' || steps.meta.outputs.update-type == 'version-update:semver-major'",
		"github.event.pull_request.user.login == 'dependabot[bot]'",
		"steps.meta.outputs.update-type == 'version-update:semver-minor'",
		"steps.meta.outputs.update-type == 'version-update:semver-patch'",
		"",
	} {
		if len(updateTypeConditionProblems(bad)) == 0 {
			t.Errorf("the condition check accepts %q", bad)
		}
	}
}

// updateTypeConditionProblems says what is wrong with a merge-step if: — a
// disjunct that isn't a minor/patch test, or minor or patch left unnamed (that
// update type would silently stop merging itself).
func updateTypeConditionProblems(cond string) []string {
	var problems []string
	if strings.TrimSpace(cond) == "" {
		return []string{"empty condition"}
	}
	named := map[string]bool{}
	for _, d := range strings.Split(cond, "||") {
		m := minorOrPatch.FindStringSubmatch(strings.TrimSpace(d))
		if m == nil {
			problems = append(problems, "disjunct "+strings.TrimSpace(d)+" is not an update-type == semver-minor/patch test")
			continue
		}
		named[m[1]] = true
	}
	for _, typ := range []string{"minor", "patch"} {
		if !named[typ] {
			problems = append(problems, "semver-"+typ+" is not named, so those PRs never merge themselves")
		}
	}
	return problems
}

// stepOutput finds the step id an if: reads update-type from.
var stepOutput = regexp.MustCompile(`steps\.([A-Za-z0-9_-]+)\.outputs\.update-type`)

// The merge step's if: reads the fetch-metadata step's output by its id. A
// renamed or missing id leaves the condition always false: no minor or patch
// PR would merge itself, with nothing failing to say so.
func TestAutoMergeReadsFetchMetadata(t *testing.T) {
	cond, _ := keyValue(mergeStep(t), "if")
	var ids []string
	for _, m := range stepOutput.FindAllStringSubmatch(cond, -1) {
		if !slices.Contains(ids, m[1]) {
			ids = append(ids, m[1])
		}
	}
	if len(ids) != 1 {
		t.Fatalf("merge step if: %q reads update-type from steps %v; want exactly one", cond, ids)
	}
	id := ids[0]

	job, _, _ := jobLines(readRepoFile(t, autoMergeWorkflow), "automerge")
	metaAt, mergeAt := -1, -1
	for i, step := range stepBlocks(job) {
		if v, ok := keyValue(step, "id"); ok && v == id {
			metaAt = i
			if uses, _ := keyValue(step, "uses"); !strings.HasPrefix(uses, "dependabot/fetch-metadata@") {
				t.Errorf("step id: %s uses %q, want dependabot/fetch-metadata", id, uses)
			}
		}
		if run, ok := keyValue(step, "run"); ok && strings.Contains(run, "gh pr merge") {
			mergeAt = i
		}
	}
	if metaAt < 0 {
		t.Fatalf("the merge step reads steps.%s, but no step has id: %s — its if: is always false", id, id)
	}
	if metaAt > mergeAt {
		t.Errorf("step id: %s runs after the merge step, so its output is empty when the if: reads it", id)
	}
}

// --auto arms the merge and leaves it to the ruleset; without it gh merges
// now, before any required check has reported. --admin would skip them.
func TestAutoMergeWaitsForChecks(t *testing.T) {
	run, _ := keyValue(mergeStep(t), "run")
	fields := strings.Fields(run)
	if !contains(fields, "--auto") {
		t.Errorf("merge step runs %q without --auto", run)
	}
	if contains(fields, "--admin") {
		t.Errorf("merge step runs %q with --admin, which bypasses the required checks", run)
	}
}

// The job keys on the PR's author, and only the job holds write scopes.
func TestAutoMergeShape(t *testing.T) {
	src := readRepoFile(t, autoMergeWorkflow)
	job, _, ok := jobLines(src, "automerge")
	if !ok {
		t.Fatal("no automerge job")
	}
	if got, _ := keyValue(job, "if"); got != "github.event.pull_request.user.login == 'dependabot[bot]'" {
		t.Errorf("job if: = %q; it must key on the PR author being dependabot[bot], not on github.actor", got)
	}

	top, inline, ok := permissionsAt(strings.Split(src, "\n"), 0)
	if !ok || inline != "" || len(top) != 1 || top["contents"] != "read" {
		t.Errorf("workflow-level permissions = %v (inline %q), want exactly contents: read", top, inline)
	}
	scopes, inline, ok := permissionsAt(job, 4)
	want := map[string]string{"contents": "write", "pull-requests": "write"}
	if !ok || inline != "" || !maps.Equal(scopes, want) {
		t.Errorf("job permissions = %v (inline %q), want exactly %v", scopes, inline, want)
	}
}

// Every Dependabot group stays minor+patch, so a major always arrives as its
// own PR that the merge step's if: leaves alone.
func TestDependabotGroupsExcludeMajor(t *testing.T) {
	types := dependabotGroupUpdateTypes(readRepoFile(t, ".github/dependabot.yml"))
	if len(types) == 0 {
		t.Fatal("no update-types found in dependabot.yml — the sweep cannot pass over an empty set")
	}
	for i, list := range types {
		if contains(list, "major") {
			t.Errorf("group %d's update-types %v include major", i, list)
		}
	}
}

// dependabotGroupUpdateTypes returns each `update-types:` list in a
// dependabot.yml, quotes stripped.
func dependabotGroupUpdateTypes(src string) [][]string {
	var (
		out   [][]string
		at    = -1
		lines = strings.Split(src, "\n")
	)
	for _, raw := range lines {
		content, _ := splitYAMLComment(raw)
		trimmed := strings.TrimSpace(content)
		if trimmed == "" {
			continue
		}
		indent := len(content) - len(strings.TrimLeft(content, " "))
		if trimmed == "update-types:" {
			at = indent
			out = append(out, nil)
			continue
		}
		if at >= 0 {
			if item, ok := strings.CutPrefix(trimmed, "- "); ok && indent >= at {
				out[len(out)-1] = append(out[len(out)-1], strings.Trim(item, `"'`))
				continue
			}
			at = -1
		}
	}
	return out
}

// dependabot.yml used to say no bot PR auto-merges; it must name the workflow
// that now does it.
func TestDependabotConfigNamesAutoMerge(t *testing.T) {
	src := readRepoFile(t, ".github/dependabot.yml")
	if strings.Contains(src, "No auto-merge") {
		t.Error("dependabot.yml still says \"No auto-merge\"")
	}
	if !strings.Contains(src, "dependabot-automerge.yml") {
		t.Error("dependabot.yml does not name dependabot-automerge.yml")
	}
}
