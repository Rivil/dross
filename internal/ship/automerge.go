package ship

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Rivil/dross/internal/configenum"
)

// ErrAutoMergeUnavailable is AutoMergePR's answer when GitHub won't merge the
// PR on dross's behalf: auto-merge is switched off for the repo, or the PR was
// mergeable at once and the direct merge was refused. The PR is still open;
// the caller reports its URL and leaves the merge to a human. The wrapped text
// carries gh's reason.
var ErrAutoMergeUnavailable = errors.New("auto-merge unavailable")

// AutoMergeResult is what arming auto-merge achieved.
type AutoMergeResult struct {
	// AutoEnabled: auto-merge is armed; GitHub merges the PR when the base
	// branch's required checks pass.
	AutoEnabled bool
	// Merged: the PR is merged already. Either gh merged it outright, or it
	// was mergeable at once and AutoMergePR merged it directly.
	Merged bool
}

// mergeMethods are the gh pr merge method flags AutoMergePR accepts.
var mergeMethods = map[string]bool{"merge": true, "squash": true, "rebase": true}

// AutoMergePR arms GitHub auto-merge on PR n with method ("merge" or
// "squash"): `gh pr merge --auto --<method> -- <n>`. It never passes --admin,
// so the base branch's ruleset still decides when, and whether, the PR lands.
//
// GitHub refuses to arm auto-merge on a PR that is already mergeable ("clean
// status") — on a branch with no required checks, every PR is. Then it merges
// directly with `gh pr merge --<method> -- <n>`, which the server still holds
// to the ruleset. Auto-merge switched off for the repo, or a refused direct
// merge, is ErrAutoMergeUnavailable; any other refusal is a plain error and
// makes no direct-merge attempt.
func AutoMergePR(opts OpenOpts, n int, method string) (AutoMergeResult, error) {
	if p := configenum.Normalize(opts.Provider); p != "github" {
		return AutoMergeResult{}, fmt.Errorf("auto-merge is GitHub only; provider is %q", opts.Provider)
	}
	if n <= 0 {
		return AutoMergeResult{}, errors.New("auto-merge needs a PR number")
	}
	if !mergeMethods[method] {
		return AutoMergeResult{}, fmt.Errorf("auto-merge: unknown merge method %q (want merge, squash or rebase)", method)
	}
	pr := strconv.Itoa(n)
	what := fmt.Sprintf("gh pr merge --auto #%d", n)

	// Flags ahead of the separator, the PR number behind it: cobra's `--` ends
	// flag parsing, so a flag after it would become a positional.
	cmd, err := screenedGH("pr", "merge", "--auto", "--"+method, "--", pr)
	if err != nil {
		return AutoMergeResult{}, err
	}
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err == nil {
		switch {
		case strings.Contains(text, "will be automatically merged"):
			return AutoMergeResult{AutoEnabled: true}, nil
		case strings.Contains(text, "Merged"), strings.Contains(text, "already merged"):
			return AutoMergeResult{Merged: true}, nil
		}
		return AutoMergeResult{}, ghUnparseable(what, out)
	}

	if mentions(text, "clean status") {
		return mergeDirectly(n, method)
	}
	if mentions(text, "auto merge is not allowed", "auto-merge is not allowed") {
		return AutoMergeResult{}, ghUnavailable(what, out, "auto-merge is not allowed for this repository — turn on its allow_auto_merge setting (`dross protect --apply` does)")
	}
	return AutoMergeResult{}, ghFailed(what, err, out)
}

// mergeDirectly merges PR n now. Only reached when GitHub said the PR is
// already mergeable, so the ruleset has nothing left to wait for; the server
// still checks it.
func mergeDirectly(n int, method string) (AutoMergeResult, error) {
	cmd, err := screenedGH("pr", "merge", "--"+method, "--", strconv.Itoa(n))
	if err != nil {
		return AutoMergeResult{}, err
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return AutoMergeResult{}, ghUnavailable(fmt.Sprintf("gh pr merge #%d", n), out,
			fmt.Sprintf("PR #%d was mergeable at once, but GitHub refused the direct merge", n))
	}
	return AutoMergeResult{Merged: true}, nil
}

// ghUnavailable reports a refusal the way ghFailed reports a failure — gh's
// own words on stderr, where the user is already looking — and returns
// ErrAutoMergeUnavailable with fixed prose saying why. gh's output never
// rides the error: it can carry an API response body.
func ghUnavailable(what string, out []byte, why string) error {
	if len(out) > 0 {
		fmt.Fprintf(ghStderr, "%s: gh said:\n%s\n", what, bytes.TrimRight(out, "\n"))
	}
	return fmt.Errorf("%w: %s", ErrAutoMergeUnavailable, why)
}

// mentions reports whether text contains any of phrases (lower-case),
// ignoring case.
func mentions(text string, phrases ...string) bool {
	lower := strings.ToLower(text)
	for _, p := range phrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// AutoMergePRFunc is the overridable seam cmd-package callers use, and
// cmd-package tests stub, to arm auto-merge without gh or a network — the
// unexported ghCommand seam is unreachable from package cmd.
var AutoMergePRFunc = AutoMergePR
