package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// codeAdd is one fenced `git add` in a prompt that stages something outside
// .dross/ — a commit the commit-green gate judges.
type codeAdd struct {
	prompt, heading, line string
	// tested is whether a bare `dross test` runs between the previous fenced
	// `git add`/`git commit` (or the top of the prompt) and this one.
	tested bool
}

func (a codeAdd) String() string {
	return fmt.Sprintf("%s %q: `%s`", a.prompt, a.heading, a.line)
}

// codeAdds audits one prompt body. It keys on the `git add`, not the `git
// commit`: execute §1f and quick §5 stage in one fence and commit in another,
// or narrate the commit without a command line at all. A commit's window runs
// back to the previous commit in the same prompt rather than to its heading —
// quick.md's §4 test gate covers its §5 commit across a heading, with nothing
// edited in between. Only fenced lines count: prose like "never `git add -A`"
// is not a command, and a `#` line inside a fence is not a heading.
func codeAdds(prompt, body string) []codeAdd {
	var out []codeAdd
	fenced, heading, tested := false, "", false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if !fenced {
			if strings.HasPrefix(line, "#") {
				heading = line
			}
			continue
		}
		switch {
		case line == "dross test":
			tested = true
		case strings.HasPrefix(line, "git add "):
			if stagesCode(line) {
				out = append(out, codeAdd{prompt: prompt, heading: heading, line: line, tested: tested})
			}
			tested = false
		case strings.HasPrefix(line, "git commit"):
			tested = false
		}
	}
	return out
}

// stagesCode reports whether a `git add` line stages a path outside .dross/.
// The pathspecs end at the first shell operator (`git add . && git commit`).
func stagesCode(line string) bool {
	for _, w := range strings.Fields(strings.TrimPrefix(line, "git add ")) {
		if w == "&&" || w == ";" || w == "||" {
			break
		}
		if strings.HasPrefix(w, "-") || strings.HasPrefix(w, ".dross/") || w == ".dross" {
			continue
		}
		return true
	}
	return false
}

// TestPromptCommitsSatisfyGreenGate: the commit-green gate refuses a code
// commit whose tree no full `dross test` passed, so every prompt step that
// stages code must run a bare `dross test` first. .dross-only commits — verify,
// review, execute §2, quick §6 — are bookkeeping the gate passes ungated.
func TestPromptCommitsSatisfyGreenGate(t *testing.T) {
	root := repoRootForHybridTest(t)
	entries, err := os.ReadDir(filepath.Join(root, "assets", "prompts"))
	if err != nil {
		t.Fatal(err)
	}
	var all []codeAdd
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		all = append(all, codeAdds(e.Name(), promptBody(t, e.Name()))...)
	}
	for _, a := range all {
		if !a.tested {
			t.Errorf("%s stages code with no bare `dross test` since the previous commit — the commit gate will refuse it", a)
		}
	}

	// Vacuity: the audit must actually be reading the two task-loop commits.
	for _, want := range [][2]string{{"execute.md", "### 1f."}, {"quick.md", "## 5."}} {
		found := false
		for _, a := range all {
			found = found || (a.prompt == want[0] && strings.HasPrefix(a.heading, want[1]))
		}
		if !found {
			t.Errorf("the audit matched no code `git add` in %s %s — it is passing vacuously (matched: %v)", want[0], want[1], all)
		}
	}

	// It bites: ship.md without its pre-commit `dross test` fails, and a
	// .dross-only add is never matched.
	ship := bareDrossTestRE.ReplaceAllString(promptBody(t, "ship.md"), "")
	bites := false
	for _, a := range codeAdds("ship.md", ship) {
		bites = bites || !a.tested
	}
	if !bites {
		t.Error("removing ship.md's `dross test` step left every code commit passing")
	}
	if got := codeAdds("x.md", "```\ngit add .dross/\ngit commit -m x\n```\n"); len(got) != 0 {
		t.Errorf("a .dross-only add was matched: %v", got)
	}
}
