// Package forge talks to a repository's issue tracker so dross can mirror
// its planning artefacts onto a board: milestones, phase issues (with a
// task checklist), and standalone quick-task issues.
//
// It mirrors internal/ship's provider-dispatch shape (forgejo | gitea |
// github) but covers issues/milestones/labels instead of pull requests.
// Only the Forgejo/Gitea REST backend is implemented today; github methods
// return ErrNotImplemented until someone wires `gh issue`.
package forge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ErrNotImplemented is returned by every Client method when the configured
// provider has no board backend yet (currently: github).
var ErrNotImplemented = errors.New("issue-board sync is not implemented for this provider yet (forgejo/gitea only)")

// defaultLabelColor is applied to dross-created labels. Users can recolour
// them in the board UI; the value only matters at creation time.
const defaultLabelColor = "#7057ff"

// Client talks to one repo's issue tracker. Construct with New.
type Client struct {
	owner   string
	repo    string
	apiBase string
	token   string

	http     *http.Client
	labelIDs map[string]int // name -> id cache, lazily populated
}

// Config is the subset of [remote] settings the forge client needs. It maps
// 1:1 onto project.toml's Remote so callers can pass them straight through.
type Config struct {
	Provider string // forgejo | gitea | github
	URL      string // canonical https URL of the repo
	APIBase  string // forgejo/gitea REST base (e.g. https://forge/api/v1)
	AuthEnv  string // env var name holding the token (never the value)
}

// New validates config, resolves the token from the environment, and returns
// a ready Client. It errors early on the same conditions the ship backend
// checks: missing APIBase/AuthEnv, unset token, unparseable repo URL.
func New(cfg Config) (*Client, error) {
	switch strings.ToLower(cfg.Provider) {
	case "forgejo", "gitea":
		// supported below
	case "github":
		return nil, ErrNotImplemented
	default:
		return nil, fmt.Errorf("unsupported provider %q (expected forgejo | gitea)", cfg.Provider)
	}
	if cfg.APIBase == "" {
		return nil, errors.New("forgejo backend needs APIBase (set [remote].api_base)")
	}
	if cfg.AuthEnv == "" {
		return nil, errors.New("forgejo backend needs AuthEnv (set [remote].auth_env)")
	}
	token := os.Getenv(cfg.AuthEnv)
	if token == "" {
		return nil, fmt.Errorf("$%s is not set; run `dross env set %s` in your shell", cfg.AuthEnv, cfg.AuthEnv)
	}
	owner, repo, err := splitOwnerRepo(cfg.URL)
	if err != nil {
		return nil, err
	}
	return &Client{
		owner:    owner,
		repo:     repo,
		apiBase:  strings.TrimRight(cfg.APIBase, "/"),
		token:    token,
		http:     &http.Client{Timeout: 30 * time.Second},
		labelIDs: map[string]int{},
	}, nil
}

// --- public types ---

// Issue is the minimal shape dross cares about across operations.
type Issue struct {
	Number    int
	Title     string
	Body      string
	State     string // "open" | "closed"
	Labels    []string
	Milestone string // milestone title, "" if none
	URL       string // html_url
}

// IssueInput is the create payload. Labels are names; missing ones are
// created on the fly. Milestone is a milestone id (0 = unassigned).
type IssueInput struct {
	Title     string
	Body      string
	Labels    []string
	Milestone int
}

// IssuePatch is a partial update. Nil fields are left unchanged. Labels, when
// non-nil, replace the issue's full label set (names; missing ones created).
type IssuePatch struct {
	Title     *string
	Body      *string
	State     *string // "open" | "closed"
	Labels    *[]string
	Milestone *int
}

// IssueFilter selects issues for ListIssues. State defaults to "open".
type IssueFilter struct {
	State  string   // "open" | "closed" | "all"
	Labels []string // label names; empty = any
}

// LabelSpec describes a label to ensure-exists. Color/Description are only
// used when the label has to be created.
type LabelSpec struct {
	Name        string
	Color       string // "#rrggbb"; defaults to defaultLabelColor
	Description string
}

// --- milestones ---

// EnsureMilestone returns the id of the milestone titled `title`, creating it
// if absent. Idempotent: safe to call on every milestone-sync.
func (c *Client) EnsureMilestone(title, description string) (int, error) {
	var existing []struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	}
	if err := c.do("GET", c.path("/milestones")+"?state=all", nil, &existing); err != nil {
		return 0, fmt.Errorf("list milestones: %w", err)
	}
	for _, m := range existing {
		if m.Title == title {
			return m.ID, nil
		}
	}
	var created struct {
		ID int `json:"id"`
	}
	if err := c.do("POST", c.path("/milestones"), map[string]any{
		"title":       title,
		"description": description,
	}, &created); err != nil {
		return 0, fmt.Errorf("create milestone %q: %w", title, err)
	}
	return created.ID, nil
}

// --- labels ---

// EnsureLabels makes sure every named label exists, creating missing ones
// with the given color/description, and returns a name->id map for all of
// them. Results are cached on the client for the rest of its lifetime.
func (c *Client) EnsureLabels(specs []LabelSpec) (map[string]int, error) {
	if err := c.loadLabels(); err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, s := range specs {
		if id, ok := c.labelIDs[s.Name]; ok {
			out[s.Name] = id
			continue
		}
		color := s.Color
		if color == "" {
			color = defaultLabelColor
		}
		var created struct {
			ID int `json:"id"`
		}
		if err := c.do("POST", c.path("/labels"), map[string]any{
			"name":        s.Name,
			"color":       color,
			"description": s.Description,
		}, &created); err != nil {
			return nil, fmt.Errorf("create label %q: %w", s.Name, err)
		}
		c.labelIDs[s.Name] = created.ID
		out[s.Name] = created.ID
	}
	return out, nil
}

