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

	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/redact"
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

	// tagIDs caches the instance's tag index (name -> entity id) for the
	// client's lifetime. YouTrack tags are global entities referenced by id,
	// so every tag write needs this lookup; re-reading it per issue would
	// multiply a phase-sync's request count by its label count.
	tagIDs map[string]string

	// fields overrides the tracker-native custom-field names below. Every
	// entry is optional; the accessors fall back to YouTrack's own defaults.
	fields Fields

	http *http.Client
}

// Default YouTrack custom-field names. A project that renamed one — or runs a
// non-English UI — overrides it under [board.fields] rather than patching this.
const (
	ytDefaultStateField       = "State"
	ytDefaultTypeField        = "Type"
	ytDefaultFixVersionsField = "Fix versions"
)

// stateField names the custom field carrying an issue's workflow state.
func (c *YouTrackClient) stateField() string {
	if c.fields.State != "" {
		return c.fields.State
	}
	return ytDefaultStateField
}

// typeField names the custom field carrying an issue's type (Epic, Bug, …).
func (c *YouTrackClient) typeField() string {
	if c.fields.Type != "" {
		return c.fields.Type
	}
	return ytDefaultTypeField
}

// fixVersionsField names the version-bundle field milestones attach through.
func (c *YouTrackClient) fixVersionsField() string {
	if c.fields.FixVersions != "" {
		return c.fields.FixVersions
	}
	return ytDefaultFixVersionsField
}

var (
	_ BoardClient = (*YouTrackClient)(nil)
	_ IssueLinker = (*YouTrackClient)(nil)
)

// ytIssueFields is the projection requested on every issue read/write. Without
// an explicit fields list YouTrack returns only the database id, so we always
// ask for the readable id, summary/description, tags, and custom fields (State
// rides in there).
const ytIssueFields = "idReadable,summary,description,resolved,tags(name),customFields(name,value(name))"

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
	if err := cfg.Hosts.Check("[board].base_url", cfg.APIBase); err != nil {
		return nil, err
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
		fields:  cfg.Fields,
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
//
// Tags are a second write: YouTrack takes them as entity references, not names
// on the create body, so they go through applyTags once the issue exists. A
// failure there warns and still returns the issue — a tagging blip must not
// orphan a real issue that the caller is about to record.
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
	iss := raw.toIssue(c.stateField())
	if len(in.Labels) > 0 {
		if err := c.applyTags(iss.Key, in.Labels); err != nil {
			fmt.Fprintf(os.Stderr, "warning: issue %s created but tagging it failed: %v\n", iss.Key, err)
		} else {
			iss.Labels = in.Labels
		}
	}
	return iss, nil
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
			{"name": c.fixVersionsField(), "$type": "MultiVersionIssueCustomField", "value": []map[string]any{{"name": fixVersion}}},
		}
	}
	var raw youtrackIssue
	if err := c.do("POST", c.endpoint("/issues")+"?fields="+url.QueryEscape(ytIssueFields), body, &raw); err != nil {
		return nil, fmt.Errorf("create backlog item: %w", err)
	}
	return raw.toIssue(c.stateField()), nil
}

// LinkSubtask makes childKey a subtask of parentKey via the commands API
// (applying "subtask of <parent>" to the child). YouTrack treats links as a
// set, so re-applying an existing link is a no-op — safe to call on re-sync.
func (c *YouTrackClient) LinkSubtask(parentKey, childKey string) error {
	body := map[string]any{
		"query":  "subtask of " + parentKey,
		"issues": []map[string]any{{"idReadable": childKey}},
	}
	if err := c.do("POST", c.endpoint("/commands"), body, nil); err != nil {
		return fmt.Errorf("link %s as subtask of %s: %w", childKey, parentKey, err)
	}
	return nil
}

// LinkIssues relates `from` to `to` via the commands API. Distinct from
// LinkSubtask, which asserts a hierarchy: a routed backlog item and the phase
// it is destined for are peers, not parent and child.
//
// YouTrack treats links as a set, so re-applying an existing relation is a
// no-op — safe on every re-sync with no read-back needed.
func (c *YouTrackClient) LinkIssues(from, to string) error {
	body := map[string]any{
		"query":  "relates to " + to,
		"issues": []map[string]any{{"idReadable": from}},
	}
	if err := c.do("POST", c.endpoint("/commands"), body, nil); err != nil {
		return fmt.Errorf("link %s to %s: %w", from, to, err)
	}
	return nil
}

