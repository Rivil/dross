package ship

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Rivil/dross/internal/hostallow"
	"github.com/Rivil/dross/internal/secretscan"
)

// Synthesized secret shapes, ASSEMBLED at runtime: a literal token shape in a
// test file is exactly what the phase's self-scan trips on.
func synthGitHubToken() string { return "ghp_" + strings.Repeat("A1b2", 9) }
func synthSlackToken() string  { return "xoxb-" + strings.Repeat("1234", 4) }
func synthPEMHeader() string {
	return strings.Repeat("-", 5) + "BEGIN RSA PRIVATE KEY" + strings.Repeat("-", 5)
}

// benignBody is the shape a PR body carries — an identity id, a SHA and a UUID.
// None of it is a credential and all of it must post.
const benignBody = "## Phase\n\n" +
	"issue id: 0123456789abcdef\n" +
	"commit: 0123456789abcdef0123456789abcdef01234567\n" +
	"run: 123e4567-e89b-12d3-a456-426614174000\n"

// httpProvider is one REST-backed ship provider with the opts that point it at
// an httptest server.
type httpProvider struct {
	name         string
	commentField string // JSON path the comment body travels in
	open         func(srv string) OpenOpts
	comment      func(srv string) CommentOpts
}

func httpProviders(t *testing.T) []httpProvider {
	t.Helper()
	t.Setenv("MOCK_SHIP_TOKEN", "tok123")
	return []httpProvider{
		{
			name:         "forgejo",
			commentField: "body",
			open: func(srv string) OpenOpts {
				return OpenOpts{Provider: "forgejo", URL: "https://forge.example/me/proj", APIBase: srv, Hosts: hostallow.Derive(srv, nil), AuthEnv: "MOCK_SHIP_TOKEN", HeadBranch: "phase/x", BaseBranch: "main"}
			},
			comment: func(srv string) CommentOpts {
				return CommentOpts{Provider: "forgejo", URL: "https://forge.example/me/proj", APIBase: srv, Hosts: hostallow.Derive(srv, nil), AuthEnv: "MOCK_SHIP_TOKEN", PRNumber: 7}
			},
		},
		{
			name:         "gitlab",
			commentField: "body",
			open: func(srv string) OpenOpts {
				return OpenOpts{Provider: "gitlab", URL: "https://gitlab.example/me/proj", APIBase: srv, Hosts: hostallow.Derive(srv, nil), AuthEnv: "MOCK_SHIP_TOKEN", HeadBranch: "phase/x", BaseBranch: "main"}
			},
			comment: func(srv string) CommentOpts {
				return CommentOpts{Provider: "gitlab", URL: "https://gitlab.example/me/proj", APIBase: srv, Hosts: hostallow.Derive(srv, nil), AuthEnv: "MOCK_SHIP_TOKEN", PRNumber: 7}
			},
		},
		{
			name:         "bitbucket",
			commentField: "content.raw",
			open: func(srv string) OpenOpts {
				return OpenOpts{Provider: "bitbucket", URL: "https://bitbucket.org/acme/widget", APIBase: srv, Hosts: hostallow.Derive(srv, nil), AuthEnv: "MOCK_SHIP_TOKEN", AuthUser: "wsuser", HeadBranch: "phase/x", BaseBranch: "main"}
			},
			comment: func(srv string) CommentOpts {
				return CommentOpts{Provider: "bitbucket", URL: "https://bitbucket.org/acme/widget", APIBase: srv, Hosts: hostallow.Derive(srv, nil), AuthEnv: "MOCK_SHIP_TOKEN", AuthUser: "wsuser", PRNumber: 7}
			},
		},
	}
}

// countingServer records every request and answers with a shape each
// provider's response decoder accepts.
func countingServer(t *testing.T, n *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1,"iid":1,"number":1,"html_url":"https://x/pull/1","web_url":"https://x/-/merge_requests/1","links":{"html":{"href":"https://x/pull-requests/1"}}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// requireRefused asserts err carries a *secretscan.ErrHit with a hit naming
// rule whose rendered fingerprint (`<rule> at <location>:<line> …`) contains
// locPart.
func requireRefused(t *testing.T, err error, rule, locPart string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected refusal, got nil error")
	}
	var hit *secretscan.ErrHit
	if !errors.As(err, &hit) {
		t.Fatalf("error is not a *secretscan.ErrHit: %v", err)
	}
	for _, h := range hit.Hits {
		if h.Rule == rule && strings.Contains(h.String(), locPart) {
			return
		}
	}
	t.Fatalf("no hit with rule %q and location containing %q in %v", rule, locPart, hit.Hits)
}

// TestOpenPRGitHubRefusedBeforeGh pins that the argv scan runs before exec:
// a token on line 3 of the body is refused at `gh --body:3` and gh is never
// spawned.
func TestOpenPRGitHubRefusedBeforeGh(t *testing.T) {
	stubGhOnPath(t)
	refuseGh(t)
	_, err := OpenPR(OpenOpts{Provider: "github", Title: "t", Body: "line1\nline2\n" + synthGitHubToken(), HeadBranch: "phase/x", BaseBranch: "main"})
	requireRefused(t, err, "github-token", "gh --body:3")
}

