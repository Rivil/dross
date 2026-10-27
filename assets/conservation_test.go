package assets

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// Instruction conservation: moving prompt text into partials, or into the CLI,
// must not lose an instruction. The manifest records, once, every command line
// in a fenced block of execute.md and verify.md and every `- **...**` lead-in,
// as they stood before the split. Each row must still appear in the expanded
// prompt, or be claimed by exactly one `// absorbs: <row>` marker in the
// internal/cmd source that now does the step.

const manifestFile = "testdata/prompt_instructions.txt"

// recordEnv rewrites the manifest from today's expanded prompts. Re-recording
// after rows were absorbed would drop them, so it is for the first recording
// and for adding a prompt, never for silencing a failure.
const recordEnv = "DROSS_RECORD_PROMPT_INSTRUCTIONS"

var manifestPrompts = []string{"execute.md", "verify.md"}

// minRows keeps the manifest from going vacuous.
var minRows = map[string]int{"execute.md": 40, "verify.md": 25}

var (
	leadInRe = regexp.MustCompile(`^- (\*\*.+?\*\*)`)
	absorbRe = regexp.MustCompile(`^\s*// absorbs: (.+?)\s*$`)
)

// extractInstructions returns body's fenced `dross `/`git ` lines and its
// top-level `- **...**` lead-ins, deduplicated, in order.
func extractInstructions(body string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(r string) {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	fenced := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			if strings.HasPrefix(trimmed, "dross ") || strings.HasPrefix(trimmed, "git ") {
				add(trimmed)
			}
			continue
		}
		if m := leadInRe.FindStringSubmatch(line); m != nil {
			add("- " + m[1])
		}
	}
	return out
}

// readManifest parses `<prompt>\t<row>` lines into rows per prompt.
func readManifest(t *testing.T) map[string][]string {
	t.Helper()
	f, err := os.Open(manifestFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		prompt, row, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			t.Fatalf("%s: malformed line %q", manifestFile, sc.Text())
		}
		out[prompt] = append(out[prompt], row)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// absorbsMarkers maps each `// absorbs:` row to the file:line places claiming
// it, across internal/cmd's non-test Go source.
func absorbsMarkers(t *testing.T) map[string][]string {
	t.Helper()
	dir := filepath.Join("..", "internal", "cmd")
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if m := absorbRe.FindStringSubmatch(line); m != nil {
				out[m[1]] = append(out[m[1]], filepath.Base(f)+":"+itoa(i+1))
			}
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// conservationProblems lists every manifest row neither in its expanded
// prompt nor claimed by exactly one marker, and every marker claiming no row.
func conservationProblems(fsys fs.FS, manifest, markers map[string][]string) []string {
	var problems []string
	rows := map[string]bool{}
	for _, prompt := range manifestPrompts {
		exp, err := Expand(fsys, prompt)
		if err != nil {
			problems = append(problems, prompt+": "+err.Error())
			continue
		}
		for _, row := range manifest[prompt] {
			rows[row] = true
			if strings.Contains(string(exp), row) {
				continue
			}
			switch n := len(markers[row]); {
			case n == 0:
				problems = append(problems, prompt+": instruction lost: "+row)
			case n > 1:
				problems = append(problems, prompt+": "+row+" is claimed by "+itoa(n)+" absorbs markers ("+strings.Join(markers[row], ", ")+"), want one")
			}
		}
	}
	var stale []string
	for row, at := range markers {
		if !rows[row] {
			stale = append(stale, strings.Join(at, ", ")+": absorbs marker matches no manifest row: "+row)
		}
	}
	sort.Strings(stale)
	return append(problems, stale...)
}

func TestPromptInstructionsConserved(t *testing.T) {
	if os.Getenv(recordEnv) != "" {
		var b strings.Builder
		for _, prompt := range manifestPrompts {
			exp, err := Expand(FS, prompt)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range extractInstructions(string(exp)) {
				b.WriteString(prompt + "\t" + row + "\n")
			}
		}
		if err := os.WriteFile(manifestFile, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := readManifest(t)
	for prompt, min := range minRows {
		if n := len(manifest[prompt]); n < min {
			t.Errorf("manifest holds %d rows for %s, want at least %d — the extractor went vacuous", n, prompt, min)
		}
	}
	markers := absorbsMarkers(t)
	for _, p := range conservationProblems(FS, manifest, markers) {
		t.Error(p)
	}
	// execute begin absorbed section 0's three mechanical steps; each row has
	// exactly one claim, and it lives in execute.go.
	for _, row := range []string{
		"dross state set version <major>.<minor>.$PATCH.0",
		"dross issue phase sync <id> --status in-progress",
		"dross issue task pull <phase>",
	} {
		if !contains(manifest["execute.md"], row) {
			t.Errorf("manifest lost execute.md row %q", row)
		}
		at := markers[row]
		if len(at) != 1 || !strings.HasPrefix(at[0], "execute.go:") {
			t.Errorf("row %q is claimed at %v, want exactly one absorbs marker in execute.go", row, at)
		}
	}
}

func TestExtractInstructions(t *testing.T) {
	body := "# p\n- **Rule one.** text\n  - **nested** skipped\n```\ndross a b\n# dross comment\n  git add x\necho no\ndross a b\n```\ndross outside fence\n"
	got := strings.Join(extractInstructions(body), "|")
	if want := "- **Rule one.**|dross a b|git add x"; got != want {
		t.Errorf("extractInstructions = %q, want %q", got, want)
	}
}

func TestConservationCatchesALostRow(t *testing.T) {
	const row = "dross verify finalize <phase-id>"
	exp, err := Expand(FS, "verify")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exp), row) {
		t.Fatalf("expanded verify.md no longer carries %q; pick another row", row)
	}
	// The whole prompt tree, partials included, with the row gone from every file.
	files, err := fs.Glob(FS, "prompts/*.md")
	if err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{}
	for _, f := range files {
		b, err := FS.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		fsys[f] = &fstest.MapFile{Data: []byte(strings.ReplaceAll(string(b), row, "dross verify done"))}
	}
	manifest := map[string][]string{"verify.md": {row}}
	problems := strings.Join(conservationProblems(fsys, manifest, nil), "\n")
	if !strings.Contains(problems, "instruction lost: "+row) {
		t.Errorf("a removed row went unreported; problems:\n%s", problems)
	}
	// Claimed by one marker, it passes; once that marker goes, it fails.
	claimed := map[string][]string{row: {"verify.go:1"}}
	if p := conservationProblems(fsys, manifest, claimed); len(p) != 0 {
		t.Errorf("a marker-claimed row still failed: %v", p)
	}
	twice := map[string][]string{row: {"verify.go:1", "verify.go:9"}}
	if p := strings.Join(conservationProblems(fsys, manifest, twice), "\n"); !strings.Contains(p, "want one") {
		t.Errorf("a doubly claimed row passed: %q", p)
	}
	stale := map[string][]string{"dross nothing here": {"x.go:3"}}
	if p := strings.Join(conservationProblems(FS, map[string][]string{}, stale), "\n"); !strings.Contains(p, "matches no manifest row") {
		t.Errorf("a stale marker passed: %q", p)
	}
}