// GetIssue fetches a single issue by its readable id (e.g. "PROJ-7").
func (c *YouTrackClient) GetIssue(key string) (*Issue, error) {
	var raw youtrackIssue
	if err := c.do("GET", c.endpoint("/issues/"+key)+"?fields="+url.QueryEscape(ytIssueFields), nil, &raw); err != nil {
		return nil, fmt.Errorf("get issue %s: %w", key, err)
	}
	return raw.toIssue(c.stateField()), nil
}

// UpdateIssue applies a partial patch addressed by readable id. YouTrack
// updates use POST (not PATCH/PUT). Title→summary and Body→description here;
// the State custom-field write lands with the state-map task (plan t-7).
//
// A non-nil patch.Labels replaces the issue's tag set (see applyTags); a nil
// one leaves tags entirely alone, so a title-only patch costs no tag traffic.
// Unlike create, a failing tag write is returned: the label set is what the
// phase and deferred resolvers query by, so silently leaving it wrong would
// mint a duplicate issue on the next sync.
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
	if patch.Labels != nil {
		if err := c.applyTags(key, *patch.Labels); err != nil {
			return nil, fmt.Errorf("update issue %s: %w", key, err)
		}
	}
	iss := raw.toIssue(c.stateField())
	if iss.Key == "" {
		// A labels-only patch sends no issue body, so nothing echoed back.
		iss.Key = key
	}
	if patch.Labels != nil {
		iss.Labels = *patch.Labels
	}
	return iss, nil
}

// CloseIssue resolves an issue using the default lifecycle mapping for
// "complete". Callers that know the project's [board].state_map should use
// CloseIssueAs so a renamed resolved state is honoured.
func (c *YouTrackClient) CloseIssue(key string) error {
	return c.CloseIssueAs(key, "complete", nil)
}

// CloseIssueAs resolves an issue by writing the State value `status` maps to,
// then reads the issue back and fails unless the tracker reports it resolved.
//
// This used to be `return nil`. That is the c-5 bug in one line: ship printed
// "(closed)" for every YouTrack phase while the issue stayed open forever, and
// nothing anywhere could tell the difference. Two things fix it — writing the
// state for real, and verifying the write took. The read-back is not
// belt-and-braces: a YouTrack workflow can accept the POST and refuse the
// transition, so a 200 is not evidence the issue is resolved.
//
// An unmapped status is an error here, unlike SetState's lenient warn: a sync
// that could not label an issue is a cosmetic loss, but a CLOSE that silently
// did nothing is a false claim about the state of the work.
func (c *YouTrackClient) CloseIssueAs(key, status string, override map[string]string) error {
	value, ok := resolveYouTrackState(status, override)
	if !ok {
		return fmt.Errorf("cannot close %s: dross status %q has no YouTrack State mapping (set [board].state_map.%s)", key, status, status)
	}
	body := map[string]any{
		"customFields": []map[string]any{
			{"name": c.stateField(), "$type": "StateIssueCustomField", "value": map[string]any{"name": value}},
		},
	}
	if err := c.do("POST", c.endpoint("/issues/"+key)+"?fields="+url.QueryEscape("idReadable"), body, nil); err != nil {
		return fmt.Errorf("close %s: set state %q: %w", key, value, err)
	}
	iss, err := c.GetIssue(key)
	if err != nil {
		return fmt.Errorf("close %s: read back: %w", key, err)
	}
	if !iss.Resolved {
		return fmt.Errorf("close %s: wrote State %q but the issue still reads unresolved — the workflow may not allow that transition", key, value)
	}
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
	switch configenum.Normalize(mode) {
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
	// Prefer the bundle behind the configured fix-versions field. A project
	// can carry several VersionBundle-typed fields, and taking whichever came
	// back first would write the milestone into the wrong one — silently, since
	// the write itself succeeds.
	bundleID, values := "", []struct {
		Name string `json:"name"`
	}(nil)
	for _, f := range fields {
		if f.Bundle == nil || f.Bundle.Type != "VersionBundle" {
			continue
		}
		if f.Field.Name == c.fixVersionsField() {
			bundleID, values = f.Bundle.ID, f.Bundle.Values
			break
		}
		if bundleID == "" {
			// Fallback: the first version bundle, used only when no field
			// matches by name — so a project whose field is named something
			// else still syncs rather than erroring.
			bundleID, values = f.Bundle.ID, f.Bundle.Values
		}
	}
	for _, v := range values {
		if v.Name == name {
			return name, nil // already present — idempotent reuse
		}
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
	q.Set("query", "project: "+c.project+" "+c.typeField()+": Epic")
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
			{"name": c.typeField(), "$type": "SingleEnumIssueCustomField", "value": map[string]any{"name": "Epic"}},
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
//
// The keys are exactly configenum.LifecycleStatuses — the set dross emits — and
// internal/cmd/board_lifecycle_divergence_test.go fails the build if a key here
// stops being emitted or an emitted status stops being keyed here.
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
			{"name": c.stateField(), "$type": "StateIssueCustomField", "value": map[string]any{"name": value}},
		},
	}
	if err := c.do("POST", c.endpoint("/issues/"+key)+"?fields="+url.QueryEscape("idReadable"), body, nil); err != nil {
		return fmt.Errorf("set state %q on %s: %w", value, key, err)
	}
	return nil
}

