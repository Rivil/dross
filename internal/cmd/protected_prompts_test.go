package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The slash-command prompts route around a protected base (phase
// main-branch-protection, c-1/c-5/c-10): /dross-quick asks
// `dross protect --check` and takes a quick/ branch and PR when the base
// refuses direct pushes; /dross-ship and /dross-milestone say what a chore PR
// is and when to re-run. Pinned here because a prompt edit has no compiler.

// quickPRRouteBullet is quick.md's PR-route bullet: the §0.4 line for a
// protected or unknown base.
func quickPRRouteBullet(t *testing.T, quick string) string {
	t.Helper()
	for _, line := range strings.Split(quick, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- `protected` or `unknown`") {
			return line
		}
	}
	t.Fatal("quick.md has no `protected` or `unknown` route bullet")
	return ""
}

func TestQuickPromptRoutesProtectedBase(t *testing.T) {
	quick := readPrompt(t, "quick.md")
	for _, want := range []string{"dross protect --check <base>", "quick/<", "**PR route**", "**direct route**"} {
		if !strings.Contains(quick, want) {
			t.Errorf("quick.md lost %q", want)
		}
	}
	if strings.Contains(quick, "go to that base directly") {
		t.Error("quick.md still sends every standalone quick straight to the base")
	}
	pr := quickPRRouteBullet(t, quick)
	if strings.Contains(pr, "local set quick_base") || !strings.Contains(pr, "Record **no** quick_base") {
		t.Errorf("the PR route must record no quick_base — a feature branch is not a base:\n%s", pr)
	}
}

// The PR route names its commands, and none of them merges the PR for the
// user or around the checks.
func TestQuickPRRouteCommands(t *testing.T) {
	quick := readPrompt(t, "quick.md")
	for _, want := range []string{"git push -u origin quick/", "gh pr create --base", "git pull --ff-only"} {
		if !strings.Contains(quick, want) {
			t.Errorf("quick.md's PR route lost %q", want)
		}
	}
	for _, banned := range []string{"gh pr merge", "--auto", "--admin"} {
		if strings.Contains(quick, banned) {
			t.Errorf("quick.md carries %q — the user merges a quick PR once CI is green", banned)
		}
	}
}

// TestQuickClosesBoardIssueBeforeLeavingTheQuickBranch: the close resolves the
// quick's issue through the link §6 writes into .dross/board.json, which on the
// PR route is committed on quick/<NEW_VERSION> only. A close placed after
// `dross checkout <base>` reads the base's board.json, finds no link, and fails
// "no board issue linked to quick ref" on every PR-route quick (seen live on
// quick 1.7.26.1). So: one close, after the issue is opened, before the switch.
func TestQuickClosesBoardIssueBeforeLeavingTheQuickBranch(t *testing.T) {
	quick := readPrompt(t, "quick.md")
	const (
		open   = `dross issue quick $NEW_VERSION "quick:`
		close  = "dross issue quick $NEW_VERSION --close"
		toBase = "dross checkout <base>"
	)
	if n := strings.Count(quick, close); n != 1 {
		t.Fatalf("quick.md carries %d %q steps, want exactly one", n, close)
	}
	o, c, b := strings.Index(quick, open), strings.Index(quick, close), strings.Index(quick, toBase)
	if o < 0 || b < 0 {
		t.Fatalf("quick.md lost its issue-open (%t) or base-switch (%t) step", o >= 0, b >= 0)
	}
	if !(o < c && c < b) {
		t.Errorf("quick.md must open the issue, close it, then switch to the base; "+
			"got open@%d close@%d switch@%d — a close after the switch cannot see the link", o, c, b)
	}
}

// directBasePush matches a prompt telling the agent to push a base branch.
var directBasePush = regexp.MustCompile(`git push (?:-u )?origin (?:main|<base>|milestone/)`)

func TestPromptsNeverPushTheBase(t *testing.T) {
	dir := filepath.Join(repoRootFromTest(t), "assets", "prompts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".md" {
			continue
		}
		seen++
		if m := directBasePush.FindString(readPrompt(t, e.Name())); m != "" {
			t.Errorf("%s says %q — a protected base refuses it; go through a PR", e.Name(), m)
		}
	}
	if seen < 10 {
		t.Fatalf("swept only %d prompts", seen)
	}
	if directBasePush.FindString("then `git push origin main`") == "" || directBasePush.FindString("git push -u origin quick/1.2.3.4") != "" {
		t.Error("directBasePush misreads its fixtures")
	}
}

func TestShipAndMilestoneNarrateChorePR(t *testing.T) {
	ship := readPrompt(t, "ship.md")
	for _, want := range []string{"chore PR", "dross-chores/<base>", "re-run **`dross phase complete <phase-id>`**", "never `--recover` here"} {
		if !strings.Contains(ship, want) {
			t.Errorf("ship.md lost %q", want)
		}
	}
	milestone := readPrompt(t, "milestone.md")
	if !strings.Contains(milestone, "chore PR merges first") {
		t.Error("milestone.md no longer says the chore PR merges first")
	}
	deletes := false
	for _, line := range strings.Split(milestone, "\n") {
		if strings.Contains(line, "--finalize") && strings.Contains(line, "still deletes") {
			deletes = true
		}
	}
	if !deletes {
		t.Error("milestone.md no longer says --finalize still deletes the branch")
	}
}

// protectRef matches a `dross protect …` invocation and its flags.
var protectRef = regexp.MustCompile("`dross protect((?: --[a-z-]+(?: [^`\\s]+)?)*)`?")

// Every `dross protect` a prompt runs resolves against the real command,
// flags included. TestShipPromptCommandsExist reads only ship.md and never
// looks at flags.
func TestProtectedPromptCommandsResolve(t *testing.T) {
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Protect())
	flag := regexp.MustCompile(`--([a-z-]+)`)
	for _, name := range []string{"quick.md", "ship.md", "milestone.md"} {
		refs := protectRef.FindAllStringSubmatch(readPrompt(t, name), -1)
		if len(refs) == 0 {
			t.Errorf("%s runs no `dross protect` — the protected-base route is gone", name)
			continue
		}
		for _, r := range refs {
			c, _, err := root.Find([]string{"protect"})
			if err != nil || c.Name() != "protect" {
				t.Fatalf("`dross protect` does not resolve: %v", err)
			}
			for _, f := range flag.FindAllStringSubmatch(r[1], -1) {
				if c.Flags().Lookup(f[1]) == nil {
					t.Errorf("%s runs `dross protect --%s`, which is not a flag", name, f[1])
				}
			}
			if !strings.Contains(r[1], "--check") {
				t.Errorf("%s runs `dross protect%s` without --check — prompts only ask", name, r[1])
			}
		}
	}
}

func TestReadmeProtectedBaseRows(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	for _, cmd := range []string{"/dross-quick", "/dross-ship", "/dross-milestone"} {
		row := ""
		for _, line := range strings.Split(readme, "\n") {
			if strings.HasPrefix(line, "| `"+cmd+"` |") {
				row = line
			}
		}
		if row == "" {
			t.Errorf("README has no %s row", cmd)
			continue
		}
		for _, want := range []string{"dross protect --check", "chore PR"} {
			if !strings.Contains(row, want) {
				t.Errorf("README's %s row lost %q", cmd, want)
			}
		}
	}
}
