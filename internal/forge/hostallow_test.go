package forge

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/hostallow"
)

// allowingSelf is the policy an HONEST repo has: [remote].url and the API base
// share a host, so the derivation reaches it with no machine-local additions.
//
// Tests whose subject is not the host guard use this rather than an "off"
// switch, so they keep exercising the real derivation. A test helper that
// disabled the check would make every other test in this package blind to a
// regression in it.
func allowingSelf(base string) hostallow.Policy { return hostallow.Derive(base, nil) }

// hostileEnv is the env var the fixture names. Its value is a sentinel: no
// error string produced on a refused path may contain it.
const hostileEnv = "DROSS_FIXTURE_TOKEN"

const sentinelToken = "s3cr3t-sentinel-do-not-leak"

// assertRefusedBeforeToken is the shared assertion for all four constructors:
// the refusal names the host, wraps the policy sentinel, yields no client, and
// — the part that matters most — does not contain the token. A refusal that
// echoed the secret would hand it to whatever reads dross's stderr.
func assertRefusedBeforeToken(t *testing.T, err error, client any) {
	t.Helper()
	if err == nil {
		t.Fatal("constructor accepted an off-allowlist api_base")
	}
	if !errors.Is(err, hostallow.ErrRefused) {
		t.Errorf("refusal does not wrap hostallow.ErrRefused: %v", err)
	}
	if !strings.Contains(err.Error(), "attacker.example") {
		t.Errorf("refusal does not name the host: %v", err)
	}
	if strings.Contains(err.Error(), sentinelToken) {
		t.Errorf("the refusal echoed the token: %v", err)
	}
	if client != nil {
		t.Errorf("a client was returned alongside the refusal: %#v", client)
	}
}

// One test per constructor, not a table over a shared helper: the failure mode
// is "someone added a constructor and forgot the check", and a table over the
// three that do call it passes while the fourth ships unguarded.

func TestForgeNewRefusesOffAllowlistHost(t *testing.T) {
	t.Setenv(hostileEnv, sentinelToken)
	c, err := New(Config{
		Provider: "forgejo",
		URL:      "https://github.com/Rivil/dross",
		APIBase:  "https://attacker.example",
		AuthEnv:  hostileEnv,
	})
	if c == nil {
		assertRefusedBeforeToken(t, err, nil)
		return
	}
	assertRefusedBeforeToken(t, err, c)
}

func TestGitHubProjectsRefusesOffAllowlistHost(t *testing.T) {
	t.Setenv(hostileEnv, sentinelToken)
	c, err := NewGitHubProjects(Config{
		APIBase: "https://attacker.example",
		AuthEnv: hostileEnv,
		Project: "octo/repo",
		Hosts:   hostallow.Derive("https://github.com/Rivil/dross", nil),
	})
	if c == nil {
		assertRefusedBeforeToken(t, err, nil)
		return
	}
	assertRefusedBeforeToken(t, err, c)
}

func TestJiraRefusesOffAllowlistHost(t *testing.T) {
	t.Setenv(hostileEnv, sentinelToken)
	c, err := NewJira(Config{
		APIBase:  "https://attacker.example",
		AuthEnv:  hostileEnv,
		Project:  "PROJ",
		AuthUser: "me@example.com",
		Hosts:    hostallow.Derive("https://github.com/Rivil/dross", nil),
	})
	if c == nil {
		assertRefusedBeforeToken(t, err, nil)
		return
	}
	assertRefusedBeforeToken(t, err, c)
}

func TestYouTrackRefusesOffAllowlistHost(t *testing.T) {
	t.Setenv(hostileEnv, sentinelToken)
	c, err := NewYouTrack(Config{
		APIBase: "https://attacker.example",
		AuthEnv: hostileEnv,
		Project: "PROJ",
		Hosts:   hostallow.Derive("https://github.com/Rivil/dross", nil),
	})
	if c == nil {
		assertRefusedBeforeToken(t, err, nil)
		return
	}
	assertRefusedBeforeToken(t, err, c)
}

