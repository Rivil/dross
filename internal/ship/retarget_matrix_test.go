package ship

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
)

// TestPRStatusBaseRefAcrossProviders proves each provider's raw wire shape
// flows into PRStatus.BaseRef correctly: baseRefName (github), target_branch
// (gitlab), base.ref (forgejo and gitea), destination.branch.name
// (bitbucket). A misnamed or dropped field fails this table.
func TestPRStatusBaseRefAcrossProviders(t *testing.T) {
	const wantBase = "release/x"

	t.Run("github", func(t *testing.T) {
		prev := ghCommand
		ghCommand = func(args ...string) *exec.Cmd {
			return exec.Command("printf", "%s", `{"state":"MERGED","mergedAt":"2026-07-01T00:00:00Z","baseRefName":"release/x"}`)
		}
		defer func() { ghCommand = prev }()

		status, err := GetPRStatus(OpenOpts{Provider: "github", PRNumber: 7})
		if err != nil {
			t.Fatalf("GetPRStatus: %v", err)
		}
		if status.BaseRef != wantBase {
			t.Errorf("BaseRef = %q, want %q", status.BaseRef, wantBase)
		}
	})

	t.Run("gitlab", func(t *testing.T) {
		t.Setenv("MOCK_GITLAB_TOKEN", "secret")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"state":"merged","target_branch":"release/x"}`))
		}))
		t.Cleanup(server.Close)

		status, err := GetPRStatus(OpenOpts{
			Provider: "gitlab", URL: "https://gitlab.example/me/p", APIBase: server.URL,
			AuthEnv: "MOCK_GITLAB_TOKEN", PRNumber: 7,
		})
		if err != nil {
			t.Fatalf("GetPRStatus: %v", err)
		}
		if status.BaseRef != wantBase {
			t.Errorf("BaseRef = %q, want %q", status.BaseRef, wantBase)
		}
	})

	for _, prov := range []string{"forgejo", "gitea"} {
		t.Run(prov, func(t *testing.T) {
			t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"merged":true,"state":"closed","base":{"ref":"release/x"}}`))
			}))
			t.Cleanup(server.Close)

			status, err := GetPRStatus(OpenOpts{
				Provider: prov, URL: "https://forge.example/me/p", APIBase: server.URL,
				AuthEnv: "MOCK_FORGEJO_TOKEN", PRNumber: 7,
			})
			if err != nil {
				t.Fatalf("GetPRStatus: %v", err)
			}
			if status.BaseRef != wantBase {
				t.Errorf("BaseRef = %q, want %q", status.BaseRef, wantBase)
			}
		})
	}

	t.Run("bitbucket", func(t *testing.T) {
		t.Setenv("MOCK_BB_TOKEN", "secret")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"state":"MERGED","destination":{"branch":{"name":"release/x"}}}`))
		}))
		t.Cleanup(server.Close)

		status, err := GetPRStatus(OpenOpts{
			Provider: "bitbucket", URL: "https://bitbucket.org/acme/widget", APIBase: server.URL,
			AuthEnv: "MOCK_BB_TOKEN", AuthUser: "wsuser", PRNumber: 7,
		})
		if err != nil {
			t.Fatalf("GetPRStatus: %v", err)
		}
		if status.BaseRef != wantBase {
			t.Errorf("BaseRef = %q, want %q", status.BaseRef, wantBase)
		}
	})
}
