package protect

import (
	"encoding/json"
	"path"
	"reflect"
	"strings"
	"testing"
)

// wire marshals a ruleset and decodes it back generically, so every assertion
// below reads what the rulesets API would receive, not the Go struct.
func wire(t *testing.T, rs Ruleset) map[string]any {
	t.Helper()
	b, err := json.Marshal(rs)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// rulesOf returns a wire ruleset's rules keyed by type.
func rulesOf(t *testing.T, m map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, r := range m["rules"].([]any) {
		rule := r.(map[string]any)
		typ := rule["type"].(string)
		if _, dup := out[typ]; dup {
			t.Errorf("rule %q appears twice", typ)
		}
		params, _ := rule["parameters"].(map[string]any)
		out[typ] = params
	}
	return out
}

func mainWire(t *testing.T) map[string]any {
	t.Helper()
	rs, err := MainRuleset("main", []string{"test", "shellcheck", "mutation-ts", "goreleaser-check"})
	if err != nil {
		t.Fatal(err)
	}
	return wire(t, rs)
}

func bothWire(t *testing.T) map[string]map[string]any {
	t.Helper()
	return map[string]map[string]any{"main": mainWire(t), "milestones": wire(t, MilestoneRuleset())}
}

func strs(v any) []string {
	var out []string
	for _, s := range v.([]any) {
		out = append(out, s.(string))
	}
	return out
}

// An omitted or null bypass_actors lets a PUT keep the ruleset's existing
// bypass list, a stale admin bypass included. It must reach the API as [].
func TestBypassActorsExplicitlyEmpty(t *testing.T) {
	for name, m := range bothWire(t) {
		v, ok := m["bypass_actors"]
		if !ok {
			t.Errorf("%s: bypass_actors is omitted", name)
			continue
		}
		list, isList := v.([]any)
		if !isList || len(list) != 0 {
			t.Errorf("%s: bypass_actors = %#v, want []", name, v)
		}
	}
}

func TestRulesetsAreActiveBranchRulesets(t *testing.T) {
	want := map[string]string{"main": MainRulesetName, "milestones": MilestoneRulesetName}
	for name, m := range bothWire(t) {
		if m["name"] != want[name] || m["target"] != "branch" || m["enforcement"] != "active" {
			t.Errorf("%s: name/target/enforcement = %v/%v/%v, want %s/branch/active", name, m["name"], m["target"], m["enforcement"], want[name])
		}
	}
}

// c-4: milestone PRs land on main as merge commits, so main may require
// neither linear history nor a merge queue, and must allow merge commits.
func TestMainRulesetKeepsMergeCommits(t *testing.T) {
	rules := rulesOf(t, mainWire(t))
	for _, banned := range []string{"required_linear_history", "merge_queue"} {
		if _, ok := rules[banned]; ok {
			t.Errorf("main ruleset carries %s", banned)
		}
	}
	pr, ok := rules["pull_request"]
	if !ok {
		t.Fatal("main ruleset has no pull_request rule")
	}
	if got := strs(pr["allowed_merge_methods"]); !reflect.DeepEqual(got, []string{"merge", "squash"}) {
		t.Errorf("allowed_merge_methods = %v, want exactly [merge squash]", got)
	}
}

// c-7: main can't be force-pushed or deleted.
func TestMainRulesetBlocksForcePushAndDelete(t *testing.T) {
	rules := rulesOf(t, mainWire(t))
	for _, want := range []string{"deletion", "non_fast_forward", "pull_request", "required_status_checks"} {
		if _, ok := rules[want]; !ok {
			t.Errorf("main ruleset has no %s rule", want)
		}
	}
}

// Locked decision approvals: zero required reviews on BOTH rulesets. Main's
// checks are not strict and pin no integration.
func TestRulesetParams(t *testing.T) {
	for name, m := range bothWire(t) {
		pr := rulesOf(t, m)["pull_request"]
		if pr == nil {
			t.Errorf("%s: no pull_request rule", name)
			continue
		}
		if n, ok := pr["required_approving_review_count"].(float64); !ok || n != 0 {
			t.Errorf("%s: required_approving_review_count = %#v, want 0", name, pr["required_approving_review_count"])
		}
		for _, key := range []string{"dismiss_stale_reviews_on_push", "require_code_owner_review", "require_last_push_approval", "required_review_thread_resolution",
			// GitHub defaults this one to true when it is omitted; an
			// unattributed commit would then need an approval a solo
			// author can't give.
			"require_extra_approval_for_unattributed_changes"} {
			if v, ok := pr[key].(bool); !ok || v {
				t.Errorf("%s: %s = %#v, want present and false (the API requires it)", name, key, pr[key])
			}
		}
	}

	checks := rulesOf(t, mainWire(t))["required_status_checks"]
	if v, ok := checks["strict_required_status_checks_policy"].(bool); !ok || v {
		t.Errorf("strict_required_status_checks_policy = %#v, want false", checks["strict_required_status_checks_policy"])
	}
	var contexts []string
	for _, c := range checks["required_status_checks"].([]any) {
		check := c.(map[string]any)
		if _, pinned := check["integration_id"]; pinned {
			t.Errorf("check %v pins an integration_id", check)
		}
		contexts = append(contexts, check["context"].(string))
	}
	if want := []string{"goreleaser-check", "mutation-ts", "shellcheck", "test"}; !reflect.DeepEqual(contexts, want) {
		t.Errorf("required contexts = %v, want %v", contexts, want)
	}
}

// milestone/* receives phase PRs (squash-merged) and dross's chore PRs
// (merged as merge commits); a ruleset allowing only one blocks the other.
func TestMilestoneRulesetAllowsPhaseAndChoreMerges(t *testing.T) {
	pr := rulesOf(t, wire(t, MilestoneRuleset()))["pull_request"]
	if pr == nil {
		t.Fatal("milestone ruleset has no pull_request rule")
	}
	got := strs(pr["allowed_merge_methods"])
	for _, want := range []string{"squash", "merge"} {
		if !contains(got, want) {
			t.Errorf("allowed_merge_methods = %v, missing %q", got, want)
		}
	}
}

// `dross milestone complete --finalize` deletes the branch, and the branch is
// created before any CI has run on it: the milestone ruleset may carry
// neither a deletion rule nor required checks — only the PR requirement.
func TestMilestoneRulesetAllowsFinalizeDelete(t *testing.T) {
	rules := rulesOf(t, wire(t, MilestoneRuleset()))
	for typ := range rules {
		if typ != "pull_request" {
			t.Errorf("milestone ruleset carries a %s rule; want pull_request only", typ)
		}
	}
	if _, ok := rules["pull_request"]; !ok {
		t.Error("milestone ruleset has no pull_request rule (c-10)")
	}
}

func TestMainRulesetBuilder(t *testing.T) {
	if _, err := MainRuleset("main", nil); err == nil {
		t.Error("MainRuleset with no contexts must be an error, not a ruleset gating nothing")
	}
	if _, err := MainRuleset("main", []string{""}); err == nil {
		t.Error("MainRuleset with only an empty context must be an error")
	}
	if _, err := MainRuleset("", []string{"test"}); err == nil {
		t.Error("MainRuleset with no branch must be an error")
	}
	rs, err := MainRuleset("trunk", []string{"test", "test"})
	if err != nil {
		t.Fatal(err)
	}
	m := wire(t, rs)
	inc := strs(m["conditions"].(map[string]any)["ref_name"].(map[string]any)["include"])
	if !reflect.DeepEqual(inc, []string{"refs/heads/trunk"}) {
		t.Errorf("include = %v, want [refs/heads/trunk]", inc)
	}
	if n := len(rulesOf(t, m)["required_status_checks"]["required_status_checks"].([]any)); n != 1 {
		t.Errorf("duplicate contexts must collapse, got %d checks", n)
	}
}

// release.yml pushes tags, and Dependabot and the pin-currency bot push their
// own branches: neither ruleset may reach those refs.
func TestRulesetIncludesStayOffTagsAndBotBranches(t *testing.T) {
	outside := []string{
		"refs/tags/v1.7.20",
		"refs/heads/dependabot/go_modules/golang.org/x/mod-0.42.0",
		"refs/heads/dependabot/github_actions/actions/checkout-5",
		"refs/heads/pin-currency/bump",
		"refs/heads/phase/main-branch-protection",
		"refs/heads/dross-chores/main",
		"refs/heads/dross-chores/milestone-v1.7",
	}
	for name, m := range bothWire(t) {
		ref := m["conditions"].(map[string]any)["ref_name"].(map[string]any)
		inc := strs(ref["include"])
		if len(inc) != 1 {
			t.Errorf("%s: include = %v, want exactly one pattern", name, inc)
		}
		if ex, ok := ref["exclude"].([]any); !ok || len(ex) != 0 {
			t.Errorf("%s: exclude = %#v, want []", name, ref["exclude"])
		}
		for _, pattern := range inc {
			if strings.HasPrefix(pattern, "~") || strings.HasPrefix(pattern, "refs/tags/") || strings.Contains(pattern, "**") {
				t.Errorf("%s: include %q reaches past its branch", name, pattern)
			}
			for _, r := range outside {
				if ok, _ := path.Match(pattern, r); ok {
					t.Errorf("%s: include %q matches %s", name, pattern, r)
				}
			}
		}
	}
	if ok, _ := path.Match(MilestoneRef, "refs/heads/milestone/v1.7"); !ok {
		t.Errorf("%s does not match refs/heads/milestone/v1.7", MilestoneRef)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
