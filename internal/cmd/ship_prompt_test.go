package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shipPromptContent loads assets/prompts/ship.md and normalises it —
// lowercased, with markdown emphasis and backticks stripped — so assertions
// test the presence of a rule, not its exact formatting.
func shipPromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "ship.md"))
	if err != nil {
		t.Fatalf("read ship.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
}

// TestShipPromptRecoverySection (c-5) gates the recovery cookbook: all three
// mid-merge failure states and both recovery commands must be present, and the
// section must never instruct manual .dross/ surgery (the drift the cookbook
// exists to prevent).
func TestShipPromptRecoverySection(t *testing.T) {
	content := shipPromptContent(t)

	// Required: the three failure-state phrases + both recovery commands.
	for _, n := range []string{
		"fast-forward",
		"diverged",
		"dirty tree",
		"dross phase complete --recover",
		"dross ship recover",
	} {
		if !strings.Contains(content, n) {
			t.Errorf("ship.md recovery section missing required phrase %q", n)
		}
	}

	// Forbidden: manual .dross/ surgery presented as a user step. The whole
	// point is that a dross command owns the restore — reintroducing these
	// must fail the gate.
	for _, n := range []string{
		"git add .dross",
		"-- .dross/",
	} {
		if strings.Contains(content, n) {
			t.Errorf("ship.md must not instruct manual .dross/ surgery (found %q)", n)
		}
	}
}

// TestShipPromptReadsTypedLandmarks proves c-1's consumer side: ship.md §3.5
// reads the structured `landmarks` array of {feature, symbol, loc, what} objects
// from `dross changes show`, and no longer parses a notes string for the
// landmark. Regressing §3.5 to "notes is a landmark" reintroduces the forbidden
// phrase and fails this.
func TestShipPromptReadsTypedLandmarks(t *testing.T) {
	content := shipPromptContent(t)
	for _, needle := range []string{"landmarks", "feature", "symbol", "loc", "what"} {
		if !strings.Contains(content, needle) {
			t.Errorf("ship.md §3.5 must read the typed landmark fields: missing %q", needle)
		}
	}
	if strings.Contains(content, "notes is a landmark") {
		t.Error("ship.md §3.5 must not parse the notes string for the landmark (legacy phrasing survived)")
	}
}

// TestShipPromptAutoFastPath (c-2) gates the non-interactive --auto path: ship.md
// must document an --auto fast-path that skips the §1 body-preview dump, the §2
// body-override prompt, and the §3 reviewer prompt, and shells out to
// `dross ship --auto`. The uniquely-added tokens ("dross ship --auto",
// "non-interactive", "returns without merging") are absent from the rest of the
// prompt, so deleting the fast-path section drops them and fails this test.
func TestShipPromptAutoFastPath(t *testing.T) {
	content := shipPromptContent(t)

	// Tokens unique to the fast-path section — these carry the fail-on-removal
	// guarantee (none appear elsewhere in ship.md).
	for _, needle := range []string{
		"dross ship --auto",       // shells out to the non-interactive CLI path
		"non-interactive",         // the section's defining property
		"returns without merging", // c-4: --auto opens the PR and stops
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("ship.md --auto fast-path missing unique token %q", needle)
		}
	}

	// The section must name what it skips: the body-override and reviewer
	// prompts (and the body preview).
	for _, needle := range []string{"skip", "body override", "reviewers"} {
		if !strings.Contains(content, needle) {
			t.Errorf("ship.md --auto fast-path must name skipped step %q", needle)
		}
	}
}

// TestShipPromptGitLabSections proves c-4: ship.md's §5 (CI gate) and §6 (merge
// gate) carry the GitLab pipeline-watch and squash-merge steps, and §5 pins the
// ENTIRE locked pipeline_status_mapping — terminal, keep-polling, AND ambiguous
// states — not just the terminal ones. Dropping any branch of the mapping or
// either ship step removes its token and fails this. Tokens are matched against
// shipPromptContent, which lowercases and strips underscores (so merge_requests
// -> mergerequests, should_remove_source_branch -> shouldremovesourcebranch).
// (r-01: the prompt edit is only live for the running binary after `make install`;
// this reads the source file, gating the committed prompt directly.)
func TestShipPromptGitLabSections(t *testing.T) {
	content := shipPromptContent(t)

	// §5 GitLab CI-watch endpoint + both auth schemes.
	for _, needle := range []string{
		"projects/<id>/pipelines",
		"pipelines?sha",
		"private-token",
		"bearer",
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("ship.md §5 missing GitLab CI-watch token %q", needle)
		}
	}

	// Locked pipeline_status_mapping — every branch must be documented.
	for _, state := range []string{
		"success", "failed", "canceled", // terminal
		"running", "pending", "created", "preparing", // keep-polling
		"manual", "skipped", // surface-and-ask
	} {
		if !strings.Contains(content, state) {
			t.Errorf("ship.md §5 dropped pipeline status %q from the locked mapping", state)
		}
	}

	// No-pipeline-for-the-SHA ask path.
	if !strings.Contains(content, "empty pipelines array") {
		t.Error("ship.md §5 missing the no-pipeline (empty pipelines array) surface-and-ask path")
	}

	// §6 GitLab squash-merge + remote-branch removal (underscore-stripped forms).
	for _, needle := range []string{
		"mergerequests",            // merge_requests
		"shouldremovesourcebranch", // should_remove_source_branch
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("ship.md §6 missing GitLab squash-merge token %q", needle)
		}
	}
}

// TestShipPromptAutoBackfill proves ship-architecture-autogen: an absent
// ARCHITECTURE.md self-heals automatically on the interactive ship path (c-1),
// --auto documents skipping that backfill (c-2), and the backfill is non-blocking
// (c-3). Regressing §3.5 back to a manual-run-or-skip drops these needles.
func TestShipPromptAutoBackfill(t *testing.T) {
	// Collapse whitespace so multi-word needles match across line wraps.
	content := strings.Join(strings.Fields(shipPromptContent(t)), " ")
	// c-1: absent doc self-heals via an automatic backfill.
	for _, needle := range []string{"self-heal", "automatically backfill"} {
		if !strings.Contains(content, needle) {
			t.Errorf("ship.md §3.5 must auto-backfill an absent ARCHITECTURE.md: missing %q", needle)
		}
	}
	// c-1 regression guard: must not punt to a manual run.
	if strings.Contains(content, "generate it first") {
		t.Error("ship.md §3.5 regressed to a manual /dross-architecture run (should auto-backfill)")
	}
	// c-2: --auto documents skipping the auto-backfill.
	if !strings.Contains(content, "auto-backfill") {
		t.Error("ship.md must document the ARCHITECTURE.md auto-backfill (incl. the --auto skip)")
	}
	// c-3: the backfill is non-blocking.
	for _, needle := range []string{"non-blocking", "must never block the ship"} {
		if !strings.Contains(content, needle) {
			t.Errorf("ship.md backfill must be non-blocking: missing %q", needle)
		}
	}
}
