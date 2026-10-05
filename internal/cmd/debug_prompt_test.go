package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/debugsession"
)

// TestDebugPromptRequirements pins every c-9 duty /dross-debug's prompt
// carries, one named subtest per requirement, so deleting one names it.
func TestDebugPromptRequirements(t *testing.T) {
	body := promptBody(t, "debug.md")
	loop := promptSection(t, body, "## 2. Probe loop")
	closing := promptSection(t, body, "## 5. Close")
	cases := []struct {
		name  string
		scope string
		wants []string
	}{
		{"one hypothesis per probe", body, []string{
			"Pick **one hypothesis**", "Write **one probe**", "One hypothesis per probe",
		}},
		{"evidence and verdict before the next probe", loop, []string{
			"**Write the evidence and the verdict to the session before the next probe starts.**",
			"the next probe does not begin until the session holds this one's result",
		}},
		{"tree returned to the pre-session snapshot", body, []string{
			"git status --porcelain\n", "git diff HEAD | shasum -a 256\n",
			"The tree must match this snapshot before and after every probe",
			"**Return the tree to the pre-session snapshot.**", "re-run both snapshot lines and compare",
		}},
		{"dross debug list after every fix attempt", loop, []string{
			"**run `dross debug list` after every Fix-attempts entry**",
			"If the session reads `needs-replan`, or `dross debug list` prints any `problem:` line for it, go to §3.",
		}},
		{"BLOCKED hard stop in pair and solo", body, []string{
			"## 3. Hard stop — BLOCKED", "in pair **and** in solo", "put this on the first line of your reply",
			"BLOCKED: debug session <slug> needs a re-plan", "BLOCKED: debug session <slug> is malformed",
			"lists a `problem:` line", "the hard stops below apply in solo exactly as in pair",
		}},
		{"fix routed, never committed", body, []string{
			"/dross-debug never commits", "**Standalone** (no phase execution in progress) → `/dross-quick`",
			"**Mid-phase** (a `/dross-execute` run is in progress on this phase) → add the fix as a task with `dross task add <phase-id>`",
		}},
		{"snapshot retaken after a fix lands", body, []string{
			"When the fix has landed, record its commit in the session and **retake the pre-session snapshot**",
		}},
		{"Edit for session updates", body, []string{
			"Every update to a session goes through `Edit`", "A `Write` over an existing session is forbidden",
		}},
		{"independent close signals", closing, []string{
			"two **independent** signals", "the original repro no longer reproduces",
			"a regression test that was red before the fix and is green after it",
			"the suite (`dross test`) or CI is green", "the commit the fix landed in, cited among them",
		}},
		{"rule offered in pair, listed in solo", closing, []string{
			"**Pair:** offer it with `AskUserQuestion`", "runs exactly the printed line",
			"**Solo:** do not run it; list it in the wrap-up",
		}},
		{"session content never published", body, []string{
			"Never paste session content into a commit message, a PR body or a board issue",
			"The one sanctioned exit is the `dross rule add` line",
			"Prevention must be a rule statement carrying no captured output, paths, hostnames or tokens",
		}},
		{"tests run through dross test", body, []string{"Run tests through `dross test`"}},
		{"interaction playbook", body, []string{"interaction playbook", "`dross interaction show`"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, w := range c.wants {
				if !strings.Contains(c.scope, w) {
					t.Errorf("debug.md lacks %q", w)
				}
			}
		})
	}
	t.Run("no raw test command", func(t *testing.T) {
		if loc := rawInterpolationRE.FindString(body); loc != "" {
			t.Errorf("debug.md interpolates the raw test command: %q", loc)
		}
	})
}

// forbiddenDebugCommands are the command lines /dross-debug must never run:
// commits are routed through /dross-quick or a task, the tree is restored by
// undoing a probe's own edits, and a rule is added only from the printed line.
var forbiddenDebugCommands = []string{"git add", "git commit", "git reset --hard", "git clean", "git stash", "git checkout -- .", "dross rule add"}

// commandSplitRE splits a shell line into the commands it chains.
var commandSplitRE = regexp.MustCompile(`&&|\|\||;|\|`)

// fencedForbidden returns every fenced line of body that runs a forbidden
// command — as the line's first command or chained after another.
func fencedForbidden(body string) []string {
	var out []string
	fenced := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if !fenced {
			continue
		}
		if slices.ContainsFunc(commandSplitRE.Split(line, -1), func(cmd string) bool {
			cmd = strings.TrimSpace(cmd)
			return slices.ContainsFunc(forbiddenDebugCommands, func(f string) bool { return strings.HasPrefix(cmd, f) })
		}) {
			out = append(out, line)
		}
	}
	return out
}

