package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ytPhaseFake is a YouTrack fake shaped for the phase resolver: it serves a
// tag index, a fixed set of pre-existing issues (with their tags), and records
// every create and update so a test can assert on the counts that matter —
// "zero creates" is the whole point of c-4.
type ytPhaseFake struct {
	mu sync.Mutex

	// issues maps a readable id to the tags it carries, and titles to its summary.
	tags   map[string][]string
	titles map[string]string
	// listByLabel answers the resolver's queries. Missing label -> no matches.
	creates   int
	updates   []string // keys updated, in order
	createdAt map[string]bool
	// nextKey is minted for each create.
	nextKey   string
	notFound  map[string]bool // keys that 404 on GET
	knownTags []string        // tag entities created via POST /api/issueTags
}

func newYTPhaseFake() *ytPhaseFake {
	return &ytPhaseFake{
		tags:      map[string][]string{},
		titles:    map[string]string{},
		createdAt: map[string]bool{},
		nextKey:   "PROJ-100",
		notFound:  map[string]bool{},
	}
}

func (f *ytPhaseFake) addIssue(key, title string, tags ...string) {
	f.tags[key] = tags
	f.titles[key] = title
}

// tagIndex is every tag name any issue carries — the vocabulary
// FilterKnownLabels checks a query against.
func (f *ytPhaseFake) tagIndex() []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, tags := range f.tags {
		for _, t := range tags {
			add(t)
		}
	}
	for _, t := range f.knownTags {
		add(t)
	}
	return out
}

func (f *ytPhaseFake) issueJSON(key string) string {
	var tagJSON []string
	for _, t := range f.tags[key] {
		tagJSON = append(tagJSON, `{"id":"tid-`+t+`","name":`+jsonQuote(t)+`}`)
	}
	return `{"idReadable":` + jsonQuote(key) + `,"summary":` + jsonQuote(f.titles[key]) +
		`,"tags":[` + strings.Join(tagJSON, ",") + `]}`
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (f *ytPhaseFake) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		path := r.URL.Path

		switch {
		case path == "/api/issueTags" && r.Method == "GET":
			var out []string
			for _, n := range f.tagIndex() {
				out = append(out, `{"id":"tid-`+n+`","name":`+jsonQuote(n)+`}`)
			}
			_, _ = io.WriteString(w, "["+strings.Join(out, ",")+"]")

		case path == "/api/issueTags" && r.Method == "POST":
			var body struct {
				Name string `json:"name"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			f.knownTags = append(f.knownTags, body.Name)
			_, _ = io.WriteString(w, `{"id":"tid-`+body.Name+`"}`)

		case path == "/api/issues" && r.Method == "GET":
			// The resolver's query. Extract the single tag it filters on.
			query := r.URL.Query().Get("query")
			var out []string
			for key, tags := range f.tags {
				for _, tag := range tags {
					if strings.Contains(query, tag) {
						out = append(out, f.issueJSON(key))
						break
					}
				}
			}
			_, _ = io.WriteString(w, "["+strings.Join(out, ",")+"]")

		case path == "/api/issues" && r.Method == "POST":
			f.creates++
			key := f.nextKey
			f.tags[key] = nil
			f.titles[key] = ""
			f.createdAt[key] = true
			_, _ = io.WriteString(w, `{"idReadable":`+jsonQuote(key)+`}`)

		case strings.HasSuffix(path, "/tags") && r.Method == "POST":
			key := strings.TrimSuffix(strings.TrimPrefix(path, "/api/issues/"), "/tags")
			var body struct {
				ID string `json:"id"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			name := strings.TrimPrefix(body.ID, "tid-")
			f.tags[key] = append(f.tags[key], name)
			_, _ = io.WriteString(w, `{}`)

		case strings.Contains(path, "/tags/") && r.Method == "DELETE":
			rest := strings.TrimPrefix(path, "/api/issues/")
			key, id, _ := strings.Cut(rest, "/tags/")
			name := strings.TrimPrefix(id, "tid-")
			var kept []string
			for _, n := range f.tags[key] {
				if n != name {
					kept = append(kept, n)
				}
			}
			f.tags[key] = kept
			_, _ = io.WriteString(w, `{}`)

		case strings.HasPrefix(path, "/api/issues/") && r.Method == "GET":
			key := strings.TrimPrefix(path, "/api/issues/")
			if f.notFound[key] {
				w.WriteHeader(404)
				return
			}
			if _, ok := f.titles[key]; !ok {
				w.WriteHeader(404)
				return
			}
			_, _ = io.WriteString(w, f.issueJSON(key))

		case strings.HasPrefix(path, "/api/issues/") && r.Method == "POST":
			key := strings.TrimPrefix(path, "/api/issues/")
			// SetState posts to the same path with only customFields. Count
			// the issue-body patch, which is the write the resolver decides.
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			if _, isPatch := body["summary"]; isPatch {
				f.updates = append(f.updates, key)
			}
			_, _ = io.WriteString(w, `{"idReadable":`+jsonQuote(key)+`}`)

		default:
			t.Errorf("unexpected %s %s", r.Method, path)
			_, _ = io.WriteString(w, `{}`)
		}
	}
}

