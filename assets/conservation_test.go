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
// in a fenced block of each manifest prompt and every `- **...**` lead-in, as
// they stood before the split; for inlineSpanPrompts it also records every
// inline backticked span starting with `dross `, since their prose carries
// steps outside any fence. Each row must still appear in the expanded prompt,
// or be claimed by exactly one `// absorbs: <row>` marker in the internal/cmd
// source that now does the step.

const manifestFile = "testdata/prompt_instructions.txt"

// recordEnv appends rows for every manifest prompt that has none yet, from
// today's expanded prompts. It never rewrites a prompt's existing rows: those
// were recorded before a split, and rows absorbed into the CLI since are gone
// from the prompt, so re-extracting them would drop them. It is for adding a
// prompt, never for silencing a failure.
const recordEnv = "DROSS_RECORD_PROMPT_INSTRUCTIONS"

var manifestPrompts = []string{"execute.md", "verify.md", "options.md", "ship.md", "plan.md", "spec.md", "quick.md"}

// inlineSpanPrompts also contribute inline `dross ...` spans outside fences.
var inlineSpanPrompts = map[string]bool{"options.md": true, "ship.md": true, "plan.md": true, "spec.md": true, "quick.md": true}

// minRows keeps the manifest from going vacuous: for the prompts added after
// execute and verify, 80% of the rows first recorded.
var minRows = map[string]int{"execute.md": 40, "verify.md": 25, "options.md": 58, "ship.md": 26, "plan.md": 20, "spec.md": 29, "quick.md": 29}

var (
	leadInRe     = regexp.MustCompile(`^- (\*\*.+?\*\*)`)
	absorbRe     = regexp.MustCompile(`^\s*// absorbs: (.+?)\s*$`)
	inlineSpanRe = regexp.MustCompile("`(dross [^`]+)`")
)

// extractInstructions returns body's fenced `dross `/`git ` lines and its
// top-level `- **...**` lead-ins, deduplicated, in order. With inline set it
// also returns each inline `dross ...` span outside a fence, in line order.
func extractInstructions(body string, inline bool) []string {
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
		if inline {
			for _, m := range inlineSpanRe.FindAllStringSubmatch(line, -1) {
				add(strings.TrimSpace(m[1]))
			}
		}
	}
	return out
}

// recordManifest returns existing with rows appended for each manifest prompt
// that has none in it, extracted from fsys's expanded prompts. existing comes
// back byte-identical as the output's prefix.
func recordManifest(fsys fs.FS, existing string) (string, error) {
	have := map[string]bool{}
	for _, line := range strings.Split(existing, "\n") {
		if prompt, _, ok := strings.Cut(line, "\t"); ok {
			have[prompt] = true
		}
	}
	var b strings.Builder
	b.WriteString(existing)
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		b.WriteString("\n")
	}
	for _, prompt := range manifestPrompts {
		if have[prompt] {
			continue
		}
		exp, err := Expand(fsys, prompt)
		if err != nil {
			return "", err
		}
		for _, row := range extractInstructions(string(exp), inlineSpanPrompts[prompt]) {
			b.WriteString(prompt + "\t" + row + "\n")
		}
	}
	return b.String(), nil
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
		existing, err := os.ReadFile(manifestFile)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		out, err := recordManifest(FS, string(existing))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifestFile, []byte(out), 0o644); err != nil {
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
	// quick.md was recorded before quick end absorbed section 6's bookkeeping.
	for _, row := range []string{
		"dross state bump internal",
		`dross issue quick $NEW_VERSION "quick: <one-line summary>"`,
	} {
		if !contains(manifest["quick.md"], row) {
			t.Errorf("manifest lacks quick.md row %q — was it recorded after section 6 moved?", row)
		}
	}
}

