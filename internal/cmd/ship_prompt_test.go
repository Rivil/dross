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

	// §6 GitLab squash-merge endpoint (underscore-stripped form).
	if !strings.Contains(content, "mergerequests") {
		t.Errorf("ship.md §6 missing GitLab squash-merge endpoint token %q", "mergerequests")
	}
	// The remote-branch removal flag is deliberately GONE. gitlab-ship-provider
	// pinned `should_remove_source_branch` as part of the locked merge mapping;
	// completion-state-truth supersedes that, because provider-side teardown is
	// now dross's job on every provider (see TestShipPromptNoUnguardedSwitch).
	if strings.Contains(content, "shouldremovesourcebranch") {
		t.Error("ship.md §6 must not ask GitLab to remove the source branch — `dross phase complete` owns the teardown")
	}
}

// TestShipPromptNoUnguardedSwitch (c-1) is the executable guard on the hole
// this phase closes: no step of /dross-ship may switch branches outside dross's
// guarded primitives. `gh pr merge --delete-branch` does its own raw checkout of
// the base branch — that is what destroyed a live state.json on the
// state-json-branch-safety ship — and a raw `git checkout` in the prompt is the
// same hole with the user's hands on it.
//
// The whole file is scanned, not just §4-to-EOF: §0's pre-flight step 5 has a
// guarded replacement now (`dross phase checkout`), so there is nowhere left
// that legitimately types one.
func TestShipPromptNoUnguardedSwitch(t *testing.T) {
	content := shipPromptContent(t)

	for _, forbidden := range []string{
		"--delete-branch",
		"shouldremovesourcebranch", // should_remove_source_branch
		"deletesourcebranch",       // delete_source_branch
		"git checkout",
		"git switch",
	} {
		if strings.Contains(content, forbidden) {
			t.Errorf("ship.md must not contain %q — every branch switch and branch deletion goes through dross", forbidden)
		}
	}
}

// TestShipPromptNamesCompleteAsTeardownOwner (c-1, c-3): with the provider's
// delete flags gone, the prompt has to say who does the deletion instead — and
// §6.3's description of `dross phase complete` has to stop describing the old
// behaviour (a switch to main, and a chore commit carrying the record).
func TestShipPromptNamesCompleteAsTeardownOwner(t *testing.T) {
	// Collapse whitespace so multi-word needles match across line wraps.
	content := strings.Join(strings.Fields(shipPromptContent(t)), " ")

	for _, needle := range []string{
		"dross phase complete performs the local and remote",
		"on every provider",
	} {
		if !strings.Contains(content, needle) {
			t.Errorf("ship.md §6 must name `dross phase complete` as the teardown owner: missing %q", needle)
		}
	}
	for _, stale := range []string{
		"switches to main",
		"chore commit",
	} {
		if strings.Contains(content, stale) {
			t.Errorf("ship.md §6.3 still describes the retired completion behaviour: %q", stale)
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

// TestShipPromptEmitsTerminalBoardStatuses proves c-6: ship moves the board
// issue to a terminal lifecycle state rather than just closing it.
//
// The bare `phase-sync <phase-id> --close` this replaces is why "shipped" and
// "complete" sat in both forge state maps as keys nothing ever resolved — dross
// keyed them but never emitted them. Giving ship the two call sites is what
// makes the bidirectional divergence gate satisfiable in both directions.
func TestShipPromptEmitsTerminalBoardStatuses(t *testing.T) {
	content := shipPromptContent(t)

	marks := []struct {
		name string
		at   int
	}{
		{"squash-merge bullet", strings.Index(content, "squash-merge via provider")},
		{"--status shipped call", strings.Index(content, "phase-sync <phase-id> --status shipped")},
		{"dross phase complete", strings.Index(content, "dross phase complete <phase-id>")},
		{"--status complete --close call", strings.Index(content, "phase-sync <phase-id> --status complete --close")},
	}
	for _, m := range marks {
		if m.at < 0 {
			t.Fatalf("ship.md has no %s", m.name)
		}
	}

	// Order, not mere presence. Both calls exist in either arrangement, so a
	// substring check would pass with them swapped — and swapped is wrong:
	// shipped is the state once the PR merges but before the phase finalizes,
	// complete is the state after.
	for i := 1; i < len(marks); i++ {
		if marks[i].at <= marks[i-1].at {
			t.Errorf("%s (at %d) must come after %s (at %d)", marks[i].name, marks[i].at, marks[i-1].name, marks[i-1].at)
		}
	}

	// The pattern that produced the dead map entries.
	if strings.Contains(content, "phase-sync <phase-id> --close") {
		t.Error("ship.md still closes the board issue with a bare --close — that call emits no status, which is what left shipped and complete as state-map keys nothing resolves")
	}

	// Locked terminal_emit_sites: `dross phase complete` gains no board
	// coupling. Both board moments are ship's own, so a command with zero board
	// awareness today stays that way.
	for i, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "dross phase complete") && strings.Contains(line, "phase-sync") {
			t.Errorf("ship.md:%d couples `dross phase complete` with a phase-sync call: %s", i+1, strings.TrimSpace(line))
		}
	}
}
