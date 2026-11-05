package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/spf13/pflag"

	"github.com/Rivil/dross/assets"
	"github.com/Rivil/dross/internal/gatestate"
)

// showRepoFixture is an initialised repo whose SessionStart hook has announced
// session "s", with that id in the environment.
func showRepoFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	ruleCovFakeHome(t)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := gatestate.BumpContext(dir, "s"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "s")
	return dir
}

func ruleShowOut(t *testing.T, args ...string) string {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Rule(), append([]string{"show"}, args...)...) })
	if err != nil {
		t.Fatalf("rule show: %v", err)
	}
	return out
}

func interactionShowOut(t *testing.T, args ...string) string {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Interaction(), append([]string{"show"}, args...)...) })
	if err != nil {
		t.Fatalf("interaction show: %v", err)
	}
	return out
}

func TestRuleShowRepeatPrintsMarker(t *testing.T) {
	showRepoFixture(t)
	first := ruleShowOut(t)
	if !strings.Contains(first, "<rules>") {
		t.Fatalf("the first show is not the full block:\n%s", first)
	}
	second := ruleShowOut(t)
	if second != RulesUnchanged+"\n" {
		t.Fatalf("the repeat printed %q, want exactly the RulesUnchanged line", second)
	}
	if len(second) > 160 || !strings.Contains(second, "MUST-FOLLOW") || !strings.Contains(second, "dross rule show --full") {
		t.Fatalf("the marker (%d bytes) must be ≤ 160 bytes and name MUST-FOLLOW and --full: %q", len(second), second)
	}
}

func TestRuleShowChangeReprints(t *testing.T) {
	t.Run("rule add", func(t *testing.T) {
		showRepoFixture(t)
		ruleShowOut(t)
		if err := runCmd(t, Rule(), "add", "show-marker-change-sentinel"); err != nil {
			t.Fatal(err)
		}
		if out := ruleShowOut(t); !strings.Contains(out, "show-marker-change-sentinel") {
			t.Fatalf("a changed rule set printed the marker:\n%s", out)
		}
	})
	t.Run("global rules", func(t *testing.T) {
		showRepoFixture(t)
		ruleShowOut(t)
		home, _ := os.UserHomeDir()
		mustWrite(t, filepath.Join(home, ".claude", "dross", "rules.toml"), "[[rule]]\n  id = \"g-01\"\n  text = \"global-change-sentinel\"\n  severity = \"hard\"\n")
		if out := ruleShowOut(t); !strings.Contains(out, "global-change-sentinel") {
			t.Fatalf("a changed global rules.toml printed the marker:\n%s", out)
		}
	})
}

