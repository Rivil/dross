package cmd

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// readDotted/writeDotted are the dotted-path field accessors the
// /dross-init slash command relies on for `dross project set X Y`.
// If a path is missing here, the prompt can't write that field.
//
// These tests pin the supported set so adding/removing a path is
// a deliberate change with test surface, not a silent drift.

func TestReadDottedSupportedPaths(t *testing.T) {
	p := &project.Project{
		Project: project.ProjectMeta{
			Name:        "feast",
			Version:     "1.2.3.0",
			Description: "meal plans",
		},
		Stack: project.Stack{
			PackageManager: "pnpm",
			Languages:      []string{"typescript", "go"},
			Frameworks:     []string{"sveltekit", "drizzle"},
		},
		Runtime: project.Runtime{
			Mode:             "docker",
			DevCommand:       "docker compose up app",
			TestCommand:      "docker compose exec app pnpm test",
			TypecheckCommand: "docker compose exec app pnpm typecheck",
			LintCommand:      "docker compose exec app pnpm lint",
			BuildCommand:     "docker compose exec app pnpm build",
			MigrateCommand:   "docker compose exec app pnpm db:migrate",
		},
		Repo: project.Repo{
			GitMainBranch: "main",
			Layout:        "single",
		},
		Goals: project.Goals{
			CoreValue: "respects household constraints",
		},
	}

	cases := map[string]string{
		"project.name":              "feast",
		"project.description":       "meal plans",
		"project.version":           "1.2.3.0",
		"stack.package_manager":     "pnpm",
		"stack.languages":           "typescript,go",
		"stack.frameworks":          "sveltekit,drizzle",
		"runtime.mode":              "docker",
		"runtime.dev_command":       "docker compose up app",
		"runtime.test_command":      "docker compose exec app pnpm test",
		"runtime.typecheck_command": "docker compose exec app pnpm typecheck",
		"runtime.lint_command":      "docker compose exec app pnpm lint",
		"runtime.build_command":     "docker compose exec app pnpm build",
		"runtime.migrate_command":   "docker compose exec app pnpm db:migrate",
		"repo.git_main_branch":      "main",
		"repo.layout":               "single",
		"goals.core_value":          "respects household constraints",
	}
	for path, want := range cases {
		got, ok := readDotted(p, path)
		if !ok {
			t.Errorf("readDotted(%q): not found", path)
			continue
		}
		if got != want {
			t.Errorf("readDotted(%q): got %q want %q", path, got, want)
		}
	}
}

func TestReadDottedUnknownPath(t *testing.T) {
	p := &project.Project{}
	if _, ok := readDotted(p, "nonsense.field"); ok {
		t.Error("expected ok=false for unknown path")
	}
	if _, ok := readDotted(p, "project.nonsense"); ok {
		t.Error("expected ok=false for unknown subfield")
	}
}

func TestWriteDottedRoundTripsThroughReadDotted(t *testing.T) {
	p := &project.Project{}
	cases := map[string]string{
		"project.name":              "x-app",
		"project.description":       "tagline",
		"project.version":           "0.2.0.0",
		"stack.package_manager":     "pnpm",
		"runtime.mode":              "docker",
		"runtime.dev_command":       "docker compose up",
		"runtime.test_command":      "docker compose exec app pnpm test",
		"runtime.typecheck_command": "tsc --noEmit",
		"runtime.lint_command":      "eslint .",
		"runtime.build_command":     "vite build",
		"runtime.migrate_command":   "drizzle-kit push",
		"repo.git_main_branch":      "main",
		"repo.layout":               "monorepo",
		"goals.core_value":          "ship fast",
	}
	for path, value := range cases {
		if err := writeDotted(p, path, value); err != nil {
			t.Fatalf("writeDotted(%q, %q): %v", path, value, err)
		}
	}
	for path, want := range cases {
		got, ok := readDotted(p, path)
		if !ok {
			t.Errorf("read after write: %q not found", path)
			continue
		}
		if got != want {
			t.Errorf("round-trip %q: got %q want %q", path, got, want)
		}
	}
}

func TestWriteDottedSplitsCSV(t *testing.T) {
	p := &project.Project{}

	if err := writeDotted(p, "stack.languages", "typescript, go,  csharp,gdscript "); err != nil {
		t.Fatal(err)
	}
	got := p.Stack.Languages
	want := []string{"typescript", "go", "csharp", "gdscript"}
	if len(got) != len(want) {
		t.Fatalf("languages len: got %d want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("languages[%d]: got %q want %q", i, got[i], w)
		}
	}

	// frameworks share the same splitter
	if err := writeDotted(p, "stack.frameworks", "sveltekit,drizzle,paraglide"); err != nil {
		t.Fatal(err)
	}
	if len(p.Stack.Frameworks) != 3 {
		t.Errorf("frameworks: %v", p.Stack.Frameworks)
	}
}

func TestWriteDottedDropsEmptyCSVEntries(t *testing.T) {
	p := &project.Project{}
	if err := writeDotted(p, "stack.languages", "typescript,,go,"); err != nil {
		t.Fatal(err)
	}
	if len(p.Stack.Languages) != 2 {
		t.Errorf("expected empty entries dropped: got %v", p.Stack.Languages)
	}
}

func TestWriteDottedRejectsUnknownPath(t *testing.T) {
	p := &project.Project{}
	err := writeDotted(p, "nonsense.field", "x")
	if err == nil {
		t.Fatal("expected error for unknown path")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("error should mention 'unknown': %v", err)
	}
}
