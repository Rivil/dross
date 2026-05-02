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
	"strings"
	"time"
)

// OpenOpts is everything OpenPR needs across providers.
type OpenOpts struct {
	Provider   string   // "github" | "forgejo" | "gitea"
	URL        string   // canonical https URL of the repo
	APIBase    string   // forgejo/gitea: base of the REST API; ignored for github
	AuthEnv    string   // env var name holding the token; only used for forgejo/gitea
	HeadBranch string   // e.g. "pr/01-x"
	BaseBranch string   // e.g. "main"
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
	default:
		return nil, fmt.Errorf("unsupported provider %q (expected github | forgejo | gitea)", opts.Provider)
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
