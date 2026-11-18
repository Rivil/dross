package cmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// TestDocsDropRetiredContextNudge: the context nudge and its [context]
// threshold knob are retired, so neither the README nor the man page may still
// document them as a setting to reach for.
func TestDocsDropRetiredContextNudge(t *testing.T) {
	root := repoRootFromTest(t)
	for _, rel := range []string{"README.md", "docs/dross.1"} {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		for _, gone := range []string{"[context] threshold", "{ensure,nudge}", "\\fBnudge\\fR", "context checkpoint:"} {
			if strings.Contains(string(b), gone) {
				t.Errorf("%s still documents the retired context nudge (%q)", rel, gone)
			}
		}
	}
}

// fourLensCount matches a /dross-review lens count from before the panel grew
// to six. It deliberately leaves `all four` alone: the README's "all four
// providers" is not a lens count.
var fourLensCount = regexp.MustCompile(`(?i)\b(four|4)[- ]lens|across 4 lenses`)

// knownLensSurfaces are the files that describe /dross-review's panel; the
// stale-count guard must visit every one of them.
var knownLensSurfaces = []string{
	"README.md",
	"docs/dross.1",
	"docs/subagent-offload-audit.md",
	"assets/prompts/review.md",
	"assets/commands/dross-review.md",
}

// lensSurfaceFiles lists, root-relative with slash separators, README.md,
// ARCHITECTURE.md and every .md or .1 file under docs/ and assets/.
func lensSurfaceFiles(t *testing.T, root string) []string {
	t.Helper()
	files := []string{"README.md", "ARCHITECTURE.md"}
	for _, dir := range []string{"docs", "assets"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if ext := filepath.Ext(p); ext != ".md" && ext != ".1" {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

// TestNoFourLensSurface (c-5): no doc or prompt still counts the review panel
// as four lenses, and the walk reaches every surface known to describe it.
func TestNoFourLensSurface(t *testing.T) {
	root := repoRootFromTest(t)
	files := lensSurfaceFiles(t, root)

	visited := map[string]bool{}
	for _, f := range files {
		visited[f] = true
	}
	var missing []string
	for _, k := range knownLensSurfaces {
		if !visited[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		t.Errorf("guard visited %d of the %d known lens surfaces; missing %v",
			len(knownLensSurfaces)-len(missing), len(knownLensSurfaces), missing)
	}

	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if m := fourLensCount.FindString(line); m != "" {
				t.Errorf("%s:%d still counts the review panel as four lenses (%q)", f, i+1, m)
			}
		}
	}
}

// reviewLensTokens are the six /dross-review lenses as every surface names them.
var reviewLensTokens = []string{"security", "quality", "tests", "spec-fidelity", "edge-case", "silent-failure"}

// TestReviewSurfacesNameSixLenses (c-5): each place a user meets the review
// panel names all six lenses, so a dropped lens cannot hide behind a correct
// count word.
func TestReviewSurfacesNameSixLenses(t *testing.T) {
	root := repoRootFromTest(t)
	read := func(parts ...string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	lineWhere := func(doc string, match func(string) bool) string {
		for _, line := range strings.Split(doc, "\n") {
			if match(line) {
				return line
			}
		}
		return ""
	}
	// between returns doc's text after start up to end, or "" without start.
	between := func(doc, start, end string) string {
		_, body, ok := strings.Cut(doc, start)
		if !ok {
			return ""
		}
		body, _, _ = strings.Cut(body, end)
		return body
	}

	readme := read("README.md")
	surfaces := []struct{ name, text string }{
		{"README /dross-review row", lineWhere(readme, func(l string) bool {
			return strings.HasPrefix(l, "| `/dross-review` |")
		})},
		{"README roadmap /dross-review bullet", lineWhere(readme, func(l string) bool {
			return strings.HasPrefix(l, "- [x] `/dross-review`")
		})},
		{"docs/dross.1 /dross-review entry", between(read("docs", "dross.1"), "\n.B /dross-review ", "\n.TP")},
		{"docs/subagent-offload-audit.md review entry", between(read("docs", "subagent-offload-audit.md"), "\n### review\n", "\n### ")},
		{"dross-review command description", lineWhere(read("assets", "commands", "dross-review.md"), func(l string) bool {
			return strings.HasPrefix(l, "description:")
		})},
		{"review.md summary line", lineWhere(expandedPrompt(t, "review.md"), func(l string) bool {
			return strings.Contains(l, "<one-line summary:")
		})},
	}
	for _, s := range surfaces {
		if s.text == "" {
			t.Errorf("%s not found", s.name)
			continue
		}
		for _, tok := range reviewLensTokens {
			if !strings.Contains(s.text, tok) {
				t.Errorf("%s does not name the %s lens: %q", s.name, tok, s.text)
			}
		}
	}
}

// optionsSection returns the body of options.md's "## <n>." section — the
// heading line excluded, so a check cannot be satisfied by the title alone —
// up to the next "## ", read from the expanded prompt so a section's partial
// text is part of its body.
func optionsSection(t *testing.T, n int) string {
	t.Helper()
	_, sec, ok := strings.Cut(expandedPrompt(t, "options.md"), "\n## "+strconv.Itoa(n)+". ")
	if !ok {
		t.Fatalf("options.md has no §%d", n)
	}
	sec, _, _ = strings.Cut(sec, "\n## ")
	_, body, _ := strings.Cut(sec, "\n")
	return body
}

// hookCountWord matches a spelled-out hook count — a word that goes stale
// whenever a hook is added or retired.
var hookCountWord = regexp.MustCompile(`(?i)\b(two|three|four|five|six|seven|eight)\b`)

// TestOptionsHooksSectionNamesAllHooks: §15 lists every hook `dross hooks
// ensure` wires, by command, and carries no count word to go stale again.
func TestOptionsHooksSectionNamesAllHooks(t *testing.T) {
	sec := optionsSection(t, 15)
	var want []string
	for _, h := range userHooks {
		want = append(want, h.command)
	}
	for _, w := range want {
		if !strings.Contains(sec, w) {
			t.Errorf("options.md §15 does not name `%s`", w)
		}
	}
	if m := hookCountWord.FindString(sec); m != "" {
		t.Errorf("options.md §15 counts the hooks (%q); name them instead, so the count cannot drift", m)
	}
}
