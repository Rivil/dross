package protect

import (
	"encoding/json"
	"sort"
	"strconv"
)

// LiveRule is one rule as GET /repos/{owner}/{repo}/rules/branches/{branch}
// returns it. That endpoint returns active rules only: a ruleset in evaluate
// or disabled enforcement contributes none.
type LiveRule struct {
	Type       string          `json:"type"`
	RulesetID  int64           `json:"ruleset_id"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

// LiveRuleset is the part of GET /repos/{owner}/{repo}/rulesets/{id} that
// decides whether its rules hold for everyone.
type LiveRuleset struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Enforcement string `json:"enforcement"`
	// BypassActors is nil when the response omits the key: GitHub returns it
	// only to a caller with write access to the ruleset, so nil means the
	// bypass list could not be read, never that it is empty.
	BypassActors         *[]BypassActor `json:"bypass_actors"`
	CurrentUserCanBypass string         `json:"current_user_can_bypass"`
}

// Contexts returns the check contexts a required_status_checks rule requires;
// nil for any other rule.
func (r LiveRule) Contexts() []string {
	if r.Type != "required_status_checks" || len(r.Parameters) == 0 {
		return nil
	}
	var p StatusCheckParams
	if json.Unmarshal(r.Parameters, &p) != nil {
		return nil
	}
	out := make([]string, 0, len(p.RequiredStatusChecks))
	for _, c := range p.RequiredStatusChecks {
		out = append(out, c.Context)
	}
	return out
}

// GapKind names one way a branch's live rules fall short of dross's main
// ruleset.
type GapKind int

const (
	// Unprotected: direct pushes to the branch are accepted.
	Unprotected GapKind = iota
	// MissingCheck: a pull_request job's check isn't required.
	MissingCheck
	// StaleCheck: a required check has no job reporting it, so every PR
	// waits on it forever.
	StaleCheck
	// AdminBypass: someone, admin included, can skip the rules.
	AdminBypass
	// BypassUnknown: a ruleset's bypass list couldn't be read.
	BypassUnknown
	// ForcePushAllowed: the branch can be force-pushed.
	ForcePushAllowed
	// DeletionAllowed: the branch can be deleted.
	DeletionAllowed
)

// Gap is one shortfall. Check names the context for MissingCheck and
// StaleCheck; Ruleset names the ruleset for AdminBypass and BypassUnknown.
type Gap struct {
	Kind    GapKind
	Check   string
	Ruleset string
}

func (g Gap) String() string {
	switch g.Kind {
	case Unprotected:
		return "unprotected: direct pushes are accepted"
	case MissingCheck:
		return "required check missing: " + g.Check
	case StaleCheck:
		return "required check " + g.Check + " has no pull_request job, so it never reports and every PR waits on it"
	case AdminBypass:
		return "admin bypass allowed (" + g.Ruleset + ")"
	case BypassUnknown:
		return "bypass list unknown (" + g.Ruleset + "): it is returned only to a caller with write access to the ruleset"
	case ForcePushAllowed:
		return "force push allowed"
	case DeletionAllowed:
		return "deletion allowed"
	}
	return "unknown gap"
}

// pushRefusers are the rule types that refuse a direct push to an existing
// branch: a PR is required, updates are restricted, or the pushed commit must
// already carry passing checks — which a freshly pushed commit never does.
var pushRefusers = map[string]bool{"pull_request": true, "update": true, "required_status_checks": true}

// RequiresPR reports whether rules refuse a direct push to the branch, so
// changes have to arrive through a PR. non_fast_forward and deletion alone
// still accept a plain push.
func RequiresPR(rules []LiveRule) bool {
	for _, r := range rules {
		if pushRefusers[r.Type] {
			return true
		}
	}
	return false
}

// Assess compares a branch's live rules with what dross's main ruleset
// requires and returns every gap, in a fixed order. want are the check
// contexts of the repo's pull_request jobs. rulesets are the details of the
// rulesets rules came from; a rule from a ruleset in evaluate or disabled
// enforcement doesn't count, and a rule whose ruleset is missing from
// rulesets counts but leaves its bypass list unknown.
//
// A branch with no rule in force reports Unprotected alone: with nothing
// configured, every other gap is the same fact restated.
func Assess(rules []LiveRule, rulesets []LiveRuleset, want []string) []Gap {
	byID := map[int64]LiveRuleset{}
	for _, rs := range rulesets {
		byID[rs.ID] = rs
	}
	var live []LiveRule
	for _, r := range rules {
		if rs, known := byID[r.RulesetID]; known && rs.Enforcement != "active" {
			continue
		}
		live = append(live, r)
	}
	if len(live) == 0 {
		return []Gap{{Kind: Unprotected}}
	}

	var gaps []Gap
	if !RequiresPR(live) {
		gaps = append(gaps, Gap{Kind: Unprotected})
	}

	required := map[string]bool{}
	has := map[string]bool{}
	for _, r := range live {
		has[r.Type] = true
		for _, c := range r.Contexts() {
			required[c] = true
		}
	}
	wanted := map[string]bool{}
	for _, c := range distinctSorted(want) {
		wanted[c] = true
		if !required[c] {
			gaps = append(gaps, Gap{Kind: MissingCheck, Check: c})
		}
	}
	for _, c := range sortedKeys(required) {
		if !wanted[c] {
			gaps = append(gaps, Gap{Kind: StaleCheck, Check: c})
		}
	}

	gaps = append(gaps, bypassGaps(live, byID)...)

	if !has["non_fast_forward"] {
		gaps = append(gaps, Gap{Kind: ForcePushAllowed})
	}
	if !has["deletion"] {
		gaps = append(gaps, Gap{Kind: DeletionAllowed})
	}
	return gaps
}

// bypassGaps reports, per ruleset that contributes a rule in force, whether
// anyone can skip it (AdminBypass) or whether that can't be told
// (BypassUnknown). AdminBypass gaps come before BypassUnknown ones.
func bypassGaps(live []LiveRule, byID map[int64]LiveRuleset) []Gap {
	var ids []int64
	seen := map[int64]bool{}
	for _, r := range live {
		if !seen[r.RulesetID] {
			seen[r.RulesetID] = true
			ids = append(ids, r.RulesetID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	var bypass, unknown []Gap
	for _, id := range ids {
		rs, known := byID[id]
		if !known {
			unknown = append(unknown, Gap{Kind: BypassUnknown, Ruleset: "ruleset " + strconv.FormatInt(id, 10)})
			continue
		}
		switch {
		case rs.CurrentUserCanBypass == "always", rs.CurrentUserCanBypass == "pull_requests_only", rs.CurrentUserCanBypass == "exempt",
			rs.BypassActors != nil && len(*rs.BypassActors) > 0:
			bypass = append(bypass, Gap{Kind: AdminBypass, Ruleset: rs.Name})
		case rs.BypassActors == nil:
			unknown = append(unknown, Gap{Kind: BypassUnknown, Ruleset: rs.Name})
		}
	}
	return append(bypass, unknown...)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
