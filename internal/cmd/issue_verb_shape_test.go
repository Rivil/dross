package cmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The five mirror verbs are nested subcommands, not hyphenated compounds.
// These guards hold that shape from both ends: the cobra tree must expose the
// nested spelling and refuse the old one, and no Go source may still tell a
// user to type the old one.

// nestedIssueVerbs is the full set the rename produced. Each entry is the
// argv path under `dross issue`.
var nestedIssueVerbs = [][]string{
	{"phase", "sync"},
	{"task", "sync"},
	{"task", "pull"},
	{"milestone", "sync"},
	{"backlog", "sync"},
}

// TestNestedIssueVerbsResolve fails when a nested path no longer resolves —
// the direct proof that the rename landed on the real command tree rather
// than only in the prose that describes it.
func TestNestedIssueVerbsResolve(t *testing.T) {
	for _, path := range nestedIssueVerbs {
		got, _, err := Issue().Find(path)
		if err != nil {
			t.Errorf("issue %s does not resolve: %v", strings.Join(path, " "), err)
			continue
		}
		// Find falls back to the nearest resolvable ancestor rather than
		// erroring on a missing leaf, so the name has to be checked too: a
		// tree with `task` but no `task sync` would otherwise pass here.
		if got.Name() != path[len(path)-1] {
			t.Errorf("issue %s resolved to %q, not the leaf", strings.Join(path, " "), got.CommandPath())
		}
		if got.RunE == nil && got.Run == nil {
			t.Errorf("issue %s resolved to a command with no handler", strings.Join(path, " "))
		}
	}
}

// TestIssueVerbsAreNested walks the whole `issue` tree and fails on any
// command whose name is a hyphenated compound. Restoring "task-sync" reddens
// this even if the nested pair is left in place alongside it.
func TestIssueVerbsAreNested(t *testing.T) {
	var walk func(c *cobra.Command)
	seen := 0
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			seen++
			if strings.Contains(sub.Name(), "-") {
				t.Errorf("%s is a hyphenated compound verb; nest it as a parent+child pair", sub.CommandPath())
			}
			walk(sub)
		}
	}
	walk(Issue())
	if seen == 0 {
		t.Fatal("walked no subcommands — the guard would pass vacuously")
	}
}

// TestOldCompoundIssueVerbIsRejected: the old spelling must fail loudly, not
// silently print help and exit 0. That is EnforceSubcommandKnown's job, so the
// tree is wired the way main.go wires it before the call.
func TestOldCompoundIssueVerbIsRejected(t *testing.T) {
	for _, old := range []string{"phase-sync", "task-sync", "task-pull", "milestone-sync", "backlog-sync"} {
		c := Issue()
		EnforceSubcommandKnown(c)
		err := runCmd(t, c, old, "01-auth")
		if err == nil {
			t.Errorf("issue %s exited 0; the retired spelling must be an error", old)
			continue
		}
		// Cobra's own arg validation rejects it before the guard's RunE is
		// reached, so either wording is the correct outcome — what matters is
		// that the retired spelling is named as unknown rather than silently
		// printing help.
		if !strings.Contains(err.Error(), "unknown") || !strings.Contains(err.Error(), old) {
			t.Errorf("issue %s failed with %v; expected an unknown-command error naming it", old, err)
		}
	}
}

// hyphenatedIssueVerbRE matches the prose form a user is told to type.
var hyphenatedIssueVerbRE = regexp.MustCompile(`dross issue [a-z]+-[a-z]+`)

// TestNoHyphenatedIssueVerbInGoSource scans the non-test Go source for a
// user-facing instruction still naming a retired spelling — deferred_add.go's
// Printf naming `dross issue backlog-sync` fails here while the compiler stays
// green, because a string is not a call site.
//
// Test files are out of scope on purpose: several of them assert on the
// PROMPT corpus, which still carries the old spelling until that corpus is
// rewritten. Guarding the prompts is that task's job, not this one's.
func TestNoHyphenatedIssueVerbInGoSource(t *testing.T) {
	root := repoRootFromTest(t)
	scanned := 0
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(b), "\n") {
			if m := hyphenatedIssueVerbRE.FindString(line); m != "" {
				t.Errorf("%s:%d names the retired verb %q: %s", rel, i+1, m, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned no Go files — the guard would pass vacuously")
	}
}

// --- the prompt corpus ---
//
// The corpus is repointed in the same commit as the tree: cmd/dross's
// TestShipPromptCommandsExist resolves ship.md against the assembled root, so
// nesting the verbs reddens the prompts in the same instant it reddens the Go
// call sites. These three guards hold the corpus half of that boundary.

// promptIssueInvocationRE captures the argv words following `dross issue` on a
// prompt line, stopping at the first flag or placeholder. Only bare lowercase
// words are verb candidates.
var promptIssueInvocationRE = regexp.MustCompile(`dross issue((?: [a-z][a-z0-9-]*)+)`)

func promptFiles(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repoRootFromTest(t), "assets", "prompts", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("globbed no prompts — the guard would pass vacuously")
	}
	return paths
}

// TestNoHyphenatedIssueVerbInThePromptCorpus: no prompt may still tell the
// model to type a retired spelling.
func TestNoHyphenatedIssueVerbInThePromptCorpus(t *testing.T) {
	for _, p := range promptFiles(t) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if m := hyphenatedIssueVerbRE.FindString(line); m != "" {
				t.Errorf("%s:%d names the retired verb %q: %s", filepath.Base(p), i+1, m, strings.TrimSpace(line))
			}
		}
	}
}

// TestEveryPromptIssueInvocationResolves walks each `dross issue …` line in
// the corpus against the REAL cobra tree rather than an allowlist of the five
// new spellings. An allowlist would pass a prompt repointed to a verb that was
// never registered; this fails on it here, by prompt and line, instead of on a
// live board.
func TestEveryPromptIssueInvocationResolves(t *testing.T) {
	checked := 0
	for _, p := range promptFiles(t) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			for _, m := range promptIssueInvocationRE.FindAllStringSubmatch(line, -1) {
				args := strings.Fields(m[1])
				cmd, _, err := Issue().Find(args)
				if err != nil {
					t.Errorf("%s:%d `dross issue %s` does not resolve: %v", filepath.Base(p), i+1, strings.Join(args, " "), err)
					continue
				}
				checked++
				// Find returns the nearest resolvable ancestor, so a prompt
				// naming a leaf that does not exist under a real parent would
				// otherwise pass. Only a command with a handler is runnable.
				if cmd.RunE == nil && cmd.Run == nil {
					t.Errorf("%s:%d `dross issue %s` resolves to %q, which has no handler", filepath.Base(p), i+1, strings.Join(args, " "), cmd.CommandPath())
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("checked zero issue invocations — the guard would pass vacuously")
	}
}

// TestTaskSyncEdgeRegexIsNotVacuous: taskSyncEdgeRE drives the emit-set that
// board_lifecycle_divergence_test's execute-edge guard compares against. A
// rename that leaves the regex on the old spelling silently empties that set
// and the lifecycle guard then passes on nothing — a guard measuring zero
// lines is indistinguishable from a guard that agrees.
func TestTaskSyncEdgeRegexIsNotVacuous(t *testing.T) {
	matched := 0
	for _, p := range promptFiles(t) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		matched += len(taskSyncEdgeRE.FindAllStringSubmatch(string(b), -1))
	}
	if matched == 0 {
		t.Fatal("taskSyncEdgeRE matches no prompt line — the execute-edge lifecycle guard is running on an empty set")
	}
}