// ListIssues returns issues in the configured project matching the filter.
// State maps to YouTrack's resolved/unresolved query clauses and the labels
// fold into one OR'd `tag:` clause.
//
// Labels the tracker does not know are dropped (and named on stderr) before the
// query is built; if every requested label is unknown the call returns nothing
// rather than degrading into an unfiltered whole-board query.
func (c *YouTrackClient) ListIssues(f IssueFilter) ([]Issue, error) {
	if len(f.Labels) > 0 {
		known, err := c.listTagNames()
		if err != nil {
			return nil, err
		}
		kept, dropped := FilterKnownLabels(f.Labels, known)
		WarnDroppedLabels("youtrack", dropped)
		if len(kept) == 0 {
			return nil, nil
		}
		f.Labels = kept
	}
	q := url.Values{}
	q.Set("query", c.buildQuery(f))
	q.Set("fields", ytIssueFields)
	var raw []youtrackIssue
	if err := c.do("GET", c.endpoint("/issues")+"?"+q.Encode(), nil, &raw); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	out := make([]Issue, 0, len(raw))
	for i := range raw {
		out = append(out, *raw[i].toIssue(c.stateField()))
	}
	return out, nil
}

// loadTags reads and caches YouTrack's tag index as name -> entity id. Tags
// are instance-global entities, so this doubles as the label vocabulary a
// query may name and as the id lookup every tag write needs.
func (c *YouTrackClient) loadTags() (map[string]string, error) {
	if c.tagIDs != nil {
		return c.tagIDs, nil // already populated this run
	}
	var tags []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.do("GET", c.endpoint("/issueTags")+"?fields=id,name&$top=1000", nil, &tags); err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	index := make(map[string]string, len(tags))
	for _, t := range tags {
		index[t.Name] = t.ID
	}
	c.tagIDs = index
	return index, nil
}

// listTagNames reads the tag index — the label vocabulary a query may name. A
// failure refuses the query rather than degrading it: an unfiltered list on a
// shared board is far worse than an error.
func (c *YouTrackClient) listTagNames() ([]string, error) {
	index, err := c.loadTags()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(index))
	for name := range index {
		names = append(names, name)
	}
	return names, nil
}

