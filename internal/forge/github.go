package forge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// GitHubClient talks to GitHub's REST issues API. GitHub's issues + milestones
// surface is shaped exactly like the forge *Client (issue-number keys, integer
// repo milestones), so board sync is issue-centric and needs no YouTrack-style
// concrete milestone path. It gets its own type only because GitHub adds one
// extra capability the forge backends don't have: attaching a created issue to
// a Projects v2 board via a single GraphQL mutation.
//
// Construct with NewGitHubProjects. forge.NewBoard dispatches provider=github
// here (the forge REST New() still returns ErrNotImplemented for github — that
// is the PR/ship path, separate from board sync).
type GitHubClient struct {
	owner     string
	repo      string
	apiBase   string // REST base, no trailing slash (default https://api.github.com)
	token     string
	authEnv   string // env var name (kept for diagnostic error messages)
	projectID string // Projects v2 node id; "" disables the add-to-board step

	http *http.Client
}

var _ BoardClient = (*GitHubClient)(nil)

// NewGitHubProjects validates config, resolves the token from the environment,
// and returns a ready client. owner/repo come from cfg.Project ("owner/repo").
// A non-empty cfg.BoardID enables the Projects v2 add-to-board step on create.
func NewGitHubProjects(cfg Config) (*GitHubClient, error) {
	if cfg.AuthEnv == "" {
		return nil, fmt.Errorf("github backend needs AuthEnv (set [board].auth_env)")
	}
	if cfg.Project == "" {
		return nil, fmt.Errorf("github backend needs Project (set [board].project to \"owner/repo\")")
	}
	owner, repo, ok := strings.Cut(cfg.Project, "/")
	if !ok || owner == "" || repo == "" {
		return nil, fmt.Errorf("github [board].project %q is not \"owner/repo\"", cfg.Project)
	}
	token := os.Getenv(cfg.AuthEnv)
	if token == "" {
		return nil, fmt.Errorf("$%s is not set; run `dross env set %s` in your shell", cfg.AuthEnv, cfg.AuthEnv)
	}
	apiBase := strings.TrimRight(cfg.APIBase, "/")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	return &GitHubClient{
		owner:     owner,
		repo:      repo,
		apiBase:   apiBase,
		token:     token,
		authEnv:   cfg.AuthEnv,
		projectID: strings.TrimSpace(cfg.BoardID),
		http:      &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// repoPath builds a repo-scoped REST path (e.g. "/issues", "/issues/42",
// "/milestones").
func (c *GitHubClient) repoPath(suffix string) string {
	return c.apiBase + fmt.Sprintf("/repos/%s/%s", c.owner, c.repo) + suffix
}

// --- issues ---

// CreateIssue opens a new issue and returns it. When a Projects v2 board is
// configured, the created issue is additionally added to that board via a
// GraphQL mutation — best-effort, so a failure there warns but does not fail
// the issue create (and a projectless config skips it entirely).
func (c *GitHubClient) CreateIssue(in IssueInput) (*Issue, error) {
	body := map[string]any{"title": in.Title, "body": in.Body}
	if in.Milestone > 0 {
		body["milestone"] = in.Milestone
	}
	if len(in.Labels) > 0 {
		body["labels"] = in.Labels // GitHub takes label names directly, auto-creating any missing
	}
	var raw githubIssue
	if err := c.do("POST", c.repoPath("/issues"), body, &raw); err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}
	if c.projectID != "" && raw.NodeID != "" {
		if err := c.addToProject(raw.NodeID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: issue %d created but adding it to the project board failed: %v\n", raw.Number, err)
		}
	}
	return raw.toIssue(), nil
}

// GetIssue fetches a single issue by its number (as a string key).
func (c *GitHubClient) GetIssue(key string) (*Issue, error) {
	number, err := strconv.Atoi(key)
	if err != nil {
		return nil, fmt.Errorf("invalid github issue id %q (expected a number): %w", key, err)
	}
	var raw githubIssue
	if err := c.do("GET", c.repoPath(fmt.Sprintf("/issues/%d", number)), nil, &raw); err != nil {
		return nil, fmt.Errorf("get issue #%d: %w", number, err)
	}
	return raw.toIssue(), nil
}

// UpdateIssue applies a partial patch addressed by issue number. GitHub accepts
// title/body/state/milestone/labels in a single PATCH — labels are names (no id
// resolution, unlike the Forgejo/Gitea backend).
func (c *GitHubClient) UpdateIssue(key string, patch IssuePatch) (*Issue, error) {
	number, err := strconv.Atoi(key)
	if err != nil {
		return nil, fmt.Errorf("invalid github issue id %q (expected a number): %w", key, err)
	}
	body := map[string]any{}
	if patch.Title != nil {
		body["title"] = *patch.Title
	}
	if patch.Body != nil {
		body["body"] = *patch.Body
	}
	if patch.State != nil {
		body["state"] = *patch.State
	}
	if patch.Milestone != nil {
		body["milestone"] = *patch.Milestone
	}
	if patch.Labels != nil {
		body["labels"] = *patch.Labels
	}
	var raw githubIssue
	if len(body) > 0 {
		if err := c.do("PATCH", c.repoPath(fmt.Sprintf("/issues/%d", number)), body, &raw); err != nil {
			return nil, fmt.Errorf("update issue #%d: %w", number, err)
		}
	}
	return raw.toIssue(), nil
}

// CloseIssue closes an issue via a state PATCH.
func (c *GitHubClient) CloseIssue(key string) error {
	closed := "closed"
	_, err := c.UpdateIssue(key, IssuePatch{State: &closed})
	return err
}

// ListIssues returns issues matching the filter. Pull requests are excluded
// (GitHub's issues endpoint returns both, and each PR carries a pull_request
// object) so inbound triage never surfaces PRs as new work.
func (c *GitHubClient) ListIssues(f IssueFilter) ([]Issue, error) {
	state := f.State
	if state == "" {
		state = "open"
	}
	q := url.Values{}
	q.Set("state", state)
	q.Set("per_page", "50")
	if len(f.Labels) > 0 {
		q.Set("labels", strings.Join(f.Labels, ","))
	}
	var raw []githubIssue
	if err := c.do("GET", c.repoPath("/issues")+"?"+q.Encode(), nil, &raw); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	out := make([]Issue, 0, len(raw))
	for i := range raw {
		if raw[i].PullRequest != nil {
			continue // skip PRs
		}
		out = append(out, *raw[i].toIssue())
	}
	return out, nil
}

// --- milestones ---

// EnsureMilestone returns the (integer) number of the milestone titled `title`
// as a string, creating it if absent. Idempotent. This is the same
// integer-milestone shape the forge backends use, so it slots into the existing
// command-layer milestone path with no concrete branch.
func (c *GitHubClient) EnsureMilestone(title, description string) (string, error) {
	var existing []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	}
	if err := c.do("GET", c.repoPath("/milestones")+"?state=all&per_page=100", nil, &existing); err != nil {
		return "", fmt.Errorf("list milestones: %w", err)
	}
	for _, m := range existing {
		if m.Title == title {
			return strconv.Itoa(m.Number), nil
		}
	}
	var created struct {
		Number int `json:"number"`
	}
	if err := c.do("POST", c.repoPath("/milestones"), map[string]any{
		"title":       title,
		"state":       "open",
		"description": description,
	}, &created); err != nil {
		return "", fmt.Errorf("create milestone %q: %w", title, err)
	}
	return strconv.Itoa(created.Number), nil
}

