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

// CreateBacklogItem creates an Open backlog issue and, when fixVersion is set,
// attaches it to the milestone's Version bundle value (version mode) via the
// Fix versions field. New YouTrack issues are Open by default.
func (c *YouTrackClient) CreateBacklogItem(summary, description, fixVersion string) (*Issue, error) {
	body := map[string]any{
		"project":     map[string]any{"shortName": c.project},
		"summary":     summary,
		"description": description,
	}
	if fixVersion != "" {
		body["customFields"] = []map[string]any{
			{"name": "Fix versions", "$type": "MultiVersionIssueCustomField", "value": []map[string]any{{"name": fixVersion}}},
		}
	}
	var raw youtrackIssue
	if err := c.do("POST", c.endpoint("/issues")+"?fields="+url.QueryEscape(ytIssueFields), body, &raw); err != nil {
		return nil, fmt.Errorf("create backlog item: %w", err)
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

// EnsureMilestoneEntity ensures the YouTrack entity a dross milestone maps to,
// per the configured milestone_mode, and returns its readable id (or "" when
// the mode degrades to a skip). Idempotent — re-running reuses, never
// duplicates.
//
//   - version (default): a value in the project's Version bundle. Returns the
//     version name (the identifier issues are tagged with).
//   - agile: a pre-existing Agile board, looked up by name. A missing board
//     warns and skips (no error, "" id) rather than failing the sync.
//   - epic: a create-or-reuse Epic issue. Returns its idReadable.
func (c *YouTrackClient) EnsureMilestoneEntity(mode, name, description string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "version":
		return c.ensureVersion(name)
	case "agile":
		return c.ensureAgile(name)
	case "epic":
		return c.ensureEpic(name, description)
	default:
		return "", fmt.Errorf("unknown milestone_mode %q (expected version | agile | epic)", mode)
	}
}

// ensureVersion ensures a value exists in the project's Version bundle. It
// discovers the bundle (and its current values) through the project's custom
// fields, then adds the value via the version-bundle endpoint if absent.
func (c *YouTrackClient) ensureVersion(name string) (string, error) {
	var fields []struct {
		Field struct {
			Name string `json:"name"`
		} `json:"field"`
		Bundle *struct {
			ID     string `json:"id"`
			Type   string `json:"$type"`
			Values []struct {
				Name string `json:"name"`
			} `json:"values"`
		} `json:"bundle"`
	}
	q := "?fields=" + url.QueryEscape("field(name),bundle(id,$type,values(name))")
	if err := c.do("GET", c.endpoint("/admin/projects/"+c.project+"/customFields")+q, nil, &fields); err != nil {
		return "", fmt.Errorf("list project custom fields: %w", err)
	}
	bundleID := ""
	for _, f := range fields {
		if f.Bundle == nil || f.Bundle.Type != "VersionBundle" {
			continue
		}
		bundleID = f.Bundle.ID
		for _, v := range f.Bundle.Values {
			if v.Name == name {
				return name, nil // already present — idempotent reuse
			}
		}
		break
	}
	if bundleID == "" {
		return "", fmt.Errorf("project %s has no version bundle (no version-typed field?)", c.project)
	}
	// The version-bundle values endpoint lives under customFieldSettings, not
	// the project — the project custom-field GET above is how we resolve which
	// bundle to write to.
	if err := c.do("POST", c.endpoint("/admin/customFieldSettings/bundles/version/"+bundleID+"/values")+"?fields="+url.QueryEscape("name"),
		map[string]any{"name": name}, nil); err != nil {
		return "", fmt.Errorf("add version %q: %w", name, err)
	}
	return name, nil
}

// ensureAgile returns the id of a pre-existing Agile board matching name. A
// missing board warns and skips (no error) so milestone sync degrades rather
// than failing on a project without the expected board.
func (c *YouTrackClient) ensureAgile(name string) (string, error) {
	var boards []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.do("GET", c.endpoint("/agiles")+"?fields="+url.QueryEscape("id,name"), nil, &boards); err != nil {
		return "", fmt.Errorf("list agile boards: %w", err)
	}
	for _, b := range boards {
		if b.Name == name {
			return b.ID, nil
		}
	}
	fmt.Fprintf(os.Stderr, "warning: no Agile board named %q on this YouTrack — skipping milestone attach\n", name)
	return "", nil
}

// ensureEpic creates-or-reuses an Epic issue named `name` and returns its
// readable id. Reuse matches an existing Epic by summary.
func (c *YouTrackClient) ensureEpic(name, description string) (string, error) {
	q := url.Values{}
	q.Set("query", "project: "+c.project+" Type: Epic")
	q.Set("fields", "idReadable,summary")
	var existing []youtrackIssue
	if err := c.do("GET", c.endpoint("/issues")+"?"+q.Encode(), nil, &existing); err != nil {
		return "", fmt.Errorf("list epics: %w", err)
	}
	for _, e := range existing {
		if e.Summary == name {
			return e.IDReadable, nil // reuse
		}
	}
	body := map[string]any{
		"project":     map[string]any{"shortName": c.project},
		"summary":     name,
		"description": description,
		"customFields": []map[string]any{
			{"name": "Type", "$type": "SingleEnumIssueCustomField", "value": map[string]any{"name": "Epic"}},
		},
	}
	var created youtrackIssue
	if err := c.do("POST", c.endpoint("/issues")+"?fields="+url.QueryEscape("idReadable,summary"), body, &created); err != nil {
		return "", fmt.Errorf("create epic %q: %w", name, err)
	}
	return created.IDReadable, nil
}

// defaultYouTrackStateMap maps dross lifecycle states to YouTrack State values.
// YouTrack state names are instance-specific, so this is a sensible default
// overridden per project via [board].state_map.
var defaultYouTrackStateMap = map[string]string{
	"planned":     "Open",
	"in-progress": "In Progress",
	"verifying":   "In Progress",
	"shipped":     "Fixed",
	"complete":    "Verified",
}

// resolveYouTrackState maps a dross lifecycle status to a YouTrack State value:
// the per-project override wins, falling back to the built-in default. ok is
// false when neither maps it (or the override blanks it).
func resolveYouTrackState(status string, override map[string]string) (string, bool) {
	if v, ok := override[status]; ok {
		return v, v != ""
	}
	v, ok := defaultYouTrackStateMap[status]
	return v, ok
}

// SetState updates an issue's State custom field from a dross lifecycle status,
// mapped via the default map overridden by `override`. A status that maps to
// nothing warns and skips the State write, returning nil so the rest of the
// issue sync still succeeds.
func (c *YouTrackClient) SetState(key, status string, override map[string]string) error {
	value, ok := resolveYouTrackState(status, override)
	if !ok {
		fmt.Fprintf(os.Stderr, "warning: dross state %q has no YouTrack State mapping — skipping State update on %s\n", status, key)
		return nil
	}
	body := map[string]any{
		"customFields": []map[string]any{
			{"name": "State", "$type": "StateIssueCustomField", "value": map[string]any{"name": value}},
		},
	}
	if err := c.do("POST", c.endpoint("/issues/"+key)+"?fields="+url.QueryEscape("idReadable"), body, nil); err != nil {
		return fmt.Errorf("set state %q on %s: %w", value, key, err)
	}
	return nil
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
