package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestEnforceSubcommandKnown_UnknownSubcommandErrors(t *testing.T) {
	// Cobra already errors with "unknown command" at the root level,
	// regardless of RunE. The guard's job is the parent-of-parent case
	// (e.g. `dross phase add`), where cobra would otherwise print help
	// and exit 0. Nest one level to exercise that.
	root := &cobra.Command{Use: "root"}
	parent := &cobra.Command{Use: "parent"}
	parent.AddCommand(&cobra.Command{Use: "child", RunE: func(*cobra.Command, []string) error { return nil }})
	root.AddCommand(parent)
	EnforceSubcommandKnown(root)

	err := runCmd(t, root, "parent", "missing")
	if err == nil {
		t.Fatal("expected error for unknown subcommand, got nil")
	}
	if !strings.Contains(err.Error(), `unknown subcommand "missing"`) {
		t.Errorf("error missing the expected phrase: %v", err)
	}
}

func TestEnforceSubcommandKnown_NoArgsStillPrintsHelp(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	root.AddCommand(&cobra.Command{Use: "child", RunE: func(*cobra.Command, []string) error { return nil }})
	EnforceSubcommandKnown(root)

	// No args on a parent with subcommands should still succeed (help screen).
	if err := runCmd(t, root); err != nil {
		t.Errorf("bare parent invocation should not error, got: %v", err)
	}
}

func TestEnforceSubcommandKnown_ValidSubcommandUntouched(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	called := false
	root.AddCommand(&cobra.Command{
		Use:  "child",
		RunE: func(*cobra.Command, []string) error { called = true; return nil },
	})
	EnforceSubcommandKnown(root)

	if err := runCmd(t, root, "child"); err != nil {
		t.Fatalf("valid subcommand returned error: %v", err)
	}
	if !called {
		t.Error("valid subcommand's RunE was not invoked")
	}
}

func TestEnforceSubcommandKnown_PreservesExistingRun(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	ran := false
	parent := &cobra.Command{
		Use:  "parent",
		RunE: func(*cobra.Command, []string) error { ran = true; return nil },
	}
	parent.AddCommand(&cobra.Command{Use: "child", RunE: func(*cobra.Command, []string) error { return nil }})
	root.AddCommand(parent)
	EnforceSubcommandKnown(root)

	// Parent already has a RunE — guard must not overwrite it.
	if err := runCmd(t, root, "parent", "anything"); err != nil {
		t.Fatalf("parent with own RunE should not error on extra args: %v", err)
	}
	if !ran {
		t.Error("parent's pre-existing RunE was clobbered by the guard")
	}
}

func TestEnforceSubcommandKnown_Suggests(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	root.AddCommand(&cobra.Command{Use: "list", RunE: func(*cobra.Command, []string) error { return nil }})
	EnforceSubcommandKnown(root)

	err := runCmd(t, root, "lits")
	if err == nil || !strings.Contains(err.Error(), "Did you mean") || !strings.Contains(err.Error(), "list") {
		t.Errorf("expected suggestion for 'lits' -> 'list', got: %v", err)
	}
}

func TestEnforceSubcommandKnown_Recurses(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	parent := &cobra.Command{Use: "parent"}
	parent.AddCommand(&cobra.Command{Use: "grandchild", RunE: func(*cobra.Command, []string) error { return nil }})
	root.AddCommand(parent)
	EnforceSubcommandKnown(root)

	err := runCmd(t, root, "parent", "nope")
	if err == nil || !strings.Contains(err.Error(), `unknown subcommand "nope"`) {
		t.Errorf("guard did not recurse into nested parent: %v", err)
	}
}

func TestSubcommandGuardCover_SuggestionBlockExactString(t *testing.T) {
	// Pin the exact concatenation on subcommand_guard.go:37 so any mutation of
	// the `+=`/`+` that assembles the "Did you mean this?" block is observable.
	root := &cobra.Command{Use: "root"}
	parent := &cobra.Command{Use: "parent"}
	parent.AddCommand(&cobra.Command{Use: "list", RunE: func(*cobra.Command, []string) error { return nil }})
	root.AddCommand(parent)
	EnforceSubcommandKnown(root)

	err := runCmd(t, root, "parent", "lits")
	if err == nil {
		t.Fatal("expected error for near-miss subcommand 'lits'")
	}
	// The block must appear verbatim: header, blank line, then a tab-indented
	// suggestion. A dropped or altered concatenation would change this exactly.
	if !strings.Contains(err.Error(), "\n\nDid you mean this?\n\tlist") {
		t.Errorf("suggestion block not assembled verbatim, got: %q", err.Error())
	}
	// And the far-off "Available subcommands" branch must NOT have fired.
	if strings.Contains(err.Error(), "Available subcommands") {
		t.Errorf("near-miss must not list available subcommands: %q", err.Error())
	}
}

