package ship

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// OpenOpts is everything OpenPR needs across providers.
type OpenOpts struct {
	Provider   string // "github" | "forgejo" | "gitea" | "gitlab"
	URL        string // canonical https URL of the repo
	APIBase    string // forgejo/gitea/gitlab: base of the REST API; ignored for github
	AuthEnv    string // env var name holding the token; only used for forgejo/gitea/gitlab
	AuthScheme string // gitlab: "private-token" (default) | "bearer"
	ProjectID  string // gitlab: numeric project-id override; empty = derive from URL
	HeadBranch string // e.g. "pr/01-x"
	BaseBranch string // e.g. "main"
	Title      string
	Body       string
	Reviewers  []string
	Draft      bool
}

// OpenResult is the minimal successful response shape.
type OpenResult struct {
	Number int    // PR number on the host
	URL    string // browser URL
}

// OpenPR dispatches to the right backend based on Provider.
func OpenPR(opts OpenOpts) (*OpenResult, error) {
	switch strings.ToLower(opts.Provider) {
	case "github":
		return openGitHubPR(opts)
	case "forgejo", "gitea":
		return openForgejoPR(opts)
	case "gitlab":
		return openGitLabPR(opts)
	default:
		return nil, fmt.Errorf("unsupported provider %q (expected github | forgejo | gitea | gitlab)", opts.Provider)
	}
}

// --- GitHub via gh ---

// ghCommand is overridable from tests.
var ghCommand = func(args ...string) *exec.Cmd { return exec.Command("gh", args...) }

func openGitHubPR(opts OpenOpts) (*OpenResult, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errors.New("github backend needs the `gh` CLI on PATH (https://cli.github.com)")
	}
	args := []string{
		"pr", "create",
		"--title", opts.Title,
		"--body", opts.Body,
		"--head", opts.HeadBranch,
		"--base", opts.BaseBranch,
	}
	if opts.Draft {
		args = append(args, "--draft")
	}
	for _, r := range opts.Reviewers {
		args = append(args, "--reviewer", r)
	}
	out, err := ghCommand(args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr create: %w\n%s", err, string(out))
	}
	prURL := strings.TrimSpace(string(out))
	// gh prints the URL on the last line; pick that to be safe.
	if lines := strings.Split(prURL, "\n"); len(lines) > 0 {
		prURL = strings.TrimSpace(lines[len(lines)-1])
	}
	return &OpenResult{
		Number: parsePRNumber(prURL),
		URL:    prURL,
	}, nil
}

// --- Forgejo / Gitea via REST ---

func openForgejoPR(opts OpenOpts) (*OpenResult, error) {
	if opts.APIBase == "" {
		return nil, errors.New("forgejo backend needs APIBase (set [remote].api_base)")
	}
	if opts.AuthEnv == "" {
		return nil, errors.New("forgejo backend needs AuthEnv (set [remote].auth_env)")
	}
	token := os.Getenv(opts.AuthEnv)
	if token == "" {
		return nil, fmt.Errorf("$%s is not set; run `dross env set %s` in your shell", opts.AuthEnv, opts.AuthEnv)
	}
	owner, repo, err := splitOwnerRepo(opts.URL)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"title": opts.Title,
		"body":  opts.Body,
		"head":  opts.HeadBranch,
		"base":  opts.BaseBranch,
	}
	if opts.Draft {
		// Forgejo / Gitea support draft via the title prefix convention.
		// REST API doesn't accept a "draft" boolean; titles starting with
		// "WIP:" or "Draft:" are treated as drafts in the UI.
		body["title"] = "Draft: " + opts.Title
	}

	endpoint := strings.TrimRight(opts.APIBase, "/") + fmt.Sprintf("/repos/%s/%s/pulls", owner, repo)
	resp, err := jsonPost(endpoint, token, body)
	if err != nil {
		return nil, fmt.Errorf("create PR: %w", err)
	}
	num, _ := resp["number"].(float64)
	htmlURL, _ := resp["html_url"].(string)
	if num == 0 {
		return nil, fmt.Errorf("forgejo response missing number: %v", resp)
	}

	if len(opts.Reviewers) > 0 {
		revEndpoint := strings.TrimRight(opts.APIBase, "/") +
			fmt.Sprintf("/repos/%s/%s/pulls/%d/requested_reviewers", owner, repo, int(num))
		if _, err := jsonPost(revEndpoint, token, map[string]any{
			"reviewers": opts.Reviewers,
		}); err != nil {
			// Don't fail the whole ship for reviewer-assignment trouble — the PR is open.
			return &OpenResult{Number: int(num), URL: htmlURL}, fmt.Errorf("PR opened (#%d) but reviewer request failed: %w", int(num), err)
		}
	}

	return &OpenResult{Number: int(num), URL: htmlURL}, nil
}

// --- GitLab via REST ---

