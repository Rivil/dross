package forge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// YouTrackClient talks to a YouTrack instance's REST API. Unlike the forge
// *Client (which serves the /repos-shaped forgejo/gitea/gitlab backends off
// one concrete type), YouTrack's REST surface is different enough — issues
// keyed by a readable id (PROJ-7), bearer permanent-token auth, a ?fields
// projection on every read, State carried as a custom field — to warrant its
// own sibling type behind the BoardClient interface.
//
// Construct with NewYouTrack. The forge.New dispatch to this backend for
// provider=youtrack lands in the string-id migration (plan t-5), when the
// board call sites consume BoardClient instead of the concrete *Client.
type YouTrackClient struct {
	baseURL string // instance root, no trailing slash; "/api/..." is appended
	project string // project short-name (e.g. "PROJ")
	token   string
	authEnv string // env var name (kept for diagnostic error messages)

	http *http.Client
}

var _ BoardClient = (*YouTrackClient)(nil)

// ytIssueFields is the projection requested on every issue read/write. Without
// an explicit fields list YouTrack returns only the database id, so we always
// ask for the readable id, summary/description, tags, and custom fields (State
// rides in there).
const ytIssueFields = "idReadable,summary,description,tags(name),customFields(name,value(name))"

// NewYouTrack validates config, resolves the permanent token from the
// environment, and returns a ready client. It errors early on the same shape
// of problems the forge New checks: missing base URL / auth env, unset token,
// missing project.
func NewYouTrack(cfg Config) (*YouTrackClient, error) {
	if cfg.APIBase == "" {
		return nil, fmt.Errorf("youtrack backend needs APIBase (set [board].base_url)")
	}
	if cfg.AuthEnv == "" {
		return nil, fmt.Errorf("youtrack backend needs AuthEnv (set [board].auth_env)")
	}
	if cfg.Project == "" {
		return nil, fmt.Errorf("youtrack backend needs Project (set [board].project)")
	}
	token := os.Getenv(cfg.AuthEnv)
	if token == "" {
		return nil, fmt.Errorf("$%s is not set; run `dross env set %s` in your shell", cfg.AuthEnv, cfg.AuthEnv)
	}
	return &YouTrackClient{
		baseURL: strings.TrimRight(cfg.APIBase, "/"),
		project: cfg.Project,
		token:   token,
		authEnv: cfg.AuthEnv,
		http:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// endpoint builds a full API URL for a path suffix (e.g. "/issues",
// "/issues/PROJ-7", "/admin/projects/PROJ").
func (c *YouTrackClient) endpoint(suffix string) string {
	return c.baseURL + "/api" + suffix
}

// --- issues ---

// CreateIssue opens a new issue in the configured project and returns it. The
// project is referenced by short-name; the ?fields projection makes YouTrack
// echo back the readable id (otherwise only the database id comes back).
func (c *YouTrackClient) CreateIssue(in IssueInput) (*Issue, error) {
	body := map[string]any{
		"project":     map[string]any{"shortName": c.project},
		"summary":     in.Title,
		"description": in.Body,
	}
	var raw youtrackIssue
	if err := c.do("POST", c.endpoint("/issues")+"?fields="+url.QueryEscape(ytIssueFields), body, &raw); err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}
	return raw.toIssue(), nil
}

// GetIssue fetches a single issue by its readable id (e.g. "PROJ-7").
func (c *YouTrackClient) GetIssue(key string) (*Issue, error) {
	var raw youtrackIssue
	if err := c.do("GET", c.endpoint("/issues/"+key)+"?fields="+url.QueryEscape(ytIssueFields), nil, &raw); err != nil {
		return nil, fmt.Errorf("get issue %s: %w", key, err)
	}
	return raw.toIssue(), nil
}

// UpdateIssue applies a partial patch addressed by readable id. YouTrack
// updates use POST (not PATCH/PUT). Title→summary and Body→description here;
// the State custom-field write lands with the state-map task (plan t-7).
func (c *YouTrackClient) UpdateIssue(key string, patch IssuePatch) (*Issue, error) {
	body := map[string]any{}
	if patch.Title != nil {
		body["summary"] = *patch.Title
	}
	if patch.Body != nil {
		body["description"] = *patch.Body
	}
	var raw youtrackIssue
	if len(body) > 0 {
		if err := c.do("POST", c.endpoint("/issues/"+key)+"?fields="+url.QueryEscape(ytIssueFields), body, &raw); err != nil {
			return nil, fmt.Errorf("update issue %s: %w", key, err)
		}
	}
	return raw.toIssue(), nil
}

// CloseIssue resolves an issue. YouTrack has no separate "close" — an issue is
// closed by moving its State to a resolved value, which the lifecycle-driven
// state sync handles (plan t-7). Standalone close is a no-op here so the
// BoardClient contract is satisfied without guessing at a resolved State name.
func (c *YouTrackClient) CloseIssue(key string) error {
	return nil
}

// EnsureMilestone is the forge-shaped milestone hook. YouTrack milestones are
// entity-mode specific (version bundle / agile board / epic), wired in plan
// t-6 (entity dispatch) and t-9 (milestone-sync). This placeholder satisfies
// the BoardClient contract; it returns no link so milestone-sync treats the
// entity as not-yet-ensured until the mode dispatch lands.
func (c *YouTrackClient) EnsureMilestone(title, description string) (string, error) {
	return "", nil
}

// ListIssues returns issues in the configured project matching the filter.
// State maps to YouTrack's resolved/unresolved query clauses and each label
// becomes a `tag:` clause.
func (c *YouTrackClient) ListIssues(f IssueFilter) ([]Issue, error) {
	q := url.Values{}
	q.Set("query", c.buildQuery(f))
	q.Set("fields", ytIssueFields)
	var raw []youtrackIssue
	if err := c.do("GET", c.endpoint("/issues")+"?"+q.Encode(), nil, &raw); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	out := make([]Issue, 0, len(raw))
	for i := range raw {
		out = append(out, *raw[i].toIssue())
	}
	return out, nil
}

// buildQuery assembles a YouTrack search query scoped to the project, with the
// open/closed state and any label tags folded in.
func (c *YouTrackClient) buildQuery(f IssueFilter) string {
	parts := []string{"project: " + c.project}
	switch f.State {
	case "", "open":
		parts = append(parts, "#Unresolved")
	case "closed":
		parts = append(parts, "#Resolved")
	}
	for _, l := range f.Labels {
		parts = append(parts, "tag: "+l)
	}
	return strings.Join(parts, " ")
}

// --- wire types ---

// youtrackIssue is the subset of YouTrack's Issue entity dross reads back.
// State lives among customFields; its value may be an object, null, or (for
// multi-value fields) an array, so each value is kept raw and parsed leniently.
type youtrackIssue struct {
	IDReadable  string `json:"idReadable"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Tags        []struct {
		Name string `json:"name"`
	} `json:"tags"`
	CustomFields []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	} `json:"customFields"`
}

func (r *youtrackIssue) toIssue() *Issue {
	iss := &Issue{
		Key:   r.IDReadable,
		Title: r.Summary,
		Body:  r.Description,
	}
	for _, t := range r.Tags {
		iss.Labels = append(iss.Labels, t.Name)
	}
	for _, cf := range r.CustomFields {
		if cf.Name != "State" || len(cf.Value) == 0 || string(cf.Value) == "null" {
			continue
		}
		var v struct {
			Name string `json:"name"`
		}
		// Skip array/scalar shapes that don't carry a single named value.
		if json.Unmarshal(cf.Value, &v) == nil {
			iss.State = v.Name
		}
	}
	return iss
}

// --- low-level REST ---

// do performs a bearer-authenticated JSON request. If out is non-nil and the
// response has a body, it's decoded into out. Non-2xx responses become errors
// carrying the status and a (truncated) body snippet.
func (c *YouTrackClient) do(method, endpoint string, body, out any) error {
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
			hint = " (token lacks permission for this project or action)"
		case 404:
			hint = fmt.Sprintf(" (project %s or endpoint not found — check [board].base_url and .project)", c.project)
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