func (c *Client) loadLabels() error {
	if len(c.labelIDs) > 0 {
		return nil // already populated this run
	}
	var labels []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := c.do("GET", c.path("/labels")+"?limit=100", nil, &labels); err != nil {
		return fmt.Errorf("list labels: %w", err)
	}
	for _, l := range labels {
		c.labelIDs[l.Name] = l.ID
	}
	return nil
}

// resolveLabelIDs maps label names to ids, creating any that don't exist with
// the default color.
func (c *Client) resolveLabelIDs(names []string) ([]int, error) {
	specs := make([]LabelSpec, len(names))
	for i, n := range names {
		specs[i] = LabelSpec{Name: n}
	}
	byName, err := c.EnsureLabels(specs)
	if err != nil {
		return nil, err
	}
	ids := make([]int, len(names))
	for i, n := range names {
		ids[i] = byName[n]
	}
	return ids, nil
}

// --- issues ---

// CreateIssue opens a new issue and returns it.
func (c *Client) CreateIssue(in IssueInput) (*Issue, error) {
	body := map[string]any{"title": in.Title, "body": in.Body}
	if in.Milestone > 0 {
		body["milestone"] = in.Milestone
	}
	if len(in.Labels) > 0 {
		ids, err := c.resolveLabelIDs(in.Labels)
		if err != nil {
			return nil, err
		}
		body["labels"] = ids
	}
	var raw issueResponse
	if err := c.do("POST", c.path("/issues"), body, &raw); err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}
	return raw.toIssue(), nil
}

// UpdateIssue applies a partial patch. Label changes go through the dedicated
// labels endpoint (a full replace); everything else rides the issue PATCH.
func (c *Client) UpdateIssue(number int, patch IssuePatch) (*Issue, error) {
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
	var raw issueResponse
	if len(body) > 0 {
		if err := c.do("PATCH", c.path(fmt.Sprintf("/issues/%d", number)), body, &raw); err != nil {
			return nil, fmt.Errorf("update issue #%d: %w", number, err)
		}
	}
	if patch.Labels != nil {
		ids, err := c.resolveLabelIDs(*patch.Labels)
		if err != nil {
			return nil, err
		}
		// PUT replaces the issue's label set wholesale.
		if err := c.do("PUT", c.path(fmt.Sprintf("/issues/%d/labels", number)),
			map[string]any{"labels": ids}, &raw); err != nil {
			return nil, fmt.Errorf("set labels on issue #%d: %w", number, err)
		}
	}
	return raw.toIssue(), nil
}

// CloseIssue is a convenience for the ship step.
func (c *Client) CloseIssue(number int) error {
	closed := "closed"
	_, err := c.UpdateIssue(number, IssuePatch{State: &closed})
	return err
}

// GetIssue fetches a single issue by number.
func (c *Client) GetIssue(number int) (*Issue, error) {
	var raw issueResponse
	if err := c.do("GET", c.path(fmt.Sprintf("/issues/%d", number)), nil, &raw); err != nil {
		return nil, fmt.Errorf("get issue #%d: %w", number, err)
	}
	return raw.toIssue(), nil
}

// ListIssues returns issues matching the filter. PRs are excluded (the
// Forgejo/Gitea issues endpoint otherwise returns both) so inbound triage
// never surfaces pull requests as "new work".
func (c *Client) ListIssues(f IssueFilter) ([]Issue, error) {
	state := f.State
	if state == "" {
		state = "open"
	}
	q := url.Values{}
	q.Set("state", state)
	q.Set("type", "issues") // exclude PRs
	q.Set("limit", "50")
	if len(f.Labels) > 0 {
		q.Set("labels", strings.Join(f.Labels, ","))
	}
	var raw []issueResponse
	if err := c.do("GET", c.path("/issues")+"?"+q.Encode(), nil, &raw); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	out := make([]Issue, 0, len(raw))
	for i := range raw {
		// Defensive: some instances ignore type=issues on older versions.
		if raw[i].PullRequest != nil {
			continue
		}
		out = append(out, *raw[i].toIssue())
	}
	return out, nil
}

// --- wire types ---

type issueResponse struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	State       string `json:"state"`
	HTMLURL     string `json:"html_url"`
	PullRequest *struct {
		Merged bool `json:"merged"`
	} `json:"pull_request"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
}

func (r *issueResponse) toIssue() *Issue {
	iss := &Issue{
		Number: r.Number,
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

// --- low-level REST ---

// path builds a /repos/{owner}/{repo}{suffix} API path.
func (c *Client) path(suffix string) string {
	return c.apiBase + fmt.Sprintf("/repos/%s/%s", c.owner, c.repo) + suffix
}

// do performs a token-authenticated JSON request. If out is non-nil and the
// response has a body, it's decoded into out. Non-2xx responses become errors
// carrying the status and (truncated) body.
func (c *Client) do(method, endpoint string, body any, out any) error {
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
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "token "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		snippet := string(respBody)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet)
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// splitOwnerRepo parses a canonical https://host/owner/repo URL. Duplicated
// from internal/ship (unexported there) to keep the packages decoupled.
func splitOwnerRepo(repoURL string) (owner, repo string, err error) {
	u, perr := url.Parse(repoURL)
	if perr != nil || u.Host == "" {
		return "", "", fmt.Errorf("bad repo URL %q", repoURL)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("URL %q does not look like /owner/repo", repoURL)
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}
