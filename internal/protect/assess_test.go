package protect

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// liveRules decodes a rules/branches response body.
func liveRules(t *testing.T, body string) []LiveRule {
	t.Helper()
	var rules []LiveRule
	if err := json.Unmarshal([]byte(body), &rules); err != nil {
		t.Fatal(err)
	}
	return rules
}

// liveRuleset decodes a rulesets/{id} response body.
func liveRuleset(t *testing.T, body string) LiveRuleset {
	t.Helper()
	var rs LiveRuleset
	if err := json.Unmarshal([]byte(body), &rs); err != nil {
		t.Fatal(err)
	}
	return rs
}

const activeNoBypass = `{"id": 1, "name": "dross: main", "enforcement": "active", "bypass_actors": [], "current_user_can_bypass": "never"}`

// appliedMain is what rules/branches returns for a branch under dross's own
// main ruleset: the rules MainRuleset builds, tagged with its ruleset id.
func appliedMain(t *testing.T, contexts ...string) []LiveRule {
	t.Helper()
	rs, err := MainRuleset("main", contexts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(rs.Rules)
	if err != nil {
		t.Fatal(err)
	}
	rules := liveRules(t, string(b))
	for i := range rules {
		rules[i].RulesetID = 1
	}
	return rules
}

// What dross applies reads back with no gap at all.
func TestAppliedMainRulesetHasNoGaps(t *testing.T) {
	want := []string{"test", "shellcheck"}
	if gaps := Assess(appliedMain(t, want...), []LiveRuleset{liveRuleset(t, activeNoBypass)}, want); len(gaps) != 0 {
		t.Errorf("gaps = %v, want none", gaps)
	}
}

// Nothing in force is Unprotected and nothing else: no rules at all, or rules
// only from a ruleset that is evaluating or switched off.
func TestEnforcementTable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rules    []LiveRule
		rulesets []LiveRuleset
	}{
		{"zero rules", nil, nil},
		{"evaluate ruleset", appliedMain(t, "test"), []LiveRuleset{liveRuleset(t, strings.Replace(activeNoBypass, `"active"`, `"evaluate"`, 1))}},
		{"disabled ruleset", appliedMain(t, "test"), []LiveRuleset{liveRuleset(t, strings.Replace(activeNoBypass, `"active"`, `"disabled"`, 1))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Assess(tc.rules, tc.rulesets, []string{"test"})
			if want := []Gap{{Kind: Unprotected}}; !reflect.DeepEqual(got, want) {
				t.Errorf("gaps = %v, want exactly [Unprotected]", got)
			}
		})
	}
}

