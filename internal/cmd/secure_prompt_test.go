package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// securePromptContent loads assets/prompts/secure.md (the audit orchestration
// prompt) and normalises it — lowercased, with markdown emphasis and backticks
// stripped — so the assertions below test the *presence of a rule*, not its exact
// formatting.
func securePromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "secure.md"))
	if err != nil {
		t.Fatalf("read secure.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
}

// TestSecurePromptMandatedSections content-gates the four rules secure.md must
// carry (t-7 authors them; this is where they're asserted). Each criterion is an
// individually-failing sub-assertion, so removing any one mandated section from
// secure.md fails exactly that sub-test rather than a single coarse pass/fail.
func TestSecurePromptMandatedSections(t *testing.T) {
	content := securePromptContent(t)
	cases := []struct {
		name    string
		needles []string
	}{
		{"c-2 refute-panel majority-vote drop", []string{"refute", "majority vote", "drop"}},
		{"c-3 context-free, reads no .dross planning artifacts", []string{"context-free", "no .dross/ planning artifacts"}},
		{"c-6 read-only, no --fix, never edits app code", []string{"no --fix", "never edit"}},
		{"c-5 propose-then-ask before locking the scaffold", []string{"propose-then-ask before locking"}},
		{"c-1/c-3 post-scan reconcile against prior state", []string{"dross security findings reconcile"}},
		{"private-code semgrep no-egress guidance", []string{"metrics=off", "config auto", "semgrep.dev", "private"}},
		{"c-4 gitleaks run-dir allowlist via --config", []string{"gitleaks", "--config", "gitleaks.toml", "run-dir", "allowlist"}},
		{"c-5 detect names exclusions", []string{"exclusions"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range tc.needles {
				if !strings.Contains(content, n) {
					t.Errorf("secure.md is missing the required phrase %q for %s", n, tc.name)
				}
			}
		})
	}

	// c-4's needles must sit in ONE paragraph — the gitleaks bullet — not be
	// scattered across the prompt, and that bullet's exemplar must keep
	// --config ahead of the ` -- ` separator (the fence discipline the semgrep
	// exemplar already follows). Paragraphs are split on blank lines, bullets on
	// their leading "- ".
	t.Run("c-4 gitleaks bullet is one paragraph with --config before --", func(t *testing.T) {
		var bullet string
		for _, para := range strings.Split(content, "\n\n") {
			for _, item := range strings.Split(para, "\n   - ") {
				flat := strings.Join(strings.Fields(item), " ")
				ok := true
				for _, n := range []string{"gitleaks", "--config", "gitleaks.toml", "run-dir", "allowlist"} {
					if !strings.Contains(flat, n) {
						ok = false
						break
					}
				}
				if ok {
					bullet = flat
				}
			}
		}
		if bullet == "" {
			t.Fatal("no single paragraph of secure.md carries gitleaks, --config, gitleaks.toml, run-dir and allowlist together — the gitleaks bullet is missing or split")
		}
		ex := strings.Index(bullet, "gitleaks git")
		if ex < 0 {
			t.Fatalf("the gitleaks bullet has no `gitleaks git` exemplar: %s", bullet)
		}
		tail := bullet[ex:]
		cfg, sep := strings.Index(tail, "--config"), strings.Index(tail, " -- ")
		if cfg < 0 || sep < 0 || sep < cfg {
			t.Errorf("the gitleaks exemplar does not put --config ahead of the ` -- ` separator: %.120s", tail)
		}
	})

	// c-5: the exclusions mention must live in the Detect step, not elsewhere.
	t.Run("c-5 exclusions named in the detect step", func(t *testing.T) {
		at := strings.Index(content, "1. detect")
		if at < 0 {
			t.Fatal("secure.md has no `1. Detect` step")
		}
		end := strings.Index(content[at:], "2. plan")
		if end < 0 {
			t.Fatal("secure.md has no `2. Plan` step after Detect")
		}
		if !strings.Contains(content[at:at+end], "exclusions") {
			t.Errorf("the Detect step does not mention exclusions:\n%s", content[at:at+end])
		}
	})
}

// TestSecurePromptFencesScannerOperands carries c-2's semgrep leg.
//
// dross spawns semgrep nowhere in Go — internal/security/catalog.go's entry is a
// LookPath presence check — so internal/argfence's table entry for it is
// forward-looking and TestEveryCatalogToolHasAnArgvPolicy satisfies the criterion
// vacuously. The place semgrep genuinely runs with derived paths is this prompt,
// which means the guarantee has to be gated here or it is not gated at all.
//
// The needles are deliberately specific. Asserting a bare "--" would prove
// nothing in a document full of --flags; "end-of-options" appears nowhere else in
// secure.md, and the command exemplar pins the ORDER — flags ahead of the
// separator, paths behind it — which is the half that is silently wrong rather
// than loudly broken.
func TestSecurePromptFencesScannerOperands(t *testing.T) {
	// Collapse whitespace so the assertions survive line wrapping in the prompt.
	content := strings.Join(strings.Fields(securePromptContent(t)), " ")

	for _, needle := range []string{
		"end-of-options",
		"before the path operand",
		"semgrep --metrics=off --config <ruleset> -- <path>",
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("secure.md lost the semgrep operand-fencing guidance: missing %q", needle)
		}
	}

	// The guidance must stay attached to semgrep. A drift into some unrelated
	// section would keep every needle above present while fencing nothing, so
	// require semgrep to be named in the run-up to the separator rule.
	at := strings.Index(content, "end-of-options")
	if at < 0 {
		return // already reported above
	}
	from := at - 200
	if from < 0 {
		from = 0
	}
	if !strings.Contains(content[from:at], "semgrep") {
		t.Errorf("the end-of-options guidance does not name semgrep within the preceding 200 chars — it has drifted off the tool it is meant to fence:\n...%s", content[from:at])
	}

	// Order guard, stated independently of the exemplar: the separator must come
	// after --config, not before it. A `--` ahead of the flags demotes the
	// ruleset into a scan path — green test, wrong command.
	ex := strings.Index(content, "semgrep --metrics=off")
	if ex < 0 {
		return // already reported above
	}
	tail := content[ex:]
	cfg, sep := strings.Index(tail, "--config"), strings.Index(tail, " -- ")
	if cfg < 0 || sep < 0 || sep < cfg {
		t.Errorf("the semgrep exemplar does not put its flags ahead of the separator: %.120s", tail)
	}
}
