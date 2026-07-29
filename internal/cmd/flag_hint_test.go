package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/telemetry"
)

// hintedTree is a `dross {task,phase}` tree with the hooks installed the same
// way main.go installs them — one EnforceSubcommandKnown call on the root.
func hintedTree() *cobra.Command {
	root := &cobra.Command{Use: "dross", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(Task(), Phase())
	EnforceSubcommandKnown(root)
	return root
}

func TestFlagHintUsesCuratedEntry(t *testing.T) {
	err := runCmd(t, hintedTree(), "phase", "create", "--title", "x")
	if err == nil {
		t.Fatal("`phase create --title x` succeeded")
	}
	if !strings.Contains(err.Error(), `dross phase create "<title>"`) {
		t.Errorf("cobra's bare message was not augmented with the curated fix: %v", err)
	}
}

func TestFlagHintFallsBackToNearestDeclaredFlag(t *testing.T) {
	err := runCmd(t, hintedTree(), "task", "edit", "01-p", "t-1", "--titel", "x")
	if err == nil {
		t.Fatal("`task edit --titel` succeeded")
	}
	if !strings.Contains(err.Error(), "--title") {
		t.Errorf("the nearest declared flag was not named: %v", err)
	}
}

func TestFlagHintNamesNothingWhenNothingIsClose(t *testing.T) {
	err := runCmd(t, hintedTree(), "task", "edit", "01-p", "t-1", "--zzzzzzzzz", "x")
	if err == nil {
		t.Fatal("an unknown flag succeeded")
	}
	if strings.Contains(err.Error(), "Did you mean") {
		t.Errorf("a suggestion was fabricated for a far-off flag: %v", err)
	}
}

// TestFlagHintReachesDepthThree proves cobra's FlagErrorFunc parent-walk
// rather than assuming it: the hook is installed only on the root, and
// `dross task edit` is three levels down.
func TestFlagHintReachesDepthThree(t *testing.T) {
	root := hintedTree()
	edit, _, err := root.Find([]string{"task", "edit"})
	if err != nil {
		t.Fatal(err)
	}
	if edit.CommandPath() != "dross task edit" {
		t.Fatalf("expected a depth-3 command, got %q", edit.CommandPath())
	}
	if edit.FlagErrorFunc() == nil {
		t.Error("no FlagErrorFunc resolved at depth 3 — the parent-walk did not reach it")
	}
	runErr := runCmd(t, root, "task", "edit", "01-p", "t-1", "--files", "a.go")
	if runErr == nil {
		t.Fatal("`task edit --files a.go` succeeded")
	}
	if !strings.Contains(runErr.Error(), "dross task add") {
		t.Errorf("the depth-3 curated hint did not fire: %v", runErr)
	}
}

func TestFlagHintLeavesWorkingInvocationsAlone(t *testing.T) {
	root := &cobra.Command{Use: "dross", SilenceUsage: true, SilenceErrors: true}
	var ran bool
	var title string
	child := &cobra.Command{
		Use:  "child",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { ran = true; return nil },
	}
	child.Flags().StringVar(&title, "title", "", "")
	parent := &cobra.Command{Use: "parent"}
	parent.AddCommand(child)
	root.AddCommand(parent)
	EnforceSubcommandKnown(root)

	if err := runCmd(t, root, "parent", "child", "--title", "ok"); err != nil {
		t.Fatalf("a known flag stopped parsing: %v", err)
	}
	if !ran || title != "ok" {
		t.Errorf("RunE ran=%v, title=%q — installing the hooks changed behaviour", ran, title)
	}
}

// TestHintedFlagErrorStillClassifiesAsUnknownFlag asserts through telemetry's
// own classifier, not by eyeballing the string: a replace-the-message
// implementation drops the "unknown flag" substring telemetry matches on and
// silently demotes the event to `other`.
func TestHintedFlagErrorStillClassifiesAsUnknownFlag(t *testing.T) {
	err := runCmd(t, hintedTree(), "phase", "create", "--title", "x")
	if err == nil {
		t.Fatal("expected an unknown-flag error")
	}
	if got := telemetry.ClassifyError(err); got != "unknown_flag" {
		t.Errorf("hinted flag error classifies as %q, want unknown_flag\nmessage: %v", got, err)
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("cobra's literal prefix was not preserved: %v", err)
	}
}

func TestOffendingFlag(t *testing.T) {
	cases := map[string]string{
		"unknown flag: --title":              "--title",
		"unknown shorthand flag: 'x' in -xy": "-x",
		"flag needs an argument: --title":    "",
		"some other error":                   "",
	}
	for msg, want := range cases {
		if got := offendingFlag(msg); got != want {
			t.Errorf("offendingFlag(%q) = %q, want %q", msg, got, want)
		}
	}
}