func TestCheckDiff(t *testing.T) {
	rulesets := []LiveRuleset{liveRuleset(t, activeNoBypass)}

	got := Assess(appliedMain(t, "test"), rulesets, []string{"test", "shellcheck"})
	if want := []Gap{{Kind: MissingCheck, Check: "shellcheck"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("want {test, shellcheck}, required {test}: gaps = %v, want %v", got, want)
	}

	got = Assess(appliedMain(t, "test", "lint"), rulesets, []string{"test"})
	if want := []Gap{{Kind: StaleCheck, Check: "lint"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("required lint with no job: gaps = %v, want %v", got, want)
	}
}

func TestBypassGaps(t *testing.T) {
	for _, tc := range []struct{ name, ruleset string }{
		{"non-empty bypass_actors", `{"id": 1, "name": "dross: main", "enforcement": "active", "bypass_actors": [{"actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "always"}], "current_user_can_bypass": "never"}`},
		{"current user always", `{"id": 1, "name": "dross: main", "enforcement": "active", "bypass_actors": [], "current_user_can_bypass": "always"}`},
		{"current user pull_requests_only", `{"id": 1, "name": "dross: main", "enforcement": "active", "bypass_actors": [], "current_user_can_bypass": "pull_requests_only"}`},
		{"current user exempt", `{"id": 1, "name": "dross: main", "enforcement": "active", "current_user_can_bypass": "exempt"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Assess(appliedMain(t, "test"), []LiveRuleset{liveRuleset(t, tc.ruleset)}, []string{"test"})
			if want := []Gap{{Kind: AdminBypass, Ruleset: "dross: main"}}; !reflect.DeepEqual(got, want) {
				t.Errorf("gaps = %v, want %v", got, want)
			}
		})
	}
}

// GitHub omits bypass_actors from a caller without write access to the
// ruleset. An absent key is an unread list, never an empty one.
func TestUnreadableBypassIsUnknown(t *testing.T) {
	rs := liveRuleset(t, `{"id": 1, "name": "dross: main", "enforcement": "active", "current_user_can_bypass": "never"}`)
	got := Assess(appliedMain(t, "test"), []LiveRuleset{rs}, []string{"test"})
	if want := []Gap{{Kind: BypassUnknown, Ruleset: "dross: main"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("absent bypass_actors: gaps = %v, want %v", got, want)
	}

	got = Assess(appliedMain(t, "test"), nil, []string{"test"})
	if want := []Gap{{Kind: BypassUnknown, Ruleset: "ruleset 1"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("ruleset details missing: gaps = %v, want %v", got, want)
	}
}

// Every gap is reported at once; nothing short-circuits after the first.
func TestMultipleGapsAllReported(t *testing.T) {
	rules := liveRules(t, `[{"type": "non_fast_forward", "ruleset_id": 1}]`)
	rs := liveRuleset(t, `{"id": 1, "name": "legacy", "enforcement": "active", "bypass_actors": [{"actor_id": 5, "actor_type": "RepositoryRole"}], "current_user_can_bypass": "always"}`)
	got := Assess(rules, []LiveRuleset{rs}, []string{"test", "shellcheck"})
	want := []Gap{
		{Kind: Unprotected},
		{Kind: MissingCheck, Check: "shellcheck"},
		{Kind: MissingCheck, Check: "test"},
		{Kind: AdminBypass, Ruleset: "legacy"},
		{Kind: DeletionAllowed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("gaps = %v\nwant %v", got, want)
	}

	rules = liveRules(t, `[{"type": "pull_request", "ruleset_id": 2}]`)
	got = Assess(rules, []LiveRuleset{liveRuleset(t, strings.Replace(activeNoBypass, `"id": 1`, `"id": 2`, 1))}, nil)
	if want := []Gap{{Kind: ForcePushAllowed}, {Kind: DeletionAllowed}}; !reflect.DeepEqual(got, want) {
		t.Errorf("PR-only branch: gaps = %v, want %v", got, want)
	}
}

func TestRequiresPRPredicate(t *testing.T) {
	for _, tc := range []struct {
		types []string
		want  bool
	}{
		{nil, false},
		{[]string{"non_fast_forward"}, false},
		{[]string{"deletion"}, false},
		{[]string{"deletion", "non_fast_forward"}, false},
		{[]string{"pull_request"}, true},
		{[]string{"update"}, true},
		{[]string{"required_status_checks"}, true},
		{[]string{"deletion", "pull_request"}, true},
	} {
		var rules []LiveRule
		for _, typ := range tc.types {
			rules = append(rules, LiveRule{Type: typ})
		}
		if got := RequiresPR(rules); got != tc.want {
			t.Errorf("RequiresPR(%v) = %v, want %v", tc.types, got, tc.want)
		}
	}
}

func TestLiveRuleContexts(t *testing.T) {
	r := liveRules(t, `[{"type": "required_status_checks", "parameters": {"strict_required_status_checks_policy": false, "required_status_checks": [{"context": "test", "integration_id": 15368}, {"context": "lint"}]}}]`)[0]
	if got := r.Contexts(); !reflect.DeepEqual(got, []string{"test", "lint"}) {
		t.Errorf("contexts = %v, want [test lint]", got)
	}
	for _, r := range []LiveRule{
		{Type: "pull_request", Parameters: json.RawMessage(`{"required_status_checks": [{"context": "x"}]}`)},
		{Type: "required_status_checks"},
		{Type: "required_status_checks", Parameters: json.RawMessage(`{"required_status_checks": "garbled"}`)},
	} {
		if got := r.Contexts(); got != nil {
			t.Errorf("%s %s: contexts = %v, want nil", r.Type, r.Parameters, got)
		}
	}
}

// Each gap names itself; doctor appends the fix. BypassUnknown must read as
// unknown, never as an empty list.
func TestGapStrings(t *testing.T) {
	for _, tc := range []struct {
		gap  Gap
		want []string
	}{
		{Gap{Kind: Unprotected}, []string{"unprotected"}},
		{Gap{Kind: MissingCheck, Check: "shellcheck"}, []string{"missing", "shellcheck"}},
		{Gap{Kind: StaleCheck, Check: "lint"}, []string{"lint", "never reports"}},
		{Gap{Kind: AdminBypass, Ruleset: "dross: main"}, []string{"admin bypass allowed", "dross: main"}},
		{Gap{Kind: BypassUnknown, Ruleset: "dross: main"}, []string{"unknown", "dross: main"}},
		{Gap{Kind: ForcePushAllowed}, []string{"force push allowed"}},
		{Gap{Kind: DeletionAllowed}, []string{"deletion allowed"}},
		{Gap{Kind: GapKind(99)}, []string{"unknown gap"}},
	} {
		s := tc.gap.String()
		for _, w := range tc.want {
			if !strings.Contains(s, w) {
				t.Errorf("%+v renders %q, missing %q", tc.gap, s, w)
			}
		}
	}
}
