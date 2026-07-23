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