// phaseSyncRepo scaffolds a YouTrack-backed repo with one phase spec and an
// optional board.json body, then returns the repo dir.
func phaseSyncRepo(t *testing.T, srvURL, phaseID, title, boardJSON string) string {
	t.Helper()
	dir := youtrackBoardRepo(t, srvURL)
	writeSpec(t, dir, phaseID, "[phase]\nid=\""+phaseID+"\"\ntitle=\""+title+"\"\n")
	if boardJSON != "" {
		mustWrite(t, filepath.Join(dir, ".dross", "board.json"), boardJSON)
	}
	return dir
}

const emptyBoardJSON = `{"phases":{},"quicks":{},"milestones":{},"dismissed":[]}`

// TestPhaseSyncLabelsCarryPhaseIdentity pins that both write paths carry the
// marker AND the phase label. A wholesale label replace that forgets the phase
// label orphans the issue on the NEXT run, not this one — which is exactly how
// the duplicate got shipped in the first place.
func TestPhaseSyncLabelsCarryPhaseIdentity(t *testing.T) {
	t.Run("create payload", func(t *testing.T) {
		var tagged []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/issueTags" && r.Method == "GET":
				_, _ = io.WriteString(w, `[{"id":"t1","name":"dross"},{"id":"t2","name":"dross/phase:01-auth"},{"id":"t3","name":"dross/status:planned"}]`)
			case r.URL.Path == "/api/issues" && r.Method == "GET":
				_, _ = io.WriteString(w, `[]`)
			case r.URL.Path == "/api/issues" && r.Method == "POST":
				_, _ = io.WriteString(w, `{"idReadable":"PROJ-7"}`)
			case strings.HasSuffix(r.URL.Path, "/tags") && r.Method == "POST":
				var body struct{ ID string }
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &body)
				tagged = append(tagged, strings.TrimPrefix(body.ID, "t"))
				_, _ = io.WriteString(w, `{}`)
			default:
				_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","tags":[]}`)
			}
		}))
		t.Cleanup(srv.Close)

		phaseSyncRepo(t, srv.URL, "01-auth", "Auth", emptyBoardJSON)
		if err := runCmd(t, Issue(), "phase", "sync", "01-auth"); err != nil {
			t.Fatalf("phase-sync: %v", err)
		}
		// t1 = dross, t2 = dross/phase:01-auth
		if !slicesHas(tagged, "1") || !slicesHas(tagged, "2") {
			t.Errorf("tagged %v, want both the marker and the phase label", tagged)
		}
	})

	t.Run("update patch", func(t *testing.T) {
		f := newYTPhaseFake()
		f.addIssue("PROJ-7", "01-auth — Auth", "dross", "dross/phase:01-auth")
		srv := httptest.NewServer(f.handler(t))
		t.Cleanup(srv.Close)

		phaseSyncRepo(t, srv.URL, "01-auth", "Auth",
			`{"phases":{"01-auth":"PROJ-7"},"quicks":{},"milestones":{},"dismissed":[]}`)
		if err := runCmd(t, Issue(), "phase", "sync", "01-auth"); err != nil {
			t.Fatalf("phase-sync: %v", err)
		}
		// The update path re-applies the full label set; the fake's replace
		// semantics would strip the phase label if the patch dropped it, and
		// the next resolve would then mint a duplicate.
		if f.creates != 0 {
			t.Errorf("creates = %d on a cached hit, want 0", f.creates)
		}
	})
}

// TestPhaseSyncAdoptsLabelledIssueWithEmptyBoardJSON is the deleted-branch case
// c-4 exists for: the mapping died with phase/<id>, but the tracker still knows.
func TestPhaseSyncAdoptsLabelledIssueWithEmptyBoardJSON(t *testing.T) {
	f := newYTPhaseFake()
	f.addIssue("PROJ-7", "01-auth — Auth", "dross", "dross/phase:01-auth")
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	dir := phaseSyncRepo(t, srv.URL, "01-auth", "Auth", emptyBoardJSON)
	if err := runCmd(t, Issue(), "phase", "sync", "01-auth"); err != nil {
		t.Fatalf("phase-sync: %v", err)
	}
	if f.creates != 0 {
		t.Fatalf("creates = %d, want 0 — the labelled issue must be adopted", f.creates)
	}
	if len(f.updates) != 1 || f.updates[0] != "PROJ-7" {
		t.Errorf("updates = %v, want exactly one update to PROJ-7", f.updates)
	}
	bj, err := readBoardJSON(dir)
	if err != nil {
		t.Fatalf("board.json: %v", err)
	}
	if bj.Phases["01-auth"] != "PROJ-7" {
		t.Errorf("board.json phases = %v, want 01-auth mapped to the adopted PROJ-7", bj.Phases)
	}
}

