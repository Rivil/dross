package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadmeDocumentsInstallAndUpdate is c-5's guard: the README must document the
// supported install (curl|sh of install.sh) and update (`dross update`) flows. If the
// installer entrypoint or the update command is renamed without updating the docs,
// these greps fail.
func TestReadmeDocumentsInstallAndUpdate(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)

	// The curl|sh one-liner: raw githubusercontent URL for Rivil/dross's install.sh, piped to sh.
	if !strings.Contains(readme, "raw.githubusercontent.com/Rivil/dross") {
		t.Error("README missing the raw.githubusercontent.com/Rivil/dross install URL")
	}
	if !strings.Contains(readme, "install.sh | sh") {
		t.Error("README missing the `install.sh | sh` one-liner (installer entrypoint renamed?)")
	}
	if !strings.Contains(readme, "curl -fsSL") {
		t.Error("README curl one-liner missing the -fsSL flags")
	}

	// The self-update command must be documented.
	if !strings.Contains(readme, "dross update") {
		t.Error("README does not document `dross update`")
	}
}

// TestReadmeDocumentsBaseTruthSurfaces (c-6) is the same grep-needle guard for
// the surfaces this milestone's base-truth work added: the machine-local store
// and completion's explicit base. Both change how a user recovers from a
// refusal, so an undocumented one is a support burden, not a cosmetic gap.
func TestReadmeDocumentsBaseTruthSurfaces(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)

	if !strings.Contains(readme, "dross local") {
		t.Error("README does not document `dross local` (the machine-local store)")
	}
	if !strings.Contains(readme, "quick_base") {
		t.Error("README does not name `quick_base`, the store's first key")
	}
	// The completion flag, in the phase row that describes `complete`.
	phaseRow := ""
	for _, line := range strings.Split(readme, "\n") {
		if strings.HasPrefix(line, "| `dross phase {") {
			phaseRow = line
		}
	}
	if phaseRow == "" {
		t.Fatal("README no longer has a `dross phase {...}` command row to check")
	}
	if !strings.Contains(phaseRow, "--base") {
		t.Errorf("the phase row must document complete's --base escape hatch: %s", phaseRow)
	}
	if !strings.Contains(phaseRow, "--recover") {
		t.Errorf("the phase row must document complete's --recover behaviour: %s", phaseRow)
	}
}

// TestReadmeDocumentsDebugSessions: the README-sync convention for
// debug-sessions. TestReadmeAdvertisesOnlyRealCommands only catches an
// over-claim, so dropping a row would otherwise stay green.
func TestReadmeDocumentsDebugSessions(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)

	hasRow := func(prefix string) bool {
		for _, line := range strings.Split(readme, "\n") {
			if strings.HasPrefix(line, prefix) {
				return true
			}
		}
		return false
	}
	if !hasRow("| `/dross-debug` |") {
		t.Error("README's slash-command table has no `/dross-debug` row")
	}
	if !hasRow("| `dross debug {new,list,close}` |") {
		t.Error("README has no `dross debug {new,list,close}` command row")
	}

	// The per-project artefacts block must say the sessions are gitignored:
	// they quote captured output (the locked session_tracking decision).
	_, block, ok := strings.Cut(readme, "### Per-project artefacts")
	if !ok {
		t.Fatal("README has no Per-project artefacts section")
	}
	block, _, _ = strings.Cut(block, "\n### ")
	debugLine := ""
	for _, line := range strings.Split(block, "\n") {
		if strings.Contains(line, "debug/") {
			debugLine = line
		}
	}
	if debugLine == "" || !strings.Contains(debugLine, "gitignored") {
		t.Errorf("the artefacts block's debug/ line %q does not say gitignored", debugLine)
	}

	man, err := os.ReadFile(filepath.Join(root, "docs", "dross.1"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(man), "dross debug") {
		t.Error("docs/dross.1 does not document `dross debug`")
	}
	_, files, ok := strings.Cut(string(man), "\n.SH FILES\n")
	if !ok {
		t.Fatal("docs/dross.1 has no FILES section")
	}
	files, _, _ = strings.Cut(files, "\n.SH ")
	if !strings.Contains(files, ".I .dross/debug/") {
		t.Error("docs/dross.1's FILES section has no .dross/debug entry")
	}
}

// TestReadmeDocumentsRespond: the README and the man page carry
// /dross-respond, the `dross pr` verbs, the tracked pr-triage.toml and the
// watch pointer — the surfaces a user meets the feature through.
func TestReadmeDocumentsRespond(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)
	row := func(prefix string) string {
		for _, line := range strings.Split(readme, "\n") {
			if strings.HasPrefix(line, prefix) {
				return line
			}
		}
		return ""
	}
	if row("| `dross pr {comments,resolve,reply}` |") == "" {
		t.Error("README has no `dross pr {comments,resolve,reply}` command row")
	}
	if row("| `/dross-respond` |") == "" {
		t.Error("README's slash-command table has no `/dross-respond` row")
	}
	if !strings.Contains(readme, "├── dross-respond/SKILL.md") {
		t.Error("README's install layout lacks dross-respond/SKILL.md")
	}
	_, block, ok := strings.Cut(readme, "### Per-project artefacts")
	if !ok {
		t.Fatal("README has no Per-project artefacts section")
	}
	block, _, _ = strings.Cut(block, "\n### ")
	triage := ""
	for _, line := range strings.Split(block, "\n") {
		if strings.Contains(line, "pr-triage.toml") {
			triage = line
		}
	}
	if triage == "" || !strings.Contains(triage, "tracked") {
		t.Errorf("the artefacts block's pr-triage.toml line %q does not say tracked", triage)
	}
	if !strings.Contains(row("| `/dross-watch` |"), "untriaged — /dross-respond") {
		t.Error("README's /dross-watch row does not show the untriaged pointer")
	}
	_, v17, ok := strings.Cut(readme, "### Milestone v1.7")
	if !ok {
		t.Fatal("README has no v1.7 roadmap section")
	}
	v17, _, _ = strings.Cut(v17, "\n### ")
	if !strings.Contains(v17, "- [x] Review-comment ingest") {
		t.Error("the v1.7 roadmap does not record review-comment ingest")
	}

	man, err := os.ReadFile(filepath.Join(root, "docs", "dross.1"))
	if err != nil {
		t.Fatal(err)
	}
	for section, wants := range map[string][]string{
		"CLI COMMANDS":   {".B dross pr comments", ".B dross pr resolve", ".B dross pr reply"},
		"SLASH COMMANDS": {".B /dross-respond"},
		"FILES":          {".I pr-triage.toml"},
	} {
		_, body, ok := strings.Cut(string(man), "\n.SH "+section+"\n")
		if !ok {
			t.Fatalf("docs/dross.1 has no %s section", section)
		}
		body, _, _ = strings.Cut(body, "\n.SH ")
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("docs/dross.1's %s section lacks %q", section, want)
			}
		}
	}
}
