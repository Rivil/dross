package ship

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// CommentOpts is the input shape for posting a comment to an open PR.
// Mirrors OpenOpts where it overlaps so callers configure them
// identically.
type CommentOpts struct {
	Provider   string // "github" | "forgejo" | "gitea" | "gitlab"
	URL        string // canonical https URL of the repo
	APIBase    string // forgejo/gitea/gitlab: REST API base; ignored for github
	AuthEnv    string // env var holding the token; only forgejo/gitea/gitlab
	AuthScheme string // gitlab: "private-token" (default) | "bearer"
	ProjectID  string // gitlab: numeric project-id override; empty = derive from URL
	PRNumber   int    // PR / MR / issue number to comment on (gitlab: MR iid)
	Body       string // comment body, markdown
}

// PostComment dispatches to the right provider and posts a single
// comment to an existing PR. Used by /dross-review to publish the
// aggregated subagent panel findings as one consolidated comment.
func PostComment(opts CommentOpts) error {
	if opts.Body == "" {
		return errors.New("comment body is empty")
	}
	if opts.PRNumber <= 0 {
		return errors.New("PRNumber must be set")
	}
	switch strings.ToLower(opts.Provider) {
	case "github":
		return postGitHubComment(opts)
	case "forgejo", "gitea":
		return postForgejoComment(opts)
	case "gitlab":
		return postGitLabComment(opts)
	default:
		return fmt.Errorf("unsupported provider %q (expected github | forgejo | gitea | gitlab)", opts.Provider)
	}
}

func postGitHubComment(opts CommentOpts) error {
	args := []string{"pr", "comment", fmt.Sprint(opts.PRNumber), "--body", opts.Body}
	out, err := ghCommand(args...).CombinedOutput()
	if err != nil {
		// Surface the missing-gh case with the original install pointer
		// rather than the raw exec error. Tests override ghCommand so
		// the LookPath check we used to do here doesn't apply uniformly.
		if _, perr := exec.LookPath("gh"); perr != nil {
			return errors.New("github backend needs the `gh` CLI on PATH (https://cli.github.com)")
		}
		return fmt.Errorf("gh pr comment: %w\n%s", err, string(out))
	}
	return nil
}

func postForgejoComment(opts CommentOpts) error {
	if opts.APIBase == "" {
		return errors.New("forgejo backend needs APIBase (set [remote].api_base)")
	}
	if opts.AuthEnv == "" {
		return errors.New("forgejo backend needs AuthEnv (set [remote].auth_env)")
	}
	token := os.Getenv(opts.AuthEnv)
	if token == "" {
		return fmt.Errorf("$%s is not set; run `dross env set %s` in your shell", opts.AuthEnv, opts.AuthEnv)
	}
	owner, repo, err := splitOwnerRepo(opts.URL)
	if err != nil {
		return err
	}
	// Forgejo / Gitea reuses the issues endpoint for PR comments; the
	// number space for issues and PRs is shared.
	endpoint := strings.TrimRight(opts.APIBase, "/") +
		fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, opts.PRNumber)
	if _, err := jsonPost(endpoint, token, map[string]any{
		"body": opts.Body,
	}); err != nil {
		return fmt.Errorf("post comment: %w", err)
	}
	return nil
}

func postGitLabComment(opts CommentOpts) error {
	if opts.APIBase == "" {
		return errors.New("gitlab backend needs APIBase (set [remote].api_base)")
	}
	if opts.AuthEnv == "" {
		return errors.New("gitlab backend needs AuthEnv (set [remote].auth_env)")
	}
	token := os.Getenv(opts.AuthEnv)
	if token == "" {
		return fmt.Errorf("$%s is not set; run `dross env set %s` in your shell", opts.AuthEnv, opts.AuthEnv)
	}
	owner, repo, err := splitOwnerRepo(opts.URL)
	if err != nil {
		return err
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(opts.ProjectID))
	ref := gitlabProjectRef(owner, repo, pid)
	// GitLab MR comments are "notes" on the merge request; PRNumber is the iid.
	endpoint := strings.TrimRight(opts.APIBase, "/") +
		fmt.Sprintf("/projects/%s/merge_requests/%d/notes", ref, opts.PRNumber)
	rb, status, err := gitlabReq("POST", endpoint, opts.AuthScheme, token, map[string]any{"body": opts.Body})
	if err != nil {
		return fmt.Errorf("post note: %w", err)
	}
	if status >= 300 {
		return fmt.Errorf("post note: HTTP %d: %s", status, string(rb))
	}
	return nil
}