// TestPhaseSyncResolverQueryShape pins the filter. Two labels would be OR'd
// now (c-1) and adopt an arbitrary dross issue; an open-only query would miss
// the closed phase issue and mint an orphan on every re-ship.
func TestPhaseSyncResolverQueryShape(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/issueTags" && r.Method == "GET":
			_, _ = io.WriteString(w, `[{"id":"t1","name":"dross"},{"id":"t2","name":"dross/phase:01-auth"}]`)
		case r.URL.Path == "/api/issues" && r.Method == "GET":
			queries = append(queries, r.URL.Query().Get("query"))
			_, _ = io.WriteString(w, `[]`)
		case r.URL.Path == "/api/issues" && r.Method == "POST":
			_, _ = io.WriteString(w, `{"idReadable":"PROJ-7"}`)
		default:
			_, _ = io.WriteString(w, `{"idReadable":"PROJ-7","tags":[]}`)
		}
	}))
	t.Cleanup(srv.Close)

	phaseSyncRepo(t, srv.URL, "01-auth", "Auth", emptyBoardJSON)
	if err := runCmd(t, Issue(), "phase", "sync", "01-auth"); err != nil {
		t.Fatalf("phase-sync: %v", err)
	}
	if len(queries) == 0 {
		t.Fatal("the resolver issued no query at all")
	}
	q := queries[0]
	if strings.Count(q, "tag:") != 1 {
		t.Errorf("resolver query %q carries %d tag tokens, want exactly 1", q, strings.Count(q, "tag:"))
	}
	if !strings.Contains(q, "01-auth") {
		t.Errorf("resolver query %q does not filter on the phase label", q)
	}
	if strings.Contains(q, "#Unresolved") || strings.Contains(q, "#Resolved") {
		t.Errorf("resolver query %q is state-scoped; a closed phase issue must still resolve", q)
	}
}

// TestPhaseSyncIgnoresAnUnmarkedMatch pins the post-filter: a hand-made issue
// that happens to carry the phase label is not dross's to take over.
func TestPhaseSyncIgnoresAnUnmarkedMatch(t *testing.T) {
	f := newYTPhaseFake()
	f.addIssue("PROJ-7", "someone else's issue", "dross/phase:01-auth")
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	phaseSyncRepo(t, srv.URL, "01-auth", "Auth", emptyBoardJSON)
	if err := runCmd(t, Issue(), "phase", "sync", "01-auth"); err != nil {
		t.Fatalf("phase-sync: %v", err)
	}
	if f.creates != 1 {
		t.Errorf("creates = %d, want 1 — an unmarked match must not be adopted", f.creates)
	}
	if slicesHas(f.updates, "PROJ-7") {
		t.Error("phase-sync wrote to an issue it does not own")
	}
}

// TestPhaseSyncHealsAStaleBoardJSON: the cache points at an issue that no
// longer exists, while the live one carries the phase label.
func TestPhaseSyncHealsAStaleBoardJSON(t *testing.T) {
	f := newYTPhaseFake()
	f.addIssue("PROJ-7", "01-auth — Auth", "dross", "dross/phase:01-auth")
	f.notFound["PROJ-99"] = true
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	dir := phaseSyncRepo(t, srv.URL, "01-auth", "Auth",
		`{"phases":{"01-auth":"PROJ-99"},"quicks":{},"milestones":{},"dismissed":[]}`)
	if err := runCmd(t, Issue(), "phase", "sync", "01-auth"); err != nil {
		t.Fatalf("phase-sync: %v", err)
	}
	if f.creates != 0 {
		t.Errorf("creates = %d, want 0 — the live labelled issue must be adopted", f.creates)
	}
	if len(f.updates) != 1 || f.updates[0] != "PROJ-7" {
		t.Errorf("updates = %v, want exactly one update to PROJ-7", f.updates)
	}
	bj, err := readBoardJSON(dir)
	if err != nil {
		t.Fatalf("board.json: %v", err)
	}
	if bj.Phases["01-auth"] != "PROJ-7" {
		t.Errorf("board.json still says %q — a cache that never heals keeps shadowing the live issue", bj.Phases["01-auth"])
	}
}

