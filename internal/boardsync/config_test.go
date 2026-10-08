package boardsync

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// TestBoardConfigDerivesFromRemoteURL: Config synthesises a
// "https://board.local/<project>" URL to carry owner/repo to the forge
// backends. Deriving the allowlist from THAT would authorize a host nobody
// configured and make the policy self-satisfying, so the derivation source is
// the real [remote].url.
func TestBoardConfigDerivesFromRemoteURL(t *testing.T) {
	cfg := Config(project.Board{
		Provider: "forgejo",
		BaseURL:  "https://git.corp.internal/api/v1",
		AuthEnv:  "TOKEN",
		Project:  "me/proj",
	}, "https://git.corp.internal/me/proj", nil)

	allowed := strings.Join(cfg.Hosts.Allowed(), " ")
	if !strings.Contains(allowed, "git.corp.internal") {
		t.Errorf("policy does not carry the [remote].url host: %v", cfg.Hosts.Allowed())
	}
	if strings.Contains(allowed, "board.local") {
		t.Errorf("policy was derived from the synthetic board URL: %v", cfg.Hosts.Allowed())
	}
	// And the synthetic URL is still doing its real job.
	if cfg.URL != "https://board.local/me/proj" {
		t.Errorf("synthetic owner/repo URL changed: %q", cfg.URL)
	}
}

// TestConfigGitLabCarriesProjectID: the GitLab backend addresses a project by
// id/path rather than owner/repo, so Config copies Project into ProjectID for
// it alone — the forge backends must not see one.
func TestConfigGitLabCarriesProjectID(t *testing.T) {
	gl := Config(project.Board{Provider: "gitlab", BaseURL: "https://gitlab.example/api/v4", Project: "group/proj"}, "https://gitlab.example/group/proj", nil)
	if gl.ProjectID != "group/proj" {
		t.Errorf("gitlab ProjectID = %q, want the project ref", gl.ProjectID)
	}
	fj := Config(project.Board{Provider: "forgejo", BaseURL: "https://git.example/api/v1", Project: "me/proj"}, "https://git.example/me/proj", nil)
	if fj.ProjectID != "" {
		t.Errorf("forgejo ProjectID = %q, want empty", fj.ProjectID)
	}
	// Machine-local extras widen the allowlist; the board's own base_url does not.
	extra := Config(project.Board{Provider: "forgejo", BaseURL: "https://board.example/api/v1", Project: "me/proj"}, "https://git.example/me/proj", []string{"board.example"})
	if !strings.Contains(strings.Join(extra.Hosts.Allowed(), " "), "board.example") {
		t.Errorf("an allow_hosts extra did not reach the policy: %v", extra.Hosts.Allowed())
	}
}
