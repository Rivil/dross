package ship

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Rivil/dross/internal/configenum"
)

// ErrBasePRLookupUnsupported is returned by OpenPRsTargeting for providers
// whose open-PR-by-base query isn't wired yet. Callers announce the skip and
// fall back to the offline record scan rather than blocking — the locked
// dependent_detection decision makes an announced skip a first-class outcome,
// so an unwired backend is a supported shape rather than a gap.
var ErrBasePRLookupUnsupported = errors.New("open-PR-by-base lookup is not supported for this provider")

// BasePR is one open pull request that targets a given base branch — the
// evidence that deleting that branch would orphan somebody's review.
type BasePR struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	HeadRefName string `json:"headRefName"`
}

// OpenPRsTargeting lists the open PRs whose base is `base`. It mirrors
// GetPRStatus's provider dispatch: GitHub answers via the overridable
// ghCommand seam, GitLab answers via its REST API, the still-unwired
// providers return ErrBasePRLookupUnsupported, and an outright unknown
// provider returns a plain error so a misconfiguration stays distinguishable
// from a missing backend.
//
// A lookup failure is always an error, never an empty slice: an empty slice
// reads as "no dependents" and authorizes an irreversible branch delete.
func OpenPRsTargeting(opts OpenOpts, base string) ([]BasePR, error) {
	switch configenum.Normalize(opts.Provider) {
	case "github":
		return gitHubOpenPRsTargeting(base)
	case "gitlab":
		return gitlabOpenMRsTargeting(opts, base)
	case "bitbucket", "forgejo", "gitea":
		return nil, ErrBasePRLookupUnsupported
	default:
		return nil, fmt.Errorf("unsupported provider %q (expected %s)", opts.Provider, configenum.ShipProviders.List())
	}
}

// OpenPRsTargetingFunc is the exported, overridable seam that cmd-package
// callers use (and that cmd-package tests stub) to query open PRs without a
// `gh` binary or network — the unexported ghCommand seam is unreachable from
// package cmd. Production code calls OpenPRsTargetingFunc, not OpenPRsTargeting.
var OpenPRsTargetingFunc = OpenPRsTargeting

func gitHubOpenPRsTargeting(base string) ([]BasePR, error) {
	if base == "" {
		return nil, errors.New("github open-PR lookup needs a base branch")
	}
	out, err := ghCommand("pr", "list", "--base", base, "--state", "open", "--json", "number,title,url,headRefName").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr list --base %s: %w\n%s", base, err, string(out))
	}
	var prs []BasePR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("parse gh pr list --base %s: %w", base, err)
	}
	return prs, nil
}
