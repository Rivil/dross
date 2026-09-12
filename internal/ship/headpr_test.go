package ship

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/Rivil/dross/internal/hostallow"
)

// TestFindOpenPRByHeadGitHubArgv walks the gh argv by index: `pr list`, the
// head in its own flag's value slot, and `--state open`. A head that escaped
// its slot would be read as a flag; a missing `--state open` would return
// closed PRs as live duplicates.
func TestFindOpenPRByHeadGitHubArgv(t *testing.T) {
	got := captureGh(t, `[{"number":7,"url":"https://github.com/o/r/pull/7"}]`)

	res, err := FindOpenPRByHead(OpenOpts{Provider: "github"}, "phase/x")
	if err != nil {
		t.Fatalf("FindOpenPRByHead: %v", err)
	}
	if res == nil || res.Number != 7 {
		t.Fatalf("got %+v, want Number 7", res)
	}
	if res.URL != "https://github.com/o/r/pull/7" {
		t.Errorf("URL = %q", res.URL)
	}

	argv := *got
	if len(argv) < 2 || argv[0] != "pr" || argv[1] != "list" {
		t.Fatalf("argv does not start with `pr list`: %v", argv)
	}
	if i := indexOf(argv, "--head"); i < 0 || i+1 >= len(argv) || argv[i+1] != "phase/x" {
		t.Errorf("--head's value is not phase/x in its slot: %v", argv)
	}
	if i := indexOf(argv, "--state"); i < 0 || i+1 >= len(argv) || argv[i+1] != "open" {
		t.Errorf("--state is not followed by open: %v", argv)
	}
}

func TestFindOpenPRByHeadGitHubNoResults(t *testing.T) {
	captureGh(t, `[]`)
	res, err := FindOpenPRByHead(OpenOpts{Provider: "github"}, "phase/x")
	if err != nil {
		t.Fatalf("FindOpenPRByHead: %v", err)
	}
	if res != nil {
		t.Errorf("got %+v, want nil for no open PR", res)
	}
}

// A lookup that swallows failure into (nil, nil) lets ship open a duplicate
// on a flaky network.
func TestFindOpenPRByHeadGitHubFailureIsError(t *testing.T) {
	prev := ghCommand
	ghCommand = func(...string) *exec.Cmd { return exec.Command("sh", "-c", "echo boom >&2; exit 1") }
	t.Cleanup(func() { ghCommand = prev })

	res, err := FindOpenPRByHead(OpenOpts{Provider: "github"}, "phase/x")
	if err == nil {
		t.Fatal("a failed gh must be an error, never nil-nil")
	}
	if res != nil {
		t.Errorf("result must be nil on error, got %+v", res)
	}
}

func forgejoHeadOpts(server *httptest.Server) OpenOpts {
	return OpenOpts{
		Provider: "forgejo", URL: "https://forge.example/me/p", APIBase: server.URL, Hosts: hostallow.Derive(server.URL, nil),
		AuthEnv: "MOCK_FORGEJO_TOKEN",
	}
}

func forgejoItem(number int, head string) map[string]any {
	return map[string]any{
		"number": number, "title": "t", "html_url": "u",
		"head": map[string]any{"ref": head}, "base": map[string]any{"ref": "main"},
	}
}

// TestFindOpenPRByHeadForgejoPaginates: the match sits on page 2, so a lister
// that stops at the first full page never finds it.
func TestFindOpenPRByHeadForgejoPaginates(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	page1 := make([]map[string]any, 50)
	for i := range page1 {
		page1[i] = forgejoItem(i+1, "other")
	}
	page1JSON, _ := json.Marshal(page1)
	page2JSON, _ := json.Marshal([]map[string]any{forgejoItem(12, "phase/x")})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "1" {
			_, _ = w.Write(page1JSON)
			return
		}
		_, _ = w.Write(page2JSON)
	}))
	t.Cleanup(server.Close)

	res, err := FindOpenPRByHead(forgejoHeadOpts(server), "phase/x")
	if err != nil {
		t.Fatalf("FindOpenPRByHead: %v", err)
	}
	if res == nil || res.Number != 12 {
		t.Fatalf("got %+v, want Number 12 from page 2", res)
	}
}

func TestFindOpenPRByHeadForgejoNoMatch(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"number":1,"title":"t","html_url":"u","head":{"ref":"other"},"base":{"ref":"main"}}]`))
	}))
	t.Cleanup(server.Close)

	res, err := FindOpenPRByHead(forgejoHeadOpts(server), "phase/x")
	if err != nil {
		t.Fatalf("FindOpenPRByHead: %v", err)
	}
	if res != nil {
		t.Errorf("got %+v, want nil when no head matches", res)
	}
}

func TestFindOpenPRByHeadForgejo500IsError(t *testing.T) {
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	res, err := FindOpenPRByHead(forgejoHeadOpts(server), "phase/x")
	if err == nil {
		t.Fatalf("a 500 must be an error, got %+v", res)
	}
	if res != nil {
		t.Errorf("result must be nil on error, got %+v", res)
	}
}

// Unwired providers report the sentinel, so ship can announce the skip;
// an unknown provider is a plain error, distinguishable from it.
func TestFindOpenPRByHeadUnsupportedVsUnknown(t *testing.T) {
	for _, p := range []string{"gitlab", "bitbucket"} {
		if _, err := FindOpenPRByHead(OpenOpts{Provider: p}, "phase/x"); !errors.Is(err, ErrHeadPRLookupUnsupported) {
			t.Errorf("%s: got %v, want ErrHeadPRLookupUnsupported", p, err)
		}
	}
	_, err := FindOpenPRByHead(OpenOpts{Provider: "sourcehut"}, "phase/x")
	if err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
	if errors.Is(err, ErrHeadPRLookupUnsupported) {
		t.Error("an unknown provider should not report as merely unsupported")
	}
}

// Mirror of TestOpenPRsTargetingFuncDefaultsToOpenPRsTargeting.
func TestFindOpenPRByHeadFuncDefaultsToFindOpenPRByHead(t *testing.T) {
	if FindOpenPRByHeadFunc == nil {
		t.Fatal("FindOpenPRByHeadFunc must be a non-nil overridable var")
	}
	if _, err := FindOpenPRByHeadFunc(OpenOpts{Provider: "bitbucket"}, "phase/x"); !errors.Is(err, ErrHeadPRLookupUnsupported) {
		t.Errorf("FindOpenPRByHeadFunc should delegate to FindOpenPRByHead, got: %v", err)
	}
}
