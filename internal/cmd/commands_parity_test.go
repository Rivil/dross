package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommandsPromptsParity asserts the slash-command shims and the prompt
// bodies they @-include stay 1:1. Each assets/commands/dross-<name>.md must
// have a matching assets/prompts/<name>.md and vice versa. This is the
// invariant the Makefile `doctor` target checks at install time, lifted into
// `go test` so a missing engine prompt or an orphaned command fails CI.
//
// It directly guards the architecture backfill engine: if either
// assets/commands/dross-architecture.md or assets/prompts/architecture.md is
// missing, this test fails (t-3 / c-7 test_contract).
func TestCommandsPromptsParity(t *testing.T) {
	root := repoRootFromTest(t)
	cmds := mdNamesIn(t, filepath.Join(root, "assets", "commands"), "dross-")
	prompts := mdNamesIn(t, filepath.Join(root, "assets", "prompts"), "")

	for name := range cmds {
		if !prompts[name] {
			t.Errorf("command dross-%s.md has no matching prompt assets/prompts/%s.md", name, name)
		}
	}
	for name := range prompts {
		if !cmds[name] {
			t.Errorf("prompt %s.md has no matching command assets/commands/dross-%s.md", name, name)
		}
	}

	// The architecture backfill engine + its command must both exist. The
	// parity loops above miss the case where *both* are absent, so assert it
	// explicitly — this is the contract wording ("... or ... is missing").
	if !cmds["architecture"] {
		t.Error("missing command assets/commands/dross-architecture.md")
	}
	if !prompts["architecture"] {
		t.Error("missing prompt assets/prompts/architecture.md")
	}
}

// repoRootFromTest walks up from the test's working directory to the module
// root (the directory containing go.mod). Tests run with cwd set to their
// package dir, so assets/ is not directly relative.
func repoRootFromTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find module root (no go.mod above %s)", dir)
		}
		dir = parent
	}
}

// mdNamesIn returns the set of *.md basenames in dir, with prefix and the .md
// suffix stripped. Non-.md files and entries lacking the prefix are skipped.
func mdNamesIn(t *testing.T, dir, prefix string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".md") || !strings.HasPrefix(n, prefix) {
			continue
		}
		out[strings.TrimSuffix(strings.TrimPrefix(n, prefix), ".md")] = true
	}
	return out
}