// --- Projects v2 (GraphQL) ---

// addToProject adds an issue (by its GraphQL node id) to the configured
// Projects v2 board via the addProjectV2ItemById mutation. Idempotent on
// GitHub's side (adding an existing item returns the existing item id).
func (c *GitHubClient) addToProject(contentNodeID string) error {
	const mutation = `mutation($projectId:ID!,$contentId:ID!){addProjectV2ItemById(input:{projectId:$projectId,contentId:$contentId}){item{id}}}`
	reqBody := map[string]any{
		"query": mutation,
		"variables": map[string]any{
			"projectId": c.projectID,
			"contentId": contentNodeID,
		},
	}
	var resp struct {
		Data struct {
			AddProjectV2ItemById struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"addProjectV2ItemById"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	// GraphQL errors ride a 200 response in the `errors` array, so inspect it
	// even on success.
	if err := c.do("POST", c.apiBase+"/graphql", reqBody, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		return fmt.Errorf("graphql: %s", resp.Errors[0].Message)
	}
	return nil
}

// --- wire types ---

// githubIssue is the subset of GitHub's issue shape dross reads. node_id is the
// GraphQL node id used to add the issue to a Projects v2 board.
type githubIssue struct {
	Number      int    `json:"number"`
	NodeID      string `json:"node_id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	State       string `json:"state"`
	HTMLURL     string `json:"html_url"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
}

func (r *githubIssue) toIssue() *Issue {
	iss := &Issue{
		Number: r.Number,
		Key:    strconv.Itoa(r.Number),
		Title:  r.Title,
		Body:   r.Body,
		State:  r.State,
		URL:    r.HTMLURL,
	}
	for _, l := range r.Labels {
		iss.Labels = append(iss.Labels, l.Name)
	}
	if r.Milestone != nil {
		iss.Milestone = r.Milestone.Title
	}
	return iss
}

// --- low-level REST/GraphQL ---

// do performs a token-authenticated JSON request against GitHub's API. If out
// is non-nil and the response has a body, it's decoded into out. Non-2xx
// responses become errors carrying the status and a (truncated) body snippet.
func (c *GitHubClient) do(method, endpoint string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			return err
		}
		rdr = buf
	}
	req, err := http.NewRequest(method, endpoint, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		snippet := string(respBody)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		hint := ""
		switch resp.StatusCode {
		case 401:
			hint = " (check $" + c.authEnv + " — token may be expired or wrong scope)"
		case 403:
			hint = " (token lacks permission, or rate-limited)"
		case 404:
			hint = fmt.Sprintf(" (repo %s/%s or endpoint not found — check [board].project)", c.owner, c.repo)
		}
		return fmt.Errorf("%s %s: HTTP %d%s: %s", method, endpoint, resp.StatusCode, hint, snippet)
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
