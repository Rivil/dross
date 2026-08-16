package forge

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// bundleServer is a YouTrack that rejects a State value it does not hold —
// which is what a real one does, and the behaviour the whole task exists for.
type bundleServer struct {
	mu sync.Mutex
	// values is the project's State bundle contents.
	values []string
	// projects is how many projects share the bundle. More than one makes it
	// shared, and dross must not write to it.
	projects int
	// setAttempts counts POSTs to the issue, valueCreates counts POSTs to the
	// bundle. The pair is what proves a retry happened rather than a silent skip.
	setAttempts  int
	valueCreates int
	createFails  bool
	srv          *httptest.Server
}

func newBundleServer(t *testing.T, values []string, projects int) *bundleServer {
	t.Helper()
	b := &bundleServer{values: values, projects: projects}
	b.srv = httptest.NewServer(http.HandlerFunc(b.handle))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *bundleServer) handle(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	switch {
	case strings.Contains(r.URL.Path, "/admin/projects/") && strings.HasSuffix(r.URL.Path, "/customFields"):
		var vals []map[string]any
		for _, v := range b.values {
			vals = append(vals, map[string]any{"name": v})
		}
		var projs []map[string]any
		for i := 0; i < b.projects; i++ {
			projs = append(projs, map[string]any{"shortName": "P" + string(rune('A'+i))})
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"field":  map[string]any{"name": "State"},
			"bundle": map[string]any{"id": "bundle-1", "values": vals, "projects": projs},
		}})

	case strings.Contains(r.URL.Path, "/bundles/state/") && strings.HasSuffix(r.URL.Path, "/values"):
		b.valueCreates++
		if b.createFails {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"no permission"}`)
			return
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if name, ok := in["name"].(string); ok {
			b.values = append(b.values, name)
		}
		_, _ = io.WriteString(w, `{"id":"v-9"}`)

	case strings.Contains(r.URL.Path, "/issues/"):
		b.setAttempts++
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if b.hasValue(wantedState(in)) {
			_, _ = io.WriteString(w, `{"idReadable":"PROJ-1"}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"value not found in bundle"}`)

	default:
		_, _ = io.WriteString(w, `{}`)
	}
}

func (b *bundleServer) hasValue(v string) bool {
	for _, have := range b.values {
		if strings.EqualFold(have, v) {
			return true
		}
	}
	return false
}

func wantedState(in map[string]any) string {
	fields, _ := in["customFields"].([]any)
	for _, f := range fields {
		m, _ := f.(map[string]any)
		val, _ := m["value"].(map[string]any)
		if name, ok := val["name"].(string); ok {
			return name
		}
	}
	return ""
}

func bundleClient(t *testing.T, b *bundleServer) *YouTrackClient {
	t.Helper()
	t.Setenv(ytTokenEnv, "secret")
	c, err := NewYouTrack(Config{APIBase: b.srv.URL, AuthEnv: ytTokenEnv, Project: "PROJ", Hosts: allowingSelf(b.srv.URL)})
	if err != nil {
		t.Fatalf("NewYouTrack: %v", err)
	}
	return c
}

// TestMissingStateIsCreatedInThePrivateBundle is c-4: the value dross emits is
// not in a stock project, and skipping it silently reports a state change that
// did not happen.
func TestMissingStateIsCreatedInThePrivateBundle(t *testing.T) {
	b := newBundleServer(t, []string{"Open", "In Progress"}, 1)
	c := bundleClient(t, b)

	if err := c.SetState("PROJ-1", "uat", nil); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.valueCreates != 1 {
		t.Errorf("bundle value creates = %d, want 1 — the missing state was skipped rather than added", b.valueCreates)
	}
	if b.setAttempts != 2 {
		t.Errorf("issue set attempts = %d, want 2 — the retry after creating the value is what makes the state actually land", b.setAttempts)
	}
	if !b.hasValue("UAT") {
		t.Errorf("the bundle does not hold the created value: %v", b.values)
	}
}

// TestSharedBundleIsNeverWritten: a shared bundle is other projects'
// configuration. Adding to it changes what their boards and workflows read —
// a change nobody in them asked for.
func TestSharedBundleIsNeverWritten(t *testing.T) {
	b := newBundleServer(t, []string{"Open"}, 3) // three projects share it
	c := bundleClient(t, b)

	stderr := captureStderrForge(t, func() {
		if err := c.SetState("PROJ-1", "uat", nil); err != nil {
			t.Fatalf("a shared bundle must warn and continue, not fail: %v", err)
		}
	})

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.valueCreates != 0 {
		t.Errorf("dross wrote to a bundle shared by %d projects", b.projects)
	}
	if !strings.Contains(stderr, "shared") {
		t.Errorf("the warning does not say why it declined:\n%s", stderr)
	}
}

// TestPresentStateSkipsTheCreate: a value already in the bundle must not be
// re-created. A write on every sync would churn the tracker's configuration
// for nothing.
func TestPresentStateSkipsTheCreate(t *testing.T) {
	b := newBundleServer(t, []string{"Open", "In Progress"}, 1)
	c := bundleClient(t, b)

	if err := c.SetState("PROJ-1", "in-progress", nil); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.valueCreates != 0 {
		t.Errorf("a present value triggered %d bundle write(s)", b.valueCreates)
	}
	if b.setAttempts != 1 {
		t.Errorf("set attempts = %d, want 1 — a value already there needs no retry", b.setAttempts)
	}
}

// TestBundleCreateFailureWarnsAndContinues: a tracker whose configuration dross
// may not edit is a capability gap, not an outage. The phase-level record is
// still worth writing.
func TestBundleCreateFailureWarnsAndContinues(t *testing.T) {
	b := newBundleServer(t, []string{"Open"}, 1)
	b.createFails = true
	c := bundleClient(t, b)

	stderr := captureStderrForge(t, func() {
		if err := c.SetState("PROJ-1", "uat", nil); err != nil {
			t.Fatalf("a failed bundle create must not fail the sync: %v", err)
		}
	})
	if !strings.Contains(stderr, "could not add State value") {
		t.Errorf("the failure was silent:\n%s", stderr)
	}
	if !strings.Contains(stderr, "UAT") {
		t.Errorf("the warning does not name the value it could not add:\n%s", stderr)
	}
}

// TestUnmappedStateStillSkipsWithoutTouchingTheBundle: a dross status with no
// mapping at all was always a warn-and-skip, and it must not now reach for the
// bundle — there is no value to create, only an unmapped name.
func TestUnmappedStateStillSkipsWithoutTouchingTheBundle(t *testing.T) {
	b := newBundleServer(t, []string{"Open"}, 1)
	c := bundleClient(t, b)

	if err := c.SetState("PROJ-1", "not-a-dross-status", nil); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.setAttempts != 0 || b.valueCreates != 0 {
		t.Errorf("an unmapped status reached the tracker: sets=%d creates=%d", b.setAttempts, b.valueCreates)
	}
}

// captureStderrForge is this package's copy of the stderr capture the cmd tests
// use. The warnings under test go to stderr by design — they are gaps a human
// should see, not values a caller branches on — so reading them is the only way
// to assert they were said.
func captureStderrForge(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	done := make(chan string)
	go func() {
		var buf strings.Builder
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	os.Stderr = orig
	return <-done
}
