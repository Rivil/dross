package ship

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/hostallow"
	"github.com/Rivil/dross/internal/redact"
)

// OpenOpts is everything OpenPR needs across providers.
type OpenOpts struct {
	Provider   string // one of configenum.ShipProviders
	URL        string // canonical https URL of the repo
	APIBase    string // forgejo/gitea/gitlab/bitbucket: base of the REST API; ignored for github
	AuthEnv    string // env var name holding the token; only used for forgejo/gitea/gitlab/bitbucket
	AuthUser   string // bitbucket: account user for HTTP Basic auth (user:token)
	AuthScheme string // gitlab: "private-token" (default) | "bearer"
	ProjectID  string // gitlab: numeric project-id override; empty = derive from URL
	HeadBranch string // e.g. "pr/01-x"
	BaseBranch string // e.g. "main"
	Title      string
	Body       string
	Reviewers  []string
	Draft      bool
	PRNumber   int // GetPRStatus: the PR/MR number whose merged status to look up

	// Hosts is the API host allowlist APIBase is checked against before the
	// token is read. The zero value is not unrestricted — it resolves to
	// hostallow's SaaS defaults — so a caller that forgets it fails closed.
	Hosts hostallow.Policy
}

// OpenResult is the minimal successful response shape.
type OpenResult struct {
	Number int    // PR number on the host
	URL    string // browser URL
}

// OpenPR dispatches to the right backend based on Provider.
func OpenPR(opts OpenOpts) (*OpenResult, error) {
	switch configenum.Normalize(opts.Provider) {
	case "github":
		return openGitHubPR(opts)
	case "forgejo", "gitea":
		return openForgejoPR(opts)
	case "gitlab":
		return openGitLabPR(opts)
	case "bitbucket":
		return openBitbucketPR(opts)
	default:
		return nil, fmt.Errorf("unsupported provider %q (expected %s)", opts.Provider, configenum.ShipProviders.List())
	}
}

// --- GitHub via gh ---

// ghCommand is overridable from tests.
var ghCommand = func(args ...string) *exec.Cmd { return exec.Command("gh", args...) }

func openGitHubPR(opts OpenOpts) (*OpenResult, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errors.New("github backend needs the `gh` CLI on PATH (https://cli.github.com)")
	}
	// Every derived value here sits in the VALUE SLOT of its own flag, which is
	// the other way to fence one: pflag consumes the argument after a string
	// flag verbatim, so a --title of "--json" is a title, not a flag. There is
	// no positional in this argv at all, which is why it carries no `--` — a
	// separator would have nothing to protect and would demote the flags dross
	// chose into positionals. The property that has to hold is that no value
	// ever escapes its slot; TestOpenPRArgvWalk asserts it by index.
	args := []string{
		"pr", "create",
		"--title", opts.Title,
		"--body", opts.Body,
		"--head", opts.HeadBranch,
		"--base", opts.BaseBranch,
	}
	if opts.Draft {
		args = append(args, "--draft")
	}
	for _, r := range opts.Reviewers {
		args = append(args, "--reviewer", r)
	}
	out, err := ghCommand(args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr create: %w\n%s", err, string(out))
	}
	prURL := strings.TrimSpace(string(out))
	// gh prints the URL on the last line; pick that to be safe.
	if lines := strings.Split(prURL, "\n"); len(lines) > 0 {
		prURL = strings.TrimSpace(lines[len(lines)-1])
	}
	return &OpenResult{
		Number: parsePRNumber(prURL),
		URL:    prURL,
	}, nil
}

// --- helpers ---

// parsePRNumber extracts the trailing integer from a PR URL like
// https://github.com/o/r/pull/123. Returns 0 on failure.
func parsePRNumber(url string) int {
	idx := strings.LastIndex(url, "/")
	if idx < 0 {
		return 0
	}
	tail := url[idx+1:]
	n := 0
	for _, c := range tail {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// jsonPost POSTs JSON with a token auth header. Returns parsed
// response body (or the raw bytes via "_raw") on success.
func jsonPost(endpoint, authEnv, token string, body any) (map[string]any, error) {
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(body); err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", endpoint, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "token "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	// Scrubbed HERE, at the one place the body enters the package, rather than at
	// each Errorf that interpolates it. Every caller's `string(respBody)` is then
	// safe by construction — including the ones that are not about HTTP status at
	// all ("response missing iid"), which a per-error-site scrub would miss.
	respBody = []byte(redact.Scrub(string(respBody), authEnv, token))
	if resp.StatusCode >= 300 {
		// The one bare `HTTP %d: %s` in ship, and the one that mirrors back an
		// Authorization header this function set itself a few lines above.
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	out := map[string]any{}
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &out)
	}
	return out, nil
}