// ensureTag returns the entity id for a tag name, creating the tag when the
// instance has never seen it. Idempotent: a name already in the index costs no
// request.
func (c *YouTrackClient) ensureTag(name string) (string, error) {
	index, err := c.loadTags()
	if err != nil {
		return "", err
	}
	if id, ok := index[name]; ok {
		return id, nil
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := c.do("POST", c.endpoint("/issueTags")+"?fields=id,name", map[string]any{"name": name}, &created); err != nil {
		return "", fmt.Errorf("create tag %q: %w", name, err)
	}
	index[name] = created.ID
	return created.ID, nil
}

// applyTags makes the issue's tag set exactly `names` — added tags are added,
// tags no longer wanted are removed.
//
// Replace semantics matter because dross tags carry meaning that changes: a
// re-routed deferred item must lose `dross/target:a` when it gains
// `dross/target:b`, and a purely additive write would leave the issue claiming
// both destinations forever.
func (c *YouTrackClient) applyTags(key string, names []string) error {
	var current struct {
		Tags []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tags"`
	}
	if err := c.do("GET", c.endpoint("/issues/"+key)+"?fields=tags(id,name)", nil, &current); err != nil {
		return fmt.Errorf("read tags on %s: %w", key, err)
	}
	have := make(map[string]string, len(current.Tags))
	for _, t := range current.Tags {
		have[t.Name] = t.ID
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}

	for _, n := range names {
		if _, ok := have[n]; ok {
			continue
		}
		id, err := c.ensureTag(n)
		if err != nil {
			return err
		}
		if err := c.do("POST", c.endpoint("/issues/"+key+"/tags")+"?fields=id", map[string]any{"id": id}, nil); err != nil {
			return fmt.Errorf("tag %s with %q: %w", key, n, err)
		}
	}
	for name, id := range have {
		if want[name] {
			continue
		}
		if err := c.do("DELETE", c.endpoint("/issues/"+key+"/tags/"+id), nil, nil); err != nil {
			return fmt.Errorf("untag %q from %s: %w", name, key, err)
		}
	}
	return nil
}

// buildQuery assembles a YouTrack search query scoped to the project, with the
// open/closed state and any label tags folded in.
//
// Labels are OR'd, not AND'd: YouTrack reads the comma-separated values of a
// single `tag:` token as alternatives, whereas repeating the token once per
// label intersects them. A filter naming two labels means "either label".
func (c *YouTrackClient) buildQuery(f IssueFilter) string {
	parts := []string{"project: " + c.project}
	switch f.State {
	case "", "open":
		parts = append(parts, "#Unresolved")
	case "closed":
		parts = append(parts, "#Resolved")
	}
	if len(f.Labels) > 0 {
		vals := make([]string, len(f.Labels))
		for i, l := range f.Labels {
			vals[i] = quoteYouTrackValue(l)
		}
		parts = append(parts, "tag: "+strings.Join(vals, ", "))
	}
	return strings.Join(parts, " ")
}

// quoteYouTrackValue brace-wraps a query value that carries characters YouTrack
// would otherwise read as syntax — `/`, `:`, `-` and spaces all appear in dross's
// own tag names (`dross/phase:01-x`). A plain alphanumeric value is left bare.
func quoteYouTrackValue(v string) string {
	bare := v != ""
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			bare = false
			break
		}
	}
	if bare {
		return v
	}
	return "{" + v + "}"
}

// --- wire types ---

// youtrackIssue is the subset of YouTrack's Issue entity dross reads back.
// State lives among customFields; its value may be an object, null, or (for
// multi-value fields) an array, so each value is kept raw and parsed leniently.
type youtrackIssue struct {
	IDReadable  string `json:"idReadable"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	// Resolved is a millisecond timestamp when the issue is resolved and null
	// otherwise — YouTrack's own verdict, not a state name we chose.
	Resolved *int64 `json:"resolved"`
	Tags     []struct {
		Name string `json:"name"`
	} `json:"tags"`
	CustomFields []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	} `json:"customFields"`
}

// toIssue converts the wire shape, reading the workflow state out of the named
// custom field. The name is passed in rather than hardcoded because a project
// may have renamed it ([board.fields].state) — and a read that looked for
// "State" on such an instance would report every issue as stateless.
func (r *youtrackIssue) toIssue(stateField string) *Issue {
	iss := &Issue{
		Key:      r.IDReadable,
		Title:    r.Summary,
		Body:     r.Description,
		Resolved: r.Resolved != nil,
	}
	for _, t := range r.Tags {
		iss.Labels = append(iss.Labels, t.Name)
	}
	for _, cf := range r.CustomFields {
		if cf.Name != stateField || len(cf.Value) == 0 || string(cf.Value) == "null" {
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

// do performs a bearer-authenticated JSON request, with the credential removed
// from whatever error comes back. See Client.do for why the wrap sits around
// the whole request rather than at each Errorf.
func (c *YouTrackClient) do(method, endpoint string, body, out any) error {
	return redact.Err(c.doRaw(method, endpoint, body, out), c.authEnv, c.token)
}

// doRaw is the unredacted request. Nothing outside do may call it.
func (c *YouTrackClient) doRaw(method, endpoint string, body, out any) error {
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