func TestRuleShowNoSessionFull(t *testing.T) {
	dir := showRepoFixture(t)
	if err := os.RemoveAll(filepath.Join(dir, ".dross", "gate", gatestate.ShowDir)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	os.Unsetenv("CLAUDE_CODE_SESSION_ID")
	for i := 0; i < 2; i++ {
		if out := ruleShowOut(t); !strings.Contains(out, "<rules>") {
			t.Fatalf("show %d without a session id printed:\n%s", i+1, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".dross", "gate", gatestate.ShowDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a show without a session id wrote show records (stat err = %v)", err)
	}
}

func TestMarkerNamesFullCommand(t *testing.T) {
	for marker, cmd := range map[string]string{RulesUnchanged: "dross rule show --full", PlaybookUnchanged: "dross interaction show --full"} {
		if !strings.Contains(marker, "not in your context?") || !strings.Contains(marker, cmd) {
			t.Errorf("marker %q must say \"not in your context?\" and name %q", marker, cmd)
		}
	}
	// CLAUDE_CODE_CHILD_SESSION is set in background-job main sessions and
	// their subagents alike, so it changes nothing: the marker still prints,
	// and names the way back.
	showRepoFixture(t)
	ruleShowOut(t)
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	if out := ruleShowOut(t); out != RulesUnchanged+"\n" {
		t.Fatalf("with CLAUDE_CODE_CHILD_SESSION set the repeat printed %q", out)
	}
}

func TestShowRecordsOnlyAfterPrint(t *testing.T) {
	showRepoFixture(t)
	prev := showPrint
	showPrint = func(string) error { return syscall.EPIPE }
	ruleShowOut(t)
	showPrint = prev
	t.Cleanup(func() { showPrint = prev })
	if out := ruleShowOut(t); !strings.Contains(out, "<rules>") {
		t.Fatalf("a print that failed was recorded; the next show printed:\n%s", out)
	}
}

func TestShowStoreFailureStillPrints(t *testing.T) {
	dir := showRepoFixture(t)
	gate := filepath.Join(dir, ".dross", "gate")
	if err := os.RemoveAll(gate); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, gate, "not a directory\n")
	for i := 0; i < 2; i++ {
		if out := ruleShowOut(t); !strings.Contains(out, "<rules>") {
			t.Fatalf("show %d with a broken store printed:\n%s", i+1, out)
		}
	}
}

func TestInteractionShowMarker(t *testing.T) {
	showRepoFixture(t)
	if out := interactionShowOut(t); out != assets.InteractionPlaybook {
		t.Fatal("the first interaction show is not the playbook")
	}
	ruleShowOut(t)
	out := interactionShowOut(t)
	if out != PlaybookUnchanged+"\n" || len(out) > 160 || !strings.Contains(out, "dross interaction show --full") {
		t.Fatalf("the repeat printed %q, want exactly the PlaybookUnchanged line", out)
	}
	// A rules-only change reprints the rules and leaves the playbook marked.
	if err := runCmd(t, Rule(), "add", "rules-only-change"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ruleShowOut(t), "rules-only-change") {
		t.Fatal("the rules change did not reprint the rules")
	}
	if out := interactionShowOut(t); out != PlaybookUnchanged+"\n" {
		t.Fatalf("a rules-only change reprinted the playbook: %q", out[:min(len(out), 80)])
	}
}

func TestShowFullFlag(t *testing.T) {
	showRepoFixture(t)
	ruleShowOut(t)
	if out := ruleShowOut(t, "--full"); !strings.Contains(out, "<rules>") {
		t.Fatalf("--full after a current record printed %q", out)
	}
	interactionShowOut(t)
	if out := interactionShowOut(t, "--full"); out != assets.InteractionPlaybook {
		t.Fatal("interaction show --full did not print the playbook")
	}
	for _, m := range []string{RulesUnchanged, PlaybookUnchanged} {
		if !strings.Contains(m, "--full") || !strings.Contains(m, "MUST-FOLLOW") {
			t.Errorf("marker %q lacks --full or MUST-FOLLOW", m)
		}
	}
}

func TestRuleListMergedNeverMarks(t *testing.T) {
	showRepoFixture(t)
	ruleShowOut(t)
	for i := 0; i < 2; i++ {
		var err error
		out := captureStdout(t, func() { err = runCmd(t, Rule(), "list", "--merged") })
		if err != nil || !strings.Contains(out, "<rules>") {
			t.Fatalf("rule list --merged %d printed %q (err %v), want the full block", i+1, out, err)
		}
	}
}

func TestRulePromptShowUsesListMerged(t *testing.T) {
	b, err := assets.FS.ReadFile("prompts/rule.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, `"show what claude sees"`) {
			if !strings.Contains(l, "`dross rule list --merged`") {
				t.Fatalf("the show-what-claude-sees row is %q, want `dross rule list --merged` (a human must never get the marker)", l)
			}
			return
		}
	}
	t.Fatal("rule.md has no \"show what claude sees\" row")
}

func TestShowFlags(t *testing.T) {
	for name, c := range map[string]interface {
		Flags() *pflag.FlagSet
	}{"rule show": ruleShow(), "interaction show": interactionShow()} {
		var got []string
		c.Flags().VisitAll(func(f *pflag.Flag) { got = append(got, f.Name) })
		sort.Strings(got)
		if strings.Join(got, ",") != "full" {
			t.Errorf("%s defines flags %v, want exactly full", name, got)
		}
	}
}
