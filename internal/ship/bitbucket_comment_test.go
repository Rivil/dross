package ship

import (
	"encoding/json"
	"github.com/Rivil/dross/internal/hostallow"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/configenum"
)

func TestPostBitbucketCommentHappyPath(t *testing.T) {
	t.Setenv("MOCK_BB_TOKEN", "secret")

	var (
		posted   bool
		gotPath  string
		gotBody  map[string]any
		gotAuth  string
		strayHdr []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if v := r.Header.Get("PRIVATE-TOKEN"); v != "" {
			strayHdr = append(strayHdr, "PRIVATE-TOKEN: "+v)
		}
		body, _ := io.ReadAll(r.Body)
		posted = true
		gotPath = r.URL.Path
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":9}`))
	}))
	t.Cleanup(server.Close)

	err := PostComment(CommentOpts{
		Provider: "bitbucket", URL: "https://bitbucket.org/acme/widget", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_BB_TOKEN", AuthUser: "wsuser",
		PRNumber: 42, Body: "panel findings",
	})
	if err != nil {
		t.Fatalf("PostComment: %v", err)
	}
	if !posted {
		t.Fatal("no comment POST was made")
	}

	// Bitbucket keys PR comments off the pullrequests endpoint. Forgejo's
	// shared issue/PR number space has no equivalent here, so an
	// /issues/{n}/comments path 404s against a real host.
	if want := "/repositories/acme/widget/pullrequests/42/comments"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAuth != wantBasic("wsuser", "secret") {
		t.Errorf("Authorization = %q, want %q", gotAuth, wantBasic("wsuser", "secret"))
	}
	if len(strayHdr) > 0 {
		t.Errorf("a second auth header was sent alongside Basic: %v", strayHdr)
	}

	// The body nests under content.raw; the flat {body:...} shape every other
	// backend uses is silently accepted by nothing on this API.
	if _, flat := gotBody["body"]; flat {
		t.Error("payload carries a flat body field — that is the GitHub/Forgejo shape")
	}
	content, _ := gotBody["content"].(map[string]any)
	if got := content["raw"]; got != "panel findings" {
		t.Errorf("content.raw = %v, want %q", got, "panel findings")
	}
}

func TestPostBitbucketCommentMissingAuthUser(t *testing.T) {
	t.Setenv("MOCK_BB_TOKEN", "secret")

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	t.Cleanup(server.Close)

	err := PostComment(CommentOpts{
		Provider: "bitbucket", URL: "https://bitbucket.org/acme/widget", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv:  "MOCK_BB_TOKEN", // AuthUser deliberately empty
		PRNumber: 1, Body: "x",
	})
	if err == nil {
		t.Fatal("expected an error when auth_user is unset")
	}
	if !strings.Contains(err.Error(), "auth_user") {
		t.Errorf("error should name the missing setting: %v", err)
	}
	// Basic base64(:token) would 401 with a message naming nothing actionable,
	// so the check has to land before the socket opens.
	if requests != 0 {
		t.Errorf("%d request(s) made before the credential check", requests)
	}
}

func TestPostBitbucketCommentNon2xx(t *testing.T) {
	t.Setenv("MOCK_BB_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"no access"}}`))
	}))
	t.Cleanup(server.Close)

	err := PostComment(CommentOpts{
		Provider: "bitbucket", URL: "https://bitbucket.org/acme/widget", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_BB_TOKEN", AuthUser: "wsuser", PRNumber: 1, Body: "x",
	})
	if err == nil {
		t.Fatal("a 403 must not be reported as a posted comment")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should carry the status: %v", err)
	}
}

// TestPRStatusBitbucket pins merged to state == MERGED. Bitbucket closes a PR
// as DECLINED or SUPERSEDED without ever landing it, so a `!= OPEN` test would
// report those as merged and false-complete a phase whose work was discarded.
func TestPRStatusBitbucket(t *testing.T) {
	tests := []struct {
		state string
		want  bool
	}{
		{"MERGED", true},
		{"OPEN", false},
		{"DECLINED", false},
		{"SUPERSEDED", false},
	}
	for _, tc := range tests {
		t.Run(tc.state, func(t *testing.T) {
			t.Setenv("MOCK_BB_TOKEN", "secret")

			var gotPath, gotMethod, gotAuth string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotMethod, gotAuth = r.URL.Path, r.Method, r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":42,"state":"` + tc.state + `"}`))
			}))
			t.Cleanup(server.Close)

			status, err := GetPRStatus(OpenOpts{
				Provider: "bitbucket", URL: "https://bitbucket.org/acme/widget", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
				AuthEnv: "MOCK_BB_TOKEN", AuthUser: "wsuser", PRNumber: 42,
			})
			if err != nil {
				t.Fatalf("GetPRStatus: %v", err)
			}
			if status.Merged != tc.want {
				t.Errorf("state %q: merged = %v, want %v", tc.state, status.Merged, tc.want)
			}
			if want := "/repositories/acme/widget/pullrequests/42"; gotPath != want {
				t.Errorf("path = %q, want %q", gotPath, want)
			}
			if gotMethod != http.MethodGet {
				t.Errorf("method = %q, want GET", gotMethod)
			}
			if gotAuth != wantBasic("wsuser", "secret") {
				t.Errorf("Authorization = %q, want %q", gotAuth, wantBasic("wsuser", "secret"))
			}
		})
	}
}