func openGitLabPR(opts OpenOpts) (*OpenResult, error) {
	if opts.APIBase == "" {
		return nil, errors.New("gitlab backend needs APIBase (set [remote].api_base)")
	}
	if opts.AuthEnv == "" {
		return nil, errors.New("gitlab backend needs AuthEnv (set [remote].auth_env)")
	}
	token := os.Getenv(opts.AuthEnv)
	if token == "" {
		return nil, fmt.Errorf("$%s is not set; run `dross env set %s` in your shell", opts.AuthEnv, opts.AuthEnv)
	}
	owner, repo, err := splitOwnerRepo(opts.URL)
	if err != nil {
		return nil, err
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(opts.ProjectID))
	ref := gitlabProjectRef(owner, repo, pid)

	title := opts.Title
	if opts.Draft {
		// GitLab marks a Merge Request as a draft via a "Draft:" title prefix.
		title = "Draft: " + opts.Title
	}
	body := map[string]any{
		"source_branch": opts.HeadBranch,
		"target_branch": opts.BaseBranch,
		"title":         title,
		"description":   opts.Body,
	}
	endpoint := strings.TrimRight(opts.APIBase, "/") + fmt.Sprintf("/projects/%s/merge_requests", ref)
	respBody, status, err := gitlabReq("POST", endpoint, opts.AuthScheme, token, body)
	if err != nil {
		return nil, fmt.Errorf("create MR: %w", err)
	}
	if status >= 300 {
		return nil, fmt.Errorf("create MR: HTTP %d: %s", status, string(respBody))
	}
	var mr struct {
		IID    int    `json:"iid"`
		WebURL string `json:"web_url"`
	}
	_ = json.Unmarshal(respBody, &mr)
	if mr.IID == 0 {
		return nil, fmt.Errorf("gitlab response missing iid: %s", string(respBody))
	}
	result := &OpenResult{Number: mr.IID, URL: mr.WebURL}

	if len(opts.Reviewers) > 0 {
		ids, err := gitlabReviewerIDs(opts.APIBase, opts.AuthScheme, token, opts.Reviewers)
		if err != nil {
			// Non-fatal: the MR is open. Surface the reviewer trouble as a warning.
			return result, fmt.Errorf("MR opened (!%d) but reviewer lookup failed: %w", mr.IID, err)
		}
		if len(ids) > 0 {
			updEndpoint := strings.TrimRight(opts.APIBase, "/") +
				fmt.Sprintf("/projects/%s/merge_requests/%d", ref, mr.IID)
			rb, st, err := gitlabReq("PUT", updEndpoint, opts.AuthScheme, token, map[string]any{"reviewer_ids": ids})
			if err != nil || st >= 300 {
				return result, fmt.Errorf("MR opened (!%d) but reviewer assignment failed (HTTP %d): %v %s", mr.IID, st, err, string(rb))
			}
		}
	}
	return result, nil
}

// gitlabProjectRef returns the GitLab project identifier for the API path.
// A positive numeric projectID wins (the config override); otherwise the
// URL-encoded "owner/repo" path (owner%2Frepo) is used.
func gitlabProjectRef(owner, repo string, projectID int) string {
	if projectID > 0 {
		return strconv.Itoa(projectID)
	}
	return url.PathEscape(owner + "/" + repo)
}

// gitlabAuthHeader sets the GitLab auth header on req per the scheme: "bearer"
// uses Authorization: Bearer; anything else (incl. "" and "private-token") uses
// the PRIVATE-TOKEN header. Exactly one scheme's header is set.
func gitlabAuthHeader(req *http.Request, scheme, token string) {
	if strings.ToLower(scheme) == "bearer" {
		req.Header.Set("Authorization", "Bearer "+token)
		return
	}
	req.Header.Set("PRIVATE-TOKEN", token)
}

// gitlabReq performs a GitLab REST request with the scheme-appropriate auth
// header, returning the raw body and status. body is JSON-encoded when non-nil.
func gitlabReq(method, endpoint, scheme, token string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b := new(bytes.Buffer)
		if err := json.NewEncoder(b).Encode(body); err != nil {
			return nil, 0, err
		}
		buf = b
	}
	req, err := http.NewRequest(method, endpoint, buf)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	gitlabAuthHeader(req, scheme, token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

// gitlabReviewerIDs resolves usernames to numeric GitLab user ids via
// GET /users?username=. An unresolved username is skipped; a transport or
// HTTP error is returned so the caller can warn (reviewer failure is non-fatal).
func gitlabReviewerIDs(apiBase, scheme, token string, usernames []string) ([]int, error) {
	var ids []int
	for _, name := range usernames {
		endpoint := strings.TrimRight(apiBase, "/") + "/users?username=" + url.QueryEscape(name)
		respBody, status, err := gitlabReq("GET", endpoint, scheme, token, nil)
		if err != nil {
			return ids, err
		}
		if status >= 300 {
			return ids, fmt.Errorf("user lookup %q: HTTP %d", name, status)
		}
		var users []struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(respBody, &users); err != nil {
			return ids, fmt.Errorf("user lookup %q: %w", name, err)
		}
		if len(users) > 0 && users[0].ID > 0 {
			ids = append(ids, users[0].ID)
		}
	}
	return ids, nil
}

// --- helpers ---

// splitOwnerRepo parses a canonical https://host/owner/repo URL.
func splitOwnerRepo(repoURL string) (owner, repo string, err error) {
	u, perr := url.Parse(repoURL)
	if perr != nil || u.Host == "" {
		return "", "", fmt.Errorf("bad repo URL %q", repoURL)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("URL %q does not look like /owner/repo", repoURL)
	}
	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	return owner, repo, nil
}

// parsePRNumber extracts the trailing integer from a PR URL like
// https://github.com/o/r/pull/123. Returns 0 on failure.
func parsePRNumber(url string) int {
	idx := strings.LastIndex(url, "/")
	if idx < 0 {
		return 0
	}
	tail := url[idx+1:]
	n := 0
	for _, c := range tail {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// jsonPost POSTs JSON with a token auth header. Returns parsed
// response body (or the raw bytes via "_raw") on success.
func jsonPost(endpoint, token string, body any) (map[string]any, error) {
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(body); err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", endpoint, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "token "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	out := map[string]any{}
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &out)
	}
	return out, nil
}