// TestOpenPRTitleIsScreenedToo pins that every argv element is scanned, not
// only the body.
func TestOpenPRTitleIsScreenedToo(t *testing.T) {
	stubGhOnPath(t)
	refuseGh(t)
	_, err := OpenPR(OpenOpts{Provider: "github", Title: synthGitHubToken(), Body: "clean", HeadBranch: "phase/x", BaseBranch: "main"})
	requireRefused(t, err, "github-token", "gh --title:1")
}

// TestPostCommentRefused pins the comment path on every provider: a PEM header
// in the body is refused with zero invocations / zero requests.
func TestPostCommentRefused(t *testing.T) {
	t.Run("github", func(t *testing.T) {
		refuseGh(t)
		err := PostComment(CommentOpts{Provider: "github", PRNumber: 7, Body: synthPEMHeader()})
		requireRefused(t, err, "pem-private-key", "gh --body:1")
	})
	for _, p := range httpProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			var n atomic.Int32
			srv := countingServer(t, &n)
			opts := p.comment(srv.URL)
			opts.Body = "findings\n" + synthPEMHeader()
			err := PostComment(opts)
			requireRefused(t, err, "pem-private-key", ":"+p.commentField+":2")
			if n.Load() != 0 {
				t.Fatalf("%d request(s) reached the server", n.Load())
			}
		})
	}
}

// TestOpenPRForgejoGitLabBitbucketRefused pins that every REST transport
// carries the screen, not only jsonPost: a slack-shaped body is refused on
// each provider with zero requests.
func TestOpenPRForgejoGitLabBitbucketRefused(t *testing.T) {
	for _, p := range httpProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			var n atomic.Int32
			srv := countingServer(t, &n)
			opts := p.open(srv.URL)
			opts.Title, opts.Body = "t", "hook: "+synthSlackToken()
			_, err := OpenPR(opts)
			requireRefused(t, err, "slack-token", "POST ")
			if n.Load() != 0 {
				t.Fatalf("%d request(s) reached the server", n.Load())
			}
		})
	}
}

// TestOpenPRBenignBodyStillPosts pins that the gate does not break ordinary
// shipping: ids, SHAs and UUIDs reach the transport on every provider.
func TestOpenPRBenignBodyStillPosts(t *testing.T) {
	t.Run("github", func(t *testing.T) {
		stubGhOnPath(t)
		got := captureGh(t, "https://github.com/o/r/pull/9\n")
		if _, err := OpenPR(OpenOpts{Provider: "github", Title: "phase", Body: benignBody, HeadBranch: "phase/x", BaseBranch: "main"}); err != nil {
			t.Fatalf("benign body refused: %v", err)
		}
		if len(*got) == 0 {
			t.Fatal("gh was never invoked")
		}
	})
	for _, p := range httpProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			var n atomic.Int32
			srv := countingServer(t, &n)
			opts := p.open(srv.URL)
			opts.Title, opts.Body = "phase", benignBody
			if _, err := OpenPR(opts); err != nil {
				t.Fatalf("benign body refused: %v", err)
			}
			if n.Load() != 1 {
				t.Fatalf("want exactly 1 request, got %d", n.Load())
			}
		})
	}
}

// TestNoRawGhCommandCallOutsideTheSeam pins, over the AST of every non-test
// file in this package, that the only call whose callee is the identifier
// ghCommand sits inside screenedGH. A call site that goes around the screen
// is named by file:line.
func TestNoRawGhCommandCallOutsideTheSeam(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "ghCommand" {
					calls++
					if fn.Name.Name != "screenedGH" {
						t.Errorf("%s: raw ghCommand( call outside screenedGH, in %s", fset.Position(call.Pos()), fn.Name.Name)
					}
				}
				return true
			})
		}
	}
	if calls != 1 {
		t.Fatalf("want exactly one ghCommand( call (inside screenedGH), found %d", calls)
	}
}

// TestAllowMarkerOnBodyLine pins the marker route on a composed body: a token
// line that ends with the marker is accepted and gh is invoked once.
func TestAllowMarkerOnBodyLine(t *testing.T) {
	stubGhOnPath(t)
	got := captureGh(t, "https://github.com/o/r/pull/9\n")
	body := "intro\nexample: " + synthGitHubToken() + " " + secretscan.AllowMarker + "\n"
	if _, err := OpenPR(OpenOpts{Provider: "github", Title: "t", Body: body, HeadBranch: "phase/x", BaseBranch: "main"}); err != nil {
		t.Fatalf("marked line refused: %v", err)
	}
	if len(*got) == 0 {
		t.Fatal("gh was never invoked")
	}
}
