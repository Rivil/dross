package ship

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrMergeStatusUnsupported is returned by PRMerged for providers whose
// authoritative merged-status lookup isn't wired yet (forgejo/gitea/gitlab).
// Callers treat it as "the provider can't answer" and fall back to a
// git-ancestry check rather than blocking. GitHub is the only authoritative
// provider today; the others are deferred (see the phase spec's deferred note).
var ErrMergeStatusUnsupported = errors.New("PR merged-status lookup is not supported for this provider")

// PRMerged reports whether the PR/MR identified by opts.PRNumber has been
// merged, using the provider's authoritative status. It mirrors OpenPR's
// provider dispatch. Only GitHub is wired (via the overridable ghCommand);
// other providers return ErrMergeStatusUnsupported so the caller can fall
// back to a git-ancestry check instead of blocking.
func PRMerged(opts OpenOpts) (bool, error) {
	switch strings.ToLower(opts.Provider) {
	case "github":
		return gitHubPRMerged(opts)
	case "forgejo", "gitea", "gitlab":
		return false, ErrMergeStatusUnsupported
	default:
		return false, fmt.Errorf("unsupported provider %q (expected github | forgejo | gitea | gitlab)", opts.Provider)
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
