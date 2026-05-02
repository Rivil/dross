package project

import (
	"net/url"
	"strings"
)

// KnownHostProviders maps hostname → provider for hosts dross can identify
// without asking. Anything not in this list is unknown — caller should ask
// the user (likely a self-hosted instance).
var KnownHostProviders = map[string]string{
	"github.com":    "github",
	"codeberg.org":  "forgejo",
	"bitbucket.org": "bitbucket",
}

// DetectRemote parses a git remote URL (https or ssh form) and returns a
// best-effort Remote with URL, Provider, APIBase pre-filled.
//
// Recognised provider hosts get Public=true; unknown hosts (likely
// self-hosted Forgejo / Gitea) leave Provider="" and Public=false so the
// caller knows to prompt.
func DetectRemote(remoteURL string) Remote {
	r := Remote{}
	host, path := parseGitRemote(remoteURL)
	if host == "" {
		return r
	}

	r.URL = "https://" + host + "/" + strings.TrimSuffix(path, ".git")

	if provider, ok := KnownHostProviders[host]; ok {
		r.Provider = provider
		r.Public = true
		switch provider {
		case "github":
			r.APIBase = "https://api.github.com"
		case "forgejo", "gitea":
			r.APIBase = "https://" + host + "/api/v1"
		case "bitbucket":
			r.APIBase = "https://api.bitbucket.org/2.0"
		}
	}
	return r
}

// parseGitRemote extracts host and "owner/repo" from any common git URL form:
//   - https://host/owner/repo(.git)
//   - https://user@host/owner/repo(.git)
//   - git@host:owner/repo(.git)
//   - ssh://git@host/owner/repo(.git)
//
// Returns empty strings on parse failure.
func parseGitRemote(raw string) (host, path string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	// scp-like form: git@host:owner/repo
	if !strings.Contains(raw, "://") && strings.Contains(raw, "@") && strings.Contains(raw, ":") {
		afterAt := raw[strings.Index(raw, "@")+1:]
		colon := strings.Index(afterAt, ":")
		if colon < 0 {
			return "", ""
		}
		return afterAt[:colon], afterAt[colon+1:]
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", ""
	}
	return u.Host, strings.TrimPrefix(u.Path, "/")
}
