package protect

import (
	"errors"
	"sort"
)

// The names dross gives its two rulesets. The apply verb finds an existing
// ruleset by name and updates it in place, so these are its identity on the
// repo: renaming one makes the next apply create a duplicate.
const (
	MainRulesetName      = "dross: main"
	MilestoneRulesetName = "dross: milestones"
)

// MilestoneRef is the ref pattern the milestone ruleset targets.
const MilestoneRef = "refs/heads/milestone/*"

// Ruleset is a repository ruleset as the rulesets REST API takes it
// (POST /repos/{owner}/{repo}/rulesets, PUT …/rulesets/{id}).
type Ruleset struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	Enforcement string `json:"enforcement"`
	// BypassActors is always present and always empty: admin included, no one
	// skips the rules. Never omitempty — a PUT without the key keeps whatever
	// bypass list the ruleset already had, a stale admin bypass included.
	BypassActors []BypassActor `json:"bypass_actors"`
	Conditions   Conditions    `json:"conditions"`
	Rules        []Rule        `json:"rules"`
}

// BypassActor is one entry of a ruleset's bypass list.
type BypassActor struct {
	ActorID    int64  `json:"actor_id"`
	ActorType  string `json:"actor_type"`
	BypassMode string `json:"bypass_mode,omitempty"`
}

// Conditions picks the refs a ruleset applies to.
type Conditions struct {
	RefName RefName `json:"ref_name"`
}

// RefName lists ref patterns a ruleset includes and excludes.
type RefName struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

// Rule is one rule of a ruleset. Parameters is nil for the rules that take
// none (deletion, non_fast_forward).
type Rule struct {
	Type       string `json:"type"`
	Parameters any    `json:"parameters,omitempty"`
}

// PullRequestParams are a pull_request rule's parameters. The first five are
// required by the API.
type PullRequestParams struct {
	DismissStaleReviewsOnPush      bool     `json:"dismiss_stale_reviews_on_push"`
	RequireCodeOwnerReview         bool     `json:"require_code_owner_review"`
	RequireLastPushApproval        bool     `json:"require_last_push_approval"`
	RequiredApprovingReviewCount   int      `json:"required_approving_review_count"`
	RequiredReviewThreadResolution bool     `json:"required_review_thread_resolution"`
	AllowedMergeMethods            []string `json:"allowed_merge_methods"`
}

// StatusCheckParams are a required_status_checks rule's parameters.
type StatusCheckParams struct {
	StrictRequiredStatusChecksPolicy bool          `json:"strict_required_status_checks_policy"`
	RequiredStatusChecks             []StatusCheck `json:"required_status_checks"`
}

// StatusCheck is one required check, by context only. No integration_id: a
// pin to one app would make a check from any other source — a re-dispatched
// run, a renamed app — never satisfy the rule.
type StatusCheck struct {
	Context string `json:"context"`
}

// pullRequest is the pull_request rule both rulesets share. Zero approvals
// (locked decision approvals: GitHub refuses an author's approval of their own
// PR, so any count blocks a solo repo), and merge commits allowed alongside
// squash: milestone PRs land on main as merge commits (locked merge_history),
// phase PRs squash into a milestone, and dross's chore PRs merge as merge
// commits so the local base can fast-forward to them.
func pullRequest() Rule {
	return Rule{Type: "pull_request", Parameters: PullRequestParams{
		RequiredApprovingReviewCount: 0,
		AllowedMergeMethods:          []string{"merge", "squash"},
	}}
}

// MainRuleset builds the "dross: main" ruleset for branch: no deletion, no
// force push, changes only through a PR, and every one of contexts reported
// on the PR's head commit before it merges. contexts are the check contexts
// of the repo's pull_request jobs; an empty set is an error, since a ruleset
// requiring no checks would read as protected while gating nothing.
//
// Not strict (a PR need not be up to date with the branch first): a strict
// policy makes every merge into main re-run CI on a rebased head. No linear
// history and no merge queue: c-4's milestone merges are merge commits.
func MainRuleset(branch string, contexts []string) (Ruleset, error) {
	if branch == "" {
		return Ruleset{}, errors.New("main ruleset: no branch name")
	}
	checks := distinctSorted(contexts)
	if len(checks) == 0 {
		return Ruleset{}, errors.New("main ruleset: no required checks — the repo's pull_request workflows declare no jobs, so a ruleset would gate nothing")
	}
	required := make([]StatusCheck, len(checks))
	for i, c := range checks {
		required[i] = StatusCheck{Context: c}
	}
	return Ruleset{
		Name:         MainRulesetName,
		Target:       "branch",
		Enforcement:  "active",
		BypassActors: []BypassActor{},
		Conditions:   Conditions{RefName: RefName{Include: []string{"refs/heads/" + branch}, Exclude: []string{}}},
		Rules: []Rule{
			{Type: "deletion"},
			{Type: "non_fast_forward"},
			pullRequest(),
			{Type: "required_status_checks", Parameters: StatusCheckParams{
				StrictRequiredStatusChecksPolicy: false,
				RequiredStatusChecks:             required,
			}},
		},
	}, nil
}

// MilestoneRuleset builds the "dross: milestones" ruleset: every
// milestone/* branch takes changes only through a PR. Nothing else — no
// deletion rule, so `dross milestone complete --finalize` can still delete
// the branch, and no required checks, so creating the branch and opening
// phase PRs into it keep working before any CI has run on it.
func MilestoneRuleset() Ruleset {
	return Ruleset{
		Name:         MilestoneRulesetName,
		Target:       "branch",
		Enforcement:  "active",
		BypassActors: []BypassActor{},
		Conditions:   Conditions{RefName: RefName{Include: []string{MilestoneRef}, Exclude: []string{}}},
		Rules:        []Rule{pullRequest()},
	}
}

func distinctSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
