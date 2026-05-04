package codex

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// findCallers does a best-effort cross-file reference scan. For each
// symbol in `defined`, walk the project (rooted at the first target
// file's nearest ancestor with a `.dross` or `.git` dir) looking for
// other files that mention the symbol's name. Naive token match —
// false positives possible on common names (e.g. "id", "New") — but
// good enough for the LLM as a "where else does this appear" hint.
//
// The output Symbols carry:
//   - Name = the defined symbol's name (so the caller knows what
//     match they're looking at)
//   - Kind = "ref"
//   - File = the file that mentions the name
//   - Line = first line in that file where the name appears
//
// Capped at 50 results overall to keep `dross codex` output bounded.
func findCallers(targetFiles []string, defined []Symbol) []Symbol {
	if len(defined) == 0 {
		return nil
	}
	root := projectRoot(targetFiles)
	if root == "" {
		return nil
	}

	// Index target file paths so we don't report a symbol's defining
	// file as one of its callers.
	targetSet := map[string]bool{}
	for _, t := range targetFiles {
		if abs, err := filepath.Abs(t); err == nil {
			targetSet[abs] = true
		}
	}
	// Names to scan for. Strip method prefixes (e.g. "Foo.Bar" → "Bar")
	// so we find calls written `obj.Bar(...)` regardless of receiver.
	names := map[string]bool{}
	for _, s := range defined {
		bare := s.Name
		if i := strings.LastIndex(bare, "."); i >= 0 {
			bare = bare[i+1:]
		}
		// Skip very common short names — they're ref-spam magnets.
		if len(bare) < 3 {
			continue
		}
		names[bare] = true
	}
	if len(names) == 0 {
		return nil
	}

	var out []Symbol
	const cap = 50
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || len(out) >= cap {
			return nil
		}
		if d.IsDir() {
			// Skip noisy dirs — they rarely contain meaningful refs
			// and dominate the walk time.
			name := d.Name()
			if name == ".git" || name == ".dross" || name == "node_modules" ||
				name == "vendor" || name == "dist" || name == "build" ||
				name == ".idea" || name == ".vscode" {
				return filepath.SkipDir
			}
			return nil
		}
		if targetSet[path] {
			return nil
		}
		if !looksScannable(path) {
			return nil
		}
		matches := scanFileForNames(path, names)
		for name, line := range matches {
			out = append(out, Symbol{Name: name, Kind: "ref", File: path, Line: line})
			if len(out) >= cap {
				break
			}
		}
		return nil
	})
	return out
}

// projectRoot finds the nearest ancestor of the first target file that
// contains a .dross or .git directory. Falls back to the file's parent
// if neither is found.
func projectRoot(targetFiles []string) string {
	if len(targetFiles) == 0 {
		return ""
	}
	start := targetFiles[0]
	abs, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	dir := filepath.Dir(abs)
	for {
		for _, marker := range []string{".dross", ".git"} {
			if info, err := os.Stat(filepath.Join(dir, marker)); err == nil && info.IsDir() {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Dir(abs)
}

// looksScannable returns true for file extensions worth grepping for
// references. The list is broad — refs across language boundaries are
// useful (e.g. a TS test referencing a Go-generated symbol). Skips
// binary-ish extensions to avoid scanning gigabytes of node_modules
// images even after the directory pruning above.
func looksScannable(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
		".svelte", ".vue", ".cs", ".gd", ".gdshader",
		".html", ".htm", ".css", ".scss", ".sass",
		".py", ".rb", ".rs", ".java", ".kt", ".swift",
		".md", ".toml", ".yaml", ".yml", ".json":
		return true
	}
	return false
}

// scanFileForNames reads a file line-by-line and returns the first
// line number for each name in `names` that appears as a substring on
// any line. Substring (not whole-word) is intentional — call sites
// often spell symbols inside other tokens (e.g. "FooBar" inside
// "FooBarFactory"). The cost is more false positives, paid by the
// reader.
func scanFileForNames(path string, names map[string]bool) map[string]int {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	// Some generated/minified files have very long lines; bump the
	// buffer so we don't fail on those.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	out := map[string]int{}
	lineNo := 0
	remaining := len(names)
	for scanner.Scan() {
		lineNo++
		if remaining == 0 {
			break
		}
		line := scanner.Text()
		for name := range names {
			if _, already := out[name]; already {
				continue
			}
			if strings.Contains(line, name) {
				out[name] = lineNo
				remaining--
			}
		}
	}
	return out
}