func TestManifestRecordIsAppendOnly(t *testing.T) {
	absorbed := []string{
		"dross state set version <major>.<minor>.$PATCH.0",
		"dross issue phase sync <id> --status in-progress",
		"dross issue task pull <phase>",
	}
	// execute.md rows as recorded, the absorbed three included, plus one that
	// no prompt carries any more: re-extracting execute.md would drop it.
	existing := "execute.md\tdross gone from every prompt\n"
	for _, row := range absorbed {
		existing += "execute.md\t" + row + "\n"
	}
	out, err := recordManifest(FS, existing)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, existing) {
		t.Fatalf("recording rewrote existing rows; got prefix:\n%s", out[:min(len(out), len(existing))])
	}
	added := strings.TrimPrefix(out, existing)
	if strings.Contains(added, "execute.md\t") {
		t.Error("recording re-extracted execute.md, which already had rows")
	}
	for _, prompt := range manifestPrompts[1:] {
		if !strings.Contains(added, "\n"+prompt+"\t") && !strings.HasPrefix(added, prompt+"\t") {
			t.Errorf("recording added no rows for %s, which had none", prompt)
		}
	}
	// A recorded manifest is a fixed point.
	again, err := recordManifest(FS, out)
	if err != nil {
		t.Fatal(err)
	}
	if again != out {
		t.Error("re-recording a full manifest changed it")
	}
}

func TestExtractInstructions(t *testing.T) {
	body := "# p\n- **Rule one.** run `dross x y` then `git z`\n  - **nested** skipped\n```\ndross a b\n# dross comment\n  git add x\necho no `dross in fence`\ndross a b\n```\ndross outside fence, then **`dross c <id>`** and `dross a b`\n"
	got := strings.Join(extractInstructions(body, false), "|")
	if want := "- **Rule one.**|dross a b|git add x"; got != want {
		t.Errorf("extractInstructions = %q, want %q", got, want)
	}
	got = strings.Join(extractInstructions(body, true), "|")
	if want := "- **Rule one.**|dross x y|dross a b|git add x|dross c <id>"; got != want {
		t.Errorf("extractInstructions(inline) = %q, want %q", got, want)
	}
}

// withoutRow is the whole prompt tree, partials included, with row replaced
// by stand-in in every file.
func withoutRow(t *testing.T, row, stand string) fstest.MapFS {
	t.Helper()
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
		fsys[f] = &fstest.MapFile{Data: []byte(strings.ReplaceAll(string(b), row, stand))}
	}
	return fsys
}

func TestConservationCatchesALostRow(t *testing.T) {
	for _, c := range []struct {
		prompt, row, stand string
		inline             bool // the row sits in prose, not a fence
	}{
		{"verify.md", "dross verify finalize <phase-id>", "dross verify done", false},
		{"ship.md", "dross ship <phase-id>", "dross ship it", false},
		{"quick.md", "dross local set quick_base <base>", "dross local set", true},
	} {
		t.Run(c.prompt, func(t *testing.T) {
			exp, err := Expand(FS, c.prompt)
			if err != nil {
				t.Fatal(err)
			}
			rows := extractInstructions(string(exp), inlineSpanPrompts[c.prompt])
			if !contains(rows, c.row) {
				t.Fatalf("today's %s does not yield row %q; pick another", c.prompt, c.row)
			}
			if c.inline && contains(extractInstructions(string(exp), false), c.row) {
				t.Fatalf("%q is fenced in %s; it does not prove the inline extractor", c.row, c.prompt)
			}
			manifest := map[string][]string{c.prompt: {c.row}}
			problems := strings.Join(conservationProblems(withoutRow(t, c.row, c.stand), manifest, nil), "\n")
			if !strings.Contains(problems, c.prompt+": instruction lost: "+c.row) {
				t.Errorf("a removed row went unreported; problems:\n%s", problems)
			}
		})
	}
	const row = "dross verify finalize <phase-id>"
	fsys := withoutRow(t, row, "dross verify done")
	manifest := map[string][]string{"verify.md": {row}}
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
