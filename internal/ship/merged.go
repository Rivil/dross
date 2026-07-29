package ship

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Rivil/dross/internal/configenum"
)

// ErrMergeStatusUnsupported is returned by PRMerged for providers whose
// authoritative merged-status lookup isn't wired yet (forgejo/gitea/gitlab).
// Callers treat it as "the provider can't answer" and fall back to a
// git-ancestry check rather than blocking. GitHub is the only authoritative
// provider today; the others are deferred (see the phase spec's deferred note).
var ErrMergeStatusUnsupported = errors.New("PR merged-status lookup is not supported for this provider")

// PRMerged reports whether the PR/MR identified by opts.PRNumber has been
// merged, using the provider's authoritative status. It mirrors OpenPR's
// provider dispatch. GitHub (via the overridable ghCommand) and Bitbucket
// answer authoritatively; the rest return ErrMergeStatusUnsupported so the
// caller can fall back to a git-ancestry check instead of blocking.
func PRMerged(opts OpenOpts) (bool, error) {
	switch configenum.Normalize(opts.Provider) {
	case "github":
		return gitHubPRMerged(opts)
	case "bitbucket":
		return bitbucketPRMerged(opts)
	case "forgejo", "gitea", "gitlab":
		return false, ErrMergeStatusUnsupported
	default:
		return false, fmt.Errorf("unsupported provider %q (expected %s)", opts.Provider, configenum.ShipProviders.List())
	}
}

// PRMergedFunc is the exported, overridable seam that cmd-package callers use
// (and that cmd-package tests stub) to check merged status without a `gh`
// binary or network — the unexported ghCommand seam is unreachable from
// package cmd. Production code calls PRMergedFunc, not PRMerged directly.
var PRMergedFunc = PRMerged

// gitHubPRMerged asks `gh pr view <n> --json state,mergedAt` and reports
// merged == (state == "MERGED"). Any lookup or parse failure is returned so
// the caller can fall back to git ancestry rather than trust a stale signal.
func gitHubPRMerged(opts OpenOpts) (bool, error) {
	if opts.PRNumber <= 0 {
		return false, errors.New("github merged-status lookup needs a PR number")
	}
	out, err := ghCommand("pr", "view", strconv.Itoa(opts.PRNumber), "--json", "state,mergedAt").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("gh pr view #%d: %w\n%s", opts.PRNumber, err, string(out))
	}
	var view struct {
		State    string `json:"state"`
		MergedAt string `json:"mergedAt"`
	}
	if err := json.Unmarshal(out, &view); err != nil {
		return false, fmt.Errorf("parse gh pr view #%d: %w", opts.PRNumber, err)
	}
	return strings.EqualFold(view.State, "MERGED"), nil
}

// bitbucketPRMerged reads the PR's authoritative state from Bitbucket Cloud.
//
// The state is one of OPEN, MERGED, DECLINED or SUPERSEDED — so merged is
// state == MERGED, never state != OPEN. A declined or superseded PR is closed
// without ever having landed, and reporting it merged would false-complete a
// phase whose work was thrown away.
func bitbucketPRMerged(opts OpenOpts) (bool, error) {
	if opts.PRNumber <= 0 {
		return false, errors.New("bitbucket merged-status lookup needs a PR number")
	}
	user, token, err := bbCredentials(opts.APIBase, opts.AuthEnv, opts.AuthUser)
	if err != nil {
		return false, err
	}
	workspace, slug, err := bbRepoRef(opts.URL)
	if err != nil {
		return false, err
	}
	endpoint := strings.TrimRight(opts.APIBase, "/") +
		fmt.Sprintf("/repositories/%s/%s/pullrequests/%d", workspace, slug, opts.PRNumber)
	rb, status, err := bbRequest("GET", endpoint, user, token, nil)
	if err != nil {
		return false, fmt.Errorf("get PR #%d: %w", opts.PRNumber, err)
	}
	if status >= 300 {
		return false, fmt.Errorf("get PR #%d: HTTP %d: %s", opts.PRNumber, status, string(rb))
	}
	var pr struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(rb, &pr); err != nil {
		return false, fmt.Errorf("parse bitbucket PR #%d: %w", opts.PRNumber, err)
	}
	return strings.EqualFold(pr.State, "MERGED"), nil
}
