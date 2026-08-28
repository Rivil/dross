package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// executePromptContent loads assets/prompts/execute.md and normalises it
// (lowercased, backticks/emphasis stripped) so assertions test for the presence
// of an instruction rather than its exact formatting.
func executePromptContent(t *testing.T) string {
	t.Helper()
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "execute.md"))
	if err != nil {
		t.Fatalf("read execute.md: %v", err)
	}
	s := strings.ToLower(string(b))
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
}

// TestExecutePromptWiresPhaseNumber proves pc-3's "the version patch digit uses
// DisplayNumber" clause: the execute orchestration sets the patch digit from
// `dross phase number` rather than counting by hand. Removing that wiring fails
// this. (r-01: the prompt edit is only live after `make install`.)
func TestExecutePromptWiresPhaseNumber(t *testing.T) {
	content := executePromptContent(t)
	if !strings.Contains(content, "dross phase number") {
		t.Error("execute.md must derive the version patch digit from `dross phase number`")
	}
}

// TestExecutePromptInvokesLoadout proves c-4 end-to-end: the execute orchestration
// must call `dross stack loadout` and inject the block. If the invocation is
// removed from execute.md, this fails.
func TestExecutePromptInvokesLoadout(t *testing.T) {
	content := executePromptContent(t)
	for _, needle := range []string{"dross stack loadout", "inject"} {
		if !strings.Contains(content, needle) {
			t.Errorf("execute.md must wire the stack loadout: missing %q", needle)
		}
	}
}

// executeGateSection extracts the §1g post-commit gate section from the
// normalised prompt. Failing to find it at all is the primary regression this
// guards: the gate must be POST-commit (its own section after 1f), not grafted
// onto the §1c pre-task approach approval.
func executeGateSection(t *testing.T) string {
	t.Helper()
	content := executePromptContent(t)
	start := strings.Index(content, "### 1g. post-commit gate")
	if start == -1 {
		t.Fatal("execute.md must have a '### 1g. Post-commit gate' section after 1f — a checkpoint option on the pre-task approach gate does not satisfy c-2")
	}
	section := content[start:]
	if end := strings.Index(section, "\n## "); end != -1 {
		section = section[:end]
	}
	return section
}

// TestExecutePromptPostCommitGateOptions proves c-2's gate shape: the
// post-commit gate enumerates continue, stop, AND checkpoint.
func TestExecutePromptPostCommitGateOptions(t *testing.T) {
	section := executeGateSection(t)
	for _, opt := range []string{"continue", "stop", "checkpoint"} {
		if !strings.Contains(section, opt) {
			t.Errorf("post-commit gate must enumerate %q", opt)
		}
	}
}

// TestExecutePromptCheckpointConsistencyBeforeReentry proves c-2's ordering:
// checkpoint instructs the plan.toml/state consistency check (dross validate +
// dross task next) BEFORE emitting the `--from <next-task>` re-entry command.
func TestExecutePromptCheckpointConsistencyBeforeReentry(t *testing.T) {
	section := executeGateSection(t)
	validateIdx := strings.Index(section, "dross validate")
	reentryIdx := strings.Index(section, "--from <next-task>")
	if validateIdx == -1 {
		t.Fatal("checkpoint must confirm consistency via dross validate")
	}
	if reentryIdx == -1 {
		t.Fatal("checkpoint must emit the /dross-execute --from <next-task> re-entry command")
	}
	if validateIdx > reentryIdx {
		t.Error("consistency check must be instructed BEFORE the re-entry command is emitted")
	}
}

// TestExecutePromptCheckpointWaveLead proves the checkpoint_posture locked
// decision: at wave boundaries the gate leads with checkpoint as the
// recommended option.
func TestExecutePromptCheckpointWaveLead(t *testing.T) {
	section := executeGateSection(t)
	if !strings.Contains(section, "wave") {
		t.Fatal("post-commit gate must special-case wave boundaries")
	}
	if !strings.Contains(section, "lead with checkpoint") {
		t.Error("wave-boundary branch must mark checkpoint as the lead/recommended option")
	}
}