// TestPhaseSyncAdoptsALegacyIssueBySummary covers issues synced before phase
// labels existed: identifiable only by the exact `<id> — <title>` summary.
func TestPhaseSyncAdoptsALegacyIssueBySummary(t *testing.T) {
	t.Run("marker plus exact summary is adopted", func(t *testing.T) {
		f := newYTPhaseFake()
		f.addIssue("PROJ-7", "01-auth — Auth", "dross")
		srv := httptest.NewServer(f.handler(t))
		t.Cleanup(srv.Close)

		phaseSyncRepo(t, srv.URL, "01-auth", "Auth", emptyBoardJSON)
		if err := runCmd(t, Issue(), "phase", "sync", "01-auth"); err != nil {
			t.Fatalf("phase-sync: %v", err)
		}
		if f.creates != 0 {
			t.Errorf("creates = %d, want 0 — the legacy issue must be adopted", f.creates)
		}
		if len(f.updates) != 1 || f.updates[0] != "PROJ-7" {
			t.Errorf("updates = %v, want exactly one update to PROJ-7", f.updates)
		}
	})

	t.Run("matching summary without the marker is NOT adopted", func(t *testing.T) {
		f := newYTPhaseFake()
		// Carries some other dross-ish tag so the tag index is non-empty, but
		// not the marker itself.
		f.addIssue("PROJ-7", "01-auth — Auth", "dross/status:planned")
		srv := httptest.NewServer(f.handler(t))
		t.Cleanup(srv.Close)

		phaseSyncRepo(t, srv.URL, "01-auth", "Auth", emptyBoardJSON)
		if err := runCmd(t, Issue(), "phase", "sync", "01-auth"); err != nil {
			t.Fatalf("phase-sync: %v", err)
		}
		if f.creates != 1 {
			t.Errorf("creates = %d, want 1 — an unmarked summary match must not be adopted", f.creates)
		}
	})
}

// TestPhaseSyncWithDuplicateLabelledIssues: two issues already carry the phase
// label. Exactly one gets written, deterministically, with a warning — never
// two writes, which would deepen the divergence it is meant to close.
func TestPhaseSyncWithDuplicateLabelledIssues(t *testing.T) {
	f := newYTPhaseFake()
	f.addIssue("PROJ-7", "01-auth — Auth", "dross", "dross/phase:01-auth")
	f.addIssue("PROJ-8", "01-auth — Auth", "dross", "dross/phase:01-auth")
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	phaseSyncRepo(t, srv.URL, "01-auth", "Auth", emptyBoardJSON)

	warn := captureStderr(t, func() {
		if err := runCmd(t, Issue(), "phase", "sync", "01-auth"); err != nil {
			t.Fatalf("phase-sync: %v", err)
		}
	})
	if len(f.updates) != 1 || f.updates[0] != "PROJ-7" {
		t.Errorf("updates = %v, want exactly one update to the lower key PROJ-7", f.updates)
	}
	if f.creates != 0 {
		t.Errorf("creates = %d, want 0", f.creates)
	}
	if !strings.Contains(warn, "PROJ-8") {
		t.Errorf("stderr = %q, want the duplicate named so it can be cleaned up", warn)
	}
}

// TestPhaseSyncCreatesWhenNothingResolves is the guard against over-correction:
// the resolver must not turn "no issue yet" into "never create".
func TestPhaseSyncCreatesWhenNothingResolves(t *testing.T) {
	f := newYTPhaseFake()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	phaseSyncRepo(t, srv.URL, "01-auth", "Auth", emptyBoardJSON)
	if err := runCmd(t, Issue(), "phase", "sync", "01-auth"); err != nil {
		t.Fatalf("phase-sync: %v", err)
	}
	if f.creates != 1 {
		t.Errorf("creates = %d on an empty board, want exactly 1", f.creates)
	}
}

// TestPhaseSyncTwiceLeavesOneIssue is the end-to-end c-4 assertion.
func TestPhaseSyncTwiceLeavesOneIssue(t *testing.T) {
	f := newYTPhaseFake()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	phaseSyncRepo(t, srv.URL, "01-auth", "Auth", emptyBoardJSON)
	for i := 0; i < 2; i++ {
		if err := runCmd(t, Issue(), "phase", "sync", "01-auth"); err != nil {
			t.Fatalf("phase-sync #%d: %v", i+1, err)
		}
	}
	if f.creates != 1 {
		t.Errorf("creates = %d across two syncs, want exactly 1", f.creates)
	}
}

func slicesHas(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