// TestRefusedHostNeverReadsToken pins the ORDER, which is the whole point of
// where the check was placed. With the host off-allowlist AND the env var
// unset, the error must be the host refusal — "$X is not set" would prove the
// Getenv ran first, and a token that has been read is a token this process can
// leak on any later error path.
func TestRefusedHostNeverReadsToken(t *testing.T) {
	const unset = "DROSS_DEFINITELY_UNSET_HOSTGUARD"
	if _, ok := os.LookupEnv(unset); ok {
		t.Skipf("%s is set in this environment", unset)
	}
	policy := hostallow.Derive("https://github.com/Rivil/dross", nil)

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"forge.New", func() error {
			_, err := New(Config{Provider: "forgejo", URL: "https://github.com/Rivil/dross",
				APIBase: "https://attacker.example", AuthEnv: unset, Hosts: policy})
			return err
		}},
		{"NewGitHubProjects", func() error {
			_, err := NewGitHubProjects(Config{APIBase: "https://attacker.example",
				AuthEnv: unset, Project: "octo/repo", Hosts: policy})
			return err
		}},
		{"NewJira", func() error {
			_, err := NewJira(Config{APIBase: "https://attacker.example", AuthEnv: unset,
				Project: "PROJ", AuthUser: "me@example.com", Hosts: policy})
			return err
		}},
		{"NewYouTrack", func() error {
			_, err := NewYouTrack(Config{APIBase: "https://attacker.example", AuthEnv: unset,
				Project: "PROJ", Hosts: policy})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if strings.Contains(err.Error(), "is not set") {
				t.Errorf("the token was read before the host was checked: %v", err)
			}
			if !errors.Is(err, hostallow.ErrRefused) {
				t.Errorf("not the host refusal: %v", err)
			}
		})
	}
}

// TestForgeNewAllowsDerivedHost is the over-refusal gate. github.com as the
// remote and api.github.com as the API base is the single most common
// configuration dross sees; if the guard breaks it, the guard gets deleted.
func TestForgeNewAllowsDerivedHost(t *testing.T) {
	t.Setenv(hostileEnv, sentinelToken)
	policy := hostallow.Derive("https://github.com/Rivil/dross", nil)

	if _, err := NewGitHubProjects(Config{
		APIBase: "https://api.github.com", AuthEnv: hostileEnv,
		Project: "octo/repo", Hosts: policy,
	}); err != nil {
		t.Errorf("the github.com/api.github.com split must construct: %v", err)
	}

	// And a self-hosted forge, where remote and api_base share a host.
	if _, err := New(Config{
		Provider: "forgejo", URL: "https://git.corp.internal/team/app",
		APIBase: "https://git.corp.internal/api/v1", AuthEnv: hostileEnv,
		Hosts: hostallow.Derive("https://git.corp.internal/team/app", nil),
	}); err != nil {
		t.Errorf("a self-hosted forge must construct: %v", err)
	}
}

// TestGitHubDefaultAPIBaseIsChecked: github.go substitutes
// https://api.github.com when api_base is empty, which is the path most repos
// take. Leaving the substituted value unchecked would exempt the common case.
func TestGitHubDefaultAPIBaseIsChecked(t *testing.T) {
	t.Setenv(hostileEnv, sentinelToken)
	// A policy that does NOT cover api.github.com cannot exist through the
	// derivation (it is a built-in default), so the assertion here is the
	// positive one: the default resolves and passes rather than bypassing.
	if _, err := NewGitHubProjects(Config{
		APIBase: "", AuthEnv: hostileEnv, Project: "octo/repo",
		Hosts: hostallow.Derive("https://github.com/Rivil/dross", nil),
	}); err != nil {
		t.Fatalf("the substituted default must pass the check, not bypass it: %v", err)
	}
}
