package cmd

import (
	"strconv"
	"strings"
	"testing"
)

// TestValidateGitRefAccepts is the over-tightening gate. A guard that refuses
// too much is not a safer guard — it is a guard that gets reverted the first
// time someone's release branch stops working, and then protects nothing. Every
// name here is one dross itself produces or is routinely pointed at.
func TestValidateGitRefAccepts(t *testing.T) {
	for _, name := range []string{
		"main",
		"master",
		"trunk",
		"milestone/v1.2",
		"phase/config-trust-hardening",
		"release-1.0",
		"origin/main",
		"origin/phase/config-trust-hardening",
		"refs/heads/main",
		"HEAD",
		"v1.0.3",
		"feature/JIRA-123_add-thing",
		"d62be41",
		"d62be413a8f0c1a0a1b2c3d4e5f60718293a4b5c",
	} {
		if err := validateGitRef("branch", name); err != nil {
			t.Errorf("validateGitRef(%q) = %v, want nil", name, err)
		}
	}
}

// TestValidateGitRefRejects covers the reject table. The leading dash is the
// vector the phase exists for; the rest is check-ref-format's own set, applied
// in dross so the refusal lands before argv rather than several layers later.
func TestValidateGitRefRejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		ref  string
	}{
		{"empty", ""},
		{"leading dash", "-x"},
		{"output injection", "--output=/tmp/dross-pwned"},
		{"upload-pack injection", "--upload-pack=touch /tmp/dross-pwned"},
		{"rendered branch pattern", "-p/config-trust-hardening"},
		{"space", "my branch"},
		{"tab", "my\tbranch"},
		{"newline", "main\nrm -rf /"},
		{"control char", "main\x01"},
		{"double dot", "main..work"},
		{"tilde", "main~1"},
		{"caret", "main^"},
		{"colon", "main:.dross/state.json"},
		{"question mark", "main?"},
		{"star", "main*"},
		{"bracket", "main[0]"},
		{"backslash", `main\x`},
		{"reflog syntax", "main@{1}"},
		{"bare at", "@"},
		{"lock suffix", "main.lock"},
		{"lock component", "main.lock/next"},
		{"trailing slash", "main/"},
		{"leading slash", "/main"},
		{"empty component", "refs//heads"},
		{"trailing dot", "main."},
		{"dot component", "refs/.hidden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateGitRef("repo.git_main_branch", tc.ref)
			if err == nil {
				t.Fatalf("validateGitRef(%q) = nil, want a refusal", tc.ref)
			}
			// The refusal must name both the source and the value: a message
			// that says only "invalid ref" sends the user hunting for which
			// committed line produced it.
			if !strings.HasPrefix(err.Error(), "unsafe git ref") {
				t.Errorf("refusal does not carry the pinned prefix: %v", err)
			}
			if !strings.Contains(err.Error(), "repo.git_main_branch") {
				t.Errorf("refusal does not name the kind: %v", err)
			}
			// Quoted, because the refusal renders the value with %q — which is
			// the point for a payload carrying a tab or a newline: an
			// unescaped one would let the attacker's value forge line breaks
			// in dross's own error output.
			if tc.ref != "" && !strings.Contains(err.Error(), strconv.Quote(tc.ref)) {
				t.Errorf("refusal does not name the offending value %q: %v", tc.ref, err)
			}
		})
	}
}

// TestGuardedHelpersRefuseLeadingDash drives every guarded helper with a
// payload-bearing ref, from a directory that is NOT a git repo.
//
// That last part is the point. If the guard were placed after the first exec,
// git would run, fail, and the helper would return git's own "not a git
// repository" text wrapped in its "git checkout %s" format string. Asserting
// the absence of both is what proves the refusal precedes the process, rather
// than merely coinciding with it.
//
// One subtest per call site, not one table over validateGitRef: dropping the
// guard from a single helper must fail that helper's subtest alone.
func TestGuardedHelpersRefuseLeadingDash(t *testing.T) {
	const payload = "--upload-pack=touch /tmp/dross-pwned"

	assertRefused := func(t *testing.T, err error, gitVerb string) {
		t.Helper()
		if err == nil {
			t.Fatal("helper accepted a leading-dash ref, want a refusal")
		}
		msg := err.Error()
		if !strings.HasPrefix(msg, "unsafe git ref") {
			t.Errorf("not the guard's refusal: %v", err)
		}
		if !strings.Contains(msg, payload) {
			t.Errorf("refusal does not name the payload: %v", err)
		}
		if strings.Contains(msg, "not a git repository") {
			t.Errorf("git ran before the guard — the refusal carries git's own error: %v", err)
		}
		if strings.Contains(msg, gitVerb) {
			t.Errorf("git ran before the guard — the refusal is wrapped in %q: %v", gitVerb, err)
		}
	}

	t.Run("checkoutBranch", func(t *testing.T) {
		assertRefused(t, checkoutBranch(t.TempDir(), payload), "git checkout")
	})
	t.Run("checkoutBranchNew branch", func(t *testing.T) {
		assertRefused(t, checkoutBranchNew(t.TempDir(), payload, "main"), "git checkout")
	})
	t.Run("checkoutBranchNew base", func(t *testing.T) {
		assertRefused(t, checkoutBranchNew(t.TempDir(), "phase/x", payload), "git checkout")
	})
	t.Run("guardedFF", func(t *testing.T) {
		_, err := guardedFF(t.TempDir(), payload)
		assertRefused(t, err, "git merge")
	})
	t.Run("guardedResetHard", func(t *testing.T) {
		_, err := guardedResetHard(t.TempDir(), payload)
		assertRefused(t, err, "git reset")
	})
}
