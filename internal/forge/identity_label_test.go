package forge

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// issueLister is the slice of a board client these tests drive.
type issueLister interface {
	ListIssues(IssueFilter) ([]Issue, error)
}

// labelIndexProviders builds each provider's client over an httptest label
// index holding exactly `known`, counting every request that is not an index
// read — an issue-list query — into *issueCalls.
var labelIndexProviders = []struct {
	name string
	make func(t *testing.T, known []string, issueCalls *int) issueLister
}{
	{"forgejo", func(t *testing.T, known []string, issueCalls *int) issueLister {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				var idx []map[string]any
				for i, k := range known {
					idx = append(idx, map[string]any{"id": i + 1, "name": k})
				}
				writeJSON(t, w, idx)
				return
			}
			*issueCalls++
			_, _ = io.WriteString(w, `[]`)
		})
		return c
	}},
	{"github", func(t *testing.T, known []string, issueCalls *int) issueLister {
		c, _ := newTestGitHubClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/labels") {
				writeJSON(t, w, namedList(known))
				return
			}
			*issueCalls++
			_, _ = io.WriteString(w, `[]`)
		})
		return c
	}},
	{"jira", func(t *testing.T, known []string, issueCalls *int) issueLister {
		c, _ := newTestJiraClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/label") {
				writeJSON(t, w, map[string]any{"values": known})
				return
			}
			*issueCalls++
			_, _ = io.WriteString(w, `{"issues":[]}`)
		})
		return c
	}},
	{"youtrack", func(t *testing.T, known []string, issueCalls *int) issueLister {
		c, _ := newTestYTClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/issueTags") {
				writeJSON(t, w, namedList(known))
				return
			}
			*issueCalls++
			_, _ = io.WriteString(w, `[]`)
		})
		return c
	}},
}

func namedList(names []string) []map[string]string {
	out := make([]map[string]string, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]string{"name": n})
	}
	return out
}

func writeJSON(t *testing.T, w io.Writer, v any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode fake response: %v", err)
	}
}

// TestIdentityLabelLookupIsSilent pins c-3: an identity label the board has
// never heard of means no card exists yet. The lookup resolves to nothing,
// issues no query, and says nothing — on every provider.
func TestIdentityLabelLookupIsSilent(t *testing.T) {
	for _, p := range labelIndexProviders {
		t.Run(p.name, func(t *testing.T) {
			var calls int
			c := p.make(t, []string{"bug"}, &calls)
			var got []Issue
			warn := captureForgeStderr(t, func() {
				var err error
				if got, err = c.ListIssues(IssueFilter{Labels: []string{"dross/task:p/t-1"}}); err != nil {
					t.Fatalf("ListIssues: %v", err)
				}
			})
			if len(got) != 0 {
				t.Errorf("returned %+v, want no issues", got)
			}
			if calls != 0 {
				t.Errorf("issued %d issue-list requests, want 0", calls)
			}
			if warn != "" {
				t.Errorf("stderr = %q, want nothing for an unknown identity label", warn)
			}
		})
	}
}

// TestUnknownFilterLabelStillWarns keeps the warning where it means something:
// a genuine filter label the board lacks is a stale filter, and is named.
func TestUnknownFilterLabelStillWarns(t *testing.T) {
	for _, p := range labelIndexProviders {
		t.Run(p.name, func(t *testing.T) {
			var calls int
			c := p.make(t, []string{"other"}, &calls)
			warn := captureForgeStderr(t, func() {
				if _, err := c.ListIssues(IssueFilter{Labels: []string{"bug"}}); err != nil {
					t.Fatalf("ListIssues: %v", err)
				}
			})
			if !strings.Contains(warn, "does not know the label(s) bug") {
				t.Errorf("stderr = %q, want the unknown filter label named", warn)
			}
		})
	}
}

// TestMixedQueryWarnsOnlyFilterLabel: when an identity label and a filter label
// are both unknown, only the filter label is named.
func TestMixedQueryWarnsOnlyFilterLabel(t *testing.T) {
	for _, p := range labelIndexProviders {
		t.Run(p.name, func(t *testing.T) {
			var calls int
			c := p.make(t, []string{"other"}, &calls)
			warn := captureForgeStderr(t, func() {
				if _, err := c.ListIssues(IssueFilter{Labels: []string{"dross/phase:x", "bug"}}); err != nil {
					t.Fatalf("ListIssues: %v", err)
				}
			})
			if !strings.Contains(warn, "bug") {
				t.Errorf("stderr = %q, want bug named", warn)
			}
			if strings.Contains(warn, "dross/phase:x") {
				t.Errorf("stderr = %q names the identity label", warn)
			}
		})
	}
}

// TestSharedDrossLabelsStillWarn: the dross/ prefix alone is not identity. The
// status labels, dross/quick and the bare marker are shared by many cards, so a
// board lacking one is a stale filter and still warns.
func TestSharedDrossLabelsStillWarn(t *testing.T) {
	for _, l := range []string{"dross/status:done", "dross/quick", "dross"} {
		t.Run(l, func(t *testing.T) {
			warn := captureForgeStderr(t, func() { WarnDroppedLabels("youtrack", []string{l}) })
			if !strings.Contains(warn, "does not know the label(s) "+l) {
				t.Errorf("stderr = %q, want %s named", warn, l)
			}
		})
	}
}

func TestIsIdentityLabel(t *testing.T) {
	for _, tc := range []struct {
		label string
		want  bool
	}{
		{"dross/task:p/t-1", true},
		{"dross/phase:p", true},
		{"dross/deferred:abc", true},
		{"dross/target:slug", true},
		{"dross/status:done", false},
		{"dross/quick", false},
		{"dross", false},
		{"dross/phase", false},
		{"bug", false},
		{"x-dross/task:p/t-1", false},
	} {
		if got := IsIdentityLabel(tc.label); got != tc.want {
			t.Errorf("IsIdentityLabel(%q) = %v, want %v", tc.label, got, tc.want)
		}
	}
}
