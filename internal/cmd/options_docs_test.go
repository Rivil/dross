package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// optionsPrompt reads the /dross-options prompt, which is the only surface that
// claims to reach EVERY dross-managed setting.
func optionsPrompt(t *testing.T) string {
	t.Helper()
	root := repoRootForDocs(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "options.md"))
	if err != nil {
		t.Fatalf("read options.md: %v", err)
	}
	return string(b)
}

// TestOptionsCoversEveryLocalKey walks localKeys itself rather than a hand-typed
// list, so a key added to the store without a line in the prompt fails here.
//
// The store is the case that motivated this test: .dross/local.toml is
// gitignored, absent from `dross project show`, and every value in it was
// written by a one-shot command. If the settings surface does not name a key,
// nothing else will, and the user finds out it exists by reading source.
func TestOptionsCoversEveryLocalKey(t *testing.T) {
	prompt := optionsPrompt(t)
	for key := range localKeys {
		if !strings.Contains(prompt, key) {
			t.Errorf("options.md does not mention the settable local key %q", key)
		}
	}
}

// TestOptionsCoversTheConsentVerbs pins the keys `dross local set` deliberately
// REFUSES.
//
// They are the ones a settings surface is most likely to miss, precisely
// because the generic key-writer cannot reach them — and the ones whose absence
// costs most, since a stale grant or a lapsed trust is silent until a verify
// refuses to run.
func TestOptionsCoversTheConsentVerbs(t *testing.T) {
	prompt := optionsPrompt(t)
	for _, want := range []string{
		"mutation remote grant",
		"mutation remote revoke",
		"mutation remote status",
		"dross trust",
		"trusted_test_command",
		"mutation_remote_host",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("options.md does not mention %q", want)
		}
	}
	// ...and it must not tell the user to write a grant key through the generic
	// writer, which would be the consent_model decision inverted.
	for _, forbidden := range []string{
		"local set mutation_remote_host",
		"local set mutation_remote_workdir",
		"local set trusted_test_command",
	} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("options.md tells the user to write a consent key via the generic key-writer: %q", forbidden)
		}
	}
}

// TestOptionsCoversEveryProjectSection is the drift guard for project.toml.
//
// options.md instructs the reader to STOP on a field it does not cover, calling
// that a schema-vs-prompt drift bug. That instruction was itself unenforced:
// [board] and [mutation] were printed by `dross project show` while the prompt
// named neither. This asserts the top-level sections; adding a new one to the
// Project struct without a section here fails.
func TestOptionsCoversEveryProjectSection(t *testing.T) {
	prompt := optionsPrompt(t)
	for _, section := range []string{
		"project.", "stack.", "runtime.", "repo.", "remote.",
		"board.", "mutation.", "paths.", "env.", "goals.",
		"constraints", "competition",
	} {
		if !strings.Contains(prompt, section) {
			t.Errorf("options.md does not cover the project.toml section %q", section)
		}
	}
}

// TestOptionsCoversTheProviderConditionalRemoteFields.
//
// These three are the fields a reader skims past because they are empty in the
// common case — and each one is load-bearing for exactly one provider, where a
// missing value produces an auth failure that names nothing useful.
func TestOptionsCoversTheProviderConditionalRemoteFields(t *testing.T) {
	prompt := optionsPrompt(t)
	for _, want := range []string{
		"remote.auth_user", "remote.auth_scheme", "remote.project_id",
		"board.auth_user", "board.github_project", "board.milestone_mode",
		"stack.profile",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("options.md does not mention %q", want)
		}
	}
}

// TestOptionsSaysMutationIsNotSettableThroughProjectSet.
//
// `dross project set mutation.adapters` fails — there is no dotted-path writer
// for [mutation]. A prompt that listed the field without that caveat would send
// the user into an error and leave the adapters allowlist unset, which is the
// state that makes `dross doctor` probe every toolchain and fail a Go-only repo
// over a missing npx.
func TestOptionsSaysMutationIsNotSettableThroughProjectSet(t *testing.T) {
	prompt := optionsPrompt(t)
	idx := strings.Index(prompt, "## 13. Mutation adapters")
	if idx < 0 {
		t.Fatal("options.md has no mutation adapters section")
	}
	section := prompt[idx:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	if !strings.Contains(section, "not settable") {
		t.Error("the mutation section does not say the fields are unsettable through `dross project set`")
	}
	if !strings.Contains(section, "mutation.adapters") {
		t.Error("the mutation section does not name mutation.adapters")
	}
}

// TestOptionsSectionNumbersAreContiguous keeps the numbering honest: the prompt
// cross-references sections by number ("see §11", "see §14"), and a duplicated
// or skipped heading silently points the reader at the wrong one.
func TestOptionsSectionNumbersAreContiguous(t *testing.T) {
	prompt := optionsPrompt(t)
	seen := 0
	for _, line := range strings.Split(prompt, "\n") {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		rest := strings.TrimPrefix(line, "## ")
		dot := strings.Index(rest, ".")
		if dot <= 0 {
			continue // "## Section pick" and friends carry no number
		}
		n, err := strconv.Atoi(rest[:dot])
		if err != nil {
			continue
		}
		if n != seen {
			t.Errorf("section heading %q breaks the sequence: expected %d", line, seen)
		}
		seen = n + 1
	}
	if seen < 17 {
		t.Errorf("options.md has only %d numbered sections; the settings surface lost one", seen)
	}
}