func TestDebugPromptNoCommitPath(t *testing.T) {
	body := promptBody(t, "debug.md")
	if adds := codeAdds("debug.md", body); len(adds) != 0 {
		t.Errorf("debug.md stages code: %v", adds)
	}
	if hits := fencedForbidden(body); len(hits) != 0 {
		t.Errorf("debug.md runs forbidden commands: %q", hits)
	}
	// The scan itself bites: every forbidden command in a fence is reported.
	var fixture strings.Builder
	fixture.WriteString("prose `git stash` is not a command\n```\n")
	for _, f := range forbiddenDebugCommands {
		fixture.WriteString("  " + f + " x\n")
	}
	chained := []string{"git add -A && git commit -m x", "cd sub && git stash", "dross test; git reset --hard", "true || git clean -fd"}
	for _, c := range chained {
		fixture.WriteString(c + "\n")
	}
	fixture.WriteString("dross test ./internal/x/...\n```\n")
	if hits := fencedForbidden(fixture.String()); len(hits) != len(forbiddenDebugCommands)+len(chained) {
		t.Fatalf("the fixture's %d forbidden lines yielded %q", len(forbiddenDebugCommands)+len(chained), hits)
	}
}

func TestDebugPromptParity(t *testing.T) {
	body := promptBody(t, "debug.md")

	t.Run("headings", func(t *testing.T) {
		var named []string
		for _, m := range regexp.MustCompile("`## ([^`]+)`").FindAllStringSubmatch(body, -1) {
			if !slices.Contains(named, m[1]) {
				named = append(named, m[1])
			}
		}
		for _, h := range named {
			if !slices.Contains(debugsession.Headings(), h) {
				t.Errorf("debug.md names `## %s`, which is not a session heading", h)
			}
		}
		for _, h := range debugsession.Headings() {
			if !slices.Contains(named, h) {
				t.Errorf("debug.md never names `## %s`", h)
			}
		}
	})

	t.Run("markers", func(t *testing.T) {
		named := map[string]bool{}
		for _, m := range regexp.MustCompile("`- (\\[[^\\]`]*\\])").FindAllStringSubmatch(body, -1) {
			named[m[1]] = true
		}
		want := map[string]bool{debugsession.MarkerFailed: true, debugsession.MarkerReplan: true}
		for m := range named {
			if !want[m] {
				t.Errorf("debug.md names marker %s, which the parser does not count", m)
			}
		}
		for m := range want {
			if !named[m] {
				t.Errorf("debug.md never names marker %s", m)
			}
		}
	})

	t.Run("states", func(t *testing.T) {
		want := []string{string(debugsession.StateAbandoned), string(debugsession.StateNeedsReplan), string(debugsession.StateOpen), string(debugsession.StateResolved)}
		i := strings.Index(body, "States `dross debug list` reports:")
		if i < 0 {
			t.Fatal("debug.md has no states line")
		}
		line, _, _ := strings.Cut(body[i+len("States `dross debug list` reports:"):], ".")
		var got []string
		for _, m := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(line, -1) {
			got = append(got, m[1])
		}
		sort.Strings(got)
		if !slices.Equal(got, want) {
			t.Errorf("states line names %q, want %q", got, want)
		}
		wrap := regexp.MustCompile(`Debug session <slug>: <([^>]+)>`).FindStringSubmatch(body)
		if wrap == nil {
			t.Fatal("debug.md has no wrap-up state line")
		}
		got = strings.Split(wrap[1], " | ")
		sort.Strings(got)
		if !slices.Equal(got, want) {
			t.Errorf("wrap-up names states %q, want %q", got, want)
		}
	})

	t.Run("thresholds", func(t *testing.T) {
		if debugsession.ReplanThreshold != 3 || !strings.Contains(body, "Three `- [failed]` items since the last `- [replan]` read `needs-replan`") {
			t.Errorf("the prompt's re-plan threshold drifted from ReplanThreshold=%d", debugsession.ReplanThreshold)
		}
		if debugsession.MinSignals != 2 || !strings.Contains(body, "at least two **independent** signals") {
			t.Errorf("the prompt's signal count drifted from MinSignals=%d", debugsession.MinSignals)
		}
	})
}

// TestDebugPromptIsClassified: the new command is enrolled in every
// fail-closed audit as an interactive, footer-bearing prompt.
func TestDebugPromptIsClassified(t *testing.T) {
	root := repoRootFromTest(t)
	if st, err := promptFooterState(root, "debug"); err != nil || st != footerPresent {
		t.Errorf("promptFooterState(debug) = %v, %v; want footerPresent", st, err)
	}
	if interactive, err := shimIsInteractive(root, "debug"); err != nil || !interactive {
		t.Errorf("the dross-debug shim is not interactive: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, "docs", "interaction-audit.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	i := strings.Index(doc, "\n### dross-debug\n")
	if i < 0 {
		t.Fatal("interaction-audit.md has no ### dross-debug section")
	}
	section := doc[i+1:]
	if j := strings.Index(section[1:], "\n### "); j >= 0 {
		section = section[:j+1]
	}
	if !strings.Contains(section, "✅") {
		t.Error("the dross-debug audit section marks nothing conforming")
	}
	for _, bad := range []string{"⬜", "🟡", "❌"} {
		if strings.Contains(section, bad) {
			t.Errorf("the dross-debug audit section carries %s", bad)
		}
	}
	if gaps, err := offloadAuditGaps(root); err != nil || slices.Contains(gaps, "debug") {
		t.Errorf("subagent-offload-audit.md has no ### debug section: %v", err)
	}
}
