package hostallow

import (
	"errors"
	"strings"
	"testing"
)

const remote = "https://github.com/Rivil/dross"

// TestCheckRefusesOffAllowlistHost is the criterion in one assertion: a
// committed api_base pointing somewhere the derivation never reached gets a
// refusal, and the refusal names the host so the user can see what the repo
// tried to do.
func TestCheckRefusesOffAllowlistHost(t *testing.T) {
	p := Derive(remote, nil)
	err := p.Check("[remote].api_base", "https://attacker.example")
	if err == nil {
		t.Fatal("off-allowlist host was allowed")
	}
	if !strings.Contains(err.Error(), "refusing to contact host") {
		t.Errorf("refusal does not carry the pinned prefix: %v", err)
	}
	if !strings.Contains(err.Error(), "attacker.example") {
		t.Errorf("refusal does not name the host: %v", err)
	}
	if !strings.Contains(err.Error(), "[remote].api_base") {
		t.Errorf("refusal does not name the setting: %v", err)
	}
}

// TestCheckRefusalIsErrRefused pins the sentinel across every refusal path.
// This is what lets mergeGate tell "the repo is attacking you" apart from "the
// forge is down" — the first must surface, the second may degrade to git
// ancestry. Collapse them and an active attack looks like a flaky network.
func TestCheckRefusalIsErrRefused(t *testing.T) {
	p := Derive(remote, nil)
	for _, tc := range []struct {
		name string
		url  string
	}{
		{"off allowlist", "https://attacker.example"},
		{"plain http", "http://github.com"},
		{"odd port", "https://api.github.com:8443"},
		{"unparseable", "://nonsense"},
		{"empty", ""},
		{"no host", "https:///path"},
		{"wildcard near miss", "https://evil-atlassian.net"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := p.Check("[remote].api_base", tc.url)
			if err == nil {
				t.Fatalf("Check(%q) = nil, want a refusal", tc.url)
			}
			if !errors.Is(err, ErrRefused) {
				t.Errorf("refusal does not wrap ErrRefused: %v", err)
			}
		})
	}
}

// TestSuffixWildcardIsNotSubstring is the hole a strings.HasSuffix shortcut
// opens. "*.atlassian.net" must require the dot boundary and must anchor at the
// end of the host, or an attacker registers evil-atlassian.net (or points
// atlassian.net.attacker.example at themselves) and inherits every Jira token
// dross holds.
func TestSuffixWildcardIsNotSubstring(t *testing.T) {
	p := Derive(remote, nil)
	if err := p.Check("[board].base_url", "https://acme.atlassian.net"); err != nil {
		t.Errorf("a real Jira Cloud tenant must be allowed: %v", err)
	}
	for _, host := range []string{
		"https://evil-atlassian.net",
		"https://atlassian.net.attacker.example",
		"https://atlassian.net",
		"https://xatlassian.net",
		"https://acme.atlassian.net.attacker.example",
	} {
		if err := p.Check("[board].base_url", host); err == nil {
			t.Errorf("wildcard matched %s — the dot boundary is not being enforced", host)
		}
	}
}

// TestCheckAllowsRemoteHost is the over-refusal gate: a self-hosted install
// where the remote and the API share a host must keep working with no Extra at
// all, or the derivation costs honest repos something and gets turned off.
func TestCheckAllowsRemoteHost(t *testing.T) {
	p := Derive("https://git.corp.internal/team/app", nil)
	for _, u := range []string{
		"https://git.corp.internal/api/v1",
		"https://git.corp.internal",
		"https://git.corp.internal:443/api/v4",
	} {
		if err := p.Check("[remote].api_base", u); err != nil {
			t.Errorf("self-hosted pair refused: %s: %v", u, err)
		}
	}
	// And the github.com / api.github.com split the SaaS defaults exist for.
	gh := Derive(remote, nil)
	if err := gh.Check("[remote].api_base", "https://api.github.com"); err != nil {
		t.Errorf("api.github.com refused under a github.com remote: %v", err)
	}
}

// TestCheckRefusesSchemeAndPort covers the two ways an allowed *name* is not an
// allowed *endpoint*: cleartext, which leaks the token to anyone on the path
// regardless of who is listening at the other end, and an unexpected port,
// which is a different service.
func TestCheckRefusesSchemeAndPort(t *testing.T) {
	p := Derive(remote, nil)
	if err := p.Check("[remote].api_base", "http://api.github.com"); err == nil {
		t.Error("http on an allowed host must be refused — the token would go in cleartext")
	}
	if err := p.Check("[remote].api_base", "https://api.github.com:8443"); err == nil {
		t.Error("an unexpected port on an allowed host must be refused")
	}
	// Loopback is the deliberate exception: it cannot leave the machine, and
	// refusing it would make local instances unreachable. It is still gated by
	// the allowlist, so it is an exception to the scheme rule, not to the
	// policy.
	if err := p.Check("[remote].api_base", "http://127.0.0.1:9999"); err == nil {
		t.Error("loopback must still be subject to the allowlist")
	}
	local := Derive("http://127.0.0.1:9999/team/app", nil)
	if err := local.Check("[remote].api_base", "http://127.0.0.1:9999/api/v1"); err != nil {
		t.Errorf("a loopback remote must allow its own loopback API base: %v", err)
	}
}

// TestDeriveEmptyIsNotAllowAll is the fail-closed gate. A caller that forgets
// to populate Hosts gets the SaaS defaults, not a pass — because a zero value
// that means "unrestricted" is reached by exactly the code path nobody
// remembered to wire up.
func TestDeriveEmptyIsNotAllowAll(t *testing.T) {
	empty := Derive("", nil)
	if err := empty.Check("[remote].api_base", "https://attacker.example"); err == nil {
		t.Fatal("Derive(\"\", nil) allowed an arbitrary host")
	}
	var zero Policy
	if err := zero.Check("[remote].api_base", "https://attacker.example"); err == nil {
		t.Fatal("the zero Policy allowed an arbitrary host")
	}
	// It is the SaaS defaults, not an empty set: an empty set would refuse
	// github.com too, and get worked around rather than fixed.
	if err := empty.Check("[remote].api_base", "https://api.github.com"); err != nil {
		t.Errorf("Derive(\"\", nil) must still carry the SaaS defaults: %v", err)
	}
	if got := empty.Allowed(); len(got) != len(saasDefaults) {
		t.Errorf("Allowed() = %v, want exactly the %d SaaS defaults", got, len(saasDefaults))
	}
}

// TestExtraWidensTheSet covers the escape hatch from the locked escape_hatch
// decision. Extra comes from the gitignored, machine-local local.toml, which a
// hostile repo cannot ship — that is what stops the allowlist from being
// self-authorizing.
func TestExtraWidensTheSet(t *testing.T) {
	p := Derive(remote, []string{"git.corp.internal", "odd.example:8443", "https://third.example/x"})
	for _, u := range []string{
		"https://git.corp.internal/api/v1",
		"https://odd.example:8443/api",
		"https://third.example",
	} {
		if err := p.Check("[remote].api_base", u); err != nil {
			t.Errorf("Extra entry not honoured for %s: %v", u, err)
		}
	}
	// Widened, not opened: an unlisted host is still refused, and an Extra
	// entry pinned to a port does not authorize the same host on another one.
	if err := p.Check("[remote].api_base", "https://attacker.example"); err == nil {
		t.Error("Extra must widen the set, not disable it")
	}
	if err := p.Check("[remote].api_base", "https://odd.example"); err == nil {
		t.Error("a port-pinned Extra entry must not authorize the default port")
	}
}
