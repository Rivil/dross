package ship

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

func TestPostCommentRejectsEmptyBody(t *testing.T) {
	err := PostComment(CommentOpts{Provider: "github", PRNumber: 1, Body: ""})
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected empty-body error, got: %v", err)
	}
}

func TestPostCommentRejectsZeroPRNumber(t *testing.T) {
	err := PostComment(CommentOpts{Provider: "github", PRNumber: 0, Body: "hi"})
	if err == nil || !strings.Contains(err.Error(), "PRNumber") {
		t.Errorf("expected PRNumber error, got: %v", err)
	}
}

func TestPostCommentRejectsUnknownProvider(t *testing.T) {
	err := PostComment(CommentOpts{Provider: "weird", PRNumber: 1, Body: "hi"})
	if err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("expected unsupported-provider error, got: %v", err)
	}
}

func TestPostForgejoCommentHappyPath(t *testing.T) {
	var got struct {
		Method string
		Path   string
		Body   string
		Auth   string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Method = r.Method
		got.Path = r.URL.Path
		got.Auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(b, &parsed)
		got.Body, _ = parsed["body"].(string)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 42}`))
	}))
	defer srv.Close()

	t.Setenv("FAKE_TOKEN", "tok123")
	err := PostComment(CommentOpts{
		Provider: "forgejo",
		URL:      "https://forge.example.com/me/proj",
		APIBase:  srv.URL,
		AuthEnv:  "FAKE_TOKEN",
		PRNumber: 7,
		Body:     "subagent panel findings",
	})
	if err != nil {
		t.Fatalf("PostComment: %v", err)
	}
	if got.Method != "POST" {
		t.Errorf("method: %q", got.Method)
	}
	if got.Path != "/repos/me/proj/issues/7/comments" {
		t.Errorf("path: %q", got.Path)
	}
	if got.Auth != "token tok123" {
		t.Errorf("auth header: %q", got.Auth)
	}
	if got.Body != "subagent panel findings" {
		t.Errorf("body: %q", got.Body)
	}
}

func TestPostForgejoCommentMissingTokenSurfacesEnvVar(t *testing.T) {
	err := PostComment(CommentOpts{
		Provider: "forgejo",
		URL:      "https://x.example/o/r",
		APIBase:  "https://api.example",
		AuthEnv:  "DROSS_TEST_NO_SUCH_VAR",
		PRNumber: 1,
		Body:     "x",
	})
	if err == nil || !strings.Contains(err.Error(), "DROSS_TEST_NO_SUCH_VAR") {
		t.Errorf("expected missing-env error mentioning the var name, got: %v", err)
	}
}

// TestPostGitHubCommentInvokesGh swaps the ghCommand factory to a
// no-op `true` (always-success) and verifies the args are correct.
func TestPostGitHubCommentInvokesGh(t *testing.T) {
	var capturedArgs []string
	prev := ghCommand
	ghCommand = func(args ...string) *exec.Cmd {
		capturedArgs = append([]string{}, args...)
		// Use `true` so the stub command exits 0 without doing anything.
		return exec.Command("true")
	}
	defer func() { ghCommand = prev }()

	err := PostComment(CommentOpts{
		Provider: "github",
		PRNumber: 42,
		Body:     "hello",
	})
	if err != nil {
		t.Fatalf("PostComment github: %v", err)
	}
	want := []string{"pr", "comment", "42", "--body", "hello"}
	if len(capturedArgs) != len(want) {
		t.Fatalf("arg count: got %d want %d (%v)", len(capturedArgs), len(want), capturedArgs)
	}
	for i := range want {
		if capturedArgs[i] != want[i] {
			t.Errorf("arg %d: got %q want %q", i, capturedArgs[i], want[i])
		}
	}
}