// TestPRStatusBitbucketIsAuthoritative proves bitbucket left the unsupported
// table. If it rejoined it, ship would fall back to a git-ancestry guess on a
// provider that can answer for itself.
func TestPRStatusBitbucketIsAuthoritative(t *testing.T) {
	t.Setenv("MOCK_BB_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":1,"state":"MERGED"}`))
	}))
	t.Cleanup(server.Close)

	status, err := GetPRStatus(OpenOpts{
		Provider: "bitbucket", URL: "https://bitbucket.org/acme/widget", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_BB_TOKEN", AuthUser: "wsuser", PRNumber: 1,
	})
	if err != nil {
		t.Fatalf("bitbucket must answer authoritatively: %v", err)
	}
	if !status.Merged {
		t.Error("a MERGED PR reported as not merged")
	}
}

func TestPRStatusBitbucketMissingAuthUser(t *testing.T) {
	t.Setenv("MOCK_BB_TOKEN", "secret")

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"id":1,"state":"MERGED"}`))
	}))
	t.Cleanup(server.Close)

	if _, err := GetPRStatus(OpenOpts{
		Provider: "bitbucket", URL: "https://bitbucket.org/acme/widget", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_BB_TOKEN", PRNumber: 1, // AuthUser deliberately empty
	}); err == nil || !strings.Contains(err.Error(), "auth_user") {
		t.Fatalf("expected an auth_user error, got: %v", err)
	}
	if requests != 0 {
		t.Errorf("%d request(s) made before the credential check", requests)
	}
}

// TestCommentAndMergedNormaliseProvider proves both entry points are exactly as
// forgiving as doctor. With bare strings.ToLower a leading-space provider that
// doctor accepts dispatches nowhere.
func TestCommentAndMergedNormaliseProvider(t *testing.T) {
	for _, p := range []string{" bitbucket", "Bitbucket", "BITBUCKET\t"} {
		err := PostComment(CommentOpts{
			Provider: p, URL: "https://bitbucket.org/acme/widget", PRNumber: 1, Body: "x",
		})
		if err == nil {
			t.Fatalf("PostComment %q: expected the backend's own credential error", p)
		}
		if strings.Contains(err.Error(), "unsupported provider") {
			t.Errorf("PostComment %q dispatched nowhere: %v", p, err)
		}

		_, err = GetPRStatus(OpenOpts{
			Provider: p, URL: "https://bitbucket.org/acme/widget", PRNumber: 1,
		})
		if err == nil {
			t.Fatalf("GetPRStatus %q: expected the backend's own credential error", p)
		}
		if strings.Contains(err.Error(), "unsupported provider") {
			t.Errorf("GetPRStatus %q dispatched nowhere: %v", p, err)
		}
	}
}

// TestShipMessagesListShipProviders keeps both unsupported-provider messages
// derived. A hand-typed list omits every arm added after it was written —
// which is exactly how the pre-bitbucket messages went stale.
func TestShipMessagesListShipProviders(t *testing.T) {
	commentErr := PostComment(CommentOpts{Provider: "perforce", PRNumber: 1, Body: "x"})
	if commentErr == nil {
		t.Fatal("expected error for unknown provider")
	}
	_, mergedErr := GetPRStatus(OpenOpts{Provider: "perforce", PRNumber: 1})
	if mergedErr == nil {
		t.Fatal("expected error for unknown provider")
	}

	for name, err := range map[string]error{"PostComment": commentErr, "GetPRStatus": mergedErr} {
		if !strings.Contains(err.Error(), configenum.ShipProviders.List()) {
			t.Errorf("%s message must be derived from ShipProviders, got: %v", name, err)
		}
		if !strings.Contains(err.Error(), "bitbucket") {
			t.Errorf("%s message omits an implemented backend: %v", name, err)
		}
	}
}
