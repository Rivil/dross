package forge

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/hostallow"
	"github.com/Rivil/dross/internal/project"
)

// The forge half of the c-5 suite: every board/forge constructor, driven from
// the SAME committed fixture the cmd-side suite uses, rather than from a config
// literal composed here.
//
// That matters. A test that builds its own hostile Config proves the guard
// works on the input the test author imagined; reading the fixture proves it
// works on the input the fixture pins — which is the artifact a future reader
// will change, and the one the red replay reproduces.

const hostileFixture = "../../fixtures/hostile-config-c5/project.toml"

func loadHostileFixture(t *testing.T) *project.Project {
	t.Helper()
	path := filepath.Clean(hostileFixture)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture missing at %s: %v", path, err)
	}
	p, err := project.Load(path)
	if err != nil {
		t.Fatalf("load hostile fixture: %v", err)
	}
	if p.Remote.APIBase != "https://attacker.example" || p.Board.BaseURL != "https://attacker.example" {
		t.Fatalf("fixture no longer carries the redirect payload: api_base=%q base_url=%q",
			p.Remote.APIBase, p.Board.BaseURL)
	}
	if p.Remote.URL == p.Remote.APIBase {
		t.Fatal("fixture's [remote].url must stay honest — it is the derivation source the attack diverges from")
	}
	return p
}

// TestHostileConfigForgeClientsRefuse drives all four constructors with the
// fixture's own values and asserts the three things that together mean "the
// token did not leave": a policy refusal, no client, and a fake host that
// recorded no requests.
func TestHostileConfigForgeClientsRefuse(t *testing.T) {
	var hits int
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(fake.Close)

	p := loadHostileFixture(t)
	const token = "s3cr3t-fixture-token-do-not-leak"
	t.Setenv(p.Remote.AuthEnv, token)

	policy := hostallow.Derive(p.Remote.URL, nil)

	for _, tc := range []struct {
		name string
		call func() (any, error)
	}{
		{"forge.New", func() (any, error) {
			c, err := New(Config{
				Provider: "forgejo", URL: p.Remote.URL, APIBase: p.Remote.APIBase,
				AuthEnv: p.Remote.AuthEnv, Hosts: policy,
			})
			return c, err
		}},
		{"NewGitHubProjects", func() (any, error) {
			c, err := NewGitHubProjects(Config{
				APIBase: p.Board.BaseURL, AuthEnv: p.Board.AuthEnv,
				Project: p.Board.Project, Hosts: policy,
			})
			return c, err
		}},
		{"NewJira", func() (any, error) {
			c, err := NewJira(Config{
				APIBase: p.Board.BaseURL, AuthEnv: p.Board.AuthEnv,
				Project: "PROJ", AuthUser: "me@example.com", Hosts: policy,
			})
			return c, err
		}},
		{"NewYouTrack", func() (any, error) {
			c, err := NewYouTrack(Config{
				APIBase: p.Board.BaseURL, AuthEnv: p.Board.AuthEnv,
				Project: "PROJ", Hosts: policy,
			})
			return c, err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits = 0
			_, err := tc.call()
			if err == nil {
				t.Fatal("the constructor accepted the fixture's attacker-controlled base")
			}
			if !errors.Is(err, hostallow.ErrRefused) {
				t.Errorf("not a policy refusal: %v", err)
			}
			if !strings.Contains(err.Error(), "attacker.example") {
				t.Errorf("refusal does not name the host: %v", err)
			}
			if strings.Contains(err.Error(), token) {
				t.Errorf("the refusal echoed the token: %v", err)
			}
			if hits != 0 {
				t.Errorf("the fake host saw %d request(s) — the refusal came after the socket", hits)
			}
		})
	}
}

// TestHostileConfigHonestRemoteStillWorks is the over-refusal gate on the same
// fixture: swap only the attacker-controlled key back to the repo's own host
// and every constructor must build. A guard that also breaks the honest
// configuration is a guard that gets reverted.
func TestHostileConfigHonestRemoteStillWorks(t *testing.T) {
	p := loadHostileFixture(t)
	t.Setenv(p.Remote.AuthEnv, "token")
	policy := hostallow.Derive(p.Remote.URL, nil)

	if _, err := NewGitHubProjects(Config{
		APIBase: "https://api.github.com", AuthEnv: p.Remote.AuthEnv,
		Project: p.Board.Project, Hosts: policy,
	}); err != nil {
		t.Errorf("the honest github.com/api.github.com pair was refused: %v", err)
	}
}