func TestEnforceSubcommandKnown_ListsAvailableWhenNoSuggestion(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	parent := &cobra.Command{Use: "parent"}
	parent.AddCommand(
		&cobra.Command{Use: "alpha", RunE: func(*cobra.Command, []string) error { return nil }},
		&cobra.Command{Use: "beta", RunE: func(*cobra.Command, []string) error { return nil }},
	)
	root.AddCommand(parent)
	EnforceSubcommandKnown(root)

	// A far-off guess (no close match) should list what IS valid rather than
	// only pointing at --help, and must not fabricate a suggestion.
	err := runCmd(t, root, "parent", "zzzzz")
	if err == nil {
		t.Fatal("expected error for unknown far-off subcommand")
	}
	for _, want := range []string{"Available subcommands", "alpha", "beta"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "Did you mean") {
		t.Errorf("far-off guess should not produce a suggestion: %v", err)
	}
}

// realTaskTree builds a `dross task {status,show,edit}` tree so the guard is
// exercised against the real command paths the curated table keys on.
func realTaskTree() *cobra.Command {
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Task())
	EnforceSubcommandKnown(root)
	return root
}

func TestSubcommandGuardPrefersCuratedHint(t *testing.T) {
	root := realTaskTree()
	err := runCmd(t, root, "task", "done", "t-1")
	if err == nil {
		t.Fatal("`dross task done t-1` succeeded")
	}
	if !strings.Contains(err.Error(), "dross task status") {
		t.Errorf("error does not carry the curated hint: %v", err)
	}
	// The curated entry wins: no competing "Did you mean" list alongside it.
	if strings.Contains(err.Error(), "Did you mean") {
		t.Errorf("cobra's suggestion list fired alongside the curated hint: %v", err)
	}
}

// TestSubcommandGuardFallbackBeyondCobra pins that the fallback adds reach
// rather than echoing cobra. `hswoo` is distance 3 from `show` — outside
// cobra's SuggestionsMinimumDistance of 2 — so cobra finds nothing and only
// our Nearest can name it.
func TestSubcommandGuardFallbackBeyondCobra(t *testing.T) {
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(State())
	EnforceSubcommandKnown(root)

	state, _, err := root.Find([]string{"state"})
	if err != nil {
		t.Fatal(err)
	}
	// First prove cobra gives up — otherwise the assertion below proves
	// nothing about our fallback.
	if sug := state.SuggestionsFor("hswoo"); len(sug) > 0 {
		t.Fatalf("cobra already suggests %v for `hswoo`; pick a typo outside its reach", sug)
	}
	runErr := runCmd(t, root, "state", "hswoo")
	if runErr == nil {
		t.Fatal("`dross state hswoo` succeeded")
	}
	if !strings.Contains(runErr.Error(), "show") {
		t.Errorf("the fallback did not name `show`: %v", runErr)
	}
}

func TestSubcommandGuardFarOffGuessStillListsAvailable(t *testing.T) {
	root := realTaskTree()
	err := runCmd(t, root, "task", "wibble")
	if err == nil {
		t.Fatal("`dross task wibble` succeeded")
	}
	if !strings.Contains(err.Error(), "Available subcommands") {
		t.Errorf("the available-subcommands fallback was replaced, not added to: %v", err)
	}
	if !strings.Contains(err.Error(), "status") {
		t.Errorf("the available list does not name the real subcommands: %v", err)
	}
}

func TestSubcommandGuardDoneStillExitsNonZero(t *testing.T) {
	// The pre-existing exit-0-on-help regression: a parent with an unknown
	// subcommand must error, not print help and succeed.
	if err := runCmd(t, realTaskTree(), "task", "done"); err == nil {
		t.Error("`dross task done` exited 0")
	}
}
