package forge

import (
	"bytes"
	"encoding/base64"
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

// JiraClient talks to a Jira Cloud instance's REST API v3. Like the
// YouTrackClient (and unlike the /repos-shaped forge *Client), Jira's surface
// is different enough — issues keyed by a readable string key (PROJ-123),
// HTTP Basic email:token auth, an Atlassian Document Format body, a
// transition-driven state model, and milestones modelled as project versions —
// to warrant its own sibling type behind the BoardClient interface.
//
// Construct with NewJira. forge.NewBoard dispatches provider=jira here.
type JiraClient struct {
	baseURL string // instance root, no trailing slash; "/rest/api/3/..." is appended
	project string // project key (e.g. "PROJ")
	email   string // Basic-auth username (Jira account email)
	token   string // API token
	authEnv string // env var name (kept for diagnostic error messages)

	http *http.Client
}

var _ BoardClient = (*JiraClient)(nil)

// defaultJiraIssueType is the issue type new dross issues are created as. Jira
// projects always have a "Task" type in the default schemes.
const defaultJiraIssueType = "Task"

// NewJira validates config, resolves the API token from the environment, and
// returns a ready client. It errors early on the same shape of problems the
// other backends check: missing base URL / auth env / project / account email,
// unset token.
func NewJira(cfg Config) (*JiraClient, error) {
	if cfg.APIBase == "" {
		return nil, fmt.Errorf("jira backend needs APIBase (set [board].base_url)")
	}
	if cfg.AuthEnv == "" {
		return nil, fmt.Errorf("jira backend needs AuthEnv (set [board].auth_env)")
	}
	if cfg.Project == "" {
		return nil, fmt.Errorf("jira backend needs Project (set [board].project)")
	}
	if cfg.AuthUser == "" {
		return nil, fmt.Errorf("jira backend needs AuthUser (set [board].auth_user to your Jira account email)")
	}
	token := os.Getenv(cfg.AuthEnv)
	if token == "" {
		return nil, fmt.Errorf("$%s is not set; run `dross env set %s` in your shell", cfg.AuthEnv, cfg.AuthEnv)
	}
	return &JiraClient{
		baseURL: strings.TrimRight(cfg.APIBase, "/"),
		project: cfg.Project,
		email:   cfg.AuthUser,
		token:   token,
		authEnv: cfg.AuthEnv,
		http:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// endpoint builds a full REST v3 URL for a path suffix (e.g. "/issue",
// "/issue/PROJ-7", "/issue/PROJ-7/transitions").
func (c *JiraClient) endpoint(suffix string) string {
	return c.baseURL + "/rest/api/3" + suffix
}

// --- issues ---

// CreateIssue opens a new issue in the configured project and returns it. The
// project is referenced by key; the milestone int (when > 0) is a Jira version
// id, attached through the issue's Fix Version/s field (Jira has no integer
// "milestone" field — versions are its release grouping).
func (c *JiraClient) CreateIssue(in IssueInput) (*Issue, error) {
	fields := map[string]any{
		"project":   map[string]any{"key": c.project},
		"summary":   in.Title,
		"issuetype": map[string]any{"name": defaultJiraIssueType},
	}
	if in.Body != "" {
		fields["description"] = adfDoc(in.Body)
	}
	if len(in.Labels) > 0 {
		// Jira labels are plain strings and are created on the fly; they cannot
		// contain spaces, so join words with underscores defensively.
		labels := make([]string, len(in.Labels))
		for i, l := range in.Labels {
			labels[i] = strings.ReplaceAll(l, " ", "_")
		}
		fields["labels"] = labels
	}
	if in.Milestone > 0 {
		fields["fixVersions"] = []map[string]any{{"id": strconv.Itoa(in.Milestone)}}
	}
	var raw jiraCreated
	if err := c.do("POST", c.endpoint("/issue"), map[string]any{"fields": fields}, &raw); err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}
	return &Issue{Key: raw.Key, Title: in.Title, Body: in.Body}, nil
}

// GetIssue fetches a single issue by its readable key (e.g. "PROJ-7").
func (c *JiraClient) GetIssue(key string) (*Issue, error) {
	var raw jiraIssue
	if err := c.do("GET", c.endpoint("/issue/"+url.PathEscape(key)), nil, &raw); err != nil {
		return nil, fmt.Errorf("get issue %s: %w", key, err)
	}
	return raw.toIssue(), nil
}

// UpdateIssue applies a partial patch addressed by readable key. Title/Body/
// Labels/Milestone ride a single PUT to the issue's fields; a State change is a
// workflow move, so it's dispatched to a transition (Jira has no direct state
// field on the issue-update endpoint).
func (c *JiraClient) UpdateIssue(key string, patch IssuePatch) (*Issue, error) {
	fields := map[string]any{}
	if patch.Title != nil {
		fields["summary"] = *patch.Title
	}
	if patch.Body != nil {
		fields["description"] = adfDoc(*patch.Body)
	}
	if patch.Labels != nil {
		labels := make([]string, len(*patch.Labels))
		for i, l := range *patch.Labels {
			labels[i] = strings.ReplaceAll(l, " ", "_")
		}
		fields["labels"] = labels
	}
	if patch.Milestone != nil {
		fields["fixVersions"] = []map[string]any{{"id": strconv.Itoa(*patch.Milestone)}}
	}
	if len(fields) > 0 {
		// A successful issue edit returns 204 No Content.
		if err := c.do("PUT", c.endpoint("/issue/"+url.PathEscape(key)), map[string]any{"fields": fields}, nil); err != nil {
			return nil, fmt.Errorf("update issue %s: %w", key, err)
		}
	}
	if patch.State != nil {
		if *patch.State == "closed" {
			if err := c.CloseIssue(key); err != nil {
				return nil, err
			}
		} else {
			if err := c.transitionToCategory(key, "new", "indeterminate"); err != nil {
				return nil, fmt.Errorf("reopen issue %s: %w", key, err)
			}
		}
	}
	return &Issue{Key: key}, nil
}

// CloseIssue resolves an issue by moving it through a transition whose target
// status is in the "done" category. Jira has no direct close — a status change
// is always a workflow transition.
func (c *JiraClient) CloseIssue(key string) error {
	if err := c.transitionToCategory(key, "done"); err != nil {
		return fmt.Errorf("close issue %s: %w", key, err)
	}
	return nil
}

// ListIssues returns issues in the configured project matching the filter, via
// a JQL search. State maps to statusCategory clauses and each label becomes a
// `labels = ` clause.
func (c *JiraClient) ListIssues(f IssueFilter) ([]Issue, error) {
	q := url.Values{}
	q.Set("jql", c.buildJQL(f))
	q.Set("fields", "summary,description,status,labels")
	q.Set("maxResults", "50")
	var raw jiraSearch
	if err := c.do("GET", c.endpoint("/search")+"?"+q.Encode(), nil, &raw); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	out := make([]Issue, 0, len(raw.Issues))
	for i := range raw.Issues {
		out = append(out, *raw.Issues[i].toIssue())
	}
	return out, nil
}

// buildJQL assembles a JQL query scoped to the project, folding in the
// open/closed state (via statusCategory) and any label clauses.
func (c *JiraClient) buildJQL(f IssueFilter) string {
	parts := []string{fmt.Sprintf("project = %q", c.project)}
	switch f.State {
	case "", "open":
		parts = append(parts, "statusCategory != Done")
	case "closed":
		parts = append(parts, "statusCategory = Done")
	}
	for _, l := range f.Labels {
		parts = append(parts, fmt.Sprintf("labels = %q", strings.ReplaceAll(l, " ", "_")))
	}
	return strings.Join(parts, " AND ") + " ORDER BY created DESC"
}

// --- milestones (versions) ---

// EnsureMilestone maps a dross milestone to a Jira project version, returning
// its (numeric) version id as a string. Idempotent — reuses an existing
// version of the same name. Satisfies the BoardClient contract; the command
// layer prefers the concrete EnsureMilestoneEntity path.
func (c *JiraClient) EnsureMilestone(title, description string) (string, error) {
	return c.ensureVersion(title, description)
}

// EnsureMilestoneEntity is the concrete milestone hook the issue command layer
// calls (mirroring YouTrack's method of the same name). Jira always maps a
// milestone to a project version, so mode is accepted for signature symmetry
// but only "version" (or empty) is meaningful.
func (c *JiraClient) EnsureMilestoneEntity(mode, name, description string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "version":
		return c.ensureVersion(name, description)
	default:
		return "", fmt.Errorf("jira only supports milestone_mode=version, got %q", mode)
	}
}

// ensureVersion ensures a version named `name` exists in the project and
// returns its numeric id as a string. It discovers the project's numeric id and
// existing versions in one GET, then POSTs the version only if absent.
func (c *JiraClient) ensureVersion(name, description string) (string, error) {
	var proj struct {
		ID       string `json:"id"`
		Versions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"versions"`
	}
	if err := c.do("GET", c.endpoint("/project/"+url.PathEscape(c.project)), nil, &proj); err != nil {
		return "", fmt.Errorf("get project %s: %w", c.project, err)
	}
	for _, v := range proj.Versions {
		if v.Name == name {
			return v.ID, nil // already present — idempotent reuse
		}
	}
	projectID, err := strconv.Atoi(proj.ID)
	if err != nil {
		return "", fmt.Errorf("project %s has non-numeric id %q", c.project, proj.ID)
	}
	body := map[string]any{"name": name, "projectId": projectID}
	if description != "" {
		body["description"] = description
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := c.do("POST", c.endpoint("/version"), body, &created); err != nil {
		return "", fmt.Errorf("create version %q: %w", name, err)
	}
	return created.ID, nil
}

// --- transitions / state ---

// defaultJiraStateMap maps dross lifecycle states to Jira status names. Status
// names are workflow-specific, so this is a sensible default for the built-in
// scheme, overridden per project via [board].state_map.
var defaultJiraStateMap = map[string]string{
	"planned":     "To Do",
	"in-progress": "In Progress",
	"verifying":   "In Progress",
	"shipped":     "Done",
	"complete":    "Done",
}

// resolveJiraState maps a dross lifecycle status to a Jira status name: the
// per-project override wins, falling back to the built-in default. ok is false
// when neither maps it (or the override blanks it).
func resolveJiraState(status string, override map[string]string) (string, bool) {
	if v, ok := override[status]; ok {
		return v, v != ""
	}
	v, ok := defaultJiraStateMap[status]
	return v, ok
}

// SetState moves an issue to the Jira status a dross lifecycle status maps to
// (default map overridden by `override`), by finding and applying the matching
// workflow transition. A status that maps to nothing — or a target with no
// available transition — warns and skips, returning nil so the rest of the sync
// still succeeds (mirrors YouTrack.SetState).
func (c *JiraClient) SetState(key, status string, override map[string]string) error {
	target, ok := resolveJiraState(status, override)
	if !ok {
		fmt.Fprintf(os.Stderr, "warning: dross state %q has no Jira status mapping — skipping transition on %s\n", status, key)
		return nil
	}
	trans, err := c.listTransitions(key)
	if err != nil {
		return err
	}
	for _, t := range trans {
		if strings.EqualFold(t.To.Name, target) {
			return c.applyTransition(key, t.ID)
		}
	}
	fmt.Fprintf(os.Stderr, "warning: no Jira transition to status %q available on %s — skipping\n", target, key)
	return nil
}

// jiraTransition is the subset of a workflow transition dross reads.
type jiraTransition struct {
	ID string `json:"id"`
	To struct {
		Name           string `json:"name"`
		StatusCategory struct {
			Key string `json:"key"` // "new" | "indeterminate" | "done"
		} `json:"statusCategory"`
	} `json:"to"`
}

// listTransitions returns the transitions currently available on an issue.
func (c *JiraClient) listTransitions(key string) ([]jiraTransition, error) {
	var resp struct {
		Transitions []jiraTransition `json:"transitions"`
	}
	if err := c.do("GET", c.endpoint("/issue/"+url.PathEscape(key)+"/transitions"), nil, &resp); err != nil {
		return nil, fmt.Errorf("list transitions for %s: %w", key, err)
	}
	return resp.Transitions, nil
}

// applyTransition performs the workflow transition with the given id.
func (c *JiraClient) applyTransition(key, id string) error {
	body := map[string]any{"transition": map[string]any{"id": id}}
	if err := c.do("POST", c.endpoint("/issue/"+url.PathEscape(key)+"/transitions"), body, nil); err != nil {
		return fmt.Errorf("transition %s: %w", key, err)
	}
	return nil
}

// transitionToCategory applies the first available transition whose target
// status falls in one of the given status categories (e.g. "done" to close, or
// "new"/"indeterminate" to reopen). Errors if none is available.
func (c *JiraClient) transitionToCategory(key string, categories ...string) error {
	trans, err := c.listTransitions(key)
	if err != nil {
		return err
	}
	for _, t := range trans {
		for _, cat := range categories {
			if strings.EqualFold(t.To.StatusCategory.Key, cat) {
				return c.applyTransition(key, t.ID)
			}
		}
	}
	return fmt.Errorf("no transition to a %s status available (available transitions may be workflow-gated)", strings.Join(categories, "/"))
}

// --- wire types ---

// jiraCreated is the response to a successful issue create.
type jiraCreated struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
}

// jiraIssue is the subset of Jira's Issue entity dross reads back. The
// description is Atlassian Document Format (a nested doc), flattened to text.
type jiraIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary     string          `json:"summary"`
		Description json.RawMessage `json:"description"`
		Labels      []string        `json:"labels"`
		Status      struct {
			Name           string `json:"name"`
			StatusCategory struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
		FixVersions []struct {
			Name string `json:"name"`
		} `json:"fixVersions"`
	} `json:"fields"`
}

func (r *jiraIssue) toIssue() *Issue {
	iss := &Issue{
		Key:    r.Key,
		Title:  r.Fields.Summary,
		Body:   adfToText(r.Fields.Description),
		Labels: r.Fields.Labels,
	}
	// Normalise Jira's status-category to dross's open/closed vocabulary.
	if strings.EqualFold(r.Fields.Status.StatusCategory.Key, "done") {
		iss.State = "closed"
	} else if r.Fields.Status.Name != "" {
		iss.State = "open"
	}
	if len(r.Fields.FixVersions) > 0 {
		iss.Milestone = r.Fields.FixVersions[0].Name
	}
	return iss
}

// jiraSearch is the JQL search response envelope.
type jiraSearch struct {
	Issues []jiraIssue `json:"issues"`
}

// --- Atlassian Document Format helpers ---

// adfDoc wraps a plain-text string in a minimal ADF document (Jira v3 requires
// rich-text fields like description to be ADF, not a bare string).
func adfDoc(text string) map[string]any {
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []map[string]any{
			{
				"type":    "paragraph",
				"content": []map[string]any{{"type": "text", "text": text}},
			},
		},
	}
}

// adfToText flattens an ADF document back to plain text by concatenating every
// text node, joining top-level blocks with newlines. Returns "" for a null or
// unparseable body.
func adfToText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var doc struct {
		Content []adfNode `json:"content"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	var blocks []string
	for _, n := range doc.Content {
		blocks = append(blocks, n.text())
	}
	return strings.Join(blocks, "\n")
}

// adfNode is a recursive ADF content node: it either carries text or nests
// further content.
type adfNode struct {
	Type    string    `json:"type"`
	Text    string    `json:"text"`
	Content []adfNode `json:"content"`
}

func (n adfNode) text() string {
	if n.Text != "" {
		return n.Text
	}
	var b strings.Builder
	for _, c := range n.Content {
		b.WriteString(c.text())
	}
	return b.String()
}

// --- low-level REST ---

// do performs a Basic-authenticated JSON request. If out is non-nil and the
// response has a body, it's decoded into out. Non-2xx responses become errors
// carrying the status and a (truncated) body snippet.
func (c *JiraClient) do(method, endpoint string, body, out any) error {
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
	basic := base64.StdEncoding.EncodeToString([]byte(c.email + ":" + c.token))
	req.Header.Set("Authorization", "Basic "+basic)

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
			hint = " (check $" + c.authEnv + " and [board].auth_user — token/email may be wrong)"
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