// TestExecutePromptEmitsTypedLandmark proves c-1's producer side: execute.md
// records the landmark through the typed `--landmark feature=…, symbol=…, loc=…,
// what=…` flag and no longer through the legacy `--notes "feature: …"` form. If
// the prompt regresses to the notes-string landmark, the forbidden token returns
// and this fails. (r-01: gates the source prompt directly, independent of install.)
func TestExecutePromptEmitsTypedLandmark(t *testing.T) {
	content := executePromptContent(t)
	for _, needle := range []string{"--landmark", "feature=", "symbol=", "loc=", "what="} {
		if !strings.Contains(content, needle) {
			t.Errorf("execute.md must emit the typed landmark: missing %q", needle)
		}
	}
	if strings.Contains(content, `--notes "feature:`) {
		t.Error("execute.md must not encode the landmark in --notes (legacy `--notes \"feature: …\"` form survived)")
	}
}

// TestExecutePromptOffloadGuidance pins execute.md §1b's offload passage
// (subagent-offload-audit c-3/c-4): code insight fans out read-only
// subagents when the file surface is large, and the passage keeps both
// halves of the contract — the size gate (offload_posture lock:
// conditional, never mandatory) and the agent-gate boundary (only the
// main loop writes code).
func TestExecutePromptOffloadGuidance(t *testing.T) {
	prompt := executePromptContent(t)

	for _, phrase := range []string{
		"offload the reading when the task's file surface is large",
		"read-only subagents",
		"never edit files or commit; only the main loop writes code",
		"for a typical small task, stay inline",
		"docs/subagent-offload-audit.md",
	} {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("execute.md lost its offload guidance phrase %q", phrase)
		}
	}
}

// TestExecutePromptDocumentsPrepareExit is the agent-facing half of c-3.
//
// exitPrepareFailed is worth nothing if the reader of the exit status does not
// know what 7 means. An agent that fell through to "non-zero, and not one of
// the codes I know" reads a failed bootstrap as a red suite and goes hunting a
// bug in code that was never executed — the exact failure the code was split
// out to prevent. Both halves are asserted: the entry itself, and its
// membership in the line that says which codes mean the run did not happen.
// (r-01: the prompt edit is only live after `make install`.)
func TestExecutePromptDocumentsPrepareExit(t *testing.T) {
	content := executePromptContent(t)
	if !strings.Contains(content, "- 7 —") {
		t.Error("execute.md's exit-code list has no entry for 7 — an agent would read a failed prepare as a red suite")
	}
	if !strings.Contains(content, "prepare") {
		t.Error("execute.md's exit-code list never names the prepare command")
	}
	if !strings.Contains(content, "2, 3, 4, 5, 6 and 7 all mean the run did not happen") {
		t.Error("execute.md's did-not-happen line still omits 7, so 7 reads as a verdict about the code")
	}
}

// TestExecutePromptPullsBoardTaskMoves guards the inbound half of board task
// sync. The outbound calls (`dross issue task sync`) have been wired since the
// task loop existed, but nothing ever called `dross issue task pull`, so a card
// moved on the board never reached plan.toml and `dross task next` picked from a
// stale plan. Removing the edge again fails this.
//
// The `--apply` needle matters as much as the verb: the bare command only
// reports, so an edge that never names `--apply` can surface drift but can never
// close it. (r-01: the prompt edit is only live after `make install`.)
func TestExecutePromptPullsBoardTaskMoves(t *testing.T) {
	content := executePromptContent(t)
	for _, needle := range []string{"dross issue task pull", "--apply"} {
		if !strings.Contains(content, needle) {
			t.Errorf("execute.md must wire the inbound board task pull: missing %q", needle)
		}
	}
}
